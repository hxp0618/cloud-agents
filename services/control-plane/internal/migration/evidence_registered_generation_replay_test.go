package migration

import (
	"crypto/sha256"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestVerifiedAdmissionGenerationReplayBindsCompactRecoveryAndPhysicalFacts(t *testing.T) {
	for name, frameCount := range map[string]int{"header-only": 1, "completed": 5} {
		t.Run(name, func(t *testing.T) {
			replay, identity, _, _, _, _ := registeredGenerationReplayFixture(t, frameCount)
			if validVerifiedAdmissionGenerationReplay(nil, identity) {
				t.Fatal("nil compact replay was accepted")
			}
			if !validVerifiedAdmissionGenerationReplay(replay, identity) || replay.cursor.nextSequence != uint64(frameCount) || replay.journalRecords != uint64(frameCount) || len(replay.segmentFacts) != 1 || len(replay.segmentRecords) != 1 || replay.segmentRecords[0] != uint64(frameCount) {
				t.Fatalf("compact replay is invalid: %+v", replay)
			}
			wantState, wantAction := RecoveryBrandNew, RecoveryBeginFirstAttempt
			if frameCount > 1 {
				wantState, wantAction = RecoveryCompleted, RecoveryReturnSuccess
			}
			if replay.recovery.State() != wantState || replay.recovery.NextAction() != wantAction {
				t.Fatalf("recovery=%s/%s want=%s/%s", replay.recovery.State(), replay.recovery.NextAction(), wantState, wantAction)
			}
		})
	}
}

func TestVerifiedAdmissionGenerationReplayRejectsEveryBoundFactMutation(t *testing.T) {
	replay, identity, _, _, _, _ := registeredGenerationReplayFixture(t, 5)
	mutations := map[string]func(*verifiedAdmissionGenerationReplay){
		"index":              func(v *verifiedAdmissionGenerationReplay) { v.indexFact.Size++ },
		"segment":            func(v *verifiedAdmissionGenerationReplay) { v.segmentFacts[0].Size++ },
		"segment records":    func(v *verifiedAdmissionGenerationReplay) { v.segmentRecords[0]++ },
		"cursor":             func(v *verifiedAdmissionGenerationReplay) { v.cursor.nextSequence++ },
		"recovery":           func(v *verifiedAdmissionGenerationReplay) { v.recovery.state = RecoveryDivergent },
		"recovered body":     func(v *verifiedAdmissionGenerationReplay) { v.recovery.lastTerminal.value.MigrationID = "999999" },
		"reservation":        func(v *verifiedAdmissionGenerationReplay) { v.reservation.ReservedRecords++ },
		"schema":             func(v *verifiedAdmissionGenerationReplay) { v.schema.finalCatalogDigest = testDigest("other-final") },
		"journal records":    func(v *verifiedAdmissionGenerationReplay) { v.journalRecords++ },
		"journal bytes":      func(v *verifiedAdmissionGenerationReplay) { v.journalBytes++ },
		"checkpoint records": func(v *verifiedAdmissionGenerationReplay) { v.checkpointRecords++ },
		"index records":      func(v *verifiedAdmissionGenerationReplay) { v.indexDebitRecords++ },
		"index bytes":        func(v *verifiedAdmissionGenerationReplay) { v.indexDebitBytes++ },
		"index header debit": func(v *verifiedAdmissionGenerationReplay) { v.indexHeaderDebited = true },
		"supersession debit": func(v *verifiedAdmissionGenerationReplay) { v.supersessionDebited = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			value := cloneVerifiedAdmissionGenerationReplay(replay)
			mutate(value)
			if validVerifiedAdmissionGenerationReplay(value, identity) || verifiedAdmissionGenerationReplayDigest(value, identity) == replay.canonical {
				t.Fatal("mutated compact replay retained its original authority")
			}
		})
	}
	copyValue := *replay
	copyValue.cursor = replay.cursor.clone()
	if copyValue.canonical == ([32]byte{}) || !validVerifiedAdmissionGenerationReplay(&copyValue, identity) {
		t.Fatal("ordinary nested replay copy should remain bound by its enclosing registered generation")
	}
}

func TestVerifiedSupersededAdmissionGenerationReplayBindsOnlyExactPendingBoundary(t *testing.T) {
	_, identity, lineage, generation, descriptor, facts := registeredGenerationReplayFixture(t, 5)
	superseded := testDigest("target-replay-superseded")
	planned := plannedSuccessorReplayFixture(generation)
	generation.supersessionRecordDigest = &superseded
	generation.plannedSuccessor = &planned
	generation.indexDebits = append(generation.indexDebits, admissionReplayIndexDebit{kind: LineageRecordGenerationSuperseded, recordDigest: superseded, framedBytes: 17})
	lineage.state = admissionLineageSuperseded
	lineage.indexRecords++
	lineage.indexTailRecordDigest = superseded
	lineage.generations = []admissionReplayGeneration{cloneAdmissionGeneration(generation)}

	replay, err := bindVerifiedSupersededAdmissionGenerationReplay(lineage, &generation, descriptor, facts)
	if err != nil || replay == nil || !replay.supersessionDebited || !validVerifiedAdmissionGenerationReplay(replay, identity) || replay.indexDebitRecords != uint64(len(generation.indexDebits)) {
		t.Fatalf("superseded replay was not sealed: replay=%+v err=%v", replay, err)
	}
	if ordinary, err := bindVerifiedAdmissionGenerationReplay(lineage, &generation, descriptor, facts); err != nil || ordinary != nil {
		t.Fatalf("superseded replay entered ordinary binder: replay=%+v err=%v", ordinary, err)
	}

	for name, mutate := range map[string]func(*admissionReplayLineage, *admissionReplayGeneration){
		"state": func(l *admissionReplayLineage, _ *admissionReplayGeneration) {
			l.state = admissionLineageActiveCheckpointed
		},
		"tail": func(l *admissionReplayLineage, _ *admissionReplayGeneration) {
			l.indexTailRecordDigest = testDigest("other-tail")
		},
		"planned":    func(_ *admissionReplayLineage, g *admissionReplayGeneration) { g.plannedSuccessor = nil },
		"superseded": func(_ *admissionReplayLineage, g *admissionReplayGeneration) { g.supersessionRecordDigest = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidateLineage := lineage
			candidateGeneration := cloneAdmissionGeneration(generation)
			mutate(&candidateLineage, &candidateGeneration)
			if value, err := bindVerifiedSupersededAdmissionGenerationReplay(candidateLineage, &candidateGeneration, descriptor, facts); value != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
				t.Fatalf("invalid superseded boundary was accepted: replay=%+v err=%v", value, err)
			}
		})
	}
}

func TestVerifiedMaterializedSupersededAdmissionGenerationReplayBindsExactDurableSuccessor(t *testing.T) {
	_, identity, lineage, source, descriptor, facts := registeredGenerationReplayFixture(t, 5)
	planned := plannedSuccessorReplayFixture(source)
	superseded := testDigest("materialized-source-superseded")
	source.supersessionRecordDigest = &superseded
	source.plannedSuccessor = &planned
	source.indexDebits = append(source.indexDebits, admissionReplayIndexDebit{kind: LineageRecordGenerationSuperseded, recordDigest: superseded, framedBytes: 17})

	actual := cloneAdmissionGeneration(planned)
	reserved := testDigest("materialized-successor-reserved")
	activation := testDigest("materialized-successor-activation")
	actual.reservedRecordDigest = reserved
	actual.activationRecordDigest = &activation
	actual.indexDebits = []admissionReplayIndexDebit{
		{kind: LineageRecordGenerationReserved, recordDigest: reserved, framedBytes: 19},
		{kind: LineageRecordGenerationActivated, recordDigest: activation, framedBytes: 23},
	}
	lineage.state = admissionLineageActiveInitial
	lineage.indexRecords += 3
	lineage.indexTailRecordDigest = activation
	lineage.generations = []admissionReplayGeneration{cloneAdmissionGeneration(source), cloneAdmissionGeneration(actual)}
	sourceGeneration := &lineage.generations[0]
	actualSuccessor := &lineage.generations[1]

	if !admissionSuccessorReservationMatches(lineage.id, &planned, &actual) || !materializedAdmissionSuccessorMatches(lineage.id, &planned, &actual) {
		t.Fatal("byte-exact durable successor was not recognized")
	}
	if !materializedAdmissionSuccessorIsAdjacent(lineage, sourceGeneration, actualSuccessor) {
		t.Fatal("durable successor was not exact adjacent transcript state")
	}
	replay, err := bindVerifiedMaterializedSupersededAdmissionGenerationReplay(lineage, sourceGeneration, actualSuccessor, descriptor, facts)
	if err != nil || replay == nil || !replay.supersessionDebited || !validVerifiedAdmissionGenerationReplay(replay, identity) || replay.cursor.lineageIndexNextSequence != lineage.indexRecords || replay.cursor.lineageIndexPreviousRecordDigest != activation {
		t.Fatalf("materialized superseded replay was not sealed: replay=%+v err=%v", replay, err)
	}
	if pending, err := bindVerifiedSupersededAdmissionGenerationReplay(lineage, sourceGeneration, descriptor, facts); pending != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("materialized successor re-entered pending-tail binder: replay=%+v err=%v", pending, err)
	}
	if value, err := bindVerifiedMaterializedSupersededAdmissionGenerationReplay(lineage, &source, actualSuccessor, descriptor, facts); value != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("detached source clone entered materialized binder: replay=%+v err=%v", value, err)
	}

	unactivated := cloneAdmissionGeneration(actual)
	unactivated.activationRecordDigest = nil
	if !admissionSuccessorReservationMatches(lineage.id, &planned, &unactivated) || materializedAdmissionSuccessorMatches(lineage.id, &planned, &unactivated) {
		t.Fatal("unactivated adjacent reservation was classified as fully materialized")
	}
	unactivatedLineage := lineage
	unactivatedLineage.generations = []admissionReplayGeneration{cloneAdmissionGeneration(source), unactivated}
	if value, err := bindVerifiedMaterializedSupersededAdmissionGenerationReplay(unactivatedLineage, &unactivatedLineage.generations[0], &unactivatedLineage.generations[1], descriptor, facts); value != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("unactivated successor entered materialized binder: replay=%+v err=%v", value, err)
	}

	drifted := cloneAdmissionGeneration(actual)
	drifted.reservedBytes++
	if admissionSuccessorReservationMatches(lineage.id, &planned, &drifted) || materializedAdmissionSuccessorMatches(lineage.id, &planned, &drifted) {
		t.Fatal("mutated adjacent reservation matched stored supersession")
	}
	driftedLineage := lineage
	driftedLineage.generations = []admissionReplayGeneration{cloneAdmissionGeneration(source), drifted}
	if value, err := bindVerifiedMaterializedSupersededAdmissionGenerationReplay(driftedLineage, &driftedLineage.generations[0], &driftedLineage.generations[1], descriptor, facts); value != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("mutated successor entered materialized binder: replay=%+v err=%v", value, err)
	}
}

func TestVerifiedAdmissionGenerationReplayRejectsSummaryAndTailDrift(t *testing.T) {
	_, _, lineage, generation, descriptor, facts := registeredGenerationReplayFixture(t, 5)
	mutatedSummary := cloneAdmissionGeneration(generation)
	mutatedSummary.summary.lastTerminalDigest = digestPointer(testDigest("wrong-terminal"))
	if replay, err := bindVerifiedAdmissionGenerationReplay(lineage, &mutatedSummary, descriptor, facts); replay != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("summary drift replay=%+v err=%v", replay, err)
	}
	mutatedTail := cloneAdmissionGeneration(generation)
	mutatedTail.currentTail.terminal.body.MigrationID = "999999"
	if replay, err := bindVerifiedAdmissionGenerationReplay(lineage, &mutatedTail, descriptor, facts); replay != nil || !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("typed tail drift replay=%+v err=%v", replay, err)
	}
}

func TestVerifiedAdmissionGenerationReplayAuthorityDoesNotSpread(t *testing.T) {
	t.Parallel()
	allowed := map[string]map[string]bool{
		"verifiedAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go":  true,
			"evidence_admission_history.go":             true,
			"evidence_registered_generation_handoff.go": true,
			"evidence_generation_journal.go":            true,
		},
		"bindVerifiedAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go": true,
			"evidence_admission_history.go":            true,
		},
		"bindVerifiedSupersededAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go": true,
			"evidence_admission_history.go":            true,
		},
		"bindVerifiedMaterializedSupersededAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go": true,
			"evidence_admission_history.go":            true,
		},
		"materializedAdmissionSuccessorIsAdjacent": {
			"evidence_registered_generation_replay.go": true,
			"evidence_admission_history.go":            true,
		},
		"retainMaterializedAdmissionHistoryGeneration": {
			"evidence_admission_history.go": true,
		},
		"verifyMaterializedHistoricalSupersession": {
			"evidence_admission_history.go": true,
		},
		"cloneVerifiedAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go":  true,
			"evidence_admission_history.go":             true,
			"evidence_registered_generation_handoff.go": true,
		},
		"verifiedAdmissionGenerationReplayDigest": {
			"evidence_registered_generation_replay.go":  true,
			"evidence_registered_generation_handoff.go": true,
		},
		"validVerifiedAdmissionGenerationReplay": {
			"evidence_registered_generation_replay.go":  true,
			"evidence_admission_history.go":             true,
			"evidence_registered_generation_handoff.go": true,
			"evidence_generation_journal.go":            true,
		},
		"buildRecoverySnapshotFromTail": {
			"evidence_registered_generation_replay.go": true,
			"evidence_recovery.go":                     true,
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
			files, guarded := allowed[identifier.Name]
			if guarded && !files[name] {
				t.Fatalf("registered generation replay authority %s spread into %s", identifier.Name, name)
			}
			return true
		})
	}
}

func plannedSuccessorReplayFixture(generation admissionReplayGeneration) admissionReplayGeneration {
	planned := cloneAdmissionGeneration(generation)
	planned.reservedRecordDigest = ""
	planned.activationRecordDigest = nil
	planned.latestCheckpointRecordDigest = nil
	planned.latestCheckpointTailDigest = nil
	planned.latestCheckpointNext = 0
	planned.indexDebits = nil
	planned.summary = nil
	planned.currentTail = nil
	planned.verificationTerminals = nil
	planned.verificationFinals = nil
	planned.verificationCommits = nil
	planned.verificationRetries = nil
	planned.verificationResolutions = nil
	planned.verificationOpen = nil
	planned.supersessionRecordDigest = nil
	planned.plannedSuccessor = nil
	return planned
}

func registeredGenerationReplayFixture(t *testing.T, frameCount int) (*verifiedAdmissionGenerationReplay, generationIdentity, admissionReplayLineage, admissionReplayGeneration, GenerationDescriptor, *admissionHistoricalVerificationFacts) {
	t.Helper()
	document := fixtureObject(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"))
	allFrames := decodeEvidenceFrames(t, document["frames"])
	if frameCount < 1 || frameCount > len(allFrames) {
		t.Fatalf("invalid frame count %d", frameCount)
	}
	frames := cloneProjectionValue(allFrames[:frameCount])
	header := cloneProjectionValue(*allFrames[0].Record.Header)
	context := fixtureObjectValue(t, document["validation_context"], "validation context")
	witness := buildEvidenceWitness(t, allFrames, context)
	intent := cloneProjectionValue(*allFrames[1].Record.StatementIntent)
	commit := cloneProjectionValue(*allFrames[3].Record.CommitIntent)
	statementSubject, err := admissionStatementPlanSubject(intent)
	if err != nil {
		t.Fatal(err)
	}
	migration := intent.MigrationID
	facts := &admissionHistoricalVerificationFacts{
		maxAttempts: witness.maxAttempts[migration], manifestDigest: header.ManifestDigest,
		runnerProjectionDecisionDigest: header.RunnerProjectionDecisionDigest, schemaBundleDigest: header.SchemaBundleDigest,
		authorityProfileDigest: header.AuthorityProfileDigest, authorityBindingDigest: header.AuthorityBindingDigest,
		orderedMigrations: []string{migration}, statementSubjects: map[string][][32]byte{migration: {statementSubject}},
		finalCatalogDigest:        map[string][32]byte{migration: digestRaw(witness.finalCatalogDigest[migration])},
		catalogContractDigest:     map[string][32]byte{migration: digestRaw(intent.CatalogContractDigest)},
		attemptPredecessorCatalog: map[string][32]byte{migration: digestRaw(commit.AttemptPredecessorCatalogDigest)},
		ledgerRows:                []CommitIntentLedgerRow{cloneProjectionValue(commit.LedgerRow)},
	}
	if !validAdmissionRecoveryFacts(facts) {
		t.Fatal("replay fixture facts are invalid")
	}
	reservation, err := calculateEvidenceQuotaReservationFromArithmeticFacts(quotaBundleArithmeticFacts{uint64(facts.maxAttempts), []uint64{1}}, false)
	if err != nil {
		t.Fatal(err)
	}
	collector := &admissionReplayJournalCollector{}
	var tail *admissionReplayRecoveryTail
	for index := 1; index < len(frames); index++ {
		if err := collector.observe(frames[index]); err != nil {
			t.Fatal(err)
		}
		if tail == nil {
			tail = &admissionReplayRecoveryTail{}
		}
		if err := tail.observe(frames[index]); err != nil {
			t.Fatal(err)
		}
	}
	if err := collector.validate(); err != nil {
		t.Fatal(err)
	}
	summary, err := summarizeEvidenceJournal(frames)
	if err != nil {
		t.Fatal(err)
	}
	activation := testDigest("target-replay-activation")
	checkpoint := testDigest("target-replay-checkpoint")
	headerFacts := compactAdmissionHeaderFacts(header)
	indexDebits := []admissionReplayIndexDebit{{kind: LineageRecordGenerationReserved, recordDigest: testDigest("target-replay-reserved"), framedBytes: 16}, {kind: LineageRecordGenerationActivated, recordDigest: activation, framedBytes: 16}}
	indexRecords := uint64(3)
	indexTail := activation
	var latestCheckpoint, latestCheckpointTail *Digest
	var latestCheckpointNext uint64
	if frameCount > 1 {
		indexDebits = append(indexDebits, admissionReplayIndexDebit{kind: LineageRecordGenerationCheckpoint, recordDigest: checkpoint, framedBytes: 16})
		indexRecords++
		latestCheckpoint, latestCheckpointTail, latestCheckpointNext = &checkpoint, digestPointer(frames[len(frames)-1].RecordDigest), uint64(frameCount)
		indexTail = checkpoint
	}
	generation := admissionReplayGeneration{
		journalID: header.JournalIdentityDigest, reservedRecordDigest: testDigest("target-replay-reserved"),
		runnerProjectionDecisionDigest: header.RunnerProjectionDecisionDigest, schemaBundleDigest: header.SchemaBundleDigest,
		quotaReservationDigest: header.QuotaReservationDigest, reservedRecords: header.ReservedRecords, reservedBytes: header.ReservedBytes, reservedSegments: header.ReservedSegments,
		expectedSegment0HeaderDigest: allFrames[0].RecordDigest, activationRecordDigest: &activation,
		latestCheckpointRecordDigest: latestCheckpoint, latestCheckpointTailDigest: latestCheckpointTail, latestCheckpointNext: latestCheckpointNext,
		indexDebits: indexDebits,
		header:      &headerFacts, summary: &summary, currentTail: cloneAdmissionRecoveryTail(tail),
		verificationTerminals: append([]admissionReplayTerminalEvent(nil), collector.terminals...), verificationFinals: append([]admissionReplayTerminalFinal(nil), collector.finals...),
		verificationCommits: append([]admissionReplayTerminalCommit(nil), collector.commits...), verificationRetries: append([]admissionReplayTerminalRetry(nil), collector.retries...),
		verificationResolutions: append([]admissionReplayTerminalResolution(nil), collector.resolutions...), verificationOpen: collector.openAttempt(),
		verificationCatalogContract: collector.catalogContract,
		runtimeInspection:           &admissionReplayRuntimeInspection{manifestDigest: header.ManifestDigest, schemaBundleDigest: header.SchemaBundleDigest, maxAttempts: uint64(facts.maxAttempts), statementCounts: []uint64{1}, reservation: reservation},
	}
	if err := verifyAdmissionGeneration(&generation, facts); err != nil {
		t.Fatalf("fixture verification: %v", err)
	}
	var raw []byte
	for _, frame := range frames {
		framed, err := EncodeCanonicalEvidenceFrame(frame)
		if err != nil {
			t.Fatal(err)
		}
		raw = append(raw, framed...)
	}
	journalRaw := sha256.Sum256(raw)
	lineageID := digestRaw(header.ExecutionLineageDigest)
	journalID := digestRaw(header.JournalIdentityDigest)
	lineage := admissionReplayLineage{
		id: lineageID, index: admissionReplayFile{ordinal: 0, size: 512, digest: [32]byte{1}, identity: [32]byte{2}, handoffIdentity: [32]byte{12}}, indexRecords: indexRecords,
		indexHeaderFramedBytes: 17, indexTailRecordDigest: indexTail,
		journals:    []admissionReplayJournal{{id: journalID, segments: []admissionReplaySegment{{file: admissionReplayFile{ordinal: 0, size: uint64(len(raw)), digest: journalRaw, identity: [32]byte{3}, handoffIdentity: [32]byte{13}}, records: uint64(frameCount)}}, records: uint64(frameCount), tail: frames[len(frames)-1].RecordDigest}},
		generations: []admissionReplayGeneration{cloneAdmissionGeneration(generation)},
	}
	owner := &evidenceOwnerToken{nonce: [16]byte{77}}
	identity := generationIdentity{owner, header.ExecutionLineageDigest, header.JournalIdentityDigest, header.RunnerProjectionDecisionDigest, header.SchemaBundleDigest}
	descriptor := GenerationDescriptor{identity: identity, header: header, replayTailDigest: frames[len(frames)-1].RecordDigest, recoveryArtifactDigest: header.DecisionRecoveryArtifactSHA256, recoveryArtifactSize: header.DecisionRecoveryArtifactSizeBytes}
	replay, err := bindVerifiedAdmissionGenerationReplay(lineage, &generation, descriptor, facts)
	if err != nil {
		t.Fatal(err)
	}
	return replay, identity, lineage, generation, descriptor, facts
}
