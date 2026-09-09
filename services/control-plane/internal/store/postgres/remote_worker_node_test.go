package postgres

import (
	"testing"
	"time"
)

func TestRemoteWorkerSandboxCreateRetryOmitsPhysicalVolume(t *testing.T) {
	volume := "ca-ws-existing"
	command, err := remoteWorkerSandboxCommand(FoundationSandboxClaim{
		OperationID: "op-create", WorkspaceID: "workspace", WorkspaceName: "workspace",
		TargetID: "target", SandboxID: "sandbox", Action: "sandbox.create",
		ImageURI:        "node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5",
		SpecDigest:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		NetworkPolicyID: "network", PhysicalVolumeName: &volume, DeliveryAttempts: 2,
		SandboxGeneration: 1, WorkloadTrust: "trusted-single-tenant", IsolationRuntime: "runc",
		CPUMillis: 500, MemoryBytes: 536870912, ClaimExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil || command.PhysicalVolumeName != "" {
		t.Fatalf("create retry command retained server-side volume: command=%+v err=%v", command, err)
	}
}
