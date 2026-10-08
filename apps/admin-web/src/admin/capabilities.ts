import type {
  ManagedAgentEvent,
  ManagedAgentExecution,
  ManagedAgentSession,
  McpServer,
  SkillBundle,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import type { AdminClient } from "./client";
import { AdminUIError } from "./errors";
import { collectAdminPages } from "./pagination";
import { newRequestId } from "./request";

export type AdminManagedAgentRuntime = Readonly<{
  sessions: readonly ManagedAgentSession[];
  executions: readonly ManagedAgentExecution[];
  sessionPageToken?: string;
  nextSessionPageToken?: string;
  seenSessionPageTokens: readonly string[];
  selectedSessionId: string;
  executionPageToken?: string;
  nextExecutionPageToken?: string;
  seenExecutionPageTokens: readonly string[];
}>;

export type AdminManagedAgentEventPage = Readonly<{
  events: readonly ManagedAgentEvent[];
  nextCursor?: string;
  hasMore: boolean;
  seenCursors: readonly string[];
}>;

export async function loadAdminManagedAgentEventPage(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  sessionId: string,
  signal: AbortSignal,
  previous?: AdminManagedAgentEventPage,
): Promise<AdminManagedAgentEventPage> {
  const page = await client.listAdminManagedAgentEvents(
    tenantId,
    projectId,
    sessionId,
    newRequestId(),
    previous?.nextCursor,
    64,
    signal,
  );
  const nextCursor = page.value.nextCursor;
  const seenCursors = new Set(previous?.seenCursors ?? []);
  if (
    page.value.hasMore &&
    (!nextCursor || nextCursor === previous?.nextCursor || seenCursors.has(nextCursor))
  )
    throw new AdminUIError("error.agentEventCursor");
  if (nextCursor) seenCursors.add(nextCursor);
  return Object.freeze({
    events: Object.freeze(page.value.events.toReversed()),
    ...(nextCursor === undefined ? {} : { nextCursor }),
    hasMore: page.value.hasMore,
    seenCursors: Object.freeze([...seenCursors]),
  });
}

export async function listAdminMcpServers(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly McpServer[]> {
  const servers = await collectAdminPages<McpServer>(
    (pageToken) =>
      client
        .listAdminMcpServers(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.mcpServers,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.capabilityPageToken"),
  );
  return Object.freeze(
    servers.toSorted((left, right) => left.metadata.uid.localeCompare(right.metadata.uid)),
  );
}

export async function listAdminSkillBundles(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<readonly SkillBundle[]> {
  const bundles = await collectAdminPages<SkillBundle>(
    (pageToken) =>
      client
        .listAdminSkillBundles(tenantId, projectId, newRequestId(), 200, pageToken, signal)
        .then((page) => ({
          items: page.value.skillBundles,
          nextPageToken: page.value.nextPageToken,
        })),
    () => new AdminUIError("error.capabilityPageToken"),
  );
  return Object.freeze(
    bundles.toSorted((left, right) => left.metadata.uid.localeCompare(right.metadata.uid)),
  );
}

export async function loadAdminManagedAgentRuntime(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  sandboxId: string,
  signal: AbortSignal,
  previous?: AdminManagedAgentRuntime,
  nextSession = false,
  selectedSessionId = previous?.selectedSessionId ?? "",
  nextExecution = false,
): Promise<AdminManagedAgentRuntime> {
  const sessionPageToken = nextSession
    ? previous?.nextSessionPageToken
    : previous?.sessionPageToken;
  const sessionPage = await client.listAdminManagedAgentSessions(
    tenantId,
    projectId,
    newRequestId(),
    sandboxId || undefined,
    64,
    sessionPageToken,
    signal,
  );
  const sessions = sessionPage.value.sessions;
  const seenSessionPageTokens = new Set(previous?.seenSessionPageTokens ?? []);
  if (
    sessionPage.value.nextPageToken &&
    (sessionPage.value.nextPageToken === sessionPageToken ||
      (nextSession && seenSessionPageTokens.has(sessionPage.value.nextPageToken)))
  )
    throw new AdminUIError("error.agentSessionPageToken");
  if (sessionPage.value.nextPageToken) seenSessionPageTokens.add(sessionPage.value.nextPageToken);

  const sessionId = sessions.some(({ metadata }) => metadata.uid === selectedSessionId)
    ? selectedSessionId
    : (sessions[0]?.metadata.uid ?? "");
  let executions: readonly ManagedAgentExecution[] = Object.freeze([]);
  let executionPageToken: string | undefined;
  let nextExecutionPageToken: string | undefined;
  let seenExecutionPageTokens: readonly string[] = Object.freeze([]);
  if (sessionId !== "") {
    executionPageToken =
      !nextSession && sessionId === previous?.selectedSessionId
        ? nextExecution
          ? previous.nextExecutionPageToken
          : previous.executionPageToken
        : undefined;
    const executionPage = await client.listAdminManagedAgentExecutions(
      tenantId,
      projectId,
      sessionId,
      newRequestId(),
      64,
      executionPageToken,
      signal,
    );
    executions = Object.freeze(executionPage.value.executions);
    nextExecutionPageToken = executionPage.value.nextPageToken;
    const seen = new Set(
      sessionId === previous?.selectedSessionId ? previous.seenExecutionPageTokens : [],
    );
    if (
      nextExecutionPageToken &&
      (nextExecutionPageToken === executionPageToken ||
        (nextExecution && seen.has(nextExecutionPageToken)))
    )
      throw new AdminUIError("error.agentExecutionPageToken");
    if (nextExecutionPageToken) seen.add(nextExecutionPageToken);
    seenExecutionPageTokens = Object.freeze([...seen]);
  }
  return Object.freeze({
    sessions: Object.freeze([...sessions]),
    executions,
    ...(sessionPageToken === undefined ? {} : { sessionPageToken }),
    ...(sessionPage.value.nextPageToken === undefined
      ? {}
      : { nextSessionPageToken: sessionPage.value.nextPageToken }),
    seenSessionPageTokens: Object.freeze([...seenSessionPageTokens]),
    selectedSessionId: sessionId,
    ...(executionPageToken === undefined ? {} : { executionPageToken }),
    ...(nextExecutionPageToken === undefined ? {} : { nextExecutionPageToken }),
    seenExecutionPageTokens,
  });
}

export function listAdminManagedAgentBindings(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<AdminManagedAgentRuntime> {
  return loadAdminManagedAgentRuntime(client, tenantId, projectId, "", signal);
}

export type AdminCapabilityBinding = Readonly<{
  kind: "mcp" | "skill";
  resourceId: string;
  version: string;
  digest: `sha256:${string}`;
  sessionIds: readonly string[];
  executionIds: readonly string[];
}>;

export function capabilityBindingRelations(
  mcpServers: readonly McpServer[],
  skillBundles: readonly SkillBundle[],
  sessions: readonly ManagedAgentSession[],
  executions: readonly ManagedAgentExecution[],
): readonly AdminCapabilityBinding[] {
  const relations = new Map<
    string,
    {
      kind: "mcp" | "skill";
      resourceId: string;
      version: string;
      digest: `sha256:${string}`;
      sessionIds: Set<string>;
      executionIds: Set<string>;
    }
  >();
  for (const server of mcpServers) {
    relations.set(`mcp\0${server.metadata.uid}`, {
      kind: "mcp",
      resourceId: server.metadata.uid,
      version: server.spec.version,
      digest: server.spec.digest,
      sessionIds: new Set(),
      executionIds: new Set(),
    });
  }
  for (const bundle of skillBundles) {
    relations.set(`skill\0${bundle.metadata.uid}`, {
      kind: "skill",
      resourceId: bundle.metadata.uid,
      version: bundle.spec.version,
      digest: bundle.spec.digest,
      sessionIds: new Set(),
      executionIds: new Set(),
    });
  }
  for (const session of sessions) {
    for (const ref of session.spec.mcpServerRefs ?? [])
      relations.get(`mcp\0${ref.serverId}`)?.sessionIds.add(session.metadata.uid);
    for (const ref of session.spec.skillBundleRefs ?? [])
      relations.get(`skill\0${ref.bundleId}`)?.sessionIds.add(session.metadata.uid);
  }
  for (const execution of executions) {
    for (const ref of execution.spec.mcpServerRefs ?? [])
      relations.get(`mcp\0${ref.serverId}`)?.executionIds.add(execution.metadata.uid);
    for (const ref of execution.spec.skillBundleRefs ?? [])
      relations.get(`skill\0${ref.bundleId}`)?.executionIds.add(execution.metadata.uid);
  }
  return Object.freeze(
    [...relations.values()].map((relation) =>
      Object.freeze({
        ...relation,
        sessionIds: Object.freeze([...relation.sessionIds].toSorted()),
        executionIds: Object.freeze([...relation.executionIds].toSorted()),
      }),
    ),
  );
}
