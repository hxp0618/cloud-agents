package authn

import (
	"sync/atomic"
	"time"
)

// VerifiedPrincipal is an opaque pointer-only proof returned by the offline
// verifier. Its fields and construction remain package-private.
type VerifiedPrincipal struct {
	self       *VerifiedPrincipal
	consumed   *atomic.Bool
	lineage    *trustLineage
	generation *trustGeneration
	clock      func() time.Time

	profileDigest       string
	registryDigest      string
	snapshotDigest      string
	snapshotGeneration  int64
	tokenInputDigest    string
	principalDigest     string
	issuer              string
	subjectKind         string
	subjectValue        string
	audience            string
	clientID            string
	scopes              []string
	targetTenantID      string
	targetResourceLevel targetResourceLevel
	targetResourceID    string
	requiredPermission  string
	tokenProjectID      string
	hasTokenProject     bool
	securityEpoch       int64
	issuedAt            int64
	notBefore           int64
	hasNotBefore        bool
	expiresAt           int64
	tokenID             string
	keyID               string
	tokenType           string
}

// VerifiedPrincipalView is valid only while the Consume callback is active.
// Every method reports ok=false after callback return, so retaining the view
// cannot retain usable authorization authority.
type VerifiedPrincipalView interface {
	Actor() (kind, issuer, value string, ok bool)
	AuthorizationContext() (tenantID, resourceLevel, resourceID, permission string, ok bool)
	Check() bool
	sealedVerifiedPrincipalView()
}

type verifiedPrincipalView struct {
	principal *VerifiedPrincipal
	live      *atomic.Bool
}

func (view *verifiedPrincipalView) Actor() (string, string, string, bool) {
	if !view.Check() {
		return "", "", "", false
	}
	return view.principal.subjectKind, view.principal.issuer, view.principal.subjectValue, true
}

func (view *verifiedPrincipalView) AuthorizationContext() (string, string, string, string, bool) {
	if !view.Check() {
		return "", "", "", "", false
	}
	return view.principal.targetTenantID, string(view.principal.targetResourceLevel), view.principal.targetResourceID, view.principal.requiredPermission, true
}

func (view *verifiedPrincipalView) Check() bool {
	return view != nil && view.live != nil && view.live.Load() && view.principal != nil
}

func (*verifiedPrincipalView) sealedVerifiedPrincipalView() {}

// Consume atomically spends the principal and holds its exact generation read
// lease through callback completion. Failed and successful attempts are both
// permanently consumed.
func ConsumeVerifiedPrincipal(principal *VerifiedPrincipal, callback func(VerifiedPrincipalView) error) error {
	if principal == nil || principal.consumed == nil || !principal.consumed.CompareAndSwap(false, true) {
		return verifierError(errorInternalFailure)
	}
	if callback == nil || !principal.selfBound() {
		return verifierError(errorInternalFailure)
	}
	snapshot, acquired := principal.lineage.acquireExact(principal.generation)
	if !acquired {
		return verifierError(errorInternalFailure)
	}
	defer principal.generation.lease.RUnlock()
	profile := generatedIdentityVerifierProfile()
	nowSecond := principal.clock().UTC().Unix()
	if !principal.matchesSnapshot(snapshot) || !validSnapshotAt(snapshot, nowSecond, profile) || nowSecond >= principal.expiresAt+int64(profile.limits.clockSkewSeconds) {
		return verifierError(errorInternalFailure)
	}
	if _, revoked := snapshot.revokedKeyIDs[principal.keyID]; revoked {
		return verifierError(errorRevokedKey)
	}
	if _, revoked := snapshot.revokedTokenIDs[principal.tokenID]; revoked {
		return verifierError(errorRevokedToken)
	}
	live := &atomic.Bool{}
	live.Store(true)
	view := &verifiedPrincipalView{principal: principal, live: live}
	defer live.Store(false)
	return callback(view)
}

func newVerifiedPrincipal(context verificationContext, generation *trustGeneration, snapshot *trustSnapshot, claims parsedClaims, keyID, tokenDigest string) (*VerifiedPrincipal, bool) {
	principal := &VerifiedPrincipal{
		consumed: &atomic.Bool{}, lineage: context.lineage, generation: generation, clock: context.clock,
		profileDigest: identityVerifierProfileDigest, registryDigest: identityVerifierRegistryDigest,
		snapshotDigest: snapshot.digest, snapshotGeneration: snapshot.generation, tokenInputDigest: tokenDigest,
		issuer: claims.issuer, subjectKind: claims.subjectKind, subjectValue: claims.subject,
		audience: claims.audience, clientID: claims.clientID, scopes: append([]string(nil), claims.scopes...),
		targetTenantID: context.targetTenantID, targetResourceLevel: context.targetResourceLevel,
		targetResourceID: context.targetResourceID, requiredPermission: context.requiredPermission,
		tokenProjectID: claims.projectID, hasTokenProject: claims.hasProject, securityEpoch: claims.securityEpoch,
		issuedAt: claims.issuedAt, notBefore: claims.notBefore, hasNotBefore: claims.hasNotBefore,
		expiresAt: claims.expiresAt, tokenID: claims.tokenID, keyID: keyID, tokenType: "at+jwt",
	}
	canonical, ok := principalCanonical(principal)
	if !ok {
		return nil, false
	}
	principal.principalDigest = domainDigest(generatedIdentityVerifierProfile().digestRules.domains.verifiedPrincipal, canonical)
	principal.self = principal
	return principal, true
}

func (principal *VerifiedPrincipal) selfBound() bool {
	if principal == nil || principal.self != principal || principal.lineage == nil || principal.generation == nil || principal.clock == nil ||
		principal.profileDigest != identityVerifierProfileDigest || principal.registryDigest != identityVerifierRegistryDigest || principal.principalDigest == "" {
		return false
	}
	canonical, ok := principalCanonical(principal)
	if !ok {
		return false
	}
	want := domainDigest(generatedIdentityVerifierProfile().digestRules.domains.verifiedPrincipal, canonical)
	return principal.principalDigest == want
}

func (principal *VerifiedPrincipal) matchesSnapshot(snapshot *trustSnapshot) bool {
	if snapshot == nil || principal.snapshotDigest != snapshot.digest || principal.snapshotGeneration != snapshot.generation ||
		principal.securityEpoch != snapshot.securityEpoch || principal.issuer != snapshot.issuer || principal.audience != snapshot.audience {
		return false
	}
	key, exists := snapshot.keys[principal.keyID]
	return exists && key.enabled && key.notBefore <= principal.issuedAt && principal.issuedAt < key.notAfter
}

func principalCanonical(principal *VerifiedPrincipal) ([]byte, bool) {
	object := newCanonicalObject()
	object.member("audience", jsonString(principal.audience))
	object.member("clientId", jsonString(principal.clientID))
	context := newCanonicalObject()
	context.member("requiredPermission", jsonString(principal.requiredPermission))
	context.member("targetResourceId", jsonString(principal.targetResourceID))
	context.member("targetResourceLevel", jsonString(string(principal.targetResourceLevel)))
	context.member("targetTenantId", jsonString(principal.targetTenantID))
	context.member("trustGeneration", jsonInteger(principal.snapshotGeneration))
	object.member("context", context.bytes())
	object.member("issuer", jsonString(principal.issuer))
	object.member("keyId", jsonString(principal.keyID))
	object.member("profileDigest", jsonString(principal.profileDigest))
	object.member("registryDigest", jsonString(principal.registryDigest))
	object.member("scopes", jsonStringArray(principal.scopes))
	object.member("securityEpoch", jsonInteger(principal.securityEpoch))
	object.member("snapshotDigest", jsonString(principal.snapshotDigest))
	object.member("subjectKind", jsonString(principal.subjectKind))
	object.member("subjectValue", jsonString(principal.subjectValue))
	times := newCanonicalObject()
	times.member("expiresAt", jsonInteger(principal.expiresAt))
	times.member("issuedAt", jsonInteger(principal.issuedAt))
	if principal.hasNotBefore {
		times.member("notBefore", jsonInteger(principal.notBefore))
	}
	object.member("times", times.bytes())
	object.member("tokenId", jsonString(principal.tokenID))
	object.member("tokenInputDigest", jsonString(principal.tokenInputDigest))
	if principal.hasTokenProject {
		object.member("tokenProjectId", jsonString(principal.tokenProjectID))
	}
	object.member("tokenType", jsonString(principal.tokenType))
	result := object.bytes()
	if !result.valid {
		return nil, false
	}
	return append([]byte(nil), result.bytes...), true
}
