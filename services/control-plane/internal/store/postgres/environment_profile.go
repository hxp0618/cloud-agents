package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type EnvironmentProfilePage struct {
	EnvironmentProfiles  []internalenvironmentprofile.Snapshot
	NextProfileVersionID string
}

type PublishedEnvironmentProfilePage struct {
	EnvironmentProfiles  []internalenvironmentprofile.Summary
	NextProfileVersionID string
}

type EnvironmentProfileAuditPage struct {
	Events         []internalenvironmentprofile.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type environmentProfilePageRow struct {
	TenantID              string     `json:"tenant_id"`
	ProjectID             string     `json:"project_uid"`
	ProfileVersionUID     string     `json:"profile_version_uid"`
	ProfileID             string     `json:"profile_uid"`
	ProfileName           string     `json:"profile_name"`
	Version               int64      `json:"profile_version"`
	Description           string     `json:"description"`
	Status                string     `json:"status"`
	ProviderKinds         []string   `json:"provider_kinds"`
	CPULimitMillis        int64      `json:"cpu_limit_millis"`
	MemoryLimitBytes      int64      `json:"memory_limit_bytes"`
	StoragePolicyRef      string     `json:"storage_policy_ref"`
	NetworkPolicyRef      string     `json:"network_policy_ref"`
	ReleaseDigest         string     `json:"release_digest"`
	TargetRefs            []string   `json:"target_refs"`
	ProviderCredentialRef string     `json:"provider_credential_ref"`
	ResourceVersion       int64      `json:"resource_version"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	PublishedAt           *time.Time `json:"published_at"`
	DisabledAt            *time.Time `json:"disabled_at"`
}

type publishedEnvironmentProfilePageRow struct {
	TenantID          string   `json:"tenant_id"`
	ProjectID         string   `json:"project_uid"`
	ProfileVersionUID string   `json:"profile_version_uid"`
	ProfileID         string   `json:"profile_uid"`
	ProfileName       string   `json:"profile_name"`
	Version           int64    `json:"profile_version"`
	Description       string   `json:"description"`
	ProviderKinds     []string `json:"provider_kinds"`
	CPULimitMillis    int64    `json:"cpu_limit_millis"`
	MemoryLimitBytes  int64    `json:"memory_limit_bytes"`
}

type environmentProfileAuditPageRow struct {
	TenantID          string    `json:"tenant_id"`
	ProjectID         string    `json:"project_uid"`
	ProfileVersionUID string    `json:"profile_version_uid"`
	EventID           string    `json:"event_uid"`
	OperationID       string    `json:"operation_uid"`
	Actor             string    `json:"subject_digest"`
	Action            string    `json:"action"`
	ProfileVersion    int64     `json:"profile_version"`
	Result            string    `json:"result"`
	RequestID         string    `json:"request_id"`
	OccurredAt        time.Time `json:"occurred_at"`
}

const environmentProfileColumns = `profile_version_uid, profile_uid, profile_name, profile_version,
    description, status, provider_kinds, cpu_limit_millis, memory_limit_bytes,
    storage_policy_ref, network_policy_ref, release_digest, target_refs, provider_credential_ref,
    resource_version, created_at, updated_at, published_at, disabled_at`

var (
	createEnvironmentProfileSQL = `SELECT ` + environmentProfileColumns + `
FROM cloud_agents.create_environment_profile_draft_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
	transitionEnvironmentProfileSQL = `SELECT ` + environmentProfileColumns + `
FROM cloud_agents.transition_environment_profile_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	getEnvironmentProfileSQL = `SELECT ` + environmentProfileColumns + `
FROM cloud_agents.environment_profiles
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND profile_uid = $2 AND profile_version = $3`
	environmentProfilePageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.environment_profiles
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND profile_version_uid = $2`
	listEnvironmentProfilesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(profile_row)
    ORDER BY profile_row.profile_version_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, ` + environmentProfileColumns + `
    FROM cloud_agents.environment_profiles
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
        AND profile_version_uid > $2
    ORDER BY profile_version_uid
    LIMIT $3
) AS profile_row`
	publishedEnvironmentProfilePageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.environment_profiles AS profile
WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = $1
    AND profile.profile_version_uid = $2 AND profile.status = 'published'
    AND EXISTS (
        SELECT 1 FROM cloud_agents.deployment_targets AS target
        WHERE target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
            AND target.target_uid = ANY(profile.target_refs) AND target.observed_phase = 'ready'
            AND target.scheduling_state = 'active'
    )`
	listPublishedEnvironmentProfilesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(profile_row)
    ORDER BY profile_row.profile_version_uid), '[]'::jsonb)
FROM (
    SELECT profile.tenant_id, profile.project_uid, profile.profile_version_uid,
        profile.profile_uid, profile.profile_name, profile.profile_version, profile.description,
        profile.provider_kinds, profile.cpu_limit_millis, profile.memory_limit_bytes
    FROM cloud_agents.environment_profiles AS profile
    WHERE profile.tenant_id = cloud_agents.require_tenant_id() AND profile.project_uid = $1
        AND profile.profile_version_uid > $2 AND profile.status = 'published'
        AND EXISTS (
            SELECT 1 FROM cloud_agents.deployment_targets AS target
            WHERE target.tenant_id = profile.tenant_id AND target.project_uid = profile.project_uid
                AND target.target_uid = ANY(profile.target_refs) AND target.observed_phase = 'ready'
                AND target.scheduling_state = 'active'
        )
    ORDER BY profile.profile_version_uid
    LIMIT $3
) AS profile_row`
	environmentProfileAuditCursorIdentitySQL = `SELECT 1
FROM cloud_agents.environment_profile_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
    AND profile_version_uid = $2 AND event_uid = $3 AND occurred_at = $4`
	listEnvironmentProfileAuditSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, profile_version_uid, event_uid, operation_uid,
        subject_digest, action, profile_version, result, request_id, occurred_at
    FROM cloud_agents.environment_profile_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1
        AND profile_version_uid = $2
        AND ($3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4))
    ORDER BY occurred_at DESC, event_uid DESC
    LIMIT $5
) AS audit_row`
)

func (service *DurableCoordinationService) CreateEnvironmentProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalenvironmentprofile.CreateInput,
) (internalenvironmentprofile.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalenvironmentprofile.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalenvironmentprofile.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalenvironmentprofile.CreateMutationDigest(input)
	if err != nil {
		return internalenvironmentprofile.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalenvironmentprofile.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		actor, ok := operation.Actor()
		if !ok {
			return authz.ErrOperationDenied
		}
		subjectDigest, digestErr := actor.Digest()
		if digestErr != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanEnvironmentProfile(handle.transaction.queryRow(ctx, createEnvironmentProfileSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID, input.ProfileName,
					input.Version, input.Description, strings.Join(input.ProviderKinds, ","), input.CPULimitMillis,
					input.MemoryLimitBytes, input.StoragePolicyRef, input.NetworkPolicyRef,
					input.ReleaseDigest, strings.Join(input.TargetRefs, ","), input.ProviderCredentialRef,
					input.Mutation.IdempotencyKey, digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func (service *DurableCoordinationService) TransitionEnvironmentProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalenvironmentprofile.TransitionInput,
) (internalenvironmentprofile.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalenvironmentprofile.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalenvironmentprofile.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalenvironmentprofile.TransitionMutationDigest(input)
	if err != nil {
		return internalenvironmentprofile.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalenvironmentprofile.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		actor, ok := operation.Actor()
		if !ok {
			return authz.ErrOperationDenied
		}
		subjectDigest, digestErr := actor.Digest()
		if digestErr != nil {
			return authz.ErrOperationDenied
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				return scanEnvironmentProfile(handle.transaction.queryRow(ctx, transitionEnvironmentProfileSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.ProfileID, input.Version,
					input.ExpectedResourceVersion, input.Action, input.Mutation.IdempotencyKey,
					digest, input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func (service *DurableCoordinationService) GetEnvironmentProfile(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, profileID string, version int64,
) (internalenvironmentprofile.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalenvironmentprofile.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(profileID) || version < 1 || version > 2147483647 {
		return internalenvironmentprofile.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internalenvironmentprofile.Snapshot
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				err := scanEnvironmentProfile(handle.transaction.queryRow(readContext, getEnvironmentProfileSQL,
					projectID, profileID, version), internalenvironmentprofile.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internalenvironmentprofile.ErrNotFound
				}
				return err
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func (service *DurableCoordinationService) ListEnvironmentProfiles(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterProfileVersionID string, limit int,
) (EnvironmentProfilePage, error) {
	if service == nil || service.runner == nil {
		return EnvironmentProfilePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterProfileVersionID != "" && !validMutationIdentifier(afterProfileVersionID) || limit < 1 || limit > 200 {
		return EnvironmentProfilePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result EnvironmentProfilePage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterProfileVersionID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, environmentProfilePageCursorIdentitySQL,
						projectID, afterProfileVersionID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("environment profile page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listEnvironmentProfilesSQL,
					projectID, afterProfileVersionID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("environment profiles", err)
				}
				var decodeErr error
				result, decodeErr = decodeEnvironmentProfilePageRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func (service *DurableCoordinationService) ListPublishedEnvironmentProfiles(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterProfileVersionID string, limit int,
) (PublishedEnvironmentProfilePage, error) {
	if service == nil || service.runner == nil {
		return PublishedEnvironmentProfilePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterProfileVersionID != "" && !validMutationIdentifier(afterProfileVersionID) || limit < 1 || limit > 200 {
		return PublishedEnvironmentProfilePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result PublishedEnvironmentProfilePage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterProfileVersionID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, publishedEnvironmentProfilePageCursorIdentitySQL,
						projectID, afterProfileVersionID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("published environment profile page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listPublishedEnvironmentProfilesSQL,
					projectID, afterProfileVersionID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("published environment profiles", err)
				}
				var decodeErr error
				result, decodeErr = decodePublishedEnvironmentProfilePageRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func (service *DurableCoordinationService) ListEnvironmentProfileAuditEvents(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, profileVersionUID string,
	afterOccurredAt *time.Time, afterEventID string, limit int,
) (EnvironmentProfileAuditPage, error) {
	if service == nil || service.runner == nil {
		return EnvironmentProfileAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		!validMutationIdentifier(profileVersionUID) || (afterOccurredAt == nil) != (afterEventID == "") ||
		afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return EnvironmentProfileAuditPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result EnvironmentProfileAuditPage
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		operation, bindErr := binder.Bind(tenantID, scope, "projects.get")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.WithTenantRead(ctx, tenantID, func(readContext context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok {
				return ErrTenantCapabilityClosed
			}
			return executeVerifiedRBACOperation(readContext, handle, operation, scope, func() error {
				if afterOccurredAt != nil {
					var exists int
					if err := handle.transaction.queryRow(readContext, environmentProfileAuditCursorIdentitySQL,
						projectID, profileVersionUID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listEnvironmentProfileAuditSQL,
					projectID, profileVersionUID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeEnvironmentProfileAuditRows(raw, tenantID, projectID, profileVersionUID, limit)
				return decodeErr
			})
		})
	})
	return result, mapEnvironmentProfileError(err)
}

func decodeEnvironmentProfilePageRows(raw []byte, tenantID, projectID string, limit int) (EnvironmentProfilePage, error) {
	var rows []environmentProfilePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return EnvironmentProfilePage{}, ErrCoordinationResultDrift
	}
	profiles := make([]internalenvironmentprofile.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := environmentProfileSnapshot(row, tenantID, projectID)
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return EnvironmentProfilePage{}, ErrCoordinationResultDrift
		}
		profiles = append(profiles, snapshot)
	}
	result := EnvironmentProfilePage{EnvironmentProfiles: profiles}
	if len(profiles) > limit {
		result.EnvironmentProfiles = profiles[:limit]
		result.NextProfileVersionID = result.EnvironmentProfiles[len(result.EnvironmentProfiles)-1].ProfileVersionUID
	}
	return result, nil
}

func decodePublishedEnvironmentProfilePageRows(raw []byte, tenantID, projectID string, limit int) (PublishedEnvironmentProfilePage, error) {
	var rows []publishedEnvironmentProfilePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return PublishedEnvironmentProfilePage{}, ErrCoordinationResultDrift
	}
	profiles := make([]internalenvironmentprofile.Summary, 0, len(rows))
	for _, row := range rows {
		summary := internalenvironmentprofile.Summary{
			Scope:             internalenvironmentprofile.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			ProfileVersionUID: row.ProfileVersionUID, ProfileID: row.ProfileID, ProfileName: row.ProfileName,
			Version: row.Version, Description: row.Description, ProviderKinds: row.ProviderKinds,
			CPULimitMillis: row.CPULimitMillis, MemoryLimitBytes: row.MemoryLimitBytes,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || summary.Validate() != nil {
			return PublishedEnvironmentProfilePage{}, ErrCoordinationResultDrift
		}
		profiles = append(profiles, summary)
	}
	result := PublishedEnvironmentProfilePage{EnvironmentProfiles: profiles}
	if len(profiles) > limit {
		result.EnvironmentProfiles = profiles[:limit]
		result.NextProfileVersionID = result.EnvironmentProfiles[len(result.EnvironmentProfiles)-1].ProfileVersionUID
	}
	return result, nil
}

func decodeEnvironmentProfileAuditRows(raw []byte, tenantID, projectID, profileVersionUID string, limit int) (EnvironmentProfileAuditPage, error) {
	var rows []environmentProfileAuditPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return EnvironmentProfileAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internalenvironmentprofile.AuditEvent, 0, len(rows))
	for _, row := range rows {
		event := internalenvironmentprofile.AuditEvent{
			Scope:   internalenvironmentprofile.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			EventID: row.EventID, OperationID: row.OperationID, Actor: row.Actor, Action: row.Action,
			ProfileUID: row.ProfileVersionUID, ProfileVersion: row.ProfileVersion,
			Result: row.Result, RequestID: row.RequestID, OccurredAt: row.OccurredAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.ProfileVersionUID != profileVersionUID || event.Validate() != nil {
			return EnvironmentProfileAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := EnvironmentProfileAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func scanEnvironmentProfile(row rowScanner, scope internalenvironmentprofile.Scope, result *internalenvironmentprofile.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.ProfileVersionUID, &result.ProfileID, &result.ProfileName,
		&result.Version, &result.Description, &result.Status, &result.ProviderKinds,
		&result.CPULimitMillis, &result.MemoryLimitBytes, &result.StoragePolicyRef,
		&result.NetworkPolicyRef, &result.ReleaseDigest, &result.TargetRefs,
		&result.ProviderCredentialRef, &result.ResourceVersion, &result.CreatedAt,
		&result.UpdatedAt, &result.PublishedAt, &result.DisabledAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: environment profile projection", ErrCoordinationResultDrift)
	}
	return nil
}

func environmentProfileSnapshot(row environmentProfilePageRow, tenantID, projectID string) internalenvironmentprofile.Snapshot {
	return internalenvironmentprofile.Snapshot{
		Scope:             internalenvironmentprofile.Scope{TenantID: tenantID, ProjectID: projectID},
		ProfileVersionUID: row.ProfileVersionUID, ProfileID: row.ProfileID, ProfileName: row.ProfileName,
		Version: row.Version, Description: row.Description, Status: row.Status, ProviderKinds: row.ProviderKinds,
		CPULimitMillis: row.CPULimitMillis, MemoryLimitBytes: row.MemoryLimitBytes,
		StoragePolicyRef: row.StoragePolicyRef, NetworkPolicyRef: row.NetworkPolicyRef,
		ReleaseDigest: row.ReleaseDigest, TargetRefs: row.TargetRefs,
		ProviderCredentialRef: row.ProviderCredentialRef, ResourceVersion: row.ResourceVersion,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, PublishedAt: row.PublishedAt, DisabledAt: row.DisabledAt,
	}
}

func mapEnvironmentProfileError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		if postgresError.Code == "23503" && postgresError.Message == "environment profile was not found" {
			return ErrEnvironmentProfileNotFound
		}
		if postgresError.Code == "23505" {
			switch postgresError.Message {
			case "environment profile idempotency conflict":
				return ErrEnvironmentProfileIdempotencyConflict
			case "environment profile version conflict", "environment profile name conflict":
				return ErrEnvironmentProfileVersionConflict
			case "environment profile transition conflict":
				return ErrEnvironmentProfileTransitionConflict
			}
		}
	}
	switch {
	case errors.Is(err, internalenvironmentprofile.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return ErrEnvironmentProfileNotFound
	case errors.Is(err, internalenvironmentprofile.ErrConflict), errors.Is(err, internalenvironmentprofile.ErrIdempotencyConflict):
		return ErrCoordinationRejected
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("environment profile", err)
	}
}

var ErrEnvironmentProfileNotFound = errors.New("environment profile was not found")
var ErrEnvironmentProfileIdempotencyConflict = errors.New("environment profile idempotency key conflicts")
var ErrEnvironmentProfileVersionConflict = errors.New("environment profile version conflicts")
var ErrEnvironmentProfileTransitionConflict = errors.New("environment profile transition conflicts")
