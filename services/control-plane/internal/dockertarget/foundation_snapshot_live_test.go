package dockertarget

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLivePortableSnapshotSourceNode(t *testing.T) {
	address := os.Getenv("CLOUD_AGENTS_PORTABLE_SOURCE_DOCKER_ADDRESS")
	archivePath := os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY")
	if address == "" || archivePath == "" {
		t.Skip("portable source Docker environment is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	server := liveSnapshotDockerProxy(t, "tcp", address)
	directory := credentialDirectoryForIsolationTest(t, server)
	archives, err := NewFoundationSnapshotArchiveDirectory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	image := os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE")
	helperImage := os.Getenv("CLOUD_AGENTS_PORTABLE_SOURCE_HELPER_IMAGE")
	if helperImage == "" {
		t.Fatal("portable source helper image is missing")
	}
	source := FoundationWorkspaceVolume{TenantID: "tenant", ProjectID: "project", TargetID: "source-node", WorkspaceID: "workspace"}
	volume, err := directory.EnsureFoundationWorkspaceVolume(ctx, server.URL, "docker", source)
	if err != nil {
		t.Fatal(err)
	}
	input := FoundationWorkspaceSnapshot{TenantID: source.TenantID, ProjectID: source.ProjectID, TargetID: source.TargetID,
		WorkspaceID: source.WorkspaceID, SnapshotID: "snapshot", SourceVolumeName: volume, ImageURI: image}
	archive := snapshotTestArchive(t, time.Unix(100, 0))
	client, transport, base, err := directory.client(server.URL, "docker")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	if err := createArchiveHelper(ctx, client, base, input.helperName(), helperImage, []string{volume + ":/workspace"}, input.labels()); err != nil {
		t.Fatal(err)
	}
	if err := writeDockerArchive(ctx, client, base, input.helperName(), "/workspace", archive); err != nil {
		t.Fatal(err)
	}
	if err := removeSnapshotHelper(ctx, client, base, input.helperName(), input.labels()); err != nil {
		t.Fatal(err)
	}
	if err := createArchiveHelper(ctx, client, base, input.helperName(), helperImage, []string{volume + ":/source:ro"}, input.labels()); err != nil {
		t.Fatal(err)
	}
	captured, err := readDockerArchive(ctx, client, base, input.helperName(), "/source/.")
	if err != nil {
		t.Fatal(err)
	}
	contentDigest, err := snapshotArchiveDigest(captured)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := archives.Put(input, captured, contentDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeSnapshotHelper(ctx, client, base, input.helperName(), input.labels()); err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(map[string]any{"physicalSnapshotId": physical,
		"contentDigest": contentDigest, "sizeBytes": len(captured), "sourceVolume": volume})
	t.Logf("PORTABLE_SOURCE_SNAPSHOT=%s", receipt)
}

func TestLivePortableSnapshotDestinationNode(t *testing.T) {
	socket := os.Getenv("CLOUD_AGENTS_PORTABLE_DESTINATION_DOCKER_SOCKET")
	archivePath := os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY")
	physical := os.Getenv("CLOUD_AGENTS_PORTABLE_PHYSICAL_SNAPSHOT_ID")
	digest := os.Getenv("CLOUD_AGENTS_PORTABLE_CONTENT_DIGEST")
	startedRaw := os.Getenv("CLOUD_AGENTS_PORTABLE_FAILOVER_STARTED_UNIX_NANO")
	if socket == "" || archivePath == "" || physical == "" || digest == "" || startedRaw == "" {
		t.Skip("portable destination Docker environment is not configured")
	}
	startedNano, err := strconv.ParseInt(startedRaw, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	server := liveSnapshotDockerProxy(t, "unix", socket)
	directory := credentialDirectoryForIsolationTest(t, server)
	archives, err := NewFoundationSnapshotArchiveDirectory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	input := FoundationWorkspaceRestore{TenantID: "tenant", ProjectID: "project", TargetID: "destination-node",
		SourceWorkspaceID: "workspace", SnapshotID: "snapshot", SnapshotVolumeName: physical,
		ContentDigest: digest, WorkspaceID: "workspace-restored", ImageURI: os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE")}
	result, err := directory.RestoreFoundationWorkspacePortable(ctx, server.URL, "docker", input, archives)
	if err != nil || !result.CleanupComplete || result.ContentDigest != digest {
		t.Fatalf("portable destination restore=%+v err=%v", result, err)
	}
	client, transport, base, err := directory.client(server.URL, "docker")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	if err := createArchiveHelper(ctx, client, base, input.helperName(), input.ImageURI,
		[]string{result.VolumeName + ":/workspace:ro"}, input.labels()); err != nil {
		t.Fatal(err)
	}
	restored, err := readDockerArchive(ctx, client, base, input.helperName(), "/workspace/.")
	restoredDigest, digestErr := snapshotArchiveDigest(restored)
	if err != nil || digestErr != nil || restoredDigest != digest {
		t.Fatalf("restored archive digest=%s err=%v/%v", restoredDigest, err, digestErr)
	}
	if err := removeSnapshotHelper(ctx, client, base, input.helperName(), input.labels()); err != nil {
		t.Fatal(err)
	}
	destination := FoundationWorkspaceVolume{TenantID: input.TenantID, ProjectID: input.ProjectID,
		TargetID: input.TargetID, WorkspaceID: input.WorkspaceID}
	if err := removeSnapshotVolume(ctx, client, base, result.VolumeName, destination.labels()); err != nil {
		t.Fatal(err)
	}
	if err := archives.Remove(FoundationWorkspaceSnapshotCleanup{TenantID: input.TenantID, ProjectID: input.ProjectID,
		SourceWorkspaceID: input.SourceWorkspaceID, SnapshotID: input.SnapshotID, PhysicalSnapshotID: physical}); err != nil {
		t.Fatal(err)
	}
	proof := sha256.Sum256([]byte("exact-offline-data"))
	receipt, _ := json.Marshal(map[string]any{"contentDigest": restoredDigest,
		"proofDigest": hex.EncodeToString(proof[:]), "destinationVolume": result.VolumeName,
		"rtoMilliseconds": time.Since(time.Unix(0, startedNano)).Milliseconds(), "rpoBytes": 0,
		"destinationCleanup": true, "snapshotCleanup": true})
	t.Logf("PORTABLE_DESTINATION_RESTORE=%s", receipt)
}

func TestLivePortableSnapshotDestinationRejectsUnavailableArchive(t *testing.T) {
	socket := os.Getenv("CLOUD_AGENTS_PORTABLE_DESTINATION_DOCKER_SOCKET")
	archivePath := os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY")
	physical := os.Getenv("CLOUD_AGENTS_PORTABLE_PHYSICAL_SNAPSHOT_ID")
	digest := os.Getenv("CLOUD_AGENTS_PORTABLE_CONTENT_DIGEST")
	mode := os.Getenv("CLOUD_AGENTS_PORTABLE_NEGATIVE_MODE")
	if socket == "" || archivePath == "" || physical == "" || digest == "" || (mode != "missing" && mode != "corrupt") {
		t.Skip("portable destination snapshot negative environment is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := liveSnapshotDockerProxy(t, "unix", socket)
	directory := credentialDirectoryForIsolationTest(t, server)
	archives, err := NewFoundationSnapshotArchiveDirectory(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	input := FoundationWorkspaceRestore{TenantID: "tenant", ProjectID: "project", TargetID: "destination-node",
		SourceWorkspaceID: "workspace", SnapshotID: "snapshot", SnapshotVolumeName: physical,
		ContentDigest: digest, WorkspaceID: "workspace-negative-" + mode,
		ImageURI: os.Getenv("CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE")}
	if _, err := directory.RestoreFoundationWorkspacePortable(ctx, server.URL, "docker", input, archives); !errors.Is(err, ErrDeploymentConflict) {
		t.Fatalf("unavailable archive mode=%s err=%v", mode, err)
	}
	t.Logf("PORTABLE_DESTINATION_NEGATIVE={\"mode\":%q,\"rejected\":true}", mode)
}

func liveSnapshotDockerProxy(t *testing.T, network, address string) *httptest.Server {
	t.Helper()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	proxy := &httputil.ReverseProxy{Director: func(request *http.Request) {
		request.URL.Scheme = "http"
		request.URL.Host = "docker"
		request.Host = "docker"
	}, Transport: transport}
	server := httptest.NewUnstartedServer(proxy)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	server.StartTLS()
	t.Cleanup(func() {
		server.Close()
		transport.CloseIdleConnections()
	})
	if strings.TrimSpace(address) != address {
		t.Fatal("Docker address contains surrounding whitespace")
	}
	return server
}
