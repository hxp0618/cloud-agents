package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	claimFoundationSandboxNetworkUsageSQL = `SELECT tenant_id,project_uid,sandbox_uid,sandbox_generation,
    runtime_uid,target_uid,target_generation,endpoint,credential_ref,
    previous_received_bytes,previous_transmitted_bytes,measurement_generation,claim_expires_at
FROM cloud_agents.claim_foundation_sandbox_network_usage_v1($1)`
	settleFoundationSandboxNetworkUsageSQL = `SELECT state,received_bytes,transmitted_bytes,checkpointed_at,observed_at,stable_error_code
FROM cloud_agents.settle_foundation_sandbox_network_usage_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
)

type FoundationSandboxNetworkUsageClaim struct {
	TenantID, ProjectID, SandboxID, RuntimeID string
	SandboxGeneration                         int64
	TargetID, TargetEndpoint, CredentialRef   string
	TargetGeneration, MeasurementGeneration   int64
	PreviousReceivedBytes                     *int64
	PreviousTransmittedBytes                  *int64
	ClaimExpiresAt                            time.Time
}

type FoundationSandboxNetworkUsageClaimResult struct {
	DatabaseOutcome DatabaseOutcome
	Found           bool
	Claim           FoundationSandboxNetworkUsageClaim
}

type FoundationSandboxNetworkUsageSettlement struct {
	Claim                           FoundationSandboxNetworkUsageClaim
	Transition                      string
	ReceivedBytes, TransmittedBytes *int64
	StableErrorCode                 string
}

type FoundationSandboxNetworkUsageSettlementResult struct {
	DatabaseOutcome                 DatabaseOutcome
	State                           string
	ReceivedBytes, TransmittedBytes *int64
	CheckpointedAt                  *time.Time
	ObservedAt                      time.Time
	StableErrorCode                 *string
}

func (service *DurableCoordinationService) ClaimFoundationSandboxNetworkUsage(ctx context.Context, leaseSeconds int32) (FoundationSandboxNetworkUsageClaimResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxNetworkUsageClaimResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || leaseSeconds < 1 || leaseSeconds > 60 {
		return FoundationSandboxNetworkUsageClaimResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxNetworkUsageClaimResult{Found: true}
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, claimFoundationSandboxNetworkUsageSQL, leaseSeconds).Scan(
			&result.Claim.TenantID, &result.Claim.ProjectID, &result.Claim.SandboxID, &result.Claim.SandboxGeneration,
			&result.Claim.RuntimeID, &result.Claim.TargetID, &result.Claim.TargetGeneration,
			&result.Claim.TargetEndpoint, &result.Claim.CredentialRef,
			&result.Claim.PreviousReceivedBytes, &result.Claim.PreviousTransmittedBytes,
			&result.Claim.MeasurementGeneration, &result.Claim.ClaimExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxNetworkUsageClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxNetworkUsageClaimResult{}, mapCoordinationDatabaseError("claim foundation sandbox network usage", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.Found && !validFoundationSandboxNetworkUsageClaim(result.Claim) {
		return FoundationSandboxNetworkUsageClaimResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}

func validFoundationSandboxNetworkUsageClaim(claim FoundationSandboxNetworkUsageClaim) bool {
	for _, value := range []string{claim.TenantID, claim.ProjectID, claim.SandboxID, claim.RuntimeID, claim.TargetID, claim.CredentialRef} {
		if !validMutationIdentifier(value) {
			return false
		}
	}
	return claim.TargetEndpoint != "" && claim.SandboxGeneration > 0 && claim.TargetGeneration > 0 &&
		claim.MeasurementGeneration > 0 && !claim.ClaimExpiresAt.IsZero() &&
		(claim.PreviousReceivedBytes == nil) == (claim.PreviousTransmittedBytes == nil) &&
		(claim.PreviousReceivedBytes == nil || *claim.PreviousReceivedBytes >= 0 && *claim.PreviousTransmittedBytes >= 0)
}

func (service *DurableCoordinationService) SettleFoundationSandboxNetworkUsage(ctx context.Context, input FoundationSandboxNetworkUsageSettlement) (FoundationSandboxNetworkUsageSettlementResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxNetworkUsageSettlementResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationSandboxNetworkUsageClaim(input.Claim) ||
		input.Transition != "ready" && input.Transition != "failed" ||
		(input.ReceivedBytes == nil) != (input.TransmittedBytes == nil) ||
		input.Transition == "ready" && (input.ReceivedBytes == nil || *input.ReceivedBytes < 0 || *input.ReceivedBytes > 1<<60 || *input.TransmittedBytes < 0 || *input.TransmittedBytes > 1<<60 || input.StableErrorCode != "") ||
		input.Transition == "failed" && (input.ReceivedBytes != nil || !validMutationIdentifier(input.StableErrorCode)) {
		return FoundationSandboxNetworkUsageSettlementResult{}, ErrCoordinationInvalidInput
	}
	var result FoundationSandboxNetworkUsageSettlementResult
	err := service.runner.withTenantMutation(ctx, input.Claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleFoundationSandboxNetworkUsageSQL,
			input.Claim.TenantID, input.Claim.ProjectID, input.Claim.SandboxID, input.Claim.SandboxGeneration,
			input.Claim.RuntimeID, input.Claim.TargetID, input.Claim.TargetGeneration, input.Claim.MeasurementGeneration,
			input.Transition, input.ReceivedBytes, input.TransmittedBytes, nullableString(input.StableErrorCode)).Scan(
			&result.State, &result.ReceivedBytes, &result.TransmittedBytes, &result.CheckpointedAt,
			&result.ObservedAt, &result.StableErrorCode)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxNetworkUsageSettlementResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxNetworkUsageSettlementResult{}, mapCoordinationDatabaseError("settle foundation sandbox network usage", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.State != input.Transition || result.ObservedAt.IsZero() ||
		(result.ReceivedBytes == nil) != (result.TransmittedBytes == nil) ||
		result.State == "ready" && (result.ReceivedBytes == nil || result.CheckpointedAt == nil || result.StableErrorCode != nil) ||
		result.State == "failed" && result.StableErrorCode == nil {
		return FoundationSandboxNetworkUsageSettlementResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}
