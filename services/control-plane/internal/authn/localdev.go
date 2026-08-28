//go:build localdev

package authn

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"
)

const (
	localIssuer     = "https://local.invalid/cloud-agents/authn"
	localAudience   = "https://local.invalid/cloud-agents/control-plane"
	localKeyID      = "local-ephemeral-rs256"
	localClientID   = "local-control-plane"
	localPermission = "projects.create projects.get"
	localTokenTTL   = 5 * time.Minute
)

// LocalVerifierConfig configures the explicitly local-only verifier. The
// constructor always creates a fresh ephemeral signing key and trust lineage.
type LocalVerifierConfig struct {
	Clock func() time.Time
}

// LocalTokenClaims are the only caller-controlled claims admitted by the local
// issuer. Tokens are always tenant scoped and grant only projects.create.
type LocalTokenClaims struct {
	TenantID string
	Subject  string
}

// LocalVerificationRequest binds verification to the resource authority
// selected by the local HTTP transport.
type LocalVerificationRequest struct {
	TenantID           string
	ResourceLevel      string
	ResourceID         string
	RequiredPermission string
}

// LocalVerifier is available only in localdev builds. It owns an ephemeral
// private key; callers can neither extract that key nor construct principals.
type LocalVerifier struct {
	mu          sync.RWMutex
	lineage     *trustLineage
	privateKey  *rsa.PrivateKey
	clock       func() time.Time
	invalidated bool
}

// NewLocalVerifier creates one immutable in-memory trust snapshot around a
// newly generated RSA-2048 key. It performs no OIDC, JWKS, or network access.
func NewLocalVerifier(config LocalVerifierConfig) (*LocalVerifier, error) {
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, verifierError(errorInternalFailure)
	}
	now := clock().UTC().Unix()
	profile := generatedIdentityVerifierProfile()
	window := int64(profile.limits.trustSnapshotValiditySeconds)
	if !profile.valid() || window <= int64(profile.limits.clockSkewSeconds) {
		return nil, verifierError(errorInternalFailure)
	}
	notBefore := now - int64(profile.limits.clockSkewSeconds)
	expiresAt := notBefore + window
	jwk, ok := localPublicJWK(&key.PublicKey)
	if !ok {
		return nil, verifierError(errorInternalFailure)
	}
	lineage := &trustLineage{}
	if err := lineage.replace(snapshotCandidate{
		issuer:        localIssuer,
		audience:      localAudience,
		generation:    1,
		securityEpoch: 1,
		notBefore:     notBefore,
		expiresAt:     expiresAt,
		keys: []snapshotKeyCandidate{{
			jwk:       jwk,
			enabled:   true,
			notBefore: notBefore,
			notAfter:  expiresAt,
		}},
	}); err != nil {
		return nil, verifierError(errorInternalFailure)
	}
	return &LocalVerifier{lineage: lineage, privateKey: key, clock: clock}, nil
}

// IssueToken signs a short-lived local bearer. No key, token input, or signing
// failure detail is retained in returned errors.
func (verifier *LocalVerifier) IssueToken(claims LocalTokenClaims) (string, error) {
	if verifier == nil {
		return "", verifierError(errorInternalFailure)
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	if verifier.invalidated || verifier.privateKey == nil || verifier.lineage == nil || verifier.clock == nil {
		return "", verifierError(errorInternalFailure)
	}
	profile := generatedIdentityVerifierProfile()
	if !validOpaqueIdentifier(claims.TenantID, int(profile.limits.opaqueIdentifierBytes)) ||
		!validExactString(claims.Subject, int(profile.limits.subjectScalars), true) {
		return "", verifierError(errorMalformed)
	}
	tokenIDBytes := make([]byte, 18)
	if _, err := rand.Read(tokenIDBytes); err != nil {
		return "", verifierError(errorInternalFailure)
	}
	now := verifier.clock().UTC().Unix()
	header := map[string]any{"alg": "RS256", "kid": localKeyID, "typ": "at+jwt"}
	payload := map[string]any{
		"iss":              localIssuer,
		"sub":              claims.Subject,
		"aud":              localAudience,
		"exp":              now + int64(localTokenTTL/time.Second),
		"iat":              now,
		"jti":              base64.RawURLEncoding.EncodeToString(tokenIDBytes),
		"client_id":        localClientID,
		"scope":            localPermission,
		claimSubjectKind:   "user",
		claimTenantID:      claims.TenantID,
		claimSecurityEpoch: int64(1),
		claimTokenProfile:  profile.claims.tokenProfileValue,
	}
	protected, err := json.Marshal(header)
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	claimsJSON, err := json.Marshal(payload)
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(protected)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	input := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, verifier.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", verifierError(errorInternalFailure)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// Verify delegates all token and resource checks to the closed offline verifier
// and returns only its opaque, one-shot principal.
func (verifier *LocalVerifier) Verify(token string, request LocalVerificationRequest) (*VerifiedPrincipal, error) {
	if verifier == nil {
		return nil, verifierError(errorInternalFailure)
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	if verifier.invalidated || verifier.lineage == nil || verifier.clock == nil {
		return nil, verifierError(errorInternalFailure)
	}
	level, ok := localResourceLevel(request.ResourceLevel)
	if !ok {
		return nil, verifierError(errorInternalFailure)
	}
	return verifyAccessToken(verificationContext{
		lineage:             verifier.lineage,
		clock:               verifier.clock,
		targetTenantID:      request.TenantID,
		targetResourceLevel: level,
		targetResourceID:    request.ResourceID,
		requiredPermission:  request.RequiredPermission,
	}, token)
}

// Invalidate permanently closes this verifier, drops its signing-key reference,
// and invalidates every not-yet-consumed principal from its lineage.
func (verifier *LocalVerifier) Invalidate() {
	if verifier == nil {
		return
	}
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	if verifier.invalidated {
		return
	}
	verifier.invalidated = true
	verifier.privateKey = nil
	if verifier.lineage != nil {
		verifier.lineage.invalidate()
	}
}

func localResourceLevel(value string) (targetResourceLevel, bool) {
	switch value {
	case "tenant":
		return targetTenant, true
	case "organization":
		return targetOrganization, true
	case "project":
		return targetProject, true
	default:
		return "", false
	}
}

func localPublicJWK(key *rsa.PublicKey) ([]byte, bool) {
	if key == nil || key.E != 65537 || key.N == nil {
		return nil, false
	}
	encoded, err := json.Marshal(map[string]any{
		"alg":     "RS256",
		"e":       "AQAB",
		"key_ops": []string{"verify"},
		"kid":     localKeyID,
		"kty":     "RSA",
		"n":       base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"use":     "sig",
	})
	return encoded, err == nil
}
