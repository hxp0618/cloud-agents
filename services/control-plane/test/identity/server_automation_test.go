package identity_test

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/browserauth"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/identity"
)

type automationHTTPStore struct {
	*store
	application api.IdentityApplication
	digest      [sha256.Size]byte
	request     api.TenantTokenIssueRequest
	result      api.TenantToken
	automation  int
	expected    [sha256.Size]byte
}

func (backend *automationHTTPStore) IssueAutomationTenantToken(_ context.Context, application api.IdentityApplication, digest [sha256.Size]byte, request api.TenantTokenIssueRequest) (api.TenantToken, error) {
	backend.automation++
	backend.application, backend.digest, backend.request = application, digest, request
	if backend.expected != ([sha256.Size]byte{}) && digest != backend.expected {
		return api.TenantToken{}, browserauth.ErrUnauthorized
	}
	return backend.result, backend.err
}

func TestIdentityAutomationHTTPBoundaryUsesServiceApplicationAndPrivateCredential(t *testing.T) {
	proof := func() string {
		value, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	credentials := identity.ServiceCredentials{AdminWeb: proof(), UserWeb: proof(), ControlPlane: proof()}
	automationCredential := proof()
	wantDigest, _ := browserauth.ProofDigest(automationCredential)
	backend := &automationHTTPStore{store: &store{}, expected: wantDigest, result: api.TenantToken{
		AccessToken: strings.Repeat("a", 22) + "." + strings.Repeat("b", 22) + "." + strings.Repeat("c", 22),
		TokenType:   "Bearer", ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}}
	handler, err := identity.NewServer(backend, credentials)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(handler)
	defer upstream.Close()
	client, err := api.NewIdentityServiceHTTPClientWithClient(upstream.URL, credentials.AdminWeb, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.IssueAutomationTenantToken(context.Background(), "request-automation", automationCredential, api.TenantTokenIssueRequest{TenantID: "tenant-one", ProjectID: "project-one"})
	if err != nil || result.AccessToken == "" || backend.automation != 1 || backend.application != api.IdentityApplicationAdmin ||
		backend.digest != wantDigest || backend.request != (api.TenantTokenIssueRequest{TenantID: "tenant-one", ProjectID: "project-one"}) {
		t.Fatalf("result=%#v error=%v backend=%#v", result, err, backend)
	}

	wrong := httptest.NewRequest(http.MethodPost, "/v1/identity/automation/tenant-token", strings.NewReader(`{"tenantId":"tenant-one"}`))
	wrong.Header.Set("Authorization", "Bearer "+credentials.AdminWeb)
	wrong.Header.Set("Content-Type", "application/json")
	wrong.Header.Set("X-Request-ID", "request-wrong")
	wrong.Header.Set("X-Cloud-Agents-Automation-Credential", proof())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, wrong)
	if response.Code != http.StatusUnauthorized || backend.automation != 2 || strings.Contains(response.Body.String(), automationCredential) {
		t.Fatalf("wrong credential status=%d calls=%d body=%s", response.Code, backend.automation, response.Body.String())
	}
}

func TestIdentityAutomationHTTPBoundaryRejectsControlPlaneAndMalformedRequests(t *testing.T) {
	proof := func() string {
		value, _, err := browserauth.NewProof()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	credentials := identity.ServiceCredentials{AdminWeb: proof(), UserWeb: proof(), ControlPlane: proof()}
	backend := &automationHTTPStore{store: &store{}}
	handler, err := identity.NewServer(backend, credentials)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, serviceCredential, automationCredential, body string
		method                                              string
		want                                                int
	}{
		{name: "control plane service", serviceCredential: credentials.ControlPlane, automationCredential: proof(), body: `{"tenantId":"tenant-one"}`, method: http.MethodPost, want: http.StatusForbidden},
		{name: "missing automation credential", serviceCredential: credentials.UserWeb, body: `{"tenantId":"tenant-one"}`, method: http.MethodPost, want: http.StatusUnauthorized},
		{name: "malformed body", serviceCredential: credentials.UserWeb, automationCredential: proof(), body: `{"tenantId":"tenant-one","application":"admin"}`, method: http.MethodPost, want: http.StatusBadRequest},
		{name: "wrong method", serviceCredential: credentials.UserWeb, automationCredential: proof(), method: http.MethodGet, want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/v1/identity/automation/tenant-token", strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+test.serviceCredential)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Request-ID", "request-automation")
			if test.automationCredential != "" {
				request.Header.Set("X-Cloud-Agents-Automation-Credential", test.automationCredential)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want || backend.automation != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, backend.automation, response.Body.String())
			}
		})
	}
}
