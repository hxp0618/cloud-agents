package migration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

type recoveryVerifierFake struct {
	historicalCalls, supersessionPolicyCalls, supersessionCalls int
	historicalDecision                                          VerifiedTrustDecision
	historicalBindings                                          RunnerProjectionBindings
	historicalSubject                                           historicalRecoveryPolicySubject
	supersessionPolicies                                        map[Digest]historicalRecoveryPolicySubject
}

func (*recoveryVerifierFake) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return VerifiedTrustDecision{}, fail(CodeUntrusted, "fake", "ordinary verification is outside recovery ABI tests", nil)
}
func (f *recoveryVerifierFake) recoverHistoricalDecision(context.Context, VerifiedTrustDecision, GenerationDescriptor, VerifiedDecisionRecoveryArtifact) (VerifiedTrustDecision, RunnerProjectionBindings, historicalRecoveryPolicySubject, error) {
	f.historicalCalls++
	if f.historicalDecision.verified {
		return f.historicalDecision, f.historicalBindings, f.historicalSubject, nil
	}
	return VerifiedTrustDecision{}, RunnerProjectionBindings{}, historicalRecoveryPolicySubject{}, fail(CodeEvidenceRecoveryRequired, "fake-recovery", "test stop", nil)
}
func (f *recoveryVerifierFake) recoverHistoricalSupersessionPolicy(_ context.Context, _ VerifiedTrustDecision, source GenerationDescriptor, _ VerifiedDecisionRecoveryArtifact, _ GenerationDescriptor, _ VerifiedDecisionRecoveryArtifact, _ GenerationSuperseded) (historicalRecoveryPolicySubject, error) {
	f.supersessionPolicyCalls++
	if subject, ok := f.supersessionPolicies[source.identity.runnerProjectionDecisionDigest]; ok {
		return cloneProjectionValue(subject), nil
	}
	return historicalRecoveryPolicySubject{}, fail(CodeEvidenceRecoveryRequired, "fake-recovery", "test stop", nil)
}
func (f *recoveryVerifierFake) recoverHistoricalSupersession(context.Context, VerifiedTrustDecision, *VerifiedLineageSupersessionAuthority, GenerationSuperseded, VerifiedDecisionRecoveryArtifact, VerifiedRuntimeArtifact, VerifiedContentReceipt, VerifiedDecisionRecoveryArtifact, VerifiedDecisionRecoveryReceipt) (*verifiedHistoricalSupersessionReceipt, error) {
	f.supersessionCalls++
	return nil, fail(CodeEvidenceRecoveryRequired, "fake-recovery", "test stop", nil)
}

func TestSameVerifierRecoveryCapabilityRejectsSwapAndRejectingVerifier(t *testing.T) {
	decisionDigest := DigestBytes([]byte("current-decision"))
	artifactBytes := []byte("owned recovery artifact")
	fakeA, fakeB := &recoveryVerifierFake{}, &recoveryVerifierFake{}
	tokenA, tokenB := &evidenceOwnerToken{nonce: [16]byte{1}}, &evidenceOwnerToken{nonce: [16]byte{2}}
	ownerA, ownerB := &recoveryVerifierOwner{fakeA, tokenA}, &recoveryVerifierOwner{fakeB, tokenB}
	artifactA := VerifiedDecisionRecoveryArtifact{ownerA, artifactBytes, DigestBytes(artifactBytes), uint64(len(artifactBytes)), decisionDigest}

	if _, err := bindOwnedVerifiedDecision(RejectingTrustVerifier{}, VerifiedTrustDecision{}, decisionDigest, artifactA); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("rejecting verifier gained recovery capability: %v", err)
	}
	current := OwnedVerifiedDecision{ownerA, VerifiedTrustDecision{}, decisionDigest, sameVerifierRecoveryCapability{ownerA}}
	oldDigest := DigestBytes([]byte("old-decision"))
	recoveryDigest := DigestBytes(artifactBytes)
	identity := generationIdentity{tokenA, DigestBytes([]byte("lineage")), DigestBytes([]byte("journal")), oldDigest, DigestBytes([]byte("schema"))}
	generation := GenerationDescriptor{identity: identity, header: JournalHeader{ExecutionLineageDigest: identity.executionLineageDigest, JournalIdentityDigest: identity.journalIdentityDigest, RunnerProjectionDecisionDigest: oldDigest, SchemaBundleDigest: identity.schemaBundleDigest, DecisionRecoveryArtifactSHA256: recoveryDigest, DecisionRecoveryArtifactSizeBytes: uint64(len(artifactBytes))}, recoveryArtifactDigest: recoveryDigest, recoveryArtifactSize: uint64(len(artifactBytes))}
	swapped := VerifiedDecisionRecoveryArtifact{ownerB, artifactBytes, DigestBytes(artifactBytes), uint64(len(artifactBytes)), oldDigest}
	if _, _, _, err := current.recoverHistoricalDecision(context.Background(), generation, swapped); !IsCode(err, CodeEvidenceRecoveryRequired) || fakeA.historicalCalls != 0 || fakeB.historicalCalls != 0 {
		t.Fatalf("cross-verifier swap reached verifier: err=%v calls=%d/%d", err, fakeA.historicalCalls, fakeB.historicalCalls)
	}
	ownedOld := VerifiedDecisionRecoveryArtifact{ownerA, artifactBytes, DigestBytes(artifactBytes), uint64(len(artifactBytes)), oldDigest}
	if _, _, _, err := current.recoverHistoricalDecision(context.Background(), generation, ownedOld); !IsCode(err, CodeEvidenceRecoveryRequired) || fakeA.historicalCalls != 0 || fakeB.historicalCalls != 0 {
		t.Fatalf("unvalidated current decision reached verifier: err=%v calls=%d/%d", err, fakeA.historicalCalls, fakeB.historicalCalls)
	}
}

func TestRegisteredHistoricalRecoveryArtifactBinderOwnsCanonicalBytesAndRejectsEverySwap(t *testing.T) {
	inputs := decisionRecoveryVerificationInputs{
		FormatVersion: decisionRecoveryArtifactFormatVersion, ProfileDigest: decisionRecoveryArtifactProfileDigest,
		OldRunnerProjectionDecisionDigest: DigestBytes([]byte("old-decision")), RepositoryIdentity: "hxp0618/cloud-agents", ReleaseIdentity: "v0.0.0-old",
		CandidateSubjectBase64URLNoPadding: "Y2FuZGlkYXRl", CandidateDetachedEnvelopeBase64URLNoPadding: "c2lnbmF0dXJl",
		ProjectionSubjectInputs: []decisionRecoveryProjectionSubjectInput{
			{Kind: "release", SubjectDigest: DigestBytes([]byte("release")), SubjectBase64URLNoPadding: "cmVsZWFzZQ", DetachedEnvelopeBase64URLNoPadding: "c2ln"},
			{Kind: "authority_profile", SubjectDigest: DigestBytes([]byte("profile")), SubjectBase64URLNoPadding: "cHJvZmlsZQ", DetachedEnvelopeBase64URLNoPadding: "c2ln"},
			{Kind: "authority_binding", SubjectDigest: DigestBytes([]byte("binding")), SubjectBase64URLNoPadding: "YmluZGluZw", DetachedEnvelopeBase64URLNoPadding: "c2ln"},
		},
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil {
		t.Fatal(err)
	}
	bytes := []byte(canonical)
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), nil)
	current := candidate.verifiedRun.currentDecision
	owner, token := current.owner, current.owner.token
	identity := generationIdentity{owner: token, executionLineageDigest: DigestBytes([]byte("lineage")), journalIdentityDigest: DigestBytes([]byte("journal")), runnerProjectionDecisionDigest: inputs.OldRunnerProjectionDecisionDigest, schemaBundleDigest: DigestBytes([]byte("schema"))}
	header := JournalHeader{
		FormatVersion: EvidenceJournalFormat, JournalIdentityDigest: identity.journalIdentityDigest, ReleaseTrustDecisionDigest: DigestBytes([]byte("release-decision")), RunnerProjectionDecisionDigest: identity.runnerProjectionDecisionDigest,
		ExecutionLineageDigest: identity.executionLineageDigest, OuterArtifactDigest: DigestBytes([]byte("runtime")), OuterArtifactSizeBytes: 1, DecisionRecoveryArtifactSHA256: DigestBytes(bytes), DecisionRecoveryArtifactSizeBytes: uint64(len(bytes)),
		ManifestDigest: DigestBytes([]byte("manifest")), RunnerReleaseDigest: DigestBytes([]byte("runner")), SchemaBundleDigest: identity.schemaBundleDigest, AuthorityProfileDigest: DigestBytes([]byte("authority-profile")), AuthorityBindingDigest: DigestBytes([]byte("authority-binding")),
		LimitsProfile: EvidenceLimitsProfile, QuotaReservationDigest: DigestBytes([]byte("quota")), ReservedRecords: 1, ReservedBytes: 1, ReservedSegments: 1,
	}
	journalDigest, err := JournalIdentityDigest(header)
	if err != nil {
		t.Fatal(err)
	}
	header.JournalIdentityDigest, identity.journalIdentityDigest = journalDigest, journalDigest
	generation := GenerationDescriptor{identity: identity, header: header, replayTailDigest: DigestBytes([]byte("tail")), recoveryArtifactDigest: DigestBytes(bytes), recoveryArtifactSize: uint64(len(bytes))}

	artifact, err := bindHistoricalRecoveryVerifierInput(current, generation, bytes)
	if err != nil || artifact.owner != owner || artifact.decision != inputs.OldRunnerProjectionDecisionDigest || artifact.digest != DigestBytes(bytes) || string(artifact.bytes) != string(bytes) {
		t.Fatalf("registered historical artifact did not bind: artifact=%+v err=%v", artifact, err)
	}
	bytes[0] ^= 1
	if string(artifact.bytes) == string(bytes) {
		t.Fatal("bound historical artifact aliases caller bytes")
	}
	verifier := owner.verifier.(*recoveryVerifierFake)
	if _, _, _, err := current.recoverHistoricalDecision(t.Context(), generation, artifact); !IsCode(err, CodeEvidenceRecoveryRequired) || verifier.historicalCalls != 0 {
		t.Fatalf("unauthorized old decision reached the current verifier: err=%v calls=%d", err, verifier.historicalCalls)
	}

	for name, mutate := range map[string]func(*OwnedVerifiedDecision, *GenerationDescriptor, *[]byte){
		"zero-current": func(c *OwnedVerifiedDecision, _ *GenerationDescriptor, _ *[]byte) { *c = OwnedVerifiedDecision{} },
		"foreign-generation": func(_ *OwnedVerifiedDecision, g *GenerationDescriptor, _ *[]byte) {
			g.identity.owner = &evidenceOwnerToken{}
		},
		"decision": func(_ *OwnedVerifiedDecision, g *GenerationDescriptor, _ *[]byte) {
			g.identity.runnerProjectionDecisionDigest = DigestBytes([]byte("other-decision"))
		},
		"digest": func(_ *OwnedVerifiedDecision, g *GenerationDescriptor, _ *[]byte) {
			g.recoveryArtifactDigest = DigestBytes([]byte("other-artifact"))
		},
		"size": func(_ *OwnedVerifiedDecision, g *GenerationDescriptor, _ *[]byte) { g.recoveryArtifactSize++ },
		"noncanonical": func(_ *OwnedVerifiedDecision, g *GenerationDescriptor, raw *[]byte) {
			*raw = append([]byte(" "), *raw...)
			g.recoveryArtifactDigest, g.recoveryArtifactSize = DigestBytes(*raw), uint64(len(*raw))
			g.header.DecisionRecoveryArtifactSHA256, g.header.DecisionRecoveryArtifactSizeBytes = g.recoveryArtifactDigest, g.recoveryArtifactSize
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidateCurrent, candidateGeneration, candidateBytes := current, generation, []byte(canonical)
			mutate(&candidateCurrent, &candidateGeneration, &candidateBytes)
			if _, err := bindHistoricalRecoveryVerifierInput(candidateCurrent, candidateGeneration, candidateBytes); !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("historical recovery swap accepted: %v", err)
			}
		})
	}
}

func TestHistoricalRecoveryVerifierInputHasNoProductionCallerBeforeAdmissionPlan(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "evidence_trust_recovery.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("bindHistoricalRecoveryVerifierInput(")) && name != "evidence_admission_history.go" {
			t.Fatalf("historical recovery input binder has premature production caller in %s", name)
		}
	}
}

func TestHistoricalProjectionValidationIgnoresOnlyElapsedClock(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), nil)
	decision := candidate.verifiedRun.currentDecision.decision
	bindings, err := decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if err := bindings.validateAt(time.Now().Add(48 * time.Hour)); !IsCode(err, CodeUntrusted) {
		t.Fatalf("future clock did not expire ordinary bindings: %v", err)
	}
	if err := bindings.validateHistorical(); err != nil {
		t.Fatalf("immutable historical binding was rejected: %v", err)
	}
	if err := decision.validateHistorical(bindings); err != nil {
		t.Fatalf("historical decision did not match recovered bindings: %v", err)
	}

	mutatedBindings := bindings
	mutatedBindings.expectedCanonical += "x"
	if err := mutatedBindings.validateHistorical(); !IsCode(err, CodeUntrusted) {
		t.Fatalf("mutated historical binding accepted: %v", err)
	}
	mutatedDecision := decision
	mutatedDecision.expectedManifestDigest = DigestBytes([]byte("other-manifest"))
	if err := mutatedDecision.validateHistorical(bindings); !IsCode(err, CodeUntrusted) {
		t.Fatalf("mutated historical decision accepted: %v", err)
	}
	expiredDecision, expiredBindings := decision, bindings.ownedCopy()
	expired := time.Now().Add(-time.Hour)
	expiredDecision.expiresAt, expiredBindings.releaseExpiresAt = expired, expired
	expiredBindings.releaseSubject.ExpiresAt = canonicalProjectionExpiry(expired)
	expiredBindings.releaseTrustDecisionDigest, err = digestRunnerProjectionCanonical(expiredBindings.releaseSubject)
	if err != nil {
		t.Fatal(err)
	}
	expiredBindings.runnerProjectionDecisionDigest, err = digestRunnerProjectionCanonical(runnerProjectionDecisionSubject{
		Domain: runnerProjectionDecisionDomain, ReleaseTrustDecisionDigest: expiredBindings.releaseTrustDecisionDigest,
		SchemaBundleDigest: expiredBindings.schemaBundleDigest, AuthorityProfileDigest: expiredBindings.authorityProfileDigest,
		AuthorityBindingDigest: expiredBindings.authorityBindingDigest, AuthorityExpiresAt: canonicalProjectionExpiry(expiredBindings.authorityExpiresAt), AuthoritySecurityEpoch: expiredBindings.authoritySecurityEpoch,
		RecoveryPolicySubjectDigest: expiredBindings.recoveryPolicySubjectDigest, DecisionRecoveryArtifactProfileDigest: expiredBindings.decisionRecoveryArtifactProfileDigest,
		CatalogContracts: catalogDecisionSubjects(expiredBindings.executableCatalogs),
	})
	if err != nil {
		t.Fatal(err)
	}
	expiredBindings.initialSchemaScope.verifiedDecisionExpiresAt = expired
	expiredBindings.initialSchemaScope.bindingCanonical, expiredBindings.initialSchemaScope.bindingDigest, err = canonicalVerifiedBinding(expiredBindings.initialSchemaScope.schemaBundleBinding())
	if err != nil {
		t.Fatal(err)
	}
	expiredBindings.initialSchemaScopeBindingCanonical = expiredBindings.initialSchemaScope.bindingCanonical
	expiredBindings.expectedCanonical, _, err = canonicalVerifiedBinding(expiredBindings.sentinel())
	if err != nil {
		t.Fatal(err)
	}
	expiredDecision.projectionBindings = &expiredBindings
	if err := expiredDecision.validate(); !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired decision passed ordinary validation: %v", err)
	}
	if err := expiredDecision.validateHistorical(expiredBindings); err != nil {
		t.Fatalf("expired but immutable historical decision was rejected: %v", err)
	}
}

func TestHistoricalVerifierOutputIsTotallyBoundToGenerationAndCurrentPolicy(t *testing.T) {
	oldFixture := newRunnerBindingFixture(t, []string{"000001"})
	old, err := bindVerifiedRunnerProjectionDecision(oldFixture.decision, oldFixture.authorityProfile, oldFixture.authorityBinding, oldFixture.authority, oldFixture.recoveryPolicy, oldFixture.initialScope, oldFixture.catalogs, oldFixture.now)
	if err != nil {
		t.Fatal(err)
	}
	oldBindings, err := old.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	currentFixture := newRunnerBindingFixture(t, []string{"000001"})
	currentFixture.recoveryPolicy, err = bindVerifiedRecoveryPolicySubject(recoveryPolicySignedSubject{
		Domain: recoveryPolicySubjectDomain, IssuerKeyIdentityDigest: testDigest("recovery-policy-issuer"), ExpiresAt: currentFixture.expiresAt.Format(time.RFC3339), SecurityEpoch: 1, MinimumOldSecurityEpoch: 1,
		OldRevocationPolicyDigest: testDigest("old-revocation-policy"), OldDecisionAuthorizations: []oldDecisionAuthorization{{OldRunnerProjectionDecisionDigest: oldBindings.runnerProjectionDecisionDigest, AllowExpired: true}},
	}, 1, currentFixture.now)
	if err != nil {
		t.Fatal(err)
	}
	currentDecision, err := bindVerifiedRunnerProjectionDecision(currentFixture.decision, currentFixture.authorityProfile, currentFixture.authorityBinding, currentFixture.authority, currentFixture.recoveryPolicy, currentFixture.initialScope, currentFixture.catalogs, currentFixture.now)
	if err != nil {
		t.Fatal(err)
	}
	currentBindings, err := currentDecision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	projection := func(kind, value string) decisionRecoveryProjectionSubjectInput {
		raw := []byte(value)
		return decisionRecoveryProjectionSubjectInput{Kind: kind, SubjectDigest: DigestBytes(raw), SubjectBase64URLNoPadding: base64.RawURLEncoding.EncodeToString(raw), DetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("sig-" + kind))}
	}
	currentInputs := decisionRecoveryVerificationInputs{
		FormatVersion: decisionRecoveryArtifactFormatVersion, ProfileDigest: currentBindings.decisionRecoveryArtifactProfileDigest,
		OldRunnerProjectionDecisionDigest: currentBindings.runnerProjectionDecisionDigest, RepositoryIdentity: currentDecision.repositoryIdentity, ReleaseIdentity: currentDecision.releaseIdentity,
		CandidateSubjectBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("current-candidate")), CandidateDetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("current-signature")),
		ProjectionSubjectInputs: []decisionRecoveryProjectionSubjectInput{projection("release", "current-release"), projection("authority_profile", "current-profile"), projection("authority_binding", "current-binding")},
	}
	currentCanonical, err := canonicalContractKey(currentInputs)
	if err != nil {
		t.Fatal(err)
	}
	fake := &recoveryVerifierFake{}
	current, _, err := bindVerifierOwnedDecision(fake, currentDecision, currentBindings.runnerProjectionDecisionDigest, []byte(currentCanonical))
	if err != nil {
		t.Fatal(err)
	}
	if oldBindings.executionLineageDigest != currentBindings.executionLineageDigest || oldBindings.runnerProjectionDecisionDigest == current.digest {
		t.Fatal("historical fixture is not a strict old decision in the current lineage")
	}
	inputs := decisionRecoveryVerificationInputs{
		FormatVersion: decisionRecoveryArtifactFormatVersion, ProfileDigest: currentBindings.decisionRecoveryArtifactProfileDigest,
		OldRunnerProjectionDecisionDigest: oldBindings.runnerProjectionDecisionDigest, RepositoryIdentity: old.repositoryIdentity, ReleaseIdentity: old.releaseIdentity,
		CandidateSubjectBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("old-candidate")), CandidateDetachedEnvelopeBase64URLNoPadding: base64.RawURLEncoding.EncodeToString([]byte("old-signature")),
		ProjectionSubjectInputs: []decisionRecoveryProjectionSubjectInput{projection("release", "old-release"), projection("authority_profile", "old-profile"), projection("authority_binding", "old-binding")},
	}
	canonical, err := canonicalContractKey(inputs)
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes := []byte(canonical)
	header := JournalHeader{
		FormatVersion: EvidenceJournalFormat, ReleaseTrustDecisionDigest: oldBindings.releaseTrustDecisionDigest, RunnerProjectionDecisionDigest: oldBindings.runnerProjectionDecisionDigest,
		ExecutionLineageDigest: oldBindings.executionLineageDigest, OuterArtifactDigest: old.expectedOuterArtifactDigest, OuterArtifactSizeBytes: 1,
		DecisionRecoveryArtifactSHA256: DigestBytes(artifactBytes), DecisionRecoveryArtifactSizeBytes: uint64(len(artifactBytes)), ManifestDigest: old.expectedManifestDigest,
		RunnerReleaseDigest: old.expectedRunnerReleaseDigest, SchemaBundleDigest: old.expectedSchemaBundleDigest, AuthorityProfileDigest: oldBindings.authorityProfileDigest,
		AuthorityBindingDigest: oldBindings.authorityBindingDigest, LimitsProfile: EvidenceLimitsProfile, QuotaReservationDigest: DigestBytes([]byte("old-quota")), ReservedRecords: 1, ReservedBytes: 1, ReservedSegments: 1,
	}
	header.JournalIdentityDigest, err = JournalIdentityDigest(header)
	if err != nil {
		t.Fatal(err)
	}
	identity := generationIdentity{current.owner.token, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
	generation := GenerationDescriptor{identity: identity, header: header, replayTailDigest: DigestBytes([]byte("old-tail")), recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes}
	subject := recoveryPolicyFixtureSubject(identity)
	subject.RecoveryPolicySubjectDigest = currentBindings.recoveryPolicySubjectDigest
	subject.OldDecisionRecoveryArtifactSHA256, subject.OldDecisionRecoveryArtifactSizeBytes = generation.recoveryArtifactDigest, generation.recoveryArtifactSize
	subject.SuccessorRunnerProjectionDecisionDigest, subject.SuccessorSchemaBundleDigest = current.digest, current.decision.expectedSchemaBundleDigest
	fake.historicalDecision, fake.historicalBindings, fake.historicalSubject = old, oldBindings, subject
	artifact, err := bindHistoricalRecoveryVerifierInput(current, generation, artifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	ownedOld, recoveredBindings, policy, err := current.recoverHistoricalDecision(t.Context(), generation, artifact)
	if err != nil || ownedOld.owner != current.owner || ownedOld.digest != oldBindings.runnerProjectionDecisionDigest || !recoveredBindings.historicallyExactlyMatches(oldBindings) || policy.owner != current.owner || fake.historicalCalls != 1 {
		t.Fatalf("historical verifier output was not totally bound: old=%+v policy=%+v calls=%d err=%v", ownedOld, policy, fake.historicalCalls, err)
	}

	fake.historicalCalls = 0
	fake.historicalSubject.SuccessorSchemaBundleDigest = DigestBytes([]byte("wrong-successor-schema"))
	if _, _, _, err := current.recoverHistoricalDecision(t.Context(), generation, artifact); !IsCode(err, CodeEvidenceRecoveryRequired) || fake.historicalCalls != 1 {
		t.Fatalf("mismatched verifier output was accepted or skipped verifier: err=%v calls=%d", err, fake.historicalCalls)
	}
}

func TestHistoricalRuntimeLoaderRequiresRecoveredAuthorityAndExactRegisteredBytes(t *testing.T) {
	raw, manifest := buildCheckedInRuntimeTar(t)
	oldFixture := newRunnerBindingFixture(t, []string{"000001"})
	oldFixture.decision.expectedSchemaBundleDigest = manifest.SchemaBundleDigest
	oldFixture.decision.expectedBootstrapBundleDigest = manifest.BootstrapBundleDigest
	oldFixture.decision.expectedManifestDigest = manifest.ManifestDigest
	oldFixture.decision.expectedOuterArtifactDigest = DigestBytes(raw)
	oldFixture.initialScope, _ = bindVerifiedSchemaBundleScope(manifest.SchemaBundleDigest, oldFixture.initialScope.Scope(), oldFixture.initialScope.BoundPrecondition(), oldFixture.initialScope.DefaultACLOwners(), oldFixture.initialScope.ObjectCreatorClosure(), oldFixture.expiresAt, 1)
	old, err := bindVerifiedRunnerProjectionDecision(oldFixture.decision, oldFixture.authorityProfile, oldFixture.authorityBinding, oldFixture.authority, oldFixture.recoveryPolicy, oldFixture.initialScope, oldFixture.catalogs, oldFixture.now)
	if err != nil {
		t.Fatal(err)
	}
	oldBindings, err := old.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	current := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), nil).verifiedRun.currentDecision
	currentOwner := current.owner
	currentBindings, err := current.decision.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	if currentBindings.executionLineageDigest != oldBindings.executionLineageDigest {
		t.Fatal("historical and current fixture lineages differ")
	}
	ownedOld := OwnedVerifiedDecision{owner: currentOwner, decision: old, digest: oldBindings.runnerProjectionDecisionDigest, capability: sameVerifierRecoveryCapability{owner: currentOwner}}
	header := JournalHeader{
		FormatVersion: EvidenceJournalFormat, ReleaseTrustDecisionDigest: oldBindings.releaseTrustDecisionDigest, RunnerProjectionDecisionDigest: oldBindings.runnerProjectionDecisionDigest, ExecutionLineageDigest: oldBindings.executionLineageDigest,
		OuterArtifactDigest: DigestBytes(raw), OuterArtifactSizeBytes: uint64(len(raw)), DecisionRecoveryArtifactSHA256: DigestBytes([]byte("recovery")), DecisionRecoveryArtifactSizeBytes: 1,
		ManifestDigest: manifest.ManifestDigest, RunnerReleaseDigest: old.expectedRunnerReleaseDigest, SchemaBundleDigest: manifest.SchemaBundleDigest, AuthorityProfileDigest: oldBindings.authorityProfileDigest,
		AuthorityBindingDigest: oldBindings.authorityBindingDigest, LimitsProfile: EvidenceLimitsProfile, QuotaReservationDigest: DigestBytes([]byte("quota")), ReservedRecords: 1, ReservedBytes: 1, ReservedSegments: 1,
	}
	header.JournalIdentityDigest, err = JournalIdentityDigest(header)
	if err != nil {
		t.Fatal(err)
	}
	identity := generationIdentity{currentOwner.token, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
	generation := GenerationDescriptor{identity: identity, header: header, replayTailDigest: DigestBytes([]byte("tail")), recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes}
	policySubject := recoveryPolicyFixtureSubject(identity)
	policySubject.RecoveryPolicySubjectDigest = currentBindings.recoveryPolicySubjectDigest
	policySubject.OldDecisionRecoveryArtifactSHA256, policySubject.OldDecisionRecoveryArtifactSizeBytes = generation.recoveryArtifactDigest, generation.recoveryArtifactSize
	policySubject.SuccessorRunnerProjectionDecisionDigest, policySubject.SuccessorSchemaBundleDigest = current.digest, DigestBytes([]byte("current-schema"))
	policy := VerifiedHistoricalRecoveryPolicy{owner: currentOwner, subject: policySubject}
	policy.digest, err = policySubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := loadHistoricalRuntimeBundle(current, ownedOld, oldBindings, policy, generation, raw)
	if err != nil || bundle == nil || bundle.Manifest.ManifestDigest != manifest.ManifestDigest || !bundle.quotaFacts.valid() {
		t.Fatalf("historical runtime bundle did not bind: bundle=%+v err=%v", bundle, err)
	}
	mutated := append([]byte(nil), raw...)
	mutated[len(mutated)/2] ^= 1
	if _, err := loadHistoricalRuntimeBundle(current, ownedOld, oldBindings, policy, generation, mutated); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("registered runtime digest swap was accepted: %v", err)
	}
	foreign := ownedOld
	foreign.owner = &recoveryVerifierOwner{verifier: currentOwner.verifier, token: currentOwner.token}
	if _, err := loadHistoricalRuntimeBundle(current, foreign, oldBindings, policy, generation, raw); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("foreign recovered decision owner accepted: %v", err)
	}
	mutatedPolicy := policy
	mutatedPolicy.digest = DigestBytes([]byte("other-policy"))
	if _, err := loadHistoricalRuntimeBundle(current, ownedOld, oldBindings, mutatedPolicy, generation, raw); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("mutated historical policy accepted: %v", err)
	}
	mutatedHeader := generation
	mutatedHeader.header.ManifestDigest = DigestBytes([]byte("other-manifest"))
	if _, err := loadHistoricalRuntimeBundle(current, ownedOld, oldBindings, policy, mutatedHeader, raw); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("header and recovered decision mismatch accepted: %v", err)
	}
}

func TestRecoveredHistoricalSupersessionPolicyBindsOriginalIntermediateSuccessor(t *testing.T) {
	token := &evidenceOwnerToken{nonce: [16]byte{31}}
	owner := &recoveryVerifierOwner{verifier: &recoveryVerifierFake{}, token: token}
	sourceIdentity := generationIdentity{
		owner: token, executionLineageDigest: testDigest("chain-lineage"), journalIdentityDigest: testDigest("chain-a-journal"),
		runnerProjectionDecisionDigest: testDigest("chain-a-decision"), schemaBundleDigest: testDigest("chain-a-schema"),
	}
	successorIdentity := generationIdentity{
		owner: token, executionLineageDigest: sourceIdentity.executionLineageDigest, journalIdentityDigest: testDigest("chain-b-journal"),
		runnerProjectionDecisionDigest: testDigest("chain-b-decision"), schemaBundleDigest: testDigest("chain-b-schema"),
	}
	artifactBytes := []byte("chain-a-recovery")
	artifact := VerifiedDecisionRecoveryArtifact{owner: owner, bytes: artifactBytes, digest: DigestBytes(artifactBytes), sizeBytes: uint64(len(artifactBytes)), decision: sourceIdentity.runnerProjectionDecisionDigest}
	source := GenerationDescriptor{identity: sourceIdentity, recoveryArtifactDigest: artifact.digest, recoveryArtifactSize: artifact.sizeBytes}
	successor := GenerationDescriptor{identity: successorIdentity}
	currentPolicy := testDigest("chain-current-policy")
	bindings := RunnerProjectionBindings{recoveryPolicySubjectDigest: currentPolicy}
	subject := recoveryPolicyFixtureSubject(sourceIdentity)
	subject.RecoveryPolicySubjectDigest = currentPolicy
	subject.OldDecisionRecoveryArtifactSHA256 = artifact.digest
	subject.OldDecisionRecoveryArtifactSizeBytes = artifact.sizeBytes
	subject.SuccessorRunnerProjectionDecisionDigest = successorIdentity.runnerProjectionDecisionDigest
	subject.SuccessorSchemaBundleDigest = successorIdentity.schemaBundleDigest

	policy, err := bindRecoveredHistoricalSupersessionPolicy(owner, bindings, source, artifact, successor, successorIdentity.runnerProjectionDecisionDigest, subject)
	if err != nil || policy.owner != owner || policy.subject.SuccessorRunnerProjectionDecisionDigest != successorIdentity.runnerProjectionDecisionDigest || policy.digest.Validate() != nil {
		t.Fatalf("original A -> B policy was not bound under current C: policy=%+v err=%v", policy, err)
	}
	for name, mutate := range map[string]func(*historicalRecoveryPolicySubject){
		"current policy": func(value *historicalRecoveryPolicySubject) {
			value.RecoveryPolicySubjectDigest = testDigest("other-current-policy")
		},
		"source": func(value *historicalRecoveryPolicySubject) {
			value.OldRunnerProjectionDecisionDigest = testDigest("other-source")
		},
		"artifact": func(value *historicalRecoveryPolicySubject) {
			value.OldDecisionRecoveryArtifactSHA256 = testDigest("other-artifact")
		},
		"successor": func(value *historicalRecoveryPolicySubject) {
			value.SuccessorRunnerProjectionDecisionDigest = testDigest("other-successor")
		},
		"schema": func(value *historicalRecoveryPolicySubject) {
			value.SuccessorSchemaBundleDigest = testDigest("other-successor-schema")
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := cloneProjectionValue(subject)
			mutate(&candidate)
			if _, err := bindRecoveredHistoricalSupersessionPolicy(owner, bindings, source, artifact, successor, successorIdentity.runnerProjectionDecisionDigest, candidate); !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("foreign policy field was accepted: %v", err)
			}
		})
	}
}

func TestHistoricalSupersessionExecutionBindsCurrentCToOriginalAToBPolicy(t *testing.T) {
	token := &evidenceOwnerToken{nonce: [16]byte{32}}
	owner := &recoveryVerifierOwner{verifier: &recoveryVerifierFake{}, token: token}
	a, b, c := testDigest("execution-a"), testDigest("execution-b"), testDigest("execution-c")
	identity := generationIdentity{token, testDigest("execution-lineage"), testDigest("execution-journal"), a, testDigest("execution-a-schema")}
	subject := recoveryPolicyFixtureSubject(identity)
	subject.SuccessorRunnerProjectionDecisionDigest = b
	subject.SuccessorSchemaBundleDigest = testDigest("execution-b-schema")
	policyDigest, err := subject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	policy := VerifiedHistoricalRecoveryPolicy{owner: owner, subject: subject, digest: policyDigest}
	tail := testDigest("execution-tail")
	generation := GenerationDescriptor{identity: identity, replayTailDigest: tail, recoveryArtifactDigest: subject.OldDecisionRecoveryArtifactSHA256, recoveryArtifactSize: subject.OldDecisionRecoveryArtifactSizeBytes}
	snapshot := &RecoverySnapshot{owner: token, generation: identity, tailDigest: tail, state: RecoveryTerminal}
	current := OwnedVerifiedDecision{owner: owner, digest: c}
	old := OwnedVerifiedDecision{owner: owner, digest: a}
	oldBindings := RunnerProjectionBindings{runnerProjectionDecisionDigest: a}
	execution, err := bindHistoricalSupersessionRecoveryExecution(policy, current, old, oldBindings, generation, snapshot, b)
	if err != nil || execution.subject.CurrentRunnerProjectionDecisionDigest != c || execution.policy.SuccessorRunnerProjectionDecisionDigest != b {
		t.Fatalf("intermediate recovery execution was not cross-bound: execution=%+v err=%v", execution, err)
	}
	evidence := &ownedCheckpointSupersessionEvidence{
		owner: token, generation: identity, tailDigest: tail, checkpointDigest: testDigest("execution-checkpoint"),
		outcome: "exact_committed_bundle_complete",
	}
	authority, err := bindLineageSupersession(policy, execution, evidence)
	if err != nil || authority.subject.SuccessorRunnerProjectionDecisionDigest != b || validateRecoveryAuthorityBindings(c, policy.subject, execution.subject, authority.subject) != nil {
		t.Fatalf("A -> B authority was not reconstructed under current C: authority=%+v err=%v", authority, err)
	}
	if _, err := bindHistoricalSupersessionRecoveryExecution(policy, current, old, oldBindings, generation, snapshot, c); err == nil {
		t.Fatal("A -> C successor swap reused the original A -> B policy")
	}
}

func TestRecoveryPolicyAuthorizationRequiresExactHistoricalDecision(t *testing.T) {
	a, b := testDigest("authorized-a"), testDigest("authorized-b")
	for name, authorizations := range map[string][]oldDecisionAuthorization{
		"only-a": {{OldRunnerProjectionDecisionDigest: a}},
		"only-b": {{OldRunnerProjectionDecisionDigest: b}},
		"both":   {{OldRunnerProjectionDecisionDigest: a}, {OldRunnerProjectionDecisionDigest: b}},
	} {
		t.Run(name, func(t *testing.T) {
			bindings := RunnerProjectionBindings{verifiedRecoveryPolicy: verifiedRecoveryPolicySubject{subject: recoveryPolicySignedSubject{OldDecisionAuthorizations: authorizations}}}
			wantA, wantB := name != "only-b", name != "only-a"
			if recoveryPolicyAuthorizesDecision(bindings, a) != wantA || recoveryPolicyAuthorizesDecision(bindings, b) != wantB || recoveryPolicyAuthorizesDecision(bindings, testDigest("unauthorized-c")) || recoveryPolicyAuthorizesDecision(bindings, "") {
				t.Fatal("historical decision authorization was not exact")
			}
		})
	}
}

func TestSupersessionAuthorityRejectsSessionGenerationTailSwapAndOneShot(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{3}}
	generation := generationIdentity{owner, DigestBytes([]byte("lineage")), DigestBytes([]byte("journal")), DigestBytes([]byte("old-decision")), DigestBytes([]byte("old-schema"))}
	policySubject := recoveryPolicyFixtureSubject(generation)
	policyDigest, err := policySubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	verifierOwner := &recoveryVerifierOwner{&recoveryVerifierFake{}, owner}
	policy := VerifiedHistoricalRecoveryPolicy{verifierOwner, policySubject, policyDigest}
	snapshot := &RecoverySnapshot{owner: owner, generation: generation, tailDigest: DigestBytes([]byte("tail")), state: RecoveryTerminal}
	executionSubject := recoveryExecutionBindingsSubject{HistoricalRecoveryPolicyDigest: policyDigest, ExecutionLineageDigest: generation.executionLineageDigest, CurrentRunnerProjectionDecisionDigest: policySubject.SuccessorRunnerProjectionDecisionDigest, OldRunnerProjectionDecisionDigest: generation.runnerProjectionDecisionDigest, OldJournalIdentityDigest: generation.journalIdentityDigest, OldSchemaBundleDigest: generation.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: policySubject.OldDecisionRecoveryArtifactSHA256, OldDecisionRecoveryArtifactSizeBytes: policySubject.OldDecisionRecoveryArtifactSizeBytes, OldJournalReplayTailDigest: snapshot.tailDigest, OldRecoveryState: string(snapshot.state), ActionsProfile: oldAttemptRecoveryActionsProfile}
	executionDigest, err := executionSubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	bindings := VerifiedRecoveryExecutionBindings{verifierOwner, owner, generation, snapshot.tailDigest, snapshot, policySubject, executionSubject, executionDigest}
	evidence := &ownedCheckpointSupersessionEvidence{owner: owner, generation: generation, tailDigest: snapshot.tailDigest, checkpointDigest: DigestBytes([]byte("checkpoint")), terminalDigest: digestPointer(DigestBytes([]byte("terminal"))), outcome: "exact_committed_bundle_complete"}
	authority, err := bindLineageSupersession(policy, bindings, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if authority.session != owner || !sameGenerationIdentity(authority.generation, generation) || authority.tailDigest != snapshot.tailDigest {
		t.Fatal("authority lost session/generation/tail binding")
	}
	if _, err := bindLineageSupersession(policy, bindings, evidence); err == nil {
		t.Fatal("reused supersession evidence")
	}
	wrong := *bindings.snapshot
	wrong.tailDigest = DigestBytes([]byte("other-tail"))
	bindings.snapshot = &wrong
	swapped := &ownedCheckpointSupersessionEvidence{owner: owner, generation: generation, tailDigest: wrong.tailDigest, checkpointDigest: DigestBytes([]byte("checkpoint-2")), outcome: "exact_committed_bundle_complete"}
	if _, err := bindLineageSupersession(policy, bindings, swapped); err == nil {
		t.Fatal("accepted replay-tail swap")
	}
	if _, err := authority.consume(&evidenceOwnerToken{}, generation, snapshot.tailDigest); err == nil {
		t.Fatal("accepted successor session swap")
	}
	if _, err := authority.consume(owner, generation, snapshot.tailDigest); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.consume(owner, generation, snapshot.tailDigest); err == nil {
		t.Fatal("reused successor authority")
	}
}

func TestHeaderOnlySupersessionAuthorityCarriesActivatedGenerationContinuation(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{41}}
	generation := generationIdentity{owner, testDigest("header-only-lineage"), testDigest("header-only-journal"), testDigest("header-only-old-decision"), testDigest("header-only-old-schema")}
	policySubject := recoveryPolicyFixtureSubject(generation)
	policySubject.AllowedOutcomes = []string{"activated_no_migration_progress"}
	policySubject.OutcomeConstraints = []historicalOutcomeConstraint{{Outcome: "activated_no_migration_progress", Continuation: historicalOutcomeContinuation{Kind: "exact_carry_old_generation"}}}
	policyDigest, err := policySubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	verifierOwner := &recoveryVerifierOwner{verifier: &recoveryVerifierFake{}, token: owner}
	policy := VerifiedHistoricalRecoveryPolicy{owner: verifierOwner, subject: policySubject, digest: policyDigest}
	tail := testDigest("header-only-tail")
	snapshot := &RecoverySnapshot{owner: owner, generation: generation, tailDigest: tail, state: RecoveryBrandNewInherited}
	executionSubject := recoveryExecutionBindingsSubject{
		HistoricalRecoveryPolicyDigest: policyDigest, ExecutionLineageDigest: generation.executionLineageDigest,
		CurrentRunnerProjectionDecisionDigest: policySubject.SuccessorRunnerProjectionDecisionDigest,
		OldRunnerProjectionDecisionDigest:     generation.runnerProjectionDecisionDigest, OldJournalIdentityDigest: generation.journalIdentityDigest,
		OldSchemaBundleDigest: generation.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: policySubject.OldDecisionRecoveryArtifactSHA256,
		OldDecisionRecoveryArtifactSizeBytes: policySubject.OldDecisionRecoveryArtifactSizeBytes, OldJournalReplayTailDigest: tail,
		OldRecoveryState: string(snapshot.state), ActionsProfile: oldAttemptRecoveryActionsProfile,
	}
	executionDigest, err := executionSubject.ComputeDigest()
	if err != nil {
		t.Fatal(err)
	}
	bindings := VerifiedRecoveryExecutionBindings{owner: verifierOwner, session: owner, generation: generation, tailDigest: tail, snapshot: snapshot, policy: policySubject, subject: executionSubject, digest: executionDigest}
	activation := testDigest("header-only-activation")
	terminal := testDigest("header-only-previous-terminal")
	continuation := &LineageContinuationContext{
		StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2,
		PreviousAttemptTerminalDigest: digestPointer(terminal), SourceJournalIdentityDigest: testDigest("header-only-source-journal"),
		SourceCheckpointRecordDigest: testDigest("header-only-source-checkpoint"), SourceTerminalDigest: terminal,
	}
	evidence := &ownedHeaderOnlySupersessionEvidence{owner: owner, generation: generation, tailDigest: tail, activationDigest: activation, initialTailDigest: tail, continuation: cloneProjectionValue(continuation)}
	authority, err := bindLineageSupersession(policy, bindings, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if authority.subject.ObservedOutcome != "activated_no_migration_progress" || authority.subject.OldActivationRecordDigest == nil || *authority.subject.OldActivationRecordDigest != activation || authority.subject.OldInitialJournalTailDigest == nil || *authority.subject.OldInitialJournalTailDigest != tail || authority.subject.OldCheckpointRecordDigest != nil || !canonicalEqual(authority.subject.Continuation, continuation) || validateRecoveryAuthorityBindings(policySubject.SuccessorRunnerProjectionDecisionDigest, policySubject, executionSubject, authority.subject) != nil {
		t.Fatal("header-only supersession authority lost the activated generation boundary")
	}
	for name, mutate := range map[string]func(*ownedHeaderOnlySupersessionEvidence){
		"generation": func(value *ownedHeaderOnlySupersessionEvidence) {
			value.generation.journalIdentityDigest = testDigest("header-only-other-journal")
		},
		"tail": func(value *ownedHeaderOnlySupersessionEvidence) {
			value.tailDigest = testDigest("header-only-other-tail")
		},
		"initial tail": func(value *ownedHeaderOnlySupersessionEvidence) {
			value.initialTailDigest = testDigest("header-only-other-initial-tail")
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := &ownedHeaderOnlySupersessionEvidence{owner: owner, generation: generation, tailDigest: tail, activationDigest: activation, initialTailDigest: tail, continuation: cloneProjectionValue(continuation)}
			mutate(candidate)
			if _, err := bindLineageSupersession(policy, bindings, candidate); err == nil {
				t.Fatal("mutated header-only supersession evidence was accepted")
			}
		})
	}
	changedActivation := &ownedHeaderOnlySupersessionEvidence{owner: owner, generation: generation, tailDigest: tail, activationDigest: testDigest("header-only-other-activation"), initialTailDigest: tail, continuation: cloneProjectionValue(continuation)}
	changedAuthority, err := bindLineageSupersession(policy, bindings, changedActivation)
	if err != nil || changedAuthority.digest == authority.digest {
		t.Fatalf("activation boundary did not change header-only authority identity: authority=%+v err=%v", changedAuthority, err)
	}
}

func TestFirstVerificationConstructorRequiresCanonicalRecoveryABIAndExactDecisionIdentity(t *testing.T) {
	raw, err := os.ReadFile(migrationFixturePath(t, "golden/decision-recovery-inputs-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Same json.RawMessage `json:"same_bits_input"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	var inputs decisionRecoveryVerificationInputs
	if _, err := DecodeStrict(fixture.Same, &inputs); err != nil {
		t.Fatal(err)
	}
	fake := &recoveryVerifierFake{}
	decision := VerifiedTrustDecision{verified: true, repositoryIdentity: inputs.RepositoryIdentity, releaseIdentity: inputs.ReleaseIdentity, projectionBindings: &RunnerProjectionBindings{verified: true, runnerProjectionDecisionDigest: inputs.OldRunnerProjectionDecisionDigest, decisionRecoveryArtifactProfileDigest: inputs.ProfileDigest}}
	// The intentionally incomplete decision cannot bypass ordinary verified
	// binding validation merely because the recovery bytes themselves are valid.
	if _, _, err := bindVerifierOwnedDecision(fake, decision, inputs.OldRunnerProjectionDecisionDigest, fixture.Same); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("incomplete decision was accepted: %v", err)
	}
	mutated := append([]byte(" "), fixture.Same...)
	if _, _, err := bindVerifierOwnedDecision(fake, decision, inputs.OldRunnerProjectionDecisionDigest, mutated); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("noncanonical recovery bytes were accepted: %v", err)
	}
}

func TestTypedReceiptBindersRejectKindDigestSizeAndStoreIdentitySwaps(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{7}}
	runtimeBytes := []byte("runtime")
	runtime := VerifiedRuntimeArtifact{owner: owner, bytes: runtimeBytes, digest: DigestBytes(runtimeBytes), sizeBytes: uint64(len(runtimeBytes))}
	if _, err := bindRuntimeContentReceipt(owner, runtime, verifiedDurableContentObject{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime receipt escaped unimplemented durable publication authority: %v", err)
	}
	wrong := runtime
	wrong.digest = DigestBytes([]byte("wrong"))
	if _, err := bindRuntimeContentReceipt(owner, wrong, verifiedDurableContentObject{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("runtime digest swap escaped unavailable publication authority: %v", err)
	}

	verifierOwner := &recoveryVerifierOwner{&recoveryVerifierFake{}, owner}
	recoveryBytes := []byte("recovery")
	recovery := VerifiedDecisionRecoveryArtifact{verifierOwner, recoveryBytes, DigestBytes(recoveryBytes), uint64(len(recoveryBytes)), DigestBytes([]byte("decision"))}
	if _, err := bindDecisionRecoveryReceipt(owner, recovery, verifiedDurableContentObject{}); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("recovery receipt escaped unimplemented durable publication authority: %v", err)
	}
	if validRuntimeReceipt(VerifiedContentReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: runtime.digest, sizeBytes: runtime.sizeBytes}, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("typed receipt kind swap accepted")
	}
	recoveryReceipt := VerifiedDecisionRecoveryReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: recovery.digest, sizeBytes: recovery.sizeBytes}
	if validDecisionRecoveryReceipt(recoveryReceipt, owner, recovery.digest, recovery.sizeBytes) {
		t.Fatal("missing store identity accepted")
	}
}

func TestHistoricalSupersessionRejectsLiteralReceiptsBeforeVerifierCall(t *testing.T) {
	t.Parallel()
	token := &evidenceOwnerToken{nonce: [16]byte{17}}
	verifier := &recoveryVerifierFake{}
	owner := &recoveryVerifierOwner{verifier: verifier, token: token}
	decision := OwnedVerifiedDecision{owner: owner, capability: sameVerifierRecoveryCapability{owner: owner}}
	authority := &VerifiedLineageSupersessionAuthority{owner: owner}

	oldBytes := []byte("old-recovery")
	oldArtifact := VerifiedDecisionRecoveryArtifact{owner: owner, bytes: oldBytes, digest: DigestBytes(oldBytes), sizeBytes: uint64(len(oldBytes))}
	runtimeBytes := []byte("planned-runtime")
	plannedRuntime := VerifiedRuntimeArtifact{owner: token, bytes: runtimeBytes, digest: DigestBytes(runtimeBytes), sizeBytes: uint64(len(runtimeBytes))}
	recoveryBytes := []byte("planned-recovery")
	plannedRecovery := VerifiedDecisionRecoveryArtifact{owner: owner, bytes: recoveryBytes, digest: DigestBytes(recoveryBytes), sizeBytes: uint64(len(recoveryBytes))}
	runtimeReceipt := VerifiedContentReceipt{owner: token, kind: durableRuntimeContentObject, digest: plannedRuntime.digest, sizeBytes: plannedRuntime.sizeBytes}
	recoveryReceipt := VerifiedDecisionRecoveryReceipt{owner: token, kind: durableDecisionRecoveryContentObject, digest: plannedRecovery.digest, sizeBytes: plannedRecovery.sizeBytes}

	if _, err := decision.recoverHistoricalSupersession(context.Background(), authority, GenerationSuperseded{}, oldArtifact, plannedRuntime, runtimeReceipt, plannedRecovery, recoveryReceipt); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal receipts did not stop at publication boundary: %v", err)
	}
	if verifier.supersessionCalls != 0 {
		t.Fatalf("literal receipts reached historical verifier: calls=%d", verifier.supersessionCalls)
	}
}

func TestHistoricalSupersessionReceiptModesRejectLiteralAndCrossMode(t *testing.T) {
	owner := &evidenceOwnerToken{nonce: [16]byte{18}}
	digest := testDigest("historical-receipt-mode")
	if validHistoricalSupersessionRuntimeReceipt(VerifiedContentReceipt{}, owner, digest, 1) || validHistoricalSupersessionRecoveryReceipt(VerifiedDecisionRecoveryReceipt{}, owner, digest, 1) {
		t.Fatal("literal receipt entered historical supersession recovery")
	}
	transientRuntime := VerifiedContentReceipt{publication: &evidencefs.Publication{}}
	registeredRuntime := VerifiedContentReceipt{registeredPublication: &evidencefs.RegisteredPublication{}}
	if sameHistoricalSupersessionRuntimeObject(transientRuntime, registeredRuntime) || sameHistoricalSupersessionRuntimeObject(registeredRuntime, transientRuntime) {
		t.Fatal("runtime receipt mode swap retained object identity")
	}
	transientRecovery := VerifiedDecisionRecoveryReceipt{publication: &evidencefs.Publication{}}
	registeredRecovery := VerifiedDecisionRecoveryReceipt{registeredPublication: &evidencefs.RegisteredPublication{}}
	if sameHistoricalSupersessionRecoveryObject(transientRecovery, registeredRecovery) || sameHistoricalSupersessionRecoveryObject(registeredRecovery, transientRecovery) {
		t.Fatal("decision-recovery receipt mode swap retained object identity")
	}
	mixedRuntime := VerifiedContentReceipt{publication: &evidencefs.Publication{}, registeredPublication: &evidencefs.RegisteredPublication{}}
	mixedRecovery := VerifiedDecisionRecoveryReceipt{publication: &evidencefs.Publication{}, registeredPublication: &evidencefs.RegisteredPublication{}}
	if sameHistoricalSupersessionRuntimeObject(mixedRuntime, mixedRuntime) || sameHistoricalSupersessionRecoveryObject(mixedRecovery, mixedRecovery) {
		t.Fatal("mixed fresh and registered receipt shape was accepted")
	}
}

func recoveryPolicyFixtureSubject(g generationIdentity) historicalRecoveryPolicySubject {
	return historicalRecoveryPolicySubject{
		RecoveryPolicySubjectDigest: DigestBytes([]byte("policy")), ExecutionLineageDigest: g.executionLineageDigest,
		OldJournalIdentityDigest: g.journalIdentityDigest, OldRunnerProjectionDecisionDigest: g.runnerProjectionDecisionDigest,
		OldSchemaBundleDigest: g.schemaBundleDigest, OldDecisionRecoveryArtifactSHA256: DigestBytes([]byte("artifact")), OldDecisionRecoveryArtifactSizeBytes: 8,
		SuccessorRunnerProjectionDecisionDigest: DigestBytes([]byte("successor")), SuccessorSchemaBundleDigest: DigestBytes([]byte("successor-schema")),
		AllowedOutcomes: []string{"exact_committed_bundle_complete"}, OutcomeConstraints: []historicalOutcomeConstraint{{Outcome: "exact_committed_bundle_complete", Continuation: historicalOutcomeContinuation{Kind: "must_be_null"}}},
	}
}
