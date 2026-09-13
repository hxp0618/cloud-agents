package kubernetestarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
)

const snapshotPodTimeout = 2 * time.Minute
const snapshotExecBufferSize = 32 * 1024

const snapshotExportCommand = "cd /workspace && find . -mindepth 1 -print0 | tar --null --no-recursion -cf - -T -"

type snapshotPod struct {
	Metadata resourceMetadata `json:"metadata"`
	Status   struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func (directory *CredentialDirectory) SnapshotFoundationWorkspacePortable(
	ctx context.Context, endpoint, credentialRef string, input dockertarget.FoundationWorkspaceSnapshot,
	archives *dockertarget.FoundationSnapshotArchiveDirectory,
) (result dockertarget.FoundationWorkspaceSnapshotResult, err error) {
	if ctx == nil || archives == nil || input.Name() == "" || input.SourceVolumeName == "" {
		return result, ErrDeploymentConfigInvalid
	}
	config, err := directory.foundationConfig(credentialRef)
	if err != nil {
		return result, err
	}
	namespace := config.Namespace
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return result, err
	}
	defer transport.CloseIdleConnections()
	volumePath := base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(input.SourceVolumeName)
	var volume resource
	status, err := kubernetesJSON(ctx, client, http.MethodGet, volumePath, "", nil, &volume)
	if err != nil || status != http.StatusOK || !ownedResource(volume.Metadata, input.SourceVolumeName, namespace, workspaceAnnotations(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID)) {
		return result, ErrDeploymentConflict
	}
	podName := snapshotPodName(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, "export")
	labels := snapshotPodLabels(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID)
	if err = createSnapshotPod(ctx, client, base, namespace, podName, input.ImageURI, input.SourceVolumeName, labels, config.NodeName); err != nil {
		return result, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		cleanupErr := deleteOwnedResource(cleanupCtx, client, base+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(podName), namespace, podName, labels)
		cancel()
		result.CleanupComplete = cleanupErr == nil
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()
	if err = waitSnapshotPod(ctx, client, base, namespace, podName); err != nil {
		return result, err
	}
	archive, err := execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, podName, []string{"/bin/sh", "-c", snapshotExportCommand}, nil)
	if err != nil {
		return result, err
	}
	digest, err := dockertarget.SnapshotArchiveDigest(archive)
	if err != nil {
		return result, err
	}
	result.VolumeName, err = archives.Put(input, archive, digest)
	if err != nil {
		return result, err
	}
	result.ContentDigest, result.SizeBytes = digest, int64(len(archive))
	return result, nil
}

func (directory *CredentialDirectory) RestoreFoundationWorkspacePortable(
	ctx context.Context, endpoint, credentialRef string, input dockertarget.FoundationWorkspaceRestore,
	archives *dockertarget.FoundationSnapshotArchiveDirectory,
) (result dockertarget.FoundationWorkspaceRestoreResult, err error) {
	if ctx == nil || archives == nil || input.SnapshotVolumeName == "" {
		return result, ErrDeploymentConfigInvalid
	}
	archive, err := archives.Read(input)
	if err != nil {
		return result, err
	}
	config, err := directory.foundationConfig(credentialRef)
	if err != nil {
		return result, err
	}
	namespace := config.Namespace
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		return result, err
	}
	defer transport.CloseIdleConnections()
	workspace := FoundationWorkspaceVolume{TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID}
	volumePath := base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(workspace.Name())
	var existing resource
	status, err := kubernetesJSON(ctx, client, http.MethodGet, volumePath, "", nil, &existing)
	if err != nil && status != http.StatusNotFound {
		return result, ErrDeploymentFailed
	}
	volumeExists := status == http.StatusOK
	result.VolumeName, err = directory.EnsureFoundationWorkspaceVolume(ctx, endpoint, credentialRef, workspace)
	if err != nil {
		return result, err
	}
	volumePath = base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/persistentvolumeclaims/" + url.PathEscape(result.VolumeName)
	var volume resource
	status, err = kubernetesJSON(ctx, client, http.MethodGet, volumePath, "", nil, &volume)
	if err != nil || status != http.StatusOK {
		return result, ErrDeploymentFailed
	}
	labels := snapshotRestorePodLabels(input)
	podName := snapshotPodName(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, "restore")
	if err = createSnapshotPod(ctx, client, base, namespace, podName, input.ImageURI, result.VolumeName, labels, config.NodeName); err != nil {
		return result, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		cleanupErr := deleteOwnedResource(cleanupCtx, client, base+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(podName), namespace, podName, labels)
		cancel()
		result.CleanupComplete = cleanupErr == nil
		if err == nil && cleanupErr != nil {
			err = cleanupErr
		}
	}()
	if err = waitSnapshotPod(ctx, client, base, namespace, podName); err != nil {
		return result, err
	}
	current, err := execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, podName, []string{"/bin/sh", "-c", snapshotExportCommand}, nil)
	if err != nil {
		return result, err
	}
	currentDigest, err := dockertarget.SnapshotArchiveDigest(current)
	if err != nil {
		return result, err
	}
	if volumeExists && currentDigest != input.ContentDigest {
		return result, dockertarget.ErrDeploymentConflict
	}
	if !volumeExists {
		if _, err = execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, podName, []string{"/bin/sh", "-c", fmt.Sprintf("head -c %d | tar -xf - -C /workspace --no-same-owner --same-permissions --no-overwrite-dir", len(archive))}, archive); err != nil {
			return result, err
		}
	}
	verified, err := execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, podName, []string{"/bin/sh", "-c", snapshotExportCommand}, nil)
	if err != nil {
		return result, err
	}
	verifiedDigest, err := dockertarget.SnapshotArchiveDigest(verified)
	if err != nil {
		return result, fmt.Errorf("%w: restored archive digest: %v", dockertarget.ErrDeploymentFailed, err)
	}
	if verifiedDigest != input.ContentDigest {
		return result, fmt.Errorf("%w: restored archive digest %s (bytes=%d) does not match %s (bytes=%d)", dockertarget.ErrDeploymentFailed, verifiedDigest, len(verified), input.ContentDigest, len(archive))
	}
	result.ContentDigest = verifiedDigest
	return result, nil
}

func workspaceAnnotations(tenant, project, target, workspace string) map[string]string {
	return map[string]string{"cloud-agents.dev/tenant": tenant, "cloud-agents.dev/project": project, "cloud-agents.dev/target": target, "cloud-agents.dev/workspace": workspace}
}

func snapshotPodLabels(tenant, project, target, workspace, snapshot string) map[string]string {
	labels := workspaceAnnotations(tenant, project, target, workspace)
	labels["cloud-agents.dev/managed"] = "true"
	labels["cloud-agents.dev/resource"] = "foundation-workspace-snapshot-helper"
	labels["cloud-agents.dev/snapshot"] = snapshot
	return labels
}

func snapshotRestorePodLabels(input dockertarget.FoundationWorkspaceRestore) map[string]string {
	return snapshotPodLabels(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID)
}

func snapshotPodName(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return "ca-snap-helper-" + hex.EncodeToString(hash.Sum(nil))[:40]
}

func createSnapshotPod(ctx context.Context, client *http.Client, base, namespace, name, image, volumeName string, labels map[string]string, nodeNames ...string) error {
	nodeName := ""
	if len(nodeNames) > 0 {
		nodeName = nodeNames[0]
	}
	podSpec := map[string]any{
		"restartPolicy": "Never", "automountServiceAccountToken": false, "terminationGracePeriodSeconds": 1,
		"securityContext": map[string]any{"runAsNonRoot": true, "runAsUser": 1000, "runAsGroup": 1000, "fsGroup": 1000, "seccompProfile": map[string]string{"type": "RuntimeDefault"}},
		"containers":      []map[string]any{{"name": "workspace", "image": image, "imagePullPolicy": "IfNotPresent", "command": []string{"/bin/sh", "-c", "sleep 300"}, "securityContext": map[string]any{"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true, "capabilities": map[string]any{"drop": []string{"ALL"}}}, "volumeMounts": []map[string]any{{"name": "workspace", "mountPath": "/workspace"}, {"name": "tmp", "mountPath": "/tmp"}}}},
		"volumes":         []map[string]any{{"name": "workspace", "persistentVolumeClaim": map[string]string{"claimName": volumeName}}, {"name": "tmp", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "64Mi"}}},
	}
	if nodeName != "" {
		podSpec["nodeSelector"] = map[string]string{"kubernetes.io/hostname": nodeName}
	}
	body := map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": name, "namespace": namespace, "labels": labels, "annotations": labels},
		"spec":     podSpec,
	}
	status, err := kubernetesJSON(ctx, client, http.MethodPost, base+"/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods", "application/json", body, &snapshotPod{})
	if err != nil || status != http.StatusCreated {
		if status == http.StatusConflict {
			return ErrDeploymentConflict
		}
		return ErrDeploymentFailed
	}
	return nil
}

func waitSnapshotPod(ctx context.Context, client *http.Client, base, namespace, name string) error {
	waitCtx, cancel := context.WithTimeout(ctx, snapshotPodTimeout)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	path := base + "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(name)
	for {
		var pod snapshotPod
		status, err := kubernetesJSON(waitCtx, client, http.MethodGet, path, "", nil, &pod)
		if err != nil || status != http.StatusOK {
			return ErrDeploymentFailed
		}
		switch pod.Status.Phase {
		case "Running":
			return nil
		case "Failed", "Succeeded":
			return ErrDeploymentFailed
		}
		select {
		case <-waitCtx.Done():
			return ErrDeploymentFailed
		case <-ticker.C:
		}
	}
}

func execSnapshotPod(ctx context.Context, directory *CredentialDirectory, endpoint, credentialRef, namespace, pod string, command []string, input []byte) ([]byte, error) {
	if len(command) == 0 {
		return nil, ErrDeploymentConfigInvalid
	}
	roots, token, err := directory.credentials(credentialRef)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, ErrInvalidEndpoint
	}
	parsed.Scheme = "wss"
	parsed.Path = "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(pod) + "/exec"
	query := parsed.Query()
	for _, value := range command {
		query.Add("command", value)
	}
	query.Set("stdin", boolQuery(input != nil))
	query.Set("stdout", "1")
	query.Set("stderr", "1")
	query.Set("tty", "0")
	query.Set("container", "workspace")
	parsed.RawQuery = query.Encode()
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, ReadBufferSize: snapshotExecBufferSize + 1024, WriteBufferSize: snapshotExecBufferSize + 1024, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, Subprotocols: []string{"v5.channel.k8s.io", "v4.channel.k8s.io"}}
	connection, response, err := dialer.DialContext(ctx, parsed.String(), http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		status := ""
		if response != nil {
			status = response.Status
			response.Body.Close()
		}
		return nil, fmt.Errorf("%w: exec websocket handshake (%s)", ErrDeploymentFailed, status)
	}
	defer connection.Close()
	protocol := connection.Subprotocol()
	if protocol != "v5.channel.k8s.io" && protocol != "v4.channel.k8s.io" {
		return nil, fmt.Errorf("%w: exec websocket protocol %q", ErrDeploymentFailed, protocol)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
	}
	if input != nil {
		for offset := 0; offset < len(input); {
			end := offset + snapshotExecBufferSize
			if end > len(input) {
				end = len(input)
			}
			payload := append([]byte{0}, input[offset:end]...)
			if err := connection.WriteMessage(websocket.BinaryMessage, payload); err != nil {
				return nil, fmt.Errorf("%w: exec stdin offset=%d: %v", ErrDeploymentFailed, offset, err)
			}
			offset = end
		}
		if protocol == "v5.channel.k8s.io" {
			if err := connection.WriteMessage(websocket.BinaryMessage, []byte{255, 0}); err != nil {
				return nil, fmt.Errorf("%w: exec stdin close", ErrDeploymentFailed)
			}
		}
	}
	var stdout, stderr bytes.Buffer
	for {
		messageType, payload, readErr := connection.ReadMessage()
		if errors.Is(readErr, io.EOF) || websocket.IsCloseError(readErr, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("%w: exec read: %v", ErrDeploymentFailed, readErr)
		}
		if messageType != websocket.BinaryMessage || len(payload) == 0 {
			return nil, fmt.Errorf("%w: exec frame type %d size %d", ErrDeploymentFailed, messageType, len(payload))
		}
		switch payload[0] {
		case 1:
			if stdout.Len()+len(payload)-1 > dockertarget.FoundationSnapshotMaxBytes {
				return nil, dockertarget.ErrSnapshotTooLarge
			}
			_, _ = stdout.Write(payload[1:])
		case 2:
			if stderr.Len()+len(payload)-1 > 64*1024 {
				return nil, fmt.Errorf("%w: exec stderr exceeded limit", ErrDeploymentFailed)
			}
			_, _ = stderr.Write(payload[1:])
		case 3:
			var status struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(payload[1:], &status) != nil || status.Status != "Success" {
				return nil, fmt.Errorf("%w: exec status: %s", ErrDeploymentFailed, strings.TrimSpace(stderr.String()))
			}
			return stdout.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("%w: exec closed without status", ErrDeploymentFailed)
}

func boolQuery(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
