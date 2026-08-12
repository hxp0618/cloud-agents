//go:build linux

package migration

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	evidenceResolveFlags = unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV
)

type linuxEvidenceFSOps struct{}

var readLinuxMountInfo = func() ([]byte, error) { return os.ReadFile("/proc/self/mountinfo") }

func newProductionEvidenceFSRoot(ctx context.Context, rootPath string) (*evidenceFSRoot, error) {
	// No current kernel-derived proof distinguishes a direct local filesystem
	// mount from a whole-filesystem bind/clone in every namespace. Until trusted
	// provisioning supplies that external mount authority, production admission
	// remains fail closed before any filesystem mutation.
	_ = ctx
	_ = rootPath
	return nil, filesystemFailure("filesystem", "trusted production mount authority is not implemented")
}

func supportedEvidenceFilesystem(fsType int64) bool {
	return fsType == unix.EXT4_SUPER_MAGIC || fsType == unix.XFS_SUPER_MAGIC
}

func (linuxEvidenceFSOps) verifyMount(fd int, fsType int64, st evidenceFileStat) error {
	if !supportedEvidenceFilesystem(fsType) || st.device == 0 {
		return unix.ENOTSUP
	}
	_, mountID, err := unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH)
	if err != nil || mountID <= 0 {
		return unix.ENOTSUP
	}
	raw, err := readLinuxMountInfo()
	if err != nil {
		return unix.ENOTSUP
	}
	return verifyLinuxMountInfo(raw, mountID, fsType, st.device)
}

// verifyLinuxMountInfo is a lower-level consistency matcher only. Passing it
// does not create production mount authority: Linux can expose a whole-
// filesystem bind/clone with the same root/source/device facts and no stable
// bind option. newProductionEvidenceFSRoot therefore remains fail closed.
func verifyLinuxMountInfo(raw []byte, mountID int, fsType int64, device uint64) error {
	if mountID <= 0 {
		return unix.ENOTSUP
	}
	entry, entries, err := parseLinuxMountInfo(raw, mountID)
	if err != nil {
		return unix.ENOTSUP
	}
	expectedType := "ext4"
	if fsType == unix.XFS_SUPER_MAGIC {
		expectedType = "xfs"
	}
	if entry.mountPoint != "/" || entry.fsType != expectedType || !strings.HasPrefix(entry.source, "/dev/") || entry.source == "/dev/root" || entry.root != "/" || containsMountOption(entry.mountOptions, "bind") || containsMountOption(entry.mountOptions, "rbind") || containsMountOption(entry.superOptions, "bind") {
		return unix.ENOTSUP
	}
	expectedDevice := fmt.Sprintf("%d:%d", unix.Major(device), unix.Minor(device))
	if entry.majorMinor != expectedDevice {
		return unix.ENOTSUP
	}
	backingMatches := 0
	for _, candidate := range entries {
		if candidate.majorMinor == entry.majorMinor && candidate.root == entry.root && candidate.fsType == entry.fsType && candidate.source == entry.source {
			backingMatches++
		}
	}
	if backingMatches != 1 {
		return unix.ENOTSUP
	}
	return nil
}

type linuxMountInfoEntry struct {
	id           int
	majorMinor   string
	root         string
	mountPoint   string
	mountOptions string
	fsType       string
	source       string
	superOptions string
}

func uniqueLinuxMountInfo(raw []byte, mountID int) (linuxMountInfoEntry, error) {
	found, _, err := parseLinuxMountInfo(raw, mountID)
	return found, err
}

func parseLinuxMountInfo(raw []byte, mountID int) (linuxMountInfoEntry, []linuxMountInfoEntry, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var found linuxMountInfoEntry
	var entries []linuxMountInfoEntry
	matches := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) {
			return linuxMountInfoEntry{}, nil, errors.New("invalid mountinfo")
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			return linuxMountInfoEntry{}, nil, errors.New("invalid mount id")
		}
		if _, _, ok := strings.Cut(fields[2], ":"); !ok {
			return linuxMountInfoEntry{}, nil, errors.New("invalid device identity")
		}
		entry := linuxMountInfoEntry{id: id, majorMinor: fields[2], root: fields[3], mountPoint: fields[4], mountOptions: fields[5], fsType: fields[separator+1], source: fields[separator+2], superOptions: fields[separator+3]}
		entries = append(entries, entry)
		if id == mountID {
			matches++
			found = entry
		}
	}
	if scanner.Err() != nil || matches != 1 {
		return linuxMountInfoEntry{}, nil, fmt.Errorf("mount match count")
	}
	return found, entries, nil
}

func containsMountOption(options, target string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == target {
			return true
		}
	}
	return false
}

func (linuxEvidenceFSOps) openRoot(path string) (int, error) {
	anchor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(anchor)
	relative := strings.TrimPrefix(path, "/")
	if relative == "" {
		return -1, unix.EINVAL
	}
	return unix.Openat2(anchor, relative, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: evidenceResolveFlags,
	})
}

func (linuxEvidenceFSOps) openDirAt(parent int, name string) (int, error) {
	return unix.Openat2(parent, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: evidenceResolveFlags,
	})
}

func (linuxEvidenceFSOps) openFileAt(parent int, name string, create bool) (int, bool, error) {
	flags := uint64(unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW)
	mode := uint64(0)
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
		mode = uint64(evidenceFileMode)
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: flags, Mode: mode, Resolve: evidenceResolveFlags})
	return fd, create && err == nil, err
}

func (linuxEvidenceFSOps) stat(fd int) (evidenceFileStat, error) {
	var raw unix.Stat_t
	if err := unix.Fstat(fd, &raw); err != nil {
		return evidenceFileStat{}, err
	}
	kind := evidenceFileUnknown
	switch raw.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		kind = evidenceFileDirectory
	case unix.S_IFREG:
		kind = evidenceFileRegular
	}
	if raw.Size < 0 {
		return evidenceFileStat{}, unix.EOVERFLOW
	}
	return evidenceFileStat{device: uint64(raw.Dev), inode: raw.Ino, size: uint64(raw.Size), mode: raw.Mode & 0o7777, uid: raw.Uid, nlink: uint64(raw.Nlink), kind: kind}, nil
}

func (linuxEvidenceFSOps) filesystemType(fd int) (int64, error) {
	var raw unix.Statfs_t
	if err := unix.Fstatfs(fd, &raw); err != nil {
		return 0, err
	}
	return int64(raw.Type), nil
}

func (linuxEvidenceFSOps) fdatasync(fd int) error              { return unix.Fdatasync(fd) }
func (linuxEvidenceFSOps) fsync(fd int) error                  { return unix.Fsync(fd) }
func (linuxEvidenceFSOps) read(fd int, p []byte) (int, error)  { return unix.Read(fd, p) }
func (linuxEvidenceFSOps) write(fd int, p []byte) (int, error) { return unix.Write(fd, p) }
func (linuxEvidenceFSOps) readDirectoryNames(fd int, maximum int) ([]string, error) {
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
func (linuxEvidenceFSOps) renameNoReplace(parent int, oldName, newName string) error {
	return unix.Renameat2(parent, oldName, parent, newName, unix.RENAME_NOREPLACE)
}
func (linuxEvidenceFSOps) isNoReplaceConflict(err error) bool { return errors.Is(err, unix.EEXIST) }
func (linuxEvidenceFSOps) linkAt(parent int, oldName, newName string) error {
	return unix.Linkat(parent, oldName, parent, newName, 0)
}
func (linuxEvidenceFSOps) unlinkAt(parent int, name string) error {
	return unix.Unlinkat(parent, name, 0)
}
func (linuxEvidenceFSOps) isNotExist(err error) bool { return errors.Is(err, unix.ENOENT) }
func (linuxEvidenceFSOps) tryLock(fd int) (bool, error) {
	err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}
func (linuxEvidenceFSOps) unlock(fd int) error { return unix.Flock(fd, unix.LOCK_UN) }
func (linuxEvidenceFSOps) close(fd int) error  { return unix.Close(fd) }
func (linuxEvidenceFSOps) probeStep(string)    {}
