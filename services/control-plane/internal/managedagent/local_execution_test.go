package managedagent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type localExecutionFixture struct {
	clock       *time.Time
	store       *Store
	coordinator *LocalExecutionCoordinator
	executor    *localExecutionExecutor
	input       LocalExecutionInput
}

type localExecutionExecutor struct {
	mu     sync.Mutex
	calls  int
	result workerkernel.OperationExecutionResult
	wait   chan struct{}
}

func (e *localExecutionExecutor) Execute(ctx context.Context, _ workerkernel.OperationExecutionInput) (workerkernel.OperationExecutionResult, error) {
	e.mu.Lock()
	e.calls++
	wait := e.wait
	result := e.result
	e.mu.Unlock()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return workerkernel.OperationExecutionResult{}, ctx.Err()
		}
	}
	return result, nil
}

func (e *localExecutionExecutor) count() int { e.mu.Lock(); defer e.mu.Unlock(); return e.calls }

func newLocalExecutionFixture(t *testing.T, result workerkernel.OperationExecutionResult) localExecutionFixture {
	t.Helper()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	clock := &now
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/local", TrustDomain: "cloud-agents.test", LeafCertificateSha256: []byte("01234567890123456789012345678901")}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/local", TrustDomain: "cloud-agents.test", LeafCertificateSha256: []byte("12345678901234567890123456789012")}
	executor := &localExecutionExecutor{result: result}
	service, err := workerkernel.NewService(workerkernel.Config{WorkerIdentity: workerIdentity, Capabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH}, AdmissionLeaseID: "lease-local", AdmissionGeneration: 7, IdentityProvider: workerkernel.StaticIdentityProvider{Identity: supervisorIdentity}, NegotiationTTL: time.Minute, IDGenerator: func() (string, error) { return "receipt-local-1", nil }, Clock: func() time.Time { return *clock }, Executor: executor})
	if err != nil {
		t.Fatal(err)
	}
	spv, err := supervisor.NewLocal(supervisor.LocalConfig{Handle: service.LocalDispatchHandle(), ExpectedWorkerIdentity: workerIdentity, Clock: func() time.Time { return *clock }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spv.BindLocalDispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(func() time.Time { return *clock })
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}
	if _, err := store.CreateSession(context.Background(), CreateSessionInput{Scope: scope, SessionID: "session-local", ProviderKind: "localdev", Mutation: Mutation{RequestID: "session-request", IdempotencyKey: "local-op-key"}}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewLocalExecutionCoordinator(LocalExecutionCoordinatorConfig{Store: store, Supervisor: spv, Clock: func() time.Time { return *clock }, FencingLeaseID: "lease-local", FencingGeneration: 7, FencingToken: []byte("token-local")})
	if err != nil {
		t.Fatal(err)
	}
	input := LocalExecutionInput{Scope: scope, SessionID: "session-local", TurnID: "turn-local", ExecutionID: "execution-local", OperationID: "operation-local", AttemptID: "attempt-local", AttemptNumber: 1, InputText: "hello", Generation: 7, FencingLeaseID: "lease-local", FencingGeneration: 7, FencingToken: []byte("token-local"), Deadline: now.Add(20 * time.Second), Mutation: Mutation{RequestID: "local-request", IdempotencyKey: "local-op-key"}, Command: LocalExecutionCommand{Kind: LocalProbeCommand, ProbeName: "contract-only"}}
	return localExecutionFixture{clock: clock, store: store, coordinator: coordinator, executor: executor, input: input}
}

func TestLocalExecutionHappyPathAndReplay(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "local probe completed"})
	result, err := f.coordinator.Execute(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition.Turn.State != TurnCompleted || result.Transition.Execution.State != ExecutionSucceeded || result.Receipt == nil {
		t.Fatalf("result = %#v", result)
	}
	replay, err := f.coordinator.Execute(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Transition != result.Transition || f.executor.count() != 1 {
		t.Fatalf("replay = %#v calls=%d", replay.Transition, f.executor.count())
	}
	page, err := f.store.ReadEvents(context.Background(), f.input.Scope, EventCursor{}, 64)
	if err != nil || len(page.Events) != 5 {
		t.Fatalf("replay emitted duplicate lifecycle events: len=%d err=%v", len(page.Events), err)
	}
}

func TestLocalExecutionRejectsChangedDispatchIdentityBeforeWorker(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "local probe completed"})
	if _, err := f.coordinator.Execute(context.Background(), f.input); err != nil {
		t.Fatal(err)
	}
	changedCommand := f.input
	changedCommand.Command = LocalExecutionCommand{Kind: LocalProbeCommand, ProbeName: "different-probe"}
	if _, err := f.coordinator.Execute(context.Background(), changedCommand); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed command replay error = %v", err)
	}
	changedAttempt := f.input
	changedAttempt.AttemptID = "attempt-other"
	if _, err := f.coordinator.Execute(context.Background(), changedAttempt); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed attempt replay error = %v", err)
	}
	changedOperation := f.input
	changedOperation.OperationID = "operation-other"
	if _, err := f.coordinator.Execute(context.Background(), changedOperation); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed operation replay error = %v", err)
	}
	if f.executor.count() != 1 {
		t.Fatalf("changed replay invoked worker %d times", f.executor.count())
	}
	page, err := f.store.ReadEvents(context.Background(), f.input.Scope, EventCursor{}, 64)
	if err != nil || len(page.Events) != 5 {
		t.Fatalf("changed replay emitted lifecycle events: len=%d err=%v", len(page.Events), err)
	}
}

func TestLocalExecutionValidateBindingHappyPath(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "binding validated"})
	f.input.Command = LocalExecutionCommand{Kind: LocalValidateBindingCommand, Binding: f.input.Scope}
	result, err := f.coordinator.Execute(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition.Turn.State != TurnCompleted || result.Transition.Execution.State != ExecutionSucceeded || f.executor.count() != 1 {
		t.Fatalf("validate binding result = %#v calls=%d", result, f.executor.count())
	}
}

func TestLocalExecutionParallelReplayInvokesWorkerOnce(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, RedactedSummary: "local probe completed"})
	const callers = 8
	var wait sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := f.coordinator.Execute(context.Background(), f.input)
			if err == nil && (result.Transition.Turn.State != TurnCompleted || result.Transition.Execution.State != ExecutionSucceeded) {
				err = errors.New("parallel replay returned a non-terminal result")
			}
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if f.executor.count() != 1 {
		t.Fatalf("executor calls = %d", f.executor.count())
	}
}

func TestLocalExecutionRejectsUnboundAndInvalidInputsBeforeMutation(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED})
	f.input.Deadline = time.Time{}
	if _, err := f.coordinator.Execute(context.Background(), f.input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("deadline error = %v", err)
	}
	if _, err := f.store.GetTurn(context.Background(), f.input.Scope, f.input.SessionID, f.input.TurnID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("turn after rejected input = %v", err)
	}
	f.input.Deadline = (*f.clock).Add(20 * time.Second)
	f.input.AttemptNumber = 2
	if _, err := f.coordinator.Execute(context.Background(), f.input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("attempt number error = %v", err)
	}
	f.input.AttemptNumber = 1
	f.input.FencingToken = []byte("caller-selected-token")
	if _, err := f.coordinator.Execute(context.Background(), f.input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("fencing error = %v", err)
	}
	f.input.FencingToken = []byte("token-local")
	f.input.Command = LocalExecutionCommand{Kind: LocalProbeCommand, ProbeName: "bad\x00probe"}
	if _, err := f.coordinator.Execute(context.Background(), f.input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("probe text error = %v", err)
	}
	f.input.Command = LocalExecutionCommand{Kind: LocalValidateBindingCommand, Binding: Scope{TenantID: "other", ProjectID: "project-alpha"}}
	if _, err := f.coordinator.Execute(context.Background(), f.input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("binding scope error = %v", err)
	}
}

func TestLocalExecutionMissingReceiptFailureReconcilesConcurrentWinner(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED})
	if _, err := f.store.CreateTurn(context.Background(), CreateTurnInput{Scope: f.input.Scope, SessionID: f.input.SessionID, TurnID: f.input.TurnID, InputText: f.input.InputText, Mutation: f.input.Mutation}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateExecution(context.Background(), CreateExecutionInput{Scope: f.input.Scope, SessionID: f.input.SessionID, TurnID: f.input.TurnID, ExecutionID: f.input.ExecutionID, Generation: f.input.Generation, Mutation: f.input.Mutation}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.StartExecution(context.Background(), StartExecutionInput{Scope: f.input.Scope, SessionID: f.input.SessionID, TurnID: f.input.TurnID, ExecutionID: f.input.ExecutionID, Generation: f.input.Generation, Mutation: f.input.Mutation}); err != nil {
		t.Fatal(err)
	}
	winnerMutation := Mutation{RequestID: "winner-request", IdempotencyKey: "winner-operation"}
	if _, err := f.store.FailExecution(context.Background(), FailExecutionInput{Scope: f.input.Scope, SessionID: f.input.SessionID, TurnID: f.input.TurnID, ExecutionID: f.input.ExecutionID, Generation: f.input.Generation, ErrorCode: "worker_failed", Mutation: winnerMutation}); err != nil {
		t.Fatal(err)
	}
	transition, err := f.coordinator.failDetached(f.input, "receipt_missing")
	if err != nil {
		t.Fatal(err)
	}
	if transition.Turn.State != TurnFailed || transition.Execution.State != ExecutionFailed || transition.Execution.ErrorCode != "worker_failed" {
		t.Fatalf("reconciled transition = %#v", transition)
	}
}

func TestLocalExecutionMapsCancellationToCancelledLifecycle(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED})
	f.executor.mu.Lock()
	f.executor.wait = make(chan struct{})
	f.executor.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan LocalExecutionResult, 1)
	errCh := make(chan error, 1)
	go func() { result, err := f.coordinator.Execute(ctx, f.input); resultCh <- result; errCh <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	result := <-resultCh
	err := <-errCh
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if result.Transition.Turn.State != TurnCancelled || result.Transition.Execution.State != ExecutionCancelled {
		t.Fatalf("cancel transition = %#v", result.Transition)
	}
}

func TestLocalExecutionMapsWorkerFailure(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_FAILED, StableErrorCode: "provider_failed", RedactedSummary: "local failure"})
	result, err := f.coordinator.Execute(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition.Turn.State != TurnFailed || result.Transition.Execution.State != ExecutionFailed || result.Transition.Execution.ErrorCode != "worker_failed" {
		t.Fatalf("failure transition = %#v", result.Transition)
	}
}

func TestLocalExecutionMapsWorkerDeadline(t *testing.T) {
	f := newLocalExecutionFixture(t, workerkernel.OperationExecutionResult{Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_DEADLINE_EXCEEDED, RedactedSummary: "deadline"})
	result, err := f.coordinator.Execute(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Transition.Turn.State != TurnFailed || result.Transition.Execution.ErrorCode != "deadline_exceeded" {
		t.Fatalf("deadline transition = %#v", result.Transition)
	}
}

func TestLocalExecutionScopesWorkerNamespaceByTenantAndProject(t *testing.T) {
	ref, err := workerScopeFromLifecycle(Scope{TenantID: "tenant-a", ProjectID: "project-a"})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "scope-5049bb62f3c9870331f77f21ae2defdfec5fe62c14005b2dacf5dc31762ada03" || ref.Namespace != "cloud-agents" || ref.Kind != "project" {
		t.Fatalf("scope ref = %#v", ref)
	}
	left, err := workerScopeFromLifecycle(Scope{TenantID: "tenant~a", ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := workerScopeFromLifecycle(Scope{TenantID: "tenant", ProjectID: "a~project"})
	if err != nil {
		t.Fatal(err)
	}
	if left.ID == right.ID {
		t.Fatalf("length-prefixed scope projection collided: %q", left.ID)
	}
	if _, err := workerScopeFromLifecycle(Scope{TenantID: string(make([]byte, 129)), ProjectID: "project"}); err == nil {
		t.Fatal("overlong worker scope accepted")
	}
}

func TestLocalExecutionStableErrorCodeIsClosed(t *testing.T) {
	for _, code := range []string{"execution_failed", "worker_dispatch_failed", "deadline_exceeded", "fenced", "cancelled", "worker_failed"} {
		if got := localStableErrorCode(code); got != code {
			t.Fatalf("stable code %q mapped to %q", code, got)
		}
	}
	if got := localStableErrorCode("receipt_missing"); got != "worker_failed" {
		t.Fatalf("undeclared code mapped to %q", got)
	}
}

func TestLocalExecutionReceiptDigestExcludesVolatileFields(t *testing.T) {
	base := &workerv1alpha1.DurableReceipt{
		ReceiptId: "receipt-a", OperationId: "operation-a", AttemptId: "attempt-a", IdempotencyKey: "idempotency-a", Sequence: 1,
		ObservedAt: timestamppb.New(time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)), Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED,
		RedactedSummary: "done", Fencing: &workerv1alpha1.FencingStamp{LeaseId: "lease-a", Generation: 7, TokenSha256: []byte("token-digest-a")},
	}
	volatile := proto.Clone(base).(*workerv1alpha1.DurableReceipt)
	volatile.ReceiptId = "receipt-b"
	volatile.Sequence = 99
	volatile.ObservedAt = timestamppb.New(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	volatile.Fencing.TokenSha256 = []byte("token-digest-b")
	if receiptDigest(base) != receiptDigest(volatile) {
		t.Fatal("volatile receipt fields changed result digest")
	}
	changed := proto.Clone(base).(*workerv1alpha1.DurableReceipt)
	changed.RedactedSummary = "different"
	if receiptDigest(base) == receiptDigest(changed) {
		t.Fatal("semantic receipt field did not change result digest")
	}
}
