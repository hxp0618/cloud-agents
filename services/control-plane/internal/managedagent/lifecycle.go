// Package managedagent contains the transport-neutral Managed Agent lifecycle
// kernel. It is deliberately in-memory: durable PostgreSQL state, HTTP
// routing, Worker dispatch, and Provider execution are separate slices.
package managedagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// LifecycleProfileID is the versioned, transport-neutral contract boundary
	// implemented by this package. It is not a public HTTP API version.
	LifecycleProfileID = "cloud-agents/managed-agent-lifecycle/v1alpha1"
	maxIdentifierBytes = 128
	maxProviderBytes   = 64
	maxMutationBytes   = 256
	maxInputBytes      = 1 << 20
)

var (
	ErrInvalidInput        = errors.New("managed agent lifecycle input is invalid")
	ErrInvalidTransition   = errors.New("managed agent lifecycle transition is invalid")
	ErrAlreadyExists       = errors.New("managed agent lifecycle resource already exists")
	ErrNotFound            = errors.New("managed agent lifecycle resource was not found")
	ErrIdempotencyConflict = errors.New("managed agent lifecycle idempotency key conflicts")
	ErrSessionBusy         = errors.New("managed agent session has an active turn")
	ErrContractDrift       = errors.New("managed agent lifecycle contract profile drift")
	ErrInvalidClock        = errors.New("managed agent lifecycle clock is invalid")
	ErrNilContext          = errors.New("managed agent lifecycle context is nil")
)

// SessionState is the Control Plane state of a Managed Agent session.
type SessionState string

const (
	SessionActive SessionState = "active"
	SessionClosed SessionState = "closed"
)

// TurnState is the Control Plane state of a turn. A turn has at most one
// execution attempt in this first lifecycle kernel.
type TurnState string

const (
	TurnQueued      TurnState = "queued"
	TurnRunning     TurnState = "running"
	TurnCompleted   TurnState = "completed"
	TurnFailed      TurnState = "failed"
	TurnInterrupted TurnState = "interrupted"
	TurnCancelled   TurnState = "cancelled"
)

// ExecutionState is the Control Plane state of an execution attempt.
type ExecutionState string

const (
	ExecutionQueued    ExecutionState = "queued"
	ExecutionRunning   ExecutionState = "running"
	ExecutionSucceeded ExecutionState = "succeeded"
	ExecutionFailed    ExecutionState = "failed"
	ExecutionCancelled ExecutionState = "cancelled"
)

// ResourceKind identifies one state-machine row family.
type ResourceKind string

const (
	ResourceSession   ResourceKind = "session"
	ResourceTurn      ResourceKind = "turn"
	ResourceExecution ResourceKind = "execution"
)

// Transition is an immutable state-machine edge. Initial resource creation is
// represented by the resource's initial state, not by a caller-selectable
// transition.
type Transition struct {
	Resource ResourceKind
	From     string
	Event    string
	To       string
}

var lifecycleTransitions = [...]Transition{
	{Resource: ResourceSession, From: string(SessionActive), Event: "close", To: string(SessionClosed)},
	{Resource: ResourceTurn, From: string(TurnQueued), Event: "start_execution", To: string(TurnRunning)},
	{Resource: ResourceTurn, From: string(TurnRunning), Event: "execution_succeeded", To: string(TurnCompleted)},
	{Resource: ResourceTurn, From: string(TurnRunning), Event: "execution_failed", To: string(TurnFailed)},
	{Resource: ResourceTurn, From: string(TurnRunning), Event: "interrupt", To: string(TurnInterrupted)},
	{Resource: ResourceTurn, From: string(TurnRunning), Event: "cancel", To: string(TurnCancelled)},
	{Resource: ResourceExecution, From: string(ExecutionQueued), Event: "start", To: string(ExecutionRunning)},
	{Resource: ResourceExecution, From: string(ExecutionRunning), Event: "complete", To: string(ExecutionSucceeded)},
	{Resource: ResourceExecution, From: string(ExecutionRunning), Event: "fail", To: string(ExecutionFailed)},
	{Resource: ResourceExecution, From: string(ExecutionRunning), Event: "interrupt", To: string(ExecutionCancelled)},
	{Resource: ResourceExecution, From: string(ExecutionRunning), Event: "cancel", To: string(ExecutionCancelled)},
}

// LifecycleProfile is the small versioned authority consumed by the kernel.
// No caller-supplied state or digest can replace it.
type LifecycleProfile struct {
	ID                 string
	StateMachineDigest string
}

// ManagedAgentLifecycleProfile returns the immutable profile metadata.
func ManagedAgentLifecycleProfile() LifecycleProfile {
	return LifecycleProfile{ID: LifecycleProfileID, StateMachineDigest: lifecycleStateMachineDigest}
}

// Valid reports whether the checked-in profile still matches its transition
// table. Mutations fail closed if this invariant is broken.
func (profile LifecycleProfile) Valid() bool {
	return profile.ID == LifecycleProfileID && profile.StateMachineDigest != "" &&
		profile.StateMachineDigest == computeLifecycleStateMachineDigest()
}

// AllowedTransitions returns a detached copy of the frozen transition table.
func (profile LifecycleProfile) AllowedTransitions() []Transition {
	transitions := make([]Transition, len(lifecycleTransitions))
	copy(transitions, lifecycleTransitions[:])
	return transitions
}

func transitionAllowed(resource ResourceKind, from, event, to string) bool {
	for _, transition := range lifecycleTransitions {
		if transition.Resource == resource && transition.From == from && transition.Event == event && transition.To == to {
			return true
		}
	}
	return false
}

// Scope is the tenant/project isolation tuple required by every Managed Agent
// resource. It is never inferred from an ID or from a caller-selected state.
type Scope struct {
	TenantID  string
	ProjectID string
}

// Mutation identifies a caller retry. The request digest is derived from the
// typed input inside the kernel; callers cannot supply or override it.
type Mutation struct {
	RequestID      string
	IdempotencyKey string
	// executionBindingDigest is an internal coordinator binding. It is never
	// accepted from a transport caller; the local execution coordinator derives
	// it from the complete typed Worker attempt before the first mutation.
	executionBindingDigest string
}

type CreateSessionInput struct {
	Scope        Scope
	SessionID    string
	ProviderKind string
	Mutation     Mutation
}

type CloseSessionInput struct {
	Scope     Scope
	SessionID string
	Mutation  Mutation
}

type CreateTurnInput struct {
	Scope     Scope
	SessionID string
	TurnID    string
	InputText string
	Mutation  Mutation
}

type CreateExecutionInput struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	ExecutionID string
	Generation  uint64
	Mutation    Mutation
}

type StartExecutionInput struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	ExecutionID string
	Generation  uint64
	Mutation    Mutation
}

type CompleteExecutionInput struct {
	Scope        Scope
	SessionID    string
	TurnID       string
	ExecutionID  string
	Generation   uint64
	ResultDigest string
	Mutation     Mutation
}

type FailExecutionInput struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	ExecutionID string
	Generation  uint64
	ErrorCode   string
	Mutation    Mutation
}

type InterruptTurnInput struct {
	Scope             Scope
	SessionID         string
	TurnID            string
	TargetExecutionID string
	Generation        uint64
	Mutation          Mutation
}

type CancelTurnInput struct {
	Scope             Scope
	SessionID         string
	TurnID            string
	TargetExecutionID string
	Generation        uint64
	Mutation          Mutation
}

type SessionSnapshot struct {
	Scope        Scope
	SessionID    string
	ProviderKind string
	State        SessionState
	Version      uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type TurnSnapshot struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	InputDigest string
	ExecutionID string
	State       TurnState
	Version     uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ExecutionSnapshot struct {
	Scope        Scope
	SessionID    string
	TurnID       string
	ExecutionID  string
	Generation   uint64
	State        ExecutionState
	ResultDigest string
	ErrorCode    string
	Version      uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ExecutionTransitionResult returns both coupled rows after an execution
// transition. The values are detached snapshots and contain no mutable store
// authority.
type ExecutionTransitionResult struct {
	Turn      TurnSnapshot
	Execution ExecutionSnapshot
}

// Clock is injected solely for deterministic local tests. It has no external
// I/O contract.
type Clock func() time.Time

// Store is an in-memory, concurrency-safe lifecycle kernel. It intentionally
// has no PostgreSQL, HTTP, Worker, Provider, Workspace, Credential, or
// deployment dependency.
type Store struct {
	mu                sync.RWMutex
	clock             Clock
	sessions          map[sessionKey]sessionRecord
	turns             map[turnKey]turnRecord
	executions        map[executionKey]executionRecord
	mutations         map[mutationKey]mutationRecord
	events            []LifecycleEvent
	nextEventSequence uint64
}

func (store *Store) ready() bool {
	return store != nil && store.clock != nil && store.sessions != nil && store.turns != nil &&
		store.executions != nil && store.mutations != nil
}

// NewStore constructs a store with an explicit clock. A nil clock is rejected
// so time is never silently sourced from an unreviewed dependency.
func NewStore(clock Clock) (*Store, error) {
	if clock == nil {
		return nil, fmt.Errorf("%w: clock", ErrInvalidInput)
	}
	if !ManagedAgentLifecycleProfile().Valid() {
		return nil, ErrContractDrift
	}
	return &Store{
		clock:      clock,
		sessions:   make(map[sessionKey]sessionRecord),
		turns:      make(map[turnKey]turnRecord),
		executions: make(map[executionKey]executionRecord),
		mutations:  make(map[mutationKey]mutationRecord),
		events:     make([]LifecycleEvent, 0),
	}, nil
}

// NewInMemoryStore is the convenience constructor for local callers.
func NewInMemoryStore() *Store {
	store, err := NewStore(time.Now)
	if err != nil {
		panic(err)
	}
	return store
}

func (store *Store) CreateSession(ctx context.Context, input CreateSessionInput) (SessionSnapshot, error) {
	if !store.ready() {
		return SessionSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return SessionSnapshot{}, err
	}
	if err := input.Scope.validate(); err != nil {
		return SessionSnapshot{}, err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return SessionSnapshot{}, err
	}
	if err := validateIdentifier(input.ProviderKind, maxProviderBytes, "provider kind"); err != nil {
		return SessionSnapshot{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return SessionSnapshot{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, ProviderKind: input.ProviderKind,
	})
	record, err := store.mutate(ctx, input.Scope, "session.create", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		key := sessionKey{scope: input.Scope, id: input.SessionID}
		if _, exists := store.sessions[key]; exists {
			return mutationRecord{}, ErrAlreadyExists
		}
		snapshot := SessionSnapshot{
			Scope: input.Scope, SessionID: input.SessionID, ProviderKind: input.ProviderKind,
			State: SessionActive, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		store.sessions[key] = sessionRecord{snapshot: snapshot}
		return mutationRecord{kind: mutationSession, session: snapshot}, nil
	})
	return record.session, err
}

func (store *Store) CloseSession(ctx context.Context, input CloseSessionInput) (SessionSnapshot, error) {
	if !store.ready() {
		return SessionSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return SessionSnapshot{}, err
	}
	if err := input.Scope.validate(); err != nil {
		return SessionSnapshot{}, err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return SessionSnapshot{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return SessionSnapshot{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.close", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID,
	})
	record, err := store.mutate(ctx, input.Scope, "session.close", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		key := sessionKey{scope: input.Scope, id: input.SessionID}
		session, exists := store.sessions[key]
		if !exists {
			return mutationRecord{}, ErrNotFound
		}
		if session.snapshot.State != SessionActive || !transitionAllowed(ResourceSession, string(session.snapshot.State), "close", string(SessionClosed)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		for turnKey, turn := range store.turns {
			if turnKey.session == key && !turn.snapshot.terminal() {
				return mutationRecord{}, ErrSessionBusy
			}
		}
		session.snapshot.State = SessionClosed
		session.snapshot.Version++
		session.snapshot.UpdatedAt = now
		store.sessions[key] = session
		return mutationRecord{kind: mutationSession, session: session.snapshot}, nil
	})
	return record.session, err
}

func (store *Store) CreateTurn(ctx context.Context, input CreateTurnInput) (TurnSnapshot, error) {
	if !store.ready() {
		return TurnSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return TurnSnapshot{}, err
	}
	if err := input.Scope.validate(); err != nil {
		return TurnSnapshot{}, err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return TurnSnapshot{}, err
	}
	if err := validateIdentifier(input.TurnID, maxIdentifierBytes, "turn id"); err != nil {
		return TurnSnapshot{}, err
	}
	inputDigest, err := digestInput(input.InputText)
	if err != nil {
		return TurnSnapshot{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return TurnSnapshot{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, InputDigest: inputDigest,
	})
	record, err := store.mutate(ctx, input.Scope, "turn.create", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		sessionKey := sessionKey{scope: input.Scope, id: input.SessionID}
		session, exists := store.sessions[sessionKey]
		if !exists {
			return mutationRecord{}, ErrNotFound
		}
		if session.snapshot.State != SessionActive {
			return mutationRecord{}, ErrInvalidTransition
		}
		for turnKey, turn := range store.turns {
			if turnKey.session == sessionKey && !turn.snapshot.terminal() {
				return mutationRecord{}, ErrSessionBusy
			}
		}
		key := turnKey{session: sessionKey, id: input.TurnID}
		if _, exists := store.turns[key]; exists {
			return mutationRecord{}, ErrAlreadyExists
		}
		snapshot := TurnSnapshot{
			Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID,
			InputDigest: inputDigest, State: TurnQueued, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		store.turns[key] = turnRecord{snapshot: snapshot}
		return mutationRecord{kind: mutationTurn, turn: snapshot}, nil
	})
	return record.turn, err
}

func (store *Store) CreateExecution(ctx context.Context, input CreateExecutionInput) (ExecutionSnapshot, error) {
	if !store.ready() {
		return ExecutionSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionSnapshot{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return ExecutionSnapshot{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionSnapshot{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
	})
	record, err := store.mutate(ctx, input.Scope, "execution.create", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		sessionKey := sessionKey{scope: input.Scope, id: input.SessionID}
		session, exists := store.sessions[sessionKey]
		if !exists {
			return mutationRecord{}, ErrNotFound
		}
		if session.snapshot.State != SessionActive {
			return mutationRecord{}, ErrInvalidTransition
		}
		turnKey := turnKey{session: sessionKey, id: input.TurnID}
		turn, exists := store.turns[turnKey]
		if !exists {
			return mutationRecord{}, ErrNotFound
		}
		if turn.snapshot.State != TurnQueued {
			return mutationRecord{}, ErrInvalidTransition
		}
		if turn.snapshot.ExecutionID != "" {
			return mutationRecord{}, ErrAlreadyExists
		}
		key := executionKey{turn: turnKey, id: input.ExecutionID}
		if _, exists := store.executions[key]; exists {
			return mutationRecord{}, ErrAlreadyExists
		}
		snapshot := ExecutionSnapshot{
			Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID,
			ExecutionID: input.ExecutionID, Generation: input.Generation, State: ExecutionQueued,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		store.executions[key] = executionRecord{snapshot: snapshot}
		turn.snapshot.ExecutionID = input.ExecutionID
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		return mutationRecord{kind: mutationExecution, execution: snapshot}, nil
	})
	return record.execution, err
}

func (store *Store) StartExecution(ctx context.Context, input StartExecutionInput) (ExecutionTransitionResult, error) {
	if !store.ready() {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionTransitionResult{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.start", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
	})
	record, err := store.mutate(ctx, input.Scope, "execution.start", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		turn, execution, turnKey, executionKey, err := store.lookupExecution(input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
		if err != nil {
			return mutationRecord{}, err
		}
		if turn.snapshot.ExecutionID != input.ExecutionID || execution.snapshot.Generation != input.Generation ||
			turn.snapshot.State != TurnQueued || execution.snapshot.State != ExecutionQueued ||
			!transitionAllowed(ResourceTurn, string(TurnQueued), "start_execution", string(TurnRunning)) ||
			!transitionAllowed(ResourceExecution, string(ExecutionQueued), "start", string(ExecutionRunning)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		turn.snapshot.State = TurnRunning
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		execution.snapshot.State = ExecutionRunning
		execution.snapshot.Version++
		execution.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		store.executions[executionKey] = execution
		return mutationRecord{kind: mutationTransition, transition: ExecutionTransitionResult{Turn: turn.snapshot, Execution: execution.snapshot}}, nil
	})
	return record.transition, err
}

func (store *Store) CompleteExecution(ctx context.Context, input CompleteExecutionInput) (ExecutionTransitionResult, error) {
	if !store.ready() {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateDigest(input.ResultDigest, "result digest"); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionTransitionResult{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.complete", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID,
		Generation: input.Generation, ResultDigest: input.ResultDigest,
	})
	record, err := store.mutate(ctx, input.Scope, "execution.complete", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		turn, execution, turnKey, executionKey, err := store.lookupExecution(input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
		if err != nil {
			return mutationRecord{}, err
		}
		if turn.snapshot.ExecutionID != input.ExecutionID || execution.snapshot.Generation != input.Generation ||
			turn.snapshot.State != TurnRunning || execution.snapshot.State != ExecutionRunning ||
			!transitionAllowed(ResourceTurn, string(TurnRunning), "execution_succeeded", string(TurnCompleted)) ||
			!transitionAllowed(ResourceExecution, string(ExecutionRunning), "complete", string(ExecutionSucceeded)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		turn.snapshot.State = TurnCompleted
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		execution.snapshot.State = ExecutionSucceeded
		execution.snapshot.ResultDigest = input.ResultDigest
		execution.snapshot.Version++
		execution.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		store.executions[executionKey] = execution
		return mutationRecord{kind: mutationTransition, transition: ExecutionTransitionResult{Turn: turn.snapshot, Execution: execution.snapshot}}, nil
	})
	return record.transition, err
}

func (store *Store) FailExecution(ctx context.Context, input FailExecutionInput) (ExecutionTransitionResult, error) {
	if !store.ready() {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateErrorCode(input.ErrorCode); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionTransitionResult{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.fail", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID,
		Generation: input.Generation, ErrorCode: input.ErrorCode,
	})
	record, err := store.mutate(ctx, input.Scope, "execution.fail", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		turn, execution, turnKey, executionKey, err := store.lookupExecution(input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
		if err != nil {
			return mutationRecord{}, err
		}
		if turn.snapshot.ExecutionID != input.ExecutionID || execution.snapshot.Generation != input.Generation ||
			turn.snapshot.State != TurnRunning || execution.snapshot.State != ExecutionRunning ||
			!transitionAllowed(ResourceTurn, string(TurnRunning), "execution_failed", string(TurnFailed)) ||
			!transitionAllowed(ResourceExecution, string(ExecutionRunning), "fail", string(ExecutionFailed)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		turn.snapshot.State = TurnFailed
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		execution.snapshot.State = ExecutionFailed
		execution.snapshot.ErrorCode = input.ErrorCode
		execution.snapshot.Version++
		execution.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		store.executions[executionKey] = execution
		return mutationRecord{kind: mutationTransition, transition: ExecutionTransitionResult{Turn: turn.snapshot, Execution: execution.snapshot}}, nil
	})
	return record.transition, err
}

func (store *Store) InterruptTurn(ctx context.Context, input InterruptTurnInput) (ExecutionTransitionResult, error) {
	if !store.ready() {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID, input.Generation); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionTransitionResult{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.interrupt", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.TargetExecutionID,
		Generation: input.Generation, TargetExecutionID: input.TargetExecutionID,
	})
	record, err := store.mutate(ctx, input.Scope, "turn.interrupt", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		turn, execution, turnKey, executionKey, err := store.lookupExecution(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID)
		if err != nil {
			return mutationRecord{}, err
		}
		if turn.snapshot.ExecutionID != input.TargetExecutionID || execution.snapshot.Generation != input.Generation ||
			turn.snapshot.State != TurnRunning || execution.snapshot.State != ExecutionRunning ||
			!transitionAllowed(ResourceTurn, string(TurnRunning), "interrupt", string(TurnInterrupted)) ||
			!transitionAllowed(ResourceExecution, string(ExecutionRunning), "interrupt", string(ExecutionCancelled)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		turn.snapshot.State = TurnInterrupted
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		execution.snapshot.State = ExecutionCancelled
		execution.snapshot.ErrorCode = "interrupted"
		execution.snapshot.Version++
		execution.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		store.executions[executionKey] = execution
		return mutationRecord{kind: mutationTransition, transition: ExecutionTransitionResult{Turn: turn.snapshot, Execution: execution.snapshot}}, nil
	})
	return record.transition, err
}

// CancelTurn closes an active turn as a caller-requested cancellation. It is
// distinct from InterruptTurn so the terminal reason remains explicit in the
// lifecycle state machine.
func (store *Store) CancelTurn(ctx context.Context, input CancelTurnInput) (ExecutionTransitionResult, error) {
	if !store.ready() {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID, input.Generation); err != nil {
		return ExecutionTransitionResult{}, err
	}
	if err := input.Mutation.validate(); err != nil {
		return ExecutionTransitionResult{}, err
	}
	digest := digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.cancel", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.TargetExecutionID,
		Generation: input.Generation, TargetExecutionID: input.TargetExecutionID,
	})
	record, err := store.mutate(ctx, input.Scope, "turn.cancel", input.Mutation, digest, func(now time.Time) (mutationRecord, error) {
		turn, execution, turnKey, executionKey, err := store.lookupExecution(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID)
		if err != nil {
			return mutationRecord{}, err
		}
		if turn.snapshot.ExecutionID != input.TargetExecutionID || execution.snapshot.Generation != input.Generation ||
			turn.snapshot.State != TurnRunning || execution.snapshot.State != ExecutionRunning ||
			!transitionAllowed(ResourceTurn, string(TurnRunning), "cancel", string(TurnCancelled)) ||
			!transitionAllowed(ResourceExecution, string(ExecutionRunning), "cancel", string(ExecutionCancelled)) {
			return mutationRecord{}, ErrInvalidTransition
		}
		turn.snapshot.State = TurnCancelled
		turn.snapshot.Version++
		turn.snapshot.UpdatedAt = now
		execution.snapshot.State = ExecutionCancelled
		execution.snapshot.ErrorCode = "cancelled"
		execution.snapshot.Version++
		execution.snapshot.UpdatedAt = now
		store.turns[turnKey] = turn
		store.executions[executionKey] = execution
		return mutationRecord{kind: mutationTransition, transition: ExecutionTransitionResult{Turn: turn.snapshot, Execution: execution.snapshot}}, nil
	})
	return record.transition, err
}

func (store *Store) GetSession(ctx context.Context, scope Scope, sessionID string) (SessionSnapshot, error) {
	if !store.ready() {
		return SessionSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return SessionSnapshot{}, err
	}
	if err := scope.validate(); err != nil {
		return SessionSnapshot{}, err
	}
	if err := validateIdentifier(sessionID, maxIdentifierBytes, "session id"); err != nil {
		return SessionSnapshot{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, exists := store.sessions[sessionKey{scope: scope, id: sessionID}]
	if !exists {
		return SessionSnapshot{}, ErrNotFound
	}
	return snapshot.snapshot, nil
}

func (store *Store) GetTurn(ctx context.Context, scope Scope, sessionID, turnID string) (TurnSnapshot, error) {
	if !store.ready() {
		return TurnSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return TurnSnapshot{}, err
	}
	if err := scope.validate(); err != nil {
		return TurnSnapshot{}, err
	}
	if err := validateIdentifier(sessionID, maxIdentifierBytes, "session id"); err != nil {
		return TurnSnapshot{}, err
	}
	if err := validateIdentifier(turnID, maxIdentifierBytes, "turn id"); err != nil {
		return TurnSnapshot{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, exists := store.turns[turnKey{session: sessionKey{scope: scope, id: sessionID}, id: turnID}]
	if !exists {
		return TurnSnapshot{}, ErrNotFound
	}
	return snapshot.snapshot, nil
}

func (store *Store) GetExecution(ctx context.Context, scope Scope, sessionID, turnID, executionID string) (ExecutionSnapshot, error) {
	if !store.ready() {
		return ExecutionSnapshot{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return ExecutionSnapshot{}, err
	}
	if err := validateExecutionPath(scope, sessionID, turnID, executionID); err != nil {
		return ExecutionSnapshot{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	snapshot, exists := store.executions[executionKey{turn: turnKey{session: sessionKey{scope: scope, id: sessionID}, id: turnID}, id: executionID}]
	if !exists {
		return ExecutionSnapshot{}, ErrNotFound
	}
	return snapshot.snapshot, nil
}

func (store *Store) mutate(
	ctx context.Context,
	scope Scope,
	operation string,
	mutation Mutation,
	digest string,
	apply func(time.Time) (mutationRecord, error),
) (mutationRecord, error) {
	if !store.ready() {
		return mutationRecord{}, ErrInvalidInput
	}
	if err := validateContext(ctx); err != nil {
		return mutationRecord{}, err
	}
	if !ManagedAgentLifecycleProfile().Valid() {
		return mutationRecord{}, ErrContractDrift
	}
	if !ManagedAgentLifecycleEventProfile().Valid() || !lifecycleEventOperationKnown(operation) {
		return mutationRecord{}, ErrContractDrift
	}
	key := mutationKey{scope: scope, operation: operation, idempotencyKey: mutation.IdempotencyKey}
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, exists := store.mutations[key]; exists {
		if existing.digest != digest {
			return mutationRecord{}, ErrIdempotencyConflict
		}
		return existing, nil
	}
	if err := contextErr(ctx); err != nil {
		return mutationRecord{}, err
	}
	now := store.clock().UTC()
	if now.IsZero() {
		return mutationRecord{}, ErrInvalidClock
	}
	record, err := apply(now)
	if err != nil {
		return mutationRecord{}, err
	}
	record.digest = digest
	store.nextEventSequence++
	store.events = append(store.events, buildLifecycleEvent(scope, operation, digest, record, store.nextEventSequence, now))
	store.mutations[key] = record
	return record, nil
}

func (store *Store) lookupExecution(scope Scope, sessionID, turnID, executionID string) (turnRecord, executionRecord, turnKey, executionKey, error) {
	sessionKey := sessionKey{scope: scope, id: sessionID}
	turnResourceKey := turnKey{session: sessionKey, id: turnID}
	turn, exists := store.turns[turnResourceKey]
	if !exists {
		return turnRecord{}, executionRecord{}, turnKey{}, executionKey{}, ErrNotFound
	}
	executionResourceKey := executionKey{turn: turnResourceKey, id: executionID}
	execution, exists := store.executions[executionResourceKey]
	if !exists {
		return turnRecord{}, executionRecord{}, turnKey{}, executionKey{}, ErrNotFound
	}
	return turn, execution, turnResourceKey, executionResourceKey, nil
}

func (scope Scope) validate() error {
	if err := validateIdentifier(scope.TenantID, maxIdentifierBytes, "tenant id"); err != nil {
		return err
	}
	return validateIdentifier(scope.ProjectID, maxIdentifierBytes, "project id")
}

func (mutation Mutation) validate() error {
	if err := validateToken(mutation.RequestID, maxMutationBytes, "request id"); err != nil {
		return err
	}
	if err := validateToken(mutation.IdempotencyKey, maxMutationBytes, "idempotency key"); err != nil {
		return err
	}
	if mutation.executionBindingDigest != "" {
		return validateDigest(mutation.executionBindingDigest, "execution binding digest")
	}
	return nil
}

// SessionCreateMutationDigest returns the canonical request digest used by
// both the in-memory kernel and durable Session writers.
func SessionCreateMutationDigest(input CreateSessionInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.ProviderKind, maxProviderBytes, "provider kind"); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, ProviderKind: input.ProviderKind,
	}), nil
}

// SessionCloseMutationDigest returns the canonical request digest used by
// both the in-memory kernel and durable Session writers.
func SessionCloseMutationDigest(input CloseSessionInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "session.close", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID,
	}), nil
}

// TurnCreateMutationDigest returns the canonical request digest used by the
// in-memory kernel and durable Turn writers.
func TurnCreateMutationDigest(input CreateTurnInput) (string, error) {
	if err := input.Scope.validate(); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.SessionID, maxIdentifierBytes, "session id"); err != nil {
		return "", err
	}
	if err := validateIdentifier(input.TurnID, maxIdentifierBytes, "turn id"); err != nil {
		return "", err
	}
	inputDigest, err := TurnInputDigest(input.InputText)
	if err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, InputDigest: inputDigest,
	}), nil
}

// TurnInputDigest returns the bounded content digest persisted with a Turn.
func TurnInputDigest(inputText string) (string, error) {
	return digestInput(inputText)
}

// ExecutionCreateMutationDigest returns the canonical digest used by the
// durable Execution create writer.
func ExecutionCreateMutationDigest(input CreateExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.create", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
	}), nil
}

// ExecutionStartMutationDigest returns the canonical digest used by the
// durable Execution start writer.
func ExecutionStartMutationDigest(input StartExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.start", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
	}), nil
}

// ExecutionCompleteMutationDigest returns the canonical digest used by the
// durable successful Execution settlement writer.
func ExecutionCompleteMutationDigest(input CompleteExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := validateDigest(input.ResultDigest, "result digest"); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.complete", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
		ResultDigest: input.ResultDigest,
	}), nil
}

// ExecutionFailMutationDigest returns the canonical digest used by the
// durable failed Execution settlement writer.
func ExecutionFailMutationDigest(input FailExecutionInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := validateErrorCode(input.ErrorCode); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "execution.fail", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation,
		ErrorCode: input.ErrorCode,
	}), nil
}

// TurnCancelMutationDigest returns the canonical digest used by the durable
// caller-requested cancellation writer.
func TurnCancelMutationDigest(input CancelTurnInput) (string, error) {
	if err := validateExecutionInput(input.Scope, input.SessionID, input.TurnID, input.TargetExecutionID, input.Generation); err != nil {
		return "", err
	}
	if err := input.Mutation.validate(); err != nil {
		return "", err
	}
	return digestMutationWithBinding(input.Mutation, mutationDigestInput{
		Operation: "turn.cancel", TenantID: input.Scope.TenantID, ProjectID: input.Scope.ProjectID,
		SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.TargetExecutionID,
		Generation: input.Generation, TargetExecutionID: input.TargetExecutionID,
	}), nil
}

func validateExecutionInput(scope Scope, sessionID, turnID, executionID string, generation uint64) error {
	if err := validateExecutionPath(scope, sessionID, turnID, executionID); err != nil {
		return err
	}
	if generation == 0 {
		return fmt.Errorf("%w: generation", ErrInvalidInput)
	}
	return nil
}

func validateExecutionPath(scope Scope, sessionID, turnID, executionID string) error {
	if err := scope.validate(); err != nil {
		return err
	}
	if err := validateIdentifier(sessionID, maxIdentifierBytes, "session id"); err != nil {
		return err
	}
	if err := validateIdentifier(turnID, maxIdentifierBytes, "turn id"); err != nil {
		return err
	}
	return validateIdentifier(executionID, maxIdentifierBytes, "execution id")
}

func validateIdentifier(value string, maximum int, field string) error {
	if len(value) == 0 || len(value) > maximum || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		alphaNumeric := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9'
		if index == 0 || index == len(value)-1 {
			if !alphaNumeric {
				return fmt.Errorf("%w: %s", ErrInvalidInput, field)
			}
			continue
		}
		if !alphaNumeric && character != '-' && character != '_' && character != '.' && character != '~' {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func validateToken(value string, maximum int, field string) error {
	if len(value) == 0 || len(value) > maximum || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func digestInput(input string) (string, error) {
	if len(input) == 0 || len(input) > maxInputBytes || !utf8.ValidString(input) {
		return "", fmt.Errorf("%w: input text", ErrInvalidInput)
	}
	digest := sha256.Sum256([]byte(input))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateDigest(value, field string) error {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value[len("sha256:"):] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func validateErrorCode(value string) error {
	if len(value) == 0 || len(value) > 64 {
		return fmt.Errorf("%w: error code", ErrInvalidInput)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return fmt.Errorf("%w: error code", ErrInvalidInput)
		}
	}
	return nil
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return contextErr(ctx)
}

func contextErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (snapshot TurnSnapshot) terminal() bool {
	return snapshot.State == TurnCompleted || snapshot.State == TurnFailed || snapshot.State == TurnInterrupted || snapshot.State == TurnCancelled
}

type sessionKey struct {
	scope Scope
	id    string
}

type turnKey struct {
	session sessionKey
	id      string
}

type executionKey struct {
	turn turnKey
	id   string
}

type mutationKey struct {
	scope          Scope
	operation      string
	idempotencyKey string
}

type sessionRecord struct{ snapshot SessionSnapshot }
type turnRecord struct{ snapshot TurnSnapshot }
type executionRecord struct{ snapshot ExecutionSnapshot }

type mutationResultKind uint8

const (
	mutationSession mutationResultKind = iota + 1
	mutationTurn
	mutationExecution
	mutationTransition
)

type mutationRecord struct {
	digest     string
	kind       mutationResultKind
	session    SessionSnapshot
	turn       TurnSnapshot
	execution  ExecutionSnapshot
	transition ExecutionTransitionResult
}

type mutationDigestInput struct {
	Operation              string `json:"operation"`
	TenantID               string `json:"tenant_id"`
	ProjectID              string `json:"project_id"`
	SessionID              string `json:"session_id,omitempty"`
	TurnID                 string `json:"turn_id,omitempty"`
	ExecutionID            string `json:"execution_id,omitempty"`
	TargetExecutionID      string `json:"target_execution_id,omitempty"`
	ProviderKind           string `json:"provider_kind,omitempty"`
	Generation             uint64 `json:"generation,omitempty"`
	InputDigest            string `json:"input_digest,omitempty"`
	ResultDigest           string `json:"result_digest,omitempty"`
	ErrorCode              string `json:"error_code,omitempty"`
	ExecutionBindingDigest string `json:"execution_binding_digest,omitempty"`
}

func digestMutation(input mutationDigestInput) string {
	encoded, _ := json.Marshal(input)
	hash := sha256.Sum256(append([]byte(LifecycleProfileID+"/mutation\x00"), encoded...))
	return "sha256:" + hex.EncodeToString(hash[:])
}

func digestMutationWithBinding(mutation Mutation, input mutationDigestInput) string {
	input.ExecutionBindingDigest = mutation.executionBindingDigest
	return digestMutation(input)
}
