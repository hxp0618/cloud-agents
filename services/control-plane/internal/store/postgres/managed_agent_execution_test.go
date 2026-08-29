package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	internalmanagedagent "github.com/hxp0618/cloud-agents/services/control-plane/internal/managedagent"
	"github.com/jackc/pgx/v5"
)

func TestScanManagedAgentExecutionBuildsDetachedSnapshot(t *testing.T) {
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	resultDigest := "sha256:" + strings.Repeat("a", 64)
	var snapshot internalmanagedagent.ExecutionSnapshot
	err := scanManagedAgentExecution(rowValues("execution-alpha", int64(7), "succeeded", &resultDigest, nil, int64(3), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, "session-alpha", "turn-alpha", &snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ExecutionID != "execution-alpha" || snapshot.Generation != 7 || snapshot.State != internalmanagedagent.ExecutionSucceeded || snapshot.ResultDigest == "" || snapshot.Version != 3 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestScanManagedAgentExecutionTransitionPreservesNullableTerminalFields(t *testing.T) {
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	var result internalmanagedagent.ExecutionTransitionResult
	err := scanManagedAgentExecutionTransition(rowValues("turn-alpha", "running", int64(4), now, now, "execution-alpha", int64(7), "running", nil, nil, int64(2), now, now), internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, "session-alpha", &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Turn.State != internalmanagedagent.TurnRunning || result.Execution.State != internalmanagedagent.ExecutionRunning || result.Execution.ResultDigest != "" || result.Execution.ErrorCode != "" {
		t.Fatalf("transition = %#v", result)
	}
}

func TestScanManagedAgentExecutionPreservesNotFound(t *testing.T) {
	var snapshot internalmanagedagent.ExecutionSnapshot
	if err := scanManagedAgentExecution(rowError(pgx.ErrNoRows), internalmanagedagent.Scope{}, "", "", &snapshot); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

func TestManagedAgentExecutionSQLUsesTypedFunctionsAndTenantRLS(t *testing.T) {
	for name, sql := range map[string]string{
		"create": createManagedAgentExecutionSQL,
		"start":  startManagedAgentExecutionSQL,
		"settle": settleManagedAgentExecutionSQL,
		"cancel": cancelManagedAgentExecutionSQL,
		"get":    getManagedAgentExecutionSQL,
	} {
		if !strings.Contains(sql, "managed_agent") || (name == "get" && !strings.Contains(sql, "cloud_agents.require_tenant_id()")) {
			t.Fatalf("%s SQL is not tenant-bound execution SQL: %s", name, sql)
		}
	}
}

func TestManagedAgentExecutionDigestsMatchLifecycleKernel(t *testing.T) {
	scope := internalmanagedagent.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}
	mutation := internalmanagedagent.Mutation{RequestID: "request-alpha", IdempotencyKey: "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4"}
	created, err := internalmanagedagent.ExecutionCreateMutationDigest(internalmanagedagent.CreateExecutionInput{Scope: scope, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 7, Mutation: mutation})
	if err != nil || !strings.HasPrefix(created, "sha256:") {
		t.Fatalf("create digest/error = %q/%v", created, err)
	}
	completed, err := internalmanagedagent.ExecutionCompleteMutationDigest(internalmanagedagent.CompleteExecutionInput{Scope: scope, SessionID: "session-alpha", TurnID: "turn-alpha", ExecutionID: "execution-alpha", Generation: 7, ResultDigest: "sha256:" + strings.Repeat("b", 64), Mutation: mutation})
	if err != nil || !strings.HasPrefix(completed, "sha256:") || completed == created {
		t.Fatalf("complete digest/error = %q/%v", completed, err)
	}
	cancelled, err := internalmanagedagent.TurnCancelMutationDigest(internalmanagedagent.CancelTurnInput{Scope: scope, SessionID: "session-alpha", TurnID: "turn-alpha", TargetExecutionID: "execution-alpha", Generation: 7, Mutation: mutation})
	if err != nil || !strings.HasPrefix(cancelled, "sha256:") || cancelled == completed {
		t.Fatalf("cancel digest/error = %q/%v", cancelled, err)
	}
	interrupted, err := internalmanagedagent.TurnInterruptMutationDigest(internalmanagedagent.InterruptTurnInput{Scope: scope, SessionID: "session-alpha", TurnID: "turn-alpha", TargetExecutionID: "execution-alpha", Generation: 7, Mutation: mutation})
	if err != nil || !strings.HasPrefix(interrupted, "sha256:") || interrupted == cancelled {
		t.Fatalf("interrupt digest/error = %q/%v", interrupted, err)
	}
}
