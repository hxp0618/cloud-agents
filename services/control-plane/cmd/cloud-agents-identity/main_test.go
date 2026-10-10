package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
)

func TestInitializationIDsMatchDurableSchemas(t *testing.T) {
	eventID, correlationID, err := newInitializationIDs()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(eventID) {
		t.Fatalf("event ID %q does not match the durable audit schema", eventID)
	}
	if !regexp.MustCompile(`^identity-init-request-[a-f0-9]{32}$`).MatchString(correlationID) {
		t.Fatalf("correlation ID %q does not match the request identifier schema", correlationID)
	}
}

func TestIdentityRunConfigIsStrictAndReferencesSecretsOnly(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "identity.json")
	value := validRunConfigFixture(directory)
	writeJSONFile(t, configPath, value, 0o644)
	loaded, err := loadRunConfig(configPath)
	if err != nil || loaded.ControlPlaneURL != "https://control-plane.example.test" {
		t.Fatalf("config=%#v err=%v", loaded, err)
	}

	value["databaseUrl"] = "postgres://inline-secret-marker"
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadRunConfig(configPath); err == nil || strings.Contains(err.Error(), "inline-secret-marker") {
		t.Fatalf("inline secret error = %v", err)
	}
	delete(value, "databaseUrl")
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := bytes.Replace(encoded, []byte(`"listen":"127.0.0.1:8443"`), []byte(`"listen":"127.0.0.1:8443","listen":"127.0.0.1:9443"`), 1)
	if err := os.WriteFile(configPath, duplicate, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRunConfig(configPath); err == nil {
		t.Fatal("duplicate configuration field was accepted")
	}
	if err := os.WriteFile(configPath, bytes.Repeat([]byte(" "), maximumIdentityConfigBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRunConfig(configPath); err == nil {
		t.Fatal("oversized configuration was accepted")
	}

	value = validRunConfigFixture(directory)
	value["providerSecretFiles"] = json.RawMessage(`{"duplicate-ref":"/first","duplicate-ref":"/second"}`)
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadRunConfig(configPath); err == nil {
		t.Fatal("duplicate provider material reference was accepted")
	}
}

func TestProviderMaterialsResolveConfiguredFilesOnly(t *testing.T) {
	directory := t.TempDir()
	secretPath := filepath.Join(directory, "provider.secret")
	rootPath := filepath.Join(directory, "provider-ca.crt")
	writeFile(t, secretPath, []byte("provider-client-secret"), 0o600)
	certificatePEM, _, _ := testTLSIdentity(t)
	writeFile(t, rootPath, certificatePEM, 0o644)
	materials, err := newFileProviderMaterials(
		map[string]string{"provider-secret": secretPath},
		map[string]string{"provider-root": rootPath},
	)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := materials.ClientSecret("provider-secret")
	if err != nil || secret != "provider-client-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if _, err := materials.ClientSecret("unknown-secret"); err == nil {
		t.Fatal("unknown provider secret reference was accepted")
	}
	client, err := materials.HTTPClient("provider-root")
	if err != nil || client == nil {
		t.Fatalf("client=%v err=%v", client, err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil || transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("provider client did not pin configured TLS roots")
	}
	if _, err := materials.HTTPClient("unknown-root"); err == nil {
		t.Fatal("unknown provider root reference was accepted")
	}
	if again, err := materials.HTTPClient("provider-root"); err != nil || again != client {
		t.Fatal("unchanged provider roots did not reuse the pooled client", err)
	}
	rotatedPEM, _, _ := testTLSIdentity(t)
	writeFile(t, rootPath, rotatedPEM, 0o644)
	if rotated, err := materials.HTTPClient("provider-root"); err != nil || rotated == client {
		t.Fatal("rotated provider roots reused the previous client", err)
	}
	if err := os.Chmod(secretPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := materials.ClientSecret("provider-secret"); err == nil {
		t.Fatal("provider secret with broad permissions was accepted")
	}
}

func TestIdentityInitializeConfigRejectsInlinePasswordAndNoncanonicalTime(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "initialize.json")
	value := validInitializeConfigFixture(directory)
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadInitializeConfig(configPath); err != nil {
		t.Fatal(err)
	}
	value["password"] = "inline-password-marker"
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadInitializeConfig(configPath); err == nil || strings.Contains(err.Error(), "inline-password-marker") {
		t.Fatalf("inline password error = %v", err)
	}
	if _, err := parseCanonicalTime("2026-10-08T00:00:00+08:00"); err == nil {
		t.Fatal("non-UTC signing time was accepted")
	}
}

func TestIdentityInitializeConfigMatchesDurableAccountBounds(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "initialize.json")
	value := validInitializeConfigFixture(directory)

	value["displayName"] = strings.Repeat("界", 160)
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadInitializeConfig(configPath); err != nil {
		t.Fatal("160 Unicode-character display name was rejected")
	}
	value["displayName"] = strings.Repeat("界", 161)
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadInitializeConfig(configPath); err == nil {
		t.Fatal("161 Unicode-character display name was accepted")
	}

	value["displayName"] = "Initial Admin"
	value["email"] = strings.Repeat("a", 243) + "@example.com"
	writeJSONFile(t, configPath, value, 0o644)
	if _, err := loadInitializeConfig(configPath); err == nil {
		t.Fatal("255-byte email was accepted")
	}
}

func TestIdentityAuthorizationClientUsesPinnedTLSAndCanonicalCredential(t *testing.T) {
	certificatePEM, _, certificate := testTLSIdentity(t)
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/identity/authorize-tenant-token" || request.Header.Get("Authorization") != "Bearer "+credential {
			t.Errorf("unexpected request %s %s auth=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, encodeErr := api.EncodeTenantTokenAuthorizationJSON(api.TenantTokenAuthorization{
			UserID: "account-alpha", Issuer: "https://identity.example.test", TenantID: "tenant-alpha",
			Application: api.IdentityApplicationAdmin, Scopes: []string{"projects.get"},
		})
		if encodeErr != nil {
			t.Error(encodeErr)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write(body)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	defer server.Close()
	directory := t.TempDir()
	rootCAPath := filepath.Join(directory, "root-ca.pem")
	writeFile(t, rootCAPath, certificatePEM, 0o644)
	client, err := newAuthorizationClient(server.URL, rootCAPath, credential)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.AuthorizeTenantToken(context.Background(), "request-authorize", api.TenantTokenAuthorizationRequest{
		Application: api.IdentityApplicationAdmin, SessionSHA256: "sha256:" + strings.Repeat("1", 64), TenantID: "tenant-alpha",
	})
	if err != nil || result.UserID != "account-alpha" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if client, err := newAuthorizationClient("http://"+server.Listener.Addr().String(), rootCAPath, credential); client != nil || err == nil {
		t.Fatal("plaintext Control Plane URL was accepted")
	}
	if client, err := newAuthorizationClient(server.URL, rootCAPath, "not-a-proof"); client != nil || err == nil {
		t.Fatal("noncanonical authorization credential was accepted")
	}
	wrongRootPEM, _, _ := testTLSIdentity(t)
	wrongRootPath := filepath.Join(directory, "wrong-root-ca.pem")
	writeFile(t, wrongRootPath, wrongRootPEM, 0o644)
	untrustedClient, err := newAuthorizationClient(server.URL, wrongRootPath, credential)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrustedClient.AuthorizeTenantToken(context.Background(), "request-untrusted", api.TenantTokenAuthorizationRequest{
		Application: api.IdentityApplicationAdmin, SessionSHA256: "sha256:" + strings.Repeat("1", 64), TenantID: "tenant-alpha",
	}); err == nil {
		t.Fatal("untrusted Control Plane TLS identity was accepted")
	}
}

func TestIdentityHTTPServerRequiresTLS12AndShutsDown(t *testing.T) {
	certificatePEM, privateKeyPEM, _ := testTLSIdentity(t)
	directory := t.TempDir()
	certificatePath := filepath.Join(directory, "tls.crt")
	privateKeyPath := filepath.Join(directory, "tls.key")
	writeFile(t, certificatePath, certificatePEM, 0o644)
	writeFile(t, privateKeyPath, privateKeyPEM, 0o600)
	config := runConfig{TLSCertificateFile: certificatePath, TLSPrivateKeyFile: privateKeyPath}
	tlsConfig, err := loadServerTLSConfig(config)
	if err != nil || tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("TLS config=%#v err=%v", tlsConfig, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	server := newIdentityHTTPServer(listener.Addr().String(), tlsConfig, handler, ctx)
	done := make(chan error, 1)
	go func() { done <- serveIdentityHTTPS(ctx, listener, server) }()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("test root CA is invalid")
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}}, Timeout: 5 * time.Second}
	response, err := httpClient.Get("https://" + listener.Addr().String())
	if err != nil || response.StatusCode != http.StatusNoContent {
		t.Fatalf("response=%v err=%v", response, err)
	}
	_ = response.Body.Close()
	legacyClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MaxVersion: tls.VersionTLS11, RootCAs: roots}}, Timeout: 5 * time.Second}
	if response, err := legacyClient.Get("https://" + listener.Addr().String()); err == nil {
		_ = response.Body.Close()
		t.Fatal("TLS 1.1 client was accepted")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("identity HTTPS server did not shut down")
	}
}

func TestIdentityHashPasswordCreatesExclusive0600Secret(t *testing.T) {
	directory := t.TempDir()
	passwordPath := filepath.Join(directory, "password")
	outputPath := filepath.Join(directory, "password-hash")
	password := "correct horse battery staple"
	writeFile(t, passwordPath, []byte(password), 0o600)
	var output bytes.Buffer
	if err := execute(context.Background(), []string{"hash-password", "--password-file", passwordPath, "--output-file", outputPath}, nil, &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatal("hash-password wrote secret material to stdout")
	}
	hash, err := os.ReadFile(outputPath)
	if err != nil || !browserauth.ValidPasswordHash(string(hash)) || strings.Contains(string(hash), password) {
		t.Fatalf("password hash file is invalid: err=%v", err)
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("password hash mode=%v", info.Mode().Perm())
	}
	original := append([]byte(nil), hash...)
	if err := hashPasswordFile(passwordPath, outputPath); err == nil {
		t.Fatal("existing password hash file was overwritten")
	}
	if current, err := os.ReadFile(outputPath); err != nil || !bytes.Equal(current, original) {
		t.Fatal("existing password hash changed")
	}
}

func TestIdentitySecretFilesRequirePrivateRegularFilesWithoutLeakage(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "credential-secret-marker")
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, []byte(credential), 0o644)
	if value, err := readCanonicalProof(path); value != "" || err == nil || strings.Contains(err.Error(), credential) || strings.Contains(err.Error(), path) {
		t.Fatalf("public secret file value=%q err=%v", value, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if value, err := readCanonicalProof(path); err != nil || value != credential {
		t.Fatalf("private secret value=%q err=%v", value, err)
	}
	link := filepath.Join(directory, "credential-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if value, err := readCanonicalProof(link); value != "" || err == nil {
		t.Fatal("symlinked secret file was accepted")
	}
}

func testTLSIdentity(t *testing.T) ([]byte, []byte, tls.Certificate) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "identity-test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return certificatePEM, privateKeyPEM, pair
}

func validRunConfigFixture(directory string) map[string]any {
	return map[string]any{
		"listen": "127.0.0.1:8443", "databaseUrlFile": filepath.Join(directory, "database-url"),
		"tlsCertificateFile": filepath.Join(directory, "tls.crt"), "tlsPrivateKeyFile": filepath.Join(directory, "tls.key"),
		"issuer": "https://identity.example.test", "adminAudience": "https://admin.example.test", "userAudience": "https://user.example.test",
		"signingKeyId": "identity-key-1", "signingPrivateKeyFile": filepath.Join(directory, "signing.key"), "csrfKeyFile": filepath.Join(directory, "csrf.key"),
		"adminWebCredentialFile": filepath.Join(directory, "admin.proof"), "userWebCredentialFile": filepath.Join(directory, "user.proof"),
		"controlPlaneCredentialFile": filepath.Join(directory, "control-plane.proof"), "controlPlaneUrl": "https://control-plane.example.test",
		"controlPlaneRootCaFile": filepath.Join(directory, "control-plane-ca.crt"), "controlPlaneAuthorizationCredentialFile": filepath.Join(directory, "identity-to-cp.proof"),
		"providerFlowKeyFile": filepath.Join(directory, "provider-flow.key"), "providerSecretFiles": map[string]string{}, "providerRootCaFiles": map[string]string{},
	}
}

func validInitializeConfigFixture(directory string) map[string]any {
	return map[string]any{
		"bootstrapDatabaseUrlFile": filepath.Join(directory, "bootstrap-database-url"), "issuer": "https://identity.example.test",
		"adminAudience": "https://admin.example.test", "userAudience": "https://user.example.test", "signingKeyId": "identity-key-1",
		"signingPrivateKeyFile": filepath.Join(directory, "signing.key"), "setupProofFile": filepath.Join(directory, "setup.proof"),
		"userId": "admin-initial", "email": "admin@example.test", "displayName": "Initial Admin",
		"passwordHashFile": filepath.Join(directory, "password-hash"), "keyNotBefore": "2026-10-08T00:00:00Z", "keyNotAfter": "2027-10-08T00:00:00Z",
	}
}

func writeJSONFile(t *testing.T, path string, value any, mode os.FileMode) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, data, mode)
}

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
