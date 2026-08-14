package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"sync"
)

// GenerationSnapshot is a sealed, ordinary-byte snapshot of the exact target
// lineage index and ordered generation segments while GenerationLease retains
// both writer locks. It carries no C3 meaning, cursor, or append authority.
type GenerationSnapshot struct {
	self      *GenerationSnapshot
	seal      *struct{}
	lease     *GenerationLease
	target    [32]byte
	journal   [32]byte
	index     generationSnapshotFile
	segments  []generationSnapshotFile
	canonical [32]byte
	binding   *generationSnapshotBinding
}

type generationSnapshotFile struct {
	role     inventoryFileRole
	ordinal  uint32
	stat     fileStat
	digest   [32]byte
	identity [32]byte
	bytes    []byte
}

type generationSnapshotBinding struct {
	snapshot  *GenerationSnapshot
	lease     *GenerationLease
	target    [32]byte
	journal   [32]byte
	canonical [32]byte
}

type generationSnapshotRegistryRecord struct {
	snapshot  *GenerationSnapshot
	binding   *generationSnapshotBinding
	lease     *GenerationLease
	target    [32]byte
	journal   [32]byte
	index     generationSnapshotFile
	segments  []generationSnapshotFile
	canonical [32]byte
}

var generationSnapshotRegistry sync.Map

// GenerationFileFact is a non-authoritative copy of one snapshot file's
// bounded identity. It contains no path, descriptor, or writable handle.
type GenerationFileFact struct {
	Ordinal        uint32
	Size           uint64
	ContentDigest  [32]byte
	IdentityDigest [32]byte
}

func (l *GenerationLease) Snapshot(ctx context.Context) (*GenerationSnapshot, error) {
	if l == nil || l.self != l || l.seal == nil || l.mu == nil {
		return nil, ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !l.activeLocked() {
		return nil, ErrLeaseInvalid
	}
	index, segments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		if !isContextError(err) {
			l.valid = false
		}
		return nil, err
	}
	return l.mintGenerationSnapshotLocked(index, segments)
}

func (l *GenerationLease) mintGenerationSnapshotLocked(index generationSnapshotFile, segments []generationSnapshotFile) (*GenerationSnapshot, error) {
	if l == nil || !l.activeLocked() || !validGenerationSnapshotFile(index) || len(segments) == 0 {
		return nil, ErrLeaseInvalid
	}
	snapshot := &GenerationSnapshot{
		seal: &struct{}{}, lease: l, target: l.target, journal: l.journal,
		index: compactGenerationSnapshotFile(index), segments: compactGenerationSnapshotFiles(segments),
	}
	snapshot.self = snapshot
	snapshot.canonical = generationSnapshotDigest(snapshot)
	snapshot.binding = &generationSnapshotBinding{
		snapshot: snapshot, lease: l, target: l.target, journal: l.journal, canonical: snapshot.canonical,
	}
	invalidateGenerationSnapshotsLocked(l)
	l.snapshot = snapshot
	generationSnapshotRegistry.Store(snapshot, generationSnapshotRegistryRecord{
		snapshot: snapshot, binding: snapshot.binding, lease: l, target: l.target, journal: l.journal,
		index: snapshot.index, segments: append([]generationSnapshotFile(nil), snapshot.segments...), canonical: snapshot.canonical,
	})
	if !snapshot.validLocked() {
		generationSnapshotRegistry.Delete(snapshot)
		l.valid = false
		return nil, ErrLeaseInvalid
	}
	return snapshot, nil
}

func (s *GenerationSnapshot) IndexBytes() ([]byte, error) {
	return s.readFile(context.Background(), inventoryIndex, 0)
}

// ReadIndex reads the exact index bytes using caller cancellation.
func (s *GenerationSnapshot) ReadIndex(ctx context.Context) ([]byte, error) {
	return s.readFile(ctx, inventoryIndex, 0)
}

func (s *GenerationSnapshot) IndexFact() (GenerationFileFact, error) {
	var fact GenerationFileFact
	err := s.withValid(func() error {
		fact = generationFileFact(s.index)
		return nil
	})
	return fact, err
}

func (s *GenerationSnapshot) SegmentCount() (uint32, error) {
	var count uint32
	err := s.withValid(func() error {
		count = uint32(len(s.segments))
		return nil
	})
	return count, err
}

func (s *GenerationSnapshot) SegmentBytes(ordinal uint32) ([]byte, error) {
	return s.ReadSegment(context.Background(), ordinal)
}

// ReadSegment reads one exact segment using caller cancellation.
func (s *GenerationSnapshot) ReadSegment(ctx context.Context, ordinal uint32) ([]byte, error) {
	return s.readFile(ctx, inventorySegment, ordinal)
}

func (s *GenerationSnapshot) SegmentFact(ordinal uint32) (GenerationFileFact, error) {
	var fact GenerationFileFact
	err := s.withValid(func() error {
		if uint64(ordinal) >= uint64(len(s.segments)) || s.segments[ordinal].ordinal != ordinal {
			return ErrInvalidInput
		}
		fact = generationFileFact(s.segments[ordinal])
		return nil
	})
	return fact, err
}

func (s *GenerationSnapshot) IdentityDigest() ([32]byte, error) {
	var digest [32]byte
	err := s.withValid(func() error {
		digest = s.canonical
		return nil
	})
	return digest, err
}

// OwnsSnapshot reports whether the snapshot is the current sealed snapshot of
// this exact retained lease. It exposes no descriptor, path, or lock identity.
func (l *GenerationLease) OwnsSnapshot(snapshot *GenerationSnapshot) bool {
	if l == nil || l.self != l || l.seal == nil || l.mu == nil || snapshot == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshot == snapshot && snapshot.lease == l && snapshot.validLocked()
}

// Revalidate reopens and rehashes the exact index and segment set while the
// retained locks remain held. Non-context uncertainty revokes the lease.
func (s *GenerationSnapshot) Revalidate(ctx context.Context) error {
	if s == nil || s.lease == nil || s.lease.mu == nil {
		return ErrLeaseInvalid
	}
	l := s.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if !s.validLocked() {
		return ErrLeaseInvalid
	}
	index, segments, err := l.readGenerationSnapshotLocked(ctx)
	if err != nil {
		if !isContextError(err) {
			l.valid = false
		}
		return err
	}
	if !sameGenerationSnapshotFile(index, s.index) || !sameGenerationSnapshotFiles(segments, s.segments) {
		l.valid = false
		return corrupt("generation-snapshot-drift")
	}
	return nil
}

func (s *GenerationSnapshot) withValid(fn func() error) error {
	if s == nil || s.lease == nil || s.lease.mu == nil {
		return ErrLeaseInvalid
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	if !s.validLocked() {
		return ErrLeaseInvalid
	}
	return fn()
}

func (s *GenerationSnapshot) validLocked() bool {
	if s == nil || s.self != s || s.seal == nil || s.lease == nil || !s.lease.activeLocked() || s.binding == nil ||
		s.binding.snapshot != s || s.binding.lease != s.lease || s.binding.target != s.target || s.binding.journal != s.journal ||
		s.target != s.lease.target || s.journal != s.lease.journal || s.lease.snapshot != s || !validCompactGenerationSnapshotFile(s.index) || len(s.segments) == 0 ||
		s.canonical == ([32]byte{}) || s.binding.canonical != s.canonical || generationSnapshotDigest(s) != s.canonical {
		return false
	}
	for ordinal := range s.segments {
		if s.segments[ordinal].ordinal != uint32(ordinal) || !validCompactGenerationSnapshotFile(s.segments[ordinal]) {
			return false
		}
	}
	registered, ok := generationSnapshotRegistry.Load(s)
	record, recordOK := registered.(generationSnapshotRegistryRecord)
	return ok && recordOK && record.snapshot == s && record.binding == s.binding && record.lease == s.lease &&
		record.target == s.target && record.journal == s.journal && sameGenerationSnapshotFile(record.index, s.index) &&
		sameGenerationSnapshotFiles(record.segments, s.segments) && record.canonical == s.canonical
}

func (s *GenerationSnapshot) readFile(ctx context.Context, role inventoryFileRole, ordinal uint32) ([]byte, error) {
	if s == nil || s.lease == nil || s.lease.mu == nil {
		return nil, ErrLeaseInvalid
	}
	l := s.lease
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !s.validLocked() {
		return nil, ErrLeaseInvalid
	}
	var expected generationSnapshotFile
	switch role {
	case inventoryIndex:
		expected = s.index
	case inventorySegment:
		if uint64(ordinal) >= uint64(len(s.segments)) || s.segments[ordinal].ordinal != ordinal {
			return nil, ErrInvalidInput
		}
		expected = s.segments[ordinal]
	default:
		return nil, ErrInvalidInput
	}
	current, err := l.readOneGenerationSnapshotFileLocked(ctx, s, role, ordinal)
	if err != nil {
		if !isContextError(err) {
			l.valid = false
		}
		return nil, err
	}
	if !sameGenerationSnapshotFile(current, expected) {
		l.valid = false
		return nil, corrupt("generation-snapshot-file-drift")
	}
	return append([]byte(nil), current.bytes...), nil
}

func (l *GenerationLease) readGenerationSnapshotLocked(ctx context.Context) (generationSnapshotFile, []generationSnapshotFile, error) {
	if err := contextError(ctx); err != nil {
		return generationSnapshotFile{}, nil, err
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return generationSnapshotFile{}, nil, err
	}
	lineagesFD, lineageFD, journalFD := -1, -1, -1
	closeAll := func() error {
		failed := false
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-snapshot-cleanup")
		}
		return nil
	}
	fail := func(cause error) (generationSnapshotFile, []generationSnapshotFile, error) {
		if cleanupErr := closeAll(); cleanupErr != nil {
			cause = cleanupErr
		}
		return generationSnapshotFile{}, nil, cause
	}
	lineagesFD, _, err = l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return fail(err)
	}
	lineageName, journalName := hex.EncodeToString(l.target[:]), hex.EncodeToString(l.journal[:])
	lineageFD, _, err = l.store.openVerifiedDirectory(lineagesFD, lineageName)
	if err != nil {
		return fail(err)
	}
	lineageLock, err := l.store.statVerifiedRegular(lineageFD, "writer.lock")
	if err != nil || !sameIdentity(lineageLock, l.lineage.stat) {
		if err == nil {
			err = corrupt("generation-snapshot-lineage-lock")
		}
		return fail(err)
	}
	index, err := l.readGenerationFile(ctx, lineageFD, "index.caj", inventoryIndex, 0, maximumAdmissionIndexBytes)
	if err != nil {
		return fail(err)
	}
	index = compactGenerationSnapshotFile(index)
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, journalName)
	if err != nil {
		return fail(err)
	}
	journalLock, err := l.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, l.generation.stat) {
		if err == nil {
			err = corrupt("generation-snapshot-journal-lock")
		}
		return fail(err)
	}
	names, err := l.store.ops.readDirNames(journalFD, maximumAdmissionSegments+1)
	if err != nil {
		if l.store.ops.isOverflow(err) {
			err = limit("generation-segment-count")
		} else {
			err = filesystem("generation-segment-list")
		}
		return fail(err)
	}
	sort.Strings(names)
	segmentCapacity := len(names)
	if segmentCapacity > 0 {
		segmentCapacity--
	}
	segments := make([]generationSnapshotFile, 0, segmentCapacity)
	seenLock := false
	var journalBytes uint64
	for _, name := range names {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		if name == "writer.lock" {
			if seenLock {
				return fail(corrupt("generation-snapshot-duplicate-lock"))
			}
			seenLock = true
			continue
		}
		ordinal := uint32(len(segments))
		if len(segments) == maximumAdmissionSegments || name != admissionSegmentName(int(ordinal)) {
			return fail(corrupt("generation-snapshot-segment-order"))
		}
		segment, readErr := l.readGenerationFile(ctx, journalFD, name, inventorySegment, ordinal, maximumAdmissionSegmentBytes)
		if readErr != nil {
			return fail(readErr)
		}
		if segment.stat.size > maximumAdmissionJournalBytes-journalBytes {
			return fail(limit("generation-journal-bytes"))
		}
		journalBytes += segment.stat.size
		segments = append(segments, compactGenerationSnapshotFile(segment))
	}
	if !seenLock || len(segments) == 0 {
		return fail(corrupt("generation-snapshot-required-entry"))
	}
	if err := closeAll(); err != nil {
		return generationSnapshotFile{}, nil, err
	}
	return index, segments, nil
}

func (l *GenerationLease) readOneGenerationSnapshotFileLocked(ctx context.Context, snapshot *GenerationSnapshot, role inventoryFileRole, ordinal uint32) (generationSnapshotFile, error) {
	if err := contextError(ctx); err != nil {
		return generationSnapshotFile{}, err
	}
	rootFD, err := l.store.freshRoot()
	if err != nil {
		return generationSnapshotFile{}, err
	}
	lineagesFD, lineageFD, journalFD := -1, -1, -1
	closeAll := func() error {
		failed := false
		for _, fd := range []int{journalFD, lineageFD, lineagesFD, rootFD} {
			failed = l.store.checkedClose(fd) != nil || failed
		}
		if failed {
			return filesystem("generation-snapshot-read-cleanup")
		}
		return nil
	}
	fail := func(cause error) (generationSnapshotFile, error) {
		if cleanupErr := closeAll(); cleanupErr != nil {
			cause = cleanupErr
		}
		return generationSnapshotFile{}, cause
	}
	lineagesFD, _, err = l.store.openVerifiedDirectory(rootFD, "lineages")
	if err != nil {
		return fail(err)
	}
	lineageName, journalName := hex.EncodeToString(l.target[:]), hex.EncodeToString(l.journal[:])
	lineageFD, _, err = l.store.openVerifiedDirectory(lineagesFD, lineageName)
	if err != nil {
		return fail(err)
	}
	lineageLock, err := l.store.statVerifiedRegular(lineageFD, "writer.lock")
	if err != nil || !sameIdentity(lineageLock, l.lineage.stat) {
		if err == nil {
			err = corrupt("generation-snapshot-lineage-lock")
		}
		return fail(err)
	}
	if role == inventoryIndex {
		result, readErr := l.readGenerationFile(ctx, lineageFD, "index.caj", inventoryIndex, 0, maximumAdmissionIndexBytes)
		if readErr != nil {
			return fail(readErr)
		}
		if err := closeAll(); err != nil {
			return generationSnapshotFile{}, err
		}
		return result, nil
	}
	journalFD, _, err = l.store.openVerifiedDirectory(lineageFD, journalName)
	if err != nil {
		return fail(err)
	}
	journalLock, err := l.store.statVerifiedRegular(journalFD, "writer.lock")
	if err != nil || !sameIdentity(journalLock, l.generation.stat) {
		if err == nil {
			err = corrupt("generation-snapshot-journal-lock")
		}
		return fail(err)
	}
	names, err := l.store.ops.readDirNames(journalFD, maximumAdmissionSegments+1)
	if err != nil {
		if l.store.ops.isOverflow(err) {
			err = limit("generation-segment-count")
		} else {
			err = filesystem("generation-snapshot-segment-list")
		}
		return fail(err)
	}
	sort.Strings(names)
	if len(names) != len(snapshot.segments)+1 {
		return fail(corrupt("generation-snapshot-segment-set"))
	}
	seenLock, segmentIndex := false, 0
	for _, name := range names {
		if name == "writer.lock" {
			if seenLock {
				return fail(corrupt("generation-snapshot-segment-set"))
			}
			seenLock = true
			continue
		}
		if segmentIndex == len(snapshot.segments) || name != admissionSegmentName(segmentIndex) {
			return fail(corrupt("generation-snapshot-segment-set"))
		}
		segmentIndex++
	}
	if !seenLock || segmentIndex != len(snapshot.segments) {
		return fail(corrupt("generation-snapshot-segment-set"))
	}
	result, readErr := l.readGenerationFile(ctx, journalFD, admissionSegmentName(int(ordinal)), inventorySegment, ordinal, maximumAdmissionSegmentBytes)
	if readErr != nil {
		return fail(readErr)
	}
	if err := closeAll(); err != nil {
		return generationSnapshotFile{}, err
	}
	return result, nil
}

func (l *GenerationLease) readGenerationFile(ctx context.Context, parent int, name string, role inventoryFileRole, ordinal uint32, maximum uint64) (result generationSnapshotFile, resultErr error) {
	fd, before, err := l.store.openVerifiedRegular(parent, name)
	if err != nil {
		return generationSnapshotFile{}, err
	}
	defer func() {
		if closeErr := l.store.checkedClose(fd); closeErr != nil {
			result, resultErr = generationSnapshotFile{}, closeErr
		}
	}()
	if before.size == 0 {
		return generationSnapshotFile{}, corrupt("generation-file-empty")
	}
	if before.size > maximum || before.size > uint64(^uint(0)>>1) {
		return generationSnapshotFile{}, limit("generation-file-size")
	}
	if err := contextError(ctx); err != nil {
		return generationSnapshotFile{}, err
	}
	bytes := make([]byte, int(before.size))
	for offset := 0; offset < len(bytes); {
		if err := contextError(ctx); err != nil {
			return generationSnapshotFile{}, err
		}
		read, readErr := l.store.ops.pread(fd, bytes[offset:], int64(offset))
		if read <= 0 || read > len(bytes)-offset || readErr != nil && !errors.Is(readErr, io.EOF) {
			return generationSnapshotFile{}, corrupt("generation-file-read")
		}
		offset += read
		if errors.Is(readErr, io.EOF) && offset != len(bytes) {
			return generationSnapshotFile{}, corrupt("generation-file-short-read")
		}
	}
	after, err := l.store.ops.fstat(fd)
	if err != nil || !sameIdentity(before, after) {
		return generationSnapshotFile{}, corrupt("generation-file-identity")
	}
	digest := sha256.Sum256(bytes)
	return generationSnapshotFile{role: role, ordinal: ordinal, stat: after, digest: digest, identity: generationSnapshotFileIdentity(role, name, ordinal, after, digest), bytes: bytes}, nil
}

func generationSnapshotFileIdentity(role inventoryFileRole, name string, ordinal uint32, stat fileStat, digest [32]byte) [32]byte {
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidencefs-generation-snapshot-file/v1\x00"))
	h.Write([]byte{byte(role)})
	h.Write([]byte(name))
	var encoded [8]byte
	for _, value := range []uint64{uint64(ordinal), stat.device, stat.inode, stat.size, uint64(stat.mode), uint64(stat.uid), stat.nlink, uint64(stat.kind)} {
		binary.BigEndian.PutUint64(encoded[:], value)
		h.Write(encoded[:])
	}
	h.Write(digest[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func generationSnapshotDigest(snapshot *GenerationSnapshot) [32]byte {
	if snapshot == nil || snapshot.self != snapshot || snapshot.lease == nil || !validGenerationSnapshotFile(snapshot.index) || len(snapshot.segments) == 0 {
		return [32]byte{}
	}
	h := sha256.New()
	h.Write([]byte("cloud-agents-platform-evidencefs-generation-snapshot/v1\x00"))
	h.Write(snapshot.target[:])
	h.Write(snapshot.journal[:])
	h.Write(snapshot.index.identity[:])
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(len(snapshot.segments)))
	h.Write(encoded[:])
	for _, segment := range snapshot.segments {
		h.Write(segment.identity[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

func sameGenerationSnapshotFile(left, right generationSnapshotFile) bool {
	return left.role == right.role && left.ordinal == right.ordinal && sameIdentity(left.stat, right.stat) && left.digest == right.digest && left.identity == right.identity
}

func sameGenerationSnapshotFiles(left, right []generationSnapshotFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameGenerationSnapshotFile(left[index], right[index]) {
			return false
		}
	}
	return true
}

func compactGenerationSnapshotFile(file generationSnapshotFile) generationSnapshotFile {
	file.bytes = nil
	return file
}

func compactGenerationSnapshotFiles(files []generationSnapshotFile) []generationSnapshotFile {
	result := make([]generationSnapshotFile, len(files))
	for index := range files {
		result[index] = compactGenerationSnapshotFile(files[index])
	}
	return result
}

func validGenerationSnapshotFile(file generationSnapshotFile) bool {
	name := "index.caj"
	if file.role == inventorySegment {
		name = admissionSegmentName(int(file.ordinal))
	} else if file.role != inventoryIndex || file.ordinal != 0 {
		return false
	}
	return file.stat.size > 0 && file.digest != ([32]byte{}) && file.identity == generationSnapshotFileIdentity(file.role, name, file.ordinal, file.stat, file.digest)
}

func validCompactGenerationSnapshotFile(file generationSnapshotFile) bool {
	return len(file.bytes) == 0 && validGenerationSnapshotFile(file)
}

func generationFileFact(file generationSnapshotFile) GenerationFileFact {
	return GenerationFileFact{Ordinal: file.ordinal, Size: file.stat.size, ContentDigest: file.digest, IdentityDigest: file.identity}
}

func invalidateGenerationSnapshotsLocked(lease *GenerationLease) {
	if lease == nil {
		return
	}
	if lease.snapshot != nil {
		generationSnapshotRegistry.Delete(lease.snapshot)
		lease.snapshot = nil
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
