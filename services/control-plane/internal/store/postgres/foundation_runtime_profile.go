package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type RuntimeProfilePage struct {
	Profiles             []internalcoordination.RuntimeProfileSnapshot
	NextProfileVersionID string
}

type PublishedRuntimeProfilePage struct {
	Profiles             []internalcoordination.RuntimeProfileSummary
	NextProfileVersionID string
}

type runtimeProfilePageRow struct {
	TenantID         string     `json:"tenant_id"`
	ProjectID        string     `json:"project_uid"`
	ProfileVersionID string     `json:"profile_version_uid"`
	ProfileID        string     `json:"profile_uid"`
	ProfileName      string     `json:"profile_name"`
	Version          int64      `json:"profile_version"`
	Description      string     `json:"description"`
	Status           string     `json:"status"`
	TargetID         string     `json:"target_uid"`
	NetworkPolicyID  string     `json:"network_policy_ref"`
	ImageURI         string     `json:"image_uri"`
	ReleaseDigest    string     `json:"release_digest"`
	CPUMillis        int64      `json:"cpu_millis"`
	MemoryBytes      int64      `json:"memory_bytes"`
	ResourceVersion  int64      `json:"resource_version"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	PublishedAt      *time.Time `json:"published_at"`
	DisabledAt       *time.Time `json:"disabled_at"`
}

type publishedRuntimeProfilePageRow struct {
	TenantID         string `json:"tenant_id"`
	ProjectID        string `json:"project_uid"`
	ProfileVersionID string `json:"profile_version_uid"`
	ProfileID        string `json:"profile_uid"`
	ProfileName      string `json:"profile_name"`
	Version          int64  `json:"profile_version"`
	Description      string `json:"description"`
	CPUMillis        int64  `json:"cpu_millis"`
	MemoryBytes      int64  `json:"memory_bytes"`
}

const runtimeProfileColumns = `profile_version_uid, profile_uid, profile_name, profile_version,
    description, status, target_uid, COALESCE(network_policy_ref, '') AS network_policy_ref,
    image_uri, release_digest, cpu_millis, memory_bytes,
    resource_version, created_at, updated_at, published_at, disabled_at`

var (
	createRuntimeProfileSQL = `SELECT ` + runtimeProfileColumns + `
FROM cloud_agents.create_runtime_profile_draft_v2($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`
	transitionRuntimeProfileSQL = `SELECT ` + runtimeProfileColumns + `
FROM cloud_agents.transition_runtime_profile_v2($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	getRuntimeProfileSQL = `SELECT ` + runtimeProfileColumns + `
FROM cloud_agents.runtime_profiles
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND profile_uid = $2 AND profile_version = $3`
	runtimeProfilePageCursorSQL = `SELECT 1 FROM cloud_agents.runtime_profiles
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND profile_version_uid = $2`
	listRuntimeProfilesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(profile_row)
    ORDER BY profile_row.profile_version_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, ` + runtimeProfileColumns + `
    FROM cloud_agents.runtime_profiles
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
        AND profile_version_uid > $2
    ORDER BY profile_version_uid
    LIMIT $3
) AS profile_row`
	publishedRuntimeProfilePageCursorSQL = `SELECT 1
FROM cloud_agents.runtime_profiles AS profile
JOIN cloud_agents.deployment_targets AS target
  ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
 AND target.target_uid = profile.target_uid
JOIN cloud_agents.network_policies AS policy
  ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
 AND policy.policy_uid = profile.network_policy_ref
WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = $1
    AND profile.profile_version_uid = $2 AND profile.status = 'published'
    AND policy.default_egress IN ('restricted', 'deny')
    AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
    AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
    AND target.target_kind = 'docker' AND target.observed_phase = 'ready'
    AND target.scheduling_state = 'active'`
	listPublishedRuntimeProfilesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(profile_row)
    ORDER BY profile_row.profile_version_uid), '[]'::jsonb)
FROM (
    SELECT profile.tenant_id, profile.project_uid, profile.profile_version_uid,
        profile.profile_uid, profile.profile_name, profile.profile_version,
        profile.description, profile.cpu_millis, profile.memory_bytes
    FROM cloud_agents.runtime_profiles AS profile
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
     AND target.target_uid = profile.target_uid
    JOIN cloud_agents.network_policies AS policy
      ON policy.tenant_id = profile.tenant_id AND policy.project_uid = profile.project_uid
     AND policy.policy_uid = profile.network_policy_ref
    WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = $1
        AND profile.profile_version_uid > $2 AND profile.status = 'published'
        AND policy.default_egress IN ('restricted', 'deny')
        AND policy.allowlist_policy_ref IS NULL AND policy.dns_policy_ref IS NULL
        AND policy.proxy_policy_ref IS NULL AND NOT policy.ingress_enabled
        AND target.target_kind = 'docker' AND target.observed_phase = 'ready'
        AND target.scheduling_state = 'active'
    ORDER BY profile.profile_version_uid
    LIMIT $3
) AS profile_row`
	createFoundationSandboxSQL = `SELECT operation_uid, workspace_uid, sandbox_uid, profile_uid,
    profile_version, generation, desired_state, observed_state, ttl_seconds, expires_at
FROM cloud_agents.accept_foundation_sandbox_v3($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
)

func (service *DurableCoordinationService) CreateRuntimeProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalcoordination.RuntimeProfileCreateInput,
) (internalcoordination.RuntimeProfileSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalcoordination.RuntimeProfileCreateDigest(input)
	if err != nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalcoordination.RuntimeProfileSnapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		return scanRuntimeProfile(handle.transaction.queryRow(operationContext, createRuntimeProfileSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID, input.ProfileName, input.Version,
			input.Description, input.TargetID, input.NetworkPolicyID, input.ImageURI, input.ReleaseDigest, input.CPUMillis,
			input.MemoryBytes, input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) TransitionRuntimeProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalcoordination.RuntimeProfileTransitionInput,
) (internalcoordination.RuntimeProfileSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalcoordination.RuntimeProfileTransitionDigest(input)
	if err != nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalcoordination.RuntimeProfileSnapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		return scanRuntimeProfile(handle.transaction.queryRow(operationContext, transitionRuntimeProfileSQL,
			input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID, input.Version,
			input.ExpectedResourceVersion, input.Action, input.Mutation.IdempotencyKey,
			digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) GetRuntimeProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, profileID string, version int64,
) (internalcoordination.RuntimeProfileSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(profileID) || version < 1 || version > 2147483647 {
		return internalcoordination.RuntimeProfileSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalcoordination.RuntimeProfileSnapshot
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
		err := scanRuntimeProfile(handle.transaction.queryRow(operationContext, getRuntimeProfileSQL, projectID, profileID, version),
			internalcoordination.FoundationScope{TenantID: tenantID, ProjectID: projectID}, &result)
		if errors.Is(err, pgx.ErrNoRows) {
			return internalcoordination.ErrRuntimeProfileNotFound
		}
		return err
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) ListRuntimeProfiles(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterProfileVersionID string, limit int,
) (RuntimeProfilePage, error) {
	if service == nil || service.runner == nil {
		return RuntimeProfilePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationPageInput(tenantID, projectID, afterProfileVersionID, limit) {
		return RuntimeProfilePage{}, ErrCoordinationInvalidInput
	}
	var result RuntimeProfilePage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
		if err := validateRuntimeProfileCursor(operationContext, handle, runtimeProfilePageCursorSQL, projectID, afterProfileVersionID); err != nil {
			return err
		}
		var raw []byte
		if err := handle.transaction.queryRow(operationContext, listRuntimeProfilesSQL, projectID, afterProfileVersionID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var err error
		result, err = decodeRuntimeProfilePageRows(raw, tenantID, projectID, limit)
		return err
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) ListPublishedRuntimeProfiles(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterProfileVersionID string, limit int,
) (PublishedRuntimeProfilePage, error) {
	if service == nil || service.runner == nil {
		return PublishedRuntimeProfilePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validFoundationPageInput(tenantID, projectID, afterProfileVersionID, limit) {
		return PublishedRuntimeProfilePage{}, ErrCoordinationInvalidInput
	}
	var result PublishedRuntimeProfilePage
	err := service.withFoundationOperation(ctx, tenantID, principal, projectID, "projects.get", false, func(operationContext context.Context, handle *tenantReadHandle, _ string) error {
		if err := validateRuntimeProfileCursor(operationContext, handle, publishedRuntimeProfilePageCursorSQL, projectID, afterProfileVersionID); err != nil {
			return err
		}
		var raw []byte
		if err := handle.transaction.queryRow(operationContext, listPublishedRuntimeProfilesSQL, projectID, afterProfileVersionID, limit+1).Scan(&raw); err != nil {
			return err
		}
		var err error
		result, err = decodePublishedRuntimeProfilePageRows(raw, tenantID, projectID, limit)
		return err
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) CreateFoundationSandbox(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalcoordination.FoundationSandboxCreateInput,
) (internalcoordination.FoundationSandboxSnapshot, error) {
	if service == nil || service.runner == nil {
		return internalcoordination.FoundationSandboxSnapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalcoordination.FoundationSandboxSnapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalcoordination.FoundationSandboxCreateDigest(input)
	if err != nil {
		return internalcoordination.FoundationSandboxSnapshot{}, ErrCoordinationInvalidInput
	}
	var result internalcoordination.FoundationSandboxSnapshot
	err = service.withFoundationOperation(ctx, tenantID, principal, input.Scope.ProjectID, "projects.act", true, func(operationContext context.Context, handle *tenantReadHandle, subjectDigest string) error {
		row := handle.transaction.queryRow(operationContext, createFoundationSandboxSQL, input.Scope.ProjectID,
			input.WorkspaceID, input.WorkspaceName, input.SandboxID, input.RuntimeProfileID,
			input.RuntimeProfileVersion, subjectDigest, input.Mutation.IdempotencyKey, digest, input.TTLSeconds)
		if err := row.Scan(&result.OperationID, &result.WorkspaceID, &result.SandboxID,
			&result.RuntimeProfileID, &result.RuntimeProfileVersion, &result.Generation,
			&result.DesiredState, &result.ObservedState, &result.TTLSeconds, &result.ExpiresAt); err != nil {
			return err
		}
		result.Scope = input.Scope
		if result.Validate() != nil {
			return ErrCoordinationResultDrift
		}
		return nil
	})
	return result, mapRuntimeProfileError(err)
}

func (service *DurableCoordinationService) withFoundationOperation(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, permission string, mutation bool,
	operation func(context.Context, *tenantReadHandle, string) error,
) error {
	return authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
		verified, err := binder.Bind(tenantID, scope, permission)
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		subjectDigest := ""
		if mutation {
			actor, ok := verified.Actor()
			if !ok {
				return authz.ErrOperationDenied
			}
			subjectDigest, err = actor.Digest()
			if err != nil {
				return authz.ErrOperationDenied
			}
		}
		if mutation {
			return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
				return executeVerifiedRBACOperation(ctx, handle, verified, scope, func() error { return operation(ctx, handle, subjectDigest) })
			})
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, verified, scope, func() error { return operation(readContext, handle, subjectDigest) })
		})
	})
}

func validFoundationPageInput(tenantID, projectID, after string, limit int) bool {
	return validMutationIdentifier(tenantID) && validMutationIdentifier(projectID) &&
		(after == "" || validMutationIdentifier(after)) && limit >= 1 && limit <= 200
}

func validateRuntimeProfileCursor(ctx context.Context, handle *tenantReadHandle, query, projectID, after string) error {
	if after == "" {
		return nil
	}
	var exists int
	if err := handle.transaction.queryRow(ctx, query, projectID, after).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCoordinationInvalidInput
		}
		return err
	}
	return nil
}

func scanRuntimeProfile(row rowScanner, scope internalcoordination.FoundationScope, result *internalcoordination.RuntimeProfileSnapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	var createdAt, updatedAt time.Time
	var publishedAt, disabledAt *time.Time
	if err := row.Scan(&result.ProfileVersionID, &result.ProfileID, &result.ProfileName, &result.Version,
		&result.Description, &result.Status, &result.TargetID, &result.NetworkPolicyID, &result.ImageURI, &result.ReleaseDigest,
		&result.CPUMillis, &result.MemoryBytes, &result.ResourceVersion, &createdAt, &updatedAt,
		&publishedAt, &disabledAt); err != nil {
		return err
	}
	result.Scope, result.CreatedAt, result.UpdatedAt = scope, createdAt, updatedAt
	if publishedAt != nil {
		result.PublishedAt = publishedAt
	}
	if disabledAt != nil {
		result.DisabledAt = disabledAt
	}
	if result.Validate() != nil {
		return fmt.Errorf("%w: runtime profile projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeRuntimeProfilePageRows(raw []byte, tenantID, projectID string, limit int) (RuntimeProfilePage, error) {
	var rows []runtimeProfilePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return RuntimeProfilePage{}, ErrCoordinationResultDrift
	}
	profiles := make([]internalcoordination.RuntimeProfileSnapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := runtimeProfileSnapshot(row)
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return RuntimeProfilePage{}, ErrCoordinationResultDrift
		}
		profiles = append(profiles, snapshot)
	}
	result := RuntimeProfilePage{Profiles: profiles}
	if len(profiles) > limit {
		result.Profiles = profiles[:limit]
		result.NextProfileVersionID = result.Profiles[len(result.Profiles)-1].ProfileVersionID
	}
	return result, nil
}

func decodePublishedRuntimeProfilePageRows(raw []byte, tenantID, projectID string, limit int) (PublishedRuntimeProfilePage, error) {
	var rows []publishedRuntimeProfilePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return PublishedRuntimeProfilePage{}, ErrCoordinationResultDrift
	}
	profiles := make([]internalcoordination.RuntimeProfileSummary, 0, len(rows))
	for _, row := range rows {
		summary := internalcoordination.RuntimeProfileSummary{
			Scope:            internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			ProfileVersionID: row.ProfileVersionID, ProfileID: row.ProfileID, ProfileName: row.ProfileName,
			Version: row.Version, Description: row.Description, CPUMillis: row.CPUMillis, MemoryBytes: row.MemoryBytes,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || summary.Validate() != nil {
			return PublishedRuntimeProfilePage{}, ErrCoordinationResultDrift
		}
		profiles = append(profiles, summary)
	}
	result := PublishedRuntimeProfilePage{Profiles: profiles}
	if len(profiles) > limit {
		result.Profiles = profiles[:limit]
		result.NextProfileVersionID = result.Profiles[len(result.Profiles)-1].ProfileVersionID
	}
	return result, nil
}

func runtimeProfileSnapshot(row runtimeProfilePageRow) internalcoordination.RuntimeProfileSnapshot {
	result := internalcoordination.RuntimeProfileSnapshot{
		Scope:            internalcoordination.FoundationScope{TenantID: row.TenantID, ProjectID: row.ProjectID},
		ProfileVersionID: row.ProfileVersionID, ProfileID: row.ProfileID, ProfileName: row.ProfileName,
		Version: row.Version, Description: row.Description, Status: row.Status, TargetID: row.TargetID,
		NetworkPolicyID: row.NetworkPolicyID,
		ImageURI:        row.ImageURI, ReleaseDigest: row.ReleaseDigest, CPUMillis: row.CPUMillis,
		MemoryBytes: row.MemoryBytes, ResourceVersion: row.ResourceVersion,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.PublishedAt != nil {
		result.PublishedAt = row.PublishedAt
	}
	if row.DisabledAt != nil {
		result.DisabledAt = row.DisabledAt
	}
	return result
}

func mapRuntimeProfileError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23503" && pgErr.Message == "foundation sandbox was not found":
			return internalcoordination.ErrFoundationSandboxNotFound
		case pgErr.Code == "23503" && pgErr.Message == "runtime profile was not found":
			return internalcoordination.ErrRuntimeProfileNotFound
		case pgErr.Code == "23503":
			return internalcoordination.ErrRuntimeProfileUnavailable
		case pgErr.Code == "23505" && pgErr.Message == "runtime profile is not available":
			return internalcoordination.ErrRuntimeProfileUnavailable
		case pgErr.Code == "23505" && (pgErr.Message == "foundation sandbox profile conflict" ||
			pgErr.Message == "foundation sandbox transition conflict" ||
			pgErr.Message == "foundation sandbox idempotency conflict"):
			return internalcoordination.ErrFoundationSandboxConflict
		case pgErr.Code == "23505":
			return internalcoordination.ErrRuntimeProfileConflict
		}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, internalcoordination.ErrRuntimeProfileNotFound),
		errors.Is(err, internalcoordination.ErrRuntimeProfileConflict),
		errors.Is(err, internalcoordination.ErrRuntimeProfileUnavailable),
		errors.Is(err, internalcoordination.ErrFoundationSandboxConflict),
		errors.Is(err, internalcoordination.ErrFoundationSandboxNotFound):
		return err
	default:
		return mapCoordinationDatabaseError("foundation runtime profile", err)
	}
}
