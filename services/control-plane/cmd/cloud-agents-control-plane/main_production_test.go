//go:build !localdev

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
	var fetched string
	config, err := loadConfiguredVerifierConfigWith(path, func(rawURL string) ([]json.RawMessage, error) {
		fetched = rawURL
		return []json.RawMessage{json.RawMessage(`{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB"}`)}, nil
	})
	if err != nil || fetched != "https://issuer.example/jwks" || len(config.Keys) != 1 || string(config.Keys[0].JWK) != `{"kty":"RSA","kid":"key-1","n":"AQ","e":"AQAB"}` || !config.Keys[0].Enabled || config.Keys[0].NotBefore != 100 || config.Keys[0].NotAfter != 200 {
		t.Fatalf("config=%#v fetched=%q err=%v", config, fetched, err)
	}
}
