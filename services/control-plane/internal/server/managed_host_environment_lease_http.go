package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const (
	ManagedHostEnvironmentLeaseRoutePrefix = "/v1/managed-host/tenants/"
	AdminEnvironmentLeaseRoutePrefix       = "/v1/admin/tenants/"
	environmentActuationTimeout            = 2 * time.Minute
)

type managedHostEnvironmentLeaseStore interface {
	CreateManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CreateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error)
	GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error)
	ListManagedHostEnvironmentLeases(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.ManagedHostEnvironmentLeasePage, error)
	ListAdminWorkers(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.AdminWorkerPage, error)
	PreviewAdminEnvironmentLeaseUpgrade(context.Context, string, *authn.VerifiedPrincipal, string, string, string, string) (internalmanagedhost.AdminEnvironmentLeaseUpgradePreview, error)
	BeginAdminEnvironmentLeaseUpgrade(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.AdminEnvironmentLeaseUpgradeInput) (postgres.AdminEnvironmentLeaseUpgradeStart, error)
	CompleteAdminEnvironmentLeaseUpgrade(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CompleteAdminEnvironmentLeaseUpgradeInput) (postgres.AdminEnvironmentLeaseUpgradeResult, error)
	BeginManagedHostEnvironmentLeaseUpgrade(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.UpgradeEnvironmentLeaseInput) (internalmanagedhost.UpgradeStart, error)
	TerminateManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.TerminateEnvironmentLeaseInput) (internalmanagedhost.Snapshot, error)
	CompleteManagedHostEnvironmentLeaseTermination(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CompleteEnvironmentLeaseTerminationInput) (internalmanagedhost.Snapshot, error)
	CompleteManagedHostEnvironmentLeaseDeployment(context.Context, string, *authn.VerifiedPrincipal, internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput) (internalmanagedhost.Snapshot, error)
	GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error)
}

type ManagedHostEnvironmentLeaseHTTPServer struct {
	verifier AccessTokenVerifier
	store    managedHostEnvironmentLeaseStore
	environmentActuator
	admin bool
}

func NewManagedHostEnvironmentLeaseHTTPServer(verifier AccessTokenVerifier, store managedHostEnvironmentLeaseStore, dockerCredentials *dockertarget.CredentialDirectory, kubernetesCredentials *kubernetestarget.CredentialDirectory, sshCredentials *sshtarget.CredentialDirectory, workerTrust dockertarget.WorkerTrust) (*ManagedHostEnvironmentLeaseHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("managed host environment lease HTTP server configuration is invalid")
	}
	return &ManagedHostEnvironmentLeaseHTTPServer{verifier: verifier, store: store, environmentActuator: newEnvironmentActuator(dockerCredentials, kubernetesCredentials, sshCredentials, workerTrust)}, nil
}

func NewAdminEnvironmentLeaseHTTPServer(verifier AccessTokenVerifier, store managedHostEnvironmentLeaseStore, dockerCredentials *dockertarget.CredentialDirectory, kubernetesCredentials *kubernetestarget.CredentialDirectory, sshCredentials *sshtarget.CredentialDirectory, workerTrust dockertarget.WorkerTrust) (*ManagedHostEnvironmentLeaseHTTPServer, error) {
	server, err := NewManagedHostEnvironmentLeaseHTTPServer(verifier, store, dockerCredentials, kubernetesCredentials, sshCredentials, workerTrust)
	if err != nil {
		return nil, err
	}
	server.admin = true
	return server, nil
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, leaseID, action, ok := managedHostEnvironmentLeasePath(request.URL.Path)
	if server.admin {
		tenantID, projectID, leaseID, action, ok = adminEnvironmentLeasePath(request.URL.Path)
	}
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
	permission, allowed := environmentLeasePermission(action, request.Method, server.admin)
	if !allowed {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	projectPermission := "projects.get"
	if request.Method != http.MethodGet {
		projectPermission = "projects.act"
	}
	if _, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: projectPermission}); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission}); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch {
	case action == "worker-health" && request.Method == http.MethodGet:
		server.getWorkerHealth(writer, request, tenantID, projectID, leaseID, requestID, bearer)
	case action == "worker-collection" && request.Method == http.MethodGet:
		server.listWorkers(writer, request, tenantID, projectID, requestID, bearer)
	case action == "collection" && request.Method == http.MethodGet:
		server.list(writer, request, tenantID, projectID, requestID, bearer)
	case action == "collection" && request.Method == http.MethodPost:
		server.create(writer, request, tenantID, projectID, requestID, bearer)
	case action == "get" && request.Method == http.MethodGet:
		server.get(writer, request, tenantID, projectID, leaseID, requestID, bearer)
	case (action == "upgrade-preview" || action == "rollback-preview") && request.Method == http.MethodGet:
		server.previewAdminUpgrade(writer, request, tenantID, projectID, leaseID, action, requestID, bearer)
	case action == "terminate" && request.Method == http.MethodPost:
		server.terminate(writer, request, tenantID, projectID, leaseID, requestID, bearer)
	case (action == "upgrade" || action == "rollback") && request.Method == http.MethodPost:
		server.upgrade(writer, request, tenantID, projectID, leaseID, action, requestID, bearer)
	default:
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) listWorkers(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	writer.Header().Set("Cache-Control", "no-store")
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListAdminWorkersServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterWorkerID := ""
	if validated.PageToken != "" {
		if afterWorkerID, ok = decodeAdminWorkerPageToken(validated.TenantID, validated.ProjectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: validated.TenantID, ResourceLevel: "project", ResourceID: validated.ProjectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListAdminWorkers(request.Context(), validated.TenantID, principal, validated.ProjectID, afterWorkerID, validated.PageSize)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeAdminWorkerPage(writer, requestID, tenantID, projectID, page)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) previewAdminUpgrade(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, routeAction, requestID, bearer string) {
	action, releaseDigest := "upgrade", ""
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if routeAction == "rollback-preview" {
		action = "rollback"
		if len(query) != 0 {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		if _, err = openapiv1alpha1.ValidateGetEnvironmentLeaseServerRequest(tenantID, projectID, leaseID, requestID); err != nil {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	} else {
		values, found := query["releaseDigest"]
		if len(query) != 1 || !found || len(values) != 1 {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
		releaseDigest = values[0]
		if _, err = openapiv1alpha1.ValidatePreviewAdminEnvironmentLeaseUpgradeServerRequest(tenantID, projectID, leaseID, releaseDigest, requestID); err != nil {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	preview, err := server.store.PreviewAdminEnvironmentLeaseUpgrade(request.Context(), tenantID, principal, projectID, leaseID, action, releaseDigest)
	if err != nil {
		status, code := adminEnvironmentLeaseUpgradeErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeAdminEnvironmentLeaseUpgradePreview(writer, requestID, preview)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) adminUpgrade(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, action, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateAdminEnvironmentLeaseUpgradeServerRequest(tenantID, projectID, leaseID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	readPrincipal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	actPrincipal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	resourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input := internalmanagedhost.AdminEnvironmentLeaseUpgradeInput{
		Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, LeaseID: leaseID, Action: action,
		ReleaseDigest: validated.Body.ReleaseDigest, ExpectedGeneration: validated.Body.ExpectedGeneration,
		ExpectedResourceVersion: resourceVersion, ImpactDigest: validated.Body.ImpactDigest,
		Mutation: internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	}
	started, err := server.store.BeginAdminEnvironmentLeaseUpgrade(request.Context(), tenantID, actPrincipal, input)
	if err != nil {
		status, code := adminEnvironmentLeaseUpgradeErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if !started.Execute {
		writeMaintenanceOperation(writer, http.StatusOK, requestID, started.Operation)
		return
	}
	completion := internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput{
		Scope: started.Snapshot.Scope, LeaseID: started.Snapshot.LeaseID, TargetID: started.Snapshot.TargetID,
		ExpectedGeneration: started.Snapshot.Generation, ExpectedTargetGeneration: started.Snapshot.TargetGeneration,
	}
	target, targetErr := server.store.GetDeploymentTarget(request.Context(), tenantID, readPrincipal, projectID, started.Snapshot.TargetID)
	if targetErr != nil {
		completion.StableErrorCode = "deployment-target-unavailable"
	} else if target.SchedulingState != "drained" {
		completion.StableErrorCode = "deployment-target-not-drained"
	} else {
		completion = server.completeEnvironmentDeployment(request.Context(), tenantID, projectID, started.Snapshot, target, true)
		if completion.Succeeded && (target.Kind == "docker" || target.Kind == "ssh") {
			cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(request.Context()), environmentActuationTimeout)
			cleanupErr := server.environmentActuator.cleanupOlderEnvironmentWorkers(cleanupContext, tenantID, projectID, target, started.Snapshot)
			cancelCleanup()
			if cleanupErr != nil {
				completion.Succeeded, completion.WorkerEndpoint, completion.WorkerSPIFFEID, completion.WorkerServerName = false, "", "", ""
				completion.StableErrorCode = "environment-upgrade-cleanup-failed"
				if errors.Is(cleanupErr, dockertarget.ErrDeploymentConflict) || errors.Is(cleanupErr, sshtarget.ErrDeploymentConflict) {
					completion.StableErrorCode = "environment-upgrade-cleanup-conflict"
				}
			}
		}
	}
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
	defer cancel()
	actPrincipal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.CompleteAdminEnvironmentLeaseUpgrade(completionContext, tenantID, actPrincipal, internalmanagedhost.CompleteAdminEnvironmentLeaseUpgradeInput{Upgrade: input, Deployment: completion, ImpactSummary: started.Operation.ImpactSummary})
	if err != nil {
		status, code := adminEnvironmentLeaseUpgradeErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeMaintenanceOperation(writer, http.StatusOK, requestID, result.Operation)
}

func (server *ManagedHostEnvironmentLeaseHTTPServer) upgrade(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, action, requestID, bearer string) {
	if server.admin {
		server.adminUpgrade(writer, request, tenantID, projectID, leaseID, action, requestID, bearer)
		return
	}
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
	validated, err := openapiv1alpha1.ValidateUpgradeEnvironmentLeaseServerRequest(tenantID, projectID, leaseID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	input := internalmanagedhost.UpgradeEnvironmentLeaseInput{
		Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, LeaseID: leaseID,
		ReleaseDigest: validated.Body.ReleaseDigest, ExpectedGeneration: validated.Body.ExpectedGeneration,
		Mutation: internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	}
	started, err := server.store.BeginManagedHostEnvironmentLeaseUpgrade(request.Context(), tenantID, principal, input)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if !started.Execute {
		writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, started.Snapshot)
		return
	}
	principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	target, targetErr := server.store.GetDeploymentTarget(request.Context(), tenantID, principal, projectID, started.Snapshot.TargetID)
	if targetErr != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(targetErr)
		writePublicProblem(writer, status, code)
		return
	}
	completion := server.completeEnvironmentDeployment(request.Context(), tenantID, projectID, started.Snapshot, target, true)
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
	defer cancel()
	principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.CompleteManagedHostEnvironmentLeaseDeployment(completionContext, tenantID, principal, completion)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if completion.Succeeded && (target.Kind == "docker" || target.Kind == "ssh") {
		cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(request.Context()), environmentActuationTimeout)
		cleanupErr := server.environmentActuator.cleanupOlderEnvironmentWorkers(cleanupContext, tenantID, projectID, target, result)
		cancelCleanup()
		if cleanupErr != nil {
			if errors.Is(cleanupErr, dockertarget.ErrDeploymentConflict) || errors.Is(cleanupErr, sshtarget.ErrDeploymentConflict) {
				writePublicProblem(writer, http.StatusConflict, "environment_upgrade_cleanup_conflict")
			} else {
				writePublicProblem(writer, http.StatusBadGateway, "environment_upgrade_cleanup_failed")
			}
			return
		}
	}
	writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, result)
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
	result, err = server.deployEnvironment(request.Context(), tenantID, projectID, bearer, result)
	if err != nil {
		if errors.Is(err, errEnvironmentActuationAuthentication) {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeManagedHostEnvironmentLease(writer, http.StatusCreated, requestID, result)
}

var errEnvironmentActuationAuthentication = errors.New("environment actuation authentication failed")

func (server *ManagedHostEnvironmentLeaseHTTPServer) deployEnvironment(ctx context.Context, tenantID, projectID, bearer string, result internalmanagedhost.Snapshot) (internalmanagedhost.Snapshot, error) {
	if server.dockerCredentials == nil && server.kubernetesCredentials == nil && server.sshCredentials == nil || result.ObservedPhase != "provisioning" && result.ObservedPhase != "failed" {
		return result, nil
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		return internalmanagedhost.Snapshot{}, errEnvironmentActuationAuthentication
	}
	target, err := server.store.GetDeploymentTarget(ctx, tenantID, principal, projectID, result.TargetID)
	if err != nil {
		return internalmanagedhost.Snapshot{}, err
	}
	completion := server.completeEnvironmentDeployment(ctx, tenantID, projectID, result, target, false)
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), deploymentTargetCompletionTimeout)
	defer cancel()
	principal, err = server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if err != nil {
		return internalmanagedhost.Snapshot{}, errEnvironmentActuationAuthentication
	}
	return server.store.CompleteManagedHostEnvironmentLeaseDeployment(completionContext, tenantID, principal, completion)
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
	result, err := server.terminateEnvironment(request.Context(), tenantID, projectID, leaseID, bearer, validated.Body.ExpectedGeneration, internalmanagedhost.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey})
	if err != nil {
		status, code := environmentTerminationErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	writeManagedHostEnvironmentLease(writer, http.StatusOK, requestID, result)
}

var (
	errEnvironmentCleanupUnavailable = errors.New("environment cleanup actuator is unavailable")
	errEnvironmentCleanupConflict    = errors.New("environment cleanup authority conflicts with current state")
	errEnvironmentCleanupFailed      = errors.New("environment cleanup failed")
)

func (server *ManagedHostEnvironmentLeaseHTTPServer) terminateEnvironment(ctx context.Context, tenantID, projectID, environmentID, bearer string, expectedGeneration int64, mutation internalmanagedhost.Mutation) (internalmanagedhost.Snapshot, error) {
	var result internalmanagedhost.Snapshot
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		principal, verifyErr := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
		if verifyErr != nil {
			return internalmanagedhost.Snapshot{}, errEnvironmentActuationAuthentication
		}
		result, err = server.store.TerminateManagedHostEnvironmentLease(ctx, tenantID, principal, internalmanagedhost.TerminateEnvironmentLeaseInput{Scope: internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, LeaseID: environmentID, ExpectedGeneration: expectedGeneration, Mutation: mutation})
		if !errors.Is(err, postgres.ErrCoordinationRejected) {
			break
		}
	}
	if err != nil || result.CleanupPhase == "complete" {
		return result, err
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), environmentActuationTimeout)
	defer cancelCleanup()
	if result.WorkerEndpoint != "" && server.dockerCredentials == nil && server.kubernetesCredentials == nil && server.sshCredentials == nil {
		return internalmanagedhost.Snapshot{}, errEnvironmentCleanupUnavailable
	}
	if result.TargetID != "" && result.ProviderCredentialRef != "" && (result.WorkerEndpoint != "" || server.dockerCredentials != nil || server.kubernetesCredentials != nil || server.sshCredentials != nil) {
		readPrincipal, verifyErr := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
		if verifyErr != nil {
			return internalmanagedhost.Snapshot{}, errEnvironmentActuationAuthentication
		}
		target, targetErr := server.store.GetDeploymentTarget(cleanupContext, tenantID, readPrincipal, projectID, result.TargetID)
		if targetErr != nil {
			return internalmanagedhost.Snapshot{}, targetErr
		}
		if target.Generation != result.TargetGeneration {
			return internalmanagedhost.Snapshot{}, errEnvironmentCleanupConflict
		}
		var cleanupErr error
		switch target.Kind {
		case "docker":
			if server.dockerCredentials == nil {
				return internalmanagedhost.Snapshot{}, errEnvironmentCleanupUnavailable
			}
			cleanupErr = server.dockerCredentials.CleanupWorker(cleanupContext, target.Endpoint, target.CredentialRef, dockerDeployRequest(tenantID, projectID, result, expectedGeneration))
		case "kubernetes":
			if server.kubernetesCredentials == nil {
				return internalmanagedhost.Snapshot{}, errEnvironmentCleanupUnavailable
			}
			cleanupErr = server.kubernetesCredentials.CleanupWorker(cleanupContext, target.Endpoint, target.CredentialRef, kubernetesDeployRequest(tenantID, projectID, result, expectedGeneration))
		case "ssh":
			if server.sshCredentials == nil {
				return internalmanagedhost.Snapshot{}, errEnvironmentCleanupUnavailable
			}
			cleanupErr = server.sshCredentials.CleanupWorker(cleanupContext, target.Endpoint, target.CredentialRef, dockerDeployRequest(tenantID, projectID, result, expectedGeneration))
		default:
			cleanupErr = errEnvironmentCleanupConflict
		}
		if errors.Is(cleanupErr, dockertarget.ErrDeploymentConflict) || errors.Is(cleanupErr, kubernetestarget.ErrDeploymentConflict) || errors.Is(cleanupErr, sshtarget.ErrDeploymentConflict) {
			return internalmanagedhost.Snapshot{}, errEnvironmentCleanupConflict
		}
		if cleanupErr != nil {
			return internalmanagedhost.Snapshot{}, errEnvironmentCleanupFailed
		}
	} else if result.WorkerEndpoint != "" {
		return internalmanagedhost.Snapshot{}, errEnvironmentCleanupUnavailable
	}
	completionPrincipal, verifyErr := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.act"})
	if verifyErr != nil {
		return internalmanagedhost.Snapshot{}, errEnvironmentActuationAuthentication
	}
	completionContext, cancelCompletion := context.WithTimeout(context.WithoutCancel(ctx), deploymentTargetCompletionTimeout)
	defer cancelCompletion()
	return server.store.CompleteManagedHostEnvironmentLeaseTermination(completionContext, tenantID, completionPrincipal, internalmanagedhost.CompleteEnvironmentLeaseTerminationInput{Scope: result.Scope, LeaseID: result.LeaseID, ExpectedGeneration: result.Generation})
}
func managedHostEnvironmentLeaseErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedHostEnvironmentLeaseNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "lease_conflict"
	case errors.Is(err, postgres.ErrProjectConcurrentLeaseQuotaExceeded):
		return http.StatusConflict, "project_lease_count_quota_exceeded"
	case errors.Is(err, postgres.ErrProjectLeaseCPUQuotaExceeded):
		return http.StatusConflict, "project_lease_cpu_quota_exceeded"
	case errors.Is(err, postgres.ErrProjectLeaseMemoryQuotaExceeded):
		return http.StatusConflict, "project_lease_memory_quota_exceeded"
	case errors.Is(err, postgres.ErrProjectLeaseTTLQuotaExceeded):
		return http.StatusConflict, "project_lease_ttl_quota_exceeded"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func adminEnvironmentLeaseUpgradeErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, postgres.ErrManagedHostEnvironmentLeaseNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, postgres.ErrMutationDenied):
		return http.StatusForbidden, "authorization_denied"
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseUpgradeIdempotencyConflict):
		return http.StatusConflict, "idempotency_conflict"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseGenerationConflict):
		return http.StatusConflict, "lease_generation_conflict"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseResourceVersionConflict):
		return http.StatusConflict, "lease_resource_version_conflict"
	case errors.Is(err, postgres.ErrCoordinationRejected):
		return http.StatusConflict, "lease_resource_version_conflict"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseStateConflict):
		return http.StatusConflict, "lease_state_conflict"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseTargetNotDrained):
		return http.StatusConflict, "target_not_drained"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseRollbackUnavailable):
		return http.StatusConflict, "rollback_unavailable"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseImpactConflict):
		return http.StatusConflict, "upgrade_impact_conflict"
	case errors.Is(err, postgres.ErrAdminEnvironmentLeaseReleaseNotApproved):
		return http.StatusConflict, "worker_release_not_approved"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}
