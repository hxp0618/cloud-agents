import {
  ClientError,
  JSONContractError,
  type Client,
  type DeploymentTarget,
  type EnvironmentLease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

export type AdminClient = Pick<
  Client,
  | "listAdminDeploymentTargets"
  | "registerAdminDeploymentTarget"
  | "getAdminDeploymentTarget"
  | "probeAdminDeploymentTarget"
  | "previewAdminDeploymentTargetCleanup"
  | "listAdminEnvironmentLeases"
  | "getAdminEnvironmentLease"
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
      if (seenTokens.has(pageToken)) throw new Error("Control Plane repeated a target page token.");
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
      if (seenTokens.has(pageToken)) throw new Error("Control Plane repeated a lease page token.");
      seenTokens.add(pageToken);
    }
  } while (pageToken !== undefined);
  return Object.freeze(
    leases.toSorted((left, right) => left.metadata.name.localeCompare(right.metadata.name)),
  );
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

export function adminErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "The admin token expired or was rejected. Disconnect and authenticate again.";
  if (error instanceof ClientError && error.status === 403)
    return "This token is valid but lacks the required Admin API scope or project authority.";
  if (error instanceof ClientError && error.status === 404)
    return "The selected resource no longer exists. Refresh the project authority.";
  if (error instanceof ClientError && error.status === 409)
    return "The target generation changed or the operation conflicts with current state. Refresh and retry.";
  if (error instanceof ClientError && error.status === 400)
    return "Control Plane rejected the request fields. Check identifiers and generation.";
  if (error instanceof ClientError && (error.status === 502 || error.status === 503))
    return "The target actuator is unavailable. Server state is retained; inspect the target and retry.";
  if (error instanceof JSONContractError)
    return "Control Plane returned a response outside the generated Admin API contract.";
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "The operation timed out. Refresh before retrying because it may have completed server-side.";
  if (error instanceof DOMException && error.name === "AbortError")
    return "The operation was cancelled.";
  if (error instanceof TypeError)
    return "The Control Plane endpoint or token format is invalid. Use HTTPS or loopback HTTP.";
  return "The Admin API operation failed. Refresh server state, then retry safely.";
}
