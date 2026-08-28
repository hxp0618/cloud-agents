//go:build !localdev

package main

import (
	"bytes"
	"testing"
)

func TestParseProductionConfigRequiresTLSAndUsesEnvironment(t *testing.T) {
	if _, err := parseProductionConfig(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected required production configuration error")
	}
	values := map[string]string{
		productionDatabaseEnvironment:            "postgres://runtime@db/cloud_agents",
		productionAuthConfigEnvironment:          "/etc/cloud-agents/auth.json",
		productionWorkerEndpointEnvironment:      "https://worker:8091",
		productionWorkerSPIFFEEnvironment:        "spiffe://cloud-agents.test/worker",
		productionWorkerClientCertEnvironment:    "/etc/cloud-agents/worker-client.crt",
		productionWorkerClientKeyEnvironment:     "/etc/cloud-agents/worker-client.key",
		productionWorkerCAEnvironment:            "/etc/cloud-agents/worker-ca.crt",
		productionWorkspaceEnvironment:           "/workspace",
		productionAdmissionLeaseEnvironment:      "runtime-lease",
		productionAdmissionGenerationEnvironment: "7",
		productionAdmissionTokenEnvironment:      "runtime-token",
	}
	config, err := parseProductionConfig([]string{"--listen", "127.0.0.1:9443", "--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.listen != "127.0.0.1:9443" || config.database == "" || config.authPath == "" || config.tlsCert != "/tmp/cert" || config.tlsKey != "/tmp/key" || config.workerEndpoint != "https://worker:8091" || config.admissionGeneration != 7 || !bytes.Equal(config.admissionToken, []byte("runtime-token")) {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseProductionConfigRejectsPartialTLS(t *testing.T) {
	if _, err := parseProductionConfig([]string{"--database-url", "postgres://runtime@db/cloud_agents", "--auth-config", "/etc/cloud-agents/auth.json", "--tls-cert", "/tmp/cert"}, nil); err == nil {
		t.Fatal("expected partial TLS configuration error")
	}
}
