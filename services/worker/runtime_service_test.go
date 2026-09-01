package worker

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		RuntimeCommand:      []string{os.Args[0]},
		AdmissionLeaseID:    "lease-runtime",
		AdmissionGeneration: 7,
		AdmissionToken:      []byte("expected-token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	open := &workerruntimev1alpha1.RuntimeSessionOpen{ExecutionId: "execution", Generation: 7, Fencing: &workerv1alpha1.FencingProof{LeaseId: "lease-runtime", Generation: 7, Token: []byte("expected-token")}}
	if err := service.validateRuntimeFencing(open.GetFencing(), open.GetGeneration()); err != nil {
		t.Fatalf("matching Runtime token = %v", err)
	}
	open.Fencing.Token = []byte("wrong-token")
	if err := service.validateRuntimeFencing(open.GetFencing(), open.GetGeneration()); connect.CodeOf(err) != connect.CodePermissionDenied || !strings.Contains(err.Error(), "fencing_token_mismatch") {
		t.Fatalf("wrong Runtime token = %v", err)
	}
}

func TestRuntimeArtifactReadIsBoundedToCandidateRoot(t *testing.T) {
	workerIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"}
	supervisorIdentity := &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/supervisor", TrustDomain: "cloud-agents.test"}
	runtimeRoot := t.TempDir()
	root := filepath.Join(runtimeRoot, "tenants", "tenant-alpha", "sessions", "session-alpha", "workspace")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Config{
		WorkerIdentity: workerIdentity, Capabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION},
		IdentityProvider: StaticIdentityProvider{Identity: supervisorIdentity}, AdmissionLeaseID: "lease-artifact", AdmissionGeneration: 7,
		AdmissionToken: []byte("artifact-token"), NegotiationTTL: time.Minute,
		RuntimeCommand: []string{os.Args[0]}, RuntimeDirectory: runtimeRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	workerPath, workerHandler := NewHandler(service)
	runtimePath, runtimeHandler := NewRuntimeHandler(service)
	mux.Handle(workerPath, workerHandler)
	mux.Handle(runtimePath, runtimeHandler)
	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	workerClient := workerv1alpha1connect.NewWorkerExecutionServiceClient(server.Client(), server.URL)
	runtimeClient := workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(server.Client(), server.URL)
	negotiation, err := workerClient.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: ProtocolMajor, Minor: ProtocolMinor}}, RequiredCapabilities: []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION}, ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte("artifact bytes")
	if err := os.WriteFile(filepath.Join(root, "result.txt"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	size := uint64(len(contents))
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	request := func(relativePath string) *workerruntimev1alpha1.RuntimeArtifactReadRequest {
		return &workerruntimev1alpha1.RuntimeArtifactReadRequest{
			Negotiation: &workerv1alpha1.NegotiationBinding{ProtocolVersion: negotiation.Msg.GetSelectedVersion(), NegotiationId: negotiation.Msg.GetNegotiationId(), ExpiresAt: negotiation.Msg.GetExpiresAt()},
			Fencing:     &workerv1alpha1.FencingProof{LeaseId: "lease-artifact", Generation: 7, Token: []byte("artifact-token")}, ExecutionId: "execution-artifact", Generation: 7,
			ExpectedWorkerIdentity: workerIdentity, RootDirectory: root, RelativePath: relativePath, ExpectedSizeBytes: &size, ExpectedSha256: digest,
		}
	}
	stream, err := runtimeClient.ReadArtifact(context.Background(), connect.NewRequest(request("result.txt")))
	if err != nil || !stream.Receive() || string(stream.Msg().GetData()) != string(contents) || stream.Msg().GetSizeBytes() != size || stream.Receive() || stream.Err() != nil {
		t.Fatalf("artifact stream=%v err=%v streamErr=%v", stream, err, stream.Err())
	}
	outsideRoot := t.TempDir()
	outside := filepath.Join(outsideRoot, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideRoot, filepath.Join(runtimeRoot, "escaped-root")); err != nil {
		t.Fatal(err)
	}
	outsideRequest := request("outside.txt")
	outsideRequest.RootDirectory = outsideRoot
	escapedRootRequest := request("outside.txt")
	escapedRootRequest.RootDirectory = filepath.Join(runtimeRoot, "escaped-root")
	for name, unsafeRequest := range map[string]*workerruntimev1alpha1.RuntimeArtifactReadRequest{
		"parent traversal": request("../outside.txt"),
		"file symlink":     request("escape"),
		"outside root":     outsideRequest,
		"root symlink":     escapedRootRequest,
	} {
		rejected, callErr := runtimeClient.ReadArtifact(context.Background(), connect.NewRequest(unsafeRequest))
		if callErr == nil {
			for rejected.Receive() {
			}
			callErr = rejected.Err()
		}
		if callErr == nil {
			t.Fatalf("unsafe Artifact %q was accepted", name)
		}
	}
}

func TestRuntimeSessionCapacityConfigDefaultsAndRejectsInvalidBounds(t *testing.T) {
	base := Config{
		WorkerIdentity:      &workerv1alpha1.WorkloadIdentity{SpiffeId: "spiffe://cloud-agents.test/worker", TrustDomain: "cloud-agents.test"},
		RuntimeCommand:      []string{os.Args[0]},
		AdmissionToken:      []byte("runtime-token"),
		AdmissionLeaseID:    "lease-runtime",
		AdmissionGeneration: 7,
	}
	service, err := NewService(base)
	if err != nil {
		t.Fatal(err)
	}
	if cap(service.runtimeSlots) != DefaultRuntimeMaxSessions {
		t.Fatalf("default Runtime capacity = %d", cap(service.runtimeSlots))
	}
	for _, limit := range []int{-1, MaxRuntimeSessions + 1} {
		candidate := base
		candidate.RuntimeMaxSessions = limit
		if _, err := NewService(candidate); err == nil {
			t.Fatalf("Runtime max sessions %d unexpectedly accepted", limit)
		}
	}
	if _, err := NewService(Config{WorkerIdentity: base.WorkerIdentity, RuntimeMaxSessions: 1}); err == nil {
		t.Fatal("Runtime capacity without a Runtime command unexpectedly accepted")
	}
	unavailable := base
	unavailable.RuntimeCommand = []string{filepath.Join(t.TempDir(), "missing-runtime")}
	if _, err := NewService(unavailable); err == nil {
		t.Fatal("unavailable Runtime command unexpectedly accepted")
	}
	missingDirectory := base
	missingDirectory.RuntimeCredentialDirectory = filepath.Join(t.TempDir(), "missing-credentials")
	if _, err := NewService(missingDirectory); err == nil {
		t.Fatal("unavailable Runtime credential directory unexpectedly accepted")
	}
	missingDirectory.RuntimeCredentialDirectory = ""
	missingDirectory.RuntimeDirectory = filepath.Join(t.TempDir(), "missing-workspace")
	if _, err := NewService(missingDirectory); err == nil {
		t.Fatal("unavailable Runtime directory unexpectedly accepted")
	}
	credentialFile := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(credentialFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	notDirectory := base
	notDirectory.RuntimeCredentialDirectory = credentialFile
	if _, err := NewService(notDirectory); err == nil {
		t.Fatal("Runtime credential file unexpectedly accepted as a directory")
	}
	withoutCommand := Config{WorkerIdentity: base.WorkerIdentity, RuntimeCredentialDirectory: t.TempDir()}
	if _, err := NewService(withoutCommand); err == nil {
		t.Fatal("Runtime credential directory without a Runtime command unexpectedly accepted")
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
