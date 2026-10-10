package controlplane_test

import (
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/deploymenttarget"
	"strings"
	"testing"
	"time"
)

func TestDeploymentTargetValidationAndDigests(t *testing.T) {
	input := deploymenttarget.RegisterInput{
		Scope: deploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha",
		Mutation: deploymenttarget.Mutation{RequestID: "request-alpha", IdempotencyKey: "register-key-123456"},
	}
	first, err := deploymenttarget.RegisterMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Endpoint = "https://docker-other.example.test:2376"
	second, err := deploymenttarget.RegisterMutationDigest(input)
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
	input.Kind, input.Endpoint = "remote-worker", "remote-worker://enrollment-alpha"
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("accepted direct registration of a server-owned RemoteWorker target")
	}

	input.Kind, input.Endpoint = "kubernetes", "https://kubernetes.example.test:6443"
	withoutCredential, err := deploymenttarget.RegisterMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	credential := deploymenttarget.SealedCredential{KeyID: "k1-" + strings.Repeat("a", 32), Sealed: make([]byte, 64), Fingerprint: "hmac-sha256:" + strings.Repeat("1", 64)}
	input.SealedCredential = &credential
	firstCredential, err := deploymenttarget.RegisterMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	credential.Fingerprint = "hmac-sha256:" + strings.Repeat("2", 64)
	secondCredential, err := deploymenttarget.RegisterMutationDigest(input)
	if err != nil || withoutCredential == firstCredential || firstCredential == secondCredential {
		t.Fatalf("registration digest did not bind the sealed credential: %q %q %q %v", withoutCredential, firstCredential, secondCredential, err)
	}
	input.Kind, input.Endpoint = "docker", "https://docker.example.test:2376"
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("accepted a sealed Kubernetes credential on a Docker target")
	}
}

func TestDeploymentTargetSnapshotKeepsProbeFactsPhaseBound(t *testing.T) {
	now := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)
	snapshot := deploymenttarget.Snapshot{
		Scope: deploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha", Generation: 1,
		SchedulingState: "active", ObservedPhase: "ready", APIVersion: "1.54", EngineVersion: "29.4.0", OS: "linux", Arch: "arm64",
		LastProbeAt: &now, ResourceVersion: 2, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	snapshot.Kind, snapshot.Endpoint, snapshot.CredentialRef = "remote-worker", "remote-worker://enrollment-alpha", "enrollment-alpha"
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("RemoteWorker target projection: %v", err)
	}
	snapshot.StableErrorCode = "must-not-coexist"
	if err := snapshot.Validate(); err == nil {
		t.Fatal("ready target accepted a failure code")
	}
}

func TestDeploymentTargetSchedulingPreviewBindsImpactWithoutStoppingLeases(t *testing.T) {
	now := time.Date(2026, time.September, 4, 8, 0, 0, 0, time.UTC)
	target := deploymenttarget.Snapshot{
		Scope: deploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha", TargetName: "docker-alpha",
		Kind: "docker", Endpoint: "https://docker.example.test:2376", CredentialRef: "docker-alpha", Generation: 2,
		SchedulingState: "active", ObservedPhase: "ready", APIVersion: "1.54", EngineVersion: "29.4.0", OS: "linux", Arch: "arm64",
		LastProbeAt: &now, ResourceVersion: 7, CreatedAt: now.Add(-time.Minute), UpdatedAt: now,
	}
	leases := []deploymenttarget.SchedulingLease{{LeaseID: "lease-alpha", LeaseName: "lease-alpha", Generation: 3, ObservedPhase: "ready"}}
	preview, err := deploymenttarget.NewSchedulingPreview(target, leases)
	if err != nil || preview.DesiredState != "drained" || len(preview.ActiveLeases) != 1 {
		t.Fatalf("preview = %#v / %v", preview, err)
	}
	target.SchedulingState = "drained"
	other, err := deploymenttarget.SchedulingImpactDigest(target, "active", leases)
	if err != nil || other == preview.ImpactDigest {
		t.Fatalf("scheduling digest did not bind state: %q %q / %v", preview.ImpactDigest, other, err)
	}
	if summary, err := deploymenttarget.SchedulingImpactSummary("drained", len(leases)); err != nil || !strings.Contains(summary, "new lease/session/turn/execution admission stopped") || !strings.Contains(summary, "1 active leases retained, running tasks may finish") {
		t.Fatalf("impact summary = %q / %v", summary, err)
	}
}

func TestDeploymentTargetCleanupDigestBindsConfirmationFences(t *testing.T) {
	input := deploymenttarget.CleanupInput{
		Scope: deploymenttarget.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, TargetID: "target-alpha",
		ExpectedGeneration: 2, ExpectedResourceVersion: 7, ImpactDigest: "sha256:" + strings.Repeat("a", 64),
		Mutation: deploymenttarget.Mutation{RequestID: "request-cleanup", IdempotencyKey: "cleanup-key-1234~"},
	}
	first, err := deploymenttarget.CleanupMutationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedResourceVersion++
	second, err := deploymenttarget.CleanupMutationDigest(input)
	if err != nil || first == second {
		t.Fatalf("cleanup digest did not bind resourceVersion: %q %q %v", first, second, err)
	}
	completion := deploymenttarget.CleanupCompletion{Input: input, Succeeded: false, StableErrorCode: "target-cleanup-impact-conflict", ImpactSummary: "Cleanup stopped because the confirmed resource impact changed"}
	if err := completion.Validate("tenant-alpha"); err != nil {
		t.Fatal(err)
	}
}
