import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  type ManagedAgentEvent,
  type ManagedAgentExecution,
  type ManagedAgentSession,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  agentErrorMessage,
  isExecutionActive,
  loadAgentResources,
  mergeAgentEvents,
  readAgentEventBatch,
  readAgentSelection,
  writeAgentSelection,
  type AgentClient,
} from "./agent";

function storage(initial: string | null = null) {
  let value = initial;
  return {
    getItem: vi.fn(() => value),
    setItem: vi.fn((_key: string, next: string) => {
      value = next;
    }),
    value: () => value,
  };
}

function session(uid: string, updatedAt: string): ManagedAgentSession {
  return { metadata: { uid, updatedAt }, spec: { state: "active" } } as ManagedAgentSession;
}

function execution(uid: string, turnId: string, updatedAt: string): ManagedAgentExecution {
  return {
    metadata: { uid, turnId, updatedAt },
    spec: { generation: 1, state: "running" },
  } as ManagedAgentExecution;
}

function event(uid: string, sequence: string): ManagedAgentEvent {
  return { metadata: { uid, sequence } } as ManagedAgentEvent;
}

describe("Agent server authority", () => {
  it("loads paginated Sessions and Executions, then hydrates the saved Execution", async () => {
    const selected = execution("execution-selected", "turn-selected", "2026-09-02T02:00:00Z");
    const client = {
      listManagedAgentSessions: vi
        .fn()
        .mockResolvedValueOnce({
          value: {
            sessions: [session("session-old", "2026-09-02T00:00:00Z")],
            nextPageToken: "sessions-next",
          },
        })
        .mockResolvedValueOnce({
          value: { sessions: [session("session-selected", "2026-09-02T01:00:00Z")] },
        }),
      listManagedAgentExecutions: vi
        .fn()
        .mockResolvedValueOnce({
          value: {
            executions: [execution("execution-old", "turn-old", "2026-09-02T00:00:00Z")],
            nextPageToken: "executions-next",
          },
        })
        .mockResolvedValueOnce({ value: { executions: [selected] } }),
      getManagedAgentExecution: vi.fn().mockResolvedValue({
        value: { ...selected, messages: [{ messageType: "Progress" }] },
      }),
    } as unknown as AgentClient;

    const resources = await loadAgentResources(
      client,
      "tenant-local",
      "project-alpha",
      "session-selected",
      "turn-selected",
      "execution-selected",
      new AbortController().signal,
    );

    expect(resources.sessions.map(({ metadata }) => metadata.uid)).toEqual([
      "session-selected",
      "session-old",
    ]);
    expect(resources.session?.metadata.uid).toBe("session-selected");
    expect(resources.execution?.metadata.uid).toBe("execution-selected");
    expect(resources.execution?.messages).toHaveLength(1);
    expect(client.listManagedAgentSessions).toHaveBeenCalledTimes(2);
    expect(client.listManagedAgentExecutions).toHaveBeenCalledTimes(2);
  });

  it("rejects a repeated Session page token", async () => {
    const client = {
      listManagedAgentSessions: vi.fn().mockResolvedValue({
        value: { sessions: [], nextPageToken: "repeated-session-page" },
      }),
    } as unknown as AgentClient;

    await expect(
      loadAgentResources(
        client,
        "tenant-local",
        "project-alpha",
        "",
        "",
        "",
        new AbortController().signal,
      ),
    ).rejects.toThrow("repeated a session page token");
  });
});

describe("Agent event polling", () => {
  it("advances through bounded continuation pages and de-duplicates repeated events", async () => {
    const duplicate = event("event-1", "1");
    const client = {
      listManagedAgentEvents: vi
        .fn()
        .mockResolvedValueOnce({
          value: { events: [duplicate], nextCursor: "cursor-1", hasMore: true },
        })
        .mockResolvedValueOnce({
          value: {
            events: [duplicate, event("event-2", "2")],
            nextCursor: "cursor-2",
            hasMore: false,
          },
        }),
    } as unknown as AgentClient;

    const batch = await readAgentEventBatch(
      client,
      "tenant-local",
      "project-alpha",
      "session-alpha",
      "",
      new AbortController().signal,
    );

    expect(batch.nextCursor).toBe("cursor-2");
    expect(batch.hasMore).toBe(false);
    expect(mergeAgentEvents([], batch.events).map(({ metadata }) => metadata.uid)).toEqual([
      "event-1",
      "event-2",
    ]);
  });

  it("stops when an event page does not advance its cursor", async () => {
    const client = {
      listManagedAgentEvents: vi.fn().mockResolvedValue({
        value: { events: [event("event-1", "1")], nextCursor: "cursor-same", hasMore: false },
      }),
    } as unknown as AgentClient;

    await expect(
      readAgentEventBatch(
        client,
        "tenant-local",
        "project-alpha",
        "session-alpha",
        "cursor-same",
        new AbortController().signal,
      ),
    ).rejects.toThrow("cursor did not advance");
  });

  it("stops when a continuation page moves back to an earlier cursor", async () => {
    const client = {
      listManagedAgentEvents: vi
        .fn()
        .mockResolvedValueOnce({
          value: { events: [event("event-2", "2")], nextCursor: "cursor-2", hasMore: true },
        })
        .mockResolvedValueOnce({
          value: { events: [event("event-3", "3")], nextCursor: "cursor-1", hasMore: false },
        }),
    } as unknown as AgentClient;

    await expect(
      readAgentEventBatch(
        client,
        "tenant-local",
        "project-alpha",
        "session-alpha",
        "cursor-1",
        new AbortController().signal,
      ),
    ).rejects.toThrow("repeated a cursor");
  });

  it("propagates AbortSignal cancellation", async () => {
    const controller = new AbortController();
    const client = {
      listManagedAgentEvents: vi.fn(
        (...args: unknown[]) =>
          new Promise((_resolve, reject) => {
            const signal = args.at(-1) as AbortSignal;
            signal.addEventListener("abort", () => reject(signal.reason), { once: true });
          }),
      ),
    } as unknown as AgentClient;
    const pending = readAgentEventBatch(
      client,
      "tenant-local",
      "project-alpha",
      "session-alpha",
      "",
      controller.signal,
    );
    controller.abort(new DOMException("cancelled", "AbortError"));

    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
  });
});

describe("Agent recovery and errors", () => {
  it("restores only project-bound identifiers and event cursor", () => {
    const target = storage();
    writeAgentSelection(target, {
      tenantId: "tenant-local",
      projectId: "project-alpha",
      sessionId: "session-alpha",
      turnId: "turn-alpha",
      executionId: "execution-alpha",
      eventCursor: "opaque-cursor",
    });

    expect(readAgentSelection(target, "tenant-local", "project-alpha").eventCursor).toBe(
      "opaque-cursor",
    );
    expect(readAgentSelection(target, "tenant-local", "project-other").sessionId).toBe("");
    expect(target.value()).not.toContain("Bearer");
  });

  it("distinguishes authentication and authorization failures", () => {
    expect(agentErrorMessage(new ClientError("events", 401))).toContain("expired");
    expect(agentErrorMessage(new ClientError("events", 403))).toContain("cannot run Agents");
  });

  it("polls only queued and running Executions", () => {
    expect(
      isExecutionActive(execution("execution-running", "turn-1", "2026-09-02T00:00:00Z")),
    ).toBe(true);
    expect(
      isExecutionActive({
        ...execution("execution-done", "turn-2", "2026-09-02T00:00:00Z"),
        spec: { generation: 1, state: "succeeded" },
      } as ManagedAgentExecution),
    ).toBe(false);
  });
});
