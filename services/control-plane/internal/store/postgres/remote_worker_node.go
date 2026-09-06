package postgres

import (
	"context"
	"errors"
	"fmt"

	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/jackc/pgx/v5/pgconn"
)

type RemoteWorkerHeartbeatResult struct {
	Node              internalremoteworker.NodeStatus
	ReconcileRequired bool
}

const remoteWorkerNodeAdminColumns = `node_resource_version, node_generation, node_observed_generation,
    node_desired_state, node_observed_state,
    CASE WHEN node_last_heartbeat_at IS NULL THEN NULL
        WHEN certificate_state <> 'active' OR certificate_not_after <= clock_timestamp()
            OR node_heartbeat_expires_at <= clock_timestamp() THEN 'offline'
        WHEN node_last_heartbeat_at + interval '10 seconds' <= clock_timestamp() THEN 'degraded'
        ELSE 'online' END,
    node_worker_version, node_os, node_architecture, node_kernel_version, node_capabilities,
    node_capacity_cpu_millis, node_capacity_memory_bytes, node_capacity_disk_bytes,
    node_first_connected_at, node_last_heartbeat_at, node_heartbeat_expires_at`

const heartbeatRemoteWorkerSQL = `SELECT enrollment_uid, worker_uid, worker_name, incarnation_uid,
    node_resource_version, node_generation, node_observed_generation,
    node_desired_state, node_observed_state, node_health_state,
    node_worker_version, node_os, node_architecture, node_kernel_version, node_capabilities,
    node_capacity_cpu_millis, node_capacity_memory_bytes, node_capacity_disk_bytes,
    node_first_connected_at, node_last_heartbeat_at, node_heartbeat_expires_at,
    reconcile_required
FROM cloud_agents.heartbeat_remote_worker_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`

func (service *DurableCoordinationService) HeartbeatRemoteWorker(ctx context.Context, tenantID string, input internalremoteworker.HeartbeatInput) (RemoteWorkerHeartbeatResult, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerHeartbeatResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return RemoteWorkerHeartbeatResult{}, ErrCoordinationInvalidInput
	}
	result := RemoteWorkerHeartbeatResult{Node: internalremoteworker.NodeStatus{Scope: input.Scope}}
	err := service.runner.withTenantMutationBinder(ctx, tenantID, func(handle *tenantReadHandle) error {
		row := handle.transaction.queryRow(ctx, heartbeatRemoteWorkerSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.PeerCertificateSHA256,
			input.IncarnationID, input.ObservedGeneration, input.ObservedState, input.WorkerVersion,
			input.OS, input.Architecture, input.KernelVersion, input.Capabilities, input.Capacity.CPUMillis,
			input.Capacity.MemoryBytes, input.Capacity.DiskBytes)
		targets := append(remoteWorkerNodeScanTargets(&result.Node), &result.ReconcileRequired)
		if err := row.Scan(targets...); err != nil {
			return err
		}
		if result.Node.Validate() != nil || result.Node.EnrollmentID != input.EnrollmentID ||
			result.Node.IncarnationID != input.IncarnationID || result.Node.HealthState != "online" {
			return fmt.Errorf("%w: remote worker heartbeat projection", ErrCoordinationResultDrift)
		}
		return nil
	}, bindTenantSetting)
	return result, mapRemoteWorkerNodeError(err)
}

func assignRemoteWorkerNodeStatus(snapshot *internalremoteworker.Snapshot, row remoteWorkerEnrollmentRow) error {
	if row.NodeResourceVersion == nil {
		if row.NodeGeneration != nil || row.NodeObservedGeneration != nil || row.NodeDesiredState != nil ||
			row.NodeObservedState != nil || row.NodeHealthState != nil || row.NodeWorkerVersion != nil ||
			row.NodeOS != nil || row.NodeArchitecture != nil || row.NodeKernelVersion != nil || row.NodeCapabilities != nil ||
			row.NodeCapacityCPUMillis != nil || row.NodeCapacityMemory != nil || row.NodeCapacityDisk != nil ||
			row.NodeFirstConnectedAt != nil || row.NodeLastHeartbeatAt != nil || row.NodeHeartbeatExpiresAt != nil {
			return ErrCoordinationResultDrift
		}
		return nil
	}
	if row.NodeGeneration == nil || row.NodeObservedGeneration == nil || row.NodeDesiredState == nil ||
		row.NodeObservedState == nil || row.NodeHealthState == nil || row.NodeWorkerVersion == nil || row.NodeOS == nil ||
		row.NodeArchitecture == nil || row.NodeKernelVersion == nil || row.NodeCapabilities == nil ||
		row.NodeCapacityCPUMillis == nil || row.NodeCapacityMemory == nil || row.NodeCapacityDisk == nil ||
		row.NodeFirstConnectedAt == nil || row.NodeLastHeartbeatAt == nil || row.NodeHeartbeatExpiresAt == nil {
		return ErrCoordinationResultDrift
	}
	node := internalremoteworker.NodeStatus{
		Scope: snapshot.Scope, EnrollmentID: snapshot.EnrollmentID, WorkerID: snapshot.WorkerID,
		WorkerName: snapshot.WorkerName, IncarnationID: snapshot.IncarnationID,
		ResourceVersion: *row.NodeResourceVersion, Generation: *row.NodeGeneration,
		ObservedGeneration: *row.NodeObservedGeneration, DesiredState: *row.NodeDesiredState,
		ObservedState: *row.NodeObservedState, HealthState: *row.NodeHealthState,
		WorkerVersion: *row.NodeWorkerVersion, OS: *row.NodeOS, Architecture: *row.NodeArchitecture,
		KernelVersion: *row.NodeKernelVersion, Capabilities: row.NodeCapabilities,
		Capacity:         internalremoteworker.Capacity{CPUMillis: *row.NodeCapacityCPUMillis, MemoryBytes: *row.NodeCapacityMemory, DiskBytes: *row.NodeCapacityDisk},
		FirstConnectedAt: *row.NodeFirstConnectedAt, LastHeartbeatAt: *row.NodeLastHeartbeatAt,
		HeartbeatExpiresAt: *row.NodeHeartbeatExpiresAt,
	}
	if node.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	snapshot.Node = &node
	return nil
}

func remoteWorkerNodeRowScanTargets(row *remoteWorkerEnrollmentRow) []any {
	return []any{&row.NodeResourceVersion, &row.NodeGeneration, &row.NodeObservedGeneration,
		&row.NodeDesiredState, &row.NodeObservedState, &row.NodeHealthState,
		&row.NodeWorkerVersion, &row.NodeOS, &row.NodeArchitecture, &row.NodeKernelVersion,
		&row.NodeCapabilities, &row.NodeCapacityCPUMillis, &row.NodeCapacityMemory,
		&row.NodeCapacityDisk, &row.NodeFirstConnectedAt, &row.NodeLastHeartbeatAt,
		&row.NodeHeartbeatExpiresAt}
}

func remoteWorkerNodeScanTargets(node *internalremoteworker.NodeStatus) []any {
	return []any{&node.EnrollmentID, &node.WorkerID, &node.WorkerName, &node.IncarnationID,
		&node.ResourceVersion, &node.Generation, &node.ObservedGeneration, &node.DesiredState,
		&node.ObservedState, &node.HealthState, &node.WorkerVersion, &node.OS, &node.Architecture,
		&node.KernelVersion, &node.Capabilities, &node.Capacity.CPUMillis, &node.Capacity.MemoryBytes,
		&node.Capacity.DiskBytes, &node.FirstConnectedAt, &node.LastHeartbeatAt, &node.HeartbeatExpiresAt}
}

func mapRemoteWorkerNodeError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker certificate authentication failed":
			return ErrRemoteWorkerEnrollmentAuthentication
		case "remote worker generation conflict":
			return ErrRemoteWorkerGenerationConflict
		case "remote worker heartbeat input is invalid":
			return ErrCoordinationInvalidInput
		}
	}
	return mapRemoteWorkerEnrollmentError(err)
}

var ErrRemoteWorkerGenerationConflict = errors.New("remote worker generation conflicts")
