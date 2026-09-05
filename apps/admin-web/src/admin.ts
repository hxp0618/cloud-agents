import {
  ClientError,
  JSONContractError,
  parseProblem,
  type Client,
  type AdminEnvironmentLeaseUpgradeRequest,
  type AdminAuditEvent,
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
  type StoragePolicy,
  type NetworkPolicy,
  type Worker,
  type WorkerRelease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import type { MessageKey } from "./i18n";

export type AdminClient = Pick<
  Client,
  | "listAdminDeploymentTargets"
  | "listAdminDeploymentTargetOperations"
  | "listAdminMaintenanceOperations"
  | "listAdminDeploymentTargetAuditEvents"
  | "registerAdminDeploymentTarget"
  | "getAdminDeploymentTarget"
  | "probeAdminDeploymentTarget"
  | "previewAdminDeploymentTargetCleanup"
  | "cleanupAdminDeploymentTarget"
  | "previewAdminDeploymentTargetScheduling"
  | "transitionAdminDeploymentTargetScheduling"
  | "listAdminEnvironmentLeases"
  | "listAdminWorkers"
  | "listAdminWorkerReleases"
  | "registerAdminWorkerRelease"
  | "getAdminEnvironmentLease"
  | "previewAdminEnvironmentLeaseUpgrade"
  | "previewAdminEnvironmentLeaseRollback"
  | "upgradeAdminEnvironmentLease"
  | "rollbackAdminEnvironmentLease"
  | "listAdminEnvironmentProfiles"
  | "createAdminEnvironmentProfile"
  | "publishAdminEnvironmentProfile"
  | "disableAdminEnvironmentProfile"
  | "getAdminEnvironmentProfile"
  | "listAdminEnvironmentProfileAuditEvents"
  | "getAdminProjectLeaseQuota"
  | "setAdminProjectLeaseQuota"
  | "listAdminProjectLeaseQuotaAuditEvents"
  | "listAdminStoragePolicies"
  | "getAdminStoragePolicy"
  | "setAdminStoragePolicy"
  | "listAdminStoragePolicyAuditEvents"
  | "listAdminNetworkPolicies"
  | "getAdminNetworkPolicy"
  | "setAdminNetworkPolicy"
  | "listAdminNetworkPolicyAuditEvents"
>;

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

class AdminUIError extends Error {
  constructor(readonly messageKey: MessageKey) {
    super(messageKey);
  }
}

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

export function newRequestId(): string {
  return `admin-${crypto.randomUUID()}`;
}

export function newIdempotencyKey(): string {
  return `admin-${crypto.randomUUID()}`;
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

export async function listAdminTargets(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly DeploymentTarget[]> {
  const targets: DeploymentTarget[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminDeploymentTargets(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    targets.push(...page.value.deploymentTargets);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.targetPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
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
  const leases: EnvironmentLease[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminEnvironmentLeases(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    leases.push(...page.value.environmentLeases);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.leasePageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
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
  const workers: Worker[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminWorkers(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    workers.push(...page.value.workers);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.workerPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(
    workers.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminReleases(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly WorkerRelease[]> {
  const releases: WorkerRelease[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminWorkerReleases(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    releases.push(...page.value.workerReleases);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.releasePageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(
    releases.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export function replaceRelease(
  releases: readonly WorkerRelease[],
  release: WorkerRelease,
): readonly WorkerRelease[] {
  return Object.freeze(
    [...releases.filter(({ metadata }) => metadata.uid !== release.metadata.uid), release].toSorted(
      (left, right) => left.metadata.name.localeCompare(right.metadata.name),
    ),
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
  const profiles: EnvironmentProfile[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminEnvironmentProfiles(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    profiles.push(...page.value.environmentProfiles);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.profilePageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(
    profiles.toSorted(
      (left, right) =>
        left.metadata.name.localeCompare(right.metadata.name) ||
        right.spec.version - left.spec.version,
    ),
  );
}

export async function listAdminStoragePolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly StoragePolicy[]> {
  const policies: StoragePolicy[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminStoragePolicies(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    policies.push(...page.value.storagePolicies);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.storagePolicyPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(
    policies.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
}

export async function listAdminStoragePolicyAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  policyId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events: AdminAuditEvent[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminStoragePolicyAuditEvents(
      tenantId,
      projectId,
      policyId,
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
  return Object.freeze(events);
}

export function replaceStoragePolicy(
  policies: readonly StoragePolicy[],
  policy: StoragePolicy,
): readonly StoragePolicy[] {
  return Object.freeze(
    [...policies.filter(({ metadata }) => metadata.uid !== policy.metadata.uid), policy].toSorted(
      (left, right) => left.metadata.name.localeCompare(right.metadata.name),
    ),
  );
}
export async function listAdminNetworkPolicies(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly NetworkPolicy[]> {
  const policies: NetworkPolicy[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminNetworkPolicies(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    policies.push(...page.value.networkPolicies);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.networkPolicyPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
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
  const events: AdminAuditEvent[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminNetworkPolicyAuditEvents(
      tenantId,
      projectId,
      policyId,
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
  return Object.freeze(events);
}

export function replaceNetworkPolicy(
  policies: readonly NetworkPolicy[],
  policy: NetworkPolicy,
): readonly NetworkPolicy[] {
  return Object.freeze(
    [...policies.filter(({ metadata }) => metadata.uid !== policy.metadata.uid), policy].toSorted(
      (left, right) => left.metadata.name.localeCompare(right.metadata.name),
    ),
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
  const events: AdminAuditEvent[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminEnvironmentProfileAuditEvents(
      tenantId,
      projectId,
      profileId,
      version,
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
  return Object.freeze(events);
}

export async function listAdminTargetOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  const operations: MaintenanceOperation[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminDeploymentTargetOperations(
      tenantId,
      projectId,
      targetId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    operations.push(...page.value.operations);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.operationPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(operations);
}

export async function listAdminMaintenanceOperations(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly MaintenanceOperation[]> {
  const operations: MaintenanceOperation[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminMaintenanceOperations(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    operations.push(...page.value.operations);
    pageToken = page.value.nextPageToken;
    if (pageToken !== undefined) {
      if (seenTokens.has(pageToken)) throw new AdminUIError("error.operationPageToken");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(operations);
}

export async function listAdminTargetAuditEvents(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  targetId: string,
  signal: AbortSignal,
): Promise<readonly AdminAuditEvent[]> {
  const events: AdminAuditEvent[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listAdminDeploymentTargetAuditEvents(
      tenantId,
      projectId,
      targetId,
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
  return Object.freeze(events);
}

export function replaceTarget(
  targets: readonly DeploymentTarget[],
  target: DeploymentTarget,
): readonly DeploymentTarget[] {
  return Object.freeze(
    [...targets.filter(({ metadata }) => metadata.uid !== target.metadata.uid), target].toSorted(
      (left, right) => left.metadata.name.localeCompare(right.metadata.name),
    ),
  );
}

export function replaceLease(
  leases: readonly EnvironmentLease[],
  lease: EnvironmentLease,
): readonly EnvironmentLease[] {
  return Object.freeze(
    [...leases.filter(({ metadata }) => metadata.uid !== lease.metadata.uid), lease].toSorted(
      (left, right) => left.metadata.name.localeCompare(right.metadata.name),
    ),
  );
}

export function replaceProfile(
  profiles: readonly EnvironmentProfile[],
  profile: EnvironmentProfile,
): readonly EnvironmentProfile[] {
  return Object.freeze(
    [...profiles.filter(({ metadata }) => metadata.uid !== profile.metadata.uid), profile].toSorted(
      (left, right) =>
        left.metadata.name.localeCompare(right.metadata.name) ||
        right.spec.version - left.spec.version,
    ),
  );
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
