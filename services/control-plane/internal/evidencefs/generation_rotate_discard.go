package evidencefs

import (
	"context"
	"encoding/hex"
)

// GenerationRotationDiscardResult is the closed outcome of removing only the
// response-lost final segment created by one exact unknown rotation. It is not
// a general segment deletion capability. Durable owns the restored pre-rotation
// snapshot; unknown leaves the original rotation result as the sole replay
// authority.
type GenerationRotationDiscardResult struct {
	outcome                  AdmissionTransitionOutcome
	snapshot                 *GenerationSnapshot
	previousSnapshotIdentity [32]byte
	nextSnapshotIdentity     [32]byte
	segmentOrdinal           uint32
}

func (r GenerationRotationDiscardResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationRotationDiscardResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationRotationDiscardResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationRotationDiscardResult) NextSnapshotIdentity() [32]byte {
	return r.nextSnapshotIdentity
}
func (r GenerationRotationDiscardResult) SegmentOrdinal() uint32 { return r.segmentOrdinal }

func (r GenerationRotationDiscardResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationRotationDiscardResult) validForLocked(lease *GenerationLease) bool {
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity == r.previousSnapshotIdentity && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// DiscardIncompleteSegment removes the exact new final segment only while the
// bound unknown rotation classifies it as empty or as a strict torn prefix of
// the proposed rotation header. The unlink is followed by fsync(journal-dir).
// Any uncertainty after the unlink attempt is unknown.
func (r GenerationRotationResult) DiscardIncompleteSegment(ctx context.Context, lease *GenerationLease) (GenerationRotationDiscardResult, error) {
	result := GenerationRotationDiscardResult{outcome: AdmissionTransitionPreMutationFailure}
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return result, ErrLeaseInvalid
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return result, err
	}
	if !r.validUnknownForLocked(lease) {
		return result, ErrLeaseInvalid
	}
	result.previousSnapshotIdentity = r.previousSnapshotIdentity
	result.segmentOrdinal = r.segmentOrdinal
	state, err := r.reconcileLocked(ctx, lease)
	if err != nil {
		return result, err
	}
	if state != GenerationRotationReconcileSegmentEmpty && state != GenerationRotationReconcileHeaderTorn {
		return result, ErrInvalidInput
	}

	rootFD, err := lease.store.freshRoot()
	if err != nil {
		lease.valid = false
		invalidateGenerationSnapshotsLocked(lease)
		return result, err
	}
	lineagesFD, lineageFD, journalFD := -1, -1, -1
	attempted := false
	cleanup := func() error {
		failed := false
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = lease.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-rotation-discard-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationRotationDiscardResult, error) {
		if cleanupErr := cleanup(); cleanupErr != nil {
			cause = cleanupErr
		}
		if attempted {
			invalidateGenerationSnapshotsLocked(lease)
			result.outcome, result.snapshot = AdmissionTransitionUnknown, nil
			return result, unknown(cause)
		}
		if !isContextError(cause) {
			lease.valid = false
			invalidateGenerationSnapshotsLocked(lease)
		}
		return result, cause
	}

	lineagesFD, _, err = lease.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return finishFailure(err)
	}
	lineageFD, _, err = lease.store.openVerifiedDirectory(lineagesFD, hex.EncodeToString(lease.target[:]))
	if err != nil {
		return finishFailure(err)
	}
	lineageLock, err := lease.store.statVerifiedRegular(lineageFD, "writer.lock")
	if err != nil || !sameIdentity(lineageLock, lease.lineage.stat) {
		if err == nil {
			err = corrupt("generation-rotation-discard-lineage-lock")
		}
		return finishFailure(err)
	}
	index, err := lease.readGenerationFile(ctx, lineageFD, "index.caj", inventoryIndex, 0, maximumAdmissionIndexBytes)
	if err != nil || !sameGenerationSnapshotFile(compactGenerationSnapshotFile(index), r.previousIndex) {
		if err == nil {
			err = corrupt("generation-rotation-discard-index")
		}
		return finishFailure(err)
	}
	journalFD, _, err = lease.store.openVerifiedDirectory(lineageFD, hex.EncodeToString(lease.journal[:]))
	if err != nil {
		return finishFailure(err)
	}
	journalLock, err := lease.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, lease.generation.stat) {
		if err == nil {
			err = corrupt("generation-rotation-discard-journal-lock")
		}
		return finishFailure(err)
	}
	segmentName := admissionSegmentName(int(r.segmentOrdinal))
	segment, err := readGenerationRotationTailFile(ctx, lease, journalFD, segmentName, r.segmentOrdinal)
	if err != nil {
		return finishFailure(err)
	}
	segmentState, ok := classifyGenerationRotationSuffix(segment.bytes, 0, r.rotationHeaderFramed, r.callerFramed)
	if !ok || segmentState != generationRotationSuffixAbsent && segmentState != generationRotationSuffixFirstPartial {
		return finishFailure(corrupt("generation-rotation-discard-segment"))
	}

	attempted = true
	if lease.store.ops.unlinkAt(journalFD, segmentName) != nil {
		return finishFailure(filesystem("generation-rotation-discard-unlink"))
	}
	if lease.store.ops.fsync(journalFD) != nil {
		return finishFailure(filesystem("generation-rotation-discard-directory-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := cleanup(); err != nil {
		invalidateGenerationSnapshotsLocked(lease)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	rootFD, lineagesFD, lineageFD, journalFD = -1, -1, -1, -1
	index, segments, err := lease.readGenerationSnapshotLocked(ctx)
	if err != nil || !sameGenerationSnapshotFile(index, r.previousIndex) || !sameGenerationSnapshotFiles(segments, r.previousSegments) {
		if err == nil {
			err = corrupt("generation-rotation-discard-terminal-snapshot")
		}
		invalidateGenerationSnapshotsLocked(lease)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	next, err := lease.mintGenerationSnapshotLocked(index, segments)
	if err != nil {
		invalidateGenerationSnapshotsLocked(lease)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	result.outcome, result.snapshot = AdmissionTransitionDurable, next
	result.nextSnapshotIdentity = next.canonical
	if !result.validForLocked(lease) {
		invalidateGenerationSnapshotsLocked(lease)
		result.outcome, result.snapshot = AdmissionTransitionUnknown, nil
		return result, unknown(ErrLeaseInvalid)
	}
	return result, nil
}
