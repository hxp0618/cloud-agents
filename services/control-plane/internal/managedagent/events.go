package managedagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	// LifecycleEventProfileID identifies the local, transport-neutral event
	// projection. It is not a public HTTP or durable-storage version.
	LifecycleEventProfileID     = "cloud-agents/managed-agent-events/v1alpha1"
	lifecycleEventProfileDigest = "sha256:e38816e4df5b8aff6338537283f7eb7f9757aef9333b9a4e464dcec365a913b4"
	lifecycleEventAlgorithm     = "global-sequence-scope-filter-v1"
	lifecycleEventFields        = "event_id|sequence|scope|operation|resource|session_id|turn_id|execution_id|generation|occurred_at|mutation_digest|input_digest|result_digest|error_code|changes(resource,from,to,version)"
	maxEventPageSize            = 64
)

var lifecycleEventOperations = [...]string{
	"session.create",
	"session.close",
	"turn.create",
	"execution.create",
	"execution.start",
	"execution.complete",
	"execution.fail",
	"turn.interrupt",
	"turn.cancel",
}

// LifecycleEventProfile is the immutable local authority for event ordering
// and cursor binding. It deliberately says nothing about persistence or
// transport delivery.
type LifecycleEventProfile struct {
	ID          string
	Digest      string
	Algorithm   string
	Fields      string
	MaxPageSize int
}

// ManagedAgentLifecycleEventProfile returns the checked-in event profile.
func ManagedAgentLifecycleEventProfile() LifecycleEventProfile {
	return LifecycleEventProfile{
		ID:          LifecycleEventProfileID,
		Digest:      lifecycleEventProfileDigest,
		Algorithm:   lifecycleEventAlgorithm,
		Fields:      lifecycleEventFields,
		MaxPageSize: maxEventPageSize,
	}
}

// Valid reports whether the event authority still matches its frozen
// operation vocabulary and cursor algorithm.
func (profile LifecycleEventProfile) Valid() bool {
	return profile.ID == LifecycleEventProfileID &&
		profile.Digest == lifecycleEventProfileDigest &&
		profile.Algorithm == lifecycleEventAlgorithm &&
		profile.Fields == lifecycleEventFields &&
		profile.MaxPageSize == maxEventPageSize &&
		profile.Digest == computeLifecycleEventProfileDigest()
}

// AllowedOperations returns a detached copy of the event operation set.
func (profile LifecycleEventProfile) AllowedOperations() []string {
	operations := make([]string, len(lifecycleEventOperations))
	copy(operations, lifecycleEventOperations[:])
	return operations
}

func computeLifecycleEventProfileDigest() string {
	var builder strings.Builder
	builder.WriteString(LifecycleEventProfileID)
	builder.WriteByte(0)
	builder.WriteString(lifecycleEventAlgorithm)
	builder.WriteByte(0)
	builder.WriteString(lifecycleEventFields)
	builder.WriteByte(0)
	for _, operation := range lifecycleEventOperations {
		builder.WriteString(operation)
		builder.WriteByte(0)
	}
	return digestBytes([]byte(builder.String()))
}

// LifecycleStateChange records one frozen state-machine edge in an event.
// Version is the post-mutation resource version.
type LifecycleStateChange struct {
	Resource ResourceKind
	From     string
	To       string
	Version  uint64
}

// LifecycleEvent is an immutable, in-memory projection of one successful
// lifecycle mutation. It contains typed IDs and digests only; raw turn input,
// provider credentials, and external response bodies are never retained.
type LifecycleEvent struct {
	EventID        string
	Sequence       uint64
	Scope          Scope
	Operation      string
	Resource       ResourceKind
	SessionID      string
	TurnID         string
	ExecutionID    string
	Generation     uint64
	OccurredAt     time.Time
	MutationDigest string
	InputDigest    string
	ResultDigest   string
	ErrorCode      string
	Changes        []LifecycleStateChange
}

// EventCursor is bound to one scope, event profile, and exact event ID. The
// zero value is the only cursor accepted for a stream's beginning.
type EventCursor struct {
	Scope         Scope
	Sequence      uint64
	EventID       string
	ProfileID     string
	ProfileDigest string
}

// EventPage is a detached read result. NextCursor can be reused only with the
// same scope and profile; it never grants access to another tenant/project.
type EventPage struct {
	Events     []LifecycleEvent
	NextCursor EventCursor
	HasMore    bool
}

// ReadEvents returns at most limit events for scope after the exact cursor.
// This is an ephemeral local read seam: it does not open a database, publish a
// stream, or invoke an HTTP/Worker/Provider actuator.
func (store *Store) ReadEvents(ctx context.Context, scope Scope, after EventCursor, limit int) (EventPage, error) {
	if !store.ready() {
		return EventPage{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return EventPage{}, err
	}
	if err := scope.validate(); err != nil {
		return EventPage{}, err
	}
	profile := ManagedAgentLifecycleEventProfile()
	if !profile.Valid() {
		return EventPage{}, ErrContractDrift
	}
	if limit < 1 || limit > profile.MaxPageSize {
		return EventPage{}, fmt.Errorf("%w: event page size", ErrInvalidInput)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if err := validateEventCursor(scope, after, store.events, store.nextEventSequence, profile); err != nil {
		return EventPage{}, err
	}
	if err := contextErr(ctx); err != nil {
		return EventPage{}, err
	}

	events := make([]LifecycleEvent, 0, limit)
	lastSequence := after.Sequence
	for _, event := range store.events {
		if event.Sequence <= after.Sequence || event.Scope != scope {
			continue
		}
		events = append(events, detachLifecycleEvent(event))
		lastSequence = event.Sequence
		if len(events) == limit {
			break
		}
	}
	hasMore := false
	for _, event := range store.events {
		if event.Sequence > lastSequence && event.Scope == scope {
			hasMore = true
			break
		}
	}
	next := after
	if len(events) > 0 {
		last := events[len(events)-1]
		next = EventCursor{
			Scope:         scope,
			Sequence:      last.Sequence,
			EventID:       last.EventID,
			ProfileID:     profile.ID,
			ProfileDigest: profile.Digest,
		}
	}
	return EventPage{Events: events, NextCursor: next, HasMore: hasMore}, nil
}

func validateEventCursor(
	scope Scope,
	cursor EventCursor,
	events []LifecycleEvent,
	latest uint64,
	profile LifecycleEventProfile,
) error {
	if cursor == (EventCursor{}) {
		return nil
	}
	if cursor.Sequence == 0 || cursor.EventID == "" || cursor.ProfileID == "" || cursor.ProfileDigest == "" ||
		cursor.Scope == (Scope{}) || cursor.Scope != scope ||
		cursor.ProfileID != profile.ID || cursor.ProfileDigest != profile.Digest ||
		cursor.Sequence > latest {
		return fmt.Errorf("%w: event cursor", ErrInvalidInput)
	}
	for _, event := range events {
		if event.Sequence == cursor.Sequence {
			if event.EventID != cursor.EventID || event.Scope != scope {
				return fmt.Errorf("%w: event cursor identity", ErrInvalidInput)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: event cursor sequence", ErrInvalidInput)
}

func lifecycleEventOperationKnown(operation string) bool {
	for _, known := range lifecycleEventOperations {
		if operation == known {
			return true
		}
	}
	return false
}

// buildLifecycleEvent is called while Store.mu is held, after a successful
// mutation. All callers use the fixed operation vocabulary above, so this
// function has no caller-controlled event shape.
func buildLifecycleEvent(scope Scope, operation, mutationDigest string, record mutationRecord, sequence uint64, occurredAt time.Time) LifecycleEvent {
	event := LifecycleEvent{
		EventID:        fmt.Sprintf("managed-agent-event-%020d", sequence),
		Sequence:       sequence,
		Scope:          scope,
		Operation:      operation,
		OccurredAt:     occurredAt.UTC(),
		MutationDigest: mutationDigest,
	}
	switch operation {
	case "session.create":
		event.resourceFromSession(record.session, "", string(record.session.State), record.session.Version)
	case "session.close":
		event.resourceFromSession(record.session, string(SessionActive), string(record.session.State), record.session.Version)
	case "turn.create":
		event.resourceFromTurn(record.turn, "", string(record.turn.State), record.turn.Version)
	case "execution.create":
		event.resourceFromExecution(record.execution, "", string(record.execution.State), record.execution.Version)
	case "execution.start":
		event.resourceFromTransition(record.transition, []LifecycleStateChange{
			{Resource: ResourceTurn, From: string(TurnQueued), To: string(TurnRunning), Version: record.transition.Turn.Version},
			{Resource: ResourceExecution, From: string(ExecutionQueued), To: string(ExecutionRunning), Version: record.transition.Execution.Version},
		})
	case "execution.complete":
		event.resourceFromTransition(record.transition, []LifecycleStateChange{
			{Resource: ResourceTurn, From: string(TurnRunning), To: string(TurnCompleted), Version: record.transition.Turn.Version},
			{Resource: ResourceExecution, From: string(ExecutionRunning), To: string(ExecutionSucceeded), Version: record.transition.Execution.Version},
		})
		event.ResultDigest = record.transition.Execution.ResultDigest
	case "execution.fail":
		event.resourceFromTransition(record.transition, []LifecycleStateChange{
			{Resource: ResourceTurn, From: string(TurnRunning), To: string(TurnFailed), Version: record.transition.Turn.Version},
			{Resource: ResourceExecution, From: string(ExecutionRunning), To: string(ExecutionFailed), Version: record.transition.Execution.Version},
		})
		event.ErrorCode = record.transition.Execution.ErrorCode
	case "turn.interrupt":
		event.resourceFromTransition(record.transition, []LifecycleStateChange{
			{Resource: ResourceTurn, From: string(TurnRunning), To: string(TurnInterrupted), Version: record.transition.Turn.Version},
			{Resource: ResourceExecution, From: string(ExecutionRunning), To: string(ExecutionCancelled), Version: record.transition.Execution.Version},
		})
		event.ErrorCode = record.transition.Execution.ErrorCode
	case "turn.cancel":
		event.resourceFromTransition(record.transition, []LifecycleStateChange{
			{Resource: ResourceTurn, From: string(TurnRunning), To: string(TurnCancelled), Version: record.transition.Turn.Version},
			{Resource: ResourceExecution, From: string(ExecutionRunning), To: string(ExecutionCancelled), Version: record.transition.Execution.Version},
		})
		event.ErrorCode = record.transition.Execution.ErrorCode
	}
	if record.kind == mutationTurn {
		event.InputDigest = record.turn.InputDigest
	}
	return event
}

func (event *LifecycleEvent) resourceFromSession(snapshot SessionSnapshot, from, to string, version uint64) {
	event.Resource = ResourceSession
	event.SessionID = snapshot.SessionID
	event.Changes = []LifecycleStateChange{{Resource: ResourceSession, From: from, To: to, Version: version}}
}

func (event *LifecycleEvent) resourceFromTurn(snapshot TurnSnapshot, from, to string, version uint64) {
	event.Resource = ResourceTurn
	event.SessionID = snapshot.SessionID
	event.TurnID = snapshot.TurnID
	event.Changes = []LifecycleStateChange{{Resource: ResourceTurn, From: from, To: to, Version: version}}
	event.InputDigest = snapshot.InputDigest
	if snapshot.ExecutionID != "" {
		event.ExecutionID = snapshot.ExecutionID
	}
}

func (event *LifecycleEvent) resourceFromExecution(snapshot ExecutionSnapshot, from, to string, version uint64) {
	event.Resource = ResourceExecution
	event.SessionID = snapshot.SessionID
	event.TurnID = snapshot.TurnID
	event.ExecutionID = snapshot.ExecutionID
	event.Generation = snapshot.Generation
	event.Changes = []LifecycleStateChange{{Resource: ResourceExecution, From: from, To: to, Version: version}}
	event.ResultDigest = snapshot.ResultDigest
	event.ErrorCode = snapshot.ErrorCode
}

func (event *LifecycleEvent) resourceFromTransition(result ExecutionTransitionResult, changes []LifecycleStateChange) {
	event.Resource = ResourceExecution
	event.SessionID = result.Execution.SessionID
	event.TurnID = result.Execution.TurnID
	event.ExecutionID = result.Execution.ExecutionID
	event.Generation = result.Execution.Generation
	event.Changes = changes
	event.ResultDigest = result.Execution.ResultDigest
	event.ErrorCode = result.Execution.ErrorCode
}

func detachLifecycleEvent(event LifecycleEvent) LifecycleEvent {
	event.Changes = append([]LifecycleStateChange(nil), event.Changes...)
	return event
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
