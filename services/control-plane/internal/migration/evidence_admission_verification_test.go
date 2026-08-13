package migration

import (
	"reflect"
	"testing"
)

func TestHistoricalVerificationFactsRebuildExactOrdinarySubjects(t *testing.T) {
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bundle, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	bundle.Manifest.ExecutionPolicy.MaxAttempts = 3
	bundle.Manifest.SchemaBundle.Migrations[0].Name = "test migration"
	var err error
	bundle.ownedInputs, err = bindVerifiedRuntimeBundleInputs(bundle.Manifest, bundle.Files, bundle.ownedInputs.outerArtifactDigest, bundle.ownedInputs.outerArtifactSize)
	if err != nil {
		t.Fatal(err)
	}
	catalogRaw := mustJSON(t, catalog)
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, DigestBytes(catalogRaw), fixture.expiresAt, 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, []verifiedExecutableCatalogSubject{catalogSubject}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	facts, err := buildHistoricalVerificationFacts(bundle, bindings)
	if err != nil {
		t.Fatal(err)
	}
	if facts.maxAttempts != uint32(bundle.ownedInputs.manifest.ExecutionPolicy.MaxAttempts) || !reflect.DeepEqual(facts.orderedMigrations, []string{"000001"}) || len(facts.statementSubjects["000001"]) != 1 || len(facts.ledgerRows) != 1 || facts.finalCatalogDigest["000001"] == ([32]byte{}) {
		t.Fatalf("historical facts are incomplete: %+v", facts)
	}
	plans, err := buildExactStatementPlans(bundle, bindings, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	want, err := admissionStatementPlanSubjectFromPlan(plans[0])
	if err != nil || facts.statementSubjects["000001"][0] != want {
		t.Fatalf("statement subject differs: got=%x want=%x err=%v", facts.statementSubjects["000001"][0], want, err)
	}
	entry := bundle.ownedInputs.manifest.SchemaBundle.Migrations[0]
	if !canonicalEqual(facts.ledgerRows[0], commitIntentLedgerRow(entry, bundle.ownedInputs.manifest.SchemaBundleDigest)) {
		t.Fatal("historical ledger row differs from immutable manifest")
	}

	// The historical path validates at the shared pre-expiry point, while the
	// live execution path rejects the same binding after expiry.
	if _, err := buildExactStatementPlans(bundle, bindings, fixture.expiresAt); !IsCode(err, CodeUntrusted) {
		t.Fatalf("expired live plan was accepted: %v", err)
	}
	if _, err := buildHistoricalVerificationFacts(bundle, bindings); err != nil {
		t.Fatalf("immutable historical facts were rejected after elapsed clock: %v", err)
	}

	mutated := bindings.ownedCopy()
	mutated.schemaBundleDigest = testDigest("historical-facts-schema-swap")
	if _, err := buildHistoricalVerificationFacts(bundle, mutated); !IsCode(err, CodeUntrusted) {
		t.Fatalf("mutated historical binding was accepted: %v", err)
	}
}

func TestHistoricalVerificationFactsContainNoExecutablePlan(t *testing.T) {
	typeOf := reflect.TypeOf(admissionHistoricalVerificationFacts{})
	for index := 0; index < typeOf.NumField(); index++ {
		field := typeOf.Field(index)
		if field.Type == reflect.TypeOf(StatementPlan{}) || field.Type == reflect.TypeOf([]StatementPlan{}) || field.Name == "sqlBytes" {
			t.Fatalf("ordinary historical facts expose executable plan field %s", field.Name)
		}
	}
}

func TestAdmissionGenerationVerificationClosesHistoricalFacts(t *testing.T) {
	facts := admissionHistoricalFactsFixture(t)
	generation := admissionVerifiedGenerationFixture(t, facts)
	if err := verifyAdmissionGeneration(&generation, facts); err != nil {
		t.Fatalf("exact historical generation rejected: %v", err)
	}

	mutations := map[string]func(*admissionReplayGeneration){
		"manifest":        func(v *admissionReplayGeneration) { v.header.manifestDigest = testDigest("pass2-manifest") },
		"statement chain": func(v *admissionReplayGeneration) { v.verificationTerminals[0].statementChain[0] ^= 1 },
		"retry bound":     func(v *admissionReplayGeneration) { v.verificationTerminals[0].attemptIndex = facts.maxAttempts + 1 },
		"catalog":         func(v *admissionReplayGeneration) { v.verificationCatalogContract[0] ^= 1 },
		"final catalog":   func(v *admissionReplayGeneration) { v.verificationFinals[0].preledgerCatalog[0] ^= 1 },
		"commit":          func(v *admissionReplayGeneration) { v.verificationCommits[0].commitBody[0] ^= 1 },
		"statement count": func(v *admissionReplayGeneration) { v.runtimeInspection.statementCounts[0]++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneAdmissionGenerationForTest(generation)
			mutate(&value)
			if err := verifyAdmissionGeneration(&value, facts); !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("stored mismatch accepted: %v", err)
			}
		})
	}

	retry := cloneAdmissionGenerationForTest(generation)
	retry.verificationTerminals[0].outcome = 2
	retry.verificationTerminals[0].flags = admissionTerminalHasStatements | admissionTerminalHasRetry
	retry.verificationFinals, retry.verificationCommits = nil, nil
	retry.verificationRetries = []admissionReplayTerminalRetry{{ordinal: 0, proofKind: 1, attemptPredecessorCatalog: digestRaw(testDigest("retry-predecessor")), observedCatalog: digestRaw(testDigest("retry-predecessor")), ledgerPrefix: digestRaw(testDigest("retry-ledger")), authorityResult: digestRaw(testDigest("retry-authority"))}}
	if err := verifyAdmissionGeneration(&retry, facts); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("disk retry proof was treated as lifecycle authority: %v", err)
	}

	ambiguous := cloneAdmissionGenerationForTest(generation)
	ambiguous.verificationTerminals[0].outcome = 4
	if err := verifyAdmissionGeneration(&ambiguous, facts); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("disk ambiguous boundary was treated as lifecycle authority: %v", err)
	}
}

func admissionHistoricalFactsFixture(t *testing.T) *admissionHistoricalVerificationFacts {
	t.Helper()
	fixture := newRunnerBindingFixture(t, []string{"000001"})
	bundle, catalog := exactPlanBundle(t, fixture.decision.expectedSchemaBundleDigest, fixture.initialScope.BoundPrecondition(), fixture.authorityProfile)
	bundle.Manifest.ExecutionPolicy.MaxAttempts = 3
	bundle.Manifest.SchemaBundle.Migrations[0].Name = "test migration"
	var err error
	bundle.ownedInputs, err = bindVerifiedRuntimeBundleInputs(bundle.Manifest, bundle.Files, bundle.ownedInputs.outerArtifactDigest, bundle.ownedInputs.outerArtifactSize)
	if err != nil {
		t.Fatal(err)
	}
	catalogRaw := mustJSON(t, catalog)
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(catalogRaw, DigestBytes(catalogRaw), fixture.expiresAt, 1, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindVerifiedRunnerProjectionDecision(fixture.decision, fixture.authorityProfile, fixture.authorityBinding, fixture.authority, fixture.recoveryPolicy, fixture.initialScope, []verifiedExecutableCatalogSubject{catalogSubject}, fixture.now)
	if err != nil {
		t.Fatal(err)
	}
	bindings, err := bound.runnerProjectionBindings()
	if err != nil {
		t.Fatal(err)
	}
	facts, err := buildHistoricalVerificationFacts(bundle, bindings)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func admissionVerifiedGenerationFixture(t *testing.T, facts *admissionHistoricalVerificationFacts) admissionReplayGeneration {
	t.Helper()
	migration := facts.orderedMigrations[0]
	subjects := facts.statementSubjects[migration]
	var chain [32]byte
	for index, subject := range subjects {
		chain = admissionStatementChainStep(chain, migration, 1, uint32(index), subject)
	}
	predecessor, state := facts.attemptPredecessorCatalog[migration], digestRaw(testDigest("pass2-state"))
	commit := CommitIntent{
		SchemaBundleDigest: facts.schemaBundleDigest, CatalogContractDigest: digestString(facts.catalogContractDigest[migration]),
		AuthorityProfileDigest: facts.authorityProfileDigest, AuthorityBindingDigest: facts.authorityBindingDigest,
		MigrationID: migration, AttemptIndex: 1, AttemptPredecessorCatalogDigest: digestString(predecessor), LastIntermediateStateDigest: digestString(state),
		ExpectedLedgerLength: 1, ExpectedLedgerHead: migration, LedgerRow: cloneProjectionValue(facts.ledgerRows[0]),
	}
	commitBody, err := admissionCommitSubject(commit)
	if err != nil {
		t.Fatal(err)
	}
	header := &admissionReplayHeaderFacts{manifestDigest: facts.manifestDigest, runnerProjectionDecisionDigest: facts.runnerProjectionDecisionDigest, schemaBundleDigest: facts.schemaBundleDigest, authorityProfileDigest: facts.authorityProfileDigest, authorityBindingDigest: facts.authorityBindingDigest}
	return admissionReplayGeneration{
		runnerProjectionDecisionDigest: facts.runnerProjectionDecisionDigest, schemaBundleDigest: facts.schemaBundleDigest, header: header,
		runtimeInspection:           &admissionReplayRuntimeInspection{manifestDigest: facts.manifestDigest, schemaBundleDigest: facts.schemaBundleDigest, maxAttempts: uint64(facts.maxAttempts), statementCounts: []uint64{uint64(len(subjects))}},
		verificationCatalogContract: facts.catalogContractDigest[migration],
		verificationTerminals:       []admissionReplayTerminalEvent{{migrationID: 1, attemptIndex: 1, statementCount: uint32(len(subjects)), lastStatementIndex: uint32(len(subjects) - 1), outcome: 1, flags: admissionTerminalHasFinal | admissionTerminalHasCommit | admissionTerminalHasStatements, terminalDigest: digestRaw(testDigest("pass2-terminal")), statementChain: chain}},
		verificationFinals:          []admissionReplayTerminalFinal{{ordinal: 0, lastIntermediateRecord: digestRaw(testDigest("pass2-intermediate")), preledgerCatalog: facts.finalCatalogDigest[migration]}},
		verificationCommits:         []admissionReplayTerminalCommit{{ordinal: 0, expectedLedgerLength: 1, commitRecord: digestRaw(testDigest("pass2-commit")), commitBody: commitBody, attemptPredecessorCatalog: predecessor, lastIntermediateState: state}},
	}
}

func cloneAdmissionGenerationForTest(value admissionReplayGeneration) admissionReplayGeneration {
	owned := value
	header := *value.header
	owned.header = &header
	inspection := *value.runtimeInspection
	inspection.statementCounts = append([]uint64(nil), value.runtimeInspection.statementCounts...)
	owned.runtimeInspection = &inspection
	owned.verificationTerminals = append([]admissionReplayTerminalEvent(nil), value.verificationTerminals...)
	owned.verificationFinals = append([]admissionReplayTerminalFinal(nil), value.verificationFinals...)
	owned.verificationCommits = append([]admissionReplayTerminalCommit(nil), value.verificationCommits...)
	owned.verificationRetries = append([]admissionReplayTerminalRetry(nil), value.verificationRetries...)
	return owned
}
