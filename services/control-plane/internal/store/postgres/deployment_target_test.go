package postgres

import (
	"errors"
	"testing"
	"time"

	internaldeploymenttarget "github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
)

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
