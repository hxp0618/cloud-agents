package controlplane_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	controlplane "github.com/hxp0618/cloud-agents/services/control-plane"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

func TestManagedAgentServiceExposesInMemoryLifecycle(t *testing.T) {
	service, err := controlplane.NewManagedAgentService()
	if err != nil {
		t.Fatal(err)
	}
	scope := controlplane.Scope{TenantID: "tenant-public", ProjectID: "project-public"}
	ctx := context.Background()

	session, err := service.CreateSession(ctx, controlplane.CreateSessionInput{
		Scope: scope, SessionID: "session-1", ProviderKind: "codex",
		Mutation: controlplane.Mutation{RequestID: "request-session", IdempotencyKey: "idem-session"},
	})
	if err != nil || session.State != controlplane.SessionActive {
		t.Fatalf("session = %#v, err = %v", session, err)
	}
	turn, err := service.CreateTurn(ctx, controlplane.CreateTurnInput{
		Scope: scope, SessionID: session.SessionID, TurnID: "turn-1", InputText: "hello",
		Mutation: controlplane.Mutation{RequestID: "request-turn", IdempotencyKey: "idem-turn"},
	})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := service.CreateExecution(ctx, controlplane.CreateExecutionInput{
		Scope: scope, SessionID: session.SessionID, TurnID: turn.TurnID, ExecutionID: "execution-1", Generation: 1,
		Mutation: controlplane.Mutation{RequestID: "request-execution", IdempotencyKey: "idem-execution"},
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartExecution(ctx, controlplane.StartExecutionInput{
		Scope: scope, SessionID: session.SessionID, TurnID: turn.TurnID, ExecutionID: execution.ExecutionID, Generation: 1,
		Mutation: controlplane.Mutation{RequestID: "request-start", IdempotencyKey: "idem-start"},
	})
	if err != nil || started.Execution.State != controlplane.ExecutionRunning {
		t.Fatalf("started = %#v, err = %v", started, err)
	}
	completed, err := service.CompleteExecution(ctx, controlplane.CompleteExecutionInput{
		Scope: scope, SessionID: session.SessionID, TurnID: turn.TurnID, ExecutionID: execution.ExecutionID, Generation: 1,
		ResultDigest: "sha256:" + strings.Repeat("a", 64),
		Mutation:     controlplane.Mutation{RequestID: "request-complete", IdempotencyKey: "idem-complete"},
	})
	if err != nil || completed.Turn.State != controlplane.TurnCompleted {
		t.Fatalf("completed = %#v, err = %v", completed, err)
	}
	page, err := service.ReadEvents(ctx, scope, controlplane.EventCursor{}, 8)
	if err != nil || len(page.Events) != 5 || page.Events[0].Scope != scope {
		t.Fatalf("events = %#v, err = %v", page, err)
	}
	if _, err := service.GetSession(ctx, controlplane.Scope{TenantID: "other-tenant", ProjectID: "project-public"}, session.SessionID); !errors.Is(err, controlplane.ErrNotFound) {
		t.Fatalf("cross-tenant read err = %v", err)
	}
}

func TestManagedAgentServiceNilReceiverFailsClosed(t *testing.T) {
	var service *controlplane.ManagedAgentService
	if _, err := service.GetSession(context.Background(), controlplane.Scope{TenantID: "tenant", ProjectID: "project"}, "session"); !errors.Is(err, controlplane.ErrInvalidInput) {
		t.Fatalf("nil receiver err = %v", err)
	}
	if _, err := service.NewLocalExecutionCoordinator(controlplane.LocalExecutionConfig{}); !errors.Is(err, controlplane.ErrInvalidInput) {
		t.Fatalf("nil service coordinator err = %v", err)
	}
	var coordinator *controlplane.LocalExecutionCoordinator
	if _, err := coordinator.Execute(context.Background(), controlplane.LocalExecutionInput{}); !errors.Is(err, controlplane.ErrInvalidInput) {
		t.Fatalf("nil coordinator err = %v", err)
	}
}

func TestManagedAgentServiceExposesLocalWorkerExecution(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/public", TrustDomain: "cloud-agents.test", LeafCertificateSha256: []byte("01234567890123456789012345678901")}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor/public", TrustDomain: "cloud-agents.test", LeafCertificateSha256: []byte("12345678901234567890123456789012")}
	worker, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		AdmissionLeaseID: "public-lease", AdmissionGeneration: 7,
		IdentityProvider: workerkernel.StaticIdentityProvider{Identity: supervisorIdentity},
		Clock:            func() time.Time { return now },
		Executor:         workerkernel.DeterministicLocalExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	workerSupervisor, err := supervisor.NewLocal(supervisor.LocalConfig{
		Handle:                 worker.LocalDispatchHandle(),
		ExpectedWorkerIdentity: workerIdentity,
		Clock:                  func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workerSupervisor.BindLocalDispatch(context.Background()); err != nil {
		t.Fatal(err)
	}

	service, err := controlplane.NewManagedAgentService()
	if err != nil {
		t.Fatal(err)
	}
	scope := controlplane.Scope{TenantID: "tenant-public-execution", ProjectID: "project-public-execution"}
	if _, err := service.CreateSession(context.Background(), controlplane.CreateSessionInput{
		Scope: scope, SessionID: "session-public", ProviderKind: "localdev",
		Mutation: controlplane.Mutation{RequestID: "session-request", IdempotencyKey: "session-key"},
	}); err != nil {
		t.Fatal(err)
	}
	coordinator, err := service.NewLocalExecutionCoordinator(controlplane.LocalExecutionConfig{
		Supervisor: workerSupervisor, Clock: func() time.Time { return now },
		FencingLeaseID: "public-lease", FencingGeneration: 7, FencingToken: []byte("public-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	input := controlplane.LocalExecutionInput{
		Scope: scope, SessionID: "session-public", TurnID: "turn-public", ExecutionID: "execution-public",
		OperationID: "operation-public", AttemptID: "attempt-public", AttemptNumber: 1,
		InputText: "hello", Generation: 7, FencingLeaseID: "public-lease", FencingGeneration: 7,
		FencingToken: []byte("public-token"), Deadline: now.Add(20 * time.Second),
		Mutation: controlplane.Mutation{RequestID: "execution-request", IdempotencyKey: "execution-key"},
		Command:  controlplane.LocalExecutionCommand{Kind: controlplane.LocalProbeCommand, ProbeName: "public-probe"},
	}
	result, err := coordinator.Execute(context.Background(), input)
	if err != nil || result.Receipt == nil || result.Transition.Turn.State != controlplane.TurnCompleted || result.Transition.Execution.State != controlplane.ExecutionSucceeded {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	replay, err := coordinator.Execute(context.Background(), input)
	if err != nil || replay.Transition != result.Transition || replay.Receipt == nil || replay.Receipt.GetReceiptId() != result.Receipt.GetReceiptId() {
		t.Fatalf("replay = %#v, err = %v", replay, err)
	}
	page, err := service.ReadEvents(context.Background(), scope, controlplane.EventCursor{}, 8)
	if err != nil || len(page.Events) != 5 {
		t.Fatalf("replay events = %#v, err = %v", page, err)
	}
}
