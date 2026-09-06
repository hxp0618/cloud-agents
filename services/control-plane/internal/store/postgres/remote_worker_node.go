package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RemoteWorkerHeartbeatResult struct {
	Node              internalremoteworker.NodeStatus
	ReconcileRequired bool
	Command           *internalremoteworker.Command
}

type RemoteWorkerOperationPage struct {
	Operations      []internalremoteworker.Operation
	NextRequestedAt *time.Time
	NextOperationID string
}

type remoteWorkerOperationRow struct {
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_uid"`
	EnrollmentID    string    `json:"enrollment_uid"`
	OperationID     string    `json:"operation_uid"`
	CommandID       string    `json:"command_uid"`
	IdempotencyKey  string    `json:"idempotency_key"`
	Action          string    `json:"action"`
	Generation      int64     `json:"node_generation"`
	RequestedBy     string    `json:"subject_digest"`
	RequestID       string    `json:"request_id"`
	RequestedAt     time.Time `json:"requested_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	State           string    `json:"state"`
	CurrentStep     string    `json:"current_step"`
	StableErrorCode string    `json:"stable_error_code"`
	ImpactSummary   string    `json:"impact_summary"`
	Retryable       bool      `json:"retryable"`
	CommandDeadline time.Time `json:"command_deadline_at"`
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
    reconcile_required, command_uid, command_generation, command_desired_state, command_deadline_at
FROM cloud_agents.heartbeat_remote_worker_v2($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`

const lockRemoteWorkerSchedulingSQL = `SELECT cloud_agents.lock_remote_worker_scheduling_v1($1,$2,$3)`
const transitionRemoteWorkerSchedulingSQL = `SELECT operation_uid, idempotency_key, action, enrollment_uid,
    command_uid, node_generation, subject_digest, request_id, requested_at, updated_at, state,
    current_step, stable_error_code, impact_summary, retryable, command_deadline_at
FROM cloud_agents.transition_remote_worker_scheduling_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
const remoteWorkerOperationCursorSQL = `SELECT 1 FROM cloud_agents.remote_worker_node_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
  AND operation_uid = $3 AND requested_at = $4`
const listRemoteWorkerOperationsSQL = `WITH latest AS (
    SELECT DISTINCT ON (operation_uid) tenant_id, project_uid, enrollment_uid, operation_uid,
        command_uid, idempotency_key, action, node_generation, subject_digest, request_id,
        requested_at, occurred_at AS updated_at, state, current_step,
        COALESCE(stable_error_code, '') AS stable_error_code, impact_summary, retryable,
        command_deadline_at
    FROM cloud_agents.remote_worker_node_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
    ORDER BY operation_uid, occurred_at DESC, event_uid DESC
), operation_page AS (
    SELECT * FROM latest
    WHERE $3::timestamptz IS NULL OR (requested_at, operation_uid) < ($3, $4)
    ORDER BY requested_at DESC, operation_uid DESC LIMIT $5
)
SELECT COALESCE(jsonb_agg(to_jsonb(operation_row)
    ORDER BY operation_row.requested_at DESC, operation_row.operation_uid DESC), '[]'::jsonb)
FROM operation_page AS operation_row`

func (service *DurableCoordinationService) HeartbeatRemoteWorker(ctx context.Context, tenantID string, input internalremoteworker.HeartbeatInput) (RemoteWorkerHeartbeatResult, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerHeartbeatResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return RemoteWorkerHeartbeatResult{}, ErrCoordinationInvalidInput
	}
	result := RemoteWorkerHeartbeatResult{Node: internalremoteworker.NodeStatus{Scope: input.Scope}}
	var receiptCommand, receiptResult, receiptError, receiptDigest any
	var receiptGeneration any
	if input.CommandReceipt != nil {
		digest, digestErr := internalremoteworker.CommandReceiptDigest(*input.CommandReceipt)
		if digestErr != nil {
			return RemoteWorkerHeartbeatResult{}, ErrCoordinationInvalidInput
		}
		receiptCommand, receiptGeneration, receiptResult, receiptDigest = input.CommandReceipt.CommandID, input.CommandReceipt.Generation, input.CommandReceipt.Result, digest
		if input.CommandReceipt.StableErrorCode != "" {
			receiptError = input.CommandReceipt.StableErrorCode
		}
	}
	err := service.runner.withTenantMutationBinder(ctx, tenantID, func(handle *tenantReadHandle) error {
		row := handle.transaction.queryRow(ctx, heartbeatRemoteWorkerSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.PeerCertificateSHA256,
			input.IncarnationID, input.ObservedGeneration, input.ObservedState, input.WorkerVersion,
			input.OS, input.Architecture, input.KernelVersion, input.Capabilities, input.Capacity.CPUMillis,
			input.Capacity.MemoryBytes, input.Capacity.DiskBytes, receiptCommand, receiptGeneration,
			receiptResult, receiptError, receiptDigest)
		var commandID, desiredState *string
		var commandGeneration *int64
		var commandDeadline *time.Time
		targets := append(remoteWorkerNodeScanTargets(&result.Node), &result.ReconcileRequired, &commandID,
			&commandGeneration, &desiredState, &commandDeadline)
		if err := row.Scan(targets...); err != nil {
			return err
		}
		if commandID != nil || commandGeneration != nil || desiredState != nil || commandDeadline != nil {
			if commandID == nil || commandGeneration == nil || desiredState == nil || commandDeadline == nil {
				return fmt.Errorf("%w: remote worker command projection", ErrCoordinationResultDrift)
			}
			command := internalremoteworker.Command{CommandID: *commandID, Generation: *commandGeneration, DesiredState: *desiredState, Deadline: *commandDeadline}
			if command.Validate() != nil || command.Generation != result.Node.Generation || command.DesiredState != result.Node.DesiredState {
				return fmt.Errorf("%w: remote worker command projection", ErrCoordinationResultDrift)
			}
			result.Command = &command
		}
		if result.Node.Validate() != nil || result.Node.EnrollmentID != input.EnrollmentID ||
			result.Node.IncarnationID != input.IncarnationID || result.Node.HealthState != "online" {
			return fmt.Errorf("%w: remote worker heartbeat projection", ErrCoordinationResultDrift)
		}
		return nil
	}, bindTenantSetting)
	return result, mapRemoteWorkerNodeError(err)
}

func (service *DurableCoordinationService) PreviewRemoteWorkerScheduling(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, enrollmentID string) (internalremoteworker.SchedulingPreview, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.SchedulingPreview{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(enrollmentID) {
		return internalremoteworker.SchedulingPreview{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.SchedulingPreview
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(readContext context.Context, handle *tenantReadHandle, _ string) error {
		var enrollment internalremoteworker.Snapshot
		if err := scanRemoteWorkerEnrollmentWithNode(handle.transaction.queryRow(readContext, getRemoteWorkerEnrollmentSQL, projectID, enrollmentID), internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}, &enrollment); err != nil {
			return err
		}
		var err error
		result, err = internalremoteworker.NewSchedulingPreview(enrollment)
		return err
	})
	return result, mapRemoteWorkerNodeError(err)
}

func (service *DurableCoordinationService) TransitionRemoteWorkerScheduling(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalremoteworker.SchedulingInput) (internalremoteworker.Operation, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Operation{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalremoteworker.Operation{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.SchedulingMutationDigest(input)
	if err != nil {
		return internalremoteworker.Operation{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Operation
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		var found bool
		if err := handle.transaction.queryRow(operationContext, lockRemoteWorkerSchedulingSQL, tenantID, input.Scope.ProjectID, input.EnrollmentID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrRemoteWorkerEnrollmentNotFound
		}
		var enrollment internalremoteworker.Snapshot
		if err := scanRemoteWorkerEnrollmentWithNode(handle.transaction.queryRow(operationContext, getRemoteWorkerEnrollmentSQL, input.Scope.ProjectID, input.EnrollmentID), input.Scope, &enrollment); err != nil {
			return err
		}
		preview, err := internalremoteworker.NewSchedulingPreview(enrollment)
		if err != nil || preview.DesiredState != input.DesiredState {
			return ErrRemoteWorkerSchedulingStateConflict
		}
		return scanRemoteWorkerOperation(handle.transaction.queryRow(operationContext, transitionRemoteWorkerSchedulingSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.ExpectedGeneration,
			input.ExpectedResourceVersion, input.DesiredState, input.ImpactDigest, preview.ImpactDigest,
			input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID, subjectDigest, preview.ImpactSummary), input.Scope, &result)
	})
	return result, mapRemoteWorkerNodeError(err)
}

func (service *DurableCoordinationService) ListRemoteWorkerOperations(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, enrollmentID string, afterRequestedAt *time.Time, afterOperationID string, limit int) (RemoteWorkerOperationPage, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerOperationPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(enrollmentID) ||
		(afterRequestedAt == nil) != (afterOperationID == "") || afterOperationID != "" && !validMutationIdentifier(afterOperationID) || limit < 1 || limit > 200 {
		return RemoteWorkerOperationPage{}, ErrCoordinationInvalidInput
	}
	var result RemoteWorkerOperationPage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(readContext context.Context, handle *tenantReadHandle, _ string) error {
		var exists int
		if err := handle.transaction.queryRow(readContext, remoteWorkerEnrollmentAuditIdentitySQL, projectID, enrollmentID).Scan(&exists); err != nil {
			return err
		}
		if afterRequestedAt != nil {
			if err := handle.transaction.queryRow(readContext, remoteWorkerOperationCursorSQL, projectID, enrollmentID, afterOperationID, *afterRequestedAt).Scan(&exists); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrCoordinationInvalidInput
				}
				return err
			}
		}
		var raw []byte
		if err := handle.transaction.queryRow(readContext, listRemoteWorkerOperationsSQL, projectID, enrollmentID, afterRequestedAt, afterOperationID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var err error
		result, err = decodeRemoteWorkerOperationRows(raw, tenantID, projectID, enrollmentID, limit)
		return err
	})
	return result, mapRemoteWorkerNodeError(err)
}

func scanRemoteWorkerOperation(row rowScanner, scope internalremoteworker.Scope, result *internalremoteworker.Operation) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.OperationID, &result.IdempotencyKey, &result.Action, &result.EnrollmentID,
		&result.CommandID, &result.Generation, &result.RequestedBy, &result.RequestID, &result.RequestedAt,
		&result.UpdatedAt, &result.State, &result.CurrentStep, &result.StableErrorCode, &result.ImpactSummary,
		&result.Retryable, &result.CommandDeadline); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func decodeRemoteWorkerOperationRows(raw []byte, tenantID, projectID, enrollmentID string, limit int) (RemoteWorkerOperationPage, error) {
	var rows []remoteWorkerOperationRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return RemoteWorkerOperationPage{}, ErrCoordinationResultDrift
	}
	operations := make([]internalremoteworker.Operation, 0, len(rows))
	for _, row := range rows {
		operation := internalremoteworker.Operation{Scope: internalremoteworker.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID}, OperationID: row.OperationID, CommandID: row.CommandID, IdempotencyKey: row.IdempotencyKey, Action: row.Action, EnrollmentID: row.EnrollmentID, Generation: row.Generation, RequestedBy: row.RequestedBy, RequestID: row.RequestID, RequestedAt: row.RequestedAt, UpdatedAt: row.UpdatedAt, State: row.State, CurrentStep: row.CurrentStep, StableErrorCode: row.StableErrorCode, ImpactSummary: row.ImpactSummary, Retryable: row.Retryable, CommandDeadline: row.CommandDeadline}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.EnrollmentID != enrollmentID || operation.Validate() != nil {
			return RemoteWorkerOperationPage{}, ErrCoordinationResultDrift
		}
		operations = append(operations, operation)
	}
	result := RemoteWorkerOperationPage{Operations: operations}
	if len(operations) > limit {
		result.Operations = operations[:limit]
		last := result.Operations[len(result.Operations)-1]
		requestedAt := last.RequestedAt
		result.NextRequestedAt, result.NextOperationID = &requestedAt, last.OperationID
	}
	return result, nil
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
	for _, known := range []error{ErrRemoteWorkerSchedulingStateConflict, ErrRemoteWorkerEnrollmentNotFound} {
		if errors.Is(err, known) {
			return known
		}
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker certificate authentication failed":
			return ErrRemoteWorkerEnrollmentAuthentication
		case "remote worker generation conflict":
			return ErrRemoteWorkerGenerationConflict
		case "remote worker command receipt conflict":
			return ErrRemoteWorkerCommandReceiptConflict
		case "remote worker scheduling idempotency conflict":
			return ErrRemoteWorkerSchedulingIdempotencyConflict
		case "remote worker operation is in progress":
			return ErrRemoteWorkerOperationInProgress
		case "remote worker resource version conflict":
			return ErrRemoteWorkerSchedulingResourceVersionConflict
		case "remote worker scheduling state conflict":
			return ErrRemoteWorkerSchedulingStateConflict
		case "remote worker scheduling impact conflict":
			return ErrRemoteWorkerSchedulingImpactConflict
		case "remote worker node is unavailable":
			return ErrRemoteWorkerNodeUnavailable
		case "remote worker heartbeat input is invalid":
			return ErrCoordinationInvalidInput
		}
	}
	return mapRemoteWorkerEnrollmentError(err)
}

var ErrRemoteWorkerGenerationConflict = errors.New("remote worker generation conflicts")
var ErrRemoteWorkerCommandReceiptConflict = errors.New("remote worker command receipt conflicts")
var ErrRemoteWorkerSchedulingIdempotencyConflict = errors.New("remote worker scheduling idempotency key conflicts")
var ErrRemoteWorkerOperationInProgress = errors.New("remote worker operation is in progress")
var ErrRemoteWorkerSchedulingResourceVersionConflict = errors.New("remote worker scheduling resource version conflicts")
var ErrRemoteWorkerSchedulingStateConflict = errors.New("remote worker scheduling state conflicts")
var ErrRemoteWorkerSchedulingImpactConflict = errors.New("remote worker scheduling impact conflicts")
var ErrRemoteWorkerNodeUnavailable = errors.New("remote worker node is unavailable")
