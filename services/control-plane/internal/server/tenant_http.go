package server

import (
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

const PlatformTenantRoute = "/v1/tenants/{tenantId}"

type PlatformTenantHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentTenantReader
}

func NewPlatformTenantHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentTenantReader) (*PlatformTenantHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrNilManagedAgentTenantReadService
	}
	return &PlatformTenantHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *PlatformTenantHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if request == nil || server == nil || server.verifier == nil || server.reader == nil {
		writeTenantError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeTenantError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	tenantID, ok := tenantPath(request.URL.Path)
	if !ok {
		writeTenantError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeTenantError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, err := openapiv1.ValidateGetServerRequest(tenantID, tenantID, requestID); err != nil {
		writeTenantError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeTenantError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeTenantError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: "tenant", ResourceID: tenantID, RequiredPermission: "tenants.get",
	})
	if err != nil {
		writeTenantError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	tenant, err := GetPlatformTenant(request.Context(), server.reader, principal, ManagedAgentGetTenantRequest{TenantID: tenantID, RequestID: requestID})
	if err != nil {
		status, code := tenantErrorStatus(err)
		writeTenantError(writer, status, code)
		return
	}
	value := tenantResource(tenant)
	body, err := platformv1alpha1.EncodePlatformTenantResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.PlatformTenant]{Value: value})
	if err != nil {
		writeTenantError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func tenantPath(path string) (string, bool) {
	const prefix = "/v1/tenants/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	value := strings.TrimPrefix(path, prefix)
	return value, value != "" && !strings.Contains(value, "/")
}

func tenantResource(tenant postgres.PlatformTenant) platformv1alpha1.PlatformTenant {
	ref := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: tenant.TenantID}
	return platformv1alpha1.PlatformTenant{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: projectAPIVersion, Kind: "PlatformTenant", Metadata: commonv1alpha1.ResourceMetadata{
			UID: tenant.TenantUID, Name: tenant.TenantID, TenantRef: ref, ResourceVersion: strconv.FormatInt(tenant.ResourceVersion, 10),
			CreatedAt: tenant.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: tenant.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.TenantSpec{DisplayName: tenant.DisplayName, State: tenant.State},
	}
}

func tenantErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrPlatformTenantNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeTenantError(writer http.ResponseWriter, status int, code string) {
	writePublicProblem(writer, status, code)
}
