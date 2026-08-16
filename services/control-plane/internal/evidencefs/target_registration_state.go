package evidencefs

import "context"

// TargetRegistrationState is the closed physical state of the requested
// target before it has any generation journal. Only evidencefs can mint the
// accompanying fact; the bytes remain opaque to this package.
type TargetRegistrationState string

const (
	TargetRegistrationAbsent          TargetRegistrationState = "target_absent"
	TargetRegistrationPrefixDirectory TargetRegistrationState = "registration_prefix_directory"
	TargetRegistrationPrefixLock      TargetRegistrationState = "registration_prefix_lock"
	TargetRegistrationPrefixIndex     TargetRegistrationState = "registration_prefix_index"
	TargetRegistrationRegisteredEmpty TargetRegistrationState = "registered_empty"
	// Corrupt is terminal: evidencefs returns ErrCorrupt and deliberately
	// mints no inventory or TargetRegistrationFact for that state.
	TargetRegistrationCorrupt TargetRegistrationState = "corrupt"
)

// TargetRegistrationFact is revision-bound observation, not mutation
// authority. A migration-owned verified plan still has to be cross-bound with
// the inventory's one-shot AdmissionMutationToken before recovery can run.
type TargetRegistrationFact struct {
	self    *TargetRegistrationFact
	seal    *struct{}
	owner   *AdmissionInventory
	binding admissionBinding
	state   TargetRegistrationState
	target  [32]byte
	fullSet [32]byte
	name    string
	index   *AdmissionFileView
}

type discoveredTargetRegistration struct {
	state     TargetRegistrationState
	name      string
	directory fileStat
	lock      *fileStat
	index     *fileStat
}

func (i *AdmissionInventory) TargetRegistration() (*TargetRegistrationFact, error) {
	var result *TargetRegistrationFact
	err := i.withValid(func() error { result = i.registration; return nil })
	return result, err
}

func (f *TargetRegistrationFact) State() (TargetRegistrationState, error) {
	var state TargetRegistrationState
	err := f.valid(func() error { state = f.state; return nil })
	return state, err
}

func (f *TargetRegistrationFact) Target() ([32]byte, error) {
	var target [32]byte
	err := f.valid(func() error { target = f.target; return nil })
	return target, err
}

func (f *TargetRegistrationFact) FullSetDigest() ([32]byte, error) {
	var digest [32]byte
	err := f.valid(func() error { digest = f.fullSet; return nil })
	return digest, err
}

// Index returns the bounded prefix-index view only for
// registration_prefix_index. Other valid states return nil.
func (f *TargetRegistrationFact) Index() (*AdmissionFileView, error) {
	var index *AdmissionFileView
	err := f.valid(func() error { index = f.index; return nil })
	return index, err
}

func (f *TargetRegistrationFact) valid(fn func() error) error {
	if f == nil || f.self != f || f.seal == nil || f.owner == nil || !f.binding.validFor(f.owner) {
		return ErrLeaseInvalid
	}
	return f.owner.withValid(func() error {
		if !targetRegistrationFactValidLocked(f.owner, f) {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func targetRegistrationFactValidLocked(owner *AdmissionInventory, fact *TargetRegistrationFact) bool {
	if owner == nil || owner.slot == nil || fact == nil || owner.registration != fact || owner.slot.registration != fact || fact.owner != owner || fact.target != owner.target || fact.fullSet != owner.fullSet || fact.name == "" || fact.name != targetName(owner.target) {
		return false
	}
	switch fact.state {
	case TargetRegistrationAbsent:
		return owner.absent != nil && owner.slot.discovery.registration == nil && owner.lineageMap[owner.target] == nil && fact.index == nil
	case TargetRegistrationPrefixDirectory, TargetRegistrationPrefixLock, TargetRegistrationPrefixIndex:
		discovered := owner.slot.discovery.registration
		if discovered == nil || discovered.state != fact.state || discovered.name != fact.name || owner.absent != nil || owner.lineageMap[owner.target] != nil {
			return false
		}
		if fact.state == TargetRegistrationPrefixDirectory {
			return discovered.lock == nil && discovered.index == nil && fact.index == nil
		}
		if discovered.lock == nil || discovered.lock.size != 0 {
			return false
		}
		if fact.state == TargetRegistrationPrefixLock {
			return discovered.index == nil && fact.index == nil
		}
		if discovered.index == nil || fact.index == nil || !sameIdentity(*discovered.index, fact.index.stat) {
			return false
		}
		expected, ok := owner.slot.files[fact.index]
		return ok && inventoryFileGraphValid(owner, fact.index, expected)
	case TargetRegistrationRegisteredEmpty:
		lineage := owner.lineageMap[owner.target]
		return owner.slot.discovery.registration == nil && owner.absent == nil && lineage != nil && len(lineage.journals) == 0 && len(lineage.registrations) == 0 && fact.index == nil
	default:
		return false
	}
}

func targetName(target [32]byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, 64)
	for index, value := range target {
		result[index*2] = digits[value>>4]
		result[index*2+1] = digits[value&0x0f]
	}
	return string(result)
}

func (l *AdmissionLease) discoverAdmissionRootForInventory(ctx context.Context, inventory *AdmissionInventory) (admissionDiscovery, error) {
	if l == nil || inventory == nil || inventory.lease != l {
		return admissionDiscovery{}, ErrLeaseInvalid
	}
	_, promote := inventory.lineageMap[inventory.target]
	return l.store.discoverAdmissionRootForTarget(ctx, targetName(inventory.target), promote)
}

func targetRegisteredForMutationLocked(inventory *AdmissionInventory) bool {
	if inventory == nil || inventory.absent != nil || inventory.lineageMap[inventory.target] == nil {
		return false
	}
	return inventory.registration == nil || inventory.registration.state == TargetRegistrationRegisteredEmpty
}
