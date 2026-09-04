package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalprojectleasequota "github.com/hxp0618/cloud-agents/services/control-plane/internal/projectleasequota"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProjectLeaseQuotaProjectionAndConflictMapping(t *testing.T) {
	now := time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)
	var snapshot internalprojectleasequota.Snapshot
	if err := scanProjectLeaseQuota(
		rowValues("quota-project-alpha", "project-lease-quota", int64(2), int64(4000), int64(8589934592), int64(3600), int64(1), int64(2000), int64(4294967296), int64(3), now, now),
		internalprojectleasequota.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		&snapshot,
	); err != nil || snapshot.ActiveLeases != 1 {
		t.Fatalf("snapshot=%#v error=%v", snapshot, err)
	}
	for message, expected := range map[string]error{
		"project lease quota idempotency conflict":      ErrProjectLeaseQuotaIdempotencyConflict,
		"project lease quota resource version conflict": ErrProjectLeaseQuotaResourceVersionConflict,
	} {
		if err := mapProjectLeaseQuotaError(&pgconn.PgError{Code: "23505", Message: message}); !errors.Is(err, expected) {
			t.Fatalf("%s mapped to %v", message, err)
		}
	}
	if !strings.Contains(setProjectLeaseQuotaSQL, "set_project_lease_quota_v1") ||
		!strings.Contains(getProjectLeaseQuotaSQL, "cloud_agents.require_tenant_id()") {
		t.Fatal("project Lease quota store is not bound to migration and tenant authority")
	}
}

func TestDecodeProjectLeaseQuotaAuditRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 5, 1, 0, 0, 0, time.UTC)
	row := func(eventID string) projectLeaseQuotaAuditRow {
		return projectLeaseQuotaAuditRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", QuotaID: "quota-project-alpha",
			EventID: eventID, OperationID: "operation-quota-alpha", Actor: "sha256:" + strings.Repeat("a", 64),
			Action: "quota.set", QuotaResourceVersion: 1, Result: "succeeded",
			RequestID: "request-quota-alpha", OccurredAt: now,
		}
	}
	raw, err := json.Marshal([]projectLeaseQuotaAuditRow{row("event-quota-alpha"), row("event-quota-beta")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeProjectLeaseQuotaAuditRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.Events) != 1 || page.NextEventID != "event-quota-alpha" {
		t.Fatalf("page=%#v error=%v", page, err)
	}
	if _, err := decodeProjectLeaseQuotaAuditRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project audit error=%v", err)
	}
}
