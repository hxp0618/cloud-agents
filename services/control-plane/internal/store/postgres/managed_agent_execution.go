package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	runtimeprotocol "github.com/hxp0618/cloud-agents/sdk/go/runtime"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentExecutionNotFound = errors.New("managed agent execution was not found")

type ManagedAgentExecutionPage struct {
	Executions []internalmanagedagent.ExecutionSnapshot
	NextTurnID string
}

type managedAgentExecutionPageRow struct {
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_uid"`
	SessionID       string    `json:"session_uid"`
	TurnID          string    `json:"turn_uid"`
	ExecutionID     string    `json:"execution_uid"`
	Generation      int64     `json:"generation"`
	State           string    `json:"state"`
	ResultDigest    *string   `json:"result_digest"`
	ErrorCode       *string   `json:"error_code"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	createManagedAgentExecutionSQL = `SELECT execution_uid
FROM cloud_agents.create_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	startManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.start_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	settleManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.settle_managed_agent_execution_v4($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`
	cancelManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.cancel_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	interruptManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.interrupt_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	getManagedAgentExecutionSQL = `SELECT execution_uid, generation, state, result_digest, error_code, resource_version, created_at, updated_at, terminal_message, runtime_messages
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3 AND execution_uid = $4`
	managedAgentExecutionPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3`
	listManagedAgentExecutionsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(managed_execution)
    ORDER BY managed_execution.turn_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, session_uid, turn_uid, execution_uid, generation, state,
        result_digest, error_code, resource_version, created_at, updated_at
    FROM cloud_agents.managed_agent_executions
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND session_uid = $2
        AND turn_uid > $3
    ORDER BY turn_uid
    LIMIT $4
) AS managed_execution`
)

func (service *DurableCoordinationService) CreateManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CreateExecutionInput,
) (internalmanagedagent.ExecutionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.ExecutionCreateMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionSnapshot
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		var executionID string
		if err := handle.transaction.queryRow(ctx, createManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest).Scan(&executionID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCoordinationResultDrift
			}
			return mapMutationDatabaseError("managed agent execution", err)
		}
		if !validMutationIdentifier(executionID) {
			return ErrCoordinationResultDrift
		}
		if err := scanManagedAgentExecution(handle.transaction.queryRow(ctx, getManagedAgentExecutionSQL,
			input.Scope.ProjectID, input.SessionID, input.TurnID, executionID), input.Scope, input.SessionID, input.TurnID, &result); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCoordinationResultDrift
			}
			return err
		}
		if result.ExecutionID != executionID {
			return ErrCoordinationResultDrift
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.SessionID, Operation: "execution.create", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.TurnID, ExecutionID: result.ExecutionID, Generation: result.Generation, MutationDigest: digest,
			Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceExecution, To: string(result.State), Version: result.Version}},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) StartManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.StartExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrNilCoordinationRunner
	}
	if input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedagent.ExecutionStartMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, startManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "execution.start", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation, MutationDigest: digest,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnQueued), To: string(internalmanagedagent.TurnRunning), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionQueued), To: string(internalmanagedagent.ExecutionRunning), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) CompleteManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CompleteRuntimeExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.RuntimeExecutionCompleteMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	terminalMessage, err := json.Marshal(input.Messages[len(input.Messages)-1])
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	runtimeMessages, err := json.Marshal(input.Messages)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "succeeded", input.ResultDigest, "", input.Mutation.IdempotencyKey, digest, input.ProviderResumeCursor, string(terminalMessage), string(runtimeMessages))
}

func (service *DurableCoordinationService) FailManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.FailRuntimeExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.RuntimeExecutionFailMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var runtimeMessages string
	if len(input.Messages) > 0 {
		encoded, encodeErr := json.Marshal(input.Messages)
		if encodeErr != nil {
			return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
		}
		runtimeMessages = string(encoded)
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "failed", "", input.ErrorCode, input.Mutation.IdempotencyKey, digest, "", "", runtimeMessages)
}

func (service *DurableCoordinationService) CancelManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.CancelTurnInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.TurnCancelMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	if service == nil || service.runner == nil || input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, cancelManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.TargetExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "turn.cancel", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: digest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: string(internalmanagedagent.TurnCancelled), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: string(internalmanagedagent.ExecutionCancelled), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) InterruptManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.InterruptTurnInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.TurnInterruptMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	if service == nil || service.runner == nil || input.Scope.TenantID != tenantID || ctx == nil || input.Generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err = withManagedAgentProjectMutation(service, ctx, tenantID, principal, input.Scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, interruptManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.TargetExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, &result); err != nil {
			return err
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: input.Scope, SessionID: result.Execution.SessionID, Operation: "turn.interrupt", Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: digest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: string(internalmanagedagent.TurnInterrupted), Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: string(internalmanagedagent.ExecutionCancelled), Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) settleManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	scope internalmanagedagent.Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	outcome, resultDigest, errorCode, idempotencyKey, requestDigest, providerResumeCursor, terminalMessage, runtimeMessages string,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrNilCoordinationRunner
	}
	if scope.TenantID != tenantID || ctx == nil || generation > math.MaxInt64 {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionTransitionResult
	err := withManagedAgentProjectMutation(service, ctx, tenantID, principal, scope.ProjectID, func(handle *tenantReadHandle) error {
		if err := scanManagedAgentExecutionTransition(handle.transaction.queryRow(ctx, settleManagedAgentExecutionSQL,
			scope.TenantID, scope.ProjectID, sessionID, turnID, executionID, int64(generation), outcome,
			nullableString(resultDigest), nullableString(errorCode), idempotencyKey, requestDigest, nullableString(providerResumeCursor), nullableString(terminalMessage), nullableString(runtimeMessages)), scope, sessionID, &result); err != nil {
			return err
		}
		operation := "execution.complete"
		executionTo := string(internalmanagedagent.ExecutionSucceeded)
		turnTo := string(internalmanagedagent.TurnCompleted)
		if outcome == "failed" {
			operation = "execution.fail"
			executionTo = string(internalmanagedagent.ExecutionFailed)
			turnTo = string(internalmanagedagent.TurnFailed)
		}
		return appendManagedAgentEvent(ctx, handle.transaction, managedAgentEventInput{
			Scope: scope, SessionID: result.Execution.SessionID, Operation: operation, Resource: internalmanagedagent.ResourceExecution,
			TurnID: result.Turn.TurnID, ExecutionID: result.Execution.ExecutionID, Generation: result.Execution.Generation,
			MutationDigest: requestDigest, ResultDigest: result.Execution.ResultDigest, ErrorCode: result.Execution.ErrorCode,
			Changes: []internalmanagedagent.LifecycleStateChange{
				{Resource: internalmanagedagent.ResourceTurn, From: string(internalmanagedagent.TurnRunning), To: turnTo, Version: result.Turn.Version},
				{Resource: internalmanagedagent.ResourceExecution, From: string(internalmanagedagent.ExecutionRunning), To: executionTo, Version: result.Execution.Version},
			},
		})
	})
	return result, err
}

func (service *DurableCoordinationService) GetManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID, sessionID, turnID, executionID string,
) (internalmanagedagent.ExecutionSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedagent.ExecutionSnapshot{}, ErrNilCoordinationRunner
	}
	scope := internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}
	if ctx == nil || tenantID == "" || projectID == "" || sessionID == "" || turnID == "" || executionID == "" {
		return internalmanagedagent.ExecutionSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedagent.ExecutionSnapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, func() error {
				err := scanManagedAgentExecution(handle.transaction.queryRow(readContext, getManagedAgentExecutionSQL, projectID, sessionID, turnID, executionID), scope, sessionID, turnID, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrManagedAgentExecutionNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) ListManagedAgentExecutions(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
	sessionID string,
	afterTurnID string,
	limit int,
) (ManagedAgentExecutionPage, error) {
	if service == nil || service.runner == nil {
		return ManagedAgentExecutionPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(sessionID) || afterTurnID != "" && !validMutationIdentifier(afterTurnID) ||
		limit < 1 || limit > 200 {
		return ManagedAgentExecutionPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ManagedAgentExecutionPage
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
					if err := handle.transaction.queryRow(readContext, managedAgentExecutionPageCursorIdentitySQL, projectID, sessionID, afterTurnID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("managed agent execution page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedAgentExecutionsSQL, projectID, sessionID, afterTurnID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("managed agent executions", err)
				}
				var err error
				result, err = decodeManagedAgentExecutionPageRows(raw, tenantID, projectID, sessionID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func withManagedAgentProjectMutation(service *DurableCoordinationService, ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID string, callback func(*tenantReadHandle) error) error {
	return authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, func() error { return callback(handle) })
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
}

func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func decodeManagedAgentExecutionPageRows(raw []byte, tenantID, projectID, sessionID string, limit int) (ManagedAgentExecutionPage, error) {
	var rows []managedAgentExecutionPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ManagedAgentExecutionPage{}, ErrCoordinationResultDrift
	}
	executions := make([]internalmanagedagent.ExecutionSnapshot, 0, len(rows))
	for _, row := range rows {
		state := internalmanagedagent.ExecutionState(row.State)
		if row.TenantID != tenantID || row.ProjectID != projectID || row.SessionID != sessionID ||
			!validMutationIdentifier(row.TurnID) || !validMutationIdentifier(row.ExecutionID) ||
			row.Generation < 1 || row.ResourceVersion < 1 || !validManagedAgentExecutionState(state) ||
			row.ResultDigest != nil && !validCoordinationDigest(*row.ResultDigest) ||
			row.ErrorCode != nil && !internalmanagedagent.ValidRuntimeErrorCode(*row.ErrorCode) ||
			state == internalmanagedagent.ExecutionSucceeded && row.ResultDigest == nil ||
			state == internalmanagedagent.ExecutionFailed && row.ErrorCode == nil ||
			row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() {
			return ManagedAgentExecutionPage{}, ErrCoordinationResultDrift
		}
		execution := internalmanagedagent.ExecutionSnapshot{
			Scope: internalmanagedagent.Scope{TenantID: tenantID, ProjectID: projectID}, SessionID: sessionID,
			TurnID: row.TurnID, ExecutionID: row.ExecutionID, Generation: uint64(row.Generation), State: state,
			Version: uint64(row.ResourceVersion), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.ResultDigest != nil {
			execution.ResultDigest = *row.ResultDigest
		}
		if row.ErrorCode != nil {
			execution.ErrorCode = *row.ErrorCode
		}
		executions = append(executions, execution)
	}
	result := ManagedAgentExecutionPage{Executions: executions}
	if len(executions) > limit {
		result.Executions = executions[:limit]
		result.NextTurnID = result.Executions[len(result.Executions)-1].TurnID
	}
	return result, nil
}

func validManagedAgentExecutionState(state internalmanagedagent.ExecutionState) bool {
	return state == internalmanagedagent.ExecutionQueued || state == internalmanagedagent.ExecutionRunning ||
		state == internalmanagedagent.ExecutionSucceeded || state == internalmanagedagent.ExecutionFailed ||
		state == internalmanagedagent.ExecutionCancelled
}

func scanManagedAgentExecution(row rowScanner, scope internalmanagedagent.Scope, sessionID, turnID string, result *internalmanagedagent.ExecutionSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var generation, version int64
	var state string
	var resultDigest, errorCode, terminalMessage, runtimeMessages *string
	if err := row.Scan(&result.ExecutionID, &generation, &state, &resultDigest, &errorCode, &version, &result.CreatedAt, &result.UpdatedAt, &terminalMessage, &runtimeMessages); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return mapMutationDatabaseError("managed agent execution", err)
	}
	result.Scope = scope
	result.SessionID = sessionID
	result.TurnID = turnID
	result.Generation = uint64(generation)
	result.State = internalmanagedagent.ExecutionState(state)
	if resultDigest != nil {
		result.ResultDigest = *resultDigest
	}
	if errorCode != nil {
		result.ErrorCode = *errorCode
	}
	if generation <= 0 || version <= 0 || result.ExecutionID == "" || !validManagedAgentExecutionState(result.State) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent execution projection", ErrCoordinationResultDrift)
	}
	if (result.State == internalmanagedagent.ExecutionSucceeded && result.ResultDigest == "") || (result.State == internalmanagedagent.ExecutionFailed && result.ErrorCode == "") {
		return fmt.Errorf("%w: managed agent execution terminal projection", ErrCoordinationResultDrift)
	}
	if runtimeMessages != nil {
		messages, err := decodePersistedRuntimeMessages(*runtimeMessages, result.ExecutionID, result.Generation, result.State, result.ErrorCode)
		if err != nil {
			return fmt.Errorf("%w: managed agent execution Runtime messages", ErrCoordinationResultDrift)
		}
		result.Messages = messages
	} else if terminalMessage != nil {
		message, err := decodePersistedRuntimeMessage(*terminalMessage)
		if err != nil || result.State != internalmanagedagent.ExecutionSucceeded || message.MessageType != "Result" || message.ExecutionID != result.ExecutionID || message.Generation != result.Generation {
			return fmt.Errorf("%w: managed agent execution terminal message", ErrCoordinationResultDrift)
		}
		digest, err := internalmanagedagent.RuntimeMessageDigest(message)
		if err != nil || digest != result.ResultDigest {
			return fmt.Errorf("%w: managed agent execution terminal digest", ErrCoordinationResultDrift)
		}
		result.Messages = []runtimeprotocol.Message{message}
	}
	if len(result.Messages) > 0 && result.State == internalmanagedagent.ExecutionSucceeded {
		terminal := result.Messages[len(result.Messages)-1]
		digest, err := internalmanagedagent.RuntimeMessageDigest(terminal)
		if err != nil || digest != result.ResultDigest || runtimeMessages != nil && terminalMessage != nil && !runtimeMessagesEndWithTerminal(*runtimeMessages, *terminalMessage) {
			return fmt.Errorf("%w: managed agent execution terminal digest", ErrCoordinationResultDrift)
		}
	}
	result.Version = uint64(version)
	return nil
}

func decodePersistedRuntimeMessage(value string) (runtimeprotocol.Message, error) {
	if len(value) == 0 || len(value) > runtimeprotocol.MaxMessageBytes {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	var message runtimeprotocol.Message
	var trailing any
	if err := decoder.Decode(&message); err != nil || decoder.Decode(&trailing) != io.EOF || runtimeprotocol.ValidateMessage(message) != nil {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	encoded, err := json.Marshal(message)
	if err != nil || !bytes.Equal(encoded, []byte(value)) {
		return runtimeprotocol.Message{}, ErrCoordinationResultDrift
	}
	return message, nil
}

func decodePersistedRuntimeMessages(value, executionID string, generation uint64, state internalmanagedagent.ExecutionState, errorCode string) ([]runtimeprotocol.Message, error) {
	if len(value) == 0 || len(value) > runtimeprotocol.MaxMessageBytes {
		return nil, ErrCoordinationResultDrift
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	var messages []runtimeprotocol.Message
	var trailing any
	if err := decoder.Decode(&messages); err != nil || decoder.Decode(&trailing) != io.EOF || len(messages) == 0 {
		return nil, ErrCoordinationResultDrift
	}
	encoded, err := json.Marshal(messages)
	if err != nil || !bytes.Equal(encoded, []byte(value)) {
		return nil, ErrCoordinationResultDrift
	}
	mutation := internalmanagedagent.Mutation{RequestID: "request", IdempotencyKey: "idempotency-key"}
	if state == internalmanagedagent.ExecutionSucceeded {
		terminal := messages[len(messages)-1]
		digest, digestErr := internalmanagedagent.RuntimeMessageDigest(terminal)
		if digestErr != nil {
			return nil, ErrCoordinationResultDrift
		}
		input := internalmanagedagent.CompleteRuntimeExecutionInput{CompleteExecutionInput: internalmanagedagent.CompleteExecutionInput{Scope: internalmanagedagent.Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: executionID, Generation: generation, ResultDigest: digest, Mutation: mutation}, Messages: messages}
		if _, validateErr := internalmanagedagent.RuntimeExecutionCompleteMutationDigest(input); validateErr != nil {
			return nil, ErrCoordinationResultDrift
		}
	} else if state == internalmanagedagent.ExecutionFailed {
		input := internalmanagedagent.FailRuntimeExecutionInput{FailExecutionInput: internalmanagedagent.FailExecutionInput{Scope: internalmanagedagent.Scope{TenantID: "tenant", ProjectID: "project"}, SessionID: "session", TurnID: "turn", ExecutionID: executionID, Generation: generation, ErrorCode: errorCode, Mutation: mutation}, Messages: messages}
		if _, validateErr := internalmanagedagent.RuntimeExecutionFailMutationDigest(input); validateErr != nil {
			return nil, ErrCoordinationResultDrift
		}
	} else {
		return nil, ErrCoordinationResultDrift
	}
	return messages, nil
}

func runtimeMessagesEndWithTerminal(messages, terminal string) bool {
	var transcript []json.RawMessage
	var terminalMessage json.RawMessage
	if json.Unmarshal([]byte(messages), &transcript) != nil || len(transcript) == 0 || json.Unmarshal([]byte(terminal), &terminalMessage) != nil {
		return false
	}
	return bytes.Equal(transcript[len(transcript)-1], terminalMessage)
}

func scanManagedAgentExecutionTransition(row rowScanner, scope internalmanagedagent.Scope, sessionID string, result *internalmanagedagent.ExecutionTransitionResult) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var turnVersion, executionGeneration, executionVersion int64
	var turnState, executionState string
	var resultDigest, errorCode *string
	if err := row.Scan(&result.Turn.TurnID, &turnState, &turnVersion, &result.Turn.CreatedAt, &result.Turn.UpdatedAt, &result.Execution.ExecutionID, &executionGeneration, &executionState, &resultDigest, &errorCode, &executionVersion, &result.Execution.CreatedAt, &result.Execution.UpdatedAt); err != nil {
		return mapMutationDatabaseError("managed agent execution transition", err)
	}
	result.Turn.Scope = scope
	result.Turn.SessionID = sessionID
	result.Turn.State = internalmanagedagent.TurnState(turnState)
	result.Turn.Version = uint64(turnVersion)
	result.Turn.ExecutionID = result.Execution.ExecutionID
	result.Execution.Scope = scope
	result.Execution.SessionID = sessionID
	result.Execution.TurnID = result.Turn.TurnID
	result.Execution.Generation = uint64(executionGeneration)
	result.Execution.State = internalmanagedagent.ExecutionState(executionState)
	if resultDigest != nil {
		result.Execution.ResultDigest = *resultDigest
	}
	if errorCode != nil {
		result.Execution.ErrorCode = *errorCode
	}
	result.Execution.Version = uint64(executionVersion)
	if result.Turn.TurnID == "" || turnVersion <= 0 || executionGeneration <= 0 || executionVersion <= 0 ||
		!validManagedAgentExecutionTransition(result.Turn.State, result.Execution.State, result.Execution.ResultDigest, result.Execution.ErrorCode) ||
		result.Execution.ExecutionID == "" || result.Turn.CreatedAt.IsZero() || result.Turn.UpdatedAt.IsZero() || result.Execution.CreatedAt.IsZero() || result.Execution.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent execution transition projection", ErrCoordinationResultDrift)
	}
	return nil
}

func validManagedAgentExecutionTransition(turn internalmanagedagent.TurnState, execution internalmanagedagent.ExecutionState, resultDigest, errorCode string) bool {
	switch {
	case turn == internalmanagedagent.TurnRunning && execution == internalmanagedagent.ExecutionRunning:
		return resultDigest == "" && errorCode == ""
	case turn == internalmanagedagent.TurnCompleted && execution == internalmanagedagent.ExecutionSucceeded:
		return validCoordinationDigest(resultDigest) && errorCode == ""
	case turn == internalmanagedagent.TurnFailed && execution == internalmanagedagent.ExecutionFailed:
		return resultDigest == "" && internalmanagedagent.ValidRuntimeErrorCode(errorCode)
	case turn == internalmanagedagent.TurnCancelled && execution == internalmanagedagent.ExecutionCancelled:
		return resultDigest == "" && errorCode == "cancelled"
	case turn == internalmanagedagent.TurnInterrupted && execution == internalmanagedagent.ExecutionCancelled:
		return resultDigest == "" && errorCode == "interrupted"
	default:
		return false
	}
}
