package postgres

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	internalcoordination "github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimeProfileProjectionAndPublicRedaction(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	row := runtimeProfilePageRow{
		TenantID: "tenant-alpha", ProjectID: "project-alpha", ProfileVersionID: "rp-0123456789abcdef0123456789abcdef",
		ProfileID: "standard", ProfileName: "standard", Version: 1, Description: "No-agent workspace",
		Status: "published", TargetID: "docker-primary", ImageURI: "node@" + digest, ReleaseDigest: digest,
		CPUMillis: 1000, MemoryBytes: 536870912, ResourceVersion: 2, CreatedAt: now, UpdatedAt: now, PublishedAt: &now,
	}
	raw, err := json.Marshal([]runtimeProfilePageRow{row, row})
	if err != nil {
		t.Fatal(err)
	}
	page, err := decodeRuntimeProfilePageRows(raw, "tenant-alpha", "project-alpha", 1)
	if err != nil || len(page.Profiles) != 1 || page.NextProfileVersionID != row.ProfileVersionID {
		t.Fatalf("page = %#v / %v", page, err)
	}
	if _, err := decodeRuntimeProfilePageRows(raw, "tenant-alpha", "project-other", 1); !errors.Is(err, ErrCoordinationResultDrift) {
		t.Fatalf("cross-project page error = %v", err)
	}

	publicRaw, err := json.Marshal([]publishedRuntimeProfilePageRow{{
		TenantID: row.TenantID, ProjectID: row.ProjectID, ProfileVersionID: row.ProfileVersionID,
		ProfileID: row.ProfileID, ProfileName: row.ProfileName, Version: row.Version,
		Description: row.Description, CPUMillis: row.CPUMillis, MemoryBytes: row.MemoryBytes,
	}})
	if err != nil {
		t.Fatal(err)
	}
	public, err := decodePublishedRuntimeProfilePageRows(publicRaw, row.TenantID, row.ProjectID, 1)
	if err != nil || len(public.Profiles) != 1 {
		t.Fatalf("public page = %#v / %v", public, err)
	}
	projection := strings.Split(strings.Split(listPublishedRuntimeProfilesSQL, "SELECT profile.tenant_id")[1], "FROM cloud_agents.runtime_profiles")[0]
	for _, forbidden := range []string{"target_uid", "image_uri", "release_digest", "credential", "endpoint"} {
		if strings.Contains(projection, forbidden) {
			t.Fatalf("public profile query projects %q", forbidden)
		}
	}
	for _, authority := range []string{"cloud_agents.require_tenant_id()", "profile.status = 'published'", "target.observed_phase = 'ready'", "target.scheduling_state = 'active'"} {
		if !strings.Contains(listPublishedRuntimeProfilesSQL, authority) || !strings.Contains(publishedRuntimeProfilePageCursorSQL, authority) {
			t.Fatalf("public profile authority is missing %q", authority)
		}
	}
}

func TestRuntimeProfileScanAndConflictMapping(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)
	digest := "sha256:" + strings.Repeat("a", 64)
	var snapshot internalcoordination.RuntimeProfileSnapshot
	if err := scanRuntimeProfile(rowValues(
		"rp-0123456789abcdef0123456789abcdef", "standard", "standard", int64(1), "No-agent workspace",
		"draft", "docker-primary", "node@"+digest, digest, int64(1000), int64(536870912), int64(1),
		now, now, (*time.Time)(nil), (*time.Time)(nil),
	), internalcoordination.FoundationScope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, &snapshot); err != nil {
		t.Fatal(err)
	}
	for input, want := range map[*pgconn.PgError]error{
		{Code: "23503", Message: "runtime profile was not found"}:       internalcoordination.ErrRuntimeProfileNotFound,
		{Code: "23503", Message: "runtime profile target unavailable"}:  internalcoordination.ErrRuntimeProfileUnavailable,
		{Code: "23505", Message: "runtime profile version conflict"}:    internalcoordination.ErrRuntimeProfileConflict,
		{Code: "23505", Message: "foundation sandbox profile conflict"}: internalcoordination.ErrFoundationSandboxConflict,
	} {
		if err := mapRuntimeProfileError(input); !errors.Is(err, want) {
			t.Fatalf("%s mapped to %v", input.Message, err)
		}
	}
	if !strings.Contains(createRuntimeProfileSQL, "create_runtime_profile_draft_v1") ||
		!strings.Contains(createFoundationSandboxSQL, "accept_foundation_sandbox_v1") {
		t.Fatal("runtime profile store is not bound to the migration authority")
	}
}
