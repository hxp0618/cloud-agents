import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";
import type { DeepSeekHarnessOptions } from "@deepseek-ai/dsh-sdk-client";
import { ManagedCapabilityCallResultUnknownError } from "@cloud-agents/cloud-agent-provider-api/internal";
import { startDeepSeekHarnessProviderRun } from "./index";

describe("deepseek-harness Provider", () => {
  it("rejects control characters in credential values before starting a session", () => {
    expect(() =>
      startDeepSeekHarnessProviderRun(
        {
          execution: { id: "execution-dsh-invalid-credential" },
          workload: {
            provider: "deepseek-harness",
            inputText: "hello",
            model: "model-dsh",
          },
          workspaceDirectory: "/tmp/cloud-agents-dsh-workspace",
          providerStateDirectory: "/tmp/cloud-agents-dsh-state",
        },
        { payload: { apiKey: "secret\ndsh", baseUrl: "https://gateway.example/v1" } },
        () => {},
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
          },
        },
      ),
    ).toThrow(/non-empty string/u);
  });

  it("composes the pinned MCP and Skill plugins from Host-managed materialization", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-dsh-capability-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    let harnessOptions: DeepSeekHarnessOptions | undefined;
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "mcp-server",
          resourceId: "server-1",
          version: "v1",
          digest: `sha256:${"a".repeat(64)}`,
          transport: "streamable-http",
          connectionRef: "connection-1",
          credentialRef: "credential-1",
          grantId: "grant-1",
          networkPolicyRef: "network-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          permissions: ["mcp.call"],
          readOnly: false,
        },
        {
          resourceKind: "skill-bundle",
          resourceId: "skill-1",
          version: "v1",
          digest: `sha256:${"b".repeat(64)}`,
          grantId: "grant-2",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          readOnly: true,
        },
      ],
    };
    const emitted: Array<Record<string, unknown>> = [];
    try {
      const controller = startDeepSeekHarnessProviderRun(
        {
          execution: { id: "execution-dsh-capability", generation: 1 },
          workload: {
            provider: "deepseek-harness",
            inputText: "use managed capabilities",
            model: null,
          },
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
        },
        {
          payload: {
            apiKey: "secret-dsh",
            baseUrl: "https://gateway.example/v1",
            model: "model-dsh",
          },
        },
        (message) => emitted.push(message),
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(JSON.stringify(manifest)).toString(
              "base64url",
            ),
            CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
            CLOUD_AGENT_MCP_TOKEN_SERVER_1: "short-lived-token",
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT: "/run/cloud-agents/skills/skill-1",
            CLOUD_AGENT_DEEPSEEK_HARNESS_BIN: "/opt/cloud-agents/dsh.mjs",
          },
          harnessFactory(options) {
            harnessOptions = options;
            return {
              close: async () => {},
              async run(_prompt, options) {
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "assistant/message",
                      data: {
                        message: {
                          content: [
                            {
                              type: "tool-call",
                              id: "mcp-call-1",
                              name: "mcp__ca_server-1_abcc4a81__read",
                              arguments: "{}",
                            },
                          ],
                        },
                      },
                    },
                  },
                });
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "tool/call",
                      data: {
                        callId: "mcp-call-1",
                        name: "mcp__ca_server-1_abcc4a81__read",
                        arguments: "{}",
                      },
                    },
                  },
                });
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "tool/result",
                      data: { message: { source: { callId: "mcp-call-1" } } },
                    },
                  },
                });
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "tool/call",
                      data: {
                        callId: "skill-call-1",
                        name: "skill",
                        arguments: '{"name":"managed-capability-acceptance"}',
                      },
                    },
                  },
                });
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "tool/result",
                      data: { message: { source: { callId: "skill-call-1" } } },
                    },
                  },
                });
                return {
                  sessionId: "session-capability",
                  finalResponse: "done",
                  events: [],
                  notifications: [],
                };
              },
            };
          },
        },
      );
      await expect(controller.result).resolves.toMatchObject({
        output: { provider: "deepseek-harness", text: "done" },
      });
      expect(harnessOptions?.patches).toHaveLength(2);
      const modelPatch = readFileSync(harnessOptions?.patches?.[0] ?? "", "utf8");
      expect(modelPatch).toContain("@deepseek-ai/dsh-llm-pi-ai");
      expect(modelPatch).toContain("api: openai-responses");
      expect(modelPatch).toContain("cloud-agents-openai");
      expect(modelPatch).toContain("maxTokens: 32768");
      expect(modelPatch).toContain("- id: tool-fs");
      expect(modelPatch).toContain("  disabled: true");
      expect(modelPatch).toContain("Use str_replace_editor for workspace files");
      const capabilityPatch = readFileSync(harnessOptions?.patches?.[1] ?? "", "utf8");
      expect(capabilityPatch).toContain("@deepseek-ai/dsh-mcp-client");
      expect(capabilityPatch).toContain("- id: skill-filesystem");
      expect(capabilityPatch).toContain("/run/cloud-agents/skills/skill-1/skills");
      expect(capabilityPatch).toMatch(/serverName: ca_server-1_[0-9a-f]{8}/u);
      expect(capabilityPatch).toContain(
        "Authorization: !!js '`Bearer ${process.env.CLOUD_AGENT_MCP_TOKEN_SERVER_1}`'",
      );
      expect(capabilityPatch).not.toContain("short-lived-token");
      expect(harnessOptions?.env?.CLOUD_AGENT_MCP_TOKEN_SERVER_1).toBe("short-lived-token");
      expect(harnessOptions?.env?.CLOUD_AGENT_MCP_BROKER_URL).toBe("http://127.0.0.1:8765/mcp");
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemType: "mcp__ca_server-1_abcc4a81__read",
            itemId: "mcp-call-1",
            capabilityResourceId: "server-1",
            supportMode: "emulated",
            status: "inProgress",
          }),
        }),
      );
      expect(
        emitted.filter(
          (message) =>
            message.eventType === "runtime.provider.activity" &&
            (message.payload as { itemId?: string }).itemId === "mcp-call-1" &&
            (message.payload as { status?: string }).status === "inProgress",
        ),
      ).toHaveLength(1);
      expect(
        emitted.filter(
          (message) =>
            message.eventType === "runtime.provider.activity" &&
            (message.payload as { itemId?: string }).itemId === "skill-call-1" &&
            (message.payload as { status?: string }).status === "inProgress",
        ),
      ).toHaveLength(1);
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemType: "skill",
            itemId: "skill-call-1",
            capabilityResourceId: "skill-1",
            supportMode: "emulated",
            status: "inProgress",
          }),
        }),
      );
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemType: "skill",
            itemId: "skill-call-1",
            capabilityResourceId: "skill-1",
            supportMode: "emulated",
            status: "completed",
          }),
        }),
      );
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemType: "mcp__ca_server-1_abcc4a81__read",
            itemId: "mcp-call-1",
            capabilityResourceId: "server-1",
            supportMode: "emulated",
            status: "completed",
          }),
        }),
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("uses SDK JSON-RPC with a pinned third-party route and durable session id", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-dsh-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    const emitted: Array<Record<string, unknown>> = [];
    let harnessOptions: DeepSeekHarnessOptions | undefined;
    try {
      const controller = startDeepSeekHarnessProviderRun(
        {
          execution: { id: "execution-dsh", generation: 1 },
          workload: { provider: "deepseek-harness", inputText: "write result.txt", model: null },
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
        },
        {
          payload: {
            apiKey: "secret-dsh",
            baseUrl: "https://gateway.example/v1",
            model: "model-dsh",
          },
        },
        (message) => emitted.push(message),
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_DEEPSEEK_HARNESS_BIN: "/opt/cloud-agents/dsh.mjs",
          },
          harnessFactory(options) {
            harnessOptions = options;
            return {
              close: async () => {},
              async run(_prompt, options) {
                writeFileSync(join(workspace, "result.txt"), "dsh");
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "assistant/message",
                      data: {
                        message: {
                          content: [
                            {
                              type: "tool-call",
                              id: "tool-1",
                              name: "write_file",
                              arguments: '{"path":"result.txt"}',
                            },
                          ],
                        },
                      },
                    },
                  },
                });
                return {
                  sessionId: options?.sessionId ?? "missing",
                  finalResponse: "done",
                  events: [],
                  notifications: [],
                };
              },
            };
          },
        },
      );
      await expect(controller.result).resolves.toMatchObject({
        output: { provider: "deepseek-harness", model: "model-dsh", text: "done" },
        providerResumeCursor: expect.stringMatching(/^session-/u),
      });
      expect(harnessOptions?.env?.DEEPSEEK_API_KEY).toBe("secret-dsh");
      expect(harnessOptions?.env?.DEEPSEEK_BASE_URL).toBe("https://gateway.example/v1");
      expect(harnessOptions?.dshBin).toBe("/opt/cloud-agents/dsh.mjs");
      const patch = readFileSync(join(root, "state/cloud-agents-model.cordis.yml"), "utf8");
      expect(patch).toContain('id: "model-dsh"');
      expect(patch).not.toContain("secret-dsh");
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "artifact",
          artifact: expect.objectContaining({ path: "result.txt" }),
        }),
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("fails closed when a tool result reports an error", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-dsh-tool-failure-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    const emitted: Array<Record<string, unknown>> = [];
    try {
      const controller = startDeepSeekHarnessProviderRun(
        {
          execution: { id: "execution-dsh-tool-failure", generation: 1 },
          workload: {
            provider: "deepseek-harness",
            inputText: "use the managed tool",
            model: null,
          },
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
        },
        {
          payload: {
            apiKey: "secret-dsh",
            baseUrl: "https://gateway.example/v1",
            model: "model-dsh",
          },
        },
        (message) => emitted.push(message),
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
          },
          harnessFactory() {
            return {
              close: async () => {},
              async run(_prompt, options) {
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "assistant/message",
                      data: {
                        message: {
                          content: [
                            {
                              type: "tool-call",
                              id: "mcp-call-failed",
                              name: "mcp__managed__read",
                              arguments: "{}",
                            },
                          ],
                        },
                      },
                    },
                  },
                });
                options?.onNotification?.({
                  method: "session.event",
                  params: {
                    event: {
                      type: "tool/result",
                      data: {
                        message: {
                          source: { callId: "mcp-call-failed", name: "mcp__managed__read" },
                          isError: true,
                        },
                      },
                    },
                  },
                });
                return {
                  sessionId: options?.sessionId ?? "missing",
                  finalResponse: "ignored",
                  events: [],
                  notifications: [],
                };
              },
            };
          },
        },
      );
      await expect(controller.result).rejects.toThrow("managed tool failed");
      expect(emitted).toContainEqual(
        expect.objectContaining({
          type: "event",
          eventType: "runtime.provider.activity",
          payload: expect.objectContaining({
            itemId: "mcp-call-failed",
            status: "failed",
          }),
        }),
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it.each(["notification", "run-result"] as const)(
    "stops without ending the active tool when an MCP %s contains the unknown marker",
    async (source) => {
      const root = mkdtempSync(join(tmpdir(), "cloud-agent-dsh-result-unknown-"));
      const workspace = join(root, "workspace");
      mkdirSync(workspace);
      const emitted: Array<Record<string, unknown>> = [];
      let closed = 0;
      const unknownNotification = {
        method: "session.event",
        params: {
          event: {
            type: "tool/result",
            data: {
              message: {
                source: { callId: "mcp-call-unknown", name: "mcp__managed__side_effect" },
                isError: true,
                error: { code: -32000, message: "mcp_call_result_unknown" },
              },
            },
          },
        },
      };
      try {
        const controller = startDeepSeekHarnessProviderRun(
          {
            execution: { id: `execution-dsh-${source}`, generation: 1 },
            workload: {
              provider: "deepseek-harness",
              inputText: "use the managed side effect",
              model: "model-dsh",
            },
            workspaceDirectory: workspace,
            providerStateDirectory: join(root, "state"),
          },
          { payload: { apiKey: "secret-dsh", baseUrl: "https://gateway.example/v1" } },
          (message) => emitted.push(message),
          {
            environment: {
              CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            },
            harnessFactory() {
              return {
                close: async () => {
                  closed += 1;
                },
                async run(_prompt, options) {
                  options?.onNotification?.({
                    method: "session.event",
                    params: {
                      event: {
                        type: "assistant/message",
                        data: {
                          message: {
                            content: [
                              {
                                type: "tool-call",
                                id: "mcp-call-unknown",
                                name: "mcp__managed__side_effect",
                                arguments: "{}",
                              },
                            ],
                          },
                        },
                      },
                    },
                  });
                  if (source === "notification") options?.onNotification?.(unknownNotification);
                  return {
                    sessionId: options?.sessionId ?? "missing",
                    finalResponse: "must not complete",
                    events: [],
                    notifications: source === "run-result" ? [unknownNotification] : [],
                  };
                },
              };
            },
          },
        );
        await expect(controller.result).rejects.toBeInstanceOf(
          ManagedCapabilityCallResultUnknownError,
        );
        expect(closed).toBeGreaterThan(0);
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
        rmSync(root, { recursive: true, force: true });
      }
    },
  );

  it("reconstructs a resumed Turn into a fresh SDK session", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-dsh-resume-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    let observedPrompt = "";
    let observedSessionId = "";
    try {
      const controller = startDeepSeekHarnessProviderRun(
        {
          execution: { id: "execution-dsh-resume", generation: 1 },
          workload: {
            provider: "deepseek-harness",
            inputText: "continue",
            model: "model-dsh",
            conversationHistory: [{ role: "user", text: "prior request" }],
          },
          workspaceDirectory: workspace,
          providerStateDirectory: join(root, "state"),
          providerResumeCursor: "session-persisted",
        },
        { payload: { apiKey: "secret-dsh", baseUrl: "https://gateway.example/v1" } },
        () => {},
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
          },
          harnessFactory() {
            return {
              close: async () => {},
              async run(prompt, options) {
                observedPrompt = String(prompt);
                observedSessionId = options?.sessionId ?? "";
                return {
                  sessionId: observedSessionId,
                  finalResponse: "continued",
                  events: [],
                  notifications: [],
                };
              },
            };
          },
        },
      );
      await expect(controller.result).resolves.toMatchObject({
        providerResumeCursor: expect.stringMatching(/^session-/u),
      });
      expect(observedSessionId).not.toBe("session-persisted");
      expect(observedPrompt).toContain("prior request");
      expect(observedPrompt).toContain("continue");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
