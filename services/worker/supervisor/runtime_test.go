package supervisor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	workerruntimev1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestRuntimeSessionBridgesWorkerAndRuntimeProcess(t *testing.T) {
	if os.Getenv("CLOUD_AGENTS_RUNTIME_BRIDGE_HELPER") == "1" {
		runRuntimeBridgeHelper()
		return
	}
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor", TrustDomain: "cloud-agents.test"}
	now := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	credentialDirectory := t.TempDir()
	if err := os.WriteFile(credentialDirectory+"/codex.json", []byte(`{"payload":{"apiKey":"test-key"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerIdentity,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		IdentityProvider:           workerkernel.StaticIdentityProvider{Identity: supervisorIdentity},
		AdmissionLeaseID:           "lease-runtime-test",
		AdmissionGeneration:        7,
		AdmissionToken:             []byte("runtime-token"),
		RuntimeCommand:             []string{os.Args[0], "-test.run=TestRuntimeSessionBridgesWorkerAndRuntimeProcess", "--"},
		RuntimeEnvironment:         append(os.Environ(), "CLOUD_AGENTS_RUNTIME_BRIDGE_HELPER=1"),
		RuntimeCredentialDirectory: credentialDirectory,
		NegotiationTTL:             time.Minute,
		Clock:                      func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	workerPath, workerHandler := workerkernel.NewHandler(service)
	runtimePath, runtimeHandler := workerkernel.NewRuntimeHandler(service)
	mux := http.NewServeMux()
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	workerClient := workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL)
	runtimeClient := workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL)
	supervisor, err := New(Config{Client: workerClient, RuntimeClient: runtimeClient, ExpectedWorkerIdentity: workerIdentity, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := supervisor.OpenRuntimeSession(context.Background(), "execution-runtime-test", "codex", 7, &workerv1alpha1.FencingProof{LeaseId: "lease-runtime-test", Generation: 7, Token: []byte("runtime-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stream.CloseRequest()
		_ = stream.CloseResponse()
	}()

	start := runtimeCommand("StartSession", "start-runtime-test", "execution-runtime-test", 7)
	if err := stream.Send(context.Background(), start); err != nil {
		t.Fatal(err)
	}
	if message, err := stream.Receive(); err != nil || message.MessageType != "Result" {
		t.Fatalf("StartSession = %#v, %v", message, err)
	}

	turn := runtimeCommand("SendTurn", "turn-runtime-test", "execution-runtime-test", 7)
	if err := stream.Send(context.Background(), turn); err != nil {
		t.Fatal(err)
	}
	seenEvent, seenResult := false, false
	for !(seenEvent && seenResult) {
		message, receiveErr := stream.Receive()
		if receiveErr != nil {
			t.Fatalf("SendTurn receive = %#v, %v", message, receiveErr)
		}
		switch message.MessageType {
		case "Event":
			seenEvent = true
		case "Result":
			seenResult = true
		default:
			t.Fatalf("unexpected Runtime message = %#v", message)
		}
	}
	firstBinding, ok := supervisor.CurrentBinding()
	if !ok {
		t.Fatal("automatic Runtime binding is not current")
	}
	if err := stream.CloseRequest(); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseResponse(); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	secondStream, err := supervisor.OpenRuntimeSession(context.Background(), "execution-runtime-test-2", "codex", 7, &workerv1alpha1.FencingProof{LeaseId: "lease-runtime-test", Generation: 7, Token: []byte("runtime-token")})
	if err != nil {
		t.Fatalf("OpenRuntimeSession after binding expiry = %v", err)
	}
	defer func() {
		_ = secondStream.CloseRequest()
		_ = secondStream.CloseResponse()
	}()
	secondBinding, ok := supervisor.CurrentBinding()
	if !ok || secondBinding.NegotiationID == firstBinding.NegotiationID {
		t.Fatalf("expired Runtime binding was not replaced: first=%#v second=%#v", firstBinding, secondBinding)
	}
	if err := supervisor.CheckRuntimeHealth(context.Background()); err != nil {
		t.Fatalf("Runtime health check = %v", err)
	}
	mismatchStream, err := supervisor.OpenRuntimeSession(context.Background(), "execution-runtime-test-3", "codex", 7, &workerv1alpha1.FencingProof{LeaseId: "lease-runtime-test", Generation: 7, Token: []byte("runtime-token")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mismatchStream.CloseRequest(); _ = mismatchStream.CloseResponse() }()
	mismatch := runtimeCommand("StartSession", "start-runtime-mismatch", "execution-runtime-test-3", 7)
	mismatch.Payload["runnerInput"].(map[string]any)["workload"].(map[string]any)["provider"] = "claudeAgent"
	if err := mismatchStream.Send(context.Background(), mismatch); err != nil {
		t.Fatal(err)
	}
	if _, err := mismatchStream.Receive(); err == nil || !strings.Contains(err.Error(), "provider_mismatch") {
		t.Fatalf("cross-Provider Runtime command error = %v", err)
	}
}

func TestOpenRuntimeSessionRequiresRuntimeBindingAndFencing(t *testing.T) {
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	client := &fakeWorkerClient{}
	supervisor, err := New(Config{Client: client, RuntimeClient: nil, ExpectedWorkerIdentity: workerIdentity})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.OpenRuntimeSession(context.Background(), "execution", "codex", 1, &workerv1alpha1.FencingProof{LeaseId: "lease", Generation: 1, Token: []byte("token")}); err == nil {
		t.Fatal("OpenRuntimeSession without Runtime client unexpectedly succeeded")
	}
}

func runRuntimeBridgeHelper() {
	credentialFD, err := strconv.Atoi(os.Getenv("CLOUD_AGENT_PROVIDER_CREDENTIAL_FD"))
	if err != nil {
		os.Exit(90)
	}
	credential, err := io.ReadAll(os.NewFile(uintptr(credentialFD), "credential"))
	if err != nil || string(credential) != `{"payload":{"apiKey":"test-key"}}` {
		os.Exit(91)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command runtimeprotocol.Command
		if json.Unmarshal(scanner.Bytes(), &command) != nil {
			continue
		}
		if command.CommandType == "SendTurn" {
			_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T00:00:00Z","messageType":"Event","payload":{"text":"partial"}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID)
		}
		_, _ = fmt.Fprintf(os.Stdout, `{"requestId":%q,"protocolVersion":{"major":2,"minor":3},"executionId":%q,"generation":%d,"commandId":%q,"occurredAt":"2026-08-29T00:00:00Z","messageType":"Result","payload":{"ok":true}}
`, command.RequestID, command.ExecutionID, command.Generation, command.CommandID)
	}
	os.Exit(0)
}

func runtimeCommand(commandType, commandID, executionID string, generation uint64) runtimeprotocol.Command {
	payload := map[string]any{}
	if commandType == "StartSession" || commandType == "ResumeSession" {
		payload["runnerInput"] = map[string]any{"workload": map[string]any{"provider": "codex"}}
	}
	return runtimeprotocol.Command{RequestID: "request-" + commandID, Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: executionID, Generation: generation, CommandType: commandType, CommandID: commandID, OccurredAt: "2026-08-29T00:00:00Z", Payload: payload}
}
