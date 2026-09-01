package controlplane_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	controlplane "github.com/hxp0618/cloud-agents/services/control-plane"
)

func TestManagedAgentServiceExposesInMemoryLifecycle(t *testing.T) {
	service, err := controlplane.NewManagedAgentService()
	if err != nil {
		t.Fatal(err)
	}
	scope := controlplane.Scope{TenantID: "tenant-public", ProjectID: "project-public"}
	ctx := context.Background()

	session, err := service.CreateSession(ctx, controlplane.CreateSessionInput{
		Scope: scope, SessionID: "session-1", ProviderKind: "codex", EnvironmentLeaseID: "environment-1",
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
}
