import { createHash } from "node:crypto";
import { isRecord } from "./json";

const MANIFEST_ENV = "CLOUD_AGENT_CAPABILITY_MANIFEST_B64";
export const CLOUD_AGENT_SKILL_ROOT_ENV = "CLOUD_AGENT_SKILL_ROOT";
export const CLOUD_AGENT_DEFAULT_SKILL_ROOT = "/run/cloud-agents/skills";
export const CLOUD_AGENT_EPHEMERAL_SKILL_ROOT = "/tmp/cloud-agents-skills";
const MAX_MANIFEST_BYTES = 64 * 1024;
const MAX_BINDINGS = 32;

export type RuntimeCapabilityBinding = Readonly<{
  resourceKind: "mcp-server" | "skill-bundle";
  resourceId: string;
  version: string;
  digest: `sha256:${string}`;
  transport?: "stdio" | "sse" | "streamable-http";
  connectionRef?: string;
  credentialRef?: string;
  grantId: string;
  networkPolicyRef?: string;
  expiresAtUnixSeconds: number;
  permissions: ReadonlyArray<string>;
  readOnly: boolean;
}>;

export type RuntimeCapabilityManifest = Readonly<{
  version: 1;
  bindings: ReadonlyArray<RuntimeCapabilityBinding>;
  digest: `sha256:${string}`;
}>;

export type ManagedMcpConfiguration = Readonly<{
  environment: Readonly<Record<string, string>>;
  codexServers: Readonly<Record<string, { url: string; bearer_token_env_var: string }>>;
  claudeServers: Readonly<
    Record<
      string,
      {
        type: "http";
        url: string;
        headers: Record<string, string>;
        alwaysLoad: true;
      }
    >
  >;
}>;

export class ManagedCapabilityUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ManagedCapabilityUnavailableError";
  }
}

export function capabilityTokenEnvironmentName(resourceId: string): string {
  return `CLOUD_AGENT_MCP_TOKEN_${resourceId.replaceAll(/[^A-Za-z0-9]/gu, "_").toUpperCase()}`;
}

export function managedSkillBundleDirectories(
  manifest: RuntimeCapabilityManifest | null,
  environment: NodeJS.ProcessEnv,
): ReadonlyArray<string> {
  const skills =
    manifest?.bindings.filter((binding) => binding.resourceKind === "skill-bundle") ?? [];
  const suffixes = new Set<string>();
  return skills.map((binding) => {
    const suffix = binding.resourceId.replaceAll(/[^A-Za-z0-9]/gu, "_").toUpperCase();
    if (suffixes.has(suffix)) {
      throw new ManagedCapabilityUnavailableError("Skill Bundle environment references collide.");
    }
    suffixes.add(suffix);
    const path = environment[`CLOUD_AGENT_SKILL_BUNDLE_${suffix}_ROOT`]?.trim();
    if (!path || !isManagedSkillBundlePath(path)) {
      throw new ManagedCapabilityUnavailableError(
        `Skill Bundle ${binding.resourceId} is not mounted by the Runtime.`,
      );
    }
    return path;
  });
}

/** Returns the only Runtime-owned Skill root selected by the host. */
export function managedSkillRootDirectory(environment: NodeJS.ProcessEnv = process.env): string {
  const configured = environment[CLOUD_AGENT_SKILL_ROOT_ENV]?.trim();
  if (!configured) return CLOUD_AGENT_DEFAULT_SKILL_ROOT;
  if (
    configured !== CLOUD_AGENT_DEFAULT_SKILL_ROOT &&
    configured !== CLOUD_AGENT_EPHEMERAL_SKILL_ROOT
  ) {
    throw new Error("Skill Bundle materialization root is not Runtime-owned.");
  }
  return configured;
}

export function isManagedSkillBundlePath(path: string): boolean {
  return (
    !path.includes("..") &&
    [CLOUD_AGENT_DEFAULT_SKILL_ROOT, CLOUD_AGENT_EPHEMERAL_SKILL_ROOT].some((root) =>
      path.startsWith(`${root}/`),
    )
  );
}

/** Read the Worker-injected, digest-pinned manifest; absent means no capabilities. */
export function readCapabilityManifest(
  environment: NodeJS.ProcessEnv = process.env,
): RuntimeCapabilityManifest | null {
  const encoded = environment[MANIFEST_ENV]?.trim();
  if (!encoded) return null;
  if (!/^[A-Za-z0-9_-]+$/u.test(encoded))
    throw new Error("Capability manifest encoding is invalid.");
  let bytes: Buffer;
  try {
    bytes = Buffer.from(encoded, "base64url");
  } catch {
    throw new Error("Capability manifest encoding is invalid.");
  }
  if (bytes.length === 0 || bytes.length > MAX_MANIFEST_BYTES) {
    throw new Error("Capability manifest size is invalid.");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new Error("Capability manifest JSON is invalid.");
  }
  if (
    !isRecord(parsed) ||
    parsed.version !== 1 ||
    !Array.isArray(parsed.bindings) ||
    parsed.bindings.length > MAX_BINDINGS
  ) {
    throw new Error("Capability manifest shape is invalid.");
  }
  const bindings = parsed.bindings.map(readBinding);
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}` as const;
  return Object.freeze({
    version: 1,
    bindings: Object.freeze(bindings),
    digest,
  });
}

/** Materialize only Host-provided loopback MCP credentials; never derive endpoints from refs. */
export function managedMcpConfiguration(
  manifest: RuntimeCapabilityManifest | null,
  environment: NodeJS.ProcessEnv,
): ManagedMcpConfiguration {
  const mcp = manifest?.bindings.filter((binding) => binding.resourceKind === "mcp-server") ?? [];
  if (mcp.length === 0)
    return Object.freeze({
      environment: {},
      codexServers: {},
      claudeServers: {},
    });
  if (mcp.some((binding) => binding.transport !== "streamable-http")) {
    throw new ManagedCapabilityUnavailableError(
      "Only MCP streamable-http transport is available through the Host-managed broker.",
    );
  }
  const broker = environment.CLOUD_AGENT_MCP_BROKER_URL?.trim();
  if (!broker)
    throw new ManagedCapabilityUnavailableError("MCP capability requires a Host-managed broker.");
  let brokerURL: URL;
  try {
    brokerURL = new URL(broker);
  } catch {
    throw new ManagedCapabilityUnavailableError("MCP broker URL is invalid.");
  }
  if (
    brokerURL.protocol !== "http:" ||
    !["127.0.0.1", "localhost", "[::1]", "::1"].includes(brokerURL.hostname) ||
    brokerURL.pathname !== "/mcp" ||
    brokerURL.username ||
    brokerURL.password ||
    brokerURL.search ||
    brokerURL.hash
  ) {
    throw new ManagedCapabilityUnavailableError(
      "MCP broker must be an unauthenticated loopback /mcp endpoint.",
    );
  }
  const injected: Record<string, string> = {
    CLOUD_AGENT_MCP_BROKER_URL: brokerURL.toString(),
  };
  const codexServers: Record<string, { url: string; bearer_token_env_var: string }> = {};
  const claudeServers: Record<
    string,
    {
      type: "http";
      url: string;
      headers: Record<string, string>;
      alwaysLoad: true;
    }
  > = {};
  const tokenEnvironmentNames = new Set<string>();
  for (const binding of mcp) {
    const tokenEnv = capabilityTokenEnvironmentName(binding.resourceId);
    if (tokenEnvironmentNames.has(tokenEnv)) {
      throw new ManagedCapabilityUnavailableError("MCP capability environment references collide.");
    }
    tokenEnvironmentNames.add(tokenEnv);
    const token = environment[tokenEnv]?.trim();
    if (!token || token.length > 4096 || /[\u0000-\u001f\u007f]/u.test(token)) {
      throw new ManagedCapabilityUnavailableError(
        `MCP capability credential ${binding.resourceId} is unavailable.`,
      );
    }
    const name = `cloud_agents_${binding.resourceId}`;
    injected[tokenEnv] = token;
    codexServers[name] = {
      url: brokerURL.toString(),
      bearer_token_env_var: tokenEnv,
    };
    claudeServers[name] = {
      type: "http",
      url: brokerURL.toString(),
      headers: { Authorization: `Bearer ${token}` },
      alwaysLoad: true,
    };
  }
  return Object.freeze({
    environment: Object.freeze(injected),
    codexServers: Object.freeze(codexServers),
    claudeServers: Object.freeze(claudeServers),
  });
}

function readBinding(value: unknown): RuntimeCapabilityBinding {
  const permissions = isRecord(value) ? (value.permissions ?? []) : [];
  if (
    !isRecord(value) ||
    !identifier(value.resourceId) ||
    !identifier(value.version) ||
    !digest(value.digest) ||
    !identifier(value.grantId) ||
    !safeInteger(value.expiresAtUnixSeconds) ||
    value.expiresAtUnixSeconds <= Math.floor(Date.now() / 1000) ||
    !Array.isArray(permissions) ||
    !permissions.every(
      (item) => typeof item === "string" && /^[a-z][a-z0-9._:-]{0,127}$/u.test(item),
    )
  ) {
    throw new Error("Capability binding is invalid or expired.");
  }
  if (value.resourceKind === "mcp-server") {
    if (
      !onlyKeys(value, [
        "resourceKind",
        "resourceId",
        "version",
        "digest",
        "transport",
        "connectionRef",
        "credentialRef",
        "grantId",
        "networkPolicyRef",
        "expiresAtUnixSeconds",
        "permissions",
        "readOnly",
      ]) ||
      !["stdio", "sse", "streamable-http"].includes(value.transport as string) ||
      !identifier(value.connectionRef) ||
      !identifier(value.credentialRef) ||
      !identifier(value.networkPolicyRef) ||
      permissions.length === 0 ||
      value.readOnly !== false
    ) {
      throw new Error("MCP capability binding is invalid.");
    }
    return Object.freeze({
      resourceKind: value.resourceKind,
      resourceId: value.resourceId,
      version: value.version,
      digest: value.digest as `sha256:${string}`,
      transport: value.transport as "stdio" | "sse" | "streamable-http",
      connectionRef: value.connectionRef,
      credentialRef: value.credentialRef,
      grantId: value.grantId,
      networkPolicyRef: value.networkPolicyRef,
      expiresAtUnixSeconds: value.expiresAtUnixSeconds,
      permissions: Object.freeze([...permissions]),
      readOnly: false,
    });
  }
  if (
    value.resourceKind === "skill-bundle" &&
    (onlyKeys(value, [
      "resourceKind",
      "resourceId",
      "version",
      "digest",
      "grantId",
      "expiresAtUnixSeconds",
      "readOnly",
    ]) ||
      onlyKeys(value, [
        "resourceKind",
        "resourceId",
        "version",
        "digest",
        "grantId",
        "expiresAtUnixSeconds",
        "permissions",
        "readOnly",
      ])) &&
    permissions.length === 0 &&
    value.readOnly === true
  ) {
    return Object.freeze({
      resourceKind: value.resourceKind,
      resourceId: value.resourceId,
      version: value.version,
      digest: value.digest as `sha256:${string}`,
      grantId: value.grantId,
      expiresAtUnixSeconds: value.expiresAtUnixSeconds,
      permissions: Object.freeze([...permissions]),
      readOnly: true,
    });
  }
  throw new Error("Skill capability binding is invalid.");
}

function identifier(value: unknown): value is string {
  return (
    typeof value === "string" && /^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$/u.test(value)
  );
}

function safeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value);
}

function digest(value: unknown): value is string {
  return typeof value === "string" && /^sha256:[0-9a-f]{64}$/u.test(value);
}

function onlyKeys(value: Record<string, unknown>, allowed: ReadonlyArray<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === allowed.length && keys.every((key) => allowed.includes(key));
}
