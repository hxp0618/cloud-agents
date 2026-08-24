//go:build linux

package evidencefs

import (
	"errors"
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

func TestLinuxBackendClassifiesOnlyENOENTAsAbsent(t *testing.T) {
	backend := linuxBackend{}
	if !backend.isNotExist(unix.ENOENT) || backend.isNotExist(unix.EACCES) || backend.isNotExist(errors.New("missing text")) {
		t.Fatal("absence classification is not exact")
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

func TestLinuxMkdirUsesPrivateDirectoryMode(t *testing.T) {
	if directoryMode != 0o700 {
		t.Fatalf("directory mode=%#o", directoryMode)
	}
}
