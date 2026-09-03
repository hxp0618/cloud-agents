package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
)

func TestDecodeDeploymentTargetPageRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.UTC)
	row := func(targetID string) deploymentTargetPageRow {
		return deploymentTargetPageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: targetID, TargetName: targetID,
			Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha",
			Generation: 1, ObservedPhase: "unprobed", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
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
	operationRow := func(id string) deploymentTargetOperationPageRow {
		return deploymentTargetOperationPageRow{TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "target-alpha", OperationID: id,
			IdempotencyKey: id + "-key-123456789", Action: "target.probe", RequestID: "request-alpha", RequestedBy: digest,
			TargetGeneration: 1, State: "succeeded", CurrentStep: "probe-complete", ImpactSummary: "Probed deployment target target-alpha", RequestedAt: now, UpdatedAt: now}
	}
	raw, err := json.Marshal([]deploymentTargetOperationPageRow{operationRow("operation-a"), operationRow("operation-b")})
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
		int64(1), "ready", "1.54", "29.4.0", "linux", "arm64", "unexpected-error", &now, int64(2), now, now)
	var snapshot internaldeploymenttarget.Snapshot
	err := scanDeploymentTarget(row, internaldeploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot)
	if !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("projection error = %v", err)
	}
}
