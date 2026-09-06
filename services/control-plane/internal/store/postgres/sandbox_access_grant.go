package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSandboxAccessGrantDenied   = errors.New("sandbox access grant denied")
	ErrSandboxPreviewPortNotFound = errors.New("sandbox Preview port was not found")
)

type SandboxAccessGrantIssueInput struct {
	Scope                           internalcoordination.FoundationScope
	SandboxID, GrantID, TokenDigest string
	ExpectedGeneration              int64
	TTLSeconds                      int32
	RequestDigest                   string
	Mutation                        internalcoordination.FoundationMutation
}

type SandboxAccessGrantRevokeInput struct {
	Scope                                       internalcoordination.FoundationScope
	SandboxID, GrantID, ConfirmedGrantID        string
	ExpectedGeneration, ExpectedResourceVersion int64
	RequestDigest                               string
	Mutation                                    internalcoordination.FoundationMutation
}

type SandboxAccessGrantSnapshot struct {
	Scope                                  internalcoordination.FoundationScope
	GrantID, SandboxID, AccessKind, Status string
	Generation, ResourceVersion            int64
	PTYSessionCount, FileAccessCount       int64
	FileFailureCount                       int64
	PreviewPorts                           []int32
	LastFileAction, LastFileStatus         *string
	LastFileErrorCode                      *string
	LastFileAccessAt                       *time.Time
	CreatedAt, UpdatedAt, ExpiresAt        time.Time
	RevokedAt                              *time.Time
}

type SandboxAccessGrantPage struct {
	Grants      []SandboxAccessGrantSnapshot
	NextGrantID string
}

type sandboxAccessGrantPageRow struct {
	TenantID          string     `json:"tenant_id"`
	ProjectID         string     `json:"project_uid"`
	GrantID           string     `json:"grant_uid"`
	SandboxID         string     `json:"sandbox_uid"`
	AccessKind        string     `json:"access_kind"`
	Status            string     `json:"status"`
	Generation        int64      `json:"sandbox_generation"`
	ResourceVersion   int64      `json:"resource_version"`
	PTYSessionCount   int64      `json:"pty_session_count"`
	FileAccessCount   int64      `json:"file_access_count"`
	FileFailureCount  int64      `json:"file_failure_count"`
	PreviewPorts      []int32    `json:"preview_ports"`
	LastFileAction    *string    `json:"last_file_action"`
	LastFileStatus    *string    `json:"last_file_status"`
	LastFileErrorCode *string    `json:"last_file_error_code"`
	LastFileAccessAt  *time.Time `json:"last_file_access_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	RevokedAt         *time.Time `json:"revoked_at"`
}

func validFileActivity(row sandboxAccessGrantPageRow) bool {
	if row.FileAccessCount == 0 {
		return row.LastFileAction == nil && row.LastFileStatus == nil && row.LastFileErrorCode == nil && row.LastFileAccessAt == nil
	}
	if row.LastFileAction == nil || row.LastFileStatus == nil || row.LastFileAccessAt == nil || row.LastFileAccessAt.IsZero() {
		return false
	}
	switch *row.LastFileAction {
	case "list", "read", "write", "delete":
	default:
		return false
	}
	switch *row.LastFileStatus {
	case "started", "succeeded":
		return row.LastFileErrorCode == nil
	case "failed":
		return row.LastFileErrorCode != nil && validMutationIdentifier(*row.LastFileErrorCode)
	default:
		return false
	}
}

type SandboxAccessGrantAuthority struct {
	Access    FoundationSandboxAccess
	GrantID   string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type SandboxPTYSessionAuthority struct {
	Grant     SandboxAccessGrantAuthority
	SessionID string
	CreatedAt time.Time
}

type SandboxPreviewPortAuthority struct {
	Grant        SandboxAccessGrantAuthority
	Port         int32
	RegisteredAt time.Time
}

type AccessGatewayStore struct{ runner *TenantTransactionRunner }

func NewAccessGatewayStore(pool *pgxpool.Pool) (*AccessGatewayStore, error) {
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	return &AccessGatewayStore{runner: runner}, nil
}

const accessGrantResultColumns = `grant_uid, sandbox_uid, sandbox_generation, access_kind,
    status, resource_version, created_at, updated_at, expires_at, revoked_at`

func (service *DurableCoordinationService) IssueSandboxAccessGrant(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input SandboxAccessGrantIssueInput,
) (SandboxAccessGrantSnapshot, error) {
	if service == nil || service.runner == nil {
		return SandboxAccessGrantSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Scope.TenantID != tenantID || !validMutationIdentifier(tenantID) ||
		!validMutationIdentifier(input.Scope.ProjectID) || !validMutationIdentifier(input.SandboxID) ||
		!validMutationIdentifier(input.GrantID) || !validCoordinationDigest(input.TokenDigest) ||
		!validCoordinationDigest(input.RequestDigest) || input.ExpectedGeneration < 1 ||
		input.ExpectedGeneration > 9007199254740991 || input.TTLSeconds < 60 || input.TTLSeconds > 900 ||
		!validMutationIdentifier(input.Mutation.RequestID) || !validMutationIdentifier(input.Mutation.IdempotencyKey) {
		return SandboxAccessGrantSnapshot{}, ErrCoordinationInvalidInput
	}
	var result SandboxAccessGrantSnapshot
	err := service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			return handle.transaction.queryRow(operationContext, `SELECT `+accessGrantResultColumns+`
FROM cloud_agents.issue_sandbox_access_grant_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				input.Scope.ProjectID, input.SandboxID, input.ExpectedGeneration, input.GrantID,
				input.TokenDigest, input.TTLSeconds, subjectDigest, input.Mutation.IdempotencyKey,
				input.RequestDigest, input.Mutation.RequestID,
			).Scan(&result.GrantID, &result.SandboxID, &result.Generation, &result.AccessKind,
				&result.Status, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				&result.ExpiresAt, &result.RevokedAt)
		})
	result.Scope = input.Scope
	return result, mapSandboxAccessGrantError(err)
}

func (service *DurableCoordinationService) RevokeSandboxAccessGrant(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input SandboxAccessGrantRevokeInput,
) (SandboxAccessGrantSnapshot, error) {
	if service == nil || service.runner == nil {
		return SandboxAccessGrantSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Scope.TenantID != tenantID || !validMutationIdentifier(tenantID) ||
		!validMutationIdentifier(input.Scope.ProjectID) || !validMutationIdentifier(input.SandboxID) ||
		!validMutationIdentifier(input.GrantID) || input.ConfirmedGrantID != input.GrantID ||
		input.ExpectedGeneration < 1 || input.ExpectedGeneration > 9007199254740991 ||
		input.ExpectedResourceVersion < 1 || !validCoordinationDigest(input.RequestDigest) ||
		!validMutationIdentifier(input.Mutation.RequestID) || !validMutationIdentifier(input.Mutation.IdempotencyKey) {
		return SandboxAccessGrantSnapshot{}, ErrCoordinationInvalidInput
	}
	var result SandboxAccessGrantSnapshot
	err := service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true,
		func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
			return handle.transaction.queryRow(operationContext, `SELECT `+accessGrantResultColumns+`
FROM cloud_agents.revoke_sandbox_access_grant_v1($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
				input.Scope.ProjectID, input.SandboxID, input.GrantID, input.ExpectedGeneration,
				input.ExpectedResourceVersion, input.ConfirmedGrantID, subjectDigest,
				input.Mutation.IdempotencyKey, input.RequestDigest, input.Mutation.RequestID,
			).Scan(&result.GrantID, &result.SandboxID, &result.Generation, &result.AccessKind,
				&result.Status, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				&result.ExpiresAt, &result.RevokedAt)
		})
	result.Scope = input.Scope
	return result, mapSandboxAccessGrantError(err)
}

func (service *DurableCoordinationService) ListSandboxAccessGrants(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal,
	projectID, sandboxID, afterGrantID string, limit int,
) (SandboxAccessGrantPage, error) {
	if service == nil || service.runner == nil {
		return SandboxAccessGrantPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(sandboxID) || afterGrantID != "" && !validMutationIdentifier(afterGrantID) ||
		limit < 1 || limit > 200 {
		return SandboxAccessGrantPage{}, ErrCoordinationInvalidInput
	}
	result := SandboxAccessGrantPage{}
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false,
		func(readContext context.Context, handle *tenantReadHandle, _ string) error {
			if afterGrantID != "" {
				var exists int
				if err := handle.transaction.queryRow(readContext, `SELECT 1 FROM cloud_agents.sandbox_access_grants
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND sandbox_uid = $2 AND grant_uid = $3`,
					projectID, sandboxID, afterGrantID).Scan(&exists); err != nil {
					return err
				}
			}
			var raw []byte
			if err := handle.transaction.queryRow(readContext, `SELECT COALESCE(jsonb_agg(to_jsonb(grant_row)
    ORDER BY grant_row.grant_uid), '[]'::jsonb)
FROM (
    SELECT access_grant.tenant_id, access_grant.project_uid, access_grant.grant_uid, access_grant.sandbox_uid,
        access_grant.sandbox_generation, access_grant.access_kind,
        CASE WHEN access_grant.status = 'revoked' THEN 'revoked'
             WHEN access_grant.expires_at <= clock_timestamp() THEN 'expired' ELSE 'active' END AS status,
        access_grant.resource_version, access_grant.created_at, access_grant.updated_at, access_grant.expires_at, access_grant.revoked_at,
        (SELECT count(*) FROM cloud_agents.sandbox_pty_sessions AS session
         WHERE session.tenant_id = access_grant.tenant_id AND session.project_uid = access_grant.project_uid
           AND session.grant_uid = access_grant.grant_uid) AS pty_session_count,
        file_counts.file_access_count, file_counts.file_failure_count,
        COALESCE((SELECT jsonb_agg(preview.port ORDER BY preview.port)
            FROM cloud_agents.sandbox_preview_ports AS preview
            WHERE preview.tenant_id = access_grant.tenant_id
              AND preview.project_uid = access_grant.project_uid
              AND preview.grant_uid = access_grant.grant_uid AND preview.revoked_at IS NULL
              AND access_grant.status = 'active' AND access_grant.expires_at > clock_timestamp()), '[]'::jsonb) AS preview_ports,
        latest_file.last_file_action, latest_file.last_file_status,
        latest_file.last_file_error_code, latest_file.last_file_access_at
    FROM cloud_agents.sandbox_access_grants AS access_grant
    CROSS JOIN LATERAL (
        SELECT count(*) FILTER (WHERE activity.action LIKE 'file_%') AS file_access_count,
            count(*) FILTER (WHERE activity.action LIKE 'file_%' AND activity.outcome = 'failed') AS file_failure_count
        FROM cloud_agents.sandbox_access_grant_activity AS activity
        WHERE activity.tenant_id = access_grant.tenant_id AND activity.project_uid = access_grant.project_uid
          AND activity.grant_uid = access_grant.grant_uid
    ) AS file_counts
    LEFT JOIN LATERAL (
        SELECT substring(activity.action FROM 6) AS last_file_action,
            activity.outcome AS last_file_status, activity.stable_error_code AS last_file_error_code,
            COALESCE(activity.completed_at, activity.occurred_at) AS last_file_access_at
        FROM cloud_agents.sandbox_access_grant_activity AS activity
        WHERE activity.tenant_id = access_grant.tenant_id AND activity.project_uid = access_grant.project_uid
          AND activity.grant_uid = access_grant.grant_uid AND activity.action LIKE 'file_%'
        ORDER BY activity.occurred_at DESC, activity.event_uid DESC LIMIT 1
    ) AS latest_file ON true
    WHERE access_grant.tenant_id = cloud_agents.require_tenant_id() AND access_grant.project_uid = $1
      AND access_grant.sandbox_uid = $2 AND access_grant.grant_uid > $3
    ORDER BY access_grant.grant_uid LIMIT $4
) AS grant_row`, projectID, sandboxID, afterGrantID, limit+1).Scan(&raw); err != nil {
				return err
			}
			var rows []sandboxAccessGrantPageRow
			if err := json.Unmarshal(raw, &rows); err != nil || rows == nil || len(rows) > limit+1 {
				return ErrCoordinationResultDrift
			}
			for _, row := range rows {
				if row.TenantID != tenantID || row.ProjectID != projectID || row.SandboxID != sandboxID ||
					!validMutationIdentifier(row.GrantID) || row.Generation < 1 || row.ResourceVersion < 1 ||
					row.AccessKind != "sandbox" || row.Status != "active" && row.Status != "expired" && row.Status != "revoked" ||
					row.PTYSessionCount < 0 || row.PTYSessionCount > 10000 || row.FileAccessCount < 0 ||
					row.FileFailureCount < 0 || row.FileFailureCount > row.FileAccessCount ||
					!validPreviewPorts(row.PreviewPorts) || !validFileActivity(row) {
					return ErrCoordinationResultDrift
				}
				result.Grants = append(result.Grants, SandboxAccessGrantSnapshot{
					Scope:   internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
					GrantID: row.GrantID, SandboxID: row.SandboxID, AccessKind: row.AccessKind,
					Status: row.Status, Generation: row.Generation, ResourceVersion: row.ResourceVersion,
					PTYSessionCount: row.PTYSessionCount, FileAccessCount: row.FileAccessCount,
					FileFailureCount: row.FileFailureCount, PreviewPorts: row.PreviewPorts,
					LastFileAction: row.LastFileAction,
					LastFileStatus: row.LastFileStatus, LastFileErrorCode: row.LastFileErrorCode,
					LastFileAccessAt: row.LastFileAccessAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
					ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt,
				})
			}
			if len(result.Grants) > limit {
				result.Grants = result.Grants[:limit]
				result.NextGrantID = result.Grants[limit-1].GrantID
			}
			return nil
		})
	return result, mapSandboxAccessGrantError(err)
}

func validPreviewPorts(ports []int32) bool {
	if ports == nil || len(ports) > 32 {
		return false
	}
	for index, port := range ports {
		if port < 1024 || port > 65535 || port == 44772 || index > 0 && ports[index-1] >= port {
			return false
		}
	}
	return true
}

func (store *AccessGatewayStore) ResolveGrant(ctx context.Context, tenantID, projectID, grantID, tokenDigest string) (SandboxAccessGrantAuthority, error) {
	return store.resolve(ctx, tenantID, projectID, grantID, tokenDigest)
}

func (store *AccessGatewayStore) ResolvePTYSession(ctx context.Context, tenantID, projectID, grantID, sessionID, tokenDigest string) (SandboxPTYSessionAuthority, error) {
	if !validMutationIdentifier(sessionID) {
		return SandboxPTYSessionAuthority{}, ErrCoordinationInvalidInput
	}
	authority, err := store.resolve(ctx, tenantID, projectID, grantID, tokenDigest)
	if err != nil {
		return SandboxPTYSessionAuthority{}, err
	}
	var createdAt time.Time
	err = store.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok {
			return ErrTenantCapabilityClosed
		}
		return handle.transaction.queryRow(readContext, `SELECT created_at FROM cloud_agents.sandbox_pty_sessions
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND grant_uid = $2
  AND session_uid = $3 AND deleted_at IS NULL`, projectID, grantID, sessionID).Scan(&createdAt)
	})
	if err != nil {
		return SandboxPTYSessionAuthority{}, mapSandboxAccessGrantError(err)
	}
	return SandboxPTYSessionAuthority{Grant: authority, SessionID: sessionID, CreatedAt: createdAt}, nil
}

func (store *AccessGatewayStore) RegisterPreviewPort(ctx context.Context, tenantID, projectID, grantID string, port int32, tokenDigest string) (SandboxPreviewPortAuthority, error) {
	if store == nil || store.runner == nil || ctx == nil || port < 1024 || port > 65535 || port == 44772 {
		return SandboxPreviewPortAuthority{}, ErrCoordinationInvalidInput
	}
	var registered SandboxPreviewPortAuthority
	var sandboxID string
	var generation int64
	err := store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, `SELECT port, sandbox_uid, sandbox_generation, registered_at
FROM cloud_agents.register_sandbox_preview_port_v1($1,$2,$3,$4)`, projectID, grantID, port, tokenDigest).
			Scan(&registered.Port, &sandboxID, &generation, &registered.RegisteredAt)
	})
	if err != nil {
		return SandboxPreviewPortAuthority{}, mapSandboxAccessGrantError(err)
	}
	registered.Grant, err = store.ResolveGrant(ctx, tenantID, projectID, grantID, tokenDigest)
	if err != nil || registered.Grant.Access.SandboxID != sandboxID || registered.Grant.Access.Generation != generation {
		return SandboxPreviewPortAuthority{}, ErrSandboxAccessGrantDenied
	}
	return registered, nil
}

func (store *AccessGatewayStore) ResolvePreviewPort(ctx context.Context, tenantID, projectID, grantID string, port int32, tokenDigest string) (SandboxPreviewPortAuthority, error) {
	if port < 1024 || port > 65535 || port == 44772 {
		return SandboxPreviewPortAuthority{}, ErrCoordinationInvalidInput
	}
	authority, err := store.resolve(ctx, tenantID, projectID, grantID, tokenDigest)
	if err != nil {
		return SandboxPreviewPortAuthority{}, err
	}
	var registeredAt time.Time
	err = store.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok {
			return ErrTenantCapabilityClosed
		}
		return handle.transaction.queryRow(readContext, `SELECT registered_at
FROM cloud_agents.sandbox_preview_ports
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND grant_uid = $2
  AND port = $3 AND revoked_at IS NULL`, projectID, grantID, port).Scan(&registeredAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return SandboxPreviewPortAuthority{}, ErrSandboxPreviewPortNotFound
	}
	if err != nil {
		return SandboxPreviewPortAuthority{}, mapSandboxAccessGrantError(err)
	}
	return SandboxPreviewPortAuthority{Grant: authority, Port: port, RegisteredAt: registeredAt}, nil
}

func (store *AccessGatewayStore) RevokePreviewPort(ctx context.Context, tenantID, projectID, grantID string, port int32, tokenDigest string) error {
	if _, err := store.ResolveGrant(ctx, tenantID, projectID, grantID, tokenDigest); err != nil {
		return err
	}
	return mapSandboxAccessGrantError(store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		var revokedAt time.Time
		return handle.transaction.queryRow(ctx, `SELECT cloud_agents.revoke_sandbox_preview_port_v1($1,$2,$3,$4)`,
			projectID, grantID, port, tokenDigest).Scan(&revokedAt)
	}))
}

func (store *AccessGatewayStore) resolve(ctx context.Context, tenantID, projectID, grantID, tokenDigest string) (SandboxAccessGrantAuthority, error) {
	if store == nil || store.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) ||
		!validMutationIdentifier(projectID) || !validMutationIdentifier(grantID) ||
		!validCoordinationDigest(tokenDigest) {
		return SandboxAccessGrantAuthority{}, ErrCoordinationInvalidInput
	}
	var result SandboxAccessGrantAuthority
	var tenant, project, sandboxID, workspaceID, runtimeID, runtimeOperationID, runtimeSpecDigest, credentialRef string
	var grantGeneration, generation, runtimeGeneration, observedGeneration int64
	var desiredState, observedState, runtimeState, volumeState, targetKind, operationState, cleanupPhase, specDigest string
	var writerReleased bool
	err := store.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok {
			return ErrTenantCapabilityClosed
		}
		return handle.transaction.queryRow(readContext, `SELECT access_grant.tenant_id, access_grant.project_uid,
    access_grant.grant_uid, access_grant.sandbox_uid, access_grant.sandbox_generation, access_grant.expires_at, access_grant.created_at,
    sandbox.workspace_uid, sandbox.generation, sandbox.observed_generation,
    sandbox.desired_state, sandbox.observed_state, sandbox.writer_released,
    sandbox.runtime_uid, sandbox.runtime_state, sandbox.runtime_operation_uid,
    sandbox.runtime_generation, sandbox.runtime_spec_digest, sandbox.spec_digest,
    volume.observed_state, target.target_kind, target.credential_ref,
    operation.state, operation.cleanup_phase
FROM cloud_agents.sandbox_access_grants AS access_grant
JOIN cloud_agents.sandbox_sessions AS sandbox
  ON sandbox.tenant_id = access_grant.tenant_id AND sandbox.project_uid = access_grant.project_uid
 AND sandbox.sandbox_uid = access_grant.sandbox_uid
JOIN cloud_agents.workspace_volumes AS volume
  ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
 AND volume.workspace_uid = sandbox.workspace_uid
JOIN cloud_agents.deployment_targets AS target
  ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
 AND target.target_uid = volume.target_uid
JOIN cloud_agents.platform_operations AS operation
  ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
 AND operation.operation_generation = sandbox.operation_generation
WHERE access_grant.tenant_id = cloud_agents.require_tenant_id() AND access_grant.project_uid = $1
  AND access_grant.grant_uid = $2 AND access_grant.token_digest = $3 AND access_grant.status = 'active'
  AND access_grant.expires_at > clock_timestamp()`, projectID, grantID, tokenDigest).Scan(
			&tenant, &project, &result.GrantID, &sandboxID, &grantGeneration, &result.ExpiresAt, &result.CreatedAt,
			&workspaceID, &generation, &observedGeneration, &desiredState, &observedState, &writerReleased,
			&runtimeID, &runtimeState, &runtimeOperationID, &runtimeGeneration, &runtimeSpecDigest, &specDigest,
			&volumeState, &targetKind, &credentialRef, &operationState, &cleanupPhase)
	})
	if err != nil {
		return SandboxAccessGrantAuthority{}, mapSandboxAccessGrantError(err)
	}
	if tenant != tenantID || project != projectID || result.GrantID != grantID || generation < 1 || grantGeneration != generation ||
		observedGeneration != generation || desiredState != "running" || observedState != "running" || writerReleased ||
		runtimeState != "Running" || runtimeGeneration != generation || runtimeSpecDigest != specDigest ||
		volumeState != "available" || targetKind != "docker" || operationState != "succeeded" || cleanupPhase != "complete" ||
		!validMutationIdentifier(sandboxID) || !validMutationIdentifier(workspaceID) || !validMutationIdentifier(runtimeID) ||
		!validMutationIdentifier(runtimeOperationID) || !validMutationIdentifier(credentialRef) || !validCoordinationDigest(runtimeSpecDigest) {
		return SandboxAccessGrantAuthority{}, ErrSandboxAccessGrantDenied
	}
	result.Access = FoundationSandboxAccess{
		Scope:       internalcoordination.FoundationScope{TenantID: tenant, ProjectID: project},
		WorkspaceID: workspaceID, SandboxID: sandboxID, RuntimeID: runtimeID,
		RuntimeOperationID: runtimeOperationID, RuntimeSpecDigest: runtimeSpecDigest,
		CredentialRef: credentialRef, Generation: generation, RuntimeGeneration: runtimeGeneration,
	}
	return result, nil
}

func (store *AccessGatewayStore) PersistPTYSession(ctx context.Context, tenantID, projectID, grantID, sessionID, tokenDigest string) (SandboxPTYSessionAuthority, error) {
	if store == nil || store.runner == nil || ctx == nil {
		return SandboxPTYSessionAuthority{}, ErrCoordinationInvalidInput
	}
	var sandboxID string
	var generation int64
	var createdAt time.Time
	err := store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, `SELECT session_uid, sandbox_uid, sandbox_generation, created_at
FROM cloud_agents.register_sandbox_pty_session_v1($1,$2,$3,$4)`, projectID, grantID, sessionID, tokenDigest).
			Scan(&sessionID, &sandboxID, &generation, &createdAt)
	})
	if err != nil {
		return SandboxPTYSessionAuthority{}, mapSandboxAccessGrantError(err)
	}
	authority, err := store.ResolveGrant(ctx, tenantID, projectID, grantID, tokenDigest)
	if err != nil || authority.Access.SandboxID != sandboxID || authority.Access.Generation != generation {
		return SandboxPTYSessionAuthority{}, ErrSandboxAccessGrantDenied
	}
	return SandboxPTYSessionAuthority{Grant: authority, SessionID: sessionID, CreatedAt: createdAt}, nil
}

func (store *AccessGatewayStore) MarkPTYSessionDeleted(ctx context.Context, tenantID, projectID, grantID, sessionID, tokenDigest string) error {
	if store == nil || store.runner == nil || ctx == nil {
		return ErrCoordinationInvalidInput
	}
	return mapSandboxAccessGrantError(store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		var deletedAt time.Time
		return handle.transaction.queryRow(ctx, `SELECT cloud_agents.delete_sandbox_pty_session_v1($1,$2,$3,$4)`,
			projectID, grantID, sessionID, tokenDigest).Scan(&deletedAt)
	}))
}

func validFileAction(action string) bool {
	switch action {
	case "list", "read", "write", "delete":
		return true
	default:
		return false
	}
}

func (store *AccessGatewayStore) StartFileAccess(ctx context.Context, tenantID, projectID, grantID, eventID, action, tokenDigest, requestID string) error {
	if store == nil || store.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) ||
		!validMutationIdentifier(projectID) || !validMutationIdentifier(grantID) ||
		!validMutationIdentifier(eventID) || !validFileAction(action) ||
		!validCoordinationDigest(tokenDigest) || !validMutationIdentifier(requestID) {
		return ErrCoordinationInvalidInput
	}
	return mapSandboxAccessGrantError(store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		var startedAt time.Time
		return handle.transaction.queryRow(ctx, `SELECT cloud_agents.start_sandbox_file_access_v1($1,$2,$3,$4,$5,$6)`,
			projectID, grantID, eventID, "file_"+action, tokenDigest, requestID).Scan(&startedAt)
	}))
}

func (store *AccessGatewayStore) CompleteFileAccess(ctx context.Context, tenantID, projectID, grantID, eventID, tokenDigest, outcome, stableErrorCode string, bytesTransferred int64) error {
	if store == nil || store.runner == nil || ctx == nil || !validMutationIdentifier(tenantID) ||
		!validMutationIdentifier(projectID) || !validMutationIdentifier(grantID) ||
		!validMutationIdentifier(eventID) || !validCoordinationDigest(tokenDigest) ||
		(outcome != "succeeded" && outcome != "failed") ||
		(outcome == "succeeded" && stableErrorCode != "") ||
		(outcome == "failed" && !validMutationIdentifier(stableErrorCode)) ||
		bytesTransferred < 0 || bytesTransferred > 16<<20 {
		return ErrCoordinationInvalidInput
	}
	var errorCode any
	if stableErrorCode != "" {
		errorCode = stableErrorCode
	}
	return mapSandboxAccessGrantError(store.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		var completedAt time.Time
		return handle.transaction.queryRow(ctx, `SELECT cloud_agents.complete_sandbox_file_access_v1($1,$2,$3,$4,$5,$6,$7)`,
			projectID, grantID, eventID, tokenDigest, outcome, errorCode, bytesTransferred).Scan(&completedAt)
	}))
}

func mapSandboxAccessGrantError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "28000":
			return ErrSandboxAccessGrantDenied
		case pgErr.Code == "23503" && pgErr.Message == "sandbox access grant was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		case pgErr.Code == "23503" && pgErr.Message == "foundation sandbox was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		case pgErr.Code == "23503" && pgErr.Message == "sandbox Preview port was not found":
			return ErrSandboxPreviewPortNotFound
		case pgErr.Code == "23505" || pgErr.Code == "54000":
			return internalcoordination.ErrFoundationSandboxConflict
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrSandboxAccessGrantDenied
	}
	return mapRuntimeProfileError(err)
}
