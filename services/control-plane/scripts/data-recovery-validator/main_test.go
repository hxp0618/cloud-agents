package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckedInRecoveryPlanIsClosedAndSameBits(t *testing.T) {
	repoRoot := findRepositoryRoot(t)
	manifestPath := filepath.Join(repoRoot, "services", "control-plane", "migrations", "manifest.json")
	plan, err := loadRecoveryPlan(manifestPath, repoRoot)
	if err != nil {
		t.Fatalf("load checked-in recovery plan: %v", err)
	}
	var ledger bytes.Buffer
	if err := plan.writeLedgerTSV(&ledger); err != nil {
		t.Fatalf("write ledger TSV: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(ledger.String(), "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("ledger row count = %d, want 12", len(lines))
	}
	for index, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 17 {
			t.Fatalf("ledger row %d field count = %d, want 17", index, len(fields))
		}
		if fields[0] != plan.manifest.SchemaBundle.Migrations[index].ID {
			t.Fatalf("ledger row %d id = %q", index, fields[0])
		}
	}
	if !strings.Contains(lines[0], "\t\\N\t") {
		t.Fatal("first ledger row does not preserve a PostgreSQL NULL predecessor")
	}

	var apply bytes.Buffer
	if err := plan.writeApplySQL(&apply, "/workspace/repo", "/workspace/run/ledger.tsv"); err != nil {
		t.Fatalf("write apply SQL: %v", err)
	}
	applyText := apply.String()
	for _, entry := range plan.manifest.SchemaBundle.Migrations {
		needle := "\\i '/workspace/repo/" + entry.SQLArtifact.Path + "'"
		if strings.Count(applyText, needle) != 1 {
			t.Fatalf("apply SQL reference count for %s = %d", entry.ID, strings.Count(applyText, needle))
		}
	}
	if strings.Count(applyText, "BEGIN;") != 13 || strings.Count(applyText, "COMMIT;") != 13 ||
		strings.Count(applyText, `\copy cloud_agents.schema_migrations`) != 1 {
		t.Fatalf("apply SQL transaction or ledger boundary drifted:\n%s", applyText)
	}
}

func TestRecoveryPlanRejectsArtifactDriftAndUnsafePaths(t *testing.T) {
	repoRoot := findRepositoryRoot(t)
	manifestPath := filepath.Join(repoRoot, "services", "control-plane", "migrations", "manifest.json")
	copyRoot := t.TempDir()
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	copyManifest := filepath.Join(copyRoot, "manifest.json")
	if err := os.WriteFile(copyManifest, manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoveryPlan(copyManifest, copyRoot); err == nil || !strings.Contains(err.Error(), "read migration 000001 artifact") {
		t.Fatalf("missing checked-in artifacts error = %v", err)
	}
	for _, path := range []string{"", "relative", "/workspace/../escape", "/workspace/repo'bad", "/workspace//repo"} {
		if validAbsoluteContainerPath(path) {
			t.Fatalf("unsafe container path accepted: %q", path)
		}
	}
}

func TestRecoveryScriptKeepsLocalOwnedClosedBoundary(t *testing.T) {
	repoRoot := findRepositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "services", "control-plane", "scripts", "test-p1-data-recovery-postgres.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	required := []string{
		`source "$script_dir/p1-data-recovery-cleanup.sh"`,
		"postgres@sha256:742f40ea20b9ff2ff31db5458d127452988a2164df9e17441e191f3b72252193",
		"--pull=never",
		"com.hxp0618.cloud-agents.test-run",
		"pg_dump --format=custom --compress=0 --no-owner",
		"pg_restore --exit-on-error --no-owner --role=cloud_agents_migration_owner",
		`if [[ $restored_digest != "$source_digest" ]]`,
		`rm -rf "$artifact_dir" || status=1`,
		"TestDurableCoordinationPostgresRecovery",
		`p1_data_recovery_finish "$pass_line"`,
	}
	for _, value := range required {
		if !strings.Contains(script, value) {
			t.Fatalf("recovery script lost required boundary %q", value)
		}
	}
	forbidden := []string{"docker pull", "--network host", "ssh ", "kubectl ", "go test ./...", "go test -race"}
	for _, value := range forbidden {
		if strings.Contains(script, value) {
			t.Fatalf("recovery script contains forbidden expansion %q", value)
		}
	}
}

func TestRecoveryCleanupFixtureUsesBash32(t *testing.T) {
	repoRoot := findRepositoryRoot(t)
	fixture := filepath.Join(repoRoot, "services", "control-plane", "scripts", "test-p1-data-recovery-cleanup.sh")
	command := exec.Command("/bin/bash", fixture)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run Bash 3.2 cleanup fixture: %v\n%s", err, output)
	}
	if string(output) != "p1-data-recovery-cleanup-fixture: PASS\n" {
		t.Fatalf("cleanup fixture output = %q", output)
	}
}

func findRepositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		manifest := filepath.Join(directory, "services", "control-plane", "migrations", "manifest.json")
		if _, err := os.Stat(manifest); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root not found")
		}
		directory = parent
	}
}
