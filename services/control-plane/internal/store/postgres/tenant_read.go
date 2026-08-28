package postgres

import (
	"context"
	"errors"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrPlatformTenantNotFound = errors.New("postgres platform tenant was not found")

// GetPlatformTenant reads the authenticated tenant projection through the
// same tenant-bound transaction and RBAC path used by other public reads.
func (service *DurableCoordinationService) GetPlatformTenant(
	ctx context.Context,
	principal *authn.VerifiedPrincipal,
	tenantID string,
) (PlatformTenant, error) {
	if service == nil || service.runner == nil {
		return PlatformTenant{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) {
		return PlatformTenant{}, ErrCoordinationInvalidInput
	}
	var result PlatformTenant
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, "tenants.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, func() error {
				err := handle.transaction.queryRow(readContext, getPlatformTenantSQL).Scan(
					&result.TenantID, &result.TenantUID, &result.TenantName, &result.DisplayName,
					&result.State, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrPlatformTenantNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}
