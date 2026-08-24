package evidencefs

import (
	"context"
	"encoding/hex"
)

// GenerationResyncResult is the closed outcome of redoing durability barriers
// for the current final segment and lineage index without changing bytes.
// Durable owns a replacement snapshot with the same canonical identity.
type GenerationResyncResult struct {
	outcome                  AdmissionTransitionOutcome
	snapshot                 *GenerationSnapshot
	previousSnapshotIdentity [32]byte
	nextSnapshotIdentity     [32]byte
	segmentOrdinal           uint32
}

func (r GenerationResyncResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationResyncResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationResyncResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationResyncResult) NextSnapshotIdentity() [32]byte { return r.nextSnapshotIdentity }
func (r GenerationResyncResult) SegmentOrdinal() uint32         { return r.segmentOrdinal }

func (r GenerationResyncResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationResyncResult) validForLocked(lease *GenerationLease) bool {
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity == r.previousSnapshotIdentity && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// ResyncGenerationSnapshot revalidates the complete snapshot, redoes the final
// segment fdatasync followed by the lineage-index fdatasync, and revalidates the
// complete snapshot again. It is the replay proof for already-complete bytes;
// it never appends, truncates, rotates, or interprets frames. Any uncertainty
// after the first sync attempt invalidates the old snapshot and returns unknown.
func (l *GenerationLease) ResyncGenerationSnapshot(ctx context.Context, snapshot *GenerationSnapshot) (GenerationResyncResult, error) {
	result := GenerationResyncResult{outcome: AdmissionTransitionPreMutationFailure}
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
	result.previousSnapshotIdentity = snapshot.canonical
	result.segmentOrdinal = uint32(len(snapshot.segments) - 1)

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
		return result, corrupt("generation-resync-prefix-snapshot")
	}

	rootFD, err := l.store.freshRoot()
	if err != nil {
		l.valid = false
		invalidateGenerationSnapshotsLocked(l)
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
			return filesystem("generation-resync-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationResyncResult, error) {
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
			err = corrupt("generation-resync-lineage-lock")
		}
		return finishFailure(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return finishFailure(filesystem("generation-resync-index-open"))
	}
	if _, err = readGenerationAppendFile(ctx, l, indexFD, snapshot.index); err != nil {
		return finishFailure(err)
	}
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, hex.EncodeToString(l.journal[:]))
	if err != nil {
		return finishFailure(err)
	}
	journalLock, err := l.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, l.generation.stat) {
		if err == nil {
			err = corrupt("generation-resync-journal-lock")
		}
		return finishFailure(err)
	}
	segmentFD, err = l.store.ops.openFileAtReadWrite(journalFD, admissionSegmentName(int(result.segmentOrdinal)))
	if err != nil {
		return finishFailure(filesystem("generation-resync-segment-open"))
	}
	if _, err = readGenerationAppendFile(ctx, l, segmentFD, snapshot.segments[result.segmentOrdinal]); err != nil {
		return finishFailure(err)
	}

	attempted = true
	if l.store.ops.fdatasync(segmentFD) != nil {
		return finishFailure(filesystem("generation-resync-segment-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return finishFailure(filesystem("generation-resync-index-sync"))
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
	if !sameGenerationSnapshotFile(index, snapshot.index) || !sameGenerationSnapshotFiles(segments, snapshot.segments) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(corrupt("generation-resync-terminal-snapshot"))
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
