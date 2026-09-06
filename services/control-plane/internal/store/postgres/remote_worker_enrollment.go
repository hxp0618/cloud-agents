package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalremoteworker "github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RemoteWorkerEnrollmentPage struct {
	Enrollments      []internalremoteworker.Snapshot
	NextEnrollmentID string
}

type RemoteWorkerEnrollmentAuditPage struct {
	Events         []internalremoteworker.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type remoteWorkerEnrollmentRow struct {
	TenantID        string     `json:"tenant_id"`
	ProjectID       string     `json:"project_uid"`
	EnrollmentID    string     `json:"enrollment_uid"`
	WorkerID        string     `json:"worker_uid"`
	WorkerName      string     `json:"worker_name"`
	State           string     `json:"state"`
	ResourceVersion int64      `json:"resource_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	SecretClaimedAt *time.Time `json:"secret_claimed_at"`
	EnrolledAt      *time.Time `json:"enrolled_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
}

type remoteWorkerEnrollmentAuditRow struct {
	TenantID                  string    `json:"tenant_id"`
	ProjectID                 string    `json:"project_uid"`
	EnrollmentID              string    `json:"enrollment_uid"`
	EventID                   string    `json:"event_uid"`
	OperationID               string    `json:"operation_uid"`
	Actor                     string    `json:"subject_digest"`
	Action                    string    `json:"action"`
	EnrollmentResourceVersion int64     `json:"enrollment_resource_version"`
	Result                    string    `json:"result"`
	RequestID                 string    `json:"request_id"`
	OccurredAt                time.Time `json:"occurred_at"`
}

const remoteWorkerEnrollmentColumns = `enrollment_uid, worker_uid, worker_name,
    CASE WHEN state IN ('pending', 'secret-issued') AND expires_at <= clock_timestamp()
        THEN 'expired' ELSE state END,
    resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at`

var (
	createRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.create_remote_worker_enrollment_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	claimRemoteWorkerEnrollmentSecretSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.claim_remote_worker_enrollment_secret_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	revokeRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.revoke_remote_worker_enrollment_v1($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	getRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	remoteWorkerEnrollmentCursorSQL = `SELECT 1 FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	listRemoteWorkerEnrollmentsSQL = `SELECT COALESCE(jsonb_agg(to_jsonb(enrollment_row) ORDER BY enrollment_row.enrollment_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, enrollment_uid, worker_uid, worker_name,
        CASE WHEN state IN ('pending', 'secret-issued') AND expires_at <= clock_timestamp()
            THEN 'expired' ELSE state END AS state,
        resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at
    FROM cloud_agents.remote_worker_enrollments
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid > $2
    ORDER BY enrollment_uid LIMIT $3
) AS enrollment_row`
	remoteWorkerEnrollmentAuditIdentitySQL = `SELECT 1 FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	remoteWorkerEnrollmentAuditCursorSQL = `SELECT 1 FROM cloud_agents.remote_worker_enrollment_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
  AND event_uid = $3 AND occurred_at = $4`
	listRemoteWorkerEnrollmentAuditSQL = `SELECT COALESCE(jsonb_agg(to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, subject_digest,
        action, enrollment_resource_version, result, request_id, occurred_at
    FROM cloud_agents.remote_worker_enrollment_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
      AND ($3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4))
    ORDER BY occurred_at DESC, event_uid DESC LIMIT $5
) AS audit_row`
)

func (service *DurableCoordinationService) CreateRemoteWorkerEnrollment(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalremoteworker.CreateInput) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.CreateMutationDigest(input)
	if err != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Snapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(operationContext, createRemoteWorkerEnrollmentSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.WorkerID, input.WorkerName, input.TTLSeconds,
			subjectDigest, input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID), input.Scope, &result)
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) ClaimRemoteWorkerEnrollmentSecret(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalremoteworker.TransitionInput, secretDigest string) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil || !coordinationDigestPattern.MatchString(secretDigest) {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.TransitionMutationDigest("claim-secret", input)
	if err != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Snapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(operationContext, claimRemoteWorkerEnrollmentSecretSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.ExpectedResourceVersion, input.ConfirmedEnrollmentID,
			secretDigest, subjectDigest, input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID), input.Scope, &result)
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) RevokeRemoteWorkerEnrollment(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalremoteworker.TransitionInput) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.TransitionMutationDigest("revoke", input)
	if err != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Snapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(operationContext, revokeRemoteWorkerEnrollmentSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.ExpectedResourceVersion, input.ConfirmedEnrollmentID,
			subjectDigest, input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID), input.Scope, &result)
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) GetRemoteWorkerEnrollment(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, enrollmentID string) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(enrollmentID) {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := internalremoteworker.Scope{TenantID: tenantID, ProjectID: projectID}
	var result internalremoteworker.Snapshot
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(readContext context.Context, handle *tenantReadHandle, _ string) error {
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(readContext, getRemoteWorkerEnrollmentSQL, projectID, enrollmentID), scope, &result)
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) ListRemoteWorkerEnrollments(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterEnrollmentID string, limit int) (RemoteWorkerEnrollmentPage, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerEnrollmentPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationPageInput(tenantID, projectID, afterEnrollmentID, limit) {
		return RemoteWorkerEnrollmentPage{}, ErrCoordinationInvalidInput
	}
	var result RemoteWorkerEnrollmentPage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(readContext context.Context, handle *tenantReadHandle, _ string) error {
		if err := validateRuntimeProfileCursor(readContext, handle, remoteWorkerEnrollmentCursorSQL, projectID, afterEnrollmentID); err != nil {
			return err
		}
		var raw []byte
		if err := handle.transaction.queryRow(readContext, listRemoteWorkerEnrollmentsSQL, projectID, afterEnrollmentID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var decodeErr error
		result, decodeErr = decodeRemoteWorkerEnrollmentRows(raw, tenantID, projectID, limit)
		return decodeErr
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) ListRemoteWorkerEnrollmentAuditEvents(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, enrollmentID string, afterOccurredAt *time.Time, afterEventID string, limit int) (RemoteWorkerEnrollmentAuditPage, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerEnrollmentAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(enrollmentID) ||
		(afterOccurredAt == nil) != (afterEventID == "") || afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return RemoteWorkerEnrollmentAuditPage{}, ErrCoordinationInvalidInput
	}
	var result RemoteWorkerEnrollmentAuditPage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(readContext context.Context, handle *tenantReadHandle, _ string) error {
		var exists int
		if err := handle.transaction.queryRow(readContext, remoteWorkerEnrollmentAuditIdentitySQL, projectID, enrollmentID).Scan(&exists); err != nil {
			return err
		}
		if afterOccurredAt != nil {
			if err := handle.transaction.queryRow(readContext, remoteWorkerEnrollmentAuditCursorSQL, projectID, enrollmentID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrCoordinationInvalidInput
				}
				return err
			}
		}
		var raw []byte
		if err := handle.transaction.queryRow(readContext, listRemoteWorkerEnrollmentAuditSQL, projectID, enrollmentID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var decodeErr error
		result, decodeErr = decodeRemoteWorkerEnrollmentAuditRows(raw, tenantID, projectID, enrollmentID, limit)
		return decodeErr
	})
	return result, mapRemoteWorkerEnrollmentError(err)
}

func scanRemoteWorkerEnrollment(row rowScanner, scope internalremoteworker.Scope, result *internalremoteworker.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.EnrollmentID, &result.WorkerID, &result.WorkerName, &result.State, &result.ResourceVersion,
		&result.CreatedAt, &result.UpdatedAt, &result.ExpiresAt, &result.SecretClaimedAt, &result.EnrolledAt, &result.RevokedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: remote worker enrollment projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeRemoteWorkerEnrollmentRows(raw []byte, tenantID, projectID string, limit int) (RemoteWorkerEnrollmentPage, error) {
	var rows []remoteWorkerEnrollmentRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return RemoteWorkerEnrollmentPage{}, ErrCoordinationResultDrift
	}
	values := make([]internalremoteworker.Snapshot, 0, len(rows))
	for _, row := range rows {
		value := internalremoteworker.Snapshot{Scope: internalremoteworker.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID}, EnrollmentID: row.EnrollmentID, WorkerID: row.WorkerID, WorkerName: row.WorkerName, State: row.State, ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ExpiresAt: row.ExpiresAt, SecretClaimedAt: row.SecretClaimedAt, EnrolledAt: row.EnrolledAt, RevokedAt: row.RevokedAt}
		if row.TenantID != tenantID || row.ProjectID != projectID || value.Validate() != nil {
			return RemoteWorkerEnrollmentPage{}, ErrCoordinationResultDrift
		}
		values = append(values, value)
	}
	result := RemoteWorkerEnrollmentPage{Enrollments: values}
	if len(values) > limit {
		result.Enrollments = values[:limit]
		result.NextEnrollmentID = result.Enrollments[len(result.Enrollments)-1].EnrollmentID
	}
	return result, nil
}

func decodeRemoteWorkerEnrollmentAuditRows(raw []byte, tenantID, projectID, enrollmentID string, limit int) (RemoteWorkerEnrollmentAuditPage, error) {
	var rows []remoteWorkerEnrollmentAuditRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return RemoteWorkerEnrollmentAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internalremoteworker.AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := internalremoteworker.AuditEvent{Scope: internalremoteworker.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID}, EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action, EnrollmentID: row.EnrollmentID, EnrollmentResourceVersion: row.EnrollmentResourceVersion, Result: row.Result, RequestID: row.RequestID, OccurredAt: row.OccurredAt}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.EnrollmentID != enrollmentID || event.Validate() != nil {
			return RemoteWorkerEnrollmentAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := RemoteWorkerEnrollmentAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func mapRemoteWorkerEnrollmentError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "remote worker enrollment idempotency conflict":
			return ErrRemoteWorkerEnrollmentIdempotencyConflict
		case "remote worker enrollment secret is unavailable":
			return ErrRemoteWorkerEnrollmentSecretUnavailable
		case "remote worker enrollment revoke conflict":
			return ErrRemoteWorkerEnrollmentResourceVersionConflict
		case "remote worker enrollment was not found":
			return ErrRemoteWorkerEnrollmentNotFound
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRemoteWorkerEnrollmentNotFound
	}
	if err == nil {
		return nil
	}
	return mapCoordinationDatabaseError("remote worker enrollment", err)
}

var (
	ErrRemoteWorkerEnrollmentNotFound                = errors.New("remote worker enrollment was not found")
	ErrRemoteWorkerEnrollmentIdempotencyConflict     = errors.New("remote worker enrollment idempotency key conflicts")
	ErrRemoteWorkerEnrollmentResourceVersionConflict = errors.New("remote worker enrollment resource version conflicts")
	ErrRemoteWorkerEnrollmentSecretUnavailable       = errors.New("remote worker enrollment secret is unavailable")
)
