import {
  ClientError,
  JSONContractError,
  type Client,
  type AdminAuditEvent,
  type DeploymentTarget,
  type DeploymentTargetCleanupPreview,
  type DeploymentTargetCleanupRequest,
  type EnvironmentLease,
  type EnvironmentProfile,
  type MaintenanceOperation,
  type Worker,
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
  | "listAdminEnvironmentLeases"
  | "listAdminWorkers"
  | "getAdminEnvironmentLease"
  | "listAdminEnvironmentProfiles"
  | "createAdminEnvironmentProfile"
  | "publishAdminEnvironmentProfile"
  | "disableAdminEnvironmentProfile"
  | "getAdminEnvironmentProfile"
  | "listAdminEnvironmentProfileAuditEvents"
>;

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

export function cleanupRequestFromPreview(
  preview: DeploymentTargetCleanupPreview,
): DeploymentTargetCleanupRequest {
  return Object.freeze({
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
