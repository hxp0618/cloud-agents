package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFoundationControllerPostgres(t *testing.T) {
	runtimeURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_CONTROLLER_DATABASE_URL")
	ownerURL := os.Getenv("CLOUD_AGENTS_FOUNDATION_CONTROLLER_OWNER_DATABASE_URL")
	if runtimeURL == "" || ownerURL == "" {
		t.Skip("isolated foundation Controller PostgreSQL environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runtimePool := openCoordinationIntegrationPool(t, ctx, runtimeURL, 2)
	ownerConfig, err := pgxpool.ParseConfig(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	ownerConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, "SET ROLE cloud_agents_migration_owner")
		return err
	}
	owner, err := pgxpool.NewWithConfig(ctx, ownerConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	service, err := NewDurableCoordinationService(runtimePool)
	if err != nil {
		t.Fatal(err)
	}
	subject := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	legacy, err := service.ClaimOutbox(ctx, "tenant", OutboxClaimInput{
		HolderID: "legacy", HolderIncarnation: "legacy-incarnation", ClaimToken: "legacy-claim",
		LeaseSeconds: 60, SubjectDigest: subject, AuditFactID: "audit-legacy-isolation",
	})
	if err != nil || legacy.DatabaseOutcome != DatabaseCommitted || legacy.Found {
		t.Fatalf("legacy dispatcher isolation = %#v / %v", legacy, err)
	}

	claim := claimFoundationControllerTest(t, ctx, service, subject, "first", 60)
	if claim.SandboxID != "sandbox" {
		t.Fatalf("first sandbox = %q", claim.SandboxID)
	}
	priorExpiry := claim.ClaimExpiresAt
	claim.ClaimExpiresAt, err = service.RenewFoundationSandbox(ctx, claim, 60)
	if err != nil || !claim.ClaimExpiresAt.After(priorExpiry) {
		t.Fatalf("renewed expiry = %s / %v", claim.ClaimExpiresAt, err)
	}
	retried, err := service.SettleFoundationSandbox(ctx, FoundationSandboxSettlement{
		Claim: claim, Transition: "retry", RuntimeID: "runtime-pending", RuntimeState: "Pending",
		VolumeName: "ca-ws-integration", StableErrorCode: "foundation_runtime_unavailable",
		SubjectDigest: subject, AuditFactID: "audit-foundation-retry",
	})
	if err != nil || retried.DatabaseOutcome != DatabaseCommitted || retried.OutboxState != "retry_wait" || retried.OperationState != "reconciling" {
		t.Fatalf("retry settlement = %#v / %v", retried, err)
	}
	time.Sleep(1100 * time.Millisecond)
	claim = claimFoundationControllerTest(t, ctx, service, subject, "expired", 1)
	time.Sleep(1100 * time.Millisecond)
	reaped, err := service.ReapFoundationSandbox(ctx, subject, "audit-foundation-reap")
	if err != nil || reaped.DatabaseOutcome != DatabaseCommitted || !reaped.Found ||
		reaped.EventID != claim.EventID || reaped.OutboxState != "pending" || reaped.DeliveryAttempts != 2 {
		t.Fatalf("retryable reap = %#v / %v", reaped, err)
	}
	claim = claimFoundationControllerTest(t, ctx, service, subject, "success", 60)
	succeeded, err := service.SettleFoundationSandbox(ctx, FoundationSandboxSettlement{
		Claim: claim, Transition: "succeeded", RuntimeID: "runtime-ready", RuntimeState: "Running",
		VolumeName: "ca-ws-integration", SubjectDigest: subject, AuditFactID: "audit-foundation-success",
	})
	if err != nil || succeeded.DatabaseOutcome != DatabaseCommitted || succeeded.OutboxState != "delivered" ||
		succeeded.OperationState != "succeeded" || succeeded.ResourceVersion == nil {
		t.Fatalf("success settlement = %#v / %v", succeeded, err)
	}

	command, err := owner.Exec(ctx, `UPDATE cloud_agents.outbox_events SET delivery_attempts=7
		WHERE tenant_id='tenant' AND aggregate_id='sandbox-terminal' AND state='pending'`)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("terminal attempt fixture = %d / %v", command.RowsAffected(), err)
	}
	terminalClaim := claimFoundationControllerTest(t, ctx, service, subject, "terminal", 1)
	if terminalClaim.SandboxID != "sandbox-terminal" || terminalClaim.DeliveryAttempts != 8 {
		t.Fatalf("terminal claim = %#v", terminalClaim)
	}
	time.Sleep(1100 * time.Millisecond)
	terminalReap, err := service.ReapFoundationSandbox(ctx, subject, "audit-foundation-terminal-reap")
	if err != nil || terminalReap.DatabaseOutcome != DatabaseCommitted || !terminalReap.Found ||
		terminalReap.EventID != terminalClaim.EventID || terminalReap.OutboxState != "dead_letter" || terminalReap.DeliveryAttempts != 8 {
		t.Fatalf("terminal reap = %#v / %v", terminalReap, err)
	}

	var successFacts, terminalFacts, attempts, audits int
	err = owner.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM cloud_agents.sandbox_sessions s
		 JOIN cloud_agents.workspace_volumes v USING (tenant_id,project_uid,workspace_uid)
		 JOIN cloud_agents.platform_operations o ON o.tenant_id=s.tenant_id AND o.operation_id=s.operation_id AND o.operation_generation=s.operation_generation
		 JOIN cloud_agents.outbox_events e ON e.tenant_id=s.tenant_id AND e.operation_id=s.operation_id AND e.operation_generation=s.operation_generation
		 WHERE s.sandbox_uid='sandbox' AND s.observed_state='running' AND s.runtime_uid='runtime-ready'
		   AND v.observed_state='available' AND v.physical_volume_uid='ca-ws-integration'
		   AND o.state='succeeded' AND o.cleanup_phase='complete' AND e.state='delivered' AND e.delivery_attempts=3),
		(SELECT count(*) FROM cloud_agents.sandbox_sessions s
		 JOIN cloud_agents.platform_operations o ON o.tenant_id=s.tenant_id AND o.operation_id=s.operation_id AND o.operation_generation=s.operation_generation
		 JOIN cloud_agents.outbox_events e ON e.tenant_id=s.tenant_id AND e.operation_id=s.operation_id AND e.operation_generation=s.operation_generation
		 JOIN cloud_agents.operation_finalizers f ON f.tenant_id=s.tenant_id AND f.operation_id=s.operation_id AND f.operation_generation=s.operation_generation
		 JOIN cloud_agents.idempotency_records i ON i.tenant_id=s.tenant_id AND i.operation_id=s.operation_id AND i.operation_generation=s.operation_generation
		 JOIN cloud_agents.terminal_receipts r ON r.tenant_id=s.tenant_id AND r.operation_id=s.operation_id AND r.operation_generation=s.operation_generation
		 WHERE s.sandbox_uid='sandbox-terminal' AND s.observed_state='failed' AND s.stable_error_code='foundation_claim_expired'
		   AND o.state='failed' AND o.cleanup_phase='blocked' AND e.state='dead_letter'
		   AND f.state='dead_letter' AND i.state='failed' AND r.outcome='failed'),
		(SELECT count(*) FROM cloud_agents.operation_attempts WHERE tenant_id='tenant'
		   AND state IN ('unknown','succeeded','failed')),
		(SELECT count(*) FROM cloud_agents.coordination_audit_facts WHERE tenant_id='tenant'
		   AND transition IN ('sandbox.claim_expired_retryable','sandbox.claim_expired_terminal'))`).Scan(
		&successFacts, &terminalFacts, &attempts, &audits)
	if err != nil || successFacts != 1 || terminalFacts != 1 || attempts != 4 || audits != 2 {
		t.Fatalf("durable controller facts = %d/%d/%d/%d err=%v", successFacts, terminalFacts, attempts, audits, err)
	}
}

func claimFoundationControllerTest(t *testing.T, ctx context.Context, service *DurableCoordinationService, subject, suffix string, lease int32) FoundationSandboxClaim {
	t.Helper()
	result, err := service.ClaimFoundationSandbox(ctx, FoundationSandboxClaimInput{
		TargetKind: "docker",
		HolderID:   "controller", HolderIncarnation: "controller-incarnation", ClaimToken: "claim-" + suffix,
		LeaseSeconds: lease, SubjectDigest: subject, AuditFactID: "audit-foundation-claim-" + suffix,
	})
	if err != nil || result.DatabaseOutcome != DatabaseCommitted || !result.Found {
		t.Fatalf("claim %s = %#v / %v", suffix, result, err)
	}
	return result.Claim
}
