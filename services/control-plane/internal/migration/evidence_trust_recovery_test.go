package migration

import (
	"context"
	"encoding/json"
	"os"
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
	runtime := VerifiedRuntimeArtifact{owner, runtimeBytes, DigestBytes(runtimeBytes), uint64(len(runtimeBytes))}
	receipt, err := bindRuntimeContentReceipt(owner, runtime, DigestBytes([]byte("runtime-store")))
	if err != nil {
		t.Fatal(err)
	}
	if !validRuntimeReceipt(receipt, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("valid runtime receipt rejected")
	}
	wrong := runtime
	wrong.digest = DigestBytes([]byte("wrong"))
	if _, err := bindRuntimeContentReceipt(owner, wrong, DigestBytes([]byte("runtime-store"))); err == nil {
		t.Fatal("runtime digest swap accepted")
	}

	verifierOwner := &recoveryVerifierOwner{&recoveryVerifierFake{}, owner}
	recoveryBytes := []byte("recovery")
	recovery := VerifiedDecisionRecoveryArtifact{verifierOwner, recoveryBytes, DigestBytes(recoveryBytes), uint64(len(recoveryBytes)), DigestBytes([]byte("decision"))}
	recoveryReceipt, err := bindDecisionRecoveryReceipt(owner, recovery, DigestBytes([]byte("recovery-store")))
	if err != nil {
		t.Fatal(err)
	}
	if !validDecisionRecoveryReceipt(recoveryReceipt, owner, recovery.digest, recovery.sizeBytes) {
		t.Fatal("valid recovery receipt rejected")
	}
	if validRuntimeReceipt(VerifiedContentReceipt{owner: owner, kind: "decision_recovery", digest: runtime.digest, sizeBytes: runtime.sizeBytes, identity: receipt.identity}, owner, runtime.digest, runtime.sizeBytes) {
		t.Fatal("typed receipt kind swap accepted")
	}
	recoveryReceipt.identity = ""
	if validDecisionRecoveryReceipt(recoveryReceipt, owner, recovery.digest, recovery.sizeBytes) {
		t.Fatal("missing store identity accepted")
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
