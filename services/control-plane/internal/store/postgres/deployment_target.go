package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type DeploymentTargetPage struct {
	DeploymentTargets []internaldeploymenttarget.Snapshot
	NextTargetID      string
}

type DeploymentTargetOperationPage struct {
	Operations      []internaldeploymenttarget.Operation
	NextRequestedAt *time.Time
	NextOperationID string
}

type DeploymentTargetAuditPage struct {
	Events         []internaldeploymenttarget.AuditEvent
	NextOccurredAt *time.Time
	NextEventID    string
}

type deploymentTargetPageRow struct {
	TenantID        string     `json:"tenant_id"`
	ProjectID       string     `json:"project_uid"`
	TargetID        string     `json:"target_uid"`
	TargetName      string     `json:"target_name"`
	Kind            string     `json:"target_kind"`
	Endpoint        string     `json:"endpoint"`
	CredentialRef   string     `json:"credential_ref"`
	Generation      int64      `json:"generation"`
	ObservedPhase   string     `json:"observed_phase"`
	APIVersion      string     `json:"api_version"`
	EngineVersion   string     `json:"engine_version"`
	OS              string     `json:"target_os"`
	Arch            string     `json:"target_arch"`
	StableErrorCode string     `json:"stable_error_code"`
	LastProbeAt     *time.Time `json:"last_probe_at"`
	ResourceVersion int64      `json:"resource_version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type deploymentTargetOperationPageRow struct {
	TenantID         string    `json:"tenant_id"`
	ProjectID        string    `json:"project_uid"`
	TargetID         string    `json:"target_uid"`
	OperationID      string    `json:"operation_uid"`
	IdempotencyKey   string    `json:"idempotency_key"`
	Action           string    `json:"action"`
	RequestID        string    `json:"request_id"`
	RequestedBy      string    `json:"subject_digest"`
	TargetGeneration int64     `json:"target_generation"`
	State            string    `json:"state"`
	CurrentStep      string    `json:"current_step"`
	StableErrorCode  string    `json:"stable_error_code"`
	ImpactSummary    string    `json:"impact_summary"`
	Retryable        bool      `json:"retryable"`
	RequestedAt      time.Time `json:"requested_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type deploymentTargetAuditPageRow struct {
	TenantID         string    `json:"tenant_id"`
	ProjectID        string    `json:"project_uid"`
	TargetID         string    `json:"target_uid"`
	EventID          string    `json:"event_uid"`
	OperationID      string    `json:"operation_uid"`
	Actor            string    `json:"subject_digest"`
	Action           string    `json:"action"`
	TargetGeneration int64     `json:"target_generation"`
	State            string    `json:"state"`
	StableErrorCode  string    `json:"stable_error_code"`
	RequestID        string    `json:"request_id"`
	OccurredAt       time.Time `json:"occurred_at"`
}

const deploymentTargetColumns = `target_uid, target_name, target_kind, endpoint, credential_ref, generation,
    observed_phase, api_version, engine_version, target_os, target_arch, stable_error_code,
    last_probe_at, resource_version, created_at, updated_at`

const deploymentTargetOperationColumns = `operation_uid, idempotency_key, action, target_uid,
    target_generation, subject_digest, request_id, requested_at, updated_at, state, current_step,
    stable_error_code, impact_summary, retryable`

var (
	registerDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.register_deployment_target_v3($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	getDeploymentTargetSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.deployment_targets
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2`
	deploymentTargetPageCursorIdentitySQL = `SELECT 1
FROM cloud_agents.deployment_targets
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2`
	listDeploymentTargetsSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(deployment_target)
    ORDER BY deployment_target.target_uid), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, ` + deploymentTargetColumns + `
    FROM cloud_agents.deployment_targets
    WHERE tenant_id = cloud_agents.require_tenant_id()
        AND project_uid = $1
        AND target_uid > $2
    ORDER BY target_uid
    LIMIT $3
) AS deployment_target`
	beginDeploymentTargetProbeSQL = `SELECT ` + deploymentTargetColumns + `, execute_probe
FROM cloud_agents.begin_deployment_target_probe_v2($1, $2, $3, $4, $5, $6, $7, $8)`
	completeDeploymentTargetProbeSQL = `SELECT ` + deploymentTargetColumns + `
FROM cloud_agents.complete_deployment_target_probe_v2($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	beginDeploymentTargetCleanupSQL = `SELECT ` + deploymentTargetOperationColumns + `, execute_cleanup
FROM cloud_agents.begin_deployment_target_cleanup_v1($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	completeDeploymentTargetCleanupSQL = `SELECT ` + deploymentTargetOperationColumns + `
FROM cloud_agents.complete_deployment_target_cleanup_v1($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	deploymentTargetOperationCursorIdentitySQL = `SELECT 1
FROM cloud_agents.deployment_target_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2
    AND operation_uid = $3 AND requested_at = $4`
	listDeploymentTargetOperationsSQL = `WITH latest AS (
    SELECT DISTINCT ON (operation_uid)
        tenant_id, project_uid, target_uid, operation_uid, idempotency_key, action, request_id,
        subject_digest, target_generation, state, current_step, stable_error_code, impact_summary,
        retryable, requested_at, occurred_at AS updated_at
    FROM cloud_agents.deployment_target_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2
    ORDER BY operation_uid, occurred_at DESC, event_uid DESC
), operation_page AS (
    SELECT * FROM latest
    WHERE $3::timestamptz IS NULL OR (requested_at, operation_uid) < ($3, $4)
    ORDER BY requested_at DESC, operation_uid DESC
    LIMIT $5
)
SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(operation_row)
    ORDER BY operation_row.requested_at DESC, operation_row.operation_uid DESC), '[]'::jsonb)
FROM operation_page AS operation_row`
	deploymentTargetAuditCursorIdentitySQL = `SELECT 1
FROM cloud_agents.deployment_target_activity
WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2
    AND event_uid = $3 AND occurred_at = $4`
	listDeploymentTargetAuditSQL = `SELECT COALESCE(pg_catalog.jsonb_agg(pg_catalog.to_jsonb(audit_row)
    ORDER BY audit_row.occurred_at DESC, audit_row.event_uid DESC), '[]'::jsonb)
FROM (
    SELECT tenant_id, project_uid, target_uid, event_uid, operation_uid, subject_digest, action,
        target_generation, state, stable_error_code, request_id, occurred_at
    FROM cloud_agents.deployment_target_activity
    WHERE tenant_id = cloud_agents.require_tenant_id() AND project_uid = $1 AND target_uid = $2
        AND ($3::timestamptz IS NULL OR (occurred_at, event_uid) < ($3, $4))
    ORDER BY occurred_at DESC, event_uid DESC
    LIMIT $5
) AS audit_row`
)

func (service *DurableCoordinationService) RegisterDeploymentTarget(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internaldeploymenttarget.RegisterInput,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.RegisterMutationDigest(input)
	if err != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.Snapshot
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
				return scanDeploymentTarget(handle.transaction.queryRow(ctx, registerDeploymentTargetSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.TargetName, input.Kind,
					input.Endpoint, input.CredentialRef, input.Mutation.IdempotencyKey, digest,
					input.Mutation.RequestID, subjectDigest), input.Scope, &result)
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) GetDeploymentTarget(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, targetID string,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(targetID) {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result internaldeploymenttarget.Snapshot
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
				err := scanDeploymentTarget(handle.transaction.queryRow(readContext, getDeploymentTargetSQL, projectID, targetID),
					internaldeploymenttarget.Scope{TenantID: tenantID, ProjectID: projectID}, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				}
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) ListDeploymentTargets(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, afterTargetID string, limit int,
) (DeploymentTargetPage, error) {
	if service == nil || service.runner == nil {
		return DeploymentTargetPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) ||
		afterTargetID != "" && !validMutationIdentifier(afterTargetID) || limit < 1 || limit > 200 {
		return DeploymentTargetPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result DeploymentTargetPage
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
				if afterTargetID != "" {
					var exists int
					if err := handle.transaction.queryRow(readContext, deploymentTargetPageCursorIdentitySQL, projectID, afterTargetID).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return mapCoordinationDatabaseError("deployment target page cursor", err)
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listDeploymentTargetsSQL, projectID, afterTargetID, limit+1).Scan(&raw); err != nil {
					return mapCoordinationDatabaseError("deployment targets", err)
				}
				var err error
				result, err = decodeDeploymentTargetPageRows(raw, tenantID, projectID, limit)
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func decodeDeploymentTargetPageRows(raw []byte, tenantID, projectID string, limit int) (DeploymentTargetPage, error) {
	var rows []deploymentTargetPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return DeploymentTargetPage{}, ErrCoordinationResultDrift
	}
	targets := make([]internaldeploymenttarget.Snapshot, 0, len(rows))
	for _, row := range rows {
		snapshot := internaldeploymenttarget.Snapshot{
			Scope:    internaldeploymenttarget.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			TargetID: row.TargetID, TargetName: row.TargetName, Kind: row.Kind, Endpoint: row.Endpoint,
			CredentialRef: row.CredentialRef, Generation: row.Generation, ObservedPhase: row.ObservedPhase,
			APIVersion: row.APIVersion, EngineVersion: row.EngineVersion, OS: row.OS, Arch: row.Arch,
			StableErrorCode: row.StableErrorCode, LastProbeAt: row.LastProbeAt, ResourceVersion: row.ResourceVersion,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || snapshot.Validate() != nil {
			return DeploymentTargetPage{}, ErrCoordinationResultDrift
		}
		targets = append(targets, snapshot)
	}
	result := DeploymentTargetPage{DeploymentTargets: targets}
	if len(targets) > limit {
		result.DeploymentTargets = targets[:limit]
		result.NextTargetID = result.DeploymentTargets[len(result.DeploymentTargets)-1].TargetID
	}
	return result, nil
}

func (service *DurableCoordinationService) ListDeploymentTargetOperations(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, targetID string,
	afterRequestedAt *time.Time, afterOperationID string, limit int,
) (DeploymentTargetOperationPage, error) {
	if service == nil || service.runner == nil {
		return DeploymentTargetOperationPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(targetID) ||
		(afterRequestedAt == nil) != (afterOperationID == "") || afterOperationID != "" && !validMutationIdentifier(afterOperationID) || limit < 1 || limit > 200 {
		return DeploymentTargetOperationPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result DeploymentTargetOperationPage
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
				if afterRequestedAt != nil {
					var exists int
					if err := handle.transaction.queryRow(readContext, deploymentTargetOperationCursorIdentitySQL,
						projectID, targetID, afterOperationID, *afterRequestedAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listDeploymentTargetOperationsSQL,
					projectID, targetID, afterRequestedAt, afterOperationID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeDeploymentTargetOperationRows(raw, tenantID, projectID, targetID, limit)
				return decodeErr
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func decodeDeploymentTargetOperationRows(raw []byte, tenantID, projectID, targetID string, limit int) (DeploymentTargetOperationPage, error) {
	var rows []deploymentTargetOperationPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return DeploymentTargetOperationPage{}, ErrCoordinationResultDrift
	}
	operations := make([]internaldeploymenttarget.Operation, 0, len(rows))
	for _, row := range rows {
		operation := internaldeploymenttarget.Operation{
			Scope:       internaldeploymenttarget.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			OperationID: row.OperationID, IdempotencyKey: row.IdempotencyKey, Action: row.Action,
			TargetID: row.TargetID, TargetGeneration: row.TargetGeneration, RequestedBy: row.RequestedBy,
			RequestID: row.RequestID, State: row.State, CurrentStep: row.CurrentStep,
			StableErrorCode: row.StableErrorCode, ImpactSummary: row.ImpactSummary, Retryable: row.Retryable,
			RequestedAt: row.RequestedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.TargetID != targetID || operation.Validate() != nil {
			return DeploymentTargetOperationPage{}, ErrCoordinationResultDrift
		}
		operations = append(operations, operation)
	}
	result := DeploymentTargetOperationPage{Operations: operations}
	if len(operations) > limit {
		result.Operations = operations[:limit]
		last := result.Operations[len(result.Operations)-1]
		requestedAt := last.RequestedAt
		result.NextRequestedAt, result.NextOperationID = &requestedAt, last.OperationID
	}
	return result, nil
}

func (service *DurableCoordinationService) ListDeploymentTargetAuditEvents(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, projectID, targetID string,
	afterOccurredAt *time.Time, afterEventID string, limit int,
) (DeploymentTargetAuditPage, error) {
	if service == nil || service.runner == nil {
		return DeploymentTargetAuditPage{}, ErrNilCoordinationRunner
	}
	if ctx == nil || !validMutationIdentifier(tenantID) || !validMutationIdentifier(projectID) || !validMutationIdentifier(targetID) ||
		(afterOccurredAt == nil) != (afterEventID == "") || afterEventID != "" && !validMutationIdentifier(afterEventID) || limit < 1 || limit > 200 {
		return DeploymentTargetAuditPage{}, ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeProject, ID: projectID}
	var result DeploymentTargetAuditPage
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
					if err := handle.transaction.queryRow(readContext, deploymentTargetAuditCursorIdentitySQL,
						projectID, targetID, afterEventID, *afterOccurredAt).Scan(&exists); err != nil {
						if errors.Is(err, pgx.ErrNoRows) {
							return ErrCoordinationInvalidInput
						}
						return err
					}
				}
				var raw []byte
				if err := handle.transaction.queryRow(readContext, listDeploymentTargetAuditSQL,
					projectID, targetID, afterOccurredAt, afterEventID, limit+1).Scan(&raw); err != nil {
					return err
				}
				var decodeErr error
				result, decodeErr = decodeDeploymentTargetAuditRows(raw, tenantID, projectID, targetID, limit)
				return decodeErr
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func decodeDeploymentTargetAuditRows(raw []byte, tenantID, projectID, targetID string, limit int) (DeploymentTargetAuditPage, error) {
	var rows []deploymentTargetAuditPageRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > limit+1 {
		return DeploymentTargetAuditPage{}, ErrCoordinationResultDrift
	}
	events := make([]internaldeploymenttarget.AuditEvent, 0, len(rows))
	for _, row := range rows {
		result := map[string]string{"running": "requested", "succeeded": "succeeded", "failed": "failed"}[row.State]
		event := internaldeploymenttarget.AuditEvent{
			Scope:   internaldeploymenttarget.Scope{TenantID: row.TenantID, ProjectID: row.ProjectID},
			EventID: row.EventID, Actor: row.Actor, Action: row.Action, TargetID: row.TargetID,
			TargetGeneration: row.TargetGeneration, Result: result, RequestID: row.RequestID,
			OperationID: row.OperationID, StableErrorCode: row.StableErrorCode, OccurredAt: row.OccurredAt,
		}
		if row.TenantID != tenantID || row.ProjectID != projectID || row.TargetID != targetID || event.Validate() != nil {
			return DeploymentTargetAuditPage{}, ErrCoordinationResultDrift
		}
		events = append(events, event)
	}
	result := DeploymentTargetAuditPage{Events: events}
	if len(events) > limit {
		result.Events = events[:limit]
		last := result.Events[len(result.Events)-1]
		occurredAt := last.OccurredAt
		result.NextOccurredAt, result.NextEventID = &occurredAt, last.EventID
	}
	return result, nil
}

func (service *DurableCoordinationService) BeginDeploymentTargetProbe(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internaldeploymenttarget.ProbeInput,
) (internaldeploymenttarget.ProbeStart, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.ProbeStart{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internaldeploymenttarget.ProbeStart{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.ProbeMutationDigest(input)
	if err != nil {
		return internaldeploymenttarget.ProbeStart{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.ProbeStart
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
				row := handle.transaction.queryRow(ctx, beginDeploymentTargetProbeSQL, input.Scope.TenantID, input.Scope.ProjectID,
					input.TargetID, input.ExpectedGeneration, input.Mutation.IdempotencyKey, digest,
					input.Mutation.RequestID, subjectDigest)
				if err := scanDeploymentTargetWithExecute(row, input.Scope, &result); errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				} else {
					return err
				}
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) CompleteDeploymentTargetProbe(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, completion internaldeploymenttarget.ProbeCompletion,
) (internaldeploymenttarget.Snapshot, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Snapshot{}, ErrNilCoordinationRunner
	}
	if ctx == nil || completion.Validate(tenantID) != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.ProbeMutationDigest(completion.Input)
	if err != nil {
		return internaldeploymenttarget.Snapshot{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.Snapshot
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: completion.Input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				err := scanDeploymentTarget(handle.transaction.queryRow(ctx, completeDeploymentTargetProbeSQL,
					completion.Input.Scope.TenantID, completion.Input.Scope.ProjectID, completion.Input.TargetID,
					completion.Input.ExpectedGeneration, completion.Input.Mutation.IdempotencyKey, digest, completion.Succeeded,
					completion.APIVersion, completion.EngineVersion, completion.OS, completion.Arch, completion.StableErrorCode),
					completion.Input.Scope, &result)
				if errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				}
				return err
			})
		})
	})
	return result, mapDeploymentTargetError(err)
}

func (service *DurableCoordinationService) BeginDeploymentTargetCleanup(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, input internaldeploymenttarget.CleanupInput,
) (internaldeploymenttarget.CleanupStart, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.CleanupStart{}, ErrNilCoordinationRunner
	}
	if ctx == nil || input.Validate(tenantID) != nil {
		return internaldeploymenttarget.CleanupStart{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.CleanupMutationDigest(input)
	if err != nil {
		return internaldeploymenttarget.CleanupStart{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.CleanupStart
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
				row := handle.transaction.queryRow(ctx, beginDeploymentTargetCleanupSQL,
					input.Scope.TenantID, input.Scope.ProjectID, input.TargetID, input.ExpectedGeneration,
					input.ExpectedResourceVersion, input.ImpactDigest, input.Mutation.IdempotencyKey, digest,
					input.Mutation.RequestID, subjectDigest)
				if err := scanDeploymentTargetCleanupStart(row, input.Scope, &result); errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				} else {
					return err
				}
			})
		})
	})
	return result, mapDeploymentTargetCleanupError(err)
}

func (service *DurableCoordinationService) CompleteDeploymentTargetCleanup(
	ctx context.Context, tenantID string, principal *authn.VerifiedPrincipal, completion internaldeploymenttarget.CleanupCompletion,
) (internaldeploymenttarget.Operation, error) {
	if service == nil || service.runner == nil {
		return internaldeploymenttarget.Operation{}, ErrNilCoordinationRunner
	}
	if ctx == nil || completion.Validate(tenantID) != nil {
		return internaldeploymenttarget.Operation{}, ErrCoordinationInvalidInput
	}
	digest, err := internaldeploymenttarget.CleanupMutationDigest(completion.Input)
	if err != nil {
		return internaldeploymenttarget.Operation{}, ErrCoordinationInvalidInput
	}
	var result internaldeploymenttarget.Operation
	err = authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope := authz.ScopeRef{Level: authz.ScopeProject, ID: completion.Input.Scope.ProjectID}
		operation, bindErr := binder.Bind(tenantID, scope, "projects.act")
		if bindErr != nil {
			return mapVerifiedCoordinationAuthorizationError(bindErr)
		}
		return service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				row := handle.transaction.queryRow(ctx, completeDeploymentTargetCleanupSQL,
					completion.Input.Scope.TenantID, completion.Input.Scope.ProjectID, completion.Input.TargetID,
					completion.Input.ExpectedGeneration, completion.Input.Mutation.IdempotencyKey, digest,
					completion.Succeeded, completion.StableErrorCode, completion.ImpactSummary)
				if err := scanDeploymentTargetOperation(row, completion.Input.Scope, &result); errors.Is(err, pgx.ErrNoRows) {
					return internaldeploymenttarget.ErrNotFound
				} else {
					return err
				}
			})
		})
	})
	return result, mapDeploymentTargetCleanupError(err)
}

func scanDeploymentTarget(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.Snapshot) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.TargetID, &result.TargetName, &result.Kind, &result.Endpoint, &result.CredentialRef,
		&result.Generation, &result.ObservedPhase, &result.APIVersion, &result.EngineVersion, &result.OS, &result.Arch,
		&result.StableErrorCode, &result.LastProbeAt, &result.ResourceVersion, &result.CreatedAt, &result.UpdatedAt); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return fmt.Errorf("%w: deployment target projection", ErrCoordinationResultDrift)
	}
	return nil
}

func scanDeploymentTargetWithExecute(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.ProbeStart) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.Target.TargetID, &result.Target.TargetName, &result.Target.Kind, &result.Target.Endpoint,
		&result.Target.CredentialRef, &result.Target.Generation, &result.Target.ObservedPhase, &result.Target.APIVersion,
		&result.Target.EngineVersion, &result.Target.OS, &result.Target.Arch, &result.Target.StableErrorCode,
		&result.Target.LastProbeAt, &result.Target.ResourceVersion, &result.Target.CreatedAt, &result.Target.UpdatedAt,
		&result.Execute); err != nil {
		return err
	}
	result.Target.Scope = scope
	if result.Target.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func scanDeploymentTargetOperation(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.Operation) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.OperationID, &result.IdempotencyKey, &result.Action, &result.TargetID,
		&result.TargetGeneration, &result.RequestedBy, &result.RequestID, &result.RequestedAt, &result.UpdatedAt,
		&result.State, &result.CurrentStep, &result.StableErrorCode, &result.ImpactSummary, &result.Retryable); err != nil {
		return err
	}
	result.Scope = scope
	if result.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func scanDeploymentTargetCleanupStart(row rowScanner, scope internaldeploymenttarget.Scope, result *internaldeploymenttarget.CleanupStart) error {
	if row == nil || result == nil {
		return ErrCoordinationResultDrift
	}
	if err := row.Scan(&result.Operation.OperationID, &result.Operation.IdempotencyKey, &result.Operation.Action,
		&result.Operation.TargetID, &result.Operation.TargetGeneration, &result.Operation.RequestedBy,
		&result.Operation.RequestID, &result.Operation.RequestedAt, &result.Operation.UpdatedAt,
		&result.Operation.State, &result.Operation.CurrentStep, &result.Operation.StableErrorCode,
		&result.Operation.ImpactSummary, &result.Operation.Retryable, &result.Execute); err != nil {
		return err
	}
	result.Operation.Scope = scope
	if result.Operation.Validate() != nil {
		return ErrCoordinationResultDrift
	}
	return nil
}

func mapDeploymentTargetCleanupError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		switch postgresError.Message {
		case "deployment target cleanup idempotency conflict":
			return ErrDeploymentTargetCleanupIdempotencyConflict
		case "deployment target cleanup is already running":
			return ErrDeploymentTargetCleanupBusy
		case "deployment target generation conflict":
			return ErrDeploymentTargetCleanupGenerationConflict
		case "deployment target resource version conflict":
			return ErrDeploymentTargetCleanupResourceVersionConflict
		case "deployment target is not ready":
			return ErrDeploymentTargetCleanupNotReady
		}
	}
	return mapDeploymentTargetError(err)
}

func mapDeploymentTargetError(err error) error {
	switch {
	case errors.Is(err, internaldeploymenttarget.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		return ErrDeploymentTargetNotFound
	case errors.Is(err, internaldeploymenttarget.ErrConflict), errors.Is(err, internaldeploymenttarget.ErrIdempotencyConflict):
		return ErrCoordinationRejected
	case err == nil:
		return nil
	default:
		return mapCoordinationDatabaseError("deployment target", err)
	}
}

var ErrDeploymentTargetNotFound = errors.New("deployment target was not found")
var ErrDeploymentTargetCleanupIdempotencyConflict = errors.New("deployment target cleanup idempotency key conflicts")
var ErrDeploymentTargetCleanupBusy = errors.New("deployment target cleanup is already running")
var ErrDeploymentTargetCleanupGenerationConflict = errors.New("deployment target cleanup generation conflicts")
var ErrDeploymentTargetCleanupResourceVersionConflict = errors.New("deployment target cleanup resource version conflicts")
var ErrDeploymentTargetCleanupNotReady = errors.New("deployment target cleanup target is not ready")
