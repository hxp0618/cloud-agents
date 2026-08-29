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

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/worker/runtime"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

// DurableRuntimeExecutionStore is the persistence seam for the production
// Runtime path. It is intentionally narrow: every method is implemented by
// the PostgreSQL store and each call carries the verified principal.
type DurableRuntimeExecutionStore interface {
	GetManagedAgentSessionForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string) (SessionSnapshot, error)
	CreateManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, CreateTurnInput) (TurnSnapshot, error)
	CreateManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CreateExecutionInput) (ExecutionSnapshot, error)
	StartManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, StartExecutionInput) (ExecutionTransitionResult, error)
	CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CompleteExecutionInput) (ExecutionTransitionResult, error)
	FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, FailExecutionInput) (ExecutionTransitionResult, error)
	CancelManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CancelTurnInput) (ExecutionTransitionResult, error)
}

type DurableRuntimeExecutionConfig struct {
	Store              DurableRuntimeExecutionStore
	Supervisor         *supervisor.Supervisor
	Clock              Clock
	FencingLeaseID     string
	FencingGeneration  uint64
	FencingToken       []byte
	WorkspaceDirectory string
	MaxDuration        time.Duration
}

type DurableRuntimeExecutionInput struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	ExecutionID string
	Model       string
	InputText   string
	Mutation    Mutation
}

type DurableRuntimeExecutionResult struct {
	Transition ExecutionTransitionResult
	Messages   []runtime.Message
}

type RuntimeTurnInput struct {
	RequestID          string
	ExecutionID        string
	Generation         uint64
	Fencing            *workerv1alpha1.FencingProof
	WorkspaceDirectory string
	ProviderKind       string
	Model              string
	InputText          string
	OccurredAt         time.Time
}

type RuntimeTurnResult struct {
	Messages    []runtime.Message
	Terminal    runtime.Message
	FailureCode string
}

type DurableRuntimeExecutionCoordinator struct {
	store              DurableRuntimeExecutionStore
	supervisor         *supervisor.Supervisor
	now                Clock
	fencingLeaseID     string
	fencingGeneration  uint64
	fencingToken       []byte
	workspaceDirectory string
	maxDuration        time.Duration
	activeMu           sync.Mutex
	active             map[durableExecutionKey]*activeDurableExecution
}

type durableExecutionKey struct {
	tenantID    string
	projectID   string
	sessionID   string
	turnID      string
	executionID string
	generation  uint64
}

type activeDurableExecution struct {
	cancel              context.CancelFunc
	externallyCancelled bool
}

var (
	ErrDurableRuntimeExecutionUnavailable = errors.New("durable Runtime execution is unavailable")
	ErrDurableRuntimeExecutionFailed      = errors.New("durable Runtime execution failed")
)

func NewDurableRuntimeExecutionCoordinator(config DurableRuntimeExecutionConfig) (*DurableRuntimeExecutionCoordinator, error) {
	if config.Store == nil || config.Supervisor == nil || config.Clock == nil || config.FencingLeaseID == "" || config.FencingGeneration == 0 || len(config.FencingToken) == 0 || strings.TrimSpace(config.WorkspaceDirectory) == "" {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	if config.MaxDuration <= 0 || config.MaxDuration > 5*time.Minute {
		config.MaxDuration = 5 * time.Minute
	}
	return &DurableRuntimeExecutionCoordinator{
		store: config.Store, supervisor: config.Supervisor, now: config.Clock,
		fencingLeaseID: config.FencingLeaseID, fencingGeneration: config.FencingGeneration,
		fencingToken: append([]byte(nil), config.FencingToken...), workspaceDirectory: config.WorkspaceDirectory,
		maxDuration: config.MaxDuration, active: make(map[durableExecutionKey]*activeDurableExecution),
	}, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) Cancel(ctx context.Context, principal *authn.VerifiedPrincipal, input CancelTurnInput) (ExecutionTransitionResult, error) {
	if coordinator == nil || coordinator.store == nil {
		return ExecutionTransitionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return ExecutionTransitionResult{}, ErrNilContext
	}
	result, err := coordinator.store.CancelManagedAgentExecution(ctx, input.Scope.TenantID, principal, input)
	if err != nil {
		return ExecutionTransitionResult{}, err
	}
	coordinator.activeMu.Lock()
	active := coordinator.active[durableExecutionKey{
		tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID,
		turnID: input.TurnID, executionID: input.TargetExecutionID, generation: input.Generation,
	}]
	var cancel context.CancelFunc
	if active != nil {
		active.externallyCancelled = true
		cancel = active.cancel
	}
	coordinator.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return result, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) Execute(ctx context.Context, principal *authn.VerifiedPrincipal, input DurableRuntimeExecutionInput) (DurableRuntimeExecutionResult, error) {
	if coordinator == nil || coordinator.store == nil || coordinator.supervisor == nil || coordinator.now == nil {
		return DurableRuntimeExecutionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return DurableRuntimeExecutionResult{}, ErrNilContext
	}
	now := coordinator.now().UTC()
	if now.IsZero() || input.Scope.validate() != nil || input.SessionID == "" || input.TurnID == "" || input.ExecutionID == "" || input.InputText == "" || input.Mutation.validate() != nil {
		return DurableRuntimeExecutionResult{}, ErrInvalidInput
	}
	if input.Mutation.RequestID == "" || input.Mutation.IdempotencyKey == "" || coordinator.fencingGeneration == 0 || coordinator.fencingLeaseID == "" || len(coordinator.fencingToken) == 0 {
		return DurableRuntimeExecutionResult{}, ErrInvalidInput
	}
	runCtx, cancel := context.WithTimeout(ctx, coordinator.maxDuration)
	defer cancel()
	runtimeCtx, runtimeCancel := context.WithCancel(runCtx)
	key := durableExecutionKey{tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID, turnID: input.TurnID, executionID: input.ExecutionID, generation: coordinator.fencingGeneration}
	unregister := coordinator.registerActiveExecution(key, runtimeCancel)
	defer func() {
		unregister()
		runtimeCancel()
	}()
	session, err := coordinator.store.GetManagedAgentSessionForExecution(runCtx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	turn, err := coordinator.store.CreateManagedAgentTurn(runCtx, input.Scope.TenantID, principal, CreateTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, InputText: input.InputText, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	execution, err := coordinator.store.CreateManagedAgentExecution(runCtx, input.Scope.TenantID, principal, CreateExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: turn.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	if execution.State != ExecutionQueued {
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}}, nil
	}
	started, err := coordinator.store.StartManagedAgentExecution(runCtx, input.Scope.TenantID, principal, StartExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	if started.Execution.State != ExecutionRunning {
		return DurableRuntimeExecutionResult{Transition: started}, nil
	}
	runtimeResult, err := ExecuteRuntimeTurn(runtimeCtx, coordinator.supervisor, RuntimeTurnInput{
		RequestID: input.Mutation.RequestID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration,
		Fencing:            &workerv1alpha1.FencingProof{LeaseId: coordinator.fencingLeaseID, Generation: coordinator.fencingGeneration, Token: append([]byte(nil), coordinator.fencingToken...)},
		WorkspaceDirectory: coordinator.workspaceDirectory, ProviderKind: session.ProviderKind, Model: input.Model, InputText: input.InputText, OccurredAt: now,
	})
	if err != nil {
		if unregister() {
			return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, context.Canceled
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return coordinator.cancel(principal, input, started, runtimeResult.Messages)
		}
		return coordinator.fail(principal, input, started, runtimeResult.Messages, runtimeResult.FailureCode, err)
	}
	if runtimeResult.Terminal.MessageType == "Error" {
		code := "runtime_failed"
		if runtimeResult.Terminal.Error != nil && ValidRuntimeErrorCode(runtimeResult.Terminal.Error.Code) {
			code = runtimeResult.Terminal.Error.Code
		}
		return coordinator.fail(principal, input, started, runtimeResult.Messages, code, errors.New(code))
	}
	digest, err := RuntimeMessageDigest(runtimeResult.Terminal)
	if err != nil {
		return coordinator.fail(principal, input, started, runtimeResult.Messages, "runtime_result_invalid", err)
	}
	completed, err := coordinator.store.CompleteManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, ResultDigest: digest, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, err
	}
	return DurableRuntimeExecutionResult{Transition: completed, Messages: runtimeResult.Messages}, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) registerActiveExecution(key durableExecutionKey, cancel context.CancelFunc) func() bool {
	active := &activeDurableExecution{cancel: cancel}
	coordinator.activeMu.Lock()
	coordinator.active[key] = active
	coordinator.activeMu.Unlock()
	return func() bool {
		coordinator.activeMu.Lock()
		defer coordinator.activeMu.Unlock()
		if coordinator.active[key] != active {
			return active.externallyCancelled
		}
		externallyCancelled := active.externallyCancelled
		delete(coordinator.active, key)
		return externallyCancelled
	}
}
func (coordinator *DurableRuntimeExecutionCoordinator) cancel(principal *authn.VerifiedPrincipal, input DurableRuntimeExecutionInput, started ExecutionTransitionResult, messages []runtime.Message) (DurableRuntimeExecutionResult, error) {
	cancelled, err := coordinator.store.CancelManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, CancelTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, TargetExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(context.Canceled, err)
	}
	return DurableRuntimeExecutionResult{Transition: cancelled, Messages: messages}, context.Canceled
}

func (coordinator *DurableRuntimeExecutionCoordinator) fail(principal *authn.VerifiedPrincipal, input DurableRuntimeExecutionInput, started ExecutionTransitionResult, messages []runtime.Message, code string, cause error) (DurableRuntimeExecutionResult, error) {
	failed, err := coordinator.store.FailManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, ErrorCode: code, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	return DurableRuntimeExecutionResult{Transition: failed, Messages: messages}, fmt.Errorf("%w: %v", ErrDurableRuntimeExecutionFailed, cause)
}

func ExecuteRuntimeTurn(ctx context.Context, workerSupervisor *supervisor.Supervisor, input RuntimeTurnInput) (RuntimeTurnResult, error) {
	result := RuntimeTurnResult{FailureCode: "runtime_open_failed"}
	session, err := workerSupervisor.OpenRuntimeSession(ctx, input.ExecutionID, input.Generation, input.Fencing)
	if err != nil {
		return result, err
	}
	defer func() { _ = session.CloseRequest(); _ = session.CloseResponse() }()
	command := func(commandType, commandID string, payload map[string]any) runtime.Command {
		return runtime.Command{RequestID: input.RequestID + ":" + commandType, Protocol: runtime.Protocol{Major: runtime.ProtocolMajor, Minor: runtime.ProtocolMinor}, ExecutionID: input.ExecutionID, Generation: input.Generation, CommandType: commandType, CommandID: commandID, OccurredAt: input.OccurredAt.Format(time.RFC3339Nano), Payload: payload}
	}
	start := command("StartSession", input.RequestID+":start", map[string]any{"runnerInput": map[string]any{
		"workspaceDirectory": input.WorkspaceDirectory,
		"workload":           map[string]any{"provider": input.ProviderKind, "model": input.Model},
		"execution":          map[string]any{"id": input.ExecutionID},
	}})
	result.FailureCode = "runtime_start_failed"
	if err := session.Send(ctx, start); err != nil {
		return result, err
	}
	startMessages, terminal, err := receiveRuntimeMessages(session, start.CommandID)
	result.Messages = append(result.Messages, startMessages...)
	if err != nil {
		return result, err
	}
	if terminal.MessageType == "Error" {
		return result, errors.New("Runtime StartSession failed")
	}
	turn := command("SendTurn", input.RequestID+":turn", map[string]any{"inputText": input.InputText})
	result.FailureCode = "runtime_turn_failed"
	if err := session.Send(ctx, turn); err != nil {
		return result, err
	}
	turnMessages, terminal, err := receiveRuntimeMessages(session, turn.CommandID)
	result.Messages = append(result.Messages, turnMessages...)
	result.Terminal = terminal
	if err != nil {
		return result, err
	}
	result.FailureCode = ""
	return result, nil
}

func receiveRuntimeMessages(session *supervisor.RuntimeSession, commandID string) ([]runtime.Message, runtime.Message, error) {
	var messages []runtime.Message
	for {
		message, err := session.Receive()
		if err != nil {
			return messages, runtime.Message{}, err
		}
		messages = append(messages, message)
		if message.CommandID != commandID {
			return messages, message, errors.New("Runtime response command id mismatch")
		}
		if message.MessageType == "Result" || message.MessageType == "Error" {
			return messages, message, nil
		}
	}
}

func RuntimeMessageDigest(message runtime.Message) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ValidRuntimeErrorCode(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
