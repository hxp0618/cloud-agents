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

func TestGenerationEvidenceJournalRejectsLiteralAuthorityAndConsumerSpread(t *testing.T) {
	candidate := quotaCandidateForBundle(t, quotaAdmissionBundleForTest(t), []byte("journal-literal"))
	if journal, err := (&GenerationRecoveryReady{}).BindJournal(context.Background(), candidate); journal != nil || !IsCode(err, CodeEvidenceRecoveryRequired) {
		t.Fatalf("literal recovery entered journal binder: journal=%T err=%v", journal, err)
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
