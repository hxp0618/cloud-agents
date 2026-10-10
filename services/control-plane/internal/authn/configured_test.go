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

func TestConfiguredVerifierReloadTogetherRejectsPartialPublication(t *testing.T) {
	user := configuredVerifierForTest(t)
	adminConfig := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://admin.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-1"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	admin, err := NewConfiguredVerifier(adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Invalidate)
	userConfig := adminConfig
	userConfig.Audience = "https://api.example"
	userConfig.Generation = 2
	adminConfig.Generation = 2
	invalidAdmin := adminConfig
	invalidAdmin.Issuer = "https://substituted.example"
	if err := user.ReloadTogether(userConfig, admin, invalidAdmin); !errors.Is(err, ErrInvalidConfiguredVerifier) {
		t.Fatalf("invalid second candidate accepted: %v", err)
	}
	if currentSnapshot(user.lineage).generation != 1 || currentSnapshot(admin.lineage).generation != 1 {
		t.Fatal("failed pair reload changed one audience")
	}
	if err := user.ReloadTogether(userConfig, admin, adminConfig); err != nil {
		t.Fatal(err)
	}
	if currentSnapshot(user.lineage).generation != 2 || currentSnapshot(admin.lineage).generation != 2 {
		t.Fatal("pair reload did not publish both candidates")
	}
	claims := validClaims()
	claims["aud"] = adminConfig.Audience
	request := VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"}
	if _, err := admin.Verify(tokenFor(t, testPrivateKey(t), validHeader(), claims), request); err != nil {
		t.Fatalf("admin candidate did not verify: %v", err)
	}
	if _, err := user.Verify(tokenFor(t, testPrivateKey(t), validHeader(), claims), request); errorCategory(err) != errorAudienceMismatch {
		t.Fatalf("cross-audience token accepted: %v", err)
	}
	userConfig.Generation, adminConfig.Generation = 3, 3
	results := make(chan error, 2)
	go func() { results <- user.ReloadTogether(userConfig, admin, adminConfig) }()
	go func() { results <- admin.ReloadTogether(adminConfig, user, userConfig) }()
	accepted := 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrInvalidConfiguredVerifier) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("reverse-order reload deadlocked")
		}
	}
	if accepted != 1 || currentSnapshot(user.lineage).generation != 3 || currentSnapshot(admin.lineage).generation != 3 {
		t.Fatal("concurrent pair reload did not admit exactly one generation")
	}
}

func TestConfiguredVerifierReloadTogetherClosesBothAdmissionsBeforeDrain(t *testing.T) {
	user := configuredVerifierForTest(t)
	adminConfig := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://admin.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-1"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	admin, err := NewConfiguredVerifier(adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Invalidate)

	claims := validClaims()
	claims["aud"] = adminConfig.Audience
	principal, err := admin.Verify(
		tokenFor(t, testPrivateKey(t), validHeader(), claims),
		VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"},
	)
	if err != nil {
		t.Fatal(err)
	}
	heldGeneration, _, acquired := user.lineage.acquireCurrent()
	if !acquired {
		t.Fatal("could not hold old User generation")
	}
	released := false
	defer func() {
		if !released {
			heldGeneration.lease.RUnlock()
		}
	}()

	userConfig := adminConfig
	userConfig.Audience = "https://api.example"
	userConfig.Generation = 2
	adminConfig.Generation = 2
	reloaded := make(chan error, 1)
	go func() { reloaded <- user.ReloadTogether(userConfig, admin, adminConfig) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		user.lineage.state.Lock()
		closed := !user.lineage.admitting && user.lineage.current == nil
		user.lineage.state.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pair reload did not close User admission")
		}
		time.Sleep(time.Millisecond)
	}
	callbackRan := false
	if err := ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error {
		callbackRan = true
		return nil
	}); errorCategory(err) != errorInternalFailure || callbackRan {
		t.Fatalf("old peer principal entered after pair cutover: err=%v callback=%t", err, callbackRan)
	}

	heldGeneration.lease.RUnlock()
	released = true
	select {
	case err := <-reloaded:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pair reload did not finish after old lease settled")
	}
}

func TestConfiguredVerifierInvalidateTogetherClosesBothAdmissionsBeforeDrain(t *testing.T) {
	user := configuredVerifierForTest(t)
	adminConfig := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Audience: "https://admin.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Keys:  []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-1"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}},
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	admin, err := NewConfiguredVerifier(adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Invalidate)

	claims := validClaims()
	claims["aud"] = adminConfig.Audience
	principal, err := admin.Verify(
		tokenFor(t, testPrivateKey(t), validHeader(), claims),
		VerificationRequest{TenantID: "tenant-1", ResourceLevel: "tenant", ResourceID: "tenant-1", RequiredPermission: "agents.get"},
	)
	if err != nil {
		t.Fatal(err)
	}
	heldGeneration, _, acquired := user.lineage.acquireCurrent()
	if !acquired {
		t.Fatal("could not hold old User generation")
	}
	released := false
	defer func() {
		if !released {
			heldGeneration.lease.RUnlock()
		}
	}()

	invalidated := make(chan struct{})
	go func() {
		user.InvalidateTogether(admin)
		close(invalidated)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		user.lineage.state.Lock()
		userClosed := !user.lineage.admitting && user.lineage.current == nil
		user.lineage.state.Unlock()
		admin.lineage.state.Lock()
		adminClosed := !admin.lineage.admitting && admin.lineage.current == nil
		admin.lineage.state.Unlock()
		if userClosed && adminClosed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pair invalidation did not close both admissions before draining")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-invalidated:
		t.Fatal("pair invalidation finished while an old lease was held")
	default:
	}
	callbackRan := false
	if err := ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error {
		callbackRan = true
		return nil
	}); errorCategory(err) != errorInternalFailure || callbackRan {
		t.Fatalf("old peer principal entered after pair invalidation: err=%v callback=%t", err, callbackRan)
	}

	heldGeneration.lease.RUnlock()
	released = true
	select {
	case <-invalidated:
	case <-time.After(5 * time.Second):
		t.Fatal("pair invalidation did not finish after old lease settled")
	}
	if user.Ready() || admin.Ready() {
		t.Fatal("pair invalidation left one verifier ready")
	}
}

func TestConfiguredVerifierReloadTogetherRejectsDivergentPermanentHistory(t *testing.T) {
	base := ConfiguredVerifierConfig{
		Issuer: "https://issuer.example", Generation: 1, SecurityEpoch: 7,
		NotBefore: testNow - 100, ExpiresAt: testNow + 1000,
		Clock: func() time.Time { return time.Unix(testNow, 0) },
	}
	userConfig := base
	userConfig.Audience = "https://api.example"
	userConfig.Keys = []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "retired-user"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}}
	adminConfig := base
	adminConfig.Audience = "https://admin.example"
	adminConfig.Keys = []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "retired-admin"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}}
	user, err := NewConfiguredVerifier(userConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(user.Invalidate)
	admin, err := NewConfiguredVerifier(adminConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Invalidate)

	sharedKey := []ConfiguredVerifierKey{{JWK: jwkFor(t, testPrivateKey(t), "key-2"), Enabled: true, NotBefore: testNow - 1000, NotAfter: testNow + 1000}}
	userConfig.Generation, adminConfig.Generation = 2, 2
	userConfig.Keys, adminConfig.Keys = sharedKey, sharedKey
	if err := user.ReloadTogether(userConfig, admin, adminConfig); !errors.Is(err, ErrInvalidConfiguredVerifier) {
		t.Fatalf("divergent permanent histories accepted: %v", err)
	}
	if currentSnapshot(user.lineage).generation != 1 || currentSnapshot(admin.lineage).generation != 1 {
		t.Fatal("rejected divergent histories changed an active generation")
	}
}
