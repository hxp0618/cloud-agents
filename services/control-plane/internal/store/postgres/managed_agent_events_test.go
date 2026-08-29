package postgres

import (
	"strings"
	"testing"
	"time"

	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
)

func TestManagedAgentEventRowMapsDurableResourceNames(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	row := managedAgentEventRow{
		EventID: "managed-agent-event-1", Sequence: 1, Operation: "execution.start", Resource: "Execution",
		TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 1, MutationDigest: "sha256:" + strings.Repeat("a", 64), OccurredAt: now,
		Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceKind("Turn"), From: "queued", To: "running", Version: 2}},
	}
	event := row.snapshot(internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"})
	if event.Resource != internalmanagedagent.ResourceExecution || len(event.Changes) != 1 || event.Changes[0].Resource != internalmanagedagent.ResourceTurn {
		t.Fatalf("event = %#v", event)
	}
}

func TestManagedAgentEventSQLUsesTenantBoundAppendAndRead(t *testing.T) {
	for _, sql := range []string{appendManagedAgentEventSQL, managedAgentEventSessionExistsSQL, listManagedAgentEventsSQL, managedAgentEventCursorIdentitySQL} {
		if !strings.Contains(sql, "cloud_agents.") || !strings.Contains(sql, "managed_agent") {
			t.Fatalf("event SQL is not schema-qualified: %s", sql)
		}
	}
	if !strings.Contains(appendManagedAgentEventSQL, "append_managed_agent_event_v1") || !strings.Contains(listManagedAgentEventsSQL, "require_tenant_id()") || !strings.Contains(managedAgentEventCursorIdentitySQL, "require_tenant_id()") {
		t.Fatal("event SQL lost the durable append or tenant binding")
	}
}
