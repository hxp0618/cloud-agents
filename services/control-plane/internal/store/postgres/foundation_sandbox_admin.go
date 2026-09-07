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
	TenantID               string     `json:"tenant_id"`
	ProjectID              string     `json:"project_uid"`
	SandboxID              string     `json:"sandbox_uid"`
	OperationID            string     `json:"operation_id"`
	OperationState         string     `json:"operation_state"`
	CleanupPhase           string     `json:"cleanup_phase"`
	WorkspaceID            string     `json:"workspace_uid"`
	WorkspaceName          string     `json:"workspace_name"`
	VolumeID               string     `json:"volume_uid"`
	PhysicalVolumeID       *string    `json:"physical_volume_uid"`
	WorkspaceRetention     string     `json:"retention"`
	WorkspaceObservedState string     `json:"workspace_observed_state"`
	RuntimeProfileID       string     `json:"runtime_profile_uid"`
	RuntimeProfileVersion  int64      `json:"runtime_profile_version"`
	NetworkPolicyID        string     `json:"network_policy_ref"`
	TargetID               string     `json:"target_uid"`
	Generation             int64      `json:"generation"`
	ObservedGeneration     int64      `json:"observed_generation"`
	DesiredState           string     `json:"desired_state"`
	ObservedState          string     `json:"observed_state"`
	WriterReleased         bool       `json:"writer_released"`
	TTLSeconds             *int32     `json:"ttl_seconds"`
	ExpiresAt              *time.Time `json:"expires_at"`
	LifecycleTrigger       *string    `json:"lifecycle_trigger"`
	RuntimeID              *string    `json:"runtime_uid"`
	RuntimeState           string     `json:"runtime_state"`
	StableErrorCode        *string    `json:"stable_error_code"`
	ResourceVersion        int64      `json:"resource_version"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	ObservedAt             *time.Time `json:"observed_at"`
}

const adminSandboxColumns = `sandbox.tenant_id, sandbox.project_uid, sandbox.sandbox_uid,
    sandbox.operation_id, operation.state AS operation_state, operation.cleanup_phase,
    sandbox.workspace_uid, workspace.workspace_name, volume.volume_uid, volume.physical_volume_uid,
    volume.retention, volume.observed_state AS workspace_observed_state,
    sandbox.runtime_profile_uid, sandbox.runtime_profile_version, volume.target_uid,
	COALESCE(profile.network_policy_ref, '') AS network_policy_ref,
    sandbox.generation, sandbox.observed_generation, sandbox.desired_state, sandbox.observed_state,
    sandbox.writer_released, sandbox.ttl_seconds, sandbox.expires_at, activity.lifecycle_trigger,
    sandbox.runtime_uid, sandbox.runtime_state, sandbox.stable_error_code,
    sandbox.resource_version, operation.created_at, operation.updated_at, sandbox.observed_at`

var (
	getAdminSandboxSQL = `SELECT ` + adminSandboxColumns + `
FROM cloud_agents.sandbox_sessions AS sandbox
JOIN cloud_agents.workspaces AS workspace USING (tenant_id, project_uid, workspace_uid)
JOIN cloud_agents.workspace_volumes AS volume USING (tenant_id, project_uid, workspace_uid)
JOIN cloud_agents.platform_operations AS operation
  ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
 AND operation.operation_generation = sandbox.operation_generation
LEFT JOIN cloud_agents.runtime_profiles AS profile
  ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
 AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
  ON activity.tenant_id = sandbox.tenant_id AND activity.operation_uid = sandbox.operation_id
 AND activity.sandbox_generation = sandbox.operation_generation
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
    JOIN cloud_agents.platform_operations AS operation
      ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
     AND operation.operation_generation = sandbox.operation_generation
    LEFT JOIN cloud_agents.runtime_profiles AS profile
      ON profile.tenant_id = sandbox.tenant_id AND profile.project_uid = sandbox.project_uid
     AND profile.profile_uid = sandbox.runtime_profile_uid AND profile.profile_version = sandbox.runtime_profile_version
    LEFT JOIN cloud_agents.foundation_sandbox_activity AS activity
      ON activity.tenant_id = sandbox.tenant_id AND activity.operation_uid = sandbox.operation_id
     AND activity.sandbox_generation = sandbox.operation_generation
    WHERE sandbox.tenant_id = cloud_agents.require_tenant_id() AND sandbox.project_uid = $1
      AND sandbox.sandbox_uid > $2
    ORDER BY sandbox.sandbox_uid
    LIMIT $3
) AS sandbox_row`
	transitionFoundationSandboxSQL = `SELECT operation_uid, idempotency_key, action, sandbox_uid,
    sandbox_generation, requested_by, request_id, requested_at, updated_at, operation_state,
    cleanup_phase, stable_error_code, compute_disposition, workspace_disposition
FROM cloud_agents.transition_foundation_sandbox_v4($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
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
				(targetKind != "docker" && targetKind != "remote-worker") || runtimeID == nil || runtimeOperationID == nil ||
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
			if targetKind == "docker" {
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
		&value.RuntimeProfileID, &value.RuntimeProfileVersion, &value.TargetID, &value.NetworkPolicyID,
		&value.Generation,
		&value.ObservedGeneration, &value.DesiredState, &value.ObservedState, &value.WriterReleased,
		&value.TTLSeconds, &value.ExpiresAt, &value.LifecycleTrigger, &value.RuntimeID,
		&value.RuntimeState, &value.StableErrorCode, &value.ResourceVersion,
		&value.CreatedAt, &value.UpdatedAt, &value.ObservedAt)
}

func adminSandboxSnapshot(row adminSandboxPageRow, tenantID, projectID string) (AdminSandboxSnapshot, error) {
	value := AdminSandboxSnapshot{
		Scope:     internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
		SandboxID: row.SandboxID, OperationID: row.OperationID, OperationState: row.OperationState,
		CleanupPhase: row.CleanupPhase, WorkspaceID: row.WorkspaceID, WorkspaceName: row.WorkspaceName,
		VolumeID: row.VolumeID, PhysicalVolumeID: row.PhysicalVolumeID, WorkspaceObservedState: row.WorkspaceObservedState,
		RuntimeProfileID: row.RuntimeProfileID, RuntimeProfileVersion: row.RuntimeProfileVersion,
		NetworkPolicyID: row.NetworkPolicyID,
		TargetID:        row.TargetID, Generation: row.Generation, ObservedGeneration: row.ObservedGeneration,
		DesiredState: row.DesiredState, ObservedState: row.ObservedState, WriterReleased: row.WriterReleased,
		TTLSeconds: row.TTLSeconds, ExpiresAt: row.ExpiresAt, LifecycleTrigger: row.LifecycleTrigger,
		RuntimeID: row.RuntimeID, RuntimeState: row.RuntimeState, StableErrorCode: row.StableErrorCode,
		ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		ObservedAt: row.ObservedAt,
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
