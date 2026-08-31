package server

import (
	"encoding/base64"
	"errors"
	"io"
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
	OrganizationCollectionRoute = "/v1/tenants/{tenantId}/organizations"
	OrganizationRoute           = "/v1/tenants/{tenantId}/organizations/{organizationId}"
)

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
	preparePublicRequestID(writer, request)
	if request == nil || server == nil || server.verifier == nil || server.reader == nil {
		writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.Method == http.MethodPost {
		tenantID, ok := organizationCollectionPath(request.URL.Path)
		if !ok {
			writeOrganizationError(writer, http.StatusNotFound, "route_not_found")
			return
		}
		server.create(writer, request, tenantID)
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeOrganizationError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if tenantID, ok := organizationCollectionPath(request.URL.Path); ok {
		server.list(writer, request, tenantID)
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
	server.writeOrganization(writer, requestID, http.StatusOK, organization)
}

func (server *OrganizationHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	pageSize, pageToken, ok := organizationPagination(request)
	if !ok {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListOrganizationsServerRequest(tenantID, requestID, pageSize, pageToken)
	if err != nil {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterOrganizationUID := ""
	if validated.PageToken != "" {
		if afterOrganizationUID, ok = decodeOrganizationPageToken(validated.TenantID, validated.PageToken); !ok {
			writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
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
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "organizations.list",
	})
	if err != nil {
		writeOrganizationError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListOrganizations(request.Context(), validated.TenantID, principal, afterOrganizationUID, validated.PageSize)
	if err != nil {
		status, code := organizationErrorStatus(err)
		writeOrganizationError(writer, status, code)
		return
	}
	server.writeOrganizationPage(writer, validated.RequestID, validated.TenantID, page)
}

func (server *OrganizationHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writeOrganizationError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateCreateOrganizationServerRequest(tenantID, requestID, body)
	if err != nil {
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
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "organizations.create",
	})
	if err != nil {
		writeOrganizationError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	organization, err := server.reader.CreateOrganization(request.Context(), validated.TenantID, principal, postgres.CreateOrganizationInput{
		ExpectedTenantRevision: validated.Body.ExpectedTenantRevision,
		OrganizationUID:        validated.Body.OrganizationID,
		OrganizationName:       validated.Body.Name,
		DisplayName:            validated.Body.DisplayName,
		AuditFactUID:           validated.Body.AuditFactUID,
		ReasonCode:             validated.Body.ReasonCode,
	})
	if err != nil {
		status, code := organizationErrorStatus(err)
		writeOrganizationError(writer, status, code)
		return
	}
	server.writeOrganization(writer, validated.RequestID, http.StatusCreated, organization)
}

func (server *OrganizationHTTPServer) writeOrganization(writer http.ResponseWriter, requestID string, status int, organization postgres.Organization) {
	value := organizationResource(organization)
	body, err := platformv1alpha1.EncodeOrganizationResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Organization]{Value: value})
	if err != nil {
		writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func (server *OrganizationHTTPServer) writeOrganizationPage(writer http.ResponseWriter, requestID, tenantID string, page postgres.OrganizationPage) {
	organizations := make([]platformv1alpha1.Organization, 0, len(page.Organizations))
	for _, organization := range page.Organizations {
		organizations = append(organizations, organizationResource(organization))
	}
	nextPageToken := ""
	if page.NextOrganizationUID != "" {
		var ok bool
		nextPageToken, ok = encodeOrganizationPageToken(tenantID, page.NextOrganizationUID)
		if !ok {
			writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	value := platformv1alpha1.OrganizationPage{APIVersion: projectAPIVersion, Kind: "OrganizationPage", Organizations: organizations, NextPageToken: nextPageToken}
	body, err := platformv1alpha1.EncodeOrganizationPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.OrganizationPage]{Value: value})
	if err != nil {
		writeOrganizationError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func organizationCollectionPath(path string) (string, bool) {
	const prefix = "/v1/tenants/"
	const suffix = "/organizations"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return tenantID, tenantID != "" && !strings.Contains(tenantID, "/")
}

func organizationPagination(request *http.Request) (int, string, bool) {
	pageSize := 50
	pageToken := ""
	for name, values := range request.URL.Query() {
		if len(values) != 1 || values[0] == "" {
			return 0, "", false
		}
		switch name {
		case "pageSize":
			value, err := strconv.Atoi(values[0])
			if err != nil {
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

func encodeOrganizationPageToken(tenantID, organizationUID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(organizationUID, "/organizationId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("organization/v1\x00" + tenantID + "\x00" + organizationUID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeOrganizationPageToken(tenantID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 3 || parts[0] != "organization/v1" || parts[1] != tenantID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/organizationId") != nil {
		return "", false
	}
	return parts[2], true
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
	case errors.Is(err, postgres.ErrMutationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, postgres.ErrMutationConflict):
		return http.StatusConflict, "mutation_conflict"
	case errors.Is(err, postgres.ErrMutationCommitUnknown):
		return http.StatusInternalServerError, "commit_outcome_unknown"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeOrganizationError(writer http.ResponseWriter, status int, code string) {
	writePublicProblem(writer, status, code)
}
