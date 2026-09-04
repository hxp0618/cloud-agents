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
	internalworkerrelease "github.com/hxp0618/cloud-agents/services/control-plane/internal/workerrelease"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type WorkerReleasePage struct {
	WorkerReleases []internalworkerrelease.Snapshot
	NextReleaseID  string
}

type workerReleasePageRow struct {
	TenantID                   string    `json:"tenant_id"`
	ProjectID                  string    `json:"project_uid"`
	ReleaseID                  string    `json:"release_uid"`
	ReleaseName                string    `json:"release_name"`
	ImageRepository            string    `json:"image_repository"`
	ReleaseDigest              string    `json:"release_digest"`
	PlatformVersion            string    `json:"platform_version"`
	RuntimeVersion             string    `json:"runtime_version"`
	CodexVersion               string    `json:"codex_version"`
	ClaudeCodeVersion          string    `json:"claude_code_version"`
	Architectures              []string  `json:"architectures"`
	Status                     string    `json:"status"`
	VerificationState          string    `json:"verification_state"`
	VerificationEvidenceDigest string    `json:"verification_evidence_digest"`
	ResourceVersion            int64     `json:"resource_version"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
	ApprovedAt                 time.Time `json:"approved_at"`
}

const workerReleaseColumns = `release_uid, release_name, image_repository, release_digest,
    platform_version, runtime_version, codex_version, claude_code_version, architectures,
    status, verification_state, verification_evidence_digest, resource_version,
    created_at, updated_at, approved_at`

var (
	registerWorkerReleaseSQL = `SELECT ` + workerReleaseColumns + `
FROM cloud_agents.register_worker_release_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`
	workerReleasePageCursorIdentitySQL = `SELECT 1 FROM cloud_agents.worker_releases
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND release_uid = $2`
	listWorkerReleasesSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(release_row)
    ORDER BY release_row.release_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, ` + workerReleaseColumns + `
    FROM cloud_agents.worker_releases
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND release_uid > $2
    ORDER BY release_uid
    LIMIT $3
) AS release_row`
)

func (service *DurableCoordinationService) RegisterWorkerRelease(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internalworkerrelease.RegisterInput,
) (internalworkerrelease.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internalworkerrelease.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internalworkerrelease.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internalworkerrelease.RegisterMutationDigest(input)
	if err != nil {
		return internalworkerrelease.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internalworkerrelease.Snapshot
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
				return scanWorkerRelease(handle.transaction.queryRow(ctx, registerWorkerReleaseSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.ReleaseID, input.ReleaseName,
					input.ImageRepository, input.ReleaseDigest, input.PlatformVersion, input.RuntimeVersion,
					input.CodexVersion, input.ClaudeCodeVersion, strings.Join(input.Architectures, ","),
					input.VerificationEvidenceDigest, input.Mutation.IdempotencyKey, digest,
					input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapWorkerReleaseError(err)
}

func (service *DurableCoordinationService) ListWorkerReleases(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterReleaseID string, limit int,
) (WorkerReleasePage, error) {
	if service == nil || service.runner == nil {
		return WorkerReleasePage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterReleaseID != "" && !validMutationIdentifier(afterReleaseID) || limit < 1 || limit > 200 {
		return WorkerReleasePage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result WorkerReleasePage
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
				if afterReleaseID != "" {
					var exists int
					if cursorErr := handle.transaction.queryRow(readContext, workerReleasePageCursorIdentitySQL, projectID, afterReleaseID).Scan(&exists); cursorErr != nil {
						if errors.Is(cursorErr, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return cursorErr
					}
				}
				var raw []byte
				if queryErr := handle.transaction.queryRow(readContext, listWorkerReleasesSQL, projectID, afterReleaseID, limit+1).Scan(&raw); queryErr != nil {
					return queryErr
				}
				var decodeErr error
				result, decodeErr = decodeWorkerReleasePageRows(raw, tenantID, projectID, limit)
				return decodeErr
			})
		})
	})
	return result, mapWorkerReleaseError(err)
}

func scanWorkerRelease(row rowScanner, scope internalworkerrelease.Scope, result *internalworkerrelease.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.ReleaseID, &result.ReleaseName, &result.ImageRepository,
		&result.ReleaseDigest, &result.PlatformVersion, &result.RuntimeVersion,
		&result.CodexVersion, &result.ClaudeCodeVersion, &result.Architectures,
		&result.Status, &result.VerificationState, &result.VerificationEvidenceDigest,
		&result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt, &result.ApprovedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: worker release projection", ErrCoordinationResultDrift)
	}
	return nil
}

func decodeWorkerReleasePageRows(raw []byte, tenantID, projectID string, limit int) (WorkerReleasePage, error) {
	var rows []workerReleasePageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return WorkerReleasePage{}, ErrCoordinationResultDrift
	}
	releases := make([]internalworkerrelease.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := internalworkerrelease.Snapshot{
			Scope:     internalworkerrelease.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			ReleaseID: row.ReleaseID, ReleaseName: row.ReleaseName, ImageRepository: row.ImageRepository,
			ReleaseDigest: row.ReleaseDigest, PlatformVersion: row.PlatformVersion,
			RuntimeVersion: row.RuntimeVersion, CodexVersion: row.CodexVersion,
			ClaudeCodeVersion: row.ClaudeCodeVersion, Architectures: row.Architectures,
			Status: row.Status, VerificationState: row.VerificationState,
			VerificationEvidenceDigest: row.VerificationEvidenceDigest,
			ResourceVersion:            row.ResourceVersion, CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt, ApprovedAt: row.ApprovedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return WorkerReleasePage{}, ErrCoordinationResultDrift
		}
		releases = append(releases, snapshot)
	}
	result := WorkerReleasePage{WorkerReleases: releases}
	if len(releases) > limit {
		result.WorkerReleases = releases[:limit]
		result.NextReleaseID = result.WorkerReleases[len(result.WorkerReleases)-1].ReleaseID
	}
	return result, nil
}

func mapWorkerReleaseError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		if postgresError.Message == "worker release idempotency conflict" {
			return ErrWorkerReleaseIdempotencyConflict
		}
		return ErrWorkerReleaseConflict
	}
	if err == nil {
		return nil
	}
	return mapCoordinationDatabaseError("worker release", err)
}

var ErrWorkerReleaseIdempotencyConflict = errors.New("worker release idempotency key conflicts")
var ErrWorkerReleaseConflict = errors.New("worker release conflicts")
