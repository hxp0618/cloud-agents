package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// GenerationRotationResult is the closed filesystem outcome for creating the
// next segment and durably appending its rotation header/checkpoint followed by
// one caller record/checkpoint. evidencefs treats all four payloads as opaque.
// Only durable owns the replacement snapshot; unknown retains private bytes for
// a later exact classification.
type GenerationRotationResult struct {
	outcome                        AdmissionTransitionOutcome
	snapshot                       *GenerationSnapshot
	previousSnapshotIdentity       [32]byte
	nextSnapshotIdentity           [32]byte
	segmentOrdinal                 uint32
	indexPreviousSize              uint64
	rotationHeaderFramedDigest     [32]byte
	rotationCheckpointFramedDigest [32]byte
	callerFramedDigest             [32]byte
	callerCheckpointFramedDigest   [32]byte
	previousIndex                  generationSnapshotFile
	previousSegments               []generationSnapshotFile
	rotationHeaderFramed           []byte
	rotationCheckpointFramed       []byte
	callerFramed                   []byte
	callerCheckpointFramed         []byte
}

func (r GenerationRotationResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationRotationResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationRotationResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationRotationResult) NextSnapshotIdentity() [32]byte { return r.nextSnapshotIdentity }
func (r GenerationRotationResult) SegmentOrdinal() uint32         { return r.segmentOrdinal }
func (r GenerationRotationResult) IndexPreviousSize() uint64      { return r.indexPreviousSize }
func (r GenerationRotationResult) RotationHeaderFramedDigest() [32]byte {
	return r.rotationHeaderFramedDigest
}
func (r GenerationRotationResult) RotationCheckpointFramedDigest() [32]byte {
	return r.rotationCheckpointFramedDigest
}
func (r GenerationRotationResult) CallerFramedDigest() [32]byte { return r.callerFramedDigest }
func (r GenerationRotationResult) CallerCheckpointFramedDigest() [32]byte {
	return r.callerCheckpointFramedDigest
}

func (r GenerationRotationResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationRotationResult) validForLocked(lease *GenerationLease) bool {
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity != ([32]byte{}) && r.previousSnapshotIdentity != r.nextSnapshotIdentity && r.rotationHeaderFramedDigest != ([32]byte{}) && r.rotationCheckpointFramedDigest != ([32]byte{}) && r.callerFramedDigest != ([32]byte{}) && r.callerCheckpointFramedDigest != ([32]byte{}) && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// AppendRotatedSegmentComposite creates the next deterministic segment and
// executes the exact durability order: empty file + directory entry, rotation
// header, header checkpoint, caller record, caller checkpoint. Any failure
// after the create attempt is unknown and invalidates the old snapshot.
func (l *GenerationLease) AppendRotatedSegmentComposite(ctx context.Context, snapshot *GenerationSnapshot, rotationHeaderFramed, rotationCheckpointFramed, callerFramed, callerCheckpointFramed []byte) (GenerationRotationResult, error) {
	result := GenerationRotationResult{outcome: AdmissionTransitionPreMutationFailure}
	if l == nil || l.self != l || l.seal == nil || l.mu == nil || snapshot == nil {
		return result, ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return result, err
	}
	if !l.activeLocked() || !snapshot.validLocked() || snapshot.lease != l || l.snapshot != snapshot {
		return result, ErrLeaseInvalid
	}
	if len(rotationHeaderFramed) == 0 || len(rotationCheckpointFramed) == 0 || len(callerFramed) == 0 || len(callerCheckpointFramed) == 0 {
		return result, ErrInvalidInput
	}
	headerBytes, callerBytes := uint64(len(rotationHeaderFramed)), uint64(len(callerFramed))
	headerCheckpointBytes, callerCheckpointBytes := uint64(len(rotationCheckpointFramed)), uint64(len(callerCheckpointFramed))
	if len(snapshot.segments) >= maximumAdmissionSegments || headerBytes > maximumAdmissionSegmentBytes || callerBytes > maximumAdmissionSegmentBytes-headerBytes || headerCheckpointBytes > maximumAdmissionIndexBytes || callerCheckpointBytes > maximumAdmissionIndexBytes-headerCheckpointBytes {
		return result, ErrLimit
	}
	indexExpected := snapshot.index
	if indexExpected.stat.size > maximumAdmissionIndexBytes-headerCheckpointBytes-callerCheckpointBytes {
		return result, ErrLimit
	}
	journalBytes := uint64(0)
	for _, segment := range snapshot.segments {
		if segment.stat.size > maximumAdmissionJournalBytes-journalBytes {
			return result, ErrLimit
		}
		journalBytes += segment.stat.size
	}
	if headerBytes > maximumAdmissionJournalBytes-journalBytes || callerBytes > maximumAdmissionJournalBytes-journalBytes-headerBytes {
		return result, ErrLimit
	}

	result.previousSnapshotIdentity = snapshot.canonical
	result.segmentOrdinal = uint32(len(snapshot.segments))
	result.indexPreviousSize = indexExpected.stat.size
	result.previousIndex = indexExpected
	result.previousSegments = append([]generationSnapshotFile(nil), snapshot.segments...)
	result.rotationHeaderFramed = append([]byte(nil), rotationHeaderFramed...)
	result.rotationCheckpointFramed = append([]byte(nil), rotationCheckpointFramed...)
	result.callerFramed = append([]byte(nil), callerFramed...)
	result.callerCheckpointFramed = append([]byte(nil), callerCheckpointFramed...)
	result.rotationHeaderFramedDigest = sha256.Sum256(result.rotationHeaderFramed)
	result.rotationCheckpointFramedDigest = sha256.Sum256(result.rotationCheckpointFramed)
	result.callerFramedDigest = sha256.Sum256(result.callerFramed)
	result.callerCheckpointFramedDigest = sha256.Sum256(result.callerCheckpointFramed)

	currentIndex, currentSegments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		if !isContextError(err) {
			l.valid = false
			invalidateGenerationSnapshotsLocked(l)
		}
		clearGenerationRotationReconcile(&result)
		return result, err
	}
	if !sameGenerationSnapshotFile(currentIndex, snapshot.index) || !sameGenerationSnapshotFiles(currentSegments, snapshot.segments) {
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
		clearGenerationRotationReconcile(&result)
		return result, corrupt("generation-rotation-prefix-snapshot")
	}

	rootFD, err := l.store.freshRoot()
	if err != nil {
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
		clearGenerationRotationReconcile(&result)
		return result, err
	}
	lineagesFD, lineageFD, journalFD, segmentFD, indexFD := -1, -1, -1, -1, -1
	attempted := false
	cleanup := func() error {
		failed := false
		for _, fd := range []int{indexFD, segmentFD, journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-rotation-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationRotationResult, error) {
		if cleanupErr := cleanup(); cleanupErr != nil {
			cause = cleanupErr
		}
		if attempted {
			invalidateGenerationSnapshotsLocked(l)
			result.outcome, result.snapshot = AdmissionTransitionUnknown, nil
			return result, unknown(cause)
		}
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
		clearGenerationRotationReconcile(&result)
		return result, cause
	}
	lineagesFD, _, err = l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return finishFailure(err)
	}
	lineageFD, _, err = l.store.openVerifiedDirectory(lineagesFD, hex.EncodeToString(l.target[:]))
	if err != nil {
		return finishFailure(err)
	}
	lineageLock, err := l.store.statVerifiedRegular(lineageFD, "writer.lock")
	if err != nil || !sameIdentity(lineageLock, l.lineage.stat) {
		if err == nil {
			err = corrupt("generation-rotation-lineage-lock")
		}
		return finishFailure(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return finishFailure(filesystem("generation-rotation-index-open"))
	}
	indexBytes, err := readGenerationAppendFile(ctx, l, indexFD, indexExpected)
	if err != nil {
		return finishFailure(err)
	}
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, hex.EncodeToString(l.journal[:]))
	if err != nil {
		return finishFailure(err)
	}
	journalLock, err := l.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, l.generation.stat) {
		if err == nil {
			err = corrupt("generation-rotation-journal-lock")
		}
		return finishFailure(err)
	}

	attempted = true
	segmentName := admissionSegmentName(int(result.segmentOrdinal))
	segmentFD, err = l.store.ops.openFileAt(journalFD, segmentName, true)
	if err != nil {
		return finishFailure(filesystem("generation-rotation-segment-create"))
	}
	segmentEmpty, err := l.store.ops.fstat(segmentFD)
	if err != nil || !validRegular(segmentEmpty, l.store.uid, l.store.identity.device) || segmentEmpty.size != 0 {
		return finishFailure(filesystem("generation-rotation-segment-empty"))
	}
	emptySyncFailed := l.store.ops.fdatasync(segmentFD) != nil
	directorySyncFailed := l.store.ops.fsync(journalFD) != nil
	if emptySyncFailed || directorySyncFailed {
		return finishFailure(filesystem("generation-rotation-segment-create-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := appendGenerationBytes(ctx, l, segmentFD, 0, result.rotationHeaderFramed); err != nil {
		return finishFailure(err)
	}
	segmentAfterHeader, err := l.store.ops.fstat(segmentFD)
	if err != nil || !generationAppendIdentity(segmentEmpty, segmentAfterHeader, headerBytes, l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-rotation-header-size"))
	}
	expectedHeader := createdGenerationSegmentSnapshotFile(result.segmentOrdinal, segmentAfterHeader, result.rotationHeaderFramed)
	if !validCompactGenerationSnapshotFile(expectedHeader) {
		return finishFailure(filesystem("generation-rotation-header-fact"))
	}
	if l.store.ops.fdatasync(segmentFD) != nil {
		return finishFailure(filesystem("generation-rotation-header-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := appendGenerationBytes(ctx, l, indexFD, indexExpected.stat.size, result.rotationCheckpointFramed); err != nil {
		return finishFailure(err)
	}
	indexAfterHeader, err := l.store.ops.fstat(indexFD)
	if err != nil || !generationAppendIdentity(indexExpected.stat, indexAfterHeader, headerCheckpointBytes, l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-rotation-header-checkpoint-size"))
	}
	expectedHeaderIndex := appendedGenerationSnapshotFile(indexExpected, indexAfterHeader, indexBytes, result.rotationCheckpointFramed)
	if !validCompactGenerationSnapshotFile(expectedHeaderIndex) {
		return finishFailure(filesystem("generation-rotation-header-checkpoint-fact"))
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return finishFailure(filesystem("generation-rotation-header-checkpoint-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := appendGenerationBytes(ctx, l, segmentFD, headerBytes, result.callerFramed); err != nil {
		return finishFailure(err)
	}
	segmentAfterCaller, err := l.store.ops.fstat(segmentFD)
	if err != nil || !generationAppendIdentity(segmentAfterHeader, segmentAfterCaller, callerBytes, l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-rotation-caller-size"))
	}
	expectedSegment := appendedGenerationSnapshotFile(expectedHeader, segmentAfterCaller, result.rotationHeaderFramed, result.callerFramed)
	if !validCompactGenerationSnapshotFile(expectedSegment) {
		return finishFailure(filesystem("generation-rotation-caller-fact"))
	}
	if l.store.ops.fdatasync(segmentFD) != nil {
		return finishFailure(filesystem("generation-rotation-caller-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	indexWithHeader := make([]byte, 0, len(indexBytes)+len(result.rotationCheckpointFramed))
	indexWithHeader = append(indexWithHeader, indexBytes...)
	indexWithHeader = append(indexWithHeader, result.rotationCheckpointFramed...)
	if err := appendGenerationBytes(ctx, l, indexFD, indexAfterHeader.size, result.callerCheckpointFramed); err != nil {
		return finishFailure(err)
	}
	indexAfterCaller, err := l.store.ops.fstat(indexFD)
	if err != nil || !generationAppendIdentity(indexAfterHeader, indexAfterCaller, callerCheckpointBytes, l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-rotation-caller-checkpoint-size"))
	}
	expectedIndex := appendedGenerationSnapshotFile(expectedHeaderIndex, indexAfterCaller, indexWithHeader, result.callerCheckpointFramed)
	if !validCompactGenerationSnapshotFile(expectedIndex) {
		return finishFailure(filesystem("generation-rotation-caller-checkpoint-fact"))
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return finishFailure(filesystem("generation-rotation-caller-checkpoint-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := cleanup(); err != nil {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	rootFD, lineagesFD, lineageFD, journalFD, segmentFD, indexFD = -1, -1, -1, -1, -1, -1
	index, segments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	expectedSegments := append([]generationSnapshotFile(nil), snapshot.segments...)
	expectedSegments = append(expectedSegments, expectedSegment)
	if !sameGenerationSnapshotFile(index, expectedIndex) || !sameGenerationSnapshotFiles(segments, expectedSegments) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(corrupt("generation-rotation-terminal-snapshot"))
	}
	next, err := l.mintGenerationSnapshotLocked(index, segments)
	if err != nil {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	result.outcome, result.snapshot = AdmissionTransitionDurable, next
	result.nextSnapshotIdentity = next.canonical
	if !result.validForLocked(l) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome, result.snapshot = AdmissionTransitionUnknown, nil
		return result, unknown(ErrLeaseInvalid)
	}
	clearGenerationRotationReconcile(&result)
	return result, nil
}

func createdGenerationSegmentSnapshotFile(ordinal uint32, stat fileStat, content []byte) generationSnapshotFile {
	if len(content) == 0 || stat.size != uint64(len(content)) {
		return generationSnapshotFile{}
	}
	digest := sha256.Sum256(content)
	name := admissionSegmentName(int(ordinal))
	return generationSnapshotFile{role: inventorySegment, ordinal: ordinal, stat: stat, digest: digest, identity: generationSnapshotFileIdentity(inventorySegment, name, ordinal, stat, digest)}
}

func clearGenerationRotationReconcile(result *GenerationRotationResult) {
	if result == nil {
		return
	}
	result.previousIndex = generationSnapshotFile{}
	result.previousSegments = nil
	result.rotationHeaderFramed = nil
	result.rotationCheckpointFramed = nil
	result.callerFramed = nil
	result.callerCheckpointFramed = nil
}
