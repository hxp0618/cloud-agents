package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeStoragePolicyAuditRowsBindsPolicyAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)
	row := storagePolicyAuditRow{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", PolicyID: "storage-alpha",
		EventID: "event-storage-alpha", OperationID: "operation-storage-alpha",
		Actor: "sha256:" + strings.Repeat("a", 64), Action: "storage-policy.set",
		PolicyResourceVersion: 1, Result: "succeeded", RequestID: "request-storage-alpha", OccurredAt: now,
	}
	raw, err := json.Marshal([]storagePolicyAuditRow{row, row})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeStoragePolicyAuditRows(raw, "tenant-alpha", "project-alpha", "storage-alpha", 1)
	if err != nil || len(page.Events) != 1 || page.NextEventID != row.EventID {
		t.Fatalf("page=%#v error=%v", page, err)
	}
	if _, err := decodeStoragePolicyAuditRows(raw, "tenant-alpha", "project-alpha", "storage-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-policy audit error=%v", err)
	}
	if !strings.Contains(storagePolicyAuditPolicyIdentitySQL, "policy_uid = $2") {
		t.Fatal("Storage Policy audit authority is not bound to policy identity")
	}
}
