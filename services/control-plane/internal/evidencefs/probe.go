package evidencefs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
)

const (
	requiredProbeNameAttempts = 8
	requiredProbePayload      = "cloud-agents-evidencefs-required-syscall-probe-v1"
)

// probeRequiredSyscalls holds the existing root-wide lease for the complete
// probe. A clean context failure before the first temporary-file mutation may
// be retried. Every capability or post-mutation failure poisons the root.
func (r *Root) probeRequiredSyscalls(ctx context.Context) (resultErr error) {
	if err := contextError(ctx); err != nil {
		return err
	}
	lease, err := r.Acquire(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := lease.Close(); err != nil {
			r.poison()
			resultErr = filesystem("required-probe-lease-close")
		}
	}()
	resultErr = lease.probeRequiredSyscalls(ctx)
	if resultErr != nil && !errors.Is(resultErr, context.Canceled) && !errors.Is(resultErr, context.DeadlineExceeded) {
		r.poison()
	}
	return resultErr
}

func (l *Lease) probeRequiredSyscalls(ctx context.Context) (resultErr error) {
	if l == nil {
		return ErrLeaseInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := contextError(ctx); err != nil {
		return err
	}
	if !l.active() {
		return ErrLeaseInvalid
	}

	closeFD := func(fd *int, operation string) {
		if fd == nil || *fd < 0 {
			return
		}
		current := *fd
		*fd = -1
		if l.root.ops.close(current) != nil {
			l.root.poison()
			l.invalidate()
			resultErr = filesystem(operation)
		}
	}

	objectsFD, _, err := l.root.openVerifiedDirectory(l.rootFD, "objects")
	if err != nil {
		return err
	}
	defer closeFD(&objectsFD, "required-probe-objects-close")
	names, err := l.root.ops.readDirNames(objectsFD, 2)
	if err != nil || len(names) != 1 || names[0] != "sha256" {
		return filesystem("required-probe-objects-grammar")
	}
	shaFD, _, err := l.root.openVerifiedDirectory(objectsFD, "sha256")
	if err != nil {
		return err
	}
	defer closeFD(&shaFD, "required-probe-sha256-close")

	contenderFD, contenderStat, err := l.root.openVerifiedRegular(l.rootFD, "lineages.lock")
	if err != nil || !sameIdentity(contenderStat, l.lock) {
		if contenderFD >= 0 {
			closeFD(&contenderFD, "required-probe-contender-close")
		}
		return filesystem("required-probe-lock-identity")
	}
	defer closeFD(&contenderFD, "required-probe-contender-close")
	contenderLocked, err := l.root.ops.tryLock(contenderFD)
	if err != nil {
		return filesystem("required-probe-lock")
	}
	if contenderLocked {
		if l.root.ops.unlock(contenderFD) != nil {
			l.root.poison()
			l.invalidate()
			return filesystem("required-probe-lock-release")
		}
		return filesystem("required-probe-lock-exclusion")
	}
	if err := contextError(ctx); err != nil {
		return err
	}

	selected := make(map[string]bool, 3)
	probeNames := make([]string, 3)
	for index := range probeNames {
		probeNames[index], err = l.newRequiredProbeTempName(shaFD, selected)
		if err != nil {
			return err
		}
		selected[probeNames[index]] = true
	}

	owned := make(map[string]bool, 3)
	mutated := false
	defer func() {
		if !mutated {
			return
		}
		if cleanupErr := l.cleanupRequiredProbeTemps(shaFD, owned); cleanupErr != nil {
			l.root.poison()
			l.invalidate()
			resultErr = cleanupErr
		}
	}()

	payloadA := []byte(requiredProbePayload + "/a")
	payloadC := []byte(requiredProbePayload + "/c")
	statA, created, err := l.createRequiredProbeTemp(ctx, shaFD, probeNames[0], payloadA)
	if created {
		mutated, owned[probeNames[0]] = true, true
	}
	if err != nil {
		return filesystem("required-probe-source-create")
	}
	if l.root.ops.fsync(shaFD) != nil {
		return filesystem("required-probe-source-directory-sync")
	}
	if err := l.root.ops.renameNoReplace(shaFD, probeNames[0], probeNames[1]); err != nil {
		// The destination was proven absent while the root lock was held. If a
		// backend reports an error after performing the rename, either name may
		// still contain our exact O_EXCL-created inode and must be drained.
		owned[probeNames[1]] = true
		return filesystem("required-probe-rename")
	}
	owned[probeNames[1]] = true
	if _, err := l.root.ops.lstatAt(shaFD, probeNames[0]); err == nil || !l.root.ops.isNotExist(err) {
		return filesystem("required-probe-rename-source")
	}
	delete(owned, probeNames[0])
	if l.root.ops.fsync(shaFD) != nil {
		return filesystem("required-probe-rename-directory-sync")
	}
	if err := l.verifyRequiredProbeTemp(ctx, shaFD, probeNames[1], statA, payloadA); err != nil {
		return err
	}

	statC, created, err := l.createRequiredProbeTemp(ctx, shaFD, probeNames[2], payloadC)
	if created {
		mutated, owned[probeNames[2]] = true, true
	}
	if err != nil {
		return filesystem("required-probe-conflict-source-create")
	}
	if l.root.ops.fsync(shaFD) != nil {
		return filesystem("required-probe-conflict-directory-sync")
	}
	conflictErr := l.root.ops.renameNoReplace(shaFD, probeNames[2], probeNames[1])
	if conflictErr == nil || !l.root.ops.isExist(conflictErr) {
		if conflictErr == nil {
			delete(owned, probeNames[2])
			owned[probeNames[1]] = true
		} else {
			// A non-conflict error cannot prove whether the source moved. Both
			// names were already owned by this probe under the root-wide lock.
			owned[probeNames[1]], owned[probeNames[2]] = true, true
		}
		return filesystem("required-probe-no-replace")
	}
	if err := l.verifyRequiredProbeTemp(ctx, shaFD, probeNames[1], statA, payloadA); err != nil {
		return err
	}
	if err := l.verifyRequiredProbeTemp(ctx, shaFD, probeNames[2], statC, payloadC); err != nil {
		return err
	}
	return nil
}

func (l *Lease) newRequiredProbeTempName(shaFD int, selected map[string]bool) (string, error) {
	for attempt := 0; attempt < requiredProbeNameAttempts; attempt++ {
		nonce := make([]byte, 16)
		if err := l.root.ops.random(nonce); err != nil {
			return "", filesystem("required-probe-random")
		}
		name := ".tmp-" + hex.EncodeToString(nonce)
		if selected[name] || !tempNamePattern.MatchString(name) {
			continue
		}
		if _, err := l.root.ops.lstatAt(shaFD, name); err == nil {
			continue
		} else if !l.root.ops.isNotExist(err) {
			return "", filesystem("required-probe-name")
		}
		return name, nil
	}
	return "", filesystem("required-probe-name-exhausted")
}

func (l *Lease) createRequiredProbeTemp(ctx context.Context, shaFD int, name string, payload []byte) (result fileStat, created bool, resultErr error) {
	fd, err := l.root.ops.openFileAt(shaFD, name, true)
	if err != nil {
		return fileStat{}, false, filesystem("required-probe-file-create")
	}
	created = true
	defer func() {
		if l.root.ops.close(fd) != nil {
			result, resultErr = fileStat{}, filesystem("required-probe-file-close")
		}
	}()
	initial, err := l.root.ops.fstat(fd)
	if err != nil || !validRegular(initial, l.root.uid, l.root.identity.device) || initial.size != 0 {
		return fileStat{}, created, filesystem("required-probe-file-identity")
	}
	if err := writeAll(ctx, l.root.ops, fd, payload); err != nil {
		return fileStat{}, created, err
	}
	result, err = l.root.ops.fstat(fd)
	if err != nil || !validRegular(result, l.root.uid, l.root.identity.device) || result.size != uint64(len(payload)) || !sameNodeIdentity(initial, result) {
		return fileStat{}, created, filesystem("required-probe-file-size")
	}
	if l.root.ops.fdatasync(fd) != nil {
		return fileStat{}, created, filesystem("required-probe-file-datasync")
	}
	return result, created, nil
}

func (l *Lease) verifyRequiredProbeTemp(ctx context.Context, shaFD int, name string, expected fileStat, payload []byte) (resultErr error) {
	fd, observed, err := l.root.openVerifiedRegular(shaFD, name)
	if err != nil {
		return err
	}
	defer func() {
		if l.root.ops.close(fd) != nil {
			resultErr = filesystem("required-probe-verify-close")
		}
	}()
	if !sameIdentity(observed, expected) {
		return filesystem("required-probe-verify-identity")
	}
	digest, err := l.fullHash(ctx, fd, observed)
	if err != nil || digest != sha256.Sum256(payload) {
		return filesystem("required-probe-verify-content")
	}
	return nil
}

func (l *Lease) cleanupRequiredProbeTemps(shaFD int, owned map[string]bool) error {
	names := make([]string, 0, len(owned))
	for name, isOwned := range owned {
		if isOwned {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	failed := false
	for _, name := range names {
		if err := l.root.ops.unlinkAt(shaFD, name); err != nil && !l.root.ops.isNotExist(err) {
			failed = true
		}
	}
	failed = l.root.ops.fsync(shaFD) != nil || failed
	if failed {
		return filesystem("required-probe-cleanup")
	}
	return nil
}
