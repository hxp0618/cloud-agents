//go:build !localdev

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
)

func TestParseProductionConfigRequiresTLSAndUsesEnvironment(t *testing.T) {
	if _, err := parseProductionConfig(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected required production configuration error")
	}
	values := map[string]string{
		productionDatabaseEnvironment:            "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:          "/etc/cloud-agents/auth.json",
		productionWorkerEndpointEnvironment:      "https://worker:8091",
		productionWorkerSPIFFEEnvironment:        "spiffe://cloud-agents.test/worker",
		productionWorkerClientCertEnvironment:    "/etc/cloud-agents/worker-client.crt",
		productionWorkerClientKeyEnvironment:     "/etc/cloud-agents/worker-client.key",
		productionWorkerCAEnvironment:            "/etc/cloud-agents/worker-ca.crt",
		productionWorkspaceEnvironment:           "/workspace",
		productionDockerCredentialsEnvironment:   "/etc/cloud-agents/docker-targets",
		productionAdmissionLeaseEnvironment:      "runtime-lease",
		productionAdmissionGenerationEnvironment: "7",
		productionAdmissionTokenEnvironment:      "runtime-token",
	}
	args := []string{"--listen", "127.0.0.1:9443", "--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}
	getenv := func(name string) string { return values[name] }
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.listen != "127.0.0.1:9443" || config.database == "" || config.authPath == "" || config.tlsCert != "/tmp/cert" || config.tlsKey != "/tmp/key" || config.workerEndpoint != "https://worker:8091" || config.dockerCredentials != "/etc/cloud-agents/docker-targets" || config.admissionGeneration != 7 || !bytes.Equal(config.admissionToken, []byte("runtime-token")) || config.maxConcurrentRequests != defaultProductionMaxConcurrentRequests {
		t.Fatalf("config = %#v", config)
	}
	for _, invalid := range []string{"0", "10001"} {
		candidate := append(append([]string{}, args...), "--max-concurrent-requests", invalid)
		if _, err := parseProductionConfig(candidate, getenv); err == nil {
			t.Fatalf("accepted max concurrent requests %s", invalid)
		}
	}
	if _, err := parseProductionConfig(append(append([]string{}, args...), "--docker-credentials-directory", " /tmp/docker-targets"), getenv); err == nil {
		t.Fatal("accepted invalid Docker credential directory")
	}
}

func TestParseProductionConfigRejectsPartialTLS(t *testing.T) {
	if _, err := parseProductionConfig([]string{"--database-url", "postgres://runtime@db/cloud_agents", "--auth-config", "/etc/cloud-agents/auth.json", "--tls-cert", "/tmp/cert"}, nil); err == nil {
		t.Fatal("expected partial TLS configuration error")
	}
}

func TestParseProductionConfigAllowsEnvironmentRoutedWorkers(t *testing.T) {
	values := map[string]string{
		productionDatabaseEnvironment:         "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:       "/etc/cloud-agents/auth.json",
		productionWorkerClientCertEnvironment: "/etc/cloud-agents/worker-client.crt",
		productionWorkerClientKeyEnvironment:  "/etc/cloud-agents/worker-client.key",
		productionWorkerCAEnvironment:         "/etc/cloud-agents/worker-ca.crt",
		productionWorkspaceEnvironment:        "/workspace",
		productionAdmissionTokenEnvironment:   "runtime-token",
	}
	args := []string{"--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}
	getenv := func(name string) string { return values[name] }
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.workerEndpoint != "" || config.workerSPIFFE != "" || config.admissionLeaseID != "" || config.admissionGeneration != 0 {
		t.Fatalf("unexpected fixed Worker route: %#v", config)
	}
	if _, err := parseProductionConfig(append(args, "--worker-endpoint", "https://worker:8091"), getenv); err == nil {
		t.Fatal("accepted partial fixed Worker route")
	}
}

func TestProductionAccessLogIsCorrelatedAndDoesNotLeakRequestInputs(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Request-ID", "request-alpha")
		writer.WriteHeader(http.StatusForbidden)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects?pageToken=opaque-secret", nil)
	request.Header.Set("Authorization", "Bearer bearer-secret")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event["msg"] != "http request" || event["method"] != http.MethodPost || event["path"] != "/v1/tenants/tenant-alpha/projects" || event["status"] != float64(http.StatusForbidden) || event["request_id"] != "request-alpha" {
		t.Fatalf("access log = %#v", event)
	}
	if _, ok := event["duration_ms"].(float64); !ok || strings.Contains(output.String(), "opaque-secret") || strings.Contains(output.String(), "bearer-secret") {
		t.Fatalf("unsafe access log = %s", output.String())
	}

	output.Reset()
	probe := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	probe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if output.Len() != 0 {
		t.Fatalf("successful probe was logged: %s", output.String())
	}
	failedProbe := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusServiceUnavailable) }))
	failedProbe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if !strings.Contains(output.String(), `"status":503`) {
		t.Fatalf("failed probe was not logged: %s", output.String())
	}
}

func TestFetchJWKSRequiresHTTPSAndPreservesKeys(t *testing.T) {
	if _, err := fetchJWKSWithClient("http://issuer.example/jwks", http.DefaultClient); err == nil {
		t.Fatal("accepted non-HTTPS JWKS URL")
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		_, _ = io.WriteString(writer, `{"keys":[{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB"}]}`)
	}))
	defer server.Close()
	client := server.Client()
	keys, err := fetchJWKSWithClient(server.URL, client)
	if err != nil || len(keys) != 1 || string(keys[0]) != `{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB"}` {
		t.Fatalf("keys=%s err=%v", keys, err)
	}
}

func TestLoadConfiguredVerifierConfigUsesJWKSURL(t *testing.T) {
	path := t.TempDir() + "/auth.json"
	if err := os.WriteFile(path, []byte(`{"issuer":"https://issuer.example","audience":"https://api.example","jwksUrl":"https://issuer.example/jwks","generation":1,"securityEpoch":7,"notBefore":100,"expiresAt":200,"keys":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	modulus := make([]byte, 256)
	modulus[0], modulus[len(modulus)-1] = 0x80, 1
	n := base64.RawURLEncoding.EncodeToString(modulus)
	var fetched string
	config, err := loadConfiguredVerifierConfigWith(path, func(rawURL string) ([]json.RawMessage, error) {
		fetched = rawURL
		return []json.RawMessage{
			json.RawMessage(`{"kty":"EC","kid":"ec-key","crv":"P-256","x":"AQ","y":"AQ"}`),
			json.RawMessage(`{"alg":"RS256","kty":"RSA","use":"sig","x5c":["certificate"],"n":"` + n + `","e":"AQAB","kid":"key-1","x5t":"thumbprint"}`),
		}, nil
	})
	wantJWK := `{"alg":"RS256","e":"AQAB","key_ops":["verify"],"kid":"key-1","kty":"RSA","n":"` + n + `","use":"sig"}`
	if err != nil || fetched != "https://issuer.example/jwks" || len(config.Keys) != 1 || string(config.Keys[0].JWK) != wantJWK || !config.Keys[0].Enabled || config.Keys[0].NotBefore != 100 || config.Keys[0].NotAfter != 200 {
		t.Fatalf("config=%#v fetched=%q err=%v", config, fetched, err)
	}
	if _, err := authn.NewConfiguredVerifier(config); err != nil {
		t.Fatalf("normalized remote JWKS cannot initialize verifier: %v", err)
	}
}

func TestLoadConfiguredVerifierConfigRejectsUnsafeJWKSKeys(t *testing.T) {
	path := t.TempDir() + "/auth.json"
	if err := os.WriteFile(path, []byte(`{"issuer":"https://issuer.example","audience":"https://api.example","jwksUrl":"https://issuer.example/jwks","generation":1,"securityEpoch":7,"notBefore":100,"expiresAt":200,"keys":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB","d":"private"}`,
		`{"kty":"RSA","kid":"key-1","\u006bid":"key-2","n":"AQ","e":"AQAB"}`,
		`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB","key_ops":"verify"}`,
		`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB","alg":"RS512"}`,
		`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB","use":"enc"}`,
		`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB","key_ops":["sign"]}`,
	} {
		if _, err := loadConfiguredVerifierConfigWith(path, func(string) ([]json.RawMessage, error) {
			return []json.RawMessage{json.RawMessage(key)}, nil
		}); err == nil {
			t.Fatalf("accepted unsafe JWKS key: %s", key)
		}
	}
}
