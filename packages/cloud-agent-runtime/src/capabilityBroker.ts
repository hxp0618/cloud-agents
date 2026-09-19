import { lookup } from "node:dns/promises";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { readFileSync, fstatSync } from "node:fs";
import { isIP } from "node:net";

import {
  CLOUD_AGENT_ENVIRONMENT,
  capabilityTokenEnvironmentName,
  type RuntimeCapabilityManifest,
} from "@cloud-agents/cloud-agent-provider-api/internal";

// Descriptor includes base64url Skill bytes; keep it below a Kubernetes Secret's 1 MiB limit.
const MAX_MATERIALIZATION_BYTES = 768 * 1024;
const MAX_REQUEST_BYTES = 1 * 1024 * 1024;
const MAX_RESPONSE_BYTES = 4 * 1024 * 1024;
const UPSTREAM_TIMEOUT_MS = 10_000;

type McpMaterialization = Readonly<{
  resourceId: string;
  version: string;
  digest: `sha256:${string}`;
  transport: "sse" | "streamable-http";
  endpoint: string;
  token: string;
  allowedHosts: ReadonlyArray<string>;
}>;

type AuthorizedMcpMaterialization = McpMaterialization &
  Readonly<{
    expiresAtUnixSeconds: number;
  }>;

type CapabilityMaterialization = Readonly<{
  version: 1;
  mcp: ReadonlyArray<McpMaterialization>;
  skills: ReadonlyArray<SkillMaterialization>;
}>;

type SkillMaterialization = Readonly<{
  resourceId: string;
  version: string;
  digest: `sha256:${string}`;
  bundle?: string;
  signature?: string;
  publicKey?: string;
  signingKeyId?: string;
}>;

export type ManagedMcpBroker = Readonly<{
  url: string;
  environment: Readonly<Record<string, string>>;
  close(): Promise<void>;
}>;

/**
 * Reads the host-only materialization descriptor from an inherited anonymous
 * FD. The descriptor is intentionally separate from the public capability
 * manifest, so endpoint and credential bytes never cross the SDK boundary.
 */
export function readCapabilityMaterialization(
  environment: Readonly<Record<string, string | undefined>>,
  manifest: RuntimeCapabilityManifest | null,
): CapabilityMaterialization | null {
  const mcp = manifest?.bindings.filter((binding) => binding.resourceKind === "mcp-server") ?? [];
  const rawFd = environment[CLOUD_AGENT_ENVIRONMENT.capabilityMaterializationFd]?.trim();
  if (!rawFd) return null;
  const fd = Number(rawFd);
  if (!Number.isSafeInteger(fd) || fd < 3 || fd > 1024) {
    throw new Error("Capability materialization FD is invalid.");
  }
  const stat = fstatSync(fd);
  if (!stat.isFile() || stat.size < 1 || stat.size > MAX_MATERIALIZATION_BYTES) {
    throw new Error("Capability materialization size is invalid.");
  }
  const parsed: unknown = JSON.parse(readFileSync(fd, "utf8"));
  if (
    !isRecord(parsed) ||
    !onlyKeys(
      parsed,
      Array.isArray(parsed.skills) ? ["version", "mcp", "skills"] : ["version", "mcp"],
    ) ||
    parsed.version !== 1 ||
    !Array.isArray(parsed.mcp)
  ) {
    throw new Error("Capability materialization shape is invalid.");
  }
  if (parsed.mcp.length !== mcp.length) {
    throw new Error("Capability materialization does not match the capability manifest.");
  }
  const materialized = parsed.mcp.map(readMcpMaterialization);
  const skills = Array.isArray(parsed.skills) ? parsed.skills.map(readSkillMaterialization) : [];
  const expected = new Map(mcp.map((binding) => [binding.resourceId, binding]));
  const tokens = new Set<string>();
  for (const item of materialized) {
    const binding = expected.get(item.resourceId);
    if (
      !binding ||
      binding.version !== item.version ||
      binding.digest !== item.digest ||
      binding.transport !== item.transport
    ) {
      throw new Error("Capability materialization identity does not match the manifest.");
    }
    if (tokens.has(item.token))
      throw new Error("Capability materialization credentials are ambiguous.");
    tokens.add(item.token);
  }
  const expectedSkills = new Map(
    (manifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? []).map(
      (binding) => [binding.resourceId, binding],
    ),
  );
  if (skills.length !== expectedSkills.size)
    throw new Error("Capability materialization does not match the capability manifest.");
  const skillIds = new Set<string>();
  for (const item of skills) {
    const binding = expectedSkills.get(item.resourceId);
    if (
      !binding ||
      binding.version !== item.version ||
      binding.digest !== item.digest ||
      skillIds.has(item.resourceId)
    ) {
      throw new Error("Skill materialization identity does not match the manifest.");
    }
    skillIds.add(item.resourceId);
  }
  return Object.freeze({
    version: 1,
    mcp: Object.freeze(materialized),
    skills: Object.freeze(skills),
  });
}

/** Starts a loopback-only MCP HTTP broker for already-authorized materializations. */
export async function startManagedMcpBroker(
  manifest: RuntimeCapabilityManifest | null,
  materialization: CapabilityMaterialization | null,
): Promise<ManagedMcpBroker | null> {
  const bindings =
    manifest?.bindings.filter((binding) => binding.resourceKind === "mcp-server") ?? [];
  if (bindings.length === 0) return null;
  if (!materialization || materialization.mcp.length !== bindings.length) {
    return null;
  }
  const byToken = new Map<string, AuthorizedMcpMaterialization>();
  const environmentNames = new Set<string>();
  for (const item of materialization.mcp) {
    const binding = bindings.find((candidate) => candidate.resourceId === item.resourceId);
    if (!binding) return null;
    const environmentName = capabilityTokenEnvironmentName(item.resourceId);
    if (environmentNames.has(environmentName)) {
      throw new Error("Capability materialization environment references collide.");
    }
    environmentNames.add(environmentName);
    byToken.set(item.token, { ...item, expiresAtUnixSeconds: binding.expiresAtUnixSeconds });
  }
  const server = createServer((request, response) => {
    void handleRequest(request, response, byToken);
  });
  await listenLoopback(server);
  const address = server.address();
  if (!address || typeof address === "string") {
    await closeServer(server);
    throw new Error("MCP broker failed to bind a loopback address.");
  }
  const url = `http://127.0.0.1:${address.port}/mcp`;
  const environment: Record<string, string> = { CLOUD_AGENT_MCP_BROKER_URL: url };
  for (const item of materialization.mcp) {
    environment[capabilityTokenEnvironmentName(item.resourceId)] = item.token;
  }
  return Object.freeze({
    url,
    environment: Object.freeze(environment),
    close: () => closeServer(server),
  });
}

async function handleRequest(
  request: IncomingMessage,
  response: ServerResponse,
  byToken: ReadonlyMap<string, AuthorizedMcpMaterialization>,
): Promise<void> {
  try {
    if (request.url !== "/mcp") {
      writeJson(response, 404, { error: "mcp_route_not_found" });
      return;
    }
    const token = bearerToken(request.headers.authorization);
    const materialization = token ? findToken(byToken, token) : undefined;
    if (!materialization) {
      writeJson(response, 401, { error: "mcp_authorization_required" });
      return;
    }
    if (materialization.expiresAtUnixSeconds <= Math.floor(Date.now() / 1000)) {
      writeJson(response, 403, { error: "mcp_capability_expired" });
      return;
    }
    if (materialization.transport !== "streamable-http") {
      writeJson(response, 501, { error: "mcp_transport_unsupported" });
      return;
    }
    if (request.method !== "POST") {
      response.setHeader("allow", "POST");
      writeJson(response, 405, { error: "mcp_stream_unavailable" });
      return;
    }
    const endpoint = parseEndpoint(materialization);
    await assertEndpointNetwork(endpoint, materialization.allowedHosts);
    const body = await readRequestBody(request);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), UPSTREAM_TIMEOUT_MS);
    timeout.unref();
    let upstream: Response;
    try {
      const headers: Record<string, string> = {
        accept: "application/json, text/event-stream",
        "content-type": request.headers["content-type"] ?? "application/json",
        authorization: `Bearer ${token}`,
      };
      for (const name of ["mcp-session-id", "mcp-protocol-version"] as const) {
        const value = request.headers[name];
        if (typeof value === "string") headers[name] = value;
      }
      upstream = await fetch(endpoint, {
        method: "POST",
        headers,
        body,
        redirect: "error",
        signal: controller.signal,
      });
    } catch {
      writeJson(response, 502, { error: "mcp_upstream_unavailable" });
      return;
    } finally {
      clearTimeout(timeout);
    }
    const payload = await readResponseBody(upstream);
    response.statusCode = upstream.status;
    response.setHeader("content-type", upstream.headers.get("content-type") ?? "application/json");
    response.setHeader("cache-control", "no-store");
    for (const name of ["mcp-session-id", "mcp-protocol-version"] as const) {
      const value = upstream.headers.get(name);
      if (value) response.setHeader(name, value);
    }
    response.end(payload);
  } catch (error) {
    const code = error instanceof BrokerRequestError ? error.status : 400;
    writeJson(response, code, {
      error: error instanceof BrokerRequestError ? error.code : "mcp_request_invalid",
    });
  }
}

function readMcpMaterialization(value: unknown): McpMaterialization {
  if (
    !isRecord(value) ||
    !identifier(value.resourceId) ||
    !identifier(value.version) ||
    !digest(value.digest) ||
    !["sse", "streamable-http"].includes(value.transport as string) ||
    typeof value.endpoint !== "string" ||
    typeof value.token !== "string" ||
    value.token.length === 0 ||
    value.token.length > 4096 ||
    /[\u0000-\u001f\u007f]/u.test(value.token) ||
    !Array.isArray(value.allowedHosts) ||
    value.allowedHosts.length < 1 ||
    value.allowedHosts.length > 32 ||
    !value.allowedHosts.every((host) => typeof host === "string" && validHost(host))
  ) {
    throw new Error("MCP materialization is invalid.");
  }
  return Object.freeze({
    resourceId: value.resourceId,
    version: value.version,
    digest: value.digest as `sha256:${string}`,
    transport: value.transport as "sse" | "streamable-http",
    endpoint: value.endpoint,
    token: value.token,
    allowedHosts: Object.freeze([...value.allowedHosts]),
  });
}

function readSkillMaterialization(value: unknown): SkillMaterialization {
  if (
    !isRecord(value) ||
    !onlyKeys(value, [
      "resourceId",
      "version",
      "digest",
      "bundle",
      "signature",
      "publicKey",
      "signingKeyId",
    ]) ||
    !identifier(value.resourceId) ||
    !identifier(value.version) ||
    !digest(value.digest)
  ) {
    throw new Error("Skill materialization identity is invalid.");
  }
  const bundle = value.bundle;
  if (
    bundle !== undefined &&
    (typeof bundle !== "string" || !/^[A-Za-z0-9_-]+$/u.test(bundle) || bundle.length > 700_000)
  ) {
    throw new Error("Skill materialization bundle is invalid.");
  }
  if (!bundle) throw new Error("Skill materialization requires a signed bundle.");
  const signed = [value.signature, value.publicKey];
  if (
    signed.some(
      (item) => item !== undefined && (typeof item !== "string" || !/^[A-Za-z0-9_-]+$/u.test(item)),
    )
  ) {
    throw new Error("Skill materialization signature is invalid.");
  }
  if (
    bundle &&
    (typeof value.signature !== "string" ||
      typeof value.publicKey !== "string" ||
      !identifier(value.signingKeyId))
  ) {
    throw new Error("Skill materialization signature is required.");
  }
  return Object.freeze({
    resourceId: value.resourceId,
    version: value.version,
    digest: value.digest as `sha256:${string}`,
    bundle,
    ...(typeof value.signature === "string" ? { signature: value.signature } : {}),
    ...(typeof value.publicKey === "string" ? { publicKey: value.publicKey } : {}),
    ...(typeof value.signingKeyId === "string" ? { signingKeyId: value.signingKeyId } : {}),
  });
}

function parseEndpoint(item: McpMaterialization): string {
  let parsed: URL;
  try {
    parsed = new URL(item.endpoint);
  } catch {
    throw new BrokerRequestError(400, "mcp_endpoint_invalid");
  }
  const hostname = normalizedHostname(parsed.hostname);
  if (
    !["http:", "https:"].includes(parsed.protocol) ||
    !hostname ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash
  ) {
    throw new BrokerRequestError(400, "mcp_endpoint_invalid");
  }
  if (parsed.protocol !== "https:" && !isLoopback(hostname)) {
    throw new BrokerRequestError(400, "mcp_endpoint_requires_tls");
  }
  if (!item.allowedHosts.some((host) => normalizedHostname(host) === hostname)) {
    throw new BrokerRequestError(403, "mcp_network_policy_denied");
  }
  return parsed.toString();
}

async function assertEndpointNetwork(
  endpoint: string,
  allowedHosts: ReadonlyArray<string>,
): Promise<void> {
  const parsed = new URL(endpoint);
  const hostname = normalizedHostname(parsed.hostname);
  if (isIP(hostname)) {
    if (
      isPrivateAddress(hostname) &&
      !allowedHosts.some((host) => normalizedHostname(host) === hostname)
    ) {
      throw new BrokerRequestError(403, "mcp_network_policy_denied");
    }
    return;
  }
  let addresses;
  try {
    addresses = await lookup(hostname, { all: true, verbatim: true });
  } catch {
    throw new BrokerRequestError(502, "mcp_upstream_unavailable");
  }
  if (addresses.length === 0 || addresses.some(({ address }) => isPrivateAddress(address))) {
    throw new BrokerRequestError(403, "mcp_network_policy_denied");
  }
}

function bearerToken(value: string | undefined): string | undefined {
  const match = /^Bearer ([\x21-\x7e]{1,4096})$/u.exec(value ?? "");
  return match?.[1];
}

function findToken(
  byToken: ReadonlyMap<string, AuthorizedMcpMaterialization>,
  candidate: string,
): AuthorizedMcpMaterialization | undefined {
  for (const [token, value] of byToken) {
    if (token.length !== candidate.length) continue;
    let different = 0;
    for (let index = 0; index < token.length; index += 1)
      different |= token.charCodeAt(index) ^ candidate.charCodeAt(index);
    if (different === 0) return value;
  }
  return undefined;
}

async function readRequestBody(request: IncomingMessage): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
    size += bytes.length;
    if (size > MAX_REQUEST_BYTES) throw new BrokerRequestError(413, "mcp_request_too_large");
    chunks.push(bytes);
  }
  if (size === 0) throw new BrokerRequestError(400, "mcp_request_empty");
  return Buffer.concat(chunks);
}

async function readResponseBody(response: Response): Promise<Buffer> {
  const contentLength = Number(response.headers.get("content-length") ?? "");
  if (Number.isSafeInteger(contentLength) && contentLength > MAX_RESPONSE_BYTES) {
    throw new BrokerRequestError(502, "mcp_response_too_large");
  }
  if (!response.body) return Buffer.alloc(0);
  const reader = response.body.getReader();
  const chunks: Buffer[] = [];
  let size = 0;
  while (true) {
    const next = await reader.read();
    if (next.done) break;
    const chunk = Buffer.from(next.value);
    size += chunk.length;
    if (size > MAX_RESPONSE_BYTES) {
      await reader.cancel();
      throw new BrokerRequestError(502, "mcp_response_too_large");
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

function writeJson(response: ServerResponse, status: number, value: Record<string, string>): void {
  response.statusCode = status;
  response.setHeader("content-type", "application/json");
  response.setHeader("cache-control", "no-store");
  response.end(JSON.stringify(value));
}

function listenLoopback(server: Server): Promise<void> {
  return new Promise((resolve, reject) => {
    const onError = (error: Error) => reject(error);
    server.once("error", onError);
    server.listen(0, "127.0.0.1", () => {
      server.off("error", onError);
      resolve();
    });
  });
}

function closeServer(server: Server): Promise<void> {
  server.closeAllConnections?.();
  return new Promise((resolve) => server.close(() => resolve()));
}

function validHost(value: string): boolean {
  const normalized = normalizedHostname(value);
  return normalized.length > 0 && normalized.length <= 253 && !/[\s/:?#@]/u.test(normalized);
}

function normalizedHostname(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/^\[|\]$/gu, "")
    .replace(/\.$/u, "");
}

function isLoopback(value: string): boolean {
  return value === "localhost" || value === "127.0.0.1" || value === "::1";
}

function isPrivateAddress(value: string): boolean {
  const normalized = normalizedHostname(value);
  if (
    normalized === "::1" ||
    normalized.startsWith("fc") ||
    normalized.startsWith("fd") ||
    normalized.startsWith("fe80:")
  )
    return true;
  if (isIP(normalized) !== 4) return false;
  const [first = -1, second = -1] = normalized.split(".").map(Number);
  return (
    first === 10 ||
    first === 127 ||
    first === 0 ||
    (first === 169 && second === 254) ||
    (first === 172 && second >= 16 && second <= 31) ||
    (first === 192 && second === 168)
  );
}

function identifier(value: unknown): value is string {
  return (
    typeof value === "string" && /^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$/u.test(value)
  );
}

function digest(value: unknown): value is string {
  return typeof value === "string" && /^sha256:[0-9a-f]{64}$/u.test(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function onlyKeys(value: Record<string, unknown>, allowed: ReadonlyArray<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === new Set(allowed).size && keys.every((key) => allowed.includes(key));
}

class BrokerRequestError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(code);
  }
}

export type { CapabilityMaterialization, McpMaterialization, SkillMaterialization };
