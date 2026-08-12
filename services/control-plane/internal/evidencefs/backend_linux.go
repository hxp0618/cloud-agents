//go:build linux

package evidencefs

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	rootResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS
	resolveFlags     = rootResolveFlags | unix.RESOLVE_NO_XDEV
)

const (
	linuxExistingOpenFlags = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOFOLLOW
	linuxCreateOpenFlags   = unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_CREAT | unix.O_EXCL
)

// linuxBackend is package-private and presently unreachable from Open: syscall
// mechanics do not mint trusted mount authority.
type linuxBackend struct{}

func (linuxBackend) openRoot(path string) (int, error) {
	anchor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	relative := strings.TrimPrefix(path, "/")
	if relative == "" {
		_ = unix.Close(anchor)
		return -1, unix.EINVAL
	}
	fd, openErr := unix.Openat2(anchor, relative, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: rootResolveFlags})
	if closeErr := unix.Close(anchor); closeErr != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		return -1, closeErr
	}
	return fd, openErr
}

func (linuxBackend) openDirAt(parent int, name string) (int, error) {
	return unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: resolveFlags})
}

func (linuxBackend) lstatAt(parent int, name string) (fileStat, error) {
	var raw unix.Stat_t
	if err := unix.Fstatat(parent, name, &raw, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fileStat{}, err
	}
	return linuxFileStat(raw)
}

func (linuxBackend) openFileAt(parent int, name string, create bool) (int, error) {
	flags := uint64(linuxExistingOpenFlags)
	mode := uint64(0)
	if create {
		flags = linuxCreateOpenFlags
		mode = uint64(fileMode)
	}
	return unix.Openat2(parent, name, &unix.OpenHow{Flags: flags, Mode: mode, Resolve: resolveFlags})
}

func (linuxBackend) fstat(fd int) (fileStat, error) {
	var raw unix.Stat_t
	if err := unix.Fstat(fd, &raw); err != nil {
		return fileStat{}, err
	}
	return linuxFileStat(raw)
}

func linuxFileStat(raw unix.Stat_t) (fileStat, error) {
	if raw.Size < 0 {
		return fileStat{}, unix.EOVERFLOW
	}
	kind := kindUnknown
	switch raw.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		kind = kindDirectory
	case unix.S_IFREG:
		kind = kindRegular
	}
	return fileStat{device: uint64(raw.Dev), inode: raw.Ino, size: uint64(raw.Size), mode: raw.Mode & 0o7777, uid: raw.Uid, nlink: uint64(raw.Nlink), kind: kind}, nil
}

func (linuxBackend) readDirNames(fd int, maximum int) ([]string, error) {
	if maximum <= 0 {
		return nil, unix.EINVAL
	}
	buffer := make([]byte, 32<<10)
	names := make([]string, 0, min(maximum, 64))
	for {
		n, err := unix.ReadDirent(fd, buffer)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return names, nil
		}
		_, _, names = unix.ParseDirent(buffer[:n], maximum+1-len(names), names)
		if len(names) > maximum {
			return nil, unix.EOVERFLOW
		}
	}
}
func (linuxBackend) isOverflow(err error) bool { return errors.Is(err, unix.EOVERFLOW) }

func (linuxBackend) pread(fd int, target []byte, offset int64) (int, error) {
	return unix.Pread(fd, target, offset)
}
func (linuxBackend) write(fd int, source []byte) (int, error) { return unix.Write(fd, source) }
func (linuxBackend) fdatasync(fd int) error                   { return unix.Fdatasync(fd) }
func (linuxBackend) fsync(fd int) error                       { return unix.Fsync(fd) }
func (linuxBackend) renameNoReplace(parent int, oldName, newName string) error {
	return unix.Renameat2(parent, oldName, parent, newName, unix.RENAME_NOREPLACE)
}
func (linuxBackend) unlinkAt(parent int, name string) error {
	return unix.Unlinkat(parent, name, 0)
}
func (linuxBackend) isExist(err error) bool { return errors.Is(err, unix.EEXIST) }
func (linuxBackend) tryLock(fd int) (bool, error) {
	err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
func (linuxBackend) unlock(fd int) error { return unix.Flock(fd, unix.LOCK_UN) }
func (linuxBackend) close(fd int) error  { return unix.Close(fd) }
func (linuxBackend) random(target []byte) error {
	offset := 0
	for offset < len(target) {
		n, err := unix.Getrandom(target[offset:], 0)
		if err != nil {
			return err
		}
		if n <= 0 {
			return unix.EIO
		}
		offset += n
	}
	return nil
}
