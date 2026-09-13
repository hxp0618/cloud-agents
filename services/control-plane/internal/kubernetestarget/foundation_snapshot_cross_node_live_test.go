package kubernetestarget

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
)

type kubernetesCrossNodeSnapshot struct {
	VolumeName, ContentDigest, SourceNodeName, SourcePersistentVolumeName string
	SizeBytes                                                             int64
}

func TestFoundationWorkspacePortableSnapshotKubernetesCrossNodeSourceLive(t *testing.T) {
	endpoint, credentials, archivePath, evidencePath, image := crossNodeSnapshotEnvironment(t, "SOURCE")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	directory, err := NewCredentialDirectory(credentials)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := dockertarget.NewFoundationSnapshotArchiveDirectory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	input := dockertarget.FoundationWorkspaceSnapshot{
		TenantID: "kind-cross-node-tenant", ProjectID: "kind-cross-node-project", TargetID: "kind-source-target",
		WorkspaceID: "kind-source-workspace", SnapshotID: "kind-cross-node-snapshot", ImageURI: image,
	}
	input.SourceVolumeName, err = directory.EnsureFoundationWorkspaceVolume(ctx, endpoint, "source", FoundationWorkspaceVolume{
		TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, transport, base, err := directory.client(endpoint, "source")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	namespace, err := directory.foundationNamespace("source")
	if err != nil {
		t.Fatal(err)
	}
	seedName := snapshotPodName(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, "cross-node-seed")
	seedLabels := snapshotPodLabels(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID)
	if err = createSnapshotPod(ctx, client, base, namespace, seedName, image, input.SourceVolumeName, seedLabels); err != nil {
		t.Fatal(err)
	}
	cleanupSnapshotPod(t, client, base, namespace, seedName, seedLabels)
	if err = waitSnapshotPod(ctx, client, base, namespace, seedName); err != nil {
		t.Fatal(err)
	}
	sourceNode, sourceVolume, err := crossNodePlacement(ctx, client, base, namespace, seedName, input.SourceVolumeName)
	if err != nil {
		t.Fatal(err)
	}
	const proof = "portable-kubernetes-cross-node-proof"
	if _, err = execSnapshotPod(ctx, directory, endpoint, "source", namespace, seedName,
		[]string{"/bin/sh", "-c", fmt.Sprintf("printf '%s\\n' > /workspace/proof.txt", proof)}, nil); err != nil {
		t.Fatal(err)
	}
	deleteSnapshotPod(t, client, base, namespace, seedName, seedLabels)
	result, err := directory.SnapshotFoundationWorkspacePortable(ctx, endpoint, "source", input, archives)
	if err != nil || !result.CleanupComplete || result.VolumeName == "" || result.ContentDigest == "" || result.SizeBytes < 1 {
		t.Fatalf("snapshot result=%+v err=%v", result, err)
	}
	evidence := kubernetesCrossNodeSnapshot{
		VolumeName: result.VolumeName, ContentDigest: result.ContentDigest, SizeBytes: result.SizeBytes,
		SourceNodeName: sourceNode, SourcePersistentVolumeName: sourceVolume,
	}
	file, err := os.OpenFile(evidencePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(file).Encode(evidence); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("FOUNDATION_KUBERNETES_CROSS_NODE_SOURCE={\"snapshotVolume\":%q,\"contentDigest\":%q,\"sizeBytes\":%d,\"sourceNode\":%q,\"sourcePersistentVolume\":%q,\"proof\":%q}", result.VolumeName, result.ContentDigest, result.SizeBytes, sourceNode, sourceVolume, proof)
}

func TestFoundationWorkspacePortableSnapshotKubernetesCrossNodeDestinationLive(t *testing.T) {
	endpoint, credentials, archivePath, evidencePath, image := crossNodeSnapshotEnvironment(t, "DESTINATION")
	failoverStarted, err := strconv.ParseInt(os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_FAILOVER_STARTED_UNIX_NANO"), 10, 64)
	if err != nil || failoverStarted < 1 {
		t.Skip("cross-node failover start is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	directory, err := NewCredentialDirectory(credentials)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := dockertarget.NewFoundationSnapshotArchiveDirectory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	var source kubernetesCrossNodeSnapshot
	value, err := os.ReadFile(evidencePath)
	if err != nil || json.Unmarshal(value, &source) != nil || source.VolumeName == "" || source.ContentDigest == "" || source.SizeBytes < 1 {
		t.Fatal("invalid source snapshot evidence")
	}
	input := dockertarget.FoundationWorkspaceRestore{
		TenantID: "kind-cross-node-tenant", ProjectID: "kind-cross-node-project", TargetID: "kind-destination-target",
		SourceWorkspaceID: "kind-source-workspace", SnapshotID: "kind-cross-node-snapshot", SnapshotVolumeName: source.VolumeName,
		ContentDigest: source.ContentDigest, WorkspaceID: "kind-destination-workspace", ImageURI: image,
	}
	result, err := directory.RestoreFoundationWorkspacePortable(ctx, endpoint, "destination", input, archives)
	if err != nil || !result.CleanupComplete || result.ContentDigest != source.ContentDigest {
		t.Fatalf("restore result=%+v err=%v", result, err)
	}
	client, transport, base, err := directory.client(endpoint, "destination")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	namespace, err := directory.foundationNamespace("destination")
	if err != nil {
		t.Fatal(err)
	}
	workspace := FoundationWorkspaceVolume{TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID}
	defer deleteSnapshotPVC(client, base, namespace, workspace)
	verifyName := snapshotPodName(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, "cross-node-verify")
	verifyLabels := snapshotRestorePodLabels(input)
	if err = createSnapshotPod(ctx, client, base, namespace, verifyName, image, result.VolumeName, verifyLabels); err != nil {
		t.Fatal(err)
	}
	cleanupSnapshotPod(t, client, base, namespace, verifyName, verifyLabels)
	if err = waitSnapshotPod(ctx, client, base, namespace, verifyName); err != nil {
		t.Fatal(err)
	}
	destinationNode, destinationVolume, err := crossNodePlacement(ctx, client, base, namespace, verifyName, result.VolumeName)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := execSnapshotPod(ctx, directory, endpoint, "destination", namespace, verifyName, []string{"/bin/sh", "-c", "cat /workspace/proof.txt"}, nil)
	if err != nil || strings.TrimSpace(string(proof)) != "portable-kubernetes-cross-node-proof" {
		t.Fatalf("restored proof=%q err=%v", proof, err)
	}
	if err = archives.Remove(dockertarget.FoundationWorkspaceSnapshotCleanup{TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: "kind-source-target", SourceWorkspaceID: input.SourceWorkspaceID,
		SnapshotID: input.SnapshotID, PhysicalSnapshotID: input.SnapshotVolumeName,
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("FOUNDATION_KUBERNETES_CROSS_NODE_DESTINATION={\"contentDigest\":%q,\"sizeBytes\":%d,\"sourceNode\":%q,\"sourcePersistentVolume\":%q,\"destinationNode\":%q,\"destinationPersistentVolume\":%q,\"rtoMilliseconds\":%d,\"rpoBytes\":0,\"proof\":%q}", result.ContentDigest, source.SizeBytes, source.SourceNodeName, source.SourcePersistentVolumeName, destinationNode, destinationVolume, time.Since(time.Unix(0, failoverStarted)).Milliseconds(), strings.TrimSpace(string(proof)))
}

func crossNodePlacement(ctx context.Context, client *http.Client, base, namespace, podName, volumeName string) (string, string, error) {
	var pod, volume struct {
		Spec struct {
			NodeName   string `json:"nodeName"`
			VolumeName string `json:"volumeName"`
		} `json:"spec"`
	}
	prefix := base + "/api/v1/namespaces/" + url.PathEscape(namespace)
	status, err := kubernetesJSON(ctx, client, http.MethodGet, prefix+"/pods/"+url.PathEscape(podName), "", nil, &pod)
	if err != nil || status != http.StatusOK || pod.Spec.NodeName == "" {
		return "", "", ErrDeploymentFailed
	}
	status, err = kubernetesJSON(ctx, client, http.MethodGet, prefix+"/persistentvolumeclaims/"+url.PathEscape(volumeName), "", nil, &volume)
	if err != nil || status != http.StatusOK || volume.Spec.VolumeName == "" {
		return "", "", ErrDeploymentFailed
	}
	return pod.Spec.NodeName, volume.Spec.VolumeName, nil
}

func crossNodeSnapshotEnvironment(t *testing.T, phase string) (string, string, string, string, string) {
	t.Helper()
	values := []string{
		os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_ENDPOINT"),
		os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_CREDENTIAL_DIRECTORY"),
		os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_ARCHIVE_DIRECTORY"),
		os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_EVIDENCE_FILE"),
		os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_IMAGE_URI"),
	}
	for _, value := range values {
		if value == "" {
			t.Skip("cross-node Kubernetes snapshot environment is not configured for " + phase)
		}
	}
	return values[0], values[1], values[2], values[3], values[4]
}

func cleanupSnapshotPod(t *testing.T, client *http.Client, base, namespace, name string, labels map[string]string) {
	t.Helper()
	t.Cleanup(func() { deleteSnapshotPod(t, client, base, namespace, name, labels) })
}

func deleteSnapshotPod(t *testing.T, client *http.Client, base, namespace, name string, labels map[string]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := deleteOwnedResource(ctx, client, base+"/api/v1/namespaces/"+namespace+"/pods/"+name, namespace, name, labels); err != nil {
		t.Logf("snapshot helper cleanup: %v", err)
	}
}

func deleteSnapshotPVC(client *http.Client, base, namespace string, workspace FoundationWorkspaceVolume) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = deleteOwnedResource(ctx, client, base+"/api/v1/namespaces/"+namespace+"/persistentvolumeclaims/"+workspace.Name(), namespace, workspace.Name(), workspace.annotations())
}
