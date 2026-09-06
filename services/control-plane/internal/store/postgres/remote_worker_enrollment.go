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

type RemoteWorkerCertificateRotationAuthorization struct {
	Enrollment   internalremoteworker.Snapshot
	NeedsSigning bool
}

type remoteWorkerEnrollmentRow struct {
	TenantID               string     `json:"tenant_id"`
	ProjectID              string     `json:"project_uid"`
	EnrollmentID           string     `json:"enrollment_uid"`
	WorkerID               string     `json:"worker_uid"`
	WorkerName             string     `json:"worker_name"`
	State                  string     `json:"state"`
	ResourceVersion        int64      `json:"resource_version"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	ExpiresAt              time.Time  `json:"expires_at"`
	SecretClaimedAt        *time.Time `json:"secret_claimed_at"`
	EnrolledAt             *time.Time `json:"enrolled_at"`
	RevokedAt              *time.Time `json:"revoked_at"`
	IncarnationID          string     `json:"incarnation_uid"`
	SPIFFEID               string     `json:"certificate_spiffe_id"`
	CertificateSHA256      string     `json:"certificate_sha256"`
	CertificateChainPEM    string     `json:"certificate_chain_pem"`
	CertificateSerial      string     `json:"certificate_serial"`
	CertificateNotBefore   *time.Time `json:"certificate_not_before"`
	CertificateNotAfter    *time.Time `json:"certificate_not_after"`
	CertificateState       string     `json:"certificate_state"`
	CertificateRevokedAt   *time.Time `json:"certificate_revoked_at"`
	NodeResourceVersion    *int64     `json:"node_resource_version"`
	NodeGeneration         *int64     `json:"node_generation"`
	NodeObservedGeneration *int64     `json:"node_observed_generation"`
	NodeDesiredState       *string    `json:"node_desired_state"`
	NodeObservedState      *string    `json:"node_observed_state"`
	NodeHealthState        *string    `json:"node_health_state"`
	NodeWorkerVersion      *string    `json:"node_worker_version"`
	NodeOS                 *string    `json:"node_os"`
	NodeArchitecture       *string    `json:"node_architecture"`
	NodeKernelVersion      *string    `json:"node_kernel_version"`
	NodeCapabilities       []string   `json:"node_capabilities"`
	NodeCapacityCPUMillis  *int64     `json:"node_capacity_cpu_millis"`
	NodeCapacityMemory     *int64     `json:"node_capacity_memory_bytes"`
	NodeCapacityDisk       *int64     `json:"node_capacity_disk_bytes"`
	NodeFirstConnectedAt   *time.Time `json:"node_first_connected_at"`
	NodeLastHeartbeatAt    *time.Time `json:"node_last_heartbeat_at"`
	NodeHeartbeatExpiresAt *time.Time `json:"node_heartbeat_expires_at"`
}

type remoteWorkerEnrollmentAuditRow struct {
	TenantID           string    `json:"tenant_id"`
	ProjectID          string    `json:"project_uid"`
	EnrollmentID       string    `json:"enrollment_uid"`
	EventID            string    `json:"event_uid"`
	OperationID        string    `json:"operation_uid"`
	Actor              string    `json:"subject_digest"`
	Action             string    `json:"action"`
	ResourceGeneration int64     `json:"resource_generation"`
	Result             string    `json:"result"`
	RequestID          string    `json:"request_id"`
	StableErrorCode    string    `json:"stable_error_code"`
	OccurredAt         time.Time `json:"occurred_at"`
}

const remoteWorkerEnrollmentColumns = `enrollment_uid, worker_uid, worker_name,
	    CASE WHEN state IN ('pending', 'secret-issued') AND expires_at <= clock_timestamp()
	        THEN 'expired' ELSE state END,
	    resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at,
	    COALESCE(incarnation_uid, ''), COALESCE(certificate_spiffe_id, ''),
	    COALESCE(certificate_sha256, ''), COALESCE(certificate_chain_pem, ''),
	    COALESCE(certificate_serial, ''), certificate_not_before, certificate_not_after,
	    COALESCE(certificate_state, ''), certificate_revoked_at`

const remoteWorkerEnrollmentLegacyMutationColumns = `enrollment_uid, worker_uid, worker_name, state,
	resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at,
	''::text, ''::text, ''::text, ''::text, ''::text, NULL::timestamptz, NULL::timestamptz,
	''::text, NULL::timestamptz`

const remoteWorkerEnrollmentIssueMutationColumns = `enrollment_uid, worker_uid, worker_name, state,
	resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at,
	incarnation_uid, certificate_spiffe_id, certificate_sha256, certificate_chain_pem,
	certificate_serial, certificate_not_before, certificate_not_after, 'active'::text, NULL::timestamptz`

var (
	createRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentLegacyMutationColumns + `
	FROM cloud_agents.create_remote_worker_enrollment_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	claimRemoteWorkerEnrollmentSecretSQL = `SELECT ` + remoteWorkerEnrollmentLegacyMutationColumns + `
	FROM cloud_agents.claim_remote_worker_enrollment_secret_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	issueRemoteWorkerCertificateSQL = `SELECT ` + remoteWorkerEnrollmentIssueMutationColumns + `
FROM cloud_agents.issue_remote_worker_certificate_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`
	authenticateRemoteWorkerCertificateSQL      = `SELECT cloud_agents.authenticate_remote_worker_certificate_v1($1,$2,$3,$4,$5)`
	authorizeRemoteWorkerCertificateRotationSQL = `SELECT needs_signing, ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.authorize_remote_worker_certificate_rotation_v1($1,$2,$3,$4,$5,$6,$7,$8)`
	rotateRemoteWorkerCertificateSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
FROM cloud_agents.rotate_remote_worker_certificate_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`
	revokeRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `
	FROM cloud_agents.revoke_remote_worker_enrollment_or_certificate_v1($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	getRemoteWorkerEnrollmentSQL = `SELECT ` + remoteWorkerEnrollmentColumns + `, ` + remoteWorkerNodeAdminColumns + `
FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	remoteWorkerEnrollmentCursorSQL = `SELECT 1 FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	listRemoteWorkerEnrollmentsSQL = `SELECT COALESCE(jsonb_agg(to_jsonb(enrollment_row) ORDER BY enrollment_row.enrollment_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, enrollment_uid, worker_uid, worker_name,
        CASE WHEN state IN ('pending', 'secret-issued') AND expires_at <= clock_timestamp()
            THEN 'expired' ELSE state END AS state,
        resource_version, created_at, updated_at, expires_at, secret_claimed_at, enrolled_at, revoked_at,
        incarnation_uid, certificate_spiffe_id, certificate_sha256, certificate_chain_pem,
        certificate_serial, certificate_not_before, certificate_not_after,
        certificate_state, certificate_revoked_at,
        node_resource_version, node_generation, node_observed_generation,
        node_desired_state, node_observed_state,
        CASE WHEN node_last_heartbeat_at IS NULL THEN NULL
            WHEN certificate_state <> 'active' OR certificate_not_after <= clock_timestamp()
                OR node_heartbeat_expires_at <= clock_timestamp() THEN 'offline'
            WHEN node_last_heartbeat_at + interval '10 seconds' <= clock_timestamp() THEN 'degraded'
            ELSE 'online' END AS node_health_state,
        node_worker_version, node_os, node_architecture, node_kernel_version, node_capabilities,
        node_capacity_cpu_millis, node_capacity_memory_bytes, node_capacity_disk_bytes,
        node_first_connected_at, node_last_heartbeat_at, node_heartbeat_expires_at
    FROM cloud_agents.remote_worker_enrollments
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid > $2
    ORDER BY enrollment_uid LIMIT $3
) AS enrollment_row`
	remoteWorkerEnrollmentAuditIdentitySQL = `SELECT 1 FROM cloud_agents.remote_worker_enrollments
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2`
	remoteWorkerEnrollmentAuditCursorSQL = `SELECT 1 FROM (
    SELECT event_uid, occurred_at FROM cloud_agents.remote_worker_enrollment_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
    UNION ALL
    SELECT event_uid, occurred_at FROM cloud_agents.remote_worker_node_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
) AS event WHERE event_uid = $3 AND occurred_at = $4`
	listRemoteWorkerEnrollmentAuditSQL = `SELECT COALESCE(jsonb_agg(to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT * FROM (
        SELECT tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, subject_digest,
            action, enrollment_resource_version AS resource_generation, result, request_id,
            ''::text AS stable_error_code, occurred_at
        FROM cloud_agents.remote_worker_enrollment_activity
        WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
        UNION ALL
        SELECT tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, subject_digest,
            action, node_generation AS resource_generation, result, request_id,
            COALESCE(stable_error_code, '') AS stable_error_code, occurred_at
        FROM cloud_agents.remote_worker_node_activity
        WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND enrollment_uid = $2
    ) AS all_events
    WHERE $3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4)
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

func (service *DurableCoordinationService) IssueRemoteWorkerCertificate(ctx context.Context, tenantID string, input internalremoteworker.CertificatePersistenceInput) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.CertificateMutationDigest(input)
	if err != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Snapshot
	err = service.runner.withTenantMutationBinder(ctx, tenantID, func(handle *tenantReadHandle) error {
		certificate := input.Certificate
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(ctx, issueRemoteWorkerCertificateSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.ExpectedResourceVersion,
			input.ConfirmedEnrollmentID, input.SecretDigest, certificate.IncarnationID, certificate.SPIFFEID,
			certificate.CSRSHA256, certificate.Serial, certificate.SHA256, certificate.ChainPEM,
			certificate.NotBefore, certificate.NotAfter, input.ActorDigest, input.Mutation.IdempotencyKey,
			digest, input.Mutation.RequestID), input.Scope, &result)
	}, bindTenantSetting)
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) AuthenticateRemoteWorkerCertificate(ctx context.Context, tenantID, projectID, enrollmentID, secretDigest string, expectedResourceVersion int64) error {
	if service == nil || service.runner == nil {
		return ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(enrollmentID) || !coordinationDigestPattern.MatchString(secretDigest) || expectedResourceVersion < 1 {
		return ErrCoordinationInvalidInput
	}
	err := service.runner.withTenantReadBinder(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok || handle == nil {
			return ErrTenantCapabilityClosed
		}
		var authenticated bool
		if err := handle.transaction.queryRow(readContext, authenticateRemoteWorkerCertificateSQL, tenantID, projectID, enrollmentID, expectedResourceVersion, secretDigest).Scan(&authenticated); err != nil {
			return err
		}
		if !authenticated {
			return ErrRemoteWorkerEnrollmentAuthentication
		}
		return nil
	}, bindTenantSetting)
	return mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) AuthorizeRemoteWorkerCertificateRotation(ctx context.Context, tenantID string, input internalremoteworker.CertificateRotationRequest) (RemoteWorkerCertificateRotationAuthorization, error) {
	if service == nil || service.runner == nil {
		return RemoteWorkerCertificateRotationAuthorization{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return RemoteWorkerCertificateRotationAuthorization{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.CertificateRotationMutationDigest(input)
	if err != nil {
		return RemoteWorkerCertificateRotationAuthorization{}, ErrCoordinationInvalidInput
	}
	var result RemoteWorkerCertificateRotationAuthorization
	err = service.runner.withTenantReadBinder(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok || handle == nil {
			return ErrTenantCapabilityClosed
		}
		targets := append([]any{&result.NeedsSigning}, remoteWorkerEnrollmentScanTargets(&result.Enrollment)...)
		if err := handle.transaction.queryRow(readContext, authorizeRemoteWorkerCertificateRotationSQL,
			tenantID, input.Scope.ProjectID, input.EnrollmentID, input.ExpectedResourceVersion,
			input.ConfirmedEnrollmentID, input.PeerCertificateSHA256, input.Mutation.IdempotencyKey, digest).Scan(targets...); err != nil {
			return err
		}
		result.Enrollment.Scope = input.Scope
		result.Enrollment.TargetID = internalremoteworker.TargetID(input.Scope, result.Enrollment.EnrollmentID)
		if result.Enrollment.Validate() != nil {
			return fmt.Errorf("%w: remote worker rotation authorization projection", ErrCoordinationResultDrift)
		}
		return nil
	}, bindTenantSetting)
	return result, mapRemoteWorkerEnrollmentError(err)
}

func (service *DurableCoordinationService) RotateRemoteWorkerCertificate(ctx context.Context, tenantID string, input internalremoteworker.CertificateRotationPersistenceInput) (internalremoteworker.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalremoteworker.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalremoteworker.CertificateRotationMutationDigest(input.Request)
	if err != nil {
		return internalremoteworker.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalremoteworker.Snapshot
	err = service.runner.withTenantMutationBinder(ctx, tenantID, func(handle *tenantReadHandle) error {
		request, certificate := input.Request, input.Certificate
		return scanRemoteWorkerEnrollment(handle.transaction.queryRow(ctx, rotateRemoteWorkerCertificateSQL,
			tenantID, request.Scope.ProjectID, request.EnrollmentID, request.ExpectedResourceVersion,
			request.ConfirmedEnrollmentID, request.PeerCertificateSHA256, certificate.IncarnationID,
			certificate.SPIFFEID, certificate.CSRSHA256, certificate.Serial, certificate.SHA256,
			certificate.ChainPEM, certificate.NotBefore, certificate.NotAfter,
			request.Mutation.IdempotencyKey, digest, request.Mutation.RequestID), request.Scope, &result)
	}, bindTenantSetting)
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
		return scanRemoteWorkerEnrollmentWithNode(handle.transaction.queryRow(readContext, getRemoteWorkerEnrollmentSQL, projectID, enrollmentID), scope, &result)
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
	if err := row.Scan(remoteWorkerEnrollmentScanTargets(result)...); err != nil {
		return err
	}
	result.Scope = scope
	result.TargetID = internalremoteworker.TargetID(scope, result.EnrollmentID)
	if result.Validate() != nil {
		return fmt.Errorf("%w: remote worker enrollment projection", ErrCoordinationResultDrift)
	}
	return nil
}

func scanRemoteWorkerEnrollmentWithNode(row rowScanner, scope internalremoteworker.Scope, result *internalremoteworker.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var nodeRow remoteWorkerEnrollmentRow
	if err := row.Scan(append(remoteWorkerEnrollmentScanTargets(result), remoteWorkerNodeRowScanTargets(&nodeRow)...)...); err != nil {
		return err
	}
	result.Scope = scope
	result.TargetID = internalremoteworker.TargetID(scope, result.EnrollmentID)
	if err := assignRemoteWorkerNodeStatus(result, nodeRow); err != nil || result.Validate() != nil {
		return fmt.Errorf("%w: remote worker enrollment node projection", ErrCoordinationResultDrift)
	}
	return nil
}

func remoteWorkerEnrollmentScanTargets(result *internalremoteworker.Snapshot) []any {
	return []any{&result.EnrollmentID, &result.WorkerID, &result.WorkerName, &result.State, &result.ResourceVersion,
		&result.CreatedAt, &result.UpdatedAt, &result.ExpiresAt, &result.SecretClaimedAt, &result.EnrolledAt, &result.RevokedAt,
		&result.IncarnationID, &result.SPIFFEID, &result.CertificateSHA256, &result.CertificateChainPEM,
		&result.CertificateSerial, &result.CertificateNotBefore, &result.CertificateNotAfter,
		&result.CertificateState, &result.CertificateRevokedAt}
}

func decodeRemoteWorkerEnrollmentRows(raw []byte, tenantID, projectID string, limit int) (RemoteWorkerEnrollmentPage, error) {
	var rows []remoteWorkerEnrollmentRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return RemoteWorkerEnrollmentPage{}, ErrCoordinationResultDrift
	}
	values := make([]internalremoteworker.Snapshot, 0, len(rows))
	for _, row := range rows {
		scope := internalremoteworker.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID}
		value := internalremoteworker.Snapshot{Scope: scope, EnrollmentID: row.EnrollmentID,
			TargetID: internalremoteworker.TargetID(scope, row.EnrollmentID), WorkerID: row.WorkerID,
			WorkerName: row.WorkerName, State: row.State, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, ExpiresAt: row.ExpiresAt,
			SecretClaimedAt: row.SecretClaimedAt, EnrolledAt: row.EnrolledAt, RevokedAt: row.RevokedAt,
			IncarnationID: row.IncarnationID, SPIFFEID: row.SPIFFEID,
			CertificateSHA256: row.CertificateSHA256, CertificateChainPEM: row.CertificateChainPEM,
			CertificateSerial: row.CertificateSerial, CertificateNotBefore: row.CertificateNotBefore,
			CertificateNotAfter: row.CertificateNotAfter, CertificateState: row.CertificateState,
			CertificateRevokedAt: row.CertificateRevokedAt}
		if err := assignRemoteWorkerNodeStatus(&value, row); err != nil {
			return RemoteWorkerEnrollmentPage{}, err
		}
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
		event := internalremoteworker.AuditEvent{Scope: internalremoteworker.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID}, EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action, EnrollmentID: row.EnrollmentID, ResourceGeneration: row.ResourceGeneration, Result: row.Result, RequestID: row.RequestID, StableErrorCode: row.StableErrorCode, OccurredAt: row.OccurredAt}
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
		case "remote worker enrollment authentication failed":
			return ErrRemoteWorkerEnrollmentAuthentication
		case "remote worker certificate authentication failed":
			return ErrRemoteWorkerEnrollmentAuthentication
		case "remote worker certificate idempotency conflict":
			return ErrRemoteWorkerEnrollmentIdempotencyConflict
		case "remote worker certificate enrollment is unavailable":
			return ErrRemoteWorkerEnrollmentResourceVersionConflict
		case "remote worker enrollment revoke conflict":
			return ErrRemoteWorkerEnrollmentResourceVersionConflict
		case "remote worker certificate revoke conflict":
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
	ErrRemoteWorkerEnrollmentAuthentication          = errors.New("remote worker enrollment authentication failed")
)
