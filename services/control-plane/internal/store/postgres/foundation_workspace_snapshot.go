package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrWorkspaceSnapshotNotFound = errors.New("workspace snapshot was not found")
	ErrWorkspaceSnapshotConflict = errors.New("workspace snapshot conflicts with current authority")
)

type WorkspaceSnapshotCreateInput struct {
	Scope                       internalcoordination.FoundationScope
	SnapshotID, SourceSandboxID string
	ExpectedSandboxGeneration   int64
	Mutation                    internalcoordination.FoundationMutation
}

type WorkspaceSnapshot struct {
	Scope                                           internalcoordination.FoundationScope
	SnapshotID, SourceWorkspaceID, Backend          string
	ConsistencyMode, Status, OperationID            string
	StableErrorCode                                 *string
	SourceWorkspaceResourceVersion, ResourceVersion int64
	SizeBytes                                       *int64
	CreatedAt, UpdatedAt                            time.Time
	ObservedAt                                      *time.Time
}

type WorkspaceSnapshotPage struct {
	Snapshots      []WorkspaceSnapshot
	NextSnapshotID string
}

type workspaceSnapshotRow struct {
	TenantID, ProjectID, SnapshotID, SourceWorkspaceID string
	Backend, ConsistencyMode, Status, OperationID      string
	SourceWorkspaceResourceVersion, ResourceVersion    int64
	SizeBytes                                          *int64
	StableErrorCode                                    *string
	CreatedAt, UpdatedAt                               time.Time
	ObservedAt                                         *time.Time
}

type WorkspaceSnapshotClaim struct {
	TenantID, EventID, OperationID, ProjectID, SnapshotID string
	WorkspaceID, TargetID, TargetEndpoint, CredentialRef  string
	SourceVolumeName, ImageURI                            string
	DeliveryAttempts                                      int32
	ClaimExpiresAt                                        time.Time
	HolderID, HolderIncarnation, ClaimToken               string
}

type WorkspaceSnapshotClaimResult struct {
	DatabaseOutcome DatabaseOutcome
	Found           bool
	Claim           WorkspaceSnapshotClaim
}

type WorkspaceSnapshotSettlement struct {
	Claim                         WorkspaceSnapshotClaim
	Transition                    string
	SnapshotVolume, ContentDigest string
	SizeBytes                     int64
	StableErrorCode               string
	CleanupComplete               bool
	SubjectDigest, AuditFactID    string
}

type WorkspaceSnapshotSettlementResult struct {
	DatabaseOutcome             DatabaseOutcome
	OutboxState, OperationState string
	ResourceVersion             *int64
}

type WorkspaceSnapshotReapResult struct {
	DatabaseOutcome                DatabaseOutcome
	Found                          bool
	TenantID, EventID, OutboxState string
	DeliveryAttempts               int32
}

const workspaceSnapshotColumns = `snapshot.tenant_id, snapshot.project_uid, snapshot.snapshot_uid,
    snapshot.source_workspace_uid, snapshot.source_workspace_resource_version, snapshot.backend,
    snapshot.consistency_mode, snapshot.status, snapshot.operation_id, snapshot.resource_version,
    snapshot.size_bytes, snapshot.stable_error_code, snapshot.created_at, snapshot.updated_at, snapshot.observed_at`

var (
	acceptWorkspaceSnapshotSQL = `SELECT cloud_agents.accept_foundation_workspace_snapshot_v1($1,$2,$3,$4,$5,$6,$7)`
	getWorkspaceSnapshotSQL    = `SELECT ` + workspaceSnapshotColumns + ` FROM cloud_agents.workspace_snapshots AS snapshot
WHERE snapshot.tenant_id = cloud_agents.require_tenant_id() AND snapshot.project_uid = $1 AND snapshot.snapshot_uid = $2`
	workspaceSnapshotPageCursorSQL = `SELECT 1 FROM cloud_agents.workspace_snapshots
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND snapshot_uid = $2`
	listWorkspaceSnapshotsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(snapshot_row) ORDER BY snapshot_row.snapshot_uid), '[]'::jsonb)
FROM (SELECT snapshot.tenant_id, snapshot.project_uid, snapshot.snapshot_uid, snapshot.source_workspace_uid,
    snapshot.source_workspace_resource_version, snapshot.backend, snapshot.consistency_mode, snapshot.status,
    snapshot.operation_id, snapshot.resource_version, snapshot.size_bytes, snapshot.stable_error_code,
    snapshot.created_at, snapshot.updated_at, snapshot.observed_at
  FROM cloud_agents.workspace_snapshots AS snapshot
  WHERE snapshot.tenant_id = cloud_agents.require_tenant_id() AND snapshot.project_uid = $1 AND snapshot.snapshot_uid > $2
  ORDER BY snapshot.snapshot_uid LIMIT $3) AS snapshot_row`
	claimWorkspaceSnapshotSQL = `SELECT tenant_id,event_id,delivery_attempts,claim_expires_at,operation_id,
    project_uid,snapshot_uid,workspace_uid,target_uid,target_endpoint,credential_ref,source_volume_uid,image_uri
FROM cloud_agents.claim_foundation_workspace_snapshot_v1($1,$2,$3,$4,$5,$6)`
	renewWorkspaceSnapshotSQL  = `SELECT cloud_agents.renew_foundation_workspace_snapshot_claim_v1($1,$2,$3,$4,$5,$6,$7)`
	settleWorkspaceSnapshotSQL = `SELECT outbox_state,operation_state,resource_version
FROM cloud_agents.settle_foundation_workspace_snapshot_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`
	reapWorkspaceSnapshotSQL = `SELECT tenant_id,event_id,outbox_state,delivery_attempts
FROM cloud_agents.reap_foundation_workspace_snapshot_claim_v1($1,$2)`
)

func (input WorkspaceSnapshotCreateInput) valid(tenantID string) bool {
	return input.Scope.TenantID == tenantID && validMutationIdentifier(tenantID) && validMutationIdentifier(input.Scope.ProjectID) &&
		validMutationIdentifier(input.SnapshotID) && validMutationIdentifier(input.SourceSandboxID) &&
		input.ExpectedSandboxGeneration > 0 && input.ExpectedSandboxGeneration <= 9007199254740991 &&
		validMutationIdentifier(input.Mutation.RequestID) && validIdempotencyKey(input.Mutation.IdempotencyKey)
}

func workspaceSnapshotDigest(input WorkspaceSnapshotCreateInput) (string, error) {
	canonical, err := json.Marshal(map[string]any{"profileId": internalcoordination.WorkspaceSnapshotProfileID,
		"tenant": input.Scope.TenantID, "project": input.Scope.ProjectID, "snapshot": input.SnapshotID,
		"sourceSandbox": input.SourceSandboxID, "expectedSandboxGeneration": input.ExpectedSandboxGeneration})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (service *DurableCoordinationService) CreateWorkspaceSnapshot(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input WorkspaceSnapshotCreateInput) (WorkspaceSnapshot, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !input.valid(tenantID) {
		return WorkspaceSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := workspaceSnapshotDigest(input)
	if err != nil {
		return WorkspaceSnapshot{}, ErrCoordinationInvalidInput
	}
	var result WorkspaceSnapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			var operationID string
			if err := handle.transaction.queryRow(operationContext, acceptWorkspaceSnapshotSQL, input.Scope.ProjectID,
				input.SnapshotID, input.SourceSandboxID, input.ExpectedSandboxGeneration,
				subjectDigest, input.Mutation.IdempotencyKey, digest).Scan(&operationID); err != nil {
				return err
			}
			if !validMutationIdentifier(operationID) {
				return ErrCoordinationResultDrift
			}
			var row workspaceSnapshotRow
			if err := scanWorkspaceSnapshot(handle.transaction.queryRow(operationContext, getWorkspaceSnapshotSQL,
				input.Scope.ProjectID, input.SnapshotID), &row); err != nil {
				return err
			}
			var decodeErr error
			result, decodeErr = workspaceSnapshotFromRow(row, tenantID, input.Scope.ProjectID)
			return decodeErr
		})
	return result, mapWorkspaceSnapshotError(err)
}

func (service *DurableCoordinationService) GetWorkspaceSnapshot(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, snapshotID string) (WorkspaceSnapshot, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(snapshotID) {
		return WorkspaceSnapshot{}, ErrCoordinationInvalidInput
	}
	var row workspaceSnapshotRow
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false,
		func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
			err := scanWorkspaceSnapshot(handle.transaction.queryRow(operationContext, getWorkspaceSnapshotSQL, projectID, snapshotID), &row)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrWorkspaceSnapshotNotFound
			}
			return err
		})
	if err != nil {
		return WorkspaceSnapshot{}, mapWorkspaceSnapshotError(err)
	}
	return workspaceSnapshotFromRow(row, tenantID, projectID)
}

func (service *DurableCoordinationService) ListWorkspaceSnapshots(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterSnapshotID string, limit int) (WorkspaceSnapshotPage, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshotPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationPageInput(tenantID, projectID, afterSnapshotID, limit) {
		return WorkspaceSnapshotPage{}, ErrCoordinationInvalidInput
	}
	var result WorkspaceSnapshotPage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false,
		func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
			if err := validateRuntimeProfileCursor(operationContext, handle, workspaceSnapshotPageCursorSQL, projectID, afterSnapshotID); err != nil {
				return err
			}
			var raw []byte
			if err := handle.transaction.queryRow(operationContext, listWorkspaceSnapshotsSQL, projectID, afterSnapshotID, limit+1).Scan(&raw); err != nil {
				return err
			}
			var rows []workspaceSnapshotRow
			if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
				return ErrCoordinationResultDrift
			}
			for _, row := range rows {
				value, err := workspaceSnapshotFromRow(row, tenantID, projectID)
				if err != nil {
					return err
				}
				result.Snapshots = append(result.Snapshots, value)
			}
			if len(result.Snapshots) > limit {
				result.Snapshots = result.Snapshots[:limit]
				result.NextSnapshotID = result.Snapshots[len(result.Snapshots)-1].SnapshotID
			}
			return nil
		})
	return result, mapWorkspaceSnapshotError(err)
}

func scanWorkspaceSnapshot(row rowScanner, value *workspaceSnapshotRow) error {
	return row.Scan(&value.TenantID, &value.ProjectID, &value.SnapshotID, &value.SourceWorkspaceID,
		&value.SourceWorkspaceResourceVersion, &value.Backend, &value.ConsistencyMode, &value.Status,
		&value.OperationID, &value.ResourceVersion, &value.SizeBytes, &value.StableErrorCode,
		&value.CreatedAt, &value.UpdatedAt, &value.ObservedAt)
}

func workspaceSnapshotFromRow(row workspaceSnapshotRow, tenantID, projectID string) (WorkspaceSnapshot, error) {
	value := WorkspaceSnapshot{Scope: internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
		SnapshotID: row.SnapshotID, SourceWorkspaceID: row.SourceWorkspaceID,
		SourceWorkspaceResourceVersion: row.SourceWorkspaceResourceVersion, Backend: row.Backend,
		ConsistencyMode: row.ConsistencyMode, Status: row.Status, OperationID: row.OperationID,
		ResourceVersion: row.ResourceVersion, SizeBytes: row.SizeBytes, StableErrorCode: row.StableErrorCode,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ObservedAt: row.ObservedAt}
	if row.TenantID != tenantID || row.ProjectID != projectID || !validMutationIdentifier(value.SnapshotID) ||
		!validMutationIdentifier(value.SourceWorkspaceID) || !validMutationIdentifier(value.OperationID) ||
		value.SourceWorkspaceResourceVersion < 1 || value.ResourceVersion < 1 || value.Backend != "docker-volume-v1" || value.ConsistencyMode != "offline" ||
		(value.Status != "pending" && value.Status != "available" && value.Status != "unknown" && value.Status != "failed") || value.CreatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return WorkspaceSnapshot{}, ErrCoordinationResultDrift
	}
	return value, nil
}

func (service *DurableCoordinationService) ClaimWorkspaceSnapshot(ctx context.Context, holder, incarnation, token string, leaseSeconds int32, subjectDigest, auditFactID string) (WorkspaceSnapshotClaimResult, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshotClaimResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(holder) || !validMutationIdentifier(incarnation) || !validMutationIdentifier(token) || leaseSeconds < 1 || leaseSeconds > 60 || !validCoordinationDigest(subjectDigest) || !validMutationIdentifier(auditFactID) {
		return WorkspaceSnapshotClaimResult{}, ErrCoordinationInvalidInput
	}
	result := WorkspaceSnapshotClaimResult{Found: true}
	result.Claim.HolderID, result.Claim.HolderIncarnation, result.Claim.ClaimToken = holder, incarnation, token
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, claimWorkspaceSnapshotSQL, holder, incarnation, token, leaseSeconds, subjectDigest, auditFactID).Scan(
			&result.Claim.TenantID, &result.Claim.EventID, &result.Claim.DeliveryAttempts, &result.Claim.ClaimExpiresAt,
			&result.Claim.OperationID, &result.Claim.ProjectID, &result.Claim.SnapshotID, &result.Claim.WorkspaceID,
			&result.Claim.TargetID, &result.Claim.TargetEndpoint, &result.Claim.CredentialRef,
			&result.Claim.SourceVolumeName, &result.Claim.ImageURI)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return WorkspaceSnapshotClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return WorkspaceSnapshotClaimResult{}, mapWorkspaceSnapshotError(err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	if result.Found && !validWorkspaceSnapshotClaim(result.Claim) {
		return WorkspaceSnapshotClaimResult{}, ErrCoordinationResultDrift
	}
	return result, nil
}

func validWorkspaceSnapshotClaim(claim WorkspaceSnapshotClaim) bool {
	for _, value := range []string{claim.TenantID, claim.EventID, claim.OperationID, claim.ProjectID, claim.SnapshotID, claim.WorkspaceID, claim.TargetID, claim.CredentialRef, claim.SourceVolumeName} {
		if !validMutationIdentifier(value) {
			return false
		}
	}
	return claim.TargetEndpoint != "" && claim.ImageURI != "" && claim.DeliveryAttempts >= 1 && claim.DeliveryAttempts <= 8 && !claim.ClaimExpiresAt.IsZero() && validMutationIdentifier(claim.HolderID) && validMutationIdentifier(claim.HolderIncarnation) && validMutationIdentifier(claim.ClaimToken)
}

func (service *DurableCoordinationService) RenewWorkspaceSnapshot(ctx context.Context, claim WorkspaceSnapshotClaim, leaseSeconds int32) (time.Time, error) {
	if service == nil || service.runner == nil {
		return time.Time{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validWorkspaceSnapshotClaim(claim) || leaseSeconds < 1 || leaseSeconds > 60 {
		return time.Time{}, ErrCoordinationInvalidInput
	}
	var expiry time.Time
	err := service.runner.withTenantMutation(ctx, claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, renewWorkspaceSnapshotSQL, claim.TenantID, claim.EventID, claim.HolderID, claim.HolderIncarnation, claim.ClaimToken, claim.ClaimExpiresAt, leaseSeconds).Scan(&expiry)
	})
	return expiry, mapWorkspaceSnapshotError(err)
}

func (service *DurableCoordinationService) SettleWorkspaceSnapshot(ctx context.Context, input WorkspaceSnapshotSettlement) (WorkspaceSnapshotSettlementResult, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshotSettlementResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validWorkspaceSnapshotClaim(input.Claim) || (input.Transition != "succeeded" && input.Transition != "retry" && input.Transition != "failed") || !validCoordinationDigest(input.SubjectDigest) || !validMutationIdentifier(input.AuditFactID) {
		return WorkspaceSnapshotSettlementResult{}, ErrCoordinationInvalidInput
	}
	var volume, content, size, stable any
	if input.Transition == "succeeded" {
		volume = input.SnapshotVolume
		content = input.ContentDigest
		size = input.SizeBytes
	}
	if input.StableErrorCode != "" {
		stable = input.StableErrorCode
	}
	var result WorkspaceSnapshotSettlementResult
	err := service.runner.withTenantMutation(ctx, input.Claim.TenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, settleWorkspaceSnapshotSQL, input.Claim.TenantID, input.Claim.EventID,
			input.Claim.HolderID, input.Claim.HolderIncarnation, input.Claim.ClaimToken, input.Claim.ClaimExpiresAt,
			input.Transition, volume, content, size, stable, input.CleanupComplete, input.SubjectDigest, input.AuditFactID).
			Scan(&result.OutboxState, &result.OperationState, &result.ResourceVersion)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return WorkspaceSnapshotSettlementResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return WorkspaceSnapshotSettlementResult{}, mapWorkspaceSnapshotError(err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func (service *DurableCoordinationService) ReapWorkspaceSnapshot(ctx context.Context, subjectDigest, auditFactID string) (WorkspaceSnapshotReapResult, error) {
	if service == nil || service.runner == nil {
		return WorkspaceSnapshotReapResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validCoordinationDigest(subjectDigest) || !validMutationIdentifier(auditFactID) {
		return WorkspaceSnapshotReapResult{}, ErrCoordinationInvalidInput
	}
	result := WorkspaceSnapshotReapResult{Found: true}
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		err := handle.transaction.queryRow(ctx, reapWorkspaceSnapshotSQL, subjectDigest, auditFactID).Scan(&result.TenantID, &result.EventID, &result.OutboxState, &result.DeliveryAttempts)
		if errors.Is(err, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return err
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return WorkspaceSnapshotReapResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return WorkspaceSnapshotReapResult{}, mapWorkspaceSnapshotError(err)
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func mapWorkspaceSnapshotError(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Message {
		case "workspace snapshot source conflict", "workspace snapshot requires an offline source", "workspace snapshot source is unavailable", "workspace snapshot idempotency conflict":
			return ErrWorkspaceSnapshotConflict
		}
	}
	return mapRuntimeProfileError(err)
}
