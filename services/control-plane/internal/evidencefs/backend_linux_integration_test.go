//go:build linux && evidencefsintegration

package evidencefs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	linuxIntegrationRequiredEnv = "CLOUD_AGENTS_REQUIRE_EVIDENCEFS_LINUX_INTEGRATION"
	linuxIntegrationRootEnv     = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT"
	linuxIntegrationFSEnv       = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_FS"
	linuxIntegrationHelperEnv   = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_HELPER"
	linuxIntegrationVerifyEnv   = "CLOUD_AGENTS_EVIDENCEFS_VERIFY_EXISTING"
	linuxIntegrationReady       = "EVIDENCEFS_INTEGRATION_PUBLISHED_AND_LOCKED"
	linuxIntegrationPayload     = "cloud-agents-evidencefs-linux-integration-v1"
)

var linuxIntegrationDigest = sha256.Sum256([]byte(linuxIntegrationPayload))

// TestLinuxIntegrationObjectRestartAndCrossProcessLock is deliberately hidden
// behind an opt-in build tag and environment gate. It exercises the real Linux
// backend with a package-private test authority; it does not make Open usable
// and cannot mint production trusted-mount authority.
func TestLinuxIntegrationObjectRestartAndCrossProcessLock(t *testing.T) {
	if os.Getenv(linuxIntegrationRequiredEnv) != "1" {
		t.Skip("real Linux evidencefs integration was not explicitly required")
	}
	rootPath := os.Getenv(linuxIntegrationRootEnv)
	filesystem := os.Getenv(linuxIntegrationFSEnv)
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		t.Fatal("integration root must be an absolute canonical path")
	}
	mountID := requireLinuxIntegrationMount(t, rootPath, filesystem)

	switch os.Getenv(linuxIntegrationHelperEnv) {
	case "publish-hold":
		publishAndHoldLinuxIntegrationObject(t, rootPath)
		return
	case "verify":
		verifyLinuxIntegrationObject(t, rootPath)
		return
	case "":
	default:
		t.Fatal("unknown integration helper mode")
	}

	if root, err := Open(context.Background(), rootPath); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("production Open bypassed trusted mount authority: root=%v err=%v", root, err)
	}
	if os.Getenv(linuxIntegrationVerifyEnv) == "1" {
		verifyLinuxIntegrationObject(t, rootPath)
		t.Logf("EVIDENCEFS_LINUX_REOPEN filesystem=%s mount_id=%d object=%s", filesystem, mountID, hex.EncodeToString(linuxIntegrationDigest[:]))
		return
	}

	command := integrationHelperCommand("publish-hold")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var helperErrors bytes.Buffer
	command.Stderr = &helperErrors
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if command.Process != nil && !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != linuxIntegrationReady {
		_ = command.Process.Kill()
		_ = command.Wait()
		waited = true
		t.Fatalf("publisher did not hold the durable root lock: line=%q scan_err=%v stderr=%q", scanner.Text(), scanner.Err(), helperErrors.String())
	}

	contender := newLinuxIntegrationRoot(t, rootPath)
	blockedContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	blockedLease, blockedErr := contender.Acquire(blockedContext)
	if blockedLease != nil || !errors.Is(blockedErr, context.DeadlineExceeded) {
		if blockedLease != nil {
			_ = blockedLease.Close()
		}
		t.Fatalf("cross-process root lock did not block: lease=%v err=%v", blockedLease, blockedErr)
	}

	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = stdin.Close()
	if err := command.Wait(); err == nil {
		t.Fatal("publisher unexpectedly exited cleanly instead of being killed")
	}
	waited = true

	verifyLinuxIntegrationObject(t, rootPath)
	verifyCommand := integrationHelperCommand("verify")
	if output, err := verifyCommand.CombinedOutput(); err != nil {
		t.Fatalf("fresh verifier process rejected durable object: err=%v output=%q", err, output)
	}
	t.Logf("EVIDENCEFS_LINUX_INTEGRATION filesystem=%s mount_id=%d object=%s", filesystem, mountID, hex.EncodeToString(linuxIntegrationDigest[:]))
}

func integrationHelperCommand(mode string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestLinuxIntegrationObjectRestartAndCrossProcessLock$", "-test.count=1")
	command.Env = append(os.Environ(), linuxIntegrationHelperEnv+"="+mode)
	return command
}

func publishAndHoldLinuxIntegrationObject(t *testing.T, rootPath string) {
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := lease.Publish(context.Background(), scan, linuxIntegrationDigest, []byte(linuxIntegrationPayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.BindPublication(publication, linuxIntegrationDigest, uint64(len(linuxIntegrationPayload))); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, linuxIntegrationReady); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func verifyLinuxIntegrationObject(t *testing.T, rootPath string) {
	t.Helper()
	root := newLinuxIntegrationRoot(t, rootPath)
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lease.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	scan, err := lease.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !scan.HasObject(linuxIntegrationDigest, uint64(len(linuxIntegrationPayload))) {
		t.Fatal("durable integration object was absent or mismatched")
	}
}

func newLinuxIntegrationRoot(t *testing.T, rootPath string) *Root {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), rootPath, uint32(os.Getuid()), linuxBackend{}, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func requireLinuxIntegrationMount(t *testing.T, rootPath, filesystem string) int {
	t.Helper()
	expected := int64(0)
	switch filesystem {
	case "ext4":
		expected = unix.EXT4_SUPER_MAGIC
	case "xfs":
		expected = unix.XFS_SUPER_MAGIC
	default:
		t.Fatal("integration filesystem must be ext4 or xfs")
	}
	var statfs unix.Statfs_t
	if err := unix.Statfs(rootPath, &statfs); err != nil || int64(statfs.Type) != expected {
		t.Fatalf("integration filesystem mismatch: type=%#x expected=%#x err=%v", statfs.Type, expected, err)
	}
	_, mountID, err := unix.NameToHandleAt(unix.AT_FDCWD, rootPath, 0)
	if err != nil || mountID <= 0 {
		t.Fatalf("integration mount identity is unavailable: mount_id=%d err=%v", mountID, err)
	}
	return mountID
}
