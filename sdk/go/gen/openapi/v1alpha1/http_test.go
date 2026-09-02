package v1alpha1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
)

func TestHTTPClientUsesBearerAndExistingGeneratedOperation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/tenants/tenant-alpha/projects/project-alpha" || request.Header.Get("Authorization") != "Bearer token-alpha" {
			t.Fatalf("request = %s %s auth=%q", request.Method, request.URL.Path, request.Header.Get("Authorization"))
		}
		writer.Header().Set(HeaderResourceVersion, "3")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"Project","metadata":{"uid":"project-alpha","name":"project-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"3","createdAt":"2026-08-28T00:00:00Z"},"spec":{"tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha","state":"active"}}`))
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token-alpha")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GetProject(context.Background(), "tenant-alpha", "project-alpha", "request-alpha")
	if err != nil || result.Value.Spec.DisplayName != "Project Alpha" || result.Value.Metadata.ResourceVersion != "3" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestHTTPClientUsesProvidedHTTPClient(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set(HeaderResourceVersion, "1")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"apiVersion":"platform.cloud-agents.dev/v1alpha1","kind":"PlatformTenant","metadata":{"uid":"tenant-alpha","name":"tenant-alpha","tenantRef":{"namespace":"cloud-agents","kind":"tenant","id":"tenant-alpha"},"resourceVersion":"1","createdAt":"2026-08-31T00:00:00Z"},"spec":{"displayName":"Tenant Alpha","state":"active"}}`))
	}))
	defer server.Close()

	client, err := NewHTTPClientWithClient(server.URL, "token-alpha", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPlatformTenant(context.Background(), "tenant-alpha", "request-alpha"); err != nil {
		t.Fatal(err)
	}
	if client, err := NewHTTPClientWithClient(server.URL, "token-alpha", nil); client != nil || err == nil {
		t.Fatal("nil HTTP client was accepted")
	}
}

func TestHTTPClientSendsJSONContentTypeForRequestBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("method=%s content-type=%q", request.Method, request.Header.Get("Content-Type"))
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token-alpha")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.roundTrip(context.Background(), Request{Method: http.MethodPost, Path: "/resource", Body: []byte(`{}`)})
	if err != nil || response.Status != http.StatusNoContent {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestHTTPClientRejectsUnsafeConfigAndRedirects(t *testing.T) {
	for _, value := range []struct{ base, token string }{
		{"", "token"}, {"ftp://example.com", "token"}, {"http://example.com", "token"}, {"http://localhost", "token"}, {"https://user@example.com", "token"}, {"https://example.com?x=1", "token"}, {"https://example.com", " token"},
	} {
		if client, err := NewHTTPClient(value.base, value.token); client != nil || err == nil {
			t.Fatalf("unsafe config accepted: %#v", value)
		}
	}
	if client, err := NewHTTPClient("https://example.com", "token"); client == nil || err != nil {
		t.Fatalf("HTTPS endpoint rejected: client=%v err=%v", client, err)
	}
	redirectFollowed := false
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		redirectFollowed = true
		writer.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, redirectTarget.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client, err := NewHTTPClient(redirect.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPlatformTenant(context.Background(), "tenant-alpha", "request-alpha"); err == nil {
		t.Fatal("redirect response unexpectedly decoded as success")
	}
	if redirectFollowed {
		t.Fatal("default HTTP client followed a credentialed redirect")
	}
	client, err = NewHTTPClientWithClient(redirect.URL, "token", &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPlatformTenant(context.Background(), "tenant-alpha", "request-alpha"); err == nil || redirectFollowed {
		t.Fatal("provided HTTP client followed a credentialed redirect")
	}
}

func TestHTTPClientAcceptsMaximumRuntimeResultResponse(t *testing.T) {
	message := runtimeprotocol.Message{RequestID: "request-alpha", Protocol: runtimeprotocol.Protocol{Major: 2, Minor: 3}, ExecutionID: "execution-alpha", Generation: 1, CommandID: "command-alpha", OccurredAt: "2026-08-29T08:01:00Z", MessageType: "Result", Payload: map[string]any{"text": ""}}
	base, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	message.Payload["text"] = strings.Repeat("x", runtimeprotocol.MaxMessageBytes-len(base))
	encoded, err := json.Marshal(message)
	if err != nil || len(encoded) != runtimeprotocol.MaxMessageBytes {
		t.Fatalf("message bytes=%d err=%v", len(encoded), err)
	}
	var sdkMessage ManagedAgentExecutionMessage
	if err := json.Unmarshal(encoded, &sdkMessage); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(ManagedAgentExecution{
		APIVersion: "managed-agent.cloud-agents.dev/v1alpha1", Kind: "Execution",
		Metadata: ManagedAgentExecutionMetadata{UID: "execution-alpha", ProjectID: "project-alpha", SessionID: "session-alpha", TurnID: "turn-alpha", ResourceVersion: "3", CreatedAt: "2026-08-29T08:00:00Z", UpdatedAt: "2026-08-29T08:01:00Z"},
		Spec:     ManagedAgentExecutionSpec{Generation: 1, State: "succeeded"}, Messages: []ManagedAgentExecutionMessage{sdkMessage},
	})
	if err != nil || len(body) <= runtimeprotocol.MaxMessageBytes || len(body) > maxHTTPJSONResponseBytes {
		t.Fatalf("response bytes=%d err=%v", len(body), err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set(HeaderResourceVersion, "3")
		_, _ = writer.Write(body)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.GetManagedAgentExecution(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha")
	if err != nil || len(result.Value.Messages) != 1 {
		t.Fatalf("messages=%d err=%v", len(result.Value.Messages), err)
	}
}

func TestHTTPClientRejectsResponseAboveLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(strings.Repeat("x", maxHTTPJSONResponseBytes+1)))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPlatformTenant(context.Background(), "tenant-alpha", "request-alpha"); err == nil || !strings.Contains(err.Error(), "exceeds the SDK limit") {
		t.Fatalf("oversized response error=%v", err)
	}
}

func TestHTTPClientUsesArtifactLimitOnlyForSuccessfulDownloads(t *testing.T) {
	artifact := strings.Repeat("x", maxHTTPJSONResponseBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Content-Disposition", "attachment; filename=artifact.bin")
		_, _ = writer.Write([]byte(artifact))
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha", 0)
	if err != nil || len(result.Data) != len(artifact) {
		t.Fatalf("artifact bytes=%d err=%v", len(result.Data), err)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(artifact))
	}))
	defer errorServer.Close()
	errorClient, err := NewHTTPClient(errorServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := errorClient.DownloadManagedAgentArtifact(context.Background(), "tenant-alpha", "project-alpha", "session-alpha", "turn-alpha", "execution-alpha", "request-alpha", 0); err == nil || !strings.Contains(err.Error(), "exceeds the SDK limit") {
		t.Fatalf("oversized Artifact error response err=%v", err)
	}
}
