package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

type AdmissionTransitionOutcome string

const (
	AdmissionTransitionPreMutationFailure AdmissionTransitionOutcome = "pre_mutation_failure"
	AdmissionTransitionUnknown            AdmissionTransitionOutcome = "unknown"
	AdmissionTransitionDurable            AdmissionTransitionOutcome = "durable"
)

type AdmissionTransitionResult struct {
	outcome           AdmissionTransitionOutcome
	inventory         *AdmissionInventory
	candidateKind     string
	candidateDigest   [32]byte
	candidateRevision uint64
	previousRevision  uint64
}

func (r AdmissionTransitionResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r AdmissionTransitionResult) Inventory() *AdmissionInventory      { return r.inventory }
func (r AdmissionTransitionResult) CandidateKind() string               { return r.candidateKind }
func (r AdmissionTransitionResult) CandidateDigest() [32]byte           { return r.candidateDigest }
func (r AdmissionTransitionResult) CandidateRevision() uint64           { return r.candidateRevision }
func (r AdmissionTransitionResult) PreviousRevision() uint64            { return r.previousRevision }

// CreateTargetLineage consumes this token to durably register an absent target
// with exact caller-owned index-header bytes. evidencefs treats those bytes as
// opaque; the migration composite permit must validate their C3 meaning before
// this method is called.
func (t *AdmissionMutationToken) CreateTargetLineage(ctx context.Context, inventory *AdmissionInventory, indexHeader []byte) (AdmissionTransitionResult, error) {
	digest := sha256.Sum256(indexHeader)
	pre := AdmissionTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateKind: "target_lineage", candidateDigest: digest}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || inventory.absent == nil || inventory.absent.owner != inventory || len(indexHeader) == 0 || uint64(len(indexHeader)) > maximumAdmissionIndexBytes || inventory.revision == ^uint64(0) {
		return pre, ErrInvalidInput
	}
	pre.candidateRevision = inventory.revision + 1
	if !inventory.snapshotMatchesLocked() {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	if len(inventory.lineages) == maximumAdmissionLineages {
		return pre, ErrLimit
	}
	indexBytes := uint64(len(indexHeader))
	for _, lineage := range inventory.lineages {
		if lineage == nil || lineage.index == nil || lineage.index.stat.size > maximumAdmissionIndexAggregate-indexBytes {
			return pre, ErrLimit
		}
		indexBytes += lineage.index.stat.size
	}
	if indexBytes > maximumAdmissionIndexAggregate {
		return pre, ErrLimit
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}
	mutated := false
	unknownResult := func(err error) (AdmissionTransitionResult, error) {
		t.consumed = true
		l.revokeLocked()
		return AdmissionTransitionResult{outcome: AdmissionTransitionUnknown, candidateKind: "target_lineage", candidateDigest: digest, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision}, unknown(err)
	}
	closeChecked := func(fd int) error {
		if fd < 0 {
			return nil
		}
		return l.store.checkedClose(fd)
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return pre, err
	}
	lineagesFD, lineageFD, indexFD, lockFD := -1, -1, -1, -1
	lockRetained := false
	cleanup := func() error {
		failed := false
		failed = closeChecked(indexFD) != nil || failed
		if lockFD >= 0 && !lockRetained {
			failed = l.store.ops.unlock(lockFD) != nil || failed
			failed = closeChecked(lockFD) != nil || failed
		}
		failed = closeChecked(lineageFD) != nil || failed
		failed = closeChecked(lineagesFD) != nil || failed
		failed = closeChecked(rootFD) != nil || failed
		if failed {
			return filesystem("target-registration-cleanup")
		}
		return nil
	}
	fail := func(cause error) (AdmissionTransitionResult, error) {
		cleanupErr := cleanup()
		if cleanupErr != nil {
			cause = cleanupErr
		}
		if mutated {
			return unknownResult(cause)
		}
		t.consumed = true
		l.revokeLocked()
		return pre, cause
	}
	if inventory.slot.discovery.lineagesDirectory == nil {
		mutated = true
		if err := l.store.ops.mkdirAt(rootFD, "lineages"); err != nil {
			return fail(filesystem("target-lineages-create"))
		}
		if l.store.ops.fsync(rootFD) != nil {
			return fail(filesystem("target-lineages-parent-sync"))
		}
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
	}
	lineagesFD, _, err = l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return fail(err)
	}
	name := hex.EncodeToString(t.target[:])
	mutated = true
	if err := l.store.ops.mkdirAt(lineagesFD, name); err != nil {
		return fail(filesystem("target-directory-create"))
	}
	if l.store.ops.fsync(lineagesFD) != nil {
		return fail(filesystem("target-directory-parent-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}
	lineageFD, _, err = l.store.openVerifiedDirectory(lineagesFD, name)
	if err != nil {
		return fail(err)
	}
	lockFD, err = l.store.ops.openFileAt(lineageFD, "writer.lock", true)
	if err != nil {
		return fail(filesystem("target-lock-create"))
	}
	lockStat, err := l.store.ops.fstat(lockFD)
	if err != nil || !validRegular(lockStat, l.store.uid, l.store.identity.device) || lockStat.size != 0 {
		return fail(filesystem("target-lock-identity"))
	}
	if l.store.ops.fdatasync(lockFD) != nil || l.store.ops.fsync(lineageFD) != nil {
		return fail(filesystem("target-lock-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}
	locked, err := l.store.ops.tryLock(lockFD)
	if err != nil || !locked {
		return fail(filesystem("target-lock-acquire"))
	}
	l.locks = append(l.locks, heldLineageLock{name: name, fd: lockFD, stat: lockStat})
	sort.Slice(l.locks, func(i, j int) bool { return l.locks[i].name < l.locks[j].name })
	lockRetained = true
	indexFD, err = l.store.ops.openFileAt(lineageFD, "index.caj", true)
	if err != nil {
		return fail(filesystem("target-index-create"))
	}
	indexStat, err := l.store.ops.fstat(indexFD)
	if err != nil || !validRegular(indexStat, l.store.uid, l.store.identity.device) || indexStat.size != 0 {
		return fail(filesystem("target-index-identity"))
	}
	for offset := 0; offset < len(indexHeader); {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		written, writeErr := l.store.ops.write(indexFD, indexHeader[offset:])
		if writeErr != nil || written <= 0 || written > len(indexHeader)-offset {
			return fail(filesystem("target-index-write"))
		}
		offset += written
	}
	if l.store.ops.fdatasync(indexFD) != nil || l.store.ops.fsync(lineageFD) != nil {
		return fail(filesystem("target-index-sync"))
	}
	if err := contextError(ctx); err != nil {
		return fail(err)
	}
	if err := cleanup(); err != nil {
		return unknownResult(err)
	}
	rootFD, lineagesFD, lineageFD, indexFD = -1, -1, -1, -1
	discovery, err := l.store.discoverAdmissionRoot(ctx)
	if err != nil {
		return unknownResult(err)
	}
	if !targetRegistrationDiscoveryMatches(inventory.slot.discovery, discovery, name) {
		return unknownResult(filesystem("target-registration-cardinality"))
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		return unknownResult(err)
	}
	registered, present := next.lineageMap[inventory.target]
	if !present || next.absent != nil || registered.index == nil || registered.index.digest != digest || registered.index.stat.size != uint64(len(indexHeader)) || len(registered.journals) != 0 {
		return unknownResult(filesystem("target-registration-missing"))
	}
	if err := contextError(ctx); err != nil {
		return unknownResult(err)
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		return unknownResult(ErrLeaseInvalid)
	}
	return AdmissionTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, candidateKind: "target_lineage", candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision}, nil
}

func targetRegistrationDiscoveryMatches(previous, next admissionDiscovery, target string) bool {
	if len(next.lineages) != len(previous.lineages)+1 || next.lineagesDirectory == nil {
		return false
	}
	if previous.lineagesDirectory != nil && !sameDirectoryAfterChildMkdir(*previous.lineagesDirectory, *next.lineagesDirectory) {
		return false
	}
	filtered := admissionDiscovery{lineagesDirectory: next.lineagesDirectory, lineages: make([]discoveredLineage, 0, len(previous.lineages))}
	seen := false
	for _, lineage := range next.lineages {
		if lineage.name == target {
			if seen {
				return false
			}
			seen = true
			continue
		}
		filtered.lineages = append(filtered.lineages, lineage)
	}
	return seen && sameAdmissionDiscoveryIgnoringLineagesDirectory(previous, filtered)
}

func sameDirectoryAfterChildMkdir(previous, next fileStat) bool {
	return sameNodeIdentity(previous, next) && previous.mode == next.mode && previous.uid == next.uid && previous.nlink != ^uint64(0) && next.nlink == previous.nlink+1
}

func sameAdmissionDiscoveryIgnoringLineagesDirectory(previous, next admissionDiscovery) bool {
	previous.lineagesDirectory = nil
	next.lineagesDirectory = nil
	return sameAdmissionDiscovery(previous, next)
}
