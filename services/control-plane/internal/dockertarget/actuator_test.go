package dockertarget

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerWorkerContainerUsesOnlyCredentialReferences(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "docker-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 1,
		ReleaseDigest: "sha256:" + strings.Repeat("a", 64), ProviderCredentialRef: "provider-alpha",
		CPULimitMillis: 1500, MemoryLimitBytes: 512 << 20,
	}
	config := DeploymentConfig{
		WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test",
	}
	labels := DeploymentLabels(request, config)
	if labels["cloud-agents.dev/worker-credential-ref"] != config.WorkerCredentialRef {
		t.Fatal("deployment labels do not bind the Worker credential reference")
	}
	var create map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		switch {
		case httpRequest.Method == http.MethodGet && strings.HasPrefix(httpRequest.URL.Path, "/volumes/"):
			_ = json.NewEncoder(writer).Encode(map[string]string{"Name": strings.TrimPrefix(httpRequest.URL.Path, "/volumes/")})
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/json":
			_, _ = writer.Write([]byte("[]"))
		case httpRequest.Method == http.MethodPost && httpRequest.URL.Path == "/containers/create":
			if httpRequest.URL.Query().Get("name") != WorkerContainerName(request) {
				t.Errorf("container name = %q", httpRequest.URL.Query().Get("name"))
			}
			if err := json.NewDecoder(httpRequest.Body).Decode(&create); err != nil {
				t.Error(err)
			}
			writer.WriteHeader(http.StatusCreated)
			_, _ = writer.Write([]byte(`{"Id":"container-alpha"}`))
		case httpRequest.Method == http.MethodPost && httpRequest.URL.Path == "/containers/container-alpha/start":
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, httpRequest)
		}
	}))
	t.Cleanup(server.Close)

	for _, volume := range []string{config.WorkerCredentialRef, request.ProviderCredentialRef} {
		if err := requireVolume(context.Background(), server.Client(), server.URL, volume); err != nil {
			t.Fatal(err)
		}
	}
	if found, err := findWorkerContainer(context.Background(), server.Client(), server.URL, request); err != nil || found != "" {
		t.Fatalf("found = %q, error = %v", found, err)
	}
	image := config.WorkerImageRepository + "@" + request.ReleaseDigest
	containerID, err := createWorkerContainer(context.Background(), server.Client(), server.URL, request, config, image, labels)
	if err != nil || containerID != "container-alpha" {
		t.Fatalf("container = %q, error = %v", containerID, err)
	}
	environment, _ := create["Env"].([]any)
	if create["Image"] != image || !containsJSONStrings(environment,
		"CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent",
		"CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
	) {
		t.Fatalf("image/env = %#v/%#v", create["Image"], create["Env"])
	}
	command, _ := create["Cmd"].([]any)
	if !containsJSONStrings(command, "--admission-token-file", "/run/cloud-agents/worker-credentials/admission-token") {
		t.Fatalf("command = %#v", command)
	}
	host, _ := create["HostConfig"].(map[string]any)
	if host["Memory"] != float64(request.MemoryLimitBytes) || host["NanoCpus"] != float64(request.CPULimitMillis*1_000_000) || host["ReadonlyRootfs"] != true {
		t.Fatalf("host config = %#v", host)
	}
	var workspace map[string]any
	for _, value := range host["Mounts"].([]any) {
		mount, _ := value.(map[string]any)
		if mount["Target"] == "/workspace" {
			workspace = mount
		}
	}
	volumeOptions, _ := workspace["VolumeOptions"].(map[string]any)
	volumeLabels, _ := volumeOptions["Labels"].(map[string]any)
	if workspace["Source"] != workspaceVolumeName(request) || volumeLabels["cloud-agents.dev/lease"] != request.LeaseID {
		t.Fatalf("workspace mount = %#v", workspace)
	}
	if err := startWorkerContainer(context.Background(), server.Client(), server.URL, containerID); err != nil {
		t.Fatal(err)
	}
}

func TestDeployedWorkerEndpointAcceptsDualStackBindingOnOnePort(t *testing.T) {
	var inspect containerInspect
	if err := json.Unmarshal([]byte(`{"NetworkSettings":{"Ports":{"8091/tcp":[{"HostPort":"32768"},{"HostPort":"32768"}]}}}`), &inspect); err != nil {
		t.Fatal(err)
	}
	endpoint, err := deployedWorkerEndpoint("https://docker.example.test:2376", inspect)
	if err != nil || endpoint != "https://docker.example.test:32768" {
		t.Fatalf("endpoint=%q error=%v", endpoint, err)
	}
	inspect.NetworkSettings.Ports[workerPort][1].HostPort = "32769"
	if _, err := deployedWorkerEndpoint("https://docker.example.test:2376", inspect); err != ErrDeploymentFailed {
		t.Fatalf("conflicting dual-stack binding error=%v", err)
	}
}

func TestEnsureWorkerContainerReconcilesConcurrentCreate(t *testing.T) {
	request := DeployRequest{TenantID: "tenant-alpha", ProjectID: "project-alpha", LeaseID: "lease-alpha"}
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		switch {
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/json":
			listCalls++
			if listCalls == 1 {
				_, _ = writer.Write([]byte("[]"))
			} else {
				_, _ = writer.Write([]byte(`[{"Id":"container-winner"}]`))
			}
		case httpRequest.Method == http.MethodPost && httpRequest.URL.Path == "/containers/create":
			writer.WriteHeader(http.StatusConflict)
		default:
			http.NotFound(writer, httpRequest)
		}
	}))
	t.Cleanup(server.Close)
	containerID, err := ensureWorkerContainer(context.Background(), server.Client(), server.URL, request, DeploymentConfig{}, "image", nil)
	if err != nil || containerID != "container-winner" || listCalls != 2 {
		t.Fatalf("container = %q, list calls = %d, error = %v", containerID, listCalls, err)
	}
}

func TestCredentialDirectoryReadsNonSecretDeploymentDescriptor(t *testing.T) {
	root := t.TempDir()
	ref := filepath.Join(root, "docker-alpha")
	if err := os.Mkdir(ref, 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := `{"workerImageRepository":"registry.example.test/cloud-agents/worker","workerCredentialRef":"worker-alpha","workerSpiffeId":"spiffe://cloud-agents.test/workers/docker-alpha","workerServerName":"worker.example.test"}`
	if err := os.WriteFile(filepath.Join(ref, "deployment.json"), []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	directory, err := NewCredentialDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	config, err := directory.readDeploymentConfig("docker-alpha")
	if err != nil || config.WorkerCredentialRef != "worker-alpha" {
		t.Fatalf("config = %#v, error = %v", config, err)
	}
	if err := os.WriteFile(filepath.Join(ref, "deployment.json"), []byte(descriptor[:len(descriptor)-1]+`,"apiKey":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.readDeploymentConfig("docker-alpha"); err != ErrDeploymentConfigInvalid {
		t.Fatalf("secret field error = %v", err)
	}
	if (DeploymentConfig{WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker\nexample.test"}).Valid() {
		t.Fatal("accepted Worker server name with a control character")
	}
}

func TestCleanupWorkerContainerIsOwnedAndIdempotent(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "docker-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 1, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
	}
	config := DeploymentConfig{
		WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test",
	}
	ownedImage, ownedLabels := config.WorkerImageRepository+"@"+request.ReleaseDigest, DeploymentLabels(request, config)
	actualLabels := maps.Clone(ownedLabels)
	present, volumePresent, deletes, volumeDeletes, targetLists := true, true, 0, 0, 0
	workspace := workspaceVolumeName(request)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		switch {
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/json":
			if strings.Contains(httpRequest.URL.Query().Get("filters"), `"volume"`) {
				_, _ = writer.Write([]byte("[]"))
				return
			}
			if strings.Contains(httpRequest.URL.Query().Get("filters"), "cloud-agents.dev/target=docker-alpha") {
				targetLists++
			}
			if present {
				_, _ = writer.Write([]byte(`[{"Id":"container-alpha"}]`))
			} else {
				_, _ = writer.Write([]byte("[]"))
			}
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/container-alpha/json":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"Name": "/" + WorkerContainerName(request), "Config": map[string]any{"Image": ownedImage, "Labels": actualLabels},
				"Mounts": []map[string]any{{"Type": "volume", "Name": workspace, "Destination": "/workspace"}},
			})
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/volumes/"+workspace:
			if !volumePresent {
				http.NotFound(writer, httpRequest)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"Name": workspace, "Labels": workspaceVolumeLabels(request)})
		case httpRequest.Method == http.MethodDelete && httpRequest.URL.Path == "/containers/container-alpha":
			present, deletes = false, deletes+1
			writer.WriteHeader(http.StatusNoContent)
		case httpRequest.Method == http.MethodDelete && httpRequest.URL.Path == "/volumes/"+workspace:
			volumePresent, volumeDeletes = false, volumeDeletes+1
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, httpRequest)
		}
	}))
	t.Cleanup(server.Close)
	for range 2 {
		if err := cleanupWorkerContainer(context.Background(), server.Client(), server.URL, request, config); err != nil {
			t.Fatal(err)
		}
	}
	if deletes != 1 {
		t.Fatalf("delete calls = %d", deletes)
	}
	config.WorkerServerName = "other.example.test"
	present = true
	if err := cleanupWorkerContainer(context.Background(), server.Client(), server.URL, request, config); err != ErrDeploymentConflict {
		t.Fatalf("ownership drift error = %v", err)
	}
	actualLabels["cloud-agents.dev/target-generation"] = "2"
	if _, err := listManagedWorkers(context.Background(), server.Client(), server.URL, request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration); err != ErrDeploymentConflict {
		t.Fatalf("future generation list error = %v", err)
	}
	actualLabels = maps.Clone(ownedLabels)
	workers, err := listManagedWorkers(context.Background(), server.Client(), server.URL, request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil || len(workers) != 1 || workers[0].Request != request {
		t.Fatalf("managed workers = %#v, error = %v", workers, err)
	}
	if container, gotWorkspace := workers[0].CleanupResourceNames(); container != WorkerContainerName(request) || gotWorkspace != workspace {
		t.Fatalf("cleanup resources = %q/%q", container, gotWorkspace)
	}
	for range 2 {
		if err := cleanupManagedWorker(context.Background(), server.Client(), server.URL, workers[0]); err != nil {
			t.Fatal(err)
		}
	}
	if present || volumePresent || deletes != 2 || volumeDeletes != 1 || targetLists != 4 {
		t.Fatalf("managed cleanup present=%v volume=%v deletes=%d/%d targetLists=%d", present, volumePresent, deletes, volumeDeletes, targetLists)
	}
}

func TestCleanupManagedWorkerTargetsItsGenerationDuringUpgrade(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "docker-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 1, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
	}
	config := DeploymentConfig{
		WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test",
	}
	image, labels := config.WorkerImageRepository+"@"+request.ReleaseDigest, DeploymentLabels(request, config)
	workspace, inUse, deletes, volumeDeletes := strings.Repeat("b", 64), true, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		switch {
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/json":
			filters := httpRequest.URL.Query().Get("filters")
			if strings.Contains(filters, `"volume"`) {
				if inUse {
					_, _ = writer.Write([]byte(`[{"Id":"container-new"}]`))
				} else {
					_, _ = writer.Write([]byte("[]"))
				}
			} else if strings.Contains(filters, "cloud-agents.dev/lease-generation=1") {
				_, _ = writer.Write([]byte(`[{"Id":"container-old"}]`))
			} else {
				_, _ = writer.Write([]byte(`[{"Id":"container-old"},{"Id":"container-new"}]`))
			}
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/container-old/json":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"Name": "/" + WorkerContainerName(request), "Config": map[string]any{"Image": image, "Labels": labels},
				"Mounts": []map[string]any{{"Type": "volume", "Name": workspace, "Destination": "/workspace"}},
			})
		case httpRequest.Method == http.MethodDelete && httpRequest.URL.Path == "/containers/container-old":
			deletes++
			writer.WriteHeader(http.StatusNoContent)
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/volumes/"+workspace:
			_ = json.NewEncoder(writer).Encode(map[string]any{"Name": workspace})
		case httpRequest.Method == http.MethodDelete && strings.HasPrefix(httpRequest.URL.Path, "/volumes/"):
			volumeDeletes++
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, httpRequest)
		}
	}))
	t.Cleanup(server.Close)

	worker := ManagedWorker{Request: request, id: "container-old", image: image, labels: labels}
	if err := cleanupManagedWorker(context.Background(), server.Client(), server.URL, worker); err != nil {
		t.Fatal(err)
	}
	if deletes != 1 || volumeDeletes != 0 {
		t.Fatalf("delete calls = %d/%d", deletes, volumeDeletes)
	}
	inUse = false
	if err := removeWorkspaceVolumeIfUnused(context.Background(), server.Client(), server.URL, workspace, request, config.WorkerCredentialRef); err != nil {
		t.Fatal(err)
	}
	if volumeDeletes != 1 {
		t.Fatalf("legacy workspace delete calls = %d", volumeDeletes)
	}
}

func containsJSONStrings(values []any, expected ...string) bool {
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		for index, candidate := range expected {
			if text == candidate {
				expected[index] = ""
			}
		}
	}
	for _, candidate := range expected {
		if candidate != "" {
			return false
		}
	}
	return true
}
