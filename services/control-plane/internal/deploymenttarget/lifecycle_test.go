package deploymenttarget

import (
	"strings"
	"testing"
	"time"
)

func TestDeploymentTargetValidationAndDigests(t *testing.T) {
	input := RegisterInput{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha",
		Mutation: Mutation{RequestID: "request-alpha", IdempotencyKey: "register-key-123456"},
	}
	first, err := RegisterMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Endpoint = "https://docker-other.example.test:2376"
	second, err := RegisterMutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("registration digest did not bind endpoint: %q %q %v", first, second, err)
	}
	input.Endpoint = "unix:///var/run/docker.sock"
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("Control Plane accepted a Docker socket endpoint")
	}
	input.Kind, input.Endpoint = "kubernetes", "https://kubernetes.example.test:6443"
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatalf("Kubernetes target validation: %v", err)
	}
	input.Kind, input.Endpoint = "ssh", "ssh://ssh.example.test:22"
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatalf("SSH target validation: %v", err)
	}
	input.Mutation.IdempotencyKey = "register-key-12345~"
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatalf("contract-valid idempotency key: %v", err)
	}
	input.Endpoint = "https://ssh.example.test:22"
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("SSH target accepted an HTTPS endpoint")
	}
}

func TestDeploymentTargetSnapshotKeepsProbeFactsPhaseBound(t *testing.T) {
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	snapshot := Snapshot{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha", Generation: 1,
		SchedulingState: "active", ObservedPhase: "ready", APIVersion: "1.54", EngineVersion: "29.4.0", OS: "linux", Arch: "arm64",
		LastProbeAt: &now, ResourceVersion: 2, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.StableErrorCode = "must-not-coexist"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("ready target accepted a failure code")
	}
}

func TestDeploymentTargetSchedulingPreviewBindsImpactWithoutStoppingLeases(t *testing.T) {
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	target := Snapshot{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha", Generation: 2,
		SchedulingState: "active", ObservedPhase: "ready", APIVersion: "1.54", EngineVersion: "29.4.0", OS: "linux", Arch: "arm64",
		LastProbeAt: &now, ResourceVersion: 7, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}
	leases := []SchedulingLease{{LeaseID: "lease-alpha", LeaseName: "lease-alpha", Generation: 3, ObservedPhase: "ready"}}
	preview, err := NewSchedulingPreview(target, leases)
	if err != nil || preview.DesiredState != "drained" || len(preview.ActiveLeases) != 1 {
		t.Fatalf("preview = %#v / %v", preview, err)
	}
	target.SchedulingState = "drained"
	other, err := SchedulingImpactDigest(target, "active", leases)
	if err != nil || other == preview.ImpactDigest {
		t.Fatalf("scheduling digest did not bind state: %q %q / %v", preview.ImpactDigest, other, err)
	}
	if summary, err := SchedulingImpactSummary("drained", len(leases)); err != nil || !strings.Contains(summary, "1 active leases remain running") {
		t.Fatalf("impact summary = %q / %v", summary, err)
	}
}

func TestDeploymentTargetCleanupDigestBindsConfirmationFences(t *testing.T) {
	input := CleanupInput{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha",
		ExpectedGeneration: 2, ExpectedResourceVersion: 7, ImpactDigest: "sha256:" + strings.Repeat("a", 64),
		Mutation: Mutation{RequestID: "request-cleanup", IdempotencyKey: "cleanup-key-1234~"},
	}
	first, err := CleanupMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedResourceVersion++
	second, err := CleanupMutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("cleanup digest did not bind resourceVersion: %q %q %v", first, second, err)
	}
	completion := CleanupCompletion{Input: input, Succeeded: false, StableErrorCode: "target-cleanup-impact-conflict", ImpactSummary: "Cleanup stopped because the confirmed resource impact changed"}
	if err := completion.Validate("tenant-alpha"); err != nil {
		t.Fatal(err)
	}
}
