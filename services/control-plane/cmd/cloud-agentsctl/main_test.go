package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunProjectGetUsesTokenFile(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotAuthorization = request.Header.Get("Authorization")
		if request.Method != http.MethodGet || request.URL.Path != "/v1/tenants/tenant-alpha/projects/project-alpha" || request.Header.Get("X-Request-ID") != "request-alpha" {
			t.Fatalf("request = %s %s headers=%v", request.Method, request.URL.Path, request.Header)
		}
		writer.Header().Set("X-Resource-Version", "3")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Project","metadata":{"uid":"project-alpha","name":"project-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"3","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha","state":"active"}}`))
	}))
	defer server.Close()
	tokenFile := t.TempDir() + "/token"
	if err := os.WriteFile(tokenFile, []byte("token-alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"--endpoint", server.URL, "--token-file", tokenFile, "--tenant", "tenant-alpha", "--project", "project-alpha", "--request-id", "request-alpha",
		"project", "get",
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuthorization != "Bearer token-alpha" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
	if !strings.Contains(stdout.String(), `"kind":"Project"`) || stderr.Len() != 0 {
		t.Fatalf("stdout/stderr = %q / %q", stdout.String(), stderr.String())
	}
}

func TestParseArgsRejectsMissingRequiredGlobalInput(t *testing.T) {
	if _, _, _, _, err := parseArgs([]string{"--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha", "project", "get"}); err == nil || !strings.Contains(err.Error(), "--endpoint is required") {
		t.Fatalf("error = %v", err)
	}
	base := []string{"--endpoint", "https://control-plane.example", "--tenant", "tenant-alpha", "--request-id", "request-alpha", "tenant", "get"}
	if _, _, _, _, err := parseArgs(base); err == nil || !strings.Contains(err.Error(), "--token or --token-file") {
		t.Fatalf("missing token error = %v", err)
	}
	if _, _, _, _, err := parseArgs(append([]string{"--token", "token-alpha", "--token-file", "/tmp/token"}, base...)); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("conflicting token error = %v", err)
	}
}

func TestRunRejectsOversizedTokenFile(t *testing.T) {
	tokenFile := t.TempDir() + "/token"
	if err := os.WriteFile(tokenFile, bytes.Repeat([]byte("x"), maxBearerTokenFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"--endpoint", "https://control-plane.example", "--token-file", tokenFile, "--tenant", "tenant-alpha", "--request-id", "request-alpha", "tenant", "get"}, io.Discard)
	if err == nil || err.Error() != "cannot read bearer token file" {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCancelsRequestAtConfiguredTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	started := time.Now()
	err := run([]string{"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha", "--timeout", "25ms", "tenant", "get"}, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request timeout took %v", elapsed)
	}
	if _, _, _, _, err := parseArgs([]string{"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha", "--timeout", "0s", "tenant", "get"}); err == nil {
		t.Fatal("zero timeout was accepted")
	}
}

func TestParseArgsAcceptsPublicReadResources(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "tenant", args: []string{"tenant", "get"}},
		{name: "organization", args: []string{"--organization", "organization-alpha", "organization", "get"}},
		{name: "organization list", args: []string{"organization", "list"}},
		{name: "project", args: []string{"--project", "project-alpha", "project", "get"}},
		{name: "membership", args: []string{"--membership", "membership-alpha", "membership", "get"}},
		{name: "role", args: []string{"--role", "role-alpha", "role", "get"}},
		{name: "role binding", args: []string{"--role-binding", "binding-alpha", "role-binding", "get"}},
		{name: "managed host project", args: []string{"--project", "project-alpha", "managed-host-project", "get"}},
		{name: "managed host role binding", args: []string{"--role-binding", "binding-alpha", "managed-host-role-binding", "get"}},
		{name: "environment lease", args: []string{"--project", "project-alpha", "--lease", "lease-alpha", "environment-lease", "get"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--endpoint", "http://127.0.0.1:8080", "--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha"}, test.args...)
			if _, _, _, _, err := parseArgs(args); err != nil {
				t.Fatalf("parseArgs(%v) = %v", args, err)
			}
		})
	}
}

func TestParseArgsAcceptsRBACMutations(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "organization create", args: []string{"--organization", "organization-beta", "organization", "create"}},
		{name: "membership create", args: []string{"--membership", "membership-alpha", "membership", "create"}},
		{name: "membership suspend", args: []string{"--membership", "membership-alpha", "membership", "suspend"}},
		{name: "membership revoke", args: []string{"--membership", "membership-alpha", "membership", "revoke"}},
		{name: "role binding create", args: []string{"--role-binding", "binding-alpha", "role-binding", "create"}},
		{name: "role binding revoke", args: []string{"--role-binding", "binding-alpha", "role-binding", "revoke"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"--endpoint", "http://127.0.0.1:8080", "--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha"}, test.args...)
			if _, _, _, _, err := parseArgs(args); err != nil {
				t.Fatalf("parseArgs(%v) = %v", args, err)
			}
		})
	}
}

func TestRunOrganizationCreate(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/tenants/tenant-alpha/organizations" || request.Header.Get("X-Request-ID") != "request-alpha" {
			t.Fatalf("request = %s %s headers=%v", request.Method, request.URL.Path, request.Header)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		gotBody = string(body)
		writer.Header().Set("X-Resource-Version", "5")
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Organization","metadata":{"uid":"organization-beta","name":"organization-beta","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"5","createdAt":"2026-08-30T08:00:00Z","updatedAt":"2026-08-30T08:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"displayName":"Organization Beta","state":"active"}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run([]string{
		"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--organization", "organization-beta", "--request-id", "request-alpha",
		"organization", "create", "--expected-tenant-revision", "4", "--name", "organization-beta", "--display-name", "Organization Beta", "--audit-fact-uid", "audit-organization-beta", "--reason-code", "operator-request",
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"expectedTenantRevision":4`, `"organizationId":"organization-beta"`, `"name":"organization-beta"`, `"auditFactUid":"audit-organization-beta"`} {
		if !strings.Contains(gotBody, part) {
			t.Fatalf("body %q does not contain %q", gotBody, part)
		}
	}
	if !strings.Contains(stdout.String(), `"kind":"Organization"`) {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunOrganizationList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/tenants/tenant-alpha/organizations" || request.URL.Query().Get("pageSize") != "1" || request.URL.Query().Get("pageToken") != "organization-page-token-1" {
			t.Fatalf("request = %s %s", request.Method, request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"OrganizationPage","organizations":[{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Organization","metadata":{"uid":"organization-alpha","name":"organization-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"2","createdAt":"2026-08-30T08:00:00Z","updatedAt":"2026-08-30T08:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"displayName":"Organization Alpha","state":"active"}}]}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := run([]string{
		"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--request-id", "request-alpha",
		"organization", "list", "--page-size", "1", "--page-token", "organization-page-token-1",
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"kind":"OrganizationPage"`) {
		t.Fatalf("output = %q", stdout.String())
	}
}

func TestRunRBACMutations(t *testing.T) {
	const base = "--endpoint PLACEHOLDER --token token-alpha --tenant tenant-alpha --request-id request-alpha"
	tests := []struct {
		name       string
		resource   string
		action     string
		resourceID string
		status     int
		bodyParts  []string
	}{
		{
			name:       "membership create",
			resource:   "membership",
			action:     "create",
			resourceID: "membership-alpha",
			status:     http.StatusCreated,
			bodyParts: []string{
				`"expectedTenantRevision":7`, `"membershipId":"membership-alpha"`, `"membershipName":"membership-alpha"`,
				`"kind":"user"`, `"issuer":"https://issuer.example"`, `"subject":"user-alpha"`,
				`"level":"tenant"`, `"id":"tenant-alpha"`, `"auditFactUid":"audit-create"`, `"reasonCode":"operator-request"`,
			},
		},
		{
			name:       "membership suspend",
			resource:   "membership",
			action:     "suspend",
			resourceID: "membership-alpha",
			status:     http.StatusOK,
			bodyParts:  []string{`"expectedTenantRevision":8`, `"expectedResourceVersion":3`, `"auditFactUid":"audit-transition"`, `"reasonCode":"operator-request"`},
		},
		{
			name:       "membership revoke",
			resource:   "membership",
			action:     "revoke",
			resourceID: "membership-alpha",
			status:     http.StatusOK,
			bodyParts:  []string{`"expectedTenantRevision":8`, `"expectedResourceVersion":3`, `"auditFactUid":"audit-transition"`, `"reasonCode":"operator-request"`},
		},
		{
			name:       "role binding create",
			resource:   "role-binding",
			action:     "create",
			resourceID: "binding-alpha",
			status:     http.StatusCreated,
			bodyParts: []string{
				`"expectedTenantRevision":9`, `"roleBindingId":"binding-alpha"`, `"roleBindingName":"binding-alpha"`,
				`"roleName":"project.viewer"`, `"roleVersion":1`, `"level":"project"`, `"id":"project-alpha"`,
				`"auditFactUid":"audit-bind"`, `"reasonCode":"operator-request"`,
			},
		},
		{
			name:       "role binding revoke",
			resource:   "role-binding",
			action:     "revoke",
			resourceID: "binding-alpha",
			status:     http.StatusOK,
			bodyParts:  []string{`"expectedTenantRevision":9`, `"expectedResourceVersion":4`, `"auditFactUid":"audit-revoke"`, `"reasonCode":"operator-request"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotPath, gotBody string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				gotPath = request.URL.Path
				if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer token-alpha" || request.Header.Get("X-Request-ID") != "request-alpha" || request.Header.Get("Idempotency-Key") != "" {
					t.Fatalf("request = %s %s headers=%v", request.Method, request.URL.Path, request.Header)
				}
				contents, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatal(err)
				}
				gotBody = string(contents)
				writer.Header().Set("X-Resource-Version", "4")
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(test.status)
				state := "active"
				if test.action == "suspend" {
					state = "suspended"
				} else if test.action == "revoke" {
					state = "revoked"
				}
				_, _ = writer.Write([]byte(`{"resourceUid":"` + test.resourceID + `","resourceVersion":"4","state":"` + state + `"}`))
			}))
			defer server.Close()

			args := strings.Fields(strings.Replace(base, "PLACEHOLDER", server.URL, 1))
			if test.resource == "membership" {
				args = append(args, "--membership", test.resourceID)
			} else {
				args = append(args, "--role-binding", test.resourceID)
			}
			// Resource flags are global and must precede the command for the Go flag package.
			args = append(args, test.resource, test.action)
			if test.action == "create" && test.resource == "membership" {
				args = append(args, "--expected-tenant-revision", "7", "--name", "membership-alpha", "--subject-kind", "user", "--subject-issuer", "https://issuer.example", "--subject", "user-alpha", "--scope-level", "tenant", "--scope-id", "tenant-alpha", "--audit-fact-uid", "audit-create", "--reason-code", "operator-request")
			} else if test.action == "create" {
				args = append(args, "--expected-tenant-revision", "9", "--name", "binding-alpha", "--subject-kind", "user", "--subject-issuer", "https://issuer.example", "--subject", "user-alpha", "--role-name", "project.viewer", "--role-version", "1", "--scope-level", "project", "--scope-id", "project-alpha", "--audit-fact-uid", "audit-bind", "--reason-code", "operator-request")
			} else if test.resource == "membership" {
				args = append(args, "--expected-tenant-revision", "8", "--expected-resource-version", "3", "--audit-fact-uid", "audit-transition", "--reason-code", "operator-request")
			} else {
				args = append(args, "--expected-tenant-revision", "9", "--expected-resource-version", "4", "--audit-fact-uid", "audit-revoke", "--reason-code", "operator-request")
			}
			var stdout bytes.Buffer
			if err := run(args, &stdout); err != nil {
				t.Fatal(err)
			}
			wantPath := "/v1/tenants/tenant-alpha/"
			if test.resource == "membership" {
				wantPath += "memberships"
			} else {
				wantPath += "role-bindings"
			}
			if test.action == "create" {
				if gotPath != wantPath {
					t.Fatalf("path = %q, want %q", gotPath, wantPath)
				}
			} else if !strings.HasPrefix(gotPath, wantPath+"/"+test.resourceID+":") || !strings.HasSuffix(gotPath, ":"+test.action) {
				t.Fatalf("path = %q", gotPath)
			}
			for _, part := range test.bodyParts {
				if !strings.Contains(gotBody, part) {
					t.Fatalf("body %q does not contain %q", gotBody, part)
				}
			}
			if !strings.Contains(stdout.String(), `"resourceUid":"`+test.resourceID+`"`) {
				t.Fatalf("output = %q", stdout.String())
			}
		})
	}
}

func TestRunEnvironmentLeaseLifecycle(t *testing.T) {
	const releaseDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	const responseBody = `{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"CloudEnvironmentLease","metadata":{"uid":"lease-alpha","name":"lease-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:00:00Z"},"spec":{"projectRef":{"namespace":"cloud-agents","kind":"project","id":"project-alpha"},"generation":1,"desiredPhase":"active","observedPhase":"provisioning","cleanupPhase":"none","environmentId":"lease-alpha","releaseDigest":"` + releaseDigest + `","expiresAt":"2026-08-29T09:00:00Z"}}`
	tests := []struct {
		name        string
		globalArgs  []string
		actionArgs  []string
		method      string
		path        string
		status      int
		idempotency string
		bodyParts   []string
	}{
		{
			name:        "create",
			globalArgs:  []string{"--idempotency-key", "lease-create-key-1234"},
			actionArgs:  []string{"environment-lease", "create", "--name", "lease-alpha", "--release-digest", releaseDigest, "--ttl-seconds", "3600"},
			method:      http.MethodPost,
			path:        "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases",
			status:      http.StatusCreated,
			idempotency: "lease-create-key-1234",
			bodyParts:   []string{`"leaseId":"lease-alpha"`, `"leaseName":"lease-alpha"`, `"releaseDigest":"` + releaseDigest + `"`, `"ttlSeconds":3600`},
		},
		{
			name:       "get",
			actionArgs: []string{"environment-lease", "get"},
			method:     http.MethodGet,
			path:       "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha",
			status:     http.StatusOK,
		},
		{
			name:        "terminate",
			globalArgs:  []string{"--idempotency-key", "lease-terminate-key"},
			actionArgs:  []string{"environment-lease", "terminate", "--generation", "1"},
			method:      http.MethodPost,
			path:        "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:terminate",
			status:      http.StatusOK,
			idempotency: "lease-terminate-key",
			bodyParts:   []string{`"expectedGeneration":1`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotBody string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != test.method || request.URL.Path != test.path || request.Header.Get("Authorization") != "Bearer token-alpha" || request.Header.Get("X-Request-ID") != "request-alpha" || request.Header.Get("Idempotency-Key") != test.idempotency {
					t.Fatalf("request = %s %s headers=%v", request.Method, request.URL.Path, request.Header)
				}
				contents, _ := io.ReadAll(request.Body)
				gotBody = string(contents)
				writer.Header().Set("X-Resource-Version", "1")
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(responseBody))
			}))
			defer server.Close()

			args := append([]string{"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--project", "project-alpha", "--lease", "lease-alpha", "--request-id", "request-alpha"}, test.globalArgs...)
			args = append(args, test.actionArgs...)
			var stdout bytes.Buffer
			if err := run(args, &stdout); err != nil {
				t.Fatal(err)
			}
			for _, part := range test.bodyParts {
				if !strings.Contains(gotBody, part) {
					t.Fatalf("body %q does not contain %q", gotBody, part)
				}
			}
			if !strings.Contains(stdout.String(), `"kind":"CloudEnvironmentLease"`) {
				t.Fatalf("output = %q", stdout.String())
			}
		})
	}
}

func TestRunExecutionMutationUsesGenerationAndIdempotency(t *testing.T) {
	for _, action := range []string{"cancel", "interrupt"} {
		t.Run(action, func(t *testing.T) {
			var method, path, body string
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				method, path = request.Method, request.URL.Path
				contents, _ := io.ReadAll(request.Body)
				body = string(contents)
				writer.Header().Set("X-Resource-Version", "2")
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"apiVersion":"managed-agent.cloud-agents.dev/v1alpha1","kind":"Execution","metadata":{"uid":"execution-alpha","projectId":"project-alpha","sessionId":"session-alpha","turnId":"turn-alpha","resourceVersion":"2","createdAt":"2026-08-29T08:00:00Z","updatedAt":"2026-08-29T08:01:00Z"},"spec":{"generation":7,"state":"cancelled"}}`))
			}))
			defer server.Close()
			args := []string{"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--project", "project-alpha", "--session", "session-alpha", "--turn", "turn-alpha", "--execution", "execution-alpha", "--request-id", "request-alpha", "--idempotency-key", "idempotency-alpha", "execution", action, "--generation", "7"}
			var stdout bytes.Buffer
			if err := run(args, &stdout); err != nil {
				t.Fatal(err)
			}
			wantSuffix := ":" + action
			if method != http.MethodPost || !strings.HasSuffix(path, wantSuffix) || !strings.Contains(body, `"generation":7`) || !strings.Contains(stdout.String(), `"state":"cancelled"`) {
				t.Fatalf("request=%s %s body=%q output=%q", method, path, body, stdout.String())
			}
		})
	}
}
