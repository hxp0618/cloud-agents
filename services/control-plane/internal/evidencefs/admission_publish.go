package evidencefs

import (
	"context"
	"crypto/sha256"
	"errors"
)

type AdmissionPublicationTransitionResult struct {
	outcome           AdmissionTransitionOutcome
	inventory         *AdmissionInventory
	publication       *Publication
	candidateDigest   [32]byte
	candidateSequence uint64
	candidateRevision uint64
	previousRevision  uint64
	size              uint64
	reused            bool
}

func (r AdmissionPublicationTransitionResult) Outcome() AdmissionTransitionOutcome { return r.outcome }
func (r AdmissionPublicationTransitionResult) Inventory() *AdmissionInventory      { return r.inventory }
func (r AdmissionPublicationTransitionResult) Publication() *Publication           { return r.publication }
func (r AdmissionPublicationTransitionResult) CandidateKind() string               { return "content_object" }
func (r AdmissionPublicationTransitionResult) CandidateDigest() [32]byte           { return r.candidateDigest }
func (r AdmissionPublicationTransitionResult) CandidateSequence() uint64           { return r.candidateSequence }
func (r AdmissionPublicationTransitionResult) CandidateRevision() uint64           { return r.candidateRevision }
func (r AdmissionPublicationTransitionResult) PreviousRevision() uint64            { return r.previousRevision }
func (r AdmissionPublicationTransitionResult) Size() uint64                        { return r.size }
func (r AdmissionPublicationTransitionResult) Reused() bool                        { return r.reused }

func (r AdmissionPublicationTransitionResult) ValidFor(inventory *AdmissionInventory) bool {
	if r.outcome != AdmissionTransitionDurable || r.inventory != inventory || r.publication == nil || inventory == nil || inventory.lease == nil || r.candidateRevision != r.previousRevision+1 || r.size == 0 {
		return false
	}
	l := inventory.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if !inventory.validLocked() || inventory.revision != r.candidateRevision {
		return false
	}
	l.rootLease.mu.Lock()
	defer l.rootLease.mu.Unlock()
	return r.publication.transientValid() && r.publication.lease == l.rootLease && r.publication.digest == r.candidateDigest && r.publication.size == r.size && r.publication.generation == l.rootLease.generation
}

// Invalidate permanently revokes a durable next pair and consumes its
// transient Publication. It only reduces authority and leaves the genuine
// AdmissionLease closable for deterministic lock/descriptor cleanup.
func (r AdmissionPublicationTransitionResult) Invalidate() error {
	if r.outcome != AdmissionTransitionDurable || r.inventory == nil || r.publication == nil || r.candidateRevision != r.previousRevision+1 {
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
	l.rootLease.mu.Lock()
	defer l.rootLease.mu.Unlock()
	if !r.publication.transientValid() || r.publication.lease != l.rootLease || r.publication.digest != r.candidateDigest || r.publication.generation != l.rootLease.generation {
		l.revokeLocked()
		return ErrLeaseInvalid
	}
	r.publication.consumed = true
	l.revokeLocked()
	return nil
}

// PublishObject consumes this token to publish or durably reuse one exact
// content-addressed object, then advances the full admission inventory. The
// returned Publication remains transient and must be consumed by the immediate
// bind transition before any later publish.
func (t *AdmissionMutationToken) PublishObject(ctx context.Context, inventory *AdmissionInventory, digest [32]byte, source []byte) (AdmissionPublicationTransitionResult, error) {
	pre := AdmissionPublicationTransitionResult{outcome: AdmissionTransitionPreMutationFailure, candidateDigest: digest}
	if t == nil || inventory == nil || t.lease == nil {
		return pre, ErrLeaseInvalid
	}
	l := t.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	pre.previousRevision = inventory.revision
	if !t.validLocked(inventory) || !targetRegisteredForMutationLocked(inventory) || len(source) == 0 || uint64(len(source)) > maximumObjectSize || sha256.Sum256(source) != digest || inventory.revision == ^uint64(0) {
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
	scan, err := l.rootLease.Scan(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return pre, err
		}
		t.consumed = true
		l.revokeLocked()
		return pre, err
	}
	reused := scan.HasObject(digest, uint64(len(source)))
	publication, err := l.rootLease.Publish(ctx, scan, digest, source)
	if err != nil {
		if errors.Is(err, ErrUnknown) {
			t.consumed = true
			l.revokeLocked()
			return AdmissionPublicationTransitionResult{outcome: AdmissionTransitionUnknown, candidateDigest: digest, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision, size: uint64(len(source)), reused: reused}, err
		}
		return pre, err
	}
	unknownResult := func(cause error) (AdmissionPublicationTransitionResult, error) {
		if publication != nil && publication.self == publication {
			publication.consumed = true
		}
		t.consumed = true
		l.revokeLocked()
		return AdmissionPublicationTransitionResult{outcome: AdmissionTransitionUnknown, candidateDigest: digest, candidateRevision: inventory.revision + 1, previousRevision: inventory.revision, size: uint64(len(source)), reused: reused}, unknown(cause)
	}
	if publication == nil || !publication.transientValid() || publication.lease != l.rootLease || publication.digest != digest || publication.size != uint64(len(source)) {
		return unknownResult(ErrLeaseInvalid)
	}
	discovery, err := l.discoverAdmissionRootForInventory(ctx, inventory)
	if err != nil || !sameAdmissionDiscovery(inventory.slot.discovery, discovery) {
		if err == nil {
			err = filesystem("admission-publish-lineages")
		}
		return unknownResult(err)
	}
	nextRevision := inventory.revision + 1
	next, err := l.buildAdmissionInventory(ctx, inventory.target, nextRevision, discovery)
	if err != nil {
		return unknownResult(err)
	}
	found := false
	for _, object := range next.objects {
		if !object.temporary && object.digest == digest && object.file != nil && object.file.stat.size == uint64(len(source)) {
			found = true
			break
		}
	}
	if !found || (next.fullSet == inventory.fullSet) != reused {
		return unknownResult(filesystem("admission-publish-object"))
	}
	nextSlot := newAdmissionSlot(l.epoch, next, nextRevision)
	next.discovery, next.objectSet = admissionDiscovery{}, admissionObjectDiscovery{}
	inventory.slot.active = false
	t.consumed = true
	l.current, next.slot = nextSlot, nextSlot
	if !next.snapshotMatchesLocked() {
		return unknownResult(ErrLeaseInvalid)
	}
	return AdmissionPublicationTransitionResult{outcome: AdmissionTransitionDurable, inventory: next, publication: publication, candidateDigest: digest, candidateRevision: nextRevision, previousRevision: inventory.revision, size: uint64(len(source)), reused: reused}, nil
}
