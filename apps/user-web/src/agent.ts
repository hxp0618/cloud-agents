import {
  ClientError,
  JSONContractError,
  type Client,
  type ManagedAgentEvent,
  type ManagedAgentExecution,
  type ManagedAgentExecutionMessage,
  type ManagedAgentSession,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import { newRequestId } from "./infrastructure";
import { recordPageToken } from "./pagination";

export type AgentClient = Pick<
  Client,
  | "createManagedAgentSession"
  | "closeManagedAgentSession"
  | "getManagedAgentSession"
  | "listManagedAgentSessions"
  | "createManagedAgentTurn"
  | "executeManagedAgent"
  | "getManagedAgentExecution"
  | "listManagedAgentExecutions"
  | "cancelManagedAgentExecution"
  | "interruptManagedAgentExecution"
  | "listManagedAgentEvents"
>;

export type AgentSelection = Readonly<{
  tenantId: string;
  projectId: string;
  sessionId: string;
  turnId: string;
  executionId: string;
  eventCursor: string;
}>;

export type AgentResources = Readonly<{
  sessions: readonly ManagedAgentSession[];
  session?: ManagedAgentSession;
  executions: readonly ManagedAgentExecution[];
  execution?: ManagedAgentExecution;
}>;

export type AgentEventBatch = Readonly<{
  events: readonly ManagedAgentEvent[];
  nextCursor: string;
  hasMore: boolean;
}>;

type SelectionStorage = Pick<Storage, "getItem" | "setItem">;

const selectionStorageKey = "cloud-agents.user-web.agent.v1";

export class AgentEventStreamError extends Error {}

function newestFirst<T extends { metadata: { updatedAt: string; uid: string } }>(
  left: T,
  right: T,
): number {
  return (
    right.metadata.updatedAt.localeCompare(left.metadata.updatedAt) ||
    left.metadata.uid.localeCompare(right.metadata.uid)
  );
}

async function listSessions(
  client: AgentClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly ManagedAgentSession[]> {
  const sessions: ManagedAgentSession[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listManagedAgentSessions(
      tenantId,
      projectId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    sessions.push(...page.value.sessions);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "session");
  } while (pageToken !== undefined);
  return sessions.toSorted(newestFirst);
}

export async function loadSessionExecutions(
  client: AgentClient,
  tenantId: string,
  projectId: string,
  sessionId: string,
  preferredTurnId: string,
  preferredExecutionId: string,
  signal: AbortSignal,
): Promise<
  Readonly<{ executions: readonly ManagedAgentExecution[]; execution?: ManagedAgentExecution }>
> {
  const executions: ManagedAgentExecution[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await client.listManagedAgentExecutions(
      tenantId,
      projectId,
      sessionId,
      newRequestId(),
      200,
      pageToken,
      signal,
    );
    executions.push(...page.value.executions);
    pageToken = page.value.nextPageToken;
    recordPageToken(seenTokens, pageToken, "execution");
  } while (pageToken !== undefined);
  const sorted = executions.toSorted(newestFirst);
  const selected =
    sorted.find(
      ({ metadata }) =>
        metadata.uid === preferredExecutionId &&
        (preferredTurnId === "" || metadata.turnId === preferredTurnId),
    ) ?? sorted[0];
  if (selected === undefined) return Object.freeze({ executions: Object.freeze(sorted) });
  const hydrated = await client.getManagedAgentExecution(
    tenantId,
    projectId,
    sessionId,
    selected.metadata.turnId,
    selected.metadata.uid,
    newRequestId(),
    signal,
  );
  return Object.freeze({
    executions: Object.freeze(sorted),
    execution: hydrated.value,
  });
}

export async function loadAgentResources(
  client: AgentClient,
  tenantId: string,
  projectId: string,
  preferredSessionId: string,
  preferredTurnId: string,
  preferredExecutionId: string,
  signal: AbortSignal,
): Promise<AgentResources> {
  const sessions = await listSessions(client, tenantId, projectId, signal);
  const session =
    sessions.find(({ metadata }) => metadata.uid === preferredSessionId) ??
    sessions.find(({ spec }) => spec.state === "active") ??
    sessions[0];
  if (session === undefined)
    return Object.freeze({ sessions: Object.freeze(sessions), executions: Object.freeze([]) });
  const loaded = await loadSessionExecutions(
    client,
    tenantId,
    projectId,
    session.metadata.uid,
    preferredTurnId,
    preferredExecutionId,
    signal,
  );
  return Object.freeze({ sessions: Object.freeze(sessions), session, ...loaded });
}

export async function readAgentEventBatch(
  client: AgentClient,
  tenantId: string,
  projectId: string,
  sessionId: string,
  cursor: string,
  signal: AbortSignal,
  maxPages = 8,
): Promise<AgentEventBatch> {
  const events: ManagedAgentEvent[] = [];
  const seenCursors = new Set(cursor === "" ? [] : [cursor]);
  let nextCursor = cursor;
  let hasMore = false;
  for (let pageIndex = 0; pageIndex < maxPages; pageIndex += 1) {
    const page = await client.listManagedAgentEvents(
      tenantId,
      projectId,
      sessionId,
      newRequestId(),
      nextCursor,
      64,
      signal,
    );
    const value = page.value;
    if (value.events.length > 0 && (value.nextCursor === "" || value.nextCursor === nextCursor))
      throw new AgentEventStreamError("Event cursor did not advance after receiving events.");
    if (value.hasMore && value.events.length === 0)
      throw new AgentEventStreamError("Event stream returned an empty continuation page.");
    events.push(...value.events);
    hasMore = value.hasMore;
    if (value.nextCursor !== "") {
      if (value.nextCursor !== nextCursor && seenCursors.has(value.nextCursor))
        throw new AgentEventStreamError("Event stream repeated a cursor.");
      seenCursors.add(value.nextCursor);
      nextCursor = value.nextCursor;
    }
    if (!hasMore) break;
  }
  return Object.freeze({ events: Object.freeze(events), nextCursor, hasMore });
}

function compareSequence(left: string, right: string): number {
  return left.length - right.length || left.localeCompare(right);
}

export function mergeAgentEvents(
  current: readonly ManagedAgentEvent[],
  incoming: readonly ManagedAgentEvent[],
): readonly ManagedAgentEvent[] {
  const unique = new Map(current.map((event) => [event.metadata.uid, event]));
  for (const event of incoming)
    if (!unique.has(event.metadata.uid)) unique.set(event.metadata.uid, event);
  return [...unique.values()].toSorted((left, right) =>
    compareSequence(left.metadata.sequence, right.metadata.sequence),
  );
}

export function isExecutionActive(execution: ManagedAgentExecution | undefined): boolean {
  return execution?.spec.state === "queued" || execution?.spec.state === "running";
}

export function executionMessageText(message: ManagedAgentExecutionMessage): string {
  if (message.error?.message) return message.error.message;
  if (typeof message.payload === "object" && message.payload !== null) {
    const payload = message.payload as Record<string, unknown>;
    if (typeof payload.text === "string") return payload.text;
    if (typeof payload.summary === "string") return payload.summary;
    return JSON.stringify(payload, null, 2);
  }
  return message.messageType;
}

export function readAgentSelection(
  storage: SelectionStorage,
  tenantId: string,
  projectId: string,
): AgentSelection {
  const empty = Object.freeze({
    tenantId,
    projectId,
    sessionId: "",
    turnId: "",
    executionId: "",
    eventCursor: "",
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
      typeof candidate.sessionId !== "string" ||
      typeof candidate.turnId !== "string" ||
      typeof candidate.executionId !== "string" ||
      typeof candidate.eventCursor !== "string" ||
      candidate.sessionId.length > 128 ||
      candidate.turnId.length > 128 ||
      candidate.executionId.length > 128 ||
      candidate.eventCursor.length > 2048
    )
      return empty;
    return Object.freeze({
      tenantId,
      projectId,
      sessionId: candidate.sessionId,
      turnId: candidate.turnId,
      executionId: candidate.executionId,
      eventCursor: candidate.eventCursor,
    });
  } catch {
    return empty;
  }
}

export function writeAgentSelection(storage: SelectionStorage, selection: AgentSelection): void {
  try {
    storage.setItem(selectionStorageKey, JSON.stringify(selection));
  } catch {
    // Server authority remains available when browser storage is unavailable.
  }
}

export function agentErrorMessage(error: unknown): string {
  if (error instanceof ClientError && error.status === 401)
    return "The connection token expired or was rejected. Disconnect and authenticate again.";
  if (error instanceof ClientError && error.status === 403)
    return "This token cannot run Agents in the selected project.";
  if (error instanceof ClientError && error.status === 404)
    return "The selected Session or Execution no longer exists. Refresh server state.";
  if (error instanceof ClientError && error.status === 409)
    return "The Session, Turn, or Execution changed concurrently. Refresh and retry the same operation.";
  if (error instanceof ClientError && (error.status === 502 || error.status === 503))
    return "The Worker or Runtime is unavailable. The Execution state is retained for a safe retry.";
  if (error instanceof JSONContractError)
    return "Control Plane returned a response outside the Managed Agent API contract.";
  if (error instanceof AgentEventStreamError) return error.message;
  if (error instanceof DOMException && error.name === "TimeoutError")
    return "The Agent operation timed out. Refresh before retrying because it may still have completed.";
  if (error instanceof DOMException && error.name === "AbortError")
    return "The Agent operation was cancelled.";
  return "The Agent operation failed. Refresh server state, then retry safely.";
}
