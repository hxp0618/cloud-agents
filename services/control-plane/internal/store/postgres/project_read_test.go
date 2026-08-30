package postgres

import (
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
