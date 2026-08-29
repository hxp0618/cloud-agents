//go:build !localdev

package main

import (
	"errors"
	"testing"

	workerv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1"
	workerkernel "github.com/hxp0618/cloud-agents/services/worker"
)

func TestParseProductionWorkerConfigRequiresExplicitTLSInputs(t *testing.T) {
	if _, err := parseProductionWorkerConfig(nil, nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("missing TLS config error = %v", err)
	}
	base := []string{"--tls-cert", "server.pem", "--tls-key", "server.key", "--client-ca", "clients.pem"}
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
	}); err != nil || cfg.listen != "worker.example:8091" {
		t.Fatalf("hostname listen = %q error = %v", cfg.listen, err)
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

func TestProductionWorkerRuntimeAdvertisesOperationDispatch(t *testing.T) {
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
	if descriptor == nil || len(descriptor.GetCapabilities()) != 3 || descriptor.GetCapabilities()[2] != workerv1alpha1.Capability_CAPABILITY_OPERATION_DISPATCH {
		t.Fatalf("production Runtime capabilities = %#v", descriptor.GetCapabilities())
	}
}
