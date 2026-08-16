package evidencefs

// GenerationRegistrationState is the closed physical state of an incomplete
// generation journal directory. A directory with writer.lock and segment-0 is
// exposed as a normal AdmissionJournalView; its opaque bytes still need an
// upper-layer verified plan before recovery may run.
type GenerationRegistrationState string

const (
	GenerationRegistrationPrefixDirectory GenerationRegistrationState = "generation_prefix_directory"
	GenerationRegistrationPrefixLock      GenerationRegistrationState = "generation_prefix_lock"
	// Corrupt is terminal: evidencefs returns ErrCorrupt and mints no fact.
	GenerationRegistrationCorrupt GenerationRegistrationState = "corrupt"
)

// GenerationRegistrationFact is a revision-bound physical observation. It is
// not mutation authority and carries no C3 or quota conclusion.
type GenerationRegistrationFact struct {
	self    *GenerationRegistrationFact
	seal    *struct{}
	owner   *AdmissionInventory
	binding admissionBinding
	state   GenerationRegistrationState
	lineage [32]byte
	journal [32]byte
	fullSet [32]byte
	name    string
}

func (f *GenerationRegistrationFact) State() (GenerationRegistrationState, error) {
	var state GenerationRegistrationState
	err := f.valid(func() error { state = f.state; return nil })
	return state, err
}

func (f *GenerationRegistrationFact) Lineage() ([32]byte, error) {
	var lineage [32]byte
	err := f.valid(func() error { lineage = f.lineage; return nil })
	return lineage, err
}

func (f *GenerationRegistrationFact) Journal() ([32]byte, error) {
	var journal [32]byte
	err := f.valid(func() error { journal = f.journal; return nil })
	return journal, err
}

func (f *GenerationRegistrationFact) FullSetDigest() ([32]byte, error) {
	var digest [32]byte
	err := f.valid(func() error { digest = f.fullSet; return nil })
	return digest, err
}

func (f *GenerationRegistrationFact) valid(fn func() error) error {
	if f == nil || f.self != f || f.seal == nil || f.owner == nil || !f.binding.validFor(f.owner) {
		return ErrLeaseInvalid
	}
	return f.owner.withValid(func() error {
		if !generationRegistrationFactValidLocked(f.owner, f) {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func generationRegistrationFactValidLocked(owner *AdmissionInventory, fact *GenerationRegistrationFact) bool {
	if owner == nil || owner.slot == nil || fact == nil || fact.owner != owner || fact.fullSet != owner.fullSet || fact.name == "" {
		return false
	}
	expected, ok := owner.slot.registrations[fact]
	if !ok || expected.state != fact.state || expected.lineage != fact.lineage || expected.journal != fact.journal || expected.name != fact.name || expected.parent == nil || expected.parent.owner != owner || !containsGenerationRegistrationPointer(expected.parent.registrations, fact) {
		return false
	}
	lineageExpected, ok := owner.slot.lineages[expected.parent]
	if !ok || lineageExpected.id != fact.lineage || !containsGenerationRegistrationPointer(lineageExpected.registrations, fact) || fact.name != targetName(fact.journal) {
		return false
	}
	lineageIndex := indexOfLineage(owner, expected.parent)
	if lineageIndex < 0 || lineageIndex >= len(owner.slot.discovery.lineages) {
		return false
	}
	discovered := owner.slot.discovery.lineages[lineageIndex]
	for _, registration := range discovered.registrations {
		if registration.name != fact.name {
			continue
		}
		if registration.state != fact.state || registration.name != expected.name || !sameDirectoryIdentity(registration.stat, expected.directory) || (registration.lock != nil) != expected.hasLock {
			return false
		}
		switch fact.state {
		case GenerationRegistrationPrefixDirectory:
			return registration.lock == nil && !expected.hasLock
		case GenerationRegistrationPrefixLock:
			return registration.lock != nil && registration.lock.size == 0 && expected.hasLock && sameIdentity(*registration.lock, expected.lock)
		default:
			return false
		}
	}
	return false
}

func containsGenerationRegistrationPointer(values []*GenerationRegistrationFact, target *GenerationRegistrationFact) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameGenerationRegistrationPointers(a, b []*GenerationRegistrationFact) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func findGenerationRegistration(lineage *AdmissionLineageView, journal [32]byte) *GenerationRegistrationFact {
	if lineage == nil {
		return nil
	}
	for _, registration := range lineage.registrations {
		if registration != nil && registration.journal == journal {
			return registration
		}
	}
	return nil
}
