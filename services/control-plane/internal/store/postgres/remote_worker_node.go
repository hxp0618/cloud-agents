package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	platform "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RemoteWorkerHeartbeatResult struct {
	Node                  internalremoteworker.NodeStatus
	ReconcileRequired     bool
	Command               *internalremoteworker.Command
	SandboxCommand        *internalremoteworker.SandboxCommand
	SandboxExecCommand    *internalremoteworker.SandboxExecCommand
	SandboxFileCommand    *internalremoteworker.SandboxFileCommand
	SandboxPTYCommand     *internalremoteworker.SandboxPTYCommand
	SandboxPreviewCommand *internalremoteworker.SandboxPreviewCommand
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

const settleRemoteWorkerSandboxSQL = `SELECT outbox_state, operation_state, resource_version
FROM cloud_agents.settle_remote_worker_foundation_sandbox_v3(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`

const claimRemoteWorkerSandboxExecSQL = `SELECT command_uid, workspace_uid, target_uid,
    sandbox_uid, sandbox_generation, runtime_uid, runtime_operation_uid,
    runtime_spec_digest, command_text, timeout_seconds, command_deadline_at
FROM cloud_agents.claim_remote_worker_sandbox_exec_v1($1,$2,$3,$4,$5)`

const settleRemoteWorkerSandboxExecSQL = `SELECT command_state, command_deadline_at
FROM cloud_agents.settle_remote_worker_sandbox_exec_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`

const claimRemoteWorkerSandboxFileSQL = `SELECT command_uid, event_uid, grant_uid,
    workspace_uid, target_uid, sandbox_uid, sandbox_generation, runtime_uid,
    runtime_operation_uid, runtime_spec_digest, action, file_path, read_offset,
    read_limit, read_file_version, write_content, command_deadline_at
FROM cloud_agents.claim_remote_worker_sandbox_file_v1($1,$2,$3,$4,$5)`

const settleRemoteWorkerSandboxFileSQL = `SELECT command_state, command_deadline_at
FROM cloud_agents.settle_remote_worker_sandbox_file_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`

const claimRemoteWorkerSandboxPTYSQL = `SELECT command_uid, grant_uid, workspace_uid,
    target_uid, sandbox_uid, sandbox_generation, runtime_uid, runtime_operation_uid,
    runtime_spec_digest, action, session_uid, since_offset, takeover,
    input_message_type, input_payload, command_deadline_at
FROM cloud_agents.claim_remote_worker_sandbox_pty_v1($1,$2,$3,$4,$5)`

const settleRemoteWorkerSandboxPTYSQL = `SELECT command_state, command_deadline_at
FROM cloud_agents.settle_remote_worker_sandbox_pty_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`

const claimRemoteWorkerSandboxPreviewSQL = `SELECT command_uid, grant_uid, workspace_uid,
    target_uid, sandbox_uid, sandbox_generation, runtime_uid, runtime_operation_uid,
    runtime_spec_digest, port, method, request_path, raw_query, request_headers,
    request_body, command_deadline_at
FROM cloud_agents.claim_remote_worker_sandbox_preview_v1($1,$2,$3,$4,$5)`

const settleRemoteWorkerSandboxPreviewSQL = `SELECT command_state, command_deadline_at
FROM cloud_agents.settle_remote_worker_sandbox_preview_v1(
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`

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
		if err := handle.transaction.queryRow(ctx, `SELECT target_uid FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`,
			input.Scope.ProjectID, input.EnrollmentID).Scan(&result.Node.TargetID); err != nil {
			return err
		}
		if result.Node.Validate() != nil || result.Node.EnrollmentID != input.EnrollmentID ||
			result.Node.IncarnationID != input.IncarnationID || result.Node.HealthState != "online" {
			return fmt.Errorf("%w: remote worker heartbeat projection", ErrCoordinationResultDrift)
		}
		return nil
	}, bindTenantSetting)
	if err != nil {
		return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
	}
	if input.SandboxCommandReceipt != nil {
		if err := service.settleRemoteWorkerSandbox(ctx, result.Node, input.PeerCertificateSHA256, *input.SandboxCommandReceipt); err != nil {
			return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
		}
	}
	if input.SandboxExecCommandReceipt != nil {
		if err := service.settleRemoteWorkerSandboxExec(ctx, result.Node, input.PeerCertificateSHA256, *input.SandboxExecCommandReceipt); err != nil {
			return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
		}
	}
	if input.SandboxFileCommandReceipt != nil {
		if err := service.settleRemoteWorkerSandboxFile(ctx, result.Node, input.PeerCertificateSHA256, *input.SandboxFileCommandReceipt); err != nil {
			return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
		}
	}
	if input.SandboxPTYCommandReceipt != nil {
		if err := service.settleRemoteWorkerSandboxPTY(ctx, result.Node, input.PeerCertificateSHA256, *input.SandboxPTYCommandReceipt); err != nil {
			return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
		}
	}
	if input.SandboxPreviewCommandReceipt != nil {
		if err := service.settleRemoteWorkerSandboxPreview(ctx, result.Node, input.PeerCertificateSHA256, *input.SandboxPreviewCommandReceipt); err != nil {
			return RemoteWorkerHeartbeatResult{}, mapRemoteWorkerNodeError(err)
		}
	}
	if result.Node.DesiredState != "active" || result.Node.ObservedState != "active" || !slices.Contains(result.Node.Capabilities, "docker") {
		return result, nil
	}
	reaped, err := service.ReapFoundationSandbox(ctx, input.PeerCertificateSHA256, "audit-"+rand.Text())
	if err != nil {
		return RemoteWorkerHeartbeatResult{}, err
	}
	if reaped.DatabaseOutcome != DatabaseCommitted {
		return RemoteWorkerHeartbeatResult{}, ErrMutationCommitUnknown
	}
	claim, err := service.ClaimFoundationSandbox(ctx, FoundationSandboxClaimInput{
		TargetKind: "remote-worker", TargetID: result.Node.TargetID,
		HolderID: result.Node.TargetID, HolderIncarnation: result.Node.IncarnationID,
		ClaimToken: "claim-" + rand.Text(), LeaseSeconds: 60,
		SubjectDigest: input.PeerCertificateSHA256, AuditFactID: "audit-" + rand.Text(),
	})
	if err != nil {
		return result, err
	}
	if !claim.Found {
		if slices.Contains(result.Node.Capabilities, "exec") {
			result.SandboxExecCommand, err = service.claimRemoteWorkerSandboxExec(ctx, result.Node, input.PeerCertificateSHA256)
		}
		if err == nil && result.SandboxExecCommand == nil && slices.Contains(result.Node.Capabilities, "files") {
			result.SandboxFileCommand, err = service.claimRemoteWorkerSandboxFile(ctx, result.Node, input.PeerCertificateSHA256)
		}
		if err == nil && result.SandboxExecCommand == nil && result.SandboxFileCommand == nil && slices.Contains(result.Node.Capabilities, "pty") {
			result.SandboxPTYCommand, err = service.claimRemoteWorkerSandboxPTY(ctx, result.Node, input.PeerCertificateSHA256)
		}
		if err == nil && result.SandboxExecCommand == nil && result.SandboxFileCommand == nil && result.SandboxPTYCommand == nil && slices.Contains(result.Node.Capabilities, "preview") {
			result.SandboxPreviewCommand, err = service.claimRemoteWorkerSandboxPreview(ctx, result.Node, input.PeerCertificateSHA256)
		}
		return result, err
	}
	if claim.DatabaseOutcome != DatabaseCommitted {
		return RemoteWorkerHeartbeatResult{}, ErrMutationCommitUnknown
	}
	command := internalremoteworker.SandboxCommand{
		CommandID: internalremoteworker.SandboxCommandID(claim.Claim.OperationID, int64(claim.Claim.DeliveryAttempts)),
		Attempt:   int64(claim.Claim.DeliveryAttempts), Action: claim.Claim.Action,
		OperationID: claim.Claim.OperationID, WorkspaceID: claim.Claim.WorkspaceID,
		WorkspaceName: claim.Claim.WorkspaceName, TargetID: claim.Claim.TargetID,
		SandboxID: claim.Claim.SandboxID, SandboxGeneration: claim.Claim.SandboxGeneration,
		ImageURI: claim.Claim.ImageURI, CPUMillis: claim.Claim.CPUMillis, MemoryBytes: claim.Claim.MemoryBytes,
		SpecDigest: claim.Claim.SpecDigest, NetworkPolicyID: claim.Claim.NetworkPolicyID,
		NetworkAllowedEgress: claim.Claim.NetworkAllowedEgress, Deadline: claim.Claim.ClaimExpiresAt,
	}
	if claim.Claim.PhysicalVolumeName != nil {
		command.PhysicalVolumeName = *claim.Claim.PhysicalVolumeName
	}
	if claim.Claim.RuntimeID != nil {
		command.RuntimeID = *claim.Claim.RuntimeID
	}
	command.RuntimeState = claim.Claim.RuntimeState
	if claim.Claim.RuntimeOperationID != nil {
		command.RuntimeOperationID = *claim.Claim.RuntimeOperationID
	}
	if claim.Claim.RuntimeGeneration != nil {
		command.RuntimeGeneration = *claim.Claim.RuntimeGeneration
	}
	if claim.Claim.RuntimeSpecDigest != nil {
		command.RuntimeSpecDigest = *claim.Claim.RuntimeSpecDigest
	}
	if command.Validate() != nil {
		return RemoteWorkerHeartbeatResult{}, ErrCoordinationResultDrift
	}
	result.SandboxCommand = &command
	return result, nil
}

func (service *DurableCoordinationService) claimRemoteWorkerSandboxExec(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string) (*internalremoteworker.SandboxExecCommand, error) {
	command := internalremoteworker.SandboxExecCommand{}
	err := service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, claimRemoteWorkerSandboxExecSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID, subjectDigest).Scan(
			&command.CommandID, &command.WorkspaceID, &command.TargetID, &command.SandboxID,
			&command.SandboxGeneration, &command.RuntimeID, &command.RuntimeOperationID,
			&command.RuntimeSpecDigest, &command.Command, &command.TimeoutSeconds, &command.Deadline)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRemoteWorkerNodeError(err)
	}
	if command.Validate() != nil || command.TargetID != node.TargetID {
		return nil, ErrCoordinationResultDrift
	}
	return &command, nil
}

func (service *DurableCoordinationService) settleRemoteWorkerSandboxExec(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string, receipt internalremoteworker.SandboxExecCommandReceipt) error {
	digest, err := internalremoteworker.SandboxExecCommandReceiptDigest(receipt)
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var exitCode, executionTime, stdout, stderr, stableError any
	if receipt.Result == "succeeded" {
		exitCode, executionTime, stdout, stderr = receipt.ExitCode, receipt.ExecutionTimeMillis, receipt.Stdout, receipt.Stderr
	} else {
		stableError = receipt.StableErrorCode
	}
	var state string
	var deadline time.Time
	err = service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleRemoteWorkerSandboxExecSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID,
			subjectDigest, receipt.CommandID, receipt.SandboxID, receipt.SandboxGeneration,
			receipt.Result, exitCode, stdout, stderr, executionTime, stableError, digest).Scan(&state, &deadline)
	})
	return err
}

func (service *DurableCoordinationService) claimRemoteWorkerSandboxFile(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string) (*internalremoteworker.SandboxFileCommand, error) {
	command := internalremoteworker.SandboxFileCommand{}
	var readOffset, readLimit *int64
	var readVersion *string
	var writeContent []byte
	var deadline time.Time
	err := service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, claimRemoteWorkerSandboxFileSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID, subjectDigest).Scan(
			&command.CommandID, &command.EventID, &command.GrantID, &command.WorkspaceID,
			&command.TargetID, &command.SandboxID, &command.SandboxGeneration, &command.RuntimeID,
			&command.RuntimeOperationID, &command.RuntimeSpecDigest, &command.Action, &command.Path,
			&readOffset, &readLimit, &readVersion, &writeContent, &deadline)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRemoteWorkerNodeError(err)
	}
	if command.Action == "read" {
		if readOffset == nil || readLimit == nil {
			return nil, ErrCoordinationResultDrift
		}
		command.Read = &platform.RemoteWorkerSandboxFileReadCommand{Offset: *readOffset, Limit: *readLimit}
		if readVersion != nil {
			command.Read.FileVersion = *readVersion
		}
	} else if command.Action == "write" {
		command.Write = &platform.RemoteWorkerSandboxFileWriteCommand{ContentBase64URL: base64.RawURLEncoding.EncodeToString(writeContent)}
	} else if readOffset != nil || readLimit != nil || readVersion != nil || writeContent != nil {
		return nil, ErrCoordinationResultDrift
	}
	command.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	if internalremoteworker.ValidateSandboxFileCommand(command) != nil || command.TargetID != node.TargetID {
		return nil, ErrCoordinationResultDrift
	}
	return &command, nil
}

func (service *DurableCoordinationService) settleRemoteWorkerSandboxFile(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string, receipt internalremoteworker.SandboxFileCommandReceipt) error {
	digest, err := internalremoteworker.SandboxFileCommandReceiptDigest(receipt)
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var listEntries, readVersion, readOffset, readTotal, readContent, writeEntry, stableError any
	if receipt.Result == "failed" {
		stableError = receipt.StableErrorCode
	} else {
		switch receipt.Action {
		case "list":
			listEntries, err = json.Marshal(receipt.List.Entries)
		case "read":
			readVersion, readOffset, readTotal = receipt.Read.FileVersion, receipt.Read.Offset, receipt.Read.TotalBytes
			readContent, err = base64.RawURLEncoding.Strict().DecodeString(receipt.Read.ContentBase64URL)
		case "write":
			writeEntry, err = json.Marshal(receipt.Write.Entry)
		case "delete":
		}
	}
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var state string
	var deadline time.Time
	return service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleRemoteWorkerSandboxFileSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID,
			subjectDigest, receipt.CommandID, receipt.EventID, receipt.GrantID, receipt.SandboxID,
			receipt.SandboxGeneration, receipt.Action, receipt.Result, receipt.BytesTransferred,
			listEntries, readVersion, readOffset, readTotal, readContent, writeEntry, stableError, digest).Scan(&state, &deadline)
	})
}

func (service *DurableCoordinationService) claimRemoteWorkerSandboxPTY(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string) (*internalremoteworker.SandboxPTYCommand, error) {
	command := internalremoteworker.SandboxPTYCommand{}
	var session, messageType *string
	var since *int64
	var takeover *bool
	var inputPayload []byte
	var deadline time.Time
	err := service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, claimRemoteWorkerSandboxPTYSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID, subjectDigest).Scan(
			&command.CommandID, &command.GrantID, &command.WorkspaceID, &command.TargetID,
			&command.SandboxID, &command.SandboxGeneration, &command.RuntimeID,
			&command.RuntimeOperationID, &command.RuntimeSpecDigest, &command.Action,
			&session, &since, &takeover, &messageType, &inputPayload, &deadline)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRemoteWorkerNodeError(err)
	}
	if session != nil {
		command.SessionID = *session
	}
	command.Since, command.Takeover = since, takeover
	if messageType != nil {
		if inputPayload == nil {
			return nil, ErrCoordinationResultDrift
		}
		command.Input = &platform.RemoteWorkerSandboxPTYFrame{MessageType: *messageType,
			PayloadBase64URL: base64.RawURLEncoding.EncodeToString(inputPayload)}
	} else if inputPayload != nil {
		return nil, ErrCoordinationResultDrift
	}
	command.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	if internalremoteworker.ValidateSandboxPTYCommand(command) != nil || command.TargetID != node.TargetID {
		return nil, ErrCoordinationResultDrift
	}
	return &command, nil
}

func (service *DurableCoordinationService) settleRemoteWorkerSandboxPTY(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string, receipt internalremoteworker.SandboxPTYCommandReceipt) error {
	digest, err := internalremoteworker.SandboxPTYCommandReceiptDigest(receipt)
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var session, running, outputOffset, frames, stableError any
	if receipt.Result == "failed" {
		stableError = receipt.StableErrorCode
	} else {
		session = receipt.SessionID
		if receipt.Running != nil {
			running = *receipt.Running
		}
		if receipt.OutputOffset != nil {
			outputOffset = *receipt.OutputOffset
		}
		if receipt.Frames != nil {
			frames, err = json.Marshal(*receipt.Frames)
		}
	}
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var state string
	var deadline time.Time
	return service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleRemoteWorkerSandboxPTYSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID,
			subjectDigest, receipt.CommandID, receipt.GrantID, receipt.SandboxID,
			receipt.SandboxGeneration, receipt.Action, receipt.Result, receipt.BytesTransferred,
			session, running, outputOffset, frames, stableError, digest).Scan(&state, &deadline)
	})
}

func (service *DurableCoordinationService) claimRemoteWorkerSandboxPreview(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string) (*internalremoteworker.SandboxPreviewCommand, error) {
	command := internalremoteworker.SandboxPreviewCommand{}
	var headersJSON, body []byte
	var deadline time.Time
	err := service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, claimRemoteWorkerSandboxPreviewSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID, subjectDigest).Scan(
			&command.CommandID, &command.GrantID, &command.WorkspaceID, &command.TargetID,
			&command.SandboxID, &command.SandboxGeneration, &command.RuntimeID,
			&command.RuntimeOperationID, &command.RuntimeSpecDigest, &command.Port,
			&command.Method, &command.Path, &command.RawQuery, &headersJSON, &body, &deadline)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRemoteWorkerNodeError(err)
	}
	if json.Unmarshal(headersJSON, &command.Headers) != nil || command.Headers == nil {
		return nil, ErrCoordinationResultDrift
	}
	command.BodyBase64URL = base64.RawURLEncoding.EncodeToString(body)
	command.Deadline = deadline.UTC().Format(time.RFC3339Nano)
	if internalremoteworker.ValidateSandboxPreviewCommand(command) != nil || command.TargetID != node.TargetID {
		return nil, ErrCoordinationResultDrift
	}
	return &command, nil
}

func (service *DurableCoordinationService) settleRemoteWorkerSandboxPreview(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string, receipt internalremoteworker.SandboxPreviewCommandReceipt) error {
	digest, err := internalremoteworker.SandboxPreviewCommandReceiptDigest(receipt)
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var status, headers, body, stableError any
	if receipt.Result == "failed" {
		stableError = receipt.StableErrorCode
	} else {
		status = *receipt.StatusCode
		headers, err = json.Marshal(*receipt.Headers)
		if err == nil {
			body, err = base64.RawURLEncoding.Strict().DecodeString(*receipt.BodyBase64URL)
		}
	}
	if err != nil {
		return ErrCoordinationInvalidInput
	}
	var state string
	var deadline time.Time
	return service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleRemoteWorkerSandboxPreviewSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID,
			subjectDigest, receipt.CommandID, receipt.GrantID, receipt.SandboxID,
			receipt.SandboxGeneration, receipt.Port, receipt.Result, receipt.BytesTransferred,
			status, headers, body, stableError, digest).Scan(&state, &deadline)
	})
}

func (service *DurableCoordinationService) settleRemoteWorkerSandbox(ctx context.Context, node internalremoteworker.NodeStatus, subjectDigest string, receipt internalremoteworker.SandboxCommandReceipt) error {
	transition := "succeeded"
	if receipt.Result == "failed" {
		transition = "retry"
		if receipt.Attempt >= 8 || slices.Contains([]string{"opensandbox_runtime_failed", "foundation_network_policy_unenforced", "foundation_ownership_conflict", "foundation_configuration_invalid"}, receipt.StableErrorCode) {
			transition = "failed"
		}
	}
	optional := func(value string) any {
		if value == "" {
			return nil
		}
		return value
	}
	var outboxState, operationState string
	var resourceVersion *int64
	err := service.runner.withTenantMutation(ctx, node.Scope.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleRemoteWorkerSandboxSQL,
			node.Scope.TenantID, node.Scope.ProjectID, node.TargetID, node.IncarnationID,
			receipt.CommandID, receipt.Attempt, receipt.Action, receipt.OperationID, receipt.SandboxID,
			receipt.SandboxGeneration, transition, optional(receipt.RuntimeID), optional(receipt.RuntimeState),
			optional(receipt.VolumeName), optional(receipt.StableErrorCode), receipt.CleanupComplete,
			subjectDigest, "audit-"+rand.Text()).Scan(&outboxState, &operationState, &resourceVersion)
	})
	return err
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
		case "remote worker sandbox receipt conflict":
			return ErrRemoteWorkerSandboxReceiptConflict
		case "remote worker sandbox exec receipt conflict":
			return ErrRemoteWorkerSandboxExecReceiptConflict
		case "remote worker sandbox file receipt conflict":
			return ErrRemoteWorkerSandboxFileReceiptConflict
		case "remote worker sandbox PTY receipt conflict":
			return ErrRemoteWorkerSandboxPTYReceiptConflict
		case "remote worker sandbox Preview receipt conflict":
			return ErrRemoteWorkerSandboxPreviewReceiptConflict
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
		case "remote worker sandbox receipt is invalid", "remote worker sandbox exec claim is invalid",
			"remote worker sandbox exec receipt is invalid", "remote worker sandbox file claim is invalid",
			"remote worker sandbox file receipt is invalid", "remote worker sandbox PTY claim is invalid",
			"remote worker sandbox PTY receipt is invalid", "remote worker sandbox Preview claim is invalid",
			"remote worker sandbox Preview receipt is invalid":
			return ErrCoordinationInvalidInput
		}
	}
	return mapRemoteWorkerEnrollmentError(err)
}

var ErrRemoteWorkerGenerationConflict = errors.New("remote worker generation conflicts")
var ErrRemoteWorkerCommandReceiptConflict = errors.New("remote worker command receipt conflicts")
var ErrRemoteWorkerSandboxReceiptConflict = errors.New("remote worker sandbox receipt conflicts")
var ErrRemoteWorkerSandboxExecReceiptConflict = errors.New("remote worker sandbox exec receipt conflicts")
var ErrRemoteWorkerSandboxFileReceiptConflict = errors.New("remote worker sandbox file receipt conflicts")
var ErrRemoteWorkerSandboxPTYReceiptConflict = errors.New("remote worker sandbox PTY receipt conflicts")
var ErrRemoteWorkerSandboxPreviewReceiptConflict = errors.New("remote worker sandbox Preview receipt conflicts")
var ErrRemoteWorkerSchedulingIdempotencyConflict = errors.New("remote worker scheduling idempotency key conflicts")
var ErrRemoteWorkerOperationInProgress = errors.New("remote worker operation is in progress")
var ErrRemoteWorkerSchedulingResourceVersionConflict = errors.New("remote worker scheduling resource version conflicts")
var ErrRemoteWorkerSchedulingStateConflict = errors.New("remote worker scheduling state conflicts")
var ErrRemoteWorkerSchedulingImpactConflict = errors.New("remote worker scheduling impact conflicts")
var ErrRemoteWorkerNodeUnavailable = errors.New("remote worker node is unavailable")
