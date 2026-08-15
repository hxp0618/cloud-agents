package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestHistoricalSuccessorRecoveryRejectsLiteralAndRuntimeInterfaces(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-recovery-literal"))
	if ready, err := (&HistoricalSuccessorGenerationReplayReady{}).BindRecovery(context.Background(), candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical replay entered recovery: ready=%+v err=%v", ready, err)
	}
	if validHistoricalSuccessorGenerationRecoveryReady(&HistoricalSuccessorGenerationRecoveryReady{}, candidate) {
		t.Fatal("literal historical recovery authority passed validation")
	}
	value := any(&HistoricalSuccessorGenerationRecoveryReady{})
	if _, ok := value.(EvidenceJournal); ok {
		t.Fatal("historical recovery authority implemented EvidenceJournal")
	}
	if _, ok := value.(interface{ Cursor() JournalCursor }); ok {
		t.Fatal("historical recovery authority exposed JournalCursor")
	}
	if _, ok := value.(interface{ ActiveGeneration() ActiveGeneration }); ok {
		t.Fatal("historical recovery authority exposed ActiveGeneration")
	}
	if err := (&HistoricalSuccessorGenerationRecoveryReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal historical recovery close=%v", err)
	}
	if ready, err := (&HistoricalSuccessorGenerationRecoveryReady{}).bindHeaderOnlySupersession(candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal header-only supersession bind: ready=%+v err=%v", ready, err)
	}
	if err := (&historicalSuccessorSupersessionReady{}).close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal header-only supersession close=%v", err)
	}
	forged := &historicalSuccessorSupersessionReady{consumed: &atomic.Bool{}}
	forged.self = forged
	if err := forged.close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("partially forged header-only supersession close=%v", err)
	}
}

func TestHistoricalSuccessorSupersessionDigestBindsEveryOrdinaryFact(t *testing.T) {
	prior := &HistoricalSuccessorGenerationRecoveryReady{binding: &historicalSuccessorGenerationRecoveryBinding{canonical: [32]byte{1}}}
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{2}}
	authority := &VerifiedLineageSupersessionAuthority{digest: testDigest("historical-successor-resupersession-authority")}
	terminal := testDigest("historical-successor-resupersession-terminal")
	continuation := &LineageContinuationContext{
		StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2,
		PreviousAttemptTerminalDigest: digestPointer(terminal),
		SourceJournalIdentityDigest:   testDigest("historical-successor-resupersession-source-journal"),
		SourceCheckpointRecordDigest:  testDigest("historical-successor-resupersession-source-checkpoint"),
		SourceTerminalDigest:          terminal,
	}
	ready := &historicalSuccessorSupersessionReady{
		prior: prior, candidateBinding: candidateBinding, authority: authority,
		activation: testDigest("historical-successor-resupersession-activation"), initialTail: testDigest("historical-successor-resupersession-tail"), continuation: continuation, consumed: &atomic.Bool{},
	}
	ready.self = ready
	baseline := historicalSuccessorSupersessionDigest(ready)
	if baseline == ([32]byte{}) {
		t.Fatal("header-only supersession digest was not minted")
	}
	for name, mutate := range map[string]func(*historicalSuccessorSupersessionReady){
		"prior":     func(value *historicalSuccessorSupersessionReady) { value.prior.binding.canonical[0]++ },
		"candidate": func(value *historicalSuccessorSupersessionReady) { value.candidateBinding.canonical[0]++ },
		"authority": func(value *historicalSuccessorSupersessionReady) {
			value.authority.digest = testDigest("historical-successor-resupersession-other-authority")
		},
		"activation": func(value *historicalSuccessorSupersessionReady) {
			value.activation = testDigest("historical-successor-resupersession-other-activation")
		},
		"tail": func(value *historicalSuccessorSupersessionReady) {
			value.initialTail = testDigest("historical-successor-resupersession-other-tail")
		},
		"continuation": func(value *historicalSuccessorSupersessionReady) {
			value.continuation.AttemptIndex++
		},
	} {
		t.Run(name, func(t *testing.T) {
			copyReady := *ready
			copyPrior := *prior
			copyPriorBinding := *prior.binding
			copyPrior.binding = &copyPriorBinding
			copyCandidateBinding := *candidateBinding
			copyReady.prior = &copyPrior
			copyReady.candidateBinding = &copyCandidateBinding
			copyReady.authority = &VerifiedLineageSupersessionAuthority{digest: authority.digest}
			copyReady.continuation = cloneProjectionValue(continuation)
			copyReady.self = &copyReady
			mutate(&copyReady)
			if historicalSuccessorSupersessionDigest(&copyReady) == baseline {
				t.Fatal("header-only supersession mutation retained canonical digest")
			}
		})
	}
}

func TestHistoricalSuccessorRequiresSupersessionUsesImmutableRegistry(t *testing.T) {
	ready := &HistoricalSuccessorGenerationRecoveryReady{requiresSupersession: true, consumed: &atomic.Bool{}}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationRecoveryBinding{ready: ready, canonical: [32]byte{1}}
	historicalSuccessorGenerationRecoveryRegistry.Store(ready, historicalSuccessorGenerationRecoveryRecord{
		ready: ready, binding: ready.binding, requiresSupersession: true, canonical: ready.binding.canonical,
	})
	t.Cleanup(func() { historicalSuccessorGenerationRecoveryRegistry.Delete(ready) })
	if !ready.RequiresSupersession() {
		t.Fatal("sealed historical recovery lost supersession diagnostic")
	}
	copyReady := *ready
	if copyReady.RequiresSupersession() {
		t.Fatal("copied historical recovery retained supersession diagnostic authority")
	}
	ready.consumed.Store(true)
	if ready.RequiresSupersession() {
		t.Fatal("consumed historical recovery retained supersession diagnostic authority")
	}
}

func TestRegisteredBrandNewRecoveryWitnessUsesGenerationFacts(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-witness"))
	facts := admissionHistoricalFactsFixture(t)
	bindRecoveryFactsToCandidate(facts, candidate)
	generation := generationIdentity{
		owner: candidate.owner, executionLineageDigest: candidate.verifiedRun.executionLineageDigest, journalIdentityDigest: testDigest("historical-witness-journal"),
		runnerProjectionDecisionDigest: candidate.verifiedRun.runnerProjectionDecisionDigest, schemaBundleDigest: candidate.verifiedRun.schemaBundleDigest,
	}
	runtime := ownedContentReceiptWitness{"runtime", testDigest("historical-witness-runtime"), 128}
	recovery := ownedContentReceiptWitness{"decision_recovery", testDigest("historical-witness-recovery"), 64}
	chain, schema, err := buildBrandNewRecoveryWitnessFromFacts(generation, facts, runtime, recovery)
	if err != nil {
		t.Fatal(err)
	}
	if chain.runtimeReceipt != runtime || chain.recoveryReceipt != recovery || schema.owner != candidate.owner || !sameGenerationIdentity(schema.generation, generation) || len(schema.orderedMigrations) != len(facts.orderedMigrations) {
		t.Fatal("registered generation facts did not produce an exact recovery witness")
	}
	mismatched := cloneAdmissionHistoricalVerificationFacts(facts)
	mismatched.runnerProjectionDecisionDigest = testDigest("other-historical-witness-decision")
	if _, _, err := buildBrandNewRecoveryWitnessFromFacts(generation, mismatched, runtime, recovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("foreign historical facts entered recovery witness: %v", err)
	}
	oversized := runtime
	oversized.sizeBytes = maxRuntimeTarSize + 1
	if _, _, err := buildBrandNewRecoveryWitnessFromFacts(generation, facts, oversized, recovery); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("oversized registered runtime entered recovery witness: %v", err)
	}
}

func TestHistoricalSuccessorRecoveredContinuationBindsReservedContext(t *testing.T) {
	ready, _, _, _, _ := historicalSuccessorGenerationReplayFixture(t)
	handoff := &HistoricalSuccessorGenerationHandoffReady{prior: ready}
	replay := &HistoricalSuccessorGenerationReplayReady{prior: handoff, journalTail: ready.headerFrame.RecordDigest}
	owner := &evidenceOwnerToken{nonce: [16]byte{2}}
	header := ready.headerFrame.Record.Header
	if header == nil {
		t.Fatal("historical successor fixture has no header")
	}
	generation := generationIdentity{owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
	previous := replay.journalTail
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: owner, generation: generation, segmentIndex: 0, nextSequence: 1, previousRecordDigest: &previous,
		lineageIndexNextSequence: ready.indexRecords, lineageIndexPreviousRecordDigest: ready.activatedFrame.RecordDigest, valid: valid,
	}
	continuation, err := historicalSuccessorRecoveredContinuation(replay, generation, cursor)
	reserved := ready.reservedFrame.Record.Reserved
	if err != nil || reserved == nil || reserved.Continuation == nil || continuation.owned == nil || continuation.inheritedWithoutContext || continuation.owned.recordDigest != ready.reservedFrame.RecordDigest || !canonicalEqual(continuation.owned.value, *reserved.Continuation) || !sameCursorIdentity(continuation.owned.cursor, cursor) {
		t.Fatalf("historical successor continuation was not exact: continuation=%+v err=%v", continuation, err)
	}

	without := cloneProjectionValue(ready.reservedFrame)
	without.Record.Reserved.Continuation = nil
	readyCopy := *ready
	readyCopy.reservedFrame = without
	handoffCopy := *handoff
	handoffCopy.prior = &readyCopy
	replayCopy := *replay
	replayCopy.prior = &handoffCopy
	inherited, err := historicalSuccessorRecoveredContinuation(&replayCopy, generation, cursor)
	if err != nil || inherited.owned != nil || !inherited.inheritedWithoutContext {
		t.Fatalf("header-only inherited continuation was not retained: continuation=%+v err=%v", inherited, err)
	}
}

func TestHistoricalSuccessorRecoveryActionIsClosedByContinuation(t *testing.T) {
	ready, _, _, _, _ := historicalSuccessorGenerationReplayFixture(t)
	handoff := &HistoricalSuccessorGenerationHandoffReady{prior: ready}
	replay := &HistoricalSuccessorGenerationReplayReady{prior: handoff}
	for name, test := range map[string]struct {
		continuation *LineageContinuationContext
		action       RecoveryAction
	}{
		"header carry": {nil, RecoveryBeginFirstAttempt},
		"retry":        {&LineageContinuationContext{StartAction: "begin_next_attempt"}, RecoveryBeginNextAttempt},
		"next entry":   {&LineageContinuationContext{StartAction: "begin_first_attempt_next_entry"}, RecoveryBeginFirstAttemptNextEntry},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := &RecoverySnapshot{state: RecoveryBrandNewInherited, nextPermittedAction: test.action}
			if test.continuation != nil {
				snapshot.lineageContinuation = &OwnedRecovered[LineageContinuationContext]{recordDigest: ready.reservedFrame.RecordDigest, value: cloneProjectionValue(*test.continuation)}
			}
			if err := validateHistoricalSuccessorRecoveryAction(replay, snapshot, test.continuation); err != nil {
				t.Fatal(err)
			}
			wrong := *snapshot
			wrong.nextPermittedAction = RecoveryReturnFailure
			if err := validateHistoricalSuccessorRecoveryAction(replay, &wrong, test.continuation); err == nil {
				t.Fatal("wrong historical successor recovery action was accepted")
			}
		})
	}
}

func TestHistoricalSuccessorGenerationRecoveryDigestRejectsCopyAndMutation(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	validation := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{3}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	cursor := recoveryFixtureCursor(generation, frames[:1])
	schema := recoveryFixtureSchema(t, owner, generation, frames[:1], validation)
	recovery, err := buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{inheritedWithoutContext: true}, schema)
	if err != nil {
		t.Fatal(err)
	}
	planned := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{1}}
	handoff := &HistoricalSuccessorGenerationHandoffReady{binding: &historicalSuccessorGenerationHandoffBinding{canonical: [32]byte{2}}}
	replay := &HistoricalSuccessorGenerationReplayReady{prior: handoff, binding: &historicalSuccessorGenerationReplayBinding{canonical: [32]byte{3}}}
	ready := &HistoricalSuccessorGenerationRecoveryReady{
		prior: replay, planned: planned, candidateBinding: &verifiedEvidenceRunBinding{canonical: [32]byte{4}},
		generation: generation, cursor: cursor, recovery: recovery, factsDigest: [32]byte{5}, consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := historicalSuccessorGenerationRecoveryDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("historical successor recovery digest is empty")
	}
	copyReady := *ready
	if historicalSuccessorGenerationRecoveryDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copied historical successor recovery retained digest authority")
	}
	for name, mutate := range map[string]func(*HistoricalSuccessorGenerationRecoveryReady){
		"facts": func(value *HistoricalSuccessorGenerationRecoveryReady) { value.factsDigest[0]++ },
		"generation": func(value *HistoricalSuccessorGenerationRecoveryReady) {
			value.generation.journalIdentityDigest = testDigest("other-historical-recovery-journal")
		},
		"cursor": func(value *HistoricalSuccessorGenerationRecoveryReady) { value.cursor.lineageIndexNextSequence++ },
		"recovery": func(value *HistoricalSuccessorGenerationRecoveryReady) {
			value.recovery.nextPermittedAction = RecoveryReturnFailure
		},
		"planned": func(value *HistoricalSuccessorGenerationRecoveryReady) {
			plannedCopy := *value.planned
			plannedCopy.canonical[0]++
			value.planned = &plannedCopy
		},
		"currentness": func(value *HistoricalSuccessorGenerationRecoveryReady) {
			value.requiresSupersession = true
			value.executionBindings = &VerifiedRecoveryExecutionBindings{digest: testDigest("historical-recovery-execution")}
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			value.cursor = ready.cursor.clone()
			value.recovery = cloneRecoverySnapshot(ready.recovery)
			mutate(&value)
			if historicalSuccessorGenerationRecoveryDigest(&value) == want {
				t.Fatal("historical successor recovery mutation retained canonical digest")
			}
		})
	}
}

func TestHistoricalSuccessorRecoveryExecutionRequiresPolicyForHistoricalB(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("historical-successor-execution"))
	planned := registeredGenerationDigestFixture(t, candidate)
	if execution, required, err := bindHistoricalSuccessorRecoveryExecution(planned, candidate.verifiedRun.currentDecision, &RecoverySnapshot{}); err != nil || required || execution != nil {
		t.Fatalf("current B unexpectedly required supersession: execution=%+v required=%t err=%v", execution, required, err)
	}
	historical := *planned
	historical.decision.digest = testDigest("historical-successor-old-decision")
	if execution, required, err := bindHistoricalSuccessorRecoveryExecution(&historical, candidate.verifiedRun.currentDecision, &RecoverySnapshot{}); execution != nil || !required || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("historical B escaped without policy: execution=%+v required=%t err=%v", execution, required, err)
	}
}

func TestHistoricalSuccessorRecoveryRegistryProvenanceSurvivesCursorInvalidation(t *testing.T) {
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{1}}
	planned := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{2}}
	lease := &evidencefs.GenerationLease{}
	generationReady := &HistoricalSuccessorGenerationReadyPermit{planned: planned}
	handoff := &HistoricalSuccessorGenerationHandoffReady{prior: generationReady, candidateBinding: candidateBinding, lease: lease}
	handoff.self = handoff
	handoff.binding = &historicalSuccessorGenerationHandoffBinding{ready: handoff, prior: generationReady, candidateBinding: candidateBinding, lease: lease, canonical: [32]byte{3}}
	replay := &HistoricalSuccessorGenerationReplayReady{prior: handoff, candidateBinding: candidateBinding, lease: lease, snapshot: &evidencefs.GenerationSnapshot{}}
	replay.self = replay
	replay.binding = &historicalSuccessorGenerationReplayBinding{ready: replay, prior: handoff, candidateBinding: candidateBinding, lease: lease, snapshot: replay.snapshot, canonical: [32]byte{4}}
	consumed := &atomic.Bool{}
	consumed.Store(true)
	cursorValid := &atomic.Bool{}
	ready := &HistoricalSuccessorGenerationRecoveryReady{
		prior: replay, planned: planned, candidateBinding: candidateBinding, cursor: JournalCursor{valid: cursorValid},
		recovery: &RecoverySnapshot{}, factsDigest: [32]byte{5}, consumed: consumed,
	}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationRecoveryBinding{ready: ready, prior: replay, planned: planned, candidateBinding: candidateBinding, canonical: [32]byte{6}}
	historicalSuccessorGenerationHandoffRegistry.Store(handoff, historicalSuccessorGenerationHandoffRecord{
		ready: handoff, binding: handoff.binding, prior: generationReady, planned: planned,
		candidateBinding: candidateBinding, lease: lease, canonical: handoff.binding.canonical,
	})
	historicalSuccessorGenerationReplayRegistry.Store(replay, historicalSuccessorGenerationReplayRecord{
		ready: replay, binding: replay.binding, prior: handoff, candidateBinding: candidateBinding,
		lease: lease, snapshot: replay.snapshot, canonical: replay.binding.canonical,
	})
	historicalSuccessorGenerationRecoveryRegistry.Store(ready, historicalSuccessorGenerationRecoveryRecord{
		ready: ready, binding: ready.binding, prior: replay, planned: planned, candidateBinding: candidateBinding,
		cursorValid: cursorValid, canonical: ready.binding.canonical,
	})
	t.Cleanup(func() {
		historicalSuccessorGenerationRecoveryRegistry.Delete(ready)
		historicalSuccessorGenerationReplayRegistry.Delete(replay)
		historicalSuccessorGenerationHandoffRegistry.Delete(handoff)
	})
	if cursorValid.Load() {
		t.Fatal("fixture cursor unexpectedly remained valid")
	}
	if !historicalSuccessorGenerationRecoveryReadyRecordMatches(ready) {
		t.Fatal("consumed historical recovery provenance depended on obsolete cursor validity")
	}
}

func TestHistoricalSuccessorRecoveryCloseInvalidatesCursorAndRegistryChain(t *testing.T) {
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{1}}
	planned := &verifiedAdmissionRegisteredGeneration{canonical: [32]byte{2}}
	lease := &evidencefs.GenerationLease{}
	generationReady := &HistoricalSuccessorGenerationReadyPermit{planned: planned}
	handoff := &HistoricalSuccessorGenerationHandoffReady{prior: generationReady, candidateBinding: candidateBinding, lease: lease}
	handoff.self = handoff
	handoff.binding = &historicalSuccessorGenerationHandoffBinding{ready: handoff, prior: generationReady, candidateBinding: candidateBinding, lease: lease, canonical: [32]byte{3}}
	replay := &HistoricalSuccessorGenerationReplayReady{prior: handoff, candidateBinding: candidateBinding, lease: lease, snapshot: &evidencefs.GenerationSnapshot{}}
	replay.self = replay
	replay.binding = &historicalSuccessorGenerationReplayBinding{ready: replay, prior: handoff, candidateBinding: candidateBinding, lease: lease, snapshot: replay.snapshot, canonical: [32]byte{4}}
	cursorValid := &atomic.Bool{}
	cursorValid.Store(true)
	ready := &HistoricalSuccessorGenerationRecoveryReady{
		prior: replay, planned: planned, candidateBinding: candidateBinding, cursor: JournalCursor{valid: cursorValid},
		recovery: &RecoverySnapshot{}, factsDigest: [32]byte{5}, consumed: &atomic.Bool{},
	}
	ready.self = ready
	ready.binding = &historicalSuccessorGenerationRecoveryBinding{ready: ready, prior: replay, planned: planned, candidateBinding: candidateBinding, canonical: [32]byte{6}}
	historicalSuccessorGenerationHandoffRegistry.Store(handoff, historicalSuccessorGenerationHandoffRecord{ready: handoff, binding: handoff.binding, prior: generationReady, planned: planned, candidateBinding: candidateBinding, lease: lease, canonical: handoff.binding.canonical})
	historicalSuccessorGenerationReplayRegistry.Store(replay, historicalSuccessorGenerationReplayRecord{ready: replay, binding: replay.binding, prior: handoff, candidateBinding: candidateBinding, lease: lease, snapshot: replay.snapshot, canonical: replay.binding.canonical})
	historicalSuccessorGenerationRecoveryRegistry.Store(ready, historicalSuccessorGenerationRecoveryRecord{ready: ready, binding: ready.binding, prior: replay, planned: planned, candidateBinding: candidateBinding, cursorValid: cursorValid, canonical: ready.binding.canonical})
	if err := ready.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal retained lease close=%v", err)
	}
	if cursorValid.Load() {
		t.Fatal("historical recovery close retained cursor authority")
	}
	for name, entry := range map[string]struct {
		registry *sync.Map
		key      any
	}{
		"recovery": {&historicalSuccessorGenerationRecoveryRegistry, ready},
		"replay":   {&historicalSuccessorGenerationReplayRegistry, replay},
		"handoff":  {&historicalSuccessorGenerationHandoffRegistry, handoff},
	} {
		if _, ok := entry.registry.Load(entry.key); ok {
			t.Fatalf("%s registry survived historical recovery close", name)
		}
	}
}

func TestHistoricalSuccessorRecoveryAuthorityDoesNotSpread(t *testing.T) {
	const owner = "evidence_historical_supersession_recovery.go"
	allowedConsumers := map[string]map[string]bool{
		"evidence_generation_journal.go": {
			"HistoricalSuccessorGenerationRecoveryReady":              true,
			"validHistoricalSuccessorGenerationRecoveryReady":         true,
			"historicalSuccessorGenerationRecoveryDigest":             true,
			"historicalSuccessorGenerationRecoveryReadyRecordMatches": true,
			"closeConsumedHistoricalSuccessorGenerationRecovery":      true,
		},
		"evidence_session.go": {
			"HistoricalSuccessorGenerationRecoveryReady":      true,
			"validHistoricalSuccessorGenerationRecoveryReady": true,
		},
	}
	allowedMethods := map[string]map[string]bool{
		"HistoricalSuccessorGenerationReplayReady":   {"BindRecovery": true, "Close": true},
		"HistoricalSuccessorGenerationRecoveryReady": {"RequiresSupersession": true, "BindJournal": true, "BindSession": true, "Close": true},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == owner || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (strings.HasPrefix(identifier.Name, "HistoricalSuccessorGenerationRecovery") || strings.HasPrefix(identifier.Name, "historicalSuccessorGenerationRecovery") || strings.HasPrefix(identifier.Name, "validHistoricalSuccessorGenerationRecovery") || strings.HasPrefix(identifier.Name, "validConsumedHistoricalSuccessorGenerationRecovery") || strings.HasPrefix(identifier.Name, "historicalSuccessorSupersession") || identifier.Name == "closeConsumedHistoricalSuccessorGenerationRecovery") && !allowedConsumers[name][identifier.Name] {
				t.Fatalf("historical successor recovery authority spread into %s through %s", name, identifier.Name)
			}
			return true
		})
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
			if name == owner {
				if call, ok := node.(*ast.CallExpr); ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && (selector.Sel.Name == "Connect" || selector.Sel.Name == "Open" || selector.Sel.Name == "BindJournal") {
						t.Fatalf("historical successor recovery called forbidden runtime entrypoint %s", selector.Sel.Name)
					}
				}
			}
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 || !ast.IsExported(function.Name.Name) {
				return true
			}
			star, ok := function.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				return true
			}
			receiver, ok := star.X.(*ast.Ident)
			if ok && allowedMethods[receiver.Name] != nil && !allowedMethods[receiver.Name][function.Name.Name] {
				t.Fatalf("historical successor recovery stage %s exposed unexpected method %s in %s", receiver.Name, function.Name.Name, name)
			}
			return true
		})
	}
}

func TestHistoricalSuccessorRuntimeBindersRejectHistoricalBBeforeMutation(t *testing.T) {
	for name, test := range map[string]struct {
		file  string
		start string
		end   string
		later []string
	}{
		"journal": {
			file:  "evidence_generation_journal.go",
			start: "func (r *HistoricalSuccessorGenerationRecoveryReady) BindJournal",
			end:   "func consumeGenerationJournalRecovery",
			later: []string{"contextAdmissionError(ctx)", ".Revalidate(ctx)", "consumeGenerationJournalRecovery("},
		},
		"session": {
			file:  "evidence_session.go",
			start: "func (r *HistoricalSuccessorGenerationRecoveryReady) BindSession",
			end:   "func bindGenerationEvidenceSession",
			later: []string{"r.BindJournal(ctx, candidate)"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(test.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(raw)
			start := strings.Index(source, test.start)
			if start < 0 {
				t.Fatal("runtime binder is absent")
			}
			endOffset := strings.Index(source[start:], test.end)
			if endOffset < 0 {
				t.Fatal("runtime binder boundary is absent")
			}
			method := source[start : start+endOffset]
			guard := strings.Index(method, "if r.requiresSupersession")
			if guard < 0 {
				t.Fatal("historical currentness guard is absent")
			}
			for _, later := range test.later {
				position := strings.Index(method, later)
				if position < 0 || position <= guard {
					t.Fatalf("%s is absent or precedes the historical currentness guard", later)
				}
			}
		})
	}
}

func TestRecoveryWitnessConstructorsHaveOnlyReviewedConsumers(t *testing.T) {
	allowed := map[string]map[string]bool{
		"evidence_generation_recovery.go": {
			"buildBrandNewRecoveryWitnessFromFacts":  true,
			"buildRegisteredBrandNewRecoveryWitness": true,
		},
		"evidence_historical_supersession_recovery.go": {
			"buildRegisteredBrandNewRecoveryWitness": true,
		},
		"evidence_generation_journal.go": {
			"buildRegisteredBrandNewRecoveryWitness": true,
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
			if ok && (identifier.Name == "buildBrandNewRecoveryWitnessFromFacts" || identifier.Name == "buildRegisteredBrandNewRecoveryWitness") && !allowed[name][identifier.Name] {
				t.Fatalf("recovery witness constructor %s spread into %s", identifier.Name, name)
			}
			return true
		})
	}
}
