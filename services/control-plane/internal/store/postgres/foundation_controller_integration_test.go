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

	volumeUsageClaim, err := service.ClaimFoundationWorkspaceVolumeUsage(ctx, 200, 60)
	if err != nil || volumeUsageClaim.DatabaseOutcome != DatabaseCommitted || !volumeUsageClaim.Found ||
		volumeUsageClaim.Claim.VolumeID != "workspace" || volumeUsageClaim.Claim.PhysicalVolumeID != "ca-ws-integration" ||
		volumeUsageClaim.Claim.MeasurementGeneration != 1 {
		t.Fatalf("workspace usage claim = %#v / %v", volumeUsageClaim, err)
	}
	usedBytes := int64(12345)
	volumeUsage, err := service.SettleFoundationWorkspaceVolumeUsage(ctx, FoundationWorkspaceVolumeUsageSettlement{
		Claim: volumeUsageClaim.Claim, Transition: "ready", UsedBytes: &usedBytes,
	})
	if err != nil || volumeUsage.DatabaseOutcome != DatabaseCommitted || volumeUsage.State != "ready" ||
		volumeUsage.UsedBytes == nil || *volumeUsage.UsedBytes != usedBytes || volumeUsage.CheckpointedAt == nil {
		t.Fatalf("workspace usage settlement = %#v / %v", volumeUsage, err)
	}
	notDue, err := service.ClaimFoundationWorkspaceVolumeUsage(ctx, 200, 60)
	if err != nil || notDue.DatabaseOutcome != DatabaseCommitted || notDue.Found {
		t.Fatalf("workspace usage immediate replay = %#v / %v", notDue, err)
	}
	command, err := owner.Exec(ctx, `UPDATE cloud_agents.workspace_volume_usage_checkpoints
		SET checkpointed_at=checkpointed_at-interval '2 minutes', observed_at=observed_at-interval '2 minutes'
		WHERE tenant_id='tenant' AND project_uid='project' AND volume_uid='workspace'`)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("stale workspace usage fixture = %d / %v", command.RowsAffected(), err)
	}
	volumeUsageClaim, err = service.ClaimFoundationWorkspaceVolumeUsage(ctx, 200, 60)
	if err != nil || volumeUsageClaim.DatabaseOutcome != DatabaseCommitted || !volumeUsageClaim.Found ||
		volumeUsageClaim.Claim.MeasurementGeneration != 2 {
		t.Fatalf("workspace usage reclaim = %#v / %v", volumeUsageClaim, err)
	}
	volumeUsage, err = service.SettleFoundationWorkspaceVolumeUsage(ctx, FoundationWorkspaceVolumeUsageSettlement{
		Claim: volumeUsageClaim.Claim, Transition: "failed", StableErrorCode: "workspace_volume_usage_unavailable",
	})
	if err != nil || volumeUsage.State != "failed" || volumeUsage.UsedBytes == nil || *volumeUsage.UsedBytes != usedBytes ||
		volumeUsage.CheckpointedAt == nil || volumeUsage.StableErrorCode == nil || *volumeUsage.StableErrorCode != "workspace_volume_usage_unavailable" {
		t.Fatalf("workspace usage failed settlement = %#v / %v", volumeUsage, err)
	}

	command, err = owner.Exec(ctx, `UPDATE cloud_agents.outbox_events SET delivery_attempts=7
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

	command, err = owner.Exec(ctx, `DELETE FROM cloud_agents.sandbox_usage_checkpoints
		WHERE tenant_id='tenant' AND sandbox_uid='sandbox' AND runtime_uid='runtime-ready'`)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("legacy usage fixture = %d / %v", command.RowsAffected(), err)
	}
	checkpoint, err := service.CheckpointFoundationSandboxUsage(ctx, 10)
	if err != nil || checkpoint.DatabaseOutcome != DatabaseCommitted || checkpoint.Count != 0 {
		t.Fatalf("legacy usage backfill = %#v / %v", checkpoint, err)
	}
	command, err = owner.Exec(ctx, `UPDATE cloud_agents.sandbox_usage_checkpoints
		SET started_at=checkpointed_at-interval '2 minutes', checkpointed_at=checkpointed_at-interval '2 minutes'
		WHERE tenant_id='tenant' AND sandbox_uid='sandbox' AND runtime_uid='runtime-ready' AND finalized_at IS NULL`)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("stale usage checkpoint fixture = %d / %v", command.RowsAffected(), err)
	}
	checkpoint, err = service.CheckpointFoundationSandboxUsage(ctx, 10)
	if err != nil || checkpoint.DatabaseOutcome != DatabaseCommitted || checkpoint.Count != 1 {
		t.Fatalf("usage checkpoint = %#v / %v", checkpoint, err)
	}
	var allocated, cpuAllocated, memoryAllocated int64
	if err := owner.QueryRow(ctx, `SELECT allocated_milliseconds::bigint,
		cpu_millis_milliseconds::bigint, memory_byte_milliseconds::bigint
		FROM cloud_agents.sandbox_usage_checkpoints
		WHERE tenant_id='tenant' AND sandbox_uid='sandbox' AND runtime_uid='runtime-ready'`).Scan(
		&allocated, &cpuAllocated, &memoryAllocated,
	); err != nil || allocated < 120000 || cpuAllocated != allocated*500 || memoryAllocated != allocated*536870912 {
		t.Fatalf("usage totals = %d/%d/%d err=%v", allocated, cpuAllocated, memoryAllocated, err)
	}
	command, err = owner.Exec(ctx, `UPDATE cloud_agents.sandbox_sessions SET
		desired_state='stopped', observed_state='stopped', observed_generation=generation, writer_released=true,
		runtime_uid=NULL, runtime_state='', runtime_operation_uid=NULL, runtime_generation=NULL,
		runtime_spec_digest=NULL, stable_error_code=NULL, observed_at=transaction_timestamp()
		WHERE tenant_id='tenant' AND sandbox_uid='sandbox'`)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("usage finalization fixture = %d / %v", command.RowsAffected(), err)
	}
	var finalizedAt *time.Time
	if err := owner.QueryRow(ctx, `SELECT allocated_milliseconds::bigint,
		cpu_millis_milliseconds::bigint, memory_byte_milliseconds::bigint, finalized_at
		FROM cloud_agents.sandbox_usage_checkpoints
		WHERE tenant_id='tenant' AND sandbox_uid='sandbox' AND runtime_uid='runtime-ready'`).Scan(
		&allocated, &cpuAllocated, &memoryAllocated, &finalizedAt,
	); err != nil || finalizedAt == nil || cpuAllocated != allocated*500 || memoryAllocated != allocated*536870912 {
		t.Fatalf("finalized usage totals = %d/%d/%d/%v err=%v", allocated, cpuAllocated, memoryAllocated, finalizedAt, err)
	}
	checkpoint, err = service.CheckpointFoundationSandboxUsage(ctx, 10)
	if err != nil || checkpoint.DatabaseOutcome != DatabaseCommitted || checkpoint.Count != 0 {
		t.Fatalf("finalized usage changed = %#v / %v", checkpoint, err)
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
