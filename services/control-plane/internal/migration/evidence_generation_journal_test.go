package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sync/atomic"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func TestGenerationEvidenceJournalPreparesExactCompositeWithoutConsumingRecord(t *testing.T) {
	journal, cursor, owned, terminal, prefix := generationJournalPreparationFixture(t)
	prepared, err := journal.prepareAppendLocked(cursor, owned)
	if err != nil {
		t.Fatal(err)
	}
	if owned.consumed.Load() || !cursor.Valid() || prepared.frame.RecordDigest != terminal.RecordDigest || prepared.checkpoint.Record.Checkpoint == nil || prepared.checkpoint.Record.Checkpoint.JournalTailDigest != terminal.RecordDigest || prepared.checkpoint.Record.Checkpoint.JournalNextSequence != terminal.Sequence+1 || prepared.nextCursor.nextSequence != cursor.nextSequence+1 || prepared.nextCursor.lineageIndexNextSequence != cursor.lineageIndexNextSequence+1 || prepared.recovery == nil || prepared.recovery.State() != RecoveryCompleted || prepared.recovery.NextAction() != RecoveryReturnSuccess || prepared.journalRecords != uint64(len(prefix)+1) || prepared.checkpointRecords != journal.state.checkpointRecords+1 || prepared.canonical == ([32]byte{}) {
		t.Fatalf("prepared append mismatch: prepared=%+v recovery=%+v", prepared, prepared.recovery)
	}
	want := prepared.canonical
	prepared.invalidate()
	if prepared.nextCursor.Valid() || preparedGenerationJournalAppendDigest(prepared) != want {
		t.Fatal("cursor invalidation destroyed or retained the wrong prepared authority")
	}
	state := &generationEvidenceJournalState{
		journal: journal, indexFact: evidencefs.GenerationFileFact{Size: 1, ContentDigest: [32]byte{1}, IdentityDigest: [32]byte{2}},
		segmentFacts: []evidencefs.GenerationFileFact{{Size: 1, ContentDigest: [32]byte{3}, IdentityDigest: [32]byte{4}}},
		cursor:       cursor, journalRecords: prepared.journalRecords, journalBytes: prepared.journalBytes,
		segmentRecords: prepared.segmentRecords, segmentBytes: prepared.segmentBytes, checkpointRecords: prepared.checkpointRecords,
		indexDebitRecords: prepared.indexDebitRecords, indexDebitBytes: prepared.indexDebitBytes,
		unknown: &generationJournalUnknownAppend{prepared: prepared},
	}
	state.self = state
	state.cursor.valid.Store(false)
	if generationEvidenceJournalStateDigest(state) != ([32]byte{}) {
		t.Fatal("literal filesystem outcome entered an unknown state seal")
	}
}

func TestGenerationEvidenceJournalPreflightLimitsPreserveAppendAuthority(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*generationEvidenceJournal)
		code   ErrorCode
	}{
		"segment reservation": {
			mutate: func(j *generationEvidenceJournal) {
				j.state.segmentBytes = evidenceSegmentMaximumBytes
				j.state.journalBytes = evidenceSegmentMaximumBytes
				j.reservation.ReservedJournalBytes = evidenceSegmentMaximumBytes + maxEvidenceFrameBytes
				j.reservation.ReservedBytes = j.reservation.ReservedJournalBytes + j.reservation.ReservedIndexBytes
			},
			code: CodeEvidenceJournalLimitExceeded,
		},
		"reservation": {
			mutate: func(j *generationEvidenceJournal) {
				j.reservation.ReservedRecords = j.state.journalRecords
				j.reservation.ReservedCheckpointRecords = j.state.checkpointRecords
			},
			code: CodeEvidenceJournalLimitExceeded,
		},
	} {
		t.Run(name, func(t *testing.T) {
			journal, cursor, owned, _, _ := generationJournalPreparationFixture(t)
			test.mutate(journal)
			prepared, err := journal.prepareAppendLocked(cursor, owned)
			if prepared != nil || !IsCode(err, test.code) || owned.consumed.Load() || !cursor.Valid() {
				t.Fatalf("prepared=%+v err=%v consumed=%v cursor=%v", prepared, err, owned.consumed.Load(), cursor.Valid())
			}
		})
	}
}

func TestGenerationEvidenceJournalPreparesExactRotationComposite(t *testing.T) {
	journal, cursor, owned, terminal, _ := generationJournalPreparationFixtureWithSegments(t, 2)
	journal.state.segmentRecords = evidenceSegmentMaximumRecords
	journal.state.segmentFacts = []evidencefs.GenerationFileFact{{Ordinal: cursor.segmentIndex, Size: journal.state.segmentBytes, ContentDigest: [32]byte{1}, IdentityDigest: [32]byte{2}}}
	prepared, err := journal.prepareAppendLocked(cursor, owned)
	if err != nil {
		t.Fatal(err)
	}
	rotation := prepared.rotation
	if rotation == nil || prepared.canonical == ([32]byte{}) || owned.consumed.Load() || !cursor.Valid() || rotation.header.Record.Header == nil || rotation.header.Record.Header.SegmentIndex != cursor.segmentIndex+1 || rotation.header.Sequence != cursor.nextSequence || rotation.header.PreviousRecordDigest == nil || *rotation.header.PreviousRecordDigest != *cursor.previousRecordDigest || rotation.headerCheckpoint.Sequence != cursor.lineageIndexNextSequence || rotation.headerCursor.segmentIndex != cursor.segmentIndex+1 || rotation.headerCursor.nextSequence != cursor.nextSequence+1 || prepared.frame.Sequence != cursor.nextSequence+1 || prepared.frame.PreviousRecordDigest == nil || *prepared.frame.PreviousRecordDigest != rotation.header.RecordDigest || !canonicalEqual(prepared.frame.Record, terminal.Record) || prepared.checkpoint.Sequence != cursor.lineageIndexNextSequence+1 || prepared.nextCursor.segmentIndex != cursor.segmentIndex+1 || prepared.nextCursor.nextSequence != cursor.nextSequence+2 || prepared.nextCursor.lineageIndexNextSequence != cursor.lineageIndexNextSequence+2 || rotation.headerRecovery == nil || rotation.headerRecovery.tailDigest != rotation.header.RecordDigest || prepared.recovery == nil || prepared.recovery.tailDigest != prepared.frame.RecordDigest || rotation.segmentRecords != 1 || prepared.segmentRecords != 2 || prepared.journalRecords != journal.state.journalRecords+2 || prepared.checkpointRecords != journal.state.checkpointRecords+2 {
		t.Fatalf("rotation preparation mismatch: prepared=%+v rotation=%+v", prepared, rotation)
	}
	want := prepared.canonical
	prepared.invalidate()
	if prepared.nextCursor.Valid() || rotation.headerCursor.Valid() || preparedGenerationJournalAppendDigest(prepared) != want {
		t.Fatal("rotation invalidation destroyed or retained the wrong prepared authority")
	}
}

func TestPreparedGenerationJournalRotationDigestRejectsEveryMutableFact(t *testing.T) {
	mutations := map[string]func(*preparedGenerationJournalAppend){
		"header bytes":            func(value *preparedGenerationJournalAppend) { value.rotation.headerFramed[0] ^= 1 },
		"header checkpoint bytes": func(value *preparedGenerationJournalAppend) { value.rotation.headerCheckpointFramed[0] ^= 1 },
		"header digest": func(value *preparedGenerationJournalAppend) {
			value.rotation.header.RecordDigest = testDigest("rotation-header")
		},
		"header checkpoint digest": func(value *preparedGenerationJournalAppend) {
			value.rotation.headerCheckpoint.RecordDigest = testDigest("rotation-checkpoint")
		},
		"header cursor":        func(value *preparedGenerationJournalAppend) { value.rotation.headerCursor.nextSequence++ },
		"header recovery":      func(value *preparedGenerationJournalAppend) { value.rotation.headerRecovery.state = RecoveryDivergent },
		"header journal usage": func(value *preparedGenerationJournalAppend) { value.rotation.journalBytes++ },
		"header index usage":   func(value *preparedGenerationJournalAppend) { value.rotation.indexDebitBytes++ },
		"candidate sequence":   func(value *preparedGenerationJournalAppend) { value.frame.Sequence++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			journal, cursor, owned, _, _ := generationJournalPreparationFixtureWithSegments(t, 2)
			journal.state.segmentRecords = evidenceSegmentMaximumRecords
			journal.state.segmentFacts = []evidencefs.GenerationFileFact{{Ordinal: cursor.segmentIndex, Size: journal.state.segmentBytes, ContentDigest: [32]byte{1}, IdentityDigest: [32]byte{2}}}
			prepared, err := journal.prepareAppendLocked(cursor, owned)
			if err != nil || prepared.rotation == nil || prepared.canonical == ([32]byte{}) {
				t.Fatalf("baseline=%+v err=%v", prepared, err)
			}
			want := prepared.canonical
			mutate(prepared)
			if got := preparedGenerationJournalAppendDigest(prepared); got == want {
				t.Fatal("rotation mutation retained prepared append digest")
			}
		})
	}
}

func TestPreparedGenerationJournalAppendDigestRejectsEveryMutableFact(t *testing.T) {
	mutations := map[string]func(*preparedGenerationJournalAppend){
		"journal bytes": func(value *preparedGenerationJournalAppend) { value.framed[0] ^= 1 },
		"checkpoint bytes": func(value *preparedGenerationJournalAppend) {
			value.checkpointFramed[0] ^= 1
		},
		"frame digest": func(value *preparedGenerationJournalAppend) { value.frame.RecordDigest = testDigest("frame") },
		"checkpoint digest": func(value *preparedGenerationJournalAppend) {
			value.checkpoint.RecordDigest = testDigest("checkpoint")
		},
		"cursor sequence": func(value *preparedGenerationJournalAppend) { value.nextCursor.nextSequence++ },
		"cursor checkpoint": func(value *preparedGenerationJournalAppend) {
			value.nextCursor.latestCheckpointRecordDigest = digestPointer(testDigest("latest"))
		},
		"previous recovery": func(value *preparedGenerationJournalAppend) {
			value.previousRecovery.state = RecoveryDivergent
		},
		"candidate recovery": func(value *preparedGenerationJournalAppend) {
			value.recovery.nextPermittedAction = RecoveryReturnFailure
		},
		"candidate body": func(value *preparedGenerationJournalAppend) {
			value.recovery.lastTerminal.value.MigrationID = "999999"
		},
		"journal usage": func(value *preparedGenerationJournalAppend) { value.journalBytes++ },
		"index usage":   func(value *preparedGenerationJournalAppend) { value.indexDebitBytes++ },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			journal, cursor, owned, _, _ := generationJournalPreparationFixture(t)
			prepared, err := journal.prepareAppendLocked(cursor, owned)
			if err != nil || prepared.canonical == ([32]byte{}) {
				t.Fatalf("baseline=%+v err=%v", prepared, err)
			}
			want := prepared.canonical
			mutate(prepared)
			if got := preparedGenerationJournalAppendDigest(prepared); got == want {
				t.Fatal("mutation retained prepared append digest")
			}
		})
	}
}

func TestGenerationJournalSchemaDigestRejectsEveryVerifierFactMutation(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	contextValue := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{33}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	baseline := recoveryFixtureSchema(t, owner, generation, frames[:1], contextValue)
	want := generationJournalSchemaDigest(baseline, generation)
	if want == ([32]byte{}) {
		t.Fatal("schema baseline digest is empty")
	}
	mutations := map[string]func(*verifiedRecoverySchemaWitness){
		"owner": func(value *verifiedRecoverySchemaWitness) { value.owner = &evidenceOwnerToken{} },
		"generation": func(value *verifiedRecoverySchemaWitness) {
			value.generation.journalIdentityDigest = testDigest("other-journal")
		},
		"order": func(value *verifiedRecoverySchemaWitness) { value.orderedMigrations[0] = "000002" },
		"max attempts": func(value *verifiedRecoverySchemaWitness) {
			value.maxAttempts["000001"]++
		},
		"final statement": func(value *verifiedRecoverySchemaWitness) {
			value.finalStatementIndex["000001"]++
		},
		"signed row": func(value *verifiedRecoverySchemaWitness) {
			value.signedExpectedLedgerRows[0].MigrationName = "mutated"
		},
		"signed digest": func(value *verifiedRecoverySchemaWitness) {
			value.signedExpectedLedgerDigest = testDigest("signed")
		},
		"observed digest": func(value *verifiedRecoverySchemaWitness) {
			value.durableObservedLedgerDigest = testDigest("observed")
		},
		"final catalog": func(value *verifiedRecoverySchemaWitness) {
			value.finalCatalogDigest = testDigest("catalog")
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneGenerationJournalSchema(baseline)
			mutate(&value)
			if generationJournalSchemaDigest(value, generation) == want {
				t.Fatal("schema mutation retained digest")
			}
		})
	}
}

func TestRenewGenerationJournalRecoveryRebindsAllNestedBodies(t *testing.T) {
	journal, cursor, owned, _, _ := generationJournalPreparationFixture(t)
	prepared, err := journal.prepareAppendLocked(cursor, owned)
	if err != nil {
		t.Fatal(err)
	}
	prepared.invalidate()
	nextCursor, err := renewGenerationJournalCursor(prepared.nextCursor)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := renewGenerationJournalRecovery(prepared.recovery, nextCursor, journal.generation)
	if err != nil || !validRecoverySnapshotForJournal(recovery, journal.generation, nextCursor) || recovery.lastStatementIntent == nil || recovery.lastIntermediateEvidence == nil || recovery.commitIntent == nil || recovery.lastTerminal == nil {
		t.Fatalf("renewed recovery=%+v err=%v", recovery, err)
	}
	for _, valid := range []*atomic.Bool{recovery.lastStatementIntent.cursor.valid, recovery.lastIntermediateEvidence.cursor.valid, recovery.commitIntent.cursor.valid, recovery.lastTerminal.cursor.valid} {
		if valid != nextCursor.valid {
			t.Fatal("nested recovered body did not share the fresh cursor identity")
		}
	}
	prepared.recovery.lastTerminal.owner = &evidenceOwnerToken{}
	other, err := renewGenerationJournalCursor(prepared.nextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if rebound, err := renewGenerationJournalRecovery(prepared.recovery, other, journal.generation); rebound != nil || err == nil {
		t.Fatalf("mutated recovered body rebound: recovery=%+v err=%v", rebound, err)
	}
}

func TestRegisteredGenerationJournalStateUsesExactRenewedReplayFacts(t *testing.T) {
	replay, identity, _, _, _, _ := registeredGenerationReplayFixture(t, 5)
	ready := &RegisteredGenerationRecoveryReady{
		snapshot: &evidencefs.GenerationSnapshot{}, replay: replay, generation: identity,
		cursor: replay.cursor.clone(), recovery: cloneRecoverySnapshot(replay.recovery), snapshotIdentity: [32]byte{12},
	}
	state, err := registeredGenerationJournalStateFromFacts(ready, ready.snapshotIdentity, replay.indexFact, replay.segmentFacts)
	if err != nil {
		t.Fatal(err)
	}
	last := len(replay.segmentFacts) - 1
	if state.snapshot != ready.snapshot || state.snapshotIdentity != ready.snapshotIdentity || state.indexFact != replay.indexFact || len(state.segmentFacts) != len(replay.segmentFacts) || state.journalRecords != replay.journalRecords || state.journalBytes != replay.journalBytes || state.segmentRecords != replay.segmentRecords[last] || state.segmentBytes != replay.segmentFacts[last].Size || state.checkpointRecords != replay.checkpointRecords || state.indexDebitRecords != replay.indexDebitRecords || state.indexDebitBytes != replay.indexDebitBytes || !sameCursorIdentity(state.cursor, replay.cursor) || !validRecoverySnapshotForJournal(state.recovery, identity, state.cursor) {
		t.Fatalf("registered initial state differs: %+v", state)
	}
	for name, mutate := range map[string]func(*evidencefs.GenerationFileFact, []evidencefs.GenerationFileFact, *[32]byte){
		"index": func(index *evidencefs.GenerationFileFact, _ []evidencefs.GenerationFileFact, _ *[32]byte) {
			index.Size++
		},
		"segment": func(_ *evidencefs.GenerationFileFact, segments []evidencefs.GenerationFileFact, _ *[32]byte) {
			segments[0].IdentityDigest[0]++
		},
		"identity": func(_ *evidencefs.GenerationFileFact, _ []evidencefs.GenerationFileFact, identity *[32]byte) {
			identity[0]++
		},
	} {
		t.Run(name, func(t *testing.T) {
			index := replay.indexFact
			segments := append([]evidencefs.GenerationFileFact(nil), replay.segmentFacts...)
			identityDigest := ready.snapshotIdentity
			mutate(&index, segments, &identityDigest)
			if state, err := registeredGenerationJournalStateFromFacts(ready, identityDigest, index, segments); state != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("mutated state=%+v err=%v", state, err)
			}
		})
	}
}

func TestGenerationEvidenceJournalDigestBindsRegisteredProvenance(t *testing.T) {
	replay, identity, _, _, _, _ := registeredGenerationReplayFixture(t, 5)
	registered := &verifiedAdmissionRegisteredGeneration{descriptor: GenerationDescriptor{identity: identity}, replay: replay, canonical: [32]byte{13}}
	history := &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{14}}}
	ready := &RegisteredGenerationRecoveryReady{history: history, registered: registered, replay: replay, binding: &registeredGenerationRecoveryReadyBinding{canonical: [32]byte{15}}}
	candidateBinding := &verifiedEvidenceRunBinding{owner: identity.owner, canonical: [32]byte{16}}
	runtimeBinding := &verifiedContentReceiptBinding{}
	recoveryBinding := &verifiedDecisionRecoveryReceiptBinding{}
	journal := &generationEvidenceJournal{
		registeredPrior: ready, history: history, candidateBinding: candidateBinding, lease: &evidencefs.GenerationLease{},
		generation: identity, reservation: replay.reservation, schema: cloneGenerationJournalSchema(replay.schema),
		runtimeReceipt:  VerifiedContentReceipt{kind: durableRuntimeContentObject, digest: testDigest("registered-runtime"), sizeBytes: 1, binding: runtimeBinding},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{kind: durableDecisionRecoveryContentObject, digest: testDigest("registered-recovery"), sizeBytes: 1, binding: recoveryBinding},
	}
	journal.self = journal
	want := generationEvidenceJournalDigest(journal)
	if want == ([32]byte{}) || !generationJournalProvenanceShape(journal) {
		t.Fatal("registered journal provenance did not seal")
	}
	copyJournal := cloneRegisteredJournalDigestFixture(journal)
	copyJournal.self = journal
	if generationEvidenceJournalDigest(copyJournal) != ([32]byte{}) {
		t.Fatal("copied registered journal retained digest authority")
	}
	for name, mutate := range map[string]func(*generationEvidenceJournal){
		"source":      func(v *generationEvidenceJournal) { v.registeredPrior.binding.canonical[0]++ },
		"history":     func(v *generationEvidenceJournal) { v.history.binding.canonical[0]++ },
		"candidate":   func(v *generationEvidenceJournal) { v.candidateBinding.canonical[0]++ },
		"generation":  func(v *generationEvidenceJournal) { v.generation.journalIdentityDigest = testDigest("other-journal") },
		"reservation": func(v *generationEvidenceJournal) { v.reservation.ReservedRecords++ },
		"schema":      func(v *generationEvidenceJournal) { v.schema.finalCatalogDigest = testDigest("other-catalog") },
		"runtime":     func(v *generationEvidenceJournal) { v.runtimeReceipt.digest = testDigest("other-runtime") },
		"recovery":    func(v *generationEvidenceJournal) { v.recoveryReceipt.digest = testDigest("other-recovery") },
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneRegisteredJournalDigestFixture(journal)
			historyCopy, readyCopy, readyBinding, candidateCopy := *history, *ready, *ready.binding, *candidateBinding
			historyBinding := *history.binding
			historyCopy.binding = &historyBinding
			readyCopy.history, readyCopy.binding = &historyCopy, &readyBinding
			value.registeredPrior, value.history, value.candidateBinding = &readyCopy, &historyCopy, &candidateCopy
			value.schema = cloneGenerationJournalSchema(journal.schema)
			mutate(value)
			if generationEvidenceJournalDigest(value) == want {
				t.Fatal("registered journal mutation retained digest")
			}
		})
	}
	invalid := cloneRegisteredJournalDigestFixture(journal)
	invalid.prior = &GenerationRecoveryReady{}
	invalid.replay = &GenerationReplayReady{}
	invalid.plan = &VerifiedAdmissionPlan{}
	if generationJournalProvenanceShape(invalid) || generationEvidenceJournalDigest(invalid) != ([32]byte{}) {
		t.Fatal("mixed brand-new and registered provenance was accepted")
	}
}

func TestGenerationEvidenceJournalDigestBindsSuccessorProvenance(t *testing.T) {
	replayFacts, identity, _, _, _, _ := registeredGenerationReplayFixture(t, 5)
	history := &VerifiedAdmissionHistory{reservation: replayFacts.reservation, binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{21}}}
	state := &successorAdmissionState{history: history, binding: &successorAdmissionStateBinding{canonical: [32]byte{22}}}
	replay := &SuccessorGenerationReplayReady{state: state, binding: &successorGenerationReplayBinding{canonical: [32]byte{23}}}
	ready := &SuccessorGenerationRecoveryReady{state: state, prior: replay, binding: &successorGenerationRecoveryBinding{canonical: [32]byte{24}}}
	candidateBinding := &verifiedEvidenceRunBinding{owner: identity.owner, canonical: [32]byte{25}}
	journal := &generationEvidenceJournal{
		successorPrior: ready, successorReplay: replay, history: history, candidateBinding: candidateBinding,
		lease: &evidencefs.GenerationLease{}, generation: identity, reservation: replayFacts.reservation,
		schema: cloneGenerationJournalSchema(replayFacts.schema),
		runtimeReceipt: VerifiedContentReceipt{
			kind: durableRuntimeContentObject, digest: testDigest("successor-journal-runtime"), sizeBytes: 1,
			binding: &verifiedContentReceiptBinding{},
		},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{
			kind: durableDecisionRecoveryContentObject, digest: testDigest("successor-journal-recovery"), sizeBytes: 1,
			binding: &verifiedDecisionRecoveryReceiptBinding{},
		},
	}
	journal.self = journal
	want := generationEvidenceJournalDigest(journal)
	if want == ([32]byte{}) || !generationJournalProvenanceShape(journal) {
		t.Fatal("successor journal provenance did not seal")
	}
	for name, mutate := range map[string]func(*generationEvidenceJournal){
		"source":    func(value *generationEvidenceJournal) { value.successorPrior.binding.canonical[0]++ },
		"replay":    func(value *generationEvidenceJournal) { value.successorReplay.binding.canonical[0]++ },
		"history":   func(value *generationEvidenceJournal) { value.history.binding.canonical[0]++ },
		"candidate": func(value *generationEvidenceJournal) { value.candidateBinding.canonical[0]++ },
		"generation": func(value *generationEvidenceJournal) {
			value.generation.journalIdentityDigest = testDigest("other-journal")
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneRegisteredJournalDigestFixture(journal)
			historyCopy, historyBinding := *history, *history.binding
			historyCopy.binding = &historyBinding
			stateCopy, stateBinding := *state, *state.binding
			stateCopy.history, stateCopy.binding = &historyCopy, &stateBinding
			replayCopy, replayBinding := *replay, *replay.binding
			replayCopy.state, replayCopy.binding = &stateCopy, &replayBinding
			readyCopy, readyBinding := *ready, *ready.binding
			readyCopy.state, readyCopy.prior, readyCopy.binding = &stateCopy, &replayCopy, &readyBinding
			candidateCopy := *candidateBinding
			value.successorPrior, value.successorReplay = &readyCopy, &replayCopy
			value.history, value.candidateBinding = &historyCopy, &candidateCopy
			mutate(value)
			if generationEvidenceJournalDigest(value) == want {
				t.Fatal("successor journal mutation retained digest")
			}
		})
	}
	mixed := cloneRegisteredJournalDigestFixture(journal)
	mixed.registeredPrior = &RegisteredGenerationRecoveryReady{binding: &registeredGenerationRecoveryReadyBinding{}}
	if generationJournalProvenanceShape(mixed) || generationEvidenceJournalDigest(mixed) != ([32]byte{}) {
		t.Fatal("mixed successor and registered provenance was accepted")
	}
}

func TestGenerationEvidenceJournalDigestBindsHistoricalSuccessorProvenance(t *testing.T) {
	replayFacts, identity, _, _, descriptor, _ := registeredGenerationReplayFixture(t, 1)
	planned := &verifiedAdmissionRegisteredGeneration{descriptor: descriptor, canonical: [32]byte{31}}
	replay := &HistoricalSuccessorGenerationReplayReady{binding: &historicalSuccessorGenerationReplayBinding{canonical: [32]byte{32}}}
	ready := &HistoricalSuccessorGenerationRecoveryReady{
		prior: replay, planned: planned, binding: &historicalSuccessorGenerationRecoveryBinding{canonical: [32]byte{33}}, consumed: &atomic.Bool{},
	}
	ready.consumed.Store(true)
	candidateBinding := &verifiedEvidenceRunBinding{owner: identity.owner, canonical: [32]byte{34}}
	journal := &generationEvidenceJournal{
		historicalSuccessorPrior: ready, historicalSuccessorReplay: replay, candidateBinding: candidateBinding,
		lease: &evidencefs.GenerationLease{}, generation: identity, reservation: replayFacts.reservation,
		schema: cloneGenerationJournalSchema(replayFacts.schema),
		runtimeReceipt: VerifiedContentReceipt{
			kind: durableRuntimeContentObject, digest: testDigest("historical-successor-journal-runtime"), sizeBytes: 1,
			binding: &verifiedContentReceiptBinding{},
		},
		recoveryReceipt: VerifiedDecisionRecoveryReceipt{
			kind: durableDecisionRecoveryContentObject, digest: testDigest("historical-successor-journal-recovery"), sizeBytes: 1,
			binding: &verifiedDecisionRecoveryReceiptBinding{},
		},
	}
	journal.self = journal
	want := generationEvidenceJournalDigest(journal)
	if want == ([32]byte{}) || !generationJournalProvenanceShape(journal) || !generationJournalHistoryShape(journal) {
		t.Fatal("historical successor journal provenance did not seal")
	}
	copyJournal := cloneRegisteredJournalDigestFixture(journal)
	copyJournal.self = journal
	if generationEvidenceJournalDigest(copyJournal) != ([32]byte{}) {
		t.Fatal("copied historical successor journal retained digest authority")
	}
	for name, mutate := range map[string]func(*generationEvidenceJournal){
		"source": func(value *generationEvidenceJournal) { value.historicalSuccessorPrior.binding.canonical[0]++ },
		"replay": func(value *generationEvidenceJournal) { value.historicalSuccessorReplay.binding.canonical[0]++ },
		"planned": func(value *generationEvidenceJournal) {
			value.historicalSuccessorPrior.planned.canonical[0]++
		},
		"history": func(value *generationEvidenceJournal) {
			value.history = &VerifiedAdmissionHistory{binding: &verifiedAdmissionHistoryBinding{canonical: [32]byte{1}}}
		},
		"candidate": func(value *generationEvidenceJournal) { value.candidateBinding.canonical[0]++ },
		"generation": func(value *generationEvidenceJournal) {
			value.generation.journalIdentityDigest = testDigest("other-historical-journal")
		},
		"reservation": func(value *generationEvidenceJournal) { value.reservation.ReservedRecords++ },
		"schema": func(value *generationEvidenceJournal) {
			value.schema.finalCatalogDigest = testDigest("other-historical-catalog")
		},
		"runtime": func(value *generationEvidenceJournal) {
			value.runtimeReceipt.digest = testDigest("other-historical-runtime")
		},
		"recovery": func(value *generationEvidenceJournal) {
			value.recoveryReceipt.digest = testDigest("other-historical-recovery")
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := cloneRegisteredJournalDigestFixture(journal)
			plannedCopy := *planned
			replayCopy, replayBinding := *replay, *replay.binding
			replayCopy.binding = &replayBinding
			readyCopy, readyBinding := *ready, *ready.binding
			readyCopy.prior, readyCopy.planned, readyCopy.binding = &replayCopy, &plannedCopy, &readyBinding
			candidateCopy := *candidateBinding
			value.historicalSuccessorPrior, value.historicalSuccessorReplay, value.candidateBinding = &readyCopy, &replayCopy, &candidateCopy
			mutate(value)
			if generationEvidenceJournalDigest(value) == want {
				t.Fatal("historical successor journal mutation retained digest")
			}
		})
	}
	mixed := cloneRegisteredJournalDigestFixture(journal)
	mixed.successorPrior = &SuccessorGenerationRecoveryReady{binding: &successorGenerationRecoveryBinding{}}
	mixed.successorReplay = &SuccessorGenerationReplayReady{binding: &successorGenerationReplayBinding{}}
	if generationJournalProvenanceShape(mixed) || generationEvidenceJournalDigest(mixed) != ([32]byte{}) {
		t.Fatal("mixed historical and live successor provenance was accepted")
	}
}

func TestHistoricalSuccessorGenerationReservationUsesRegisteredLineageArithmetic(t *testing.T) {
	bundle := quotaAdmissionBundleForTest(t)
	_, _, _, _, descriptor, _ := registeredGenerationReplayFixture(t, 1)
	facts, err := bundle.quotaFactsForAdmission()
	if err != nil {
		t.Fatal(err)
	}
	want, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{lineageQuotaProfile: facts.lineageQuotaProfile, maxAttempts: facts.maxAttempts, statementCounts: append([]uint64(nil), facts.statementCounts...)}, false)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.header.ReservedRecords = want.ReservedRecords
	descriptor.header.ReservedBytes = want.ReservedBytes
	descriptor.header.ReservedSegments = want.ReservedSegments
	descriptor.header.LimitsProfile = facts.lineageQuotaProfile
	planned := &verifiedAdmissionRegisteredGeneration{descriptor: descriptor, bundle: bundle}
	got, err := historicalSuccessorGenerationReservation(planned)
	if err != nil || got != want {
		t.Fatalf("reservation=%+v want=%+v err=%v", got, want, err)
	}
	for name, mutate := range map[string]func(*JournalHeader){
		"records":  func(header *JournalHeader) { header.ReservedRecords++ },
		"bytes":    func(header *JournalHeader) { header.ReservedBytes++ },
		"segments": func(header *JournalHeader) { header.ReservedSegments++ },
		"profile":  func(header *JournalHeader) { header.LimitsProfile = EvidenceLimitsProfile },
	} {
		t.Run(name, func(t *testing.T) {
			copyPlanned := *planned
			copyPlanned.descriptor = descriptor
			mutate(&copyPlanned.descriptor.header)
			if reservation, err := historicalSuccessorGenerationReservation(&copyPlanned); reservation != (evidenceQuotaReservation{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("mutated reservation=%+v err=%v", reservation, err)
			}
		})
	}
	if reservation, err := historicalSuccessorGenerationReservation(&verifiedAdmissionRegisteredGeneration{}); reservation != (evidenceQuotaReservation{}) || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal reservation=%+v err=%v", reservation, err)
	}
}

func TestHistoricalSuccessorInitialIndexDebitIsReservedThenActivatedOnly(t *testing.T) {
	ready := &HistoricalSuccessorGenerationReadyPermit{reservedFrameBytes: []byte{1, 2, 3}, activatedBytes: []byte{4, 5}}
	records, bytes, err := historicalSuccessorGenerationInitialIndexDebit(ready)
	if err != nil || records != 2 || bytes != 5 {
		t.Fatalf("records=%d bytes=%d err=%v", records, bytes, err)
	}
	for name, value := range map[string]*HistoricalSuccessorGenerationReadyPermit{
		"nil":               nil,
		"missing reserved":  {activatedBytes: []byte{1}},
		"missing activated": {reservedFrameBytes: []byte{1}},
	} {
		t.Run(name, func(t *testing.T) {
			if records, bytes, err := historicalSuccessorGenerationInitialIndexDebit(value); records != 0 || bytes != 0 || !IsCode(err, CodeEvidenceRecoveryRequired) {
				t.Fatalf("records=%d bytes=%d err=%v", records, bytes, err)
			}
		})
	}
}

func TestGenerationJournalSourceCountIncludesHistoricalSuccessorPair(t *testing.T) {
	replay := &GenerationReplayReady{}
	registered := &RegisteredGenerationRecoveryReady{}
	successor := &SuccessorGenerationRecoveryReady{}
	successorReplay := &SuccessorGenerationReplayReady{}
	historical := &HistoricalSuccessorGenerationRecoveryReady{}
	historicalReplay := &HistoricalSuccessorGenerationReplayReady{}
	for name, test := range map[string]struct {
		record generationEvidenceJournalRegistryRecord
		want   int
	}{
		"brand new":                 {generationEvidenceJournalRegistryRecord{replay: replay}, 1},
		"registered":                {generationEvidenceJournalRegistryRecord{registeredPrior: registered}, 1},
		"successor":                 {generationEvidenceJournalRegistryRecord{successorPrior: successor, successorReplay: successorReplay}, 1},
		"historical successor":      {generationEvidenceJournalRegistryRecord{historicalSuccessorPrior: historical, historicalSuccessorReplay: historicalReplay}, 1},
		"missing successor replay":  {generationEvidenceJournalRegistryRecord{successorPrior: successor}, 2},
		"missing historical replay": {generationEvidenceJournalRegistryRecord{historicalSuccessorPrior: historical}, 2},
		"mixed sources":             {generationEvidenceJournalRegistryRecord{registeredPrior: registered, historicalSuccessorPrior: historical, historicalSuccessorReplay: historicalReplay}, 2},
		"stray replay":              {generationEvidenceJournalRegistryRecord{replay: replay, historicalSuccessorReplay: historicalReplay}, 2},
		"empty":                     {generationEvidenceJournalRegistryRecord{}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			if got := generationJournalRecordSourceCount(test.record); got != test.want {
				t.Fatalf("source count=%d want=%d", got, test.want)
			}
		})
	}
}

func TestGenerationJournalLosingBinderDoesNotRevokeWinningCursor(t *testing.T) {
	consumed := &atomic.Bool{}
	valid := &atomic.Bool{}
	valid.Store(true)
	if err := consumeGenerationJournalRecovery(consumed, "journal-cas-test", "consumed"); err != nil {
		t.Fatal(err)
	}
	if err := consumeGenerationJournalRecovery(consumed, "journal-cas-test", "consumed"); !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("losing consume=%v", err)
	}
	if !valid.Load() {
		t.Fatal("losing binder revoked the winning cursor validity cell")
	}
}

func cloneRegisteredJournalDigestFixture(source *generationEvidenceJournal) *generationEvidenceJournal {
	result := &generationEvidenceJournal{
		prior: source.prior, registeredPrior: source.registeredPrior, successorPrior: source.successorPrior, historicalSuccessorPrior: source.historicalSuccessorPrior,
		replay: source.replay, successorReplay: source.successorReplay, historicalSuccessorReplay: source.historicalSuccessorReplay, plan: source.plan,
		history: source.history, candidateBinding: source.candidateBinding,
		runtimeReceipt: source.runtimeReceipt, recoveryReceipt: source.recoveryReceipt,
		lease: source.lease, generation: source.generation, reservation: source.reservation,
		schema: cloneGenerationJournalSchema(source.schema), binding: source.binding,
	}
	result.self = result
	return result
}

func TestGenerationEvidenceJournalRejectsLiteralAuthorityAndConsumerSpread(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("journal-literal"))
	if journal, err := (&GenerationRecoveryReady{}).BindJournal(context.Background(), candidate); journal != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovery entered journal binder: journal=%T err=%v", journal, err)
	}
	if journal, err := (&RegisteredGenerationRecoveryReady{}).BindJournal(context.Background(), candidate); journal != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal registered recovery entered journal binder: journal=%T err=%v", journal, err)
	}
	if journal, err := (&SuccessorGenerationRecoveryReady{}).BindJournal(context.Background(), candidate); journal != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal successor recovery entered journal binder: journal=%T err=%v", journal, err)
	}
	if journal, err := (&HistoricalSuccessorGenerationRecoveryReady{}).BindJournal(context.Background(), candidate); journal != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal historical successor recovery entered journal binder: journal=%T err=%v", journal, err)
	}
	literal := &generationEvidenceJournal{}
	if _, _, err := literal.Replay(context.Background()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal replay=%v", err)
	}
	if _, err := literal.AppendDurable(context.Background(), JournalCursor{}, &OwnedEvidenceRecord{}); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal append=%v", err)
	}
	if err := literal.Close(context.Background()); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal close=%v", err)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "evidence_generation_recovery.go" || name == "evidence_generation_journal.go" || name == "evidence_generation_journal_rotation.go" || name == "evidence_session.go" || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && (identifier.Name == "generationEvidenceJournal" || identifier.Name == "generationEvidenceJournalState" || identifier.Name == "generationJournalUnknownAppend" || identifier.Name == "preparedGenerationJournalAppend") {
				t.Fatalf("sealed journal internals spread into %s", name)
			}
			return true
		})
	}
}

func TestGenerationJournalObservedLedgerIsExactCommittedPrefix(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	contextValue := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{31}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	schema := recoveryFixtureSchema(t, owner, generation, frames[:len(frames)-1], contextValue)
	if len(schema.durableObservedLedgerPrefix) != 0 {
		t.Fatal("pre-terminal fixture unexpectedly has durable ledger rows")
	}
	if err := refreshGenerationJournalObservedLedger(&schema, frames); err != nil || len(schema.durableObservedLedgerPrefix) != 1 || !canonicalEqual(schema.durableObservedLedgerPrefix[0], schema.signedExpectedLedgerRows[0]) {
		t.Fatalf("committed prefix=%+v err=%v", schema.durableObservedLedgerPrefix, err)
	}
	schema.orderedMigrations = append(schema.orderedMigrations, "000002")
	row := cloneProjectionValue(schema.signedExpectedLedgerRows[0])
	row.MigrationID = "000002"
	predecessor := "000001"
	row.PredecessorID = &predecessor
	schema.signedExpectedLedgerRows = append(schema.signedExpectedLedgerRows, row)
	bad := cloneProjectionValue(frames)
	bad[len(bad)-1].Record.AttemptTerminal.MigrationID = "000002"
	if err := refreshGenerationJournalObservedLedger(&schema, bad); err == nil {
		t.Fatal("non-prefix committed migration was accepted")
	}
}

func generationJournalPreparationFixture(t *testing.T) (*generationEvidenceJournal, JournalCursor, *OwnedEvidenceRecord, EvidenceFrame, []EvidenceFrame) {
	return generationJournalPreparationFixtureWithSegments(t, 1)
}

func generationJournalPreparationFixtureWithSegments(t *testing.T, reservedSegments uint32) (*generationEvidenceJournal, JournalCursor, *OwnedEvidenceRecord, EvidenceFrame, []EvidenceFrame) {
	t.Helper()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	if reservedSegments != frames[0].Record.Header.ReservedSegments {
		header := cloneProjectionValue(*frames[0].Record.Header)
		header.ReservedSegments = reservedSegments
		frames[0].Record.Header = &header
		redigestEvidenceFrames(t, frames)
	}
	contextValue := fixtureObjectValue(t, document["validation_context"], "validation context")
	chain := buildEvidenceWitness(t, frames, contextValue)
	owner := &evidenceOwnerToken{nonce: [16]byte{30}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	terminal := frames[len(frames)-1]
	prefix := cloneProjectionValue(frames[:len(frames)-1])
	cursor := runtimeCursorAt(generation, prefix[len(prefix)-1].RecordDigest, terminal.Sequence)
	cursor.lineageIndexNextSequence = 9
	cursor.lineageIndexPreviousRecordDigest = testDigest("generation-index-tail")
	latest := testDigest("generation-checkpoint-tail")
	cursor.latestCheckpointRecordDigest = &latest
	witness := ownedAttemptTerminalWitness{ownedAppendContext{generation, cursor, prefix, chain}, terminal.Record.AttemptTerminal.TerminalDigest, nil, 3}
	owned, err := bindOwnedEvidenceRecord(terminal.Record, witness)
	if err != nil {
		t.Fatal(err)
	}
	schema := recoveryFixtureSchema(t, owner, generation, prefix, contextValue)
	recovery, err := buildRecoverySnapshot(prefix, cursor, generation, recoveredContinuation{}, schema)
	if err != nil {
		t.Fatal(err)
	}
	journalBytes := uint64(0)
	for _, frame := range prefix {
		framed, err := EncodeCanonicalEvidenceFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		journalBytes += uint64(len(framed))
	}
	state := &generationEvidenceJournalState{
		cursor: cursor, recovery: recovery, journalRecords: uint64(len(prefix)), journalBytes: journalBytes,
		segmentRecords: uint64(len(prefix)), segmentBytes: journalBytes, checkpointRecords: uint64(len(prefix) - 1),
		indexDebitRecords: 9, indexDebitBytes: 1024,
	}
	journal := &generationEvidenceJournal{
		generation: generation,
		reservation: evidenceQuotaReservation{
			ReservedRecords: 4096, ReservedJournalBytes: evidenceSegmentMaximumBytes, ReservedSegments: reservedSegments,
			ReservedCheckpointRecords: 4095, ReservedIndexRecords: 16384, ReservedIndexBytes: 16 << 20,
			ReservedBytes: evidenceSegmentMaximumBytes + 16<<20,
		},
		schema: schema, state: state,
	}
	journal.self = journal
	return journal, cursor, owned, terminal, prefix
}

func TestRenewGenerationJournalCursorMintsFreshOneShotIdentity(t *testing.T) {
	valid := &atomic.Bool{}
	valid.Store(true)
	owner := &evidenceOwnerToken{nonce: [16]byte{32}}
	generation := generationIdentity{owner, testDigest("lineage"), testDigest("journal"), testDigest("decision"), testDigest("schema")}
	previous := testDigest("previous")
	source := JournalCursor{owner: owner, generation: generation, nextSequence: 2, previousRecordDigest: &previous, lineageIndexNextSequence: 3, lineageIndexPreviousRecordDigest: testDigest("index"), valid: valid}
	next, err := renewGenerationJournalCursor(source)
	if err != nil || !next.Valid() || next.valid == source.valid || sameCursorIdentity(next, source) {
		t.Fatalf("renewed cursor=%+v err=%v", next, err)
	}
	source.valid.Store(false)
	if !next.Valid() {
		t.Fatal("renewed cursor shared invalidation identity")
	}
}
