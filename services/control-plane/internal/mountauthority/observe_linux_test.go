//go:build linux

package mountauthority

import (
	"errors"
	"testing"
)

func TestParseMountInfoRequiresOneExactClosedEntry(t *testing.T) {
	line := "42 31 259:7 / /srv/cloud-agents/evidence rw,nodev - ext4 /dev/nvme0n1p7 rw,errors=remount-ro\n"
	entry, err := parseMountInfo([]byte(line), 42)
	if err != nil || entry.mountPoint != "/srv/cloud-agents/evidence" || entry.source != "/dev/nvme0n1p7" || entry.major != 259 || entry.minor != 7 {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	for _, raw := range []string{
		"",
		line + line,
		"42 malformed\n",
		"x 31 259:7 / /srv rw - ext4 /dev/sda1 rw\n",
		"42 31 bad / /srv rw - ext4 /dev/sda1 rw\n",
		"42 31 259:7 / /srv rw - ext4 /dev/sda1 rw trailing\n",
	} {
		if _, err := parseMountInfo([]byte(raw), 42); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid mountinfo accepted: %q err=%v", raw, err)
		}
	}
}

func TestMountInfoEscapesAndSourceClassificationAreExact(t *testing.T) {
	line := "42 31 8:1 / /srv/evidence\\040root rw - xfs /dev/mapper/evidence\\040disk rw\n"
	entry, err := parseMountInfo([]byte(line), 42)
	if err != nil || entry.mountPoint != "/srv/evidence root" || entry.source != "/dev/mapper/evidence disk" {
		t.Fatalf("entry=%+v err=%v", entry, err)
	}
	for _, invalid := range []string{"bad\\", "bad\\000", "bad\\777", "bad\\04"} {
		if _, err := unescapeMountField(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatalf("escape %q accepted: %v", invalid, err)
		}
	}
	for _, source := range []string{"/dev/root", "tmpfs", "server:/volume", "/dev/../tmp/x"} {
		if directLocalSource(source) {
			t.Fatalf("source %q accepted", source)
		}
	}
	if !directLocalSource("/dev/mapper/evidence") {
		t.Fatal("direct local device rejected")
	}
	if !hasMountOption("rw,nodev,bind", "bind") || hasMountOption("rw,bindish", "bind") {
		t.Fatal("mount option matching is not token exact")
	}
}

func TestParseBootIDIsStrictAndNonzero(t *testing.T) {
	want := [16]byte{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}
	got, err := parseBootID([]byte("12345678-1234-1234-1234-123456789abc\n"))
	if err != nil || got != want {
		t.Fatalf("boot=%x err=%v", got, err)
	}
	for _, raw := range []string{"", "00000000-0000-0000-0000-000000000000\n", "12345678-1234-1234-1234-123456789ABC\n", "12345678123412341234123456789abc\n", "12345678-1234-1234-1234-123456789abc\nextra"} {
		if _, err := parseBootID([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("boot id %q accepted: %v", raw, err)
		}
	}
}
