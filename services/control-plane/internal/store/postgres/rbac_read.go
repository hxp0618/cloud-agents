package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var (
	ErrMembershipNotFound  = errors.New("postgres membership was not found")
	ErrRoleBindingNotFound = errors.New("postgres role binding was not found")
)

type Membership struct {
	UID             string
	Name            string
	TenantID        string
	Subject         authz.SubjectRef
	Scope           authz.ScopeRef
	State           string
	ExpiresAt       *time.Time
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RoleBinding struct {
	UID             string
	Name            string
	TenantID        string
	Subject         authz.SubjectRef
	RoleName        string
	RoleVersion     int64
	Scope           authz.ScopeRef
	State           string
	ExpiresAt       *time.Time
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const (
	resolveMembershipScopeSQL = `SELECT
    scope_level,
    CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND membership_uid = $1`
	resolveRoleBindingScopeSQL = `SELECT
    scope_level,
    CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END
FROM cloud_agents.role_bindings
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND role_binding_uid = $1`

	getMembershipSQL = `SELECT
    membership_uid, membership_name, tenant_id,
    subject_kind, subject_issuer, subject_value,
    scope_level,
    CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END,
    state, expires_at, resource_version, created_at, updated_at
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND membership_uid = $1
    AND scope_level = $2
    AND CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END = $3`
	getRoleBindingSQL = `SELECT
    role_binding_uid, role_binding_name, tenant_id,
    subject_kind, subject_issuer, subject_value,
    role_name, role_version,
    scope_level,
    CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END,
    state, expires_at, resource_version, created_at, updated_at
FROM cloud_agents.role_bindings
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND role_binding_uid = $1
    AND scope_level = $2
    AND CASE scope_level
        WHEN 'tenant' THEN scope_tenant_uid
        WHEN 'organization' THEN scope_organization_uid
        WHEN 'project' THEN scope_project_uid
    END = $3`
)

func (service *DurableCoordinationService) ResolveMembershipScope(ctx context.Context, tenantID, membershipID string) (authz.ScopeRef, error) {
	return resolveRBACScope(ctx, service, tenantID, membershipID, resolveMembershipScopeSQL, ErrMembershipNotFound)
}

func (service *DurableCoordinationService) ResolveRoleBindingScope(ctx context.Context, tenantID, roleBindingID string) (authz.ScopeRef, error) {
	return resolveRBACScope(ctx, service, tenantID, roleBindingID, resolveRoleBindingScopeSQL, ErrRoleBindingNotFound)
}

func resolveRBACScope(ctx context.Context, service *DurableCoordinationService, tenantID, resourceID, statement string, notFound error) (authz.ScopeRef, error) {
	if service == nil || service.runner == nil {
		return authz.ScopeRef{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(resourceID) {
		return authz.ScopeRef{}, ErrCoordinationInvalidInput
	}
	var level string
	var id *string
	err := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
		handle, ok := capability.(*tenantReadHandle)
		if !ok {
			return ErrTenantCapabilityClosed
		}
		return handle.transaction.queryRow(readContext, statement, resourceID).Scan(&level, &id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ScopeRef{}, notFound
	}
	if err != nil {
		return authz.ScopeRef{}, err
	}
	scope := authz.ScopeRef{Level: authz.ScopeLevel(level)}
	if id != nil {
		scope.ID = *id
	}
	if scope.Level == authz.ScopePlatform || scope.Validate(tenantID) != nil {
		return authz.ScopeRef{}, fmt.Errorf("stored RBAC scope is invalid")
	}
	return scope, nil
}

func (service *DurableCoordinationService) GetMembership(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, membershipID string, scope authz.ScopeRef) (Membership, error) {
	if service == nil || service.runner == nil {
		return Membership{}, ErrNilCoordinationRunner
	}
	if ctx == nil {
		return Membership{}, ErrCoordinationInvalidInput
	}
	if err := validateStoredRBACRead(tenantID, membershipID, scope); err != nil {
		return Membership{}, err
	}
	var result Membership
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, scope, "memberships.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var storedLevel, storedID string
				err := handle.transaction.queryRow(readContext, getMembershipSQL, membershipID, string(scope.Level), scope.ID).Scan(
					&result.UID, &result.Name, &result.TenantID,
					&result.Subject.Kind, &result.Subject.Issuer, &result.Subject.Subject,
					&storedLevel, &storedID, &result.State, &result.ExpiresAt,
					&result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrMembershipNotFound
				}
				if err == nil && (storedLevel != string(scope.Level) || storedID != scope.ID) {
					return ErrMembershipNotFound
				}
				result.Scope = scope
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) GetRoleBinding(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, roleBindingID string, scope authz.ScopeRef) (RoleBinding, error) {
	if service == nil || service.runner == nil {
		return RoleBinding{}, ErrNilCoordinationRunner
	}
	if ctx == nil {
		return RoleBinding{}, ErrCoordinationInvalidInput
	}
	if err := validateStoredRBACRead(tenantID, roleBindingID, scope); err != nil {
		return RoleBinding{}, err
	}
	var result RoleBinding
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, scope, "role-bindings.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				var storedLevel, storedID string
				err := handle.transaction.queryRow(readContext, getRoleBindingSQL, roleBindingID, string(scope.Level), scope.ID).Scan(
					&result.UID, &result.Name, &result.TenantID,
					&result.Subject.Kind, &result.Subject.Issuer, &result.Subject.Subject,
					&result.RoleName, &result.RoleVersion, &storedLevel, &storedID,
					&result.State, &result.ExpiresAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrRoleBindingNotFound
				}
				if err == nil && (storedLevel != string(scope.Level) || storedID != scope.ID) {
					return ErrRoleBindingNotFound
				}
				result.Scope = scope
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func validateStoredRBACRead(tenantID, resourceID string, scope authz.ScopeRef) error {
	if !validMutationIdentifier(tenantID) || !validMutationIdentifier(resourceID) || scope.Level == authz.ScopePlatform || scope.Validate(tenantID) != nil {
		return ErrCoordinationInvalidInput
	}
	return nil
}
