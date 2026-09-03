package environmentprofile

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDraftProfileValidationAndDigestBindSchedulingInputs(t *testing.T) {
	input := CreateInput{
		Scope:     Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		ProfileID: "standard", ProfileName: "standard", Version: 1, Description: "Standard workspace",
		ProviderKinds: []string{"codex", "claudeAgent"}, CPULimitMillis: 2000, MemoryLimitBytes: 4294967296,
		StoragePolicyRef: "workspace-8gb", NetworkPolicyRef: "public-egress",
		ReleaseDigest: "sha256:" + strings.Repeat("a", 64), TargetRefs: []string{"docker-primary"},
		ProviderCredentialRef: "provider-primary",
		Mutation:              Mutation{RequestID: "request-create", IdempotencyKey: "profile-create-1234"},
	}
	first, err := CreateMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.TargetRefs = []string{"docker-secondary"}
	second, err := CreateMutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("profile digest did not bind target refs: %q %q %v", first, second, err)
	}
	now := time.Date(2026, time.September, 3, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Scope: input.Scope, ProfileVersionUID: "ep-0123456789abcdef0123456789abcdef",
		ProfileID: input.ProfileID, ProfileName: input.ProfileName, Version: input.Version,
		Description: input.Description, Status: "draft", ProviderKinds: input.ProviderKinds,
		CPULimitMillis: input.CPULimitMillis, MemoryLimitBytes: input.MemoryLimitBytes,
		StoragePolicyRef: input.StoragePolicyRef, NetworkPolicyRef: input.NetworkPolicyRef,
		ReleaseDigest: input.ReleaseDigest, TargetRefs: input.TargetRefs,
		ProviderCredentialRef: input.ProviderCredentialRef, ResourceVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.PublishedAt = &now
	if err := snapshot.Validate(); err == nil {
		t.Fatal("draft profile accepted a published timestamp")
	}
}

func TestProfileRejectsDuplicateAuthorityLists(t *testing.T) {
	if validProviderKinds([]string{"codex", "codex"}) || validIdentifiers([]string{"target-a", "target-a"}, 32) {
		t.Fatal("profile accepted duplicate provider or target authority")
	}
}

func TestProfileTransitionDigestBindsActionAndResourceVersion(t *testing.T) {
	input := TransitionInput{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, ProfileID: "standard",
		Version: 1, ExpectedResourceVersion: 1, Action: TransitionPublish,
		Mutation: Mutation{RequestID: "request-publish", IdempotencyKey: "profile-publish-1234"},
	}
	publish, err := TransitionMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Action = TransitionDisable
	disable, err := TransitionMutationDigest(input)
	if err != nil || publish == disable {
		t.Fatalf("transition digest did not bind action: %q %q %v", publish, disable, err)
	}
	input.Action = "delete"
	if _, err := TransitionMutationDigest(input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unsupported transition error = %v", err)
	}
}
