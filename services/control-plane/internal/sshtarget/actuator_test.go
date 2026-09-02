package sshtarget

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/dockertarget"
)

func TestSSHRemoteDockerWorkerIsOwnedIdempotentAndCleaned(t *testing.T) {
	request := dockertarget.DeployRequest{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", TargetID: "ssh-alpha", LeaseID: "lease-alpha",
		TargetGeneration: 1, LeaseGeneration: 2, ReleaseDigest: "sha256:" + strings.Repeat("a", 64),
		ProviderCredentialRef: "provider-alpha", CPULimitMillis: 1500, MemoryLimitBytes: 512 << 20,
	}
	config := dockertarget.DeploymentConfig{
		WorkerImageRepository: "registry.example.test/cloud-agents/worker", WorkerCredentialRef: "worker-alpha",
		WorkerSPIFFEID: "spiffe://cloud-agents.test/workers/ssh-alpha", WorkerServerName: "worker.example.test",
	}
	name, image := dockertarget.WorkerContainerName(request), config.WorkerImageRepository+"@"+request.ReleaseDigest
	labels := dockertarget.DeploymentLabels(request, config)
	actualLabels := maps.Clone(labels)
	present, running, runs, starts, deletes := false, false, 0, 0, 0
	var runCommand string
	host := newTestSSHHost(t, func(command string) (string, uint32) {
		switch {
		case strings.HasPrefix(command, "docker 'volume' 'inspect'"):
			if strings.Contains(command, "'worker-alpha'") {
				return "worker-alpha\n", 0
			}
			if strings.Contains(command, "'provider-alpha'") {
				return "provider-alpha\n", 0
			}
		case strings.HasPrefix(command, "docker 'ps'"):
			if present {
				return name + "\n", 0
			}
			return "", 0
		case strings.HasPrefix(command, "docker 'run'"):
			present, running, runs, runCommand = true, true, runs+1, command
			return "container-alpha\n", 0
		case strings.HasPrefix(command, "docker 'inspect'") && present:
			value, _ := json.Marshal([]map[string]any{{
				"Config": map[string]any{"Image": image, "Labels": actualLabels},
				"State":  map[string]any{"Running": running},
				"NetworkSettings": map[string]any{"Ports": map[string]any{
					remoteWorkerPort: []map[string]string{{"HostPort": "32768"}},
				}},
			}})
			return string(value), 0
		case strings.HasPrefix(command, "docker 'start'") && present:
			running, starts = true, starts+1
			return name + "\n", 0
		case strings.HasPrefix(command, "docker 'rm'") && present:
			present, running, deletes = false, false, deletes+1
			return name + "\n", 0
		}
		return "", 1
	})
	descriptor, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host.directory, "host-alpha.deployment.json"), descriptor, 0o600); err != nil {
		t.Fatal(err)
	}
	client, _, err := host.credentials.connect(context.Background(), host.endpoint, "host-alpha", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, volume := range []string{config.WorkerCredentialRef, request.ProviderCredentialRef} {
		if err := requireRemoteVolume(client, volume); err != nil {
			t.Fatal(err)
		}
	}
	if err := ensureRemoteWorker(client, name, image, request, config, labels); err != nil {
		t.Fatal(err)
	}
	running = false
	if err := ensureRemoteWorker(client, name, image, request, config, labels); err != nil {
		t.Fatal(err)
	}
	if inspect, err := waitForRemoteWorker(context.Background(), client, name); err != nil {
		t.Fatal(err)
	} else if port, err := remotePublishedWorkerPort(inspect); err != nil || port != "32768" {
		t.Fatalf("port=%q error=%v", port, err)
	}
	for range 2 {
		if err := cleanupRemoteWorker(context.Background(), client, name, image, labels); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 || starts != 1 || deletes != 1 || present {
		t.Fatalf("runs=%d starts=%d deletes=%d present=%v", runs, starts, deletes, present)
	}
	present = true
	actualLabels["cloud-agents.dev/lease-generation"] = "1"
	if err := cleanupRemoteWorker(context.Background(), client, name, image, labels); !errors.Is(err, ErrDeploymentConflict) || deletes != 1 {
		t.Fatalf("stale generation cleanup error=%v deletes=%d", err, deletes)
	}
	actualLabels = maps.Clone(labels)
	workers, err := host.credentials.ListManagedWorkers(context.Background(), host.endpoint, "host-alpha", request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration)
	if err != nil || len(workers) != 1 || workers[0].Request != request {
		t.Fatalf("managed workers=%#v error=%v", workers, err)
	}
	for range 2 {
		if err := host.credentials.CleanupManagedWorker(context.Background(), host.endpoint, "host-alpha", workers[0]); err != nil {
			t.Fatal(err)
		}
	}
	if present || deletes != 2 {
		t.Fatalf("managed cleanup present=%v deletes=%d", present, deletes)
	}
	present, actualLabels = true, maps.Clone(labels)
	actualLabels["cloud-agents.dev/target-generation"] = "2"
	if _, err := host.credentials.ListManagedWorkers(context.Background(), host.endpoint, "host-alpha", request.TenantID, request.ProjectID, request.TargetID, request.TargetGeneration); !errors.Is(err, ErrDeploymentConflict) {
		t.Fatalf("future generation list error=%v", err)
	}
	present = false
	for _, expected := range []string{"'--pull' 'never'", "'--read-only'", "'--cap-drop' 'ALL'", "'--memory' '536870912'", "src=worker-alpha", "src=provider-alpha", image} {
		if !strings.Contains(runCommand, expected) {
			t.Fatalf("run command lacks %q: %s", expected, runCommand)
		}
	}
	if !strings.Contains(runCommand, "'--publish' '"+remoteWorkerHostPort(request)+":8091'") {
		t.Fatalf("run command does not pin a restart-stable host port: %s", runCommand)
	}
	for _, forbidden := range []string{"PRIVATE KEY", "apiKey", "authToken"} {
		if strings.Contains(runCommand, forbidden) {
			t.Fatalf("run command contains %q", forbidden)
		}
	}

	if err := os.WriteFile(filepath.Join(host.directory, "host-alpha.deployment.json"), append(descriptor[:len(descriptor)-1], []byte(`,"apiKey":"secret"}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := host.credentials.readDeploymentConfig("host-alpha"); err != ErrDeploymentConfigInvalid {
		t.Fatalf("secret descriptor field error=%v", err)
	}
}
