package migration

import (
	"context"
	"io"
	"os"
	"syscall"
)

// FileArtifactSource is a bounded outer-artifact reader. Runner invokes it only
// after a trust decision exists and LoadRuntimeBundle verifies the digest again.
type FileArtifactSource struct{ Path string }

func (source FileArtifactSource) Read(ctx context.Context, expected Digest) ([]byte, error) {
	if err := expected.Validate(); err != nil {
		return nil, fail(CodeUntrusted, "artifact-source", "expected outer digest is invalid", err)
	}
	info, err := os.Lstat(source.Path)
	if err != nil {
		return nil, fail(CodeInvalidArtifact, "artifact-source", "cannot inspect outer artifact", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > maxRuntimeTarSize {
		return nil, fail(CodeInvalidArtifact, "artifact-source", "outer artifact is not a bounded regular file", nil)
	}
	fd, err := syscall.Open(source.Path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fail(CodeInvalidArtifact, "artifact-source", "cannot open outer artifact without following links", err)
	}
	file := os.NewFile(uintptr(fd), source.Path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, fail(CodeInvalidArtifact, "artifact-source", "cannot bind outer artifact file descriptor", nil)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
		return nil, fail(CodeInvalidArtifact, "artifact-source", "outer artifact changed while opening", err)
	}
	reader := io.LimitReader(file, maxRuntimeTarSize+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fail(CodeInvalidArtifact, "artifact-source", "cannot read outer artifact", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > maxRuntimeTarSize || DigestBytes(data) != expected {
		return nil, fail(CodeUntrusted, "artifact-source", "outer artifact digest mismatch", nil)
	}
	return data, nil
}
