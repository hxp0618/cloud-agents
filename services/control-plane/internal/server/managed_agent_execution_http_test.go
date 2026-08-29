package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/worker/runtime"
)

type managedAgentExecutionStoreFake struct {
	execution internalmanagedagent.ExecutionSnapshot
	gotTurn   string
	gotExec   string
	cancel    internalmanagedagent.CancelTurnInput
}

func (fake *managedAgentExecutionStoreFake) GetManagedAgentSessionForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedagent.SessionSnapshot, error) {
	return internalmanagedagent.SessionSnapshot{}, nil
}

func (fake *managedAgentExecutionStoreFake) CreateManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CreateTurnInput) (internalmanagedagent.TurnSnapshot, error) {
	return internalmanagedagent.TurnSnapshot{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) CreateManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CreateExecutionInput) (internalmanagedagent.ExecutionSnapshot, error) {
	return internalmanagedagent.ExecutionSnapshot{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) StartManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.StartExecutionInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	return internalmanagedagent.ExecutionTransitionResult{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CompleteExecutionInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	return internalmanagedagent.ExecutionTransitionResult{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.FailExecutionInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	return internalmanagedagent.ExecutionTransitionResult{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) CancelManagedAgentExecution(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedagent.CancelTurnInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	fake.cancel = input
	fake.execution.State = internalmanagedagent.ExecutionCancelled
	fake.execution.ErrorCode = "cancelled"
	fake.execution.Version++
	return internalmanagedagent.ExecutionTransitionResult{Turn: internalmanagedagent.TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: internalmanagedagent.TurnCancelled}, Execution: fake.execution}, nil
}

func (fake *managedAgentExecutionStoreFake) GetManagedAgentExecution(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _, turnID, executionID string) (internalmanagedagent.ExecutionSnapshot, error) {
	fake.gotTurn, fake.gotExec = turnID, executionID
	return fake.execution, nil
}

type managedAgentExecutionRunnerFake struct {
	input internalmanagedagent.DurableRuntimeExecutionInput
}

func (fake *managedAgentExecutionRunnerFake) Execute(_ context.Context, _ *authn.VerifiedPrincipal, input internalmanagedagent.DurableRuntimeExecutionInput) (internalmanagedagent.DurableRuntimeExecutionResult, error) {
	fake.input = input
	return internalmanagedagent.DurableRuntimeExecutionResult{Transition: internalmanagedagent.ExecutionTransitionResult{
		Execution: internalmanagedagent.ExecutionSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: 7, State: internalmanagedagent.ExecutionSucceeded, Version: 2, CreatedAt: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC)},
	}, Messages: []runtime.Message{{MessageType: "Result", CommandID: "turn", ExecutionID: input.ExecutionID, Generation: 7}}}, nil
}

func TestManagedAgentExecutionHTTPServerExecutesAndReadsByTurn(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentExecutionStoreFake{execution: internalmanagedagent.ExecutionSnapshot{Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 7, State: internalmanagedagent.ExecutionSucceeded, Version: 2, CreatedAt: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC)}}
	runner := &managedAgentExecutionRunnerFake{}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions", strings.NewReader(`{"turnId":"turn-alpha","executionId":"execution-alpha","model":"gpt-test","inputText":"hello"}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("execute status=%d verification=%#v body=%s", response.Code, verifier.seen, response.Body.String())
	}
	if runner.input.TurnID != "turn-alpha" || runner.input.ExecutionID != "execution-alpha" || runner.input.Model != "gpt-test" || runner.input.Mutation.IdempotencyKey == "" || !strings.Contains(response.Body.String(), `"kind":"Execution"`) {
		t.Fatalf("execute input=%#v body=%s", runner.input, response.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha", nil)
	get.Header.Set("Authorization", "Bearer access-token")
	get.Header.Set("X-Request-ID", "request-get")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.get" || store.gotTurn != "turn-alpha" || store.gotExec != "execution-alpha" {
		t.Fatalf("get status=%d verification=%#v turn=%q execution=%q body=%s", got.Code, verifier.seen, store.gotTurn, store.gotExec, got.Body.String())
	}

	cancel := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel", strings.NewReader(`{"generation":7}`))
	cancel.Header.Set("Authorization", "Bearer access-token")
	cancel.Header.Set("X-Request-ID", "request-cancel")
	cancel.Header.Set("Idempotency-Key", "idem-cancel-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancel)
	if cancelled.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.act" || store.cancel.Generation != 7 || store.cancel.TargetExecutionID != "execution-alpha" || !strings.Contains(cancelled.Body.String(), `"state":"cancelled"`) {
		t.Fatalf("cancel status=%d verification=%#v input=%#v body=%s", cancelled.Code, verifier.seen, store.cancel, cancelled.Body.String())
	}
}

func TestManagedAgentExecutionPathRejectsCrossTurnLookup(t *testing.T) {
	if HandlesManagedAgentExecutionPath("/v1/tenants/t/projects/p/sessions/s/turns/t:bad/executions/e") {
		t.Fatal("accepted a colon-bearing turn id")
	}
	if !HandlesManagedAgentExecutionPath("/v1/tenants/t/projects/p/sessions/s/turns/turn/executions/execution:cancel") {
		t.Fatal("did not accept execution cancel path")
	}
}
