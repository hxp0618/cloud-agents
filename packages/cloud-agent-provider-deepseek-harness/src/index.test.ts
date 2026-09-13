import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";
import type { DeepSeekHarnessOptions } from "@deepseek-ai/dsh-sdk-client";
import { startDeepSeekHarnessProviderRun } from "./index";

describe("deepseek-harness Provider", () => {
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
                              name: "write",
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
