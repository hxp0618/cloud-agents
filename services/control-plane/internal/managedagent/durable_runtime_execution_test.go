package managedagent

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

type durableRuntimeExecutionStoreFake struct {
	calls     []string
	execution ExecutionSnapshot
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
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CompleteExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "complete")
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, FailExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "fail")
	return ExecutionTransitionResult{}, nil
}

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
