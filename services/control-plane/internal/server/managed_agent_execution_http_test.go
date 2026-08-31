package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type managedAgentExecutionStoreFake struct {
	execution internalmanagedagent.ExecutionSnapshot
	gotTurn   string
	gotExec   string
	cancel    internalmanagedagent.CancelTurnInput
	list      int
	page      postgres.ManagedAgentExecutionPage
	after     string
	limit     int
}

func (fake *managedAgentExecutionStoreFake) GetManagedAgentSessionForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedagent.RuntimeSessionSnapshot, error) {
	return internalmanagedagent.RuntimeSessionSnapshot{}, nil
}

func (fake *managedAgentExecutionStoreFake) FindManagedAgentTurnForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string) (internalmanagedagent.TurnSnapshot, bool, error) {
	return internalmanagedagent.TurnSnapshot{}, false, nil
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

func (fake *managedAgentExecutionStoreFake) CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.CompleteRuntimeExecutionInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	return internalmanagedagent.ExecutionTransitionResult{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, internalmanagedagent.FailRuntimeExecutionInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	return internalmanagedagent.ExecutionTransitionResult{}, errors.New("not used")
}

func (fake *managedAgentExecutionStoreFake) InterruptManagedAgentExecution(_ context.Context, _ string, _ *authn.VerifiedPrincipal, input internalmanagedagent.InterruptTurnInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	fake.execution.State = internalmanagedagent.ExecutionCancelled
	fake.execution.ErrorCode = "interrupted"
	return internalmanagedagent.ExecutionTransitionResult{Turn: internalmanagedagent.TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: internalmanagedagent.TurnInterrupted}, Execution: fake.execution}, nil
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

func (fake *managedAgentExecutionStoreFake) ListManagedAgentExecutions(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _, _ string, after string, limit int) (postgres.ManagedAgentExecutionPage, error) {
	fake.list++
	fake.after = after
	fake.limit = limit
	return fake.page, nil
}

type managedAgentExecutionRunnerFake struct {
	input           internalmanagedagent.DurableRuntimeExecutionInput
	sourceReads     int
	interruptInput  internalmanagedagent.InterruptTurnInput
	interruptResult internalmanagedagent.ExecutionTransitionResult
	cancelInput     internalmanagedagent.CancelTurnInput
	cancelResult    internalmanagedagent.ExecutionTransitionResult
	interactions    []runtimeprotocol.Message
	approvalInput   internalmanagedagent.RuntimeApprovalResolutionInput
	userInput       internalmanagedagent.RuntimeUserInputResolutionInput
	resolveErr      error
	artifactInput   internalmanagedagent.RuntimeArtifactReadInput
	artifactResult  internalmanagedagent.RuntimeArtifact
	artifactErr     error
}

func (fake *managedAgentExecutionRunnerFake) Execute(_ context.Context, principalSource internalmanagedagent.VerifiedPrincipalSource, input internalmanagedagent.DurableRuntimeExecutionInput) (internalmanagedagent.DurableRuntimeExecutionResult, error) {
	if principalSource == nil {
		return internalmanagedagent.DurableRuntimeExecutionResult{}, errors.New("missing principal source")
	}
	reads := fake.sourceReads
	if reads == 0 {
		reads = 1
	}
	for range reads {
		if _, err := principalSource(); err != nil {
			return internalmanagedagent.DurableRuntimeExecutionResult{}, err
		}
	}
	fake.input = input
	return internalmanagedagent.DurableRuntimeExecutionResult{Transition: internalmanagedagent.ExecutionTransitionResult{
		Execution: internalmanagedagent.ExecutionSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: 7, State: internalmanagedagent.ExecutionSucceeded, Version: 2, CreatedAt: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC)},
	}, Messages: []runtimeprotocol.Message{{MessageType: "Result", CommandID: "turn", ExecutionID: input.ExecutionID, Generation: 7}}}, nil
}

func (fake *managedAgentExecutionRunnerFake) Interrupt(_ context.Context, _ *authn.VerifiedPrincipal, input internalmanagedagent.InterruptTurnInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	fake.interruptInput = input
	return fake.interruptResult, nil
}

func (fake *managedAgentExecutionRunnerFake) Cancel(_ context.Context, _ *authn.VerifiedPrincipal, input internalmanagedagent.CancelTurnInput) (internalmanagedagent.ExecutionTransitionResult, error) {
	fake.cancelInput = input
	return fake.cancelResult, nil
}

func (fake *managedAgentExecutionRunnerFake) ActiveInteractions(internalmanagedagent.RuntimeExecutionReference) []runtimeprotocol.Message {
	return fake.interactions
}

func (fake *managedAgentExecutionRunnerFake) ResolveApproval(_ context.Context, input internalmanagedagent.RuntimeApprovalResolutionInput) error {
	fake.approvalInput = input
	return fake.resolveErr
}

func (fake *managedAgentExecutionRunnerFake) ResolveUserInput(_ context.Context, input internalmanagedagent.RuntimeUserInputResolutionInput) error {
	fake.userInput = input
	return fake.resolveErr
}

func (fake *managedAgentExecutionRunnerFake) ReadArtifact(_ context.Context, input internalmanagedagent.RuntimeArtifactReadInput) (internalmanagedagent.RuntimeArtifact, error) {
	fake.artifactInput = input
	return fake.artifactResult, fake.artifactErr
}

type managedAgentExecutionVerifierFake struct {
	calls      int
	failOnCall int
}

func (fake *managedAgentExecutionVerifierFake) Verify(_ string, _ authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	fake.calls++
	if fake.failOnCall == fake.calls {
		return nil, errors.New("verification failed")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestManagedAgentExecutionHTTPServerExecutesAndReadsByTurn(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	terminal := runtimeprotocol.Message{RequestID: "request-alpha", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution-alpha", Generation: 7, CommandID: "turn", OccurredAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "persisted"}}
	store := &managedAgentExecutionStoreFake{execution: internalmanagedagent.ExecutionSnapshot{Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 7, State: internalmanagedagent.ExecutionSucceeded, Messages: []runtimeprotocol.Message{terminal}, Version: 2, CreatedAt: time.Date(2026, 8, 29, 8, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 8, 29, 8, 0, 1, 0, time.UTC)}}
	store.page = postgres.ManagedAgentExecutionPage{Executions: []internalmanagedagent.ExecutionSnapshot{store.execution}, NextTurnID: "turn-alpha"}
	cancelledExecution := store.execution
	cancelledExecution.State = internalmanagedagent.ExecutionCancelled
	cancelledExecution.ErrorCode = "cancelled"
	interruptedExecution := cancelledExecution
	interruptedExecution.ErrorCode = "interrupted"
	runner := &managedAgentExecutionRunnerFake{
		cancelResult:    internalmanagedagent.ExecutionTransitionResult{Execution: cancelledExecution},
		interruptResult: internalmanagedagent.ExecutionTransitionResult{Execution: interruptedExecution},
	}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions", strings.NewReader(`{"turnId":"turn-alpha","executionId":"execution-alpha","model":"gpt-test","runtimeMode":"approval-required","interactionMode":"plan","inputText":"hello"}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.act" {
		t.Fatalf("execute status=%d verification=%#v body=%s", response.Code, verifier.seen, response.Body.String())
	}
	if runner.input.TurnID != "turn-alpha" || runner.input.ExecutionID != "execution-alpha" || runner.input.Model != "gpt-test" || runner.input.RuntimeMode != "approval-required" || runner.input.InteractionMode != "plan" || runner.input.Mutation.IdempotencyKey == "" || !strings.Contains(response.Body.String(), `"kind":"Execution"`) {
		t.Fatalf("execute input=%#v body=%s", runner.input, response.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha", nil)
	get.Header.Set("Authorization", "Bearer access-token")
	get.Header.Set("X-Request-ID", "request-get")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.get" || store.gotTurn != "turn-alpha" || store.gotExec != "execution-alpha" || !strings.Contains(got.Body.String(), `"text":"persisted"`) {
		t.Fatalf("get status=%d verification=%#v turn=%q execution=%q body=%s", got.Code, verifier.seen, store.gotTurn, store.gotExec, got.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions?pageSize=1", nil)
	list.Header.Set("Authorization", "Bearer access-token")
	list.Header.Set("X-Request-ID", "request-list")
	listed := httptest.NewRecorder()
	handler.ServeHTTP(listed, list)
	var page managedAgentExecutionPageResource
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || store.list != 1 || store.limit != 1 || store.after != "" || verifier.seen.RequiredPermission != "projects.get" || len(page.Executions) != 1 || page.Executions[0].Messages != nil || page.NextPageToken == "" {
		t.Fatalf("list status=%d calls=%d after=%q limit=%d verification=%#v page=%#v", listed.Code, store.list, store.after, store.limit, verifier.seen, page)
	}
	wrongSessionToken, ok := encodeManagedAgentExecutionPageToken("tenant-alpha", "project-alpha", "session-other", "turn-alpha")
	if !ok {
		t.Fatal("failed to encode wrong-session token")
	}
	wrongSession := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions?pageToken="+wrongSessionToken, nil)
	wrongSession.Header.Set("Authorization", "Bearer access-token")
	wrongSession.Header.Set("X-Request-ID", "request-list-wrong")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, wrongSession)
	if rejected.Code != http.StatusBadRequest || store.list != 1 {
		t.Fatalf("wrong-session token status=%d calls=%d body=%s", rejected.Code, store.list, rejected.Body.String())
	}

	cancel := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel", strings.NewReader(`{"generation":7}`))
	cancel.Header.Set("Authorization", "Bearer access-token")
	cancel.Header.Set("X-Request-ID", "request-cancel")
	cancel.Header.Set("Idempotency-Key", "idem-cancel-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	cancelled := httptest.NewRecorder()
	handler.ServeHTTP(cancelled, cancel)
	if cancelled.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.act" || runner.cancelInput.Generation != 7 || runner.cancelInput.TargetExecutionID != "execution-alpha" || !strings.Contains(cancelled.Body.String(), `"state":"cancelled"`) {
		t.Fatalf("cancel status=%d verification=%#v input=%#v body=%s", cancelled.Code, verifier.seen, runner.cancelInput, cancelled.Body.String())
	}

	interrupt := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt", strings.NewReader(`{"generation":7}`))
	interrupt.Header.Set("Authorization", "Bearer access-token")
	interrupt.Header.Set("X-Request-ID", "request-interrupt")
	interrupt.Header.Set("Idempotency-Key", "idem-interrupt-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	interrupted := httptest.NewRecorder()
	handler.ServeHTTP(interrupted, interrupt)
	if interrupted.Code != http.StatusOK || runner.interruptInput.Generation != 7 || runner.interruptInput.TargetExecutionID != "execution-alpha" || !strings.Contains(interrupted.Body.String(), `"errorCode":"interrupted"`) {
		t.Fatalf("interrupt status=%d input=%#v body=%s", interrupted.Code, runner.interruptInput, interrupted.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerDownloadsPersistedArtifactCandidate(t *testing.T) {
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	candidate := runtimeprotocol.Message{
		RequestID: "request-artifact", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution-alpha", Generation: 7,
		CommandID: "turn-command", OccurredAt: now.Format(time.RFC3339Nano), MessageType: "ArtifactCandidate",
		Payload: map[string]any{"artifact": map[string]any{"path": "provider-diffs/result.diff", "kind": "diff", "sourceRoot": "runtime-output", "contentType": "text/x-diff"}},
	}
	terminal := candidate
	terminal.MessageType = "Result"
	terminal.Payload = map[string]any{"status": "completed"}
	store := &managedAgentExecutionStoreFake{execution: internalmanagedagent.ExecutionSnapshot{
		Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha",
		Generation: 7, State: internalmanagedagent.ExecutionSucceeded, Messages: []runtimeprotocol.Message{candidate, terminal}, Version: 2, CreatedAt: now, UpdatedAt: now,
	}}
	runner := &managedAgentExecutionRunnerFake{artifactResult: internalmanagedagent.RuntimeArtifact{Data: []byte("diff bytes"), ContentType: "text/x-diff", SHA256: strings.Repeat("a", 64)}}
	verifier := &projectHTTPVerifierFake{}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha/messages/0/artifact", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-download")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "diff bytes" || response.Header().Get("Content-Type") != "text/x-diff" || response.Header().Get("ETag") != `"sha256:`+strings.Repeat("a", 64)+`"` || verifier.seen.RequiredPermission != "projects.get" || runner.artifactInput.Message.MessageType != "ArtifactCandidate" {
		t.Fatalf("status=%d headers=%v body=%q verification=%#v input=%#v", response.Code, response.Header(), response.Body.String(), verifier.seen, runner.artifactInput)
	}
	notArtifact := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha/messages/1/artifact", nil)
	notArtifact.Header.Set("Authorization", "Bearer access-token")
	notArtifact.Header.Set("X-Request-ID", "request-not-artifact")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, notArtifact)
	if rejected.Code != http.StatusNotFound {
		t.Fatalf("non-ArtifactCandidate status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerExposesAndResolvesActiveInteractions(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	store := &managedAgentExecutionStoreFake{execution: internalmanagedagent.ExecutionSnapshot{Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 7, State: internalmanagedagent.ExecutionRunning, Version: 3, CreatedAt: now, UpdatedAt: now}}
	interaction := runtimeprotocol.Message{RequestID: "turn-request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution-alpha", Generation: 7, CommandID: "turn-command", OccurredAt: now.Format(time.RFC3339Nano), MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "codex:generation-7:approval:1", "interactionType": "approval", "summary": "Run command"}}
	runner := &managedAgentExecutionRunnerFake{interactions: []runtimeprotocol.Message{interaction}}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}

	get := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha", nil)
	get.Header.Set("Authorization", "Bearer access-token")
	get.Header.Set("X-Request-ID", "request-get")
	got := httptest.NewRecorder()
	handler.ServeHTTP(got, get)
	if got.Code != http.StatusOK || verifier.seen.RequiredPermission != "projects.get" || !strings.Contains(got.Body.String(), `"messageType":"InteractionRequest"`) || !strings.Contains(got.Body.String(), `"requestId":"codex:generation-7:approval:1"`) {
		t.Fatalf("get status=%d verification=%#v body=%s", got.Code, verifier.seen, got.Body.String())
	}

	approval := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveApproval", strings.NewReader(`{"generation":7,"requestId":"codex:generation-7:approval:1","decision":"accept"}`))
	approval.Header.Set("Authorization", "Bearer access-token")
	approval.Header.Set("X-Request-ID", "request-approval")
	approved := httptest.NewRecorder()
	handler.ServeHTTP(approved, approval)
	if approved.Code != http.StatusNoContent || verifier.seen.RequiredPermission != "projects.act" || runner.approvalInput.Generation != 7 || runner.approvalInput.InteractionRequestID != "codex:generation-7:approval:1" || runner.approvalInput.Decision != "accept" {
		t.Fatalf("approval status=%d verification=%#v input=%#v body=%s", approved.Code, verifier.seen, runner.approvalInput, approved.Body.String())
	}

	questionID := strings.Repeat("问", 200)
	userInput := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveUserInput", strings.NewReader(`{"generation":7,"requestId":"claude:generation-7:user-input:2","answers":{"`+questionID+`":["one","two"]}}`))
	userInput.Header.Set("Authorization", "Bearer access-token")
	userInput.Header.Set("X-Request-ID", "request-user-input")
	answered := httptest.NewRecorder()
	handler.ServeHTTP(answered, userInput)
	if answered.Code != http.StatusNoContent || runner.userInput.InteractionRequestID != "claude:generation-7:user-input:2" || !reflect.DeepEqual(runner.userInput.Answers, map[string][]string{questionID: {"one", "two"}}) {
		t.Fatalf("user-input status=%d input=%#v body=%s", answered.Code, runner.userInput, answered.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveApproval", strings.NewReader(`{"generation":7,"requestId":"approval","decision":"always"}`))
	invalid.Header.Set("Authorization", "Bearer access-token")
	invalid.Header.Set("X-Request-ID", "request-invalid")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, invalid)
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("invalid approval status=%d body=%s", rejected.Code, rejected.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerAcceptsMaximumEscapedInput(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentExecutionStoreFake{}
	runner := &managedAgentExecutionRunnerFake{}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}
	inputText := strings.Repeat("<", managedAgentMaximumInputBytes)
	body, err := json.Marshal(managedAgentExecutionRequest{TurnID: "turn-alpha", ExecutionID: "execution-alpha", InputText: inputText})
	if err != nil || len(body) <= managedAgentMaximumInputBytes || len(body) > managedAgentMaximumBodyBytes {
		t.Fatalf("maximum input body bytes=%d err=%v", len(body), err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || runner.input.InputText != inputText || runner.input.RuntimeMode != "full-access" || runner.input.InteractionMode != "default" {
		t.Fatalf("status=%d inputBytes=%d body=%s", response.Code, len(runner.input.InputText), response.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerRejectsPrincipalReverificationFailure(t *testing.T) {
	verifier := &managedAgentExecutionVerifierFake{failOnCall: 2}
	store := &managedAgentExecutionStoreFake{}
	runner := &managedAgentExecutionRunnerFake{sourceReads: 2}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions", strings.NewReader(`{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":"hello"}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || verifier.calls != 2 || runner.input.TurnID != "" || !strings.Contains(response.Body.String(), `"code":"AUTHENTICATION_FAILED"`) {
		t.Fatalf("status=%d verifierCalls=%d runnerInput=%#v body=%s", response.Code, verifier.calls, runner.input, response.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerRejectsNonIdentifierRequestID(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	store := &managedAgentExecutionStoreFake{}
	runner := &managedAgentExecutionRunnerFake{}
	handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions", strings.NewReader(`{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":"hello"}`))
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request:invalid")
	request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || response.Header().Get("X-Request-ID") != publicFallbackRequestID || verifier.calls != 0 || !strings.Contains(response.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("status=%d requestID=%q verifierCalls=%d body=%s", response.Code, response.Header().Get("X-Request-ID"), verifier.calls, response.Body.String())
	}
}

func TestManagedAgentExecutionHTTPServerRejectsInvalidExecuteInputs(t *testing.T) {
	validPath := "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions"
	validRequestID := "request-alpha"
	validIdempotencyKey := "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX"
	tests := []struct {
		name string
		body string
	}{
		{name: "turn identifier", body: `{"turnId":"-turn","executionId":"execution-alpha","inputText":"hello"}`},
		{name: "execution identifier", body: `{"turnId":"turn-alpha","executionId":"-execution","inputText":"hello"}`},
		{name: "empty input", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":""}`},
		{name: "empty model", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","model":"","inputText":"hello"}`},
		{name: "null model", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","model":null,"inputText":"hello"}`},
		{name: "runtime mode", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","runtimeMode":"always-allow","inputText":"hello"}`},
		{name: "interaction mode", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","interactionMode":"chat","inputText":"hello"}`},
		{name: "unknown field", body: `{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":"hello","extra":true}`},
		{name: "duplicate field", body: `{"turnId":"turn-alpha","turnId":"turn-beta","executionId":"execution-alpha","inputText":"hello"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			store := &managedAgentExecutionStoreFake{}
			runner := &managedAgentExecutionRunnerFake{}
			handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, validPath, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer access-token")
			request.Header.Set("X-Request-ID", validRequestID)
			request.Header.Set("Idempotency-Key", validIdempotencyKey)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || verifier.calls != 0 || runner.input.TurnID != "" || runner.input.ExecutionID != "" {
				t.Fatalf("status=%d verifierCalls=%d runnerInput=%#v body=%s", response.Code, verifier.calls, runner.input, response.Body.String())
			}
		})
	}
}

func TestManagedAgentExecutionHTTPServerRejectsInvalidMutationInputs(t *testing.T) {
	tests := []struct {
		name   string
		action string
		body   string
	}{
		{name: "cancel generation overflow", action: "cancel", body: `{"generation":9223372036854775808}`},
		{name: "interrupt generation overflow", action: "interrupt", body: `{"generation":9223372036854775808}`},
		{name: "cancel unknown field", action: "cancel", body: `{"generation":7,"extra":true}`},
		{name: "interrupt zero generation", action: "interrupt", body: `{"generation":0}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier := &projectHTTPVerifierFake{}
			store := &managedAgentExecutionStoreFake{}
			runner := &managedAgentExecutionRunnerFake{}
			handler, err := NewManagedAgentExecutionHTTPServer(verifier, store, runner)
			if err != nil {
				t.Fatal(err)
			}
			path := "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:" + test.action
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer access-token")
			request.Header.Set("X-Request-ID", "request-alpha")
			request.Header.Set("Idempotency-Key", "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || verifier.calls != 0 || runner.cancelInput.Generation != 0 || runner.interruptInput.Generation != 0 {
				t.Fatalf("status=%d verifierCalls=%d cancel=%#v interrupt=%#v body=%s", response.Code, verifier.calls, runner.cancelInput, runner.interruptInput, response.Body.String())
			}
		})
	}
}

func TestManagedAgentExecutionPathRejectsCrossTurnLookup(t *testing.T) {
	if HandlesManagedAgentExecutionPath("/v1/tenants/t/projects/p/sessions/s/turns/t:bad/executions/e") {
		t.Fatal("accepted a colon-bearing turn id")
	}
	if !HandlesManagedAgentExecutionPath("/v1/tenants/t/projects/p/sessions/s/turns/turn/executions/execution:cancel") {
		t.Fatal("did not accept execution cancel path")
	}
	if !HandlesManagedAgentExecutionPath("/v1/tenants/t/projects/p/sessions/s/turns/turn/executions/execution:interrupt") {
		t.Fatal("did not accept execution interrupt path")
	}
}
