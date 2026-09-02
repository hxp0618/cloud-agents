package kubernetestarget

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialDirectoryListsAndCleansManagedWorkers(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "kubernetes-alpha", LeaseID: "lease-orphan",
		TargetGeneration: 1, LeaseGeneration: 2, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
	}
	config := deploymentConfig{
		Namespace: "agents", WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialSecretRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/kubernetes-alpha", WorkerServerName: "worker.example.test",
	}
	name := workerResourceName(request)
	annotations := deploymentAnnotations(request, config)
	deleted, deletes := map[string]bool{}, 0
	cluster := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Header.Get("Authorization") != "Bearer service-account-token" {
			t.Errorf("authorization = %q", incoming.Header.Get("Authorization"))
		}
		if incoming.Method == http.MethodGet && !strings.HasSuffix(incoming.URL.Path, "/"+name) {
			if incoming.URL.Query().Get("labelSelector") != "cloud-agents.dev/managed=true" || incoming.URL.Query().Get("limit") != "200" {
				t.Errorf("list query = %q", incoming.URL.RawQuery)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"metadata": map[string]string{}, "items": []resource{{Metadata: resourceMetadata{Name: name, Namespace: config.Namespace, Annotations: annotations}}}})
			return
		}
		if deleted[incoming.URL.Path] {
			http.NotFound(writer, incoming)
			return
		}
		switch incoming.Method {
		case http.MethodGet:
			_ = json.NewEncoder(writer).Encode(resource{Metadata: resourceMetadata{Name: name, Namespace: config.Namespace, UID: "uid-" + name, ResourceVersion: "7", Annotations: annotations}})
		case http.MethodDelete:
			var options struct {
				Preconditions map[string]string `json:"preconditions"`
			}
			if err := json.NewDecoder(incoming.Body).Decode(&options); err != nil || options.Preconditions["uid"] != "uid-"+name || options.Preconditions["resourceVersion"] != "7" {
				t.Errorf("delete preconditions = %#v, error = %v", options.Preconditions, err)
			}
			deleted[incoming.URL.Path], deletes = true, deletes+1
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, incoming)
		}
	}))
	t.Cleanup(cluster.Close)

	directory := t.TempDir()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cluster.Certificate().Raw})
	descriptor, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for file, value := range map[string][]byte{"cluster-alpha.ca.crt": certificate, "cluster-alpha.token": []byte("service-account-token\n"), "cluster-alpha.deployment.json": descriptor} {
		if err := os.WriteFile(filepath.Join(directory, file), value, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	credentials, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	workers, err := credentials.ListManagedWorkers(context.Background(), cluster.URL, "cluster-alpha", request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil || len(workers) != 1 || workers[0].Request != request {
		t.Fatalf("workers=%#v error=%v", workers, err)
	}
	for range 2 {
		if err := credentials.CleanupManagedWorker(context.Background(), cluster.URL, "cluster-alpha", workers[0]); err != nil {
			t.Fatal(err)
		}
	}
	if deletes != 3 {
		t.Fatalf("deletes=%d", deletes)
	}
	if _, err := credentials.ListManagedWorkers(context.Background(), cluster.URL, "cluster-alpha", request.TenantID, request.ProjectID, request.TargetID, 0); !errors.Is(err, ErrDeploymentConfigInvalid) {
		t.Fatalf("invalid generation error=%v", err)
	}
}

func TestKubernetesWorkerResourcesApplyBecomeReadyAndCleanup(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "kubernetes-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 2, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1500, MemoryLimitBytes: 512 << 20,
	}
	config := deploymentConfig{
		Namespace: "agents", WorkerImageRepository: "registry.example.test/cloud-agents/worker",
		WorkerCredentialSecretRef: "worker-alpha", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/kubernetes-alpha", WorkerServerName: "worker.example.test",
	}
	name := workerResourceName(request)
	annotations := deploymentAnnotations(request, config)
	desired := desiredResources(name, request, config, annotations)
	bodies := make([]map[string]any, 0, len(desired))
	for _, resource := range desired {
		bodies = append(bodies, resource.body)
	}
	encoded, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, expected := range []string{"PersistentVolumeClaim", "LoadBalancer", "Deployment", config.WorkerCredentialSecretRef, request.ProviderCredentialRef, config.WorkerImageRepository + "@" + request.ReleaseDigest, "readOnlyRootFilesystem", "1500m", workspaceStorage} {
		if !strings.Contains(text, expected) {
			t.Fatalf("desired resources do not contain %q", expected)
		}
	}
	for _, forbidden := range []string{"apiKey", "authToken", "privateKey", "service-account-token"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("desired resources contain credential field %q", forbidden)
		}
	}
	if strings.Count(text, `"defaultMode":256`) != 2 {
		t.Fatal("Worker and Provider Secret volumes must use mode 0400")
	}

	present := map[string]bool{}
	applies, deletes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPatch:
			if request.Header.Get("Content-Type") != "application/apply-patch+yaml" || request.URL.Query().Get("fieldManager") != "cloud-agents-control-plane" || request.URL.Query().Get("fieldValidation") != "Strict" {
				t.Errorf("apply headers/query = %q/%q", request.Header.Get("Content-Type"), request.URL.RawQuery)
			}
			present[request.URL.Path] = true
			applies++
			writer.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			if !present[request.URL.Path] {
				http.NotFound(writer, request)
				return
			}
			value := resource{Metadata: resourceMetadata{Name: name, Namespace: config.Namespace, UID: "uid-" + name, ResourceVersion: "7", Generation: 3, Annotations: annotations}}
			if strings.Contains(request.URL.Path, "/deployments/") {
				value.Status.ObservedGeneration, value.Status.AvailableReplicas = 3, 1
			}
			if strings.Contains(request.URL.Path, "/services/") {
				value.Status.LoadBalancer.Ingress = append(value.Status.LoadBalancer.Ingress, struct {
					IP       string `json:"ip"`
					Hostname string `json:"hostname"`
				}{IP: "192.0.2.10"})
			}
			_ = json.NewEncoder(writer).Encode(value)
		case http.MethodDelete:
			var options struct {
				Preconditions map[string]string `json:"preconditions"`
			}
			if err := json.NewDecoder(request.Body).Decode(&options); err != nil || options.Preconditions["uid"] != "uid-"+name || options.Preconditions["resourceVersion"] != "7" {
				t.Errorf("delete preconditions = %#v, error = %v", options.Preconditions, err)
			}
			present[request.URL.Path] = false
			deletes++
			writer.WriteHeader(http.StatusOK)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	for _, resource := range desired {
		if err := applyResource(context.Background(), server.Client(), server.URL, resource); err != nil {
			t.Fatal(err)
		}
	}
	endpoint, err := waitForReadyResources(context.Background(), server.Client(), server.URL, config.Namespace, name, annotations)
	if err != nil || endpoint != "https://192.0.2.10:8091" {
		t.Fatalf("endpoint=%q error=%v", endpoint, err)
	}
	for range 2 {
		if err := cleanupResources(context.Background(), server.Client(), server.URL, config.Namespace, name, annotations); err != nil {
			t.Fatal(err)
		}
	}
	if applies != 3 || deletes != 3 {
		t.Fatalf("applies=%d deletes=%d", applies, deletes)
	}
	stale := deploymentAnnotations(request, config)
	stale["cloud-agents.dev/lease-generation"] = "1"
	if ownedResource(resourceMetadata{Name: name, Namespace: config.Namespace, Annotations: stale}, name, config.Namespace, annotations) {
		t.Fatal("stale generation accepted as owned")
	}
}

func TestKubernetesUpgradeKeepsAnOldReplicaUntilTheNewOneIsReady(t *testing.T) {
	request := DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "kubernetes-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 2, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1000, MemoryLimitBytes: 512 << 20,
	}
	config := deploymentConfig{
		Namespace: "agents", WorkerImageRepository: "registry.example.test/cloud-agents/worker",
		WorkerCredentialSecretRef: "worker-alpha", WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/kubernetes-alpha", WorkerServerName: "worker.example.test",
	}
	resources := desiredResourcesWithStrategy(workerResourceName(request), request, config, deploymentAnnotations(request, config), true)
	deploymentSpec := resources[2].body["spec"].(map[string]any)
	strategy := deploymentSpec["strategy"].(map[string]any)
	if strategy["type"] != "RollingUpdate" {
		t.Fatalf("upgrade strategy=%#v", strategy)
	}
	rollingUpdate := strategy["rollingUpdate"].(map[string]string)
	if rollingUpdate["maxUnavailable"] != "0" || rollingUpdate["maxSurge"] != "1" {
		t.Fatalf("rolling update policy=%#v", rollingUpdate)
	}
}

func TestCredentialDirectoryReadsKubernetesDeploymentDescriptor(t *testing.T) {
	directory := t.TempDir()
	descriptor := `{"namespace":"agents","workerImageRepository":"registry.example.test/cloud-agents/worker","workerCredentialSecretRef":"worker-alpha","workerSpiffeId":"spiffe://cloud-agents.test/workers/kubernetes-alpha","workerServerName":"worker.example.test"}`
	path := filepath.Join(directory, "kubernetes-alpha.deployment.json")
	if err := os.WriteFile(path, []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials, err := NewCredentialDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	config, err := credentials.readDeploymentConfig("kubernetes-alpha")
	if err != nil || config.Namespace != "agents" || config.WorkerCredentialSecretRef != "worker-alpha" {
		t.Fatalf("config=%#v error=%v", config, err)
	}
	if err := os.WriteFile(path, []byte(descriptor[:len(descriptor)-1]+`,"token":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.readDeploymentConfig("kubernetes-alpha"); !errors.Is(err, ErrDeploymentConfigInvalid) {
		t.Fatalf("secret field error=%v", err)
	}
	if validDNSSubdomain("secret..name") {
		t.Fatal("invalid Kubernetes DNS subdomain accepted")
	}
}
