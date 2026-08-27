package managedagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var lifecycleTestScope = Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}

func TestLifecycleProfileIsFrozenAndDetached(t *testing.T) {
	profile := ManagedAgentLifecycleProfile()
	if !profile.Valid() {
		t.Fatalf("profile is not valid: %#v", profile)
	}
	if profile.ID != LifecycleProfileID || profile.StateMachineDigest == "" {
		t.Fatalf("unexpected profile identity: %#v", profile)
	}
	transitions := profile.AllowedTransitions()
	if len(transitions) != len(lifecycleTransitions) {
		t.Fatalf("transition count = %d, want %d", len(transitions), len(lifecycleTransitions))
	}
	transitions[0].To = "caller-selected"
	if profile.AllowedTransitions()[0].To == "caller-selected" || !profile.Valid() {
		t.Fatal("caller mutation changed the profile authority")
	}
	seen := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		key := fmt.Sprintf("%s:%s:%s:%s", transition.Resource, transition.From, transition.Event, transition.To)
		if _, exists := seen[key]; exists {
			t.Fatalf("duplicate transition %q", key)
		}
		seen[key] = struct{}{}
	}
}

func TestLifecycleInterruptAndCancelKeepDistinctExecutionEdges(t *testing.T) {
	if !transitionAllowed(ResourceTurn, string(TurnRunning), "interrupt", string(TurnInterrupted)) {
		t.Fatal("interrupt turn edge is missing")
	}
	if !transitionAllowed(ResourceExecution, string(ExecutionRunning), "interrupt", string(ExecutionCancelled)) {
		t.Fatal("interrupt execution edge is missing")
	}
	if !transitionAllowed(ResourceTurn, string(TurnRunning), "cancel", string(TurnCancelled)) {
		t.Fatal("cancel turn edge is missing")
	}
	if !transitionAllowed(ResourceExecution, string(ExecutionRunning), "cancel", string(ExecutionCancelled)) {
		t.Fatal("cancel execution edge is missing")
	}
}

func TestLifecycleHappyPathAndIdempotentReplay(t *testing.T) {
	clock := newTestClock()
	store := newTestStore(t, clock)
	ctx := context.Background()

	sessionInput := CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-1", ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-session-1", IdempotencyKey: "idem-session-1"},
	}
	session, err := store.CreateSession(ctx, sessionInput)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != SessionActive || session.Version != 1 || session.ProviderKind != "codex" {
		t.Fatalf("session = %#v", session)
	}
	replayedSession, err := store.CreateSession(ctx, sessionInput)
	if err != nil || replayedSession != session {
		t.Fatalf("session replay = %#v / %v", replayedSession, err)
	}

	turnInput := CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-1", InputText: "hello",
		Mutation: Mutation{RequestID: "request-turn-1", IdempotencyKey: "idem-turn-1"},
	}
	turn, err := store.CreateTurn(ctx, turnInput)
	if err != nil {
		t.Fatal(err)
	}
	if turn.State != TurnQueued || turn.Version != 1 || turn.InputDigest == "" || turn.ExecutionID != "" {
		t.Fatalf("turn = %#v", turn)
	}
	if strings.Contains(turn.InputDigest, "hello") {
		t.Fatal("turn snapshot retained raw input")
	}

	executionInput := CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		Mutation: Mutation{RequestID: "request-execution-1", IdempotencyKey: "idem-execution-1"},
	}
	execution, err := store.CreateExecution(ctx, executionInput)
	if err != nil {
		t.Fatal(err)
	}
	if execution.State != ExecutionQueued || execution.Version != 1 || execution.Generation != 1 {
		t.Fatalf("execution = %#v", execution)
	}
	turn, err = store.GetTurn(ctx, lifecycleTestScope, "session-1", "turn-1")
	if err != nil || turn.ExecutionID != "execution-1" || turn.Version != 2 {
		t.Fatalf("turn after execution = %#v / %v", turn, err)
	}

	started, err := store.StartExecution(ctx, StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		Mutation: Mutation{RequestID: "request-start-1", IdempotencyKey: "idem-start-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Turn.State != TurnRunning || started.Execution.State != ExecutionRunning || started.Turn.Version != 3 || started.Execution.Version != 2 {
		t.Fatalf("started = %#v", started)
	}
	resultDigest := "sha256:" + strings.Repeat("a", 64)
	completed, err := store.CompleteExecution(ctx, CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		ResultDigest: resultDigest, Mutation: Mutation{RequestID: "request-complete-1", IdempotencyKey: "idem-complete-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Turn.State != TurnCompleted || completed.Execution.State != ExecutionSucceeded || completed.Execution.ResultDigest != resultDigest {
		t.Fatalf("completed = %#v", completed)
	}
	replayed, err := store.CompleteExecution(ctx, CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		ResultDigest: resultDigest, Mutation: Mutation{RequestID: "request-complete-retry", IdempotencyKey: "idem-complete-1"},
	})
	if err != nil || replayed != completed {
		t.Fatalf("terminal replay = %#v / %v", replayed, err)
	}

	closed, err := store.CloseSession(ctx, CloseSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-1",
		Mutation: Mutation{RequestID: "request-close-1", IdempotencyKey: "idem-close-1"},
	})
	if err != nil || closed.State != SessionClosed || closed.Version != 2 {
		t.Fatalf("closed = %#v / %v", closed, err)
	}
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-1", TurnID: "turn-2", InputText: "after close",
		Mutation: Mutation{RequestID: "request-turn-2", IdempotencyKey: "idem-turn-2"},
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("create after close error = %v", err)
	}
}

func TestLifecycleFailureAndInterruptTransitions(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		errorCode string
		interrupt bool
		cancel    bool
		turnState TurnState
		execState ExecutionState
	}{
		{name: "failure", errorCode: "provider_unavailable", turnState: TurnFailed, execState: ExecutionFailed},
		{name: "interrupt", interrupt: true, turnState: TurnInterrupted, execState: ExecutionCancelled},
		{name: "cancel", cancel: true, turnState: TurnCancelled, execState: ExecutionCancelled},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t, newTestClock())
			createLifecycle(t, store, "session-"+test.name, "turn-"+test.name, "execution-"+test.name)
			var result ExecutionTransitionResult
			var err error
			if test.interrupt {
				result, err = store.InterruptTurn(ctx, InterruptTurnInput{
					Scope: lifecycleTestScope, SessionID: "session-" + test.name, TurnID: "turn-" + test.name,
					TargetExecutionID: "execution-" + test.name, Generation: 1,
					Mutation: Mutation{RequestID: "request-interrupt-" + test.name, IdempotencyKey: "idem-interrupt-" + test.name},
				})
			} else if test.cancel {
				result, err = store.CancelTurn(ctx, CancelTurnInput{
					Scope: lifecycleTestScope, SessionID: "session-" + test.name, TurnID: "turn-" + test.name,
					TargetExecutionID: "execution-" + test.name, Generation: 1,
					Mutation: Mutation{RequestID: "request-cancel-" + test.name, IdempotencyKey: "idem-cancel-" + test.name},
				})
			} else {
				result, err = store.FailExecution(ctx, FailExecutionInput{
					Scope: lifecycleTestScope, SessionID: "session-" + test.name, TurnID: "turn-" + test.name,
					ExecutionID: "execution-" + test.name, Generation: 1, ErrorCode: test.errorCode,
					Mutation: Mutation{RequestID: "request-fail-" + test.name, IdempotencyKey: "idem-fail-" + test.name},
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Turn.State != test.turnState || result.Execution.State != test.execState {
				t.Fatalf("result = %#v", result)
			}
			if test.interrupt && result.Execution.ErrorCode != "interrupted" {
				t.Fatalf("interrupt error code = %q", result.Execution.ErrorCode)
			}
			if !test.interrupt && !test.cancel && result.Execution.ErrorCode != test.errorCode {
				t.Fatalf("failure error code = %q", result.Execution.ErrorCode)
			}
			if test.cancel && result.Execution.ErrorCode != "cancelled" {
				t.Fatalf("cancel error code = %q", result.Execution.ErrorCode)
			}
			if _, err := store.StartExecution(ctx, StartExecutionInput{
				Scope: lifecycleTestScope, SessionID: "session-" + test.name, TurnID: "turn-" + test.name,
				ExecutionID: "execution-" + test.name, Generation: 1,
				Mutation: Mutation{RequestID: "request-start-again-" + test.name, IdempotencyKey: "idem-start-again-" + test.name},
			}); !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("terminal restart error = %v", err)
			}
		})
	}
}

func TestLifecycleRejectsInvalidTransitionsAndBusySession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "missing", TurnID: "turn-1", InputText: "hello",
		Mutation: Mutation{RequestID: "request-missing", IdempotencyKey: "idem-missing"},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session error = %v", err)
	}
	createSession(t, store, "session-busy")
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-1", InputText: "one",
		Mutation: Mutation{RequestID: "request-turn-busy-1", IdempotencyKey: "idem-turn-busy-1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-2", InputText: "two",
		Mutation: Mutation{RequestID: "request-turn-busy-2", IdempotencyKey: "idem-turn-busy-2"},
	}); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second foreground turn error = %v", err)
	}
	if _, err := store.CloseSession(ctx, CloseSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-busy",
		Mutation: Mutation{RequestID: "request-close-busy", IdempotencyKey: "idem-close-busy"},
	}); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("close busy session error = %v", err)
	}
	if _, err := store.StartExecution(ctx, StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		Mutation: Mutation{RequestID: "request-start-missing", IdempotencyKey: "idem-start-missing"},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("start without execution error = %v", err)
	}
	if _, err := store.CompleteExecution(ctx, CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		ResultDigest: "sha256:" + strings.Repeat("a", 64), Mutation: Mutation{RequestID: "request-complete-missing", IdempotencyKey: "idem-complete-missing"},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("complete without execution error = %v", err)
	}
	createExecutionForTest(t, store, "session-busy", "turn-1", "execution-1")
	if _, err := store.StartExecution(ctx, StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-1", ExecutionID: "execution-1", Generation: 1,
		Mutation: Mutation{RequestID: "request-start-real", IdempotencyKey: "idem-start-real"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InterruptTurn(ctx, InterruptTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-busy", TurnID: "turn-1", TargetExecutionID: "foreign-execution", Generation: 1,
		Mutation: Mutation{RequestID: "request-foreign-target", IdempotencyKey: "idem-foreign-target"},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign execution target error = %v", err)
	}
	turnBefore, err := store.GetTurn(ctx, lifecycleTestScope, "session-busy", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	if turnBefore.State != TurnRunning {
		t.Fatalf("foreign target changed turn = %#v", turnBefore)
	}
}

func TestLifecycleIdempotencyConflictAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	input := CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-idem", ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-idem", IdempotencyKey: "same-key"},
	}
	if _, err := store.CreateSession(ctx, input); err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.ProviderKind = "claude"
	if _, err := store.CreateSession(ctx, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	otherTenant := input
	otherTenant.Scope = Scope{TenantID: "tenant-beta", ProjectID: "project-beta"}
	if _, err := store.CreateSession(ctx, otherTenant); err != nil {
		t.Fatalf("other tenant create error = %v", err)
	}
	if _, err := store.GetSession(ctx, Scope{TenantID: "tenant-beta", ProjectID: "project-alpha"}, "session-idem"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign project lookup error = %v", err)
	}
	if _, err := store.GetSession(ctx, Scope{TenantID: "tenant-foreign", ProjectID: "project-foreign"}, "session-idem"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant lookup error = %v", err)
	}
}

func TestLifecycleValidationFailsClosed(t *testing.T) {
	if _, err := NewStore(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil clock error = %v", err)
	}
	var zero Store
	if _, err := zero.CreateSession(context.Background(), CreateSessionInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero store mutation error = %v", err)
	}
	if _, err := zero.GetSession(context.Background(), lifecycleTestScope, "session-zero"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero store read error = %v", err)
	}
	store := newTestStore(t, newTestClock())
	ctx := context.Background()
	valid := CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-valid", IdempotencyKey: "idem-valid"},
	}
	invalids := []struct {
		name  string
		input CreateSessionInput
	}{
		{name: "tenant path", input: func() CreateSessionInput { value := valid; value.Scope.TenantID = "tenant/escape"; return value }()},
		{name: "session leading punctuation", input: func() CreateSessionInput { value := valid; value.SessionID = "-session"; return value }()},
		{name: "provider control", input: func() CreateSessionInput { value := valid; value.ProviderKind = "codex\n"; return value }()},
		{name: "request control", input: func() CreateSessionInput { value := valid; value.Mutation.RequestID = "request\x00"; return value }()},
		{name: "idempotency empty", input: func() CreateSessionInput { value := valid; value.Mutation.IdempotencyKey = ""; return value }()},
	}
	for _, test := range invalids {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.CreateSession(ctx, test.input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := store.CreateSession(ctx, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid", InputText: "",
		Mutation: Mutation{RequestID: "request-empty-input", IdempotencyKey: "idem-empty-input"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty input error = %v", err)
	}
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid", InputText: string([]byte{0xff}),
		Mutation: Mutation{RequestID: "request-invalid-input", IdempotencyKey: "idem-invalid-input"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid UTF-8 input error = %v", err)
	}
	if _, err := store.CreateExecution(ctx, CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid", ExecutionID: "execution-valid", Generation: 0,
		Mutation: Mutation{RequestID: "request-zero-generation", IdempotencyKey: "idem-zero-generation"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero generation error = %v", err)
	}
	createTurn(t, store, "session-valid", "turn-valid-2")
	if _, err := store.CreateExecution(ctx, CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid-2", ExecutionID: "execution-valid", Generation: 1,
		Mutation: Mutation{RequestID: "request-valid-execution", IdempotencyKey: "idem-valid-execution"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartExecution(ctx, StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid-2", ExecutionID: "execution-valid", Generation: 2,
		Mutation: Mutation{RequestID: "request-wrong-generation", IdempotencyKey: "idem-wrong-generation"},
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("wrong generation error = %v", err)
	}
	if _, err := store.CompleteExecution(ctx, CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-valid", TurnID: "turn-valid-2", ExecutionID: "execution-valid", Generation: 1,
		ResultDigest: "sha256:bad", Mutation: Mutation{RequestID: "request-bad-digest", IdempotencyKey: "idem-bad-digest"},
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad digest error = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.CreateSession(cancelled, CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-cancelled", ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-cancelled", IdempotencyKey: "idem-cancelled"},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	if _, err := store.GetSession(ctx, lifecycleTestScope, "session-cancelled"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelled mutation left state: %v", err)
	}
}

func TestLifecycleTerminalStateIsImmutableAcrossNewCommands(t *testing.T) {
	store := newTestStore(t, newTestClock())
	createLifecycle(t, store, "session-terminal", "turn-terminal", "execution-terminal")
	resultDigest := "sha256:" + strings.Repeat("b", 64)
	if _, err := store.CompleteExecution(context.Background(), CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-terminal", TurnID: "turn-terminal", ExecutionID: "execution-terminal", Generation: 1,
		ResultDigest: resultDigest, Mutation: Mutation{RequestID: "request-terminal-complete", IdempotencyKey: "idem-terminal-complete"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FailExecution(context.Background(), FailExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-terminal", TurnID: "turn-terminal", ExecutionID: "execution-terminal", Generation: 1,
		ErrorCode: "late-failure", Mutation: Mutation{RequestID: "request-late-failure", IdempotencyKey: "idem-late-failure"},
	}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("late failure error = %v", err)
	}
	turn, err := store.GetTurn(context.Background(), lifecycleTestScope, "session-terminal", "turn-terminal")
	if err != nil || turn.State != TurnCompleted {
		t.Fatalf("terminal turn changed = %#v / %v", turn, err)
	}
	execution, err := store.GetExecution(context.Background(), lifecycleTestScope, "session-terminal", "turn-terminal", "execution-terminal")
	if err != nil || execution.State != ExecutionSucceeded || execution.ResultDigest != resultDigest {
		t.Fatalf("terminal execution changed = %#v / %v", execution, err)
	}
}

func TestLifecycleConcurrentIdempotentCreateIsSingleResult(t *testing.T) {
	store := newTestStore(t, newTestClock())
	input := CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-concurrent", ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-concurrent", IdempotencyKey: "idem-concurrent"},
	}
	const callers = 32
	results := make(chan SessionSnapshot, callers)
	errorsOut := make(chan error, callers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer waitGroup.Done()
			result, err := store.CreateSession(context.Background(), input)
			results <- result
			errorsOut <- err
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsOut)
	var first SessionSnapshot
	for result := range results {
		if first.SessionID == "" {
			first = result
		}
		if result != first {
			t.Fatalf("concurrent result drift: %#v vs %#v", result, first)
		}
	}
	for err := range errorsOut {
		if err != nil {
			t.Fatalf("concurrent create error: %v", err)
		}
	}
}

func createLifecycle(t *testing.T, store *Store, sessionID, turnID, executionID string) {
	t.Helper()
	createSession(t, store, sessionID)
	createTurn(t, store, sessionID, turnID)
	if _, err := store.CreateExecution(context.Background(), CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: 1,
		Mutation: Mutation{RequestID: "request-create-" + executionID, IdempotencyKey: "idem-create-" + executionID},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartExecution(context.Background(), StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: 1,
		Mutation: Mutation{RequestID: "request-start-" + executionID, IdempotencyKey: "idem-start-" + executionID},
	}); err != nil {
		t.Fatal(err)
	}
}

func createSession(t *testing.T, store *Store, sessionID string) {
	t.Helper()
	if _, err := store.CreateSession(context.Background(), CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: sessionID, ProviderKind: "codex",
		Mutation: Mutation{RequestID: "request-" + sessionID, IdempotencyKey: "idem-" + sessionID},
	}); err != nil {
		t.Fatal(err)
	}
}

func createTurn(t *testing.T, store *Store, sessionID, turnID string) {
	t.Helper()
	if _, err := store.CreateTurn(context.Background(), CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: sessionID, TurnID: turnID, InputText: "hello",
		Mutation: Mutation{RequestID: "request-" + turnID, IdempotencyKey: "idem-" + turnID},
	}); err != nil {
		t.Fatal(err)
	}
}

func createExecutionForTest(t *testing.T, store *Store, sessionID, turnID, executionID string) {
	t.Helper()
	if _, err := store.CreateExecution(context.Background(), CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: sessionID, TurnID: turnID, ExecutionID: executionID, Generation: 1,
		Mutation: Mutation{RequestID: "request-create-" + executionID, IdempotencyKey: "idem-create-" + executionID},
	}); err != nil {
		t.Fatal(err)
	}
}

func newTestStore(t *testing.T, clock Clock) *Store {
	t.Helper()
	store, err := NewStore(clock)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newTestClock() Clock {
	current := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return current }
}
