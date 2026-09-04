package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDecodeDeploymentTargetPageRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	row := func(targetID string) deploymentTargetPageRow {
		return deploymentTargetPageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: targetID, TargetName: targetID,
			Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha",
			Generation: 1, SchedulingState: "active", ObservedPhase: "unprobed", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	raw, err := json.Marshal([]deploymentTargetPageRow{row("target-alpha"), row("target-beta")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeDeploymentTargetPageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.DeploymentTargets) != 1 || page.DeploymentTargets[0].TargetID != "target-alpha" || page.NextTargetID != "target-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeDeploymentTargetPageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error = %v", err)
	}
}

func TestDeploymentTargetListSQLBindsTenantProjectAndCursor(t *testing.T) {
	if !strings.Contains(listDeploymentTargetsSQL, "cloud_agents.require_tenant_id()") ||
		!strings.Contains(listDeploymentTargetsSQL, "project_uid = $1") ||
		!strings.Contains(deploymentTargetPageCursorIdentitySQL, "target_uid = $2") {
		t.Fatal("deployment target list does not bind tenant, project, and cursor identity")
	}
}

func TestDecodeDeploymentTargetActivityRowsBindsTargetAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	operationRow := func(id, targetID string) deploymentTargetOperationPageRow {
		return deploymentTargetOperationPageRow{TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: targetID, OperationID: id,
			IdempotencyKey: id + "-key-123456789", Action: "target.probe", RequestID: "request-alpha", RequestedBy: digest,
			TargetGeneration: 1, State: "succeeded", CurrentStep: "probe-complete", ImpactSummary: "Probed deployment target target-alpha", RequestedAt: now, UpdatedAt: now}
	}
	raw, err := json.Marshal([]deploymentTargetOperationPageRow{operationRow("operation-a", "target-alpha"), operationRow("operation-b", "target-alpha")})
	if err != nil {
		t.Fatal(err)
	}
	operations, err := decodeDeploymentTargetOperationRows(raw, "tenant-alpha", "project-alpha", "target-alpha", 1)
	if err != nil || len(operations.Operations) != 1 || operations.NextRequestedAt == nil || operations.NextOperationID != "operation-a" {
		t.Fatalf("operation page = %#v / %v", operations, err)
	}
	if _, err := decodeDeploymentTargetOperationRows(raw, "tenant-alpha", "project-alpha", "target-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-target operation page error = %v", err)
	}
	projectRaw, err := json.Marshal([]deploymentTargetOperationPageRow{operationRow("operation-a", "target-alpha"), operationRow("operation-b", "target-beta")})
	if err != nil {
		t.Fatal(err)
	}
	projectOperations, err := decodeDeploymentTargetOperationRows(projectRaw, "tenant-alpha", "project-alpha", "", 1)
	if err != nil || len(projectOperations.Operations) != 1 || projectOperations.NextTargetID != "target-alpha" || projectOperations.NextOperationID != "operation-a" {
		t.Fatalf("project operation page = %#v / %v", projectOperations, err)
	}
	if !strings.Contains(listMaintenanceOperationsSQL, "DISTINCT ON (target_uid, operation_uid)") || !strings.Contains(listMaintenanceOperationsSQL, "(requested_at, target_uid, operation_uid) < ($2, $3, $4)") || !strings.Contains(maintenanceOperationCursorIdentitySQL, "target_uid = $2") {
		t.Fatal("maintenance operation list does not bind latest state and project cursor identity")
	}
	auditRaw, err := json.Marshal([]deploymentTargetAuditPageRow{{TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "target-alpha", EventID: "event-a", OperationID: "operation-a", Actor: digest, Action: "target.probe", TargetGeneration: 1, State: "succeeded", RequestID: "request-alpha", OccurredAt: now}})
	if err != nil {
		t.Fatal(err)
	}
	audit, err := decodeDeploymentTargetAuditRows(auditRaw, "tenant-alpha", "project-alpha", "target-alpha", 1)
	if err != nil || len(audit.Events) != 1 || audit.Events[0].Result != "succeeded" {
		t.Fatalf("audit page = %#v / %v", audit, err)
	}
}

func TestDeploymentTargetProjectionRejectsPhaseFactDrift(t *testing.T) {
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	row := rowValues("target-alpha", "docker-alpha", "docker", "https://docker.example.test:2376", "docker-alpha",
		int64(1), "active", "ready", "1.54", "29.4.0", "linux", "arm64", "unexpected-error", &now, int64(2), now, now)
	var snapshot internaldeploymenttarget.Snapshot
	err := scanDeploymentTarget(row, internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot)
	if !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("projection error = %v", err)
	}
}

func TestDeploymentTargetSchedulingProjectionAndConflicts(t *testing.T) {
	raw, err := json.Marshal([]deploymentTargetSchedulingLeaseRow{{LeaseID: "lease-alpha", LeaseName: "lease-alpha", Generation: 2, ObservedPhase: "ready"}})
	if err != nil {
		t.Fatal(err)
	}
	leases, err := scanDeploymentTargetSchedulingLeases(rowValues(raw))
	if err != nil || len(leases) != 1 || leases[0].LeaseID != "lease-alpha" {
		t.Fatalf("leases = %#v / %v", leases, err)
	}
	for message, expected := range map[string]error{
		"deployment target scheduling idempotency conflict": ErrDeploymentTargetSchedulingIdempotencyConflict,
		"deployment target generation conflict":             ErrDeploymentTargetSchedulingGenerationConflict,
		"deployment target resource version conflict":       ErrDeploymentTargetSchedulingResourceVersionConflict,
		"deployment target scheduling state conflict":       ErrDeploymentTargetSchedulingStateConflict,
		"deployment target scheduling impact conflict":      ErrDeploymentTargetSchedulingImpactConflict,
	} {
		if err := mapDeploymentTargetSchedulingError(&pgconn.PgError{Code: "23505", Message: message}); !errors.Is(err, expected) {
			t.Fatalf("%s mapped to %v", message, err)
		}
	}
	if !strings.Contains(transitionDeploymentTargetSchedulingSQL, "transition_deployment_target_scheduling_v1") ||
		!strings.Contains(lockDeploymentTargetSchedulingSQL, "lock_deployment_target_scheduling_v1") ||
		!strings.Contains(deploymentTargetSchedulingLeasesSQL, "deployment_target_uid = $2") {
		t.Fatal("scheduling store SQL is not bound to the migration authority and Lease lock")
	}
}

func TestDeploymentTargetCleanupOperationProjectionAndConflicts(t *testing.T) {
	now := time.Date(2026, time.September, 3, 9, 0, 0, 0, time.UTC)
	row := rowValues("operation-alpha", "cleanup-key-1234~", "target.cleanup", "target-alpha",
		int64(2), "sha256:"+strings.Repeat("a", 64), "request-alpha", now, now,
		"succeeded", "complete", "", "Cleaned 1 orphan worker and 2 resources", false, true)
	var result internaldeploymenttarget.CleanupStart
	if err := scanDeploymentTargetCleanupStart(row, internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &result); err != nil || !result.Execute || result.Operation.Action != "target.cleanup" {
		t.Fatalf("cleanup start = %#v / %v", result, err)
	}
	for message, expected := range map[string]error{
		"deployment target cleanup idempotency conflict": ErrDeploymentTargetCleanupIdempotencyConflict,
		"deployment target cleanup is already running":   ErrDeploymentTargetCleanupBusy,
		"deployment target generation conflict":          ErrDeploymentTargetCleanupGenerationConflict,
		"deployment target resource version conflict":    ErrDeploymentTargetCleanupResourceVersionConflict,
		"deployment target is not ready":                 ErrDeploymentTargetCleanupNotReady,
	} {
		if err := mapDeploymentTargetCleanupError(&pgconn.PgError{Code: "23505", Message: message}); !errors.Is(err, expected) {
			t.Fatalf("%s mapped to %v", message, err)
		}
	}
	if !strings.Contains(beginDeploymentTargetCleanupSQL, "begin_deployment_target_cleanup_v1") ||
		!strings.Contains(completeDeploymentTargetCleanupSQL, "complete_deployment_target_cleanup_v1") {
		t.Fatal("cleanup store SQL is not bound to the migration authority")
	}
}
