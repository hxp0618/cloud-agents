package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const identityAuthorizationRequestJSON = `{"application":"admin","sessionSha256":"sha256:1111111111111111111111111111111111111111111111111111111111111111","tenantId":"tenant-alpha","projectId":"project-alpha"}`

type identityTenantTokenAuthorizerFake struct {
	mutex      sync.Mutex
	request    api.TenantTokenAuthorizationRequest
	calls      int
	result     api.TenantTokenAuthorization
	err        error
	contextErr error
}

func (fake *identityTenantTokenAuthorizerFake) AuthorizeTenantToken(ctx context.Context, request api.TenantTokenAuthorizationRequest) (api.TenantTokenAuthorization, error) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.calls++
	fake.request = request
	fake.contextErr = ctx.Err()
	if fake.contextErr != nil {
		return api.TenantTokenAuthorization{}, fake.contextErr
	}
	return fake.result, fake.err
}

func (fake *identityTenantTokenAuthorizerFake) snapshot() (int, api.TenantTokenAuthorizationRequest, error) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.calls, fake.request, fake.contextErr
}

func TestIdentityAuthorizationHTTPServerGeneratedClientRoundTrip(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &identityTenantTokenAuthorizerFake{result: validIdentityTenantTokenAuthorization()}
	handler, err := NewIdentityAuthorizationHTTPServer(authorizer, credential)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()
	client, err := api.NewIdentityAuthorizationHTTPClientWithClient(httpServer.URL, credential, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	request := validIdentityTenantTokenAuthorizationRequest()
	result, err := client.AuthorizeTenantToken(context.Background(), "request-authorize", request)
	if err != nil {
		t.Fatal(err)
	}
	if result.UserID != "account-alpha" || result.TenantID != request.TenantID || result.ProjectID != request.ProjectID || result.Application != request.Application {
		t.Fatalf("result = %#v", result)
	}
	calls, captured, _ := authorizer.snapshot()
	if calls != 1 || captured != request {
		t.Fatalf("calls=%d request=%#v", calls, captured)
	}
	wrongCredential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	wrongClient, err := api.NewIdentityAuthorizationHTTPClientWithClient(httpServer.URL, wrongCredential, httpServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongClient.AuthorizeTenantToken(context.Background(), "request-authorize", request); err == nil {
		t.Fatal("wrong service credential was accepted")
	} else if clientErr, ok := err.(*api.ClientError); !ok || clientErr.Status != http.StatusUnauthorized || strings.Contains(err.Error(), wrongCredential) {
		t.Fatalf("wrong credential error = %T %v", err, err)
	}
	if calls, _, _ := authorizer.snapshot(); calls != 1 {
		t.Fatalf("wrong credential reached authorizer: calls=%d", calls)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, identityAuthorizationRequest(t, context.Background(), credential, identityAuthorizationRequestJSON))
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("X-Request-ID") != "request-authorize" || response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if _, err := api.DecodeTenantTokenAuthorizationJSON(response.Body.Bytes()); err != nil {
		t.Fatalf("response contract: %v", err)
	}
}

func TestIdentityAuthorizationHTTPServerRejectsUnauthenticatedAndMalformedRequests(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	wrongCredential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &identityTenantTokenAuthorizerFake{result: validIdentityTenantTokenAuthorization()}
	handler, err := NewIdentityAuthorizationHTTPServer(authorizer, credential)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*http.Request)
		status int
	}{
		{name: "missing credential", mutate: func(request *http.Request) { request.Header.Del("Authorization") }, status: http.StatusUnauthorized},
		{name: "wrong credential", mutate: func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+wrongCredential) }, status: http.StatusUnauthorized},
		{name: "wrong method", mutate: func(request *http.Request) { request.Method = http.MethodGet }, status: http.StatusMethodNotAllowed},
		{name: "wrong path", mutate: func(request *http.Request) { request.URL.Path = "/v1/identity/authorize-tenant-token/other" }, status: http.StatusNotFound},
		{name: "query", mutate: func(request *http.Request) { request.URL.RawQuery = "application=admin" }, status: http.StatusBadRequest},
		{name: "missing request ID", mutate: func(request *http.Request) { request.Header.Del("X-Request-ID") }, status: http.StatusBadRequest},
		{name: "malformed JSON", mutate: func(request *http.Request) { request.Body = body(`{"application":`) }, status: http.StatusBadRequest},
		{name: "authority injection", mutate: func(request *http.Request) {
			request.Body = body(strings.TrimSuffix(identityAuthorizationRequestJSON, "}") + `,"userId":"user-injected","scopes":["projects.get"]}`)
		}, status: http.StatusBadRequest},
		{name: "oversized JSON", mutate: func(request *http.Request) {
			request.Body = body(strings.Repeat(" ", identityAuthorizationMaxBodyBytes+1))
		}, status: http.StatusBadRequest},
		{name: "wrong media type", mutate: func(request *http.Request) { request.Header.Set("Content-Type", "text/plain") }, status: http.StatusUnsupportedMediaType},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := identityAuthorizationRequest(t, context.Background(), credential, identityAuthorizationRequestJSON)
			test.mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
			if strings.Contains(response.Body.String(), credential) || strings.Contains(response.Body.String(), wrongCredential) || strings.Contains(response.Body.String(), "user-injected") {
				t.Fatalf("response leaked caller material: %s", response.Body.String())
			}
		})
	}
	if calls, _, _ := authorizer.snapshot(); calls != 0 {
		t.Fatalf("authorizer calls = %d", calls)
	}
}

func TestIdentityAuthorizationHTTPServerFailsClosedOnDeniedInvalidAndCanceledResults(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		result    api.TenantTokenAuthorization
		err       error
		cancel    bool
		status    int
		forbidden string
	}{
		{name: "denied", err: postgres.ErrTokenAuthorizationDenied, status: http.StatusForbidden},
		{name: "unavailable", err: errors.New("database secret detail"), status: http.StatusServiceUnavailable, forbidden: "database secret detail"},
		{name: "mismatched authority", result: func() api.TenantTokenAuthorization {
			result := validIdentityTenantTokenAuthorization()
			result.TenantID = "tenant-other"
			return result
		}(), status: http.StatusServiceUnavailable, forbidden: "tenant-other"},
		{name: "invalid response", result: func() api.TenantTokenAuthorization {
			result := validIdentityTenantTokenAuthorization()
			result.Scopes = []string{"agents.list", "agents.get"}
			return result
		}(), status: http.StatusServiceUnavailable, forbidden: "agents.list"},
		{name: "canceled", cancel: true, status: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &identityTenantTokenAuthorizerFake{result: test.result, err: test.err}
			handler, err := NewIdentityAuthorizationHTTPServer(authorizer, credential)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, identityAuthorizationRequest(t, ctx, credential, identityAuthorizationRequestJSON))
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
			}
			if test.forbidden != "" && strings.Contains(response.Body.String(), test.forbidden) {
				t.Fatalf("response leaked operation detail: %s", response.Body.String())
			}
			calls, _, contextErr := authorizer.snapshot()
			if calls != 1 || test.cancel && !errors.Is(contextErr, context.Canceled) {
				t.Fatalf("calls=%d contextErr=%v", calls, contextErr)
			}
		})
	}
}

func TestIdentityAuthorizationHTTPServerRejectsInvalidConfiguration(t *testing.T) {
	credential, _, err := browserauth.NewProof()
	if err != nil {
		t.Fatal(err)
	}
	if server, err := NewIdentityAuthorizationHTTPServer(nil, credential); server != nil || !errors.Is(err, ErrInvalidIdentityAuthorizationHTTPServer) {
		t.Fatalf("nil authorizer server=%#v err=%v", server, err)
	}
	if server, err := NewIdentityAuthorizationHTTPServer(&identityTenantTokenAuthorizerFake{}, "not-a-proof"); server != nil || !errors.Is(err, ErrInvalidIdentityAuthorizationHTTPServer) {
		t.Fatalf("invalid credential server=%#v err=%v", server, err)
	}
}

func identityAuthorizationRequest(t *testing.T, ctx context.Context, credential, requestBody string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, IdentityTenantTokenAuthorizationRoute, body(requestBody)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("X-Request-ID", "request-authorize")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func body(value string) io.ReadCloser { return io.NopCloser(strings.NewReader(value)) }

func validIdentityTenantTokenAuthorizationRequest() api.TenantTokenAuthorizationRequest {
	return api.TenantTokenAuthorizationRequest{
		Application:   api.IdentityApplicationAdmin,
		SessionSHA256: "sha256:" + strings.Repeat("1", 64),
		TenantID:      "tenant-alpha", ProjectID: "project-alpha",
	}
}

func validIdentityTenantTokenAuthorization() api.TenantTokenAuthorization {
	return api.TenantTokenAuthorization{
		UserID: "account-alpha", Issuer: "https://identity.example.com/",
		TenantID: "tenant-alpha", ProjectID: "project-alpha",
		Application: api.IdentityApplicationAdmin, Scopes: []string{"agents.get", "agents.list"},
	}
}
