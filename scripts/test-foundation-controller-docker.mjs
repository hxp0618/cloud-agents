import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [output] = process.argv.slice(2);
assert.ok(output, "usage: node scripts/test-foundation-controller-docker.mjs NEW_OUTPUT_DIRECTORY");
const root = resolve(import.meta.dirname, "..");
const evidenceDirectory = resolve(output);
mkdirSync(evidenceDirectory, { mode: 0o700 });
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-foundation-controller-"));
const run = `foundation-controller-${randomUUID()}`;
const postgresName = `${run}-postgres`;
const sandboxServerName = `${run}-opensandbox`;
const serverImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb";
const execdImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd";
const sandboxImage = "node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5";
const failureImage =
  "rancher/mirrored-pause:3.6@sha256:74c4244427b7312c5b901fe0f67cbc53683d06f4f24c6faee65d4182bf0fa893";
const apiKey = randomUUID();
const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 180_000,
  }).trim();
const psql = (query, user = "postgres", database = "foundation_live") =>
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      postgresName,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      user,
      "-d",
      database,
    ],
    { encoding: "utf8", input: query, timeout: 120_000, stdio: ["pipe", "pipe", "pipe"] },
  ).trim();
const mappedPort = (name, port) => {
  const address = docker("port", name, `${port}/tcp`).split("\n")[0];
  return Number(address.slice(address.lastIndexOf(":") + 1));
};
const goBuild = (outputPath, packagePath, test = false) =>
  execFileSync(
    "go",
    test
      ? ["test", "-c", "-o", outputPath, packagePath]
      : ["build", "-trimpath", "-o", outputPath, packagePath],
    { cwd: root, timeout: 180_000 },
  );
const parseMarker = (text, marker) => {
  const match = text.match(new RegExp(`${marker}=(\\{[^\\n]+\\})`));
  assert.ok(match, `${marker} missing from ${text}`);
  return JSON.parse(match[1]);
};
const request = async (base, path, method = "GET") => {
  const response = await fetch(base + path, {
    method,
    headers: { "OPEN-SANDBOX-API-KEY": apiKey },
    signal: AbortSignal.timeout(10_000),
  });
  const text = await response.text();
  return { status: response.status, text };
};

let postgresStarted = false;
let sandboxServerStarted = false;
let sandboxBase = "";
let prepareReceipt;
try {
  assert.equal(
    docker("ps", "-aq", "--filter", "label=opensandbox.io/id"),
    "",
    "refusing to share an OpenSandbox Docker engine with existing runtime objects",
  );
  assert.equal(
    docker(
      "volume",
      "ls",
      "-q",
      "--filter",
      "label=cloud-agents.dev/resource=foundation-workspace",
    ),
    "",
    "refusing to share an engine with existing foundation Workspace volumes",
  );
  for (const image of [
    "postgres:17.6-bookworm",
    serverImage,
    execdImage,
    sandboxImage,
    failureImage,
  ]) {
    docker("image", "inspect", image);
  }
  const dockerHost = docker(
    "context",
    "inspect",
    "orbstack",
    "--format",
    "{{.Endpoints.docker.Host}}",
  );
  assert.ok(dockerHost.startsWith("unix://"));
  const dockerSocket = dockerHost.slice("unix://".length);

  docker(
    "run",
    "-d",
    "--name",
    postgresName,
    "--label",
    `cloud-agents-foundation-controller-test=${run}`,
    "-p",
    "127.0.0.1::5432",
    "-e",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "postgres:17.6-bookworm",
  );
  postgresStarted = true;
  for (let attempt = 0; ; attempt++) {
    try {
      assert.equal(psql("SELECT 1", "postgres", "postgres"), "1");
      await delay(100);
      assert.equal(psql("SELECT 1", "postgres", "postgres"), "1");
      break;
    } catch {
      if (attempt === 100) throw new Error("PostgreSQL did not start");
      await delay(100);
    }
  }
  const postgresPort = mappedPort(postgresName, 5432);
  const migrationURL = `postgres://foundation_migration@127.0.0.1:${postgresPort}/foundation_live?sslmode=disable`;
  const runtimeURL = `postgres://foundation_runtime@127.0.0.1:${postgresPort}/foundation_live?sslmode=disable`;
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      postgresName,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "postgres",
    ],
    {
      input: readFileSync(resolve(root, "services/control-plane/migrations/bootstrap/roles.sql")),
      timeout: 120_000,
    },
  );
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      postgresName,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      "postgres",
      "-d",
      "postgres",
    ],
    {
      input: `CREATE DATABASE foundation_live;\nCREATE ROLE foundation_migration LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;\nCREATE ROLE foundation_runtime LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;\nCREATE ROLE foundation_bootstrap LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;\nGRANT cloud_agents_migration_owner TO foundation_migration;\nGRANT cloud_agents_runtime TO foundation_runtime;\nGRANT cloud_agents_bootstrap_admin TO foundation_bootstrap;\nGRANT CREATE ON DATABASE foundation_live TO cloud_agents_migration_owner;\n`,
      timeout: 120_000,
    },
  );

  const migrateBinary = resolve(build, "cloud-agents-product-migrate");
  const serverTestBinary = resolve(build, "server.test");
  const controllerTestBinary = resolve(build, "foundationcontroller.test");
  goBuild(migrateBinary, "./services/control-plane/cmd/cloud-agents-product-migrate");
  goBuild(serverTestBinary, "./services/control-plane/internal/server", true);
  goBuild(controllerTestBinary, "./services/control-plane/internal/foundationcontroller", true);
  const migration = JSON.parse(
    execFileSync(
      migrateBinary,
      [
        "--repository-root",
        root,
        "--manifest",
        "services/control-plane/migrations/product/000057/manifest.json",
        "--selector",
        "product-000057",
      ],
      {
        encoding: "utf8",
        env: { ...process.env, CLOUD_AGENTS_PLATFORM_DATABASE_URL: migrationURL },
        timeout: 180_000,
      },
    ),
  );
  assert.equal(migration.schema_head, "000057");

  psql(
    `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
      'tenant','tenant','Foundation live tenant','organization','organization','Foundation live organization',
      'user','https://foundation.test','foundation-admin','membership','membership','binding','binding',
      'tenant-audit','membership-audit','binding-audit','foundation-live');`,
    "foundation_bootstrap",
  );
  psql(`SET ROLE cloud_agents_migration_owner;
    WITH next_revision AS (
      UPDATE cloud_agents.tenant_resource_versions SET current_revision=current_revision+1,updated_at=clock_timestamp()
      WHERE tenant_id='tenant' AND tenant_uid='tenant' RETURNING current_revision
    ), project_change AS (
      INSERT INTO cloud_agents.resource_changes (tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at)
      SELECT 'tenant','tenant',current_revision,'project','project','created','foundation-live',clock_timestamp()
      FROM next_revision RETURNING resource_version
    ) INSERT INTO cloud_agents.projects (tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,resource_version,created_at,updated_at)
      SELECT 'tenant','tenant','project','project','organization','Foundation live project','active',resource_version,clock_timestamp(),clock_timestamp() FROM project_change;
    INSERT INTO cloud_agents.deployment_targets (
      tenant_id,tenant_ref_id,project_uid,target_uid,target_name,target_kind,endpoint,credential_ref,generation,
      observed_phase,api_version,engine_version,target_os,target_arch,stable_error_code,last_probe_at,
      resource_version,create_idempotency_key,create_request_digest,created_at,updated_at
    ) VALUES ('tenant','tenant','project','target','target','docker','https://127.0.0.1:1','fixture-only',1,
      'ready','1.52','29.4.0','linux','arm64','',clock_timestamp(),1,'foundation-live-target-key',
      'sha256:${"d".repeat(64)}',clock_timestamp(),clock_timestamp());`);

  const serverOutput = execFileSync(
    serverTestBinary,
    ["-test.run", "^TestFoundationRuntimeProfilePostgres$", "-test.v"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
        CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
        CLOUD_AGENTS_FOUNDATION_SUCCESS_IMAGE_URI: sandboxImage,
        CLOUD_AGENTS_FOUNDATION_FAILURE_IMAGE_URI: failureImage,
      },
      timeout: 120_000,
    },
  );
  assert.ok(serverOutput.includes("--- PASS: TestFoundationRuntimeProfilePostgres"));

  const configPath = resolve(build, "opensandbox.toml");
  writeFileSync(
    configPath,
    `[server]\nhost="0.0.0.0"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${execdImage}"\n[docker]\nnetwork_mode="bridge"\nhost_ip="127.0.0.1"\nport_range_min=49310\nport_range_max=49520\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
    { mode: 0o600 },
  );
  docker(
    "run",
    "-d",
    "--name",
    sandboxServerName,
    "--label",
    `cloud-agents-foundation-controller-test=${run}`,
    "-p",
    "127.0.0.1::8080",
    "--mount",
    `type=bind,src=${configPath},dst=/etc/opensandbox/config.toml,readonly`,
    "--mount",
    "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
    serverImage,
  );
  sandboxServerStarted = true;
  sandboxBase = `http://127.0.0.1:${mappedPort(sandboxServerName, 8080)}`;
  for (let attempt = 0; ; attempt++) {
    try {
      if ((await fetch(sandboxBase + "/health", { signal: AbortSignal.timeout(1000) })).ok) break;
    } catch {}
    if (attempt === 300) throw new Error("OpenSandbox did not start");
    await delay(100);
  }
  const credentialDirectory = resolve(build, "credentials");
  const fixtureCredentialDirectory = resolve(credentialDirectory, "fixture-only");
  mkdirSync(fixtureCredentialDirectory, { recursive: true, mode: 0o700 });
  writeFileSync(
    resolve(fixtureCredentialDirectory, "opensandbox.json"),
    JSON.stringify({ endpoint: sandboxBase, apiKey }) + "\n",
    { mode: 0o600 },
  );

  const commonEnvironment = {
    ...process.env,
    CLOUD_AGENTS_FOUNDATION_LIVE_DATABASE_URL: runtimeURL,
    CLOUD_AGENTS_FOUNDATION_LIVE_OWNER_DATABASE_URL: migrationURL,
    CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_ENDPOINT: sandboxBase,
    CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_KEY: apiKey,
    CLOUD_AGENTS_FOUNDATION_LIVE_DOCKER_SOCKET: dockerSocket,
  };
  const prepareOutput = execFileSync(
    controllerTestBinary,
    ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
    {
      encoding: "utf8",
      env: { ...commonEnvironment, CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "prepare" },
      timeout: 180_000,
    },
  );
  prepareReceipt = parseMarker(prepareOutput, "FOUNDATION_LIVE_PREPARE");
  await delay(1200);
  const recoverOutput = execFileSync(
    controllerTestBinary,
    ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
    {
      encoding: "utf8",
      env: {
        ...commonEnvironment,
        CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "recover",
        CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID: prepareReceipt.runtimeId,
        CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME: prepareReceipt.volumeName,
        CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_PROOF_DIGEST: prepareReceipt.proofDigest,
      },
      timeout: 180_000,
    },
  );
  const recoverReceipt = parseMarker(recoverOutput, "FOUNDATION_LIVE_RECOVER");
  assert.equal(recoverReceipt.runtimeId, prepareReceipt.runtimeId);
  assert.equal(
    recoverReceipt.cleanup,
    "failed runtime and volume removed; successful runtime retained for lifecycle",
  );

  const lifecycleAPI = (action) =>
    parseMarker(
      execFileSync(
        serverTestBinary,
        ["-test.run", "^TestFoundationSandboxLifecyclePostgres$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
            CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
            CLOUD_AGENTS_FOUNDATION_LIFECYCLE_ACTION: action,
          },
          timeout: 120_000,
        },
      ),
      "FOUNDATION_LIFECYCLE_API",
    );
  const lifecycleController = (phase, marker, prior = prepareReceipt) =>
    parseMarker(
      execFileSync(
        controllerTestBinary,
        ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...commonEnvironment,
            CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: phase,
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID: prior.runtimeId,
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME: prepareReceipt.volumeName,
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_PROOF_DIGEST: prepareReceipt.proofDigest,
            CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_OPERATION_ID: prior.operationId,
            CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_SPEC_DIGEST: prior.specDigest,
          },
          timeout: 180_000,
        },
      ),
      marker,
    );
  const stopAPIReceipt = lifecycleAPI("stop");
  const stopReceipt = lifecycleController("stop", "FOUNDATION_LIVE_STOP");
  assert.equal(stopReceipt.generation, stopAPIReceipt.generation);
  assert.equal(stopReceipt.workspaceVolume, prepareReceipt.volumeName);
  const rebuildAPIReceipt = lifecycleAPI("rebuild");
  const rebuildReceipt = lifecycleController("rebuild", "FOUNDATION_LIVE_REBUILD");
  assert.equal(rebuildReceipt.generation, rebuildAPIReceipt.generation);
  assert.equal(rebuildReceipt.workspaceDigest, prepareReceipt.proofDigest);
  assert.equal(rebuildReceipt.lifecycleTrigger, "manual");
  const execReceipt = parseMarker(
    execFileSync(
      serverTestBinary,
      ["-test.run", "^TestFoundationSandboxExecPostgres$", "-test.v"],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
          CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          CLOUD_AGENTS_FOUNDATION_ACCESS_CREDENTIAL_DIRECTORY: credentialDirectory,
          CLOUD_AGENTS_FOUNDATION_EXPECTED_PROOF_DIGEST: prepareReceipt.proofDigest,
        },
        timeout: 120_000,
      },
    ),
    "FOUNDATION_EXEC_API",
  );
  assert.equal(execReceipt.generation, rebuildReceipt.generation);
  assert.equal(execReceipt.exitCode, 7);
  assert.equal(execReceipt.proofDigestVerified, true);
  assert.ok(execReceipt.executionTimeMillis >= 0 && execReceipt.executionTimeMillis <= 65000);
  assert.equal(execReceipt.adminStatus, 403);
  assert.equal(execReceipt.staleGenerationStatus, 409);
  assert.equal(execReceipt.outputLimitStatus, 413);
  assert.equal(execReceipt.responseInfrastructureRedacted, true);
  assert.notEqual(docker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
  let expired = false;
  for (let attempt = 0; attempt < 150; attempt++) {
    if (
      psql(
        "SELECT expires_at <= clock_timestamp() FROM cloud_agents.sandbox_sessions WHERE tenant_id='tenant' AND sandbox_uid='sandbox';",
      ) === "t"
    ) {
      expired = true;
      break;
    }
    await delay(500);
  }
  assert.ok(expired, "database TTL did not elapse");
  const ttlReceipt = lifecycleController("ttl", "FOUNDATION_LIVE_TTL", rebuildReceipt);
  assert.equal(ttlReceipt.generation, 4);
  assert.equal(ttlReceipt.lifecycleTrigger, "ttl");
  assert.equal(ttlReceipt.workspaceVolume, prepareReceipt.volumeName);
  const finalRebuildAPIReceipt = lifecycleAPI("rebuild");
  const finalRebuildReceipt = lifecycleController(
    "rebuild-final",
    "FOUNDATION_LIVE_REBUILD_FINAL",
    rebuildReceipt,
  );
  assert.equal(finalRebuildReceipt.generation, finalRebuildAPIReceipt.generation);
  assert.equal(finalRebuildReceipt.workspaceDigest, prepareReceipt.proofDigest);
  assert.equal(docker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
  assert.equal(
    docker(
      "volume",
      "ls",
      "-q",
      "--filter",
      "label=cloud-agents.dev/resource=foundation-workspace",
    ),
    "",
  );

  const evidence = {
    run,
    source: {
      branch: execFileSync("git", ["branch", "--show-current"], {
        cwd: root,
        encoding: "utf8",
      }).trim(),
      head: execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim(),
      dirty: true,
    },
    backend: {
      dockerContext: "orbstack",
      dockerVersion: docker("version", "--format", "{{.Server.Version}}"),
      postgres: psql("SHOW server_version;"),
    },
    candidate: {
      sourceRef: "207d94c7dc7735c143856fe5c6538b743e478786",
      serverImage,
      execdImage,
      sandboxImage,
      failureImage,
    },
    prepare: prepareReceipt,
    recover: recoverReceipt,
    stop: { api: stopAPIReceipt, controller: stopReceipt },
    rebuild: { api: rebuildAPIReceipt, controller: rebuildReceipt },
    exec: execReceipt,
    ttl: ttlReceipt,
    finalRebuild: { api: finalRebuildAPIReceipt, controller: finalRebuildReceipt },
    checks: [
      "public RuntimeProfile and Sandbox admission",
      "generated Admin Sandbox list/detail, ordinary-user 403, and response redaction",
      "real durable claim and physical retained Docker volume",
      "real OpenSandbox create and execd readiness",
      "OS-process exit before settlement",
      "expired claim reap by a new Controller process",
      "exact runtime adoption without duplicate",
      "workspace bytes preserved across Controller restart",
      "real OpenSandbox Failed state and exact compensation",
      "generated Admin stop/rebuild with ordinary-user 403, idempotent replay, stale fencing, Operation and Audit",
      "stop deletes the exact runtime, releases its writer, and retains the physical Workspace volume",
      "rebuild uses the same physical volume and preserves Workspace bytes",
      "generated Product Sandbox Exec uses database-authorized exact generation and physical runtime receipt",
      "real bounded foreground command runs in /workspace and returns exit code, stdout, stderr, and duration",
      "Admin token, stale generation, and combined output above 1 MiB are denied with 403, 409, and 413",
      "Product Exec response omits endpoint, runtime identifier, and credential references",
      "database-clock TTL accepts the same durable Stop authority and records its trigger and Audit",
      "TTL stop deletes compute, releases its writer, and retains the physical Workspace volume",
      "rebuild after TTL expiry restores the same Workspace bytes",
      "stale runtime generation and foreign physical volume ownership are rejected",
      "zero test-owned runtime containers and Workspace volumes",
    ],
    boundary:
      "Local OrbStack Docker and disposable PostgreSQL only; no Admin Web browser, deployment, image publication, Kubernetes or customer node",
  };
  writeFileSync(
    resolve(evidenceDirectory, "evidence.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  process.stdout.write(
    `Verified Controller restart/adoption and failure compensation; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
  );
} finally {
  const ownedRuntimeIDs = new Set(prepareReceipt?.runtimeId ? [prepareReceipt.runtimeId] : []);
  if (postgresStarted) {
    try {
      for (const id of psql(
        "SELECT COALESCE(runtime_uid,'') FROM cloud_agents.sandbox_sessions WHERE runtime_uid IS NOT NULL;",
      )
        .split("\n")
        .filter(Boolean)) {
        ownedRuntimeIDs.add(id);
      }
    } catch {}
  }
  if (sandboxServerStarted) {
    const logs = spawnSync("docker", ["--context", "orbstack", "logs", sandboxServerName], {
      encoding: "utf8",
      timeout: 10_000,
    });
    writeFileSync(
      resolve(evidenceDirectory, "opensandbox.log"),
      `${logs.stdout ?? ""}${logs.stderr ?? ""}`.replaceAll(apiKey, "[REDACTED]"),
      { mode: 0o600 },
    );
    try {
      const page = await request(sandboxBase, "/v1/sandboxes?page=1&pageSize=100");
      if (page.status === 200) {
        for (const item of JSON.parse(page.text).items ?? []) {
          if (
            item?.id === prepareReceipt?.runtimeId ||
            item?.metadata?.["cloud-agents-receipt-version"] === "2"
          ) {
            await request(sandboxBase, `/v1/sandboxes/${item.id}`, "DELETE");
          }
        }
      }
    } catch {}
  }
  for (const container of docker("ps", "-aq", "--filter", "label=opensandbox.io/id")
    .split("\n")
    .filter(Boolean)) {
    const runtimeID = docker(
      "inspect",
      "--format",
      '{{index .Config.Labels "opensandbox.io/id"}}',
      container,
    );
    if (ownedRuntimeIDs.has(runtimeID)) docker("rm", "-f", container);
  }
  for (const volume of docker(
    "volume",
    "ls",
    "-q",
    "--filter",
    "label=cloud-agents.dev/resource=foundation-workspace",
  )
    .split("\n")
    .filter(Boolean)) {
    const info = JSON.parse(docker("volume", "inspect", volume))[0];
    if (
      info.Labels?.["cloud-agents.dev/tenant"] === "tenant" &&
      info.Labels?.["cloud-agents.dev/project"] === "project" &&
      ["workspace", "workspace-terminal", "workspace-foreign"].includes(
        info.Labels?.["cloud-agents.dev/workspace"],
      )
    ) {
      docker("volume", "rm", volume);
    }
  }
  if (sandboxServerStarted) docker("rm", "-f", "-v", sandboxServerName);
  if (postgresStarted) docker("rm", "-f", "-v", postgresName);
  rmSync(build, { recursive: true, force: true });
  assert.equal(
    docker("ps", "-aq", "--filter", `label=cloud-agents-foundation-controller-test=${run}`),
    "",
  );
}
