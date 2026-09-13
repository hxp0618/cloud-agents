package dockertarget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const portableSnapshotPrefix = "ca-portable-snapshot-"

// FoundationSnapshotArchiveDirectory is a deployment-mounted, persistent
// directory shared by every controller that may reconcile Workspace snapshots.
type FoundationSnapshotArchiveDirectory struct{ path string }

func NewFoundationSnapshotArchiveDirectory(path string) (*FoundationSnapshotArchiveDirectory, error) {
	if path == "" || filepath.Clean(path) != path || !filepath.IsAbs(path) {
		return nil, ErrDeploymentConfigInvalid
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil, ErrDeploymentConfigUnavailable
	}
	return &FoundationSnapshotArchiveDirectory{path: path}, nil
}

func IsPortableFoundationSnapshot(id string) bool {
	return len(id) == 64 && len(id) > len(portableSnapshotPrefix) && id[:len(portableSnapshotPrefix)] == portableSnapshotPrefix
}

func portableSnapshotID(input FoundationWorkspaceSnapshot) string {
	hash := sha256.New()
	for _, value := range []string{input.TenantID, input.ProjectID, input.WorkspaceID, input.SnapshotID, "portable-archive"} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return portableSnapshotPrefix + hex.EncodeToString(hash.Sum(nil))[:64-len(portableSnapshotPrefix)]
}

func (directory *FoundationSnapshotArchiveDirectory) Put(input FoundationWorkspaceSnapshot, archive []byte, contentDigest string) (string, error) {
	if directory == nil || !input.valid() || len(archive) > maxFoundationSnapshotBytes || !foundationSnapshotDigestRE.MatchString(contentDigest) {
		return "", ErrDeploymentConfigInvalid
	}
	actual, err := snapshotArchiveDigest(archive)
	if err != nil || actual != contentDigest {
		return "", ErrDeploymentConflict
	}
	id := portableSnapshotID(input)
	path := filepath.Join(directory.path, id+".tar")
	if existing, readErr := directory.read(path, contentDigest); readErr == nil {
		if !bytes.Equal(existing, archive) {
			return "", ErrDeploymentConflict
		}
		return id, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return "", readErr
	}
	temporary, err := os.CreateTemp(directory.path, ".snapshot-*.tmp")
	if err != nil {
		return "", ErrDeploymentFailed
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(archive)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", ErrDeploymentFailed
	}
	if err = os.Link(temporaryPath, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", ErrDeploymentFailed
		}
		existing, readErr := directory.read(path, contentDigest)
		if readErr != nil || !bytes.Equal(existing, archive) {
			return "", ErrDeploymentConflict
		}
	}
	return id, nil
}

func (directory *FoundationSnapshotArchiveDirectory) Read(input FoundationWorkspaceRestore) ([]byte, error) {
	if directory == nil || !portableRestoreValid(input) || input.SnapshotVolumeName != portableRestoreSnapshotID(input) {
		return nil, ErrDeploymentConfigInvalid
	}
	archive, err := directory.read(filepath.Join(directory.path, input.SnapshotVolumeName+".tar"), input.ContentDigest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrDeploymentConflict
	}
	return archive, err
}

func (directory *FoundationSnapshotArchiveDirectory) Remove(input FoundationWorkspaceSnapshotCleanup) error {
	if directory == nil || !portableCleanupValid(input) {
		return ErrDeploymentConfigInvalid
	}
	path := filepath.Join(directory.path, input.PhysicalSnapshotID+".tar")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrDeploymentFailed
	}
	return nil
}

func (directory *FoundationSnapshotArchiveDirectory) read(path, contentDigest string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > maxFoundationSnapshotBytes {
		return nil, ErrDeploymentConflict
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	defer file.Close()
	archive, err := io.ReadAll(io.LimitReader(file, maxFoundationSnapshotBytes+1))
	if err != nil || len(archive) > maxFoundationSnapshotBytes {
		return nil, ErrDeploymentFailed
	}
	digest, err := snapshotArchiveDigest(archive)
	if err != nil || digest != contentDigest {
		return nil, ErrDeploymentConflict
	}
	return archive, nil
}

func portableRestoreSnapshotID(input FoundationWorkspaceRestore) string {
	return portableSnapshotID(FoundationWorkspaceSnapshot{TenantID: input.TenantID, ProjectID: input.ProjectID,
		WorkspaceID: input.SourceWorkspaceID, SnapshotID: input.SnapshotID})
}

func portableRestoreValid(input FoundationWorkspaceRestore) bool {
	for path, value := range map[string]string{
		"/tenantId": input.TenantID, "/projectId": input.ProjectID, "/targetId": input.TargetID,
		"/sourceWorkspaceId": input.SourceWorkspaceID, "/snapshotId": input.SnapshotID, "/workspaceId": input.WorkspaceID,
	} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return false
		}
	}
	return foundationSnapshotDigestRE.MatchString(input.ContentDigest) && foundationSnapshotImageRE.MatchString(input.ImageURI) && len(input.ImageURI) <= 1024
}

func portableCleanupValid(input FoundationWorkspaceSnapshotCleanup) bool {
	for path, value := range map[string]string{"/tenantId": input.TenantID, "/projectId": input.ProjectID,
		"/sourceWorkspaceId": input.SourceWorkspaceID, "/snapshotId": input.SnapshotID,
		"/physicalSnapshotId": input.PhysicalSnapshotID} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return false
		}
	}
	expected := portableSnapshotID(FoundationWorkspaceSnapshot{TenantID: input.TenantID, ProjectID: input.ProjectID,
		WorkspaceID: input.SourceWorkspaceID, SnapshotID: input.SnapshotID})
	return input.PhysicalSnapshotID == expected
}
