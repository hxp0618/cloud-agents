package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/migration"
)

func TestRunRejectsIncompleteOrUnknownConfiguration(t *testing.T) {
	t.Setenv(databaseURLEnvironment, "postgres://runner:secret@127.0.0.1:1/control_plane")
	complete := []string{
		"--artifact", "/srv/cloud-agents/migrations.tar",
		"--repository", "hxp0618/cloud-agents",
		"--release", "v0.1.0-alpha.1",
		"--evidence-root", "/srv/cloud-agents/evidence",
	}
	tests := [][]string{
		nil,
		complete[:2],
		complete[:4],
		complete[:6],
		append(append([]string(nil), complete...), "extra"),
		append(append([]string(nil), complete...), "--unknown"),
	}
	for _, args := range tests {
		if err := run(context.Background(), args); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("args=%q returned unsafe or nil error: %v", args, err)
		}
	}

	t.Setenv(databaseURLEnvironment, "")
	if err := run(context.Background(), complete); err == nil {
		t.Fatal("missing database locator was accepted")
	}
}

func TestRunConfiguresEvidenceAndRejectsBeforeExternalIO(t *testing.T) {
	t.Setenv(databaseURLEnvironment, "postgres://runner:secret@127.0.0.1:1/control_plane?connect_timeout=1")
	directory := t.TempDir()
	artifactPath := filepath.Join(directory, "missing-runtime.tar")
	evidenceRoot := filepath.Join(directory, "missing-evidence-root")
	err := run(context.Background(), []string{
		"--artifact", artifactPath,
		"--repository", "hxp0618/cloud-agents",
		"--release", "v0.1.0-alpha.1",
		"--evidence-root", evidenceRoot,
	})
	if !migration.IsCode(err, migration.CodeUntrusted) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("configured fail-closed runner returned %v", err)
	}
	if _, statErr := os.Lstat(artifactPath); !os.IsNotExist(statErr) {
		t.Fatalf("artifact path changed before trust rejection: %v", statErr)
	}
	if _, statErr := os.Lstat(evidenceRoot); !os.IsNotExist(statErr) {
		t.Fatalf("evidence root changed before trust rejection: %v", statErr)
	}
}

func TestRunRejectsInvalidEvidenceRootWithoutFilesystemAccess(t *testing.T) {
	t.Setenv(databaseURLEnvironment, "postgres://runner:secret@127.0.0.1:1/control_plane")
	err := run(context.Background(), []string{
		"--artifact", "/srv/cloud-agents/migrations.tar",
		"--repository", "hxp0618/cloud-agents",
		"--release", "v0.1.0-alpha.1",
		"--evidence-root", "relative/evidence",
	})
	if !migration.IsCode(err, migration.CodeEvidenceJournalFailed) || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid evidence locator returned %v", err)
	}
}
