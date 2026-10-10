package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const AdminManagementRoutePrefix = "/v1/admin/tenants/"

var ErrInvalidAdminManagementHTTPServer = errors.New("admin management HTTP server configuration is invalid")

// AdminManagementHTTPServer exposes only the existing tenant, organization,
// project, role, membership, and role-binding operations under the Admin
// audience. It translates an admitted Admin path to the existing typed
// handler; it does not add another authorization or storage implementation.
type AdminManagementHTTPServer struct {
	tenant       http.Handler
	organization http.Handler
	project      http.Handler
	role         http.Handler
	rbac         http.Handler
}

func NewAdminManagementHTTPServer(
	verifier AccessTokenVerifier,
	coordination *postgres.DurableCoordinationService,
	mutation *postgres.RBACMutationService,
) (*AdminManagementHTTPServer, error) {
	if verifier == nil || coordination == nil || mutation == nil {
		return nil, ErrInvalidAdminManagementHTTPServer
	}
	creator, err := NewDurableProjectCreateServer(coordination)
	if err != nil {
		return nil, err
	}
	tenant, err := NewPlatformTenantHTTPServer(verifier, coordination)
	if err != nil {
		return nil, err
	}
	organization, err := NewOrganizationHTTPServer(verifier, coordination)
	if err != nil {
		return nil, err
	}
	project, err := NewProjectHTTPServer(verifier, coordination, creator)
	if err != nil {
		return nil, err
	}
	role, err := NewRoleHTTPServer(verifier, coordination)
	if err != nil {
		return nil, err
	}
	rbac, err := NewRBACHTTPServer(verifier, coordination, mutation)
	if err != nil {
		return nil, err
	}
	return newAdminManagementHTTPServer(tenant, organization, project, role, rbac)
}

func newAdminManagementHTTPServer(tenant, organization, project, role, rbac http.Handler) (*AdminManagementHTTPServer, error) {
	if tenant == nil || organization == nil || project == nil || role == nil || rbac == nil {
		return nil, ErrInvalidAdminManagementHTTPServer
	}
	return &AdminManagementHTTPServer{tenant: tenant, organization: organization, project: project, role: role, rbac: rbac}, nil
}

func (server *AdminManagementHTTPServer) HandlesPath(path string) bool {
	if server == nil {
		return false
	}
	_, _, _, known := server.route("", path)
	return known
}

func (server *AdminManagementHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || request == nil || request.URL == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.URL.RawPath != "" || request.URL.Opaque != "" {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	handler, path, allow, known := server.route(request.Method, request.URL.Path)
	if !known {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if handler == nil {
		writer.Header().Set("Allow", strings.Join(allow, ", "))
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	forward := request.Clone(request.Context())
	forward.URL.Path = path
	forward.URL.RawPath = ""
	handler.ServeHTTP(writer, forward)
}

func (server *AdminManagementHTTPServer) route(method, path string) (http.Handler, string, []string, bool) {
	if !strings.HasPrefix(path, AdminManagementRoutePrefix) {
		return nil, "", nil, false
	}
	parts := strings.Split(strings.TrimPrefix(path, AdminManagementRoutePrefix), "/")
	if len(parts) == 0 || parts[0] == "" || slicesContainEmpty(parts) {
		return nil, "", nil, false
	}
	mapped := "/v1/tenants/" + strings.Join(parts, "/")
	if len(parts) == 1 {
		return admittedAdminMethod(server.tenant, mapped, method, http.MethodGet)
	}
	switch parts[1] {
	case "organizations":
		if len(parts) == 2 {
			return admittedAdminMethod(server.organization, mapped, method, http.MethodGet, http.MethodPost)
		}
		if len(parts) == 3 {
			return admittedAdminMethod(server.organization, mapped, method, http.MethodGet)
		}
	case "projects":
		if len(parts) == 2 {
			return admittedAdminMethod(server.project, mapped, method, http.MethodGet, http.MethodPost)
		}
		if len(parts) == 3 {
			return admittedAdminMethod(server.project, mapped, method, http.MethodGet)
		}
	case "roles":
		if len(parts) == 2 || len(parts) == 3 {
			return admittedAdminMethod(server.role, mapped, method, http.MethodGet)
		}
	case "memberships":
		if len(parts) == 2 {
			return admittedAdminMethod(server.rbac, mapped, method, http.MethodGet, http.MethodPost)
		}
		if len(parts) == 3 {
			if _, action, ok := strings.Cut(parts[2], ":"); ok {
				if action == "resume" || action == "suspend" || action == "revoke" {
					return admittedAdminMethod(server.rbac, mapped, method, http.MethodPost)
				}
				return nil, "", nil, false
			}
			return admittedAdminMethod(server.rbac, mapped, method, http.MethodGet)
		}
	case "role-bindings":
		if len(parts) == 2 {
			return admittedAdminMethod(server.rbac, mapped, method, http.MethodGet, http.MethodPost)
		}
		if len(parts) == 3 {
			if _, action, ok := strings.Cut(parts[2], ":"); ok {
				if action == "revoke" {
					return admittedAdminMethod(server.rbac, mapped, method, http.MethodPost)
				}
				return nil, "", nil, false
			}
			return admittedAdminMethod(server.rbac, mapped, method, http.MethodGet)
		}
	}
	return nil, "", nil, false
}

func admittedAdminMethod(handler http.Handler, path, method string, allowed ...string) (http.Handler, string, []string, bool) {
	for _, candidate := range allowed {
		if method == candidate {
			return handler, path, allowed, true
		}
	}
	return nil, path, allowed, true
}

func slicesContainEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}
	return false
}
