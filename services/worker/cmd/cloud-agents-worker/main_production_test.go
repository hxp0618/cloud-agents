//go:build !localdev

package main

import (
	"errors"
	"testing"
)

func TestParseProductionWorkerConfigRequiresExplicitTLSInputs(t *testing.T) {
	if _, err := parseProductionWorkerConfig(nil); !errors.Is(err, errInvalidProductionWorkerConfig) {
		t.Fatalf("missing TLS config error = %v", err)
	}
	base := []string{"--tls-cert", "server.pem", "--tls-key", "server.key", "--client-ca", "clients.pem"}
	for _, address := range []string{"127.0.0.1:0", ":not-a-port", ":65536", "worker.example"} {
		args := append([]string{"--listen", address}, base...)
		if _, err := parseProductionWorkerConfig(args); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Errorf("listen %q error = %v", address, err)
		}
	}
	validArgs := append(append([]string{}, base...), "--worker-spiffe-id", "spiffe://cloud-agents.example/worker", "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
	validArgs = append([]string{"--listen", "worker.example:8091"}, validArgs...)
	if cfg, err := parseProductionWorkerConfig(validArgs); err != nil || cfg.listen != "worker.example:8091" {
		t.Fatalf("hostname listen config = %#v error = %v", cfg, err)
	}
	for _, identity := range []string{"", "https://cloud-agents.example/worker", "spiffe://", "spiffe://cloud-agents.example/worker?bad=1"} {
		args := append(append([]string{}, base...), "--worker-spiffe-id", identity, "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
		if _, err := parseProductionWorkerConfig(args); !errors.Is(err, errInvalidProductionWorkerConfig) {
			t.Errorf("identity %q error = %v", identity, err)
		}
	}
	args := append(append([]string{"--listen", ":8091"}, base...), "--worker-spiffe-id", "spiffe://cloud-agents.example/worker", "--runtime-command", "/opt/cloud-agents/runtime", "--admission-lease-id", "lease-production", "--admission-generation", "7")
	if cfg, err := parseProductionWorkerConfig(args); err != nil || cfg.listen != ":8091" {
		t.Fatalf("valid config = %#v error = %v", cfg, err)
	}
}
