package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type AdminSandboxSnapshot struct {
	Scope internalcoordination.FoundationScope

	SandboxID, OperationID, OperationState, CleanupPhase  string
	WorkspaceID, WorkspaceName, VolumeID, TargetID        string
	WorkspaceObservedState                                string
	RuntimeProfileID                                      string
	WorkloadTrust, IsolationRuntime                       string
	NetworkPolicyID, NetworkPolicyEnforcement             string
	DesiredState, ObservedState                           string
	RuntimeState                                          string
	PhysicalVolumeID, RuntimeID, StableErrorCode          *string
	LifecycleTrigger                                      *string
	TTLSeconds                                            *int32
	RuntimeProfileVersion, Generation, ObservedGeneration int64
	ResourceVersion                                       int64
	WriterReleased                                        bool
	CreatedAt, UpdatedAt                                  time.Time
	ObservedAt, ExpiresAt                                 *time.Time
	Usage                                                 *AdminSandboxUsageSnapshot
	NetworkUsage                                          *AdminSandboxNetworkUsageSnapshot
	WorkspaceVolumeUsage                                  *AdminWorkspaceVolumeUsageSnapshot
	UsageCorrections                                      []AdminSandboxUsageCorrectionSnapshot
}

type AdminSandboxUsageSnapshot struct {
	LatestRuntimeGeneration                                              int64
	AllocatedMilliseconds, CPUMillisMilliseconds, MemoryByteMilliseconds string
	CheckpointedAt                                                       time.Time
	FinalizedAt                                                          *time.Time
}

type AdminWorkspaceVolumeUsageSnapshot struct {
	MeasurementGeneration int64
	State                 string
	UsedBytes             *string
	CheckpointedAt        *time.Time
	ObservedAt            *time.Time
	StableErrorCode       *string
}

type AdminSandboxNetworkUsageSnapshot struct {
	LatestRuntimeGeneration, MeasurementGeneration int64
	State                                          string
	ReceivedBytes, TransmittedBytes                *string
	CheckpointedAt, ObservedAt                     *time.Time
	StableErrorCode                                *string
}

type AdminSandboxUsageCorrectionSnapshot struct {
	CorrectionID, Metric, Adjustment, ReasonCode string
	RequestedBy, RequestID                       string
	SandboxGeneration, PriorResourceVersion      int64
	CreatedAt                                    time.Time
}

type SandboxUsageCorrectionInput struct {
	Scope                                             internalcoordination.FoundationScope
	SandboxID, ConfirmedSandboxID, Metric, Adjustment string
	ReasonCode, RequestDigest                         string
	ExpectedGeneration, ExpectedResourceVersion       int64
	Mutation                                          internalcoordination.FoundationMutation
}

type AdminSandboxPage struct {
	Sandboxes     []AdminSandboxSnapshot
	NextSandboxID string
}

type FoundationSandboxAccess struct {
	Scope                                        internalcoordination.FoundationScope
	WorkspaceID, SandboxID, TargetID, TargetKind string
	RuntimeID, RuntimeOperationID                string
	RuntimeSpecDigest, CredentialRef             string
	Generation, RuntimeGeneration                int64
}

type FoundationSandboxExec struct {
	Access                                           FoundationSandboxAccess
	CommandID, SubjectDigest, State, StableErrorCode string
	Deadline                                         time.Time
	ExitCode, ExecutionTimeMillis                    int64
	Stdout, Stderr                                   string
}

type adminSandboxPageRow struct {
	TenantID                string          `json:"tenant_id"`
	ProjectID               string          `json:"project_uid"`
	SandboxID               string          `json:"sandbox_uid"`
	OperationID             string          `json:"operation_id"`
	OperationState          string          `json:"operation_state"`
	CleanupPhase            string          `json:"cleanup_phase"`
	WorkspaceID             string          `json:"workspace_uid"`
	WorkspaceName           string          `json:"workspace_name"`
	VolumeID                string          `json:"volume_uid"`
	PhysicalVolumeID        *string         `json:"physical_volume_uid"`
	WorkspaceRetention      string          `json:"retention"`
	WorkspaceObservedState  string          `json:"workspace_observed_state"`
	RuntimeProfileID        string          `json:"runtime_profile_uid"`
	RuntimeProfileVersion   int64           `json:"runtime_profile_version"`
	WorkloadTrust           string          `json:"workload_trust"`
	IsolationRuntime        string          `json:"isolation_runtime"`
	NetworkPolicyID         string          `json:"network_policy_ref"`
	TargetID                string          `json:"target_uid"`
	Generation              int64           `json:"generation"`
	ObservedGeneration      int64           `json:"observed_generation"`
	DesiredState            string          `json:"desired_state"`
	ObservedState           string          `json:"observed_state"`
	WriterReleased          bool            `json:"writer_released"`
	TTLSeconds              *int32          `json:"ttl_seconds"`
	ExpiresAt               *time.Time      `json:"expires_at"`
	LifecycleTrigger        *string         `json:"lifecycle_trigger"`
	RuntimeID               *string         `json:"runtime_uid"`
	RuntimeState            string          `json:"runtime_state"`
	StableErrorCode         *string         `json:"stable_error_code"`
	ResourceVersion         int64           `json:"resource_version"`
	CreatedAt               time.Time       `json:"created_at"`
	UpdatedAt               time.Time       `json:"updated_at"`
	ObservedAt              *time.Time      `json:"observed_at"`
	UsageLatestGeneration   *int64          `json:"usage_latest_runtime_generation"`
	UsageAllocatedMillis    *string         `json:"usage_allocated_milliseconds"`
	UsageCPUMillisMillis    *string         `json:"usage_cpu_millis_milliseconds"`
	UsageMemoryByteMillis   *string         `json:"usage_memory_byte_milliseconds"`
	UsageCheckpointedAt     *time.Time      `json:"usage_checkpointed_at"`
	UsageFinalizedAt        *time.Time      `json:"usage_finalized_at"`
	VolumeUsageGeneration   *int64          `json:"volume_usage_generation"`
	VolumeUsageState        *string         `json:"volume_usage_state"`
	VolumeUsageUsedBytes    *string         `json:"volume_usage_used_bytes"`
	VolumeUsageCheckpoint   *time.Time      `json:"volume_usage_checkpointed_at"`
	VolumeUsageObservedAt   *time.Time      `json:"volume_usage_observed_at"`
	VolumeUsageStableError  *string         `json:"volume_usage_stable_error_code"`
	NetworkLatestGeneration *int64          `json:"network_latest_runtime_generation"`
	NetworkMeasurement      *int64          `json:"network_measurement_generation"`
	NetworkState            *string         `json:"network_state"`
	NetworkReceivedBytes    *string         `json:"network_received_bytes"`
	NetworkTransmittedBytes *string         `json:"network_transmitted_bytes"`
	NetworkCheckpointedAt   *time.Time      `json:"network_checkpointed_at"`
	NetworkObservedAt       *time.Time      `json:"network_observed_at"`
	NetworkStableError      *string         `json:"network_stable_error_code"`
	UsageCorrections        json.RawMessage `json:"usage_corrections"`
}

const adminSandboxColumns = `sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid,
    sandbox.operation_id, operation.state AS operation_state, operation.cleanup_phase,
    sandbox.workspace_uid, workspace.workspace_name, volume.volume_uid, volume.physical_volume_uid,
    volume.retention, volume.observed_state AS workspace_observed_state,
    sandbox.runtime_profile_uid, sandbox.runtime_profile_version,
    COALESCE(profile.workload_trust, 'trusted-single-tenant') AS workload_trust,
    COALESCE(profile.isolation_runtime, 'runc') AS isolation_runtime, volume.target_uid,
	COALESCE(profile.network_policy_ref, '') AS network_policy_ref,
    sandbox.generation, sandbox.observed_generation, sandbox.desired_state, sandbox.observed_state,
    sandbox.writer_released, sandbox.ttl_seconds, sandbox.expires_at, activity.lifecycle_trigger,
    sandbox.runtime_uid, sandbox.runtime_state, sandbox.stable_error_code,
    sandbox.resource_version, operation.created_at, operation.updated_at, sandbox.observed_at,
    usage.latest_runtime_generation AS usage_latest_runtime_generation,
    usage.allocated_milliseconds AS usage_allocated_milliseconds,
    usage.cpu_millis_milliseconds AS usage_cpu_millis_milliseconds,
    usage.memory_byte_milliseconds AS usage_memory_byte_milliseconds,
    usage.checkpointed_at AS usage_checkpointed_at, usage.finalized_at AS usage_finalized_at,
    volume_usage.measurement_generation AS volume_usage_generation,
    volume_usage.state AS volume_usage_state, volume_usage.used_bytes::text AS volume_usage_used_bytes,
    volume_usage.checkpointed_at AS volume_usage_checkpointed_at,
    volume_usage.observed_at AS volume_usage_observed_at,
    volume_usage.stable_error_code AS volume_usage_stable_error_code,
    network_usage.latest_runtime_generation AS network_latest_runtime_generation,
    network_usage.measurement_generation AS network_measurement_generation,
    network_usage.state AS network_state,
    network_usage.received_bytes AS network_received_bytes,
    network_usage.transmitted_bytes AS network_transmitted_bytes,
    network_usage.checkpointed_at AS network_checkpointed_at,
    network_usage.observed_at AS network_observed_at,
    network_usage.stable_error_code AS network_stable_error_code,
    corrections.records AS usage_corrections`

const adminSandboxUsageJoin = `LEFT JOIN LATERAL (
    SELECT pg_catalog.max(checkpoint.sandbox_generation) AS latest_runtime_generation,
        pg_catalog.sum(checkpoint.allocated_milliseconds)::text AS allocated_milliseconds,
        pg_catalog.sum(checkpoint.cpu_millis_milliseconds)::text AS cpu_millis_milliseconds,
        pg_catalog.sum(checkpoint.memory_byte_milliseconds)::text AS memory_byte_milliseconds,
        pg_catalog.max(checkpoint.checkpointed_at) AS checkpointed_at,
        CASE WHEN pg_catalog.bool_and(checkpoint.finalized_at IS NOT NULL)
            THEN pg_catalog.max(checkpoint.finalized_at) END AS finalized_at
    FROM cloud_agents.sandbox_usage_checkpoints AS checkpoint
    WHERE checkpoint.tenant_id = sandbox.tenant_id AND checkpoint.project_uid = sandbox.project_uid
      AND checkpoint.sandbox_uid = sandbox.sandbox_uid
    HAVING pg_catalog.count(*) > 0
) AS usage ON true`

const adminSandboxNetworkUsageJoin = `LEFT JOIN LATERAL (
    SELECT checkpoint.sandbox_generation AS latest_runtime_generation,
        checkpoint.network_measurement_generation AS measurement_generation,
        checkpoint.network_state AS state,
        checkpoint.network_received_bytes::text AS received_bytes,
        checkpoint.network_transmitted_bytes::text AS transmitted_bytes,
        checkpoint.network_checkpointed_at AS checkpointed_at,
        checkpoint.network_observed_at AS observed_at,
        checkpoint.network_stable_error_code AS stable_error_code
    FROM cloud_agents.sandbox_usage_checkpoints AS checkpoint
    WHERE checkpoint.tenant_id = sandbox.tenant_id AND checkpoint.project_uid = sandbox.project_uid
      AND checkpoint.sandbox_uid = sandbox.sandbox_uid AND checkpoint.network_measurement_generation > 0
    ORDER BY checkpoint.sandbox_generation DESC, checkpoint.network_observed_at DESC
    LIMIT 1
) AS network_usage ON true`

const adminSandboxUsageCorrectionsJoin = `LEFT JOIN LATERAL (
    SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
        'correctionId', correction.correction_uid,
        'metric', correction.metric,
        'adjustment', correction.adjustment::text,
        'reasonCode', correction.reason_code,
        'sandboxGeneration', correction.sandbox_generation,
        'priorResourceVersion', correction.prior_resource_version,
        'requestedBy', correction.subject_digest,
        'requestId', correction.request_id,
        'createdAt', correction.created_at
    ) ORDER BY correction.created_at DESC, correction.correction_uid DESC), '[]'::jsonb) AS records
    FROM cloud_agents.sandbox_usage_corrections AS correction
    WHERE correction.tenant_id = sandbox.tenant_id AND correction.project_uid = sandbox.project_uid
      AND correction.sandbox_uid = sandbox.sandbox_uid
) AS corrections ON true`

var (
	getAdminSandboxSQL = `SELECT ` + adminSandboxColumns + `
FROM cloud_agents.sandbox_sessions AS sandbox
JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
LEFT JOIN cloud_agents.workspace_volume_usage_checkpoints AS volume_usage
  ON volume_usage.tenant_id = volume.tenant_id AND volume_usage.project_uid = volume.project_uid
 AND volume_usage.volume_uid = volume.volume_uid
JOIN cloud_agents.platform_operations AS operation
  ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
 AND operation.operation_generation = sandbox.operation_generation
LEFT JOIN cloud_agents.runtime_profiles AS profile
  ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
 AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
  ON activity.tenant_id = sandbox.tenant_id AND activity.operation_uid = sandbox.operation_id
 AND activity.sandbox_generation = sandbox.operation_generation
` + adminSandboxUsageJoin + `
` + adminSandboxNetworkUsageJoin + `
` + adminSandboxUsageCorrectionsJoin + `
WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = $1
  AND sandbox.sandbox_uid = $2`
	getFoundationSandboxAccessSQL = `SELECT sandbox.tenant_id, sandbox.project_uid,
    sandbox.workspace_uid, sandbox.sandbox_uid, sandbox.generation, sandbox.observed_generation,
    sandbox.desired_state, sandbox.observed_state, sandbox.writer_released,
    sandbox.runtime_uid, sandbox.runtime_state, sandbox.runtime_operation_uid,
    sandbox.runtime_generation, sandbox.runtime_spec_digest, sandbox.spec_digest,
    volume.observed_state, target.target_uid, target.target_kind, target.credential_ref,
    operation.state, operation.cleanup_phase,
    sandbox.expires_at IS NULL OR sandbox.expires_at > pg_catalog.clock_timestamp()
FROM cloud_agents.sandbox_sessions AS sandbox
JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
JOIN cloud_agents.deployment_targets AS target
  ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
 AND target.target_uid = volume.target_uid
JOIN cloud_agents.platform_operations AS operation
  ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
 AND operation.operation_generation = sandbox.operation_generation
WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = $1
  AND sandbox.sandbox_uid = $2`
	requestRemoteWorkerSandboxExecSQL = `SELECT command_uid, command_state, command_deadline_at,
    exit_code, stdout, stderr, execution_time_millis, stable_error_code
FROM cloud_agents.request_remote_worker_sandbox_exec_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`
	getRemoteWorkerSandboxExecSQL = `SELECT command_state, command_deadline_at,
    exit_code, stdout, stderr, execution_time_millis, stable_error_code
FROM cloud_agents.get_remote_worker_sandbox_exec_v1($1,$2,$3,$4)`
	adminSandboxPageCursorSQL = `SELECT 1 FROM cloud_agents.sandbox_sessions
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND sandbox_uid = $2`
	listAdminSandboxesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(sandbox_row)
    ORDER BY sandbox_row.sandbox_uid), '[]'::jsonb)
FROM (
    SELECT ` + adminSandboxColumns + `
    FROM cloud_agents.sandbox_sessions AS sandbox
    JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
    JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
    LEFT JOIN cloud_agents.workspace_volume_usage_checkpoints AS volume_usage
      ON volume_usage.tenant_id = volume.tenant_id AND volume_usage.project_uid = volume.project_uid
     AND volume_usage.volume_uid = volume.volume_uid
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
     AND operation.operation_generation = sandbox.operation_generation
    LEFT JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = sandbox.tenant_id AND activity.operation_uid = sandbox.operation_id
     AND activity.sandbox_generation = sandbox.operation_generation
    ` + adminSandboxUsageJoin + `
    ` + adminSandboxNetworkUsageJoin + `
    ` + adminSandboxUsageCorrectionsJoin + `
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = $1
      AND sandbox.sandbox_uid > $2
    ORDER BY sandbox.sandbox_uid
    LIMIT $3
) AS sandbox_row`
	transitionFoundationSandboxSQL = `SELECT operation_uid, idempotency_key, action, sandbox_uid,
    sandbox_generation, requested_by, request_id, requested_at, updated_at, operation_state,
    cleanup_phase, stable_error_code, compute_disposition, workspace_disposition
FROM cloud_agents.transition_foundation_sandbox_v4($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	createSandboxUsageCorrectionSQL = `SELECT cloud_agents.create_foundation_sandbox_usage_correction_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
)

func (service *DurableCoordinationService) TransitionFoundationSandbox(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	input internalcoordination.FoundationSandboxLifecycleInput,
) (internalcoordination.FoundationSandboxLifecycleOperation, error) {
	if service == nil || service.runner == nil {
		return internalcoordination.FoundationSandboxLifecycleOperation{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalcoordination.FoundationSandboxLifecycleOperation{}, ErrCoordinationInvalidInput
	}
	digest, err := internalcoordination.FoundationSandboxLifecycleDigest(input)
	if err != nil {
		return internalcoordination.FoundationSandboxLifecycleOperation{}, ErrCoordinationInvalidInput
	}
	var result internalcoordination.FoundationSandboxLifecycleOperation
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			var stableErrorCode *string
			err := handle.transaction.queryRow(operationContext, transitionFoundationSandboxSQL,
				input.Scope.ProjectID, input.SandboxID, input.ExpectedGeneration,
				input.ExpectedResourceVersion, input.Action, input.ConfirmedSandboxID,
				input.ComputeDisposition, input.WorkspaceDisposition, subjectDigest,
				input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID,
			).Scan(&result.OperationID, &result.IdempotencyKey, &result.Action, &result.SandboxID,
				&result.SandboxGeneration, &result.RequestedBy, &result.RequestID, &result.RequestedAt,
				&result.UpdatedAt, &result.State, &result.CleanupPhase, &stableErrorCode,
				&result.ComputeDisposition, &result.WorkspaceDisposition)
			if err != nil {
				return err
			}
			result.Scope = input.Scope
			if stableErrorCode != nil {
				result.StableErrorCode = *stableErrorCode
			}
			if result.Validate() != nil {
				return ErrCoordinationResultDrift
			}
			return nil
		})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) GetAdminSandbox(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, sandboxID string,
) (AdminSandboxSnapshot, error) {
	if service == nil || service.runner == nil {
		return AdminSandboxSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(sandboxID) {
		return AdminSandboxSnapshot{}, ErrCoordinationInvalidInput
	}
	var row adminSandboxPageRow
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
		err := scanAdminSandboxRow(handle.transaction.queryRow(operationContext, getAdminSandboxSQL, projectID, sandboxID), &row)
		if errors.Is(err, pgx.ErrNoRows) {
			return internalcoordination.ErrFoundationSandboxNotFound
		}
		return err
	})
	if err != nil {
		return AdminSandboxSnapshot{}, mapRuntimeProfileError(err)
	}
	return adminSandboxSnapshot(row, tenantID, projectID)
}

func (service *DurableCoordinationService) CreateSandboxUsageCorrection(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input SandboxUsageCorrectionInput,
) (AdminSandboxSnapshot, error) {
	if service == nil || service.runner == nil {
		return AdminSandboxSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validSandboxUsageCorrectionInput(input, tenantID) {
		return AdminSandboxSnapshot{}, ErrCoordinationInvalidInput
	}
	var row adminSandboxPageRow
	err := service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			correctionID := "correction-" + rand.Text()
			var storedCorrectionID string
			if err := handle.transaction.queryRow(operationContext, createSandboxUsageCorrectionSQL,
				input.Scope.ProjectID, input.SandboxID, correctionID, input.ExpectedGeneration,
				input.ExpectedResourceVersion, input.ConfirmedSandboxID, input.Metric, input.Adjustment,
				input.ReasonCode, subjectDigest, input.Mutation.IdempotencyKey, input.RequestDigest,
				input.Mutation.RequestID).Scan(&storedCorrectionID); err != nil {
				return err
			}
			if !validMutationIdentifier(storedCorrectionID) {
				return ErrCoordinationResultDrift
			}
			return scanAdminSandboxRow(handle.transaction.queryRow(operationContext, getAdminSandboxSQL,
				input.Scope.ProjectID, input.SandboxID), &row)
		})
	if err != nil {
		return AdminSandboxSnapshot{}, mapRuntimeProfileError(err)
	}
	return adminSandboxSnapshot(row, tenantID, input.Scope.ProjectID)
}

func validSandboxUsageCorrectionInput(input SandboxUsageCorrectionInput, tenantID string) bool {
	return input.Scope.TenantID == tenantID && validMutationIdentifier(tenantID) &&
		validMutationIdentifier(input.Scope.ProjectID) && validMutationIdentifier(input.SandboxID) &&
		input.ConfirmedSandboxID == input.SandboxID && validSandboxUsageCorrectionMetric(input.Metric) && validSignedDecimal(input.Adjustment, 40) &&
		validMutationIdentifier(input.ReasonCode) && input.ExpectedGeneration > 0 &&
		input.ExpectedGeneration <= 9007199254740991 && input.ExpectedResourceVersion > 0 &&
		validCoordinationDigest(input.RequestDigest) && validMutationIdentifier(input.Mutation.RequestID) &&
		validIdempotencyKey(input.Mutation.IdempotencyKey)
}

func validSignedDecimal(value string, maximumDigits int) bool {
	if value == "" || maximumDigits < 1 {
		return false
	}
	digits := value
	if digits[0] == '-' {
		digits = digits[1:]
	}
	if len(digits) < 1 || len(digits) > maximumDigits || digits[0] == '0' {
		return false
	}
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func (service *DurableCoordinationService) PrepareFoundationSandboxExec(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	projectID, sandboxID string, expectedGeneration int64, requestID, requestDigest, command string, timeoutSeconds int64,
) (FoundationSandboxExec, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxExec{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(sandboxID) || expectedGeneration < 1 || expectedGeneration > 9007199254740991 ||
		!validMutationIdentifier(requestID) || !validCoordinationDigest(requestDigest) || len(command) < 1 ||
		len(command) > 8192 || !utf8.ValidString(command) || strings.ContainsRune(command, 0) ||
		timeoutSeconds < 1 || timeoutSeconds > 60 {
		return FoundationSandboxExec{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxExec{CommandID: "rwexec-" + rand.Text()}
	var tenant, project, desiredState, observedState, runtimeState, volumeState, targetKind string
	var operationState, cleanupPhase, specDigest string
	var observedGeneration int64
	var writerReleased, notExpired bool
	var runtimeID, runtimeOperationID, runtimeSpecDigest *string
	var runtimeGeneration *int64
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			err := handle.transaction.queryRow(operationContext, getFoundationSandboxAccessSQL, projectID, sandboxID).Scan(
				&tenant, &project, &result.Access.WorkspaceID, &result.Access.SandboxID, &result.Access.Generation,
				&observedGeneration, &desiredState, &observedState, &writerReleased, &runtimeID,
				&runtimeState, &runtimeOperationID, &runtimeGeneration, &runtimeSpecDigest, &specDigest,
				&volumeState, &result.Access.TargetID, &targetKind, &result.Access.CredentialRef,
				&operationState, &cleanupPhase, &notExpired)
			if errors.Is(err, pgx.ErrNoRows) {
				return internalcoordination.ErrFoundationSandboxNotFound
			}
			if err != nil {
				return err
			}
			if result.Access.Generation != expectedGeneration || desiredState != "running" || observedState != "running" ||
				observedGeneration != result.Access.Generation || writerReleased || runtimeState != "Running" || volumeState != "available" ||
				operationState != "succeeded" || cleanupPhase != "complete" || !notExpired {
				return internalcoordination.ErrFoundationSandboxConflict
			}
			if tenant != tenantID || project != projectID || result.Access.SandboxID != sandboxID ||
				(targetKind != "docker" && targetKind != "kubernetes" && targetKind != "remote-worker") || runtimeID == nil || runtimeOperationID == nil ||
				runtimeGeneration == nil || runtimeSpecDigest == nil || *runtimeGeneration != result.Access.Generation ||
				*runtimeSpecDigest != specDigest || !validMutationIdentifier(result.Access.WorkspaceID) ||
				!validMutationIdentifier(result.Access.SandboxID) || !validMutationIdentifier(result.Access.TargetID) ||
				!validMutationIdentifier(*runtimeID) || !validMutationIdentifier(*runtimeOperationID) ||
				!validMutationIdentifier(result.Access.CredentialRef) || !validCoordinationDigest(*runtimeSpecDigest) {
				return ErrCoordinationResultDrift
			}
			result.Access.Scope = internalcoordination.FoundationScope{TenantID: tenant, ProjectID: project}
			result.Access.TargetKind, result.Access.RuntimeID, result.Access.RuntimeOperationID = targetKind, *runtimeID, *runtimeOperationID
			result.Access.RuntimeGeneration, result.Access.RuntimeSpecDigest = *runtimeGeneration, *runtimeSpecDigest
			if targetKind == "docker" || targetKind == "kubernetes" {
				result.CommandID = ""
				return nil
			}
			result.SubjectDigest = subjectDigest
			return scanFoundationSandboxExec(handle.transaction.queryRow(operationContext, requestRemoteWorkerSandboxExecSQL,
				tenantID, projectID, result.CommandID, result.Access.TargetID, result.Access.WorkspaceID,
				result.Access.SandboxID, result.Access.Generation, result.Access.RuntimeID,
				result.Access.RuntimeOperationID, result.Access.RuntimeSpecDigest, command, timeoutSeconds,
				requestID, requestDigest, subjectDigest), &result, true)
		})
	if err != nil {
		return FoundationSandboxExec{}, mapFoundationSandboxExecError(err)
	}
	return result, nil
}

func (service *DurableCoordinationService) WaitRemoteWorkerSandboxExec(ctx context.Context, result FoundationSandboxExec) (FoundationSandboxExec, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxExec{}, ErrNilCoordinationRunner
	}
	if ctx == nil || result.Access.TargetKind != "remote-worker" || !validMutationIdentifier(result.CommandID) ||
		!validCoordinationDigest(result.SubjectDigest) || result.Deadline.IsZero() {
		return FoundationSandboxExec{}, ErrCoordinationInvalidInput
	}
	waitContext, cancel := context.WithDeadline(ctx, result.Deadline.Add(2*time.Second))
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for result.State == "pending" || result.State == "delivered" {
		select {
		case <-waitContext.Done():
			return FoundationSandboxExec{}, context.DeadlineExceeded
		case <-ticker.C:
		}
		err := service.runner.WithTenantRead(waitContext, result.Access.Scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return scanFoundationSandboxExec(handle.transaction.queryRow(readContext, getRemoteWorkerSandboxExecSQL,
				result.Access.Scope.TenantID, result.Access.Scope.ProjectID, result.CommandID, result.SubjectDigest), &result, false)
		})
		if err != nil {
			return FoundationSandboxExec{}, mapFoundationSandboxExecError(err)
		}
	}
	return result, nil
}

func scanFoundationSandboxExec(row rowScanner, result *FoundationSandboxExec, includeCommandID bool) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var exitCode, executionTime *int64
	var stdout, stderr, stableErrorCode *string
	targets := []any{&result.State, &result.Deadline, &exitCode, &stdout, &stderr, &executionTime, &stableErrorCode}
	if includeCommandID {
		targets = append([]any{&result.CommandID}, targets...)
	}
	if err := row.Scan(targets...); err != nil {
		return err
	}
	result.ExitCode, result.ExecutionTimeMillis, result.Stdout, result.Stderr, result.StableErrorCode = 0, 0, "", "", ""
	if result.State == "succeeded" {
		if exitCode == nil || stdout == nil || stderr == nil || executionTime == nil || stableErrorCode != nil {
			return ErrCoordinationResultDrift
		}
		result.ExitCode, result.ExecutionTimeMillis, result.Stdout, result.Stderr = *exitCode, *executionTime, *stdout, *stderr
	} else if result.State == "failed" {
		if exitCode != nil || stdout != nil || stderr != nil || executionTime != nil || stableErrorCode == nil {
			return ErrCoordinationResultDrift
		}
		result.StableErrorCode = *stableErrorCode
	} else if result.State != "pending" && result.State != "delivered" ||
		exitCode != nil || stdout != nil || stderr != nil || executionTime != nil || stableErrorCode != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapFoundationSandboxExecError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker sandbox exec request conflict", "remote worker sandbox exec unavailable":
			return internalcoordination.ErrFoundationSandboxConflict
		case "remote worker sandbox exec was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		case "remote worker sandbox exec request is invalid", "remote worker sandbox exec lookup is invalid":
			return ErrCoordinationInvalidInput
		}
	}
	return mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) ListAdminSandboxes(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterSandboxID string, limit int,
) (AdminSandboxPage, error) {
	if service == nil || service.runner == nil {
		return AdminSandboxPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationPageInput(tenantID, projectID, afterSandboxID, limit) {
		return AdminSandboxPage{}, ErrCoordinationInvalidInput
	}
	var result AdminSandboxPage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
		if err := validateRuntimeProfileCursor(operationContext, handle, adminSandboxPageCursorSQL, projectID, afterSandboxID); err != nil {
			return err
		}
		var raw []byte
		if err := handle.transaction.queryRow(operationContext, listAdminSandboxesSQL, projectID, afterSandboxID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var rows []adminSandboxPageRow
		if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
			return ErrCoordinationResultDrift
		}
		sandboxes := make([]AdminSandboxSnapshot, 0, len(rows))
		for _, row := range rows {
			snapshot, err := adminSandboxSnapshot(row, tenantID, projectID)
			if err != nil {
				return err
			}
			sandboxes = append(sandboxes, snapshot)
		}
		result.Sandboxes = sandboxes
		if len(sandboxes) > limit {
			result.Sandboxes = sandboxes[:limit]
			result.NextSandboxID = result.Sandboxes[len(result.Sandboxes)-1].SandboxID
		}
		return nil
	})
	return result, mapRuntimeProfileError(err)
}

func scanAdminSandboxRow(row rowScanner, value *adminSandboxPageRow) error {
	if row == nil || value == nil {
		return ErrCoordinationResultDrift
	}
	return row.Scan(&value.TenantID, &value.ProjectID, &value.SandboxID, &value.OperationID,
		&value.OperationState, &value.CleanupPhase, &value.WorkspaceID, &value.WorkspaceName,
		&value.VolumeID, &value.PhysicalVolumeID, &value.WorkspaceRetention, &value.WorkspaceObservedState,
		&value.RuntimeProfileID, &value.RuntimeProfileVersion, &value.WorkloadTrust, &value.IsolationRuntime,
		&value.TargetID, &value.NetworkPolicyID,
		&value.Generation,
		&value.ObservedGeneration, &value.DesiredState, &value.ObservedState, &value.WriterReleased,
		&value.TTLSeconds, &value.ExpiresAt, &value.LifecycleTrigger, &value.RuntimeID,
		&value.RuntimeState, &value.StableErrorCode, &value.ResourceVersion,
		&value.CreatedAt, &value.UpdatedAt, &value.ObservedAt,
		&value.UsageLatestGeneration, &value.UsageAllocatedMillis, &value.UsageCPUMillisMillis,
		&value.UsageMemoryByteMillis, &value.UsageCheckpointedAt, &value.UsageFinalizedAt,
		&value.VolumeUsageGeneration, &value.VolumeUsageState, &value.VolumeUsageUsedBytes,
		&value.VolumeUsageCheckpoint, &value.VolumeUsageObservedAt, &value.VolumeUsageStableError,
		&value.NetworkLatestGeneration, &value.NetworkMeasurement, &value.NetworkState,
		&value.NetworkReceivedBytes, &value.NetworkTransmittedBytes, &value.NetworkCheckpointedAt,
		&value.NetworkObservedAt, &value.NetworkStableError, &value.UsageCorrections)
}

func adminSandboxSnapshot(row adminSandboxPageRow, tenantID, projectID string) (AdminSandboxSnapshot, error) {
	value := AdminSandboxSnapshot{
		Scope:     internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
		SandboxID: row.SandboxID, OperationID: row.OperationID, OperationState: row.OperationState,
		CleanupPhase: row.CleanupPhase, WorkspaceID: row.WorkspaceID, WorkspaceName: row.WorkspaceName,
		VolumeID: row.VolumeID, PhysicalVolumeID: row.PhysicalVolumeID, WorkspaceObservedState: row.WorkspaceObservedState,
		RuntimeProfileID: row.RuntimeProfileID, RuntimeProfileVersion: row.RuntimeProfileVersion,
		WorkloadTrust: row.WorkloadTrust, IsolationRuntime: row.IsolationRuntime,
		NetworkPolicyID: row.NetworkPolicyID,
		TargetID:        row.TargetID, Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		DesiredState: row.DesiredState, ObservedState: row.ObservedState, WriterReleased: row.WriterReleased,
		TTLSeconds: row.TTLSeconds, ExpiresAt: row.ExpiresAt, LifecycleTrigger: row.LifecycleTrigger,
		RuntimeID: row.RuntimeID, RuntimeState: row.RuntimeState, StableErrorCode: row.StableErrorCode,
		ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		ObservedAt: row.ObservedAt,
	}
	if row.UsageCorrections == nil || json.Unmarshal(row.UsageCorrections, &value.UsageCorrections) != nil ||
		value.UsageCorrections == nil || len(value.UsageCorrections) > 100 {
		return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
	}
	if len(value.UsageCorrections) > 0 && value.UpdatedAt.Before(value.UsageCorrections[0].CreatedAt) {
		value.UpdatedAt = value.UsageCorrections[0].CreatedAt
	}
	if row.UsageLatestGeneration != nil {
		if row.UsageAllocatedMillis == nil || row.UsageCPUMillisMillis == nil || row.UsageMemoryByteMillis == nil || row.UsageCheckpointedAt == nil {
			return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
		}
		value.Usage = &AdminSandboxUsageSnapshot{
			LatestRuntimeGeneration: *row.UsageLatestGeneration,
			AllocatedMilliseconds:   *row.UsageAllocatedMillis,
			CPUMillisMilliseconds:   *row.UsageCPUMillisMillis,
			MemoryByteMilliseconds:  *row.UsageMemoryByteMillis,
			CheckpointedAt:          *row.UsageCheckpointedAt,
			FinalizedAt:             row.UsageFinalizedAt,
		}
	} else if row.UsageAllocatedMillis != nil || row.UsageCPUMillisMillis != nil || row.UsageMemoryByteMillis != nil || row.UsageCheckpointedAt != nil || row.UsageFinalizedAt != nil {
		return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
	}
	if row.VolumeUsageGeneration != nil {
		if row.VolumeUsageState == nil {
			return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
		}
		value.WorkspaceVolumeUsage = &AdminWorkspaceVolumeUsageSnapshot{
			MeasurementGeneration: *row.VolumeUsageGeneration,
			State:                 *row.VolumeUsageState,
			UsedBytes:             row.VolumeUsageUsedBytes,
			CheckpointedAt:        row.VolumeUsageCheckpoint,
			ObservedAt:            row.VolumeUsageObservedAt,
			StableErrorCode:       row.VolumeUsageStableError,
		}
	} else if row.VolumeUsageState != nil || row.VolumeUsageUsedBytes != nil || row.VolumeUsageCheckpoint != nil || row.VolumeUsageObservedAt != nil || row.VolumeUsageStableError != nil {
		return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
	}
	if row.NetworkLatestGeneration != nil {
		if row.NetworkMeasurement == nil || row.NetworkState == nil {
			return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
		}
		value.NetworkUsage = &AdminSandboxNetworkUsageSnapshot{
			LatestRuntimeGeneration: *row.NetworkLatestGeneration,
			MeasurementGeneration:   *row.NetworkMeasurement,
			State:                   *row.NetworkState,
			ReceivedBytes:           row.NetworkReceivedBytes,
			TransmittedBytes:        row.NetworkTransmittedBytes,
			CheckpointedAt:          row.NetworkCheckpointedAt,
			ObservedAt:              row.NetworkObservedAt,
			StableErrorCode:         row.NetworkStableError,
		}
	} else if row.NetworkMeasurement != nil || row.NetworkState != nil || row.NetworkReceivedBytes != nil ||
		row.NetworkTransmittedBytes != nil || row.NetworkCheckpointedAt != nil || row.NetworkObservedAt != nil || row.NetworkStableError != nil {
		return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
	}
	value.NetworkPolicyEnforcement = networkPolicyEnforcement(value)
	if row.TenantID != tenantID || row.ProjectID != projectID || row.WorkspaceRetention != "retain" || !validAdminSandboxSnapshot(value) {
		return AdminSandboxSnapshot{}, ErrCoordinationResultDrift
	}
	return value, nil
}

func networkPolicyEnforcement(value AdminSandboxSnapshot) string {
	if value.NetworkPolicyID == "" {
		return "legacy"
	}
	if value.DesiredState == "stopped" && value.ObservedState == "stopped" {
		return "stopped"
	}
	if value.OperationState == "failed" || value.ObservedState == "failed" {
		return "failed"
	}
	if value.OperationState == "succeeded" && value.ObservedState == "running" &&
		value.ObservedGeneration == value.Generation && value.RuntimeState == "Running" {
		return "enforced"
	}
	return "pending"
}

func validAdminSandboxSnapshot(value AdminSandboxSnapshot) bool {
	for _, id := range []string{value.Scope.TenantID, value.Scope.ProjectID, value.SandboxID, value.OperationID,
		value.WorkspaceID, value.WorkspaceName, value.VolumeID, value.RuntimeProfileID, value.TargetID} {
		if !validMutationIdentifier(id) {
			return false
		}
	}
	for _, id := range []*string{value.PhysicalVolumeID, value.RuntimeID, value.StableErrorCode} {
		if id != nil && !validMutationIdentifier(*id) {
			return false
		}
	}
	if value.NetworkPolicyID != "" && !validMutationIdentifier(value.NetworkPolicyID) {
		return false
	}
	if (value.WorkloadTrust != "trusted-single-tenant" && value.WorkloadTrust != "dedicated-node" && value.WorkloadTrust != "shared-untrusted") ||
		(value.IsolationRuntime != "runc" && value.IsolationRuntime != "gvisor") ||
		(value.WorkloadTrust == "shared-untrusted") != (value.IsolationRuntime == "gvisor") {
		return false
	}
	switch value.NetworkPolicyEnforcement {
	case "legacy", "pending", "enforced", "failed", "stopped":
	default:
		return false
	}
	if (value.TTLSeconds == nil) != (value.ExpiresAt == nil) || value.TTLSeconds != nil &&
		(*value.TTLSeconds < 60 || *value.TTLSeconds > 86400 || value.ExpiresAt.IsZero()) {
		return false
	}
	if value.LifecycleTrigger != nil && *value.LifecycleTrigger != "manual" && *value.LifecycleTrigger != "ttl" {
		return false
	}
	if value.RuntimeProfileVersion < 1 || value.RuntimeProfileVersion > 2147483647 || value.Generation < 1 ||
		value.ObservedGeneration < 0 || value.ObservedGeneration > value.Generation || value.ResourceVersion < 0 ||
		value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) || value.WriterReleased && value.ObservedState != "stopped" {
		return false
	}
	if value.Usage != nil && (value.Usage.LatestRuntimeGeneration < 1 || value.Usage.LatestRuntimeGeneration > value.Generation ||
		value.Usage.AllocatedMilliseconds == "" || value.Usage.CPUMillisMilliseconds == "" || value.Usage.MemoryByteMilliseconds == "" ||
		value.Usage.CheckpointedAt.IsZero() || value.Usage.FinalizedAt != nil && value.Usage.FinalizedAt.IsZero()) {
		return false
	}
	if usage := value.WorkspaceVolumeUsage; usage != nil {
		if usage.MeasurementGeneration < 0 || (usage.UsedBytes == nil) != (usage.CheckpointedAt == nil) ||
			usage.CheckpointedAt != nil && usage.CheckpointedAt.IsZero() || usage.ObservedAt != nil && usage.ObservedAt.IsZero() {
			return false
		}
		switch usage.State {
		case "pending":
			if usage.MeasurementGeneration != 0 || usage.UsedBytes != nil || usage.ObservedAt != nil || usage.StableErrorCode != nil {
				return false
			}
		case "measuring", "ready":
			if usage.MeasurementGeneration < 1 || usage.ObservedAt == nil || usage.StableErrorCode != nil || usage.State == "ready" && usage.UsedBytes == nil {
				return false
			}
		case "failed":
			if usage.MeasurementGeneration < 1 || usage.ObservedAt == nil || usage.StableErrorCode == nil || !validMutationIdentifier(*usage.StableErrorCode) {
				return false
			}
		default:
			return false
		}
	}
	if usage := value.NetworkUsage; usage != nil {
		if usage.LatestRuntimeGeneration < 1 || usage.LatestRuntimeGeneration > value.Generation || usage.MeasurementGeneration < 1 ||
			(usage.ReceivedBytes == nil) != (usage.TransmittedBytes == nil) ||
			(usage.ReceivedBytes == nil) != (usage.CheckpointedAt == nil) || usage.ObservedAt == nil || usage.ObservedAt.IsZero() ||
			usage.CheckpointedAt != nil && usage.CheckpointedAt.IsZero() {
			return false
		}
		switch usage.State {
		case "measuring":
			if usage.StableErrorCode != nil {
				return false
			}
		case "ready":
			if usage.ReceivedBytes == nil || usage.StableErrorCode != nil {
				return false
			}
		case "failed":
			if usage.StableErrorCode == nil || !validMutationIdentifier(*usage.StableErrorCode) {
				return false
			}
		default:
			return false
		}
	}
	for _, correction := range value.UsageCorrections {
		if !validMutationIdentifier(correction.CorrectionID) || !validSandboxUsageCorrectionMetric(correction.Metric) ||
			!validSignedDecimal(correction.Adjustment, 40) || !validMutationIdentifier(correction.ReasonCode) ||
			correction.SandboxGeneration < 1 || correction.SandboxGeneration > value.Generation ||
			correction.PriorResourceVersion < 1 || !validCoordinationDigest(correction.RequestedBy) ||
			!validMutationIdentifier(correction.RequestID) || correction.CreatedAt.IsZero() {
			return false
		}
	}
	switch value.OperationState {
	case "pending", "running", "reconciling", "succeeded", "failed":
	default:
		return false
	}
	switch value.CleanupPhase {
	case "none", "complete", "blocked":
	default:
		return false
	}
	switch value.WorkspaceObservedState {
	case "pending", "available", "unknown", "failed":
	default:
		return false
	}
	switch value.DesiredState {
	case "running", "stopped":
	default:
		return false
	}
	switch value.ObservedState {
	case "pending", "running", "unknown", "failed", "stopped":
	default:
		return false
	}
	switch value.RuntimeState {
	case "", "Pending", "Running", "Pausing", "Paused", "Resuming", "Stopping", "Terminated", "Failed":
	default:
		return false
	}
	return value.ObservedAt == nil || !value.ObservedAt.IsZero()
}

func validSandboxUsageCorrectionMetric(value string) bool {
	switch value {
	case "allocatedMilliseconds", "cpuMillisMilliseconds", "memoryByteMilliseconds",
		"workspaceUsedBytes", "networkReceivedBytes", "networkTransmittedBytes":
		return true
	default:
		return false
	}
}
