package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/jackc/pgx/v5"
)

const deploymentTargetColumns = `target_uid, target_name, target_kind, endpoint, credential_ref, generation,
    observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
    last_probe_at, resource_version, created_at, updated_at`

var (
	registerDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.register_deployment_target_v2($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	getDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.deployment_targets
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2`
	beginDeploymentTargetProbeSQL = `SELECT ` + deploymentTargetColumns + `, execute_probe
FROM cloud_agents.begin_deployment_target_probe_v1($1, $2, $3, $4, $5, $6)`
	completeDeploymentTargetProbeSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.complete_deployment_target_probe_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
)

func (service *DurableCoordinationService) RegisterDeploymentTarget(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internaldeploymenttarget.RegisterInput,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.RegisterMutationDigest(input)
	if err != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanDeploymentTarget(handle.transaction.queryRow(ctx, registerDeploymentTargetSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.TargetName, input.Kind,
					input.Endpoint, input.CredentialRef, input.Mutation.IdempotencyKey, digest), input.Scope, &result)
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) GetDeploymentTarget(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, targetID string,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(targetID) {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internaldeploymenttarget.Snapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				err := scanDeploymentTarget(handle.transaction.queryRow(readContext, getDeploymentTargetSQL, projectID, targetID),
					internaldeploymenttarget.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				}
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) BeginDeploymentTargetProbe(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internaldeploymenttarget.ProbeInput,
) (internaldeploymenttarget.ProbeStart, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.ProbeStart{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internaldeploymenttarget.ProbeStart{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.ProbeMutationDigest(input)
	if err != nil {
		return internaldeploymenttarget.ProbeStart{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.ProbeStart
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				row := handle.transaction.queryRow(ctx, beginDeploymentTargetProbeSQL, input.Scope.TenantID, input.Scope.ProjectID,
					input.TargetID, input.ExpectedGeneration, input.Mutation.IdempotencyKey, digest)
				if err := scanDeploymentTargetWithExecute(row, input.Scope, &result); errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				} else {
					return err
				}
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) CompleteDeploymentTargetProbe(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, completion internaldeploymenttarget.ProbeCompletion,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || completion.Validate(tenantID) != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.ProbeMutationDigest(completion.Input)
	if err != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: completion.Input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				err := scanDeploymentTarget(handle.transaction.queryRow(ctx, completeDeploymentTargetProbeSQL,
					completion.Input.Scope.TenantID, completion.Input.Scope.ProjectID, completion.Input.TargetID,
					completion.Input.ExpectedGeneration, completion.Input.Mutation.IdempotencyKey, digest, completion.Succeeded,
					completion.APIVersion, completion.EngineVersion, completion.OS, completion.Arch, completion.StableErrorCode),
					completion.Input.Scope, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				}
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func scanDeploymentTarget(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.TargetID, &result.TargetName, &result.Kind, &result.Endpoint, &result.CredentialRef,
		&result.Generation, &result.ObservedPhase, &result.APIVersion, &result.EngineVersion, &result.OS, &result.Arch,
		&result.StableErrorCode, &result.LastProbeAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: deployment target projection", ErrCoordinationResultDrift)
	}
	return nil
}

func scanDeploymentTargetWithExecute(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.ProbeStart) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.Target.TargetID, &result.Target.TargetName, &result.Target.Kind, &result.Target.Endpoint,
		&result.Target.CredentialRef, &result.Target.Generation, &result.Target.ObservedPhase, &result.Target.APIVersion,
		&result.Target.EngineVersion, &result.Target.OS, &result.Target.Arch, &result.Target.StableErrorCode,
		&result.Target.LastProbeAt, &result.Target.ResourceVersion, &result.Target.CreatedAt, &result.Target.UpdatedAt,
		&result.Execute); err != nil {
		return err
	}
	result.Target.Scope = scope
	if result.Target.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapDeploymentTargetError(err error) error {
	switch {
	case errors.Is(err, internaldeploymenttarget.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return ErrDeploymentTargetNotFound
	case errors.Is(err, internaldeploymenttarget.ErrConflict), errors.Is(err, internaldeploymenttarget.ErrIdempotencyConflict):
		return ErrCoordinationRejected
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("deployment target", err)
	}
}

var ErrDeploymentTargetNotFound = errors.New("deployment target was not found")
