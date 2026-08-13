package migration

import (
	"sort"
	"time"
)

const (
	releaseTrustDecisionDomain                   = "cloud-agents-platform-release-trust-decision/v1"
	runnerProjectionDecisionDomain               = "cloud-agents-platform-runner-projection-decision/v1"
	executionLineageDomain                       = "cloud-agents-platform-execution-lineage/v1"
	recoveryPolicySubjectDomain                  = "cloud-agents-platform-recovery-policy-subject/v1"
	decisionRecoveryArtifactProfileDomain        = "cloud-agents-platform-decision-recovery-artifact-profile/v1"
	decisionRecoveryArtifactFormatVersion        = "cloud-agents-platform-decision-recovery-artifact/v1"
	decisionRecoveryArtifactProfileDigest Digest = "sha256:3f3f8f82796d2cf33128ddafaab49e7f565ee8ca1807ee02a91f22e780fe36ae"
)

// RunnerProjectionBindings is an opaque, package-owned projection decision.
// It has no exported constructor and never accepts JSON, environment, CLI, or
// DSN overlays. A trust verifier may bind it only after all signed subjects
// have been promoted through the existing verified wrapper seam.
type RunnerProjectionBindings struct {
	verified                              bool
	releaseTrustDecisionDigest            Digest
	runnerProjectionDecisionDigest        Digest
	executionLineageDigest                Digest
	schemaBundleDigest                    Digest
	authorityProfileDigest                Digest
	authorityBindingDigest                Digest
	recoveryPolicySubjectDigest           Digest
	decisionRecoveryArtifactProfileDigest Digest
	authorityBinding                      AuthorityBinding
	authorityBindingCanonical             string
	verifiedAuthorityProfile              verifiedAuthorityProfileSubject
	verifiedAuthority                     VerifiedAuthorityContract
	verifiedRecoveryPolicy                verifiedRecoveryPolicySubject
	initialSchemaScope                    VerifiedSchemaBundleScope
	initialSchemaScopeBindingCanonical    string
	executableCatalogs                    []ExecutableCatalogBinding
	releaseExpiresAt                      time.Time
	releaseSecurityEpoch                  uint64
	authorityExpiresAt                    time.Time
	authoritySecurityEpoch                uint64
	releaseSubject                        releaseTrustDecisionSubject
	deploymentID                          string
	expectedDatabaseName                  string
	expectedCanonical                     string
}

type oldDecisionAuthorization struct {
	OldRunnerProjectionDecisionDigest Digest `json:"old_runner_projection_decision_digest"`
	AllowExpired                      bool   `json:"allow_expired"`
	AllowRevoked                      bool   `json:"allow_revoked"`
	AllowCompromised                  bool   `json:"allow_compromised"`
}

// recoveryPolicySignedSubject is the closed signed body accepted by the
// verifier-owned promotion seam. There is deliberately no JSON decoder or
// caller-facing constructor for this subject.
type recoveryPolicySignedSubject struct {
	Domain                    string                     `json:"domain"`
	IssuerKeyIdentityDigest   Digest                     `json:"issuer_key_identity_digest"`
	ExpiresAt                 string                     `json:"expires_at"`
	SecurityEpoch             uint64                     `json:"security_epoch"`
	MinimumOldSecurityEpoch   uint64                     `json:"minimum_old_security_epoch"`
	OldRevocationPolicyDigest Digest                     `json:"old_revocation_policy_digest"`
	OldDecisionAuthorizations []oldDecisionAuthorization `json:"old_decision_authorizations"`
}

// verifiedRecoveryPolicySubject is verifier-owned opaque state. In addition
// to the exact signed body, it freezes the current minimum epoch against which
// that body was accepted so a weaker valid wrapper cannot be swapped in later.
type verifiedRecoveryPolicySubject struct {
	verified                    bool
	subject                     recoveryPolicySignedSubject
	subjectDigest               Digest
	expiresAt                   time.Time
	securityEpoch               uint64
	currentMinimumSecurityEpoch uint64
	subjectCanonical            string
	expectedCanonical           string
}

type recoveryPolicyBindingSentinel struct {
	SubjectDigest               Digest `json:"subject_digest"`
	SubjectCanonical            string `json:"subject_canonical"`
	CurrentMinimumSecurityEpoch uint64 `json:"current_minimum_security_epoch"`
}

type decisionRecoveryArtifactProfile struct {
	Domain               string   `json:"domain"`
	FormatVersion        string   `json:"format_version"`
	Canonicalization     string   `json:"canonicalization"`
	Base64URL            string   `json:"base64url"`
	IdentityMaxBytes     uint64   `json:"identity_max_bytes"`
	EncodedFieldMaxBytes uint64   `json:"encoded_field_max_bytes"`
	ProjectionInputsMax  uint64   `json:"projection_inputs_max"`
	CatalogInputsMax     uint64   `json:"catalog_inputs_max"`
	KindRank             []string `json:"kind_rank"`
	MaxSizeBytes         uint64   `json:"max_size_bytes"`
}

// verifiedAuthorityProfileSubject freezes the complete release-level
// authority profile artifact under its exact manifest descriptor digest.
type verifiedAuthorityProfileSubject struct {
	artifactDigest    Digest
	raw               []byte
	contract          AuthorityContract
	contractCanonical string
}

// ExecutableCatalogBinding owns one exact published catalog subject. Its
// fields stay private so a caller cannot swap a head, expiry, epoch, or wrapper
// after the combined decision has been frozen.
type ExecutableCatalogBinding struct {
	schemaHead            string
	catalogContractDigest Digest
	catalogContract       CatalogContract
	catalogCanonical      string
	verifiedCatalog       VerifiedCatalogContract
	expiresAt             time.Time
	securityEpoch         uint64
}

// verifiedExecutableCatalogSubject freezes the complete decoded catalog,
// including source descriptors and expected transitions, before it may enter a
// runner decision. It is package-private and can only be produced from exact
// artifact bytes whose descriptor digest has already been trusted.
type verifiedExecutableCatalogSubject struct {
	artifactDigest    Digest
	contract          CatalogContract
	contractCanonical string
	verifiedCatalog   VerifiedCatalogContract
}

type releaseTrustDecisionSubject struct {
	Domain                string `json:"domain"`
	RepositoryIdentity    string `json:"repository_identity"`
	ReleaseIdentity       string `json:"release_identity"`
	SchemaBundleDigest    Digest `json:"schema_bundle_digest"`
	BootstrapBundleDigest Digest `json:"bootstrap_bundle_digest"`
	ManifestDigest        Digest `json:"manifest_digest"`
	OuterArtifactDigest   Digest `json:"outer_artifact_digest"`
	RunnerReleaseDigest   Digest `json:"runner_release_digest"`
	ExpiresAt             string `json:"expires_at"`
	SecurityEpoch         uint64 `json:"security_epoch"`
}

type runnerCatalogDecisionSubject struct {
	SchemaHead            string `json:"schema_head"`
	CatalogContractDigest Digest `json:"catalog_contract_digest"`
	ExpiresAt             string `json:"expires_at"`
	SecurityEpoch         uint64 `json:"security_epoch"`
}

type runnerProjectionDecisionSubject struct {
	Domain                                string                         `json:"domain"`
	ReleaseTrustDecisionDigest            Digest                         `json:"release_trust_decision_digest"`
	SchemaBundleDigest                    Digest                         `json:"schema_bundle_digest"`
	AuthorityProfileDigest                Digest                         `json:"authority_profile_digest"`
	AuthorityBindingDigest                Digest                         `json:"authority_binding_digest"`
	AuthorityExpiresAt                    string                         `json:"authority_expires_at"`
	AuthoritySecurityEpoch                uint64                         `json:"authority_security_epoch"`
	RecoveryPolicySubjectDigest           Digest                         `json:"recovery_policy_subject_digest"`
	DecisionRecoveryArtifactProfileDigest Digest                         `json:"decision_recovery_artifact_profile_digest"`
	CatalogContracts                      []runnerCatalogDecisionSubject `json:"catalog_contracts"`
}

type executionLineageSubject struct {
	Domain                   string                   `json:"domain"`
	DeploymentID             string                   `json:"deployment_id"`
	ExpectedDatabaseIdentity expectedDatabaseIdentity `json:"expected_database_identity"`
	RepositoryIdentity       string                   `json:"repository_identity"`
}

type expectedDatabaseIdentity struct {
	DatabaseName string `json:"database_name"`
}

type runnerProjectionBindingSentinel struct {
	ReleaseTrustDecisionDigest            Digest                         `json:"release_trust_decision_digest"`
	RunnerProjectionDecisionDigest        Digest                         `json:"runner_projection_decision_digest"`
	ExecutionLineageDigest                Digest                         `json:"execution_lineage_digest"`
	SchemaBundleDigest                    Digest                         `json:"schema_bundle_digest"`
	AuthorityProfileDigest                Digest                         `json:"authority_profile_digest"`
	AuthorityBindingDigest                Digest                         `json:"authority_binding_digest"`
	RecoveryPolicySubjectDigest           Digest                         `json:"recovery_policy_subject_digest"`
	DecisionRecoveryArtifactProfileDigest Digest                         `json:"decision_recovery_artifact_profile_digest"`
	RecoveryPolicyBindingCanonical        string                         `json:"recovery_policy_binding_canonical"`
	AuthorityBindingCanonical             string                         `json:"authority_binding_canonical"`
	InitialSchemaScopeBindingCanonical    string                         `json:"initial_schema_scope_binding_canonical"`
	ReleaseExpiresAt                      string                         `json:"release_expires_at"`
	ReleaseSecurityEpoch                  uint64                         `json:"release_security_epoch"`
	AuthorityExpiresAt                    string                         `json:"authority_expires_at"`
	AuthoritySecurityEpoch                uint64                         `json:"authority_security_epoch"`
	CatalogContracts                      []runnerCatalogDecisionSubject `json:"catalog_contracts"`
	DeploymentID                          string                         `json:"deployment_id"`
	ExpectedDatabaseName                  string                         `json:"expected_database_name"`
}

func fixedDecisionRecoveryArtifactProfile() decisionRecoveryArtifactProfile {
	return decisionRecoveryArtifactProfile{
		Domain:               decisionRecoveryArtifactProfileDomain,
		FormatVersion:        decisionRecoveryArtifactFormatVersion,
		Canonicalization:     "RFC8785",
		Base64URL:            "unpadded-canonical",
		IdentityMaxBytes:     1024,
		EncodedFieldMaxBytes: 1048576,
		ProjectionInputsMax:  4099,
		CatalogInputsMax:     4096,
		KindRank:             []string{"release", "authority_profile", "authority_binding", "catalog"},
		MaxSizeBytes:         4194304,
	}
}

func validateDecisionRecoveryArtifactProfileDigest(digest Digest) error {
	computed, err := digestRunnerProjectionCanonical(fixedDecisionRecoveryArtifactProfile())
	if err != nil || computed != decisionRecoveryArtifactProfileDigest || digest != decisionRecoveryArtifactProfileDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "decision recovery artifact profile differs from the fixed profile", err)
	}
	return nil
}

// bindVerifiedRecoveryPolicySubject is a package-private promotion seam for a
// signed subject already verified by the release trust verifier. It accepts a
// closed typed body, never loose JSON, and owns every nested authorization.
func bindVerifiedRecoveryPolicySubject(subject recoveryPolicySignedSubject, currentMinimumSecurityEpoch uint64, now time.Time) (verifiedRecoveryPolicySubject, error) {
	expiresAt, err := parseCanonicalUTCTime(subject.ExpiresAt)
	if err != nil {
		return verifiedRecoveryPolicySubject{}, fail(CodeUntrusted, "recovery-policy", "recovery policy expiry is not canonical UTC", err)
	}
	ownedSubject := cloneRecoveryPolicySignedSubject(subject)
	canonical, digest, err := canonicalVerifiedBinding(ownedSubject)
	if err != nil || canonical == "" {
		return verifiedRecoveryPolicySubject{}, fail(CodeUntrusted, "recovery-policy", "recovery policy subject cannot be canonically identified", err)
	}
	verified := verifiedRecoveryPolicySubject{
		verified:                    true,
		subject:                     ownedSubject,
		subjectDigest:               digest,
		expiresAt:                   expiresAt,
		securityEpoch:               subject.SecurityEpoch,
		currentMinimumSecurityEpoch: currentMinimumSecurityEpoch,
		subjectCanonical:            canonical,
	}
	verified.expectedCanonical, _, err = canonicalVerifiedBinding(verified.sentinel())
	if err != nil {
		return verifiedRecoveryPolicySubject{}, fail(CodeUntrusted, "recovery-policy", "recovery policy sentinel cannot be canonicalized", err)
	}
	if err := verified.validateAt(now); err != nil {
		return verifiedRecoveryPolicySubject{}, err
	}
	return verified, nil
}

func (subject verifiedRecoveryPolicySubject) validateAt(now time.Time) error {
	if !subject.verified || subject.subjectCanonical == "" || subject.expectedCanonical == "" {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy was not produced by the trust verifier", nil)
	}
	if subject.subject.Domain != recoveryPolicySubjectDomain {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy subject domain is invalid", nil)
	}
	for field, digest := range map[string]Digest{
		"subject":               subject.subjectDigest,
		"issuer_key_identity":   subject.subject.IssuerKeyIdentityDigest,
		"old_revocation_policy": subject.subject.OldRevocationPolicyDigest,
	} {
		if err := requireDigest("recovery-policy."+field, digest); err != nil {
			return fail(CodeUntrusted, "recovery-policy", "recovery policy contains an invalid digest", err)
		}
	}
	parsedExpiry, err := parseCanonicalUTCTime(subject.subject.ExpiresAt)
	if err != nil || !parsedExpiry.Equal(subject.expiresAt) || subject.expiresAt.IsZero() || !now.Before(subject.expiresAt) {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy is expired or its expiry differs from the signed subject", err)
	}
	if subject.securityEpoch == 0 || subject.currentMinimumSecurityEpoch == 0 || subject.securityEpoch < subject.currentMinimumSecurityEpoch ||
		subject.subject.SecurityEpoch != subject.securityEpoch || subject.subject.MinimumOldSecurityEpoch == 0 {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy epoch is invalid or below the current minimum", nil)
	}
	if subject.subject.OldDecisionAuthorizations == nil {
		return fail(CodeUntrusted, "recovery-policy", "old decision authorizations must be a closed array", nil)
	}
	for index, authorization := range subject.subject.OldDecisionAuthorizations {
		if err := requireDigest("recovery-policy.old-decision", authorization.OldRunnerProjectionDecisionDigest); err != nil {
			return fail(CodeUntrusted, "recovery-policy", "old decision authorization contains an invalid digest", err)
		}
		if index > 0 && subject.subject.OldDecisionAuthorizations[index-1].OldRunnerProjectionDecisionDigest >= authorization.OldRunnerProjectionDecisionDigest {
			return fail(CodeUntrusted, "recovery-policy", "old decision authorizations are duplicate or not canonically sorted", nil)
		}
	}
	canonical, digest, err := canonicalVerifiedBinding(subject.subject)
	if err != nil || canonical != subject.subjectCanonical || digest != subject.subjectDigest {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy differs from its immutable signed subject", err)
	}
	expectedCanonical, _, err := canonicalVerifiedBinding(subject.sentinel())
	if err != nil || expectedCanonical != subject.expectedCanonical {
		return fail(CodeUntrusted, "recovery-policy", "recovery policy differs from its immutable verifier binding", err)
	}
	return nil
}

func (subject verifiedRecoveryPolicySubject) sentinel() recoveryPolicyBindingSentinel {
	return recoveryPolicyBindingSentinel{
		SubjectDigest: subject.subjectDigest, SubjectCanonical: subject.subjectCanonical,
		CurrentMinimumSecurityEpoch: subject.currentMinimumSecurityEpoch,
	}
}

func (subject verifiedRecoveryPolicySubject) ownedCopy() verifiedRecoveryPolicySubject {
	owned := subject
	owned.subject = cloneRecoveryPolicySignedSubject(subject.subject)
	return owned
}

func cloneRecoveryPolicySignedSubject(subject recoveryPolicySignedSubject) recoveryPolicySignedSubject {
	owned := subject
	if subject.OldDecisionAuthorizations != nil {
		owned.OldDecisionAuthorizations = append([]oldDecisionAuthorization{}, subject.OldDecisionAuthorizations...)
	}
	return owned
}

func rejectCurrentRecoveryAuthorization(policy verifiedRecoveryPolicySubject, current Digest) error {
	for _, authorization := range policy.subject.OldDecisionAuthorizations {
		if authorization.OldRunnerProjectionDecisionDigest == current {
			return fail(CodeUntrusted, "runner-projection-bindings", "recovery policy authorizes its current combined decision instead of a strict historical decision", nil)
		}
	}
	return nil
}

// bindVerifiedRunnerProjectionDecision is the package-private trust-verifier
// seam. verifiedAuthorityProfile and authorityBinding are distinct, already
// signature-verified subjects; every projection body must independently match
// its opaque verified wrapper.
func bindVerifiedRunnerProjectionDecision(decision VerifiedTrustDecision, verifiedAuthorityProfile verifiedAuthorityProfileSubject, authorityBinding AuthorityBinding, verifiedAuthority VerifiedAuthorityContract, verifiedRecoveryPolicy verifiedRecoveryPolicySubject, initialSchemaScope VerifiedSchemaBundleScope, verifiedCatalogs []verifiedExecutableCatalogSubject, now time.Time) (VerifiedTrustDecision, error) {
	if err := decision.validate(); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if err := verifiedAuthorityProfile.validate(); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if err := validateDecisionRecoveryArtifactProfileDigest(decisionRecoveryArtifactProfileDigest); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if err := verifiedRecoveryPolicy.validateAt(now); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if decision.expiresAt.IsZero() || !now.Before(decision.expiresAt) || decision.securityEpoch == 0 {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "release decision is missing or expired", nil)
	}
	if err := authorityBinding.Validate(); err != nil {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "authority binding is invalid", err)
	}
	if authorityBinding.AuthorityProfileDigest != verifiedAuthorityProfile.artifactDigest {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "authority binding profile digest differs from the verified authority profile", nil)
	}
	authorityExpiry, err := parseCanonicalUTCTime(authorityBinding.ExpiresAt)
	if err != nil || !now.Before(authorityExpiry) {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "authority binding is expired", err)
	}
	authorityBindingCanonical, authorityBindingDigest, err := canonicalVerifiedBinding(authorityBinding)
	if err != nil || authorityBindingCanonical == "" {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "authority binding cannot be canonically identified", err)
	}
	if err := verifiedAuthority.validateAt(now); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if verifiedAuthority.subjectDigest != authorityBindingDigest ||
		!verifiedAuthority.verifiedDecisionExpiresAt.Equal(authorityExpiry) ||
		verifiedAuthority.verifiedDecisionSecurityEpoch != uint64(authorityBinding.SecurityEpoch) ||
		!runnerCanonicalEqual(verifiedAuthority.expected, authorityBinding.ExpectedProjections) {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "verified authority differs from its signed binding", nil)
	}
	if err := initialSchemaScope.validateAt(now); err != nil {
		return VerifiedTrustDecision{}, err
	}
	if initialSchemaScope.subjectDigest != decision.expectedSchemaBundleDigest ||
		!initialSchemaScope.verifiedDecisionExpiresAt.Equal(decision.expiresAt) ||
		initialSchemaScope.verifiedDecisionSecurityEpoch != decision.securityEpoch {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "verified schema scope differs from the release decision", nil)
	}
	if len(verifiedCatalogs) == 0 {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "executable catalog set is empty", nil)
	}

	catalogs := make([]ExecutableCatalogBinding, len(verifiedCatalogs))
	previousHead := ""
	for index, verifiedSubject := range verifiedCatalogs {
		if err := verifiedSubject.validateAt(now); err != nil {
			return VerifiedTrustDecision{}, err
		}
		verifiedCatalog := verifiedSubject.verifiedCatalog
		head := verifiedCatalog.expected.SchemaHead
		if head == "" || (index > 0 && previousHead >= head) {
			return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "catalog subjects are duplicate or not canonically sorted", nil)
		}
		previousHead = head
		catalogs[index] = ExecutableCatalogBinding{
			schemaHead: head, catalogContractDigest: verifiedSubject.artifactDigest,
			catalogContract: cloneProjectionValue(verifiedSubject.contract), catalogCanonical: verifiedSubject.contractCanonical,
			verifiedCatalog: cloneVerifiedCatalogContract(verifiedCatalog),
			expiresAt:       verifiedCatalog.verifiedDecisionExpiresAt, securityEpoch: verifiedCatalog.verifiedDecisionSecurityEpoch,
		}
	}

	releaseSubject := releaseTrustDecisionSubject{
		Domain: releaseTrustDecisionDomain, RepositoryIdentity: decision.repositoryIdentity, ReleaseIdentity: decision.releaseIdentity,
		SchemaBundleDigest: decision.expectedSchemaBundleDigest, BootstrapBundleDigest: decision.expectedBootstrapBundleDigest,
		ManifestDigest: decision.expectedManifestDigest, OuterArtifactDigest: decision.expectedOuterArtifactDigest,
		RunnerReleaseDigest: decision.expectedRunnerReleaseDigest, ExpiresAt: canonicalProjectionExpiry(decision.expiresAt),
		SecurityEpoch: decision.securityEpoch,
	}
	releaseDigest, err := digestRunnerProjectionCanonical(releaseSubject)
	if err != nil {
		return VerifiedTrustDecision{}, err
	}
	catalogSubjects := catalogDecisionSubjects(catalogs)
	combinedSubject := runnerProjectionDecisionSubject{
		Domain: runnerProjectionDecisionDomain, ReleaseTrustDecisionDigest: releaseDigest,
		SchemaBundleDigest: decision.expectedSchemaBundleDigest, AuthorityProfileDigest: authorityBinding.AuthorityProfileDigest,
		AuthorityBindingDigest: authorityBindingDigest, AuthorityExpiresAt: authorityBinding.ExpiresAt,
		AuthoritySecurityEpoch:                uint64(authorityBinding.SecurityEpoch),
		RecoveryPolicySubjectDigest:           verifiedRecoveryPolicy.subjectDigest,
		DecisionRecoveryArtifactProfileDigest: decisionRecoveryArtifactProfileDigest,
		CatalogContracts:                      catalogSubjects,
	}
	combinedDigest, err := digestRunnerProjectionCanonical(combinedSubject)
	if err != nil {
		return VerifiedTrustDecision{}, err
	}
	if err := rejectCurrentRecoveryAuthorization(verifiedRecoveryPolicy, combinedDigest); err != nil {
		return VerifiedTrustDecision{}, err
	}
	lineageSubject := executionLineageSubject{
		Domain: executionLineageDomain, DeploymentID: authorityBinding.DeploymentID,
		ExpectedDatabaseIdentity: expectedDatabaseIdentity{DatabaseName: authorityBinding.ExpectedProjections.ConnectedSession.DatabaseName},
		RepositoryIdentity:       decision.repositoryIdentity,
	}
	lineageDigest, err := digestRunnerProjectionCanonical(lineageSubject)
	if err != nil {
		return VerifiedTrustDecision{}, err
	}
	bindings := RunnerProjectionBindings{
		verified: true, releaseTrustDecisionDigest: releaseDigest, runnerProjectionDecisionDigest: combinedDigest,
		executionLineageDigest: lineageDigest, schemaBundleDigest: decision.expectedSchemaBundleDigest,
		authorityProfileDigest: authorityBinding.AuthorityProfileDigest, authorityBindingDigest: authorityBindingDigest,
		recoveryPolicySubjectDigest:           verifiedRecoveryPolicy.subjectDigest,
		decisionRecoveryArtifactProfileDigest: decisionRecoveryArtifactProfileDigest,
		authorityBinding:                      cloneProjectionValue(authorityBinding), authorityBindingCanonical: authorityBindingCanonical,
		verifiedAuthorityProfile: verifiedAuthorityProfile.ownedCopy(),
		verifiedAuthority:        cloneVerifiedAuthorityContract(verifiedAuthority),
		verifiedRecoveryPolicy:   verifiedRecoveryPolicy.ownedCopy(), initialSchemaScope: cloneVerifiedSchemaBundleScope(initialSchemaScope),
		initialSchemaScopeBindingCanonical: initialSchemaScope.bindingCanonical,
		executableCatalogs:                 catalogs, releaseExpiresAt: decision.expiresAt, releaseSecurityEpoch: decision.securityEpoch,
		authorityExpiresAt: authorityExpiry, authoritySecurityEpoch: uint64(authorityBinding.SecurityEpoch),
		releaseSubject: releaseSubject, deploymentID: authorityBinding.DeploymentID,
		expectedDatabaseName: authorityBinding.ExpectedProjections.ConnectedSession.DatabaseName,
	}
	canonical, _, err := canonicalVerifiedBinding(bindings.sentinel())
	if err != nil {
		return VerifiedTrustDecision{}, fail(CodeUntrusted, "runner-projection-bindings", "projection binding sentinel cannot be canonicalized", err)
	}
	bindings.expectedCanonical = canonical
	if err := bindings.validateAt(now); err != nil {
		return VerifiedTrustDecision{}, err
	}
	owned := bindings.ownedCopy()
	decision.projectionBindings = &owned
	return decision, nil
}

func (bindings RunnerProjectionBindings) validateAt(now time.Time) error {
	if !bindings.verified || bindings.expectedCanonical == "" {
		return fail(CodeUntrusted, "runner-projection-bindings", "projection bindings were not produced by the trust verifier", nil)
	}
	for field, digest := range map[string]Digest{
		"release_decision": bindings.releaseTrustDecisionDigest, "runner_projection_decision": bindings.runnerProjectionDecisionDigest,
		"execution_lineage": bindings.executionLineageDigest, "schema_bundle": bindings.schemaBundleDigest,
		"authority_profile": bindings.authorityProfileDigest, "authority_binding": bindings.authorityBindingDigest,
		"recovery_policy_subject":            bindings.recoveryPolicySubjectDigest,
		"decision_recovery_artifact_profile": bindings.decisionRecoveryArtifactProfileDigest,
	} {
		if err := requireDigest("runner-projection-bindings."+field, digest); err != nil {
			return fail(CodeUntrusted, "runner-projection-bindings", "projection bindings contain an invalid digest", err)
		}
	}
	if bindings.releaseExpiresAt.IsZero() || !now.Before(bindings.releaseExpiresAt) || bindings.releaseSecurityEpoch == 0 ||
		bindings.authorityExpiresAt.IsZero() || !now.Before(bindings.authorityExpiresAt) || bindings.authoritySecurityEpoch == 0 {
		return fail(CodeUntrusted, "runner-projection-bindings", "projection binding expiry or epoch is invalid", nil)
	}
	if err := validateDecisionRecoveryArtifactProfileDigest(bindings.decisionRecoveryArtifactProfileDigest); err != nil {
		return err
	}
	if err := bindings.verifiedRecoveryPolicy.validateAt(now); err != nil {
		return err
	}
	if bindings.verifiedRecoveryPolicy.subjectDigest != bindings.recoveryPolicySubjectDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "verified recovery policy differs from the total recovery policy binding", nil)
	}
	if err := bindings.verifiedAuthority.validateAt(now); err != nil {
		return err
	}
	if bindings.verifiedAuthority.subjectDigest != bindings.authorityBindingDigest ||
		!bindings.verifiedAuthority.verifiedDecisionExpiresAt.Equal(bindings.authorityExpiresAt) ||
		bindings.verifiedAuthority.verifiedDecisionSecurityEpoch != bindings.authoritySecurityEpoch {
		return fail(CodeUntrusted, "runner-projection-bindings", "verified authority wrapper differs from the total authority binding", nil)
	}
	if err := bindings.authorityBinding.Validate(); err != nil {
		return fail(CodeUntrusted, "runner-projection-bindings", "frozen authority binding is invalid", err)
	}
	authorityCanonical, authorityDigest, authorityErr := canonicalVerifiedBinding(bindings.authorityBinding)
	authorityExpiry, expiryErr := parseCanonicalUTCTime(bindings.authorityBinding.ExpiresAt)
	if authorityErr != nil || expiryErr != nil || authorityCanonical != bindings.authorityBindingCanonical || authorityDigest != bindings.authorityBindingDigest ||
		bindings.authorityBinding.AuthorityProfileDigest != bindings.authorityProfileDigest ||
		!authorityExpiry.Equal(bindings.authorityExpiresAt) || uint64(bindings.authorityBinding.SecurityEpoch) != bindings.authoritySecurityEpoch ||
		bindings.authorityBinding.DeploymentID != bindings.deploymentID ||
		bindings.authorityBinding.ExpectedProjections.ConnectedSession.DatabaseName != bindings.expectedDatabaseName ||
		!runnerCanonicalEqual(bindings.verifiedAuthority.expected, bindings.authorityBinding.ExpectedProjections) {
		return fail(CodeUntrusted, "runner-projection-bindings", "frozen authority binding differs from the total projection binding", authorityErr)
	}
	if err := bindings.verifiedAuthorityProfile.validate(); err != nil {
		return err
	}
	if bindings.verifiedAuthorityProfile.artifactDigest != bindings.authorityProfileDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "authority profile body differs from its bound digest", nil)
	}
	if err := bindings.initialSchemaScope.validateAt(now); err != nil {
		return err
	}
	if bindings.initialSchemaScope.subjectDigest != bindings.schemaBundleDigest ||
		!bindings.initialSchemaScope.verifiedDecisionExpiresAt.Equal(bindings.releaseExpiresAt) ||
		bindings.initialSchemaScope.verifiedDecisionSecurityEpoch != bindings.releaseSecurityEpoch {
		return fail(CodeUntrusted, "runner-projection-bindings", "verified initial schema scope differs from the total release binding", nil)
	}
	if bindings.initialSchemaScope.bindingCanonical == "" || bindings.initialSchemaScope.bindingCanonical != bindings.initialSchemaScopeBindingCanonical {
		return fail(CodeUntrusted, "runner-projection-bindings", "verified initial schema scope differs from its frozen total binding", nil)
	}
	if len(bindings.executableCatalogs) == 0 || !sort.SliceIsSorted(bindings.executableCatalogs, func(i, j int) bool {
		return bindings.executableCatalogs[i].schemaHead < bindings.executableCatalogs[j].schemaHead
	}) {
		return fail(CodeUntrusted, "runner-projection-bindings", "catalog bindings are absent or unsorted", nil)
	}
	for index := range bindings.executableCatalogs {
		catalog := bindings.executableCatalogs[index]
		if index > 0 && bindings.executableCatalogs[index-1].schemaHead == catalog.schemaHead {
			return fail(CodeUntrusted, "runner-projection-bindings", "catalog bindings contain a duplicate head", nil)
		}
		if err := catalog.verifiedCatalog.validateAt(now); err != nil {
			return err
		}
		contractCanonical, contractErr := canonicalContractKey(catalog.catalogContract)
		if contractErr != nil || contractCanonical == "" || contractCanonical != catalog.catalogCanonical ||
			catalog.catalogContract.PublicationStatus != "PUBLISHED_IMMUTABLE" || catalog.catalogContract.RuntimeIntrospectionStatus != "IMPLEMENTED" || catalog.catalogContract.bootstrapShape {
			return fail(CodeUntrusted, "runner-projection-bindings", "catalog contract closure differs from its verified binding", contractErr)
		}
		if catalog.schemaHead != catalog.catalogContract.SchemaHead || catalog.schemaHead != catalog.verifiedCatalog.expected.SchemaHead || catalog.catalogContractDigest != catalog.verifiedCatalog.subjectDigest ||
			!runnerCanonicalEqual(catalog.catalogContract.ExpectedProjection, catalog.verifiedCatalog.expected) ||
			!catalog.expiresAt.Equal(catalog.verifiedCatalog.verifiedDecisionExpiresAt) || catalog.securityEpoch != catalog.verifiedCatalog.verifiedDecisionSecurityEpoch {
			return fail(CodeUntrusted, "runner-projection-bindings", "catalog binding differs from its verified wrapper", nil)
		}
	}
	releaseDigest, err := digestRunnerProjectionCanonical(bindings.releaseSubject)
	if err != nil || releaseDigest != bindings.releaseTrustDecisionDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "release decision digest differs from its canonical subject", err)
	}
	combinedDigest, err := digestRunnerProjectionCanonical(runnerProjectionDecisionSubject{
		Domain: runnerProjectionDecisionDomain, ReleaseTrustDecisionDigest: bindings.releaseTrustDecisionDigest,
		SchemaBundleDigest: bindings.schemaBundleDigest, AuthorityProfileDigest: bindings.authorityProfileDigest,
		AuthorityBindingDigest: bindings.authorityBindingDigest, AuthorityExpiresAt: canonicalProjectionExpiry(bindings.authorityExpiresAt),
		AuthoritySecurityEpoch:                bindings.authoritySecurityEpoch,
		RecoveryPolicySubjectDigest:           bindings.recoveryPolicySubjectDigest,
		DecisionRecoveryArtifactProfileDigest: bindings.decisionRecoveryArtifactProfileDigest,
		CatalogContracts:                      catalogDecisionSubjects(bindings.executableCatalogs),
	})
	if err != nil || combinedDigest != bindings.runnerProjectionDecisionDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "combined projection decision digest differs from its canonical subject", err)
	}
	if err := rejectCurrentRecoveryAuthorization(bindings.verifiedRecoveryPolicy, bindings.runnerProjectionDecisionDigest); err != nil {
		return err
	}
	lineageDigest, err := digestRunnerProjectionCanonical(executionLineageSubject{
		Domain: executionLineageDomain, DeploymentID: bindings.deploymentID,
		ExpectedDatabaseIdentity: expectedDatabaseIdentity{DatabaseName: bindings.expectedDatabaseName},
		RepositoryIdentity:       bindings.releaseSubject.RepositoryIdentity,
	})
	if err != nil || lineageDigest != bindings.executionLineageDigest {
		return fail(CodeUntrusted, "runner-projection-bindings", "execution lineage digest differs from its canonical subject", err)
	}
	canonical, _, err := canonicalVerifiedBinding(bindings.sentinel())
	if err != nil || canonical != bindings.expectedCanonical {
		return fail(CodeUntrusted, "runner-projection-bindings", "projection bindings differ from their immutable sentinel", err)
	}
	return nil
}

func (bindings RunnerProjectionBindings) exactlyMatches(other RunnerProjectionBindings) bool {
	now := time.Now()
	return bindings.validateAt(now) == nil && other.validateAt(now) == nil && bindings.expectedCanonical == other.expectedCanonical
}

// validateHistorical rechecks the complete immutable projection binding while
// deliberately not applying today's clock to an old decision. The historical
// verifier and current signed recovery policy remain responsible for expiry
// and revocation authorization.
func (bindings RunnerProjectionBindings) validateHistorical() error {
	expiries := []time.Time{
		bindings.releaseExpiresAt, bindings.authorityExpiresAt,
		bindings.verifiedRecoveryPolicy.expiresAt,
		bindings.verifiedAuthority.verifiedDecisionExpiresAt,
		bindings.initialSchemaScope.verifiedDecisionExpiresAt,
	}
	for _, catalog := range bindings.executableCatalogs {
		expiries = append(expiries, catalog.expiresAt, catalog.verifiedCatalog.verifiedDecisionExpiresAt)
	}
	var earliest time.Time
	for _, expiry := range expiries {
		if expiry.IsZero() {
			return fail(CodeUntrusted, "runner-projection-bindings", "historical projection binding expiry is unavailable", nil)
		}
		if earliest.IsZero() || expiry.Before(earliest) {
			earliest = expiry
		}
	}
	return bindings.validateAt(earliest.Add(-time.Nanosecond))
}

func (bindings RunnerProjectionBindings) historicallyExactlyMatches(other RunnerProjectionBindings) bool {
	return bindings.validateHistorical() == nil && other.validateHistorical() == nil && bindings.expectedCanonical == other.expectedCanonical
}

func (bindings RunnerProjectionBindings) sentinel() runnerProjectionBindingSentinel {
	return runnerProjectionBindingSentinel{
		ReleaseTrustDecisionDigest: bindings.releaseTrustDecisionDigest, RunnerProjectionDecisionDigest: bindings.runnerProjectionDecisionDigest,
		ExecutionLineageDigest: bindings.executionLineageDigest, SchemaBundleDigest: bindings.schemaBundleDigest,
		AuthorityProfileDigest: bindings.authorityProfileDigest, AuthorityBindingDigest: bindings.authorityBindingDigest,
		RecoveryPolicySubjectDigest:           bindings.recoveryPolicySubjectDigest,
		DecisionRecoveryArtifactProfileDigest: bindings.decisionRecoveryArtifactProfileDigest,
		RecoveryPolicyBindingCanonical:        bindings.verifiedRecoveryPolicy.expectedCanonical,
		AuthorityBindingCanonical:             bindings.authorityBindingCanonical,
		InitialSchemaScopeBindingCanonical:    bindings.initialSchemaScopeBindingCanonical,
		ReleaseExpiresAt:                      canonicalProjectionExpiry(bindings.releaseExpiresAt), ReleaseSecurityEpoch: bindings.releaseSecurityEpoch,
		AuthorityExpiresAt: canonicalProjectionExpiry(bindings.authorityExpiresAt), AuthoritySecurityEpoch: bindings.authoritySecurityEpoch,
		CatalogContracts: catalogDecisionSubjects(bindings.executableCatalogs), DeploymentID: bindings.deploymentID,
		ExpectedDatabaseName: bindings.expectedDatabaseName,
	}
}

func (bindings RunnerProjectionBindings) ownedCopy() RunnerProjectionBindings {
	owned := bindings
	owned.authorityBinding = cloneProjectionValue(bindings.authorityBinding)
	owned.verifiedAuthorityProfile = bindings.verifiedAuthorityProfile.ownedCopy()
	owned.verifiedAuthority = cloneVerifiedAuthorityContract(bindings.verifiedAuthority)
	owned.verifiedRecoveryPolicy = bindings.verifiedRecoveryPolicy.ownedCopy()
	owned.initialSchemaScope = cloneVerifiedSchemaBundleScope(bindings.initialSchemaScope)
	owned.executableCatalogs = make([]ExecutableCatalogBinding, len(bindings.executableCatalogs))
	for index, catalog := range bindings.executableCatalogs {
		owned.executableCatalogs[index] = catalog
		owned.executableCatalogs[index].catalogContract = cloneProjectionValue(catalog.catalogContract)
		owned.executableCatalogs[index].verifiedCatalog = cloneVerifiedCatalogContract(catalog.verifiedCatalog)
	}
	owned.releaseSubject = bindings.releaseSubject
	return owned
}

// bindVerifiedAuthorityProfileSubject is the only package-private promotion
// seam for exact authority profile artifact bytes.
func bindVerifiedAuthorityProfileSubject(raw []byte, artifactDigest Digest) (verifiedAuthorityProfileSubject, error) {
	if err := requireDigest("runner-projection-bindings.authority-profile", artifactDigest); err != nil || DigestBytes(raw) != artifactDigest {
		return verifiedAuthorityProfileSubject{}, fail(CodeUntrusted, "runner-projection-bindings", "authority profile artifact digest mismatch", err)
	}
	contract, err := DecodeAuthorityContract(append([]byte(nil), raw...))
	if err != nil {
		return verifiedAuthorityProfileSubject{}, err
	}
	if contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || contract.RuntimeIntrospectionStatus != "IMPLEMENTED" {
		return verifiedAuthorityProfileSubject{}, fail(CodeProjectionNotImplemented, "authority-profile", "authority profile is not published executable", nil)
	}
	canonical, err := canonicalContractKey(*contract)
	if err != nil {
		return verifiedAuthorityProfileSubject{}, fail(CodeUntrusted, "authority-profile", "authority profile cannot be canonically frozen", err)
	}
	subject := verifiedAuthorityProfileSubject{artifactDigest: artifactDigest, raw: append([]byte(nil), raw...), contract: cloneProjectionValue(*contract), contractCanonical: canonical}
	if err := subject.validate(); err != nil {
		return verifiedAuthorityProfileSubject{}, err
	}
	return subject, nil
}

func (subject verifiedAuthorityProfileSubject) validate() error {
	if err := requireDigest("runner-projection-bindings.authority-profile", subject.artifactDigest); err != nil {
		return err
	}
	if len(subject.raw) == 0 || DigestBytes(subject.raw) != subject.artifactDigest {
		return fail(CodeUntrusted, "authority-profile", "authority profile bytes differ from their descriptor digest", nil)
	}
	decoded, err := DecodeAuthorityContract(append([]byte(nil), subject.raw...))
	if err != nil {
		return err
	}
	if err := subject.contract.Validate(); err != nil {
		return err
	}
	if subject.contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || subject.contract.RuntimeIntrospectionStatus != "IMPLEMENTED" {
		return fail(CodeProjectionNotImplemented, "authority-profile", "authority profile is not published executable", nil)
	}
	canonical, err := canonicalContractKey(subject.contract)
	decodedCanonical, decodedErr := canonicalContractKey(*decoded)
	if err != nil || decodedErr != nil || canonical == "" || canonical != subject.contractCanonical || decodedCanonical != canonical || !runnerCanonicalEqual(*decoded, subject.contract) {
		return fail(CodeUntrusted, "authority-profile", "authority profile differs from its immutable binding", err)
	}
	return nil
}

func (subject verifiedAuthorityProfileSubject) ownedCopy() verifiedAuthorityProfileSubject {
	owned := subject
	owned.raw = append([]byte(nil), subject.raw...)
	owned.contract = cloneProjectionValue(subject.contract)
	return owned
}

// bindVerifiedExecutableCatalogSubject is the only promotion seam for an
// executable catalog artifact. It freezes both the complete decoded contract
// and the existing opaque projection wrapper under the same artifact digest.
func bindVerifiedExecutableCatalogSubject(raw []byte, artifactDigest Digest, expiresAt time.Time, securityEpoch uint64, now time.Time) (verifiedExecutableCatalogSubject, error) {
	if err := requireDigest("runner-projection-bindings.catalog", artifactDigest); err != nil || DigestBytes(raw) != artifactDigest {
		return verifiedExecutableCatalogSubject{}, fail(CodeUntrusted, "runner-projection-bindings", "catalog artifact digest mismatch", err)
	}
	contract, err := DecodeCatalogContract(append([]byte(nil), raw...))
	if err != nil {
		return verifiedExecutableCatalogSubject{}, err
	}
	if contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || contract.RuntimeIntrospectionStatus != "IMPLEMENTED" || contract.bootstrapShape {
		return verifiedExecutableCatalogSubject{}, fail(CodeProjectionNotImplemented, contract.SchemaHead, "catalog contract is not published executable", nil)
	}
	if expiresAt.IsZero() || !now.Before(expiresAt) || securityEpoch == 0 {
		return verifiedExecutableCatalogSubject{}, fail(CodeUntrusted, contract.SchemaHead, "catalog trust decision is missing or expired", nil)
	}
	head := contract.SchemaHead
	scope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: cloneObjectIdentities(contract.DeclaredObjectIdentities)}
	verified, err := bindVerifiedCatalogContract(artifactDigest, scope, contract.ExpectedProjection, expiresAt, securityEpoch)
	if err != nil {
		return verifiedExecutableCatalogSubject{}, err
	}
	canonical, err := canonicalContractKey(*contract)
	if err != nil {
		return verifiedExecutableCatalogSubject{}, fail(CodeUntrusted, contract.SchemaHead, "catalog contract cannot be canonically frozen", err)
	}
	subject := verifiedExecutableCatalogSubject{
		artifactDigest: artifactDigest, contract: cloneProjectionValue(*contract), contractCanonical: canonical,
		verifiedCatalog: cloneVerifiedCatalogContract(verified),
	}
	if err := subject.validateAt(now); err != nil {
		return verifiedExecutableCatalogSubject{}, err
	}
	return subject, nil
}

func (subject verifiedExecutableCatalogSubject) validateAt(now time.Time) error {
	if err := requireDigest("runner-projection-bindings.catalog", subject.artifactDigest); err != nil {
		return err
	}
	canonical, err := canonicalContractKey(subject.contract)
	if err != nil || canonical == "" || canonical != subject.contractCanonical {
		return fail(CodeUntrusted, "runner-projection-bindings", "catalog contract differs from its immutable binding", err)
	}
	if subject.contract.PublicationStatus != "PUBLISHED_IMMUTABLE" || subject.contract.RuntimeIntrospectionStatus != "IMPLEMENTED" || subject.contract.bootstrapShape {
		return fail(CodeProjectionNotImplemented, subject.contract.SchemaHead, "catalog contract is not published executable", nil)
	}
	if err := subject.verifiedCatalog.validateAt(now); err != nil {
		return err
	}
	if subject.verifiedCatalog.subjectDigest != subject.artifactDigest || subject.verifiedCatalog.expected.SchemaHead != subject.contract.SchemaHead ||
		!runnerCanonicalEqual(subject.verifiedCatalog.expected, subject.contract.ExpectedProjection) {
		return fail(CodeUntrusted, subject.contract.SchemaHead, "catalog projection wrapper differs from the complete catalog subject", nil)
	}
	return nil
}

func runnerCanonicalEqual(left, right any) bool {
	leftCanonical, leftErr := canonicalContractKey(left)
	rightCanonical, rightErr := canonicalContractKey(right)
	return leftErr == nil && rightErr == nil && leftCanonical == rightCanonical
}

func catalogDecisionSubjects(catalogs []ExecutableCatalogBinding) []runnerCatalogDecisionSubject {
	subjects := make([]runnerCatalogDecisionSubject, len(catalogs))
	for index, catalog := range catalogs {
		subjects[index] = runnerCatalogDecisionSubject{
			SchemaHead: catalog.schemaHead, CatalogContractDigest: catalog.catalogContractDigest,
			ExpiresAt: canonicalProjectionExpiry(catalog.expiresAt), SecurityEpoch: catalog.securityEpoch,
		}
	}
	return subjects
}

func digestRunnerProjectionCanonical(value any) (Digest, error) {
	canonical, err := canonicalContractKey(value)
	if err != nil {
		return "", fail(CodeUntrusted, "runner-projection-bindings", "canonical decision encoding failed", err)
	}
	return DigestBytes([]byte(canonical)), nil
}

func cloneVerifiedAuthorityContract(contract VerifiedAuthorityContract) VerifiedAuthorityContract {
	owned := contract
	owned.expected = cloneProjectionValue(contract.expected)
	return owned
}

func cloneVerifiedCatalogContract(contract VerifiedCatalogContract) VerifiedCatalogContract {
	owned := contract
	owned.scope = cloneProjectionValue(contract.scope)
	owned.expected = cloneProjectionValue(contract.expected)
	return owned
}

func cloneVerifiedSchemaBundleScope(scope VerifiedSchemaBundleScope) VerifiedSchemaBundleScope {
	owned := scope
	owned.scope = cloneProjectionValue(scope.scope)
	owned.defaultACLOwners = append([]string(nil), scope.defaultACLOwners...)
	owned.objectCreatorClosure = append([]string(nil), scope.objectCreatorClosure...)
	owned.boundPrecondition = cloneProjectionValue(scope.boundPrecondition)
	return owned
}
