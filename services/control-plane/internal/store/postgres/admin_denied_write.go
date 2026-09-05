package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

type AdminDeniedWrite struct {
	TenantID       string    `json:"tenant_id"`
	ProjectID      string    `json:"project_uid"`
	EventID        string    `json:"event_uid"`
	Actor          string    `json:"subject_digest"`
	Action         string    `json:"action"`
	ResourceID     string    `json:"resource_uid"`
	ProfileVersion int64     `json:"profile_version"`
	RequestID      string    `json:"request_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

type AdminDeniedWritePage struct {
	Events      []AdminDeniedWrite
	NextEventID string
}

func validAdminDeniedWrite(event AdminDeniedWrite) bool {
	if !validMutationIdentifier(event.TenantID) || !validMutationIdentifier(event.ProjectID) ||
		!validMutationIdentifier(event.RequestID) || event.ResourceID != "" && !validMutationIdentifier(event.ResourceID) {
		return false
	}
	switch event.Action {
	case "adminPublishEnvironmentProfile", "adminDisableEnvironmentProfile":
		return event.ResourceID != "" && event.ProfileVersion > 0 && event.ProfileVersion <= 2147483647
	case "adminUpgradeEnvironmentLease", "adminRollbackEnvironmentLease", "adminSetStoragePolicy", "adminSetNetworkPolicy",
		"adminProbeDeploymentTarget", "adminTransitionDeploymentTargetScheduling", "adminCleanupDeploymentTarget":
		return event.ResourceID != "" && event.ProfileVersion == 0
	case "adminRegisterWorkerRelease", "adminSetProjectLeaseQuota", "adminCreateEnvironmentProfile", "adminRegisterDeploymentTarget":
		return event.ResourceID == "" && event.ProfileVersion == 0
	}
	return false
}

// RecordAdminDeniedWrite consumes identity proof, NOT an RBAC mutation grant. This narrow
// authority can only append rejection metadata; requiring the rejected permission here
// would erase precisely the evidence it records. Identity and timestamps are never caller supplied.
func (service *DurableCoordinationService) RecordAdminDeniedWrite(ctx context.Context, principal *authn.VerifiedPrincipal, event AdminDeniedWrite) error {
	if service == nil || service.runner == nil {
		return ErrNilCoordinationRunner
	}
	if ctx == nil || !validAdminDeniedWrite(event) {
		return ErrCoordinationInvalidInput
	}
	return authn.ConsumeVerifiedPrincipal(principal, func(view authn.VerifiedPrincipalView) error {
		tenant, level, project, permission, ok := view.AuthorizationContext()
		kind, issuer, subject, actorOK := view.Actor()
		if !ok || !actorOK || !view.Check() || tenant != event.TenantID || level != "project" || project != event.ProjectID || permission != "projects.act" {
			return authz.ErrOperationDenied
		}
		actor := authz.SubjectRef{Kind: kind, Issuer: issuer, Subject: subject}
		digest, err := actor.Digest()
		if err != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenant, func(handle *tenantReadHandle) error {
			var id string
			return handle.transaction.queryRow(ctx, `SELECT cloud_agents.record_admin_denied_write_v1($1, $2, $3, $4, $5, $6, $7)`,
				project, "denied-"+rand.Text(), digest, event.Action, event.ResourceID, event.ProfileVersion, event.RequestID).Scan(&id)
		})
	})
}

func (service *DurableCoordinationService) ListAdminDeniedWrites(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterID string, limit int) (AdminDeniedWritePage, error) {
	if service == nil || service.runner == nil {
		return AdminDeniedWritePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || afterID != "" && !validMutationIdentifier(afterID) || limit < 1 || limit > 200 {
		return AdminDeniedWritePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	result := AdminDeniedWritePage{}
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, scope, "projects.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var after *time.Time
				if afterID != "" {
					if err := handle.transaction.queryRow(readContext, `SELECT occurred_at FROM cloud_agents.admin_denied_writes
                        WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND event_uid = $2`, projectID, afterID).Scan(&after); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(event)
                    ORDER BY event.occurred_at DESC, event.event_uid DESC), '[]'::jsonb)
                    FROM (SELECT tenant_id, project_uid, event_uid, subject_digest, action, resource_uid,
                        profile_version, request_id, occurred_at FROM cloud_agents.admin_denied_writes
                        WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
                        AND ($2::timestamptz IS NULL OR (occurred_at, event_uid) < ($2, $3))
                        ORDER BY occurred_at DESC, event_uid DESC LIMIT $4) AS event`, projectID, after, afterID, limit+1).Scan(&raw); err != nil {
					return err
				}
				if json.Unmarshal(raw, &result.Events) != nil || result.Events == nil || len(result.Events) > limit+1 {
					return ErrCoordinationResultDrift
				}
				for _, event := range result.Events {
					if event.TenantID != tenantID || event.ProjectID != projectID || !validAdminDeniedWrite(event) || !validMutationIdentifier(event.EventID) || event.OccurredAt.IsZero() {
						return ErrCoordinationResultDrift
					}
				}
				if len(result.Events) > limit {
					result.Events = result.Events[:limit]
					result.NextEventID = result.Events[limit-1].EventID
				}
				return nil
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}
