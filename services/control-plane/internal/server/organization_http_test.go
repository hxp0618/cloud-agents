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

type organizationHTTPReaderFake struct {
	organization postgres.Organization
	err          error
	calls        int
}

func (fake *organizationHTTPReaderFake) GetOrganization(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Organization, error) {
	fake.calls++
	return fake.organization, fake.err
}

func TestOrganizationHTTPServerReturnsGeneratedResource(t *testing.T) {
	now := time.Date(2026, time.August, 28, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &organizationHTTPReaderFake{organization: postgres.Organization{
		UID: "organization-alpha", Name: "organization-alpha", TenantID: "tenant-alpha",
		DisplayName: "Organization Alpha", State: "active", ResourceVersion: 5, CreatedAt: now, UpdatedAt: now,
	}}
	server, err := NewOrganizationHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/organizations/organization-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") != "request-alpha" || response.Header().Get("X-Resource-Version") != "5" || reader.calls != 1 {
		t.Fatalf("status=%d headers=%v calls=%d body=%s", response.Code, response.Header(), reader.calls, response.Body.String())
	}
	value, err := platformv1alpha1.DecodeOrganizationResponseJSON(response.Body.Bytes())
	if err != nil {
		t.Fatalf("response is not the generated organization contract: %v", err)
	}
	if value.Value.Metadata.Name != "organization-alpha" || value.Value.Spec.TenantRef.ID != "tenant-alpha" {
		t.Fatalf("organization response = %#v", value.Value)
	}
	if verifier.seen.TenantID != "tenant-alpha" || verifier.seen.ResourceID != "organization-alpha" || verifier.seen.RequiredPermission != "organizations.get" || verifier.seen.ResourceLevel != "organization" {
		t.Fatalf("verification request = %#v", verifier.seen)
	}
}

func TestOrganizationHTTPServerMapsNotFoundAndRequiresDependencies(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &organizationHTTPReaderFake{err: postgres.ErrOrganizationNotFound}
	server, err := NewOrganizationHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/organizations/organization-alpha", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || reader.calls != 1 {
		t.Fatalf("not found status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}
	if server, err := NewOrganizationHTTPServer(nil, reader); server != nil || !errors.Is(err, ErrNilManagedAgentOrganizationReadService) {
		t.Fatalf("nil verifier result=%v err=%v", server, err)
	}
}
