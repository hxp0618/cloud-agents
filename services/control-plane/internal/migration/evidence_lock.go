package migration

import (
	"context"
	"errors"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/evidencefs"
)

const evidenceLockRetryLimit = 64

type evidenceLockKind uint8

const (
	evidenceLineageLockKind evidenceLockKind = iota + 1
	evidenceGenerationLockKind
)

type evidenceLockFile struct {
	ops    evidenceFSOps
	fd     int
	device uint64
	inode  uint64
	kind   evidenceLockKind
}

func verifiedEvidenceLockFile(root *evidenceFSRoot, fd int, kind evidenceLockKind) (evidenceLockFile, error) {
	if root == nil || root.ops == nil || fd < 0 || (kind != evidenceLineageLockKind && kind != evidenceGenerationLockKind) {
		if root != nil && root.ops != nil && fd >= 0 && root.ops.close(fd) != nil {
			return evidenceLockFile{}, filesystemFailure("lock-file", "invalid lock file close failed")
		}
		return evidenceLockFile{}, filesystemFailure("lock-file", "lock file is not admissible")
	}
	st, err := root.ops.stat(fd)
	if err != nil || validateEvidenceRegularFile(st, root.uid, root.device) != nil || st.device == 0 || st.inode == 0 {
		if root.ops.close(fd) != nil {
			return evidenceLockFile{}, filesystemFailure("lock-file", "lock file metadata and close failed")
		}
		return evidenceLockFile{}, filesystemFailure("lock-file", "lock file identity is not admissible")
	}
	return evidenceLockFile{ops: root.ops, fd: fd, device: st.device, inode: st.inode, kind: kind}, nil
}

// evidenceRootLeaseHandle is orchestration-only. Only the sealed production
// wrapper below can expose an evidencefs lease; deterministic test handles may
// exercise ordering but cannot mint Scan, Publish, receipt, or quota authority.
type evidenceRootLeaseHandle interface {
	Active() bool
	Close() error
	publicationLease() *evidencefs.RootLease
}

type evidenceRootStore interface {
	AcquireRoot(context.Context) (evidenceRootLeaseHandle, error)
}

type evidenceFSRootStore struct{ store *evidencefs.Store }

type evidenceFSRootLease struct {
	self   *evidenceFSRootLease
	lease  *evidencefs.RootLease
	closed bool
}

func newEvidenceRootStore(store *evidencefs.Store) evidenceRootStore {
	return &evidenceFSRootStore{store: store}
}

func (s *evidenceFSRootStore) AcquireRoot(ctx context.Context) (evidenceRootLeaseHandle, error) {
	if s == nil || s.store == nil {
		return nil, evidencefs.ErrLeaseInvalid
	}
	lease, err := s.store.AcquireRoot(ctx)
	if err != nil {
		return nil, err
	}
	handle := &evidenceFSRootLease{lease: lease}
	handle.self = handle
	return handle, nil
}

func (l *evidenceFSRootLease) Active() bool {
	return l != nil && l.self == l && !l.closed && l.lease != nil && l.lease.Active()
}

func (l *evidenceFSRootLease) Close() error {
	if l == nil || l.self != l || l.closed || l.lease == nil {
		return evidencefs.ErrLeaseInvalid
	}
	l.closed = true
	return l.lease.Close()
}

func (l *evidenceFSRootLease) publicationLease() *evidencefs.RootLease {
	if !l.Active() {
		return nil
	}
	return l.lease
}

func mapEvidenceRootError(err error, op string) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fail(CodeContextCanceled, op, "context canceled before root lock acquisition", nil)
	case errors.Is(err, context.DeadlineExceeded):
		return fail(CodeDeadlineExceeded, op, "deadline exceeded before root lock acquisition", nil)
	case errors.Is(err, evidencefs.ErrCorrupt):
		return fail(CodeEvidenceJournalCorrupt, op, "evidence object store is corrupt", nil)
	case errors.Is(err, evidencefs.ErrLimit):
		return fail(CodeEvidenceJournalLimitExceeded, op, "evidence object store limit exceeded", nil)
	default:
		return fail(CodeEvidenceJournalFailed, op, "evidence root lock operation failed", nil)
	}
}

type evidenceLineageLock struct {
	self         *evidenceLineageLock
	root         evidenceRootLeaseHandle
	lineage      evidenceLockFile
	rootReleased bool
	lineageHeld  bool
	done         bool
}

type evidenceGenerationLock struct {
	self *evidenceGenerationLock
	file evidenceLockFile
	held bool
	done bool
}

func validLockFile(file evidenceLockFile, kind evidenceLockKind) bool {
	return file.ops != nil && file.fd >= 0 && file.device != 0 && file.inode != 0 && file.kind == kind
}

func validDistinctLockFiles(a, b evidenceLockFile, aKind, bKind evidenceLockKind) bool {
	return validLockFile(a, aKind) && validLockFile(b, bKind) && a.ops == b.ops && a.fd != b.fd && a.device == b.device && a.inode != b.inode
}

func closeLineageFile(lineage evidenceLockFile) error {
	if lineage.ops != nil && lineage.fd >= 0 && lineage.ops.close(lineage.fd) != nil {
		return filesystemFailure("lock-cleanup", "lineage lock file close failed")
	}
	return nil
}

func failRootLineageAcquisition(root evidenceRootLeaseHandle, lineage evidenceLockFile, lineageHeld bool, op, message string) error {
	failed := false
	if lineageHeld {
		failed = lineage.ops.unlock(lineage.fd) != nil
	}
	failed = lineage.ops.close(lineage.fd) != nil || failed
	if root != nil {
		failed = root.Close() != nil || failed
	}
	if failed {
		return filesystemFailure("lock-cleanup", "lock acquisition cleanup failed")
	}
	return filesystemFailure(op, message)
}

// acquireRootThenTryLineage takes ownership of lineage.fd on every path. The
// evidencefs store remains the sole root-wide lock authority. There is no
// production caller in this slice: wiring must first prove that store and the
// legacy lineage descriptor are admitted from the same root identity.
func acquireRootThenTryLineage(ctx context.Context, store evidenceRootStore, lineage evidenceLockFile) (*evidenceLineageLock, error) {
	if store == nil || !validLockFile(lineage, evidenceLineageLockKind) {
		if cleanupErr := closeLineageFile(lineage); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, filesystemFailure("lock-order", "root store or lineage lock is not admissible")
	}
	for attempt := 0; attempt < evidenceLockRetryLimit; attempt++ {
		if err := evidenceContextError(ctx); err != nil {
			if cleanupErr := closeLineageFile(lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, err
		}
		root, err := store.AcquireRoot(ctx)
		if err != nil || root == nil || !root.Active() {
			cleanupFailed := false
			if root != nil {
				cleanupFailed = root.Close() != nil
			}
			cleanupFailed = closeLineageFile(lineage) != nil || cleanupFailed
			if cleanupFailed {
				return nil, filesystemFailure("lock-cleanup", "root admission cleanup failed")
			}
			return nil, mapEvidenceRootError(err, "evidence-root-lock")
		}
		lineageOK, lineageErr := lineage.ops.tryLock(lineage.fd)
		if lineageErr != nil {
			return nil, failRootLineageAcquisition(root, lineage, false, "lineage-lock", "lineage lock acquisition failed")
		}
		if lineageOK {
			handle := &evidenceLineageLock{root: root, lineage: lineage, lineageHeld: true}
			handle.self = handle
			return handle, nil
		}
		// Never retain the root-wide lease while a lineage is busy.
		if err := root.Close(); err != nil {
			if cleanupErr := closeLineageFile(lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, mapEvidenceRootError(err, "evidence-root-unlock")
		}
		if attempt+1 == evidenceLockRetryLimit {
			if cleanupErr := closeLineageFile(lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, filesystemFailure("lineage-lock", "lineage lock retry limit reached")
		}
		if err := evidenceLockBackoff(ctx, attempt); err != nil {
			if cleanupErr := closeLineageFile(lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, err
		}
	}
	panic("unreachable")
}

func (l *evidenceLineageLock) ReleaseRoot() error {
	if l == nil || l.self != l || l.done || l.root == nil || !l.lineageHeld {
		return filesystemFailure("root-lock", "root lock handle is not active")
	}
	if !l.root.Active() {
		// An inactive genuine root handle may represent a poisoned/unknown
		// evidencefs lease. It cannot be treated as released: poison this
		// composite and attempt every owned cleanup operation exactly once.
		l.done, l.rootReleased = true, true
		failed := l.lineage.ops.unlock(l.lineage.fd) != nil
		l.lineageHeld = false
		failed = l.lineage.ops.close(l.lineage.fd) != nil || failed
		failed = l.root.Close() != nil || failed
		if failed {
			return filesystemFailure("root-lock", "inactive root lock cleanup failed")
		}
		return filesystemFailure("root-lock", "root lock became inactive")
	}
	if err := l.root.Close(); err != nil {
		l.rootReleased = true
		l.done = true
		failed := l.lineage.ops.unlock(l.lineage.fd) != nil
		l.lineageHeld = false
		failed = l.lineage.ops.close(l.lineage.fd) != nil || failed
		if failed {
			return filesystemFailure("root-lock", "root lock release and cleanup failed")
		}
		return mapEvidenceRootError(err, "evidence-root-unlock")
	}
	l.rootReleased = true
	return nil
}

func (l *evidenceLineageLock) Close() error {
	if l == nil || l.self != l || l.done {
		return filesystemFailure("lock-close", "lineage lock handle is already closed")
	}
	l.done = true
	failed := false
	if l.lineageHeld {
		failed = l.lineage.ops.unlock(l.lineage.fd) != nil
		l.lineageHeld = false
	}
	failed = l.lineage.ops.close(l.lineage.fd) != nil || failed
	if l.root != nil && !l.rootReleased {
		failed = l.root.Close() != nil || failed
		l.rootReleased = true
	}
	if failed {
		return filesystemFailure("lock-close", "lineage or root lock release failed")
	}
	return nil
}

// acquireGenerationLock preserves the existing lineage->generation lock domain.
func acquireGenerationLock(ctx context.Context, lineage *evidenceLineageLock, generation evidenceLockFile) (*evidenceGenerationLock, error) {
	valid := lineage != nil && lineage.self == lineage && !lineage.done && lineage.lineageHeld && generation.ops != nil && validDistinctLockFiles(lineage.lineage, generation, evidenceLineageLockKind, evidenceGenerationLockKind)
	if !valid {
		if generation.ops != nil && generation.fd >= 0 && generation.ops.close(generation.fd) != nil {
			return nil, filesystemFailure("generation-lock", "invalid generation lock close failed")
		}
		return nil, filesystemFailure("generation-lock", "lineage lock must be held before a distinct generation lock")
	}
	for attempt := 0; attempt < evidenceLockRetryLimit; attempt++ {
		if err := evidenceContextError(ctx); err != nil {
			if generation.ops.close(generation.fd) != nil {
				return nil, filesystemFailure("generation-lock", "generation lock close failed")
			}
			return nil, err
		}
		ok, err := generation.ops.tryLock(generation.fd)
		if err != nil {
			if generation.ops.close(generation.fd) != nil {
				return nil, filesystemFailure("generation-lock", "generation lock cleanup failed")
			}
			return nil, filesystemFailure("generation-lock", "generation lock acquisition failed")
		}
		if ok {
			handle := &evidenceGenerationLock{file: generation, held: true}
			handle.self = handle
			return handle, nil
		}
		if err := evidenceLockBackoff(ctx, attempt); err != nil {
			if generation.ops.close(generation.fd) != nil {
				return nil, filesystemFailure("generation-lock", "generation lock close failed")
			}
			return nil, err
		}
	}
	if generation.ops.close(generation.fd) != nil {
		return nil, filesystemFailure("generation-lock", "generation lock close failed")
	}
	return nil, filesystemFailure("generation-lock", "generation lock retry limit reached")
}

func (l *evidenceGenerationLock) Close() error {
	if l == nil || l.self != l || l.done || !l.held {
		return filesystemFailure("generation-lock", "generation lock handle is already closed")
	}
	l.done, l.held = true, false
	failed := l.file.ops.unlock(l.file.fd) != nil
	failed = l.file.ops.close(l.file.fd) != nil || failed
	if failed {
		return filesystemFailure("generation-lock", "generation lock release or close failed")
	}
	return nil
}
