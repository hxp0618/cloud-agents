package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type tenantHTTPReaderFake struct {
	tenant postgres.PlatformTenant
	err    error
	calls  int
}

func (fake *tenantHTTPReaderFake) GetPlatformTenant(_ context.Context, _ *authn.VerifiedPrincipal, _ string) (postgres.PlatformTenant, error) {
	fake.calls++
	return fake.tenant, fake.err
}

func TestPlatformTenantHTTPServerReturnsGeneratedResource(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &tenantHTTPReaderFake{tenant: postgres.PlatformTenant{
		TenantID: "tenant-alpha", TenantUID: "tenant-alpha", TenantName: "tenant-name",
		DisplayName: "Tenant Alpha", State: "active", ResourceVersion: 4, CreatedAt: now, UpdatedAt: now,
	}}
	server, err := NewPlatformTenantHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") != "request-alpha" || response.Header().Get("X-Resource-Version") != "4" || reader.calls != 1 {
		t.Fatalf("status=%d headers=%v calls=%d body=%s", response.Code, response.Header(), reader.calls, response.Body.String())
	}
	value, err := platformv1alpha1.DecodePlatformTenantResponseJSON(response.Body.Bytes())
	if err != nil {
		t.Fatalf("response is not the generated tenant contract: %v", err)
	}
	if value.Value.Metadata.Name != "tenant-alpha" || value.Value.Spec.DisplayName != "Tenant Alpha" {
		t.Fatalf("tenant response = %#v", value.Value)
	}
	if verifier.seen.TenantID != "tenant-alpha" || verifier.seen.ResourceID != "tenant-alpha" || verifier.seen.RequiredPermission != "tenants.get" || verifier.seen.ResourceLevel != "tenant" {
		t.Fatalf("verification request = %#v", verifier.seen)
	}
}

func TestPlatformTenantHTTPServerMapsNotFoundAndRejectsDuplicateAuthorization(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &tenantHTTPReaderFake{err: postgres.ErrPlatformTenantNotFound}
	server, err := NewPlatformTenantHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha", nil)
	request.Header.Set("Authorization", "Bearer one")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || reader.calls != 1 {
		t.Fatalf("not found status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}

	duplicate := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha", nil)
	duplicate.Header.Add("Authorization", "Bearer one")
	duplicate.Header.Add("Authorization", "Bearer two")
	duplicate.Header.Set("X-Request-ID", "request-alpha")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, duplicate)
	if response.Code != http.StatusUnauthorized || verifier.calls != 1 || reader.calls != 1 {
		t.Fatalf("duplicate status=%d verifier=%d reader=%d body=%s", response.Code, verifier.calls, reader.calls, response.Body.String())
	}
}

func TestPlatformTenantHTTPServerRequiresConfiguredDependencies(t *testing.T) {
	if server, err := NewPlatformTenantHTTPServer(nil, &tenantHTTPReaderFake{}); server != nil || !errors.Is(err, ErrNilManagedAgentTenantReadService) {
		t.Fatalf("nil verifier result=%v err=%v", server, err)
	}
	if server, err := NewPlatformTenantHTTPServer(&projectHTTPVerifierFake{}, nil); server != nil || !errors.Is(err, ErrNilManagedAgentTenantReadService) {
		t.Fatalf("nil reader result=%v err=%v", server, err)
	}
}
