package authn

// identityVerifierProfile is the closed, generated identity verifier contract.
// Every field is comparable so the package can reject zero values, copies from
// another profile, and any mutation with one whole-value equality check.
//
// Slice A deliberately defines facts only. It defines no verifier, trust
// snapshot, principal, verification context, token parser, or authority API.
type identityVerifierProfile struct {
	formatVersion  string
	registryID     string
	registryDigest string
	profileID      string
	profileDigest  string

	token                   identityVerifierTokenRules
	algorithm               identityVerifierAlgorithmRules
	protectedHeader         identityVerifierProtectedHeaderRules
	jwk                     identityVerifierJWKRules
	claims                  identityVerifierClaimRules
	limits                  identityVerifierLimits
	lexicalRules            identityVerifierLexicalRules
	timeRules               identityVerifierTimeRules
	parsingRules            identityVerifierParsingRules
	bindingRules            identityVerifierBindingRules
	errorRules              identityVerifierErrorRules
	digestRules             identityVerifierDigestRules
	keyLineage              identityVerifierKeyLineageRules
	trustSnapshot           identityVerifierTrustSnapshotRules
	verificationContext     identityVerifierContextRules
	verifiedPrincipal       identityVerifierPrincipalRules
	implementationNonClaims identityVerifierImplementationNonClaims
}

type identityVerifierTokenRules struct {
	serialization  string
	segmentCount   uint8
	signature      string
	acceptedTypes  [2]string
	typeComparison string
	canonicalType  string
	forbiddenForms [6]string
}

type identityVerifierAlgorithmRules struct {
	accepted           [1]string
	selectionAuthority string
	implementation     string
	none               string
	hmac               string
	callerSelected     string
}

type identityVerifierProtectedHeaderRules struct {
	requiredMembers        [3]string
	allowedMembers         [3]string
	algorithmComparison    string
	forbiddenMembers       [5]string
	unknownMembers         string
	tokenSuppliedKeyLookup string
}

type identityVerifierJWKRules struct {
	allowedMembers             [7]string
	requiredMembers            [7]string
	kty                        string
	alg                        string
	use                        string
	keyOps                     [1]string
	exponentBase64urlUInt      string
	exponentDecimal            uint32
	modulusEncoding            string
	privateMembers             [7]string
	privateOrSymmetricMaterial string
	unknownMembers             string
}

type identityVerifierClaimRules struct {
	requiredRegistered     [8]string
	subjectKindClaim       string
	tenantIDClaim          string
	projectIDClaim         string
	securityEpochClaim     string
	tokenProfileClaim      string
	tokenProfileValue      string
	requiredCustom         [4]string
	optionalCustom         [1]string
	subjectKinds           [3]string
	audienceCardinality    string
	scopeEncoding          string
	projectBinding         string
	additionalSignedClaims string
}

type identityVerifierLimits struct {
	compactTokenBytes            uint32
	decodedProtectedHeaderBytes  uint32
	decodedClaimsBytes           uint32
	jsonDepth                    uint32
	trustSnapshotBytes           uint32
	lifetimeKeyLineageRecords    uint32
	audiences                    uint32
	scopes                       uint32
	revokedTokenIDs              uint32
	kidBytes                     uint32
	issuerScalars                uint32
	audienceScalars              uint32
	subjectScalars               uint32
	clientIDScalars              uint32
	tokenIDScalars               uint32
	opaqueIdentifierBytes        uint32
	scopeItemBytesMin            uint32
	scopeItemBytesMax            uint32
	rsaModulusBitsMin            uint32
	rsaModulusBitsMax            uint32
	tokenLifetimeSeconds         uint32
	clockSkewSeconds             uint32
	trustSnapshotValiditySeconds uint32
}

type identityVerifierLexicalRules struct {
	decodedStringComparison string
	jsonEscapeEquivalence   string
	issuerAndAudience       string
	subject                 string
	clientIDAndTokenID      string
	opaqueIdentifier        string
	audience                string
	scopeSplit              string
	scopeItemPattern        string
	scopeOrdering           string
	integerEncoding         string
	epochAndGenerationRange string
	numericDateRange        string
	compactBase64url        string
}

type identityVerifierTimeRules struct {
	clock                   string
	tokenChecks             [6]string
	keyIssuanceInterval     string
	snapshotInterval        string
	snapshotMaximumValidity string
	snapshotClockSkew       string
}

type identityVerifierParsingRules struct {
	duplicateDecodedMembers string
	jsonEncoding            string
	topLevel                string
	numericDates            string
	sizeAndDepthAdmission   string
	base64url               string
	claimsObject            string
}

type identityVerifierBindingRules struct {
	issuer        string
	key           string
	audience      string
	tokenType     string
	time          string
	revocation    string
	securityEpoch string
	subject       string
	tenant        string
	project       string
	permission    string
	profile       string
	inference     string
}

type identityVerifierErrorRules struct {
	categories        [15]string
	stability         string
	redactedFacts     [4]string
	redactionSurfaces [4]string
}

type identityVerifierDigestRules struct {
	algorithm                    string
	textFormat                   string
	framing                      string
	jsonCanonicalization         string
	setArrayOrdering             string
	keyAndLineageOrdering        string
	domains                      identityVerifierDigestDomains
	projections                  identityVerifierDigestProjections
	ordinaryGenerationLockHashes string
}

type identityVerifierDigestDomains struct {
	profile           string
	registry          string
	trustSnapshot     string
	tokenInput        string
	verifiedPrincipal string
}

type identityVerifierDigestProjections struct {
	profile           string
	registry          string
	trustSnapshot     string
	tokenInput        string
	verifiedPrincipal string
}

type identityVerifierKeyLineageRules struct {
	generationStart                    uint32
	generationStep                     uint32
	previousSnapshotAtGenerationOne    string
	previousSnapshotAfterGenerationOne string
	kidBinding                         string
	sameKidNewMaterial                 string
	sameKidSameMaterial                string
	records                            string
	overflow                           string
}

type identityVerifierTrustSnapshotRules struct {
	authority      string
	provisioning   string
	requiredFacts  [12]string
	mutation       string
	selection      string
	externalLookup string
	invalidation   string
}

type identityVerifierContextRules struct {
	authority                        string
	requiredFacts                    [6]string
	audienceAuthority                string
	tenantProjectPermissionAuthority string
	productionConstructor            string
}

type identityVerifierPrincipalRules struct {
	construction              string
	lifetime                  string
	consumption               string
	leaseScope                string
	boundFacts                [17]string
	forbiddenPayloadFacts     [7]string
	secondOrConcurrentConsume string
}

type identityVerifierImplementationNonClaims struct {
	httpSurface                 string
	oidcDiscovery               string
	remoteJWKs                  string
	providerSideEffects         string
	p2Surface                   string
	productionTrustProvisioning string
	productionDatabaseWrites    string
	deployment                  string
	publication                 string
	gateStatus                  string
}

func (profile identityVerifierProfile) valid() bool {
	return profile == generatedIdentityVerifierProfile()
}
