package server

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const publishedEnvironmentProfileRoutePrefix = "/v1/tenants/"

type publishedEnvironmentProfileStore interface {
	ListPublishedEnvironmentProfiles(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.PublishedEnvironmentProfilePage, error)
}

type PublishedEnvironmentProfileHTTPServer struct {
	verifier AccessTokenVerifier
	store    publishedEnvironmentProfileStore
}

func NewPublishedEnvironmentProfileHTTPServer(verifier AccessTokenVerifier, store publishedEnvironmentProfileStore) (*PublishedEnvironmentProfileHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("published environment profile HTTP server configuration is invalid")
	}
	return &PublishedEnvironmentProfileHTTPServer{verifier: verifier, store: store}, nil
}

func (server *PublishedEnvironmentProfileHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, ok := publishedEnvironmentProfilePath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListEnvironmentProfilesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterProfileVersionID := ""
	if validated.PageToken != "" {
		if afterProfileVersionID, ok = decodePublishedEnvironmentProfilePageToken(tenantID, projectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "environment-profiles.list"}); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	page, err := server.store.ListPublishedEnvironmentProfiles(request.Context(), tenantID, principal, projectID, afterProfileVersionID, validated.PageSize)
	if err != nil {
		writeEnvironmentProfileError(writer, err)
		return
	}
	profiles := make([]platformv1alpha1.EnvironmentProfileSummary, 0, len(page.EnvironmentProfiles))
	for _, profile := range page.EnvironmentProfiles {
		profiles = append(profiles, publishedEnvironmentProfileResource(profile))
	}
	nextPageToken := ""
	if page.NextProfileVersionID != "" {
		if nextPageToken, ok = encodePublishedEnvironmentProfilePageToken(tenantID, projectID, page.NextProfileVersionID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeEnvironmentProfileSummaryPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentProfileSummaryPage]{Value: platformv1alpha1.EnvironmentProfileSummaryPage{
		APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentProfileSummaryPage",
		EnvironmentProfiles: profiles, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func publishedEnvironmentProfileResource(summary internalenvironmentprofile.Summary) platformv1alpha1.EnvironmentProfileSummary {
	return platformv1alpha1.EnvironmentProfileSummary{
		APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentProfileSummary",
		ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: summary.Scope.ProjectID},
		ProfileID:  summary.ProfileID, Name: summary.ProfileName, Version: summary.Version,
		Description: summary.Description, Status: "published", Availability: "available",
		ProviderKinds: summary.ProviderKinds, CPULimitMillis: summary.CPULimitMillis, MemoryLimitBytes: summary.MemoryLimitBytes,
	}
}

func publishedEnvironmentProfilePath(path string) (tenantID, projectID string, ok bool) {
	if !strings.HasPrefix(path, publishedEnvironmentProfileRoutePrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, publishedEnvironmentProfileRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "environment-profiles" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], true
	}
	return "", "", false
}

func HandlesPublishedEnvironmentProfilePath(path string) bool {
	_, _, ok := publishedEnvironmentProfilePath(path)
	return ok
}

func encodePublishedEnvironmentProfilePageToken(tenantID, projectID, profileVersionID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(profileVersionID, "/profileVersionId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("published-environment-profile/v1\x00" + tenantID + "\x00" + projectID + "\x00" + profileVersionID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodePublishedEnvironmentProfilePageToken(tenantID, projectID, token string) (string, bool) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "published-environment-profile/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/profileVersionId") != nil {
		return "", false
	}
	return parts[3], true
}
