package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	worker "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapi "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/workerclient"
)

func (server *ManagedHostEnvironmentLeaseHTTPServer) getWorkerHealth(writer http.ResponseWriter, request *http.Request, tenantID, projectID, leaseID, requestID, bearer string) {
	writer.Header().Set("Cache-Control", "no-store")
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["expectedGeneration"]) != 1 {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	generation, err := strconv.ParseInt(query.Get("expectedGeneration"), 10, 64)
	if err != nil || strconv.FormatInt(generation, 10) != query.Get("expectedGeneration") || openapi.ValidateGetAdminWorkerHealthServerRequest(tenantID, projectID, leaseID, requestID, generation) != nil {
		writePublicProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	principal, err := server.verifier.Verify(bearer, authn.VerificationRequest{TenantID: tenantID, ResourceLevel: "project", ResourceID: projectID, RequiredPermission: "projects.get"})
	if err != nil {
		writePublicProblem(writer, http.StatusUnauthorized, "authentication_failed")
		return
	}
	before, err := server.store.GetManagedHostEnvironmentLease(request.Context(), tenantID, principal, projectID, leaseID)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	if before.Generation != generation || before.DesiredPhase != "active" || before.ObservedPhase != "ready" || before.CleanupPhase != "none" || before.TargetID == "" || before.ProviderCredentialRef == "" || !time.Now().Before(before.ExpiresAt) {
		writePublicProblem(writer, http.StatusConflict, "worker_not_ready")
		return
	}
	identity, err := url.Parse(before.WorkerSPIFFEID)
	if err != nil || before.WorkerServerName == "" || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path == "" || identity.User != nil || identity.RawQuery != "" || identity.Fragment != "" {
		writePublicProblem(writer, http.StatusServiceUnavailable, "worker_health_config_unavailable")
		return
	}
	supervisor, err := workerclient.NewMTLS(workerclient.MTLSConfig{
		Endpoint: before.WorkerEndpoint, ServerName: before.WorkerServerName,
		ExpectedWorkerIdentity: &worker.WorkloadIdentity{SpiffeId: before.WorkerSPIFFEID, TrustDomain: identity.Host},
		ClientCertificate:      server.workerTrust.ClientCertificate, RootCAs: server.workerTrust.RootCAs,
	})
	if err != nil {
		writePublicProblem(writer, http.StatusServiceUnavailable, "worker_health_config_unavailable")
		return
	}
	defer supervisor.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
	defer cancel()
	state := "serving"
	if supervisor.CheckRuntimeHealth(ctx) != nil {
		state = "unavailable" // Never serialize transport errors, endpoints, or remote payloads.
	}
	checkedAt := time.Now().UTC()
	after, err := server.store.GetManagedHostEnvironmentLease(request.Context(), tenantID, principal, projectID, leaseID)
	if err != nil {
		status, code := managedHostEnvironmentLeaseErrorStatus(err)
		writePublicProblem(writer, status, code)
		return
	}
	// A health response is not a fencing acknowledgement. Discard it if the authoritative route changed.
	if before.Generation != after.Generation || before.ResourceVersion != after.ResourceVersion || before.WorkerEndpoint != after.WorkerEndpoint || before.WorkerSPIFFEID != after.WorkerSPIFFEID || before.WorkerServerName != after.WorkerServerName || after.ObservedPhase != "ready" || after.DesiredPhase != "active" || after.CleanupPhase != "none" || !checkedAt.Before(after.ExpiresAt) {
		writePublicProblem(writer, http.StatusConflict, "worker_changed_during_health_check")
		return
	}
	body, err := platform.EncodeWorkerHealthObservationResponseJSON(common.ResponseEnvelope[platform.WorkerHealthObservation]{Value: platform.WorkerHealthObservation{
		APIVersion: platform.APIVersion, Kind: "WorkerHealthObservation", TenantID: tenantID, ProjectID: projectID, WorkerID: leaseID,
		Generation: generation, ResourceVersion: strconv.FormatInt(after.ResourceVersion, 10), State: state, CheckedAt: checkedAt.Format(time.RFC3339Nano),
	}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}
