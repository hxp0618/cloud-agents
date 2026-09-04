import {
  ClientError,
  JSONContractError,
  type Client,
  type EnvironmentProfileSummary,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { recordPageToken } from "./pagination";

export type EnvironmentClient = Pick<
  Client,
  "listEnvironmentProfiles" | "createEnvironment" | "getEnvironment"
>;

export type EnvironmentSelection = Readonly<{
  tenantId: string;
  projectId: string;
  profileId: string;
  profileVersion: number;
  environmentId: string;
}>;

type SelectionStorage = Pick<Storage, "getItem" | "setItem">;

const selectionStorageKey = "cloud-agents.user-web.environment.v1";

export function newRequestId(): string {
  return `web-${crypto.randomUUID()}`;
}

export function newIdempotencyKey(): string {
  return `web-${crypto.randomUUID()}`;
}

export async function loadEnvironmentProfiles(
  client: EnvironmentClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly EnvironmentProfileSummary[]> {
  const profiles: EnvironmentProfileSummary[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listEnvironmentProfiles(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    profiles.push(...page.value.environmentProfiles);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "published Profile");
  } while (pageToken !== undefined);
  return Object.freeze(
    profiles.toSorted(
      (left, right) =>
        left.name.localeCompare(right.name, undefined, { sensitivity: "base" }) ||
        right.version - left.version,
    ),
  );
}

export function readEnvironmentSelection(
  storage: SelectionStorage,
  tenantId: string,
  projectId: string,
): EnvironmentSelection {
  const empty = Object.freeze({
    tenantId,
    projectId,
    profileId: "",
    profileVersion: 0,
    environmentId: "",
  });
  try {
    const raw = storage.getItem(selectionStorageKey);
    if (raw === null) return empty;
    const value = JSON.parse(raw) as unknown;
    if (typeof value !== "object" || value === null || Array.isArray(value)) return empty;
    const candidate = value as Record<string, unknown>;
    if (
      candidate.tenantId !== tenantId ||
      candidate.projectId !== projectId ||
      typeof candidate.profileId !== "string" ||
      !Number.isInteger(candidate.profileVersion) ||
      typeof candidate.profileVersion !== "number" ||
      typeof candidate.environmentId !== "string" ||
      candidate.profileId.length > 128 ||
      candidate.profileVersion < 0 ||
      candidate.profileVersion > 2_147_483_647 ||
      candidate.environmentId.length > 128
    )
      return empty;
    return Object.freeze({
      tenantId,
      projectId,
      profileId: candidate.profileId,
      profileVersion: candidate.profileVersion,
      environmentId: candidate.environmentId,
    });
  } catch {
    return empty;
  }
}

export function writeEnvironmentSelection(
  storage: SelectionStorage,
  selection: EnvironmentSelection,
): void {
  try {
    storage.setItem(selectionStorageKey, JSON.stringify(selection));
  } catch {
    // Recovery is optional when browser storage is unavailable.
  }
}

export function environmentErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "The connection token expired or was rejected. Disconnect and authenticate again.";
  if (error instanceof ClientError && error.status === 403)
    return "This token cannot use environments in the selected project.";
  if (error instanceof ClientError && error.status === 404)
    return "The selected Profile or environment no longer exists. Refresh available Profiles.";
  if (error instanceof ClientError && error.status === 409)
    return "The Profile is no longer available or the environment conflicts with current state. Refresh and retry.";
  if (error instanceof JSONContractError)
    return "Control Plane returned an environment response outside the Platform API contract.";
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "The environment request timed out. Refresh before retrying because preparation may still be running.";
  if (error instanceof DOMException && error.name === "AbortError")
    return "The environment request was cancelled.";
  return "The environment operation failed. Refresh server state, then retry.";
}
