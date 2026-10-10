package server

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	AdminServiceAccountRoutePrefix = "/v1/admin/tenants/"
	serviceAccountMaxBodyBytes     = 4 << 10
	serviceAccountDefaultPageSize  = 50
	serviceAccountMaximumPageSize  = 200
)

var ErrInvalidServiceAccountHTTPServer = errors.New("service account HTTP server configuration is invalid")

type ServiceAccountManager interface {
	Create(context.Context, string, *authn.VerifiedPrincipal, *authn.VerifiedPrincipal, api.ServiceAccountCreateRequest, string) (api.ServiceAccountCreated, error)
	ManagementScope(context.Context, string, string) (authz.ScopeRef, error)
	Rotate(context.Context, string, string, *authn.VerifiedPrincipal, int64, string) (api.ServiceAccountRotated, error)
	Disable(context.Context, string, string, *authn.VerifiedPrincipal, int64, string) (int64, error)
	List(context.Context, string, *authn.VerifiedPrincipal, string, int) (api.ServiceAccountPage, error)
	RecordPermissionDenial(context.Context, string, string, string, api.IdentityApplication, authz.ScopeRef, *authn.VerifiedPrincipal, string) error
}

type ServiceAccountHTTPServer struct {
	verifier AccessTokenVerifier
	manager  ServiceAccountManager
}

func NewServiceAccountHTTPServer(verifier AccessTokenVerifier, manager ServiceAccountManager) (*ServiceAccountHTTPServer, error) {
	if verifier == nil || manager == nil {
		return nil, ErrInvalidServiceAccountHTTPServer
	}
	return &ServiceAccountHTTPServer{verifier: verifier, manager: manager}, nil
}

func (server *ServiceAccountHTTPServer) HandlesPath(path string) bool {
	if server == nil {
		return false
	}
	_, _, _, ok := serviceAccountPath(path)
	return ok
}

func (server *ServiceAccountHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.manager == nil || request == nil || request.URL == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	if request.URL.RawPath != "" || request.URL.Opaque != "" {
		writePublicProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	tenantID, serviceAccountID, action, ok := serviceAccountPath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok || common.ValidateIdentifier(requestID, "/X-Request-ID") != nil || common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)

	switch action {
	case "collection":
		if request.Method == http.MethodGet {
			server.list(writer, request, tenantID)
			return
		}
		if request.Method == http.MethodPost {
			server.create(writer, request, tenantID)
			return
		}
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
	case "rotate", "disable":
		if request.Method == http.MethodPost {
			server.transition(writer, request, tenantID, serviceAccountID, action)
			return
		}
		writer.Header().Set("Allow", http.MethodPost)
	default:
		writePublicProblem(writer, http.StatusNotFound, "ROUTE_NOT_FOUND")
		return
	}
	writePublicProblem(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}

func (server *ServiceAccountHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID := writer.Header().Get("X-Request-ID")
	body, ok := serviceAccountRequestBody(writer, request)
	if !ok {
		return
	}
	input, err := api.DecodeServiceAccountCreateRequestJSON(body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	scope := authz.ScopeRef{Level: authz.ScopeLevel(input.ScopeLevel), ID: input.ScopeID}
	if scope.Validate(tenantID) != nil || scope.Level == authz.ScopePlatform {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	bearer, ok := serviceAccountBearer(request)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	membershipPrincipal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: "memberships.create",
	})
	if err != nil {
		server.writePermissionDenial(writer, request, bearer, tenantID, input.ServiceAccountID, "create", input.Application, scope)
		return
	}
	bindingPrincipal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: "role-bindings.bind",
	})
	if err != nil {
		server.writePermissionDenial(writer, request, bearer, tenantID, input.ServiceAccountID, "create", input.Application, scope)
		return
	}
	result, err := server.manager.Create(request.Context(), tenantID, membershipPrincipal, bindingPrincipal, input, requestID)
	if err != nil {
		writeServiceAccountError(writer, err)
		return
	}
	encoded, err := api.EncodeServiceAccountCreatedJSON(result)
	if err != nil || result.ServiceAccount.TenantID != tenantID {
		writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	writer.Header().Set("X-Resource-Version", result.ServiceAccount.ResourceVersion)
	writeServiceAccountJSON(writer, http.StatusCreated, encoded)
}

func (server *ServiceAccountHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID string) {
	pageSize, pageToken, ok := serviceAccountPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	after := ""
	if pageToken != "" {
		after, ok = decodeServiceAccountPageToken(tenantID, pageToken)
		if !ok {
			writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
	}
	bearer, ok := serviceAccountBearer(request)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	principal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: "tenant", ResourceID: tenantID, RequiredPermission: "memberships.list",
	})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	page, err := server.manager.List(request.Context(), tenantID, principal, after, pageSize)
	if err != nil {
		writeServiceAccountError(writer, err)
		return
	}
	if page.NextPageToken != "" {
		page.NextPageToken, ok = encodeServiceAccountPageToken(tenantID, page.NextPageToken)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
			return
		}
	}
	for _, account := range page.ServiceAccounts {
		if account.TenantID != tenantID {
			writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
			return
		}
	}
	encoded, err := api.EncodeServiceAccountPageJSON(page)
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
		return
	}
	writeServiceAccountJSON(writer, http.StatusOK, encoded)
}

func (server *ServiceAccountHTTPServer) writePermissionDenial(
	writer http.ResponseWriter,
	request *http.Request,
	bearer, tenantID, serviceAccountID, action string,
	application api.IdentityApplication,
	scope authz.ScopeRef,
) {
	permission, ok := serviceAccountIdentityPermission(scope)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	principal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: permission,
	})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	if err := server.manager.RecordPermissionDenial(request.Context(), tenantID, serviceAccountID, action, application, scope, principal, writer.Header().Get("X-Request-ID")); err != nil {
		writeServiceAccountError(writer, err)
		return
	}
	writePublicProblem(writer, http.StatusForbidden, "AUTHORIZATION_DENIED")
}

func serviceAccountIdentityPermission(scope authz.ScopeRef) (string, bool) {
	switch scope.Level {
	case authz.ScopeTenant:
		return "tenants.get", true
	case authz.ScopeOrganization:
		return "organizations.get", true
	case authz.ScopeProject:
		return "projects.get", true
	default:
		return "", false
	}
}

func (server *ServiceAccountHTTPServer) transition(writer http.ResponseWriter, request *http.Request, tenantID, serviceAccountID, action string) {
	if common.ValidateIdentifier(serviceAccountID, "/serviceAccountId") != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	body, ok := serviceAccountRequestBody(writer, request)
	if !ok {
		return
	}
	var expected string
	if action == "rotate" {
		input, err := api.DecodeServiceAccountRotateRequestJSON(body)
		if err != nil {
			writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
		expected = input.ExpectedResourceVersion
	} else {
		input, err := api.DecodeServiceAccountDisableRequestJSON(body)
		if err != nil {
			writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
			return
		}
		expected = input.ExpectedResourceVersion
	}
	version, err := strconv.ParseInt(expected, 10, 64)
	if err != nil || version < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	bearer, ok := serviceAccountBearer(request)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
		return
	}
	scope, err := server.manager.ManagementScope(request.Context(), tenantID, serviceAccountID)
	if err != nil {
		// Scope lookup happens before token verification. Keep absent resources
		// indistinguishable from invalid credentials at this boundary.
		if errors.Is(err, postgres.ErrMutationTargetNotFound) {
			writePublicProblem(writer, http.StatusUnauthorized, "AUTHENTICATION_FAILED")
			return
		}
		writeServiceAccountError(writer, err)
		return
	}
	permission := "memberships.update"
	if action == "disable" {
		permission = "memberships.delete"
	}
	principal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: string(scope.Level), ResourceID: scope.ID, RequiredPermission: permission,
	})
	if err != nil {
		server.writePermissionDenial(writer, request, bearer, tenantID, serviceAccountID, action, "", scope)
		return
	}
	requestID := writer.Header().Get("X-Request-ID")
	if action == "rotate" {
		result, err := server.manager.Rotate(request.Context(), tenantID, serviceAccountID, principal, version, requestID)
		if err != nil {
			writeServiceAccountError(writer, err)
			return
		}
		encoded, err := api.EncodeServiceAccountRotatedJSON(result)
		if err != nil {
			writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
			return
		}
		writer.Header().Set("X-Resource-Version", result.ResourceVersion)
		writeServiceAccountJSON(writer, http.StatusOK, encoded)
		return
	}
	resultVersion, err := server.manager.Disable(request.Context(), tenantID, serviceAccountID, principal, version, requestID)
	if err != nil {
		writeServiceAccountError(writer, err)
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(resultVersion, 10))
	writer.WriteHeader(http.StatusNoContent)
}

func serviceAccountPath(path string) (tenantID, serviceAccountID, action string, ok bool) {
	if !strings.HasPrefix(path, AdminServiceAccountRoutePrefix) {
		return "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, AdminServiceAccountRoutePrefix), "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] == "service-accounts" {
		return parts[0], "", "collection", true
	}
	if len(parts) != 3 || parts[0] == "" || parts[1] != "service-accounts" || parts[2] == "" {
		return "", "", "", false
	}
	if id := strings.TrimSuffix(parts[2], ":rotate-credential"); id != parts[2] && id != "" {
		return parts[0], id, "rotate", true
	}
	if id := strings.TrimSuffix(parts[2], ":disable"); id != parts[2] && id != "" {
		return parts[0], id, "disable", true
	}
	return "", "", "", false
}

func serviceAccountBearer(request *http.Request) (string, bool) {
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		return "", false
	}
	return bearerToken(authorization)
}

func serviceAccountRequestBody(writer http.ResponseWriter, request *http.Request) ([]byte, bool) {
	contentType, ok := exactSingleHeader(request.Header, "Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if !ok || err != nil || mediaType != "application/json" {
		writePublicProblem(writer, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, serviceAccountMaxBodyBytes))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
		return nil, false
	}
	return body, true
}

func serviceAccountPagination(request *http.Request) (int, string, bool) {
	pageSize, pageToken := serviceAccountDefaultPageSize, ""
	for name, values := range request.URL.Query() {
		if len(values) != 1 || values[0] == "" {
			return 0, "", false
		}
		switch name {
		case "pageSize":
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 1 || value > serviceAccountMaximumPageSize {
				return 0, "", false
			}
			pageSize = value
		case "pageToken":
			pageToken = values[0]
		default:
			return 0, "", false
		}
	}
	return pageSize, pageToken, true
}

func encodeServiceAccountPageToken(tenantID, serviceAccountID string) (string, bool) {
	return encodePageToken("service-accounts/v1",
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: serviceAccountID, path: "/serviceAccountId"},
	)
}

func decodeServiceAccountPageToken(tenantID, token string) (string, bool) {
	parts, ok := decodePageToken("service-accounts/v1", token,
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{path: "/serviceAccountId"},
	)
	if !ok {
		return "", false
	}
	return parts[1], true
}

func writeServiceAccountJSON(writer http.ResponseWriter, status int, body []byte) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeServiceAccountError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrMutationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "INVALID_REQUEST")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "AUTHORIZATION_DENIED")
	case errors.Is(err, postgres.ErrMutationTargetNotFound):
		writePublicProblem(writer, http.StatusNotFound, "RESOURCE_NOT_FOUND")
	case errors.Is(err, postgres.ErrMutationConflict):
		writePublicProblem(writer, http.StatusConflict, "RESOURCE_VERSION_CONFLICT")
	case errors.Is(err, postgres.ErrServiceAccountAuditUnavailable):
		writePublicProblem(writer, http.StatusServiceUnavailable, "SERVICE_ACCOUNT_AUDIT_UNAVAILABLE")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}
