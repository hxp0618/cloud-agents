import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { gunzipSync } from "node:zlib";
import { describe, expect, it, onTestFinished } from "vitest";

import { readDeterministicUstar } from "../../scripts/lib/platform-migration-ustar";
import {
  expectedArtifactIdentities,
  buildPlatformContractPackage,
  buildPlatformGoSDKPackage,
  buildPlatformTypeScriptSDKPackage,
  buildPlatformMigrationPackage,
  buildPlatformDeploymentPackage,
  buildRuntimeNotice,
  captureRuntimeReleaseCandidate,
  parsePlatformReleaseOptions,
  platformReleaseArtifactFilename,
  platformReleaseArtifact,
  PLATFORM_RELEASE_CLI_TARGETS,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_MIGRATION_HEAD,
  PLATFORM_RELEASE_TARGETS,
  CLOUD_AGENT_RUNTIME_NOTICES_FILENAME,
  CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH,
  validatePlatformReleaseDirectory,
  validatePlatformReleaseManifest,
} from "../../scripts/lib/platform-release";
import {
  cloudAgentCandidateDigest,
  CLOUD_AGENT_PUBLIC_PACKAGES,
  type PackedCloudAgentPackage,
} from "../../scripts/lib/cloud-agent-release";
import {
  buildWorkerOciSupplyArtifacts,
  WORKER_OCI_INSTALL_MANIFEST_ARCHIVE_PATH,
  WORKER_OCI_INSTALL_MANIFEST_FILENAME,
  WORKER_OCI_NOTICES_ARCHIVE_PATH,
  WORKER_OCI_NOTICES_FILENAME,
} from "../../scripts/lib/worker-oci-supply";

describe("platform release", () => {
  it("binds the exact Runtime notice bytes and rejects missing or empty source", () => {
    const root = temporaryDirectory("cloud-agents-runtime-notice-");

    expect(() => buildRuntimeNotice(root)).toThrow();
    mkdirSync(join(root, "packages/cloud-agent-runtime"), { recursive: true });
    const path = join(root, CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH);
    writeFileSync(path, "");
    expect(() => buildRuntimeNotice(root)).toThrow(/empty/i);
    const bytes = Buffer.from("A third-party notice\nwith retained bytes.\r\n");
    writeFileSync(path, bytes);
    const notice = buildRuntimeNotice(root);
    expect(notice.bytes).toEqual(bytes);
    expect(notice.artifact).toEqual({
      name: "cloud-agent-runtime-notices",
      target: "portable",
      filename: CLOUD_AGENT_RUNTIME_NOTICES_FILENAME,
      sizeBytes: bytes.length,
      sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    });
  });

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
      runtimeCandidateDirectory: undefined,
    });
    expect(() =>
      parsePlatformReleaseOptions([
        "--version",
        "0.1.0-rc.1",
        "--output-dir",
        "out",
        "--runtime-candidate-dir",
      ]),
    ).toThrow(/--runtime-candidate-dir requires a value/);
    expect(() =>
      parsePlatformReleaseOptions([
        "--version",
        "0.1.0-rc.1",
        "--output-dir",
        "out",
        "--runtime-candidate-dir",
        "runtime-a",
        "--runtime-candidate-dir",
        "runtime-b",
      ]),
    ).toThrow(/specified more than once/);
  });

  it("captures one qualified Runtime candidate snapshot", () => {
    const fixture = runtimeCandidateFixture();

    const capture = captureRuntimeReleaseCandidate(
      fixture.candidateDirectory,
      fixture.repositoryRoot,
      fixture.sourceCommit,
      true,
    );
    writeFileSync(
      join(fixture.candidateDirectory, "cloud-agent-runtime-standalone.mjs"),
      "changed",
    );
    writeFileSync(
      join(fixture.candidateDirectory, CLOUD_AGENT_RUNTIME_NOTICES_FILENAME),
      "changed",
    );

    expect(capture.runtimeBytes).toEqual(fixture.runtimeBytes);
    expect(capture.noticeBytes).toEqual(fixture.noticeBytes);
    expect(capture.runtimeArtifact.filename).toBe("cloud-agent-runtime-standalone.mjs");
    expect(capture.noticeArtifact.filename).toBe(CLOUD_AGENT_RUNTIME_NOTICES_FILENAME);
    expect(capture.candidateDigest).toBe(fixture.manifest.candidateDigest);
    expect(capture.manifestSha256).toBe(
      `sha256:${createHash("sha256").update(fixture.manifestBytes).digest("hex")}`,
    );
  });

  it.each([
    [
      "source commit",
      (manifest: RuntimeCandidateManifest) => ({ ...manifest, sourceCommit: "b".repeat(40) }),
    ],
    [
      "candidate digest",
      (manifest: RuntimeCandidateManifest) => ({
        ...manifest,
        candidateDigest: `sha256:${"0".repeat(64)}`,
      }),
    ],
    [
      "same-bits qualification",
      (manifest: RuntimeCandidateManifest) => ({ ...manifest, sameBitsVerified: false }),
    ],
    [
      "sourceDirty metadata",
      (manifest: RuntimeCandidateManifest) => ({ ...manifest, sourceDirty: false }),
    ],
    [
      "standalone size",
      (manifest: RuntimeCandidateManifest) => ({
        ...manifest,
        standaloneRuntime: { ...manifest.standaloneRuntime, sizeBytes: 1 },
      }),
    ],
    [
      "standalone filename escape",
      (manifest: RuntimeCandidateManifest) => ({
        ...manifest,
        standaloneRuntime: { ...manifest.standaloneRuntime, filename: "../runtime.mjs" },
      }),
    ],
  ])("rejects Runtime candidate %s drift", (_label, mutate) => {
    const fixture = runtimeCandidateFixture();

    writeFileSync(
      join(fixture.candidateDirectory, "candidate-manifest.json"),
      `${JSON.stringify(mutate(fixture.manifest), null, 2)}\n`,
    );
    expect(() =>
      captureRuntimeReleaseCandidate(
        fixture.candidateDirectory,
        fixture.repositoryRoot,
        fixture.sourceCommit,
        true,
      ),
    ).toThrow();
  });

  it("rejects mutated, mismatched-notice and symlink Runtime candidate files", () => {
    const mutated = runtimeCandidateFixture();
    const wrongNotice = runtimeCandidateFixture();
    const linked = runtimeCandidateFixture();

    writeFileSync(
      join(mutated.candidateDirectory, "cloud-agent-runtime-standalone.mjs"),
      "changed",
    );
    expect(() =>
      captureRuntimeReleaseCandidate(
        mutated.candidateDirectory,
        mutated.repositoryRoot,
        mutated.sourceCommit,
        true,
      ),
    ).toThrow(/integrity/);

    const noticeBytes = Buffer.from("different authority\n");
    const manifest = {
      ...wrongNotice.manifest,
      runtimeNotices: {
        ...wrongNotice.manifest.runtimeNotices,
        sizeBytes: noticeBytes.length,
        sha256: `sha256:${createHash("sha256").update(noticeBytes).digest("hex")}`,
      },
    };
    writeFileSync(
      join(wrongNotice.candidateDirectory, CLOUD_AGENT_RUNTIME_NOTICES_FILENAME),
      noticeBytes,
    );
    writeFileSync(
      join(wrongNotice.candidateDirectory, "candidate-manifest.json"),
      `${JSON.stringify(manifest, null, 2)}\n`,
    );
    expect(() =>
      captureRuntimeReleaseCandidate(
        wrongNotice.candidateDirectory,
        wrongNotice.repositoryRoot,
        wrongNotice.sourceCommit,
        true,
      ),
    ).toThrow(/notice authority/);

    const target = join(linked.root, "linked-runtime.mjs");
    writeFileSync(target, linked.runtimeBytes);
    rmSync(join(linked.candidateDirectory, "cloud-agent-runtime-standalone.mjs"));
    symlinkSync(target, join(linked.candidateDirectory, "cloud-agent-runtime-standalone.mjs"));
    expect(() =>
      captureRuntimeReleaseCandidate(
        linked.candidateDirectory,
        linked.repositoryRoot,
        linked.sourceCommit,
        true,
      ),
    ).toThrow(/regular file/);
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
    const directory = temporaryDirectory("cloud-agents-platform-release-");

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
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-worker-oci-install-manifest",
      target: "portable",
    });
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-worker-oci-notices",
      target: "portable",
    });
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agent-runtime-notices",
      target: "portable",
    });
    expect(
      platformReleaseArtifactFilename(
        "cloud-agent-runtime-notices",
        "portable",
        PLATFORM_RELEASE_MIGRATION_HEAD,
      ),
    ).toBe(CLOUD_AGENT_RUNTIME_NOTICES_FILENAME);
    expect(buildRuntimeNotice(process.cwd()).bytes).toEqual(
      readFileSync(CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH),
    );
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agents-access-gateway");
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agents-remote-worker");
    expect(PLATFORM_RELEASE_GO_COMMANDS).toContain("cloud-agentsctl");
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
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain(
      '"$real_provider_credentials_directory/tenant-local.$provider.json"',
    );
  });

  it("keeps acceptance model values outside the harness source", () => {
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(composeSmoke).not.toMatch(/\bgpt-[0-9]/);
  });

  it("fails closed before Kubernetes smoke without an explicit context and kubeconfig", () => {
    const env = { ...process.env };
    delete env.CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT;
    delete env.CLOUD_AGENTS_COMPOSE_KUBECONFIG;
    const result = spawnSync("sh", ["test/e2e/test-platform-compose.sh", "/tmp"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...env,
        CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME: "1",
        CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY: "1",
        CLOUD_AGENTS_COMPOSE_CAPABILITY_NEGATIVES: "1",
      },
    });
    expect(result.status).toBe(2);
    expect(result.stderr).toContain(
      "Kubernetes Runtime smoke requires an explicit context, a regular kubeconfig, and either real Provider credentials or capability contract-only mode",
    );
  });

  it("keeps Kubernetes image loading explicit for external runtimes", () => {
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain("CLOUD_AGENTS_COMPOSE_KUBERNETES_IMAGE_LOADER");
    expect(composeSmoke).toContain("must be an absolute executable regular file");
    expect(composeSmoke).toContain(
      '"$kubernetes_image_loader" "$worker_repository" "$worker_release_digest" "$worker_upgrade_release_digest"',
    );
  });

  it("rejects an invalid Kubernetes image loader before runtime commands", () => {
    const directory = temporaryDirectory("cloud-agents-kubernetes-loader-");
    const kubeconfig = join(directory, "kubeconfig");
    writeFileSync(kubeconfig, "apiVersion: v1\n");

    const result = spawnSync("sh", ["test/e2e/test-platform-compose.sh", "/tmp"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_COMPOSE_KUBERNETES_RUNTIME: "1",
        CLOUD_AGENTS_COMPOSE_CAPABILITY_CONTRACT_ONLY: "1",
        CLOUD_AGENTS_COMPOSE_KUBERNETES_CONTEXT: "kind-test",
        CLOUD_AGENTS_COMPOSE_KUBECONFIG: kubeconfig,
        CLOUD_AGENTS_COMPOSE_KUBERNETES_IMAGE_LOADER: "loader.sh",
      },
    });
    expect(result.status).toBe(2);
    expect(result.stderr).toContain(
      "CLOUD_AGENTS_COMPOSE_KUBERNETES_IMAGE_LOADER must be an absolute executable regular file",
    );
  });

  it("rejects a DeepSeek recovery delay outside the Foundation binding range", () => {
    const result = spawnSync("sh", ["test/e2e/test-platform-compose.sh", "/tmp"], {
      cwd: process.cwd(),
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_COMPOSE_CROSS_NODE_RECOVERY: "1",
        CLOUD_AGENTS_COMPOSE_CROSS_NODE_PROVIDER: "deepseek-harness",
        CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS: "30001",
      },
    });
    expect(result.status).toBe(2);
    expect(result.stderr).toContain(
      "DeepSeek Harness recovery requires CLOUD_AGENT_DEEPSEEK_HARNESS_TOOL_DELAY_MS to be an integer from 1 to 30000",
    );
  });

  it("emits the recovery receipt only after the final artifact check and cleanup", () => {
    const directory = temporaryDirectory("cloud-agents-recovery-receipt-");
    const executionFile = join(directory, "execution.json");
    writeFileSync(
      executionFile,
      `${JSON.stringify({
        spec: {
          state: "succeeded",
          attemptNumber: 2,
          recoveryState: "recovered",
          recoveryMode: "cross-node-takeover",
          recoverySourceTargetId: "source-target",
          recoveryTargetId: "destination-target",
          checkpoint: { sequence: 2, digest: "sha256:checkpoint" },
        },
        messages: [{ messageType: "Result" }],
      })}\n`,
    );

    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const recoveryStart = source.indexOf("run_real_provider_recovery() {");
    const tailStart = source.indexOf("  recovery_expected_mode=process-restart", recoveryStart);
    const tailEnd = source.indexOf("\n}\n\nrun_selected_real_provider_recoveries()", tailStart);
    expect(recoveryStart).toBeGreaterThanOrEqual(0);
    expect(tailStart).toBeGreaterThan(recoveryStart);
    expect(tailEnd).toBeGreaterThan(tailStart);
    const recoveryTail = source.slice(tailStart, tailEnd);
    const runRecoveryTail = (observedDigest: string, cleanupStatus: string) =>
      spawnSync(
        "sh",
        [
          "-c",
          `set -eu
recovery_result_file=$HARNESS_EXECUTION_FILE
recovery_provider_kind=codex
recovery_environment_slug=remote-worker
recovery_outcome=confirmed
recovery_source_target=source-target
remote_target_restore_id=destination-target
recovery_capability_bound=0
capability_mcp_id=
capability_skill_id=
cross_recovery_rto_ms=42
recovery_snapshot_size=16
recovery_snapshot_digest=sha256:expected-digest
recovery_artifact_absolute=/workspace/recovery-artifact
expected_recovery_digest=expected-digest
recovery_cross_node=1
run_recovery_sandbox_probe() {
  [ "$1" = final-probe ] || return 1
  printf '{"exitCode":0,"stdout":"%s\\\\n"}' "$HARNESS_OBSERVED_DIGEST"
}
cleanup_codex_recovery_snapshot() {
  return "$HARNESS_CLEANUP_STATUS"
}
${recoveryTail}`,
        ],
        {
          cwd: process.cwd(),
          encoding: "utf8",
          env: {
            ...process.env,
            HARNESS_EXECUTION_FILE: executionFile,
            HARNESS_OBSERVED_DIGEST: observedDigest,
            HARNESS_CLEANUP_STATUS: cleanupStatus,
          },
        },
      );

    const success = runRecoveryTail("expected-digest", "0");
    expect(success.status).toBe(0);
    expect(success.stdout.match(/ANYWHERE_RUNTIME_R4_RECOVERY=/g)).toHaveLength(1);

    for (const failure of [
      runRecoveryTail("different-digest", "0"),
      runRecoveryTail("expected-digest", "1"),
    ]) {
      expect(failure.status).not.toBe(0);
      expect(failure.stdout).not.toContain("ANYWHERE_RUNTIME_R4_RECOVERY=");
    }
  });

  it("uses the unique Compose run identity as the business Project name", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const start = source.indexOf("project_output=$(cloud_agentsctl_user");
    const end = source.indexOf("\n\nremote_target_id=", start);
    expect(start).toBeGreaterThanOrEqual(0);
    expect(end).toBeGreaterThan(start);
    const projectCreation = source.slice(start, end);
    const directory = temporaryDirectory("cloud-agents-project-name-");

    const run = (project: string, nameFile: string) =>
      spawnSync(
        "sh",
        [
          "-c",
          `set -eu
cloud_agentsctl_user() {
  project_name=
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --name ]; then
      shift
      project_name=$1
    fi
    shift
  done
  printf '%s\n' "$project_name" > "$HARNESS_NAME_FILE"
  printf '%s\n' '{"metadata":{"uid":"project-valid"}}'
}
project=$HARNESS_PROJECT
${projectCreation}`,
        ],
        {
          cwd: process.cwd(),
          encoding: "utf8",
          env: { ...process.env, HARNESS_PROJECT: project, HARNESS_NAME_FILE: nameFile },
        },
      );
    const firstNameFile = join(directory, "first-name");
    const secondNameFile = join(directory, "second-name");
    expect(run("cloud-agents-compose-smoke-101", firstNameFile).status).toBe(0);
    expect(run("cloud-agents-compose-smoke-202", secondNameFile).status).toBe(0);
    expect(readFileSync(firstNameFile, "utf8")).toBe("cloud-agents-compose-smoke-101\n");
    expect(readFileSync(secondNameFile, "utf8")).toBe("cloud-agents-compose-smoke-202\n");
  });

  it("ignores foreign OpenSandbox resources while rejecting each owned leak", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const start = source.indexOf("# OpenSandbox egress cleanup is asynchronous");
    const end = source.indexOf("\ndone", start);
    expect(start).toBeGreaterThanOrEqual(0);
    expect(end).toBeGreaterThan(start);
    const ownedCleanupCheck = source.slice(start, end + "\ndone".length);
    const directory = temporaryDirectory("cloud-agents-owned-runtime-");
    for (const baseline of [
      "opensandbox-runtime-baseline",
      "opensandbox-egress-baseline",
      "opensandbox-volume-baseline",
    ]) {
      writeFileSync(join(directory, baseline), "");
    }

    const run = (
      ownedRuntime: string,
      ownedEgress: string,
      ownedVolume: string,
      volumeQueryError = "0",
    ) =>
      spawnSync(
        "sh",
        [
          "-c",
          `set -eu
smoke_directory=$HARNESS_DIRECTORY
foundation_sandbox_runtime_id=owned-runtime
foundation_sandbox_runtime_volume=opensandbox-runtime-owned-runtime
docker() {
  case "$*" in
    "ps -aq --filter label=opensandbox.io/id=owned-runtime")
      [ "$OWNED_RUNTIME" = 0 ] || printf 'owned-runtime-container\n'
      ;;
    "ps -aq --filter label=opensandbox.io/egress-sidecar-for=owned-runtime")
      [ "$OWNED_EGRESS" = 0 ] || printf 'owned-egress-container\n'
      ;;
    "volume inspect opensandbox-runtime-owned-runtime")
      [ "$VOLUME_QUERY_ERROR" = 0 ] || return 125
      [ "$OWNED_VOLUME" = 1 ]
      ;;
    "volume ls -q --filter name=^opensandbox-runtime-owned-runtime$")
      [ "$VOLUME_QUERY_ERROR" = 0 ] || return 125
      [ "$OWNED_VOLUME" = 0 ] || printf 'opensandbox-runtime-owned-runtime\n'
      ;;
    "ps -aq --filter label=opensandbox.io/id") printf 'foreign-runtime-container\n' ;;
    "ps -aq --filter label=opensandbox.io/egress-sidecar-for") printf 'foreign-egress-container\n' ;;
    "volume ls -q --filter label=opensandbox.io/volume-managed-by=server") printf 'foreign-volume\n' ;;
    *) return 1 ;;
  esac
}
sleep() { :; }
${ownedCleanupCheck}`,
        ],
        {
          cwd: process.cwd(),
          encoding: "utf8",
          env: {
            ...process.env,
            HARNESS_DIRECTORY: directory,
            OWNED_RUNTIME: ownedRuntime,
            OWNED_EGRESS: ownedEgress,
            OWNED_VOLUME: ownedVolume,
            VOLUME_QUERY_ERROR: volumeQueryError,
          },
        },
      );

    expect(run("0", "0", "0").status).toBe(0);
    expect(run("0", "0", "0", "1").status).not.toBe(0);
    for (const ownedLeak of [run("1", "0", "0"), run("0", "1", "0"), run("0", "0", "1")]) {
      expect(ownedLeak.status).not.toBe(0);
    }
  });

  it("reports one safe interaction failure phase and cleans up exactly once", () => {
    const source = readFileSync("test/e2e/test-platform-agent-interactions.sh", "utf8");
    const cleanupInitialization = source.indexOf("cleanup_complete=0\ncleanup() {");
    const lifecycleStart =
      cleanupInitialization >= 0 ? cleanupInitialization : source.indexOf("cleanup() {");
    const lifecycleEnd = source.indexOf("\ncreate_turn() {", lifecycleStart);
    expect(lifecycleStart).toBeGreaterThanOrEqual(0);
    expect(lifecycleEnd).toBeGreaterThan(lifecycleStart);
    const lifecycle = source.slice(lifecycleStart, lifecycleEnd);
    const recoveryOnlyStart = source.indexOf(
      'if [ "${CLOUD_AGENTS_E2E_RECOVERY_ONLY:-0}" = 1 ]; then',
    );
    const recoveryOnlyTailStart = source.indexOf("  interaction_phase=cleanup", recoveryOnlyStart);
    const recoveryOnlyTailEnd = source.indexOf("\nfi", recoveryOnlyTailStart);
    expect(recoveryOnlyStart).toBeGreaterThan(lifecycleEnd);
    expect(recoveryOnlyTailStart).toBeGreaterThan(recoveryOnlyStart);
    expect(recoveryOnlyTailEnd).toBeGreaterThan(recoveryOnlyTailStart);
    const recoveryOnlySuccess = source.slice(recoveryOnlyTailStart, recoveryOnlyTailEnd);
    const ordinaryTailStart = source.lastIndexOf("interaction_phase=cleanup");
    expect(ordinaryTailStart).toBeGreaterThan(recoveryOnlyTailEnd);
    const ordinarySuccess = source.slice(ordinaryTailStart);
    const directory = temporaryDirectory("cloud-agents-interaction-phase-");

    const run = (scenario: string, inheritedCleanupComplete = "0") => {
      const countFile = join(directory, `${scenario}.count`);
      writeFileSync(countFile, "0\n");
      const action =
        scenario === "ordinary-success"
          ? ordinarySuccess
          : scenario === "recovery-only-success"
            ? recoveryOnlySuccess
            : `case "$HARNESS_SCENARIO" in
  command-failure) interaction_phase=cancel; false ;;
  explicit-exit) interaction_phase=interrupt; exit 7 ;;
  hup) interaction_phase=agent-recovery; kill -HUP "$$"; printf 'continued\\n' ;;
  int) interaction_phase=agent-recovery; kill -INT "$$"; printf 'continued\\n' ;;
  term) interaction_phase=agent-recovery; kill -TERM "$$"; printf 'continued\\n' ;;
esac`;
      const result = spawnSync(
        "sh",
        [
          "-c",
          `set -eu
execute_pid=
active_sessions=session-one
CLOUD_AGENTS_PROJECT=project-one
recovery_environment=remote-worker
interaction_phase=approval
bounded_request_id() { printf 'bounded'; }
run_ctl() {
  count=$(cat "$HARNESS_COUNT_FILE")
  count=$((count + 1))
  printf '%s\n' "$count" > "$HARNESS_COUNT_FILE"
}
${lifecycle}
${action}`,
        ],
        {
          cwd: process.cwd(),
          encoding: "utf8",
          env: {
            ...process.env,
            HARNESS_COUNT_FILE: countFile,
            HARNESS_SCENARIO: scenario,
            SENSITIVE_VALUE: "must-not-appear",
            cleanup_complete: inheritedCleanupComplete,
          },
        },
      );
      return { result, cleanupCount: readFileSync(countFile, "utf8").trim() };
    };

    for (const [scenario, phase, status] of [
      ["command-failure", "cancel", 1],
      ["explicit-exit", "interrupt", 7],
      ["hup", "agent-recovery", 129],
      ["int", "agent-recovery", 130],
      ["term", "agent-recovery", 143],
    ] as const) {
      const outcome = run(scenario);
      expect(outcome.result.status).toBe(status);
      expect(outcome.cleanupCount).toBe("1");
      expect(outcome.result.stderr).toBe(
        `AGENT_INTERACTIONS_FAILURE phase=${phase} exit_status=${status}\n`,
      );
      expect(outcome.result.stderr).not.toContain("must-not-appear");
      expect(outcome.result.stdout).toBe("");
    }

    const ordinarySuccessOutcome = run("ordinary-success", "1");
    expect(ordinarySuccessOutcome.result.status).toBe(0);
    expect(ordinarySuccessOutcome.result.stdout).toBe(
      "Agent approval and user-input real E2E passed\n",
    );
    expect(ordinarySuccessOutcome.result.stderr).toBe("");
    expect(ordinarySuccessOutcome.cleanupCount).toBe("1");

    const recoveryOnlySuccessOutcome = run("recovery-only-success", "1");
    expect(recoveryOnlySuccessOutcome.result.status).toBe(0);
    expect(recoveryOnlySuccessOutcome.result.stdout).toBe(
      "Agent Worker and Runtime recovery real E2E passed\n",
    );
    expect(recoveryOnlySuccessOutcome.result.stderr).toBe("");
    expect(recoveryOnlySuccessOutcome.cleanupCount).toBe("1");

    for (const [phase, next] of [
      ["approval", "approval_turn="],
      ["user-input", "input_turn="],
      ["long-task", "run_long_task"],
      ["worker-recovery", "run_worker_exit_recovery"],
      ["agent-recovery", "run_agent_exit_recovery"],
      ["cancel", "run_controlled_stop cancel"],
      ["interrupt", "run_controlled_stop interrupt"],
      ["cleanup", "cleanup"],
    ]) {
      expect(source).toContain(`interaction_phase=${phase}\n${next}`);
    }
  });

  it("keeps the local dev Go toolchain pinned without auto-download", () => {
    const devLauncher = readFileSync("scripts/cloud-agents-dev.sh", "utf8");
    const mise = readFileSync(".mise.toml", "utf8");
    expect(devLauncher).toContain("export GOTOOLCHAIN=local");
    expect(devLauncher).toContain("go version go1.26.6 ");
    expect(devLauncher).not.toContain("GOTOOLCHAIN=go1.26.6");
    expect(mise).toContain('GOTOOLCHAIN = "local"');
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
    expect(dockerfile).toMatch(/^ARG BASE_IMAGE=\S+@sha256:[0-9a-f]{64}$/mu);
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
    expect(adminDockerfile).toMatch(/^ARG BASE_IMAGE=\S+@sha256:[0-9a-f]{64}$/mu);
    expect(userDockerfile).toMatch(/^ARG BASE_IMAGE=\S+@sha256:[0-9a-f]{64}$/mu);
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
      expect(dockerfile).toMatch(/^ARG BASE_IMAGE=\S+@sha256:[0-9a-f]{64}$/mu);
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
    const workerSupply = buildWorkerOciSupplyArtifacts(process.cwd());
    const deployment = readDeterministicUstar(
      buildPlatformDeploymentPackage(process.cwd(), workerSupply),
    );
    const deploymentPaths = deployment.map(({ path }) => path);
    expect(deploymentPaths).toEqual(
      expect.arrayContaining([
        "LICENSE",
        "NOTICE",
        "SOURCE_PROVENANCE.md",
        "services/control-plane/THIRD_PARTY_NOTICES.md",
      ]),
    );
    for (const path of [
      "NOTICE",
      "SOURCE_PROVENANCE.md",
      "services/control-plane/THIRD_PARTY_NOTICES.md",
      "deploy/docker/worker-oci-package-authority.json",
      "deploy/docker/worker-tools/package.json",
      "deploy/docker/worker-tools/package-lock.json",
      "tools/opensandbox-execd-successor/v1/README.md",
      "tools/opensandbox-execd-successor/v1/source.json",
      "tools/opensandbox-execd-successor/v1/LICENSE",
      "tools/opensandbox-execd-successor/v1/Dockerfile",
      "tools/opensandbox-execd-successor/v1/patches/pty-terminal-exit.patch",
      "scripts/build-opensandbox-execd-successor.ts",
      "scripts/lib/opensandbox-execd-successor.ts",
      "tools/opensandbox-server-successor/v1/README.md",
      "tools/opensandbox-server-successor/v1/source.json",
      "tools/opensandbox-server-successor/v1/LICENSE",
      "tools/opensandbox-server-successor/v1/Dockerfile",
      "tools/opensandbox-server-successor/v1/patches/isolate-http-client-cookie.patch",
      "test/scripts/cookie-isolation-regression.py",
      "scripts/build-opensandbox-server-successor.ts",
    ]) {
      expect(Buffer.from(deployment.find((entry) => entry.path === path)!.data)).toEqual(
        readFileSync(path),
      );
    }
    const migrateDockerfile = new TextDecoder().decode(
      deployment.find(({ path }) => path === "deploy/docker/migrate.Dockerfile")!.data,
    );
    expect(migrateDockerfile).toMatch(/^ARG BASE_IMAGE=\S+@sha256:[0-9a-f]{64}$/mu);
    expect(migrateDockerfile).toContain(
      `cloud-agents-migrations-${PLATFORM_RELEASE_MIGRATION_HEAD}.tar`,
    );
    expect(migrateDockerfile).toContain(`product/${PLATFORM_RELEASE_MIGRATION_HEAD}/manifest.json`);
    expect(migrateDockerfile).not.toContain("@PLATFORM_MIGRATION_");
    expect(
      Buffer.from(
        deployment.find(({ path }) => path === WORKER_OCI_INSTALL_MANIFEST_ARCHIVE_PATH)!.data,
      ),
    ).toEqual(workerSupply.manifestBytes);
    expect(
      Buffer.from(deployment.find(({ path }) => path === WORKER_OCI_NOTICES_ARCHIVE_PATH)!.data),
    ).toEqual(workerSupply.noticeBytes);
    expect(
      platformReleaseArtifactFilename(
        "cloud-agents-worker-oci-install-manifest",
        "portable",
        PLATFORM_RELEASE_MIGRATION_HEAD,
      ),
    ).toBe(WORKER_OCI_INSTALL_MANIFEST_FILENAME);
    expect(
      platformReleaseArtifactFilename(
        "cloud-agents-worker-oci-notices",
        "portable",
        PLATFORM_RELEASE_MIGRATION_HEAD,
      ),
    ).toBe(WORKER_OCI_NOTICES_FILENAME);
    const workerDockerfile = readFileSync("deploy/docker/worker.Dockerfile", "utf8");
    const packagedWorkerDockerfile = new TextDecoder().decode(
      deployment.find(({ path }) => path === "deploy/docker/worker.Dockerfile")!.data,
    );
    const packagedManagedAgentCompose = new TextDecoder().decode(
      deployment.find(({ path }) => path === "deploy/compose/docker-compose.managed-agent.yml")!
        .data,
    );
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-landlock-run-${TARGETOS}-${TARGETARCH} /usr/local/bin/cloud-agents-landlock-run",
    );
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-landlock-notices.txt /usr/share/doc/cloud-agents/landlock-notices.txt",
    );
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-worker-oci-install-manifest.json /usr/share/doc/cloud-agents/worker-oci-install-manifest.json",
    );
    expect(workerDockerfile).toContain(
      "COPY cloud-agents-worker-oci-notices.md /usr/share/doc/cloud-agents/worker-oci-notices.md",
    );
    expect(workerDockerfile).toContain("ARG DEBIAN_MIRROR=http://deb.debian.org/debian");
    expect(workerDockerfile).toContain(
      "ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security",
    );
    expect(workerDockerfile).toContain(
      "s#http://deb.debian.org/debian-security#${DEBIAN_SECURITY_MIRROR}#g",
    );
    expect(packagedWorkerDockerfile).toContain("ARG DEBIAN_MIRROR=http://deb.debian.org/debian");
    expect(packagedWorkerDockerfile).toContain(
      "ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security",
    );
    expect(packagedManagedAgentCompose).toContain(
      "DEBIAN_MIRROR: ${CLOUD_AGENTS_DEBIAN_MIRROR:-http://deb.debian.org/debian}",
    );
    expect(packagedManagedAgentCompose).toContain(
      "DEBIAN_SECURITY_MIRROR: ${CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR:-http://deb.debian.org/debian-security}",
    );
    expect(managedAgent).toContain(
      "DEBIAN_MIRROR: ${CLOUD_AGENTS_DEBIAN_MIRROR:-http://deb.debian.org/debian}",
    );
    expect(managedAgent).toContain(
      "DEBIAN_SECURITY_MIRROR: ${CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR:-http://deb.debian.org/debian-security}",
    );
    const ociSmoke = readFileSync("test/e2e/test-platform-oci-images.sh", "utf8");
    expect(ociSmoke).toContain('--build-arg "DEBIAN_MIRROR=$CLOUD_AGENTS_DEBIAN_MIRROR"');
    expect(ociSmoke).toContain(
      '--build-arg "DEBIAN_SECURITY_MIRROR=$CLOUD_AGENTS_DEBIAN_SECURITY_MIRROR"',
    );
    expect(ociSmoke).toContain('cmp "$smoke_directory/image-worker-package-lock.json"');
    expect(ociSmoke).toContain('cmp "$smoke_directory/image-worker-oci-install-manifest.json"');
    expect(ociSmoke).toContain(
      'cmp "$smoke_directory/deploy/docker/worker-oci-install-manifest.json"',
    );
    expect(ociSmoke).toContain(
      '["packageManifest", "packageManifestSha256", "deploy/docker/worker-tools/package.json"]',
    );
    expect(ociSmoke).toContain(
      '["lockfile", "lockfileSha256", "deploy/docker/worker-tools/package-lock.json"]',
    );
    expect(ociSmoke).toContain('require("/opt/cloud-agents-worker-tools/node_modules/node-pty")');
    const workerPackage = JSON.parse(
      readFileSync("deploy/docker/worker-tools/package.json", "utf8"),
    ) as {
      dependencies: Record<string, string>;
      optionalDependencies: Record<string, string>;
    };
    expect(workerPackage.dependencies).toMatchObject({
      "@openai/codex": "0.154.0",
      "@deepseek-ai/dsh": "0.1.2-rc.1",
      "@deepseek-ai/dsh-llm-pi-ai": "0.1.2-rc.1",
    });
    expect(workerPackage.optionalDependencies).toMatchObject({
      "@anthropic-ai/claude-agent-sdk-linux-arm64": "0.3.207",
      "@anthropic-ai/claude-agent-sdk-linux-x64": "0.3.207",
    });
    expect(workerDockerfile).toContain("ADD cloud-agents-deployment-*.tar /deployment/");
    expect(workerDockerfile).toContain(
      "COPY --from=lock-inputs /deployment/deploy/docker/worker-tools/package-lock.json /opt/cloud-agents-worker-tools/package-lock.json",
    );
    expect(workerDockerfile).toContain(
      "npm ci --prefix /opt/cloud-agents-worker-tools --ignore-scripts --omit=dev",
    );
    expect(workerDockerfile).not.toContain("npm install --global");
    expect(workerDockerfile).toContain('test "$(codex --version)" = "codex-cli 0.154.0"');
    expect(workerDockerfile).toContain('test "$(claude --version)" = "2.1.207 (Claude Code)"');
    expect(workerDockerfile).toContain('test "$(dsh --version)" = "0.1.2-rc.1"');
    expect(workerDockerfile).toContain("CLOUD_AGENT_DEEPSEEK_HARNESS_BIN=/usr/local/bin/dsh");
    expect(workerDockerfile).toContain("chown 1000:1000 /workspace");
    expect(workerDockerfile).toContain("chmod 0700 /workspace");
    expect(workerDockerfile).not.toContain("@latest");
  });

  it("binds the installed inventory collector into the Worker deployment authority", () => {
    const workerSupply = buildWorkerOciSupplyArtifacts(process.cwd());
    const path = "scripts/lib/worker-oci-installed.ts";
    expect(workerSupply.manifest.source).toMatchObject({
      installedInventoryCollector: path,
      installedInventoryCollectorSha256: expect.stringMatching(/^[0-9a-f]{64}$/u),
    });
    const deployment = readDeterministicUstar(
      buildPlatformDeploymentPackage(process.cwd(), workerSupply),
    );
    expect(Buffer.from(deployment.find((entry) => entry.path === path)!.data)).toEqual(
      workerSupply.sourceFiles.get(path),
    );
  });

  it("packages the exact Worker supply source capture after source paths change", () => {
    const sourceRoot = temporaryDirectory("cloud-agents-worker-supply-capture-");

    for (const [path, bytes] of buildWorkerOciSupplyArtifacts(process.cwd()).sourceFiles) {
      mkdirSync(dirname(join(sourceRoot, path)), { recursive: true });
      writeFileSync(join(sourceRoot, path), bytes);
    }
    const workerSupply = buildWorkerOciSupplyArtifacts(sourceRoot);
    writeFileSync(join(sourceRoot, "deploy/docker/worker-tools/package-lock.json"), "changed\n");
    writeFileSync(join(sourceRoot, "scripts/lib/worker-oci-installed.ts"), "changed\n");
    for (const path of workerSupply.sourceFiles.keys()) {
      if (path.startsWith("deploy/docker/worker-oci-supplemental-licenses/")) {
        writeFileSync(join(sourceRoot, path), "changed after capture\n");
      }
    }

    const deployment = readDeterministicUstar(
      buildPlatformDeploymentPackage(process.cwd(), workerSupply),
    );
    for (const [path, bytes] of workerSupply.sourceFiles) {
      expect(deployment.filter((entry) => entry.path === path)).toHaveLength(1);
      expect(Buffer.from(deployment.find((entry) => entry.path === path)!.data)).toEqual(bytes);
    }
    expect(
      Buffer.from(
        deployment.find(({ path }) => path === WORKER_OCI_INSTALL_MANIFEST_ARCHIVE_PATH)!.data,
      ),
    ).toEqual(workerSupply.manifestBytes);
  });

  it("rejects a Worker OCI manifest that redirects a source binding to another file", () => {
    const directory = temporaryDirectory("cloud-agents-worker-source-binding-");

    const workerSupply = buildWorkerOciSupplyArtifacts(process.cwd());
    for (const [path, bytes] of workerSupply.sourceFiles) {
      mkdirSync(dirname(join(directory, path)), { recursive: true });
      writeFileSync(join(directory, path), bytes);
    }
    const manifestPath = join(directory, "manifest.json");
    writeFileSync(manifestPath, workerSupply.manifestBytes);
    const ociSmoke = readFileSync("test/e2e/test-platform-oci-images.sh", "utf8");
    const start = ociSmoke.indexOf(
      'CLOUD_AGENTS_OCI_MANIFEST="$candidate_directory/cloud-agents-worker-oci-install-manifest.json"',
    );
    const script = ociSmoke.slice(start).match(/node <<'NODE'\n([\s\S]*?)\nNODE/u)?.[1];
    expect(script).toBeDefined();
    const env = {
      ...process.env,
      CLOUD_AGENTS_OCI_MANIFEST: manifestPath,
      CLOUD_AGENTS_OCI_DEPLOY: directory,
    };
    expect(spawnSync("node", ["-e", script!], { env }).status).toBe(0);

    const manifest = JSON.parse(workerSupply.manifestBytes.toString("utf8")) as {
      source: Record<string, string>;
    };
    manifest.source.lockfile = manifest.source.packageManifest!;
    manifest.source.lockfileSha256 = manifest.source.packageManifestSha256!;
    writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
    const rejected = spawnSync("node", ["-e", script!], { encoding: "utf8", env });
    expect(rejected.status).not.toBe(0);
    expect(rejected.stderr).toContain("Worker OCI source binding lockfile is invalid");
  });

  it("keeps retained OCI evidence outside the candidate and refuses existing destinations", () => {
    const directory = temporaryDirectory("cloud-agents-oci-evidence-boundary-");

    const source = readFileSync("test/e2e/test-platform-oci-images.sh", "utf8");
    const start = source.indexOf("evidence_directory=$(node - ");
    const script = source.slice(start).match(/<<'NODE'\n([\s\S]*?)\nNODE/u)?.[1];
    expect(script).toBeDefined();
    const candidate = join(directory, "candidate");
    mkdirSync(candidate);
    const input = join(candidate, "evidence");
    const run = (destination: string) =>
      spawnSync("node", ["-", destination, candidate], {
        input: script!,
        encoding: "utf8",
        env: { ...process.env, TMPDIR: candidate },
      });
    expect(run(input).stderr).toContain("OCI evidence must be outside the candidate directory");
    expect(existsSync(input)).toBe(false);
    const external = join(directory, "external");
    expect(run(external).status).toBe(0);
    writeFileSync(join(external, "sentinel"), "keep");
    expect(run(external).status).not.toBe(0);
    expect(readFileSync(join(external, "sentinel"), "utf8")).toBe("keep");
    const defaultDestination = run("");
    expect(defaultDestination.status).toBe(0);
    const created = defaultDestination.stdout.trim();
    expect(created).not.toContain(candidate);
    rmSync(created, { recursive: true });
  });

  it("binds retained OCI receipts to the observed image architecture and inventory bytes", () => {
    const directory = temporaryDirectory("cloud-agents-worker-oci-receipt-");

    const source = readFileSync("test/e2e/test-platform-oci-images.sh", "utf8");
    const script = source.match(
      /node - "\$evidence_directory" "\$platform" "\$candidate_directory" <<'NODE'\n([\s\S]*?)\nNODE/u,
    )?.[1];
    expect(script).toBeDefined();
    const image = { imageId: `sha256:${"a".repeat(64)}`, os: "linux", architecture: "arm64" };
    const packageFiles = [{ path: "package.json", size: 3, sha256: "b".repeat(64) }];
    const currentPaths = ["node_modules/native-addon", "node_modules/native-fixture"];
    const packageLockBytes = JSON.stringify({
      lockfileVersion: 3,
      packages: {
        ...Object.fromEntries(
          currentPaths.map((path) => [path, { cpu: ["arm64"], os: ["linux"] }]),
        ),
        "node_modules/native-x64": { cpu: ["x64"], os: ["linux"] },
      },
    });
    const packageLockSha256 = createHash("sha256").update(packageLockBytes).digest("hex");
    const staticBytes = JSON.stringify({
      schemaVersion: 2,
      source: { lockfileSha256: packageLockSha256 },
      packageBindings: [...currentPaths, "node_modules/native-x64"].map((path) => ({
        path,
        packageFiles,
      })),
    });
    const inventory = {
      schemaVersion: 1,
      kind: "cloud-agents-worker-oci-installed-inventory",
      platform: "linux/arm64",
      status: "BLOCKED",
      packageRecordConsistency: "PASS",
      licenseArtifactPresence: "BLOCKED",
      source: {
        staticManifestSha256: createHash("sha256").update(staticBytes).digest("hex"),
        packageLockSha256,
      },
      fullSupplyChain: { status: "BLOCKED", limits: ["License review remains open"] },
      npm: {
        packages: [
          ...currentPaths.map((physicalPath) => ({
            physicalPath,
            packageContentVerification: "PASS",
            packageFiles,
          })),
          {
            physicalPath: "node_modules/unbound",
            packageContentVerification: "NOT_RUN",
            packageFiles: null,
          },
        ],
      },
    };
    const candidateDirectory = join(directory, "candidate");
    mkdirSync(candidateDirectory);
    writeFileSync(join(candidateDirectory, "deployment.tar"), "deployment\n");
    writeFileSync(join(candidateDirectory, "static.json"), staticBytes);
    const candidate = {
      version: "0.1.0-dev.20261007",
      sourceCommit: "a".repeat(40),
      sourceDirty: true,
      artifacts: [
        ["cloud-agents-deployment", "deployment.tar"],
        ["cloud-agents-worker-oci-install-manifest", "static.json"],
      ].map(([name, filename]) => ({
        name,
        filename,
        target: "portable",
        sha256: `sha256:${createHash("sha256")
          .update(readFileSync(join(candidateDirectory, filename!)))
          .digest("hex")}`,
      })),
    };
    writeFileSync(
      join(candidateDirectory, "platform-release-manifest.json"),
      JSON.stringify(candidate),
    );
    writeFileSync(join(directory, "image.json"), JSON.stringify(image));
    const sharpResult = {
      status: "PASS",
      scope: "native Sharp RGB PNG encode/decode",
      width: 1,
      height: 1,
      channels: 3,
      pixelHex: "020304",
      loadedPackagePaths: currentPaths,
    };
    writeFileSync(join(directory, "sharp-native.json"), JSON.stringify(sharpResult));
    writeFileSync(join(directory, "package-lock.json"), packageLockBytes);
    writeFileSync(join(directory, "installed-inventory.json"), JSON.stringify(inventory));
    writeFileSync(
      join(directory, "npm-ls.json"),
      JSON.stringify({ name: "tools", version: "1.0.0" }),
    );
    const run = (platform: string) =>
      spawnSync("node", ["-", directory, platform, candidateDirectory], {
        input: script!,
        encoding: "utf8",
      });
    const mismatch = run("linux/amd64");
    expect(mismatch.status).not.toBe(0);
    expect(mismatch.stderr).toContain("Worker inventory image binding mismatch");
    writeFileSync(
      join(directory, "installed-inventory.json"),
      JSON.stringify({ ...inventory, fullSupplyChain: { status: "PASS" } }),
    );
    expect(run("linux/arm64").stderr).toContain(
      "Worker inventory schema or supply-chain scope is invalid",
    );
    writeFileSync(join(directory, "installed-inventory.json"), JSON.stringify(inventory));
    writeFileSync(join(candidateDirectory, "deployment.tar"), "tampered\n");
    expect(run("linux/arm64").stderr).toContain(
      "OCI candidate artifact cloud-agents-deployment has drifted",
    );
    writeFileSync(join(candidateDirectory, "deployment.tar"), "deployment\n");
    writeFileSync(
      join(directory, "installed-inventory.json"),
      JSON.stringify({
        ...inventory,
        npm: { packages: [{ ...inventory.npm.packages[0], packageFiles: [] }] },
      }),
    );
    expect(run("linux/arm64").stderr).toContain("package content binding mismatch");
    for (const packages of [inventory.npm.packages.slice(1), inventory.npm.packages.slice(2)]) {
      writeFileSync(
        join(directory, "installed-inventory.json"),
        JSON.stringify({ ...inventory, npm: { packages } }),
      );
      expect(run("linux/arm64").stderr).toContain(
        "current-platform package content checks are incomplete",
      );
    }
    writeFileSync(join(directory, "installed-inventory.json"), JSON.stringify(inventory));
    const reducedStatic = JSON.stringify({
      ...JSON.parse(staticBytes),
      packageBindings: JSON.parse(staticBytes).packageBindings.slice(1),
    });
    const reducedSha256 = createHash("sha256").update(reducedStatic).digest("hex");
    writeFileSync(join(candidateDirectory, "static.json"), reducedStatic);
    writeFileSync(
      join(candidateDirectory, "platform-release-manifest.json"),
      JSON.stringify({
        ...candidate,
        artifacts: candidate.artifacts.map((artifact) =>
          artifact.filename === "static.json"
            ? { ...artifact, sha256: `sha256:${reducedSha256}` }
            : artifact,
        ),
      }),
    );
    writeFileSync(
      join(directory, "installed-inventory.json"),
      JSON.stringify({
        ...inventory,
        source: { ...inventory.source, staticManifestSha256: reducedSha256 },
        npm: {
          packages: inventory.npm.packages.map((item) =>
            item.physicalPath === currentPaths[0]
              ? { ...item, packageContentVerification: "NOT_RUN", packageFiles: null }
              : item,
          ),
        },
      }),
    );
    expect(run("linux/arm64").stderr).toContain(
      "Sharp loaded packages do not have verified contents",
    );
    writeFileSync(join(candidateDirectory, "static.json"), staticBytes);
    writeFileSync(
      join(candidateDirectory, "platform-release-manifest.json"),
      JSON.stringify(candidate),
    );
    writeFileSync(join(directory, "installed-inventory.json"), JSON.stringify(inventory));
    for (const result of [
      { ...sharpResult, status: "FAIL" },
      { ...sharpResult, pixelHex: "000000" },
      { ...sharpResult, scope: "another check" },
    ]) {
      writeFileSync(join(directory, "sharp-native.json"), JSON.stringify(result));
      expect(run("linux/arm64").stderr).toContain("Sharp native result is invalid");
    }
    writeFileSync(join(directory, "sharp-native.json"), JSON.stringify(sharpResult));
    writeFileSync(join(directory, "package-lock.json"), "tampered\n");
    expect(run("linux/arm64").stderr).toContain("package lock binding mismatch");
    writeFileSync(join(directory, "package-lock.json"), packageLockBytes);
    const pass = run("linux/arm64");
    expect(pass.status, pass.stderr).toBe(0);
    const receipt = JSON.parse(readFileSync(join(directory, "receipt.json"), "utf8"));
    expect(receipt).toMatchObject({
      imageId: image.imageId,
      inventoryStatus: "BLOCKED",
      fullSupplyChain: inventory.fullSupplyChain,
      qualifiedPackageContents: currentPaths.map((path) => ({ path, files: 1 })),
    });
    expect(receipt.candidate).toMatchObject({
      version: candidate.version,
      sourceCommit: candidate.sourceCommit,
    });
    expect(receipt.artifacts).toContainEqual({
      filename: "installed-inventory.json",
      sha256: createHash("sha256")
        .update(readFileSync(join(directory, "installed-inventory.json")))
        .digest("hex"),
    });
    expect(run("linux/arm64").status).not.toBe(0);
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
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
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

  it("retries target cleanup conflicts even when the CLI reports zero status", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const definition = source.match(/cleanup_target_with_retry\(\) \{[\s\S]*?\n\}/u)?.[0];
    expect(definition).toBeDefined();
    const script = [
      "set -eu",
      "project_id=project",
      "state_file=$(mktemp)",
      "trap 'rm -f \"$state_file\"' EXIT",
      "cloud_agentsctl() {",
      "  attempt=$(cat \"$state_file\" 2>/dev/null || printf '0')",
      "  attempt=$((attempt + 1))",
      '  printf \'%s\' "$attempt" > "$state_file"',
      '  if [ "$attempt" -eq 1 ]; then',
      "    printf '%s\\n' 'adminCleanupDeploymentTarget: TARGET_CONFLICT'",
      "    return 0",
      "  fi",
      "  printf '%s\\n' 'cleanup=ok'",
      "}",
      "sleep() { :; }",
      definition!,
      "output=$(cleanup_target_with_retry target 1 cleanup)",
      'test "$output" = cleanup=ok',
      'test "$(cat "$state_file")" -eq 2',
    ].join("\n");
    const result = spawnSync("sh", ["-c", script], { encoding: "utf8" });
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("");
    expect(result.stderr).toBe("");
  });

  it("verifies candidate bytes before Compose extraction or CLI use", () => {
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const verifier =
      'node "$script_directory/../../scripts/lib/platform-release-verifier.ts" "$candidate_directory"';
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
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(composeSmoke).toContain("opensandbox_owned_cleanup_attempt_limit=180");
    expect(composeSmoke).toContain(
      'if [ "$attempt" -ge "$opensandbox_owned_cleanup_attempt_limit" ]; then',
    );
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
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
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
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(source).toContain('NO_PROXY="$kubernetes_no_proxy" no_proxy="$kubernetes_no_proxy"');
    expect(source).toContain("kubernetes_ctl get --raw /version >/dev/null");
    expect(source).toContain("Kubernetes Runtime smoke requires digest-pinned OpenSandbox images");
    const start = source.indexOf(
      "CLOUD_AGENTS_COMPOSE_OPERATION_FILE=\"$upgrade_operation_file\" node <<'NODE'",
    );
    const script = source.slice(start).match(/node <<'NODE'\n([\s\S]*?)\nNODE/u)?.[1];
    expect(script).toBeDefined();
    const directory = temporaryDirectory("cloud-agents-upgrade-operation-");

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
    const success = spawnSync("node", ["-e", script!], {
      encoding: "utf8",
      env: { ...process.env, CLOUD_AGENTS_COMPOSE_OPERATION_FILE: file },
    });
    expect(success.status).toBe(0);

    writeFileSync(file, JSON.stringify({ ...operation, state: "failed", currentStep: "cleanup" }));
    const failure = spawnSync("node", ["-e", script!], {
      encoding: "utf8",
      env: { ...process.env, CLOUD_AGENTS_COMPOSE_OPERATION_FILE: file },
    });
    expect(failure.status).not.toBe(0);
    expect(failure.stderr).toContain('"state":"failed"');
    expect(failure.stderr).toContain('"currentStep":"cleanup"');
  });

  it("keeps restored snapshot semantic digest independent from raw tar bytes", () => {
    const composeSmoke = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
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
    const directory = temporaryDirectory("cloud-agents-snapshot-digest-");

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
  });

  it.each(["approval", "recovery"])(
    "keeps %s failure diagnostics free of error text and tool content",
    (kind) => {
      const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
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
      const directory = temporaryDirectory("cloud-agents-capability-diagnostics-");

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
    },
  );

  it("reports capability acceptance only after artifact and event checks", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
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

  it("accepts capability materialization from the credentials capability subdirectory", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(source).toContain(
      'capability_materialization_source_directory="$real_provider_credentials_directory/capability"',
    );
    expect(source).toContain('-v "$capability_materialization_source_directory:/source:ro"');
    expect(source).toContain(
      "capability acceptance requires tenant-compose-smoke.capabilities.json in the credentials directory or its capability subdirectory",
    );
  });

  it("runs capability-bound recovery for supported Providers and parameterized negative checks", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const interactions = readFileSync("test/e2e/test-platform-agent-interactions.sh", "utf8");
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
      "assert_interaction_takeover \"$final_file\" 'process-restart|same-node-reconnect'",
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
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const recovery = source.slice(source.indexOf("run_real_provider_recovery()"));
    expect(recovery).not.toContain('"$recovery_cross_node" -eq 0');
    expect(recovery).toContain('start_capability_mcp_fixture "$kubernetes_agent_runtime_id"');
    expect(recovery).toContain('start_capability_mcp_fixture "$remote_agent_runtime_id"');
    expect(recovery).toContain('start_capability_mcp_fixture "$foundation_agent_runtime_id"');
    expect(source).toContain('value.spec?.runtimeId??""');
    expect(source).toContain('kubernetes_ctl -n "$kubernetes_active_namespace" get pods');
  });

  it("binds Kubernetes cross-node OpenSandbox upstreams to the validated kind network", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    expect(source).toContain("kind_node_ip() {");
    expect(source).toContain(
      String.raw`docker inspect "$kind_node_name" --format "{{(index .NetworkSettings.Networks \"$kind_network\").IPAddress}}"`,
    );
    expect(source).toContain(
      'kubernetes_opensandbox_upstream_host=$(kind_node_ip "$kubernetes_source_node")',
    );
    expect(source).toContain(
      'kubernetes_destination_opensandbox_upstream_host=$(kind_node_ip "$kubernetes_destination_node")',
    );
    expect(source).not.toContain("'{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'");
  });

  it("gates one real MCP transport disconnect and reconnect cell without replay", () => {
    const source = readFileSync("test/e2e/test-platform-compose.sh", "utf8");
    const fixture = readFileSync("test/e2e/fixtures/managed-capability-mcp-server.mjs", "utf8");
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

type RuntimeCandidateManifest = {
  readonly schemaVersion: 1;
  readonly kind: "cloud-agent-portable-runtime-rc-candidate";
  readonly candidateDigest: string;
  readonly sourceCommit: string;
  readonly sourceDirty: boolean;
  readonly sameBitsVerified: boolean;
  readonly standaloneRuntime: { readonly filename: string; readonly sha256: string };
  readonly runtimeNotices: {
    readonly name: string;
    readonly target: string;
    readonly filename: string;
    readonly sizeBytes: number;
    readonly sha256: string;
  };
  readonly packages: ReadonlyArray<PackedCloudAgentPackage>;
};

function runtimeCandidateFixture(): {
  readonly root: string;
  readonly repositoryRoot: string;
  readonly candidateDirectory: string;
  readonly sourceCommit: string;
  readonly runtimeBytes: Buffer;
  readonly noticeBytes: Buffer;
  readonly manifest: RuntimeCandidateManifest;
  readonly manifestBytes: Buffer;
} {
  const root = temporaryDirectory("cloud-agents-runtime-candidate-");
  const repositoryRoot = join(root, "repository");
  const candidateDirectory = join(root, "candidate");
  mkdirSync(join(repositoryRoot, dirname(CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH)), {
    recursive: true,
  });
  mkdirSync(candidateDirectory);
  const sourceCommit = "a".repeat(40);
  const runtimeBytes = Buffer.from("export const runtime = true;\n");
  const noticeBytes = Buffer.from("Runtime notice authority\n");
  writeFileSync(join(repositoryRoot, CLOUD_AGENT_RUNTIME_NOTICES_SOURCE_PATH), noticeBytes);
  writeFileSync(join(candidateDirectory, "cloud-agent-runtime-standalone.mjs"), runtimeBytes);
  writeFileSync(join(candidateDirectory, CLOUD_AGENT_RUNTIME_NOTICES_FILENAME), noticeBytes);
  const packages: PackedCloudAgentPackage[] = CLOUD_AGENT_PUBLIC_PACKAGES.map((name, index) => {
    const filename = `package-${String(index)}.tgz`;
    const bytes = Buffer.from(`package ${name}\n`);
    writeFileSync(join(candidateDirectory, filename), bytes);
    return {
      name,
      version: "0.1.0-rc.1",
      filename,
      sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    };
  });
  const manifest: RuntimeCandidateManifest = {
    schemaVersion: 1,
    kind: "cloud-agent-portable-runtime-rc-candidate",
    candidateDigest: cloudAgentCandidateDigest(packages),
    sourceCommit,
    sourceDirty: true,
    sameBitsVerified: true,
    standaloneRuntime: {
      filename: "cloud-agent-runtime-standalone.mjs",
      sha256: `sha256:${createHash("sha256").update(runtimeBytes).digest("hex")}`,
    },
    runtimeNotices: {
      name: "cloud-agent-runtime-notices",
      target: "portable",
      filename: CLOUD_AGENT_RUNTIME_NOTICES_FILENAME,
      sizeBytes: noticeBytes.length,
      sha256: `sha256:${createHash("sha256").update(noticeBytes).digest("hex")}`,
    },
    packages,
  };
  const manifestBytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  writeFileSync(join(candidateDirectory, "candidate-manifest.json"), manifestBytes);
  return {
    root,
    repositoryRoot,
    candidateDirectory,
    sourceCommit,
    runtimeBytes,
    noticeBytes,
    manifest,
    manifestBytes,
  };
}

function temporaryDirectory(prefix: string): string {
  const directory = mkdtempSync(join(tmpdir(), prefix));
  onTestFinished(() => rmSync(directory, { recursive: true, force: true }));
  return directory;
}
