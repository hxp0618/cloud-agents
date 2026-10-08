import { tmpdir } from "node:os";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("../src/claudeAgentSdkRuntime", () => ({
  startClaudeAgentSdkRun: vi.fn(() => ({
    result: Promise.resolve({
      type: "result",
      output: { provider: "claudeAgent", text: "done" },
    }),
    interrupt: () => undefined,
  })),
}));

import { createClaudeProvider, startClaudeProviderRun } from "../src/index";
import { startClaudeAgentSdkRun } from "../src/claudeAgentSdkRuntime";

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
      expect(call?.environment.CLAUDE_CODE_EFFORT_LEVEL).toBe("unset");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("passes only Host-mounted Skill Bundles to the pinned Claude SDK", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-provider-claude-skill-"));
    const managedSkillRoot = "/tmp/cloud-agents-skills";
    mkdirSync(managedSkillRoot, { recursive: true });
    const bundleDirectory = mkdtempSync(join(managedSkillRoot, "claude-provider-skill-"));
    mkdirSync(join(bundleDirectory, ".claude-plugin"));
    mkdirSync(join(bundleDirectory, "skills", "managed-capability-acceptance"), {
      recursive: true,
    });
    writeFileSync(
      join(bundleDirectory, ".claude-plugin", "plugin.json"),
      JSON.stringify({ name: "managed-capability-acceptance" }),
    );
    writeFileSync(
      join(bundleDirectory, "skills", "managed-capability-acceptance", "SKILL.md"),
      "---\nname: managed-capability-acceptance\ndescription: acceptance\n---\n",
    );
    const digest = `sha256:${"a".repeat(64)}`;
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle",
          resourceId: "skill-1",
          version: "v1",
          digest,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
          readOnly: true,
        },
      ],
    };
    try {
      const run = startClaudeProviderRun(
        {
          execution: { id: "execution-skill" },
          workload: { provider: "claudeAgent", inputText: "hello" },
          workspaceDirectory: root,
        },
        { payload: { apiKey: "provider-key" } },
        () => undefined,
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS: "claudeAgent",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: Buffer.from(
              JSON.stringify({ ...manifest, digest }),
            ).toString("base64url"),
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT: bundleDirectory,
          },
        },
      );
      await run.result;
      expect(vi.mocked(startClaudeAgentSdkRun).mock.calls.at(-1)?.[0].skillDirectories).toEqual([
        bundleDirectory,
      ]);
      expect(
        vi.mocked(startClaudeAgentSdkRun).mock.calls.at(-1)?.[0].skillResourceIdsByQualifiedName,
      ).toEqual({
        "managed-capability-acceptance:managed-capability-acceptance": "skill-1",
      });
    } finally {
      rmSync(root, { recursive: true, force: true });
      rmSync(bundleDirectory, { recursive: true, force: true });
    }
  });
});
