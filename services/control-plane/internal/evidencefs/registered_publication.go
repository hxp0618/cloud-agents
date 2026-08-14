package evidencefs

import (
	"context"
	"crypto/sha256"
	"errors"
)

// RegisteredPublication is purpose-neutral durable object authority recovered
// from an active full-root admission inventory. It is intentionally distinct
// from Publication: it cannot enter the fresh publish/bind transition and does
// not by itself prove runtime or decision-recovery purpose.
type RegisteredPublication struct {
	self         *RegisteredPublication
	seal         *struct{}
	root         *Root
	digest       [32]byte
	size         uint64
	identity     *Identity
	fullSet      [32]byte
	revision     uint64
	viewIdentity [32]byte
	canonical    [32]byte
}

func (p *RegisteredPublication) Identity() *Identity {
	if !p.valid() {
		return nil
	}
	return p.identity
}

func (p *RegisteredPublication) Matches(digest [32]byte, size uint64) bool {
	return p.valid() && p.digest == digest && p.size == size
}

func (p *RegisteredPublication) SameStore(other *RegisteredPublication) bool {
	return p.valid() && other.valid() && p.root == other.root
}

func (p *RegisteredPublication) SameObject(other *RegisteredPublication) bool {
	return p.SameStore(other) && p.digest == other.digest && p.size == other.size && sameIdentity(p.identity.object, other.identity.object)
}

func (p *RegisteredPublication) valid() bool {
	return p != nil && p.self == p && p.seal != nil && p.root != nil && p.digest != ([32]byte{}) && p.size != 0 && p.identity != nil && p.identity.self == p.identity && p.identity.seal != nil && sameIdentity(p.identity.root, p.root.identity) && validRegular(p.identity.object, p.root.uid, p.root.identity.device) && p.identity.object.size == p.size && p.fullSet != ([32]byte{}) && p.viewIdentity != ([32]byte{}) && p.canonical != ([32]byte{}) && p.canonical == registeredPublicationDigest(p)
}

// RegisterPublication reopens and hashes the exact final object through its
// sealed view, terminally revalidates the complete full-root inventory, and
// then mints purpose-neutral recovery authority. Temporary and zero-length
// objects can never mint this authority.
func (v *AdmissionObjectView) RegisterPublication(ctx context.Context) (*RegisteredPublication, error) {
	if v == nil || v.owner == nil {
		return nil, ErrLeaseInvalid
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	type facts struct {
		root         *Root
		digest       [32]byte
		size         uint64
		object       fileStat
		fullSet      [32]byte
		revision     uint64
		viewIdentity [32]byte
	}
	var before facts
	if err := v.valid(func() error {
		if v.temporary || v.file.stat.size == 0 || v.digest == ([32]byte{}) || v.file.identity == ([32]byte{}) {
			return ErrInvalidInput
		}
		before = facts{v.owner.store, v.digest, v.file.stat.size, v.file.stat, v.owner.fullSet, v.owner.revision, v.file.identity}
		return nil
	}); err != nil {
		return nil, err
	}
	raw, err := v.ReadAll(ctx)
	if err != nil {
		revokeRegisteredPublicationInventory(v.owner, err)
		return nil, err
	}
	if uint64(len(raw)) != before.size || sha256.Sum256(raw) != before.digest {
		revokeRegisteredPublicationInventory(v.owner, ErrCorrupt)
		return nil, ErrCorrupt
	}
	if err := v.owner.Revalidate(ctx); err != nil {
		return nil, err
	}
	var after facts
	if err := v.valid(func() error {
		after = facts{v.owner.store, v.digest, v.file.stat.size, v.file.stat, v.owner.fullSet, v.owner.revision, v.file.identity}
		return nil
	}); err != nil {
		return nil, err
	}
	if before != after || before.root == nil || before.fullSet == ([32]byte{}) || !validRegular(before.object, before.root.uid, before.root.identity.device) {
		revokeRegisteredPublicationInventory(v.owner, ErrCorrupt)
		return nil, ErrCorrupt
	}
	identity := &Identity{seal: &struct{}{}, root: before.root.identity, object: before.object}
	identity.self = identity
	publication := &RegisteredPublication{
		seal: &struct{}{}, root: before.root, digest: before.digest, size: before.size, identity: identity,
		fullSet: before.fullSet, revision: before.revision, viewIdentity: before.viewIdentity,
	}
	publication.self = publication
	publication.canonical = registeredPublicationDigest(publication)
	if !publication.valid() {
		revokeRegisteredPublicationInventory(v.owner, ErrUnknown)
		return nil, ErrUnknown
	}
	return publication, nil
}

func revokeRegisteredPublicationInventory(inventory *AdmissionInventory, cause error) {
	if inventory == nil || inventory.lease == nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return
	}
	inventory.lease.mu.Lock()
	defer inventory.lease.mu.Unlock()
	if inventory.validLocked() {
		inventory.lease.revokeLocked()
	}
}

func registeredPublicationDigest(publication *RegisteredPublication) [32]byte {
	if publication == nil || publication.self != publication || publication.seal == nil || publication.root == nil || publication.identity == nil || publication.identity.self != publication.identity || publication.identity.seal == nil || publication.digest == ([32]byte{}) || publication.size == 0 || publication.fullSet == ([32]byte{}) || publication.viewIdentity == ([32]byte{}) {
		return [32]byte{}
	}
	h := sha256.New()
	_, _ = h.Write([]byte("cloud-agents-platform-registered-publication/v1\x00"))
	writeFullSetEntry(h, "root", "root", publication.identity.root, [32]byte{})
	writeFullSetEntry(h, "object", "final", publication.identity.object, publication.digest)
	_, _ = h.Write(publication.fullSet[:])
	_, _ = h.Write(publication.viewIdentity[:])
	writeFullSetCount(h, publication.revision)
	writeFullSetCount(h, publication.size)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
