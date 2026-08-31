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
		t.Fatalf("invalid key result=%v err=%v", verifier, err)
	}
}

func TestConfiguredVerifierStartsFromCurrentTrustGeneration(t *testing.T) {
	config := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 2, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-2"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	verifier, err := NewConfiguredVerifier(config)
	if err != nil || !verifier.Ready() {
		t.Fatalf("current trust generation result=%v err=%v", verifier, err)
	}
	t.Cleanup(verifier.Invalidate)
	config.Generation++
	if err := verifier.Reload(config); err != nil {
		t.Fatalf("next trust generation reload: %v", err)
	}
}

func TestConfiguredVerifierReloadAdvancesTrustGenerationAtomically(t *testing.T) {
	verifier := configuredVerifierForTest(t)
	oldToken := tokenFor(t, testPrivateKey(t), validHeader(), validClaims())
	newHeader := validHeader()
	newHeader["kid"] = "key-2"
	newToken := tokenFor(t, testPrivateKey(t), newHeader, validClaims())
	config := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 2, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-2"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	if err := verifier.Reload(config); err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(newToken, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}); err != nil {
		t.Fatalf("new trust snapshot rejected token: %v", err)
	}
	if _, err := verifier.Verify(oldToken, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}); errorCategory(err) != errorUnknownKey {
		t.Fatalf("old key category=%v", errorCategory(err))
	}
	if err := verifier.Reload(config); !errors.Is(err, ErrInvalidConfiguredVerifier) {
		t.Fatalf("repeated generation reload error=%v", err)
	}
	if _, err := verifier.Verify(newToken, VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}); err != nil {
		t.Fatalf("failed reload disturbed active snapshot: %v", err)
	}
}

func TestConfiguredVerifierReadinessTracksTrustWindowAndReload(t *testing.T) {
	now := time.Unix(testNow, 0)
	clock := func() time.Time { return now }
	key := jwkFor(t, testPrivateKey(t), "key-1")
	verifier, err := NewConfiguredVerifier(ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 100,
		Keys: []ConfiguredVerifierKey{{JWK: key, Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}}, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.Ready() {
		t.Fatal("active trust snapshot is not ready")
	}
	now = time.Unix(testNow+100, 0)
	if verifier.Ready() {
		t.Fatal("expired trust snapshot is ready")
	}
	if err := verifier.Reload(ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://api.example", Generation: 2, SecurityEpoch: 7,
		NotBefore: testNow + 99, ExpiresAt: testNow + 1000,
		Keys: []ConfiguredVerifierKey{{JWK: key, Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}}, Clock: clock,
	}); err != nil {
		t.Fatal(err)
	}
	if !verifier.Ready() {
		t.Fatal("reloaded trust snapshot is not ready")
	}
	verifier.Invalidate()
	if verifier.Ready() || (*ConfiguredVerifier)(nil).Ready() {
		t.Fatal("invalidated or nil verifier is ready")
	}
}
