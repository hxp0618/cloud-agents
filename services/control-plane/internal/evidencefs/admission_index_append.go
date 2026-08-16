package evidencefs

import (
	"context"
	"crypto/sha256"
)

// AppendTargetIndex consumes the current mutation token to append exact opaque
// bytes at the inventoried target index EOF, then advances the full inventory.
// C3/frame semantics remain migration-owned.
func (t *AdmissionMutationToken) AppendTargetIndex(ctx context.Context, inventory *AdmissionInventory, framed []byte) (AdmissionTransitionResult, error) {
	digest := sha256.Sum256(framed)
	pre := AdmissionTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateKind: "target_index_append", candidateDigest: digest}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || inventory.absent != nil || len(framed) == 0 || uint64(len(framed)) > maximumAdmissionIndexBytes || inventory.revision == ^uint64(0) {
		return pre, ErrInvalidInput
	}
	pre.candidateRevision = inventory.revision + 1
	if !inventory.snapshotMatchesLocked() {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	lineage := inventory.lineageMap[inventory.target]
	if lineage == nil || lineage.index == nil || lineage.index.stat.size > maximumAdmissionIndexBytes-uint64(len(framed)) || inventory.slot.discovery.lineagesDirectory == nil {
		return pre, ErrLimit
	}
	var aggregate uint64
	for _, value := range inventory.lineages {
		if value == nil || value.index == nil || value.index.stat.size > maximumAdmissionIndexAggregate-aggregate {
			return pre, ErrLimit
		}
		aggregate += value.index.stat.size
	}
	if uint64(len(framed)) > maximumAdmissionIndexAggregate-aggregate {
		return pre, ErrLimit
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return pre, err
	}
	lineagesFD, lineageFD, indexFD := -1, -1, -1
	mutated := false
	cleanup := func() error {
		failed := false
		for _, fd := range []int{indexFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("target-index-append-cleanup")
		}
		return nil
	}
	unknownResult := func(cause error) (AdmissionTransitionResult, error) {
		t.consumed = true
		l.revokeLocked()
		return AdmissionTransitionResult{outcome: AdmissionTransitionUnknown, candidateKind: "target_index_append", candidateDigest: digest, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision}, unknown(cause)
	}
	fail := func(cause error) (AdmissionTransitionResult, error) {
		if cleanupErr := cleanup(); cleanupErr != nil {
			cause = cleanupErr
		}
		if mutated {
			return unknownResult(cause)
		}
		t.consumed = true
		l.revokeLocked()
		return pre, cause
	}
	lineagesFD, _, err = l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return fail(err)
	}
	lineageFD, _, err = l.store.openVerifiedDirectory(lineagesFD, lineage.name)
	if err != nil {
		return fail(err)
	}
	indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
	if err != nil {
		return fail(filesystem("target-index-append-open"))
	}
	before, err := l.store.ops.fstat(indexFD)
	if err != nil || !sameIdentity(before, lineage.index.stat) {
		return fail(corrupt("target-index-append-identity"))
	}
	oldBytes, err := l.readInventoryFileRaw(ctx, lineage.index)
	if err != nil || uint64(len(oldBytes)) != before.size || sha256.Sum256(oldBytes) != lineage.index.digest {
		return fail(corrupt("target-index-append-prefix"))
	}
	mutated = true
	for offset := 0; offset < len(framed); {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		written, writeErr := l.store.ops.pwrite(indexFD, framed[offset:], int64(before.size)+int64(offset))
		if writeErr != nil || written <= 0 || written > len(framed)-offset {
			return fail(filesystem("target-index-append-write"))
		}
		offset += written
	}
	after, err := l.store.ops.fstat(indexFD)
	if err != nil || !validRegular(after, l.store.uid, l.store.identity.device) || after.device != before.device || after.inode != before.inode || after.size != before.size+uint64(len(framed)) {
		return fail(filesystem("target-index-append-size"))
	}
	if l.store.ops.fdatasync(indexFD) != nil {
		return fail(filesystem("target-index-append-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}
	if err := cleanup(); err != nil {
		return unknownResult(err)
	}
	rootFD, lineagesFD, lineageFD, indexFD = -1, -1, -1, -1
	discovery, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil || !targetIndexAppendDiscoveryMatches(inventory.slot.discovery, discovery, lineage.name, after) {
		if err == nil {
			err = filesystem("target-index-append-discovery")
		}
		return unknownResult(err)
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		return unknownResult(err)
	}
	// The first post-header index append ends registered_empty even though no
	// generation directory exists yet. Membership remains bound by lineageMap;
	// the physical two-file shape alone must never remint the empty fact.
	next.registration = nil
	nextLineage := next.lineageMap[inventory.target]
	wantDigest := sha256.Sum256(append(append([]byte(nil), oldBytes...), framed...))
	if nextLineage == nil || nextLineage.index == nil || nextLineage.index.stat.size != after.size || nextLineage.index.digest != wantDigest || next.fullSet == inventory.fullSet {
		return unknownResult(filesystem("target-index-append-missing"))
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		return unknownResult(ErrLeaseInvalid)
	}
	return AdmissionTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, candidateKind: "target_index_append", candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision}, nil
}

func targetIndexAppendDiscoveryMatches(previous, next admissionDiscovery, target string, nextIndex fileStat) bool {
	if len(previous.lineages) != len(next.lineages) || (previous.lineagesDirectory == nil) != (next.lineagesDirectory == nil) || previous.lineagesDirectory != nil && !sameDirectoryIdentity(*previous.lineagesDirectory, *next.lineagesDirectory) {
		return false
	}
	seen := false
	for index := range previous.lineages {
		before, after := previous.lineages[index], next.lineages[index]
		if before.name == target {
			seen = true
			if after.name != target || !sameDirectoryIdentity(before.stat, after.stat) || !sameIdentity(before.lock, after.lock) || !sameIdentity(after.index, nextIndex) || len(before.journals) != len(after.journals) {
				return false
			}
			for journal := range before.journals {
				if !sameAdmissionJournalDiscovery(before.journals[journal], after.journals[journal]) {
					return false
				}
			}
			continue
		}
		if !sameAdmissionLineageDiscovery(before, after) {
			return false
		}
	}
	return seen
}

func sameAdmissionLineageDiscovery(a, b discoveredLineage) bool {
	if a.name != b.name || !sameDirectoryIdentity(a.stat, b.stat) || !sameIdentity(a.lock, b.lock) || !sameIdentity(a.index, b.index) || len(a.journals) != len(b.journals) {
		return false
	}
	for index := range a.journals {
		if !sameAdmissionJournalDiscovery(a.journals[index], b.journals[index]) {
			return false
		}
	}
	return true
}

func sameAdmissionJournalDiscovery(a, b discoveredJournal) bool {
	if a.name != b.name || !sameDirectoryIdentity(a.stat, b.stat) || !sameIdentity(a.lock, b.lock) || len(a.segments) != len(b.segments) {
		return false
	}
	for index := range a.segments {
		if !sameIdentity(a.segments[index], b.segments[index]) {
			return false
		}
	}
	return true
}
