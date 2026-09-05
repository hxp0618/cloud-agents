package networkpolicy

import (
	"testing"
	"time"
)

func TestNetworkPolicyValidationAndDigest(t *testing.T) {
	input := SetInput{
		Scope:    Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "network-restricted", PolicyName: "network-restricted",
		UserSummary: "Approved destinations only", DefaultEgress: DefaultEgressRestricted,
		AllowlistPolicyRef: "allowlist-standard", DNSPolicyRef: "dns-standard", ProxyPolicyRef: "proxy-standard",
		Mutation: Mutation{RequestID: "request-network", IdempotencyKey: "network-policy-test-001"},
	}
	first, err := MutationDigest(input)
	if err != nil || len(first) != 71 {
		t.Fatalf("MutationDigest() = %q, %v", first, err)
	}
	input.PreviewEnabled = true
	second, err := MutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("preview must affect digest: %q %q %v", first, second, err)
	}
	input.DefaultEgress = "unknown"
	if input.Validate(input.Scope.TenantID) == nil {
		t.Fatal("unsupported default egress must be rejected")
	}
	now := time.Now().UTC()
	snapshot := Snapshot{
		Scope:    Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		PolicyID: "network-public", PolicyName: "network-public",
		UserSummary: "Public internet access", DefaultEgress: DefaultEgressPublic,
		ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Snapshot.Validate() error = %v", err)
	}
}
