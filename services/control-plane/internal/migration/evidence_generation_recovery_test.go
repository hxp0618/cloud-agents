package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sync/atomic"
	"testing"
)

func TestBrandNewRecoveryWitnessIsSameVerifierBound(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	generation := generationIdentity{candidate.owner, candidate.verifiedRun.executionLineageDigest, testDigest("journal"), candidate.verifiedRun.runnerProjectionDecisionDigest, candidate.verifiedRun.schemaBundleDigest}
	chain, schema, err := buildBrandNewRecoveryWitness(candidate, generation, facts)
	if err != nil {
		t.Fatal(err)
	}
	if len(chain.maxAttempts) != len(facts.orderedMigrations) || len(chain.finalStatementIndex) != len(facts.orderedMigrations) || len(schema.orderedMigrations) != len(facts.orderedMigrations) || schema.owner != candidate.owner || !sameGenerationIdentity(schema.generation, generation) {
		t.Fatal("same-verifier recovery witness is incomplete")
	}
	if err := validateRecoverySchemaWitness(schema, []EvidenceFrame{{}}); err != nil {
		t.Fatal(err)
	}
	mismatched := cloneAdmissionHistoricalVerificationFacts(facts)
	mismatched.runnerProjectionDecisionDigest = testDigest("other-decision")
	if _, _, err := buildBrandNewRecoveryWitness(candidate, generation, mismatched); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("foreign verifier facts entered recovery witness: %v", err)
	}
	before := admissionRecoveryFactsDigest(facts)
	if before == ([32]byte{}) {
		t.Fatal("recovery facts digest is empty")
	}
	facts.statementSubjects[facts.orderedMigrations[0]][0][0]++
	if admissionRecoveryFactsDigest(facts) == before {
		t.Fatal("statement subject mutation did not change recovery facts digest")
	}
}

func TestGenerationRecoveryReadyDigestRejectsCopyAndMutation(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	history := &VerifiedAdmissionHistory{currentFacts: facts, binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
	plan := &VerifiedAdmissionPlan{history: history, binding: &verifiedAdmissionPlanBinding{canonical: [32]byte{2}}}
	prior := &GenerationReplayReady{plan: plan, history: history, candidateBinding: candidate.binding, journal: testDigest("journal"), journalTail: testDigest("header"), binding: &generationReplayReadyBinding{canonical: [32]byte{3}}, consumed: &atomic.Bool{}}
	prior.consumed.Store(true)
	generation := generationIdentity{candidate.owner, candidate.verifiedRun.executionLineageDigest, prior.journal, candidate.verifiedRun.runnerProjectionDecisionDigest, candidate.verifiedRun.schemaBundleDigest}
	previous := prior.journalTail
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{owner: candidate.owner, generation: generation, segmentIndex: 0, nextSequence: 1, previousRecordDigest: &previous, lineageIndexNextSequence: 3, lineageIndexPreviousRecordDigest: testDigest("activation"), valid: valid}
	recovery := &RecoverySnapshot{owner: candidate.owner, generation: generation, cursor: cursor.clone(), tailDigest: previous, state: RecoveryBrandNew, nextPermittedAction: RecoveryBeginFirstAttempt}
	ready := &GenerationRecoveryReady{prior: prior, plan: plan, history: history, candidateBinding: candidate.binding, generation: generation, cursor: cursor, recovery: recovery, factsDigest: admissionRecoveryFactsDigest(facts), consumed: &atomic.Bool{}}
	ready.self = ready
	want := generationRecoveryReadyDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("generation recovery digest is empty")
	}
	copyReady := *ready
	if generationRecoveryReadyDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copy retained recovery digest")
	}
	for name, mutate := range map[string]func(*GenerationRecoveryReady){
		"facts":      func(v *GenerationRecoveryReady) { v.factsDigest[0]++ },
		"generation": func(v *GenerationRecoveryReady) { v.generation.journalIdentityDigest = testDigest("other-journal") },
		"sequence":   func(v *GenerationRecoveryReady) { v.cursor.nextSequence++ },
		"index":      func(v *GenerationRecoveryReady) { v.cursor.lineageIndexNextSequence++ },
		"tail":       func(v *GenerationRecoveryReady) { *v.cursor.previousRecordDigest = testDigest("other-tail") },
		"state":      func(v *GenerationRecoveryReady) { v.recovery.state = RecoveryTerminal },
		"optional": func(v *GenerationRecoveryReady) {
			migration := "000001"
			v.recovery.migrationID = &migration
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			value.cursor = ready.cursor.clone()
			value.cursor.valid = ready.cursor.valid
			value.recovery = cloneRecoverySnapshot(ready.recovery)
			mutate(&value)
			if generationRecoveryReadyDigest(&value) == want {
				t.Fatal("mutation did not change recovery digest")
			}
		})
	}
}

func bindRecoveryFactsToCandidate(facts *admissionHistoricalVerificationFacts, candidate OwnedCurrentCandidate) {
	facts.manifestDigest = candidate.verifiedRun.manifestDigest
	facts.runnerProjectionDecisionDigest = candidate.verifiedRun.runnerProjectionDecisionDigest
	facts.schemaBundleDigest = candidate.verifiedRun.schemaBundleDigest
	facts.authorityProfileDigest = candidate.verifiedRun.authorityProfileDigest
	facts.authorityBindingDigest = candidate.verifiedRun.authorityBindingDigest
	for index := range facts.ledgerRows {
		facts.ledgerRows[index].BundleDigest = facts.schemaBundleDigest
	}
}

func TestBrandNewRecoverySnapshotRejectsEveryOptionalField(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	generation := generationIdentity{candidate.owner, candidate.verifiedRun.executionLineageDigest, testDigest("journal"), candidate.verifiedRun.runnerProjectionDecisionDigest, candidate.verifiedRun.schemaBundleDigest}
	tail := testDigest("tail")
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{owner: candidate.owner, generation: generation, previousRecordDigest: &tail, valid: valid}
	baseline := &RecoverySnapshot{owner: candidate.owner, generation: generation, cursor: cursor.clone(), tailDigest: tail, state: RecoveryBrandNew, nextPermittedAction: RecoveryBeginFirstAttempt}
	if !validBrandNewRecoverySnapshot(baseline, generation, cursor, tail) {
		t.Fatal("brand-new baseline rejected")
	}
	for name, mutate := range map[string]func(*RecoverySnapshot){
		"migration":       func(v *RecoverySnapshot) { migration := "000001"; v.migrationID = &migration },
		"attempt":         func(v *RecoverySnapshot) { attempt := uint32(1); v.attemptIndex = &attempt },
		"terminal digest": func(v *RecoverySnapshot) { digest := testDigest("terminal"); v.lastTerminalDigest = &digest },
		"state digest":    func(v *RecoverySnapshot) { digest := testDigest("state"); v.lastIntermediateStateDigest = &digest },
		"intent":          func(v *RecoverySnapshot) { v.lastStatementIntent = &OwnedRecovered[StatementIntent]{} },
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneRecoverySnapshot(baseline)
			mutate(value)
			if validBrandNewRecoverySnapshot(value, generation, cursor, tail) {
				t.Fatal("non-brand-new recovery state accepted")
			}
		})
	}
}

func TestGenerationRecoveryReadyIsNotRuntimeOrAppendAuthority(t *testing.T) {
	value := any(&GenerationRecoveryReady{})
	if _, ok := value.(EvidenceJournal); ok {
		t.Fatal("recovery-ready implemented EvidenceJournal")
	}
	if _, ok := value.(interface{ Cursor() JournalCursor }); ok {
		t.Fatal("recovery-ready exposed JournalCursor")
	}
	if _, ok := value.(interface{ RecoverySnapshot() *RecoverySnapshot }); ok {
		t.Fatal("recovery-ready exposed mutable recovery snapshot")
	}
	if _, ok := value.(interface{ Connect(context.Context) error }); ok {
		t.Fatal("recovery-ready exposed Connect")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "evidence_generation_recovery.go" || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "GenerationRecoveryReady" || identifier.Name == "validGenerationRecoveryReady") {
				t.Fatalf("recovery-ready authority has unreviewed consumer in %s", name)
			}
			return true
		})
	}
}

func TestGenerationRecoveryRejectsLiteralAndCloseRequiresRegistry(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("recovery"))
	if ready, err := (&GenerationReplayReady{}).BindRecovery(context.Background(), candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal replay entered recovery: ready=%v err=%v", ready, err)
	}
	ready := &GenerationRecoveryReady{consumed: &atomic.Bool{}}
	ready.self = ready
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal recovery close=%v", err)
	}
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double recovery close=%v", err)
	}
}

func TestConsumedReplayCleanupUsesImmutableRegistryAndDominatesCause(t *testing.T) {
	_, handoff, _, _ := generationReplayFixture(t)
	consumed := &atomic.Bool{}
	consumed.Store(true)
	replay := &GenerationReplayReady{self: nil, prior: handoff, lease: handoff.lease, consumed: consumed}
	replay.self = replay
	binding := &generationReplayReadyBinding{ready: replay, lease: handoff.lease, canonical: [32]byte{1}}
	replay.binding = binding
	generationReplayReadyRegistry.Store(replay, generationReplayReadyRegistryRecord{ready: replay, binding: binding, prior: handoff, lease: handoff.lease, canonical: binding.canonical})
	_, err := replay.failRecovery(admissionCorrupt("generation-recovery-test", "stored contradiction", nil), "generation-recovery-test")
	if !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("cleanup uncertainty did not dominate corruption: %v", err)
	}
	if _, ok := generationReplayReadyRegistry.Load(replay); ok {
		t.Fatal("failed recovery retained replay registry")
	}
	if _, ok := generationHandoffReadyRegistry.Load(handoff); ok {
		t.Fatal("failed recovery retained handoff registry")
	}
}

func TestGenerationRecoveryCloseInvalidatesCursorBeforeConsumedReplayCleanup(t *testing.T) {
	_, handoff, _, _ := generationReplayFixture(t)
	consumedReplay := &atomic.Bool{}
	consumedReplay.Store(true)
	replay := &GenerationReplayReady{prior: handoff, lease: handoff.lease, consumed: consumedReplay}
	replay.self = replay
	replayBinding := &generationReplayReadyBinding{ready: replay, lease: handoff.lease, canonical: [32]byte{1}}
	replay.binding = replayBinding
	generationReplayReadyRegistry.Store(replay, generationReplayReadyRegistryRecord{ready: replay, binding: replayBinding, prior: handoff, lease: handoff.lease, canonical: replayBinding.canonical})
	cursorValid := &atomic.Bool{}
	cursorValid.Store(true)
	ready := &GenerationRecoveryReady{prior: replay, cursor: JournalCursor{valid: cursorValid}, consumed: &atomic.Bool{}}
	ready.self = ready
	recoveryBinding := &generationRecoveryReadyBinding{ready: ready, prior: replay, canonical: [32]byte{2}}
	ready.binding = recoveryBinding
	generationRecoveryReadyRegistry.Store(ready, generationRecoveryReadyRegistryRecord{ready: ready, binding: recoveryBinding, prior: replay, cursorValid: cursorValid, canonical: recoveryBinding.canonical})
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal filesystem lease close=%v", err)
	}
	if cursorValid.Load() {
		t.Fatal("recovery close retained cursor authority")
	}
	if _, ok := generationRecoveryReadyRegistry.Load(ready); ok {
		t.Fatal("recovery close retained recovery registry")
	}
	if _, ok := generationReplayReadyRegistry.Load(replay); ok {
		t.Fatal("recovery close retained replay registry")
	}
	if _, ok := generationHandoffReadyRegistry.Load(handoff); ok {
		t.Fatal("recovery close retained handoff registry")
	}
}

func TestGenerationRecoveryFrameDecoderRejectsCorruptAndOwnsBytes(t *testing.T) {
	_, ready, _, segments := generationReplayFixture(t)
	frames, err := decodeGenerationRecoveryFrames(segments[0])
	if err != nil || len(frames) != 1 || frames[0].RecordDigest != ready.headerDigest {
		t.Fatalf("frames=%+v err=%v", frames, err)
	}
	segments[0][len(segments[0])-1] ^= 1
	if _, err := decodeGenerationRecoveryFrames(segments[0]); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("corrupt frame accepted: %v", err)
	}
	if _, err := decodeGenerationRecoveryFrames(nil); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("empty segment accepted: %v", err)
	}
	generationHandoffReadyRegistry.Delete(ready)
}

func TestGenerationRecoveryReceiptAuthorityRejectsMissingAndMismatchedChain(t *testing.T) {
	candidate, handoff, _, segments := generationReplayFixture(t)
	frames, err := decodeGenerationRecoveryFrames(segments[0])
	if err != nil {
		t.Fatal(err)
	}
	replay := &GenerationReplayReady{prior: handoff, plan: handoff.plan}
	if validGenerationReplayReceiptAuthority(replay, candidate, *frames[0].Record.Header) {
		t.Fatal("unregistered typed receipts authorized recovery")
	}
	fault := cloneProjectionValue(*frames[0].Record.Header)
	fault.OuterArtifactSizeBytes++
	if validGenerationReplayReceiptAuthority(replay, candidate, fault) {
		t.Fatal("mismatched header authorized recovery")
	}
	generationHandoffReadyRegistry.Delete(handoff)
}
