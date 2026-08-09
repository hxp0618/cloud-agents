import { isAbsolute, resolve } from "node:path";

import {
  createCloudAgentStdioClient,
  type CloudAgentStdioClient,
  type CloudAgentStdioClientOptions,
} from "@synara/cloud-agent-runtime";

import sourceManifest from "../manifest.json";

export type CloudAgentDistributionManifest = typeof sourceManifest;

/** Validates the executable distribution manifest while keeping it ordinary JSON. */
export function assertCloudAgentDistributionManifest(
  value: unknown,
): asserts value is CloudAgentDistributionManifest {
  const manifest = asRecord(value);
  if (!manifest || manifest.schemaVersion !== 1) {
    throw new Error("Cloud Agent Distribution manifest schemaVersion must be 1.");
  }
  const protocol = asRecord(manifest.runtimeProtocol);
  if (manifest.protocol !== "2.3" || protocol?.major !== 2 || protocol.minor !== 3) {
    throw new Error("Cloud Agent Distribution manifest must target Protocol 2.3.");
  }
  if (manifest.runtimeEvent !== 2 || manifest.providerPluginAbi !== 1) {
    throw new Error("Cloud Agent Distribution manifest ABI versions are unsupported.");
  }
  const runtime = asRecord(manifest.runtime);
  if (
    runtime?.package !== "@synara/cloud-agent-runtime" ||
    typeof runtime.version !== "string" ||
    !isExactSemver(runtime.version)
  ) {
    throw new Error("Cloud Agent Distribution manifest Runtime pin is invalid.");
  }
  if (!Array.isArray(manifest.providers) || manifest.providers.length === 0) {
    throw new Error("Cloud Agent Distribution manifest Provider allowlist is empty.");
  }
  const kinds = new Set<string>();
  for (const value of manifest.providers) {
    const provider = asRecord(value);
    if (
      !provider ||
      typeof provider.kind !== "string" ||
      !/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/u.test(provider.kind) ||
      typeof provider.package !== "string" ||
      !provider.package.startsWith("@synara/cloud-agent-provider-") ||
      typeof provider.version !== "string" ||
      !isExactSemver(provider.version) ||
      kinds.has(provider.kind)
    ) {
      throw new Error("Cloud Agent Distribution manifest Provider entry is invalid.");
    }
    kinds.add(provider.kind);
  }
}

export type CloudAgentDistributionClientOptions = Omit<CloudAgentStdioClientOptions, "command"> & {
  /** Absolute path to the packed `cloud-agent-runtime` executable. */
  readonly executable: string;
};

export interface CloudAgentRuntimeLaunchDescriptor {
  /** Absolute Node.js 24 executable selected by the host. */
  readonly executable: string;
  /** The bundled Runtime module; the stdio client appends `--protocol-v2`. */
  readonly args: readonly [string];
}

/** Creates a client for a packed runtime executable without host-private parsing. */
export function createCloudAgentDistributionClient(
  options: CloudAgentDistributionClientOptions,
): CloudAgentStdioClient {
  if (!isAbsolute(options.executable)) {
    throw new Error("Cloud Agent Runtime executable path must be absolute.");
  }
  const { executable, ...clientOptions } = options;
  return createCloudAgentStdioClient({ ...clientOptions, command: executable });
}

/** Resolves the stable bin entry from an installed package root. */
export function resolveCloudAgentRuntimeExecutable(packageRoot: string): string {
  if (!isAbsolute(packageRoot)) {
    throw new Error("Cloud Agent Distribution package root must be absolute.");
  }
  return resolve(packageRoot, "dist/stdio.mjs");
}

/**
 * Resolves a cross-platform launch descriptor for the bundled Runtime module.
 * Hosts embedding Electron must pass a real Node.js 24 executable rather than
 * Electron's `process.execPath`.
 */
export function resolveCloudAgentRuntimeLaunch(
  packageRoot: string,
  nodeExecutable: string,
): CloudAgentRuntimeLaunchDescriptor {
  if (!isAbsolute(nodeExecutable)) {
    throw new Error("Cloud Agent Runtime Node executable path must be absolute.");
  }
  return {
    executable: nodeExecutable,
    args: [resolveCloudAgentRuntimeExecutable(packageRoot)],
  };
}

function isExactSemver(value: string): boolean {
  return /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?$/u.test(value);
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}
