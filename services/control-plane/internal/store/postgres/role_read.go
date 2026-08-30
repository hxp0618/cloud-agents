package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrRoleNotFound = errors.New("postgres role was not found")

type Role struct {
	UID             string    `json:"uid"`
	Name            string    `json:"name"`
	TenantID        string    `json:"tenant_id"`
	RoleName        string    `json:"role_name"`
	Version         int64     `json:"role_version"`
	Permissions     []string  `json:"permissions"`
	State           string    `json:"state"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
}

type RolePage struct {
	Roles           []Role
	NextRoleName    string
	NextRoleVersion int64
}

const (
	getRoleSQL = `SELECT
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
	rolePageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.builtin_roles
WHERE role_name = $1 AND role_version = $2 AND state <> 'revoked'`
	listRolesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(role_page)
    ORDER BY role_page.role_name, role_page.role_version), '[]'::jsonb)
FROM (
    SELECT
        'role-' || replace(role.role_name, '.', '-') || '-v' || role.role_version AS uid,
        'role-' || replace(role.role_name, '.', '-') || '-v' || role.role_version AS name,
        cloud_agents.require_tenant_id() AS tenant_id,
        role.role_name, role.role_version,
        COALESCE(array_agg(permission.permission ORDER BY permission.permission)
            FILTER (WHERE permission.permission IS NOT NULL), '{}'::text[]) AS permissions,
        role.state, role.catalog_revision AS resource_version, role.published_at AS created_at
    FROM cloud_agents.builtin_roles AS role
    LEFT JOIN cloud_agents.builtin_role_permissions AS permission
        ON permission.role_name = role.role_name
        AND permission.role_version = role.role_version
    WHERE role.state <> 'revoked'
        AND (role.role_name > $1 OR (role.role_name = $1 AND role.role_version > $2))
    GROUP BY role.role_name, role.role_version, role.state, role.catalog_revision, role.published_at
    ORDER BY role.role_name, role.role_version
    LIMIT $3
) AS role_page`
)

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
				if err == nil && !validRoleProjection(result, tenantID) {
					return ErrCoordinationResultDrift
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) ListRoles(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	afterRoleName string,
	afterRoleVersion int64,
	limit int,
) (RolePage, error) {
	if service == nil || service.runner == nil {
		return RolePage{}, ErrNilCoordinationRunner
	}
	validCursor := afterRoleName == "" && afterRoleVersion == 0 || validBuiltinRoleName(afterRoleName) && afterRoleVersion > 0
	if ctx == nil || !validMutationIdentifier(tenantID) || !validCursor || limit < 1 || limit > 200 {
		return RolePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	var result RolePage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "roles.list")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterRoleName != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, rolePageCursorIdentitySQL, afterRoleName, afterRoleVersion).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("role page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listRolesSQL, afterRoleName, afterRoleVersion, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("roles", err)
				}
				var err error
				result, err = decodeRolePageRows(raw, tenantID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeRolePageRows(raw []byte, tenantID string, limit int) (RolePage, error) {
	var roles []Role
	if json.Unmarshal(raw, &roles) != nil || roles == nil || len(roles) > limit+1 {
		return RolePage{}, ErrCoordinationResultDrift
	}
	for index, role := range roles {
		if !validRoleProjection(role, tenantID) || index > 0 && (roles[index-1].RoleName > role.RoleName ||
			roles[index-1].RoleName == role.RoleName && roles[index-1].Version >= role.Version) {
			return RolePage{}, ErrCoordinationResultDrift
		}
	}
	result := RolePage{Roles: roles}
	if len(roles) > limit {
		result.Roles = roles[:limit]
		last := result.Roles[len(result.Roles)-1]
		result.NextRoleName, result.NextRoleVersion = last.RoleName, last.Version
	}
	return result, nil
}

func validRoleProjection(role Role, tenantID string) bool {
	if !validBuiltinRoleName(role.RoleName) || role.Version < 1 || role.TenantID != tenantID ||
		role.UID != roleResourceID(role.RoleName, role.Version) || role.Name != role.UID ||
		(role.State != "active" && role.State != "deprecated") || role.ResourceVersion < 1 || role.CreatedAt.IsZero() ||
		len(role.Permissions) < 1 || len(role.Permissions) > 256 {
		return false
	}
	for index, permission := range role.Permissions {
		if !validMutationIdentifier(permission) || !strings.Contains(permission, ".") || index > 0 && role.Permissions[index-1] >= permission {
			return false
		}
	}
	return true
}

func roleResourceID(roleName string, version int64) string {
	return "role-" + strings.ReplaceAll(roleName, ".", "-") + "-v" + strconv.FormatInt(version, 10)
}

func validBuiltinRoleName(roleName string) bool {
	switch roleName {
	case "platform.admin", "tenant.admin", "organization.admin", "project.admin", "project.operator", "project.developer", "project.viewer":
		return true
	default:
		return false
	}
}
