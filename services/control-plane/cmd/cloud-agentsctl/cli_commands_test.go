package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRunCLILoginUsesLoopbackPKCEAndWritesPrivateProfile(t *testing.T) {
	const (
		grant       = "EEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEEE"
		accessToken = "aaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbb.ccccccccccccccccccccccc"
	)
	callbackCode := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte("c"), 32))
	var callbackPort int
	var state, challenge string
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/cli/start":
			var body struct {
				Application   string `json:"application"`
				CallbackPort  int    `json:"callbackPort"`
				State         string `json:"state"`
				CodeChallenge string `json:"codeChallenge"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Application != "admin" || len(body.State) != 43 || len(body.CodeChallenge) != 43 {
				t.Fatalf("start body = %#v", body)
			}
			callbackPort, state, challenge = body.CallbackPort, body.State, body.CodeChallenge
			_, _ = io.WriteString(writer, `{"authorizationId":"cli-authorization-alpha","verificationUrl":"`+server.URL+`/auth/cli/authorize?id=cli-authorization-alpha&state=`+state+`","expiresAt":"2026-11-08T10:05:00Z"}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/cli/exchange":
			var body struct {
				AuthorizationID   string `json:"authorizationId"`
				AuthorizationCode string `json:"authorizationCode"`
				CodeVerifier      string `json:"codeVerifier"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(body.CodeVerifier))
			if body.AuthorizationID != "cli-authorization-alpha" || body.AuthorizationCode != callbackCode || base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
				t.Fatalf("exchange body does not bind authorization and PKCE")
			}
			_, _ = io.WriteString(writer, `{"credential":"`+grant+`","application":"admin","expiresAt":"2026-11-08T10:00:00Z"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/auth/cli/tenants":
			if request.Header.Get("Authorization") != "Bearer "+grant {
				t.Fatalf("tenant authorization = %q", request.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(writer, `{"tenants":[{"id":"tenant-alpha","name":"Tenant Alpha","displayRoles":["tenant.admin"]}]}`)
		case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/cli/tenant-token":
			if request.Header.Get("Authorization") != "Bearer "+grant {
				t.Fatalf("token authorization = %q", request.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(writer, `{"accessToken":"`+accessToken+`","tokenType":"Bearer","expiresAt":"2026-11-08T10:15:00Z"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/admin/tenants/tenant-alpha/organizations":
			if request.Header.Get("Authorization") != "Bearer "+accessToken {
				t.Fatalf("organization authorization = %q", request.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(writer, `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"OrganizationPage","organizations":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Organization","metadata":{"uid":"organization-alpha","name":"organization-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"displayName":"Organization Alpha","state":"active"}}]}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/admin/tenants/tenant-alpha/projects":
			if request.URL.Query().Get("organizationId") != "organization-alpha" || request.Header.Get("Authorization") != "Bearer "+accessToken {
				t.Fatalf("project request = %s authorization=%q", request.URL.RawQuery, request.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(writer, `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"ProjectPage","projects":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Project","metadata":{"uid":"project-alpha","name":"project-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha","state":"active"}}]}`)
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "profile.json")
	previous := cliOpenBrowser
	cliOpenBrowser = func(ctx context.Context, verificationURL string) error {
		if !strings.HasPrefix(verificationURL, server.URL+"/auth/cli/authorize?") || callbackPort == 0 || state == "" {
			t.Fatalf("verification URL/context = %q %d %q", verificationURL, callbackPort, state)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(callbackPort)+cliLoginCallbackPath+"?code="+callbackCode+"&state="+state, nil)
		if err != nil {
			return err
		}
		response, err := loopbackHTTPClient().Do(request)
		if err == nil {
			_ = response.Body.Close()
		}
		return err
	}
	defer func() { cliOpenBrowser = previous }()

	var output bytes.Buffer
	if err := run([]string{"login", "--web-endpoint", server.URL, "--control-plane-endpoint", server.URL, "--ca-file", caFile, "--application", "admin", "--profile", profilePath}, &output); err != nil {
		t.Fatal(err)
	}
	profile, err := readCLIProfile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Credential != grant || profile.DefaultTenantID != "tenant-alpha" || profile.DefaultProjectID != "project-alpha" {
		t.Fatalf("profile = %#v", profile)
	}
	if strings.Contains(output.String(), grant) || strings.Contains(output.String(), accessToken) {
		t.Fatal("login output exposed a credential")
	}
}

func TestConfigureServiceAccountProfileReadsPrivateCredentialFile(t *testing.T) {
	const credential = "SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/auth/automation/tenant-token" || request.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatalf("request = %s %s authorization=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"tenantId":"tenant-alpha","projectId":"project-alpha"}` {
			t.Fatalf("body = %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"accessToken":"aaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbb.ccccccccccccccccccccccc","tokenType":"Bearer","expiresAt":"2026-11-08T10:15:00Z"}`)
	}))
	defer server.Close()

	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	credentialFile := filepath.Join(root, "service-account.credential")
	if err := os.WriteFile(credentialFile, []byte(credential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(root, "automation-profile.json")
	var output bytes.Buffer
	if err := run([]string{
		"profile", "configure-service-account", "--web-endpoint", server.URL,
		"--control-plane-endpoint", server.URL, "--ca-file", caFile,
		"--application", "admin", "--credential-file", credentialFile,
		"--tenant", "tenant-alpha", "--project", "project-alpha", "--profile", profilePath,
	}, &output); err != nil {
		t.Fatal(err)
	}
	profile, err := readCLIProfile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if profile.CredentialKind != "serviceAccount" || profile.Credential != credential || profile.DefaultTenantID != "tenant-alpha" || profile.DefaultProjectID != "project-alpha" {
		t.Fatalf("profile = %#v", profile)
	}
	if strings.Contains(output.String(), credential) {
		t.Fatal("configuration output exposed the credential")
	}
}

func TestValidateCLIContextUsesAdminProjectRoute(t *testing.T) {
	const accessToken = "aaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbb.ccccccccccccccccccccccc"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/auth/automation/tenant-token":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != `{"tenantId":"tenant-alpha","projectId":"project-alpha"}` {
				t.Fatalf("token body = %s", body)
			}
			_, _ = io.WriteString(writer, `{"accessToken":"`+accessToken+`","tokenType":"Bearer","expiresAt":"2026-11-08T10:15:00Z"}`)
		case request.Method == http.MethodGet && request.URL.Path == "/v1/admin/tenants/tenant-alpha/projects/project-alpha":
			if request.Header.Get("Authorization") != "Bearer "+accessToken {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			writer.Header().Set("X-Resource-Version", "1")
			_, _ = io.WriteString(writer, `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Project","metadata":{"uid":"project-alpha","name":"project-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-10-09T10:00:00Z","updatedAt":"2026-10-09T10:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha","state":"active"}}`)
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.String())
		}
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(root, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := cliProfile{
		Version: cliProfileVersion, WebEndpoint: server.URL, ControlPlaneEndpoint: server.URL, CAFile: caFile,
		Application: "admin", CredentialKind: "serviceAccount", Credential: strings.Repeat("S", 43),
		DefaultTenantID: "tenant-alpha", DefaultProjectID: "project-alpha",
	}
	if err := validateCLIContext(context.Background(), profile, "tenant-alpha", "project-alpha"); err != nil {
		t.Fatal(err)
	}
}
