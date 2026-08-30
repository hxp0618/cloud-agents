//go:build localdev

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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
	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestParseLocalWorkerConfigRejectsNonLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8091", ":8091", "192.168.31.234:8091", "worker.test:8091", "127.0.0.1"} {
		if _, err := parseLocalWorkerConfig([]string{"--listen", address}); !errors.Is(err, errNonLoopbackListen) {
			t.Errorf("%q error = %v, want loopback rejection", address, err)
		}
	}
	for _, address := range []string{"127.0.0.1:0", "127.0.0.1:not-a-port", "127.0.0.1:65536", "127.0.0.1:-1", "[::1]:0"} {
		if _, err := parseLocalWorkerConfig([]string{"--listen", address, "--token-file", filepath.Join(t.TempDir(), "token")}); !errors.Is(err, errNonLoopbackListen) {
			t.Errorf("%q error = %v, want invalid-port rejection", address, err)
		}
	}
	for _, address := range []string{"127.0.0.1:8091", "[::1]:8091"} {
		if cfg, err := parseLocalWorkerConfig([]string{"--listen", address, "--token-file", filepath.Join(t.TempDir(), "token")}); err != nil || cfg.listen != address {
			t.Errorf("%q config = %#v, error = %v", address, cfg, err)
		}
	}
}

func TestParseLocalWorkerConfigAcceptsBoundedRuntimeMode(t *testing.T) {
	directory := t.TempDir()
	cfg, err := parseLocalWorkerConfig([]string{"--token-file", filepath.Join(directory, "worker.token"), "--runtime-command", "/bin/cat", "--runtime-directory", directory, "--runtime-max-sessions", "7"})
	if err != nil || cfg.runtimeCommand != "/bin/cat" || cfg.runtimeDirectory != directory || cfg.runtimeMaxSessions != 7 {
		t.Fatalf("config = %#v, err = %v", cfg, err)
	}
	if _, err := parseLocalWorkerConfig([]string{"--token-file", filepath.Join(directory, "incomplete.token"), "--runtime-command", "/bin/cat"}); !errors.Is(err, errInvalidWorkerConfig) {
		t.Fatalf("incomplete Runtime error = %v", err)
	}
	for _, value := range []string{"0", "1025"} {
		if _, err := parseLocalWorkerConfig([]string{"--token-file", filepath.Join(directory, "invalid-"+value+".token"), "--runtime-command", "/bin/cat", "--runtime-directory", directory, "--runtime-max-sessions", value}); !errors.Is(err, errInvalidWorkerConfig) {
			t.Fatalf("Runtime max sessions %s error = %v", value, err)
		}
	}
}

func TestRunMainReturnsFailureWithoutExitingProcess(t *testing.T) {
	err := runMain([]string{"--listen", "192.168.31.234:8091", "--token-file", filepath.Join(t.TempDir(), "token")}, context.Background())
	if !errors.Is(err, errNonLoopbackListen) {
		t.Fatalf("runMain error = %v, want loopback rejection", err)
	}
	if err := runMain(nil, nil); !errors.Is(err, errInvalidWorkerConfig) {
		t.Fatalf("nil context error = %v, want invalid config", err)
	}
}

func TestRunLocalWorkerDoesNotLeaveTokenWhenListenFails(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	tokenPath := filepath.Join(t.TempDir(), "worker-token")
	err = runLocalWorker(context.Background(), localWorkerConfig{
		listen:    occupied.Addr().String(),
		tokenFile: tokenPath,
		token:     "token-bind-failure",
	})
	if err == nil {
		t.Fatal("runLocalWorker unexpectedly succeeded on an occupied listener")
	}
	if _, statErr := os.Lstat(tokenPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("token file after failed listen: stat=%v, want absent", statErr)
	}
}

func TestWriteLocalWorkerTokenFileIs0600AndExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker-token")
	if err := writeLocalWorkerTokenFile(path, "token-alpha"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("token mode = %o, want 600", mode)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) != "token-alpha\n" {
		t.Fatalf("token file = %q", string(bytes))
	}
	if err := writeLocalWorkerTokenFile(path, "token-beta"); !errors.Is(err, errInvalidTokenPath) {
		t.Fatalf("second token write = %v, want exclusive rejection", err)
	}
	bytes, _ = os.ReadFile(path)
	if string(bytes) != "token-alpha\n" {
		t.Fatalf("exclusive write changed token = %q", string(bytes))
	}
}

func TestLocalWorkerAuthMiddlewareBindsOnlyFixedIdentity(t *testing.T) {
	identity := generatedSupervisorIdentity()
	seen := make(chan *workerv1alpha1.WorkloadIdentity, 1)
	next := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		got, err := (contextIdentityProvider{}).ClientIdentity(request.Context())
		if err != nil {
			t.Errorf("identity provider = %v", err)
		}
		seen <- got
		response.WriteHeader(http.StatusNoContent)
	})
	handler := localWorkerAuthMiddleware("token-alpha", identity, next)
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/test", nil)
	request.RemoteAddr = "127.0.0.1:4242"
	request.Header.Set("Authorization", "Bearer token-alpha")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authenticated status = %d", response.Code)
	}
	got := <-seen
	if got.GetSpiffeId() != identity.GetSpiffeId() || got.GetTrustDomain() != identity.GetTrustDomain() {
		t.Fatalf("context identity = %#v, want %#v", got, identity)
	}
	got.SpiffeId = "mutated"
	if identity.GetSpiffeId() == "mutated" {
		t.Fatal("middleware exposed mutable identity")
	}

	for name, mutate := range map[string]func(*http.Request){
		"missing token":  func(req *http.Request) {},
		"wrong token":    func(req *http.Request) { req.Header.Set("Authorization", "Bearer wrong") },
		"duplicate":      func(req *http.Request) { req.Header.Add("Authorization", "Bearer token-alpha") },
		"non-loopback":   func(req *http.Request) { req.RemoteAddr = "192.168.31.234:4242" },
		"malformed-peer": func(req *http.Request) { req.RemoteAddr = "127.0.0.1:not-a-port" },
	} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/test", nil)
		request.RemoteAddr = "127.0.0.1:4242"
		if name != "missing token" {
			request.Header.Set("Authorization", "Bearer token-alpha")
		}
		mutate(request)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if name == "non-loopback" || name == "malformed-peer" {
			want = http.StatusNotFound
		}
		if response.Code != want {
			t.Errorf("%s status = %d, want %d", name, response.Code, want)
		}
	}
}

func TestGeneratedConnectClientNegotiateAndHealthAgainstLoopbackServer(t *testing.T) {
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:18092", token: "token-connect", clock: func() time.Time { return time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header.Set("Authorization", "Bearer token-connect")
		return http.DefaultTransport.RoundTrip(clone)
	})
	clientHTTP := &http.Client{Transport: transport}
	ts := httptest.NewServer(server.Server.Handler)
	defer ts.Close()
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(clientHTTP, strings.TrimSuffix(ts.URL, workerkernel.WorkerLocalDevLauncherHTTPRoutePrefix))
	workerIdentity := generatedWorkerIdentity()
	response, err := client.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions:      []*workerv1alpha1.ProtocolVersion{{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor}},
		RequiredCapabilities:   []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH},
		ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetAuthenticatedServerIdentity().GetSpiffeId() != workerIdentity.GetSpiffeId() {
		t.Fatalf("authenticated worker identity = %#v", response.Msg.GetAuthenticatedServerIdentity())
	}
	health, err := client.CheckHealth(context.Background(), connect.NewRequest(&workerv1alpha1.HealthRequest{
		Negotiation:            &workerv1alpha1.NegotiationBinding{ProtocolVersion: response.Msg.GetSelectedVersion(), NegotiationId: response.Msg.GetNegotiationId(), ExpiresAt: response.Msg.GetExpiresAt()},
		RequiredCapabilities:   []workerv1alpha1.Capability{workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH},
		ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if health.Msg.GetState() != workerv1alpha1.HealthState_HEALTH_STATE_SERVING {
		t.Fatalf("health state = %s", health.Msg.GetState())
	}
}

func TestGeneratedConnectClientDispatchAndReceiptAgainstLoopbackServer(t *testing.T) {
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:18093", token: "token-noop"})
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header.Set("Authorization", "Bearer token-noop")
		return http.DefaultTransport.RoundTrip(clone)
	})
	ts := httptest.NewServer(server.Server.Handler)
	defer ts.Close()
	client := workerv1alpha1connect.NewWorkerExecutionServiceClient(&http.Client{Transport: transport}, strings.TrimSuffix(ts.URL, workerkernel.WorkerLocalDevLauncherHTTPRoutePrefix))
	now := time.Now().UTC()
	workerIdentity := generatedWorkerIdentity()
	negotiation, err := client.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor}},
		RequiredCapabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION,
			workerv1alpha1.Capability_CAPABILITY_HEALTH,
			workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		},
		ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	binding := &workerv1alpha1.NegotiationBinding{ProtocolVersion: negotiation.Msg.GetSelectedVersion(), NegotiationId: negotiation.Msg.GetNegotiationId(), ExpiresAt: negotiation.Msg.GetExpiresAt()}
	attempt, err := workerkernel.BuildLocalOperationAttempt(workerkernel.LocalOperationAttemptInput{
		OperationID: "operation-http-001", IdempotencyKey: "idempotency-http-001",
		Scope:          commonv1alpha1.NamespaceRef{Namespace: "cloud-agents", Kind: "project", ID: "project-http"},
		FencingLeaseID: workerkernel.WorkerLocalDevLauncherLeaseID, FencingGeneration: workerkernel.WorkerLocalDevLauncherGeneration,
		FencingToken: []byte("http-fencing-token"), Deadline: now.Add(time.Minute), AttemptID: "attempt-http-001", AttemptNumber: 1,
		ExpectedExecutorIdentity: workerIdentity, Negotiation: binding,
		Command: &workerv1alpha1.OperationCommand{Command: &workerv1alpha1.OperationCommand_Probe{Probe: &workerv1alpha1.ProbeOperation{ProbeName: "loopback"}}}, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ExecuteOperation(context.Background(), connect.NewRequest(attempt))
	if err != nil || response == nil || response.Msg == nil || response.Msg.GetOutcome() != workerv1alpha1.OperationOutcome_OPERATION_OUTCOME_SUCCEEDED {
		t.Fatalf("ExecuteOperation response=%v error=%v", response, err)
	}
	receipt, err := client.GetOperationReceipt(context.Background(), connect.NewRequest(&workerv1alpha1.ReceiptRequest{
		OperationId: response.Msg.GetOperationId(), ReceiptId: response.Msg.GetReceiptId(), ExpectedServerIdentity: workerIdentity,
		RequiredCapability: workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH, Negotiation: binding,
		Fencing: &workerv1alpha1.FencingProof{LeaseId: workerkernel.WorkerLocalDevLauncherLeaseID, Generation: workerkernel.WorkerLocalDevLauncherGeneration, Token: []byte("http-fencing-token")},
	}))
	if err != nil || receipt == nil || receipt.Msg == nil || receipt.Msg.GetReceiptId() != response.Msg.GetReceiptId() {
		t.Fatalf("GetOperationReceipt response=%v error=%v", receipt, err)
	}
}

func TestLocalWorkerRuntimeModeServesGeneratedWireOverH2C(t *testing.T) {
	built, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:18095", token: "runtime-token", runtimeCommand: "/bin/cat", runtimeDirectory: t.TempDir(), runtimeMaxSessions: workerkernel.DefaultRuntimeMaxSessions})
	if err != nil {
		t.Fatal(err)
	}
	if built.Server.ReadTimeout != 0 || built.Server.WriteTimeout != 0 {
		t.Fatalf("Runtime HTTP timeouts = %s/%s", built.Server.ReadTimeout, built.Server.WriteTimeout)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serve := make(chan error, 1)
	go func() { serve <- built.Server.Serve(listener) }()
	defer func() {
		_ = built.Server.Shutdown(context.Background())
		<-serve
	}()
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{Proxy: nil, Protocols: protocols}
	httpClient := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		clone := request.Clone(request.Context())
		clone.Header.Set("Authorization", "Bearer runtime-token")
		return transport.RoundTrip(clone)
	})}
	endpoint := "http://" + listener.Addr().String()
	healthResponse, err := httpClient.Get(endpoint + workerkernel.WorkerLocalDevLauncherHealthRoute)
	if err != nil {
		t.Fatal(err)
	}
	var health localWorkerHealth
	if healthResponse.StatusCode != http.StatusOK || json.NewDecoder(healthResponse.Body).Decode(&health) != nil || health.Profile != "cloud-agents/worker-localdev-runtime/v1" || !health.ExternalEffects {
		t.Fatalf("health = %#v, status = %d", health, healthResponse.StatusCode)
	}
	_ = healthResponse.Body.Close()
	workerIdentity := generatedWorkerIdentity()
	workerClient := workerv1alpha1connect.NewWorkerExecutionServiceClient(httpClient, endpoint)
	negotiation, err := workerClient.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{
		SupportedVersions: []*workerv1alpha1.ProtocolVersion{{Major: workerkernel.ProtocolMajor, Minor: workerkernel.ProtocolMinor}}, RequiredCapabilities: []workerv1alpha1.Capability{
			workerv1alpha1.Capability_CAPABILITY_NEGOTIATION, workerv1alpha1.Capability_CAPABILITY_HEALTH, workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH,
		}, ExpectedServerIdentity: workerIdentity,
	}))
	if err != nil {
		t.Fatal(err)
	}
	runtimeClient := workerruntimev1alpha1connect.NewWorkerRuntimeServiceClient(httpClient, endpoint)
	stream := runtimeClient.OpenSession(context.Background())
	defer func() { _ = stream.CloseResponse() }()
	if err := stream.Send(&workerruntimev1alpha1.RuntimeSessionRequest{Frame: &workerruntimev1alpha1.RuntimeSessionRequest_Open{Open: &workerruntimev1alpha1.RuntimeSessionOpen{
		Negotiation: &workerv1alpha1.NegotiationBinding{ProtocolVersion: negotiation.Msg.GetSelectedVersion(), NegotiationId: negotiation.Msg.GetNegotiationId(), ExpiresAt: negotiation.Msg.GetExpiresAt()},
		Fencing:     &workerv1alpha1.FencingProof{LeaseId: workerkernel.WorkerLocalDevLauncherLeaseID, Generation: workerkernel.WorkerLocalDevLauncherGeneration, Token: []byte("runtime-token")}, ExecutionId: "execution-localdev", Generation: workerkernel.WorkerLocalDevLauncherGeneration, ExpectedWorkerIdentity: workerIdentity, ProviderKind: "codex",
	}}}); err != nil {
		t.Fatal(err)
	}
	ready, err := stream.Receive()
	if err != nil || ready.GetReady() == nil || ready.GetReady().GetExecutionId() != "execution-localdev" {
		t.Fatalf("ready = %#v, err = %v", ready, err)
	}
	if err := stream.CloseRequest(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRuntimeEnvironmentExcludesControlPlaneAuthority(t *testing.T) {
	filtered := localRuntimeEnvironment([]string{
		"PATH=/usr/bin", "CLOUD_AGENTS_PLATFORM_DATABASE_URL=postgres://runtime:secret@127.0.0.1/cloud_agents", "CLOUD_AGENTS_PLATFORM_AUTH_CONFIG=/run/cloud-agents/auth.json", "CLOUD_AGENTS_ADMISSION_TOKEN=worker-secret", "CLOUD_AGENT_PROVIDER_CREDENTIAL_FD=9", "OPENAI_API_KEY=provider-key",
	})
	want := []string{"PATH=/usr/bin", "OPENAI_API_KEY=provider-key"}
	if len(filtered) != len(want) || filtered[0] != want[0] || filtered[1] != want[1] {
		t.Fatalf("filtered environment = %#v, want %#v", filtered, want)
	}
}

func TestLocalWorkerHealthAndUnknownRouteFailClosed(t *testing.T) {
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:18094", token: "token-health"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Server.Handler)
	defer ts.Close()
	for _, test := range []struct {
		name string
		path string
		code int
		head string
	}{
		{name: "unknown", path: "/unknown", code: http.StatusNotFound, head: "Bearer token-health"},
		{name: "health unauthorized", path: workerkernel.WorkerLocalDevLauncherHealthRoute, code: http.StatusUnauthorized},
		{name: "health authorized", path: workerkernel.WorkerLocalDevLauncherHealthRoute, code: http.StatusOK, head: "Bearer token-health"},
	} {
		request, _ := http.NewRequest(http.MethodGet, ts.URL+test.path, nil)
		if test.head != "" {
			request.Header.Set("Authorization", test.head)
		}
		response, requestErr := ts.Client().Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		if response.StatusCode != test.code {
			t.Errorf("%s status = %d, want %d", test.name, response.StatusCode, test.code)
		}
		if test.name == "health authorized" {
			var health localWorkerHealth
			if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
				t.Fatal(err)
			}
			if health.Authority != workerkernel.WorkerLocalDevLauncherAuthorityID ||
				health.Profile != workerkernel.WorkerLocalDevLauncherProfileID ||
				health.Revision != workerkernel.WorkerLocalDevLauncherRevision ||
				health.ProfileDigest != workerkernel.WorkerLocalDevLauncherProfileDigest ||
				health.WorkerIdentity != workerkernel.WorkerLocalDevLauncherWorkerIdentitySPIFFE ||
				health.Supervisor != workerkernel.WorkerLocalDevLauncherSupervisorIdentitySPIFFE ||
				health.LeaseID != workerkernel.WorkerLocalDevLauncherLeaseID ||
				health.Generation != workerkernel.WorkerLocalDevLauncherGeneration ||
				health.Transport != "loopback_http_connect" ||
				health.Status != "serving" || health.ExternalEffects {
				t.Fatalf("health = %#v", health)
			}
		}
		_ = response.Body.Close()
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
