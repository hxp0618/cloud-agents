//go:build linux

package migration

import (
	"context"
	"testing"

	"golang.org/x/sys/unix"
)

func TestUniqueLinuxMountInfoRequiresOneExactMount(t *testing.T) {
	line := "42 31 8:1 / /srv/evidence rw,nosuid,nodev - ext4 /dev/sda1 rw,errors=remount-ro\n"
	entry, err := uniqueLinuxMountInfo([]byte(line), 42)
	if err != nil || entry.fsType != "ext4" || entry.source != "/dev/sda1" || entry.root != "/" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	for _, raw := range []string{
		"",
		line + line,
		"42 malformed\n",
		"not-a-number 31 8:1 / /srv rw - ext4 /dev/sda1 rw\n",
	} {
		if _, err := uniqueLinuxMountInfo([]byte(raw), 42); err == nil {
			t.Fatalf("invalid mountinfo accepted: %q", raw)
		}
	}
}

func TestVerifyLinuxMountInfoAdmissionMatrix(t *testing.T) {
	dev := uint64(unix.Mkdev(8, 1))
	base := "42 31 8:1 / / rw,nosuid,nodev - ext4 /dev/sda1 rw,errors=remount-ro\n"
	tests := []struct {
		name       string
		raw        string
		mountID    int
		fsType     int64
		device     uint64
		admissible bool
	}{
		{"root-ext4", base, 42, unix.EXT4_SUPER_MAGIC, dev, true},
		{"mount-id-ambiguity", base + base, 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"device-mismatch", base, 42, unix.EXT4_SUPER_MAGIC, uint64(unix.Mkdev(8, 2)), false},
		{"fs-mismatch", base, 42, unix.XFS_SUPER_MAGIC, dev, false},
		{"source-not-device", "42 31 8:1 / / rw - ext4 tmpfs rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"device-root-alias", "42 31 8:1 / / rw - ext4 /dev/root rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"subtree-root", "42 31 8:1 /sub / rw - ext4 /dev/sda1 rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"non-root-mountpoint", "42 31 8:1 / /evidence rw - ext4 /dev/sda1 rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"bind-token", "42 31 8:1 / / rw,bind - ext4 /dev/sda1 rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"remote", "42 31 0:44 / / rw - nfs server:/export rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
		{"overlay", "42 31 0:44 / / rw - overlay overlay rw\n", 42, unix.EXT4_SUPER_MAGIC, dev, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := verifyLinuxMountInfo([]byte(test.raw), test.mountID, test.fsType, test.device)
			if (err == nil) != test.admissible {
				t.Fatalf("admissible=%v err=%v", test.admissible, err)
			}
		})
	}
}

func TestProductionEvidenceFSRootFailsClosedWithoutMountAuthority(t *testing.T) {
	root, err := newProductionEvidenceFSRoot(context.Background(), "/evidence")
	if root != nil || !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("root=%v err=%v", root, err)
	}
}

func TestLinuxMountOptionExactToken(t *testing.T) {
	if !containsMountOption("rw,nodev,bind", "bind") || containsMountOption("rw,bindish", "bind") {
		t.Fatal("mount option matching is not token exact")
	}
}
