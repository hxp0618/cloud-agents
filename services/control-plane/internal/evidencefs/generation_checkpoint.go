package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// GenerationCheckpointAppendResult is the closed filesystem outcome for one
// index-only checkpoint append under an exact retained generation lease. It
// assigns no lineage/C3 meaning to the opaque framed bytes. Only durable owns
// the replacement snapshot.
type GenerationCheckpointAppendResult struct {
	outcome                  AdmissionTransitionOutcome
	snapshot                 *GenerationSnapshot
	checkpointFramedDigest   [32]byte
	previousSnapshotIdentity [32]byte
	nextSnapshotIdentity     [32]byte
	indexPreviousSize        uint64
}

func (r GenerationCheckpointAppendResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationCheckpointAppendResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationCheckpointAppendResult) CheckpointFramedDigest() [32]byte {
	return r.checkpointFramedDigest
}
func (r GenerationCheckpointAppendResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationCheckpointAppendResult) NextSnapshotIdentity() [32]byte {
	return r.nextSnapshotIdentity
}
func (r GenerationCheckpointAppendResult) IndexPreviousSize() uint64 {
	return r.indexPreviousSize
}

func (r GenerationCheckpointAppendResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationCheckpointAppendResult) validForLocked(lease *GenerationLease) bool {
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && r.checkpointFramedDigest != ([32]byte{}) && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity != ([32]byte{}) && r.previousSnapshotIdentity != r.nextSnapshotIdentity && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// AppendGenerationCheckpoint appends opaque checkpoint bytes to the current
// lineage index and makes the file durable. It is intended only for replay
// healing after the upper layer has revalidated and re-synced an exact journal
// linear extension. Any failure after entering the write phase is unknown and
// invalidates the old snapshot.
func (l *GenerationLease) AppendGenerationCheckpoint(ctx context.Context, snapshot *GenerationSnapshot, checkpointFramed []byte) (GenerationCheckpointAppendResult, error) {
	result := GenerationCheckpointAppendResult{outcome: AdmissionTransitionPreMutationFailure}
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
	if len(checkpointFramed) == 0 || uint64(len(checkpointFramed)) > maximumAdmissionIndexBytes {
		return result, ErrInvalidInput
	}
	indexExpected := snapshot.index
	result.previousSnapshotIdentity = snapshot.canonical
	result.indexPreviousSize = indexExpected.stat.size
	if indexExpected.stat.size > maximumAdmissionIndexBytes-uint64(len(checkpointFramed)) {
		return result, ErrLimit
	}
	checkpointFramed = append([]byte(nil), checkpointFramed...)
	result.checkpointFramedDigest = sha256.Sum256(checkpointFramed)

	currentIndex, currentSegments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		if !isContextError(err) {
			l.valid = false
			invalidateGenerationSnapshotsLocked(l)
		}
		return result, err
	}
	if !sameGenerationSnapshotFile(currentIndex, snapshot.index) || !sameGenerationSnapshotFiles(currentSegments, snapshot.segments) {
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
		return result, corrupt("generation-checkpoint-prefix-snapshot")
	}

	rootFD, err := l.store.freshRoot()
	if err != nil {
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
		return result, err
	}
	lineagesFD, lineageFD, journalFD, indexFD := -1, -1, -1, -1
	attempted := false
	cleanup := func() error {
		failed := false
		for _, fd := range []int{indexFD, journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-checkpoint-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationCheckpointAppendResult, error) {
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
			err = corrupt("generation-checkpoint-lineage-lock")
		}
		return finishFailure(err)
	}
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, hex.EncodeToString(l.journal[:]))
	if err != nil {
		return finishFailure(err)
	}
	journalLock, err := l.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, l.generation.stat) {
		if err == nil {
			err = corrupt("generation-checkpoint-journal-lock")
		}
		return finishFailure(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return finishFailure(filesystem("generation-checkpoint-index-open"))
	}
	indexBytes, err := readGenerationAppendFile(ctx, l, indexFD, indexExpected)
	if err != nil {
		return finishFailure(err)
	}

	attempted = true
	if err := appendGenerationBytes(ctx, l, indexFD, indexExpected.stat.size, checkpointFramed); err != nil {
		return finishFailure(err)
	}
	indexAfter, err := l.store.ops.fstat(indexFD)
	if err != nil || !generationAppendIdentity(indexExpected.stat, indexAfter, uint64(len(checkpointFramed)), l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-checkpoint-index-size"))
	}
	expectedIndex := appendedGenerationSnapshotFile(indexExpected, indexAfter, indexBytes, checkpointFramed)
	if !validCompactGenerationSnapshotFile(expectedIndex) {
		return finishFailure(filesystem("generation-checkpoint-index-fact"))
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return finishFailure(filesystem("generation-checkpoint-index-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := cleanup(); err != nil {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	rootFD, lineagesFD, lineageFD, journalFD, indexFD = -1, -1, -1, -1, -1
	index, segments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(err)
	}
	if !sameGenerationSnapshotFile(index, expectedIndex) || !sameGenerationSnapshotFiles(segments, snapshot.segments) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(corrupt("generation-checkpoint-terminal-snapshot"))
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
	return result, nil
}
