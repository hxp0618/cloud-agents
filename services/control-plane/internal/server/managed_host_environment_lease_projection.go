package server

import (
	"net/http"
	"strconv"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	openapiv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

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

func writeAdminEnvironmentLeaseUpgradePreview(writer http.ResponseWriter, requestID string, preview internalmanagedhost.AdminEnvironmentLeaseUpgradePreview) {
	snapshot := preview.Lease
	value := platformv1alpha1.EnvironmentLeaseUpgradePreview{ResourceBase: platformv1alpha1.ResourceBase{
		APIVersion: platformv1alpha1.APIVersion, Kind: "EnvironmentLeaseUpgradePreview",
		Metadata: commonv1alpha1.ResourceMetadata{UID: snapshot.LeaseID, Name: snapshot.LeaseName,
			TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
			ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)},
	}, Spec: platformv1alpha1.EnvironmentLeaseUpgradePreviewSpec{
		ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		Action:     preview.Action, TargetID: snapshot.TargetID, TargetKind: preview.TargetKind,
		CurrentReleaseDigest: snapshot.ReleaseDigest, TargetReleaseDigest: preview.TargetReleaseDigest,
		RollbackReleaseDigest: preview.RollbackReleaseDigest, RollbackGeneration: preview.RollbackGeneration,
		ExpectedGeneration: snapshot.Generation, ExpectedResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10),
		AffectedTargets: 1, AffectedWorkers: 1, AffectedLeases: 1, ImpactDigest: preview.ImpactDigest,
	}}
	body, err := platformv1alpha1.EncodeEnvironmentLeaseUpgradePreviewResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.EnvironmentLeaseUpgradePreview]{Value: value})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}

func managedHostEnvironmentLeaseResource(snapshot internalmanagedhost.Snapshot) platformv1alpha1.EnvironmentLease {
	tenant := commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID}
	return platformv1alpha1.EnvironmentLease{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "CloudEnvironmentLease", Metadata: commonv1alpha1.ResourceMetadata{UID: snapshot.LeaseID, Name: snapshot.LeaseName, TenantRef: tenant, ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)}}, Spec: platformv1alpha1.EnvironmentLeaseSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID}, Generation: snapshot.Generation, DesiredPhase: snapshot.DesiredPhase, ObservedPhase: snapshot.ObservedPhase, CleanupPhase: snapshot.CleanupPhase, EnvironmentID: snapshot.EnvironmentID, ReleaseDigest: snapshot.ReleaseDigest, TargetID: snapshot.TargetID, TargetGeneration: snapshot.TargetGeneration, ProviderCredentialRef: snapshot.ProviderCredentialRef, CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes, WorkerEndpoint: snapshot.WorkerEndpoint, WorkerSPIFFEID: snapshot.WorkerSPIFFEID, WorkerServerName: snapshot.WorkerServerName, StableErrorCode: snapshot.StableErrorCode, ExpiresAt: snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano)}}
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

func writeAdminWorkerPage(writer http.ResponseWriter, requestID, tenantID, projectID string, page postgres.AdminWorkerPage) {
	workers := make([]platformv1alpha1.Worker, 0, len(page.Workers))
	for _, snapshot := range page.Workers {
		spec := platformv1alpha1.WorkerSpec{
			ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
			LeaseID:    snapshot.LeaseID, TargetID: snapshot.TargetID, TargetKind: snapshot.TargetKind,
			TargetGeneration: snapshot.TargetGeneration, Generation: snapshot.Generation,
			ReleaseDigest: snapshot.ReleaseDigest, State: snapshot.State, CleanupPhase: snapshot.CleanupPhase,
			CPULimitMillis: snapshot.CPULimitMillis, MemoryLimitBytes: snapshot.MemoryLimitBytes,
			WorkerSPIFFEID: snapshot.WorkerSPIFFEID, WorkerServerName: snapshot.WorkerServerName,
			StableErrorCode: snapshot.StableErrorCode,
		}
		if snapshot.LastHealthAt != nil {
			spec.LastHealthAt = snapshot.LastHealthAt.UTC().Format(time.RFC3339Nano)
		}
		if snapshot.ReadyAt != nil {
			spec.ReadyAt = snapshot.ReadyAt.UTC().Format(time.RFC3339Nano)
		}
		if snapshot.Health != nil {
			spec.Health = &platformv1alpha1.WorkerHealthStatus{State: snapshot.Health.State,
				CheckedAt: snapshot.Health.CheckedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: snapshot.Health.ExpiresAt.UTC().Format(time.RFC3339Nano)}
			if snapshot.Health.LastSuccessAt != nil {
				spec.Health.LastSuccessAt = snapshot.Health.LastSuccessAt.UTC().Format(time.RFC3339Nano)
			}
		}
		workers = append(workers, platformv1alpha1.Worker{ResourceBase: platformv1alpha1.ResourceBase{
			APIVersion: platformv1alpha1.APIVersion, Kind: "Worker",
			Metadata: commonv1alpha1.ResourceMetadata{UID: snapshot.WorkerID, Name: snapshot.WorkerName,
				TenantRef:       commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
				ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano)},
		}, Spec: spec})
	}
	nextPageToken := ""
	if page.NextWorkerID != "" {
		var ok bool
		nextPageToken, ok = encodeAdminWorkerPageToken(tenantID, projectID, page.NextWorkerID)
		if !ok {
			writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
			return
		}
	}
	body, err := platformv1alpha1.EncodeWorkerPageResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.WorkerPage]{Value: platformv1alpha1.WorkerPage{APIVersion: platformv1alpha1.APIVersion, Kind: "WorkerPage", Workers: workers, NextPageToken: nextPageToken}})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Request-ID", requestID)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(body)
}
