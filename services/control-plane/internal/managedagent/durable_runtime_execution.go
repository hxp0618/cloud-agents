package managedagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
)

// DurableRuntimeExecutionStore is the persistence seam for the production
// Runtime path. It is intentionally narrow: every method is implemented by
// the PostgreSQL store and each call carries the verified principal.
type DurableRuntimeExecutionStore interface {
	GetManagedAgentSessionForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string) (RuntimeSessionSnapshot, error)
	FindManagedAgentTurnForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string) (TurnSnapshot, bool, error)
	CreateManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, CreateTurnInput) (TurnSnapshot, error)
	CreateManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CreateExecutionInput) (ExecutionSnapshot, error)
	StartManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, StartExecutionInput) (ExecutionTransitionResult, error)
	CompleteManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CompleteRuntimeExecutionInput) (ExecutionTransitionResult, error)
	FailManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, FailRuntimeExecutionInput) (ExecutionTransitionResult, error)
	InterruptManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, InterruptTurnInput) (ExecutionTransitionResult, error)
	CancelManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CancelTurnInput) (ExecutionTransitionResult, error)
}

// VerifiedPrincipalSource returns a fresh, one-shot principal for one protected
// persistence operation. A Runtime execution spans several transactions, so
// the coordinator must never reuse a consumed principal.
type VerifiedPrincipalSource func() (*authn.VerifiedPrincipal, error)

type DurableRuntimeExecutionConfig struct {
	Store              DurableRuntimeExecutionStore
	Supervisor         *workerclient.Supervisor
	Clock              Clock
	FencingLeaseID     string
	FencingGeneration  uint64
	FencingToken       []byte
	WorkspaceDirectory string
	MaxDuration        time.Duration
}

type DurableRuntimeExecutionInput struct {
	Scope           Scope
	SessionID       string
	TurnID          string
	ExecutionID     string
	Model           string
	RuntimeMode     string
	InteractionMode string
	InputText       string
	Mutation        Mutation
}

type DurableRuntimeExecutionResult struct {
	Transition ExecutionTransitionResult
	Messages   []runtimeprotocol.Message
}

type RuntimeTurnInput struct {
	Scope                Scope
	SessionID            string
	TurnID               string
	RequestID            string
	ExecutionID          string
	Generation           uint64
	WorkspaceDirectory   string
	ProviderKind         string
	ProviderResumeCursor string
	Model                string
	RuntimeMode          string
	InteractionMode      string
	InputText            string
	OccurredAt           time.Time
}

type runtimeWorkspacePaths struct {
	workspaceDirectory     string
	runtimeOutputDirectory string
	providerStateDirectory string
}

type RuntimeTurnResult struct {
	Messages             []runtimeprotocol.Message
	Terminal             runtimeprotocol.Message
	ProviderResumeCursor string
	FailureCode          string
}

type RuntimeExecutionReference struct {
	Scope       Scope
	SessionID   string
	TurnID      string
	ExecutionID string
	Generation  uint64
}

type RuntimeArtifactReadInput struct {
	RuntimeExecutionReference
	Message runtimeprotocol.Message
}

type RuntimeArtifact struct {
	Data        []byte
	ContentType string
	SHA256      string
}

type RuntimeApprovalResolutionInput struct {
	RuntimeExecutionReference
	RequestID            string
	InteractionRequestID string
	Decision             string
}

type RuntimeUserInputResolutionInput struct {
	RuntimeExecutionReference
	RequestID            string
	InteractionRequestID string
	Answers              map[string][]string
}

type DurableRuntimeExecutionCoordinator struct {
	store              DurableRuntimeExecutionStore
	supervisor         *workerclient.Supervisor
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
	stop                func()
	externallyCancelled bool
	send                func(context.Context, runtimeprotocol.Command) error
	messages            []runtimeprotocol.Message
	interactions        map[string]*activeRuntimeInteraction
	controls            map[string]*activeRuntimeInteractionResolution
	nextControl         uint64
}

type activeRuntimeInteraction struct {
	interactionType string
	resolvedPayload string
	resolution      *activeRuntimeInteractionResolution
}

type activeRuntimeInteractionResolution struct {
	interaction *activeRuntimeInteraction
	payload     string
	requestID   string
	commandID   string
	done        chan struct{}
	err         error
}

const (
	maxPublicExecutionMessageIdentifierBytes = 128
	maxRuntimeExecutionMessages              = 64
	maxRuntimeInteractionRequestIDCharacters = 200
	maxRuntimeInteractionAnswers             = 3
	maxRuntimeInteractionAnswerValues        = 20
	maxRuntimeInteractionAnswerCharacters    = 2000
)

var (
	ErrDurableRuntimeExecutionUnavailable = errors.New("durable Runtime execution is unavailable")
	ErrDurableRuntimeExecutionConflict    = errors.New("durable Runtime execution is already active")
	ErrDurableRuntimeExecutionFailed      = errors.New("durable Runtime execution failed")
	ErrRuntimeCapacityExhausted           = errors.New("Runtime session capacity is exhausted")
	ErrRuntimeInteractionUnavailable      = errors.New("Runtime interaction is unavailable")
	ErrRuntimeInteractionConflict         = errors.New("Runtime interaction resolution conflicts with active state")
	ErrRuntimeInteractionFailed           = errors.New("Runtime interaction resolution failed")
	ErrRuntimeArtifactUnavailable         = errors.New("Runtime artifact is unavailable")
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
	coordinator.stopActiveExecution(durableExecutionKey{
		tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID,
		turnID: input.TurnID, executionID: input.TargetExecutionID, generation: input.Generation,
	})
	return result, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) Interrupt(ctx context.Context, principal *authn.VerifiedPrincipal, input InterruptTurnInput) (ExecutionTransitionResult, error) {
	if coordinator == nil || coordinator.store == nil {
		return ExecutionTransitionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return ExecutionTransitionResult{}, ErrNilContext
	}
	result, err := coordinator.store.InterruptManagedAgentExecution(ctx, input.Scope.TenantID, principal, input)
	if err != nil {
		return ExecutionTransitionResult{}, err
	}
	coordinator.stopActiveExecution(durableExecutionKey{
		tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID,
		turnID: input.TurnID, executionID: input.TargetExecutionID, generation: input.Generation,
	})
	return result, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) stopActiveExecution(key durableExecutionKey) {
	coordinator.activeMu.Lock()
	active := coordinator.active[key]
	var cancel context.CancelFunc
	var stop func()
	if active != nil {
		active.externallyCancelled = true
		failActiveInteractionResolutions(active, context.Canceled)
		cancel = active.cancel
		stop = active.stop
	}
	coordinator.activeMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if stop != nil {
		stop()
	}
}

func (coordinator *DurableRuntimeExecutionCoordinator) ActiveMessages(reference RuntimeExecutionReference) []runtimeprotocol.Message {
	key, err := durableRuntimeExecutionKey(reference)
	if coordinator == nil || err != nil {
		return nil
	}
	coordinator.activeMu.Lock()
	defer coordinator.activeMu.Unlock()
	active := coordinator.active[key]
	if active == nil {
		return nil
	}
	return append([]runtimeprotocol.Message(nil), active.messages...)
}

func (coordinator *DurableRuntimeExecutionCoordinator) ReadArtifact(ctx context.Context, input RuntimeArtifactReadInput) (RuntimeArtifact, error) {
	if coordinator == nil || coordinator.supervisor == nil || ctx == nil {
		return RuntimeArtifact{}, ErrRuntimeArtifactUnavailable
	}
	candidate, err := runtimeArtifactCandidate(input)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	paths, err := deriveRuntimeWorkspacePaths(coordinator.workspaceDirectory, input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
	if err != nil {
		return RuntimeArtifact{}, err
	}
	root := paths.workspaceDirectory
	if candidate.sourceRoot == "runtime-output" {
		root = paths.runtimeOutputDirectory
	}
	data, err := coordinator.supervisor.ReadRuntimeArtifact(ctx, input.ExecutionID, coordinator.fencingGeneration, &workerv1alpha1.FencingProof{
		LeaseId: coordinator.fencingLeaseID, Generation: coordinator.fencingGeneration, Token: append([]byte(nil), coordinator.fencingToken...),
	}, root, candidate.relativePath, candidate.expectedSize, candidate.sha256)
	if err != nil {
		return RuntimeArtifact{}, fmt.Errorf("%w: %v", ErrRuntimeArtifactUnavailable, err)
	}
	return RuntimeArtifact{Data: data, ContentType: candidate.contentType, SHA256: candidate.sha256}, nil
}

type runtimeArtifactReference struct {
	sourceRoot   string
	relativePath string
	contentType  string
	expectedSize *uint64
	sha256       string
}

func runtimeArtifactCandidate(input RuntimeArtifactReadInput) (runtimeArtifactReference, error) {
	message := input.Message
	if runtimeprotocol.ValidateMessage(message) != nil || message.MessageType != "ArtifactCandidate" || message.ExecutionID != input.ExecutionID || message.Generation != input.Generation {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	artifact, ok := message.Payload["artifact"].(map[string]any)
	if !ok {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	sourceRoot, _ := artifact["sourceRoot"].(string)
	relativePath, _ := artifact["path"].(string)
	kind, _ := artifact["kind"].(string)
	if sourceRoot != "workspace" && sourceRoot != "runtime-output" || !validRuntimeArtifactRelativePath(relativePath) || !validRuntimeArtifactKind(kind) {
		return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
	}
	contentType := "application/octet-stream"
	if raw, exists := artifact["contentType"]; exists {
		value, ok := raw.(string)
		mediaType, parameters, err := mime.ParseMediaType(value)
		if !ok || err != nil || len(value) > 255 || mediaType == "" {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		contentType = mime.FormatMediaType(mediaType, parameters)
	}
	var expectedSize *uint64
	if raw, exists := artifact["reportedSize"]; exists {
		size, ok := runtimeArtifactSize(raw)
		if !ok || size > runtimeprotocol.MaxArtifactBytes {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		expectedSize = &size
	}
	digest := ""
	if raw, exists := artifact["sha256"]; exists {
		value, ok := raw.(string)
		decoded, err := hex.DecodeString(value)
		if !ok || err != nil || len(decoded) != sha256.Size || strings.ToLower(value) != value {
			return runtimeArtifactReference{}, ErrRuntimeArtifactUnavailable
		}
		digest = value
	}
	return runtimeArtifactReference{sourceRoot: sourceRoot, relativePath: relativePath, contentType: contentType, expectedSize: expectedSize, sha256: digest}, nil
}

func validRuntimeArtifactRelativePath(value string) bool {
	return value != "" && len(value) <= 4096 && value != "." && value != ".." && !strings.HasPrefix(value, "../") && !path.IsAbs(value) && path.Clean(value) == value && !strings.Contains(value, `\`) && !containsRuntimePathControl(value)
}

func validRuntimeArtifactKind(value string) bool {
	switch strings.ReplaceAll(value, "_", "-") {
	case "diff", "generated-file", "terminal-log", "provider-output":
		return true
	default:
		return false
	}
}

func runtimeArtifactSize(value any) (uint64, bool) {
	switch size := value.(type) {
	case float64:
		return uint64(size), size >= 0 && size <= runtimeprotocol.MaxArtifactBytes && float64(uint64(size)) == size
	case int:
		return uint64(size), size >= 0
	case uint64:
		return size, true
	default:
		return 0, false
	}
}

func (coordinator *DurableRuntimeExecutionCoordinator) ResolveApproval(ctx context.Context, input RuntimeApprovalResolutionInput) error {
	if input.Decision != "accept" && input.Decision != "decline" {
		return ErrInvalidInput
	}
	payload := map[string]any{"requestId": input.InteractionRequestID, "resolution": map[string]any{"decision": input.Decision}}
	return coordinator.resolveRuntimeInteraction(ctx, input.RuntimeExecutionReference, input.RequestID, input.InteractionRequestID, "approval", "ResolveApproval", payload)
}

func (coordinator *DurableRuntimeExecutionCoordinator) ResolveUserInput(ctx context.Context, input RuntimeUserInputResolutionInput) error {
	answers := make(map[string]any, len(input.Answers))
	if len(input.Answers) == 0 || len(input.Answers) > maxRuntimeInteractionAnswers {
		return ErrInvalidInput
	}
	for questionID, values := range input.Answers {
		if err := validateRuntimeInteractionToken(questionID, "question id"); err != nil || len(values) == 0 || len(values) > maxRuntimeInteractionAnswerValues {
			return ErrInvalidInput
		}
		copyValues := append([]string(nil), values...)
		for _, value := range copyValues {
			if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRuntimeInteractionAnswerCharacters || strings.ContainsRune(value, '\x00') {
				return ErrInvalidInput
			}
		}
		answers[questionID] = copyValues
	}
	payload := map[string]any{"requestId": input.InteractionRequestID, "resolution": map[string]any{"answers": answers}}
	return coordinator.resolveRuntimeInteraction(ctx, input.RuntimeExecutionReference, input.RequestID, input.InteractionRequestID, "user-input", "ResolveUserInput", payload)
}

func (coordinator *DurableRuntimeExecutionCoordinator) resolveRuntimeInteraction(ctx context.Context, reference RuntimeExecutionReference, requestID, interactionRequestID, interactionType, commandType string, payload map[string]any) error {
	if coordinator == nil || ctx == nil || coordinator.now == nil || validateIdentifier(requestID, maxIdentifierBytes, "request id") != nil || validateRuntimeInteractionToken(interactionRequestID, "interaction request id") != nil {
		return ErrInvalidInput
	}
	key, err := durableRuntimeExecutionKey(reference)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(struct {
		CommandType string         `json:"commandType"`
		Payload     map[string]any `json:"payload"`
	}{CommandType: commandType, Payload: payload})
	if err != nil {
		return ErrInvalidInput
	}
	canonical := string(encoded)

	coordinator.activeMu.Lock()
	active := coordinator.active[key]
	if active == nil || active.send == nil {
		coordinator.activeMu.Unlock()
		return ErrRuntimeInteractionUnavailable
	}
	interaction := active.interactions[interactionRequestID]
	if interaction == nil || interaction.interactionType != interactionType {
		coordinator.activeMu.Unlock()
		return ErrRuntimeInteractionConflict
	}
	if interaction.resolvedPayload != "" {
		resolvedPayload := interaction.resolvedPayload
		coordinator.activeMu.Unlock()
		if resolvedPayload == canonical {
			return nil
		}
		return ErrRuntimeInteractionConflict
	}
	if interaction.resolution != nil {
		resolution := interaction.resolution
		coordinator.activeMu.Unlock()
		if resolution.payload != canonical {
			return ErrRuntimeInteractionConflict
		}
		return waitRuntimeInteractionResolution(ctx, resolution)
	}
	active.nextControl++
	commandID := boundedRuntimeIdentifier(requestID, fmt.Sprintf("interaction-%d", active.nextControl))
	resolution := &activeRuntimeInteractionResolution{interaction: interaction, payload: canonical, requestID: requestID, commandID: commandID, done: make(chan struct{})}
	interaction.resolution = resolution
	active.controls[commandID] = resolution
	send := active.send
	now := coordinator.now().UTC()
	coordinator.activeMu.Unlock()
	if now.IsZero() {
		coordinator.failRuntimeInteractionResolution(key, active, resolution, ErrRuntimeInteractionUnavailable)
		return ErrRuntimeInteractionUnavailable
	}
	command := runtimeprotocol.Command{RequestID: requestID, Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: reference.ExecutionID, Generation: reference.Generation, CommandType: commandType, CommandID: commandID, OccurredAt: now.Format(time.RFC3339Nano), Payload: payload}
	if err := send(ctx, command); err != nil {
		coordinator.failRuntimeInteractionResolution(key, active, resolution, fmt.Errorf("%w: %v", ErrRuntimeInteractionFailed, err))
	}
	return waitRuntimeInteractionResolution(ctx, resolution)
}

func waitRuntimeInteractionResolution(ctx context.Context, resolution *activeRuntimeInteractionResolution) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-resolution.done:
		return resolution.err
	}
}

func durableRuntimeExecutionKey(reference RuntimeExecutionReference) (durableExecutionKey, error) {
	if err := validateExecutionInput(reference.Scope, reference.SessionID, reference.TurnID, reference.ExecutionID, reference.Generation); err != nil {
		return durableExecutionKey{}, err
	}
	return durableExecutionKey{tenantID: reference.Scope.TenantID, projectID: reference.Scope.ProjectID, sessionID: reference.SessionID, turnID: reference.TurnID, executionID: reference.ExecutionID, generation: reference.Generation}, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) Execute(ctx context.Context, principalSource VerifiedPrincipalSource, input DurableRuntimeExecutionInput) (DurableRuntimeExecutionResult, error) {
	if coordinator == nil || coordinator.store == nil || coordinator.supervisor == nil || coordinator.now == nil {
		return DurableRuntimeExecutionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return DurableRuntimeExecutionResult{}, ErrNilContext
	}
	if principalSource == nil {
		return DurableRuntimeExecutionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if input.RuntimeMode == "" {
		input.RuntimeMode = "full-access"
	}
	if input.InteractionMode == "" {
		input.InteractionMode = "default"
	}
	now := coordinator.now().UTC()
	if now.IsZero() || input.Scope.validate() != nil || input.SessionID == "" || input.TurnID == "" || input.ExecutionID == "" || input.RuntimeMode != "approval-required" && input.RuntimeMode != "full-access" || input.InteractionMode != "default" && input.InteractionMode != "plan" || input.InputText == "" || input.Mutation.validate() != nil {
		return DurableRuntimeExecutionResult{}, ErrInvalidInput
	}
	if input.Mutation.RequestID == "" || input.Mutation.IdempotencyKey == "" || coordinator.fencingGeneration == 0 || coordinator.fencingLeaseID == "" || len(coordinator.fencingToken) == 0 {
		return DurableRuntimeExecutionResult{}, ErrInvalidInput
	}
	runCtx, cancel := context.WithTimeout(ctx, coordinator.maxDuration)
	defer cancel()
	runtimeCtx, runtimeCancel := context.WithCancel(runCtx)
	key := durableExecutionKey{tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID, turnID: input.TurnID, executionID: input.ExecutionID, generation: coordinator.fencingGeneration}
	active, unregister, err := coordinator.registerActiveExecution(key, runtimeCancel)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	defer func() {
		unregister()
		runtimeCancel()
	}()
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	session, err := coordinator.store.GetManagedAgentSessionForExecution(runCtx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	turn, found, err := coordinator.store.FindManagedAgentTurnForExecution(runCtx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID, input.TurnID)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	if found {
		inputDigest, digestErr := TurnInputDigest(input.InputText)
		if digestErr != nil || turn.Scope != input.Scope || turn.SessionID != input.SessionID || turn.TurnID != input.TurnID || turn.InputDigest != inputDigest || turn.ExecutionID != "" && turn.ExecutionID != input.ExecutionID || turn.ExecutionID == "" && turn.State != TurnQueued {
			return DurableRuntimeExecutionResult{}, ErrDurableRuntimeExecutionConflict
		}
	} else {
		principal, err = nextVerifiedPrincipal(principalSource)
		if err != nil {
			return DurableRuntimeExecutionResult{}, err
		}
		turn, err = coordinator.store.CreateManagedAgentTurn(runCtx, input.Scope.TenantID, principal, CreateTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, InputText: input.InputText, Mutation: input.Mutation})
		if err != nil {
			return DurableRuntimeExecutionResult{}, err
		}
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	execution, err := coordinator.store.CreateManagedAgentExecution(runCtx, input.Scope.TenantID, principal, CreateExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: turn.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	if execution.State == ExecutionRunning {
		return coordinator.fail(principalSource, input, ExecutionTransitionResult{Turn: turn, Execution: execution}, nil, "orphaned_execution", errors.New("running Runtime execution has no active owner"))
	}
	if execution.State != ExecutionQueued {
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, nil
	}
	// Claim Worker capacity before the durable running transition so saturation remains replayable as queued.
	runtimeSession, openErr := coordinator.supervisor.OpenRuntimeSession(runtimeCtx, input.ExecutionID, session.ProviderKind, coordinator.fencingGeneration, &workerv1alpha1.FencingProof{
		LeaseId: coordinator.fencingLeaseID, Generation: coordinator.fencingGeneration, Token: append([]byte(nil), coordinator.fencingToken...),
	})
	if connect.CodeOf(openErr) == connect.CodeResourceExhausted {
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}}, fmt.Errorf("%w: %v", ErrRuntimeCapacityExhausted, openErr)
	}
	if runtimeSession != nil {
		defer func() {
			_ = runtimeSession.CloseRequest()
			_ = runtimeSession.CloseResponse()
		}()
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	started, err := coordinator.store.StartManagedAgentExecution(runCtx, input.Scope.TenantID, principal, StartExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	if started.Execution.State != ExecutionRunning {
		return DurableRuntimeExecutionResult{Transition: started}, nil
	}
	if openErr != nil {
		return coordinator.fail(principalSource, input, started, nil, "runtime_open_failed", openErr)
	}
	runtimeResult, err := coordinator.executeRuntimeTurn(runtimeCtx, key, active, runtimeSession, RuntimeTurnInput{
		Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID,
		RequestID: input.Mutation.RequestID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration,
		WorkspaceDirectory: coordinator.workspaceDirectory, ProviderKind: session.ProviderKind, ProviderResumeCursor: session.ProviderResumeCursor, Model: input.Model, RuntimeMode: input.RuntimeMode, InteractionMode: input.InteractionMode, InputText: input.InputText, OccurredAt: now,
	})
	if err != nil {
		if unregister() {
			return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, context.Canceled
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return coordinator.cancel(principalSource, input, started, runtimeResult.Messages)
		}
		return coordinator.fail(principalSource, input, started, runtimeResult.Messages, runtimeResult.FailureCode, err)
	}
	if runtimeResult.Terminal.MessageType == "Error" {
		code := runtimeFailureCode(runtimeResult.Terminal, "runtime_failed")
		return coordinator.fail(principalSource, input, started, runtimeResult.Messages, code, errors.New(code))
	}
	terminal := publicRuntimeMessage(runtimeResult.Terminal)
	digest, err := RuntimeMessageDigest(terminal)
	if err != nil {
		return coordinator.fail(principalSource, input, started, runtimeResult.Messages, "runtime_result_invalid", err)
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, err
	}
	completed, err := coordinator.store.CompleteManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, CompleteRuntimeExecutionInput{CompleteExecutionInput: CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, ResultDigest: digest, Mutation: input.Mutation}, ProviderResumeCursor: runtimeResult.ProviderResumeCursor, Messages: runtimeResult.Messages})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, err
	}
	return DurableRuntimeExecutionResult{Transition: completed, Messages: runtimeResult.Messages}, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) registerActiveExecution(key durableExecutionKey, cancel context.CancelFunc) (*activeDurableExecution, func() bool, error) {
	active := &activeDurableExecution{cancel: cancel, interactions: make(map[string]*activeRuntimeInteraction), controls: make(map[string]*activeRuntimeInteractionResolution)}
	coordinator.activeMu.Lock()
	if _, exists := coordinator.active[key]; exists {
		coordinator.activeMu.Unlock()
		return nil, nil, ErrDurableRuntimeExecutionConflict
	}
	coordinator.active[key] = active
	coordinator.activeMu.Unlock()
	return active, func() bool {
		coordinator.activeMu.Lock()
		defer coordinator.activeMu.Unlock()
		if coordinator.active[key] != active {
			return active.externallyCancelled
		}
		externallyCancelled := active.externallyCancelled
		failActiveInteractionResolutions(active, ErrRuntimeInteractionUnavailable)
		delete(coordinator.active, key)
		return externallyCancelled
	}, nil
}

func failActiveInteractionResolutions(active *activeDurableExecution, err error) {
	for commandID, resolution := range active.controls {
		delete(active.controls, commandID)
		if resolution.interaction.resolution == resolution {
			resolution.interaction.resolution = nil
		}
		resolution.err = err
		close(resolution.done)
	}
}

func (coordinator *DurableRuntimeExecutionCoordinator) failRuntimeInteractionResolution(key durableExecutionKey, active *activeDurableExecution, resolution *activeRuntimeInteractionResolution, err error) {
	coordinator.activeMu.Lock()
	defer coordinator.activeMu.Unlock()
	if coordinator.active[key] != active || active.controls[resolution.commandID] != resolution {
		return
	}
	delete(active.controls, resolution.commandID)
	if resolution.interaction.resolution == resolution {
		resolution.interaction.resolution = nil
	}
	resolution.err = err
	close(resolution.done)
}
func (coordinator *DurableRuntimeExecutionCoordinator) cancel(principalSource VerifiedPrincipalSource, input DurableRuntimeExecutionInput, started ExecutionTransitionResult, messages []runtimeprotocol.Message) (DurableRuntimeExecutionResult, error) {
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(context.Canceled, err)
	}
	cancelled, err := coordinator.store.CancelManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, CancelTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, TargetExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(context.Canceled, err)
	}
	return DurableRuntimeExecutionResult{Transition: cancelled, Messages: messages}, context.Canceled
}

func (coordinator *DurableRuntimeExecutionCoordinator) fail(principalSource VerifiedPrincipalSource, input DurableRuntimeExecutionInput, started ExecutionTransitionResult, messages []runtimeprotocol.Message, code string, cause error) (DurableRuntimeExecutionResult, error) {
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	failed, err := coordinator.store.FailManagedAgentExecution(context.Background(), input.Scope.TenantID, principal, FailRuntimeExecutionInput{FailExecutionInput: FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: coordinator.fencingGeneration, ErrorCode: code, Mutation: input.Mutation}, Messages: messages})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	return DurableRuntimeExecutionResult{Transition: failed, Messages: messages}, fmt.Errorf("%w: %v", ErrDurableRuntimeExecutionFailed, cause)
}

func nextVerifiedPrincipal(source VerifiedPrincipalSource) (*authn.VerifiedPrincipal, error) {
	if source == nil {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	principal, err := source()
	if err != nil {
		return nil, err
	}
	if principal == nil {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	return principal, nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) executeRuntimeTurn(ctx context.Context, key durableExecutionKey, active *activeDurableExecution, session *workerclient.RuntimeSession, input RuntimeTurnInput) (RuntimeTurnResult, error) {
	result := RuntimeTurnResult{FailureCode: "runtime_open_failed"}
	if session == nil {
		return result, ErrDurableRuntimeExecutionUnavailable
	}
	if err := ValidateProviderResumeCursor(input.ProviderResumeCursor); err != nil {
		result.FailureCode = "runtime_result_invalid"
		return result, err
	}
	paths, err := deriveRuntimeWorkspacePaths(input.WorkspaceDirectory, input.Scope, input.SessionID, input.TurnID, input.ExecutionID)
	if err != nil {
		result.FailureCode = "workspace_invalid"
		return result, err
	}
	closeSession := func() { _ = session.CloseRequest(); _ = session.CloseResponse() }
	coordinator.activeMu.Lock()
	if coordinator.active[key] != active || active.externallyCancelled {
		coordinator.activeMu.Unlock()
		return result, context.Canceled
	}
	active.send = session.Send
	active.stop = closeSession
	coordinator.activeMu.Unlock()
	command := func(commandType, commandID string, payload map[string]any) runtimeprotocol.Command {
		return runtimeprotocol.Command{RequestID: boundedRuntimeIdentifier(input.RequestID, strings.ToLower(commandType)), Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: input.ExecutionID, Generation: input.Generation, CommandType: commandType, CommandID: commandID, OccurredAt: input.OccurredAt.Format(time.RFC3339Nano), Payload: payload}
	}
	runnerInput := map[string]any{
		"workspaceDirectory":     paths.workspaceDirectory,
		"runtimeOutputDirectory": paths.runtimeOutputDirectory,
		"providerStateDirectory": paths.providerStateDirectory,
		"workload":               map[string]any{"provider": input.ProviderKind, "model": input.Model, "runtimeMode": input.RuntimeMode, "interactionMode": input.InteractionMode, "inputText": ""},
		"execution":              map[string]any{"id": input.ExecutionID},
	}
	sessionCommand, sessionSuffix := "StartSession", "start"
	if input.ProviderResumeCursor != "" {
		sessionCommand, sessionSuffix = "ResumeSession", "resume"
		runnerInput["providerResumeCursor"] = input.ProviderResumeCursor
	}
	start := command(sessionCommand, boundedRuntimeIdentifier(input.RequestID, sessionSuffix), map[string]any{"runnerInput": runnerInput})
	result.FailureCode = "runtime_start_failed"
	if err := session.Send(ctx, start); err != nil {
		return result, err
	}
	startMessages, terminal, err := receiveRuntimeMessages(session.Receive, start.CommandID, nil, nil)
	if err != nil {
		result.Messages = publicRuntimeMessages(startMessages)
		return result, err
	}
	if terminal.MessageType == "Error" {
		result.Messages = publicRuntimeMessages(startMessages)
		result.Terminal = terminal
		result.FailureCode = runtimeFailureCode(terminal, result.FailureCode)
		return result, errors.New("Runtime StartSession failed")
	}
	turn := command("SendTurn", boundedRuntimeIdentifier(input.RequestID, "turn"), map[string]any{"inputText": input.InputText})
	result.FailureCode = "runtime_turn_failed"
	if err := session.Send(ctx, turn); err != nil {
		return result, err
	}
	messages, terminal, err := receiveRuntimeMessages(session.Receive, turn.CommandID, func(message runtimeprotocol.Message) (bool, error) {
		return coordinator.routeRuntimeMessage(key, active, turn.CommandID, message)
	}, func(message runtimeprotocol.Message) {
		coordinator.recordActiveRuntimeMessage(key, active, message)
	})
	result.Messages = publicRuntimeMessages(messages)
	result.Terminal = terminal
	if err != nil {
		return result, err
	}
	providerResumeCursor, err := runtimeProviderResumeCursor(terminal)
	if err != nil {
		result.FailureCode = "runtime_result_invalid"
		return result, err
	}
	result.ProviderResumeCursor = providerResumeCursor
	result.FailureCode = ""
	return result, nil
}

func runtimeProviderResumeCursor(message runtimeprotocol.Message) (string, error) {
	value, exists := message.Payload["providerResumeCursor"]
	if !exists || value == nil {
		return "", nil
	}
	cursor, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%w: provider resume cursor", ErrInvalidInput)
	}
	if err := ValidateProviderResumeCursor(cursor); err != nil || cursor == "" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: provider resume cursor", ErrInvalidInput)
	}
	return cursor, nil
}

func publicRuntimeMessage(message runtimeprotocol.Message) runtimeprotocol.Message {
	if _, exists := message.Payload["providerResumeCursor"]; exists {
		message.Payload = maps.Clone(message.Payload)
		delete(message.Payload, "providerResumeCursor")
	}
	return message
}

func publicRuntimeMessages(messages []runtimeprotocol.Message) []runtimeprotocol.Message {
	result := make([]runtimeprotocol.Message, len(messages))
	for index, message := range messages {
		result[index] = publicRuntimeMessage(message)
	}
	return result
}

func runtimeFailureCode(message runtimeprotocol.Message, fallback string) string {
	if message.MessageType == "Error" && message.Error != nil && ValidRuntimeErrorCode(message.Error.Code) {
		return message.Error.Code
	}
	return fallback
}

func deriveRuntimeWorkspacePaths(base string, scope Scope, sessionID, turnID, executionID string) (runtimeWorkspacePaths, error) {
	if err := scope.validate(); err != nil {
		return runtimeWorkspacePaths{}, err
	}
	for value, field := range map[string]string{
		sessionID:   "session id",
		turnID:      "turn id",
		executionID: "execution id",
	} {
		if err := validateIdentifier(value, maxIdentifierBytes, field); err != nil {
			return runtimeWorkspacePaths{}, err
		}
	}
	if base == "" || strings.TrimSpace(base) != base || filepath.Clean(base) == string(filepath.Separator) || !filepath.IsAbs(base) || containsRuntimePathControl(base) {
		return runtimeWorkspacePaths{}, fmt.Errorf("%w: workspace directory", ErrInvalidInput)
	}
	base = filepath.Clean(base)
	sessionRoot := filepath.Join(base, ".cloud-agents", "managed-agent", "tenants", scope.TenantID, "projects", scope.ProjectID, "sessions", sessionID)
	return runtimeWorkspacePaths{
		workspaceDirectory:     filepath.Join(sessionRoot, "workspace"),
		runtimeOutputDirectory: filepath.Join(sessionRoot, "runtime-output", turnID, executionID),
		providerStateDirectory: filepath.Join(sessionRoot, "provider-state"),
	}, nil
}

func containsRuntimePathControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func boundedRuntimeIdentifier(base, suffix string) string {
	separator := "-"
	maximumBase := maxPublicExecutionMessageIdentifierBytes - len(separator) - len(suffix)
	if len(base) > maximumBase {
		base = base[:maximumBase]
	}
	return base + separator + suffix
}

func receiveRuntimeMessages(receive func() (runtimeprotocol.Message, error), commandID string, route func(runtimeprotocol.Message) (bool, error), accepted func(runtimeprotocol.Message)) ([]runtimeprotocol.Message, runtimeprotocol.Message, error) {
	messages := make([]runtimeprotocol.Message, 0, maxRuntimeExecutionMessages)
	totalBytes := 2
	for {
		message, err := receive()
		if err != nil {
			return messages, runtimeprotocol.Message{}, err
		}
		if message.CommandID != commandID && route != nil {
			handled, routeErr := route(message)
			if routeErr != nil {
				return messages, message, routeErr
			}
			if handled {
				continue
			}
		}
		if message.CommandID != commandID {
			return messages, message, errors.New("Runtime response command id mismatch")
		}
		encoded, err := json.Marshal(message)
		separatorBytes := 0
		if len(messages) > 0 {
			separatorBytes = 1
		}
		if err != nil || len(messages) == maxRuntimeExecutionMessages || totalBytes+len(encoded)+separatorBytes > runtimeprotocol.MaxMessageBytes {
			return messages, message, errors.New("Runtime response transcript exceeds the public limit")
		}
		if route != nil {
			handled, routeErr := route(message)
			if routeErr != nil {
				return messages, message, routeErr
			}
			if handled {
				continue
			}
		}
		totalBytes += len(encoded) + separatorBytes
		messages = append(messages, message)
		if accepted != nil {
			accepted(message)
		}
		if message.MessageType == "Result" || message.MessageType == "Error" {
			return messages, message, nil
		}
	}
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordActiveRuntimeMessage(key durableExecutionKey, active *activeDurableExecution, message runtimeprotocol.Message) {
	coordinator.activeMu.Lock()
	defer coordinator.activeMu.Unlock()
	if coordinator.active[key] == active {
		active.messages = append(active.messages, publicRuntimeMessage(message))
	}
}

func (coordinator *DurableRuntimeExecutionCoordinator) routeRuntimeMessage(key durableExecutionKey, active *activeDurableExecution, turnCommandID string, message runtimeprotocol.Message) (bool, error) {
	coordinator.activeMu.Lock()
	defer coordinator.activeMu.Unlock()
	if coordinator.active[key] != active {
		return false, ErrRuntimeInteractionUnavailable
	}
	if message.CommandID == turnCommandID {
		if message.MessageType != "InteractionRequest" {
			return false, nil
		}
		requestID, interactionType, err := runtimeInteractionIdentity(message)
		if err != nil {
			return false, err
		}
		if _, exists := active.interactions[requestID]; exists {
			return false, errors.New("Runtime reused an interaction request id")
		}
		active.interactions[requestID] = &activeRuntimeInteraction{interactionType: interactionType}
		return false, nil
	}
	resolution := active.controls[message.CommandID]
	if resolution == nil {
		return false, nil
	}
	if message.RequestID != resolution.requestID {
		return true, errors.New("Runtime interaction response request id mismatch")
	}
	if message.MessageType != "Result" && message.MessageType != "Error" {
		return true, nil
	}
	delete(active.controls, resolution.commandID)
	resolution.interaction.resolution = nil
	if message.MessageType == "Result" {
		resolution.interaction.resolvedPayload = resolution.payload
	} else {
		resolution.err = ErrRuntimeInteractionFailed
	}
	close(resolution.done)
	return true, nil
}

func runtimeInteractionIdentity(message runtimeprotocol.Message) (string, string, error) {
	requestID, requestOK := message.Payload["requestId"].(string)
	interactionType, typeOK := message.Payload["interactionType"].(string)
	if !requestOK || validateRuntimeInteractionToken(requestID, "interaction request id") != nil || !typeOK || interactionType != "approval" && interactionType != "user-input" {
		return "", "", errors.New("Runtime interaction request payload is invalid")
	}
	return requestID, interactionType, nil
}

func validateRuntimeInteractionToken(value, field string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRuntimeInteractionRequestIDCharacters {
		return fmt.Errorf("%w: %s", ErrInvalidInput, field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("%w: %s", ErrInvalidInput, field)
		}
	}
	return nil
}

func RuntimeMessageDigest(message runtimeprotocol.Message) (string, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateRuntimeTerminalMessage(input CompleteRuntimeExecutionInput) error {
	if err := validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, true); err != nil {
		return fmt.Errorf("%w: Runtime terminal message", ErrInvalidInput)
	}
	digest, err := RuntimeMessageDigest(input.Messages[len(input.Messages)-1])
	if err != nil || digest != input.ResultDigest {
		return fmt.Errorf("%w: Runtime terminal digest", ErrInvalidInput)
	}
	return nil
}

func validateRuntimeFailureMessages(input FailRuntimeExecutionInput) error {
	err := validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, false)
	if err == nil || input.ErrorCode != "runtime_result_invalid" {
		return err
	}
	return validateRuntimeMessageTranscript(input.Messages, input.ExecutionID, input.Generation, true)
}

func validateRuntimeMessageTranscript(messages []runtimeprotocol.Message, executionID string, generation uint64, requireResult bool) error {
	if len(messages) == 0 {
		if requireResult {
			return ErrInvalidInput
		}
		return nil
	}
	if len(messages) > maxRuntimeExecutionMessages {
		return ErrInvalidInput
	}
	requestID, commandID := messages[0].RequestID, messages[0].CommandID
	for index, message := range messages {
		terminal := message.MessageType == "Result" || message.MessageType == "Error"
		if runtimeprotocol.ValidateMessage(message) != nil || message.ExecutionID != executionID || message.Generation != generation || message.RequestID != requestID || message.CommandID != commandID || terminal && index != len(messages)-1 || !requireResult && message.MessageType == "Result" {
			return ErrInvalidInput
		}
	}
	encoded, err := json.Marshal(messages)
	if err != nil || len(encoded) > runtimeprotocol.MaxMessageBytes || requireResult && messages[len(messages)-1].MessageType != "Result" {
		return ErrInvalidInput
	}
	return nil
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
