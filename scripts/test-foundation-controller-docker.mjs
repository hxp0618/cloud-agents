import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { isIP } from "node:net";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [output] = process.argv.slice(2);
assert.ok(output, "usage: node scripts/test-foundation-controller-docker.mjs NEW_OUTPUT_DIRECTORY");
const remoteWorkerOnly = process.argv.includes("--remote-worker-only");
const remoteWorkerComplete = Symbol("remote-worker-complete");
const root = resolve(import.meta.dirname, "..");
const currentHead = readdirSync(resolve(root, "services/control-plane/migrations/product"))
  .filter((entry) => /^\d{6}$/.test(entry))
  .sort()
  .at(-1);
assert.ok(currentHead, "product migration package is required");
const evidenceDirectory = resolve(output);
mkdirSync(evidenceDirectory, { mode: 0o700 });
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-foundation-controller-"));
const run = `foundation-controller-${randomUUID()}`;
const postgresName = `${run}-postgres`;
const sandboxServerName = `${run}-opensandbox`;
const allowedSinkName = `${run}-allowed-sink`;
const blockedSinkName = `${run}-blocked-sink`;
const sandboxServerPort = 18891;
const serverImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb";
const execdImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd";
const egressImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a";
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
let remoteWorkerReceipt;
const startedSinks = [];
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
    egressImage,
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
  const dockerGateway = docker(
    "network",
    "inspect",
    "bridge",
    "--format",
    "{{(index .IPAM.Config 0).Gateway}}",
  );
  assert.equal(isIP(dockerGateway), 4);

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
  const remoteWorkerBinary = resolve(build, "cloud-agents-remote-worker");
  goBuild(migrateBinary, "./services/control-plane/cmd/cloud-agents-product-migrate");
  goBuild(serverTestBinary, "./services/control-plane/internal/server", true);
  goBuild(controllerTestBinary, "./services/control-plane/internal/foundationcontroller", true);
  goBuild(remoteWorkerBinary, "./services/control-plane/cmd/cloud-agents-remote-worker");
  const migration = JSON.parse(
    execFileSync(
      migrateBinary,
      [
        "--repository-root",
        root,
        "--manifest",
        `services/control-plane/migrations/product/${currentHead}/manifest.json`,
        "--selector",
        `product-${currentHead}`,
      ],
      {
        encoding: "utf8",
        env: { ...process.env, CLOUD_AGENTS_PLATFORM_DATABASE_URL: migrationURL },
        timeout: 180_000,
      },
    ),
  );
  assert.equal(migration.schema_head, currentHead);

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

  for (const name of [allowedSinkName, blockedSinkName]) {
    docker(
      "run",
      "-d",
      "--name",
      name,
      "--label",
      `cloud-agents-foundation-controller-test=${run}`,
      sandboxImage,
      "node",
      "-e",
      'require("http").createServer((request,response)=>response.end("allowed\\n")).listen(8080,"0.0.0.0")',
    );
    startedSinks.push(name);
  }
  await delay(200);
  const allowedSinkIP = docker(
    "inspect",
    "--format",
    "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
    allowedSinkName,
  );
  const blockedSinkIP = docker(
    "inspect",
    "--format",
    "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
    blockedSinkName,
  );
  assert.equal(isIP(allowedSinkIP), 4);
  assert.equal(isIP(blockedSinkIP), 4);
  for (const address of [allowedSinkIP, blockedSinkIP]) {
    assert.equal(
      docker(
        "run",
        "--rm",
        sandboxImage,
        "node",
        "-e",
        `fetch("http://${address}:8080").then(async response=>{if(!response.ok||await response.text()!=="allowed\\n")process.exit(1)}).catch(()=>process.exit(1))`,
      ),
      "",
    );
  }

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
        CLOUD_AGENTS_FOUNDATION_ALLOWED_EGRESS: `${allowedSinkIP}/32`,
      },
      timeout: 120_000,
    },
  );
  assert.ok(serverOutput.includes("--- PASS: TestFoundationRuntimeProfilePostgres"));

  const configPath = resolve(build, "opensandbox.toml");
  writeFileSync(
    configPath,
    `[server]\nhost="0.0.0.0"\neip="127.0.0.1"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${execdImage}"\n[docker]\nnetwork_mode="bridge"\nhost_ip="${dockerGateway}"\nport_range_min=49310\nport_range_max=49520\n[egress]\nimage="${egressImage}"\nmode="dns+nft"\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
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
    `127.0.0.1:${sandboxServerPort}:8080`,
    "--mount",
    `type=bind,src=${configPath},dst=/etc/opensandbox/config.toml,readonly`,
    "--mount",
    "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
    serverImage,
  );
  sandboxServerStarted = true;
  sandboxBase = `http://127.0.0.1:${sandboxServerPort}`;
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

  remoteWorkerReceipt = parseMarker(
    execFileSync(
      serverTestBinary,
      ["-test.run", "^TestRemoteWorkerEnrollmentPostgres$", "-test.v"],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
          CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          CLOUD_AGENTS_REMOTE_WORKER_BINARY: remoteWorkerBinary,
          CLOUD_AGENTS_REMOTE_WORKER_DOCKER_SOCKET: dockerSocket,
          CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY: credentialDirectory,
          CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF: "fixture-only",
          CLOUD_AGENTS_REMOTE_WORKER_SUCCESS_IMAGE_URI: sandboxImage,
          CLOUD_AGENTS_REMOTE_WORKER_ALLOWED_EGRESS: `${allowedSinkIP}/32`,
        },
        timeout: 180_000,
      },
    ),
    "REMOTE_WORKER_SANDBOX",
  );
  assert.equal(remoteWorkerReceipt.receiptReplay, true);
  assert.equal(remoteWorkerReceipt.stopReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.rebuildReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.cleanupStopReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.capabilityAdmissionCases, 7);
  assert.equal(remoteWorkerReceipt.incompatibleProfileHidden, true);
  assert.equal(remoteWorkerReceipt.incompatibleCommandBlocked, true);
  assert.equal(remoteWorkerReceipt.capacityReservationRejected, true);
  assert.equal(remoteWorkerReceipt.regionId, "region-local");
  assert.equal(remoteWorkerReceipt.resourcePoolId, "pool-remote-worker");
  assert.equal(remoteWorkerReceipt.longClaimRenewed, true);
  assert.equal(remoteWorkerReceipt.renewWrongCommandStatus, 409);
  assert.equal(remoteWorkerReceipt.reconnectOriginalCommandNotReplayed, true);
  assert.equal(remoteWorkerReceipt.reconnectAttempt, 2);
  assert.equal(remoteWorkerReceipt.execExitCode, 7);
  assert.equal(remoteWorkerReceipt.execWorkspaceDigestVerified, true);
  assert.equal(remoteWorkerReceipt.execRequestReplay, true);
  assert.equal(remoteWorkerReceipt.execRequestConflictStatus, 409);
  assert.equal(remoteWorkerReceipt.execReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.execReceiptConflictStatus, 409);
  assert.equal(remoteWorkerReceipt.execAdminStatus, 403);
  assert.equal(remoteWorkerReceipt.execStaleGenerationStatus, 409);
  assert.equal(remoteWorkerReceipt.execIncarnationBound, true);
  assert.equal(remoteWorkerReceipt.execCertificateBound, true);
  assert.equal(remoteWorkerReceipt.execContentTableHidden, true);
  assert.equal(remoteWorkerReceipt.fileBytes, 1900000);
  assert.equal(remoteWorkerReceipt.fileLargeHeartbeatResponse, true);
  assert.equal(remoteWorkerReceipt.fileGatewayRestart, true);
  assert.equal(remoteWorkerReceipt.fileWrongTokenStatus, 403);
  assert.equal(remoteWorkerReceipt.fileCrossTenantStatus, 403);
  assert.equal(remoteWorkerReceipt.fileSymlinkStatus, 409);
  assert.equal(remoteWorkerReceipt.fileAfterDeleteStatus, 404);
  assert.equal(remoteWorkerReceipt.fileAuthorityMismatchStatus, 409);
  assert.equal(remoteWorkerReceipt.fileReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.fileReceiptConflictStatus, 409);
  assert.equal(remoteWorkerReceipt.fileAdminStatus, 403);
  assert.equal(remoteWorkerReceipt.fileAccessCount, 7);
  assert.equal(remoteWorkerReceipt.fileFailureCount, 2);
  assert.equal(remoteWorkerReceipt.fileAdminRedacted, true);
  assert.equal(remoteWorkerReceipt.fileIncarnationBound, true);
  assert.equal(remoteWorkerReceipt.fileCertificateBound, true);
  assert.equal(remoteWorkerReceipt.fileReceiptsSettled, true);
  assert.equal(remoteWorkerReceipt.fileContentTableHidden, true);
  assert.equal(remoteWorkerReceipt.previewCommandCount, 2);
  assert.equal(remoteWorkerReceipt.previewGatewayRestart, true);
  assert.equal(remoteWorkerReceipt.previewWrongTokenStatus, 403);
  assert.equal(remoteWorkerReceipt.previewCrossTenantStatus, 403);
  assert.equal(remoteWorkerReceipt.previewUnregisteredStatus, 404);
  assert.equal(remoteWorkerReceipt.previewInternalPortStatus, 404);
  assert.equal(remoteWorkerReceipt.previewAfterRevokeStatus, 404);
  assert.equal(remoteWorkerReceipt.previewAuthorityMismatchStatus, 409);
  assert.equal(remoteWorkerReceipt.previewReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.previewReceiptConflictStatus, 409);
  assert.equal(remoteWorkerReceipt.previewAdminRedacted, true);
  assert.equal(remoteWorkerReceipt.previewIncarnationBound, true);
  assert.equal(remoteWorkerReceipt.previewCertificateBound, true);
  assert.equal(remoteWorkerReceipt.previewReceiptsSettled, true);
  assert.equal(remoteWorkerReceipt.previewContentTableHidden, true);
  assert.ok(remoteWorkerReceipt.ptyOutputOffset > 0);
  assert.equal(remoteWorkerReceipt.ptyGatewayRestart, true);
  assert.equal(remoteWorkerReceipt.ptyCursorReplay, true);
  assert.equal(remoteWorkerReceipt.ptyWrongTokenStatus, 403);
  assert.equal(remoteWorkerReceipt.ptyCrossTenantStatus, 403);
  assert.equal(remoteWorkerReceipt.ptyAuthorityMismatchStatus, 409);
  assert.equal(remoteWorkerReceipt.ptyReceiptReplay, true);
  assert.equal(remoteWorkerReceipt.ptyReceiptConflictStatus, 409);
  assert.equal(remoteWorkerReceipt.ptyAfterDeleteStatus, 403);
  assert.equal(remoteWorkerReceipt.ptySessionCount, 1);
  assert.equal(remoteWorkerReceipt.ptyAdminRedacted, true);
  assert.equal(remoteWorkerReceipt.ptyCommandCount, 7);
  assert.equal(remoteWorkerReceipt.ptyIncarnationBound, true);
  assert.equal(remoteWorkerReceipt.ptyCertificateBound, true);
  assert.equal(remoteWorkerReceipt.ptyReceiptsSettled, true);
  assert.equal(remoteWorkerReceipt.ptyContentTableHidden, true);
  assert.equal(remoteWorkerReceipt.sshExitCode, 7);
  assert.equal(remoteWorkerReceipt.sshGatewayRestart, true);
  assert.equal(remoteWorkerReceipt.sshWrongPassword, true);
  assert.equal(remoteWorkerReceipt.sshCrossTenant, true);
  assert.equal(remoteWorkerReceipt.sshCapabilityRejected, true);
  assert.equal(remoteWorkerReceipt.sshDirectTCPIPRejected, true);
  assert.equal(remoteWorkerReceipt.sshEnvironmentRejected, true);
  assert.ok(remoteWorkerReceipt.sshCommandCount >= 6);
  assert.ok(remoteWorkerReceipt.sshInputCommandCount >= 2);
  assert.equal(remoteWorkerReceipt.sshAdminRedacted, true);
  assert.equal(remoteWorkerReceipt.sshIncarnationBound, true);
  assert.equal(remoteWorkerReceipt.sshCertificateBound, true);
  assert.equal(remoteWorkerReceipt.sshReceiptsSettled, true);
  assert.equal(remoteWorkerReceipt.sshShapeBound, true);
  assert.equal(remoteWorkerReceipt.sshPTYBound, true);
  assert.equal(remoteWorkerReceipt.sshNonPTYBound, true);
  assert.equal(remoteWorkerReceipt.sshContentTableHidden, true);
  assert.match(remoteWorkerReceipt.workspaceDigest, /^[0-9a-f]{64}\s+/u);
  assert.equal(remoteWorkerReceipt.observedState, "stopped");
  for (const runtimeId of [remoteWorkerReceipt.runtimeId, remoteWorkerReceipt.rebuiltRuntimeId]) {
    for (let attempt = 0; ; attempt++) {
      if (docker("ps", "-aq", "--filter", `label=opensandbox.io/id=${runtimeId}`) === "") break;
      if (attempt === 100) throw new Error("RemoteWorker Sandbox runtime did not terminate");
      await delay(100);
    }
  }
  docker("volume", "rm", remoteWorkerReceipt.volumeName);
  if (remoteWorkerOnly) {
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
        schemaHead: migration.schema_head,
      },
      remoteWorker: remoteWorkerReceipt,
      checks: [
        "generated Admin APIs reject RemoteWorker Profile publication for unsupported runtime, architecture, storage, network, CPU, memory and disk declarations",
        "generated User API hides incompatible published Profiles and the database claim path blocks lifecycle delivery after capability drift",
        "database-authoritative region-local and pool-remote-worker placement reports aggregate CPU, memory and retained Workspace volume reservations",
        "an individually valid second Sandbox is hidden and rejected when the existing real Sandbox exhausts aggregate node capacity, and its Workspace transaction leaves no residue",
        "generated User Exec API queues only the database-authorized exact RemoteWorker Sandbox generation",
        "authenticated outbound worker receives the command over its existing mTLS heartbeat and executes it in /workspace",
        "non-zero exit, stdout, stderr and duration settle through the public response without infrastructure fields",
        "command delivery is bound to the current incarnation and certificate and accepts only exact request and receipt replay",
        "runtime role cannot read the command/output table directly",
        "the unchanged generated Files API uses the same short-lived Grant and queues only database-authorized RemoteWorker work",
        "a 1.9 MiB real write crosses the former 2 MiB heartbeat response limit and survives list plus version-bound paged reads across Gateway restart",
        "wrong-token, cross-tenant, symlink traversal and read-after-delete paths return 403, 403, 409 and 404",
        "file receipts are bound to the exact command path plus incarnation/certificate, are exact-replay only, and the runtime role cannot read path or content rows",
        "Admin Grant diagnostics contain only counters and stable errors, never file paths or content",
        "the unchanged Preview API and Access Gateway carry bounded HTTP requests through outbound heartbeat commands without exposing a node endpoint",
        "Preview strips credentials, forwarding and Set-Cookie headers, survives Gateway restart, and rejects wrong-token, cross-tenant, unregistered, internal and revoked access",
        "Preview receipts are bound to the exact incarnation and certificate, are exact-replay only, and neither runtime nor Admin can read request or response content",
        "the unchanged PTY API and Access Gateway carry real WebSocket frames through outbound heartbeat commands without exposing a node endpoint",
        "PTY survives Gateway restart, replays from an absolute cursor, and rejects wrong-token, cross-tenant, authority-mismatched, changed-receipt and deleted-session access",
        "PTY commands are bound to the current incarnation and certificate, the runtime role cannot read frame rows, and Admin sees only the session count",
        "the existing SSH Gateway carries PTY and non-PTY sessions through bounded RemoteWorker commands without a browser or inbound customer-node connection",
        "SSH rejects missing node capability, wrong password, cross-tenant username, direct-tcpip forwarding and environment mutation, preserves exit status, and survives Gateway restart",
        "SSH command delivery is bound to the exact incarnation and certificate, runtime cannot read command rows, and Admin receives metadata without command content or credentials",
        "a lifecycle effect longer than the original claim renews through the existing mTLS heartbeat, and a wrong command ID is rejected",
        "a dropped outbound heartbeat clears the uncertain local command; after natural claim expiry, reconnect executes only a new reconciled attempt",
        "create, stop, retained-volume rebuild and final cleanup leave zero test-owned runtime containers and Workspace volumes",
      ],
      boundary:
        "Local OrbStack Docker RemoteWorker customer node and disposable PostgreSQL only; one database-authoritative Region/Pool/Node and aggregate CPU/memory/fixed-20-GiB Workspace reservation are covered, while deterministic multi-node selection, image-manifest architecture, strong-isolation runtime, streaming Preview, Kubernetes and external SSH customer nodes are not covered",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified RemoteWorker Sandbox Exec, Files, Preview, PTY, SSH and cleanup; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw remoteWorkerComplete;
  }

  const commonEnvironment = {
    ...process.env,
    CLOUD_AGENTS_FOUNDATION_LIVE_DATABASE_URL: runtimeURL,
    CLOUD_AGENTS_FOUNDATION_LIVE_OWNER_DATABASE_URL: migrationURL,
    CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_ENDPOINT: sandboxBase,
    CLOUD_AGENTS_FOUNDATION_LIVE_OPENSANDBOX_KEY: apiKey,
    CLOUD_AGENTS_FOUNDATION_LIVE_DOCKER_SOCKET: dockerSocket,
    CLOUD_AGENTS_FOUNDATION_LIVE_ALLOWED_IP: allowedSinkIP,
    CLOUD_AGENTS_FOUNDATION_LIVE_BLOCKED_IP: blockedSinkIP,
    CLOUD_AGENTS_FOUNDATION_LIVE_DOCKER_GATEWAY: dockerGateway,
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
  const ptyReceipt = parseMarker(
    execFileSync(
      serverTestBinary,
      ["-test.run", "^TestFoundationSandboxAccessGrantPTYPostgres$", "-test.v"],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
          CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          CLOUD_AGENTS_FOUNDATION_ACCESS_CREDENTIAL_DIRECTORY: credentialDirectory,
        },
        timeout: 180_000,
      },
    ),
    "FOUNDATION_PTY_API",
  );
  assert.equal(ptyReceipt.generation, rebuildReceipt.generation);
  assert.equal(ptyReceipt.wrongTokenStatus, 403);
  assert.equal(ptyReceipt.crossTenantStatus, 403);
  assert.equal(ptyReceipt.revokedStatus, 403);
  assert.equal(ptyReceipt.expiredStatus, 403);
  assert.equal(ptyReceipt.activeConnectionRevoked, true);
  assert.equal(ptyReceipt.adminContentRedacted, true);
  assert.equal(ptyReceipt.fileGatewayRestart, true);
  assert.equal(ptyReceipt.filePages, 2);
  assert.equal(ptyReceipt.fileWrongTokenStatus, 403);
  assert.equal(ptyReceipt.fileCrossTenantStatus, 403);
  assert.equal(ptyReceipt.fileTraversalStatus, 400);
  assert.equal(ptyReceipt.fileSymlinkStatus, 409);
  assert.equal(ptyReceipt.fileOversizedStatus, 413);
  assert.equal(ptyReceipt.fileDeletedStatus, 404);
  assert.equal(ptyReceipt.fileAccessCount, 7);
  assert.equal(ptyReceipt.fileFailureCount, 2);
  assert.equal(ptyReceipt.previewPort, 3000);
  assert.equal(ptyReceipt.previewPrivate, true);
  assert.equal(ptyReceipt.previewGatewayRestart, true);
  assert.equal(ptyReceipt.previewWrongTokenStatus, 403);
  assert.equal(ptyReceipt.previewCrossTenantStatus, 403);
  assert.equal(ptyReceipt.previewUnregisteredStatus, 404);
  assert.equal(ptyReceipt.previewInternalPortStatus, 404);
  assert.equal(ptyReceipt.previewHeadersRedacted, true);
  assert.equal(ptyReceipt.previewActiveResponseRevoked, true);
  assert.equal(ptyReceipt.previewRevokedPortStatus, 404);
  assert.equal(ptyReceipt.previewRevokedGrantStatus, 403);
  assert.equal(ptyReceipt.previewExpiredGrantStatus, 403);
  assert.equal(ptyReceipt.previewPolicyDisabledStatus, 403);
  assert.equal(ptyReceipt.sshFixedRoute, true);
  assert.equal(ptyReceipt.sshWrongPasswordDenied, true);
  assert.equal(ptyReceipt.sshCrossTenantDenied, true);
  assert.equal(ptyReceipt.sshForwardingDenied, true);
  assert.equal(ptyReceipt.sshEnvironmentDenied, true);
  assert.equal(ptyReceipt.sshGatewayRestart, true);
  assert.equal(ptyReceipt.sshActiveConnectionRevoked, true);
  assert.equal(ptyReceipt.sshRevokedDenied, true);
  assert.equal(ptyReceipt.sshExpiredDenied, true);
  assert.equal(ptyReceipt.sshOldGenerationDenied, true);
  assert.ok(ptyReceipt.boundedOutputOffset >= 1_100_000);
  assert.ok(ptyReceipt.boundedReplayOffset > 0);
  assert.ok(ptyReceipt.boundedReplayBytes <= 1 << 20);
  assert.notEqual(docker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
  psql(
    "SET ROLE cloud_agents_migration_owner; UPDATE cloud_agents.sandbox_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE tenant_id='tenant' AND project_uid='project' AND sandbox_uid='sandbox';",
    "foundation_migration",
  );
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
      egressImage,
      sandboxImage,
      failureImage,
    },
    prepare: prepareReceipt,
    remoteWorker: remoteWorkerReceipt,
    recover: recoverReceipt,
    stop: { api: stopAPIReceipt, controller: stopReceipt },
    rebuild: { api: rebuildAPIReceipt, controller: rebuildReceipt },
    exec: execReceipt,
    pty: ptyReceipt,
    ttl: ttlReceipt,
    finalRebuild: { api: finalRebuildAPIReceipt, controller: finalRebuildReceipt },
    checks: [
      "public RuntimeProfile and Sandbox admission",
      "outbound customer-node RemoteWorker claims and physically creates a real Sandbox through the existing durable Operation",
      "RemoteWorker settlement and exact receipt replay persist Running state without endpoint or credential bytes in the command",
      "Admin Stop dispatches the exact prior runtime and retained Workspace volume to the authenticated RemoteWorker",
      "RemoteWorker physically deletes compute, settles Stopped with writer release, and accepts exact Stop receipt replay",
      "Admin Rebuild dispatches the retained Workspace volume and creates a new customer-node runtime",
      "RemoteWorker Rebuild preserves Workspace bytes, settles Running, accepts exact receipt replay, and is stopped without compute residue",
      "generated Admin Sandbox list/detail, ordinary-user 403, and response redaction",
      "real durable claim and physical retained Docker volume",
      "real OpenSandbox create and execd readiness",
      "RuntimeProfile Network Policy is sent to OpenSandbox and verified through the authenticated egress sidecar as dns+nft",
      "restricted policy reaches only the approved sink and blocks a second container, the Docker host, and metadata IP",
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
      "short-lived Sandbox Grant is idempotent, generation-bound, database persisted, expiring, and revocable",
      "standalone Access Gateway allows only fixed PTY, Files, private Preview, and SSH Sandbox routes and denies wrong-token and cross-tenant requests",
      "PTY survives Gateway restart and reconnects from an absolute cursor without output loss",
      "fixed candidate replay buffer remains bounded to 1 MiB after more than 1.1 MiB output",
      "Files writes, lists, and reads two version-bound pages across Gateway restart inside /workspace",
      "Files rejects path traversal, symlink traversal, oversized input, and reads after deletion",
      "private Preview registers one explicit non-internal port and proxies only the fixed candidate server route",
      "Preview strips Grant and Cookie headers, survives Gateway restart, and rejects unregistered and internal ports",
      "Network Policy disables new Preview port registration with 403",
      "Preview port revoke closes an active response; port, Grant, and expiry revocation reject subsequent access",
      "real SSH protocol authenticates with the short-lived Grant, pins the server host key, and runs only in the fixed /workspace Sandbox route",
      "SSH rejects direct-tcpip forwarding, environment mutation, wrong password, cross-tenant identity, expiry, revoke, and old generation",
      "SSH reconnects after Gateway restart and Grant revoke closes an active transport",
      "revocation closes an active PTY connection and rejects reconnects; expired Grants reject new sessions",
      "Admin Grant metadata includes PTY/SSH, file, and active Preview port diagnostics but excludes tokens, SSH usernames, terminal/file/Preview content, paths, endpoints, proxy paths, and credential references",
      "database-clock TTL accepts the same durable Stop authority and records its trigger and Audit",
      "TTL stop deletes compute, releases its writer, and retains the physical Workspace volume",
      "rebuild after TTL expiry restores the same Workspace bytes",
      "stale runtime generation and foreign physical volume ownership are rejected",
      "zero test-owned runtime containers and Workspace volumes",
    ],
    boundary:
      "Local OrbStack Docker customer-node process and disposable PostgreSQL only; Admin Web is build-tested but no browser visual run is included; no deployment, image publication, Kubernetes or SSH node",
  };
  writeFileSync(
    resolve(evidenceDirectory, "evidence.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  process.stdout.write(
    `Verified Controller restart/adoption and failure compensation; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
  );
} catch (error) {
  if (error !== remoteWorkerComplete) throw error;
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
      ["workspace", "workspace-terminal", "workspace-foreign", "remote-workspace"].includes(
        info.Labels?.["cloud-agents.dev/workspace"],
      )
    ) {
      docker("volume", "rm", volume);
    }
  }
  if (sandboxServerStarted) docker("rm", "-f", "-v", sandboxServerName);
  for (const sink of startedSinks) docker("rm", "-f", sink);
  if (postgresStarted) docker("rm", "-f", "-v", postgresName);
  rmSync(build, { recursive: true, force: true });
  assert.equal(
    docker("ps", "-aq", "--filter", `label=cloud-agents-foundation-controller-test=${run}`),
    "",
  );
}
