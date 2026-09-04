package storagepolicy

import (
	"testing"
	"time"
)

func TestStoragePolicyValidationAndDigest(t *testing.T) {
	input := SetInput{
		Scope:    Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "storage-standard", PolicyName: "storage-standard",
		UserSummary: "20 GiB managed workspace", WorkspaceType: WorkspaceTypeManagedVolume,
		WorkspaceCapacityBytes: 21474836480, CleanupOnLeaseTermination: true,
		AllowWorkspaceReuse: true, Mutation: Mutation{RequestID: "request-storage", IdempotencyKey: "storage-policy-test-001"},
	}
	first, err := MutationDigest(input)
	if err != nil || len(first) != 71 {
		t.Fatalf("MutationDigest() = %q, %v", first, err)
	}
	input.WorkspaceCapacityBytes++
	second, err := MutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("capacity must affect digest: %q %q %v", first, second, err)
	}
	input.RetentionSeconds = 1
	if input.Validate(input.Scope.TenantID) == nil {
		t.Fatal("unsupported retention must be rejected")
	}
	now := time.Now().UTC()
	snapshot := Snapshot{
		Scope:    Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "storage-standard", PolicyName: "storage-standard",
		UserSummary: "20 GiB managed workspace", WorkspaceType: WorkspaceTypeManagedVolume,
		WorkspaceCapacityBytes: 21474836480, CleanupOnLeaseTermination: true,
		AllowWorkspaceReuse: true, ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Snapshot.Validate() error = %v", err)
	}
}
