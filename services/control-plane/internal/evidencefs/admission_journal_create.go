package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

type AdmissionJournalTransitionResult struct {
	outcome           AdmissionTransitionOutcome
	inventory         *AdmissionInventory
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
	journal           [32]byte
	headerDigest      [32]byte
	headerSize        uint64
}

func (r AdmissionJournalTransitionResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r AdmissionJournalTransitionResult) Inventory() *AdmissionInventory      { return r.inventory }
func (r AdmissionJournalTransitionResult) CandidateKind() string               { return "generation_header" }
func (r AdmissionJournalTransitionResult) CandidateDigest() [32]byte           { return r.candidateDigest }
func (r AdmissionJournalTransitionResult) CandidateSequence() uint64           { return r.candidateSequence }
func (r AdmissionJournalTransitionResult) CandidateRevision() uint64           { return r.candidateRevision }
func (r AdmissionJournalTransitionResult) PreviousRevision() uint64            { return r.previousRevision }
func (r AdmissionJournalTransitionResult) Journal() [32]byte                   { return r.journal }
func (r AdmissionJournalTransitionResult) HeaderDigest() [32]byte              { return r.headerDigest }
func (r AdmissionJournalTransitionResult) HeaderSize() uint64                  { return r.headerSize }

func (r AdmissionJournalTransitionResult) Invalidate() error {
	if r.inventory == nil || r.inventory.lease == nil {
		return ErrLeaseInvalid
	}
	l := r.inventory.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if !r.inventory.validLocked() {
		return ErrLeaseInvalid
	}
	if !r.validLocked(r.inventory) {
		l.revokeLocked()
		return ErrLeaseInvalid
	}
	l.revokeLocked()
	return nil
}

func (r AdmissionJournalTransitionResult) ValidFor(inventory *AdmissionInventory) bool {
	if r.outcome != AdmissionTransitionDurable || r.inventory != inventory || inventory == nil || inventory.lease == nil || r.candidateRevision != r.previousRevision+1 || r.headerSize == 0 || r.candidateDigest == ([32]byte{}) || r.headerDigest == ([32]byte{}) {
		return false
	}
	l := inventory.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	return r.validLocked(inventory)
}

func (r AdmissionJournalTransitionResult) validLocked(inventory *AdmissionInventory) bool {
	if r.outcome != AdmissionTransitionDurable || r.inventory != inventory || inventory == nil || inventory.lease == nil || r.candidateRevision != r.previousRevision+1 || r.headerSize == 0 || r.candidateDigest == ([32]byte{}) || r.headerDigest == ([32]byte{}) || !inventory.validLocked() || !inventory.snapshotMatchesLocked() || inventory.revision != r.candidateRevision || r.candidateDigest != admissionJournalCandidateDigest(r.journal, r.headerDigest, r.headerSize) {
		return false
	}
	lineage := inventory.lineageMap[inventory.target]
	if lineage == nil {
		return false
	}
	journal := findAdmissionJournal(lineage, r.journal)
	if journal == nil || len(journal.segments) != 1 || journal.segments[0].digest != r.headerDigest || journal.segments[0].stat.size != r.headerSize {
		return false
	}
	lineageIndex := indexOfLineage(inventory, lineage)
	journalIndex := indexOfJournal(lineage.journals, journal)
	if lineageIndex < 0 || journalIndex < 0 || journalIndex >= len(inventory.slot.discovery.lineages[lineageIndex].journals) {
		return false
	}
	for _, held := range inventory.lease.journalLocks {
		if held.lineage == lineage.name && held.name == journal.name {
			return sameIdentity(held.stat, inventory.slot.discovery.lineages[lineageIndex].journals[journalIndex].lock)
		}
	}
	return false
}

// CreateGenerationHeader creates and locks a deterministic generation journal,
// writes exact opaque segment-0 bytes, and advances the full inventory.
func (t *AdmissionMutationToken) CreateGenerationHeader(ctx context.Context, inventory *AdmissionInventory, journal [32]byte, header []byte) (AdmissionJournalTransitionResult, error) {
	headerDigest := sha256.Sum256(header)
	candidate := admissionJournalCandidateDigest(journal, headerDigest, uint64(len(header)))
	pre := AdmissionJournalTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateDigest: candidate, journal: journal, headerDigest: headerDigest, headerSize: uint64(len(header))}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || inventory.absent != nil || journal == ([32]byte{}) || len(header) == 0 || uint64(len(header)) > maximumAdmissionSegmentBytes || inventory.revision == ^uint64(0) {
		return pre, ErrInvalidInput
	}
	pre.candidateRevision = inventory.revision + 1
	if !inventory.snapshotMatchesLocked() {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	lineage := inventory.lineageMap[inventory.target]
	totalJournals := 0
	var journalBytes uint64
	for _, value := range inventory.lineages {
		if value == nil || len(value.journals) > maximumAdmissionJournals-totalJournals {
			return pre, ErrLimit
		}
		totalJournals += len(value.journals)
		for _, existingJournal := range value.journals {
			if existingJournal == nil {
				return pre, ErrLeaseInvalid
			}
			for _, segment := range existingJournal.segments {
				if segment == nil || segment.stat.size > maximumAdmissionJournalBytes-journalBytes {
					return pre, ErrLimit
				}
				journalBytes += segment.stat.size
			}
		}
	}
	if lineage == nil || len(lineage.journals) == maximumAdmissionJournalsPerLineage || totalJournals == maximumAdmissionJournals || uint64(len(header)) > maximumAdmissionJournalBytes-journalBytes {
		return pre, ErrLimit
	}
	lineageIndex := indexOfLineage(inventory, lineage)
	if lineageIndex < 0 || inventory.slot.discovery.lineagesDirectory == nil || lineageIndex >= len(inventory.slot.discovery.lineages) || inventory.slot.discovery.lineages[lineageIndex].name != lineage.name {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	expectedLineage := inventory.slot.discovery.lineages[lineageIndex]
	journalName := hex.EncodeToString(journal[:])
	if findAdmissionJournal(lineage, journal) != nil {
		return pre, ErrInvalidInput
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return pre, err
	}
	lineagesFD, lineageFD, journalFD, lockFD, segmentFD := -1, -1, -1, -1, -1
	lockRetained := false
	mutated := false
	cleanup := func() error {
		failed := false
		for _, fd := range []int{segmentFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if lockFD >= 0 && !lockRetained {
			failed = l.store.ops.unlock(lockFD) != nil || failed
			failed = l.store.checkedClose(lockFD) != nil || failed
		}
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-header-cleanup")
		}
		return nil
	}
	unknownResult := func(cause error) (AdmissionJournalTransitionResult, error) {
		t.consumed = true
		l.revokeLocked()
		return AdmissionJournalTransitionResult{outcome: AdmissionTransitionUnknown, candidateDigest: candidate, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision, journal: journal, headerDigest: headerDigest, headerSize: uint64(len(header))}, unknown(cause)
	}
	fail := func(cause error) (AdmissionJournalTransitionResult, error) {
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
	lineagesFD, lineagesStat, err := l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil || !sameDirectoryIdentity(lineagesStat, *inventory.slot.discovery.lineagesDirectory) {
		if err == nil {
			err = corrupt("generation-lineages-identity")
		}
		return fail(err)
	}
	lineageFD, lineageStat, err := l.store.openVerifiedDirectory(lineagesFD, lineage.name)
	if err != nil || !sameDirectoryIdentity(lineageStat, expectedLineage.stat) {
		if err == nil {
			err = corrupt("generation-lineage-identity")
		}
		return fail(err)
	}
	mutated = true
	if err := l.store.ops.mkdirAt(lineageFD, journalName); err != nil {
		return fail(filesystem("generation-directory-create"))
	}
	if l.store.ops.fsync(lineageFD) != nil {
		return fail(filesystem("generation-directory-parent-sync"))
	}
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, journalName)
	if err != nil {
		return fail(err)
	}
	lockFD, err = l.store.ops.openFileAt(journalFD, "writer.lock", true)
	if err != nil {
		return fail(filesystem("generation-lock-create"))
	}
	lockStat, err := l.store.ops.fstat(lockFD)
	if err != nil || !validRegular(lockStat, l.store.uid, l.store.identity.device) || lockStat.size != 0 {
		return fail(filesystem("generation-lock-identity"))
	}
	lockSyncFailed := l.store.ops.fdatasync(lockFD) != nil
	journalLockSyncFailed := l.store.ops.fsync(journalFD) != nil
	if lockSyncFailed || journalLockSyncFailed {
		return fail(filesystem("generation-lock-sync"))
	}
	locked, err := l.store.ops.tryLock(lockFD)
	if err != nil || !locked {
		return fail(filesystem("generation-lock-acquire"))
	}
	l.journalLocks = append(l.journalLocks, heldJournalLock{lineage: lineage.name, name: journalName, fd: lockFD, stat: lockStat})
	lockRetained = true
	segmentFD, err = l.store.ops.openFileAt(journalFD, admissionSegmentName(0), true)
	if err != nil {
		return fail(filesystem("generation-segment-create"))
	}
	segmentStat, err := l.store.ops.fstat(segmentFD)
	if err != nil || !validRegular(segmentStat, l.store.uid, l.store.identity.device) || segmentStat.size != 0 {
		return fail(filesystem("generation-segment-identity"))
	}
	segmentBefore := segmentStat
	for offset := 0; offset < len(header); {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		written, writeErr := l.store.ops.pwrite(segmentFD, header[offset:], int64(offset))
		if writeErr != nil || written <= 0 || written > len(header)-offset {
			return fail(filesystem("generation-segment-write"))
		}
		offset += written
	}
	segmentStat, err = l.store.ops.fstat(segmentFD)
	if err != nil || !validRegular(segmentStat, l.store.uid, l.store.identity.device) || !sameNodeIdentity(segmentBefore, segmentStat) || segmentBefore.mode != segmentStat.mode || segmentBefore.uid != segmentStat.uid || segmentBefore.nlink != segmentStat.nlink || segmentStat.size != uint64(len(header)) {
		return fail(filesystem("generation-segment-size"))
	}
	segmentSyncFailed := l.store.ops.fdatasync(segmentFD) != nil
	journalSegmentSyncFailed := l.store.ops.fsync(journalFD) != nil
	if segmentSyncFailed || journalSegmentSyncFailed {
		return fail(filesystem("generation-segment-sync"))
	}
	lineageAfter, lineageErr := l.store.ops.fstat(lineageFD)
	journalAfter, journalErr := l.store.ops.fstat(journalFD)
	if lineageErr != nil || journalErr != nil || !validDirectory(lineageAfter, l.store.uid, l.store.identity.device) || !sameDirectoryAfterChildMkdir(expectedLineage.stat, lineageAfter) || !validDirectory(journalAfter, l.store.uid, l.store.identity.device) {
		return fail(filesystem("generation-directory-final-identity"))
	}
	if err := cleanup(); err != nil {
		return unknownResult(err)
	}
	rootFD, lineagesFD, lineageFD, journalFD, segmentFD = -1, -1, -1, -1, -1
	discovery, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil || !generationHeaderDiscoveryMatches(inventory.slot.discovery, discovery, lineage.name, journalName, lineageAfter, journalAfter, lockStat, segmentStat) {
		if err == nil {
			err = filesystem("generation-header-discovery")
		}
		return unknownResult(err)
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		return unknownResult(err)
	}
	nextLineage := next.lineageMap[inventory.target]
	nextJournal := findAdmissionJournal(nextLineage, journal)
	if nextJournal == nil || len(nextJournal.segments) != 1 || nextJournal.segments[0].digest != headerDigest || nextJournal.segments[0].stat.size != uint64(len(header)) || next.fullSet == inventory.fullSet {
		return unknownResult(filesystem("generation-header-missing"))
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		return unknownResult(ErrLeaseInvalid)
	}
	return AdmissionJournalTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, candidateDigest: candidate, candidateRevision: nextRevision, previousRevision: inventory.revision, journal: journal, headerDigest: headerDigest, headerSize: uint64(len(header))}, nil
}

func admissionJournalCandidateDigest(journal, header [32]byte, size uint64) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidencefs-generation-header/v1\x00"))
	h.Write(journal[:])
	h.Write(header[:])
	var raw [8]byte
	for index := 0; index < 8; index++ {
		raw[7-index] = byte(size >> (index * 8))
	}
	h.Write(raw[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationHeaderDiscoveryMatches(previous, next admissionDiscovery, lineageName, journalName string, lineageAfter, journalAfter, lock, segment fileStat) bool {
	if len(previous.lineages) != len(next.lineages) || (previous.lineagesDirectory == nil) != (next.lineagesDirectory == nil) || previous.lineagesDirectory != nil && !sameDirectoryIdentity(*previous.lineagesDirectory, *next.lineagesDirectory) {
		return false
	}
	seen := false
	for index := range previous.lineages {
		before, after := previous.lineages[index], next.lineages[index]
		if before.name != lineageName {
			if !sameAdmissionLineageDiscovery(before, after) {
				return false
			}
			continue
		}
		seen = true
		if before.name != after.name || !sameDirectoryAfterChildMkdir(before.stat, after.stat) || !sameDirectoryIdentity(after.stat, lineageAfter) || !sameIdentity(before.lock, after.lock) || !sameIdentity(before.index, after.index) || len(after.journals) != len(before.journals)+1 {
			return false
		}
		filtered := make([]discoveredJournal, 0, len(before.journals))
		found := false
		for _, journal := range after.journals {
			if journal.name == journalName {
				found = !found && sameDirectoryIdentity(journal.stat, journalAfter) && sameIdentity(journal.lock, lock) && len(journal.segments) == 1 && sameIdentity(journal.segments[0], segment)
				continue
			}
			filtered = append(filtered, journal)
		}
		if !found || len(filtered) != len(before.journals) {
			return false
		}
		for journal := range filtered {
			if !sameAdmissionJournalDiscovery(before.journals[journal], filtered[journal]) {
				return false
			}
		}
	}
	return seen
}

func indexOfLineage(inventory *AdmissionInventory, target *AdmissionLineageView) int {
	for index, lineage := range inventory.lineages {
		if lineage == target {
			return index
		}
	}
	return -1
}

func indexOfJournal(values []*AdmissionJournalView, target *AdmissionJournalView) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}

func findAdmissionJournal(lineage *AdmissionLineageView, id [32]byte) *AdmissionJournalView {
	if lineage == nil {
		return nil
	}
	for _, journal := range lineage.journals {
		if journal != nil && journal.id == id {
			return journal
		}
	}
	return nil
}
