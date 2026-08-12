package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

const (
	maximumAdmissionLineages           = 64
	maximumAdmissionJournalsPerLineage = 16
	maximumAdmissionJournals           = 16
	maximumAdmissionSegments           = 16
	maximumAdmissionIndexBytes         = uint64(16 << 20)
	maximumAdmissionIndexAggregate     = uint64(1 << 30)
	maximumAdmissionSegmentBytes       = uint64(16 << 20)
	maximumAdmissionJournalBytes       = uint64(4 << 30)
)

type inventoryFileRole uint8

const (
	inventoryIndex inventoryFileRole = iota + 1
	inventorySegment
	inventoryObject
)

// AdmissionInventory is revision-zero physical authority for the complete
// registered root set. It contains no decoded C3 or quota conclusions.
type AdmissionInventory struct {
	self       *AdmissionInventory
	seal       *struct{}
	store      *Store
	lease      *AdmissionLease
	epoch      *admissionEpoch
	slot       *admissionSlot
	revision   uint64
	target     [32]byte
	lineages   []*AdmissionLineageView
	lineageMap map[[32]byte]*AdmissionLineageView
	objects    []*AdmissionObjectView
	absent     *TargetAbsentFact
	fullSet    [32]byte
}

type AdmissionLineageView struct {
	self     *AdmissionLineageView
	seal     *struct{}
	owner    *AdmissionInventory
	binding  admissionBinding
	id       [32]byte
	name     string
	index    *AdmissionFileView
	journals []*AdmissionJournalView
}

type AdmissionJournalView struct {
	self     *AdmissionJournalView
	seal     *struct{}
	owner    *AdmissionInventory
	binding  admissionBinding
	id       [32]byte
	name     string
	lineage  string
	segments []*AdmissionFileView
}

// AdmissionFileView provides bounded, lease-controlled reads without exposing
// a path, descriptor, device, inode, or mutable byte buffer.
type AdmissionFileView struct {
	self     *AdmissionFileView
	seal     *struct{}
	owner    *AdmissionInventory
	binding  admissionBinding
	role     inventoryFileRole
	lineage  string
	journal  string
	name     string
	ordinal  uint32
	parents  []inventoryDirectory
	stat     fileStat
	digest   [32]byte
	identity [32]byte
}

type inventoryDirectory struct {
	name string
	stat fileStat
}

type AdmissionObjectView struct {
	self      *AdmissionObjectView
	seal      *struct{}
	owner     *AdmissionInventory
	binding   admissionBinding
	file      *AdmissionFileView
	digest    [32]byte
	temporary bool
}

// TargetAbsentFact is minted only when the canonical target has no registered
// lineage in the locked full set.
type TargetAbsentFact struct {
	self    *TargetAbsentFact
	seal    *struct{}
	owner   *AdmissionInventory
	binding admissionBinding
	target  [32]byte
	fullSet [32]byte
}

type admissionBinding struct {
	store    *Store
	lease    *AdmissionLease
	epoch    *admissionEpoch
	revision uint64
}

func (i *AdmissionInventory) bindingForView() admissionBinding {
	return admissionBinding{store: i.store, lease: i.lease, epoch: i.epoch, revision: i.revision}
}

func (b admissionBinding) validFor(i *AdmissionInventory) bool {
	return i != nil && b.store == i.store && b.lease == i.lease && b.epoch == i.epoch && b.revision == i.revision && b.revision == 0
}

func (i *AdmissionInventory) validLocked() bool {
	return i != nil && i.self == i && i.seal != nil && i.store != nil &&
		i.lease != nil && i.lease.self == i.lease && i.lease.store == i.store &&
		i.epoch != nil && i.epoch.seal != nil && i.epoch == i.lease.epoch &&
		i.slot != nil && i.slot == i.lease.current && i.slot.epoch == i.epoch &&
		i.slot.revision == i.revision && i.slot.revision == 0 &&
		i.slot.inventory == i && i.slot.active && i.slot.target == i.target && i.slot.fullSet == i.fullSet &&
		sameLineagePointers(i.slot.lineageOrder, i.lineages) && sameObjectPointers(i.slot.objectOrder, i.objects) && i.slot.absent == i.absent && i.lease.activeLocked()
}

func (i *AdmissionInventory) withValid(fn func() error) error {
	if i == nil || i.lease == nil {
		return ErrLeaseInvalid
	}
	i.lease.mu.Lock()
	defer i.lease.mu.Unlock()
	if !i.validLocked() {
		return ErrLeaseInvalid
	}
	return fn()
}

func (i *AdmissionInventory) Revision() (uint64, error) {
	var revision uint64
	err := i.withValid(func() error { revision = i.revision; return nil })
	return revision, err
}

func (i *AdmissionInventory) FullSetDigest() ([32]byte, error) {
	var digest [32]byte
	err := i.withValid(func() error { digest = i.fullSet; return nil })
	return digest, err
}

func (i *AdmissionInventory) LineageIDs() ([][32]byte, error) {
	var result [][32]byte
	err := i.withValid(func() error {
		result = make([][32]byte, len(i.lineages))
		for index, lineage := range i.lineages {
			result[index] = lineage.id
		}
		return nil
	})
	return result, err
}

func (i *AdmissionInventory) Lineage(id [32]byte) (*AdmissionLineageView, error) {
	var result *AdmissionLineageView
	err := i.withValid(func() error {
		result = i.lineageMap[id]
		if result == nil {
			return ErrInvalidInput
		}
		return nil
	})
	return result, err
}

func (i *AdmissionInventory) Objects() ([]*AdmissionObjectView, error) {
	var result []*AdmissionObjectView
	err := i.withValid(func() error {
		for _, object := range i.objects {
			if !object.temporary {
				result = append(result, object)
			}
		}
		return nil
	})
	return result, err
}

func (i *AdmissionInventory) TemporaryObjects() ([]*AdmissionObjectView, error) {
	var result []*AdmissionObjectView
	err := i.withValid(func() error {
		for _, object := range i.objects {
			if object.temporary {
				result = append(result, object)
			}
		}
		return nil
	})
	return result, err
}

func (i *AdmissionInventory) TargetAbsent() (*TargetAbsentFact, error) {
	var result *TargetAbsentFact
	err := i.withValid(func() error { result = i.absent; return nil })
	return result, err
}

func (f *TargetAbsentFact) Target() ([32]byte, error) {
	var target [32]byte
	if f == nil || f.self != f || f.seal == nil || f.owner == nil || !f.binding.validFor(f.owner) {
		return target, ErrLeaseInvalid
	}
	err := f.owner.withValid(func() error {
		if f.owner.absent != f || f.owner.slot.absent != f || f.target != f.owner.target || f.fullSet != f.owner.fullSet {
			return ErrLeaseInvalid
		}
		target = f.target
		return nil
	})
	return target, err
}

func (f *TargetAbsentFact) FullSetDigest() ([32]byte, error) {
	var digest [32]byte
	if f == nil || f.self != f || f.seal == nil || f.owner == nil || !f.binding.validFor(f.owner) {
		return digest, ErrLeaseInvalid
	}
	err := f.owner.withValid(func() error {
		if f.owner.absent != f || f.owner.slot.absent != f || f.target != f.owner.target || f.fullSet != f.owner.fullSet {
			return ErrLeaseInvalid
		}
		digest = f.fullSet
		return nil
	})
	return digest, err
}

func (v *AdmissionLineageView) ID() ([32]byte, error) {
	var id [32]byte
	err := v.valid(func() error { id = v.id; return nil })
	return id, err
}

func (v *AdmissionLineageView) Index() (*AdmissionFileView, error) {
	var result *AdmissionFileView
	err := v.valid(func() error { result = v.index; return nil })
	return result, err
}

func (v *AdmissionLineageView) Journals() ([]*AdmissionJournalView, error) {
	var result []*AdmissionJournalView
	err := v.valid(func() error { result = append([]*AdmissionJournalView(nil), v.journals...); return nil })
	return result, err
}

func (v *AdmissionLineageView) valid(fn func() error) error {
	if v == nil || v.self != v || v.seal == nil || v.owner == nil || !v.binding.validFor(v.owner) {
		return ErrLeaseInvalid
	}
	return v.owner.withValid(func() error {
		expected, registered := v.owner.slot.lineages[v]
		if !registered || expected.id != v.id || expected.name != v.name || expected.index != v.index || !sameJournalPointers(expected.journals, v.journals) || v.owner.lineageMap[v.id] != v || v.name == "" {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func (v *AdmissionJournalView) ID() ([32]byte, error) {
	var id [32]byte
	err := v.valid(func() error { id = v.id; return nil })
	return id, err
}

func (v *AdmissionJournalView) Segments() ([]*AdmissionFileView, error) {
	var result []*AdmissionFileView
	err := v.valid(func() error { result = append([]*AdmissionFileView(nil), v.segments...); return nil })
	return result, err
}

func (v *AdmissionJournalView) valid(fn func() error) error {
	if v == nil || v.self != v || v.seal == nil || v.owner == nil || !v.binding.validFor(v.owner) {
		return ErrLeaseInvalid
	}
	return v.owner.withValid(func() error {
		expected, registered := v.owner.slot.journals[v]
		parentExpected, parentRegistered := v.owner.slot.lineages[expected.parent]
		if !registered || !parentRegistered || expected.id != v.id || expected.name != v.name || expected.lineage != v.lineage || expected.parent == nil || parentExpected.name != v.lineage || !containsJournalPointer(parentExpected.journals, v) || !sameFilePointers(expected.segments, v.segments) || v.name == "" || v.lineage == "" {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func (v *AdmissionFileView) Size() (uint64, error) {
	var size uint64
	err := v.valid(func() error { size = v.stat.size; return nil })
	return size, err
}

func (v *AdmissionFileView) Digest() ([32]byte, error) {
	var digest [32]byte
	err := v.valid(func() error { digest = v.digest; return nil })
	return digest, err
}

func (v *AdmissionFileView) IdentityDigest() ([32]byte, error) {
	var identity [32]byte
	err := v.valid(func() error { identity = v.identity; return nil })
	return identity, err
}

func (v *AdmissionFileView) Ordinal() (uint32, error) {
	var ordinal uint32
	err := v.valid(func() error { ordinal = v.ordinal; return nil })
	return ordinal, err
}

func (v *AdmissionFileView) ReadAll(ctx context.Context) ([]byte, error) {
	var result []byte
	err := v.valid(func() error {
		var err error
		result, err = v.owner.lease.readInventoryFileLocked(ctx, v)
		return err
	})
	return result, err
}

func (v *AdmissionFileView) valid(fn func() error) error {
	if v == nil || v.self != v || v.seal == nil || v.owner == nil || !v.binding.validFor(v.owner) {
		return ErrLeaseInvalid
	}
	return v.owner.withValid(func() error {
		expected, registered := v.owner.slot.files[v]
		if !registered || !matchesFileExpectation(v.owner.slot, v, expected) || v.role < inventoryIndex || v.role > inventoryObject || v.name == "" || len(v.parents) != 2 && len(v.parents) != 3 || v.identity != inventoryIdentityDigest(v.role, v.lineage, v.journal, v.name, v.ordinal, v.parents, v.stat, v.digest) {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func (v *AdmissionObjectView) Digest() ([32]byte, error) {
	var digest [32]byte
	err := v.valid(func() error { digest = v.digest; return nil })
	return digest, err
}

func (v *AdmissionObjectView) Size() (uint64, error) {
	var size uint64
	err := v.valid(func() error { size = v.file.stat.size; return nil })
	return size, err
}

func (v *AdmissionObjectView) IdentityDigest() ([32]byte, error) {
	var identity [32]byte
	err := v.valid(func() error { identity = v.file.identity; return nil })
	return identity, err
}

func (v *AdmissionObjectView) Temporary() (bool, error) {
	var temporary bool
	err := v.valid(func() error { temporary = v.temporary; return nil })
	return temporary, err
}

func (v *AdmissionObjectView) ReadAll(ctx context.Context) ([]byte, error) {
	var result []byte
	err := v.valid(func() error {
		var err error
		result, err = v.owner.lease.readInventoryFileLocked(ctx, v.file)
		return err
	})
	return result, err
}

func (v *AdmissionObjectView) valid(fn func() error) error {
	if v == nil || v.self != v || v.seal == nil || v.owner == nil || !v.binding.validFor(v.owner) || v.file == nil || v.file.owner != v.owner || v.file.role != inventoryObject || v.digest != v.file.digest {
		return ErrLeaseInvalid
	}
	return v.owner.withValid(func() error {
		expected, registered := v.owner.slot.objects[v]
		if !registered || expected.file != v.file || expected.digest != v.digest || expected.temporary != v.temporary {
			return ErrLeaseInvalid
		}
		fileExpected, registered := v.owner.slot.files[v.file]
		if !registered || !matchesFileExpectation(v.owner.slot, v.file, fileExpected) || v.file.self != v.file || v.file.seal == nil || v.file.name == "" || len(v.file.parents) != 2 || v.file.identity != inventoryIdentityDigest(v.file.role, v.file.lineage, v.file.journal, v.file.name, v.file.ordinal, v.file.parents, v.file.stat, v.file.digest) {
			return ErrLeaseInvalid
		}
		return fn()
	})
}

func sameJournalPointers(a, b []*AdmissionJournalView) bool {
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

func sameLineagePointers(a, b []*AdmissionLineageView) bool {
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

func sameObjectPointers(a, b []*AdmissionObjectView) bool {
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

func containsJournalPointer(values []*AdmissionJournalView, target *AdmissionJournalView) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sameFilePointers(a, b []*AdmissionFileView) bool {
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

func matchesFileExpectation(slot *admissionSlot, v *AdmissionFileView, expected fileExpectation) bool {
	if v.role != expected.role || v.lineage != expected.lineage || v.journal != expected.journal || v.name != expected.name || v.ordinal != expected.ordinal || !sameIdentity(v.stat, expected.stat) || v.digest != expected.digest || v.identity != expected.identity || len(v.parents) != len(expected.parents) {
		return false
	}
	for index := range v.parents {
		if v.parents[index].name != expected.parents[index].name || !sameDirectoryIdentity(v.parents[index].stat, expected.parents[index].stat) {
			return false
		}
	}
	if expected.lineageView != nil {
		lineageExpected, ok := slot.lineages[expected.lineageView]
		if !ok || lineageExpected.name != v.lineage || v.role == inventoryIndex && lineageExpected.index != v {
			return false
		}
	}
	if expected.journalView != nil {
		journalExpected, ok := slot.journals[expected.journalView]
		if !ok || journalExpected.name != v.journal || journalExpected.lineage != v.lineage || !containsFilePointer(journalExpected.segments, v) {
			return false
		}
	}
	return true
}

func containsFilePointer(values []*AdmissionFileView, target *AdmissionFileView) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (l *AdmissionLease) buildAdmissionInventory(ctx context.Context, target [32]byte, discovery admissionDiscovery) (*AdmissionInventory, error) {
	if !l.activeLockedForBuild() {
		return nil, ErrLeaseInvalid
	}
	var indexBytes, journalBytes uint64
	journalCount := 0
	segmentCount := 0
	for _, lineage := range discovery.lineages {
		if lineage.index.size > maximumAdmissionIndexBytes || lineage.index.size > maximumAdmissionIndexAggregate-indexBytes {
			return nil, limit("index-bytes")
		}
		indexBytes += lineage.index.size
		journalCount += len(lineage.journals)
		if journalCount > maximumAdmissionJournals {
			return nil, limit("journal-count")
		}
		for _, journal := range lineage.journals {
			segmentCount += len(journal.segments)
			for _, segment := range journal.segments {
				if segment.size > maximumAdmissionSegmentBytes || segment.size > maximumAdmissionJournalBytes-journalBytes {
					return nil, limit("journal-bytes")
				}
				journalBytes += segment.size
			}
		}
	}
	// Reuse the object-store scanner so its closed object/temp grammar and
	// physical limits remain the single object admission implementation.
	objectsBefore, err := l.discoverAdmissionObjects()
	if err != nil {
		return nil, err
	}
	scan, err := l.rootLease.Scan(ctx)
	if err != nil {
		return nil, err
	}
	objectsAfter, err := l.discoverAdmissionObjects()
	if err != nil {
		return nil, err
	}
	if !sameAdmissionObjects(objectsBefore, objectsAfter) || !objectDiscoveryMatchesScan(objectsAfter, scan) {
		return nil, filesystem("admission-object-mismatch")
	}
	inventory := &AdmissionInventory{seal: &struct{}{}, store: l.store, lease: l, epoch: l.epoch, revision: 0, target: target, lineageMap: map[[32]byte]*AdmissionLineageView{}}
	inventory.self = inventory
	full := sha256.New()
	full.Write([]byte("cloud-agents-platform-evidencefs-admission-full-set/v1\x00"))
	if discovery.lineagesDirectory == nil && len(discovery.lineages) != 0 {
		return nil, filesystem("admission-lineages-directory")
	}
	if discovery.lineagesDirectory == nil {
		writeFullSetEntry(full, "directory-absent", "lineages", fileStat{}, [32]byte{})
	} else {
		writeFullSetEntry(full, "directory", "lineages", *discovery.lineagesDirectory, [32]byte{})
	}
	writeFullSetEntry(full, "directory", "objects", objectsAfter.objectsStat, [32]byte{})
	writeFullSetEntry(full, "directory", "objects/sha256", objectsAfter.shaStat, [32]byte{})
	writeFullSetCount(full, uint64(len(discovery.lineages)))
	writeFullSetCount(full, uint64(len(discovery.lineages))) // one index per registered lineage
	writeFullSetCount(full, uint64(journalCount))
	writeFullSetCount(full, uint64(segmentCount))
	writeFullSetCount(full, uint64(len(scan.objects)))
	writeFullSetCount(full, scan.tempCount)
	writeFullSetCount(full, scan.finalBytes)
	writeFullSetCount(full, scan.tempBytes)
	for _, lineage := range discovery.lineages {
		lineageID, ok := decodeCanonicalDigest(lineage.name)
		if !ok {
			return nil, filesystem("lineage-decode")
		}
		view := &AdmissionLineageView{seal: &struct{}{}, owner: inventory, binding: inventory.bindingForView(), id: lineageID, name: lineage.name}
		view.self = view
		index, err := l.mintInventoryFile(ctx, inventory, inventoryIndex, lineage.name, "", "index.caj", 0,
			[]inventoryDirectory{{name: "lineages", stat: *discovery.lineagesDirectory}, {name: lineage.name, stat: lineage.stat}}, lineage.index)
		if err != nil {
			return nil, err
		}
		view.index = index
		writeFullSetEntry(full, "lineage", lineage.name, lineage.stat, [32]byte{})
		writeFullSetEntry(full, "lineage-lock", lineage.name+"/writer.lock", lineage.lock, [32]byte{})
		writeFullSetView(full, index)
		for _, journal := range lineage.journals {
			journalID, ok := decodeCanonicalDigest(journal.name)
			if !ok {
				return nil, filesystem("journal-decode")
			}
			journalView := &AdmissionJournalView{seal: &struct{}{}, owner: inventory, binding: inventory.bindingForView(), id: journalID, name: journal.name, lineage: lineage.name}
			journalView.self = journalView
			writeFullSetEntry(full, "journal", lineage.name+"/"+journal.name, journal.stat, [32]byte{})
			journalParents := []inventoryDirectory{{name: "lineages", stat: *discovery.lineagesDirectory}, {name: lineage.name, stat: lineage.stat}, {name: journal.name, stat: journal.stat}}
			writeFullSetEntry(full, "journal-lock", lineage.name+"/"+journal.name+"/writer.lock", journal.lock, [32]byte{})
			for ordinal, segment := range journal.segments {
				segmentView, err := l.mintInventoryFile(ctx, inventory, inventorySegment, lineage.name, journal.name, admissionSegmentName(ordinal), uint32(ordinal), journalParents, segment)
				if err != nil {
					return nil, err
				}
				journalView.segments = append(journalView.segments, segmentView)
				writeFullSetView(full, segmentView)
			}
			view.journals = append(view.journals, journalView)
		}
		inventory.lineages = append(inventory.lineages, view)
		inventory.lineageMap[lineageID] = view
	}
	objectParents := []inventoryDirectory{{name: "objects", stat: objectsAfter.objectsStat}, {name: "sha256", stat: objectsAfter.shaStat}}
	for _, object := range objectsAfter.objects {
		file, err := l.mintInventoryFile(ctx, inventory, inventoryObject, "", "", object.name, 0, objectParents, object.stat)
		if err != nil {
			return nil, err
		}
		writeFullSetView(full, file)
		if object.final {
			expected, _ := decodeCanonicalDigest(object.name)
			if file.digest != expected {
				return nil, corrupt("admission-final-digest")
			}
			view := &AdmissionObjectView{seal: &struct{}{}, owner: inventory, binding: inventory.bindingForView(), file: file, digest: file.digest}
			view.self = view
			inventory.objects = append(inventory.objects, view)
		} else {
			view := &AdmissionObjectView{seal: &struct{}{}, owner: inventory, binding: inventory.bindingForView(), file: file, digest: file.digest, temporary: true}
			view.self = view
			inventory.objects = append(inventory.objects, view)
		}
	}
	if err := l.verifyTerminalInventory(ctx, discovery, objectsAfter, inventory); err != nil {
		return nil, err
	}
	copy(inventory.fullSet[:], full.Sum(nil))
	if _, present := inventory.lineageMap[target]; !present {
		fact := &TargetAbsentFact{seal: &struct{}{}, owner: inventory, binding: inventory.bindingForView(), target: target, fullSet: inventory.fullSet}
		fact.self = fact
		inventory.absent = fact
	}
	return inventory, nil
}

func (l *AdmissionLease) verifyTerminalInventory(ctx context.Context, discovery admissionDiscovery, objects admissionObjectDiscovery, inventory *AdmissionInventory) error {
	verifyBoundary := func() (admissionDiscovery, admissionObjectDiscovery, error) {
		root, err := l.store.discoverAdmissionRoot(ctx)
		if err != nil {
			return admissionDiscovery{}, admissionObjectDiscovery{}, err
		}
		objectSet, err := l.discoverAdmissionObjects()
		if err != nil {
			return admissionDiscovery{}, admissionObjectDiscovery{}, err
		}
		scan, err := l.rootLease.Scan(ctx)
		if err != nil {
			return admissionDiscovery{}, admissionObjectDiscovery{}, err
		}
		if !objectDiscoveryMatchesScan(objectSet, scan) {
			return admissionDiscovery{}, admissionObjectDiscovery{}, filesystem("admission-terminal-object-scan")
		}
		return root, objectSet, nil
	}
	beforeRoot, beforeObjects, err := verifyBoundary()
	if err != nil {
		return err
	}
	if !sameAdmissionDiscovery(discovery, beforeRoot) || !sameAdmissionObjects(objects, beforeObjects) {
		return filesystem("admission-terminal-mismatch")
	}
	for _, lineage := range inventory.lineages {
		bytes, err := l.readInventoryFileRaw(ctx, lineage.index)
		if err != nil || sha256.Sum256(bytes) != lineage.index.digest {
			if err != nil {
				return err
			}
			return corrupt("admission-terminal-index")
		}
		for _, journal := range lineage.journals {
			for _, segment := range journal.segments {
				bytes, err := l.readInventoryFileRaw(ctx, segment)
				if err != nil || sha256.Sum256(bytes) != segment.digest {
					if err != nil {
						return err
					}
					return corrupt("admission-terminal-segment")
				}
			}
		}
	}
	for _, object := range inventory.objects {
		bytes, err := l.readInventoryFileRaw(ctx, object.file)
		if err != nil || sha256.Sum256(bytes) != object.digest {
			if err != nil {
				return err
			}
			return corrupt("admission-terminal-object")
		}
	}
	afterRoot, afterObjects, err := verifyBoundary()
	if err != nil {
		return err
	}
	if !sameAdmissionDiscovery(discovery, afterRoot) || !sameAdmissionDiscovery(beforeRoot, afterRoot) || !sameAdmissionObjects(objects, afterObjects) || !sameAdmissionObjects(beforeObjects, afterObjects) || !l.store.usable() || !l.rootLease.Active() {
		return filesystem("admission-terminal-mismatch")
	}
	return nil
}

func (l *AdmissionLease) activeLockedForBuild() bool {
	return l != nil && l.self == l && l.seal != nil && l.store != nil && l.rootLease != nil && l.epoch != nil && l.epoch.seal != nil && l.valid && !l.closed && l.rootLease.Active()
}

func (l *AdmissionLease) mintInventoryFile(ctx context.Context, owner *AdmissionInventory, role inventoryFileRole, lineage, journal, name string, ordinal uint32, parents []inventoryDirectory, st fileStat) (*AdmissionFileView, error) {
	probe := &AdmissionFileView{owner: owner, binding: owner.bindingForView(), role: role, lineage: lineage, journal: journal, name: name, ordinal: ordinal, parents: append([]inventoryDirectory(nil), parents...), stat: st}
	bytes, err := l.readInventoryFileRaw(ctx, probe)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(bytes)
	return mintInventoryFileKnownDigest(owner, role, lineage, journal, name, ordinal, parents, st, digest), nil
}

func mintInventoryFileKnownDigest(owner *AdmissionInventory, role inventoryFileRole, lineage, journal, name string, ordinal uint32, parents []inventoryDirectory, st fileStat, digest [32]byte) *AdmissionFileView {
	view := &AdmissionFileView{seal: &struct{}{}, owner: owner, binding: owner.bindingForView(), role: role, lineage: lineage, journal: journal, name: name, ordinal: ordinal, parents: append([]inventoryDirectory(nil), parents...), stat: st, digest: digest}
	view.identity = inventoryIdentityDigest(role, lineage, journal, name, ordinal, view.parents, st, digest)
	view.self = view
	return view
}

func (l *AdmissionLease) readInventoryFileLocked(ctx context.Context, view *AdmissionFileView) ([]byte, error) {
	bytes, err := l.readInventoryFileRaw(ctx, view)
	if err != nil {
		return nil, err
	}
	if !l.store.usable() {
		return nil, filesystem("inventory-read-cleanup")
	}
	if sha256.Sum256(bytes) != view.digest {
		return nil, corrupt("inventory-content-mutated")
	}
	return bytes, nil
}

func (l *AdmissionLease) readInventoryFileRaw(ctx context.Context, view *AdmissionFileView) (result []byte, resultErr error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return nil, err
	}
	defer l.closeReadFD(&rootFD, &result, &resultErr)()
	var fd int
	switch view.role {
	case inventoryObject:
		objectsFD, objectsStat, err := l.store.openVerifiedDirectory(rootFD, "objects")
		if err != nil {
			return nil, err
		}
		defer l.closeReadFD(&objectsFD, &result, &resultErr)()
		if len(view.parents) != 2 || !sameDirectoryIdentity(objectsStat, view.parents[0].stat) {
			return nil, corrupt("inventory-object-directory")
		}
		shaFD, shaStat, err := l.store.openVerifiedDirectory(objectsFD, "sha256")
		if err != nil {
			return nil, err
		}
		defer l.closeReadFD(&shaFD, &result, &resultErr)()
		if !sameDirectoryIdentity(shaStat, view.parents[1].stat) {
			return nil, corrupt("inventory-sha-directory")
		}
		fd, _, err = l.store.openVerifiedRegular(shaFD, view.name)
		if err != nil {
			return nil, err
		}
	default:
		lineagesFD, lineagesStat, err := l.store.openVerifiedDirectory(rootFD, "lineages")
		if err != nil {
			return nil, err
		}
		defer l.closeReadFD(&lineagesFD, &result, &resultErr)()
		if len(view.parents) < 2 || !sameDirectoryIdentity(lineagesStat, view.parents[0].stat) {
			return nil, corrupt("inventory-lineages-directory")
		}
		lineageFD, lineageStat, err := l.store.openVerifiedDirectory(lineagesFD, view.lineage)
		if err != nil {
			return nil, err
		}
		defer l.closeReadFD(&lineageFD, &result, &resultErr)()
		if !sameDirectoryIdentity(lineageStat, view.parents[1].stat) {
			return nil, corrupt("inventory-lineage-directory")
		}
		parent := lineageFD
		if view.journal != "" {
			journalFD, journalStat, err := l.store.openVerifiedDirectory(lineageFD, view.journal)
			if err != nil {
				return nil, err
			}
			defer l.closeReadFD(&journalFD, &result, &resultErr)()
			if len(view.parents) != 3 || !sameDirectoryIdentity(journalStat, view.parents[2].stat) {
				return nil, corrupt("inventory-journal-directory")
			}
			parent = journalFD
		}
		fd, _, err = l.store.openVerifiedRegular(parent, view.name)
		if err != nil {
			return nil, err
		}
	}
	defer l.closeReadFD(&fd, &result, &resultErr)()
	before, err := l.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, view.stat) {
		return nil, corrupt("inventory-file-identity")
	}
	if before.size > uint64(^uint(0)>>1) {
		return nil, limit("inventory-read-size")
	}
	result = make([]byte, int(before.size))
	var offset uint64
	for offset < before.size {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		n, readErr := l.store.ops.pread(fd, result[offset:], int64(offset))
		if n > 0 {
			offset += uint64(n)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, filesystem("inventory-read")
		}
		if n == 0 {
			return nil, corrupt("inventory-short-read")
		}
	}
	after, err := l.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, after) || !sameIdentity(after, view.stat) {
		return nil, corrupt("inventory-read-mutated")
	}
	return result, nil
}

func (l *AdmissionLease) closeReadFD(fd *int, result *[]byte, resultErr *error) func() {
	return func() {
		if fd == nil || *fd < 0 {
			return
		}
		current := *fd
		*fd = -1
		if l.store.checkedClose(current) != nil {
			*result = nil
			*resultErr = filesystem("inventory-read-close")
		}
	}
}

func inventoryIdentityDigest(role inventoryFileRole, lineage, journal, name string, ordinal uint32, parents []inventoryDirectory, st fileStat, digest [32]byte) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidencefs-inventory-file/v1\x00"))
	h.Write([]byte{byte(role)})
	h.Write([]byte(lineage))
	h.Write([]byte{0})
	h.Write([]byte(journal))
	h.Write([]byte{0})
	h.Write([]byte(name))
	h.Write([]byte{0})
	var encoded [8]byte
	binary.BigEndian.PutUint32(encoded[:4], ordinal)
	h.Write(encoded[:4])
	writeFullSetCount(h, uint64(len(parents)))
	for _, parent := range parents {
		h.Write([]byte(parent.name))
		h.Write([]byte{0})
		writeFullSetCount(h, parent.stat.device)
		writeFullSetCount(h, parent.stat.inode)
	}
	binary.BigEndian.PutUint64(encoded[:], st.device)
	h.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], st.inode)
	h.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], st.size)
	h.Write(encoded[:])
	binary.BigEndian.PutUint32(encoded[:4], st.mode)
	h.Write(encoded[:4])
	binary.BigEndian.PutUint32(encoded[:4], st.uid)
	h.Write(encoded[:4])
	binary.BigEndian.PutUint64(encoded[:], st.nlink)
	h.Write(encoded[:])
	h.Write([]byte{byte(st.kind)})
	h.Write(digest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func writeFullSetCount(w io.Writer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = w.Write(encoded[:])
}

func writeFullSetView(w io.Writer, view *AdmissionFileView) {
	_, _ = w.Write([]byte{byte(view.role)})
	_, _ = w.Write(view.identity[:])
	_, _ = w.Write(view.digest[:])
	writeFullSetCount(w, view.stat.size)
}

func writeFullSetEntry(w io.Writer, role, name string, st fileStat, digest [32]byte) {
	_, _ = w.Write([]byte(role))
	_, _ = w.Write([]byte{0})
	_, _ = w.Write([]byte(name))
	_, _ = w.Write([]byte{0})
	writeFullSetCount(w, st.device)
	writeFullSetCount(w, st.inode)
	writeFullSetCount(w, st.size)
	writeFullSetCount(w, uint64(st.mode))
	writeFullSetCount(w, uint64(st.uid))
	writeFullSetCount(w, st.nlink)
	writeFullSetCount(w, uint64(st.kind))
	_, _ = w.Write(digest[:])
}
