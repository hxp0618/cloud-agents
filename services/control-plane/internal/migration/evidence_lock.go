package migration

import "context"

const evidenceLockRetryLimit = 64

type evidenceLockKind uint8

const (
	evidenceRootLockKind evidenceLockKind = iota + 1
	evidenceLineageLockKind
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
	if root == nil || root.ops == nil || fd < 0 || (kind != evidenceRootLockKind && kind != evidenceLineageLockKind && kind != evidenceGenerationLockKind) {
		if root != nil && root.ops != nil && fd >= 0 {
			if root.ops.close(fd) != nil {
				return evidenceLockFile{}, filesystemFailure("lock-file", "invalid lock file close failed")
			}
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

// evidenceLineageLock is the only root->lineage lock-order authority. flock is
// the single protocol used for every evidence filesystem lock; mixing fcntl or
// process-local mutexes would create disjoint lock domains.
type evidenceLineageLock struct {
	root        evidenceLockFile
	lineage     evidenceLockFile
	rootHeld    bool
	lineageHeld bool
	done        bool
}

type evidenceGenerationLock struct {
	file evidenceLockFile
	held bool
	done bool
}

func validDistinctLockFiles(a, b evidenceLockFile, aKind, bKind evidenceLockKind) bool {
	return a.ops != nil && a.ops == b.ops && a.fd >= 0 && b.fd >= 0 && a.fd != b.fd && a.device != 0 && a.device == b.device && a.inode != 0 && b.inode != 0 && a.inode != b.inode && a.kind == aKind && b.kind == bKind
}

func closeUnacquiredLocks(root, lineage evidenceLockFile) error {
	failed := false
	if lineage.ops != nil && lineage.fd >= 0 {
		failed = lineage.ops.close(lineage.fd) != nil
	}
	if root.ops != nil && root.fd >= 0 && (root.ops != lineage.ops || root.fd != lineage.fd) {
		failed = root.ops.close(root.fd) != nil || failed
	}
	if failed {
		return filesystemFailure("lock-cleanup", "unacquired lock file close failed")
	}
	return nil
}

func failRootLineageAcquisition(root, lineage evidenceLockFile, rootHeld, lineageHeld bool, op, message string) error {
	failed := false
	if lineageHeld {
		failed = root.ops.unlock(lineage.fd) != nil
	}
	if rootHeld {
		failed = root.ops.unlock(root.fd) != nil || failed
	}
	failed = root.ops.close(lineage.fd) != nil || failed
	failed = root.ops.close(root.fd) != nil || failed
	if failed {
		return filesystemFailure("lock-cleanup", "lock acquisition cleanup failed")
	}
	return filesystemFailure(op, message)
}

// acquireRootThenTryLineage takes ownership of both descriptors on every path.
func acquireRootThenTryLineage(ctx context.Context, root, lineage evidenceLockFile) (*evidenceLineageLock, error) {
	if !validDistinctLockFiles(root, lineage, evidenceRootLockKind, evidenceLineageLockKind) {
		if cleanupErr := closeUnacquiredLocks(root, lineage); cleanupErr != nil {
			return nil, cleanupErr
		}
		return nil, filesystemFailure("lock-order", "lock files are not admissible")
	}
	lineageAttempts := 0
	totalAttempts := 0
	for {
		if err := evidenceContextError(ctx); err != nil {
			if cleanupErr := closeUnacquiredLocks(root, lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, err
		}
		rootOK, err := root.ops.tryLock(root.fd)
		totalAttempts++
		if err != nil {
			return nil, failRootLineageAcquisition(root, lineage, false, false, "root-lock", "root lock acquisition failed")
		}
		if !rootOK {
			if totalAttempts >= evidenceLockRetryLimit {
				return nil, failRootLineageAcquisition(root, lineage, false, false, "root-lock", "root lock retry limit reached")
			}
			if err := evidenceLockBackoff(ctx, totalAttempts-1); err != nil {
				if cleanupErr := closeUnacquiredLocks(root, lineage); cleanupErr != nil {
					return nil, cleanupErr
				}
				return nil, err
			}
			continue
		}
		lineageOK, lineageErr := lineage.ops.tryLock(lineage.fd)
		if lineageErr != nil {
			return nil, failRootLineageAcquisition(root, lineage, true, false, "lineage-lock", "lineage lock acquisition failed")
		}
		if lineageOK {
			return &evidenceLineageLock{root: root, lineage: lineage, rootHeld: true, lineageHeld: true}, nil
		}
		// Never hold the root-wide lock while a lineage is busy.
		if root.ops.unlock(root.fd) != nil {
			// The failed unlock means the descriptor may still own the lock. Keep
			// that state explicit so cleanup retries unlock before close.
			return nil, failRootLineageAcquisition(root, lineage, true, false, "root-lock", "root lock release failed")
		}
		lineageAttempts++
		if lineageAttempts >= evidenceLockRetryLimit {
			return nil, failRootLineageAcquisition(root, lineage, false, false, "lineage-lock", "lineage lock retry limit reached")
		}
		if totalAttempts >= evidenceLockRetryLimit {
			return nil, failRootLineageAcquisition(root, lineage, false, false, "root-lock", "combined lock retry limit reached")
		}
		if err := evidenceLockBackoff(ctx, totalAttempts-1); err != nil {
			if cleanupErr := closeUnacquiredLocks(root, lineage); cleanupErr != nil {
				return nil, cleanupErr
			}
			return nil, err
		}
	}
}

func (l *evidenceLineageLock) ReleaseRoot() error {
	if l == nil || l.done || !l.rootHeld || !l.lineageHeld {
		return filesystemFailure("root-lock", "root lock handle is not active")
	}
	if l.root.ops.unlock(l.root.fd) != nil {
		l.done = true
		cleanupFailed := l.lineage.ops.unlock(l.lineage.fd) != nil
		// Retry the root unlock: the first failure must not be treated as proof
		// that the lock is no longer held.
		cleanupFailed = l.root.ops.unlock(l.root.fd) != nil || cleanupFailed
		cleanupFailed = l.lineage.ops.close(l.lineage.fd) != nil || cleanupFailed
		cleanupFailed = l.root.ops.close(l.root.fd) != nil || cleanupFailed
		if cleanupFailed {
			return filesystemFailure("root-lock", "root lock release and cleanup failed")
		}
		return filesystemFailure("root-lock", "root lock release failed")
	}
	l.rootHeld = false
	return nil
}

func (l *evidenceLineageLock) Close() error {
	if l == nil || l.done {
		return filesystemFailure("lock-close", "lineage lock handle is already closed")
	}
	l.done = true
	failed := false
	if l.lineageHeld {
		failed = l.lineage.ops.unlock(l.lineage.fd) != nil
		l.lineageHeld = false
	}
	if l.rootHeld {
		failed = l.root.ops.unlock(l.root.fd) != nil || failed
		l.rootHeld = false
	}
	failed = l.lineage.ops.close(l.lineage.fd) != nil || failed
	failed = l.root.ops.close(l.root.fd) != nil || failed
	if failed {
		return filesystemFailure("lock-close", "lineage lock release or close failed")
	}
	return nil
}

// acquireGenerationLock takes ownership of generation.fd on every path.
func acquireGenerationLock(ctx context.Context, lineage *evidenceLineageLock, generation evidenceLockFile) (*evidenceGenerationLock, error) {
	valid := lineage != nil && !lineage.done && lineage.lineageHeld && generation.ops != nil && validDistinctLockFiles(lineage.lineage, generation, evidenceLineageLockKind, evidenceGenerationLockKind)
	if !valid {
		if generation.ops != nil && generation.fd >= 0 {
			if generation.ops.close(generation.fd) != nil {
				return nil, filesystemFailure("generation-lock", "invalid generation lock close failed")
			}
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
			return &evidenceGenerationLock{file: generation, held: true}, nil
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
	if l == nil || l.done || !l.held {
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
