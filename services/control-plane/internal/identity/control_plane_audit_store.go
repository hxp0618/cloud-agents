package identity

import (
	"context"
	"time"

	common "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/jackc/pgx/v5/pgtype"
)

func (store *AccountSecurityStore) ListControlPlaneAuditEvents(ctx context.Context, sessionDigest [32]byte, tenantID string, pageSize int, pageToken string) (api.ControlPlaneAuditPage, error) {
	if ctx == nil || pageSize < 1 || pageSize > 200 || common.ValidateIdentifier(tenantID, "/tenantId") != nil {
		return api.ControlPlaneAuditPage{}, ErrAccountSecurityInvalid
	}
	cursorScope := "control-plane:" + tenantID
	afterAt, afterID, err := store.auditCursorAfter(sessionDigest, cursorScope, pageToken)
	if err != nil {
		return api.ControlPlaneAuditPage{}, err
	}
	var at, id any
	if !afterAt.IsZero() {
		at, id = afterAt, afterID
	}
	databaseCtx, cancel := context.WithTimeout(ctx, databaseOperationTimeout)
	defer cancel()
	rows, err := store.pool.Query(databaseCtx, `SELECT id, action, actor_subject_kind,
actor_subject_issuer, actor_subject_value, actor_subject_digest, application,
resource_kind, resource_id, tenant_id, decision, reason_code, correlation_id, occurred_at
FROM cloud_agents_identity.list_control_plane_audit_facts($1,$2,$3,$4,$5)`, sessionDigest[:], tenantID, at, id, pageSize+1)
	if err != nil {
		return api.ControlPlaneAuditPage{}, accountSecurityError(ctx, err)
	}
	defer rows.Close()
	page := api.ControlPlaneAuditPage{Events: make([]api.ControlPlaneAuditEvent, 0, pageSize)}
	for rows.Next() {
		var event api.ControlPlaneAuditEvent
		var kind, issuer, subject, digest, application, correlation pgtype.Text
		var occurredAt time.Time
		if err := rows.Scan(&event.ID, &event.Action, &kind, &issuer, &subject, &digest, &application,
			&event.ResourceKind, &event.ResourceID, &event.TenantID, &event.Decision, &event.ReasonCode, &correlation, &occurredAt); err != nil {
			return api.ControlPlaneAuditPage{}, ErrUnavailable
		}
		if event.TenantID != tenantID || kind.Valid != issuer.Valid || kind.Valid != subject.Valid ||
			kind.Valid != digest.Valid || kind.Valid != application.Valid || kind.Valid != correlation.Valid {
			return api.ControlPlaneAuditPage{}, ErrUnavailable
		}
		if len(page.Events) == pageSize {
			last := page.Events[len(page.Events)-1]
			page.NextPageToken = store.auditCursor(sessionDigest, cursorScope, last.OccurredAt, last.ID)
			break
		}
		if kind.Valid {
			event.Actor = &common.SubjectRef{Kind: kind.String, Issuer: issuer.String, Subject: subject.String}
			actorDigest, err := event.Actor.Digest()
			if err != nil || actorDigest != digest.String {
				return api.ControlPlaneAuditPage{}, ErrUnavailable
			}
			event.Application = api.IdentityApplication(application.String)
			event.CorrelationID = correlation.String
		}
		event.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
		page.Events = append(page.Events, event)
	}
	if err := rows.Err(); err != nil {
		return api.ControlPlaneAuditPage{}, accountSecurityError(ctx, err)
	}
	if _, err := api.EncodeControlPlaneAuditPageJSON(page); err != nil {
		return api.ControlPlaneAuditPage{}, ErrUnavailable
	}
	return page, nil
}
