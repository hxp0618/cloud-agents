package authn

import (
	"errors"
	"testing"
	"time"
)

func configuredVerifierForTest(t *testing.T) *ConfiguredVerifier {
	t.Helper()
	verifier, err := NewConfiguredVerifier(ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-1"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(verifier.Invalidate)
	return verifier
}

func TestConfiguredVerifierUsesImmutableStartupTrust(t *testing.T) {
	verifier := configuredVerifierForTest(t)
	token := tokenFor(t, testPrivateKey(t), validHeader(), validClaims())
	principal, err := verifier.Verify(token, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"})
	if err != nil || principal == nil {
		t.Fatalf("configured verification failed: %v", err)
	}
	if _, err := verifier.Verify(token, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.delete"}); errorCategory(err) != errorScopeMismatch {
		t.Fatalf("wrong permission category=%v", errorCategory(err))
	}
	verifier.Invalidate()
	if _, err := verifier.Verify(token, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}); errorCategory(err) != errorInternalFailure {
		t.Fatalf("invalidated verifier category=%v", errorCategory(err))
	}
}

func TestConfiguredVerifierRejectsIncompleteTrustInput(t *testing.T) {
	if verifier, err := NewConfiguredVerifier(ConfiguredVerifierConfig{}); verifier != nil || !errors.Is(err, ErrInvalidConfiguredVerifier) {
		t.Fatalf("empty config result=%v err=%v", verifier, err)
	}
	config := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 2, SecurityEpoch: 1,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys: []ConfiguredVerifierKey{{JWK: []byte(`{"kty":"RSA","n":"bad","e":"AQAB"}`), Enabled: true}}, Clock: time.Now,
	}
	if verifier, err := NewConfiguredVerifier(config); verifier != nil || !errors.Is(err, ErrInvalidConfiguredVerifier) {
		t.Fatalf("non-initial generation result=%v err=%v", verifier, err)
	}
}
