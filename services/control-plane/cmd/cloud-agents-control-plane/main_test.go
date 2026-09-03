//go:build localdev

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authn"
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
		"--docker-credentials-directory", "/tmp/docker-targets",
		"--kubernetes-credentials-directory", "/tmp/kubernetes-targets",
		"--ssh-credentials-directory", "/tmp/ssh-targets",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if config.workerEndpoint != "http://127.0.0.1:8091" || config.workerTokenFile != "/tmp/cloud-agents-worker.token" || config.workspaceDirectory != "/tmp/workspace" || config.dockerCredentials != "/tmp/docker-targets" || config.kubernetesCredentials != "/tmp/kubernetes-targets" || config.sshCredentials != "/tmp/ssh-targets" {
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

func TestRefreshLocalTokenFileAtomicallyReplacesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	adminPath := filepath.Join(t.TempDir(), "admin-token")
	verifier, err := authn.NewLocalVerifier(authn.LocalVerifierConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Invalidate()
	claims := authn.LocalTokenClaims{TenantID: "tenant-local", Subject: "user-local"}
	initial, err := verifier.IssueToken(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLocalTokenFile(path, initial); err != nil {
		t.Fatal(err)
	}
	adminInitial, err := verifier.IssueAdminToken(claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLocalTokenFile(adminPath, adminInitial); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errorsChannel := refreshLocalTokenFiles(ctx, verifier, claims, path, adminPath, time.Millisecond)
	deadline := time.Now().Add(time.Second)
	for {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		adminContents, adminReadErr := os.ReadFile(adminPath)
		if string(contents) != initial+"\n" && adminReadErr == nil && string(adminContents) != adminInitial+"\n" {
			info, statErr := os.Stat(path)
			if statErr != nil {
				t.Fatal(statErr)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("token mode = %v", info.Mode().Perm())
			}
			break
		}
		select {
		case refreshErr := <-errorsChannel:
			t.Fatal(refreshErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("local token was not refreshed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	for refreshErr := range errorsChannel {
		t.Fatal(refreshErr)
	}
}
