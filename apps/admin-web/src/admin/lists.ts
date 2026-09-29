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

import type { MessageKey } from "../i18n";
import type { AdminClient } from "./client";
import { AdminUIError } from "./errors";
import { collectAdminPages } from "./pagination";
import { newRequestId } from "./request";

const PAGE_SIZE = 200;

type ListResponse<K extends string, T> = Promise<
  Readonly<{ value: Readonly<Record<K, readonly T[]> & { nextPageToken?: string }> }>
>;

/** Loads every page of one Admin list endpoint and returns a frozen, optionally sorted copy. */
async function listAll<T, K extends string>(
  load: (requestId: string, pageSize: number, pageToken: string | undefined) => ListResponse<K, T>,
  field: K,
  pageTokenError: MessageKey,
  compare?: (left: T, right: T) => number,
): Promise<readonly T[]> {
  const items = await collectAdminPages<T>(
    (pageToken) =>
      load(newRequestId(), PAGE_SIZE, pageToken).then(({ value }) => ({
        items: value[field],
        nextPageToken: value.nextPageToken,
      })),
    () => new AdminUIError(pageTokenError),
  );
  return Object.freeze(compare === undefined ? items : items.toSorted(compare));
}

const byName = (
  left: { metadata: { name: string } },
  right: { metadata: { name: string } },
): number => left.metadata.name.localeCompare(right.metadata.name);

const byNameThenNewestVersion = (
  left: { metadata: { name: string }; spec: { version: number } },
  right: { metadata: { name: string }; spec: { version: number } },
): number => byName(left, right) || right.spec.version - left.spec.version;

const newestCreatedFirst = (
  left: { metadata: { createdAt: string } },
  right: { metadata: { createdAt: string } },
): number => right.metadata.createdAt.localeCompare(left.metadata.createdAt);

export function listAdminTargets(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly DeploymentTarget[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminDeploymentTargets(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "deploymentTargets",
    "error.targetPageToken",
    byName,
  );
}

export function listAdminLeases(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentLease[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminEnvironmentLeases(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "environmentLeases",
    "error.leasePageToken",
    byName,
  );
}

export function listAdminWorkers(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly Worker[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminWorkers(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "workers",
    "error.workerPageToken",
    byName,
  );
}

export function listAdminRemoteWorkerEnrollments(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly RemoteWorkerEnrollment[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminRemoteWorkerEnrollments(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "remoteWorkerEnrollments",
    "error.remoteWorkerEnrollmentPageToken",
    byName,
  );
}

export function listAdminRemoteWorkerEnrollmentAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  enrollmentId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminRemoteWorkerEnrollmentAuditEvents(
        tenantId,
        projectId,
        enrollmentId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "events",
    "error.remoteWorkerEnrollmentAuditPageToken",
  );
}

export function listAdminRemoteWorkerOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  enrollmentId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminRemoteWorkerOperations(
        tenantId,
        projectId,
        enrollmentId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "operations",
    "error.operationPageToken",
  );
}

export function listAdminReleases(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly WorkerRelease[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminWorkerReleases(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "workerReleases",
    "error.releasePageToken",
    byName,
  );
}

export function listAdminProfiles(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentProfile[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminEnvironmentProfiles(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "environmentProfiles",
    "error.profilePageToken",
    byNameThenNewestVersion,
  );
}

export function listAdminRuntimeProfiles(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly RuntimeProfile[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminRuntimeProfiles(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "runtimeProfiles",
    "error.runtimeProfilePageToken",
    byNameThenNewestVersion,
  );
}

export function listAdminSandboxes(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly AdminSandboxSession[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminSandboxSessions(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "sandboxSessions",
    "error.sandboxPageToken",
    (left, right) =>
      (right.metadata.updatedAt ?? right.metadata.createdAt).localeCompare(
        left.metadata.updatedAt ?? left.metadata.createdAt,
      ),
  );
}

export function listAdminSandboxAccessGrants(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  sandboxId: string,
  signal: AbortSignal,
): Promise<readonly AdminSandboxAccessGrant[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminSandboxAccessGrants(
        tenantId,
        projectId,
        sandboxId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "accessGrants",
    "error.sandboxGrantPageToken",
    newestCreatedFirst,
  );
}

export function listAdminStoragePolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly StoragePolicy[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminStoragePolicies(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "storagePolicies",
    "error.storagePolicyPageToken",
    byName,
  );
}

export function listAdminWorkspaceSnapshots(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly WorkspaceSnapshot[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminWorkspaceSnapshots(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "workspaceSnapshots",
    "error.workspaceSnapshotPageToken",
    newestCreatedFirst,
  );
}

export function listAdminStoragePolicyAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  policyId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminStoragePolicyAuditEvents(
        tenantId,
        projectId,
        policyId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "events",
    "error.auditPageToken",
  );
}

export function listAdminNetworkPolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly NetworkPolicy[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminNetworkPolicies(tenantId, projectId, requestId, pageSize, pageToken, signal),
    "networkPolicies",
    "error.networkPolicyPageToken",
    byName,
  );
}

export function listAdminNetworkPolicyAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  policyId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminNetworkPolicyAuditEvents(
        tenantId,
        projectId,
        policyId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "events",
    "error.auditPageToken",
  );
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
  let loadedAny = false;
  try {
    return await listAll(
      (requestId, pageSize, pageToken) =>
        client
          .listAdminProjectLeaseQuotaAuditEvents(
            tenantId,
            projectId,
            requestId,
            pageSize,
            pageToken,
            signal,
          )
          .then((page) => {
            loadedAny ||= page.value.events.length > 0;
            return page;
          }),
      "events",
      "error.auditPageToken",
    );
  } catch (error) {
    if (!loadedAny && error instanceof ClientError && error.status === 404)
      return Object.freeze([]);
    throw error;
  }
}

export function listAdminProfileAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  profileId: string,
  version: number,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminEnvironmentProfileAuditEvents(
        tenantId,
        projectId,
        profileId,
        version,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "events",
    "error.auditPageToken",
  );
}

export function listAdminTargetOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminDeploymentTargetOperations(
        tenantId,
        projectId,
        targetId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "operations",
    "error.operationPageToken",
  );
}

export function listAdminMaintenanceOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminMaintenanceOperations(
        tenantId,
        projectId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "operations",
    "error.operationPageToken",
  );
}

export function listAdminTargetAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  return listAll(
    (requestId, pageSize, pageToken) =>
      client.listAdminDeploymentTargetAuditEvents(
        tenantId,
        projectId,
        targetId,
        requestId,
        pageSize,
        pageToken,
        signal,
      ),
    "events",
    "error.auditPageToken",
  );
}
