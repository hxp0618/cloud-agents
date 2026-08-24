package authn

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"
)

const (
	claimSubjectKind   = "https://schemas.cloud-agents.dev/claims/subject-kind"
	claimTenantID      = "https://schemas.cloud-agents.dev/claims/tenant-id"
	claimProjectID     = "https://schemas.cloud-agents.dev/claims/project-id"
	claimSecurityEpoch = "https://schemas.cloud-agents.dev/claims/security-epoch"
	claimTokenProfile  = "https://schemas.cloud-agents.dev/claims/token-profile"
)

type targetResourceLevel string

const (
	targetTenant       targetResourceLevel = "tenant"
	targetOrganization targetResourceLevel = "organization"
	targetProject      targetResourceLevel = "project"
)

type verificationContext struct {
	lineage             *trustLineage
	clock               func() time.Time
	targetTenantID      string
	targetResourceLevel targetResourceLevel
	targetResourceID    string
	requiredPermission  string
}

type parsedClaims struct {
	issuer        string
	subject       string
	audience      string
	expiresAt     int64
	issuedAt      int64
	notBefore     int64
	hasNotBefore  bool
	tokenID       string
	clientID      string
	scopes        []string
	subjectKind   string
	tenantID      string
	projectID     string
	hasProject    bool
	securityEpoch int64
	tokenProfile  string
}

func verifyAccessToken(context verificationContext, token string) (*VerifiedPrincipal, error) {
	profile := generatedIdentityVerifierProfile()
	if !profile.valid() || !validVerificationContext(context, profile) {
		return nil, verifierError(errorInternalFailure)
	}
	if len(token) == 0 || len(token) > int(profile.limits.compactTokenBytes) {
		return nil, verifierError(errorMalformed)
	}
	segments := strings.Split(token, ".")
	if len(segments) != int(profile.token.segmentCount) {
		return nil, verifierError(errorMalformed)
	}
	headerBytes, ok := decodeCanonicalBase64url(segments[0], int(profile.limits.decodedProtectedHeaderBytes))
	if !ok {
		return nil, verifierError(errorMalformed)
	}
	claimsBytes, ok := decodeCanonicalBase64url(segments[1], int(profile.limits.decodedClaimsBytes))
	if !ok {
		return nil, verifierError(errorMalformed)
	}
	signature, ok := decodeCanonicalBase64url(segments[2], int(profile.limits.rsaModulusBitsMax/8))
	if !ok {
		return nil, verifierError(errorMalformed)
	}
	header, ok := strictJSONObject(headerBytes, int(profile.limits.decodedProtectedHeaderBytes), int(profile.limits.jsonDepth))
	if !ok || len(header) != 3 {
		return nil, verifierError(errorMalformed)
	}
	claimsObject, ok := strictJSONObject(claimsBytes, int(profile.limits.decodedClaimsBytes), int(profile.limits.jsonDepth))
	if !ok {
		return nil, verifierError(errorMalformed)
	}
	for _, member := range []string{"alg", "kid", "typ"} {
		if _, exists := header[member]; !exists {
			return nil, verifierError(errorMalformed)
		}
	}
	algorithm, algorithmOK := exactJSONString(header["alg"])
	if !algorithmOK {
		return nil, verifierError(errorMalformed)
	}
	if algorithm != "RS256" {
		return nil, verifierError(errorUnsupportedAlgorithm)
	}
	kid, kidOK := exactJSONString(header["kid"])
	if !kidOK || !validOpaqueIdentifier(kid, int(profile.limits.kidBytes)) {
		return nil, verifierError(errorMalformed)
	}
	tokenType, typeOK := exactJSONString(header["typ"])
	if !typeOK {
		return nil, verifierError(errorMalformed)
	}
	if !acceptedTokenType(tokenType) {
		return nil, verifierError(errorUnsupportedProfile)
	}

	generation, snapshot, acquired := context.lineage.acquireCurrent()
	if !acquired {
		return nil, verifierError(errorInternalFailure)
	}
	defer generation.lease.RUnlock()
	if _, revoked := snapshot.revokedKeyIDs[kid]; revoked {
		return nil, verifierError(errorRevokedKey)
	}
	key, exists := snapshot.keys[kid]
	if !exists {
		return nil, verifierError(errorUnknownKey)
	}
	if !key.enabled {
		return nil, verifierError(errorRevokedKey)
	}
	if len(signature) != key.publicKey.Size() {
		return nil, verifierError(errorMalformed)
	}
	hash := sha256.Sum256([]byte(segments[0] + "." + segments[1]))
	if rsa.VerifyPKCS1v15(&key.publicKey, crypto.SHA256, hash[:], signature) != nil {
		return nil, verifierError(errorInvalidSignature)
	}
	claims, category := parseClaimsObject(claimsObject, profile)
	if category != "" {
		return nil, verifierError(category)
	}
	if snapshot.profileDigest != identityVerifierProfileDigest || snapshot.registryDigest != identityVerifierRegistryDigest {
		return nil, verifierError(errorInternalFailure)
	}
	if claims.tokenProfile != profile.claims.tokenProfileValue {
		return nil, verifierError(errorUnsupportedProfile)
	}
	if claims.issuer != snapshot.issuer {
		return nil, verifierError(errorIssuerMismatch)
	}
	if claims.audience != snapshot.audience {
		return nil, verifierError(errorAudienceMismatch)
	}
	nowSecond := context.clock().UTC().Unix()
	if !validSnapshotAt(snapshot, nowSecond, profile) || !validTokenTimes(claims, key, nowSecond, profile) {
		return nil, verifierError(errorTimeInvalid)
	}
	if _, revoked := snapshot.revokedTokenIDs[claims.tokenID]; revoked {
		return nil, verifierError(errorRevokedToken)
	}
	if claims.securityEpoch != snapshot.securityEpoch {
		return nil, verifierError(errorEpochMismatch)
	}
	if claims.tenantID != context.targetTenantID {
		return nil, verifierError(errorTenantMismatch)
	}
	if claims.hasProject && (context.targetResourceLevel != targetProject || claims.projectID != context.targetResourceID) {
		return nil, verifierError(errorProjectMismatch)
	}
	if !containsSorted(claims.scopes, context.requiredPermission) {
		return nil, verifierError(errorScopeMismatch)
	}
	principal, principalOK := newVerifiedPrincipal(context, generation, snapshot, claims, kid, domainDigest(profile.digestRules.domains.tokenInput, []byte(token)))
	if !principalOK {
		return nil, verifierError(errorInternalFailure)
	}
	return principal, nil
}

func parseClaimsObject(object map[string]json.RawMessage, profile identityVerifierProfile) (parsedClaims, verifierErrorCategory) {
	required := []string{"aud", "client_id", "exp", "iat", "iss", "jti", "scope", "sub", claimSecurityEpoch, claimSubjectKind, claimTenantID, claimTokenProfile}
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return parsedClaims{}, errorMalformed
		}
	}
	claims := parsedClaims{}
	var valid bool
	claims.issuer, valid = exactJSONString(object["iss"])
	if !valid || !validAbsoluteURI(claims.issuer, int(profile.limits.issuerScalars)) {
		return parsedClaims{}, errorMalformed
	}
	claims.subject, valid = exactJSONString(object["sub"])
	if !valid || !validExactString(claims.subject, int(profile.limits.subjectScalars), true) {
		return parsedClaims{}, errorMalformed
	}
	claims.audience, valid = exactJSONString(object["aud"])
	if !valid || !validAbsoluteURI(claims.audience, int(profile.limits.audienceScalars)) {
		return parsedClaims{}, errorMalformed
	}
	claims.expiresAt, valid = exactJSONInteger(object["exp"], 0, 253402300799)
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	claims.issuedAt, valid = exactJSONInteger(object["iat"], 0, 253402300799)
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	if rawNotBefore, exists := object["nbf"]; exists {
		claims.notBefore, valid = exactJSONInteger(rawNotBefore, 0, 253402300799)
		if !valid {
			return parsedClaims{}, errorMalformed
		}
		claims.hasNotBefore = true
	}
	claims.tokenID, valid = exactJSONString(object["jti"])
	if !valid || !validExactString(claims.tokenID, int(profile.limits.tokenIDScalars), false) {
		return parsedClaims{}, errorMalformed
	}
	claims.clientID, valid = exactJSONString(object["client_id"])
	if !valid || !validExactString(claims.clientID, int(profile.limits.clientIDScalars), false) {
		return parsedClaims{}, errorMalformed
	}
	scope, valid := exactJSONString(object["scope"])
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	claims.scopes, valid = parseScopes(scope, int(profile.limits.scopes), int(profile.limits.scopeItemBytesMin), int(profile.limits.scopeItemBytesMax))
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	claims.subjectKind, valid = exactJSONString(object[claimSubjectKind])
	if !valid || claims.subjectKind != "user" && claims.subjectKind != "serviceAccount" && claims.subjectKind != "workload" {
		return parsedClaims{}, errorMalformed
	}
	claims.tenantID, valid = exactJSONString(object[claimTenantID])
	if !valid || !validOpaqueIdentifier(claims.tenantID, int(profile.limits.opaqueIdentifierBytes)) {
		return parsedClaims{}, errorMalformed
	}
	if rawProject, exists := object[claimProjectID]; exists {
		claims.projectID, valid = exactJSONString(rawProject)
		if !valid || !validOpaqueIdentifier(claims.projectID, int(profile.limits.opaqueIdentifierBytes)) {
			return parsedClaims{}, errorMalformed
		}
		claims.hasProject = true
	}
	claims.securityEpoch, valid = exactJSONInteger(object[claimSecurityEpoch], 1, 9007199254740991)
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	claims.tokenProfile, valid = exactJSONString(object[claimTokenProfile])
	if !valid {
		return parsedClaims{}, errorMalformed
	}
	return claims, ""
}

func validVerificationContext(context verificationContext, profile identityVerifierProfile) bool {
	if context.lineage == nil || context.clock == nil || !validOpaqueIdentifier(context.targetTenantID, int(profile.limits.opaqueIdentifierBytes)) ||
		!validOpaqueIdentifier(context.targetResourceID, int(profile.limits.opaqueIdentifierBytes)) {
		return false
	}
	if context.targetResourceLevel != targetTenant && context.targetResourceLevel != targetOrganization && context.targetResourceLevel != targetProject {
		return false
	}
	if context.targetResourceLevel == targetTenant && context.targetResourceID != context.targetTenantID {
		return false
	}
	permissions, ok := parseScopes(context.requiredPermission, 1, int(profile.limits.scopeItemBytesMin), int(profile.limits.scopeItemBytesMax))
	return ok && len(permissions) == 1
}

func acceptedTokenType(value string) bool {
	if value == "" {
		return false
	}
	lower := make([]byte, len(value))
	for index := range len(value) {
		character := value[index]
		if character > 0x7f {
			return false
		}
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lower[index] = character
	}
	return string(lower) == "at+jwt" || string(lower) == "application/at+jwt"
}

func validSnapshotAt(snapshot *trustSnapshot, now int64, profile identityVerifierProfile) bool {
	return snapshot != nil && snapshot.notBefore <= now && now < snapshot.expiresAt &&
		snapshot.expiresAt-snapshot.notBefore <= int64(profile.limits.trustSnapshotValiditySeconds)
}

func validTokenTimes(claims parsedClaims, key rsaVerificationKey, now int64, profile identityVerifierProfile) bool {
	skew := int64(profile.limits.clockSkewSeconds)
	if claims.issuedAt > now+skew || now >= claims.expiresAt+skew || claims.issuedAt >= claims.expiresAt ||
		claims.expiresAt-claims.issuedAt > int64(profile.limits.tokenLifetimeSeconds) ||
		key.notBefore > claims.issuedAt || claims.issuedAt >= key.notAfter {
		return false
	}
	return !claims.hasNotBefore || claims.notBefore <= now+skew && claims.notBefore < claims.expiresAt
}

func containsSorted(values []string, wanted string) bool {
	index := sortSearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func sortSearchStrings(values []string, wanted string) int {
	low, high := 0, len(values)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if values[middle] < wanted {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low
}
