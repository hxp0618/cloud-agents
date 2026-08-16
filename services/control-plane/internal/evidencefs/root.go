package evidencefs

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

const (
	directoryMode = uint32(0o700)
	fileMode      = uint32(0o600)
	lockRetries   = 64
)

type fileKind uint8

const (
	kindUnknown fileKind = iota
	kindDirectory
	kindRegular
)

type fileStat struct {
	device uint64
	inode  uint64
	size   uint64
	mode   uint32
	uid    uint32
	nlink  uint64
	kind   fileKind
}

// backend is deliberately package-private. There is no public backend or
// callback constructor: sibling packages cannot mint filesystem authority.
// The deterministic implementation used by tests lives only in _test.go.
type backend interface {
	openRoot(string) (int, error)
	openDirAt(int, string) (int, error)
	mkdirAt(int, string) error
	lstatAt(int, string) (fileStat, error)
	openFileAt(int, string, bool) (int, error)
	openFileAtReadWrite(int, string) (int, error)
	fstat(int) (fileStat, error)
	readDirNames(int, int) ([]string, error)
	isOverflow(error) bool
	isNotExist(error) bool
	pread(int, []byte, int64) (int, error)
	pwrite(int, []byte, int64) (int, error)
	truncate(int, int64) error
	write(int, []byte) (int, error)
	fdatasync(int) error
	fsync(int) error
	renameNoReplace(int, string, string) error
	unlinkAt(int, string) error
	isExist(error) bool
	tryLock(int) (bool, error)
	unlock(int) error
	close(int) error
	random([]byte) error
}

type mountAuthority struct{ seal *struct{} }

// Root owns the admitted root identity. Its fields are intentionally opaque.
type Root struct {
	self     *Root
	seal     *struct{}
	mu       sync.Mutex
	poisoned bool
	ops      backend
	path     string
	uid      uint32
	identity fileStat
}

// Store and RootLease name the object-store and root-wide lease APIs while the
// aliases preserve the short internal names used throughout this slice.
type Store = Root
type RootLease = Lease

// Open is the sole production constructor. Admission remains fail closed
// because no trusted provisioning component can mint mountAuthority yet.
func Open(ctx context.Context, rootPath string) (*Root, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	_ = rootPath
	return nil, ErrTrustedMountAuthority
}

func OpenStore(ctx context.Context, rootPath string) (*Store, error) { return Open(ctx, rootPath) }

func newRootWithAuthority(ctx context.Context, rootPath string, uid uint32, ops backend, authority mountAuthority) (*Root, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if ops == nil || authority.seal == nil || rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		return nil, ErrInvalidInput
	}
	fd, err := ops.openRoot(rootPath)
	if err != nil {
		return nil, filesystem("root-open")
	}
	st, statErr := ops.fstat(fd)
	closeErr := ops.close(fd)
	if statErr != nil || closeErr != nil || !validDirectory(st, uid, st.device) {
		return nil, filesystem("root-identity")
	}
	root := &Root{seal: &struct{}{}, ops: ops, path: rootPath, uid: uid, identity: st}
	root.self = root
	return root, nil
}

// newRootWithRequiredProbe remains package-private and is not a production
// authority constructor. A future trusted provisioner may call it only after
// minting mountAuthority from an external non-forgeable mount capability.
func newRootWithRequiredProbe(ctx context.Context, rootPath string, uid uint32, ops backend, authority mountAuthority) (*Root, error) {
	root, err := newRootWithAuthority(ctx, rootPath, uid, ops, authority)
	if err != nil {
		return nil, err
	}
	if err := root.probeRequiredSyscalls(ctx); err != nil {
		return nil, err
	}
	return root, nil
}

func (r *Root) freshRoot() (int, error) {
	if !r.usable() {
		return -1, filesystem("root-unavailable")
	}
	fd, err := r.ops.openRoot(r.path)
	if err != nil {
		return -1, filesystem("root-reopen")
	}
	st, err := r.ops.fstat(fd)
	if err != nil || !sameNodeIdentity(st, r.identity) || !validDirectory(st, r.uid, r.identity.device) {
		if r.ops.close(fd) != nil {
			r.poison()
		}
		return -1, filesystem("root-replaced")
	}
	return fd, nil
}

func (r *Root) usable() bool {
	if r == nil || r.self != r || r.seal == nil || r.ops == nil || r.identity.inode == 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.poisoned
}

func (r *Root) poison() {
	if r != nil && r.self == r {
		r.mu.Lock()
		r.poisoned = true
		r.mu.Unlock()
	}
}

// Lease owns the root-wide lineages.lock flock. A mutation whose durable
// outcome cannot be proven poisons the lease and every Scan minted by it.
type Lease struct {
	self       *Lease
	seal       *struct{}
	mu         sync.Mutex
	root       *Root
	rootFD     int
	lockFD     int
	lock       fileStat
	generation uint64
	valid      bool
	closed     bool
}

func (r *Root) Acquire(ctx context.Context) (*Lease, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	rootFD, err := r.freshRoot()
	if err != nil {
		return nil, err
	}
	fail := func() (*Lease, error) {
		if r.ops.close(rootFD) != nil {
			r.poison()
		}
		return nil, filesystem("lease-admission")
	}
	grammar, err := r.verifyFreshRootGrammar()
	if err != nil {
		if r.ops.close(rootFD) != nil {
			r.poison()
		}
		return nil, err
	}
	lockFD, lockStat, err := r.openVerifiedRegular(rootFD, "lineages.lock")
	if err != nil || !sameIdentity(lockStat, grammar.lock) {
		if lockFD >= 0 {
			if r.ops.close(lockFD) != nil {
				r.poison()
			}
		}
		return fail()
	}
	locked := false
	for attempt := 0; attempt < lockRetries; attempt++ {
		if err := contextError(ctx); err != nil {
			failed := r.ops.close(lockFD) != nil
			failed = r.ops.close(rootFD) != nil || failed
			if failed {
				r.poison()
			}
			return nil, err
		}
		ok, lockErr := r.ops.tryLock(lockFD)
		if lockErr != nil {
			if r.ops.close(lockFD) != nil {
				r.poison()
			}
			return fail()
		}
		if ok {
			locked = true
			break
		}
		if err := lockBackoff(ctx, attempt); err != nil {
			failed := r.ops.close(lockFD) != nil
			failed = r.ops.close(rootFD) != nil || failed
			if failed {
				r.poison()
			}
			return nil, err
		}
	}
	if !locked {
		if r.ops.close(lockFD) != nil {
			r.poison()
		}
		return fail()
	}
	grammar, err = r.verifyFreshRootGrammar()
	if err != nil || !sameIdentity(lockStat, grammar.lock) {
		failed := r.ops.unlock(lockFD) != nil
		failed = r.ops.close(lockFD) != nil || failed
		failed = r.ops.close(rootFD) != nil || failed
		if failed {
			r.poison()
		}
		if err != nil {
			return nil, err
		}
		return nil, filesystem("lease-lock-replaced")
	}
	lease := &Lease{seal: &struct{}{}, root: r, rootFD: rootFD, lockFD: lockFD, lock: lockStat, generation: 1, valid: true}
	lease.self = lease
	return lease, nil
}

func (r *Root) AcquireRoot(ctx context.Context) (*RootLease, error) { return r.Acquire(ctx) }

func (l *Lease) Active() bool {
	if l == nil || l.self != l || l.seal == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active()
}

func (l *Lease) Close() error {
	if l == nil || l.self != l || l.seal == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrLeaseInvalid
	}
	l.closed, l.valid = true, false
	failed := l.root.ops.unlock(l.lockFD) != nil
	failed = l.root.ops.close(l.lockFD) != nil || failed
	failed = l.root.ops.close(l.rootFD) != nil || failed
	if failed {
		l.root.poison()
		return filesystem("lease-close")
	}
	return nil
}

func (l *Lease) active() bool {
	return l != nil && l.self == l && l.seal != nil && l.root != nil && l.root.usable() && l.valid && !l.closed && l.rootFD >= 0 && l.lockFD >= 0
}

func (l *Lease) invalidate() {
	l.valid = false
	l.generation++
}

func lockBackoff(ctx context.Context, attempt int) error {
	delay := time.Duration(attempt+1) * 100 * time.Microsecond
	if delay > 5*time.Millisecond {
		delay = 5 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type rootGrammar struct{ lock fileStat }

func (r *Root) verifyFreshRootGrammar() (rootGrammar, error) {
	fd, err := r.freshRoot()
	if err != nil {
		return rootGrammar{}, err
	}
	grammar, grammarErr := r.verifyRootGrammar(fd)
	if r.ops.close(fd) != nil {
		r.poison()
		return rootGrammar{}, filesystem("root-grammar-close")
	}
	return grammar, grammarErr
}

func (r *Root) verifyRootGrammar(rootFD int) (rootGrammar, error) {
	names, err := r.ops.readDirNames(rootFD, 4)
	if err != nil {
		return rootGrammar{}, filesystem("root-list")
	}
	seen := map[string]bool{}
	var grammar rootGrammar
	for _, name := range names {
		if seen[name] || (name != "objects" && name != "lineages" && name != "lineages.lock") {
			return rootGrammar{}, filesystem("root-grammar")
		}
		seen[name] = true
		switch name {
		case "objects", "lineages":
			fd, _, openErr := r.openVerifiedDirectory(rootFD, name)
			if openErr != nil {
				return rootGrammar{}, openErr
			}
			if r.ops.close(fd) != nil {
				r.poison()
				return rootGrammar{}, filesystem("root-directory-close")
			}
		case "lineages.lock":
			fd, st, openErr := r.openVerifiedRegular(rootFD, name)
			if openErr != nil {
				return rootGrammar{}, openErr
			}
			if r.ops.close(fd) != nil {
				r.poison()
				return rootGrammar{}, filesystem("root-lock-close")
			}
			grammar.lock = st
		}
	}
	if !seen["objects"] || !seen["lineages.lock"] {
		return rootGrammar{}, filesystem("root-required-entry")
	}
	return grammar, nil
}

func (r *Root) openVerifiedDirectory(parent int, name string) (int, fileStat, error) {
	before, err := r.ops.lstatAt(parent, name)
	if err != nil || !validDirectory(before, r.uid, r.identity.device) {
		return -1, fileStat{}, filesystem("directory-lstat")
	}
	fd, err := r.ops.openDirAt(parent, name)
	if err != nil {
		return -1, fileStat{}, filesystem("directory-open")
	}
	after, err := r.ops.fstat(fd)
	if err != nil || !sameNodeIdentity(before, after) || !validDirectory(after, r.uid, r.identity.device) {
		if r.ops.close(fd) != nil {
			r.poison()
		}
		return -1, fileStat{}, filesystem("directory-identity")
	}
	return fd, after, nil
}

func (r *Root) openVerifiedRegular(parent int, name string) (int, fileStat, error) {
	before, err := r.ops.lstatAt(parent, name)
	if err != nil || !validRegular(before, r.uid, r.identity.device) {
		return -1, fileStat{}, filesystem("file-lstat")
	}
	fd, err := r.ops.openFileAt(parent, name, false)
	if err != nil {
		return -1, fileStat{}, filesystem("file-open")
	}
	after, err := r.ops.fstat(fd)
	if err != nil || !sameIdentity(before, after) || !validRegular(after, r.uid, r.identity.device) {
		if r.ops.close(fd) != nil {
			r.poison()
		}
		return -1, fileStat{}, filesystem("file-identity")
	}
	return fd, after, nil
}

func validDirectory(st fileStat, uid uint32, device uint64) bool {
	return st.kind == kindDirectory && st.uid == uid && st.device != 0 && st.device == device && st.inode != 0 && st.mode&^directoryMode == 0
}

func validRegular(st fileStat, uid uint32, device uint64) bool {
	return st.kind == kindRegular && st.uid == uid && st.device != 0 && st.device == device && st.inode != 0 && st.nlink == 1 && st.mode&^fileMode == 0
}

func sameIdentity(a, b fileStat) bool {
	return a.device == b.device && a.inode == b.inode && a.kind == b.kind && a.uid == b.uid && a.mode == b.mode && a.nlink == b.nlink && a.size == b.size
}

func sameNodeIdentity(a, b fileStat) bool {
	return a.device == b.device && a.inode == b.inode && a.kind == b.kind
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func filesystem(op string) error { return fmt.Errorf("%w: %s", ErrFilesystem, op) }
func corrupt(op string) error    { return fmt.Errorf("%w: %s", ErrCorrupt, op) }
func limit(op string) error      { return fmt.Errorf("%w: %s", ErrLimit, op) }
func unknown(cause error) error  { return fmt.Errorf("%w: %w", ErrUnknown, cause) }
