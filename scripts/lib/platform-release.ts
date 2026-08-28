import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { createDeterministicUstar } from "./platform-migration-ustar";

export const PLATFORM_RELEASE_TARGETS = ["linux-amd64", "linux-arm64"] as const;
export type PlatformReleaseTarget = (typeof PLATFORM_RELEASE_TARGETS)[number];

export const PLATFORM_RELEASE_GO_COMMANDS = [
  "cloud-agents-control-plane",
  "cloud-agents-worker",
  "cloud-agents-product-migrate",
  "cloud-agents-evidencefs-provision",
] as const;

export const PLATFORM_RELEASE_RUNTIME = "cloud-agent-runtime-standalone.mjs";
export const PLATFORM_RELEASE_MIGRATIONS = "cloud-agents-migrations-000017.tar";
export const PLATFORM_RELEASE_DEPLOYMENT = "cloud-agents-deployment-000017.tar";
const SHA256 = /^sha256:[0-9a-f]{64}$/u;
const SEMVER =
  /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/u;
const COMMIT = /^[0-9a-f]{40}$/u;

export type PlatformReleaseOptions = {
  readonly outputDirectory: string;
  readonly version: string;
  readonly allowDirty: boolean;
};

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

export function parsePlatformReleaseOptions(
  args: ReadonlyArray<string>,
  cwd = process.cwd(),
): PlatformReleaseOptions {
  let outputDirectory: string | undefined;
  let version: string | undefined;
  let allowDirty = false;
  for (let index = 0; index < args.length; index += 1) {
    const value = args[index];
    if (value === "--skip-build") {
      throw new Error(
        "--skip-build is not supported: a platform release must build every artifact.",
      );
    }
    if (value === "--allow-dirty") {
      allowDirty = true;
      continue;
    }
    if (value === "--output-dir" || value === "--version") {
      const candidate = args[index + 1];
      if (!candidate || candidate.startsWith("--")) {
        throw new Error(`${value} requires a value.`);
      }
      if (value === "--output-dir") outputDirectory = candidate;
      else version = candidate;
      index += 1;
      continue;
    }
    throw new Error(`Unknown argument: ${String(value)}`);
  }
  if (!outputDirectory || !version || !SEMVER.test(version)) {
    throw new Error(
      "Usage: bun scripts/cloud-agents-platform-release.ts --version <semver> --output-dir <new-directory>",
    );
  }
  return { outputDirectory: resolve(cwd, outputDirectory), version, allowDirty };
}

export function platformReleaseArtifact(
  name: string,
  target: string,
  filename: string,
  bytes: Uint8Array,
): PlatformReleaseArtifact {
  if (!name || !target || !filename || filename.includes("/")) {
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

export function buildPlatformMigrationPackage(root: string): Uint8Array {
  const manifestPath = "services/control-plane/migrations/product/000017/manifest.json";
  const manifest = JSON.parse(readFileSync(resolve(root, manifestPath), "utf8")) as {
    readonly schema_bundle: {
      readonly migrations: ReadonlyArray<{
        readonly sql_artifact: { readonly path: string };
        readonly catalog_contract: { readonly path: string };
      }>;
    };
  };
  const paths = new Set<string>([
    manifestPath,
    "services/control-plane/migrations/product/000017/schema-bundle.json",
  ]);
  for (const migration of manifest.schema_bundle.migrations) {
    paths.add(migration.sql_artifact.path);
    paths.add(migration.catalog_contract.path);
  }
  return createDeterministicUstar(
    [...paths].map((path) => ({ path, data: readFileSync(resolve(root, path)) })),
  );
}

export function buildPlatformDeploymentPackage(root: string): Uint8Array {
  const paths = [
    "deploy/compose/.env.example",
    "deploy/compose/README.md",
    "deploy/compose/docker-compose.yml",
    "deploy/docker/control-plane.Dockerfile",
    "deploy/docker/migrate.Dockerfile",
    "deploy/docker/worker.Dockerfile",
    "services/control-plane/migrations/bootstrap/database.sql",
    "services/control-plane/migrations/bootstrap/roles.sql",
  ];
  return createDeterministicUstar(
    paths.map((path) => ({
      path: path.startsWith("services/")
        ? path.replace("services/control-plane/migrations/bootstrap/", "deploy/bootstrap/")
        : path,
      data: readFileSync(resolve(root, path)),
    })),
  );
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
  requireString(manifest.version, "platform release version");
  if (!SEMVER.test(manifest.version)) throw new Error("platform release version is not semver.");
  if (typeof manifest.sourceCommit !== "string" || !COMMIT.test(manifest.sourceCommit)) {
    throw new Error("platform release source commit is invalid.");
  }
  if (typeof manifest.sourceDirty !== "boolean")
    throw new Error("platform release sourceDirty is invalid.");
  if (!Array.isArray(manifest.artifacts) || manifest.artifacts.length !== expectedArtifactCount()) {
    throw new Error(`platform release must contain ${String(expectedArtifactCount())} artifacts.`);
  }
  const identities = new Set<string>();
  const filenames = new Set<string>();
  const expected = new Set(
    expectedArtifactIdentities().map(({ name, target }) => `${name}\0${target}`),
  );
  for (const artifact of manifest.artifacts) {
    if (!isRecord(artifact)) throw new Error("platform release artifact is invalid.");
    const identity = `${requireString(artifact.name, "artifact name")}\0${requireString(artifact.target, "artifact target")}`;
    if (identities.has(identity)) throw new Error(`duplicate platform artifact ${identity}.`);
    identities.add(identity);
    if (!expected.has(identity)) throw new Error(`unexpected platform artifact ${identity}.`);
    const filename = requireString(artifact.filename, "artifact filename");
    if (filename.includes("/") || filenames.has(filename))
      throw new Error("platform artifact filename is invalid or duplicated.");
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
  if (identities.size !== expected.size)
    throw new Error("platform release artifact identities are incomplete.");
}

export function expectedArtifactIdentities(): ReadonlyArray<{
  readonly name: string;
  readonly target: string;
}> {
  return [
    ...PLATFORM_RELEASE_TARGETS.flatMap((target) =>
      PLATFORM_RELEASE_GO_COMMANDS.map((name) => ({ name, target })),
    ),
    { name: "cloud-agent-runtime", target: "portable" },
    { name: "cloud-agents-migrations", target: "portable" },
    { name: "cloud-agents-deployment", target: "portable" },
  ];
}

export function expectedArtifactCount(): number {
  return PLATFORM_RELEASE_TARGETS.length * PLATFORM_RELEASE_GO_COMMANDS.length + 3;
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`${label} is missing.`);
  return value;
}

function isRecord(value: unknown): value is Record<string, any> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
