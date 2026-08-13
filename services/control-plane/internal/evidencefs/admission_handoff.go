package evidencefs

import (
	"context"
	"encoding/hex"
	"sync"
)

// GenerationLease is the post-admission filesystem lock authority for one
// exact target lineage and generation journal. It owns no root-wide lock and
// exposes no path or descriptor. Normal-run journal operations are separate
// transitions and are not implied by possession of this lease alone.
type GenerationLease struct {
	self       *GenerationLease
	seal       *struct{}
	mu         *sync.Mutex
	store      *Store
	target     [32]byte
	journal    [32]byte
	lineage    heldLineageLock
	generation heldJournalLock
	binding    *generationLeaseBinding
	valid      bool
	closed     bool
}

type generationLeaseBinding struct {
	lease      *GenerationLease
	store      *Store
	target     [32]byte
	journal    [32]byte
	lineage    fileStat
	generation fileStat
}

// Active reports whether the exact lineage and generation lock ownership is
// still live. A genuine lease remains closable after Store poison.
func (l *GenerationLease) Active() bool {
	if l == nil || l.self != l || l.seal == nil || l.mu == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeLocked()
}

func (l *GenerationLease) activeLocked() bool {
	return l != nil && l.self == l && l.seal != nil && l.store != nil && l.store.usable() && l.binding != nil &&
		l.binding.lease == l && l.binding.store == l.store && l.binding.target == l.target && l.binding.journal == l.journal &&
		sameIdentity(l.binding.lineage, l.lineage.stat) && sameIdentity(l.binding.generation, l.generation.stat) &&
		l.lineage.fd >= 0 && l.generation.fd >= 0 && l.lineage.name == hex.EncodeToString(l.target[:]) &&
		l.generation.lineage == l.lineage.name && l.generation.name == hex.EncodeToString(l.journal[:]) && l.valid && !l.closed
}

func (l *GenerationLease) Target() ([32]byte, error) {
	var target [32]byte
	if l == nil || l.self != l || l.seal == nil || l.mu == nil {
		return target, ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.activeLocked() {
		return target, ErrLeaseInvalid
	}
	return l.target, nil
}

func (l *GenerationLease) Journal() ([32]byte, error) {
	var journal [32]byte
	if l == nil || l.self != l || l.seal == nil || l.mu == nil {
		return journal, ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.activeLocked() {
		return journal, ErrLeaseInvalid
	}
	return l.journal, nil
}

// Close releases the generation lock before its owning lineage lock. Both
// unlock and close operations are attempted independently; uncertainty poisons
// Store and the lease cannot be reused.
func (l *GenerationLease) Close() error {
	if l == nil || l.self != l || l.seal == nil || l.mu == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLeaseInvalid
	}
	l.closed, l.valid = true, false
	failed := releaseJournalLocks(l.store, []heldJournalLock{l.generation})
	failed = releaseLineageLocks(l.store, []heldLineageLock{l.lineage}) || failed
	l.generation.fd, l.lineage.fd = -1, -1
	if failed {
		l.store.poison()
		return filesystem("generation-lease-close")
	}
	return nil
}

// HandoffGeneration consumes the current admission revision authority and
// releases the full-root critical section while retaining only the exact target
// lineage and generation locks. It performs no filesystem mutation and mints
// no journal cursor or database/runtime authority.
func (t *AdmissionMutationToken) HandoffGeneration(ctx context.Context, inventory *AdmissionInventory, journal [32]byte) (*GenerationLease, error) {
	if t == nil || inventory == nil || t.lease == nil || t.inventory != inventory {
		return nil, ErrLeaseInvalid
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if journal == ([32]byte{}) {
		return nil, ErrInvalidInput
	}
	if err := inventory.Revalidate(ctx); err != nil {
		return nil, err
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !t.validLocked(inventory) || inventory.absent != nil || !inventory.snapshotMatchesLocked() {
		return nil, ErrLeaseInvalid
	}
	targetName := hex.EncodeToString(t.target[:])
	journalName := hex.EncodeToString(journal[:])
	targetLineage := inventory.lineageMap[t.target]
	journalView := findAdmissionJournal(targetLineage, journal)
	if targetLineage == nil || journalView == nil {
		return nil, ErrInvalidInput
	}
	targetIndex, journalIndex := indexOfLineage(inventory, targetLineage), indexOfJournal(targetLineage.journals, journalView)
	if targetIndex < 0 || journalIndex < 0 || targetIndex >= len(inventory.slot.discovery.lineages) || journalIndex >= len(inventory.slot.discovery.lineages[targetIndex].journals) {
		return nil, ErrLeaseInvalid
	}
	discoveredLineage := inventory.slot.discovery.lineages[targetIndex]
	discoveredJournal := discoveredLineage.journals[journalIndex]
	lineageLockIndex, journalLockIndex := -1, -1
	for index, held := range l.locks {
		if held.name == targetName {
			if lineageLockIndex >= 0 || !sameIdentity(held.stat, discoveredLineage.lock) {
				return nil, ErrLeaseInvalid
			}
			lineageLockIndex = index
		}
	}
	for index, held := range l.journalLocks {
		if held.lineage == targetName && held.name == journalName {
			if journalLockIndex >= 0 || !sameIdentity(held.stat, discoveredJournal.lock) {
				return nil, ErrLeaseInvalid
			}
			journalLockIndex = index
		}
	}
	if lineageLockIndex < 0 || journalLockIndex < 0 || targetLineage.name != targetName || journalView.lineage != targetName || journalView.name != journalName || !sameHeldJournalLocks(inventory.slot.journalLocks, l.journalLocks) {
		return nil, ErrInvalidInput
	}
	retainedLineage, retainedGeneration := l.locks[lineageLockIndex], l.journalLocks[journalLockIndex]
	otherLineages := append([]heldLineageLock(nil), l.locks[:lineageLockIndex]...)
	otherLineages = append(otherLineages, l.locks[lineageLockIndex+1:]...)
	otherJournals := append([]heldJournalLock(nil), l.journalLocks[:journalLockIndex]...)
	otherJournals = append(otherJournals, l.journalLocks[journalLockIndex+1:]...)

	// Ownership becomes one-shot before the first unlock. Context cancellation
	// after this point cannot short-circuit cleanup or restore old authority.
	t.consumed = true
	l.closed, l.valid = true, false
	if l.current != nil {
		l.current.active = false
	}
	l.locks, l.journalLocks = nil, nil
	failed := releaseJournalLocks(l.store, otherJournals)
	failed = releaseLineageLocks(l.store, otherLineages) || failed
	if l.rootLease.Close() != nil {
		failed = true
	}
	if failed {
		failed = releaseJournalLocks(l.store, []heldJournalLock{retainedGeneration}) || failed
		failed = releaseLineageLocks(l.store, []heldLineageLock{retainedLineage}) || failed
		l.store.poison()
		return nil, filesystem("generation-handoff")
	}
	lease := &GenerationLease{
		seal: &struct{}{}, mu: &sync.Mutex{}, store: l.store, target: t.target, journal: journal,
		lineage: retainedLineage, generation: retainedGeneration, valid: true,
	}
	lease.self = lease
	lease.binding = &generationLeaseBinding{
		lease: lease, store: l.store, target: t.target, journal: journal,
		lineage: retainedLineage.stat, generation: retainedGeneration.stat,
	}
	if !lease.activeLocked() {
		_ = lease.Close()
		l.store.poison()
		return nil, filesystem("generation-handoff-seal")
	}
	return lease, nil
}
