package v1alpha1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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

func TestHTTPClientRejectsUnsafeConfigAndRedirects(t *testing.T) {
	for _, value := range []struct{ base, token string }{
		{"", "token"}, {"ftp://example.com", "token"}, {"https://user@example.com", "token"}, {"https://example.com?x=1", "token"}, {"https://example.com", " token"},
	} {
		if client, err := NewHTTPClient(value.base, value.token); client != nil || err == nil {
			t.Fatalf("unsafe config accepted: %#v", value)
		}
	}
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
}
