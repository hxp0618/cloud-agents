package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dataRecoveryPhaseEnvironment  = "CLOUD_AGENTS_DATA_RECOVERY_PHASE"
	dataRecoveryTenantEnvironment = "CLOUD_AGENTS_DATA_RECOVERY_TENANT_ID"
	dataRecoveryRequired          = "CLOUD_AGENTS_REQUIRE_DATA_RECOVERY_TEST"
	dataRecoverySuccessKey        = "idempotency-recovery-success"
	dataRecoveryPendingKey        = "idempotency-recovery-pending"
	dataRecoveryEventID           = "event-recovery-success"
	dataRecoveryProjectID         = "project-recovery"
)

func TestDurableCoordinationPostgresRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("PostgreSQL logical recovery conformance is disabled in short mode")
	}
	databaseURL := os.Getenv("CLOUD_AGENTS_TEST_DATABASE_URL")
	verificationURL := os.Getenv("CLOUD_AGENTS_TEST_MIGRATION_DATABASE_URL")
	phase := os.Getenv(dataRecoveryPhaseEnvironment)
	tenantID := os.Getenv(dataRecoveryTenantEnvironment)
	if databaseURL == "" || verificationURL == "" || (phase != "prepare" && phase != "recover") || !validMutationIdentifier(tenantID) {
		if os.Getenv(dataRecoveryRequired) == "1" {
			t.Fatal("logical recovery test requires database URLs, prepare|recover phase, and a valid tenant")
		}
		t.Skip("local PostgreSQL logical recovery matrix is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	runtimePool := openCoordinationIntegrationPool(t, ctx, databaseURL, 4)
	verificationPool := openCoordinationIntegrationPool(t, ctx, verificationURL, 2)
	service, err := NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatalf("create durable coordination service: %v", err)
	}
	actor := authz.SubjectRef{Kind: "user", Issuer: "https://identity.example.test/", Subject: "user-admin"}
	scope := authz.ScopeRef{Level: authz.ScopeOrganization, ID: "organization-recovery"}
	profile := coordination.ManagedAgentCreateProject()

	switch phase {
	case "prepare":
		prepareDurableCoordinationRecovery(t, ctx, service, tenantID, profile, actor, scope)
	case "recover":
		recoverDurableCoordination(t, ctx, service, verificationPool, tenantID, profile, actor, scope)
	}
}

func prepareDurableCoordinationRecovery(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
) {
	t.Helper()
	successClaim := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope,
		dataRecoverySuccessKey, coordinationIntegrationRequest, "audit-recovery-success-claim")
	if successClaim.DatabaseOutcome != DatabaseCommitted || successClaim.Disposition != "created" || successClaim.ReplayState != "pending" {
		t.Fatalf("prepare success claim = %#v", successClaim)
	}
	success := completeCoordinationIntegrationSuccess(t, ctx, service, tenantID, profile, actor, scope,
		dataRecoverySuccessKey, coordinationIntegrationRequest, dataRecoveryProjectID, dataRecoveryEventID,
		"audit-recovery-success-complete")
	if success.DatabaseOutcome != DatabaseCommitted || success.OutboxState != "pending" {
		t.Fatalf("prepare success completion = %#v", success)
	}
	pending := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope,
		dataRecoveryPendingKey, coordinationIntegrationRequest, "audit-recovery-pending-claim")
	if pending.DatabaseOutcome != DatabaseCommitted || pending.Disposition != "created" || pending.ReplayState != "pending" {
		t.Fatalf("prepare pending claim = %#v", pending)
	}
	claimed, err := service.ClaimOutbox(ctx, tenantID, OutboxClaimInput{
		HolderID: "holder-before-restore", HolderIncarnation: "incarnation-before-restore",
		ClaimToken: "claim-before-restore", LeaseSeconds: 1,
		SubjectDigest: coordinationIntegrationRequest, AuditFactID: "audit-recovery-outbox-claim",
	})
	if err != nil || claimed.DatabaseOutcome != DatabaseCommitted || !claimed.Found || claimed.Claim.EventID != dataRecoveryEventID {
		t.Fatalf("prepare outbox claim = %#v / %v", claimed, err)
	}
	lease, err := service.AcquireLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "leader-before-restore",
		HolderIncarnation: "leader-incarnation-before-restore", LeaseSeconds: 1,
	})
	if err != nil || lease.DatabaseOutcome != DatabaseCommitted || lease.Disposition != "acquired" || lease.FencingToken != 1 {
		t.Fatalf("prepare leader lease = %#v / %v", lease, err)
	}
	time.Sleep(1100 * time.Millisecond)
}

func recoverDurableCoordination(
	t *testing.T,
	ctx context.Context,
	service *DurableCoordinationService,
	verification *pgxpool.Pool,
	tenantID string,
	profile coordination.Profile,
	actor authz.SubjectRef,
	scope authz.ScopeRef,
) {
	t.Helper()
	replayed := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope,
		dataRecoverySuccessKey, coordinationIntegrationRequest, "audit-recovery-success-replay")
	if replayed.DatabaseOutcome != DatabaseCommitted || replayed.Disposition != "replay" || replayed.ReplayState != "succeeded" ||
		replayed.ResourceID == nil || *replayed.ResourceID != dataRecoveryProjectID {
		t.Fatalf("restored terminal idempotency replay = %#v", replayed)
	}
	pending := claimCoordinationIntegration(t, ctx, service, tenantID, profile, actor, scope,
		dataRecoveryPendingKey, coordinationIntegrationRequest, "audit-recovery-pending-replay")
	if pending.DatabaseOutcome != DatabaseCommitted || pending.Disposition != "replay" || pending.ReplayState != "pending" {
		t.Fatalf("restored pending idempotency replay = %#v", pending)
	}

	lease, err := service.AcquireLeader(ctx, LeaderLeaseInput{
		LeaderName: "outbox-dispatcher", HolderID: "leader-after-restore",
		HolderIncarnation: "leader-incarnation-after-restore", LeaseSeconds: 60,
	})
	if err != nil || lease.DatabaseOutcome != DatabaseCommitted || lease.Disposition != "acquired" || lease.FencingToken != 2 {
		t.Fatalf("restored leader recovery = %#v / %v", lease, err)
	}
	reaped, err := service.ReapExpiredOutbox(ctx, tenantID, OutboxReapInput{
		LeaderHolderID: "leader-after-restore", LeaderHolderIncarnation: "leader-incarnation-after-restore",
		FencingToken: lease.FencingToken, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-recovery-outbox-reap",
	})
	if err != nil || reaped.DatabaseOutcome != DatabaseCommitted || !reaped.Found || reaped.EventID != dataRecoveryEventID || reaped.State != "pending" {
		t.Fatalf("restored outbox reap = %#v / %v", reaped, err)
	}
	claimed, err := service.ClaimOutbox(ctx, tenantID, OutboxClaimInput{
		HolderID: "holder-after-restore", HolderIncarnation: "incarnation-after-restore",
		ClaimToken: "claim-after-restore", LeaseSeconds: 60,
		SubjectDigest: coordinationIntegrationRequest, AuditFactID: "audit-recovery-outbox-reclaim",
	})
	if err != nil || claimed.DatabaseOutcome != DatabaseCommitted || !claimed.Found || claimed.Claim.EventID != dataRecoveryEventID {
		t.Fatalf("restored outbox reclaim = %#v / %v", claimed, err)
	}
	settled, err := service.AcknowledgeOutbox(ctx, tenantID, OutboxSettlementInput{
		Claim: claimed.Claim, SubjectDigest: coordinationIntegrationRequest,
		AuditFactID: "audit-recovery-outbox-acknowledge",
	})
	if err != nil || settled.DatabaseOutcome != DatabaseCommitted || settled.State != "delivered" {
		t.Fatalf("restored outbox settlement = %#v / %v", settled, err)
	}
	failed := completeCoordinationIntegrationFailure(
		t, ctx, service, tenantID, profile, actor, scope, dataRecoveryPendingKey,
		coordinationIntegrationRequest, "recovered.terminal", "audit-recovery-pending-complete",
	)
	if failed.DatabaseOutcome != DatabaseCommitted || failed.ReplayState != "failed" || failed.StableErrorCode != "recovered.terminal" {
		t.Fatalf("restored pending idempotency completion = %#v", failed)
	}

	var succeeded, failedCount, delivered, recoveredLeader int64
	verificationConnection, err := verification.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire restored verification connection: %v", err)
	}
	defer verificationConnection.Release()
	if _, err := verificationConnection.Exec(ctx, "SET ROLE cloud_agents_migration_owner"); err != nil {
		t.Fatalf("set restored verification role: %v", err)
	}
	defer func() {
		if _, resetErr := verificationConnection.Exec(context.Background(), "RESET ROLE"); resetErr != nil {
			t.Errorf("reset restored verification role: %v", resetErr)
		}
	}()
	err = verificationConnection.QueryRow(ctx, `SELECT
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records
     WHERE tenant_id=$1 AND idempotency_key=$2 AND state='succeeded' AND resource_id=$3),
    (SELECT pg_catalog.count(*) FROM cloud_agents.idempotency_records
     WHERE tenant_id=$1 AND idempotency_key=$4 AND state='failed' AND stable_error_code='recovered.terminal'),
    (SELECT pg_catalog.count(*) FROM cloud_agents.outbox_events
     WHERE tenant_id=$1 AND event_id=$5 AND state='delivered' AND delivery_attempts=2),
    (SELECT pg_catalog.count(*) FROM cloud_agents.leader_leases
     WHERE leader_name='outbox-dispatcher' AND holder_id='leader-after-restore' AND fencing_token=2)`,
		tenantID, dataRecoverySuccessKey, dataRecoveryProjectID, dataRecoveryPendingKey, dataRecoveryEventID,
	).Scan(&succeeded, &failedCount, &delivered, &recoveredLeader)
	if err != nil || succeeded != 1 || failedCount != 1 || delivered != 1 || recoveredLeader != 1 {
		t.Fatalf("restored durable facts = %d/%d/%d/%d err=%v", succeeded, failedCount, delivered, recoveredLeader, err)
	}
	empty, err := service.ClaimOutbox(ctx, tenantID, OutboxClaimInput{
		HolderID: "holder-empty", HolderIncarnation: "incarnation-empty", ClaimToken: "claim-empty",
		LeaseSeconds: 60, SubjectDigest: coordinationIntegrationRequest, AuditFactID: "audit-recovery-outbox-empty",
	})
	if err != nil || empty.DatabaseOutcome != DatabaseCommitted || empty.Found {
		t.Fatalf("empty restored outbox probe = %#v / %v", empty, err)
	}
}
