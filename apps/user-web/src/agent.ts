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
  | "resolveManagedAgentApproval"
  | "resolveManagedAgentUserInput"
  | "downloadManagedAgentArtifact"
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

export type AgentApprovalInteraction = Readonly<{
  kind: "approval";
  executionId: string;
  generation: number;
  requestId: string;
  summary: string;
  details: readonly string[];
}>;

export type AgentUserInputQuestion = Readonly<{
  id: string;
  header: string;
  question: string;
  options: readonly Readonly<{ label: string; description: string }>[];
  multiSelect: boolean;
  isOther: boolean;
  isSecret: boolean;
}>;

export type AgentUserInputInteraction = Readonly<{
  kind: "user-input";
  executionId: string;
  generation: number;
  requestId: string;
  questions: readonly AgentUserInputQuestion[];
}>;

export type AgentInteraction = AgentApprovalInteraction | AgentUserInputInteraction;

export type AgentArtifact = Readonly<{
  executionId: string;
  generation: number;
  messageIndex: number;
  path: string;
  kind: string;
  sourceRoot: "workspace" | "runtime-output";
  contentType: string;
  reportedSize?: number;
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

function payloadRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function payloadText(value: unknown, maxLength: number): string | undefined {
  return typeof value === "string" &&
    value !== "" &&
    value.length <= maxLength &&
    !value.includes("\0")
    ? value
    : undefined;
}

function payloadToken(value: unknown, maxLength: number): string | undefined {
  const text = payloadText(value, maxLength);
  return text !== undefined && !/[\u0000-\u001f\u007f]/u.test(text) ? text : undefined;
}

function inputQuestions(value: unknown): readonly AgentUserInputQuestion[] | undefined {
  if (!Array.isArray(value) || value.length === 0 || value.length > 3) return undefined;
  const seen = new Set<string>();
  const questions: AgentUserInputQuestion[] = [];
  for (const item of value) {
    const source = payloadRecord(item);
    const id = payloadToken(source?.id, 200);
    const question = payloadText(source?.question, 2_000);
    if (source === undefined || id === undefined || question === undefined || seen.has(id))
      return undefined;
    seen.add(id);
    const header = payloadText(source.header, 200) ?? id;
    const options: { label: string; description: string }[] = [];
    const optionLabels = new Set<string>();
    if (source.options !== undefined && source.options !== null) {
      if (!Array.isArray(source.options) || source.options.length > 20) return undefined;
      for (const optionValue of source.options) {
        const option = payloadRecord(optionValue);
        const label = payloadText(option?.label, 200);
        if (option === undefined || label === undefined || optionLabels.has(label))
          return undefined;
        optionLabels.add(label);
        const description = option.description ?? "";
        if (
          typeof description !== "string" ||
          description.length > 2_000 ||
          description.includes("\0")
        )
          return undefined;
        options.push({ label, description });
      }
    }
    questions.push(
      Object.freeze({
        id,
        header,
        question,
        options: Object.freeze(options),
        multiSelect: source.multiSelect === true,
        isOther: source.isOther === true,
        isSecret: source.isSecret === true,
      }),
    );
  }
  return Object.freeze(questions);
}

export function agentInteractions(
  execution: ManagedAgentExecution | undefined,
): readonly AgentInteraction[] {
  if (execution === undefined) return Object.freeze([]);
  const interactions: AgentInteraction[] = [];
  const seen = new Set<string>();
  for (const message of execution.messages ?? []) {
    if (
      message.messageType !== "InteractionRequest" ||
      message.executionId !== execution.metadata.uid ||
      message.generation !== execution.spec.generation
    )
      continue;
    const payload = payloadRecord(message.payload);
    const requestId = payloadToken(payload?.requestId, 200);
    if (payload === undefined || requestId === undefined || seen.has(requestId)) continue;
    if (payload.interactionType === "approval") {
      const summary = payloadText(payload.summary, 4_096) ?? "Agent requests approval.";
      const details = ["provider", "requestKind", "command", "cwd", "path", "toolName"].flatMap(
        (key) => {
          const value = payloadText(payload[key], 4_096);
          return value === undefined ? [] : [`${key}: ${value}`];
        },
      );
      interactions.push(
        Object.freeze({
          kind: "approval",
          executionId: execution.metadata.uid,
          generation: message.generation,
          requestId,
          summary,
          details: Object.freeze(details),
        }),
      );
      seen.add(requestId);
    } else if (payload.interactionType === "user-input") {
      const questions = inputQuestions(payload.questions);
      if (questions === undefined) continue;
      interactions.push(
        Object.freeze({
          kind: "user-input",
          executionId: execution.metadata.uid,
          generation: message.generation,
          requestId,
          questions,
        }),
      );
      seen.add(requestId);
    }
  }
  return Object.freeze(interactions);
}

export function agentArtifacts(
  execution: ManagedAgentExecution | undefined,
): readonly AgentArtifact[] {
  if (execution === undefined) return Object.freeze([]);
  const artifacts: AgentArtifact[] = [];
  for (const [messageIndex, message] of (execution.messages ?? []).entries()) {
    if (
      message.messageType !== "ArtifactCandidate" ||
      message.executionId !== execution.metadata.uid ||
      message.generation !== execution.spec.generation
    )
      continue;
    const artifact = payloadRecord(payloadRecord(message.payload)?.artifact);
    const path = payloadText(artifact?.path, 4_096);
    const kind = payloadText(artifact?.kind, 64);
    const sourceRoot = artifact?.sourceRoot;
    const contentType = payloadText(artifact?.contentType, 255) ?? "application/octet-stream";
    if (
      artifact === undefined ||
      path === undefined ||
      kind === undefined ||
      (sourceRoot !== "workspace" && sourceRoot !== "runtime-output")
    )
      continue;
    const reportedSize = artifact.reportedSize;
    artifacts.push(
      Object.freeze({
        executionId: execution.metadata.uid,
        generation: message.generation,
        messageIndex,
        path,
        kind,
        sourceRoot,
        contentType,
        ...(typeof reportedSize === "number" &&
        Number.isSafeInteger(reportedSize) &&
        reportedSize >= 0
          ? { reportedSize }
          : {}),
      }),
    );
  }
  return Object.freeze(artifacts);
}

export function isAgentPollingFatal(error: unknown): boolean {
  return (
    (error instanceof ClientError && (error.status === 401 || error.status === 403)) ||
    error instanceof AgentEventStreamError
  );
}

export function executionMessageText(message: ManagedAgentExecutionMessage): string {
  if (message.error?.message) return message.error.message;
  const payload = payloadRecord(message.payload);
  if (payload !== undefined) {
    if (typeof payload.text === "string") return payload.text;
    if (typeof payload.summary === "string") return payload.summary;
    if (message.messageType === "InteractionRequest" && payload.interactionType === "user-input")
      return "Agent requested user input.";
    if (message.messageType === "ArtifactCandidate") {
      const path = payloadText(payloadRecord(payload.artifact)?.path, 4_096);
      if (path !== undefined) return `Artifact ready: ${path}`;
    }
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
