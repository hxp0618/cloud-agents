package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	workerruntimev1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentSessionNotFound = errors.New("managed agent session was not found")
var ErrManagedAgentCapabilityUnavailable = errors.New("managed agent capability is unavailable")

type ManagedAgentSessionPage struct {
	Sessions      []internalmanagedagent.SessionSnapshot
	NextSessionID string
}

type managedAgentSessionPageRow struct {
	TenantID                  string                                `json:"tenant_id"`
	ProjectID                 string                                `json:"project_uid"`
	SessionID                 string                                `json:"session_uid"`
	ProviderKind              string                                `json:"provider_kind"`
	EnvironmentLeaseID        *string                               `json:"environment_lease_uid"`
	EnvironmentGeneration     *int64                                `json:"environment_generation"`
	WorkspaceID               *string                               `json:"workspace_uid"`
	SandboxID                 *string                               `json:"sandbox_uid"`
	SandboxGeneration         *int64                                `json:"sandbox_generation"`
	EnvironmentProfileID      *string                               `json:"environment_profile_uid"`
	EnvironmentProfileVersion *int64                                `json:"environment_profile_version"`
	State                     string                                `json:"state"`
	ResourceVersion           int64                                 `json:"resource_version"`
	CreatedAt                 time.Time                             `json:"created_at"`
	UpdatedAt                 time.Time                             `json:"updated_at"`
	McpServerRefs             []internalmanagedagent.McpServerRef   `json:"mcp_server_refs"`
	SkillBundleRefs           []internalmanagedagent.SkillBundleRef `json:"skill_bundle_refs"`
}

const (
	createManagedAgentSessionSQL = `SELECT session_uid, provider_kind, environment_lease_uid, environment_generation,
    workspace_uid, sandbox_uid, sandbox_generation, environment_profile_uid,
    environment_profile_version, state, resource_version, created_at, updated_at, mcp_server_refs, skill_bundle_refs
FROM cloud_agents.create_managed_agent_session_v5($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
	closeManagedAgentSessionSQL = `SELECT transition.session_uid, transition.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    session.workspace_uid, session.sandbox_uid, session.sandbox_generation,
    COALESCE(session.environment_profile_uid, environment.environment_profile_uid),
    COALESCE(session.environment_profile_version, environment.environment_profile_version),
    transition.state, transition.resource_version, transition.created_at, transition.updated_at,
    session.mcp_server_refs, session.skill_bundle_refs
FROM cloud_agents.close_managed_agent_session_v1($1, $2, $3, $4, $5) AS transition
JOIN cloud_agents.managed_agent_sessions AS session
    ON session.tenant_id = cloud_agents.require_tenant_id()
    AND session.project_uid = $2 AND session.session_uid = transition.session_uid
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid`
	getManagedAgentSessionSQL = `SELECT session.session_uid, session.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    session.workspace_uid, session.sandbox_uid, session.sandbox_generation,
    COALESCE(session.environment_profile_uid, environment.environment_profile_uid),
    COALESCE(session.environment_profile_version, environment.environment_profile_version),
    session.state, session.resource_version, session.created_at, session.updated_at,
    session.mcp_server_refs, session.skill_bundle_refs
FROM cloud_agents.managed_agent_sessions AS session
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid
WHERE session.tenant_id = cloud_agents.require_tenant_id()
    AND session.project_uid = $1 AND session.session_uid = $2`
	getManagedAgentSessionForExecutionSQL = `SELECT session.session_uid, session.provider_kind,
    session.environment_lease_uid, session.environment_generation,
    session.workspace_uid, session.sandbox_uid, session.sandbox_generation,
    COALESCE(session.environment_profile_uid, environment.environment_profile_uid),
    COALESCE(session.environment_profile_version, environment.environment_profile_version),
    session.state, session.resource_version, session.created_at, session.updated_at,
    session.mcp_server_refs, session.skill_bundle_refs, session.provider_resume_cursor,
    COALESCE(environment.worker_endpoint, ''), COALESCE(environment.worker_spiffe_id, ''),
    COALESCE(environment.worker_server_name, ''),
    COALESCE(environment.generation = session.environment_generation
        AND environment.desired_phase = 'active' AND environment.observed_phase = 'ready'
        AND environment.cleanup_phase = 'none' AND environment.expires_at > pg_catalog.transaction_timestamp(), false),
    COALESCE(target.target_kind, ''), COALESCE(target.credential_ref, ''),
    COALESCE(sandbox.runtime_uid, ''), COALESCE(sandbox.runtime_operation_uid, ''),
    COALESCE(sandbox.runtime_spec_digest, ''),
    COALESCE(sandbox.workspace_uid = session.workspace_uid
        AND sandbox.generation = session.sandbox_generation
        AND sandbox.desired_state = 'running' AND sandbox.observed_state = 'running'
        AND sandbox.observed_generation = sandbox.generation AND NOT sandbox.writer_released
        AND sandbox.runtime_uid IS NOT NULL AND sandbox.runtime_state = 'Running'
        AND sandbox.runtime_generation = sandbox.generation
        AND sandbox.runtime_spec_digest = sandbox.spec_digest
        AND (sandbox.expires_at IS NULL OR sandbox.expires_at > pg_catalog.transaction_timestamp())
        AND volume.observed_state = 'available' AND target.observed_phase = 'ready'
        AND target.scheduling_state = 'active'
        AND operation.state = 'succeeded' AND operation.cleanup_phase = 'complete', false)
FROM cloud_agents.managed_agent_sessions AS session
LEFT JOIN cloud_agents.managed_host_environment_leases AS environment
    ON environment.tenant_id = session.tenant_id AND environment.project_uid = session.project_uid
    AND environment.lease_uid = session.environment_lease_uid
LEFT JOIN cloud_agents.sandbox_sessions AS sandbox
    ON sandbox.tenant_id = session.tenant_id AND sandbox.project_uid = session.project_uid
    AND sandbox.sandbox_uid = session.sandbox_uid
LEFT JOIN cloud_agents.workspace_volumes AS volume
    ON volume.tenant_id = sandbox.tenant_id AND volume.project_uid = sandbox.project_uid
    AND volume.workspace_uid = sandbox.workspace_uid
LEFT JOIN cloud_agents.deployment_targets AS target
    ON target.tenant_id = volume.tenant_id AND target.project_uid = volume.project_uid
    AND target.target_uid = volume.target_uid
LEFT JOIN cloud_agents.platform_operations AS operation
    ON operation.tenant_id = sandbox.tenant_id AND operation.operation_id = sandbox.operation_id
    AND operation.operation_generation = sandbox.operation_generation
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
        session.workspace_uid, session.sandbox_uid, session.sandbox_generation,
        COALESCE(session.environment_profile_uid, environment.environment_profile_uid) AS environment_profile_uid,
        COALESCE(session.environment_profile_version, environment.environment_profile_version) AS environment_profile_version,
        session.state, session.resource_version, session.created_at, session.updated_at,
        session.mcp_server_refs, session.skill_bundle_refs
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
	resolveMcpServerCapabilitySQL   = `SELECT version, digest, transport, connection_ref, credential_ref, network_policy_ref, permissions, status, pg_catalog.floor(EXTRACT(EPOCH FROM (pg_catalog.transaction_timestamp() + INTERVAL '15 minutes')))::bigint FROM cloud_agents.mcp_servers WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND server_uid=$2`
	resolveSkillBundleCapabilitySQL = `SELECT version, digest, compatible_providers, status, pg_catalog.floor(EXTRACT(EPOCH FROM (pg_catalog.transaction_timestamp() + INTERVAL '15 minutes')))::bigint FROM cloud_agents.skill_bundles WHERE tenant_id=cloud_agents.require_tenant_id() AND project_uid=$1 AND bundle_uid=$2`
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
		transactionErr := service.runner.withTenantReadCommittedMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				mcpRefs, skillRefs, err := managedAgentCapabilityRefsJSON(input.McpServerRefs, input.SkillBundleRefs)
				if err != nil {
					return ErrCoordinationInvalidInput
				}
				if err := scanManagedAgentSessionWithCapabilities(handle.transaction.queryRow(ctx, createManagedAgentSessionSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.ProviderKind,
					nullableManagedAgentString(input.EnvironmentLeaseID), nullableManagedAgentString(input.WorkspaceID),
					nullableManagedAgentString(input.SandboxID), nullableManagedAgentGeneration(input.SandboxGeneration),
					nullableManagedAgentString(input.EnvironmentProfileID), nullableManagedAgentGeneration(input.EnvironmentProfileVersion),
					input.Mutation.IdempotencyKey, digest, mcpRefs, skillRefs), input.Scope, &result); err != nil {
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
				err := scanManagedAgentSessionWithCapabilities(handle.transaction.queryRow(ctx, closeManagedAgentSessionSQL,
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
				err := scanManagedAgentSessionWithCapabilities(handle.transaction.queryRow(readContext, getManagedAgentSessionSQL, scope.ProjectID, sessionID), scope, &result)
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
		if err := internalmanagedagent.ValidateCapabilityRefs(row.McpServerRefs, row.SkillBundleRefs); err != nil {
			return ManagedAgentSessionPage{}, ErrCoordinationResultDrift
		}
		state := internalmanagedagent.SessionState(row.State)
		environmentLeaseID, environmentGeneration, validEnvironment := managedAgentSessionEnvironment(row.EnvironmentLeaseID, row.EnvironmentGeneration)
		workspaceID, sandboxID, sandboxGeneration, validFoundation := managedAgentSessionFoundation(row.WorkspaceID, row.SandboxID, row.SandboxGeneration)
		environmentProfileID, environmentProfileVersion, validProfile := managedAgentSessionProfile(row.EnvironmentProfileID, row.EnvironmentProfileVersion)
		if row.TenantID != tenantID || row.ProjectID != projectID || !validMutationIdentifier(row.SessionID) ||
			!validMutationIdentifier(row.ProviderKind) || state != internalmanagedagent.SessionActive && state != internalmanagedagent.SessionClosed ||
			!validEnvironment || !validFoundation || !validProfile || !validManagedAgentBinding(environmentLeaseID, workspaceID, sandboxID, environmentProfileID) || row.ResourceVersion < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentSessionPage{}, ErrCoordinationResultDrift
		}
		sessions = append(sessions, internalmanagedagent.SessionSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: row.SessionID,
			ProviderKind: row.ProviderKind, EnvironmentLeaseID: environmentLeaseID, EnvironmentGeneration: environmentGeneration,
			WorkspaceID: workspaceID, SandboxID: sandboxID, SandboxGeneration: sandboxGeneration,
			EnvironmentProfileID: environmentProfileID, EnvironmentProfileVersion: environmentProfileVersion,
			McpServerRefs:   append([]internalmanagedagent.McpServerRef(nil), row.McpServerRefs...),
			SkillBundleRefs: append([]internalmanagedagent.SkillBundleRef(nil), row.SkillBundleRefs...),
			State:           state, Version: uint64(row.ResourceVersion), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
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

// ResolveManagedAgentCapabilityBindings resolves the persisted Execution
// binding set under the same project action authority used to open Runtime.
// It deliberately accepts refs rather than reading Session refs: an Execution
// may pin a different, explicitly requested capability set.
func (service *DurableCoordinationService) ResolveManagedAgentCapabilityBindings(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	provider string,
	mcpRefs []internalmanagedagent.McpServerRef,
	skillRefs []internalmanagedagent.SkillBundleRef,
) ([]*workerruntimev1alpha1.RuntimeCapabilityBinding, string, error) {
	if service == nil || service.runner == nil || ctx == nil || tenantID == "" || projectID == "" || sessionID == "" || provider == "" {
		return nil, "", ErrCoordinationInvalidInput
	}
	if err := internalmanagedagent.ValidateCapabilityRefs(mcpRefs, skillRefs); err != nil {
		return nil, "", ErrCoordinationInvalidInput
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	var bindings []*workerruntimev1alpha1.RuntimeCapabilityBinding
	var manifestDigest string
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
				var err error
				bindings, manifestDigest, err = resolveRuntimeCapabilityBindings(readContext, handle, scope, sessionID, provider, mcpRefs, skillRefs)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return bindings, manifestDigest, err
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
				var workspaceID *string
				var sandboxID *string
				var sandboxGeneration *int64
				var state string
				var version int64
				var mcpRefsJSON, skillRefsJSON []byte
				err := handle.transaction.queryRow(readContext, getManagedAgentSessionForExecutionSQL, scope.ProjectID, sessionID).Scan(
					&result.SessionID, &result.ProviderKind, &environmentLeaseID, &environmentGeneration,
					&workspaceID, &sandboxID, &sandboxGeneration, &environmentProfileID, &environmentProfileVersion,
					&state, &version, &result.CreatedAt, &result.UpdatedAt, &mcpRefsJSON, &skillRefsJSON, &cursor,
					&result.WorkerEndpoint, &result.WorkerSPIFFEID, &result.WorkerServerName, &result.EnvironmentReady,
					&result.FoundationTargetKind, &result.FoundationTargetCredential,
					&result.FoundationRuntimeID, &result.FoundationRuntimeOperation,
					&result.FoundationRuntimeSpecDigest, &result.FoundationSandboxReady,
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
				var validFoundation bool
				result.WorkspaceID, result.SandboxID, result.SandboxGeneration, validFoundation = managedAgentSessionFoundation(workspaceID, sandboxID, sandboxGeneration)
				result.EnvironmentProfileID, result.EnvironmentProfileVersion, validProfile = managedAgentSessionProfile(environmentProfileID, environmentProfileVersion)
				result.McpServerRefs, result.SkillBundleRefs, err = decodeManagedAgentCapabilityRefs(mcpRefsJSON, skillRefsJSON)
				if err != nil {
					return fmt.Errorf("%w: managed agent capability refs", ErrCoordinationResultDrift)
				}
				result.CapabilityBindings, result.CapabilityManifestDigest, err = resolveRuntimeCapabilityBindings(readContext, handle, scope, result.SessionID, result.ProviderKind, result.McpServerRefs, result.SkillBundleRefs)
				if err != nil {
					return err
				}
				if !validEnvironment || !validFoundation || !validProfile || !validManagedAgentBinding(result.EnvironmentLeaseID, result.WorkspaceID, result.SandboxID, result.EnvironmentProfileID) || !validManagedAgentSessionSnapshot(result.SessionSnapshot, version) || result.EnvironmentReady && (result.WorkerEndpoint == "" || result.WorkerSPIFFEID == "" || result.WorkerServerName == "") || result.FoundationSandboxReady && (result.FoundationTargetKind == "" || result.FoundationTargetCredential == "" || result.FoundationRuntimeID == "" || result.FoundationRuntimeOperation == "" || result.FoundationRuntimeSpecDigest == "") {
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
	return scanManagedAgentSessionRow(row, scope, result, false)
}

func scanManagedAgentSessionWithCapabilities(row rowScanner, scope internalmanagedagent.Scope, result *internalmanagedagent.SessionSnapshot) error {
	return scanManagedAgentSessionRow(row, scope, result, true)
}

func scanManagedAgentSessionRow(row rowScanner, scope internalmanagedagent.Scope, result *internalmanagedagent.SessionSnapshot, includeCapabilities bool) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var environmentLeaseID, environmentProfileID, workspaceID, sandboxID *string
	var environmentGeneration, environmentProfileVersion, sandboxGeneration *int64
	var state string
	var version int64
	var mcpRefsJSON, skillRefsJSON []byte
	destinations := []any{&result.SessionID, &result.ProviderKind, &environmentLeaseID, &environmentGeneration, &workspaceID, &sandboxID, &sandboxGeneration, &environmentProfileID, &environmentProfileVersion, &state, &version, &result.CreatedAt, &result.UpdatedAt}
	if includeCapabilities {
		destinations = append(destinations, &mcpRefsJSON, &skillRefsJSON)
	}
	if err := row.Scan(destinations...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent session", err)
	}
	result.Scope = scope
	result.State = internalmanagedagent.SessionState(state)
	var validEnvironment, validProfile, validFoundation bool
	result.EnvironmentLeaseID, result.EnvironmentGeneration, validEnvironment = managedAgentSessionEnvironment(environmentLeaseID, environmentGeneration)
	result.WorkspaceID, result.SandboxID, result.SandboxGeneration, validFoundation = managedAgentSessionFoundation(workspaceID, sandboxID, sandboxGeneration)
	result.EnvironmentProfileID, result.EnvironmentProfileVersion, validProfile = managedAgentSessionProfile(environmentProfileID, environmentProfileVersion)
	if includeCapabilities {
		var err error
		result.McpServerRefs, result.SkillBundleRefs, err = decodeManagedAgentCapabilityRefs(mcpRefsJSON, skillRefsJSON)
		if err != nil {
			return fmt.Errorf("%w: managed agent capability refs", ErrCoordinationResultDrift)
		}
	}
	if !validEnvironment || !validFoundation || !validProfile || !validManagedAgentBinding(result.EnvironmentLeaseID, result.WorkspaceID, result.SandboxID, result.EnvironmentProfileID) || !validManagedAgentSessionSnapshot(*result, version) {
		return fmt.Errorf("%w: managed agent session projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
}

func managedAgentCapabilityRefsJSON(mcp []internalmanagedagent.McpServerRef, skills []internalmanagedagent.SkillBundleRef) ([]byte, []byte, error) {
	if err := internalmanagedagent.ValidateCapabilityRefs(mcp, skills); err != nil {
		return nil, nil, err
	}
	if mcp == nil {
		mcp = []internalmanagedagent.McpServerRef{}
	}
	if skills == nil {
		skills = []internalmanagedagent.SkillBundleRef{}
	}
	mcpJSON, err := json.Marshal(mcp)
	if err != nil {
		return nil, nil, err
	}
	skillJSON, err := json.Marshal(skills)
	if err != nil {
		return nil, nil, err
	}
	return mcpJSON, skillJSON, nil
}

func decodeManagedAgentCapabilityRefs(mcpJSON, skillJSON []byte) ([]internalmanagedagent.McpServerRef, []internalmanagedagent.SkillBundleRef, error) {
	if len(mcpJSON) == 0 {
		mcpJSON = []byte("[]")
	}
	if len(skillJSON) == 0 {
		skillJSON = []byte("[]")
	}
	var mcp []internalmanagedagent.McpServerRef
	var skills []internalmanagedagent.SkillBundleRef
	if err := json.Unmarshal(mcpJSON, &mcp); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(skillJSON, &skills); err != nil {
		return nil, nil, err
	}
	if err := internalmanagedagent.ValidateCapabilityRefs(mcp, skills); err != nil {
		return nil, nil, err
	}
	return mcp, skills, nil
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

func managedAgentSessionFoundation(workspaceID, sandboxID *string, generation *int64) (string, string, uint64, bool) {
	if workspaceID == nil || sandboxID == nil || generation == nil {
		return "", "", 0, workspaceID == nil && sandboxID == nil && generation == nil
	}
	if !validMutationIdentifier(*workspaceID) || !validMutationIdentifier(*sandboxID) || *generation <= 0 {
		return "", "", 0, false
	}
	return *workspaceID, *sandboxID, uint64(*generation), true
}

func validManagedAgentBinding(environmentLeaseID, workspaceID, sandboxID, environmentProfileID string) bool {
	return environmentLeaseID == "" && workspaceID == "" && sandboxID == "" && environmentProfileID == "" ||
		environmentLeaseID != "" && workspaceID == "" && sandboxID == "" ||
		environmentLeaseID == "" && workspaceID != "" && sandboxID != "" && environmentProfileID != ""
}

func nullableManagedAgentString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableManagedAgentGeneration(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func validManagedAgentSessionSnapshot(result internalmanagedagent.SessionSnapshot, version int64) bool {
	return version > 0 && result.SessionID != "" && (result.State == internalmanagedagent.SessionActive || result.State == internalmanagedagent.SessionClosed) && !result.CreatedAt.IsZero() && !result.UpdatedAt.IsZero()
}

type runtimeCapabilityManifest struct {
	Version  uint32                           `json:"version"`
	Bindings []runtimeCapabilityManifestEntry `json:"bindings"`
}

type runtimeCapabilityManifestEntry struct {
	ResourceKind        string   `json:"resourceKind"`
	ResourceID          string   `json:"resourceId"`
	Version             string   `json:"version"`
	Digest              string   `json:"digest"`
	Transport           string   `json:"transport,omitempty"`
	ConnectionRef       string   `json:"connectionRef,omitempty"`
	CredentialRef       string   `json:"credentialRef,omitempty"`
	GrantID             string   `json:"grantId"`
	NetworkPolicyRef    string   `json:"networkPolicyRef,omitempty"`
	ExpiresAtUnixSecond uint64   `json:"expiresAtUnixSeconds"`
	Permissions         []string `json:"permissions,omitempty"`
	ReadOnly            bool     `json:"readOnly"`
}

func resolveRuntimeCapabilityBindings(ctx context.Context, handle *tenantReadHandle, scope internalmanagedagent.Scope, sessionID, provider string, mcpRefs []internalmanagedagent.McpServerRef, skillRefs []internalmanagedagent.SkillBundleRef) ([]*workerruntimev1alpha1.RuntimeCapabilityBinding, string, error) {
	if handle == nil || ctx == nil || !validMutationIdentifier(sessionID) || !validMutationIdentifier(provider) {
		return nil, "", ErrCoordinationResultDrift
	}
	bindings := make([]*workerruntimev1alpha1.RuntimeCapabilityBinding, 0, len(mcpRefs)+len(skillRefs))
	manifest := runtimeCapabilityManifest{Version: 1, Bindings: make([]runtimeCapabilityManifestEntry, 0, len(mcpRefs)+len(skillRefs))}
	for _, ref := range mcpRefs {
		var version, digest, transport, connectionRef, credentialRef, networkPolicyRef, status string
		var permissions []string
		var expires int64
		if err := handle.transaction.queryRow(ctx, resolveMcpServerCapabilitySQL, scope.ProjectID, ref.ServerID).Scan(&version, &digest, &transport, &connectionRef, &credentialRef, &networkPolicyRef, &permissions, &status, &expires); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "unavailable")
			}
			return nil, "", mapMutationDatabaseError("managed agent MCP capability", err)
		}
		if status != "active" {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "revoked")
		}
		if version != ref.Version {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "version_mismatch")
		}
		if digest != ref.Digest {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "digest_mismatch")
		}
		if transport != "streamable-http" {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "transport_unsupported")
		}
		if expires <= time.Now().UTC().Unix() || expires > time.Now().UTC().Add(15*time.Minute).Unix() {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceMcpServer, ref.ServerID, ref.Version, ref.Digest, "grant_invalid")
		}
		grantID := runtimeCapabilityGrantID(scope, sessionID, "mcp-server", ref.ServerID, version, digest, expires)
		binding := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "mcp-server", ResourceId: ref.ServerID, Version: version, Digest: digest, Transport: transport, ConnectionRef: connectionRef, CredentialRef: credentialRef, GrantId: grantID, NetworkPolicyRef: networkPolicyRef, ExpiresAtUnixSeconds: uint64(expires), Permissions: append([]string(nil), permissions...)}
		bindings = append(bindings, binding)
		manifest.Bindings = append(manifest.Bindings, runtimeCapabilityManifestEntry{ResourceKind: binding.ResourceKind, ResourceID: binding.ResourceId, Version: binding.Version, Digest: binding.Digest, Transport: binding.Transport, ConnectionRef: binding.ConnectionRef, CredentialRef: binding.CredentialRef, GrantID: binding.GrantId, NetworkPolicyRef: binding.NetworkPolicyRef, ExpiresAtUnixSecond: binding.ExpiresAtUnixSeconds, Permissions: binding.Permissions, ReadOnly: binding.ReadOnly})
	}
	for _, ref := range skillRefs {
		var version, digest, status string
		var providers []string
		var expires int64
		if err := handle.transaction.queryRow(ctx, resolveSkillBundleCapabilitySQL, scope.ProjectID, ref.BundleID).Scan(&version, &digest, &providers, &status, &expires); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "unavailable")
			}
			return nil, "", mapMutationDatabaseError("managed agent Skill capability", err)
		}
		if status != "active" {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "revoked")
		}
		if version != ref.Version {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "version_mismatch")
		}
		if digest != ref.Digest {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "digest_mismatch")
		}
		if expires <= time.Now().UTC().Unix() || expires > time.Now().UTC().Add(15*time.Minute).Unix() {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "grant_invalid")
		}
		if !runtimeSkillProviderCompatible(provider, providers) {
			return nil, "", runtimeCapabilityResolutionFailure(internalmanagedagent.ResourceSkillBundle, ref.BundleID, ref.Version, ref.Digest, "provider_incompatible")
		}
		grantID := runtimeCapabilityGrantID(scope, sessionID, "skill-bundle", ref.BundleID, version, digest, expires)
		binding := &workerruntimev1alpha1.RuntimeCapabilityBinding{ResourceKind: "skill-bundle", ResourceId: ref.BundleID, Version: version, Digest: digest, GrantId: grantID, ExpiresAtUnixSeconds: uint64(expires), ReadOnly: true}
		bindings = append(bindings, binding)
		manifest.Bindings = append(manifest.Bindings, runtimeCapabilityManifestEntry{ResourceKind: binding.ResourceKind, ResourceID: binding.ResourceId, Version: binding.Version, Digest: binding.Digest, GrantID: binding.GrantId, ExpiresAtUnixSecond: binding.ExpiresAtUnixSeconds, ReadOnly: true})
	}
	if len(bindings) == 0 {
		return nil, "", nil
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || len(encoded) > 64<<10 {
		return nil, "", ErrCoordinationResultDrift
	}
	sum := sha256.Sum256(encoded)
	return bindings, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func runtimeCapabilityResolutionFailure(resource internalmanagedagent.ResourceKind, resourceID, version, digest, reason string) error {
	return fmt.Errorf("%w: %w", ErrManagedAgentCapabilityUnavailable, &internalmanagedagent.CapabilityResolutionFailure{
		Resource: resource, ResourceID: resourceID, Version: version, Digest: digest, Reason: reason,
	})
}

func runtimeSkillProviderCompatible(provider string, providers []string) bool {
	for _, candidate := range providers {
		if candidate == provider || provider == "claudeAgent" && candidate == "claude-code" || provider == "claude-code" && candidate == "claudeAgent" {
			return true
		}
	}
	return false
}

func runtimeCapabilityGrantID(scope internalmanagedagent.Scope, sessionID, kind, resourceID, version, digest string, expires int64) string {
	value := scope.TenantID + "\x00" + scope.ProjectID + "\x00" + sessionID + "\x00" + kind + "\x00" + resourceID + "\x00" + version + "\x00" + digest + "\x00" + strconv.FormatInt(expires, 10)
	sum := sha256.Sum256([]byte(value))
	return "grant-" + hex.EncodeToString(sum[:])
}
