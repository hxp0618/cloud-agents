import {
  ClientError,
  type AdminAuditEvent,
  type AdminSandboxAccessGrant,
  type AdminSandboxSession,
  type DeploymentTarget,
  type EnvironmentLease,
  type EnvironmentProfile,
  type MaintenanceOperation,
  type NetworkPolicy,
  type ProjectLeaseQuota,
  type RemoteWorkerEnrollment,
  type RuntimeProfile,
  type StoragePolicy,
  type Worker,
  type WorkerRelease,
  type WorkspaceSnapshot,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import type { AdminClient } from "./client";
import { AdminUIError } from "./errors";
import { collectAdminPages } from "./pagination";
import { newRequestId } from "./request";

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
