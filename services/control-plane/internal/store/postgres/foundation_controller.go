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
    operation_id, operation_generation, action, project_uid, workspace_uid, workspace_name, volume_uid,
    physical_volume_uid, target_uid, target_generation, target_endpoint, credential_ref, sandbox_uid,
    sandbox_generation, image_uri, runtime_profile_uid, runtime_profile_version,
    workload_trust, isolation_runtime,
    cpu_millis, memory_bytes, spec_digest, runtime_uid, runtime_state, runtime_operation_uid,
	runtime_generation, runtime_spec_digest, ttl_seconds, expires_at
	, network_policy_uid, network_default_egress, network_allowed_egress, network_preview_enabled
FROM cloud_agents.claim_foundation_sandbox_v9($1,$2,$3,$4,$5,$6,$7,$8)`
	renewFoundationSandboxSQL  = `SELECT cloud_agents.renew_foundation_sandbox_claim_v1($1,$2,$3,$4,$5,$6,$7)`
	settleFoundationSandboxSQL = `SELECT outbox_state, operation_state, resource_version
FROM cloud_agents.settle_foundation_sandbox_v2($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
	reapFoundationSandboxSQL = `SELECT tenant_id, event_id, outbox_state, delivery_attempts
FROM cloud_agents.reap_foundation_sandbox_claim_v1($1,$2)`
	expireFoundationSandboxSQL = `SELECT tenant_id, project_uid, sandbox_uid, expired_at,
    operation_uid, sandbox_generation
FROM cloud_agents.expire_foundation_sandbox_v1($1,$2)`
)

type FoundationSandboxClaimInput struct {
	TargetKind, TargetID                    string
	HolderID, HolderIncarnation, ClaimToken string
	LeaseSeconds                            int32
	SubjectDigest, AuditFactID              string
}

type FoundationSandboxClaim struct {
	TenantID, EventID, OperationID, ProjectID, WorkspaceID, WorkspaceName string
	VolumeID, TargetID, TargetEndpoint, CredentialRef, SandboxID          string
	TargetKind                                                            string
	Action, ImageURI, SpecDigest, RuntimeState                            string
	PhysicalVolumeName, RuntimeID, RuntimeOperationID, RuntimeSpecDigest  *string
	RestoreSnapshotID, RestoreSourceWorkspaceID, RestoreSnapshotVolume    *string
	RestoreContentDigest                                                  *string
	RestoreSnapshotResourceVersion                                        *int64
	RuntimeGeneration                                                     *int64
	TTLSeconds                                                            *int32
	ExpiresAt                                                             *time.Time
	DeliveryAttempts                                                      int32
	OperationGeneration, TargetGeneration, SandboxGeneration              int64
	RuntimeProfileID                                                      string
	RuntimeProfileVersion                                                 int64
	WorkloadTrust, IsolationRuntime                                       string
	NetworkPolicyID, NetworkDefaultEgress                                 string
	NetworkAllowedEgress                                                  []string
	NetworkPreviewEnabled                                                 bool
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

type FoundationSandboxExpiryResult struct {
	DatabaseOutcome                DatabaseOutcome
	Found                          bool
	TenantID, ProjectID, SandboxID string
	OperationID                    string
	ExpiredAt                      time.Time
	SandboxGeneration              int64
}

func (service *DurableCoordinationService) ExpireFoundationSandbox(ctx context.Context, subjectDigest, auditFactID string) (FoundationSandboxExpiryResult, error) {
	if service == nil || service.runner == nil {
		return FoundationSandboxExpiryResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validCoordinationDigest(subjectDigest) || !validMutationIdentifier(auditFactID) {
		return FoundationSandboxExpiryResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxExpiryResult{Found: true}
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, expireFoundationSandboxSQL, subjectDigest, auditFactID).
			Scan(&result.TenantID, &result.ProjectID, &result.SandboxID, &result.ExpiredAt,
				&result.OperationID, &result.SandboxGeneration)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return FoundationSandboxExpiryResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return FoundationSandboxExpiryResult{}, mapCoordinationDatabaseError("expire foundation sandbox", err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.Found && (!validMutationIdentifier(result.TenantID) || !validMutationIdentifier(result.ProjectID) ||
		!validMutationIdentifier(result.SandboxID) || !validMutationIdentifier(result.OperationID) ||
		result.ExpiredAt.IsZero() || result.SandboxGeneration < 2) {
		return FoundationSandboxExpiryResult{}, ErrCoordinationResultDrift
	}
	return result, nil
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
	if ctx == nil || input.TargetKind != "docker" && input.TargetKind != "kubernetes" && input.TargetKind != "remote-worker" ||
		(input.TargetKind == "docker" || input.TargetKind == "kubernetes") && input.TargetID != "" || input.TargetKind == "remote-worker" && !validMutationIdentifier(input.TargetID) ||
		!validMutationIdentifier(input.HolderID) || !validMutationIdentifier(input.HolderIncarnation) ||
		!validMutationIdentifier(input.ClaimToken) || input.LeaseSeconds < 1 || input.LeaseSeconds > 60 ||
		!validCoordinationDigest(input.SubjectDigest) || !validMutationIdentifier(input.AuditFactID) {
		return FoundationSandboxClaimResult{}, ErrCoordinationInvalidInput
	}
	result := FoundationSandboxClaimResult{Found: true}
	claim := &result.Claim
	claim.TargetKind = input.TargetKind
	claim.HolderID, claim.HolderIncarnation, claim.ClaimToken = input.HolderID, input.HolderIncarnation, input.ClaimToken
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := scanFoundationSandboxClaim(handle.transaction.queryRow(ctx, claimFoundationSandboxSQL,
			input.TargetKind, input.TargetID, input.HolderID, input.HolderIncarnation, input.ClaimToken,
			input.LeaseSeconds, input.SubjectDigest, input.AuditFactID), claim)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		if err != nil {
			return err
		}
		var restoreTarget string
		err = handle.transaction.queryRow(ctx, `SELECT snapshot_uid,source_workspace_uid,source_physical_snapshot_uid,
    source_content_digest,snapshot_resource_version,target_uid FROM cloud_agents.workspace_snapshot_restores
WHERE tenant_id=cloud_agents.require_tenant_id() AND operation_id=$1 AND operation_generation=$2`,
			claim.OperationID, claim.OperationGeneration).Scan(&claim.RestoreSnapshotID, &claim.RestoreSourceWorkspaceID,
			&claim.RestoreSnapshotVolume, &claim.RestoreContentDigest, &claim.RestoreSnapshotResourceVersion, &restoreTarget)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil && restoreTarget != claim.TargetID {
			return ErrCoordinationResultDrift
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

func scanFoundationSandboxClaim(row rowScanner, claim *FoundationSandboxClaim) error {
	if row == nil || claim == nil {
		return ErrCoordinationResultDrift
	}
	return row.Scan(&claim.TenantID, &claim.EventID, &claim.DeliveryAttempts, &claim.ClaimExpiresAt,
		&claim.OperationID, &claim.OperationGeneration, &claim.Action, &claim.ProjectID, &claim.WorkspaceID,
		&claim.WorkspaceName, &claim.VolumeID, &claim.PhysicalVolumeName, &claim.TargetID, &claim.TargetGeneration,
		&claim.TargetEndpoint, &claim.CredentialRef, &claim.SandboxID, &claim.SandboxGeneration,
		&claim.ImageURI, &claim.RuntimeProfileID, &claim.RuntimeProfileVersion,
		&claim.WorkloadTrust, &claim.IsolationRuntime,
		&claim.CPUMillis, &claim.MemoryBytes, &claim.SpecDigest, &claim.RuntimeID,
		&claim.RuntimeState, &claim.RuntimeOperationID, &claim.RuntimeGeneration,
		&claim.RuntimeSpecDigest, &claim.TTLSeconds, &claim.ExpiresAt,
		&claim.NetworkPolicyID, &claim.NetworkDefaultEgress, &claim.NetworkAllowedEgress,
		&claim.NetworkPreviewEnabled)
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
		MemoryBytes: claim.MemoryBytes, NetworkPolicyID: claim.NetworkPolicyID,
		WorkloadTrust: claim.WorkloadTrust, IsolationRuntime: claim.IsolationRuntime,
		NetworkDefaultEgress: claim.NetworkDefaultEgress, NetworkAllowedEgress: claim.NetworkAllowedEgress,
		NetworkPreviewEnabled: claim.NetworkPreviewEnabled,
	})
	requestDigest := claim.SpecDigest
	requestErr := error(nil)
	if claim.Action == "sandbox.create" {
		if claim.RestoreSnapshotID != nil && claim.RestoreSourceWorkspaceID != nil && claim.RestoreSnapshotVolume != nil &&
			claim.RestoreContentDigest != nil && claim.RestoreSnapshotResourceVersion != nil && claim.TTLSeconds != nil {
			requestDigest, requestErr = workspaceSnapshotRestoreDigest(WorkspaceSnapshotRestoreInput{
				Scope:      coordination.FoundationScope{TenantID: claim.TenantID, ProjectID: claim.ProjectID},
				SnapshotID: *claim.RestoreSnapshotID, ExpectedSnapshotResourceVersion: *claim.RestoreSnapshotResourceVersion,
				WorkspaceID: claim.WorkspaceID, WorkspaceName: claim.WorkspaceName, SandboxID: claim.SandboxID,
				RuntimeProfileID: claim.RuntimeProfileID, RuntimeProfileVersion: claim.RuntimeProfileVersion,
				TTLSeconds: int64(*claim.TTLSeconds),
			})
		} else {
			input := coordination.FoundationSandboxCreateInput{
				Scope:       coordination.FoundationScope{TenantID: claim.TenantID, ProjectID: claim.ProjectID},
				WorkspaceID: claim.WorkspaceID, WorkspaceName: claim.WorkspaceName, SandboxID: claim.SandboxID,
				RuntimeProfileID: claim.RuntimeProfileID, RuntimeProfileVersion: claim.RuntimeProfileVersion,
				Mutation: coordination.FoundationMutation{RequestID: "claim", IdempotencyKey: "foundation-claim-check"},
			}
			if claim.TTLSeconds == nil {
				requestDigest, requestErr = coordination.FoundationSandboxCreateDigestV1(input)
			} else {
				input.TTLSeconds = int64(*claim.TTLSeconds)
				requestDigest, requestErr = coordination.FoundationSandboxCreateDigest(input)
			}
		}
	}
	endpoint, endpointErr := url.Parse(claim.TargetEndpoint)
	validVolume := claim.PhysicalVolumeName == nil || validMutationIdentifier(*claim.PhysicalVolumeName)
	validTTL := claim.TTLSeconds == nil && claim.ExpiresAt == nil || claim.TTLSeconds != nil && claim.ExpiresAt != nil &&
		*claim.TTLSeconds >= 60 && *claim.TTLSeconds <= 86400 && !claim.ExpiresAt.IsZero()
	validCurrentReceipt := claim.RuntimeID == nil && claim.RuntimeOperationID == nil &&
		claim.RuntimeGeneration == nil && claim.RuntimeSpecDigest == nil ||
		claim.RuntimeID != nil && claim.RuntimeOperationID != nil && claim.RuntimeGeneration != nil &&
			claim.RuntimeSpecDigest != nil && validMutationIdentifier(*claim.RuntimeID) &&
			*claim.RuntimeOperationID == claim.OperationID && *claim.RuntimeGeneration == claim.SandboxGeneration &&
			*claim.RuntimeSpecDigest == claim.SpecDigest
	validActionReceipt := claim.Action == "sandbox.create" && claim.SandboxGeneration == 1 && validVolume && validCurrentReceipt ||
		claim.Action == "sandbox.rebuild" && claim.SandboxGeneration > 1 && claim.PhysicalVolumeName != nil &&
			validVolume && validCurrentReceipt ||
		claim.Action == "sandbox.stop" && claim.PhysicalVolumeName != nil && claim.RuntimeID != nil &&
			claim.RuntimeOperationID != nil && claim.RuntimeGeneration != nil && claim.RuntimeSpecDigest != nil &&
			validMutationIdentifier(*claim.PhysicalVolumeName) && validMutationIdentifier(*claim.RuntimeID) &&
			validMutationIdentifier(*claim.RuntimeOperationID) && *claim.RuntimeGeneration > 0 &&
			*claim.RuntimeGeneration < claim.SandboxGeneration && validCoordinationDigest(*claim.RuntimeSpecDigest)
	validTarget := (claim.TargetKind == "docker" || claim.TargetKind == "kubernetes") && endpointErr == nil && endpoint.Scheme == "https" && endpoint.Host != "" ||
		claim.TargetKind == "remote-worker" && endpointErr == nil && endpoint.Scheme == "remote-worker" && endpoint.Host == claim.CredentialRef && endpoint.Path == ""
	restoreFields := 0
	for _, value := range []*string{claim.RestoreSnapshotID, claim.RestoreSourceWorkspaceID, claim.RestoreSnapshotVolume, claim.RestoreContentDigest} {
		if value != nil {
			restoreFields++
		}
	}
	if claim.RestoreSnapshotResourceVersion != nil {
		restoreFields++
	}
	validRestore := restoreFields == 0 || restoreFields == 5 && claim.Action == "sandbox.create" && claim.TargetKind == "docker" &&
		validMutationIdentifier(*claim.RestoreSnapshotID) && validMutationIdentifier(*claim.RestoreSourceWorkspaceID) &&
		validMutationIdentifier(*claim.RestoreSnapshotVolume) && validCoordinationDigest(*claim.RestoreContentDigest) &&
		*claim.RestoreSnapshotResourceVersion > 0
	return resolvedErr == nil && requestErr == nil && requestDigest == claim.SpecDigest && validTTL && validActionReceipt && validTarget && validRestore &&
		validMutationIdentifier(claim.EventID) && validMutationIdentifier(claim.OperationID) &&
		validMutationIdentifier(claim.CredentialRef) && claim.DeliveryAttempts >= 1 && claim.DeliveryAttempts <= 8 &&
		claim.OperationGeneration > 0 && claim.TargetGeneration > 0 && claim.SandboxGeneration > 0 &&
		!claim.ClaimExpiresAt.IsZero() && validMutationIdentifier(claim.HolderID) &&
		validMutationIdentifier(claim.HolderIncarnation) && validMutationIdentifier(claim.ClaimToken)
}
