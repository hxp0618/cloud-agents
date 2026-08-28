package supervisor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestValidateLocalLauncherEndpoint(t *testing.T) {
	valid := []string{"http://127.0.0.1:8091", "http://[::1]:8091", "http://127.0.0.1:8091/"}
	for _, raw := range valid {
		if got, err := validateLocalLauncherEndpoint(raw); err != nil || got == "" {
			t.Fatalf("valid endpoint %q: got=%q err=%v", raw, got, err)
		}
	}
	invalid := []string{
		"", "https://127.0.0.1:8091", "http://localhost:8091", "http://0.0.0.0:8091",
		"http://192.168.31.234:8091", "http://127.0.0.1:0", "http://127.0.0.1:65536",
		"http://127.0.0.1:not-a-port", "http://user@127.0.0.1:8091", "http://127.0.0.1:8091/path",
		"http://127.0.0.1:8091/?x=1", "http://127.0.0.1:8091/#frag", "http://[::1%25lo0]:8091",
	}
	for _, raw := range invalid {
		if got, err := validateLocalLauncherEndpoint(raw); got != "" || err != errLocalLauncherURL {
			t.Fatalf("invalid endpoint %q: got=%q err=%v", raw, got, err)
		}
	}
}

func TestReadLocalLauncherTokenAcceptsLauncherTrailingLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("token-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readLocalLauncherToken(path)
	if err != nil || string(got) != "token-value" {
		t.Fatalf("token got=%q err=%v", got, err)
	}
	for _, body := range []string{"token-value\n\n", " token-value\n", "token value\n", "token-value\r\n"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readLocalLauncherToken(path); err != errLocalLauncherToken {
			t.Fatalf("body %q accepted: %v", body, err)
		}
	}
	if err := os.WriteFile(path, []byte("token-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalLauncherToken(path); err != errLocalLauncherToken {
		t.Fatalf("token without trailing LF accepted: %v", err)
	}
}

func TestReadLocalLauncherTokenRejectsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(target, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalLauncherToken(path); err != errLocalLauncherToken {
		t.Fatalf("symlink accepted: %v", err)
	}
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte("token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalLauncherToken(path); err != errLocalLauncherToken {
		t.Fatalf("mode accepted: %v", err)
	}
}

func TestLocalLauncherRouteRejectsForeignOutboundRequest(t *testing.T) {
	rt := &localLauncherRoundTripper{base: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil }), endpoint: "http://127.0.0.1:8091", token: []byte("token")}
	for _, raw := range []string{"http://127.0.0.1:8091/foreign", "http://127.0.0.2:8091/healthz", "http://127.0.0.1:8091/healthz?x=1", "http://127.0.0.1:8091/cloudagents.worker.v1alpha1.WorkerExecutionService/ExecuteOperation"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := rt.RoundTrip(req); err != errLocalLauncherURL {
			t.Fatalf("request %q accepted: %v", raw, err)
		}
	}
}

func TestGeneratedLocalLauncherProfileValidatesAgainstFrozenConstants(t *testing.T) {
	profile := workerkernel.GeneratedWorkerLocalDevBridgeProfile
	if !profile.Valid() || !workerkernel.WorkerLocalDevBridgeProfileValid() {
		t.Fatal("generated profile should be valid")
	}
	profile.ProfileDigest = "sha256:tampered"
	if profile.Valid() {
		t.Fatal("tampered profile accepted")
	}
	original := workerkernel.GeneratedWorkerLocalDevBridgeProfile
	mutated := original
	mutated.Mode = "production"
	workerkernel.GeneratedWorkerLocalDevBridgeProfile = mutated
	t.Cleanup(func() { workerkernel.GeneratedWorkerLocalDevBridgeProfile = original })
	if workerkernel.WorkerLocalDevBridgeProfileValid() {
		t.Fatal("mutated exported generated profile accepted")
	}
}

func TestNewLocalLauncherRejectsCallerTransportHooks(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalLauncher(LocalLauncherConfig{
		Endpoint:   "http://127.0.0.1:8091",
		TokenFile:  tokenPath,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })},
	}); err != errLocalLauncherURL {
		t.Fatalf("custom round tripper accepted: %v", err)
	}
}

func TestLocalLauncherBindsAfterD056HealthMetadata(t *testing.T) {
	workerID := &workerv1alpha1.WorkloadIdentity{SpiffeId: workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE, TrustDomain: "cloud-agents.local"}
	supervisorID := &workerv1alpha1.WorkloadIdentity{SpiffeId: workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE, TrustDomain: "cloud-agents.local"}
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: workerID,
		Capabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		IdentityProvider:    workerkernel.StaticIdentityProvider{Identity: supervisorID},
		AdmissionLeaseID:    workerkernel.WorkerLocalDevLauncherLeaseID,
		AdmissionGeneration: workerkernel.WorkerLocalDevLauncherGeneration,
		Clock:               time.Now,
		Executor:            workerkernel.DeterministicLocalExecutor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	connectPath, connectHandler := workerkernel.NewHandler(service)
	mux := http.NewServeMux()
	mux.Handle(connectPath, connectHandler)
	mux.HandleFunc(workerkernel.WorkerLocalDevLauncherHealthRoute, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"api_version": "cloud-agents.localdev/v1", "authority": workerkernel.WorkerLocalDevLauncherAuthorityID,
			"profile": workerkernel.WorkerLocalDevLauncherProfileID, "revision": workerkernel.WorkerLocalDevLauncherRevision,
			"profile_digest": workerkernel.WorkerLocalDevLauncherProfileDigest, "version": "v1.0", "status": "serving",
			"worker_identity": workerkernel.WorkerLocalDevBridgeWorkerIdentitySPIFFE, "supervisor_identity": workerkernel.WorkerLocalDevBridgeSupervisorIdentitySPIFFE,
			"lease_id": workerkernel.WorkerLocalDevBridgeLeaseID, "generation": workerkernel.WorkerLocalDevBridgeGeneration,
			"external_side_effects": false, "transport": "loopback_http_connect",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	launcher, err := NewLocalLauncher(LocalLauncherConfig{Endpoint: server.URL, TokenFile: tokenPath})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := launcher.BindLocalLauncherDispatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempt, err := workerkernel.BuildLocalOperationAttempt(workerkernel.LocalOperationAttemptInput{
		OperationID: "operation-launcher-001", IdempotencyKey: "idempotency-launcher-001",
		Scope: workerkernelNamespaceRef("project-launcher"), FencingLeaseID: workerkernel.WorkerLocalDevLauncherLeaseID,
		FencingGeneration: workerkernel.WorkerLocalDevLauncherGeneration, FencingToken: []byte("launcher-fencing-token"),
		Deadline: now.Add(time.Minute), AttemptID: "attempt-launcher-001", AttemptNumber: 1,
		ExpectedExecutorIdentity: workerID, Negotiation: binding.Negotiation(),
		Command: &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "loopback"}}}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := launcher.DispatchOperation(context.Background(), attempt)
	if err != nil || response == nil || response.Msg == nil || response.Msg.GetOutcome() != workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED {
		t.Fatalf("dispatch response=%v err=%v", response, err)
	}
	receipt, err := launcher.GetOperationReceipt(context.Background(), &workerv1alpha1.ReceiptRequest{
		OperationId: response.Msg.GetOperationId(), ReceiptId: response.Msg.GetReceiptId(),
		ExpectedServerIdentity: workerID, RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		Negotiation: binding.Negotiation(), Fencing: &workerv1alpha1.FencingProof{
			LeaseId: workerkernel.WorkerLocalDevLauncherLeaseID, Generation: workerkernel.WorkerLocalDevLauncherGeneration, Token: []byte("launcher-fencing-token"),
		},
	})
	if err != nil || receipt == nil || receipt.Msg == nil || receipt.Msg.GetReceiptId() != response.Msg.GetReceiptId() {
		t.Fatalf("receipt response=%v err=%v", receipt, err)
	}
}

func workerkernelNamespaceRef(id string) commonv1alpha1.NamespaceRef {
	return commonv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", ID: id}
}

func TestLocalLauncherHealthRejectsDuplicateKeysAndClosesRejectedBody(t *testing.T) {
	closed := false
	body := &trackingReadCloser{Reader: strings.NewReader(`{"api_version":"cloud-agents.localdev/v1","authority":"wrong","authority":"D-056-WORKER-LOCALDEV-LAUNCHER-000001"}`), onClose: func() { closed = true }}
	launcher := &LocalLauncher{
		endpoint: "http://127.0.0.1:8091",
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: body, Header: make(http.Header)}, nil
		})},
	}
	if err := launcher.checkLauncherHealth(context.Background()); err != errLocalLauncherHealth {
		t.Fatalf("health error=%v", err)
	}
	if !closed {
		t.Fatal("rejected health response body was not closed")
	}

	duplicateClosed := false
	duplicateBody := &trackingReadCloser{Reader: strings.NewReader(localLauncherDuplicateHealthJSON()), onClose: func() { duplicateClosed = true }}
	duplicateLauncher := &LocalLauncher{
		endpoint: "http://127.0.0.1:8091",
		httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: duplicateBody, Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
		})},
	}
	if err := duplicateLauncher.checkLauncherHealth(context.Background()); err != errLocalLauncherHealth {
		t.Fatalf("duplicate health error=%v", err)
	}
	if !duplicateClosed {
		t.Fatal("duplicate health response body was not closed")
	}

	duplicate := []byte(`{"api_version":"cloud-agents.localdev/v1","authority":"first","authority":"second"}`)
	if localLauncherJSONKeysUnique(duplicate) {
		t.Fatal("duplicate JSON key accepted")
	}
}

func localLauncherDuplicateHealthJSON() string {
	quote := func(value string) string {
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
	return `{"api_version":` + quote("cloud-agents.localdev/v1") +
		`,"authority":"wrong","authority":` + quote(workerkernel.WorkerLocalDevLauncherAuthorityID) +
		`,"profile":` + quote(workerkernel.WorkerLocalDevLauncherProfileID) +
		`,"revision":` + quote(workerkernel.WorkerLocalDevLauncherRevision) +
		`,"profile_digest":` + quote(workerkernel.WorkerLocalDevLauncherProfileDigest) +
		`,"version":"v1.0","status":"serving","worker_identity":` + quote(workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE) +
		`,"supervisor_identity":` + quote(workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE) +
		`,"lease_id":` + quote(workerkernel.WorkerLocalDevLauncherLeaseID) +
		`,"generation":1,"external_side_effects":false,"transport":"loopback_http_connect"}`
}

type trackingReadCloser struct {
	io.Reader
	onClose func()
}

func (r *trackingReadCloser) Close() error {
	if r.onClose != nil {
		r.onClose()
	}
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
