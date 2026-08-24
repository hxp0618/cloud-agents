package evidencefs

import (
	"context"
	"errors"
)

type AdmissionBindingTransitionResult struct {
	outcome           AdmissionTransitionOutcome
	inventory         *AdmissionInventory
	publication       *Publication
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
	size              uint64
}

func (r AdmissionBindingTransitionResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r AdmissionBindingTransitionResult) Inventory() *AdmissionInventory      { return r.inventory }
func (r AdmissionBindingTransitionResult) Publication() *Publication           { return r.publication }
func (r AdmissionBindingTransitionResult) CandidateKind() string               { return "content_binding" }
func (r AdmissionBindingTransitionResult) CandidateDigest() [32]byte           { return r.candidateDigest }
func (r AdmissionBindingTransitionResult) CandidateSequence() uint64           { return r.candidateSequence }
func (r AdmissionBindingTransitionResult) CandidateRevision() uint64           { return r.candidateRevision }
func (r AdmissionBindingTransitionResult) PreviousRevision() uint64            { return r.previousRevision }
func (r AdmissionBindingTransitionResult) Size() uint64                        { return r.size }

func (r AdmissionBindingTransitionResult) ValidFor(inventory *AdmissionInventory) bool {
	if r.outcome != AdmissionTransitionDurable || r.inventory != inventory || r.publication == nil || inventory == nil || inventory.lease == nil || r.candidateRevision != r.previousRevision+1 || r.size == 0 || !r.publication.Matches(r.candidateDigest, r.size) {
		return false
	}
	l := inventory.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	return inventory.validLocked() && inventory.revision == r.candidateRevision
}

// Invalidate revokes the durable next admission pair after an upper-layer seal
// failure. The already-bound Publication remains immutable content authority,
// but cannot continue this admission epoch.
func (r AdmissionBindingTransitionResult) Invalidate() error {
	if r.outcome != AdmissionTransitionDurable || r.inventory == nil || r.publication == nil || r.candidateRevision != r.previousRevision+1 || !r.publication.Matches(r.candidateDigest, r.size) {
		return ErrLeaseInvalid
	}
	l := r.inventory.lease
	if l == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !r.inventory.validLocked() || r.inventory.revision != r.candidateRevision {
		return ErrLeaseInvalid
	}
	l.revokeLocked()
	return nil
}

// BindPublishedObject consumes the exact transient Publication under the same
// root lease/generation. All filesystem validation and the next inventory are
// completed before the one-way bind, so success has no fallible work after it.
func (t *AdmissionMutationToken) BindPublishedObject(ctx context.Context, inventory *AdmissionInventory, publication *Publication, digest [32]byte, size uint64) (AdmissionBindingTransitionResult, error) {
	pre := AdmissionBindingTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateDigest: digest, size: size}
	if t == nil || inventory == nil || publication == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || !targetRegisteredForMutationLocked(inventory) || inventory.revision == ^uint64(0) || size == 0 {
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
	l.rootLease.mu.Lock()
	transientExact := publication.transientValid() && publication.lease == l.rootLease && publication.root == l.store && publication.generation == l.rootLease.generation && publication.digest == digest && publication.size == size
	l.rootLease.mu.Unlock()
	if !transientExact {
		return pre, ErrInvalidInput
	}
	discovery, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil || !sameAdmissionDiscovery(inventory.slot.discovery, discovery) {
		if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			return pre, err
		}
		t.consumed = true
		l.revokeLocked()
		if err == nil {
			err = filesystem("admission-bind-lineages")
		}
		return pre, err
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return pre, err
		}
		t.consumed = true
		l.revokeLocked()
		return pre, err
	}
	if next.fullSet != inventory.fullSet {
		t.consumed = true
		l.revokeLocked()
		return pre, filesystem("admission-bind-full-set")
	}
	if err := contextError(ctx); err != nil {
		return pre, err
	}
	if err := l.rootLease.BindPublication(publication, digest, size); err != nil {
		t.consumed = true
		l.revokeLocked()
		return pre, err
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		l.revokeLocked()
		return AdmissionBindingTransitionResult{outcome: AdmissionTransitionUnknown, candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision, size: size}, unknown(ErrLeaseInvalid)
	}
	return AdmissionBindingTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, publication: publication, candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision, size: size}, nil
}
