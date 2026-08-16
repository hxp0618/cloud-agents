//go:build linux

package mountauthority

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	linuxAuthorityIntegrationEnv = "CLOUD_AGENTS_MOUNTAUTHORITY_INTEGRATION"
	linuxAuthorityChildEnv       = "CLOUD_AGENTS_MOUNTAUTHORITY_CHILD"
	linuxAuthorityChildWantEnv   = "CLOUD_AGENTS_MOUNTAUTHORITY_CHILD_WANT"
	linuxAuthorityTestRoot       = "/srv/cloud-agents/evidencefs-authority-test"
)

func TestLinuxFixedAuthorityLoadIntegration(t *testing.T) {
	if os.Getenv(linuxAuthorityChildEnv) == "1" {
		runLinuxAuthorityLoadChild(t)
		return
	}
	if os.Getenv(linuxAuthorityIntegrationEnv) != "1" {
		t.Skip("requires an isolated disposable Linux root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("integration parent must be root")
	}
	requireLinuxOpenat2(t)
	if _, err := os.Lstat("/run/cloud-agents"); !errors.Is(err, os.ErrNotExist) {
		t.Skip("refusing to touch an existing /run/cloud-agents")
	}
	if err := os.MkdirAll(authorityDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll("/run/cloud-agents") })
	body := linuxIntegrationAttestation(t)
	basename, err := AuthorityBasename(linuxAuthorityTestRoot)
	if err != nil {
		t.Fatal(err)
	}
	finalPath := filepath.Join(authorityDirectory, basename)
	linkedPath := finalPath + ".link"
	reset := func(t *testing.T) {
		t.Helper()
		_ = os.Remove(linkedPath)
		_ = os.Remove(finalPath)
		if err := os.Chmod("/run/cloud-agents", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(authorityDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		writeLinuxIntegrationAuthority(t, finalPath, body)
	}

	tests := []struct {
		name    string
		prepare func(*testing.T)
		valid   bool
	}{
		{name: "valid", valid: true},
		{name: "file-write-bit", prepare: func(t *testing.T) { mustChmod(t, finalPath, 0o644) }},
		{name: "file-owner", prepare: func(t *testing.T) { mustChown(t, finalPath, 1001, 1001) }},
		{name: "file-hardlink", prepare: func(t *testing.T) {
			if err := os.Link(finalPath, linkedPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "file-torn", prepare: func(t *testing.T) {
			if err := os.Truncate(finalPath, authoritySize-1); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "file-oversize", prepare: func(t *testing.T) {
			mustChmod(t, finalPath, 0o600)
			file, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.Write([]byte{0})
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Fatalf("write=%v close=%v", writeErr, closeErr)
			}
			mustChmod(t, finalPath, 0o444)
		}},
		{name: "file-symlink", prepare: func(t *testing.T) {
			if err := os.Remove(finalPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/passwd", finalPath); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "file-fifo", prepare: func(t *testing.T) {
			if err := os.Remove(finalPath); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mkfifo(finalPath, 0o444); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory-write-bit", prepare: func(t *testing.T) { mustChmod(t, "/run/cloud-agents", 0o777) }},
		{name: "wrong-path", prepare: func(t *testing.T) {
			wrong := body
			wrong.rootPath = sha256.Sum256([]byte("/srv/other"))
			wrong.observed.RootPathDigest = wrong.rootPath
			writeLinuxIntegrationAuthority(t, finalPath, wrong)
		}},
		{name: "wrong-runner", prepare: func(t *testing.T) {
			wrong := body
			wrong.runnerUID = 1002
			wrong.observed.RootUID = 1002
			writeLinuxIntegrationAuthority(t, finalPath, wrong)
		}},
		{name: "wrong-boot", prepare: func(t *testing.T) {
			wrong := body
			wrong.observed.BootID[0] ^= 1
			writeLinuxIntegrationAuthority(t, finalPath, wrong)
		}},
		{name: "wrong-namespace", prepare: func(t *testing.T) {
			wrong := body
			wrong.observed.MountNamespaceInode++
			writeLinuxIntegrationAuthority(t, finalPath, wrong)
		}},
		{name: "copied-bytes-only", prepare: func(t *testing.T) {
			copyPath := filepath.Join(t.TempDir(), "copied.authority")
			raw, err := os.ReadFile(finalPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(copyPath, raw, 0o444); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(finalPath); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reset(t)
			if test.prepare != nil {
				test.prepare(t)
			}
			runLinuxAuthorityChild(t, test.valid)
		})
	}

	reset(t)
	if claim, err := Load(context.Background(), linuxAuthorityTestRoot, 1001); claim != nil || !errors.Is(err, ErrNotPrivileged) {
		t.Fatalf("root runner claim=%v err=%v", claim, err)
	}
	if err := Revoke(context.Background(), linuxAuthorityTestRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(finalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("revoked file remains: %v", err)
	}
}

func requireLinuxOpenat2(t *testing.T) {
	t.Helper()
	anchor, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal(err)
	}
	child, openErr := unix.Openat2(anchor, "run", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: authorityResolveFlags})
	anchorCloseErr := unix.Close(anchor)
	if errors.Is(openErr, unix.ENOSYS) {
		t.Skip("Linux environment does not implement openat2")
	}
	if openErr != nil || anchorCloseErr != nil {
		t.Fatalf("openat2=%v close=%v", openErr, anchorCloseErr)
	}
	if err := unix.Close(child); err != nil {
		t.Fatal(err)
	}
}

func runLinuxAuthorityLoadChild(t *testing.T) {
	t.Helper()
	want := os.Getenv(linuxAuthorityChildWantEnv) == "success"
	claim, err := Load(context.Background(), linuxAuthorityTestRoot, 1001)
	if want {
		if err != nil || claim == nil {
			t.Fatalf("claim=%v err=%v", claim, err)
		}
		if uid, ok := claim.RunnerUID(); !ok || uid != 1001 {
			t.Fatalf("uid=%d ok=%v", uid, ok)
		}
		return
	}
	if err == nil || claim != nil {
		t.Fatalf("invalid authority admitted: claim=%v err=%v", claim, err)
	}
}

func runLinuxAuthorityChild(t *testing.T, success bool) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := "failure"
	if success {
		want = "success"
	}
	command := exec.Command(executable, "-test.run=^TestLinuxFixedAuthorityLoadIntegration$", "-test.count=1")
	command.Env = append(os.Environ(), linuxAuthorityChildEnv+"=1", linuxAuthorityChildWantEnv+"="+want)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 1001, Gid: 1001}}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("child=%v output=%s", err, output)
	}
}

func linuxIntegrationAttestation(t *testing.T) attestation {
	t.Helper()
	pathDigest, err := RootPathDigest(linuxAuthorityTestRoot)
	if err != nil {
		t.Fatal(err)
	}
	bootID, namespaceDevice, namespaceInode, err := currentBootAndNamespace()
	if err != nil {
		t.Fatal(err)
	}
	device := linuxDevice(8, 1)
	return attestation{
		nonce:     [16]byte{1, 2, 3, 4},
		rootPath:  pathDigest,
		runnerUID: 1001,
		observed: Observation{
			RootPathDigest:      pathDigest,
			BootID:              bootID,
			MountNamespaceDev:   namespaceDevice,
			MountNamespaceInode: namespaceInode,
			MountID:             42,
			FilesystemType:      ext4SuperMagic,
			RootDevice:          device,
			RootInode:           43,
			RootUID:             1001,
			RootMode:            0o700,
			DeviceMajor:         8,
			DeviceMinor:         1,
			SourceDigest:        sha256.Sum256([]byte("/dev/test")),
			OptionsDigest:       sha256.Sum256([]byte("rw")),
		},
	}
}

func writeLinuxIntegrationAuthority(t *testing.T, path string, body attestation) {
	t.Helper()
	encoded, err := encodeAttestation(body)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.WriteFile(path, encoded, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(path, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mustChown(t *testing.T, path string, uid, gid int) {
	t.Helper()
	if err := os.Chown(path, uid, gid); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxProvisionRejectsUntrustedBackingBeforeCapabilityMutation(t *testing.T) {
	if os.Getenv(linuxAuthorityIntegrationEnv) != "1" {
		t.Skip("requires an isolated disposable Linux root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("integration parent must be root")
	}
	requireLinuxOpenat2(t)
	rootPath := t.TempDir()
	if err := os.Chown(rootPath, 1001, 1001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	err := Provision(context.Background(), ProvisionRequest{RootPath: rootPath, RunnerUID: 1001, ConfirmDirectLocalMount: true})
	if err == nil {
		t.Fatal("container backing mount was provisioned")
	}
	basename, basenameErr := AuthorityBasename(rootPath)
	if basenameErr != nil {
		t.Fatal(basenameErr)
	}
	if _, statErr := os.Lstat(filepath.Join(authorityDirectory, basename)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed provision left capability: err=%v provision=%v", statErr, err)
	}
	t.Logf("UNTRUSTED_BACKING_REJECTED error=%s", fmt.Sprint(err))
}
