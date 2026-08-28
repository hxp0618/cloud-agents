package supervisor

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeWorkerClient struct {
	negotiateFn func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error)
	healthFn    func(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error)
	executeFn   func(context.Context, *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error)
	receiptFn   func(context.Context, *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error)

	negotiateCalls int
	healthCalls    int
	executeCalls   int
	receiptCalls   int
}

var _ workerv1alpha1connect.WorkerExecutionServiceClient = (*fakeWorkerClient)(nil)

func (fake *fakeWorkerClient) Negotiate(ctx context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	fake.negotiateCalls++
	if fake.negotiateFn != nil {
		return fake.negotiateFn(ctx, request)
	}
	return nil, errors.New("fake negotiate not configured")
}

func (fake *fakeWorkerClient) CheckHealth(ctx context.Context, request *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
	fake.healthCalls++
	if fake.healthFn != nil {
		return fake.healthFn(ctx, request)
	}
	return nil, errors.New("fake health not configured")
}

func (fake *fakeWorkerClient) ExecuteOperation(ctx context.Context, request *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	fake.executeCalls++
	if fake.executeFn != nil {
		return fake.executeFn(ctx, request)
	}
	return nil, errors.New("execute must not be called")
}

func (fake *fakeWorkerClient) GetOperationReceipt(ctx context.Context, request *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
	fake.receiptCalls++
	if fake.receiptFn != nil {
		return fake.receiptFn(ctx, request)
	}
	return nil, errors.New("receipt must not be called")
}

func TestRemoteOperationDispatchUsesThePublicSupervisorMethods(t *testing.T) {
	fixture := newLocalDispatchFixture(t, true)
	fake := &fakeWorkerClient{}
	fake.negotiateFn = func(_ context.Context, _ *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		response := validNegotiationResponse(fixture.workerIdentity, fixture.now.Add(time.Minute), "remote-operation")
		response.Msg.AcceptedCapabilities = operationAdmissionCapabilities()
		response.Msg.Server = descriptorWithCapabilities(operationAdmissionCapabilities())
		return response, nil
	}
	var received *workerv1alpha1.OperationAttemptEnvelope
	tokenDigest := sha256.Sum256(fixture.token)
	receipt := &workerv1alpha1.DurableReceipt{
		ReceiptId: "remote-receipt", OperationId: fixture.attempt.GetOperation().GetOperationId(),
		AttemptId: fixture.attempt.GetAttemptId(), IdempotencyKey: fixture.attempt.GetOperation().GetIdempotencyKey(), Sequence: 1,
		Fencing: &workerv1alpha1.FencingStamp{LeaseId: "lease-local", Generation: 7, TokenSha256: tokenDigest[:]},
		Outcome: workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED, ObservedAt: timestamppb.New(fixture.now),
	}
	fake.executeFn = func(_ context.Context, request *connect.Request[workerv1alpha1.OperationAttemptEnvelope]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
		received = proto.Clone(request.Msg).(*workerv1alpha1.OperationAttemptEnvelope)
		return connect.NewResponse(proto.Clone(receipt).(*workerv1alpha1.DurableReceipt)), nil
	}
	fake.receiptFn = func(_ context.Context, request *connect.Request[workerv1alpha1.ReceiptRequest]) (*connect.Response[workerv1alpha1.DurableReceipt], error) {
		if request.Msg.GetReceiptId() != receipt.GetReceiptId() {
			t.Fatalf("receipt request = %v", request.Msg)
		}
		return connect.NewResponse(proto.Clone(receipt).(*workerv1alpha1.DurableReceipt)), nil
	}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: fixture.workerIdentity, Clock: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := supervisor.BindOperationAdmission(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	attempt := proto.Clone(fixture.attempt).(*workerv1alpha1.OperationAttemptEnvelope)
	attempt.Negotiation = binding.Negotiation()
	response, err := supervisor.DispatchOperation(context.Background(), attempt)
	if err != nil || response == nil || response.Msg.GetReceiptId() != receipt.GetReceiptId() {
		t.Fatalf("remote dispatch = %v / %v", response, err)
	}
	if fake.executeCalls != 1 || received == nil || received.GetNegotiation().GetNegotiationId() != binding.NegotiationID {
		t.Fatalf("remote execute calls=%d request=%v", fake.executeCalls, received)
	}
	receiptRequest := &workerv1alpha1.ReceiptRequest{
		OperationId: attempt.GetOperation().GetOperationId(), ReceiptId: receipt.GetReceiptId(),
		Fencing:                proto.Clone(attempt.GetOperation().GetFencing()).(*workerv1alpha1.FencingProof),
		ExpectedServerIdentity: proto.Clone(fixture.workerIdentity).(*workerv1alpha1.WorkloadIdentity),
		Negotiation:            binding.Negotiation(), RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
	}
	if _, err := supervisor.GetOperationReceipt(context.Background(), receiptRequest); err != nil {
		t.Fatal(err)
	}
	if fake.receiptCalls != 1 {
		t.Fatalf("remote receipt calls = %d", fake.receiptCalls)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	identity := testWorkerIdentity()
	if supervisor, err := New(Config{ExpectedWorkerIdentity: identity}); supervisor != nil || !errors.Is(err, errInvalidConfig) {
		t.Fatalf("nil client result = %#v / %v", supervisor, err)
	}
	if supervisor, err := New(Config{Client: &fakeWorkerClient{}}); supervisor != nil || !errors.Is(err, errInvalidConfig) {
		t.Fatalf("nil identity result = %#v / %v", supervisor, err)
	}
	bad := &workerv1alpha1.WorkloadIdentity{SpiffeId: strings.Repeat("x", int(workerkernel.MaxStringBytes)+1), TrustDomain: "cloud-agents.test"}
	if supervisor, err := New(Config{Client: &fakeWorkerClient{}, ExpectedWorkerIdentity: bad}); supervisor != nil || !errors.Is(err, errInvalidConfig) {
		t.Fatalf("overlong identity result = %#v / %v", supervisor, err)
	}
}

func TestNewMTLSRequiresHTTPSAndExplicitCredentials(t *testing.T) {
	identity := testWorkerIdentity()
	rootCAs := x509.NewCertPool()
	credentials := tls.Certificate{Certificate: [][]byte{{1}}, PrivateKey: &rsa.PrivateKey{}}
	base := MTLSConfig{Endpoint: "https://worker.example:8091", ExpectedWorkerIdentity: identity, ClientCertificate: credentials, RootCAs: rootCAs}
	for name, mutate := range map[string]func(*MTLSConfig){
		"cleartext":           func(config *MTLSConfig) { config.Endpoint = "http://worker.example:8091" },
		"missing client cert": func(config *MTLSConfig) { config.ClientCertificate = tls.Certificate{} },
		"missing private key": func(config *MTLSConfig) { config.ClientCertificate.PrivateKey = nil },
		"missing CA pool":     func(config *MTLSConfig) { config.RootCAs = nil },
	} {
		config := base
		mutate(&config)
		if supervisor, err := NewMTLS(config); supervisor != nil || !errors.Is(err, errInvalidMTLSConfig) {
			t.Errorf("%s result = %#v / %v", name, supervisor, err)
		}
	}
	if supervisor, err := NewMTLS(base); err != nil || supervisor == nil {
		t.Fatalf("valid mTLS constructor result = %#v / %v", supervisor, err)
	}
}

func TestBindUsesFixedProfileAndCopiesState(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	response := validNegotiationResponse(expected, now.Add(2*time.Minute), "binding-alpha")
	fake := &fakeWorkerClient{negotiateFn: func(_ context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		message := request.Msg
		if len(message.GetSupportedVersions()) != 1 || !isProtocolVersion(message.GetSupportedVersions()[0]) {
			t.Fatalf("supported versions = %#v", message.GetSupportedVersions())
		}
		if !exactCapabilities(message.GetRequiredCapabilities(), requiredCapabilities()) {
			t.Fatalf("required capabilities = %#v", message.GetRequiredCapabilities())
		}
		if !sameIdentity(message.GetExpectedServerIdentity(), expected) {
			t.Fatalf("expected identity = %#v", message.GetExpectedServerIdentity())
		}
		return response, nil
	}}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := supervisor.Bind(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.NegotiationID != "binding-alpha" || !snapshot.ExpiresAt.Equal(now.Add(2*time.Minute)) || len(snapshot.AcceptedCapabilities) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !sameIdentity(snapshot.ServerIdentity, expected) || !proto.Equal(snapshot.Protocol, response.Msg.GetServer()) {
		t.Fatalf("snapshot identity/descriptor = %#v / %#v", snapshot.ServerIdentity, snapshot.Protocol)
	}

	response.Msg.NegotiationId = "mutated"
	response.Msg.AuthenticatedServerIdentity.SpiffeId = "mutated"
	response.Msg.Server.Capabilities[0] = workerv1alpha1.Capability_CAPABILITY_ADAPTER_REGISTRATION
	current, ok := supervisor.CurrentBinding()
	if !ok || current.NegotiationID != "binding-alpha" || !sameIdentity(current.ServerIdentity, expected) || !containsCapability(current.Protocol.GetCapabilities(), workerv1alpha1.Capability_CAPABILITY_NEGOTIATION) {
		t.Fatalf("current binding changed through response mutation = %#v", current)
	}
	binding := current.Negotiation()
	if binding.GetNegotiationId() != "binding-alpha" || !binding.GetExpiresAt().AsTime().Equal(now.Add(2*time.Minute)) || !isProtocolVersion(binding.GetProtocolVersion()) {
		t.Fatalf("negotiation binding = %#v", binding)
	}
}

func TestBindOperationAdmissionUsesSeparateFixedProfile(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	response := validNegotiationResponse(expected, now.Add(time.Minute), "binding-operation-admission")
	response.Msg.AcceptedCapabilities = operationAdmissionCapabilities()
	response.Msg.Server = descriptorWithCapabilities(operationAdmissionCapabilities())
	fake := &fakeWorkerClient{
		negotiateFn: func(_ context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
			if !exactCapabilities(request.Msg.GetRequiredCapabilities(), operationAdmissionCapabilities()) {
				t.Fatalf("operation admission request capabilities = %#v", request.Msg.GetRequiredCapabilities())
			}
			return response, nil
		},
		healthFn: func(_ context.Context, request *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
			if !exactCapabilities(request.Msg.GetRequiredCapabilities(), operationAdmissionCapabilities()) {
				t.Fatalf("operation admission health capabilities = %#v", request.Msg.GetRequiredCapabilities())
			}
			return connect.NewResponse(&workerv1alpha1.HealthResponse{State: workerv1alpha1.HealthState_HEALTH_STATE_SERVING, Protocol: descriptorWithCapabilities(operationAdmissionCapabilities()), ObservedAt: timestamppb.New(now)}), nil
		},
	}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := supervisor.BindOperationAdmission(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProfileID != OperationAdmissionProfileID || !exactCapabilities(snapshot.AcceptedCapabilities, operationAdmissionCapabilities()) {
		t.Fatalf("operation admission snapshot = %#v", snapshot)
	}
	if _, err := supervisor.CheckHealth(context.Background()); err != nil {
		t.Fatalf("operation admission health = %v", err)
	}
}

func TestBindOperationAdmissionRejectsHealthOnlyWorker(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	fake := &fakeWorkerClient{negotiateFn: func(_ context.Context, _ *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return validNegotiationResponse(expected, now.Add(time.Minute), "binding-health-only"), nil
	}}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = supervisor.BindOperationAdmission(context.Background())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "negotiated_capabilities_invalid") {
		t.Fatalf("health-only operation binding err = %v", err)
	}
}

func TestBindCanonicalizesCapabilityOrder(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	response := validNegotiationResponse(expected, now.Add(time.Minute), "binding-order")
	response.Msg.AcceptedCapabilities = []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
	}
	response.Msg.Server.Capabilities = []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
	}
	fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return response, nil
	}}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := supervisor.Bind(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !exactCapabilities(snapshot.AcceptedCapabilities, requiredCapabilities()) {
		t.Fatalf("accepted capabilities were not treated as a set: %#v", snapshot.AcceptedCapabilities)
	}
	wantDescriptor := testDescriptor()
	if !proto.Equal(snapshot.Protocol, wantDescriptor) {
		t.Fatalf("descriptor was not canonicalized: got=%v want=%v", snapshot.Protocol, wantDescriptor)
	}
}

func TestGeneratedConnectClientBindsWorkerServiceInProcess(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	workerIdentity := testWorkerIdentity()
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{
		SpiffeId:    "spiffe://cloud-agents.test/supervisor/test",
		TrustDomain: "cloud-agents.test",
	}
	workerService, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity:   workerIdentity,
		IdentityProvider: workerkernel.StaticIdentityProvider{Identity: supervisorIdentity},
		NegotiationTTL:   time.Minute,
		IDGenerator:      func() (string, error) { return "in-process-binding", nil },
		Clock:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	path, handler := workerkernel.NewHandler(workerService)
	ts := httptest.NewServer(handler)
	defer ts.Close()
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(ts.Client(), strings.TrimSuffix(ts.URL, path))
	supervisor, err := New(Config{Client: client, ExpectedWorkerIdentity: workerIdentity, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.CheckHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBindDoesNotCommitAfterContextCancellation(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		cancel()
		return validNegotiationResponse(expected, now.Add(time.Minute), "binding-canceled"), nil
	}}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(ctx); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("canceled bind error = %v", err)
	}
	if _, ok := supervisor.CurrentBinding(); ok {
		t.Fatal("canceled bind committed state")
	}
}

func TestBindRejectsUnknownCapabilityAndInconsistentDescriptor(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	cases := []struct {
		name       string
		mutate     func(*workerv1alpha1.NegotiationResponse)
		wantStable string
	}{
		{
			name: "unknown descriptor capability",
			mutate: func(response *workerv1alpha1.NegotiationResponse) {
				response.Server.Capabilities = append(response.Server.Capabilities, workerv1alpha1.Capability(99))
			},
			wantStable: "protocol_descriptor_capabilities_invalid",
		},
		{
			name: "payload exceeds wire",
			mutate: func(response *workerv1alpha1.NegotiationResponse) {
				response.Server.MaxWireMessageBytes = 128
				response.Server.MaxPayloadBytes = 129
			},
			wantStable: "protocol_descriptor_bounds_invalid",
		},
		{
			name: "capability count exceeds advertised limit",
			mutate: func(response *workerv1alpha1.NegotiationResponse) {
				response.Server.MaxRepeatedItems = 1
			},
			wantStable: "protocol_descriptor_capabilities_invalid",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := validNegotiationResponse(expected, now.Add(time.Minute), "binding-descriptor")
			test.mutate(response.Msg)
			fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
				return response, nil
			}}
			supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = supervisor.Bind(context.Background())
			if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), test.wantStable) {
				t.Fatalf("error = %v, code = %v", err, connect.CodeOf(err))
			}
			if _, ok := supervisor.CurrentBinding(); ok {
				t.Fatal("invalid descriptor committed a binding")
			}
		})
	}
}

func TestBindRejectsMalformedResponses(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	cases := []struct {
		name       string
		response   *connect.Response[workerv1alpha1.NegotiationResponse]
		wantCode   connect.Code
		wantStable string
	}{
		{name: "missing", response: nil, wantCode: connect.CodeInternal, wantStable: "negotiation_response_missing"},
		{name: "version", response: validNegotiationResponse(expected, now.Add(time.Minute), "id"), wantCode: connect.CodeFailedPrecondition, wantStable: "negotiated_version_invalid"},
		{name: "capability missing", response: validNegotiationResponse(expected, now.Add(time.Minute), "id"), wantCode: connect.CodeFailedPrecondition, wantStable: "negotiated_capabilities_invalid"},
		{name: "identity", response: validNegotiationResponse(expected, now.Add(time.Minute), "id"), wantCode: connect.CodePermissionDenied, wantStable: "server_identity_mismatch"},
		{name: "id", response: validNegotiationResponse(expected, now.Add(time.Minute), "bad\x00id"), wantCode: connect.CodeFailedPrecondition, wantStable: "negotiation_id_invalid"},
		{name: "expiry", response: validNegotiationResponse(expected, now.Add(-time.Second), "id"), wantCode: connect.CodeDeadlineExceeded, wantStable: "negotiation_already_expired"},
		{name: "descriptor bounds", response: validNegotiationResponse(expected, now.Add(time.Minute), "id"), wantCode: connect.CodeFailedPrecondition, wantStable: "protocol_descriptor_bounds_invalid"},
		{name: "descriptor extra capability", response: validNegotiationResponse(expected, now.Add(time.Minute), "id"), wantCode: connect.CodeFailedPrecondition, wantStable: "protocol_descriptor_capabilities_invalid"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := test.response
			if response != nil {
				switch test.name {
				case "version":
					response.Msg.SelectedVersion = &workerv1alpha1.ProtocolVersion{Major: 2, Minor: 0}
				case "capability missing":
					response.Msg.AcceptedCapabilities = []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION}
				case "identity":
					response.Msg.AuthenticatedServerIdentity = &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://wrong/worker", TrustDomain: "cloud-agents.test"}
				case "descriptor bounds":
					response.Msg.Server.MaxWireMessageBytes = workerkernel.MaxWireMessageBytes + 1
				case "descriptor extra capability":
					response.Msg.Server.Capabilities = append(response.Msg.Server.Capabilities, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH)
				}
			}
			fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
				return response, nil
			}}
			supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = supervisor.Bind(context.Background())
			if connect.CodeOf(err) != test.wantCode || !strings.Contains(err.Error(), test.wantStable) {
				t.Fatalf("error = %v, code = %v", err, connect.CodeOf(err))
			}
			if _, ok := supervisor.CurrentBinding(); ok {
				t.Fatal("malformed response committed a binding")
			}
		})
	}
}

func TestCheckHealthUsesExactBindingAndRejectsDrift(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	negotiation := validNegotiationResponse(expected, now.Add(time.Minute), "binding-health")
	fake := &fakeWorkerClient{}
	fake.negotiateFn = func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return negotiation, nil
	}
	fake.healthFn = func(_ context.Context, request *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
		if request.Msg.GetNegotiation().GetNegotiationId() != "binding-health" || !request.Msg.GetNegotiation().GetExpiresAt().AsTime().Equal(now.Add(time.Minute)) {
			t.Fatalf("binding = %#v", request.Msg.GetNegotiation())
		}
		if !exactCapabilities(request.Msg.GetRequiredCapabilities(), requiredCapabilities()) || !sameIdentity(request.Msg.GetExpectedServerIdentity(), expected) {
			t.Fatalf("health request = %#v", request.Msg)
		}
		return connect.NewResponse(&workerv1alpha1.HealthResponse{
			State:      workerv1alpha1.HealthState_HEALTH_STATE_SERVING,
			Protocol:   cloneDescriptor(negotiation.Msg.GetServer()),
			ObservedAt: timestamppb.New(now.Add(10 * time.Second)),
		}), nil
	}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	health, err := supervisor.CheckHealth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.State != workerv1alpha1.HealthState_HEALTH_STATE_SERVING || !health.ObservedAt.Equal(now.Add(10*time.Second)) {
		t.Fatalf("health = %#v", health)
	}

	fake.healthFn = func(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
		drift := cloneDescriptor(negotiation.Msg.GetServer())
		drift.MaxStringBytes--
		return connect.NewResponse(&workerv1alpha1.HealthResponse{State: workerv1alpha1.HealthState_HEALTH_STATE_SERVING, Protocol: drift, ObservedAt: timestamppb.New(now)}), nil
	}
	_, err = supervisor.CheckHealth(context.Background())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "protocol_descriptor_drift") {
		t.Fatalf("descriptor drift error = %v", err)
	}
}

func TestCheckHealthExpiryPreventsRPC(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return connect.NewResponse(validNegotiationResponse(expected, now.Add(time.Second), "binding-expiry").Msg), nil
	}, healthFn: func(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
		return nil, errors.New("health must not be called after expiry")
	}}
	clock := now
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	_, err = supervisor.CheckHealth(context.Background())
	if connect.CodeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "binding_expired") || fake.healthCalls != 0 {
		t.Fatalf("expiry = %v, health calls = %d", err, fake.healthCalls)
	}
}

func TestCheckHealthRechecksExpiryAfterRPC(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	clock := now
	negotiation := validNegotiationResponse(expected, now.Add(time.Second), "binding-health-expiry")
	fake := &fakeWorkerClient{
		negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
			return negotiation, nil
		},
		healthFn: func(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
			clock = now.Add(2 * time.Second)
			return connect.NewResponse(&workerv1alpha1.HealthResponse{
				State:      workerv1alpha1.HealthState_HEALTH_STATE_SERVING,
				Protocol:   testDescriptor(),
				ObservedAt: timestamppb.New(now),
			}), nil
		},
	}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.CheckHealth(context.Background()); connect.CodeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "binding_expired") {
		t.Fatalf("post-RPC expiry error = %v", err)
	}
	if fake.healthCalls != 1 {
		t.Fatalf("health calls = %d", fake.healthCalls)
	}
}

func TestCheckHealthDoesNotReturnSuccessAfterContextCancellation(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	negotiation := validNegotiationResponse(expected, now.Add(time.Minute), "binding-health-canceled")
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeWorkerClient{
		negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
			return negotiation, nil
		},
		healthFn: func(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
			cancel()
			return connect.NewResponse(&workerv1alpha1.HealthResponse{State: workerv1alpha1.HealthState_HEALTH_STATE_SERVING, Protocol: testDescriptor(), ObservedAt: timestamppb.New(now)}), nil
		},
	}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.CheckHealth(ctx); connect.CodeOf(err) != connect.CodeCanceled {
		t.Fatalf("canceled health error = %v", err)
	}
}

func TestCurrentBindingHidesExpiredState(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 0, 0, 0, time.UTC)
	expected := testWorkerIdentity()
	fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return validNegotiationResponse(expected, now.Add(time.Second), "binding-current-expiry"), nil
	}}
	clock := now
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: expected, Clock: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.Bind(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Second)
	if _, ok := supervisor.CurrentBinding(); ok {
		t.Fatal("expired binding remained observable")
	}
	if _, ok := supervisor.CurrentBinding(); ok {
		t.Fatal("expired binding was not cleared")
	}
}

func TestNewRejectsTypedNilClient(t *testing.T) {
	var client *fakeWorkerClient
	if supervisor, err := New(Config{Client: client, ExpectedWorkerIdentity: testWorkerIdentity()}); supervisor != nil || !errors.Is(err, errInvalidConfig) {
		t.Fatalf("typed nil client result = %#v / %v", supervisor, err)
	}
}

func TestNoOpOperationsAndContextFailBeforeClient(t *testing.T) {
	fake := &fakeWorkerClient{}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: testWorkerIdentity()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = supervisor.DispatchOperation(context.Background(), &workerv1alpha1.OperationAttemptEnvelope{})
	if connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "operation_dispatch_not_implemented") || fake.executeCalls != 0 {
		t.Fatalf("dispatch = %v / calls = %d", err, fake.executeCalls)
	}
	_, err = supervisor.GetOperationReceipt(context.Background(), &workerv1alpha1.ReceiptRequest{})
	if connect.CodeOf(err) != connect.CodeUnimplemented || !strings.Contains(err.Error(), "durable_receipts_not_implemented") || fake.receiptCalls != 0 {
		t.Fatalf("receipt = %v / calls = %d", err, fake.receiptCalls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = supervisor.Bind(ctx)
	if connect.CodeOf(err) != connect.CodeCanceled || fake.negotiateCalls != 0 {
		t.Fatalf("cancelled bind = %v / calls = %d", err, fake.negotiateCalls)
	}
}

func TestRPCFailureIsRedactedAndPreservesCode(t *testing.T) {
	fake := &fakeWorkerClient{negotiateFn: func(context.Context, *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("secret worker endpoint"))
	}}
	supervisor, err := New(Config{Client: fake, ExpectedWorkerIdentity: testWorkerIdentity()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = supervisor.Bind(context.Background())
	if connect.CodeOf(err) != connect.CodeUnavailable || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "negotiate_rpc_failed") {
		t.Fatalf("redacted RPC error = %v, code = %v", err, connect.CodeOf(err))
	}
}

func testWorkerIdentity() *workerv1alpha1.WorkloadIdentity {
	return &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker/test", TrustDomain: "cloud-agents.test", LeafCertificateSha256: []byte("01234567890123456789012345678901")}
}

func validNegotiationResponse(identity *workerv1alpha1.WorkloadIdentity, expires time.Time, id string) *connect.Response[workerv1alpha1.NegotiationResponse] {
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{
		SelectedVersion:             &workerv1alpha1.ProtocolVersion{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor},
		AcceptedCapabilities:        requiredCapabilities(),
		Server:                      testDescriptor(),
		AuthenticatedServerIdentity: cloneIdentity(identity),
		NegotiationId:               id,
		ExpiresAt:                   timestamppb.New(expires),
	})
}

func testDescriptor() *workerv1alpha1.ProtocolDescriptor {
	return &workerv1alpha1.ProtocolDescriptor{
		CurrentVersion:           &workerv1alpha1.ProtocolVersion{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor},
		MinimumCompatibleVersion: &workerv1alpha1.ProtocolVersion{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor},
		Capabilities:             requiredCapabilities(),
		MaxPayloadBytes:          workerkernel.MaxPayloadBytes,
		MaxDeadlineSeconds:       workerkernel.MaxDeadlineSeconds,
		MaxWireMessageBytes:      workerkernel.MaxWireMessageBytes,
		MaxRepeatedItems:         workerkernel.MaxRepeatedItems,
		MaxStringBytes:           workerkernel.MaxStringBytes,
	}
}

func descriptorWithCapabilities(capabilities []workerv1alpha1.Capability) *workerv1alpha1.ProtocolDescriptor {
	descriptor := testDescriptor()
	descriptor.Capabilities = append([]workerv1alpha1.Capability(nil), capabilities...)
	return descriptor
}
