import {
  ClientError,
  JSONContractError,
  type BrowserSession,
  type BrowserSessionClient,
  type BrowserTenant,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { collectPages } from "./pagination";

export type SavedConnection = Readonly<{
  tenantId: string;
  projectId: string;
}>;

type ConnectionStorage = Pick<Storage, "getItem" | "setItem">;

const storageKey = "cloud-agents.user-web.connection.v1";
const emptyConnection: SavedConnection = Object.freeze({ tenantId: "", projectId: "" });

function requestId(): string {
  return `web-${crypto.randomUUID()}`;
}

export function readSavedConnection(storage: ConnectionStorage): SavedConnection {
  let connection = emptyConnection;
  try {
    const raw = storage.getItem(storageKey);
    if (raw !== null) {
      const value = JSON.parse(raw) as unknown;
      const candidate = value as Record<string, unknown>;
      if (
        typeof value === "object" &&
        value !== null &&
        !Array.isArray(value) &&
        typeof candidate.tenantId === "string" &&
        typeof candidate.projectId === "string" &&
        candidate.tenantId.length <= 128 &&
        candidate.projectId.length <= 128
      )
        connection = Object.freeze({
          tenantId: candidate.tenantId,
          projectId: candidate.projectId,
        });
    }
  } catch {}
  writeSavedConnection(storage, connection);
  return connection;
}

export function writeSavedConnection(
  storage: ConnectionStorage,
  connection: SavedConnection,
): void {
  try {
    storage.setItem(storageKey, JSON.stringify(connection));
  } catch {
    // The active session remains usable when hardened browser storage is unavailable.
  }
}

export async function listAllBrowserTenants(
  sessionClient: Pick<BrowserSessionClient, "listBrowserTenants">,
  session: BrowserSession,
  signal: AbortSignal,
): Promise<readonly BrowserTenant[]> {
  const tenants = [...session.tenants];
  const tenantIds = new Set(tenants.map(({ id }) => id));
  let pageToken = session.nextPageToken;
  const seenTokens = new Set<string>();
  while (pageToken !== undefined && pageToken !== "") {
    if (seenTokens.has(pageToken)) throw new Error("Tenant pagination repeated a cursor.");
    seenTokens.add(pageToken);
    const page = await sessionClient.listBrowserTenants(200, pageToken, signal);
    for (const tenant of page.tenants) {
      if (tenantIds.has(tenant.id)) throw new Error("Tenant pagination repeated a tenant.");
      tenantIds.add(tenant.id);
      tenants.push(tenant);
    }
    pageToken = page.nextPageToken;
  }
  return Object.freeze(tenants);
}

export async function loadUserProjects(
  client: Pick<Client, "listMyProjects">,
  tenantId: string,
  signal: AbortSignal,
): Promise<readonly Project[]> {
  const projects = await collectPages<Project>(async (pageToken) => {
    const { value } = await client.listMyProjects(tenantId, requestId(), 200, pageToken, signal);
    return { items: value.projects, nextPageToken: value.nextPageToken };
  }, "project");
  return Object.freeze(
    projects
      .filter(({ spec }) => spec.state === "active")
      .toSorted(
        (left, right) =>
          left.spec.displayName.localeCompare(right.spec.displayName) ||
          left.metadata.uid.localeCompare(right.metadata.uid),
      ),
  );
}

export function sessionErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "Your session is no longer active. Sign in again.";
  if (error instanceof ClientError && error.status === 403)
    return "This account cannot use the selected tenant.";
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "Cloud Agents did not respond within 15 seconds.";
  if (error instanceof JSONContractError)
    return "Cloud Agents returned a response that does not match the Platform API contract.";
  return "Cloud Agents could not load your account. Refresh and try again.";
}
