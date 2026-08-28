//go:build !localdev

package main

import "testing"

func TestParseProductionConfigRequiresTLSAndUsesEnvironment(t *testing.T) {
	if _, err := parseProductionConfig(nil, func(string) string { return "" }); err == nil {
		t.Fatal("expected required production configuration error")
	}
	config, err := parseProductionConfig([]string{"--listen", "127.0.0.1:9443", "--tls-cert", "/tmp/cert", "--tls-key", "/tmp/key"}, func(name string) string {
		switch name {
		case productionDatabaseEnvironment:
			return "postgres://runtime@db/cloud_agents"
		case productionAuthConfigEnvironment:
			return "/etc/cloud-agents/auth.json"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.listen != "127.0.0.1:9443" || config.database == "" || config.authPath == "" || config.tlsCert != "/tmp/cert" || config.tlsKey != "/tmp/key" {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseProductionConfigRejectsPartialTLS(t *testing.T) {
	if _, err := parseProductionConfig([]string{"--database-url", "postgres://runtime@db/cloud_agents", "--auth-config", "/etc/cloud-agents/auth.json", "--tls-cert", "/tmp/cert"}, nil); err == nil {
		t.Fatal("expected partial TLS configuration error")
	}
}
