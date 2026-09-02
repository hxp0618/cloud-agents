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
