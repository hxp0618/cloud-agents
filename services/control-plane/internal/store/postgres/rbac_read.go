package postgres

import (
	"context"
	"encoding/json"
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
	UID             string           `json:"uid"`
	Name            string           `json:"name"`
	TenantID        string           `json:"tenant_id"`
	Subject         authz.SubjectRef `json:"subject"`
	Scope           authz.ScopeRef   `json:"scope"`
	State           string           `json:"state"`
	ExpiresAt       *time.Time       `json:"expires_at"`
	ResourceVersion int64            `json:"resource_version"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type MembershipPage struct {
	Memberships      []Membership
	NextMembershipID string
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
	membershipPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.memberships
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND membership_uid = $1
    AND state <> 'revoked'
    AND scope_level <> 'platform'`
	listMembershipsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(membership_page)
    ORDER BY membership_page.uid), '[]'::jsonb)
FROM (
    SELECT
        membership_uid AS uid,
        membership_name AS name,
        tenant_id,
        pg_catalog.jsonb_build_object(
            'Kind', subject_kind,
            'Issuer', subject_issuer,
            'Subject', subject_value
        ) AS subject,
        pg_catalog.jsonb_build_object(
            'Level', scope_level,
            'ID', CASE scope_level
                WHEN 'tenant' THEN scope_tenant_uid
                WHEN 'organization' THEN scope_organization_uid
                WHEN 'project' THEN scope_project_uid
            END
        ) AS scope,
        state,
        expires_at,
        resource_version,
        created_at,
        updated_at
    FROM cloud_agents.memberships
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND state <> 'revoked'
        AND scope_level <> 'platform'
        AND membership_uid > $1
    ORDER BY membership_uid
    LIMIT $2
) AS membership_page`
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

func (service *DurableCoordinationService) ListMemberships(ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, afterMembershipID string, limit int) (MembershipPage, error) {
	if service == nil || service.runner == nil {
		return MembershipPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || afterMembershipID != "" && !validMutationIdentifier(afterMembershipID) || limit < 1 || limit > 200 {
		return MembershipPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
	var result MembershipPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "memberships.list")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterMembershipID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, membershipPageCursorIdentitySQL, afterMembershipID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("membership page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listMembershipsSQL, afterMembershipID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("memberships", err)
				}
				var err error
				result, err = decodeMembershipPageRows(raw, tenantID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeMembershipPageRows(raw []byte, tenantID string, limit int) (MembershipPage, error) {
	var memberships []Membership
	if json.Unmarshal(raw, &memberships) != nil || memberships == nil || len(memberships) > limit+1 {
		return MembershipPage{}, ErrCoordinationResultDrift
	}
	for index, membership := range memberships {
		if !validMembershipProjection(membership, tenantID) || index > 0 && memberships[index-1].UID >= membership.UID {
			return MembershipPage{}, ErrCoordinationResultDrift
		}
	}
	result := MembershipPage{Memberships: memberships}
	if len(memberships) > limit {
		result.Memberships = memberships[:limit]
		result.NextMembershipID = result.Memberships[len(result.Memberships)-1].UID
	}
	return result, nil
}

func validMembershipProjection(membership Membership, tenantID string) bool {
	return membership.TenantID == tenantID && validMutationIdentifier(membership.UID) && validMutationIdentifier(membership.Name) &&
		membership.Subject.Validate() == nil && membership.Scope.Level != authz.ScopePlatform && membership.Scope.Validate(tenantID) == nil &&
		(membership.State == authz.MembershipActive || membership.State == authz.MembershipSuspended) && membership.ResourceVersion > 0 &&
		!membership.CreatedAt.IsZero() && !membership.UpdatedAt.IsZero()
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
