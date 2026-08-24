package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// GenerationTailRepairResult is the closed outcome of shrinking only the
// current final segment and/or lineage index. It carries no frame semantics;
// the upper strict replay layer alone decides whether a suffix is torn.
type GenerationTailRepairResult struct {
	outcome                  AdmissionTransitionOutcome
	snapshot                 *GenerationSnapshot
	previousSnapshotIdentity [32]byte
	nextSnapshotIdentity     [32]byte
	segmentOrdinal           uint32
	segmentPreviousSize      uint64
	segmentNextSize          uint64
	indexPreviousSize        uint64
	indexNextSize            uint64
}

func (r GenerationTailRepairResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationTailRepairResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationTailRepairResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationTailRepairResult) NextSnapshotIdentity() [32]byte { return r.nextSnapshotIdentity }
func (r GenerationTailRepairResult) SegmentOrdinal() uint32         { return r.segmentOrdinal }
func (r GenerationTailRepairResult) SegmentPreviousSize() uint64    { return r.segmentPreviousSize }
func (r GenerationTailRepairResult) SegmentNextSize() uint64        { return r.segmentNextSize }
func (r GenerationTailRepairResult) IndexPreviousSize() uint64      { return r.indexPreviousSize }
func (r GenerationTailRepairResult) IndexNextSize() uint64          { return r.indexNextSize }

func (r GenerationTailRepairResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationTailRepairResult) validForLocked(lease *GenerationLease) bool {
	shrunk := r.segmentNextSize < r.segmentPreviousSize || r.indexNextSize < r.indexPreviousSize
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && shrunk && r.segmentNextSize > 0 && r.indexNextSize > 0 && r.segmentNextSize <= r.segmentPreviousSize && r.indexNextSize <= r.indexPreviousSize && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity != ([32]byte{}) && r.previousSnapshotIdentity != r.nextSnapshotIdentity && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// TruncateGenerationTails shrinks the exact current final segment and/or
// lineage index to caller-supplied nonzero prefix lengths. It cannot extend a
// file, remove a whole file, change membership, or choose a recovery boundary.
// Each changed file is fdatasync'd immediately in segment-then-index order.
// Once the first truncate is attempted, every failure is unknown and the old
// snapshot is invalid.
func (l *GenerationLease) TruncateGenerationTails(ctx context.Context, snapshot *GenerationSnapshot, segmentSize, indexSize uint64) (GenerationTailRepairResult, error) {
	result := GenerationTailRepairResult{outcome: AdmissionTransitionPreMutationFailure}
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
	segmentExpected, indexExpected := snapshot.segments[result.segmentOrdinal], snapshot.index
	result.segmentPreviousSize, result.segmentNextSize = segmentExpected.stat.size, segmentSize
	result.indexPreviousSize, result.indexNextSize = indexExpected.stat.size, indexSize
	if segmentSize == 0 || indexSize == 0 || segmentSize > segmentExpected.stat.size || indexSize > indexExpected.stat.size || segmentSize == segmentExpected.stat.size && indexSize == indexExpected.stat.size {
		return result, ErrInvalidInput
	}

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
		return result, corrupt("generation-truncate-prefix-snapshot")
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
			return filesystem("generation-truncate-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationTailRepairResult, error) {
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
			err = corrupt("generation-truncate-lineage-lock")
		}
		return finishFailure(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return finishFailure(filesystem("generation-truncate-index-open"))
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
			err = corrupt("generation-truncate-journal-lock")
		}
		return finishFailure(err)
	}
	segmentFD, err = l.store.ops.openFileAtReadWrite(journalFD, admissionSegmentName(int(result.segmentOrdinal)))
	if err != nil {
		return finishFailure(filesystem("generation-truncate-segment-open"))
	}
	segmentBytes, err := readGenerationAppendFile(ctx, l, segmentFD, segmentExpected)
	if err != nil {
		return finishFailure(err)
	}
	expectedSegment, expectedIndex := segmentExpected, indexExpected

	if segmentSize < segmentExpected.stat.size {
		attempted = true
		if l.store.ops.truncate(segmentFD, int64(segmentSize)) != nil {
			return finishFailure(filesystem("generation-truncate-segment"))
		}
		segmentAfter, statErr := l.store.ops.fstat(segmentFD)
		if statErr != nil || !generationTruncateIdentity(segmentExpected.stat, segmentAfter, segmentSize, l.store.uid, l.store.identity.device) {
			return finishFailure(filesystem("generation-truncate-segment-size"))
		}
		expectedSegment = truncatedGenerationSnapshotFile(segmentExpected, segmentAfter, segmentBytes, segmentSize)
		if !validCompactGenerationSnapshotFile(expectedSegment) {
			return finishFailure(filesystem("generation-truncate-segment-fact"))
		}
		if l.store.ops.fdatasync(segmentFD) != nil {
			return finishFailure(filesystem("generation-truncate-segment-sync"))
		}
		if err := contextError(ctx); err != nil {
			return finishFailure(err)
		}
	}
	if indexSize < indexExpected.stat.size {
		attempted = true
		if l.store.ops.truncate(indexFD, int64(indexSize)) != nil {
			return finishFailure(filesystem("generation-truncate-index"))
		}
		indexAfter, statErr := l.store.ops.fstat(indexFD)
		if statErr != nil || !generationTruncateIdentity(indexExpected.stat, indexAfter, indexSize, l.store.uid, l.store.identity.device) {
			return finishFailure(filesystem("generation-truncate-index-size"))
		}
		expectedIndex = truncatedGenerationSnapshotFile(indexExpected, indexAfter, indexBytes, indexSize)
		if !validCompactGenerationSnapshotFile(expectedIndex) {
			return finishFailure(filesystem("generation-truncate-index-fact"))
		}
		if l.store.ops.fdatasync(indexFD) != nil {
			return finishFailure(filesystem("generation-truncate-index-sync"))
		}
		if err := contextError(ctx); err != nil {
			return finishFailure(err)
		}
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
	expectedSegments[result.segmentOrdinal] = expectedSegment
	if !sameGenerationSnapshotFile(index, expectedIndex) || !sameGenerationSnapshotFiles(segments, expectedSegments) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(corrupt("generation-truncate-terminal-snapshot"))
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

func generationTruncateIdentity(before, after fileStat, size uint64, uid uint32, device uint64) bool {
	return size > 0 && size < before.size && validRegular(after, uid, device) && sameNodeIdentity(before, after) && before.mode == after.mode && before.uid == after.uid && before.nlink == after.nlink && after.size == size
}

func truncatedGenerationSnapshotFile(before generationSnapshotFile, after fileStat, content []byte, size uint64) generationSnapshotFile {
	if !validCompactGenerationSnapshotFile(before) || size == 0 || size >= before.stat.size || uint64(len(content)) != before.stat.size || uint64(len(content)) < size || sha256.Sum256(content) != before.digest || !sameNodeIdentity(before.stat, after) || before.stat.mode != after.mode || before.stat.uid != after.uid || before.stat.nlink != after.nlink || after.size != size {
		return generationSnapshotFile{}
	}
	digest := sha256.Sum256(content[:size])
	name := "index.caj"
	if before.role == inventorySegment {
		name = admissionSegmentName(int(before.ordinal))
	}
	return generationSnapshotFile{
		role: before.role, ordinal: before.ordinal, stat: after, digest: digest,
		identity: generationSnapshotFileIdentity(before.role, name, before.ordinal, after, digest),
	}
}
