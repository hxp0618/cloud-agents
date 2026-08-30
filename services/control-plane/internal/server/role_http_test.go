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
	role         postgres.Role
	page         postgres.RolePage
	err          error
	calls        int
	listCalls    int
	afterName    string
	afterVersion int64
	limit        int
}

func (fake *roleHTTPReaderFake) GetRole(_ context.Context, _ string, _ *authn.VerifiedPrincipal, _ string) (postgres.Role, error) {
	fake.calls++
	return fake.role, fake.err
}

func (fake *roleHTTPReaderFake) ListRoles(_ context.Context, _ string, _ *authn.VerifiedPrincipal, afterName string, afterVersion int64, limit int) (postgres.RolePage, error) {
	fake.listCalls++
	fake.afterName = afterName
	fake.afterVersion = afterVersion
	fake.limit = limit
	return fake.page, fake.err
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

func TestRoleHTTPServerListsGeneratedRolePage(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	verifier := &projectHTTPVerifierFake{}
	reader := &roleHTTPReaderFake{page: postgres.RolePage{
		Roles: []postgres.Role{{
			UID: "role-project-viewer-v1", Name: "role-project-viewer-v1", TenantID: "tenant-alpha",
			RoleName: "project.viewer", Version: 1, Permissions: []string{"projects.get", "projects.list"},
			State: "active", ResourceVersion: 5, CreatedAt: now,
		}},
		NextRoleName: "project.viewer", NextRoleVersion: 1,
	}}
	server, err := NewRoleHTTPServer(verifier, reader)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/roles?pageSize=1", nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-list")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.listCalls != 1 || reader.afterName != "" || reader.afterVersion != 0 || reader.limit != 1 {
		t.Fatalf("status=%d list calls=%d after=%q/%d limit=%d body=%s", response.Code, reader.listCalls, reader.afterName, reader.afterVersion, reader.limit, response.Body.String())
	}
	page, err := platformv1alpha1.DecodeRolePageResponseJSON(response.Body.Bytes())
	if err != nil || len(page.Value.Roles) != 1 || page.Value.NextPageToken == "" {
		t.Fatalf("role page = %#v / %v", page, err)
	}
	name, version, ok := decodeRolePageToken("tenant-alpha", page.Value.NextPageToken)
	if !ok || name != "project.viewer" || version != 1 {
		t.Fatalf("next page token = %q / %q / %d / %t", page.Value.NextPageToken, name, version, ok)
	}
	if verifier.seen != (authn.VerificationRequest{TenantID: "tenant-alpha", ResourceLevel: "tenant", ResourceID: "tenant-alpha", RequiredPermission: "roles.list"}) {
		t.Fatalf("verification request = %#v", verifier.seen)
	}

	otherTenantToken, ok := encodeRolePageToken("tenant-other", "project.viewer", 1)
	if !ok {
		t.Fatal("valid cross-tenant fixture token was not encoded")
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-alpha/roles?pageToken="+otherTenantToken, nil)
	request.Header.Set("Authorization", "Bearer access-token")
	request.Header.Set("X-Request-ID", "request-invalid")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || reader.listCalls != 1 || verifier.calls != 1 {
		t.Fatalf("cross-tenant token status=%d verifier calls=%d list calls=%d body=%s", response.Code, verifier.calls, reader.listCalls, response.Body.String())
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
