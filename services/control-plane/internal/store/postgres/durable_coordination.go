package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	claimManagedAgentCreateProjectIdempotencySQL = `SELECT
    claim_disposition, replay_state, operation_id, operation_generation,
    resource_kind, resource_id, resource_version, stable_error_code, expires_at
FROM cloud_agents.claim_managed_agent_create_project_idempotency_v2($1, $2, $3, $4, $5)`
	completeManagedAgentCreateProjectSuccessSQL = `SELECT
    replay_state, resource_kind, resource_id, resource_version, outbox_event_id, outbox_state
FROM cloud_agents.complete_managed_agent_create_project_success_v2($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	completeManagedAgentCreateProjectFailureSQL = `SELECT replay_state, stable_error_code
FROM cloud_agents.complete_managed_agent_create_project_failure_v2($1, $2, $3, $4, $5, $6)`
	acquireCoordinationLeaderSQL = `SELECT lease_disposition, fencing_token, lease_expires_at
FROM cloud_agents.acquire_coordination_leader($1, $2, $3, $4)`
	renewCoordinationLeaderSQL = `SELECT lease_disposition, fencing_token, lease_expires_at
FROM cloud_agents.renew_coordination_leader($1, $2, $3, $4, $5)`
	claimOutboxEventSQL = `SELECT
    event_id, profile_id, profile_digest, event_class, aggregate_kind, aggregate_id,
    aggregate_sequence, resource_version, generation, operation_id, operation_generation,
    payload_digest, delivery_attempts, claim_expires_at
FROM cloud_agents.claim_outbox_event($1, $2, $3, $4, $5, $6, $7)`
	acknowledgeOutboxEventSQL = `SELECT event_id, outbox_state, delivery_attempts, next_attempt_at
FROM cloud_agents.acknowledge_outbox_event($1, $2, $3, $4, $5, $6, $7, $8)`
	retryOutboxEventSQL = `SELECT event_id, outbox_state, delivery_attempts, next_attempt_at
FROM cloud_agents.retry_outbox_event($1, $2, $3, $4, $5, $6, $7, $8)`
	deadLetterOutboxEventSQL = `SELECT event_id, outbox_state, delivery_attempts, next_attempt_at
FROM cloud_agents.dead_letter_outbox_event($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	reapExpiredOutboxClaimSQL = `SELECT event_id, outbox_state, delivery_attempts
FROM cloud_agents.reap_expired_outbox_claim($1, $2, $3, $4, $5, $6)`
)

var (
	ErrNilCoordinationRunner       = errors.New("postgres durable coordination runner is nil")
	ErrCoordinationInvalidInput    = errors.New("postgres durable coordination input is invalid")
	ErrCoordinationRejected        = errors.New("postgres durable coordination transition is rejected")
	ErrCoordinationAuthority       = errors.New("postgres durable coordination database authority is invalid")
	ErrCoordinationResultDrift     = errors.New("postgres durable coordination result drifted")
	ErrCoordinationCommitUnknown   = errors.New("postgres durable coordination commit outcome is unknown")
	ErrCoordinationPortUnavailable = errors.New("postgres durable coordination delivery port is unavailable")
)

var coordinationDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type DatabaseOutcome string

const (
	DatabaseCommitted DatabaseOutcome = "committed"
	DatabaseRejected  DatabaseOutcome = "rejected"
	DatabaseUnknown   DatabaseOutcome = "unknown"
)

type IdempotencyClaimInput struct {
	Profile        coordination.Profile
	Actor          authz.SubjectRef
	Request        coordination.ManagedAgentCreateProjectRequest
	IdempotencyKey string
	AuditFactID    string
}

type IdempotencyClaimResult struct {
	DatabaseOutcome     DatabaseOutcome
	Disposition         string
	ReplayState         string
	OperationID         *string
	OperationGeneration *int64
	ResourceKind        *string
	ResourceID          *string
	ResourceVersion     *int64
	StableErrorCode     *string
	ExpiresAt           time.Time
}

type IdempotencySuccessInput struct {
	Profile         coordination.Profile
	Actor           authz.SubjectRef
	Request         coordination.ManagedAgentCreateProjectRequest
	IdempotencyKey  string
	ResourceID      string
	ResourceVersion int64
	EventID         string
	PayloadDigest   string
	AuditFactID     string
}

type IdempotencySuccessResult struct {
	DatabaseOutcome DatabaseOutcome
	ReplayState     string
	ResourceKind    string
	ResourceID      string
	ResourceVersion int64
	OutboxEventID   string
	OutboxState     string
}

type IdempotencyFailureInput struct {
	Profile         coordination.Profile
	Actor           authz.SubjectRef
	Request         coordination.ManagedAgentCreateProjectRequest
	IdempotencyKey  string
	StableErrorCode string
	AuditFactID     string
}

type IdempotencyFailureResult struct {
	DatabaseOutcome DatabaseOutcome
	ReplayState     string
	StableErrorCode string
}

type LeaderLeaseInput struct {
	LeaderName        string
	HolderID          string
	HolderIncarnation string
	FencingToken      int64
	LeaseSeconds      int32
}

type LeaderLeaseResult struct {
	DatabaseOutcome DatabaseOutcome
	Disposition     string
	FencingToken    int64
	LeaseExpiresAt  time.Time
}

type OutboxClaimInput struct {
	HolderID          string
	HolderIncarnation string
	ClaimToken        string
	LeaseSeconds      int32
	SubjectDigest     string
	AuditFactID       string
}

type OutboxClaim struct {
	TenantID            string
	EventID             string
	ProfileID           string
	ProfileDigest       string
	EventClass          string
	AggregateKind       string
	AggregateID         string
	AggregateSequence   int64
	ResourceVersion     *int64
	Generation          int64
	OperationID         *string
	OperationGeneration *int64
	PayloadDigest       string
	DeliveryAttempts    int32
	HolderID            string
	HolderIncarnation   string
	ClaimToken          string
	ClaimExpiresAt      time.Time
}

type OutboxClaimResult struct {
	DatabaseOutcome DatabaseOutcome
	Found           bool
	Claim           OutboxClaim
}

type OutboxSettlementInput struct {
	Claim           OutboxClaim
	SubjectDigest   string
	AuditFactID     string
	StableErrorCode string
}

type OutboxSettlementResult struct {
	DatabaseOutcome  DatabaseOutcome
	EventID          string
	State            string
	DeliveryAttempts int32
	NextAttemptAt    *time.Time
}

type OutboxReapInput struct {
	LeaderHolderID          string
	LeaderHolderIncarnation string
	FencingToken            int64
	SubjectDigest           string
	AuditFactID             string
}

type OutboxReapResult struct {
	DatabaseOutcome  DatabaseOutcome
	Found            bool
	EventID          string
	State            string
	DeliveryAttempts int32
}

type DurableCoordinationService struct{ runner *TenantTransactionRunner }

func NewDurableCoordinationService(pool *pgxpool.Pool) (*DurableCoordinationService, error) {
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	return newDurableCoordinationService(runner)
}

func newDurableCoordinationService(runner *TenantTransactionRunner) (*DurableCoordinationService, error) {
	if runner == nil {
		return nil, ErrNilCoordinationRunner
	}
	return &DurableCoordinationService{runner: runner}, nil
}

func (service *DurableCoordinationService) ClaimIdempotency(
	ctx context.Context,
	tenantID string,
	input IdempotencyClaimInput,
) (IdempotencyClaimResult, error) {
	subjectDigest, scope, requestDigest, err := service.bindAuthorizedProfile(
		ctx, tenantID, input.Profile, input.Actor, input.Request, input.AuditFactID,
	)
	if err != nil || !validIdempotencyKey(input.IdempotencyKey) {
		if err != nil {
			return IdempotencyClaimResult{}, err
		}
		return IdempotencyClaimResult{}, ErrCoordinationInvalidInput
	}
	var result IdempotencyClaimResult
	var replayState *string
	var expiresAt *time.Time
	err = service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		if err := authorizeMutation(ctx, handle, input.Actor, input.Profile.RequiredPermission(), scope); err != nil {
			return err
		}
		return handle.transaction.queryRow(ctx, claimManagedAgentCreateProjectIdempotencySQL,
			tenantID, subjectDigest, input.IdempotencyKey, requestDigest, input.AuditFactID,
		).Scan(
			&result.Disposition, &replayState, &result.OperationID, &result.OperationGeneration,
			&result.ResourceKind, &result.ResourceID, &result.ResourceVersion, &result.StableErrorCode,
			&expiresAt,
		)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return IdempotencyClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return IdempotencyClaimResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return IdempotencyClaimResult{}, mapCoordinationDatabaseError("claim idempotency", err)
	}
	if replayState != nil {
		result.ReplayState = *replayState
	}
	if expiresAt != nil {
		result.ExpiresAt = *expiresAt
	}
	if !validIdempotencyClaimResult(result) {
		return IdempotencyClaimResult{}, ErrCoordinationResultDrift
	}
	if result.Disposition == "conflict" {
		result.DatabaseOutcome = DatabaseRejected
	} else {
		result.DatabaseOutcome = DatabaseCommitted
	}
	return result, nil
}

func (service *DurableCoordinationService) CompleteIdempotencySuccess(
	ctx context.Context,
	tenantID string,
	input IdempotencySuccessInput,
) (IdempotencySuccessResult, error) {
	subjectDigest, scope, requestDigest, err := service.bindAuthorizedProfile(
		ctx, tenantID, input.Profile, input.Actor, input.Request, input.AuditFactID,
	)
	if err != nil {
		return IdempotencySuccessResult{}, err
	}
	if !validIdempotencyKey(input.IdempotencyKey) ||
		!validMutationIdentifier(input.ResourceID) || input.ResourceVersion < 1 ||
		!validMutationIdentifier(input.EventID) || !validCoordinationDigest(input.PayloadDigest) {
		return IdempotencySuccessResult{}, ErrCoordinationInvalidInput
	}
	var result IdempotencySuccessResult
	err = service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		if err := authorizeMutation(ctx, handle, input.Actor, input.Profile.RequiredPermission(), scope); err != nil {
			return err
		}
		return handle.transaction.queryRow(ctx, completeManagedAgentCreateProjectSuccessSQL,
			tenantID, subjectDigest, input.IdempotencyKey, requestDigest, input.ResourceID,
			input.ResourceVersion, input.EventID, input.PayloadDigest, input.AuditFactID,
		).Scan(&result.ReplayState, &result.ResourceKind, &result.ResourceID, &result.ResourceVersion,
			&result.OutboxEventID, &result.OutboxState)
	})
	return settleIdempotencySuccess(result, err, input)
}

func (service *DurableCoordinationService) CompleteIdempotencyFailure(
	ctx context.Context,
	tenantID string,
	input IdempotencyFailureInput,
) (IdempotencyFailureResult, error) {
	subjectDigest, scope, requestDigest, err := service.bindAuthorizedProfile(
		ctx, tenantID, input.Profile, input.Actor, input.Request, input.AuditFactID,
	)
	if err != nil {
		return IdempotencyFailureResult{}, err
	}
	if !validIdempotencyKey(input.IdempotencyKey) ||
		!validMutationIdentifier(input.StableErrorCode) {
		return IdempotencyFailureResult{}, ErrCoordinationInvalidInput
	}
	var result IdempotencyFailureResult
	err = service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		if err := authorizeMutation(ctx, handle, input.Actor, input.Profile.RequiredPermission(), scope); err != nil {
			return err
		}
		return handle.transaction.queryRow(ctx, completeManagedAgentCreateProjectFailureSQL,
			tenantID, subjectDigest, input.IdempotencyKey, requestDigest,
			input.StableErrorCode, input.AuditFactID,
		).Scan(&result.ReplayState, &result.StableErrorCode)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return IdempotencyFailureResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return IdempotencyFailureResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return IdempotencyFailureResult{}, mapCoordinationDatabaseError("complete idempotency failure", err)
	}
	if result.ReplayState != "failed" || result.StableErrorCode != input.StableErrorCode {
		return IdempotencyFailureResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func (service *DurableCoordinationService) AcquireLeader(
	ctx context.Context,
	input LeaderLeaseInput,
) (LeaderLeaseResult, error) {
	if err := service.validateGlobal(ctx); err != nil {
		return LeaderLeaseResult{}, err
	}
	if !validLeaderInput(input, false) {
		return LeaderLeaseResult{}, ErrCoordinationInvalidInput
	}
	var result LeaderLeaseResult
	var fencingToken *int64
	var leaseExpiresAt *time.Time
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, acquireCoordinationLeaderSQL,
			input.LeaderName, input.HolderID, input.HolderIncarnation, input.LeaseSeconds,
		).Scan(&result.Disposition, &fencingToken, &leaseExpiresAt)
	})
	if fencingToken != nil {
		result.FencingToken = *fencingToken
	}
	if leaseExpiresAt != nil {
		result.LeaseExpiresAt = *leaseExpiresAt
	}
	return settleLeaderResult(result, err, "acquired", "busy")
}

func (service *DurableCoordinationService) RenewLeader(
	ctx context.Context,
	input LeaderLeaseInput,
) (LeaderLeaseResult, error) {
	if err := service.validateGlobal(ctx); err != nil {
		return LeaderLeaseResult{}, err
	}
	if !validLeaderInput(input, true) {
		return LeaderLeaseResult{}, ErrCoordinationInvalidInput
	}
	var result LeaderLeaseResult
	var fencingToken *int64
	var leaseExpiresAt *time.Time
	err := service.runner.withGlobalMutation(ctx, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, renewCoordinationLeaderSQL,
			input.LeaderName, input.HolderID, input.HolderIncarnation, input.FencingToken, input.LeaseSeconds,
		).Scan(&result.Disposition, &fencingToken, &leaseExpiresAt)
	})
	if fencingToken != nil {
		result.FencingToken = *fencingToken
	}
	if leaseExpiresAt != nil {
		result.LeaseExpiresAt = *leaseExpiresAt
	}
	return settleLeaderResult(result, err, "renewed", "rejected")
}

func (service *DurableCoordinationService) ClaimOutbox(
	ctx context.Context,
	tenantID string,
	input OutboxClaimInput,
) (OutboxClaimResult, error) {
	if err := service.validate(ctx, tenantID, coordination.ManagedAgentCreateProject(), input.SubjectDigest, input.AuditFactID); err != nil {
		return OutboxClaimResult{}, err
	}
	if !validMutationIdentifier(input.HolderID) || !validMutationIdentifier(input.HolderIncarnation) ||
		!validMutationIdentifier(input.ClaimToken) || input.LeaseSeconds < 1 || input.LeaseSeconds > 60 {
		return OutboxClaimResult{}, ErrCoordinationInvalidInput
	}
	result := OutboxClaimResult{Found: true}
	result.Claim.TenantID = tenantID
	result.Claim.HolderID = input.HolderID
	result.Claim.HolderIncarnation = input.HolderIncarnation
	result.Claim.ClaimToken = input.ClaimToken
	err := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		scanErr := handle.transaction.queryRow(ctx, claimOutboxEventSQL,
			tenantID, input.HolderID, input.HolderIncarnation, input.ClaimToken, input.LeaseSeconds,
			input.SubjectDigest, input.AuditFactID,
		).Scan(
			&result.Claim.EventID, &result.Claim.ProfileID, &result.Claim.ProfileDigest,
			&result.Claim.EventClass, &result.Claim.AggregateKind, &result.Claim.AggregateID,
			&result.Claim.AggregateSequence, &result.Claim.ResourceVersion, &result.Claim.Generation,
			&result.Claim.OperationID, &result.Claim.OperationGeneration, &result.Claim.PayloadDigest,
			&result.Claim.DeliveryAttempts, &result.Claim.ClaimExpiresAt,
		)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return scanErr
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return OutboxClaimResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return OutboxClaimResult{}, mapCoordinationDatabaseError("claim outbox event", err)
	}
	if !result.Found {
		result.DatabaseOutcome = DatabaseCommitted
		return result, nil
	}
	if !validOutboxClaim(result.Claim) {
		return OutboxClaimResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func (service *DurableCoordinationService) AcknowledgeOutbox(
	ctx context.Context, tenantID string, input OutboxSettlementInput,
) (OutboxSettlementResult, error) {
	return service.settleOutbox(ctx, tenantID, input, "delivery_succeeded")
}

func (service *DurableCoordinationService) RetryOutbox(
	ctx context.Context, tenantID string, input OutboxSettlementInput,
) (OutboxSettlementResult, error) {
	return service.settleOutbox(ctx, tenantID, input, "delivery_failed_retryable")
}

func (service *DurableCoordinationService) DeadLetterOutbox(
	ctx context.Context, tenantID string, input OutboxSettlementInput,
) (OutboxSettlementResult, error) {
	return service.settleOutbox(ctx, tenantID, input, "delivery_failed_terminal")
}

func (service *DurableCoordinationService) ReapExpiredOutbox(
	ctx context.Context, tenantID string, input OutboxReapInput,
) (OutboxReapResult, error) {
	if err := service.validate(ctx, tenantID, coordination.ManagedAgentCreateProject(), input.SubjectDigest, input.AuditFactID); err != nil {
		return OutboxReapResult{}, err
	}
	if !validMutationIdentifier(input.LeaderHolderID) || !validMutationIdentifier(input.LeaderHolderIncarnation) || input.FencingToken < 1 {
		return OutboxReapResult{}, ErrCoordinationInvalidInput
	}
	result := OutboxReapResult{Found: true}
	err := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		scanErr := handle.transaction.queryRow(ctx, reapExpiredOutboxClaimSQL,
			tenantID, input.LeaderHolderID, input.LeaderHolderIncarnation, input.FencingToken,
			input.SubjectDigest, input.AuditFactID,
		).Scan(&result.EventID, &result.State, &result.DeliveryAttempts)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			result.Found = false
			return nil
		}
		return scanErr
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return OutboxReapResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return OutboxReapResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return OutboxReapResult{}, mapCoordinationDatabaseError("reap expired outbox claim", err)
	}
	if !result.Found {
		result.DatabaseOutcome = DatabaseCommitted
		return result, nil
	}
	if !validMutationIdentifier(result.EventID) || (result.State != "pending" && result.State != "dead_letter") || result.DeliveryAttempts < 1 || result.DeliveryAttempts > 8 {
		return OutboxReapResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func (service *DurableCoordinationService) settleOutbox(
	ctx context.Context,
	tenantID string,
	input OutboxSettlementInput,
	transition string,
) (OutboxSettlementResult, error) {
	if err := service.validate(ctx, tenantID, coordination.ManagedAgentCreateProject(), input.SubjectDigest, input.AuditFactID); err != nil {
		return OutboxSettlementResult{}, err
	}
	if !validOutboxClaim(input.Claim) || input.Claim.TenantID != tenantID ||
		(transition == "delivery_failed_terminal" && !validMutationIdentifier(input.StableErrorCode)) ||
		(transition != "delivery_failed_terminal" && input.StableErrorCode != "") {
		return OutboxSettlementResult{}, ErrCoordinationInvalidInput
	}
	var result OutboxSettlementResult
	statement := acknowledgeOutboxEventSQL
	arguments := []any{
		tenantID, input.Claim.EventID, input.Claim.HolderID, input.Claim.HolderIncarnation,
		input.Claim.ClaimToken, input.Claim.ClaimExpiresAt, input.SubjectDigest, input.AuditFactID,
	}
	if transition == "delivery_failed_retryable" {
		statement = retryOutboxEventSQL
	} else if transition == "delivery_failed_terminal" {
		statement = deadLetterOutboxEventSQL
		arguments = []any{
			tenantID, input.Claim.EventID, input.Claim.HolderID, input.Claim.HolderIncarnation,
			input.Claim.ClaimToken, input.Claim.ClaimExpiresAt, input.StableErrorCode,
			input.SubjectDigest, input.AuditFactID,
		}
	}
	err := service.runner.withTenantMutation(ctx, tenantID, func(handle *tenantReadHandle) error {
		return handle.transaction.queryRow(ctx, statement, arguments...).Scan(
			&result.EventID, &result.State, &result.DeliveryAttempts, &result.NextAttemptAt,
		)
	})
	if errors.Is(err, ErrMutationCommitUnknown) {
		return OutboxSettlementResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return OutboxSettlementResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return OutboxSettlementResult{}, mapCoordinationDatabaseError("settle outbox claim", err)
	}
	if result.EventID != input.Claim.EventID || result.DeliveryAttempts != input.Claim.DeliveryAttempts || !validSettledOutboxState(result, transition) {
		return OutboxSettlementResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func (service *DurableCoordinationService) validate(
	ctx context.Context, tenantID string, profile coordination.Profile, subjectDigest, auditFactID string,
) error {
	if err := service.validateProfile(ctx, tenantID, profile, auditFactID); err != nil {
		return err
	}
	if !validCoordinationDigest(subjectDigest) {
		return ErrCoordinationInvalidInput
	}
	return nil
}

func (service *DurableCoordinationService) validateProfile(
	ctx context.Context, tenantID string, profile coordination.Profile, auditFactID string,
) error {
	if err := service.validateGlobal(ctx); err != nil {
		return err
	}
	currentProfile := coordination.ManagedAgentCreateProject()
	if !validMutationIdentifier(tenantID) || !profile.Valid() ||
		profile.OperationID() != "managedAgentCreateProject" || profile.ProfileID() != "managedAgentCreateProject/v1alpha1" ||
		profile.ProfileDigest() != currentProfile.ProfileDigest() ||
		profile.ProjectionSchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-idempotency-projection.schema.json" ||
		profile.CanonicalizationProfile() != "cloud-agents-http-idempotency/managedAgentCreateProject/v1alpha1" ||
		profile.CanonicalizationAlgorithm() != "RFC8785" || profile.DigestAlgorithm() != "SHA-256" ||
		profile.TenantSource() != "path.tenantId" || profile.ScopeSource() != "body.organizationRef" ||
		profile.ScopeIdentitySchemaID() != "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json" ||
		profile.ScopeIdentifierProfile() != "cloud-agents-authorization-scope-identifier/ascii-v1" ||
		profile.ScopeIdentityComparison() != "exact_string_no_rewrite" ||
		profile.RequiredPermission() != "projects.create" || profile.RequiredScopeLevel() != "organization" ||
		profile.CreatesPlatformOperation() || profile.ExternalSideEffectAllowed() ||
		profile.OutboxEventClass() != "resource_change" || profile.ResultResourceKind() != "project" ||
		profile.ReplayTTLSeconds() != 86400 || !validMutationIdentifier(auditFactID) {
		return ErrCoordinationInvalidInput
	}
	return nil
}

func (service *DurableCoordinationService) bindAuthorizedProfile(
	ctx context.Context,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	request coordination.ManagedAgentCreateProjectRequest,
	auditFactID string,
) (string, authz.ScopeRef, string, error) {
	if err := service.validateProfile(ctx, tenantID, profile, auditFactID); err != nil {
		return "", authz.ScopeRef{}, "", err
	}
	intent, err := coordination.BindManagedAgentCreateProject(profile, tenantID, request)
	if err != nil {
		return "", authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	scope := authz.ScopeRef{Level: authz.ScopeOrganization, ID: intent.OrganizationID()}
	if actor.Validate() != nil || scope.Level != authz.ScopeOrganization || scope.Validate(tenantID) != nil {
		return "", authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	digest, err := actor.Digest()
	if err != nil || !validCoordinationDigest(digest) || !validCoordinationDigest(intent.RequestDigest()) {
		return "", authz.ScopeRef{}, "", ErrCoordinationInvalidInput
	}
	return digest, scope, intent.RequestDigest(), nil
}

func (service *DurableCoordinationService) validateGlobal(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	if service == nil || service.runner == nil {
		return ErrNilCoordinationRunner
	}
	return ctx.Err()
}

func settleIdempotencySuccess(result IdempotencySuccessResult, err error, input IdempotencySuccessInput) (IdempotencySuccessResult, error) {
	if errors.Is(err, ErrMutationCommitUnknown) {
		return IdempotencySuccessResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if isCoordinationRejection(err) {
		return IdempotencySuccessResult{DatabaseOutcome: DatabaseRejected}, nil
	}
	if err != nil {
		return IdempotencySuccessResult{}, mapCoordinationDatabaseError("complete idempotency success", err)
	}
	if result.ReplayState != "succeeded" || result.ResourceKind != input.Profile.ResultResourceKind() ||
		result.ResourceID != input.ResourceID || result.ResourceVersion != input.ResourceVersion ||
		result.OutboxEventID != input.EventID || result.OutboxState != "pending" {
		return IdempotencySuccessResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func settleLeaderResult(result LeaderLeaseResult, err error, accepted, rejected string) (LeaderLeaseResult, error) {
	if errors.Is(err, ErrMutationCommitUnknown) {
		return LeaderLeaseResult{DatabaseOutcome: DatabaseUnknown}, nil
	}
	if err != nil {
		return LeaderLeaseResult{}, mapCoordinationDatabaseError("settle leader lease", err)
	}
	if result.Disposition == rejected {
		if rejected == "busy" && (result.FencingToken < 1 || result.LeaseExpiresAt.IsZero()) {
			return LeaderLeaseResult{}, ErrCoordinationResultDrift
		}
		if rejected == "rejected" && (result.FencingToken != 0 || !result.LeaseExpiresAt.IsZero()) {
			return LeaderLeaseResult{}, ErrCoordinationResultDrift
		}
		result.DatabaseOutcome = DatabaseRejected
		return result, nil
	}
	if result.Disposition != accepted || result.FencingToken < 1 || result.LeaseExpiresAt.IsZero() {
		return LeaderLeaseResult{}, ErrCoordinationResultDrift
	}
	result.DatabaseOutcome = DatabaseCommitted
	return result, nil
}

func mapCoordinationDatabaseError(operation string, err error) error {
	if errors.Is(err, ErrMutationCommitUnknown) {
		return ErrCoordinationCommitUnknown
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.Code {
	case "22023", "22003", "23502", "23503", "23514":
		return fmt.Errorf("%w: %s", ErrCoordinationInvalidInput, operation)
	case "23505", "40001":
		return fmt.Errorf("%w: %s", ErrCoordinationRejected, operation)
	case "42501":
		return fmt.Errorf("%w: %s", ErrCoordinationAuthority, operation)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

func isCoordinationRejection(err error) bool {
	if errors.Is(err, ErrMutationConflict) || errors.Is(err, ErrCoordinationRejected) {
		return true
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "23505")
}

func validCoordinationDigest(value string) bool { return coordinationDigestPattern.MatchString(value) }

func validIdempotencyKey(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' || character == '~' || character == '-') {
			return false
		}
	}
	return true
}

func validIdempotencyClaimResult(result IdempotencyClaimResult) bool {
	if result.Disposition != "created" && result.Disposition != "replay" && result.Disposition != "conflict" {
		return false
	}
	if result.Disposition == "conflict" {
		return result.ReplayState == "" && result.ExpiresAt.IsZero() && result.OperationID == nil &&
			result.OperationGeneration == nil && result.ResourceKind == nil && result.ResourceID == nil &&
			result.ResourceVersion == nil && result.StableErrorCode == nil
	}
	if result.ReplayState != "pending" && result.ReplayState != "succeeded" && result.ReplayState != "failed" {
		return false
	}
	if result.ExpiresAt.IsZero() || result.OperationID != nil || result.OperationGeneration != nil {
		return false
	}
	if result.ReplayState == "pending" {
		return result.ResourceKind == nil && result.ResourceID == nil && result.ResourceVersion == nil && result.StableErrorCode == nil
	}
	if result.ReplayState == "succeeded" {
		return result.ResourceKind != nil && *result.ResourceKind == "project" && result.ResourceID != nil &&
			validMutationIdentifier(*result.ResourceID) && result.ResourceVersion != nil && *result.ResourceVersion > 0 && result.StableErrorCode == nil
	}
	return result.ResourceKind == nil && result.ResourceID == nil && result.ResourceVersion == nil &&
		result.StableErrorCode != nil && validMutationIdentifier(*result.StableErrorCode)
}

func validLeaderInput(input LeaderLeaseInput, requireToken bool) bool {
	if input.LeaderName != "coordination-reconciler" && input.LeaderName != "finalizer-reconciler" && input.LeaderName != "outbox-dispatcher" {
		return false
	}
	return validMutationIdentifier(input.HolderID) && validMutationIdentifier(input.HolderIncarnation) &&
		input.LeaseSeconds >= 1 && input.LeaseSeconds <= 60 && (!requireToken || input.FencingToken > 0)
}

func validOutboxClaim(claim OutboxClaim) bool {
	profile := coordination.ManagedAgentCreateProject()
	if !validMutationIdentifier(claim.TenantID) || !validMutationIdentifier(claim.EventID) ||
		claim.ProfileID != profile.ProfileID() || claim.ProfileDigest != profile.ProfileDigest() ||
		claim.EventClass != "resource_change" || claim.AggregateKind != "project" ||
		!validMutationIdentifier(claim.AggregateID) || claim.AggregateSequence < 1 ||
		claim.ResourceVersion == nil || *claim.ResourceVersion != claim.AggregateSequence || claim.Generation != 0 ||
		claim.OperationID != nil || claim.OperationGeneration != nil || !validCoordinationDigest(claim.PayloadDigest) ||
		claim.DeliveryAttempts < 1 || claim.DeliveryAttempts > 8 || !validMutationIdentifier(claim.HolderID) ||
		!validMutationIdentifier(claim.HolderIncarnation) || !validMutationIdentifier(claim.ClaimToken) || claim.ClaimExpiresAt.IsZero() {
		return false
	}
	return true
}

func validSettledOutboxState(result OutboxSettlementResult, transition string) bool {
	switch transition {
	case "delivery_succeeded":
		return result.State == "delivered" && result.NextAttemptAt == nil
	case "delivery_failed_retryable":
		return result.State == "retry_wait" && result.NextAttemptAt != nil && !result.NextAttemptAt.IsZero()
	case "delivery_failed_terminal":
		return result.State == "dead_letter" && result.NextAttemptAt == nil
	default:
		return false
	}
}

type outboxDeliveryDisposition string

const (
	outboxDelivered  outboxDeliveryDisposition = "delivered"
	outboxRetry      outboxDeliveryDisposition = "retry"
	outboxDeadLetter outboxDeliveryDisposition = "dead_letter"
)

type outboxDeliveryResult struct {
	disposition     outboxDeliveryDisposition
	stableErrorCode string
}

type outboxDeliveryPort interface {
	deliver(context.Context, OutboxClaim) outboxDeliveryResult
}

type outboxDispatcher struct {
	service *DurableCoordinationService
	port    outboxDeliveryPort
}

func newOutboxDispatcher(service *DurableCoordinationService, port outboxDeliveryPort) (*outboxDispatcher, error) {
	if service == nil || service.runner == nil || port == nil {
		return nil, ErrCoordinationPortUnavailable
	}
	return &outboxDispatcher{service: service, port: port}, nil
}

func (dispatcher *outboxDispatcher) dispatchOne(
	ctx context.Context, tenantID string, claimInput OutboxClaimInput, settlementAuditFactID string,
) (OutboxSettlementResult, error) {
	if dispatcher == nil || dispatcher.service == nil || dispatcher.port == nil {
		return OutboxSettlementResult{}, ErrCoordinationPortUnavailable
	}
	claimResult, err := dispatcher.service.ClaimOutbox(ctx, tenantID, claimInput)
	if err != nil || claimResult.DatabaseOutcome != DatabaseCommitted || !claimResult.Found {
		return OutboxSettlementResult{DatabaseOutcome: claimResult.DatabaseOutcome}, err
	}
	portResult := dispatcher.port.deliver(ctx, claimResult.Claim)
	settlement := OutboxSettlementInput{
		Claim: claimResult.Claim, SubjectDigest: claimInput.SubjectDigest, AuditFactID: settlementAuditFactID,
		StableErrorCode: portResult.stableErrorCode,
	}
	switch portResult.disposition {
	case outboxDelivered:
		return dispatcher.service.AcknowledgeOutbox(ctx, tenantID, settlement)
	case outboxRetry:
		settlement.StableErrorCode = ""
		return dispatcher.service.RetryOutbox(ctx, tenantID, settlement)
	case outboxDeadLetter:
		return dispatcher.service.DeadLetterOutbox(ctx, tenantID, settlement)
	default:
		return OutboxSettlementResult{}, ErrCoordinationResultDrift
	}
}
