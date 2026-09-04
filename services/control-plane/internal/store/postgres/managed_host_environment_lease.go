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

type AdminWorkerSnapshot struct {
	Scope            internalmanagedhost.Scope
	WorkerID         string
	WorkerName       string
	LeaseID          string
	TargetID         string
	TargetKind       string
	TargetGeneration int64
	Generation       int64
	ReleaseDigest    string
	State            string
	CleanupPhase     string
	CPULimitMillis   int64
	MemoryLimitBytes int64
	WorkerSPIFFEID   string
	WorkerServerName string
	LastHealthAt     *time.Time
	ReadyAt          *time.Time
	StableErrorCode  string
	ResourceVersion  int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AdminWorkerPage struct {
	Workers      []AdminWorkerSnapshot
	NextWorkerID string
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

type adminWorkerPageRow struct {
	TenantID              string    `json:"tenant_id"`
	ProjectID             string    `json:"project_uid"`
	LeaseID               string    `json:"lease_uid"`
	LeaseName             string    `json:"lease_name"`
	TargetID              string    `json:"deployment_target_uid"`
	TargetKind            string    `json:"target_kind"`
	TargetGeneration      int64     `json:"deployment_target_generation"`
	Generation            int64     `json:"generation"`
	DesiredPhase          string    `json:"desired_phase"`
	ReleaseDigest         string    `json:"release_digest"`
	ObservedPhase         string    `json:"observed_phase"`
	CleanupPhase          string    `json:"cleanup_phase"`
	EnvironmentID         string    `json:"environment_id"`
	ProviderCredentialRef string    `json:"provider_credential_ref"`
	CPULimitMillis        int64     `json:"cpu_limit_millis"`
	MemoryLimitBytes      int64     `json:"memory_limit_bytes"`
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
FROM cloud_agents.create_managed_host_environment_lease_v4($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
	createUserEnvironmentSQL = `SELECT lease_uid, lease_name, release_digest, deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at, environment_profile_uid, environment_profile_version
FROM cloud_agents.create_user_environment_v3($1, $2, $3, $4, $5, $6, $7, $8)`
	getUserEnvironmentSQL = `SELECT lease_uid, lease_name, release_digest, deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at, environment_profile_uid, environment_profile_version
FROM cloud_agents.managed_host_environment_leases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND lease_uid = $2
    AND environment_profile_uid IS NOT NULL`
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
	adminWorkerPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_host_environment_leases AS lease
JOIN cloud_agents.deployment_targets AS target
    ON target.tenant_id = lease.tenant_id
    AND target.project_uid = lease.project_uid
    AND target.target_uid = lease.deployment_target_uid
WHERE lease.tenant_id = cloud_agents.require_tenant_id()
    AND lease.project_uid = $1
    AND lease.lease_uid = $2
    AND lease.deployment_target_generation IS NOT NULL
    AND lease.provider_credential_ref IS NOT NULL
    AND lease.cpu_limit_millis IS NOT NULL
    AND lease.memory_limit_bytes IS NOT NULL
    AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')
    AND (lease.observed_phase <> 'ready' OR (lease.worker_spiffe_id <> '' AND lease.worker_server_name <> ''))`
	listAdminWorkersSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(worker)
    ORDER BY worker.lease_uid), '[]'::jsonb)
FROM (
    SELECT lease.tenant_id, lease.project_uid, lease.lease_uid, lease.lease_name,
        lease.deployment_target_uid, target.target_kind, lease.deployment_target_generation,
        lease.generation, lease.desired_phase, lease.release_digest, lease.observed_phase,
        lease.cleanup_phase, lease.environment_id, lease.provider_credential_ref,
        lease.cpu_limit_millis, lease.memory_limit_bytes, lease.worker_endpoint,
        lease.worker_spiffe_id, lease.worker_server_name, lease.stable_error_code,
        lease.expires_at, lease.resource_version, lease.created_at, lease.updated_at
    FROM cloud_agents.managed_host_environment_leases AS lease
    JOIN cloud_agents.deployment_targets AS target
        ON target.tenant_id = lease.tenant_id
        AND target.project_uid = lease.project_uid
        AND target.target_uid = lease.deployment_target_uid
    WHERE lease.tenant_id = cloud_agents.require_tenant_id()
        AND lease.project_uid = $1
        AND lease.lease_uid > $2
        AND lease.deployment_target_generation IS NOT NULL
        AND lease.provider_credential_ref IS NOT NULL
        AND lease.cpu_limit_millis IS NOT NULL
        AND lease.memory_limit_bytes IS NOT NULL
        AND NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')
        AND (lease.observed_phase <> 'ready' OR (lease.worker_spiffe_id <> '' AND lease.worker_server_name <> ''))
    ORDER BY lease.lease_uid
    LIMIT $3
) AS worker`
	terminateManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest,
	    deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id,
	    worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.begin_managed_host_environment_lease_termination_v1($1, $2, $3, $4, $5, $6)`
	completeManagedHostEnvironmentLeaseTerminationSQL = `SELECT lease_uid, lease_name, release_digest,
	    deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id,
	    worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.complete_managed_host_environment_lease_termination_v1($1, $2, $3, $4)`
	completeManagedHostEnvironmentLeaseDeploymentSQL = `SELECT lease_uid, lease_name, release_digest,
	    deployment_target_uid, deployment_target_generation, generation,
	    provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id, worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at
FROM cloud_agents.complete_managed_host_environment_lease_deployment_v2($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	beginManagedHostEnvironmentLeaseUpgradeSQL = `SELECT lease_uid, lease_name, release_digest,
	    deployment_target_uid, deployment_target_generation,
	    generation, provider_credential_ref, cpu_limit_millis, memory_limit_bytes,
	    desired_phase, observed_phase, cleanup_phase, environment_id,
	    worker_endpoint, worker_spiffe_id, worker_server_name, stable_error_code,
	    expires_at, resource_version, created_at, updated_at, execute_upgrade
FROM cloud_agents.begin_managed_host_environment_lease_upgrade_v1($1, $2, $3, $4, $5, $6, $7)`
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
				if err := scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, createManagedHostEnvironmentLeaseSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.LeaseName, input.ReleaseDigest,
					input.TargetID, input.ExpectedTargetGeneration, input.ProviderCredentialRef, input.CPULimitMillis,
					input.MemoryLimitBytes, input.TTLSeconds, input.Mutation.IdempotencyKey, digest), input.Scope, &result); err != nil {
					return err
				}
				if result.ObservedPhase == "ready" && result.WorkerServerName == "" {
					return scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, getManagedHostEnvironmentLeaseSQL, input.Scope.ProjectID, input.LeaseID), input.Scope, &result)
				}
				return nil
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func (service *DurableCoordinationService) CreateUserEnvironment(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.CreateEnvironmentFromProfileInput,
) (internalmanagedhost.ProfileEnvironmentSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedhost.CreateFromProfileMutationDigest(input)
	if err != nil {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrCoordinationInvalidInput
	}
	environmentID, err := internalmanagedhost.UserEnvironmentID(input)
	if err != nil {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedhost.ProfileEnvironmentSnapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanProfileEnvironment(handle.transaction.queryRow(ctx, createUserEnvironmentSQL,
					input.Scope.TenantID, input.Scope.ProjectID, environmentID, input.ProfileID,
					input.ProfileVersion, internalmanagedhost.DefaultTTLSeconds,
					input.Mutation.IdempotencyKey, digest), input.Scope, &result)
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func (service *DurableCoordinationService) GetUserEnvironment(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, environmentID string,
) (internalmanagedhost.ProfileEnvironmentSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(environmentID) {
		return internalmanagedhost.ProfileEnvironmentSnapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalmanagedhost.ProfileEnvironmentSnapshot
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
				err := scanProfileEnvironment(handle.transaction.queryRow(readContext, getUserEnvironmentSQL,
					projectID, environmentID), internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
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

func (service *DurableCoordinationService) BeginManagedHostEnvironmentLeaseUpgrade(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.UpgradeEnvironmentLeaseInput,
) (internalmanagedhost.UpgradeStart, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.UpgradeStart{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		if ctx == nil {
			return internalmanagedhost.UpgradeStart{}, ErrNilContext
		}
		return internalmanagedhost.UpgradeStart{}, ErrCoordinationInvalidInput
	}
	digest, err := internalmanagedhost.UpgradeMutationDigest(input)
	if err != nil {
		return internalmanagedhost.UpgradeStart{}, ErrCoordinationInvalidInput
	}
	var result internalmanagedhost.UpgradeStart
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}, func() error {
				row := handle.transaction.queryRow(ctx, beginManagedHostEnvironmentLeaseUpgradeSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ExpectedGeneration,
					input.ReleaseDigest, input.Mutation.IdempotencyKey, digest)
				if err := scanManagedHostEnvironmentLeaseWithExecute(row, input.Scope, &result.Snapshot, &result.Execute); errors.Is(err, pgx.ErrNoRows) {
					return internalmanagedhost.ErrNotFound
				} else {
					return err
				}
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

func (service *DurableCoordinationService) ListAdminWorkers(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterWorkerID string, limit int,
) (AdminWorkerPage, error) {
	if service == nil || service.runner == nil {
		return AdminWorkerPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterWorkerID != "" && !validMutationIdentifier(afterWorkerID) || limit < 1 || limit > 200 {
		return AdminWorkerPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result AdminWorkerPage
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
				if afterWorkerID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, adminWorkerPageCursorIdentitySQL, projectID, afterWorkerID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("admin worker page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listAdminWorkersSQL, projectID, afterWorkerID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("admin workers", err)
				}
				var err error
				result, err = decodeAdminWorkerPageRows(raw, tenantID, projectID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, mapManagedHostEnvironmentLeaseError(err)
}

func decodeAdminWorkerPageRows(raw []byte, tenantID, projectID string, limit int) (AdminWorkerPage, error) {
	var rows []adminWorkerPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return AdminWorkerPage{}, ErrCoordinationResultDrift
	}
	workers := make([]AdminWorkerSnapshot, 0, len(rows))
	for _, row := range rows {
		lease := internalmanagedhost.Snapshot{
			Scope:   internalmanagedhost.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			LeaseID: row.LeaseID, LeaseName: row.LeaseName, ReleaseDigest: row.ReleaseDigest,
			TargetID: row.TargetID, TargetGeneration: row.TargetGeneration, Generation: row.Generation,
			DesiredPhase: row.DesiredPhase, ObservedPhase: row.ObservedPhase, CleanupPhase: row.CleanupPhase,
			EnvironmentID: row.EnvironmentID, ProviderCredentialRef: row.ProviderCredentialRef,
			CPULimitMillis: row.CPULimitMillis, MemoryLimitBytes: row.MemoryLimitBytes,
			WorkerEndpoint: row.WorkerEndpoint, WorkerSPIFFEID: row.WorkerSPIFFEID,
			WorkerServerName: row.WorkerServerName, StableErrorCode: row.StableErrorCode,
			ExpiresAt: row.ExpiresAt, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		state, ok := adminWorkerState(row.ObservedPhase, row.CleanupPhase)
		if row.TenantID != tenantID || row.ProjectID != projectID || lease.Validate() != nil ||
			row.TargetKind != "docker" && row.TargetKind != "kubernetes" && row.TargetKind != "ssh" || !ok {
			return AdminWorkerPage{}, ErrCoordinationResultDrift
		}
		worker := AdminWorkerSnapshot{
			Scope: lease.Scope, WorkerID: row.LeaseID, WorkerName: row.LeaseName, LeaseID: row.LeaseID,
			TargetID: row.TargetID, TargetKind: row.TargetKind, TargetGeneration: row.TargetGeneration,
			Generation: row.Generation, ReleaseDigest: row.ReleaseDigest, State: state,
			CleanupPhase: row.CleanupPhase, CPULimitMillis: row.CPULimitMillis,
			MemoryLimitBytes: row.MemoryLimitBytes, StableErrorCode: row.StableErrorCode,
			ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if state == "ready" {
			observedAt := row.UpdatedAt
			worker.WorkerSPIFFEID, worker.WorkerServerName = row.WorkerSPIFFEID, row.WorkerServerName
			worker.LastHealthAt, worker.ReadyAt = &observedAt, &observedAt
		}
		workers = append(workers, worker)
	}
	result := AdminWorkerPage{Workers: workers}
	if len(workers) > limit {
		result.Workers = workers[:limit]
		result.NextWorkerID = result.Workers[len(result.Workers)-1].WorkerID
	}
	return result, nil
}

func adminWorkerState(observedPhase, cleanupPhase string) (string, bool) {
	switch observedPhase {
	case "provisioning":
		return "starting", true
	case "ready":
		return "ready", true
	case "terminating":
		return "stopping", true
	case "failed":
		return "failed", true
	case "terminated":
		return "cleanup-pending", cleanupPhase != "complete"
	default:
		return "", false
	}
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

func (service *DurableCoordinationService) CompleteManagedHostEnvironmentLeaseTermination(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.CompleteEnvironmentLeaseTerminationInput,
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
				err := scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, completeManagedHostEnvironmentLeaseTerminationSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.ExpectedGeneration), input.Scope, &result)
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
	return scanManagedHostEnvironmentLeaseWithExecute(row, scope, result, nil)
}

func scanProfileEnvironment(row rowScanner, scope internalmanagedhost.Scope, result *internalmanagedhost.ProfileEnvironmentSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var targetID *string
	var targetGeneration *int64
	var providerCredentialRef *string
	var cpuLimitMillis *int64
	var memoryLimitBytes *int64
	lease := &result.Lease
	if err := row.Scan(&lease.LeaseID, &lease.LeaseName, &lease.ReleaseDigest, &targetID, &targetGeneration,
		&lease.Generation, &providerCredentialRef, &cpuLimitMillis, &memoryLimitBytes,
		&lease.DesiredPhase, &lease.ObservedPhase, &lease.CleanupPhase, &lease.EnvironmentID,
		&lease.WorkerEndpoint, &lease.WorkerSPIFFEID, &lease.WorkerServerName, &lease.StableErrorCode,
		&lease.ExpiresAt, &lease.ResourceVersion, &lease.CreatedAt, &lease.UpdatedAt,
		&result.ProfileID, &result.ProfileVersion); err != nil {
		return err
	}
	if targetID == nil || targetGeneration == nil || providerCredentialRef == nil || cpuLimitMillis == nil || memoryLimitBytes == nil {
		return ErrCoordinationResultDrift
	}
	lease.Scope = scope
	lease.TargetID, lease.TargetGeneration = *targetID, *targetGeneration
	lease.ProviderCredentialRef, lease.CPULimitMillis, lease.MemoryLimitBytes = *providerCredentialRef, *cpuLimitMillis, *memoryLimitBytes
	if result.Validate() != nil {
		return fmt.Errorf("%w: user environment projection", ErrCoordinationResultDrift)
	}
	return nil
}

func scanManagedHostEnvironmentLeaseWithExecute(row rowScanner, scope internalmanagedhost.Scope, result *internalmanagedhost.Snapshot, execute *bool) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var targetID *string
	var targetGeneration *int64
	var providerCredentialRef *string
	var cpuLimitMillis *int64
	var memoryLimitBytes *int64
	targets := []any{&result.LeaseID, &result.LeaseName, &result.ReleaseDigest, &targetID, &targetGeneration, &result.Generation, &providerCredentialRef, &cpuLimitMillis, &memoryLimitBytes, &result.DesiredPhase, &result.ObservedPhase, &result.CleanupPhase, &result.EnvironmentID, &result.WorkerEndpoint, &result.WorkerSPIFFEID, &result.WorkerServerName, &result.StableErrorCode, &result.ExpiresAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt}
	if execute != nil {
		targets = append(targets, execute)
	}
	if err := row.Scan(targets...); err != nil {
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
