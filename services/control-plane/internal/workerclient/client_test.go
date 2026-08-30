package workerclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workerWireFake struct {
	workerv1alpha1connect.UnimplementedWorkerExecutionServiceHandler
	workerruntimev1alpha1connect.UnimplementedWorkerRuntimeServiceHandler
	identity     *workerv1alpha1.WorkloadIdentity
	capabilities []workerv1alpha1.Capability
	now          time.Time
}

func (fake *workerWireFake) Negotiate(_ context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
	required := []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
	}
	if !exactCapabilities(request.Msg.GetRequiredCapabilities(), required) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errInvalidConfig)
	}
	descriptor := workerDescriptor(fake.capabilities)
	return connect.NewResponse(&workerv1alpha1.NegotiationResponse{
		SelectedVersion: request.Msg.GetSupportedVersions()[0], AcceptedCapabilities: required, Server: descriptor,
		AuthenticatedServerIdentity: proto.Clone(fake.identity).(*workerv1alpha1.WorkloadIdentity), NegotiationId: "negotiation-1", ExpiresAt: timestamppb.New(fake.now.Add(time.Minute)),
	}), nil
}

func (fake *workerWireFake) CheckHealth(context.Context, *connect.Request[workerv1alpha1.HealthRequest]) (*connect.Response[workerv1alpha1.HealthResponse], error) {
	return connect.NewResponse(&workerv1alpha1.HealthResponse{State: workerv1alpha1.HealthState_HEALTH_STATE_SERVING, Protocol: workerDescriptor(fake.capabilities), ObservedAt: timestamppb.New(fake.now)}), nil
}

func (fake *workerWireFake) OpenSession(_ context.Context, stream *connect.BidiStream[workerruntimev1alpha1.RuntimeSessionRequest, workerruntimev1alpha1.RuntimeSessionResponse]) error {
	open, err := stream.Receive()
	if err != nil {
		return err
	}
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Ready{Ready: &workerruntimev1alpha1.RuntimeSessionReady{ExecutionId: open.GetOpen().GetExecutionId(), Generation: open.GetOpen().GetGeneration(), ProtocolMajor: runtimeprotocol.ProtocolMajor, ProtocolMinor: runtimeprotocol.ProtocolMinor}}}); err != nil {
		return err
	}
	request, err := stream.Receive()
	if err != nil {
		return err
	}
	var command runtimeprotocol.Command
	if err := json.Unmarshal(request.GetCommand().GetJson(), &command); err != nil {
		return err
	}
	message, _ := json.Marshal(runtimeprotocol.Message{RequestID: command.RequestID, Protocol: command.Protocol, ExecutionID: command.ExecutionID, Generation: command.Generation, CommandID: command.CommandID, OccurredAt: command.OccurredAt, MessageType: "Result", Payload: map[string]any{"ok": true}})
	return stream.Send(&workerruntimev1alpha1.RuntimeSessionResponse{Frame: &workerruntimev1alpha1.RuntimeSessionResponse_Json{Json: message}})
}

func TestSupervisorUsesGeneratedWorkerWire(t *testing.T) {
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	fake := &workerWireFake{identity: identity, capabilities: []workerv1alpha1.Capability{
		workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
		workerv1alpha1.Capability_CAPABILITY_HEALTH,
		workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
	}, now: now}
	mux := http.NewServeMux()
	workerPath, workerHandler := workerv1alpha1connect.NewWorkerExecutionServiceHandler(fake)
	runtimePath, runtimeHandler := workerruntimev1alpha1connect.NewWorkerRuntimeServiceHandler(fake)
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	supervisor, err := New(Config{
		Client: workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL), RuntimeClient: workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL), ExpectedWorkerIdentity: identity, Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.BindRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.CheckRuntimeHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.CheckRuntimeHealth(nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("nil health context error = %v", err)
	}
	session, err := supervisor.OpenRuntimeSession(context.Background(), "execution-1", "codex", 7, &workerv1alpha1.FencingProof{LeaseId: "lease-1", Generation: 7, Token: []byte("token")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.CloseResponse()
	command := runtimeprotocol.Command{RequestID: "request-1", Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: "execution-1", Generation: 7, CommandType: "SendTurn", CommandID: "command-1", OccurredAt: now.Format(time.RFC3339), Payload: map[string]any{"inputText": "hello"}}
	if err := session.Send(context.Background(), command); err != nil {
		t.Fatal(err)
	}
	message, err := session.Receive()
	if err != nil || message.MessageType != "Result" || message.CommandID != command.CommandID {
		t.Fatalf("message = %#v, err = %v", message, err)
	}
	if err := session.CloseRequest(); err != nil {
		t.Fatal(err)
	}
}

func TestMTLSConfigurationFailsClosed(t *testing.T) {
	identity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	certificate := tls.Certificate{Certificate: [][]byte{{1}}, PrivateKey: struct{}{}}
	if _, err := NewMTLS(MTLSConfig{Endpoint: "http://worker.test:8091", ExpectedWorkerIdentity: identity, ClientCertificate: certificate, RootCAs: x509.NewCertPool()}); err == nil {
		t.Fatal("cleartext Worker endpoint accepted")
	}
	if _, err := NewMTLS(MTLSConfig{Endpoint: "https://worker.test:8091", ExpectedWorkerIdentity: identity, ClientCertificate: certificate, RootCAs: x509.NewCertPool()}); err != nil {
		t.Fatal(err)
	}
	spiffe, _ := url.Parse("spiffe://cloud-agents.test/worker")
	actual, err := peerIdentity(&x509.Certificate{URIs: []*url.URL{spiffe}, Raw: []byte("certificate")})
	if err != nil || actual.GetSpiffeId() != identity.GetSpiffeId() || len(actual.GetLeafCertificateSha256()) != 32 {
		t.Fatalf("identity = %#v, err = %v", actual, err)
	}
}

func workerDescriptor(capabilities []workerv1alpha1.Capability) *workerv1alpha1.ProtocolDescriptor {
	return &workerv1alpha1.ProtocolDescriptor{CurrentVersion: &workerv1alpha1.ProtocolVersion{Major: workerProtocolMajor, Minor: workerProtocolMinor}, MinimumCompatibleVersion: &workerv1alpha1.ProtocolVersion{Major: workerProtocolMajor, Minor: workerProtocolMinor}, Capabilities: capabilities, MaxPayloadBytes: workerMaxPayloadBytes, MaxDeadlineSeconds: workerMaxDeadlineSeconds, MaxWireMessageBytes: workerMaxWireMessageBytes, MaxRepeatedItems: workerMaxRepeatedItems, MaxStringBytes: workerMaxStringBytes}
}
