import {
  ClientError,
  JSONContractError,
  type Client,
  type Organization,
  type PlatformTenant,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { recordPageToken } from "./pagination";

export type SavedConnection = Readonly<{
  endpoint: string;
  tenantId: string;
  projectId: string;
}>;

export type ConnectionData = Readonly<{
  tenant: PlatformTenant;
  projects: readonly Project[];
}>;

type ConnectionClient = Pick<Client, "getPlatformTenant" | "listOrganizations" | "listProjects">;
type ConnectionStorage = Pick<Storage, "getItem" | "setItem">;

const storageKey = "cloud-agents.user-web.connection.v1";
const emptyConnection: SavedConnection = Object.freeze({
  endpoint: "",
  tenantId: "",
  projectId: "",
});

function requestId(): string {
  return `web-${crypto.randomUUID()}`;
}

export function readSavedConnection(storage: ConnectionStorage): SavedConnection {
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

export function writeSavedConnection(
  storage: ConnectionStorage,
  connection: SavedConnection,
): void {
  try {
    storage.setItem(storageKey, JSON.stringify(connection));
  } catch {
    // Storage can be unavailable in hardened browser contexts; the live connection still works.
  }
}

async function listOrganizations(
  client: ConnectionClient,
  tenantId: string,
  signal: AbortSignal,
): Promise<readonly Organization[]> {
  const organizations: Organization[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listOrganizations(tenantId, requestId(), 200, pageToken, signal);
    organizations.push(...page.value.organizations);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "organization");
  } while (pageToken !== undefined);
  return organizations;
}

async function listProjects(
  client: ConnectionClient,
  tenantId: string,
  organizationId: string,
  signal: AbortSignal,
): Promise<readonly Project[]> {
  const projects: Project[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listProjects(
      tenantId,
      organizationId,
      requestId(),
      200,
      pageToken,
      signal,
    );
    projects.push(...page.value.projects);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "project");
  } while (pageToken !== undefined);
  return projects;
}

export async function loadConnectionData(
  client: ConnectionClient,
  tenantId: string,
  signal: AbortSignal,
): Promise<ConnectionData> {
  const [tenant, organizations] = await Promise.all([
    client.getPlatformTenant(tenantId, requestId(), signal),
    listOrganizations(client, tenantId, signal),
  ]);
  const projectGroups = await Promise.all(
    organizations.map(({ metadata }) => listProjects(client, tenantId, metadata.uid, signal)),
  );
  const projects = projectGroups.flat().toSorted((left, right) =>
    left.spec.displayName.localeCompare(right.spec.displayName, undefined, {
      sensitivity: "base",
    }),
  );
  return Object.freeze({ tenant: tenant.value, projects: Object.freeze(projects) });
}

export function connectionErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "Control Plane rejected the token. Enter a valid token and reconnect.";
  if (error instanceof ClientError && error.status === 403)
    return "The token is valid but cannot access this tenant or its projects.";
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "Control Plane did not respond within 15 seconds.";
  if (error instanceof DOMException && error.name === "AbortError")
    return "Connection was cancelled.";
  if (error instanceof JSONContractError)
    return "Control Plane returned a response that does not match the Platform API contract.";
  if (error instanceof TypeError)
    return "Endpoint or token format is invalid. Use HTTPS, or loopback HTTP for local development.";
  return "Unable to connect to Control Plane. Check the endpoint and try again.";
}
