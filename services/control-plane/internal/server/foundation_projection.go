package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/store/postgres"
)

func sandboxSessionResource(result internalcoordination.FoundationSandboxSnapshot) platform.SandboxSession {
	return platform.SandboxSession{APIVersion: platform.APIVersion, Kind: "SandboxSession",
		ProjectRef:  common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: result.Scope.ProjectID},
		OperationID: result.OperationID, WorkspaceID: result.WorkspaceID, SandboxID: result.SandboxID,
		RuntimeProfileID: result.RuntimeProfileID, RuntimeProfileVersion: result.RuntimeProfileVersion,
		Generation: result.Generation, DesiredState: result.DesiredState, ObservedState: result.ObservedState,
		ExpiresAt: result.ExpiresAt.UTC().Format(time.RFC3339Nano)}
}

func runtimeProfileResource(snapshot internalcoordination.RuntimeProfileSnapshot) platform.RuntimeProfile {
	publishedAt, disabledAt := "", ""
	if snapshot.PublishedAt != nil {
		publishedAt = snapshot.PublishedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.DisabledAt != nil {
		disabledAt = snapshot.DisabledAt.UTC().Format(time.RFC3339Nano)
	}
	result := platform.RuntimeProfile{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "RuntimeProfile", Metadata: common.ResourceMetadata{
		UID: snapshot.ProfileVersionID, Name: snapshot.ProfileName,
		TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platform.RuntimeProfileSpec{
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		ProfileID:  snapshot.ProfileID, Version: snapshot.Version, Description: snapshot.Description,
		Status: snapshot.Status, WorkloadTrust: snapshot.WorkloadTrust, IsolationRuntime: snapshot.IsolationRuntime,
		TargetID: snapshot.TargetID, ImageURI: snapshot.ImageURI,
		NetworkPolicyRef: snapshot.NetworkPolicyID,
		ReleaseDigest:    snapshot.ReleaseDigest, CPUMillis: snapshot.CPUMillis, MemoryBytes: snapshot.MemoryBytes,
		PublishedAt: publishedAt, DisabledAt: disabledAt,
	}}
	if snapshot.TargetSelector != nil {
		result.Spec.TargetSelector = &platform.RuntimeProfileTargetSelector{
			RegionID: snapshot.TargetSelector.RegionID, ResourcePoolID: snapshot.TargetSelector.ResourcePoolID,
			Runtime: snapshot.TargetSelector.Runtime, Architecture: snapshot.TargetSelector.Architecture,
		}
	}
	return result
}

func runtimeProfileSummaryResource(summary internalcoordination.RuntimeProfileSummary) platform.RuntimeProfileSummary {
	return platform.RuntimeProfileSummary{APIVersion: platform.APIVersion, Kind: "RuntimeProfileSummary",
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: summary.Scope.ProjectID},
		ProfileID:  summary.ProfileID, Name: summary.ProfileName, Version: summary.Version,
		Description: summary.Description, Status: "published", Availability: "available",
		CPUMillis: summary.CPUMillis, MemoryBytes: summary.MemoryBytes, WorkspaceRetention: "retained"}
}

func adminSandboxAccessGrantResource(snapshot postgres.SandboxAccessGrantSnapshot) platform.AdminSandboxAccessGrant {
	revokedAt, lastFileAction, lastFileStatus, lastFileErrorCode, lastFileAccessAt := "", "", "", "", ""
	previewPorts := append([]int32{}, snapshot.PreviewPorts...)
	if snapshot.RevokedAt != nil {
		revokedAt = snapshot.RevokedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.LastFileAction != nil {
		lastFileAction = *snapshot.LastFileAction
		lastFileStatus = *snapshot.LastFileStatus
		lastFileAccessAt = snapshot.LastFileAccessAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.LastFileErrorCode != nil {
		lastFileErrorCode = *snapshot.LastFileErrorCode
	}
	return platform.AdminSandboxAccessGrant{ResourceBase: platform.ResourceBase{
		APIVersion: platform.APIVersion, Kind: "AdminSandboxAccessGrant", Metadata: common.ResourceMetadata{
			UID: snapshot.GrantID, Name: snapshot.GrantID,
			TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
			ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10),
			CreatedAt:       snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
		}}, Spec: platform.AdminSandboxAccessGrantSpec{
		ProjectRef: common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		SandboxID:  snapshot.SandboxID, Generation: snapshot.Generation, AccessKind: snapshot.AccessKind,
		Status: snapshot.Status, ExpiresAt: snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano),
		RevokedAt: revokedAt, PTYSessionCount: snapshot.PTYSessionCount,
		FileAccessCount: snapshot.FileAccessCount, FileFailureCount: snapshot.FileFailureCount,
		PreviewPorts:   previewPorts,
		LastFileAction: lastFileAction, LastFileStatus: lastFileStatus,
		LastFileErrorCode: lastFileErrorCode, LastFileAccessAt: lastFileAccessAt,
	}}
}

func adminSandboxResource(snapshot postgres.AdminSandboxSnapshot) platform.AdminSandboxSession {
	physicalVolumeID, runtimeID, stableErrorCode, observedAt, expiresAt, lifecycleTrigger := "", "", "", "", "", ""
	ttlSeconds := int64(0)
	var usage *platform.AdminSandboxUsage
	var workspaceVolumeUsage *platform.AdminWorkspaceVolumeUsage
	var networkUsage *platform.AdminSandboxNetworkUsage
	usageCorrections := make([]platform.AdminSandboxUsageCorrection, 0, len(snapshot.UsageCorrections))
	for _, correction := range snapshot.UsageCorrections {
		usageCorrections = append(usageCorrections, platform.AdminSandboxUsageCorrection{
			CorrectionID: correction.CorrectionID, Metric: correction.Metric, Adjustment: correction.Adjustment,
			ReasonCode: correction.ReasonCode, SandboxGeneration: correction.SandboxGeneration,
			PriorResourceVersion: strconv.FormatInt(correction.PriorResourceVersion, 10), RequestedBy: correction.RequestedBy,
			RequestID: correction.RequestID, CreatedAt: correction.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	if snapshot.PhysicalVolumeID != nil {
		physicalVolumeID = *snapshot.PhysicalVolumeID
	}
	if snapshot.RuntimeID != nil {
		runtimeID = *snapshot.RuntimeID
	}
	if snapshot.StableErrorCode != nil {
		stableErrorCode = *snapshot.StableErrorCode
	}
	if snapshot.ObservedAt != nil {
		observedAt = snapshot.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.TTLSeconds != nil {
		ttlSeconds = int64(*snapshot.TTLSeconds)
	}
	if snapshot.ExpiresAt != nil {
		expiresAt = snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.LifecycleTrigger != nil {
		lifecycleTrigger = *snapshot.LifecycleTrigger
	}
	if snapshot.Usage != nil {
		finalizedAt := ""
		if snapshot.Usage.FinalizedAt != nil {
			finalizedAt = snapshot.Usage.FinalizedAt.UTC().Format(time.RFC3339Nano)
		}
		usage = &platform.AdminSandboxUsage{
			LatestRuntimeGeneration: snapshot.Usage.LatestRuntimeGeneration,
			AllocatedMilliseconds:   snapshot.Usage.AllocatedMilliseconds,
			CPUMillisMilliseconds:   snapshot.Usage.CPUMillisMilliseconds,
			MemoryByteMilliseconds:  snapshot.Usage.MemoryByteMilliseconds,
			CheckpointedAt:          snapshot.Usage.CheckpointedAt.UTC().Format(time.RFC3339Nano), FinalizedAt: finalizedAt,
		}
	}
	if snapshot.WorkspaceVolumeUsage != nil {
		usedBytes, checkpointedAt, observedAt, stableErrorCode := "", "", "", ""
		if snapshot.WorkspaceVolumeUsage.UsedBytes != nil {
			usedBytes = *snapshot.WorkspaceVolumeUsage.UsedBytes
		}
		if snapshot.WorkspaceVolumeUsage.CheckpointedAt != nil {
			checkpointedAt = snapshot.WorkspaceVolumeUsage.CheckpointedAt.UTC().Format(time.RFC3339Nano)
		}
		if snapshot.WorkspaceVolumeUsage.ObservedAt != nil {
			observedAt = snapshot.WorkspaceVolumeUsage.ObservedAt.UTC().Format(time.RFC3339Nano)
		}
		if snapshot.WorkspaceVolumeUsage.StableErrorCode != nil {
			stableErrorCode = *snapshot.WorkspaceVolumeUsage.StableErrorCode
		}
		workspaceVolumeUsage = &platform.AdminWorkspaceVolumeUsage{
			Source:                "docker-system-df-v1",
			MeasurementGeneration: snapshot.WorkspaceVolumeUsage.MeasurementGeneration,
			State:                 snapshot.WorkspaceVolumeUsage.State,
			UsedBytes:             usedBytes,
			CheckpointedAt:        checkpointedAt,
			ObservedAt:            observedAt,
			StableErrorCode:       stableErrorCode,
		}
	}
	if snapshot.NetworkUsage != nil {
		receivedBytes, transmittedBytes, checkpointedAt, stableErrorCode := "", "", "", ""
		if snapshot.NetworkUsage.ReceivedBytes != nil {
			receivedBytes = *snapshot.NetworkUsage.ReceivedBytes
			transmittedBytes = *snapshot.NetworkUsage.TransmittedBytes
			checkpointedAt = snapshot.NetworkUsage.CheckpointedAt.UTC().Format(time.RFC3339Nano)
		}
		if snapshot.NetworkUsage.StableErrorCode != nil {
			stableErrorCode = *snapshot.NetworkUsage.StableErrorCode
		}
		networkUsage = &platform.AdminSandboxNetworkUsage{
			Source:                  "docker-container-stats-v1",
			LatestRuntimeGeneration: snapshot.NetworkUsage.LatestRuntimeGeneration,
			MeasurementGeneration:   snapshot.NetworkUsage.MeasurementGeneration,
			State:                   snapshot.NetworkUsage.State,
			ReceivedBytes:           receivedBytes,
			TransmittedBytes:        transmittedBytes,
			CheckpointedAt:          checkpointedAt,
			ObservedAt:              snapshot.NetworkUsage.ObservedAt.UTC().Format(time.RFC3339Nano),
			StableErrorCode:         stableErrorCode,
		}
	}
	return platform.AdminSandboxSession{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "AdminSandboxSession", Metadata: common.ResourceMetadata{
		UID: snapshot.SandboxID, Name: snapshot.SandboxID,
		TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platform.AdminSandboxSessionSpec{
		ProjectRef:  common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		OperationID: snapshot.OperationID, OperationState: snapshot.OperationState, CleanupPhase: snapshot.CleanupPhase,
		WorkspaceID: snapshot.WorkspaceID, WorkspaceName: snapshot.WorkspaceName, VolumeID: snapshot.VolumeID,
		PhysicalVolumeID: physicalVolumeID, WorkspaceRetention: "retained", WorkspaceObservedState: snapshot.WorkspaceObservedState,
		RuntimeProfileID: snapshot.RuntimeProfileID, RuntimeProfileVersion: snapshot.RuntimeProfileVersion,
		WorkloadTrust: snapshot.WorkloadTrust, IsolationRuntime: snapshot.IsolationRuntime,
		NetworkPolicyRef: snapshot.NetworkPolicyID, NetworkPolicyEnforcement: snapshot.NetworkPolicyEnforcement,
		TargetID: snapshot.TargetID, Generation: snapshot.Generation, ObservedGeneration: snapshot.ObservedGeneration,
		DesiredState: snapshot.DesiredState, ObservedState: snapshot.ObservedState, WriterReleased: snapshot.WriterReleased,
		TTLSeconds: ttlSeconds, ExpiresAt: expiresAt, LifecycleTrigger: lifecycleTrigger,
		RuntimeID: runtimeID, RuntimeState: snapshot.RuntimeState, StableErrorCode: stableErrorCode, ObservedAt: observedAt,
		Usage: usage, WorkspaceVolumeUsage: workspaceVolumeUsage, NetworkUsage: networkUsage,
		UsageCorrections: usageCorrections,
	}}
}

func workspaceSnapshotResource(snapshot postgres.WorkspaceSnapshot) platform.WorkspaceSnapshot {
	stableErrorCode, expiresAt, observedAt, deletedAt := "", "", "", ""
	cleanupOperationID, cleanupTrigger := "", ""
	if snapshot.StableErrorCode != nil {
		stableErrorCode = *snapshot.StableErrorCode
	}
	if snapshot.ObservedAt != nil {
		observedAt = snapshot.ObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.ExpiresAt != nil {
		expiresAt = snapshot.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if snapshot.CleanupOperationID != nil {
		cleanupOperationID = *snapshot.CleanupOperationID
	}
	if snapshot.CleanupTrigger != nil {
		cleanupTrigger = *snapshot.CleanupTrigger
	}
	if snapshot.DeletedAt != nil {
		deletedAt = snapshot.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	return platform.WorkspaceSnapshot{ResourceBase: platform.ResourceBase{APIVersion: platform.APIVersion, Kind: "WorkspaceSnapshot", Metadata: common.ResourceMetadata{
		UID: snapshot.SnapshotID, Name: snapshot.SnapshotID,
		TenantRef:       common.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: snapshot.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(snapshot.ResourceVersion, 10), CreatedAt: snapshot.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: snapshot.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platform.WorkspaceSnapshotSpec{
		ProjectRef:        common.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: snapshot.Scope.ProjectID},
		SourceWorkspaceID: snapshot.SourceWorkspaceID, SourceWorkspaceResourceVersion: strconv.FormatInt(snapshot.SourceWorkspaceResourceVersion, 10),
		SourceTargetID: snapshot.SourceTargetID, Backend: snapshot.Backend, ConsistencyMode: snapshot.ConsistencyMode, Status: snapshot.Status,
		OperationID: snapshot.OperationID, RetentionSeconds: snapshot.RetentionSeconds, ExpiresAt: expiresAt,
		CleanupOperationID: cleanupOperationID, CleanupTrigger: cleanupTrigger, SizeBytes: snapshot.SizeBytes,
		StableErrorCode: stableErrorCode, ObservedAt: observedAt, DeletedAt: deletedAt,
	}}
}

func sandboxLifecycleOperationResource(operation internalcoordination.FoundationSandboxLifecycleOperation) platform.SandboxSessionLifecycleOperation {
	step := "pending-controller"
	switch operation.State {
	case "running":
		if operation.Action == "sandbox.stop" {
			step = "deleting-compute"
		} else {
			step = "creating-compute"
		}
	case "reconciling":
		step = "retrying"
	case "succeeded":
		step = "complete"
	case "failed":
		step = "failed"
	}
	return platform.SandboxSessionLifecycleOperation{
		APIVersion: platform.APIVersion, Kind: "SandboxSessionLifecycleOperation",
		OperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey,
		Action: operation.Action, SandboxID: operation.SandboxID, SandboxGeneration: operation.SandboxGeneration,
		RequestedBy: operation.RequestedBy, RequestID: operation.RequestID,
		RequestedAt: operation.RequestedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		State: operation.State, CurrentStep: step, CleanupPhase: operation.CleanupPhase,
		StableErrorCode: operation.StableErrorCode, ComputeDisposition: operation.ComputeDisposition,
		WorkspaceDisposition: operation.WorkspaceDisposition,
	}
}

func foundationRequestDigest(value any) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func writeRuntimeProfile(writer http.ResponseWriter, status int, requestID string, snapshot internalcoordination.RuntimeProfileSnapshot) {
	body, err := platform.EncodeRuntimeProfileResponseJSON(common.ResponseEnvelope[platform.RuntimeProfile]{Value: runtimeProfileResource(snapshot)})
	if err != nil {
		writePublicProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(snapshot.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}
