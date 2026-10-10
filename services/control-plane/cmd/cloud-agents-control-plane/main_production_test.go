//go:build !localdev

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseProductionConfigRequiresTLSAndUsesEnvironment(t *testing.T) {
	if _, err := parseProductionConfig(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected required production configuration error")
	}
	values := map[string]string{
		productionDatabaseEnvironment:                "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:              "/etc/cloud-agents/auth.json",
		productionWorkerEndpointEnvironment:          "https://worker:8091",
		productionWorkerSPIFFEEnvironment:            "spiffe://cloud-agents.test/worker",
		productionWorkerClientCertEnvironment:        "/etc/cloud-agents/worker-client.crt",
		productionWorkerClientKeyEnvironment:         "/etc/cloud-agents/worker-client.key",
		productionWorkerCAEnvironment:                "/etc/cloud-agents/worker-ca.crt",
		productionWorkspaceEnvironment:               "/workspace",
		productionProviderCredentialsEnvironment:     "/etc/cloud-agents/provider-credentials",
		productionDockerCredentialsEnvironment:       "/etc/cloud-agents/docker-targets",
		productionSnapshotDirectoryEnvironment:       "/etc/cloud-agents/snapshots",
		productionKubernetesCredentialsEnvironment:   "/etc/cloud-agents/kubernetes-targets",
		productionSSHCredentialsEnvironment:          "/etc/cloud-agents/ssh-targets",
		productionAccessGrantKeyEnvironment:          "/etc/cloud-agents/access-grant.key",
		productionRemoteWorkerCACertEnvironment:      "/etc/cloud-agents/remote-worker-ca.crt",
		productionRemoteWorkerCAKeyEnvironment:       "/etc/cloud-agents/remote-worker-ca.key",
		productionRemoteWorkerTrustDomainEnvironment: "remote-worker.test",
		productionAdmissionLeaseEnvironment:          "runtime-lease",
		productionAdmissionGenerationEnvironment:     "7",
		productionAdmissionTokenEnvironment:          "runtime-token",
	}
	args := []string{"--listen", "127.0.0.1:9443", "--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}
	getenv := func(name string) string { return values[name] }
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.listen != "127.0.0.1:9443" || config.database == "" || config.authPath == "" || config.tlsCert != "/tmp/cert" || config.tlsKey != "/tmp/key" || config.workerEndpoint != "https://worker:8091" || config.providerCredentials != "/etc/cloud-agents/provider-credentials" || config.dockerCredentials != "/etc/cloud-agents/docker-targets" || config.snapshotDirectory != "/etc/cloud-agents/snapshots" || config.kubernetesCredentials != "/etc/cloud-agents/kubernetes-targets" || config.sshCredentials != "/etc/cloud-agents/ssh-targets" || config.accessGrantKey != "/etc/cloud-agents/access-grant.key" || config.remoteWorkerCACert != "/etc/cloud-agents/remote-worker-ca.crt" || config.remoteWorkerCAKey != "/etc/cloud-agents/remote-worker-ca.key" || config.remoteWorkerTrustDomain != "remote-worker.test" || config.admissionGeneration != 7 || !bytes.Equal(config.admissionToken, []byte("runtime-token")) || config.maxConcurrentRequests != defaultProductionMaxConcurrentRequests {
		t.Fatalf("config = %#v", config)
	}
	for _, invalid := range []string{"0", "10001"} {
		candidate := append(append([]string{}, args...), "--max-concurrent-requests", invalid)
		if _, err := parseProductionConfig(candidate, getenv); err == nil {
			t.Fatalf("accepted max concurrent requests %s", invalid)
		}
	}
	if _, err := parseProductionConfig(append(append([]string{}, args...), "--docker-credentials-directory", " /tmp/docker-targets"), getenv); err == nil {
		t.Fatal("accepted invalid Docker credential directory")
	}
	if _, err := parseProductionConfig(append(append([]string{}, args...), "--kubernetes-credentials-directory", " /tmp/kubernetes-targets"), getenv); err == nil {
		t.Fatal("accepted invalid Kubernetes credential directory")
	}
	if _, err := parseProductionConfig(append(append([]string{}, args...), "--ssh-credentials-directory", " /tmp/ssh-targets"), getenv); err == nil {
		t.Fatal("accepted invalid SSH credential directory")
	}
	partialRemoteWorker := func(name string) string {
		if name == productionRemoteWorkerCAKeyEnvironment {
			return ""
		}
		return values[name]
	}
	if _, err := parseProductionConfig(args, partialRemoteWorker); err == nil {
		t.Fatal("accepted partial RemoteWorker certificate authority configuration")
	}
}

func TestParseProductionConfigRejectsPartialTLS(t *testing.T) {
	if _, err := parseProductionConfig([]string{"--database-url", "postgres://runtime@db/cloud_agents", "--auth-config", "/etc/cloud-agents/auth.json", "--tls-cert", "/tmp/cert"}, nil); err == nil {
		t.Fatal("expected partial TLS configuration error")
	}
}

func TestParseProductionConfigAllowsNoAgentRuntime(t *testing.T) {
	values := map[string]string{
		productionDatabaseEnvironment:       "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:     "/etc/cloud-agents/auth.json",
		productionAccessGrantKeyEnvironment: "/etc/cloud-agents/access-grant.key",
	}
	config, err := parseProductionConfig([]string{"--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.workerEndpoint != "" || config.workerClientCert != "" || config.workspaceDirectory != "" || len(config.admissionToken) != 0 {
		t.Fatalf("unexpected Managed Agent Runtime configuration: %#v", config)
	}
	for name, value := range map[string]string{
		productionWorkerClientCertEnvironment: "/etc/cloud-agents/worker-client.crt",
		productionWorkspaceEnvironment:        "/workspace",
		productionAdmissionTokenEnvironment:   "runtime-token",
	} {
		partial := func(candidate string) string {
			if candidate == name {
				return value
			}
			return values[candidate]
		}
		if _, err := parseProductionConfig([]string{"--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, partial); err == nil {
			t.Fatalf("accepted partial Managed Agent Runtime field %s", name)
		}
	}
}

func TestParseProductionConfigAllowsEnvironmentRoutedWorkers(t *testing.T) {
	values := map[string]string{
		productionDatabaseEnvironment:         "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:       "/etc/cloud-agents/auth.json",
		productionWorkerClientCertEnvironment: "/etc/cloud-agents/worker-client.crt",
		productionWorkerClientKeyEnvironment:  "/etc/cloud-agents/worker-client.key",
		productionWorkerCAEnvironment:         "/etc/cloud-agents/worker-ca.crt",
		productionWorkspaceEnvironment:        "/workspace",
		productionAccessGrantKeyEnvironment:   "/etc/cloud-agents/access-grant.key",
		productionAdmissionTokenEnvironment:   "runtime-token",
	}
	args := []string{"--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}
	getenv := func(name string) string { return values[name] }
	config, err := parseProductionConfig(args, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.workerEndpoint != "" || config.workerSPIFFE != "" || config.admissionLeaseID != "" || config.admissionGeneration != 0 {
		t.Fatalf("unexpected fixed Worker route: %#v", config)
	}
	if _, err := parseProductionConfig(append(args, "--worker-endpoint", "https://worker:8091"), getenv); err == nil {
		t.Fatal("accepted partial fixed Worker route")
	}
}

func TestParseProductionConfigAllowsFoundationRuntime(t *testing.T) {
	values := map[string]string{
		productionDatabaseEnvironment:            "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:          "/etc/cloud-agents/auth.json",
		productionWorkspaceEnvironment:           "/workspace",
		productionProviderCredentialsEnvironment: "/etc/cloud-agents/provider-credentials",
		productionDockerCredentialsEnvironment:   "/etc/cloud-agents/docker-targets",
		productionAccessGrantKeyEnvironment:      "/etc/cloud-agents/access-grant.key",
	}
	config, err := parseProductionConfig([]string{"--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.providerCredentials == "" || config.workspaceDirectory != "/workspace" || config.workerClientCert != "" || len(config.admissionToken) != 0 {
		t.Fatalf("Foundation Runtime config = %#v", config)
	}
}

func TestProductionAccessLogIsCorrelatedAndDoesNotLeakRequestInputs(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Request-ID", "request-alpha")
		writer.WriteHeader(http.StatusForbidden)
	}))
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-alpha/projects?pageToken=opaque-secret", nil)
	request.Header.Set("Authorization", "Bearer bearer-secret")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event["msg"] != "http request" || event["method"] != http.MethodPost || event["path"] != "/v1/tenants/tenant-alpha/projects" || event["status"] != float64(http.StatusForbidden) || event["request_id"] != "request-alpha" {
		t.Fatalf("access log = %#v", event)
	}
	if _, ok := event["duration_ms"].(float64); !ok || strings.Contains(output.String(), "opaque-secret") || strings.Contains(output.String(), "bearer-secret") {
		t.Fatalf("unsafe access log = %s", output.String())
	}

	output.Reset()
	probe := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusOK) }))
	probe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if output.Len() != 0 {
		t.Fatalf("successful probe was logged: %s", output.String())
	}
	failedProbe := productionAccessLogHandler(logger, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusServiceUnavailable) }))
	failedProbe.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if !strings.Contains(output.String(), `"status":503`) {
		t.Fatalf("failed probe was not logged: %s", output.String())
	}
}
