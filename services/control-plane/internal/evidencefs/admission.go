package evidencefs

import (
	"context"
	"encoding/hex"
	"sort"
	"sync"
)

const maximumAdmissionAttempts = 64

type admissionEpoch struct{ seal *struct{} }

type admissionSlot struct {
	epoch        *admissionEpoch
	revision     uint64
	inventory    *AdmissionInventory
	active       bool
	lineages     map[*AdmissionLineageView]lineageExpectation
	journals     map[*AdmissionJournalView]journalExpectation
	files        map[*AdmissionFileView]fileExpectation
	objects      map[*AdmissionObjectView]objectExpectation
	absent       *TargetAbsentFact
	target       [32]byte
	fullSet      [32]byte
	lineageOrder []*AdmissionLineageView
	objectOrder  []*AdmissionObjectView
	discovery    admissionDiscovery
	objectSet    admissionObjectDiscovery
	baseline     [32]byte
	graph        [32]byte
}

type lineageExpectation struct {
	id       [32]byte
	name     string
	index    *AdmissionFileView
	journals []*AdmissionJournalView
}

type journalExpectation struct {
	id       [32]byte
	name     string
	lineage  string
	parent   *AdmissionLineageView
	segments []*AdmissionFileView
}

type fileExpectation struct {
	role        inventoryFileRole
	lineage     string
	journal     string
	name        string
	ordinal     uint32
	parents     []inventoryDirectory
	stat        fileStat
	digest      [32]byte
	identity    [32]byte
	lineageView *AdmissionLineageView
	journalView *AdmissionJournalView
}

type objectExpectation struct {
	file      *AdmissionFileView
	digest    [32]byte
	temporary bool
}

type heldLineageLock struct {
	name string
	fd   int
	stat fileStat
}

// AdmissionLease owns the root writer lock and every writer lock in the
// registered lineage set. It is sealed and cannot be reconstructed from paths,
// descriptors, or filesystem identities.
type AdmissionLease struct {
	self      *AdmissionLease
	seal      *struct{}
	mu        sync.Mutex
	store     *Store
	rootLease *RootLease
	epoch     *admissionEpoch
	current   *admissionSlot
	locks     []heldLineageLock
	valid     bool
	closed    bool
}

func (l *AdmissionLease) Active() bool {
	if l == nil || l.self != l || l.seal == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.activeLocked()
}

func (l *AdmissionLease) activeLocked() bool {
	return l != nil && l.self == l && l.seal != nil && l.store != nil &&
		l.rootLease != nil && l.epoch != nil && l.epoch.seal != nil &&
		l.valid && !l.closed && l.rootLease.Active()
}

// Close always attempts every lineage unlock/close in reverse acquisition
// order and then releases the root lease. A revoked genuine lease remains
// closable so its retained descriptors and locks can still be cleaned up. Any
// uncertain cleanup poisons Store.
func (l *AdmissionLease) Close() error {
	if l == nil || l.self != l || l.seal == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLeaseInvalid
	}
	l.closed, l.valid = true, false
	if l.current != nil {
		l.current.active = false
	}
	failed := releaseLineageLocks(l.store, l.locks)
	l.locks = nil
	if l.rootLease.Close() != nil {
		failed = true
	}
	if failed {
		l.store.poison()
		return filesystem("admission-close")
	}
	return nil
}

func releaseLineageLocks(store *Store, locks []heldLineageLock) bool {
	failed := false
	if store == nil || store.ops == nil {
		return true
	}
	for index := len(locks) - 1; index >= 0; index-- {
		failed = store.ops.unlock(locks[index].fd) != nil || failed
		failed = store.ops.close(locks[index].fd) != nil || failed
	}
	return failed
}

// AcquireAdmission returns revision-zero full-root physical inventory while
// retaining the root lock and every existing registered lineage writer lock.
// An absent target is observed only; this method never creates an entry.
func (s *Store) AcquireAdmission(ctx context.Context, target [32]byte) (*AdmissionLease, *AdmissionInventory, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if s == nil || !s.usable() {
		return nil, nil, filesystem("admission-store")
	}
	for attempt := 0; attempt < maximumAdmissionAttempts; attempt++ {
		if err := contextError(ctx); err != nil {
			return nil, nil, err
		}
		rootLease, err := s.AcquireRoot(ctx)
		if err != nil {
			return nil, nil, err
		}
		first, err := s.discoverAdmissionRoot(ctx)
		if err != nil {
			if rootLease.Close() != nil {
				s.poison()
				return nil, nil, filesystem("admission-discovery-cleanup")
			}
			return nil, nil, err
		}
		locks, retry, lockErr := s.tryAdmissionLocks(ctx, first)
		if lockErr != nil || retry {
			cleanupFailed := releaseLineageLocks(s, locks)
			cleanupFailed = rootLease.Close() != nil || cleanupFailed
			if cleanupFailed {
				s.poison()
				return nil, nil, filesystem("admission-lock-cleanup")
			}
			if lockErr != nil {
				return nil, nil, lockErr
			}
			if err := contextError(ctx); err != nil {
				return nil, nil, err
			}
			if attempt+1 == maximumAdmissionAttempts {
				return nil, nil, filesystem("admission-lock-exhausted")
			}
			if err := lockBackoff(ctx, attempt); err != nil {
				return nil, nil, err
			}
			continue
		}
		second, err := s.discoverAdmissionRoot(ctx)
		if err != nil || !sameAdmissionDiscovery(first, second) {
			cleanupFailed := releaseLineageLocks(s, locks)
			cleanupFailed = rootLease.Close() != nil || cleanupFailed
			if cleanupFailed {
				s.poison()
				return nil, nil, filesystem("admission-postlock-cleanup")
			}
			if err != nil {
				return nil, nil, err
			}
			if attempt+1 == maximumAdmissionAttempts {
				return nil, nil, filesystem("admission-discovery-exhausted")
			}
			if err := lockBackoff(ctx, attempt); err != nil {
				return nil, nil, err
			}
			continue
		}
		epoch := &admissionEpoch{seal: &struct{}{}}
		lease := &AdmissionLease{seal: &struct{}{}, store: s, rootLease: rootLease, epoch: epoch, locks: locks, valid: true}
		lease.self = lease
		inventory, err := lease.buildAdmissionInventory(ctx, target, second)
		if err == nil && (!s.usable() || !rootLease.Active()) {
			err = filesystem("admission-inventory-cleanup")
		}
		if err != nil {
			cleanupFailed := releaseLineageLocks(s, locks)
			cleanupFailed = rootLease.Close() != nil || cleanupFailed
			lease.valid, lease.closed = false, true
			if cleanupFailed {
				s.poison()
				return nil, nil, filesystem("admission-inventory-cleanup")
			}
			return nil, nil, err
		}
		slot := newAdmissionSlot(epoch, inventory)
		inventory.discovery = admissionDiscovery{}
		inventory.objectSet = admissionObjectDiscovery{}
		lease.current = slot
		inventory.slot = slot
		return lease, inventory, nil
	}
	return nil, nil, filesystem("admission-exhausted")
}

func newAdmissionSlot(epoch *admissionEpoch, inventory *AdmissionInventory) *admissionSlot {
	slot := &admissionSlot{
		epoch: epoch, revision: 0, inventory: inventory, active: true,
		lineages: map[*AdmissionLineageView]lineageExpectation{}, journals: map[*AdmissionJournalView]journalExpectation{},
		files: map[*AdmissionFileView]fileExpectation{}, objects: map[*AdmissionObjectView]objectExpectation{}, absent: inventory.absent,
		target: inventory.target, fullSet: inventory.fullSet,
		lineageOrder: append([]*AdmissionLineageView(nil), inventory.lineages...), objectOrder: append([]*AdmissionObjectView(nil), inventory.objects...),
		discovery: cloneAdmissionDiscovery(inventory.discovery), objectSet: cloneAdmissionObjectDiscovery(inventory.objectSet),
	}
	slot.baseline = admissionBaselineDigest(slot.discovery, slot.objectSet)
	for _, lineage := range inventory.lineages {
		slot.lineages[lineage] = lineageExpectation{id: lineage.id, name: lineage.name, index: lineage.index, journals: append([]*AdmissionJournalView(nil), lineage.journals...)}
		slot.files[lineage.index] = expectedFile(lineage.index, lineage, nil)
		for _, journal := range lineage.journals {
			slot.journals[journal] = journalExpectation{id: journal.id, name: journal.name, lineage: journal.lineage, parent: lineage, segments: append([]*AdmissionFileView(nil), journal.segments...)}
			for _, segment := range journal.segments {
				slot.files[segment] = expectedFile(segment, lineage, journal)
			}
		}
	}
	for _, object := range inventory.objects {
		slot.objects[object] = objectExpectation{file: object.file, digest: object.digest, temporary: object.temporary}
		slot.files[object.file] = expectedFile(object.file, nil, nil)
	}
	slot.graph = admissionSlotGraphDigest(slot)
	return slot
}

func expectedFile(file *AdmissionFileView, lineage *AdmissionLineageView, journal *AdmissionJournalView) fileExpectation {
	return fileExpectation{role: file.role, lineage: file.lineage, journal: file.journal, name: file.name, ordinal: file.ordinal,
		parents: append([]inventoryDirectory(nil), file.parents...), stat: file.stat, digest: file.digest, identity: file.identity,
		lineageView: lineage, journalView: journal}
}

func (s *Store) tryAdmissionLocks(ctx context.Context, discovery admissionDiscovery) ([]heldLineageLock, bool, error) {
	locks := make([]heldLineageLock, 0, len(discovery.lineages))
	for _, lineage := range discovery.lineages {
		if err := contextError(ctx); err != nil {
			return locks, false, err
		}
		rootFD, err := s.freshRoot()
		if err != nil {
			return locks, false, err
		}
		lineagesFD, _, err := s.openVerifiedDirectory(rootFD, "lineages")
		closeFailed := s.checkedClose(rootFD) != nil
		if err != nil {
			if lineagesFD >= 0 {
				closeFailed = s.checkedClose(lineagesFD) != nil || closeFailed
			}
			return locks, false, filesystem("admission-lineages-open")
		}
		if closeFailed {
			_ = s.checkedClose(lineagesFD)
			return locks, false, filesystem("admission-root-close")
		}
		lineageFD, lineageStat, err := s.openVerifiedDirectory(lineagesFD, lineage.name)
		closeFailed = s.checkedClose(lineagesFD) != nil
		if err != nil {
			if lineageFD >= 0 {
				closeFailed = s.checkedClose(lineageFD) != nil || closeFailed
			}
			return locks, false, filesystem("admission-lineage-open")
		}
		if closeFailed {
			_ = s.checkedClose(lineageFD)
			return locks, false, filesystem("admission-lineages-close")
		}
		if !sameDirectoryIdentity(lineageStat, lineage.stat) {
			if lineageFD >= 0 {
				closeFailed = s.checkedClose(lineageFD) != nil || closeFailed
			}
			if closeFailed {
				return locks, false, filesystem("admission-lineage-close")
			}
			return locks, true, nil
		}
		lockFD, lockStat, err := s.openVerifiedRegular(lineageFD, "writer.lock")
		closeFailed = s.checkedClose(lineageFD) != nil
		if err != nil {
			if lockFD >= 0 {
				closeFailed = s.checkedClose(lockFD) != nil || closeFailed
			}
			return locks, false, filesystem("admission-lock-open")
		}
		if closeFailed {
			_ = s.checkedClose(lockFD)
			return locks, false, filesystem("admission-lineage-close")
		}
		if !sameIdentity(lockStat, lineage.lock) {
			if lockFD >= 0 {
				closeFailed = s.checkedClose(lockFD) != nil || closeFailed
			}
			if closeFailed {
				return locks, false, filesystem("admission-lock-close")
			}
			return locks, true, nil
		}
		// Retain the current fd before tryLock. On an ambiguous try error the
		// common cleanup path attempts unlock as well as close.
		locks = append(locks, heldLineageLock{name: lineage.name, fd: lockFD, stat: lockStat})
		ok, lockErr := s.ops.tryLock(lockFD)
		if lockErr != nil {
			return locks, false, filesystem("admission-lineage-try-lock")
		}
		if !ok {
			return locks, true, nil
		}
	}
	return locks, false, nil
}

type admissionDiscovery struct {
	lineagesDirectory *fileStat
	lineages          []discoveredLineage
}

type discoveredLineage struct {
	name     string
	stat     fileStat
	lock     fileStat
	index    fileStat
	journals []discoveredJournal
}

type discoveredJournal struct {
	name     string
	stat     fileStat
	lock     fileStat
	segments []fileStat
}

type admissionObjectDiscovery struct {
	objectsStat fileStat
	shaStat     fileStat
	objects     []discoveredObject
}

type discoveredObject struct {
	name  string
	stat  fileStat
	final bool
}

func (l *AdmissionLease) discoverAdmissionObjects() (result admissionObjectDiscovery, resultErr error) {
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return admissionObjectDiscovery{}, err
	}
	defer func() {
		if closeErr := l.store.checkedClose(rootFD); closeErr != nil {
			result, resultErr = admissionObjectDiscovery{}, closeErr
		}
	}()
	objectsFD, objectsStat, err := l.store.openVerifiedDirectory(rootFD, "objects")
	if err != nil {
		return admissionObjectDiscovery{}, err
	}
	defer func() {
		if closeErr := l.store.checkedClose(objectsFD); closeErr != nil {
			result, resultErr = admissionObjectDiscovery{}, closeErr
		}
	}()
	names, err := l.store.ops.readDirNames(objectsFD, 2)
	if err != nil || len(names) != 1 || names[0] != "sha256" {
		return admissionObjectDiscovery{}, filesystem("admission-objects-grammar")
	}
	shaFD, shaStat, err := l.store.openVerifiedDirectory(objectsFD, "sha256")
	if err != nil {
		return admissionObjectDiscovery{}, err
	}
	defer func() {
		if closeErr := l.store.checkedClose(shaFD); closeErr != nil {
			result, resultErr = admissionObjectDiscovery{}, closeErr
		}
	}()
	names, err = l.store.ops.readDirNames(shaFD, maximumStoreNames)
	if err != nil {
		if l.store.ops.isOverflow(err) {
			return admissionObjectDiscovery{}, limit("admission-object-count")
		}
		return admissionObjectDiscovery{}, filesystem("admission-object-list")
	}
	sort.Strings(names)
	result = admissionObjectDiscovery{objectsStat: objectsStat, shaStat: shaStat, objects: make([]discoveredObject, 0, len(names))}
	for _, name := range names {
		final, temp := finalNamePattern.MatchString(name), tempNamePattern.MatchString(name)
		if !final && !temp {
			return admissionObjectDiscovery{}, filesystem("admission-object-name")
		}
		st, err := l.store.statVerifiedRegular(shaFD, name)
		if err != nil {
			return admissionObjectDiscovery{}, err
		}
		result.objects = append(result.objects, discoveredObject{name: name, stat: st, final: final})
	}
	return result, nil
}

func sameAdmissionObjects(a, b admissionObjectDiscovery) bool {
	if !sameDirectoryIdentity(a.objectsStat, b.objectsStat) || !sameDirectoryIdentity(a.shaStat, b.shaStat) || len(a.objects) != len(b.objects) {
		return false
	}
	for index := range a.objects {
		if a.objects[index].name != b.objects[index].name || a.objects[index].final != b.objects[index].final || !sameIdentity(a.objects[index].stat, b.objects[index].stat) {
			return false
		}
	}
	return true
}

func objectDiscoveryMatchesScan(discovery admissionObjectDiscovery, scan *Scan) bool {
	if scan == nil || !scan.valid() {
		return false
	}
	var finalCount, finalBytes, tempCount, tempBytes uint64
	for _, object := range discovery.objects {
		if object.final {
			finalCount++
			finalBytes += object.stat.size
			digest, ok := decodeCanonicalDigest(object.name)
			if !ok {
				return false
			}
			scanned, ok := scan.objects[digest]
			if !ok || !sameIdentity(scanned.stat, object.stat) || scanned.name != object.name {
				return false
			}
		} else {
			tempCount++
			tempBytes += object.stat.size
		}
	}
	return finalCount == scan.finalCount && finalBytes == scan.finalBytes && tempCount == scan.tempCount && tempBytes == scan.tempBytes
}

func (s *Store) discoverAdmissionRoot(ctx context.Context) (result admissionDiscovery, resultErr error) {
	rootFD, err := s.freshRoot()
	if err != nil {
		return admissionDiscovery{}, err
	}
	defer func() {
		if closeErr := s.checkedClose(rootFD); closeErr != nil {
			result, resultErr = admissionDiscovery{}, closeErr
		}
	}()
	before, statErr := s.ops.lstatAt(rootFD, "lineages")
	if statErr != nil {
		if s.ops.isNotExist(statErr) {
			return admissionDiscovery{}, nil
		}
		return admissionDiscovery{}, filesystem("lineages-lstat")
	}
	if !validDirectory(before, s.uid, s.identity.device) {
		return admissionDiscovery{}, filesystem("lineages-identity")
	}
	lineagesFD, after, err := s.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return admissionDiscovery{}, err
	}
	defer func() {
		if closeErr := s.checkedClose(lineagesFD); closeErr != nil {
			result, resultErr = admissionDiscovery{}, closeErr
		}
	}()
	if !sameNodeIdentity(before, after) {
		return admissionDiscovery{}, filesystem("lineages-replaced")
	}
	names, err := s.ops.readDirNames(lineagesFD, maximumAdmissionLineages)
	if err != nil {
		if s.ops.isOverflow(err) {
			return admissionDiscovery{}, limit("lineage-count")
		}
		return admissionDiscovery{}, filesystem("lineage-list")
	}
	sort.Strings(names)
	discovery := admissionDiscovery{lineagesDirectory: &after, lineages: make([]discoveredLineage, 0, len(names))}
	for _, name := range names {
		if err := contextError(ctx); err != nil {
			return admissionDiscovery{}, err
		}
		if !finalNamePattern.MatchString(name) {
			return admissionDiscovery{}, filesystem("lineage-name")
		}
		lineage, err := s.discoverLineage(lineagesFD, name)
		if err != nil {
			return admissionDiscovery{}, err
		}
		discovery.lineages = append(discovery.lineages, lineage)
	}
	return discovery, nil
}

func (s *Store) discoverLineage(lineagesFD int, name string) (result discoveredLineage, resultErr error) {
	fd, st, err := s.openVerifiedDirectory(lineagesFD, name)
	if err != nil {
		return discoveredLineage{}, err
	}
	defer func() {
		if closeErr := s.checkedClose(fd); closeErr != nil {
			result, resultErr = discoveredLineage{}, closeErr
		}
	}()
	names, err := s.ops.readDirNames(fd, maximumAdmissionJournalsPerLineage+2)
	if err != nil {
		if s.ops.isOverflow(err) {
			return discoveredLineage{}, limit("lineage-entry-count")
		}
		return discoveredLineage{}, filesystem("lineage-list")
	}
	sort.Strings(names)
	result = discoveredLineage{name: name, stat: st}
	seenLock, seenIndex := false, false
	for _, child := range names {
		switch child {
		case "writer.lock":
			if seenLock {
				return discoveredLineage{}, filesystem("lineage-duplicate-lock")
			}
			seenLock = true
			result.lock, err = s.statVerifiedRegular(fd, child)
		case "index.caj":
			if seenIndex {
				return discoveredLineage{}, filesystem("lineage-duplicate-index")
			}
			seenIndex = true
			result.index, err = s.statVerifiedRegular(fd, child)
		default:
			if !finalNamePattern.MatchString(child) || len(result.journals) == maximumAdmissionJournalsPerLineage {
				return discoveredLineage{}, filesystem("lineage-entry")
			}
			var journal discoveredJournal
			journal, err = s.discoverJournal(fd, child)
			result.journals = append(result.journals, journal)
		}
		if err != nil {
			return discoveredLineage{}, err
		}
	}
	if !seenLock || !seenIndex {
		return discoveredLineage{}, filesystem("lineage-required-entry")
	}
	return result, nil
}

func (s *Store) discoverJournal(lineageFD int, name string) (result discoveredJournal, resultErr error) {
	fd, st, err := s.openVerifiedDirectory(lineageFD, name)
	if err != nil {
		return discoveredJournal{}, err
	}
	defer func() {
		if closeErr := s.checkedClose(fd); closeErr != nil {
			result, resultErr = discoveredJournal{}, closeErr
		}
	}()
	names, err := s.ops.readDirNames(fd, maximumAdmissionSegments+1)
	if err != nil {
		if s.ops.isOverflow(err) {
			return discoveredJournal{}, limit("journal-entry-count")
		}
		return discoveredJournal{}, filesystem("journal-list")
	}
	sort.Strings(names)
	result = discoveredJournal{name: name, stat: st}
	seenLock := false
	for _, child := range names {
		if child == "writer.lock" {
			if seenLock {
				return discoveredJournal{}, filesystem("journal-duplicate-lock")
			}
			seenLock = true
			result.lock, err = s.statVerifiedRegular(fd, child)
			if err != nil {
				return discoveredJournal{}, err
			}
			continue
		}
		ordinal := len(result.segments)
		if ordinal == maximumAdmissionSegments || child != admissionSegmentName(ordinal) {
			return discoveredJournal{}, filesystem("journal-segment-order")
		}
		segment, err := s.statVerifiedRegular(fd, child)
		if err != nil {
			return discoveredJournal{}, err
		}
		result.segments = append(result.segments, segment)
	}
	if !seenLock || len(result.segments) == 0 {
		return discoveredJournal{}, filesystem("journal-required-entry")
	}
	return result, nil
}

func (s *Store) statVerifiedRegular(parent int, name string) (fileStat, error) {
	fd, st, err := s.openVerifiedRegular(parent, name)
	if err != nil {
		return fileStat{}, err
	}
	if s.ops.close(fd) != nil {
		s.poison()
		return fileStat{}, filesystem("inventory-file-close")
	}
	return st, nil
}

func (s *Store) checkedClose(fd int) error {
	if fd < 0 {
		return nil
	}
	if s.ops.close(fd) != nil {
		s.poison()
		return filesystem("descriptor-close")
	}
	return nil
}

func sameAdmissionDiscovery(a, b admissionDiscovery) bool {
	if (a.lineagesDirectory == nil) != (b.lineagesDirectory == nil) || len(a.lineages) != len(b.lineages) {
		return false
	}
	if a.lineagesDirectory != nil && !sameDirectoryIdentity(*a.lineagesDirectory, *b.lineagesDirectory) {
		return false
	}
	for index := range a.lineages {
		x, y := a.lineages[index], b.lineages[index]
		if x.name != y.name || !sameDirectoryIdentity(x.stat, y.stat) || !sameIdentity(x.lock, y.lock) || !sameIdentity(x.index, y.index) || len(x.journals) != len(y.journals) {
			return false
		}
		for journalIndex := range x.journals {
			j, k := x.journals[journalIndex], y.journals[journalIndex]
			if j.name != k.name || !sameDirectoryIdentity(j.stat, k.stat) || !sameIdentity(j.lock, k.lock) || len(j.segments) != len(k.segments) {
				return false
			}
			for segmentIndex := range j.segments {
				if !sameIdentity(j.segments[segmentIndex], k.segments[segmentIndex]) {
					return false
				}
			}
		}
	}
	return true
}

func sameDirectoryIdentity(a, b fileStat) bool {
	return sameNodeIdentity(a, b) && a.size == b.size && a.mode == b.mode && a.uid == b.uid && a.nlink == b.nlink
}

func admissionSegmentName(ordinal int) string {
	const digits = "0123456789"
	value := ordinal
	result := []byte("segment-00000000.caj")
	for index := 15; index >= 8; index-- {
		result[index] = digits[value%10]
		value /= 10
	}
	return string(result)
}

func decodeCanonicalDigest(name string) ([32]byte, bool) {
	raw, err := hex.DecodeString(name)
	if err != nil || len(raw) != 32 {
		return [32]byte{}, false
	}
	var digest [32]byte
	copy(digest[:], raw)
	return digest, true
}
