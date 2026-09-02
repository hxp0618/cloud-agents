import {
  ClientError,
  JSONContractError,
  type Client,
  type DeploymentTarget,
  type EnvironmentLease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { recordPageToken } from "./pagination";

export type InfrastructureClient = Pick<
  Client,
  | "listDeploymentTargets"
  | "registerDeploymentTarget"
  | "getDeploymentTarget"
  | "probeDeploymentTarget"
  | "cleanupDeploymentTarget"
  | "listManagedHostEnvironmentLeases"
  | "createManagedHostEnvironmentLease"
  | "getManagedHostEnvironmentLease"
  | "upgradeManagedHostEnvironmentLease"
  | "terminateManagedHostEnvironmentLease"
>;

export type InfrastructureResources = Readonly<{
  targets: readonly DeploymentTarget[];
  leases: readonly EnvironmentLease[];
}>;

export type InfrastructureSelection = Readonly<{
  tenantId: string;
  projectId: string;
  targetId: string;
  leaseId: string;
}>;

type SelectionStorage = Pick<Storage, "getItem" | "setItem">;

const selectionStorageKey = "cloud-agents.user-web.infrastructure.v1";

export function newRequestId(): string {
  return `web-${crypto.randomUUID()}`;
}

export function newIdempotencyKey(): string {
  return `web-${crypto.randomUUID()}`;
}

async function listTargets(
  client: InfrastructureClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly DeploymentTarget[]> {
  const targets: DeploymentTarget[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listDeploymentTargets(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    targets.push(...page.value.deploymentTargets);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "deployment target");
  } while (pageToken !== undefined);
  return targets.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name));
}

async function listLeases(
  client: InfrastructureClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentLease[]> {
  const leases: EnvironmentLease[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listManagedHostEnvironmentLeases(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    leases.push(...page.value.environmentLeases);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "environment lease");
  } while (pageToken !== undefined);
  return leases.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name));
}

export async function loadInfrastructure(
  client: InfrastructureClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<InfrastructureResources> {
  const [targets, leases] = await Promise.all([
    listTargets(client, tenantId, projectId, signal),
    listLeases(client, tenantId, projectId, signal),
  ]);
  return Object.freeze({ targets: Object.freeze(targets), leases: Object.freeze(leases) });
}

export function readInfrastructureSelection(
  storage: SelectionStorage,
  tenantId: string,
  projectId: string,
): InfrastructureSelection {
  const empty = Object.freeze({ tenantId, projectId, targetId: "", leaseId: "" });
  try {
    const raw = storage.getItem(selectionStorageKey);
    if (raw === null) return empty;
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== "object" || value === null || Array.isArray(value)) return empty;
    const candidate = value as Record<string, unknown>;
    if (
      candidate.tenantId !== tenantId ||
      candidate.projectId !== projectId ||
      typeof candidate.targetId !== "string" ||
      typeof candidate.leaseId !== "string" ||
      candidate.targetId.length > 128 ||
      candidate.leaseId.length > 128
    )
      return empty;
    return Object.freeze({
      tenantId,
      projectId,
      targetId: candidate.targetId,
      leaseId: candidate.leaseId,
    });
  } catch {
    return empty;
  }
}

export function writeInfrastructureSelection(
  storage: SelectionStorage,
  selection: InfrastructureSelection,
): void {
  try {
    storage.setItem(selectionStorageKey, JSON.stringify(selection));
  } catch {
    // Resource selection recovery is optional when browser storage is unavailable.
  }
}

export function infrastructureErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "The connection token expired or was rejected. Disconnect and authenticate again.";
  if (error instanceof ClientError && error.status === 403)
    return "This token cannot perform the requested project operation.";
  if (error instanceof ClientError && error.status === 404)
    return "The selected resource no longer exists. Refresh the project state.";
  if (error instanceof ClientError && error.status === 409)
    return "The resource generation changed or the operation conflicts with current state. Refresh and retry.";
  if (error instanceof ClientError && error.status === 400)
    return "Control Plane rejected the request fields. Check identifiers, generations, and limits.";
  if (error instanceof ClientError && (error.status === 502 || error.status === 503))
    return "The target actuator is unavailable. The resource state is retained; retry after checking the target.";
  if (error instanceof JSONContractError)
    return "Control Plane returned a response outside the Platform API contract.";
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "The operation timed out. Refresh before retrying because Control Plane may still have completed it.";
  if (error instanceof DOMException && error.name === "AbortError")
    return "The operation was cancelled.";
  return "The infrastructure operation failed. Refresh server state, then retry safely.";
}
