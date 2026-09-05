package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeNetworkPolicyAuditRowsBindsPolicyAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)
	row := networkPolicyAuditRow{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", PolicyID: "network-alpha",
		EventID: "event-network-alpha", OperationID: "operation-network-alpha",
		Actor: "sha256:" + strings.Repeat("a", 64), Action: "network-policy.set",
		PolicyResourceVersion: 1, Result: "succeeded", RequestID: "request-network-alpha", OccurredAt: now,
	}
	raw, err := json.Marshal([]networkPolicyAuditRow{row, row})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeNetworkPolicyAuditRows(raw, "tenant-alpha", "project-alpha", "network-alpha", 1)
	if err != nil || len(page.Events) != 1 || page.NextEventID != row.EventID {
		t.Fatalf("page=%#v error=%v", page, err)
	}
	if _, err := decodeNetworkPolicyAuditRows(raw, "tenant-alpha", "project-alpha", "network-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-policy audit error=%v", err)
	}
	if !strings.Contains(networkPolicyAuditPolicyIdentitySQL, "policy_uid = $2") {
		t.Fatal("Network Policy audit authority is not bound to policy identity")
	}
}
