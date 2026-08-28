// Package supervisor contains the Supervisor-side admission client for the
// generated Worker contract, including the production mTLS constructor.
//
// It owns the v1.0 negotiate/health bindings and a separately gated localdev
// dispatch profile. The default Bind method is the original health-only
// profile; BindOperationAdmission is an explicit local admission profile and
// still does not dispatch work. Only NewLocal + BindLocalDispatch can invoke
// the opaque in-process Worker handle. NewMTLS supplies the production HTTPS
// transport; no method starts a listener, persists a lease, invokes a
// provider, or writes a durable receipt.
package supervisor

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var errInvalidConfig = errors.New("worker_supervisor/invalid_config")

const (
	// NegotiationHealthProfileID is the original health-only supervisor
	// binding and remains the default for compatibility.
	NegotiationHealthProfileID = "cloud-agents/worker-supervisor-negotiation-health/v1"
	// OperationAdmissionProfileID names the explicit local Worker admission
	// binding. It does not imply operation execution or receipt support.
	OperationAdmissionProfileID = workerkernel.OperationAdmissionProfileID
)

// Clock is injectable for deterministic expiry checks.
type Clock func() time.Time

// Config intentionally has no endpoint URL, protocol selector, or mutable
// capability list. Bind and BindOperationAdmission select fixed, versioned
// profiles in code; callers cannot supply arbitrary capabilities.
type Config struct {
	Client                 workerv1alpha1connect.WorkerExecutionServiceClient
	ExpectedWorkerIdentity *workerv1alpha1.WorkloadIdentity
	Clock                  Clock
}

// Supervisor performs the authenticated Worker admission handshake. A
// successful binding is held in memory and replaced only after a later
// negotiation has passed every validation.
type Supervisor struct {
	client         workerv1alpha1connect.WorkerExecutionServiceClient
	workerIdentity *workerv1alpha1.WorkloadIdentity
	now            Clock

	// localHandle and localDispatchMarker are populated only by NewLocal. A
	// generated Connect client (even one that happens to expose the same RPCs)
	// can never opt into the dispatch path through New.
	localHandle         workerkernel.LocalDispatchHandle
	localDispatchMarker *localDispatchMarker

	bindMu  sync.Mutex
	mu      sync.RWMutex
	binding *bindingState
}

type bindingState struct {
	profileID     string
	negotiationID string
	expiresAt     time.Time
	accepted      []workerv1alpha1.Capability
	identity      *workerv1alpha1.WorkloadIdentity
	descriptor    *workerv1alpha1.ProtocolDescriptor
}

// BindingSnapshot is a detached copy of the negotiated binding.
type BindingSnapshot struct {
	ProfileID            string
	NegotiationID        string
	ExpiresAt            time.Time
	AcceptedCapabilities []workerv1alpha1.Capability
	ServerIdentity       *workerv1alpha1.WorkloadIdentity
	Protocol             *workerv1alpha1.ProtocolDescriptor
}

// Negotiation returns the exact generated binding tuple for a subsequent
// Worker request. It always returns a fresh protobuf value.
func (snapshot BindingSnapshot) Negotiation() *workerv1alpha1.NegotiationBinding {
	return &workerv1alpha1.NegotiationBinding{
		ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor},
		NegotiationId:   snapshot.NegotiationID,
		ExpiresAt:       timestamppb.New(snapshot.ExpiresAt.UTC()),
	}
}

// HealthSnapshot is the validated result of a Worker health check.
type HealthSnapshot struct {
	State      workerv1alpha1.HealthState
	ObservedAt time.Time
	Protocol   *workerv1alpha1.ProtocolDescriptor
}

// New constructs the admission client without performing I/O.
func New(config Config) (*Supervisor, error) {
	if !clientAvailable(config.Client) || !validIdentity(config.ExpectedWorkerIdentity) {
		return nil, errInvalidConfig
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Supervisor{
		client:         config.Client,
		workerIdentity: cloneIdentity(config.ExpectedWorkerIdentity),
		now:            config.Clock,
	}, nil
}

// Bind negotiates exactly protocol v1.0 and the generated P1-A negotiation /
// health capability pair. No binding state is committed until the response
// is fully validated.
func (s *Supervisor) Bind(ctx context.Context) (BindingSnapshot, error) {
	return s.bindWithProfile(ctx, requiredCapabilities(), NegotiationHealthProfileID)
}

// BindOperationAdmission negotiates the explicit local operation-admission
// profile. It is separate from Bind so a Worker advertising the operation
// capability never silently changes the original health-only contract. The
// loopback LocalLauncher uses this binding for its process-boundary calls.
func (s *Supervisor) BindOperationAdmission(ctx context.Context) (BindingSnapshot, error) {
	return s.bindWithProfile(ctx, operationAdmissionCapabilities(), OperationAdmissionProfileID)
}

func (s *Supervisor) bindWithProfile(ctx context.Context, capabilities []workerv1alpha1.Capability, profileID string) (BindingSnapshot, error) {
	if s == nil || !clientAvailable(s.client) || !validIdentity(s.workerIdentity) || s.now == nil {
		return BindingSnapshot{}, errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	// Serialize negotiations so an older, slower response cannot replace a
	// newer binding that was started concurrently.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := contextErr(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	response, err := s.client.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor}},
		RequiredCapabilities:   append([]workerv1alpha1.Capability(nil), capabilities...),
		ExpectedServerIdentity: cloneIdentity(s.workerIdentity),
	}))
	if err != nil {
		return BindingSnapshot{}, rpcFailure("negotiate", err)
	}
	if err := contextErr(ctx); err != nil {
		return BindingSnapshot{}, err
	}
	now, ok := s.nowUTC()
	if !ok {
		return BindingSnapshot{}, errInvalidConfig
	}
	state, err := validateNegotiationResponse(response, s.workerIdentity, now, capabilities)
	if err != nil {
		return BindingSnapshot{}, err
	}
	state.profileID = profileID
	s.mu.Lock()
	s.binding = state
	s.mu.Unlock()
	return state.snapshot(), nil
}

// CurrentBinding returns a detached copy of the last successful binding.
func (s *Supervisor) CurrentBinding() (BindingSnapshot, bool) {
	if s == nil {
		return BindingSnapshot{}, false
	}
	s.mu.RLock()
	state := cloneBindingState(s.binding)
	s.mu.RUnlock()
	if state == nil {
		return BindingSnapshot{}, false
	}
	now, ok := s.nowUTC()
	if !ok {
		return BindingSnapshot{}, false
	}
	if now.Before(state.expiresAt) {
		return state.snapshot(), true
	}
	// Do not leave an expired binding observable as current. The identity and
	// expiry comparison prevents a concurrent rebind from being cleared.
	s.clearBindingIf(state)
	return BindingSnapshot{}, false
}

// CheckHealth validates expiry locally before making the RPC and rejects any
// protocol descriptor drift from the negotiated descriptor.
func (s *Supervisor) CheckHealth(ctx context.Context) (HealthSnapshot, error) {
	if s == nil || !clientAvailable(s.client) || !validIdentity(s.workerIdentity) || s.now == nil {
		return HealthSnapshot{}, errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return HealthSnapshot{}, err
	}
	s.mu.RLock()
	state := cloneBindingState(s.binding)
	s.mu.RUnlock()
	if state == nil {
		return HealthSnapshot{}, fail(connect.CodeFailedPrecondition, "binding_required")
	}
	now, ok := s.nowUTC()
	if !ok {
		return HealthSnapshot{}, errInvalidConfig
	}
	if !now.Before(state.expiresAt) {
		s.clearBindingIf(state)
		return HealthSnapshot{}, fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	required := append([]workerv1alpha1.Capability(nil), state.accepted...)
	response, err := s.client.CheckHealth(ctx, connect.NewRequest(&workerv1alpha1.HealthRequest{
		Negotiation:            state.negotiationBinding(),
		RequiredCapabilities:   required,
		ExpectedServerIdentity: cloneIdentity(s.workerIdentity),
	}))
	if err != nil {
		return HealthSnapshot{}, rpcFailure("health", err)
	}
	if err := contextErr(ctx); err != nil {
		return HealthSnapshot{}, err
	}
	now, ok = s.nowUTC()
	if !ok {
		return HealthSnapshot{}, errInvalidConfig
	}
	if !now.Before(state.expiresAt) {
		s.clearBindingIf(state)
		return HealthSnapshot{}, fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	return validateHealthResponse(response, state)
}

func requiredCapabilities() []workerv1alpha1.Capability {
	return []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
	}
}

func operationAdmissionCapabilities() []workerv1alpha1.Capability {
	return []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
		workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
	}
}

func validateNegotiationResponse(response *connect.Response[workerv1alpha1.NegotiationResponse], expected *workerv1alpha1.WorkloadIdentity, now time.Time, required []workerv1alpha1.Capability) (*bindingState, error) {
	if response == nil || response.Msg == nil {
		return nil, fail(connect.CodeInternal, "negotiation_response_missing")
	}
	message := response.Msg
	if !isProtocolVersion(message.GetSelectedVersion()) {
		return nil, fail(connect.CodeFailedPrecondition, "negotiated_version_invalid")
	}
	if !exactCapabilities(message.GetAcceptedCapabilities(), required) {
		return nil, fail(connect.CodeFailedPrecondition, "negotiated_capabilities_invalid")
	}
	descriptor, err := validateDescriptor(message.GetServer())
	if err != nil {
		return nil, err
	}
	if !exactCapabilities(descriptor.GetCapabilities(), required) {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_capabilities_invalid")
	}
	if !sameIdentity(message.GetAuthenticatedServerIdentity(), expected) {
		return nil, fail(connect.CodePermissionDenied, "server_identity_mismatch")
	}
	if !validNegotiationID(message.GetNegotiationId()) {
		return nil, fail(connect.CodeFailedPrecondition, "negotiation_id_invalid")
	}
	if message.GetExpiresAt() == nil || message.GetExpiresAt().CheckValid() != nil {
		return nil, fail(connect.CodeFailedPrecondition, "negotiation_expiry_invalid")
	}
	expiresAt := message.GetExpiresAt().AsTime().UTC()
	if !now.Before(expiresAt) {
		return nil, fail(connect.CodeDeadlineExceeded, "negotiation_already_expired")
	}
	for _, capability := range message.GetAcceptedCapabilities() {
		if !containsCapability(descriptor.GetCapabilities(), capability) {
			return nil, fail(connect.CodeFailedPrecondition, "negotiated_capability_not_advertised")
		}
	}
	return &bindingState{
		profileID:     "",
		negotiationID: message.GetNegotiationId(),
		expiresAt:     expiresAt,
		accepted:      append([]workerv1alpha1.Capability(nil), required...),
		identity:      cloneIdentity(message.GetAuthenticatedServerIdentity()),
		descriptor:    cloneDescriptor(descriptor),
	}, nil
}

func validateHealthResponse(response *connect.Response[workerv1alpha1.HealthResponse], state *bindingState) (HealthSnapshot, error) {
	if response == nil || response.Msg == nil {
		return HealthSnapshot{}, fail(connect.CodeInternal, "health_response_missing")
	}
	message := response.Msg
	if message.GetState() != workerv1alpha1.HealthState_HEALTH_STATE_SERVING {
		return HealthSnapshot{}, fail(connect.CodeFailedPrecondition, "worker_not_serving")
	}
	descriptor, err := validateDescriptor(message.GetProtocol())
	if err != nil {
		return HealthSnapshot{}, err
	}
	if !proto.Equal(descriptor, state.descriptor) {
		return HealthSnapshot{}, fail(connect.CodeFailedPrecondition, "protocol_descriptor_drift")
	}
	if message.GetObservedAt() == nil || message.GetObservedAt().CheckValid() != nil {
		return HealthSnapshot{}, fail(connect.CodeFailedPrecondition, "health_timestamp_invalid")
	}
	return HealthSnapshot{
		State:      message.GetState(),
		ObservedAt: message.GetObservedAt().AsTime().UTC(),
		Protocol:   cloneDescriptor(descriptor),
	}, nil
}

func validateDescriptor(descriptor *workerv1alpha1.ProtocolDescriptor) (*workerv1alpha1.ProtocolDescriptor, error) {
	if descriptor == nil || !isProtocolVersion(descriptor.GetCurrentVersion()) || !isProtocolVersion(descriptor.GetMinimumCompatibleVersion()) {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_invalid")
	}
	if descriptor.GetMaxPayloadBytes() == 0 || descriptor.GetMaxPayloadBytes() > workerkernel.MaxPayloadBytes ||
		descriptor.GetMaxDeadlineSeconds() == 0 || descriptor.GetMaxDeadlineSeconds() > workerkernel.MaxDeadlineSeconds ||
		descriptor.GetMaxWireMessageBytes() == 0 || descriptor.GetMaxWireMessageBytes() > workerkernel.MaxWireMessageBytes ||
		descriptor.GetMaxRepeatedItems() == 0 || descriptor.GetMaxRepeatedItems() > workerkernel.MaxRepeatedItems ||
		descriptor.GetMaxStringBytes() == 0 || descriptor.GetMaxStringBytes() > workerkernel.MaxStringBytes ||
		descriptor.GetMaxPayloadBytes() > descriptor.GetMaxWireMessageBytes() {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_bounds_invalid")
	}
	if len(descriptor.GetCapabilities()) > int(descriptor.GetMaxRepeatedItems()) ||
		len(descriptor.GetCapabilities()) > int(workerkernel.MaxRepeatedItems) || !uniqueKnownCapabilities(descriptor.GetCapabilities()) {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_capabilities_invalid")
	}
	canonical := cloneDescriptor(descriptor)
	sort.Slice(canonical.Capabilities, func(i, j int) bool { return canonical.Capabilities[i] < canonical.Capabilities[j] })
	return canonical, nil
}

func exactCapabilities(got, want []workerv1alpha1.Capability) bool {
	if len(got) != len(want) {
		return false
	}
	if !uniqueKnownCapabilities(got) || !uniqueKnownCapabilities(want) {
		return false
	}
	for _, capability := range want {
		if !containsCapability(got, capability) {
			return false
		}
	}
	return true
}

func uniqueKnownCapabilities(capabilities []workerv1alpha1.Capability) bool {
	seen := make(map[workerv1alpha1.Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		if !knownCapability(capability) {
			return false
		}
		if _, ok := seen[capability]; ok {
			return false
		}
		seen[capability] = struct{}{}
	}
	return true
}

func knownCapability(capability workerv1alpha1.Capability) bool {
	switch capability {
	case workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
		workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		workerv1alpha1.Capability_CAPABILITY_DURABLE_RECEIPTS,
		workerv1alpha1.Capability_CAPABILITY_FINALIZERS,
		workerv1alpha1.Capability_CAPABILITY_ADAPTER_REGISTRATION:
		return true
	default:
		return false
	}
}

func containsCapability(capabilities []workerv1alpha1.Capability, want workerv1alpha1.Capability) bool {
	for _, capability := range capabilities {
		if capability == want {
			return true
		}
	}
	return false
}

func isProtocolVersion(value *workerv1alpha1.ProtocolVersion) bool {
	return value != nil && value.GetMajor() == workerkernel.ProtocolMajor && value.GetMinor() == workerkernel.ProtocolMinor
}

func clientAvailable(client workerv1alpha1connect.WorkerExecutionServiceClient) bool {
	if client == nil {
		return false
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func (s *Supervisor) nowUTC() (time.Time, bool) {
	if s == nil || s.now == nil {
		return time.Time{}, false
	}
	return s.now().UTC(), true
}

func (s *Supervisor) clearBindingIf(state *bindingState) {
	if s == nil || state == nil {
		return
	}
	s.mu.Lock()
	if s.binding != nil && s.binding.negotiationID == state.negotiationID && s.binding.expiresAt.Equal(state.expiresAt) {
		s.binding = nil
	}
	s.mu.Unlock()
}

func validIdentity(identity *workerv1alpha1.WorkloadIdentity) bool {
	if identity == nil || identity.GetSpiffeId() == "" || identity.GetTrustDomain() == "" ||
		!utf8.ValidString(identity.GetSpiffeId()) || !utf8.ValidString(identity.GetTrustDomain()) ||
		len(identity.GetSpiffeId()) > int(workerkernel.MaxStringBytes) || len(identity.GetTrustDomain()) > int(workerkernel.MaxStringBytes) {
		return false
	}
	n := len(identity.GetLeafCertificateSha256())
	return n == 0 || n == 32
}

func sameIdentity(left, right *workerv1alpha1.WorkloadIdentity) bool {
	if !validIdentity(left) || !validIdentity(right) || left.GetSpiffeId() != right.GetSpiffeId() || left.GetTrustDomain() != right.GetTrustDomain() {
		return false
	}
	return subtle.ConstantTimeCompare(left.GetLeafCertificateSha256(), right.GetLeafCertificateSha256()) == 1
}

func validNegotiationID(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > int(workerkernel.MaxIdentifierBytes) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func (state *bindingState) snapshot() BindingSnapshot {
	if state == nil {
		return BindingSnapshot{}
	}
	return BindingSnapshot{
		ProfileID:            state.profileID,
		NegotiationID:        state.negotiationID,
		ExpiresAt:            state.expiresAt,
		AcceptedCapabilities: append([]workerv1alpha1.Capability(nil), state.accepted...),
		ServerIdentity:       cloneIdentity(state.identity),
		Protocol:             cloneDescriptor(state.descriptor),
	}
}

func (state *bindingState) negotiationBinding() *workerv1alpha1.NegotiationBinding {
	return &workerv1alpha1.NegotiationBinding{
		ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor},
		NegotiationId:   state.negotiationID,
		ExpiresAt:       timestamppb.New(state.expiresAt.UTC()),
	}
}

func cloneBindingState(state *bindingState) *bindingState {
	if state == nil {
		return nil
	}
	return &bindingState{
		profileID:     state.profileID,
		negotiationID: state.negotiationID,
		expiresAt:     state.expiresAt,
		accepted:      append([]workerv1alpha1.Capability(nil), state.accepted...),
		identity:      cloneIdentity(state.identity),
		descriptor:    cloneDescriptor(state.descriptor),
	}
}

func cloneIdentity(identity *workerv1alpha1.WorkloadIdentity) *workerv1alpha1.WorkloadIdentity {
	if identity == nil {
		return nil
	}
	return proto.Clone(identity).(*workerv1alpha1.WorkloadIdentity)
}

func cloneDescriptor(descriptor *workerv1alpha1.ProtocolDescriptor) *workerv1alpha1.ProtocolDescriptor {
	if descriptor == nil {
		return nil
	}
	return proto.Clone(descriptor).(*workerv1alpha1.ProtocolDescriptor)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fail(connect.CodeInvalidArgument, "invalid_context")
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail(connect.CodeDeadlineExceeded, "deadline_exceeded")
		}
		return fail(connect.CodeCanceled, "request_canceled")
	default:
		return nil
	}
}

func fail(code connect.Code, stable string) error {
	return connect.NewError(code, errors.New("worker_supervisor/"+stable))
}

func rpcFailure(operation string, err error) error {
	code := connect.CodeOf(err)
	if code == connect.CodeUnknown {
		switch {
		case errors.Is(err, context.Canceled):
			code = connect.CodeCanceled
		case errors.Is(err, context.DeadlineExceeded):
			code = connect.CodeDeadlineExceeded
		default:
			code = connect.CodeUnavailable
		}
	}
	return fail(code, fmt.Sprintf("%s_rpc_failed", operation))
}
