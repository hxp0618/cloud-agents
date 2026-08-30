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
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_uid"`
	LeaseID         string    `json:"lease_uid"`
	LeaseName       string    `json:"lease_name"`
	ReleaseDigest   string    `json:"release_digest"`
	Generation      int64     `json:"generation"`
	DesiredPhase    string    `json:"desired_phase"`
	ObservedPhase   string    `json:"observed_phase"`
	CleanupPhase    string    `json:"cleanup_phase"`
	EnvironmentID   string    `json:"environment_id"`
	ExpiresAt       time.Time `json:"expires_at"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	createManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest, generation,
    desired_phase, observed_phase, cleanup_phase, environment_id, expires_at, resource_version, created_at, updated_at
FROM cloud_agents.create_managed_host_environment_lease_v1($1, $2, $3, $4, $5, $6, $7, $8)`
	getManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest, generation,
    desired_phase, observed_phase, cleanup_phase, environment_id, expires_at, resource_version, created_at, updated_at
FROM cloud_agents.managed_host_environment_leases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND lease_uid = $2`
	managedHostEnvironmentLeasePageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.managed_host_environment_leases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND lease_uid = $2`
	listManagedHostEnvironmentLeasesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(environment_lease)
    ORDER BY environment_lease.lease_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, lease_uid, lease_name, release_digest, generation,
        desired_phase, observed_phase, cleanup_phase, environment_id, expires_at,
        resource_version, created_at, updated_at
    FROM cloud_agents.managed_host_environment_leases
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND lease_uid > $2
    ORDER BY lease_uid
    LIMIT $3
) AS environment_lease`
	terminateManagedHostEnvironmentLeaseSQL = `SELECT lease_uid, lease_name, release_digest, generation,
    desired_phase, observed_phase, cleanup_phase, environment_id, expires_at, resource_version, created_at, updated_at
FROM cloud_agents.terminate_managed_host_environment_lease_v1($1, $2, $3, $4, $5, $6)`
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
					input.Scope.TenantID, input.Scope.ProjectID, input.LeaseID, input.LeaseName, input.ReleaseDigest, input.TTLSeconds,
					input.Mutation.IdempotencyKey, digest), input.Scope, &result)
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
		snapshot := internalmanagedhost.Snapshot{
			Scope:   internalmanagedhost.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			LeaseID: row.LeaseID, LeaseName: row.LeaseName, ReleaseDigest: row.ReleaseDigest,
			Generation: row.Generation, DesiredPhase: row.DesiredPhase, ObservedPhase: row.ObservedPhase,
			CleanupPhase: row.CleanupPhase, EnvironmentID: row.EnvironmentID, ExpiresAt: row.ExpiresAt,
			ResourceVersion: row.ResourceVersion, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
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
	if err := row.Scan(&result.LeaseID, &result.LeaseName, &result.ReleaseDigest, &result.Generation,
		&result.DesiredPhase, &result.ObservedPhase, &result.CleanupPhase, &result.EnvironmentID,
		&result.ExpiresAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
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
