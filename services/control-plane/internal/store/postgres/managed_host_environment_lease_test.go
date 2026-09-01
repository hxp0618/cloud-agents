package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeManagedHostEnvironmentLeasePageRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.August, 31, 8, 0, 0, 0, time.UTC)
	row := func(leaseID string) managedHostEnvironmentLeasePageRow {
		return managedHostEnvironmentLeasePageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", LeaseID: leaseID, LeaseName: leaseID,
			ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Generation:    1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none",
			EnvironmentID: leaseID, ExpiresAt: now.Add(time.Hour), ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	raw, err := json.Marshal([]managedHostEnvironmentLeasePageRow{row("lease-alpha"), row("lease-beta")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeManagedHostEnvironmentLeasePageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.EnvironmentLeases) != 1 || page.EnvironmentLeases[0].LeaseID != "lease-alpha" || page.NextLeaseID != "lease-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeManagedHostEnvironmentLeasePageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error = %v", err)
	}
	partial := row("lease-alpha")
	targetID := "docker-alpha"
	partial.TargetID = &targetID
	raw, err = json.Marshal([]managedHostEnvironmentLeasePageRow{partial})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeManagedHostEnvironmentLeasePageRows(raw, "tenant-alpha", "project-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("partial target binding error = %v", err)
	}
	partial = row("lease-alpha")
	providerCredentialRef := "provider-alpha"
	partial.ProviderCredentialRef = &providerCredentialRef
	raw, err = json.Marshal([]managedHostEnvironmentLeasePageRow{partial})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeManagedHostEnvironmentLeasePageRows(raw, "tenant-alpha", "project-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("partial deployment input error = %v", err)
	}
}

func TestManagedHostEnvironmentLeaseListSQLBindsTenantProjectAndCursor(t *testing.T) {
	if !strings.Contains(listManagedHostEnvironmentLeasesSQL, "cloud_agents.require_tenant_id()") ||
		!strings.Contains(listManagedHostEnvironmentLeasesSQL, "project_uid = $1") ||
		!strings.Contains(managedHostEnvironmentLeasePageCursorIdentitySQL, "lease_uid = $2") {
		t.Fatal("environment lease list does not bind tenant, project, and cursor identity")
	}
}

func TestTerminateManagedHostEnvironmentLeaseProjectsTransitionState(t *testing.T) {
	for _, field := range []string{"generation", "desired_phase", "observed_phase", "cleanup_phase", "resource_version", "updated_at"} {
		if !strings.Contains(terminateManagedHostEnvironmentLeaseSQL, "transition."+field) {
			t.Fatalf("termination projection does not use transition.%s", field)
		}
	}
	for _, field := range []string{"deployment_target_uid", "deployment_target_generation"} {
		if !strings.Contains(terminateManagedHostEnvironmentLeaseSQL, "lease."+field) {
			t.Fatalf("termination projection does not retain lease.%s", field)
		}
	}
}
