import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { gzipSync } from "node:zlib";

import { createDeterministicUstar } from "./platform-migration-ustar.ts";
import {
  expectedArtifactCount,
  expectedArtifactIdentities,
  isPlatformReleaseVersion,
  platformReleaseArtifact,
  platformReleaseArtifactFilename,
  PLATFORM_RELEASE_CLI_TARGETS,
  PLATFORM_RELEASE_CONTRACTS,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_GO_SDK,
  PLATFORM_RELEASE_RUNTIME,
  PLATFORM_RELEASE_TARGETS,
  PLATFORM_RELEASE_TYPESCRIPT_SDK,
  validatePlatformReleaseDirectory,
  validatePlatformReleaseManifest,
  type PlatformReleaseArtifact,
  type PlatformReleaseManifest,
  type PlatformReleaseTarget,
} from "./platform-release-verifier.ts";

export {
  expectedArtifactCount,
  expectedArtifactIdentities,
  platformReleaseArtifact,
  platformReleaseArtifactFilename,
  PLATFORM_RELEASE_CLI_TARGETS,
  PLATFORM_RELEASE_CONTRACTS,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_GO_SDK,
  PLATFORM_RELEASE_RUNTIME,
  PLATFORM_RELEASE_TARGETS,
  PLATFORM_RELEASE_TYPESCRIPT_SDK,
  validatePlatformReleaseDirectory,
  validatePlatformReleaseManifest,
  type PlatformReleaseArtifact,
  type PlatformReleaseManifest,
  type PlatformReleaseTarget,
};

export const PLATFORM_RELEASE_MIGRATION_HEAD = readdirSync(
  resolve(import.meta.dirname, "../../services/control-plane/migrations/product"),
  { withFileTypes: true },
)
  .filter((entry) => entry.isDirectory() && /^\d{6}$/u.test(entry.name))
  .map((entry) => entry.name)
  .sort()
  .at(-1)!;
export const PLATFORM_RELEASE_MIGRATIONS = `cloud-agents-migrations-${PLATFORM_RELEASE_MIGRATION_HEAD}.tar`;
export const PLATFORM_RELEASE_DEPLOYMENT = `cloud-agents-deployment-${PLATFORM_RELEASE_MIGRATION_HEAD}.tar`;
export type PlatformReleaseOptions = {
  readonly outputDirectory: string;
  readonly version: string;
  readonly allowDirty: boolean;
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
  if (!outputDirectory || !version || !isPlatformReleaseVersion(version)) {
    throw new Error(
      "Usage: bun scripts/cloud-agents-platform-release.ts --version <semver> --output-dir <new-directory>",
    );
  }
  return {
    outputDirectory: resolve(cwd, outputDirectory),
    version,
    allowDirty,
  };
}

export function buildPlatformMigrationPackage(root: string): Uint8Array {
  const manifestPath = `services/control-plane/migrations/product/${PLATFORM_RELEASE_MIGRATION_HEAD}/manifest.json`;
  const manifest = JSON.parse(readFileSync(resolve(root, manifestPath), "utf8")) as {
    readonly schema_bundle: {
      readonly migrations: ReadonlyArray<{
        readonly sql_artifact: { readonly path: string };
      }>;
    };
  };
  const paths = new Set<string>([
    "LICENSE",
    manifestPath,
    `services/control-plane/migrations/product/${PLATFORM_RELEASE_MIGRATION_HEAD}/schema-bundle.json`,
  ]);
  for (const migration of manifest.schema_bundle.migrations) {
    paths.add(migration.sql_artifact.path);
  }
  return createDeterministicUstar(
    [...paths].map((path) => ({
      path,
      data: readFileSync(resolve(root, path)),
    })),
  );
}

export function buildPlatformDeploymentPackage(root: string): Uint8Array {
  const paths = [
    "LICENSE",
    "deploy/compose/.env.example",
    "deploy/compose/README.md",
    "deploy/compose/cloud-agents-up.sh",
    "deploy/compose/docker-compose.managed-agent.yml",
    "deploy/compose/docker-compose.remote-worker.yml",
    "deploy/compose/docker-compose.yml",
    "deploy/compose/provision.sql",
    "deploy/compose/runtime.env.example",
    "deploy/web/server.mjs",
    ...readTree(root, "apps/admin-web/dist"),
    ...readTree(root, "apps/user-web/dist"),
    "deploy/docker/admin-web.Dockerfile",
    "deploy/docker/access-gateway.Dockerfile",
    "deploy/docker/control-plane.Dockerfile",
    "deploy/docker/migrate.Dockerfile",
    "deploy/docker/user-web.Dockerfile",
    "deploy/docker/worker.Dockerfile",
    "scripts/prepare-platform-docker-target.sh",
    "scripts/prepare-platform-kubernetes-target.sh",
    "scripts/bootstrap-platform-remote-worker.sh",
    "scripts/lib/platform-release-verifier.ts",
    "scripts/test-platform-compose-admin-web.mjs",
    "scripts/test-platform-helm.sh",
    "scripts/test-platform-agent-interactions.sh",
    "scripts/test-platform-kubernetes-target.sh",
    "scripts/test-platform-ssh-target.sh",
    ...readTree(root, "deploy/helm/cloud-agents"),
    "services/control-plane/migrations/bootstrap/database.sql",
    "services/control-plane/migrations/bootstrap/roles.sql",
  ];
  return createDeterministicUstar(
    paths.map((path) => ({
      path: deploymentPackagePath(root, path),
      data: deploymentPackageMember(root, path),
    })),
  );
}

function deploymentPackagePath(root: string, path: string): string {
  if (path.startsWith("services/")) {
    return path.replace("services/control-plane/migrations/bootstrap/", "deploy/bootstrap/");
  }
  for (const app of ["admin-web", "user-web"] as const) {
    if (path === `apps/${app}/dist/index.html`) {
      return `deploy/${app}/dist/index-${webIndexDigest(root, app)}.html`;
    }
    if (path.startsWith(`apps/${app}/dist/`)) {
      return path.replace(`apps/${app}/`, `deploy/${app}/`);
    }
  }
  return path;
}

function deploymentPackageMember(root: string, path: string): Buffer {
  const source = readFileSync(resolve(root, path));
  if (path === "deploy/docker/migrate.Dockerfile") {
    return Buffer.from(
      source
        .toString("utf8")
        .replaceAll("@PLATFORM_MIGRATION_ARCHIVE@", PLATFORM_RELEASE_MIGRATIONS)
        .replaceAll(
          "@PLATFORM_MIGRATION_MANIFEST@",
          `services/control-plane/migrations/product/${PLATFORM_RELEASE_MIGRATION_HEAD}/manifest.json`,
        ),
    );
  }
  const app =
    path === "deploy/docker/admin-web.Dockerfile"
      ? "admin-web"
      : path === "deploy/docker/user-web.Dockerfile"
        ? "user-web"
        : "";
  if (app === "") return source;
  const indexDigest = webIndexDigest(root, app);
  return Buffer.from(
    source
      .toString("utf8")
      .replace(
        `COPY ${app}/dist /opt/cloud-agents/web/dist`,
        `COPY ${app}/dist/assets /opt/cloud-agents/web/dist/assets\nCOPY ${app}/dist/index-${indexDigest}.html /opt/cloud-agents/web/dist/index.html`,
      ),
  );
}

function webIndexDigest(root: string, app: "admin-web" | "user-web"): string {
  return createHash("sha256")
    .update(readFileSync(resolve(root, `apps/${app}/dist/index.html`)))
    .digest("hex");
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
    [...new Set(paths)].map((path) => ({
      path,
      data: readFileSync(resolve(root, path)),
    })),
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
  if (!isPlatformReleaseVersion(version)) throw new Error("TypeScript SDK version is not semver.");
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
