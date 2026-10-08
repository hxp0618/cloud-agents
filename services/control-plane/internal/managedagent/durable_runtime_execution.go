package managedagent

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
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
	ResolveManagedAgentCapabilityBindings(context.Context, string, *authn.VerifiedPrincipal, string, string, string, []McpServerRef, []SkillBundleRef) ([]*workerruntimev1alpha1.RuntimeCapabilityBinding, string, error)
	RecordManagedAgentCapabilityEvent(context.Context, string, *authn.VerifiedPrincipal, CapabilityEventInput) error
	GetManagedAgentSessionForArtifact(context.Context, string, *authn.VerifiedPrincipal, string, string) (RuntimeSessionSnapshot, error)
	FindManagedAgentTurnForExecution(context.Context, string, *authn.VerifiedPrincipal, string, string, string) (TurnSnapshot, bool, error)
	CreateManagedAgentTurn(context.Context, string, *authn.VerifiedPrincipal, CreateTurnInput) (TurnSnapshot, error)
	CreateManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CreateExecutionInput) (ExecutionSnapshot, error)
	ClaimManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, ClaimRuntimeExecutionInput) (RuntimeExecutionClaim, error)
	RenewManagedAgentExecutionClaim(context.Context, string, *authn.VerifiedPrincipal, RuntimeExecutionClaim, int32) (time.Time, error)
	ReleaseQueuedManagedAgentExecutionClaim(context.Context, string, *authn.VerifiedPrincipal, RuntimeExecutionClaim) error
	CheckpointManagedAgentExecution(context.Context, string, *authn.VerifiedPrincipal, CheckpointRuntimeExecutionInput) (RuntimeExecutionCheckpoint, error)
	ResolveManagedAgentExecutionInteraction(context.Context, string, *authn.VerifiedPrincipal, ResolveRuntimeInteractionInput) (RuntimeInteractionResolution, error)
	ReconcileManagedAgentExecutionSideEffect(context.Context, string, *authn.VerifiedPrincipal, ReconcileRuntimeSideEffectInput) (RuntimeSideEffectReconciliation, error)
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
	Store                   DurableRuntimeExecutionStore
	Supervisor              *workerclient.Supervisor
	FoundationRuntime       *FoundationRuntime
	WorkerClientCertificate tls.Certificate
	WorkerRootCAs           *x509.CertPool
	Clock                   Clock
	FencingLeaseID          string
	FencingGeneration       uint64
	FencingToken            []byte
	WorkspaceDirectory      string
	MaxDuration             time.Duration
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
	McpServerRefs   []McpServerRef
	SkillBundleRefs []SkillBundleRef
	Mutation        Mutation
}

type DurableRuntimeExecutionResult struct {
	Transition ExecutionTransitionResult
	Messages   []runtimeprotocol.Message
}

type RuntimeTurnInput struct {
	Scope                    Scope
	SessionID                string
	TurnID                   string
	RequestID                string
	ExecutionID              string
	Generation               uint64
	WorkspaceDirectory       string
	ProviderKind             string
	ProviderResumeCursor     string
	Model                    string
	RuntimeMode              string
	InteractionMode          string
	InputText                string
	OccurredAt               time.Time
	ResumeSnapshot           map[string]any
	ResolvedInteractions     []RuntimeInteractionResolution
	SideEffectReconciliation *RuntimeSideEffectReconciliation
}

type RuntimeExecutionClaim struct {
	RuntimeExecutionReference
	Acquired                       bool
	AttemptNumber                  uint64
	HolderID                       string
	Incarnation                    string
	Token                          string
	ExpiresAt                      time.Time
	RecoveryState                  string
	RecoveryReason                 string
	RecoveryMode                   string
	RecoverySourceTargetID         string
	RecoveryTargetID               string
	CheckpointSequence             uint64
	CheckpointDigest               string
	CheckpointedAt                 time.Time
	CheckpointProtocol             string
	CheckpointProviderResumeCursor string
	PendingSideEffect              bool
	PendingInteractionCount        uint32
}

type ClaimRuntimeExecutionInput struct {
	RuntimeExecutionReference
	HolderID     string
	Incarnation  string
	Token        string
	LeaseSeconds int32
}

type CheckpointRuntimeExecutionInput struct {
	Claim                RuntimeExecutionClaim
	Messages             []runtimeprotocol.Message
	ProviderResumeCursor string
	Protocol             string
	PendingSideEffect    bool
	PendingInteractions  uint32
}

type RuntimeExecutionCheckpoint struct {
	Sequence  uint64
	Digest    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type ResolveRuntimeInteractionInput struct {
	RuntimeExecutionReference
	InteractionRequestID string
	InteractionType      string
	RequestID            string
	Payload              map[string]any
}

type RuntimeInteractionResolution struct {
	InteractionRequestID string
	InteractionType      string
	RequestID            string
	Digest               string
	Payload              map[string]any
	CreatedAt            time.Time
}

type ReconcileRuntimeSideEffectInput struct {
	RuntimeExecutionReference
	CheckpointDigest string
	Outcome          string
	RequestID        string
	IdempotencyKey   string
}

type RuntimeSideEffectReconciliation struct {
	CheckpointDigest string
	Outcome          string
	Digest           string
	CreatedAt        time.Time
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
	FileName    string
	ContentType string
	SHA256      string
}

type RuntimeApprovalResolutionInput struct {
	RuntimeExecutionReference
	Principal            *authn.VerifiedPrincipal
	RequestID            string
	InteractionRequestID string
	Decision             string
}

type RuntimeUserInputResolutionInput struct {
	RuntimeExecutionReference
	Principal            *authn.VerifiedPrincipal
	RequestID            string
	InteractionRequestID string
	Answers              map[string][]string
}

type DurableRuntimeExecutionCoordinator struct {
	store                   DurableRuntimeExecutionStore
	supervisor              *workerclient.Supervisor
	foundationRuntime       *FoundationRuntime
	workerClientCertificate tls.Certificate
	workerRootCAs           *x509.CertPool
	now                     Clock
	fencingLeaseID          string
	fencingGeneration       uint64
	fencingToken            []byte
	workspaceDirectory      string
	maxDuration             time.Duration
	claimHolder             string
	claimIncarnation        string
	claimRenewInterval      time.Duration
	persistenceTimeout      time.Duration
	activeMu                sync.Mutex
	active                  map[durableExecutionKey]*activeDurableExecution
}

type runtimeWorker struct {
	supervisor *workerclient.Supervisor
	foundation *FoundationRuntime
	session    RuntimeSessionSnapshot
	leaseID    string
	generation uint64
}

type runtimeSession interface {
	Send(context.Context, runtimeprotocol.Command) error
	Receive() (runtimeprotocol.Message, error)
	CloseRequest() error
	CloseResponse() error
}

// runtimeSessionHealth is optional because a session transport may not expose
// a separate health endpoint. Foundation PTY sessions do: checking the PTY
// after the stream is open detects a dead runtime process even when the
// websocket has not closed yet.
type runtimeSessionHealth interface {
	checkHealth(context.Context) error
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
	// Recovery appends the bounded transcript from the expired attempt to the
	// current attempt before settling. Keep that transcript bounded while
	// leaving room for a second attempt's provider events.
	maxRuntimeExecutionMessages              = 128
	maxRuntimeInteractionRequestIDCharacters = 200
	maxRuntimeInteractionAnswers             = 3
	maxRuntimeInteractionAnswerValues        = 20
	maxRuntimeInteractionAnswerCharacters    = 2000
	runtimeExecutionClaimLeaseSeconds        = 30
	runtimeExecutionClaimRenewInterval       = 10 * time.Second
	runtimeExecutionCheckpointProtocol       = "runtime-message-checkpoint-v1"
	runtimeWorkerReconnectWindow             = 30 * time.Second
	runtimeWorkerReconnectInterval           = 250 * time.Millisecond
	maximumDurableRuntimeExecutionDuration   = 30 * time.Minute
)

var (
	ErrDurableRuntimeExecutionUnavailable = errors.New("durable Runtime execution is unavailable")
	ErrDurableRuntimeExecutionConflict    = errors.New("durable Runtime execution is already active")
	ErrDurableRuntimeExecutionFailed      = errors.New("durable Runtime execution failed")
	ErrRuntimeEnvironmentUnavailable      = errors.New("Runtime environment is unavailable")
	ErrRuntimeCapacityExhausted           = errors.New("Runtime session capacity is exhausted")
	ErrRuntimeInteractionUnavailable      = errors.New("Runtime interaction is unavailable")
	ErrRuntimeInteractionConflict         = errors.New("Runtime interaction resolution conflicts with active state")
	ErrRuntimeInteractionFailed           = errors.New("Runtime interaction resolution failed")
	ErrRuntimeArtifactUnavailable         = errors.New("Runtime artifact is unavailable")
	ErrRuntimeRecoveryRequiresUserAction  = errors.New("Runtime recovery requires reconciliation")
)

func NewDurableRuntimeExecutionCoordinator(config DurableRuntimeExecutionConfig) (*DurableRuntimeExecutionCoordinator, error) {
	staticWorker := config.Supervisor != nil && config.FencingLeaseID != "" && config.FencingGeneration > 0
	dynamicWorker := len(config.WorkerClientCertificate.Certificate) > 0 && config.WorkerClientCertificate.PrivateKey != nil && config.WorkerRootCAs != nil
	foundationRuntime := config.FoundationRuntime != nil
	if config.Store == nil || config.Clock == nil || strings.TrimSpace(config.WorkspaceDirectory) == "" || !foundationRuntime && (!staticWorker && !dynamicWorker || len(config.FencingToken) == 0) {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	if config.MaxDuration <= 0 || config.MaxDuration > maximumDurableRuntimeExecutionDuration {
		config.MaxDuration = maximumDurableRuntimeExecutionDuration
	}
	claimIncarnation, err := randomRuntimeIdentifier("inc")
	if err != nil {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	return &DurableRuntimeExecutionCoordinator{
		store: config.Store, supervisor: config.Supervisor, foundationRuntime: config.FoundationRuntime, now: config.Clock,
		workerClientCertificate: config.WorkerClientCertificate, workerRootCAs: config.WorkerRootCAs,
		fencingLeaseID: config.FencingLeaseID, fencingGeneration: config.FencingGeneration,
		fencingToken: append([]byte(nil), config.FencingToken...), workspaceDirectory: config.WorkspaceDirectory,
		maxDuration: config.MaxDuration, claimHolder: "runtime-executor", claimIncarnation: claimIncarnation,
		active: make(map[durableExecutionKey]*activeDurableExecution),
	}, nil
}

func randomRuntimeIdentifier(prefix string) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(buffer), nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) workerForSession(session RuntimeSessionSnapshot) (runtimeWorker, error) {
	if session.SandboxID != "" {
		if coordinator.foundationRuntime == nil || !session.FoundationSandboxReady || session.SandboxGeneration == 0 {
			return runtimeWorker{}, ErrRuntimeEnvironmentUnavailable
		}
		return runtimeWorker{foundation: coordinator.foundationRuntime, session: session, generation: session.SandboxGeneration}, nil
	}
	if session.EnvironmentLeaseID == "" {
		if coordinator.supervisor == nil || coordinator.fencingLeaseID == "" || coordinator.fencingGeneration == 0 || len(coordinator.fencingToken) == 0 {
			return runtimeWorker{}, ErrRuntimeEnvironmentUnavailable
		}
		return runtimeWorker{supervisor: coordinator.supervisor, leaseID: coordinator.fencingLeaseID, generation: coordinator.fencingGeneration}, nil
	}
	if !session.EnvironmentReady || session.EnvironmentGeneration == 0 || coordinator.workerRootCAs == nil || len(coordinator.workerClientCertificate.Certificate) == 0 || coordinator.workerClientCertificate.PrivateKey == nil || len(coordinator.fencingToken) == 0 {
		return runtimeWorker{}, ErrRuntimeEnvironmentUnavailable
	}
	parsed, err := url.Parse(session.WorkerSPIFFEID)
	if err != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || parsed.Path == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return runtimeWorker{}, ErrRuntimeEnvironmentUnavailable
	}
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: session.WorkerSPIFFEID, TrustDomain: parsed.Host}
	// ponytail: create one Supervisor per execution; add a bounded route cache only after measured connection churn.
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{
		Endpoint: session.WorkerEndpoint, ExpectedWorkerIdentity: identity,
		ClientCertificate: coordinator.workerClientCertificate, RootCAs: coordinator.workerRootCAs,
		ServerName: session.WorkerServerName, Clock: workerclient.Clock(coordinator.now),
	})
	if err != nil {
		return runtimeWorker{}, ErrRuntimeEnvironmentUnavailable
	}
	return runtimeWorker{supervisor: supervisor, leaseID: session.EnvironmentLeaseID, generation: session.EnvironmentGeneration}, nil
}

func runtimeWorkspaceDirectoryForWorker(worker runtimeWorker, coordinatorWorkspace string) string {
	if worker.foundation != nil {
		return "/workspace"
	}
	return coordinatorWorkspace
}

func (worker runtimeWorker) open(ctx context.Context, principalSource VerifiedPrincipalSource, tenantID, executionID, providerKind string, fencingToken []byte) (runtimeSession, error) {
	if worker.foundation != nil {
		return worker.foundation.open(ctx, principalSource, worker.session, executionID)
	}
	return worker.supervisor.OpenRuntimeSession(ctx, tenantID, executionID, providerKind, worker.generation, &workerv1alpha1.FencingProof{
		LeaseId: worker.leaseID, Generation: worker.generation, Token: append([]byte(nil), fencingToken...),
	}, workerclient.RuntimeSessionOptions{CapabilityBindings: worker.session.CapabilityBindings, CapabilityManifestDigest: worker.session.CapabilityManifestDigest})
}

func (worker runtimeWorker) checkHealth(ctx context.Context) error {
	if worker.supervisor == nil {
		return nil
	}
	return worker.supervisor.CheckRuntimeHealthStrict(ctx)
}

func runtimeSessionHealthCheck(ctx context.Context, worker runtimeWorker, session runtimeSession) error {
	if checked, ok := session.(runtimeSessionHealth); ok {
		return checked.checkHealth(ctx)
	}
	return worker.checkHealth(ctx)
}

func (worker runtimeWorker) readArtifact(ctx context.Context, principalSource VerifiedPrincipalSource, executionID string, fencingToken []byte, rootDirectory, relativePath string, expectedSize *uint64, expectedSHA256 string) ([]byte, error) {
	if worker.foundation != nil {
		return worker.foundation.readArtifact(ctx, principalSource, worker.session, rootDirectory, relativePath, expectedSize, expectedSHA256)
	}
	return worker.supervisor.ReadRuntimeArtifact(ctx, executionID, worker.generation, &workerv1alpha1.FencingProof{
		LeaseId: worker.leaseID, Generation: worker.generation, Token: append([]byte(nil), fencingToken...),
	}, rootDirectory, relativePath, expectedSize, expectedSHA256)
}

func openRuntimeSessionWithRetry(ctx context.Context, open func(context.Context) (runtimeSession, error)) (runtimeSession, error) {
	if ctx == nil || open == nil {
		return nil, ErrDurableRuntimeExecutionUnavailable
	}
	retryDeadline := time.Now().Add(runtimeWorkerReconnectWindow)
	var lastErr error
	for {
		session, err := open(ctx)
		if err == nil {
			return session, nil
		}
		lastErr = err
		if !retryableRuntimeOpenError(err) || ctx.Err() != nil {
			return nil, lastErr
		}
		remaining := time.Until(retryDeadline)
		if remaining <= 0 {
			return nil, lastErr
		}
		interval := runtimeWorkerReconnectInterval
		if remaining < interval {
			interval = remaining
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, lastErr
		case <-timer.C:
		}
	}
}

func retryableRuntimeOpenError(err error) bool {
	code := connect.CodeOf(err)
	return code == connect.CodeUnavailable || code == connect.CodeDeadlineExceeded
}

func recoverableRuntimeTransportError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, workerclient.ErrRuntimeProcessUnavailable) || errors.Is(err, ErrDurableRuntimeExecutionUnavailable) || retryableRuntimeOpenError(err)
}

func (coordinator *DurableRuntimeExecutionCoordinator) Cancel(ctx context.Context, principal *authn.VerifiedPrincipal, input CancelTurnInput) (ExecutionTransitionResult, error) {
	if coordinator == nil || coordinator.store == nil {
		return ExecutionTransitionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	if ctx == nil {
		return ExecutionTransitionResult{}, ErrNilContext
	}
	persistenceCtx, cancel := coordinator.persistenceContext(ctx)
	result, err := coordinator.store.CancelManagedAgentExecution(persistenceCtx, input.Scope.TenantID, principal, input)
	cancel()
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

func (coordinator *DurableRuntimeExecutionCoordinator) ResolveApproval(ctx context.Context, input RuntimeApprovalResolutionInput) error {
	if input.Decision != "accept" && input.Decision != "decline" {
		return ErrInvalidInput
	}
	payload := map[string]any{"requestId": input.InteractionRequestID, "resolution": map[string]any{"decision": input.Decision}}
	return coordinator.resolveRuntimeInteraction(ctx, input.Principal, input.RuntimeExecutionReference, input.RequestID, input.InteractionRequestID, "approval", "ResolveApproval", payload)
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
	return coordinator.resolveRuntimeInteraction(ctx, input.Principal, input.RuntimeExecutionReference, input.RequestID, input.InteractionRequestID, "user-input", "ResolveUserInput", payload)
}

func (coordinator *DurableRuntimeExecutionCoordinator) resolveRuntimeInteraction(ctx context.Context, principal *authn.VerifiedPrincipal, reference RuntimeExecutionReference, requestID, interactionRequestID, interactionType, commandType string, payload map[string]any) error {
	if coordinator == nil || coordinator.store == nil || ctx == nil || principal == nil || coordinator.now == nil || validateIdentifier(requestID, maxIdentifierBytes, "request id") != nil || validateRuntimeInteractionToken(interactionRequestID, "interaction request id") != nil {
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
	if _, err := coordinator.store.ResolveManagedAgentExecutionInteraction(ctx, reference.Scope.TenantID, principal, ResolveRuntimeInteractionInput{
		RuntimeExecutionReference: reference, InteractionRequestID: interactionRequestID,
		InteractionType: interactionType, RequestID: requestID, Payload: payload,
	}); err != nil {
		return err
	}

	coordinator.activeMu.Lock()
	active := coordinator.active[key]
	if active == nil || active.send == nil {
		coordinator.activeMu.Unlock()
		return nil
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
	if coordinator == nil || coordinator.store == nil || coordinator.now == nil {
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
	if input.Mutation.RequestID == "" || input.Mutation.IdempotencyKey == "" {
		return DurableRuntimeExecutionResult{}, ErrInvalidInput
	}
	// The durable execution outlives an SDK HTTP stream. Explicit cancel and
	// interrupt endpoints remain the task cancellation authority.
	runCtx, cancel := context.WithTimeout(context.Background(), coordinator.maxDuration)
	defer cancel()
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	session, err := coordinator.store.GetManagedAgentSessionForExecution(runCtx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	worker, err := coordinator.workerForSession(session)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	runtimeCtx, runtimeCancel := context.WithCancel(runCtx)
	key := durableExecutionKey{tenantID: input.Scope.TenantID, projectID: input.Scope.ProjectID, sessionID: input.SessionID, turnID: input.TurnID, executionID: input.ExecutionID, generation: worker.generation}
	active, unregister, err := coordinator.registerActiveExecution(key, runtimeCancel)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	defer func() {
		unregister()
		runtimeCancel()
	}()
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
	execution, err := coordinator.store.CreateManagedAgentExecution(runCtx, input.Scope.TenantID, principal, CreateExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: turn.TurnID, ExecutionID: input.ExecutionID, Generation: worker.generation, McpServerRefs: input.McpServerRefs, SkillBundleRefs: input.SkillBundleRefs, Mutation: input.Mutation})
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	// Execution references are authoritative for this attempt. Resolve them
	// after the idempotent create so retries use the persisted binding set and
	// revoked capabilities fail closed before any Runtime side effect.
	if len(execution.McpServerRefs) != 0 || len(execution.SkillBundleRefs) != 0 {
		principal, err = nextVerifiedPrincipal(principalSource)
		if err != nil {
			return DurableRuntimeExecutionResult{}, err
		}
		session.CapabilityBindings, session.CapabilityManifestDigest, err = coordinator.store.ResolveManagedAgentCapabilityBindings(runCtx, input.Scope.TenantID, principal, input.Scope.ProjectID, input.SessionID, session.ProviderKind, execution.McpServerRefs, execution.SkillBundleRefs)
		if err != nil {
			if eventErr := coordinator.recordCapabilityResolutionFailureEvent(runCtx, principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, err); eventErr != nil {
				err = errors.Join(err, eventErr)
			}
			return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
		}
		// workerForSession snapshots the Session. Keep that snapshot aligned with
		// the persisted Execution pins before opening any Runtime transport.
		worker.session = session
		if err := coordinator.recordCapabilityAdmissionEvents(runCtx, principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings); err != nil {
			return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
		}
	}
	recoveringExecution := execution.State == ExecutionRunning
	if execution.State == ExecutionRunning {
		if len(execution.Messages) == 0 {
			execution.RecoveryState = "awaiting_reconciliation"
			execution.RecoveryReason = "checkpoint_missing"
		}
	}
	if execution.State != ExecutionQueued && execution.State != ExecutionRunning {
		if err := coordinator.recordCapabilityRuntimeOutcomeEvents(runCtx, principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, execution.Messages); err != nil {
			return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
		}
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, nil
	}
	fenceRuntimeWriter := func() error {
		fenced, fenceErr := openRuntimeSessionWithRetry(runtimeCtx, func(openContext context.Context) (runtimeSession, error) {
			return worker.open(openContext, principalSource, input.Scope.TenantID, input.ExecutionID, session.ProviderKind, coordinator.fencingToken)
		})
		if fenceErr != nil || fenced == nil {
			return fmt.Errorf("%w: %v", ErrRuntimeEnvironmentUnavailable, fenceErr)
		}
		_ = fenced.CloseRequest()
		_ = fenced.CloseResponse()
		return nil
	}
	_, unresolvedInteractions := runtimeCheckpointState(execution.Messages, execution.ResolvedInteractions, 0)
	if recoveringExecution && !execution.PendingSideEffect && execution.PendingInteractionCount > 0 && !execution.ClaimExpiresAt.After(now) {
		if err := fenceRuntimeWriter(); err != nil {
			return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
		}
		if unresolvedInteractions > 0 {
			return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, ErrRuntimeRecoveryRequiresUserAction
		}
	}
	claimToken, err := randomRuntimeIdentifier("claim")
	if err != nil {
		return DurableRuntimeExecutionResult{}, ErrDurableRuntimeExecutionUnavailable
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{}, err
	}
	claim, err := coordinator.store.ClaimManagedAgentExecution(runCtx, input.Scope.TenantID, principal, ClaimRuntimeExecutionInput{
		RuntimeExecutionReference: RuntimeExecutionReference{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: worker.generation},
		HolderID:                  coordinator.claimHolder, Incarnation: coordinator.claimIncarnation, Token: claimToken,
		LeaseSeconds: runtimeExecutionClaimLeaseSeconds,
	})
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
	}
	execution.AttemptNumber, execution.ClaimExpiresAt = claim.AttemptNumber, claim.ExpiresAt
	execution.RecoveryState, execution.RecoveryReason = claim.RecoveryState, claim.RecoveryReason
	execution.RecoveryMode, execution.RecoverySourceTargetID, execution.RecoveryTargetID = claim.RecoveryMode, claim.RecoverySourceTargetID, claim.RecoveryTargetID
	if !claim.Acquired {
		if recoveringExecution && claim.RecoveryState == "awaiting_reconciliation" && claim.PendingSideEffect {
			if err := fenceRuntimeWriter(); err != nil {
				return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, err
			}
		}
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}, Messages: execution.Messages}, ErrRuntimeRecoveryRequiresUserAction
	}
	var runtimeSessionMu sync.RWMutex
	var activeRuntimeSession runtimeSession
	runtimeHealth := func(healthContext context.Context) error {
		runtimeSessionMu.RLock()
		current := activeRuntimeSession
		runtimeSessionMu.RUnlock()
		return runtimeSessionHealthCheck(healthContext, worker, current)
	}
	heartbeatCtx, heartbeatCancel := context.WithCancel(context.Background())
	healthCtx, stopHealth := context.WithCancel(runtimeCtx)
	defer stopHealth()
	claimFailure := coordinator.startRuntimeClaimHeartbeat(heartbeatCtx, healthCtx, runtimeCancel, principalSource, claim, runtimeHealth)
	coordinator.activeMu.Lock()
	if coordinator.active[key] == active {
		active.cancel = func() {
			runtimeCancel()
			heartbeatCancel()
		}
	}
	coordinator.activeMu.Unlock()
	var heartbeatStopOnce sync.Once
	var heartbeatStopErr error
	stopClaimHeartbeat := func() error {
		heartbeatStopOnce.Do(func() {
			heartbeatStopErr = stopRuntimeClaimHeartbeat(heartbeatCancel, runtimeCancel, claimFailure)
		})
		return heartbeatStopErr
	}
	defer stopClaimHeartbeat()
	coordinator.activeMu.Lock()
	if coordinator.active[key] == active {
		active.messages = compactRuntimeMessages(execution.Messages)
		hydrateActiveRuntimeInteractions(active, active.messages, execution.ResolvedInteractions)
	}
	coordinator.activeMu.Unlock()
	// Claim Worker capacity before the durable running transition so saturation remains replayable as queued.
	runtimeSession, openErr := openRuntimeSessionWithRetry(runtimeCtx, func(openContext context.Context) (runtimeSession, error) {
		return worker.open(openContext, principalSource, input.Scope.TenantID, input.ExecutionID, session.ProviderKind, coordinator.fencingToken)
	})
	if connect.CodeOf(openErr) == connect.CodeResourceExhausted {
		if execution.State == ExecutionQueued {
			coordinator.releaseQueuedRuntimeClaim(principalSource, claim)
		}
		return DurableRuntimeExecutionResult{Transition: ExecutionTransitionResult{Turn: turn, Execution: execution}}, fmt.Errorf("%w: %v", ErrRuntimeCapacityExhausted, openErr)
	}
	if runtimeSession != nil {
		defer func() {
			runtimeSessionMu.Lock()
			activeRuntimeSession = nil
			runtimeSessionMu.Unlock()
			_ = runtimeSession.CloseRequest()
			_ = runtimeSession.CloseResponse()
		}()
		runtimeSessionMu.Lock()
		activeRuntimeSession = runtimeSession
		runtimeSessionMu.Unlock()
	}
	started := ExecutionTransitionResult{Turn: turn, Execution: execution}
	if execution.State == ExecutionQueued {
		principal, err = nextVerifiedPrincipal(principalSource)
		if err != nil {
			return DurableRuntimeExecutionResult{}, err
		}
		started, err = coordinator.store.StartManagedAgentExecution(runCtx, input.Scope.TenantID, principal, StartExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: worker.generation, Mutation: input.Mutation, Claim: claim})
		if err != nil {
			return DurableRuntimeExecutionResult{}, err
		}
	}
	started.Execution.AttemptNumber = claim.AttemptNumber
	started.Execution.ClaimExpiresAt = claim.ExpiresAt
	started.Execution.RecoveryState = claim.RecoveryState
	started.Execution.RecoveryReason = claim.RecoveryReason
	started.Execution.RecoveryMode = claim.RecoveryMode
	started.Execution.RecoverySourceTargetID = claim.RecoverySourceTargetID
	started.Execution.RecoveryTargetID = claim.RecoveryTargetID
	settleFailure := func(messages []runtimeprotocol.Message, code string, cause error) (DurableRuntimeExecutionResult, error) {
		if claimErr := knownRuntimeClaimFailure(claimFailure); claimErr != nil {
			return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, claimErr
		}
		failed, failErr := coordinator.fail(principalSource, input, started, messages, code, cause, claim)
		claimErr := stopClaimHeartbeat()
		if failed.Transition.Execution.State != ExecutionFailed {
			failErr = errors.Join(failErr, claimErr)
		}
		return failed, failErr
	}
	if started.Execution.State != ExecutionRunning {
		return DurableRuntimeExecutionResult{Transition: started}, nil
	}
	if openErr != nil {
		if recoveringExecution {
			return DurableRuntimeExecutionResult{Transition: started, Messages: execution.Messages}, fmt.Errorf("%w: %v", ErrRuntimeEnvironmentUnavailable, openErr)
		}
		stopHealth()
		return settleFailure(nil, "runtime_open_failed", openErr)
	}
	providerResumeCursor := session.ProviderResumeCursor
	if claim.CheckpointProviderResumeCursor != "" {
		providerResumeCursor = claim.CheckpointProviderResumeCursor
	}
	runtimeInput := RuntimeTurnInput{
		Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID,
		RequestID: input.Mutation.RequestID, ExecutionID: input.ExecutionID, Generation: worker.generation,
		WorkspaceDirectory: runtimeWorkspaceDirectoryForWorker(worker, coordinator.workspaceDirectory), ProviderKind: session.ProviderKind, ProviderResumeCursor: providerResumeCursor, Model: input.Model, RuntimeMode: input.RuntimeMode, InteractionMode: input.InteractionMode, InputText: input.InputText, OccurredAt: now,
		ResolvedInteractions: execution.ResolvedInteractions, SideEffectReconciliation: execution.SideEffectReconciliation,
	}
	runtimeInput.ResumeSnapshot = runtimeResumeSnapshot(runtimeInput, claim, execution.Messages, execution.ResolvedInteractions)
	runtimeResult, err := coordinator.executeRuntimeTurn(runtimeCtx, principalSource, &claim, execution.Messages, key, active, runtimeSession, runtimeInput)
	stopHealth()
	runtimeSessionMu.Lock()
	activeRuntimeSession = nil
	runtimeSessionMu.Unlock()
	reconciledThrough := 0
	if execution.SideEffectReconciliation != nil {
		reconciledThrough = len(compactRuntimeMessages(execution.Messages))
	}
	if pending, _ := runtimeCheckpointState(runtimeResult.Messages, execution.ResolvedInteractions, reconciledThrough); pending {
		claim.PendingSideEffect = true
	}
	preservePendingSideEffect := func(failureCode string) (DurableRuntimeExecutionResult, error) {
		// An unknown external side effect must stop renewing the execution claim
		// immediately; reconciliation owns the next attempt.
		_ = stopClaimHeartbeat()
		pendingErr := fmt.Errorf("%w: side_effect_outcome_unknown", ErrRuntimeRecoveryRequiresUserAction)
		if eventErr := coordinator.recordCapabilityExecutionFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, failureCode); eventErr != nil {
			pendingErr = errors.Join(pendingErr, eventErr)
		}
		if eventErr := coordinator.recordCapabilityRuntimeOutcomeEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			pendingErr = errors.Join(pendingErr, eventErr)
		}
		if eventErr := coordinator.recordPendingCapabilityFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			pendingErr = errors.Join(pendingErr, eventErr)
		}
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, pendingErr
	}
	if err != nil {
		if claimErr := knownRuntimeClaimFailure(claimFailure); claimErr != nil {
			return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, claimErr
		}
		if unregister() {
			return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, context.Canceled
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return coordinator.cancel(principalSource, input, started, runtimeResult.Messages)
		}
		if claim.PendingSideEffect {
			return preservePendingSideEffect(runtimeResult.FailureCode)
		}
		if claim.CheckpointSequence > 0 && recoverableRuntimeTransportError(err) {
			return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, fmt.Errorf("%w: %v", ErrRuntimeEnvironmentUnavailable, err)
		}
		failed, failErr := settleFailure(runtimeResult.Messages, runtimeResult.FailureCode, err)
		if eventErr := coordinator.recordCapabilityExecutionFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.FailureCode); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		if eventErr := coordinator.recordCapabilityRuntimeOutcomeEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		if eventErr := coordinator.recordPendingCapabilityFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		return failed, failErr
	}
	if runtimeResult.Terminal.MessageType == "Error" {
		code := runtimeFailureCode(runtimeResult.Terminal, "runtime_failed")
		if claim.PendingSideEffect {
			return preservePendingSideEffect(code)
		}
		failed, failErr := settleFailure(runtimeResult.Messages, code, errors.New(code))
		if eventErr := coordinator.recordCapabilityExecutionFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, code); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		if eventErr := coordinator.recordCapabilityRuntimeOutcomeEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		if eventErr := coordinator.recordPendingCapabilityFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
			failErr = errors.Join(failErr, eventErr)
		}
		return failed, failErr
	}
	if claim.PendingSideEffect {
		return preservePendingSideEffect("")
	}
	terminal := publicRuntimeMessage(runtimeResult.Terminal)
	digest, err := RuntimeMessageDigest(terminal)
	if err != nil {
		return settleFailure(runtimeResult.Messages, "runtime_result_invalid", err)
	}
	principal, err = nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, err
	}
	if claimErr := knownRuntimeClaimFailure(claimFailure); claimErr != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, claimErr
	}
	var completed ExecutionTransitionResult
	settlementCtx, cancelSettlement := coordinator.persistenceContext(context.Background())
	completed, err = coordinator.store.CompleteManagedAgentExecution(settlementCtx, input.Scope.TenantID, principal, CompleteRuntimeExecutionInput{CompleteExecutionInput: CompleteExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: worker.generation, ResultDigest: digest, Mutation: input.Mutation}, ProviderResumeCursor: runtimeResult.ProviderResumeCursor, Messages: runtimeResult.Messages, Claim: claim})
	cancelSettlement()
	if err != nil {
		if errors.Is(err, ErrInvalidInput) {
			failed, failErr := settleFailure(runtimeResult.Messages, "runtime_result_invalid", err)
			if failErr == nil {
				failErr = ErrDurableRuntimeExecutionFailed
			}
			if eventErr := coordinator.recordCapabilityExecutionFailureEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, "runtime_result_invalid"); eventErr != nil {
				failErr = errors.Join(failErr, eventErr)
			}
			if eventErr := coordinator.recordCapabilityRuntimeOutcomeEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); eventErr != nil {
				failErr = errors.Join(failErr, eventErr)
			}
			return failed, errors.Join(err, failErr)
		}
		return DurableRuntimeExecutionResult{Transition: started, Messages: runtimeResult.Messages}, errors.Join(err, stopClaimHeartbeat())
	}
	_ = stopClaimHeartbeat()
	completed.Execution.AttemptNumber = claim.AttemptNumber
	completed.Execution.RecoveryMode = claim.RecoveryMode
	completed.Execution.RecoverySourceTargetID = claim.RecoverySourceTargetID
	completed.Execution.RecoveryTargetID = claim.RecoveryTargetID
	if claim.AttemptNumber > 1 {
		completed.Execution.RecoveryState = "recovered"
	}
	if err := coordinator.recordCapabilityRuntimeOutcomeEvents(context.Background(), principalSource, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, worker.generation, session.CapabilityBindings, runtimeResult.Messages); err != nil {
		return DurableRuntimeExecutionResult{Transition: completed, Messages: runtimeResult.Messages}, err
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

func (coordinator *DurableRuntimeExecutionCoordinator) startRuntimeClaimHeartbeat(ctx, healthCtx context.Context, stop context.CancelFunc, principalSource VerifiedPrincipalSource, claim RuntimeExecutionClaim, health func(context.Context) error) <-chan error {
	failures := make(chan error, 1)
	interval := coordinator.claimRenewInterval
	if interval <= 0 {
		interval = runtimeExecutionClaimRenewInterval
	}
	callTimeout := max(interval/2, time.Nanosecond)
	go func() {
		defer close(failures)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if health != nil && healthCtx.Err() == nil {
					healthContext, cancel := context.WithTimeout(healthCtx, callTimeout)
					err := health(healthContext)
					cancel()
					if err != nil && healthCtx.Err() == nil {
						select {
						case <-ctx.Done():
							return
						default:
						}
						failures <- fmt.Errorf("%w: Worker health check failed: %v", ErrRuntimeEnvironmentUnavailable, err)
						stop()
						return
					}
				}
				principal, err := nextVerifiedPrincipal(principalSource)
				if err == nil {
					renewCtx, cancel := context.WithTimeout(ctx, callTimeout)
					_, err = coordinator.store.RenewManagedAgentExecutionClaim(renewCtx, claim.Scope.TenantID, principal, claim, runtimeExecutionClaimLeaseSeconds)
					cancel()
				}
				if err != nil {
					select {
					case <-ctx.Done():
						return
					default:
					}
					failures <- err
					stop()
					return
				}
			}
		}
	}()
	return failures
}

func stopRuntimeClaimHeartbeat(stopHeartbeat, stopRuntime context.CancelFunc, failures <-chan error) error {
	stopHeartbeat()
	claimErr, ok := <-failures
	stopRuntime()
	if !ok {
		return nil
	}
	return claimErr
}

func knownRuntimeClaimFailure(failures <-chan error) error {
	select {
	case claimErr, ok := <-failures:
		if ok {
			return claimErr
		}
	default:
	}
	return nil
}

func (coordinator *DurableRuntimeExecutionCoordinator) releaseQueuedRuntimeClaim(principalSource VerifiedPrincipalSource, claim RuntimeExecutionClaim) {
	principal, err := nextVerifiedPrincipal(principalSource)
	if err == nil {
		_ = coordinator.store.ReleaseQueuedManagedAgentExecutionClaim(context.Background(), claim.Scope.TenantID, principal, claim)
	}
}

func hydrateActiveRuntimeInteractions(active *activeDurableExecution, messages []runtimeprotocol.Message, resolutions []RuntimeInteractionResolution) {
	if active == nil {
		return
	}
	byRequest := make(map[string]RuntimeInteractionResolution, len(resolutions))
	for _, resolution := range resolutions {
		byRequest[resolution.InteractionRequestID] = resolution
	}
	for _, message := range messages {
		if message.MessageType != "InteractionRequest" {
			continue
		}
		requestID, interactionType, err := runtimeInteractionIdentity(message)
		if err != nil {
			continue
		}
		interaction := &activeRuntimeInteraction{interactionType: interactionType}
		if resolution, ok := byRequest[requestID]; ok {
			commandType := "ResolveApproval"
			if interactionType == "user-input" {
				commandType = "ResolveUserInput"
			}
			encoded, _ := json.Marshal(struct {
				CommandType string         `json:"commandType"`
				Payload     map[string]any `json:"payload"`
			}{CommandType: commandType, Payload: resolution.Payload})
			interaction.resolvedPayload = string(encoded)
		}
		active.interactions[requestID] = interaction
	}
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
	persistenceCtx, cancel := coordinator.persistenceContext(context.Background())
	cancelled, err := coordinator.store.CancelManagedAgentExecution(persistenceCtx, input.Scope.TenantID, principal, CancelTurnInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, TargetExecutionID: input.ExecutionID, Generation: started.Execution.Generation, Mutation: input.Mutation})
	cancel()
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(context.Canceled, err)
	}
	return DurableRuntimeExecutionResult{Transition: cancelled, Messages: messages}, context.Canceled
}

func (coordinator *DurableRuntimeExecutionCoordinator) fail(principalSource VerifiedPrincipalSource, input DurableRuntimeExecutionInput, started ExecutionTransitionResult, messages []runtimeprotocol.Message, code string, cause error, claim RuntimeExecutionClaim) (DurableRuntimeExecutionResult, error) {
	principal, err := nextVerifiedPrincipal(principalSource)
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	settlementCtx, cancelSettlement := coordinator.persistenceContext(context.Background())
	failed, err := coordinator.store.FailManagedAgentExecution(settlementCtx, input.Scope.TenantID, principal, FailRuntimeExecutionInput{FailExecutionInput: FailExecutionInput{Scope: input.Scope, SessionID: input.SessionID, TurnID: input.TurnID, ExecutionID: input.ExecutionID, Generation: started.Execution.Generation, ErrorCode: code, Mutation: input.Mutation}, Messages: messages, Claim: claim})
	cancelSettlement()
	if err != nil {
		return DurableRuntimeExecutionResult{Transition: started, Messages: messages}, errors.Join(cause, err)
	}
	return DurableRuntimeExecutionResult{Transition: failed, Messages: messages}, fmt.Errorf("%w: %v", ErrDurableRuntimeExecutionFailed, cause)
}

func (coordinator *DurableRuntimeExecutionCoordinator) persistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := coordinator.persistenceTimeout
	if timeout <= 0 {
		timeout = time.Duration(runtimeExecutionClaimLeaseSeconds) * time.Second
	}
	return context.WithTimeout(parent, timeout)
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

func (coordinator *DurableRuntimeExecutionCoordinator) executeRuntimeTurn(ctx context.Context, principalSource VerifiedPrincipalSource, claim *RuntimeExecutionClaim, priorMessages []runtimeprotocol.Message, key durableExecutionKey, active *activeDurableExecution, session runtimeSession, input RuntimeTurnInput) (RuntimeTurnResult, error) {
	result := RuntimeTurnResult{FailureCode: "runtime_open_failed"}
	if session == nil {
		return result, ErrDurableRuntimeExecutionUnavailable
	}
	// A recovered attempt shares the public transcript budget with the prior
	// attempt. Compact old assistant deltas before resuming so a provider can
	// still emit a terminal Result within that bounded transcript.
	priorMessages = compactRuntimeMessages(priorMessages)
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
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			closeSession()
		case <-closed:
		}
	}()
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
	if input.ResumeSnapshot != nil {
		runnerInput["workload"].(map[string]any)["resumeSnapshot"] = input.ResumeSnapshot
	}
	sessionCommand, sessionSuffix := "StartSession", "start"
	if input.ProviderResumeCursor != "" || input.ResumeSnapshot != nil {
		sessionCommand, sessionSuffix = "ResumeSession", "resume"
		if input.ProviderResumeCursor != "" {
			runnerInput["providerResumeCursor"] = input.ProviderResumeCursor
		}
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
	remainingMessages := maxRuntimeExecutionMessages - len(priorMessages)
	messages, terminal, err := receiveRuntimeMessagesWithLimit(session.Receive, turn.CommandID, func(message runtimeprotocol.Message) (bool, error) {
		return coordinator.routeRuntimeMessage(key, active, turn.CommandID, message)
	}, func(messages []runtimeprotocol.Message, message runtimeprotocol.Message) error {
		checkpointMessages := coordinator.recordActiveRuntimeMessage(key, active, message)
		if message.MessageType == "Result" || message.MessageType == "Error" {
			return nil
		}
		reconciledThrough := 0
		if input.SideEffectReconciliation != nil {
			reconciledThrough = len(priorMessages)
		}
		pendingSideEffect, pendingInteractions := runtimeCheckpointState(checkpointMessages, input.ResolvedInteractions, reconciledThrough)
		if pendingSideEffect {
			claim.PendingSideEffect = true
		}
		principal, err := nextVerifiedPrincipal(principalSource)
		if err != nil {
			return err
		}
		checkpointCtx, cancelCheckpoint := coordinator.persistenceContext(ctx)
		checkpoint, err := coordinator.store.CheckpointManagedAgentExecution(checkpointCtx, input.Scope.TenantID, principal, CheckpointRuntimeExecutionInput{
			Claim: *claim, Messages: checkpointMessages, Protocol: runtimeExecutionCheckpointProtocol,
			PendingSideEffect: pendingSideEffect, PendingInteractions: pendingInteractions,
		})
		cancelCheckpoint()
		if err == nil {
			claim.ExpiresAt = checkpoint.ExpiresAt
			claim.CheckpointSequence = checkpoint.Sequence
			claim.CheckpointDigest = checkpoint.Digest
			claim.CheckpointedAt = checkpoint.CreatedAt
			claim.CheckpointProtocol = runtimeExecutionCheckpointProtocol
			claim.PendingSideEffect = pendingSideEffect
			claim.PendingInteractionCount = pendingInteractions
		}
		return err
	}, remainingMessages)
	result.Messages = publicRuntimeMessages(compactRuntimeMessages(append(append([]runtimeprotocol.Message(nil), priorMessages...), messages...)))
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

func runtimeCheckpointState(messages []runtimeprotocol.Message, resolutions []RuntimeInteractionResolution, reconciledThrough int) (bool, uint32) {
	resolved := make(map[string]struct{}, len(resolutions))
	for _, resolution := range resolutions {
		resolved[resolution.InteractionRequestID] = struct{}{}
	}
	type pendingToolKey struct{ itemType, itemID string }
	pendingTools := make(map[pendingToolKey]struct{})
	pendingInteractions := make(map[string]struct{})
	for index, message := range messages {
		if message.MessageType == "InteractionRequest" {
			requestID, _, err := runtimeInteractionIdentity(message)
			if err == nil {
				if _, ok := resolved[requestID]; !ok {
					pendingInteractions[requestID] = struct{}{}
				}
			}
		}
		if message.MessageType != "Event" || message.Payload["eventType"] == nil {
			continue
		}
		if index < reconciledThrough {
			continue
		}
		eventType, _ := message.Payload["eventType"].(string)
		payload, _ := message.Payload["payload"].(map[string]any)
		if !runtimeEventMayHaveSideEffect(payload) {
			continue
		}
		data, _ := payload["data"].(map[string]any)
		itemID, _ := data["providerItemId"].(string)
		if itemID == "" {
			itemID = fmt.Sprintf("unidentified-%d", index)
		}
		itemType, _ := payload["itemType"].(string)
		key := pendingToolKey{itemType: itemType, itemID: itemID}
		switch eventType {
		case "item.started", "item.updated":
			pendingTools[key] = struct{}{}
		case "item.completed":
			delete(pendingTools, key)
		}
	}
	return len(pendingTools) > 0, uint32(len(pendingInteractions))
}

func runtimeEventMayHaveSideEffect(payload map[string]any) bool {
	itemType, _ := payload["itemType"].(string)
	switch itemType {
	case "user_message", "assistant_message", "reasoning", "plan", "web_search", "image_view", "review_entered", "review_exited", "context_compaction", "error":
		return false
	default:
		return true
	}
}

func runtimeResumeSnapshot(input RuntimeTurnInput, claim RuntimeExecutionClaim, messages []runtimeprotocol.Message, resolutions []RuntimeInteractionResolution) map[string]any {
	if claim.AttemptNumber <= 1 || len(messages) == 0 || claim.CheckpointSequence == 0 {
		return nil
	}
	var assistantText strings.Builder
	toolResults := make([]any, 0)
	pendingInteractions := make([]any, 0)
	resolved := make(map[string]struct{}, len(resolutions))
	interactionRequests := make(map[string]map[string]any)
	for _, message := range messages {
		if message.MessageType != "InteractionRequest" {
			continue
		}
		requestID, _, err := runtimeInteractionIdentity(message)
		if err == nil {
			interactionRequests[requestID] = message.Payload
		}
	}
	resumeRecordedInteractions := make([]any, 0, len(resolutions))
	for _, resolution := range resolutions {
		resolved[resolution.InteractionRequestID] = struct{}{}
		entry := map[string]any{"kind": resolution.InteractionType, "requestId": resolution.InteractionRequestID, "resolution": resolution.Payload["resolution"]}
		if request := interactionRequests[resolution.InteractionRequestID]; request != nil {
			entry["request"] = request
		}
		if resolution.InteractionType == "approval" {
			resolutionPayload, _ := resolution.Payload["resolution"].(map[string]any)
			if resolutionPayload["decision"] == "accept" {
				entry["resolutionKind"] = "approved"
			} else {
				entry["resolutionKind"] = "declined"
			}
		} else {
			entry["resolutionKind"] = "answered"
		}
		resumeRecordedInteractions = append(resumeRecordedInteractions, entry)
	}
	for _, message := range messages {
		switch message.MessageType {
		case "Event":
			eventType, _ := message.Payload["eventType"].(string)
			payload, _ := message.Payload["payload"].(map[string]any)
			if eventType == "content.delta" && payload["streamKind"] == "assistant_text" {
				if delta, ok := payload["delta"].(string); ok {
					assistantText.WriteString(delta)
				}
			}
			if strings.HasPrefix(eventType, "item.") {
				toolResults = append(toolResults, message.Payload)
			}
		case "InteractionRequest":
			requestID, _, err := runtimeInteractionIdentity(message)
			if err == nil {
				if _, ok := resolved[requestID]; !ok {
					pendingInteractions = append(pendingInteractions, message.Payload)
				}
			}
		}
	}
	digest := strings.TrimPrefix(claim.CheckpointDigest, "sha256:")
	snapshot := map[string]any{
		"version": 1, "sessionId": input.SessionID, "turnId": input.TurnID, "provider": input.ProviderKind,
		"toolResults": toolResults, "pendingInteractions": pendingInteractions,
		"resumeRecordedInteractions": resumeRecordedInteractions,
		"activeTurnCheckpoint": map[string]any{
			"suspendAttemptId": fmt.Sprintf("attempt-%d", claim.AttemptNumber-1),
			"sourceGeneration": input.Generation, "boundaryMeaningfulActivitySequence": claim.CheckpointSequence,
			"activeCommandId":           boundedRuntimeIdentifier(input.RequestID, "turn"),
			"checkpointHistorySequence": claim.CheckpointSequence, "currentTurnSequence": 1,
			"checkpointProtocol": claim.CheckpointProtocol, "receiptSha256": digest,
		},
		"sourceSequenceRange": map[string]any{"from": 1, "through": claim.CheckpointSequence},
		"currentTurnSequence": 1, "authoritativeHistorySequence": claim.CheckpointSequence,
	}
	if assistantText.Len() > 0 {
		snapshot["messages"] = []any{map[string]any{"role": "assistant", "text": assistantText.String(), "sequenceFrom": 1, "sequenceThrough": claim.CheckpointSequence}}
	}
	if input.SideEffectReconciliation != nil {
		snapshot["sideEffectReconciliation"] = map[string]any{
			"checkpointDigest": input.SideEffectReconciliation.CheckpointDigest,
			"outcome":          input.SideEffectReconciliation.Outcome,
			"reconciledAt":     input.SideEffectReconciliation.CreatedAt.UTC().Format(time.RFC3339Nano),
		}
	}
	return snapshot
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

func receiveRuntimeMessages(receive func() (runtimeprotocol.Message, error), commandID string, route func(runtimeprotocol.Message) (bool, error), accepted func([]runtimeprotocol.Message, runtimeprotocol.Message) error) ([]runtimeprotocol.Message, runtimeprotocol.Message, error) {
	return receiveRuntimeMessagesWithLimit(receive, commandID, route, accepted, maxRuntimeExecutionMessages)
}

func receiveRuntimeMessagesWithLimit(receive func() (runtimeprotocol.Message, error), commandID string, route func(runtimeprotocol.Message) (bool, error), accepted func([]runtimeprotocol.Message, runtimeprotocol.Message) error, limit int) ([]runtimeprotocol.Message, runtimeprotocol.Message, error) {
	if limit < 1 {
		return nil, runtimeprotocol.Message{}, errors.New("Runtime response transcript exceeds the public limit")
	}
	messages := make([]runtimeprotocol.Message, 0, min(limit, maxRuntimeExecutionMessages))
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
		candidate := append(append([]runtimeprotocol.Message(nil), messages...), message)
		candidate = compactRuntimeMessages(candidate)
		encoded, err := json.Marshal(candidate)
		if err != nil || len(candidate) > limit || len(encoded) > runtimeprotocol.MaxMessageBytes {
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
		messages = candidate
		if accepted != nil {
			if err := accepted(messages, message); err != nil {
				return messages, message, err
			}
		}
		if message.MessageType == "Result" || message.MessageType == "Error" {
			return messages, message, nil
		}
	}
}

func compactRuntimeMessages(messages []runtimeprotocol.Message) []runtimeprotocol.Message {
	if len(messages) < 2 {
		return append([]runtimeprotocol.Message(nil), messages...)
	}
	compacted := make([]runtimeprotocol.Message, 0, len(messages))
	for _, message := range messages {
		if len(compacted) > 0 && mergeAssistantTextDelta(&compacted[len(compacted)-1], message) {
			continue
		}
		compacted = append(compacted, message)
	}
	return compacted
}

func mergeAssistantTextDelta(previous *runtimeprotocol.Message, current runtimeprotocol.Message) bool {
	if previous == nil || previous.MessageType != "Event" || current.MessageType != "Event" ||
		previous.RequestID != current.RequestID || previous.CommandID != current.CommandID ||
		previous.ExecutionID != current.ExecutionID || previous.Generation != current.Generation {
		return false
	}
	previousEventType, previousPayload, previousOK := assistantTextDeltaPayload(previous.Payload)
	currentEventType, currentPayload, currentOK := assistantTextDeltaPayload(current.Payload)
	if !previousOK || !currentOK || previousEventType != currentEventType {
		return false
	}
	mergedPayload := maps.Clone(previous.Payload)
	mergedEventPayload := maps.Clone(previousPayload)
	mergedEventPayload["delta"] = previousPayload["delta"].(string) + currentPayload["delta"].(string)
	mergedPayload["payload"] = mergedEventPayload
	previous.Payload = mergedPayload
	return true
}

func assistantTextDeltaPayload(payload map[string]any) (string, map[string]any, bool) {
	eventType, eventOK := payload["eventType"].(string)
	eventPayload, payloadOK := payload["payload"].(map[string]any)
	streamKind, streamOK := eventPayload["streamKind"].(string)
	_, deltaOK := eventPayload["delta"].(string)
	if !eventOK || !payloadOK || !streamOK || !deltaOK || eventType != "content.delta" || streamKind != "assistant_text" {
		return "", nil, false
	}
	return eventType, eventPayload, true
}

func (coordinator *DurableRuntimeExecutionCoordinator) recordActiveRuntimeMessage(key durableExecutionKey, active *activeDurableExecution, message runtimeprotocol.Message) []runtimeprotocol.Message {
	coordinator.activeMu.Lock()
	defer coordinator.activeMu.Unlock()
	if coordinator.active[key] != active {
		return nil
	}
	if len(active.messages) == 0 || !mergeAssistantTextDelta(&active.messages[len(active.messages)-1], message) {
		active.messages = append(active.messages, publicRuntimeMessage(message))
	}
	return append([]runtimeprotocol.Message(nil), active.messages...)
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
