package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type recoveryVerifierFake struct{ historicalCalls, supersessionCalls int }

func (*recoveryVerifierFake) Verify(context.Context, CandidateEnvelope) (VerifiedTrustDecision, error) {
	return VerifiedTrustDecision{}, fail(CodeUntrusted, "fake", "ordinary verification is outside recovery ABI tests", nil)
}
func (f *recoveryVerifierFake) recoverHistoricalDecision(context.Context, VerifiedTrustDecision, GenerationDescriptor, VerifiedDecisionRecoveryArtifact) (VerifiedTrustDecision, RunnerProjectionBindings, historicalRecoveryPolicySubject, error) {
	f.historicalCalls++
	return VerifiedTrustDecision{}, RunnerProjectionBindings{}, historicalRecoveryPolicySubject{}, fail(CodeEvidenceRecoveryRequired, "fake-recovery", "test stop", nil)
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
	if _, _, _, err := current.recoverHistoricalDecision(context.Background(), generation, ownedOld); !IsCode(err, CodeEvidenceRecoveryRequired) || fakeA.historicalCalls != 1 || fakeB.historicalCalls != 0 {
		t.Fatalf("same-verifier capability did not route exactly once: err=%v calls=%d/%d", err, fakeA.historicalCalls, fakeB.historicalCalls)
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
	if _, _, _, err := current.recoverHistoricalDecision(t.Context(), generation, artifact); !IsCode(err, CodeEvidenceRecoveryRequired) || verifier.historicalCalls != 1 {
		t.Fatalf("bound artifact did not route exactly once through the current verifier: err=%v calls=%d", err, verifier.historicalCalls)
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
		if bytes.Contains(raw, []byte("bindHistoricalRecoveryVerifierInput(")) {
			t.Fatalf("historical recovery input binder has premature production caller in %s", name)
		}
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
	if _, err := bindRuntimeContentReceipt(owner, runtime, verifiedDurableContentObject{}); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("runtime receipt escaped unimplemented durable publication authority: %v", err)
	}
	wrong := runtime
	wrong.digest = DigestBytes([]byte("wrong"))
	if _, err := bindRuntimeContentReceipt(owner, wrong, verifiedDurableContentObject{}); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("runtime digest swap escaped unavailable publication authority: %v", err)
	}

	verifierOwner := &recoveryVerifierOwner{&recoveryVerifierFake{}, owner}
	recoveryBytes := []byte("recovery")
	recovery := VerifiedDecisionRecoveryArtifact{verifierOwner, recoveryBytes, DigestBytes(recoveryBytes), uint64(len(recoveryBytes)), DigestBytes([]byte("decision"))}
	if _, err := bindDecisionRecoveryReceipt(owner, recovery, verifiedDurableContentObject{}); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("recovery receipt escaped unimplemented durable publication authority: %v", err)
	}
	identity := DigestBytes([]byte("runtime-store"))
	if validRuntimeReceipt(VerifiedContentReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: runtime.digest, sizeBytes: runtime.sizeBytes, identity: identity}, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("typed receipt kind swap accepted")
	}
	recoveryReceipt := VerifiedDecisionRecoveryReceipt{owner: owner, kind: durableDecisionRecoveryContentObject, digest: recovery.digest, sizeBytes: recovery.sizeBytes, identity: identity}
	recoveryReceipt.identity = ""
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
	storeIdentity := DigestBytes([]byte("self-consistent-store"))
	runtimeReceipt := VerifiedContentReceipt{owner: token, kind: durableRuntimeContentObject, digest: plannedRuntime.digest, sizeBytes: plannedRuntime.sizeBytes, identity: storeIdentity}
	recoveryReceipt := VerifiedDecisionRecoveryReceipt{owner: token, kind: durableDecisionRecoveryContentObject, digest: plannedRecovery.digest, sizeBytes: plannedRecovery.sizeBytes, identity: storeIdentity}

	if _, err := decision.recoverHistoricalSupersession(context.Background(), authority, GenerationSuperseded{}, oldArtifact, plannedRuntime, runtimeReceipt, plannedRecovery, recoveryReceipt); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("literal receipts did not stop at publication boundary: %v", err)
	}
	if verifier.supersessionCalls != 0 {
		t.Fatalf("literal receipts reached historical verifier: calls=%d", verifier.supersessionCalls)
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
