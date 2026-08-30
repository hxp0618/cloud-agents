//go:build localdev

package authn

import (
	"strings"
	"testing"
	"time"
)

func TestLocalVerifierValidAndOneShotPrincipal(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier, err := NewLocalVerifier(LocalVerifierConfig{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.IssueToken(LocalTokenClaims{TenantID: "tenant-1", Subject: "local-user"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(token, LocalVerificationRequest{
		TenantID: "tenant-1", ResourceLevel: "organization", ResourceID: "organization-1", RequiredPermission: "projects.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ConsumeVerifiedPrincipal(principal, func(view VerifiedPrincipalView) error {
		kind, issuer, subject, ok := view.Actor()
		if !ok || kind != "user" || issuer != localIssuer || subject != "local-user" {
			t.Fatalf("unexpected actor: %q %q %q %v", kind, issuer, subject, ok)
		}
		tenant, level, resource, permission, ok := view.AuthorizationContext()
		if !ok || tenant != "tenant-1" || level != "organization" || resource != "organization-1" || permission != "projects.create" {
			t.Fatalf("unexpected authority: %q %q %q %q %v", tenant, level, resource, permission, ok)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatalf("second consume category=%v", err)
	}
}

func TestLocalVerifierRejectsTenantScopeAndSignatureMismatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier, err := NewLocalVerifier(LocalVerifierConfig{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.IssueToken(LocalTokenClaims{TenantID: "tenant-1", Subject: "local-user"})
	if err != nil {
		t.Fatal(err)
	}
	request := LocalVerificationRequest{
		TenantID: "tenant-2", ResourceLevel: "organization", ResourceID: "organization-1", RequiredPermission: "projects.create",
	}
	if _, err := verifier.Verify(token, request); errorCategory(err) != errorTenantMismatch {
		t.Fatalf("tenant mismatch category=%v", err)
	}
	request.TenantID = "tenant-1"
	request.RequiredPermission = "projects.delete"
	if _, err := verifier.Verify(token, request); errorCategory(err) != errorScopeMismatch {
		t.Fatalf("scope mismatch category=%v", err)
	}
	parts := strings.Split(token, ".")
	parts[2] = strings.Repeat("A", len(parts[2]))
	request.RequiredPermission = "projects.create"
	if _, err := verifier.Verify(strings.Join(parts, "."), request); errorCategory(err) != errorInvalidSignature {
		t.Fatalf("signature mismatch category=%v", err)
	}
}

func TestLocalVerifierAllowsLocalProductPermissions(t *testing.T) {
	verifier, err := NewLocalVerifier(LocalVerifierConfig{Clock: func() time.Time { return time.Unix(1_800_000_000, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.IssueToken(LocalTokenClaims{TenantID: "tenant-1", Subject: "local-user"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		permission, level, resourceID string
	}{
		{"memberships.create", "tenant", "tenant-1"},
		{"memberships.delete", "tenant", "tenant-1"},
		{"memberships.get", "tenant", "tenant-1"},
		{"memberships.list", "tenant", "tenant-1"},
		{"memberships.update", "tenant", "tenant-1"},
		{"organizations.create", "tenant", "tenant-1"},
		{"organizations.get", "organization", "organization-1"},
		{"organizations.list", "tenant", "tenant-1"},
		{"projects.act", "project", "project-1"},
		{"projects.create", "organization", "organization-1"},
		{"projects.get", "project", "project-1"},
		{"projects.list", "organization", "organization-1"},
		{"role-bindings.bind", "tenant", "tenant-1"},
		{"role-bindings.delete", "tenant", "tenant-1"},
		{"role-bindings.get", "tenant", "tenant-1"},
		{"role-bindings.list", "tenant", "tenant-1"},
		{"roles.get", "tenant", "tenant-1"},
		{"roles.list", "tenant", "tenant-1"},
		{"tenants.get", "tenant", "tenant-1"},
	} {
		if _, err := verifier.Verify(token, LocalVerificationRequest{
			TenantID: "tenant-1", ResourceLevel: test.level, ResourceID: test.resourceID, RequiredPermission: test.permission,
		}); err != nil {
			t.Errorf("%s verification failed: %v", test.permission, err)
		}
	}
	if _, err := verifier.Verify(token, LocalVerificationRequest{
		TenantID: "tenant-1", ResourceLevel: "organization", ResourceID: "organization-1", RequiredPermission: "organizations.delete",
	}); errorCategory(err) != errorScopeMismatch {
		t.Fatalf("unexpected unexposed permission category: %v", err)
	}
}

func TestLocalVerifierInvalidateFailsClosed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	verifier, err := NewLocalVerifier(LocalVerifierConfig{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	token, err := verifier.IssueToken(LocalTokenClaims{TenantID: "tenant-1", Subject: "local-user"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := verifier.Verify(token, LocalVerificationRequest{
		TenantID: "tenant-1", ResourceLevel: "organization", ResourceID: "organization-1", RequiredPermission: "projects.create",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier.Invalidate()
	verifier.Invalidate()
	if _, err := verifier.IssueToken(LocalTokenClaims{TenantID: "tenant-1", Subject: "local-user"}); errorCategory(err) != errorInternalFailure {
		t.Fatalf("issue after invalidation category=%v", err)
	}
	if _, err := verifier.Verify(token, LocalVerificationRequest{
		TenantID: "tenant-1", ResourceLevel: "organization", ResourceID: "organization-1", RequiredPermission: "projects.create",
	}); errorCategory(err) != errorInternalFailure {
		t.Fatalf("verify after invalidation category=%v", err)
	}
	if err := ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatalf("principal survived invalidation category=%v", err)
	}
}
