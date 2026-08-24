package migration

import (
	"sync/atomic"
	"testing"
)

func TestRecoverySnapshotClosedStatesAndImmutableBodies(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{1}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	assertRecoveryState(t, frames[:1], context, generation, nil, RecoveryBrandNew, RecoveryBeginFirstAttempt)
	assertRecoveryState(t, frames[:2], context, generation, nil, RecoveryDanglingStatementIntent, RecoveryAppendAbortedRetryable)
	assertRecoveryState(t, frames[:3], context, generation, nil, RecoveryDanglingIntermediate, RecoveryAppendAbortedRetryable)
	assertRecoveryState(t, frames[:4], context, generation, nil, RecoveryDanglingCommitIntent, RecoveryReconcileCommit)
	snapshot := assertRecoveryState(t, frames, context, generation, nil, RecoveryCompleted, RecoveryReturnSuccess)
	body := snapshot.LastTerminal()
	if body == nil {
		t.Fatal("missing recovered terminal")
	}
	value := body.Value()
	value.MigrationID = "999999"
	if snapshot.LastTerminal().Value().MigrationID != "000001" {
		t.Fatal("recovered body alias escaped")
	}

	ambiguous := fixtureObject(t, migrationFixturePath(t, "golden/evidence-ambiguous-chain-v1.json"))
	ambiguousFrames := decodeEvidenceFrames(t, ambiguous["frames"])
	witness := buildEvidenceWitness(t, ambiguousFrames, context)
	terminal := terminalFrame(t, ambiguousFrames)
	witness.ambiguousBoundaries[terminal.Record.AttemptTerminal.TerminalDigest] = buildAmbiguousBoundaryWitness(t, ambiguous["owned_ambiguous_boundary_oracle"])
	assertRecoveryStateWithWitness(t, ambiguousFrames[:5], generation, witness, RecoveryAmbiguousUnresolved, RecoveryReconcileCommit)
	assertRecoveryStateWithWitness(t, ambiguousFrames, generation, witness, RecoveryTerminal, RecoveryBeginNextAttempt)
}

func TestRecoverySnapshotInheritedAndCrossGenerationInjection(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{2}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	tail := frames[0].RecordDigest
	cursor := recoveryFixtureCursor(generation, frames[:1])
	previous := DigestBytes([]byte("source-terminal"))
	continuation := LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2, PreviousAttemptTerminalDigest: digestPointer(previous), SourceJournalIdentityDigest: DigestBytes([]byte("source-journal")), SourceCheckpointRecordDigest: DigestBytes([]byte("source-checkpoint")), SourceTerminalDigest: previous}
	owned := recoveredValue(generation, cursor, tail, DigestBytes([]byte("continuation-record")), continuation)
	schema := recoveryFixtureSchema(t, owner, generation, frames[:1], context)
	snapshot, err := buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{owned: owned}, schema)
	if err != nil || snapshot.State() != RecoveryBrandNewInherited || snapshot.NextAction() != RecoveryBeginNextAttempt {
		t.Fatalf("inherited snapshot: %v %#v", err, snapshot)
	}

	otherOwner := &evidenceOwnerToken{nonce: [16]byte{3}}
	otherGeneration := generation
	otherGeneration.owner = otherOwner
	other := *owned
	other.owner, other.generation.owner = otherOwner, otherOwner
	if _, err := buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{owned: &other}, schema); err == nil {
		t.Fatal("accepted cross-generation continuation")
	}

	snapshot, err = buildRecoverySnapshot(frames[:1], cursor, generation, recoveredContinuation{inheritedWithoutContext: true}, schema)
	if err != nil || snapshot.State() != RecoveryBrandNewInherited || snapshot.NextAction() != RecoveryBeginFirstAttempt {
		t.Fatalf("header-only inherited snapshot: %v", err)
	}
}

func TestRecoveryTerminalClosedActionsFinalNonFinalAndDivergent(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{4}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	schema := recoveryFixtureSchema(t, owner, generation, frames, context)
	if _, err := buildRecoverySnapshot(frames, recoveryFixtureCursor(generation, frames), generation, recoveredContinuation{}, schema); err != nil {
		t.Fatal(err)
	}
	skipped := schema
	skipped.orderedMigrations = []string{"000002", "000001"}
	skipped.maxAttempts["000002"] = 3
	if _, err := buildRecoverySnapshot(frames, recoveryFixtureCursor(generation, frames), generation, recoveredContinuation{}, skipped); err == nil {
		t.Fatal("accepted skipped/reordered ledger prefix")
	}
	badCatalog := schema
	badCatalog.finalCatalogDigest = DigestBytes([]byte("wrong-final-catalog"))
	snapshot, err := buildRecoverySnapshot(frames, recoveryFixtureCursor(generation, frames), generation, recoveredContinuation{}, badCatalog)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State() == RecoveryCompleted || snapshot.NextAction() == RecoveryReturnSuccess {
		t.Fatal("completed without exact final catalog")
	}
	badLedger := schema
	badLedger.durableObservedLedgerPrefix = cloneProjectionValue(schema.durableObservedLedgerPrefix)
	badLedger.durableObservedLedgerPrefix[0].MigrationID = "000002"
	badLedger.durableObservedLedgerDigest, _ = LedgerPrefixDigest(badLedger.durableObservedLedgerPrefix)
	if _, err := buildRecoverySnapshot(frames, recoveryFixtureCursor(generation, frames), generation, recoveredContinuation{}, badLedger); err == nil {
		t.Fatal("accepted ledger row/commit identity mismatch")
	}
}

func TestRecoverySchemaSeparatesSignedRowsFromObservedPrefixForDanglingNextCommit(t *testing.T) {
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	owner := &evidenceOwnerToken{nonce: [16]byte{12}}
	generation := recoveryFixtureGeneration(owner, *frames[0].Record.Header)
	base := recoveryFixtureSchema(t, owner, generation, frames, context)
	row1 := cloneProjectionValue(base.signedExpectedLedgerRows[0])
	row2 := cloneProjectionValue(row1)
	row2.MigrationID = "000002"
	predecessor := "000001"
	row2.PredecessorID = &predecessor
	base.orderedMigrations = []string{"000001", "000002"}
	base.maxAttempts["000002"] = 3
	base.signedExpectedLedgerRows = []CommitIntentLedgerRow{row1, row2}
	base.signedExpectedLedgerDigest, _ = LedgerPrefixDigest(base.signedExpectedLedgerRows)
	base.durableObservedLedgerPrefix = []CommitIntentLedgerRow{row1}
	base.durableObservedLedgerDigest, _ = LedgerPrefixDigest(base.durableObservedLedgerPrefix)
	commit := cloneProjectionValue(*frames[3].Record.CommitIntent)
	commit.MigrationID = "000002"
	commit.ExpectedLedgerLength = 2
	commit.ExpectedLedgerHead = "000002"
	commit.LedgerRow = row2
	commitFrame := EvidenceFrame{Record: EvidenceRecord{CommitIntent: &commit}}
	if err := validateRecoverySchemaWitness(base, []EvidenceFrame{commitFrame}); err != nil {
		t.Fatalf("valid dangling next-entry commit with short observed prefix: %v", err)
	}
	missing := base
	missing.signedExpectedLedgerRows = missing.signedExpectedLedgerRows[:1]
	missing.signedExpectedLedgerDigest, _ = LedgerPrefixDigest(missing.signedExpectedLedgerRows)
	if err := validateRecoverySchemaWitness(missing, []EvidenceFrame{commitFrame}); err == nil {
		t.Fatal("accepted missing signed row")
	}
	reordered := base
	reordered.signedExpectedLedgerRows = []CommitIntentLedgerRow{row2, row1}
	reordered.signedExpectedLedgerDigest, _ = LedgerPrefixDigest(reordered.signedExpectedLedgerRows)
	if err := validateRecoverySchemaWitness(reordered, []EvidenceFrame{commitFrame}); err == nil {
		t.Fatal("accepted reordered signed rows")
	}
	drift := base
	drift.signedExpectedLedgerRows = cloneProjectionValue(base.signedExpectedLedgerRows)
	drift.signedExpectedLedgerRows[1].MigrationName = "drift"
	drift.signedExpectedLedgerDigest, _ = LedgerPrefixDigest(drift.signedExpectedLedgerRows)
	if err := validateRecoverySchemaWitness(drift, []EvidenceFrame{commitFrame}); err == nil {
		t.Fatal("accepted commit row drift")
	}
	containsCurrent := base
	containsCurrent.durableObservedLedgerPrefix = []CommitIntentLedgerRow{row1, row2}
	containsCurrent.durableObservedLedgerDigest, _ = LedgerPrefixDigest(containsCurrent.durableObservedLedgerPrefix)
	if err := validateRecoverySchemaWitness(containsCurrent, []EvidenceFrame{commitFrame}); err == nil {
		t.Fatal("accepted dangling current row as already durable")
	}
	overflowCommit := commit
	overflowCommit.MigrationID = "000003"
	overflowCommit.ExpectedLedgerLength = 3
	overflowCommit.ExpectedLedgerHead = "000003"
	if err := validateRecoverySchemaWitness(base, []EvidenceFrame{{Record: EvidenceRecord{CommitIntent: &overflowCommit}}}); err == nil {
		t.Fatal("accepted out-of-range commit identity")
	}
}

func assertRecoveryState(t *testing.T, frames []EvidenceFrame, context map[string]JSONValue, generation generationIdentity, continuation *recoveredContinuation, wantState RecoveryState, wantAction RecoveryAction) *RecoverySnapshot {
	t.Helper()
	schema := recoveryFixtureSchema(t, generation.owner, generation, frames, context)
	return assertRecoveryStateWithSchema(t, frames, generation, continuation, schema, wantState, wantAction)
}
func assertRecoveryStateWithWitness(t *testing.T, frames []EvidenceFrame, generation generationIdentity, witness verifiedEvidenceChainWitness, wantState RecoveryState, wantAction RecoveryAction) *RecoverySnapshot {
	t.Helper()
	context := fixtureObjectValue(t, fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))["validation_context"], "validation context")
	schema := recoveryFixtureSchema(t, generation.owner, generation, frames, context)
	schema.chainWitness = witness
	return assertRecoveryStateWithSchema(t, frames, generation, nil, schema, wantState, wantAction)
}
func assertRecoveryStateWithSchema(t *testing.T, frames []EvidenceFrame, generation generationIdentity, continuation *recoveredContinuation, schema verifiedRecoverySchemaWitness, wantState RecoveryState, wantAction RecoveryAction) *RecoverySnapshot {
	t.Helper()
	cursor := recoveryFixtureCursor(generation, frames)
	c := recoveredContinuation{}
	if continuation != nil {
		c = *continuation
	}
	snapshot, err := buildRecoverySnapshot(frames, cursor, generation, c, schema)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.State() != wantState || snapshot.NextAction() != wantAction {
		t.Fatalf("got %s/%s want %s/%s", snapshot.State(), snapshot.NextAction(), wantState, wantAction)
	}
	return snapshot
}
func recoveryFixtureGeneration(owner *evidenceOwnerToken, header JournalHeader) generationIdentity {
	return generationIdentity{owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
}
func recoveryFixtureCursor(generation generationIdentity, frames []EvidenceFrame) JournalCursor {
	valid := &atomic.Bool{}
	valid.Store(true)
	tail := frames[len(frames)-1].RecordDigest
	return JournalCursor{owner: generation.owner, generation: generation, segmentIndex: 0, nextSequence: frames[len(frames)-1].Sequence + 1, previousRecordDigest: digestPointer(tail), lineageIndexNextSequence: 1, lineageIndexPreviousRecordDigest: DigestBytes([]byte("lineage-tail")), valid: valid}
}
func recoveryFixtureSchema(t *testing.T, owner *evidenceOwnerToken, generation generationIdentity, frames []EvidenceFrame, context map[string]JSONValue) verifiedRecoverySchemaWitness {
	t.Helper()
	witness := buildEvidenceWitness(t, frames, context)
	full := decodeEvidenceFrames(t, fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))["frames"])
	row := cloneProjectionValue(*full[3].Record.CommitIntent).LedgerRow
	signed := []CommitIntentLedgerRow{row}
	digest, err := LedgerPrefixDigest(signed)
	if err != nil {
		t.Fatal(err)
	}
	observed := []CommitIntentLedgerRow(nil)
	for _, frame := range frames {
		if terminal := frame.Record.AttemptTerminal; terminal != nil && stringIn(terminal.Outcome, "committed", "ambiguous_reconciled_committed") {
			observed = []CommitIntentLedgerRow{row}
		}
		if resolution := frame.Record.AmbiguousResolution; resolution != nil && resolution.Outcome == "resolved_committed" {
			observed = []CommitIntentLedgerRow{row}
		}
	}
	observedDigest, err := LedgerPrefixDigest(observed)
	if err != nil {
		t.Fatal(err)
	}
	return verifiedRecoverySchemaWitness{owner: owner, generation: generation, finalStatementIndex: witness.finalStatementIndex, maxAttempts: witness.maxAttempts, orderedMigrations: []string{"000001"}, signedExpectedLedgerRows: signed, signedExpectedLedgerDigest: digest, durableObservedLedgerPrefix: observed, durableObservedLedgerDigest: observedDigest, finalCatalogDigest: witness.finalCatalogDigest["000001"], chainWitness: witness}
}
