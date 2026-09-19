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

func TestManagedAgentCapabilityEventRowRetainsOnlyOpaqueIdentity(t *testing.T) {
	now := time.Date(2026, time.August, 29, 8, 0, 0, 0, time.UTC)
	row := managedAgentEventRow{
		EventID: "managed-agent-event-capability", Sequence: 2, Operation: "mcp.call", Resource: "McpServer",
		ServerID: "server-alpha", Version: "v1", Digest: "sha256:" + strings.Repeat("d", 64),
		Result: "succeeded",
		Generation: 7, MutationDigest: "sha256:" + strings.Repeat("a", 64), OccurredAt: now,
		Changes: []internalmanagedagent.LifecycleStateChange{{Resource: internalmanagedagent.ResourceMcpServer, To: "succeeded", Version: 1}},
	}
	event := row.snapshot(internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"})
	if event.Resource != internalmanagedagent.ResourceMcpServer || event.ServerID != "server-alpha" || event.BundleID != "" || event.Version != "v1" || event.Digest == "" {
		t.Fatalf("event = %#v", event)
	}
	if !validManagedAgentEventCapability(event) {
		t.Fatalf("capability event was rejected: %#v", event)
	}
}

func TestManagedAgentEventSQLUsesTenantBoundAppendAndRead(t *testing.T) {
	for _, sql := range []string{appendManagedAgentEventSQL, appendManagedAgentCapabilityEventSQL, managedAgentEventSessionExistsSQL, listManagedAgentEventsSQL, managedAgentEventCursorIdentitySQL} {
		if !strings.Contains(sql, "cloud_agents.") || !strings.Contains(sql, "managed_agent") {
			t.Fatalf("event SQL is not schema-qualified: %s", sql)
		}
	}
	if !strings.Contains(appendManagedAgentEventSQL, "append_managed_agent_event_v1") || !strings.Contains(listManagedAgentEventsSQL, "require_tenant_id()") || !strings.Contains(managedAgentEventCursorIdentitySQL, "require_tenant_id()") {
		t.Fatal("event SQL lost the durable append or tenant binding")
	}
	if !strings.Contains(appendManagedAgentCapabilityEventSQL, "append_managed_agent_capability_event_v1") || !strings.Contains(listManagedAgentEventsSQL, "capability_server_uid") {
		t.Fatal("capability event SQL lost the redacted identity projection")
	}
}
