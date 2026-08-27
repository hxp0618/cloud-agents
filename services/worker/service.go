package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	ProtocolMajor       uint32 = 1
	ProtocolMinor       uint32 = 0
	MaxWireMessageBytes uint32 = 1 << 20
	MaxRepeatedItems    uint32 = 64
	MaxStringBytes      uint32 = 1024
	MaxPayloadBytes     uint32 = 64 << 10
	MaxDeadlineSeconds  uint32 = 300
	// Negotiation identifiers use the contract's stricter 256-byte identifier cap.
	MaxIdentifierBytes uint32 = 256
)

// IdentityProvider supplies the caller identity established by the transport.
// Request expected_* fields are never used as authentication. The default
// provider rejects every call, making an explicit test/integration provider
// mandatory until a real mTLS listener is introduced.
type IdentityProvider interface {
	ClientIdentity(context.Context) (*workerv1alpha1.WorkloadIdentity, error)
}

type rejectIdentityProvider struct{}

func (rejectIdentityProvider) ClientIdentity(context.Context) (*workerv1alpha1.WorkloadIdentity, error) {
	return nil, errors.New("worker/transport_identity_missing")
}

// StaticIdentityProvider is useful for in-process tests and a future
// transport adapter. It does not implement or imply mTLS.
type StaticIdentityProvider struct {
	Identity *workerv1alpha1.WorkloadIdentity
}

func (p StaticIdentityProvider) ClientIdentity(context.Context) (*workerv1alpha1.WorkloadIdentity, error) {
	if p.Identity == nil {
		return nil, errors.New("worker/transport_identity_missing")
	}
	return cloneIdentity(p.Identity), nil
}

// IDGenerator and Clock are injectable to make negotiation deterministic in
// tests while production defaults remain cryptographically random and UTC.
type IDGenerator func() (string, error)
type Clock func() time.Time

type Config struct {
	WorkerIdentity *workerv1alpha1.WorkloadIdentity
	Capabilities   []workerv1alpha1.Capability
	NegotiationTTL time.Duration
	// AdmissionLeaseID and AdmissionGeneration bind the local, in-memory
	// operation-admission seam to one externally supplied fencing authority.
	// They do not authorize dispatch, durable receipts, or any external write.
	AdmissionLeaseID    string
	AdmissionGeneration uint64
	IdentityProvider    IdentityProvider
	IDGenerator         IDGenerator
	Clock               Clock
}

// Service is an in-memory, transport-neutral WorkerExecutionService kernel.
// Negotiation state is ephemeral and bound to client/server identities.
type Service struct {
	workerv1alpha1connect.UnimplementedWorkerExecutionServiceHandler
	workerIdentity      *workerv1alpha1.WorkloadIdentity
	capabilities        map[workerv1alpha1.Capability]struct{}
	ttl                 time.Duration
	identity            IdentityProvider
	newID               IDGenerator
	now                 Clock
	mu                  sync.RWMutex
	bindings            map[string]binding
	admissionLeaseID    string
	admissionGeneration uint64
	admissions          map[string]admissionRecord
}

type binding struct {
	version version
	client  *workerv1alpha1.WorkloadIdentity
	server  *workerv1alpha1.WorkloadIdentity
	caps    map[workerv1alpha1.Capability]struct{}
	expires time.Time
}
type version struct{ major, minor uint32 }

func NewService(cfg Config) (*Service, error) {
	if err := validateIdentity(cfg.WorkerIdentity); err != nil {
		return nil, fmt.Errorf("worker/invalid_config: worker identity is required")
	}
	if cfg.NegotiationTTL <= 0 {
		cfg.NegotiationTTL = 5 * time.Minute
	}
	if cfg.IdentityProvider == nil {
		cfg.IdentityProvider = rejectIdentityProvider{}
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if cfg.IDGenerator == nil {
		cfg.IDGenerator = randomID
	}
	caps := cfg.Capabilities
	if caps == nil {
		caps = []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
		}
	}
	set := make(map[workerv1alpha1.Capability]struct{}, len(caps))
	for _, c := range caps {
		if !knownCapability(c) {
			return nil, fmt.Errorf("worker/invalid_config: unknown capability %d", c)
		}
		if c != workerv1alpha1.Capability_CAPABILITY_NEGOTIATION && c != workerv1alpha1.Capability_CAPABILITY_HEALTH && c != workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH {
			return nil, fmt.Errorf("worker/invalid_config: capability %s is not implemented", c.String())
		}
		if _, duplicate := set[c]; duplicate {
			return nil, fmt.Errorf("worker/invalid_config: duplicate capability %d", c)
		}
		set[c] = struct{}{}
	}
	return &Service{workerIdentity: cloneIdentity(cfg.WorkerIdentity), capabilities: set, ttl: cfg.NegotiationTTL,
		identity: cfg.IdentityProvider, newID: cfg.IDGenerator, now: cfg.Clock, bindings: make(map[string]binding),
		admissionLeaseID: cfg.AdmissionLeaseID, admissionGeneration: cfg.AdmissionGeneration,
		admissions: make(map[string]admissionRecord)}, nil
}

func (s *Service) ProtocolDescriptor() *workerv1alpha1.ProtocolDescriptor {
	caps := make([]workerv1alpha1.Capability, 0, len(s.capabilities))
	for c := range s.capabilities {
		caps = append(caps, c)
	}
	// Stable order is part of the in-process response contract.
	for i := 0; i < len(caps); i++ {
		for j := i + 1; j < len(caps); j++ {
			if caps[j] < caps[i] {
				caps[i], caps[j] = caps[j], caps[i]
			}
		}
	}
	return &workerv1alpha1.ProtocolDescriptor{CurrentVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, MinimumCompatibleVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, Capabilities: caps, MaxPayloadBytes: MaxPayloadBytes, MaxDeadlineSeconds: MaxDeadlineSeconds, MaxWireMessageBytes: MaxWireMessageBytes, MaxRepeatedItems: MaxRepeatedItems, MaxStringBytes: MaxStringBytes}
}

func (s *Service) Negotiate(ctx context.Context, req *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.Msg == nil {
		return nil, fail(connect.CodeInvalidArgument, "invalid_request", "request is required")
	}
	m := req.Msg
	if len(m.SupportedVersions) == 0 || len(m.SupportedVersions) > 8 {
		return nil, fail(connect.CodeInvalidArgument, "unsupported_protocol_version", "supported_versions must contain exactly one supported v1.0")
	}
	seenVersions := make(map[version]struct{}, len(m.SupportedVersions))
	for _, v := range m.SupportedVersions {
		if v == nil || v.Major != ProtocolMajor || v.Minor != ProtocolMinor {
			return nil, fail(connect.CodeInvalidArgument, "unsupported_protocol_version", "only protocol v1.0 is supported")
		}
		key := version{v.Major, v.Minor}
		if _, duplicate := seenVersions[key]; duplicate {
			return nil, fail(connect.CodeInvalidArgument, "duplicate_protocol_version", "supported protocol versions must be unique")
		}
		seenVersions[key] = struct{}{}
	}
	if len(m.RequiredCapabilities) > int(MaxRepeatedItems) {
		return nil, fail(connect.CodeInvalidArgument, "too_many_capabilities", "required capabilities exceed limit")
	}
	if err := validateCaps(m.RequiredCapabilities); err != nil {
		return nil, err
	}
	client, err := s.identity.ClientIdentity(ctx)
	if err != nil || client == nil {
		return nil, fail(connect.CodeUnauthenticated, "transport_identity_missing", "authenticated client identity is required")
	}
	if err := validateIdentity(client); err != nil {
		return nil, fail(connect.CodeUnauthenticated, "invalid_transport_identity", "authenticated client identity is invalid")
	}
	if err := validateExpectedIdentity(m.ExpectedServerIdentity, s.workerIdentity); err != nil {
		return nil, err
	}
	accepted := make([]workerv1alpha1.Capability, 0, len(m.RequiredCapabilities))
	acceptedSet := make(map[workerv1alpha1.Capability]struct{}, len(m.RequiredCapabilities))
	for _, c := range m.RequiredCapabilities {
		if _, ok := s.capabilities[c]; !ok {
			return nil, fail(connect.CodeFailedPrecondition, "capability_not_supported", fmt.Sprintf("capability %s is not supported", c.String()))
		}
		accepted = append(accepted, c)
		acceptedSet[c] = struct{}{}
	}
	now := s.now().UTC()
	expires := now.Add(s.ttl)
	id, err := s.newID()
	if err != nil {
		return nil, fail(connect.CodeInternal, "negotiation_id_generation_failed", "negotiation id generation failed")
	}
	if err := validateNegotiationID(id); err != nil {
		return nil, fail(connect.CodeInternal, "negotiation_id_invalid", "generated negotiation id is invalid")
	}
	s.mu.Lock()
	if _, exists := s.bindings[id]; exists {
		s.mu.Unlock()
		return nil, fail(connect.CodeInternal, "negotiation_id_collision", "negotiation id already exists")
	}
	s.bindings[id] = binding{version: version{ProtocolMajor, ProtocolMinor}, client: cloneIdentity(client), server: cloneIdentity(s.workerIdentity), caps: acceptedSet, expires: expires}
	s.mu.Unlock()
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{SelectedVersion: &workerv1alpha1.ProtocolVersion{Major: ProtocolMajor, Minor: ProtocolMinor}, AcceptedCapabilities: accepted, Server: s.ProtocolDescriptor(), AuthenticatedServerIdentity: cloneIdentity(s.workerIdentity), NegotiationId: id, ExpiresAt: timestamppb.New(expires)}), nil
}

func (s *Service) CheckHealth(ctx context.Context, req *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if req == nil || req.Msg == nil {
		return nil, fail(connect.CodeInvalidArgument, "invalid_request", "request is required")
	}
	client, err := s.identity.ClientIdentity(ctx)
	if err != nil || client == nil {
		return nil, fail(connect.CodeUnauthenticated, "transport_identity_missing", "authenticated client identity is required")
	}
	if err := validateIdentity(client); err != nil {
		return nil, fail(connect.CodeUnauthenticated, "invalid_transport_identity", "authenticated client identity is invalid")
	}
	b, err := s.validateBinding(req.Msg.Negotiation, client)
	if err != nil {
		return nil, err
	}
	if err := validateExpectedIdentity(req.Msg.ExpectedServerIdentity, s.workerIdentity); err != nil {
		return nil, err
	}
	if err := validateCaps(req.Msg.RequiredCapabilities); err != nil {
		return nil, err
	}
	for _, c := range req.Msg.RequiredCapabilities {
		if _, ok := b.caps[c]; !ok {
			return nil, fail(connect.CodeFailedPrecondition, "capability_not_negotiated", fmt.Sprintf("capability %s was not negotiated", c.String()))
		}
	}
	return connect.NewResponse(&workerv1alpha1.HealthResponse{State: workerv1alpha1.HealthState_HEALTH_STATE_SERVING, Protocol: s.ProtocolDescriptor(), ObservedAt: timestamppb.New(s.now().UTC())}), nil
}

func (s *Service) ExecuteOperation(ctx context.Context, _ *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return nil, fail(connect.CodeUnimplemented, "operation_dispatch_not_implemented", "operation dispatch is not implemented")
}
func (s *Service) GetOperationReceipt(ctx context.Context, _ *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	return nil, fail(connect.CodeUnimplemented, "durable_receipts_not_implemented", "durable receipt retrieval is not implemented")
}

func (s *Service) validateBinding(got *workerv1alpha1.NegotiationBinding, client *workerv1alpha1.WorkloadIdentity) (binding, error) {
	if got == nil || got.GetNegotiationId() == "" {
		return binding{}, fail(connect.CodeFailedPrecondition, "negotiation_required", "negotiation binding is required")
	}
	if err := validateNegotiationID(got.GetNegotiationId()); err != nil {
		return binding{}, fail(connect.CodeInvalidArgument, "negotiation_id_invalid", "negotiation id is invalid")
	}
	s.mu.Lock()
	b, ok := s.bindings[got.GetNegotiationId()]
	if ok && !s.now().UTC().Before(b.expires) {
		delete(s.bindings, got.GetNegotiationId())
	}
	s.mu.Unlock()
	if !ok {
		return binding{}, fail(connect.CodeFailedPrecondition, "negotiation_unknown", "negotiation binding is unknown")
	}
	now := s.now().UTC()
	if !now.Before(b.expires) {
		return binding{}, fail(connect.CodeDeadlineExceeded, "negotiation_expired", "negotiation binding has expired")
	}
	if !sameVersion(got.GetProtocolVersion(), b.version) {
		return binding{}, fail(connect.CodeFailedPrecondition, "negotiated_version_mismatch", "negotiated protocol version does not match")
	}
	if got.GetExpiresAt() == nil || got.GetExpiresAt().CheckValid() != nil || !got.GetExpiresAt().AsTime().Equal(b.expires) {
		return binding{}, fail(connect.CodeFailedPrecondition, "negotiation_expiry_mismatch", "negotiation expiry does not match")
	}
	if !sameIdentity(client, b.client) {
		return binding{}, fail(connect.CodePermissionDenied, "client_identity_mismatch", "authenticated client identity does not match negotiation")
	}
	return b, nil
}

func NewHandler(svc *Service, opts ...connect.HandlerOption) (string, http.Handler) {
	// Hard ceilings are appended last so callers cannot raise transport limits.
	// This remains a decoded-handler seam; no network/TLS listener or pre-decode
	// enforcement is claimed by this package.
	var impl workerv1alpha1connect.WorkerExecutionServiceHandler = svc
	if svc == nil {
		impl = workerv1alpha1connect.UnimplementedWorkerExecutionServiceHandler{}
	}
	return workerv1alpha1connect.NewWorkerExecutionServiceHandler(impl, append(opts, connect.WithReadMaxBytes(int(MaxWireMessageBytes)), connect.WithSendMaxBytes(int(MaxWireMessageBytes)))...)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fail(connect.CodeInvalidArgument, "invalid_context", "context is required")
	}
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fail(connect.CodeDeadlineExceeded, "deadline_exceeded", "request deadline exceeded")
		}
		return fail(connect.CodeCanceled, "request_canceled", "request canceled")
	default:
		return nil
	}
}

func fail(code connect.Code, stable, text string) error {
	return connect.NewError(code, errors.New("worker/"+stable+": "+text))
}

func validateCaps(caps []workerv1alpha1.Capability) error {
	if len(caps) > int(MaxRepeatedItems) {
		return fail(connect.CodeInvalidArgument, "too_many_capabilities", "capability count exceeds limit")
	}
	seen := make(map[workerv1alpha1.Capability]struct{}, len(caps))
	for _, c := range caps {
		if !knownCapability(c) || c == workerv1alpha1.Capability_CAPABILITY_UNSPECIFIED {
			return fail(connect.CodeInvalidArgument, "unknown_required_capability", fmt.Sprintf("unknown capability %d", c))
		}
		if _, duplicate := seen[c]; duplicate {
			return fail(connect.CodeInvalidArgument, "duplicate_capability", "capabilities must be unique")
		}
		seen[c] = struct{}{}
	}
	return nil
}
func knownCapability(c workerv1alpha1.Capability) bool {
	return c >= workerv1alpha1.Capability_CAPABILITY_NEGOTIATION && c <= workerv1alpha1.Capability_CAPABILITY_ADAPTER_REGISTRATION
}
func sameVersion(got *workerv1alpha1.ProtocolVersion, want version) bool {
	return got != nil && got.Major == want.major && got.Minor == want.minor
}
func sameIdentity(a, b *workerv1alpha1.WorkloadIdentity) bool {
	if a == nil || b == nil || a.SpiffeId != b.SpiffeId || a.TrustDomain != b.TrustDomain {
		return false
	}
	return string(a.LeafCertificateSha256) == string(b.LeafCertificateSha256)
}

func validateIdentity(i *workerv1alpha1.WorkloadIdentity) error {
	if i == nil || i.SpiffeId == "" || i.TrustDomain == "" {
		return errors.New("identity fields are required")
	}
	if !utf8.ValidString(i.SpiffeId) || !utf8.ValidString(i.TrustDomain) {
		return errors.New("identity fields must be valid UTF-8")
	}
	if len(i.SpiffeId) > int(MaxStringBytes) || len(i.TrustDomain) > int(MaxStringBytes) {
		return errors.New("identity string exceeds limit")
	}
	if n := len(i.LeafCertificateSha256); n != 0 && n != 32 {
		return errors.New("leaf certificate digest must be 32 bytes")
	}
	return nil
}
func validateExpectedIdentity(expected, actual *workerv1alpha1.WorkloadIdentity) error {
	if expected == nil || expected.SpiffeId == "" {
		return fail(connect.CodeInvalidArgument, "expected_server_identity_required", "expected server identity is required")
	}
	if err := validateIdentity(expected); err != nil {
		return fail(connect.CodeInvalidArgument, "invalid_expected_server_identity", "expected server identity is invalid")
	}
	if !sameIdentity(expected, actual) {
		return fail(connect.CodePermissionDenied, "server_identity_mismatch", "expected server identity does not match worker identity")
	}
	return nil
}
func cloneIdentity(i *workerv1alpha1.WorkloadIdentity) *workerv1alpha1.WorkloadIdentity {
	if i == nil {
		return nil
	}
	out := &workerv1alpha1.WorkloadIdentity{SpiffeId: i.SpiffeId, TrustDomain: i.TrustDomain}
	out.LeafCertificateSha256 = append([]byte(nil), i.LeafCertificateSha256...)
	return out
}
func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validateNegotiationID(id string) error {
	if id == "" || !utf8.ValidString(id) || len(id) > int(MaxIdentifierBytes) {
		return errors.New("negotiation id is empty, invalid UTF-8, or overlong")
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return errors.New("negotiation id contains control characters")
		}
	}
	return nil
}
