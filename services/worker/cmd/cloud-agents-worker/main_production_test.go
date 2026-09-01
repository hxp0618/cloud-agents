//go:build !localdev

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestProductionWorkerErrorWriterSuppressesEmptyTLSProbe(t *testing.T) {
	var output bytes.Buffer
	writer := productionWorkerErrorWriter{output: &output}
	probe := []byte("2026/09/01 01:53:08 http: TLS handshake error from 192.168.194.1:45166: EOF\n")
	if written, err := writer.Write(probe); err != nil || written != len(probe) || output.Len() != 0 {
		t.Fatalf("probe write = %d/%v output=%q", written, err, output.String())
	}
	invalidClient := []byte("2026/09/01 01:53:09 http: TLS handshake error from 192.168.194.1:45167: tls: client didn't provide a certificate\n")
	if written, err := writer.Write(invalidClient); err != nil || written != len(invalidClient) || !bytes.Equal(output.Bytes(), invalidClient) {
		t.Fatalf("invalid client write = %d/%v output=%q", written, err, output.String())
	}
}

func TestParseProductionWorkerConfigRequiresExplicitTLSInputs(t *testing.T) {
	if _, err := parseProductionWorkerConfig(nil, nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("missing TLS config error = %v", err)
	}
	base := []string{"--tls-cert", "server.pem", "--tls-key", "server.key", "--client-ca", "clients.pem", "--runtime-directory", "/workspace", "--provider-credential-directory", "/run/cloud-agents/provider-credentials"}
	for _, address := range []string{"127.0.0.1:0", ":not-a-port", ":65536", "worker.example"} {
		args := append([]string{"--listen", address}, base...)
		if _, err := parseProductionWorkerConfig(args, nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Errorf("listen %q error = %v", address, err)
		}
	}
	validArgs := append(append([]string{}, base...), "--worker-spiffe-id", "spiffe://cloud-agents.example/worker", "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
	validArgs = append([]string{"--listen", "worker.example:8091"}, validArgs...)
	if cfg, err := parseProductionWorkerConfig(validArgs, func(name string) string {
		if name == admissionTokenEnvironment {
			return "admission-token"
		}
		return ""
	}); err != nil || cfg.listen != "worker.example:8091" || cfg.runtimeMaxSessions != workerkernel.DefaultRuntimeMaxSessions {
		t.Fatalf("hostname listen = %q error = %v", cfg.listen, err)
	}
	customArgs := append(append([]string{}, validArgs...), "--runtime-max-sessions", "7")
	if cfg, err := parseProductionWorkerConfig(customArgs, func(string) string { return "admission-token" }); err != nil || cfg.runtimeMaxSessions != 7 {
		t.Fatalf("custom Runtime capacity = %d error = %v", cfg.runtimeMaxSessions, err)
	}
	for _, value := range []string{"0", "1025"} {
		args := append(append([]string{}, validArgs...), "--runtime-max-sessions", value)
		if _, err := parseProductionWorkerConfig(args, func(string) string { return "admission-token" }); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Fatalf("Runtime max sessions %s error = %v", value, err)
		}
	}
	if _, err := parseProductionWorkerConfig(append(validArgs, "--provider-credential-directory", "relative"), func(string) string { return "admission-token" }); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("relative Provider credential directory error = %v", err)
	}
	for _, directory := range []string{"relative", "/"} {
		if _, err := parseProductionWorkerConfig(append(validArgs, "--runtime-directory", directory), func(string) string { return "admission-token" }); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Fatalf("Runtime directory %q error = %v", directory, err)
		}
	}
	for _, identity := range []string{"", "https://cloud-agents.example/worker", "spiffe://", "spiffe://cloud-agents.example/worker?bad=1"} {
		args := append(append([]string{}, base...), "--worker-spiffe-id", identity, "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
		if _, err := parseProductionWorkerConfig(args, nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Errorf("identity %q error = %v", identity, err)
		}
	}
	args := append(append([]string{"--listen", ":8091"}, base...), "--worker-spiffe-id", "spiffe://cloud-agents.example/worker", "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
	if _, err := parseProductionWorkerConfig(args, nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("missing admission token error = %v", err)
	}
	if cfg, err := parseProductionWorkerConfig(args, func(name string) string {
		if name == admissionTokenEnvironment {
			return "admission-token"
		}
		return ""
	}); err != nil || cfg.listen != ":8091" {
		t.Fatalf("valid listen = %q error = %v", cfg.listen, err)
	}
	tokenFile := filepath.Join(t.TempDir(), "admission-token")
	if err := os.WriteFile(tokenFile, []byte("token-from-file"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileArgs := append(append([]string{}, args...), "--admission-token-file", tokenFile)
	if cfg, err := parseProductionWorkerConfig(fileArgs, nil); err != nil || string(cfg.admissionToken) != "token-from-file" {
		t.Fatalf("file token bytes = %d error = %v", len(cfg.admissionToken), err)
	}
	if _, err := parseProductionWorkerConfig(fileArgs, func(string) string { return "ambiguous-token" }); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("ambiguous token error = %v", err)
	}
	environment := map[string]string{
		admissionLeaseIDEnvironment:    "lease-from-secret",
		admissionGenerationEnvironment: "9",
		admissionTokenEnvironment:      "token-from-secret",
	}
	envArgs := append(append([]string{"--listen", ":8091"}, base...), "--worker-spiffe-id", "spiffe://cloud-agents.example/worker", "--runtime-command", "/opt/cloud-agents/runtime")
	cfg, err := parseProductionWorkerConfig(envArgs, func(name string) string { return environment[name] })
	if err != nil || cfg.admissionLeaseID != "lease-from-secret" || cfg.admissionGeneration != 9 || string(cfg.admissionToken) != "token-from-secret" {
		t.Fatalf("environment lease = %q generation = %d token bytes = %d error = %v", cfg.admissionLeaseID, cfg.admissionGeneration, len(cfg.admissionToken), err)
	}
}

func TestProductionWorkerDoesNotAdvertiseUnavailableOperationDispatch(t *testing.T) {
	service, err := workerkernel.NewService(workerkernel.Config{
		WorkerIdentity: &workerv1alpha1.WorkloadIdentity{
			SpiffeId:    "spiffe://cloud-agents.example/worker",
			TrustDomain: "cloud-agents.example",
		},
		Capabilities:     productionWorkerCapabilities(),
		AdmissionLeaseID: "lease-production", AdmissionGeneration: 1, AdmissionToken: []byte("token"),
	})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := service.ProtocolDescriptor()
	capabilities := descriptor.GetCapabilities()
	if descriptor == nil || len(capabilities) != 2 || capabilities[0] != workerv1alpha1.Capability_CAPABILITY_NEGOTIATION || capabilities[1] != workerv1alpha1.Capability_CAPABILITY_HEALTH {
		t.Fatalf("production Worker capabilities = %#v", capabilities)
	}
}

func TestProductionRuntimeEnvironmentAllowsOnlyRuntimeConfiguration(t *testing.T) {
	filtered := productionRuntimeEnvironment([]string{
		"PATH=/usr/bin",
		"HOME=/home/cloud-agents",
		"CODEX_HOME=/var/lib/cloud-agents/codex",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"NODE_EXTRA_CA_CERTS=/run/cloud-agents/ca.pem",
		"CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent",
		"CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
		"CLOUD_AGENT_PROVIDER_HTTP_PROXY=http://proxy.example:8080",
		"CLOUD_AGENT_PROVIDER_PIP_CONFIG_FILE=/run/cloud-agents/pip.conf",
		admissionLeaseIDEnvironment + "=lease",
		admissionGenerationEnvironment + "=7",
		admissionTokenEnvironment + "=secret",
		"OPENAI_API_KEY=provider-key",
		"ANTHROPIC_AUTH_TOKEN=provider-token",
		"AWS_SECRET_ACCESS_KEY=cloud-secret",
		"DATABASE_URL=postgres://database-secret",
		"NODE_OPTIONS=--require=/tmp/injected.cjs",
		"CLOUD_AGENT_PROVIDER_CREDENTIAL_FD=99",
		"SYNARA_PROVIDER_CREDENTIAL_FD=98",
		"SYNARA_PROVIDER_HTTP_PROXY=http://legacy-proxy.example:8080",
		"SYNARA_PROVIDER_OUTER_SANDBOX_PROFILE=legacy-profile",
		"CLOUD_AGENT_FUTURE_SECRET=secret",
		"malformed",
	})
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/cloud-agents",
		"CODEX_HOME=/var/lib/cloud-agents/codex",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"NODE_EXTRA_CA_CERTS=/run/cloud-agents/ca.pem",
		"CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS=codex,claudeAgent",
		"CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE=single-tenant-trusted-v1",
		"CLOUD_AGENT_PROVIDER_HTTP_PROXY=http://proxy.example:8080",
		"CLOUD_AGENT_PROVIDER_PIP_CONFIG_FILE=/run/cloud-agents/pip.conf",
	}
	if !slices.Equal(filtered, want) {
		t.Fatalf("filtered environment = %#v, want %#v", filtered, want)
	}
}
