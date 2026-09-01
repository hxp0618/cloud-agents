package dockertarget

import (
	"context"
	"encoding/json"
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
	config := deploymentConfig{
		WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker.example.test",
	}
	labels := deploymentLabels(request, config)
	var create map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, httpRequest *http.Request) {
		switch {
		case httpRequest.Method == http.MethodGet && strings.HasPrefix(httpRequest.URL.Path, "/volumes/"):
			_ = json.NewEncoder(writer).Encode(map[string]string{"Name": strings.TrimPrefix(httpRequest.URL.Path, "/volumes/")})
		case httpRequest.Method == http.MethodGet && httpRequest.URL.Path == "/containers/json":
			_, _ = writer.Write([]byte("[]"))
		case httpRequest.Method == http.MethodPost && httpRequest.URL.Path == "/containers/create":
			if httpRequest.URL.Query().Get("name") != workerContainerName(request) {
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
	if create["Image"] != image || create["Env"] != nil {
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
	if err := startWorkerContainer(context.Background(), server.Client(), server.URL, containerID); err != nil {
		t.Fatal(err)
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
	containerID, err := ensureWorkerContainer(context.Background(), server.Client(), server.URL, request, deploymentConfig{}, "image", nil)
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
	if validDeploymentConfig(deploymentConfig{WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/docker-alpha", WorkerServerName: "worker\nexample.test"}) {
		t.Fatal("accepted Worker server name with a control character")
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
