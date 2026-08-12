package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"sort"
)

const (
	maximumFinalCount = uint64(64)
	maximumFinalBytes = uint64(4 << 30)
	maximumTempCount  = uint64(64)
	maximumTempBytes  = uint64(4 << 30)
	maximumObjectSize = uint64(64 << 20)
	maximumStoreNames = int(maximumFinalCount + maximumTempCount + 1)
)

var (
	finalNamePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	tempNamePattern  = regexp.MustCompile(`^\.tmp-[0-9a-f]{32}$`)
)

type scannedObject struct {
	name   string
	stat   fileStat
	digest [32]byte
	temp   bool
}

// Scan is a sealed, lease-generation-bound snapshot of objects/sha256.
type Scan struct {
	self       *Scan
	seal       *struct{}
	lease      *Lease
	generation uint64
	objects    map[[32]byte]scannedObject
	finalCount uint64
	finalBytes uint64
	tempCount  uint64
	tempBytes  uint64
}

// ObjectFact and Usage are non-authoritative copies for quota calculation.
// Mutating them cannot alter the sealed Scan used by Publish.
type ObjectFact struct {
	Digest [32]byte
	Size   uint64
}

type Usage struct {
	FinalObjects   []ObjectFact
	FinalBytes     uint64
	TemporaryCount uint64
	TemporaryBytes uint64
}

func (s *Scan) Usage() Usage {
	if s == nil || s.lease == nil {
		return Usage{}
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	if !s.validLocked() {
		return Usage{}
	}
	facts := make([]ObjectFact, 0, len(s.objects))
	for digest, object := range s.objects {
		facts = append(facts, ObjectFact{Digest: digest, Size: object.stat.size})
	}
	sort.Slice(facts, func(i, j int) bool { return string(facts[i].Digest[:]) < string(facts[j].Digest[:]) })
	return Usage{FinalObjects: facts, FinalBytes: s.finalBytes, TemporaryCount: s.tempCount, TemporaryBytes: s.tempBytes}
}

func (s *Scan) FinalUsage() (count, bytes uint64) {
	if s == nil || s.lease == nil {
		return 0, 0
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	if !s.validLocked() {
		return 0, 0
	}
	return s.finalCount, s.finalBytes
}

func (s *Scan) TemporaryUsage() (count, bytes uint64) {
	if s == nil || s.lease == nil {
		return 0, 0
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	if !s.validLocked() {
		return 0, 0
	}
	return s.tempCount, s.tempBytes
}

func (s *Scan) HasObject(digest [32]byte, size uint64) bool {
	if s == nil || s.lease == nil {
		return false
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	if !s.validLocked() {
		return false
	}
	object, ok := s.objects[digest]
	return ok && object.stat.size == size
}

func (s *Scan) valid() bool {
	if s == nil || s.lease == nil {
		return false
	}
	s.lease.mu.Lock()
	defer s.lease.mu.Unlock()
	return s.validLocked()
}

func (s *Scan) validLocked() bool {
	return s != nil && s.self == s && s.seal != nil && s.lease != nil && s.generation != 0 && s.generation == s.lease.generation && s.lease.active()
}

func (l *Lease) Scan(ctx context.Context) (result *Scan, resultErr error) {
	if l == nil {
		return nil, ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !l.active() {
		return nil, ErrLeaseInvalid
	}
	grammar, err := l.root.verifyFreshRootGrammar()
	if err != nil || !sameIdentity(grammar.lock, l.lock) {
		return nil, filesystem("scan-root")
	}
	rootFD, err := l.root.freshRoot()
	if err != nil {
		return nil, err
	}
	defer l.closeScanFD(&rootFD, &result, &resultErr, "scan-root-close")()
	objectsFD, _, err := l.root.openVerifiedDirectory(rootFD, "objects")
	if err != nil {
		return nil, err
	}
	defer l.closeScanFD(&objectsFD, &result, &resultErr, "objects-close")()
	names, err := l.root.ops.readDirNames(objectsFD, 2)
	if err != nil || len(names) != 1 || names[0] != "sha256" {
		return nil, filesystem("objects-grammar")
	}
	shaFD, _, err := l.root.openVerifiedDirectory(objectsFD, "sha256")
	if err != nil {
		return nil, err
	}
	defer l.closeScanFD(&shaFD, &result, &resultErr, "sha256-close")()
	return l.scanSHA256(ctx, shaFD)
}

func (l *Lease) closeScanFD(fd *int, result **Scan, resultErr *error, operation string) func() {
	return func() {
		if fd == nil || *fd < 0 {
			return
		}
		current := *fd
		*fd = -1
		if l.root.ops.close(current) != nil {
			l.root.poison()
			l.invalidate()
			*result = nil
			*resultErr = filesystem(operation)
		}
	}
}

func (l *Lease) scanSHA256(ctx context.Context, shaFD int) (*Scan, error) {
	names, err := l.root.ops.readDirNames(shaFD, maximumStoreNames)
	if err != nil {
		if l.root.ops.isOverflow(err) {
			return nil, limit("object-count")
		}
		return nil, filesystem("object-list")
	}
	sort.Strings(names)
	entries := make([]struct {
		object scannedObject
		fd     int
	}, 0, len(names))
	closeEntries := func() error {
		failed := false
		for index := range entries {
			if entries[index].fd < 0 {
				continue
			}
			fd := entries[index].fd
			entries[index].fd = -1
			failed = l.root.ops.close(fd) != nil || failed
		}
		if failed {
			l.root.poison()
			l.invalidate()
			return filesystem("object-close")
		}
		return nil
	}
	fail := func(err error) (*Scan, error) {
		if closeErr := closeEntries(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	scan := &Scan{seal: &struct{}{}, lease: l, generation: l.generation, objects: map[[32]byte]scannedObject{}}
	scan.self = scan
	for _, name := range names {
		if err := contextError(ctx); err != nil {
			return fail(err)
		}
		isFinal, isTemp := finalNamePattern.MatchString(name), tempNamePattern.MatchString(name)
		if !isFinal && !isTemp {
			return fail(filesystem("object-name"))
		}
		fd, st, err := l.root.openVerifiedRegular(shaFD, name)
		if err != nil {
			return fail(err)
		}
		entries = append(entries, struct {
			object scannedObject
			fd     int
		}{object: scannedObject{name: name, stat: st, temp: isTemp}, fd: fd})
		if st.size > maximumObjectSize {
			return fail(limit("object-size"))
		}
		if isFinal {
			if scan.finalCount == maximumFinalCount || st.size > maximumFinalBytes-scan.finalBytes {
				return fail(limit("final-usage"))
			}
			scan.finalCount++
			scan.finalBytes += st.size
		} else {
			if scan.tempCount == maximumTempCount || st.size > maximumTempBytes-scan.tempBytes {
				return fail(limit("temporary-usage"))
			}
			scan.tempCount++
			scan.tempBytes += st.size
		}
	}
	// All count and physical-size bounds are established before any bytes are
	// read. Every admitted final and temporary is then hashed in full.
	for index := range entries {
		entry := &entries[index]
		digest, err := l.fullHash(ctx, entry.fd, entry.object.stat)
		if err != nil {
			return fail(err)
		}
		entry.object.digest = digest
		if !entry.object.temp {
			expected, decodeErr := hex.DecodeString(entry.object.name)
			if decodeErr != nil || !equalDigestBytes(digest, expected) {
				return fail(corrupt("final-digest"))
			}
			if _, duplicate := scan.objects[digest]; duplicate {
				return fail(corrupt("duplicate-final"))
			}
			scan.objects[digest] = entry.object
		}
	}
	if err := closeEntries(); err != nil {
		return nil, err
	}
	return scan, nil
}

func (l *Lease) fullHash(ctx context.Context, fd int, expected fileStat) ([32]byte, error) {
	h := sha256.New()
	buffer := make([]byte, 128<<10)
	var offset uint64
	for offset < expected.size {
		if err := contextError(ctx); err != nil {
			return [32]byte{}, err
		}
		want := uint64(len(buffer))
		if remaining := expected.size - offset; remaining < want {
			want = remaining
		}
		n, err := l.root.ops.pread(fd, buffer[:want], int64(offset))
		if n > 0 {
			_, _ = h.Write(buffer[:n])
			offset += uint64(n)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return [32]byte{}, filesystem("object-read")
		}
		if n == 0 {
			return [32]byte{}, corrupt("object-short-read")
		}
	}
	after, err := l.root.ops.fstat(fd)
	if err != nil || !sameIdentity(expected, after) {
		return [32]byte{}, filesystem("object-mutated")
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}

// Publication is durable object authority minted only by Publish. It is bound
// to the root identity and the exact final file identity verified after fsync.
type Publication struct {
	self       *Publication
	seal       *struct{}
	root       *Root
	lease      *Lease
	digest     [32]byte
	size       uint64
	identity   *Identity
	generation uint64
	bound      bool
	consumed   bool
}

// Identity is the opaque root/file identity behind a Publication. It exposes
// no path, descriptor, backend, or construction seam.
type Identity struct {
	self   *Identity
	seal   *struct{}
	root   fileStat
	object fileStat
}

type verifiedPublication struct {
	root     *Root
	digest   [32]byte
	size     uint64
	identity *Identity
}

func (p *Publication) Identity() *Identity {
	if !p.boundValid() {
		return nil
	}
	return p.identity
}

func (p *Publication) Matches(digest [32]byte, size uint64) bool {
	return p.boundValid() && p.digest == digest && p.size == size
}

func (p *Publication) transientValid() bool {
	return p != nil && p.self == p && p.seal != nil && p.root != nil && p.lease != nil && p.identity != nil && p.identity.self == p.identity && p.identity.seal != nil && sameIdentity(p.identity.root, p.root.identity) && validRegular(p.identity.object, p.root.uid, p.root.identity.device) && p.size == p.identity.object.size && !p.bound && !p.consumed
}

func (p *Publication) boundValid() bool {
	return p != nil && p.self == p && p.seal != nil && p.root != nil && p.lease == nil && p.identity != nil && p.identity.self == p.identity && p.identity.seal != nil && sameIdentity(p.identity.root, p.root.identity) && validRegular(p.identity.object, p.root.uid, p.root.identity.device) && p.size == p.identity.object.size && p.bound && p.consumed
}

// BindPublication consumes the transient publication event under the same
// active root lease and generation. A bound Publication is immutable durable
// content authority and remains valid after the lease is released.
func (l *Lease) BindPublication(publication *Publication, digest [32]byte, size uint64) error {
	if l == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.active() || !publication.transientValid() || publication.lease != l || publication.root != l.root || publication.generation != l.generation || publication.digest != digest || publication.size != size {
		if publication != nil && publication.self == publication && publication.lease == l {
			publication.consumed = true
		}
		return ErrLeaseInvalid
	}
	publication.bound = true
	publication.consumed = true
	publication.lease = nil
	return nil
}

func (l *Lease) Publish(ctx context.Context, scan *Scan, digest [32]byte, source []byte) (result *Publication, resultErr error) {
	if l == nil {
		return nil, ErrLeaseInvalid
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(source) == 0 || uint64(len(source)) > maximumObjectSize {
		return nil, ErrInvalidInput
	}
	owned := append([]byte(nil), source...)
	if sha256.Sum256(owned) != digest {
		return nil, ErrInvalidInput
	}
	size := uint64(len(owned))
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if !l.active() || scan == nil || !scan.validLocked() || scan.lease != l || scan.generation != l.generation {
		return nil, ErrLeaseInvalid
	}
	finalName := hex.EncodeToString(digest[:])
	mutationStarted := false
	rootFD, err := l.root.freshRoot()
	if err != nil {
		return nil, err
	}
	objectsFD, _, err := l.root.openVerifiedDirectory(rootFD, "objects")
	if err != nil {
		_ = l.root.ops.close(rootFD)
		return nil, err
	}
	if l.root.ops.close(rootFD) != nil {
		_ = l.root.ops.close(objectsFD)
		return nil, filesystem("publication-root-close")
	}
	defer func() {
		if l.root.ops.close(objectsFD) != nil {
			if mutationStarted {
				l.invalidate()
				result, resultErr = nil, unknown(filesystem("objects-close"))
			} else {
				result, resultErr = nil, filesystem("objects-close")
			}
		}
	}()
	shaFD, _, err := l.root.openVerifiedDirectory(objectsFD, "sha256")
	if err != nil {
		return nil, err
	}
	defer func() {
		if l.root.ops.close(shaFD) != nil {
			if mutationStarted {
				l.invalidate()
				result, resultErr = nil, unknown(filesystem("sha256-close"))
			} else {
				result, resultErr = nil, filesystem("sha256-close")
			}
		}
	}()
	if existing, ok := scan.objects[digest]; ok {
		if existing.stat.size != size {
			return nil, corrupt("existing-size")
		}
		mutationStarted = true
		verified, verifyErr := l.verifyPublication(ctx, shaFD, finalName, digest, size)
		if verifyErr != nil {
			l.invalidate()
			return nil, unknown(verifyErr)
		}
		if l.root.ops.fsync(shaFD) != nil {
			l.invalidate()
			return nil, unknown(filesystem("existing-directory-fsync"))
		}
		l.generation++
		return l.mintPublication(verified), nil
	}
	if scan.finalCount == maximumFinalCount || size > maximumFinalBytes-scan.finalBytes || scan.tempCount == maximumTempCount || size > maximumTempBytes-scan.tempBytes {
		return nil, limit("publication-usage")
	}
	nonce := make([]byte, 16)
	if err := l.root.ops.random(nonce); err != nil {
		return nil, filesystem("nonce")
	}
	tempName := ".tmp-" + hex.EncodeToString(nonce)
	tempFD, err := l.root.ops.openFileAt(shaFD, tempName, true)
	if err != nil {
		return nil, filesystem("temporary-create")
	}
	mutationStarted = true
	mutated := true
	uncertain := func(err error) (*Publication, error) {
		if mutated {
			l.invalidate()
		}
		if tempFD >= 0 && l.root.ops.close(tempFD) != nil {
			err = filesystem("temporary-close")
		}
		return nil, unknown(err)
	}
	st, err := l.root.ops.fstat(tempFD)
	if err != nil || !validRegular(st, l.root.uid, l.root.identity.device) || st.size != 0 {
		return uncertain(filesystem("temporary-identity"))
	}
	if err := writeAll(ctx, l.root.ops, tempFD, owned); err != nil {
		return uncertain(err)
	}
	st, err = l.root.ops.fstat(tempFD)
	if err != nil || !validRegular(st, l.root.uid, l.root.identity.device) || st.size != size {
		return uncertain(filesystem("temporary-size"))
	}
	writtenDigest, err := l.fullHash(ctx, tempFD, st)
	if err != nil || writtenDigest != digest {
		if err != nil {
			return uncertain(err)
		}
		return uncertain(corrupt("temporary-digest"))
	}
	if l.root.ops.fdatasync(tempFD) != nil {
		return uncertain(filesystem("temporary-fdatasync"))
	}
	if l.root.ops.close(tempFD) != nil {
		tempFD = -1
		return uncertain(filesystem("temporary-close"))
	}
	tempFD = -1
	renameErr := l.root.ops.renameNoReplace(shaFD, tempName, finalName)
	if renameErr != nil {
		if !l.root.ops.isExist(renameErr) {
			return uncertain(filesystem("publication-rename"))
		}
		verified, verifyErr := l.verifyPublication(ctx, shaFD, finalName, digest, size)
		if verifyErr != nil {
			return uncertain(verifyErr)
		}
		cleanupFailed := l.root.ops.unlinkAt(shaFD, tempName) != nil
		cleanupFailed = l.root.ops.fsync(shaFD) != nil || cleanupFailed
		if cleanupFailed {
			return uncertain(filesystem("publication-conflict-cleanup"))
		}
		mutated = false
		l.generation++
		return l.mintPublication(verified), nil
	}
	verified, err := l.verifyPublication(ctx, shaFD, finalName, digest, size)
	if err != nil {
		return uncertain(err)
	}
	if l.root.ops.fsync(shaFD) != nil {
		return uncertain(filesystem("publication-directory-fsync"))
	}
	mutated = false
	l.generation++
	return l.mintPublication(verified), nil
}

func (l *Lease) verifyPublication(ctx context.Context, shaFD int, name string, digest [32]byte, size uint64) (result verifiedPublication, resultErr error) {
	fd, st, err := l.root.openVerifiedRegular(shaFD, name)
	if err != nil {
		return verifiedPublication{}, err
	}
	defer func() {
		if l.root.ops.close(fd) != nil {
			result, resultErr = verifiedPublication{}, filesystem("published-close")
		}
	}()
	if st.size != size {
		return verifiedPublication{}, corrupt("published-size")
	}
	observed, err := l.fullHash(ctx, fd, st)
	if err != nil {
		return verifiedPublication{}, err
	}
	if observed != digest {
		return verifiedPublication{}, corrupt("published-digest")
	}
	if l.root.ops.fdatasync(fd) != nil {
		return verifiedPublication{}, filesystem("published-fdatasync")
	}
	identity := &Identity{seal: &struct{}{}, root: l.root.identity, object: st}
	identity.self = identity
	return verifiedPublication{root: l.root, digest: digest, size: size, identity: identity}, nil
}

func (l *Lease) mintPublication(verified verifiedPublication) *Publication {
	publication := &Publication{seal: &struct{}{}, root: verified.root, lease: l, digest: verified.digest, size: verified.size, identity: verified.identity, generation: l.generation}
	publication.self = publication
	return publication
}

func writeAll(ctx context.Context, ops backend, fd int, source []byte) error {
	for offset := 0; offset < len(source); {
		if err := contextError(ctx); err != nil {
			return err
		}
		count, err := ops.write(fd, source[offset:])
		if err != nil || count <= 0 || count > len(source)-offset {
			return filesystem("temporary-write")
		}
		offset += count
	}
	return nil
}

func equalDigestBytes(digest [32]byte, raw []byte) bool {
	if len(raw) != len(digest) {
		return false
	}
	var decoded [32]byte
	copy(decoded[:], raw)
	return decoded == digest
}
