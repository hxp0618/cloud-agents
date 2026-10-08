package controlplane_test

import (
	"github.com/hxp0618/cloud-agents/services/control-plane/internal/remoteworker"
	"strings"
	"testing"
	"time"

	platformv1alpha1 "github.com/hxp0618/cloud-agents/sdk/go/gen/platform/v1alpha1"
)

func TestHeartbeatInputAndNodeStatusValidate(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	input := remoteworker.HeartbeatInput{
		Scope: remoteworker.Scope{TenantID: "tenant-alpha", ProjectID: "project-alpha"}, EnrollmentID: "enrollment-alpha",
		PeerCertificateSHA256: "sha256:" + strings.Repeat("a", 64), IncarnationID: "incarnation-alpha",
		ObservedGeneration: 1, ObservedState: "active", WorkerVersion: "v0.1.0", OS: "linux", Architecture: "arm64", KernelVersion: "6.12.1",
		Capabilities: []string{"docker", "exec", "files"}, Capacity: remoteworker.Capacity{CPUMillis: 4000, MemoryBytes: 8 << 30, DiskBytes: 40 << 30},
	}
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatal(err)
	}
	status := remoteworker.NodeStatus{
		Scope: input.Scope, EnrollmentID: input.EnrollmentID, WorkerID: "worker-alpha", WorkerName: "worker-alpha", IncarnationID: input.IncarnationID,
		ResourceVersion: 1, Generation: 1, ObservedGeneration: 1, DesiredState: "active", ObservedState: "active", HealthState: "online",
		WorkerVersion: input.WorkerVersion, OS: input.OS, Architecture: input.Architecture, KernelVersion: input.KernelVersion,
		Capabilities: input.Capabilities, Capacity: input.Capacity, FirstConnectedAt: now, LastHeartbeatAt: now, HeartbeatExpiresAt: now.Add(remoteworker.HeartbeatTTL),
	}
	if err := status.Validate(); err != nil {
		t.Fatal(err)
	}
	input.Capabilities = []string{"docker", "isolation-gvisor", "network-internal-deny"}
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatalf("strong-isolation capabilities rejected: %v", err)
	}
	input.Capabilities = status.Capabilities
	input.SandboxCommandID = "rwsc-alpha"
	if err := input.Validate("tenant-alpha"); err != nil {
		t.Fatalf("valid Sandbox claim renewal rejected: %v", err)
	}
	input.SandboxCommandID = "invalid command"
	if err := input.Validate("tenant-alpha"); err == nil {
		t.Fatal("accepted invalid Sandbox claim renewal command")
	}
	input.SandboxCommandID = ""
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
	command := remoteworker.SandboxExecCommand{CommandID: "rwexec-alpha", WorkspaceID: "workspace-alpha", TargetID: "target-alpha",
		SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha", RuntimeOperationID: "operation-alpha",
		RuntimeSpecDigest: digest, Command: "printf bounded", TimeoutSeconds: 10, Deadline: time.Now().Add(time.Minute)}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	receipt := remoteworker.SandboxExecCommandReceipt{CommandID: command.CommandID, SandboxID: command.SandboxID,
		SandboxGeneration: command.SandboxGeneration, Result: "succeeded", ExitCode: 7, Stdout: "proof\n", Stderr: "failed", ExecutionTimeMillis: 1}
	first, err := remoteworker.SandboxExecCommandReceiptDigest(receipt)
	second, replayErr := remoteworker.SandboxExecCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first != second || first == "" {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.Stdout = strings.Repeat("x", 1<<20)
	receipt.Stderr = "x"
	if receipt.Validate() == nil {
		t.Fatal("combined Exec output above 1 MiB accepted")
	}
}

func TestSandboxFileCommandAndReceiptUseGeneratedValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := remoteworker.SandboxFileCommand{CommandID: "rwfile-alpha", EventID: "event-alpha", GrantID: "grant-alpha",
		WorkspaceID: "workspace-alpha", TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3,
		RuntimeID: "runtime-alpha", RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: digest,
		Action: "write", Path: "notes.txt", Write: &platformv1alpha1.RemoteWorkerSandboxFileWriteCommand{ContentBase64URL: "aGk"},
		Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := remoteworker.ValidateSandboxFileCommand(command); err != nil {
		t.Fatal(err)
	}
	receipt := remoteworker.SandboxFileCommandReceipt{CommandID: command.CommandID, EventID: command.EventID, GrantID: command.GrantID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Action: "delete", Result: "succeeded"}
	first, err := remoteworker.SandboxFileCommandReceiptDigest(receipt)
	second, replayErr := remoteworker.SandboxFileCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first == "" || first != second {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.StableErrorCode = "secret_error"
	if remoteworker.ValidateSandboxFileCommandReceipt(receipt) == nil {
		t.Fatal("accepted a successful file receipt with a stable error")
	}
}

func TestSandboxPTYCommandAndReceiptUseGeneratedValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	since, takeover := int64(0), false
	command := remoteworker.SandboxPTYCommand{CommandID: "rwpty-alpha", GrantID: "grant-alpha", WorkspaceID: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: digest, Action: "exchange", SessionID: "session-alpha",
		Since: &since, Takeover: &takeover, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := remoteworker.ValidateSandboxPTYCommand(command); err != nil {
		t.Fatal(err)
	}
	running, outputOffset := true, int64(0)
	frames := []platformv1alpha1.RemoteWorkerSandboxPTYFrame{}
	receipt := remoteworker.SandboxPTYCommandReceipt{CommandID: command.CommandID, GrantID: command.GrantID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Action: command.Action,
		Result: "succeeded", SessionID: command.SessionID, Running: &running, OutputOffset: &outputOffset, Frames: &frames}
	first, err := remoteworker.SandboxPTYCommandReceiptDigest(receipt)
	second, replayErr := remoteworker.SandboxPTYCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first == "" || first != second {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.BytesTransferred = 1
	if remoteworker.ValidateSandboxPTYCommandReceipt(receipt) == nil {
		t.Fatal("accepted a PTY receipt with a mismatched byte count")
	}
}

func TestSandboxPreviewCommandAndReceiptUseGeneratedValidation(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := remoteworker.SandboxPreviewCommand{CommandID: "rwpreview-alpha", GrantID: "grant-alpha", WorkspaceID: "workspace-alpha",
		TargetID: "target-alpha", SandboxID: "sandbox-alpha", SandboxGeneration: 3, RuntimeID: "runtime-alpha",
		RuntimeOperationID: "operation-alpha", RuntimeSpecDigest: digest, Port: 3000, Method: "POST", Path: "/hello",
		RawQuery: "value=alpha", Headers: []platformv1alpha1.RemoteWorkerSandboxPreviewHeader{}, BodyBase64URL: "aGk",
		Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := remoteworker.ValidateSandboxPreviewCommand(command); err != nil {
		t.Fatal(err)
	}
	status, body := int64(201), "aGk"
	headers := []platformv1alpha1.RemoteWorkerSandboxPreviewHeader{}
	receipt := remoteworker.SandboxPreviewCommandReceipt{CommandID: command.CommandID, GrantID: command.GrantID,
		SandboxID: command.SandboxID, SandboxGeneration: command.SandboxGeneration, Port: command.Port,
		Result: "succeeded", BytesTransferred: 2, StatusCode: &status, Headers: &headers, BodyBase64URL: &body}
	first, err := remoteworker.SandboxPreviewCommandReceiptDigest(receipt)
	second, replayErr := remoteworker.SandboxPreviewCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first == "" || first != second {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.BytesTransferred = 1
	if remoteworker.ValidateSandboxPreviewCommandReceipt(receipt) == nil {
		t.Fatal("accepted a Preview receipt with a mismatched byte count")
	}
}

func TestWorkspaceSnapshotCommandAndReceiptValidate(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	command := remoteworker.WorkspaceSnapshotCommand{CommandID: "rwws-alpha", Attempt: 1, Action: "workspace.snapshot", OperationID: "operation-alpha", WorkspaceID: "workspace-alpha", TargetID: "target-alpha", SnapshotID: "snapshot-alpha", SourceVolumeName: "ca-ws-alpha", ImageURI: "registry.example/worker@" + digest, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	if err := remoteworker.ValidateWorkspaceSnapshotCommand(command); err != nil {
		t.Fatal(err)
	}
	receipt := remoteworker.WorkspaceSnapshotCommandReceipt{CommandID: command.CommandID, Attempt: command.Attempt, Action: command.Action, OperationID: command.OperationID, WorkspaceID: command.WorkspaceID, TargetID: command.TargetID, SnapshotID: command.SnapshotID, Result: "succeeded", VolumeName: "ca-portable-snapshot-alpha", ContentDigest: digest, SizeBytes: 17, CleanupComplete: true}
	first, err := remoteworker.WorkspaceSnapshotCommandReceiptDigest(receipt)
	second, replayErr := remoteworker.WorkspaceSnapshotCommandReceiptDigest(receipt)
	if err != nil || replayErr != nil || first == "" || first != second {
		t.Fatalf("receipt digests=%q/%q errors=%v/%v", first, second, err, replayErr)
	}
	receipt.ContentDigest = "sha256:" + strings.Repeat("b", 64)
	if err := remoteworker.ValidateWorkspaceSnapshotCommandReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	receipt.Result, receipt.VolumeName, receipt.ContentDigest, receipt.SizeBytes, receipt.StableErrorCode = "failed", "", "", 0, "workspace_snapshot_archive_unavailable"
	if err := remoteworker.ValidateWorkspaceSnapshotCommandReceipt(receipt); err != nil {
		t.Fatal(err)
	}
}
