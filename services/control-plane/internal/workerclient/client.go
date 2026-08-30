// Package workerclient is the Control Plane side of the generated Worker wire.
package workerclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	workerProtocolMajor       uint32 = 1
	workerProtocolMinor       uint32 = 0
	workerMaxWireMessageBytes uint32 = 1 << 20
	workerMaxRepeatedItems    uint32 = 64
	workerMaxStringBytes      uint32 = 1024
	workerMaxPayloadBytes     uint32 = 64 << 10
	workerMaxDeadlineSeconds  uint32 = 300
	workerMaxIdentifierBytes  uint32 = 256
)

var errInvalidConfig = errors.New("worker client configuration is invalid")

type Clock func() time.Time

type Config struct {
	Client                 workerv1alpha1connect.WorkerExecutionServiceClient
	RuntimeClient          workerruntimev1alpha1connect.WorkerRuntimeServiceClient
	ExpectedWorkerIdentity *workerv1alpha1.WorkloadIdentity
	Clock                  Clock
}

type Supervisor struct {
	client         workerv1alpha1connect.WorkerExecutionServiceClient
	runtimeClient  workerruntimev1alpha1connect.WorkerRuntimeServiceClient
	workerIdentity *workerv1alpha1.WorkloadIdentity
	now            Clock
	bindMu         sync.Mutex
	mu             sync.RWMutex
	binding        *bindingState
}

type bindingState struct {
	negotiationID string
	expiresAt     time.Time
	accepted      []workerv1alpha1.Capability
	descriptor    *workerv1alpha1.ProtocolDescriptor
}

func New(config Config) (*Supervisor, error) {
	if !clientAvailable(config.Client) || !runtimeClientAvailable(config.RuntimeClient) || !validIdentity(config.ExpectedWorkerIdentity) {
		return nil, errInvalidConfig
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Supervisor{client: config.Client, runtimeClient: config.RuntimeClient, workerIdentity: cloneIdentity(config.ExpectedWorkerIdentity), now: config.Clock}, nil
}

func (supervisor *Supervisor) BindRuntime(ctx context.Context) error {
	if supervisor == nil || !clientAvailable(supervisor.client) || !runtimeClientAvailable(supervisor.runtimeClient) || !validIdentity(supervisor.workerIdentity) || supervisor.now == nil {
		return errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	supervisor.bindMu.Lock()
	defer supervisor.bindMu.Unlock()
	if err := contextErr(ctx); err != nil {
		return err
	}
	capabilities := runtimeCapabilities()
	response, err := supervisor.client.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: workerProtocolMajor, Minor: workerProtocolMinor}},
		RequiredCapabilities:   capabilities,
		ExpectedServerIdentity: cloneIdentity(supervisor.workerIdentity),
	}))
	if err != nil {
		return rpcFailure("negotiate", err)
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	now := supervisor.now().UTC()
	state, err := validateNegotiation(response, supervisor.workerIdentity, now, capabilities)
	if err != nil {
		return err
	}
	supervisor.mu.Lock()
	supervisor.binding = state
	supervisor.mu.Unlock()
	return nil
}

func (supervisor *Supervisor) CheckRuntimeHealth(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := supervisor.ensureBinding(ctx); err != nil {
		return err
	}
	supervisor.mu.RLock()
	state := cloneBinding(supervisor.binding)
	supervisor.mu.RUnlock()
	if state == nil {
		return fail(connect.CodeFailedPrecondition, "binding_required")
	}
	response, err := supervisor.client.CheckHealth(ctx, connect.NewRequest(&workerv1alpha1.HealthRequest{
		Negotiation: state.negotiation(), RequiredCapabilities: append([]workerv1alpha1.Capability(nil), state.accepted...), ExpectedServerIdentity: cloneIdentity(supervisor.workerIdentity),
	}))
	if err != nil {
		return rpcFailure("health", err)
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	now := supervisor.now().UTC()
	if !now.Before(state.expiresAt) {
		supervisor.clearBinding(state)
		return fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	if response == nil || response.Msg == nil || response.Msg.GetState() != workerv1alpha1.HealthState_HEALTH_STATE_SERVING || response.Msg.GetObservedAt() == nil || response.Msg.GetObservedAt().CheckValid() != nil {
		return fail(connect.CodeFailedPrecondition, "worker_not_serving")
	}
	descriptor, err := validateDescriptor(response.Msg.GetProtocol())
	if err != nil || !proto.Equal(descriptor, state.descriptor) {
		return fail(connect.CodeFailedPrecondition, "protocol_descriptor_drift")
	}
	return nil
}

func (supervisor *Supervisor) ensureBinding(ctx context.Context) error {
	if supervisor == nil || supervisor.now == nil {
		return errInvalidConfig
	}
	supervisor.mu.RLock()
	state := cloneBinding(supervisor.binding)
	supervisor.mu.RUnlock()
	if state != nil && supervisor.now().UTC().Before(state.expiresAt) {
		return nil
	}
	if state != nil {
		supervisor.clearBinding(state)
	}
	return supervisor.BindRuntime(ctx)
}

type RuntimeSession struct {
	stream     *connect.BidiStreamForClient[workerruntimev1alpha1.RuntimeSessionRequest, workerruntimev1alpha1.RuntimeSessionResponse]
	execution  string
	generation uint64
	sendMu     sync.Mutex
}

func (supervisor *Supervisor) OpenRuntimeSession(ctx context.Context, executionID, providerKind string, generation uint64, fencing *workerv1alpha1.FencingProof) (*RuntimeSession, error) {
	if supervisor == nil || !runtimeClientAvailable(supervisor.runtimeClient) || !validIdentity(supervisor.workerIdentity) || executionID == "" || providerKind == "" || generation == 0 || fencing == nil {
		return nil, errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if err := supervisor.ensureBinding(ctx); err != nil {
		return nil, err
	}
	supervisor.mu.RLock()
	state := cloneBinding(supervisor.binding)
	supervisor.mu.RUnlock()
	if state == nil || !supervisor.now().UTC().Before(state.expiresAt) {
		return nil, fail(connect.CodeDeadlineExceeded, "binding_expired")
	}
	if fencing.GetGeneration() != generation || fencing.GetLeaseId() == "" || len(fencing.GetToken()) == 0 {
		return nil, fail(connect.CodeInvalidArgument, "runtime_fencing_invalid")
	}
	stream := supervisor.runtimeClient.OpenSession(ctx)
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Open{Open: &workerruntimev1alpha1.RuntimeSessionOpen{
		Negotiation: state.negotiation(), Fencing: proto.Clone(fencing).(*workerv1alpha1.FencingProof), ExecutionId: executionID, Generation: generation, ExpectedWorkerIdentity: cloneIdentity(supervisor.workerIdentity), ProviderKind: providerKind,
	}}}); err != nil {
		return nil, rpcFailure("runtime_open", err)
	}
	ready, err := stream.Receive()
	if err != nil {
		return nil, rpcFailure("runtime_ready", err)
	}
	if ready == nil || ready.GetReady() == nil || ready.GetReady().GetExecutionId() != executionID || ready.GetReady().GetGeneration() != generation || ready.GetReady().GetProtocolMajor() != runtimeprotocol.ProtocolMajor || ready.GetReady().GetProtocolMinor() != runtimeprotocol.ProtocolMinor {
		return nil, fail(connect.CodeInternal, "runtime_ready_invalid")
	}
	return &RuntimeSession{stream: stream, execution: executionID, generation: generation}, nil
}

func (session *RuntimeSession) Send(ctx context.Context, command runtimeprotocol.Command) error {
	if session == nil || session.stream == nil {
		return errInvalidConfig
	}
	if err := contextErr(ctx); err != nil {
		return err
	}
	if command.ExecutionID != session.execution || command.Generation != session.generation || runtimeprotocol.ValidateCommand(command) != nil {
		return fail(connect.CodeInvalidArgument, "runtime_command_invalid")
	}
	encoded, err := json.Marshal(command)
	if err != nil || len(encoded) > runtimeprotocol.MaxCommandBytes {
		return fail(connect.CodeInvalidArgument, "runtime_command_too_large")
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Command{Command: &workerruntimev1alpha1.RuntimeCommandFrame{Json: encoded}}})
}

func (session *RuntimeSession) Receive() (runtimeprotocol.Message, error) {
	if session == nil || session.stream == nil {
		return runtimeprotocol.Message{}, errInvalidConfig
	}
	response, err := session.stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return runtimeprotocol.Message{}, io.EOF
		}
		return runtimeprotocol.Message{}, rpcFailure("runtime_receive", err)
	}
	if response == nil {
		return runtimeprotocol.Message{}, fail(connect.CodeInternal, "runtime_message_invalid")
	}
	if runtimeError := response.GetError(); runtimeError != nil {
		return runtimeprotocol.Message{}, fmt.Errorf("runtime %s: %s", runtimeError.GetCode(), runtimeError.GetMessage())
	}
	if len(response.GetJson()) == 0 || len(response.GetJson()) > runtimeprotocol.MaxMessageBytes {
		return runtimeprotocol.Message{}, fail(connect.CodeInternal, "runtime_message_invalid")
	}
	var message runtimeprotocol.Message
	if err := json.Unmarshal(response.GetJson(), &message); err != nil || runtimeprotocol.ValidateMessage(message) != nil {
		return runtimeprotocol.Message{}, fail(connect.CodeInternal, "runtime_message_invalid")
	}
	if message.ExecutionID != session.execution || message.Generation != session.generation {
		return runtimeprotocol.Message{}, fail(connect.CodePermissionDenied, "runtime_message_identity_mismatch")
	}
	return message, nil
}

func (session *RuntimeSession) CloseRequest() error {
	if session == nil || session.stream == nil {
		return nil
	}
	session.sendMu.Lock()
	defer session.sendMu.Unlock()
	return session.stream.CloseRequest()
}

func (session *RuntimeSession) CloseResponse() error {
	if session == nil || session.stream == nil {
		return nil
	}
	return session.stream.CloseResponse()
}

type MTLSConfig struct {
	Endpoint               string
	ExpectedWorkerIdentity *workerv1alpha1.WorkloadIdentity
	ClientCertificate      tls.Certificate
	RootCAs                *x509.CertPool
	ServerName             string
	Clock                  Clock
}

func NewMTLS(config MTLSConfig) (*Supervisor, error) {
	endpoint, err := validateMTLSEndpoint(config.Endpoint)
	if err != nil || !validIdentity(config.ExpectedWorkerIdentity) || config.RootCAs == nil || len(config.ClientCertificate.Certificate) == 0 || config.ClientCertificate.PrivateKey == nil {
		return nil, errInvalidConfig
	}
	serverName := config.ServerName
	if serverName == "" {
		parsed, _ := url.Parse(endpoint)
		serverName = parsed.Hostname()
	}
	if strings.TrimSpace(serverName) != serverName || serverName == "" {
		return nil, errInvalidConfig
	}
	expectedIdentity := cloneIdentity(config.ExpectedWorkerIdentity)
	transport := &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: config.RootCAs, Certificates: []tls.Certificate{config.ClientCertificate}, ServerName: serverName, VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errInvalidConfig
			}
			actual, err := peerIdentity(state.PeerCertificates[0])
			if err != nil || actual.GetSpiffeId() != expectedIdentity.GetSpiffeId() || actual.GetTrustDomain() != expectedIdentity.GetTrustDomain() || (len(expectedIdentity.GetLeafCertificateSha256()) != 0 && !bytes.Equal(actual.GetLeafCertificateSha256(), expectedIdentity.GetLeafCertificateSha256())) {
				return errInvalidConfig
			}
			return nil
		}},
	}
	httpClient := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errInvalidConfig }}
	return New(Config{Client: workerv1alpha1connect.NewWorkerExecutionServiceClient(httpClient, endpoint), RuntimeClient: workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(httpClient, endpoint), ExpectedWorkerIdentity: config.ExpectedWorkerIdentity, Clock: config.Clock})
}

func validateNegotiation(response *connect.Response[workerv1alpha1.NegotiationResponse], expected *workerv1alpha1.WorkloadIdentity, now time.Time, required []workerv1alpha1.Capability) (*bindingState, error) {
	if response == nil || response.Msg == nil {
		return nil, fail(connect.CodeInternal, "negotiation_response_missing")
	}
	message := response.Msg
	if !isProtocolVersion(message.GetSelectedVersion()) || !exactCapabilities(message.GetAcceptedCapabilities(), required) || !sameIdentity(message.GetAuthenticatedServerIdentity(), expected) || !validNegotiationID(message.GetNegotiationId()) || message.GetExpiresAt() == nil || message.GetExpiresAt().CheckValid() != nil {
		return nil, fail(connect.CodeFailedPrecondition, "negotiation_invalid")
	}
	descriptor, err := validateDescriptor(message.GetServer())
	if err != nil || !exactCapabilities(descriptor.GetCapabilities(), required) {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_invalid")
	}
	expiresAt := message.GetExpiresAt().AsTime().UTC()
	if !now.Before(expiresAt) {
		return nil, fail(connect.CodeDeadlineExceeded, "negotiation_already_expired")
	}
	return &bindingState{negotiationID: message.GetNegotiationId(), expiresAt: expiresAt, accepted: append([]workerv1alpha1.Capability(nil), required...), descriptor: descriptor}, nil
}

func validateDescriptor(descriptor *workerv1alpha1.ProtocolDescriptor) (*workerv1alpha1.ProtocolDescriptor, error) {
	if descriptor == nil || !isProtocolVersion(descriptor.GetCurrentVersion()) || !isProtocolVersion(descriptor.GetMinimumCompatibleVersion()) ||
		descriptor.GetMaxPayloadBytes() == 0 || descriptor.GetMaxPayloadBytes() > workerMaxPayloadBytes || descriptor.GetMaxDeadlineSeconds() == 0 || descriptor.GetMaxDeadlineSeconds() > workerMaxDeadlineSeconds ||
		descriptor.GetMaxWireMessageBytes() == 0 || descriptor.GetMaxWireMessageBytes() > workerMaxWireMessageBytes || descriptor.GetMaxRepeatedItems() == 0 || descriptor.GetMaxRepeatedItems() > workerMaxRepeatedItems ||
		descriptor.GetMaxStringBytes() == 0 || descriptor.GetMaxStringBytes() > workerMaxStringBytes || descriptor.GetMaxPayloadBytes() > descriptor.GetMaxWireMessageBytes() || len(descriptor.GetCapabilities()) > int(descriptor.GetMaxRepeatedItems()) || !uniqueKnownCapabilities(descriptor.GetCapabilities()) {
		return nil, fail(connect.CodeFailedPrecondition, "protocol_descriptor_invalid")
	}
	canonical := proto.Clone(descriptor).(*workerv1alpha1.ProtocolDescriptor)
	sort.Slice(canonical.Capabilities, func(i, j int) bool { return canonical.Capabilities[i] < canonical.Capabilities[j] })
	return canonical, nil
}

func runtimeCapabilities() []workerv1alpha1.Capability {
	return []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH}
}

func exactCapabilities(got, want []workerv1alpha1.Capability) bool {
	if len(got) != len(want) || !uniqueKnownCapabilities(got) || !uniqueKnownCapabilities(want) {
		return false
	}
	for _, capability := range want {
		found := false
		for _, candidate := range got {
			if candidate == capability {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func uniqueKnownCapabilities(capabilities []workerv1alpha1.Capability) bool {
	seen := make(map[workerv1alpha1.Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		switch capability {
		case workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH, workerv1alpha1.Capability_CAPABILITY_DURABLE_RECEIPTS, workerv1alpha1.Capability_CAPABILITY_FINALIZERS, workerv1alpha1.Capability_CAPABILITY_ADAPTER_REGISTRATION:
		default:
			return false
		}
		if _, duplicate := seen[capability]; duplicate {
			return false
		}
		seen[capability] = struct{}{}
	}
	return true
}

func (state *bindingState) negotiation() *workerv1alpha1.NegotiationBinding {
	return &workerv1alpha1.NegotiationBinding{ProtocolVersion: &workerv1alpha1.ProtocolVersion{Major: workerProtocolMajor, Minor: workerProtocolMinor}, NegotiationId: state.negotiationID, ExpiresAt: timestamppb.New(state.expiresAt.UTC())}
}

func cloneBinding(state *bindingState) *bindingState {
	if state == nil {
		return nil
	}
	return &bindingState{negotiationID: state.negotiationID, expiresAt: state.expiresAt, accepted: append([]workerv1alpha1.Capability(nil), state.accepted...), descriptor: proto.Clone(state.descriptor).(*workerv1alpha1.ProtocolDescriptor)}
}

func (supervisor *Supervisor) clearBinding(state *bindingState) {
	supervisor.mu.Lock()
	if supervisor.binding != nil && supervisor.binding.negotiationID == state.negotiationID && supervisor.binding.expiresAt.Equal(state.expiresAt) {
		supervisor.binding = nil
	}
	supervisor.mu.Unlock()
}

func isProtocolVersion(version *workerv1alpha1.ProtocolVersion) bool {
	return version != nil && version.GetMajor() == workerProtocolMajor && version.GetMinor() == workerProtocolMinor
}

func validIdentity(identity *workerv1alpha1.WorkloadIdentity) bool {
	if identity == nil || identity.GetSpiffeId() == "" || identity.GetTrustDomain() == "" || !utf8.ValidString(identity.GetSpiffeId()) || !utf8.ValidString(identity.GetTrustDomain()) || len(identity.GetSpiffeId()) > int(workerMaxStringBytes) || len(identity.GetTrustDomain()) > int(workerMaxStringBytes) {
		return false
	}
	digestLength := len(identity.GetLeafCertificateSha256())
	return digestLength == 0 || digestLength == sha256.Size
}

func sameIdentity(left, right *workerv1alpha1.WorkloadIdentity) bool {
	return validIdentity(left) && validIdentity(right) && left.GetSpiffeId() == right.GetSpiffeId() && left.GetTrustDomain() == right.GetTrustDomain() && subtle.ConstantTimeCompare(left.GetLeafCertificateSha256(), right.GetLeafCertificateSha256()) == 1
}

func cloneIdentity(identity *workerv1alpha1.WorkloadIdentity) *workerv1alpha1.WorkloadIdentity {
	if identity == nil {
		return nil
	}
	return proto.Clone(identity).(*workerv1alpha1.WorkloadIdentity)
}

func validNegotiationID(value string) bool {
	if value == "" || !utf8.ValidString(value) || len(value) > int(workerMaxIdentifierBytes) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func peerIdentity(certificate *x509.Certificate) (*workerv1alpha1.WorkloadIdentity, error) {
	if certificate == nil || len(certificate.URIs) != 1 {
		return nil, errInvalidConfig
	}
	spiffe := certificate.URIs[0]
	if spiffe.Scheme != "spiffe" || spiffe.Host == "" || spiffe.Path == "" || strings.Contains(spiffe.Host, "/") || spiffe.RawQuery != "" || spiffe.Fragment != "" || spiffe.User != nil {
		return nil, errInvalidConfig
	}
	if _, err := url.ParseRequestURI(spiffe.String()); err != nil {
		return nil, errInvalidConfig
	}
	digest := sha256.Sum256(certificate.Raw)
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: spiffe.String(), TrustDomain: spiffe.Host, LeafCertificateSha256: digest[:]}
	if !validIdentity(identity) {
		return nil, errInvalidConfig
	}
	return identity, nil
}

func validateMTLSEndpoint(value string) (string, error) {
	if strings.TrimSpace(value) != value || !strings.HasPrefix(value, "https://") {
		return "", errInvalidConfig
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errInvalidConfig
	}
	return strings.TrimSuffix(value, "/"), nil
}

func clientAvailable(client workerv1alpha1connect.WorkerExecutionServiceClient) bool {
	return nonNilInterface(client)
}

func runtimeClientAvailable(client workerruntimev1alpha1connect.WorkerRuntimeServiceClient) bool {
	return nonNilInterface(client)
}

func nonNilInterface(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return !reflected.IsNil()
	default:
		return true
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fail(connect.CodeInvalidArgument, "invalid_context")
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fail(connect.CodeDeadlineExceeded, "deadline_exceeded")
		}
		return fail(connect.CodeCanceled, "request_canceled")
	}
	return nil
}

func fail(code connect.Code, stable string) error {
	return connect.NewError(code, errors.New("worker_client/"+stable))
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
