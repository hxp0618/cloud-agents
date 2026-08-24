package migration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	evidenceDirectoryMode = uint32(0o700)
	evidenceFileMode      = uint32(0o600)
)

var evidencePathComponentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// evidenceFSOps is deliberately private. Tests use a deterministic fake while
// production obtains the Linux implementation selected by the build tag. It
// keeps Linux syscall types and constants out of portable source files.
type evidenceFSOps interface {
	openRoot(string) (int, error)
	openDirAt(int, string) (int, error)
	openFileAt(int, string, bool) (int, bool, error)
	stat(int) (evidenceFileStat, error)
	filesystemType(int) (int64, error)
	verifyMount(int, int64, evidenceFileStat) error
	read(int, []byte) (int, error)
	write(int, []byte) (int, error)
	readDirectoryNames(int, int) ([]string, error)
	fdatasync(int) error
	fsync(int) error
	renameNoReplace(int, string, string) error
	isNoReplaceConflict(error) bool
	linkAt(int, string, string) error
	unlinkAt(int, string) error
	isNotExist(error) bool
	tryLock(int) (bool, error)
	unlock(int) error
	close(int) error
	probeStep(string)
}

type evidenceFileStat struct {
	device uint64
	inode  uint64
	size   uint64
	mode   uint32
	uid    uint32
	nlink  uint64
	kind   evidenceFileKind
}

type evidenceFileKind uint8

const (
	evidenceFileUnknown evidenceFileKind = iota
	evidenceFileDirectory
	evidenceFileRegular
)

type evidenceFSRoot struct {
	ops       evidenceFSOps
	fd        int
	uid       uint32
	device    uint64
	closeOnce oneShotState
}

const maximumEvidenceInventoryContainers = 4096

var evidenceSegmentFilenamePattern = regexp.MustCompile(`^segment-([0-9]{8})\.caj$`)

type verifiedEvidenceInventory struct {
	seal      *struct{}
	root      *evidenceFSRoot
	parentFD  int
	kind      frameContainerKind
	identity  string
	entries   []verifiedInventoryEntry
	canonical [32]byte
	consumed  map[uint32]bool
}

type verifiedContainerSegment struct {
	owner     *verifiedEvidenceInventory
	index     uint32
	canonical [32]byte
	entrySeal [32]byte
}

type verifiedInventoryEntry struct {
	name         string
	device       uint64
	inode        uint64
	observedSize uint64
	ordinal      uint32
}

// evidenceVerifiedReplaySource is filesystem-owned authority over one strict-
// inventoried open file. Its fields and construction path are private: frame
// replay can consume it, but a wire decoder or ordinary caller cannot mint the
// no-follow identity, final-container, or observed-size facts.
type evidenceVerifiedReplaySource struct {
	owner            *verifiedEvidenceInventory
	entryIndex       uint32
	canonical        [32]byte
	ops              evidenceFSOps
	fd               int
	device           uint64
	inode            uint64
	observedSize     uint64
	containerKind    frameContainerKind
	identity         string
	ordinal          uint32
	readBytes        uint64
	boundaryConsumed bool
	closed           bool
	reader           io.Reader
}

type oneShotState struct{ done bool }

func filesystemFailure(op, message string) error {
	// Raw paths and syscall errors are intentionally not retained in the stable
	// error. They may contain tenant names, host layout, or kernel details.
	return fail(CodeEvidenceJournalFailed, "evidence-fs-"+op, message, nil)
}

func newEvidenceFSRootWithOps(ctx context.Context, rootPath string, uid uint32, ops evidenceFSOps) (*evidenceFSRoot, error) {
	if err := evidenceContextError(ctx); err != nil {
		return nil, err
	}
	if ops == nil || rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return nil, filesystemFailure("root", "configured root is not an absolute canonical path")
	}
	fd, err := ops.openRoot(rootPath)
	if err != nil {
		return nil, filesystemFailure("root", "configured root cannot be opened safely")
	}
	root := &evidenceFSRoot{ops: ops, fd: fd, uid: uid}
	closeFailure := func(op, message string) error {
		if ops.close(fd) != nil {
			return filesystemFailure(op, message+"; root descriptor cleanup also failed")
		}
		return filesystemFailure(op, message)
	}
	st, err := ops.stat(fd)
	if err != nil || validateEvidenceRootDirectory(st, uid) != nil {
		return nil, closeFailure("root", "configured root metadata is not admissible")
	}
	root.device = st.device
	fsType, err := ops.filesystemType(fd)
	if err != nil || ops.verifyMount(fd, fsType, st) != nil {
		return nil, closeFailure("filesystem", "configured root filesystem is unsupported")
	}
	if err := root.probe(ctx); err != nil {
		if ops.close(fd) != nil {
			return nil, filesystemFailure("probe", "filesystem probe and root descriptor cleanup failed")
		}
		return nil, err
	}
	return root, nil
}

func (r *evidenceFSRoot) openDirectory(relative string) (int, error) {
	if r == nil || r.ops == nil || r.fd < 0 || r.closeOnce.done {
		return -1, filesystemFailure("open-directory", "filesystem root is closed")
	}
	parts, err := evidenceRelativeComponents(relative)
	if err != nil {
		return -1, err
	}
	current := r.fd
	owned := false
	for _, part := range parts {
		next, openErr := r.ops.openDirAt(current, part)
		if owned {
			if closeErr := r.ops.close(current); closeErr != nil {
				if next >= 0 {
					_ = r.ops.close(next)
				}
				return -1, filesystemFailure("open-directory", "intermediate directory close failed")
			}
		}
		if openErr != nil {
			return -1, filesystemFailure("open-directory", "directory component cannot be opened safely")
		}
		st, statErr := r.ops.stat(next)
		if statErr != nil || validateEvidenceDirectory(st, r.uid, r.device) != nil {
			_ = r.ops.close(next)
			return -1, filesystemFailure("open-directory", "directory component metadata is not admissible")
		}
		current, owned = next, true
	}
	if !owned {
		return -1, filesystemFailure("open-directory", "relative directory is empty")
	}
	return current, nil
}

func (r *evidenceFSRoot) inventoryEvidenceSegments(parent int, identity string) (*verifiedEvidenceInventory, error) {
	if r == nil || r.ops == nil || parent < 0 || identity == "" {
		return nil, filesystemFailure("inventory", "inventory root is not admissible")
	}
	names, err := r.ops.readDirectoryNames(parent, maximumEvidenceInventoryContainers+1)
	if err != nil || len(names) == 0 || len(names) > maximumEvidenceInventoryContainers {
		return nil, filesystemFailure("inventory", "bounded directory inventory failed")
	}
	sort.Strings(names)
	inventory := &verifiedEvidenceInventory{seal: &struct{}{}, root: r, parentFD: parent, kind: evidenceSegmentContainer, identity: identity, consumed: map[uint32]bool{}}
	for index, name := range names {
		matches := evidenceSegmentFilenamePattern.FindStringSubmatch(name)
		if len(matches) != 2 {
			return nil, filesystemFailure("inventory", "unknown journal directory entry")
		}
		ordinal, parseErr := strconv.ParseUint(matches[1], 10, 32)
		if parseErr != nil || ordinal != uint64(index) {
			return nil, filesystemFailure("inventory", "segment order is not contiguous")
		}
		fd, _, openErr := r.openRegularFile(parent, name, false)
		if openErr != nil {
			return nil, openErr
		}
		st, statErr := r.ops.stat(fd)
		closeErr := r.ops.close(fd)
		if statErr != nil || closeErr != nil || validateEvidenceRegularFile(st, r.uid, r.device) != nil || st.inode == 0 {
			return nil, filesystemFailure("inventory", "segment identity is not admissible")
		}
		inventory.entries = append(inventory.entries, verifiedInventoryEntry{name: name, device: st.device, inode: st.inode, observedSize: st.size, ordinal: uint32(ordinal)})
	}
	inventory.canonical = inventoryCanonicalDigest(inventory.kind, inventory.identity, inventory.entries)
	return inventory, nil
}

func (r *evidenceFSRoot) inventoryLineageIndex(parent int, identity string) (*verifiedEvidenceInventory, error) {
	if r == nil || r.ops == nil || parent < 0 || identity == "" {
		return nil, filesystemFailure("inventory", "inventory root is not admissible")
	}
	fd, _, err := r.openRegularFile(parent, "index.caj", false)
	if err != nil {
		return nil, err
	}
	st, statErr := r.ops.stat(fd)
	closeErr := r.ops.close(fd)
	if statErr != nil || closeErr != nil || validateEvidenceRegularFile(st, r.uid, r.device) != nil || st.inode == 0 {
		return nil, filesystemFailure("inventory", "lineage index identity is not admissible")
	}
	inventory := &verifiedEvidenceInventory{seal: &struct{}{}, root: r, parentFD: parent, kind: lineageIndexContainer, identity: identity, consumed: map[uint32]bool{}}
	inventory.entries = []verifiedInventoryEntry{{name: "index.caj", device: st.device, inode: st.inode, observedSize: st.size, ordinal: 0}}
	inventory.canonical = inventoryCanonicalDigest(inventory.kind, inventory.identity, inventory.entries)
	return inventory, nil
}

func inventoryCanonicalDigest(kind frameContainerKind, identity string, entries []verifiedInventoryEntry) [32]byte {
	hash := sha256.New()
	hash.Write([]byte("cloud-agents-platform-evidence-fs-inventory/v1\x00"))
	hash.Write([]byte{byte(kind)})
	hash.Write([]byte(identity))
	hash.Write([]byte{0})
	var encoded [8]byte
	for _, entry := range entries {
		hash.Write([]byte(entry.name))
		hash.Write([]byte{0})
		binary.BigEndian.PutUint64(encoded[:], entry.device)
		hash.Write(encoded[:])
		binary.BigEndian.PutUint64(encoded[:], entry.inode)
		hash.Write(encoded[:])
		binary.BigEndian.PutUint64(encoded[:], entry.observedSize)
		hash.Write(encoded[:])
		binary.BigEndian.PutUint32(encoded[:4], entry.ordinal)
		hash.Write(encoded[:4])
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func (inventory *verifiedEvidenceInventory) valid() bool {
	if inventory == nil || inventory.seal == nil || inventory.root == nil || inventory.parentFD < 0 || inventory.identity == "" || len(inventory.entries) == 0 || len(inventory.entries) > maximumEvidenceInventoryContainers || inventory.consumed == nil {
		return false
	}
	if inventory.kind == lineageIndexContainer && (len(inventory.entries) != 1 || inventory.entries[0].name != "index.caj") {
		return false
	}
	if inventory.kind != evidenceSegmentContainer && inventory.kind != lineageIndexContainer {
		return false
	}
	for index, entry := range inventory.entries {
		if entry.ordinal != uint32(index) || entry.device == 0 || entry.inode == 0 {
			return false
		}
	}
	return inventory.canonical == inventoryCanonicalDigest(inventory.kind, inventory.identity, inventory.entries)
}

func (inventory *verifiedEvidenceInventory) segment(ordinal uint32) (verifiedContainerSegment, error) {
	if !inventory.valid() || ordinal >= uint32(len(inventory.entries)) {
		return verifiedContainerSegment{}, filesystemFailure("inventory", "verified inventory descriptor is unavailable")
	}
	return verifiedContainerSegment{owner: inventory, index: ordinal, canonical: inventory.canonical, entrySeal: inventoryEntryDigest(inventory.canonical, ordinal)}, nil
}

func inventoryEntryDigest(canonical [32]byte, index uint32) [32]byte {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], index)
	return sha256.Sum256(append(append([]byte("cloud-agents-platform-evidence-fs-entry/v1\x00"), canonical[:]...), encoded[:]...))
}

func (r *evidenceFSRoot) openVerifiedReplaySource(descriptor verifiedContainerSegment) (*evidenceVerifiedReplaySource, error) {
	inventory := descriptor.owner
	if r == nil || !inventory.valid() || inventory.root != r || descriptor.canonical != inventory.canonical || descriptor.index >= uint32(len(inventory.entries)) || descriptor.entrySeal != inventoryEntryDigest(inventory.canonical, descriptor.index) || inventory.consumed[descriptor.index] {
		return nil, filesystemFailure("replay-source", "strict inventory descriptor is not admissible")
	}
	entry := inventory.entries[descriptor.index]
	fd, _, err := r.openRegularFile(inventory.parentFD, entry.name, false)
	if err != nil {
		return nil, err
	}
	st, statErr := r.ops.stat(fd)
	if statErr != nil || validateEvidenceRegularFile(st, r.uid, r.device) != nil || st.inode == 0 || st.device != entry.device || st.inode != entry.inode || st.size != entry.observedSize {
		_ = r.ops.close(fd)
		return nil, fail(CodeEvidenceJournalCorrupt, "evidence-fs-replay-source", "inventoried file identity changed", nil)
	}
	inventory.consumed[descriptor.index] = true
	return &evidenceVerifiedReplaySource{owner: inventory, entryIndex: descriptor.index, canonical: inventory.canonical, ops: r.ops, fd: fd, device: st.device, inode: st.inode, observedSize: st.size, containerKind: inventory.kind, identity: inventory.identity, ordinal: entry.ordinal}, nil
}

func (inventory *verifiedEvidenceInventory) OpenEvidenceReplayReader(entryIndex uint32, profile FrameReplayProfile) (*EvidenceFrameReplayReader, error) {
	if !inventory.valid() || inventory.kind != evidenceSegmentContainer {
		return nil, filesystemFailure("inventory-replay", "evidence inventory is not admissible")
	}
	descriptor, err := inventory.segment(entryIndex)
	if err != nil {
		return nil, err
	}
	source, err := inventory.root.openVerifiedReplaySource(descriptor)
	if err != nil {
		return nil, err
	}
	reader, err := newEvidenceFrameReplayReader(source, profile)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return reader, nil
}

func (inventory *verifiedEvidenceInventory) OpenLineageReplayReader(profile FrameReplayProfile) (*LineageFrameReplayReader, error) {
	if !inventory.valid() || inventory.kind != lineageIndexContainer {
		return nil, filesystemFailure("inventory-replay", "lineage inventory is not admissible")
	}
	descriptor, err := inventory.segment(0)
	if err != nil {
		return nil, err
	}
	source, err := inventory.root.openVerifiedReplaySource(descriptor)
	if err != nil {
		return nil, err
	}
	reader, err := newLineageFrameReplayReader(source, profile)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return reader, nil
}

func (s *evidenceVerifiedReplaySource) validAgainstInventory(expected frameContainerKind) bool {
	if s == nil || s.owner == nil || !s.owner.valid() || s.canonical != s.owner.canonical || s.entryIndex >= uint32(len(s.owner.entries)) || s.owner.kind != expected {
		return false
	}
	entry := s.owner.entries[s.entryIndex]
	return s.device == entry.device && s.inode == entry.inode && s.observedSize == entry.observedSize && s.containerKind == s.owner.kind && s.identity == s.owner.identity && s.ordinal == entry.ordinal
}

func (s *evidenceVerifiedReplaySource) Read(p []byte) (int, error) {
	if s == nil || s.closed || !s.validAgainstInventory(s.containerKind) || s.ops == nil || s.fd < 0 {
		if s == nil || s.closed || s.owner == nil || s.owner.seal == nil || s.reader == nil {
			return 0, filesystemFailure("replay-read", "verified replay source is closed")
		}
	}
	var n int
	var err error
	if s.reader != nil {
		n, err = s.reader.Read(p)
	} else {
		n, err = s.ops.read(s.fd, p)
	}
	if n < 0 || n > len(p) || s.readBytes > s.observedSize || uint64(n) > s.observedSize-s.readBytes {
		return 0, fail(CodeEvidenceJournalCorrupt, "evidence-fs-replay-read", "inventoried file size changed", nil)
	}
	s.readBytes += uint64(n)
	return n, err
}

func (s *evidenceVerifiedReplaySource) replayBoundary(expected frameContainerKind) (finalContainerBoundary, error) {
	if s == nil || s.closed || s.boundaryConsumed || !s.validAgainstInventory(expected) || s.readBytes != 0 {
		return finalContainerBoundary{}, filesystemFailure("replay-boundary", "verified replay source identity is not admissible")
	}
	s.boundaryConsumed = true
	return finalContainerBoundary{kind: s.containerKind, identity: s.identity, containerOrdinal: s.ordinal, replayStartOffset: 0, observedPhysicalBytes: s.observedSize}, nil
}

func (s *evidenceVerifiedReplaySource) Close() error {
	if s == nil || s.closed || ((s.ops == nil || s.fd < 0) && s.reader == nil) {
		return filesystemFailure("replay-close", "verified replay source is already closed")
	}
	s.closed = true
	if s.reader == nil && s.ops.close(s.fd) != nil {
		return filesystemFailure("replay-close", "verified replay source close failed")
	}
	s.fd = -1
	return nil
}

func (r *evidenceFSRoot) openRegularFile(parent int, name string, create bool) (int, bool, error) {
	if !validEvidencePathComponent(name) {
		return -1, false, filesystemFailure("open-file", "invalid file component")
	}
	fd, created, err := r.ops.openFileAt(parent, name, create)
	if err != nil {
		return -1, false, filesystemFailure("open-file", "regular file cannot be opened safely")
	}
	st, statErr := r.ops.stat(fd)
	if statErr != nil || validateEvidenceRegularFile(st, r.uid, r.device) != nil {
		_ = r.ops.close(fd)
		return -1, false, filesystemFailure("open-file", "regular file metadata is not admissible")
	}
	if created {
		if r.ops.fdatasync(fd) != nil || r.ops.fsync(parent) != nil {
			_ = r.ops.close(fd)
			return -1, false, filesystemFailure("create-file", "new file durability barrier failed")
		}
	}
	return fd, created, nil
}

func (r *evidenceFSRoot) probe(ctx context.Context) error {
	if err := evidenceContextError(ctx); err != nil {
		return err
	}
	mutated := false
	probeContext := func() error {
		if ctx != nil && ctx.Err() != nil {
			if mutated {
				return filesystemFailure("probe", "context ended after probe mutation")
			}
			return evidenceContextError(ctx)
		}
		return nil
	}
	step := func(name string) error {
		r.ops.probeStep(name)
		return probeContext()
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return filesystemFailure("probe", "probe identity is unavailable")
	}
	prefix := "caj-probe-" + hex.EncodeToString(nonce[:])
	source, linked, published, competitor := prefix+"-source", prefix+"-linked", prefix+"-published", prefix+"-competitor"
	lockName := prefix + "-lock"
	cleanupNames := []string{source, linked, published, competitor, lockName}
	cleanup := func(primary error) error {
		failed := false
		for _, name := range cleanupNames {
			if unlinkErr := r.ops.unlinkAt(r.fd, name); unlinkErr != nil {
				failed = !r.ops.isNotExist(unlinkErr) || failed
			}
		}
		failed = r.ops.fsync(r.fd) != nil || failed
		if failed {
			return filesystemFailure("probe", "probe failed and cleanup durability barrier failed")
		}
		return primary
	}
	failProbe := func(err error) error { return cleanup(err) }
	directoryBarrier := func(label string) error {
		if err := r.ops.fsync(r.fd); err != nil {
			return filesystemFailure("probe", "probe directory durability barrier failed")
		}
		return step(label)
	}
	createProbeFile := func(name, label string) (int, error) {
		fd, created, openErr := r.ops.openFileAt(r.fd, name, true)
		if openErr != nil || !created || fd < 0 {
			if fd >= 0 && r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "probe file create and descriptor cleanup failed")
			}
			return -1, filesystemFailure("probe", "probe file create failed")
		}
		mutated = true
		if err := step(label + "-created"); err != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "context ended and probe descriptor close failed")
			}
			return -1, err
		}
		st, statErr := r.ops.stat(fd)
		if statErr != nil || validateEvidenceRegularFile(st, r.uid, r.device) != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "probe metadata and descriptor close failed")
			}
			return -1, filesystemFailure("probe", "probe file metadata is not admissible")
		}
		if syncErr := r.ops.fdatasync(fd); syncErr != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "probe initial data barrier and descriptor close failed")
			}
			return -1, filesystemFailure("probe", "probe file initial data barrier failed")
		}
		if err := step(label + "-initial-data-synced"); err != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "context ended and probe descriptor close failed")
			}
			return -1, err
		}
		if syncErr := r.ops.fsync(r.fd); syncErr != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "probe directory barrier and descriptor close failed")
			}
			return -1, filesystemFailure("probe", "probe file initial directory barrier failed")
		}
		if err := step(label + "-initial-dir-synced"); err != nil {
			if r.ops.close(fd) != nil {
				return -1, filesystemFailure("probe", "context ended and probe descriptor close failed")
			}
			return -1, err
		}
		return fd, nil
	}

	sourceFD, err := createProbeFile(source, "source")
	if err != nil {
		return failProbe(filesystemFailure("probe", "file create or durability probe failed"))
	}
	if n, writeErr := r.ops.write(sourceFD, []byte{0x43, 0x41, 0x4a}); writeErr != nil || n != 3 {
		if r.ops.close(sourceFD) != nil {
			return failProbe(filesystemFailure("probe", "regular file write and descriptor close failed"))
		}
		return failProbe(filesystemFailure("probe", "regular file write probe failed"))
	}
	if err := step("source-written"); err != nil {
		if r.ops.close(sourceFD) != nil {
			return failProbe(filesystemFailure("probe", "context ended and probe descriptor close failed"))
		}
		return failProbe(err)
	}
	syncErr := r.ops.fdatasync(sourceFD)
	contextErr := step("source-synced")
	closeErr := r.ops.close(sourceFD)
	if syncErr != nil || contextErr != nil || closeErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "regular file sync probe failed"))
	}
	linkErr := r.ops.linkAt(r.fd, source, linked)
	contextErr = step("linked")
	if linkErr != nil || contextErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "link and unlink probe failed"))
	}
	if err := directoryBarrier("linked-dir-synced"); err != nil {
		return failProbe(err)
	}
	unlinkLinkedErr := r.ops.unlinkAt(r.fd, linked)
	contextErr = step("link-unlinked")
	if unlinkLinkedErr != nil || contextErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "link and unlink probe failed"))
	}
	if err := directoryBarrier("link-unlinked-dir-synced"); err != nil {
		return failProbe(err)
	}
	renameErr := r.ops.renameNoReplace(r.fd, source, published)
	contextErr = step("published")
	if renameErr != nil || contextErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "atomic no-replace publish probe failed"))
	}
	if err := directoryBarrier("published-dir-synced"); err != nil {
		return failProbe(err)
	}
	competitorFD, err := createProbeFile(competitor, "competitor")
	if err != nil {
		return failProbe(filesystemFailure("probe", "no-replace conflict probe setup failed"))
	}
	contextErr = step("competitor-ready")
	competitorCloseErr := r.ops.close(competitorFD)
	if contextErr != nil || competitorCloseErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "no-replace conflict probe setup failed"))
	}
	conflictErr := r.ops.renameNoReplace(r.fd, competitor, published)
	contextErr = step("conflict-checked")
	if contextErr != nil || conflictErr == nil || !r.ops.isNoReplaceConflict(conflictErr) {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "atomic no-replace publish semantics are unsupported"))
	}
	lockFD, err := createProbeFile(lockName, "lock")
	if err != nil {
		return failProbe(filesystemFailure("probe", "advisory lock probe setup failed"))
	}
	peerFD, _, err := r.openRegularFile(r.fd, lockName, false)
	if err != nil {
		if r.ops.close(lockFD) != nil {
			return failProbe(filesystemFailure("probe", "lock peer setup and descriptor close failed"))
		}
		return failProbe(filesystemFailure("probe", "advisory lock peer setup failed"))
	}
	locked, lockErr := r.ops.tryLock(lockFD)
	contextErr = step("lock-acquired")
	peerLocked, peerErr := r.ops.tryLock(peerFD)
	if secondContextErr := step("lock-contended"); contextErr == nil {
		contextErr = secondContextErr
	}
	unlockErr := r.ops.unlock(lockFD)
	unlockContextErr := step("lock-released")
	closeLockErr := r.ops.close(lockFD)
	closePeerErr := r.ops.close(peerFD)
	if finalContextErr := step("locks-closed"); contextErr == nil {
		contextErr = finalContextErr
	}
	if contextErr == nil {
		contextErr = unlockContextErr
	}
	if contextErr != nil || lockErr != nil || !locked || peerErr != nil || peerLocked || unlockErr != nil || closeLockErr != nil || closePeerErr != nil {
		if contextErr != nil {
			return failProbe(contextErr)
		}
		return failProbe(filesystemFailure("probe", "advisory lock semantics are unsupported"))
	}
	cleanupFailed := r.ops.unlinkAt(r.fd, competitor) != nil
	if err := step("competitor-unlinked"); err != nil {
		return failProbe(err)
	}
	cleanupFailed = r.ops.unlinkAt(r.fd, published) != nil || cleanupFailed
	if err := step("published-unlinked"); err != nil {
		return failProbe(err)
	}
	cleanupFailed = r.ops.unlinkAt(r.fd, lockName) != nil || cleanupFailed
	if err := step("lock-unlinked"); err != nil {
		return failProbe(err)
	}
	cleanupFailed = r.ops.fsync(r.fd) != nil || cleanupFailed
	if err := step("root-synced"); err != nil {
		return failProbe(err)
	}
	if cleanupFailed {
		return filesystemFailure("probe", "probe cleanup durability barrier failed")
	}
	return nil
}

func (r *evidenceFSRoot) Close() error {
	if r == nil || r.ops == nil || r.closeOnce.done {
		return filesystemFailure("close", "filesystem root handle is already closed")
	}
	r.closeOnce.done = true
	if r.ops.close(r.fd) != nil {
		return filesystemFailure("close", "filesystem root close failed")
	}
	r.fd = -1
	return nil
}

func validateEvidenceDirectory(st evidenceFileStat, uid uint32, device uint64) error {
	if st.kind != evidenceFileDirectory || st.uid != uid || st.mode&^evidenceDirectoryMode != 0 || st.mode&0o077 != 0 {
		return errors.New("inadmissible directory")
	}
	if st.device != device {
		return errors.New("cross-mount directory")
	}
	return nil
}

func validateEvidenceRootDirectory(st evidenceFileStat, uid uint32) error {
	if st.device == 0 || st.inode == 0 || st.kind != evidenceFileDirectory || st.uid != uid || st.mode&^evidenceDirectoryMode != 0 || st.mode&0o077 != 0 {
		return errors.New("inadmissible root directory")
	}
	return nil
}

func validateEvidenceRegularFile(st evidenceFileStat, uid uint32, device uint64) error {
	if st.kind != evidenceFileRegular || st.uid != uid || st.mode&^evidenceFileMode != 0 || st.mode&0o177 != 0 || st.nlink != 1 {
		return errors.New("inadmissible regular file")
	}
	if st.device != device {
		return errors.New("cross-mount file")
	}
	return nil
}

func evidenceRelativeComponents(relative string) ([]string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.Contains(relative, `\`) {
		return nil, filesystemFailure("path", "invalid relative path")
	}
	parts := strings.Split(relative, "/")
	for _, part := range parts {
		if !validEvidencePathComponent(part) {
			return nil, filesystemFailure("path", "invalid relative path component")
		}
	}
	return parts, nil
}

func validEvidencePathComponent(value string) bool {
	return value != "." && value != ".." && evidencePathComponentPattern.MatchString(value)
}

func evidenceContextError(ctx context.Context) error {
	if ctx == nil {
		return fail(CodeContextCanceled, "evidence-fs-context", "context is unavailable", nil)
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fail(CodeDeadlineExceeded, "evidence-fs-context", "deadline exceeded before filesystem mutation", nil)
		}
		return fail(CodeContextCanceled, "evidence-fs-context", "context canceled before filesystem mutation", nil)
	}
	return nil
}

func waitEvidenceRetry(ctx context.Context, attempt int) error {
	delay := time.Duration(1<<min(attempt, 6)) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return evidenceContextError(ctx)
	case <-timer.C:
		return nil
	}
}

var evidenceLockBackoff = waitEvidenceRetry
