package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
)

// GenerationAppendReconcileState is a closed byte-level classification of one
// prior unknown composite append against a fresh current snapshot. It grants
// no repair or append authority by itself.
type GenerationAppendReconcileState string

const (
	GenerationAppendReconcileUnchanged         GenerationAppendReconcileState = "unchanged"
	GenerationAppendReconcileJournalTorn       GenerationAppendReconcileState = "journal_torn"
	GenerationAppendReconcileJournalComplete   GenerationAppendReconcileState = "journal_complete"
	GenerationAppendReconcileCheckpointTorn    GenerationAppendReconcileState = "checkpoint_torn"
	GenerationAppendReconcileCompositeComplete GenerationAppendReconcileState = "composite_complete"
)

// Reconcile classifies exact bytes from an unknown existing-segment composite
// append. The result privately binds the previous node identities and candidate
// suffixes, so same-size content substitution, inode replacement, extra bytes,
// or an index candidate ahead of its journal candidate are corruption. The
// caller must separately invoke resync/checkpoint-healing/truncate transitions.
func (r GenerationAppendResult) Reconcile(ctx context.Context, lease *GenerationLease, snapshot *GenerationSnapshot) (GenerationAppendReconcileState, error) {
	if lease == nil || lease.self != lease || lease.seal == nil || lease.mu == nil || snapshot == nil {
		return "", ErrLeaseInvalid
	}
	lease.mu.Lock()
	defer lease.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if r.outcome != AdmissionTransitionUnknown || r.snapshot != nil || r.journalFramedDigest == ([32]byte{}) || r.checkpointFramedDigest == ([32]byte{}) || sha256.Sum256(r.journalFramed) != r.journalFramedDigest || sha256.Sum256(r.checkpointFramed) != r.checkpointFramedDigest || !validCompactGenerationSnapshotFile(r.previousIndex) || len(r.previousSegments) == 0 || int(r.segmentOrdinal) != len(r.previousSegments)-1 || r.previousSnapshotIdentity != generationSnapshotFilesDigest(lease.target, lease.journal, r.previousIndex, r.previousSegments) || r.journalPreviousSize != r.previousSegments[r.segmentOrdinal].stat.size || r.indexPreviousSize != r.previousIndex.stat.size || !lease.activeLocked() || !snapshot.validLocked() || snapshot.lease != lease || lease.snapshot != snapshot {
		return "", ErrLeaseInvalid
	}
	currentIndex, currentSegments, err := lease.readGenerationSnapshotLocked(ctx)
	if err != nil {
		if !isContextError(err) {
			lease.valid = false
			invalidateGenerationSnapshotsLocked(lease)
		}
		return "", err
	}
	if !sameGenerationSnapshotFile(currentIndex, snapshot.index) || !sameGenerationSnapshotFiles(currentSegments, snapshot.segments) || len(currentSegments) != len(r.previousSegments) {
		return r.failReconcileCorrupt("generation-append-reconcile-snapshot", lease)
	}
	for ordinal := range currentSegments {
		if uint32(ordinal) == r.segmentOrdinal {
			continue
		}
		if !sameGenerationSnapshotFile(currentSegments[ordinal], r.previousSegments[ordinal]) {
			return r.failReconcileCorrupt("generation-append-reconcile-segment-set", lease)
		}
	}
	index, err := lease.readOneGenerationSnapshotFileLocked(ctx, snapshot, inventoryIndex, 0)
	if err != nil {
		if !isContextError(err) {
			lease.valid = false
			invalidateGenerationSnapshotsLocked(lease)
		}
		return "", err
	}
	segment, err := lease.readOneGenerationSnapshotFileLocked(ctx, snapshot, inventorySegment, r.segmentOrdinal)
	if err != nil {
		if !isContextError(err) {
			lease.valid = false
			invalidateGenerationSnapshotsLocked(lease)
		}
		return "", err
	}
	if !sameGenerationSnapshotFile(compactGenerationSnapshotFile(index), currentIndex) || !sameGenerationSnapshotFile(compactGenerationSnapshotFile(segment), currentSegments[r.segmentOrdinal]) || !validGenerationReconcilePrefix(index, r.previousIndex) || !validGenerationReconcilePrefix(segment, r.previousSegments[r.segmentOrdinal]) {
		return r.failReconcileCorrupt("generation-append-reconcile-prefix", lease)
	}
	journalState, journalOK := generationReconcileSuffixState(segment.bytes, r.journalPreviousSize, r.journalFramed)
	checkpointState, checkpointOK := generationReconcileSuffixState(index.bytes, r.indexPreviousSize, r.checkpointFramed)
	if !journalOK || !checkpointOK {
		return r.failReconcileCorrupt("generation-append-reconcile-suffix", lease)
	}
	switch {
	case journalState == generationSuffixAbsent && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileUnchanged, nil
	case journalState == generationSuffixPartial && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileJournalTorn, nil
	case journalState == generationSuffixComplete && checkpointState == generationSuffixAbsent:
		return GenerationAppendReconcileJournalComplete, nil
	case journalState == generationSuffixComplete && checkpointState == generationSuffixPartial:
		return GenerationAppendReconcileCheckpointTorn, nil
	case journalState == generationSuffixComplete && checkpointState == generationSuffixComplete:
		return GenerationAppendReconcileCompositeComplete, nil
	default:
		return r.failReconcileCorrupt("generation-append-reconcile-order", lease)
	}
}

func (r GenerationAppendResult) failReconcileCorrupt(operation string, lease *GenerationLease) (GenerationAppendReconcileState, error) {
	if lease != nil {
		lease.valid = false
		invalidateGenerationSnapshotsLocked(lease)
	}
	return "", corrupt(operation)
}

func validGenerationReconcilePrefix(current, previous generationSnapshotFile) bool {
	if !validGenerationSnapshotFile(current) || !validCompactGenerationSnapshotFile(previous) || current.role != previous.role || current.ordinal != previous.ordinal || !sameNodeIdentity(current.stat, previous.stat) || current.stat.mode != previous.stat.mode || current.stat.uid != previous.stat.uid || current.stat.nlink != previous.stat.nlink || current.stat.size < previous.stat.size || uint64(len(current.bytes)) != current.stat.size || previous.stat.size > uint64(len(current.bytes)) {
		return false
	}
	return sha256.Sum256(current.bytes[:previous.stat.size]) == previous.digest
}

type generationSuffixState uint8

const (
	generationSuffixAbsent generationSuffixState = iota
	generationSuffixPartial
	generationSuffixComplete
)

func generationReconcileSuffixState(current []byte, previousSize uint64, candidate []byte) (generationSuffixState, bool) {
	if previousSize > uint64(len(current)) || len(candidate) == 0 || previousSize > ^uint64(0)-uint64(len(candidate)) {
		return 0, false
	}
	suffix := current[previousSize:]
	switch {
	case len(suffix) == 0:
		return generationSuffixAbsent, true
	case len(suffix) < len(candidate) && bytes.Equal(suffix, candidate[:len(suffix)]):
		return generationSuffixPartial, true
	case len(suffix) == len(candidate) && bytes.Equal(suffix, candidate):
		return generationSuffixComplete, true
	default:
		return 0, false
	}
}
