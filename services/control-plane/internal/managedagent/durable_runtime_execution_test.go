package managedagent

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
)

type durableRuntimeExecutionStoreFake struct {
	calls      []string
	principals []*authn.VerifiedPrincipal
	turn       *TurnSnapshot
	execution  ExecutionSnapshot
	cancel     context.CancelFunc
}

func (fake *durableRuntimeExecutionStoreFake) GetManagedAgentSessionForExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string) (RuntimeSessionSnapshot, error) {
	fake.calls = append(fake.calls, "session")
	fake.recordPrincipal(principal)
	return RuntimeSessionSnapshot{SessionSnapshot: SessionSnapshot{ProviderKind: "codex"}}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FindManagedAgentTurnForExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ string, _ string, _ string) (TurnSnapshot, bool, error) {
	fake.calls = append(fake.calls, "find-turn")
	fake.recordPrincipal(principal)
	if fake.turn == nil {
		return TurnSnapshot{}, false, nil
	}
	return *fake.turn, true, nil
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

func (fake *durableRuntimeExecutionStoreFake) CompleteManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, _ CompleteRuntimeExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "complete")
	fake.recordPrincipal(principal)
	return ExecutionTransitionResult{}, nil
}

func (fake *durableRuntimeExecutionStoreFake) FailManagedAgentExecution(_ context.Context, _ string, principal *authn.VerifiedPrincipal, input FailRuntimeExecutionInput) (ExecutionTransitionResult, error) {
	fake.calls = append(fake.calls, "fail")
	fake.recordPrincipal(principal)
	fake.execution.State = ExecutionFailed
	fake.execution.ErrorCode = input.ErrorCode
	return ExecutionTransitionResult{
		Turn:      TurnSnapshot{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, State: TurnFailed},
		Execution: fake.execution,
	}, nil
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

func TestReceiveRuntimeMessagesPreservesBoundedTranscript(t *testing.T) {
	wire := []runtimeprotocol.Message{
		{RequestID: "resolve-request", CommandID: "interaction-1", MessageType: "Result"},
		{CommandID: "turn", MessageType: "Progress"},
		{CommandID: "turn", MessageType: "Event"},
		{CommandID: "turn", MessageType: "Result", Payload: map[string]any{"text": "done"}},
	}
	index := 0
	accepted := make([]runtimeprotocol.Message, 0, len(wire)-1)
	collected, terminal, err := receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		message := wire[index]
		index++
		return message, nil
	}, "turn", func(message runtimeprotocol.Message) (bool, error) {
		return message.CommandID == "interaction-1", nil
	}, func(message runtimeprotocol.Message) {
		accepted = append(accepted, message)
	})
	if err != nil || !reflect.DeepEqual(collected, wire[1:]) || !reflect.DeepEqual(accepted, wire[1:]) || terminal.MessageType != "Result" || terminal.Payload["text"] != "done" || index != len(wire) {
		t.Fatalf("messages=%#v accepted=%#v terminal=%#v reads=%d err=%v", collected, accepted, terminal, index, err)
	}
}

func TestRuntimeFailureCodeUsesOnlyPublicStableCodes(t *testing.T) {
	terminal := runtimeprotocol.Message{MessageType: "Error", Error: &runtimeprotocol.Error{Code: "capability_unsupported"}}
	if got := runtimeFailureCode(terminal, "runtime_start_failed"); got != "capability_unsupported" {
		t.Fatalf("Runtime failure code = %q", got)
	}
	terminal.Error.Code = "INVALID_CODE!"
	if got := runtimeFailureCode(terminal, "runtime_start_failed"); got != "runtime_start_failed" {
		t.Fatalf("invalid Runtime failure code = %q", got)
	}
}

func TestRuntimeInteractionsResolveOnTheActiveStream(t *testing.T) {
	reference := RuntimeExecutionReference{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7}
	key, err := durableRuntimeExecutionKey(reference)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &DurableRuntimeExecutionCoordinator{now: func() time.Time { return time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC) }, active: make(map[durableExecutionKey]*activeDurableExecution)}
	active, unregister, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer unregister()
	sent := make(chan runtimeprotocol.Command, 3)
	active.send = func(_ context.Context, command runtimeprotocol.Command) error {
		sent <- command
		return nil
	}
	approval := runtimeprotocol.Message{RequestID: "turn-request", CommandID: "turn-command", MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "codex:generation-7:approval:1", "interactionType": "approval"}}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", approval); handled || err != nil {
		t.Fatalf("register approval handled=%v err=%v", handled, err)
	}
	coordinator.recordActiveRuntimeMessage(key, active, approval)
	if got := coordinator.ActiveMessages(reference); len(got) != 1 || got[0].Payload["requestId"] != approval.Payload["requestId"] {
		t.Fatalf("active approval = %#v", got)
	}
	resolved := make(chan error, 1)
	approvalInput := RuntimeApprovalResolutionInput{RuntimeExecutionReference: reference, RequestID: "resolve-approval", InteractionRequestID: "codex:generation-7:approval:1", Decision: "accept"}
	go func() { resolved <- coordinator.ResolveApproval(context.Background(), approvalInput) }()
	command := <-sent
	if command.CommandType != "ResolveApproval" || command.Payload["requestId"] != approvalInput.InteractionRequestID {
		t.Fatalf("approval command = %#v", command)
	}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Result"}); !handled || err != nil {
		t.Fatalf("approval result handled=%v err=%v", handled, err)
	}
	if err := <-resolved; err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ResolveApproval(context.Background(), approvalInput); err != nil {
		t.Fatalf("idempotent approval = %v", err)
	}
	approvalInput.Decision = "decline"
	if err := coordinator.ResolveApproval(context.Background(), approvalInput); !errors.Is(err, ErrRuntimeInteractionConflict) {
		t.Fatalf("conflicting approval = %v", err)
	}

	userInput := runtimeprotocol.Message{RequestID: "turn-request", CommandID: "turn-command", MessageType: "InteractionRequest", Payload: map[string]any{"requestId": "claude:generation-7:user-input:2", "interactionType": "user-input"}}
	if _, err := coordinator.routeRuntimeMessage(key, active, "turn-command", userInput); err != nil {
		t.Fatal(err)
	}
	coordinator.recordActiveRuntimeMessage(key, active, userInput)
	answered := make(chan error, 1)
	questionID := strings.Repeat("问", maxRuntimeInteractionRequestIDCharacters)
	answerInput := RuntimeUserInputResolutionInput{RuntimeExecutionReference: reference, RequestID: "resolve-input", InteractionRequestID: "claude:generation-7:user-input:2", Answers: map[string][]string{questionID: {"one", "two"}}}
	go func() { answered <- coordinator.ResolveUserInput(context.Background(), answerInput) }()
	command = <-sent
	if command.CommandType != "ResolveUserInput" {
		t.Fatalf("user-input command = %#v", command)
	}
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Error", Error: &runtimeprotocol.Error{Code: "provider_failed"}}); !handled || err != nil {
		t.Fatalf("user-input error handled=%v err=%v", handled, err)
	}
	if err := <-answered; !errors.Is(err, ErrRuntimeInteractionFailed) {
		t.Fatalf("user-input failure = %v", err)
	}
	go func() { answered <- coordinator.ResolveUserInput(context.Background(), answerInput) }()
	command = <-sent
	if handled, err := coordinator.routeRuntimeMessage(key, active, "turn-command", runtimeprotocol.Message{RequestID: command.RequestID, CommandID: command.CommandID, MessageType: "Result"}); !handled || err != nil {
		t.Fatalf("user-input retry handled=%v err=%v", handled, err)
	}
	if err := <-answered; err != nil {
		t.Fatal(err)
	}
	if got := coordinator.ActiveMessages(reference); len(got) != 2 {
		t.Fatalf("active transcript = %#v", got)
	}
}

func TestReceiveRuntimeMessagesRejectsPublicLimits(t *testing.T) {
	reads := 0
	accepted := 0
	messages, _, err := receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		reads++
		return runtimeprotocol.Message{CommandID: "turn", MessageType: "Progress"}, nil
	}, "turn", nil, func(runtimeprotocol.Message) { accepted++ })
	if err == nil || len(messages) != maxRuntimeExecutionMessages || accepted != maxRuntimeExecutionMessages || reads != maxRuntimeExecutionMessages+1 {
		t.Fatalf("count limit: messages=%d accepted=%d reads=%d err=%v", len(messages), accepted, reads, err)
	}
	accepted = 0
	messages, _, err = receiveRuntimeMessages(func() (runtimeprotocol.Message, error) {
		return runtimeprotocol.Message{CommandID: "turn", MessageType: "Progress", Payload: map[string]any{"text": strings.Repeat("x", runtimeprotocol.MaxMessageBytes)}}, nil
	}, "turn", nil, func(runtimeprotocol.Message) { accepted++ })
	if err == nil || len(messages) != 0 || accepted != 0 {
		t.Fatalf("byte limit: messages=%d accepted=%d err=%v", len(messages), accepted, err)
	}
}

func TestRuntimeTerminalDigestBindsPublicMessage(t *testing.T) {
	original := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done", "providerResumeCursor": "private-cursor"}}
	public := publicRuntimeMessage(original)
	if _, exists := public.Payload["providerResumeCursor"]; exists || original.Payload["providerResumeCursor"] != "private-cursor" {
		t.Fatalf("public/original payload = %#v / %#v", public.Payload, original.Payload)
	}
	digest, err := RuntimeMessageDigest(public)
	if err != nil {
		t.Fatal(err)
	}
	progress := public
	progress.MessageType = "Progress"
	progress.Payload = map[string]any{"text": "working"}
	input := CompleteRuntimeExecutionInput{CompleteExecutionInput: CompleteExecutionInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7, ResultDigest: digest, Mutation: Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"}}, ProviderResumeCursor: "private-cursor", Messages: []runtimeprotocol.Message{progress, public}}
	if _, err := RuntimeExecutionCompleteMutationDigest(input); err != nil {
		t.Fatal(err)
	}
	input.ResultDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := RuntimeExecutionCompleteMutationDigest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mismatched terminal digest error = %v", err)
	}
}

func TestRuntimeResultInvalidFailurePreservesReceivedResult(t *testing.T) {
	result := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "done"}}
	input := FailRuntimeExecutionInput{FailExecutionInput: FailExecutionInput{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7, ErrorCode: "runtime_result_invalid", Mutation: Mutation{RequestID: "request", IdempotencyKey: "idempotency-key-1234"}}, Messages: []runtimeprotocol.Message{result}}
	if _, err := RuntimeExecutionFailMutationDigest(input); err != nil {
		t.Fatal(err)
	}
	input.ErrorCode = "runtime_failed"
	if _, err := RuntimeExecutionFailMutationDigest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ordinary failure accepted a Result transcript: %v", err)
	}
}

func TestDeriveRuntimeWorkspacePathsScopesSessionStateAndExecutionOutput(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	first, err := deriveRuntimeWorkspacePaths("/workspace", scope, "session", "turn-a", "execution-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := deriveRuntimeWorkspacePaths("/workspace", scope, "session", "turn-b", "execution-b")
	if err != nil {
		t.Fatal(err)
	}
	wantSessionRoot := filepath.Join("/workspace", ".cloud-agents", "managed-agent", "tenants", "tenant", "projects", "project", "sessions", "session")
	want := runtimeWorkspacePaths{
		workspaceDirectory:     filepath.Join(wantSessionRoot, "workspace"),
		runtimeOutputDirectory: filepath.Join(wantSessionRoot, "runtime-output", "turn-a", "execution-a"),
		providerStateDirectory: filepath.Join(wantSessionRoot, "provider-state"),
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first paths = %#v, want %#v", first, want)
	}
	if first.workspaceDirectory != second.workspaceDirectory || first.providerStateDirectory != second.providerStateDirectory || first.runtimeOutputDirectory == second.runtimeOutputDirectory {
		t.Fatalf("session/output scoping = first %#v, second %#v", first, second)
	}
	if _, err := deriveRuntimeWorkspacePaths("relative", scope, "session", "turn", "execution"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("relative base error = %v", err)
	}
	if _, err := deriveRuntimeWorkspacePaths("/workspace", scope, "../session", "turn", "execution"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("traversal session error = %v", err)
	}
}

func TestDurableRuntimeExecutionReturnsTerminalReplayWithoutOpeningWorker(t *testing.T) {
	terminal := runtimeprotocol.Message{RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "command", OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano), MessageType: "Result", Payload: map[string]any{"text": "persisted"}}
	digest, err := RuntimeMessageDigest(terminal)
	if err != nil {
		t.Fatal(err)
	}
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionSucceeded, ResultDigest: digest, Messages: []runtimeprotocol.Message{terminal}}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded || len(result.Messages) != 1 || result.Messages[0].Payload["text"] != "persisted" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 4)
}

func TestDurableRuntimeExecutionUsesMatchingPrecreatedTurn(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	digest, err := TurnInputDigest("hello")
	if err != nil {
		t.Fatal(err)
	}
	turn := TurnSnapshot{Scope: scope, SessionID: "session", TurnID: "turn", InputDigest: digest, State: TurnQueued}
	store := &durableRuntimeExecutionStoreFake{turn: &turn, execution: ExecutionSnapshot{State: ExecutionSucceeded}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: scope, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if err != nil || result.Transition.Execution.State != ExecutionSucceeded || !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "execution"}) {
		t.Fatalf("result=%#v calls=%v err=%v", result, store.calls, err)
	}
	assertFreshPrincipals(t, store.principals, 3)
}

func TestDurableRuntimeExecutionRejectsMismatchedPrecreatedTurn(t *testing.T) {
	scope := Scope{TenantID: "tenant", ProjectID: "project"}
	digest, err := TurnInputDigest("different")
	if err != nil {
		t.Fatal(err)
	}
	turn := TurnSnapshot{Scope: scope, SessionID: "session", TurnID: "turn", InputDigest: digest, State: TurnQueued}
	store := &durableRuntimeExecutionStoreFake{turn: &turn}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: time.Now,
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: scope, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionConflict) || !reflect.DeepEqual(store.calls, []string{"session", "find-turn"}) {
		t.Fatalf("calls=%v err=%v", store.calls, err)
	}
}

func TestDurableRuntimeExecutionFailsOrphanedRunningReplayWithoutOpeningWorker(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := coordinator.Execute(context.Background(), testVerifiedPrincipalSource(), DurableRuntimeExecutionInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", InputText: "hello",
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "idem"},
	})
	if !errors.Is(err, ErrDurableRuntimeExecutionFailed) || result.Transition.Execution.State != ExecutionFailed || result.Transition.Execution.ErrorCode != "orphaned_execution" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "fail"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 5)
}

func TestDurableRuntimeExecutionPersistsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionQueued}, cancel: cancel}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
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
	if !reflect.DeepEqual(store.calls, []string{"session", "find-turn", "turn", "execution", "start", "cancel"}) {
		t.Fatalf("calls=%v", store.calls)
	}
	assertFreshPrincipals(t, store.principals, 6)
}

func TestDurableRuntimeExecutionCancelSignalsActiveRuntime(t *testing.T) {
	store := &durableRuntimeExecutionStoreFake{execution: ExecutionSnapshot{State: ExecutionRunning}}
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: store, Supervisor: &workerclient.Supervisor{}, Clock: func() time.Time { return time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC) },
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	activeContext, activeCancel := context.WithCancel(context.Background())
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	active, unregister, err := coordinator.registerActiveExecution(key, activeCancel)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	active.stop = func() { stopped = true }
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
	if !stopped {
		t.Fatal("active runtime stream was not stopped")
	}
	if !unregister() {
		t.Fatal("active cancellation was not recorded")
	}
}

func TestDurableRuntimeExecutionRejectsDuplicateActiveExecution(t *testing.T) {
	coordinator, err := NewDurableRuntimeExecutionCoordinator(DurableRuntimeExecutionConfig{
		Store: &durableRuntimeExecutionStoreFake{}, Supervisor: &workerclient.Supervisor{}, Clock: time.Now,
		FencingLeaseID: "lease", FencingGeneration: 7, FencingToken: []byte("token"), WorkspaceDirectory: "/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := durableExecutionKey{tenantID: "tenant", projectID: "project", sessionID: "session", turnID: "turn", executionID: "execution", generation: 7}
	_, first, err := coordinator.registerActiveExecution(key, func() {})
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	if _, _, err := coordinator.registerActiveExecution(key, func() {}); !errors.Is(err, ErrDurableRuntimeExecutionConflict) {
		t.Fatalf("duplicate active execution error = %v", err)
	}
}

func TestRuntimeArtifactCandidateUsesPersistedExecutionAuthority(t *testing.T) {
	message := runtimeprotocol.Message{
		RequestID: "request", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution", Generation: 7, CommandID: "turn",
		OccurredAt: "2026-09-01T08:00:00Z", MessageType: "ArtifactCandidate",
		Payload: map[string]any{"artifact": map[string]any{"path": "provider-diffs/result.diff", "kind": "diff", "sourceRoot": "runtime-output", "contentType": "text/x-diff", "reportedSize": float64(10), "sha256": strings.Repeat("a", 64)}},
	}
	input := RuntimeArtifactReadInput{RuntimeExecutionReference: RuntimeExecutionReference{Scope: Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 7}, Message: message}
	candidate, err := runtimeArtifactCandidate(input)
	if err != nil || candidate.sourceRoot != "runtime-output" || candidate.relativePath != "provider-diffs/result.diff" || candidate.expectedSize == nil || *candidate.expectedSize != 10 || candidate.sha256 != strings.Repeat("a", 64) {
		t.Fatalf("candidate=%#v err=%v", candidate, err)
	}
	message.Payload["artifact"].(map[string]any)["path"] = "../secret"
	input.Message = message
	if _, err := runtimeArtifactCandidate(input); !errors.Is(err, ErrRuntimeArtifactUnavailable) {
		t.Fatalf("escaping candidate error=%v", err)
	}
}
