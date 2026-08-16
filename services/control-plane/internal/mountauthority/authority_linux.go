//go:build linux

package mountauthority

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const authorityResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS

type authorityMutationOps interface {
	create(int, string) (int, error)
	write(int, []byte) (int, error)
	fsync(int) error
	chmod(int, uint32) error
	close(int) error
	renameNoReplace(int, string, string) error
	unlink(int, string) error
}

type unixAuthorityMutationOps struct{}

func (unixAuthorityMutationOps) create(parent int, name string) (int, error) {
	return unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_CREAT | unix.O_EXCL, Mode: 0o600, Resolve: authorityResolveFlags})
}
func (unixAuthorityMutationOps) write(fd int, source []byte) (int, error) {
	return unix.Write(fd, source)
}
func (unixAuthorityMutationOps) fsync(fd int) error              { return unix.Fsync(fd) }
func (unixAuthorityMutationOps) chmod(fd int, mode uint32) error { return unix.Fchmod(fd, mode) }
func (unixAuthorityMutationOps) close(fd int) error              { return unix.Close(fd) }
func (unixAuthorityMutationOps) renameNoReplace(parent int, oldName, newName string) error {
	return unix.Renameat2(parent, oldName, parent, newName, unix.RENAME_NOREPLACE)
}
func (unixAuthorityMutationOps) unlink(parent int, name string) error {
	return unix.Unlinkat(parent, name, 0)
}

func Load(ctx context.Context, rootPath string, runnerUID uint32) (*Claim, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	pathDigest, err := RootPathDigest(rootPath)
	if err != nil {
		return nil, err
	}
	if err := currentRunnerIsUnprivileged(runnerUID); err != nil {
		return nil, err
	}
	basename, err := AuthorityBasename(rootPath)
	if err != nil {
		return nil, err
	}
	directoryFD, err := openAuthorityDirectory(false)
	if err != nil {
		return nil, err
	}
	initialDirectory, err := authorityDirectoryStat(directoryFD)
	if err != nil {
		_ = unix.Close(directoryFD)
		return nil, err
	}
	encoded, readErr := readAuthorityFile(directoryFD, basename)
	directoryCloseErr := unix.Close(directoryFD)
	if readErr != nil {
		return nil, readErr
	}
	if directoryCloseErr != nil {
		return nil, filesystem("authority-directory-close")
	}
	currentDirectoryFD, err := openAuthorityDirectory(false)
	if err != nil {
		return nil, err
	}
	currentDirectory, statErr := authorityDirectoryStat(currentDirectoryFD)
	currentDirectoryCloseErr := unix.Close(currentDirectoryFD)
	if statErr != nil || currentDirectoryCloseErr != nil {
		return nil, filesystem("authority-directory-recheck")
	}
	if !sameAuthorityDirectoryStat(initialDirectory, currentDirectory) {
		return nil, invalid("authority-directory-changed")
	}
	body, err := decodeAttestation(encoded)
	if err != nil || body.rootPath != pathDigest || body.runnerUID != runnerUID {
		return nil, invalid("attestation-binding")
	}
	bootID, namespaceDevice, namespaceInode, err := currentBootAndNamespace()
	if err != nil {
		return nil, err
	}
	if body.observed.BootID != bootID || body.observed.MountNamespaceDev != namespaceDevice || body.observed.MountNamespaceInode != namespaceInode {
		return nil, unavailable("attestation-environment")
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	return newClaim(body)
}

func Provision(ctx context.Context, request ProvisionRequest) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return ErrNotPrivileged
	}
	if !request.ConfirmDirectLocalMount || request.RunnerUID == 0 || !validRootPath(request.RootPath) {
		return ErrInvalid
	}
	rootFD, err := openNoFollowRoot(request.RootPath)
	if err != nil {
		return err
	}
	observed, observeErr := ObserveFD(ctx, rootFD, request.RootPath)
	rootCloseErr := unix.Close(rootFD)
	if observeErr != nil {
		return observeErr
	}
	if rootCloseErr != nil {
		return filesystem("root-close")
	}
	if observed.RootUID != request.RunnerUID {
		return invalid("runner-owner")
	}
	nonce, err := provisionNonce()
	if err != nil {
		return err
	}
	body := attestation{nonce: nonce, rootPath: observed.RootPathDigest, runnerUID: request.RunnerUID, observed: observed}
	encoded, err := encodeAttestation(body)
	if err != nil {
		return err
	}
	basename, err := AuthorityBasename(request.RootPath)
	if err != nil {
		return err
	}
	directoryFD, err := openAuthorityDirectory(true)
	if err != nil {
		return err
	}
	initialDirectory, err := authorityDirectoryStat(directoryFD)
	if err != nil {
		_ = unix.Close(directoryFD)
		return err
	}
	writeErr := writeAuthorityFile(ctx, directoryFD, basename, nonce, encoded)
	directoryCloseErr := unix.Close(directoryFD)
	if writeErr != nil {
		return writeErr
	}
	if directoryCloseErr != nil {
		return filesystem("authority-directory-close")
	}
	if err := verifyPublishedAuthority(initialDirectory, basename, encoded); err != nil {
		return err
	}
	return verifyProvisionedRoot(request.RootPath, observed)
}

func Revoke(ctx context.Context, rootPath string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return ErrNotPrivileged
	}
	basename, err := AuthorityBasename(rootPath)
	if err != nil {
		return err
	}
	directoryFD, err := openAuthorityDirectory(false)
	if err != nil {
		return err
	}
	initialDirectory, err := authorityDirectoryStat(directoryFD)
	if err != nil {
		_ = unix.Close(directoryFD)
		return err
	}
	if err := checkContext(ctx); err != nil {
		if unix.Close(directoryFD) != nil {
			return filesystem("authority-directory-close")
		}
		return err
	}
	mutationErr := revokeAuthorityEntry(ctx, unixAuthorityMutationOps{}, directoryFD, basename)
	closeErr := unix.Close(directoryFD)
	if mutationErr != nil {
		return mutationErr
	}
	if closeErr != nil {
		return filesystem("authority-revoke-sync")
	}
	return verifyRevokedAuthority(initialDirectory, basename)
}

func verifyPublishedAuthority(expectedDirectory unix.Stat_t, basename string, encoded []byte) error {
	directoryFD, err := openAuthorityDirectory(false)
	if err != nil {
		return err
	}
	currentDirectory, statErr := authorityDirectoryStat(directoryFD)
	observed, readErr := readAuthorityFile(directoryFD, basename)
	closeErr := unix.Close(directoryFD)
	if statErr != nil || readErr != nil || closeErr != nil {
		return filesystem("authority-publish-recheck")
	}
	if !sameAuthorityDirectoryStat(expectedDirectory, currentDirectory) || !bytes.Equal(observed, encoded) {
		return invalid("authority-publish-changed")
	}
	return nil
}

func verifyRevokedAuthority(expectedDirectory unix.Stat_t, basename string) error {
	directoryFD, err := openAuthorityDirectory(false)
	if err != nil {
		return err
	}
	currentDirectory, statErr := authorityDirectoryStat(directoryFD)
	var linked unix.Stat_t
	entryErr := unix.Fstatat(directoryFD, basename, &linked, unix.AT_SYMLINK_NOFOLLOW)
	closeErr := unix.Close(directoryFD)
	if statErr != nil || closeErr != nil {
		return filesystem("authority-revoke-recheck")
	}
	if !sameAuthorityDirectoryStat(expectedDirectory, currentDirectory) || !errors.Is(entryErr, unix.ENOENT) {
		return invalid("authority-revoke-changed")
	}
	return nil
}

func verifyProvisionedRoot(rootPath string, expected Observation) error {
	rootFD, err := openNoFollowRoot(rootPath)
	if err != nil {
		return err
	}
	observed, observeErr := ObserveFD(context.Background(), rootFD, rootPath)
	closeErr := unix.Close(rootFD)
	if observeErr != nil || closeErr != nil {
		return filesystem("root-recheck")
	}
	if observed != expected {
		return invalid("root-changed")
	}
	return nil
}

func openNoFollowRoot(rootPath string) (int, error) {
	if !validRootPath(rootPath) {
		return -1, ErrInvalid
	}
	anchor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, filesystem("root-anchor")
	}
	relative := strings.TrimPrefix(rootPath, "/")
	rootFD, openErr := unix.Openat2(anchor, relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: authorityResolveFlags})
	anchorCloseErr := unix.Close(anchor)
	if openErr != nil {
		return -1, filesystem("root-open")
	}
	if anchorCloseErr != nil {
		_ = unix.Close(rootFD)
		return -1, filesystem("root-anchor-close")
	}
	return rootFD, nil
}

func openAuthorityDirectory(create bool) (int, error) {
	anchor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, filesystem("authority-root-open")
	}
	if err := validateAuthorityDirectoryFD(anchor); err != nil {
		_ = unix.Close(anchor)
		return -1, err
	}
	current := anchor
	components := []string{"run", "cloud-agents", "evidencefs-mounts"}
	for index, component := range components {
		child, openErr := unix.Openat2(current, component, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: authorityResolveFlags})
		if errors.Is(openErr, unix.ENOENT) && create && index > 0 {
			mode := uint32(0o755)
			mkdirErr := unix.Mkdirat(current, component, mode)
			if mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(current)
				return -1, filesystem("authority-directory-create")
			}
			if mkdirErr == nil && unix.Fsync(current) != nil {
				_ = unix.Close(current)
				return -1, filesystem("authority-directory-create-sync")
			}
			child, openErr = unix.Openat2(current, component, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: authorityResolveFlags})
		}
		if openErr != nil {
			_ = unix.Close(current)
			if errors.Is(openErr, unix.ENOENT) {
				return -1, unavailable("authority-directory")
			}
			return -1, filesystem("authority-directory-open")
		}
		validationErr := validateAuthorityDirectoryFD(child)
		parentCloseErr := unix.Close(current)
		if validationErr != nil {
			_ = unix.Close(child)
			return -1, validationErr
		}
		if parentCloseErr != nil {
			_ = unix.Close(child)
			return -1, filesystem("authority-directory-parent-close")
		}
		current = child
	}
	return current, nil
}

func validateAuthorityDirectoryFD(fd int) error {
	stat, err := authorityDirectoryStat(fd)
	if err != nil {
		return err
	}
	mode := uint32(stat.Mode & 0o7777)
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Dev == 0 || stat.Ino == 0 || stat.Uid != 0 || mode&0o022 != 0 {
		return invalid("authority-directory-metadata")
	}
	return nil
}

func authorityDirectoryStat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return unix.Stat_t{}, filesystem("authority-directory-stat")
	}
	return stat, nil
}

func sameAuthorityDirectoryStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid
}

func readAuthorityFile(directoryFD int, basename string) ([]byte, error) {
	var before unix.Stat_t
	if err := unix.Fstatat(directoryFD, basename, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, unavailable("authority-file")
		}
		return nil, filesystem("authority-file-lstat")
	}
	if !validAuthorityFileStat(before) {
		return nil, invalid("authority-file-metadata")
	}
	fd, err := unix.Openat2(directoryFD, basename, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: authorityResolveFlags})
	if err != nil {
		return nil, filesystem("authority-file-open")
	}
	var opened unix.Stat_t
	statErr := unix.Fstat(fd, &opened)
	if statErr != nil || !sameAuthorityFileStat(before, opened) || !validAuthorityFileStat(opened) {
		_ = unix.Close(fd)
		return nil, invalid("authority-file-identity")
	}
	encoded := make([]byte, authoritySize)
	offset := 0
	for offset < len(encoded) {
		n, readErr := unix.Pread(fd, encoded[offset:], int64(offset))
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = unix.Close(fd)
			return nil, filesystem("authority-file-read")
		}
		if n <= 0 {
			_ = unix.Close(fd)
			return nil, invalid("authority-file-short")
		}
		offset += n
	}
	var after unix.Stat_t
	afterErr := unix.Fstat(fd, &after)
	closeErr := unix.Close(fd)
	var directoryAfter unix.Stat_t
	directoryErr := unix.Fstat(directoryFD, &directoryAfter)
	if afterErr != nil || closeErr != nil || directoryErr != nil {
		return nil, filesystem("authority-file-close")
	}
	if !sameAuthorityFileStat(opened, after) || !validAuthorityFileStat(after) {
		return nil, invalid("authority-file-changed")
	}
	var linked unix.Stat_t
	if err := unix.Fstatat(directoryFD, basename, &linked, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameAuthorityFileStat(after, linked) || !validAuthorityFileStat(linked) {
		return nil, invalid("authority-file-entry-changed")
	}
	if err := validateAuthorityDirectoryFD(directoryFD); err != nil {
		return nil, err
	}
	return encoded, nil
}

func validAuthorityFileStat(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Dev != 0 && stat.Ino != 0 && stat.Uid == 0 && stat.Nlink == 1 && stat.Size == authoritySize && stat.Mode&0o222 == 0
}

func sameAuthorityFileStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func writeAuthorityFile(ctx context.Context, directoryFD int, basename string, nonce [16]byte, encoded []byte) error {
	return writeAuthorityFileWithOps(ctx, unixAuthorityMutationOps{}, directoryFD, basename, nonce, encoded)
}

func writeAuthorityFileWithOps(ctx context.Context, ops authorityMutationOps, directoryFD int, basename string, nonce [16]byte, encoded []byte) error {
	if ops == nil || len(encoded) != authoritySize || validZeroNonce(nonce) {
		return ErrInvalid
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	temporary := ".tmp-" + hex.EncodeToString(nonce[:])
	fd, err := ops.create(directoryFD, temporary)
	if err != nil {
		return filesystem("authority-temp-create")
	}
	cleanup := func() {
		_ = ops.close(fd)
		_ = ops.unlink(directoryFD, temporary)
		_ = ops.fsync(directoryFD)
	}
	if err := checkContext(ctx); err != nil {
		cleanup()
		return err
	}
	offset := 0
	for offset < len(encoded) {
		n, writeErr := ops.write(fd, encoded[offset:])
		if writeErr != nil || n <= 0 {
			cleanup()
			return filesystem("authority-temp-write")
		}
		offset += n
	}
	if ops.fsync(fd) != nil || ops.chmod(fd, 0o444) != nil || ops.fsync(fd) != nil {
		cleanup()
		return filesystem("authority-temp-sync")
	}
	if closeErr := ops.close(fd); closeErr != nil {
		fd = -1
		_ = ops.unlink(directoryFD, temporary)
		_ = ops.fsync(directoryFD)
		return filesystem("authority-temp-close")
	}
	fd = -1
	if err := checkContext(ctx); err != nil {
		_ = ops.unlink(directoryFD, temporary)
		_ = ops.fsync(directoryFD)
		return err
	}
	renameErr := ops.renameNoReplace(directoryFD, temporary, basename)
	if renameErr != nil {
		_ = ops.unlink(directoryFD, temporary)
		_ = ops.fsync(directoryFD)
		if errors.Is(renameErr, unix.EEXIST) {
			return ErrConflict
		}
		return filesystem("authority-publish")
	}
	if ops.fsync(directoryFD) != nil {
		return filesystem("authority-publish-sync")
	}
	return nil
}

func revokeAuthorityEntry(ctx context.Context, ops authorityMutationOps, directoryFD int, basename string) error {
	if ops == nil || directoryFD < 0 || basename == "" {
		return ErrInvalid
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	unlinkErr := ops.unlink(directoryFD, basename)
	if errors.Is(unlinkErr, unix.ENOENT) {
		return unavailable("authority-file")
	}
	fsyncErr := ops.fsync(directoryFD)
	if unlinkErr != nil || fsyncErr != nil {
		return filesystem("authority-revoke-sync")
	}
	return nil
}

func provisionNonce() ([16]byte, error) {
	var nonce [16]byte
	offset := 0
	for offset < len(nonce) {
		n, err := unix.Getrandom(nonce[offset:], 0)
		if err != nil || n <= 0 {
			return [16]byte{}, filesystem("authority-random")
		}
		offset += n
	}
	if validZeroNonce(nonce) {
		return [16]byte{}, filesystem("authority-random-zero")
	}
	return nonce, nil
}

func currentBootAndNamespace() ([16]byte, uint64, uint64, error) {
	raw, err := readBootID()
	if err != nil {
		return [16]byte{}, 0, 0, filesystem("boot-id-read")
	}
	bootID, err := parseBootID(raw)
	if err != nil {
		return [16]byte{}, 0, 0, invalid("boot-id")
	}
	var namespace unix.Stat_t
	if err := unix.Stat("/proc/self/ns/mnt", &namespace); err != nil || namespace.Dev == 0 || namespace.Ino == 0 {
		return [16]byte{}, 0, 0, filesystem("mount-namespace")
	}
	return bootID, uint64(namespace.Dev), namespace.Ino, nil
}
