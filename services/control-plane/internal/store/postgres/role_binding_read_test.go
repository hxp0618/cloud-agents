package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

func TestDecodeRoleBindingPageRowsUsesStableTenantCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	roleBindings := []RoleBinding{
		{UID: "binding-alpha", Name: "binding-alpha", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, RoleName: "tenant.admin", RoleVersion: 1, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}, State: authz.BindingActive, ResourceVersion: 1, CreatedAt: now, UpdatedAt: now},
		{UID: "binding-beta", Name: "binding-beta", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "agent-beta"}, RoleName: "project.viewer", RoleVersion: 1, Scope: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-beta"}, State: authz.BindingActive, ResourceVersion: 2, CreatedAt: now, UpdatedAt: now},
	}
	raw, err := json.Marshal(roleBindings)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeRoleBindingPageRows(raw, "tenant-alpha", 1)
	if err != nil || len(page.RoleBindings) != 1 || page.RoleBindings[0].UID != "binding-alpha" || page.NextRoleBindingID != "binding-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	roleBindings[0].TenantID = "tenant-other"
	raw, _ = json.Marshal(roleBindings[:1])
	if _, err := decodeRoleBindingPageRows(raw, "tenant-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-tenant rows error = %v", err)
	}
	for _, fragment := range []string{"cloud_agents.require_tenant_id()", "state <> 'revoked'", "scope_level <> 'platform'", "role_binding_uid > $1", "ORDER BY role_binding_uid", "LIMIT $2"} {
		if !strings.Contains(listRoleBindingsSQL, fragment) {
			t.Fatalf("role binding list SQL lost %q: %s", fragment, listRoleBindingsSQL)
		}
	}
}
