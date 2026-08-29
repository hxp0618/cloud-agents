package managedagent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

type durableRuntimeExecutionStoreFake struct {
	calls      []string
	principals []*authn.VerifiedPrincipal
	execution  ExecutionSnapshot
	cancel     context.CancelFunc
}

func (fake *durableRuntimeExecutionStoreFake) GetManagedAgentSessionForExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string) (SessionSnapshot, error) {
	fake.calls = append(fake.calls, "session")
	fake.recordPrincipal(principal)
	return SessionSnapshot{ProviderKind: "codex"}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentTurn(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CreateTurnInput) (TurnSnapshot, error) {
	fake.calls = append(fake.calls, "turn")
	fake.recordPrincipal(principal)
	return TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: TurnQueued}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CreateManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CreateExecutionInput) (ExecutionSnapshot, error) {
	fake.calls = append(fake.calls, "execution")
	fake.recordPrincipal(principal)
	fake.execution.Scope, fake.execution.SessionID, fake.execution.TurnID, fake.execution.ExecutionID = input.Scope, input.SessionID, input.TurnID, input.ExecutionID
	if fake.execution.State == "" {
		fake.execution.State = ExecutionSucceeded
	}
	return fake.execution, nil
}

func (fake *durableRuntimeExecutionStoreFake) StartManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ StartExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "start")
	fake.recordPrincipal(principal)
	if fake.cancel != nil {
		fake.cancel()
	}
	fake.execution.State = ExecutionRunning
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnRunning}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CompleteManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ CompleteExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "complete")
	fake.recordPrincipal(principal)
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FailManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ FailExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "fail")
	fake.recordPrincipal(principal)
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) InterruptManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input InterruptTurnInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "interrupt")
	fake.recordPrincipal(principal)
	fake.execution.State = ExecutionCancelled
	fake.execution.ErrorCode = "interrupted"
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnInterrupted}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) CancelManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input CancelTurnInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "cancel")
	fake.recordPrincipal(principal)
	fake.execution.State = ExecutionCancelled
	return ExecutionTransitionResult{Turn: TurnSnapshot{State: TurnCancelled}, Execution: fake.execution}, nil
}

func (fake *durableRuntimeExecutionStoreFake) recordPrincipal(principal *authn.VerifiedPrincipal) {
	if principal != nil {
		fake.principals = append(fake.principals, principal)
	}
}

var _ DurableRuntimeExecutionStore = (*durableRuntimeExecutionStoreFake)(nil)

func testVerifiedPrincipalSource() VerifiedPrincipalSource {
	return func() (*authn.VerifiedPrincipal, error) { return &authn.VerifiedPrincipal{}, nil }
}

func assertFreshPrincipals(t *testing.T, principals []*authn.VerifiedPrincipal, want int) {
	t.Helper()
	if len(principals) != want {
		t.Fatalf("principal count = %d, want %d", len(principals), want)
	}
	seen := make(map[*authn.VerifiedPrincipal]struct{}, len(principals))
	for _, principal := range principals {
		if _, exists := seen[principal]; exists {
			t.Fatal("coordinator reused a one-shot principal")
		}
		seen[principal] = struct{}{}
	}
}

func TestBoundedRuntimeIdentifierUsesPublicLimit(t *testing.T) {
	base := strings.Repeat("a", maxPublicExecutionMessageIdentifierBytes)
	got := boundedRuntimeIdentifier(base, "start")
	want := strings.Repeat("a", maxPublicExecutionMessageIdentifierBytes-len("-start")) + "-start"
	if got != want || len(got) != maxPublicExecutionMessageIdentifierBytes {
		t.Fatalf("bounded runtime identifier = %q, want %q", got, want)
	}
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
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "turn", "execution"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 3)
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
	result, err := coordinator.Execute(ctx, testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err == nil || !errors.Is(err, context.Canceled) || result.Transition.Execution.State != ExecutionCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "turn", "execution", "start", "cancel"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 5)
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
	unregister, err := coordinator.registerActiveExecution(key, activeCancel)
	if err != nil {
		t.Fatal(err)
	}
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

func TestDurableRuntimeExecutionRejectsDuplicateActiveExecution(t *testing.T) {
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: &durableRuntimeExecutionStoreFake{}, Supervisor: &supervisor.Supervisor{}, Clock: time.Now,
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	first, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	if _, err := coordinator.registerActiveExecution(key, func() {}); !errors.Is(err, ErrDurableRuntimeExecutionConflict) {
		t.Fatalf("duplicate active execution error = %v", err)
	}
}
