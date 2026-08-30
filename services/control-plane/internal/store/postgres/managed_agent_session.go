package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentSessionNotFound = errors.New("managed agent session was not found")

type ManagedAgentSessionPage struct {
	Sessions      []internalmanagedagent.SessionSnapshot
	NextSessionID string
}

type managedAgentSessionPageRow struct {
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_uid"`
	SessionID       string    `json:"session_uid"`
	ProviderKind    string    `json:"provider_kind"`
	State           string    `json:"state"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	createManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_session_v1($1, $2, $3, $4, $5, $6)`
	closeManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.close_managed_agent_session_v1($1, $2, $3, $4, $5)`
	getManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
	getManagedAgentSessionForExecutionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at, provider_resume_cursor
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
	managedAgentSessionPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
	listManagedAgentSessionsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(managed_session)
    ORDER BY managed_session.session_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, session_uid, provider_kind, state,
        resource_version, created_at, updated_at
    FROM cloud_agents.managed_agent_sessions
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND session_uid > $2
    ORDER BY session_uid
    LIMIT $3
) AS managed_session`
)

// CreateManagedAgentSession persists one active Session. The database derives
// timestamps and state; the request digest is computed from the typed input.
func (service *DurableCoordinationService) CreateManagedAgentSession(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CreateSessionInput,
) (internalmanagedagent.SessionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.SessionSnapshot{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID {
		return internalmanagedagent.SessionSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.SessionCreateMutationDigest(input)
	if err != nil || ctx == nil {
		if err != nil {
			return internalmanagedagent.SessionSnapshot{}, ErrCoordinationInvalidInput
		}
		return internalmanagedagent.SessionSnapshot{}, ErrNilContext
	}
	var result internalmanagedagent.SessionSnapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				if err := scanManagedAgentSession(handle.transaction.queryRow(ctx, createManagedAgentSessionSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.ProviderKind,
					input.Mutation.IdempotencyKey, digest), input.Scope, &result, nil); err != nil {
					return err
				}
				return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{Scope: input.Scope, SessionID: result.SessionID, Operation: "session.create", Resource: internalmanagedagent.ResourceSession, MutationDigest: digest, Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceSession, To: string(result.State), Version: result.Version}}})
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

// CloseManagedAgentSession closes an active Session. Repeating the exact
// idempotency key returns the original closed snapshot.
// ponytail: no durable Turn rows yet; busy-session checks land with the Turn slice.
func (service *DurableCoordinationService) CloseManagedAgentSession(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CloseSessionInput,
) (internalmanagedagent.SessionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.SessionSnapshot{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID {
		return internalmanagedagent.SessionSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.SessionCloseMutationDigest(input)
	if err != nil || ctx == nil {
		if err != nil {
			return internalmanagedagent.SessionSnapshot{}, ErrCoordinationInvalidInput
		}
		return internalmanagedagent.SessionSnapshot{}, ErrNilContext
	}
	var result internalmanagedagent.SessionSnapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				err := scanManagedAgentSession(handle.transaction.queryRow(ctx, closeManagedAgentSessionSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.SessionID,
					input.Mutation.IdempotencyKey, digest), input.Scope, &result, nil)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentSessionNotFound
				}
				if err != nil {
					return err
				}
				return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{Scope: input.Scope, SessionID: result.SessionID, Operation: "session.close", Resource: internalmanagedagent.ResourceSession, MutationDigest: digest, Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceSession, From: string(internalmanagedagent.SessionActive), To: string(result.State), Version: result.Version}}})
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

// GetManagedAgentSession reads a tenant/project-bound Session through the
// existing read-only transaction and RBAC capability.
func (service *DurableCoordinationService) GetManagedAgentSession(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
) (internalmanagedagent.SessionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.SessionSnapshot{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || len(scope.TenantID) == 0 || len(scope.ProjectID) == 0 || len(sessionID) == 0 {
		return internalmanagedagent.SessionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.SessionSnapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(scope.TenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, func() error {
				err := scanManagedAgentSession(handle.transaction.queryRow(readContext, getManagedAgentSessionSQL, scope.ProjectID, sessionID), scope, &result, nil)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentSessionNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) ListManagedAgentSessions(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	afterSessionID string,
	limit int,
) (ManagedAgentSessionPage, error) {
	if service == nil || service.runner == nil {
		return ManagedAgentSessionPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterSessionID != "" && !validMutationIdentifier(afterSessionID) || limit < 1 || limit > 200 {
		return ManagedAgentSessionPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ManagedAgentSessionPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterSessionID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, managedAgentSessionPageCursorIdentitySQL, projectID, afterSessionID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("managed agent session page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedAgentSessionsSQL, projectID, afterSessionID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("managed agent sessions", err)
				}
				var err error
				result, err = decodeManagedAgentSessionPageRows(raw, tenantID, projectID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeManagedAgentSessionPageRows(raw []byte, tenantID, projectID string, limit int) (ManagedAgentSessionPage, error) {
	var rows []managedAgentSessionPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ManagedAgentSessionPage{}, ErrCoordinationResultDrift
	}
	sessions := make([]internalmanagedagent.SessionSnapshot, 0, len(rows))
	for _, row := range rows {
		state := internalmanagedagent.SessionState(row.State)
		if row.TenantID != tenantID || row.ProjectID != projectID || !validMutationIdentifier(row.SessionID) ||
			!validMutationIdentifier(row.ProviderKind) || state != internalmanagedagent.SessionActive && state != internalmanagedagent.SessionClosed ||
			row.ResourceVersion < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentSessionPage{}, ErrCoordinationResultDrift
		}
		sessions = append(sessions, internalmanagedagent.SessionSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: row.SessionID,
			ProviderKind: row.ProviderKind, State: state, Version: uint64(row.ResourceVersion), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
	}
	result := ManagedAgentSessionPage{Sessions: sessions}
	if len(sessions) > limit {
		result.Sessions = sessions[:limit]
		result.NextSessionID = result.Sessions[len(result.Sessions)-1].SessionID
	}
	return result, nil
}

// GetManagedAgentSessionForExecution reads the Session under the action
// authority already verified for the execute endpoint. It keeps execution
// from requiring a separate projects.get token merely to resolve provider.
func (service *DurableCoordinationService) GetManagedAgentSessionForExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
) (internalmanagedagent.RuntimeSessionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.RuntimeSessionSnapshot{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || len(scope.TenantID) == 0 || len(scope.ProjectID) == 0 || len(sessionID) == 0 {
		return internalmanagedagent.RuntimeSessionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.RuntimeSessionSnapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(scope.TenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, scope.TenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, func() error {
				var cursor *string
				err := scanManagedAgentSession(handle.transaction.queryRow(readContext, getManagedAgentSessionForExecutionSQL, scope.ProjectID, sessionID), scope, &result.SessionSnapshot, &cursor)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentSessionNotFound
				}
				if err != nil {
					return err
				}
				if cursor == nil {
					return nil
				}
				if err := internalmanagedagent.ValidateProviderResumeCursor(*cursor); err != nil || *cursor == "" {
					return fmt.Errorf("%w: managed agent provider resume cursor", ErrCoordinationResultDrift)
				}
				result.ProviderResumeCursor = *cursor
				return nil
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func scanManagedAgentSession(row rowScanner, scope internalmanagedagent.Scope, result *internalmanagedagent.SessionSnapshot, providerResumeCursor **string) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var state string
	var version int64
	targets := []any{&result.SessionID, &result.ProviderKind, &state, &version, &result.CreatedAt, &result.UpdatedAt}
	if providerResumeCursor != nil {
		targets = append(targets, providerResumeCursor)
	}
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent session", err)
	}
	result.Scope = scope
	result.State = internalmanagedagent.SessionState(state)
	if version <= 0 || result.SessionID == "" || (result.State != internalmanagedagent.SessionActive && result.State != internalmanagedagent.SessionClosed) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent session projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
}
