package managedagent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	connectv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"github.com/hxp0618/cloud-agents/services/worker/supervisor"
	"google.golang.org/protobuf/proto"
)

// LocalExecutionCommandKind is the closed command vocabulary for this
// transport-neutral local bridge. No extension payload or caller-selected
// command is accepted.
type LocalExecutionCommandKind string

const (
	LocalProbeCommand           LocalExecutionCommandKind = "Probe"
	LocalValidateBindingCommand LocalExecutionCommandKind = "ValidateBinding"
)

type LocalExecutionCommand struct {
	Kind      LocalExecutionCommandKind
	ProbeName string
	Binding   Scope
}

// LocalExecutionInput is the complete immutable identity for one local
// Managed Agent execution. All IDs and the fencing proof are caller-visible
// inputs; no random ID, endpoint, provider, or database is selected here.
type LocalExecutionInput struct {
	Scope             Scope
	SessionID         string
	TurnID            string
	ExecutionID       string
	OperationID       string
	AttemptID         string
	AttemptNumber     uint32
	InputText         string
	Generation        uint64
	FencingLeaseID    string
	FencingGeneration uint64
	FencingToken      []byte
	Deadline          time.Time
	Mutation          Mutation
	Command           LocalExecutionCommand
}

type LocalExecutionCoordinatorConfig struct {
	Store             *Store
	Supervisor        *supervisor.Supervisor
	Clock             Clock
	FencingLeaseID    string
	FencingGeneration uint64
	FencingToken      []byte
}

type LocalExecutionCoordinator struct {
	store             *Store
	supervisor        *supervisor.Supervisor
	now               Clock
	fencingLeaseID    string
	fencingGeneration uint64
	fencingToken      []byte
}

type LocalExecutionResult struct {
	Transition ExecutionTransitionResult
	Receipt    *connectv1alpha1.DurableReceipt
}

var (
	ErrLocalExecutionUnavailable     = errors.New("managed agent local execution is unavailable")
	ErrLocalExecutionBindingRequired = errors.New("managed agent local execution binding is required")
)

func localDispatchAuthorityValid() bool {
	profile := supervisor.WorkerSupervisorLocalDispatchProfile()
	return profile.Valid() &&
		profile.ID == supervisor.LocalDispatchProfileID &&
		profile.ProfileDigest == supervisor.WorkerSupervisorLocalDispatchProfileDigest
}

func NewLocalExecutionCoordinator(config LocalExecutionCoordinatorConfig) (*LocalExecutionCoordinator, error) {
	if config.Store == nil || config.Supervisor == nil || config.Clock == nil || config.FencingLeaseID == "" || config.FencingGeneration == 0 || len(config.FencingToken) == 0 {
		return nil, ErrLocalExecutionUnavailable
	}
	if !utf8.ValidString(config.FencingLeaseID) || len(config.FencingLeaseID) > int(workerkernel.MaxIdentifierBytes) || len(config.FencingToken) > int(workerkernel.MaxPayloadBytes) {
		return nil, ErrLocalExecutionUnavailable
	}
	for _, r := range config.FencingLeaseID {
		if r < 0x20 || r == 0x7f {
			return nil, ErrLocalExecutionUnavailable
		}
	}
	if !ManagedAgentLifecycleProfile().Valid() || !ManagedAgentLifecycleEventProfile().Valid() || !workerkernel.WorkerOperationAdmissionProfile().Valid() || !localDispatchAuthorityValid() || !GeneratedManagedAgentLocalExecutionProfile.Valid() {
		return nil, ErrContractDrift
	}
	return &LocalExecutionCoordinator{
		store:             config.Store,
		supervisor:        config.Supervisor,
		now:               config.Clock,
		fencingLeaseID:    config.FencingLeaseID,
		fencingGeneration: config.FencingGeneration,
		fencingToken:      append([]byte(nil), config.FencingToken...),
	}, nil
}

// Execute runs the strictly local lifecycle -> Supervisor -> Worker path.
// Binding must have been established by NewLocal + BindLocalDispatch before
// this method is called; no implicit negotiation or transport fallback exists.
func (c *LocalExecutionCoordinator) Execute(ctx context.Context, input LocalExecutionInput) (LocalExecutionResult, error) {
	if c == nil || c.store == nil || c.supervisor == nil || c.now == nil {
		return LocalExecutionResult{}, ErrLocalExecutionUnavailable
	}
	if ctx == nil {
		return LocalExecutionResult{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return LocalExecutionResult{}, err
	}
	now := c.now().UTC()
	if now.IsZero() {
		return LocalExecutionResult{}, ErrInvalidClock
	}
	if err := validateLocalInput(input, now); err != nil {
		return LocalExecutionResult{}, err
	}
	if input.FencingLeaseID != c.fencingLeaseID || input.FencingGeneration != c.fencingGeneration || subtle.ConstantTimeCompare(input.FencingToken, c.fencingToken) != 1 {
		return LocalExecutionResult{}, fmt.Errorf("%w: fencing authority mismatch", ErrInvalidInput)
	}
	if !localDispatchAuthorityValid() || !GeneratedManagedAgentLocalExecutionProfile.Valid() {
		return LocalExecutionResult{}, ErrContractDrift
	}
	binding, ok := c.supervisor.CurrentBinding()
	if !ok || binding.ProfileID != supervisor.LocalDispatchProfileID || binding.ServerIdentity == nil {
		return LocalExecutionResult{}, ErrLocalExecutionBindingRequired
	}
	workerScope, err := workerScopeFromLifecycle(input.Scope)
	if err != nil {
		return LocalExecutionResult{}, err
	}
	command, err := buildLocalCommand(input.Command, input.Scope)
	if err != nil {
		return LocalExecutionResult{}, err
	}
	attempt, err := workerkernel.BuildLocalOperationAttempt(workerkernel.LocalOperationAttemptInput{
		OperationID: input.OperationID, IdempotencyKey: input.Mutation.IdempotencyKey,
		Scope: workerScope, FencingLeaseID: c.fencingLeaseID, FencingGeneration: c.fencingGeneration,
		FencingToken: c.fencingToken, Deadline: input.Deadline, AttemptID: input.AttemptID,
		AttemptNumber: input.AttemptNumber, ExpectedExecutorIdentity: binding.ServerIdentity,
		Negotiation: binding.Negotiation(), Command: command, Now: now,
	})
	if err != nil {
		return LocalExecutionResult{}, err
	}
	_, err = c.store.CreateTurn(ctx, CreateTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, InputText: input.InputText, Mutation: input.Mutation})
	if err != nil {
		return LocalExecutionResult{}, err
	}
	if _, err := c.store.CreateExecution(ctx, CreateExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, Mutation: input.Mutation}); err != nil {
		return LocalExecutionResult{}, err
	}
	_, err = c.store.StartExecution(ctx, StartExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, Mutation: input.Mutation})
	if err != nil {
		return LocalExecutionResult{}, err
	}
	receiptResponse, dispatchErr := c.supervisor.DispatchOperation(ctx, attempt)
	if dispatchErr != nil {
		transition := c.terminalizeFailure(input, ctx.Err(), dispatchErr)
		if callerErr := ctx.Err(); callerErr != nil {
			return LocalExecutionResult{Transition: transition}, callerErr
		}
		return LocalExecutionResult{Transition: transition}, dispatchErr
	}
	if receiptResponse == nil || receiptResponse.Msg == nil {
		transition, terminalErr := c.failDetached(input, "worker_failed")
		return LocalExecutionResult{Transition: transition}, terminalErr
	}
	receipt := proto.Clone(receiptResponse.Msg).(*connectv1alpha1.DurableReceipt)
	transition, terminalErr := c.terminalizeReceipt(input, receipt)
	if terminalErr != nil {
		return LocalExecutionResult{Transition: transition, Receipt: receipt}, terminalErr
	}
	return LocalExecutionResult{Transition: transition, Receipt: receipt}, nil
}

func validateLocalInput(input LocalExecutionInput, now time.Time) error {
	if err := input.Scope.validate(); err != nil {
		return err
	}
	for value, name := range map[string]string{input.SessionID: "session", input.TurnID: "turn", input.ExecutionID: "execution", input.OperationID: "operation", input.AttemptID: "attempt"} {
		if err := validateIdentifier(value, maxIdentifierBytes, name+" id"); err != nil {
			return err
		}
	}
	if input.AttemptNumber != 1 || input.Generation == 0 || input.FencingGeneration != input.Generation || input.FencingLeaseID == "" || len(input.FencingToken) == 0 {
		return fmt.Errorf("%w: fencing or generation", ErrInvalidInput)
	}
	if !utf8.ValidString(input.FencingLeaseID) || len(input.FencingLeaseID) > int(workerkernel.MaxIdentifierBytes) {
		return fmt.Errorf("%w: fencing lease", ErrInvalidInput)
	}
	for _, r := range input.FencingLeaseID {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: fencing lease", ErrInvalidInput)
		}
	}
	if len(input.FencingToken) > int(workerkernel.MaxPayloadBytes) {
		return fmt.Errorf("%w: fencing token", ErrInvalidInput)
	}
	if input.Deadline.IsZero() || !input.Deadline.UTC().After(now) || input.Deadline.UTC().After(now.Add(time.Duration(workerkernel.MaxDeadlineSeconds)*time.Second)) {
		return fmt.Errorf("%w: deadline", ErrInvalidInput)
	}
	if err := input.Mutation.validate(); err != nil {
		return err
	}
	if input.InputText == "" || len(input.InputText) > maxInputBytes || !utf8.ValidString(input.InputText) {
		return fmt.Errorf("%w: input text", ErrInvalidInput)
	}
	return nil
}

func workerScopeFromLifecycle(scope Scope) (commonv1alpha1.NamespaceRef, error) {
	if err := scope.validate(); err != nil {
		return commonv1alpha1.NamespaceRef{}, err
	}
	if !GeneratedManagedAgentLocalExecutionProfile.Valid() || GeneratedManagedAgentLocalExecutionProfile.ScopeProjection != ManagedAgentLocalExecutionScopeProjection {
		return commonv1alpha1.NamespaceRef{}, ErrContractDrift
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("cloud-agents/managed-agent-worker-scope/v1\x00"))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(scope.TenantID)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(scope.TenantID))
	binary.BigEndian.PutUint32(length[:], uint32(len(scope.ProjectID)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(scope.ProjectID))
	id := "scope-" + hex.EncodeToString(hash.Sum(nil))
	return commonv1alpha1.NormalizeNamespaceRef(commonv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", ID: id})
}

func buildLocalCommand(input LocalExecutionCommand, scope Scope) (*connectv1alpha1.OperationCommand, error) {
	switch input.Kind {
	case LocalProbeCommand:
		if input.ProbeName == "" || len(input.ProbeName) > int(workerkernel.MaxStringBytes) {
			return nil, fmt.Errorf("%w: probe command", ErrInvalidInput)
		}
		return &connectv1alpha1.OperationCommand{Command: &connectv1alpha1.OperationCommand_Probe{Probe: &connectv1alpha1.ProbeOperation{ProbeName: input.ProbeName}}}, nil
	case LocalValidateBindingCommand:
		if input.Binding != scope {
			return nil, fmt.Errorf("%w: binding scope", ErrInvalidInput)
		}
		ref, err := workerScopeFromLifecycle(input.Binding)
		if err != nil {
			return nil, err
		}
		return &connectv1alpha1.OperationCommand{Command: &connectv1alpha1.OperationCommand_ValidateBinding{ValidateBinding: &connectv1alpha1.ValidateBindingOperation{Binding: &connectv1alpha1.NamespaceRef{Namespace: ref.Namespace, Kind: ref.Kind, Id: ref.ID}}}}, nil
	default:
		return nil, fmt.Errorf("%w: command kind", ErrInvalidInput)
	}
}

func (c *LocalExecutionCoordinator) terminalizeReceipt(input LocalExecutionInput, receipt *connectv1alpha1.DurableReceipt) (ExecutionTransitionResult, error) {
	var (
		transition ExecutionTransitionResult
		err        error
		wantTurn   TurnState
		wantExec   ExecutionState
	)
	switch receipt.GetOutcome() {
	case connectv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED:
		digest := receiptDigest(receipt)
		transition, err = c.store.CompleteExecution(context.Background(), CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ResultDigest: digest, Mutation: input.Mutation})
		wantTurn, wantExec = TurnCompleted, ExecutionSucceeded
	case connectv1alpha1.OperationOutcome_OPERATION_OUTCOME_CANCELLED:
		transition, err = c.store.CancelTurn(context.Background(), CancelTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, TargetExecutionID: input.ExecutionID, Generation: input.Generation, Mutation: input.Mutation})
		wantTurn, wantExec = TurnCancelled, ExecutionCancelled
	default:
		code := localStableErrorCode(receipt.GetStableErrorCode())
		if receipt.GetOutcome() == connectv1alpha1.OperationOutcome_OPERATION_OUTCOME_DEADLINE_EXCEEDED {
			code = "deadline_exceeded"
		}
		if receipt.GetOutcome() == connectv1alpha1.OperationOutcome_OPERATION_OUTCOME_FENCED {
			code = "fenced"
		}
		transition, err = c.store.FailExecution(context.Background(), FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ErrorCode: code, Mutation: input.Mutation})
		wantTurn, wantExec = TurnFailed, ExecutionFailed
	}
	if err == nil {
		return transition, nil
	}
	if errors.Is(err, ErrInvalidTransition) {
		return c.reconcileTerminal(input, wantTurn, wantExec)
	}
	return transition, err
}

func localStableErrorCode(code string) string {
	switch code {
	case "execution_failed", "worker_dispatch_failed", "deadline_exceeded", "fenced", "cancelled", "worker_failed":
		return code
	default:
		return "worker_failed"
	}
}

func (c *LocalExecutionCoordinator) terminalizeFailure(input LocalExecutionInput, ctxErr error, dispatchErr error) ExecutionTransitionResult {
	var transition ExecutionTransitionResult
	var err error
	wantTurn, wantExec := TurnFailed, ExecutionFailed
	if ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			transition, err = c.store.CancelTurn(context.Background(), CancelTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, TargetExecutionID: input.ExecutionID, Generation: input.Generation, Mutation: input.Mutation})
			wantTurn, wantExec = TurnCancelled, ExecutionCancelled
		} else {
			transition, err = c.store.FailExecution(context.Background(), FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ErrorCode: "deadline_exceeded", Mutation: input.Mutation})
		}
	} else {
		code := "worker_dispatch_failed"
		if connect.CodeOf(dispatchErr) == connect.CodeDeadlineExceeded {
			code = "deadline_exceeded"
		}
		transition, err = c.store.FailExecution(context.Background(), FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ErrorCode: code, Mutation: input.Mutation})
	}
	if err == nil {
		return transition
	}
	if errors.Is(err, ErrInvalidTransition) {
		reconciled, reconcileErr := c.reconcileTerminal(input, wantTurn, wantExec)
		if reconcileErr == nil {
			return reconciled
		}
	}
	return transition
}

func (c *LocalExecutionCoordinator) reconcileTerminal(input LocalExecutionInput, wantTurn TurnState, wantExec ExecutionState) (ExecutionTransitionResult, error) {
	turn, err := c.store.GetTurn(context.Background(), input.Scope, input.SessionID, input.TurnID)
	if err != nil {
		return ExecutionTransitionResult{}, err
	}
	execution, err := c.store.GetExecution(context.Background(), input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
	if err != nil {
		return ExecutionTransitionResult{}, err
	}
	result := ExecutionTransitionResult{Turn: turn, Execution: execution}
	if turn.State != wantTurn || execution.State != wantExec {
		return result, fmt.Errorf("%w: concurrent terminal state", ErrInvalidTransition)
	}
	return result, nil
}

func (c *LocalExecutionCoordinator) failDetached(input LocalExecutionInput, code string) (ExecutionTransitionResult, error) {
	transition, err := c.store.FailExecution(context.Background(), FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: input.Generation, ErrorCode: localStableErrorCode(code), Mutation: input.Mutation})
	if errors.Is(err, ErrInvalidTransition) {
		return c.reconcileTerminal(input, TurnFailed, ExecutionFailed)
	}
	return transition, err
}

func receiptDigest(receipt *connectv1alpha1.DurableReceipt) string {
	clone := proto.Clone(receipt).(*connectv1alpha1.DurableReceipt)
	clone.ReceiptId = ""
	clone.Sequence = 0
	clone.ObservedAt = nil
	if clone.Fencing != nil {
		clone.Fencing.TokenSha256 = nil
	}
	bytes, _ := proto.MarshalOptions{Deterministic: true}.Marshal(clone)
	sum := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}
