package controlplane

import (
	"context"
	"time"

	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
)

// ManagedAgentService is the public, in-memory Control Plane lifecycle seam.
// It owns no transport, database, Worker, Provider, or deployment effect.
type ManagedAgentService struct {
	store *internalmanagedagent.Store
}

// Public lifecycle types are aliases so callers use the same validated input
// and detached result vocabulary as the kernel without importing internal code.
type (
	Scope                     = internalmanagedagent.Scope
	Mutation                  = internalmanagedagent.Mutation
	CreateSessionInput        = internalmanagedagent.CreateSessionInput
	CloseSessionInput         = internalmanagedagent.CloseSessionInput
	CreateTurnInput           = internalmanagedagent.CreateTurnInput
	CreateExecutionInput      = internalmanagedagent.CreateExecutionInput
	StartExecutionInput       = internalmanagedagent.StartExecutionInput
	CompleteExecutionInput    = internalmanagedagent.CompleteExecutionInput
	FailExecutionInput        = internalmanagedagent.FailExecutionInput
	InterruptTurnInput        = internalmanagedagent.InterruptTurnInput
	CancelTurnInput           = internalmanagedagent.CancelTurnInput
	SessionSnapshot           = internalmanagedagent.SessionSnapshot
	TurnSnapshot              = internalmanagedagent.TurnSnapshot
	ExecutionSnapshot         = internalmanagedagent.ExecutionSnapshot
	ExecutionTransitionResult = internalmanagedagent.ExecutionTransitionResult
	EventCursor               = internalmanagedagent.EventCursor
	EventPage                 = internalmanagedagent.EventPage
	LifecycleEvent            = internalmanagedagent.LifecycleEvent
	LifecycleStateChange      = internalmanagedagent.LifecycleStateChange
	SessionState              = internalmanagedagent.SessionState
	TurnState                 = internalmanagedagent.TurnState
	ExecutionState            = internalmanagedagent.ExecutionState
	ResourceKind              = internalmanagedagent.ResourceKind
)

const (
	SessionActive = internalmanagedagent.SessionActive
	SessionClosed = internalmanagedagent.SessionClosed

	TurnQueued      = internalmanagedagent.TurnQueued
	TurnRunning     = internalmanagedagent.TurnRunning
	TurnCompleted   = internalmanagedagent.TurnCompleted
	TurnFailed      = internalmanagedagent.TurnFailed
	TurnInterrupted = internalmanagedagent.TurnInterrupted
	TurnCancelled   = internalmanagedagent.TurnCancelled

	ExecutionQueued    = internalmanagedagent.ExecutionQueued
	ExecutionRunning   = internalmanagedagent.ExecutionRunning
	ExecutionSucceeded = internalmanagedagent.ExecutionSucceeded
	ExecutionFailed    = internalmanagedagent.ExecutionFailed
	ExecutionCancelled = internalmanagedagent.ExecutionCancelled

	ResourceSession   = internalmanagedagent.ResourceSession
	ResourceTurn      = internalmanagedagent.ResourceTurn
	ResourceExecution = internalmanagedagent.ResourceExecution
)

var (
	ErrInvalidInput        = internalmanagedagent.ErrInvalidInput
	ErrInvalidTransition   = internalmanagedagent.ErrInvalidTransition
	ErrAlreadyExists       = internalmanagedagent.ErrAlreadyExists
	ErrNotFound            = internalmanagedagent.ErrNotFound
	ErrIdempotencyConflict = internalmanagedagent.ErrIdempotencyConflict
	ErrSessionBusy         = internalmanagedagent.ErrSessionBusy
	ErrContractDrift       = internalmanagedagent.ErrContractDrift
	ErrInvalidClock        = internalmanagedagent.ErrInvalidClock
	ErrNilContext          = internalmanagedagent.ErrNilContext
)

// NewManagedAgentService constructs the default in-memory lifecycle service.
// Durable PostgreSQL persistence is a separate product dependency.
func NewManagedAgentService() (*ManagedAgentService, error) {
	store, err := internalmanagedagent.NewStore(time.Now)
	if err != nil {
		return nil, err
	}
	return &ManagedAgentService{store: store}, nil
}

func (service *ManagedAgentService) CreateSession(ctx context.Context, input CreateSessionInput) (SessionSnapshot, error) {
	if service == nil {
		return SessionSnapshot{}, ErrInvalidInput
	}
	return service.store.CreateSession(ctx, input)
}

func (service *ManagedAgentService) CloseSession(ctx context.Context, input CloseSessionInput) (SessionSnapshot, error) {
	if service == nil {
		return SessionSnapshot{}, ErrInvalidInput
	}
	return service.store.CloseSession(ctx, input)
}

func (service *ManagedAgentService) CreateTurn(ctx context.Context, input CreateTurnInput) (TurnSnapshot, error) {
	if service == nil {
		return TurnSnapshot{}, ErrInvalidInput
	}
	return service.store.CreateTurn(ctx, input)
}

func (service *ManagedAgentService) CreateExecution(ctx context.Context, input CreateExecutionInput) (ExecutionSnapshot, error) {
	if service == nil {
		return ExecutionSnapshot{}, ErrInvalidInput
	}
	return service.store.CreateExecution(ctx, input)
}

func (service *ManagedAgentService) StartExecution(ctx context.Context, input StartExecutionInput) (ExecutionTransitionResult, error) {
	if service == nil {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	return service.store.StartExecution(ctx, input)
}

func (service *ManagedAgentService) CompleteExecution(ctx context.Context, input CompleteExecutionInput) (ExecutionTransitionResult, error) {
	if service == nil {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	return service.store.CompleteExecution(ctx, input)
}

func (service *ManagedAgentService) FailExecution(ctx context.Context, input FailExecutionInput) (ExecutionTransitionResult, error) {
	if service == nil {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	return service.store.FailExecution(ctx, input)
}

func (service *ManagedAgentService) InterruptTurn(ctx context.Context, input InterruptTurnInput) (ExecutionTransitionResult, error) {
	if service == nil {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	return service.store.InterruptTurn(ctx, input)
}

func (service *ManagedAgentService) CancelTurn(ctx context.Context, input CancelTurnInput) (ExecutionTransitionResult, error) {
	if service == nil {
		return ExecutionTransitionResult{}, ErrInvalidInput
	}
	return service.store.CancelTurn(ctx, input)
}

func (service *ManagedAgentService) GetSession(ctx context.Context, scope Scope, sessionID string) (SessionSnapshot, error) {
	if service == nil {
		return SessionSnapshot{}, ErrInvalidInput
	}
	return service.store.GetSession(ctx, scope, sessionID)
}

func (service *ManagedAgentService) GetTurn(ctx context.Context, scope Scope, sessionID, turnID string) (TurnSnapshot, error) {
	if service == nil {
		return TurnSnapshot{}, ErrInvalidInput
	}
	return service.store.GetTurn(ctx, scope, sessionID, turnID)
}

func (service *ManagedAgentService) GetExecution(ctx context.Context, scope Scope, sessionID, turnID, executionID string) (ExecutionSnapshot, error) {
	if service == nil {
		return ExecutionSnapshot{}, ErrInvalidInput
	}
	return service.store.GetExecution(ctx, scope, sessionID, turnID, executionID)
}

func (service *ManagedAgentService) ReadEvents(ctx context.Context, scope Scope, after EventCursor, limit int) (EventPage, error) {
	if service == nil {
		return EventPage{}, ErrInvalidInput
	}
	return service.store.ReadEvents(ctx, scope, after, limit)
}
