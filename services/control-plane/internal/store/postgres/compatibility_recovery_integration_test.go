package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompatibilityRecoveryPostgresConformance(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL compatibility/recovery conformance is disabled in short mode")
	}
	bootstrapURL := os.Getenv("CLOUD_AGENTS_COMPATIBILITY_BOOTSTRAP_DATABASE_URL")
	runtimeURL := os.Getenv("CLOUD_AGENTS_COMPATIBILITY_RUNTIME_DATABASE_URL")
	migrationURL := os.Getenv("CLOUD_AGENTS_COMPATIBILITY_MIGRATION_DATABASE_URL")
	if bootstrapURL == "" || runtimeURL == "" || migrationURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_COMPATIBILITY_POSTGRES_TEST") == "1" {
			t.Fatal("all three compatibility/recovery database URLs are required")
		}
		t.Skip("compatibility/recovery PostgreSQL URLs are not configured")
	}
	runID := os.Getenv("CLOUD_AGENTS_COMPATIBILITY_RUN_ID")
	tenantID := os.Getenv("CLOUD_AGENTS_COMPATIBILITY_TENANT_ID")
	postgresMajor := int32(0)
	if _, err := fmt.Sscanf(os.Getenv("CLOUD_AGENTS_COMPATIBILITY_POSTGRES_MAJOR"), "%d", &postgresMajor); err != nil ||
		(runID != "normal" && runID != "race") || !validMutationIdentifier(tenantID) || postgresMajor < 15 || postgresMajor > 17 {
		t.Fatal("compatibility/recovery test requires an isolated run, tenant, and PostgreSQL 15/16/17 major")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	bootstrapPool := openCompatibilityIntegrationPool(t, ctx, bootstrapURL, "")
	runtimePool := openCompatibilityIntegrationPool(t, ctx, runtimeURL, "")
	migrationPool := openCompatibilityIntegrationPool(t, ctx, migrationURL, "cloud_agents_migration_owner")
	bootstrapService := mustCompatibilityService(t, bootstrapPool)
	runtimeService := mustCompatibilityService(t, runtimePool)
	migrationService := mustCompatibilityService(t, migrationPool)

	workload := WorkloadPrincipalIdentity{WorkloadID: "workload-" + runID, Provider: "postgres"}
	registerPrincipal := RegisterWorkloadPrincipalInput{
		Identity: workload, PrincipalID: "principal-" + runID, Epoch: 1,
		Evidence: integrationEvidence(1),
	}
	registered, err := bootstrapService.RegisterWorkloadPrincipal(ctx, tenantID, registerPrincipal)
	assertIntegrationOutcome(t, registered.CompatibilityResult, err, DatabaseCommitted, "applied")
	replayed, err := bootstrapService.RegisterWorkloadPrincipal(ctx, tenantID, registerPrincipal)
	assertIntegrationOutcome(t, replayed.CompatibilityResult, err, DatabaseCommitted, "observed")
	rotated, err := bootstrapService.RotateWorkloadPrincipal(ctx, tenantID, RotateWorkloadPrincipalInput{
		Identity: workload, ExpectedPrincipalID: "principal-" + runID, NewPrincipalID: "principal-rotated-" + runID,
		ExpectedEpoch: 1, NewEpoch: 2, Evidence: integrationEvidence(2),
	})
	assertIntegrationOutcome(t, rotated.CompatibilityResult, err, DatabaseCommitted, "applied")

	if _, err := runtimeService.RegisterWorkloadPrincipal(ctx, tenantID, registerPrincipal); !errors.Is(err, ErrCompatibilityRecoveryAuthority) {
		t.Fatalf("runtime cross-profile workload call = %v", err)
	}

	liveIdentity := LiveInstanceIdentity{
		ServiceKind: "control-plane", InstanceID: "instance-" + runID, Incarnation: 1, RolloutGeneration: 1,
	}
	liveRegistered, err := runtimeService.RegisterLiveInstance(ctx, tenantID, RegisterLiveInstanceInput{
		Identity: liveIdentity, WriterEpoch: 1, BinaryVersion: "v1.0.0-" + runID,
		SupportedSchemaMin: "000010", SupportedSchemaMax: "000011", HeartbeatTTLSeconds: 300,
		Evidence: integrationEvidence(3),
	})
	assertIntegrationOutcome(t, liveRegistered.CompatibilityResult, err, DatabaseCommitted, "applied")
	liveActivated, err := runtimeService.ActivateLiveInstance(ctx, tenantID, LiveInstanceEpochTransitionInput{
		Identity: liveIdentity, ExpectedWriterEpoch: 1, NewWriterEpoch: 2, Evidence: integrationEvidence(4),
	})
	assertIntegrationOutcome(t, liveActivated.CompatibilityResult, err, DatabaseCommitted, "applied")
	staleLive, err := runtimeService.ActivateLiveInstance(ctx, tenantID, LiveInstanceEpochTransitionInput{
		Identity: liveIdentity, ExpectedWriterEpoch: 1, NewWriterEpoch: 2, Evidence: integrationEvidence(5),
	})
	assertIntegrationOutcome(t, staleLive.CompatibilityResult, err, DatabaseRejected, "rejected")
	heartbeat, err := runtimeService.HeartbeatLiveInstance(ctx, tenantID, HeartbeatLiveInstanceInput{
		Identity: liveIdentity, WriterEpoch: 2, HeartbeatTTLSeconds: 300, Evidence: integrationEvidence(6),
	})
	assertIntegrationOutcome(t, heartbeat.CompatibilityResult, err, DatabaseCommitted, "applied")

	restoreDigest := integrationDigest(31)
	targetBundleDigest := integrationDigest(32)
	ledgerChecksum := integrationDigest(33)
	recorded, err := migrationService.RecordRestoreEvidence(ctx, tenantID, RecordRestoreEvidenceInput{
		DrillID: "drill-" + runID, PostgresMajor: postgresMajor, LedgerChecksum: ledgerChecksum,
		TargetSchemaBundleDigest: targetBundleDigest, TargetPhase: "000011",
		RestorePointDigest: integrationDigest(34), EvidenceDigest: restoreDigest,
		DrillAt: time.Now().UTC().Add(-time.Minute), Evidence: integrationEvidence(7),
	})
	assertIntegrationOutcome(t, recorded.CompatibilityResult, err, DatabaseCommitted, "applied")
	completedRestore, err := migrationService.CompleteRestoreEvidence(ctx, tenantID, RestoreEvidenceTransitionInput{
		DrillID: "drill-" + runID, ExpectedVersion: 1, EvidenceDigest: restoreDigest, Evidence: integrationEvidence(8),
	})
	assertIntegrationOutcome(t, completedRestore.CompatibilityResult, err, DatabaseCommitted, "applied")
	reconciledRestore, err := migrationService.ReconcileRestoreEvidence(ctx, tenantID, ReconcileRestoreEvidenceInput{
		DrillID: "drill-" + runID, TransitionDigest: integrationEvidence(8).TransitionDigest,
	})
	assertIntegrationOutcome(t, reconciledRestore.CompatibilityResult, err, DatabaseCommitted, "observed")
	if !reconciledRestore.TransitionObserved {
		t.Fatal("restore transition was not observed")
	}

	preflight, err := runtimeService.EvaluateMigrationPreflight(ctx, tenantID, EvaluateMigrationPreflightInput{
		PostgresMajor: postgresMajor, LedgerChecksum: ledgerChecksum, TargetSchemaBundleDigest: targetBundleDigest,
		TargetPhase: "000011", RolloutGeneration: 1, WriterEpoch: 2, RestoreEvidenceDigest: restoreDigest,
	})
	if err != nil || preflight.DatabaseOutcome != DatabaseCommitted || preflight.Decision != "approved" {
		t.Fatalf("preflight result/error = %#v / %v", preflight, err)
	}

	backfillStarted, err := migrationService.StartBackfill(ctx, tenantID, StartBackfillInput{
		MigrationID: "000011", Phase: "expand", Cursor: "cursor-0", Digest: integrationDigest(35),
		WriterEpoch: 1, Evidence: integrationEvidence(9),
	})
	assertIntegrationOutcome(t, backfillStarted.CompatibilityResult, err, DatabaseCommitted, "applied")
	backfillLeased, err := migrationService.AcquireBackfillLease(ctx, tenantID, AcquireBackfillLeaseInput{
		MigrationID: "000011", LeaseOwner: "migration-" + runID, ExpectedWriterEpoch: 1,
		NewWriterEpoch: 2, LeaseSeconds: 60, Evidence: integrationEvidence(10),
	})
	assertIntegrationOutcome(t, backfillLeased.CompatibilityResult, err, DatabaseCommitted, "applied")
	advanced, err := migrationService.AdvanceBackfill(ctx, tenantID, BackfillProgressInput{
		MigrationID: "000011", LeaseOwner: "migration-" + runID, WriterEpoch: 2,
		Phase: "backfill", Cursor: "cursor-1", Digest: integrationDigest(36), Count: 1,
		Evidence: integrationEvidence(11),
	})
	assertIntegrationOutcome(t, advanced.CompatibilityResult, err, DatabaseCommitted, "applied")
	backfillHeartbeat, err := migrationService.HeartbeatBackfill(ctx, tenantID, HeartbeatBackfillInput{
		MigrationID: "000011", LeaseOwner: "migration-" + runID, WriterEpoch: 2,
		LeaseSeconds: 60, Evidence: integrationEvidence(12),
	})
	assertIntegrationOutcome(t, backfillHeartbeat.CompatibilityResult, err, DatabaseCommitted, "applied")
	backfillComplete, err := migrationService.CompleteBackfill(ctx, tenantID, BackfillProgressInput{
		MigrationID: "000011", LeaseOwner: "migration-" + runID, WriterEpoch: 2,
		Phase: "contract", Cursor: "cursor-2", Digest: integrationDigest(37), Count: 1,
		Evidence: integrationEvidence(13),
	})
	assertIntegrationOutcome(t, backfillComplete.CompatibilityResult, err, DatabaseCommitted, "applied")
	backfillReconcile, err := migrationService.ReconcileBackfill(ctx, tenantID, ReconcileBackfillInput{
		MigrationID: "000011", TransitionDigest: integrationEvidence(13).TransitionDigest,
	})
	assertIntegrationOutcome(t, backfillReconcile.CompatibilityResult, err, DatabaseCommitted, "observed")
	if !backfillReconcile.TransitionObserved || backfillReconcile.Digest == nil || *backfillReconcile.Digest != integrationDigest(37) {
		t.Fatalf("backfill reconcile = %#v", backfillReconcile)
	}

	for _, transition := range []struct {
		name      string
		epoch     int64
		nextEpoch int64
		evidence  int
		call      func(context.Context, string, LiveInstanceEpochTransitionInput) (LiveInstanceResult, error)
	}{
		{name: "begin drain", epoch: 2, nextEpoch: 3, evidence: 14, call: runtimeService.BeginLiveInstanceDrain},
		{name: "finish drain", epoch: 3, nextEpoch: 4, evidence: 15, call: runtimeService.FinishLiveInstanceDrain},
		{name: "fence", epoch: 4, nextEpoch: 5, evidence: 16, call: runtimeService.FenceLiveInstance},
	} {
		result, transitionErr := transition.call(ctx, tenantID, LiveInstanceEpochTransitionInput{
			Identity: liveIdentity, ExpectedWriterEpoch: transition.epoch, NewWriterEpoch: transition.nextEpoch,
			Evidence: integrationEvidence(transition.evidence),
		})
		assertIntegrationOutcome(t, result.CompatibilityResult, transitionErr, DatabaseCommitted, "applied")
	}

	retirementIdentity := RetirementReceiptIdentity(liveIdentity)
	collected, err := runtimeService.CollectRetirementReceipt(ctx, tenantID, CollectRetirementReceiptInput{
		Identity: retirementIdentity, WriterEpoch: 5, ExpectedVersion: 0,
		CredentialRevoked: true, EndpointRevoked: true, ProcessTerminated: true,
		LeaderReleased: true, ClaimReleased: true, GenerationFenced: true,
		Evidence: integrationEvidence(17),
	})
	assertIntegrationOutcome(t, collected.CompatibilityResult, err, DatabaseCommitted, "applied")
	receiptDigest := integrationDigest(38)
	completedReceipt, err := runtimeService.CompleteRetirementReceipt(ctx, tenantID, CompleteRetirementReceiptInput{
		Identity: retirementIdentity, WriterEpoch: 5, ExpectedVersion: 1, ReceiptDigest: receiptDigest,
		Evidence: integrationEvidence(18),
	})
	assertIntegrationOutcome(t, completedReceipt.CompatibilityResult, err, DatabaseCommitted, "applied")
	reconciledReceipt, err := runtimeService.ReconcileRetirementReceipt(ctx, tenantID, ReconcileRetirementReceiptInput{
		Identity: retirementIdentity, TransitionDigest: integrationEvidence(18).TransitionDigest,
	})
	assertIntegrationOutcome(t, reconciledReceipt.CompatibilityResult, err, DatabaseCommitted, "observed")
	if !reconciledReceipt.TransitionObserved || reconciledReceipt.ReceiptDigest == nil || *reconciledReceipt.ReceiptDigest != receiptDigest {
		t.Fatalf("retirement reconcile = %#v", reconciledReceipt)
	}

	revoked, err := bootstrapService.RevokeWorkloadPrincipal(ctx, tenantID, RevokeWorkloadPrincipalInput{
		Identity: workload, ExpectedPrincipalID: "principal-rotated-" + runID, ExpectedEpoch: 2,
		Evidence: integrationEvidence(19),
	})
	assertIntegrationOutcome(t, revoked.CompatibilityResult, err, DatabaseCommitted, "applied")
	reconciledPrincipal, err := bootstrapService.ReconcileWorkloadPrincipal(ctx, tenantID, ReconcileWorkloadPrincipalInput{
		Identity: workload, TransitionDigest: integrationEvidence(19).TransitionDigest,
	})
	assertIntegrationOutcome(t, reconciledPrincipal.CompatibilityResult, err, DatabaseCommitted, "observed")
	if !reconciledPrincipal.TransitionObserved || reconciledPrincipal.PrincipalID == nil || *reconciledPrincipal.PrincipalID != "principal-rotated-"+runID {
		t.Fatalf("principal reconcile = %#v", reconciledPrincipal)
	}
}

func integrationEvidence(index int) TransitionEvidence {
	return TransitionEvidence{TransitionDigest: integrationDigest(index), RequestDigest: integrationDigest(index + 100)}
}

func integrationDigest(index int) string {
	return fmt.Sprintf("sha256:%064x", index)
}

func openCompatibilityIntegrationPool(t *testing.T, ctx context.Context, databaseURL, setRole string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse compatibility/recovery database configuration: %v", err)
	}
	config.MinConns = 1
	config.MaxConns = 8
	config.MaxConnIdleTime = 30 * time.Second
	config.MaxConnLifetime = time.Minute
	if setRole != "" {
		if setRole != "cloud_agents_migration_owner" {
			t.Fatalf("unsupported compatibility/recovery test role %q", setRole)
		}
		config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
			_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
			return err
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open compatibility/recovery database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func mustCompatibilityService(t *testing.T, pool *pgxpool.Pool) *CompatibilityRecoveryService {
	t.Helper()
	service, err := NewCompatibilityRecoveryService(pool)
	if err != nil {
		t.Fatalf("create compatibility/recovery service: %v", err)
	}
	return service
}

func assertIntegrationOutcome(t *testing.T, result CompatibilityResult, err error, outcome DatabaseOutcome, code string) {
	t.Helper()
	if err != nil || result.DatabaseOutcome != outcome || result.ResultCode != code {
		t.Fatalf("compatibility/recovery outcome = %#v / %v, want %q/%q", result, err, outcome, code)
	}
}
