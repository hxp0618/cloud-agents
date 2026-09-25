package server

import (
	"net/http"
	"strconv"
	"time"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
)

func remoteWorkerWorkspaceSnapshotCommandReceipt(value *platformv1alpha1.RemoteWorkerWorkspaceSnapshotCommandReceipt) *internalremoteworker.WorkspaceSnapshotCommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.WorkspaceSnapshotCommandReceipt{CommandID: value.CommandID, Attempt: value.Attempt, Action: value.Action, OperationID: value.OperationID, WorkspaceID: value.WorkspaceID, TargetID: value.TargetID, SnapshotID: value.SnapshotID, Result: value.Result, VolumeName: value.VolumeName, ContentDigest: value.ContentDigest, SizeBytes: value.SizeBytes, StableErrorCode: value.StableErrorCode, CleanupComplete: value.CleanupComplete}
}

func remoteWorkerSandboxExecCommandReceipt(value *platformv1alpha1.RemoteWorkerSandboxExecCommandReceipt) *internalremoteworker.SandboxExecCommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.SandboxExecCommandReceipt{CommandID: value.CommandID, SandboxID: value.SandboxID,
		SandboxGeneration: value.SandboxGeneration, Result: value.Result, ExitCode: value.ExitCode,
		Stdout: value.Stdout, Stderr: value.Stderr, ExecutionTimeMillis: value.ExecutionTimeMillis,
		StableErrorCode: value.StableErrorCode}
}

func remoteWorkerSandboxCommandReceipt(value *platformv1alpha1.RemoteWorkerSandboxCommandReceipt) *internalremoteworker.SandboxCommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.SandboxCommandReceipt{CommandID: value.CommandID, Attempt: value.Attempt, Action: value.Action,
		OperationID: value.OperationID, SandboxID: value.SandboxID, SandboxGeneration: value.SandboxGeneration,
		Result: value.Result, RuntimeID: value.RuntimeID, RuntimeState: value.RuntimeState,
		VolumeName: value.VolumeName, StableErrorCode: value.StableErrorCode, CleanupComplete: value.CleanupComplete}
}

func remoteWorkerCommandReceipt(value *platformv1alpha1.RemoteWorkerCommandReceipt) *internalremoteworker.CommandReceipt {
	if value == nil {
		return nil
	}
	return &internalremoteworker.CommandReceipt{CommandID: value.CommandID, Generation: value.Generation, Result: value.Result, StableErrorCode: value.StableErrorCode}
}

func remoteWorkerCommandResource(value *internalremoteworker.Command) *platformv1alpha1.RemoteWorkerCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerCommand{CommandID: value.CommandID, Generation: value.Generation, DesiredState: value.DesiredState, Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func remoteWorkerWorkspaceSnapshotCommandResource(value *internalremoteworker.WorkspaceSnapshotCommand) *platformv1alpha1.RemoteWorkerWorkspaceSnapshotCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerWorkspaceSnapshotCommand{CommandID: value.CommandID, Attempt: value.Attempt, Action: value.Action, OperationID: value.OperationID, WorkspaceID: value.WorkspaceID, TargetID: value.TargetID, SnapshotID: value.SnapshotID, SourceVolumeName: value.SourceVolumeName, ImageURI: value.ImageURI, Deadline: value.Deadline}
}

func remoteWorkerSandboxCommandResource(value *internalremoteworker.SandboxCommand) *platformv1alpha1.RemoteWorkerSandboxCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerSandboxCommand{CommandID: value.CommandID, Attempt: value.Attempt,
		Action: value.Action, OperationID: value.OperationID, WorkspaceID: value.WorkspaceID,
		WorkspaceName: value.WorkspaceName, TargetID: value.TargetID, SandboxID: value.SandboxID,
		SandboxGeneration: value.SandboxGeneration, ImageURI: value.ImageURI, CPUMillis: value.CPUMillis,
		WorkloadTrust: value.WorkloadTrust, IsolationRuntime: value.IsolationRuntime,
		MemoryBytes: value.MemoryBytes, SpecDigest: value.SpecDigest, NetworkPolicyID: value.NetworkPolicyID,
		NetworkAllowedEgress: value.NetworkAllowedEgress, PhysicalVolumeName: value.PhysicalVolumeName,
		RuntimeID: value.RuntimeID, RuntimeState: value.RuntimeState, RuntimeOperationID: value.RuntimeOperationID,
		RuntimeGeneration: value.RuntimeGeneration, RuntimeSpecDigest: value.RuntimeSpecDigest, RestoreSnapshotID: value.RestoreSnapshotID, RestoreSourceWorkspaceID: value.RestoreSourceWorkspaceID, RestoreSnapshotVolume: value.RestoreSnapshotVolume, RestoreContentDigest: value.RestoreContentDigest, RestoreSnapshotResourceVersion: value.RestoreSnapshotResourceVersion,
		Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func remoteWorkerSandboxExecCommandResource(value *internalremoteworker.SandboxExecCommand) *platformv1alpha1.RemoteWorkerSandboxExecCommand {
	if value == nil {
		return nil
	}
	return &platformv1alpha1.RemoteWorkerSandboxExecCommand{CommandID: value.CommandID, WorkspaceID: value.WorkspaceID,
		TargetID: value.TargetID, SandboxID: value.SandboxID, SandboxGeneration: value.SandboxGeneration,
		RuntimeID: value.RuntimeID, RuntimeOperationID: value.RuntimeOperationID,
		RuntimeSpecDigest: value.RuntimeSpecDigest, Command: value.Command, TimeoutSeconds: value.TimeoutSeconds,
		Deadline: value.Deadline.UTC().Format(time.RFC3339Nano)}
}

func writeRemoteWorkerCertificate(writer http.ResponseWriter, requestID, projectID, enrollmentID string, value internalremoteworker.Snapshot, issuedAt *time.Time) {
	if issuedAt == nil || value.CertificateNotAfter == nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	responseBody, err := platformv1alpha1.EncodeRemoteWorkerCertificateResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerCertificate]{Value: platformv1alpha1.RemoteWorkerCertificate{
		APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerCertificate",
		ProjectRef:   commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: projectID},
		EnrollmentID: enrollmentID, WorkerID: value.WorkerID, IncarnationID: value.IncarnationID,
		SPIFFEID: value.SPIFFEID, CertificateChainPEM: value.CertificateChainPEM, CertificateSHA256: value.CertificateSHA256,
		IssuedAt: issuedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: value.CertificateNotAfter.UTC().Format(time.RFC3339Nano),
	}})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writeJSONResponse(writer, 200, requestID, responseBody)
}

func remoteWorkerOperationResource(operation internalremoteworker.Operation) platformv1alpha1.MaintenanceOperation {
	return platformv1alpha1.MaintenanceOperation{APIVersion: platformv1alpha1.APIVersion, Kind: "MaintenanceOperation",
		OperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey, Action: operation.Action,
		ResourceKind: "RemoteWorkerEnrollment", ResourceID: operation.EnrollmentID, ResourceGeneration: operation.Generation,
		RequestedBy: operation.RequestedBy, RequestID: operation.RequestID,
		RequestedAt: operation.RequestedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: operation.UpdatedAt.UTC().Format(time.RFC3339Nano),
		State: operation.State, CurrentStep: operation.CurrentStep, StableErrorCode: operation.StableErrorCode,
		ImpactSummary: operation.ImpactSummary, Retryable: operation.Retryable}
}

func writeRemoteWorkerOperation(writer http.ResponseWriter, status int, requestID string, operation internalremoteworker.Operation) {
	body, err := platformv1alpha1.EncodeMaintenanceOperationResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.MaintenanceOperation]{Value: remoteWorkerOperationResource(operation)})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writeJSONResponse(writer, status, requestID, body)
}

func remoteWorkerEnrollmentResource(value internalremoteworker.Snapshot) platformv1alpha1.RemoteWorkerEnrollment {
	format := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	return platformv1alpha1.RemoteWorkerEnrollment{ResourceBase: platformv1alpha1.ResourceBase{APIVersion: platformv1alpha1.APIVersion, Kind: "RemoteWorkerEnrollment", Metadata: commonv1alpha1.ResourceMetadata{
		UID: value.EnrollmentID, Name: value.WorkerName, TenantRef: commonv1alpha1.TenantRef{Namespace: "cloud-agents", Kind: "tenant", ID: value.Scope.TenantID},
		ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), CreatedAt: value.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: value.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}}, Spec: platformv1alpha1.RemoteWorkerEnrollmentSpec{ProjectRef: commonv1alpha1.ProjectRef{Namespace: "cloud-agents", Kind: "project", ID: value.Scope.ProjectID}, TargetID: value.TargetID, WorkerID: value.WorkerID, State: value.State, ExpiresAt: value.ExpiresAt.UTC().Format(time.RFC3339Nano), SecretClaimedAt: format(value.SecretClaimedAt), EnrolledAt: format(value.EnrolledAt), RevokedAt: format(value.RevokedAt), IncarnationID: value.IncarnationID, SPIFFEID: value.SPIFFEID, CertificateSHA256: value.CertificateSHA256, CertificateExpiresAt: format(value.CertificateNotAfter), CertificateState: value.CertificateState, CertificateRevokedAt: format(value.CertificateRevokedAt), Node: remoteWorkerNodeStatusResource(value.Node)}}
}

func remoteWorkerNodeStatusResource(value *internalremoteworker.NodeStatus) *platformv1alpha1.RemoteWorkerNodeStatus {
	if value == nil {
		return nil
	}
	result := &platformv1alpha1.RemoteWorkerNodeStatus{
		ResourceVersion: strconv.FormatInt(value.ResourceVersion, 10), Generation: value.Generation,
		ObservedGeneration: value.ObservedGeneration, DesiredState: value.DesiredState,
		ObservedState: value.ObservedState, HealthState: value.HealthState, WorkerVersion: value.WorkerVersion,
		OS: value.OS, Architecture: value.Architecture, KernelVersion: value.KernelVersion,
		Capabilities:       value.Capabilities,
		Capacity:           platformv1alpha1.RemoteWorkerCapacity{CPUMillis: value.Capacity.CPUMillis, MemoryBytes: value.Capacity.MemoryBytes, DiskBytes: value.Capacity.DiskBytes},
		FirstConnectedAt:   value.FirstConnectedAt.UTC().Format(time.RFC3339Nano),
		LastHeartbeatAt:    value.LastHeartbeatAt.UTC().Format(time.RFC3339Nano),
		HeartbeatExpiresAt: value.HeartbeatExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	if value.Placement != nil && value.Reservation != nil {
		result.Placement = &platformv1alpha1.RemoteWorkerNodePlacement{RegionID: value.Placement.RegionID, ResourcePoolID: value.Placement.ResourcePoolID, NodeID: value.Placement.NodeID}
		result.Reservation = &platformv1alpha1.RemoteWorkerCapacityReservation{State: value.Reservation.State,
			ReservedCPUMillis: value.Reservation.ReservedCPUMillis, ReservedMemoryBytes: value.Reservation.ReservedMemoryBytes,
			ReservedDiskBytes: value.Reservation.ReservedDiskBytes, AvailableCPUMillis: value.Reservation.AvailableCPUMillis,
			AvailableMemoryBytes: value.Reservation.AvailableMemoryBytes, AvailableDiskBytes: value.Reservation.AvailableDiskBytes}
	}
	return result
}

func writeRemoteWorkerEnrollment(writer http.ResponseWriter, status int, requestID string, value internalremoteworker.Snapshot) {
	body, err := platformv1alpha1.EncodeRemoteWorkerEnrollmentResponseJSON(commonv1alpha1.ResponseEnvelope[platformv1alpha1.RemoteWorkerEnrollment]{Value: remoteWorkerEnrollmentResource(value)})
	if err != nil {
		writePublicProblem(writer, 500, "internal_error")
		return
	}
	writer.Header().Set("X-Resource-Version", strconv.FormatInt(value.ResourceVersion, 10))
	writeJSONResponse(writer, status, requestID, body)
}
