package authn

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var ErrInvalidConfiguredVerifier = errors.New("configured access-token verifier is invalid")

// ConfiguredVerifierConfig is the startup trust input for a Control Plane
// resource server. Keys are public JWKs; private material is rejected by the
// existing closed v1 parser. Refresh is explicit by constructing a new
// verifier, so a file or network watcher cannot silently change authority.
type ConfiguredVerifierConfig struct {
	Issuer        string
	Audience      string
	Generation    int64
	SecurityEpoch int64
	NotBefore     int64
	ExpiresAt     int64
	Keys          []ConfiguredVerifierKey
	Clock         func() time.Time
}

type ConfiguredVerifierKey struct {
	JWK       json.RawMessage
	Enabled   bool
	NotBefore int64
	NotAfter  int64
}

// ConfiguredVerifier verifies access tokens against one immutable startup
// snapshot. It is safe for concurrent requests and fails closed on Invalidate.
type ConfiguredVerifier struct {
	mu          sync.RWMutex
	lineage     *trustLineage
	clock       func() time.Time
	invalidated bool
}

func NewConfiguredVerifier(config ConfiguredVerifierConfig) (*ConfiguredVerifier, error) {
	if config.Clock == nil || config.Issuer == "" || config.Audience == "" || config.Generation < 1 || config.SecurityEpoch < 1 || config.ExpiresAt <= config.NotBefore || len(config.Keys) == 0 {
		return nil, ErrInvalidConfiguredVerifier
	}
	keys := make([]snapshotKeyCandidate, len(config.Keys))
	for index, key := range config.Keys {
		if len(key.JWK) == 0 {
			return nil, ErrInvalidConfiguredVerifier
		}
		keys[index] = snapshotKeyCandidate{jwk: append([]byte(nil), key.JWK...), enabled: key.Enabled, notBefore: key.NotBefore, notAfter: key.NotAfter}
	}
	lineage := newTrustLineage()
	if err := lineage.replace(snapshotCandidate{
		issuer: config.Issuer, audience: config.Audience, generation: config.Generation,
		securityEpoch: config.SecurityEpoch, notBefore: config.NotBefore, expiresAt: config.ExpiresAt, keys: keys,
	}); err != nil {
		return nil, ErrInvalidConfiguredVerifier
	}
	return &ConfiguredVerifier{lineage: lineage, clock: config.Clock}, nil
}

func (verifier *ConfiguredVerifier) Verify(token string, request VerificationRequest) (*VerifiedPrincipal, error) {
	if verifier == nil {
		return nil, verifierError(errorInternalFailure)
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	if verifier.invalidated || verifier.lineage == nil || verifier.clock == nil {
		return nil, verifierError(errorInternalFailure)
	}
	level, ok := targetResourceLevelForString(request.ResourceLevel)
	if !ok {
		return nil, verifierError(errorInternalFailure)
	}
	return verifyAccessToken(verificationContext{
		lineage: verifier.lineage, clock: verifier.clock, targetTenantID: request.TenantID,
		targetResourceLevel: level, targetResourceID: request.ResourceID, requiredPermission: request.RequiredPermission,
	}, token)
}

func (verifier *ConfiguredVerifier) Invalidate() {
	if verifier == nil {
		return
	}
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	if verifier.invalidated {
		return
	}
	verifier.invalidated = true
	if verifier.lineage != nil {
		verifier.lineage.invalidate()
	}
}

type VerificationRequest struct {
	TenantID           string
	ResourceLevel      string
	ResourceID         string
	RequiredPermission string
}

func targetResourceLevelForString(value string) (targetResourceLevel, bool) {
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
