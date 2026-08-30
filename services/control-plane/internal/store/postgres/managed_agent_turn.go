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

var ErrManagedAgentTurnNotFound = errors.New("managed agent turn was not found")

type ManagedAgentTurnPage struct {
	Turns      []internalmanagedagent.TurnSnapshot
	NextTurnID string
}

type managedAgentTurnPageRow struct {
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_uid"`
	SessionID       string    `json:"session_uid"`
	TurnID          string    `json:"turn_uid"`
	InputDigest     string    `json:"input_digest"`
	ExecutionID     *string   `json:"execution_uid"`
	State           string    `json:"state"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	createManagedAgentTurnSQL = `SELECT turn_uid, input_digest, execution_uid, state, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_turn_v1($1, $2, $3, $4, $5, $6, $7)`
	getManagedAgentTurnSQL = `SELECT turn_uid, input_digest, execution_uid, state, resource_version, created_at, updated_at
FROM cloud_agents.managed_agent_turns
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3`
	managedAgentTurnPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_agent_turns
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3`
	listManagedAgentTurnsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(managed_turn)
    ORDER BY managed_turn.turn_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, session_uid, turn_uid, input_digest, execution_uid,
        state, resource_version, created_at, updated_at
    FROM cloud_agents.managed_agent_turns
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND session_uid = $2
        AND turn_uid > $3
    ORDER BY turn_uid
    LIMIT $4
) AS managed_turn`
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
				if err := scanManagedAgentTurn(handle.transaction.queryRow(ctx, createManagedAgentTurnSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, inputDigest,
					input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
					return err
				}
				return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
					Scope: input.Scope, SessionID: result.SessionID, Operation: "turn.create", Resource: internalmanagedagent.ResourceTurn,
					TurnID: result.TurnID, InputDigest: result.InputDigest,
					MutationDigest: digest, Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceTurn, To: string(result.State), Version: result.Version}},
				})
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
	return service.readManagedAgentTurn(ctx, tenantID, principal, projectID, sessionID, turnID, "projects.get")
}

func (service *DurableCoordinationService) FindManagedAgentTurnForExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	turnID string,
) (internalmanagedagent.TurnSnapshot, bool, error) {
	result, err := service.readManagedAgentTurn(ctx, tenantID, principal, projectID, sessionID, turnID, "projects.act")
	if errors.Is(err, ErrManagedAgentTurnNotFound) {
		return internalmanagedagent.TurnSnapshot{}, false, nil
	}
	return result, err == nil, err
}

func (service *DurableCoordinationService) readManagedAgentTurn(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	turnID string,
	permission string,
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
		operation, bindErr := binder.Bind(scope.TenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: scope.ProjectID}, permission)
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

func (service *DurableCoordinationService) ListManagedAgentTurns(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	afterTurnID string,
	limit int,
) (ManagedAgentTurnPage, error) {
	if service == nil || service.runner == nil {
		return ManagedAgentTurnPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(sessionID) || afterTurnID != "" && !validMutationIdentifier(afterTurnID) ||
		limit < 1 || limit > 200 {
		return ManagedAgentTurnPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ManagedAgentTurnPage
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
				if afterTurnID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, managedAgentTurnPageCursorIdentitySQL, projectID, sessionID, afterTurnID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("managed agent turn page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedAgentTurnsSQL, projectID, sessionID, afterTurnID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("managed agent turns", err)
				}
				var err error
				result, err = decodeManagedAgentTurnPageRows(raw, tenantID, projectID, sessionID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeManagedAgentTurnPageRows(raw []byte, tenantID, projectID, sessionID string, limit int) (ManagedAgentTurnPage, error) {
	var rows []managedAgentTurnPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ManagedAgentTurnPage{}, ErrCoordinationResultDrift
	}
	turns := make([]internalmanagedagent.TurnSnapshot, 0, len(rows))
	for _, row := range rows {
		state := internalmanagedagent.TurnState(row.State)
		if row.TenantID != tenantID || row.ProjectID != projectID || row.SessionID != sessionID ||
			!validMutationIdentifier(row.TurnID) || !validCoordinationDigest(row.InputDigest) ||
			row.ExecutionID != nil && !validMutationIdentifier(*row.ExecutionID) || !validManagedAgentTurnState(state) ||
			row.ResourceVersion < 1 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentTurnPage{}, ErrCoordinationResultDrift
		}
		turn := internalmanagedagent.TurnSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
			TurnID: row.TurnID, InputDigest: row.InputDigest, State: state, Version: uint64(row.ResourceVersion),
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.ExecutionID != nil {
			turn.ExecutionID = *row.ExecutionID
		}
		turns = append(turns, turn)
	}
	result := ManagedAgentTurnPage{Turns: turns}
	if len(turns) > limit {
		result.Turns = turns[:limit]
		result.NextTurnID = result.Turns[len(result.Turns)-1].TurnID
	}
	return result, nil
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
	if version <= 0 || result.TurnID == "" || result.InputDigest == "" || !validManagedAgentTurnState(result.State) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent turn projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
}

func validManagedAgentTurnState(state internalmanagedagent.TurnState) bool {
	return state == internalmanagedagent.TurnQueued || state == internalmanagedagent.TurnRunning ||
		state == internalmanagedagent.TurnCompleted || state == internalmanagedagent.TurnFailed ||
		state == internalmanagedagent.TurnInterrupted || state == internalmanagedagent.TurnCancelled
}
