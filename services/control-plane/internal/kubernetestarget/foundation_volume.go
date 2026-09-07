package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"

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

func (input FoundationWorkspaceVolume) annotations() map[string]string {
	return map[string]string{
		"cloud-agents.dev/tenant": input.TenantID, "cloud-agents.dev/project": input.ProjectID,
		"cloud-agents.dev/target": input.TargetID, "cloud-agents.dev/workspace": input.WorkspaceID,
	}
}

func (directory *CredentialDirectory) EnsureFoundationWorkspaceVolume(
	ctx context.Context, endpoint, credentialRef string, input FoundationWorkspaceVolume,
) (string, error) {
	return directory.foundationWorkspaceVolume(ctx, endpoint, credentialRef, input, true)
}

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
	namespace, err := directory.foundationNamespace(credentialRef)
	if err != nil {
		return "", err
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return "", err
	}
	defer transport.CloseIdleConnections()
	name := input.Name()
	path := base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(name)
	current, exists, err := inspectFoundationWorkspaceVolume(ctx, client, path)
	if err != nil {
		return "", err
	}
	if !exists {
		if !create {
			return "", ErrDeploymentConflict
		}
		body := map[string]any{
			"apiVersion": "v1", "kind": "PersistentVolumeClaim",
			"metadata": map[string]any{"name": name, "namespace": namespace,
				"labels":      map[string]string{"cloud-agents.dev/managed": "true", "cloud-agents.dev/resource": "foundation-workspace"},
				"annotations": input.annotations()},
			"spec": map[string]any{"accessModes": []string{"ReadWriteOnce"}, "resources": map[string]any{"requests": map[string]string{"storage": workspaceStorage}}},
		}
		status, createErr := kubernetesJSON(ctx, client, http.MethodPost,
			base+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/persistentvolumeclaims", "application/json", body, &current)
		if createErr != nil || status != http.StatusCreated {
			if status != http.StatusConflict {
				return "", ErrDeploymentFailed
			}
			current, exists, err = inspectFoundationWorkspaceVolume(ctx, client, path)
			if err != nil || !exists {
				return "", ErrDeploymentFailed
			}
		}
	}
	if current.Metadata.Name != name || current.Metadata.Namespace != namespace ||
		!ownedResource(current.Metadata, name, namespace, input.annotations()) {
		return "", ErrDeploymentConflict
	}
	return name, nil
}

func inspectFoundationWorkspaceVolume(ctx context.Context, client *http.Client, path string) (resource, bool, error) {
	var current resource
	status, err := kubernetesJSON(ctx, client, http.MethodGet, path, "", nil, &current)
	if status == http.StatusNotFound {
		return resource{}, false, nil
	}
	if err != nil || status != http.StatusOK {
		return resource{}, false, ErrDeploymentFailed
	}
	return current, true, nil
}

func (directory *CredentialDirectory) foundationNamespace(credentialRef string) (string, error) {
	if directory == nil || commonv1alpha1.ValidateIdentifier(credentialRef, "/credentialRef") != nil {
		return "", ErrDeploymentConfigInvalid
	}
	root, err := os.OpenRoot(directory.path)
	if err != nil {
		return "", ErrDeploymentConfigUnavailable
	}
	defer root.Close()
	value, err := readCredential(root, credentialRef+".foundation.json")
	if err != nil {
		return "", ErrDeploymentConfigUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var config struct {
		Namespace string `json:"namespace"`
	}
	if decoder.Decode(&config) != nil || decoder.Decode(&struct{}{}) != io.EOF || !dnsLabelPattern.MatchString(config.Namespace) {
		return "", ErrDeploymentConfigInvalid
	}
	return config.Namespace, nil
}
