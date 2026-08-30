import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import { readDeterministicUstar } from "./platform-migration-ustar";
import {
  expectedArtifactIdentities,
  buildPlatformContractPackage,
  buildPlatformGoSDKPackage,
  buildPlatformMigrationPackage,
  buildPlatformDeploymentPackage,
  parsePlatformReleaseOptions,
  platformReleaseArtifact,
  PLATFORM_RELEASE_CLI_TARGETS,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_TARGETS,
  validatePlatformReleaseManifest,
} from "./platform-release";

describe("platform release", () => {
  it("requires a version and a new output directory", () => {
    expect(() => parsePlatformReleaseOptions([])).toThrow(/Usage/);
    expect(() =>
      parsePlatformReleaseOptions(["--version", "not-semver", "--output-dir", "out"]),
    ).toThrow(/Usage/);
    expect(
      parsePlatformReleaseOptions(["--version", "0.1.0-rc.1", "--output-dir", "out"], "/tmp"),
    ).toEqual({
      version: "0.1.0-rc.1",
      outputDirectory: "/tmp/out",
      allowDirty: false,
    });
  });

  it("binds artifact-level size and sha256 only", () => {
    const bytes = new TextEncoder().encode("artifact");
    const artifact = platformReleaseArtifact(
      "control-plane",
      "linux-amd64",
      "control-plane-linux-amd64",
      bytes,
    );
    expect(artifact.sizeBytes).toBe(bytes.byteLength);
    expect(artifact.sha256).toBe(`sha256:${createHash("sha256").update(bytes).digest("hex")}`);
  });

  it("accepts the exact platform artifact set and rejects drift", () => {
    const artifacts = expectedArtifactIdentities().map(({ name, target }) => ({
      name,
      target,
      filename: `${name}-${target}${target.startsWith("windows-") ? ".exe" : ""}`,
      sizeBytes: 1,
      sha256: `sha256:${"a".repeat(64)}`,
    }));
    const manifest = {
      schemaVersion: 1,
      kind: "cloud-agents-platform-release",
      version: "0.1.0",
      sourceCommit: "a".repeat(40),
      sourceDirty: false,
      artifacts,
    };
    expect(() => validatePlatformReleaseManifest(manifest)).not.toThrow();
    expect(() =>
      validatePlatformReleaseManifest({ ...manifest, artifacts: artifacts.slice(1) }),
    ).toThrow(/artifacts/);
  });

  it("publishes the CLI for every supported desktop target while keeping services Linux-only", () => {
    expect(PLATFORM_RELEASE_CLI_TARGETS).toEqual([
      "linux-amd64",
      "linux-arm64",
      "darwin-amd64",
      "darwin-arm64",
      "windows-amd64",
      "windows-arm64",
    ]);
    expect(PLATFORM_RELEASE_TARGETS).toEqual(["linux-amd64", "linux-arm64"]);
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agentsctl");
    expect(PLATFORM_RELEASE_GO_COMMANDS).not.toContain("cloud-agents-evidencefs-provision");
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agentsctl",
      target: "darwin-arm64",
    });
    expect(expectedArtifactIdentities()).not.toContainEqual({
      name: "cloud-agents-worker",
      target: "darwin-arm64",
    });
  });

  it("packages the current product migration manifest, catalog, and SQL", () => {
    const archive = buildPlatformMigrationPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    expect(entries.some(({ path }) => path.endsWith("product/000025/manifest.json"))).toBe(true);
    expect(entries.filter(({ path }) => path.endsWith(".sql")).length).toBe(25);
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-migrations",
      target: "portable",
    });
  });

  it("packages the independent Compose and Helm deployment inputs", () => {
    const archive = buildPlatformDeploymentPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    expect(entries.map(({ path }) => path)).toEqual([
      "deploy/bootstrap/database.sql",
      "deploy/bootstrap/roles.sql",
      "deploy/compose/.env.example",
      "deploy/compose/README.md",
      "deploy/compose/docker-compose.yml",
      "deploy/compose/provision.sql",
      "deploy/compose/tenant-bootstrap.sql",
      "deploy/docker/control-plane.Dockerfile",
      "deploy/docker/migrate.Dockerfile",
      "deploy/docker/worker.Dockerfile",
      "deploy/helm/cloud-agents/Chart.yaml",
      "deploy/helm/cloud-agents/templates/_helpers.tpl",
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "deploy/helm/cloud-agents/templates/migrate-job.yaml",
      "deploy/helm/cloud-agents/templates/network-policy.yaml",
      "deploy/helm/cloud-agents/templates/worker.yaml",
      "deploy/helm/cloud-agents/templates/workspace-pvc.yaml",
      "deploy/helm/cloud-agents/values.schema.json",
      "deploy/helm/cloud-agents/values.yaml",
    ]);
  });

  it("keeps the process-local execution coordinator on one Control Plane replica", () => {
    const schema = JSON.parse(
      readFileSync("deploy/helm/cloud-agents/values.schema.json", "utf8"),
    ) as { properties: { controlPlane: { properties: { replicas: { const: number } } } } };
    expect(schema.properties.controlPlane.properties.replicas.const).toBe(1);
    expect(readFileSync("deploy/helm/cloud-agents/templates/control-plane.yaml", "utf8")).toContain(
      "type: Recreate",
    );
  });

  it("packages an atomic Compose database authority bootstrap", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    expect(compose).toContain('command:\n      - >-\n        exec psql');
    expect(compose).toContain("--single-transaction");
    expect(compose).toContain("/deploy/compose/provision.sql");
    expect(compose).toContain("/deploy/compose/tenant-bootstrap.sql");
    expect(compose).toContain("-P pager=off");
    const environment = readFileSync("deploy/compose/.env.example", "utf8");
    expect(environment).toContain("postgresql://cloud_agents_runtime_login:");
    expect(environment).not.toContain("postgresql://cloud_agents_runtime:");
  });

  it("selects matching OCI base and binary architectures", () => {
    const users = {
      "control-plane": "USER 65532:65532",
      worker: "USER 1000:1000",
      migrate: "USER 999:999",
    } as const;
    for (const name of ["control-plane", "worker", "migrate"] as const) {
      const dockerfile = readFileSync(`deploy/docker/${name}.Dockerfile`, "utf8");
      expect(dockerfile).toContain("ARG TARGETOS");
      expect(dockerfile).toContain("ARG TARGETARCH");
      expect(dockerfile).toContain("${TARGETOS}-${TARGETARCH}");
      expect(dockerfile).not.toContain("ARG TARGET=");
      expect(dockerfile).toContain(users[name]);
    }
    const migrationJob = readFileSync(
      "deploy/helm/cloud-agents/templates/migrate-job.yaml",
      "utf8",
    );
    expect(migrationJob).toContain("runAsNonRoot: true");
    expect(migrationJob).toContain("readOnlyRootFilesystem: true");
    expect(migrationJob).toContain("drop: [ALL]");
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    expect(compose.match(/platform: \$\{CLOUD_AGENTS_PLATFORM:-linux\/amd64\}/gu)).toHaveLength(3);
    expect(compose).not.toContain("CLOUD_AGENTS_TARGET");
    const migrateDockerfile = readFileSync("deploy/docker/migrate.Dockerfile", "utf8");
    expect(migrateDockerfile).toContain("cloud-agents-migrations-000025.tar");
    expect(migrateDockerfile).toContain("product/000025/manifest.json");
    expect(migrateDockerfile).not.toContain("000024");
    const workerDockerfile = readFileSync("deploy/docker/worker.Dockerfile", "utf8");
    expect(workerDockerfile).toContain("@openai/codex@0.150.1");
    expect(workerDockerfile).not.toContain("@openai/codex@latest");
  });

  it("packages public contracts without internal provenance inputs", () => {
    const entries = readDeterministicUstar(buildPlatformContractPackage(process.cwd()));
    const paths = entries.map(({ path }) => path);
    expect(paths).toContain("contracts/managed-agent/v1alpha1/openapi.json");
    expect(paths).toContain("contracts/managed-host/v1alpha1/openapi.json");
    expect(paths).toContain("contracts/worker/runtime/v1alpha1/runtime.proto");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/project.schema.json");
    expect(paths).toContain(
      "contracts/platform/v1alpha1/schemas/environment-lease.schema.json",
    );
    expect(paths).not.toContain("contracts/platform/v1alpha1/fixtures/manifest.json");
    expect(
      paths.some((path) => path.includes("generation.lock") || path.includes("docs/plan")),
    ).toBe(false);
    expect(
      paths.some((path) => path.includes("contract-closure") || path.includes("runner-ledger")),
    ).toBe(false);
    expect(paths.some((path) => path.includes("platform-adapter") || path.endsWith(".md"))).toBe(
      false,
    );
  });

  it("packages the independent Go SDK module", () => {
    const entries = readDeterministicUstar(buildPlatformGoSDKPackage(process.cwd()));
    const paths = entries.map(({ path }) => path);
    expect(paths).toContain("go.mod");
    expect(paths).toContain("gen/openapi/v1alpha1/client_generated.go");
    expect(paths).toContain("gen/cloudagents/worker/runtime/v1alpha1/runtime.pb.go");
    expect(
      paths.some(
        (path) =>
          path.endsWith("_test.go") ||
          path.includes("generated-manifest.json") ||
          path.includes("platformadapter"),
      ),
    ).toBe(false);
  });
});
