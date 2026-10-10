package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const MyProjectsRoute = "/v1/tenants/{tenantId}/my-projects"

var ErrInvalidMyProjectsHTTPServer = errors.New("my projects HTTP server configuration is invalid")

type MyProjectsReader interface {
	ListMyProjects(context.Context, string, *authn.VerifiedPrincipal, postgres.MyProjectsCursor, int) (postgres.MyProjectsPage, error)
}

type MyProjectsHTTPServer struct {
	verifier AccessTokenVerifier
	reader   MyProjectsReader
}

func NewMyProjectsHTTPServer(verifier AccessTokenVerifier, reader MyProjectsReader) (*MyProjectsHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrInvalidMyProjectsHTTPServer
	}
	return &MyProjectsHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *MyProjectsHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.reader == nil || request == nil || request.URL == nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if request.URL.RawPath != "" || request.URL.Opaque != "" {
		writeProjectError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	tenantID, ok := myProjectsPath(request.URL.Path)
	if !ok {
		writeProjectError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeProjectError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	pageSize, pageToken, ok := myProjectsPagination(request)
	if !ok {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateListMyProjectsServerRequest(tenantID, requestID, pageSize, pageToken)
	if err != nil {
		writeProjectError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	cursor := postgres.MyProjectsCursor{}
	if validated.PageToken != "" {
		cursor.SubjectKey, cursor.AfterProjectUID, ok = decodeMyProjectsPageToken(validated.TenantID, validated.PageToken)
		if !ok {
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
	principal, err := verifyHTTPRequestAccessToken(request.Context(), server.verifier, bearer, authn.VerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "tenant", ResourceID: validated.TenantID, RequiredPermission: "projects.list",
	})
	if err != nil {
		writeProjectError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.reader.ListMyProjects(request.Context(), validated.TenantID, principal, cursor, validated.PageSize)
	if err != nil {
		status, code := projectErrorStatus(err)
		writeProjectError(writer, status, code)
		return
	}
	projects := make([]platformv1alpha1.Project, 0, len(page.Projects))
	for _, project := range page.Projects {
		projects = append(projects, projectResource(project))
	}
	nextPageToken := ""
	if page.NextProjectUID != "" {
		nextPageToken, ok = encodeMyProjectsPageToken(validated.TenantID, page.SubjectKey, page.NextProjectUID)
		if !ok {
			writeProjectError(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeProjectPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.ProjectPage]{Value: platformv1alpha1.ProjectPage{
		APIVersion: projectAPIVersion, Kind: "ProjectPage", Projects: projects, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writeProjectError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func myProjectsPath(path string) (string, bool) {
	const prefix = "/v1/tenants/"
	const suffix = "/my-projects"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	tenantID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return tenantID, tenantID != "" && !strings.Contains(tenantID, "/")
}

func myProjectsPagination(request *http.Request) (int, string, bool) {
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

func encodeMyProjectsPageToken(tenantID, subjectKey, projectUID string) (string, bool) {
	return encodePageToken("my-projects/v1",
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{value: subjectKey, path: "/subjectKey"},
		pageTokenPart{value: projectUID, path: "/projectId"},
	)
}

func decodeMyProjectsPageToken(tenantID, token string) (string, string, bool) {
	parts, ok := decodePageToken("my-projects/v1", token,
		pageTokenPart{value: tenantID, path: "/tenantId"},
		pageTokenPart{path: "/subjectKey"},
		pageTokenPart{path: "/projectId"},
	)
	if !ok || !validMyProjectsSubjectKey(parts[1]) {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func validMyProjectsSubjectKey(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
