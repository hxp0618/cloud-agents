package identitytrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/hxp0618/cloud-agents/sdk/go/gen/openapi/v1alpha1"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
)

const (
	maximumSafeInteger       = int64(9007199254740991)
	maximumNumericDate       = int64(253402300799)
	maximumSnapshotLifetime  = int64(86400)
	maximumLifetimeKeyCount  = 32
	maximumAuthorityDocument = 1 << 20
)

var (
	ErrInvalidConfig        = errors.New("identity trust configuration is invalid")
	ErrInvalidAuthority     = errors.New("identity trust authority is invalid")
	ErrCheckpoint           = errors.New("identity trust checkpoint failed")
	ErrTransportUnavailable = errors.New("identity trust transport is unavailable")
)

// FetchOperation obtains the exact configured authority document. The production
// HTTPS implementation is supplied by the caller; token input never selects it.
// Only errors wrapping ErrTransportUnavailable permit startup from a checkpoint.
type FetchOperation func(context.Context, string) ([]byte, error)

// Checkpoint persists one bounded current authority state. Implementations must
// atomically replace expectedDigest (empty means absent) or return an error.
type Checkpoint interface {
	Load(context.Context, string) (State, bool, error)
	CompareAndSwap(context.Context, string, string, State) error
}

type Config struct {
	Issuer        string
	UserAudience  string
	AdminAudience string
	JWKSURL       string
	Clock         func() time.Time
	Checkpoint    Checkpoint
	Fetch         FetchOperation
}

// State is the single durable checkpoint. Lineage is cumulative and bounded;
// it is not a refresh journal.
type State struct {
	Issuer          string       `json:"issuer"`
	Revision        int64        `json:"revision"`
	SecurityEpoch   int64        `json:"securityEpoch"`
	NotBefore       int64        `json:"notBefore"`
	ExpiresAt       int64        `json:"expiresAt"`
	AuthorityDigest string       `json:"authorityDigest"`
	Lineage         []LineageKey `json:"lineage"`
}

type LineageKey struct {
	JWK       api.IdentityJWK `json:"jwk"`
	Enabled   bool            `json:"enabled"`
	NotBefore int64           `json:"notBefore"`
	NotAfter  int64           `json:"notAfter"`
}

type ConfigPair struct {
	User            authn.ConfiguredVerifierConfig
	Admin           authn.ConfiguredVerifierConfig
	AuthorityDigest string
	Revision        int64
}

type Adapter struct {
	mu         sync.Mutex
	config     Config
	started    bool
	generation int64
	state      State
	pair       ConfigPair
}

func New(config Config) (*Adapter, error) {
	parsed, err := url.Parse(config.JWKSURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		strings.TrimSpace(config.JWKSURL) != config.JWKSURL || config.Issuer == "" || config.UserAudience == "" ||
		config.AdminAudience == "" || config.UserAudience == config.AdminAudience || config.Clock == nil ||
		config.Checkpoint == nil || config.Fetch == nil {
		return nil, ErrInvalidConfig
	}
	return &Adapter{config: config}, nil
}

// Start loads the durable checkpoint, accepts a newer fetched authority when
// available, and creates a new process-local v1 lineage at generation one.
func (adapter *Adapter) Start(ctx context.Context) (ConfigPair, error) {
	if adapter == nil || ctx == nil {
		return ConfigPair{}, ErrInvalidConfig
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.started {
		return ConfigPair{}, ErrInvalidConfig
	}

	stored, exists, err := adapter.config.Checkpoint.Load(ctx, adapter.config.Issuer)
	if err != nil {
		return ConfigPair{}, ErrCheckpoint
	}
	if exists {
		stored, err = normalizeStoredState(stored, adapter.config.Issuer)
		if err != nil {
			return ConfigPair{}, err
		}
	}

	accepted := stored
	raw, fetchErr := adapter.config.Fetch(ctx, adapter.config.JWKSURL)
	if fetchErr == nil {
		candidate, decodeErr := decodeAuthority(raw, adapter.config.Issuer, adapter.config.Clock().UTC().Unix())
		if decodeErr != nil {
			return ConfigPair{}, decodeErr
		}
		changed, transitionErr := validateTransition(stored, exists, candidate)
		if transitionErr != nil {
			return ConfigPair{}, transitionErr
		}
		if changed {
			pair, pairErr := adapter.configPair(candidate, 1)
			if pairErr != nil {
				return ConfigPair{}, pairErr
			}
			accepted, err = adapter.persistCandidate(ctx, stored, exists, candidate)
			if err != nil {
				return ConfigPair{}, err
			}
			if accepted.AuthorityDigest != candidate.AuthorityDigest {
				pair, pairErr = adapter.configPair(accepted, 1)
				if pairErr != nil {
					return ConfigPair{}, pairErr
				}
			}
			adapter.pair = pair
		}
	} else if !exists || !errors.Is(fetchErr, ErrTransportUnavailable) || ctx.Err() != nil {
		return ConfigPair{}, ErrInvalidAuthority
	}
	if accepted.Issuer == "" || !validAt(accepted, adapter.config.Clock().UTC().Unix()) {
		return ConfigPair{}, ErrInvalidAuthority
	}
	if adapter.pair.User.Issuer == "" {
		adapter.pair, err = adapter.configPair(accepted, 1)
		if err != nil {
			return ConfigPair{}, err
		}
	}
	adapter.state = cloneState(accepted)
	adapter.generation = 1
	adapter.started = true
	return clonePair(adapter.pair), nil
}

// Refresh accepts at most one newly observed publisher state and maps it to the
// next process-local v1 generation. A skipped publisher revision needs no journal
// because every authority document carries the complete permanent lineage.
func (adapter *Adapter) Refresh(ctx context.Context) (ConfigPair, bool, error) {
	if adapter == nil || ctx == nil {
		return ConfigPair{}, false, ErrInvalidConfig
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if !adapter.started || adapter.generation >= maximumSafeInteger {
		return ConfigPair{}, false, ErrInvalidConfig
	}
	raw, err := adapter.config.Fetch(ctx, adapter.config.JWKSURL)
	if err != nil {
		if ctx.Err() != nil {
			return ConfigPair{}, false, ctx.Err()
		}
		if errors.Is(err, ErrTransportUnavailable) {
			return ConfigPair{}, false, ErrTransportUnavailable
		}
		return ConfigPair{}, false, ErrInvalidAuthority
	}
	candidate, err := decodeAuthority(raw, adapter.config.Issuer, adapter.config.Clock().UTC().Unix())
	if err != nil {
		return ConfigPair{}, false, err
	}
	changed, err := validateTransition(adapter.state, true, candidate)
	if err != nil {
		return ConfigPair{}, false, err
	}
	if !changed {
		return clonePair(adapter.pair), false, nil
	}
	nextGeneration := adapter.generation + 1
	pair, err := adapter.configPair(candidate, nextGeneration)
	if err != nil {
		return ConfigPair{}, false, err
	}
	accepted, err := adapter.persistCandidate(ctx, adapter.state, true, candidate)
	if err != nil {
		return ConfigPair{}, false, err
	}
	if accepted.AuthorityDigest != candidate.AuthorityDigest {
		pair, err = adapter.configPair(accepted, nextGeneration)
		if err != nil {
			return ConfigPair{}, false, err
		}
	}
	adapter.state = cloneState(accepted)
	adapter.generation = nextGeneration
	adapter.pair = pair
	return clonePair(pair), true, nil
}

func (adapter *Adapter) persistCandidate(ctx context.Context, previous State, exists bool, candidate State) (State, error) {
	expected := ""
	if exists {
		expected = previous.AuthorityDigest
	}
	if err := adapter.config.Checkpoint.CompareAndSwap(ctx, adapter.config.Issuer, expected, candidate); err == nil {
		return candidate, nil
	}
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	winner, winnerExists, err := adapter.config.Checkpoint.Load(ctx, adapter.config.Issuer)
	if err != nil && ctx.Err() != nil {
		return State{}, ctx.Err()
	}
	if err != nil || !winnerExists {
		return State{}, ErrCheckpoint
	}
	winner, err = normalizeStoredState(winner, adapter.config.Issuer)
	if err != nil || !validAt(winner, adapter.config.Clock().UTC().Unix()) {
		return State{}, ErrInvalidAuthority
	}
	if _, err := validateTransition(previous, exists, winner); err != nil {
		return State{}, err
	}
	if winner.AuthorityDigest == candidate.AuthorityDigest {
		return winner, nil
	}
	if winner.Revision > candidate.Revision {
		changed, err := validateTransition(candidate, true, winner)
		if err != nil || !changed {
			return State{}, ErrInvalidAuthority
		}
		return winner, nil
	}
	if winner.Revision < candidate.Revision {
		changed, err := validateTransition(winner, true, candidate)
		if err != nil || !changed {
			return State{}, ErrInvalidAuthority
		}
		if err := adapter.config.Checkpoint.CompareAndSwap(ctx, adapter.config.Issuer, winner.AuthorityDigest, candidate); err != nil {
			return State{}, ErrCheckpoint
		}
		return candidate, nil
	}
	return State{}, ErrInvalidAuthority
}

func (adapter *Adapter) configPair(state State, generation int64) (ConfigPair, error) {
	keys := make([]authn.ConfiguredVerifierKey, len(state.Lineage))
	for index, key := range state.Lineage {
		raw, err := json.Marshal(key.JWK)
		if err != nil {
			return ConfigPair{}, ErrInvalidAuthority
		}
		keys[index] = authn.ConfiguredVerifierKey{
			JWK: append([]byte(nil), raw...), Enabled: key.Enabled,
			NotBefore: key.NotBefore, NotAfter: key.NotAfter,
		}
	}
	user := authn.ConfiguredVerifierConfig{
		Issuer: state.Issuer, Audience: adapter.config.UserAudience, Generation: generation,
		SecurityEpoch: state.SecurityEpoch, NotBefore: state.NotBefore, ExpiresAt: state.ExpiresAt,
		Keys: keys, Clock: adapter.config.Clock,
	}
	admin := user
	admin.Audience = adapter.config.AdminAudience
	admin.Keys = cloneConfiguredKeys(keys)
	user.Keys = cloneConfiguredKeys(keys)
	userVerifier, err := authn.NewConfiguredVerifier(user)
	if err != nil {
		return ConfigPair{}, ErrInvalidAuthority
	}
	adminVerifier, err := authn.NewConfiguredVerifier(admin)
	if err != nil {
		userVerifier.Invalidate()
		return ConfigPair{}, ErrInvalidAuthority
	}
	userVerifier.Invalidate()
	adminVerifier.Invalidate()
	return ConfigPair{User: user, Admin: admin, AuthorityDigest: state.AuthorityDigest, Revision: state.Revision}, nil
}

func decodeAuthority(raw []byte, pinnedIssuer string, now int64) (State, error) {
	if len(raw) == 0 || len(raw) > maximumAuthorityDocument {
		return State{}, ErrInvalidAuthority
	}
	document, err := api.DecodeIdentityJWKSJSON(raw)
	if err != nil || document.CloudAgentsAuthority.Issuer != pinnedIssuer {
		return State{}, ErrInvalidAuthority
	}
	revision, ok := parseSafePositiveDecimal(document.CloudAgentsAuthority.Revision)
	if !ok {
		return State{}, ErrInvalidAuthority
	}
	epoch, ok := parseSafePositiveDecimal(document.CloudAgentsAuthority.SecurityEpoch)
	if !ok {
		return State{}, ErrInvalidAuthority
	}
	if len(document.CloudAgentsAuthority.Lineage) < 1 || len(document.CloudAgentsAuthority.Lineage) > maximumLifetimeKeyCount || len(document.Keys) > maximumLifetimeKeyCount {
		return State{}, ErrInvalidAuthority
	}
	state := State{
		Issuer: pinnedIssuer, Revision: revision, SecurityEpoch: epoch,
		NotBefore: document.CloudAgentsAuthority.NotBefore, ExpiresAt: document.CloudAgentsAuthority.ExpiresAt,
		Lineage: make([]LineageKey, len(document.CloudAgentsAuthority.Lineage)),
	}
	lineage := make(map[string]LineageKey, len(state.Lineage))
	for index, source := range document.CloudAgentsAuthority.Lineage {
		key := LineageKey{JWK: cloneJWK(source.JWK), Enabled: source.Enabled, NotBefore: source.NotBefore, NotAfter: source.NotAfter}
		if _, duplicate := lineage[key.JWK.Kid]; duplicate || key.JWK.Kid == "" || !validKeyInterval(key) {
			return State{}, ErrInvalidAuthority
		}
		lineage[key.JWK.Kid] = key
		state.Lineage[index] = key
	}
	active := make(map[string]api.IdentityJWK, len(document.Keys))
	for _, source := range document.Keys {
		if source.Kid == "" {
			return State{}, ErrInvalidAuthority
		}
		if _, duplicate := active[source.Kid]; duplicate {
			return State{}, ErrInvalidAuthority
		}
		active[source.Kid] = cloneJWK(source)
	}
	for kid, key := range lineage {
		projected, present := active[kid]
		if key.Enabled != present || present && !equalJWK(key.JWK, projected) {
			return State{}, ErrInvalidAuthority
		}
		delete(active, kid)
	}
	if len(active) != 0 {
		return State{}, ErrInvalidAuthority
	}
	sort.Slice(state.Lineage, func(left, right int) bool { return state.Lineage[left].JWK.Kid < state.Lineage[right].JWK.Kid })
	digest, err := authorityDigest(state)
	if err != nil {
		return State{}, ErrInvalidAuthority
	}
	state.AuthorityDigest = digest
	if !validAt(state, now) {
		return State{}, ErrInvalidAuthority
	}
	return state, nil
}

func normalizeStoredState(state State, pinnedIssuer string) (State, error) {
	state = cloneState(state)
	if state.Issuer != pinnedIssuer || state.Revision < 1 || state.Revision > maximumSafeInteger ||
		state.SecurityEpoch < 1 || state.SecurityEpoch > maximumSafeInteger || len(state.Lineage) < 1 ||
		len(state.Lineage) > maximumLifetimeKeyCount {
		return State{}, ErrInvalidAuthority
	}
	seen := make(map[string]struct{}, len(state.Lineage))
	for _, key := range state.Lineage {
		if key.JWK.Kid == "" || !validKeyInterval(key) {
			return State{}, ErrInvalidAuthority
		}
		if _, duplicate := seen[key.JWK.Kid]; duplicate {
			return State{}, ErrInvalidAuthority
		}
		seen[key.JWK.Kid] = struct{}{}
	}
	sort.Slice(state.Lineage, func(left, right int) bool { return state.Lineage[left].JWK.Kid < state.Lineage[right].JWK.Kid })
	digest, err := authorityDigest(state)
	if err != nil || digest != state.AuthorityDigest {
		return State{}, ErrInvalidAuthority
	}
	return state, nil
}

func validateTransition(previous State, exists bool, candidate State) (bool, error) {
	if !exists {
		return true, nil
	}
	if candidate.Issuer != previous.Issuer || candidate.Revision < previous.Revision || candidate.SecurityEpoch < previous.SecurityEpoch {
		return false, ErrInvalidAuthority
	}
	if candidate.Revision == previous.Revision {
		if candidate.AuthorityDigest != previous.AuthorityDigest {
			return false, ErrInvalidAuthority
		}
		return false, nil
	}
	candidateKeys := make(map[string]LineageKey, len(candidate.Lineage))
	for _, key := range candidate.Lineage {
		candidateKeys[key.JWK.Kid] = key
	}
	for _, old := range previous.Lineage {
		current, present := candidateKeys[old.JWK.Kid]
		if !present || !equalJWK(old.JWK, current.JWK) {
			return false, ErrInvalidAuthority
		}
	}
	return true, nil
}

func authorityDigest(state State) (string, error) {
	lineage := make([]api.IdentityJWKSLineageKey, len(state.Lineage))
	keys := make([]api.IdentityJWK, 0, len(state.Lineage))
	for index, key := range state.Lineage {
		lineage[index] = api.IdentityJWKSLineageKey{
			JWK: cloneJWK(key.JWK), Enabled: key.Enabled, NotBefore: key.NotBefore, NotAfter: key.NotAfter,
		}
		if key.Enabled {
			keys = append(keys, cloneJWK(key.JWK))
		}
	}
	document := api.IdentityJWKS{
		Keys: keys,
		CloudAgentsAuthority: api.IdentityJWKSAuthority{
			Issuer: state.Issuer, Revision: strconv.FormatInt(state.Revision, 10),
			SecurityEpoch: strconv.FormatInt(state.SecurityEpoch, 10),
			NotBefore:     state.NotBefore, ExpiresAt: state.ExpiresAt, Lineage: lineage,
		},
	}
	encoded, err := api.EncodeIdentityJWKSJSON(document)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validAt(state State, now int64) bool {
	return state.NotBefore >= 0 && state.ExpiresAt > state.NotBefore && state.ExpiresAt <= maximumNumericDate &&
		state.ExpiresAt-state.NotBefore <= maximumSnapshotLifetime && state.NotBefore <= now && now < state.ExpiresAt
}

func validKeyInterval(key LineageKey) bool {
	return key.NotBefore >= 0 && key.NotAfter > key.NotBefore && key.NotAfter <= maximumNumericDate
}

func parseSafePositiveDecimal(value string) (int64, bool) {
	if value == "" || value[0] == '0' || len(value) > 16 {
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed >= 1 && parsed <= maximumSafeInteger
}

func equalJWK(left, right api.IdentityJWK) bool {
	return left.Alg == right.Alg && left.E == right.E && left.Kid == right.Kid && left.Kty == right.Kty &&
		left.N == right.N && left.Use == right.Use && equalStrings(left.KeyOps, right.KeyOps)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneJWK(source api.IdentityJWK) api.IdentityJWK {
	result := source
	result.KeyOps = append([]string(nil), source.KeyOps...)
	return result
}

func cloneState(source State) State {
	result := source
	result.Lineage = make([]LineageKey, len(source.Lineage))
	for index, key := range source.Lineage {
		result.Lineage[index] = key
		result.Lineage[index].JWK = cloneJWK(key.JWK)
	}
	return result
}

func cloneConfiguredKeys(source []authn.ConfiguredVerifierKey) []authn.ConfiguredVerifierKey {
	result := make([]authn.ConfiguredVerifierKey, len(source))
	for index, key := range source {
		result[index] = key
		result[index].JWK = append([]byte(nil), key.JWK...)
	}
	return result
}

func cloneConfiguredVerifierConfig(source authn.ConfiguredVerifierConfig) authn.ConfiguredVerifierConfig {
	result := source
	result.Keys = cloneConfiguredKeys(source.Keys)
	return result
}

func clonePair(source ConfigPair) ConfigPair {
	result := source
	result.User = cloneConfiguredVerifierConfig(source.User)
	result.Admin = cloneConfiguredVerifierConfig(source.Admin)
	return result
}
