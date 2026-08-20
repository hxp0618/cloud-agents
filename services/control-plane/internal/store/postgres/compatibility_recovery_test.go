package postgres

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/compatibility"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCompatibilityRecoveryServiceMatchesEveryGeneratedOperation(t *testing.T) {
	t.Parallel()
	operations := []compatibility.Operation{
		compatibility.AcquireBackfillLeaseOperation(),
		compatibility.ActivateLiveInstanceOperation(),
		compatibility.AdvanceBackfillOperation(),
		compatibility.BeginLiveInstanceDrainOperation(),
		compatibility.CollectRetirementReceiptOperation(),
		compatibility.CompleteBackfillOperation(),
		compatibility.CompleteRestoreEvidenceOperation(),
		compatibility.CompleteRetirementReceiptOperation(),
		compatibility.EvaluateMigrationPreflightOperation(),
		compatibility.FenceLiveInstanceOperation(),
		compatibility.FinishLiveInstanceDrainOperation(),
		compatibility.HeartbeatBackfillOperation(),
		compatibility.HeartbeatLiveInstanceOperation(),
		compatibility.ReconcileBackfillOperation(),
		compatibility.ReconcileLiveInstanceOperation(),
		compatibility.ReconcileRestoreEvidenceOperation(),
		compatibility.ReconcileRetirementReceiptOperation(),
		compatibility.ReconcileWorkloadPrincipalOperation(),
		compatibility.RecordRestoreEvidenceOperation(),
		compatibility.RegisterLiveInstanceOperation(),
		compatibility.RegisterWorkloadPrincipalOperation(),
		compatibility.RejectRestoreEvidenceOperation(),
		compatibility.RejectRetirementReceiptOperation(),
		compatibility.RevokeWorkloadPrincipalOperation(),
		compatibility.RotateWorkloadPrincipalOperation(),
		compatibility.StartBackfillOperation(),
	}
	serviceType := reflect.TypeOf((*CompatibilityRecoveryService)(nil))
	if len(operations) != 26 || serviceType.NumMethod() != len(operations) {
		t.Fatalf("generated operations/service methods = %d/%d", len(operations), serviceType.NumMethod())
	}
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if !operation.Valid() {
			t.Fatalf("invalid generated operation %q", operation.ServiceMethod())
		}
		if _, exists := seen[operation.ServiceMethod()]; exists {
			t.Fatalf("duplicate generated service method %q", operation.ServiceMethod())
		}
		seen[operation.ServiceMethod()] = struct{}{}
		method, exists := serviceType.MethodByName(operation.ServiceMethod())
		if !exists || method.Type.NumIn() != 4 || method.Type.NumOut() != 2 {
			t.Fatalf("generated method %q has drifted: %#v", operation.ServiceMethod(), method)
		}
	}
}

const (
	compatibilityTenant = "tenant-alpha"
	compatibilityD1     = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	compatibilityD2     = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	compatibilityD3     = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	compatibilityD4     = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
)

func TestCompatibilityRecoveryRegisterUsesExactGeneratedOperationAndOneTransaction(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	transaction := compatibilityTransaction(rowValues(compatibilityCommonRow("applied", "active", 1, 1, timestamp, nil)...))
	service, connection := compatibilityService(t, transaction)
	result, err := service.RegisterWorkloadPrincipal(context.Background(), compatibilityTenant, RegisterWorkloadPrincipalInput{
		Identity:    WorkloadPrincipalIdentity{WorkloadID: "workload-alpha", Provider: "postgres"},
		PrincipalID: "principal-alpha", Epoch: 1,
		Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.ResultCode != "applied" ||
		!result.WriteApplied || result.State != "active" || result.Version != 1 || result.WriterEpoch != 1 {
		t.Fatalf("register result/error = %#v / %v", result, err)
	}
	if len(transaction.queries) != 3 {
		t.Fatalf("query count = %d, trace = %#v", len(transaction.queries), transaction.queries)
	}
	wantSQL := "SELECT " + compatibilityCommonColumns +
		" FROM cloud_agents.compatibility_recovery_workload_principal_register_v2($1, $2, $3, $4, $5, $6, $7)"
	if transaction.queries[2].sql != wantSQL {
		t.Fatalf("generated SQL = %q", transaction.queries[2].sql)
	}
	wantArguments := []any{compatibilityTenant, "workload-alpha", "postgres", "principal-alpha", int64(1), compatibilityD1, compatibilityD2}
	if !equalQueryArguments(transaction.queries[2].arguments, wantArguments) {
		t.Fatalf("generated arguments = %#v", transaction.queries[2].arguments)
	}
	if len(connection.beginOptions) != 1 || connection.beginOptions[0] != (pgx.TxOptions{
		IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite, DeferrableMode: pgx.NotDeferrable,
	}) {
		t.Fatalf("mutation transaction options = %#v", connection.beginOptions)
	}
	assertCompatibilityCommitted(t, transaction, connection)
}

func TestCompatibilityRecoveryMutationHasClosedObservedRejectedAndUnknownOutcomes(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		row         rowScanner
		commitErr   error
		wantOutcome DatabaseOutcome
		wantCode    string
	}{
		{name: "idempotent observation", row: rowValues(compatibilityCommonRow("observed", "active", 1, 1, timestamp, nil)...), wantOutcome: DatabaseCommitted, wantCode: "observed"},
		{name: "stale fence", row: rowValues(compatibilityCommonRow("rejected", "active", 1, 1, timestamp, stringPtr("principal_fence_stale"))...), wantOutcome: DatabaseRejected, wantCode: "rejected"},
		{name: "commit response lost", row: rowValues(compatibilityCommonRow("applied", "active", 1, 1, timestamp, nil)...), commitErr: errors.New("commit response lost"), wantOutcome: DatabaseUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transaction := compatibilityTransaction(test.row)
			transaction.commitErr = test.commitErr
			service, connection := compatibilityService(t, transaction)
			result, err := service.RegisterWorkloadPrincipal(context.Background(), compatibilityTenant, RegisterWorkloadPrincipalInput{
				Identity:    WorkloadPrincipalIdentity{WorkloadID: "workload-alpha", Provider: "postgres"},
				PrincipalID: "principal-alpha", Epoch: 1,
				Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
			})
			if err != nil || result.DatabaseOutcome != test.wantOutcome || result.ResultCode != test.wantCode {
				t.Fatalf("closed result/error = %#v / %v", result, err)
			}
			if len(transaction.queries) != 3 {
				t.Fatalf("unknown/rejected path retried query: %#v", transaction.queries)
			}
			if test.wantOutcome == DatabaseUnknown {
				if !result.ReconcileRequired || result != (WorkloadPrincipalResult{CompatibilityResult: CompatibilityResult{
					DatabaseOutcome: DatabaseUnknown, ReconcileRequired: true,
				}}) || connection.hijackCalls != 1 {
					t.Fatalf("unknown result/connection = %#v / %d", result, connection.hijackCalls)
				}
			}
		})
	}
}

func TestCompatibilityRecoveryResultDriftRollsBackBeforeMutationCommit(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	transaction := compatibilityTransaction(rowValues(compatibilityCommonRow(
		"observed", "active", 1, 1, timestamp, stringPtr("unexpected_stable_error"),
	)...))
	service, connection := compatibilityService(t, transaction)
	_, err := service.RegisterWorkloadPrincipal(context.Background(), compatibilityTenant, RegisterWorkloadPrincipalInput{
		Identity:    WorkloadPrincipalIdentity{WorkloadID: "workload-alpha", Provider: "postgres"},
		PrincipalID: "principal-alpha", Epoch: 1,
		Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
	})
	if !errors.Is(err, ErrCompatibilityRecoveryDrift) || transaction.commitCalls != 0 || transaction.rollbackCalls != 1 {
		t.Fatalf("drift result/commit/rollback = %v/%d/%d", err, transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func TestCompatibilityRecoveryReconcileIsReadOnlyAndBindsTransition(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	principalID := "principal-alpha"
	transaction := compatibilityTransaction(rowValues(append(
		compatibilityCommonRow("observed", "active", 3, 7, timestamp, nil), &principalID, true,
	)...))
	service, connection := compatibilityService(t, transaction)
	result, err := service.ReconcileWorkloadPrincipal(context.Background(), compatibilityTenant, ReconcileWorkloadPrincipalInput{
		Identity:         WorkloadPrincipalIdentity{WorkloadID: "workload-alpha", Provider: "postgres"},
		TransitionDigest: compatibilityD1,
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.PrincipalID == nil ||
		*result.PrincipalID != principalID || !result.TransitionObserved {
		t.Fatalf("reconcile result/error = %#v / %v", result, err)
	}
	if len(transaction.queries) != 3 || !strings.Contains(transaction.queries[2].sql,
		"cloud_agents.compatibility_recovery_workload_principal_reconcile_v2($1, $2, $3, $4)") {
		t.Fatalf("reconcile query trace = %#v", transaction.queries)
	}
	if len(connection.beginOptions) != 1 || connection.beginOptions[0] != (pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly, DeferrableMode: pgx.NotDeferrable,
	}) {
		t.Fatalf("reconcile transaction options = %#v", connection.beginOptions)
	}
}

func TestCompatibilityRecoveryDomainScannersRemainTypedAndRedacted(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	t.Run("live stale epoch", func(t *testing.T) {
		transaction := compatibilityTransaction(rowValues(append(
			compatibilityCommonRow("rejected", "registered", 1, 1, timestamp, stringPtr("live_instance_fence_stale")), (*time.Time)(nil),
		)...))
		service, _ := compatibilityService(t, transaction)
		result, err := service.ActivateLiveInstance(context.Background(), compatibilityTenant, LiveInstanceEpochTransitionInput{
			Identity:            LiveInstanceIdentity{ServiceKind: "control-plane", InstanceID: "instance-alpha", Incarnation: 1, RolloutGeneration: 1},
			ExpectedWriterEpoch: 1, NewWriterEpoch: 2,
			Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
		})
		if err != nil || result.DatabaseOutcome != DatabaseRejected || result.StableErrorCode == nil ||
			*result.StableErrorCode != "live_instance_fence_stale" {
			t.Fatalf("live result/error = %#v / %v", result, err)
		}
	})

	t.Run("backfill lease", func(t *testing.T) {
		expires := timestamp.Add(time.Minute)
		transaction := compatibilityTransaction(rowValues(append(
			compatibilityCommonRow("applied", "leased", 2, 2, timestamp, nil), &expires,
		)...))
		service, _ := compatibilityService(t, transaction)
		result, err := service.AcquireBackfillLease(context.Background(), compatibilityTenant, AcquireBackfillLeaseInput{
			MigrationID: "000011", LeaseOwner: "migration-alpha", ExpectedWriterEpoch: 1,
			NewWriterEpoch: 2, LeaseSeconds: 60,
			Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
		})
		if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.LeaseExpiresAt == nil || !result.LeaseExpiresAt.Equal(expires) {
			t.Fatalf("backfill result/error = %#v / %v", result, err)
		}
	})

	t.Run("restore evidence", func(t *testing.T) {
		transaction := compatibilityTransaction(rowValues(append(
			compatibilityCommonRow("applied", "recorded", 1, 0, timestamp, nil), stringPtr(compatibilityD3), stringPtr(compatibilityD4), stringPtr("000011"),
		)...))
		service, _ := compatibilityService(t, transaction)
		result, err := service.RecordRestoreEvidence(context.Background(), compatibilityTenant, RecordRestoreEvidenceInput{
			DrillID: "drill-alpha", PostgresMajor: 16, LedgerChecksum: compatibilityD1,
			TargetSchemaBundleDigest: compatibilityD4, TargetPhase: "000011", RestorePointDigest: compatibilityD2,
			EvidenceDigest: compatibilityD3, DrillAt: timestamp.Add(-time.Minute),
			Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
		})
		if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.EvidenceDigest == nil || *result.EvidenceDigest != compatibilityD3 {
			t.Fatalf("restore result/error = %#v / %v", result, err)
		}
	})

	t.Run("retirement", func(t *testing.T) {
		transaction := compatibilityTransaction(rowValues(append(
			compatibilityCommonRow("applied", "collecting", 1, 3, timestamp, nil), true, true, true, false, false, true, (*string)(nil),
		)...))
		service, _ := compatibilityService(t, transaction)
		result, err := service.CollectRetirementReceipt(context.Background(), compatibilityTenant, CollectRetirementReceiptInput{
			Identity:    RetirementReceiptIdentity{ServiceKind: "control-plane", InstanceID: "instance-alpha", Incarnation: 1, RolloutGeneration: 1},
			WriterEpoch: 3, ExpectedVersion: 0, CredentialRevoked: true, EndpointRevoked: true,
			ProcessTerminated: true, GenerationFenced: true,
			Evidence: TransitionEvidence{TransitionDigest: compatibilityD1, RequestDigest: compatibilityD2},
		})
		if err != nil || result.DatabaseOutcome != DatabaseCommitted || !result.CredentialRevoked || result.LeaderReleased {
			t.Fatalf("retirement result/error = %#v / %v", result, err)
		}
	})
}

func TestCompatibilityRecoveryPreflightIsReadOnlyAndExact(t *testing.T) {
	timestamp := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	approvalDigest := compatibilityD2
	transaction := compatibilityTransaction(rowValues(append(
		compatibilityCommonRow("observed", "approved", 0, 2, timestamp, nil),
		"approved", timestamp, compatibilityD1, int32(16), compatibilityD3, int64(7), "000011", compatibilityD4,
	)...))
	service, connection := compatibilityService(t, transaction)
	result, err := service.EvaluateMigrationPreflight(context.Background(), compatibilityTenant, EvaluateMigrationPreflightInput{
		PostgresMajor: 16, LedgerChecksum: compatibilityD1, TargetSchemaBundleDigest: compatibilityD4,
		TargetPhase: "000011", RolloutGeneration: 7, WriterEpoch: 2, RestoreEvidenceDigest: compatibilityD3,
		RequiresIrreversibleContractApproval: true, IrreversibleContractApprovalDigest: &approvalDigest,
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || result.Decision != "approved" || result.WriterEpoch != 2 {
		t.Fatalf("preflight result/error = %#v / %v", result, err)
	}
	if connection.beginOptions[0].AccessMode != pgx.ReadOnly || !strings.Contains(transaction.queries[2].sql,
		"cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2") {
		t.Fatalf("preflight boundary = %#v / %#v", connection.beginOptions, transaction.queries)
	}
	if got := transaction.queries[2].arguments[9]; got != compatibilityD2 {
		t.Fatalf("preflight approval claim retained caller pointer: %#v", got)
	}
}

func TestCompatibilityRecoveryRejectsInvalidInputBeforeDatabaseAndClaimCopies(t *testing.T) {
	connection := newFakeConnection()
	service, err := newCompatibilityRecoveryService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.RegisterLiveInstance(context.Background(), compatibilityTenant, RegisterLiveInstanceInput{
		Identity:    LiveInstanceIdentity{ServiceKind: "control-plane", InstanceID: "instance-alpha", Incarnation: 1, RolloutGeneration: 1},
		WriterEpoch: 1, BinaryVersion: "v1", SupportedSchemaMin: "000010", SupportedSchemaMax: "000011", HeartbeatTTLSeconds: 300,
		Evidence: TransitionEvidence{TransitionDigest: "not-a-digest", RequestDigest: compatibilityD2},
	})
	if !errors.Is(err, ErrCompatibilityRecoveryInput) || len(connection.beginOptions) != 0 {
		t.Fatalf("invalid input/database = %v / %#v", err, connection.beginOptions)
	}

	claim, err := service.bindClaim(context.Background(), compatibilityTenant, compatibility.RegisterWorkloadPrincipalOperation(), compatibilityCommonColumns,
		compatibilityTenant, "workload-alpha", "postgres", "principal-alpha", int64(1), compatibilityD1, compatibilityD2,
	)
	if err != nil {
		t.Fatal(err)
	}
	copyClaim := *claim
	if _, _, err := copyClaim.consume(); !errors.Is(err, ErrCompatibilityRecoveryClaim) {
		t.Fatalf("copied claim error = %v", err)
	}
	if _, _, err := claim.consume(); err != nil {
		t.Fatalf("original claim consume = %v", err)
	}
	if _, _, err := claim.consume(); !errors.Is(err, ErrCompatibilityRecoveryClaim) {
		t.Fatalf("duplicate claim consume = %v", err)
	}
}

func TestCompatibilityRecoveryDatabaseErrorsAreStableAndRedacted(t *testing.T) {
	tests := []struct {
		err  error
		want error
	}{
		{err: &pgconn.PgError{Code: "22023", Message: "secret-input"}, want: ErrCompatibilityRecoveryInput},
		{err: &pgconn.PgError{Code: "42501", Message: "secret-role"}, want: ErrCompatibilityRecoveryAuthority},
		{err: &pgconn.PgError{Code: "40001", Message: "secret-transition"}, want: ErrCompatibilityRecoveryRejected},
		{err: &pgconn.PgError{Code: "XX000", Message: "secret-database"}, want: ErrCompatibilityRecoveryDatabase},
	}
	for _, test := range tests {
		got := mapCompatibilityRecoveryError(test.err)
		if got != test.want || strings.Contains(got.Error(), "secret") {
			t.Fatalf("mapped error = %v, want %v", got, test.want)
		}
	}
}

func compatibilityCommonRow(code, state string, version, writerEpoch int64, timestamp time.Time, stableError *string) []any {
	return []any{code, code == "applied", false, state, version, writerEpoch, timestamp, stableError}
}

func compatibilityTransaction(operation rowScanner) *fakeTransaction {
	return &fakeTransaction{rows: []rowScanner{rowValues(compatibilityTenant), rowValues(compatibilityTenant), operation}}
}

func compatibilityService(t *testing.T, transaction *fakeTransaction) (*CompatibilityRecoveryService, *fakeConnection) {
	t.Helper()
	connection := newFakeConnection(transaction)
	connection.outsideRows = []rowScanner{rowValues((*string)(nil))}
	service, err := newCompatibilityRecoveryService(newTenantTransactionRunner(&fakePool{connection: connection}, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return service, connection
}

func assertCompatibilityCommitted(t *testing.T, transaction *fakeTransaction, connection *fakeConnection) {
	t.Helper()
	if transaction.commitCalls != 1 || transaction.rollbackCalls != 0 {
		t.Fatalf("compatibility commit/rollback = %d/%d", transaction.commitCalls, transaction.rollbackCalls)
	}
	assertConnectionDisposition(t, connection, 1, 0)
}

func equalQueryArguments(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
