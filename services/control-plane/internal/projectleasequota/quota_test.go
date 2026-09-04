package projectleasequota

import "testing"

func TestSetInputDigestFencesResourceVersionAndLimits(t *testing.T) {
	input := SetInput{
		Scope:                   Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		ExpectedResourceVersion: 0, MaxConcurrentLeases: 2, MaxCPUMillis: 4000,
		MaxMemoryBytes: 8589934592, MaxLeaseTTLSeconds: 3600,
		Mutation: Mutation{RequestID: "request-quota", IdempotencyKey: "quota-set-key-0001"},
	}
	first, err := MutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.MaxConcurrentLeases++
	second, err := MutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("quota limit was not fenced by the mutation digest")
	}
	input.ExpectedResourceVersion = -1
	if input.Validate(input.Scope.TenantID) == nil {
		t.Fatal("negative expected resource version was accepted")
	}
}
