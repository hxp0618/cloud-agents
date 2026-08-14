package migration

import (
	"context"
	"crypto/sha256"
	"sync"
	"sync/atomic"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

// generationEvidenceJournal is the first sealed normal-run journal authority.
// It retains one exact evidencefs generation lease but no database, runner,
// connection, transaction, or projection capability.
type generationEvidenceJournal struct {
	self             *generationEvidenceJournal
	mu               sync.Mutex
	prior            *GenerationRecoveryReady
	replay           *GenerationReplayReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	generation       generationIdentity
	reservation      evidenceQuotaReservation
	schema           verifiedRecoverySchemaWitness
	binding          *generationEvidenceJournalBinding
	state            *generationEvidenceJournalState
	closed           bool
}

type generationEvidenceJournalBinding struct {
	journal          *generationEvidenceJournal
	prior            *GenerationRecoveryReady
	replay           *GenerationReplayReady
	plan             *VerifiedAdmissionPlan
	history          *VerifiedAdmissionHistory
	candidateBinding *verifiedEvidenceRunBinding
	lease            *evidencefs.GenerationLease
	canonical        [32]byte
}

type generationEvidenceJournalState struct {
	self              *generationEvidenceJournalState
	journal           *generationEvidenceJournal
	snapshot          *evidencefs.GenerationSnapshot
	snapshotIdentity  [32]byte
	indexFact         evidencefs.GenerationFileFact
	segmentFacts      []evidencefs.GenerationFileFact
	cursor            JournalCursor
	recovery          *RecoverySnapshot
	journalRecords    uint64
	journalBytes      uint64
	segmentRecords    uint64
	segmentBytes      uint64
	checkpointRecords uint64
	indexDebitRecords uint64
	indexDebitBytes   uint64
	unknown           *generationJournalUnknownAppend
	canonical         [32]byte
}

type generationJournalUnknownAppend struct {
	filesystem evidencefs.GenerationAppendResult
	rotation   *evidencefs.GenerationRotationResult
	prepared   *preparedGenerationJournalAppend
}

type generationEvidenceJournalRegistryRecord struct {
	journal        *generationEvidenceJournal
	binding        *generationEvidenceJournalBinding
	prior          *GenerationRecoveryReady
	replay         *GenerationReplayReady
	lease          *evidencefs.GenerationLease
	state          *generationEvidenceJournalState
	canonical      [32]byte
	stateCanonical [32]byte
}

var generationEvidenceJournalRegistry sync.Map

var _ EvidenceJournal = (*generationEvidenceJournal)(nil)

// BindJournal consumes same-verifier recovery authority and mints the sealed
// EvidenceJournal implementation. It still does not mint ActiveGeneration,
// EvidenceSession, database, or runner authority.
func (r *GenerationRecoveryReady) BindJournal(ctx context.Context, candidate OwnedCurrentCandidate) (EvidenceJournal, error) {
	if r == nil || !validGenerationRecoveryReady(r, candidate) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-journal-bind", "generation recovery authority is unavailable", nil)
	}
	if err := contextAdmissionError(ctx); err != nil {
		return nil, err
	}
	if err := r.prior.snapshot.Revalidate(ctx); err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-journal-bind")
	}
	_, schema, err := buildBrandNewRecoveryWitness(candidate, r.generation, cloneAdmissionHistoricalVerificationFacts(r.history.currentFacts))
	if err != nil {
		return nil, err
	}
	state, err := initialGenerationJournalState(r)
	if err != nil {
		return nil, err
	}
	if !r.consumed.CompareAndSwap(false, true) {
		state.cursor.valid.Store(false)
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-journal-bind", "generation recovery authority is consumed", nil)
	}
	journal := &generationEvidenceJournal{
		prior: r, replay: r.prior, plan: r.plan, history: r.history, candidateBinding: candidate.binding,
		lease: r.prior.lease, generation: r.generation, reservation: r.history.reservation, schema: cloneGenerationJournalSchema(schema),
	}
	journal.self = journal
	journal.binding = &generationEvidenceJournalBinding{
		journal: journal, prior: r, replay: r.prior, plan: r.plan, history: r.history,
		candidateBinding: candidate.binding, lease: r.prior.lease,
	}
	journal.binding.canonical = generationEvidenceJournalDigest(journal)
	state.journal = journal
	state.self = state
	state.canonical = generationEvidenceJournalStateDigest(state)
	journal.state = state
	generationEvidenceJournalRegistry.Store(journal, generationEvidenceJournalRegistryRecord{
		journal: journal, binding: journal.binding, prior: r, replay: r.prior, lease: r.prior.lease,
		state: state, canonical: journal.binding.canonical, stateCanonical: state.canonical,
	})
	journal.mu.Lock()
	valid := journal.validLocked()
	journal.mu.Unlock()
	if !valid {
		generationEvidenceJournalRegistry.Delete(journal)
		generationRecoveryReadyRegistry.Delete(r)
		state.cursor.valid.Store(false)
		if cleanupErr := closeRegisteredGenerationReplay(r.prior, "generation-journal-bind-cleanup"); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, admissionFailed("generation-journal-bind", "journal authority could not be sealed", nil)
	}
	generationRecoveryReadyRegistry.Delete(r)
	return journal, nil
}

func initialGenerationJournalState(ready *GenerationRecoveryReady) (*generationEvidenceJournalState, error) {
	if ready == nil || ready.prior == nil || ready.prior.snapshot == nil || ready.plan == nil || ready.history == nil || ready.prior.prior == nil || ready.prior.prior.prior == nil || !validAdmissionRecoveryFacts(ready.history.currentFacts) || !validBrandNewRecoverySnapshot(ready.recovery, ready.generation, ready.cursor, ready.prior.journalTail) {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-journal-bind", "initial journal facts are unavailable", nil)
	}
	count, err := ready.prior.snapshot.SegmentCount()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-journal-bind")
	}
	if count != 1 {
		return nil, admissionCorrupt("generation-journal-bind", "brand-new generation has an invalid segment set", nil)
	}
	indexFact, err := ready.prior.snapshot.IndexFact()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-journal-bind")
	}
	segmentFact, err := ready.prior.snapshot.SegmentFact(0)
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-journal-bind")
	}
	identity, err := ready.prior.snapshot.IdentityDigest()
	if err != nil {
		return nil, mapEvidenceAdmissionError(err, "generation-journal-bind")
	}
	reservation := ready.history.reservation
	initialIndexRecords := uint64(2)
	initialIndexBytes := uint64(len(ready.plan.reservedFrameBytes) + len(ready.prior.prior.prior.activatedBytes))
	if !ready.history.rootFacts.targetIndexPresent {
		initialIndexRecords++
		initialIndexBytes += uint64(len(ready.plan.lineageHeaderBytes))
	}
	if identity == ([32]byte{}) || indexFact.Size == 0 || segmentFact.Size == 0 || ready.cursor.nextSequence != 1 || ready.cursor.segmentIndex != 0 || ready.cursor.lineageIndexNextSequence == 0 || reservation.ReservedRecords < 1 || reservation.ReservedCheckpointRecords != reservation.ReservedRecords-1 || reservation.ReservedJournalBytes < segmentFact.Size || reservation.ReservedSegments < 1 || reservation.ReservedIndexRecords < initialIndexRecords || reservation.ReservedIndexBytes < initialIndexBytes || reservation.ReservedBytes != reservation.ReservedJournalBytes+reservation.ReservedIndexBytes {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-journal-bind", "initial journal quota or cursor facts differ", nil)
	}
	return &generationEvidenceJournalState{
		snapshot: ready.prior.snapshot, snapshotIdentity: identity, indexFact: indexFact,
		segmentFacts: []evidencefs.GenerationFileFact{segmentFact}, cursor: ready.cursor.clone(), recovery: cloneRecoverySnapshot(ready.recovery),
		journalRecords: 1, journalBytes: segmentFact.Size, segmentRecords: 1, segmentBytes: segmentFact.Size,
		checkpointRecords: 0, indexDebitRecords: initialIndexRecords, indexDebitBytes: initialIndexBytes,
	}, nil
}

func (j *generationEvidenceJournal) Replay(ctx context.Context) (JournalCursor, *RecoverySnapshot, error) {
	if j == nil || j.self != j {
		return JournalCursor{}, nil, admissionFailed("generation-journal-replay", "journal authority is unavailable", nil)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := contextAdmissionError(ctx); err != nil {
		return JournalCursor{}, nil, err
	}
	if !j.validLocked() {
		return JournalCursor{}, nil, admissionFailed("generation-journal-replay", "journal authority is invalid", nil)
	}
	if j.state.unknown != nil {
		return j.reconcileUnknownLocked(ctx)
	}
	if err := j.state.snapshot.Revalidate(ctx); err != nil {
		return JournalCursor{}, nil, j.failLocked(err, "generation-journal-replay")
	}
	cursor := j.state.cursor.clone()
	return cursor, cloneRecoverySnapshot(j.state.recovery), nil
}

func (j *generationEvidenceJournal) AppendDurable(ctx context.Context, cursor JournalCursor, record *OwnedEvidenceRecord) (AppendResult, error) {
	if j == nil || j.self != j {
		return AppendResult{}, admissionFailed("generation-journal-append", "journal authority is unavailable", nil)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := contextAdmissionError(ctx); err != nil {
		return AppendResult{}, err
	}
	if !j.validLocked() || j.state.unknown != nil || !sameCursorIdentity(cursor, j.state.cursor) || !cursor.Valid() {
		return AppendResult{}, fail(CodeEvidenceRecoveryRequired, "generation-journal-append", "current journal cursor is unavailable", nil)
	}
	prepared, err := j.prepareAppendLocked(cursor, record)
	if err != nil {
		return AppendResult{}, err
	}
	consumed, err := record.consume(j.generation, cursor)
	if err != nil || !canonicalEqual(consumed, prepared.frame.Record) {
		prepared.invalidate()
		if err == nil {
			err = invalidEvidence("generation-journal-append", "consumed record differs from prepared candidate")
		}
		return AppendResult{}, err
	}
	if prepared.rotation != nil {
		return j.appendRotatedPreparedLocked(ctx, cursor, prepared)
	}
	filesystemResult, filesystemErr := j.lease.AppendExistingSegmentComposite(ctx, j.state.snapshot, prepared.framed, prepared.checkpointFramed)
	switch filesystemResult.Outcome() {
	case evidencefs.AdmissionTransitionPreMutationFailure:
		return j.restoreAfterPreMutationFailureLocked(prepared, filesystemErr)
	case evidencefs.AdmissionTransitionUnknown:
		return j.installUnknownLocked(prepared, filesystemResult, filesystemErr)
	case evidencefs.AdmissionTransitionDurable:
		if filesystemErr != nil || !filesystemResult.ValidFor(j.lease) {
			prepared.invalidate()
			j.state.cursor.valid.Store(false)
			return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-append-bind")
		}
	default:
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-append-outcome")
	}
	if filesystemResult.PreviousSnapshotIdentity() != j.state.snapshotIdentity || filesystemResult.JournalFramedDigest() != sha256.Sum256(prepared.framed) || filesystemResult.CheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) || filesystemResult.SegmentOrdinal() != j.state.cursor.segmentIndex || filesystemResult.JournalPreviousSize() != j.state.segmentBytes || filesystemResult.IndexPreviousSize() != j.state.indexFact.Size {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-append-bind")
	}
	nextState, err := j.nextDurableStateLocked(filesystemResult.Snapshot(), prepared)
	if err != nil {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(err, "generation-journal-append-state")
	}
	appendResult, err := finishConsumedAppend(cursor, j.generation, appendOutcomeDurable, &prepared.nextCursor, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest)
	if err != nil {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(err, "generation-journal-append-result")
	}
	if !j.installStateLocked(nextState) {
		prepared.invalidate()
		return AppendResult{}, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-append-seal")
	}
	return appendResult, nil
}

func (j *generationEvidenceJournal) Close(ctx context.Context) error {
	if j == nil || j.self != j {
		return admissionFailed("generation-journal-close", "journal authority is unavailable", nil)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	contextErr := contextAdmissionError(ctx)
	registered, ok := generationEvidenceJournalRegistry.Load(j)
	record, recordOK := registered.(generationEvidenceJournalRegistryRecord)
	if j.closed || !ok || !recordOK || record.journal != j || record.replay == nil || record.canonical == ([32]byte{}) {
		return admissionFailed("generation-journal-close", "immutable journal authority is unavailable", nil)
	}
	j.closed = true
	generationEvidenceJournalRegistry.Delete(j)
	if j.state != nil && j.state.cursor.valid != nil {
		j.state.cursor.valid.Store(false)
	}
	if j.state != nil && j.state.unknown != nil {
		j.state.unknown.prepared.invalidate()
	}
	if record.prior != nil {
		generationRecoveryReadyRegistry.Delete(record.prior)
	}
	if err := closeRegisteredGenerationReplay(record.replay, "generation-journal-close"); err != nil {
		return err
	}
	return contextErr
}

func (j *generationEvidenceJournal) evidenceJournalSealed() {}

type preparedGenerationJournalAppend struct {
	frame             EvidenceFrame
	framed            []byte
	checkpoint        LineageIndexFrame
	checkpointFramed  []byte
	nextCursor        JournalCursor
	previousRecovery  *RecoverySnapshot
	recovery          *RecoverySnapshot
	journalRecords    uint64
	journalBytes      uint64
	segmentRecords    uint64
	segmentBytes      uint64
	checkpointRecords uint64
	indexDebitRecords uint64
	indexDebitBytes   uint64
	rotation          *preparedGenerationJournalRotation
	canonical         [32]byte
}

type preparedGenerationJournalRotation struct {
	header                 EvidenceFrame
	headerFramed           []byte
	headerCheckpoint       LineageIndexFrame
	headerCheckpointFramed []byte
	headerCursor           JournalCursor
	headerRecovery         *RecoverySnapshot
	journalRecords         uint64
	journalBytes           uint64
	segmentRecords         uint64
	segmentBytes           uint64
	checkpointRecords      uint64
	indexDebitRecords      uint64
	indexDebitBytes        uint64
}

type preparedGenerationJournalProgress uint8

const (
	preparedGenerationJournalPrevious preparedGenerationJournalProgress = iota
	preparedGenerationJournalRotationHeader
	preparedGenerationJournalCandidate
)

func (p *preparedGenerationJournalAppend) invalidate() {
	if p != nil && p.nextCursor.valid != nil {
		p.nextCursor.valid.Store(false)
	}
	if p != nil && p.rotation != nil && p.rotation.headerCursor.valid != nil {
		p.rotation.headerCursor.valid.Store(false)
	}
}

func (j *generationEvidenceJournal) prepareAppendLocked(cursor JournalCursor, record *OwnedEvidenceRecord) (*preparedGenerationJournalAppend, error) {
	frames, chain, frame, err := inspectGenerationJournalRecord(record, j.generation, cursor)
	if err != nil {
		return nil, err
	}
	framed, err := EncodeCanonicalEvidenceFrame(frame)
	if err != nil {
		return nil, err
	}
	nextSegmentRecords, err := admissionCheckedAdd(j.state.segmentRecords, 1)
	if err != nil {
		return nil, err
	}
	nextSegmentBytes, err := admissionCheckedAdd(j.state.segmentBytes, uint64(len(framed)))
	if err != nil {
		return nil, err
	}
	if nextSegmentRecords > evidenceSegmentMaximumRecords || nextSegmentBytes > evidenceSegmentMaximumBytes {
		return j.prepareRotatedAppendLocked(cursor, frames, chain, frame)
	}
	summary, err := summarizeEvidenceJournal(frames)
	if err != nil {
		return nil, err
	}
	checkpoint, checkpointFramed, err := buildGenerationJournalCheckpoint(j.generation, cursor, frame, summary)
	if err != nil {
		return nil, err
	}
	nextCursor, err := advanceGenerationJournalCursor(cursor, frame.RecordDigest, checkpoint.RecordDigest)
	if err != nil {
		return nil, err
	}
	schema := cloneGenerationJournalSchema(j.schema)
	schema.chainWitness = chain
	if err := refreshGenerationJournalObservedLedger(&schema, frames); err != nil {
		nextCursor.valid.Store(false)
		return nil, err
	}
	recovery, err := buildRecoverySnapshot(frames, nextCursor, j.generation, recoveredContinuation{}, schema)
	if err != nil {
		nextCursor.valid.Store(false)
		return nil, err
	}
	p := &preparedGenerationJournalAppend{
		frame: frame, framed: framed, checkpoint: checkpoint, checkpointFramed: checkpointFramed,
		nextCursor: nextCursor, previousRecovery: cloneRecoverySnapshot(j.state.recovery), recovery: recovery,
	}
	p.journalRecords, err = admissionCheckedAdd(j.state.journalRecords, 1)
	if err == nil {
		p.journalBytes, err = admissionCheckedAdd(j.state.journalBytes, uint64(len(framed)))
	}
	p.segmentRecords, p.segmentBytes = nextSegmentRecords, nextSegmentBytes
	if err == nil {
		p.checkpointRecords, err = admissionCheckedAdd(j.state.checkpointRecords, 1)
	}
	if err == nil {
		p.indexDebitRecords, err = admissionCheckedAdd(j.state.indexDebitRecords, 1)
	}
	if err == nil {
		p.indexDebitBytes, err = admissionCheckedAdd(j.state.indexDebitBytes, uint64(len(checkpointFramed)))
	}
	if err != nil {
		p.invalidate()
		return nil, err
	}
	if p.journalRecords > j.reservation.ReservedRecords || p.journalBytes > j.reservation.ReservedJournalBytes || p.checkpointRecords > j.reservation.ReservedCheckpointRecords || p.indexDebitRecords > j.reservation.ReservedIndexRecords || p.indexDebitBytes > j.reservation.ReservedIndexBytes {
		p.invalidate()
		return nil, fail(CodeEvidenceJournalLimitExceeded, "generation-journal-append", "candidate exceeds its verified generation reservation", nil)
	}
	p.canonical = preparedGenerationJournalAppendDigest(p)
	if p.canonical == ([32]byte{}) {
		p.invalidate()
		return nil, admissionFailed("generation-journal-append", "prepared append could not be sealed", nil)
	}
	return p, nil
}

func refreshGenerationJournalObservedLedger(schema *verifiedRecoverySchemaWitness, frames []EvidenceFrame) error {
	if schema == nil || len(schema.orderedMigrations) == 0 || len(schema.signedExpectedLedgerRows) != len(schema.orderedMigrations) {
		return invalidEvidence("generation-journal-schema", "signed ledger witness is unavailable")
	}
	committed := make(map[string]bool, len(schema.orderedMigrations))
	for _, frame := range frames {
		if terminal := frame.Record.AttemptTerminal; terminal != nil && stringIn(terminal.Outcome, "committed", "ambiguous_reconciled_committed") {
			committed[terminal.MigrationID] = true
		}
		if resolution := frame.Record.AmbiguousResolution; resolution != nil && resolution.Outcome == "resolved_committed" {
			committed[resolution.MigrationID] = true
		}
	}
	count := 0
	for _, migration := range schema.orderedMigrations {
		if !committed[migration] {
			break
		}
		count++
	}
	for _, migration := range schema.orderedMigrations[count:] {
		if committed[migration] {
			return invalidEvidence("generation-journal-schema", "committed ledger is not an ordered prefix")
		}
	}
	schema.durableObservedLedgerPrefix = cloneProjectionValue(schema.signedExpectedLedgerRows[:count])
	digest, err := LedgerPrefixDigest(schema.durableObservedLedgerPrefix)
	if err != nil {
		return err
	}
	schema.durableObservedLedgerDigest = digest
	return nil
}

func inspectGenerationJournalRecord(record *OwnedEvidenceRecord, generation generationIdentity, cursor JournalCursor) ([]EvidenceFrame, verifiedEvidenceChainWitness, EvidenceFrame, error) {
	if record == nil || record.consumed == nil || record.consumed.Load() || record.witness == nil || !cursor.Valid() || !sameGenerationIdentity(record.generation, generation) || !sameGenerationIdentity(record.witness.generationIdentity(), generation) || !sameCursorIdentity(record.cursor, cursor) || !sameCursorIdentity(record.witness.cursorIdentity(), cursor) || cursor.previousRecordDigest == nil {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, invalidEvidence("generation-journal-append", "owned record generation or cursor mismatch")
	}
	wire := cloneEvidenceRecord(record.wire)
	if err := validateEvidenceRecord(wire); err != nil {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, err
	}
	if !evidenceKindMatches(record.witness.kind(), wire) || record.witness.kind() == EvidenceRecordHeader {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, invalidEvidence("generation-journal-append", "record kind or header authority")
	}
	if err := validateRuntimeWitness(wire, record.witness); err != nil {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, err
	}
	prefix, chain := record.witness.prefixAndChain()
	if len(prefix) == 0 || prefix[len(prefix)-1].RecordDigest != *cursor.previousRecordDigest || prefix[len(prefix)-1].Sequence+1 != cursor.nextSequence {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, invalidEvidence("generation-journal-append", "candidate prefix differs from cursor")
	}
	frame := EvidenceFrame{FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence, PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest), RecordKind: record.witness.kind(), Record: wire}
	var err error
	frame.RecordDigest, err = frame.ComputeDigest()
	if err != nil {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, err
	}
	frames := append(cloneProjectionValue(prefix), frame)
	if err := validateEvidenceChainWithWitness(frames, chain); err != nil {
		return nil, verifiedEvidenceChainWitness{}, EvidenceFrame{}, err
	}
	return frames, chain, frame, nil
}

func buildGenerationJournalCheckpoint(generation generationIdentity, cursor JournalCursor, frame EvidenceFrame, summary evidenceJournalSummary) (LineageIndexFrame, []byte, error) {
	checkpoint := GenerationCheckpoint{
		ExecutionLineageDigest: generation.executionLineageDigest, JournalIdentityDigest: generation.journalIdentityDigest,
		RunnerProjectionDecisionDigest: generation.runnerProjectionDecisionDigest, SchemaBundleDigest: generation.schemaBundleDigest,
		JournalNextSequence: frame.Sequence + 1, JournalTailDigest: frame.RecordDigest,
		RecoveryState: summary.recoveryState, MigrationID: cloneStringPointer(summary.migrationID), AttemptIndex: cloneUint32Pointer(summary.attemptIndex),
		LastStatementIntentRecordDigest: cloneDigestPointer(summary.lastStatementIntentRecordDigest), LastIntermediateEvidenceRecordDigest: cloneDigestPointer(summary.lastIntermediateEvidenceRecordDigest),
		LastCommitIntentRecordDigest: cloneDigestPointer(summary.lastCommitIntentRecordDigest), LastTerminalDigest: cloneDigestPointer(summary.lastTerminalDigest),
		LastResolutionDigest: cloneDigestPointer(summary.lastResolutionDigest), PreviousAttemptTerminalDigest: cloneDigestPointer(summary.previousAttemptTerminalDigest),
		LastIntermediateStateDigest: cloneDigestPointer(summary.lastIntermediateStateDigest), PreviousCheckpointRecordDigest: cloneDigestPointer(cursor.latestCheckpointRecordDigest),
	}
	previous := cursor.lineageIndexPreviousRecordDigest
	lineage := LineageIndexFrame{
		FormatVersion: LineageFrameFormat, Sequence: cursor.lineageIndexNextSequence, PreviousRecordDigest: &previous,
		RecordKind: LineageRecordGenerationCheckpoint, Record: LineageIndexRecord{Checkpoint: &checkpoint},
	}
	var err error
	lineage.RecordDigest, err = lineage.ComputeDigest()
	if err != nil || lineage.Validate() != nil {
		return LineageIndexFrame{}, nil, invalidEvidence("generation-journal-checkpoint", "checkpoint frame is invalid")
	}
	framed, err := EncodeCanonicalLineageFrame(lineage)
	if err != nil {
		return LineageIndexFrame{}, nil, err
	}
	return lineage, framed, nil
}

func advanceGenerationJournalCursor(cursor JournalCursor, tail, checkpoint Digest) (JournalCursor, error) {
	if !cursor.Valid() || cursor.nextSequence == maxJSONInteger || cursor.lineageIndexNextSequence == maxJSONInteger || tail.Validate() != nil || checkpoint.Validate() != nil {
		return JournalCursor{}, invalidEvidence("generation-journal-cursor", "cursor cannot advance")
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	next := JournalCursor{
		owner: cursor.owner, generation: cursor.generation, segmentIndex: cursor.segmentIndex,
		nextSequence: cursor.nextSequence + 1, previousRecordDigest: digestPointer(tail),
		lineageIndexNextSequence: cursor.lineageIndexNextSequence + 1, lineageIndexPreviousRecordDigest: checkpoint,
		latestCheckpointRecordDigest: digestPointer(checkpoint), valid: valid,
	}
	return next, nil
}

func (j *generationEvidenceJournal) nextDurableStateLocked(snapshot *evidencefs.GenerationSnapshot, prepared *preparedGenerationJournalAppend) (*generationEvidenceJournalState, error) {
	if snapshot == nil || prepared == nil || prepared.canonical == ([32]byte{}) || prepared.canonical != preparedGenerationJournalAppendDigest(prepared) {
		return nil, evidencefs.ErrLeaseInvalid
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return nil, err
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil {
		return nil, err
	}
	count, err := snapshot.SegmentCount()
	if err != nil {
		return nil, err
	}
	if count != uint32(len(j.state.segmentFacts)) {
		return nil, evidencefs.ErrCorrupt
	}
	segments := make([]evidencefs.GenerationFileFact, count)
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		segments[ordinal], err = snapshot.SegmentFact(ordinal)
		if err != nil {
			return nil, err
		}
		if ordinal+1 != count && segments[ordinal] != j.state.segmentFacts[ordinal] {
			return nil, evidencefs.ErrCorrupt
		}
	}
	if indexFact.Size != j.state.indexFact.Size+uint64(len(prepared.checkpointFramed)) || segments[count-1].Size != j.state.segmentBytes+uint64(len(prepared.framed)) || identity == j.state.snapshotIdentity {
		return nil, evidencefs.ErrCorrupt
	}
	return &generationEvidenceJournalState{
		snapshot: snapshot, snapshotIdentity: identity, indexFact: indexFact, segmentFacts: segments,
		cursor: prepared.nextCursor.clone(), recovery: cloneRecoverySnapshot(prepared.recovery),
		journalRecords: prepared.journalRecords, journalBytes: prepared.journalBytes,
		segmentRecords: prepared.segmentRecords, segmentBytes: prepared.segmentBytes,
		checkpointRecords: prepared.checkpointRecords, indexDebitRecords: prepared.indexDebitRecords, indexDebitBytes: prepared.indexDebitBytes,
	}, nil
}

func (j *generationEvidenceJournal) knownStateFromSnapshotLocked(snapshot *evidencefs.GenerationSnapshot, prepared *preparedGenerationJournalAppend, progress preparedGenerationJournalProgress) (*generationEvidenceJournalState, error) {
	if j == nil || j.state == nil || snapshot == nil || prepared == nil || prepared.canonical == ([32]byte{}) || prepared.canonical != preparedGenerationJournalAppendDigest(prepared) || !j.lease.OwnsSnapshot(snapshot) {
		return nil, evidencefs.ErrLeaseInvalid
	}
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return nil, err
	}
	if identity == ([32]byte{}) {
		return nil, evidencefs.ErrCorrupt
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil {
		return nil, err
	}
	count, err := snapshot.SegmentCount()
	if err != nil {
		return nil, err
	}
	expectedCount := uint32(len(j.state.segmentFacts))
	if prepared.rotation != nil && progress != preparedGenerationJournalPrevious {
		expectedCount++
	}
	if count != expectedCount || count == 0 {
		return nil, evidencefs.ErrCorrupt
	}
	segments := make([]evidencefs.GenerationFileFact, count)
	for ordinal := uint32(0); ordinal < count; ordinal++ {
		segments[ordinal], err = snapshot.SegmentFact(ordinal)
		if err != nil {
			return nil, err
		}
		compareOld := ordinal < uint32(len(j.state.segmentFacts))
		if prepared.rotation == nil && ordinal+1 == count {
			compareOld = false
		}
		if compareOld && segments[ordinal] != j.state.segmentFacts[ordinal] {
			return nil, evidencefs.ErrCorrupt
		}
	}
	cursorSource := prepared.nextCursor
	recoverySource := prepared.recovery
	journalRecords, journalBytes := prepared.journalRecords, prepared.journalBytes
	segmentRecords, segmentBytes := prepared.segmentRecords, prepared.segmentBytes
	checkpointRecords := prepared.checkpointRecords
	indexDebitRecords, indexDebitBytes := prepared.indexDebitRecords, prepared.indexDebitBytes
	switch progress {
	case preparedGenerationJournalCandidate:
		expectedIndexSize := j.state.indexFact.Size + uint64(len(prepared.checkpointFramed))
		expectedSegmentSize := j.state.segmentBytes + uint64(len(prepared.framed))
		if prepared.rotation != nil {
			expectedIndexSize += uint64(len(prepared.rotation.headerCheckpointFramed))
			expectedSegmentSize = uint64(len(prepared.rotation.headerFramed) + len(prepared.framed))
		}
		if indexFact.Size != expectedIndexSize || segments[count-1].Size != expectedSegmentSize {
			return nil, evidencefs.ErrCorrupt
		}
	case preparedGenerationJournalRotationHeader:
		if prepared.rotation == nil || indexFact.Size != j.state.indexFact.Size+uint64(len(prepared.rotation.headerCheckpointFramed)) || segments[count-1].Size != uint64(len(prepared.rotation.headerFramed)) {
			return nil, evidencefs.ErrCorrupt
		}
		cursorSource, recoverySource = prepared.rotation.headerCursor, prepared.rotation.headerRecovery
		journalRecords, journalBytes = prepared.rotation.journalRecords, prepared.rotation.journalBytes
		segmentRecords, segmentBytes = prepared.rotation.segmentRecords, prepared.rotation.segmentBytes
		checkpointRecords = prepared.rotation.checkpointRecords
		indexDebitRecords, indexDebitBytes = prepared.rotation.indexDebitRecords, prepared.rotation.indexDebitBytes
	case preparedGenerationJournalPrevious:
		if indexFact.Size != j.state.indexFact.Size || indexFact.ContentDigest != j.state.indexFact.ContentDigest || segments[count-1].Size != j.state.segmentFacts[count-1].Size || segments[count-1].ContentDigest != j.state.segmentFacts[count-1].ContentDigest {
			return nil, evidencefs.ErrCorrupt
		}
		cursorSource = j.state.cursor
		recoverySource = prepared.previousRecovery
		journalRecords, journalBytes = j.state.journalRecords, j.state.journalBytes
		segmentRecords, segmentBytes = j.state.segmentRecords, j.state.segmentBytes
		checkpointRecords = j.state.checkpointRecords
		indexDebitRecords, indexDebitBytes = j.state.indexDebitRecords, j.state.indexDebitBytes
	default:
		return nil, evidencefs.ErrInvalidInput
	}
	cursor, err := renewGenerationJournalCursor(cursorSource)
	if err != nil {
		return nil, err
	}
	recovery, err := renewGenerationJournalRecovery(recoverySource, cursor, j.generation)
	if err != nil {
		cursor.valid.Store(false)
		return nil, err
	}
	return &generationEvidenceJournalState{
		snapshot: snapshot, snapshotIdentity: identity, indexFact: indexFact, segmentFacts: segments,
		cursor: cursor, recovery: recovery, journalRecords: journalRecords, journalBytes: journalBytes,
		segmentRecords: segmentRecords, segmentBytes: segmentBytes, checkpointRecords: checkpointRecords,
		indexDebitRecords: indexDebitRecords, indexDebitBytes: indexDebitBytes,
	}, nil
}

func renewGenerationJournalCursor(source JournalCursor) (JournalCursor, error) {
	if source.owner == nil || source.owner != source.generation.owner || source.previousRecordDigest == nil || source.previousRecordDigest.Validate() != nil || source.lineageIndexPreviousRecordDigest.Validate() != nil || source.nextSequence == 0 || source.lineageIndexNextSequence == 0 || !sameGenerationIdentity(source.generation, source.generation) {
		return JournalCursor{}, evidencefs.ErrLeaseInvalid
	}
	if source.latestCheckpointRecordDigest != nil && source.latestCheckpointRecordDigest.Validate() != nil {
		return JournalCursor{}, evidencefs.ErrLeaseInvalid
	}
	valid := &atomic.Bool{}
	valid.Store(true)
	return JournalCursor{
		owner: source.owner, generation: source.generation, segmentIndex: source.segmentIndex,
		nextSequence: source.nextSequence, previousRecordDigest: cloneDigestPointer(source.previousRecordDigest),
		lineageIndexNextSequence:         source.lineageIndexNextSequence,
		lineageIndexPreviousRecordDigest: source.lineageIndexPreviousRecordDigest,
		latestCheckpointRecordDigest:     cloneDigestPointer(source.latestCheckpointRecordDigest), valid: valid,
	}, nil
}

func renewGenerationJournalRecovery(source *RecoverySnapshot, cursor JournalCursor, generation generationIdentity) (*RecoverySnapshot, error) {
	if source == nil || !cursor.Valid() || cursor.previousRecordDigest == nil || source.owner != generation.owner || !sameGenerationIdentity(source.generation, generation) || source.tailDigest != *cursor.previousRecordDigest || generationJournalRecoveryDigest(source) == ([32]byte{}) {
		return nil, evidencefs.ErrLeaseInvalid
	}
	next := cloneRecoverySnapshot(source)
	next.cursor = cursor.clone()
	if !renewGenerationRecovered(next.lineageContinuation, generation, cursor, next.tailDigest) ||
		!renewGenerationRecovered(next.lastStatementIntent, generation, cursor, next.tailDigest) ||
		!renewGenerationRecovered(next.lastIntermediateEvidence, generation, cursor, next.tailDigest) ||
		!renewGenerationRecovered(next.commitIntent, generation, cursor, next.tailDigest) ||
		!renewGenerationRecovered(next.lastTerminal, generation, cursor, next.tailDigest) ||
		!renewGenerationRecovered(next.lastResolution, generation, cursor, next.tailDigest) ||
		!validRecoverySnapshotForJournal(next, generation, cursor) {
		cursor.valid.Store(false)
		return nil, evidencefs.ErrLeaseInvalid
	}
	return next, nil
}

func renewGenerationRecovered[T any](value *OwnedRecovered[T], generation generationIdentity, cursor JournalCursor, tail Digest) bool {
	if value == nil {
		return true
	}
	if value.owner != generation.owner || !sameGenerationIdentity(value.generation, generation) || value.tailDigest != tail || value.recordDigest.Validate() != nil {
		return false
	}
	value.cursor = cursor.clone()
	return true
}

func (j *generationEvidenceJournal) installUnknownLocked(prepared *preparedGenerationJournalAppend, filesystemResult evidencefs.GenerationAppendResult, cause error) (AppendResult, error) {
	if prepared == nil || prepared.rotation != nil || filesystemResult.Outcome() != evidencefs.AdmissionTransitionUnknown || filesystemResult.Snapshot() != nil || filesystemResult.NextSnapshotIdentity() != ([32]byte{}) || filesystemResult.PreviousSnapshotIdentity() != j.state.snapshotIdentity || filesystemResult.SegmentOrdinal() != j.state.cursor.segmentIndex || filesystemResult.JournalPreviousSize() != j.state.segmentBytes || filesystemResult.IndexPreviousSize() != j.state.indexFact.Size || filesystemResult.JournalFramedDigest() != sha256.Sum256(prepared.framed) || filesystemResult.CheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) {
		if prepared != nil {
			prepared.invalidate()
		}
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-append-unknown-shape")
	}
	prepared.invalidate()
	appendResult, finishErr := finishConsumedAppend(j.state.cursor, j.generation, appendOutcomeUnknown, nil, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest)
	if finishErr != nil {
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(finishErr, "generation-journal-append-result")
	}
	next := cloneGenerationEvidenceJournalState(j.state)
	next.snapshot = nil
	next.snapshotIdentity = [32]byte{}
	next.recovery = nil
	next.cursor.valid.Store(false)
	next.unknown = &generationJournalUnknownAppend{filesystem: filesystemResult, prepared: prepared}
	if !j.installStateLocked(next) {
		return AppendResult{}, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-append-unknown-seal")
	}
	_ = cause
	return appendResult, admissionPostMutationFailure("generation-journal-append")
}

func (j *generationEvidenceJournal) restoreAfterPreMutationFailureLocked(prepared *preparedGenerationJournalAppend, cause error) (AppendResult, error) {
	if j == nil || prepared == nil || j.state == nil || j.state.snapshot == nil || !j.lease.Active() || !j.lease.OwnsSnapshot(j.state.snapshot) {
		if prepared != nil {
			prepared.invalidate()
		}
		return AppendResult{}, j.failLocked(cause, "generation-journal-append-pre-mutation")
	}
	prepared.invalidate()
	var appendResult AppendResult
	var finishErr error
	if prepared.rotation == nil {
		appendResult, finishErr = finishConsumedAppend(j.state.cursor, j.generation, appendOutcomeUnknown, nil, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest)
	} else {
		appendResult, finishErr = finishConsumedRotationAppend(j.state.cursor, j.generation, appendOutcomeUnknown, nil, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest, prepared.rotation.header.RecordDigest, prepared.rotation.headerCheckpoint.RecordDigest)
	}
	if finishErr != nil {
		return AppendResult{}, j.failLocked(finishErr, "generation-journal-append-result")
	}
	next, stateErr := j.knownStateFromSnapshotLocked(j.state.snapshot, prepared, preparedGenerationJournalPrevious)
	if stateErr != nil || !j.installStateLocked(next) {
		if stateErr == nil {
			stateErr = evidencefs.ErrLeaseInvalid
		}
		return AppendResult{}, j.failLocked(stateErr, "generation-journal-append-restore")
	}
	_ = cause
	return appendResult, admissionPostMutationFailure("generation-journal-append")
}

func cloneGenerationEvidenceJournalState(value *generationEvidenceJournalState) *generationEvidenceJournalState {
	if value == nil {
		return nil
	}
	result := *value
	result.self = nil
	result.journal = nil
	result.segmentFacts = append([]evidencefs.GenerationFileFact(nil), value.segmentFacts...)
	result.cursor = value.cursor.clone()
	result.recovery = cloneRecoverySnapshot(value.recovery)
	return &result
}

func (j *generationEvidenceJournal) installStateLocked(state *generationEvidenceJournalState) bool {
	if j == nil || state == nil || state.journal != nil || state.self != nil {
		return false
	}
	state.journal, state.self = j, state
	state.canonical = generationEvidenceJournalStateDigest(state)
	if state.canonical == ([32]byte{}) {
		return false
	}
	j.state = state
	generationEvidenceJournalRegistry.Store(j, generationEvidenceJournalRegistryRecord{
		journal: j, binding: j.binding, prior: j.prior, replay: j.replay, lease: j.lease,
		state: state, canonical: j.binding.canonical, stateCanonical: state.canonical,
	})
	return j.validLocked()
}

func (j *generationEvidenceJournal) validLocked() bool {
	if j == nil || j.self != j || j.closed || j.binding == nil || j.binding.journal != j || j.prior == nil || j.replay == nil || j.plan == nil || j.history == nil || j.candidateBinding == nil || j.lease == nil || j.binding.prior != j.prior || j.binding.replay != j.replay || j.binding.plan != j.plan || j.binding.history != j.history || j.binding.candidateBinding != j.candidateBinding || j.binding.lease != j.lease || j.state == nil || j.state.self != j.state || j.state.journal != j || !j.lease.Active() || j.binding.canonical == ([32]byte{}) || j.binding.canonical != generationEvidenceJournalDigest(j) || j.state.canonical == ([32]byte{}) || j.state.canonical != generationEvidenceJournalStateDigest(j.state) {
		return false
	}
	if j.state.unknown == nil {
		if j.state.snapshot == nil || !j.lease.OwnsSnapshot(j.state.snapshot) || !j.state.cursor.Valid() || !validRecoverySnapshotForJournal(j.state.recovery, j.generation, j.state.cursor) {
			return false
		}
	} else if j.state.snapshot != nil || j.state.cursor.Valid() || j.state.unknown.prepared == nil || j.state.unknown.prepared.canonical == ([32]byte{}) || j.state.unknown.prepared.canonical != preparedGenerationJournalAppendDigest(j.state.unknown.prepared) {
		return false
	}
	registered, ok := generationEvidenceJournalRegistry.Load(j)
	record, recordOK := registered.(generationEvidenceJournalRegistryRecord)
	return ok && recordOK && record.journal == j && record.binding == j.binding && record.prior == j.prior && record.replay == j.replay && record.lease == j.lease && record.state == j.state && record.canonical == j.binding.canonical && record.stateCanonical == j.state.canonical
}

func generationEvidenceJournalDigest(j *generationEvidenceJournal) [32]byte {
	if j == nil || j.self != j || j.prior == nil || j.replay == nil || j.plan == nil || j.history == nil || j.candidateBinding == nil || j.lease == nil || j.prior.binding == nil || j.replay.binding == nil || j.plan.binding == nil || j.history.binding == nil || j.reservation.ReservedRecords == 0 || j.reservation.ReservedBytes != j.reservation.ReservedJournalBytes+j.reservation.ReservedIndexBytes {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-evidence-journal/v1\x00"))
	h.Write(j.prior.binding.canonical[:])
	h.Write(j.replay.binding.canonical[:])
	h.Write(j.plan.binding.canonical[:])
	h.Write(j.history.binding.canonical[:])
	h.Write(j.candidateBinding.canonical[:])
	schemaDigest := generationJournalSchemaDigest(j.schema, j.generation)
	if schemaDigest == ([32]byte{}) {
		return [32]byte{}
	}
	h.Write(schemaDigest[:])
	for _, value := range []Digest{j.generation.executionLineageDigest, j.generation.journalIdentityDigest, j.generation.runnerProjectionDecisionDigest, j.generation.schemaBundleDigest} {
		writeAdmissionString(h, value.String())
	}
	for _, value := range []uint64{j.reservation.ReservedRecords, j.reservation.ReservedJournalBytes, uint64(j.reservation.ReservedSegments), j.reservation.ReservedCheckpointRecords, j.reservation.ReservedIndexRecords, j.reservation.ReservedIndexBytes, j.reservation.ReservedBytes} {
		writeAdmissionUint(h, value)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationEvidenceJournalStateDigest(state *generationEvidenceJournalState) [32]byte {
	if state == nil || state.self != state || state.journal == nil || len(state.segmentFacts) == 0 || state.journalRecords == 0 || state.journalBytes == 0 || state.segmentRecords == 0 || state.segmentBytes == 0 || state.indexDebitRecords == 0 || state.indexDebitBytes == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-evidence-journal-state/v1\x00"))
	h.Write(state.snapshotIdentity[:])
	writeGenerationFileFact(h, state.indexFact)
	writeAdmissionUint(h, uint64(len(state.segmentFacts)))
	for _, fact := range state.segmentFacts {
		writeGenerationFileFact(h, fact)
	}
	writeGenerationJournalCursor(h, state.cursor)
	for _, value := range []uint64{state.journalRecords, state.journalBytes, state.segmentRecords, state.segmentBytes, state.checkpointRecords, state.indexDebitRecords, state.indexDebitBytes} {
		writeAdmissionUint(h, value)
	}
	if state.unknown == nil {
		writeAdmissionString(h, "known")
		recoveryDigest := generationJournalRecoveryDigest(state.recovery)
		if recoveryDigest == ([32]byte{}) {
			return [32]byte{}
		}
		h.Write(recoveryDigest[:])
	} else {
		if state.recovery != nil || state.unknown.prepared == nil {
			return [32]byte{}
		}
		writeAdmissionString(h, "unknown")
		var filesystemDigest [32]byte
		if state.unknown.rotation == nil {
			if state.unknown.prepared.rotation != nil {
				return [32]byte{}
			}
			filesystemDigest = generationJournalUnknownFilesystemDigest(state.unknown.filesystem, state.unknown.prepared)
		} else {
			if state.unknown.prepared.rotation == nil || state.unknown.filesystem.Outcome() != "" {
				return [32]byte{}
			}
			filesystemDigest = generationJournalUnknownRotationDigest(*state.unknown.rotation, state.unknown.prepared)
		}
		if filesystemDigest == ([32]byte{}) {
			return [32]byte{}
		}
		h.Write(filesystemDigest[:])
		h.Write(state.unknown.prepared.canonical[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func preparedGenerationJournalAppendDigest(value *preparedGenerationJournalAppend) [32]byte {
	if value == nil || value.frame.Validate() != nil || value.checkpoint.Validate() != nil || value.frame.PreviousRecordDigest == nil || value.checkpoint.Record.Checkpoint == nil || len(value.framed) == 0 || len(value.checkpointFramed) == 0 || value.nextCursor.valid == nil || value.nextCursor.previousRecordDigest == nil || value.previousRecovery == nil || value.recovery == nil || value.recovery.tailDigest != value.frame.RecordDigest || *value.nextCursor.previousRecordDigest != value.frame.RecordDigest || value.nextCursor.latestCheckpointRecordDigest == nil || *value.nextCursor.latestCheckpointRecordDigest != value.checkpoint.RecordDigest || value.nextCursor.nextSequence != value.frame.Sequence+1 || value.nextCursor.lineageIndexNextSequence != value.checkpoint.Sequence+1 || value.nextCursor.lineageIndexPreviousRecordDigest != value.checkpoint.RecordDigest || value.checkpoint.Record.Checkpoint.JournalTailDigest != value.frame.RecordDigest || value.checkpoint.Record.Checkpoint.JournalNextSequence != value.frame.Sequence+1 {
		return [32]byte{}
	}
	framed, frameErr := EncodeCanonicalEvidenceFrame(value.frame)
	checkpointFramed, checkpointErr := EncodeCanonicalLineageFrame(value.checkpoint)
	if frameErr != nil || checkpointErr != nil || string(framed) != string(value.framed) || string(checkpointFramed) != string(value.checkpointFramed) {
		return [32]byte{}
	}
	previous := value.previousRecovery.cursor
	if previous.previousRecordDigest == nil || !sameCursorIdentity(value.recovery.cursor, value.nextCursor) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-prepared-generation-append/v1\x00"))
	if value.rotation == nil {
		if value.previousRecovery.tailDigest != *value.frame.PreviousRecordDigest || value.frame.Sequence != previous.nextSequence || *value.frame.PreviousRecordDigest != *previous.previousRecordDigest || value.checkpoint.Sequence != previous.lineageIndexNextSequence || value.checkpoint.PreviousRecordDigest == nil || *value.checkpoint.PreviousRecordDigest != previous.lineageIndexPreviousRecordDigest || !equalDigestPointer(value.checkpoint.Record.Checkpoint.PreviousCheckpointRecordDigest, previous.latestCheckpointRecordDigest) {
			return [32]byte{}
		}
		writeAdmissionString(h, "existing")
	} else {
		writeAdmissionString(h, "rotation")
		if !writePreparedGenerationJournalRotation(h, value, previous) {
			return [32]byte{}
		}
	}
	h.Write(value.framed)
	h.Write(value.checkpointFramed)
	writeGenerationJournalCursor(h, value.nextCursor)
	previousRecovery := generationJournalRecoveryDigest(value.previousRecovery)
	recovery := generationJournalRecoveryDigest(value.recovery)
	if previousRecovery == ([32]byte{}) || recovery == ([32]byte{}) {
		return [32]byte{}
	}
	h.Write(previousRecovery[:])
	h.Write(recovery[:])
	for _, value := range []uint64{value.journalRecords, value.journalBytes, value.segmentRecords, value.segmentBytes, value.checkpointRecords, value.indexDebitRecords, value.indexDebitBytes} {
		writeAdmissionUint(h, value)
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writePreparedGenerationJournalRotation(h interface{ Write([]byte) (int, error) }, value *preparedGenerationJournalAppend, previous JournalCursor) bool {
	rotation := value.rotation
	if rotation == nil || rotation.header.Validate() != nil || rotation.header.Record.Header == nil || rotation.header.Record.Header.SegmentIndex != previous.segmentIndex+1 || rotation.header.PreviousRecordDigest == nil || *rotation.header.PreviousRecordDigest != *previous.previousRecordDigest || rotation.header.Sequence != previous.nextSequence || rotation.headerCheckpoint.Validate() != nil || rotation.headerCheckpoint.Record.Checkpoint == nil || len(rotation.headerFramed) == 0 || len(rotation.headerCheckpointFramed) == 0 || rotation.headerCursor.valid == nil || rotation.headerCursor.segmentIndex != rotation.header.Record.Header.SegmentIndex || rotation.headerCursor.previousRecordDigest == nil || *rotation.headerCursor.previousRecordDigest != rotation.header.RecordDigest || rotation.headerCursor.nextSequence != rotation.header.Sequence+1 || rotation.headerCursor.lineageIndexNextSequence != rotation.headerCheckpoint.Sequence+1 || rotation.headerCursor.lineageIndexPreviousRecordDigest != rotation.headerCheckpoint.RecordDigest || rotation.headerCursor.latestCheckpointRecordDigest == nil || *rotation.headerCursor.latestCheckpointRecordDigest != rotation.headerCheckpoint.RecordDigest || rotation.headerCheckpoint.Sequence != previous.lineageIndexNextSequence || rotation.headerCheckpoint.PreviousRecordDigest == nil || *rotation.headerCheckpoint.PreviousRecordDigest != previous.lineageIndexPreviousRecordDigest || !equalDigestPointer(rotation.headerCheckpoint.Record.Checkpoint.PreviousCheckpointRecordDigest, previous.latestCheckpointRecordDigest) || rotation.headerCheckpoint.Record.Checkpoint.JournalTailDigest != rotation.header.RecordDigest || rotation.headerCheckpoint.Record.Checkpoint.JournalNextSequence != rotation.header.Sequence+1 || rotation.headerRecovery == nil || rotation.headerRecovery.tailDigest != rotation.header.RecordDigest || !sameCursorIdentity(rotation.headerRecovery.cursor, rotation.headerCursor) || value.frame.Sequence != rotation.headerCursor.nextSequence || *value.frame.PreviousRecordDigest != rotation.header.RecordDigest || value.checkpoint.Sequence != rotation.headerCursor.lineageIndexNextSequence || value.checkpoint.PreviousRecordDigest == nil || *value.checkpoint.PreviousRecordDigest != rotation.headerCursor.lineageIndexPreviousRecordDigest || !equalDigestPointer(value.checkpoint.Record.Checkpoint.PreviousCheckpointRecordDigest, rotation.headerCursor.latestCheckpointRecordDigest) || value.nextCursor.segmentIndex != rotation.headerCursor.segmentIndex || rotation.journalRecords+1 != value.journalRecords || rotation.segmentRecords+1 != value.segmentRecords || rotation.checkpointRecords+1 != value.checkpointRecords || rotation.indexDebitRecords+1 != value.indexDebitRecords {
		return false
	}
	headerFramed, headerErr := EncodeCanonicalEvidenceFrame(rotation.header)
	headerCheckpointFramed, checkpointErr := EncodeCanonicalLineageFrame(rotation.headerCheckpoint)
	if headerErr != nil || checkpointErr != nil || string(headerFramed) != string(rotation.headerFramed) || string(headerCheckpointFramed) != string(rotation.headerCheckpointFramed) || rotation.journalBytes > value.journalBytes || value.journalBytes-rotation.journalBytes != uint64(len(value.framed)) || rotation.segmentBytes > value.segmentBytes || value.segmentBytes-rotation.segmentBytes != uint64(len(value.framed)) || rotation.indexDebitBytes > value.indexDebitBytes || value.indexDebitBytes-rotation.indexDebitBytes != uint64(len(value.checkpointFramed)) {
		return false
	}
	h.Write(rotation.headerFramed)
	h.Write(rotation.headerCheckpointFramed)
	writeGenerationJournalCursor(h, rotation.headerCursor)
	headerRecovery := generationJournalRecoveryDigest(rotation.headerRecovery)
	if headerRecovery == ([32]byte{}) {
		return false
	}
	h.Write(headerRecovery[:])
	for _, counter := range []uint64{rotation.journalRecords, rotation.journalBytes, rotation.segmentRecords, rotation.segmentBytes, rotation.checkpointRecords, rotation.indexDebitRecords, rotation.indexDebitBytes} {
		writeAdmissionUint(h, counter)
	}
	return true
}

func generationJournalUnknownFilesystemDigest(result evidencefs.GenerationAppendResult, prepared *preparedGenerationJournalAppend) [32]byte {
	if prepared == nil || result.Outcome() != evidencefs.AdmissionTransitionUnknown || result.Snapshot() != nil || result.NextSnapshotIdentity() != ([32]byte{}) || result.PreviousSnapshotIdentity() == ([32]byte{}) || result.JournalFramedDigest() != sha256.Sum256(prepared.framed) || result.CheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-journal-unknown-filesystem/v1\x00"))
	previousIdentity := result.PreviousSnapshotIdentity()
	journalDigest := result.JournalFramedDigest()
	checkpointDigest := result.CheckpointFramedDigest()
	h.Write(previousIdentity[:])
	h.Write(journalDigest[:])
	h.Write(checkpointDigest[:])
	writeAdmissionUint(h, uint64(result.SegmentOrdinal()))
	writeAdmissionUint(h, result.JournalPreviousSize())
	writeAdmissionUint(h, result.IndexPreviousSize())
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func generationJournalUnknownRotationDigest(result evidencefs.GenerationRotationResult, prepared *preparedGenerationJournalAppend) [32]byte {
	if prepared == nil || prepared.rotation == nil || result.Outcome() != evidencefs.AdmissionTransitionUnknown || result.Snapshot() != nil || result.NextSnapshotIdentity() != ([32]byte{}) || result.PreviousSnapshotIdentity() == ([32]byte{}) || result.SegmentOrdinal() != prepared.rotation.headerCursor.segmentIndex || result.RotationHeaderFramedDigest() != sha256.Sum256(prepared.rotation.headerFramed) || result.RotationCheckpointFramedDigest() != sha256.Sum256(prepared.rotation.headerCheckpointFramed) || result.CallerFramedDigest() != sha256.Sum256(prepared.framed) || result.CallerCheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-journal-unknown-rotation/v1\x00"))
	previousIdentity := result.PreviousSnapshotIdentity()
	headerDigest := result.RotationHeaderFramedDigest()
	headerCheckpointDigest := result.RotationCheckpointFramedDigest()
	callerDigest := result.CallerFramedDigest()
	callerCheckpointDigest := result.CallerCheckpointFramedDigest()
	h.Write(previousIdentity[:])
	h.Write(headerDigest[:])
	h.Write(headerCheckpointDigest[:])
	h.Write(callerDigest[:])
	h.Write(callerCheckpointDigest[:])
	writeAdmissionUint(h, uint64(result.SegmentOrdinal()))
	writeAdmissionUint(h, result.IndexPreviousSize())
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest
}

func writeGenerationFileFact(h interface{ Write([]byte) (int, error) }, fact evidencefs.GenerationFileFact) {
	writeAdmissionUint(h, uint64(fact.Ordinal))
	writeAdmissionUint(h, fact.Size)
	h.Write(fact.ContentDigest[:])
	h.Write(fact.IdentityDigest[:])
}

func writeGenerationJournalCursor(h interface{ Write([]byte) (int, error) }, cursor JournalCursor) {
	writeAdmissionUint(h, uint64(cursor.segmentIndex))
	writeAdmissionUint(h, cursor.nextSequence)
	writeAdmissionUint(h, cursor.lineageIndexNextSequence)
	writeAdmissionString(h, cursor.lineageIndexPreviousRecordDigest.String())
	for _, value := range []*Digest{cursor.previousRecordDigest, cursor.latestCheckpointRecordDigest} {
		if value == nil {
			writeAdmissionString(h, "absent")
		} else {
			writeAdmissionString(h, value.String())
		}
	}
}

func generationJournalRecoveryDigest(snapshot *RecoverySnapshot) [32]byte {
	if snapshot == nil || snapshot.owner == nil || snapshot.owner != snapshot.generation.owner || snapshot.tailDigest.Validate() != nil || snapshot.cursor.valid == nil || snapshot.cursor.owner != snapshot.owner || snapshot.cursor.previousRecordDigest == nil || *snapshot.cursor.previousRecordDigest != snapshot.tailDigest || !sameGenerationIdentity(snapshot.generation, snapshot.cursor.generation) {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-journal-recovery/v1\x00"))
	for _, value := range []Digest{snapshot.generation.executionLineageDigest, snapshot.generation.journalIdentityDigest, snapshot.generation.runnerProjectionDecisionDigest, snapshot.generation.schemaBundleDigest} {
		if value.Validate() != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, value.String())
	}
	writeGenerationJournalCursor(h, snapshot.cursor)
	writeAdmissionString(h, snapshot.tailDigest.String())
	writeAdmissionString(h, string(snapshot.state))
	writeAdmissionString(h, string(snapshot.nextPermittedAction))
	writeGenerationOptionalString(h, snapshot.migrationID)
	writeGenerationOptionalUint32(h, snapshot.attemptIndex)
	for _, value := range []*Digest{snapshot.lastTerminalDigest, snapshot.lastResolutionDigest, snapshot.lastStatementIntentRecordDigest, snapshot.lastIntermediateEvidenceRecordDigest, snapshot.lastCommitIntentRecordDigest, snapshot.previousAttemptTerminalDigest, snapshot.lastIntermediateStateDigest} {
		writeGenerationOptionalDigest(h, value)
	}
	if !writeGenerationRecovered(h, snapshot.lineageContinuation, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) || !writeGenerationRecovered(h, snapshot.lastStatementIntent, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) || !writeGenerationRecovered(h, snapshot.lastIntermediateEvidence, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) || !writeGenerationRecovered(h, snapshot.commitIntent, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) || !writeGenerationRecovered(h, snapshot.lastTerminal, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) || !writeGenerationRecovered(h, snapshot.lastResolution, snapshot.owner, snapshot.generation, snapshot.cursor, snapshot.tailDigest) {
		return [32]byte{}
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeGenerationRecovered[T any](h interface{ Write([]byte) (int, error) }, value *OwnedRecovered[T], owner *evidenceOwnerToken, generation generationIdentity, cursor JournalCursor, tail Digest) bool {
	if value == nil {
		writeAdmissionString(h, "absent")
		return true
	}
	if owner == nil || value.owner != owner || !sameGenerationIdentity(value.generation, generation) || value.recordDigest.Validate() != nil || value.tailDigest != tail || value.cursor.valid == nil || !sameCursorIdentity(value.cursor, cursor) {
		return false
	}
	canonical, err := canonicalContractKey(value.value)
	if err != nil {
		return false
	}
	writeAdmissionString(h, "present")
	writeAdmissionString(h, value.recordDigest.String())
	writeAdmissionString(h, value.tailDigest.String())
	writeGenerationJournalCursor(h, value.cursor)
	writeAdmissionString(h, canonical)
	return true
}

func writeGenerationOptionalString(h interface{ Write([]byte) (int, error) }, value *string) {
	if value == nil {
		writeAdmissionString(h, "absent")
		return
	}
	writeAdmissionString(h, "present")
	writeAdmissionString(h, *value)
}

func writeGenerationOptionalUint32(h interface{ Write([]byte) (int, error) }, value *uint32) {
	if value == nil {
		writeAdmissionString(h, "absent")
		return
	}
	writeAdmissionString(h, "present")
	writeAdmissionUint(h, uint64(*value))
}

func writeGenerationOptionalDigest(h interface{ Write([]byte) (int, error) }, value *Digest) {
	if value == nil {
		writeAdmissionString(h, "absent")
		return
	}
	writeAdmissionString(h, "present")
	writeAdmissionString(h, value.String())
}

func validRecoverySnapshotForJournal(snapshot *RecoverySnapshot, generation generationIdentity, cursor JournalCursor) bool {
	if snapshot == nil || snapshot.owner != generation.owner || !sameGenerationIdentity(snapshot.generation, generation) || !sameCursorIdentity(snapshot.cursor, cursor) || snapshot.tailDigest.Validate() != nil || cursor.previousRecordDigest == nil || snapshot.tailDigest != *cursor.previousRecordDigest || generationJournalRecoveryDigest(snapshot) == ([32]byte{}) {
		return false
	}
	return true
}

func cloneGenerationJournalSchema(value verifiedRecoverySchemaWitness) verifiedRecoverySchemaWitness {
	value.finalStatementIndex = cloneUint32Map(value.finalStatementIndex)
	value.maxAttempts = cloneUint32Map(value.maxAttempts)
	value.orderedMigrations = append([]string(nil), value.orderedMigrations...)
	value.signedExpectedLedgerRows = cloneProjectionValue(value.signedExpectedLedgerRows)
	value.durableObservedLedgerPrefix = cloneProjectionValue(value.durableObservedLedgerPrefix)
	return value
}

func generationJournalSchemaDigest(value verifiedRecoverySchemaWitness, generation generationIdentity) [32]byte {
	if value.owner == nil || value.owner != generation.owner || !sameGenerationIdentity(value.generation, generation) || len(value.orderedMigrations) == 0 || len(value.signedExpectedLedgerRows) != len(value.orderedMigrations) || value.signedExpectedLedgerDigest.Validate() != nil || value.durableObservedLedgerDigest.Validate() != nil || value.finalCatalogDigest.Validate() != nil {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-generation-journal-schema/v1\x00"))
	seen := make(map[string]bool, len(value.orderedMigrations))
	for index, migration := range value.orderedMigrations {
		if !migrationIDPattern.MatchString(migration) || seen[migration] || value.maxAttempts[migration] == 0 {
			return [32]byte{}
		}
		seen[migration] = true
		final, ok := value.finalStatementIndex[migration]
		if !ok || value.signedExpectedLedgerRows[index].MigrationID != migration {
			return [32]byte{}
		}
		writeAdmissionString(h, migration)
		writeAdmissionUint(h, uint64(final))
		writeAdmissionUint(h, uint64(value.maxAttempts[migration]))
		canonical, err := canonicalContractKey(value.signedExpectedLedgerRows[index])
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	if len(value.finalStatementIndex) != len(value.orderedMigrations) || len(value.maxAttempts) != len(value.orderedMigrations) {
		return [32]byte{}
	}
	signedDigest, err := LedgerPrefixDigest(value.signedExpectedLedgerRows)
	if err != nil || signedDigest != value.signedExpectedLedgerDigest {
		return [32]byte{}
	}
	observedDigest, err := LedgerPrefixDigest(value.durableObservedLedgerPrefix)
	if err != nil || observedDigest != value.durableObservedLedgerDigest {
		return [32]byte{}
	}
	writeAdmissionString(h, value.signedExpectedLedgerDigest.String())
	writeAdmissionUint(h, uint64(len(value.durableObservedLedgerPrefix)))
	for _, row := range value.durableObservedLedgerPrefix {
		canonical, err := canonicalContractKey(row)
		if err != nil {
			return [32]byte{}
		}
		writeAdmissionString(h, canonical)
	}
	writeAdmissionString(h, value.durableObservedLedgerDigest.String())
	writeAdmissionString(h, value.finalCatalogDigest.String())
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func (j *generationEvidenceJournal) failLocked(cause error, operation string) error {
	if j == nil {
		return admissionFailed(operation, "journal authority is unavailable", nil)
	}
	registered, ok := generationEvidenceJournalRegistry.Load(j)
	record, recordOK := registered.(generationEvidenceJournalRegistryRecord)
	j.closed = true
	generationEvidenceJournalRegistry.Delete(j)
	if j.state != nil && j.state.cursor.valid != nil {
		j.state.cursor.valid.Store(false)
	}
	if j.state != nil && j.state.unknown != nil {
		j.state.unknown.prepared.invalidate()
	}
	if ok && recordOK && record.replay != nil {
		if cleanupErr := closeRegisteredGenerationReplay(record.replay, operation+"-cleanup"); cleanupErr != nil {
			return cleanupErr
		}
	}
	if IsCode(cause, CodeEvidenceJournalCorrupt) || IsCode(cause, CodeEvidenceJournalFailed) || IsCode(cause, CodeEvidenceRecoveryRequired) || IsCode(cause, CodeContextCanceled) || IsCode(cause, CodeDeadlineExceeded) {
		return cause
	}
	return mapEvidenceAdmissionError(cause, operation)
}

func (j *generationEvidenceJournal) reconcileUnknownLocked(ctx context.Context) (JournalCursor, *RecoverySnapshot, error) {
	if j == nil || j.state == nil || j.state.unknown == nil || j.state.unknown.prepared == nil {
		return JournalCursor{}, nil, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-reconcile-state")
	}
	unknown := j.state.unknown
	if unknown.rotation != nil {
		return j.reconcileUnknownRotationLocked(ctx)
	}
	fresh, err := j.lease.Snapshot(ctx)
	if err != nil {
		return JournalCursor{}, nil, j.reconcileObservationFailure(err, "generation-journal-reconcile-snapshot")
	}
	classification, err := unknown.filesystem.Reconcile(ctx, j.lease, fresh)
	if err != nil {
		return JournalCursor{}, nil, j.reconcileObservationFailure(err, "generation-journal-reconcile-classify")
	}
	switch classification {
	case evidencefs.GenerationAppendReconcileUnchanged:
		return j.installReconciledKnownLocked(fresh, unknown.prepared, false)
	case evidencefs.GenerationAppendReconcileJournalTorn:
		repaired, repairErr := j.truncateReconciledTailsLocked(ctx, fresh, unknown.filesystem.JournalPreviousSize(), unknown.filesystem.IndexPreviousSize())
		if repairErr != nil {
			return JournalCursor{}, nil, repairErr
		}
		return j.installReconciledKnownLocked(repaired, unknown.prepared, false)
	case evidencefs.GenerationAppendReconcileJournalComplete:
		resynced, resyncErr := j.resyncReconciledSnapshotLocked(ctx, fresh)
		if resyncErr != nil {
			return JournalCursor{}, nil, resyncErr
		}
		checkpointed, checkpointErr := j.appendReconciledCheckpointLocked(ctx, resynced, unknown.prepared)
		if checkpointErr != nil {
			return JournalCursor{}, nil, checkpointErr
		}
		return j.installReconciledKnownLocked(checkpointed, unknown.prepared, true)
	case evidencefs.GenerationAppendReconcileCheckpointTorn:
		segmentFact, factErr := fresh.SegmentFact(unknown.filesystem.SegmentOrdinal())
		if factErr != nil {
			return JournalCursor{}, nil, j.reconcileObservationFailure(factErr, "generation-journal-reconcile-segment-fact")
		}
		repaired, repairErr := j.truncateReconciledTailsLocked(ctx, fresh, segmentFact.Size, unknown.filesystem.IndexPreviousSize())
		if repairErr != nil {
			return JournalCursor{}, nil, repairErr
		}
		resynced, resyncErr := j.resyncReconciledSnapshotLocked(ctx, repaired)
		if resyncErr != nil {
			return JournalCursor{}, nil, resyncErr
		}
		checkpointed, checkpointErr := j.appendReconciledCheckpointLocked(ctx, resynced, unknown.prepared)
		if checkpointErr != nil {
			return JournalCursor{}, nil, checkpointErr
		}
		return j.installReconciledKnownLocked(checkpointed, unknown.prepared, true)
	case evidencefs.GenerationAppendReconcileCompositeComplete:
		resynced, resyncErr := j.resyncReconciledSnapshotLocked(ctx, fresh)
		if resyncErr != nil {
			return JournalCursor{}, nil, resyncErr
		}
		return j.installReconciledKnownLocked(resynced, unknown.prepared, true)
	default:
		return JournalCursor{}, nil, j.failLocked(evidencefs.ErrCorrupt, "generation-journal-reconcile-classification")
	}
}

func (j *generationEvidenceJournal) reconcileObservationFailure(cause error, operation string) error {
	mapped := mapEvidenceAdmissionError(cause, operation)
	if IsCode(mapped, CodeContextCanceled) || IsCode(mapped, CodeDeadlineExceeded) {
		return mapped
	}
	return j.failLocked(cause, operation)
}

func (j *generationEvidenceJournal) reconcileTransitionFailure(outcome evidencefs.AdmissionTransitionOutcome, cause error, operation string) error {
	switch outcome {
	case evidencefs.AdmissionTransitionUnknown:
		if j.lease.Active() {
			return admissionPostMutationFailure(operation)
		}
		return j.failLocked(evidencefs.ErrUnknown, operation)
	case evidencefs.AdmissionTransitionPreMutationFailure:
		mapped := mapEvidenceAdmissionError(cause, operation)
		if IsCode(mapped, CodeContextCanceled) || IsCode(mapped, CodeDeadlineExceeded) {
			return mapped
		}
		return j.failLocked(cause, operation)
	default:
		return j.failLocked(evidencefs.ErrUnknown, operation)
	}
}

func (j *generationEvidenceJournal) truncateReconciledTailsLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot, segmentSize, indexSize uint64) (*evidencefs.GenerationSnapshot, error) {
	return j.truncateReconciledTailsAtLocked(ctx, snapshot, segmentSize, indexSize, j.state.unknown.filesystem.SegmentOrdinal(), "generation-journal-reconcile-truncate")
}

func (j *generationEvidenceJournal) truncateReconciledTailsAtLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot, segmentSize, indexSize uint64, segmentOrdinal uint32, operation string) (*evidencefs.GenerationSnapshot, error) {
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return nil, j.reconcileObservationFailure(err, operation+"-identity")
	}
	result, err := j.lease.TruncateGenerationTails(ctx, snapshot, segmentSize, indexSize)
	if result.Outcome() != evidencefs.AdmissionTransitionDurable || err != nil || !result.ValidFor(j.lease) || result.PreviousSnapshotIdentity() != identity || result.SegmentOrdinal() != segmentOrdinal || result.SegmentNextSize() != segmentSize || result.IndexNextSize() != indexSize {
		return nil, j.reconcileTransitionFailure(result.Outcome(), err, operation)
	}
	return result.Snapshot(), nil
}

func (j *generationEvidenceJournal) resyncReconciledSnapshotLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot) (*evidencefs.GenerationSnapshot, error) {
	return j.resyncReconciledSnapshotAtLocked(ctx, snapshot, j.state.unknown.filesystem.SegmentOrdinal(), "generation-journal-reconcile-resync")
}

func (j *generationEvidenceJournal) resyncReconciledSnapshotAtLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot, segmentOrdinal uint32, operation string) (*evidencefs.GenerationSnapshot, error) {
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return nil, j.reconcileObservationFailure(err, operation+"-identity")
	}
	result, err := j.lease.ResyncGenerationSnapshot(ctx, snapshot)
	if result.Outcome() != evidencefs.AdmissionTransitionDurable || err != nil || !result.ValidFor(j.lease) || result.PreviousSnapshotIdentity() != identity || result.NextSnapshotIdentity() != identity || result.SegmentOrdinal() != segmentOrdinal {
		return nil, j.reconcileTransitionFailure(result.Outcome(), err, operation)
	}
	return result.Snapshot(), nil
}

func (j *generationEvidenceJournal) appendReconciledCheckpointLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot, prepared *preparedGenerationJournalAppend) (*evidencefs.GenerationSnapshot, error) {
	if prepared == nil || prepared.canonical == ([32]byte{}) || prepared.canonical != preparedGenerationJournalAppendDigest(prepared) {
		return nil, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-reconcile-checkpoint-candidate")
	}
	return j.appendReconciledCheckpointBytesLocked(ctx, snapshot, prepared.checkpointFramed, "generation-journal-reconcile-checkpoint")
}

func (j *generationEvidenceJournal) appendReconciledCheckpointBytesLocked(ctx context.Context, snapshot *evidencefs.GenerationSnapshot, framed []byte, operation string) (*evidencefs.GenerationSnapshot, error) {
	identity, err := snapshot.IdentityDigest()
	if err != nil {
		return nil, j.reconcileObservationFailure(err, operation+"-identity")
	}
	indexFact, err := snapshot.IndexFact()
	if err != nil {
		return nil, j.reconcileObservationFailure(err, operation+"-fact")
	}
	result, err := j.lease.AppendGenerationCheckpoint(ctx, snapshot, framed)
	if result.Outcome() != evidencefs.AdmissionTransitionDurable || err != nil || !result.ValidFor(j.lease) || result.PreviousSnapshotIdentity() != identity || result.IndexPreviousSize() != indexFact.Size || result.CheckpointFramedDigest() != sha256.Sum256(framed) {
		return nil, j.reconcileTransitionFailure(result.Outcome(), err, operation)
	}
	return result.Snapshot(), nil
}

func (j *generationEvidenceJournal) installReconciledKnownLocked(snapshot *evidencefs.GenerationSnapshot, prepared *preparedGenerationJournalAppend, candidate bool) (JournalCursor, *RecoverySnapshot, error) {
	progress := preparedGenerationJournalPrevious
	if candidate {
		progress = preparedGenerationJournalCandidate
	}
	return j.installReconciledKnownProgressLocked(snapshot, prepared, progress)
}

func (j *generationEvidenceJournal) installReconciledKnownProgressLocked(snapshot *evidencefs.GenerationSnapshot, prepared *preparedGenerationJournalAppend, progress preparedGenerationJournalProgress) (JournalCursor, *RecoverySnapshot, error) {
	next, err := j.knownStateFromSnapshotLocked(snapshot, prepared, progress)
	if err != nil {
		return JournalCursor{}, nil, j.failLocked(err, "generation-journal-reconcile-state")
	}
	if !j.installStateLocked(next) {
		if next.cursor.valid != nil {
			next.cursor.valid.Store(false)
		}
		return JournalCursor{}, nil, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-reconcile-seal")
	}
	return j.state.cursor.clone(), cloneRecoverySnapshot(j.state.recovery), nil
}
