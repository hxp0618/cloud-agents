import { randomUUID } from "node:crypto";
import {
  appendFileSync,
  closeSync,
  existsSync,
  fsyncSync,
  openSync,
  readFileSync,
  renameSync,
  writeSync,
} from "node:fs";
import { createServer } from "node:http";

// Fixture-only, pinned to the repository's Bun lockfile entry. This server
// shares the Worker network namespace; it is never part of a release image.
import { McpServer } from "../../node_modules/.bun/@modelcontextprotocol+sdk@1.30.0/node_modules/@modelcontextprotocol/sdk/dist/esm/server/mcp.js";
import { StreamableHTTPServerTransport } from "../../node_modules/.bun/@modelcontextprotocol+sdk@1.30.0/node_modules/@modelcontextprotocol/sdk/dist/esm/server/streamableHttp.js";
import { isInitializeRequest } from "../../node_modules/.bun/@modelcontextprotocol+sdk@1.30.0/node_modules/@modelcontextprotocol/sdk/dist/esm/types.js";

const descriptor = JSON.parse(readFileSync(process.env.MCP_ACCEPTANCE_DESCRIPTOR, "utf8"));
const token = descriptor.mcp?.[0]?.token;
const sideEffectFile = process.env.MCP_ACCEPTANCE_SIDE_EFFECT;
const logFile = process.env.MCP_ACCEPTANCE_LOG;
const marker = process.env.MCP_ACCEPTANCE_MARKER;
const disconnectArmFile = process.env.MCP_ACCEPTANCE_DISCONNECT_ARM;
if (
  typeof token !== "string" ||
  !token ||
  typeof sideEffectFile !== "string" ||
  typeof logFile !== "string" ||
  typeof marker !== "string" ||
  !marker
) {
  throw new Error("MCP acceptance fixture configuration is invalid.");
}

const log = (message) => appendFileSync(logFile, `${message}\n`, { flag: "a" });

const transports = new Map();
const http = createServer((request, response) => void handle(request, response));

function createMcpServer() {
  const server = new McpServer({ name: "cloud-agents-compose-acceptance", version: "1.0.0" });
  server.registerTool(
    "acceptance_marker",
    { description: "Returns an unpredictable acceptance marker.", inputSchema: {} },
    async () => ({ content: [{ type: "text", text: `SDK_MCP_${marker}` }] }),
  );
  server.registerTool(
    "acceptance_side_effect",
    { description: "Records an explicit acceptance marker.", inputSchema: {} },
    async () => {
      if (disconnectArmFile && existsSync(disconnectArmFile)) {
        const sideEffectDescriptor = openSync(sideEffectFile, "a");
        try {
          writeSync(sideEffectDescriptor, `${marker}\n`);
          fsyncSync(sideEffectDescriptor);
        } finally {
          closeSync(sideEffectDescriptor);
        }
        renameSync(disconnectArmFile, `${disconnectArmFile}.committed`);
        process.exit(137);
      }
      appendFileSync(sideEffectFile, `${marker}\n`, { flag: "a" });
      return { content: [{ type: "text", text: "side-effect-recorded" }] };
    },
  );
  return server;
}

async function handle(request, response) {
  log(`http ${request.method} ${request.url}`);
  if (request.url !== "/mcp") return response.writeHead(404).end();
  if (request.headers.authorization !== `Bearer ${token}`) {
    log("auth rejected");
    return response.writeHead(401).end(JSON.stringify({ error: "unauthorized" }));
  }
  if (request.method !== "POST") return response.writeHead(405, { allow: "POST" }).end();
  let body = "";
  for await (const chunk of request) body += chunk;
  let parsed;
  try {
    parsed = JSON.parse(body);
  } catch {
    log("json rejected");
    return response.writeHead(400).end();
  }
  log(`rpc ${typeof parsed.method === "string" ? parsed.method : "unknown"}`);
  const sessionId = request.headers["mcp-session-id"];
  let transport = sessionId ? transports.get(sessionId) : undefined;
  if (!transport && !sessionId && isInitializeRequest(parsed)) {
    transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => randomUUID(),
      enableJsonResponse: true,
      onsessioninitialized: (id) => transports.set(id, transport),
    });
    await createMcpServer().connect(transport);
  }
  if (!transport) return response.writeHead(400).end(JSON.stringify({ error: "session_required" }));
  await transport.handleRequest(request, response, parsed);
}

http.listen(48765, "127.0.0.1", () => process.stderr.write("mcp-acceptance-ready\n"));
process.on("SIGTERM", () => http.close(() => process.exit(0)));
