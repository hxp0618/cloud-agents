package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	claimFoundationWorkspaceVolumeUsageSQL = `SELECT tenant_id,project_uid,workspace_uid,volume_uid,
    target_uid,target_generation,endpoint,credential_ref,physical_volume_uid,
    measurement_generation,claim_expires_at
FROM cloud_agents.claim_foundation_workspace_volume_usage_v1($1,$2)`
	settleFoundationWorkspaceVolumeUsageSQL = `SELECT state,used_bytes,checkpointed_at,observed_at,stable_error_code
FROM cloud_agents.settle_foundation_workspace_volume_usage_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
)

type FoundationWorkspaceVolumeUsageClaim struct {
	TenantID, ProjectID, WorkspaceID, VolumeID string
	TargetID, TargetEndpoint, CredentialRef    string
	PhysicalVolumeID                           string
	TargetGeneration, MeasurementGeneration    int64
	ClaimExpiresAt                             time.Time
}

type FoundationWorkspaceVolumeUsageClaimResult struct {
	DatabaseOutcome DatabaseOutcome
	Found           bool
	Claim           FoundationWorkspaceVolumeUsageClaim
}

type FoundationWorkspaceVolumeUsageSettlement struct {
	Claim           FoundationWorkspaceVolumeUsageClaim
	Transition      string
	UsedBytes       *int64
	StableErrorCode string
}

type FoundationWorkspaceVolumeUsageSettlementResult struct {
	DatabaseOutcome DatabaseOutcome
	State           string
	UsedBytes       *int64
	CheckpointedAt  *time.Time
	ObservedAt      time.Time
	StableErrorCode *string
}

func (service *DurableCoordinationService) ClaimFoundationWorkspaceVolumeUsage(ctx context.Context, limit, leaseSeconds int32) (FoundationWorkspaceVolumeUsageClaimResult, error) {
	if service == nil || service.runner == nil {
		return FoundationWorkspaceVolumeUsageClaimResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || limit < 1 || limit > 500 || leaseSeconds < 1 || leaseSeconds > 60 {
		return FoundationWorkspaceVolumeUsageClaimResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationWorkspaceVolumeUsageClaimResult{Found: true}
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, claimFoundationWorkspaceVolumeUsageSQL, limit, leaseSeconds).Scan(
			&result.Claim.TenantID, &result.Claim.ProjectID, &result.Claim.WorkspaceID, &result.Claim.VolumeID,
			&result.Claim.TargetID, &result.Claim.TargetGeneration, &result.Claim.TargetEndpoint,
			&result.Claim.CredentialRef, &result.Claim.PhysicalVolumeID,
			&result.Claim.MeasurementGeneration, &result.Claim.ClaimExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationWorkspaceVolumeUsageClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationWorkspaceVolumeUsageClaimResult{}, mapCoordinationDatabaseError("claim foundation workspace volume usage", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.Found && !validFoundationWorkspaceVolumeUsageClaim(result.Claim) {
		return FoundationWorkspaceVolumeUsageClaimResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}

func validFoundationWorkspaceVolumeUsageClaim(claim FoundationWorkspaceVolumeUsageClaim) bool {
	for _, value := range []string{claim.TenantID, claim.ProjectID, claim.WorkspaceID, claim.VolumeID,
		claim.TargetID, claim.CredentialRef, claim.PhysicalVolumeID} {
		if !validMutationIdentifier(value) {
			return false
		}
	}
	return claim.TargetEndpoint != "" && claim.TargetGeneration > 0 && claim.MeasurementGeneration > 0 && !claim.ClaimExpiresAt.IsZero()
}

func (service *DurableCoordinationService) SettleFoundationWorkspaceVolumeUsage(ctx context.Context, input FoundationWorkspaceVolumeUsageSettlement) (FoundationWorkspaceVolumeUsageSettlementResult, error) {
	if service == nil || service.runner == nil {
		return FoundationWorkspaceVolumeUsageSettlementResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationWorkspaceVolumeUsageClaim(input.Claim) ||
		input.Transition != "ready" && input.Transition != "failed" ||
		input.Transition == "ready" && (input.UsedBytes == nil || *input.UsedBytes < 0 || *input.UsedBytes > 1<<60 || input.StableErrorCode != "") ||
		input.Transition == "failed" && (input.UsedBytes != nil || !validMutationIdentifier(input.StableErrorCode)) {
		return FoundationWorkspaceVolumeUsageSettlementResult{}, ErrCoordinationInvalidInput
	}
	var result FoundationWorkspaceVolumeUsageSettlementResult
	err := service.runner.withTenantMutation(ctx, input.Claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleFoundationWorkspaceVolumeUsageSQL,
			input.Claim.TenantID, input.Claim.ProjectID, input.Claim.WorkspaceID, input.Claim.VolumeID,
			input.Claim.TargetID, input.Claim.TargetGeneration, input.Claim.PhysicalVolumeID,
			input.Claim.MeasurementGeneration, input.Transition, input.UsedBytes, nullableString(input.StableErrorCode)).Scan(
			&result.State, &result.UsedBytes, &result.CheckpointedAt, &result.ObservedAt, &result.StableErrorCode)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationWorkspaceVolumeUsageSettlementResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationWorkspaceVolumeUsageSettlementResult{}, mapCoordinationDatabaseError("settle foundation workspace volume usage", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.State != input.Transition || result.ObservedAt.IsZero() ||
		result.State == "ready" && (result.UsedBytes == nil || result.CheckpointedAt == nil || result.StableErrorCode != nil) ||
		result.State == "failed" && result.StableErrorCode == nil {
		return FoundationWorkspaceVolumeUsageSettlementResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}
