package kubernetestarget

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
)

// This test is opt-in because it mutates only the explicitly supplied
// Kubernetes namespace and credential directory.
func TestFoundationWorkspacePortableSnapshotKubernetesLive(t *testing.T) {
	endpoint := os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_ENDPOINT")
	credentialDirectory := os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_CREDENTIAL_DIRECTORY")
	credentialRef := os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_CREDENTIAL_REF")
	imageURI := os.Getenv("CLOUD_AGENTS_FOUNDATION_SNAPSHOT_K8S_IMAGE_URI")
	if endpoint == "" || credentialDirectory == "" || credentialRef == "" || imageURI == "" {
		t.Skip("live Kubernetes portable snapshot environment not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	directory, err := NewCredentialDirectory(credentialDirectory)
	if err != nil {
		t.Fatal(err)
	}
	archives, err := dockertarget.NewFoundationSnapshotArchiveDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	input := dockertarget.FoundationWorkspaceSnapshot{
		TenantID: "live-snapshot-tenant", ProjectID: "live-snapshot-project", TargetID: "live-snapshot-target", WorkspaceID: "live-snapshot-source",
		SnapshotID: "live-snapshot", ImageURI: imageURI,
	}
	workspace := FoundationWorkspaceVolume{TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID, WorkspaceID: input.WorkspaceID}
	input.SourceVolumeName, err = directory.EnsureFoundationWorkspaceVolume(ctx, endpoint, credentialRef, workspace)
	if err != nil {
		t.Fatal(err)
	}
	client, transport, base, err := directory.client(endpoint, credentialRef)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	namespace, err := directory.foundationNamespace(credentialRef)
	if err != nil {
		t.Fatal(err)
	}
	deletePVC := func(volume FoundationWorkspaceVolume) {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = deleteOwnedResource(cleanupContext, client, base+"/api/v1/namespaces/"+namespace+"/persistentvolumeclaims/"+volume.Name(), namespace, volume.Name(), volume.annotations())
	}
	defer deletePVC(workspace)

	seedName := snapshotPodName(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID, "seed")
	seedLabels := snapshotPodLabels(input.TenantID, input.ProjectID, input.TargetID, input.WorkspaceID, input.SnapshotID)
	if err := createSnapshotPod(ctx, client, base, namespace, seedName, imageURI, input.SourceVolumeName, seedLabels); err != nil {
		t.Fatal(err)
	}
	cleanupSeed := func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = deleteOwnedResource(cleanupContext, client, base+"/api/v1/namespaces/"+namespace+"/pods/"+seedName, namespace, seedName, seedLabels)
	}
	defer cleanupSeed()
	if err := waitSnapshotPod(ctx, client, base, namespace, seedName); err != nil {
		t.Fatal(err)
	}
	proof := "portable-kubernetes-snapshot-proof"
	if _, err := execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, seedName,
		[]string{"/bin/sh", "-c", fmt.Sprintf("printf '%s\\n' > /workspace/proof.txt", proof)}, nil); err != nil {
		t.Fatal(err)
	}
	cleanupSeed()
	result, err := directory.SnapshotFoundationWorkspacePortable(ctx, endpoint, credentialRef, input, archives)
	if err != nil || !result.CleanupComplete || result.VolumeName == "" || result.ContentDigest == "" || result.SizeBytes < 1 {
		t.Fatalf("snapshot result=%+v err=%v", result, err)
	}
	defer func() {
		_ = archives.Remove(dockertarget.FoundationWorkspaceSnapshotCleanup{
			TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID,
			SourceWorkspaceID: input.WorkspaceID, SnapshotID: input.SnapshotID, PhysicalSnapshotID: result.VolumeName,
		})
	}()

	restore := dockertarget.FoundationWorkspaceRestore{
		TenantID: input.TenantID, ProjectID: input.ProjectID, TargetID: input.TargetID,
		SourceWorkspaceID: input.WorkspaceID, SnapshotID: input.SnapshotID, SnapshotVolumeName: result.VolumeName,
		ContentDigest: result.ContentDigest, WorkspaceID: "live-snapshot-restored", ImageURI: imageURI,
	}
	restored, err := directory.RestoreFoundationWorkspacePortable(ctx, endpoint, credentialRef, restore, archives)
	if err != nil || !restored.CleanupComplete || restored.VolumeName == "" || restored.ContentDigest != result.ContentDigest {
		t.Fatalf("restore result=%+v err=%v", restored, err)
	}
	restoredWorkspace := FoundationWorkspaceVolume{TenantID: restore.TenantID, ProjectID: restore.ProjectID, TargetID: restore.TargetID, WorkspaceID: restore.WorkspaceID}
	defer deletePVC(restoredWorkspace)
	verifyName := snapshotPodName(restore.TenantID, restore.ProjectID, restore.TargetID, restore.WorkspaceID, restore.SnapshotID, "verify")
	verifyLabels := snapshotRestorePodLabels(restore)
	if err := createSnapshotPod(ctx, client, base, namespace, verifyName, imageURI, restored.VolumeName, verifyLabels); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = deleteOwnedResource(cleanupContext, client, base+"/api/v1/namespaces/"+namespace+"/pods/"+verifyName, namespace, verifyName, verifyLabels)
	}()
	if err := waitSnapshotPod(ctx, client, base, namespace, verifyName); err != nil {
		t.Fatal(err)
	}
	verified, err := execSnapshotPod(ctx, directory, endpoint, credentialRef, namespace, verifyName,
		[]string{"/bin/sh", "-c", "cat /workspace/proof.txt"}, nil)
	if err != nil || strings.TrimSpace(string(verified)) != proof {
		t.Fatalf("restored proof=%q err=%v", verified, err)
	}
	evidence, _ := json.Marshal(map[string]any{"snapshotVolume": result.VolumeName, "contentDigest": result.ContentDigest, "sizeBytes": result.SizeBytes, "restoredVolume": restored.VolumeName, "proof": proof})
	t.Logf("FOUNDATION_KUBERNETES_PORTABLE_SNAPSHOT=%s", evidence)
}
