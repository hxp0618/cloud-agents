package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunProjectGetUsesPublicHTTPClient(t *testing.T) {
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

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"--endpoint", server.URL, "--token", "token-alpha", "--tenant", "tenant-alpha", "--project", "project-alpha", "--request-id", "request-alpha",
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
}

func TestParseArgsAcceptsPublicReadResources(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "tenant", args: []string{"tenant", "get"}},
		{name: "organization", args: []string{"--organization", "organization-alpha", "organization", "get"}},
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
