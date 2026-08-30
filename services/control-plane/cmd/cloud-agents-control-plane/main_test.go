//go:build localdev

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseControlPlaneConfigUsesExplicitDatabaseURL(t *testing.T) {
	config, err := parseControlPlaneConfig([]string{"--database-url", "postgres://task-local", "--listen", "127.0.0.1:9090"}, func(string) string {
		return "postgres://environment"
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.databaseURL != "postgres://task-local" || config.listen != "127.0.0.1:9090" {
		t.Fatalf("config = %#v", config)
	}
}

func TestParseControlPlaneConfigAcceptsLocalRuntimeBridge(t *testing.T) {
	config, err := parseControlPlaneConfig([]string{
		"--database-url", "postgres://task-local", "--listen", "127.0.0.1:9090",
		"--worker-endpoint", "http://127.0.0.1:8091", "--worker-token-file", "/tmp/cloud-agents-worker.token", "--workspace-directory", "/tmp/workspace",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.workerEndpoint != "http://127.0.0.1:8091" || config.workerTokenFile != "/tmp/cloud-agents-worker.token" || config.workspaceDirectory != "/tmp/workspace" {
		t.Fatalf("config = %#v", config)
	}
}

func TestLocalWorkerEndpointAndTokenAreLoopbackOnly(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "worker.token")
	if err := os.WriteFile(tokenFile, []byte("worker-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if endpoint, err := validateLocalWorkerEndpoint("http://127.0.0.1:8091/"); err != nil || endpoint != "http://127.0.0.1:8091" {
		t.Fatalf("endpoint = %q, err = %v", endpoint, err)
	}
	if token, err := readLocalWorkerToken(tokenFile); err != nil || token != "worker-token" {
		t.Fatalf("token length = %d, err = %v", len(token), err)
	}
	if err := os.Chmod(tokenFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLocalWorkerToken(tokenFile); !errors.Is(err, errInvalidRuntimeConfig) {
		t.Fatalf("broad token mode error = %v", err)
	}
	for _, endpoint := range []string{"https://127.0.0.1:8091", "http://localhost:8091", "http://192.0.2.1:8091"} {
		if _, err := validateLocalWorkerEndpoint(endpoint); !errors.Is(err, errInvalidRuntimeConfig) {
			t.Errorf("%s should be rejected", endpoint)
		}
	}
}

func TestValidateLoopbackListenAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080"} {
		if err := validateLoopbackListenAddress(address); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", ":8080", "192.168.31.234:8080", "localhost:8080", "example.test:8080"} {
		if !errors.Is(validateLoopbackListenAddress(address), errNonLoopbackListen) {
			t.Errorf("%s should be rejected", address)
		}
	}
}

func TestParseLoopbackDatabaseConfig(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://runtime@127.0.0.1:5432/cloud_agents?sslmode=disable",
		"postgres://runtime@[::1]:5432/cloud_agents?sslmode=disable",
		"postgres://runtime@/cloud_agents?host=/tmp",
	} {
		config, err := parseLoopbackDatabaseConfig(databaseURL)
		if err != nil {
			t.Errorf("%s: %v", databaseURL, err)
			continue
		}
		if config.AfterConnect == nil {
			t.Errorf("%s: runtime connection authority hook is nil", databaseURL)
		}
	}
	for _, databaseURL := range []string{
		"postgres://runtime@192.168.31.234:5432/cloud_agents?sslmode=disable",
		"postgres://runtime@example.test:5432/cloud_agents?sslmode=disable",
		"postgres://runtime@localhost:5432/cloud_agents?sslmode=disable",
		"postgres://runtime@127.0.0.1:5432,192.168.31.234:5432/cloud_agents?sslmode=disable",
		"not-a-database-url",
	} {
		if _, err := parseLoopbackDatabaseConfig(databaseURL); !errors.Is(err, errNonLoopbackDatabase) {
			t.Errorf("%s should be rejected, got %v", databaseURL, err)
		}
	}
}

func TestRuntimeAuthoritySQLPinsEveryPG16RuntimeMembership(t *testing.T) {
	for name, fragment := range map[string]string{
		"all incoming memberships":  "WHERE incoming_membership.roleid = runtime_role.oid",
		"membership admin option":   "incoming_membership.admin_option",
		"member login":              "NOT incoming_member_role.rolcanlogin",
		"member inheritance":        "NOT incoming_member_role.rolinherit",
		"member superuser":          "incoming_member_role.rolsuper",
		"member create database":    "incoming_member_role.rolcreatedb",
		"member create role":        "incoming_member_role.rolcreaterole",
		"member replication":        "incoming_member_role.rolreplication",
		"member bypass RLS":         "incoming_member_role.rolbypassrls",
		"member usage":              "incoming_member_role.oid,\n                  runtime_role.oid,\n                  'USAGE'",
		"member child closure":      "child_membership.roleid = incoming_member_role.oid",
		"exact authority":           "member_authority_membership.member = incoming_member_role.oid",
		"grantor superuser":         "NOT incoming_grantor_role.rolsuper",
		"grantor forbidden roles":   "incoming_grantor_role.rolname IN (",
		"grantor authority closure": "WITH RECURSIVE grantor_memberships (roleid)",
	} {
		if !strings.Contains(runtimeAuthoritySQL, fragment) {
			t.Errorf("runtime authority SQL lacks %s closure", name)
		}
	}
	if count := strings.Count(runtimeAuthoritySQL, "->>'inherit_option'"); count != 2 {
		t.Errorf("inherit_option checks = %d, want direct plus every incoming membership", count)
	}
	if count := strings.Count(runtimeAuthoritySQL, "->>'set_option'"); count != 2 {
		t.Errorf("set_option checks = %d, want direct plus every incoming membership", count)
	}
	if strings.Contains(runtimeAuthoritySQL, ".inherit_option") || strings.Contains(runtimeAuthoritySQL, ".set_option") {
		t.Fatal("runtime authority SQL directly references version-specific membership columns")
	}
}

func TestWriteLocalTokenFileIs0600AndExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := writeLocalTokenFile(path, "ephemeral-token"); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o", mode)
	}
	if err := writeLocalTokenFile(path, "second-token"); err == nil {
		t.Fatal("expected existing token file to be rejected")
	}
}
