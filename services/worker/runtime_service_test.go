package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func TestRuntimeInvalidCommandHelperProcess(t *testing.T) {
	if os.Getenv("CLOUD_AGENTS_WORKER_RUNTIME_HELPER") != "1" {
		return
	}
	for scanner := bufio.NewScanner(os.Stdin); scanner.Scan(); {
		_, _ = fmt.Fprintln(os.Stdout, scanner.Text())
	}
	os.Exit(0)
}

func TestRuntimeFencingRequiresConfiguredToken(t *testing.T) {
	service, err := NewService(Config{
		WorkerIdentity:      &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		RuntimeCommand:      []string{"runtime"},
		AdmissionLeaseID:    "lease-runtime",
		AdmissionGeneration: 7,
		AdmissionToken:      []byte("expected-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	open := &workerruntimev1alpha1.RuntimeSessionOpen{ExecutionId: "execution", Generation: 7, Fencing: &workerv1alpha1.FencingProof{LeaseId: "lease-runtime", Generation: 7, Token: []byte("expected-token")}}
	if err := service.validateRuntimeFencing(open); err != nil {
		t.Fatalf("matching Runtime token = %v", err)
	}
	open.Fencing.Token = []byte("wrong-token")
	if err := service.validateRuntimeFencing(open); connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "fencing_token_mismatch") {
		t.Fatalf("wrong Runtime token = %v", err)
	}
}

func TestRuntimeSessionRejectsInvalidCommandAtWorkerBoundary(t *testing.T) {
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor", TrustDomain: "cloud-agents.test"}
	service, err := NewService(Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
		},
		IdentityProvider:    StaticIdentityProvider{Identity: supervisorIdentity},
		AdmissionLeaseID:    "lease-runtime-invalid-command",
		AdmissionGeneration: 7,
		AdmissionToken:      []byte("runtime-token"),
		RuntimeCommand:      []string{os.Args[0], "-test.run=TestRuntimeInvalidCommandHelperProcess", "--"},
		RuntimeEnvironment:  append(os.Environ(), "CLOUD_AGENTS_WORKER_RUNTIME_HELPER=1"),
		NegotiationTTL:      time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	workerPath, workerHandler := NewHandler(service)
	runtimePath, runtimeHandler := NewRuntimeHandler(service)
	mux := http.NewServeMux()
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	workerClient := workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL)
	runtimeClient := workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL)
	incomplete, err := workerClient.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: ProtocolMajor, Minor: ProtocolMinor}},
		RequiredCapabilities:   []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION},
		ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	rejected := runtimeClient.OpenSession(context.Background())
	if err := rejected.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Open{Open: &workerruntimev1alpha1.RuntimeSessionOpen{
		Negotiation: &workerv1alpha1.NegotiationBinding{ProtocolVersion: incomplete.Msg.GetSelectedVersion(), NegotiationId: incomplete.Msg.GetNegotiationId(), ExpiresAt: incomplete.Msg.GetExpiresAt()},
		Fencing:     &workerv1alpha1.FencingProof{LeaseId: "lease-runtime-invalid-command", Generation: 7, Token: []byte("runtime-token")},
		ExecutionId: "execution-incomplete-binding", Generation: 7, ExpectedWorkerIdentity: workerIdentity, ProviderKind: "codex",
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := rejected.Receive(); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "capability_not_negotiated") {
		t.Fatalf("incomplete Runtime binding error = %v", err)
	}
	_ = rejected.CloseRequest()
	_ = rejected.CloseResponse()

	negotiation, err := workerClient.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: ProtocolMajor, Minor: ProtocolMinor}},
		RequiredCapabilities:   []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH},
		ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	stream := runtimeClient.OpenSession(context.Background())
	defer func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() }()
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Open{Open: &workerruntimev1alpha1.RuntimeSessionOpen{
		Negotiation: &workerv1alpha1.NegotiationBinding{ProtocolVersion: negotiation.Msg.GetSelectedVersion(), NegotiationId: negotiation.Msg.GetNegotiationId(), ExpiresAt: negotiation.Msg.GetExpiresAt()},
		Fencing:     &workerv1alpha1.FencingProof{LeaseId: "lease-runtime-invalid-command", Generation: 7, Token: []byte("runtime-token")},
		ExecutionId: "execution-invalid-command", Generation: 7, ExpectedWorkerIdentity: workerIdentity, ProviderKind: "codex",
	}}}); err != nil {
		t.Fatal(err)
	}
	if ready, err := stream.Receive(); err != nil || ready.GetReady() == nil {
		t.Fatalf("Runtime ready = %#v, %v", ready, err)
	}
	commandBytes, err := json.Marshal(runtimeprotocol.Command{RequestID: "request-invalid-command", Protocol: runtimeprotocol.Protocol{Major: runtimeprotocol.ProtocolMajor, Minor: runtimeprotocol.ProtocolMinor}, ExecutionID: "execution-invalid-command", Generation: 7, CommandType: "Unsupported", CommandID: "command-invalid", OccurredAt: "2026-08-29T00:00:00Z", Payload: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Command{Command: &workerruntimev1alpha1.RuntimeCommandFrame{Json: commandBytes}}}); err != nil {
		t.Fatal(err)
	}
	response, err := stream.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if runtimeError := response.GetError(); runtimeError == nil || runtimeError.GetCode() != "command_invalid" {
		t.Fatalf("invalid Runtime response = %#v", response)
	}
}

func TestRuntimeCommandProviderReadsOnlyProviderBindingCommands(t *testing.T) {
	start := runtimeprotocol.Command{CommandType: "StartSession", Payload: map[string]any{"runnerInput": map[string]any{"workload": map[string]any{"provider": "codex"}}}}
	if providerKind, binds := runtimeCommandProvider(start); !binds || providerKind != "codex" {
		t.Fatalf("StartSession provider = %q, binds = %t", providerKind, binds)
	}
	if providerKind, binds := runtimeCommandProvider(runtimeprotocol.Command{CommandType: "SendTurn", Payload: map[string]any{"provider": "claudeAgent"}}); binds || providerKind != "" {
		t.Fatalf("SendTurn provider = %q, binds = %t", providerKind, binds)
	}
}

func TestRuntimeProviderCredentialFileRejectsInvalidOrMissingBinding(t *testing.T) {
	for _, providerKind := range []string{"", "../codex", "codex.json", strings.Repeat("a", 65)} {
		if validRuntimeProviderKind(providerKind) {
			t.Fatalf("provider kind %q unexpectedly valid", providerKind)
		}
	}
	if _, err := runtimeProviderCredentialFile(t.TempDir(), "codex"); connect.CodeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "provider_credential_unavailable") {
		t.Fatalf("missing Provider credential error = %v", err)
	}
}
