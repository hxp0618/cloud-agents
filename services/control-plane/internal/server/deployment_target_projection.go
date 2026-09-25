package server

import (
	"net/http"
	"strconv"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

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
			SchedulingState: snapshot.SchedulingState, ObservedPhase: snapshot.ObservedPhase, APIVersion: snapshot.APIVersion, EngineVersion: snapshot.EngineVersion,
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
