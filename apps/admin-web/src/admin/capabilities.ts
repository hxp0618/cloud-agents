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
  events: readonly ManagedAgentEvent[];
}>;

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
  includeEvents = true,
): Promise<AdminManagedAgentRuntime> {
  const sessions = (
    await collectAdminPages<ManagedAgentSession>(
      (pageToken) =>
        client
          .listAdminManagedAgentSessions(
            tenantId,
            projectId,
            newRequestId(),
            200,
            pageToken,
            signal,
          )
          .then((page) => ({
            items: page.value.sessions,
            nextPageToken: page.value.nextPageToken,
          })),
      () => new AdminUIError("error.agentSessionPageToken"),
    )
  ).filter(({ spec }) => sandboxId === "" || spec.sandboxId === sandboxId);

  const executions: ManagedAgentExecution[] = [];
  const events: ManagedAgentEvent[] = [];
  for (const session of sessions) {
    executions.push(
      ...(await collectAdminPages<ManagedAgentExecution>(
        (pageToken) =>
          client
            .listAdminManagedAgentExecutions(
              tenantId,
              projectId,
              session.metadata.uid,
              newRequestId(),
              200,
              pageToken,
              signal,
            )
            .then((page) => ({
              items: page.value.executions,
              nextPageToken: page.value.nextPageToken,
            })),
        () => new AdminUIError("error.agentExecutionPageToken"),
      )),
    );

    if (includeEvents) {
      let cursor: string | undefined;
      const seenCursors = new Set<string>();
      for (;;) {
        const page = await client.listAdminManagedAgentEvents(
          tenantId,
          projectId,
          session.metadata.uid,
          newRequestId(),
          cursor,
          64,
          signal,
        );
        events.push(...page.value.events);
        if (!page.value.hasMore) break;
        cursor = page.value.nextCursor;
        if (seenCursors.has(cursor) || events.length >= 4096)
          throw new AdminUIError("error.agentEventCursor");
        seenCursors.add(cursor);
      }
    }
  }
  return Object.freeze({
    sessions: Object.freeze(sessions),
    executions: Object.freeze(executions),
    events: Object.freeze(events.slice(-64).reverse()),
  });
}

export function listAdminManagedAgentBindings(
  client: AdminClient,
  tenantId: string,
  projectId: string,
  signal: AbortSignal,
): Promise<AdminManagedAgentRuntime> {
  return loadAdminManagedAgentRuntime(client, tenantId, projectId, "", signal, false);
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
