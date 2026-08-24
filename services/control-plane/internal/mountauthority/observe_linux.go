//go:build linux

package mountauthority

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	maximumBootIDBytes    = 64
	maximumMountInfoBytes = 4 << 20
)

type mountInfoEntry struct {
	id           uint64
	major        uint32
	minor        uint32
	root         string
	mountPoint   string
	mountOptions string
	filesystem   string
	source       string
	superOptions string
}

var (
	readBootID = func() ([]byte, error) {
		return readBoundedFile("/proc/sys/kernel/random/boot_id", maximumBootIDBytes)
	}
	readMountInfo = func() ([]byte, error) {
		return readBoundedFile("/proc/self/mountinfo", maximumMountInfoBytes)
	}
)

// ObserveFD returns kernel facts only. It never mints a Claim.
func ObserveFD(ctx context.Context, fd int, rootPath string) (Observation, error) {
	if err := checkContext(ctx); err != nil {
		return Observation{}, err
	}
	pathDigest, err := RootPathDigest(rootPath)
	if err != nil || fd < 0 {
		return Observation{}, invalid("observation-input")
	}
	var root unix.Stat_t
	if err := unix.Fstat(fd, &root); err != nil {
		return Observation{}, filesystem("root-stat")
	}
	mode := uint32(root.Mode & 0o7777)
	if root.Mode&unix.S_IFMT != unix.S_IFDIR || root.Dev == 0 || root.Ino == 0 || mode&^uint32(0o700) != 0 {
		return Observation{}, invalid("root-identity")
	}
	var statfs unix.Statfs_t
	if err := unix.Fstatfs(fd, &statfs); err != nil {
		return Observation{}, filesystem("root-statfs")
	}
	filesystemType := uint32(statfs.Type)
	if !supportedFilesystem(filesystemType) {
		return Observation{}, ErrUnsupported
	}
	_, mountID, err := unix.NameToHandleAt(fd, "", unix.AT_EMPTY_PATH)
	if err != nil || mountID <= 0 {
		return Observation{}, unavailable("mount-id")
	}
	bootRaw, err := readBootID()
	if err != nil {
		return Observation{}, filesystem("boot-id-read")
	}
	bootID, err := parseBootID(bootRaw)
	if err != nil {
		return Observation{}, invalid("boot-id")
	}
	var namespace unix.Stat_t
	if err := unix.Stat("/proc/self/ns/mnt", &namespace); err != nil || namespace.Dev == 0 || namespace.Ino == 0 {
		return Observation{}, filesystem("mount-namespace")
	}
	mountRaw, err := readMountInfo()
	if err != nil {
		return Observation{}, filesystem("mountinfo-read")
	}
	entry, err := parseMountInfo(mountRaw, uint64(mountID))
	if err != nil {
		return Observation{}, invalid("mountinfo")
	}
	expectedFilesystem := "ext4"
	if filesystemType == uint32(unix.XFS_SUPER_MAGIC) {
		expectedFilesystem = "xfs"
	}
	rootDevice := uint64(root.Dev)
	if entry.root != "/" || entry.mountPoint != rootPath || entry.filesystem != expectedFilesystem || entry.major != unix.Major(rootDevice) || entry.minor != unix.Minor(rootDevice) || !directLocalSource(entry.source) || hasMountOption(entry.mountOptions, "bind") || hasMountOption(entry.mountOptions, "rbind") || hasMountOption(entry.superOptions, "bind") || hasMountOption(entry.superOptions, "rbind") {
		return Observation{}, ErrUnsupported
	}
	observed := Observation{
		RootPathDigest:      pathDigest,
		BootID:              bootID,
		MountNamespaceDev:   uint64(namespace.Dev),
		MountNamespaceInode: namespace.Ino,
		MountID:             uint64(mountID),
		FilesystemType:      filesystemType,
		RootDevice:          rootDevice,
		RootInode:           root.Ino,
		RootUID:             root.Uid,
		RootMode:            mode,
		DeviceMajor:         unix.Major(rootDevice),
		DeviceMinor:         unix.Minor(rootDevice),
		SourceDigest:        mountSourceDigest(entry.source),
		OptionsDigest:       mountOptionsDigest(entry.mountOptions, entry.superOptions),
	}
	if !validObservation(observed) {
		return Observation{}, invalid("observation")
	}
	return observed, nil
}

func currentRunnerIsUnprivileged(runnerUID uint32) error {
	euid := os.Geteuid()
	ruid, resolvedEUID, suid := unix.Getresuid()
	fsuid, fsuidErr := unix.SetfsuidRetUid(-1)
	if fsuidErr != nil {
		return filesystem("filesystem-uid")
	}
	if runnerUID == 0 || euid <= 0 || resolvedEUID != euid || ruid != euid || suid != euid || fsuid != euid || uint32(euid) != runnerUID {
		return ErrNotPrivileged
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	data := [2]unix.CapUserData{}
	if err := unix.Capget(&header, &data[0]); err != nil {
		return filesystem("capabilities")
	}
	if data[0].Effective != 0 || data[1].Effective != 0 || data[0].Permitted != 0 || data[1].Permitted != 0 || data[0].Inheritable != 0 || data[1].Inheritable != 0 {
		return ErrNotPrivileged
	}
	return nil
}

func parseBootID(raw []byte) ([16]byte, error) {
	text := strings.TrimSuffix(string(raw), "\n")
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' || strings.ToLower(text) != text {
		return [16]byte{}, ErrInvalid
	}
	compact := strings.NewReplacer("-", "").Replace(text)
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return [16]byte{}, ErrInvalid
	}
	var bootID [16]byte
	copy(bootID[:], decoded)
	if bootID == [16]byte{} {
		return [16]byte{}, ErrInvalid
	}
	return bootID, nil
}

func parseMountInfo(raw []byte, mountID uint64) (mountInfoEntry, error) {
	if len(raw) == 0 || len(raw) > maximumMountInfoBytes || mountID == 0 {
		return mountInfoEntry{}, ErrInvalid
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), maximumMountInfoBytes)
	matches := 0
	var found mountInfoEntry
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for index, field := range fields {
			if field == "-" {
				separator = index
				break
			}
		}
		if separator < 6 || separator+3 != len(fields)-1 {
			return mountInfoEntry{}, ErrInvalid
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || id == 0 {
			return mountInfoEntry{}, ErrInvalid
		}
		major, minor, err := parseDevice(fields[2])
		if err != nil {
			return mountInfoEntry{}, err
		}
		root, err := unescapeMountField(fields[3])
		if err != nil {
			return mountInfoEntry{}, err
		}
		mountPoint, err := unescapeMountField(fields[4])
		if err != nil {
			return mountInfoEntry{}, err
		}
		source, err := unescapeMountField(fields[separator+2])
		if err != nil {
			return mountInfoEntry{}, err
		}
		if id == mountID {
			matches++
			found = mountInfoEntry{id: id, major: major, minor: minor, root: root, mountPoint: mountPoint, mountOptions: fields[5], filesystem: fields[separator+1], source: source, superOptions: fields[separator+3]}
		}
	}
	if scanner.Err() != nil || matches != 1 {
		return mountInfoEntry{}, ErrInvalid
	}
	return found, nil
}

func parseDevice(raw string) (uint32, uint32, error) {
	majorRaw, minorRaw, ok := strings.Cut(raw, ":")
	if !ok || majorRaw == "" || minorRaw == "" {
		return 0, 0, ErrInvalid
	}
	major, err := strconv.ParseUint(majorRaw, 10, 32)
	if err != nil {
		return 0, 0, ErrInvalid
	}
	minor, err := strconv.ParseUint(minorRaw, 10, 32)
	if err != nil {
		return 0, 0, ErrInvalid
	}
	return uint32(major), uint32(minor), nil
}

func unescapeMountField(raw string) (string, error) {
	var decoded strings.Builder
	decoded.Grow(len(raw))
	for index := 0; index < len(raw); index++ {
		if raw[index] != '\\' {
			if raw[index] == 0 {
				return "", ErrInvalid
			}
			decoded.WriteByte(raw[index])
			continue
		}
		if index+3 >= len(raw) {
			return "", ErrInvalid
		}
		escape := raw[index+1 : index+4]
		var value byte
		switch escape {
		case "040":
			value = ' '
		case "011":
			value = '\t'
		case "012":
			value = '\n'
		case "134":
			value = '\\'
		default:
			return "", ErrInvalid
		}
		decoded.WriteByte(value)
		index += 3
	}
	return decoded.String(), nil
}

func directLocalSource(source string) bool {
	return strings.HasPrefix(source, "/dev/") && source != "/dev/root" && filepathClean(source) == source
}

func filepathClean(path string) string {
	parts := strings.Split(path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return ""
		default:
			clean = append(clean, part)
		}
	}
	return "/" + strings.Join(clean, "/")
}

func hasMountOption(options, target string) bool {
	for _, option := range strings.Split(options, ",") {
		if option == target {
			return true
		}
	}
	return false
}

func mountSourceDigest(source string) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("cloud-agents/evidencefs-mount-source/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(source))
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func mountOptionsDigest(mountOptions, superOptions string) [32]byte {
	hash := sha256.New()
	_, _ = hash.Write([]byte("cloud-agents/evidencefs-mount-options/v1"))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(mountOptions))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(superOptions))
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximum+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(data)) > maximum {
		return nil, unix.EOVERFLOW
	}
	return data, nil
}
