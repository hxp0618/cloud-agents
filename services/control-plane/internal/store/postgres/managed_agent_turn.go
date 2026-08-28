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

var ErrManagedAgentTurnNotFound = errors.New("managed agent turn was not found")

const (
	createManagedAgentTurnSQL = `SELECT turn_uid, input_digest, execution_uid, state, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_turn_v1($1, $2, $3, $4, $5, $6, $7)`
	getManagedAgentTurnSQL = `SELECT turn_uid, input_digest, execution_uid, state, resource_version, created_at, updated_at
FROM cloud_agents.managed_agent_turns
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3`
)

// CreateManagedAgentTurn persists one queued Turn under an active Session.
func (service *DurableCoordinationService) CreateManagedAgentTurn(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CreateTurnInput,
) (internalmanagedagent.TurnSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.TurnSnapshot{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID {
		return internalmanagedagent.TurnSnapshot{}, ErrCoordinationInvalidInput
	}
	inputDigest, err := internalmanagedagent.TurnInputDigest(input.InputText)
	if err != nil || ctx == nil {
		if err != nil {
			return internalmanagedagent.TurnSnapshot{}, ErrCoordinationInvalidInput
		}
		return internalmanagedagent.TurnSnapshot{}, ErrNilContext
	}
	digest, err := internalmanagedagent.TurnCreateMutationDigest(input)
	if err != nil {
		return internalmanagedagent.TurnSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.TurnSnapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				return scanManagedAgentTurn(handle.transaction.queryRow(ctx, createManagedAgentTurnSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, inputDigest,
					input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result)
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

// GetManagedAgentTurn reads a tenant/project/session-bound Turn.
func (service *DurableCoordinationService) GetManagedAgentTurn(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	turnID string,
) (internalmanagedagent.TurnSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.TurnSnapshot{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || len(scope.TenantID) == 0 || len(scope.ProjectID) == 0 || len(sessionID) == 0 || len(turnID) == 0 {
		return internalmanagedagent.TurnSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.TurnSnapshot
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
				err := scanManagedAgentTurn(handle.transaction.queryRow(readContext, getManagedAgentTurnSQL, scope.ProjectID, sessionID, turnID), scope, sessionID, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentTurnNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func scanManagedAgentTurn(row rowScanner, scope internalmanagedagent.Scope, sessionID string, result *internalmanagedagent.TurnSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var version int64
	var executionID *string
	var state string
	if err := row.Scan(&result.TurnID, &result.InputDigest, &executionID, &state, &version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent turn", err)
	}
	result.Scope = scope
	result.SessionID = sessionID
	result.State = internalmanagedagent.TurnState(state)
	if executionID != nil {
		result.ExecutionID = *executionID
	}
	if version <= 0 || result.TurnID == "" || result.InputDigest == "" || (result.State != internalmanagedagent.TurnQueued && result.State != internalmanagedagent.TurnRunning && result.State != internalmanagedagent.TurnCompleted && result.State != internalmanagedagent.TurnFailed && result.State != internalmanagedagent.TurnInterrupted && result.State != internalmanagedagent.TurnCancelled) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent turn projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
}
