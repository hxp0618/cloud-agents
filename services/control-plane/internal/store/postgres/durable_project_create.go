package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
)

const createManagedAgentProjectDurableSQL = `SELECT
    disposition, replay_state, operation_id, operation_generation,
    resource_kind, resource_id, resource_version, stable_error_code,
    outbox_event_id, outbox_state
FROM cloud_agents.create_managed_agent_project_durable_v1($1, $2, $3, $4, $5, $6, $7, $8, $9)`

// DurableProjectCreateInput is the typed localdev writer request. The server
// supplies only the generated-profile request and idempotency metadata; the
// project UID is derived inside this package from the bound request digest.
type DurableProjectCreateInput struct {
	Profile        coordination.Profile
	Request        coordination.ManagedAgentCreateProjectRequest
	IdempotencyKey string
	AuditFactID    string
}

type DurableProjectCreateResult struct {
	DatabaseOutcome     DatabaseOutcome
	Disposition         string
	ReplayState         string
	OperationID         *string
	OperationGeneration *int64
	ResourceKind        *string
	ResourceID          *string
	ResourceVersion     *int64
	StableErrorCode     *string
	OutboxEventID       *string
	OutboxState         *string
	Project             Project
}

// CreateProjectDurable authorizes projects.create and executes one serializable
// SECURITY DEFINER transaction. It never calls a provider or Managed Agent.
func (service *DurableCoordinationService) CreateProjectDurable(
	ctx context.Context,
	tenantID string,
	principal *authn.VerifiedPrincipal,
	input DurableProjectCreateInput,
) (DurableProjectCreateResult, error) {
	if service == nil || service.runner == nil {
		return DurableProjectCreateResult{}, ErrNilCoordinationRunner
	}
	var result DurableProjectCreateResult
	err := authz.WithVerifiedOperation(principal, func(binder *authz.VerifiedOperationBinder) error {
		scope, requestDigest, err := bindDurableProjectCreateProfile(
			ctx, tenantID, input.Profile, input.Request, input.AuditFactID,
		)
		if err != nil || !validIdempotencyKey(input.IdempotencyKey) {
			if err != nil {
				return err
			}
			return ErrCoordinationInvalidInput
		}
		operation, err := binder.Bind(tenantID, scope, input.Profile.RequiredPermission())
		if err != nil {
			return mapVerifiedCoordinationAuthorizationError(err)
		}
		actor, ok := operation.Actor()
		if !ok {
			return ErrMutationDenied
		}
		subjectDigest, err := actor.Digest()
		if err != nil || !validCoordinationDigest(subjectDigest) {
			return ErrCoordinationInvalidInput
		}

		projectUID := durableProjectUID(requestDigest)
		var candidate DurableProjectCreateResult
		transactionErr := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
			return executeVerifiedRBACOperation(ctx, handle, operation, scope, func() error {
				candidate, err = createDurableProjectTransaction(
					ctx, handle, tenantID, subjectDigest, requestDigest, projectUID, input,
				)
				return err
			})
		})
		transactionErr = mapVerifiedCoordinationAuthorizationError(transactionErr)
		candidate, err = settleDurableProjectCreate(candidate, transactionErr)
		if err != nil {
			return err
		}
		result = candidate
		return nil
	})
	if err != nil {
		return DurableProjectCreateResult{}, err
	}
	return result, nil
}

func bindDurableProjectCreateProfile(
	ctx context.Context,
	tenantID string,
	profile coordination.Profile,
	request coordination.ManagedAgentCreateProjectRequest,
	auditFactID string,
) (authz.ScopeRef, string, error) {
	if ctx == nil {
		return authz.ScopeRef{}, "", ErrNilContext
	}
	if !validMutationIdentifier(tenantID) || !validMutationIdentifier(auditFactID) ||
		!profile.Valid() || profile.ProfileID() != "managedAgentCreateProjectDurable/v1alpha1" ||
		profile.ProfileDigest() != coordination.ManagedAgentCreateProjectDurable().ProfileDigest() ||
		!profile.CreatesPlatformOperation() || profile.ExternalSideEffectAllowed() ||
		profile.OutboxEventClass() != "operation_effect" {
		return authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	intent, err := coordination.BindManagedAgentCreateProjectDurable(profile, tenantID, request)
	if err != nil || !validCoordinationDigest(intent.RequestDigest()) {
		return authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeOrganization, ID: intent.OrganizationID()}
	if scope.Validate(tenantID) != nil {
		return authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	return scope, intent.RequestDigest(), nil
}

func durableProjectUID(requestDigest string) string {
	return "project-" + strings.TrimPrefix(requestDigest, "sha256:")[:32]
}

func createDurableProjectTransaction(
	ctx context.Context,
	handle *tenantReadHandle,
	tenantID string,
	subjectDigest string,
	requestDigest string,
	projectUID string,
	input DurableProjectCreateInput,
) (DurableProjectCreateResult, error) {
	var result DurableProjectCreateResult
	var replayState *string
	var operationID *string
	var operationGeneration *int64
	var resourceKind *string
	var resourceID *string
	var resourceVersion *int64
	var stableErrorCode *string
	var outboxEventID *string
	var outboxState *string
	validated := input.Request
	err := handle.transaction.queryRow(ctx, createManagedAgentProjectDurableSQL,
		tenantID, subjectDigest, input.IdempotencyKey, requestDigest, projectUID,
		validated.Name, validated.OrganizationRef.ID, validated.DisplayName, input.AuditFactID,
	).Scan(&result.Disposition, &replayState, &operationID, &operationGeneration,
		&resourceKind, &resourceID, &resourceVersion, &stableErrorCode, &outboxEventID, &outboxState)
	if err != nil {
		return DurableProjectCreateResult{}, err
	}
	result.ReplayState = valueOrEmpty(replayState)
	result.OperationID = operationID
	result.OperationGeneration = operationGeneration
	result.ResourceKind = resourceKind
	result.ResourceID = resourceID
	result.ResourceVersion = resourceVersion
	result.StableErrorCode = stableErrorCode
	result.OutboxEventID = outboxEventID
	result.OutboxState = outboxState
	if result.ResourceKind == nil || *result.ResourceKind != "project" || result.ResourceID == nil {
		return result, nil
	}
	if err := handle.transaction.queryRow(ctx, getProjectSQL, *result.ResourceID).Scan(
		&result.Project.UID, &result.Project.Name, &result.Project.OrganizationID, &result.Project.DisplayName,
		&result.Project.State, &result.Project.ResourceVersion, &result.Project.CreatedAt, &result.Project.UpdatedAt,
	); err != nil {
		return DurableProjectCreateResult{}, err
	}
	result.Project.TenantID = tenantID
	return result, nil
}

func settleDurableProjectCreate(
	result DurableProjectCreateResult,
	err error,
) (DurableProjectCreateResult, error) {
	if errors.Is(err, ErrMutationCommitUnknown) {
		return DurableProjectCreateResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return DurableProjectCreateResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return DurableProjectCreateResult{}, mapCoordinationDatabaseError("create durable project", err)
	}
	if result.Disposition != "created" && result.Disposition != "replay" && result.Disposition != "conflict" {
		return DurableProjectCreateResult{}, ErrCoordinationResultDrift
	}
	if result.Disposition == "conflict" {
		if result.ReplayState != "" || result.OperationID != nil || result.ResourceID != nil || result.ResourceVersion != nil {
			return DurableProjectCreateResult{}, ErrCoordinationResultDrift
		}
		result.DatabaseOutcome = DatabaseRejected
		return result, nil
	}
	if result.ReplayState != "pending" && result.ReplayState != "succeeded" && result.ReplayState != "failed" {
		return DurableProjectCreateResult{}, ErrCoordinationResultDrift
	}
	if result.ReplayState == "succeeded" {
		if result.ResourceKind == nil || *result.ResourceKind != "project" || result.ResourceID == nil ||
			!validMutationIdentifier(*result.ResourceID) || result.ResourceVersion == nil || *result.ResourceVersion < 1 {
			return DurableProjectCreateResult{}, ErrCoordinationResultDrift
		}
	}
	if result.Disposition == "created" {
		if result.ReplayState != "succeeded" || result.OperationID == nil || result.OperationGeneration == nil ||
			*result.OperationGeneration < 1 || result.OutboxEventID == nil || result.OutboxState == nil || *result.OutboxState != "pending" {
			return DurableProjectCreateResult{}, ErrCoordinationResultDrift
		}
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
