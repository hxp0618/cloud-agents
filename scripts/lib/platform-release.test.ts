import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { gunzipSync } from "node:zlib";
import { describe, expect, it } from "vitest";

import { readDeterministicUstar } from "./platform-migration-ustar";
import {
  expectedArtifactIdentities,
  buildPlatformContractPackage,
  buildPlatformGoSDKPackage,
  buildPlatformTypeScriptSDKPackage,
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
    expect(entries.map(({ path }) => path)).toContain("LICENSE");
    expect(entries.some(({ path }) => path.endsWith("product/000039/manifest.json"))).toBe(true);
    expect(entries.filter(({ path }) => path.endsWith(".sql")).length).toBe(39);
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-migrations",
      target: "portable",
    });
  });

  it("packages the independent Compose and Helm deployment inputs", () => {
    const archive = buildPlatformDeploymentPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    expect(entries.map(({ path }) => path)).toEqual([
      "LICENSE",
      "deploy/bootstrap/database.sql",
      "deploy/bootstrap/roles.sql",
      "deploy/compose/.env.example",
      "deploy/compose/README.md",
      "deploy/compose/cloud-agents-up.sh",
      "deploy/compose/docker-compose.yml",
      "deploy/compose/provision.sql",
      "deploy/compose/runtime.env.example",
      "deploy/docker/control-plane.Dockerfile",
      "deploy/docker/migrate.Dockerfile",
      "deploy/docker/worker.Dockerfile",
      "deploy/helm/cloud-agents/Chart.yaml",
      "deploy/helm/cloud-agents/files/tenant-bootstrap.sql",
      "deploy/helm/cloud-agents/templates/_helpers.tpl",
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "deploy/helm/cloud-agents/templates/migrate-job.yaml",
      "deploy/helm/cloud-agents/templates/network-policy.yaml",
      "deploy/helm/cloud-agents/templates/tenant-bootstrap-job.yaml",
      "deploy/helm/cloud-agents/templates/worker.yaml",
      "deploy/helm/cloud-agents/templates/workspace-pvc.yaml",
      "deploy/helm/cloud-agents/values.schema.json",
      "deploy/helm/cloud-agents/values.yaml",
      "scripts/prepare-platform-docker-target.sh",
      "scripts/prepare-platform-kubernetes-target.sh",
      "scripts/test-platform-agent-interactions.sh",
      "scripts/test-platform-kubernetes-target.sh",
      "scripts/test-platform-ssh-target.sh",
    ]);
  });

  it("keeps process-local coordinators on singleton replicas", () => {
    const schema = JSON.parse(
      readFileSync("deploy/helm/cloud-agents/values.schema.json", "utf8"),
    ) as {
      properties: Record<
        "controlPlane" | "worker",
        { properties: { replicas: { const: number } } }
      >;
    };
    expect(schema.properties.controlPlane.properties.replicas.const).toBe(1);
    expect(schema.properties.worker.properties.replicas.const).toBe(1);
    for (const component of ["control-plane", "worker"]) {
      expect(
        readFileSync(`deploy/helm/cloud-agents/templates/${component}.yaml`, "utf8"),
      ).toContain("type: Recreate");
    }
  });

  it("mounts the persistent Runtime workspace only on the Worker", () => {
    const controlPlane = readFileSync(
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "utf8",
    );
    const worker = readFileSync("deploy/helm/cloud-agents/templates/worker.yaml", "utf8");
    const claim = readFileSync("deploy/helm/cloud-agents/templates/workspace-pvc.yaml", "utf8");
    expect(controlPlane).not.toContain("mountPath: /workspace");
    expect(worker).toContain("mountPath: /workspace");
    expect(claim).toContain("accessModes: [ReadWriteOnce]");
    expect(claim).not.toContain("ReadWriteMany");
  });

  it("delivers deployment-owned Provider credentials through the Worker", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const worker = readFileSync("deploy/helm/cloud-agents/templates/worker.yaml", "utf8");
    for (const deployment of [compose, worker]) {
      expect(deployment).toContain("--provider-credential-directory");
      expect(deployment).toContain("/run/cloud-agents/provider-credentials");
    }
    expect(worker).toContain("secretName: {{ .Values.runtime.credentialSecretName }}");
  });

  it("publishes component capacity limits in Compose and Helm", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const controlPlane = readFileSync(
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "utf8",
    );
    const worker = readFileSync("deploy/helm/cloud-agents/templates/worker.yaml", "utf8");
    for (const deployment of [compose, worker]) {
      expect(deployment).toContain("--runtime-max-sessions");
    }
    expect(compose).toContain("CLOUD_AGENTS_RUNTIME_MAX_SESSIONS:-4");
    expect(worker).toContain(".Values.runtime.maxSessions");
    for (const deployment of [compose, controlPlane]) {
      expect(deployment).toContain("--max-concurrent-requests");
    }
    expect(compose).toContain("CLOUD_AGENTS_CONTROL_PLANE_MAX_CONCURRENT_REQUESTS:-128");
    expect(controlPlane).toContain(".Values.controlPlane.maxConcurrentRequests");
  });

  it("packages an atomic Compose database authority bootstrap", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const up = readFileSync("deploy/compose/cloud-agents-up.sh", "utf8");
    expect(compose).toContain("command:\n      - >-\n        exec psql");
    expect(compose).toContain("--single-transaction");
    expect(compose).toContain("/deploy/compose/provision.sql");
    expect(compose).toContain("/deploy/helm/cloud-agents/files/tenant-bootstrap.sql");
    expect(compose).toContain("-P pager=off");
    const environment = readFileSync("deploy/compose/.env.example", "utf8");
    expect(environment).toContain("postgresql://cloud_agents_runtime_login:");
    expect(environment).not.toContain("postgresql://cloud_agents_runtime:");
    expect(up).toContain("set -eu");
    expect(up).toContain("--profile bootstrap run --rm bootstrap");
    expect(up).toContain("--profile tenant-bootstrap run --rm tenant-bootstrap");
    expect(up).toContain('exec docker compose --env-file "$environment_file"');
    expect(up.trimEnd().endsWith("up --build")).toBe(true);
  });

  it("backs up and atomically restores an empty Compose database", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    expect(compose).toContain("profiles: [backup]");
    expect(compose).toContain(
      "CLOUD_AGENTS_BACKUP_DATABASE_URL: ${CLOUD_AGENTS_BOOTSTRAP_DATABASE_URL:",
    );
    expect(compose).toContain('exec pg_dump --dbname="$${CLOUD_AGENTS_BACKUP_DATABASE_URL}"');
    expect(compose).toContain("--format=custom --no-owner");
    expect(compose).toContain("profiles: [restore]");
    expect(compose).toContain("pg_catalog.to_regnamespace('cloud_agents') IS NULL");
    expect(compose).toContain(
      "--exit-on-error --single-transaction --no-owner --role=cloud_agents_migration_owner",
    );
    expect(compose).not.toContain("pg_restore --clean");
  });

  it("bootstraps the first Helm tenant administrator after migrations", () => {
    const bootstrap = readFileSync(
      "deploy/helm/cloud-agents/templates/tenant-bootstrap-job.yaml",
      "utf8",
    );
    expect(bootstrap).toContain('"helm.sh/hook": pre-install');
    expect(bootstrap).toContain('"helm.sh/hook-weight": "-4"');
    expect(bootstrap).toContain('.Files.Get "files/tenant-bootstrap.sql"');
    expect(bootstrap).toContain(".Values.database.tenantBootstrapURLKey");
    expect(bootstrap).toContain(".Values.tenantBootstrap.secretName");
    expect(bootstrap).toContain("--single-transaction");
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
    expect(compose).toContain("CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY");
    expect(compose).toContain("CLOUD_AGENTS_PLATFORM_SSH_CREDENTIALS_DIRECTORY");
    const controlPlaneTemplate = readFileSync(
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "utf8",
    );
    expect(controlPlaneTemplate).toContain("kubernetesCredentialSecretName");
    expect(controlPlaneTemplate).toContain("sshCredentialSecretName");
    const migrateDockerfile = readFileSync("deploy/docker/migrate.Dockerfile", "utf8");
    expect(migrateDockerfile).toContain("cloud-agents-migrations-000039.tar");
    expect(migrateDockerfile).toContain("product/000039/manifest.json");
    expect(migrateDockerfile).not.toContain("000029");
    const workerDockerfile = readFileSync("deploy/docker/worker.Dockerfile", "utf8");
    expect(workerDockerfile).toContain("@openai/codex@0.150.1");
    expect(workerDockerfile).toContain(
      '"@anthropic-ai/claude-agent-sdk-linux-${claude_arch}@0.3.207"',
    );
    expect(workerDockerfile).toContain('test "$(claude --version)" = "2.1.207 (Claude Code)"');
    expect(workerDockerfile).toContain("chown 1000:1000 /workspace");
    expect(workerDockerfile).toContain("chmod 0700 /workspace");
    expect(workerDockerfile).not.toContain("@openai/codex@latest");
    expect(workerDockerfile).not.toContain(
      "@anthropic-ai/claude-agent-sdk-linux-${claude_arch}@latest",
    );
  });

  it("packages public contracts without internal provenance inputs", () => {
    const entries = readDeterministicUstar(buildPlatformContractPackage(process.cwd()));
    const paths = entries.map(({ path }) => path);
    expect(paths).toContain("LICENSE");
    expect(paths).toContain("contracts/managed-agent/v1alpha1/openapi.json");
    expect(paths).toContain("contracts/managed-host/v1alpha1/openapi.json");
    expect(paths).toContain("contracts/worker/runtime/v1alpha1/runtime.proto");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/project.schema.json");
    expect(paths).toContain(
      "contracts/platform/v1alpha1/schemas/organization-create-request.schema.json",
    );
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/organization-page.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/project-page.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/role-page.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/membership-page.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/role-binding-page.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/environment-lease.schema.json");
    expect(paths).toContain("contracts/platform/v1alpha1/schemas/deployment-target.schema.json");
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
    expect(paths).toContain("LICENSE");
    expect(paths).toContain("go.mod");
    expect(paths).toContain("runtime/protocol.go");
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
    const source = entries
      .filter(({ path }) => path.endsWith(".go"))
      .map(({ data }) => new TextDecoder().decode(data))
      .join("\n");
    for (const marker of [
      "Contract manifest:",
      "Generator source manifest:",
      "Generation config:",
    ]) {
      expect(source).not.toContain(marker);
    }
  });

  it("packages an installable public TypeScript SDK without internal provenance", () => {
    const version = "0.1.0-rc.1";
    const first = buildPlatformTypeScriptSDKPackage(process.cwd(), version);
    const second = buildPlatformTypeScriptSDKPackage(process.cwd(), version);
    expect(Buffer.from(first).equals(Buffer.from(second))).toBe(true);
    const entries = readDeterministicUstar(gunzipSync(first));
    const paths = entries.map(({ path }) => path);
    expect(paths).toContain("package/package.json");
    expect(paths).toContain("package/dist/platform.mjs");
    expect(paths).toContain("package/dist/platform.d.mts");
    expect(paths).toContain("package/LICENSE");
    expect(
      paths.some(
        (path) =>
          path.includes("generated-manifest") ||
          path.includes("/src/") ||
          path.endsWith(".test.ts"),
      ),
    ).toBe(false);
    const source = entries
      .filter(({ path }) => /\.(?:[cm]?js|[cm]?ts)$/u.test(path))
      .map(({ data }) => new TextDecoder().decode(data))
      .join("\n");
    for (const marker of [
      "Contract manifest:",
      "Generator source manifest:",
      "Generation config:",
    ]) {
      expect(source).not.toContain(marker);
    }
    const packageJSON = entries.find(({ path }) => path === "package/package.json");
    expect(packageJSON).toBeDefined();
    const manifest = JSON.parse(new TextDecoder().decode(packageJSON?.data)) as Record<
      string,
      unknown
    >;
    expect(manifest.name).toBe("@cloud-agents/cloud-agent-platform-sdk");
    expect(manifest.version).toBe(version);
    expect(manifest.private).toBeUndefined();
    expect(manifest.scripts).toBeUndefined();
    expect(manifest.devDependencies).toBeUndefined();
  });
});
