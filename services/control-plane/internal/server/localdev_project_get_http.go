//go:build localdev

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

const (
	LocalProjectGetRoutePrefix = "/v1/tenants/"
	LocalProjectGetRouteSuffix = "/projects/"
	localProjectGetAPIVersion  = "platform.cloud-agents.dev/v1alpha1"
)

var ErrInvalidLocalProjectGetServer = errors.New("local project get server configuration is invalid")

type LocalProjectGetHTTPServer struct {
	verifier *authn.LocalVerifier
	reader   ManagedAgentProjectReader
}

func NewLocalProjectGetHTTPServer(verifier *authn.LocalVerifier, reader ManagedAgentProjectReader) (*LocalProjectGetHTTPServer, error) {
	if verifier == nil || reader == nil {
		return nil, ErrInvalidLocalProjectGetServer
	}
	return &LocalProjectGetHTTPServer{verifier: verifier, reader: reader}, nil
}

func (server *LocalProjectGetHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if server == nil || server.verifier == nil || server.reader == nil || request == nil {
		writeLocalProjectGetError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, ok := localProjectGetPath(request.URL.Path)
	if !ok {
		writeLocalProjectGetError(writer, http.StatusNotFound, "route_not_found")
		return
	}
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeLocalProjectGetError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	requestID, ok := exactSingleHeader(request.Header, "X-Request-ID")
	if !ok {
		writeLocalProjectGetError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1.ValidateGetServerRequest(tenantID, projectID, requestID)
	if err != nil {
		writeLocalProjectGetError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	bearer, ok := localBearer(request.Header)
	if !ok {
		writeLocalProjectGetError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.LocalVerificationRequest{
		TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ResourceID, RequiredPermission: "projects.get",
	})
	if err != nil {
		writeLocalProjectGetError(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	project, err := GetProject(request.Context(), server.reader, principal, ManagedAgentGetProjectRequest{
		TenantID: validated.TenantID, ProjectID: validated.ResourceID, RequestID: validated.RequestID,
	})
	if err != nil {
		status, code := localProjectGetErrorStatus(err)
		writeLocalProjectGetError(writer, status, code)
		return
	}
	value := platformv1alpha1.Project{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: localProjectGetAPIVersion, Kind: "Project", Metadata: commonv1alpha1.ResourceMetadata{
			UID: project.UID, Name: project.Name,
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: project.TenantID},
			ResourceVersion: strconv.FormatInt(project.ResourceVersion, 10), CreatedAt: project.CreatedAt.UTC().Format(time.RFC3339Nano),
			UpdatedAt: project.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.ProjectSpec{
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: project.TenantID},
			OrganizationRef: commonv1alpha1.OrganizationRef{Namespace: "cloud-agents", Kind: "organization", ID: project.OrganizationID},
			DisplayName:     project.DisplayName, State: project.State,
		},
	}
	body, err := platformv1alpha1.EncodeProjectResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.Project]{Value: value})
	if err != nil {
		writeLocalProjectGetError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", validated.RequestID)
	writer.Header().Set("X-Resource-Version", value.Metadata.ResourceVersion)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func localProjectGetPath(path string) (string, string, bool) {
	if !strings.HasPrefix(path, LocalProjectGetRoutePrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, LocalProjectGetRoutePrefix)
	parts := strings.Split(rest, LocalProjectGetRouteSuffix)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], "/") || strings.Contains(parts[1], "/") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func localProjectGetErrorStatus(err error) (int, string) {
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

type localProjectGetErrorResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Code       string `json:"code"`
}

func writeLocalProjectGetError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(localProjectGetErrorResponse{APIVersion: localProjectGetAPIVersion, Kind: "Error", Code: code})
}
