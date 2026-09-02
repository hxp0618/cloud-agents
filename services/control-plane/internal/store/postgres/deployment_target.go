package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/jackc/pgx/v5"
)

type DeploymentTargetPage struct {
	DeploymentTargets []internaldeploymenttarget.Snapshot
	NextTargetID      string
}

type deploymentTargetPageRow struct {
	TenantID        string     `json:"tenant_id"`
	ProjectID       string     `json:"project_uid"`
	TargetID        string     `json:"target_uid"`
	TargetName      string     `json:"target_name"`
	Kind            string     `json:"target_kind"`
	Endpoint        string     `json:"endpoint"`
	CredentialRef   string     `json:"credential_ref"`
	Generation      int64      `json:"generation"`
	ObservedPhase   string     `json:"observed_phase"`
	APIVersion      string     `json:"api_version"`
	EngineVersion   string     `json:"engine_version"`
	OS              string     `json:"target_os"`
	Arch            string     `json:"target_arch"`
	StableErrorCode string     `json:"stable_error_code"`
	LastProbeAt     *time.Time `json:"last_probe_at"`
	ResourceVersion int64      `json:"resource_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

const deploymentTargetColumns = `target_uid, target_name, target_kind, endpoint, credential_ref, generation,
    observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
    last_probe_at, resource_version, created_at, updated_at`

var (
	registerDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.register_deployment_target_v2($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	getDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.deployment_targets
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2`
	deploymentTargetPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.deployment_targets
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2`
	listDeploymentTargetsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(deployment_target)
    ORDER BY deployment_target.target_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, ` + deploymentTargetColumns + `
    FROM cloud_agents.deployment_targets
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND target_uid > $2
    ORDER BY target_uid
    LIMIT $3
) AS deployment_target`
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

func (service *DurableCoordinationService) ListDeploymentTargets(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterTargetID string, limit int,
) (DeploymentTargetPage, error) {
	if service == nil || service.runner == nil {
		return DeploymentTargetPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterTargetID != "" && !validMutationIdentifier(afterTargetID) || limit < 1 || limit > 200 {
		return DeploymentTargetPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result DeploymentTargetPage
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
				if afterTargetID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, deploymentTargetPageCursorIdentitySQL, projectID, afterTargetID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("deployment target page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listDeploymentTargetsSQL, projectID, afterTargetID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("deployment targets", err)
				}
				var err error
				result, err = decodeDeploymentTargetPageRows(raw, tenantID, projectID, limit)
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func decodeDeploymentTargetPageRows(raw []byte, tenantID, projectID string, limit int) (DeploymentTargetPage, error) {
	var rows []deploymentTargetPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return DeploymentTargetPage{}, ErrCoordinationResultDrift
	}
	targets := make([]internaldeploymenttarget.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := internaldeploymenttarget.Snapshot{
			Scope:    internaldeploymenttarget.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			TargetID: row.TargetID, TargetName: row.TargetName, Kind: row.Kind, Endpoint: row.Endpoint,
			CredentialRef: row.CredentialRef, Generation: row.Generation, ObservedPhase: row.ObservedPhase,
			APIVersion: row.APIVersion, EngineVersion: row.EngineVersion, OS: row.OS, Arch: row.Arch,
			StableErrorCode: row.StableErrorCode, LastProbeAt: row.LastProbeAt, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return DeploymentTargetPage{}, ErrCoordinationResultDrift
		}
		targets = append(targets, snapshot)
	}
	result := DeploymentTargetPage{DeploymentTargets: targets}
	if len(targets) > limit {
		result.DeploymentTargets = targets[:limit]
		result.NextTargetID = result.DeploymentTargets[len(result.DeploymentTargets)-1].TargetID
	}
	return result, nil
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
