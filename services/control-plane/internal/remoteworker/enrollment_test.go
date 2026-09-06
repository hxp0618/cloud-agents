package remoteworker

import (
	"strings"
	"testing"
	"time"
)

func TestEnrollmentSecretAndLifecycle(t *testing.T) {
	secret, err := NewEnrollmentSecret()
	if err != nil || !strings.HasPrefix(secret, "carw1_") || len(secret) != 49 {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if digest, err := SecretDigest(secret); err != nil || len(digest) != 71 {
		t.Fatalf("digest=%q err=%v", digest, err)
	}
	now := time.Now().UTC()
	claimed := now.Add(time.Second)
	snapshot := Snapshot{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, EnrollmentID: "enrollment-alpha",
		TargetID: TargetID(Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, "enrollment-alpha"),
		WorkerID: "worker-alpha", WorkerName: "worker-alpha", State: StateSecretIssued, ResourceVersion: 2,
		CreatedAt: now, UpdatedAt: claimed, ExpiresAt: now.Add(5 * time.Minute), SecretClaimedAt: &claimed,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.EnrolledAt = &claimed
	if snapshot.Validate() == nil {
		t.Fatal("secret-issued enrollment accepted enrolled timestamp")
	}
}
