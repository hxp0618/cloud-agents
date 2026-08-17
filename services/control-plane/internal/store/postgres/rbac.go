package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/jackc/pgx/v5"
)

const (
	maxCatalogJSONBytes        = 256 << 10
	maxCandidateJSONBytes      = 1 << 20
	maxAuthorizationCandidates = 256
)

const resolveAuthorizationScopeSQL = `SELECT
    resolved.scope_level,
    resolved.tenant_id,
    resolved.organization_uid,
    resolved.project_uid
FROM (
    SELECT
        'tenant'::text AS scope_level,
        tenant.tenant_id,
        NULL::text AS organization_uid,
        NULL::text AS project_uid
    FROM cloud_agents.platform_tenants AS tenant
    WHERE $1 = 'tenant'
        AND tenant.tenant_id = cloud_agents.require_tenant_id()
        AND tenant.tenant_uid = $2
        AND tenant.state = 'active'

    UNION ALL

    SELECT
        'organization'::text AS scope_level,
        organization.tenant_id,
        organization.organization_uid,
        NULL::text AS project_uid
    FROM cloud_agents.organizations AS organization
    JOIN cloud_agents.platform_tenants AS tenant
        ON tenant.tenant_id = organization.tenant_id
        AND tenant.tenant_uid = organization.tenant_id
    WHERE $1 = 'organization'
        AND organization.tenant_id = cloud_agents.require_tenant_id()
        AND organization.organization_uid = $2
        AND tenant.state = 'active'
        AND organization.state = 'active'

    UNION ALL

    SELECT
        'project'::text AS scope_level,
        project.tenant_id,
        project.organization_uid,
        project.project_uid
    FROM cloud_agents.projects AS project
    JOIN cloud_agents.organizations AS organization
        ON organization.tenant_id = project.tenant_id
        AND organization.organization_uid = project.organization_uid
    JOIN cloud_agents.platform_tenants AS tenant
        ON tenant.tenant_id = project.tenant_id
        AND tenant.tenant_uid = project.tenant_id
    WHERE $1 = 'project'
        AND project.tenant_id = cloud_agents.require_tenant_id()
        AND project.project_uid = $2
        AND tenant.state = 'active'
        AND organization.state = 'active'
        AND project.state = 'active'
) AS resolved
LIMIT 1`

const readBuiltinRoleCatalogSQL = `SELECT COALESCE(
    pg_catalog.jsonb_agg(role_row.document ORDER BY role_row.role_name, role_row.role_version),
    '[]'::jsonb
)
FROM (
    SELECT
        role.role_name,
        role.role_version,
        pg_catalog.jsonb_build_object(
            'name', role.role_name,
            'version', role.role_version,
            'catalog_revision', role.catalog_revision,
            'scope_level', role.scope_level,
            'state', role.state,
            'published_at', role.published_at,
            'permissions', COALESCE(
                (
                    SELECT pg_catalog.jsonb_agg(permission_row.permission ORDER BY permission_row.permission)
                    FROM (
                        SELECT permission.permission
                        FROM cloud_agents.builtin_role_permissions AS permission
                        WHERE permission.role_name = role.role_name
                            AND permission.role_version = role.role_version
                        ORDER BY permission.permission
                        LIMIT 36
                    ) AS permission_row
                ),
                '[]'::jsonb
            )
        ) AS document
    FROM cloud_agents.builtin_roles AS role
    ORDER BY role.role_name, role.role_version
    LIMIT 8
) AS role_row`

const readAuthorizationCandidatesSQL = `WITH candidate_rows AS (
    SELECT
        membership.membership_uid,
        binding.role_binding_uid,
        pg_catalog.jsonb_build_object(
            'membership', pg_catalog.jsonb_build_object(
                'uid', membership.membership_uid,
                'subject_kind', membership.subject_kind,
                'subject_issuer', membership.subject_issuer,
                'subject_value', membership.subject_value,
                'subject_digest', membership.subject_digest,
                'scope', pg_catalog.jsonb_build_object(
                    'level', membership.scope_level,
                    'tenant_id', membership.tenant_id,
                    'organization_id', CASE
                        WHEN membership.scope_level = 'organization' THEN membership.scope_organization_uid
                        WHEN membership.scope_level = 'project' THEN membership_project.organization_uid
                        ELSE NULL
                    END,
                    'project_id', membership.scope_project_uid
                ),
                'state', membership.state,
                'expires_at', membership.expires_at
            ),
            'binding', pg_catalog.jsonb_build_object(
                'uid', binding.role_binding_uid,
                'subject_kind', binding.subject_kind,
                'subject_issuer', binding.subject_issuer,
                'subject_value', binding.subject_value,
                'subject_digest', binding.subject_digest,
                'role_name', binding.role_name,
                'role_version', binding.role_version,
                'scope', pg_catalog.jsonb_build_object(
                    'level', binding.scope_level,
                    'tenant_id', binding.tenant_id,
                    'organization_id', CASE
                        WHEN binding.scope_level = 'organization' THEN binding.scope_organization_uid
                        WHEN binding.scope_level = 'project' THEN binding_project.organization_uid
                        ELSE NULL
                    END,
                    'project_id', binding.scope_project_uid
                ),
                'state', binding.state,
                'expires_at', binding.expires_at
            )
        ) AS document
    FROM cloud_agents.memberships AS membership
    JOIN cloud_agents.role_bindings AS binding
        ON binding.tenant_id = membership.tenant_id
        AND binding.subject_kind = membership.subject_kind
        AND binding.subject_issuer = membership.subject_issuer
        AND binding.subject_value = membership.subject_value
        AND membership.resource_version < binding.resource_version
    LEFT JOIN cloud_agents.projects AS membership_project
        ON membership.scope_level = 'project'
        AND membership_project.tenant_id = membership.tenant_id
        AND membership_project.project_uid = membership.scope_project_uid
    LEFT JOIN cloud_agents.projects AS binding_project
        ON binding.scope_level = 'project'
        AND binding_project.tenant_id = binding.tenant_id
        AND binding_project.project_uid = binding.scope_project_uid
    WHERE membership.tenant_id = cloud_agents.require_tenant_id()
        AND membership.subject_kind = $1
        AND membership.subject_issuer = $2
        AND membership.subject_value = $3
    ORDER BY membership.membership_uid, binding.role_binding_uid
    LIMIT 257
)
SELECT COALESCE(
    pg_catalog.jsonb_agg(document ORDER BY membership_uid, role_binding_uid),
    '[]'::jsonb
)
FROM candidate_rows`

func (handle *tenantReadHandle) Authorize(
	ctx context.Context,
	request authz.Request,
) (authz.Decision, error) {
	handle.mutex.Lock()
	defer handle.mutex.Unlock()

	if ctx == nil {
		return authz.Decision{}, ErrNilContext
	}
	if !handle.active || handle.transaction == nil || handle.tenantID == "" || handle.clock == nil {
		return authz.Decision{}, ErrTenantCapabilityClosed
	}
	now := handle.clock().UTC()
	base := authz.Snapshot{TenantID: handle.tenantID}
	if request.Subject.Validate() != nil || request.Resource.Validate(handle.tenantID) != nil || request.Resource.Level == authz.ScopePlatform {
		return authz.Evaluate(base, request, now)
	}

	scope, resolved, err := handle.resolveAuthorizationScope(ctx, request.Resource)
	if err != nil {
		return authz.Decision{}, err
	}
	base.Scope = scope
	base.ScopeResolved = resolved
	if !resolved {
		return authz.Evaluate(base, request, now)
	}

	catalog, err := handle.readBuiltinRoleCatalog(ctx)
	if err != nil {
		return authz.Decision{}, err
	}
	base.Catalog = catalog
	candidates, err := handle.readAuthorizationCandidates(ctx, request.Subject)
	if err != nil {
		return authz.Decision{}, err
	}
	base.Candidates = candidates
	return authz.Evaluate(base, request, now)
}

func (handle *tenantReadHandle) resolveAuthorizationScope(
	ctx context.Context,
	request authz.ScopeRef,
) (authz.ScopePath, bool, error) {
	var level string
	var tenantID string
	var organizationID *string
	var projectID *string
	err := handle.transaction.queryRow(
		ctx,
		resolveAuthorizationScopeSQL,
		string(request.Level),
		request.ID,
	).Scan(&level, &tenantID, &organizationID, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ScopePath{}, false, nil
	}
	if err != nil {
		return authz.ScopePath{}, false, fmt.Errorf("resolve authorization scope: %w", err)
	}
	path := authz.ScopePath{Level: authz.ScopeLevel(level), TenantID: tenantID}
	if organizationID != nil {
		path.OrganizationID = *organizationID
	}
	if projectID != nil {
		path.ProjectID = *projectID
	}
	return path, true, nil
}

func (handle *tenantReadHandle) readBuiltinRoleCatalog(ctx context.Context) (authz.Catalog, error) {
	var raw []byte
	if err := handle.transaction.queryRow(ctx, readBuiltinRoleCatalogSQL).Scan(&raw); err != nil {
		return authz.Catalog{}, fmt.Errorf("read builtin role catalog: %w", err)
	}
	rows, err := decodeBoundedJSON[[]databaseCatalogRole](raw, maxCatalogJSONBytes)
	if err != nil {
		return authz.Catalog{}, fmt.Errorf("decode builtin role catalog: %w", err)
	}
	roles := make([]authz.Role, len(rows))
	for index, row := range rows {
		roles[index] = authz.Role{
			Name:            row.Name,
			Version:         row.Version,
			CatalogRevision: row.CatalogRevision,
			ScopeLevel:      authz.ScopeLevel(row.ScopeLevel),
			State:           row.State,
			PublishedAt:     row.PublishedAt.UTC().Format(time.RFC3339Nano),
			Permissions:     append([]string(nil), row.Permissions...),
		}
	}
	return authz.Catalog{Roles: roles}, nil
}

func (handle *tenantReadHandle) readAuthorizationCandidates(
	ctx context.Context,
	subject authz.SubjectRef,
) ([]authz.Candidate, error) {
	var raw []byte
	if err := handle.transaction.queryRow(
		ctx,
		readAuthorizationCandidatesSQL,
		subject.Kind,
		subject.Issuer,
		subject.Subject,
	).Scan(&raw); err != nil {
		return nil, fmt.Errorf("read authorization candidates: %w", err)
	}
	rows, err := decodeBoundedJSON[[]databaseCandidate](raw, maxCandidateJSONBytes)
	if err != nil {
		return nil, fmt.Errorf("decode authorization candidates: %w", err)
	}
	if len(rows) > maxAuthorizationCandidates {
		return nil, fmt.Errorf("authorization candidate limit exceeded")
	}
	candidates := make([]authz.Candidate, len(rows))
	for index, row := range rows {
		candidates[index] = authz.Candidate{
			Membership: authz.MembershipFact{
				UID:         row.Membership.UID,
				Subject:     row.Membership.subject(),
				SubjectHash: row.Membership.SubjectDigest,
				Scope:       row.Membership.Scope.path(),
				State:       row.Membership.State,
				ExpiresAt:   cloneTime(row.Membership.ExpiresAt),
			},
			Binding: authz.RoleBindingFact{
				UID:         row.Binding.UID,
				Subject:     row.Binding.subject(),
				SubjectHash: row.Binding.SubjectDigest,
				RoleName:    row.Binding.RoleName,
				RoleVersion: row.Binding.RoleVersion,
				Scope:       row.Binding.Scope.path(),
				State:       row.Binding.State,
				ExpiresAt:   cloneTime(row.Binding.ExpiresAt),
			},
		}
	}
	return candidates, nil
}

type databaseCatalogRole struct {
	Name            string    `json:"name"`
	Version         int64     `json:"version"`
	CatalogRevision int64     `json:"catalog_revision"`
	ScopeLevel      string    `json:"scope_level"`
	State           string    `json:"state"`
	PublishedAt     time.Time `json:"published_at"`
	Permissions     []string  `json:"permissions"`
}

type databaseCandidate struct {
	Membership databaseMembership `json:"membership"`
	Binding    databaseBinding    `json:"binding"`
}

type databaseMembership struct {
	UID           string        `json:"uid"`
	SubjectKind   string        `json:"subject_kind"`
	SubjectIssuer string        `json:"subject_issuer"`
	SubjectValue  string        `json:"subject_value"`
	SubjectDigest string        `json:"subject_digest"`
	Scope         databaseScope `json:"scope"`
	State         string        `json:"state"`
	ExpiresAt     *time.Time    `json:"expires_at"`
}

func (row databaseMembership) subject() authz.SubjectRef {
	return authz.SubjectRef{Kind: row.SubjectKind, Issuer: row.SubjectIssuer, Subject: row.SubjectValue}
}

type databaseBinding struct {
	UID           string        `json:"uid"`
	SubjectKind   string        `json:"subject_kind"`
	SubjectIssuer string        `json:"subject_issuer"`
	SubjectValue  string        `json:"subject_value"`
	SubjectDigest string        `json:"subject_digest"`
	RoleName      string        `json:"role_name"`
	RoleVersion   int64         `json:"role_version"`
	Scope         databaseScope `json:"scope"`
	State         string        `json:"state"`
	ExpiresAt     *time.Time    `json:"expires_at"`
}

func (row databaseBinding) subject() authz.SubjectRef {
	return authz.SubjectRef{Kind: row.SubjectKind, Issuer: row.SubjectIssuer, Subject: row.SubjectValue}
}

type databaseScope struct {
	Level          string  `json:"level"`
	TenantID       string  `json:"tenant_id"`
	OrganizationID *string `json:"organization_id"`
	ProjectID      *string `json:"project_id"`
}

func (scope databaseScope) path() authz.ScopePath {
	path := authz.ScopePath{Level: authz.ScopeLevel(scope.Level)}
	if path.Level != authz.ScopePlatform {
		path.TenantID = scope.TenantID
	}
	if scope.OrganizationID != nil {
		path.OrganizationID = *scope.OrganizationID
	}
	if scope.ProjectID != nil {
		path.ProjectID = *scope.ProjectID
	}
	return path
}

func decodeBoundedJSON[T any](raw []byte, maximum int) (T, error) {
	var zero T
	if len(raw) == 0 || len(raw) > maximum {
		return zero, fmt.Errorf("JSON size is outside the bounded profile")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return zero, fmt.Errorf("JSON has a trailing value")
		}
		return zero, err
	}
	return value, nil
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
