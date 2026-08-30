package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hxp0618/cloud-agents/services/control-plane/internal/authz"
)

func TestDecodeMembershipPageRowsUsesStableTenantCursor(t *testing.T) {
	now := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	memberships := []Membership{
		{UID: "membership-alpha", Name: "membership-alpha", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "user", Issuer: "https://issuer.example", Subject: "user-alpha"}, Scope: authz.ScopeRef{Level: authz.ScopeTenant, ID: "tenant-alpha"}, State: authz.MembershipActive, ResourceVersion: 1, CreatedAt: now, UpdatedAt: now},
		{UID: "membership-beta", Name: "membership-beta", TenantID: "tenant-alpha", Subject: authz.SubjectRef{Kind: "serviceAccount", Issuer: "https://issuer.example", Subject: "agent-beta"}, Scope: authz.ScopeRef{Level: authz.ScopeProject, ID: "project-beta"}, State: authz.MembershipSuspended, ResourceVersion: 2, CreatedAt: now, UpdatedAt: now},
	}
	raw, err := json.Marshal(memberships)
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeMembershipPageRows(raw, "tenant-alpha", 1)
	if err != nil || len(page.Memberships) != 1 || page.Memberships[0].UID != "membership-alpha" || page.NextMembershipID != "membership-alpha" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	memberships[0].TenantID = "tenant-other"
	raw, _ = json.Marshal(memberships[:1])
	if _, err := decodeMembershipPageRows(raw, "tenant-alpha", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-tenant rows error = %v", err)
	}
	for _, fragment := range []string{"cloud_agents.require_tenant_id()", "state <> 'revoked'", "scope_level <> 'platform'", "membership_uid > $1", "ORDER BY membership_uid", "LIMIT $2"} {
		if !strings.Contains(listMembershipsSQL, fragment) {
			t.Fatalf("membership list SQL lost %q: %s", fragment, listMembershipsSQL)
		}
	}
}
