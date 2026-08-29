package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

var ErrManagedAgentExecutionNotFound = errors.New("managed agent execution was not found")

const (
	createManagedAgentExecutionSQL = `SELECT execution_uid, generation, state, result_digest, error_code, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	startManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.start_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	settleManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.settle_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	cancelManagedAgentExecutionSQL = `SELECT turn_uid, turn_state, turn_resource_version, turn_created_at, turn_updated_at,
execution_uid, execution_generation, execution_state, result_digest, error_code, execution_resource_version, execution_created_at, execution_updated_at
FROM cloud_agents.cancel_managed_agent_execution_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	getManagedAgentExecutionSQL = `SELECT execution_uid, generation, state, result_digest, error_code, resource_version, created_at, updated_at
FROM cloud_agents.managed_agent_executions
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1 AND session_uid = $2 AND turn_uid = $3 AND execution_uid = $4`
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
		if err := scanManagedAgentExecution(handle.transaction.queryRow(ctx, createManagedAgentExecutionSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.SessionID, input.TurnID, input.ExecutionID,
			int64(input.Generation), input.Mutation.IdempotencyKey, digest), input.Scope, input.SessionID, input.TurnID, &result); err != nil {
			return err
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
	input internalmanagedagent.CompleteExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.ExecutionCompleteMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "succeeded", input.ResultDigest, "", input.Mutation.IdempotencyKey, digest)
}

func (service *DurableCoordinationService) FailManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input internalmanagedagent.FailExecutionInput,
) (internalmanagedagent.ExecutionTransitionResult, error) {
	digest, err := internalmanagedagent.ExecutionFailMutationDigest(input)
	if err != nil {
		return internalmanagedagent.ExecutionTransitionResult{}, ErrCoordinationInvalidInput
	}
	return service.settleManagedAgentExecution(ctx, tenantID, principal, input.Scope, input.SessionID, input.TurnID, input.ExecutionID, input.Generation, "failed", "", input.ErrorCode, input.Mutation.IdempotencyKey, digest)
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

func (service *DurableCoordinationService) settleManagedAgentExecution(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	scope internalmanagedagent.Scope,
	sessionID, turnID, executionID string,
	generation uint64,
	outcome, resultDigest, errorCode, idempotencyKey, requestDigest string,
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
			resultDigest, nullableString(errorCode), idempotencyKey, requestDigest), scope, sessionID, &result); err != nil {
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

func scanManagedAgentExecution(row rowScanner, scope internalmanagedagent.Scope, sessionID, turnID string, result *internalmanagedagent.ExecutionSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var generation, version int64
	var state string
	var resultDigest, errorCode *string
	if err := row.Scan(&result.ExecutionID, &generation, &state, &resultDigest, &errorCode, &version, &result.CreatedAt, &result.UpdatedAt); err != nil {
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
	if generation <= 0 || version <= 0 || result.ExecutionID == "" || (result.State != internalmanagedagent.ExecutionQueued && result.State != internalmanagedagent.ExecutionRunning && result.State != internalmanagedagent.ExecutionSucceeded && result.State != internalmanagedagent.ExecutionFailed && result.State != internalmanagedagent.ExecutionCancelled) || result.CreatedAt.IsZero() || result.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent execution projection", ErrCoordinationResultDrift)
	}
	if (result.State == internalmanagedagent.ExecutionSucceeded && result.ResultDigest == "") || (result.State == internalmanagedagent.ExecutionFailed && result.ErrorCode == "") {
		return fmt.Errorf("%w: managed agent execution terminal projection", ErrCoordinationResultDrift)
	}
	result.Version = uint64(version)
	return nil
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
	if result.Turn.TurnID == "" || turnVersion <= 0 || executionGeneration <= 0 || executionVersion <= 0 || (result.Turn.State != internalmanagedagent.TurnRunning && result.Turn.State != internalmanagedagent.TurnCompleted && result.Turn.State != internalmanagedagent.TurnFailed) || (result.Execution.State != internalmanagedagent.ExecutionRunning && result.Execution.State != internalmanagedagent.ExecutionSucceeded && result.Execution.State != internalmanagedagent.ExecutionFailed) || result.Execution.ExecutionID == "" || result.Turn.CreatedAt.IsZero() || result.Turn.UpdatedAt.IsZero() || result.Execution.CreatedAt.IsZero() || result.Execution.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: managed agent execution transition projection", ErrCoordinationResultDrift)
	}
	return nil
}
