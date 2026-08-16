package evidencefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sort"
)

// RecoverTargetLineage consumes fresh revision-bound authority for one
// observed registration prefix. The caller-provided header remains opaque to
// evidencefs; an upper-layer verified plan must bind its C3 meaning before
// invoking this transition.
//
// Recovery never removes or renames a prefix. A zero-length or final-torn
// index is first truncated to zero and made durable, then rewritten from
// offset zero. Once any durability or mutation operation is attempted, every
// failure is an unknown outcome and revokes the complete admission epoch.
func (t *AdmissionMutationToken) RecoverTargetLineage(ctx context.Context, inventory *AdmissionInventory, indexHeader []byte) (AdmissionTransitionResult, error) {
	digest := sha256.Sum256(indexHeader)
	pre := AdmissionTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateKind: "target_lineage_recovery", candidateDigest: digest}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	registration := inventory.registration
	if !t.validLocked(inventory) || registration == nil || inventory.absent != nil || inventory.revision == ^uint64(0) || len(indexHeader) == 0 || uint64(len(indexHeader)) > maximumAdmissionIndexBytes {
		return pre, ErrInvalidInput
	}
	switch registration.state {
	case TargetRegistrationPrefixDirectory, TargetRegistrationPrefixLock, TargetRegistrationPrefixIndex:
	default:
		return pre, ErrInvalidInput
	}
	indexHeader = append([]byte(nil), indexHeader...)
	pre.candidateRevision = inventory.revision + 1
	if !inventory.snapshotMatchesLocked() {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	observed := inventory.slot.discovery.registration
	if observed == nil || observed.state != registration.state || observed.name != registration.name {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	if len(inventory.lineages) >= maximumAdmissionLineages {
		return pre, ErrLimit
	}
	var aggregate uint64
	for _, lineage := range inventory.lineages {
		if lineage == nil || lineage.index == nil || lineage.index.stat.size > maximumAdmissionIndexAggregate-aggregate {
			return pre, ErrLimit
		}
		aggregate += lineage.index.stat.size
	}
	if uint64(len(indexHeader)) > maximumAdmissionIndexAggregate-aggregate {
		return pre, ErrLimit
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}

	preflightFailure := func(cause error) (AdmissionTransitionResult, error) {
		if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
			return pre, cause
		}
		t.consumed = true
		l.revokeLocked()
		return pre, cause
	}
	fresh, err := l.store.discoverAdmissionRootForTarget(ctx, registration.name, false)
	if err != nil {
		return preflightFailure(err)
	}
	if !sameAdmissionDiscovery(inventory.slot.discovery, fresh) {
		return preflightFailure(corrupt("target-recovery-preflight-discovery"))
	}

	var prefix []byte
	if registration.state == TargetRegistrationPrefixIndex {
		if registration.index == nil {
			return preflightFailure(ErrLeaseInvalid)
		}
		prefix, err = l.readInventoryFileRaw(ctx, registration.index)
		if err != nil {
			return preflightFailure(err)
		}
		if sha256.Sum256(prefix) != registration.index.digest || len(prefix) > len(indexHeader) || !bytes.Equal(prefix, indexHeader[:len(prefix)]) {
			return preflightFailure(corrupt("target-recovery-index-prefix"))
		}
	}

	var retained heldLineageLock
	if observed.lock != nil {
		var ok bool
		retained, ok = heldLineageLockByName(l.locks, registration.name)
		if !ok || !sameIdentity(retained.stat, *observed.lock) {
			return preflightFailure(ErrLeaseInvalid)
		}
		stat, statErr := l.store.ops.fstat(retained.fd)
		if statErr != nil || !sameIdentity(stat, retained.stat) {
			return preflightFailure(corrupt("target-recovery-retained-lock"))
		}
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}

	rootFD, err := l.store.freshRoot()
	if err != nil {
		return preflightFailure(err)
	}
	lineagesFD, lineageFD, indexFD, createdLockFD := -1, -1, -1, -1
	createdLockTried, createdLockRetained := false, false
	mutated := false
	cleanup := func() error {
		failed := false
		failed = l.store.checkedClose(indexFD) != nil || failed
		if createdLockFD >= 0 && !createdLockRetained {
			if createdLockTried {
				failed = l.store.ops.unlock(createdLockFD) != nil || failed
			}
			failed = l.store.checkedClose(createdLockFD) != nil || failed
		}
		for _, fd := range []int{lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			l.store.poison()
			return filesystem("target-recovery-cleanup")
		}
		return nil
	}
	unknownResult := func(cause error) (AdmissionTransitionResult, error) {
		t.consumed = true
		l.revokeLocked()
		return AdmissionTransitionResult{outcome: AdmissionTransitionUnknown, candidateKind: "target_lineage_recovery", candidateDigest: digest, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision}, unknown(cause)
	}
	fail := func(cause error) (AdmissionTransitionResult, error) {
		if cleanupErr := cleanup(); cleanupErr != nil {
			cause = cleanupErr
		}
		if mutated {
			return unknownResult(cause)
		}
		return preflightFailure(cause)
	}

	lineagesFD, lineagesStat, err := l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil || inventory.slot.discovery.lineagesDirectory == nil || !sameDirectoryIdentity(lineagesStat, *inventory.slot.discovery.lineagesDirectory) {
		if err == nil {
			err = corrupt("target-recovery-lineages-identity")
		}
		return fail(err)
	}
	lineageFD, lineageStat, err := l.store.openVerifiedDirectory(lineagesFD, registration.name)
	if err != nil || !sameDirectoryIdentity(lineageStat, observed.directory) {
		if err == nil {
			err = corrupt("target-recovery-directory-identity")
		}
		return fail(err)
	}

	// Every prefix first replays durability of its already-observed directory
	// entry in <root>/lineages.
	mutated = true
	if l.store.ops.fsync(lineagesFD) != nil {
		return fail(filesystem("target-recovery-parent-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}

	lockStat := retained.stat
	if registration.state == TargetRegistrationPrefixDirectory {
		createdLockFD, err = l.store.ops.openFileAt(lineageFD, "writer.lock", true)
		if err != nil {
			return fail(filesystem("target-recovery-lock-create"))
		}
		lockStat, err = l.store.ops.fstat(createdLockFD)
		if err != nil || !validRegular(lockStat, l.store.uid, l.store.identity.device) || lockStat.size != 0 {
			return fail(filesystem("target-recovery-lock-identity"))
		}
		lockSyncFailed := l.store.ops.fdatasync(createdLockFD) != nil
		directorySyncFailed := l.store.ops.fsync(lineageFD) != nil
		if lockSyncFailed || directorySyncFailed {
			return fail(filesystem("target-recovery-lock-sync"))
		}
		createdLockTried = true
		locked, lockErr := l.store.ops.tryLock(createdLockFD)
		if lockErr != nil || !locked {
			return fail(filesystem("target-recovery-lock-acquire"))
		}
		l.locks = append(l.locks, heldLineageLock{name: registration.name, fd: createdLockFD, stat: lockStat})
		sort.Slice(l.locks, func(i, j int) bool { return l.locks[i].name < l.locks[j].name })
		createdLockRetained = true
	} else {
		// AcquireAdmission already performed the fresh nonblocking flock. Replay
		// the existing lock's file and directory durability on that exact held fd.
		lockSyncFailed := l.store.ops.fdatasync(retained.fd) != nil
		directorySyncFailed := l.store.ops.fsync(lineageFD) != nil
		if lockSyncFailed || directorySyncFailed {
			return fail(filesystem("target-recovery-lock-sync"))
		}
		locked, lockErr := l.store.ops.tryLock(retained.fd)
		if lockErr != nil || !locked {
			return fail(filesystem("target-recovery-lock-reacquire"))
		}
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}

	var indexBefore fileStat
	if registration.state == TargetRegistrationPrefixIndex {
		indexFD, err = l.store.ops.openFileAtReadWrite(lineageFD, "index.caj")
		if err != nil {
			return fail(filesystem("target-recovery-index-open"))
		}
		indexBefore, err = l.store.ops.fstat(indexFD)
		if err != nil || registration.index == nil || !sameIdentity(indexBefore, registration.index.stat) {
			return fail(corrupt("target-recovery-index-identity"))
		}
		if len(prefix) != len(indexHeader) {
			if l.store.ops.truncate(indexFD, 0) != nil {
				return fail(filesystem("target-recovery-index-truncate"))
			}
			if l.store.ops.fdatasync(indexFD) != nil {
				return fail(filesystem("target-recovery-truncate-sync"))
			}
		}
	} else {
		indexFD, err = l.store.ops.openFileAt(lineageFD, "index.caj", true)
		if err != nil {
			return fail(filesystem("target-recovery-index-create"))
		}
		indexBefore, err = l.store.ops.fstat(indexFD)
		if err != nil || !validRegular(indexBefore, l.store.uid, l.store.identity.device) || indexBefore.size != 0 {
			return fail(filesystem("target-recovery-index-identity"))
		}
	}
	if registration.state != TargetRegistrationPrefixIndex || len(prefix) != len(indexHeader) {
		for offset := 0; offset < len(indexHeader); {
			if err := contextError(ctx); err != nil {
				return fail(err)
			}
			written, writeErr := l.store.ops.pwrite(indexFD, indexHeader[offset:], int64(offset))
			if writeErr != nil || written <= 0 || written > len(indexHeader)-offset {
				return fail(filesystem("target-recovery-index-write"))
			}
			offset += written
		}
	}
	indexAfter, err := l.store.ops.fstat(indexFD)
	if err != nil || !validRegular(indexAfter, l.store.uid, l.store.identity.device) || !sameNodeIdentity(indexBefore, indexAfter) || indexAfter.mode != indexBefore.mode || indexAfter.uid != indexBefore.uid || indexAfter.nlink != indexBefore.nlink || indexAfter.size != uint64(len(indexHeader)) {
		return fail(filesystem("target-recovery-index-size"))
	}
	indexSyncFailed := l.store.ops.fdatasync(indexFD) != nil
	directorySyncFailed := l.store.ops.fsync(lineageFD) != nil
	if indexSyncFailed || directorySyncFailed {
		return fail(filesystem("target-recovery-index-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}
	if err := cleanup(); err != nil {
		return unknownResult(err)
	}
	rootFD, lineagesFD, lineageFD, indexFD, createdLockFD = -1, -1, -1, -1, -1

	discovery, err := l.store.discoverAdmissionRootForTarget(ctx, registration.name, true)
	if err != nil || !targetRecoveryDiscoveryMatches(inventory.slot.discovery, discovery, registration.name, lockStat, indexAfter) {
		if err == nil {
			err = filesystem("target-recovery-discovery")
		}
		return unknownResult(err)
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		return unknownResult(err)
	}
	registered := next.lineageMap[inventory.target]
	if registered == nil || registered.index == nil || registered.index.digest != digest || registered.index.stat.size != uint64(len(indexHeader)) || len(registered.journals) != 0 || len(registered.registrations) != 0 || next.absent != nil || next.registration == nil || next.registration.state != TargetRegistrationRegisteredEmpty {
		return unknownResult(filesystem("target-recovery-missing"))
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		return unknownResult(ErrLeaseInvalid)
	}
	return AdmissionTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, candidateKind: "target_lineage_recovery", candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision}, nil
}

func targetRecoveryDiscoveryMatches(previous, next admissionDiscovery, target string, lock, index fileStat) bool {
	if previous.registration == nil || previous.registration.name != target || next.registration != nil || previous.lineagesDirectory == nil || next.lineagesDirectory == nil || !sameDirectoryIdentity(*previous.lineagesDirectory, *next.lineagesDirectory) || len(next.lineages) != len(previous.lineages)+1 {
		return false
	}
	filtered := admissionDiscovery{lineagesDirectory: next.lineagesDirectory, lineages: make([]discoveredLineage, 0, len(previous.lineages))}
	seen := false
	for _, lineage := range next.lineages {
		if lineage.name == target {
			if seen || !sameRegistrationDirectoryAfterRecovery(previous.registration, lineage.stat) || !sameIdentity(lineage.lock, lock) || !sameIdentity(lineage.index, index) || len(lineage.journals) != 0 || len(lineage.registrations) != 0 {
				return false
			}
			seen = true
			continue
		}
		filtered.lineages = append(filtered.lineages, lineage)
	}
	old := cloneAdmissionDiscovery(previous)
	old.registration = nil
	return seen && sameAdmissionDiscoveryIgnoringLineagesDirectory(old, filtered)
}

func sameRegistrationDirectoryAfterRecovery(previous *discoveredTargetRegistration, next fileStat) bool {
	if previous == nil {
		return false
	}
	if previous.state == TargetRegistrationPrefixIndex {
		return sameDirectoryIdentity(previous.directory, next)
	}
	// Creating writer.lock and/or index.caj can legitimately change a
	// directory's implementation-defined size, but not its identity, owner,
	// mode, or link count. The final closed grammar check excludes extra names.
	return sameNodeIdentity(previous.directory, next) && previous.directory.mode == next.mode && previous.directory.uid == next.uid && previous.directory.nlink == next.nlink
}
