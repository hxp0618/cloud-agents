package dockertarget

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

const maxFoundationSnapshotBytes = 64 << 20

var (
	ErrSnapshotTooLarge       = errors.New("foundation workspace snapshot exceeds the initial size limit")
	foundationSnapshotImageRE = regexp.MustCompile(`^[A-Za-z0-9._:/-]+@sha256:[0-9a-f]{64}$`)
)

type FoundationWorkspaceSnapshot struct {
	TenantID, ProjectID, TargetID, WorkspaceID string
	SnapshotID, SourceVolumeName, ImageURI     string
}

type FoundationWorkspaceSnapshotResult struct {
	VolumeName, ContentDigest string
	SizeBytes                 int64
	CleanupComplete           bool
}

func (input FoundationWorkspaceSnapshot) valid() bool {
	for path, value := range map[string]string{
		"/tenantId": input.TenantID, "/projectId": input.ProjectID, "/targetId": input.TargetID,
		"/workspaceId": input.WorkspaceID, "/snapshotId": input.SnapshotID,
	} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return false
		}
	}
	return input.SourceVolumeName == (FoundationWorkspaceVolume{
		TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID,
	}).Name() && foundationSnapshotImageRE.MatchString(input.ImageURI) && len(input.ImageURI) <= 1024
}

func (input FoundationWorkspaceSnapshot) Name() string {
	if !input.valid() {
		return ""
	}
	return input.name("ca-snap-", "snapshot")
}

func (input FoundationWorkspaceSnapshot) helperName() string {
	return input.name("ca-snap-copy-", "helper")
}

func (input FoundationWorkspaceSnapshot) name(prefix, purpose string) string {
	hash := sha256.New()
	for _, value := range []string{input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, purpose} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return prefix + hex.EncodeToString(hash.Sum(nil))[:64-len(prefix)]
}

func (input FoundationWorkspaceSnapshot) labels() map[string]string {
	return map[string]string{
		"cloud-agents.dev/managed":          "true",
		"cloud-agents.dev/resource":         "foundation-workspace-snapshot",
		"cloud-agents.dev/tenant":           input.TenantID,
		"cloud-agents.dev/project":          input.ProjectID,
		"cloud-agents.dev/target":           input.TargetID,
		"cloud-agents.dev/workspace":        input.WorkspaceID,
		"cloud-agents.dev/snapshot":         input.SnapshotID,
		"cloud-agents.dev/source-volume":    input.SourceVolumeName,
		"cloud-agents.dev/consistency-mode": "offline",
	}
}

// SnapshotFoundationWorkspace copies only the fenced Workspace volume through
// the Docker archive API. No helper process runs and no credential volume is mounted.
func (directory *CredentialDirectory) SnapshotFoundationWorkspace(
	ctx context.Context, endpoint, credentialRef string, input FoundationWorkspaceSnapshot,
) (result FoundationWorkspaceSnapshotResult, err error) {
	if ctx == nil || !input.valid() {
		return result, ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return result, err
	}
	defer transport.CloseIdleConnections()
	result.VolumeName = input.Name()
	labels := input.labels()
	helpersClean := true
	defer func() {
		if cleanupErr := removeSnapshotHelper(ctx, client, base, input.helperName(), labels); cleanupErr != nil {
			helpersClean = false
			if err == nil {
				err = cleanupErr
			}
		} else {
			helpersClean = true
		}
		if err != nil {
			if cleanupErr := removeSnapshotVolume(ctx, client, base, result.VolumeName, labels); cleanupErr != nil {
				helpersClean = false
			}
		}
		result.CleanupComplete = helpersClean
	}()

	source := FoundationWorkspaceVolume{TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID}
	if volume, exists, inspectErr := inspectWorkspaceVolume(ctx, client, base, input.SourceVolumeName); inspectErr != nil || !exists || !exactLabels(volume.Labels, source.labels()) {
		return result, ErrDeploymentConflict
	}
	if err = removeSnapshotHelper(ctx, client, base, input.helperName(), labels); err != nil {
		return result, err
	}
	if err = resetSnapshotVolume(ctx, client, base, result.VolumeName, labels); err != nil {
		return result, err
	}
	helpersClean = false
	if err = createSnapshotHelper(ctx, client, base, input, labels); err != nil {
		return result, fmt.Errorf("create stopped snapshot helper: %w", err)
	}
	archive, err := readDockerArchive(ctx, client, base, input.helperName(), "/source/.")
	if err != nil {
		return result, fmt.Errorf("read source archive: %w", err)
	}
	if err = writeDockerArchive(ctx, client, base, input.helperName(), "/snapshot", archive); err != nil {
		return result, fmt.Errorf("write snapshot archive: %w", err)
	}
	verified, err := readDockerArchive(ctx, client, base, input.helperName(), "/snapshot/.")
	if err != nil {
		return result, fmt.Errorf("verify snapshot archive: %w", ErrDeploymentFailed)
	}
	digest, err := snapshotArchiveDigest(archive)
	verifiedDigest, verifyErr := snapshotArchiveDigest(verified)
	if err != nil || verifyErr != nil || digest != verifiedDigest {
		return result, fmt.Errorf("verify snapshot archive: %w", ErrDeploymentFailed)
	}
	result.ContentDigest = digest
	result.SizeBytes = int64(len(archive))
	return result, nil
}

type snapshotArchiveEntry struct {
	Name, Link, Digest string
	Type               byte
	Mode, Size         int64
}

func snapshotArchiveDigest(archive []byte) (string, error) {
	reader := tar.NewReader(bytes.NewReader(archive))
	entries := make([]snapshotArchiveEntry, 0)
	seen := make(map[string]struct{})
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", ErrDeploymentFailed
		}
		name := path.Clean(strings.TrimPrefix(header.Name, "./"))
		if name == "." || name == "" {
			continue
		}
		if strings.HasPrefix(name, "../") || path.IsAbs(name) {
			return "", ErrDeploymentConflict
		}
		if _, exists := seen[name]; exists {
			return "", ErrDeploymentConflict
		}
		seen[name] = struct{}{}
		entry := snapshotArchiveEntry{Name: name, Type: header.Typeflag, Mode: header.Mode, Size: header.Size}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA:
			content := sha256.New()
			if copied, err := io.Copy(content, reader); err != nil || copied != header.Size {
				return "", ErrDeploymentFailed
			}
			entry.Digest = "sha256:" + hex.EncodeToString(content.Sum(nil))
		case tar.TypeDir:
			entry.Size = 0
		case tar.TypeSymlink, tar.TypeLink:
			entry.Link = header.Linkname
			entry.Size = 0
		default:
			return "", ErrDeploymentConflict
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name < entries[right].Name })
	canonical, err := json.Marshal(entries)
	if err != nil {
		return "", ErrDeploymentFailed
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func resetSnapshotVolume(ctx context.Context, client *http.Client, base, name string, labels map[string]string) error {
	if err := removeSnapshotVolume(ctx, client, base, name, labels); err != nil {
		return err
	}
	var created volumeInspect
	if err := dockerJSON(ctx, client, http.MethodPost, base+"/volumes/create", map[string]any{"Name": name, "Labels": labels}, http.StatusCreated, &created); err != nil {
		return err
	}
	if created.Name != name || !exactLabels(created.Labels, labels) {
		return ErrDeploymentConflict
	}
	return nil
}

func removeSnapshotVolume(ctx context.Context, client *http.Client, base, name string, labels map[string]string) error {
	volume, exists, err := inspectWorkspaceVolume(ctx, client, base, name)
	if err != nil || !exists {
		return err
	}
	if !exactLabels(volume.Labels, labels) {
		return ErrDeploymentConflict
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/volumes/"+url.PathEscape(name), http.NoBody)
	if err != nil {
		return ErrDeploymentFailed
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return ErrDeploymentFailed
	}
	return nil
}

func createSnapshotHelper(ctx context.Context, client *http.Client, base string, input FoundationWorkspaceSnapshot, labels map[string]string) error {
	body := map[string]any{
		"Image": input.ImageURI, "Labels": labels,
		"HostConfig": map[string]any{"Binds": []string{input.SourceVolumeName + ":/source:ro", input.Name() + ":/snapshot"}},
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := dockerJSON(ctx, client, http.MethodPost, base+"/containers/create?name="+url.QueryEscape(input.helperName()), body, http.StatusCreated, &created); err != nil || created.ID == "" {
		return ErrDeploymentFailed
	}
	return nil
}

func removeSnapshotHelper(ctx context.Context, client *http.Client, base, name string, labels map[string]string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/containers/"+url.PathEscape(name)+"/json", http.NoBody)
	if err != nil {
		return ErrDeploymentFailed
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return nil
	}
	var helper containerInspect
	if response.StatusCode != http.StatusOK || decodeDockerJSON(response.Body, &helper) != nil {
		response.Body.Close()
		return ErrDeploymentFailed
	}
	response.Body.Close()
	if !exactLabels(helper.Config.Labels, labels) {
		return ErrDeploymentConflict
	}
	return removeWorkerContainer(ctx, client, base, name)
}

func readDockerArchive(ctx context.Context, client *http.Client, base, container, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/containers/"+url.PathEscape(container)+"/archive?path="+url.QueryEscape(path), http.NoBody)
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
		return nil, ErrDeploymentFailed
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, maxFoundationSnapshotBytes+1))
	if err != nil {
		return nil, ErrDeploymentFailed
	}
	if len(archive) > maxFoundationSnapshotBytes {
		return nil, ErrSnapshotTooLarge
	}
	return archive, nil
}

func writeDockerArchive(ctx context.Context, client *http.Client, base, container, path string, archive []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, base+"/containers/"+url.PathEscape(container)+"/archive?path="+url.QueryEscape(path), bytes.NewReader(archive))
	if err != nil {
		return ErrDeploymentFailed
	}
	request.Header.Set("Content-Type", "application/x-tar")
	response, err := client.Do(request)
	if err != nil {
		return ErrDeploymentFailed
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxDockerBodyBytes))
	if response.StatusCode != http.StatusOK {
		return ErrDeploymentFailed
	}
	return nil
}
