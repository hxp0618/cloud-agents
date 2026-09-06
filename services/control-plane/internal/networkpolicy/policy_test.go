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

func TestCanonicalAllowedEgress(t *testing.T) {
	canonical, err := CanonicalAllowedEgress([]string{" API.OpenAI.com ", "10.0.0.1/8", "2001:0db8::1"})
	want := []string{"10.0.0.0/8", "2001:db8::1", "api.openai.com"}
	if err != nil || !equalStrings(canonical, want) {
		t.Fatalf("canonical = %v, %v", canonical, err)
	}
	for _, values := range [][]string{
		{"api.openai.com", "API.OPENAI.COM"},
		{"localhost"},
		{"10.0.0.0/33"},
	} {
		if _, err := CanonicalAllowedEgress(values); err == nil {
			t.Fatalf("accepted invalid targets %v", values)
		}
	}
	input := SetInput{
		Scope: Scope{TenantID: "tenant", ProjectID: "project"}, PolicyID: "policy", PolicyName: "policy",
		UserSummary: "Direct allowlist", DefaultEgress: DefaultEgressRestricted, AllowedEgress: want,
		Mutation: Mutation{RequestID: "request", IdempotencyKey: "network-policy-test-002"},
	}
	if err := input.Validate("tenant"); err != nil {
		t.Fatal(err)
	}
	input.AllowedEgress = []string{"api.openai.com", "10.0.0.1/8"}
	if err := input.Validate("tenant"); err == nil {
		t.Fatal("accepted non-canonical direct allowlist")
	}
}
