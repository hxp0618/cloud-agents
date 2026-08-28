package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrOrganizationNotFound = errors.New("postgres organization was not found")

type Organization struct {
	UID             string
	Name            string
	TenantID        string
	DisplayName     string
	State           string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const getOrganizationSQL = `SELECT
    organization_uid, organization_name, tenant_id, display_name, state,
    resource_version, created_at, updated_at
FROM cloud_agents.organizations
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND organization_uid = $1
    AND state IN ('active', 'suspended')`

func (service *DurableCoordinationService) GetOrganization(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	organizationID string,
) (Organization, error) {
	if service == nil || service.runner == nil {
		return Organization{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(organizationID) {
		return Organization{}, ErrCoordinationInvalidInput
	}
	var result Organization
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeOrganization, ID: organizationID}, "organizations.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeOrganization, ID: organizationID}, func() error {
				err := handle.transaction.queryRow(readContext, getOrganizationSQL, organizationID).Scan(
					&result.UID, &result.Name, &result.TenantID, &result.DisplayName,
					&result.State, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrOrganizationNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}
