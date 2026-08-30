package server

import (
	"context"
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
	ProjectRoutePrefix       = "/v1/tenants/"
	ManagedHostProjectRoute  = "/v1/managed-host/tenants/{tenantId}/projects/{projectId}"
	managedHostProjectPrefix = "/v1/managed-host/tenants/"
	ProjectRouteSuffix       = "/projects/"
	projectCreateRouteSuffix = "/projects"
	projectMaximumBodyBytes  = 1 << 20
	projectAPIVersion        = "platform.cloud-agents.dev/v1alpha1"
)

var ErrInvalidProjectHTTPServer = errors.New("project HTTP server configuration is invalid")

type AccessTokenVerifier interface {
	Verify(string, authn.VerificationRequest) (*authn.VerifiedPrincipal, error)
}

type ProjectHTTPServer struct {
	verifier AccessTokenVerifier
	reader   ManagedAgentProjectReader
	creator  projectCreator
}

type projectCreator interface {
	Create(context.Context, *authn.VerifiedPrincipal, ManagedAgentCreateProjectRequest) (postgres.DurableProjectCreateResult, error)
}

func NewProjectHTTPServer(verifier AccessTokenVerifier, reader ManagedAgentProjectReader, creator projectCreator) (*ProjectHTTPServer, error) {
	if verifier == nil || reader == nil || creator == nil {
		return nil, ErrInvalidProjectHTTPServer
	}
	return &ProjectHTTPServer{verifier: verifier, reader: reader, creator: creator}, nil
}

func (server *ProjectHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.reader == nil || server.creator == nil || request == nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	managedHost := strings.HasPrefix(request.URL.Path, managedHostProjectPrefix)
	if tenantID, ok := projectCreatePath(request.URL.Path); ok && !managedHost {
		switch request.Method {
		case http.MethodGet:
			server.list(writer, request, tenantID)
		case http.MethodPost:
			server.create(writer, request, tenantID)
		default:
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
			writeProjectError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		}
		return
	}
	tenantID, projectID, ok := projectPath(request.URL.Path)
	if !ok {
		writeProjectError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		allowed := http.MethodGet
		if !managedHost {
			allowed += ", " + http.MethodPost
		}
		writer.Header().Set("Allow", allowed)
		writeProjectError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateGetServerRequest(tenantID, projectID, requestID)
	if err != nil {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ResourceID, RequiredPermission: "projects.get"})
	if err != nil {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	project, err := GetProject(request.Context(), server.reader, principal, ManagedAgentGetProjectRequest{TenantID: validated.TenantID, ProjectID: validated.ResourceID, RequestID: validated.RequestID})
	if err != nil {
		status, code := projectErrorStatus(err)
		writeProjectError(writer, status, code)
		return
	}
	value := projectResource(project)
	body, err := platformv1alpha1.EncodeProjectResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Project]{Value: value})
	if err != nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *ProjectHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	organizationID, pageSize, pageToken, ok := projectPagination(request)
	if !ok {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListProjectsServerRequest(tenantID, organizationID, requestID, pageSize, pageToken)
	if err != nil {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterProjectUID := ""
	if validated.PageToken != "" {
		if afterProjectUID, ok = decodeProjectPageToken(validated.TenantID, validated.OrganizationID, validated.PageToken); !ok {
			writeProjectError(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "organization", ResourceID: validated.OrganizationID, RequiredPermission: "projects.list",
	})
	if err != nil {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListProjects(request.Context(), validated.TenantID, principal, validated.OrganizationID, afterProjectUID, validated.PageSize)
	if err != nil {
		status, code := projectErrorStatus(err)
		writeProjectError(writer, status, code)
		return
	}
	server.writeProjectPage(writer, validated.RequestID, validated.TenantID, validated.OrganizationID, page)
}

func (server *ProjectHTTPServer) writeProjectPage(writer http.ResponseWriter, requestID, tenantID, organizationID string, page postgres.ProjectPage) {
	projects := make([]platformv1alpha1.Project, 0, len(page.Projects))
	for _, project := range page.Projects {
		projects = append(projects, projectResource(project))
	}
	nextPageToken := ""
	if page.NextProjectUID != "" {
		var ok bool
		nextPageToken, ok = encodeProjectPageToken(tenantID, organizationID, page.NextProjectUID)
		if !ok {
			writeProjectError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	value := platformv1alpha1.ProjectPage{APIVersion: projectAPIVersion, Kind: "ProjectPage", Projects: projects, NextPageToken: nextPageToken}
	body, err := platformv1alpha1.EncodeProjectPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.ProjectPage]{Value: value})
	if err != nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *ProjectHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID string) {
	requestID, requestIDOK := exactSingleHeader(request.Header, "X-Request-ID")
	idempotencyKey, idempotencyOK := exactSingleHeader(request.Header, "Idempotency-Key")
	if !requestIDOK || !idempotencyOK {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, projectMaximumBodyBytes))
	if err != nil {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateCreateProjectServerRequest(tenantID, requestID, idempotencyKey, body)
	if err != nil {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	authorization, ok := exactSingleHeader(request.Header, "Authorization")
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	bearer, ok := bearerToken(authorization)
	if !ok {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "organization", ResourceID: validated.Body.OrganizationRef.ID, RequiredPermission: "projects.create",
	})
	if err != nil {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.creator.Create(request.Context(), principal, ManagedAgentCreateProjectRequest{
		RouteTenantID: validated.TenantID, RequestID: validated.RequestID, IdempotencyKey: validated.IdempotencyKey, Body: body,
	})
	if err != nil {
		status, code := projectErrorStatus(err)
		writeProjectError(writer, status, code)
		return
	}
	if result.DatabaseOutcome == postgres.DatabaseUnknown {
		writeProjectError(writer, http.StatusInternalServerError, "commit_outcome_unknown")
		return
	}
	if result.DatabaseOutcome == postgres.DatabaseRejected || result.Disposition == "conflict" {
		writeProjectError(writer, http.StatusConflict, "create_conflict")
		return
	}
	if result.Project.UID == "" {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	value := projectResource(result.Project)
	body, err = platformv1alpha1.EncodeProjectResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Project]{Value: value})
	if err != nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusCreated)
	_, _ = writer.Write(body)
}

func projectCreatePath(path string) (string, bool) {
	if !strings.HasPrefix(path, ProjectRoutePrefix) || !strings.HasSuffix(path, projectCreateRouteSuffix) {
		return "", false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, ProjectRoutePrefix), projectCreateRouteSuffix)
	return tenantID, tenantID != "" && !strings.Contains(tenantID, "/")
}

func projectPagination(request *http.Request) (string, int, string, bool) {
	organizationID := ""
	pageSize := 50
	pageToken := ""
	for name, values := range request.URL.Query() {
		if len(values) != 1 || values[0] == "" {
			return "", 0, "", false
		}
		switch name {
		case "organizationId":
			organizationID = values[0]
		case "pageSize":
			value, err := strconv.Atoi(values[0])
			if err != nil {
				return "", 0, "", false
			}
			pageSize = value
		case "pageToken":
			pageToken = values[0]
		default:
			return "", 0, "", false
		}
	}
	return organizationID, pageSize, pageToken, organizationID != ""
}

func encodeProjectPageToken(tenantID, organizationID, projectUID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(organizationID, "/organizationId") != nil || commonv1alpha1.ValidateIdentifier(projectUID, "/projectId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("project/v1\x00" + tenantID + "\x00" + organizationID + "\x00" + projectUID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeProjectPageToken(tenantID, organizationID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "project/v1" || parts[1] != tenantID || parts[2] != organizationID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/organizationId") != nil || commonv1alpha1.ValidateIdentifier(parts[3], "/projectId") != nil {
		return "", false
	}
	return parts[3], true
}

func projectPath(path string) (string, string, bool) {
	if strings.HasPrefix(path, managedHostProjectPrefix) {
		return projectPathWithPrefix(path, managedHostProjectPrefix)
	}
	return projectPathWithPrefix(path, ProjectRoutePrefix)
}

func projectPathWithPrefix(path, prefix string) (string, string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), ProjectRouteSuffix)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], "/") || strings.Contains(parts[1], "/") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func bearerToken(value string) (string, bool) {
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(value, "Bearer ")
	return token, token != "" && strings.TrimSpace(token) == token && !strings.ContainsAny(token, "\r\n")
}

func projectResource(project postgres.Project) platformv1alpha1.Project {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: project.TenantID}
	return platformv1alpha1.Project{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: projectAPIVersion, Kind: "Project", Metadata: commonv1alpha1.ResourceMetadata{
			UID: project.UID, Name: project.Name, TenantRef: tenant, ResourceVersion: strconv.FormatInt(project.ResourceVersion, 10),
			CreatedAt: project.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: project.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.ProjectSpec{TenantRef: tenant, OrganizationRef: commonv1alpha1.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: project.OrganizationID}, DisplayName: project.DisplayName, State: project.State},
	}
}

func projectErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrProjectNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeProjectError(writer http.ResponseWriter, status int, code string) {
	writePublicProblem(writer, status, code)
}
