package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
	internalworkerrelease "github.com/hxp0618/cloud-agents/services/control-plane/internal/workerrelease"
)

type workerReleaseStore interface {
	RegisterWorkerRelease(context.Context, string, *authn.VerifiedPrincipal, internalworkerrelease.RegisterInput) (internalworkerrelease.Snapshot, error)
	ListWorkerReleases(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.WorkerReleasePage, error)
}

type WorkerReleaseHTTPServer struct {
	verifier AccessTokenVerifier
	store    workerReleaseStore
}

func NewAdminWorkerReleaseHTTPServer(verifier AccessTokenVerifier, store workerReleaseStore) (*WorkerReleaseHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("worker release HTTP server configuration is invalid")
	}
	return &WorkerReleaseHTTPServer{verifier: verifier, store: store}, nil
}

func (server *WorkerReleaseHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, ok := adminWorkerReleasePath(request.URL.Path)
	if !ok {
		writePublicProblem(writer, http.StatusNotFound, "route_not_found")
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
	projectPermission, releasePermission := "projects.get", "releases.list"
	if request.Method == http.MethodPost {
		projectPermission, releasePermission = "projects.act", "releases.create"
	} else if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, projectPermission); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, releasePermission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	if request.Method == http.MethodPost {
		server.register(writer, request, tenantID, projectID, requestID, bearer)
	} else {
		server.list(writer, request, tenantID, projectID, requestID, bearer)
	}
}

func (server *WorkerReleaseHTTPServer) register(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	idempotencyKey, ok := exactSingleHeader(request.Header, "Idempotency-Key")
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateRegisterAdminWorkerReleaseServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	value := validated.Body
	result, err := server.store.RegisterWorkerRelease(request.Context(), tenantID, principal, internalworkerrelease.RegisterInput{
		Scope:     internalworkerrelease.Scope{TenantID: tenantID, ProjectID: projectID},
		ReleaseID: value.ReleaseID, ReleaseName: value.ReleaseName,
		ImageRepository: value.ImageRepository, ReleaseDigest: value.ReleaseDigest,
		PlatformVersion: value.PlatformVersion, RuntimeVersion: value.RuntimeVersion,
		CodexVersion: value.CodexVersion, ClaudeCodeVersion: value.ClaudeCodeVersion,
		Architectures: value.Architectures, VerificationEvidenceDigest: value.VerificationEvidenceDigest,
		Mutation: internalworkerrelease.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeWorkerReleaseError(writer, err)
		return
	}
	writeWorkerRelease(writer, http.StatusCreated, requestID, result)
}

func (server *WorkerReleaseHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminWorkerReleasesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterReleaseID := ""
	if validated.PageToken != "" {
		afterReleaseID, ok = decodeProjectResourcePageToken("worker-release/v1", tenantID, projectID, validated.PageToken)
		if !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListWorkerReleases(request.Context(), tenantID, principal, projectID, afterReleaseID, validated.PageSize)
	if err != nil {
		writeWorkerReleaseError(writer, err)
		return
	}
	values := make([]platformv1alpha1.WorkerRelease, 0, len(page.WorkerReleases))
	for _, release := range page.WorkerReleases {
		values = append(values, workerReleaseResource(release))
	}
	nextPageToken := ""
	if page.NextReleaseID != "" {
		nextPageToken, ok = encodeProjectResourcePageToken("worker-release/v1", tenantID, projectID, page.NextReleaseID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeWorkerReleasePageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.WorkerReleasePage]{Value: platformv1alpha1.WorkerReleasePage{
		APIVersion: platformv1alpha1.APIVersion, Kind: "WorkerReleasePage", WorkerReleases: values, NextPageToken: nextPageToken,
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSONResponse(writer, http.StatusOK, requestID, body)
}

func (server *WorkerReleaseHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func workerReleaseResource(snapshot internalworkerrelease.Snapshot) platformv1alpha1.WorkerRelease {
	return platformv1alpha1.WorkerRelease{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "WorkerRelease", Metadata: commonv1alpha1.ResourceMetadata{
			UID: snapshot.ReleaseID, Name: snapshot.ReleaseName,
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
			ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.WorkerReleaseSpec{
			ProjectRef:      commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
			ImageRepository: snapshot.ImageRepository, ReleaseDigest: snapshot.ReleaseDigest,
			PlatformVersion: snapshot.PlatformVersion, RuntimeVersion: snapshot.RuntimeVersion,
			CodexVersion: snapshot.CodexVersion, ClaudeCodeVersion: snapshot.ClaudeCodeVersion,
			Architectures: snapshot.Architectures, Status: snapshot.Status,
			VerificationState:          snapshot.VerificationState,
			VerificationEvidenceDigest: snapshot.VerificationEvidenceDigest,
			ApprovedAt:                 snapshot.ApprovedAt.UTC().Format(time.RFC3339Nano),
		},
	}
}

func writeWorkerRelease(writer http.ResponseWriter, status int, requestID string, snapshot internalworkerrelease.Snapshot) {
	body, err := platformv1alpha1.EncodeWorkerReleaseResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.WorkerRelease]{Value: workerReleaseResource(snapshot)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}

func adminWorkerReleasePath(path string) (tenantID, projectID string, ok bool) {
	if !strings.HasPrefix(path, AdminEnvironmentLeaseRoutePrefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, AdminEnvironmentLeaseRoutePrefix), "/")
	if len(parts) == 4 && parts[0] != "" && parts[1] == "projects" && parts[2] != "" && parts[3] == "worker-releases" {
		return parts[0], parts[2], true
	}
	return "", "", false
}

func HandlesAdminWorkerReleasePath(path string) bool {
	_, _, ok := adminWorkerReleasePath(path)
	return ok
}

func writeWorkerReleaseError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrWorkerReleaseIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrWorkerReleaseConflict), errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "worker_release_conflict")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
