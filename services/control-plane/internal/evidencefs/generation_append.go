package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// GenerationAppendResult is the closed filesystem outcome for one existing-
// segment journal append followed by one lineage-index checkpoint append.
// evidencefs treats both payloads as opaque framed bytes and assigns no C3
// meaning. Only durable carries a new sealed snapshot.
type GenerationAppendResult struct {
	outcome                  AdmissionTransitionOutcome
	snapshot                 *GenerationSnapshot
	journalFramedDigest      [32]byte
	checkpointFramedDigest   [32]byte
	previousSnapshotIdentity [32]byte
	nextSnapshotIdentity     [32]byte
	segmentOrdinal           uint32
	journalPreviousSize      uint64
	indexPreviousSize        uint64
}

func (r GenerationAppendResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r GenerationAppendResult) Snapshot() *GenerationSnapshot       { return r.snapshot }
func (r GenerationAppendResult) JournalFramedDigest() [32]byte       { return r.journalFramedDigest }
func (r GenerationAppendResult) CheckpointFramedDigest() [32]byte    { return r.checkpointFramedDigest }
func (r GenerationAppendResult) PreviousSnapshotIdentity() [32]byte {
	return r.previousSnapshotIdentity
}
func (r GenerationAppendResult) NextSnapshotIdentity() [32]byte { return r.nextSnapshotIdentity }
func (r GenerationAppendResult) SegmentOrdinal() uint32         { return r.segmentOrdinal }
func (r GenerationAppendResult) JournalPreviousSize() uint64    { return r.journalPreviousSize }
func (r GenerationAppendResult) IndexPreviousSize() uint64      { return r.indexPreviousSize }

// ValidFor reports whether a durable result owns the current snapshot of the
// exact retained lease. It exposes no path, descriptor, or writable handle.
func (r GenerationAppendResult) ValidFor(lease *GenerationLease) bool {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return false
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.validForLocked(lease)
}

func (r GenerationAppendResult) validForLocked(lease *GenerationLease) bool {
	return lease != nil && r.outcome == AdmissionTransitionDurable && r.snapshot != nil && r.journalFramedDigest != ([32]byte{}) && r.checkpointFramedDigest != ([32]byte{}) && r.previousSnapshotIdentity != ([32]byte{}) && r.nextSnapshotIdentity != ([32]byte{}) && r.previousSnapshotIdentity != r.nextSnapshotIdentity && lease.snapshot == r.snapshot && r.snapshot.lease == lease && r.snapshot.validLocked()
}

// AppendExistingSegmentComposite appends opaque journal bytes to the current
// final segment, makes that file durable, then appends opaque checkpoint bytes
// to the lineage index and makes the index durable. Any failure after the
// first write attempt returns unknown and invalidates the old snapshot. This
// primitive does not rotate segments, decode frames, mint a cursor, or choose
// recovery semantics.
func (l *GenerationLease) AppendExistingSegmentComposite(ctx context.Context, snapshot *GenerationSnapshot, journalFramed, checkpointFramed []byte) (GenerationAppendResult, error) {
	result := GenerationAppendResult{outcome: AdmissionTransitionPreMutationFailure}
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
	if len(journalFramed) == 0 || len(checkpointFramed) == 0 || uint64(len(journalFramed)) > maximumAdmissionSegmentBytes || uint64(len(checkpointFramed)) > maximumAdmissionIndexBytes {
		return result, ErrInvalidInput
	}
	result.previousSnapshotIdentity = snapshot.canonical
	result.segmentOrdinal = uint32(len(snapshot.segments) - 1)
	segmentExpected := snapshot.segments[result.segmentOrdinal]
	indexExpected := snapshot.index
	result.journalPreviousSize, result.indexPreviousSize = segmentExpected.stat.size, indexExpected.stat.size
	if segmentExpected.stat.size > maximumAdmissionSegmentBytes-uint64(len(journalFramed)) || indexExpected.stat.size > maximumAdmissionIndexBytes-uint64(len(checkpointFramed)) {
		return result, ErrLimit
	}
	var journalBytes uint64
	for _, segment := range snapshot.segments {
		if segment.stat.size > maximumAdmissionJournalBytes-journalBytes {
			return result, ErrLimit
		}
		journalBytes += segment.stat.size
	}
	if uint64(len(journalFramed)) > maximumAdmissionJournalBytes-journalBytes {
		return result, ErrLimit
	}
	journalFramed = append([]byte(nil), journalFramed...)
	checkpointFramed = append([]byte(nil), checkpointFramed...)
	result.journalFramedDigest = sha256.Sum256(journalFramed)
	result.checkpointFramedDigest = sha256.Sum256(checkpointFramed)

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
			return filesystem("generation-append-cleanup")
		}
		return nil
	}
	finishFailure := func(cause error) (GenerationAppendResult, error) {
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
			err = corrupt("generation-append-lineage-lock")
		}
		return finishFailure(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return finishFailure(filesystem("generation-append-index-open"))
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
			err = corrupt("generation-append-journal-lock")
		}
		return finishFailure(err)
	}
	segmentFD, err = l.store.ops.openFileAtReadWrite(journalFD, admissionSegmentName(int(result.segmentOrdinal)))
	if err != nil {
		return finishFailure(filesystem("generation-append-segment-open"))
	}
	segmentBytes, err := readGenerationAppendFile(ctx, l, segmentFD, segmentExpected)
	if err != nil {
		return finishFailure(err)
	}

	attempted = true
	if err := appendGenerationBytes(ctx, l, segmentFD, segmentExpected.stat.size, journalFramed); err != nil {
		return finishFailure(err)
	}
	segmentAfter, err := l.store.ops.fstat(segmentFD)
	if err != nil || !generationAppendIdentity(segmentExpected.stat, segmentAfter, uint64(len(journalFramed)), l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-append-segment-size"))
	}
	expectedSegment := appendedGenerationSnapshotFile(segmentExpected, segmentAfter, segmentBytes, journalFramed)
	if !validCompactGenerationSnapshotFile(expectedSegment) {
		return finishFailure(filesystem("generation-append-segment-fact"))
	}
	if l.store.ops.fdatasync(segmentFD) != nil {
		return finishFailure(filesystem("generation-append-segment-sync"))
	}
	if err := contextError(ctx); err != nil {
		return finishFailure(err)
	}
	if err := appendGenerationBytes(ctx, l, indexFD, indexExpected.stat.size, checkpointFramed); err != nil {
		return finishFailure(err)
	}
	indexAfter, err := l.store.ops.fstat(indexFD)
	if err != nil || !generationAppendIdentity(indexExpected.stat, indexAfter, uint64(len(checkpointFramed)), l.store.uid, l.store.identity.device) {
		return finishFailure(filesystem("generation-append-index-size"))
	}
	expectedIndex := appendedGenerationSnapshotFile(indexExpected, indexAfter, indexBytes, checkpointFramed)
	if !validCompactGenerationSnapshotFile(expectedIndex) {
		return finishFailure(filesystem("generation-append-index-fact"))
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return finishFailure(filesystem("generation-append-index-sync"))
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
	expectedSegments[result.segmentOrdinal] = expectedSegment
	if !sameGenerationSnapshotFile(index, expectedIndex) || !sameGenerationSnapshotFiles(segments, expectedSegments) {
		invalidateGenerationSnapshotsLocked(l)
		result.outcome = AdmissionTransitionUnknown
		return result, unknown(corrupt("generation-append-terminal-snapshot"))
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

func readGenerationAppendFile(ctx context.Context, l *GenerationLease, fd int, expected generationSnapshotFile) ([]byte, error) {
	if l == nil || fd < 0 || !validCompactGenerationSnapshotFile(expected) {
		return nil, ErrLeaseInvalid
	}
	before, err := l.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, expected.stat) || !validRegular(before, l.store.uid, l.store.identity.device) {
		return nil, corrupt("generation-append-file-identity")
	}
	if before.size == 0 || before.size > uint64(^uint(0)>>1) {
		return nil, corrupt("generation-append-file-size")
	}
	bytes := make([]byte, int(before.size))
	for offset := 0; offset < len(bytes); {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		read, readErr := l.store.ops.pread(fd, bytes[offset:], int64(offset))
		if read <= 0 || read > len(bytes)-offset || readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, corrupt("generation-append-file-read")
		}
		offset += read
		if errors.Is(readErr, io.EOF) && offset != len(bytes) {
			return nil, corrupt("generation-append-file-short-read")
		}
	}
	after, err := l.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, after) || !sameIdentity(after, expected.stat) || sha256.Sum256(bytes) != expected.digest {
		return nil, corrupt("generation-append-file-prefix")
	}
	return bytes, nil
}

func appendGenerationBytes(ctx context.Context, l *GenerationLease, fd int, offset uint64, value []byte) error {
	maximumOffset := uint64(^uint64(0) >> 1)
	if l == nil || fd < 0 || len(value) == 0 || offset > maximumOffset || uint64(len(value)) > maximumOffset-offset {
		return ErrInvalidInput
	}
	for written := 0; written < len(value); {
		if err := contextError(ctx); err != nil {
			return err
		}
		count, err := l.store.ops.pwrite(fd, value[written:], int64(offset)+int64(written))
		if err != nil || count <= 0 || count > len(value)-written {
			return filesystem("generation-append-write")
		}
		written += count
	}
	return nil
}

func generationAppendIdentity(before, after fileStat, appended uint64, uid uint32, device uint64) bool {
	return appended > 0 && validRegular(after, uid, device) && sameNodeIdentity(before, after) && before.mode == after.mode && before.uid == after.uid && before.nlink == after.nlink && before.size <= ^uint64(0)-appended && after.size == before.size+appended
}

func appendedGenerationSnapshotFile(before generationSnapshotFile, after fileStat, prefix, appended []byte) generationSnapshotFile {
	if !validCompactGenerationSnapshotFile(before) || len(prefix) == 0 || len(appended) == 0 || uint64(len(prefix)) != before.stat.size || sha256.Sum256(prefix) != before.digest ||
		!sameNodeIdentity(before.stat, after) || before.stat.mode != after.mode || before.stat.uid != after.uid || before.stat.nlink != after.nlink ||
		before.stat.size > ^uint64(0)-uint64(len(appended)) || after.size != before.stat.size+uint64(len(appended)) {
		return generationSnapshotFile{}
	}
	content := make([]byte, 0, len(prefix)+len(appended))
	content = append(content, prefix...)
	content = append(content, appended...)
	digest := sha256.Sum256(content)
	name := "index.caj"
	if before.role == inventorySegment {
		name = admissionSegmentName(int(before.ordinal))
	}
	return generationSnapshotFile{
		role: before.role, ordinal: before.ordinal, stat: after, digest: digest,
		identity: generationSnapshotFileIdentity(before.role, name, before.ordinal, after, digest),
	}
}
