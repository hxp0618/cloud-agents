package authn

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var ErrInvalidConfiguredVerifier = errors.New("configured access-token verifier is invalid")

// Pair reloads share an order so reversed callers cannot deadlock on the two verifiers.
var configuredPairReload sync.Mutex

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
	if !validConfiguredVerifierConfig(config, true) {
		return nil, ErrInvalidConfiguredVerifier
	}
	lineage := &trustLineage{}
	if err := lineage.replace(config.snapshotCandidate()); err != nil {
		return nil, ErrInvalidConfiguredVerifier
	}
	return &ConfiguredVerifier{lineage: lineage, clock: config.Clock}, nil
}

// Reload replaces the active trust snapshot only after the complete candidate
// is valid. Generations are explicit so a SIGHUP cannot silently roll trust
// backwards or change an existing key's material under the same kid.
func (verifier *ConfiguredVerifier) Reload(config ConfiguredVerifierConfig) error {
	if verifier == nil || !validConfiguredVerifierConfig(config, false) {
		return ErrInvalidConfiguredVerifier
	}
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	if verifier.invalidated || verifier.lineage == nil || verifier.clock == nil || config.Clock == nil {
		return ErrInvalidConfiguredVerifier
	}
	previous := currentSnapshot(verifier.lineage)
	if previous == nil || config.Issuer != previous.issuer {
		return ErrInvalidConfiguredVerifier
	}
	candidate := config.snapshotCandidate()
	candidate.previousSnapshotDigest = previous.digest
	if err := verifier.lineage.replace(candidate); err != nil {
		return ErrInvalidConfiguredVerifier
	}
	verifier.clock = config.Clock
	return nil
}

// ReloadTogether validates both audience snapshots before publishing either.
// Existing operations settle under their generation leases before replacement.
func (verifier *ConfiguredVerifier) ReloadTogether(config ConfiguredVerifierConfig, other *ConfiguredVerifier, otherConfig ConfiguredVerifierConfig) error {
	if verifier == nil || other == nil || verifier == other ||
		!validConfiguredVerifierConfig(config, false) || !validConfiguredVerifierConfig(otherConfig, false) ||
		config.Issuer != otherConfig.Issuer || config.Audience == otherConfig.Audience ||
		config.Generation != otherConfig.Generation || config.SecurityEpoch != otherConfig.SecurityEpoch ||
		config.NotBefore != otherConfig.NotBefore || config.ExpiresAt != otherConfig.ExpiresAt {
		return ErrInvalidConfiguredVerifier
	}
	configuredPairReload.Lock()
	defer configuredPairReload.Unlock()
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	other.mu.Lock()
	defer other.mu.Unlock()
	if verifier.invalidated || other.invalidated || verifier.lineage == nil || other.lineage == nil {
		return ErrInvalidConfiguredVerifier
	}
	verifier.lineage.mutation.Lock()
	defer verifier.lineage.mutation.Unlock()
	other.lineage.mutation.Lock()
	defer other.lineage.mutation.Unlock()
	left, leftHistory, err := verifier.prepareReload(config)
	if err != nil {
		return ErrInvalidConfiguredVerifier
	}
	right, rightHistory, err := other.prepareReload(otherConfig)
	if err != nil || !equalTrustHistory(leftHistory, rightHistory) || len(left.keys) != len(right.keys) {
		return ErrInvalidConfiguredVerifier
	}
	for kid, key := range left.keys {
		peer, ok := right.keys[kid]
		if !ok || !equalBytes(key.modulus, peer.modulus) || key.enabled != peer.enabled ||
			key.notBefore != peer.notBefore || key.notAfter != peer.notAfter {
			return ErrInvalidConfiguredVerifier
		}
	}
	publishTrustPair(verifier.lineage, left, leftHistory, other.lineage, right, rightHistory)
	verifier.clock, other.clock = config.Clock, otherConfig.Clock
	return nil
}

func equalTrustHistory(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for kid, modulus := range left {
		peer, ok := right[kid]
		if !ok || !equalBytes(modulus, peer) {
			return false
		}
	}
	return true
}

func (verifier *ConfiguredVerifier) prepareReload(config ConfiguredVerifierConfig) (*trustSnapshot, map[string][]byte, error) {
	verifier.lineage.state.Lock()
	defer verifier.lineage.state.Unlock()
	if verifier.clock == nil || verifier.lineage.current == nil || verifier.lineage.current.snapshot == nil {
		return nil, nil, ErrInvalidConfiguredVerifier
	}
	previous := verifier.lineage.current.snapshot
	if config.Audience != previous.audience {
		return nil, nil, ErrInvalidConfiguredVerifier
	}
	candidate := config.snapshotCandidate()
	candidate.previousSnapshotDigest = previous.digest
	return buildTrustSnapshot(candidate, previous, verifier.lineage.history)
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

// Ready reports whether the active trust snapshot is within its configured
// validity window at the current verifier time.
func (verifier *ConfiguredVerifier) Ready() bool {
	if verifier == nil {
		return false
	}
	verifier.mu.RLock()
	defer verifier.mu.RUnlock()
	if verifier.invalidated || verifier.lineage == nil || verifier.clock == nil {
		return false
	}
	profile := generatedIdentityVerifierProfile()
	return profile.valid() && validSnapshotAt(currentSnapshot(verifier.lineage), verifier.clock().UTC().Unix(), profile)
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

// InvalidateTogether permanently closes both audience verifiers. Both stop
// admitting new generation leases before either waits for existing leases to
// settle, so one audience cannot remain usable during shutdown of its peer.
func (verifier *ConfiguredVerifier) InvalidateTogether(other *ConfiguredVerifier) {
	if verifier == nil {
		if other != nil {
			other.Invalidate()
		}
		return
	}
	if other == nil || verifier == other {
		verifier.Invalidate()
		return
	}
	configuredPairReload.Lock()
	defer configuredPairReload.Unlock()
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	other.mu.Lock()
	defer other.mu.Unlock()
	verifier.invalidated = true
	other.invalidated = true
	invalidateTrustPair(verifier.lineage, other.lineage)
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

func validConfiguredVerifierConfig(config ConfiguredVerifierConfig, initial bool) bool {
	return config.Clock != nil && config.Issuer != "" && config.Audience != "" &&
		((initial && config.Generation > 0) || (!initial && config.Generation > 1)) &&
		config.SecurityEpoch >= 1 && config.ExpiresAt > config.NotBefore && len(config.Keys) > 0
}

func (config ConfiguredVerifierConfig) snapshotCandidate() snapshotCandidate {
	keys := make([]snapshotKeyCandidate, len(config.Keys))
	for index, key := range config.Keys {
		keys[index] = snapshotKeyCandidate{jwk: append([]byte(nil), key.JWK...), enabled: key.Enabled, notBefore: key.NotBefore, notAfter: key.NotAfter}
	}
	return snapshotCandidate{
		issuer: config.Issuer, audience: config.Audience, generation: config.Generation,
		securityEpoch: config.SecurityEpoch, notBefore: config.NotBefore, expiresAt: config.ExpiresAt, keys: keys,
	}
}

func currentSnapshot(lineage *trustLineage) *trustSnapshot {
	if lineage == nil {
		return nil
	}
	lineage.state.Lock()
	defer lineage.state.Unlock()
	if lineage.current == nil || lineage.current.snapshot == nil {
		return nil
	}
	return lineage.current.snapshot
}
