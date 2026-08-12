package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

const fakeSupportedFilesystem int64 = 0xEF53

func newEvidenceVerifiedReplaySourceForTest(reader io.Reader, kind frameContainerKind, identity string, ordinal, finalOrdinal uint32, observedSize uint64) *evidenceVerifiedReplaySource {
	if ordinal > finalOrdinal || identity == "" || (kind != evidenceSegmentContainer && kind != lineageIndexContainer) {
		return nil
	}
	root := &evidenceFSRoot{fd: 1, uid: 501, device: 7}
	inventory := &verifiedEvidenceInventory{seal: &struct{}{}, root: root, parentFD: 2, kind: kind, identity: identity, entries: make([]verifiedInventoryEntry, finalOrdinal+1), consumed: map[uint32]bool{}}
	for index := range inventory.entries {
		name := fmt.Sprintf("segment-%08d.caj", index)
		if kind == lineageIndexContainer {
			name = "index.caj"
		}
		inventory.entries[index] = verifiedInventoryEntry{name: name, device: 7, inode: uint64(17 + index), observedSize: observedSize, ordinal: uint32(index)}
	}
	inventory.canonical = inventoryCanonicalDigest(inventory.kind, inventory.identity, inventory.entries)
	descriptor, err := inventory.segment(ordinal)
	if err != nil {
		return nil
	}
	entry := inventory.entries[descriptor.index]
	inventory.consumed[descriptor.index] = true
	return &evidenceVerifiedReplaySource{owner: inventory, entryIndex: descriptor.index, canonical: descriptor.canonical, fd: -1, device: entry.device, inode: entry.inode, observedSize: entry.observedSize, containerKind: kind, identity: identity, ordinal: entry.ordinal, reader: reader}
}

type fakeEvidenceFSOps struct {
	stats                  map[int]evidenceFileStat
	children               map[int]map[string]int
	fsType                 int64
	nextFD                 int
	files                  map[int]map[string]bool
	renameTargets          map[int]map[string]bool
	locks                  map[int]bool
	busy                   map[int]int
	closed                 map[int]bool
	unlocks                []int
	lockAttempts           []int
	fdNames                map[int]string
	lockedNames            map[string]bool
	disableNamedLockDomain bool
	mountOK                bool
	readers                map[int]*strings.Reader
	directoryNames         map[int][]string
	fdatasyncErr           error
	fsyncErr               error
	renameErr              error
	linkErr                error
	unlinkErr              error
	closeErr               error
	statErr                error
	filesystemErr          error
	openRootErr            error
	openDirErr             error
	openFileErr            error
	lockErr                error
	unlockErr              error
	probeCancelAt          string
	probeCancel            context.CancelFunc
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func newFakeEvidenceFS() *fakeEvidenceFSOps {
	f := &fakeEvidenceFSOps{
		stats:          map[int]evidenceFileStat{},
		children:       map[int]map[string]int{},
		fsType:         fakeSupportedFilesystem,
		nextFD:         100,
		files:          map[int]map[string]bool{},
		renameTargets:  map[int]map[string]bool{},
		locks:          map[int]bool{},
		busy:           map[int]int{},
		closed:         map[int]bool{},
		fdNames:        map[int]string{},
		lockedNames:    map[string]bool{},
		mountOK:        true,
		readers:        map[int]*strings.Reader{},
		directoryNames: map[int][]string{},
	}
	f.stats[1] = fakeDirectoryStat(7)
	f.files[1] = map[string]bool{}
	f.renameTargets[1] = map[string]bool{}
	return f
}

func fakeDirectoryStat(device uint64) evidenceFileStat {
	return evidenceFileStat{device: device, inode: device + 10, mode: 0o700, uid: 501, nlink: 2, kind: evidenceFileDirectory}
}
func fakeRegularStat(device uint64) evidenceFileStat {
	return evidenceFileStat{device: device, inode: device + 20, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
}

func (f *fakeEvidenceFSOps) openRoot(string) (int, error) { return 1, f.openRootErr }
func (f *fakeEvidenceFSOps) openDirAt(parent int, name string) (int, error) {
	if f.openDirErr != nil {
		return -1, f.openDirErr
	}
	fd, ok := f.children[parent][name]
	if !ok {
		return -1, errors.New("missing")
	}
	return fd, nil
}
func (f *fakeEvidenceFSOps) openFileAt(parent int, name string, create bool) (int, bool, error) {
	if f.openFileErr != nil {
		return -1, false, f.openFileErr
	}
	if create && f.files[parent][name] {
		return -1, false, errors.New("exists")
	}
	if !create && !f.files[parent][name] {
		return -1, false, errors.New("missing")
	}
	fd := f.nextFD
	f.nextFD++
	if create {
		f.files[parent][name] = true
	}
	if _, ok := f.stats[fd]; !ok {
		f.stats[fd] = fakeRegularStat(f.stats[parent].device)
	}
	if !f.disableNamedLockDomain {
		f.fdNames[fd] = name
	}
	return fd, true, nil
}
func (f *fakeEvidenceFSOps) stat(fd int) (evidenceFileStat, error) {
	if f.statErr != nil {
		return evidenceFileStat{}, f.statErr
	}
	return f.stats[fd], nil
}
func (f *fakeEvidenceFSOps) filesystemType(int) (int64, error) { return f.fsType, f.filesystemErr }
func (f *fakeEvidenceFSOps) verifyMount(_ int, fsType int64, st evidenceFileStat) error {
	if !f.mountOK || fsType != fakeSupportedFilesystem || st.device == 0 {
		return errors.New("unsupported")
	}
	return nil
}
func (f *fakeEvidenceFSOps) fdatasync(int) error                { return f.fdatasyncErr }
func (f *fakeEvidenceFSOps) fsync(int) error                    { return f.fsyncErr }
func (f *fakeEvidenceFSOps) write(_ int, p []byte) (int, error) { return len(p), nil }
func (f *fakeEvidenceFSOps) read(fd int, p []byte) (int, error) {
	reader := f.readers[fd]
	if reader == nil {
		return 0, errors.New("reader unavailable")
	}
	return reader.Read(p)
}
func (f *fakeEvidenceFSOps) readDirectoryNames(fd int, maximum int) ([]string, error) {
	names := append([]string(nil), f.directoryNames[fd]...)
	if len(names) > maximum {
		return nil, errors.New("too many")
	}
	return names, nil
}
func (f *fakeEvidenceFSOps) renameNoReplace(parent int, oldName, newName string) error {
	if f.renameErr != nil {
		return f.renameErr
	}
	if f.files[parent][newName] || f.renameTargets[parent][newName] {
		return errors.New("exists")
	}
	if !f.files[parent][oldName] {
		return errors.New("missing")
	}
	delete(f.files[parent], oldName)
	f.files[parent][newName] = true
	f.renameTargets[parent][newName] = true
	return nil
}
func (f *fakeEvidenceFSOps) isNoReplaceConflict(err error) bool {
	return err != nil && err.Error() == "exists"
}
func (f *fakeEvidenceFSOps) linkAt(parent int, oldName, newName string) error {
	if f.linkErr != nil {
		return f.linkErr
	}
	if !f.files[parent][oldName] || f.files[parent][newName] {
		return errors.New("link")
	}
	f.files[parent][newName] = true
	return nil
}
func (f *fakeEvidenceFSOps) unlinkAt(parent int, name string) error {
	if f.unlinkErr != nil {
		return f.unlinkErr
	}
	if !f.files[parent][name] {
		return errors.New("missing")
	}
	delete(f.files[parent], name)
	delete(f.renameTargets[parent], name)
	return nil
}
func (f *fakeEvidenceFSOps) isNotExist(err error) bool { return err != nil && err.Error() == "missing" }
func (f *fakeEvidenceFSOps) tryLock(fd int) (bool, error) {
	f.lockAttempts = append(f.lockAttempts, fd)
	if f.lockErr != nil {
		return false, f.lockErr
	}
	if f.busy[fd] > 0 {
		f.busy[fd]--
		return false, nil
	}
	name := f.fdNames[fd]
	if f.locks[fd] || (name != "" && f.lockedNames[name]) {
		return false, nil
	}
	f.locks[fd] = true
	if name != "" {
		f.lockedNames[name] = true
	}
	return true, nil
}
func (f *fakeEvidenceFSOps) unlock(fd int) error {
	f.unlocks = append(f.unlocks, fd)
	if f.unlockErr != nil {
		return f.unlockErr
	}
	f.locks[fd] = false
	if name := f.fdNames[fd]; name != "" {
		delete(f.lockedNames, name)
	}
	return nil
}
func (f *fakeEvidenceFSOps) close(fd int) error { f.closed[fd] = true; return f.closeErr }
func (f *fakeEvidenceFSOps) probeStep(name string) {
	if f.probeCancelAt == name && f.probeCancel != nil {
		f.probeCancel()
	}
}

func TestEvidenceFSRootRejectsPathAndMetadataFaults(t *testing.T) {
	base := newFakeEvidenceFS()
	if _, err := newEvidenceFSRootWithOps(context.Background(), "relative", 501, base); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("relative root: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*fakeEvidenceFSOps)
	}{
		{"bad-fs", func(f *fakeEvidenceFSOps) { f.fsType = 1 }},
		{"wrong-owner", func(f *fakeEvidenceFSOps) { s := f.stats[1]; s.uid = 502; f.stats[1] = s }},
		{"bad-mode", func(f *fakeEvidenceFSOps) { s := f.stats[1]; s.mode = 0o770; f.stats[1] = s }},
		{"special-root", func(f *fakeEvidenceFSOps) { s := f.stats[1]; s.kind = evidenceFileUnknown; f.stats[1] = s }},
		{"stat-error", func(f *fakeEvidenceFSOps) { f.statErr = errors.New("secret path") }},
		{"fs-error", func(f *fakeEvidenceFSOps) { f.filesystemErr = errors.New("secret mount") }},
		{"sync-error", func(f *fakeEvidenceFSOps) { f.fdatasyncErr = errors.New("secret data") }},
		{"rename-error", func(f *fakeEvidenceFSOps) { f.renameErr = errors.New("enosys") }},
		{"link-error", func(f *fakeEvidenceFSOps) { f.linkErr = errors.New("link") }},
		{"cleanup-error", func(f *fakeEvidenceFSOps) { f.unlinkErr = errors.New("unlink") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeEvidenceFS()
			tt.mutate(f)
			_, err := newEvidenceFSRootWithOps(context.Background(), "/evidence", 501, f)
			if !IsCode(err, CodeEvidenceJournalFailed) {
				t.Fatalf("got %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("raw cause leaked: %v", err)
			}
		})
	}
}

func TestEvidenceFSRootProbeCleansFilesAndAcceptsSafeRoot(t *testing.T) {
	f := newFakeEvidenceFS()
	root, err := newEvidenceFSRootWithOps(context.Background(), "/evidence", 501, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.files[1]) != 0 {
		t.Fatalf("probe debris: %v", f.files[1])
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("double close: %v", err)
	}
}

func TestEvidenceFSProbeRejectsNoReplaceAndLockSemanticFaults(t *testing.T) {
	t.Run("no-replace-wrong-error", func(t *testing.T) {
		f := newFakeEvidenceFS()
		f.renameErr = errors.New("enosys")
		if _, err := newEvidenceFSRootWithOps(context.Background(), "/evidence", 501, f); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("flock-not-exclusive", func(t *testing.T) {
		f := newFakeEvidenceFS()
		// Make each descriptor look unrelated even though it opens the same name.
		f.disableNamedLockDomain = true
		if _, err := newEvidenceFSRootWithOps(context.Background(), "/evidence", 501, f); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestEvidenceFSProbeCancellationAfterMutationFailsJournalAndCleans(t *testing.T) {
	for _, step := range []string{"source-created", "source-initial-data-synced", "source-initial-dir-synced", "source-written", "source-synced", "linked", "linked-dir-synced", "link-unlinked", "link-unlinked-dir-synced", "published", "published-dir-synced", "competitor-created", "competitor-initial-data-synced", "competitor-initial-dir-synced", "competitor-ready", "conflict-checked", "lock-created", "lock-initial-data-synced", "lock-initial-dir-synced", "lock-acquired", "lock-contended", "lock-released", "locks-closed", "competitor-unlinked", "published-unlinked", "lock-unlinked", "root-synced"} {
		t.Run(step, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			f := newFakeEvidenceFS()
			f.probeCancelAt, f.probeCancel = step, cancel
			_, err := newEvidenceFSRootWithOps(ctx, "/evidence", 501, f)
			if !IsCode(err, CodeEvidenceJournalFailed) {
				t.Fatalf("got %v", err)
			}
			// Deferred best-effort cleanup must remove every name the fake saw.
			if len(f.files[1]) != 0 {
				t.Fatalf("probe debris: %v", f.files[1])
			}
		})
	}
}

func TestEvidenceFSComponentWalkRejectsEachMetadataFault(t *testing.T) {
	f := newFakeEvidenceFS()
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	f.children[1] = map[string]int{"lineages": 2}
	f.stats[2] = fakeDirectoryStat(7)
	fd, err := root.openDirectory("lineages")
	if err != nil || fd != 2 {
		t.Fatalf("safe walk: fd=%d err=%v", fd, err)
	}
	for _, path := range []string{"", "/lineages", "../lineages", "lineages/../x", "Lineages", "lineages\\x"} {
		if _, err := root.openDirectory(path); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("path %q: %v", path, err)
		}
	}
	for _, tt := range []struct {
		name   string
		mutate func(*evidenceFileStat)
	}{
		{"symlink-or-special", func(s *evidenceFileStat) { s.kind = evidenceFileUnknown }},
		{"wrong-owner", func(s *evidenceFileStat) { s.uid = 502 }},
		{"bad-mode", func(s *evidenceFileStat) { s.mode = 0o711 }},
		{"cross-mount", func(s *evidenceFileStat) { s.device = 8 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := newFakeEvidenceFS()
			g.children[1] = map[string]int{"lineages": 2}
			st := fakeDirectoryStat(7)
			tt.mutate(&st)
			g.stats[2] = st
			r := &evidenceFSRoot{ops: g, fd: 1, uid: 501, device: 7}
			if _, err := r.openDirectory("lineages"); !IsCode(err, CodeEvidenceJournalFailed) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestEvidenceFSRegularFileRejectsHardlinkSpecialOwnerModeAndDevice(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*evidenceFileStat)
	}{
		{"hardlink", func(s *evidenceFileStat) { s.nlink = 2 }}, {"special", func(s *evidenceFileStat) { s.kind = evidenceFileUnknown }}, {"owner", func(s *evidenceFileStat) { s.uid = 2 }}, {"mode", func(s *evidenceFileStat) { s.mode = 0o640 }}, {"device", func(s *evidenceFileStat) { s.device = 8 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := fakeRegularStat(7)
			tt.mutate(&st)
			if validateEvidenceRegularFile(st, 501, 7) == nil {
				t.Fatal("fault accepted")
			}
		})
	}
}

func TestEvidenceFSOpenRegularFileRejectsUnsafeOpenAndDurabilityFaults(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*fakeEvidenceFSOps)
	}{
		{"open", func(f *fakeEvidenceFSOps) { f.openFileErr = errors.New("secret path") }},
		{"stat", func(f *fakeEvidenceFSOps) { f.statErr = errors.New("secret inode") }},
		{"fdatasync", func(f *fakeEvidenceFSOps) { f.fdatasyncErr = errors.New("secret fd") }},
		{"directory-fsync", func(f *fakeEvidenceFSOps) { f.fsyncErr = errors.New("secret directory") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeEvidenceFS()
			tt.mutate(f)
			r := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
			_, _, err := r.openRegularFile(1, "writer.lock", true)
			if !IsCode(err, CodeEvidenceJournalFailed) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestEvidenceVerifiedReplaySourceSizeDriftAndReadFailure(t *testing.T) {
	source := newEvidenceVerifiedReplaySourceForTest(strings.NewReader("ab"), evidenceSegmentContainer, "segment", 0, 0, 1)
	if _, err := source.replayBoundary(evidenceSegmentContainer); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2)
	if _, err := source.Read(buffer); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("size drift: %v", err)
	}

	source = newEvidenceVerifiedReplaySourceForTest(errorReader{errors.New("secret read")}, evidenceSegmentContainer, "segment", 0, 0, 1)
	if _, err := source.replayBoundary(evidenceSegmentContainer); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Read(buffer[:1]); err == nil || IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("read error misclassified: %v", err)
	}
}

func TestEvidenceInventoryBuildsOrderedOpaqueDescriptors(t *testing.T) {
	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000001.caj", "segment-00000000.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true, "segment-00000001.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	f.nextFD = 100
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal-identity")
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.entries) != 2 || inventory.entries[0].ordinal != 0 || inventory.entries[1].ordinal != 1 {
		t.Fatalf("inventory=%+v", inventory.entries)
	}
	descriptor, err := inventory.segment(0)
	if err != nil {
		t.Fatal(err)
	}
	// Reopen returns a new fd; bind it to the same inventoried identity.
	entry := inventory.entries[descriptor.index]
	f.stats[f.nextFD] = evidenceFileStat{device: entry.device, inode: entry.inode, size: entry.observedSize, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
	source, err := root.openVerifiedReplaySource(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.replayBoundary(evidenceSegmentContainer); err != nil {
		t.Fatal(err)
	}
	if _, err := root.openVerifiedReplaySource(descriptor); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("descriptor reuse: %v", err)
	}
}

func TestEvidenceInventoryRejectsUnknownGapAndIdentityDrift(t *testing.T) {
	for _, names := range [][]string{{"unknown"}, {"segment-00000001.caj"}, {"segment-00000000.caj", "segment-00000002.caj"}} {
		f := newFakeEvidenceFS()
		f.directoryNames[2] = names
		f.files[2] = map[string]bool{}
		f.stats[2] = fakeDirectoryStat(7)
		for _, name := range names {
			f.files[2][name] = true
		}
		root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
		if _, err := root.inventoryEvidenceSegments(2, "journal"); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("names=%v err=%v", names, err)
		}
	}

	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000000.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, _ := inventory.segment(0)
	entry := inventory.entries[descriptor.index]
	f.stats[f.nextFD] = evidenceFileStat{device: 7, inode: entry.inode + 1, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
	if _, err := root.openVerifiedReplaySource(descriptor); !IsCode(err, CodeEvidenceJournalCorrupt) {
		t.Fatalf("identity drift: %v", err)
	}
}

func TestEvidenceInventoryRejectsDescriptorMutationSwapAndSelfFinalClaims(t *testing.T) {
	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000000.caj", "segment-00000001.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true, "segment-00000001.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, _ := inventory.segment(0)

	mutated := descriptor
	mutated.index = 1
	if _, err := root.openVerifiedReplaySource(mutated); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("index mutation: %v", err)
	}
	mutated = descriptor
	mutated.canonical[0] ^= 1
	if _, err := root.openVerifiedReplaySource(mutated); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("sentinel mutation: %v", err)
	}
	inventory.entries[0].ordinal = 1
	if _, err := root.openVerifiedReplaySource(descriptor); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("self-final/entry mutation: %v", err)
	}
}

func TestEvidenceInventoryConsumedStateCannotBeBypassedByDescriptorCopy(t *testing.T) {
	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000000.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, _ := inventory.segment(0)
	copied := descriptor
	entry := inventory.entries[0]
	f.stats[f.nextFD] = evidenceFileStat{device: entry.device, inode: entry.inode, size: entry.observedSize, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
	if _, err := root.openVerifiedReplaySource(descriptor); err != nil {
		t.Fatal(err)
	}
	if _, err := root.openVerifiedReplaySource(copied); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("copy bypass: %v", err)
	}
}

func TestEvidenceInventoryProductionChainOpensTypedReader(t *testing.T) {
	raw := fixtureArrayMembers(t, migrationFixturePath(t, "golden/evidence-record-chain-v1.json"), "frames")[0]
	framed := lengthPrefix(raw)
	f := newFakeEvidenceFS()
	f.directoryNames[2] = []string{"segment-00000000.caj"}
	f.files[2] = map[string]bool{"segment-00000000.caj": true}
	f.stats[2] = fakeDirectoryStat(7)
	f.nextFD = 100
	st := fakeRegularStat(7)
	st.size = uint64(len(framed))
	f.stats[100] = st
	root := &evidenceFSRoot{ops: f, fd: 1, uid: 501, device: 7}
	inventory, err := root.inventoryEvidenceSegments(2, "journal")
	if err != nil {
		t.Fatal(err)
	}
	entry := inventory.entries[0]
	f.stats[f.nextFD] = evidenceFileStat{device: entry.device, inode: entry.inode, size: entry.observedSize, mode: 0o600, uid: 501, nlink: 1, kind: evidenceFileRegular}
	f.readers[f.nextFD] = strings.NewReader(string(framed))
	reader, err := inventory.OpenEvidenceReplayReader(0, evidenceReplayTestProfile())
	if err != nil {
		t.Fatal(err)
	}
	if result := reader.Next(); result.State != FrameReplayValid || result.Frame == nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestEvidenceReplaySourceLiteralAndSentinelDriftRejected(t *testing.T) {
	source := newEvidenceVerifiedReplaySourceForTest(strings.NewReader(""), evidenceSegmentContainer, "journal", 0, 0, 0)
	for _, mutate := range []func(*evidenceVerifiedReplaySource){
		func(s *evidenceVerifiedReplaySource) { s.canonical[0] ^= 1 },
		func(s *evidenceVerifiedReplaySource) { s.inode++ },
	} {
		copySource := *source
		mutate(&copySource)
		if _, err := copySource.replayBoundary(evidenceSegmentContainer); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("literal drift accepted: %v", err)
		}
	}
	if _, err := (&evidenceVerifiedReplaySource{}).replayBoundary(evidenceSegmentContainer); !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("literal mint: %v", err)
	}
}

func TestEvidenceRootRejectsZeroDeviceAndInode(t *testing.T) {
	for _, mutate := range []func(*evidenceFileStat){func(st *evidenceFileStat) { st.device = 0 }, func(st *evidenceFileStat) { st.inode = 0 }} {
		f := newFakeEvidenceFS()
		st := f.stats[1]
		mutate(&st)
		f.stats[1] = st
		if _, err := newEvidenceFSRootWithOps(context.Background(), "/evidence", 501, f); !IsCode(err, CodeEvidenceJournalFailed) {
			t.Fatalf("got %v", err)
		}
	}
}
