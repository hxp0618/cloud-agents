package main

import (
	"bytes"
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
