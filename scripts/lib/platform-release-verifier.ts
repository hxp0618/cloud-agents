import { createHash } from "node:crypto";
import { lstatSync, readFileSync, realpathSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

export const PLATFORM_RELEASE_TARGETS = ["linux-amd64", "linux-arm64"] as const;
export const PLATFORM_RELEASE_CLI_TARGETS = [
  "linux-amd64",
  "linux-arm64",
  "darwin-amd64",
  "darwin-arm64",
  "windows-amd64",
  "windows-arm64",
] as const;
export type PlatformReleaseTarget =
  | (typeof PLATFORM_RELEASE_TARGETS)[number]
  | (typeof PLATFORM_RELEASE_CLI_TARGETS)[number];

export const PLATFORM_RELEASE_GO_COMMANDS = [
  "cloud-agents-access-gateway",
  "cloud-agents-control-plane",
  "cloud-agents-remote-worker",
  "cloud-agentsctl",
  "cloud-agents-worker",
  "cloud-agents-product-migrate",
] as const;

export const PLATFORM_RELEASE_RUNTIME = "cloud-agent-runtime-standalone.mjs";
export const CLOUD_AGENT_RUNTIME_NOTICES_FILENAME = "cloud-agent-runtime-notices.md";
export const CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH =
  "packages/cloud-agent-runtime/THIRD_PARTY_NOTICES.md";

export function buildRuntimeNotice(root: string): {
  readonly artifact: PlatformReleaseArtifact;
  readonly bytes: Buffer;
} {
  const bytes = readFileSync(resolve(root, CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH));
  if (bytes.length === 0) throw new Error("Runtime third-party notice is empty.");
  return {
    artifact: platformReleaseArtifact(
      "cloud-agent-runtime-notices",
      "portable",
      CLOUD_AGENT_RUNTIME_NOTICES_FILENAME,
      bytes,
    ),
    bytes,
  };
}

export const PLATFORM_RELEASE_CONTRACTS = "cloud-agents-contract-bundle.tar";
export const PLATFORM_RELEASE_GO_SDK = "cloud-agents-go-sdk.tar";
export const PLATFORM_RELEASE_TYPESCRIPT_SDK = "cloud-agents-typescript-sdk.tgz";

const SHA256 = /^sha256:[0-9a-f]{64}$/u;
const SEMVER =
  /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/u;
const COMMIT = /^[0-9a-f]{40}$/u;
const CHECKSUM_LINE = /^([0-9a-f]{64})  ([^/\\]+)$/u;

export type PlatformReleaseArtifact = {
  readonly name: string;
  readonly target: string;
  readonly filename: string;
  readonly sizeBytes: number;
  readonly sha256: string;
};

export type PlatformReleaseManifest = {
  readonly schemaVersion: 1;
  readonly kind: "cloud-agents-platform-release";
  readonly version: string;
  readonly sourceCommit: string;
  readonly sourceDirty: boolean;
  readonly artifacts: ReadonlyArray<PlatformReleaseArtifact>;
};

export function platformReleaseArtifact(
  name: string,
  target: string,
  filename: string,
  bytes: Uint8Array,
): PlatformReleaseArtifact {
  if (!name || !target || !isPlatformArtifactFilename(filename)) {
    throw new Error("platform release artifact identity is invalid.");
  }
  return {
    name,
    target,
    filename,
    sizeBytes: bytes.byteLength,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
  };
}

export function isPlatformReleaseVersion(value: string): boolean {
  return SEMVER.test(value);
}

export function isPlatformArtifactFilename(value: string): boolean {
  return value !== "." && value !== ".." && !value.includes("/") && !value.includes("\\");
}

export function validatePlatformReleaseManifest(
  manifest: unknown,
): asserts manifest is PlatformReleaseManifest {
  if (
    !isRecord(manifest) ||
    manifest.schemaVersion !== 1 ||
    manifest.kind !== "cloud-agents-platform-release"
  ) {
    throw new Error("platform release manifest identity is invalid.");
  }
  const version = requireString(manifest.version, "platform release version");
  if (!isPlatformReleaseVersion(version)) {
    throw new Error("platform release version is not semver.");
  }
  if (typeof manifest.sourceCommit !== "string" || !COMMIT.test(manifest.sourceCommit)) {
    throw new Error("platform release source commit is invalid.");
  }
  if (typeof manifest.sourceDirty !== "boolean") {
    throw new Error("platform release sourceDirty is invalid.");
  }
  if (!Array.isArray(manifest.artifacts) || manifest.artifacts.length !== expectedArtifactCount()) {
    throw new Error(`platform release must contain ${String(expectedArtifactCount())} artifacts.`);
  }
  const identities = new Set<string>();
  const filenames = new Set<string>();
  const expected = new Set(
    expectedArtifactIdentities().map(({ name, target }) => `${name}\0${target}`),
  );
  const migrationHead = platformReleaseMigrationHead(manifest.artifacts);
  for (const artifact of manifest.artifacts) {
    if (!isRecord(artifact)) throw new Error("platform release artifact is invalid.");
    const name = requireString(artifact.name, "artifact name");
    const target = requireString(artifact.target, "artifact target");
    const identity = `${name}\0${target}`;
    if (identities.has(identity)) throw new Error(`duplicate platform artifact ${identity}.`);
    identities.add(identity);
    if (!expected.has(identity)) throw new Error(`unexpected platform artifact ${identity}.`);
    const filename = requireString(artifact.filename, "artifact filename");
    if (
      !isPlatformArtifactFilename(filename) ||
      filename !== platformReleaseArtifactFilename(name, target, migrationHead) ||
      filenames.has(filename)
    ) {
      throw new Error("platform artifact filename is invalid or duplicated.");
    }
    filenames.add(filename);
    if (
      typeof artifact.sizeBytes !== "number" ||
      !Number.isSafeInteger(artifact.sizeBytes) ||
      artifact.sizeBytes <= 0
    ) {
      throw new Error("platform artifact size is invalid.");
    }
    if (typeof artifact.sha256 !== "string" || !SHA256.test(artifact.sha256)) {
      throw new Error("platform artifact sha256 is invalid.");
    }
  }
  if (identities.size !== expected.size) {
    throw new Error("platform release artifact identities are incomplete.");
  }
}

export function validatePlatformReleaseDirectory(directory: string): PlatformReleaseManifest {
  const root = realpathSync(directory);
  const manifestBytes = readRegularFile(join(root, "platform-release-manifest.json"));
  let manifest: unknown;
  try {
    manifest = JSON.parse(manifestBytes.toString("utf8"));
  } catch {
    throw new Error("platform release manifest is not valid JSON.");
  }
  validatePlatformReleaseManifest(manifest);

  const checksumLines = readRegularFile(join(root, "checksums.sha256"))
    .toString("utf8")
    .split("\n");
  if (checksumLines.at(-1) === "") checksumLines.pop();
  const checksums = new Map<string, string>();
  for (const line of checksumLines) {
    const match = CHECKSUM_LINE.exec(line);
    if (!match || !isPlatformArtifactFilename(match[2]!) || checksums.has(match[2]!)) {
      throw new Error("platform release checksums are malformed or duplicated.");
    }
    checksums.set(match[2]!, `sha256:${match[1]!}`);
  }
  if (checksums.size !== manifest.artifacts.length) {
    throw new Error("platform release checksums do not match the manifest artifact set.");
  }
  for (const artifact of manifest.artifacts) {
    if (checksums.get(artifact.filename) !== artifact.sha256) {
      throw new Error(`platform release checksum does not match ${artifact.filename}.`);
    }
    const bytes = readRegularFile(join(root, artifact.filename));
    const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
    if (bytes.byteLength !== artifact.sizeBytes || digest !== artifact.sha256) {
      throw new Error(
        `platform release artifact ${artifact.filename} failed integrity validation.`,
      );
    }
  }
  return manifest;
}

export function expectedArtifactIdentities(): ReadonlyArray<{
  readonly name: string;
  readonly target: string;
}> {
  return [
    ...PLATFORM_RELEASE_TARGETS.flatMap((target) =>
      PLATFORM_RELEASE_GO_COMMANDS.filter((name) => name !== "cloud-agentsctl").map((name) => ({
        name,
        target,
      })),
    ),
    ...PLATFORM_RELEASE_CLI_TARGETS.map((target) => ({
      name: "cloud-agentsctl",
      target,
    })),
    ...PLATFORM_RELEASE_TARGETS.map((target) => ({
      name: "cloud-agents-landlock-run",
      target,
    })),
    { name: "cloud-agents-landlock-notices", target: "portable" },
    { name: "cloud-agents-worker-oci-install-manifest", target: "portable" },
    { name: "cloud-agents-worker-oci-notices", target: "portable" },
    { name: "cloud-agent-runtime", target: "portable" },
    { name: "cloud-agent-runtime-notices", target: "portable" },
    { name: "cloud-agents-migrations", target: "portable" },
    { name: "cloud-agents-deployment", target: "portable" },
    { name: "cloud-agents-contracts", target: "portable" },
    { name: "cloud-agents-go-sdk", target: "portable" },
    { name: "cloud-agents-typescript-sdk", target: "portable" },
  ];
}

export function platformReleaseArtifactFilename(
  name: string,
  target: string,
  migrationHead: string,
): string {
  if (!/^\d{6}$/u.test(migrationHead)) {
    throw new Error("platform release migration head is invalid.");
  }
  if (name === "cloud-agentsctl" && PLATFORM_RELEASE_CLI_TARGETS.some((item) => item === target)) {
    return `${name}-${target}${target.startsWith("windows-") ? ".exe" : ""}`;
  }
  if (
    PLATFORM_RELEASE_GO_COMMANDS.some(
      (command) => command === name && command !== "cloud-agentsctl",
    ) &&
    PLATFORM_RELEASE_TARGETS.some((item) => item === target)
  ) {
    return `${name}-${target}`;
  }
  if (
    name === "cloud-agents-landlock-run" &&
    PLATFORM_RELEASE_TARGETS.some((item) => item === target)
  ) {
    return `${name}-${target}`;
  }
  if (target === "portable") {
    const portable = new Map([
      ["cloud-agents-landlock-notices", "cloud-agents-landlock-notices.txt"],
      ["cloud-agents-worker-oci-install-manifest", "cloud-agents-worker-oci-install-manifest.json"],
      ["cloud-agents-worker-oci-notices", "cloud-agents-worker-oci-notices.md"],
      ["cloud-agent-runtime", PLATFORM_RELEASE_RUNTIME],
      ["cloud-agent-runtime-notices", CLOUD_AGENT_RUNTIME_NOTICES_FILENAME],
      ["cloud-agents-migrations", `cloud-agents-migrations-${migrationHead}.tar`],
      ["cloud-agents-deployment", `cloud-agents-deployment-${migrationHead}.tar`],
      ["cloud-agents-contracts", PLATFORM_RELEASE_CONTRACTS],
      ["cloud-agents-go-sdk", PLATFORM_RELEASE_GO_SDK],
      ["cloud-agents-typescript-sdk", PLATFORM_RELEASE_TYPESCRIPT_SDK],
    ]);
    const filename = portable.get(name);
    if (filename) return filename;
  }
  throw new Error(`unexpected platform artifact ${name}\0${target}.`);
}

export function expectedArtifactCount(): number {
  return expectedArtifactIdentities().length;
}

function readRegularFile(path: string): Buffer {
  const stat = lstatSync(path);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`platform release input is not a regular file: ${path}`);
  }
  return readFileSync(path);
}

function platformReleaseMigrationHead(artifacts: ReadonlyArray<unknown>): string {
  const migration = artifacts.find(
    (artifact) =>
      isRecord(artifact) &&
      artifact.name === "cloud-agents-migrations" &&
      artifact.target === "portable",
  );
  const deployment = artifacts.find(
    (artifact) =>
      isRecord(artifact) &&
      artifact.name === "cloud-agents-deployment" &&
      artifact.target === "portable",
  );
  const migrationMatch =
    isRecord(migration) && typeof migration.filename === "string"
      ? /^cloud-agents-migrations-(\d{6})\.tar$/u.exec(migration.filename)
      : null;
  const deploymentMatch =
    isRecord(deployment) && typeof deployment.filename === "string"
      ? /^cloud-agents-deployment-(\d{6})\.tar$/u.exec(deployment.filename)
      : null;
  if (!migrationMatch || !deploymentMatch || migrationMatch[1] !== deploymentMatch[1]) {
    throw new Error("platform release migration and deployment filenames are inconsistent.");
  }
  return migrationMatch[1]!;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== "string" || value === "") throw new Error(`${label} is required.`);
  return value;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv.length !== 3) {
    process.stderr.write("usage: node platform-release-verifier.ts PLATFORM_RELEASE_DIRECTORY\n");
    process.exitCode = 2;
  } else {
    validatePlatformReleaseDirectory(process.argv[2]!);
  }
}
