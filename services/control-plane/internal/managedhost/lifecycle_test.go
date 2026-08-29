package managedhost

import "testing"

func TestMutationDigestsAreStableAndBindTheOperation(t *testing.T) {
	create := CreateEnvironmentLeaseInput{
		Scope:   Scope{TenantID: "tenant-a", ProjectID: "project-a"},
		LeaseID: "lease-a", LeaseName: "default", ReleaseDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TTLSeconds: 3600, Mutation: Mutation{RequestID: "request-a", IdempotencyKey: "create-key-123456"},
	}
	first, err := CreateMutationDigest(create)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateMutationDigest(create)
	if err != nil || first != second {
		t.Fatalf("digest is not stable: %q %q err=%v", first, second, err)
	}
	create.TTLSeconds++
	third, err := CreateMutationDigest(create)
	if err != nil || first == third {
		t.Fatalf("digest did not bind request payload: %q %q err=%v", first, third, err)
	}
	create.LeaseID = "-invalid"
	if err := create.Validate("tenant-a"); err == nil {
		t.Fatal("invalid identifier was accepted")
	}
}
