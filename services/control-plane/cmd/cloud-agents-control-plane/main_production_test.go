//go:build !localdev

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
		productionAdmissionLeaseEnvironment:      "runtime-lease",
		productionAdmissionGenerationEnvironment: "7",
		productionAdmissionTokenEnvironment:      "runtime-token",
	}
	config, err := parseProductionConfig([]string{"--listen", "127.0.0.1:9443", "--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.listen != "127.0.0.1:9443" || config.database == "" || config.authPath == "" || config.tlsCert != "/tmp/cert" || config.tlsKey != "/tmp/key" || config.workerEndpoint != "https://worker:8091" || config.admissionGeneration != 7 || !bytes.Equal(config.admissionToken, []byte("runtime-token")) {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseProductionConfigRejectsPartialTLS(t *testing.T) {
	if _, err := parseProductionConfig([]string{"--database-url", "postgres://runtime@db/cloud_agents", "--auth-config", "/etc/cloud-agents/auth.json", "--tls-cert", "/tmp/cert"}, nil); err == nil {
		t.Fatal("expected partial TLS configuration error")
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
