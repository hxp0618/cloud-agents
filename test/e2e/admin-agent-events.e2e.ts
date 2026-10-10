import { test } from "@e2e-dev/web";
import { expect } from "e2e";
import { adminScopePage, mockAdminSession } from "./admin-session-fixture";

const tenantId = "tenant-test";
const projectId = "project-test";
const digest = `sha256:${"a".repeat(64)}`;

function session(uid: string) {
  return {
    apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
    kind: "Session",
    metadata: {
      uid,
      projectId,
      resourceVersion: "1",
      createdAt: "2026-10-08T01:00:00Z",
      updatedAt: "2026-10-08T01:00:00Z",
    },
    spec: { providerKind: "codex", state: "active" },
  };
}

function capabilityEvent(sessionId: string, sequence: number) {
  return {
    apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
    kind: "Event",
    metadata: {
      uid: `event-${sessionId}-${sequence}`,
      projectId,
      sessionId,
      sequence: String(sequence),
      occurredAt: "2026-10-08T01:00:00Z",
    },
    spec: {
      operation: "mcp.call",
      resource: "McpServer",
      generation: 1,
      mutationDigest: digest,
      result: "succeeded",
      serverId: `server-${sessionId}-${sequence}`,
      version: "v1",
      digest,
      changes: [{ resource: "McpServer", from: "pending", to: "succeeded", version: 1 }],
    },
  };
}

test("Admin pages Session, Execution and event windows and awaits refreshed metadata", async ({
  app,
  browser,
  screen,
}) => {
  await mockAdminSession(browser);
  const eventRequests: string[] = [];
  const sessionRequests: string[] = [];
  const executionRequests: string[] = [];
  let revealNewSession = false;
  let releaseInitialLoad!: () => void;
  let markInitialLoadStarted!: () => void;
  const initialLoadGate = new Promise<void>((resolve) => {
    releaseInitialLoad = resolve;
  });
  const initialLoadStarted = new Promise<void>((resolve) => {
    markInitialLoadStarted = resolve;
  });
  let releaseRefresh!: () => void;
  let markRefreshStarted!: () => void;
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  const refreshStarted = new Promise<void>((resolve) => {
    markRefreshStarted = resolve;
  });
  const emptyPages: Record<string, readonly [string, string]> = {
    "deployment-targets": ["DeploymentTargetPage", "deploymentTargets"],
    "environment-leases": ["EnvironmentLeasePage", "environmentLeases"],
    workers: ["WorkerPage", "workers"],
    "maintenance-operations": ["MaintenanceOperationPage", "operations"],
    "mcp-servers": ["McpServerPage", "mcpServers"],
    "skill-bundles": ["SkillBundlePage", "skillBundles"],
  };
  await browser.route("**/v1/admin/**", async (route) => {
    const url = new URL(route.request.url);
    const path = url.pathname;
    const scope = adminScopePage(path);
    if (scope !== undefined) return route.fulfill({ json: scope });
    const eventMatch = path.match(/\/sessions\/(session-[a-z]+)\/events$/u);
    if (eventMatch) {
      eventRequests.push(`${path}${url.search}`);
      const sessionId = eventMatch[1]!;
      const cursor = url.searchParams.get("cursor");
      const events =
        sessionId === "session-alpha" && cursor === null
          ? Array.from({ length: 64 }, (_, index) => capabilityEvent(sessionId, index + 1))
          : [capabilityEvent(sessionId, sessionId === "session-alpha" ? 65 : 1)];
      await route.fulfill({
        json: {
          apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
          kind: "EventPage",
          events,
          nextCursor: sessionId === "session-alpha" && cursor === null ? "cursor-alpha-64" : "",
          hasMore: sessionId === "session-alpha" && cursor === null,
        },
      });
      return;
    }
    if (path.endsWith("/sessions")) {
      sessionRequests.push(`${path}${url.search}`);
      if (sessionRequests.length === 1) {
        markInitialLoadStarted();
        await initialLoadGate;
      }
      const nextPage = url.searchParams.has("pageToken");
      await route.fulfill({
        json: {
          apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
          kind: "SessionPage",
          sessions: revealNewSession
            ? [session("session-new")]
            : nextPage
              ? [session("session-gamma")]
              : [session("session-alpha"), session("session-beta")],
          ...(!nextPage && !revealNewSession ? { nextPageToken: "sessions-page-next" } : {}),
        },
      });
      return;
    }
    if (path.endsWith("/mcp-servers") && revealNewSession) {
      markRefreshStarted();
      await refreshGate;
      await route.fulfill({ status: 503, body: "Test MCP API unavailable" });
      return;
    }
    if (/\/sessions\/session-[a-z]+\/executions$/u.test(path)) {
      executionRequests.push(`${path}${url.search}`);
      const firstAlphaPage = path.includes("/session-alpha/") && !url.searchParams.has("pageToken");
      await route.fulfill({
        json: {
          apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
          kind: "ExecutionPage",
          executions: firstAlphaPage
            ? [
                {
                  ...session("execution-first"),
                  kind: "Execution",
                  metadata: {
                    ...session("execution-first").metadata,
                    sessionId: "session-alpha",
                    turnId: "turn-first",
                  },
                  spec: { generation: 1, state: "queued", attemptNumber: 0, recoveryState: "none" },
                },
              ]
            : [],
          ...(firstAlphaPage ? { nextPageToken: "executions-page-next" } : {}),
        },
      });
      return;
    }
    const page = emptyPages[path.split("/").at(-1) ?? ""];
    if (page) {
      await route.fulfill({
        json: {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: page[0],
          [page[1]]: [],
        },
      });
      return;
    }
    await route.fulfill({ status: 503, body: "Test API unavailable" });
  });

  await app.open();
  await screen.getByLabel("Email").fill("admin@example.test");
  await screen.getByLabel("Password", { exact: true }).fill("fixture password value");
  await screen.getByRole("button", "Sign in").click();
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await screen.getByRole("button", "MCP & Skills").click();
  await expect(screen.getByRole("heading", "MCP & Skill capabilities")).toBeVisible();
  await expect(screen.getByRole("heading", "Capability events")).toBeVisible();
  await initialLoadStarted;
  try {
    await expect(screen.getByRole("button", "Refresh")).toBeDisabled();
  } finally {
    releaseInitialLoad();
  }
  await expect(screen.getByLabel("Session audit")).toHaveValue("session-alpha");
  await expect(screen.getByLabel("Session in current window")).toHaveValue("session-alpha");
  expect(sessionRequests.at(-1)).toBe(
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions?pageSize=64`,
  );
  await expect(
    screen.getByText(
      "Event history has not been loaded. Select a Session to read its audit stream.",
    ),
  ).toBeVisible();
  expect(eventRequests).toEqual([]);

  await screen.getByRole("button", "Load events").click();
  await expect(
    screen.getByText("Current page 1 · at most 64 events · more history is available."),
  ).toBeVisible();
  await expect(screen.getByText("server-session-alpha-1", { exact: true })).toBeVisible();
  await expect(screen.getByText("server-session-alpha-64", { exact: true })).toBeVisible();
  expect(eventRequests).toEqual([
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions/session-alpha/events?limit=64`,
  ]);

  await screen.getByRole("button", "Load more events").click();
  await expect(
    screen.getByText(
      "Current page 2 · at most 64 events · reached the end of this Session's stream.",
    ),
  ).toBeVisible();
  await expect(screen.getByText("server-session-alpha-65", { exact: true })).toBeVisible();
  await expect(screen.getByText("server-session-alpha-1", { exact: true })).not.toBeVisible();
  await expect(screen.getByRole("button", "Load more events")).not.toBeVisible();
  expect(eventRequests.at(-1)).toBe(
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions/session-alpha/events?cursor=cursor-alpha-64&limit=64`,
  );

  await screen.getByLabel("Session audit").selectOption({ value: "session-beta" });
  await expect(
    screen.getByText(
      "Event history has not been loaded. Select a Session to read its audit stream.",
    ),
  ).toBeVisible();
  await expect(screen.getByText("server-session-alpha-65", { exact: true })).not.toBeVisible();
  expect(eventRequests).toHaveLength(2);
  await screen.getByRole("button", "Load events").click();
  await expect(
    screen.getByText(
      "Current page 1 · at most 64 events · reached the end of this Session's stream.",
    ),
  ).toBeVisible();
  await expect(screen.getByText("server-session-beta-1", { exact: true })).toBeVisible();
  expect(eventRequests.at(-1)).toBe(
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions/session-beta/events?limit=64`,
  );

  await screen.getByRole("button", "Next Execution window").click();
  await expect(screen.getByRole("button", "Next Execution window")).toBeDisabled();
  expect(executionRequests.at(-1)).toBe(
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions/session-alpha/executions?pageSize=64&pageToken=executions-page-next`,
  );
  await screen.getByRole("button", "Next Session window").click();
  await expect(screen.getByLabel("Session in current window")).toHaveValue("session-gamma");
  await expect(screen.getByLabel("Session audit")).toHaveValue("session-gamma");
  await expect(screen.getByRole("button", "Next Session window")).toBeDisabled();
  await expect(screen.getByText("server-session-beta-1", { exact: true })).not.toBeVisible();
  expect(sessionRequests.at(-1)).toBe(
    `/v1/admin/tenants/${tenantId}/projects/${projectId}/sessions?pageSize=64&pageToken=sessions-page-next`,
  );

  revealNewSession = true;
  await screen.getByRole("button", "Refresh").click();
  await refreshStarted;
  try {
    await expect(screen.getByRole("button", "Refresh")).toBeDisabled();
  } finally {
    releaseRefresh();
  }
  await expect(screen.getByText(/^MCP servers could not be loaded/u)).toBeVisible();
  await expect(screen.getByLabel("Session in current window")).toHaveValue("session-new");
  await expect(screen.getByLabel("Session audit")).toHaveValue("session-new");
  await expect(screen.getByText("Authority refresh completed.")).not.toBeVisible();
  await expect(screen.getByRole("button", "Refresh")).toBeEnabled();
});
