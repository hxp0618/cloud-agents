package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/compatibility"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNilCompatibilityRecoveryRunner = errors.New("postgres compatibility recovery runner is nil")
	ErrCompatibilityRecoveryInput     = errors.New("postgres compatibility recovery input is invalid")
	ErrCompatibilityRecoveryAuthority = errors.New("postgres compatibility recovery database authority is invalid")
	ErrCompatibilityRecoveryRejected  = errors.New("postgres compatibility recovery transition is rejected")
	ErrCompatibilityRecoveryDatabase  = errors.New("postgres compatibility recovery database operation failed")
	ErrCompatibilityRecoveryDrift     = errors.New("postgres compatibility recovery result drifted")
	ErrCompatibilityRecoveryClaim     = errors.New("postgres compatibility recovery claim is invalid")
)

var (
	compatibilityDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	compatibilityPhasePattern  = regexp.MustCompile(`^[0-9]{6}$`)
)

const (
	compatibilityCommonColumns = `result_code, write_applied, reconcile_required,
    state, version, writer_epoch, database_timestamp, stable_error_code`
	compatibilityWorkloadReconcileColumns = compatibilityCommonColumns + `,
    principal_id, transition_observed`
	compatibilityLiveMutationColumns = compatibilityCommonColumns + `,
    heartbeat_at`
	compatibilityLiveReconcileColumns = compatibilityCommonColumns + `,
    heartbeat_at, heartbeat_ttl_seconds, transition_observed`
	compatibilityBackfillMutationColumns = compatibilityCommonColumns + `,
    lease_expires_at`
	compatibilityBackfillReconcileColumns = compatibilityCommonColumns + `,
    lease_owner, lease_expires_at, phase, cursor, digest, count, transition_observed`
	compatibilityRestoreMutationColumns = compatibilityCommonColumns + `,
    evidence_digest, target_schema_bundle_digest, target_phase`
	compatibilityRestoreReconcileColumns = compatibilityRestoreMutationColumns + `,
    transition_observed`
	compatibilityRetirementMutationColumns = compatibilityCommonColumns + `,
    credential_revoked, endpoint_revoked, process_terminated, leader_released,
    claim_released, generation_fenced, receipt_digest`
	compatibilityRetirementReconcileColumns = compatibilityRetirementMutationColumns + `,
    transition_observed`
	compatibilityPreflightColumns = compatibilityCommonColumns + `,
    decision, evaluated_at, ledger_checksum, postgres_major,
    restore_evidence_digest, rollout_generation, target_phase,
    target_schema_bundle_digest`
)

// TransitionEvidence is the complete idempotency identity for one mutation.
// Callers retain it for an explicit read-only reconcile after an unknown
// commit; the service never retries the write automatically.
type TransitionEvidence struct {
	TransitionDigest string
	RequestDigest    string
}

type CompatibilityResult struct {
	DatabaseOutcome   DatabaseOutcome
	ResultCode        string
	WriteApplied      bool
	ReconcileRequired bool
	State             string
	Version           int64
	WriterEpoch       int64
	DatabaseTimestamp time.Time
	StableErrorCode   *string
}

type WorkloadPrincipalIdentity struct {
	WorkloadID string
	Provider   string
}

type RegisterWorkloadPrincipalInput struct {
	Identity    WorkloadPrincipalIdentity
	PrincipalID string
	Epoch       int64
	Evidence    TransitionEvidence
}

type RotateWorkloadPrincipalInput struct {
	Identity            WorkloadPrincipalIdentity
	ExpectedPrincipalID string
	NewPrincipalID      string
	ExpectedEpoch       int64
	NewEpoch            int64
	Evidence            TransitionEvidence
}

type RevokeWorkloadPrincipalInput struct {
	Identity            WorkloadPrincipalIdentity
	ExpectedPrincipalID string
	ExpectedEpoch       int64
	Evidence            TransitionEvidence
}

type ReconcileWorkloadPrincipalInput struct {
	Identity         WorkloadPrincipalIdentity
	TransitionDigest string
}

type WorkloadPrincipalResult struct {
	CompatibilityResult
	PrincipalID        *string
	TransitionObserved bool
}

type LiveInstanceIdentity struct {
	ServiceKind       string
	InstanceID        string
	Incarnation       int64
	RolloutGeneration int64
}

type RegisterLiveInstanceInput struct {
	Identity            LiveInstanceIdentity
	WriterEpoch         int64
	BinaryVersion       string
	SupportedSchemaMin  string
	SupportedSchemaMax  string
	HeartbeatTTLSeconds int32
	Evidence            TransitionEvidence
}

type LiveInstanceEpochTransitionInput struct {
	Identity            LiveInstanceIdentity
	ExpectedWriterEpoch int64
	NewWriterEpoch      int64
	Evidence            TransitionEvidence
}

type HeartbeatLiveInstanceInput struct {
	Identity            LiveInstanceIdentity
	WriterEpoch         int64
	HeartbeatTTLSeconds int32
	Evidence            TransitionEvidence
}

type ReconcileLiveInstanceInput struct {
	Identity         LiveInstanceIdentity
	TransitionDigest string
}

type LiveInstanceResult struct {
	CompatibilityResult
	HeartbeatAt         *time.Time
	HeartbeatTTLSeconds *int32
	TransitionObserved  bool
}

type StartBackfillInput struct {
	MigrationID string
	Phase       string
	Cursor      string
	Digest      string
	WriterEpoch int64
	Evidence    TransitionEvidence
}

type AcquireBackfillLeaseInput struct {
	MigrationID         string
	LeaseOwner          string
	ExpectedWriterEpoch int64
	NewWriterEpoch      int64
	LeaseSeconds        int32
	Evidence            TransitionEvidence
}

type BackfillProgressInput struct {
	MigrationID string
	LeaseOwner  string
	WriterEpoch int64
	Phase       string
	Cursor      string
	Digest      string
	Count       int64
	Evidence    TransitionEvidence
}

type HeartbeatBackfillInput struct {
	MigrationID  string
	LeaseOwner   string
	WriterEpoch  int64
	LeaseSeconds int32
	Evidence     TransitionEvidence
}

type ReconcileBackfillInput struct {
	MigrationID      string
	TransitionDigest string
}

type BackfillResult struct {
	CompatibilityResult
	LeaseOwner         *string
	LeaseExpiresAt     *time.Time
	Phase              *string
	Cursor             *string
	Digest             *string
	Count              *int64
	TransitionObserved bool
}

type RecordRestoreEvidenceInput struct {
	DrillID                  string
	PostgresMajor            int32
	LedgerChecksum           string
	TargetSchemaBundleDigest string
	TargetPhase              string
	RestorePointDigest       string
	EvidenceDigest           string
	DrillAt                  time.Time
	Evidence                 TransitionEvidence
}

type RestoreEvidenceTransitionInput struct {
	DrillID         string
	ExpectedVersion int64
	EvidenceDigest  string
	Evidence        TransitionEvidence
}

type RejectRestoreEvidenceInput struct {
	RestoreEvidenceTransitionInput
	RejectionReason string
}

type ReconcileRestoreEvidenceInput struct {
	DrillID          string
	TransitionDigest string
}

type RestoreEvidenceResult struct {
	CompatibilityResult
	EvidenceDigest           *string
	TargetSchemaBundleDigest *string
	TargetPhase              *string
	TransitionObserved       bool
}

type RetirementReceiptIdentity struct {
	ServiceKind       string
	InstanceID        string
	Incarnation       int64
	RolloutGeneration int64
}

type CollectRetirementReceiptInput struct {
	Identity          RetirementReceiptIdentity
	WriterEpoch       int64
	ExpectedVersion   int64
	CredentialRevoked bool
	EndpointRevoked   bool
	ProcessTerminated bool
	LeaderReleased    bool
	ClaimReleased     bool
	GenerationFenced  bool
	Evidence          TransitionEvidence
}

type CompleteRetirementReceiptInput struct {
	Identity        RetirementReceiptIdentity
	WriterEpoch     int64
	ExpectedVersion int64
	ReceiptDigest   string
	Evidence        TransitionEvidence
}

type RejectRetirementReceiptInput struct {
	Identity        RetirementReceiptIdentity
	WriterEpoch     int64
	ExpectedVersion int64
	RejectionReason string
	Evidence        TransitionEvidence
}

type ReconcileRetirementReceiptInput struct {
	Identity         RetirementReceiptIdentity
	TransitionDigest string
}

type RetirementReceiptResult struct {
	CompatibilityResult
	CredentialRevoked  bool
	EndpointRevoked    bool
	ProcessTerminated  bool
	LeaderReleased     bool
	ClaimReleased      bool
	GenerationFenced   bool
	ReceiptDigest      *string
	TransitionObserved bool
}

type EvaluateMigrationPreflightInput struct {
	PostgresMajor                        int32
	LedgerChecksum                       string
	TargetSchemaBundleDigest             string
	TargetPhase                          string
	RolloutGeneration                    int64
	WriterEpoch                          int64
	RestoreEvidenceDigest                string
	RequiresIrreversibleContractApproval bool
	IrreversibleContractApprovalDigest   *string
}

type MigrationPreflightResult struct {
	CompatibilityResult
	Decision                 string
	EvaluatedAt              time.Time
	LedgerChecksum           string
	PostgresMajor            int32
	RestoreEvidenceDigest    string
	RolloutGeneration        int64
	TargetPhase              string
	TargetSchemaBundleDigest string
}

// CompatibilityRecoveryService exposes only generated operation-specific
// methods. It has no HTTP, provider, worker, session, turn, execution, or
// external-side-effect port.
type CompatibilityRecoveryService struct {
	runner *TenantTransactionRunner
}

func NewCompatibilityRecoveryService(pool *pgxpool.Pool) (*CompatibilityRecoveryService, error) {
	runner, err := NewTenantTransactionRunner(pool)
	if err != nil {
		return nil, err
	}
	return newCompatibilityRecoveryService(runner)
}

func newCompatibilityRecoveryService(runner *TenantTransactionRunner) (*CompatibilityRecoveryService, error) {
	if runner == nil {
		return nil, ErrNilCompatibilityRecoveryRunner
	}
	return &CompatibilityRecoveryService{runner: runner}, nil
}

type compatibilityRecoveryClaim struct {
	self      *compatibilityRecoveryClaim
	operation compatibility.Operation
	tenantID  string
	arguments []any
	columns   string
	consumed  bool
}

func (service *CompatibilityRecoveryService) bindClaim(
	ctx context.Context,
	tenantID string,
	operation compatibility.Operation,
	columns string,
	arguments ...any,
) (*compatibilityRecoveryClaim, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if service == nil || service.runner == nil {
		return nil, ErrNilCompatibilityRecoveryRunner
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validMutationIdentifier(tenantID) || !validCompatibilityOperation(operation) || columns == "" {
		return nil, ErrCompatibilityRecoveryInput
	}
	claim := &compatibilityRecoveryClaim{
		operation: operation,
		tenantID:  tenantID,
		arguments: append([]any(nil), arguments...),
		columns:   columns,
	}
	claim.self = claim
	return claim, nil
}

func (claim *compatibilityRecoveryClaim) consume() (string, []any, error) {
	if claim == nil || claim.self != claim || claim.consumed || !claim.operation.Valid() ||
		claim.tenantID == "" || claim.columns == "" {
		return "", nil, ErrCompatibilityRecoveryClaim
	}
	claim.consumed = true
	placeholders := make([]string, len(claim.arguments))
	for index := range placeholders {
		placeholders[index] = fmt.Sprintf("$%d", index+1)
	}
	statement := "SELECT " + claim.columns + " FROM " + claim.operation.SQLFunction() +
		"(" + strings.Join(placeholders, ", ") + ")"
	return statement, append([]any(nil), claim.arguments...), nil
}

func (service *CompatibilityRecoveryService) executeClaim(
	ctx context.Context,
	claim *compatibilityRecoveryClaim,
	common *CompatibilityResult,
	scan func(rowScanner) error,
) error {
	if claim == nil || common == nil || scan == nil {
		return ErrCompatibilityRecoveryClaim
	}
	var err error
	binder := bindTenant
	if claim.operation.ProfileID() == "workload-principal/v2" {
		binder = bindTenantSetting
	}
	if claim.operation.IsMutation() {
		err = service.runner.withTenantMutationBinder(ctx, claim.tenantID, func(handle *tenantReadHandle) error {
			statement, arguments, consumeErr := claim.consume()
			if consumeErr != nil {
				return consumeErr
			}
			if scanErr := scan(handle.transaction.queryRow(ctx, statement, arguments...)); scanErr != nil {
				return scanErr
			}
			if !validCompatibilityResult(*common, claim.operation) {
				return ErrCompatibilityRecoveryDrift
			}
			return nil
		}, binder)
	} else {
		err = service.runner.withTenantReadBinder(ctx, claim.tenantID, func(_ context.Context, capability TenantReadCapability) error {
			handle, ok := capability.(*tenantReadHandle)
			if !ok || handle == nil {
				return ErrCompatibilityRecoveryAuthority
			}
			statement, arguments, consumeErr := claim.consume()
			if consumeErr != nil {
				return consumeErr
			}
			if scanErr := scan(handle.transaction.queryRow(ctx, statement, arguments...)); scanErr != nil {
				return scanErr
			}
			if !validCompatibilityResult(*common, claim.operation) {
				return ErrCompatibilityRecoveryDrift
			}
			return nil
		}, binder)
	}
	if errors.Is(err, ErrMutationCommitUnknown) && claim.operation.IsMutation() {
		*common = CompatibilityResult{
			DatabaseOutcome:   DatabaseUnknown,
			ReconcileRequired: true,
		}
		return nil
	}
	if err != nil {
		return mapCompatibilityRecoveryError(err)
	}
	switch common.ResultCode {
	case "rejected", "conflict":
		common.DatabaseOutcome = DatabaseRejected
	default:
		common.DatabaseOutcome = DatabaseCommitted
	}
	return nil
}

func scanCompatibilityCommon(row rowScanner, result *CompatibilityResult, extras ...any) error {
	destinations := []any{
		&result.ResultCode, &result.WriteApplied, &result.ReconcileRequired,
		&result.State, &result.Version, &result.WriterEpoch,
		&result.DatabaseTimestamp, &result.StableErrorCode,
	}
	destinations = append(destinations, extras...)
	return row.Scan(destinations...)
}

func (service *CompatibilityRecoveryService) executeWorkload(
	ctx context.Context, tenantID string, operation compatibility.Operation, arguments []any,
) (WorkloadPrincipalResult, error) {
	columns := compatibilityCommonColumns
	result := WorkloadPrincipalResult{}
	if !operation.IsMutation() {
		columns = compatibilityWorkloadReconcileColumns
	}
	claim, err := service.bindClaim(ctx, tenantID, operation, columns, arguments...)
	if err != nil {
		return WorkloadPrincipalResult{}, err
	}
	scan := func(row rowScanner) error {
		var err error
		if operation.IsMutation() {
			err = scanCompatibilityCommon(row, &result.CompatibilityResult)
		} else {
			err = scanCompatibilityCommon(row, &result.CompatibilityResult, &result.PrincipalID, &result.TransitionObserved)
		}
		if err != nil {
			return err
		}
		if !operation.IsMutation() && !validObservedPointer(result.ResultCode, result.PrincipalID) {
			return ErrCompatibilityRecoveryDrift
		}
		return nil
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return WorkloadPrincipalResult{}, err
	}
	return result, nil
}

func (service *CompatibilityRecoveryService) RegisterWorkloadPrincipal(ctx context.Context, tenantID string, input RegisterWorkloadPrincipalInput) (WorkloadPrincipalResult, error) {
	if !validWorkloadIdentity(input.Identity) || !validMutationIdentifier(input.PrincipalID) || input.Epoch < 1 || !validTransitionEvidence(input.Evidence) {
		return WorkloadPrincipalResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeWorkload(ctx, tenantID, compatibility.RegisterWorkloadPrincipalOperation(), []any{
		tenantID, input.Identity.WorkloadID, input.Identity.Provider, input.PrincipalID, input.Epoch,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) RotateWorkloadPrincipal(ctx context.Context, tenantID string, input RotateWorkloadPrincipalInput) (WorkloadPrincipalResult, error) {
	if !validWorkloadIdentity(input.Identity) || !validMutationIdentifier(input.ExpectedPrincipalID) ||
		!validMutationIdentifier(input.NewPrincipalID) || input.ExpectedEpoch < 1 || input.NewEpoch <= input.ExpectedEpoch ||
		!validTransitionEvidence(input.Evidence) {
		return WorkloadPrincipalResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeWorkload(ctx, tenantID, compatibility.RotateWorkloadPrincipalOperation(), []any{
		tenantID, input.Identity.WorkloadID, input.Identity.Provider, input.ExpectedPrincipalID,
		input.NewPrincipalID, input.ExpectedEpoch, input.NewEpoch,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) RevokeWorkloadPrincipal(ctx context.Context, tenantID string, input RevokeWorkloadPrincipalInput) (WorkloadPrincipalResult, error) {
	if !validWorkloadIdentity(input.Identity) || !validMutationIdentifier(input.ExpectedPrincipalID) || input.ExpectedEpoch < 1 || !validTransitionEvidence(input.Evidence) {
		return WorkloadPrincipalResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeWorkload(ctx, tenantID, compatibility.RevokeWorkloadPrincipalOperation(), []any{
		tenantID, input.Identity.WorkloadID, input.Identity.Provider, input.ExpectedPrincipalID,
		input.ExpectedEpoch, input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) ReconcileWorkloadPrincipal(ctx context.Context, tenantID string, input ReconcileWorkloadPrincipalInput) (WorkloadPrincipalResult, error) {
	if !validWorkloadIdentity(input.Identity) || !validCompatibilityDigest(input.TransitionDigest) {
		return WorkloadPrincipalResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeWorkload(ctx, tenantID, compatibility.ReconcileWorkloadPrincipalOperation(), []any{
		tenantID, input.Identity.WorkloadID, input.Identity.Provider, input.TransitionDigest,
	})
}

func (service *CompatibilityRecoveryService) executeLive(
	ctx context.Context, tenantID string, operation compatibility.Operation, arguments []any,
) (LiveInstanceResult, error) {
	columns := compatibilityLiveMutationColumns
	result := LiveInstanceResult{}
	if !operation.IsMutation() {
		columns = compatibilityLiveReconcileColumns
	}
	claim, err := service.bindClaim(ctx, tenantID, operation, columns, arguments...)
	if err != nil {
		return LiveInstanceResult{}, err
	}
	scan := func(row rowScanner) error {
		if operation.IsMutation() {
			return scanCompatibilityCommon(row, &result.CompatibilityResult, &result.HeartbeatAt)
		}
		return scanCompatibilityCommon(row, &result.CompatibilityResult, &result.HeartbeatAt, &result.HeartbeatTTLSeconds, &result.TransitionObserved)
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return LiveInstanceResult{}, err
	}
	return result, nil
}

func (service *CompatibilityRecoveryService) RegisterLiveInstance(ctx context.Context, tenantID string, input RegisterLiveInstanceInput) (LiveInstanceResult, error) {
	if !validLiveIdentity(input.Identity) || input.WriterEpoch < 1 || len(input.BinaryVersion) < 1 || len(input.BinaryVersion) > 128 ||
		!compatibilityPhasePattern.MatchString(input.SupportedSchemaMin) || !compatibilityPhasePattern.MatchString(input.SupportedSchemaMax) ||
		input.SupportedSchemaMin > input.SupportedSchemaMax || input.HeartbeatTTLSeconds < 1 || input.HeartbeatTTLSeconds > 300 ||
		!validTransitionEvidence(input.Evidence) {
		return LiveInstanceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeLive(ctx, tenantID, compatibility.RegisterLiveInstanceOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.WriterEpoch, input.BinaryVersion,
		input.SupportedSchemaMin, input.SupportedSchemaMax, input.HeartbeatTTLSeconds,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) liveEpochTransition(ctx context.Context, tenantID string, operation compatibility.Operation, input LiveInstanceEpochTransitionInput) (LiveInstanceResult, error) {
	if !validLiveIdentity(input.Identity) || input.ExpectedWriterEpoch < 1 || input.NewWriterEpoch <= input.ExpectedWriterEpoch || !validTransitionEvidence(input.Evidence) {
		return LiveInstanceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeLive(ctx, tenantID, operation, []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.ExpectedWriterEpoch, input.NewWriterEpoch,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) ActivateLiveInstance(ctx context.Context, tenantID string, input LiveInstanceEpochTransitionInput) (LiveInstanceResult, error) {
	return service.liveEpochTransition(ctx, tenantID, compatibility.ActivateLiveInstanceOperation(), input)
}

func (service *CompatibilityRecoveryService) BeginLiveInstanceDrain(ctx context.Context, tenantID string, input LiveInstanceEpochTransitionInput) (LiveInstanceResult, error) {
	return service.liveEpochTransition(ctx, tenantID, compatibility.BeginLiveInstanceDrainOperation(), input)
}

func (service *CompatibilityRecoveryService) FinishLiveInstanceDrain(ctx context.Context, tenantID string, input LiveInstanceEpochTransitionInput) (LiveInstanceResult, error) {
	return service.liveEpochTransition(ctx, tenantID, compatibility.FinishLiveInstanceDrainOperation(), input)
}

func (service *CompatibilityRecoveryService) FenceLiveInstance(ctx context.Context, tenantID string, input LiveInstanceEpochTransitionInput) (LiveInstanceResult, error) {
	return service.liveEpochTransition(ctx, tenantID, compatibility.FenceLiveInstanceOperation(), input)
}

func (service *CompatibilityRecoveryService) HeartbeatLiveInstance(ctx context.Context, tenantID string, input HeartbeatLiveInstanceInput) (LiveInstanceResult, error) {
	if !validLiveIdentity(input.Identity) || input.WriterEpoch < 1 || input.HeartbeatTTLSeconds < 1 || input.HeartbeatTTLSeconds > 300 || !validTransitionEvidence(input.Evidence) {
		return LiveInstanceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeLive(ctx, tenantID, compatibility.HeartbeatLiveInstanceOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.WriterEpoch, input.HeartbeatTTLSeconds,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) ReconcileLiveInstance(ctx context.Context, tenantID string, input ReconcileLiveInstanceInput) (LiveInstanceResult, error) {
	if !validLiveIdentity(input.Identity) || !validCompatibilityDigest(input.TransitionDigest) {
		return LiveInstanceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeLive(ctx, tenantID, compatibility.ReconcileLiveInstanceOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.TransitionDigest,
	})
}

func (service *CompatibilityRecoveryService) executeBackfill(ctx context.Context, tenantID string, operation compatibility.Operation, arguments []any) (BackfillResult, error) {
	columns := compatibilityBackfillMutationColumns
	result := BackfillResult{}
	if !operation.IsMutation() {
		columns = compatibilityBackfillReconcileColumns
	}
	claim, err := service.bindClaim(ctx, tenantID, operation, columns, arguments...)
	if err != nil {
		return BackfillResult{}, err
	}
	scan := func(row rowScanner) error {
		if operation.IsMutation() {
			return scanCompatibilityCommon(row, &result.CompatibilityResult, &result.LeaseExpiresAt)
		}
		return scanCompatibilityCommon(row, &result.CompatibilityResult, &result.LeaseOwner, &result.LeaseExpiresAt,
			&result.Phase, &result.Cursor, &result.Digest, &result.Count, &result.TransitionObserved)
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return BackfillResult{}, err
	}
	return result, nil
}

func (service *CompatibilityRecoveryService) StartBackfill(ctx context.Context, tenantID string, input StartBackfillInput) (BackfillResult, error) {
	if !validBackfillPayload(input.MigrationID, input.Phase, input.Cursor, input.Digest, 0) || input.WriterEpoch < 1 || !validTransitionEvidence(input.Evidence) {
		return BackfillResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeBackfill(ctx, tenantID, compatibility.StartBackfillOperation(), []any{
		tenantID, input.MigrationID, input.Phase, input.Cursor, input.Digest, input.WriterEpoch,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) AcquireBackfillLease(ctx context.Context, tenantID string, input AcquireBackfillLeaseInput) (BackfillResult, error) {
	if !compatibilityPhasePattern.MatchString(input.MigrationID) || !validMutationIdentifier(input.LeaseOwner) || input.ExpectedWriterEpoch < 1 ||
		input.NewWriterEpoch <= input.ExpectedWriterEpoch || input.LeaseSeconds < 1 || input.LeaseSeconds > 300 || !validTransitionEvidence(input.Evidence) {
		return BackfillResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeBackfill(ctx, tenantID, compatibility.AcquireBackfillLeaseOperation(), []any{
		tenantID, input.MigrationID, input.LeaseOwner, input.ExpectedWriterEpoch, input.NewWriterEpoch,
		input.LeaseSeconds, input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) backfillProgress(ctx context.Context, tenantID string, operation compatibility.Operation, input BackfillProgressInput) (BackfillResult, error) {
	if !validBackfillPayload(input.MigrationID, input.Phase, input.Cursor, input.Digest, input.Count) ||
		!validMutationIdentifier(input.LeaseOwner) || input.WriterEpoch < 1 || !validTransitionEvidence(input.Evidence) {
		return BackfillResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeBackfill(ctx, tenantID, operation, []any{
		tenantID, input.MigrationID, input.LeaseOwner, input.WriterEpoch, input.Phase,
		input.Cursor, input.Digest, input.Count, input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) AdvanceBackfill(ctx context.Context, tenantID string, input BackfillProgressInput) (BackfillResult, error) {
	return service.backfillProgress(ctx, tenantID, compatibility.AdvanceBackfillOperation(), input)
}

func (service *CompatibilityRecoveryService) CompleteBackfill(ctx context.Context, tenantID string, input BackfillProgressInput) (BackfillResult, error) {
	return service.backfillProgress(ctx, tenantID, compatibility.CompleteBackfillOperation(), input)
}

func (service *CompatibilityRecoveryService) HeartbeatBackfill(ctx context.Context, tenantID string, input HeartbeatBackfillInput) (BackfillResult, error) {
	if !compatibilityPhasePattern.MatchString(input.MigrationID) || !validMutationIdentifier(input.LeaseOwner) || input.WriterEpoch < 1 ||
		input.LeaseSeconds < 1 || input.LeaseSeconds > 300 || !validTransitionEvidence(input.Evidence) {
		return BackfillResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeBackfill(ctx, tenantID, compatibility.HeartbeatBackfillOperation(), []any{
		tenantID, input.MigrationID, input.LeaseOwner, input.WriterEpoch, input.LeaseSeconds,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) ReconcileBackfill(ctx context.Context, tenantID string, input ReconcileBackfillInput) (BackfillResult, error) {
	if !compatibilityPhasePattern.MatchString(input.MigrationID) || !validCompatibilityDigest(input.TransitionDigest) {
		return BackfillResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeBackfill(ctx, tenantID, compatibility.ReconcileBackfillOperation(), []any{tenantID, input.MigrationID, input.TransitionDigest})
}

func (service *CompatibilityRecoveryService) executeRestore(ctx context.Context, tenantID string, operation compatibility.Operation, arguments []any) (RestoreEvidenceResult, error) {
	columns := compatibilityRestoreMutationColumns
	result := RestoreEvidenceResult{}
	if !operation.IsMutation() {
		columns = compatibilityRestoreReconcileColumns
	}
	claim, err := service.bindClaim(ctx, tenantID, operation, columns, arguments...)
	if err != nil {
		return RestoreEvidenceResult{}, err
	}
	scan := func(row rowScanner) error {
		extras := []any{&result.EvidenceDigest, &result.TargetSchemaBundleDigest, &result.TargetPhase}
		if !operation.IsMutation() {
			extras = append(extras, &result.TransitionObserved)
		}
		return scanCompatibilityCommon(row, &result.CompatibilityResult, extras...)
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return RestoreEvidenceResult{}, err
	}
	return result, nil
}

func (service *CompatibilityRecoveryService) RecordRestoreEvidence(ctx context.Context, tenantID string, input RecordRestoreEvidenceInput) (RestoreEvidenceResult, error) {
	if !validMutationIdentifier(input.DrillID) || input.PostgresMajor < 15 || input.PostgresMajor > 17 ||
		!validCompatibilityDigest(input.LedgerChecksum) || !validCompatibilityDigest(input.TargetSchemaBundleDigest) ||
		!compatibilityPhasePattern.MatchString(input.TargetPhase) || !validCompatibilityDigest(input.RestorePointDigest) ||
		!validCompatibilityDigest(input.EvidenceDigest) || input.DrillAt.IsZero() || !validTransitionEvidence(input.Evidence) {
		return RestoreEvidenceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRestore(ctx, tenantID, compatibility.RecordRestoreEvidenceOperation(), []any{
		tenantID, input.DrillID, input.PostgresMajor, input.LedgerChecksum,
		input.TargetSchemaBundleDigest, input.TargetPhase, input.RestorePointDigest,
		input.EvidenceDigest, input.DrillAt, input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) restoreTransition(ctx context.Context, tenantID string, operation compatibility.Operation, input RestoreEvidenceTransitionInput, rejectionReason *string) (RestoreEvidenceResult, error) {
	if !validMutationIdentifier(input.DrillID) || input.ExpectedVersion < 1 || !validCompatibilityDigest(input.EvidenceDigest) || !validTransitionEvidence(input.Evidence) ||
		(rejectionReason != nil && !validMutationIdentifier(*rejectionReason)) {
		return RestoreEvidenceResult{}, ErrCompatibilityRecoveryInput
	}
	arguments := []any{tenantID, input.DrillID, input.ExpectedVersion, input.EvidenceDigest}
	if rejectionReason != nil {
		arguments = append(arguments, *rejectionReason)
	}
	arguments = append(arguments, input.Evidence.TransitionDigest, input.Evidence.RequestDigest)
	return service.executeRestore(ctx, tenantID, operation, arguments)
}

func (service *CompatibilityRecoveryService) CompleteRestoreEvidence(ctx context.Context, tenantID string, input RestoreEvidenceTransitionInput) (RestoreEvidenceResult, error) {
	return service.restoreTransition(ctx, tenantID, compatibility.CompleteRestoreEvidenceOperation(), input, nil)
}

func (service *CompatibilityRecoveryService) RejectRestoreEvidence(ctx context.Context, tenantID string, input RejectRestoreEvidenceInput) (RestoreEvidenceResult, error) {
	return service.restoreTransition(ctx, tenantID, compatibility.RejectRestoreEvidenceOperation(), input.RestoreEvidenceTransitionInput, &input.RejectionReason)
}

func (service *CompatibilityRecoveryService) ReconcileRestoreEvidence(ctx context.Context, tenantID string, input ReconcileRestoreEvidenceInput) (RestoreEvidenceResult, error) {
	if !validMutationIdentifier(input.DrillID) || !validCompatibilityDigest(input.TransitionDigest) {
		return RestoreEvidenceResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRestore(ctx, tenantID, compatibility.ReconcileRestoreEvidenceOperation(), []any{tenantID, input.DrillID, input.TransitionDigest})
}

func (service *CompatibilityRecoveryService) executeRetirement(ctx context.Context, tenantID string, operation compatibility.Operation, arguments []any) (RetirementReceiptResult, error) {
	columns := compatibilityRetirementMutationColumns
	result := RetirementReceiptResult{}
	if !operation.IsMutation() {
		columns = compatibilityRetirementReconcileColumns
	}
	claim, err := service.bindClaim(ctx, tenantID, operation, columns, arguments...)
	if err != nil {
		return RetirementReceiptResult{}, err
	}
	scan := func(row rowScanner) error {
		extras := []any{&result.CredentialRevoked, &result.EndpointRevoked, &result.ProcessTerminated,
			&result.LeaderReleased, &result.ClaimReleased, &result.GenerationFenced, &result.ReceiptDigest}
		if !operation.IsMutation() {
			extras = append(extras, &result.TransitionObserved)
		}
		return scanCompatibilityCommon(row, &result.CompatibilityResult, extras...)
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return RetirementReceiptResult{}, err
	}
	return result, nil
}

func (service *CompatibilityRecoveryService) CollectRetirementReceipt(ctx context.Context, tenantID string, input CollectRetirementReceiptInput) (RetirementReceiptResult, error) {
	if !validRetirementIdentity(input.Identity) || input.WriterEpoch < 1 || input.ExpectedVersion < 0 || !validTransitionEvidence(input.Evidence) {
		return RetirementReceiptResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRetirement(ctx, tenantID, compatibility.CollectRetirementReceiptOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.WriterEpoch, input.ExpectedVersion,
		input.CredentialRevoked, input.EndpointRevoked, input.ProcessTerminated,
		input.LeaderReleased, input.ClaimReleased, input.GenerationFenced,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) CompleteRetirementReceipt(ctx context.Context, tenantID string, input CompleteRetirementReceiptInput) (RetirementReceiptResult, error) {
	if !validRetirementIdentity(input.Identity) || input.WriterEpoch < 1 || input.ExpectedVersion < 1 ||
		!validCompatibilityDigest(input.ReceiptDigest) || !validTransitionEvidence(input.Evidence) {
		return RetirementReceiptResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRetirement(ctx, tenantID, compatibility.CompleteRetirementReceiptOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.WriterEpoch, input.ExpectedVersion, input.ReceiptDigest,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) RejectRetirementReceipt(ctx context.Context, tenantID string, input RejectRetirementReceiptInput) (RetirementReceiptResult, error) {
	if !validRetirementIdentity(input.Identity) || input.WriterEpoch < 1 || input.ExpectedVersion < 0 ||
		!validMutationIdentifier(input.RejectionReason) || !validTransitionEvidence(input.Evidence) {
		return RetirementReceiptResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRetirement(ctx, tenantID, compatibility.RejectRetirementReceiptOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.WriterEpoch, input.ExpectedVersion, input.RejectionReason,
		input.Evidence.TransitionDigest, input.Evidence.RequestDigest,
	})
}

func (service *CompatibilityRecoveryService) ReconcileRetirementReceipt(ctx context.Context, tenantID string, input ReconcileRetirementReceiptInput) (RetirementReceiptResult, error) {
	if !validRetirementIdentity(input.Identity) || !validCompatibilityDigest(input.TransitionDigest) {
		return RetirementReceiptResult{}, ErrCompatibilityRecoveryInput
	}
	return service.executeRetirement(ctx, tenantID, compatibility.ReconcileRetirementReceiptOperation(), []any{
		tenantID, input.Identity.ServiceKind, input.Identity.InstanceID, input.Identity.Incarnation,
		input.Identity.RolloutGeneration, input.TransitionDigest,
	})
}

func (service *CompatibilityRecoveryService) EvaluateMigrationPreflight(ctx context.Context, tenantID string, input EvaluateMigrationPreflightInput) (MigrationPreflightResult, error) {
	if input.PostgresMajor < 15 || input.PostgresMajor > 17 || !validCompatibilityDigest(input.LedgerChecksum) ||
		!validCompatibilityDigest(input.TargetSchemaBundleDigest) || !compatibilityPhasePattern.MatchString(input.TargetPhase) ||
		input.RolloutGeneration < 1 || input.WriterEpoch < 1 || !validCompatibilityDigest(input.RestoreEvidenceDigest) ||
		(input.RequiresIrreversibleContractApproval != (input.IrreversibleContractApprovalDigest != nil)) ||
		(input.IrreversibleContractApprovalDigest != nil && !validCompatibilityDigest(*input.IrreversibleContractApprovalDigest)) {
		return MigrationPreflightResult{}, ErrCompatibilityRecoveryInput
	}
	result := MigrationPreflightResult{}
	var approvalDigest any
	if input.IrreversibleContractApprovalDigest != nil {
		approvalDigest = *input.IrreversibleContractApprovalDigest
	}
	claim, err := service.bindClaim(ctx, tenantID, compatibility.EvaluateMigrationPreflightOperation(), compatibilityPreflightColumns,
		tenantID, input.PostgresMajor, input.LedgerChecksum, input.TargetSchemaBundleDigest,
		input.TargetPhase, input.RolloutGeneration, input.WriterEpoch, input.RestoreEvidenceDigest,
		input.RequiresIrreversibleContractApproval, approvalDigest,
	)
	if err != nil {
		return MigrationPreflightResult{}, err
	}
	scan := func(row rowScanner) error {
		if err := scanCompatibilityCommon(row, &result.CompatibilityResult, &result.Decision, &result.EvaluatedAt,
			&result.LedgerChecksum, &result.PostgresMajor, &result.RestoreEvidenceDigest,
			&result.RolloutGeneration, &result.TargetPhase, &result.TargetSchemaBundleDigest); err != nil {
			return err
		}
		if result.ResultCode != "observed" || result.Decision != result.State ||
			result.EvaluatedAt.IsZero() || result.LedgerChecksum != input.LedgerChecksum || result.PostgresMajor != input.PostgresMajor ||
			result.RestoreEvidenceDigest != input.RestoreEvidenceDigest || result.RolloutGeneration != input.RolloutGeneration ||
			result.TargetPhase != input.TargetPhase || result.TargetSchemaBundleDigest != input.TargetSchemaBundleDigest {
			return ErrCompatibilityRecoveryDrift
		}
		return nil
	}
	if err := service.executeClaim(ctx, claim, &result.CompatibilityResult, scan); err != nil {
		return MigrationPreflightResult{}, err
	}
	return result, nil
}

func validCompatibilityOperation(operation compatibility.Operation) bool {
	return operation.Valid() && compatibility.RegistryFormatVersion == "cloud-agents-compatibility-recovery-registry/v2" &&
		compatibility.RegistryID == "cloud-agents/platform/compatibility-recovery" &&
		compatibility.RegistryDigest == "sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973" &&
		compatibility.StateMachineDigest == "sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863" &&
		compatibility.PolicyDigest == "sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708" &&
		compatibility.SchemaHead == "000010" &&
		compatibility.SchemaCatalogDigest == "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236" &&
		compatibility.SchemaMigrationDigest == "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6"
}

func validCompatibilityResult(result CompatibilityResult, operation compatibility.Operation) bool {
	if result.DatabaseTimestamp.IsZero() || result.State == "" || result.Version < 0 || result.WriterEpoch < 0 || result.ReconcileRequired {
		return false
	}
	if result.StableErrorCode != nil && !validMutationIdentifier(*result.StableErrorCode) {
		return false
	}
	switch result.ResultCode {
	case "applied":
		return operation.IsMutation() && result.WriteApplied && result.Version >= 1 && result.StableErrorCode == nil
	case "observed":
		return !result.WriteApplied && result.StableErrorCode == nil
	case "not_observed":
		return !operation.IsMutation() && !result.WriteApplied && result.State == "absent" && result.Version == 0 && result.StableErrorCode == nil
	case "rejected":
		return operation.IsMutation() && !result.WriteApplied && result.StableErrorCode != nil
	case "conflict":
		return operation.IsMutation() && !result.WriteApplied && result.State == "unknown" && result.Version == 0 &&
			result.StableErrorCode != nil && *result.StableErrorCode == "transition_digest_conflict"
	default:
		return false
	}
}

func validObservedPointer(resultCode string, value *string) bool {
	return resultCode != "observed" && resultCode != "not_observed" || resultCode == "not_observed" && value == nil ||
		resultCode == "observed" && value != nil && validMutationIdentifier(*value)
}

func validTransitionEvidence(evidence TransitionEvidence) bool {
	return validCompatibilityDigest(evidence.TransitionDigest) && validCompatibilityDigest(evidence.RequestDigest)
}

func validCompatibilityDigest(value string) bool {
	return compatibilityDigestPattern.MatchString(value)
}

func validWorkloadIdentity(identity WorkloadPrincipalIdentity) bool {
	return validMutationIdentifier(identity.WorkloadID) && validMutationIdentifier(identity.Provider)
}

func validLiveIdentity(identity LiveInstanceIdentity) bool {
	return validMutationIdentifier(identity.ServiceKind) && validMutationIdentifier(identity.InstanceID) &&
		identity.Incarnation >= 1 && identity.RolloutGeneration >= 1
}

func validRetirementIdentity(identity RetirementReceiptIdentity) bool {
	return validMutationIdentifier(identity.ServiceKind) && validMutationIdentifier(identity.InstanceID) &&
		identity.Incarnation >= 1 && identity.RolloutGeneration >= 1
}

func validBackfillPayload(migrationID, phase, cursor, digest string, count int64) bool {
	return compatibilityPhasePattern.MatchString(migrationID) && validMutationIdentifier(phase) &&
		len(cursor) >= 1 && len(cursor) <= 2048 && validCompatibilityDigest(digest) && count >= 0
}

func mapCompatibilityRecoveryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrCompatibilityRecoveryClaim) {
		return ErrCompatibilityRecoveryClaim
	}
	if errors.Is(err, ErrCompatibilityRecoveryDrift) {
		return ErrCompatibilityRecoveryDrift
	}
	if errors.Is(err, ErrMutationConflict) || errors.Is(err, ErrCoordinationRejected) {
		return ErrCompatibilityRecoveryRejected
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "22023", "22003", "23502", "23503", "23514":
			return ErrCompatibilityRecoveryInput
		case "23505", "40001":
			return ErrCompatibilityRecoveryRejected
		case "42501":
			return ErrCompatibilityRecoveryAuthority
		default:
			return ErrCompatibilityRecoveryDatabase
		}
	}
	return ErrCompatibilityRecoveryDatabase
}
