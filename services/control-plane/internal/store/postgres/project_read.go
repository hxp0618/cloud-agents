package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

var ErrProjectNotFound = errors.New("postgres project was not found")

type Project struct {
	UID             string
	Name            string
	TenantID        string
	OrganizationID  string
	DisplayName     string
	State           string
	ResourceVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

const getProjectSQL = `SELECT
    project_uid, project_name, organization_uid, display_name, state,
    resource_version, created_at, updated_at
FROM cloud_agents.projects
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1
    AND state <> 'archived'`

func (service *DurableCoordinationService) GetProject(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	projectID string,
) (Project, error) {
	if service == nil || service.runner == nil {
		return Project{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) {
		return Project{}, ErrCoordinationInvalidInput
	}
	var result Project
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, err := binder.Bind(tenantID, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, "projects.get")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}, func() error {
				err := handle.transaction.queryRow(readContext, getProjectSQL, projectID).Scan(
					&result.UID, &result.Name, &result.OrganizationID, &result.DisplayName, &result.State,
					&result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
				)
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrProjectNotFound
				}
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}
