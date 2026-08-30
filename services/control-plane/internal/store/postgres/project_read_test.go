package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestScanProjectBindsTenantAuthority(t *testing.T) {
	now := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	var project Project
	if err := scanProject(rowValues("project-alpha", "project-name", "organization-alpha", "Project Alpha", "active", int64(5), now, now), "tenant-alpha", &project); err != nil {
		t.Fatal(err)
	}
	if project.UID != "project-alpha" || project.TenantID != "tenant-alpha" || project.OrganizationID != "organization-alpha" || project.ResourceVersion != 5 {
		t.Fatalf("project = %#v", project)
	}
}

func TestDecodeProjectPageRowsUsesStableOrganizationCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC)
	rows := []Project{
		{UID: "project-alpha", Name: "project-alpha", TenantID: "tenant-alpha", OrganizationID: "organization-alpha", DisplayName: "Project Alpha", State: "active", ResourceVersion: 2, CreatedAt: now, UpdatedAt: now},
		{UID: "project-beta", Name: "project-beta", TenantID: "tenant-alpha", OrganizationID: "organization-alpha", DisplayName: "Project Beta", State: "suspended", ResourceVersion: 3, CreatedAt: now, UpdatedAt: now},
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeProjectPageRows(raw, "tenant-alpha", "organization-alpha", 1)
	if err != nil || len(page.Projects) != 1 || page.Projects[0].UID != "project-alpha" || page.NextProjectUID != "project-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	rows[0].OrganizationID = "organization-other"
	raw, _ = json.Marshal(rows[:1])
	if _, err := decodeProjectPageRows(raw, "tenant-alpha", "organization-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-organization rows error = %v", err)
	}
	for _, sql := range []string{projectPageCursorIdentitySQL, listProjectsSQL} {
		if !strings.Contains(sql, "cloud_agents.require_tenant_id()") || !strings.Contains(sql, "organization_uid") {
			t.Fatalf("project list SQL lost tenant or organization binding: %s", sql)
		}
	}
}
