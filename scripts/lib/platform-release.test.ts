import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
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
  platformReleaseArtifactFilename,
  platformReleaseArtifact,
  PLATFORM_RELEASE_CLI_TARGETS,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_MIGRATION_HEAD,
  PLATFORM_RELEASE_TARGETS,
  validatePlatformReleaseDirectory,
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
      filename: platformReleaseArtifactFilename(name, target, PLATFORM_RELEASE_MIGRATION_HEAD),
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
      validatePlatformReleaseManifest({
        ...manifest,
        artifacts: artifacts.slice(1),
      }),
    ).toThrow(/artifacts/);
  });

  it("verifies manifest, checksums, sizes and bytes before release use", () => {
    const directory = mkdtempSync(join(tmpdir(), "cloud-agents-platform-release-"));
    try {
      const artifacts = expectedArtifactIdentities().map(({ name, target }, index) => {
        const bytes = Buffer.from(`artifact-${String(index)}`);
        const artifact = platformReleaseArtifact(
          name,
          target,
          platformReleaseArtifactFilename(name, target, PLATFORM_RELEASE_MIGRATION_HEAD),
          bytes,
        );
        writeFileSync(join(directory, artifact.filename), bytes);
        return artifact;
      });
      const manifest = {
        schemaVersion: 1 as const,
        kind: "cloud-agents-platform-release" as const,
        version: "0.1.0",
        sourceCommit: "a".repeat(40),
        sourceDirty: false,
        artifacts,
      };
      writeFileSync(
        join(directory, "platform-release-manifest.json"),
        `${JSON.stringify(manifest)}\n`,
      );
      const checksums = `${artifacts.map(({ filename, sha256 }) => `${sha256.slice("sha256:".length)}  ${filename}`).join("\n")}\n`;
      writeFileSync(join(directory, "checksums.sha256"), checksums);

      expect(validatePlatformReleaseDirectory(directory)).toEqual(manifest);
      expect(
        spawnSync("node", ["scripts/lib/platform-release-verifier.ts", directory], {
          encoding: "utf8",
        }).status,
      ).toBe(0);

      const cliIndex = artifacts.findIndex(({ name }) => name === "cloud-agentsctl");
      const renamedCli = { ...artifacts[cliIndex]!, filename: "renamed-cloud-agentsctl" };
      const renamedArtifacts = artifacts.with(cliIndex, renamedCli);
      writeFileSync(
        join(directory, renamedCli.filename),
        readFileSync(join(directory, artifacts[cliIndex]!.filename)),
      );
      writeFileSync(
        join(directory, "platform-release-manifest.json"),
        `${JSON.stringify({ ...manifest, artifacts: renamedArtifacts })}\n`,
      );
      writeFileSync(
        join(directory, "checksums.sha256"),
        `${renamedArtifacts.map(({ filename, sha256 }) => `${sha256.slice("sha256:".length)}  ${filename}`).join("\n")}\n`,
      );
      expect(() => validatePlatformReleaseDirectory(directory)).toThrow(/filename/);
      writeFileSync(
        join(directory, "platform-release-manifest.json"),
        `${JSON.stringify(manifest)}\n`,
      );
      writeFileSync(join(directory, "checksums.sha256"), checksums);

      writeFileSync(
        join(directory, "checksums.sha256"),
        `${checksums[0] === "0" ? "1" : "0"}${checksums.slice(1)}`,
      );
      expect(() => validatePlatformReleaseDirectory(directory)).toThrow(/checksum does not match/);
      writeFileSync(join(directory, "checksums.sha256"), checksums);
      writeFileSync(join(directory, artifacts[0]!.filename), "tampered");
      expect(() => validatePlatformReleaseDirectory(directory)).toThrow(/integrity validation/);
      expect(
        spawnSync("node", ["scripts/lib/platform-release-verifier.ts", directory], {
          encoding: "utf8",
        }).status,
      ).not.toBe(0);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
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
    for (const target of PLATFORM_RELEASE_TARGETS) {
      expect(expectedArtifactIdentities()).toContainEqual({
        name: "cloud-agents-landlock-run",
        target,
      });
    }
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-landlock-notices",
      target: "portable",
    });
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agents-access-gateway");
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agents-remote-worker");
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
    expect(expectedArtifactIdentities()).not.toContainEqual({
      name: "cloud-agents-remote-worker",
      target: "darwin-arm64",
    });
  });

  it("packages only the product migrator runtime inputs", () => {
    const archive = buildPlatformMigrationPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    const productDirectory = `product/${PLATFORM_RELEASE_MIGRATION_HEAD}`;
    expect(entries.map(({ path }) => path)).toContain("LICENSE");
    expect(entries.some(({ path }) => path.endsWith(`${productDirectory}/manifest.json`))).toBe(
      true,
    );
    expect(
      entries.some(({ path }) => path.endsWith(`${productDirectory}/schema-bundle.json`)),
    ).toBe(true);
    expect(entries.filter(({ path }) => path.endsWith(".sql"))).toHaveLength(
      Number.parseInt(PLATFORM_RELEASE_MIGRATION_HEAD, 10),
    );
    expect(entries.some(({ path }) => path.includes("/catalog/"))).toBe(false);
    expect(PLATFORM_RELEASE_MIGRATION_HEAD).toMatch(/^\d{6}$/u);
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-migrations",
      target: "portable",
    });
  });

  it("packages the independent Compose and Helm deployment inputs", () => {
    const archive = buildPlatformDeploymentPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    const paths = entries.map(({ path }) => path);
    const adminAssets = paths.filter((path) => path.startsWith("deploy/admin-web/dist/assets/"));
    const userAssets = paths.filter((path) => path.startsWith("deploy/user-web/dist/assets/"));
    const indexDigest = (app: "admin-web" | "user-web") =>
      createHash("sha256")
        .update(readFileSync(`apps/${app}/dist/index.html`))
        .digest("hex");
    expect(adminAssets).toHaveLength(2);
    expect(userAssets).toHaveLength(2);
    expect(paths).toEqual([
      "LICENSE",
      ...adminAssets,
      `deploy/admin-web/dist/index-${indexDigest("admin-web")}.html`,
      "deploy/bootstrap/database.sql",
      "deploy/bootstrap/roles.sql",
      "deploy/compose/.env.example",
      "deploy/compose/README.md",
      "deploy/compose/cloud-agents-up.sh",
      "deploy/compose/docker-compose.managed-agent.yml",
      "deploy/compose/docker-compose.remote-worker.yml",
      "deploy/compose/docker-compose.yml",
      "deploy/compose/provision.sql",
      "deploy/compose/runtime.env.example",
      "deploy/docker/access-gateway.Dockerfile",
      "deploy/docker/admin-web.Dockerfile",
      "deploy/docker/control-plane.Dockerfile",
      "deploy/docker/migrate.Dockerfile",
      "deploy/docker/user-web.Dockerfile",
      "deploy/docker/worker.Dockerfile",
      "deploy/helm/cloud-agents/Chart.yaml",
      "deploy/helm/cloud-agents/README.md",
      "deploy/helm/cloud-agents/files/tenant-bootstrap.sql",
      "deploy/helm/cloud-agents/templates/_helpers.tpl",
      "deploy/helm/cloud-agents/templates/access-gateway.yaml",
      "deploy/helm/cloud-agents/templates/admin-web.yaml",
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "deploy/helm/cloud-agents/templates/migrate-job.yaml",
      "deploy/helm/cloud-agents/templates/network-policy.yaml",
      "deploy/helm/cloud-agents/templates/snapshot-pvc.yaml",
      "deploy/helm/cloud-agents/templates/tenant-bootstrap-job.yaml",
      "deploy/helm/cloud-agents/templates/user-web.yaml",
      "deploy/helm/cloud-agents/templates/worker.yaml",
      "deploy/helm/cloud-agents/templates/workspace-pvc.yaml",
      "deploy/helm/cloud-agents/values.schema.json",
      "deploy/helm/cloud-agents/values.yaml",
      ...userAssets,
      `deploy/user-web/dist/index-${indexDigest("user-web")}.html`,
      "deploy/web/server.mjs",
      "scripts/bootstrap-platform-remote-worker.sh",
      "scripts/lib/platform-release-verifier.ts",
      "scripts/prepare-platform-docker-target.sh",
      "scripts/prepare-platform-kubernetes-target.sh",
      "scripts/test-platform-agent-interactions.sh",
      "scripts/test-platform-compose-admin-web.mjs",
      "scripts/test-platform-helm.sh",
      "scripts/test-platform-kubernetes-target.sh",
      "scripts/test-platform-ssh-target.sh",
    ]);
    for (const app of ["admin-web", "user-web"]) {
      const dockerfile = new TextDecoder().decode(
        entries.find(({ path }) => path === `deploy/docker/${app}.Dockerfile`)!.data,
      );
      expect(dockerfile).toContain(
        `COPY ${app}/dist/index-${indexDigest(app)}.html /opt/cloud-agents/web/dist/index.html`,
      );
      expect(dockerfile).toContain(`COPY ${app}/dist/assets /opt/cloud-agents/web/dist/assets`);
      expect(dockerfile).not.toContain(`COPY ${app}/dist /opt/cloud-agents/web/dist`);
    }
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
    const managedAgent = readFileSync("deploy/compose/docker-compose.managed-agent.yml", "utf8");
    const worker = readFileSync("deploy/helm/cloud-agents/templates/worker.yaml", "utf8");
    for (const deployment of [managedAgent, worker]) {
      expect(deployment).toContain("--provider-credential-directory");
      expect(deployment).toContain("/run/cloud-agents/provider-credentials");
    }
    expect(compose).not.toContain("provider-credential");
    expect(compose).not.toContain("CLOUD_AGENTS_PLATFORM_WORKER");
    expect(compose).not.toContain("CLOUD_AGENTS_PLATFORM_ADMISSION");
    expect(worker).toContain("secretName: {{ .Values.runtime.credentialSecretName }}");
  });

  it("keeps acceptance model selection tenant-local", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain(
      '"$real_provider_credentials_directory/tenant-local.$provider.json"',
    );
  });

  it("keeps acceptance model values outside the harness source", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    expect(composeSmoke).not.toMatch(/\bgpt-[0-9]/);
  });

  it("packages opt-in Compose RemoteWorker authority", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const remoteWorker = readFileSync("deploy/compose/docker-compose.remote-worker.yml", "utf8");
    expect(compose).not.toContain("CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_KEY");
    expect(remoteWorker).toContain("CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_CERT");
    expect(remoteWorker).toContain("CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_CA_KEY");
    expect(remoteWorker).toContain("CLOUD_AGENTS_PLATFORM_REMOTE_WORKER_TRUST_DOMAIN");
    expect(remoteWorker).toContain(":/run/cloud-agents/remote-worker-ca:ro");
  });

  it("publishes component capacity limits in Compose and Helm", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const managedAgent = readFileSync("deploy/compose/docker-compose.managed-agent.yml", "utf8");
    const controlPlane = readFileSync(
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "utf8",
    );
    const worker = readFileSync("deploy/helm/cloud-agents/templates/worker.yaml", "utf8");
    for (const deployment of [managedAgent, worker]) {
      expect(deployment).toContain("--runtime-max-sessions");
    }
    expect(managedAgent).toContain("CLOUD_AGENTS_RUNTIME_MAX_SESSIONS:-4");
    expect(worker).toContain(".Values.runtime.maxSessions");
    for (const deployment of [compose, controlPlane]) {
      expect(deployment).toContain("--max-concurrent-requests");
    }
    expect(compose).toContain("CLOUD_AGENTS_CONTROL_PLANE_MAX_CONCURRENT_REQUESTS:-128");
    expect(controlPlane).toContain(".Values.controlPlane.maxConcurrentRequests");
    expect(compose).toContain("CLOUD_AGENTS_PLATFORM_ACCESS_GRANT_KEY_FILE");
    expect(compose).toContain("--owner=65532");
    expect(compose).toContain("cloud-agents-control-plane-secrets:/run/cloud-agents/secrets:ro");
  });

  it("packages the non-root Access Gateway without Docker authority", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const dockerfile = readFileSync("deploy/docker/access-gateway.Dockerfile", "utf8");
    expect(dockerfile).toContain("gcr.io/distroless/static-debian12:nonroot");
    expect(dockerfile).toContain("USER 65532:65532");
    expect(compose).toContain("cloud-agents-access-gateway-secrets:/run/cloud-agents/secrets:ro");
    expect(compose).toContain("read_only: true\n    cap_drop: [ALL]");
    expect(compose).toContain('security_opt: ["no-new-privileges:true"]');
    expect(compose).not.toContain("/var/run/docker.sock");
  });

  it("packages isolated non-root User and Admin Web origins", () => {
    const compose = readFileSync("deploy/compose/docker-compose.yml", "utf8");
    const adminDockerfile = readFileSync("deploy/docker/admin-web.Dockerfile", "utf8");
    const userDockerfile = readFileSync("deploy/docker/user-web.Dockerfile", "utf8");
    const server = readFileSync("deploy/web/server.mjs", "utf8");
    expect(adminDockerfile).toContain("CLOUD_AGENTS_WEB_SCOPE=admin");
    expect(userDockerfile).toContain("CLOUD_AGENTS_WEB_SCOPE=user");
    expect(adminDockerfile).toContain("USER 1000:1000");
    expect(userDockerfile).toContain("USER 1000:1000");
    expect(compose).toContain("CLOUD_AGENTS_ADMIN_WEB_UPSTREAM: https://control-plane:8080");
    expect(compose).toContain("CLOUD_AGENTS_WEB_UPSTREAM: https://control-plane:8080");
    expect(compose).toContain("CLOUD_AGENTS_CONTROL_PLANE_CA");
    expect(server).toContain("/^\\/v1\\/admin(?:\\/|$)/u");
    expect(server).toContain('scope === "user"');
    expect(compose).not.toContain("/var/run/docker.sock");
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
      "access-gateway": "USER 65532:65532",
      "admin-web": "USER 1000:1000",
      "control-plane": "USER 65532:65532",
      worker: "USER 1000:1000",
      migrate: "USER 999:999",
      "user-web": "USER 1000:1000",
    } as const;
    for (const name of [
      "access-gateway",
      "admin-web",
      "control-plane",
      "worker",
      "migrate",
      "user-web",
    ] as const) {
      const dockerfile = readFileSync(`deploy/docker/${name}.Dockerfile`, "utf8");
      if (name !== "admin-web" && name !== "user-web") {
        expect(dockerfile).toContain("ARG TARGETOS");
        expect(dockerfile).toContain("ARG TARGETARCH");
        expect(dockerfile).toContain("${TARGETOS}-${TARGETARCH}");
        expect(dockerfile).not.toContain("ARG TARGET=");
      }
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
    const managedAgent = readFileSync("deploy/compose/docker-compose.managed-agent.yml", "utf8");
    expect(compose.match(/platform: \$\{CLOUD_AGENTS_PLATFORM:-linux\/amd64\}/gu)).toHaveLength(5);
    expect(
      managedAgent.match(/platform: \$\{CLOUD_AGENTS_PLATFORM:-linux\/amd64\}/gu),
    ).toHaveLength(1);
    expect(compose).not.toContain("CLOUD_AGENTS_TARGET");
    expect(compose).toContain("CLOUD_AGENTS_PLATFORM_KUBERNETES_CREDENTIALS_DIRECTORY");
    expect(compose).toContain("CLOUD_AGENTS_PLATFORM_SSH_CREDENTIALS_DIRECTORY");
    const controlPlaneTemplate = readFileSync(
      "deploy/helm/cloud-agents/templates/control-plane.yaml",
      "utf8",
    );
    expect(controlPlaneTemplate).toContain("kubernetesCredentialSecretName");
    expect(controlPlaneTemplate).toContain("sshCredentialSecretName");
    const deployment = readDeterministicUstar(buildPlatformDeploymentPackage(process.cwd()));
    const migrateDockerfile = new TextDecoder().decode(
      deployment.find(({ path }) => path === "deploy/docker/migrate.Dockerfile")!.data,
    );
    expect(migrateDockerfile).toContain(
      `cloud-agents-migrations-${PLATFORM_RELEASE_MIGRATION_HEAD}.tar`,
    );
    expect(migrateDockerfile).toContain(`product/${PLATFORM_RELEASE_MIGRATION_HEAD}/manifest.json`);
    expect(migrateDockerfile).not.toContain("@PLATFORM_MIGRATION_");
    const workerDockerfile = readFileSync("deploy/docker/worker.Dockerfile", "utf8");
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-landlock-run-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-landlock-run",
    );
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-landlock-notices.txt /usr/share/doc/cloud-agents/landlock-notices.txt",
    );
    expect(workerDockerfile).toContain("@openai/codex@0.154.0");
    expect(workerDockerfile).toContain(
      '"@anthropic-ai/claude-agent-sdk-linux-${claude_arch}@0.3.207"',
    );
    expect(workerDockerfile).toContain('test "$(claude --version)" = "2.1.207 (Claude Code)"');
    expect(workerDockerfile).toContain("@deepseek-ai/dsh@0.1.2-rc.1");
    expect(workerDockerfile).toContain("@deepseek-ai/dsh-llm-pi-ai@0.1.2-rc.1");
    expect(workerDockerfile).toContain('test "$(dsh --version)" = "0.1.2-rc.1"');
    expect(workerDockerfile).toContain("CLOUD_AGENT_DEEPSEEK_HARNESS_BIN=/usr/local/bin/dsh");
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

  it("keeps interrupted Compose acceptance non-successful after cleanup", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const traps = composeSmoke.match(/^trap .+$/gmu)?.join("\n");
    expect(traps).toBeDefined();
    for (const [signal, expectedStatus] of [
      ["HUP", 129],
      ["INT", 130],
      ["TERM", 143],
    ] as const) {
      const result = spawnSync(
        "sh",
        [
          "-c",
          `cleanup() { rc=$?; trap - 0 HUP INT TERM; printf 'cleanup=%s\\n' "$rc"; exit "$rc"; }\n${traps}\nkill -${signal} $$`,
        ],
        { encoding: "utf8" },
      );
      expect(result.status).toBe(expectedStatus);
      expect(result.stdout).toBe(`cleanup=${expectedStatus}\n`);
    }
  });

  it("verifies candidate bytes before Compose extraction or CLI use", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const verifier =
      'node "$script_directory/lib/platform-release-verifier.ts" "$candidate_directory"';
    expect(composeSmoke).toContain(verifier);
    expect(composeSmoke.indexOf(verifier)).toBeLessThan(
      composeSmoke.indexOf('cli="$candidate_directory'),
    );
    expect(composeSmoke.indexOf(verifier)).toBeLessThan(composeSmoke.indexOf('tar -xf "$1"'));
    expect(composeSmoke).toContain(
      'docker cp "$worker_attestation_container:/usr/share/doc/cloud-agents/landlock-notices.txt" "$smoke_directory/landlock-notices-attested"',
    );
    expect(composeSmoke).toContain(
      'cmp -s "$candidate_directory/cloud-agents-landlock-notices.txt" "$smoke_directory/landlock-notices-attested"',
    );
  });

  it("cleans only OpenSandbox resources recorded by this Compose run", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain(
      'opensandbox_runtime_ids_file="$smoke_directory/opensandbox-runtime-ids"',
    );
    expect(composeSmoke).toContain('if [ ! -e "$opensandbox_runtime_ids_file" ]; then');
    expect(composeSmoke).toContain(
      "No OpenSandbox runtime IDs were recorded; no runtime cleanup required",
    );
    expect(composeSmoke).toContain("label=opensandbox.io/id=$runtime_id");
    expect(composeSmoke).toContain("label=opensandbox.io/egress-sidecar-for=$runtime_id");
    expect(composeSmoke).toContain('index .Labels "opensandbox.io/volume-managed-by"');
    expect(composeSmoke).toContain('if [ "$volume_owner_label" = server ]; then');
    expect(composeSmoke).not.toContain(
      "for container in $(docker ps -aq --filter label=opensandbox.io/id); do",
    );
    expect(composeSmoke).not.toContain(
      "for volume in $(docker volume ls -q --filter label=opensandbox.io/volume-managed-by=server); do",
    );
  });

  it("checks all Kubernetes prerequisites before creating Compose resources", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const definition = source.match(
      /verify_kubernetes_runtime_prerequisites\(\) \{\n[\s\S]*?\n\}/u,
    )?.[0];
    expect(definition).toBeDefined();
    expect(source.indexOf("  verify_kubernetes_runtime_prerequisites\n")).toBeLessThan(
      source.indexOf("docker_host=$("),
    );
    for (const missing of [
      "",
      "crd/batchsandboxes.sandbox.opensandbox.io",
      "crd/pools.sandbox.opensandbox.io",
      "crd/sandboxsnapshots.sandbox.opensandbox.io",
      "clusterrole/opensandbox-manager-role",
    ]) {
      for (const version of ["0.2.0", "0.1.0", ""]) {
        const result = spawnSync(
          "sh",
          [
            "-c",
            `set -eu
kubernetes_ctl() {
  [ "$2" != "$MISSING" ] || return 1
  printf '%s' "$VERSION"
}
${definition}
verify_kubernetes_runtime_prerequisites
printf 'ready\\n'
`,
          ],
          { encoding: "utf8", env: { ...process.env, MISSING: missing, VERSION: version } },
        );
        const ready = !missing && version === "0.2.0";
        expect(result.status).toBe(ready ? 0 : 2);
        expect(result.stdout).toBe(ready ? "ready\n" : "");
        if (!ready) expect(result.stderr).toContain("OpenSandbox 0.2.0 prerequisite:");
      }
    }
  });

  it("keeps the Worker upgrade Operation assertion fail-closed with returned state diagnostics", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    expect(source).toContain('NO_PROXY="$kubernetes_no_proxy" no_proxy="$kubernetes_no_proxy"');
    expect(source).toContain("kubernetes_ctl get --raw /version >/dev/null");
    expect(source).toContain("Kubernetes Runtime smoke requires digest-pinned OpenSandbox images");
    const start = source.indexOf(
      "CLOUD_AGENTS_COMPOSE_OPERATION_FILE=\"$upgrade_operation_file\" node <<'NODE'",
    );
    const script = source.slice(start).match(/node <<'NODE'\n([\s\S]*?)\nNODE/u)?.[1];
    expect(script).toBeDefined();
    const directory = mkdtempSync(join(tmpdir(), "cloud-agents-upgrade-operation-"));
    try {
      const file = join(directory, "operation.json");
      const operation = {
        kind: "MaintenanceOperation",
        action: "target.upgrade",
        resourceId: "docker-compose-target",
        resourceGeneration: 1,
        state: "succeeded",
        currentStep: "complete",
      };
      writeFileSync(file, JSON.stringify(operation));
      const success = spawnSync(process.execPath, ["-e", script!], {
        encoding: "utf8",
        env: { ...process.env, CLOUD_AGENTS_COMPOSE_OPERATION_FILE: file },
      });
      expect(success.status).toBe(0);

      writeFileSync(
        file,
        JSON.stringify({ ...operation, state: "failed", currentStep: "cleanup" }),
      );
      const failure = spawnSync(process.execPath, ["-e", script!], {
        encoding: "utf8",
        env: { ...process.env, CLOUD_AGENTS_COMPOSE_OPERATION_FILE: file },
      });
      expect(failure.status).not.toBe(0);
      expect(failure.stderr).toContain('"state":"failed"');
      expect(failure.stderr).toContain('"currentStep":"cleanup"');
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it("keeps restored snapshot semantic digest independent from raw tar bytes", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain("portable-snapshot-digest.mjs");
    expect(composeSmoke).toContain("tar -cf - -C /workspace .");
    expect(composeSmoke).toContain("capture_kubernetes_restored_snapshot_digest");
    expect(composeSmoke).toContain(
      'kubernetes_ctl -n "$kubernetes_destination_namespace" exec "$kubernetes_recovery_bound_pod" -c binder',
    );
    expect(composeSmoke).not.toContain("node --input-type=module -e");
    expect(composeSmoke).not.toContain(
      "recovery_restored_content_digest_sha256=${recovery_snapshot_digest#sha256:}",
    );
    const directory = mkdtempSync(join(tmpdir(), "cloud-agents-snapshot-digest-"));
    try {
      const file = join(directory, "file");
      const archive = join(directory, "snapshot.tar");
      writeFileSync(file, "hello", { mode: 0o600 });
      expect(
        spawnSync("tar", ["-cf", archive, "-C", directory, "file"], { encoding: "utf8" }).status,
      ).toBe(0);
      const result = spawnSync(
        process.execPath,
        ["scripts/lib/portable-snapshot-digest.mjs", archive],
        {
          encoding: "utf8",
        },
      );
      expect(result.status).toBe(0);
      const [semantic, raw] = result.stdout.trim().split(/\s+/);
      const fileDigest = "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824";
      const expected = `sha256:${createHash("sha256")
        .update(
          JSON.stringify([
            {
              Name: "file",
              Link: "",
              Digest: fileDigest,
              Type: 48,
              Mode: 384,
              Size: 5,
            },
          ]),
        )
        .digest("hex")}`;
      expect(semantic).toBe(expected);
      expect(raw).toBe(createHash("sha256").update(readFileSync(archive)).digest("hex"));
      expect(raw).not.toBe(semantic!.slice("sha256:".length));
      const helper = readFileSync("scripts/lib/portable-snapshot-digest.mjs", "utf8");
      expect(helper).toContain("type === 76");
      expect(helper).toContain("type === 75");
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  it.each(["approval", "recovery"])(
    "keeps %s failure diagnostics free of error text and tool content",
    (kind) => {
      const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
      const start = source.indexOf(
        kind === "approval"
          ? '  if [ "$approval_status" -ne 0 ]; then'
          : '    if [ -f "$recovery_execute_status" ]; then',
      );
      if (kind === "recovery") {
        expect(source.slice(start, source.indexOf("node <<'NODE'", start))).toContain(
          '--request-id "$recovery_prefix-failure" execution get',
        );
        expect(source.slice(start, source.indexOf("node <<'NODE'", start))).toContain(
          'CLOUD_AGENTS_COMPOSE_EXECUTION_FILE="$recovery_checkpoint_file"',
        );
      }
      const script = source.slice(start).match(/node <<'NODE'\n([\s\S]*?)\nNODE/u)?.[1];
      expect(script).toBeDefined();
      const directory = mkdtempSync(join(tmpdir(), "cloud-agents-capability-diagnostics-"));
      try {
        const file = join(directory, "execution.json");
        const secret = "PRIVATE-CONTENT-MUST-NOT-BE-PRINTED";
        writeFileSync(
          file,
          JSON.stringify({
            spec: {
              state: "failed",
              errorCode: "runtime_open_failed",
              errorMessage: `EACCES: ${secret}`,
            },
            messages: [
              {
                messageType: "Error",
                error: { code: "internal_error", message: secret },
                payload: { content: secret },
              },
            ],
          }),
        );
        const result = spawnSync(process.execPath, ["-e", script!], {
          encoding: "utf8",
          env: { ...process.env, CLOUD_AGENTS_COMPOSE_EXECUTION_FILE: file },
        });
        expect(result.status).toBe(0);
        expect(result.stderr).not.toContain(secret);
        const summary = JSON.parse(result.stderr)[
          kind === "approval" ? "capabilityExecutionFailure" : "capabilityRecoveryFailure"
        ];
        expect(summary.errorCode).toBe("runtime_open_failed");
        if (kind === "approval") expect(summary.errorHints.permissionDenied).toBe(true);
        expect(summary.errorDigests).toHaveLength(kind === "approval" ? 2 : 1);
        expect(summary.messages).toEqual([{ type: "error", errorCode: "internal_error" }]);
      } finally {
        rmSync(directory, { recursive: true, force: true });
      }
    },
  );

  it("reports capability acceptance only after artifact and event checks", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const artifactCheck = source.indexOf(
      "  artifact_index=$(",
      source.indexOf("run_real_provider_turn()"),
    );
    const eventCheck = source.indexOf("  assert_real_event_stream_resume", artifactCheck);
    const verdict = source.indexOf("capability_acceptance=passed", eventCheck);
    expect(artifactCheck).toBeGreaterThan(0);
    expect(eventCheck).toBeGreaterThan(artifactCheck);
    expect(verdict).toBeGreaterThan(eventCheck);
  });

  it("runs capability-bound recovery for supported Providers and parameterized negative checks", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const interactions = readFileSync("scripts/test-platform-agent-interactions.sh", "utf8");
    expect(source).toContain("recovery_provider=$1");
    expect(source).toContain('CLOUD_AGENTS_E2E_RECOVERY_PROVIDER="$recovery_provider"');
    expect(source).toContain("for recovery_provider in $real_provider_kinds; do");
    expect(source).toContain('  [ "$capability_bound_recovery" -eq 1 ] || return 0');
    expect(source).toContain(
      'CLOUD_AGENTS_E2E_RECOVERY_CHECKPOINT_MODE="$recovery_checkpoint_mode"',
    );
    expect(source).toContain('run_capability_process_recovery "$recovery_provider"');
    expect(source).toContain(
      'if [ "$capability_acceptance" -eq 1 ] && [ "$capability_bound_recovery" -eq 1 ] && capability_provider_enabled "$provider_kind" &&',
    );
    expect(source).toContain("for negative_provider in $real_provider_kinds; do");
    expect(source).toContain('session create --provider "$negative_provider"');
    expect(source).toContain('"providerKind":"%s"');
    expect(source).toContain(
      'if [ "$cross_node_recovery" -eq 1 ] && [ -n "$real_provider_credentials_directory" ]; then',
    );
    expect(source).toContain(
      'elif [ "$capability_bound_recovery" -eq 1 ] && [ "$capability_bound_recovery_environment" != kubernetes ] && [ -n "$real_provider_credentials_directory" ] &&\n    ! capability_bound_recovery_completed docker "$cross_node_provider_to_run"; then',
    );
    expect(source).toContain(
      "CLOUD_AGENTS_COMPOSE_CAPABILITY_PROCESS_RECOVERY_FAULTS must be worker, agent or both",
    );
    expect(source).not.toContain("CLOUD_AGENTS_E2E_RECOVERY_PROVIDER=claudeAgent");
    const workerRecovery = interactions.slice(
      interactions.indexOf("run_worker_exit_recovery()"),
      interactions.indexOf("run_agent_exit_recovery()"),
    );
    const agentRecovery = interactions.slice(interactions.indexOf("run_agent_exit_recovery()"));
    const checkpointWaiter = interactions.slice(
      interactions.indexOf("wait_for_recovery_side_effect_checkpoint()"),
      interactions.indexOf("recovery_artifact_digest()"),
    );
    expect(workerRecovery).toContain("prepare_interaction_takeover");
    expect(workerRecovery).toContain("reconcile_interaction_takeover");
    expect(workerRecovery).toContain("restart_mcp_fixture_after_worker");
    expect(workerRecovery).toContain(
      'assert_interaction_takeover "$final_file" same-node-reconnect',
    );
    expect(source).toContain('CLOUD_AGENTS_E2E_MCP_FIXTURE_CONTAINER="$mcp_fixture_container"');
    expect(source).toContain('start_capability_mcp_fixture "$foundation_agent_runtime_id"');
    expect(agentRecovery).toContain(
      "assert_interaction_takeover \"$final_file\" 'process-restart|same-node-reconnect'",
    );
    expect(interactions).toContain(
      '["skill", "Skill"].includes(message.payload?.payload?.data?.sourceItemType)',
    );
    expect(interactions).toContain(
      "message.payload?.payload?.data?.capabilityResourceId === expectedSkill.bundleId",
    );
    expect(interactions).toContain('payload?.itemType === "command_execution"');
    expect(interactions).toContain(
      '["succeeded", "failed", "cancelled"].includes(value.spec?.state)',
    );
    expect(interactions).toContain('if [ "$checkpoint_status" -eq 2 ]; then');
    expect(checkpointWaiter).toContain("completedMcp: completedCapability(expectedMcp)");
    expect(checkpointWaiter).toContain('observedSkill: observedCapability(expectedSkill, "skill")');
    expect(checkpointWaiter).toContain(
      "pendingSideEffect: value.spec?.checkpoint?.pendingSideEffect === true",
    );
    expect(checkpointWaiter).toContain('payload?.itemType === "command_execution"');
    expect(checkpointWaiter).toContain(
      'String(payload?.data?.sourceItemType ?? "").toLowerCase() === "bash"',
    );
    expect(checkpointWaiter).toContain("JSON.stringify({gates, outline})");
    expect(checkpointWaiter).not.toContain("JSON.stringify(value)");
  });

  it("retains capability-bound refs across cross-node recovery and recreates the fixture on the destination", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const recovery = source.slice(source.indexOf("run_real_provider_recovery()"));
    expect(recovery).not.toContain('"$recovery_cross_node" -eq 0');
    expect(recovery).toContain('start_capability_mcp_fixture "$kubernetes_agent_runtime_id"');
    expect(recovery).toContain('start_capability_mcp_fixture "$remote_agent_runtime_id"');
    expect(recovery).toContain('start_capability_mcp_fixture "$foundation_agent_runtime_id"');
    expect(source).toContain('value.spec?.runtimeId??""');
    expect(source).toContain('kubernetes_ctl -n "$kubernetes_active_namespace" get pods');
  });

  it("gates one real MCP transport disconnect and reconnect cell without replay", () => {
    const source = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const fixture = readFileSync("scripts/fixtures/managed-capability-mcp-server.mjs", "utf8");
    expect(source).toContain(
      "capability_transport_recovery=${CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY:-0}",
    );
    expect(source).toContain(
      "capability_transport_recovery_environment=${CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY_ENVIRONMENT-}",
    );
    expect(source).toContain(
      "CLOUD_AGENTS_COMPOSE_CAPABILITY_TRANSPORT_RECOVERY_ENVIRONMENT must be docker, remote-worker or kubernetes",
    );
    expect(source).toContain(
      "capability transport recovery selector did not match an enabled real Provider environment",
    );
    expect(source).toContain(
      "MCP_ACCEPTANCE_DISCONNECT_ARM=/tmp/cloud-agents-mcp-side-effect/disconnect-arm",
    );
    expect(source).toContain("-e MCP_ACCEPTANCE_DISCONNECT_ARM=/side-effect/disconnect-arm");
    expect(source).toContain('mcp_fixture_docker exec "$mcp_fixture_container" sh -c');
    expect(source).toContain(
      ": > /side-effect/disconnect-arm; test -f /side-effect/disconnect-arm",
    );
    expect(source).toContain("wait_capability_mcp_disconnect_commit()");
    expect(fixture).toContain("process.exit(137)");
    expect(source).toContain("assert_capability_mcp_listener_from_runtime_netns()");
    expect(source).toContain(
      'start_capability_mcp_fixture "$mcp_fixture_runtime_id" "$mcp_fixture_docker_host" 1',
    );

    const transport = source.slice(
      source.indexOf("run_capability_transport_recovery()"),
      source.indexOf("\nrun_real_provider_turn()"),
    );
    expect(transport).toContain('session create --provider "$transport_provider_kind"');
    expect(transport).toContain(
      '--session "$transport_session_id" --turn "$transport_fault_turn_id"',
    );
    expect(transport).toContain(
      '--session "$transport_session_id" --turn "$transport_reconnect_turn_id"',
    );
    expect(transport).toContain("execute_real_provider_with_mcp_approvals");
    expect(transport).not.toContain("execute_real_provider_with_safe_retry");
    expect(transport).toContain('event.spec?.errorCode === "capability_call_unknown"');
    expect(transport).toContain('fault.spec?.state !== "running"');
    expect(transport).toContain('fault.spec?.recoveryState !== "awaiting_reconciliation"');
    expect(transport).toContain('fault.spec?.recoveryReason !== "side_effect_outcome_unknown"');
    expect(transport).toContain("fault.spec?.checkpoint?.pendingSideEffect !== true");
    expect(transport).toContain('Date.parse(value.spec?.claimExpiresAt??"")+1000<Date.now()');
    expect(transport).toContain(
      "capability transport recovery replay bypassed side-effect reconciliation",
    );
    expect(transport).toContain(
      "grep -Fq 'RECOVERY_REQUIRES_RECONCILIATION' \"$transport_blocked_file\"",
    );
    expect(transport).toContain(
      '--idempotency-key "$transport_prefix-disconnect-execution" execution execute',
    );
    expect(transport).toContain('--generation "$transport_reconcile_generation"');
    expect(transport).toContain('--checkpoint-digest "$transport_reconcile_checkpoint_digest"');
    expect(transport).toContain("--outcome confirmed");
    expect(transport).toContain(
      'execution cancel \\\n    --generation "$transport_reconcile_generation"',
    );
    expect(transport).toContain('--request-id "$transport_prefix-disconnect-cancelled"');
    expect(transport).toContain('execution get >"$transport_fault_cancelled"');
    expect(transport).toContain(
      'CLOUD_AGENTS_COMPOSE_CANCELLED_EXECUTION_FILE="$transport_fault_cancelled"',
    );
    expect(transport).toContain('CLOUD_AGENTS_COMPOSE_FAULT_EVENTS_FILE="$transport_fault_events"');
    expect(transport).toContain('cancelled.spec?.state !== "cancelled"');
    expect(transport).toContain('cancelled.spec?.recoveryState !== "none"');
    expect(transport).toContain("cancelled.spec?.recoveryReason !== undefined");
    expect(transport).toContain("cancelled.spec?.checkpoint?.pendingSideEffect !== false");
    expect(transport).toContain('event.spec?.operation === "execution.reconcile"');
    expect(transport).toContain('event.spec?.operation === "turn.cancel"');
    expect(transport).toContain("faultPage.hasMore !== false || page.hasMore !== false");
    expect(transport).toContain("event.spec?.executionId === fault.metadata?.uid");
    expect(transport).toContain("event.spec?.turnId === fault.metadata?.turnId");
    expect(transport).toContain("event.spec?.generation === fault.spec?.generation");
    expect(transport).toContain("event.spec?.executionId === reconnect.metadata?.uid");
    expect(transport).toContain("event.spec?.turnId === reconnect.metadata?.turnId");
    expect(transport).toContain("event.spec?.generation === reconnect.spec?.generation");
    expect(transport.match(/event\.spec\?\.serverId === mcpId/gu)).toHaveLength(2);
    expect(
      transport.match(
        /event\.spec\?\.version === process\.env\.CLOUD_AGENTS_COMPOSE_MCP_VERSION/gu,
      ),
    ).toHaveLength(2);
    expect(
      transport.match(/event\.spec\?\.digest === process\.env\.CLOUD_AGENTS_COMPOSE_MCP_DIGEST/gu),
    ).toHaveLength(2);
    expect(transport).toContain('event.spec?.result === "failed"');
    expect(transport).toContain("event.spec?.errorCode === undefined");
    expect(transport.match(/event\.spec\?\.resource === "McpServer"/gu)).toHaveLength(2);
    expect(transport).toContain('event.spec.changes[0]?.to === "failed"');
    expect(transport).toContain('event.spec.changes[0]?.to === "succeeded"');
    expect(transport).toContain("event.spec?.executionId === cancelled.metadata?.uid");
    expect(transport).toContain("event.spec?.turnId === cancelled.metadata?.turnId");
    expect(transport).toContain("event.spec?.generation === cancelled.spec?.generation");
    expect(transport).toContain('event.spec?.resource === "Execution"');
    expect(transport).toContain(
      'hasChange(event, "Execution", "recovery:awaiting_reconciliation", "recovery:none")',
    );
    expect(transport).toContain('hasChange(event, "Turn", "running", "cancelled")');
    expect(transport).toContain('hasChange(event, "Execution", "running", "cancelled")');
    expect(transport).toContain(
      "BigInt(reconciled.metadata.sequence) >= BigInt(cancelledEvent.metadata.sequence)",
    );
    expect(transport).toContain(
      "BigInt(faultMcp.metadata.sequence) >= BigInt(reconciled.metadata.sequence)",
    );
    expect(transport).toContain(
      "BigInt(cancelledEvent.metadata.sequence) >= BigInt(reconnectMcp.metadata.sequence)",
    );
    expect(transport).toContain("inspectKeys(faultPage)");
    expect(transport).toContain(
      'test "$(capability_mcp_tool_call_count)" -eq "$transport_requests_after_fault"',
    );
    expect(transport).toContain(
      'test "$(wc -l <"$mcp_fixture_side_effect_file" 2>/dev/null || printf \'0\')" -eq "$transport_side_effects_after_fault"',
    );
    expect(transport).toContain('event.spec?.result === "succeeded"');
    expect(transport).toContain("transport_side_effects_before + 1");
    expect(transport).toContain("transport_requests_before + 1");
    expect(transport).toContain("transport_requests_after_fault + 1");
    expect(transport).toContain("capability audit exposed sensitive transport data");
    expect(transport).not.toMatch(/codex.*kubernetes|kubernetes.*codex/u);

    const providerTurn = source.slice(
      source.indexOf("run_real_provider_turn()"),
      source.indexOf("\nassert_real_event_stream_resume()"),
    );
    expect(
      providerTurn.indexOf('run_capability_transport_recovery "$provider_kind" "$provider_slug"'),
    ).toBeGreaterThan(0);
    expect(providerTurn.indexOf("capability_revoke_here=0")).toBeGreaterThan(
      providerTurn.indexOf('run_capability_transport_recovery "$provider_kind" "$provider_slug"'),
    );
  });

  it("keeps Codex and Claude capability prompts bound to the complete artifact instruction", () => {
    const composeSmoke = readFileSync("scripts/test-platform-compose.sh", "utf8");
    const agentInteractions = readFileSync("scripts/test-platform-agent-interactions.sh", "utf8");

    expect(composeSmoke).toContain(
      'artifact_path=".cloud-agents-stage3-acceptance-$real_provider_environment_slug-target-real-$provider_slug.txt"',
    );
    expect(composeSmoke).toContain(
      'artifact_prompt_path="/workspace/.cloud-agents/managed-agent/tenants/tenant-compose-smoke/projects/$project_id/sessions/$session_id/workspace/$artifact_path"',
    );
    expect(composeSmoke).toContain(
      "artifact_requirement=\"exactly one file at $artifact_prompt_path. Its complete contents must be the single ASCII line '$expected_content' followed by a newline\"",
    );
    expect(composeSmoke).toContain(
      "followup_expected_command=\"/bin/bash -lc 'cat -- $artifact_path'\"",
    );
    expect(composeSmoke).toContain(
      'followup_prompt="Run exactly this command and no other tool: cat -- $artifact_path. Reply with the command\'s exact single line. Do not modify any file."',
    );
    expect(composeSmoke).toContain(
      'followup_prompt="Read $artifact_prompt_path and reply with its exact single line. Do not modify any file."',
    );
    expect(
      composeSmoke.match(/artifact_instruction="\$file_tool \$artifact_requirement"/gu),
    ).toHaveLength(2);
    expect(composeSmoke).toContain(
      'codex) file_tool="You must use the workspace.write_text_file tool, never a shell command or another tool, to create"',
    );
    expect(composeSmoke).toContain(
      'claudeAgent)\n      file_tool="You must use the Write tool, never a shell command or another tool, to create"',
    );
    expect(composeSmoke).toContain(
      'mcp_instruction="Call the managed MCP tool named acceptance_side_effect exactly once and wait for it to succeed."',
    );
    expect(composeSmoke).toContain(
      'claudeAgent) skill_instruction="use the managed-capability-acceptance:managed-capability-acceptance Skill"',
    );
    expect(composeSmoke).toContain(
      'prompt="Call the managed MCP tool named $recovery_mcp_tool_name exactly once, then use the $recovery_skill_name Skill',
    );
    expect(agentInteractions).toContain("codex) recovery_skill_name=managed-capability-acceptance");
    expect(agentInteractions).toContain(
      "claudeAgent) recovery_skill_name=managed-capability-acceptance:managed-capability-acceptance",
    );
    expect(composeSmoke).toContain(
      'const isSkillApproval = message.payload?.toolName === "Skill" && message.payload?.requestKind === "tool";',
    );
    expect(composeSmoke).toContain(
      'const isArtifactWriteApproval = message.payload?.toolName === "Write";',
    );
    expect(composeSmoke).toContain(
      "(isMcpApproval || isSkillApproval || isArtifactWriteApproval || isExpectedCodexCommand)",
    );
    expect(composeSmoke).toContain(
      'message.payload?.requestKind === "command" && message.payload?.command === expectedCommand',
    );
    expect(composeSmoke).toContain(
      'prompt="$mcp_instruction Then $skill_instruction. Follow it to $artifact_instruction.',
    );
    expect(composeSmoke).toContain(
      "capability_bound_recovery_provider=${CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER:-claudeAgent}",
    );
    expect(composeSmoke).toContain("codex | claudeAgent | pi | deepseek-harness) ;;");
    expect(composeSmoke).toContain('if [ "$recovery_provider_kind" = "claudeAgent" ]; then');
    expect(composeSmoke).toContain(
      "recovery_skill_name=managed-capability-acceptance:managed-capability-acceptance",
    );
    expect(composeSmoke).toContain("recovery_runtime_mode=approval-required");
    expect(composeSmoke).toContain(
      "The Codex Host-managed workspace.write_text_file tool is the only available file tool",
    );
    expect(composeSmoke).toContain(
      'process.env.CLOUD_AGENTS_COMPOSE_CAPABILITY_BOUND_RECOVERY_PROVIDER === "codex" ? "30000" : "12000"',
    );
    expect(composeSmoke).toContain("mcp__mcp-compose-acce__acceptance_marker__a63aebad");
    expect(composeSmoke).not.toContain("Follow it to $file_tool.");
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
