package controlplane

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/hxp0618/cloud-agents/services/worker/runtime"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
)

// RuntimeExecutionConfig binds the public in-memory lifecycle to an already
// authenticated Worker Runtime session. The caller must bind the Supervisor
// before Execute; this coordinator never falls back to another transport.
type RuntimeExecutionConfig struct {
	Supervisor        *supervisor.Supervisor
	Clock             Clock
	FencingLeaseID    string
	FencingGeneration uint64
	FencingToken      []byte
}

// RuntimeExecutionInput is the complete input for one Provider turn.
type RuntimeExecutionInput struct {
	Scope              Scope
	SessionID          string
	TurnID             string
	ExecutionID        string
	Generation         uint64
	FencingLeaseID     string
	FencingGeneration  uint64
	FencingToken       []byte
	Deadline           time.Time
	WorkspaceDirectory string
	Model              string
	InputText          string
	Mutation           Mutation
}

type RuntimeExecutionResult struct {
	Transition ExecutionTransitionResult
	Messages   []runtime.Message
}

type RuntimeExecutionCoordinator struct {
	service           *ManagedAgentService
	supervisor        *supervisor.Supervisor
	now               Clock
	fencingLeaseID    string
	fencingGeneration uint64
	fencingToken      []byte
}

var ErrRuntimeExecutionUnavailable = errors.New("managed agent Runtime execution is unavailable")

// NewRuntimeExecutionCoordinator constructs the smallest public Go Control
// Plane Runtime path. Runtime binding and fencing remain explicit inputs.
func (service *ManagedAgentService) NewRuntimeExecutionCoordinator(config RuntimeExecutionConfig) (*RuntimeExecutionCoordinator, error) {
	if service == nil || service.store == nil || config.Supervisor == nil || config.Clock == nil || config.FencingLeaseID == "" || config.FencingGeneration == 0 || len(config.FencingToken) == 0 {
		return nil, ErrRuntimeExecutionUnavailable
	}
	return &RuntimeExecutionCoordinator{service: service, supervisor: config.Supervisor, now: config.Clock, fencingLeaseID: config.FencingLeaseID, fencingGeneration: config.FencingGeneration, fencingToken: append([]byte(nil), config.FencingToken...)}, nil
}

// Execute starts one Runtime Session, sends one Provider turn, and commits
// the in-memory lifecycle only after the Runtime terminal message arrives.
func (coordinator *RuntimeExecutionCoordinator) Execute(ctx context.Context, input RuntimeExecutionInput) (RuntimeExecutionResult, error) {
	if coordinator == nil || coordinator.service == nil || coordinator.supervisor == nil || coordinator.now == nil {
		return RuntimeExecutionResult{}, ErrRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return RuntimeExecutionResult{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return RuntimeExecutionResult{}, err
	}
	now := coordinator.now().UTC()
	if now.IsZero() || input.Generation == 0 || input.Generation != coordinator.fencingGeneration || input.FencingLeaseID != coordinator.fencingLeaseID || input.FencingGeneration != coordinator.fencingGeneration || subtle.ConstantTimeCompare(input.FencingToken, coordinator.fencingToken) != 1 || input.Deadline.IsZero() || !input.Deadline.UTC().After(now) || input.Deadline.UTC().After(now.Add(5*time.Minute)) {
		return RuntimeExecutionResult{}, ErrInvalidInput
	}
	if strings.TrimSpace(input.WorkspaceDirectory) == "" || strings.ContainsRune(input.WorkspaceDirectory, '\x00') || input.InputText == "" {
		return RuntimeExecutionResult{}, ErrInvalidInput
	}
	runCtx, cancel := context.WithDeadline(ctx, input.Deadline.UTC())
	defer cancel()
	session, err := coordinator.service.GetSession(runCtx, input.Scope, input.SessionID)
	if err != nil {
		return RuntimeExecutionResult{}, err
	}
	turn, err := coordinator.service.CreateTurn(runCtx, CreateTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, InputText: input.InputText, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{}, err
	}
	execution, err := coordinator.service.CreateExecution(runCtx, CreateExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: turn.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{}, err
	}
	current, err := coordinator.service.GetExecution(runCtx, input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
	if err != nil {
		return RuntimeExecutionResult{}, err
	}
	if current.State != ExecutionQueued {
		currentTurn, getTurnErr := coordinator.service.GetTurn(runCtx, input.Scope, input.SessionID, input.TurnID)
		if getTurnErr != nil {
			return RuntimeExecutionResult{}, getTurnErr
		}
		return RuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: currentTurn, Execution: current}}, nil
	}
	started, err := coordinator.service.StartExecution(runCtx, StartExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: execution.ExecutionID, Generation: input.Generation, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{}, err
	}

	runtimeResult, err := internalmanagedagent.ExecuteRuntimeTurn(runCtx, coordinator.supervisor, internalmanagedagent.RuntimeTurnInput{
		RequestID: input.Mutation.RequestID, ExecutionID: input.ExecutionID, Generation: input.Generation, Fencing: input.fencingProof(),
		WorkspaceDirectory: input.WorkspaceDirectory, ProviderKind: session.ProviderKind, Model: input.Model, InputText: input.InputText, OccurredAt: now,
	})
	if err != nil {
		return coordinator.failWithMessages(ctx, input, started, runtimeResult.Messages, runtimeResult.FailureCode, err)
	}
	if runtimeResult.Terminal.MessageType == "Error" {
		code := "runtime_failed"
		if runtimeResult.Terminal.Error != nil && internalmanagedagent.ValidRuntimeErrorCode(runtimeResult.Terminal.Error.Code) {
			code = runtimeResult.Terminal.Error.Code
		}
		return coordinator.failWithMessages(ctx, input, started, runtimeResult.Messages, code, errors.New(code))
	}
	digest, err := internalmanagedagent.RuntimeMessageDigest(runtimeResult.Terminal)
	if err != nil {
		return coordinator.failWithMessages(ctx, input, started, runtimeResult.Messages, "runtime_result_invalid", err)
	}
	completed, err := coordinator.service.CompleteExecution(context.Background(), CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ResultDigest: digest, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, err
	}
	return RuntimeExecutionResult{Transition: completed, Messages: runtimeResult.Messages}, nil
}

func (input RuntimeExecutionInput) fencingProof() *workerv1alpha1.FencingProof {
	return &workerv1alpha1.FencingProof{LeaseId: input.FencingLeaseID, Generation: input.FencingGeneration, Token: append([]byte(nil), input.FencingToken...)}
}

func (coordinator *RuntimeExecutionCoordinator) fail(ctx context.Context, input RuntimeExecutionInput, started ExecutionTransitionResult, code string, cause error) (RuntimeExecutionResult, error) {
	return coordinator.failWithMessages(ctx, input, started, nil, code, cause)
}

func (coordinator *RuntimeExecutionCoordinator) failWithMessages(_ context.Context, input RuntimeExecutionInput, started ExecutionTransitionResult, messages []runtime.Message, code string, cause error) (RuntimeExecutionResult, error) {
	failed, err := coordinator.service.FailExecution(context.Background(), FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ErrorCode: code, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	return RuntimeExecutionResult{Transition: failed, Messages: messages}, cause
}
