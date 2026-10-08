import { tmpdir } from "node:os";
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it, vi } from "vitest";

vi.mock("node:child_process", async (importOriginal) => {
  const actual = await importOriginal<typeof import("node:child_process")>();
  return {
    ...actual,
    spawnSync: vi.fn(() => ({ status: 0, stdout: "codex-cli 0.154.0", stderr: "" })),
  };
});

vi.mock("../src/codexAppServerRuntime", () => ({
  startCodexAppServerRun: vi.fn(() => ({
    result: Promise.resolve({
      type: "result",
      output: { text: '{"task":"thread-title","title":"Generated title"}' },
    }),
    interrupt: () => undefined,
  })),
}));

import { createCodexProvider, startCodexProviderRun } from "../src/index";
import { startCodexAppServerRun } from "../src/codexAppServerRuntime";

process.env.CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE = "single-tenant-trusted-v1";
process.env.CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS = "codex";

describe("createCodexProvider", () => {
  it("publishes a host-neutral ABI descriptor", async () => {
    const provider = createCodexProvider();
    const descriptor = await provider.describe();
    expect(provider.providerKind).toBe("codex");
    expect(descriptor.providerKind).toBe("codex");
    expect(descriptor.adapterVersion).toBe("codex-app-server-v2");
    expect(descriptor.runtime.name).toBe("codex");
  });

  it("injects the immutable default hook and no-tool marker for GenerateText", async () => {
    const provider = createCodexProvider();
    const session = await provider.createSession(
      { hostInstanceId: "instance-1", hostThreadId: "thread-1", configuration: {} },
      {
        workspace: {
          authority: "host",
          root: "/tmp/cloud-agent-provider-codex-index-test",
          generation: 1,
          readOnly: false,
        },
        credential: { acquire: async () => null },
        log: {
          debug: () => undefined,
          info: () => undefined,
          warn: () => undefined,
          error: () => undefined,
        },
      },
    );
    try {
      const base = {
        requestId: "request-1",
        protocolVersion: { major: 2, minor: 3 },
        executionId: "execution-1",
        generation: 1,
        occurredAt: "2026-08-09T00:00:00.000Z",
      } as const;
      const startResult = await session.execute({
        ...base,
        commandType: "StartSession",
        commandId: "start-1",
        payload: {
          runnerInput: {
            execution: { id: "execution-1" },
            workload: { provider: "codex", inputText: "start" },
            workspaceDirectory: "/tmp/cloud-agent-provider-codex-index-test",
          },
        },
      });
      const generateResult = await session.execute({
        ...base,
        commandType: "GenerateText",
        commandId: "generate-1",
        payload: { task: "thread-title", input: { message: "hello" } },
      });
      expect(startResult.messageType).toBe("Result");
      expect(generateResult.messageType).toBe("Result");
      const call = vi.mocked(startCodexAppServerRun).mock.calls.at(-1)?.[0];
      expect(call?.toolPolicyHookCommand).toContain(" -e ");
      expect(call?.environment.CLOUD_AGENT_CODEX_NO_TOOL_OPERATION).toBe("1");
      expect((await session.events[Symbol.asyncIterator]().next()).value).toBeDefined();
    } finally {
      await session.close();
    }
  });

  it("accepts deployment credential aliases and uses its model as a default", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-provider-codex-credential-"));
    try {
      const run = startCodexProviderRun(
        {
          execution: { id: "execution-credential" },
          workload: { provider: "codex", inputText: "hello" },
          workspaceDirectory: root,
          providerStateDirectory: join(root, "provider-state"),
        },
        {
          payload: {
            apiKey: "provider-key",
            baseURL: "https://provider.example/v1",
            model: "gpt-test",
          },
        },
        () => undefined,
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS: "codex",
          },
          codexToolPolicyHookCommand: "node /opt/cloud-agents/provider-host/index.mjs",
        },
      );
      await run.result;
      const call = vi.mocked(startCodexAppServerRun).mock.calls.at(-1)?.[0];
      expect(call?.input.workload.model).toBe("gpt-test");
      expect(call?.environment.OPENAI_BASE_URL).toBe("https://provider.example/v1");
      expect(call?.environment.CODEX_HOME).toBe(join(root, "provider-state", "codex-home"));
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("excludes managed MCP credentials from Codex shell inheritance", async () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-provider-codex-mcp-config-"));
    const manifest = Buffer.from(
      JSON.stringify({
        version: 1,
        bindings: [
          {
            resourceKind: "mcp-server",
            resourceId: "mcp-1",
            version: "1.0.0",
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
        ],
      }),
    ).toString("base64url");
    try {
      const run = startCodexProviderRun(
        {
          execution: { id: "execution-mcp-config" },
          workload: { provider: "codex", inputText: "hello" },
          workspaceDirectory: root,
          providerStateDirectory: join(root, "provider-state"),
        },
        { payload: { apiKey: "provider-key" } },
        () => undefined,
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS: "codex",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: manifest,
            CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:4000/mcp",
            CLOUD_AGENT_MCP_TOKEN_MCP_1: "managed-token",
          },
          codexToolPolicyHookCommand: "node /opt/cloud-agents/provider-host/index.mjs",
        },
      );
      await run.result;
      const call = vi.mocked(startCodexAppServerRun).mock.calls.at(-1)?.[0];
      expect(call?.mcpServers).toEqual({
        "cloud_agents_mcp-1": {
          url: "http://127.0.0.1:4000/mcp",
          bearer_token_env_var: "CLOUD_AGENT_MCP_TOKEN_MCP_1",
        },
      });
      const config = readFileSync(
        join(root, "provider-state", "codex-home", "config.toml"),
        "utf8",
      );
      expect(config).toContain("[shell_environment_policy]");
      expect(config).toContain('exclude = ["CLOUD_AGENT_MCP_TOKEN_MCP_1"]');
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("passes only Runtime-mounted Skill roots to the pinned app-server", async () => {
    const stateRoot = mkdtempSync(join(tmpdir(), "cloud-agent-provider-codex-skill-"));
    const skillParent = "/tmp/cloud-agents-skills";
    mkdirSync(skillParent, { recursive: true });
    const skillRoot = mkdtempSync(join(skillParent, "runtime-"));
    const manifest = Buffer.from(
      JSON.stringify({
        version: 1,
        bindings: [
          {
            resourceKind: "skill-bundle",
            resourceId: "skill-1",
            version: "1.0.0",
            digest: `sha256:${"b".repeat(64)}`,
            grantId: "grant-1",
            expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 300,
            readOnly: true,
          },
        ],
      }),
    ).toString("base64url");
    try {
      const run = startCodexProviderRun(
        {
          execution: { id: "execution-skill" },
          workload: { provider: "codex", inputText: "Use the managed skill." },
          workspaceDirectory: stateRoot,
          providerStateDirectory: join(stateRoot, "provider-state"),
        },
        { payload: { apiKey: "provider-key" } },
        () => undefined,
        {
          environment: {
            CLOUD_AGENT_PROVIDER_OUTER_SANDBOX_PROFILE: "single-tenant-trusted-v1",
            CLOUD_AGENT_PROVIDER_HOST_EXPERIMENTAL_PROVIDERS: "codex",
            CLOUD_AGENT_CAPABILITY_MANIFEST_B64: manifest,
            CLOUD_AGENT_SKILL_BUNDLE_SKILL_1_ROOT: skillRoot,
          },
          codexToolPolicyHookCommand: "node /opt/cloud-agents/provider-host/index.mjs",
        },
      );
      await run.result;
      const call = vi.mocked(startCodexAppServerRun).mock.calls.at(-1)?.[0];
      expect(call?.skillRoots).toEqual([join(skillRoot, "skills")]);
      expect(call?.environment.HOME).toBe(call?.environment.CODEX_HOME);
    } finally {
      rmSync(stateRoot, { recursive: true, force: true });
      rmSync(skillRoot, { recursive: true, force: true });
    }
  });
});
