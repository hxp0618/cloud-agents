package main

import "testing"

func TestParseConfigRequiresTLSOffLoopback(t *testing.T) {
	env := func(name string) string {
		return map[string]string{
			"CLOUD_AGENTS_PLATFORM_DATABASE_URL":                 "postgres://runtime@db/cloud_agents",
			"CLOUD_AGENTS_PLATFORM_DOCKER_CREDENTIALS_DIRECTORY": "/run/cloud-agents/credentials",
		}[name]
	}
	if _, err := parseConfig(nil, env); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig([]string{"--listen", ":8090"}, env); err == nil {
		t.Fatal("plaintext non-loopback listener accepted")
	}
	if _, err := parseConfig([]string{"--listen", ":8090", "--tls-cert", "/tls/cert", "--tls-key", "/tls/key"}, env); err != nil {
		t.Fatal(err)
	}
}
