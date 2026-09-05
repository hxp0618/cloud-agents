package postgres

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
)

const (
	claimFoundationSandboxSQL = `SELECT tenant_id, event_id, delivery_attempts, claim_expires_at,
    operation_id, operation_generation, project_uid, workspace_uid, workspace_name, volume_uid,
    target_uid, target_generation, target_endpoint, credential_ref, sandbox_uid,
    sandbox_generation, image_uri, runtime_profile_uid, runtime_profile_version,
    cpu_millis, memory_bytes, spec_digest
FROM cloud_agents.claim_foundation_sandbox_v1($1,$2,$3,$4,$5,$6)`
	renewFoundationSandboxSQL  = `SELECT cloud_agents.renew_foundation_sandbox_claim_v1($1,$2,$3,$4,$5,$6,$7)`
	settleFoundationSandboxSQL = `SELECT outbox_state, operation_state, resource_version
FROM cloud_agents.settle_foundation_sandbox_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
	reapFoundationSandboxSQL = `SELECT tenant_id, event_id, outbox_state, delivery_attempts
FROM cloud_agents.reap_foundation_sandbox_claim_v1($1,$2)`
)

type FoundationSandboxClaimInput struct {
	HolderID, HolderIncarnation, ClaimToken string
	LeaseSeconds                            int32
	SubjectDigest, AuditFactID              string
}

type FoundationSandboxClaim struct {
	TenantID, EventID, OperationID, ProjectID, WorkspaceID, WorkspaceName string
	VolumeID, TargetID, TargetEndpoint, CredentialRef, SandboxID          string
	ImageURI, SpecDigest                                                  string
	DeliveryAttempts                                                      int32
	OperationGeneration, TargetGeneration, SandboxGeneration              int64
	RuntimeProfileID                                                      string
	RuntimeProfileVersion                                                 int64
	CPUMillis, MemoryBytes                                                int64
	ClaimExpiresAt                                                        time.Time
	HolderID, HolderIncarnation, ClaimToken                               string
}

type FoundationSandboxClaimResult struct {
	DatabaseOutcome DatabaseOutcome
	Found           bool
	Claim           FoundationSandboxClaim
}

type FoundationSandboxSettlement struct {
	Claim                       FoundationSandboxClaim
	Transition                  string
	RuntimeID, RuntimeState     string
	VolumeName, StableErrorCode string
	CleanupComplete             bool
	SubjectDigest, AuditFactID  string
}

type FoundationSandboxSettlementResult struct {
	DatabaseOutcome             DatabaseOutcome
	OutboxState, OperationState string
	ResourceVersion             *int64
}

type FoundationSandboxReapResult struct {
	DatabaseOutcome  DatabaseOutcome
	Found            bool
	TenantID         string
	EventID          string
	OutboxState      string
	DeliveryAttempts int32
}

func (service *DurableCoordinationService) ReapFoundationSandbox(ctx context.Context, subjectDigest, auditFactID string) (FoundationSandboxReapResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxReapResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validCoordinationDigest(subjectDigest) || !validMutationIdentifier(auditFactID) {
		return FoundationSandboxReapResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxReapResult{Found: true}
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, reapFoundationSandboxSQL, subjectDigest, auditFactID).
			Scan(&result.TenantID, &result.EventID, &result.OutboxState, &result.DeliveryAttempts)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxReapResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxReapResult{}, mapCoordinationDatabaseError("reap foundation sandbox", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.Found && (!validMutationIdentifier(result.TenantID) || !validMutationIdentifier(result.EventID) ||
		(result.OutboxState != "pending" && result.OutboxState != "dead_letter") ||
		result.DeliveryAttempts < 1 || result.DeliveryAttempts > 8) {
		return FoundationSandboxReapResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}

func (service *DurableCoordinationService) ClaimFoundationSandbox(ctx context.Context, input FoundationSandboxClaimInput) (FoundationSandboxClaimResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxClaimResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(input.HolderID) || !validMutationIdentifier(input.HolderIncarnation) ||
		!validMutationIdentifier(input.ClaimToken) || input.LeaseSeconds < 1 || input.LeaseSeconds > 60 ||
		!validCoordinationDigest(input.SubjectDigest) || !validMutationIdentifier(input.AuditFactID) {
		return FoundationSandboxClaimResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxClaimResult{Found: true}
	claim := &result.Claim
	claim.HolderID, claim.HolderIncarnation, claim.ClaimToken = input.HolderID, input.HolderIncarnation, input.ClaimToken
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, claimFoundationSandboxSQL,
			input.HolderID, input.HolderIncarnation, input.ClaimToken, input.LeaseSeconds,
			input.SubjectDigest, input.AuditFactID,
		).Scan(&claim.TenantID, &claim.EventID, &claim.DeliveryAttempts, &claim.ClaimExpiresAt,
			&claim.OperationID, &claim.OperationGeneration, &claim.ProjectID, &claim.WorkspaceID,
			&claim.WorkspaceName, &claim.VolumeID, &claim.TargetID, &claim.TargetGeneration,
			&claim.TargetEndpoint, &claim.CredentialRef, &claim.SandboxID, &claim.SandboxGeneration,
			&claim.ImageURI, &claim.RuntimeProfileID, &claim.RuntimeProfileVersion,
			&claim.CPUMillis, &claim.MemoryBytes, &claim.SpecDigest)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxClaimResult{}, mapCoordinationDatabaseError("claim foundation sandbox", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if !result.Found {
		return result, nil
	}
	if !validFoundationSandboxClaim(*claim) {
		return FoundationSandboxClaimResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}

func (service *DurableCoordinationService) RenewFoundationSandbox(ctx context.Context, claim FoundationSandboxClaim, leaseSeconds int32) (time.Time, error) {
	if service == nil || service.runner == nil {
		return time.Time{}, ErrNilCoordinationRunner
	}
	if !validFoundationSandboxClaim(claim) || leaseSeconds < 1 || leaseSeconds > 60 {
		return time.Time{}, ErrCoordinationInvalidInput
	}
	var expiry time.Time
	err := service.runner.withTenantMutation(ctx, claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, renewFoundationSandboxSQL, claim.TenantID, claim.EventID,
			claim.HolderID, claim.HolderIncarnation, claim.ClaimToken, claim.ClaimExpiresAt, leaseSeconds).Scan(&expiry)
	})
	if err != nil {
		return time.Time{}, mapCoordinationDatabaseError("renew foundation sandbox", err)
	}
	return expiry, nil
}

func (service *DurableCoordinationService) SettleFoundationSandbox(ctx context.Context, input FoundationSandboxSettlement) (FoundationSandboxSettlementResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxSettlementResult{}, ErrNilCoordinationRunner
	}
	if !validFoundationSandboxClaim(input.Claim) || (input.Transition != "succeeded" && input.Transition != "retry" && input.Transition != "failed") ||
		!validCoordinationDigest(input.SubjectDigest) || !validMutationIdentifier(input.AuditFactID) {
		return FoundationSandboxSettlementResult{}, ErrCoordinationInvalidInput
	}
	var runtimeID, runtimeState, volumeName, stableError any
	if input.RuntimeID != "" {
		runtimeID = input.RuntimeID
	}
	if input.RuntimeState != "" {
		runtimeState = input.RuntimeState
	}
	if input.VolumeName != "" {
		volumeName = input.VolumeName
	}
	if input.StableErrorCode != "" {
		stableError = input.StableErrorCode
	}
	var result FoundationSandboxSettlementResult
	err := service.runner.withTenantMutation(ctx, input.Claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleFoundationSandboxSQL,
			input.Claim.TenantID, input.Claim.EventID, input.Claim.HolderID, input.Claim.HolderIncarnation,
			input.Claim.ClaimToken, input.Claim.ClaimExpiresAt, input.Transition, runtimeID, runtimeState,
			volumeName, stableError, input.CleanupComplete, input.SubjectDigest, input.AuditFactID,
		).Scan(&result.OutboxState, &result.OperationState, &result.ResourceVersion)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxSettlementResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxSettlementResult{}, mapCoordinationDatabaseError("settle foundation sandbox", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func validFoundationSandboxClaim(claim FoundationSandboxClaim) bool {
	_, resolvedErr := coordination.BindFoundationIntent(coordination.FoundationResolved{
		Tenant: claim.TenantID, Project: claim.ProjectID, Workspace: claim.WorkspaceID,
		WorkspaceName: claim.WorkspaceName, Volume: claim.VolumeID, Target: claim.TargetID,
		Sandbox: claim.SandboxID, ImageURI: claim.ImageURI, CPUMillis: claim.CPUMillis,
		MemoryBytes: claim.MemoryBytes,
	})
	requestDigest, requestErr := coordination.FoundationSandboxCreateDigest(coordination.FoundationSandboxCreateInput{
		Scope:       coordination.FoundationScope{TenantID: claim.TenantID, ProjectID: claim.ProjectID},
		WorkspaceID: claim.WorkspaceID, WorkspaceName: claim.WorkspaceName, SandboxID: claim.SandboxID,
		RuntimeProfileID: claim.RuntimeProfileID, RuntimeProfileVersion: claim.RuntimeProfileVersion,
		Mutation: coordination.FoundationMutation{RequestID: "claim", IdempotencyKey: "foundation-claim-check"},
	})
	endpoint, endpointErr := url.Parse(claim.TargetEndpoint)
	return resolvedErr == nil && requestErr == nil && requestDigest == claim.SpecDigest &&
		endpointErr == nil && endpoint.Scheme == "https" && endpoint.Host != "" &&
		validMutationIdentifier(claim.EventID) && validMutationIdentifier(claim.OperationID) &&
		validMutationIdentifier(claim.CredentialRef) && claim.DeliveryAttempts >= 1 && claim.DeliveryAttempts <= 8 &&
		claim.OperationGeneration > 0 && claim.TargetGeneration > 0 && claim.SandboxGeneration > 0 &&
		!claim.ClaimExpiresAt.IsZero() && validMutationIdentifier(claim.HolderID) &&
		validMutationIdentifier(claim.HolderIncarnation) && validMutationIdentifier(claim.ClaimToken)
}
