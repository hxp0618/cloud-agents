//go:build linux && evidencefsintegration

package migration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const (
	linuxEvidenceSinkRootEnv    = "CLOUD_AGENTS_EVIDENCEFS_INTEGRATION_ROOT"
	linuxEvidenceSinkRequired   = "CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK"
	linuxEvidenceSinkRevokedEnv = "CLOUD_AGENTS_REQUIRE_MIGRATION_EVIDENCE_SINK_REVOKED"
)

// TestLinuxProductionEvidenceSinkBrandNewAndRegisteredReopen exercises the
// public production composition root against a freshly provisioned local
// ext4/XFS mount. It uses the real opaque evidencefs constructors and never a
// migration-side fake inventory, token, publication, or descriptor seam.
func TestLinuxProductionEvidenceSinkBrandNewAndRegisteredReopen(t *testing.T) {
	if os.Getenv(linuxEvidenceSinkRequired) != "1" {
		t.Skip("production migration evidence sink was not explicitly required")
	}
	rootPath := requireLinuxEvidenceSinkRoot(t)
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	candidate := fixture.candidate
	defer revokeOwnedCurrentCandidate(candidate)
	sink, err := NewEvidenceSink(rootPath)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		session, snapshot, err := sink.Open(context.Background(), candidate.verifiedRun, candidate.runtimeArtifact)
		if err != nil || session == nil || snapshot == nil {
			t.Fatalf("production Open attempt %d: session=%T snapshot=%+v err=%v", attempt, session, snapshot, err)
		}
		if snapshot.State() != RecoveryBrandNew || snapshot.NextAction() != RecoveryBeginFirstAttempt || snapshot.TailDigest().Validate() != nil {
			t.Fatalf("production Open attempt %d returned wrong recovery: state=%s action=%s tail=%s", attempt, snapshot.State(), snapshot.NextAction(), snapshot.TailDigest())
		}
		active := session.ActiveGeneration()
		current := session.CurrentCandidate()
		accessorSnapshot := session.RecoverySnapshot()
		if active.kind != activeGenerationCurrent || active.recoveryExecutionBindings != nil || current.binding != candidate.binding || accessorSnapshot == nil || generationJournalRecoveryDigest(accessorSnapshot) != generationJournalRecoveryDigest(snapshot) {
			t.Fatalf("production Open attempt %d returned mismatched session authority", attempt)
		}
		if err := session.Close(context.Background()); err != nil {
			t.Fatalf("production session close attempt %d: %v", attempt, err)
		}
		if session.RecoverySnapshot() != nil || session.Journal() != nil {
			t.Fatalf("closed production session %d retained authority", attempt)
		}
	}
	t.Logf("MIGRATION_EVIDENCE_SINK_PRODUCTION_OPEN root=%s runner_uid=%d brand_new=PASS registered_reopen=PASS", rootPath, os.Geteuid())
}

func TestLinuxProductionEvidenceSinkOpenFailsAfterRevocation(t *testing.T) {
	if os.Getenv(linuxEvidenceSinkRevokedEnv) != "1" {
		t.Skip("revoked production migration evidence sink was not explicitly required")
	}
	rootPath := requireLinuxEvidenceSinkRoot(t)
	fixture := newRunnerPreparedCurrentSessionFixture(t)
	candidate := fixture.candidate
	defer revokeOwnedCurrentCandidate(candidate)
	sink, err := NewEvidenceSink(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	session, snapshot, err := sink.Open(context.Background(), candidate.verifiedRun, candidate.runtimeArtifact)
	if session != nil || snapshot != nil || !IsCode(err, CodeEvidenceJournalFailed) {
		t.Fatalf("revoked production Open returned a non-closed result: session=%T snapshot=%+v err=%v", session, snapshot, err)
	}
	t.Logf("MIGRATION_EVIDENCE_SINK_REVOKED root=%s runner_uid=%d result=PASS", rootPath, os.Geteuid())
}

func requireLinuxEvidenceSinkRoot(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Fatal("production migration evidence sink integration must run as non-root")
	}
	rootPath := os.Getenv(linuxEvidenceSinkRootEnv)
	if rootPath == "" || !filepath.IsAbs(rootPath) || filepath.Clean(rootPath) != rootPath {
		t.Fatal("integration root must be an absolute canonical path")
	}
	return rootPath
}
