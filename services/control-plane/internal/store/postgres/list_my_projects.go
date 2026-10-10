package postgres

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

const myProjectsScanBatchSize = 201

const myProjectsCursorIdentitySQL = `SELECT 1
FROM cloud_agents.projects AS project
JOIN cloud_agents.organizations AS organization
    ON organization.tenant_id = project.tenant_id
    AND organization.organization_uid = project.organization_uid
JOIN cloud_agents.platform_tenants AS tenant
    ON tenant.tenant_id = project.tenant_id
    AND tenant.tenant_uid = project.tenant_id
WHERE project.tenant_id = cloud_agents.require_tenant_id()
    AND project.project_uid = $1
    AND tenant.state = 'active'
    AND organization.state = 'active'
    AND project.state = 'active'`

const listMyProjectsBatchSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(project)
    ORDER BY project.project_uid), '[]'::jsonb)
FROM (
    SELECT project.project_uid, project.project_name, project.tenant_id,
        project.organization_uid, project.display_name, project.state,
        project.resource_version, project.created_at, project.updated_at
    FROM cloud_agents.projects AS project
    JOIN cloud_agents.organizations AS organization
        ON organization.tenant_id = project.tenant_id
        AND organization.organization_uid = project.organization_uid
    JOIN cloud_agents.platform_tenants AS tenant
        ON tenant.tenant_id = project.tenant_id
        AND tenant.tenant_uid = project.tenant_id
    WHERE project.tenant_id = cloud_agents.require_tenant_id()
        AND tenant.state = 'active'
        AND organization.state = 'active'
        AND project.state = 'active'
        AND project.project_uid > $1
    ORDER BY project.project_uid
    LIMIT $2
) AS project`

type MyProjectsCursor struct {
	AfterProjectUID string
	SubjectKey      string
}

type MyProjectsPage struct {
	Projects       []Project
	NextProjectUID string
	SubjectKey     string
}

func (service *DurableCoordinationService) ListMyProjects(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	cursor MyProjectsCursor,
	limit int,
) (MyProjectsPage, error) {
	if service == nil || service.runner == nil {
		return MyProjectsPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || limit < 1 || limit > 200 ||
		(cursor.AfterProjectUID == "") != (cursor.SubjectKey == "") ||
		cursor.AfterProjectUID != "" && !validMutationIdentifier(cursor.AfterProjectUID) ||
		cursor.SubjectKey != "" && !validSubjectCursorKey(cursor.SubjectKey) {
		return MyProjectsPage{}, ErrCoordinationInvalidInput
	}
	var result MyProjectsPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeTenant, ID: tenantID}
		operation, err := binder.Bind(tenantID, scope, "projects.list")
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		actor, ok := operation.Actor()
		if !ok {
			return ErrMutationDenied
		}
		digest, err := actor.Digest()
		if err != nil || !strings.HasPrefix(digest, "sha256:") {
			return ErrMutationDenied
		}
		subjectKey := strings.TrimPrefix(digest, "sha256:")
		if !validSubjectCursorKey(subjectKey) || cursor.SubjectKey != "" && cursor.SubjectKey != subjectKey {
			return ErrCoordinationInvalidInput
		}
		transactionErr := service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			page, err := executeMyProjectsSelection(readContext, handle, operation, actor, cursor.AfterProjectUID, limit)
			if err == nil {
				page.SubjectKey = subjectKey
				result = page
			}
			return err
		})
		return mapVerifiedCoordinationAuthorizationError(transactionErr)
	})
	return result, err
}

func executeMyProjectsSelection(
	ctx context.Context,
	handle *tenantReadHandle,
	operation *authz.VerifiedOperation,
	actor authz.SubjectRef,
	afterProjectUID string,
	limit int,
) (MyProjectsPage, error) {
	if handle == nil {
		return MyProjectsPage{}, ErrTenantCapabilityClosed
	}
	handle.mutex.Lock()
	defer handle.mutex.Unlock()
	if ctx == nil || operation == nil || !handle.active || handle.transaction == nil || handle.tenantID == "" || handle.clock == nil || handle.application != "user" {
		return MyProjectsPage{}, authz.ErrOperationDenied
	}
	if afterProjectUID != "" {
		var exists int
		if err := handle.transaction.queryRow(ctx, myProjectsCursorIdentitySQL, afterProjectUID).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return MyProjectsPage{}, ErrCoordinationInvalidInput
			}
			return MyProjectsPage{}, mapMutationDatabaseError("my projects cursor", err)
		}
	}
	base, now, err := handle.authorizationSnapshot(ctx, actor, authz.ScopeRef{Level: authz.ScopeTenant, ID: handle.tenantID})
	if err != nil {
		return MyProjectsPage{}, err
	}
	selected := make([]Project, 0, limit+1)
	scanAfter := afterProjectUID
	var batch []Project
	err = operation.ExecuteProjectSelector(base, now, func() ([]authz.ScopePath, bool, error) {
		var raw []byte
		if err := handle.transaction.queryRow(ctx, listMyProjectsBatchSQL, scanAfter, myProjectsScanBatchSize).Scan(&raw); err != nil {
			return nil, false, mapMutationDatabaseError("my projects", err)
		}
		var decodeErr error
		batch, decodeErr = decodeMyProjectsBatch(raw, handle.tenantID)
		if decodeErr != nil {
			return nil, false, decodeErr
		}
		done := len(batch) < myProjectsScanBatchSize
		scopes := make([]authz.ScopePath, len(batch))
		for index, project := range batch {
			scopes[index] = authz.ScopePath{Level: authz.ScopeProject, TenantID: project.TenantID, OrganizationID: project.OrganizationID, ProjectID: project.UID}
		}
		if len(batch) != 0 {
			scanAfter = batch[len(batch)-1].UID
		}
		return scopes, done, nil
	}, func(allowed []bool) (bool, error) {
		if len(allowed) != len(batch) {
			return false, ErrCoordinationResultDrift
		}
		for index, permitted := range allowed {
			if permitted {
				selected = append(selected, batch[index])
				if len(selected) > limit {
					return true, nil
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return MyProjectsPage{}, err
	}
	result := MyProjectsPage{Projects: selected}
	if len(selected) > limit {
		result.Projects = selected[:limit]
		result.NextProjectUID = result.Projects[len(result.Projects)-1].UID
	}
	return result, nil
}

func decodeMyProjectsBatch(raw []byte, tenantID string) ([]Project, error) {
	var projects []Project
	if json.Unmarshal(raw, &projects) != nil || projects == nil || len(projects) > myProjectsScanBatchSize {
		return nil, ErrCoordinationResultDrift
	}
	for index, project := range projects {
		if project.TenantID != tenantID || !validMutationIdentifier(project.UID) || !validMutationIdentifier(project.Name) ||
			!validMutationIdentifier(project.OrganizationID) || !utf8.ValidString(project.DisplayName) ||
			utf8.RuneCountInString(project.DisplayName) < 1 || utf8.RuneCountInString(project.DisplayName) > 160 ||
			project.State != "active" || project.ResourceVersion < 1 || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() ||
			index > 0 && projects[index-1].UID >= project.UID {
			return nil, ErrCoordinationResultDrift
		}
	}
	return projects, nil
}

func validSubjectCursorKey(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}
