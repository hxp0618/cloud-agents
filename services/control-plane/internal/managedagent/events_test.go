package managedagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLifecycleEventProfileIsFrozenAndDetached(t *testing.T) {
	profile := ManagedAgentLifecycleEventProfile()
	if !profile.Valid() {
		t.Fatalf("event profile is not valid: %#v", profile)
	}
	if profile.ID != LifecycleEventProfileID || profile.Digest != lifecycleEventProfileDigest ||
		profile.Algorithm != lifecycleEventAlgorithm || profile.Fields != lifecycleEventFields ||
		profile.MaxPageSize != maxEventPageSize {
		t.Fatalf("unexpected event profile: %#v", profile)
	}
	operations := profile.AllowedOperations()
	if len(operations) != len(lifecycleEventOperations) {
		t.Fatalf("operation count = %d, want %d", len(operations), len(lifecycleEventOperations))
	}
	operations[0] = "caller-selected"
	if ManagedAgentLifecycleEventProfile().AllowedOperations()[0] == "caller-selected" || !profile.Valid() {
		t.Fatal("caller mutation changed the event authority")
	}
}

func TestLifecycleEventsAppendOnlyAndPagedByBoundCursor(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	otherScope := Scope{TenantID: "tenant-beta", ProjectID: "project-beta"}

	createSessionInScope(t, store, lifecycleTestScope, "session-events-a")
	createSessionInScope(t, store, otherScope, "session-events-b")
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-events-a", TurnID: "turn-events-a", InputText: "private prompt",
		Mutation: Mutation{RequestID: "request-turn-events-a", IdempotencyKey: "idem-turn-events-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateExecution(ctx, CreateExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-events-a", TurnID: "turn-events-a", ExecutionID: "execution-events-a", Generation: 1,
		Mutation: Mutation{RequestID: "request-execution-events-a", IdempotencyKey: "idem-execution-events-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartExecution(ctx, StartExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-events-a", TurnID: "turn-events-a", ExecutionID: "execution-events-a", Generation: 1,
		Mutation: Mutation{RequestID: "request-start-events-a", IdempotencyKey: "idem-start-events-a"},
	}); err != nil {
		t.Fatal(err)
	}
	resultDigest := "sha256:" + strings.Repeat("c", 64)
	if _, err := store.CompleteExecution(ctx, CompleteExecutionInput{
		Scope: lifecycleTestScope, SessionID: "session-events-a", TurnID: "turn-events-a", ExecutionID: "execution-events-a", Generation: 1,
		ResultDigest: resultDigest,
		Mutation:     Mutation{RequestID: "request-complete-events-a", IdempotencyKey: "idem-complete-events-a"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseSession(ctx, CloseSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-events-a",
		Mutation: Mutation{RequestID: "request-close-events-a", IdempotencyKey: "idem-close-events-a"},
	}); err != nil {
		t.Fatal(err)
	}

	first, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 3 || !first.HasMore {
		t.Fatalf("first page = %#v", first)
	}
	if got := []string{first.Events[0].Operation, first.Events[1].Operation, first.Events[2].Operation}; !equalStrings(got, []string{"session.create", "turn.create", "execution.create"}) {
		t.Fatalf("first operations = %#v", got)
	}
	if first.Events[0].Sequence != 1 || first.Events[1].Sequence != 3 || first.Events[2].Sequence != 4 {
		t.Fatalf("global sequence projection = %#v", first.Events)
	}
	if first.NextCursor.Sequence != 4 || first.NextCursor.EventID != first.Events[2].EventID {
		t.Fatalf("next cursor = %#v", first.NextCursor)
	}
	encoded, marshalErr := json.Marshal(first.Events)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), "private prompt") {
		t.Fatal("event projection retained raw turn input")
	}
	if first.Events[1].InputDigest == "" {
		t.Fatal("turn event did not retain the typed input digest")
	}
	if len(first.Events[2].Changes) != 1 || first.Events[2].Changes[0].To != string(ExecutionQueued) {
		t.Fatalf("execution create change = %#v", first.Events[2].Changes)
	}

	second, err := store.ReadEvents(ctx, lifecycleTestScope, first.NextCursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 3 || second.HasMore {
		t.Fatalf("second page = %#v", second)
	}
	if got := []string{second.Events[0].Operation, second.Events[1].Operation, second.Events[2].Operation}; !equalStrings(got, []string{"execution.start", "execution.complete", "session.close"}) {
		t.Fatalf("second operations = %#v", got)
	}
	if len(second.Events[0].Changes) != 2 || second.Events[0].Changes[0].Resource != ResourceTurn ||
		second.Events[0].Changes[1].Resource != ResourceExecution {
		t.Fatalf("start changes = %#v", second.Events[0].Changes)
	}
	if second.Events[1].ResultDigest != resultDigest || second.Events[1].Changes[1].To != string(ExecutionSucceeded) {
		t.Fatalf("complete event = %#v", second.Events[1])
	}

	// Returned slices and nested changes are detached from the store authority.
	first.Events[0].Scope.TenantID = "tampered"
	first.Events[0].Changes[0].To = "tampered"
	again, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if again.Events[0].Scope != lifecycleTestScope || again.Events[0].Changes[0].To != string(SessionActive) {
		t.Fatalf("event authority was mutated through a read result: %#v", again.Events[0])
	}
}

func TestLifecycleEventsRejectForeignAndMalformedCursors(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	createSessionInScope(t, store, lifecycleTestScope, "session-cursor-a")
	createSessionInScope(t, store, Scope{TenantID: "tenant-beta", ProjectID: "project-beta"}, "session-cursor-b")
	page, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	foreignPage, err := store.ReadEvents(ctx, Scope{TenantID: "tenant-beta", ProjectID: "project-beta"}, EventCursor{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		cursor EventCursor
		scope  Scope
	}{
		{name: "foreign scope", cursor: page.NextCursor, scope: Scope{TenantID: "tenant-beta", ProjectID: "project-beta"}},
		{name: "foreign event identity", cursor: foreignPage.NextCursor, scope: lifecycleTestScope},
		{name: "partial cursor", cursor: EventCursor{Sequence: 1}, scope: lifecycleTestScope},
		{name: "profile drift", cursor: func() EventCursor {
			value := page.NextCursor
			value.ProfileDigest = "sha256:" + strings.Repeat("0", 64)
			return value
		}(), scope: lifecycleTestScope},
		{name: "event drift", cursor: func() EventCursor {
			value := page.NextCursor
			value.EventID = "managed-agent-event-foreign"
			return value
		}(), scope: lifecycleTestScope},
		{name: "future sequence", cursor: func() EventCursor { value := page.NextCursor; value.Sequence = value.Sequence + 100; return value }(), scope: lifecycleTestScope},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.ReadEvents(ctx, test.scope, test.cursor, 1); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, limit := range []int{0, maxEventPageSize + 1} {
		if _, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("limit %d error = %v", limit, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ReadEvents(cancelled, lifecycleTestScope, EventCursor{}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v", err)
	}
}

func TestLifecycleEventsDoNotDuplicateOnIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	input := CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-event-replay", ProviderKind: "codex", EnvironmentLeaseID: "environment-1",
		Mutation: Mutation{RequestID: "request-event-replay", IdempotencyKey: "idem-event-replay"},
	}
	if _, err := store.CreateSession(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, input); err != nil {
		t.Fatal(err)
	}
	page, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, maxEventPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].Operation != "session.create" || page.Events[0].Sequence != 1 {
		t.Fatalf("replay events = %#v", page.Events)
	}
}

func TestLifecycleEventsAppendOnlyAfterSuccessfulMutation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, newTestClock())
	createSessionInScope(t, store, lifecycleTestScope, "session-event-fail-closed")
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "session-event-fail-closed", TurnID: "turn-event-fail-closed", InputText: "held",
		Mutation: Mutation{RequestID: "request-turn-event-fail-closed", IdempotencyKey: "idem-turn-event-fail-closed"},
	}); err != nil {
		t.Fatal(err)
	}

	// A rejected transition must not manufacture an event or advance the
	// sequence, even though the caller supplied a fresh idempotency key.
	if _, err := store.CloseSession(ctx, CloseSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-event-fail-closed",
		Mutation: Mutation{RequestID: "request-close-event-fail-closed", IdempotencyKey: "idem-close-event-fail-closed"},
	}); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("busy-session error = %v", err)
	}
	if _, err := store.CreateTurn(ctx, CreateTurnInput{
		Scope: lifecycleTestScope, SessionID: "missing-session", TurnID: "turn-event-fail-closed-missing", InputText: "rejected",
		Mutation: Mutation{RequestID: "request-turn-event-fail-closed-missing", IdempotencyKey: "idem-turn-event-fail-closed-missing"},
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing-session error = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.CreateSession(cancelled, CreateSessionInput{
		Scope: lifecycleTestScope, SessionID: "session-cancelled-event", ProviderKind: "codex", EnvironmentLeaseID: "environment-1",
		Mutation: Mutation{RequestID: "request-cancelled-event", IdempotencyKey: "idem-cancelled-event"},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mutation error = %v", err)
	}
	page, err := store.ReadEvents(ctx, lifecycleTestScope, EventCursor{}, maxEventPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].Sequence != 1 || page.Events[0].Operation != "session.create" ||
		page.Events[1].Sequence != 2 || page.Events[1].Operation != "turn.create" {
		t.Fatalf("rejected mutations changed event stream = %#v", page.Events)
	}
}

func createSessionInScope(t *testing.T, store *Store, scope Scope, sessionID string) {
	t.Helper()
	if _, err := store.CreateSession(context.Background(), CreateSessionInput{
		Scope: scope, SessionID: sessionID, ProviderKind: "codex", EnvironmentLeaseID: "environment-1",
		Mutation: Mutation{RequestID: "request-" + sessionID, IdempotencyKey: "idem-" + sessionID},
	}); err != nil {
		t.Fatal(err)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
