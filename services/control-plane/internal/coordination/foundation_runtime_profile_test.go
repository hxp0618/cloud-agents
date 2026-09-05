package coordination

import (
	"strings"
	"testing"
	"time"
)

func TestRuntimeProfileAndSandboxInputsBindEveryPublicField(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := RuntimeProfileCreateInput{
		Scope: FoundationScope{"tenant", "project"}, ProfileID: "standard", ProfileName: "standard",
		Version: 1, Description: "No-agent Docker workspace", TargetID: "docker-primary",
		ImageURI: "ghcr.io/opensandbox/server@" + digest, ReleaseDigest: digest,
		CPUMillis: 1000, MemoryBytes: 536870912,
		Mutation: FoundationMutation{"request-profile", "runtime-profile-key-0001"},
	}
	profileDigest, err := RuntimeProfileCreateDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile.Description = "Changed"
	changed, err := RuntimeProfileCreateDigest(profile)
	if err != nil || changed == profileDigest {
		t.Fatal("profile field was not bound")
	}

	sandbox := FoundationSandboxCreateInput{
		Scope: FoundationScope{"tenant", "project"}, WorkspaceID: "workspace", WorkspaceName: "workspace",
		SandboxID: "sandbox", RuntimeProfileID: "standard", RuntimeProfileVersion: 1,
		Mutation: FoundationMutation{"request-sandbox", "foundation-sandbox-key-1"},
	}
	sandboxDigest, err := FoundationSandboxCreateDigest(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	sandbox.RuntimeProfileVersion++
	changed, err = FoundationSandboxCreateDigest(sandbox)
	if err != nil || changed == sandboxDigest {
		t.Fatal("sandbox profile version was not bound")
	}

	now := time.Now().UTC()
	snapshot := RuntimeProfileSnapshot{Scope: profile.Scope, ProfileVersionID: "rp-version", ProfileID: profile.ProfileID,
		ProfileName: profile.ProfileName, Description: profile.Description, Status: "published", TargetID: profile.TargetID,
		ImageURI: profile.ImageURI, ReleaseDigest: profile.ReleaseDigest, Version: profile.Version,
		CPUMillis: profile.CPUMillis, MemoryBytes: profile.MemoryBytes, ResourceVersion: 2,
		CreatedAt: now, UpdatedAt: now, PublishedAt: &now}
	if snapshot.Validate() != nil {
		t.Fatal("valid published snapshot rejected")
	}
	snapshot.TargetID = ""
	if snapshot.Validate() == nil {
		t.Fatal("invalid runtime profile accepted")
	}
}
