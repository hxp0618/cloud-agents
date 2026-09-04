package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalstoragepolicy "github.com/hxp0618/cloud-agents/services/control-plane/internal/storagepolicy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type StoragePolicyPage struct {
	StoragePolicies     []internalstoragepolicy.Snapshot
	NextStoragePolicyID string
}

type StoragePolicyAuditPage struct {
	Events         []internalstoragepolicy.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type storagePolicyPageRow struct {
	TenantID                  string    `json:"tenant_id"`
	ProjectID                 string    `json:"project_uid"`
	PolicyID                  string    `json:"policy_uid"`
	PolicyName                string    `json:"policy_name"`
	UserSummary               string    `json:"user_summary"`
	WorkspaceType             string    `json:"workspace_type"`
	WorkspaceCapacityBytes    int64     `json:"workspace_capacity_bytes"`
	RetentionSeconds          int64     `json:"retention_seconds"`
	CleanupOnLeaseTermination bool      `json:"cleanup_on_lease_termination"`
	SnapshotBackendRef        string    `json:"snapshot_backend_ref"`
	ArtifactBackendRef        string    `json:"artifact_backend_ref"`
	AllowWorkspaceReuse       bool      `json:"allow_workspace_reuse"`
	ResourceVersion           int64     `json:"resource_version"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type storagePolicyAuditRow struct {
	TenantID              string    `json:"tenant_id"`
	ProjectID             string    `json:"project_uid"`
	PolicyID              string    `json:"policy_uid"`
	EventID               string    `json:"event_uid"`
	OperationID           string    `json:"operation_uid"`
	Actor                 string    `json:"subject_digest"`
	Action                string    `json:"action"`
	PolicyResourceVersion int64     `json:"policy_resource_version"`
	Result                string    `json:"result"`
	RequestID             string    `json:"request_id"`
	OccurredAt            time.Time `json:"occurred_at"`
}

const storagePolicyColumns = `policy_uid, policy_name, user_summary, workspace_type,
    workspace_capacity_bytes, retention_seconds, cleanup_on_lease_termination,
    COALESCE(snapshot_backend_ref, ''), COALESCE(artifact_backend_ref, ''),
    allow_workspace_reuse, resource_version, created_at, updated_at`

var (
	setStoragePolicySQL = `SELECT ` + storagePolicyColumns + `
FROM cloud_agents.set_storage_policy_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`
	getStoragePolicySQL = `SELECT ` + storagePolicyColumns + `
FROM cloud_agents.storage_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	storagePolicyPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.storage_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	listStoragePoliciesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(policy_row)
    ORDER BY policy_row.policy_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, policy_uid, policy_name, user_summary, workspace_type,
        workspace_capacity_bytes, retention_seconds, cleanup_on_lease_termination,
        COALESCE(snapshot_backend_ref, '') AS snapshot_backend_ref,
        COALESCE(artifact_backend_ref, '') AS artifact_backend_ref,
        allow_workspace_reuse, resource_version, created_at, updated_at
    FROM cloud_agents.storage_policies
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid > $2
    ORDER BY policy_uid
    LIMIT $3
) AS policy_row`
	storagePolicyAuditCursorIdentitySQL = `SELECT 1
FROM cloud_agents.storage_policy_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND policy_uid = $2 AND event_uid = $3 AND occurred_at = $4`
	storagePolicyAuditPolicyIdentitySQL = `SELECT 1
FROM cloud_agents.storage_policies
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2`
	listStoragePolicyAuditSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, policy_uid, event_uid, operation_uid,
        subject_digest, action, policy_resource_version, result, request_id, occurred_at
    FROM cloud_agents.storage_policy_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND policy_uid = $2
        AND ($3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4))
    ORDER BY occurred_at DESC, event_uid DESC
    LIMIT $5
) AS audit_row`
)

func (service *DurableCoordinationService) SetStoragePolicy(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalstoragepolicy.SetInput,
) (internalstoragepolicy.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalstoragepolicy.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalstoragepolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalstoragepolicy.MutationDigest(input)
	if err != nil {
		return internalstoragepolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalstoragepolicy.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		actor, ok := operation.Actor()
		if !ok {
			return authz.ErrOperationDenied
		}
		subjectDigest, digestErr := actor.Digest()
		if digestErr != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanStoragePolicy(handle.transaction.queryRow(ctx, setStoragePolicySQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.PolicyID, input.PolicyName,
					input.UserSummary, input.WorkspaceType, input.WorkspaceCapacityBytes, input.RetentionSeconds,
					input.CleanupOnLeaseTermination, input.SnapshotBackendRef, input.ArtifactBackendRef,
					input.AllowWorkspaceReuse, input.ExpectedResourceVersion, input.Mutation.IdempotencyKey,
					digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapStoragePolicyError(err)
}

func (service *DurableCoordinationService) GetStoragePolicy(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, policyID string,
) (internalstoragepolicy.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalstoragepolicy.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(policyID) {
		return internalstoragepolicy.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalstoragepolicy.Snapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				return scanStoragePolicy(handle.transaction.queryRow(readContext, getStoragePolicySQL, projectID, policyID),
					internalstoragepolicy.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
			})
		})
	})
	return result, mapStoragePolicyError(err)
}

func (service *DurableCoordinationService) ListStoragePolicies(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterPolicyID string, limit int,
) (StoragePolicyPage, error) {
	if service == nil || service.runner == nil {
		return StoragePolicyPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterPolicyID != "" && !validMutationIdentifier(afterPolicyID) || limit < 1 || limit > 200 {
		return StoragePolicyPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result StoragePolicyPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterPolicyID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, storagePolicyPageCursorIdentitySQL, projectID, afterPolicyID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listStoragePoliciesSQL, projectID, afterPolicyID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeStoragePolicyRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapStoragePolicyError(err)
}

func (service *DurableCoordinationService) ListStoragePolicyAuditEvents(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, policyID string,
	afterOccurredAt *time.Time, afterEventID string, limit int,
) (StoragePolicyAuditPage, error) {
	if service == nil || service.runner == nil {
		return StoragePolicyAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(policyID) || (afterOccurredAt == nil) != (afterEventID == "") ||
		afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return StoragePolicyAuditPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result StoragePolicyAuditPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var exists int
				if err := handle.transaction.queryRow(readContext, storagePolicyAuditPolicyIdentitySQL, projectID, policyID).Scan(&exists); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return ErrStoragePolicyNotFound
					}
					return err
				}
				if afterOccurredAt != nil {
					var exists int
					if err := handle.transaction.queryRow(readContext, storagePolicyAuditCursorIdentitySQL,
						projectID, policyID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listStoragePolicyAuditSQL,
					projectID, policyID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeStoragePolicyAuditRows(raw, tenantID, projectID, policyID, limit)
				return decodeErr
			})
		})
	})
	return result, mapStoragePolicyError(err)
}

func scanStoragePolicy(row rowScanner, scope internalstoragepolicy.Scope, result *internalstoragepolicy.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.PolicyID, &result.PolicyName, &result.UserSummary,
		&result.WorkspaceType, &result.WorkspaceCapacityBytes, &result.RetentionSeconds,
		&result.CleanupOnLeaseTermination, &result.SnapshotBackendRef, &result.ArtifactBackendRef,
		&result.AllowWorkspaceReuse, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: storage policy projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeStoragePolicyRows(raw []byte, tenantID, projectID string, limit int) (StoragePolicyPage, error) {
	var rows []storagePolicyPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return StoragePolicyPage{}, ErrCoordinationResultDrift
	}
	policies := make([]internalstoragepolicy.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := internalstoragepolicy.Snapshot{
			Scope:    internalstoragepolicy.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			PolicyID: row.PolicyID, PolicyName: row.PolicyName, UserSummary: row.UserSummary,
			WorkspaceType: row.WorkspaceType, WorkspaceCapacityBytes: row.WorkspaceCapacityBytes,
			RetentionSeconds: row.RetentionSeconds, CleanupOnLeaseTermination: row.CleanupOnLeaseTermination,
			SnapshotBackendRef: row.SnapshotBackendRef, ArtifactBackendRef: row.ArtifactBackendRef,
			AllowWorkspaceReuse: row.AllowWorkspaceReuse, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return StoragePolicyPage{}, ErrCoordinationResultDrift
		}
		policies = append(policies, snapshot)
	}
	result := StoragePolicyPage{StoragePolicies: policies}
	if len(policies) > limit {
		result.StoragePolicies = policies[:limit]
		result.NextStoragePolicyID = result.StoragePolicies[len(result.StoragePolicies)-1].PolicyID
	}
	return result, nil
}

func decodeStoragePolicyAuditRows(raw []byte, tenantID, projectID, policyID string, limit int) (StoragePolicyAuditPage, error) {
	var rows []storagePolicyAuditRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return StoragePolicyAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internalstoragepolicy.AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := internalstoragepolicy.AuditEvent{
			Scope:   internalstoragepolicy.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action,
			PolicyID: row.PolicyID, PolicyResourceVersion: row.PolicyResourceVersion,
			Result: row.Result, RequestID: row.RequestID, OccurredAt: row.OccurredAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.PolicyID != policyID || event.Validate() != nil {
			return StoragePolicyAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := StoragePolicyAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func mapStoragePolicyError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "storage policy idempotency conflict":
			return ErrStoragePolicyIdempotencyConflict
		case "storage policy resource version conflict":
			return ErrStoragePolicyResourceVersionConflict
		case "storage policy is referenced":
			return ErrStoragePolicyReferenced
		case "storage policy was not found":
			return ErrStoragePolicyNotFound
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrStoragePolicyNotFound
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("storage policy", err)
	}
}

var (
	ErrStoragePolicyNotFound                = errors.New("storage policy was not found")
	ErrStoragePolicyIdempotencyConflict     = errors.New("storage policy idempotency key conflicts")
	ErrStoragePolicyResourceVersionConflict = errors.New("storage policy resource version conflicts")
	ErrStoragePolicyReferenced              = errors.New("storage policy is referenced by an environment profile")
)
