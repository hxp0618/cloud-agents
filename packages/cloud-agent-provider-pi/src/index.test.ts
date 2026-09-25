import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it, vi } from "vitest";
import { loadSkillsFromDir, type AgentSessionEventListener } from "@earendil-works/pi-coding-agent";
import { ManagedCapabilityCallResultUnknownError } from "@cloud-agents/cloud-agent-provider-api/internal";
import { startPiProviderRun } from "./index";
import { createManagedPiMcpTools } from "./managedMcpTools";

describe("Pi Provider", () => {
  it("rejects control characters in credential values before starting a session", () => {
    expect(() =>
      startPiProviderRun(
        {
          execution: { id: "execution-pi-invalid-credential" },
          workload: { provider: "pi", inputText: "hello", model: "model-pi" },
          workspaceDirectory: "/tmp/cloud-agents-pi-workspace",
          providerStateDirectory: "/tmp/cloud-agents-pi-state",
        },
        { payload: { apiKey: "secret\npi", baseURL: "https://gateway.example/v1" } },
        () => {},
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
          },
        },
      ),
    ).toThrow(/non-empty string/u);
  });

  it("fails closed before opening a session when the Host broker is missing", async () => {
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "mcp-server",
          resourceId: "mcp-pi",
          version: "v1",
          digest: `sha256:${"a".repeat(64)}`,
          transport: "streamable-http",
          connectionRef: "connection-pi-mcp",
          credentialRef: "credential-pi-mcp",
          grantId: "grant-pi-mcp",
          networkPolicyRef: "network-pi-mcp",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          permissions: ["tools.call"],
          readOnly: false,
        },
      ],
    };
    const controller = startPiProviderRun(
      {
        execution: { id: "execution-pi-mcp", generation: 1 },
        workload: { provider: "pi", inputText: "use MCP", model: "model-pi" },
        workspaceDirectory: "/tmp/cloud-agents-pi-workspace",
        providerStateDirectory: "/tmp/cloud-agents-pi-state",
      },
      { payload: { apiKey: "secret-pi", baseURL: "https://gateway.example/v1" } },
      () => {},
      {
        environment: {
          CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
          CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
            "base64url",
          ),
        },
      },
    );
    await expect(controller.result).rejects.toThrow(/Host-managed broker/u);
  });

  it("loads the pinned SDK's Agent Skills format", () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-pi-skill-"));
    const skillDirectory = join(root, "managed-skill");
    mkdirSync(skillDirectory);
    writeFileSync(
      join(skillDirectory, "SKILL.md"),
      "---\nname: managed-skill\ndescription: A managed test skill\n---\n\n# Managed Skill\n\nUse the supplied workflow.\n",
    );
    try {
      const loaded = loadSkillsFromDir({ dir: root, source: "cloud-agents-managed" });
      expect(loaded.diagnostics).toEqual([]);
      expect(loaded.skills).toHaveLength(1);
      expect(loaded.skills[0]).toMatchObject({
        name: "managed-skill",
        description: "A managed test skill",
        filePath: join(skillDirectory, "SKILL.md"),
      });
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("discovers and invokes MCP tools through the loopback broker", async () => {
    const methods: string[] = [];
    const authorizations: string[] = [];
    const server = createServer((request, response) => {
      const chunks: Buffer[] = [];
      request.on("data", (chunk: Buffer) => chunks.push(chunk));
      request.on("end", () => {
        const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as {
          id?: number;
          method?: string;
        };
        methods.push(body.method ?? "");
        authorizations.push(request.headers.authorization ?? "");
        response.setHeader("content-type", "application/json");
        response.setHeader("mcp-session-id", "pi-test-session");
        if (body.method === "notifications/initialized") {
          response.statusCode = 202;
          response.end();
          return;
        }
        const result =
          body.method === "initialize"
            ? {
                protocolVersion: "2025-06-18",
                capabilities: { tools: {} },
                serverInfo: { name: "fixture", version: "1" },
              }
            : body.method === "tools/list"
              ? {
                  tools: [
                    {
                      name: "health_check",
                      description: "Checks managed health.",
                      inputSchema: { type: "object", properties: {}, additionalProperties: false },
                    },
                  ],
                }
              : { content: [{ type: "text", text: "managed-ok" }], isError: false };
        response.end(JSON.stringify({ jsonrpc: "2.0", id: body.id, result }));
      });
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
      const port = (server.address() as AddressInfo).port;
      const manifest = {
        version: 1 as const,
        digest: `sha256:${"b".repeat(64)}` as `sha256:${string}`,
        bindings: [
          {
            resourceKind: "mcp-server" as const,
            resourceId: "mcp-pi",
            version: "v1",
            digest: `sha256:${"a".repeat(64)}` as `sha256:${string}`,
            transport: "streamable-http" as const,
            connectionRef: "connection-pi-mcp",
            credentialRef: "credential-pi-mcp",
            grantId: "grant-pi-mcp",
            networkPolicyRef: "network-pi-mcp",
            expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
            permissions: ["tools.call"],
            readOnly: false,
          },
        ],
      };
      const managed = await createManagedPiMcpTools(manifest, {
        CLOUD_AGENT_MCP_BROKER_URL: `http://127.0.0.1:${port}/mcp`,
        CLOUD_AGENT_MCP_TOKEN_MCP_PI: "broker-secret",
      });
      expect(managed.tools).toHaveLength(1);
      const tool = managed.tools[0];
      if (!tool) throw new Error("expected discovered MCP tool");
      const result = await tool.execute(
        "call-1",
        {},
        new AbortController().signal,
        undefined,
        {} as never,
      );
      expect(result.content).toEqual([{ type: "text", text: "managed-ok" }]);
      expect(methods).toEqual([
        "initialize",
        "notifications/initialized",
        "tools/list",
        "tools/call",
      ]);
      expect(authorizations.every((value) => value === "Bearer broker-secret")).toBe(true);
      expect(managed.metadataByToolName.get(tool.name)).toEqual({
        capabilityResourceId: "mcp-pi",
        mcpToolName: "health_check",
      });
      managed.close();
    } finally {
      await new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      );
    }
  });

  it("stops the turn without ending a tool activity when the MCP result is unknown", async () => {
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "mcp-server",
          resourceId: "mcp-pi",
          version: "v1",
          digest: `sha256:${"a".repeat(64)}`,
          transport: "streamable-http",
          connectionRef: "connection-pi-mcp",
          credentialRef: "credential-pi-mcp",
          grantId: "grant-pi-mcp",
          networkPolicyRef: "network-pi-mcp",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          permissions: ["tools.call"],
          readOnly: false,
        },
      ],
    };
    const emitted: Array<Record<string, unknown>> = [];
    let listener: AgentSessionEventListener | undefined;
    let aborted = false;
    let executions = 0;
    let resultUnknown = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: string | URL | Request, init?: RequestInit) => {
        const request = JSON.parse(String(init?.body)) as { id?: number; method: string };
        const headers = { "content-type": "application/json", "mcp-session-id": "pi-unknown" };
        if (request.method === "notifications/initialized")
          return new Response(null, { status: 202, headers });
        const result =
          request.method === "initialize"
            ? {
                protocolVersion: "2025-06-18",
                capabilities: { tools: {} },
                serverInfo: { name: "fixture", version: "1" },
              }
            : request.method === "tools/list"
              ? {
                  tools: [
                    {
                      name: "side_effect",
                      description: "Performs a managed side effect.",
                      inputSchema: { type: "object", properties: {} },
                    },
                  ],
                }
              : undefined;
        return new Response(
          JSON.stringify(
            result
              ? { jsonrpc: "2.0", id: request.id, result }
              : {
                  jsonrpc: "2.0",
                  id: request.id,
                  error: {
                    code: -32000,
                    message: resultUnknown ? "mcp_call_result_unknown" : "explicit tool failure",
                  },
                },
          ),
          { headers },
        );
      }),
    );
    try {
      const ordinaryFailure = await createManagedPiMcpTools(manifest as never, {
        CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
        CLOUD_AGENT_MCP_TOKEN_MCP_PI: "broker-secret",
      });
      await expect(
        ordinaryFailure.tools[0]?.execute(
          "mcp-call-failed",
          {},
          new AbortController().signal,
          undefined,
          {} as never,
        ),
      ).rejects.toThrow("MCP request failed");
      expect(ordinaryFailure.unknownToolCallIds).not.toContain("mcp-call-failed");
      ordinaryFailure.close();
      resultUnknown = true;

      const controller = startPiProviderRun(
        {
          execution: { id: "execution-pi-unknown", generation: 1 },
          workload: { provider: "pi", inputText: "use MCP", model: "model-pi" },
          workspaceDirectory: "/tmp/cloud-agents-pi-workspace",
          providerStateDirectory: "/tmp/cloud-agents-pi-state",
        },
        { payload: { apiKey: "secret-pi", baseURL: "https://gateway.example/v1" } },
        (message) => emitted.push(message),
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
              "base64url",
            ),
            CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
            CLOUD_AGENT_MCP_TOKEN_MCP_PI: "broker-secret",
          },
          async sessionFactory(options) {
            const tool = options.customTools?.[0];
            if (!tool) throw new Error("expected managed MCP tool");
            return {
              abort: async () => {
                aborted = true;
              },
              compact: async () => ({}) as never,
              dispose: () => {},
              getLastAssistantText: () => "must not complete",
              prompt: async () => {
                listener?.({
                  type: "tool_execution_start",
                  toolName: tool.name,
                  toolCallId: "mcp-call-unknown",
                  args: {},
                });
                try {
                  executions += 1;
                  await tool.execute(
                    "mcp-call-unknown",
                    {},
                    new AbortController().signal,
                    undefined,
                    {} as never,
                  );
                } catch (error) {
                  listener?.({
                    type: "tool_execution_end",
                    toolName: tool.name,
                    toolCallId: "mcp-call-unknown",
                    result: error,
                    isError: true,
                  });
                }
              },
              sessionFile: "/tmp/cloud-agents-pi-state/sessions/unknown.jsonl",
              subscribe(callback) {
                listener = callback;
                return () => {};
              },
              steer: async () => {},
            };
          },
        },
      );
      await expect(controller.result).rejects.toBeInstanceOf(
        ManagedCapabilityCallResultUnknownError,
      );
      expect(aborted).toBe(true);
      expect(executions).toBe(1);
      expect(
        emitted
          .filter(
            (message) =>
              message.eventType === "runtime.provider.activity" &&
              (message.payload as { itemId?: string }).itemId === "mcp-call-unknown",
          )
          .map((message) => (message.payload as { status?: string }).status),
      ).toEqual(["inProgress"]);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("uses the pinned RPC client, controlled third-party config, and durable session cursor", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-pi-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    const emitted: Array<Record<string, unknown>> = [];
    let sessionOptions: { model: string; apiKey: string } | undefined;
    let listener: AgentSessionEventListener | undefined;
    const skillManifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle",
          resourceId: "skill-pi",
          version: "v1",
          digest: `sha256:${"a".repeat(64)}`,
          grantId: "grant-pi",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          readOnly: true,
        },
      ],
    };
    try {
      const controller = startPiProviderRun(
        {
          execution: { id: "execution-pi", generation: 1 },
          workload: { provider: "pi", inputText: "write result.txt", model: null },
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
        },
        {
          payload: {
            apiKey: "secret-pi",
            baseURL: "https://gateway.example/v1",
            model: "model-pi",
          },
        },
        (message) => emitted.push(message),
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(
              JSON.stringify(skillManifest),
            ).toString("base64url"),
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_PI_ROOT: "/tmp/cloud-agents-skills/managed-skill",
          },
          async sessionFactory(options) {
            sessionOptions = options;
            return {
              abort: async () => {},
              compact: async () => ({}) as never,
              dispose: () => {},
              getLastAssistantText: () => "done",
              prompt: async () => {
                writeFileSync(join(workspace, "result.txt"), "pi");
                listener?.({
                  type: "tool_execution_start",
                  toolName: "write",
                  toolCallId: "tool-1",
                  args: { path: "result.txt" },
                });
                listener?.({
                  type: "tool_execution_end",
                  toolName: "write",
                  toolCallId: "tool-1",
                  result: {},
                  isError: false,
                });
              },
              sessionFile: join(root, "state/sessions/session.jsonl"),
              subscribe(callback) {
                listener = callback;
                return () => {};
              },
              steer: async () => {},
            };
          },
        },
      );
      await expect(controller.result).resolves.toMatchObject({
        output: { provider: "pi", model: "model-pi", text: "done" },
        providerResumeCursor: expect.stringContaining("session.jsonl"),
      });
      expect(sessionOptions).toMatchObject({ model: "model-pi", apiKey: "secret-pi" });
      const modelConfig = readFileSync(join(root, "state/agent/models.json"), "utf8");
      expect(modelConfig).toContain("https://gateway.example/v1");
      expect(modelConfig).toContain('"api":"openai-responses"');
      expect(modelConfig).not.toContain("secret-pi");
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "artifact",
          artifact: expect.objectContaining({ path: "result.txt" }),
        }),
      );
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemType: "skill",
            status: "completed",
            capabilityResourceId: "skill-pi",
            supportMode: "native",
          }),
        }),
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("retains managed Skill roots when native resume falls back to authoritative history", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-pi-resume-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    const resumeCursor = join(root, "state/sessions/native.jsonl");
    mkdirSync(join(root, "state/sessions"), { recursive: true });
    writeFileSync(resumeCursor, "");
    const skillRoot = "/tmp/cloud-agents-skills/managed-skill";
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle",
          resourceId: "skill-1",
          version: "v1",
          digest: `sha256:${"a".repeat(64)}`,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          readOnly: true,
        },
      ],
    };
    let calls = 0;
    const sessionOptions: Array<{ skillDirectories?: readonly string[] }> = [];
    try {
      const controller = startPiProviderRun(
        {
          execution: { id: "execution-pi-resume", generation: 2 },
          workload: {
            provider: "pi",
            inputText: "continue",
            model: "model-pi",
            conversationHistory: [{ role: "user", text: "history" }],
          },
          memoryDocuments: [
            {
              scope: "session",
              scopeId: "session-pi",
              memoryKey: "resume",
              revisionId: "revision-1",
              artifactId: "artifact-1",
              sha256: "b".repeat(64),
              contentType: "text/plain",
              content: "authoritative history",
            },
          ],
          providerResumeCursor: resumeCursor,
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
        },
        { payload: { apiKey: "secret-pi", baseURL: "https://gateway.example/v1" } },
        () => {},
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
              "base64url",
            ),
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT: skillRoot,
          },
          async sessionFactory(options) {
            calls += 1;
            sessionOptions.push(options);
            if (calls === 1) throw new Error(`invalid native cursor ${calls}`);
            return {
              abort: async () => {},
              compact: async () => ({}) as never,
              dispose: () => {},
              getLastAssistantText: () => "resumed",
              prompt: async () => {},
              sessionFile: join(root, "state/sessions/resumed.jsonl"),
              subscribe: (_callback) => () => {},
              steer: async () => {},
            };
          },
        },
      );
      await expect(controller.result).resolves.toMatchObject({
        output: { provider: "pi", text: "resumed" },
      });
      expect(sessionOptions).toHaveLength(2);
      expect(sessionOptions[0]?.skillDirectories).toEqual([skillRoot]);
      expect(sessionOptions[1]?.skillDirectories).toEqual([skillRoot]);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
