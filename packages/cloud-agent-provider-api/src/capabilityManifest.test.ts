import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import {
  managedMcpConfiguration,
  managedSkillBundleDirectories,
  readCapabilityManifest,
} from "./capabilityManifest";
import { providerEnvironment } from "./internalExecution";

function encoded(value: unknown): string {
  const bytes = Buffer.from(JSON.stringify(value));
  return bytes.toString("base64url");
}

describe("readCapabilityManifest", () => {
  it("accepts only an unexpired opaque MCP binding", () => {
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
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 60,
          permissions: ["mcp.call"],
          readOnly: false,
        },
      ],
    };
    const parsed = readCapabilityManifest({
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(manifest),
    });
    expect(parsed?.bindings[0]?.resourceId).toBe("server-1");
    expect(parsed?.digest).toBe(
      `sha256:${createHash("sha256").update(JSON.stringify(manifest)).digest("hex")}`,
    );
  });

  it("fails closed for expired or secret-bearing skill bindings", () => {
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle",
          resourceId: "bundle-1",
          version: "v1",
          digest: `sha256:${"b".repeat(64)}`,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) - 1,
          permissions: ["skill.load"],
          readOnly: true,
          token: "must-not-be-here",
        },
      ],
    };
    expect(() =>
      readCapabilityManifest({
        CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(manifest),
      }),
    ).toThrow(/invalid or expired/u);
    manifest.bindings[0]!.expiresAtUnixSeconds = Math.floor(Date.now() / 1000) + 60;
    expect(() =>
      readCapabilityManifest({
        CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(manifest),
      }),
    ).toThrow(/Skill capability binding is invalid/u);
  });

  it("accepts the Go manifest encoding for an empty Skill permission set", () => {
    const manifest = {
      version: 1,
      bindings: [
        {
          resourceKind: "skill-bundle",
          resourceId: "bundle-1",
          version: "v1",
          digest: `sha256:${"b".repeat(64)}`,
          grantId: "grant-1",
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 60,
          readOnly: true,
        },
      ],
    };
    expect(
      readCapabilityManifest({
        CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(manifest),
      })?.bindings[0]?.permissions,
    ).toEqual([]);
  });

  it("materializes MCP only through the loopback broker and short-lived token env", () => {
    const raw = {
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
          expiresAtUnixSeconds: Math.floor(Date.now() / 1000) + 60,
          permissions: ["mcp.call"],
          readOnly: false,
        },
      ],
    };
    const parsed = readCapabilityManifest({
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(raw),
    });
    expect(() =>
      managedMcpConfiguration(parsed, {
        CLOUD_AGENT_MCP_BROKER_URL: "https://evil.example/mcp",
        CLOUD_AGENT_MCP_TOKEN_SERVER_1: "secret",
      }),
    ).toThrow(/loopback/u);
    const config = managedMcpConfiguration(parsed, {
      CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
      CLOUD_AGENT_MCP_TOKEN_SERVER_1: "secret",
    });
    expect(config.codexServers["cloud_agents_server-1"]?.bearer_token_env_var).toBe(
      "CLOUD_AGENT_MCP_TOKEN_SERVER_1",
    );
    expect(config.claudeServers["cloud_agents_server-1"]?.headers).toEqual({
      Authorization: "Bearer secret",
    });
    expect(() =>
      managedMcpConfiguration(parsed, {
        CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
        CLOUD_AGENT_MCP_TOKEN_SERVER_1: "token\nforbidden",
      }),
    ).toThrow(/credential/u);
    const unsupportedTransport = readCapabilityManifest({
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded({
        ...raw,
        bindings: [{ ...raw.bindings[0], transport: "sse" }],
      }),
    });
    expect(() =>
      managedMcpConfiguration(unsupportedTransport, {
        CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
        CLOUD_AGENT_MCP_TOKEN_SERVER_1: "secret",
      }),
    ).toThrow(/streamable-http transport/u);
  });

  it("redacts Host-managed MCP tokens from Provider diagnostics", () => {
    const { redact } = providerEnvironment(
      {
        CLOUD_AGENT_MCP_TOKEN_SERVER_1: "host-managed-token",
      },
      null,
    );
    expect(redact("MCP failed with host-managed-token")).toBe("MCP failed with [REDACTED]");
  });

  it("fails closed when opaque IDs collide after environment-name normalization", () => {
    const expiresAtUnixSeconds = Math.floor(Date.now() / 1000) + 60;
    const mcpManifest = {
      version: 1,
      bindings: ["server-1", "server_1"].map((resourceId) => ({
        resourceKind: "mcp-server",
        resourceId,
        version: "v1",
        digest: `sha256:${"a".repeat(64)}`,
        transport: "streamable-http",
        connectionRef: "connection-1",
        credentialRef: "credential-1",
        grantId: `grant-${resourceId}`,
        networkPolicyRef: "network-1",
        expiresAtUnixSeconds,
        permissions: ["mcp.call"],
        readOnly: false,
      })),
    };
    const parsedMcp = readCapabilityManifest({
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(mcpManifest),
    });
    expect(() =>
      managedMcpConfiguration(parsedMcp, {
        CLOUD_AGENT_MCP_BROKER_URL: "http://127.0.0.1:8765/mcp",
        CLOUD_AGENT_MCP_TOKEN_SERVER_1: "secret",
      }),
    ).toThrow(/environment references collide/u);

    const skillManifest = {
      version: 1,
      bindings: ["bundle-1", "bundle_1"].map((resourceId) => ({
        resourceKind: "skill-bundle",
        resourceId,
        version: "v1",
        digest: `sha256:${"b".repeat(64)}`,
        grantId: `grant-${resourceId}`,
        expiresAtUnixSeconds,
        readOnly: true,
      })),
    };
    const parsedSkills = readCapabilityManifest({
      CLOUD_AGENT_CAPABILITY_MANIFEST_B64: encoded(skillManifest),
    });
    expect(() =>
      managedSkillBundleDirectories(parsedSkills, {
        CLOUD_AGENT_SKILL_BUNDLE_BUNDLE_1_ROOT: "/run/cloud-agents/skills/bundle-1",
      }),
    ).toThrow(/environment references collide/u);
  });
});
