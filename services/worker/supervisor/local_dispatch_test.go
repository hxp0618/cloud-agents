package supervisor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type localDispatchExecutor struct {
	mu        sync.Mutex
	calls     int
	onExecute func(context.Context)
	result    workerkernel.OperationExecutionResult
}

func (e *localDispatchExecutor) Execute(ctx context.Context, _ workerkernel.OperationExecutionInput) (workerkernel.OperationExecutionResult, error) {
	e.mu.Lock()
	e.calls++
	hook := e.onExecute
	result := e.result
	e.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	return result, nil
}

func (e *localDispatchExecutor) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

type localDispatchFixture struct {
	now            time.Time
	clock          *time.Time
	workerIdentity *workerv1alpha1.WorkloadIdentity
	supervisorID   *workerv1alpha1.WorkloadIdentity
	service        *workerkernel.Service
	supervisor     *Supervisor
	attempt        *workerv1alpha1.OperationAttemptEnvelope
	binding        BindingSnapshot
	token          []byte
	executor       *localDispatchExecutor
}

func newLocalDispatchFixture(t *testing.T, withExecutor bool) localDispatchFixture {
	t.Helper()
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	clock := &now
	workerIdentity := &workerv1alpha1.WorkloadIdentity{
		SpiffeId:              "spiffe://cloud-agents.test/worker/local-dispatch",
		TrustDomain:           "cloud-agents.test",
		LeafCertificateSha256: []byte("01234567890123456789012345678901"),
	}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{
		SpiffeId:              "spiffe://cloud-agents.test/supervisor/local-dispatch",
		TrustDomain:           "cloud-agents.test",
		LeafCertificateSha256: []byte("12345678901234567890123456789012"),
	}
	executor := &localDispatchExecutor{result: workerkernel.OperationExecutionResult{
		Outcome:         workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED,
		RedactedSummary: "local probe completed",
	}}
	var id int
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		AdmissionLeaseID:    "lease-local",
		AdmissionGeneration: 7,
		IdentityProvider:    workerkernel.StaticIdentityProvider{Identity: supervisorIdentity},
		NegotiationTTL:      time.Minute,
		IDGenerator: func() (string, error) {
			id++
			return fmt.Sprintf("local-id-%d", id), nil
		},
		Clock: func() time.Time { return *clock },
		Executor: func() workerkernel.OperationExecutor {
			if withExecutor {
				return executor
			}
			return nil
		}(),
	})
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewLocal(LocalConfig{
		Handle:                 service.LocalDispatchHandle(),
		ExpectedWorkerIdentity: workerIdentity,
		Clock:                  func() time.Time { return *clock },
	})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := supervisor.BindLocalDispatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(30 * time.Second)
	token := []byte("local-dispatch-fencing-token")
	op := &workerv1alpha1.OperationEnvelope{
		OperationId:        "operation-local-001",
		IdempotencyKey:     "idempotency-local-001",
		Scope:              &workerv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", Id: "project-local"},
		Fencing:            &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: append([]byte(nil), token...)},
		Deadline:           timestamppb.New(deadline),
		RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		Command:            &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "contract-only"}}},
	}
	canonical := fmt.Sprintf(
		`{"command":{"probe":{"probeName":"contract-only"}},"deadline":%q,"fencing":{"generation":"7","leaseId":"lease-local"},"finalizers":[],"idempotencyKey":"idempotency-local-001","operationId":"operation-local-001","requiredCapability":"CAPABILITY_OPERATION_DISPATCH","scope":{"id":"project-local","kind":"project","namespace":"cloud-agents"}}`,
		deadline.Format(time.RFC3339Nano),
	)
	digest := sha256.Sum256([]byte(canonical))
	op.CanonicalRequestSha256 = append([]byte(nil), digest[:]...)
	attempt := &workerv1alpha1.OperationAttemptEnvelope{
		Operation:                op,
		AttemptId:                "attempt-local-001",
		AttemptNumber:            1,
		ExpectedExecutorIdentity: proto.Clone(workerIdentity).(*workerv1alpha1.WorkloadIdentity),
		Negotiation:              binding.Negotiation(),
	}
	return localDispatchFixture{
		now: now, clock: clock, workerIdentity: workerIdentity, supervisorID: supervisorIdentity,
		service: service, supervisor: supervisor, attempt: attempt, binding: binding,
		token: token, executor: executor,
	}
}

func TestGeneratedLocalDispatchProfileAndOpaqueHandle(t *testing.T) {
	if !WorkerSupervisorLocalDispatchProfile().Valid() || !GeneratedLocalDispatchProfile.Valid() {
		t.Fatalf("generated local dispatch profile is invalid: %#v", GeneratedLocalDispatchProfile)
	}
	fixture := newLocalDispatchFixture(t, false)
	if !fixture.service.LocalDispatchHandle().Valid() {
		t.Fatal("service did not mint a valid local handle")
	}
	var zero workerkernel.LocalDispatchHandle
	if zero.Valid() {
		t.Fatal("zero local handle was valid")
	}
	if _, err := NewLocal(LocalConfig{Handle: zero, ExpectedWorkerIdentity: fixture.workerIdentity}); !errors.Is(err, errInvalidConfig) {
		t.Fatalf("zero handle constructor error = %v", err)
	}
	if _, err := zero.ExecuteOperation(context.Background(), connect.NewRequest(fixture.attempt)); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("zero handle execute error = %v", err)
	}
}

func TestGenericSupervisorCannotDispatchThroughLocalHandle(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	// The opaque handle implements the generated client, but passing it through
	// generic New must not set the private local marker.
	supervisor, err := New(Config{Client: fixture.service.LocalDispatchHandle(), ExpectedWorkerIdentity: fixture.workerIdentity, Clock: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.DispatchOperation(context.Background(), fixture.attempt); connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "operation_dispatch_not_implemented") {
		t.Fatalf("generic dispatch error = %v", err)
	}
	if _, err := supervisor.GetOperationReceipt(context.Background(), &workerv1alpha1.ReceiptRequest{}); connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "durable_receipts_not_implemented") {
		t.Fatalf("generic receipt error = %v", err)
	}
}

func TestLocalDispatchRequiresBindingAndExecutor(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	fixture.supervisor.mu.Lock()
	fixture.supervisor.binding = nil
	fixture.supervisor.mu.Unlock()
	_, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "binding_required") {
		t.Fatalf("missing binding error = %v", err)
	}

	noExecutor := newLocalDispatchFixture(t, false)
	_, err = noExecutor.supervisor.DispatchOperation(context.Background(), noExecutor.attempt)
	if connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "execute_operation_rpc_failed") {
		t.Fatalf("nil executor error = %v", err)
	}
}

func TestLocalDispatchRequiresExactBindingAndRejectsNegatives(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	if _, err := fixture.supervisor.DispatchOperation(context.Background(), nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("nil request error = %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*workerv1alpha1.OperationAttemptEnvelope)
		code   connect.Code
		stable string
	}{
		{"foreign negotiation", func(a *workerv1alpha1.OperationAttemptEnvelope) { a.Negotiation.NegotiationId = "foreign" }, connect.CodeFailedPrecondition, "negotiation_binding_mismatch"},
		{"wrong executor", func(a *workerv1alpha1.OperationAttemptEnvelope) {
			a.ExpectedExecutorIdentity.SpiffeId = "spiffe://wrong"
		}, connect.CodePermissionDenied, "executor_identity_mismatch"},
		{"wrong capability", func(a *workerv1alpha1.OperationAttemptEnvelope) {
			a.Operation.RequiredCapability = workerv1alpha1.Capability_CAPABILITY_HEALTH
		}, connect.CodeFailedPrecondition, "required_capability_invalid"},
		{"nested unknown", func(a *workerv1alpha1.OperationAttemptEnvelope) {
			a.Operation.Command.GetProbe().ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		}, connect.CodeInvalidArgument, "unknown_fields"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attempt := proto.Clone(fixture.attempt).(*workerv1alpha1.OperationAttemptEnvelope)
			tc.mutate(attempt)
			_, err := fixture.supervisor.DispatchOperation(context.Background(), attempt)
			if connect.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.stable) {
				t.Fatalf("error=%v code=%v", err, connect.CodeOf(err))
			}
			if fixture.executor.count() != 0 {
				t.Fatalf("negative request invoked executor: %d", fixture.executor.count())
			}
		})
	}
}

func TestLocalDispatchAndDetachedReceiptReplay(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	response, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
	if err != nil {
		t.Fatal(err)
	}
	if response == nil || response.Msg.GetOutcome() != workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED {
		t.Fatalf("dispatch response = %#v", response)
	}
	receiptID := response.Msg.GetReceiptId()
	if receiptID == "" || fixture.executor.count() != 1 {
		t.Fatalf("receipt=%q executor calls=%d", receiptID, fixture.executor.count())
	}
	// Responses are detached from the worker's in-memory record.
	response.Msg.ReceiptId = "caller-mutated"
	request := &workerv1alpha1.ReceiptRequest{
		OperationId:            fixture.attempt.GetOperation().GetOperationId(),
		ReceiptId:              receiptID,
		Fencing:                &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: append([]byte(nil), fixture.token...)},
		ExpectedServerIdentity: proto.Clone(fixture.workerIdentity).(*workerv1alpha1.WorkloadIdentity),
		Negotiation:            fixture.binding.Negotiation(),
		RequiredCapability:     workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
	}
	detached, err := fixture.supervisor.GetOperationReceipt(context.Background(), request)
	if err != nil || detached.Msg.GetReceiptId() != receiptID {
		t.Fatalf("detached receipt=%v err=%v", detached, err)
	}
	replay, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
	if err != nil || replay.Msg.GetReceiptId() != receiptID || fixture.executor.count() != 1 {
		t.Fatalf("replay=%v err=%v executor calls=%d", replay, err, fixture.executor.count())
	}
}

func TestLocalDispatchParallelReplayInvokesExecutorOnce(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	var wg sync.WaitGroup
	responses := make([]*workerv1alpha1.DurableReceipt, 2)
	errs := make([]error, 2)
	for i := range responses {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			response, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
			if response != nil {
				responses[index] = response.Msg
			}
			errs[index] = err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || responses[i] == nil {
			t.Fatalf("parallel response[%d]=%v err=%v", i, responses[i], err)
		}
	}
	if fixture.executor.count() != 1 || responses[0].GetReceiptId() != responses[1].GetReceiptId() {
		t.Fatalf("executor calls=%d receipts=%q/%q", fixture.executor.count(), responses[0].GetReceiptId(), responses[1].GetReceiptId())
	}
}

func TestLocalReceiptOwnershipAndFencing(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	response, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
	if err != nil {
		t.Fatal(err)
	}
	base := &workerv1alpha1.ReceiptRequest{
		OperationId:            fixture.attempt.GetOperation().GetOperationId(),
		ReceiptId:              response.Msg.GetReceiptId(),
		Fencing:                &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: append([]byte(nil), fixture.token...)},
		ExpectedServerIdentity: proto.Clone(fixture.workerIdentity).(*workerv1alpha1.WorkloadIdentity),
		Negotiation:            fixture.binding.Negotiation(),
		RequiredCapability:     workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
	}
	wrongFence := proto.Clone(base).(*workerv1alpha1.ReceiptRequest)
	wrongFence.Fencing.Token = []byte("wrong-token")
	if _, err := fixture.supervisor.GetOperationReceipt(context.Background(), wrongFence); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("wrong fence error = %v", err)
	}
	missing := proto.Clone(base).(*workerv1alpha1.ReceiptRequest)
	missing.ReceiptId = "missing-receipt"
	if _, err := fixture.supervisor.GetOperationReceipt(context.Background(), missing); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("missing receipt error = %v", err)
	}
}

func TestLocalDispatchFailsClosedOnExpiryAndCancellation(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	// The supervisor clock is captured by closure; replace the binding with an
	// expired view by advancing the service/supervisor clock through a mutable
	// clock in a purpose-built second fixture.
	clock := fixture.now
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: fixture.workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		AdmissionLeaseID: "lease-local", AdmissionGeneration: 7,
		IdentityProvider: workerkernel.StaticIdentityProvider{Identity: fixture.supervisorID},
		NegotiationTTL:   time.Second,
		IDGenerator:      func() (string, error) { return "expiry-id", nil },
		Clock:            func() time.Time { return clock }, Executor: fixture.executor,
	})
	if err != nil {
		t.Fatal(err)
	}
	expiring, err := NewLocal(LocalConfig{Handle: service.LocalDispatchHandle(), ExpectedWorkerIdentity: fixture.workerIdentity, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expiring.BindLocalDispatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	_, err = expiring.DispatchOperation(context.Background(), fixture.attempt)
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "binding_expired") {
		t.Fatalf("expired dispatch error = %v", err)
	}

	cancelFixture := newLocalDispatchFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancelFixture.executor.onExecute = func(context.Context) { cancel() }
	_, err = cancelFixture.supervisor.DispatchOperation(ctx, cancelFixture.attempt)
	if connect.CodeOf(err) != connect.CodeCanceled || cancelFixture.executor.count() != 1 {
		t.Fatalf("cancelled dispatch error=%v calls=%d", err, cancelFixture.executor.count())
	}

	postExpiry := newLocalDispatchFixture(t, true)
	postExpiry.executor.onExecute = func(context.Context) {
		*postExpiry.clock = postExpiry.binding.ExpiresAt.Add(time.Second)
	}
	_, err = postExpiry.supervisor.DispatchOperation(context.Background(), postExpiry.attempt)
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "binding_expired") || postExpiry.executor.count() != 1 {
		t.Fatalf("post-RPC expiry error=%v calls=%d", err, postExpiry.executor.count())
	}
}

func TestLocalDispatchRejectsOperationBindingProfile(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	// Re-negotiate the older admission-only profile. Dispatch must not treat it
	// as the new versioned local dispatch authority.
	if _, err := fixture.supervisor.BindOperationAdmission(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.supervisor.DispatchOperation(context.Background(), fixture.attempt)
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "operation_binding_required") {
		t.Fatalf("admission-only dispatch error = %v", err)
	}
}

func TestLocalDispatchReceiptResponseValidation(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	digest := sha256.Sum256(fixture.token)
	base := &workerv1alpha1.DurableReceipt{
		ReceiptId: "receipt-1", OperationId: "operation-local-001", AttemptId: "attempt-local-001", IdempotencyKey: "idempotency-local-001", Sequence: 1,
		Fencing: &workerv1alpha1.FencingStamp{LeaseId: "lease-local", Generation: 7, TokenSha256: digest[:]},
		Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, ObservedAt: timestamppb.New(fixture.now),
	}
	if err := validateLocalReceiptResponse(connect.NewResponse(base), base.OperationId, "", &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: fixture.token}, base.AttemptId, base.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	bad := proto.Clone(base).(*workerv1alpha1.DurableReceipt)
	bad.Fencing.Generation = 8
	if err := validateLocalReceiptResponse(connect.NewResponse(bad), bad.OperationId, "", &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: fixture.token}, bad.AttemptId, bad.IdempotencyKey); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("bad fencing response = %v", err)
	}
	unknown := proto.Clone(base).(*workerv1alpha1.DurableReceipt)
	unknown.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
	if err := validateLocalReceiptResponse(connect.NewResponse(unknown), unknown.OperationId, "", &workerv1alpha1.FencingProof{LeaseId: "lease-local", Generation: 7, Token: fixture.token}, unknown.AttemptId, unknown.IdempotencyKey); connect.CodeOf(err) != connect.CodeInternal {
		t.Fatalf("unknown response = %v", err)
	}
	if got := hex.EncodeToString(digest[:]); got == "" {
		t.Fatal("digest unexpectedly empty")
	}
}
