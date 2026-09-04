package postgres

import (
	"encoding/json"
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
	err := scanManagedAgentSession(rowValues("session-alpha", "codex", nil, nil, nil, nil, "active", int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SessionID != "session-alpha" || snapshot.ProviderKind != "codex" || snapshot.State != internalmanagedagent.SessionActive || snapshot.Version != 2 || snapshot.Scope.ProjectID != "project-alpha" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	leaseID, generation := "lease-alpha", int64(7)
	profileID, profileVersion := "profile-alpha", int64(3)
	err = scanManagedAgentSession(rowValues("session-alpha", "codex", &leaseID, &generation, &profileID, &profileVersion, "active", int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot)
	if err != nil || snapshot.EnvironmentLeaseID != leaseID || snapshot.EnvironmentGeneration != 7 {
		t.Fatalf("bound snapshot = %#v / %v", snapshot, err)
	}
	if snapshot.EnvironmentProfileID != profileID || snapshot.EnvironmentProfileVersion != 3 {
		t.Fatalf("profile binding = %#v", snapshot)
	}
	if err := scanManagedAgentSession(rowValues("session-alpha", "codex", &leaseID, nil, nil, nil, "active", int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("partial environment binding error = %v", err)
	}
	if err := scanManagedAgentSession(rowValues("session-alpha", "codex", &leaseID, &generation, &profileID, nil, "active", int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("partial profile binding error = %v", err)
	}
}

func TestScanManagedAgentSessionPreservesNotFound(t *testing.T) {
	var snapshot internalmanagedagent.SessionSnapshot
	if err := scanManagedAgentSession(rowError(pgx.ErrNoRows), internalmanagedagent.Scope{}, &snapshot); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

func TestDecodeManagedAgentSessionPageRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	raw, err := json.Marshal([]managedAgentSessionPageRow{
		{TenantID: "tenant-alpha", ProjectID: "project-alpha", SessionID: "session-alpha", ProviderKind: "codex", State: "active", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now},
		{TenantID: "tenant-alpha", ProjectID: "project-alpha", SessionID: "session-beta", ProviderKind: "claude", State: "closed", ResourceVersion: 2, CreatedAt: now, UpdatedAt: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeManagedAgentSessionPageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.Sessions) != 1 || page.Sessions[0].SessionID != "session-alpha" || page.Sessions[0].Scope.ProjectID != "project-alpha" || page.NextSessionID != "session-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeManagedAgentSessionPageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error = %v", err)
	}
}

func TestManagedAgentSessionSQLUsesTypedFunctionsAndTenantRLS(t *testing.T) {
	for _, sql := range []string{createManagedAgentSessionSQL, closeManagedAgentSessionSQL} {
		if !strings.Contains(sql, "cloud_agents.") || !strings.Contains(sql, "_v") {
			t.Fatalf("unqualified or unversioned session mutation SQL: %s", sql)
		}
	}
	if !strings.Contains(getManagedAgentSessionSQL, "cloud_agents.require_tenant_id()") {
		t.Fatal("session read does not bind the tenant context")
	}
	if !strings.Contains(listManagedAgentSessionsSQL, "cloud_agents.require_tenant_id()") || !strings.Contains(listManagedAgentSessionsSQL, "project_uid = $1") || !strings.Contains(managedAgentSessionPageCursorIdentitySQL, "session_uid = $2") {
		t.Fatal("session list does not bind tenant, project, and cursor identity")
	}
	if !strings.Contains(getManagedAgentSessionForExecutionSQL, "provider_resume_cursor") {
		t.Fatal("execution session read omits Provider continuation state")
	}
}

func TestManagedAgentSessionDigestMatchesLifecycleKernel(t *testing.T) {
	input := internalmanagedagent.CreateSessionInput{
		Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", ProviderKind: "codex", EnvironmentLeaseID: "lease-alpha",
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
