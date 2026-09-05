package dockertarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	commonv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1"
)

type FoundationWorkspaceVolume struct {
	TenantID, ProjectID, TargetID, WorkspaceID string
}

func (input FoundationWorkspaceVolume) valid() bool {
	for path, value := range map[string]string{
		"/tenantId": input.TenantID, "/projectId": input.ProjectID,
		"/targetId": input.TargetID, "/workspaceId": input.WorkspaceID,
	} {
		if commonv1alpha1.ValidateIdentifier(value, path) != nil {
			return false
		}
	}
	return true
}

func (input FoundationWorkspaceVolume) Name() string {
	if !input.valid() {
		return ""
	}
	hash := sha256.New()
	for _, value := range []string{input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return "ca-ws-" + hex.EncodeToString(hash.Sum(nil))[:56]
}

func (input FoundationWorkspaceVolume) labels() map[string]string {
	return map[string]string{
		"cloud-agents.dev/managed":   "true",
		"cloud-agents.dev/resource":  "foundation-workspace",
		"cloud-agents.dev/tenant":    input.TenantID,
		"cloud-agents.dev/project":   input.ProjectID,
		"cloud-agents.dev/target":    input.TargetID,
		"cloud-agents.dev/workspace": input.WorkspaceID,
	}
}

// EnsureFoundationWorkspaceVolume creates or adopts only the deterministic,
// exact-owner Docker volume. It never removes retained Workspace data.
func (directory *CredentialDirectory) EnsureFoundationWorkspaceVolume(
	ctx context.Context, endpoint, credentialRef string, input FoundationWorkspaceVolume,
) (string, error) {
	return directory.foundationWorkspaceVolume(ctx, endpoint, credentialRef, input, true)
}

// VerifyFoundationWorkspaceVolume fails closed when retained data is absent or
// its physical owner labels drift. Stop must never recreate an empty volume.
func (directory *CredentialDirectory) VerifyFoundationWorkspaceVolume(
	ctx context.Context, endpoint, credentialRef string, input FoundationWorkspaceVolume,
) (string, error) {
	return directory.foundationWorkspaceVolume(ctx, endpoint, credentialRef, input, false)
}

func (directory *CredentialDirectory) foundationWorkspaceVolume(
	ctx context.Context, endpoint, credentialRef string, input FoundationWorkspaceVolume, create bool,
) (string, error) {
	if ctx == nil || !input.valid() {
		return "", ErrDeploymentConfigInvalid
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return "", err
	}
	defer transport.CloseIdleConnections()
	name := input.Name()
	volume, exists, err := inspectWorkspaceVolume(ctx, client, base, name)
	if err != nil {
		return "", err
	}
	if !exists {
		if !create {
			return "", ErrDeploymentConflict
		}
		body := map[string]any{"Name": name, "Labels": input.labels()}
		if err := dockerJSON(ctx, client, http.MethodPost, base+"/volumes/create", body, http.StatusCreated, &volume); err != nil {
			volume, exists, err = inspectWorkspaceVolume(ctx, client, base, name)
			if err != nil || !exists {
				return "", ErrDeploymentFailed
			}
		}
	}
	if volume.Name != name || !exactLabels(volume.Labels, input.labels()) {
		return "", ErrDeploymentConflict
	}
	return name, nil
}
