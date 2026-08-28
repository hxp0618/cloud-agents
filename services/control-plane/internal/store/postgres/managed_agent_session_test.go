package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

func TestScanManagedAgentSessionBuildsDetachedSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	var snapshot internalmanagedagent.SessionSnapshot
	err := scanManagedAgentSession(rowValues("session-alpha", "codex", "active", int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SessionID != "session-alpha" || snapshot.ProviderKind != "codex" || snapshot.State != internalmanagedagent.SessionActive || snapshot.Version != 2 || snapshot.Scope.ProjectID != "project-alpha" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestScanManagedAgentSessionPreservesNotFound(t *testing.T) {
	var snapshot internalmanagedagent.SessionSnapshot
	if err := scanManagedAgentSession(rowError(pgx.ErrNoRows), internalmanagedagent.Scope{}, &snapshot); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

func TestManagedAgentSessionSQLUsesTypedFunctionsAndTenantRLS(t *testing.T) {
	for _, sql := range []string{createManagedAgentSessionSQL, closeManagedAgentSessionSQL} {
		if !strings.Contains(sql, "cloud_agents.") || !strings.Contains(sql, "_v1(") {
			t.Fatalf("unqualified or unversioned session mutation SQL: %s", sql)
		}
	}
	if !strings.Contains(getManagedAgentSessionSQL, "cloud_agents.require_tenant_id()") {
		t.Fatal("session read does not bind the tenant context")
	}
}

func TestManagedAgentSessionDigestMatchesLifecycleKernel(t *testing.T) {
	input := internalmanagedagent.CreateSessionInput{
		Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", ProviderKind: "codex",
		Mutation: internalmanagedagent.Mutation{RequestID: "request-alpha", IdempotencyKey: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2"},
	}
	digest, err := internalmanagedagent.SessionCreateMutationDigest(input)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest/error = %q/%v", digest, err)
	}
	if _, err := internalmanagedagent.SessionCreateMutationDigest(internalmanagedagent.CreateSessionInput{}); err == nil {
		t.Fatal("accepted invalid session digest input")
	}
}
