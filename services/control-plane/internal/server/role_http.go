package server

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	RoleCollectionRoute = "/v1/tenants/{tenantId}/roles"
	RoleRoute           = "/v1/tenants/{tenantId}/roles/{roleId}"
)

var ErrInvalidRoleHTTPServer = errors.New("role HTTP server configuration is invalid")

type RoleHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentRoleReader
}

func NewRoleHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentRoleReader) (*RoleHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrInvalidRoleHTTPServer
	}
	return &RoleHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *RoleHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.reader == nil || request == nil {
		writeRoleError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeRoleError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	tenantID, roleID, ok := rolePath(request.URL.Path)
	if !ok {
		writeRoleError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if roleID == "" {
		server.list(writer, request, tenantID)
		return
	}
	server.get(writer, request, tenantID, roleID)
}

func (server *RoleHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, roleID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRoleError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateGetServerRequest(tenantID, roleID, requestID)
	if err != nil {
		writeRoleError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "roles.get",
	})
	if err != nil {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	role, err := GetRole(request.Context(), server.reader, principal, ManagedAgentGetRoleRequest{TenantID: validated.TenantID, RoleID: validated.ResourceID, RequestID: validated.RequestID})
	if err != nil {
		status, code := roleErrorStatus(err)
		writeRoleError(writer, status, code)
		return
	}
	value := roleResource(role)
	body, err := platformv1alpha1.EncodeRoleResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Role]{Value: value})
	if err != nil {
		writeRoleError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *RoleHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeRoleError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writeRoleError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListRolesServerRequest(tenantID, requestID, pageSize, pageToken)
	if err != nil {
		writeRoleError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterRoleName := ""
	var afterRoleVersion int64
	if validated.PageToken != "" {
		if afterRoleName, afterRoleVersion, ok = decodeRolePageToken(validated.TenantID, validated.PageToken); !ok {
			writeRoleError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "roles.list",
	})
	if err != nil {
		writeRoleError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListRoles(request.Context(), validated.TenantID, principal, afterRoleName, afterRoleVersion, validated.PageSize)
	if err != nil {
		status, code := roleErrorStatus(err)
		writeRoleError(writer, status, code)
		return
	}
	roles := make([]platformv1alpha1.Role, 0, len(page.Roles))
	for _, role := range page.Roles {
		roles = append(roles, roleResource(role))
	}
	value := platformv1alpha1.RolePage{APIVersion: platformv1alpha1.APIVersion, Kind: "RolePage", Roles: roles}
	if page.NextRoleName != "" {
		value.NextPageToken, ok = encodeRolePageToken(validated.TenantID, page.NextRoleName, page.NextRoleVersion)
		if !ok {
			writeRoleError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeRolePageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RolePage]{Value: value})
	if err != nil {
		writeRoleError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func rolePath(path string) (string, string, bool) {
	const prefix = "/v1/tenants/"
	const separator = "/roles/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	if strings.HasSuffix(rest, "/roles") {
		tenantID := strings.TrimSuffix(rest, "/roles")
		return tenantID, "", tenantID != "" && !strings.Contains(tenantID, "/")
	}
	parts := strings.Split(rest, separator)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], "/") || strings.Contains(parts[1], "/") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func encodeRolePageToken(tenantID, roleName string, roleVersion int64) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(roleName, "/roleName") != nil || roleVersion < 1 {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("role/v1\x00" + tenantID + "\x00" + roleName + "\x00" + strconv.FormatInt(roleVersion, 10)))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeRolePageToken(tenantID, token string) (string, int64, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", 0, false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", 0, false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "role/v1" || parts[1] != tenantID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/roleName") != nil {
		return "", 0, false
	}
	version, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || version < 1 {
		return "", 0, false
	}
	return parts[2], version, true
}

func roleResource(role postgres.Role) platformv1alpha1.Role {
	return platformv1alpha1.Role{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "Role", Metadata: commonv1alpha1.ResourceMetadata{
			UID: role.UID, Name: role.Name, TenantRef: commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: role.TenantID},
			ResourceVersion: strconv.FormatInt(role.ResourceVersion, 10), CreatedAt: role.CreatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.RoleSpec{Name: role.RoleName, Version: int(role.Version), Permissions: append([]string(nil), role.Permissions...), State: role.State},
	}
}

func roleErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrRoleNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeRoleError(writer http.ResponseWriter, status int, code string) {
	writePublicProblem(writer, status, code)
}
