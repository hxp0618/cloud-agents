package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentSessionNotFound = errors.New("managed agent session was not found")

const (
	createManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_session_v1($1, $2, $3, $4, $5, $6)`
	closeManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.close_managed_agent_session_v1($1, $2, $3, $4, $5)`
	getManagedAgentSessionSQL = `SELECT session_uid, provider_kind, state, resource_version, created_at, updated_at
FROM cloud_agents.managed_agent_sessions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2`
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
					input.Mutation.IdempotencyKey, digest), input.Scope, &result); err != nil {
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

// GetManagedAgentSessionForExecution reads the Session under the action
// authority already verified for the execute endpoint. It keeps execution
// from requiring a separate projects.get token merely to resolve provider.
func (service *DurableCoordinationService) GetManagedAgentSessionForExecution(
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

func scanManagedAgentSession(row rowScanner, scope internalmanagedagent.Scope, result *internalmanagedagent.SessionSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var state string
	var version int64
	if err := row.Scan(&result.SessionID, &result.ProviderKind, &state, &version, &result.CreatedAt, &result.UpdatedAt); err != nil {
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
