package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalprojectleasequota "github.com/hxp0618/cloud-agents/services/control-plane/internal/projectleasequota"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ProjectLeaseQuotaAuditPage struct {
	Events         []internalprojectleasequota.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type projectLeaseQuotaAuditRow struct {
	TenantID             string    `json:"tenant_id"`
	ProjectID            string    `json:"project_uid"`
	QuotaID              string    `json:"quota_uid"`
	EventID              string    `json:"event_uid"`
	OperationID          string    `json:"operation_uid"`
	Actor                string    `json:"subject_digest"`
	Action               string    `json:"action"`
	QuotaResourceVersion int64     `json:"quota_resource_version"`
	Result               string    `json:"result"`
	RequestID            string    `json:"request_id"`
	OccurredAt           time.Time `json:"occurred_at"`
}

const projectLeaseQuotaColumns = `quota_uid, quota_name, max_concurrent_leases,
    max_cpu_millis, max_memory_bytes, max_lease_ttl_seconds`

var (
	setProjectLeaseQuotaSQL = `SELECT ` + projectLeaseQuotaColumns + `,
    active_leases, used_cpu_millis, used_memory_bytes,
    resource_version, created_at, updated_at
FROM cloud_agents.set_project_lease_quota_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	getProjectLeaseQuotaSQL = `SELECT ` + projectLeaseQuotaColumns + `,
    (SELECT pg_catalog.count(*)
        FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.tenant_id = quota.tenant_id AND lease.project_uid = quota.project_uid
            AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')),
    (SELECT COALESCE(pg_catalog.sum(lease.cpu_limit_millis), 0)
        FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.tenant_id = quota.tenant_id AND lease.project_uid = quota.project_uid
            AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')),
    (SELECT COALESCE(pg_catalog.sum(lease.memory_limit_bytes), 0)
        FROM cloud_agents.managed_host_environment_leases AS lease
        WHERE lease.tenant_id = quota.tenant_id AND lease.project_uid = quota.project_uid
            AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')),
    resource_version, created_at, updated_at
FROM cloud_agents.project_lease_quotas AS quota
WHERE quota.tenant_id = cloud_agents.require_tenant_id() AND quota.project_uid = $1`
	projectLeaseQuotaAuditCursorIdentitySQL = `SELECT 1
FROM cloud_agents.project_lease_quota_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND event_uid = $2 AND occurred_at = $3`
	listProjectLeaseQuotaAuditSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, quota_uid, event_uid, operation_uid,
        subject_digest, action, quota_resource_version, result, request_id, occurred_at
    FROM cloud_agents.project_lease_quota_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
        AND ($2::timestamptz IS NULL OR (occurred_at, event_uid) < ($2, $3))
    ORDER BY occurred_at DESC, event_uid DESC
    LIMIT $4
) AS audit_row`
)

func (service *DurableCoordinationService) SetProjectLeaseQuota(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalprojectleasequota.SetInput,
) (internalprojectleasequota.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalprojectleasequota.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalprojectleasequota.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalprojectleasequota.MutationDigest(input)
	if err != nil {
		return internalprojectleasequota.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalprojectleasequota.Snapshot
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
				return scanProjectLeaseQuota(handle.transaction.queryRow(ctx, setProjectLeaseQuotaSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.ExpectedResourceVersion,
					input.MaxConcurrentLeases, input.MaxCPUMillis, input.MaxMemoryBytes,
					input.MaxLeaseTTLSeconds, input.Mutation.IdempotencyKey, digest,
					input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapProjectLeaseQuotaError(err)
}

func (service *DurableCoordinationService) GetProjectLeaseQuota(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID string,
) (internalprojectleasequota.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalprojectleasequota.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) {
		return internalprojectleasequota.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalprojectleasequota.Snapshot
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
				return scanProjectLeaseQuota(handle.transaction.queryRow(readContext, getProjectLeaseQuotaSQL, projectID),
					internalprojectleasequota.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
			})
		})
	})
	return result, mapProjectLeaseQuotaError(err)
}

func (service *DurableCoordinationService) ListProjectLeaseQuotaAuditEvents(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID string,
	afterOccurredAt *time.Time, afterEventID string, limit int,
) (ProjectLeaseQuotaAuditPage, error) {
	if service == nil || service.runner == nil {
		return ProjectLeaseQuotaAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		(afterOccurredAt == nil) != (afterEventID == "") ||
		afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return ProjectLeaseQuotaAuditPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ProjectLeaseQuotaAuditPage
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
				if afterOccurredAt != nil {
					var exists int
					if err := handle.transaction.queryRow(readContext, projectLeaseQuotaAuditCursorIdentitySQL,
						projectID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listProjectLeaseQuotaAuditSQL,
					projectID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeProjectLeaseQuotaAuditRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapProjectLeaseQuotaError(err)
}

func scanProjectLeaseQuota(row rowScanner, scope internalprojectleasequota.Scope, result *internalprojectleasequota.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.QuotaID, &result.QuotaName, &result.MaxConcurrentLeases,
		&result.MaxCPUMillis, &result.MaxMemoryBytes, &result.MaxLeaseTTLSeconds,
		&result.ActiveLeases, &result.UsedCPUMillis, &result.UsedMemoryBytes,
		&result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: project lease quota projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeProjectLeaseQuotaAuditRows(raw []byte, tenantID, projectID string, limit int) (ProjectLeaseQuotaAuditPage, error) {
	var rows []projectLeaseQuotaAuditRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ProjectLeaseQuotaAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internalprojectleasequota.AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := internalprojectleasequota.AuditEvent{
			Scope:   internalprojectleasequota.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action,
			QuotaID: row.QuotaID, QuotaResourceVersion: row.QuotaResourceVersion,
			Result: row.Result, RequestID: row.RequestID, OccurredAt: row.OccurredAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || event.Validate() != nil {
			return ProjectLeaseQuotaAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := ProjectLeaseQuotaAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func mapProjectLeaseQuotaError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "project lease quota idempotency conflict":
			return ErrProjectLeaseQuotaIdempotencyConflict
		case "project lease quota resource version conflict":
			return ErrProjectLeaseQuotaResourceVersionConflict
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrProjectLeaseQuotaNotFound
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("project lease quota", err)
	}
}

var (
	ErrProjectLeaseQuotaNotFound                = errors.New("project lease quota was not found")
	ErrProjectLeaseQuotaIdempotencyConflict     = errors.New("project lease quota idempotency key conflicts")
	ErrProjectLeaseQuotaResourceVersionConflict = errors.New("project lease quota resource version conflicts")
)
