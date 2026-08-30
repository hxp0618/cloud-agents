package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeRolePageRowsUsesStableCatalogCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	roles := []Role{
		{UID: "role-project-admin-v1", Name: "role-project-admin-v1", TenantID: "tenant-alpha", RoleName: "project.admin", Version: 1, Permissions: []string{"projects.get", "projects.list"}, State: "active", ResourceVersion: 1, CreatedAt: now},
		{UID: "role-project-viewer-v1", Name: "role-project-viewer-v1", TenantID: "tenant-alpha", RoleName: "project.viewer", Version: 1, Permissions: []string{"projects.get", "projects.list"}, State: "active", ResourceVersion: 1, CreatedAt: now},
	}
	raw, err := json.Marshal(roles)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeRolePageRows(raw, "tenant-alpha", 1)
	if err != nil || len(page.Roles) != 1 || page.Roles[0].RoleName != "project.admin" || page.NextRoleName != "project.admin" || page.NextRoleVersion != 1 {
		t.Fatalf("page = %#v / %v", page, err)
	}
	roles[0].TenantID = "tenant-other"
	raw, _ = json.Marshal(roles[:1])
	if _, err := decodeRolePageRows(raw, "tenant-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-tenant rows error = %v", err)
	}
	for _, fragment := range []string{"cloud_agents.require_tenant_id()", "role.role_name > $1", "role.role_version > $2", "ORDER BY role.role_name, role.role_version", "LIMIT $3"} {
		if !strings.Contains(listRolesSQL, fragment) {
			t.Fatalf("role list SQL lost %q: %s", fragment, listRolesSQL)
		}
	}
}
