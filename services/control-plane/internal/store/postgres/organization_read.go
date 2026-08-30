package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrOrganizationNotFound = errors.New("postgres organization was not found")

type Organization struct {
	UID             string    `json:"organization_uid"`
	Name            string    `json:"organization_name"`
	TenantID        string    `json:"tenant_id"`
	DisplayName     string    `json:"display_name"`
	State           string    `json:"state"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type OrganizationPage struct {
	Organizations       []Organization
	NextOrganizationUID string
}

const getOrganizationSQL = `SELECT
    organization_uid, organization_name, tenant_id, display_name, state,
    resource_version, created_at, updated_at
FROM cloud_agents.organizations
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND organization_uid = $1
    AND state IN ('active', 'suspended')`

const (
	organizationPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.organizations
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND organization_uid = $1
    AND state IN ('active', 'suspended')`
	listOrganizationsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(organization)
    ORDER BY organization.organization_uid), '[]'::jsonb)
FROM (
    SELECT organization_uid, organization_name, tenant_id, display_name, state,
        resource_version, created_at, updated_at
    FROM cloud_agents.organizations
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND state IN ('active', 'suspended')
        AND organization_uid > $1
    ORDER BY organization_uid
    LIMIT $2
) AS organization`
)

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
				return scanOrganization(handle.transaction.queryRow(readContext, getOrganizationSQL, organizationID), &result)
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func (service *DurableCoordinationService) ListOrganizations(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	afterOrganizationUID string,
	limit int,
) (OrganizationPage, error) {
	if service == nil || service.runner == nil {
		return OrganizationPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) ||
		afterOrganizationUID != "" && !validMutationIdentifier(afterOrganizationUID) ||
		limit < 1 || limit > 200 {
		return OrganizationPage{}, ErrCoordinationInvalidInput
	}
	var result OrganizationPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
		operation, err := binder.Bind(tenantID, scope, "organizations.list")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterOrganizationUID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, organizationPageCursorIdentitySQL, afterOrganizationUID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("organization page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listOrganizationsSQL, afterOrganizationUID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("organizations", err)
				}
				result, err = decodeOrganizationPageRows(raw, tenantID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeOrganizationPageRows(raw []byte, tenantID string, limit int) (OrganizationPage, error) {
	var organizations []Organization
	if json.Unmarshal(raw, &organizations) != nil || organizations == nil || len(organizations) > limit+1 {
		return OrganizationPage{}, ErrCoordinationResultDrift
	}
	for _, organization := range organizations {
		if organization.TenantID != tenantID || !validMutationIdentifier(organization.UID) || !validMutationIdentifier(organization.Name) ||
			!utf8.ValidString(organization.DisplayName) || utf8.RuneCountInString(organization.DisplayName) < 1 || utf8.RuneCountInString(organization.DisplayName) > 160 ||
			organization.State != "active" && organization.State != "suspended" || organization.ResourceVersion < 1 || organization.CreatedAt.IsZero() || organization.UpdatedAt.IsZero() {
			return OrganizationPage{}, ErrCoordinationResultDrift
		}
	}
	result := OrganizationPage{Organizations: organizations}
	if len(organizations) > limit {
		result.Organizations = organizations[:limit]
		result.NextOrganizationUID = result.Organizations[len(result.Organizations)-1].UID
	}
	return result, nil
}

func scanOrganization(row rowScanner, result *Organization) error {
	err := row.Scan(
		&result.UID, &result.Name, &result.TenantID, &result.DisplayName,
		&result.State, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrOrganizationNotFound
	}
	return err
}
