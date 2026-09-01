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
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/kubernetestarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/sshtarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

const deploymentTargetCompletionTimeout = 5 * time.Second

type deploymentTargetStore interface {
	RegisterDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.RegisterInput) (internaldeploymenttarget.Snapshot, error)
	GetDeploymentTarget(context.Context, string, *authn.VerifiedPrincipal, string, string) (internaldeploymenttarget.Snapshot, error)
	GetManagedHostEnvironmentLease(context.Context, string, *authn.VerifiedPrincipal, string, string) (internalmanagedhost.Snapshot, error)
	BeginDeploymentTargetProbe(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.ProbeInput) (internaldeploymenttarget.ProbeStart, error)
	CompleteDeploymentTargetProbe(context.Context, string, *authn.VerifiedPrincipal, internaldeploymenttarget.ProbeCompletion) (internaldeploymenttarget.Snapshot, error)
}

type DeploymentTargetHTTPServer struct {
	verifier         AccessTokenVerifier
	store            deploymentTargetStore
	dockerProber     *dockertarget.CredentialDirectory
	kubernetesProber *kubernetestarget.CredentialDirectory
	sshProber        *sshtarget.CredentialDirectory
}

func NewDeploymentTargetHTTPServer(verifier AccessTokenVerifier, store deploymentTargetStore, dockerProber *dockertarget.CredentialDirectory, kubernetesProber *kubernetestarget.CredentialDirectory, sshProber *sshtarget.CredentialDirectory) (*DeploymentTargetHTTPServer, error) {
	if verifier == nil || store == nil {
		return nil, errors.New("deployment target HTTP server configuration is invalid")
	}
	return &DeploymentTargetHTTPServer{verifier: verifier, store: store, dockerProber: dockerProber, kubernetesProber: kubernetesProber, sshProber: sshProber}, nil
}

func (server *DeploymentTargetHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	preparePublicRequestID(writer, request)
	if server == nil || server.verifier == nil || server.store == nil || request == nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	tenantID, projectID, targetID, action, ok := deploymentTargetPath(request.URL.Path)
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
	case action == "collection" && request.Method == http.MethodPost:
		server.register(writer, request, tenantID, projectID, requestID, bearer)
	case action == "get" && request.Method == http.MethodGet:
		server.get(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "probe" && request.Method == http.MethodPost:
		server.probe(writer, request, tenantID, projectID, targetID, requestID, bearer)
	case action == "cleanup" && request.Method == http.MethodPost:
		server.cleanup(writer, request, tenantID, projectID, targetID, requestID, bearer)
	default:
		writePublicProblem(writer, http.StatusMethodNotAllowed, "method_not_allowed")
	}
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
	if target.Generation != validated.Body.ExpectedGeneration || target.Kind != "kubernetes" || target.ObservedPhase != "ready" {
		writePublicProblem(writer, http.StatusConflict, "target_conflict")
		return
	}
	if server.kubernetesProber == nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "environment_cleanup_unavailable")
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), environmentActuationTimeout)
	defer cancel()
	workers, err := server.kubernetesProber.ListManagedWorkers(cleanupContext, target.Endpoint, target.CredentialRef, tenantID, projectID, targetID, target.Generation)
	if err != nil {
		writeKubernetesTargetCleanupError(writer, err)
		return
	}
	orphans := make([]kubernetestarget.ManagedWorker, 0, len(workers))
	for _, worker := range workers {
		principal, err = server.verify(bearer, tenantID, projectID, "projects.get")
		if err != nil {
			writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
			return
		}
		lease, leaseErr := server.store.GetManagedHostEnvironmentLease(cleanupContext, tenantID, principal, projectID, worker.Request.LeaseID)
		if leaseErr == nil && managedKubernetesWorkerActive(target.Generation, worker, lease) {
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
		if err := server.kubernetesProber.CleanupManagedWorker(cleanupContext, target.Endpoint, target.CredentialRef, worker); err != nil {
			writeKubernetesTargetCleanupError(writer, err)
			return
		}
	}
	writeDeploymentTarget(writer, http.StatusOK, requestID, target)
}

func managedKubernetesWorkerActive(targetGeneration int64, worker kubernetestarget.ManagedWorker, lease internalmanagedhost.Snapshot) bool {
	request := worker.Request
	return request.TargetGeneration == targetGeneration && lease.Scope.TenantID == request.TenantID && lease.Scope.ProjectID == request.ProjectID &&
		lease.LeaseID == request.LeaseID && lease.TargetID == request.TargetID && lease.TargetGeneration == request.TargetGeneration &&
		lease.Generation == request.LeaseGeneration && lease.DesiredPhase == "active"
}

func writeKubernetesTargetCleanupError(writer http.ResponseWriter, err error) {
	if errors.Is(err, kubernetestarget.ErrDeploymentConflict) {
		writePublicProblem(writer, http.StatusConflict, "environment_cleanup_conflict")
		return
	}
	writePublicProblem(writer, http.StatusBadGateway, "environment_cleanup_failed")
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

func deploymentTargetPath(path string) (tenantID, projectID, targetID, action string, ok bool) {
	if !strings.HasPrefix(path, ProjectRoutePrefix) {
		return "", "", "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, ProjectRoutePrefix), "/")
	if len(parts) == 4 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		return parts[0], parts[2], "", "collection", true
	}
	if len(parts) == 5 && parts[1] == "projects" && parts[3] == "deployment-targets" && parts[0] != "" && parts[2] != "" {
		if strings.HasSuffix(parts[4], ":probe") {
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
	return "", "", "", "", false
}

func HandlesDeploymentTargetPath(path string) bool {
	_, _, _, _, ok := deploymentTargetPath(path)
	return ok
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
