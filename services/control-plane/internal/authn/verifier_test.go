package authn

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testNow = int64(1_800_000_000)

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
	testKeyErr  error
)

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() { testKey, testKeyErr = rsa.GenerateKey(rand.Reader, 2048) })
	if testKeyErr != nil {
		t.Fatal(testKeyErr)
	}
	return testKey
}

func jwkFor(t *testing.T, key *rsa.PrivateKey, kid string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": kid,
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), "use": "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func validClaims() map[string]any {
	return map[string]any{
		"iss": "https://issuer.example", "sub": "subject-1", "aud": "https://api.example",
		"exp": testNow + 300, "iat": testNow - 10, "jti": "token-1", "client_id": "client-1",
		"scope": "agents.get agents.update", claimSubjectKind: "user", claimTenantID: "tenant-1",
		claimSecurityEpoch: int64(7), claimTokenProfile: "cloud-agents-access-token/v1",
	}
}

func tokenFor(t *testing.T, key *rsa.PrivateKey, header, claims map[string]any) string {
	t.Helper()
	headerBytes, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	claimBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return tokenForRaw(t, key, headerBytes, claimBytes)
}

func tokenForRaw(t *testing.T, key *rsa.PrivateKey, header, claims []byte) string {
	t.Helper()
	protected := base64.RawURLEncoding.EncodeToString(header)
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(protected + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return protected + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func validHeader() map[string]any {
	return map[string]any{"alg": "RS256", "kid": "key-1", "typ": "at+jwt"}
}

func contextAndLineage(t *testing.T, revokedKeys, revokedTokens []string) (verificationContext, *trustLineage) {
	t.Helper()
	lineage := newTrustLineage()
	err := lineage.replace(snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
		securityEpoch: 7, notBefore: testNow - 100, expiresAt: testNow + 1000,
		keys:          []snapshotKeyCandidate{{jwk: jwkFor(t, testPrivateKey(t), "key-1"), enabled: true, notBefore: testNow - 1000, notAfter: testNow + 1000}},
		revokedKeyIDs: revokedKeys, revokedTokenIDs: revokedTokens,
	})
	if err != nil {
		t.Fatal(err)
	}
	return verificationContext{
		lineage: lineage, clock: func() time.Time { return time.Unix(testNow, 999_999_999) },
		targetTenantID: "tenant-1", targetResourceLevel: targetTenant, targetResourceID: "tenant-1",
		requiredPermission: "agents.get",
	}, lineage
}

func TestVerifyRS256AndConsumeOneShot(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	token := tokenFor(t, testPrivateKey(t), validHeader(), validClaims())
	principal, err := verifyAccessToken(context, token)
	if err != nil || principal == nil {
		t.Fatalf("valid token failed: %v", err)
	}
	var escaped VerifiedPrincipalView
	if err := ConsumeVerifiedPrincipal(principal, func(view VerifiedPrincipalView) error {
		escaped = view
		kind, issuer, subject, ok := view.Actor()
		if !ok || kind != "user" || issuer != "https://issuer.example" || subject != "subject-1" {
			t.Fatalf("unexpected subject: %q %q %q %v", kind, issuer, subject, ok)
		}
		tenant, level, resource, permission, ok := view.AuthorizationContext()
		if !ok || tenant != "tenant-1" || level != "tenant" || resource != "tenant-1" || permission != "agents.get" {
			t.Fatalf("unexpected target: %q %q %q %q %v", tenant, level, resource, permission, ok)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := escaped.Actor(); ok {
		t.Fatal("callback view remained live after callback")
	}
	if err := ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatalf("second consume category=%v", err)
	}
}

func TestPrincipalCopyTamperZeroAndConcurrentFailClosed(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	makePrincipal := func() *VerifiedPrincipal {
		principal, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
		if err != nil {
			t.Fatal(err)
		}
		return principal
	}
	copySource := makePrincipal()
	copyValue := *copySource
	if err := ConsumeVerifiedPrincipal(&copyValue, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatal("ordinary principal copy was accepted")
	}
	if err := ConsumeVerifiedPrincipal(copySource, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatal("failed copy consume did not permanently consume source")
	}
	tampered := makePrincipal()
	tampered.subjectValue = "attacker"
	if err := ConsumeVerifiedPrincipal(tampered, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatal("tampered principal was accepted")
	}
	if err := ConsumeVerifiedPrincipal(&VerifiedPrincipal{}, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatal("literal/zero principal was accepted")
	}
	concurrent := makePrincipal()
	var successes atomic.Int32
	var wait sync.WaitGroup
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if ConsumeVerifiedPrincipal(concurrent, func(VerifiedPrincipalView) error { return nil }) == nil {
				successes.Add(1)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent success count=%d", successes.Load())
	}
}

func TestConsumeNilErrorPanicAndAsyncViewAreFailClosed(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	makePrincipal := func() *VerifiedPrincipal {
		principal, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
		if err != nil {
			t.Fatal(err)
		}
		return principal
	}
	nilCallback := makePrincipal()
	if errorCategory(ConsumeVerifiedPrincipal(nilCallback, nil)) != errorInternalFailure ||
		errorCategory(ConsumeVerifiedPrincipal(nilCallback, func(VerifiedPrincipalView) error { return nil })) != errorInternalFailure {
		t.Fatal("nil callback did not permanently consume")
	}
	sentinel := errors.New("callback sentinel")
	callbackError := makePrincipal()
	if err := ConsumeVerifiedPrincipal(callbackError, func(VerifiedPrincipalView) error { return sentinel }); err != sentinel {
		t.Fatalf("callback error changed: %v", err)
	}
	if errorCategory(ConsumeVerifiedPrincipal(callbackError, func(VerifiedPrincipalView) error { return nil })) != errorInternalFailure {
		t.Fatal("callback error did not consume")
	}
	panicPrincipal := makePrincipal()
	var escaped VerifiedPrincipalView
	func() {
		defer func() {
			if recovered := recover(); recovered != "panic sentinel" {
				t.Fatalf("panic changed: %v", recovered)
			}
		}()
		_ = ConsumeVerifiedPrincipal(panicPrincipal, func(view VerifiedPrincipalView) error {
			escaped = view
			panic("panic sentinel")
		})
	}()
	if escaped.Check() {
		t.Fatal("panicking callback left view live")
	}
	if errorCategory(ConsumeVerifiedPrincipal(panicPrincipal, func(VerifiedPrincipalView) error { return nil })) != errorInternalFailure {
		t.Fatal("panic did not consume")
	}
}

func TestVerifierStableCategoryMatrixAndPrecedence(t *testing.T) {
	baseContext, _ := contextAndLineage(t, nil, nil)
	key := testPrivateKey(t)
	tests := []struct {
		name     string
		context  func() verificationContext
		header   func(map[string]any)
		claims   func(map[string]any)
		mutate   func(string) string
		category verifierErrorCategory
	}{
		{name: "unsupported algorithm", header: func(v map[string]any) { v["alg"] = "HS256" }, category: errorUnsupportedAlgorithm},
		{name: "unsupported typ", header: func(v map[string]any) { v["typ"] = "JWT" }, category: errorUnsupportedProfile},
		{name: "unknown key", header: func(v map[string]any) { v["kid"] = "unknown" }, category: errorUnknownKey},
		{name: "issuer", claims: func(v map[string]any) { v["iss"] = "https://other.example" }, category: errorIssuerMismatch},
		{name: "audience", claims: func(v map[string]any) { v["aud"] = "https://other.example" }, category: errorAudienceMismatch},
		{name: "time", claims: func(v map[string]any) { v["exp"] = testNow - 61 }, category: errorTimeInvalid},
		{name: "epoch", claims: func(v map[string]any) { v[claimSecurityEpoch] = 8 }, category: errorEpochMismatch},
		{name: "tenant", claims: func(v map[string]any) { v[claimTenantID] = "tenant-2" }, category: errorTenantMismatch},
		{name: "project", claims: func(v map[string]any) { v[claimProjectID] = "project-1" }, category: errorProjectMismatch},
		{name: "scope", claims: func(v map[string]any) { v["scope"] = "agents.update" }, category: errorScopeMismatch},
		{name: "profile", claims: func(v map[string]any) { v[claimTokenProfile] = "other/v1" }, category: errorUnsupportedProfile},
		{name: "bad signature", mutate: func(token string) string {
			parts := strings.Split(token, ".")
			parts[2] = strings.Repeat("A", len(parts[2]))
			return strings.Join(parts, ".")
		}, category: errorInvalidSignature},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := baseContext
			if test.context != nil {
				context = test.context()
			}
			header, claims := validHeader(), validClaims()
			if test.header != nil {
				test.header(header)
			}
			if test.claims != nil {
				test.claims(claims)
			}
			token := tokenFor(t, key, header, claims)
			if test.mutate != nil {
				token = test.mutate(token)
			}
			_, err := verifyAccessToken(context, token)
			if got := errorCategory(err); got != test.category {
				t.Fatalf("category=%q want=%q error=%v", got, test.category, err)
			}
			if err == nil || strings.Contains(err.Error(), token) {
				t.Fatal("error leaked or disappeared")
			}
		})
	}

	// A correct-length crypto failure wins over signed semantic mismatches.
	badClaims := validClaims()
	badClaims["iss"] = "https://other.example"
	token := tokenFor(t, key, validHeader(), badClaims)
	parts := strings.Split(token, ".")
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	signature[len(signature)-1] ^= 1
	parts[2] = base64.RawURLEncoding.EncodeToString(signature)
	_, err := verifyAccessToken(baseContext, strings.Join(parts, "."))
	if errorCategory(err) != errorInvalidSignature {
		t.Fatalf("semantic mismatch beat crypto failure: %v", err)
	}
	parts = strings.Split(tokenFor(t, key, validHeader(), validClaims()), ".")
	parts[2] = base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
	_, err = verifyAccessToken(baseContext, strings.Join(parts, "."))
	if errorCategory(err) != errorInvalidSignature {
		t.Fatalf("signature representative at modulus category=%v", err)
	}

	revokedContext, _ := contextAndLineage(t, nil, []string{"token-1"})
	claims := validClaims()
	claims[claimSecurityEpoch] = 8
	_, err = verifyAccessToken(revokedContext, tokenFor(t, key, validHeader(), claims))
	if errorCategory(err) != errorRevokedToken {
		t.Fatalf("revoked token did not precede epoch: %v", err)
	}
	revokedKeyContext, _ := contextAndLineage(t, []string{"key-1"}, nil)
	_, err = verifyAccessToken(revokedKeyContext, tokenFor(t, key, validHeader(), validClaims()))
	if errorCategory(err) != errorRevokedKey {
		t.Fatalf("revoked key category=%v", err)
	}
}

func TestRetiredRevokedKeyPrecedesUnknownAndOwnedDigestDriftIsInternal(t *testing.T) {
	context, lineage := contextAndLineage(t, nil, nil)
	previous := currentSnapshotForTest(lineage)
	secondKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if err := lineage.replace(snapshotCandidate{
		issuer: previous.issuer, audience: previous.audience, generation: 2,
		previousSnapshotDigest: previous.digest, securityEpoch: previous.securityEpoch,
		notBefore: testNow - 100, expiresAt: testNow + 1000,
		keys:          []snapshotKeyCandidate{{jwk: jwkFor(t, secondKey, "key-2"), enabled: true, notBefore: testNow - 1000, notAfter: testNow + 1000}},
		revokedKeyIDs: []string{"key-1"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
	if errorCategory(err) != errorRevokedKey {
		t.Fatalf("retired revoked kid category=%v", err)
	}

	for _, digestField := range []string{"profile", "registry"} {
		t.Run(digestField, func(t *testing.T) {
			context, lineage := contextAndLineage(t, nil, nil)
			lineage.state.Lock()
			if digestField == "profile" {
				lineage.current.snapshot.profileDigest = "sha256:" + strings.Repeat("0", 64)
			} else {
				lineage.current.snapshot.registryDigest = "sha256:" + strings.Repeat("0", 64)
			}
			lineage.state.Unlock()
			_, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
			if errorCategory(err) != errorInternalFailure {
				t.Fatalf("owned digest drift category=%v", err)
			}
		})
	}
}

func TestStrictCompactAndJSONAdmission(t *testing.T) {
	valid := []byte(`{"a":{"b":[{"c":1}]}}`)
	if _, ok := strictJSONObject(valid, len(valid), 4); !ok {
		t.Fatal("depth 4 object rejected")
	}
	faults := [][]byte{
		[]byte(`{"a":{"b":[{"c":{"d":1}}]}}`),
		[]byte(`{"a":1,"\u0061":2}`),
		[]byte(`{"a":1} {}`),
		[]byte(`[]`),
		[]byte("{\"a\":\"\xff\"}"),
		[]byte(`{"a":"\ud800"}`),
	}
	for index, raw := range faults {
		if _, ok := strictJSONObject(raw, 1024, 4); ok {
			t.Fatalf("fault %d accepted: %q", index, raw)
		}
	}
	for _, encoded := range []string{"", "AA=", "A A", "AA\n", "A", "+w"} {
		if _, ok := decodeCanonicalBase64url(encoded, 16); ok {
			t.Fatalf("noncanonical base64url %q accepted", encoded)
		}
	}
	if decoded, ok := decodeCanonicalBase64url("_w", 1); !ok || len(decoded) != 1 || decoded[0] != 0xff {
		t.Fatal("canonical base64url rejected")
	}
	for _, raw := range []string{"1.0", "1e3", "01", "-0", "9007199254740992"} {
		if _, ok := exactJSONInteger([]byte(raw), 0, 9007199254740991); ok {
			t.Fatalf("non-integer/range lexeme %q accepted", raw)
		}
	}
	object, ok := strictJSONObject([]byte(`{"unknown":-1.5e+2}`), 1024, 4)
	if !ok || object["unknown"] == nil {
		t.Fatal("ordinary unknown JSON number was rejected")
	}
}

func TestCompactStructuralAndSignatureLengthFaultsAreMalformed(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	key := testPrivateKey(t)
	valid := tokenFor(t, key, validHeader(), validClaims())
	parts := strings.Split(valid, ".")
	faults := []string{
		"a.b", "a.b.c.d", "=" + valid,
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"key-1","typ":"at+jwt","jku":"https://x"}`)) + "." + parts[1] + "." + parts[2],
		parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"x","iss":"y"}`)) + "." + parts[2],
		parts[0] + "." + parts[1] + ".AA",
	}
	for index, token := range faults {
		if _, err := verifyAccessToken(context, token); errorCategory(err) != errorMalformed {
			t.Fatalf("fault %d category=%v", index, err)
		}
	}
}

func TestChangedProtectedHeaderPayloadAndSignatureFailCrypto(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	token := tokenFor(t, testPrivateKey(t), validHeader(), validClaims())
	parts := strings.Split(token, ".")
	changedHeader := append([]string(nil), parts...)
	changedHeader[0] = base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"at+jwt","kid":"key-1","alg":"RS256"}`))
	claims := validClaims()
	claims["ordinary"] = "changed"
	changedClaimsBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	changedPayload := append([]string(nil), parts...)
	changedPayload[1] = base64.RawURLEncoding.EncodeToString(changedClaimsBytes)
	changedSignature := append([]string(nil), parts...)
	signature, _ := base64.RawURLEncoding.DecodeString(changedSignature[2])
	signature[0] ^= 1
	changedSignature[2] = base64.RawURLEncoding.EncodeToString(signature)
	for index, candidate := range [][]string{changedHeader, changedPayload, changedSignature} {
		if _, err := verifyAccessToken(context, strings.Join(candidate, ".")); errorCategory(err) != errorInvalidSignature {
			t.Fatalf("signed component change %d category=%v", index, err)
		}
	}
}

func TestRequiredClaimsTypesLexicalRangesAndUnknownClaims(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	key := testPrivateKey(t)
	required := []string{"aud", "client_id", "exp", "iat", "iss", "jti", "scope", "sub", claimSecurityEpoch, claimSubjectKind, claimTenantID, claimTokenProfile}
	for _, name := range required {
		t.Run("missing "+name, func(t *testing.T) {
			claims := validClaims()
			delete(claims, name)
			if _, err := verifyAccessToken(context, tokenFor(t, key, validHeader(), claims)); errorCategory(err) != errorMalformed {
				t.Fatalf("missing %s category=%v", name, err)
			}
		})
	}
	faults := []func(map[string]any){
		func(c map[string]any) { c["aud"] = []string{"https://api.example"} },
		func(c map[string]any) { c["exp"] = json.RawMessage(`1800000300.0`) },
		func(c map[string]any) { c["iat"] = json.RawMessage(`18e8`) },
		func(c map[string]any) { c[claimSecurityEpoch] = json.RawMessage(`-0`) },
		func(c map[string]any) { c["sub"] = "" },
		func(c map[string]any) { c["client_id"] = "client\nsecret" },
		func(c map[string]any) { c["jti"] = "token\x7f" },
		func(c map[string]any) { c[claimSubjectKind] = "admin" },
		func(c map[string]any) { c[claimTenantID] = "-tenant" },
		func(c map[string]any) { c[claimProjectID] = 1 },
		func(c map[string]any) { c["scope"] = " agents.get" },
		func(c map[string]any) { c["scope"] = "agents.get  agents.update" },
		func(c map[string]any) { c["scope"] = "agents.get agents.get" },
		func(c map[string]any) { c["scope"] = "agents.get\tagents.update" },
		func(c map[string]any) { c["iss"] = "https://issuer.example/%x" },
	}
	for index, mutate := range faults {
		claims := validClaims()
		mutate(claims)
		if _, err := verifyAccessToken(context, tokenFor(t, key, validHeader(), claims)); errorCategory(err) != errorMalformed {
			t.Fatalf("claim fault %d category=%v", index, err)
		}
	}
	claims := validClaims()
	claims["unknown_fraction"] = json.RawMessage(`-1.5e+2`)
	claims["sub"] = "subject\ncontrol-preserved"
	principal, err := verifyAccessToken(context, tokenFor(t, key, map[string]any{"alg": "RS256", "kid": "key-1", "typ": "APPLICATION/AT+JWT"}, claims))
	if err != nil || principal.tokenType != "at+jwt" || principal.subjectValue != claims["sub"] {
		t.Fatalf("ordinary unknown claim or exact subject/type admission failed: %v", err)
	}
}

func TestTimeHalfOpenBoundaries(t *testing.T) {
	context, _ := contextAndLineage(t, nil, nil)
	key := testPrivateKey(t)
	tests := []struct {
		name  string
		edit  func(map[string]any)
		valid bool
	}{
		{name: "iat skew inclusive", edit: func(c map[string]any) { c["iat"] = testNow + 60; c["exp"] = testNow + 61 }, valid: true},
		{name: "iat beyond skew", edit: func(c map[string]any) { c["iat"] = testNow + 61; c["exp"] = testNow + 62 }},
		{name: "expiry skew open", edit: func(c map[string]any) { c["iat"] = testNow - 100; c["exp"] = testNow - 60 }},
		{name: "expiry before open", edit: func(c map[string]any) { c["iat"] = testNow - 100; c["exp"] = testNow - 59 }, valid: true},
		{name: "lifetime exact", edit: func(c map[string]any) { c["iat"] = testNow - 10; c["exp"] = testNow + 3590 }, valid: true},
		{name: "lifetime over", edit: func(c map[string]any) { c["iat"] = testNow - 10; c["exp"] = testNow + 3591 }},
		{name: "nbf inclusive", edit: func(c map[string]any) { c["nbf"] = testNow + 60 }, valid: true},
		{name: "nbf beyond", edit: func(c map[string]any) { c["nbf"] = testNow + 61 }},
		{name: "iat equals exp", edit: func(c map[string]any) { c["iat"] = testNow; c["exp"] = testNow }},
		{name: "nbf equals exp", edit: func(c map[string]any) { c["nbf"] = testNow + 300 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := validClaims()
			test.edit(claims)
			principal, err := verifyAccessToken(context, tokenFor(t, key, validHeader(), claims))
			if (err == nil) != test.valid || test.valid && principal == nil || !test.valid && errorCategory(err) != errorTimeInvalid {
				t.Fatalf("valid=%v principal=%v err=%v", test.valid, principal != nil, err)
			}
		})
	}
}

func TestSnapshotAndKeyHalfOpenBoundariesAndSingleClockRead(t *testing.T) {
	key := testPrivateKey(t)
	build := func(snapshotNotBefore, snapshotExpires, keyNotBefore, keyNotAfter int64) verificationContext {
		lineage := newTrustLineage()
		if err := lineage.replace(snapshotCandidate{
			issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
			securityEpoch: 7, notBefore: snapshotNotBefore, expiresAt: snapshotExpires,
			keys: []snapshotKeyCandidate{{jwk: jwkFor(t, key, "key-1"), enabled: true, notBefore: keyNotBefore, notAfter: keyNotAfter}},
		}); err != nil {
			t.Fatal(err)
		}
		reads := 0
		return verificationContext{
			lineage: lineage, clock: func() time.Time {
				reads++
				if reads > 1 {
					t.Fatal("verification clock read more than once")
				}
				return time.Unix(testNow, 0)
			},
			targetTenantID: "tenant-1", targetResourceLevel: targetTenant, targetResourceID: "tenant-1", requiredPermission: "agents.get",
		}
	}
	claims := validClaims()
	claims["iat"] = testNow
	claims["exp"] = testNow + 1
	if _, err := verifyAccessToken(build(testNow, testNow+1, testNow, testNow+1), tokenFor(t, key, validHeader(), claims)); err != nil {
		t.Fatalf("inclusive snapshot/key lower bound rejected: %v", err)
	}
	if _, err := verifyAccessToken(build(testNow-1, testNow, testNow-1, testNow+1), tokenFor(t, key, validHeader(), claims)); errorCategory(err) != errorTimeInvalid {
		t.Fatalf("snapshot exclusive upper bound category=%v", err)
	}
	if _, err := verifyAccessToken(build(testNow-1, testNow+1, testNow-1, testNow), tokenFor(t, key, validHeader(), claims)); errorCategory(err) != errorTimeInvalid {
		t.Fatalf("key exclusive issuance upper bound category=%v", err)
	}
	lineage := newTrustLineage()
	if err := lineage.replace(snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1, securityEpoch: 7,
		notBefore: 1, expiresAt: 1 + 86401,
		keys: []snapshotKeyCandidate{{jwk: jwkFor(t, key, "key-1"), enabled: true, notBefore: 1, notAfter: 100000}},
	}); errorCategory(err) != errorInternalFailure {
		t.Fatal("snapshot validity greater than 86400 accepted")
	}
}

func TestRotationInvalidationDrainsAndStalesPrincipal(t *testing.T) {
	context, lineage := contextAndLineage(t, nil, nil)
	principal, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
	if err != nil {
		t.Fatal(err)
	}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- ConsumeVerifiedPrincipal(principal, func(VerifiedPrincipalView) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	replaced := make(chan error, 1)
	go func() {
		previous := currentSnapshotForTest(lineage)
		replaced <- lineage.replace(snapshotCandidate{
			issuer: previous.issuer, audience: previous.audience, generation: 2, previousSnapshotDigest: previous.digest,
			securityEpoch: 8, notBefore: testNow - 100, expiresAt: testNow + 1000,
			keys: []snapshotKeyCandidate{{jwk: jwkFor(t, testPrivateKey(t), "key-1"), enabled: true, notBefore: testNow - 1000, notAfter: testNow + 1000}},
		})
	}()
	select {
	case err := <-replaced:
		t.Fatalf("replacement did not drain callback: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-replaced; err != nil {
		t.Fatal(err)
	}
	stale, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), func() map[string]any { c := validClaims(); c[claimSecurityEpoch] = 8; return c }()))
	if err != nil {
		t.Fatal(err)
	}
	lineage.invalidate()
	if err := ConsumeVerifiedPrincipal(stale, func(VerifiedPrincipalView) error { return nil }); errorCategory(err) != errorInternalFailure {
		t.Fatalf("invalidated generation consumed: %v", err)
	}
}

func TestNestedSecondPrincipalCannotDeadlockConcurrentMutation(t *testing.T) {
	for _, mutationKind := range []string{"replace", "invalidate"} {
		t.Run(mutationKind, func(t *testing.T) {
			context, lineage := contextAndLineage(t, nil, nil)
			first, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), validClaims()))
			if err != nil {
				t.Fatal(err)
			}
			secondClaims := validClaims()
			secondClaims["jti"] = "token-2"
			second, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), secondClaims))
			if err != nil {
				t.Fatal(err)
			}
			previous := currentSnapshotForTest(lineage)
			next := snapshotCandidate{
				issuer: previous.issuer, audience: previous.audience, generation: previous.generation + 1,
				previousSnapshotDigest: previous.digest, securityEpoch: previous.securityEpoch + 1,
				notBefore: testNow - 100, expiresAt: testNow + 1000,
				keys: []snapshotKeyCandidate{{jwk: jwkFor(t, testPrivateKey(t), "key-1"), enabled: true, notBefore: testNow - 1000, notAfter: testNow + 1000}},
			}
			entered := make(chan struct{})
			attemptNested := make(chan struct{})
			nestedResult := make(chan error, 1)
			outerResult := make(chan error, 1)
			go func() {
				outerResult <- ConsumeVerifiedPrincipal(first, func(VerifiedPrincipalView) error {
					close(entered)
					<-attemptNested
					nestedResult <- ConsumeVerifiedPrincipal(second, func(VerifiedPrincipalView) error { return nil })
					return nil
				})
			}()
			<-entered
			mutationResult := make(chan error, 1)
			go func() {
				if mutationKind == "replace" {
					mutationResult <- lineage.replace(next)
					return
				}
				lineage.invalidate()
				mutationResult <- nil
			}()
			deadline := time.Now().Add(time.Second)
			for {
				lineage.state.Lock()
				admitting := lineage.admitting
				lineage.state.Unlock()
				if !admitting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mutation did not close admission")
				}
				time.Sleep(time.Millisecond)
			}
			close(attemptNested)
			select {
			case err := <-nestedResult:
				if errorCategory(err) != errorInternalFailure {
					t.Fatalf("nested consume category=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("nested consume deadlocked behind pending writer")
			}
			if err := <-outerResult; err != nil {
				t.Fatalf("outer consume failed: %v", err)
			}
			select {
			case err := <-mutationResult:
				if err != nil {
					t.Fatalf("mutation failed: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("mutation did not drain and complete")
			}
		})
	}
}

func TestErrorsAreRedactedClosedAndDoNotUnwrap(t *testing.T) {
	for _, category := range generatedIdentityVerifierProfile().errorRules.categories {
		err := verifierError(verifierErrorCategory(category))
		if err.Error() != category || errors.Unwrap(err) != nil || strings.ContainsAny(err.Error(), ".{") {
			t.Fatalf("unsafe error for %q: %#v", category, err)
		}
	}
}

func errorCategory(err error) verifierErrorCategory {
	var typed *verificationError
	if errors.As(err, &typed) {
		return typed.categoryValue()
	}
	return ""
}

func TestPrincipalCanonicalProjectionGolden(t *testing.T) {
	principal := &VerifiedPrincipal{
		profileDigest: "sha256:" + strings.Repeat("1", 64), registryDigest: "sha256:" + strings.Repeat("2", 64),
		snapshotDigest: "sha256:" + strings.Repeat("3", 64), snapshotGeneration: 2,
		tokenInputDigest: "sha256:" + strings.Repeat("4", 64), issuer: "https://issuer.example",
		subjectKind: "user", subjectValue: "subject", audience: "https://api.example", clientID: "client",
		scopes: []string{"agents.get", "agents.update"}, targetTenantID: "tenant-1", targetResourceLevel: targetProject,
		targetResourceID: "project-1", requiredPermission: "agents.get", tokenProjectID: "project-1", hasTokenProject: true,
		securityEpoch: 7, issuedAt: 100, notBefore: 101, hasNotBefore: true, expiresAt: 200,
		tokenID: "token", keyID: "key", tokenType: "at+jwt",
	}
	canonical, canonicalOK := principalCanonical(principal)
	if !canonicalOK {
		t.Fatal("valid principal projection rejected")
	}
	wantCanonical := `{"audience":"https://api.example","clientId":"client","context":{"requiredPermission":"agents.get","targetResourceId":"project-1","targetResourceLevel":"project","targetTenantId":"tenant-1","trustGeneration":2},"issuer":"https://issuer.example","keyId":"key","profileDigest":"sha256:1111111111111111111111111111111111111111111111111111111111111111","registryDigest":"sha256:2222222222222222222222222222222222222222222222222222222222222222","scopes":["agents.get","agents.update"],"securityEpoch":7,"snapshotDigest":"sha256:3333333333333333333333333333333333333333333333333333333333333333","subjectKind":"user","subjectValue":"subject","times":{"expiresAt":200,"issuedAt":100,"notBefore":101},"tokenId":"token","tokenInputDigest":"sha256:4444444444444444444444444444444444444444444444444444444444444444","tokenProjectId":"project-1","tokenType":"at+jwt"}`
	if string(canonical) != wantCanonical {
		t.Fatalf("principal canonical bytes changed:\n got=%s\nwant=%s", canonical, wantCanonical)
	}
	digest := domainDigest("cloud-agents/platform-identity-verifier/verified-principal/v1", canonical)
	if got, want := digest, "sha256:853977fe248a12761d108e4e67beea4c9e86e74b0d27656eabd0ce01d555a6e4"; got != want {
		t.Fatalf("principal canonical digest changed: got %s\ncanonical=%s", got, canonical)
	}
	principal.hasTokenProject = false
	principal.hasNotBefore = false
	withoutOptionalBytes, optionalOK := principalCanonical(principal)
	if !optionalOK {
		t.Fatal("valid absent-optional projection rejected")
	}
	withoutOptional := string(withoutOptionalBytes)
	if strings.Contains(withoutOptional, "tokenProjectId") || strings.Contains(withoutOptional, "notBefore") {
		t.Fatal("absent optional member encoded as present/null")
	}
}

func TestCanonicalJSONStringUsesRFC8785Escapes(t *testing.T) {
	encoded := jsonString("<>&\u2028\u2029\n\x01\"\\")
	if !encoded.valid {
		t.Fatal("valid canonical JSON string rejected")
	}
	got := string(encoded.bytes)
	want := "\"<>&\u2028\u2029\\n\\u0001\\\"\\\\\""
	if got != want {
		t.Fatalf("canonical JSON string changed: got=%q want=%q", got, want)
	}
}

func TestCanonicalJSONStringRejectsInvalidUTF8WithoutReplacementCollision(t *testing.T) {
	replacement := jsonString("\ufffd")
	invalid := jsonString(string([]byte{0xff}))
	if !replacement.valid || invalid.valid {
		t.Fatalf("UTF-8 admission replacement=%v invalid=%v", replacement.valid, invalid.valid)
	}
	principal := &VerifiedPrincipal{
		profileDigest: identityVerifierProfileDigest, registryDigest: identityVerifierRegistryDigest,
		snapshotDigest: "sha256:" + strings.Repeat("a", 64), tokenInputDigest: "sha256:" + strings.Repeat("b", 64),
		subjectValue: "\ufffd", scopes: []string{}, tokenType: "at+jwt",
	}
	legalCanonical, legalOK := principalCanonical(principal)
	if !legalOK || !strings.Contains(string(legalCanonical), "\ufffd") {
		t.Fatal("legal U+FFFD principal projection rejected")
	}
	principal.subjectValue = string([]byte{0xff})
	if invalidCanonical, invalidOK := principalCanonical(principal); invalidOK || invalidCanonical != nil {
		t.Fatal("invalid UTF-8 principal projection collided with legal replacement rune")
	}
	context, _ := contextAndLineage(t, nil, nil)
	claims := validClaims()
	claims["sub"] = "\ufffd"
	verified, err := verifyAccessToken(context, tokenFor(t, testPrivateKey(t), validHeader(), claims))
	if err != nil {
		t.Fatalf("legal U+FFFD token failed: %v", err)
	}
	verified.subjectValue = string([]byte{0xff})
	if errorCategory(ConsumeVerifiedPrincipal(verified, func(VerifiedPrincipalView) error { return nil })) != errorInternalFailure {
		t.Fatal("invalid UTF-8 tamper reused legal U+FFFD self-binding")
	}
}
