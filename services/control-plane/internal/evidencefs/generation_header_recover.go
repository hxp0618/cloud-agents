package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"
)

// RecoverGenerationHeader consumes fresh revision-bound authority for a
// directory/lock registration prefix or for a one-segment journal whose bytes
// are an exact prefix of header. The bytes remain opaque to evidencefs; an
// upper-layer verified plan must bind their C3 meaning before invoking this
// transition.
//
// Recovery never removes or renames a prefix. A zero-length or torn segment is
// first truncated to zero and made durable, then rewritten from offset zero.
// Once any durability or mutation operation is attempted, every failure is an
// unknown outcome and revokes the complete admission epoch.
func (t *AdmissionMutationToken) RecoverGenerationHeader(ctx context.Context, inventory *AdmissionInventory, journal [32]byte, header []byte) (AdmissionJournalTransitionResult, error) {
	header = append([]byte(nil), header...)
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
	lineageIndex := indexOfLineage(inventory, lineage)
	if lineage == nil || lineageIndex < 0 || inventory.slot.discovery.lineagesDirectory == nil || lineageIndex >= len(inventory.slot.discovery.lineages) {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	expectedLineage := inventory.slot.discovery.lineages[lineageIndex]
	registration := findGenerationRegistration(lineage, journal)
	journalView := findAdmissionJournal(lineage, journal)
	if (registration == nil) == (journalView == nil) {
		return pre, ErrInvalidInput
	}
	journalName := targetName(journal)
	var observedDirectory fileStat
	var observedLock fileStat
	var observedSegment *AdmissionFileView
	heldJournalLockIndex := -1
	if registration != nil {
		if !generationRegistrationFactValidLocked(inventory, registration) || registration.name != journalName {
			t.consumed = true
			l.revokeLocked()
			return pre, ErrLeaseInvalid
		}
		matches := 0
		for _, observed := range expectedLineage.registrations {
			if observed.name != journalName {
				continue
			}
			matches++
			observedDirectory = observed.stat
			if observed.lock != nil {
				observedLock = *observed.lock
			}
		}
		if matches != 1 {
			t.consumed = true
			l.revokeLocked()
			return pre, ErrLeaseInvalid
		}
	} else {
		if len(journalView.segments) != 1 {
			return pre, ErrInvalidInput
		}
		journalIndex := indexOfJournal(lineage.journals, journalView)
		if journalIndex < 0 || journalIndex >= len(expectedLineage.journals) || expectedLineage.journals[journalIndex].name != journalName {
			t.consumed = true
			l.revokeLocked()
			return pre, ErrLeaseInvalid
		}
		observed := expectedLineage.journals[journalIndex]
		observedDirectory, observedLock, observedSegment = observed.stat, observed.lock, journalView.segments[0]
		for index, held := range l.journalLocks {
			if held.lineage == lineage.name && held.name == journalName {
				if heldJournalLockIndex >= 0 || !sameIdentity(held.stat, observedLock) {
					t.consumed = true
					l.revokeLocked()
					return pre, ErrLeaseInvalid
				}
				heldJournalLockIndex = index
			}
		}
		if heldJournalLockIndex < 0 {
			t.consumed = true
			l.revokeLocked()
			return pre, ErrLeaseInvalid
		}
	}

	var aggregate uint64
	for _, value := range inventory.lineages {
		if value == nil {
			return pre, ErrLeaseInvalid
		}
		for _, existing := range value.journals {
			for _, segment := range existing.segments {
				if segment == nil || segment.stat.size > maximumAdmissionJournalBytes-aggregate {
					return pre, ErrLimit
				}
				aggregate += segment.stat.size
			}
		}
	}
	if observedSegment != nil {
		if observedSegment.stat.size > aggregate {
			return pre, ErrLeaseInvalid
		}
		aggregate -= observedSegment.stat.size
	}
	if uint64(len(header)) > maximumAdmissionJournalBytes-aggregate {
		return pre, ErrLimit
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}

	preflightFailure := func(cause error) (AdmissionJournalTransitionResult, error) {
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			return pre, cause
		}
		t.consumed = true
		l.revokeLocked()
		return pre, cause
	}
	fresh, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil {
		return preflightFailure(err)
	}
	if !sameAdmissionDiscovery(inventory.slot.discovery, fresh) {
		return preflightFailure(corrupt("generation-recovery-preflight-discovery"))
	}
	var prefix []byte
	if observedSegment != nil {
		prefix, err = l.readInventoryFileRaw(ctx, observedSegment)
		if err != nil {
			return preflightFailure(err)
		}
		if sha256.Sum256(prefix) != observedSegment.digest || len(prefix) > len(header) || !bytes.Equal(prefix, header[:len(prefix)]) {
			return preflightFailure(corrupt("generation-recovery-segment-prefix"))
		}
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}

	rootFD, err := l.store.freshRoot()
	if err != nil {
		return preflightFailure(err)
	}
	lineagesFD, lineageFD, journalFD, lockFD, segmentFD := -1, -1, -1, -1, -1
	lockTried, lockRetained := false, false
	mutated := false
	cleanup := func() error {
		failed := l.store.checkedClose(segmentFD) != nil
		if lockFD >= 0 && !lockRetained {
			if lockTried {
				failed = l.store.ops.unlock(lockFD) != nil || failed
			}
			failed = l.store.checkedClose(lockFD) != nil || failed
		}
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			l.store.poison()
			return filesystem("generation-recovery-cleanup")
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
		return preflightFailure(cause)
	}

	lineagesFD, lineagesStat, err := l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil || !sameDirectoryIdentity(lineagesStat, *inventory.slot.discovery.lineagesDirectory) {
		if err == nil {
			err = corrupt("generation-recovery-lineages-identity")
		}
		return fail(err)
	}
	lineageFD, lineageStat, err := l.store.openVerifiedDirectory(lineagesFD, lineage.name)
	if err != nil || !sameDirectoryIdentity(lineageStat, expectedLineage.stat) {
		if err == nil {
			err = corrupt("generation-recovery-lineage-identity")
		}
		return fail(err)
	}
	journalFD, journalStat, err := l.store.openVerifiedDirectory(lineageFD, journalName)
	if err != nil || !sameDirectoryIdentity(journalStat, observedDirectory) {
		if err == nil {
			err = corrupt("generation-recovery-directory-identity")
		}
		return fail(err)
	}

	// First replay durability of the already-observed journal directory entry.
	mutated = true
	if l.store.ops.fsync(lineageFD) != nil {
		return fail(filesystem("generation-recovery-parent-sync"))
	}
	if heldJournalLockIndex >= 0 {
		lockFD = l.journalLocks[heldJournalLockIndex].fd
		lockRetained = true
	} else if registration != nil && registration.state == GenerationRegistrationPrefixDirectory {
		lockFD, err = l.store.ops.openFileAt(journalFD, "writer.lock", true)
		if err != nil {
			return fail(filesystem("generation-recovery-lock-create"))
		}
		observedLock, err = l.store.ops.fstat(lockFD)
		if err != nil || !validRegular(observedLock, l.store.uid, l.store.identity.device) || observedLock.size != 0 {
			return fail(filesystem("generation-recovery-lock-identity"))
		}
	} else {
		lockFD, err = l.store.ops.openFileAtReadWrite(journalFD, "writer.lock")
		if err != nil {
			return fail(filesystem("generation-recovery-lock-open"))
		}
		lockStat, statErr := l.store.ops.fstat(lockFD)
		if statErr != nil || !sameIdentity(lockStat, observedLock) || lockStat.size != 0 {
			return fail(corrupt("generation-recovery-lock-identity"))
		}
	}
	lockSyncFailed := l.store.ops.fdatasync(lockFD) != nil
	journalLockSyncFailed := l.store.ops.fsync(journalFD) != nil
	if lockSyncFailed || journalLockSyncFailed {
		return fail(filesystem("generation-recovery-lock-sync"))
	}
	if heldJournalLockIndex < 0 {
		lockTried = true
		locked, lockErr := l.store.ops.tryLock(lockFD)
		if lockErr != nil || !locked {
			return fail(filesystem("generation-recovery-lock-acquire"))
		}
		l.journalLocks = append(l.journalLocks, heldJournalLock{lineage: lineage.name, name: journalName, fd: lockFD, stat: observedLock})
		lockRetained = true
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}

	var segmentBefore fileStat
	if observedSegment == nil {
		segmentFD, err = l.store.ops.openFileAt(journalFD, admissionSegmentName(0), true)
		if err != nil {
			return fail(filesystem("generation-recovery-segment-create"))
		}
		segmentBefore, err = l.store.ops.fstat(segmentFD)
		if err != nil || !validRegular(segmentBefore, l.store.uid, l.store.identity.device) || segmentBefore.size != 0 {
			return fail(filesystem("generation-recovery-segment-identity"))
		}
	} else {
		segmentFD, err = l.store.ops.openFileAtReadWrite(journalFD, admissionSegmentName(0))
		if err != nil {
			return fail(filesystem("generation-recovery-segment-open"))
		}
		segmentBefore, err = l.store.ops.fstat(segmentFD)
		if err != nil || !sameIdentity(segmentBefore, observedSegment.stat) {
			return fail(corrupt("generation-recovery-segment-identity"))
		}
		if len(prefix) != len(header) {
			if l.store.ops.truncate(segmentFD, 0) != nil {
				return fail(filesystem("generation-recovery-segment-truncate"))
			}
			if l.store.ops.fdatasync(segmentFD) != nil {
				return fail(filesystem("generation-recovery-truncate-sync"))
			}
		}
	}
	if observedSegment == nil || len(prefix) != len(header) {
		for offset := 0; offset < len(header); {
			if err := contextError(ctx); err != nil {
				return fail(err)
			}
			written, writeErr := l.store.ops.pwrite(segmentFD, header[offset:], int64(offset))
			if writeErr != nil || written <= 0 || written > len(header)-offset {
				return fail(filesystem("generation-recovery-segment-write"))
			}
			offset += written
		}
	}
	segmentAfter, err := l.store.ops.fstat(segmentFD)
	if err != nil || !validRegular(segmentAfter, l.store.uid, l.store.identity.device) || !sameNodeIdentity(segmentBefore, segmentAfter) || segmentBefore.mode != segmentAfter.mode || segmentBefore.uid != segmentAfter.uid || segmentBefore.nlink != segmentAfter.nlink || segmentAfter.size != uint64(len(header)) {
		return fail(filesystem("generation-recovery-segment-size"))
	}
	segmentSyncFailed := l.store.ops.fdatasync(segmentFD) != nil
	journalSegmentSyncFailed := l.store.ops.fsync(journalFD) != nil
	if segmentSyncFailed || journalSegmentSyncFailed {
		return fail(filesystem("generation-recovery-segment-sync"))
	}
	lineageAfter, lineageErr := l.store.ops.fstat(lineageFD)
	journalAfter, journalErr := l.store.ops.fstat(journalFD)
	if lineageErr != nil || journalErr != nil || !sameDirectoryIdentity(lineageAfter, expectedLineage.stat) || !validDirectory(journalAfter, l.store.uid, l.store.identity.device) || !sameNodeIdentity(journalAfter, observedDirectory) || journalAfter.mode != observedDirectory.mode || journalAfter.uid != observedDirectory.uid || journalAfter.nlink != observedDirectory.nlink {
		return fail(filesystem("generation-recovery-directory-final-identity"))
	}
	if err := cleanup(); err != nil {
		return unknownResult(err)
	}
	rootFD, lineagesFD, lineageFD, journalFD, lockFD, segmentFD = -1, -1, -1, -1, -1, -1

	discovery, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil || !generationRecoveryDiscoveryMatches(inventory.slot.discovery, discovery, lineage.name, journalName, lineageAfter, journalAfter, observedLock, segmentAfter) {
		if err == nil {
			err = filesystem("generation-recovery-discovery")
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
	if nextJournal == nil || findGenerationRegistration(nextLineage, journal) != nil || len(nextJournal.segments) != 1 || nextJournal.segments[0].digest != headerDigest || nextJournal.segments[0].stat.size != uint64(len(header)) {
		return unknownResult(filesystem("generation-recovery-missing"))
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

func generationRecoveryDiscoveryMatches(previous, next admissionDiscovery, lineageName, journalName string, lineageAfter, journalAfter, lock, segment fileStat) bool {
	expected := cloneAdmissionDiscovery(previous)
	matches := 0
	for index := range expected.lineages {
		lineage := &expected.lineages[index]
		if lineage.name != lineageName {
			continue
		}
		lineage.stat = lineageAfter
		filteredJournals := make([]discoveredJournal, 0, len(lineage.journals)+1)
		for _, journal := range lineage.journals {
			if journal.name == journalName {
				matches++
				continue
			}
			filteredJournals = append(filteredJournals, journal)
		}
		filteredRegistrations := make([]discoveredGenerationRegistration, 0, len(lineage.registrations))
		for _, registration := range lineage.registrations {
			if registration.name == journalName {
				matches++
				continue
			}
			filteredRegistrations = append(filteredRegistrations, registration)
		}
		if matches != 1 {
			return false
		}
		filteredJournals = append(filteredJournals, discoveredJournal{name: journalName, stat: journalAfter, lock: lock, segments: []fileStat{segment}})
		sort.Slice(filteredJournals, func(i, j int) bool { return filteredJournals[i].name < filteredJournals[j].name })
		lineage.journals, lineage.registrations = filteredJournals, filteredRegistrations
		break
	}
	return matches == 1 && sameAdmissionDiscovery(expected, next)
}
