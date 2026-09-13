import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";
import type { AgentSessionEventListener } from "@earendil-works/pi-coding-agent";
import { startPiProviderRun } from "./index";

describe("Pi Provider", () => {
  it("uses the pinned RPC client, controlled third-party config, and durable session cursor", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-pi-"));
    const workspace = join(root, "workspace");
    mkdirSync(workspace);
    const emitted: Array<Record<string, unknown>> = [];
    let sessionOptions: { model: string; apiKey: string } | undefined;
    let listener: AgentSessionEventListener | undefined;
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
          environment: { CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1" },
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
      expect(modelConfig).not.toContain("secret-pi");
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
});
