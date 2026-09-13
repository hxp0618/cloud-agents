package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentExecutionNotFound = errors.New("managed agent execution was not found")

const managedAgentExecutionAuditProjectionSQL = `SELECT state, recovery_state, resource_version
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND session_uid = $2 AND turn_uid = $3 AND execution_uid = $4`

type ManagedAgentExecutionPage struct {
	Executions []internalmanagedagent.ExecutionSnapshot
	NextTurnID string
}

type managedAgentExecutionPageRow struct {
	TenantID                string     `json:"tenant_id"`
	ProjectID               string     `json:"project_uid"`
	SessionID               string     `json:"session_uid"`
	TurnID                  string     `json:"turn_uid"`
	ExecutionID             string     `json:"execution_uid"`
	Generation              int64      `json:"generation"`
	State                   string     `json:"state"`
	ResultDigest            *string    `json:"result_digest"`
	ErrorCode               *string    `json:"error_code"`
	AttemptNumber           int64      `json:"attempt_number"`
	ClaimExpiresAt          *time.Time `json:"claim_expires_at"`
	CheckpointSequence      int64      `json:"checkpoint_sequence"`
	CheckpointDigest        *string    `json:"checkpoint_digest"`
	CheckpointedAt          *time.Time `json:"checkpointed_at"`
	CheckpointProtocol      *string    `json:"checkpoint_protocol"`
	PendingSideEffect       bool       `json:"pending_side_effect"`
	PendingInteractionCount int32      `json:"pending_interaction_count"`
	RecoveryState           string     `json:"recovery_state"`
	RecoveryReason          *string    `json:"recovery_reason"`
	RecoveryMode            *string    `json:"recovery_mode"`
	RecoverySourceTargetID  *string    `json:"recovery_source_target_uid"`
	RecoveryTargetID        *string    `json:"recovery_target_uid"`
	ResourceVersion         int64      `json:"resource_version"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

const (
	createManagedAgentExecutionSQL = `SELECT execution_uid
FROM cloud_agents.create_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	startManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.start_claimed_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	settleManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.settle_claimed_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
	cancelManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.cancel_managed_agent_execution_v2($1, $2, $3, $4, $5, $6, $7, $8)`
	interruptManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.interrupt_managed_agent_execution_v2($1, $2, $3, $4, $5, $6, $7, $8)`
	claimManagedAgentExecutionSQL = `SELECT acquired, attempt_number, claim_expires_at, recovery_state, recovery_reason,
checkpoint_sequence, checkpoint_digest, checkpointed_at, checkpoint_protocol, pending_side_effect,
pending_interaction_count, checkpoint_provider_resume_cursor
FROM cloud_agents.claim_managed_agent_execution_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	renewManagedAgentExecutionClaimSQL         = `SELECT cloud_agents.renew_managed_agent_execution_claim_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	releaseQueuedManagedAgentExecutionClaimSQL = `SELECT cloud_agents.release_queued_managed_agent_execution_claim_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	checkpointManagedAgentExecutionSQL         = `SELECT checkpoint_sequence, claim_expires_at, checkpointed_at
FROM cloud_agents.checkpoint_managed_agent_execution_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
	resolveManagedAgentExecutionInteractionSQL  = `SELECT cloud_agents.resolve_managed_agent_execution_interaction_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	reconcileManagedAgentExecutionSideEffectSQL = `SELECT cloud_agents.reconcile_managed_agent_execution_side_effect_v1($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	getManagedAgentExecutionSQL                 = `SELECT execution_uid, generation, attempt_number, state, result_digest, error_code,
resource_version, created_at, updated_at, terminal_message, runtime_messages, claim_expires_at,
checkpoint_sequence, checkpoint_digest, checkpointed_at, checkpoint_protocol,
checkpoint_provider_resume_cursor, pending_side_effect, pending_interaction_count, recovery_state, recovery_reason,
recovery_mode, recovery_source_target_uid, recovery_target_uid,
side_effect_reconciliation_checkpoint_digest, side_effect_reconciliation_outcome,
side_effect_reconciliation_digest, side_effect_reconciled_at,
COALESCE((SELECT pg_catalog.jsonb_agg(pg_catalog.jsonb_build_object(
    'interactionRequestId', resolution.interaction_request_uid,
    'interactionType', resolution.interaction_type,
    'requestId', resolution.request_uid,
    'digest', resolution.resolution_digest,
    'payload', resolution.resolution_payload::pg_catalog.jsonb,
    'createdAt', resolution.created_at
) ORDER BY resolution.created_at, resolution.interaction_request_uid)
FROM cloud_agents.managed_agent_execution_interaction_resolutions AS resolution
WHERE resolution.tenant_id = cloud_agents.require_tenant_id()
    AND resolution.project_uid = $1 AND resolution.session_uid = $2 AND resolution.turn_uid = $3
    AND resolution.execution_uid = $4), '[]'::pg_catalog.jsonb)
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3 AND execution_uid = $4`
	managedAgentExecutionPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3`
	listManagedAgentExecutionsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(managed_execution)
    ORDER BY managed_execution.turn_uid), '[]'::jsonb)
FROM (
	    SELECT tenant_id, project_uid, session_uid, turn_uid, execution_uid, generation, state,
	        result_digest, error_code, attempt_number, claim_expires_at,
	        checkpoint_sequence, checkpoint_digest, checkpointed_at, checkpoint_protocol,
	        pending_side_effect, pending_interaction_count, recovery_state, recovery_reason,
	        recovery_mode, recovery_source_target_uid, recovery_target_uid,
	        resource_version, created_at, updated_at
    FROM cloud_agents.managed_agent_executions
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND session_uid = $2
        AND turn_uid > $3
    ORDER BY turn_uid
    LIMIT $4
) AS managed_execution`
)

func (service *DurableCoordinationService) CreateManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CreateExecutionInput,
) (internalmanagedagent.ExecutionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.ExecutionCreateMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionSnapshot
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var executionID string
		if err := handle.transaction.queryRow(ctx, createManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest).Scan(&executionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCoordinationResultDrift
			}
			return mapMutationDatabaseError("managed agent execution", err)
		}
		if !validMutationIdentifier(executionID) {
			return ErrCoordinationResultDrift
		}
		if err := scanManagedAgentExecution(handle.transaction.queryRow(ctx, getManagedAgentExecutionSQL,
			input.Scope.ProjectID, input.SessionID, input.TurnID, executionID), input.Scope, input.SessionID, input.TurnID, &result); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCoordinationResultDrift
			}
			return err
		}
		if result.ExecutionID != executionID {
			return ErrCoordinationResultDrift
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.SessionID, Operation: "execution.create", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.TurnID, ExecutionID: result.ExecutionID, Generation: result.Generation, MutationDigest: digest,
			Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceExecution, To: string(result.State), Version: result.Version}},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) ClaimManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.ClaimRuntimeExecutionInput,
) (internalmanagedagent.RuntimeExecutionClaim, error) {
	claim := internalmanagedagent.RuntimeExecutionClaim{RuntimeExecutionReference: input.RuntimeExecutionReference,
		HolderID: input.HolderID, Incarnation: input.Incarnation, Token: input.Token}
	if service == nil || service.runner == nil || ctx == nil || input.Scope.TenantID != tenantID ||
		input.Generation > math.MaxInt64 || input.LeaseSeconds < 1 || input.LeaseSeconds > 60 ||
		!validMutationIdentifier(input.HolderID) || !validMutationIdentifier(input.Incarnation) || !validMutationIdentifier(input.Token) {
		return claim, ErrCoordinationInvalidInput
	}
	err := withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var attempt, sequence int64
		var expiresAt, checkpointedAt *time.Time
		var recoveryReason, checkpointDigest, checkpointProtocol, providerResumeCursor *string
		var pendingInteractions int32
		if err := handle.transaction.queryRow(ctx, claimManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.HolderID, input.Incarnation, input.Token, input.LeaseSeconds,
		).Scan(&claim.Acquired, &attempt, &expiresAt, &claim.RecoveryState, &recoveryReason, &sequence,
			&checkpointDigest, &checkpointedAt, &checkpointProtocol, &claim.PendingSideEffect,
			&pendingInteractions, &providerResumeCursor); err != nil {
			return mapMutationDatabaseError("managed agent execution claim", err)
		}
		if attempt < 0 || sequence < 0 || pendingInteractions < 0 || pendingInteractions > 64 {
			return ErrCoordinationResultDrift
		}
		claim.AttemptNumber = uint64(attempt)
		claim.CheckpointSequence = uint64(sequence)
		claim.PendingInteractionCount = uint32(pendingInteractions)
		if expiresAt != nil {
			claim.ExpiresAt = *expiresAt
		}
		if recoveryReason != nil {
			claim.RecoveryReason = *recoveryReason
		}
		if checkpointDigest != nil {
			claim.CheckpointDigest = *checkpointDigest
		}
		if checkpointedAt != nil {
			claim.CheckpointedAt = *checkpointedAt
		}
		if checkpointProtocol != nil {
			claim.CheckpointProtocol = *checkpointProtocol
		}
		if providerResumeCursor != nil {
			claim.CheckpointProviderResumeCursor = *providerResumeCursor
		}
		if claim.RecoveryState == "recovering" || claim.RecoveryState == "awaiting_reconciliation" {
			var snapshot internalmanagedagent.ExecutionSnapshot
			if err := scanManagedAgentExecution(handle.transaction.queryRow(ctx, getManagedAgentExecutionSQL,
				input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID), input.Scope,
				input.SessionID, input.TurnID, &snapshot); err != nil {
				return err
			}
			claim.RecoveryMode = snapshot.RecoveryMode
			claim.RecoverySourceTargetID = snapshot.RecoverySourceTargetID
			claim.RecoveryTargetID = snapshot.RecoveryTargetID
			operation := "execution.takeover"
			if !claim.Acquired {
				operation = "execution.recovery-blocked"
			} else if snapshot.RecoveryMode == "same-node-reconnect" {
				operation = "execution.reconnect"
			} else if snapshot.RecoveryMode == "process-restart" {
				operation = "execution.restart"
			}
			return appendRuntimeExecutionAuditEvent(ctx, handle.transaction, snapshot, operation,
				runtimeExecutionClaimEventDigest(input), claim.RecoveryReason,
				"recovery:none", "recovery:"+claim.RecoveryState)
		}
		return nil
	})
	return claim, err
}

func (service *DurableCoordinationService) RenewManagedAgentExecutionClaim(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	claim internalmanagedagent.RuntimeExecutionClaim, leaseSeconds int32,
) (time.Time, error) {
	if service == nil || service.runner == nil || ctx == nil || claim.Scope.TenantID != tenantID ||
		claim.Generation > math.MaxInt64 || claim.AttemptNumber > math.MaxInt64 || leaseSeconds < 1 || leaseSeconds > 60 ||
		!internalmanagedagent.ValidRuntimeExecutionClaim(claim) {
		return time.Time{}, ErrCoordinationInvalidInput
	}
	var expiresAt time.Time
	err := withManagedAgentProjectMutation(service, ctx, tenantID, principal, claim.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := handle.transaction.queryRow(ctx, renewManagedAgentExecutionClaimSQL,
			claim.Scope.TenantID, claim.Scope.ProjectID, claim.SessionID, claim.TurnID, claim.ExecutionID,
			int64(claim.Generation), int64(claim.AttemptNumber), claim.HolderID, claim.Incarnation, claim.Token, leaseSeconds,
		).Scan(&expiresAt); err != nil {
			return mapMutationDatabaseError("managed agent execution claim", err)
		}
		return nil
	})
	return expiresAt, err
}

func (service *DurableCoordinationService) ReleaseQueuedManagedAgentExecutionClaim(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, claim internalmanagedagent.RuntimeExecutionClaim,
) error {
	if service == nil || service.runner == nil || ctx == nil || claim.Scope.TenantID != tenantID ||
		claim.Generation > math.MaxInt64 || claim.AttemptNumber > math.MaxInt64 || !internalmanagedagent.ValidRuntimeExecutionClaim(claim) {
		return ErrCoordinationInvalidInput
	}
	return withManagedAgentProjectMutation(service, ctx, tenantID, principal, claim.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var released bool
		if err := handle.transaction.queryRow(ctx, releaseQueuedManagedAgentExecutionClaimSQL,
			claim.Scope.TenantID, claim.Scope.ProjectID, claim.SessionID, claim.TurnID, claim.ExecutionID,
			int64(claim.Generation), int64(claim.AttemptNumber), claim.HolderID, claim.Incarnation, claim.Token,
		).Scan(&released); err != nil || !released {
			if err != nil {
				return mapMutationDatabaseError("managed agent execution claim", err)
			}
			return ErrCoordinationResultDrift
		}
		return nil
	})
}

func (service *DurableCoordinationService) CheckpointManagedAgentExecution(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CheckpointRuntimeExecutionInput,
) (internalmanagedagent.RuntimeExecutionCheckpoint, error) {
	claim := input.Claim
	if service == nil || service.runner == nil || ctx == nil || claim.Scope.TenantID != tenantID ||
		claim.Generation > math.MaxInt64 || claim.AttemptNumber > math.MaxInt64 || input.PendingInteractions > 64 ||
		!internalmanagedagent.ValidRuntimeExecutionClaim(claim) {
		return internalmanagedagent.RuntimeExecutionCheckpoint{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.RuntimeMessagesDigest(input.Messages, claim.ExecutionID, claim.Generation)
	if err != nil || input.Protocol != "runtime-message-checkpoint-v1" {
		return internalmanagedagent.RuntimeExecutionCheckpoint{}, ErrCoordinationInvalidInput
	}
	encoded, err := json.Marshal(input.Messages)
	if err != nil {
		return internalmanagedagent.RuntimeExecutionCheckpoint{}, ErrCoordinationInvalidInput
	}
	result := internalmanagedagent.RuntimeExecutionCheckpoint{Digest: digest}
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, claim.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var sequence int64
		if err := handle.transaction.queryRow(ctx, checkpointManagedAgentExecutionSQL,
			claim.Scope.TenantID, claim.Scope.ProjectID, claim.SessionID, claim.TurnID, claim.ExecutionID,
			int64(claim.Generation), int64(claim.AttemptNumber), claim.HolderID, claim.Incarnation, claim.Token,
			int32(30), string(encoded), digest, input.Protocol, nullableString(input.ProviderResumeCursor),
			input.PendingSideEffect, int32(input.PendingInteractions),
		).Scan(&sequence, &result.ExpiresAt, &result.CreatedAt); err != nil {
			return mapMutationDatabaseError("managed agent execution checkpoint", err)
		}
		if sequence <= 0 {
			return ErrCoordinationResultDrift
		}
		result.Sequence = uint64(sequence)
		return nil
	})
	return result, err
}

func (service *DurableCoordinationService) ResolveManagedAgentExecutionInteraction(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	input internalmanagedagent.ResolveRuntimeInteractionInput,
) (internalmanagedagent.RuntimeInteractionResolution, error) {
	result := internalmanagedagent.RuntimeInteractionResolution{InteractionRequestID: input.InteractionRequestID,
		InteractionType: input.InteractionType, RequestID: input.RequestID, Payload: input.Payload}
	digest, payload, err := internalmanagedagent.RuntimeInteractionResolutionDigest(input)
	if service == nil || service.runner == nil || ctx == nil || input.Scope.TenantID != tenantID ||
		input.Generation > math.MaxInt64 || err != nil {
		return result, ErrCoordinationInvalidInput
	}
	result.Digest = digest
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var resolved bool
		if err := handle.transaction.queryRow(ctx, resolveManagedAgentExecutionInteractionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.InteractionRequestID, input.InteractionType, input.RequestID, digest, payload,
		).Scan(&resolved); err != nil || !resolved {
			if err != nil {
				return mapMutationDatabaseError("managed agent interaction resolution", err)
			}
			return ErrCoordinationResultDrift
		}
		result.CreatedAt = time.Now().UTC()
		return nil
	})
	return result, err
}

func (service *DurableCoordinationService) ReconcileManagedAgentExecutionSideEffect(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	input internalmanagedagent.ReconcileRuntimeSideEffectInput,
) (internalmanagedagent.RuntimeSideEffectReconciliation, error) {
	result := internalmanagedagent.RuntimeSideEffectReconciliation{CheckpointDigest: input.CheckpointDigest, Outcome: input.Outcome}
	digest, err := internalmanagedagent.RuntimeSideEffectReconciliationDigest(input)
	if service == nil || service.runner == nil || ctx == nil || input.Scope.TenantID != tenantID ||
		input.Generation > math.MaxInt64 || err != nil {
		return result, ErrCoordinationInvalidInput
	}
	result.Digest = digest
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := handle.transaction.queryRow(ctx, reconcileManagedAgentExecutionSideEffectSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.CheckpointDigest, input.Outcome, digest,
		).Scan(&result.CreatedAt); err != nil {
			return mapMutationDatabaseError("managed agent side effect reconciliation", err)
		}
		if result.CreatedAt.IsZero() {
			return ErrCoordinationResultDrift
		}
		var snapshot internalmanagedagent.ExecutionSnapshot
		if err := scanManagedAgentExecution(handle.transaction.queryRow(ctx, getManagedAgentExecutionSQL,
			input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID), input.Scope,
			input.SessionID, input.TurnID, &snapshot); err != nil {
			return err
		}
		return appendRuntimeExecutionAuditEvent(ctx, handle.transaction, snapshot, "execution.reconcile",
			digest, "", "recovery:awaiting_reconciliation", "recovery:none")
	})
	return result, err
}

func (service *DurableCoordinationService) StartManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.StartExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	if input.Claim.ExecutionID != input.ExecutionID || input.Claim.Generation != input.Generation ||
		input.Claim.AttemptNumber > math.MaxInt64 || !internalmanagedagent.ValidRuntimeExecutionClaim(input.Claim) {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.ExecutionStartMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, startManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest, int64(input.Claim.AttemptNumber),
			input.Claim.HolderID, input.Claim.Incarnation, input.Claim.Token), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "execution.start", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation, MutationDigest: digest,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnQueued), To: string(internalmanagedagent.TurnRunning), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionQueued), To: string(internalmanagedagent.ExecutionRunning), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) CompleteManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CompleteRuntimeExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.RuntimeExecutionCompleteMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	terminalMessage, err := json.Marshal(input.Messages[len(input.Messages)-1])
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	runtimeMessages, err := json.Marshal(input.Messages)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "succeeded", input.ResultDigest, "", input.Mutation.IdempotencyKey, digest, input.ProviderResumeCursor, string(terminalMessage), string(runtimeMessages), input.Claim)
}

func (service *DurableCoordinationService) FailManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.FailRuntimeExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.RuntimeExecutionFailMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var runtimeMessages string
	if len(input.Messages) > 0 {
		encoded, encodeErr := json.Marshal(input.Messages)
		if encodeErr != nil {
			return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
		}
		runtimeMessages = string(encoded)
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "failed", "", input.ErrorCode, input.Mutation.IdempotencyKey, digest, "", "", runtimeMessages, input.Claim)
}

func (service *DurableCoordinationService) CancelManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CancelTurnInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.TurnCancelMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	if service == nil || service.runner == nil || input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, cancelManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.TargetExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "turn.cancel", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: digest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: string(internalmanagedagent.TurnCancelled), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: string(internalmanagedagent.ExecutionCancelled), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) InterruptManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.InterruptTurnInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.TurnInterruptMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	if service == nil || service.runner == nil || input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, interruptManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.TargetExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "turn.interrupt", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: digest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: string(internalmanagedagent.TurnInterrupted), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: string(internalmanagedagent.ExecutionCancelled), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) settleManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	scope internalmanagedagent.Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	outcome, resultDigest, errorCode, idempotencyKey, requestDigest, providerResumeCursor, terminalMessage, runtimeMessages string,
	claim internalmanagedagent.RuntimeExecutionClaim,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrNilCoordinationRunner
	}
	if scope.TenantID != tenantID || ctx == nil || generation > math.MaxInt64 ||
		claim.ExecutionID != executionID || claim.Generation != generation || claim.AttemptNumber > math.MaxInt64 ||
		!internalmanagedagent.ValidRuntimeExecutionClaim(claim) {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err := withManagedAgentProjectMutation(service, ctx, tenantID, principal, scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, settleManagedAgentExecutionSQL,
			scope.TenantID, scope.ProjectID, sessionID, turnID, executionID, int64(generation), outcome,
			nullableString(resultDigest), nullableString(errorCode), idempotencyKey, requestDigest, nullableString(providerResumeCursor), nullableString(terminalMessage), nullableString(runtimeMessages),
			int64(claim.AttemptNumber), claim.HolderID, claim.Incarnation, claim.Token), scope, sessionID, &result); err != nil {
			return err
		}
		operation := "execution.complete"
		executionTo := string(internalmanagedagent.ExecutionSucceeded)
		turnTo := string(internalmanagedagent.TurnCompleted)
		if outcome == "failed" {
			operation = "execution.fail"
			executionTo = string(internalmanagedagent.ExecutionFailed)
			turnTo = string(internalmanagedagent.TurnFailed)
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: scope, SessionID: result.Execution.SessionID, Operation: operation, Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: requestDigest, ResultDigest: result.Execution.ResultDigest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: turnTo, Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: executionTo, Version: result.Execution.Version},
			},
		})
	})
	if err != nil && errors.Is(err, ErrMutationConflict) {
		auditErr := service.recordRejectedRuntimeReceipt(ctx, tenantID, principal, scope, sessionID,
			turnID, executionID, generation, requestDigest)
		if auditErr != nil {
			return result, errors.Join(err, auditErr)
		}
	}
	return result, err
}

func (service *DurableCoordinationService) recordRejectedRuntimeReceipt(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, scope internalmanagedagent.Scope,
	sessionID, turnID, executionID string, generation uint64, requestDigest string,
) error {
	return withManagedAgentProjectMutation(service, ctx, tenantID, principal, scope.ProjectID, func(handle *tenantReadHandle) error {
		var state, recoveryState string
		var version int64
		if err := handle.transaction.queryRow(ctx, managedAgentExecutionAuditProjectionSQL,
			scope.ProjectID, sessionID, turnID, executionID).Scan(&state, &recoveryState, &version); err != nil {
			return mapMutationDatabaseError("managed agent rejected receipt audit", err)
		}
		if version <= 0 {
			return ErrCoordinationResultDrift
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: scope, SessionID: sessionID, Operation: "execution.receipt-rejected",
			Resource: internalmanagedagent.ResourceExecution, TurnID: turnID, ExecutionID: executionID,
			Generation: generation, MutationDigest: requestDigest, ErrorCode: "stale_or_conflicting_receipt",
			Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceExecution,
				From: state + "/" + recoveryState, To: state + "/" + recoveryState, Version: uint64(version)}},
		})
	})
}

func appendRuntimeExecutionAuditEvent(
	ctx context.Context, transaction tenantTransaction, snapshot internalmanagedagent.ExecutionSnapshot,
	operation, digest, errorCode, from, to string,
) error {
	return appendManagedAgentEvent(ctx, transaction, managedAgentEventInput{
		Scope: snapshot.Scope, SessionID: snapshot.SessionID, Operation: operation,
		Resource: internalmanagedagent.ResourceExecution, TurnID: snapshot.TurnID,
		ExecutionID: snapshot.ExecutionID, Generation: snapshot.Generation, MutationDigest: digest,
		ErrorCode: errorCode, Changes: []internalmanagedagent.LifecycleStateChange{{
			Resource: internalmanagedagent.ResourceExecution, From: from, To: to, Version: snapshot.Version,
		}},
	})
}

func runtimeExecutionClaimEventDigest(input internalmanagedagent.ClaimRuntimeExecutionInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s",
		input.SessionID, input.TurnID, input.ExecutionID, input.Generation,
		input.HolderID, input.Incarnation, input.Token)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (service *DurableCoordinationService) GetManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID, sessionID, turnID, executionID string,
) (internalmanagedagent.ExecutionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || tenantID == "" || projectID == "" || sessionID == "" || turnID == "" || executionID == "" {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionSnapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, func() error {
				err := scanManagedAgentExecution(handle.transaction.queryRow(readContext, getManagedAgentExecutionSQL, projectID, sessionID, turnID, executionID), scope, sessionID, turnID, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentExecutionNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) ListManagedAgentExecutions(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	afterTurnID string,
	limit int,
) (ManagedAgentExecutionPage, error) {
	if service == nil || service.runner == nil {
		return ManagedAgentExecutionPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(sessionID) || afterTurnID != "" && !validMutationIdentifier(afterTurnID) ||
		limit < 1 || limit > 200 {
		return ManagedAgentExecutionPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ManagedAgentExecutionPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterTurnID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, managedAgentExecutionPageCursorIdentitySQL, projectID, sessionID, afterTurnID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("managed agent execution page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedAgentExecutionsSQL, projectID, sessionID, afterTurnID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("managed agent executions", err)
				}
				var err error
				result, err = decodeManagedAgentExecutionPageRows(raw, tenantID, projectID, sessionID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func withManagedAgentProjectMutation(service *DurableCoordinationService, ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID string, callback func(*tenantReadHandle) error) error {
	return authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantReadCommittedMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, func() error { return callback(handle) })
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func decodeManagedAgentExecutionPageRows(raw []byte, tenantID, projectID, sessionID string, limit int) (ManagedAgentExecutionPage, error) {
	var rows []managedAgentExecutionPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ManagedAgentExecutionPage{}, ErrCoordinationResultDrift
	}
	executions := make([]internalmanagedagent.ExecutionSnapshot, 0, len(rows))
	for _, row := range rows {
		state := internalmanagedagent.ExecutionState(row.State)
		if row.TenantID != tenantID || row.ProjectID != projectID || row.SessionID != sessionID ||
			!validMutationIdentifier(row.TurnID) || !validMutationIdentifier(row.ExecutionID) ||
			row.Generation < 1 || row.AttemptNumber < 0 || row.CheckpointSequence < 0 || row.PendingInteractionCount < 0 || row.PendingInteractionCount > 64 ||
			row.ResourceVersion < 1 || !validManagedAgentExecutionState(state) ||
			row.ResultDigest != nil && !validCoordinationDigest(*row.ResultDigest) ||
			row.ErrorCode != nil && !internalmanagedagent.ValidRuntimeErrorCode(*row.ErrorCode) ||
			state == internalmanagedagent.ExecutionSucceeded && row.ResultDigest == nil ||
			state == internalmanagedagent.ExecutionFailed && row.ErrorCode == nil ||
			row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentExecutionPage{}, ErrCoordinationResultDrift
		}
		execution := internalmanagedagent.ExecutionSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
			TurnID: row.TurnID, ExecutionID: row.ExecutionID, Generation: uint64(row.Generation), State: state,
			AttemptNumber: uint64(row.AttemptNumber), CheckpointSequence: uint64(row.CheckpointSequence),
			PendingSideEffect: row.PendingSideEffect, PendingInteractionCount: uint32(row.PendingInteractionCount), RecoveryState: row.RecoveryState,
			Version: uint64(row.ResourceVersion), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.ClaimExpiresAt != nil {
			execution.ClaimExpiresAt = *row.ClaimExpiresAt
		}
		if row.CheckpointDigest != nil {
			execution.CheckpointDigest = *row.CheckpointDigest
		}
		if row.CheckpointedAt != nil {
			execution.CheckpointedAt = *row.CheckpointedAt
		}
		if row.CheckpointProtocol != nil {
			execution.CheckpointProtocol = *row.CheckpointProtocol
		}
		if row.RecoveryReason != nil {
			execution.RecoveryReason = *row.RecoveryReason
		}
		if row.RecoveryMode != nil {
			execution.RecoveryMode = *row.RecoveryMode
		}
		if row.RecoverySourceTargetID != nil {
			execution.RecoverySourceTargetID = *row.RecoverySourceTargetID
		}
		if row.RecoveryTargetID != nil {
			execution.RecoveryTargetID = *row.RecoveryTargetID
		}
		if row.ResultDigest != nil {
			execution.ResultDigest = *row.ResultDigest
		}
		if row.ErrorCode != nil {
			execution.ErrorCode = *row.ErrorCode
		}
		if !validExecutionRecoveryProjection(execution) {
			return ManagedAgentExecutionPage{}, ErrCoordinationResultDrift
		}
		executions = append(executions, execution)
	}
	result := ManagedAgentExecutionPage{Executions: executions}
	if len(executions) > limit {
		result.Executions = executions[:limit]
		result.NextTurnID = result.Executions[len(result.Executions)-1].TurnID
	}
	return result, nil
}

func validManagedAgentExecutionState(state internalmanagedagent.ExecutionState) bool {
	return state == internalmanagedagent.ExecutionQueued || state == internalmanagedagent.ExecutionRunning ||
		state == internalmanagedagent.ExecutionSucceeded || state == internalmanagedagent.ExecutionFailed ||
		state == internalmanagedagent.ExecutionCancelled
}

func scanManagedAgentExecution(row rowScanner, scope internalmanagedagent.Scope, sessionID, turnID string, result *internalmanagedagent.ExecutionSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var generation, attempt, version, checkpointSequence int64
	var pendingInteractions int32
	var state string
	var resultDigest, errorCode, terminalMessage, runtimeMessages, checkpointDigest, checkpointProtocol, checkpointCursor, recoveryReason *string
	var recoveryMode, recoverySourceTargetID, recoveryTargetID *string
	var reconciliationCheckpointDigest, reconciliationOutcome, reconciliationDigest *string
	var claimExpiresAt, checkpointedAt, reconciledAt *time.Time
	var resolutionsJSON []byte
	if err := row.Scan(&result.ExecutionID, &generation, &attempt, &state, &resultDigest, &errorCode,
		&version, &result.CreatedAt, &result.UpdatedAt, &terminalMessage, &runtimeMessages, &claimExpiresAt,
		&checkpointSequence, &checkpointDigest, &checkpointedAt, &checkpointProtocol, &checkpointCursor,
		&result.PendingSideEffect, &pendingInteractions, &result.RecoveryState, &recoveryReason,
		&recoveryMode, &recoverySourceTargetID, &recoveryTargetID,
		&reconciliationCheckpointDigest, &reconciliationOutcome, &reconciliationDigest, &reconciledAt, &resolutionsJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent execution", err)
	}
	result.Scope = scope
	result.SessionID = sessionID
	result.TurnID = turnID
	result.Generation = uint64(generation)
	result.AttemptNumber = uint64(attempt)
	result.CheckpointSequence = uint64(checkpointSequence)
	result.PendingInteractionCount = uint32(pendingInteractions)
	result.State = internalmanagedagent.ExecutionState(state)
	if resultDigest != nil {
		result.ResultDigest = *resultDigest
	}
	if errorCode != nil {
		result.ErrorCode = *errorCode
	}
	if claimExpiresAt != nil {
		result.ClaimExpiresAt = *claimExpiresAt
	}
	if checkpointDigest != nil {
		result.CheckpointDigest = *checkpointDigest
	}
	if checkpointedAt != nil {
		result.CheckpointedAt = *checkpointedAt
	}
	if checkpointProtocol != nil {
		result.CheckpointProtocol = *checkpointProtocol
	}
	if checkpointCursor != nil {
		result.CheckpointProviderResumeCursor = *checkpointCursor
	}
	if recoveryReason != nil {
		result.RecoveryReason = *recoveryReason
	}
	if recoveryMode != nil {
		result.RecoveryMode = *recoveryMode
	}
	if recoverySourceTargetID != nil {
		result.RecoverySourceTargetID = *recoverySourceTargetID
	}
	if recoveryTargetID != nil {
		result.RecoveryTargetID = *recoveryTargetID
	}
	if reconciliationCheckpointDigest != nil && reconciliationOutcome != nil && reconciliationDigest != nil && reconciledAt != nil {
		result.SideEffectReconciliation = &internalmanagedagent.RuntimeSideEffectReconciliation{
			CheckpointDigest: *reconciliationCheckpointDigest, Outcome: *reconciliationOutcome,
			Digest: *reconciliationDigest, CreatedAt: *reconciledAt,
		}
	} else if reconciliationCheckpointDigest != nil || reconciliationOutcome != nil || reconciliationDigest != nil || reconciledAt != nil {
		return fmt.Errorf("%w: managed agent side effect reconciliation projection", ErrCoordinationResultDrift)
	}
	if generation <= 0 || attempt < 0 || version <= 0 || checkpointSequence < 0 || pendingInteractions < 0 || pendingInteractions > 64 ||
		result.ExecutionID == "" || !validManagedAgentExecutionState(result.State) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() ||
		!validExecutionRecoveryProjection(*result) {
		return fmt.Errorf("%w: managed agent execution projection", ErrCoordinationResultDrift)
	}
	if (result.State == internalmanagedagent.ExecutionSucceeded && result.ResultDigest == "") || (result.State == internalmanagedagent.ExecutionFailed && result.ErrorCode == "") {
		return fmt.Errorf("%w: managed agent execution terminal projection", ErrCoordinationResultDrift)
	}
	if runtimeMessages != nil {
		messages, err := decodePersistedRuntimeMessages(*runtimeMessages, result.ExecutionID, result.Generation, result.State, result.ErrorCode)
		if err != nil {
			return fmt.Errorf("%w: managed agent execution Runtime messages", ErrCoordinationResultDrift)
		}
		result.Messages = messages
	} else if terminalMessage != nil {
		message, err := decodePersistedRuntimeMessage(*terminalMessage)
		if err != nil || result.State != internalmanagedagent.ExecutionSucceeded || message.MessageType != "Result" || message.ExecutionID != result.ExecutionID || message.Generation != result.Generation {
			return fmt.Errorf("%w: managed agent execution terminal message", ErrCoordinationResultDrift)
		}
		digest, err := internalmanagedagent.RuntimeMessageDigest(message)
		if err != nil || digest != result.ResultDigest {
			return fmt.Errorf("%w: managed agent execution terminal digest", ErrCoordinationResultDrift)
		}
		result.Messages = []runtimeprotocol.Message{message}
	}
	if result.State == internalmanagedagent.ExecutionRunning {
		if len(result.Messages) == 0 || result.CheckpointSequence == 0 {
			if result.CheckpointSequence != 0 || len(result.Messages) != 0 {
				return fmt.Errorf("%w: managed agent execution checkpoint projection", ErrCoordinationResultDrift)
			}
		} else if digest, err := internalmanagedagent.RuntimeMessagesDigest(result.Messages, result.ExecutionID, result.Generation); err != nil || digest != result.CheckpointDigest {
			return fmt.Errorf("%w: managed agent execution checkpoint digest", ErrCoordinationResultDrift)
		}
	}
	resolutions, err := decodeManagedAgentInteractionResolutions(resolutionsJSON)
	if err != nil {
		return fmt.Errorf("%w: managed agent interaction resolutions", ErrCoordinationResultDrift)
	}
	result.ResolvedInteractions = resolutions
	if len(result.Messages) > 0 && result.State == internalmanagedagent.ExecutionSucceeded {
		terminal := result.Messages[len(result.Messages)-1]
		digest, err := internalmanagedagent.RuntimeMessageDigest(terminal)
		if err != nil || digest != result.ResultDigest || runtimeMessages != nil && terminalMessage != nil && !runtimeMessagesEndWithTerminal(*runtimeMessages, *terminalMessage) {
			return fmt.Errorf("%w: managed agent execution terminal digest", ErrCoordinationResultDrift)
		}
	}
	result.Version = uint64(version)
	return nil
}

func validExecutionRecoveryProjection(result internalmanagedagent.ExecutionSnapshot) bool {
	if result.RecoveryState != "none" && result.RecoveryState != "recovering" && result.RecoveryState != "recovered" && result.RecoveryState != "awaiting_reconciliation" {
		return false
	}
	if result.RecoveryReason != "" && !internalmanagedagent.ValidRuntimeErrorCode(result.RecoveryReason) {
		return false
	}
	if result.RecoveryMode == "" {
		if result.RecoverySourceTargetID != "" || result.RecoveryTargetID != "" {
			return false
		}
	} else {
		if result.RecoveryMode != "same-node-reconnect" && result.RecoveryMode != "process-restart" && result.RecoveryMode != "cross-node-takeover" ||
			result.RecoverySourceTargetID != "" && !validMutationIdentifier(result.RecoverySourceTargetID) ||
			result.RecoveryTargetID != "" && !validMutationIdentifier(result.RecoveryTargetID) ||
			result.RecoveryMode == "cross-node-takeover" && (result.RecoverySourceTargetID == "" || result.RecoveryTargetID == "" || result.RecoverySourceTargetID == result.RecoveryTargetID) {
			return false
		}
	}
	if result.SideEffectReconciliation != nil {
		reconciliation := result.SideEffectReconciliation
		if reconciliation.Outcome != "confirmed" && reconciliation.Outcome != "not-applied" ||
			!validCoordinationDigest(reconciliation.CheckpointDigest) || !validCoordinationDigest(reconciliation.Digest) ||
			reconciliation.CreatedAt.IsZero() || result.CheckpointSequence == 0 {
			return false
		}
	}
	if result.CheckpointSequence == 0 {
		return result.CheckpointDigest == "" && result.CheckpointedAt.IsZero() && result.CheckpointProtocol == "" && result.CheckpointProviderResumeCursor == "" && !result.PendingSideEffect && result.PendingInteractionCount == 0 && result.SideEffectReconciliation == nil
	}
	return validCoordinationDigest(result.CheckpointDigest) && !result.CheckpointedAt.IsZero() && result.CheckpointProtocol == "runtime-message-checkpoint-v1"
}

func decodeManagedAgentInteractionResolutions(raw []byte) ([]internalmanagedagent.RuntimeInteractionResolution, error) {
	var rows []struct {
		InteractionRequestID string         `json:"interactionRequestId"`
		InteractionType      string         `json:"interactionType"`
		RequestID            string         `json:"requestId"`
		Digest               string         `json:"digest"`
		Payload              map[string]any `json:"payload"`
		CreatedAt            time.Time      `json:"createdAt"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 64 {
		return nil, ErrCoordinationResultDrift
	}
	result := make([]internalmanagedagent.RuntimeInteractionResolution, 0, len(rows))
	for _, row := range rows {
		input := internalmanagedagent.ResolveRuntimeInteractionInput{RuntimeExecutionReference: internalmanagedagent.RuntimeExecutionReference{
			Scope: internalmanagedagent.Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: "execution", Generation: 1,
		}, InteractionRequestID: row.InteractionRequestID, InteractionType: row.InteractionType, RequestID: row.RequestID, Payload: row.Payload}
		digest, _, err := internalmanagedagent.RuntimeInteractionResolutionDigest(input)
		if err != nil || digest != row.Digest || row.CreatedAt.IsZero() {
			return nil, ErrCoordinationResultDrift
		}
		result = append(result, internalmanagedagent.RuntimeInteractionResolution{InteractionRequestID: row.InteractionRequestID,
			InteractionType: row.InteractionType, RequestID: row.RequestID, Digest: row.Digest, Payload: row.Payload, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func decodePersistedRuntimeMessage(value string) (runtimeprotocol.Message, error) {
	if len(value) == 0 || len(value) > runtimeprotocol.MaxMessageBytes {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	var message runtimeprotocol.Message
	var trailing any
	if err := decoder.Decode(&message); err != nil || decoder.Decode(&trailing) != io.EOF || runtimeprotocol.ValidateMessage(message) != nil {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	encoded, err := json.Marshal(message)
	if err != nil || !bytes.Equal(encoded, []byte(value)) {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	return message, nil
}

func decodePersistedRuntimeMessages(value, executionID string, generation uint64, state internalmanagedagent.ExecutionState, errorCode string) ([]runtimeprotocol.Message, error) {
	if len(value) == 0 || len(value) > runtimeprotocol.MaxMessageBytes {
		return nil, ErrCoordinationResultDrift
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	var messages []runtimeprotocol.Message
	var trailing any
	if err := decoder.Decode(&messages); err != nil || decoder.Decode(&trailing) != io.EOF || len(messages) == 0 {
		return nil, ErrCoordinationResultDrift
	}
	encoded, err := json.Marshal(messages)
	if err != nil || !bytes.Equal(encoded, []byte(value)) {
		return nil, ErrCoordinationResultDrift
	}
	mutation := internalmanagedagent.Mutation{RequestID: "request", IdempotencyKey: "idempotency-key"}
	if state == internalmanagedagent.ExecutionSucceeded {
		terminal := messages[len(messages)-1]
		digest, digestErr := internalmanagedagent.RuntimeMessageDigest(terminal)
		if digestErr != nil {
			return nil, ErrCoordinationResultDrift
		}
		input := internalmanagedagent.CompleteRuntimeExecutionInput{CompleteExecutionInput: internalmanagedagent.CompleteExecutionInput{Scope: internalmanagedagent.Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: executionID, Generation: generation, ResultDigest: digest, Mutation: mutation}, Messages: messages}
		if _, validateErr := internalmanagedagent.RuntimeExecutionCompleteMutationDigest(input); validateErr != nil {
			return nil, ErrCoordinationResultDrift
		}
	} else if state == internalmanagedagent.ExecutionFailed {
		input := internalmanagedagent.FailRuntimeExecutionInput{FailExecutionInput: internalmanagedagent.FailExecutionInput{Scope: internalmanagedagent.Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: executionID, Generation: generation, ErrorCode: errorCode, Mutation: mutation}, Messages: messages}
		if _, validateErr := internalmanagedagent.RuntimeExecutionFailMutationDigest(input); validateErr != nil {
			return nil, ErrCoordinationResultDrift
		}
	} else if state == internalmanagedagent.ExecutionRunning {
		if _, validateErr := internalmanagedagent.RuntimeMessagesDigest(messages, executionID, generation); validateErr != nil {
			return nil, ErrCoordinationResultDrift
		}
	} else {
		return nil, ErrCoordinationResultDrift
	}
	return messages, nil
}

func runtimeMessagesEndWithTerminal(messages, terminal string) bool {
	var transcript []json.RawMessage
	var terminalMessage json.RawMessage
	if json.Unmarshal([]byte(messages), &transcript) != nil || len(transcript) == 0 || json.Unmarshal([]byte(terminal), &terminalMessage) != nil {
		return false
	}
	return bytes.Equal(transcript[len(transcript)-1], terminalMessage)
}

func scanManagedAgentExecutionTransition(row rowScanner, scope internalmanagedagent.Scope, sessionID string, result *internalmanagedagent.ExecutionTransitionResult) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var turnVersion, executionGeneration, executionVersion int64
	var turnState, executionState string
	var resultDigest, errorCode *string
	if err := row.Scan(&result.Turn.TurnID, &turnState, &turnVersion, &result.Turn.CreatedAt, &result.Turn.UpdatedAt, &result.Execution.ExecutionID, &executionGeneration, &executionState, &resultDigest, &errorCode, &executionVersion, &result.Execution.CreatedAt, &result.Execution.UpdatedAt); err != nil {
		return mapMutationDatabaseError("managed agent execution transition", err)
	}
	result.Turn.Scope = scope
	result.Turn.SessionID = sessionID
	result.Turn.State = internalmanagedagent.TurnState(turnState)
	result.Turn.Version = uint64(turnVersion)
	result.Turn.ExecutionID = result.Execution.ExecutionID
	result.Execution.Scope = scope
	result.Execution.SessionID = sessionID
	result.Execution.TurnID = result.Turn.TurnID
	result.Execution.Generation = uint64(executionGeneration)
	result.Execution.State = internalmanagedagent.ExecutionState(executionState)
	if resultDigest != nil {
		result.Execution.ResultDigest = *resultDigest
	}
	if errorCode != nil {
		result.Execution.ErrorCode = *errorCode
	}
	result.Execution.Version = uint64(executionVersion)
	if result.Turn.TurnID == "" || turnVersion <= 0 || executionGeneration <= 0 || executionVersion <= 0 ||
		!validManagedAgentExecutionTransition(result.Turn.State, result.Execution.State, result.Execution.ResultDigest, result.Execution.ErrorCode) ||
		result.Execution.ExecutionID == "" || result.Turn.CreatedAt.IsZero() || result.Turn.UpdatedAt.IsZero() || result.Execution.CreatedAt.IsZero() || result.Execution.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent execution transition projection", ErrCoordinationResultDrift)
	}
	return nil
}

func validManagedAgentExecutionTransition(turn internalmanagedagent.TurnState, execution internalmanagedagent.ExecutionState, resultDigest, errorCode string) bool {
	switch {
	case turn == internalmanagedagent.TurnRunning && execution == internalmanagedagent.ExecutionRunning:
		return resultDigest == "" && errorCode == ""
	case turn == internalmanagedagent.TurnCompleted && execution == internalmanagedagent.ExecutionSucceeded:
		return validCoordinationDigest(resultDigest) && errorCode == ""
	case turn == internalmanagedagent.TurnFailed && execution == internalmanagedagent.ExecutionFailed:
		return resultDigest == "" && internalmanagedagent.ValidRuntimeErrorCode(errorCode)
	case turn == internalmanagedagent.TurnCancelled && execution == internalmanagedagent.ExecutionCancelled:
		return resultDigest == "" && errorCode == "cancelled"
	case turn == internalmanagedagent.TurnInterrupted && execution == internalmanagedagent.ExecutionCancelled:
		return resultDigest == "" && errorCode == "interrupted"
	default:
		return false
	}
}
