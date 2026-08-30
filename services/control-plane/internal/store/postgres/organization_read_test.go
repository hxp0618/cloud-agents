package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeOrganizationPageRowsUsesStableLastReturnedCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	rows := []Organization{
		{UID: "organization-alpha", Name: "organization-alpha", TenantID: "tenant-alpha", DisplayName: "Organization Alpha", State: "active", ResourceVersion: 2, CreatedAt: now, UpdatedAt: now},
		{UID: "organization-beta", Name: "organization-beta", TenantID: "tenant-alpha", DisplayName: "Organization Beta", State: "suspended", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now},
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeOrganizationPageRows(raw, "tenant-alpha", 1)
	if err != nil || len(page.Organizations) != 1 || page.Organizations[0].UID != "organization-alpha" || page.NextOrganizationUID != "organization-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	rows[0].TenantID = "tenant-other"
	raw, _ = json.Marshal(rows[:1])
	if _, err := decodeOrganizationPageRows(raw, "tenant-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-tenant rows error = %v", err)
	}
	for _, sql := range []string{organizationPageCursorIdentitySQL, listOrganizationsSQL} {
		if !strings.Contains(sql, "cloud_agents.require_tenant_id()") || !strings.Contains(sql, "cloud_agents.organizations") {
			t.Fatalf("organization list SQL lost tenant binding: %s", sql)
		}
	}
}
