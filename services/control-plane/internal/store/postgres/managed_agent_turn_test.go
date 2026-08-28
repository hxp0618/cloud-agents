package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

func TestScanManagedAgentTurnBuildsDetachedSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	var snapshot internalmanagedagent.TurnSnapshot
	err := scanManagedAgentTurn(rowValues("turn-alpha", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, "queued", int64(1), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, "session-alpha", &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TurnID != "turn-alpha" || snapshot.SessionID != "session-alpha" || snapshot.State != internalmanagedagent.TurnQueued || snapshot.Version != 1 || snapshot.InputDigest == "" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestScanManagedAgentTurnPreservesNotFound(t *testing.T) {
	var snapshot internalmanagedagent.TurnSnapshot
	if err := scanManagedAgentTurn(rowError(pgx.ErrNoRows), internalmanagedagent.Scope{}, "", &snapshot); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

func TestManagedAgentTurnSQLUsesTypedFunctionsAndTenantRLS(t *testing.T) {
	if !strings.Contains(createManagedAgentTurnSQL, "cloud_agents.create_managed_agent_turn_v1(") || !strings.Contains(getManagedAgentTurnSQL, "cloud_agents.require_tenant_id()") {
		t.Fatalf("turn SQL is not tenant-bound typed SQL: create=%s get=%s", createManagedAgentTurnSQL, getManagedAgentTurnSQL)
	}
}

func TestManagedAgentTurnDigestMatchesLifecycleKernel(t *testing.T) {
	input := internalmanagedagent.CreateTurnInput{
		Scope: internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, SessionID: "session-alpha", TurnID: "turn-alpha", InputText: "hello",
		Mutation: internalmanagedagent.Mutation{RequestID: "request-alpha", IdempotencyKey: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4"},
	}
	digest, err := internalmanagedagent.TurnCreateMutationDigest(input)
	if err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest/error = %q/%v", digest, err)
	}
	if _, err := internalmanagedagent.TurnCreateMutationDigest(internalmanagedagent.CreateTurnInput{}); err == nil {
		t.Fatal("accepted invalid turn digest input")
	}
}
