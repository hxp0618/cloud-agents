package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrRoleNotFound = errors.New("postgres role was not found")

type Role struct {
	UID             string
	Name            string
	TenantID        string
	RoleName        string
	Version         int64
	Permissions     []string
	State           string
	ResourceVersion int64
	CreatedAt       time.Time
}

const getRoleSQL = `SELECT
    'role-' || replace(role.role_name, '.', '-') || '-v' || role.role_version,
    'role-' || replace(role.role_name, '.', '-') || '-v' || role.role_version,
    cloud_agents.require_tenant_id(), role.role_name, role.role_version,
    COALESCE(array_agg(permission.permission ORDER BY permission.permission) FILTER (WHERE permission.permission IS NOT NULL), '{}'::text[]),
    role.state, role.catalog_revision, role.published_at
FROM cloud_agents.builtin_roles AS role
LEFT JOIN cloud_agents.builtin_role_permissions AS permission
    ON permission.role_name = role.role_name
    AND permission.role_version = role.role_version
WHERE 'role-' || replace(role.role_name, '.', '-') || '-v' || role.role_version = $1
    AND role.state <> 'revoked'
GROUP BY role.role_name, role.role_version, role.state, role.catalog_revision, role.published_at`

func (service *DurableCoordinationService) GetRole(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, roleID string) (Role, error) {
	if service == nil || service.runner == nil {
		return Role{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(roleID) {
		return Role{}, ErrCoordinationInvalidInput
	}
	var result Role
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, "roles.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}, func() error {
				err := handle.transaction.queryRow(readContext, getRoleSQL, roleID).Scan(
					&result.UID, &result.Name, &result.TenantID, &result.RoleName, &result.Version,
					&result.Permissions, &result.State, &result.ResourceVersion, &result.CreatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrRoleNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}
