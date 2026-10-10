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
  loadAdminManagedAgentEventPage,
  listAdminMcpServers,
  listAdminSkillBundles,
  loadAdminManagedAgentRuntime,
  type AdminCapabilityBinding,
  type AdminManagedAgentRuntime,
  type AdminManagedAgentEventPage,
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
export {
  identifierFromName,
  identifierWithSuffix,
  newIdentifierSuffix,
  nextProfileVersion,
  uniqueIdentifier,
} from "./admin/identifiers";
export {
  adminMutationKey,
  newIdempotencyKey,
  newRequestId,
  pendingIdempotencyKey,
} from "./admin/request";
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
  tenantId: string;
  projectId: string;
}>;

type ConnectionStorage = Pick<Storage, "getItem" | "setItem">;

const storageKey = "cloud-agents.admin-web.connection.v1";
const emptyConnection: SavedAdminConnection = Object.freeze({
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
      typeof candidate.tenantId !== "string" ||
      typeof candidate.projectId !== "string" ||
      candidate.tenantId.length > 128 ||
      candidate.projectId.length > 128
    )
      return emptyConnection;
    return Object.freeze({
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

function stableProblemCode(error: unknown): string | null {
  if (!(error instanceof ClientError)) return null;
  try {
    const problem = parseProblem(JSON.stringify(error.problem));
    return problem.status === error.status ? problem.error.code : null;
  } catch {
    // Invalid responses must not expose unvalidated diagnostics or secret fields.
    return null;
  }
}

function codeErrorKey(code: string | null): MessageKey | undefined {
  switch (code) {
    case "PROJECT_LEASE_COUNT_QUOTA_EXCEEDED":
      return "error.quotaCount";
    case "PROJECT_LEASE_CPU_QUOTA_EXCEEDED":
      return "error.quotaCpu";
    case "PROJECT_LEASE_MEMORY_QUOTA_EXCEEDED":
      return "error.quotaMemory";
    case "PROJECT_LEASE_TTL_QUOTA_EXCEEDED":
      return "error.quotaTtl";
    default:
      return undefined;
  }
}

function errorKey(error: unknown, code: string | null): MessageKey {
  // Quota codes are only actionable for the lease-conflict response. A stale
  // or malformed code must not hide a more important auth or transport error.
  const codeKey = error instanceof ClientError && error.status === 409 ? codeErrorKey(code) : undefined;
  if (codeKey !== undefined) return codeKey;
  if (error instanceof AdminUIError) return error.messageKey;
  if (error instanceof ClientError && error.status === 401) return "error.tokenExpired";
  if (error instanceof ClientError && error.status === 403) return "error.forbidden";
  if (error instanceof ClientError && error.status === 404) return "error.notFound";
  if (error instanceof ClientError && error.status === 409) return "error.conflict";
  if (error instanceof ClientError && [400, 405, 415, 422].includes(error.status))
    return "error.invalidRequest";
  if (error instanceof ClientError && error.status === 408) return "error.timeout";
  if (error instanceof ClientError && error.status === 429) return "error.rateLimited";
  if (error instanceof ClientError && [502, 503, 504].includes(error.status))
    return "error.actuatorUnavailable";
  if (error instanceof JSONContractError) return "error.contract";
  if (error instanceof DOMException && error.name === "TimeoutError") return "error.timeout";
  if (error instanceof DOMException && error.name === "AbortError") return "error.cancelled";
  if (error instanceof TypeError) return "error.connection";
  return "error.generic";
}

export function adminErrorKey(error: unknown): MessageKey {
  return errorKey(error, stableProblemCode(error));
}

export function adminFailure(error: unknown): Readonly<{ key: MessageKey; code: string | null }> {
  const code = stableProblemCode(error);
  return { key: errorKey(error, code), code };
}
