package migration

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestSuccessorGenerationRecoveryRejectsLiteralAndRuntimeInterfaces(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("successor-recovery-literal"))
	if ready, err := (&SuccessorGenerationReplayReady{}).BindRecovery(context.Background(), candidate); ready != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal successor replay entered recovery: ready=%+v err=%v", ready, err)
	}
	if validSuccessorGenerationRecoveryReady(&SuccessorGenerationRecoveryReady{}, candidate) {
		t.Fatal("literal successor recovery authority passed validation")
	}
	value := any(&SuccessorGenerationRecoveryReady{})
	if _, ok := value.(EvidenceJournal); ok {
		t.Fatal("successor recovery authority implemented EvidenceJournal")
	}
	if _, ok := value.(interface{ Cursor() JournalCursor }); ok {
		t.Fatal("successor recovery authority exposed JournalCursor")
	}
	if err := (&SuccessorGenerationRecoveryReady{}).Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal successor recovery close=%v", err)
	}
}

func TestSuccessorRecoveredContinuationBindsReservedContext(t *testing.T) {
	replay, _, _, _, _ := successorGenerationReplayFixture(t)
	owner := &evidenceOwnerToken{nonce: [16]byte{1}}
	reserved := replay.state.plan.reservedFrame.Record.Reserved
	generation := generationIdentity{
		owner: owner, executionLineageDigest: reserved.ExecutionLineageDigest,
		journalIdentityDigest: reserved.JournalIdentityDigest, runnerProjectionDecisionDigest: reserved.RunnerProjectionDecisionDigest,
		schemaBundleDigest: reserved.SchemaBundleDigest,
	}
	previous := replay.state.headerDigest
	valid := &atomic.Bool{}
	valid.Store(true)
	cursor := JournalCursor{
		owner: owner, generation: generation, segmentIndex: 0, nextSequence: 1, previousRecordDigest: &previous,
		lineageIndexNextSequence: replay.state.indexRecords, lineageIndexPreviousRecordDigest: replay.state.activationDigest, valid: valid,
	}
	continuation, err := successorRecoveredContinuation(&SuccessorGenerationReplayReady{state: replay.state, journalTail: previous}, generation, cursor)
	if err != nil || continuation.owned == nil || continuation.inheritedWithoutContext || !canonicalEqual(continuation.owned.value, *reserved.Continuation) || continuation.owned.recordDigest != replay.state.reservedDigest || continuation.owned.tailDigest != previous || !sameCursorIdentity(continuation.owned.cursor, cursor) {
		t.Fatalf("successor continuation was not exact: continuation=%+v err=%v", continuation, err)
	}

	without := cloneProjectionValue(*reserved)
	without.Continuation = nil
	state := *replay.state
	plan := *replay.state.plan
	plan.reservedFrame = cloneProjectionValue(replay.state.plan.reservedFrame)
	plan.reservedFrame.Record.Reserved = &without
	state.plan = &plan
	inherited, err := successorRecoveredContinuation(&SuccessorGenerationReplayReady{state: &state, journalTail: previous}, generation, cursor)
	if err != nil || inherited.owned != nil || !inherited.inheritedWithoutContext {
		t.Fatalf("header-only inherited continuation was not retained: continuation=%+v err=%v", inherited, err)
	}
}

func TestSuccessorRecoveryActionIsClosedByContinuation(t *testing.T) {
	replay := &SuccessorGenerationReplayReady{state: &successorAdmissionState{reservedDigest: testDigest("successor-action-reserved")}}
	for name, test := range map[string]struct {
		continuation *LineageContinuationContext
		action       RecoveryAction
	}{
		"header carry": {nil, RecoveryBeginFirstAttempt},
		"retry": {
			&LineageContinuationContext{StartAction: "begin_next_attempt"},
			RecoveryBeginNextAttempt,
		},
		"next entry": {
			&LineageContinuationContext{StartAction: "begin_first_attempt_next_entry"},
			RecoveryBeginFirstAttemptNextEntry,
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := &RecoverySnapshot{state: RecoveryBrandNewInherited, nextPermittedAction: test.action}
			if test.continuation != nil {
				snapshot.lineageContinuation = &OwnedRecovered[LineageContinuationContext]{
					recordDigest: replay.state.reservedDigest, value: cloneProjectionValue(*test.continuation),
				}
			}
			if err := replay.validateSuccessorRecoveryAction(snapshot, test.continuation); err != nil {
				t.Fatal(err)
			}
			wrong := *snapshot
			wrong.nextPermittedAction = RecoveryReturnSuccess
			if err := replay.validateSuccessorRecoveryAction(&wrong, test.continuation); err == nil {
				t.Fatal("wrong successor recovery action was accepted")
			}
		})
	}
	unknown := &LineageContinuationContext{StartAction: "unknown"}
	unknownSnapshot := &RecoverySnapshot{
		state: RecoveryBrandNewInherited,
		lineageContinuation: &OwnedRecovered[LineageContinuationContext]{
			recordDigest: replay.state.reservedDigest, value: *unknown,
		},
	}
	if err := replay.validateSuccessorRecoveryAction(unknownSnapshot, unknown); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("unknown continuation action=%v", err)
	}
}

func TestSuccessorGenerationRecoveryDigestRejectsCopyAndMutation(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	validation := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{2}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	cursor := recoveryFixtureCursor(generation, frames[:1])
	schema := recoveryFixtureSchema(t, owner, generation, frames[:1], validation)
	recovery, err := buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{inheritedWithoutContext: true}, schema)
	if err != nil {
		t.Fatal(err)
	}
	state := &successorAdmissionState{binding: &successorAdmissionStateBinding{canonical: [32]byte{1}}}
	prior := &SuccessorGenerationReplayReady{binding: &successorGenerationReplayBinding{canonical: [32]byte{2}}}
	ready := &SuccessorGenerationRecoveryReady{
		prior: prior, state: state, candidateBinding: &verifiedEvidenceRunBinding{canonical: [32]byte{3}},
		generation: generation, cursor: cursor, recovery: recovery, factsDigest: [32]byte{4}, consumed: &atomic.Bool{},
	}
	ready.self = ready
	want := successorGenerationRecoveryDigest(ready)
	if want == ([32]byte{}) {
		t.Fatal("successor recovery digest is empty")
	}
	copyReady := *ready
	if successorGenerationRecoveryDigest(&copyReady) != ([32]byte{}) {
		t.Fatal("copied successor recovery retained digest authority")
	}
	for name, mutate := range map[string]func(*SuccessorGenerationRecoveryReady){
		"facts": func(value *SuccessorGenerationRecoveryReady) { value.factsDigest[0]++ },
		"generation": func(value *SuccessorGenerationRecoveryReady) {
			value.generation.journalIdentityDigest = testDigest("other-journal")
		},
		"cursor": func(value *SuccessorGenerationRecoveryReady) { value.cursor.lineageIndexNextSequence++ },
		"recovery": func(value *SuccessorGenerationRecoveryReady) {
			value.recovery.nextPermittedAction = RecoveryReturnFailure
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := *ready
			value.self = &value
			value.cursor = ready.cursor.clone()
			value.recovery = cloneRecoverySnapshot(ready.recovery)
			mutate(&value)
			if successorGenerationRecoveryDigest(&value) == want {
				t.Fatal("successor recovery mutation did not change digest")
			}
		})
	}
}

func TestSuccessorRecoveryRegistryProvenanceSurvivesConsumedCursorInvalidation(t *testing.T) {
	state := &successorAdmissionState{binding: &successorAdmissionStateBinding{canonical: [32]byte{1}}}
	candidateBinding := &verifiedEvidenceRunBinding{canonical: [32]byte{2}}
	lease := &evidencefs.GenerationLease{}
	handoff := &SuccessorGenerationHandoffReady{state: state, candidateBinding: candidateBinding, lease: lease}
	handoff.self = handoff
	handoff.binding = &successorGenerationHandoffBinding{ready: handoff, state: state, stateBinding: state.binding, candidateBinding: candidateBinding, lease: lease, canonical: [32]byte{3}}
	replay := &SuccessorGenerationReplayReady{prior: handoff, state: state, candidateBinding: candidateBinding, lease: lease, snapshot: &evidencefs.GenerationSnapshot{}}
	replay.self = replay
	replay.binding = &successorGenerationReplayBinding{ready: replay, prior: handoff, state: state, stateBinding: state.binding, candidateBinding: candidateBinding, lease: lease, snapshot: replay.snapshot, canonical: [32]byte{4}}
	consumed := &atomic.Bool{}
	consumed.Store(true)
	valid := &atomic.Bool{}
	ready := &SuccessorGenerationRecoveryReady{
		prior: replay, state: state, candidateBinding: candidateBinding, cursor: JournalCursor{valid: valid},
		recovery: &RecoverySnapshot{}, factsDigest: [32]byte{5}, consumed: consumed,
	}
	ready.self = ready
	ready.binding = &successorGenerationRecoveryBinding{ready: ready, prior: replay, state: state, candidateBinding: candidateBinding, canonical: [32]byte{6}}
	successorGenerationHandoffRegistry.Store(handoff, successorGenerationHandoffRecord{
		ready: handoff, binding: handoff.binding, state: state, stateBinding: state.binding,
		candidateBinding: candidateBinding, lease: lease, canonical: handoff.binding.canonical,
	})
	successorGenerationReplayRegistry.Store(replay, successorGenerationReplayRecord{
		ready: replay, binding: replay.binding, prior: handoff, state: state, stateBinding: state.binding,
		candidateBinding: candidateBinding, lease: lease, snapshot: replay.snapshot, canonical: replay.binding.canonical,
	})
	successorGenerationRecoveryRegistry.Store(ready, successorGenerationRecoveryRecord{
		ready: ready, binding: ready.binding, prior: replay, state: state, candidateBinding: candidateBinding,
		cursorValid: valid, canonical: ready.binding.canonical,
	})
	t.Cleanup(func() {
		successorGenerationRecoveryRegistry.Delete(ready)
		successorGenerationReplayRegistry.Delete(replay)
		successorGenerationHandoffRegistry.Delete(handoff)
	})
	if valid.Load() {
		t.Fatal("fixture cursor unexpectedly remained valid")
	}
	if !successorGenerationRecoveryReadyRecordMatches(ready) {
		t.Fatal("consumed journal provenance depended on the obsolete cursor remaining valid")
	}
}
