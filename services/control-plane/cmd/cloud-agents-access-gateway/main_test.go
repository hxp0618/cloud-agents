package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

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
	kubernetesOnly := func(name string) string {
		if name == "CLOUD_AGENTS_PLATFORM_DATABASE_URL" {
			return "postgres://runtime@db/cloud_agents"
		}
		if name == "CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY" {
			return "/run/cloud-agents/kubernetes-credentials"
		}
		return ""
	}
	if _, err := parseConfig(nil, kubernetesOnly); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig([]string{"--listen", ":8090"}, env); err == nil {
		t.Fatal("plaintext non-loopback listener accepted")
	}
	if _, err := parseConfig([]string{"--listen", ":8090", "--tls-cert", "/tls/cert", "--tls-key", "/tls/key"}, env); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConfig([]string{"--ssh-listen", "127.0.0.1:2222"}, env); err == nil {
		t.Fatal("incomplete SSH configuration accepted")
	}
	if _, err := parseConfig([]string{"--ssh-listen", "invalid", "--ssh-host-key", "/run/host-key"}, env); err == nil {
		t.Fatal("invalid SSH listener accepted")
	}
	if _, err := parseConfig([]string{"--ssh-listen", ":2222", "--ssh-host-key", "/run/host-key"}, env); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSSHSignerRequiresPrivateRegularFile(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "host-key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHSigner(path); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "host-key-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHSigner(link); err == nil {
		t.Fatal("symlinked SSH host key accepted")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSSHSigner(path); err == nil {
		t.Fatal("public SSH host key permissions accepted")
	}
}
