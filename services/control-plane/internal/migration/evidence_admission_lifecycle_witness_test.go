package migration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestVerifiedAdmissionLifecycleWitnessIsOwnedOneShotEvidence(t *testing.T) {
	facts := admissionHistoricalFactsFixture(t)
	generation := admissionVerifiedGenerationFixture(t, facts)
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("lifecycle-witness"))
	defer revokeOwnedCurrentCandidate(candidate)
	identity := admissionLifecycleGenerationIdentityForTest(candidate, &generation)
	chain := admissionLifecycleChainForTest(facts)

	witness, err := bindVerifiedAdmissionLifecycleWitness(candidate.binding, identity, [32]byte{1}, chain)
	if err != nil {
		t.Fatal(err)
	}
	chain.maxAttempts["000001"]++
	evidence, err := consumeVerifiedAdmissionLifecycleWitness(witness, candidate)
	if err != nil {
		t.Fatal(err)
	}
	defer revokeAdmissionLifecycleEvidence(evidence)
	if !validAdmissionLifecycleEvidence(evidence, candidate.binding) || evidence.chain.maxAttempts["000001"] != facts.maxAttempts {
		t.Fatal("consumed lifecycle evidence was not an owned immutable clone")
	}
	lineage := admissionReplayLineage{id: digestRaw(identity.executionLineageDigest)}
	if !evidence.matches(lineage, &generation) {
		t.Fatal("exact generation did not match consumed lifecycle evidence")
	}
	if again, err := consumeVerifiedAdmissionLifecycleWitness(witness, candidate); again != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("lifecycle witness was reusable: evidence=%v err=%v", again, err)
	}
	copyEvidence := *evidence
	if validAdmissionLifecycleEvidence(&copyEvidence, candidate.binding) || (&admissionLifecycleEvidence{}).matches(lineage, &generation) {
		t.Fatal("ordinary lifecycle evidence copy or literal was accepted")
	}
	revokeAdmissionLifecycleEvidence(evidence)
	if validAdmissionLifecycleEvidence(evidence, candidate.binding) {
		t.Fatal("revoked lifecycle evidence remained valid")
	}
}

func TestAdmissionGenerationVerificationRequiresExactLiveRetryReceipt(t *testing.T) {
	facts := admissionHistoricalFactsFixture(t)
	generation := admissionVerifiedGenerationFixture(t, facts)
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("retry-lifecycle"))
	defer revokeOwnedCurrentCandidate(candidate)
	identity := admissionLifecycleGenerationIdentityForTest(candidate, &generation)

	retry := cloneAdmissionGenerationForTest(generation)
	retry.verificationTerminals[0].outcome = 2
	retry.verificationTerminals[0].flags = admissionTerminalHasStatements | admissionTerminalHasRetry
	retry.verificationFinals, retry.verificationCommits = nil, nil
	ledgerPrefix, err := LedgerPrefixDigest(facts.ledgerRows)
	if err != nil {
		t.Fatal(err)
	}
	predecessorCatalog := facts.attemptPredecessorCatalog["000001"]
	authorityResult := testDigest("admission-live-retry-authority")
	retry.verificationRetries = []admissionReplayTerminalRetry{{
		ordinal: 0, proofKind: 1,
		attemptPredecessorCatalog: predecessorCatalog, observedCatalog: predecessorCatalog,
		ledgerPrefix: digestRaw(ledgerPrefix), authorityResult: digestRaw(authorityResult),
	}}
	retryIdentity := ownedRetryIdentity{"000001", 1, identity.executionLineageDigest, identity.journalIdentityDigest}
	token := &retryLifecycleOrderToken{verifierNonce: [16]byte{1}}
	predecessor := ownedRecoveryPredecessorReceipt{
		identity: retryIdentity, newLifecycleID: "new", order: ownedLifecycleOrderAuthority{token, 2},
		ledgerRows: cloneProjectionValue(facts.ledgerRows), ledgerPrefixDigest: ledgerPrefix,
		attemptPredecessorCatalog: digestString(predecessorCatalog), observedCatalogDigest: digestString(predecessorCatalog),
		authorityResultDigest: authorityResult,
	}
	receipt, err := bindRollbackRetryReceipt("projection_transient_exact_predecessor", ownedRollbackReceipt{
		identity: retryIdentity, oldLifecycleID: "old", order: ownedLifecycleOrderAuthority{token, 1},
		rollbackSucceeded: true, oldHandleClosed: true,
	}, predecessor)
	if err != nil {
		t.Fatal(err)
	}
	chain := admissionLifecycleChainForTest(facts)
	chain.retryReceipts[digestString(retry.verificationTerminals[0].terminalDigest)] = receipt
	evidence := consumeAdmissionLifecycleForTest(t, candidate, identity, chain)
	defer revokeAdmissionLifecycleEvidence(evidence)

	if err := verifyAdmissionGenerationWithLifecycle(&retry, facts, evidence); err != nil {
		t.Fatalf("exact live retry receipt was rejected: %v", err)
	}
	mutated := cloneAdmissionGenerationForTest(retry)
	mutated.verificationRetries[0].authorityResult[0] ^= 1
	if err := verifyAdmissionGenerationWithLifecycle(&mutated, facts, evidence); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("mismatched live retry receipt was accepted: %v", err)
	}
	wrongGeneration := cloneAdmissionGenerationForTest(retry)
	wrongGeneration.journalID = testDigest("different-lifecycle-journal")
	if err := verifyAdmissionGenerationWithLifecycle(&wrongGeneration, facts, evidence); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("foreign generation consumed lifecycle evidence: %v", err)
	}
}

func TestAdmissionGenerationVerificationRequiresExactLiveAmbiguousBoundary(t *testing.T) {
	facts := admissionHistoricalFactsFixture(t)
	generation := admissionVerifiedGenerationFixture(t, facts)
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("ambiguous-lifecycle"))
	defer revokeOwnedCurrentCandidate(candidate)
	identity := admissionLifecycleGenerationIdentityForTest(candidate, &generation)

	ambiguous := cloneAdmissionGenerationForTest(generation)
	ambiguous.verificationTerminals[0].outcome = 4
	event := ambiguous.verificationTerminals[0]
	chain := admissionLifecycleChainForTest(facts)
	chain.ambiguousBoundaries[digestString(event.terminalDigest)] = ownedAmbiguousBoundaryWitness{
		migrationID: "000001", attemptIndex: 1, commitCalled: true,
		finalIntermediateRecordDigest: digestString(ambiguous.verificationFinals[0].lastIntermediateRecord),
		commitIntentRecordDigest:      digestString(ambiguous.verificationCommits[0].commitRecord),
	}
	evidence := consumeAdmissionLifecycleForTest(t, candidate, identity, chain)
	defer revokeAdmissionLifecycleEvidence(evidence)

	if err := verifyAdmissionGenerationWithLifecycle(&ambiguous, facts, evidence); err != nil {
		t.Fatalf("exact live ambiguous boundary was rejected: %v", err)
	}
	mutated := cloneAdmissionGenerationForTest(ambiguous)
	mutated.verificationFinals[0].lastIntermediateRecord[0] ^= 1
	if err := verifyAdmissionGenerationWithLifecycle(&mutated, facts, evidence); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("mismatched live ambiguous boundary was accepted: %v", err)
	}
}

func TestAdmissionLifecycleAuthorityHasOnlyReviewedProductionConsumers(t *testing.T) {
	allowed := map[string]map[string]bool{
		"admissionLifecycleEvidence": {
			"evidence_admission_lifecycle_witness.go": true,
			"evidence_admission_history.go":           true,
			"evidence_admission_verification.go":      true,
		},
		"verifiedAdmissionLifecycleWitness": {
			"evidence_admission_lifecycle_witness.go": true,
			"evidence_admission_history.go":           true,
			"evidence_session.go":                     true,
		},
		"bindVerifiedAdmissionLifecycleWitness": {
			"evidence_admission_lifecycle_witness.go": true,
			"evidence_session.go":                     true,
		},
		"consumeVerifiedAdmissionLifecycleWitness": {
			"evidence_admission_lifecycle_witness.go": true,
			"evidence_admission_history.go":           true,
		},
		"verifyAdmissionGenerationWithLifecycle": {
			"evidence_admission_verification.go": true,
			"evidence_admission_history.go":      true,
		},
		"bindVerifiedAdmissionHistoryForRunnerRecovery": {
			"evidence_admission_history.go": true,
			"evidence_session.go":           true,
		},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if !ok {
				return true
			}
			files, tracked := allowed[identifier.Name]
			if tracked && !files[name] {
				t.Errorf("%s uses lifecycle authority symbol %s", name, identifier.Name)
			}
			return true
		})
	}
}

func admissionLifecycleGenerationIdentityForTest(candidate OwnedCurrentCandidate, generation *admissionReplayGeneration) generationIdentity {
	generation.journalID = testDigest("admission-live-journal")
	return generationIdentity{
		owner: candidate.owner, executionLineageDigest: candidate.verifiedRun.executionLineageDigest,
		journalIdentityDigest: generation.journalID, runnerProjectionDecisionDigest: generation.runnerProjectionDecisionDigest,
		schemaBundleDigest: generation.schemaBundleDigest,
	}
}

func admissionLifecycleChainForTest(facts *admissionHistoricalVerificationFacts) verifiedEvidenceChainWitness {
	migration := facts.orderedMigrations[0]
	return verifiedEvidenceChainWitness{
		maxAttempts:         map[string]uint32{migration: facts.maxAttempts},
		finalStatementIndex: map[string]uint32{migration: uint32(len(facts.statementSubjects[migration]) - 1)},
		finalCatalogDigest:  map[string]Digest{migration: digestString(facts.finalCatalogDigest[migration])},
		plans:               map[string]exactStatementEvidenceWitness{},
		runtimeReceipt:      ownedContentReceiptWitness{"runtime", testDigest("admission-live-runtime"), 1},
		recoveryReceipt:     ownedContentReceiptWitness{"decision-recovery", testDigest("admission-live-recovery"), 1},
		retryReceipts:       map[Digest]verifiedRetryReceipt{},
		ambiguousBoundaries: map[Digest]ownedAmbiguousBoundaryWitness{},
	}
}

func consumeAdmissionLifecycleForTest(t *testing.T, candidate OwnedCurrentCandidate, identity generationIdentity, chain verifiedEvidenceChainWitness) *admissionLifecycleEvidence {
	t.Helper()
	witness, err := bindVerifiedAdmissionLifecycleWitness(candidate.binding, identity, [32]byte{1}, chain)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := consumeVerifiedAdmissionLifecycleWitness(witness, candidate)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
