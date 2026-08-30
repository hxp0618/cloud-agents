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

var ErrProjectNotFound = errors.New("postgres project was not found")

type Project struct {
	UID             string    `json:"project_uid"`
	Name            string    `json:"project_name"`
	TenantID        string    `json:"tenant_id"`
	OrganizationID  string    `json:"organization_uid"`
	DisplayName     string    `json:"display_name"`
	State           string    `json:"state"`
	ResourceVersion int64     `json:"resource_version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ProjectPage struct {
	Projects       []Project
	NextProjectUID string
}

const getProjectSQL = `SELECT
    project_uid, project_name, organization_uid, display_name, state,
    resource_version, created_at, updated_at
FROM cloud_agents.projects
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND project_uid = $1
    AND state <> 'archived'`

const (
	projectPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.projects
WHERE tenant_id = cloud_agents.require_tenant_id()
    AND organization_uid = $1
    AND project_uid = $2
    AND state <> 'archived'`
	listProjectsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(project)
    ORDER BY project.project_uid), '[]'::jsonb)
FROM (
    SELECT project_uid, project_name, tenant_id, organization_uid, display_name, state,
        resource_version, created_at, updated_at
    FROM cloud_agents.projects
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND organization_uid = $1
        AND state <> 'archived'
        AND project_uid > $2
    ORDER BY project_uid
    LIMIT $3
) AS project`
)

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
				err := scanProject(handle.transaction.queryRow(readContext, getProjectSQL, projectID), tenantID, &result)
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

func (service *DurableCoordinationService) ListProjects(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	organizationID string,
	afterProjectUID string,
	limit int,
) (ProjectPage, error) {
	if service == nil || service.runner == nil {
		return ProjectPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(organizationID) ||
		afterProjectUID != "" && !validMutationIdentifier(afterProjectUID) || limit < 1 || limit > 200 {
		return ProjectPage{}, ErrCoordinationInvalidInput
	}
	var result ProjectPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeOrganization, ID: organizationID}
		operation, err := binder.Bind(tenantID, scope, "projects.list")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterProjectUID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, projectPageCursorIdentitySQL, organizationID, afterProjectUID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapMutationDatabaseError("project page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listProjectsSQL, organizationID, afterProjectUID, limit+1).Scan(&raw); err != nil {
					return mapMutationDatabaseError("projects", err)
				}
				result, err = decodeProjectPageRows(raw, tenantID, organizationID, limit)
				return err
			})
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func decodeProjectPageRows(raw []byte, tenantID, organizationID string, limit int) (ProjectPage, error) {
	var projects []Project
	if json.Unmarshal(raw, &projects) != nil || projects == nil || len(projects) > limit+1 {
		return ProjectPage{}, ErrCoordinationResultDrift
	}
	for _, project := range projects {
		if project.TenantID != tenantID || project.OrganizationID != organizationID || !validMutationIdentifier(project.UID) || !validMutationIdentifier(project.Name) ||
			!utf8.ValidString(project.DisplayName) || utf8.RuneCountInString(project.DisplayName) < 1 || utf8.RuneCountInString(project.DisplayName) > 160 ||
			project.State != "active" && project.State != "suspended" || project.ResourceVersion < 1 || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
			return ProjectPage{}, ErrCoordinationResultDrift
		}
	}
	result := ProjectPage{Projects: projects}
	if len(projects) > limit {
		result.Projects = projects[:limit]
		result.NextProjectUID = result.Projects[len(result.Projects)-1].UID
	}
	return result, nil
}

func scanProject(row rowScanner, tenantID string, result *Project) error {
	if err := row.Scan(
		&result.UID, &result.Name, &result.OrganizationID, &result.DisplayName, &result.State,
		&result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt,
	); err != nil {
		return err
	}
	result.TenantID = tenantID
	return nil
}
