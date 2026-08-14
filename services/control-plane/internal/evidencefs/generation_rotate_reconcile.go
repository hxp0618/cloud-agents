package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
)

// GenerationRotationReconcileState is the closed byte-level classification of
// one unknown rotation composite. It grants no repair, cursor, or append
// authority by itself.
type GenerationRotationReconcileState string

const (
	GenerationRotationReconcileSegmentAbsent           GenerationRotationReconcileState = "segment_absent"
	GenerationRotationReconcileSegmentEmpty            GenerationRotationReconcileState = "segment_empty"
	GenerationRotationReconcileHeaderTorn              GenerationRotationReconcileState = "header_torn"
	GenerationRotationReconcileHeaderComplete          GenerationRotationReconcileState = "header_complete"
	GenerationRotationReconcileHeaderCheckpointTorn    GenerationRotationReconcileState = "header_checkpoint_torn"
	GenerationRotationReconcileHeaderCompositeComplete GenerationRotationReconcileState = "header_composite_complete"
	GenerationRotationReconcileCallerTorn              GenerationRotationReconcileState = "caller_torn"
	GenerationRotationReconcileCallerComplete          GenerationRotationReconcileState = "caller_complete"
	GenerationRotationReconcileCallerCheckpointTorn    GenerationRotationReconcileState = "caller_checkpoint_torn"
	GenerationRotationReconcileCompositeComplete       GenerationRotationReconcileState = "composite_complete"
)

// Reconcile observes the exact index, old segment set, and optional new final
// segment under the retained locks. It deliberately does not require a normal
// GenerationSnapshot because an empty response-lost segment is a valid state
// for this classifier but never a valid snapshot.
func (r GenerationRotationResult) Reconcile(ctx context.Context, lease *GenerationLease) (GenerationRotationReconcileState, error) {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil {
		return "", ErrLeaseInvalid
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return r.reconcileLocked(ctx, lease)
}

func (r GenerationRotationResult) reconcileLocked(ctx context.Context, lease *GenerationLease) (GenerationRotationReconcileState, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if !r.validUnknownForLocked(lease) {
		return "", ErrLeaseInvalid
	}
	index, segmentPresent, segment, err := observeGenerationRotationLocked(ctx, lease, r)
	if err != nil {
		if !isContextError(err) {
			lease.valid = false
			invalidateGenerationSnapshotsLocked(lease)
		}
		return "", err
	}
	if segmentPresent && !r.segmentOpened || !validGenerationReconcilePrefix(index, r.previousIndex) {
		return r.failRotationReconcileCorrupt(lease)
	}
	indexState, indexOK := classifyGenerationRotationSuffix(index.bytes, r.indexPreviousSize, r.rotationCheckpointFramed, r.callerCheckpointFramed)
	if !indexOK {
		return r.failRotationReconcileCorrupt(lease)
	}
	if !segmentPresent {
		if indexState == generationRotationSuffixAbsent {
			return GenerationRotationReconcileSegmentAbsent, nil
		}
		return r.failRotationReconcileCorrupt(lease)
	}
	segmentState, segmentOK := classifyGenerationRotationSuffix(segment.bytes, 0, r.rotationHeaderFramed, r.callerFramed)
	if !segmentOK {
		return r.failRotationReconcileCorrupt(lease)
	}
	switch {
	case segmentState == generationRotationSuffixAbsent && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileSegmentEmpty, nil
	case segmentState == generationRotationSuffixFirstPartial && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileHeaderTorn, nil
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixAbsent:
		return GenerationRotationReconcileHeaderComplete, nil
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixFirstPartial:
		return GenerationRotationReconcileHeaderCheckpointTorn, nil
	case segmentState == generationRotationSuffixFirstComplete && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileHeaderCompositeComplete, nil
	case segmentState == generationRotationSuffixSecondPartial && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileCallerTorn, nil
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixFirstComplete:
		return GenerationRotationReconcileCallerComplete, nil
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixSecondPartial:
		return GenerationRotationReconcileCallerCheckpointTorn, nil
	case segmentState == generationRotationSuffixSecondComplete && indexState == generationRotationSuffixSecondComplete:
		return GenerationRotationReconcileCompositeComplete, nil
	default:
		return r.failRotationReconcileCorrupt(lease)
	}
}

func (r GenerationRotationResult) validUnknownForLocked(lease *GenerationLease) bool {
	currentSnapshotValid := lease != nil && (lease.snapshot == nil || lease.snapshot.lease == lease && lease.snapshot.validLocked())
	return lease != nil && r.outcome == AdmissionTransitionUnknown && r.snapshot == nil && r.nextSnapshotIdentity == ([32]byte{}) && r.previousSnapshotIdentity != ([32]byte{}) && r.rotationHeaderFramedDigest != ([32]byte{}) && r.rotationCheckpointFramedDigest != ([32]byte{}) && r.callerFramedDigest != ([32]byte{}) && r.callerCheckpointFramedDigest != ([32]byte{}) && sha256.Sum256(r.rotationHeaderFramed) == r.rotationHeaderFramedDigest && sha256.Sum256(r.rotationCheckpointFramed) == r.rotationCheckpointFramedDigest && sha256.Sum256(r.callerFramed) == r.callerFramedDigest && sha256.Sum256(r.callerCheckpointFramed) == r.callerCheckpointFramedDigest && validCompactGenerationSnapshotFile(r.previousIndex) && len(r.previousSegments) > 0 && r.segmentOrdinal == uint32(len(r.previousSegments)) && r.segmentOrdinal < maximumAdmissionSegments && r.indexPreviousSize == r.previousIndex.stat.size && r.previousSnapshotIdentity == generationSnapshotFilesDigest(lease.target, lease.journal, r.previousIndex, r.previousSegments) && lease.activeLocked() && currentSnapshotValid
}

func (r GenerationRotationResult) failRotationReconcileCorrupt(lease *GenerationLease) (GenerationRotationReconcileState, error) {
	if lease != nil {
		lease.valid = false
		invalidateGenerationSnapshotsLocked(lease)
	}
	return "", corrupt("generation-rotation-reconcile")
}

func observeGenerationRotationLocked(ctx context.Context, lease *GenerationLease, result GenerationRotationResult) (generationSnapshotFile, bool, generationSnapshotFile, error) {
	rootFD, err := lease.store.freshRoot()
	if err != nil {
		return generationSnapshotFile{}, false, generationSnapshotFile{}, err
	}
	lineagesFD, lineageFD, journalFD := -1, -1, -1
	closeAll := func() error {
		failed := false
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = lease.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-rotation-reconcile-cleanup")
		}
		return nil
	}
	fail := func(cause error) (generationSnapshotFile, bool, generationSnapshotFile, error) {
		if cleanupErr := closeAll(); cleanupErr != nil {
			cause = cleanupErr
		}
		return generationSnapshotFile{}, false, generationSnapshotFile{}, cause
	}
	lineagesFD, _, err = lease.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return fail(err)
	}
	lineageFD, _, err = lease.store.openVerifiedDirectory(lineagesFD, hex.EncodeToString(lease.target[:]))
	if err != nil {
		return fail(err)
	}
	lineageLock, err := lease.store.statVerifiedRegular(lineageFD, "writer.lock")
	if err != nil || !sameIdentity(lineageLock, lease.lineage.stat) {
		if err == nil {
			err = corrupt("generation-rotation-reconcile-lineage-lock")
		}
		return fail(err)
	}
	index, err := lease.readGenerationFile(ctx, lineageFD, "index.caj", inventoryIndex, 0, maximumAdmissionIndexBytes)
	if err != nil {
		return fail(err)
	}
	journalFD, _, err = lease.store.openVerifiedDirectory(lineageFD, hex.EncodeToString(lease.journal[:]))
	if err != nil {
		return fail(err)
	}
	journalLock, err := lease.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, lease.generation.stat) {
		if err == nil {
			err = corrupt("generation-rotation-reconcile-journal-lock")
		}
		return fail(err)
	}
	names, err := lease.store.ops.readDirNames(journalFD, maximumAdmissionSegments+2)
	if err != nil {
		if lease.store.ops.isOverflow(err) {
			err = limit("generation-rotation-reconcile-segment-count")
		} else {
			err = filesystem("generation-rotation-reconcile-segment-list")
		}
		return fail(err)
	}
	sort.Strings(names)
	wantNew := admissionSegmentName(int(result.segmentOrdinal))
	seenLock, seenNew := false, false
	seenOld := make([]bool, len(result.previousSegments))
	for _, name := range names {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		if name == "writer.lock" {
			if seenLock {
				return fail(corrupt("generation-rotation-reconcile-duplicate-lock"))
			}
			seenLock = true
			continue
		}
		if name == wantNew {
			if seenNew {
				return fail(corrupt("generation-rotation-reconcile-duplicate-new"))
			}
			seenNew = true
			continue
		}
		matched := false
		for ordinal := range result.previousSegments {
			if name != admissionSegmentName(ordinal) {
				continue
			}
			if seenOld[ordinal] {
				return fail(corrupt("generation-rotation-reconcile-duplicate-old"))
			}
			seenOld[ordinal], matched = true, true
			break
		}
		if !matched {
			return fail(corrupt("generation-rotation-reconcile-unexpected-entry"))
		}
	}
	if !seenLock {
		return fail(corrupt("generation-rotation-reconcile-missing-lock"))
	}
	for ordinal, seen := range seenOld {
		if !seen {
			return fail(corrupt("generation-rotation-reconcile-missing-old"))
		}
		file, readErr := lease.readGenerationFile(ctx, journalFD, admissionSegmentName(ordinal), inventorySegment, uint32(ordinal), maximumAdmissionSegmentBytes)
		if readErr != nil {
			return fail(readErr)
		}
		if !sameGenerationSnapshotFile(compactGenerationSnapshotFile(file), result.previousSegments[ordinal]) {
			return fail(corrupt("generation-rotation-reconcile-old-segment"))
		}
	}
	var segment generationSnapshotFile
	if seenNew {
		segment, err = readGenerationRotationTailFile(ctx, lease, journalFD, wantNew, result.segmentOrdinal)
		if err != nil {
			return fail(err)
		}
	}
	if err := closeAll(); err != nil {
		return generationSnapshotFile{}, false, generationSnapshotFile{}, err
	}
	return index, seenNew, segment, nil
}

func readGenerationRotationTailFile(ctx context.Context, lease *GenerationLease, parent int, name string, ordinal uint32) (result generationSnapshotFile, resultErr error) {
	fd, err := lease.store.ops.openFileAt(parent, name, false)
	if err != nil {
		return generationSnapshotFile{}, filesystem("generation-rotation-reconcile-tail-open")
	}
	defer func() {
		if closeErr := lease.store.checkedClose(fd); closeErr != nil {
			result, resultErr = generationSnapshotFile{}, closeErr
		}
	}()
	before, err := lease.store.ops.fstat(fd)
	if err != nil || !validRegular(before, lease.store.uid, lease.store.identity.device) || before.size > maximumAdmissionSegmentBytes || before.size > uint64(^uint(0)>>1) {
		return generationSnapshotFile{}, corrupt("generation-rotation-reconcile-tail-identity")
	}
	raw := make([]byte, int(before.size))
	for offset := 0; offset < len(raw); {
		if err := contextError(ctx); err != nil {
			return generationSnapshotFile{}, err
		}
		count, readErr := lease.store.ops.pread(fd, raw[offset:], int64(offset))
		if count <= 0 || count > len(raw)-offset || readErr != nil && !errors.Is(readErr, io.EOF) {
			return generationSnapshotFile{}, corrupt("generation-rotation-reconcile-tail-read")
		}
		offset += count
		if errors.Is(readErr, io.EOF) && offset != len(raw) {
			return generationSnapshotFile{}, corrupt("generation-rotation-reconcile-tail-short-read")
		}
	}
	after, err := lease.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, after) {
		return generationSnapshotFile{}, corrupt("generation-rotation-reconcile-tail-drift")
	}
	digest := sha256.Sum256(raw)
	return generationSnapshotFile{role: inventorySegment, ordinal: ordinal, stat: after, digest: digest, identity: generationSnapshotFileIdentity(inventorySegment, name, ordinal, after, digest), bytes: raw}, nil
}

type generationRotationSuffixState uint8

const (
	generationRotationSuffixAbsent generationRotationSuffixState = iota
	generationRotationSuffixFirstPartial
	generationRotationSuffixFirstComplete
	generationRotationSuffixSecondPartial
	generationRotationSuffixSecondComplete
)

func classifyGenerationRotationSuffix(current []byte, previousSize uint64, first, second []byte) (generationRotationSuffixState, bool) {
	if previousSize > uint64(len(current)) || len(first) == 0 || len(second) == 0 {
		return 0, false
	}
	suffix := current[previousSize:]
	switch {
	case len(suffix) == 0:
		return generationRotationSuffixAbsent, true
	case len(suffix) < len(first) && bytes.Equal(suffix, first[:len(suffix)]):
		return generationRotationSuffixFirstPartial, true
	case len(suffix) == len(first) && bytes.Equal(suffix, first):
		return generationRotationSuffixFirstComplete, true
	case len(suffix) > len(first) && bytes.Equal(suffix[:len(first)], first):
		remainder := suffix[len(first):]
		switch {
		case len(remainder) < len(second) && bytes.Equal(remainder, second[:len(remainder)]):
			return generationRotationSuffixSecondPartial, true
		case len(remainder) == len(second) && bytes.Equal(remainder, second):
			return generationRotationSuffixSecondComplete, true
		}
	}
	return 0, false
}
