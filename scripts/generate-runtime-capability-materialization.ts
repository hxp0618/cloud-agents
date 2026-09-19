import { constants } from "node:fs";
import {
  closeSync,
  fstatSync,
  fsyncSync,
  lstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  renameSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { createHash, createPublicKey, randomBytes, verify } from "node:crypto";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";

const MAX_DESCRIPTOR_BYTES = 768 * 1024;
const MAX_TOKEN_BYTES = 4096;
const MAX_BUNDLE_BYTES = 512 * 1024;
const MAX_SIGNATURE_BYTES = 256;
const MAX_PUBLIC_KEY_BYTES = 256;

export type RuntimeCapabilityMaterializationConfig = Readonly<{
  version: 1;
  tenantId: string;
  mcp: ReadonlyArray<{
    resourceId: string;
    version: string;
    digest: `sha256:${string}`;
    transport: "sse" | "streamable-http";
    endpoint: string;
    tokenFile: string;
    allowedHosts: ReadonlyArray<string>;
  }>;
  skills: ReadonlyArray<{
    resourceId: string;
    version: string;
    digest: `sha256:${string}`;
    bundleFile: string;
    signatureFile: string;
    publicKeyFile: string;
    signingKeyId: string;
  }>;
}>;

export type GeneratedRuntimeCapabilityMaterialization = Readonly<{
  descriptorPath: string;
  descriptorDigest: `sha256:${string}`;
  signingKeyPaths: ReadonlyArray<string>;
}>;

/**
 * Builds the operator-owned descriptor consumed by Worker/Foundation Runtime.
 * Paths are resolved only relative to the protected config file; Provider
 * processes receive the resulting bytes through the existing anonymous FD.
 */
export function generateRuntimeCapabilityMaterialization(
  configPath: string,
  outputDirectory: string,
): GeneratedRuntimeCapabilityMaterialization {
  const absoluteConfig = requireAbsolute(configPath, "config path");
  const configBytes = readProtectedFile(absoluteConfig, MAX_DESCRIPTOR_BYTES, "config");
  const config = readConfig(configBytes);
  const sourceRoot = dirname(absoluteConfig);
  const mcpIds = new Set<string>();
  const tokens = new Set<string>();
  const mcp = config.mcp.map((item) => {
    assertMcp(item);
    if (mcpIds.has(item.resourceId)) {
      throw new Error(`MCP resource ${item.resourceId} is duplicated.`);
    }
    mcpIds.add(item.resourceId);
    const tokenBytes = readProtectedFile(
      operatorPath(sourceRoot, item.tokenFile),
      MAX_TOKEN_BYTES,
      "MCP token",
    );
    const tokenSource = tokenBytes.toString("utf8");
    const token = tokenSource.replace(/\r?\n$/u, "");
    if (!token || token.trim() !== token || /[\u0000-\u001f\u007f]/u.test(token)) {
      throw new Error(
        "MCP token must be a non-empty printable value without surrounding whitespace.",
      );
    }
    if (tokens.has(token)) throw new Error("MCP tokens must be unique per descriptor.");
    tokens.add(token);
    validateEndpoint(item.endpoint, item.allowedHosts);
    return {
      resourceId: item.resourceId,
      version: item.version,
      digest: item.digest,
      transport: item.transport,
      endpoint: item.endpoint,
      token,
      allowedHosts: [...item.allowedHosts],
    };
  });
  const signingKeys = new Map<string, Buffer>();
  const skillIds = new Set<string>();
  const skills = config.skills.map((item) => {
    assertSkill(item);
    if (skillIds.has(item.resourceId)) {
      throw new Error(`Skill resource ${item.resourceId} is duplicated.`);
    }
    skillIds.add(item.resourceId);
    const bundle = readProtectedFile(
      operatorPath(sourceRoot, item.bundleFile),
      MAX_BUNDLE_BYTES,
      "Skill bundle",
    );
    if (digestBytes(bundle) !== item.digest)
      throw new Error(`Skill ${item.resourceId} digest mismatch.`);
    const signature = readProtectedFile(
      operatorPath(sourceRoot, item.signatureFile),
      MAX_SIGNATURE_BYTES,
      "Skill signature",
    );
    const publicKey = readProtectedFile(
      operatorPath(sourceRoot, item.publicKeyFile),
      MAX_PUBLIC_KEY_BYTES,
      "Skill public key",
    );
    if (!signature.length || !publicKey.length)
      throw new Error("Skill signature material is empty.");
    let key;
    try {
      key = createPublicKey({ key: publicKey, format: "der", type: "spki" });
    } catch {
      throw new Error(`Skill ${item.resourceId} public key is invalid.`);
    }
    if (key.asymmetricKeyType !== "ed25519" || !verify(null, bundle, key, signature)) {
      throw new Error(`Skill ${item.resourceId} signature verification failed.`);
    }
    const prior = signingKeys.get(item.signingKeyId);
    if (prior && !prior.equals(publicKey)) {
      throw new Error(`Signing key ${item.signingKeyId} maps to multiple public keys.`);
    }
    signingKeys.set(item.signingKeyId, publicKey);
    return {
      resourceId: item.resourceId,
      version: item.version,
      digest: item.digest,
      bundle: bundle.toString("base64url"),
      signature: signature.toString("base64url"),
      publicKey: publicKey.toString("base64url"),
      signingKeyId: item.signingKeyId,
    };
  });
  const descriptor = Buffer.from(JSON.stringify({ version: 1, mcp, skills }) + "\n", "utf8");
  if (descriptor.length > MAX_DESCRIPTOR_BYTES) {
    throw new Error("Runtime capability materialization exceeds 768 KiB.");
  }
  const output = requireAbsolute(outputDirectory, "output directory");
  ensureDirectory(output);
  const signingKeyPaths: string[] = [];
  for (const [keyId, publicKey] of signingKeys) {
    const keyPath = join(output, `${keyId}.pub`);
    writeProtectedFile(keyPath, publicKey);
    signingKeyPaths.push(keyPath);
  }
  const descriptorPath = join(output, `${config.tenantId}.capabilities.json`);
  writeProtectedFile(descriptorPath, descriptor);
  return Object.freeze({
    descriptorPath,
    descriptorDigest: digestBytes(descriptor),
    signingKeyPaths: Object.freeze(signingKeyPaths),
  });
}

function readConfig(bytes: Buffer): RuntimeCapabilityMaterializationConfig {
  let value: unknown;
  try {
    value = JSON.parse(bytes.toString("utf8"));
  } catch {
    throw new Error("Capability materialization config is not valid JSON.");
  }
  if (
    !record(value) ||
    !onlyKeys(value, ["version", "tenantId", "mcp", "skills"]) ||
    value.version !== 1 ||
    !identifier(value.tenantId) ||
    !Array.isArray(value.mcp) ||
    !Array.isArray(value.skills)
  ) {
    throw new Error("Capability materialization config shape is invalid.");
  }
  return value as RuntimeCapabilityMaterializationConfig;
}

function assertMcp(
  value: unknown,
): asserts value is NonNullable<RuntimeCapabilityMaterializationConfig["mcp"]>[number] {
  if (
    !record(value) ||
    !onlyKeys(value, [
      "resourceId",
      "version",
      "digest",
      "transport",
      "endpoint",
      "tokenFile",
      "allowedHosts",
    ]) ||
    !identifier(value.resourceId) ||
    !identifier(value.version) ||
    !digestString(value.digest) ||
    (value.transport !== "sse" && value.transport !== "streamable-http") ||
    typeof value.endpoint !== "string" ||
    !relativePath(value.tokenFile) ||
    !Array.isArray(value.allowedHosts) ||
    value.allowedHosts.length < 1 ||
    value.allowedHosts.length > 32 ||
    !value.allowedHosts.every((host) => typeof host === "string" && validHost(host))
  ) {
    throw new Error("MCP materialization config entry is invalid.");
  }
}

function assertSkill(
  value: unknown,
): asserts value is NonNullable<RuntimeCapabilityMaterializationConfig["skills"]>[number] {
  if (
    !record(value) ||
    !onlyKeys(value, [
      "resourceId",
      "version",
      "digest",
      "bundleFile",
      "signatureFile",
      "publicKeyFile",
      "signingKeyId",
    ]) ||
    !identifier(value.resourceId) ||
    !identifier(value.version) ||
    !digestString(value.digest) ||
    !relativePath(value.bundleFile) ||
    !relativePath(value.signatureFile) ||
    !relativePath(value.publicKeyFile) ||
    !identifier(value.signingKeyId)
  ) {
    throw new Error("Skill materialization config entry is invalid.");
  }
}

function validateEndpoint(value: string, allowedHosts: ReadonlyArray<string>): void {
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error("MCP endpoint is invalid.");
  }
  const host = parsed.hostname.toLowerCase().replace(/\.$/u, "");
  const loopback =
    host === "127.0.0.1" || host === "localhost" || host === "[::1]" || host === "::1";
  if (
    (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && loopback)) ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash ||
    !allowedHosts.some((allowed) => allowed.toLowerCase().replace(/\.$/u, "") === host)
  ) {
    throw new Error("MCP endpoint is outside the declared network policy.");
  }
}

function readProtectedFile(path: string, maximum: number, label: string): Buffer {
  let fd = -1;
  try {
    fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const stat = fstatSync(fd);
    if (!stat.isFile() || stat.mode & 0o077 || stat.size < 1 || stat.size > maximum)
      throw new Error(`${label} file is not a protected regular file.`);
    return readFileSync(fd);
  } catch (error) {
    if (error instanceof Error && error.message.includes("protected regular file")) throw error;
    throw new Error(`${label} file is unavailable.`);
  } finally {
    if (fd >= 0) closeSync(fd);
  }
}

function writeProtectedFile(path: string, bytes: Buffer): void {
  try {
    const existing = lstatSync(path);
    if (!existing.isFile() || existing.mode & 0o077)
      throw new Error("output path is not a protected regular file.");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
  const temporary = `${path}.${process.pid}.${randomBytes(8).toString("hex")}.tmp`;
  const fd = openSync(
    temporary,
    constants.O_CREAT | constants.O_EXCL | constants.O_WRONLY | constants.O_NOFOLLOW,
    0o600,
  );
  try {
    writeFileSync(fd, bytes);
    fsyncSync(fd);
  } finally {
    closeSync(fd);
  }
  try {
    renameSync(temporary, path);
  } catch (error) {
    try {
      unlinkSync(temporary);
    } catch {
      /* best effort cleanup */
    }
    throw error;
  }
}

function ensureDirectory(path: string): void {
  mkdirSync(path, { recursive: true, mode: 0o700 });
  const stat = lstatSync(path);
  if (!stat.isDirectory() || stat.mode & 0o002)
    throw new Error("Output directory is not a safe directory.");
}

function operatorPath(root: string, value: string): string {
  const path = resolve(root, value);
  const rel = relative(root, path);
  if (!rel || rel.startsWith("..") || isAbsolute(rel))
    throw new Error("Operator source path escapes config directory.");
  return path;
}

function requireAbsolute(value: string, label: string): string {
  if (!isAbsolute(value)) throw new Error(`${label} must be absolute.`);
  return resolve(value);
}

function digestBytes(value: Uint8Array): `sha256:${string}` {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function identifier(value: unknown): value is string {
  return (
    typeof value === "string" && /^[A-Za-z0-9](?:[A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$/u.test(value)
  );
}

function digestString(value: unknown): value is `sha256:${string}` {
  return typeof value === "string" && /^sha256:[0-9a-f]{64}$/u.test(value);
}

function validHost(value: string): boolean {
  return value.length > 0 && value.length <= 253 && !/[ /:?#@\t\r\n]/u.test(value);
}

function relativePath(value: unknown): value is string {
  return (
    typeof value === "string" &&
    value.length > 0 &&
    !isAbsolute(value) &&
    !value.split("/").includes("..") &&
    !value.includes("\u0000")
  );
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function onlyKeys(value: Record<string, unknown>, allowed: ReadonlyArray<string>): boolean {
  const keys = Object.keys(value);
  return keys.length === allowed.length && keys.every((key) => allowed.includes(key));
}

function usage(): never {
  throw new Error(
    "usage: bun scripts/generate-runtime-capability-materialization.ts --config ABSOLUTE_JSON --output-directory ABSOLUTE_DIR",
  );
}

if (import.meta.main) {
  const configIndex = process.argv.indexOf("--config");
  const outputIndex = process.argv.indexOf("--output-directory");
  if (
    configIndex < 0 ||
    outputIndex < 0 ||
    !process.argv[configIndex + 1] ||
    !process.argv[outputIndex + 1]
  )
    usage();
  const result = generateRuntimeCapabilityMaterialization(
    process.argv[configIndex + 1],
    process.argv[outputIndex + 1],
  );
  process.stdout.write(`${result.descriptorPath} ${result.descriptorDigest}\n`);
}
