import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  codexMcpServersOverride,
  isCodexManagedMcpInventoryAttested,
  isCodexRuntimeIsolationConfigAttested,
} from "./codexRuntimeIsolation";

const officialCodex0145ConfigRead = JSON.parse(
  readFileSync(new URL("../test-fixtures/codex-0.145.0-config-read.json", import.meta.url), "utf8"),
) as unknown;

describe("Codex runtime-isolation configuration attestation", () => {
  it("clears deferred MCP exposure so connected Host-managed tools reach the model", () => {
    expect(
      codexMcpServersOverride({
        "cloud_agents_mcp-1": {
          url: "http://127.0.0.1:43123/mcp",
          bearer_token_env_var: "CLOUD_AGENT_MCP_TOKEN_MCP_1",
        },
      }),
    ).toBe(
      'mcp_servers={"cloud_agents_mcp-1"={url="http://127.0.0.1:43123/mcp",bearer_token_env_var="CLOUD_AGENT_MCP_TOKEN_MCP_1",required=true,default_tools_approval_mode="approve",omit_tools_from=[]}}',
    );
  });

  it("accepts the official Codex 0.145 null shell-policy defaults when no exclusions are expected", () => {
    expect(isCodexRuntimeIsolationConfigAttested(officialCodex0145ConfigRead, [])).toBe(true);
  });

  it("fails closed when the official null shell policy cannot prove a required exclusion", () => {
    expect(
      isCodexRuntimeIsolationConfigAttested(
        officialCodex0145ConfigRead,
        [],
        ["CLOUD_AGENT_GATEWAY_TOKEN"],
      ),
    ).toBe(false);
  });

  it("requires every explicit exclusion to be a string", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { exclude: unknown } };
    };
    response.config.shell_environment_policy.exclude = ["SAFE_NAME", 42];
    expect(isCodexRuntimeIsolationConfigAttested(response, [])).toBe(false);
  });

  it("rejects shell policy fields that can inject or broaden the child environment", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { set: unknown } };
    };
    response.config.shell_environment_policy.set = {
      NODE_OPTIONS: "--require=/workspace/untrusted.js",
    };
    expect(isCodexRuntimeIsolationConfigAttested(response, [])).toBe(false);
  });

  it("accepts an explicitly attested required exclusion", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: { shell_environment_policy: { exclude: unknown } };
    };
    response.config.shell_environment_policy.exclude = ["cloud_agent_gateway_token"];
    expect(isCodexRuntimeIsolationConfigAttested(response, [], ["CLOUD_AGENT_GATEWAY_TOKEN"])).toBe(
      true,
    );
  });

  it("attests the exact loopback Host-managed MCP server and credential exclusion", () => {
    const response = structuredClone(officialCodex0145ConfigRead) as {
      config: {
        mcp_servers: Record<string, unknown>;
        shell_environment_policy: { exclude: unknown };
      };
    };
    response.config.mcp_servers = {
      "cloud_agents_mcp-1": {
        url: "http://127.0.0.1:43123/mcp",
        bearer_token_env_var: "CLOUD_AGENT_MCP_TOKEN_MCP_1",
        environment_id: "local",
        enabled: true,
        tool_timeout_sec: null,
        required: true,
        default_tools_approval_mode: "approve",
        omit_tools_from: [],
      },
    };
    response.config.shell_environment_policy.exclude = ["CLOUD_AGENT_MCP_TOKEN_MCP_1"];
    expect(
      isCodexRuntimeIsolationConfigAttested(response, [
        {
          name: "cloud_agents_mcp-1",
          url: "http://127.0.0.1:43123/mcp",
          bearerTokenEnvVar: "CLOUD_AGENT_MCP_TOKEN_MCP_1",
        },
      ]),
    ).toBe(true);

    const deferred = structuredClone(response) as typeof response;
    const deferredServer = deferred.config.mcp_servers["cloud_agents_mcp-1"] as Record<string, unknown>;
    deferredServer.omit_tools_from = ["deferred"];
    expect(
      isCodexRuntimeIsolationConfigAttested(deferred, [
        {
          name: "cloud_agents_mcp-1",
          url: "http://127.0.0.1:43123/mcp",
          bearerTokenEnvVar: "CLOUD_AGENT_MCP_TOKEN_MCP_1",
        },
      ]),
    ).toBe(false);
  });

  it("attests the exact connected MCP inventory exposed by app-server", () => {
    expect(
      isCodexManagedMcpInventoryAttested(
        {
          data: [
            {
              name: "cloud_agents_mcp-1",
              authStatus: "bearerToken",
              runtimeStatus: "connected",
              tools: { acceptance_side_effect: { name: "acceptance_side_effect" } },
              resources: [],
              resourceTemplates: [],
            },
          ],
          nextCursor: null,
        },
        ["cloud_agents_mcp-1"],
      ),
    ).toBe(true);
  });

  it("fails closed for incomplete, paginated, or unexpected MCP inventory", () => {
    const response = {
      data: [
        {
          name: "cloud_agents_mcp-1",
          authStatus: "bearerToken",
          runtimeStatus: "starting",
          tools: {},
        },
      ],
      nextCursor: "more",
    };
    expect(isCodexManagedMcpInventoryAttested(response, ["cloud_agents_mcp-1"])).toBe(false);
    expect(
      isCodexManagedMcpInventoryAttested({ ...response, nextCursor: null }, [
        "cloud_agents_mcp-1",
        "unexpected",
      ]),
    ).toBe(false);
  });
});
