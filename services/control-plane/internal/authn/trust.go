package authn

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"sort"
	"sync"
)

type rsaVerificationKey struct {
	kid       string
	modulus   []byte
	publicKey rsa.PublicKey
	enabled   bool
	notBefore int64
	notAfter  int64
}

type keyLineageRecord struct {
	kid     string
	modulus []byte
}

type trustSnapshot struct {
	profileDigest          string
	registryDigest         string
	issuer                 string
	audience               string
	generation             int64
	previousSnapshotDigest string
	securityEpoch          int64
	notBefore              int64
	expiresAt              int64
	keys                   map[string]rsaVerificationKey
	keyOrder               []string
	lineage                []keyLineageRecord
	revokedKeyIDs          map[string]struct{}
	revokedKeyOrder        []string
	revokedTokenIDs        map[string]struct{}
	revokedTokenOrder      []string
	canonical              []byte
	digest                 string
}

type snapshotKeyCandidate struct {
	jwk       []byte
	enabled   bool
	notBefore int64
	notAfter  int64
}

type snapshotCandidate struct {
	issuer                 string
	audience               string
	generation             int64
	previousSnapshotDigest string
	securityEpoch          int64
	notBefore              int64
	expiresAt              int64
	keys                   []snapshotKeyCandidate
	revokedKeyIDs          []string
	revokedTokenIDs        []string
}

type trustGeneration struct {
	lease    sync.RWMutex
	active   bool
	snapshot *trustSnapshot
}

type trustLineage struct {
	mutation  sync.Mutex
	state     sync.Mutex
	admitting bool
	current   *trustGeneration
	history   map[string][]byte
}

func (lineage *trustLineage) replace(candidate snapshotCandidate) error {
	if lineage == nil {
		return verifierError(errorInternalFailure)
	}
	lineage.mutation.Lock()
	defer lineage.mutation.Unlock()

	lineage.state.Lock()
	var previous *trustSnapshot
	if lineage.current != nil {
		previous = lineage.current.snapshot
	}
	oldGeneration := lineage.current
	priorHistory := lineage.history
	lineage.state.Unlock()

	snapshot, history, err := buildTrustSnapshot(candidate, previous, priorHistory)
	if err != nil {
		return err
	}

	lineage.state.Lock()
	lineage.admitting = false
	lineage.current = nil
	lineage.state.Unlock()
	if oldGeneration != nil {
		oldGeneration.lease.Lock()
		oldGeneration.active = false
		oldGeneration.lease.Unlock()
	}
	lineage.state.Lock()
	lineage.history = history
	lineage.current = &trustGeneration{active: true, snapshot: snapshot}
	lineage.admitting = true
	lineage.state.Unlock()
	return nil
}

func (lineage *trustLineage) invalidate() {
	if lineage == nil {
		return
	}
	lineage.mutation.Lock()
	defer lineage.mutation.Unlock()
	lineage.state.Lock()
	lineage.admitting = false
	oldGeneration := lineage.current
	lineage.current = nil
	lineage.state.Unlock()
	if oldGeneration == nil {
		return
	}
	oldGeneration.lease.Lock()
	oldGeneration.active = false
	oldGeneration.lease.Unlock()
}

func (lineage *trustLineage) acquireCurrent() (*trustGeneration, *trustSnapshot, bool) {
	if lineage == nil {
		return nil, nil, false
	}
	lineage.state.Lock()
	defer lineage.state.Unlock()
	generation := lineage.current
	if !lineage.admitting || generation == nil {
		return nil, nil, false
	}
	if !generation.lease.TryRLock() {
		return nil, nil, false
	}
	if !generation.active || generation.snapshot == nil {
		generation.lease.RUnlock()
		return nil, nil, false
	}
	return generation, generation.snapshot, true
}

func (lineage *trustLineage) acquireExact(expected *trustGeneration) (*trustSnapshot, bool) {
	if lineage == nil || expected == nil {
		return nil, false
	}
	lineage.state.Lock()
	defer lineage.state.Unlock()
	if !lineage.admitting || lineage.current != expected {
		return nil, false
	}
	if !expected.lease.TryRLock() {
		return nil, false
	}
	if !expected.active || expected.snapshot == nil {
		expected.lease.RUnlock()
		return nil, false
	}
	return expected.snapshot, true
}

func validSnapshotCardinality(profile identityVerifierProfile, priorCount, keyCount int) bool {
	limit := int(profile.limits.lifetimeKeyLineageRecords)
	return limit >= 1 && priorCount >= 0 && priorCount <= limit && keyCount >= 1 && keyCount <= limit
}

func buildTrustSnapshot(candidate snapshotCandidate, previous *trustSnapshot, priorHistory map[string][]byte) (*trustSnapshot, map[string][]byte, error) {
	profile := generatedIdentityVerifierProfile()
	if !validSnapshotCardinality(profile, len(priorHistory), len(candidate.keys)) {
		return nil, nil, verifierError(errorInternalFailure)
	}
	if !profile.valid() || !validAbsoluteURI(candidate.issuer, int(profile.limits.issuerScalars)) ||
		!validAbsoluteURI(candidate.audience, int(profile.limits.audienceScalars)) ||
		candidate.generation < 1 || candidate.generation > 9007199254740991 ||
		candidate.securityEpoch < 1 || candidate.securityEpoch > 9007199254740991 ||
		candidate.notBefore < 0 || candidate.expiresAt <= candidate.notBefore ||
		candidate.expiresAt > 253402300799 || candidate.expiresAt-candidate.notBefore > int64(profile.limits.trustSnapshotValiditySeconds) {
		return nil, nil, verifierError(errorInternalFailure)
	}
	if previous == nil {
		if candidate.previousSnapshotDigest != "" || len(priorHistory) != 0 {
			return nil, nil, verifierError(errorInternalFailure)
		}
	} else if candidate.generation != previous.generation+1 || candidate.previousSnapshotDigest != previous.digest ||
		candidate.issuer != previous.issuer {
		return nil, nil, verifierError(errorInternalFailure)
	}
	history := make(map[string][]byte, len(priorHistory)+len(candidate.keys))
	for kid, modulus := range priorHistory {
		history[kid] = append([]byte(nil), modulus...)
	}
	keys := make(map[string]rsaVerificationKey, len(candidate.keys))
	keyOrder := make([]string, 0, len(candidate.keys))
	for _, source := range candidate.keys {
		key, ok := parseRSAJWK(source.jwk, source.enabled, source.notBefore, source.notAfter, profile)
		if !ok || key.notBefore < 0 || key.notAfter <= key.notBefore || key.notAfter > 253402300799 {
			return nil, nil, verifierError(errorInternalFailure)
		}
		if _, duplicate := keys[key.kid]; duplicate {
			return nil, nil, verifierError(errorInternalFailure)
		}
		if oldMaterial, exists := history[key.kid]; exists {
			if !equalBytes(oldMaterial, key.modulus) {
				return nil, nil, verifierError(errorInternalFailure)
			}
		} else {
			history[key.kid] = append([]byte(nil), key.modulus...)
		}
		keys[key.kid] = key
		keyOrder = append(keyOrder, key.kid)
	}
	if len(history) > int(profile.limits.lifetimeKeyLineageRecords) {
		return nil, nil, verifierError(errorInternalFailure)
	}
	sort.Strings(keyOrder)
	revokedKeyOrder, ok := sortedUnique(candidate.revokedKeyIDs, len(history), func(value string) bool {
		return validOpaqueIdentifier(value, int(profile.limits.kidBytes))
	})
	if !ok {
		return nil, nil, verifierError(errorInternalFailure)
	}
	for _, kid := range revokedKeyOrder {
		if _, known := history[kid]; !known {
			return nil, nil, verifierError(errorInternalFailure)
		}
	}
	revokedTokenOrder, ok := sortedUnique(candidate.revokedTokenIDs, int(profile.limits.revokedTokenIDs), func(value string) bool {
		return validExactString(value, int(profile.limits.tokenIDScalars), false)
	})
	if !ok {
		return nil, nil, verifierError(errorInternalFailure)
	}
	lineage := make([]keyLineageRecord, 0, len(history))
	for kid, modulus := range history {
		lineage = append(lineage, keyLineageRecord{kid: kid, modulus: append([]byte(nil), modulus...)})
	}
	sort.Slice(lineage, func(left, right int) bool { return lineage[left].kid < lineage[right].kid })
	snapshot := &trustSnapshot{
		profileDigest: identityVerifierProfileDigest, registryDigest: identityVerifierRegistryDigest,
		issuer: candidate.issuer, audience: candidate.audience, generation: candidate.generation,
		previousSnapshotDigest: candidate.previousSnapshotDigest, securityEpoch: candidate.securityEpoch,
		notBefore: candidate.notBefore, expiresAt: candidate.expiresAt, keys: keys, keyOrder: keyOrder,
		lineage: lineage, revokedKeyIDs: stringSet(revokedKeyOrder), revokedKeyOrder: revokedKeyOrder,
		revokedTokenIDs: stringSet(revokedTokenOrder), revokedTokenOrder: revokedTokenOrder,
	}
	canonical, canonicalOK := snapshotCanonical(snapshot)
	if !canonicalOK {
		return nil, nil, verifierError(errorInternalFailure)
	}
	snapshot.canonical = canonical
	if len(snapshot.canonical) > int(profile.limits.trustSnapshotBytes) {
		return nil, nil, verifierError(errorInternalFailure)
	}
	snapshot.digest = domainDigest(profile.digestRules.domains.trustSnapshot, snapshot.canonical)
	return snapshot, history, nil
}

func parseRSAJWK(raw []byte, enabled bool, notBefore, notAfter int64, profile identityVerifierProfile) (rsaVerificationKey, bool) {
	object, ok := strictJSONObject(raw, 8192, int(profile.limits.jsonDepth))
	if !ok || len(object) != 7 {
		return rsaVerificationKey{}, false
	}
	required := []string{"alg", "e", "key_ops", "kid", "kty", "n", "use"}
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return rsaVerificationKey{}, false
		}
	}
	alg, algOK := exactJSONString(object["alg"])
	exponent, exponentOK := exactJSONString(object["e"])
	kid, kidOK := exactJSONString(object["kid"])
	kty, ktyOK := exactJSONString(object["kty"])
	modulusText, modulusOK := exactJSONString(object["n"])
	use, useOK := exactJSONString(object["use"])
	var operations []string
	operationsOK := json.Unmarshal(object["key_ops"], &operations) == nil
	if !algOK || !exponentOK || !kidOK || !ktyOK || !modulusOK || !useOK || !operationsOK ||
		alg != "RS256" || exponent != "AQAB" || kty != "RSA" || use != "sig" ||
		len(operations) != 1 || operations[0] != "verify" || !validOpaqueIdentifier(kid, int(profile.limits.kidBytes)) {
		return rsaVerificationKey{}, false
	}
	modulus, ok := decodeCanonicalBase64url(modulusText, int(profile.limits.rsaModulusBitsMax/8))
	if !ok || len(modulus) == 0 || modulus[0] == 0 {
		return rsaVerificationKey{}, false
	}
	integer := new(big.Int).SetBytes(modulus)
	if integer.BitLen() < int(profile.limits.rsaModulusBitsMin) || integer.BitLen() > int(profile.limits.rsaModulusBitsMax) || integer.Bit(0) == 0 {
		return rsaVerificationKey{}, false
	}
	return rsaVerificationKey{
		kid: kid, modulus: append([]byte(nil), modulus...),
		publicKey: rsa.PublicKey{N: new(big.Int).Set(integer), E: int(profile.jwk.exponentDecimal)},
		enabled:   enabled, notBefore: notBefore, notAfter: notAfter,
	}, true
}

func snapshotCanonical(snapshot *trustSnapshot) ([]byte, bool) {
	object := newCanonicalObject()
	object.member("audience", jsonString(snapshot.audience))
	object.member("generation", jsonInteger(snapshot.generation))
	object.member("issuer", jsonString(snapshot.issuer))
	lineage := make([]canonicalValue, 0, len(snapshot.lineage))
	for _, record := range snapshot.lineage {
		item := newCanonicalObject()
		item.member("e", jsonString("AQAB"))
		item.member("kid", jsonString(record.kid))
		item.member("n", jsonString(base64url(record.modulus)))
		lineage = append(lineage, item.bytes())
	}
	object.member("keyLineage", canonicalArray(lineage))
	keys := make([]canonicalValue, 0, len(snapshot.keyOrder))
	for _, kid := range snapshot.keyOrder {
		key := snapshot.keys[kid]
		item := newCanonicalObject()
		item.member("enabled", jsonBoolean(key.enabled))
		jwk := newCanonicalObject()
		jwk.member("alg", jsonString("RS256"))
		jwk.member("e", jsonString("AQAB"))
		jwk.member("key_ops", jsonStringArray([]string{"verify"}))
		jwk.member("kid", jsonString(key.kid))
		jwk.member("kty", jsonString("RSA"))
		jwk.member("n", jsonString(base64url(key.modulus)))
		jwk.member("use", jsonString("sig"))
		item.member("jwk", jwk.bytes())
		item.member("notAfter", jsonInteger(key.notAfter))
		item.member("notBefore", jsonInteger(key.notBefore))
		keys = append(keys, item.bytes())
	}
	object.member("keys", canonicalArray(keys))
	if snapshot.previousSnapshotDigest != "" {
		object.member("previousSnapshotDigest", jsonString(snapshot.previousSnapshotDigest))
	}
	object.member("profileDigest", jsonString(snapshot.profileDigest))
	object.member("registryDigest", jsonString(snapshot.registryDigest))
	object.member("revokedKeyIds", jsonStringArray(snapshot.revokedKeyOrder))
	object.member("revokedTokenIds", jsonStringArray(snapshot.revokedTokenOrder))
	object.member("securityEpoch", jsonInteger(snapshot.securityEpoch))
	validity := newCanonicalObject()
	validity.member("expiresAt", jsonInteger(snapshot.expiresAt))
	validity.member("notBefore", jsonInteger(snapshot.notBefore))
	object.member("validity", validity.bytes())
	result := object.bytes()
	if !result.valid {
		return nil, false
	}
	return append([]byte(nil), result.bytes...), true
}

func canonicalArray(items []canonicalValue) canonicalValue {
	result := []byte{'['}
	for index, item := range items {
		if !item.valid {
			return canonicalValue{}
		}
		if index > 0 {
			result = append(result, ',')
		}
		result = append(result, item.bytes...)
	}
	return canonicalValue{bytes: append(result, ']'), valid: true}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func base64url(value []byte) string {
	return base64.RawURLEncoding.EncodeToString(value)
}
