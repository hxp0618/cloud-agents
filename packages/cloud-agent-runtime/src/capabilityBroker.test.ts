import { createServer, type Server } from "node:http";
import { closeSync, mkdtempSync, openSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { readCapabilityMaterialization, startManagedMcpBroker } from "./capabilityBroker";

const digest = `sha256:${"a".repeat(64)}` as const;

function manifest(expiresAtUnixSeconds = Math.floor(Date.now() / 1000) + 300) {
  return {
    version: 1 as const,
    bindings: [
      {
        resourceKind: "mcp-server" as const,
        resourceId: "server-1",
        version: "1.0.0",
        digest,
        transport: "streamable-http" as const,
        connectionRef: "connection-1",
        credentialRef: "credential-1",
        grantId: "grant-1",
        networkPolicyRef: "network-1",
        expiresAtUnixSeconds,
        permissions: ["mcp.call"],
        readOnly: false,
      },
    ],
    digest,
  };
}

async function listen(server: Server): Promise<number> {
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("server did not bind");
  return address.port;
}

describe("managed MCP broker", () => {
  it("forwards an authorized streamable HTTP call through loopback", async () => {
    let receivedAuthorization = "";
    let receivedSession = "";
    let receivedProtocolVersion = "";
    const upstream = createServer((request, response) => {
      receivedAuthorization = request.headers.authorization ?? "";
      receivedSession =
        typeof request.headers["mcp-session-id"] === "string"
          ? request.headers["mcp-session-id"]
          : "";
      receivedProtocolVersion =
        typeof request.headers["mcp-protocol-version"] === "string"
          ? request.headers["mcp-protocol-version"]
          : "";
      response.setHeader("content-type", "application/json");
      response.setHeader("mcp-session-id", "upstream-session");
      response.setHeader("mcp-protocol-version", "2025-06-18");
      response.end(JSON.stringify({ jsonrpc: "2.0", id: 1, result: { tools: [] } }));
    });
    const port = await listen(upstream);
    const broker = await startManagedMcpBroker(manifest(), {
      version: 1,
      skills: [],
      mcp: [
        {
          resourceId: "server-1",
          version: "1.0.0",
          digest,
          transport: "streamable-http",
          endpoint: `http://127.0.0.1:${port}/mcp`,
          token: "upstream-token",
          allowedHosts: ["127.0.0.1"],
        },
      ],
    });
    try {
      expect(broker).not.toBeNull();
      const response = await fetch(broker!.url, {
        method: "POST",
        headers: {
          authorization: "Bearer upstream-token",
          "content-type": "application/json",
        },
        body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" }),
      });
      expect(response.status).toBe(200);
      expect(await response.json()).toEqual({
        jsonrpc: "2.0",
        id: 1,
        result: { tools: [] },
      });
      expect(response.headers.get("mcp-session-id")).toBe("upstream-session");
      expect(response.headers.get("mcp-protocol-version")).toBe("2025-06-18");
      expect(receivedAuthorization).toBe("Bearer upstream-token");
      const followUp = await fetch(broker!.url, {
        method: "POST",
        headers: {
          authorization: "Bearer upstream-token",
          "mcp-session-id": "upstream-session",
          "mcp-protocol-version": "2025-06-18",
        },
        body: JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/list" }),
      });
      expect(followUp.status).toBe(200);
      expect(receivedSession).toBe("upstream-session");
      expect(receivedProtocolVersion).toBe("2025-06-18");
      const stream = await fetch(broker!.url, {
        headers: { authorization: "Bearer upstream-token" },
      });
      expect(stream.status).toBe(405);
      expect(stream.headers.get("allow")).toBe("POST");
      expect(await stream.json()).toEqual({ error: "mcp_stream_unavailable" });
    } finally {
      await broker?.close();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }
  });

  it("rejects unknown credentials and refuses a materialization identity mismatch", async () => {
    const upstream = createServer((_request, response) => response.end("ok"));
    const port = await listen(upstream);
    const broker = await startManagedMcpBroker(manifest(), {
      version: 1,
      skills: [],
      mcp: [
        {
          resourceId: "server-1",
          version: "1.0.0",
          digest,
          transport: "streamable-http",
          endpoint: `http://127.0.0.1:${port}/mcp`,
          token: "correct-token",
          allowedHosts: ["127.0.0.1"],
        },
      ],
    });
    try {
      const response = await fetch(broker!.url, {
        method: "POST",
        headers: { authorization: "Bearer wrong-token" },
        body: "{}",
      });
      expect(response.status).toBe(401);
      expect(await response.json()).toEqual({ error: "mcp_authorization_required" });
    } finally {
      await broker?.close();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }

    const root = mkdtempSync(join(tmpdir(), "cloud-agent-capability-fd-"));
    const path = join(root, "materialization.json");
    writeFileSync(path, JSON.stringify({ version: 1, mcp: [] }), { mode: 0o600 });
    const fd = openSync(path, "r");
    try {
      expect(() =>
        readCapabilityMaterialization(
          { CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(fd) },
          manifest(),
        ),
      ).toThrow("does not match");
    } finally {
      closeSync(fd);
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects an expired grant before calling the upstream MCP server", async () => {
    let calls = 0;
    const upstream = createServer((_request, response) => {
      calls += 1;
      response.end("ok");
    });
    const port = await listen(upstream);
    const broker = await startManagedMcpBroker(manifest(Math.floor(Date.now() / 1000) - 1), {
      version: 1,
      skills: [],
      mcp: [
        {
          resourceId: "server-1",
          version: "1.0.0",
          digest,
          transport: "streamable-http",
          endpoint: `http://127.0.0.1:${port}/mcp`,
          token: "expired-token",
          allowedHosts: ["127.0.0.1"],
        },
      ],
    });
    try {
      const response = await fetch(broker!.url, {
        method: "POST",
        headers: { authorization: "Bearer expired-token" },
        body: "{}",
      });
      expect(response.status).toBe(403);
      expect(await response.json()).toEqual({ error: "mcp_capability_expired" });
      expect(calls).toBe(0);
    } finally {
      await broker?.close();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }
  });

  it("rejects MCP bindings that collide after environment-name normalization", async () => {
    const base = manifest();
    const first = base.bindings[0]!;
    const collisionManifest = {
      ...base,
      bindings: [first, { ...first, resourceId: "server_1", grantId: "grant-2" }],
    };
    await expect(
      startManagedMcpBroker(collisionManifest, {
        version: 1,
        skills: [],
        mcp: [
          {
            resourceId: "server-1",
            version: "1.0.0",
            digest,
            transport: "streamable-http",
            endpoint: "http://127.0.0.1:8765/mcp",
            token: "token-1",
            allowedHosts: ["127.0.0.1"],
          },
          {
            resourceId: "server_1",
            version: "1.0.0",
            digest,
            transport: "streamable-http",
            endpoint: "http://127.0.0.1:8766/mcp",
            token: "token-2",
            allowedHosts: ["127.0.0.1"],
          },
        ],
      }),
    ).rejects.toThrow("environment references collide");
  });
});
