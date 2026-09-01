package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/jackc/pgx/v5"
)

type ManagedHostEnvironmentLeasePage struct {
	EnvironmentLeases []internalmanagedhost.Snapshot
	NextLeaseID       string
}

type managedHostEnvironmentLeasePageRow struct {
	TenantID              string    `json:"tenant_id"`
	ProjectID             string    `json:"project_uid"`
	LeaseID               string    `json:"lease_uid"`
	LeaseName             string    `json:"lease_name"`
	ReleaseDigest         string    `json:"release_digest"`
	TargetID              *string   `json:"deployment_target_uid"`
	TargetGeneration      *int64    `json:"deployment_target_generation"`
	ProviderCredentialRef *string   `json:"provider_credential_ref"`
	CPULimitMillis        *int64    `json:"cpu_limit_millis"`
	MemoryLimitBytes      *int64    `json:"memory_limit_bytes"`
	Generation            int64     `json:"generation"`
	DesiredPhase          string    `json:"desired_phase"`
	ObservedPhase         string    `json:"observed_phase"`
	CleanupPhase          string    `json:"cleanup_phase"`
	EnvironmentID         string    `json:"environment_id"`
	WorkerEndpoint        string    `json:"worker_endpoint"`
	WorkerSPIFFEID        string    `json:"worker_spiffe_id"`
	WorkerServerName      string    `json:"worker_server_name"`
	StableErrorCode       string    `json:"stable_error_code"`
	ExpiresAt             time.Time `json:"expires_at"`
	ResourceVersion       int64     `json:"resource_version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

const (
	createManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest, deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, ''::text, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_host_environment_lease_v3($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	getManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest, deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.managed_host_environment_leases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND lease_uid = $2`
	managedHostEnvironmentLeasePageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_host_environment_leases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND lease_uid = $2`
	listManagedHostEnvironmentLeasesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(environment_lease)
    ORDER BY environment_lease.lease_uid), '[]'::jsonb)
FROM (
	    SELECT tenant_id, project_uid, lease_uid, lease_name, release_digest,
	        deployment_target_uid, deployment_target_generation,
	        provider_credential_ref, cpu_limit_millis, memory_limit_bytes, generation,
	        desired_phase, observed_phase, cleanup_phase, environment_id,
	        worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code, expires_at,
        resource_version, created_at, updated_at
    FROM cloud_agents.managed_host_environment_leases
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND lease_uid > $2
    ORDER BY lease_uid
    LIMIT $3
) AS environment_lease`
	terminateManagedHostEnvironmentLeaseSQL = `SELECT transition.lease_uid, transition.lease_name, transition.release_digest,
	    lease.deployment_target_uid, lease.deployment_target_generation, transition.generation,
	    lease.provider_credential_ref, lease.cpu_limit_millis, lease.memory_limit_bytes,
	    transition.desired_phase, transition.observed_phase, transition.cleanup_phase, transition.environment_id,
	    lease.worker_endpoint, lease.worker_spiffe_id, lease.worker_server_name, ''::text,
	    transition.expires_at, transition.resource_version, transition.created_at, transition.updated_at
FROM cloud_agents.terminate_managed_host_environment_lease_v1($1, $2, $3, $4, $5, $6) AS transition
JOIN cloud_agents.managed_host_environment_leases AS lease
    ON lease.tenant_id = cloud_agents.require_tenant_id() AND lease.project_uid = $2
	    AND lease.lease_uid = transition.lease_uid`
	completeManagedHostEnvironmentLeaseDeploymentSQL = `SELECT lease_uid, lease_name, release_digest,
	    deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.complete_managed_host_environment_lease_deployment_v2($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
)

func (service *DurableCoordinationService) CreateManagedHostEnvironmentLease(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.CreateEnvironmentLeaseInput,
) (internalmanagedhost.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.Snapshot{}, ErrNilCoordinationRunner
	}
	if err := input.Validate(tenantID); err != nil || ctx == nil {
		if ctx == nil {
			return internalmanagedhost.Snapshot{}, ErrNilContext
		}
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedhost.CreateMutationDigest(input)
	if err != nil {
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedhost.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				return scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, createManagedHostEnvironmentLeaseSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.LeaseName, input.ReleaseDigest,
					input.TargetID, input.ExpectedTargetGeneration, input.ProviderCredentialRef, input.CPULimitMillis,
					input.MemoryLimitBytes, input.TTLSeconds, input.Mutation.IdempotencyKey, digest), input.Scope, &result)
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func (service *DurableCoordinationService) GetManagedHostEnvironmentLease(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, leaseID string,
) (internalmanagedhost.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || tenantID == "" || projectID == "" || leaseID == "" {
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}
	var result internalmanagedhost.Snapshot
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
				err := scanManagedHostEnvironmentLease(handle.transaction.queryRow(readContext, getManagedHostEnvironmentLeaseSQL, projectID, leaseID), scope, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internalmanagedhost.ErrNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func (service *DurableCoordinationService) ListManagedHostEnvironmentLeases(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterLeaseID string, limit int,
) (ManagedHostEnvironmentLeasePage, error) {
	if service == nil || service.runner == nil {
		return ManagedHostEnvironmentLeasePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterLeaseID != "" && !validMutationIdentifier(afterLeaseID) || limit < 1 || limit > 200 {
		return ManagedHostEnvironmentLeasePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result ManagedHostEnvironmentLeasePage
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
				if afterLeaseID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, managedHostEnvironmentLeasePageCursorIdentitySQL, projectID, afterLeaseID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("managed host environment lease page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listManagedHostEnvironmentLeasesSQL, projectID, afterLeaseID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("managed host environment leases", err)
				}
				var err error
				result, err = decodeManagedHostEnvironmentLeasePageRows(raw, tenantID, projectID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func decodeManagedHostEnvironmentLeasePageRows(raw []byte, tenantID, projectID string, limit int) (ManagedHostEnvironmentLeasePage, error) {
	var rows []managedHostEnvironmentLeasePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return ManagedHostEnvironmentLeasePage{}, ErrCoordinationResultDrift
	}
	leases := make([]internalmanagedhost.Snapshot, 0, len(rows))
	for _, row := range rows {
		if (row.TargetID == nil) != (row.TargetGeneration == nil) {
			return ManagedHostEnvironmentLeasePage{}, ErrCoordinationResultDrift
		}
		if (row.ProviderCredentialRef == nil) != (row.CPULimitMillis == nil) || (row.ProviderCredentialRef == nil) != (row.MemoryLimitBytes == nil) {
			return ManagedHostEnvironmentLeasePage{}, ErrCoordinationResultDrift
		}
		snapshot := internalmanagedhost.Snapshot{
			Scope:   internalmanagedhost.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			LeaseID: row.LeaseID, LeaseName: row.LeaseName, ReleaseDigest: row.ReleaseDigest,
			Generation: row.Generation, DesiredPhase: row.DesiredPhase, ObservedPhase: row.ObservedPhase,
			CleanupPhase: row.CleanupPhase, EnvironmentID: row.EnvironmentID, WorkerEndpoint: row.WorkerEndpoint,
			WorkerSPIFFEID: row.WorkerSPIFFEID, WorkerServerName: row.WorkerServerName, StableErrorCode: row.StableErrorCode, ExpiresAt: row.ExpiresAt,
			ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TargetID != nil && row.TargetGeneration != nil {
			snapshot.TargetID, snapshot.TargetGeneration = *row.TargetID, *row.TargetGeneration
		}
		if row.ProviderCredentialRef != nil && row.CPULimitMillis != nil && row.MemoryLimitBytes != nil {
			snapshot.ProviderCredentialRef, snapshot.CPULimitMillis, snapshot.MemoryLimitBytes = *row.ProviderCredentialRef, *row.CPULimitMillis, *row.MemoryLimitBytes
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return ManagedHostEnvironmentLeasePage{}, ErrCoordinationResultDrift
		}
		leases = append(leases, snapshot)
	}
	result := ManagedHostEnvironmentLeasePage{EnvironmentLeases: leases}
	if len(leases) > limit {
		result.EnvironmentLeases = leases[:limit]
		result.NextLeaseID = result.EnvironmentLeases[len(result.EnvironmentLeases)-1].LeaseID
	}
	return result, nil
}

func (service *DurableCoordinationService) CompleteManagedHostEnvironmentLeaseDeployment(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.CompleteEnvironmentLeaseDeploymentInput,
) (internalmanagedhost.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		if ctx == nil {
			return internalmanagedhost.Snapshot{}, ErrNilContext
		}
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedhost.Snapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				return scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, completeManagedHostEnvironmentLeaseDeploymentSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ExpectedGeneration,
					input.TargetID, input.ExpectedTargetGeneration, input.Succeeded, input.WorkerEndpoint,
					input.WorkerSPIFFEID, input.WorkerServerName, input.StableErrorCode), input.Scope, &result)
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func (service *DurableCoordinationService) TerminateManagedHostEnvironmentLease(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.TerminateEnvironmentLeaseInput,
) (internalmanagedhost.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.Snapshot{}, ErrNilCoordinationRunner
	}
	if err := input.Validate(tenantID); err != nil || ctx == nil {
		if ctx == nil {
			return internalmanagedhost.Snapshot{}, ErrNilContext
		}
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedhost.TerminateMutationDigest(input)
	if err != nil {
		return internalmanagedhost.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedhost.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				err := scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, terminateManagedHostEnvironmentLeaseSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ExpectedGeneration,
					input.Mutation.IdempotencyKey, digest), input.Scope, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internalmanagedhost.ErrNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func scanManagedHostEnvironmentLease(row rowScanner, scope internalmanagedhost.Scope, result *internalmanagedhost.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var targetID *string
	var targetGeneration *int64
	var providerCredentialRef *string
	var cpuLimitMillis *int64
	var memoryLimitBytes *int64
	if err := row.Scan(&result.LeaseID, &result.LeaseName, &result.ReleaseDigest, &targetID, &targetGeneration, &result.Generation,
		&providerCredentialRef, &cpuLimitMillis, &memoryLimitBytes,
		&result.DesiredPhase, &result.ObservedPhase, &result.CleanupPhase, &result.EnvironmentID,
		&result.WorkerEndpoint, &result.WorkerSPIFFEID, &result.WorkerServerName, &result.StableErrorCode,
		&result.ExpiresAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	if (targetID == nil) != (targetGeneration == nil) {
		return ErrCoordinationResultDrift
	}
	if (providerCredentialRef == nil) != (cpuLimitMillis == nil) || (providerCredentialRef == nil) != (memoryLimitBytes == nil) {
		return ErrCoordinationResultDrift
	}
	result.Scope = scope
	if targetID != nil && targetGeneration != nil {
		result.TargetID, result.TargetGeneration = *targetID, *targetGeneration
	}
	if providerCredentialRef != nil && cpuLimitMillis != nil && memoryLimitBytes != nil {
		result.ProviderCredentialRef, result.CPULimitMillis, result.MemoryLimitBytes = *providerCredentialRef, *cpuLimitMillis, *memoryLimitBytes
	}
	if err := result.Validate(); err != nil {
		return fmt.Errorf("%w: managed host environment lease projection", ErrCoordinationResultDrift)
	}
	return nil
}

func mapManagedHostEnvironmentLeaseError(err error) error {
	switch {
	case errors.Is(err, internalmanagedhost.ErrNotFound):
		return ErrManagedHostEnvironmentLeaseNotFound
	case errors.Is(err, internalmanagedhost.ErrConflict):
		return ErrCoordinationRejected
	case errors.Is(err, internalmanagedhost.ErrIdempotencyConflict):
		return ErrCoordinationRejected
	case errors.Is(err, pgx.ErrNoRows):
		return ErrManagedHostEnvironmentLeaseNotFound
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("managed host environment lease", err)
	}
}

var ErrManagedHostEnvironmentLeaseNotFound = errors.New("managed host environment lease was not found")
