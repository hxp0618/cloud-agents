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
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const ManagedHostEnvironmentLeaseRoutePrefix = "/v1/managed-host/tenants/"

type managedHostEnvironmentLeaseStore interface {
	CreateManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CreateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error)
	GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error)
	ListManagedHostEnvironmentLeases(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.ManagedHostEnvironmentLeasePage, error)
	TerminateManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.TerminateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error)
	CompleteManagedHostEnvironmentLeaseTermination(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CompleteEnvironmentLeaseTerminationInput) (internalmanagedhost.Snapshot, error)
	CompleteManagedHostEnvironmentLeaseDeployment(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput) (internalmanagedhost.Snapshot, error)
	GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error)
}

type ManagedHostEnvironmentLeaseHTTPServer struct {
	verifier          AccessTokenVerifier
	store             managedHostEnvironmentLeaseStore
	dockerCredentials *dockertarget.CredentialDirectory
	workerTrust       dockertarget.WorkerTrust
}

func NewManagedHostEnvironmentLeaseHTTPServer(verifier AccessTokenVerifier, store managedHostEnvironmentLeaseStore, dockerCredentials *dockertarget.CredentialDirectory, workerTrust dockertarget.WorkerTrust) (*ManagedHostEnvironmentLeaseHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("managed host environment lease HTTP server configuration is invalid")
	}
	return &ManagedHostEnvironmentLeaseHTTPServer{verifier: verifier, store: store, dockerCredentials: dockerCredentials, workerTrust: workerTrust}, nil
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, leaseID, action, ok := managedHostEnvironmentLeasePath(request.URL.Path)
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
	switch {
	case action == "collection" && request.Method == http.MethodGet:
		server.list(writer, request, tenantID, projectID, requestID, bearer)
	case action == "collection" && request.Method == http.MethodPost:
		server.create(writer, request, tenantID, projectID, requestID, bearer)
	case action == "get" && request.Method == http.MethodGet:
		server.get(writer, request, tenantID, projectID, leaseID, requestID, bearer)
	case action == "terminate" && request.Method == http.MethodPost:
		server.terminate(writer, request, tenantID, projectID, leaseID, requestID, bearer)
	default:
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListManagedHostEnvironmentLeasesServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterLeaseID := ""
	if validated.PageToken != "" {
		if afterLeaseID, ok = decodeManagedHostEnvironmentLeasePageToken(validated.TenantID, validated.ProjectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ProjectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListManagedHostEnvironmentLeases(request.Context(), validated.TenantID, principal, validated.ProjectID, afterLeaseID, validated.PageSize)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeManagedHostEnvironmentLeasePage(writer, requestID, tenantID, projectID, page)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) create(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateCreateEnvironmentLeaseServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.CreateManagedHostEnvironmentLease(request.Context(), tenantID, principal, internalmanagedhost.CreateEnvironmentLeaseInput{Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, LeaseID: validated.Body.LeaseID, LeaseName: validated.Body.LeaseName, ReleaseDigest: validated.Body.ReleaseDigest, TargetID: validated.Body.TargetID, ProviderCredentialRef: validated.Body.ProviderCredentialRef, CPULimitMillis: validated.Body.CPULimitMillis, MemoryLimitBytes: validated.Body.MemoryLimitBytes, TTLSeconds: validated.Body.TTLSeconds, ExpectedTargetGeneration: validated.Body.ExpectedTargetGeneration, Mutation: internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey}})
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if server.dockerCredentials != nil && (result.ObservedPhase == "provisioning" || result.ObservedPhase == "failed") {
		principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
		if err != nil {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		target, targetErr := server.store.GetDeploymentTarget(request.Context(), tenantID, principal, projectID, result.TargetID)
		completion := internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput{Scope: result.Scope, LeaseID: result.LeaseID, TargetID: result.TargetID, ExpectedGeneration: result.Generation, ExpectedTargetGeneration: result.TargetGeneration}
		if targetErr != nil {
			status, code := managedHostEnvironmentLeaseErrorStatus(targetErr)
			writePublicProblem(writer, status, code)
			return
		}
		if target.Generation != result.TargetGeneration || target.ObservedPhase != "ready" || target.Kind != "docker" {
			completion.StableErrorCode = "docker-target-not-ready"
		} else if deployed, deployErr := server.dockerCredentials.DeployWorker(request.Context(), target.Endpoint, target.CredentialRef, dockertarget.DeployRequest{
			TenantID: tenantID, ProjectID: projectID, TargetID: target.TargetID, LeaseID: result.LeaseID,
			TargetGeneration: result.TargetGeneration, LeaseGeneration: result.Generation,
			ReleaseDigest: result.ReleaseDigest, ProviderCredentialRef: result.ProviderCredentialRef,
			CPULimitMillis: result.CPULimitMillis, MemoryLimitBytes: result.MemoryLimitBytes,
		}, server.workerTrust); deployErr != nil {
			completion.StableErrorCode = dockerDeploymentErrorCode(deployErr)
		} else {
			completion.Succeeded, completion.WorkerEndpoint, completion.WorkerSPIFFEID, completion.WorkerServerName = true, deployed.Endpoint, deployed.WorkerSPIFFEID, deployed.WorkerServerName
		}
		completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
		defer cancel()
		principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
		if err != nil {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		result, err = server.store.CompleteManagedHostEnvironmentLeaseDeployment(completionContext, tenantID, principal, completion)
		if err != nil {
			status, code := managedHostEnvironmentLeaseErrorStatus(err)
			writePublicProblem(writer, status, code)
			return
		}
	}
	writeManagedHostEnvironmentLease(writer, http.StatusCreated, requestID, result)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, requestID, bearer string) {
	if _, err := openapiv1alpha1.ValidateGetEnvironmentLeaseServerRequest(tenantID, projectID, leaseID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.GetManagedHostEnvironmentLease(request.Context(), tenantID, principal, projectID, leaseID)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, result)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) terminate(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateTerminateEnvironmentLeaseServerRequest(tenantID, projectID, leaseID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.TerminateManagedHostEnvironmentLease(request.Context(), tenantID, principal, internalmanagedhost.TerminateEnvironmentLeaseInput{Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, LeaseID: leaseID, ExpectedGeneration: validated.Body.ExpectedGeneration, Mutation: internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey}})
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if result.CleanupPhase == "complete" {
		writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, result)
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
	defer cancel()
	if server.dockerCredentials != nil && result.TargetID != "" && result.ProviderCredentialRef != "" {
		principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
		if err != nil {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		target, targetErr := server.store.GetDeploymentTarget(cleanupContext, tenantID, principal, projectID, result.TargetID)
		if targetErr != nil {
			writeDeploymentTargetError(writer, targetErr)
			return
		}
		if target.Kind != "docker" || target.Generation != result.TargetGeneration {
			writePublicProblem(writer, http.StatusConflict, "lease_conflict")
			return
		}
		cleanupErr := server.dockerCredentials.CleanupWorker(cleanupContext, target.Endpoint, target.CredentialRef, dockertarget.DeployRequest{
			TenantID: tenantID, ProjectID: projectID, TargetID: result.TargetID, LeaseID: result.LeaseID,
			TargetGeneration: result.TargetGeneration, LeaseGeneration: validated.Body.ExpectedGeneration,
			ReleaseDigest: result.ReleaseDigest, ProviderCredentialRef: result.ProviderCredentialRef,
			CPULimitMillis: result.CPULimitMillis, MemoryLimitBytes: result.MemoryLimitBytes,
		})
		if cleanupErr != nil {
			if errors.Is(cleanupErr, dockertarget.ErrDeploymentConflict) {
				writePublicProblem(writer, http.StatusConflict, "environment_cleanup_conflict")
			} else {
				writePublicProblem(writer, http.StatusBadGateway, "environment_cleanup_failed")
			}
			return
		}
	} else if result.WorkerEndpoint != "" {
		writePublicProblem(writer, http.StatusServiceUnavailable, "environment_cleanup_unavailable")
		return
	}
	principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err = server.store.CompleteManagedHostEnvironmentLeaseTermination(cleanupContext, tenantID, principal, internalmanagedhost.CompleteEnvironmentLeaseTerminationInput{Scope: result.Scope, LeaseID: result.LeaseID, ExpectedGeneration: result.Generation})
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, result)
}

func writeManagedHostEnvironmentLease(writer http.ResponseWriter, status int, requestID string, snapshot internalmanagedhost.Snapshot) {
	value := managedHostEnvironmentLeaseResource(snapshot)
	body, err := openapiv1alpha1.EncodeEnvironmentLeaseResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentLease]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func managedHostEnvironmentLeaseResource(snapshot internalmanagedhost.Snapshot) platformv1alpha1.EnvironmentLease {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID}
	return platformv1alpha1.EnvironmentLease{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "CloudEnvironmentLease", Metadata: commonv1alpha1.ResourceMetadata{UID: snapshot.LeaseID, Name: snapshot.LeaseName, TenantRef: tenant, ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)}}, Spec: platformv1alpha1.EnvironmentLeaseSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID}, Generation: snapshot.Generation, DesiredPhase: snapshot.DesiredPhase, ObservedPhase: snapshot.ObservedPhase, CleanupPhase: snapshot.CleanupPhase, EnvironmentID: snapshot.EnvironmentID, ReleaseDigest: snapshot.ReleaseDigest, TargetID: snapshot.TargetID, TargetGeneration: snapshot.TargetGeneration, ProviderCredentialRef: snapshot.ProviderCredentialRef, CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes, WorkerEndpoint: snapshot.WorkerEndpoint, WorkerSPIFFEID: snapshot.WorkerSPIFFEID, WorkerServerName: snapshot.WorkerServerName, StableErrorCode: snapshot.StableErrorCode, ExpiresAt: snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano)}}
}

func dockerDeploymentErrorCode(err error) string {
	switch {
	case errors.Is(err, dockertarget.ErrDeploymentConfigUnavailable):
		return "docker-deployment-config-unavailable"
	case errors.Is(err, dockertarget.ErrDeploymentConfigInvalid):
		return "docker-deployment-config-invalid"
	case errors.Is(err, dockertarget.ErrDeploymentConflict):
		return "docker-deployment-conflict"
	case errors.Is(err, dockertarget.ErrWorkerUnavailable):
		return "docker-worker-unavailable"
	default:
		return "docker-deployment-failed"
	}
}

func writeManagedHostEnvironmentLeasePage(writer http.ResponseWriter, requestID, tenantID, projectID string, page postgres.ManagedHostEnvironmentLeasePage) {
	leases := make([]platformv1alpha1.EnvironmentLease, 0, len(page.EnvironmentLeases))
	for _, snapshot := range page.EnvironmentLeases {
		leases = append(leases, managedHostEnvironmentLeaseResource(snapshot))
	}
	nextPageToken := ""
	if page.NextLeaseID != "" {
		var ok bool
		nextPageToken, ok = encodeManagedHostEnvironmentLeasePageToken(tenantID, projectID, page.NextLeaseID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeEnvironmentLeasePageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentLeasePage]{Value: platformv1alpha1.EnvironmentLeasePage{APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentLeasePage", EnvironmentLeases: leases, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func managedHostEnvironmentLeasePath(path string) (tenantID, projectID, leaseID, action string, ok bool) {
	if !strings.HasPrefix(path, ManagedHostEnvironmentLeaseRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ManagedHostEnvironmentLeaseRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "environment-leases" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && parts[0] != "" && parts[2] != "" && parts[4] != "" && !strings.Contains(parts[4], ":") {
		return parts[0], parts[2], parts[4], "get", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "environment-leases" && strings.HasSuffix(parts[4], ":terminate") {
		leaseID = strings.TrimSuffix(parts[4], ":terminate")
		if leaseID != "" {
			return parts[0], parts[2], leaseID, "terminate", true
		}
	}
	return "", "", "", "", false
}

func encodeManagedHostEnvironmentLeasePageToken(tenantID, projectID, leaseID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(leaseID, "/leaseId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("environment-lease/v1\x00" + tenantID + "\x00" + projectID + "\x00" + leaseID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeManagedHostEnvironmentLeasePageToken(tenantID, projectID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "environment-lease/v1" || parts[1] != tenantID || parts[2] != projectID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/projectId") != nil || commonv1alpha1.ValidateIdentifier(parts[3], "/leaseId") != nil {
		return "", false
	}
	return parts[3], true
}

func HandlesManagedHostEnvironmentLeasePath(path string) bool {
	_, _, _, _, ok := managedHostEnvironmentLeasePath(path)
	return ok
}

func managedHostEnvironmentLeaseErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedHostEnvironmentLeaseNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "lease_conflict"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
