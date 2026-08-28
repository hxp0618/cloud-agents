package controlplane

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
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

	runtimeSession, err := coordinator.supervisor.OpenRuntimeSession(runCtx, input.ExecutionID, input.Generation, input.fencingProof())
	if err != nil {
		return coordinator.fail(ctx, input, started, "runtime_open_failed", err)
	}
	defer func() { _ = runtimeSession.CloseRequest(); _ = runtimeSession.CloseResponse() }()

	start := runtimeCommand(input, "StartSession", input.Mutation.RequestID+":start", now, map[string]any{
		"runnerInput": map[string]any{
			"workspaceDirectory": input.WorkspaceDirectory,
			"workload":           map[string]any{"provider": session.ProviderKind, "model": input.Model},
			"execution":          map[string]any{"id": input.ExecutionID},
		},
	})
	if err := runtimeSession.Send(runCtx, start); err != nil {
		return coordinator.fail(ctx, input, started, "runtime_start_failed", err)
	}
	messages, err := receiveRuntimeTerminal(runtimeSession, start.CommandID)
	if err != nil {
		return coordinator.fail(ctx, input, started, "runtime_start_failed", err)
	}
	if len(messages) == 0 || messages[len(messages)-1].MessageType == "Error" {
		return coordinator.fail(ctx, input, started, "runtime_start_failed", errors.New("Runtime StartSession failed"))
	}

	turnCommand := runtimeCommand(input, "SendTurn", input.Mutation.RequestID+":turn", now, map[string]any{"inputText": input.InputText})
	if err := runtimeSession.Send(runCtx, turnCommand); err != nil {
		return coordinator.fail(ctx, input, started, "runtime_turn_failed", err)
	}
	turnMessages, terminal, err := receiveRuntimeMessages(runtimeSession, turnCommand.CommandID)
	messages = append(messages, turnMessages...)
	if err != nil {
		return coordinator.failWithMessages(ctx, input, started, messages, "runtime_turn_failed", err)
	}
	if terminal.MessageType == "Error" {
		code := "runtime_failed"
		if terminal.Error != nil && terminal.Error.Code != "" {
			code = terminal.Error.Code
		}
		return coordinator.failWithMessages(ctx, input, started, messages, code, errors.New(code))
	}
	digest, err := runtimeMessageDigest(terminal)
	if err != nil {
		return coordinator.failWithMessages(ctx, input, started, messages, "runtime_result_invalid", err)
	}
	completed, err := coordinator.service.CompleteExecution(context.Background(), CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ResultDigest: digest, Mutation: input.Mutation})
	if err != nil {
		return RuntimeExecutionResult{Transition: started, Messages: messages}, err
	}
	return RuntimeExecutionResult{Transition: completed, Messages: messages}, nil
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

func runtimeCommand(input RuntimeExecutionInput, commandType, commandID string, now time.Time, payload map[string]any) runtime.Command {
	return runtime.Command{RequestID: input.Mutation.RequestID + ":" + commandType, Protocol: runtime.Protocol{Major: runtime.ProtocolMajor, Minor: runtime.ProtocolMinor}, ExecutionID: input.ExecutionID, Generation: input.Generation, CommandType: commandType, CommandID: commandID, OccurredAt: now.Format(time.RFC3339Nano), Payload: payload}
}

func receiveRuntimeTerminal(session *supervisor.RuntimeSession, commandID string) ([]runtime.Message, error) {
	messages, _, err := receiveRuntimeMessages(session, commandID)
	return messages, err
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
			return messages, message, fmt.Errorf("runtime response command id mismatch")
		}
		if message.MessageType == "Result" || message.MessageType == "Error" {
			return messages, message, nil
		}
	}
}

func runtimeMessageDigest(message runtime.Message) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
