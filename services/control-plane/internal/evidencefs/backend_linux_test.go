//go:build linux

package evidencefs

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxExistingOpenIsNonblockingAndNoFollow(t *testing.T) {
	required := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if linuxExistingOpenFlags&required != required || linuxExistingOpenFlags&(unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_RDWR) != 0 {
		t.Fatalf("existing flags=%#x", linuxExistingOpenFlags)
	}
	createRequired := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_CREAT | unix.O_EXCL
	if linuxCreateOpenFlags&createRequired != createRequired || linuxCreateOpenFlags&unix.O_NONBLOCK != 0 {
		t.Fatalf("create flags=%#x", linuxCreateOpenFlags)
	}
}

func TestLinuxRootMayCrossMountButChildrenMayNot(t *testing.T) {
	if rootResolveFlags&unix.RESOLVE_NO_XDEV != 0 {
		t.Fatalf("root resolve unexpectedly forbids dedicated mount: %#x", rootResolveFlags)
	}
	if resolveFlags&unix.RESOLVE_NO_XDEV == 0 || resolveFlags&rootResolveFlags != rootResolveFlags {
		t.Fatalf("child resolve flags=%#x root=%#x", resolveFlags, rootResolveFlags)
	}
}
