import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

import { createDeterministicUstar } from "./platform-migration-ustar";

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
  "cloud-agents-control-plane",
  "cloud-agentsctl",
  "cloud-agents-worker",
  "cloud-agents-product-migrate",
] as const;

export const PLATFORM_RELEASE_RUNTIME = "cloud-agent-runtime-standalone.mjs";
export const PLATFORM_RELEASE_MIGRATIONS = "cloud-agents-migrations-000051.tar";
export const PLATFORM_RELEASE_DEPLOYMENT = "cloud-agents-deployment-000051.tar";
export const PLATFORM_RELEASE_CONTRACTS = "cloud-agents-contract-bundle.tar";
export const PLATFORM_RELEASE_GO_SDK = "cloud-agents-go-sdk.tar";
export const PLATFORM_RELEASE_TYPESCRIPT_SDK = "cloud-agents-typescript-sdk.tgz";
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
  const manifestPath = "services/control-plane/migrations/product/000051/manifest.json";
  const manifest = JSON.parse(readFileSync(resolve(root, manifestPath), "utf8")) as {
    readonly schema_bundle: {
      readonly migrations: ReadonlyArray<{
        readonly sql_artifact: { readonly path: string };
        readonly catalog_contract: { readonly path: string };
      }>;
    };
  };
  const paths = new Set<string>([
    "LICENSE",
    manifestPath,
    "services/control-plane/migrations/product/000051/schema-bundle.json",
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
    "LICENSE",
    "deploy/compose/.env.example",
    "deploy/compose/README.md",
    "deploy/compose/cloud-agents-up.sh",
    "deploy/compose/docker-compose.yml",
    "deploy/compose/provision.sql",
    "deploy/compose/runtime.env.example",
    "deploy/docker/control-plane.Dockerfile",
    "deploy/docker/migrate.Dockerfile",
    "deploy/docker/worker.Dockerfile",
    "scripts/prepare-platform-docker-target.sh",
    "scripts/prepare-platform-kubernetes-target.sh",
    "scripts/test-platform-agent-interactions.sh",
    "scripts/test-platform-kubernetes-target.sh",
    "scripts/test-platform-ssh-target.sh",
    ...readTree(root, "deploy/helm/cloud-agents"),
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

export function buildPlatformContractPackage(root: string): Uint8Array {
  const paths = [
    "LICENSE",
    ...readTree(root, "contracts/common/v1alpha1").filter((path) => !path.endsWith("README.md")),
    ...readTree(root, "contracts/managed-agent/v1alpha1"),
    ...readTree(root, "contracts/managed-host/v1alpha1"),
    ...readTree(root, "contracts/worker/v1alpha1").filter((path) => !path.endsWith("README.md")),
    ...readTree(root, "contracts/worker/runtime/v1alpha1"),
    ...PUBLIC_PLATFORM_CONTRACT_PATHS,
    "contracts/generated/proto/cloud-agents-worker-runtime-v1alpha1.binpb",
  ];
  return createDeterministicUstar(
    [...new Set(paths)].map((path) => ({ path, data: readFileSync(resolve(root, path)) })),
  );
}

export function buildPlatformGoSDKPackage(root: string): Uint8Array {
  const paths = [
    "sdk/go/LICENSE",
    "sdk/go/THIRD_PARTY_NOTICES.md",
    "sdk/go/doc.go",
    "sdk/go/go.mod",
    "sdk/go/go.sum",
    ...readTree(root, "sdk/go/runtime").filter(
      (path) => path.endsWith(".go") && !path.endsWith("_test.go"),
    ),
    ...readTree(root, "sdk/go/gen").filter(
      (path) =>
        path.endsWith(".go") && !path.endsWith("_test.go") && !path.includes("/platformadapter/"),
    ),
  ];
  return createDeterministicUstar(
    paths.map((path) => ({
      path: path.replace("sdk/go/", ""),
      data: readFileSync(resolve(root, path)),
    })),
  );
}

export function buildPlatformTypeScriptSDKPackage(root: string, version: string): Uint8Array {
  if (!SEMVER.test(version)) throw new Error("TypeScript SDK version is not semver.");
  const source = JSON.parse(
    readFileSync(resolve(root, "sdk/typescript/package.json"), "utf8"),
  ) as Record<string, unknown>;
  if (source.name !== "@cloud-agents/cloud-agent-platform-sdk") {
    throw new Error("TypeScript SDK package identity is invalid.");
  }
  const manifest = {
    ...source,
    version,
    description: "Cloud Agents Platform TypeScript SDK",
    files: ["dist", "LICENSE", "README.md", "THIRD_PARTY_NOTICES.md"],
  };
  delete manifest.private;
  delete manifest.scripts;
  delete manifest.devDependencies;
  const files = [
    "sdk/typescript/LICENSE",
    "sdk/typescript/README.md",
    "sdk/typescript/THIRD_PARTY_NOTICES.md",
    ...readTree(root, "sdk/typescript/dist"),
  ];
  const archive = createDeterministicUstar([
    {
      path: "package/package.json",
      data: Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`),
    },
    ...files.map((path) => ({
      path: `package/${path.replace("sdk/typescript/", "")}`,
      data: readFileSync(resolve(root, path)),
    })),
  ]);
  return gzipSync(archive, { level: 9 });
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
      PLATFORM_RELEASE_GO_COMMANDS.filter((name) => name !== "cloud-agentsctl").map((name) => ({
        name,
        target,
      })),
    ),
    ...PLATFORM_RELEASE_CLI_TARGETS.map((target) => ({ name: "cloud-agentsctl", target })),
    { name: "cloud-agent-runtime", target: "portable" },
    { name: "cloud-agents-migrations", target: "portable" },
    { name: "cloud-agents-deployment", target: "portable" },
    { name: "cloud-agents-contracts", target: "portable" },
    { name: "cloud-agents-go-sdk", target: "portable" },
    { name: "cloud-agents-typescript-sdk", target: "portable" },
  ];
}

export function expectedArtifactCount(): number {
  return (
    PLATFORM_RELEASE_TARGETS.length * (PLATFORM_RELEASE_GO_COMMANDS.length - 1) +
    PLATFORM_RELEASE_CLI_TARGETS.length +
    6
  );
}

const PUBLIC_PLATFORM_CONTRACT_PATHS = [
  "contracts/platform/v1alpha1/fixtures/golden/membership.json",
  "contracts/platform/v1alpha1/fixtures/golden/organization.json",
  "contracts/platform/v1alpha1/fixtures/golden/platform-tenant.json",
  "contracts/platform/v1alpha1/fixtures/golden/project-create-request.json",
  "contracts/platform/v1alpha1/fixtures/golden/project.json",
  "contracts/platform/v1alpha1/fixtures/golden/role-binding.json",
  "contracts/platform/v1alpha1/fixtures/golden/role.json",
  "contracts/platform/v1alpha1/fixtures/negative/cross-tenant-project.json",
  "contracts/platform/v1alpha1/fixtures/negative/organization-tenant-ref-mismatch.json",
  "contracts/platform/v1alpha1/fixtures/negative/project-create-server-owned-field.json",
  "contracts/platform/v1alpha1/fixtures/negative/project-response-n-minus-one.json",
  "contracts/platform/v1alpha1/fixtures/negative/role-binding-scope-mismatch.json",
  "contracts/platform/v1alpha1/fixtures/negative/role-binding-unknown-role.json",
  "contracts/platform/v1alpha1/fixtures/negative/role-wildcard-permission.json",
  "contracts/platform/v1alpha1/schemas/deployment-target-probe-request.schema.json",
  "contracts/platform/v1alpha1/schemas/deployment-target-register-request.schema.json",
  "contracts/platform/v1alpha1/schemas/deployment-target-scheduling-preview.schema.json",
  "contracts/platform/v1alpha1/schemas/deployment-target-scheduling-request.schema.json",
  "contracts/platform/v1alpha1/schemas/deployment-target.schema.json",
  "contracts/platform/v1alpha1/schemas/environment-lease-create-request.schema.json",
  "contracts/platform/v1alpha1/schemas/environment-lease-terminate-request.schema.json",
  "contracts/platform/v1alpha1/schemas/environment-lease-upgrade-request.schema.json",
  "contracts/platform/v1alpha1/schemas/environment-lease.schema.json",
  "contracts/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json",
  "contracts/platform/v1alpha1/schemas/membership-create-request.schema.json",
  "contracts/platform/v1alpha1/schemas/membership-page.schema.json",
  "contracts/platform/v1alpha1/schemas/membership.schema.json",
  "contracts/platform/v1alpha1/schemas/membership-transition-request.schema.json",
  "contracts/platform/v1alpha1/schemas/organization-create-request.schema.json",
  "contracts/platform/v1alpha1/schemas/organization-page.schema.json",
  "contracts/platform/v1alpha1/schemas/organization.schema.json",
  "contracts/platform/v1alpha1/schemas/permission.schema.json",
  "contracts/platform/v1alpha1/schemas/platform-tenant.schema.json",
  "contracts/platform/v1alpha1/schemas/project-create-request.schema.json",
  "contracts/platform/v1alpha1/schemas/project-page.schema.json",
  "contracts/platform/v1alpha1/schemas/project.schema.json",
  "contracts/platform/v1alpha1/schemas/rbac-mutation-result.schema.json",
  "contracts/platform/v1alpha1/schemas/role-binding-create-request.schema.json",
  "contracts/platform/v1alpha1/schemas/role-binding-page.schema.json",
  "contracts/platform/v1alpha1/schemas/role-binding.schema.json",
  "contracts/platform/v1alpha1/schemas/role-binding-revoke-request.schema.json",
  "contracts/platform/v1alpha1/schemas/role-page.schema.json",
  "contracts/platform/v1alpha1/schemas/role.schema.json",
  "contracts/platform/v1alpha1/schemas/worker-release-page.schema.json",
  "contracts/platform/v1alpha1/schemas/worker-release-register-request.schema.json",
  "contracts/platform/v1alpha1/schemas/worker-release.schema.json",
] as const;

function readTree(root: string, directory: string): string[] {
  const absolute = resolve(root, directory);
  return readdirSync(absolute, { withFileTypes: true }).flatMap((entry) => {
    const path = `${directory}/${entry.name}`;
    if (entry.isDirectory()) return readTree(root, path);
    if (!entry.isFile()) throw new Error(`release package member is not a file: ${path}`);
    return [path];
  });
}

function requireString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`${label} is missing.`);
  return value;
}

function isRecord(value: unknown): value is Record<string, any> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
