package server

import (
	"encoding/json"
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

const OrganizationRoute = "/v1/tenants/{tenantId}/organizations/{organizationId}"

type OrganizationHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentOrganizationReader
}

func NewOrganizationHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentOrganizationReader) (*OrganizationHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrNilManagedAgentOrganizationReadService
	}
	return &OrganizationHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *OrganizationHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request == nil || server == nil || server.verifier == nil || server.reader == nil {
		writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeOrganizationError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	tenantID, organizationID, ok := organizationPath(request.URL.Path)
	if !ok {
		writeOrganizationError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, err := openapiv1.ValidateGetServerRequest(tenantID, organizationID, requestID); err != nil {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeOrganizationError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeOrganizationError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: tenantID, ResourceLevel: "organization", ResourceID: organizationID, RequiredPermission: "organizations.get",
	})
	if err != nil {
		writeOrganizationError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	organization, err := GetOrganization(request.Context(), server.reader, principal, ManagedAgentGetOrganizationRequest{TenantID: tenantID, OrganizationID: organizationID, RequestID: requestID})
	if err != nil {
		status, code := organizationErrorStatus(err)
		writeOrganizationError(writer, status, code)
		return
	}
	value := organizationResource(organization)
	body, err := platformv1alpha1.EncodeOrganizationResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Organization]{Value: value})
	if err != nil {
		writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func organizationPath(path string) (string, string, bool) {
	const prefix = "/v1/tenants/"
	const separator = "/organizations/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), separator)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], "/") || strings.Contains(parts[1], "/") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func organizationResource(organization postgres.Organization) platformv1alpha1.Organization {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: organization.TenantID}
	return platformv1alpha1.Organization{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: projectAPIVersion, Kind: "Organization", Metadata: commonv1alpha1.ResourceMetadata{
			UID: organization.UID, Name: organization.Name, TenantRef: tenant, ResourceVersion: strconv.FormatInt(organization.ResourceVersion, 10),
			CreatedAt: organization.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: organization.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.OrganizationSpec{TenantRef: tenant, DisplayName: organization.DisplayName, State: organization.State},
	}
}

func organizationErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrOrganizationNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeOrganizationError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(projectErrorResponse{APIVersion: projectAPIVersion, Kind: "Error", Code: code})
}
