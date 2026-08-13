package migration

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"testing"
)

type structuralObserverProbe struct {
	headerCalls int
	headerErrAt int
	headerErr   error
	intentErr   error
	terminalErr error
}

func (p *structuralObserverProbe) observeHeader(JournalHeader) error {
	p.headerCalls++
	if p.headerCalls == p.headerErrAt {
		return p.headerErr
	}
	return nil
}
func (p *structuralObserverProbe) observeIntent(StatementIntent) error { return p.intentErr }
func (p *structuralObserverProbe) observeTerminal(AttemptTerminalState, evidenceCompactAttemptState, JournalHeader) error {
	return p.terminalErr
}

func TestStructuralObserverErrorPriorityAndStructuralDominance(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	headerErr, intentErr, terminalErr := errors.New("header"), errors.New("intent"), errors.New("terminal")

	all := &structuralObserverProbe{headerErrAt: 1, headerErr: headerErr, intentErr: intentErr, terminalErr: terminalErr}
	if _, err := validateEvidenceChainStructureSegmentsObserved([][]EvidenceFrame{frames}, nil, all); !errors.Is(err, headerErr) {
		t.Fatalf("header category did not dominate: %v", err)
	}

	corrupt := cloneProjectionValue(frames)
	corrupt[len(corrupt)-1].Sequence++
	late := &structuralObserverProbe{intentErr: intentErr, terminalErr: terminalErr}
	if _, err := validateEvidenceChainStructureSegmentsObserved([][]EvidenceFrame{corrupt}, nil, late); err == nil || errors.Is(err, intentErr) || errors.Is(err, terminalErr) {
		t.Fatalf("observer error masked structural corruption: %v", err)
	}

	// A header error discovered in a later physical segment must still beat an
	// earlier intent error, matching the legacy headers->intents->terminals pass.
	rotated := cloneProjectionValue(frames)
	header := *rotated[0].Record.Header
	header.ReservedSegments = 2
	header.ReservedRecords++
	header.ReservedBytes += 65536
	rotated[0].Record.Header = &header
	redigestEvidenceFrames(t, rotated)
	rotation := header
	rotation.SegmentIndex = 1
	rotation.PreviousSegmentRecordDigest = digestPointer(rotated[len(rotated)-1].RecordDigest)
	rotationFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: uint64(len(rotated)), PreviousRecordDigest: digestPointer(rotated[len(rotated)-1].RecordDigest), RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &rotation}}
	rotationFrame.RecordDigest, _ = rotationFrame.ComputeDigest()
	lateHeader := &structuralObserverProbe{headerErrAt: 2, headerErr: headerErr, intentErr: intentErr}
	if _, err := validateEvidenceChainStructureSegmentsObserved([][]EvidenceFrame{rotated, {rotationFrame}}, nil, lateHeader); !errors.Is(err, headerErr) {
		t.Fatalf("late header category did not dominate earlier intent: %v", err)
	}
}

func TestStructuralStreamingStateContainsNoFrameCollections(t *testing.T) {
	t.Parallel()
	for _, root := range []reflect.Type{
		reflect.TypeOf(evidenceStructuralReplay{}),
		reflect.TypeOf(evidenceStructuralAccumulator{}),
		reflect.TypeOf(evidenceMigrationStructuralState{}),
		reflect.TypeOf(lineageStructuralPlan{}),
	} {
		var walk func(reflect.Type, map[reflect.Type]bool)
		walk = func(value reflect.Type, seen map[reflect.Type]bool) {
			if seen[value] {
				return
			}
			seen[value] = true
			switch value.Kind() {
			case reflect.Pointer:
				walk(value.Elem(), seen)
			case reflect.Slice, reflect.Array:
				if value.Elem() == reflect.TypeOf(EvidenceFrame{}) {
					t.Fatalf("%v retains EvidenceFrame collection", root)
				}
				walk(value.Elem(), seen)
			case reflect.Map:
				if value.Elem() == reflect.TypeOf(EvidenceFrame{}) {
					t.Fatalf("%v retains EvidenceFrame map", root)
				}
				walk(value.Elem(), seen)
			case reflect.Struct:
				for index := 0; index < value.NumField(); index++ {
					walk(value.Field(index).Type, seen)
				}
			}
		}
		walk(root, map[reflect.Type]bool{})
	}
}

func TestStructuralStreamingHeaderOnlySummaryAndHighWater(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	header := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	canonical, err := canonicalContractKey(header)
	if err != nil {
		t.Fatal(err)
	}
	accumulator := newEvidenceStructuralAccumulator([]evidenceCheckpointRequirement{{1, header.RecordDigest, evidenceJournalSummaryDigest(evidenceJournalSummary{recoveryState: "brand_new"})}}, nil)
	if err := accumulator.beginSegment(); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.consumeFrame(header, uint64(len(canonical))+8); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.endSegment(); err != nil {
		t.Fatal(err)
	}
	replay, err := accumulator.finish()
	if err != nil {
		t.Fatal(err)
	}
	if replay.summary.recoveryState != "brand_new" {
		t.Fatalf("header-only summary=%q", replay.summary.recoveryState)
	}
}

func TestStructuralContinuationSeedConsumesExactFirstIntent(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	var header EvidenceFrame
	var intent EvidenceFrame
	for _, frame := range frames {
		if frame.RecordKind == EvidenceRecordHeader {
			header = cloneProjectionValue(frame)
		}
		if frame.RecordKind == EvidenceRecordStatementIntent {
			intent = cloneProjectionValue(frame)
			break
		}
	}
	previous := DigestBytes([]byte("previous-terminal"))
	continuation := &LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: intent.Record.StatementIntent.MigrationID, AttemptIndex: 2, PreviousAttemptTerminalDigest: &previous, SourceJournalIdentityDigest: DigestBytes([]byte("source-journal")), SourceCheckpointRecordDigest: DigestBytes([]byte("source-checkpoint")), SourceTerminalDigest: previous}
	header.Sequence = 0
	header.PreviousRecordDigest = nil
	header.RecordDigest, _ = header.ComputeDigest()
	v := intent.Record.StatementIntent
	v.AttemptIndex = 2
	v.StatementIndex = 0
	v.PreviousAttemptTerminalDigest = &previous
	intent.Sequence = 1
	intent.PreviousRecordDigest = digestPointer(header.RecordDigest)
	intent.RecordDigest, _ = intent.ComputeDigest()
	replay := func(continuation *LineageContinuationContext, records []EvidenceFrame) error {
		seed, seedErr := newEvidenceStructuralContinuationSeed(continuation)
		if seedErr != nil {
			return seedErr
		}
		plan := &lineageStructuralPlan{journals: map[Digest]*lineageJournalPlan{header.Record.Header.JournalIdentityDigest: {continuation: seed}}}
		a, ok := plan.newJournalAccumulator(header.Record.Header.JournalIdentityDigest, nil)
		if !ok {
			return invalidEvidence("test", "missing accumulator")
		}
		if err := a.beginSegment(); err != nil {
			return err
		}
		for _, f := range records {
			canonical, _ := canonicalContractKey(f)
			if err := a.consumeFrame(f, uint64(len(canonical))+8); err != nil {
				return err
			}
		}
		if err := a.endSegment(); err != nil {
			return err
		}
		_, err := a.finish()
		return err
	}
	if err := replay(continuation, []EvidenceFrame{header}); err != nil {
		t.Fatalf("seeded header-only journal rejected: %v", err)
	}
	if err := replay(continuation, []EvidenceFrame{header, intent}); err != nil {
		t.Fatalf("exact seeded intent rejected: %v", err)
	}
	legacy := cloneProjectionValue(intent)
	legacy.Record.StatementIntent.AttemptIndex = 1
	legacy.Record.StatementIntent.PreviousAttemptTerminalDigest = nil
	legacy.RecordDigest, _ = legacy.ComputeDigest()
	if err := replay(nil, []EvidenceFrame{header, legacy}); err != nil {
		t.Fatalf("legacy nil seed changed behavior: %v", err)
	}
	if err := replay(nil, []EvidenceFrame{header, intent}); err == nil {
		t.Fatal("legacy nil seed accepted a successor attempt")
	}
	candidates := append(cloneProjectionValue(frames[2:]), decodeEvidenceFrames(t, fixtureObject(t, migrationFixturePath(t, "golden/evidence-ambiguous-chain-v1.json"))["frames"])...)
	for _, candidate := range candidates {
		if candidate.RecordKind == EvidenceRecordHeader || candidate.RecordKind == EvidenceRecordStatementIntent {
			continue
		}
		first := cloneProjectionValue(candidate)
		first.Sequence = 1
		first.PreviousRecordDigest = digestPointer(header.RecordDigest)
		first.RecordDigest, _ = first.ComputeDigest()
		if err := replay(continuation, []EvidenceFrame{header, first}); err == nil {
			t.Fatalf("seed accepted %s as first record", first.RecordKind)
		}
	}
	mutations := []func(*EvidenceFrame){func(f *EvidenceFrame) { f.Record.StatementIntent.MigrationID = "000002" }, func(f *EvidenceFrame) { f.Record.StatementIntent.AttemptIndex = 3 }, func(f *EvidenceFrame) { f.Record.StatementIntent.StatementIndex = 1 }, func(f *EvidenceFrame) { f.Record.StatementIntent.PreviousAttemptTerminalDigest = nil }, func(f *EvidenceFrame) {
		f.Record.StatementIntent.PreviousAttemptTerminalDigest = digestPointer(projectionTestDigest)
	}}
	for _, mutate := range mutations {
		fault := cloneProjectionValue(intent)
		mutate(&fault)
		fault.RecordDigest, _ = fault.ComputeDigest()
		if err := replay(continuation, []EvidenceFrame{header, fault}); err == nil {
			t.Fatal("continuation mismatch accepted")
		}
	}
	bad := cloneProjectionValue(*continuation)
	bad.AttemptIndex = 1
	if _, err := newEvidenceStructuralContinuationSeed(&bad); err == nil {
		t.Fatal("invalid continuation seed accepted")
	}
}

func TestStructuralLineageJournalReplayInputsOwnContinuation(t *testing.T) {
	t.Parallel()
	previous := DigestBytes([]byte("previous-terminal"))
	continuation := &LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2, PreviousAttemptTerminalDigest: &previous, SourceJournalIdentityDigest: DigestBytes([]byte("source-journal")), SourceCheckpointRecordDigest: DigestBytes([]byte("source-checkpoint")), SourceTerminalDigest: previous}
	seed, err := newEvidenceStructuralContinuationSeed(continuation)
	if err != nil {
		t.Fatal(err)
	}
	id := DigestBytes([]byte("journal"))
	plan := &lineageStructuralPlan{journals: map[Digest]*lineageJournalPlan{id: {requirements: []evidenceCheckpointRequirement{{nextSequence: 2, tailDigest: projectionTestDigest}}, continuation: seed}}}
	got, ok := plan.newJournalAccumulator(id, nil)
	if !ok || got == nil || got.structuralSeed == nil || len(got.requirements) != 1 {
		t.Fatal("journal replay inputs missing")
	}
	got.requirements[0].nextSequence = 9
	got.structuralSeed.context.MigrationID = "000002"
	*got.structuralSeed.context.PreviousAttemptTerminalDigest = projectionTestDigest
	second, ok := plan.newJournalAccumulator(id, nil)
	if !ok || second.requirements[0].nextSequence != 2 || second.structuralSeed.context.MigrationID != "000001" || *second.structuralSeed.context.PreviousAttemptTerminalDigest != previous {
		t.Fatal("journal replay inputs aliased plan state")
	}
	finalID := DigestBytes([]byte("final-reserved-journal"))
	finalReserved := cloneProjectionValue(*continuation)
	finalPlan := &lineageStructuralPlan{journals: map[Digest]*lineageJournalPlan{}, finalReserved: &GenerationReserved{JournalIdentityDigest: finalID, Continuation: &finalReserved}}
	final, ok := finalPlan.newJournalAccumulator(finalID, nil)
	if !ok || final == nil || final.structuralSeed == nil || final.structuralSeed.context.MigrationID != continuation.MigrationID {
		t.Fatal("final reserved continuation was not bound to its accumulator")
	}
}

func TestStructuralContinuationSeedIgnoresRotationHeaders(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	header := cloneProjectionValue(journal[0])
	base := cloneProjectionValue(*header.Record.Header)
	base.ReservedSegments = 2
	base.ReservedRecords++
	base.ReservedBytes += 65536
	header.Record.Header = &base
	header.RecordDigest, _ = header.ComputeDigest()
	rotation := cloneProjectionValue(base)
	rotation.SegmentIndex = 1
	rotation.PreviousSegmentRecordDigest = digestPointer(header.RecordDigest)
	rotationFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 1, PreviousRecordDigest: digestPointer(header.RecordDigest), RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &rotation}}
	rotationFrame.RecordDigest, _ = rotationFrame.ComputeDigest()
	previous := DigestBytes([]byte("rotation-predecessor"))
	continuation := &LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2, PreviousAttemptTerminalDigest: &previous, SourceJournalIdentityDigest: DigestBytes([]byte("source-journal")), SourceCheckpointRecordDigest: DigestBytes([]byte("source-checkpoint")), SourceTerminalDigest: previous}
	seed, err := newEvidenceStructuralContinuationSeed(continuation)
	if err != nil {
		t.Fatal(err)
	}
	plan := &lineageStructuralPlan{journals: map[Digest]*lineageJournalPlan{base.JournalIdentityDigest: {continuation: seed}}}
	accumulator, ok := plan.newJournalAccumulator(base.JournalIdentityDigest, nil)
	if !ok {
		t.Fatal("missing seeded accumulator")
	}
	if err := accumulator.beginSegment(); err != nil {
		t.Fatal(err)
	}
	canonical, _ := canonicalContractKey(header)
	if err := accumulator.consumeFrame(header, uint64(len(canonical))+8); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.endSegment(); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.beginSegment(); err != nil {
		t.Fatal(err)
	}
	canonical, _ = canonicalContractKey(rotationFrame)
	if err := accumulator.consumeFrame(rotationFrame, uint64(len(canonical))+8); err != nil {
		t.Fatal(err)
	}
	intent := cloneProjectionValue(journal[1])
	intent.Record.StatementIntent.AttemptIndex = 2
	intent.Record.StatementIntent.PreviousAttemptTerminalDigest = &previous
	intent.Sequence = 2
	intent.PreviousRecordDigest = digestPointer(rotationFrame.RecordDigest)
	intent.RecordDigest, _ = intent.ComputeDigest()
	canonical, _ = canonicalContractKey(intent)
	if err := accumulator.consumeFrame(intent, uint64(len(canonical))+8); err != nil {
		t.Fatalf("rotation header consumed or invalidated continuation: %v", err)
	}
	if err := accumulator.endSegment(); err != nil {
		t.Fatal(err)
	}
	if _, err := accumulator.finish(); err != nil {
		t.Fatal(err)
	}
}

func TestStructuralLineagePlanSeedsSuccessorJournalEndToEnd(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	index := decodeLineageFrames(t, fixture["frames"])
	journalFixture := decodeEvidenceFrames(t, fixture["journal_frames"])
	var baseIntent EvidenceFrame
	for _, frame := range journalFixture {
		if frame.Record.StatementIntent != nil {
			baseIntent = cloneProjectionValue(frame)
			break
		}
	}
	if len(index) < 5 || index[3].Record.Checkpoint == nil || index[4].Record.Superseded == nil || baseIntent.Record.StatementIntent == nil {
		t.Fatal("fixture lacks successor inputs")
	}
	oldCheckpoint := index[3]
	oldTerminal := cloneDigestPointer(oldCheckpoint.Record.Checkpoint.LastTerminalDigest)
	if oldTerminal == nil {
		t.Fatal("fixture checkpoint has no terminal")
	}
	for _, test := range []struct {
		name, outcome, action, migration string
		attempt                          uint32
		previous                         *Digest
	}{
		{"next-entry", "exact_committed_continue_successor", "begin_first_attempt_next_entry", "000002", 1, nil},
		{"next-attempt", "exact_pending", "begin_next_attempt", *oldCheckpoint.Record.Checkpoint.MigrationID, *oldCheckpoint.Record.Checkpoint.AttemptIndex + 1, oldTerminal},
	} {
		t.Run(test.name, func(t *testing.T) {
			chain := cloneProjectionValue(index[:5])
			planned := cloneProjectionValue(*chain[1].Record.Reserved)
			plannedHeader := cloneProjectionValue(planned.PlannedSegment0Header)
			plannedHeader.OuterArtifactDigest = DigestBytes([]byte("successor-runtime-" + test.name))
			journalID, err := JournalIdentityDigest(plannedHeader)
			if err != nil {
				t.Fatal(err)
			}
			planned.JournalIdentityDigest = journalID
			plannedHeader.JournalIdentityDigest = journalID
			planned.Continuation = &LineageContinuationContext{StartAction: test.action, MigrationID: test.migration, AttemptIndex: test.attempt, PreviousAttemptTerminalDigest: cloneDigestPointer(test.previous), SourceJournalIdentityDigest: chain[4].Record.Superseded.OldJournalIdentityDigest, SourceCheckpointRecordDigest: oldCheckpoint.RecordDigest, SourceTerminalDigest: *oldTerminal}
			planned.QuotaReservationDigest, err = QuotaReservationDigest(planned)
			if err != nil {
				t.Fatal(err)
			}
			plannedHeader.QuotaReservationDigest = planned.QuotaReservationDigest
			planned.PlannedSegment0Header = plannedHeader
			header := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: 0, RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &planned.PlannedSegment0Header}}
			header.RecordDigest, err = header.ComputeDigest()
			if err != nil {
				t.Fatal(err)
			}
			planned.ExpectedSegment0HeaderDigest = header.RecordDigest
			chain[4].Record.Superseded.Outcome = test.outcome
			chain[4].Record.Superseded.PlannedGenerationReserved = &planned
			redigestStructuralLineageFrames(t, chain)

			reservedFrame := LineageIndexFrame{FormatVersion: LineageFrameFormat, RecordKind: LineageRecordGenerationReserved, Record: LineageIndexRecord{Reserved: &planned}}
			chain = append(chain, reservedFrame)
			redigestStructuralLineageFrames(t, chain)
			oldJournalID := journalFixture[0].Record.Header.JournalIdentityDigest
			reservedActual := map[Digest]EvidenceFrame{oldJournalID: journalFixture[0], journalID: header}
			reservedJournals := map[Digest][][]EvidenceFrame{oldJournalID: {cloneProjectionValue(journalFixture)}, journalID: {{header}}}
			if _, err := validateLineageChainStructure(chain, reservedActual, reservedJournals); err != nil {
				t.Fatalf("successor reserved header-only replay failed: %v", err)
			}
			activated := GenerationActivated{ExecutionLineageDigest: planned.ExecutionLineageDigest, JournalIdentityDigest: planned.JournalIdentityDigest, RunnerProjectionDecisionDigest: planned.RunnerProjectionDecisionDigest, SchemaBundleDigest: planned.SchemaBundleDigest, QuotaReservationDigest: planned.QuotaReservationDigest, GenerationReservedRecordDigest: chain[5].RecordDigest, Segment0HeaderDigest: planned.ExpectedSegment0HeaderDigest, InitialJournalTailDigest: planned.ExpectedSegment0HeaderDigest}
			chain = append(chain, LineageIndexFrame{FormatVersion: LineageFrameFormat, RecordKind: LineageRecordGenerationActivated, Record: LineageIndexRecord{Activated: &activated}})
			redigestStructuralLineageFrames(t, chain)

			intent := cloneProjectionValue(baseIntent)
			intent.Record.StatementIntent.MigrationID = test.migration
			intent.Record.StatementIntent.AttemptIndex = test.attempt
			intent.Record.StatementIntent.StatementIndex = 0
			intent.Record.StatementIntent.PreviousAttemptTerminalDigest = cloneDigestPointer(test.previous)
			intent.Sequence = 1
			intent.PreviousRecordDigest = digestPointer(header.RecordDigest)
			intent.RecordDigest, err = intent.ComputeDigest()
			if err != nil {
				t.Fatal(err)
			}
			actual := map[Digest]EvidenceFrame{oldJournalID: journalFixture[0], journalID: header}
			journals := map[Digest][][]EvidenceFrame{oldJournalID: {cloneProjectionValue(journalFixture)}, journalID: {{header, intent}}}
			if _, err := validateLineageChainStructure(chain, actual, journals); err != nil {
				t.Fatalf("successor seeded journal replay failed: %v", err)
			}
		})
	}
}

func TestStructuralEvidenceReplayDoesNotRequireAppendTimeWitnesses(t *testing.T) {
	t.Parallel()
	contextDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	context := fixtureObjectValue(t, contextDocument["validation_context"], "validation context")

	retryDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	retryChains := retryDocument["chains"].([]JSONValue)
	for _, rawChain := range retryChains {
		chain := fixtureObjectValue(t, rawChain, "retry chain")
		name, _ := chain["name"].(string)
		t.Run("retry/"+name, func(t *testing.T) {
			frames := decodeEvidenceFrames(t, chain["frames"])
			if _, err := validateEvidenceChainStructure(frames); err != nil {
				t.Fatalf("structural replay required append-time retry receipt: %v", err)
			}
			witness := buildEvidenceWitness(t, frames, context)
			if err := validateEvidenceChainWithWitness(frames, witness); err == nil {
				t.Fatal("append-time validation accepted a retry proof without its owned receipt")
			}
		})
	}

	ambiguousDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-ambiguous-chain-v1.json"))
	ambiguousFrames := decodeEvidenceFrames(t, ambiguousDocument["frames"])
	if _, err := validateEvidenceChainStructure(ambiguousFrames); err != nil {
		t.Fatalf("structural replay required append-time ambiguous boundary: %v", err)
	}
	if err := validateEvidenceChainWithWitness(ambiguousFrames, buildEvidenceWitness(t, ambiguousFrames, context)); err == nil {
		t.Fatal("append-time validation accepted ambiguous evidence without its owned boundary")
	}
}

func TestStructuralEvidenceReplayRetainsWireFSMFaults(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	base := decodeEvidenceFrames(t, document["frames"])

	sequence := cloneProjectionValue(base)
	sequence[1].Sequence++
	if _, err := validateEvidenceChainStructure(sequence); err == nil {
		t.Fatal("structural replay accepted a sequence fault")
	}

	statementGap := cloneProjectionValue(base)
	intent := *statementGap[1].Record.StatementIntent
	intent.StatementIndex = 2
	statementGap[1].Record.StatementIntent = &intent
	redigestEvidenceFrames(t, statementGap)
	if _, err := validateEvidenceChainStructure(statementGap); err == nil {
		t.Fatal("structural replay accepted a first-statement gap")
	}

	terminalBoundary := cloneProjectionValue(base)
	terminalIndex := -1
	for index := range terminalBoundary {
		if terminalBoundary[index].Record.AttemptTerminal != nil {
			terminalIndex = index
			break
		}
	}
	if terminalIndex < 2 {
		t.Fatal("fixture has no terminal boundary")
	}
	terminalBoundary[terminalIndex-1], terminalBoundary[terminalIndex-2] = terminalBoundary[terminalIndex-2], terminalBoundary[terminalIndex-1]
	redigestEvidenceFrames(t, terminalBoundary)
	if _, err := validateEvidenceChainStructure(terminalBoundary); err == nil {
		t.Fatal("structural replay accepted a non-adjacent commit/terminal boundary")
	}
}

func TestStructuralEvidenceReplayPhysicalGroupingAndReservationBounds(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{frames}); err != nil {
		t.Fatalf("single physical segment: %v", err)
	}

	middleHeader := cloneProjectionValue(frames)
	middleHeader = append(middleHeader[:2], append([]EvidenceFrame{middleHeader[0]}, middleHeader[2:]...)...)
	redigestEvidenceFrames(t, middleHeader)
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{middleHeader}); err == nil {
		t.Fatal("accepted a middle header in one physical segment")
	}

	first := cloneProjectionValue(frames)
	header := *first[0].Record.Header
	header.ReservedRecords = uint64(len(first) - 1)
	first[0].Record.Header = &header
	redigestEvidenceFrames(t, first)
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{first}); err == nil {
		t.Fatal("accepted actual record count above reserved_records")
	}

	tooManySegments := cloneProjectionValue(frames)
	header = *tooManySegments[0].Record.Header
	header.ReservedSegments = 1
	tooManySegments[0].Record.Header = &header
	redigestEvidenceFrames(t, tooManySegments)
	rotation := header
	rotation.SegmentIndex = 1
	rotation.PreviousSegmentRecordDigest = digestPointer(tooManySegments[len(tooManySegments)-1].RecordDigest)
	rotationFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: uint64(len(tooManySegments)), PreviousRecordDigest: digestPointer(tooManySegments[len(tooManySegments)-1].RecordDigest), RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &rotation}}
	rotationFrame.RecordDigest, _ = rotationFrame.ComputeDigest()
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{tooManySegments, {rotationFrame}}); err == nil {
		t.Fatal("accepted actual segment count above reserved_segments")
	}

	physical := cloneProjectionValue(frames)
	header = *physical[0].Record.Header
	header.ReservedBytes = 1
	physical[0].Record.Header = &header
	redigestEvidenceFrames(t, physical)
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{physical}); err == nil {
		t.Fatal("accepted physical journal bytes above combined reserved_bytes")
	}

	exact := cloneProjectionValue(frames)
	exactHeader := *exact[0].Record.Header
	exactHeader.ReservedRecords = uint64(len(exact))
	exactHeader.ReservedSegments = 1
	for attempts := 0; attempts < 4; attempts++ {
		exact[0].Record.Header = &exactHeader
		redigestEvidenceFrames(t, exact)
		var bytes uint64
		for _, frame := range exact {
			canonical, err := canonicalContractKey(frame)
			if err != nil {
				t.Fatal(err)
			}
			bytes += uint64(len(canonical)) + 8
		}
		if exactHeader.ReservedBytes == bytes {
			break
		}
		exactHeader.ReservedBytes = bytes
	}
	exact[0].Record.Header = &exactHeader
	redigestEvidenceFrames(t, exact)
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{exact}); err != nil {
		t.Fatalf("exact physical reservation boundary: %v", err)
	}
	plusOne := cloneProjectionValue(exact)
	plusOneHeader := *plusOne[0].Record.Header
	plusOneHeader.ReservedBytes--
	plusOne[0].Record.Header = &plusOneHeader
	redigestEvidenceFrames(t, plusOne)
	if _, err := validateEvidenceChainStructureSegments([][]EvidenceFrame{plusOne}); err == nil {
		t.Fatal("accepted physical byte reservation plus one")
	}
}

func TestStructuralSummaryFollowsCurrentAttemptTail(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chain := fixtureObjectValue(t, document["chains"].([]JSONValue)[0], "retry chain")
	base := decodeEvidenceFrames(t, chain["frames"])
	terminal := base[len(base)-1].Record.AttemptTerminal
	intent := cloneProjectionValue(*base[1].Record.StatementIntent)
	intent.AttemptIndex = 2
	intent.PreviousAttemptTerminalDigest = digestPointer(terminal.TerminalDigest)
	intent.PreviousIntermediateStateDigest = nil
	intentFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &intent}}
	stages := []struct {
		frame EvidenceFrame
		state string
	}{
		{intentFrame, "dangling_statement_intent"},
	}
	intermediate := cloneProjectionValue(*base[2].Record.Intermediate)
	intermediate.State.AttemptIndex = 2
	intermediate.State.PreviousAttemptTerminalDigest = digestPointer(terminal.TerminalDigest)
	intermediate.State.IntermediateStateDigest, _ = intermediate.State.ComputeDigest()
	intermediateFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, RecordKind: EvidenceRecordIntermediate, Record: EvidenceRecord{Intermediate: &intermediate}}
	stages = append(stages, struct {
		frame EvidenceFrame
		state string
	}{intermediateFrame, "dangling_intermediate"})
	commitDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	commitTemplate := decodeEvidenceFrames(t, commitDocument["frames"])[3].Record.CommitIntent
	commit := cloneProjectionValue(*commitTemplate)
	commit.AttemptIndex = 2
	commit.PreviousAttemptTerminalDigest = digestPointer(terminal.TerminalDigest)
	commit.LastIntermediateStateDigest = intermediate.State.IntermediateStateDigest
	commitFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, RecordKind: EvidenceRecordCommitIntent, Record: EvidenceRecord{CommitIntent: &commit}}
	stages = append(stages, struct {
		frame EvidenceFrame
		state string
	}{commitFrame, "dangling_commit_intent"})
	for index := range stages {
		frames := cloneProjectionValue(base)
		for stage := 0; stage <= index; stage++ {
			frames = append(frames, cloneProjectionValue(stages[stage].frame))
		}
		redigestEvidenceFrames(t, frames)
		summary, err := summarizeEvidenceJournal(frames)
		if err != nil {
			t.Fatal(err)
		}
		if summary.recoveryState != stages[index].state || summary.attemptIndex == nil || *summary.attemptIndex != 2 {
			t.Fatalf("stage %d summary=%s attempt=%v", index, summary.recoveryState, summary.attemptIndex)
		}
	}

	// Within one attempt, the next statement intent must dominate the completed
	// intermediate for the previous statement.
	chainDocument := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, chainDocument["frames"])
	frames = frames[:3]
	next := cloneProjectionValue(*frames[1].Record.StatementIntent)
	next.StatementIndex = 1
	next.PreviousIntermediateStateDigest = digestPointer(frames[2].Record.Intermediate.State.IntermediateStateDigest)
	nextFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, RecordKind: EvidenceRecordStatementIntent, Record: EvidenceRecord{StatementIntent: &next}}
	frames = append(frames, nextFrame)
	redigestEvidenceFrames(t, frames)
	summary, err := summarizeEvidenceJournal(frames)
	if err != nil {
		t.Fatal(err)
	}
	if summary.recoveryState != "dangling_statement_intent" {
		t.Fatalf("next statement summary=%s", summary.recoveryState)
	}
}

func TestStructuralEvidenceReplayRetainsRetryProofBoundaries(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-retry-chains-v1.json"))
	chains := document["chains"].([]JSONValue)
	for _, rawChain := range chains {
		chain := fixtureObjectValue(t, rawChain, "retry chain")
		frames := decodeEvidenceFrames(t, chain["frames"])
		terminalIndex := -1
		for index := range frames {
			if frames[index].Record.AttemptTerminal != nil {
				terminalIndex = index
				break
			}
		}
		if terminalIndex < 0 {
			t.Fatal("retry fixture terminal missing")
		}
		proofKind := frames[terminalIndex].Record.AttemptTerminal.RetryProof.ProofKind
		switch proofKind {
		case "commit_rejected_exact_predecessor":
			withoutCommit := append(cloneProjectionValue(frames[:terminalIndex-1]), cloneProjectionValue(frames[terminalIndex:])...)
			redigestEvidenceFrames(t, withoutCommit)
			if _, err := validateEvidenceChainStructure(withoutCommit); err == nil {
				t.Fatal("structural replay accepted commit-rejected proof without commit intent")
			}
		default:
			commit := cloneProjectionValue(frames[terminalIndex-1])
			commit.RecordKind = EvidenceRecordCommitIntent
			commit.Record = EvidenceRecord{CommitIntent: &CommitIntent{
				SchemaBundleDigest:              frames[0].Record.Header.SchemaBundleDigest,
				CatalogContractDigest:           frames[terminalIndex].Record.AttemptTerminal.CatalogContractDigest,
				AuthorityProfileDigest:          frames[0].Record.Header.AuthorityProfileDigest,
				AuthorityBindingDigest:          frames[0].Record.Header.AuthorityBindingDigest,
				MigrationID:                     frames[terminalIndex].Record.AttemptTerminal.MigrationID,
				AttemptIndex:                    frames[terminalIndex].Record.AttemptTerminal.AttemptIndex,
				PreviousAttemptTerminalDigest:   cloneDigestPointer(frames[terminalIndex].Record.AttemptTerminal.PreviousAttemptTerminalDigest),
				AttemptPredecessorCatalogDigest: projectionTestDigest,
				LastIntermediateStateDigest:     frames[terminalIndex-1].Record.Intermediate.State.IntermediateStateDigest,
				ExpectedLedgerLength:            1,
				ExpectedLedgerHead:              frames[terminalIndex].Record.AttemptTerminal.MigrationID,
				LedgerRow: CommitIntentLedgerRow{
					MigrationID: frames[terminalIndex].Record.AttemptTerminal.MigrationID, MigrationName: "test", Phase: "expand", SchemaFrom: "0", SchemaTo: "1", CompatibleBinaryMin: "0", CompatibleBinaryMax: "1", SQLPath: "migration.sql", SQLSizeBytes: 1, SQLSHA256: projectionTestDigest, BundleDigest: projectionTestDigest, TransactionMode: "single", Reentrancy: "idempotent", RollbackBoundary: "precommit",
				},
			}}
			withCommit := append(cloneProjectionValue(frames[:terminalIndex]), append([]EvidenceFrame{commit}, cloneProjectionValue(frames[terminalIndex:])...)...)
			redigestEvidenceFrames(t, withCommit)
			if _, err := validateEvidenceChainStructure(withCommit); err == nil {
				t.Fatalf("structural replay accepted %s proof after commit intent", proofKind)
			}
		}
	}
}

func TestStructuralEvidenceReplayOwnsInputWithoutMintingAuthority(t *testing.T) {
	t.Parallel()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	frames := decodeEvidenceFrames(t, document["frames"])
	replay, err := validateEvidenceChainStructure(frames)
	if err != nil {
		t.Fatal(err)
	}
	frames[0].Record.Header.SchemaBundleDigest = projectionTestDigest
	if replay.firstFrame.Record.Header.SchemaBundleDigest == projectionTestDigest {
		t.Fatal("structural replay aliased caller frames")
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"acceptJournal":                                  true,
		"finish":                                         true,
		"summarizeStructuralEvidenceJournal":             true,
		"validateEvidenceChainStructure":                 true,
		"validateEvidenceChainStructureSegments":         true,
		"validateEvidenceChainStructureSegmentsObserved": true,
		"validateLineageChainStructure":                  true,
	}
	containsTranscript := func(fields *ast.FieldList) bool {
		found := false
		if fields == nil {
			return false
		}
		for _, field := range fields.List {
			ast.Inspect(field.Type, func(node ast.Node) bool {
				name, ok := node.(*ast.Ident)
				if ok && (name.Name == "evidenceStructuralReplay" || name.Name == "lineageStructuralReplay") {
					found = true
					return false
				}
				return !found
			})
		}
		return found
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Type == nil || !containsTranscript(function.Type.Params) && !containsTranscript(function.Type.Results) {
				continue
			}
			if !allowed[function.Name.Name] {
				t.Fatalf("production structural transcript signature is not a pure structural allowlist member: %s in %s", function.Name.Name, name)
			}
			seen[function.Name.Name] = true
		}
	}
	for name := range allowed {
		if !seen[name] {
			t.Fatalf("structural signature allowlist entry disappeared: %s", name)
		}
	}
	bannedSeedSymbols := map[string]bool{
		"evidenceStructuralContinuationSeed":    true,
		"newEvidenceStructuralContinuationSeed": true,
		"newJournalAccumulator":                 true,
		"structuralSeed":                        true,
		"structuralSeedConsumed":                true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "evidence_replay_structural.go" || len(name) < 3 || name[len(name)-3:] != ".go" || len(name) >= 8 && name[len(name)-8:] == "_test.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && bannedSeedSymbols[identifier.Name] {
				t.Fatalf("continuation seed authority escaped to %s via %s", name, identifier.Name)
			}
			return true
		})
	}
}

func TestStructuralLineageReservedStatesCloseInventory(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	lineage := decodeLineageFrames(t, fixture["frames"])[:2]
	header := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	journalID := lineage[1].Record.Reserved.JournalIdentityDigest

	if _, err := validateLineageChainStructure(lineage, nil, nil); err != nil {
		t.Fatalf("reserved without materialized header: %v", err)
	}
	actual := map[Digest]EvidenceFrame{journalID: header}
	journals := map[Digest][][]EvidenceFrame{journalID: {{header}}}
	if _, err := validateLineageChainStructure(lineage, actual, journals); err != nil {
		t.Fatalf("reserved with exact materialized header: %v", err)
	}

	mismatch := cloneProjectionValue(header)
	mismatch.Sequence = 1
	if _, err := validateLineageChainStructure(lineage, map[Digest]EvidenceFrame{journalID: mismatch}, journals); err == nil {
		t.Fatal("accepted a mismatched reserved segment-zero header")
	}
	progress := decodeEvidenceFrames(t, fixture["journal_frames"])
	if _, err := validateLineageChainStructure(lineage, actual, map[Digest][][]EvidenceFrame{journalID: {progress[:2]}}); err == nil {
		t.Fatal("accepted progress before reserved generation activation")
	}
	orphanID := DigestBytes([]byte("reserved-orphan"))
	if _, err := validateLineageChainStructure(lineage, map[Digest]EvidenceFrame{orphanID: header}, nil); err == nil {
		t.Fatal("accepted orphan segment-zero inventory for reserved generation")
	}
}

func TestStructuralLineageActiveTailRecoveryWindow(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	header := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	journalID := header.Record.Header.JournalIdentityDigest
	actual := map[Digest]EvidenceFrame{journalID: header}

	activeWithoutCheckpoint := cloneProjectionValue(frames[:3])
	for length, wantOK := range map[int]bool{1: true, 2: true, 3: false} {
		_, err := validateLineageChainStructure(activeWithoutCheckpoint, actual, map[Digest][][]EvidenceFrame{journalID: {cloneProjectionValue(journal[:length])}})
		if (err == nil) != wantOK {
			t.Fatalf("active no-checkpoint journal length %d accepted=%t err=%v", length, err == nil, err)
		}
	}

	activeWithCheckpoint := cloneProjectionValue(frames[:4])
	checkpoint := activeWithCheckpoint[3].Record.Checkpoint
	checkpoint.JournalNextSequence = 3
	checkpoint.JournalTailDigest = journal[2].RecordDigest
	summary, err := summarizeEvidenceJournal(journal[:3])
	if err != nil {
		t.Fatal(err)
	}
	applySummaryToCheckpoint(checkpoint, summary)
	redigestStructuralLineageFrames(t, activeWithCheckpoint)
	for length, wantOK := range map[int]bool{3: true, 4: true, 5: false} {
		_, err := validateLineageChainStructure(activeWithCheckpoint, actual, map[Digest][][]EvidenceFrame{journalID: {cloneProjectionValue(journal[:length])}})
		if (err == nil) != wantOK {
			t.Fatalf("active checkpoint journal length %d accepted=%t err=%v", length, err == nil, err)
		}
	}
}

func TestStructuralLineageReplayDoesNotRequireHistoricalAuthority(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	journalHeader := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	journalID := journalHeader.Record.Header.JournalIdentityDigest
	actual := map[Digest]EvidenceFrame{journalID: journalHeader}
	journals := map[Digest][][]EvidenceFrame{journalID: {journal}}

	if _, err := validateLineageChainStructure(frames, actual, journals); err != nil {
		t.Fatalf("structural lineage replay required historical authority: %v", err)
	}
	extraID := DigestBytes([]byte("orphan-journal"))
	orphanJournals := cloneProjectionValue(journals)
	orphanJournals[extraID] = cloneProjectionValue(journals[journalID])
	if _, err := validateLineageChainStructure(frames, actual, orphanJournals); err == nil {
		t.Fatal("accepted an orphan registered journal")
	}
	orphanSegment0 := cloneProjectionValue(actual)
	orphanSegment0[extraID] = journalHeader
	if _, err := validateLineageChainStructure(frames, orphanSegment0, journals); err == nil {
		t.Fatal("accepted an orphan segment-zero view")
	}
	witness := verifiedLineageChainWitness{header: *frames[0].Record.Header, actualSegment0: actual, journals: map[Digest][]EvidenceFrame{journalID: journal}, historicalRecovery: verifiedHistoricalRecoveryChain{authorities: map[Digest]lineageSupersessionAuthoritySubject{}}}
	if err := validateLineageChainWithWitness(frames, witness); err == nil {
		t.Fatal("authority replay accepted a supersession without historical authority")
	}

	checkpointFault := cloneProjectionValue(frames)
	checkpoint := *checkpointFault[3].Record.Checkpoint
	checkpoint.RecoveryState = "dangling_commit_intent"
	checkpointFault[3].Record.Checkpoint = &checkpoint
	checkpointFault[3].RecordDigest, _ = checkpointFault[3].ComputeDigest()
	if _, err := validateLineageChainStructure(checkpointFault, actual, journals); err == nil {
		t.Fatal("structural lineage replay accepted a checkpoint summary fault")
	}
	initialContinuation := cloneProjectionValue(frames)
	initialContinuation[1].Record.Reserved.Continuation = &LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: "000001", AttemptIndex: 2, PreviousAttemptTerminalDigest: digestPointer(projectionTestDigest), SourceJournalIdentityDigest: journalID, SourceCheckpointRecordDigest: projectionTestDigest, SourceTerminalDigest: projectionTestDigest}
	initialContinuation[1].Record.Reserved.QuotaReservationDigest, _ = QuotaReservationDigest(*initialContinuation[1].Record.Reserved)
	initialContinuation[1].Record.Reserved.PlannedSegment0Header.QuotaReservationDigest = initialContinuation[1].Record.Reserved.QuotaReservationDigest
	redigestStructuralLineageFrames(t, initialContinuation)
	if _, err := validateLineageChainStructure(initialContinuation, actual, journals); err == nil {
		t.Fatal("accepted an initial generation continuation")
	}

}

func TestStructuralLineageCheckpointAcceptsValidatedMultiSegmentJournal(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])[:4]
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])

	reserved := frames[1].Record.Reserved
	reserved.ReservedSegments = 2
	reserved.QuotaReservationDigest, _ = QuotaReservationDigest(*reserved)
	header := *journal[0].Record.Header
	header.ReservedSegments = reserved.ReservedSegments
	header.QuotaReservationDigest = reserved.QuotaReservationDigest
	journal[0].Record.Header = &header
	redigestEvidenceFrames(t, journal)
	rotation := header
	rotation.SegmentIndex = 1
	rotation.PreviousSegmentRecordDigest = digestPointer(journal[len(journal)-1].RecordDigest)
	rotationFrame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: uint64(len(journal)), PreviousRecordDigest: digestPointer(journal[len(journal)-1].RecordDigest), RecordKind: EvidenceRecordHeader, Record: EvidenceRecord{Header: &rotation}}
	rotationFrame.RecordDigest, _ = rotationFrame.ComputeDigest()
	journal = append(journal, rotationFrame)

	reserved.PlannedSegment0Header = header
	reserved.ExpectedSegment0HeaderDigest = journal[0].RecordDigest
	frames[1].Record.Reserved = reserved
	activated := frames[2].Record.Activated
	activated.QuotaReservationDigest = reserved.QuotaReservationDigest
	activated.Segment0HeaderDigest = reserved.ExpectedSegment0HeaderDigest
	activated.InitialJournalTailDigest = reserved.ExpectedSegment0HeaderDigest
	frames[2].Record.Activated = activated
	checkpoint := frames[3].Record.Checkpoint
	checkpoint.JournalNextSequence = uint64(len(journal))
	checkpoint.JournalTailDigest = journal[len(journal)-1].RecordDigest
	checkpoint.RecoveryState = "completed"
	summary, err := summarizeEvidenceJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.MigrationID = cloneStringPointer(summary.migrationID)
	checkpoint.AttemptIndex = cloneUint32Pointer(summary.attemptIndex)
	checkpoint.LastStatementIntentRecordDigest = cloneDigestPointer(summary.lastStatementIntentRecordDigest)
	checkpoint.LastIntermediateEvidenceRecordDigest = cloneDigestPointer(summary.lastIntermediateEvidenceRecordDigest)
	checkpoint.LastCommitIntentRecordDigest = cloneDigestPointer(summary.lastCommitIntentRecordDigest)
	checkpoint.LastTerminalDigest = cloneDigestPointer(summary.lastTerminalDigest)
	checkpoint.LastResolutionDigest = cloneDigestPointer(summary.lastResolutionDigest)
	checkpoint.PreviousAttemptTerminalDigest = cloneDigestPointer(summary.previousAttemptTerminalDigest)
	checkpoint.LastIntermediateStateDigest = cloneDigestPointer(summary.lastIntermediateStateDigest)
	frames[3].Record.Checkpoint = checkpoint

	var previous *Digest
	for index := range frames {
		if index == 2 {
			frames[index].Record.Activated.GenerationReservedRecordDigest = frames[1].RecordDigest
		}
		frames[index].Sequence = uint64(index)
		frames[index].PreviousRecordDigest = cloneDigestPointer(previous)
		frames[index].RecordDigest, _ = frames[index].ComputeDigest()
		previous = digestPointer(frames[index].RecordDigest)
	}

	journalID := header.JournalIdentityDigest
	actual := map[Digest]EvidenceFrame{journalID: journal[0]}
	journals := map[Digest][][]EvidenceFrame{journalID: {journal[:len(journal)-1], journal[len(journal)-1:]}}
	if _, err := validateLineageChainStructure(frames, actual, journals); err != nil {
		t.Fatalf("validated multi-segment checkpoint replay: %v", err)
	}
}

func TestStructuralLineageCheckpointAcceptsHistoricalPrefixes(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	frames := decodeLineageFrames(t, fixture["frames"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	journalHeader := decodeOneEvidenceFrame(t, fixture["journal_header_frame"])
	firstCheckpoint := cloneProjectionValue(frames[3])
	firstCheckpoint.Record.Checkpoint.JournalNextSequence = 3
	firstCheckpoint.Record.Checkpoint.JournalTailDigest = journal[2].RecordDigest
	firstSummary, err := summarizeEvidenceJournal(journal[:3])
	if err != nil {
		t.Fatal(err)
	}
	applySummaryToCheckpoint(firstCheckpoint.Record.Checkpoint, firstSummary)
	lineage := append(cloneProjectionValue(frames[:3]), firstCheckpoint, cloneProjectionValue(frames[3]))
	lineage[4].Record.Checkpoint.PreviousCheckpointRecordDigest = digestPointer(lineage[3].RecordDigest)
	lineage = lineage[:5]
	redigestStructuralLineageFrames(t, lineage)
	lineage[4].Record.Checkpoint.PreviousCheckpointRecordDigest = digestPointer(lineage[3].RecordDigest)
	redigestStructuralLineageFrames(t, lineage)
	journalID := journalHeader.Record.Header.JournalIdentityDigest
	if _, err := validateLineageChainStructure(lineage, map[Digest]EvidenceFrame{journalID: journalHeader}, map[Digest][][]EvidenceFrame{journalID: {journal}}); err != nil {
		t.Fatalf("historical checkpoint prefix: %v", err)
	}

	nonIncreasing := cloneProjectionValue(lineage)
	nonIncreasing[4].Record.Checkpoint.JournalNextSequence = 3
	nonIncreasing[4].Record.Checkpoint.JournalTailDigest = journal[2].RecordDigest
	applySummaryToCheckpoint(nonIncreasing[4].Record.Checkpoint, firstSummary)
	redigestStructuralLineageFrames(t, nonIncreasing)
	if _, err := validateLineageChainStructure(nonIncreasing, map[Digest]EvidenceFrame{journalID: journalHeader}, map[Digest][][]EvidenceFrame{journalID: {journal}}); err == nil {
		t.Fatal("accepted non-increasing checkpoint prefix")
	}

	withSupersession := append(cloneProjectionValue(lineage), cloneProjectionValue(frames[4]))
	withSupersession[5].Record.Superseded.OldCheckpointRecordDigest = digestPointer(withSupersession[3].RecordDigest)
	redigestStructuralLineageFrames(t, withSupersession)
	if _, err := validateLineageChainStructure(withSupersession, map[Digest]EvidenceFrame{journalID: journalHeader}, map[Digest][][]EvidenceFrame{journalID: {journal}}); err == nil {
		t.Fatal("accepted supersession referring to a non-latest checkpoint")
	}
}

func applySummaryToCheckpoint(checkpoint *GenerationCheckpoint, summary evidenceJournalSummary) {
	checkpoint.RecoveryState = summary.recoveryState
	checkpoint.MigrationID = cloneStringPointer(summary.migrationID)
	checkpoint.AttemptIndex = cloneUint32Pointer(summary.attemptIndex)
	checkpoint.LastStatementIntentRecordDigest = cloneDigestPointer(summary.lastStatementIntentRecordDigest)
	checkpoint.LastIntermediateEvidenceRecordDigest = cloneDigestPointer(summary.lastIntermediateEvidenceRecordDigest)
	checkpoint.LastCommitIntentRecordDigest = cloneDigestPointer(summary.lastCommitIntentRecordDigest)
	checkpoint.LastTerminalDigest = cloneDigestPointer(summary.lastTerminalDigest)
	checkpoint.LastResolutionDigest = cloneDigestPointer(summary.lastResolutionDigest)
	checkpoint.PreviousAttemptTerminalDigest = cloneDigestPointer(summary.previousAttemptTerminalDigest)
	checkpoint.LastIntermediateStateDigest = cloneDigestPointer(summary.lastIntermediateStateDigest)
}

func redigestStructuralLineageFrames(t *testing.T, frames []LineageIndexFrame) {
	t.Helper()
	var previous *Digest
	for index := range frames {
		frames[index].Sequence = uint64(index)
		frames[index].PreviousRecordDigest = cloneDigestPointer(previous)
		frames[index].RecordDigest, _ = frames[index].ComputeDigest()
		previous = digestPointer(frames[index].RecordDigest)
	}
}

func TestStructuralSupersessionContinuationRejectsStoredContradictions(t *testing.T) {
	t.Parallel()
	journalID := DigestBytes([]byte("old-journal"))
	checkpointDigest := DigestBytes([]byte("old-checkpoint"))
	terminalDigest := DigestBytes([]byte("old-terminal"))
	migrationID := "000001"
	attempt := uint32(1)
	checkpoint := &LineageIndexFrame{Record: LineageIndexRecord{Checkpoint: &GenerationCheckpoint{JournalIdentityDigest: journalID, MigrationID: &migrationID, AttemptIndex: &attempt, LastTerminalDigest: &terminalDigest}}, RecordDigest: checkpointDigest}
	continuation := &LineageContinuationContext{StartAction: "begin_next_attempt", MigrationID: migrationID, AttemptIndex: 2, PreviousAttemptTerminalDigest: &terminalDigest, SourceJournalIdentityDigest: journalID, SourceCheckpointRecordDigest: checkpointDigest, SourceTerminalDigest: terminalDigest}
	superseded := GenerationSuperseded{Outcome: "exact_pending", OldJournalIdentityDigest: journalID, PlannedGenerationReserved: &GenerationReserved{Continuation: continuation}}
	if err := validateStructuralSupersessionContinuation(superseded, GenerationReserved{}, checkpoint); err != nil {
		t.Fatalf("valid stored continuation: %v", err)
	}
	mutations := []func(*LineageContinuationContext){
		func(value *LineageContinuationContext) { value.SourceJournalIdentityDigest = projectionTestDigest },
		func(value *LineageContinuationContext) { value.SourceCheckpointRecordDigest = projectionTestDigest },
		func(value *LineageContinuationContext) { value.SourceTerminalDigest = projectionTestDigest },
	}
	for index, mutate := range mutations {
		fault := cloneProjectionValue(superseded)
		mutate(fault.PlannedGenerationReserved.Continuation)
		if err := validateStructuralSupersessionContinuation(fault, GenerationReserved{}, checkpoint); err == nil {
			t.Fatalf("accepted stored continuation contradiction %d", index)
		}
	}

	oldContinuation := cloneProjectionValue(continuation)
	carried := cloneProjectionValue(continuation)
	carried.SourceTerminalDigest = projectionTestDigest
	carry := GenerationSuperseded{Outcome: "activated_no_migration_progress", PlannedGenerationReserved: &GenerationReserved{Continuation: carried}}
	if err := validateStructuralSupersessionContinuation(carry, GenerationReserved{Continuation: oldContinuation}, nil); err == nil {
		t.Fatal("accepted activated-no-progress continuation that was not an exact carry")
	}
}

func TestStructuralActivatedNoProgressRequiresHeaderOnlyJournal(t *testing.T) {
	t.Parallel()
	fixture := fixtureObject(t, migrationFixturePath(t, "golden/lineage-index-chain-v1.json"))
	base := decodeLineageFrames(t, fixture["frames"])
	journal := decodeEvidenceFrames(t, fixture["journal_frames"])
	lineage := cloneProjectionValue(base[:3])
	reserved := cloneProjectionValue(*lineage[1].Record.Reserved)
	reserved.Continuation = nil
	reserved.QuotaReservationDigest, _ = QuotaReservationDigest(reserved)
	reserved.PlannedSegment0Header.QuotaReservationDigest = reserved.QuotaReservationDigest
	lineage[1].Record.Reserved = &reserved
	redigestStructuralLineageFrames(t, lineage)
	lineage[2].Record.Activated.GenerationReservedRecordDigest = lineage[1].RecordDigest
	lineage[2].Record.Activated.QuotaReservationDigest = reserved.QuotaReservationDigest
	redigestStructuralLineageFrames(t, lineage)
	superseded := cloneProjectionValue(*base[4].Record.Superseded)
	superseded.Outcome = "activated_no_migration_progress"
	superseded.OldCheckpointRecordDigest = nil
	superseded.OldActivationRecordDigest = digestPointer(lineage[2].RecordDigest)
	superseded.OldInitialJournalTailDigest = digestPointer(lineage[2].Record.Activated.InitialJournalTailDigest)
	planned := reserved
	planned.JournalIdentityDigest = DigestBytes([]byte("successor-journal"))
	planned.PlannedSegment0Header.JournalIdentityDigest = planned.JournalIdentityDigest
	planned.ExpectedSegment0HeaderDigest = projectionTestDigest
	planned.QuotaReservationDigest, _ = QuotaReservationDigest(planned)
	planned.PlannedSegment0Header.QuotaReservationDigest = planned.QuotaReservationDigest
	superseded.PlannedGenerationReserved = &planned
	lineage = append(lineage, LineageIndexFrame{FormatVersion: LineageFrameFormat, RecordKind: LineageRecordGenerationSuperseded, Record: LineageIndexRecord{Superseded: &superseded}})
	redigestStructuralLineageFrames(t, lineage)
	journalID := journal[0].Record.Header.JournalIdentityDigest
	actual := map[Digest]EvidenceFrame{journalID: journal[0]}
	if _, err := validateLineageChainStructure(lineage, actual, map[Digest][][]EvidenceFrame{journalID: {journal}}); err == nil {
		t.Fatal("accepted activated-no-progress with journal progress after the header")
	}
}
