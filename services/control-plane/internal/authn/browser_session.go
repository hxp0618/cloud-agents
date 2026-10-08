package authn

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	browserSessionIssuer   = "https://cloud-agents.invalid/accounts"
	browserSessionAudience = "https://cloud-agents.invalid/control-plane"
	browserSessionClientID = "admin-browser"
	browserSessionTokenTTL = time.Minute
	browserSessionKeyTTL   = 24 * time.Hour
	browserSessionRotateBy = 10 * time.Minute
)

// BrowserSessionTokenAuthority mints short-lived, tenant-scoped tokens for a
// live browser session. It never exposes signing keys or constructs principals.
type BrowserSessionTokenAuthority struct {
	mu         sync.RWMutex
	clock      func() time.Time
	privateKey *rsa.PrivateKey
	kid        string
	expiresAt  time.Time
	verifier   *ConfiguredVerifier
	closed     bool
}

func NewBrowserSessionTokenAuthority(clock func() time.Time) (*BrowserSessionTokenAuthority, error) {
	if clock == nil {
		clock = time.Now
	}
	authority := &BrowserSessionTokenAuthority{clock: clock}
	authority.mu.Lock()
	err := authority.rotateLocked()
	authority.mu.Unlock()
	if err != nil {
		return nil, ErrInvalidConfiguredVerifier
	}
	return authority, nil
}

func (authority *BrowserSessionTokenAuthority) IssueToken(accountID, tenantID string, permissions []string) (string, error) {
	if authority == nil {
		return "", ErrInvalidConfiguredVerifier
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed || authority.clock == nil {
		return "", ErrInvalidConfiguredVerifier
	}
	now := authority.clock().UTC()
	if authority.privateKey == nil || !now.Add(browserSessionRotateBy).Before(authority.expiresAt) {
		if err := authority.rotateLocked(); err != nil {
			return "", ErrInvalidConfiguredVerifier
		}
		now = authority.clock().UTC()
	}
	profile := generatedIdentityVerifierProfile()
	if !validOpaqueIdentifier(accountID, int(profile.limits.subjectScalars)) ||
		!validOpaqueIdentifier(tenantID, int(profile.limits.opaqueIdentifierBytes)) {
		return "", verifierError(errorMalformed)
	}
	permissions = append([]string(nil), permissions...)
	sort.Strings(permissions)
	for index, permission := range permissions {
		if index > 0 && permission == permissions[index-1] {
			return "", verifierError(errorMalformed)
		}
	}
	if _, ok := parseScopes(strings.Join(permissions, " "), int(profile.limits.scopes), int(profile.limits.scopeItemBytesMin), int(profile.limits.scopeItemBytesMax)); !ok {
		return "", verifierError(errorMalformed)
	}
	tokenID := make([]byte, 18)
	if _, err := rand.Read(tokenID); err != nil {
		return "", verifierError(errorInternalFailure)
	}
	nowSecond := now.Unix()
	header := map[string]any{"alg": "RS256", "kid": authority.kid, "typ": "at+jwt"}
	payload := map[string]any{
		"iss": browserSessionIssuer, "sub": accountID, "aud": browserSessionAudience,
		"exp": nowSecond + int64(browserSessionTokenTTL/time.Second), "iat": nowSecond,
		"jti": base64.RawURLEncoding.EncodeToString(tokenID), "client_id": browserSessionClientID,
		"scope": strings.Join(permissions, " "), claimSubjectKind: "user", claimTenantID: tenantID,
		claimSecurityEpoch: int64(1), claimTokenProfile: profile.claims.tokenProfileValue,
	}
	protected, err := json.Marshal(header)
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	claims, err := json.Marshal(payload)
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(protected)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claims)
	input := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, authority.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (authority *BrowserSessionTokenAuthority) Verify(token string, request VerificationRequest) (*VerifiedPrincipal, error) {
	if authority == nil {
		return nil, verifierError(errorInternalFailure)
	}
	authority.mu.RLock()
	defer authority.mu.RUnlock()
	if authority.closed || authority.verifier == nil {
		return nil, verifierError(errorInternalFailure)
	}
	return authority.verifier.Verify(token, request)
}

func (authority *BrowserSessionTokenAuthority) Invalidate() {
	if authority == nil {
		return
	}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.closed {
		return
	}
	authority.closed = true
	authority.privateKey = nil
	if authority.verifier != nil {
		authority.verifier.Invalidate()
	}
	authority.verifier = nil
}

func (authority *BrowserSessionTokenAuthority) rotateLocked() error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	kidBytes := make([]byte, 12)
	if _, err := rand.Read(kidBytes); err != nil {
		return err
	}
	kid := "browser-" + base64.RawURLEncoding.EncodeToString(kidBytes)
	now := authority.clock().UTC()
	notBefore := now.Add(-time.Minute)
	expiresAt := now.Add(browserSessionKeyTTL - time.Minute)
	jwk, err := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": kid,
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "use": "sig",
	})
	if err != nil {
		return err
	}
	verifier, err := NewConfiguredVerifier(ConfiguredVerifierConfig{
		Issuer: browserSessionIssuer, Audience: browserSessionAudience, Generation: 1, SecurityEpoch: 1,
		NotBefore: notBefore.Unix(), ExpiresAt: expiresAt.Unix(), Clock: authority.clock,
		Keys: []ConfiguredVerifierKey{{JWK: jwk, Enabled: true, NotBefore: notBefore.Unix(), NotAfter: expiresAt.Unix()}},
	})
	if err != nil {
		return err
	}
	old := authority.verifier
	authority.privateKey = key
	authority.kid = kid
	authority.expiresAt = expiresAt
	authority.verifier = verifier
	if old != nil {
		old.Invalidate()
	}
	return nil
}
