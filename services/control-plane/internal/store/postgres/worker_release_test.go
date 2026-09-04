package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalworkerrelease "github.com/hxp0618/cloud-agents/services/control-plane/internal/workerrelease"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWorkerReleaseProjectionPaginationAndConflicts(t *testing.T) {
	now := time.Date(2026, time.September, 4, 7, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	row := func(id string) workerReleasePageRow {
		return workerReleasePageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", ReleaseID: id, ReleaseName: id,
			ImageRepository: "registry.example.test/cloud-agents/worker", ReleaseDigest: digest,
			PlatformVersion: "platform-v1", RuntimeVersion: "runtime-v1", CodexVersion: "codex-v1", ClaudeCodeVersion: "claude-v1",
			Architectures: []string{"linux/arm64"}, Status: "approved", VerificationState: "attested",
			VerificationEvidenceDigest: digest, ResourceVersion: 1, CreatedAt: now, UpdatedAt: now, ApprovedAt: now,
		}
	}
	raw, err := json.Marshal([]workerReleasePageRow{row("release-alpha"), row("release-beta")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeWorkerReleasePageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.WorkerReleases) != 1 || page.NextReleaseID != "release-alpha" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	if _, err := decodeWorkerReleasePageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error=%v", err)
	}

	var snapshot internalworkerrelease.Snapshot
	projection := rowValues("release-alpha", "release-alpha", "registry.example.test/cloud-agents/worker", digest,
		"platform-v1", "runtime-v1", "codex-v1", "claude-v1", []string{"linux/arm64"},
		"approved", "attested", digest, int64(1), now, now, now)
	if err := scanWorkerRelease(projection, internalworkerrelease.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot); err != nil {
		t.Fatal(err)
	}
	for message, expected := range map[string]error{
		"worker release idempotency conflict": ErrWorkerReleaseIdempotencyConflict,
		"worker release conflict":             ErrWorkerReleaseConflict,
	} {
		if err := mapWorkerReleaseError(&pgconn.PgError{Code: "23505", Message: message}); !errors.Is(err, expected) {
			t.Fatalf("%s mapped to %v", message, err)
		}
	}
	if !strings.Contains(registerWorkerReleaseSQL, "register_worker_release_v1") ||
		!strings.Contains(listWorkerReleasesSQL, "cloud_agents.require_tenant_id()") ||
		!strings.Contains(workerReleasePageCursorIdentitySQL, "release_uid = $2") {
		t.Fatal("worker release store is not bound to migration and tenant authority")
	}
}
