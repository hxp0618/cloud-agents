package evidencefs

import "context"

// Advance consumes the current mutation token and returns an unchanged
// full-root inventory at revision+1. It performs no filesystem mutation; it is
// the evidencefs half of pure upper-layer authority transitions such as
// sealing reserve-ready state.
func (t *AdmissionMutationToken) Advance(ctx context.Context, inventory *AdmissionInventory) (AdmissionTransitionResult, error) {
	pre := AdmissionTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateKind: "inventory_advance"}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || inventory.revision == ^uint64(0) {
		return pre, ErrInvalidInput
	}
	pre.candidateRevision = inventory.revision + 1
	if !inventory.snapshotMatchesLocked() {
		t.consumed = true
		l.revokeLocked()
		return pre, ErrLeaseInvalid
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}
	discovery, err := l.store.discoverAdmissionRoot(ctx)
	if err != nil || !sameAdmissionDiscovery(inventory.slot.discovery, discovery) {
		if err == nil {
			err = filesystem("admission-advance-lineages")
		}
		t.consumed = true
		l.revokeLocked()
		return pre, err
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		t.consumed = true
		l.revokeLocked()
		return pre, err
	}
	if next.fullSet != inventory.fullSet {
		t.consumed = true
		l.revokeLocked()
		return pre, filesystem("admission-advance-full-set")
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		l.revokeLocked()
		return AdmissionTransitionResult{outcome: AdmissionTransitionUnknown, candidateKind: "inventory_advance", candidateRevision: nextRevision, previousRevision: inventory.revision}, unknown(ErrLeaseInvalid)
	}
	return AdmissionTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, candidateKind: "inventory_advance", candidateRevision: nextRevision, previousRevision: inventory.revision}, nil
}
