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

type roleHTTPReaderFake struct {
	role  postgres.Role
	err   error
	calls int
}

func (fake *roleHTTPReaderFake) GetRole(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Role, error) {
	fake.calls++
	return fake.role, fake.err
}

func TestRoleHTTPServerReturnsGeneratedResource(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &roleHTTPReaderFake{role: postgres.Role{
		UID: "role-project-viewer-v1", Name: "role-project-viewer-v1", TenantID: "tenant-alpha",
		RoleName: "project.viewer", Version: 1, Permissions: []string{"projects.get", "projects.list"},
		State: "active", ResourceVersion: 5, CreatedAt: now,
	}}
	server, err := NewRoleHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, RoleRoute, nil)
	request.URL.Path = "/v1/tenants/tenant-alpha/roles/role-project-viewer-v1"
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-Request-ID") != "request-alpha" || response.Header().Get("X-Resource-Version") != "5" || reader.calls != 1 {
		t.Fatalf("status=%d headers=%v calls=%d body=%s", response.Code, response.Header(), reader.calls, response.Body.String())
	}
	value, err := platformv1alpha1.DecodeRoleResponseJSON(response.Body.Bytes())
	if err != nil {
		t.Fatalf("response is not the generated role contract: %v", err)
	}
	if value.Value.Metadata.Name != "role-project-viewer-v1" || value.Value.Spec.Name != "project.viewer" || len(value.Value.Spec.Permissions) != 2 {
		t.Fatalf("role response = %#v", value.Value)
	}
	if verifier.seen.TenantID != "tenant-alpha" || verifier.seen.ResourceID != "tenant-alpha" || verifier.seen.RequiredPermission != "roles.get" || verifier.seen.ResourceLevel != "tenant" {
		t.Fatalf("verification request = %#v", verifier.seen)
	}
}

func TestRoleHTTPServerMapsNotFoundAndRequiresDependencies(t *testing.T) {
	verifier := &projectHTTPVerifierFake{}
	reader := &roleHTTPReaderFake{err: postgres.ErrRoleNotFound}
	server, err := NewRoleHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/roles/missing", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-alpha")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || reader.calls != 1 {
		t.Fatalf("not found status=%d calls=%d body=%s", response.Code, reader.calls, response.Body.String())
	}
	if server, err := NewRoleHTTPServer(nil, reader); server != nil || !errors.Is(err, ErrInvalidRoleHTTPServer) {
		t.Fatalf("nil verifier result=%v err=%v", server, err)
	}
}
