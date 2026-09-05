package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	internalmanagedhost "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedhost"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ManagedHostEnvironmentLeasePage struct {
	EnvironmentLeases []internalmanagedhost.Snapshot
	NextLeaseID       string
}

type AdminWorkerSnapshot struct {
	Health           *AdminWorkerHealth
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

type AdminWorkerHealth struct {
	State         string     `json:"state"`
	CheckedAt     time.Time  `json:"checkedAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	LastSuccessAt *time.Time `json:"lastSuccessAt"`
}

type AdminEnvironmentLeaseUpgradeStart struct {
	Snapshot  internalmanagedhost.Snapshot
	Operation internaldeploymenttarget.Operation
	Execute   bool
}

type AdminEnvironmentLeaseUpgradeResult struct {
	Snapshot  internalmanagedhost.Snapshot
	Operation internaldeploymenttarget.Operation
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
	Health                *AdminWorkerHealth `json:"health"`
	TenantID              string             `json:"tenant_id"`
	ProjectID             string             `json:"project_uid"`
	LeaseID               string             `json:"lease_uid"`
	LeaseName             string             `json:"lease_name"`
	TargetID              string             `json:"deployment_target_uid"`
	TargetKind            string             `json:"target_kind"`
	TargetGeneration      int64              `json:"deployment_target_generation"`
	Generation            int64              `json:"generation"`
	DesiredPhase          string             `json:"desired_phase"`
	ReleaseDigest         string             `json:"release_digest"`
	ObservedPhase         string             `json:"observed_phase"`
	CleanupPhase          string             `json:"cleanup_phase"`
	EnvironmentID         string             `json:"environment_id"`
	ProviderCredentialRef string             `json:"provider_credential_ref"`
	CPULimitMillis        int64              `json:"cpu_limit_millis"`
	MemoryLimitBytes      int64              `json:"memory_limit_bytes"`
	WorkerEndpoint        string             `json:"worker_endpoint"`
	WorkerSPIFFEID        string             `json:"worker_spiffe_id"`
	WorkerServerName      string             `json:"worker_server_name"`
	StableErrorCode       string             `json:"stable_error_code"`
	ExpiresAt             time.Time          `json:"expires_at"`
	ResourceVersion       int64              `json:"resource_version"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
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
FROM cloud_agents.create_user_environment_v6($1, $2, $3, $4, $5, $6, $7, $8)`
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
        lease.expires_at, lease.resource_version, lease.created_at, lease.updated_at,
        CASE WHEN lease.observed_phase = 'ready' AND lease.worker_health_generation = lease.generation
            AND lease.worker_health_resource_version = lease.resource_version THEN
            pg_catalog.jsonb_build_object(
                'state', CASE WHEN LEAST(lease.worker_health_checked_at + interval '60 seconds', lease.expires_at) <= pg_catalog.statement_timestamp() THEN 'expired'
                    WHEN lease.worker_health_succeeded THEN 'online' ELSE 'unavailable' END,
                'checkedAt', lease.worker_health_checked_at,
                'expiresAt', LEAST(lease.worker_health_checked_at + interval '60 seconds', lease.expires_at),
                'lastSuccessAt', lease.worker_health_success_at)
            ELSE NULL END AS health
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
	adminEnvironmentLeaseUpgradeAuthoritySQL = `SELECT target.target_kind, target.scheduling_state, target.observed_phase,
    lease.rollback_release_digest, lease.rollback_generation,
    EXISTS (
        SELECT 1 FROM cloud_agents.worker_releases AS release
        WHERE release.tenant_id = lease.tenant_id AND release.project_uid = lease.project_uid
            AND release.release_digest = COALESCE(NULLIF($3, ''), lease.rollback_release_digest)
            AND release.status = 'approved' AND release.verification_state = 'attested'
    )
FROM cloud_agents.managed_host_environment_leases AS lease
JOIN cloud_agents.deployment_targets AS target
    ON target.tenant_id = lease.tenant_id
    AND target.project_uid = lease.project_uid
    AND target.target_uid = lease.deployment_target_uid
WHERE lease.tenant_id = cloud_agents.require_tenant_id()
    AND lease.project_uid = $1 AND lease.lease_uid = $2`
	beginAdminEnvironmentLeaseUpgradeSQL = `SELECT ` + deploymentTargetOperationColumns + `, execute_upgrade
FROM cloud_agents.begin_admin_environment_lease_upgrade_v1(
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)`
	completeAdminEnvironmentLeaseUpgradeSQL = `SELECT ` + deploymentTargetOperationColumns + `
FROM cloud_agents.complete_admin_environment_lease_upgrade_v1(
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)`
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

func (service *DurableCoordinationService) PreviewAdminEnvironmentLeaseUpgrade(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, leaseID, action, releaseDigest string,
) (internalmanagedhost.AdminEnvironmentLeaseUpgradePreview, error) {
	if service == nil || service.runner == nil {
		return internalmanagedhost.AdminEnvironmentLeaseUpgradePreview{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(leaseID) ||
		(action != "upgrade" && action != "rollback") || (action == "upgrade" && !validAdminEnvironmentLeaseReleaseDigest(releaseDigest)) || (action == "rollback" && releaseDigest != "") {
		return internalmanagedhost.AdminEnvironmentLeaseUpgradePreview{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalmanagedhost.AdminEnvironmentLeaseUpgradePreview
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
				authority, authorityErr := readAdminEnvironmentLeaseUpgradeAuthority(readContext, handle.transaction, tenantID, projectID, leaseID, releaseDigest)
				if authorityErr != nil {
					return authorityErr
				}
				if action == "rollback" && authority.RollbackReleaseDigest == "" {
					return ErrAdminEnvironmentLeaseRollbackUnavailable
				}
				if !authority.TargetReleaseApproved {
					return ErrAdminEnvironmentLeaseReleaseNotApproved
				}
				if authority.TargetSchedulingState != "drained" || authority.TargetObservedPhase != "ready" {
					return ErrAdminEnvironmentLeaseTargetNotDrained
				}
				var previewErr error
				result, previewErr = internalmanagedhost.NewAdminEnvironmentLeaseUpgradePreview(authority, action, releaseDigest)
				if errors.Is(previewErr, internalmanagedhost.ErrConflict) {
					return ErrAdminEnvironmentLeaseStateConflict
				}
				return previewErr
			})
		})
	})
	return result, mapAdminEnvironmentLeaseUpgradeError(err)
}

func (service *DurableCoordinationService) BeginAdminEnvironmentLeaseUpgrade(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.AdminEnvironmentLeaseUpgradeInput,
) (AdminEnvironmentLeaseUpgradeStart, error) {
	if service == nil || service.runner == nil {
		return AdminEnvironmentLeaseUpgradeStart{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return AdminEnvironmentLeaseUpgradeStart{}, ErrCoordinationInvalidInput
	}
	requestDigest, err := internalmanagedhost.AdminUpgradeMutationDigest(input)
	if err != nil {
		return AdminEnvironmentLeaseUpgradeStart{}, ErrCoordinationInvalidInput
	}
	impactSummary, err := internalmanagedhost.AdminUpgradeImpactSummary(input.Action, input.ExpectedGeneration)
	if err != nil {
		return AdminEnvironmentLeaseUpgradeStart{}, ErrCoordinationInvalidInput
	}
	var result AdminEnvironmentLeaseUpgradeStart
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		actor, ok := operation.Actor()
		if !ok {
			return authz.ErrOperationDenied
		}
		subjectDigest, digestErr := actor.Digest()
		if digestErr != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				authority, authorityErr := readAdminEnvironmentLeaseUpgradeAuthority(ctx, handle.transaction, tenantID, input.Scope.ProjectID, input.LeaseID, input.ReleaseDigest)
				if authorityErr != nil {
					return authorityErr
				}
				rollbackDigest, rollbackGeneration := authority.Lease.ReleaseDigest, authority.Lease.Generation
				if input.Action == "rollback" {
					rollbackDigest, rollbackGeneration = input.ReleaseDigest, authority.RollbackGeneration
					if rollbackGeneration < 1 {
						rollbackGeneration = 1
					}
				}
				currentImpactDigest, impactErr := internalmanagedhost.AdminUpgradeImpactDigest(authority.Lease, authority.TargetKind, input.Action, input.ReleaseDigest, rollbackDigest, rollbackGeneration)
				if impactErr != nil {
					return impactErr
				}
				row := handle.transaction.queryRow(ctx, beginAdminEnvironmentLeaseUpgradeSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.Action, input.ReleaseDigest,
					input.ExpectedGeneration, input.ExpectedResourceVersion, input.ImpactDigest, currentImpactDigest,
					input.Mutation.IdempotencyKey, requestDigest, input.Mutation.RequestID, subjectDigest, impactSummary)
				if scanErr := scanDeploymentTargetOperationWithExecute(row, input.Scope, &result.Operation, &result.Execute); scanErr != nil {
					if errors.Is(scanErr, pgx.ErrNoRows) {
						return internalmanagedhost.ErrNotFound
					}
					return scanErr
				}
				return scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, getManagedHostEnvironmentLeaseSQL, input.Scope.ProjectID, input.LeaseID), input.Scope, &result.Snapshot)
			})
		})
	})
	return result, mapAdminEnvironmentLeaseUpgradeError(err)
}

func (service *DurableCoordinationService) CompleteAdminEnvironmentLeaseUpgrade(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalmanagedhost.CompleteAdminEnvironmentLeaseUpgradeInput,
) (AdminEnvironmentLeaseUpgradeResult, error) {
	if service == nil || service.runner == nil {
		return AdminEnvironmentLeaseUpgradeResult{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return AdminEnvironmentLeaseUpgradeResult{}, ErrCoordinationInvalidInput
	}
	requestDigest, err := internalmanagedhost.AdminUpgradeMutationDigest(input.Upgrade)
	if err != nil {
		return AdminEnvironmentLeaseUpgradeResult{}, ErrCoordinationInvalidInput
	}
	var result AdminEnvironmentLeaseUpgradeResult
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Upgrade.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				deployment := input.Deployment
				if scanErr := scanManagedHostEnvironmentLease(handle.transaction.queryRow(ctx, completeManagedHostEnvironmentLeaseDeploymentSQL,
					deployment.Scope.TenantID, deployment.Scope.ProjectID, deployment.LeaseID, deployment.ExpectedGeneration,
					deployment.TargetID, deployment.ExpectedTargetGeneration, deployment.Succeeded, deployment.WorkerEndpoint,
					deployment.WorkerSPIFFEID, deployment.WorkerServerName, deployment.StableErrorCode), deployment.Scope, &result.Snapshot); scanErr != nil {
					return scanErr
				}
				return scanDeploymentTargetOperation(handle.transaction.queryRow(ctx, completeAdminEnvironmentLeaseUpgradeSQL,
					input.Upgrade.Scope.TenantID, input.Upgrade.Scope.ProjectID, deployment.TargetID, input.Upgrade.Action,
					deployment.ExpectedTargetGeneration, input.Upgrade.Mutation.IdempotencyKey, requestDigest,
					deployment.Succeeded, deployment.StableErrorCode, input.ImpactSummary), internaldeploymenttarget.Scope{TenantID: input.Upgrade.Scope.TenantID, ProjectID: input.Upgrade.Scope.ProjectID}, &result.Operation)
			})
		})
	})
	return result, mapAdminEnvironmentLeaseUpgradeError(err)
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
		if row.Health != nil {
			health := row.Health
			if state != "ready" || health.CheckedAt.IsZero() || !health.ExpiresAt.After(health.CheckedAt) || health.ExpiresAt.Sub(health.CheckedAt) > time.Minute ||
				health.State != "online" && health.State != "unavailable" && health.State != "expired" ||
				health.LastSuccessAt != nil && health.LastSuccessAt.After(health.CheckedAt) ||
				health.State == "online" && (health.LastSuccessAt == nil || !health.LastSuccessAt.Equal(health.CheckedAt)) {
				return AdminWorkerPage{}, ErrCoordinationResultDrift
			}
			worker.Health = health
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

func readAdminEnvironmentLeaseUpgradeAuthority(
	ctx context.Context, transaction tenantTransaction, tenantID, projectID, leaseID, releaseDigest string,
) (internalmanagedhost.AdminUpgradeAuthority, error) {
	if transaction == nil {
		return internalmanagedhost.AdminUpgradeAuthority{}, ErrCoordinationInvalidInput
	}
	authority := internalmanagedhost.AdminUpgradeAuthority{}
	if err := scanManagedHostEnvironmentLease(transaction.queryRow(ctx, getManagedHostEnvironmentLeaseSQL, projectID, leaseID), internalmanagedhost.Scope{TenantID: tenantID, ProjectID: projectID}, &authority.Lease); err != nil {
		return authority, err
	}
	var rollbackDigest *string
	var rollbackGeneration *int64
	if err := transaction.queryRow(ctx, adminEnvironmentLeaseUpgradeAuthoritySQL, projectID, leaseID, releaseDigest).Scan(
		&authority.TargetKind, &authority.TargetSchedulingState, &authority.TargetObservedPhase,
		&rollbackDigest, &rollbackGeneration, &authority.TargetReleaseApproved,
	); err != nil {
		return authority, err
	}
	if (rollbackDigest == nil) != (rollbackGeneration == nil) {
		return authority, ErrCoordinationResultDrift
	}
	if rollbackDigest != nil {
		authority.RollbackReleaseDigest, authority.RollbackGeneration = *rollbackDigest, *rollbackGeneration
	}
	return authority, nil
}

func scanDeploymentTargetOperationWithExecute(row rowScanner, scope internalmanagedhost.Scope, result *internaldeploymenttarget.Operation, execute *bool) error {
	if row == nil || result == nil || execute == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.OperationID, &result.IdempotencyKey, &result.Action, &result.TargetID,
		&result.TargetGeneration, &result.RequestedBy, &result.RequestID, &result.RequestedAt, &result.UpdatedAt,
		&result.State, &result.CurrentStep, &result.StableErrorCode, &result.ImpactSummary, &result.Retryable, execute); err != nil {
		return err
	}
	result.Scope = internaldeploymenttarget.Scope{TenantID: scope.TenantID, ProjectID: scope.ProjectID}
	if result.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func validAdminEnvironmentLeaseReleaseDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[7:] {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func mapAdminEnvironmentLeaseUpgradeError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "environment lease upgrade idempotency conflict":
			return ErrAdminEnvironmentLeaseUpgradeIdempotencyConflict
		case "environment lease generation conflict":
			return ErrAdminEnvironmentLeaseGenerationConflict
		case "environment lease resource version conflict":
			return ErrAdminEnvironmentLeaseResourceVersionConflict
		case "environment lease upgrade state conflict", "environment lease release is unchanged":
			return ErrAdminEnvironmentLeaseStateConflict
		case "deployment target is not ready and drained":
			return ErrAdminEnvironmentLeaseTargetNotDrained
		case "environment lease rollback target conflict":
			return ErrAdminEnvironmentLeaseRollbackUnavailable
		case "environment lease upgrade impact conflict":
			return ErrAdminEnvironmentLeaseImpactConflict
		case "Worker release is not approved":
			return ErrAdminEnvironmentLeaseReleaseNotApproved
		}
	}
	switch {
	case errors.Is(err, ErrAdminEnvironmentLeaseUpgradeIdempotencyConflict),
		errors.Is(err, ErrAdminEnvironmentLeaseGenerationConflict),
		errors.Is(err, ErrAdminEnvironmentLeaseResourceVersionConflict),
		errors.Is(err, ErrAdminEnvironmentLeaseStateConflict),
		errors.Is(err, ErrAdminEnvironmentLeaseTargetNotDrained),
		errors.Is(err, ErrAdminEnvironmentLeaseRollbackUnavailable),
		errors.Is(err, ErrAdminEnvironmentLeaseImpactConflict),
		errors.Is(err, ErrAdminEnvironmentLeaseReleaseNotApproved):
		return err
	default:
		return mapManagedHostEnvironmentLeaseError(err)
	}
}

func mapManagedHostEnvironmentLeaseError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Message {
		case "project concurrent lease quota exceeded":
			return ErrProjectConcurrentLeaseQuotaExceeded
		case "project lease cpu quota exceeded":
			return ErrProjectLeaseCPUQuotaExceeded
		case "project lease memory quota exceeded":
			return ErrProjectLeaseMemoryQuotaExceeded
		case "project lease ttl quota exceeded":
			return ErrProjectLeaseTTLQuotaExceeded
		}
	}
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

var (
	ErrProjectConcurrentLeaseQuotaExceeded = errors.New("project concurrent lease quota exceeded")
	ErrProjectLeaseCPUQuotaExceeded        = errors.New("project lease CPU quota exceeded")
	ErrProjectLeaseMemoryQuotaExceeded     = errors.New("project lease memory quota exceeded")
	ErrProjectLeaseTTLQuotaExceeded        = errors.New("project lease TTL quota exceeded")
)

var (
	ErrAdminEnvironmentLeaseUpgradeIdempotencyConflict = errors.New("Admin environment lease upgrade idempotency key conflicts")
	ErrAdminEnvironmentLeaseGenerationConflict         = errors.New("Admin environment lease generation conflicts")
	ErrAdminEnvironmentLeaseResourceVersionConflict    = errors.New("Admin environment lease resource version conflicts")
	ErrAdminEnvironmentLeaseStateConflict              = errors.New("Admin environment lease state conflicts")
	ErrAdminEnvironmentLeaseTargetNotDrained           = errors.New("Admin environment lease Target is not ready and drained")
	ErrAdminEnvironmentLeaseRollbackUnavailable        = errors.New("Admin environment lease rollback target is unavailable")
	ErrAdminEnvironmentLeaseImpactConflict             = errors.New("Admin environment lease upgrade impact conflicts")
	ErrAdminEnvironmentLeaseReleaseNotApproved         = errors.New("Admin environment lease Worker release is not approved")
)
