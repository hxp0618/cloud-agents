package evidencefs

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
)

func TestRequiredSyscallProbeIsCleanAndKeepsRootUsable(t *testing.T) {
	f := newFakeBackend()
	root, err := newRootWithRequiredProbe(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	if root == nil || !root.usable() {
		t.Fatal("clean required-syscall probe did not return a usable root")
	}
	if len(f.handles) != 0 {
		t.Fatalf("required-syscall probe leaked descriptors: %v", f.handles)
	}
	if f.randomCalls != 3 || f.renames != 2 || f.fdatasyncs != 2 || f.fsyncs != 4 || f.unlinks != 2 {
		t.Fatalf("probe operations differ: random=%d rename=%d fdatasync=%d fsync=%d unlink=%d", f.randomCalls, f.renames, f.fdatasyncs, f.fsyncs, f.unlinks)
	}
	if len(f.tryLockAttempts) != 2 || f.tryLockAttempts[0] != "lineages.lock" || f.tryLockAttempts[1] != "lineages.lock" {
		t.Fatalf("probe did not test an independent root-lock open: %v", f.tryLockAttempts)
	}
	if names := probeTempNames(f); len(names) != 0 {
		t.Fatalf("clean probe left temporary files: %v", names)
	}
	lease, err := root.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRequiredSyscallProbeFaultsFailClosedAndDrainOwnedTemps(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeBackend)
	}{
		{"lock-error", func(f *fakeBackend) {
			f.onTryLock = func(value *fakeBackend, node *fakeNode, attempt int) {
				if attempt == 2 {
					value.failTryLockInodes[node.stat.inode] = true
				}
			}
		}},
		{"lock-not-exclusive", func(f *fakeBackend) {
			f.onTryLock = func(_ *fakeBackend, node *fakeNode, attempt int) {
				if attempt == 2 {
					node.locked = false
				}
			}
		}},
		{"random", func(f *fakeBackend) { f.failRandomAt = 2 }},
		{"write", func(f *fakeBackend) { f.failWriteAt = 1 }},
		{"fdatasync", func(f *fakeBackend) { f.failFdatasyncAt = 1 }},
		{"directory-sync", func(f *fakeBackend) { f.failFsyncAt = 1 }},
		{"rename", func(f *fakeBackend) { f.failRename = true }},
		{"rename-response-lost", func(f *fakeBackend) { f.failRenameAfterAt = 1 }},
		{"rename-left-source", func(f *fakeBackend) { f.copyRenameAt = 1 }},
		{"conflict-error", func(f *fakeBackend) { f.failRenameAt = 2 }},
		{"no-replace-overwrite", func(f *fakeBackend) { f.replaceRenameAt = 2 }},
		{"file-close", func(f *fakeBackend) {
			f.onWrite = func(value *fakeBackend, node *fakeNode, _ int) { value.failCloseName = node.name }
		}},
		{"contender-close", func(f *fakeBackend) { f.failCloseAt["lineages.lock"] = 3 }},
		{"sha-close", func(f *fakeBackend) { f.failCloseAt["sha256"] = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFakeBackend()
			test.mutate(f)
			root := testUnprobedRoot(t, f)
			if err := root.probeRequiredSyscalls(context.Background()); !errors.Is(err, ErrFilesystem) {
				t.Fatalf("probe fault did not fail closed: %v", err)
			}
			if root.usable() {
				t.Fatal("failed required-syscall probe left root usable")
			}
			if names := probeTempNames(f); len(names) != 0 {
				t.Fatalf("probe fault left owned temporary files: %v", names)
			}
			if len(f.handles) != 0 {
				t.Fatalf("probe fault leaked descriptors: %v", f.handles)
			}
		})
	}
}

func TestRequiredSyscallProbeCleanupAttemptsEveryOwnedTempAndDirectorySync(t *testing.T) {
	f := newFakeBackend()
	f.failUnlinkAt = 1
	root := testUnprobedRoot(t, f)
	if err := root.probeRequiredSyscalls(context.Background()); !errors.Is(err, ErrFilesystem) {
		t.Fatalf("cleanup fault did not fail closed: %v", err)
	}
	if f.unlinks != 2 || f.fsyncs != 4 {
		t.Fatalf("cleanup did not attempt both unlinks and final directory sync: unlinks=%d fsyncs=%d", f.unlinks, f.fsyncs)
	}
	if root.usable() || len(f.handles) != 0 {
		t.Fatalf("cleanup fault retained authority or descriptors: usable=%v handles=%v", root.usable(), f.handles)
	}
	names := probeTempNames(f)
	if len(names) != 1 || !tempNamePattern.MatchString(names[0]) {
		t.Fatalf("cleanup fault did not leave one conservatively countable temp: %v", names)
	}
}

func TestRequiredSyscallProbeRetriesRandomNameCollisionWithoutDeletingForeignTemp(t *testing.T) {
	f := newFakeBackend()
	foreign := requiredProbeFakeName(f, 1)
	f.shaDir().children[foreign] = f.regular(foreign, []byte("preexisting"))
	root, err := newRootWithRequiredProbe(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil || root == nil || !root.usable() {
		t.Fatalf("collision retry failed: root=%v err=%v", root, err)
	}
	node, ok := f.shaDir().children[foreign]
	if !ok || string(node.data) != "preexisting" || f.randomCalls != 4 {
		t.Fatalf("collision temp changed: present=%v data=%q random=%d", ok, node.data, f.randomCalls)
	}
	if names := probeTempNames(f); len(names) != 1 || names[0] != foreign {
		t.Fatalf("probe did not clean only its owned temps: %v", names)
	}
}

func TestRequiredSyscallProbeContextBoundary(t *testing.T) {
	f := newFakeBackend()
	root := testUnprobedRoot(t, f)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.probeRequiredSyscalls(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-mutation cancellation changed: %v", err)
	}
	if !root.usable() || f.randomCalls != 0 || len(probeTempNames(f)) != 0 {
		t.Fatal("pre-mutation cancellation mutated or poisoned the root")
	}

	f = newFakeBackend()
	root = testUnprobedRoot(t, f)
	ctx, cancelMid := context.WithCancel(context.Background())
	f.partialWrite = 1
	f.onWrite = func(_ *fakeBackend, _ *fakeNode, attempt int) {
		if attempt == 1 {
			cancelMid()
		}
	}
	if err := root.probeRequiredSyscalls(ctx); !errors.Is(err, ErrFilesystem) || errors.Is(err, context.Canceled) {
		t.Fatalf("post-mutation cancellation did not become filesystem failure: %v", err)
	}
	if root.usable() || len(probeTempNames(f)) != 0 || len(f.handles) != 0 {
		t.Fatalf("post-mutation cancellation retained authority: usable=%v temps=%v handles=%v", root.usable(), probeTempNames(f), f.handles)
	}
}

func TestRequiredSyscallProbeHasNoProductionAuthorityConsumer(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "root.go" || name == "probe.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, symbol := range []string{"newRootWithRequiredProbe(", "newRootWithAuthority(", ".probeRequiredSyscalls("} {
			if strings.Contains(string(raw), symbol) {
				t.Fatalf("required probe acquired an unreviewed production authority consumer %s: %s", symbol, name)
			}
		}
	}
	if root, err := Open(context.Background(), "/evidence"); root != nil || !errors.Is(err, ErrTrustedMountAuthority) {
		t.Fatalf("required probe made production Open usable: root=%v err=%v", root, err)
	}
}

func testUnprobedRoot(t *testing.T, f *fakeBackend) *Root {
	t.Helper()
	root, err := newRootWithAuthority(context.Background(), "/evidence", f.uid, f, mountAuthority{seal: &struct{}{}})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func probeTempNames(f *fakeBackend) []string {
	var names []string
	for name := range f.shaDir().children {
		if tempNamePattern.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func requiredProbeFakeName(f *fakeBackend, call int) string {
	nonce := f.nonce
	nonce[len(nonce)-1] ^= byte(call - 1)
	return ".tmp-" + hex.EncodeToString(nonce[:])
}
