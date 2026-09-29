import {
  ClientError,
  JSONContractError,
  parseProblem,
  type AdminEnvironmentLeaseUpgradeRequest,
  type AdminSandboxSession,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetCleanupRequest,
  type DeploymentTargetSchedulingPreview,
  type DeploymentTargetSchedulingRequest,
  type EnvironmentLeaseUpgradePreview,
  type RemoteWorkerNodeStatus,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import type { MessageKey } from "./i18n";
import { AdminUIError } from "./admin/errors";
export {
  capabilityBindingRelations,
  listAdminManagedAgentBindings,
  listAdminMcpServers,
  listAdminSkillBundles,
  loadAdminManagedAgentRuntime,
  type AdminCapabilityBinding,
  type AdminManagedAgentRuntime,
} from "./admin/capabilities";
export {
  listAdminTargets,
  listAdminLeases,
  listAdminWorkers,
  listAdminRemoteWorkerEnrollments,
  listAdminRemoteWorkerEnrollmentAuditEvents,
  listAdminRemoteWorkerOperations,
  listAdminReleases,
  listAdminProfiles,
  listAdminRuntimeProfiles,
  listAdminSandboxes,
  listAdminSandboxAccessGrants,
  listAdminStoragePolicies,
  listAdminWorkspaceSnapshots,
  listAdminStoragePolicyAuditEvents,
  listAdminNetworkPolicies,
  listAdminNetworkPolicyAuditEvents,
  loadAdminProjectLeaseQuota,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminProfileAuditEvents,
  listAdminTargetOperations,
  listAdminMaintenanceOperations,
  listAdminTargetAuditEvents,
} from "./admin/lists";
export {
  filterAdminLeases,
  filterAdminMaintenanceOperations,
  filterAdminTargets,
  filterAdminWorkers,
  leaseNeedsAttention,
  matchesAdminSearch,
  pageAdminTargets,
  summarizeClusterHosts,
  targetIdentifierPattern,
  targetPageSizes,
} from "./admin/filters";
export type { ClusterHostSummary, WorkerStatusFilter } from "./admin/filters";
export { newIdempotencyKey, newRequestId } from "./admin/request";
export {
  keepIfUnchanged,
  replaceLease,
  replaceNetworkPolicy,
  replaceProfile,
  replaceRelease,
  replaceRemoteWorkerEnrollment,
  replaceRuntimeProfile,
  replaceStoragePolicy,
  replaceTarget,
  selectAdminResourceId,
  workerRefreshKey,
} from "./admin/resources";

export type { AdminClient } from "./admin/client";
export { AdminUIError } from "./admin/errors";

export function remoteWorkerFoundationSupport(
  node: Pick<RemoteWorkerNodeStatus, "architecture" | "capabilities" | "capacity">,
) {
  return {
    runtime: node.capabilities.includes("docker"),
    architecture: node.architecture === "amd64" || node.architecture === "arm64",
    storage:
      node.capabilities.includes("workspace-volume") && node.capacity.diskBytes >= 20 * 1024 ** 3,
    network: node.capabilities.includes("network-dns-nft"),
  } as const;
}

export type SandboxLifecycleAction = "stop" | "rebuild";

export function availableSandboxLifecycleAction({
  spec,
}: AdminSandboxSession): SandboxLifecycleAction | null {
  if (
    spec.operationState !== "succeeded" ||
    spec.cleanupPhase !== "complete" ||
    spec.observedGeneration !== spec.generation ||
    spec.workspaceObservedState !== "available" ||
    spec.physicalVolumeId === undefined
  )
    return null;
  if (
    spec.desiredState === "running" &&
    spec.observedState === "running" &&
    !spec.writerReleased &&
    spec.runtimeId !== undefined
  )
    return "stop";
  if (
    spec.desiredState === "stopped" &&
    spec.observedState === "stopped" &&
    spec.writerReleased &&
    spec.runtimeId === undefined
  )
    return "rebuild";
  return null;
}

export type SavedAdminConnection = Readonly<{
  endpoint: string;
  tenantId: string;
  projectId: string;
}>;

type ConnectionStorage = Pick<Storage, "getItem" | "setItem">;

const storageKey = "cloud-agents.admin-web.connection.v1";
const emptyConnection: SavedAdminConnection = Object.freeze({
  endpoint: "",
  tenantId: "",
  projectId: "",
});

export function readSavedAdminConnection(storage: ConnectionStorage): SavedAdminConnection {
  try {
    const raw = storage.getItem(storageKey);
    if (raw === null) return emptyConnection;
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== "object" || value === null || Array.isArray(value)) return emptyConnection;
    const candidate = value as Record<string, unknown>;
    if (
      typeof candidate.endpoint !== "string" ||
      typeof candidate.tenantId !== "string" ||
      typeof candidate.projectId !== "string" ||
      candidate.endpoint.length > 2048 ||
      candidate.tenantId.length > 128 ||
      candidate.projectId.length > 128
    )
      return emptyConnection;
    return Object.freeze({
      endpoint: candidate.endpoint,
      tenantId: candidate.tenantId,
      projectId: candidate.projectId,
    });
  } catch {
    return emptyConnection;
  }
}

export function writeSavedAdminConnection(
  storage: ConnectionStorage,
  connection: SavedAdminConnection,
): void {
  try {
    storage.setItem(
      storageKey,
      JSON.stringify({
        endpoint: connection.endpoint,
        tenantId: connection.tenantId,
        projectId: connection.projectId,
      }),
    );
  } catch {
    // The live connection still works in hardened contexts without browser storage.
  }
}

export function cleanupRequestFromPreview(
  preview: DeploymentTargetCleanupPreview,
): DeploymentTargetCleanupRequest {
  return Object.freeze({
    expectedGeneration: preview.spec.expectedGeneration,
    expectedResourceVersion: preview.spec.expectedResourceVersion,
    impactDigest: preview.spec.impactDigest,
  });
}

export function schedulingRequestFromPreview(
  preview: DeploymentTargetSchedulingPreview,
): DeploymentTargetSchedulingRequest {
  return Object.freeze({
    expectedGeneration: preview.spec.expectedGeneration,
    expectedResourceVersion: preview.spec.expectedResourceVersion,
    desiredState: preview.spec.desiredState,
    impactDigest: preview.spec.impactDigest,
  });
}

export function leaseReleaseRequestFromPreview(
  preview: EnvironmentLeaseUpgradePreview,
): AdminEnvironmentLeaseUpgradeRequest {
  return Object.freeze({
    releaseDigest: preview.spec.targetReleaseDigest,
    expectedGeneration: preview.spec.expectedGeneration,
    expectedResourceVersion: preview.spec.expectedResourceVersion,
    impactDigest: preview.spec.impactDigest,
  });
}

export function adminErrorKey(error: unknown): MessageKey {
  if (error instanceof AdminUIError) return error.messageKey;
  if (error instanceof ClientError && error.status === 401) return "error.tokenExpired";
  if (error instanceof ClientError && error.status === 403) return "error.forbidden";
  if (error instanceof ClientError && error.status === 404) return "error.notFound";
  if (error instanceof ClientError && error.status === 409) return "error.conflict";
  if (error instanceof ClientError && error.status === 400) return "error.invalidRequest";
  if (error instanceof ClientError && (error.status === 502 || error.status === 503))
    return "error.actuatorUnavailable";
  if (error instanceof JSONContractError) return "error.contract";
  if (error instanceof DOMException && error.name === "TimeoutError") return "error.timeout";
  if (error instanceof DOMException && error.name === "AbortError") return "error.cancelled";
  if (error instanceof TypeError) return "error.connection";
  return "error.generic";
}

export function adminFailure(error: unknown): Readonly<{ key: MessageKey; code: string | null }> {
  let code: string | null = null;
  if (error instanceof ClientError) {
    try {
      const problem = parseProblem(JSON.stringify(error.problem));
      if (problem.status === error.status) code = problem.error.code;
    } catch {
      // Invalid responses must not expose unvalidated diagnostics or secret fields.
    }
  }
  return { key: adminErrorKey(error), code };
}
