package controlplane_test

import (
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/coordination"
	"strings"
	"testing"
	"time"
)

func TestRuntimeProfileAndSandboxInputsBindEveryPublicField(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	profile := coordination.RuntimeProfileCreateInput{
		Scope: coordination.FoundationScope{TenantID: "tenant", ProjectID: "project"}, ProfileID: "standard", ProfileName: "standard",
		Version: 1, Description: "No-agent Docker workspace", TargetID: "docker-primary",
		WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		NetworkPolicyID: "network-deny",
		ImageURI:        "ghcr.io/opensandbox/server@" + digest, ReleaseDigest: digest,
		CPUMillis: 1000, MemoryBytes: 536870912,
		Mutation: coordination.FoundationMutation{RequestID: "request-profile", IdempotencyKey: "runtime-profile-key-0001"},
	}
	profileDigest, err := coordination.RuntimeProfileCreateDigest(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile.Description = "Changed"
	changed, err := coordination.RuntimeProfileCreateDigest(profile)
	if err != nil || changed == profileDigest {
		t.Fatal("profile field was not bound")
	}

	sandbox := coordination.FoundationSandboxCreateInput{
		Scope: coordination.FoundationScope{TenantID: "tenant", ProjectID: "project"}, WorkspaceID: "workspace", WorkspaceName: "workspace",
		SandboxID: "sandbox", RuntimeProfileID: "standard", RuntimeProfileVersion: 1, TTLSeconds: 60,
		Mutation: coordination.FoundationMutation{RequestID: "request-sandbox", IdempotencyKey: "foundation-sandbox-key-1"},
	}
	sandboxDigest, err := coordination.FoundationSandboxCreateDigest(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	sandbox.RuntimeProfileVersion++
	changed, err = coordination.FoundationSandboxCreateDigest(sandbox)
	if err != nil || changed == sandboxDigest {
		t.Fatal("sandbox profile version was not bound")
	}
	sandbox.RuntimeProfileVersion--
	sandbox.TTLSeconds++
	changed, err = coordination.FoundationSandboxCreateDigest(sandbox)
	if err != nil || changed == sandboxDigest {
		t.Fatal("sandbox TTL was not bound")
	}

	now := time.Now().UTC()
	snapshot := coordination.RuntimeProfileSnapshot{Scope: profile.Scope, ProfileVersionID: "rp-version", ProfileID: profile.ProfileID,
		ProfileName: profile.ProfileName, Description: profile.Description, Status: "published", TargetID: profile.TargetID,
		WorkloadTrust: profile.WorkloadTrust, IsolationRuntime: profile.IsolationRuntime,
		NetworkPolicyID: profile.NetworkPolicyID,
		ImageURI:        profile.ImageURI, ReleaseDigest: profile.ReleaseDigest, Version: profile.Version,
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
