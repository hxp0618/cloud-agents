package managedagent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

type durableRuntimeExecutionStoreFake struct {
	calls     []string
	execution ExecutionSnapshot
	cancel    context.CancelFunc
}

func (fake *durableRuntimeExecutionStoreFake) GetManagedAgentSessionForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string) (SessionSnapshot, error) {
	fake.calls = append(fake.calls, "session")
	return SessionSnapshot{ProviderKind: "codex"}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentTurn(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input CreateTurnInput) (TurnSnapshot, error) {
	fake.calls = append(fake.calls, "turn")
	return TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: TurnQueued}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentExecution(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input CreateExecutionInput) (ExecutionSnapshot, error) {
	fake.calls = append(fake.calls, "execution")
	fake.execution.Scope, fake.execution.SessionID, fake.execution.TurnID, fake.execution.ExecutionID = input.Scope, input.SessionID, input.TurnID, input.ExecutionID
	if fake.execution.State == "" {
		fake.execution.State = ExecutionSucceeded
	}
	return fake.execution, nil
}

func (fake *durableRuntimeExecutionStoreFake) StartManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, StartExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "start")
	if fake.cancel != nil {
		fake.cancel()
	}
	fake.execution.State = ExecutionRunning
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnRunning}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CompleteExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "complete")
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, FailExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "fail")
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CancelManagedAgentExecution(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input CancelTurnInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "cancel")
	fake.execution.State = ExecutionCancelled
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnCancelled}, Execution: fake.execution}, nil
}

var _ DurableRuntimeExecutionStore = (*durableRuntimeExecutionStoreFake)(nil)

func TestDurableRuntimeExecutionReturnsTerminalReplayWithoutOpeningWorker(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &supervisor.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), nil, DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "turn", "execution"}) {
		t.Fatalf("calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionPersistsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, cancel: cancel}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &supervisor.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(ctx, nil, DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err == nil || !errors.Is(err, context.Canceled) || result.Transition.Execution.State != ExecutionCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "turn", "execution", "start", "cancel"}) {
		t.Fatalf("calls=%v", store.calls)
	}
}

func TestDurableRuntimeExecutionCancelSignalsActiveRuntime(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &supervisor.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	activeContext, activeCancel := context.WithCancel(context.Background())
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	unregister := coordinator.registerActiveExecution(key, activeCancel)
	_, err = coordinator.Cancel(context.Background(), nil, CancelTurnInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", TargetExecutionID: "execution", Generation: 7,
		Mutation: Mutation{RequestID: "cancel-request", IdempotencyKey: "cancel-idem"},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-activeContext.Done():
	case <-time.After(time.Second):
		t.Fatal("active runtime context was not cancelled")
	}
	if !unregister() {
		t.Fatal("active cancellation was not recorded")
	}
}
