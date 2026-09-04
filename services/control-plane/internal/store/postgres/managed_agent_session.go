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
	TenantID                  string    `json:"tenant_id"`
	ProjectID                 string    `json:"project_uid"`
	SessionID                 string    `json:"session_uid"`
	ProviderKind              string    `json:"provider_kind"`
	EnvironmentLeaseID        *string   `json:"environment_lease_uid"`
	EnvironmentGeneration     *int64    `json:"environment_generation"`
	EnvironmentProfileID      *string   `json:"environment_profile_uid"`
	EnvironmentProfileVersion *int64    `json:"environment_profile_version"`
	State                     string    `json:"state"`
	ResourceVersion           int64     `json:"resource_version"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

const (
	createManagedAgentSessionSQL = `SELECT session_uid, provider_kind, environment_lease_uid, environment_generation,
    environment_profile_uid, environment_profile_version, state, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_session_v3($1, $2, $3, $4, $5, $6, $7)`
	closeManagedAgentSessionSQL = `SELECT transition.session_uid, transition.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    environment.environment_profile_uid, environment.environment_profile_version,
    transition.state, transition.resource_version, transition.created_at, transition.updated_at
FROM cloud_agents.close_managed_agent_session_v1($1, $2, $3, $4, $5) AS transition
JOIN cloud_agents.managed_agent_sessions AS session
    ON session.tenant_id = cloud_agents.require_tenant_id()
    AND session.project_uid = $2 AND session.session_uid = transition.session_uid
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid`
	getManagedAgentSessionSQL = `SELECT session.session_uid, session.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    environment.environment_profile_uid, environment.environment_profile_version,
    session.state, session.resource_version, session.created_at, session.updated_at
FROM cloud_agents.managed_agent_sessions AS session
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid
WHERE session.tenant_id = cloud_agents.require_tenant_id()
    AND session.project_uid = $1 AND session.session_uid = $2`
	getManagedAgentSessionForExecutionSQL = `SELECT session.session_uid, session.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    environment.environment_profile_uid, environment.environment_profile_version,
    session.state, session.resource_version, session.created_at, session.updated_at, session.provider_resume_cursor,
    COALESCE(environment.worker_endpoint, ''), COALESCE(environment.worker_spiffe_id, ''),
    COALESCE(environment.worker_server_name, ''),
    COALESCE(environment.generation = session.environment_generation
        AND environment.desired_phase = 'active' AND environment.observed_phase = 'ready'
        AND environment.cleanup_phase = 'none' AND environment.expires_at > pg_catalog.transaction_timestamp(), false)
FROM cloud_agents.managed_agent_sessions AS session
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid
WHERE session.tenant_id = cloud_agents.require_tenant_id()
    AND session.project_uid = $1 AND session.session_uid = $2`
	managedAgentSessionPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
	listManagedAgentSessionsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(managed_session)
    ORDER BY managed_session.session_uid), '[]'::jsonb)
FROM (
	SELECT session.tenant_id, session.project_uid, session.session_uid, session.provider_kind,
        session.environment_lease_uid, session.environment_generation,
        environment.environment_profile_uid, environment.environment_profile_version,
        session.state, session.resource_version, session.created_at, session.updated_at
    FROM cloud_agents.managed_agent_sessions AS session
    LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
        ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
        AND environment.lease_uid = session.environment_lease_uid
    WHERE session.tenant_id = cloud_agents.require_tenant_id()
        AND session.project_uid = $1
        AND session.session_uid > $2
    ORDER BY session.session_uid
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
					input.EnvironmentLeaseID, input.Mutation.IdempotencyKey, digest), input.Scope, &result); err != nil {
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
					input.Mutation.IdempotencyKey, digest), input.Scope, &result)
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
				err := scanManagedAgentSession(handle.transaction.queryRow(readContext, getManagedAgentSessionSQL, scope.ProjectID, sessionID), scope, &result)
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
		environmentLeaseID, environmentGeneration, validEnvironment := managedAgentSessionEnvironment(row.EnvironmentLeaseID, row.EnvironmentGeneration)
		environmentProfileID, environmentProfileVersion, validProfile := managedAgentSessionProfile(row.EnvironmentProfileID, row.EnvironmentProfileVersion)
		if row.TenantID != tenantID || row.ProjectID != projectID || !validMutationIdentifier(row.SessionID) ||
			!validMutationIdentifier(row.ProviderKind) || state != internalmanagedagent.SessionActive && state != internalmanagedagent.SessionClosed ||
			!validEnvironment || !validProfile || row.ResourceVersion < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentSessionPage{}, ErrCoordinationResultDrift
		}
		sessions = append(sessions, internalmanagedagent.SessionSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: row.SessionID,
			ProviderKind: row.ProviderKind, EnvironmentLeaseID: environmentLeaseID, EnvironmentGeneration: environmentGeneration,
			EnvironmentProfileID: environmentProfileID, EnvironmentProfileVersion: environmentProfileVersion,
			State: state, Version: uint64(row.ResourceVersion), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
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
	return service.getManagedAgentSessionForRuntime(ctx, tenantID, principal, projectID, sessionID, "projects.act")
}

// GetManagedAgentSessionForArtifact resolves the same Worker route under
// read authority after the artifact endpoint has verified the execution.
func (service *DurableCoordinationService) GetManagedAgentSessionForArtifact(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
) (internalmanagedagent.RuntimeSessionSnapshot, error) {
	return service.getManagedAgentSessionForRuntime(ctx, tenantID, principal, projectID, sessionID, "projects.get")
}

func (service *DurableCoordinationService) getManagedAgentSessionForRuntime(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	requiredPermission string,
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
		operation, bindErr := binder.Bind(scope.TenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, requiredPermission)
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
				var environmentLeaseID *string
				var environmentGeneration *int64
				var environmentProfileID *string
				var environmentProfileVersion *int64
				var state string
				var version int64
				err := handle.transaction.queryRow(readContext, getManagedAgentSessionForExecutionSQL, scope.ProjectID, sessionID).Scan(
					&result.SessionID, &result.ProviderKind, &environmentLeaseID, &environmentGeneration,
					&environmentProfileID, &environmentProfileVersion,
					&state, &version, &result.CreatedAt, &result.UpdatedAt, &cursor,
					&result.WorkerEndpoint, &result.WorkerSPIFFEID, &result.WorkerServerName, &result.EnvironmentReady,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentSessionNotFound
				}
				if err != nil {
					return mapMutationDatabaseError("managed agent session", err)
				}
				result.Scope = scope
				result.State = internalmanagedagent.SessionState(state)
				var validEnvironment bool
				var validProfile bool
				result.EnvironmentLeaseID, result.EnvironmentGeneration, validEnvironment = managedAgentSessionEnvironment(environmentLeaseID, environmentGeneration)
				result.EnvironmentProfileID, result.EnvironmentProfileVersion, validProfile = managedAgentSessionProfile(environmentProfileID, environmentProfileVersion)
				if !validEnvironment || !validProfile || !validManagedAgentSessionSnapshot(result.SessionSnapshot, version) || result.EnvironmentReady && (result.WorkerEndpoint == "" || result.WorkerSPIFFEID == "" || result.WorkerServerName == "") {
					return fmt.Errorf("%w: managed agent execution Session projection", ErrCoordinationResultDrift)
				}
				result.Version = uint64(version)
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

func scanManagedAgentSession(row rowScanner, scope internalmanagedagent.Scope, result *internalmanagedagent.SessionSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var environmentLeaseID *string
	var environmentGeneration *int64
	var environmentProfileID *string
	var environmentProfileVersion *int64
	var state string
	var version int64
	if err := row.Scan(&result.SessionID, &result.ProviderKind, &environmentLeaseID, &environmentGeneration, &environmentProfileID, &environmentProfileVersion, &state, &version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent session", err)
	}
	result.Scope = scope
	result.State = internalmanagedagent.SessionState(state)
	var validEnvironment bool
	var validProfile bool
	result.EnvironmentLeaseID, result.EnvironmentGeneration, validEnvironment = managedAgentSessionEnvironment(environmentLeaseID, environmentGeneration)
	result.EnvironmentProfileID, result.EnvironmentProfileVersion, validProfile = managedAgentSessionProfile(environmentProfileID, environmentProfileVersion)
	if !validEnvironment || !validProfile || !validManagedAgentSessionSnapshot(*result, version) {
		return fmt.Errorf("%w: managed agent session projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
}

func managedAgentSessionEnvironment(leaseID *string, generation *int64) (string, uint64, bool) {
	if leaseID == nil || generation == nil {
		return "", 0, leaseID == nil && generation == nil
	}
	if !validMutationIdentifier(*leaseID) || *generation <= 0 {
		return "", 0, false
	}
	return *leaseID, uint64(*generation), true
}

func managedAgentSessionProfile(profileID *string, version *int64) (string, uint64, bool) {
	if profileID == nil || version == nil {
		return "", 0, profileID == nil && version == nil
	}
	if !validMutationIdentifier(*profileID) || *version < 1 || *version > 2147483647 {
		return "", 0, false
	}
	return *profileID, uint64(*version), true
}

func validManagedAgentSessionSnapshot(result internalmanagedagent.SessionSnapshot, version int64) bool {
	return version > 0 && result.SessionID != "" && (result.State == internalmanagedagent.SessionActive || result.State == internalmanagedagent.SessionClosed) && !result.CreatedAt.IsZero() && !result.UpdatedAt.IsZero()
}
