package migration

import (
	"crypto/sha256"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// verifiedAdmissionGenerationReplay is the bounded target-generation output
// of ALL-history structural replay plus same-verifier verification. It owns no
// filesystem handle and cannot append; a later handoff must still compare its
// exact file facts with a fresh evidencefs GenerationSnapshot.
type verifiedAdmissionGenerationReplay struct {
	indexFact           evidencefs.GenerationFileFact
	segmentFacts        []evidencefs.GenerationFileFact
	segmentRecords      []uint64
	cursor              JournalCursor
	recovery            *RecoverySnapshot
	reservation         evidenceQuotaReservation
	schema              verifiedRecoverySchemaWitness
	journalRecords      uint64
	journalBytes        uint64
	checkpointRecords   uint64
	indexDebitRecords   uint64
	indexDebitBytes     uint64
	indexHeaderDebited  bool
	supersessionDebited bool
	canonical           [32]byte
}

func bindVerifiedAdmissionGenerationReplay(lineage admissionReplayLineage, generation *admissionReplayGeneration, descriptor GenerationDescriptor, facts *admissionHistoricalVerificationFacts) (*verifiedAdmissionGenerationReplay, error) {
	return bindVerifiedAdmissionGenerationReplayMode(lineage, generation, descriptor, facts, false)
}

// bindVerifiedSupersededAdmissionGenerationReplay reconstructs the durable A
// journal boundary for historical A -> B authorization. Unlike the ordinary
// target-generation replay, it includes the already-durable Superseded index
// debit and can never enter the registered-generation handoff path.
func bindVerifiedSupersededAdmissionGenerationReplay(lineage admissionReplayLineage, generation *admissionReplayGeneration, descriptor GenerationDescriptor, facts *admissionHistoricalVerificationFacts) (*verifiedAdmissionGenerationReplay, error) {
	if generation == nil || generation.supersessionRecordDigest == nil || generation.plannedSuccessor == nil || lineage.state != admissionLineageSuperseded {
		return nil, admissionCorrupt("admission-supersession-replay", "superseded generation boundary is incomplete", nil)
	}
	return bindVerifiedAdmissionGenerationReplayMode(lineage, generation, descriptor, facts, true)
}

func bindVerifiedAdmissionGenerationReplayMode(lineage admissionReplayLineage, generation *admissionReplayGeneration, descriptor GenerationDescriptor, facts *admissionHistoricalVerificationFacts, allowSuperseded bool) (*verifiedAdmissionGenerationReplay, error) {
	if generation == nil || generation.activationRecordDigest == nil || generation.supersessionRecordDigest != nil && !allowSuperseded {
		return nil, nil
	}
	if allowSuperseded && (generation.supersessionRecordDigest == nil || lineage.indexTailRecordDigest != *generation.supersessionRecordDigest) {
		return nil, admissionCorrupt("admission-supersession-replay", "superseded index tail is not exact", nil)
	}
	if generation.header == nil || generation.summary == nil || generation.runtimeInspection == nil || !validAdmissionRecoveryFacts(facts) || descriptor.identity.owner == nil || descriptor.header.Validate() != nil || !sameGenerationHeader(descriptor.identity, descriptor.header) || descriptor.identity.executionLineageDigest != digestString(lineage.id) || lineage.indexRecords == 0 || lineage.indexTailRecordDigest.Validate() != nil || lineage.index.size == 0 || lineage.index.digest == ([32]byte{}) || lineage.index.identity == ([32]byte{}) {
		return nil, admissionCorrupt("admission-target-replay", "target generation replay facts are incomplete", nil)
	}
	var journal *admissionReplayJournal
	journalID := digestRaw(generation.journalID)
	for index := range lineage.journals {
		if lineage.journals[index].id == journalID {
			if journal != nil {
				return nil, admissionCorrupt("admission-target-replay", "target generation journal is duplicated", nil)
			}
			journal = &lineage.journals[index]
		}
	}
	if journal == nil || len(journal.segments) == 0 || journal.records == 0 || journal.tail != descriptor.replayTailDigest || uint64(len(journal.segments)) > uint64(^uint32(0)) {
		return nil, admissionCorrupt("admission-target-replay", "target generation journal boundary is incomplete", nil)
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	previous := journal.tail
	cursor := JournalCursor{
		owner: descriptor.identity.owner, generation: descriptor.identity, segmentIndex: uint32(len(journal.segments) - 1),
		nextSequence: journal.records, previousRecordDigest: &previous,
		lineageIndexNextSequence: lineage.indexRecords, lineageIndexPreviousRecordDigest: lineage.indexTailRecordDigest,
		latestCheckpointRecordDigest: cloneDigestPointer(generation.latestCheckpointRecordDigest), valid: valid,
	}
	schema, err := admissionRecoverySchemaForGeneration(generation, facts, descriptor.identity)
	if err != nil {
		cursor.valid.Store(false)
		return nil, err
	}
	continuation, err := admissionRecoveredContinuation(generation, descriptor.identity, cursor, journal.tail)
	if err != nil {
		cursor.valid.Store(false)
		return nil, err
	}
	recovery, err := buildRecoverySnapshotFromTail(descriptor.header, generation.currentTail, cursor, descriptor.identity, continuation, schema)
	if err != nil || !admissionRecoverySnapshotMatchesSummary(recovery, generation.summary) {
		cursor.valid.Store(false)
		return nil, admissionCorrupt("admission-target-replay", "target recovery snapshot differs from verified journal summary", err)
	}
	replay := &verifiedAdmissionGenerationReplay{
		indexFact:    evidencefs.GenerationFileFact{Ordinal: lineage.index.ordinal, Size: lineage.index.size, ContentDigest: lineage.index.digest, IdentityDigest: lineage.index.identity},
		segmentFacts: make([]evidencefs.GenerationFileFact, len(journal.segments)), segmentRecords: make([]uint64, len(journal.segments)),
		cursor: cursor, recovery: recovery, reservation: generation.runtimeInspection.reservation, schema: cloneGenerationJournalSchema(schema),
		journalRecords:      journal.records,
		indexHeaderDebited:  generation.indexHeaderDebited,
		supersessionDebited: allowSuperseded,
	}
	for index, segment := range journal.segments {
		replay.segmentFacts[index] = evidencefs.GenerationFileFact{Ordinal: segment.file.ordinal, Size: segment.file.size, ContentDigest: segment.file.digest, IdentityDigest: segment.file.identity}
		replay.segmentRecords[index] = segment.records
		var addErr error
		replay.journalBytes, addErr = admissionCheckedAdd(replay.journalBytes, segment.file.size)
		if addErr != nil {
			cursor.valid.Store(false)
			return nil, addErr
		}
	}
	for _, debit := range generation.indexDebits {
		var addErr error
		replay.indexDebitRecords, addErr = admissionCheckedAdd(replay.indexDebitRecords, 1)
		if addErr == nil {
			replay.indexDebitBytes, addErr = admissionCheckedAdd(replay.indexDebitBytes, debit.framedBytes)
		}
		if addErr != nil {
			cursor.valid.Store(false)
			return nil, addErr
		}
		if debit.kind == LineageRecordGenerationCheckpoint {
			replay.checkpointRecords++
		}
	}
	if generation.indexHeaderDebited {
		var addErr error
		replay.indexDebitRecords, addErr = admissionCheckedAdd(replay.indexDebitRecords, 1)
		if addErr == nil {
			replay.indexDebitBytes, addErr = admissionCheckedAdd(replay.indexDebitBytes, lineage.indexHeaderFramedBytes)
		}
		if addErr != nil {
			cursor.valid.Store(false)
			return nil, addErr
		}
	}
	replay.canonical = verifiedAdmissionGenerationReplayDigest(replay, descriptor.identity)
	if !validVerifiedAdmissionGenerationReplay(replay, descriptor.identity) {
		cursor.valid.Store(false)
		return nil, admissionCorrupt("admission-target-replay", "target generation replay could not be sealed", nil)
	}
	return replay, nil
}

func admissionRecoverySchemaForGeneration(generation *admissionReplayGeneration, facts *admissionHistoricalVerificationFacts, identity generationIdentity) (verifiedRecoverySchemaWitness, error) {
	if generation == nil || identity.owner == nil || !validAdmissionRecoveryFacts(facts) {
		return verifiedRecoverySchemaWitness{}, admissionCorrupt("admission-target-replay", "recovery schema facts are unavailable", nil)
	}
	schema := verifiedRecoverySchemaWitness{
		owner: identity.owner, generation: identity,
		finalStatementIndex: make(map[string]uint32, len(facts.orderedMigrations)), maxAttempts: make(map[string]uint32, len(facts.orderedMigrations)),
		orderedMigrations: append([]string(nil), facts.orderedMigrations...), signedExpectedLedgerRows: cloneProjectionValue(facts.ledgerRows),
	}
	for _, migration := range facts.orderedMigrations {
		subjects := facts.statementSubjects[migration]
		if len(subjects) == 0 {
			return verifiedRecoverySchemaWitness{}, admissionCorrupt("admission-target-replay", "recovery schema statement closure is empty", nil)
		}
		schema.finalStatementIndex[migration] = uint32(len(subjects) - 1)
		schema.maxAttempts[migration] = facts.maxAttempts
	}
	var err error
	schema.signedExpectedLedgerDigest, err = LedgerPrefixDigest(schema.signedExpectedLedgerRows)
	if err != nil {
		return verifiedRecoverySchemaWitness{}, err
	}
	committed := make(map[string]bool, len(facts.orderedMigrations))
	for _, event := range generation.verificationTerminals {
		migration := admissionMigrationString(event.migrationID)
		if migration == "" {
			return verifiedRecoverySchemaWitness{}, admissionCorrupt("admission-target-replay", "terminal migration identity is invalid", nil)
		}
		if event.outcome == 1 || event.outcome == 4 || event.outcome == 7 && event.resolutionOutcome == 1 {
			committed[migration] = true
		}
	}
	committedCount := 0
	for _, migration := range facts.orderedMigrations {
		if !committed[migration] {
			break
		}
		committedCount++
	}
	for _, migration := range facts.orderedMigrations[committedCount:] {
		if committed[migration] {
			return verifiedRecoverySchemaWitness{}, admissionCorrupt("admission-target-replay", "committed migrations are not an ordered prefix", nil)
		}
	}
	schema.durableObservedLedgerPrefix = cloneProjectionValue(schema.signedExpectedLedgerRows[:committedCount])
	schema.durableObservedLedgerDigest, err = LedgerPrefixDigest(schema.durableObservedLedgerPrefix)
	if err != nil {
		return verifiedRecoverySchemaWitness{}, err
	}
	last := facts.orderedMigrations[len(facts.orderedMigrations)-1]
	schema.finalCatalogDigest = digestString(facts.finalCatalogDigest[last])
	if generationJournalSchemaDigest(schema, identity) == ([32]byte{}) {
		return verifiedRecoverySchemaWitness{}, admissionCorrupt("admission-target-replay", "recovery schema could not be sealed", nil)
	}
	return schema, nil
}

func admissionMigrationString(value uint32) string {
	if value > 999999 {
		return ""
	}
	var raw [6]byte
	for index := len(raw) - 1; index >= 0; index-- {
		raw[index] = byte('0' + value%10)
		value /= 10
	}
	return string(raw[:])
}

func admissionRecoveredContinuation(generation *admissionReplayGeneration, identity generationIdentity, cursor JournalCursor, tail Digest) (recoveredContinuation, error) {
	if generation == nil || generation.continuation == nil {
		return recoveredContinuation{}, nil
	}
	value := LineageContinuationContext{
		StartAction: generation.continuation.startAction, MigrationID: generation.continuation.migrationID, AttemptIndex: generation.continuation.attemptIndex,
		PreviousAttemptTerminalDigest: cloneDigestPointer(generation.continuation.previousAttemptTerminalDigest),
		SourceJournalIdentityDigest:   generation.continuation.sourceJournalIdentityDigest, SourceCheckpointRecordDigest: generation.continuation.sourceCheckpointRecordDigest, SourceTerminalDigest: generation.continuation.sourceTerminalDigest,
	}
	if err := value.Validate(); err != nil || generation.reservedRecordDigest.Validate() != nil {
		return recoveredContinuation{}, admissionCorrupt("admission-target-replay", "lineage continuation is invalid", err)
	}
	return recoveredContinuation{owned: recoveredValue(identity, cursor, tail, generation.reservedRecordDigest, value)}, nil
}

func admissionRecoverySnapshotMatchesSummary(snapshot *RecoverySnapshot, summary *evidenceJournalSummary) bool {
	if snapshot == nil || summary == nil || !equalStringPointer(snapshot.migrationID, summary.migrationID) || !equalUint32Pointer(snapshot.attemptIndex, summary.attemptIndex) || !equalDigestPointer(snapshot.lastStatementIntentRecordDigest, summary.lastStatementIntentRecordDigest) || !equalDigestPointer(snapshot.lastIntermediateEvidenceRecordDigest, summary.lastIntermediateEvidenceRecordDigest) || !equalDigestPointer(snapshot.lastCommitIntentRecordDigest, summary.lastCommitIntentRecordDigest) || !equalDigestPointer(snapshot.lastTerminalDigest, summary.lastTerminalDigest) || !equalDigestPointer(snapshot.lastResolutionDigest, summary.lastResolutionDigest) || !equalDigestPointer(snapshot.previousAttemptTerminalDigest, summary.previousAttemptTerminalDigest) || !equalDigestPointer(snapshot.lastIntermediateStateDigest, summary.lastIntermediateStateDigest) {
		return false
	}
	state := string(snapshot.state)
	if snapshot.state == RecoveryBrandNewInherited {
		state = string(RecoveryBrandNew)
	} else if snapshot.state == RecoveryTerminal && snapshot.nextPermittedAction == RecoveryBeginFirstAttemptNextEntry {
		state = string(RecoveryCompleted)
	}
	return state == summary.recoveryState
}

func cloneVerifiedAdmissionGenerationReplay(value *verifiedAdmissionGenerationReplay) *verifiedAdmissionGenerationReplay {
	if value == nil {
		return nil
	}
	result := *value
	result.segmentFacts = append([]evidencefs.GenerationFileFact(nil), value.segmentFacts...)
	result.segmentRecords = append([]uint64(nil), value.segmentRecords...)
	result.cursor = value.cursor.clone()
	result.recovery = cloneRecoverySnapshot(value.recovery)
	result.schema = cloneGenerationJournalSchema(value.schema)
	return &result
}

func verifiedAdmissionGenerationReplayDigest(value *verifiedAdmissionGenerationReplay, identity generationIdentity) [32]byte {
	if value == nil || identity.owner == nil || len(value.segmentFacts) == 0 || len(value.segmentFacts) != len(value.segmentRecords) || !value.cursor.Valid() || !sameGenerationIdentity(value.cursor.generation, identity) || !validRecoverySnapshotForJournal(value.recovery, identity, value.cursor) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-verified-admission-generation-replay/v1\x00"))
	for _, digest := range []Digest{identity.executionLineageDigest, identity.journalIdentityDigest, identity.runnerProjectionDecisionDigest, identity.schemaBundleDigest} {
		writeAdmissionString(h, digest.String())
	}
	writeGenerationFileFact(h, value.indexFact)
	writeAdmissionUint(h, uint64(len(value.segmentFacts)))
	for index, fact := range value.segmentFacts {
		writeGenerationFileFact(h, fact)
		writeAdmissionUint(h, value.segmentRecords[index])
	}
	writeGenerationJournalCursor(h, value.cursor)
	recoveryDigest := generationJournalRecoveryDigest(value.recovery)
	schemaDigest := generationJournalSchemaDigest(value.schema, identity)
	if recoveryDigest == ([32]byte{}) || schemaDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h.Write(recoveryDigest[:])
	h.Write(schemaDigest[:])
	writeAdmissionReservation(h, value.reservation)
	for _, count := range []uint64{value.journalRecords, value.journalBytes, value.checkpointRecords, value.indexDebitRecords, value.indexDebitBytes} {
		writeAdmissionUint(h, count)
	}
	if value.indexHeaderDebited {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	if value.supersessionDebited {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func validVerifiedAdmissionGenerationReplay(value *verifiedAdmissionGenerationReplay, identity generationIdentity) bool {
	if value == nil || identity.owner == nil {
		return false
	}
	expectedIndexDebits := uint64(2) + value.checkpointRecords
	if value.indexHeaderDebited {
		expectedIndexDebits++
	}
	if value.supersessionDebited {
		expectedIndexDebits++
	}
	if len(value.segmentFacts) == 0 || len(value.segmentFacts) != len(value.segmentRecords) || value.indexFact.Ordinal != 0 || value.indexFact.Size == 0 || value.indexFact.ContentDigest == ([32]byte{}) || value.indexFact.IdentityDigest == ([32]byte{}) || !value.cursor.Valid() || value.cursor.owner != identity.owner || !sameGenerationIdentity(value.cursor.generation, identity) || value.cursor.previousRecordDigest == nil || value.cursor.previousRecordDigest.Validate() != nil || value.cursor.lineageIndexNextSequence == 0 || value.cursor.lineageIndexPreviousRecordDigest.Validate() != nil || value.cursor.nextSequence != value.journalRecords || value.cursor.segmentIndex != uint32(len(value.segmentFacts)-1) || (value.cursor.latestCheckpointRecordDigest == nil) != (value.checkpointRecords == 0) || value.cursor.latestCheckpointRecordDigest != nil && value.cursor.latestCheckpointRecordDigest.Validate() != nil || value.journalRecords == 0 || value.journalBytes == 0 || value.reservation.ReservedRecords == 0 || value.reservation.ReservedJournalBytes == 0 || value.reservation.ReservedSegments == 0 || value.reservation.ReservedCheckpointRecords != value.reservation.ReservedRecords-1 || value.reservation.ReservedBytes != value.reservation.ReservedJournalBytes+value.reservation.ReservedIndexBytes || value.journalRecords > value.reservation.ReservedRecords || value.journalBytes > value.reservation.ReservedJournalBytes || uint64(len(value.segmentFacts)) > uint64(value.reservation.ReservedSegments) || value.checkpointRecords > value.reservation.ReservedCheckpointRecords || value.indexDebitRecords != expectedIndexDebits || value.indexDebitRecords > value.reservation.ReservedIndexRecords || value.indexDebitBytes == 0 || value.indexDebitBytes > value.reservation.ReservedIndexBytes || !validRecoverySnapshotForJournal(value.recovery, identity, value.cursor) || generationJournalSchemaDigest(value.schema, identity) == ([32]byte{}) || value.canonical == ([32]byte{}) || value.canonical != verifiedAdmissionGenerationReplayDigest(value, identity) {
		return false
	}
	var records, bytes uint64
	for index, fact := range value.segmentFacts {
		if fact.Ordinal != uint32(index) || fact.Size == 0 || fact.ContentDigest == ([32]byte{}) || fact.IdentityDigest == ([32]byte{}) || value.segmentRecords[index] == 0 {
			return false
		}
		var err error
		records, err = admissionCheckedAdd(records, value.segmentRecords[index])
		if err == nil {
			bytes, err = admissionCheckedAdd(bytes, fact.Size)
		}
		if err != nil {
			return false
		}
	}
	return records == value.journalRecords && bytes == value.journalBytes && value.segmentRecords[len(value.segmentRecords)-1] != 0
}
