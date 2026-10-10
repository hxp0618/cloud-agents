package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

type adminManagementVerifier struct{}

func (*adminManagementVerifier) Verify(token string, _ authn.VerificationRequest) (*authn.VerifiedPrincipal, error) {
	if token != "admin-token" {
		return nil, errors.New("wrong audience")
	}
	return &authn.VerifiedPrincipal{}, nil
}

func TestAdminManagementHTTPServerKeepsAdminAudienceAtExistingHandlerBoundary(t *testing.T) {
	now := time.Date(2026, time.October, 8, 8, 0, 0, 0, time.UTC)
	reader := &tenantHTTPReaderFake{tenant: postgres.PlatformTenant{
		TenantID: "tenant-alpha", TenantUID: "tenant-alpha", TenantName: "tenant-alpha",
		DisplayName: "Tenant Alpha", State: "active", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}}
	tenant, err := NewPlatformTenantHTTPServer(&adminManagementVerifier{}, reader)
	if err != nil {
		t.Fatal(err)
	}
	unused := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("wrong handler") })
	server, err := newAdminManagementHTTPServer(tenant, unused, unused, unused, unused)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name, token string
		want        int
	}{
		{name: "admin", token: "admin-token", want: http.StatusOK},
		{name: "user", token: "user-token", want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-alpha", nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			request.Header.Set("X-Request-ID", "request-alpha")
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls=%d, want admin request only", reader.calls)
	}
}

func TestAdminManagementHTTPServerUsesClosedRouteAndMethodMap(t *testing.T) {
	seen := make(chan *http.Request, 1)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen <- request
		writer.WriteHeader(http.StatusNoContent)
	})
	server, err := newAdminManagementHTTPServer(handler, handler, handler, handler, handler)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		method, path, mapped string
	}{
		{http.MethodGet, "/v1/admin/tenants/tenant-a", "/v1/tenants/tenant-a"},
		{http.MethodPost, "/v1/admin/tenants/tenant-a/organizations", "/v1/tenants/tenant-a/organizations"},
		{http.MethodGet, "/v1/admin/tenants/tenant-a/projects/project-a", "/v1/tenants/tenant-a/projects/project-a"},
		{http.MethodPost, "/v1/admin/tenants/tenant-a/memberships/member-a:suspend", "/v1/tenants/tenant-a/memberships/member-a:suspend"},
		{http.MethodPost, "/v1/admin/tenants/tenant-a/role-bindings/binding-a:revoke", "/v1/tenants/tenant-a/role-bindings/binding-a:revoke"},
	} {
		request := httptest.NewRequest(test.method, test.path, nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s %s status=%d body=%s", test.method, test.path, response.Code, response.Body.String())
		}
		if got := (<-seen).URL.Path; got != test.mapped {
			t.Fatalf("%s mapped=%q want=%q", test.path, got, test.mapped)
		}
	}

	for _, test := range []struct {
		name, method, target string
		want                 int
	}{
		{name: "user path", method: http.MethodGet, target: "/v1/tenants/tenant-a", want: http.StatusNotFound},
		{name: "content path", method: http.MethodGet, target: "/v1/admin/tenants/tenant-a/projects/project-a/sessions", want: http.StatusNotFound},
		{name: "unknown action", method: http.MethodPost, target: "/v1/admin/tenants/tenant-a/memberships/member-a:delete", want: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodDelete, target: "/v1/admin/tenants/tenant-a/projects/project-a", want: http.StatusMethodNotAllowed},
		{name: "trailing slash", method: http.MethodGet, target: "/v1/admin/tenants/tenant-a/projects/", want: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.target, nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}

	raw := httptest.NewRequest(http.MethodGet, "/v1/admin/tenants/tenant-a/projects/project-a", nil)
	raw.URL.RawPath = "/v1/admin/tenants/tenant-a/projects%2fproject-a"
	response := httptest.NewRecorder()
	server.ServeHTTP(response, raw)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ambiguous raw path status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNewAdminManagementHTTPServerRejectsNilDependencies(t *testing.T) {
	if server, err := NewAdminManagementHTTPServer(nil, nil, nil); server != nil || !errors.Is(err, ErrInvalidAdminManagementHTTPServer) {
		t.Fatalf("server=%v err=%v", server, err)
	}
}
