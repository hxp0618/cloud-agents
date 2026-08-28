//go:build localdev

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerv1alpha1connect "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestParseLocalWorkerConfigRejectsNonLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:8091", ":8091", "192.168.31.234:8091", "worker.test:8091", "127.0.0.1"} {
		if _, err := parseLocalWorkerConfig([]string{"--listen", address}); !errors.Is(err, errNonLoopbackListen) {
			t.Errorf("%q error = %v, want loopback rejection", address, err)
		}
	}
	for _, address := range []string{"127.0.0.1:8091", "[::1]:8091"} {
		if cfg, err := parseLocalWorkerConfig([]string{"--listen", address, "--token-file", filepath.Join(t.TempDir(), "token")}); err != nil || cfg.listen != address {
			t.Errorf("%q config = %#v, error = %v", address, cfg, err)
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
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:0", token: "token-connect", clock: func() time.Time { return time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC) }})
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

func TestGeneratedConnectClientDispatchAndReceiptRemainUnimplemented(t *testing.T) {
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:0", token: "token-noop"})
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
	if _, err := client.ExecuteOperation(context.Background(), connect.NewRequest(&workerv1alpha1.OperationAttemptEnvelope{})); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("ExecuteOperation error = %v, want unimplemented", err)
	}
	if _, err := client.GetOperationReceipt(context.Background(), connect.NewRequest(&workerv1alpha1.ReceiptRequest{})); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("GetOperationReceipt error = %v, want unimplemented", err)
	}
}

func TestLocalWorkerHealthAndUnknownRouteFailClosed(t *testing.T) {
	server, err := newLocalWorkerHTTPServer(localWorkerConfig{listen: "127.0.0.1:0", token: "token-health"})
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
