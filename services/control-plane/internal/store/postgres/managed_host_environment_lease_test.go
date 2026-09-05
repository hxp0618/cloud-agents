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

func TestDecodeAdminWorkerPageRowsProjectsLeaseBackedWorkers(t *testing.T) {
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	row := func(leaseID string) adminWorkerPageRow {
		return adminWorkerPageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", LeaseID: leaseID, LeaseName: leaseID,
			TargetID: "docker-alpha", TargetKind: "docker", TargetGeneration: 2, Generation: 3,
			DesiredPhase: "active", ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ObservedPhase: "ready", CleanupPhase: "none", EnvironmentID: leaseID,
			ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 536870912,
			WorkerEndpoint: "https://worker-alpha.test", WorkerSPIFFEID: "spiffe://cloud-agents.test/worker/" + leaseID,
			WorkerServerName: "worker-alpha.test", ExpiresAt: now.Add(time.Hour), ResourceVersion: 4,
			CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
		}
	}
	failed := row("lease-beta")
	healthy := row("lease-alpha")
	healthy.Health = &AdminWorkerHealth{State: "online", CheckedAt: now, ExpiresAt: now.Add(time.Minute), LastSuccessAt: &now}
	for _, health := range []*AdminWorkerHealth{
		healthy.Health,
		{State: "unavailable", CheckedAt: now, ExpiresAt: now.Add(time.Second)},
		{State: "expired", CheckedAt: now, ExpiresAt: now.Add(time.Minute)},
	} {
		healthy.Health = health
		raw, _ := json.Marshal([]adminWorkerPageRow{healthy})
		page, err := decodeAdminWorkerPageRows(raw, "tenant-alpha", "project-alpha", 1)
		if err != nil || page.Workers[0].Health == nil || page.Workers[0].Health.State != health.State {
			t.Fatalf("health projection: %v", err)
		}
	}
	for _, health := range []*AdminWorkerHealth{
		{State: "online", CheckedAt: now, ExpiresAt: now.Add(time.Minute)},
		{State: "ready", CheckedAt: now, ExpiresAt: now.Add(time.Minute)},
		{State: "unavailable", CheckedAt: now, ExpiresAt: now},
		{State: "expired", CheckedAt: now, ExpiresAt: now.Add(61 * time.Second)},
	} {
		healthy.Health = health
		raw, _ := json.Marshal([]adminWorkerPageRow{healthy})
		if _, err := decodeAdminWorkerPageRows(raw, "tenant-alpha", "project-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
			t.Fatalf("accepted health drift: %v", err)
		}
	}
	failed.ObservedPhase = "failed"
	failed.WorkerEndpoint, failed.WorkerSPIFFEID, failed.WorkerServerName = "", "", ""
	failed.StableErrorCode = "docker-worker-unavailable"
	raw, err := json.Marshal([]adminWorkerPageRow{row("lease-alpha"), failed})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeAdminWorkerPageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.Workers) != 1 || page.Workers[0].WorkerID != "lease-alpha" || page.Workers[0].State != "ready" || page.Workers[0].LastHealthAt == nil || page.NextWorkerID != "lease-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeAdminWorkerPageRows(raw, "tenant-alpha", "project-other", 2); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project worker page error = %v", err)
	}
	cleaned := row("lease-cleaned")
	cleaned.DesiredPhase, cleaned.ObservedPhase, cleaned.CleanupPhase = "terminated", "terminated", "complete"
	cleaned.WorkerEndpoint, cleaned.WorkerSPIFFEID, cleaned.WorkerServerName = "", "", ""
	raw, err = json.Marshal([]adminWorkerPageRow{cleaned})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeAdminWorkerPageRows(raw, "tenant-alpha", "project-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cleaned worker projection error = %v", err)
	}
}

func TestAdminWorkerListSQLBindsAuthorityAndExcludesCleanedLeases(t *testing.T) {
	for _, fragment := range []string{
		"cloud_agents.require_tenant_id()", "lease.project_uid = $1", "lease.lease_uid > $2",
		"target.target_uid = lease.deployment_target_uid", "NOT (lease.observed_phase = 'terminated' AND lease.cleanup_phase = 'complete')",
	} {
		if !strings.Contains(listAdminWorkersSQL, fragment) {
			t.Fatalf("worker authority SQL missing %q", fragment)
		}
	}
	if !strings.Contains(adminWorkerPageCursorIdentitySQL, "cloud_agents.require_tenant_id()") ||
		!strings.Contains(adminWorkerPageCursorIdentitySQL, "lease.project_uid = $1") ||
		!strings.Contains(adminWorkerPageCursorIdentitySQL, "lease.lease_uid = $2") {
		t.Fatal("worker cursor is not bound to its lease-backed identity")
	}
}

func TestTerminateManagedHostEnvironmentLeaseProjectsTransitionState(t *testing.T) {
	if !strings.Contains(terminateManagedHostEnvironmentLeaseSQL, "begin_managed_host_environment_lease_termination_v1") {
		t.Fatal("termination does not begin the fenced cleanup transition")
	}
	if !strings.Contains(completeManagedHostEnvironmentLeaseTerminationSQL, "complete_managed_host_environment_lease_termination_v1") {
		t.Fatal("termination does not complete cleanup separately")
	}
}
