package migration

import (
	"context"
	"crypto/sha256"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

func (j *generationEvidenceJournal) appendRotatedPreparedLocked(ctx context.Context, cursor JournalCursor, prepared *preparedGenerationJournalAppend) (AppendResult, error) {
	if prepared == nil || prepared.rotation == nil || prepared.canonical == ([32]byte{}) || prepared.canonical != preparedGenerationJournalAppendDigest(prepared) {
		return AppendResult{}, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-rotation-candidate")
	}
	rotation := prepared.rotation
	result, filesystemErr := j.lease.AppendRotatedSegmentComposite(ctx, j.state.snapshot, rotation.headerFramed, rotation.headerCheckpointFramed, prepared.framed, prepared.checkpointFramed)
	switch result.Outcome() {
	case evidencefs.AdmissionTransitionPreMutationFailure:
		return j.restoreAfterPreMutationFailureLocked(prepared, filesystemErr)
	case evidencefs.AdmissionTransitionUnknown:
		return j.installUnknownRotationLocked(prepared, result, filesystemErr)
	case evidencefs.AdmissionTransitionDurable:
		if filesystemErr != nil || !result.ValidFor(j.lease) {
			prepared.invalidate()
			j.state.cursor.valid.Store(false)
			return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-rotation-bind")
		}
	default:
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-rotation-outcome")
	}
	if result.PreviousSnapshotIdentity() != j.state.snapshotIdentity || result.SegmentOrdinal() != rotation.headerCursor.segmentIndex || result.IndexPreviousSize() != j.state.indexFact.Size || result.RotationHeaderFramedDigest() != sha256.Sum256(rotation.headerFramed) || result.RotationCheckpointFramedDigest() != sha256.Sum256(rotation.headerCheckpointFramed) || result.CallerFramedDigest() != sha256.Sum256(prepared.framed) || result.CallerCheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-rotation-bind")
	}
	nextState, err := j.knownStateFromSnapshotLocked(result.Snapshot(), prepared, preparedGenerationJournalCandidate)
	if err != nil {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(err, "generation-journal-rotation-state")
	}
	appendResult, err := finishConsumedRotationAppend(cursor, j.generation, appendOutcomeDurable, &prepared.nextCursor, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest, rotation.header.RecordDigest, rotation.headerCheckpoint.RecordDigest)
	if err != nil {
		prepared.invalidate()
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(err, "generation-journal-rotation-result")
	}
	if !j.installStateLocked(nextState) {
		prepared.invalidate()
		return AppendResult{}, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-rotation-seal")
	}
	return appendResult, nil
}

func (j *generationEvidenceJournal) installUnknownRotationLocked(prepared *preparedGenerationJournalAppend, filesystemResult evidencefs.GenerationRotationResult, cause error) (AppendResult, error) {
	if prepared == nil || prepared.rotation == nil || filesystemResult.Outcome() != evidencefs.AdmissionTransitionUnknown || filesystemResult.Snapshot() != nil || filesystemResult.NextSnapshotIdentity() != ([32]byte{}) || filesystemResult.PreviousSnapshotIdentity() != j.state.snapshotIdentity || filesystemResult.SegmentOrdinal() != prepared.rotation.headerCursor.segmentIndex || filesystemResult.IndexPreviousSize() != j.state.indexFact.Size || filesystemResult.RotationHeaderFramedDigest() != sha256.Sum256(prepared.rotation.headerFramed) || filesystemResult.RotationCheckpointFramedDigest() != sha256.Sum256(prepared.rotation.headerCheckpointFramed) || filesystemResult.CallerFramedDigest() != sha256.Sum256(prepared.framed) || filesystemResult.CallerCheckpointFramedDigest() != sha256.Sum256(prepared.checkpointFramed) {
		if prepared != nil {
			prepared.invalidate()
		}
		return AppendResult{}, j.failLocked(evidencefs.ErrUnknown, "generation-journal-rotation-unknown-shape")
	}
	prepared.invalidate()
	appendResult, finishErr := finishConsumedRotationAppend(j.state.cursor, j.generation, appendOutcomeUnknown, nil, prepared.frame.RecordDigest, prepared.checkpoint.RecordDigest, prepared.rotation.header.RecordDigest, prepared.rotation.headerCheckpoint.RecordDigest)
	if finishErr != nil {
		j.state.cursor.valid.Store(false)
		return AppendResult{}, j.failLocked(finishErr, "generation-journal-rotation-result")
	}
	next := cloneGenerationEvidenceJournalState(j.state)
	next.snapshot = nil
	next.snapshotIdentity = [32]byte{}
	next.recovery = nil
	next.cursor.valid.Store(false)
	copyResult := filesystemResult
	next.unknown = &generationJournalUnknownAppend{rotation: &copyResult, prepared: prepared}
	if !j.installStateLocked(next) {
		return AppendResult{}, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-rotation-unknown-seal")
	}
	_ = cause
	return appendResult, admissionPostMutationFailure("generation-journal-rotation")
}

func (j *generationEvidenceJournal) reconcileUnknownRotationLocked(ctx context.Context) (JournalCursor, *RecoverySnapshot, error) {
	if j == nil || j.state == nil || j.state.unknown == nil || j.state.unknown.rotation == nil || j.state.unknown.prepared == nil || j.state.unknown.prepared.rotation == nil {
		return JournalCursor{}, nil, j.failLocked(evidencefs.ErrLeaseInvalid, "generation-journal-rotation-reconcile-state")
	}
	unknown := j.state.unknown
	rotationResult := *unknown.rotation
	classification, err := rotationResult.Reconcile(ctx, j.lease)
	if err != nil {
		return JournalCursor{}, nil, j.reconcileObservationFailure(err, "generation-journal-rotation-reconcile-classify")
	}
	snapshot := func(operation string) (*evidencefs.GenerationSnapshot, error) {
		fresh, snapshotErr := j.lease.Snapshot(ctx)
		if snapshotErr != nil {
			return nil, j.reconcileObservationFailure(snapshotErr, operation)
		}
		return fresh, nil
	}
	install := func(fresh *evidencefs.GenerationSnapshot, progress preparedGenerationJournalProgress) (JournalCursor, *RecoverySnapshot, error) {
		return j.installReconciledKnownProgressLocked(fresh, unknown.prepared, progress)
	}
	resync := func(fresh *evidencefs.GenerationSnapshot) (*evidencefs.GenerationSnapshot, error) {
		return j.resyncReconciledSnapshotAtLocked(ctx, fresh, rotationResult.SegmentOrdinal(), "generation-journal-rotation-reconcile-resync")
	}
	appendCheckpoint := func(fresh *evidencefs.GenerationSnapshot, framed []byte, stage string) (*evidencefs.GenerationSnapshot, error) {
		return j.appendReconciledCheckpointBytesLocked(ctx, fresh, framed, "generation-journal-rotation-reconcile-"+stage+"-checkpoint")
	}

	switch classification {
	case evidencefs.GenerationRotationReconcileSegmentAbsent:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-previous-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		return install(fresh, preparedGenerationJournalPrevious)
	case evidencefs.GenerationRotationReconcileSegmentEmpty, evidencefs.GenerationRotationReconcileHeaderTorn:
		discarded, discardErr := rotationResult.DiscardIncompleteSegment(ctx, j.lease)
		if discarded.Outcome() != evidencefs.AdmissionTransitionDurable || discardErr != nil || !discarded.ValidFor(j.lease) || discarded.PreviousSnapshotIdentity() != rotationResult.PreviousSnapshotIdentity() || discarded.NextSnapshotIdentity() != rotationResult.PreviousSnapshotIdentity() || discarded.SegmentOrdinal() != rotationResult.SegmentOrdinal() {
			return JournalCursor{}, nil, j.reconcileTransitionFailure(discarded.Outcome(), discardErr, "generation-journal-rotation-reconcile-discard")
		}
		return install(discarded.Snapshot(), preparedGenerationJournalPrevious)
	case evidencefs.GenerationRotationReconcileHeaderComplete:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-header-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		fresh, err = resync(fresh)
		if err == nil {
			fresh, err = appendCheckpoint(fresh, unknown.prepared.rotation.headerCheckpointFramed, "header")
		}
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalRotationHeader)
	case evidencefs.GenerationRotationReconcileHeaderCheckpointTorn:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-header-checkpoint-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		segmentFact, factErr := fresh.SegmentFact(rotationResult.SegmentOrdinal())
		if factErr != nil {
			return JournalCursor{}, nil, j.reconcileObservationFailure(factErr, "generation-journal-rotation-reconcile-header-segment-fact")
		}
		fresh, err = j.truncateReconciledTailsAtLocked(ctx, fresh, segmentFact.Size, rotationResult.IndexPreviousSize(), rotationResult.SegmentOrdinal(), "generation-journal-rotation-reconcile-header-checkpoint-truncate")
		if err == nil {
			fresh, err = resync(fresh)
		}
		if err == nil {
			fresh, err = appendCheckpoint(fresh, unknown.prepared.rotation.headerCheckpointFramed, "header")
		}
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalRotationHeader)
	case evidencefs.GenerationRotationReconcileHeaderCompositeComplete:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-header-composite-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		fresh, err = resync(fresh)
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalRotationHeader)
	case evidencefs.GenerationRotationReconcileCallerTorn:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-caller-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		headerIndexSize, addErr := admissionCheckedAdd(rotationResult.IndexPreviousSize(), uint64(len(unknown.prepared.rotation.headerCheckpointFramed)))
		if addErr != nil {
			return JournalCursor{}, nil, j.failLocked(addErr, "generation-journal-rotation-reconcile-header-index-size")
		}
		fresh, err = j.truncateReconciledTailsAtLocked(ctx, fresh, uint64(len(unknown.prepared.rotation.headerFramed)), headerIndexSize, rotationResult.SegmentOrdinal(), "generation-journal-rotation-reconcile-caller-truncate")
		if err == nil {
			fresh, err = resync(fresh)
		}
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalRotationHeader)
	case evidencefs.GenerationRotationReconcileCallerComplete:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-caller-complete-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		fresh, err = resync(fresh)
		if err == nil {
			fresh, err = appendCheckpoint(fresh, unknown.prepared.checkpointFramed, "caller")
		}
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalCandidate)
	case evidencefs.GenerationRotationReconcileCallerCheckpointTorn:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-caller-checkpoint-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		segmentFact, factErr := fresh.SegmentFact(rotationResult.SegmentOrdinal())
		if factErr != nil {
			return JournalCursor{}, nil, j.reconcileObservationFailure(factErr, "generation-journal-rotation-reconcile-caller-segment-fact")
		}
		headerIndexSize, addErr := admissionCheckedAdd(rotationResult.IndexPreviousSize(), uint64(len(unknown.prepared.rotation.headerCheckpointFramed)))
		if addErr != nil {
			return JournalCursor{}, nil, j.failLocked(addErr, "generation-journal-rotation-reconcile-header-index-size")
		}
		fresh, err = j.truncateReconciledTailsAtLocked(ctx, fresh, segmentFact.Size, headerIndexSize, rotationResult.SegmentOrdinal(), "generation-journal-rotation-reconcile-caller-checkpoint-truncate")
		if err == nil {
			fresh, err = resync(fresh)
		}
		if err == nil {
			fresh, err = appendCheckpoint(fresh, unknown.prepared.checkpointFramed, "caller")
		}
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalCandidate)
	case evidencefs.GenerationRotationReconcileCompositeComplete:
		fresh, snapshotErr := snapshot("generation-journal-rotation-reconcile-composite-snapshot")
		if snapshotErr != nil {
			return JournalCursor{}, nil, snapshotErr
		}
		fresh, err = resync(fresh)
		if err != nil {
			return JournalCursor{}, nil, err
		}
		return install(fresh, preparedGenerationJournalCandidate)
	default:
		return JournalCursor{}, nil, j.failLocked(evidencefs.ErrCorrupt, "generation-journal-rotation-reconcile-classification")
	}
}

func (j *generationEvidenceJournal) prepareRotatedAppendLocked(cursor JournalCursor, inspected []EvidenceFrame, chain verifiedEvidenceChainWitness, inspectedCandidate EvidenceFrame) (*preparedGenerationJournalAppend, error) {
	if j == nil || j.state == nil || len(inspected) < 2 || inspectedCandidate.RecordKind == EvidenceRecordHeader || cursor.previousRecordDigest == nil || cursor.segmentIndex == ^uint32(0) || uint32(len(j.state.segmentFacts)) != cursor.segmentIndex+1 || len(j.state.segmentFacts) >= int(j.reservation.ReservedSegments) || uint32(len(j.state.segmentFacts)) >= maxEvidenceReservedSegments {
		return nil, fail(CodeEvidenceJournalLimitExceeded, "generation-journal-rotation", "candidate cannot enter another reserved segment", nil)
	}
	profile, ok := generationJournalLimitsProfile(j)
	if !ok {
		return nil, fail(CodeEvidenceRecoveryRequired, "generation-journal-rotation", "generation quota profile is unavailable", nil)
	}
	prefix := cloneProjectionValue(inspected[:len(inspected)-1])
	baseHeader := prefix[0].Record.Header
	if baseHeader == nil || baseHeader.SegmentIndex != 0 || !sameGenerationHeader(j.generation, *baseHeader) {
		return nil, invalidEvidence("generation-journal-rotation", "segment-zero header is unavailable")
	}
	header := cloneProjectionValue(*baseHeader)
	header.SegmentIndex = cursor.segmentIndex + 1
	header.PreviousSegmentRecordDigest = cloneDigestPointer(cursor.previousRecordDigest)
	headerFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: cursor.nextSequence,
		PreviousRecordDigest: cloneDigestPointer(cursor.previousRecordDigest), RecordKind: EvidenceRecordHeader,
		Record: EvidenceRecord{Header: &header},
	}
	var err error
	headerFrame.RecordDigest, err = headerFrame.ComputeDigest()
	if err != nil || headerFrame.Validate() != nil {
		return nil, invalidEvidence("generation-journal-rotation", "rotation header frame is invalid")
	}
	headerFramed, err := EncodeCanonicalEvidenceFrame(headerFrame)
	if err != nil {
		return nil, err
	}
	headerFrames := append(prefix, headerFrame)
	if err := validateEvidenceChainWithWitness(headerFrames, chain); err != nil {
		return nil, err
	}
	headerSummary, err := summarizeEvidenceJournal(headerFrames)
	if err != nil {
		return nil, err
	}
	headerCheckpoint, headerCheckpointFramed, err := buildGenerationJournalCheckpoint(j.generation, cursor, headerFrame, headerSummary, profile)
	if err != nil {
		return nil, err
	}
	headerCursor, err := advanceGenerationJournalCursor(cursor, headerFrame.RecordDigest, headerCheckpoint.RecordDigest)
	if err != nil {
		return nil, err
	}
	headerCursor.segmentIndex = header.SegmentIndex

	callerFrame := EvidenceFrame{
		FormatVersion: EvidenceFrameFormat, Sequence: headerCursor.nextSequence,
		PreviousRecordDigest: digestPointer(headerFrame.RecordDigest), RecordKind: inspectedCandidate.RecordKind,
		Record: cloneEvidenceRecord(inspectedCandidate.Record),
	}
	callerFrame.RecordDigest, err = callerFrame.ComputeDigest()
	if err != nil || callerFrame.Validate() != nil {
		headerCursor.valid.Store(false)
		return nil, invalidEvidence("generation-journal-rotation", "caller frame is invalid after rotation")
	}
	callerFramed, err := EncodeCanonicalEvidenceFrame(callerFrame)
	if err != nil {
		headerCursor.valid.Store(false)
		return nil, err
	}
	frames := append(headerFrames, callerFrame)
	if err := validateEvidenceChainWithWitness(frames, chain); err != nil {
		headerCursor.valid.Store(false)
		return nil, err
	}
	callerSummary, err := summarizeEvidenceJournal(frames)
	if err != nil {
		headerCursor.valid.Store(false)
		return nil, err
	}
	callerCheckpoint, callerCheckpointFramed, err := buildGenerationJournalCheckpoint(j.generation, headerCursor, callerFrame, callerSummary, profile)
	if err != nil {
		headerCursor.valid.Store(false)
		return nil, err
	}
	nextCursor, err := advanceGenerationJournalCursor(headerCursor, callerFrame.RecordDigest, callerCheckpoint.RecordDigest)
	if err != nil {
		headerCursor.valid.Store(false)
		return nil, err
	}

	headerSchema := cloneGenerationJournalSchema(j.schema)
	headerSchema.chainWitness = chain
	if err := refreshGenerationJournalObservedLedger(&headerSchema, headerFrames); err != nil {
		headerCursor.valid.Store(false)
		nextCursor.valid.Store(false)
		return nil, err
	}
	headerRecovery, err := buildRecoverySnapshot(headerFrames, headerCursor, j.generation, recoveredContinuation{}, headerSchema)
	if err != nil {
		headerCursor.valid.Store(false)
		nextCursor.valid.Store(false)
		return nil, err
	}
	candidateSchema := cloneGenerationJournalSchema(j.schema)
	candidateSchema.chainWitness = chain
	if err := refreshGenerationJournalObservedLedger(&candidateSchema, frames); err != nil {
		headerCursor.valid.Store(false)
		nextCursor.valid.Store(false)
		return nil, err
	}
	recovery, err := buildRecoverySnapshot(frames, nextCursor, j.generation, recoveredContinuation{}, candidateSchema)
	if err != nil {
		headerCursor.valid.Store(false)
		nextCursor.valid.Store(false)
		return nil, err
	}

	headerState := &preparedGenerationJournalRotation{
		header: headerFrame, headerFramed: headerFramed, headerCheckpoint: headerCheckpoint,
		headerCheckpointFramed: headerCheckpointFramed, headerCursor: headerCursor, headerRecovery: headerRecovery,
		segmentRecords: 1, segmentBytes: uint64(len(headerFramed)),
	}
	headerState.journalRecords, err = admissionCheckedAdd(j.state.journalRecords, 1)
	if err == nil {
		headerState.journalBytes, err = admissionCheckedAdd(j.state.journalBytes, uint64(len(headerFramed)))
	}
	if err == nil {
		headerState.checkpointRecords, err = admissionCheckedAdd(j.state.checkpointRecords, 1)
	}
	if err == nil {
		headerState.indexDebitRecords, err = admissionCheckedAdd(j.state.indexDebitRecords, 1)
	}
	if err == nil {
		headerState.indexDebitBytes, err = admissionCheckedAdd(j.state.indexDebitBytes, uint64(len(headerCheckpointFramed)))
	}
	if err != nil {
		headerCursor.valid.Store(false)
		nextCursor.valid.Store(false)
		return nil, err
	}
	p := &preparedGenerationJournalAppend{
		limitsProfile: profile,
		frame:         callerFrame, framed: callerFramed, checkpoint: callerCheckpoint, checkpointFramed: callerCheckpointFramed,
		nextCursor: nextCursor, previousRecovery: cloneRecoverySnapshot(j.state.recovery), recovery: recovery,
		rotation: headerState,
	}
	p.journalRecords, err = admissionCheckedAdd(headerState.journalRecords, 1)
	if err == nil {
		p.journalBytes, err = admissionCheckedAdd(headerState.journalBytes, uint64(len(callerFramed)))
	}
	if err == nil {
		p.segmentRecords, err = admissionCheckedAdd(headerState.segmentRecords, 1)
	}
	if err == nil {
		p.segmentBytes, err = admissionCheckedAdd(headerState.segmentBytes, uint64(len(callerFramed)))
	}
	if err == nil {
		p.checkpointRecords, err = admissionCheckedAdd(headerState.checkpointRecords, 1)
	}
	if err == nil {
		p.indexDebitRecords, err = admissionCheckedAdd(headerState.indexDebitRecords, 1)
	}
	if err == nil {
		p.indexDebitBytes, err = admissionCheckedAdd(headerState.indexDebitBytes, uint64(len(callerCheckpointFramed)))
	}
	if err != nil {
		p.invalidate()
		return nil, err
	}
	if p.segmentRecords > evidenceSegmentMaximumRecords || p.segmentBytes > evidenceSegmentMaximumBytes || p.journalRecords > j.reservation.ReservedRecords || p.journalBytes > j.reservation.ReservedJournalBytes || p.checkpointRecords > j.reservation.ReservedCheckpointRecords || p.indexDebitRecords > j.reservation.ReservedIndexRecords || p.indexDebitBytes > j.reservation.ReservedIndexBytes {
		p.invalidate()
		return nil, fail(CodeEvidenceJournalLimitExceeded, "generation-journal-rotation", "rotation composite exceeds its verified generation reservation", nil)
	}
	p.canonical = preparedGenerationJournalAppendDigest(p)
	if p.canonical == ([32]byte{}) {
		p.invalidate()
		return nil, admissionFailed("generation-journal-rotation", "prepared rotation could not be sealed", nil)
	}
	return p, nil
}
