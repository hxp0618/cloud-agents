package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const deploymentTargetCompletionTimeout = 5 * time.Second

const adminDeploymentTargetRoutePrefix = "/v1/admin/tenants/"

var (
	errDeploymentTargetCleanupUnavailable = errors.New("deployment target cleanup actuator is unavailable")
	errDeploymentTargetCleanupConflict    = errors.New("deployment target cleanup authority conflicts with current state")
)

type deploymentTargetStore interface {
	RegisterDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.RegisterInput) (internaldeploymenttarget.Snapshot, error)
	GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error)
	ListDeploymentTargets(context.Context, string, *authn.VerifiedPrincipal, string, string, int) (postgres.DeploymentTargetPage, error)
	ListDeploymentTargetOperations(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.DeploymentTargetOperationPage, error)
	ListMaintenanceOperations(context.Context, string, *authn.VerifiedPrincipal, string, *time.Time, string, string, int) (postgres.DeploymentTargetOperationPage, error)
	ListDeploymentTargetAuditEvents(context.Context, string, *authn.VerifiedPrincipal, string, string, *time.Time, string, int) (postgres.DeploymentTargetAuditPage, error)
	GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error)
	BeginDeploymentTargetProbe(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.ProbeInput) (internaldeploymenttarget.ProbeStart, error)
	CompleteDeploymentTargetProbe(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.ProbeCompletion) (internaldeploymenttarget.Snapshot, error)
	BeginDeploymentTargetCleanup(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.CleanupInput) (internaldeploymenttarget.CleanupStart, error)
	CompleteDeploymentTargetCleanup(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.CleanupCompletion) (internaldeploymenttarget.Operation, error)
}

type DeploymentTargetHTTPServer struct {
	verifier         AccessTokenVerifier
	store            deploymentTargetStore
	dockerProber     *dockertarget.CredentialDirectory
	kubernetesProber *kubernetestarget.CredentialDirectory
	sshProber        *sshtarget.CredentialDirectory
	admin            bool
}

type managedDeploymentTargetWorker struct {
	tenantID, projectID, targetID, leaseID, workerName string
	targetGeneration, leaseGeneration                  int64
	resources                                          []platformv1alpha1.DeploymentTargetCleanupResource
	cleanup                                            func(context.Context) error
}

type deploymentTargetCleanupPlan struct {
	workers        []managedDeploymentTargetWorker
	previewWorkers []platformv1alpha1.DeploymentTargetCleanupWorker
	impactDigest   string
	resourceCount  int
	canCleanup     bool
}

func NewDeploymentTargetHTTPServer(verifier AccessTokenVerifier, store deploymentTargetStore, dockerProber *dockertarget.CredentialDirectory, kubernetesProber *kubernetestarget.CredentialDirectory, sshProber *sshtarget.CredentialDirectory) (*DeploymentTargetHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("deployment target HTTP server configuration is invalid")
	}
	return &DeploymentTargetHTTPServer{verifier: verifier, store: store, dockerProber: dockerProber, kubernetesProber: kubernetesProber, sshProber: sshProber}, nil
}

func NewAdminDeploymentTargetHTTPServer(verifier AccessTokenVerifier, store deploymentTargetStore, dockerProber *dockertarget.CredentialDirectory, kubernetesProber *kubernetestarget.CredentialDirectory, sshProber *sshtarget.CredentialDirectory) (*DeploymentTargetHTTPServer, error) {
	server, err := NewDeploymentTargetHTTPServer(verifier, store, dockerProber, kubernetesProber, sshProber)
	if err != nil {
		return nil, err
	}
	server.admin = true
	return server, nil
}

func (server *DeploymentTargetHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, targetID, action, ok := deploymentTargetPath(request.URL.Path)
	if server.admin {
		tenantID, projectID, targetID, action, ok = adminDeploymentTargetPath(request.URL.Path)
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
	permission, allowed := deploymentTargetAdminPermission(action, request.Method)
	if !allowed {
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	projectPermission := "projects.get"
	if request.Method != http.MethodGet {
		projectPermission = "projects.act"
	}
	if _, err := server.verify(bearer, tenantID, projectID, projectPermission); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	if _, err := server.verify(bearer, tenantID, projectID, permission); err != nil {
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
		return
	}
	switch {
	case action == "collection" && request.Method == http.MethodGet:
		server.list(writer, request, tenantID, projectID, requestID, bearer)
	case action == "collection" && request.Method == http.MethodPost:
		server.register(writer, request, tenantID, projectID, requestID, bearer)
	case action == "get" && request.Method == http.MethodGet:
		server.get(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "operations" && request.Method == http.MethodGet:
		server.listOperations(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "maintenance-operations" && request.Method == http.MethodGet:
		server.listOperations(writer, request, tenantID, projectID, "", requestID, bearer)
	case action == "audit-events" && request.Method == http.MethodGet:
		server.listAuditEvents(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "probe" && request.Method == http.MethodPost:
		server.probe(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "cleanup-preview" && request.Method == http.MethodGet:
		server.cleanupPreview(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "cleanup" && request.Method == http.MethodPost:
		if server.admin {
			server.cleanupAdmin(writer, request, tenantID, projectID, targetID, requestID, bearer)
		} else {
			server.cleanup(writer, request, tenantID, projectID, targetID, requestID, bearer)
		}
	default:
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (server *DeploymentTargetHTTPServer) listOperations(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListDeploymentTargetOperationsServerRequest(tenantID, projectID, targetID, requestID, pageSize, pageToken)
	if targetID == "" {
		validated, err = openapiv1alpha1.ValidateListMaintenanceOperationsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	}
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var after *time.Time
	afterTargetID := ""
	afterID := ""
	if validated.PageToken != "" {
		if targetID == "" {
			after, afterTargetID, afterID, ok = decodeMaintenanceOperationPageToken(tenantID, projectID, validated.PageToken)
		} else {
			after, afterID, ok = decodeDeploymentTargetActivityPageToken("operation", tenantID, projectID, targetID, validated.PageToken)
		}
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
	var page postgres.DeploymentTargetOperationPage
	if targetID == "" {
		page, err = server.store.ListMaintenanceOperations(request.Context(), tenantID, principal, projectID, after, afterTargetID, afterID, validated.PageSize)
	} else {
		page, err = server.store.ListDeploymentTargetOperations(request.Context(), tenantID, principal, projectID, targetID, after, afterID, validated.PageSize)
	}
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	nextPageToken := ""
	if page.NextRequestedAt != nil {
		if targetID == "" {
			nextPageToken, ok = encodeMaintenanceOperationPageToken(tenantID, projectID, page.NextTargetID, *page.NextRequestedAt, page.NextOperationID)
		} else {
			nextPageToken, ok = encodeDeploymentTargetActivityPageToken("operation", tenantID, projectID, targetID, *page.NextRequestedAt, page.NextOperationID)
		}
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	operations := make([]platformv1alpha1.MaintenanceOperation, 0, len(page.Operations))
	for _, operation := range page.Operations {
		operations = append(operations, maintenanceOperationResource(operation))
	}
	body, err := platformv1alpha1.EncodeMaintenanceOperationPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MaintenanceOperationPage]{Value: platformv1alpha1.MaintenanceOperationPage{APIVersion: platformv1alpha1.APIVersion, Kind: "MaintenanceOperationPage", Operations: operations, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *DeploymentTargetHTTPServer) listAuditEvents(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListDeploymentTargetAuditEventsServerRequest(tenantID, projectID, targetID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	var after *time.Time
	afterID := ""
	if validated.PageToken != "" {
		if after, afterID, ok = decodeDeploymentTargetActivityPageToken("audit", tenantID, projectID, targetID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListDeploymentTargetAuditEvents(request.Context(), tenantID, principal, projectID, targetID, after, afterID, validated.PageSize)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	nextPageToken := ""
	if page.NextOccurredAt != nil {
		if nextPageToken, ok = encodeDeploymentTargetActivityPageToken("audit", tenantID, projectID, targetID, *page.NextOccurredAt, page.NextEventID); !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	events := make([]platformv1alpha1.AdminAuditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, adminAuditEventResource(event))
	}
	body, err := platformv1alpha1.EncodeAdminAuditEventPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.AdminAuditEventPage]{Value: platformv1alpha1.AdminAuditEventPage{APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEventPage", Events: events, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func (server *DeploymentTargetHTTPServer) list(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
	pageSize, pageToken, ok := managedAgentPagination(request)
	if !ok {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	validated, err := openapiv1alpha1.ValidateListDeploymentTargetsServerRequest(tenantID, projectID, requestID, pageSize, pageToken)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	afterTargetID := ""
	if validated.PageToken != "" {
		if afterTargetID, ok = decodeDeploymentTargetPageToken(validated.TenantID, validated.ProjectID, validated.PageToken); !ok {
			writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	principal, err := server.verify(bearer, validated.TenantID, validated.ProjectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	page, err := server.store.ListDeploymentTargets(request.Context(), validated.TenantID, principal, validated.ProjectID, afterTargetID, validated.PageSize)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	writeDeploymentTargetPage(writer, requestID, tenantID, projectID, page)
}

func (server *DeploymentTargetHTTPServer) cleanup(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateCleanupDeploymentTargetServerRequest(tenantID, projectID, targetID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if _, err = server.verify(bearer, tenantID, projectID, "projects.act"); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	target, err := server.store.GetDeploymentTarget(request.Context(), tenantID, principal, projectID, targetID)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	if target.Generation != validated.Body.ExpectedGeneration || target.ObservedPhase != "ready" {
		writePublicProblem(writer, http.StatusConflict, "target_conflict")
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), environmentActuationTimeout)
	defer cancel()
	workers, err := server.listManagedDeploymentTargetWorkers(cleanupContext, target)
	if err != nil {
		writeDeploymentTargetCleanupError(writer, target.Kind, err)
		return
	}
	orphans := make([]managedDeploymentTargetWorker, 0, len(workers))
	for _, worker := range workers {
		principal, err = server.verify(bearer, tenantID, projectID, "projects.get")
		if err != nil {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		lease, leaseErr := server.store.GetManagedHostEnvironmentLease(cleanupContext, tenantID, principal, projectID, worker.leaseID)
		if leaseErr == nil && managedDeploymentTargetWorkerActive(target.Generation, worker, lease) {
			continue
		}
		if leaseErr != nil && !errors.Is(leaseErr, postgres.ErrManagedHostEnvironmentLeaseNotFound) {
			status, code := managedHostEnvironmentLeaseErrorStatus(leaseErr)
			writePublicProblem(writer, status, code)
			return
		}
		orphans = append(orphans, worker)
	}
	if _, err = server.verify(bearer, tenantID, projectID, "projects.act"); err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	for _, worker := range orphans {
		if err := worker.cleanup(cleanupContext); err != nil {
			writeDeploymentTargetCleanupError(writer, target.Kind, err)
			return
		}
	}
	writeDeploymentTarget(writer, http.StatusOK, requestID, target)
}

func (server *DeploymentTargetHTTPServer) cleanupAdmin(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateCleanupAdminDeploymentTargetServerRequest(tenantID, projectID, targetID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	expectedResourceVersion, err := strconv.ParseInt(validated.Body.ExpectedResourceVersion, 10, 64)
	if err != nil || expectedResourceVersion < 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	readPrincipal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	input := internaldeploymenttarget.CleanupInput{
		Scope: internaldeploymenttarget.Scope{TenantID: tenantID, ProjectID: projectID}, TargetID: targetID,
		ExpectedGeneration: validated.Body.ExpectedGeneration, ExpectedResourceVersion: expectedResourceVersion,
		ImpactDigest: validated.Body.ImpactDigest,
		Mutation:     internaldeploymenttarget.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	}
	started, err := server.store.BeginDeploymentTargetCleanup(request.Context(), tenantID, principal, input)
	if err != nil {
		writeDeploymentTargetCleanupAuthorityError(writer, err)
		return
	}
	if !started.Execute {
		writeDeploymentTargetCleanupReplay(writer, requestID, started.Operation)
		return
	}
	complete := func(succeeded bool, stableErrorCode, impactSummary string) (internaldeploymenttarget.Operation, error) {
		completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
		defer cancel()
		completionPrincipal, verifyErr := server.verify(bearer, tenantID, projectID, "projects.act")
		if verifyErr != nil {
			return internaldeploymenttarget.Operation{}, verifyErr
		}
		return server.store.CompleteDeploymentTargetCleanup(completionContext, tenantID, completionPrincipal, internaldeploymenttarget.CleanupCompletion{
			Input: input, Succeeded: succeeded, StableErrorCode: stableErrorCode, ImpactSummary: impactSummary,
		})
	}
	fail := func(status int, publicCode, stableErrorCode, summary string) {
		if _, completeErr := complete(false, stableErrorCode, summary); completeErr != nil {
			writeDeploymentTargetError(writer, completeErr)
			return
		}
		writePublicProblem(writer, status, publicCode)
	}

	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), environmentActuationTimeout)
	defer cancel()
	target, err := server.store.GetDeploymentTarget(cleanupContext, tenantID, readPrincipal, projectID, targetID)
	if err != nil {
		fail(http.StatusConflict, "cleanup_external_state_conflict", "target-cleanup-state-conflict", "Cleanup stopped because target state could not be confirmed")
		return
	}
	if target.Generation != input.ExpectedGeneration || target.ResourceVersion != input.ExpectedResourceVersion || target.ObservedPhase != "ready" {
		fail(http.StatusConflict, "cleanup_external_state_conflict", "target-cleanup-state-conflict", "Cleanup stopped because target state changed after confirmation")
		return
	}
	plan, err := server.inspectDeploymentTargetCleanup(cleanupContext, bearer, target)
	if err != nil {
		status, publicCode, stableErrorCode := deploymentTargetCleanupProblem(target.Kind, err)
		fail(status, publicCode, stableErrorCode, "Cleanup stopped while reading target resources")
		return
	}
	if plan.impactDigest != input.ImpactDigest {
		fail(http.StatusConflict, "cleanup_impact_conflict", "target-cleanup-impact-conflict", "Cleanup stopped because the confirmed resource impact changed")
		return
	}
	if !plan.canCleanup {
		fail(http.StatusConflict, "cleanup_active_lease_conflict", "target-cleanup-active-lease", "Cleanup rejected because an active Environment Lease owns a listed Worker")
		return
	}
	for _, worker := range plan.workers {
		if err := worker.cleanup(cleanupContext); err != nil {
			status, publicCode, stableErrorCode := deploymentTargetCleanupProblem(target.Kind, err)
			fail(status, publicCode, stableErrorCode, "Cleanup failed while removing a confirmed target resource")
			return
		}
	}
	operation, err := complete(true, "", "Cleaned "+strconv.Itoa(len(plan.workers))+" orphan workers and "+strconv.Itoa(plan.resourceCount)+" resources")
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	writeMaintenanceOperation(writer, http.StatusOK, requestID, operation)
}

func (server *DeploymentTargetHTTPServer) listManagedDeploymentTargetWorkers(ctx context.Context, target internaldeploymenttarget.Snapshot) ([]managedDeploymentTargetWorker, error) {
	workers := make([]managedDeploymentTargetWorker, 0)
	switch target.Kind {
	case "docker":
		if server.dockerProber == nil {
			return nil, errDeploymentTargetCleanupUnavailable
		}
		listed, err := server.dockerProber.ListManagedWorkers(ctx, target.Endpoint, target.CredentialRef, target.Scope.TenantID, target.Scope.ProjectID, target.TargetID, target.Generation)
		if err != nil {
			return nil, err
		}
		for _, worker := range listed {
			worker := worker
			request := worker.Request
			container, workspace := worker.CleanupResourceNames()
			workers = append(workers, managedDeploymentTargetWorker{
				tenantID: request.TenantID, projectID: request.ProjectID, targetID: request.TargetID, leaseID: request.LeaseID, workerName: container,
				targetGeneration: request.TargetGeneration, leaseGeneration: request.LeaseGeneration,
				resources: []platformv1alpha1.DeploymentTargetCleanupResource{{ResourceKind: "container", ResourceName: container}, {ResourceKind: "workspace-volume", ResourceName: workspace}},
				cleanup: func(ctx context.Context) error {
					return server.dockerProber.CleanupManagedWorker(ctx, target.Endpoint, target.CredentialRef, worker)
				},
			})
		}
	case "kubernetes":
		if server.kubernetesProber == nil {
			return nil, errDeploymentTargetCleanupUnavailable
		}
		listed, err := server.kubernetesProber.ListManagedWorkers(ctx, target.Endpoint, target.CredentialRef, target.Scope.TenantID, target.Scope.ProjectID, target.TargetID, target.Generation)
		if err != nil {
			return nil, err
		}
		for _, worker := range listed {
			worker := worker
			request := worker.Request
			namespace, name := worker.CleanupResourceNames()
			resourceName := namespace + "/" + name
			workers = append(workers, managedDeploymentTargetWorker{
				tenantID: request.TenantID, projectID: request.ProjectID, targetID: request.TargetID, leaseID: request.LeaseID, workerName: name,
				targetGeneration: request.TargetGeneration, leaseGeneration: request.LeaseGeneration,
				resources: []platformv1alpha1.DeploymentTargetCleanupResource{{ResourceKind: "deployment", ResourceName: resourceName}, {ResourceKind: "pods", ResourceName: namespace + "/cloud-agents.dev/worker=" + name}, {ResourceKind: "service", ResourceName: resourceName}, {ResourceKind: "workspace-volume", ResourceName: resourceName}},
				cleanup: func(ctx context.Context) error {
					return server.kubernetesProber.CleanupManagedWorker(ctx, target.Endpoint, target.CredentialRef, worker)
				},
			})
		}
	case "ssh":
		if server.sshProber == nil {
			return nil, errDeploymentTargetCleanupUnavailable
		}
		listed, err := server.sshProber.ListManagedWorkers(ctx, target.Endpoint, target.CredentialRef, target.Scope.TenantID, target.Scope.ProjectID, target.TargetID, target.Generation)
		if err != nil {
			return nil, err
		}
		for _, worker := range listed {
			worker := worker
			request := worker.Request
			container, workspace := worker.CleanupResourceNames()
			workers = append(workers, managedDeploymentTargetWorker{
				tenantID: request.TenantID, projectID: request.ProjectID, targetID: request.TargetID, leaseID: request.LeaseID, workerName: container,
				targetGeneration: request.TargetGeneration, leaseGeneration: request.LeaseGeneration,
				resources: []platformv1alpha1.DeploymentTargetCleanupResource{{ResourceKind: "container", ResourceName: container}, {ResourceKind: "workspace-volume", ResourceName: workspace}},
				cleanup: func(ctx context.Context) error {
					return server.sshProber.CleanupManagedWorker(ctx, target.Endpoint, target.CredentialRef, worker)
				},
			})
		}
	default:
		return nil, errDeploymentTargetCleanupConflict
	}
	slices.SortFunc(workers, func(left, right managedDeploymentTargetWorker) int {
		return strings.Compare(left.workerName, right.workerName)
	})
	return workers, nil
}

func (server *DeploymentTargetHTTPServer) inspectDeploymentTargetCleanup(ctx context.Context, bearer string, target internaldeploymenttarget.Snapshot) (deploymentTargetCleanupPlan, error) {
	workers, err := server.listManagedDeploymentTargetWorkers(ctx, target)
	if err != nil {
		return deploymentTargetCleanupPlan{}, err
	}
	plan := deploymentTargetCleanupPlan{workers: workers, previewWorkers: make([]platformv1alpha1.DeploymentTargetCleanupWorker, 0, len(workers)), canCleanup: true}
	for _, worker := range workers {
		principal, verifyErr := server.verify(bearer, target.Scope.TenantID, target.Scope.ProjectID, "projects.get")
		if verifyErr != nil {
			return deploymentTargetCleanupPlan{}, verifyErr
		}
		lease, leaseErr := server.store.GetManagedHostEnvironmentLease(ctx, target.Scope.TenantID, principal, target.Scope.ProjectID, worker.leaseID)
		disposition := "cleanup"
		if leaseErr == nil && managedDeploymentTargetWorkerActive(target.Generation, worker, lease) {
			disposition = "blocked"
			plan.canCleanup = false
		}
		if leaseErr != nil && !errors.Is(leaseErr, postgres.ErrManagedHostEnvironmentLeaseNotFound) {
			return deploymentTargetCleanupPlan{}, leaseErr
		}
		plan.resourceCount += len(worker.resources)
		plan.previewWorkers = append(plan.previewWorkers, platformv1alpha1.DeploymentTargetCleanupWorker{
			WorkerName: worker.workerName, LeaseID: worker.leaseID, LeaseGeneration: worker.leaseGeneration,
			Disposition: disposition, Resources: worker.resources,
		})
	}
	encoded, _ := json.Marshal(struct {
		TargetID        string                                           `json:"targetId"`
		Generation      int64                                            `json:"generation"`
		ResourceVersion int64                                            `json:"resourceVersion"`
		Workers         []platformv1alpha1.DeploymentTargetCleanupWorker `json:"workers"`
	}{target.TargetID, target.Generation, target.ResourceVersion, plan.previewWorkers})
	sum := sha256.Sum256(encoded)
	plan.impactDigest = "sha256:" + hex.EncodeToString(sum[:])
	return plan, nil
}

func (server *DeploymentTargetHTTPServer) cleanupPreview(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
	validated, err := openapiv1alpha1.ValidatePreviewAdminDeploymentTargetCleanupServerRequest(tenantID, projectID, targetID, requestID)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, validated.TenantID, validated.ProjectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	target, err := server.store.GetDeploymentTarget(request.Context(), validated.TenantID, principal, validated.ProjectID, validated.TargetID)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	previewContext, cancel := context.WithTimeout(request.Context(), environmentActuationTimeout)
	defer cancel()
	plan, err := server.inspectDeploymentTargetCleanup(previewContext, bearer, target)
	if err != nil {
		writeDeploymentTargetCleanupError(writer, target.Kind, err)
		return
	}
	resourceVersion := strconv.FormatInt(target.ResourceVersion, 10)
	preview := platformv1alpha1.DeploymentTargetCleanupPreview{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "DeploymentTargetCleanupPreview", Metadata: commonv1alpha1.ResourceMetadata{UID: target.TargetID, Name: target.TargetName, TenantRef: commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: target.Scope.TenantID}, ResourceVersion: resourceVersion, CreatedAt: target.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: target.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
		Spec:         platformv1alpha1.DeploymentTargetCleanupPreviewSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: target.Scope.ProjectID}, TargetKind: target.Kind, ExpectedGeneration: target.Generation, ExpectedResourceVersion: resourceVersion, ImpactDigest: plan.impactDigest, CanCleanup: plan.canCleanup, Workers: plan.previewWorkers},
	}
	body, err := platformv1alpha1.EncodeDeploymentTargetCleanupPreviewResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.DeploymentTargetCleanupPreview]{Value: preview})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", resourceVersion)
	writer.Header().Set("ETag", `"`+resourceVersion+`"`)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func managedDeploymentTargetWorkerActive(targetGeneration int64, worker managedDeploymentTargetWorker, lease internalmanagedhost.Snapshot) bool {
	return worker.targetGeneration == targetGeneration && lease.Scope.TenantID == worker.tenantID && lease.Scope.ProjectID == worker.projectID &&
		lease.LeaseID == worker.leaseID && lease.TargetID == worker.targetID && lease.TargetGeneration == worker.targetGeneration &&
		lease.Generation == worker.leaseGeneration && lease.DesiredPhase == "active"
}

func writeDeploymentTargetCleanupError(writer http.ResponseWriter, kind string, err error) {
	status, code, _ := deploymentTargetCleanupProblem(kind, err)
	writePublicProblem(writer, status, code)
}

func deploymentTargetCleanupProblem(kind string, err error) (int, string, string) {
	if errors.Is(err, errDeploymentTargetCleanupUnavailable) {
		return http.StatusServiceUnavailable, "environment_cleanup_unavailable", "target-cleanup-unavailable"
	}
	if errors.Is(err, errDeploymentTargetCleanupConflict) {
		return http.StatusConflict, "target_conflict", "target-cleanup-conflict"
	}
	if (kind == "docker" && errors.Is(err, dockertarget.ErrDeploymentConflict)) ||
		(kind == "kubernetes" && errors.Is(err, kubernetestarget.ErrDeploymentConflict)) ||
		(kind == "ssh" && errors.Is(err, sshtarget.ErrDeploymentConflict)) {
		return http.StatusConflict, "environment_cleanup_conflict", "target-cleanup-actuator-conflict"
	}
	return http.StatusBadGateway, "environment_cleanup_failed", "target-cleanup-failed"
}

func writeDeploymentTargetCleanupAuthorityError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrDeploymentTargetCleanupIdempotencyConflict):
		writePublicProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, postgres.ErrDeploymentTargetCleanupBusy):
		writePublicProblem(writer, http.StatusConflict, "operation_in_progress")
	case errors.Is(err, postgres.ErrDeploymentTargetCleanupGenerationConflict):
		writePublicProblem(writer, http.StatusConflict, "target_generation_conflict")
	case errors.Is(err, postgres.ErrDeploymentTargetCleanupResourceVersionConflict):
		writePublicProblem(writer, http.StatusConflict, "target_resource_version_conflict")
	case errors.Is(err, postgres.ErrDeploymentTargetCleanupNotReady):
		writePublicProblem(writer, http.StatusConflict, "target_not_ready")
	default:
		writeDeploymentTargetError(writer, err)
	}
}

func writeDeploymentTargetCleanupReplay(writer http.ResponseWriter, requestID string, operation internaldeploymenttarget.Operation) {
	if operation.State != "failed" {
		writeMaintenanceOperation(writer, http.StatusOK, requestID, operation)
		return
	}
	switch operation.StableErrorCode {
	case "target-cleanup-impact-conflict":
		writePublicProblem(writer, http.StatusConflict, "cleanup_impact_conflict")
	case "target-cleanup-active-lease":
		writePublicProblem(writer, http.StatusConflict, "cleanup_active_lease_conflict")
	case "target-cleanup-state-conflict":
		writePublicProblem(writer, http.StatusConflict, "cleanup_external_state_conflict")
	case "target-cleanup-unavailable":
		writePublicProblem(writer, http.StatusServiceUnavailable, "environment_cleanup_unavailable")
	case "target-cleanup-conflict", "target-cleanup-actuator-conflict":
		writePublicProblem(writer, http.StatusConflict, "environment_cleanup_conflict")
	default:
		writePublicProblem(writer, http.StatusBadGateway, "environment_cleanup_failed")
	}
}

func (server *DeploymentTargetHTTPServer) register(writer http.ResponseWriter, request *http.Request, tenantID, projectID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateRegisterDeploymentTargetServerRequest(tenantID, projectID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.RegisterDeploymentTarget(request.Context(), tenantID, principal, internaldeploymenttarget.RegisterInput{
		Scope: internaldeploymenttarget.Scope{TenantID: tenantID, ProjectID: projectID}, TargetID: validated.Body.TargetID,
		TargetName: validated.Body.TargetName, Kind: validated.Body.TargetKind, Endpoint: validated.Body.Endpoint,
		CredentialRef: validated.Body.CredentialRef, Mutation: internaldeploymenttarget.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey},
	})
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	writeDeploymentTarget(writer, http.StatusCreated, requestID, result)
}

func (server *DeploymentTargetHTTPServer) get(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
	if _, err := openapiv1alpha1.ValidateGetDeploymentTargetServerRequest(tenantID, projectID, targetID, requestID); err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.get")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.GetDeploymentTarget(request.Context(), tenantID, principal, projectID, targetID)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	writeDeploymentTarget(writer, http.StatusOK, requestID, result)
}

func (server *DeploymentTargetHTTPServer) probe(writer http.ResponseWriter, request *http.Request, tenantID, projectID, targetID, requestID, bearer string) {
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
	validated, err := openapiv1alpha1.ValidateProbeDeploymentTargetServerRequest(tenantID, projectID, targetID, requestID, idempotencyKey, body)
	if err != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	input := internaldeploymenttarget.ProbeInput{Scope: internaldeploymenttarget.Scope{TenantID: tenantID, ProjectID: projectID}, TargetID: targetID, ExpectedGeneration: validated.Body.ExpectedGeneration, Mutation: internaldeploymenttarget.Mutation{RequestID: requestID, IdempotencyKey: idempotencyKey}}
	principal, err := server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	started, err := server.store.BeginDeploymentTargetProbe(request.Context(), tenantID, principal, input)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	if !started.Execute {
		writeDeploymentTarget(writer, http.StatusOK, requestID, started.Target)
		return
	}
	completion := internaldeploymenttarget.ProbeCompletion{Input: input}
	switch started.Target.Kind {
	case "docker":
		if server.dockerProber == nil {
			completion.StableErrorCode = "docker-probe-unconfigured"
		} else if result, probeErr := server.dockerProber.Probe(request.Context(), started.Target.Endpoint, started.Target.CredentialRef); probeErr != nil {
			completion.StableErrorCode = dockerTargetProbeErrorCode(probeErr)
		} else {
			completion.Succeeded, completion.APIVersion, completion.EngineVersion = true, result.APIVersion, result.EngineVersion
			completion.OS, completion.Arch = result.OS, result.Architecture
		}
	case "kubernetes":
		if server.kubernetesProber == nil {
			completion.StableErrorCode = "kubernetes-probe-unconfigured"
		} else if result, probeErr := server.kubernetesProber.Probe(request.Context(), started.Target.Endpoint, started.Target.CredentialRef); probeErr != nil {
			completion.StableErrorCode = kubernetesTargetProbeErrorCode(probeErr)
		} else {
			completion.Succeeded, completion.APIVersion, completion.EngineVersion = true, result.APIVersion, result.EngineVersion
			completion.OS, completion.Arch = result.OS, result.Architecture
		}
	case "ssh":
		if server.sshProber == nil {
			completion.StableErrorCode = "ssh-probe-unconfigured"
		} else if result, probeErr := server.sshProber.Probe(request.Context(), started.Target.Endpoint, started.Target.CredentialRef); probeErr != nil {
			completion.StableErrorCode = sshTargetProbeErrorCode(probeErr)
		} else {
			completion.Succeeded, completion.APIVersion, completion.EngineVersion = true, result.APIVersion, result.EngineVersion
			completion.OS, completion.Arch = result.OS, result.Architecture
		}
	}
	completionContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), deploymentTargetCompletionTimeout)
	defer cancel()
	principal, err = server.verify(bearer, tenantID, projectID, "projects.act")
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	result, err := server.store.CompleteDeploymentTargetProbe(completionContext, tenantID, principal, completion)
	if err != nil {
		writeDeploymentTargetError(writer, err)
		return
	}
	writeDeploymentTarget(writer, http.StatusOK, requestID, result)
}

func (server *DeploymentTargetHTTPServer) verify(bearer, tenantID, projectID, permission string) (*authn.VerifiedPrincipal, error) {
	return server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: permission})
}

func writeDeploymentTarget(writer http.ResponseWriter, status int, requestID string, snapshot internaldeploymenttarget.Snapshot) {
	value := deploymentTargetResource(snapshot)
	body, err := openapiv1alpha1.EncodeDeploymentTargetResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.DeploymentTarget]{Value: value})
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

func writeMaintenanceOperation(writer http.ResponseWriter, status int, requestID string, operation internaldeploymenttarget.Operation) {
	body, err := platformv1alpha1.EncodeMaintenanceOperationResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MaintenanceOperation]{Value: maintenanceOperationResource(operation)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func writeDeploymentTargetPage(writer http.ResponseWriter, requestID, tenantID, projectID string, page postgres.DeploymentTargetPage) {
	targets := make([]platformv1alpha1.DeploymentTarget, 0, len(page.DeploymentTargets))
	for _, snapshot := range page.DeploymentTargets {
		targets = append(targets, deploymentTargetResource(snapshot))
	}
	nextPageToken := ""
	if page.NextTargetID != "" {
		var ok bool
		nextPageToken, ok = encodeDeploymentTargetPageToken(tenantID, projectID, page.NextTargetID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeDeploymentTargetPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.DeploymentTargetPage]{Value: platformv1alpha1.DeploymentTargetPage{APIVersion: platformv1alpha1.APIVersion, Kind: "DeploymentTargetPage", DeploymentTargets: targets, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func deploymentTargetResource(snapshot internaldeploymenttarget.Snapshot) platformv1alpha1.DeploymentTarget {
	lastProbeAt := ""
	if snapshot.LastProbeAt != nil {
		lastProbeAt = snapshot.LastProbeAt.UTC().Format(time.RFC3339Nano)
	}
	return platformv1alpha1.DeploymentTarget{
		ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "DeploymentTarget", Metadata: commonv1alpha1.ResourceMetadata{
			UID: snapshot.TargetID, Name: snapshot.TargetName, TenantRef: commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
			ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}},
		Spec: platformv1alpha1.DeploymentTargetSpec{
			ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
			Generation: snapshot.Generation, TargetKind: snapshot.Kind, Endpoint: snapshot.Endpoint, CredentialRef: snapshot.CredentialRef,
			ObservedPhase: snapshot.ObservedPhase, APIVersion: snapshot.APIVersion, EngineVersion: snapshot.EngineVersion,
			OS: snapshot.OS, Architecture: snapshot.Arch, StableErrorCode: snapshot.StableErrorCode, LastProbeAt: lastProbeAt,
		},
	}
}

func maintenanceOperationResource(operation internaldeploymenttarget.Operation) platformv1alpha1.MaintenanceOperation {
	return platformv1alpha1.MaintenanceOperation{
		APIVersion: platformv1alpha1.APIVersion, Kind: "MaintenanceOperation", OperationID: operation.OperationID,
		IdempotencyKey: operation.IdempotencyKey, Action: operation.Action, ResourceKind: "DeploymentTarget",
		ResourceID: operation.TargetID, ResourceGeneration: operation.TargetGeneration, RequestedBy: operation.RequestedBy,
		RequestID: operation.RequestID, RequestedAt: operation.RequestedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339Nano), State: operation.State, CurrentStep: operation.CurrentStep,
		StableErrorCode: operation.StableErrorCode, ImpactSummary: operation.ImpactSummary, Retryable: operation.Retryable,
	}
}

func adminAuditEventResource(event internaldeploymenttarget.AuditEvent) platformv1alpha1.AdminAuditEvent {
	return platformv1alpha1.AdminAuditEvent{
		APIVersion: platformv1alpha1.APIVersion, Kind: "AdminAuditEvent", EventID: event.EventID, Actor: event.Actor,
		Action: event.Action, ResourceKind: "DeploymentTarget", ResourceID: event.TargetID,
		ResourceGeneration: event.TargetGeneration, Result: event.Result, OccurredAt: event.OccurredAt.UTC().Format(time.RFC3339Nano),
		RequestID: event.RequestID, OperationID: event.OperationID, StableErrorCode: event.StableErrorCode,
	}
}

func deploymentTargetPath(path string) (tenantID, projectID, targetID, action string, ok bool) {
	return deploymentTargetPathWithPrefix(path, ProjectRoutePrefix, false)
}

func adminDeploymentTargetPath(path string) (tenantID, projectID, targetID, action string, ok bool) {
	return deploymentTargetPathWithPrefix(path, adminDeploymentTargetRoutePrefix, true)
}

func deploymentTargetPathWithPrefix(path, prefix string, admin bool) (tenantID, projectID, targetID, action string, ok bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if admin && len(parts) == 4 && parts[1] == "projects" && parts[3] == "maintenance-operations" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "maintenance-operations", true
	}
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		if admin && strings.HasSuffix(parts[4], ":cleanup-preview") {
			targetID = strings.TrimSuffix(parts[4], ":cleanup-preview")
			if targetID != "" {
				return parts[0], parts[2], targetID, "cleanup-preview", true
			}
		} else if strings.HasSuffix(parts[4], ":probe") {
			targetID = strings.TrimSuffix(parts[4], ":probe")
			if targetID != "" {
				return parts[0], parts[2], targetID, "probe", true
			}
		} else if strings.HasSuffix(parts[4], ":cleanup") {
			targetID = strings.TrimSuffix(parts[4], ":cleanup")
			if targetID != "" {
				return parts[0], parts[2], targetID, "cleanup", true
			}
		} else if parts[4] != "" && !strings.Contains(parts[4], ":") {
			return parts[0], parts[2], parts[4], "get", true
		}
	}
	if admin && len(parts) == 6 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" && parts[4] != "" {
		if parts[5] == "operations" || parts[5] == "audit-events" {
			return parts[0], parts[2], parts[4], parts[5], true
		}
	}
	return "", "", "", "", false
}

func deploymentTargetAdminPermission(action, method string) (string, bool) {
	switch {
	case action == "collection" && method == http.MethodGet:
		return "targets.list", true
	case action == "collection" && method == http.MethodPost:
		return "targets.create", true
	case action == "get" && method == http.MethodGet:
		return "targets.get", true
	case action == "cleanup-preview" && method == http.MethodGet:
		return "targets.get", true
	case action == "operations" && method == http.MethodGet:
		return "operations.list", true
	case action == "maintenance-operations" && method == http.MethodGet:
		return "operations.list", true
	case action == "audit-events" && method == http.MethodGet:
		return "audit.list", true
	case action == "probe" && method == http.MethodPost:
		return "targets.act", true
	case action == "cleanup" && method == http.MethodPost:
		return "targets.act", true
	default:
		return "", false
	}
}

func HandlesDeploymentTargetPath(path string) bool {
	_, _, _, _, ok := deploymentTargetPath(path)
	return ok
}

func HandlesAdminDeploymentTargetPath(path string) bool {
	_, _, _, _, ok := adminDeploymentTargetPath(path)
	return ok
}

func encodeDeploymentTargetPageToken(tenantID, projectID, targetID string) (string, bool) {
	if commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("deployment-target/v1\x00" + tenantID + "\x00" + projectID + "\x00" + targetID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeDeploymentTargetPageToken(tenantID, projectID, token string) (string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 4 || parts[0] != "deployment-target/v1" || parts[1] != tenantID || parts[2] != projectID ||
		commonv1alpha1.ValidateIdentifier(parts[1], "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(parts[2], "/projectId") != nil || commonv1alpha1.ValidateIdentifier(parts[3], "/targetId") != nil {
		return "", false
	}
	return parts[3], true
}

func encodeDeploymentTargetActivityPageToken(kind, tenantID, projectID, targetID string, timestamp time.Time, id string) (string, bool) {
	if kind != "operation" && kind != "audit" || timestamp.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || commonv1alpha1.ValidateIdentifier(id, "/cursorId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("deployment-target-activity/v1\x00" + kind + "\x00" + tenantID + "\x00" + projectID + "\x00" + targetID + "\x00" + timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + id))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeDeploymentTargetActivityPageToken(kind, tenantID, projectID, targetID, token string) (*time.Time, string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return nil, "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 7 || parts[0] != "deployment-target-activity/v1" || parts[1] != kind || parts[2] != tenantID || parts[3] != projectID || parts[4] != targetID || commonv1alpha1.ValidateIdentifier(parts[6], "/cursorId") != nil {
		return nil, "", false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[5])
	if err != nil {
		return nil, "", false
	}
	return &timestamp, parts[6], true
}

func encodeMaintenanceOperationPageToken(tenantID, projectID, targetID string, timestamp time.Time, operationID string) (string, bool) {
	if timestamp.IsZero() || commonv1alpha1.ValidateIdentifier(tenantID, "/tenantId") != nil || commonv1alpha1.ValidateIdentifier(projectID, "/projectId") != nil || commonv1alpha1.ValidateIdentifier(targetID, "/targetId") != nil || commonv1alpha1.ValidateIdentifier(operationID, "/operationId") != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("maintenance-operation/v1\x00" + tenantID + "\x00" + projectID + "\x00" + targetID + "\x00" + timestamp.UTC().Format(time.RFC3339Nano) + "\x00" + operationID))
	return token, commonv1alpha1.ValidatePageToken(token, "/pageToken") == nil
}

func decodeMaintenanceOperationPageToken(tenantID, projectID, token string) (*time.Time, string, string, bool) {
	if commonv1alpha1.ValidatePageToken(token, "/pageToken") != nil {
		return nil, "", "", false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil {
		return nil, "", "", false
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 6 || parts[0] != "maintenance-operation/v1" || parts[1] != tenantID || parts[2] != projectID || commonv1alpha1.ValidateIdentifier(parts[3], "/targetId") != nil || commonv1alpha1.ValidateIdentifier(parts[5], "/operationId") != nil {
		return nil, "", "", false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, parts[4])
	if err != nil {
		return nil, "", "", false
	}
	return &timestamp, parts[3], parts[5], true
}

func dockerTargetProbeErrorCode(err error) string {
	switch {
	case errors.Is(err, dockertarget.ErrInvalidEndpoint):
		return "docker-endpoint-invalid"
	case errors.Is(err, dockertarget.ErrInvalidDirectory):
		return "docker-probe-unconfigured"
	case errors.Is(err, dockertarget.ErrCredentialUnavailable), errors.Is(err, dockertarget.ErrCredentialInvalid):
		return "docker-credential-unavailable"
	case errors.Is(err, dockertarget.ErrInvalidResponse):
		return "docker-response-invalid"
	default:
		return "docker-target-unavailable"
	}
}

func kubernetesTargetProbeErrorCode(err error) string {
	switch {
	case errors.Is(err, kubernetestarget.ErrInvalidEndpoint):
		return "kubernetes-endpoint-invalid"
	case errors.Is(err, kubernetestarget.ErrInvalidDirectory):
		return "kubernetes-probe-unconfigured"
	case errors.Is(err, kubernetestarget.ErrCredentialUnavailable), errors.Is(err, kubernetestarget.ErrCredentialInvalid):
		return "kubernetes-credential-unavailable"
	case errors.Is(err, kubernetestarget.ErrInvalidResponse):
		return "kubernetes-response-invalid"
	default:
		return "kubernetes-target-unavailable"
	}
}

func sshTargetProbeErrorCode(err error) string {
	switch {
	case errors.Is(err, sshtarget.ErrInvalidEndpoint):
		return "ssh-endpoint-invalid"
	case errors.Is(err, sshtarget.ErrInvalidDirectory):
		return "ssh-probe-unconfigured"
	case errors.Is(err, sshtarget.ErrHostKeyMismatch):
		return "ssh-host-key-mismatch"
	case errors.Is(err, sshtarget.ErrCredentialUnavailable), errors.Is(err, sshtarget.ErrCredentialInvalid):
		return "ssh-credential-unavailable"
	case errors.Is(err, sshtarget.ErrInvalidResponse):
		return "ssh-response-invalid"
	default:
		return "ssh-target-unavailable"
	}
}

func writeDeploymentTargetError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrDeploymentTargetNotFound):
		writePublicProblem(writer, http.StatusNotFound, "not_found")
	case errors.Is(err, postgres.ErrMutationDenied):
		writePublicProblem(writer, http.StatusForbidden, "authorization_denied")
	case errors.Is(err, postgres.ErrCoordinationRejected):
		writePublicProblem(writer, http.StatusConflict, "target_conflict")
	case errors.Is(err, postgres.ErrCoordinationInvalidInput):
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
	default:
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}
