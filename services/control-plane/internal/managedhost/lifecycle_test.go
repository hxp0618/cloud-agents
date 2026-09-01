package managedhost

import (
	"testing"
	"time"
)

func TestMutationDigestsAreStableAndBindTheOperation(t *testing.T) {
	create := CreateEnvironmentLeaseInput{
		Scope:   Scope{TenantID: "tenant-a", ProjectID: "project-a"},
		LeaseID: "lease-a", LeaseName: "default", ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TargetID: "docker-a", TTLSeconds: 3600, ExpectedTargetGeneration: 1,
		Mutation: Mutation{RequestID: "request-a", IdempotencyKey: "create-key-123456"},
	}
	first, err := CreateMutationDigest(create)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateMutationDigest(create)
	if err != nil || first != second {
		t.Fatalf("digest is not stable: %q %q err=%v", first, second, err)
	}
	create.ExpectedTargetGeneration++
	third, err := CreateMutationDigest(create)
	if err != nil || first == third {
		t.Fatalf("digest did not bind request payload: %q %q err=%v", first, third, err)
	}
	create.LeaseID = "-invalid"
	if err := create.Validate("tenant-a"); err == nil {
		t.Fatal("invalid identifier was accepted")
	}
}

func TestSnapshotTargetBindingIsOptionalOnlyAsAPair(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Scope: Scope{TenantID: "tenant-a", ProjectID: "project-a"}, LeaseID: "lease-a", LeaseName: "lease-a",
		ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Generation:    1, DesiredPhase: "active", ObservedPhase: "provisioning", CleanupPhase: "none",
		EnvironmentID: "lease-a", ExpiresAt: now.Add(time.Hour), ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("legacy snapshot: %v", err)
	}
	snapshot.TargetID = "docker-a"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("partial target binding was accepted")
	}
	snapshot.TargetGeneration = 1
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("target-bound snapshot: %v", err)
	}
}
