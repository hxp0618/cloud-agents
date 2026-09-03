package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalenvironmentprofile "github.com/hxp0618/cloud-agents/services/control-plane/internal/environmentprofile"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDecodeEnvironmentProfileRowsBindsProjectAndCursor(t *testing.T) {
	now := time.Date(2026, time.September, 3, 13, 0, 0, 0, time.UTC)
	row := func(uid, profileID string) environmentProfilePageRow {
		return environmentProfilePageRow{
			TenantID: "tenant-alpha", ProjectID: "project-alpha", ProfileVersionUID: uid,
			ProfileID: profileID, ProfileName: profileID, Version: 1, Description: "Standard workspace",
			Status: "draft", ProviderKinds: []string{"codex", "claudeAgent"},
			CPULimitMillis: 2000, MemoryLimitBytes: 4294967296,
			StoragePolicyRef: "workspace-8gb", NetworkPolicyRef: "public-egress",
			ReleaseDigest: "sha256:" + strings.Repeat("a", 64), TargetRefs: []string{"docker-primary"},
			ProviderCredentialRef: "provider-primary", ResourceVersion: 1, CreatedAt: now, UpdatedAt: now,
		}
	}
	raw, err := json.Marshal([]environmentProfilePageRow{
		row("ep-0123456789abcdef0123456789abcdef", "standard"),
		row("ep-1123456789abcdef0123456789abcdef", "large"),
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeEnvironmentProfilePageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.EnvironmentProfiles) != 1 || page.NextProfileVersionID != "ep-0123456789abcdef0123456789abcdef" {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeEnvironmentProfilePageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error = %v", err)
	}
}

func TestEnvironmentProfileProjectionAndConflictMapping(t *testing.T) {
	now := time.Date(2026, time.September, 3, 13, 0, 0, 0, time.UTC)
	row := rowValues(
		"ep-0123456789abcdef0123456789abcdef", "standard", "standard", int64(1),
		"Standard workspace", "draft", []string{"codex"}, int64(2000), int64(4294967296),
		"workspace-8gb", "public-egress", "sha256:"+strings.Repeat("a", 64),
		[]string{"docker-primary"}, "provider-primary", int64(1), now, now, (*time.Time)(nil), (*time.Time)(nil),
	)
	var snapshot internalenvironmentprofile.Snapshot
	if err := scanEnvironmentProfile(row, internalenvironmentprofile.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot); err != nil {
		t.Fatal(err)
	}
	for message, expected := range map[string]error{
		"environment profile idempotency conflict": ErrEnvironmentProfileIdempotencyConflict,
		"environment profile version conflict":     ErrEnvironmentProfileVersionConflict,
		"environment profile name conflict":        ErrEnvironmentProfileVersionConflict,
		"environment profile transition conflict":  ErrEnvironmentProfileTransitionConflict,
	} {
		if err := mapEnvironmentProfileError(&pgconn.PgError{Code: "23505", Message: message}); !errors.Is(err, expected) {
			t.Fatalf("%s mapped to %v", message, err)
		}
	}
	if err := mapEnvironmentProfileError(&pgconn.PgError{Code: "23503", Message: "environment profile was not found"}); !errors.Is(err, ErrEnvironmentProfileNotFound) {
		t.Fatalf("missing profile mapped to %v", err)
	}
	if !strings.Contains(createEnvironmentProfileSQL, "create_environment_profile_draft_v1") ||
		!strings.Contains(transitionEnvironmentProfileSQL, "transition_environment_profile_v1") ||
		!strings.Contains(listEnvironmentProfilesSQL, "cloud_agents.require_tenant_id()") {
		t.Fatal("environment profile store is not bound to migration and tenant authority")
	}
}

func TestDecodeEnvironmentProfileAuditRowsBindsVersion(t *testing.T) {
	now := time.Date(2026, time.September, 3, 13, 0, 0, 0, time.UTC)
	uid := "ep-0123456789abcdef0123456789abcdef"
	raw, err := json.Marshal([]environmentProfileAuditPageRow{{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", ProfileVersionUID: uid,
		EventID: "operation-alpha-succeeded", OperationID: "operation-alpha",
		Actor: "sha256:" + strings.Repeat("b", 64), Action: "profile.create", ProfileVersion: 1,
		Result: "succeeded", RequestID: "request-alpha", OccurredAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeEnvironmentProfileAuditRows(raw, "tenant-alpha", "project-alpha", uid, 1)
	if err != nil || len(page.Events) != 1 || page.Events[0].ProfileUID != uid {
		t.Fatalf("audit page = %#v / %v", page, err)
	}
}
