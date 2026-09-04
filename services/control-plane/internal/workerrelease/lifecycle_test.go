package workerrelease

import (
	"strings"
	"testing"
	"time"
)

func TestReleaseValidationAndDigestBindImmutableImage(t *testing.T) {
	input := RegisterInput{
		Scope:     Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"},
		ReleaseID: "release-1", ReleaseName: "release-1", ImageRepository: "registry.example/agents/worker",
		ReleaseDigest: "sha256:" + strings.Repeat("a", 64), PlatformVersion: "platform-1",
		RuntimeVersion: "runtime-1", CodexVersion: "codex-1", ClaudeCodeVersion: "claude-1",
		Architectures: []string{"linux/amd64"}, VerificationEvidenceDigest: "sha256:" + strings.Repeat("b", 64),
		Mutation: Mutation{RequestID: "request-1", IdempotencyKey: "release-register-1234"},
	}
	first, err := RegisterMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ReleaseDigest = "sha256:" + strings.Repeat("c", 64)
	second, err := RegisterMutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("mutation digest did not bind OCI digest: %q %q %v", first, second, err)
	}
	now := time.Date(2026, time.September, 4, 12, 0, 0, 0, time.UTC)
	snapshot := Snapshot{Scope: input.Scope, ReleaseID: input.ReleaseID, ReleaseName: input.ReleaseName,
		ImageRepository: input.ImageRepository, ReleaseDigest: input.ReleaseDigest,
		PlatformVersion: input.PlatformVersion, RuntimeVersion: input.RuntimeVersion,
		CodexVersion: input.CodexVersion, ClaudeCodeVersion: input.ClaudeCodeVersion,
		Architectures: input.Architectures, Status: "approved", VerificationState: "attested",
		VerificationEvidenceDigest: input.VerificationEvidenceDigest, ResourceVersion: 1,
		CreatedAt: now, UpdatedAt: now, ApprovedAt: now}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Architectures = []string{"linux/amd64", "linux/amd64"}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("duplicate architectures accepted")
	}
}
