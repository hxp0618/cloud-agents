//go:build linux

package mountauthority

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

type fakeAuthorityMutationOps struct {
	calls              []string
	failStep           string
	partialWrite       int
	fileFD             int
	directoryFD        int
	temporary          bool
	final              bool
	mode               uint32
	data               []byte
	renameAfterFailure bool
	unlinkAfterFailure bool
	conflict           bool
}

func newFakeAuthorityMutationOps() *fakeAuthorityMutationOps {
	return &fakeAuthorityMutationOps{fileFD: 20, directoryFD: 10}
}

func (f *fakeAuthorityMutationOps) create(parent int, _ string) (int, error) {
	f.calls = append(f.calls, "create")
	if parent != f.directoryFD || f.failStep == "create" {
		return -1, unix.EIO
	}
	f.temporary = true
	return f.fileFD, nil
}

func (f *fakeAuthorityMutationOps) write(fd int, source []byte) (int, error) {
	f.calls = append(f.calls, "write")
	if fd != f.fileFD || f.failStep == "write" {
		return 0, unix.EIO
	}
	length := len(source)
	if f.partialWrite > 0 && length > f.partialWrite {
		length = f.partialWrite
	}
	f.data = append(f.data, source[:length]...)
	return length, nil
}

func (f *fakeAuthorityMutationOps) fsync(fd int) error {
	step := "fsync-directory"
	if fd == f.fileFD {
		fileSyncs := 0
		for _, call := range f.calls {
			if call == "fsync-file-1" || call == "fsync-file-2" {
				fileSyncs++
			}
		}
		step = fmt.Sprintf("fsync-file-%d", fileSyncs+1)
	}
	f.calls = append(f.calls, step)
	if f.failStep == step {
		return unix.EIO
	}
	return nil
}

func (f *fakeAuthorityMutationOps) chmod(fd int, mode uint32) error {
	f.calls = append(f.calls, "chmod")
	if fd != f.fileFD || f.failStep == "chmod" {
		return unix.EIO
	}
	f.mode = mode
	return nil
}

func (f *fakeAuthorityMutationOps) close(fd int) error {
	f.calls = append(f.calls, "close")
	if fd != f.fileFD || f.failStep == "close" {
		return unix.EIO
	}
	return nil
}

func (f *fakeAuthorityMutationOps) renameNoReplace(parent int, _, _ string) error {
	f.calls = append(f.calls, "rename")
	if parent != f.directoryFD {
		return unix.EIO
	}
	if f.conflict {
		return unix.EEXIST
	}
	if f.failStep == "rename" {
		if f.renameAfterFailure {
			f.temporary, f.final = false, true
		}
		return unix.EIO
	}
	f.temporary, f.final = false, true
	return nil
}

func (f *fakeAuthorityMutationOps) unlink(parent int, name string) error {
	f.calls = append(f.calls, "unlink")
	if parent != f.directoryFD {
		return unix.EIO
	}
	if name == "authority" {
		if !f.final {
			return unix.ENOENT
		}
		if f.failStep == "unlink" {
			if f.unlinkAfterFailure {
				f.final = false
			}
			return unix.EIO
		}
		f.final = false
		return nil
	}
	if f.temporary {
		f.temporary = false
		return nil
	}
	return unix.ENOENT
}

func TestAuthorityPublishIsAtomicDurableAndReadOnly(t *testing.T) {
	ops := newFakeAuthorityMutationOps()
	ops.partialWrite = 17
	encoded := bytes.Repeat([]byte{0x5a}, authoritySize)
	nonce := [16]byte{1}
	if err := writeAuthorityFileWithOps(context.Background(), ops, ops.directoryFD, "final", nonce, encoded); err != nil {
		t.Fatal(err)
	}
	if !ops.final || ops.temporary || ops.mode != 0o444 || !bytes.Equal(ops.data, encoded) {
		t.Fatalf("final=%v temp=%v mode=%#o bytes=%d", ops.final, ops.temporary, ops.mode, len(ops.data))
	}
	wantSuffix := []string{"fsync-file-1", "chmod", "fsync-file-2", "close", "rename", "fsync-directory"}
	if len(ops.calls) < len(wantSuffix) || !equalStrings(ops.calls[len(ops.calls)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("calls=%v", ops.calls)
	}
}

func TestAuthorityPublishFailureMatrixReturnsNoAuthority(t *testing.T) {
	encoded := bytes.Repeat([]byte{0x5a}, authoritySize)
	nonce := [16]byte{1}
	for _, step := range []string{"create", "write", "fsync-file-1", "chmod", "fsync-file-2", "close", "rename", "fsync-directory"} {
		t.Run(step, func(t *testing.T) {
			ops := newFakeAuthorityMutationOps()
			ops.failStep = step
			err := writeAuthorityFileWithOps(context.Background(), ops, ops.directoryFD, "final", nonce, encoded)
			if !errors.Is(err, ErrFilesystem) {
				t.Fatalf("err=%v calls=%v", err, ops.calls)
			}
			if step != "fsync-directory" && ops.final {
				t.Fatalf("failed step retained final authority: %v", ops.calls)
			}
			if step != "create" && step != "fsync-directory" && ops.temporary {
				t.Fatalf("failed step retained temporary: %v", ops.calls)
			}
		})
	}

	ops := newFakeAuthorityMutationOps()
	ops.failStep, ops.renameAfterFailure = "rename", true
	if err := writeAuthorityFileWithOps(context.Background(), ops, ops.directoryFD, "final", nonce, encoded); !errors.Is(err, ErrFilesystem) || !ops.final || ops.temporary || !containsString(ops.calls, "fsync-directory") {
		t.Fatalf("rename response loss err=%v final=%v temp=%v calls=%v", err, ops.final, ops.temporary, ops.calls)
	}

	ops = newFakeAuthorityMutationOps()
	ops.conflict, ops.final = true, true
	if err := writeAuthorityFileWithOps(context.Background(), ops, ops.directoryFD, "final", nonce, encoded); !errors.Is(err, ErrConflict) || !ops.final || ops.temporary {
		t.Fatalf("conflict err=%v final=%v temp=%v calls=%v", err, ops.final, ops.temporary, ops.calls)
	}
}

func TestAuthorityPublishCancellationBeforeMutation(t *testing.T) {
	ops := newFakeAuthorityMutationOps()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := writeAuthorityFileWithOps(ctx, ops, ops.directoryFD, "final", [16]byte{1}, bytes.Repeat([]byte{1}, authoritySize))
	if !errors.Is(err, context.Canceled) || len(ops.calls) != 0 || ops.final || ops.temporary {
		t.Fatalf("err=%v calls=%v final=%v temp=%v", err, ops.calls, ops.final, ops.temporary)
	}
}

func TestAuthorityRevokeSyncsEvenAfterResponseLoss(t *testing.T) {
	for _, test := range []struct {
		name        string
		failStep    string
		unlinkAfter bool
		wantError   error
	}{
		{name: "success"},
		{name: "unlink-before", failStep: "unlink", wantError: ErrFilesystem},
		{name: "unlink-after", failStep: "unlink", unlinkAfter: true, wantError: ErrFilesystem},
		{name: "sync", failStep: "fsync-directory", wantError: ErrFilesystem},
	} {
		t.Run(test.name, func(t *testing.T) {
			ops := newFakeAuthorityMutationOps()
			ops.final, ops.failStep, ops.unlinkAfterFailure = true, test.failStep, test.unlinkAfter
			err := revokeAuthorityEntry(context.Background(), ops, ops.directoryFD, "authority")
			if test.wantError == nil && err != nil || test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("err=%v calls=%v", err, ops.calls)
			}
			if !containsString(ops.calls, "fsync-directory") {
				t.Fatalf("directory was not synced: %v", ops.calls)
			}
		})
	}

	ops := newFakeAuthorityMutationOps()
	if err := revokeAuthorityEntry(context.Background(), ops, ops.directoryFD, "authority"); !errors.Is(err, ErrUnavailable) || containsString(ops.calls, "fsync-directory") {
		t.Fatalf("absent err=%v calls=%v", err, ops.calls)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
