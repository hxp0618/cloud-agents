import { describe, expect, it } from "vitest";
import type {
  DeploymentTarget,
  DeploymentTargetCleanupPreview,
  DeploymentTargetSchedulingPreview,
  EnvironmentLease,
  Worker,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { deriveAdminView } from "./derived";

function target(
  uid: string,
  observedPhase: "ready" | "unprobed",
  resourceVersion: string,
): DeploymentTarget {
  return {
    metadata: { uid, name: uid, resourceVersion },
    spec: {
      targetKind: "docker",
      observedPhase,
      schedulingState: "active",
      engineVersion: "29",
      apiVersion: "1.54",
      os: "linux",
      architecture: "amd64",
    },
  } as unknown as DeploymentTarget;
}

describe("admin derived view", () => {
  it("keeps selection freshness, counts, and filtered resources in one pure result", () => {
    const readyTarget = target("target-ready", "ready", "2");
    const unprobedTarget = target("target-unprobed", "unprobed", "1");
    const lease = {
      metadata: { uid: "lease-ready", name: "lease-ready" },
      spec: {
        environmentId: "environment-ready",
        observedPhase: "ready",
        cleanupPhase: "complete",
      },
    } as unknown as EnvironmentLease;
    const worker = {
      metadata: { uid: "worker-ready", name: "worker-ready" },
      spec: {
        targetId: readyTarget.metadata.uid,
        targetKind: "docker",
        leaseId: lease.metadata.uid,
        state: "ready",
        releaseDigest: "sha256:release",
        health: { state: "online" },
      },
    } as unknown as Worker;
    const cleanup = {
      metadata: { uid: readyTarget.metadata.uid, resourceVersion: "2" },
    } as unknown as DeploymentTargetCleanupPreview;
    const staleScheduling = {
      metadata: { uid: readyTarget.metadata.uid, resourceVersion: "1" },
    } as unknown as DeploymentTargetSchedulingPreview;

    const view = deriveAdminView({
      resources: {
        targets: [readyTarget, unprobedTarget],
        leases: [lease],
        workers: [worker],
        releases: [],
        profiles: [],
        runtimeProfiles: [],
        sandboxes: [],
        workspaceSnapshots: [],
        storagePolicies: [],
        maintenanceOperations: [],
      },
      selection: {
        page: "overview",
        targetId: readyTarget.metadata.uid,
        leaseId: lease.metadata.uid,
        upgradeReleaseDigest: "",
        workerId: worker.metadata.uid,
        profileVersionId: "",
        runtimeProfileVersionId: "",
        sandboxId: "",
        storagePolicyId: "",
        restoreSnapshotId: "",
        maintenanceOperationId: "",
      },
      previews: { cleanup, scheduling: staleScheduling, leaseRelease: null },
      filters: {
        query: " ready ",
        targetKinds: [],
        targetPhases: [],
        leaseAttentionOnly: false,
        leasePhase: "",
        leaseCleanupBlockedOnly: false,
        workerStatus: "",
        failedOperationsOnly: false,
      },
    });

    expect(view.selectedTarget).toBe(readyTarget);
    expect(view.selectedCleanupPreview).toBe(cleanup);
    expect(view.selectedSchedulingPreview).toBeNull();
    expect(view.readyCount).toBe(1);
    expect(view.unprobedCount).toBe(1);
    expect(view.attentionCount).toBe(1);
    expect(view.readyLeaseCount).toBe(1);
    expect(view.onlineWorkerCount).toBe(1);
    expect(view.visibleTargets.map(({ metadata }) => metadata.uid)).toEqual(["target-ready"]);
    expect(view.visibleLeases).toEqual([lease]);
    expect(view.pageEntry.id).toBe("overview");
  });
});
