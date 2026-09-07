package remoteworker

import (
	"strings"
	"testing"
	"time"
)

func TestHeartbeatInputAndNodeStatusValidate(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	input := HeartbeatInput{
		Scope: Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, EnrollmentID: "enrollment-alpha",
		PeerCertificateSHA256: "sha256:" + strings.Repeat("a", 64), IncarnationID: "incarnation-alpha",
		ObservedGeneration: 1, ObservedState: "active", WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files"}, Capacity: Capacity{CPUMillis: 4000, MemoryBytes: 8 << 30, DiskBytes: 40 << 30},
	}
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatal(err)
	}
	status := NodeStatus{
		Scope: input.Scope, EnrollmentID: input.EnrollmentID, WorkerID: "worker-alpha", WorkerName: "worker-alpha", IncarnationID: input.IncarnationID,
		ResourceVersion: 1, Generation: 1, ObservedGeneration: 1, DesiredState: "active", ObservedState: "active", HealthState: "online",
		WorkerVersion: input.WorkerVersion, OS: input.OS, Architecture: input.Architecture, KernelVersion: input.KernelVersion,
		Capabilities: input.Capabilities, Capacity: input.Capacity, FirstConnectedAt: now, LastHeartbeatAt: now, HeartbeatExpiresAt: now.Add(HeartbeatTTL),
	}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
	input.Capabilities = []string{"files", "docker"}
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("accepted non-canonical capabilities")
	}
	status.ObservedGeneration = 2
	if err := status.Validate(); err == nil {
		t.Fatal("accepted observed generation ahead of authority")
	}
}

func TestSandboxExecCommandAndReceiptValidate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := SandboxExecCommand{CommandID: "rwexec-alpha", WorkspaceID: "workspace-alpha", TargetID: "target-alpha",
		SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha", RuntimeOperationID: "operation-alpha",
		RuntimeSpecDigest: digest, Command: "printf bounded", TimeoutSeconds: 10, Deadline: time.Now().Add(time.Minute)}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt := SandboxExecCommandReceipt{CommandID: command.CommandID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Result: "succeeded", ExitCode: 7, Stdout: "proof\n", Stderr: "failed", ExecutionTimeMillis: 1}
	first, err := SandboxExecCommandReceiptDigest(receipt)
	second, replayErr := SandboxExecCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first != second || first == "" {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.Stdout = strings.Repeat("x", 1<<20)
	receipt.Stderr = "x"
	if receipt.Validate() == nil {
		t.Fatal("combined Exec output above 1 MiB accepted")
	}
}
