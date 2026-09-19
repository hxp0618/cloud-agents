import {
  ClientError,
  JSONContractError,
  parseProblem,
  type AdminEnvironmentLeaseUpgradeRequest,
  type AdminAuditEvent,
  type AdminSandboxAccessGrant,
  type AdminSandboxSession,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetCleanupRequest,
  type DeploymentTargetSchedulingPreview,
  type DeploymentTargetSchedulingRequest,
  type EnvironmentLease,
  type EnvironmentLeaseUpgradePreview,
  type EnvironmentProfile,
  type MaintenanceOperation,
  type ProjectLeaseQuota,
  type RuntimeProfile,
  type StoragePolicy,
  type NetworkPolicy,
  type RemoteWorkerEnrollment,
  type RemoteWorkerNodeStatus,
  type Worker,
  type WorkerRelease,
  type WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import type { MessageKey } from "./i18n";
import { AdminUIError } from "./admin/errors";
import type { AdminClient } from "./admin/client";
import { collectAdminPages } from "./admin/pagination";
import { newRequestId } from "./admin/request";
export {
  capabilityBindingRelations,
  listAdminManagedAgentBindings,
  listAdminMcpServers,
  listAdminSkillBundles,
  loadAdminManagedAgentRuntime,
  type AdminCapabilityBinding,
  type AdminManagedAgentRuntime,
} from "./admin/capabilities";
export { newIdempotencyKey, newRequestId } from "./admin/request";
export {
  replaceLease,
  replaceNetworkPolicy,
  replaceProfile,
  replaceRelease,
  replaceRemoteWorkerEnrollment,
  replaceRuntimeProfile,
  replaceStoragePolicy,
  replaceTarget,
  selectAdminResourceId,
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

export type ClusterHostSummary = Readonly<{
  target: DeploymentTarget;
  workerCount: number;
  readyWorkerCount: number;
  latestHealthAt: string | undefined;
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

export function filterAdminTargets(
  targets: readonly DeploymentTarget[],
  query: string,
  kinds: readonly DeploymentTarget["spec"]["targetKind"][],
  phases: readonly DeploymentTarget["spec"]["observedPhase"][],
): readonly DeploymentTarget[] {
  const search = query.trim().toLocaleLowerCase();
  return targets.filter(
    ({ metadata, spec }) =>
      (kinds.length === 0 || kinds.includes(spec.targetKind)) &&
      (phases.length === 0 || phases.includes(spec.observedPhase)) &&
      [
        metadata.uid,
        metadata.name,
        spec.targetKind,
        spec.observedPhase,
        spec.schedulingState,
        spec.engineVersion,
        spec.apiVersion,
        spec.os,
        spec.architecture,
      ].some((value) => value.toLocaleLowerCase().includes(search)),
  );
}

export function leaseNeedsAttention({ spec }: EnvironmentLease): boolean {
  return spec.observedPhase === "failed" || spec.cleanupPhase === "blocked";
}

export type WorkerStatusFilter =
  | ""
  | "online"
  | "expired"
  | "unavailable"
  | "not-observed"
  | "failed";
export function filterAdminWorkers(
  workers: readonly Worker[],
  query: string,
  status: WorkerStatusFilter = "",
): readonly Worker[] {
  const search = query.trim().toLocaleLowerCase();
  return workers.filter(
    ({ metadata, spec }) =>
      (status === "" ||
        (status === "failed"
          ? spec.state === "failed"
          : status === "not-observed"
            ? spec.health === undefined
            : spec.health?.state === status)) &&
      [
        metadata.uid,
        metadata.name,
        spec.leaseId,
        spec.targetId,
        spec.targetKind,
        spec.state,
        spec.releaseDigest,
        spec.health?.state ?? "not-observed",
      ].some((value) => value.toLocaleLowerCase().includes(search)),
  );
}

export function filterAdminMaintenanceOperations(
  operations: readonly MaintenanceOperation[],
  query: string,
  failedOnly: boolean,
): readonly MaintenanceOperation[] {
  const search = query.trim().toLocaleLowerCase();
  return operations
    .filter(
      (operation) =>
        (!failedOnly || operation.state === "failed") &&
        [
          operation.operationId,
          operation.action,
          operation.resourceKind,
          operation.resourceId,
          operation.state,
          operation.currentStep,
          operation.requestId,
          operation.stableErrorCode ?? "",
        ].some((value) => value.toLocaleLowerCase().includes(search)),
    )
    .sort(
      (left, right) =>
        Date.parse(right.updatedAt) - Date.parse(left.updatedAt) ||
        (left.operationId < right.operationId ? -1 : left.operationId > right.operationId ? 1 : 0),
    );
}

export function filterAdminLeases(
  leases: readonly EnvironmentLease[],
  query: string,
  attentionOnly: boolean,
  observedPhase: EnvironmentLease["spec"]["observedPhase"] | "" = "",
  cleanupBlockedOnly = false,
): readonly EnvironmentLease[] {
  const search = query.trim().toLocaleLowerCase();
  return leases.filter(
    (lease) =>
      (!attentionOnly || leaseNeedsAttention(lease)) &&
      (observedPhase === "" || lease.spec.observedPhase === observedPhase) &&
      (!cleanupBlockedOnly || lease.spec.cleanupPhase === "blocked") &&
      [
        lease.metadata.uid,
        lease.metadata.name,
        lease.spec.environmentId,
        lease.spec.observedPhase,
        lease.spec.cleanupPhase,
      ].some((value) => value.toLocaleLowerCase().includes(search)),
  );
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

export const targetPageSizes = [10, 25, 50, 100, 200] as const;
// HTML pattern uses the v flag; escape the hyphen while matching SDK identifiers.
export const targetIdentifierPattern = String.raw`[A-Za-z0-9](?:[A-Za-z0-9._~\-]{0,126}[A-Za-z0-9])?`;

export function pageAdminTargets(
  targets: readonly DeploymentTarget[],
  requestedPage: number,
  pageSize: number,
) {
  const size = targetPageSizes.find((value) => value === pageSize) ?? 25;
  const count = Math.max(1, Math.ceil(targets.length / size));
  const index = Math.min(
    count - 1,
    Math.max(0, Number.isFinite(requestedPage) ? Math.floor(requestedPage) : 0),
  );
  return {
    index,
    count,
    size,
    items: targets.slice(index * size, (index + 1) * size),
  };
}

export async function listAdminTargets(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly DeploymentTarget[]> {
  const targets = await collectAdminPages<DeploymentTarget>(
    (pageToken) =>
      client
        .listAdminDeploymentTargets(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.deploymentTargets,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.targetPageToken"),
  );
  return Object.freeze(
    targets.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminLeases(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentLease[]> {
  const leases = await collectAdminPages<EnvironmentLease>(
    (pageToken) =>
      client
        .listAdminEnvironmentLeases(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.environmentLeases,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.leasePageToken"),
  );
  return Object.freeze(
    leases.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminWorkers(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly Worker[]> {
  const workers = await collectAdminPages<Worker>(
    (pageToken) =>
      client
        .listAdminWorkers(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({ items: page.value.workers, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.workerPageToken"),
  );
  return Object.freeze(
    workers.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminRemoteWorkerEnrollments(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly RemoteWorkerEnrollment[]> {
  const enrollments = await collectAdminPages<RemoteWorkerEnrollment>(
    (pageToken) =>
      client
        .listAdminRemoteWorkerEnrollments(
          tenantId,
          projectId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({
          items: page.value.remoteWorkerEnrollments,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.remoteWorkerEnrollmentPageToken"),
  );
  return Object.freeze(
    enrollments.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminRemoteWorkerEnrollmentAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  enrollmentId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events = await collectAdminPages<AdminAuditEvent>(
    (pageToken) =>
      client
        .listAdminRemoteWorkerEnrollmentAuditEvents(
          tenantId,
          projectId,
          enrollmentId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({ items: page.value.events, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.remoteWorkerEnrollmentAuditPageToken"),
  );
  return Object.freeze(events);
}

export async function listAdminRemoteWorkerOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  enrollmentId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  const operations = await collectAdminPages<MaintenanceOperation>(
    (pageToken) =>
      client
        .listAdminRemoteWorkerOperations(
          tenantId,
          projectId,
          enrollmentId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({
          items: page.value.operations,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.operationPageToken"),
  );
  return Object.freeze(operations);
}

export async function listAdminReleases(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly WorkerRelease[]> {
  const releases = await collectAdminPages<WorkerRelease>(
    (pageToken) =>
      client
        .listAdminWorkerReleases(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.workerReleases,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.releasePageToken"),
  );
  return Object.freeze(
    releases.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export function summarizeClusterHosts(
  targets: readonly DeploymentTarget[],
  workers: readonly Worker[],
): readonly ClusterHostSummary[] {
  const summaries = new Map(
    targets.map((target) => [
      target.metadata.uid,
      {
        target,
        workerCount: 0,
        readyWorkerCount: 0,
        latestHealthAt: undefined as string | undefined,
      },
    ]),
  );
  for (const worker of workers) {
    const summary = summaries.get(worker.spec.targetId);
    if (summary === undefined) continue;
    summary.workerCount += 1;
    if (worker.spec.state === "ready") summary.readyWorkerCount += 1;
    if (
      worker.spec.lastHealthAt !== undefined &&
      (summary.latestHealthAt === undefined ||
        Date.parse(worker.spec.lastHealthAt) > Date.parse(summary.latestHealthAt))
    )
      summary.latestHealthAt = worker.spec.lastHealthAt;
  }
  return Object.freeze([...summaries.values()].map((summary) => Object.freeze(summary)));
}

export async function listAdminProfiles(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentProfile[]> {
  const profiles = await collectAdminPages<EnvironmentProfile>(
    (pageToken) =>
      client
        .listAdminEnvironmentProfiles(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.environmentProfiles,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.profilePageToken"),
  );
  return Object.freeze(
    profiles.toSorted(
      (left, right) =>
        left.metadata.name.localeCompare(right.metadata.name) ||
        right.spec.version - left.spec.version,
    ),
  );
}

export async function listAdminRuntimeProfiles(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly RuntimeProfile[]> {
  const profiles = await collectAdminPages<RuntimeProfile>(
    (pageToken) =>
      client
        .listAdminRuntimeProfiles(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.runtimeProfiles,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.runtimeProfilePageToken"),
  );
  return Object.freeze(
    profiles.toSorted(
      (left, right) =>
        left.metadata.name.localeCompare(right.metadata.name) ||
        right.spec.version - left.spec.version,
    ),
  );
}

export async function listAdminSandboxes(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly AdminSandboxSession[]> {
  const sandboxes = await collectAdminPages<AdminSandboxSession>(
    (pageToken) =>
      client
        .listAdminSandboxSessions(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.sandboxSessions,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.sandboxPageToken"),
  );
  return Object.freeze(
    sandboxes.toSorted((left, right) =>
      (right.metadata.updatedAt ?? right.metadata.createdAt).localeCompare(
        left.metadata.updatedAt ?? left.metadata.createdAt,
      ),
    ),
  );
}

export async function listAdminSandboxAccessGrants(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  sandboxId: string,
  signal: AbortSignal,
): Promise<readonly AdminSandboxAccessGrant[]> {
  const grants = await collectAdminPages<AdminSandboxAccessGrant>(
    (pageToken) =>
      client
        .listAdminSandboxAccessGrants(
          tenantId,
          projectId,
          sandboxId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({
          items: page.value.accessGrants,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.sandboxGrantPageToken"),
  );
  return Object.freeze(
    grants.toSorted((left, right) =>
      right.metadata.createdAt.localeCompare(left.metadata.createdAt),
    ),
  );
}

export async function listAdminStoragePolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly StoragePolicy[]> {
  const policies = await collectAdminPages<StoragePolicy>(
    (pageToken) =>
      client
        .listAdminStoragePolicies(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.storagePolicies,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.storagePolicyPageToken"),
  );
  return Object.freeze(
    policies.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminWorkspaceSnapshots(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly WorkspaceSnapshot[]> {
  const snapshots = await collectAdminPages<WorkspaceSnapshot>(
    (pageToken) =>
      client
        .listAdminWorkspaceSnapshots(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.workspaceSnapshots,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.workspaceSnapshotPageToken"),
  );
  return Object.freeze(
    snapshots.toSorted((left, right) =>
      right.metadata.createdAt.localeCompare(left.metadata.createdAt),
    ),
  );
}

export async function listAdminStoragePolicyAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  policyId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events = await collectAdminPages<AdminAuditEvent>(
    (pageToken) =>
      client
        .listAdminStoragePolicyAuditEvents(
          tenantId,
          projectId,
          policyId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({ items: page.value.events, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.auditPageToken"),
  );
  return Object.freeze(events);
}

export async function listAdminNetworkPolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly NetworkPolicy[]> {
  const policies = await collectAdminPages<NetworkPolicy>(
    (pageToken) =>
      client
        .listAdminNetworkPolicies(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.networkPolicies,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.networkPolicyPageToken"),
  );
  return Object.freeze(
    policies.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminNetworkPolicyAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  policyId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events = await collectAdminPages<AdminAuditEvent>(
    (pageToken) =>
      client
        .listAdminNetworkPolicyAuditEvents(
          tenantId,
          projectId,
          policyId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({ items: page.value.events, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.auditPageToken"),
  );
  return Object.freeze(events);
}

export async function loadAdminProjectLeaseQuota(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<ProjectLeaseQuota | undefined> {
  try {
    return (await client.getAdminProjectLeaseQuota(tenantId, projectId, newRequestId(), signal))
      .value;
  } catch (error) {
    if (error instanceof ClientError && error.status === 404) return undefined;
    throw error;
  }
}

export async function listAdminProjectLeaseQuotaAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events: AdminAuditEvent[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  try {
    do {
      const page = await client.listAdminProjectLeaseQuotaAuditEvents(
        tenantId,
        projectId,
        newRequestId(),
        200,
        pageToken,
        signal,
      );
      events.push(...page.value.events);
      pageToken = page.value.nextPageToken;
      if (pageToken !== undefined) {
        if (seenTokens.has(pageToken)) throw new AdminUIError("error.auditPageToken");
        seenTokens.add(pageToken);
      }
    } while (pageToken !== undefined);
  } catch (error) {
    if (events.length === 0 && error instanceof ClientError && error.status === 404)
      return Object.freeze([]);
    throw error;
  }
  return Object.freeze(events);
}

export async function listAdminProfileAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  profileId: string,
  version: number,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events = await collectAdminPages<AdminAuditEvent>(
    (pageToken) =>
      client
        .listAdminEnvironmentProfileAuditEvents(
          tenantId,
          projectId,
          profileId,
          version,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({ items: page.value.events, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.auditPageToken"),
  );
  return Object.freeze(events);
}

export async function listAdminTargetOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  const operations = await collectAdminPages<MaintenanceOperation>(
    (pageToken) =>
      client
        .listAdminDeploymentTargetOperations(
          tenantId,
          projectId,
          targetId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({
          items: page.value.operations,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.operationPageToken"),
  );
  return Object.freeze(operations);
}

export async function listAdminMaintenanceOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  const operations = await collectAdminPages<MaintenanceOperation>(
    (pageToken) =>
      client
        .listAdminMaintenanceOperations(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.operations,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.operationPageToken"),
  );
  return Object.freeze(operations);
}

export async function listAdminTargetAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events = await collectAdminPages<AdminAuditEvent>(
    (pageToken) =>
      client
        .listAdminDeploymentTargetAuditEvents(
          tenantId,
          projectId,
          targetId,
          newRequestId(),
          200,
          pageToken,
          signal,
        )
        .then((page) => ({ items: page.value.events, nextPageToken: page.value.nextPageToken })),
    () => new AdminUIError("error.auditPageToken"),
  );
  return Object.freeze(events);
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
