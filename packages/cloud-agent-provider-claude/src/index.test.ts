import { tmpdir } from "node:os";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("./claudeAgentSdkRuntime", () => ({
  startClaudeAgentSdkRun: vi.fn(() => ({
    result: Promise.resolve({
      type: "result",
      output: { provider: "claudeAgent", text: "done" },
    }),
    interrupt: () => undefined,
  })),
}));

import { createClaudeProvider, startClaudeProviderRun } from "./index";
import { startClaudeAgentSdkRun } from "./claudeAgentSdkRuntime";

describe("createClaudeProvider", () => {
  it("publishes a host-neutral ABI descriptor", async () => {
    const provider = createClaudeProvider();
    const descriptor = await provider.describe();
    expect(provider.providerKind).toBe("claudeAgent");
    expect(descriptor.providerKind).toBe("claudeAgent");
    expect(descriptor.adapterVersion).toBe("claude-agent-sdk-v2");
    expect(descriptor.runtime.name).toBe("@anthropic-ai/claude-agent-sdk");
  });

  it("accepts deployment credential aliases and uses its model as a default", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-provider-claude-credential-"));
    try {
      const run = startClaudeProviderRun(
        {
          execution: { id: "execution-credential" },
          workload: { provider: "claudeAgent", inputText: "hello" },
          workspaceDirectory: root,
        },
        {
          payload: {
            apiKey: "provider-key",
            baseURL: "https://provider.example/v1",
            model: "claude-test",
          },
        },
        () => undefined,
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS: "claudeAgent",
          },
        },
      );
      await run.result;
      const call = vi.mocked(startClaudeAgentSdkRun).mock.calls.at(-1)?.[0];
      expect(call?.input.workload.model).toBe("claude-test");
      expect(call?.environment.ANTHROPIC_BASE_URL).toBe("https://provider.example");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });
});
