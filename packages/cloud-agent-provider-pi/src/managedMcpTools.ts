import { createHash } from "node:crypto";

import type { ToolDefinition } from "@earendil-works/pi-coding-agent";
import {
  ManagedCapabilityCallResultUnknownError,
  ManagedCapabilityUnavailableError,
  capabilityTokenEnvironmentName,
  isManagedMcpCallResultUnknown,
  managedMcpConfiguration,
  recordValue,
  stringValue,
  type RuntimeCapabilityBinding,
  type RuntimeCapabilityManifest,
} from "@cloud-agents/cloud-agent-provider-api/internal";

const MCP_PROTOCOL_VERSION = "2025-06-18";
const MAX_MCP_RESPONSE_BYTES = 4 * 1024 * 1024;
const MAX_MCP_SCHEMA_BYTES = 64 * 1024;
const MAX_MCP_TOOLS = 128;
const MAX_MCP_PAGES = 32;
const MAX_MCP_RESULT_TEXT = 64 * 1024;

export type ManagedPiMcpToolMetadata = Readonly<{
  capabilityResourceId: string;
  mcpToolName: string;
}>;

export type ManagedPiMcpTools = Readonly<{
  tools: ReadonlyArray<ToolDefinition>;
  metadataByToolName: ReadonlyMap<string, ManagedPiMcpToolMetadata>;
  unknownToolCallIds: ReadonlySet<string>;
  resultUnknown: Promise<ManagedCapabilityCallResultUnknownError>;
  close(): void;
}>;

type JsonRpcResponse = Readonly<{
  jsonrpc: "2.0";
  id?: unknown;
  result?: unknown;
  error?: unknown;
}>;

type DiscoveredMcpTool = Readonly<{
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
}>;

class ManagedMcpClient {
  private requestId = 0;
  private sessionId: string | undefined;
  private protocolVersion = MCP_PROTOCOL_VERSION;

  constructor(
    private readonly brokerUrl: string,
    private readonly token: string,
    private readonly lifetimeSignal: AbortSignal,
  ) {}

  async initialize(): Promise<void> {
    const result = recordValue(
      await this.request("initialize", {
        protocolVersion: MCP_PROTOCOL_VERSION,
        capabilities: {},
        clientInfo: { name: "cloud-agents-pi", version: "0.85.1" },
      }),
    );
    const negotiated = stringValue(result?.protocolVersion);
    if (!negotiated) throw unavailable("MCP initialize response is invalid.");
    this.protocolVersion = negotiated;
    await this.notify("notifications/initialized");
  }

  async listTools(): Promise<ReadonlyArray<DiscoveredMcpTool>> {
    const tools: DiscoveredMcpTool[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < MAX_MCP_PAGES; page += 1) {
      const result = recordValue(await this.request("tools/list", cursor ? { cursor } : {}));
      if (!result || !Array.isArray(result.tools))
        throw unavailable("MCP tools/list response is invalid.");
      for (const value of result.tools) {
        const tool = readDiscoveredTool(value);
        if (tools.some((existing) => existing.name === tool.name))
          throw unavailable("MCP tools/list returned duplicate tool names.");
        tools.push(tool);
        if (tools.length > MAX_MCP_TOOLS)
          throw unavailable("MCP tools/list exceeded the managed limit.");
      }
      cursor = optionalCursor(result.nextCursor);
      if (!cursor) return tools;
    }
    throw unavailable("MCP tools/list exceeded the managed page limit.");
  }

  async callTool(name: string, params: unknown, signal: AbortSignal | undefined): Promise<unknown> {
    return this.request("tools/call", { name, arguments: params }, signal);
  }

  private async request(method: string, params: unknown, signal?: AbortSignal): Promise<unknown> {
    const id = ++this.requestId;
    const response = await this.post(
      { jsonrpc: "2.0", id, method, params },
      combineSignals(this.lifetimeSignal, signal),
    );
    const message = readJsonRpcResponse(response);
    if (message.id !== id) throw unavailable("MCP request failed.");
    if (message.error !== undefined) {
      if (method === "tools/call" && isManagedMcpCallResultUnknown(message.error))
        throw new ManagedCapabilityCallResultUnknownError();
      throw unavailable("MCP request failed.");
    }
    return message.result;
  }

  private async notify(method: string): Promise<void> {
    await this.post(
      { jsonrpc: "2.0", method },
      combineSignals(this.lifetimeSignal, undefined),
      true,
    );
  }

  private async post(
    body: Record<string, unknown>,
    signal: AbortSignal,
    allowEmpty = false,
  ): Promise<unknown> {
    let response: Response;
    try {
      response = await fetch(this.brokerUrl, {
        method: "POST",
        headers: {
          accept: "application/json, text/event-stream",
          authorization: `Bearer ${this.token}`,
          "content-type": "application/json",
          "mcp-protocol-version": this.protocolVersion,
          ...(this.sessionId ? { "mcp-session-id": this.sessionId } : {}),
        },
        body: JSON.stringify(body),
        signal,
      });
    } catch {
      throw unavailable("MCP broker is unavailable.");
    }
    if (!response.ok) throw unavailable("MCP broker request failed.");
    const returnedSessionId = response.headers.get("mcp-session-id")?.trim();
    if (returnedSessionId) this.sessionId = returnedSessionId;
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (bytes.length > MAX_MCP_RESPONSE_BYTES)
      throw unavailable("MCP response exceeded the managed limit.");
    if (bytes.length === 0 && allowEmpty) return undefined;
    const text = new TextDecoder().decode(bytes);
    try {
      if (response.headers.get("content-type")?.toLowerCase().includes("text/event-stream"))
        return parseEventStream(text);
      return JSON.parse(text) as unknown;
    } catch {
      throw unavailable("MCP response is invalid.");
    }
  }
}

export async function createManagedPiMcpTools(
  manifest: RuntimeCapabilityManifest | null,
  environment: NodeJS.ProcessEnv,
): Promise<ManagedPiMcpTools> {
  const bindings =
    manifest?.bindings.filter((binding) => binding.resourceKind === "mcp-server") ?? [];
  if (bindings.length === 0) return emptyManagedTools();
  if (bindings.some((binding) => binding.transport !== "streamable-http"))
    throw unavailable("Pi managed MCP requires streamable-http transport.");
  if (bindings.some((binding) => !binding.permissions.includes("tools.call")))
    throw unavailable("Pi managed MCP binding does not grant tools.call.");

  const configuration = managedMcpConfiguration(manifest, environment);
  const lifetime = new AbortController();
  const tools: ToolDefinition[] = [];
  const metadata = new Map<string, ManagedPiMcpToolMetadata>();
  const unknownToolCallIds = new Set<string>();
  let reportResultUnknown!: (error: ManagedCapabilityCallResultUnknownError) => void;
  const resultUnknown = new Promise<ManagedCapabilityCallResultUnknownError>((resolve) => {
    reportResultUnknown = resolve;
  });
  try {
    for (const binding of bindings) {
      const token = configuration.environment[capabilityTokenEnvironmentName(binding.resourceId)];
      const brokerUrl = configuration.environment.CLOUD_AGENT_MCP_BROKER_URL;
      if (!token || !brokerUrl) throw unavailable("Pi managed MCP injection is incomplete.");
      const client = new ManagedMcpClient(brokerUrl, token, lifetime.signal);
      await client.initialize();
      for (const discovered of await client.listTools()) {
        const name = managedToolName(binding, discovered.name);
        if (metadata.has(name)) throw unavailable("Pi managed MCP tool names collide.");
        const tool: ToolDefinition = {
          name,
          label: `Managed MCP: ${discovered.name}`,
          description: discovered.description || "Host-managed MCP tool.",
          parameters: discovered.inputSchema as ToolDefinition["parameters"],
          executionMode: "sequential",
          async execute(toolCallId, params, signal) {
            try {
              const result = recordValue(await client.callTool(discovered.name, params, signal));
              if (!result || result.isError === true || !Array.isArray(result.content))
                throw unavailable("Managed MCP tool execution failed.");
              return { content: mcpResultContent(result.content), details: {} };
            } catch (error) {
              if (error instanceof ManagedCapabilityCallResultUnknownError) {
                unknownToolCallIds.add(toolCallId);
                reportResultUnknown(error);
              }
              throw error;
            }
          },
        };
        tools.push(tool);
        metadata.set(name, {
          capabilityResourceId: binding.resourceId,
          mcpToolName: discovered.name,
        });
      }
    }
    return Object.freeze({
      tools: Object.freeze(tools),
      metadataByToolName: metadata,
      unknownToolCallIds,
      resultUnknown,
      close: () => lifetime.abort(),
    });
  } catch (error) {
    lifetime.abort();
    if (error instanceof ManagedCapabilityUnavailableError) throw error;
    throw unavailable("Pi managed MCP initialization failed.");
  }
}

function emptyManagedTools(): ManagedPiMcpTools {
  return Object.freeze({
    tools: Object.freeze([]),
    metadataByToolName: new Map(),
    unknownToolCallIds: new Set<string>(),
    resultUnknown: new Promise<ManagedCapabilityCallResultUnknownError>(() => {}),
    close: () => {},
  });
}

function readDiscoveredTool(value: unknown): DiscoveredMcpTool {
  const tool = recordValue(value);
  const name = stringValue(tool?.name);
  const schema = recordValue(tool?.inputSchema);
  if (!name || name.length > 256 || !schema || schema.type !== "object")
    throw unavailable("MCP tool definition is invalid.");
  let serialized: string;
  try {
    serialized = JSON.stringify(schema);
  } catch {
    throw unavailable("MCP tool schema is invalid.");
  }
  if (Buffer.byteLength(serialized) > MAX_MCP_SCHEMA_BYTES)
    throw unavailable("MCP tool schema exceeded the managed limit.");
  const description = typeof tool?.description === "string" ? tool.description.slice(0, 4096) : "";
  return { name, description, inputSchema: schema };
}

function managedToolName(binding: RuntimeCapabilityBinding, mcpToolName: string): string {
  const cleanResource = safeName(binding.resourceId).slice(0, 16) || "server";
  const cleanTool = safeName(mcpToolName).slice(0, 24) || "tool";
  const suffix = createHash("sha256")
    .update(`${binding.resourceId}\u0000${mcpToolName}`)
    .digest("hex")
    .slice(0, 8);
  return `mcp__${cleanResource}__${cleanTool}__${suffix}`;
}

function safeName(value: string): string {
  return value.replaceAll(/[^A-Za-z0-9_-]/gu, "_");
}

function mcpResultContent(value: unknown): Array<{ type: "text"; text: string }> {
  if (!Array.isArray(value)) return [{ type: "text", text: "Managed MCP tool completed." }];
  let remaining = MAX_MCP_RESULT_TEXT;
  const content: Array<{ type: "text"; text: string }> = [];
  for (const item of value) {
    const block = recordValue(item);
    if (block?.type !== "text" || typeof block.text !== "string" || remaining === 0) continue;
    const text = block.text.slice(0, remaining);
    remaining -= text.length;
    content.push({ type: "text", text });
  }
  return content.length ? content : [{ type: "text", text: "Managed MCP tool completed." }];
}

function readJsonRpcResponse(value: unknown): JsonRpcResponse {
  const response = recordValue(value);
  if (!response || response.jsonrpc !== "2.0") throw unavailable("MCP response is invalid.");
  return response as JsonRpcResponse;
}

function parseEventStream(value: string): unknown {
  const messages = value
    .split(/\r?\n\r?\n/u)
    .flatMap((event) =>
      event
        .split(/\r?\n/u)
        .filter((line) => line.startsWith("data:"))
        .map((line) => line.slice(5).trim()),
    )
    .filter(Boolean);
  const last = messages.at(-1);
  if (!last) throw unavailable("MCP event stream is empty.");
  return JSON.parse(last) as unknown;
}

function combineSignals(lifetime: AbortSignal, call: AbortSignal | undefined): AbortSignal {
  if (!call) return lifetime;
  return AbortSignal.any([lifetime, call]);
}

function optionalCursor(value: unknown): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  if (typeof value !== "string" || value.length > 1024)
    throw unavailable("MCP tools/list cursor is invalid.");
  return value;
}

function unavailable(message: string): ManagedCapabilityUnavailableError {
  return new ManagedCapabilityUnavailableError(message);
}
