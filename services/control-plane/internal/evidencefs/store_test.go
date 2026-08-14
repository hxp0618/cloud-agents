package evidencefs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"testing"
)

var (
	fakeExist    = errors.New("exist")
	fakeNotExist = errors.New("not exist")
)

type fakeNode struct {
	name        string
	stat        fileStat
	data        []byte
	children    map[string]*fakeNode
	locked      bool
	virtualZero bool
}

type fakeHandle struct {
	node    *fakeNode
	dirRead bool
	locked  bool
}

type fakeBackend struct {
	root                *fakeNode
	handles             map[int]fakeHandle
	nextFD              int
	nextInode           uint64
	uid                 uint32
	device              uint64
	rootOpens           int
	directoryOpens      map[string]int
	mkdirs              []string
	lstats              int
	preads              int
	writes              int
	failWriteAt         int
	onWrite             func(*fakeBackend, *fakeNode, int)
	truncates           int
	truncateNames       []string
	failTruncateAt      int
	failTruncateAfterAt int
	onTruncate          func(*fakeBackend, *fakeNode, int)
	fdatasyncs          int
	fdatasyncNames      []string
	onFdatasync         func(*fakeBackend, *fakeNode, int)
	fsyncs              int
	fsyncNames          []string
	onFsync             func(*fakeBackend, *fakeNode, int)
	renames             int
	unlinks             int
	partialWrite        int
	renameConflict      bool
	conflictContent     []byte
	failFdatasync       bool
	failFdatasyncAt     int
	failFsync           bool
	failFsyncAt         int
	failUnlock          bool
	failCloseNames      map[string]bool
	failCloseAt         map[string]int
	closeNameCounts     map[string]int
	closeAttempts       []string
	unlockAttempts      int
	unlockInodes        []uint64
	onUnlock            func(*fakeBackend, *fakeNode, int)
	failRename          bool
	replaceOnOpen       string
	replaceOnOpenAt     int
	replaceAfterLock    bool
	failCloseAfterLock  bool
	openNameCounts      map[string]int
	consumeDirents      bool
	readDirErr          error
	readDirCalls        int
	onReadDir           func(*fakeBackend, *fakeNode, int)
	preadCalls          int
	onPread             func(*fakeBackend, *fakeNode, int)
	tryLockAttempts     []string
	onTryLock           func(*fakeBackend, *fakeNode, int)
	busyInodeAttempts   map[uint64]int
	failTryLockInodes   map[uint64]bool
	failCloseName       string
	failOpenNames       map[string]error
	nonce               [16]byte
}

func newFakeBackend() *fakeBackend {
	f := &fakeBackend{handles: map[int]fakeHandle{}, nextFD: 10, nextInode: 100, uid: 501, device: 7, directoryOpens: map[string]int{}, openNameCounts: map[string]int{}}
	f.failCloseNames = map[string]bool{}
	f.failCloseAt = map[string]int{}
	f.closeNameCounts = map[string]int{}
	f.busyInodeAttempts = map[uint64]int{}
	f.failTryLockInodes = map[uint64]bool{}
	f.failOpenNames = map[string]error{}
	f.root = f.directory("root")
	objects := f.directory("objects")
	sha := f.directory("sha256")
	objects.children["sha256"] = sha
	f.root.children["objects"] = objects
	f.root.children["lineages.lock"] = f.regular("lineages.lock", nil)
	for index := range f.nonce {
		f.nonce[index] = byte(index + 1)
	}
	return f
}

func (f *fakeBackend) directory(name string) *fakeNode {
	f.nextInode++
	return &fakeNode{name: name, stat: fileStat{device: f.device, inode: f.nextInode, mode: 0o700, uid: f.uid, nlink: 2, kind: kindDirectory}, children: map[string]*fakeNode{}}
}

func (f *fakeBackend) regular(name string, data []byte) *fakeNode {
	f.nextInode++
	copyData := append([]byte(nil), data...)
	return &fakeNode{name: name, stat: fileStat{device: f.device, inode: f.nextInode, size: uint64(len(copyData)), mode: 0o600, uid: f.uid, nlink: 1, kind: kindRegular}, data: copyData}
}

func (f *fakeBackend) shaDir() *fakeNode { return f.root.children["objects"].children["sha256"] }

func (f *fakeBackend) addFinal(data []byte) [32]byte {
	digest := sha256.Sum256(data)
	name := fmt.Sprintf("%x", digest)
	f.shaDir().children[name] = f.regular(name, data)
	return digest
}

func (f *fakeBackend) addTemp(index int, data []byte) string {
	name := fmt.Sprintf(".tmp-%032x", index)
	f.shaDir().children[name] = f.regular(name, data)
	return name
}

func (f *fakeBackend) alloc(node *fakeNode) int {
	fd := f.nextFD
	f.nextFD++
	f.handles[fd] = fakeHandle{node: node}
	return fd
}

func (f *fakeBackend) node(fd int) (*fakeNode, error) {
	handle, ok := f.handles[fd]
	if !ok || handle.node == nil {
		return nil, fakeNotExist
	}
	return handle.node, nil
}

func (f *fakeBackend) openRoot(string) (int, error) {
	f.rootOpens++
	return f.alloc(f.root), nil
}

func (f *fakeBackend) openDirAt(parent int, name string) (int, error) {
	if err := f.failOpenNames[name]; err != nil {
		return -1, err
	}
	node, err := f.child(parent, name)
	if err != nil || node.stat.kind != kindDirectory {
		return -1, fakeNotExist
	}
	f.directoryOpens[name]++
	return f.alloc(node), nil
}

func (f *fakeBackend) lstatAt(parent int, name string) (fileStat, error) {
	f.lstats++
	node, err := f.child(parent, name)
	if err != nil {
		return fileStat{}, err
	}
	return node.stat, nil
}

func (f *fakeBackend) openFileAt(parent int, name string, create bool) (int, error) {
	if err := f.failOpenNames[name]; err != nil {
		return -1, err
	}
	parentNode, err := f.node(parent)
	if err != nil || parentNode.stat.kind != kindDirectory {
		return -1, fakeNotExist
	}
	if create {
		if _, exists := parentNode.children[name]; exists {
			return -1, fakeExist
		}
		node := f.regular(name, nil)
		parentNode.children[name] = node
		return f.alloc(node), nil
	}
	node, exists := parentNode.children[name]
	if !exists || node.stat.kind != kindRegular {
		return -1, fakeNotExist
	}
	f.openNameCounts[name]++
	replaceAt := f.replaceOnOpenAt
	if replaceAt == 0 {
		replaceAt = 1
	}
	if f.replaceOnOpen == name && f.openNameCounts[name] == replaceAt {
		replacement := f.regular(name, node.data)
		parentNode.children[name] = replacement
		node = replacement
		f.replaceOnOpen = ""
	}
	return f.alloc(node), nil
}

func (f *fakeBackend) openFileAtReadWrite(parent int, name string) (int, error) {
	return f.openFileAt(parent, name, false)
}

func (f *fakeBackend) mkdirAt(parent int, name string) error {
	parentNode, err := f.node(parent)
	if err != nil || parentNode.stat.kind != kindDirectory {
		return fakeNotExist
	}
	f.mkdirs = append(f.mkdirs, name)
	if _, exists := parentNode.children[name]; exists {
		return fakeExist
	}
	parentNode.children[name] = f.directory(name)
	parentNode.stat.nlink++
	return nil
}

func (f *fakeBackend) child(parent int, name string) (*fakeNode, error) {
	node, err := f.node(parent)
	if err != nil || node.children == nil {
		return nil, fakeNotExist
	}
	child, ok := node.children[name]
	if !ok {
		return nil, fakeNotExist
	}
	return child, nil
}

func (f *fakeBackend) fstat(fd int) (fileStat, error) {
	node, err := f.node(fd)
	if err != nil {
		return fileStat{}, err
	}
	return node.stat, nil
}

func (f *fakeBackend) readDirNames(fd int, maximum int) ([]string, error) {
	if f.readDirErr != nil {
		return nil, f.readDirErr
	}
	handle, ok := f.handles[fd]
	if !ok || handle.node == nil {
		return nil, fakeNotExist
	}
	node := handle.node
	f.readDirCalls++
	if f.onReadDir != nil {
		f.onReadDir(f, node, f.readDirCalls)
	}
	if f.consumeDirents && handle.dirRead {
		return nil, nil
	}
	handle.dirRead = true
	f.handles[fd] = handle
	var err error
	if err != nil || node.children == nil {
		return nil, fakeNotExist
	}
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maximum {
		return nil, errors.New("too many names")
	}
	return names, nil
}
func (f *fakeBackend) isOverflow(err error) bool {
	return err != nil && err.Error() == "too many names"
}
func (f *fakeBackend) isNotExist(err error) bool { return errors.Is(err, fakeNotExist) }

func (f *fakeBackend) pread(fd int, target []byte, offset int64) (int, error) {
	f.preads++
	node, err := f.node(fd)
	if err != nil || offset < 0 {
		return 0, fakeNotExist
	}
	f.preadCalls++
	if f.onPread != nil {
		f.onPread(f, node, f.preadCalls)
	}
	if offset >= int64(len(node.data)) {
		if node.virtualZero && offset < int64(node.stat.size) {
			remaining := int64(node.stat.size) - offset
			n := len(target)
			if int64(n) > remaining {
				n = int(remaining)
			}
			clear(target[:n])
			if offset+int64(n) == int64(node.stat.size) {
				return n, io.EOF
			}
			return n, nil
		}
		return 0, io.EOF
	}
	n := copy(target, node.data[offset:])
	if int(offset)+n == len(node.data) {
		return n, io.EOF
	}
	return n, nil
}

func (f *fakeBackend) pwrite(fd int, source []byte, offset int64) (int, error) {
	f.writes++
	node, err := f.node(fd)
	if err != nil || offset < 0 || offset > int64(len(node.data)) {
		return 0, fakeNotExist
	}
	if f.onWrite != nil {
		f.onWrite(f, node, f.writes)
	}
	if f.failWriteAt > 0 && f.writes == f.failWriteAt {
		return 0, errors.New("write")
	}
	n := len(source)
	if f.partialWrite > 0 && n > f.partialWrite {
		n = f.partialWrite
	}
	end := int(offset) + n
	if end > len(node.data) {
		node.data = append(node.data, make([]byte, end-len(node.data))...)
	}
	copy(node.data[int(offset):end], source[:n])
	node.stat.size = uint64(len(node.data))
	return n, nil
}

func (f *fakeBackend) truncate(fd int, size int64) error {
	f.truncates++
	node, err := f.node(fd)
	if err != nil || size < 0 {
		return fakeNotExist
	}
	f.truncateNames = append(f.truncateNames, node.name)
	if f.onTruncate != nil {
		f.onTruncate(f, node, f.truncates)
	}
	if f.failTruncateAt > 0 && f.truncates == f.failTruncateAt {
		return errors.New("truncate")
	}
	next := int(size)
	if next < len(node.data) {
		node.data = node.data[:next]
	} else if next > len(node.data) {
		node.data = append(node.data, make([]byte, next-len(node.data))...)
	}
	node.stat.size = uint64(next)
	if f.failTruncateAfterAt > 0 && f.truncates == f.failTruncateAfterAt {
		return errors.New("truncate")
	}
	return nil
}

func (f *fakeBackend) write(fd int, source []byte) (int, error) {
	f.writes++
	node, err := f.node(fd)
	if err != nil {
		return 0, err
	}
	if f.onWrite != nil {
		f.onWrite(f, node, f.writes)
	}
	if f.failWriteAt > 0 && f.writes == f.failWriteAt {
		return 0, errors.New("write")
	}
	n := len(source)
	if f.partialWrite > 0 && n > f.partialWrite {
		n = f.partialWrite
	}
	node.data = append(node.data, source[:n]...)
	node.stat.size = uint64(len(node.data))
	return n, nil
}

func (f *fakeBackend) fdatasync(fd int) error {
	f.fdatasyncs++
	if node, err := f.node(fd); err == nil {
		f.fdatasyncNames = append(f.fdatasyncNames, node.name)
		if f.onFdatasync != nil {
			f.onFdatasync(f, node, f.fdatasyncs)
		}
	}
	if f.failFdatasync || (f.failFdatasyncAt > 0 && f.fdatasyncs == f.failFdatasyncAt) {
		return errors.New("fdatasync")
	}
	return nil
}

func (f *fakeBackend) fsync(fd int) error {
	f.fsyncs++
	if node, err := f.node(fd); err == nil {
		f.fsyncNames = append(f.fsyncNames, node.name)
		if f.onFsync != nil {
			f.onFsync(f, node, f.fsyncs)
		}
	}
	if f.failFsync || (f.failFsyncAt > 0 && f.fsyncs == f.failFsyncAt) {
		return errors.New("fsync")
	}
	return nil
}

func (f *fakeBackend) renameNoReplace(parent int, oldName, newName string) error {
	f.renames++
	parentNode, err := f.node(parent)
	if err != nil {
		return err
	}
	if f.failRename {
		return errors.New("rename")
	}
	if f.renameConflict {
		if _, exists := parentNode.children[newName]; !exists {
			parentNode.children[newName] = f.regular(newName, f.conflictContent)
		}
		return fakeExist
	}
	if _, exists := parentNode.children[newName]; exists {
		return fakeExist
	}
	node, exists := parentNode.children[oldName]
	if !exists {
		return fakeNotExist
	}
	delete(parentNode.children, oldName)
	node.name = newName
	parentNode.children[newName] = node
	return nil
}

func (f *fakeBackend) unlinkAt(parent int, name string) error {
	f.unlinks++
	parentNode, err := f.node(parent)
	if err != nil {
		return err
	}
	if _, exists := parentNode.children[name]; !exists {
		return fakeNotExist
	}
	delete(parentNode.children, name)
	return nil
}

func (f *fakeBackend) isExist(err error) bool { return errors.Is(err, fakeExist) }

func (f *fakeBackend) tryLock(fd int) (bool, error) {
	node, err := f.node(fd)
	if err != nil {
		return false, err
	}
	f.tryLockAttempts = append(f.tryLockAttempts, node.name)
	if f.onTryLock != nil {
		f.onTryLock(f, node, len(f.tryLockAttempts))
	}
	if f.failTryLockInodes[node.stat.inode] {
		delete(f.failTryLockInodes, node.stat.inode)
		return false, errors.New("try lock")
	}
	if f.busyInodeAttempts[node.stat.inode] > 0 {
		f.busyInodeAttempts[node.stat.inode]--
		return false, nil
	}
	if node.locked {
		return false, nil
	}
	node.locked = true
	handle := f.handles[fd]
	handle.locked = true
	f.handles[fd] = handle
	if f.replaceAfterLock {
		f.root.children["lineages.lock"] = f.regular("lineages.lock", node.data)
		f.replaceAfterLock = false
	}
	if f.failCloseAfterLock {
		f.failCloseNames["lineages.lock"] = true
		f.failCloseAfterLock = false
	}
	return true, nil
}

func (f *fakeBackend) unlock(fd int) error {
	f.unlockAttempts++
	node, err := f.node(fd)
	if err != nil {
		return err
	}
	f.unlockInodes = append(f.unlockInodes, node.stat.inode)
	if f.onUnlock != nil {
		f.onUnlock(f, node, f.unlockAttempts)
	}
	handle := f.handles[fd]
	if handle.locked {
		node.locked = false
		handle.locked = false
		f.handles[fd] = handle
	}
	if f.failUnlock {
		return errors.New("unlock")
	}
	return nil
}

func (f *fakeBackend) close(fd int) error {
	handle := f.handles[fd]
	if handle.node != nil {
		f.closeAttempts = append(f.closeAttempts, handle.node.name)
		f.closeNameCounts[handle.node.name]++
	}
	delete(f.handles, fd)
	if handle.node != nil && (handle.node.name == f.failCloseName || f.failCloseNames[handle.node.name] || f.failCloseAt[handle.node.name] == f.closeNameCounts[handle.node.name]) {
		f.failCloseName = ""
		return errors.New("close")
	}
	return nil
}

func (f *fakeBackend) random(target []byte) error {
	if len(target) != len(f.nonce) {
		return errors.New("nonce size")
	}
	copy(target, f.nonce[:])
	return nil
}

func testLease(t *testing.T, f *fakeBackend) *Lease {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	return lease
}

func TestProductionOpenFailsClosed(t *testing.T) {
	root, err := Open(context.Background(), "/evidence")
	if root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("root=%v err=%v", root, err)
	}
}

func TestAcquireUsesLineagesLockAndRejectsRootGrammar(t *testing.T) {
	f := newFakeBackend()
	lease := testLease(t, f)
	if !f.root.children["lineages.lock"].locked || lease.lock.inode == 0 {
		t.Fatal("lineages.lock was not acquired")
	}
	f2 := newFakeBackend()
	f2.root.children["objects.lock"] = f2.regular("objects.lock", nil)
	root, err := newRootWithAuthority(context.Background(), "/evidence", f2.uid, f2, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("unexpected root entry admitted: %v", err)
	}
}

func TestRootGrammarCloseFailuresPoisonAndMintNoLease(t *testing.T) {
	for _, name := range []string{"root", "objects", "lineages", "lineages.lock"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			f.root.children["lineages"] = f.directory("lineages")
			root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
			if err != nil {
				t.Fatal(err)
			}
			f.failCloseNames[name] = true
			lease, err := root.Acquire(context.Background())
			if lease != nil || !errors.Is(err, ErrFilesystem) || root.usable() {
				t.Fatalf("lease=%v err=%v usable=%v closes=%v", lease, err, root.usable(), f.closeAttempts)
			}
		})
	}
}

func TestRootConstructorCloseFailureReturnsNoAuthority(t *testing.T) {
	f := newFakeBackend()
	f.failCloseAt["root"] = 1
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if root != nil || !errors.Is(err, ErrFilesystem) {
		t.Fatalf("root=%v err=%v closes=%v", root, err, f.closeAttempts)
	}
}

func TestFreshDirectoryHandlesSurviveConsumingReadDir(t *testing.T) {
	f := newFakeBackend()
	f.consumeDirents = true
	f.addFinal([]byte("artifact"))
	lease := testLease(t, f)
	if _, err := lease.Scan(context.Background()); err != nil {
		t.Fatalf("scan reused a consumed directory descriptor: %v", err)
	}
}

func TestTwoRootsContendOnTheSameLineagesLock(t *testing.T) {
	f := newFakeBackend()
	rootA, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	rootB, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	leaseA, err := rootA.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rootB.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("second root acquired shared lineages.lock: %v", err)
	}
	if err := leaseA.Close(); err != nil {
		t.Fatal(err)
	}
	leaseB, err := rootB.Acquire(context.Background())
	if err != nil {
		t.Fatalf("lock remained held after close: %v", err)
	}
	if err := leaseB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireCleanupAttemptsUnlockAndEveryClose(t *testing.T) {
	f := newFakeBackend()
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	// Force the post-flock grammar recheck to observe a different lock inode.
	original := f.root.children["lineages.lock"]
	f.replaceAfterLock = true
	f.failUnlock = true
	f.failCloseAfterLock = true
	if lease, err := root.Acquire(context.Background()); lease != nil || !errors.Is(err, ErrFilesystem) {
		t.Fatalf("lease=%v err=%v", lease, err)
	}
	if f.unlockAttempts != 1 {
		t.Fatalf("unlock attempts=%d locked=%v", f.unlockAttempts, original.locked)
	}
	lockCloses, rootCloses := 0, 0
	for _, name := range f.closeAttempts {
		if name == "lineages.lock" {
			lockCloses++
		}
		if name == "root" {
			rootCloses++
		}
	}
	if lockCloses == 0 || rootCloses == 0 || root.usable() {
		t.Fatalf("close attempts=%v root usable=%v", f.closeAttempts, root.usable())
	}
}

func TestScanHashesFinalAndTempWithFreshHandles(t *testing.T) {
	f := newFakeBackend()
	final := f.addFinal([]byte("artifact"))
	tempName := f.addTemp(1, []byte("partial"))
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !scan.HasObject(final, 8) || scan.finalCount != 1 || scan.tempCount != 1 || f.preads != 2 {
		t.Fatalf("scan=%+v preads=%d", scan, f.preads)
	}
	if _, exists := f.shaDir().children[tempName]; !exists {
		t.Fatal("scan performed forbidden temp GC")
	}
	if f.directoryOpens["objects"] < 3 || f.directoryOpens["sha256"] != 1 {
		t.Fatalf("directory opens=%v", f.directoryOpens)
	}
}

func TestScanCloseFailuresPoisonAndAttemptAllCleanup(t *testing.T) {
	tests := map[string]func(*fakeBackend){
		"objects-open-root-close": func(f *fakeBackend) {
			f.failOpenNames["objects"] = errors.New("objects open")
			f.failCloseNames["root"] = true
		},
		"root-close-objects-close": func(f *fakeBackend) {
			f.failCloseNames["root"] = true
			f.failCloseNames["objects"] = true
		},
		"objects-grammar-close": func(f *fakeBackend) {
			f.root.children["objects"].children["unknown"] = f.directory("unknown")
			f.failCloseNames["objects"] = true
		},
		"sha-open-objects-close": func(f *fakeBackend) {
			f.failOpenNames["sha256"] = errors.New("sha open")
			f.failCloseNames["objects"] = true
		},
		"objects-close-sha-cleanup": func(f *fakeBackend) { f.failCloseNames["objects"] = true },
		"sha-close":                 func(f *fakeBackend) { f.failCloseNames["sha256"] = true },
		"entry-close": func(f *fakeBackend) {
			f.addFinal([]byte("object"))
			f.failCloseNames[fmt.Sprintf("%x", sha256.Sum256([]byte("object")))] = true
		},
	}
	for name, configure := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFakeBackend()
			root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
			if err != nil {
				t.Fatal(err)
			}
			lease, err := root.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			baseline := len(f.handles)
			configure(f)
			if scan, err := lease.Scan(context.Background()); scan != nil || !errors.Is(err, ErrFilesystem) || root.usable() {
				t.Fatalf("scan=%v err=%v usable=%v closes=%v", scan, err, root.usable(), f.closeAttempts)
			}
			if len(f.handles) != baseline {
				t.Fatalf("handles=%d baseline=%d closes=%v", len(f.handles), baseline, f.closeAttempts)
			}
			if _, err := root.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) {
				t.Fatalf("poisoned root reacquired: %v", err)
			}
			_ = lease.Close()
		})
	}
}

func TestScanCloseAmbiguityInvalidatesEarlierScan(t *testing.T) {
	f := newFakeBackend()
	digest := f.addFinal([]byte("object"))
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	old, err := lease.Scan(context.Background())
	if err != nil || !old.HasObject(digest, 6) {
		t.Fatalf("old scan err=%v", err)
	}
	f.failCloseNames["sha256"] = true
	if scan, err := lease.Scan(context.Background()); scan != nil || !errors.Is(err, ErrFilesystem) {
		t.Fatalf("scan=%v err=%v", scan, err)
	}
	if old.HasObject(digest, 6) || old.Usage().FinalBytes != 0 {
		t.Fatal("old scan retained authority after close ambiguity")
	}
	_ = lease.Close()
}

func TestScanContextAndCloseFailureReturnsFilesystem(t *testing.T) {
	f := newFakeBackend()
	f.addFinal([]byte("object"))
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.onPread = func(_ *fakeBackend, node *fakeNode, _ int) {
		if finalNamePattern.MatchString(node.name) {
			cancel()
			f.failCloseNames[node.name] = true
		}
	}
	if scan, err := lease.Scan(ctx); scan != nil || !errors.Is(err, ErrFilesystem) || root.usable() {
		t.Fatalf("scan=%v err=%v usable=%v", scan, err, root.usable())
	}
	_ = lease.Close()
}

func TestScanEnforcesGrammarAndBoundsBeforeReading(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		f := newFakeBackend()
		f.shaDir().children["unknown"] = f.regular("unknown", []byte("x"))
		lease := testLease(t, f)
		if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrFilesystem) || f.preads != 0 {
			t.Fatalf("err=%v preads=%d", err, f.preads)
		}
	})
	t.Run("sixty-fifth-temp", func(t *testing.T) {
		f := newFakeBackend()
		for index := 0; index < 65; index++ {
			f.addTemp(index, nil)
		}
		lease := testLease(t, f)
		if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrLimit) || f.preads != 0 {
			t.Fatalf("err=%v preads=%d", err, f.preads)
		}
	})
	t.Run("oversized", func(t *testing.T) {
		f := newFakeBackend()
		name := f.addTemp(1, nil)
		f.shaDir().children[name].stat.size = maximumObjectSize + 1
		lease := testLease(t, f)
		if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrLimit) || f.preads != 0 {
			t.Fatalf("err=%v preads=%d", err, f.preads)
		}
	})
}

func TestScanDistinguishesReadDirIOFromOverflow(t *testing.T) {
	f := newFakeBackend()
	f.readDirErr = errors.New("io failure")
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) || errors.Is(err, ErrLimit) {
		t.Fatalf("readdir I/O classification=%v", err)
	}
}

func TestCopiedAndZeroRootLeaseRejectWithoutClosingOwnedFDs(t *testing.T) {
	f := newFakeBackend()
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	rootCopy := Root{self: root.self, seal: root.seal, ops: root.ops, path: root.path, uid: root.uid, identity: root.identity}
	if _, err := rootCopy.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("copied root acquired: %v", err)
	}
	var zeroRoot Root
	if _, err := zeroRoot.Acquire(context.Background()); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("zero root acquired: %v", err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	leaseCopy := Lease{self: lease.self, seal: lease.seal, root: lease.root, rootFD: lease.rootFD, lockFD: lease.lockFD, lock: lease.lock, generation: lease.generation, valid: lease.valid}
	if leaseCopy.Active() || !errors.Is(leaseCopy.Close(), ErrLeaseInvalid) || !lease.Active() {
		t.Fatal("copied lease affected original authority")
	}
	var zeroLease Lease
	if zeroLease.Active() || !errors.Is(zeroLease.Close(), ErrLeaseInvalid) {
		t.Fatal("zero lease admitted")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestScanRejectsIdentityRaceAndDigestMismatch(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		f := newFakeBackend()
		digest := f.addFinal([]byte("artifact"))
		f.replaceOnOpen = fmt.Sprintf("%x", digest)
		lease := testLease(t, f)
		if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrFilesystem) {
			t.Fatalf("identity race admitted: %v", err)
		}
	})
	t.Run("digest", func(t *testing.T) {
		f := newFakeBackend()
		f.shaDir().children[fmt.Sprintf("%064x", 1)] = f.regular("bad", []byte("artifact"))
		lease := testLease(t, f)
		if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("digest mismatch admitted: %v", err)
		}
	})
}

func TestPublishDurablyVerifiesAndInvalidatesPriorScan(t *testing.T) {
	f := newFakeBackend()
	f.partialWrite = 2
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("published artifact")
	digest := sha256.Sum256(content)
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if publication.Matches(digest, uint64(len(content))) || publication.Identity() != nil {
		t.Fatal("transient publication exposed authority before binding")
	}
	if err := lease.BindPublication(publication, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if !publication.Matches(digest, uint64(len(content))) || publication.Identity() == nil || f.fdatasyncs != 2 || f.renames != 1 || f.fsyncs != 1 || f.writes < 2 {
		t.Fatalf("publication=%+v sync=%d/%d rename=%d writes=%d", publication, f.fdatasyncs, f.fsyncs, f.renames, f.writes)
	}
	if _, err := lease.Publish(context.Background(), scan, digest, content); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("stale scan reused: %v", err)
	}
	rescan, err := lease.Scan(context.Background())
	if err != nil || !rescan.HasObject(digest, uint64(len(content))) {
		t.Fatalf("rescan=%v err=%v", rescan, err)
	}
}

func TestPublicationBindIsOneShotAndCopyCannotMintAuthority(t *testing.T) {
	f := newFakeBackend()
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("kind-bound")
	digest := sha256.Sum256(content)
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	copyValue := *publication
	if err := lease.BindPublication(&copyValue, digest, uint64(len(content))); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied transient publication bound: %v", err)
	}
	if copyValue.Matches(digest, uint64(len(content))) || copyValue.Identity() != nil {
		t.Fatal("copied publication retained authority")
	}
	if err := lease.BindPublication(publication, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if err := lease.BindPublication(publication, digest, uint64(len(content))); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("publication bound twice: %v", err)
	}
	var zero Publication
	if zero.Matches(digest, uint64(len(content))) || zero.Identity() != nil {
		t.Fatal("zero publication minted authority")
	}
	boundCopy := *publication
	if boundCopy.Matches(digest, uint64(len(content))) || boundCopy.Identity() != nil {
		t.Fatal("copied bound publication retained authority")
	}
}

func TestUnboundPublicationExpiresOnCloseOrNextPublish(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		f := newFakeBackend()
		root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
		if err != nil {
			t.Fatal(err)
		}
		lease, err := root.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		scan, err := lease.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("close expires")
		digest := sha256.Sum256(content)
		publication, err := lease.Publish(context.Background(), scan, digest, content)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
		if err := lease.BindPublication(publication, digest, uint64(len(content))); !errors.Is(err, ErrLeaseInvalid) || publication.Matches(digest, uint64(len(content))) {
			t.Fatalf("closed lease bound publication: %v", err)
		}
	})
	t.Run("next-publish", func(t *testing.T) {
		f := newFakeBackend()
		lease := testLease(t, f)
		firstScan, err := lease.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		first := []byte("first")
		firstDigest := sha256.Sum256(first)
		firstPublication, err := lease.Publish(context.Background(), firstScan, firstDigest, first)
		if err != nil {
			t.Fatal(err)
		}
		secondScan, err := lease.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		second := []byte("second")
		secondDigest := sha256.Sum256(second)
		secondPublication, err := lease.Publish(context.Background(), secondScan, secondDigest, second)
		if err != nil {
			t.Fatal(err)
		}
		if err := lease.BindPublication(firstPublication, firstDigest, uint64(len(first))); !errors.Is(err, ErrLeaseInvalid) {
			t.Fatalf("older publication bound after next publish: %v", err)
		}
		if err := lease.BindPublication(secondPublication, secondDigest, uint64(len(second))); err != nil {
			t.Fatal(err)
		}
		if !secondPublication.Matches(secondDigest, uint64(len(second))) {
			t.Fatal("current publication did not bind")
		}
	})
}

func TestBoundPublicationSurvivesLeaseClose(t *testing.T) {
	f := newFakeBackend()
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("detached authority")
	digest := sha256.Sum256(content)
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.BindPublication(publication, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if !publication.Matches(digest, uint64(len(content))) || publication.Identity() == nil {
		t.Fatal("bound publication did not survive normal lease close")
	}
}

func TestBoundPublicationOpaqueComparators(t *testing.T) {
	f := newFakeBackend()
	lease := testLease(t, f)
	content := []byte("same-content")
	digest := sha256.Sum256(content)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.BindPublication(first, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	scan, err = lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if first.SameStore(second) || first.SameObject(second) {
		t.Fatal("transient publication entered bound comparator")
	}
	if err := lease.BindPublication(second, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if !first.SameStore(second) || !first.SameObject(second) {
		t.Fatal("same bound object did not compare equal")
	}
	otherBackend := newFakeBackend()
	otherLease := testLease(t, otherBackend)
	otherScan, err := otherLease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := otherLease.Publish(context.Background(), otherScan, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if err := otherLease.BindPublication(foreign, digest, uint64(len(content))); err != nil {
		t.Fatal(err)
	}
	if first.SameStore(foreign) || first.SameObject(foreign) || (&Publication{}).SameStore(first) || (&Publication{}).SameObject(first) {
		t.Fatal("foreign or literal publication compared as bound authority")
	}
}

func TestExistingReuseSyncOrderAndFailures(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newFakeBackend()
		content := []byte("existing")
		digest := f.addFinal(content)
		lease := testLease(t, f)
		scan, err := lease.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		publication, err := lease.Publish(context.Background(), scan, digest, content)
		if err != nil {
			t.Fatal(err)
		}
		if f.fdatasyncs != 1 || f.fsyncs != 1 {
			t.Fatalf("final/directory sync=%d/%d", f.fdatasyncs, f.fsyncs)
		}
		if err := lease.BindPublication(publication, digest, uint64(len(content))); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("final-fdatasync", func(t *testing.T) {
		f := newFakeBackend()
		content := []byte("existing")
		digest := f.addFinal(content)
		lease := testLease(t, f)
		scan, err := lease.Scan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.failFdatasync = true
		publication, err := lease.Publish(context.Background(), scan, digest, content)
		if publication != nil || !errors.Is(err, ErrFilesystem) || !errors.Is(err, ErrUnknown) || lease.Active() {
			t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
		}
	})
}

func TestConflictFinalSyncFailureIsUnknown(t *testing.T) {
	f := newFakeBackend()
	content := []byte("conflict")
	digest := sha256.Sum256(content)
	f.renameConflict = true
	f.conflictContent = content
	f.failFdatasyncAt = 2
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if publication != nil || !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) || lease.Active() {
		t.Fatalf("publication=%v err=%v active=%v", publication, err, lease.Active())
	}
}

func TestPublicationCloseFailureReturnsNilAuthority(t *testing.T) {
	f := newFakeBackend()
	content := []byte("already durable")
	digest := f.addFinal(content)
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.failCloseName = fmt.Sprintf("%x", digest)
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if publication != nil || !errors.Is(err, ErrFilesystem) {
		t.Fatalf("publication=%v err=%v", publication, err)
	}
}

func TestUsageReturnsOwnedCopy(t *testing.T) {
	f := newFakeBackend()
	digest := f.addFinal([]byte("artifact"))
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	usage := scan.Usage()
	if len(usage.FinalObjects) != 1 || usage.FinalObjects[0].Digest != digest {
		t.Fatalf("usage=%+v", usage)
	}
	usage.FinalObjects[0].Size = 999
	if !scan.HasObject(digest, 8) || scan.Usage().FinalObjects[0].Size != 8 {
		t.Fatal("usage mutation changed sealed scan")
	}
}

func TestCopiedAndZeroScanCannotAuthorizePublication(t *testing.T) {
	f := newFakeBackend()
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("copy scan")
	digest := sha256.Sum256(content)
	copyValue := *scan
	if _, err := lease.Publish(context.Background(), &copyValue, digest, content); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("copied scan authorized publication: %v", err)
	}
	var zero Scan
	if _, err := lease.Publish(context.Background(), &zero, digest, content); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("zero scan authorized publication: %v", err)
	}
}

func TestPublishConflictVerifiesThenCleansOwnTemp(t *testing.T) {
	f := newFakeBackend()
	content := []byte("same artifact")
	digest := sha256.Sum256(content)
	f.renameConflict = true
	f.conflictContent = content
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := lease.Publish(context.Background(), scan, digest, content)
	if err != nil || publication == nil {
		t.Fatalf("publication=%v err=%v", publication, err)
	}
	if f.unlinks != 1 || f.fsyncs != 1 {
		t.Fatalf("unlink=%d fsync=%d", f.unlinks, f.fsyncs)
	}
	for name := range f.shaDir().children {
		if tempNamePattern.MatchString(name) {
			t.Fatalf("owned temp remains after verified conflict: %s", name)
		}
	}
}

func TestMutationUncertaintyInvalidatesLeaseAndPreservesTemp(t *testing.T) {
	f := newFakeBackend()
	f.failFdatasync = true
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("uncertain")
	digest := sha256.Sum256(content)
	if _, err := lease.Publish(context.Background(), scan, digest, content); !errors.Is(err, ErrUnknown) || !errors.Is(err, ErrFilesystem) {
		t.Fatalf("publish error=%v", err)
	}
	if _, err := lease.Scan(context.Background()); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatalf("invalidated lease scanned: %v", err)
	}
	temps := 0
	for name := range f.shaDir().children {
		if tempNamePattern.MatchString(name) {
			temps++
		}
	}
	if temps != 1 {
		t.Fatalf("uncertain temp count=%d", temps)
	}
}

func TestPublishRejectsDigestMismatchBeforeMutation(t *testing.T) {
	f := newFakeBackend()
	lease := testLease(t, f)
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("owned source")
	wrong := sha256.Sum256([]byte("different"))
	if publication, err := lease.Publish(context.Background(), scan, wrong, content); publication != nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("publication=%v err=%v", publication, err)
	}
	if f.writes != 0 || f.renames != 0 || !lease.Active() {
		t.Fatalf("writes=%d renames=%d active=%v", f.writes, f.renames, lease.Active())
	}
}
