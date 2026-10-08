import { createServer, type Server } from "node:http";
import { closeSync, mkdtempSync, openSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

import { describe, expect, it } from "vitest";

import { readCapabilityMaterialization, startManagedMcpBroker } from "../src/capabilityBroker";

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

async function brokerFor(port: number, token = "upstream-token", grant = manifest()) {
  return startManagedMcpBroker(grant, {
    version: 1,
    skills: [],
    mcp: [
      {
        resourceId: "server-1",
        version: "1.0.0",
        digest,
        transport: "streamable-http",
        endpoint: `http://127.0.0.1:${port}/mcp`,
        token,
        allowedHosts: ["127.0.0.1"],
      },
    ],
  });
}

function rpc(url: string, body: unknown, headers: Record<string, string> = {}) {
  return fetch(url, {
    method: "POST",
    headers: {
      authorization: "Bearer upstream-token",
      "content-type": "application/json",
      ...headers,
    },
    body: JSON.stringify(body),
  });
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
    const broker = await brokerFor(port);
    try {
      expect(broker).not.toBeNull();
      const response = await rpc(broker!.url, { jsonrpc: "2.0", id: 1, method: "tools/list" });
      expect(response.status).toBe(200);
      expect(await response.json()).toEqual({
        jsonrpc: "2.0",
        id: 1,
        result: { tools: [] },
      });
      expect(response.headers.get("mcp-session-id")).toBe("upstream-session");
      expect(response.headers.get("mcp-protocol-version")).toBe("2025-06-18");
      expect(receivedAuthorization).toBe("Bearer upstream-token");
      const followUp = await rpc(
        broker!.url,
        { jsonrpc: "2.0", id: 2, method: "tools/list" },
        {
          "mcp-session-id": "upstream-session",
          "mcp-protocol-version": "2025-06-18",
        },
      );
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

  it("marks only an ambiguous tools/call transport failure as result unknown", async () => {
    const methods: string[] = [];
    const upstream = createServer((request) => {
      const chunks: Buffer[] = [];
      request.on("data", (chunk: Buffer) => chunks.push(chunk));
      request.on("end", () => {
        const body = JSON.parse(Buffer.concat(chunks).toString("utf8")) as { method?: string };
        methods.push(body.method ?? "");
        request.socket.destroy();
      });
    });
    const port = await listen(upstream);
    const broker = await brokerFor(port);
    try {
      const toolCall = await rpc(broker!.url, {
        jsonrpc: "2.0",
        id: "call-1",
        method: "tools/call",
        params: { name: "side_effect", arguments: {} },
      });
      expect(toolCall.status).toBe(200);
      expect(await toolCall.json()).toEqual({
        jsonrpc: "2.0",
        id: "call-1",
        error: { code: -32000, message: "mcp_call_result_unknown" },
      });

      const list = await rpc(broker!.url, { jsonrpc: "2.0", id: 2, method: "tools/list" });
      expect(list.status).toBe(502);
      expect(await list.json()).toEqual({ error: "mcp_upstream_unavailable" });
      expect(methods).toEqual(["tools/call", "tools/list"]);
    } finally {
      await broker?.close();
      upstream.closeAllConnections?.();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }
  });

  it.each(["oversized body", "body failure", "deadline", "broker close"])(
    "cancels upstream work after %s",
    async (reason) => {
      let start!: () => void;
      let close!: () => void;
      const started = new Promise<void>((resolve) => {
        start = resolve;
      });
      const closed = new Promise<void>((resolve) => {
        close = resolve;
      });
      const upstream = createServer((_request, response) => {
        response.once("close", close);
        response.setHeader("content-type", "application/json");
        if (reason === "oversized body")
          response.setHeader("content-length", String(4 * 1024 * 1024 + 1));
        response.flushHeaders();
        response.write(" ");
        start();
        if (reason === "body failure") setTimeout(() => response.socket?.destroy(), 50).unref();
      });
      const port = await listen(upstream);
      const broker = await brokerFor(port);
      const pending = rpc(broker!.url, {
        jsonrpc: "2.0",
        id: 3,
        method: reason === "body failure" ? "tools/list" : "tools/call",
        params: {},
      }).then(
        async (response) => ({ status: response.status, body: await response.json() }),
        () => null,
      );
      try {
        await started;
        if (reason === "broker close") await broker!.close();
        const result = await Promise.race([
          pending,
          delay(reason === "deadline" ? 11_000 : 500, "timed out", { ref: false }),
        ]);
        if (reason === "broker close") expect(result).toBeNull();
        else if (reason === "body failure")
          expect(result).toEqual({
            status: 502,
            body: { error: "mcp_upstream_unavailable" },
          });
        else
          expect(result).toEqual({
            status: 200,
            body: {
              jsonrpc: "2.0",
              id: 3,
              error: { code: -32000, message: "mcp_call_result_unknown" },
            },
          });
        expect(
          await Promise.race([closed.then(() => true), delay(500, false, { ref: false })]),
        ).toBe(true);
      } finally {
        await broker?.close();
        upstream.closeAllConnections?.();
        await new Promise<void>((resolve) => upstream.close(() => resolve()));
        await pending;
      }
    },
    15_000,
  );

  it("forwards an explicit upstream JSON-RPC tool failure unchanged", async () => {
    const explicit = {
      jsonrpc: "2.0",
      id: 4,
      error: { code: -32603, message: "explicit tool failure" },
    };
    const upstream = createServer((_request, response) => {
      response.setHeader("content-type", "application/json");
      response.end(JSON.stringify(explicit));
    });
    const port = await listen(upstream);
    const broker = await brokerFor(port);
    try {
      const response = await rpc(broker!.url, {
        jsonrpc: "2.0",
        id: 4,
        method: "tools/call",
        params: {},
      });
      expect(response.status).toBe(200);
      expect(await response.json()).toEqual(explicit);
    } finally {
      await broker?.close();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }
  });

  it("rejects unknown credentials and refuses a materialization identity mismatch", async () => {
    const upstream = createServer((_request, response) => response.end("ok"));
    const port = await listen(upstream);
    const broker = await brokerFor(port, "correct-token");
    try {
      const response = await rpc(broker!.url, {}, { authorization: "Bearer wrong-token" });
      expect(response.status).toBe(401);
      expect(await response.json()).toEqual({
        error: "mcp_authorization_required",
      });
    } finally {
      await broker?.close();
      await new Promise<void>((resolve) => upstream.close(() => resolve()));
    }

    const root = mkdtempSync(join(tmpdir(), "cloud-agent-capability-fd-"));
    const path = join(root, "materialization.json");
    writeFileSync(path, JSON.stringify({ version: 1, mcp: [] }), {
      mode: 0o600,
    });
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

  it("rejects SSE materialization before starting the broker", () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-capability-sse-"));
    const path = join(root, "materialization.json");
    writeFileSync(
      path,
      JSON.stringify({
        version: 1,
        mcp: [
          {
            resourceId: "server-1",
            version: "1.0.0",
            digest,
            transport: "sse",
            endpoint: "http://127.0.0.1:8765/mcp",
            token: "token-1",
            allowedHosts: ["127.0.0.1"],
          },
        ],
        skills: [],
      }),
      { mode: 0o600 },
    );
    const fd = openSync(path, "r");
    try {
      expect(() =>
        readCapabilityMaterialization(
          { CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(fd) },
          manifest(),
        ),
      ).toThrow("MCP materialization is invalid");
    } finally {
      closeSync(fd);
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects duplicate MCP resource identities while reading materialization", () => {
    const base = manifest();
    const duplicateManifest = {
      ...base,
      bindings: [base.bindings[0]!, { ...base.bindings[0]!, resourceId: "server-2" }],
    };
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-capability-duplicate-"));
    const path = join(root, "materialization.json");
    writeFileSync(
      path,
      JSON.stringify({
        version: 1,
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
            resourceId: "server-1",
            version: "1.0.0",
            digest,
            transport: "streamable-http",
            endpoint: "http://127.0.0.1:8766/mcp",
            token: "token-2",
            allowedHosts: ["127.0.0.1"],
          },
        ],
      }),
      { mode: 0o600 },
    );
    const fd = openSync(path, "r");
    try {
      expect(() =>
        readCapabilityMaterialization(
          { CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(fd) },
          duplicateManifest,
        ),
      ).toThrow("duplicate MCP resources");
    } finally {
      closeSync(fd);
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("reads an inherited materialization FD repeatedly without consuming its offset", () => {
    const root = mkdtempSync(join(tmpdir(), "cloud-agent-capability-reread-"));
    const path = join(root, "materialization.json");
    const value = {
      version: 1 as const,
      mcp: [
        {
          resourceId: "server-1",
          version: "1.0.0",
          digest,
          transport: "streamable-http" as const,
          endpoint: "http://127.0.0.1:8765/mcp",
          token: "token-1",
          allowedHosts: ["127.0.0.1"],
        },
      ],
      skills: [],
    };
    writeFileSync(path, JSON.stringify(value), { mode: 0o600 });
    const fd = openSync(path, "r");
    try {
      const environment = {
        CLOUD_AGENT_CAPABILITY_MATERIALIZATION_FD: String(fd),
      };
      expect(readCapabilityMaterialization(environment, manifest())).toEqual(value);
      expect(readCapabilityMaterialization(environment, manifest())).toEqual(value);
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
    const broker = await brokerFor(
      port,
      "expired-token",
      manifest(Math.floor(Date.now() / 1000) - 1),
    );
    try {
      const response = await rpc(broker!.url, {}, { authorization: "Bearer expired-token" });
      expect(response.status).toBe(403);
      expect(await response.json()).toEqual({
        error: "mcp_capability_expired",
      });
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
