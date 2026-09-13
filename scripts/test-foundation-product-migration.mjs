import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { createHash, randomUUID } from "node:crypto";

// Real product migration runner against one owned, network-disabled PostgreSQL.
const root = resolve(import.meta.dirname, "..");
const productHeads = readdirSync(resolve(root, "services/control-plane/migrations/product"))
  .filter((entry) => /^\d{6}$/.test(entry))
  .sort();
const currentHead = productHeads.at(-1);
const previousHead = productHeads.at(-2);
assert.ok(currentHead && previousHead, "at least two product migration packages are required");
const migrationCount = Number.parseInt(currentHead, 10);
const name = `foundation-migrate-${randomUUID()}`;
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-foundation-migrate-"));
const binary = resolve(build, "cloud-agents-product-migrate");
const testBinary = resolve(build, "localmigration.test");
const serverTestBinary = resolve(build, "server.test");
const postgresTestBinary = resolve(build, "postgres.test");
const remoteWorkerBinary = resolve(build, "cloud-agents-remote-worker");
const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 120_000,
  }).trim();
const psql = (query, user = "postgres", database = "postgres") =>
  execFileSync(
    "docker",
    [
      "--context",
      "orbstack",
      "exec",
      "-i",
      name,
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
let started = false;
try {
  docker("image", "inspect", "postgres:17.6-bookworm");
  docker(
    "run",
    "-d",
    "--name",
    name,
    "--label",
    `cloud-agents-foundation-migration-test=${name}`,
    "--network",
    "none",
    "--mount",
    `type=bind,src=${root},dst=/repo,readonly`,
    "-e",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "postgres:17.6-bookworm",
  );
  started = true;
  for (let attempt = 0; ; attempt++) {
    try {
      assert.equal(psql("SELECT 1"), "1");
      await delay(100);
      assert.equal(psql("SELECT 1"), "1");
      break;
    } catch {
      if (attempt === 50) throw new Error("PostgreSQL did not start");
      await delay(100);
    }
  }
  psql(readFileSync(resolve(root, "services/control-plane/migrations/bootstrap/roles.sql")));
  psql("CREATE DATABASE foundation_upgrade");
  psql("CREATE DATABASE foundation_fresh");
  psql(`CREATE ROLE foundation_migration LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE foundation_runtime LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE foundation_bootstrap LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT cloud_agents_migration_owner TO foundation_migration;
GRANT cloud_agents_runtime TO foundation_runtime;
GRANT cloud_agents_bootstrap_admin TO foundation_bootstrap;
GRANT CREATE ON DATABASE foundation_upgrade TO cloud_agents_migration_owner;
GRANT CREATE ON DATABASE foundation_fresh TO cloud_agents_migration_owner;`);

  const architecture = docker("info", "--format", "{{.Architecture}}");
  const goarch = new Map([
    ["aarch64", "arm64"],
    ["arm64", "arm64"],
    ["x86_64", "amd64"],
    ["amd64", "amd64"],
  ]).get(architecture);
  assert.ok(goarch, `unsupported Docker architecture: ${architecture}`);
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-o",
      binary,
      "./services/control-plane/cmd/cloud-agents-product-migrate",
    ],
    {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch },
      timeout: 120_000,
    },
  );
  execFileSync(
    "go",
    ["test", "-c", "-o", testBinary, "./services/control-plane/internal/localmigration"],
    {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch },
      timeout: 120_000,
    },
  );
  execFileSync(
    "go",
    ["test", "-c", "-o", serverTestBinary, "./services/control-plane/internal/server"],
    {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch },
      timeout: 120_000,
    },
  );
  execFileSync(
    "go",
    ["test", "-c", "-o", postgresTestBinary, "./services/control-plane/internal/store/postgres"],
    {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch },
      timeout: 120_000,
    },
  );
  execFileSync(
    "go",
    [
      "build",
      "-trimpath",
      "-o",
      remoteWorkerBinary,
      "./services/control-plane/cmd/cloud-agents-remote-worker",
    ],
    {
      cwd: root,
      env: { ...process.env, CGO_ENABLED: "0", GOOS: "linux", GOARCH: goarch },
      timeout: 120_000,
    },
  );
  docker("cp", binary, `${name}:/tmp/cloud-agents-product-migrate`);
  docker("cp", testBinary, `${name}:/tmp/localmigration.test`);
  docker("cp", serverTestBinary, `${name}:/tmp/server.test`);
  docker("cp", postgresTestBinary, `${name}:/tmp/postgres.test`);
  docker("cp", remoteWorkerBinary, `${name}:/tmp/cloud-agents-remote-worker`);
  const migrate = (head, database) => {
    const output = docker(
      "exec",
      "-e",
      `CLOUD_AGENTS_PLATFORM_DATABASE_URL=postgres://foundation_migration@127.0.0.1/${database}?sslmode=disable`,
      name,
      "/tmp/cloud-agents-product-migrate",
      "--repository-root",
      "/repo",
      "--manifest",
      `services/control-plane/migrations/product/${head}/manifest.json`,
      "--selector",
      `product-${head}`,
    );
    return JSON.parse(output);
  };
  const upgradeOutput = docker(
    "exec",
    "-e",
    "CLOUD_AGENTS_FOUNDATION_MIGRATION_UPGRADE_DATABASE_URL=postgres://foundation_migration@127.0.0.1/foundation_upgrade?sslmode=disable",
    "-e",
    "CLOUD_AGENTS_FOUNDATION_MIGRATION_REPOSITORY_ROOT=/repo",
    name,
    "/tmp/localmigration.test",
    "-test.run",
    "^TestFoundationProductUpgradePostgres$",
    "-test.v",
  );
  assert.ok(upgradeOutput.includes("--- PASS: TestFoundationProductUpgradePostgres"));
  const fresh = migrate(currentHead, "foundation_fresh");
  assert.deepEqual(
    { applied: fresh.applied, no_op: fresh.no_op, schema_head: fresh.schema_head },
    { applied: migrationCount, no_op: false, schema_head: currentHead },
  );
  const replay = migrate(currentHead, "foundation_fresh");
  assert.deepEqual(
    { applied: replay.applied, no_op: replay.no_op, schema_head: replay.schema_head },
    { applied: 0, no_op: true, schema_head: currentHead },
  );
  const schemaBundleDigest = JSON.parse(
    readFileSync(
      resolve(root, `services/control-plane/migrations/product/${currentHead}/manifest.json`),
      "utf8",
    ),
  ).schema_bundle_digest;
  assert.equal(
    psql(
      `SET ROLE cloud_agents_migration_owner;
SELECT count(*) || '|' || min(migration_id) || '|' || max(migration_id) || '|' || count(DISTINCT bundle_digest)
FROM cloud_agents.schema_migrations;`,
      "foundation_migration",
      "foundation_upgrade",
    )
      .split("\n")
      .at(-1),
    `${migrationCount}|000001|${currentHead}|2`,
  );
  assert.equal(
    psql(
      `SET ROLE cloud_agents_migration_owner; SELECT bundle_digest FROM cloud_agents.schema_migrations WHERE migration_id='${currentHead}';`,
      "foundation_migration",
      "foundation_upgrade",
    )
      .split("\n")
      .at(-1),
    schemaBundleDigest,
  );
  assert.equal(
    psql(
      `SET ROLE cloud_agents_migration_owner;
SELECT count(*) || '|' || min(migration_id) || '|' || max(migration_id) || '|' || count(DISTINCT bundle_digest)
FROM cloud_agents.schema_migrations;`,
      "foundation_migration",
      "foundation_fresh",
    )
      .split("\n")
      .at(-1),
    `${migrationCount}|000001|${currentHead}|1`,
  );
  assert.equal(
    psql(
      `SET ROLE cloud_agents_migration_owner;
SELECT count(*) FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
WHERE namespace_row.nspname = 'cloud_agents' AND relation.relname IN ('workspaces','workspace_volumes','sandbox_sessions','runtime_profiles','runtime_profile_activity','foundation_sandbox_activity','network_policies','remote_worker_enrollments','remote_worker_enrollment_activity','remote_worker_node_activity','deployment_target_admin_projection');`,
      "foundation_migration",
      "foundation_fresh",
    )
      .split("\n")
      .at(-1),
    "11",
  );
  psql(
    `SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
  'tenant','tenant','Foundation test tenant','organization','organization','Foundation test organization',
  'user','https://foundation.test','foundation-admin','membership','membership','binding','binding',
  'tenant-audit','membership-audit','binding-audit','foundation-test');`,
    "foundation_bootstrap",
    "foundation_fresh",
  );
  psql(
    `SET ROLE cloud_agents_migration_owner;
WITH next_revision AS (
  UPDATE cloud_agents.tenant_resource_versions
  SET current_revision=current_revision+1,updated_at=clock_timestamp()
  WHERE tenant_id='tenant' AND tenant_uid='tenant'
  RETURNING current_revision
), project_change AS (
  INSERT INTO cloud_agents.resource_changes (
    tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at
  ) SELECT 'tenant','tenant',current_revision,'project','project','created','foundation-fixture',clock_timestamp()
  FROM next_revision RETURNING resource_version
)
INSERT INTO cloud_agents.projects (
  tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,resource_version,created_at,updated_at
) SELECT 'tenant','tenant','project','project','organization','Foundation test project','active',resource_version,clock_timestamp(),clock_timestamp()
FROM project_change;
INSERT INTO cloud_agents.deployment_targets (
  tenant_id,tenant_ref_id,project_uid,target_uid,target_name,target_kind,endpoint,credential_ref,generation,
  observed_phase,api_version,engine_version,target_os,target_arch,stable_error_code,last_probe_at,
  resource_version,create_idempotency_key,create_request_digest,created_at,updated_at
) VALUES (
  'tenant','tenant','project','target','target','docker','https://127.0.0.1:1','fixture-only',1,
  'ready','fixture','fixture','linux','arm64','',clock_timestamp(),1,'foundation-target-key',
  'sha256:${"d".repeat(64)}',clock_timestamp(),clock_timestamp()
);`,
    "foundation_migration",
    "foundation_fresh",
  );
  const runtimeResult = (query) =>
    psql(
      `BEGIN; SET LOCAL ROLE cloud_agents_runtime; SET LOCAL cloud_agents.tenant_id='tenant'; ${query}; COMMIT;`,
      "foundation_runtime",
      "foundation_fresh",
    )
      .split("\n")
      .filter((line) => !new Set(["BEGIN", "SET", "COMMIT"]).has(line))
      .at(-1);
  const digest = (value) => `sha256:${createHash("sha256").update(value).digest("hex")}`;
  const progress = JSON.stringify({
    requestId: "runtime-request",
    protocolVersion: { major: 2, minor: 3 },
    executionId: "execution-recovery",
    generation: 7,
    commandId: "turn-command",
    occurredAt: "2026-09-10T00:00:00Z",
    messageType: "Progress",
    payload: { text: "checkpointed" },
  });
  const terminal = JSON.stringify({
    requestId: "runtime-request",
    protocolVersion: { major: 2, minor: 3 },
    executionId: "execution-recovery",
    generation: 7,
    commandId: "turn-command",
    occurredAt: "2026-09-10T00:00:01Z",
    messageType: "Result",
    payload: { text: "recovered" },
  });
  const checkpointMessages = `[${progress}]`;
  const settledMessages = `[${progress},${terminal}]`;
  const sideEffectProgress = JSON.stringify({
    ...JSON.parse(progress),
    executionId: "execution-side-effect",
    messageType: "Event",
    payload: { eventType: "item.started", payload: { data: { providerItemId: "tool-1" } } },
  });
  const sideEffectMessages = `[${sideEffectProgress}]`;
  const sideEffectCheckpointDigest = digest(sideEffectMessages);
  const reconciliationDigest = digest(
    JSON.stringify({ checkpointDigest: sideEffectCheckpointDigest, outcome: "confirmed" }),
  );
  const d = "sha256:" + "1".repeat(64);
  psql(
    `SET ROLE cloud_agents_migration_owner;
INSERT INTO cloud_agents.managed_agent_sessions (
  tenant_id,tenant_ref_id,project_uid,session_uid,provider_kind,state,resource_version,
  create_idempotency_key,create_request_digest,created_at,updated_at
) VALUES ('tenant','tenant','project','session-recovery','codex','active',1,
  'session-recovery-key','${d}',clock_timestamp(),clock_timestamp());
INSERT INTO cloud_agents.managed_agent_turns (
  tenant_id,tenant_ref_id,project_uid,session_uid,turn_uid,input_digest,state,resource_version,
  create_idempotency_key,create_request_digest,created_at,updated_at
) VALUES ('tenant','tenant','project','session-recovery','turn-recovery','${d}','queued',1,
  'turn-recovery-key-1','${d}',clock_timestamp(),clock_timestamp()),
  ('tenant','tenant','project','session-recovery','turn-side-effect','${d}','queued',1,
  'turn-recovery-key-2','${d}',clock_timestamp(),clock_timestamp());
INSERT INTO cloud_agents.managed_agent_executions (
  tenant_id,tenant_ref_id,project_uid,session_uid,turn_uid,execution_uid,generation,state,
  resource_version,create_idempotency_key,create_request_digest,created_at,updated_at
) VALUES ('tenant','tenant','project','session-recovery','turn-recovery','execution-recovery',7,
  'queued',1,'execution-recovery-key','${d}',clock_timestamp(),clock_timestamp()),
  ('tenant','tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
  'queued',1,'execution-side-effect-key','${d}',clock_timestamp(),clock_timestamp());
UPDATE cloud_agents.managed_agent_turns SET execution_uid='execution-recovery'
WHERE tenant_id='tenant' AND project_uid='project' AND session_uid='session-recovery'
  AND turn_uid='turn-recovery';
UPDATE cloud_agents.managed_agent_turns SET execution_uid='execution-side-effect'
WHERE tenant_id='tenant' AND project_uid='project' AND session_uid='session-recovery'
  AND turn_uid='turn-side-effect';`,
    "foundation_migration",
    "foundation_fresh",
  );
  assert.equal(
    runtimeResult(`SELECT acquired || '|' || attempt_number FROM cloud_agents.claim_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-recovery','execution-recovery',7,
      'control-plane-a','incarnation-a','claim-a',1)`),
    "true|1",
  );
  runtimeResult(`SELECT execution_state FROM cloud_agents.start_claimed_managed_agent_execution_v1(
    'tenant','project','session-recovery','turn-recovery','execution-recovery',7,
    'execution-start-key','${d}',1,'control-plane-a','incarnation-a','claim-a')`);
  assert.equal(
    runtimeResult(`SELECT checkpoint_sequence FROM cloud_agents.checkpoint_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-recovery','execution-recovery',7,1,
      'control-plane-a','incarnation-a','claim-a',1,'${checkpointMessages}',
      '${digest(checkpointMessages)}','runtime-message-checkpoint-v1',NULL,false,0)`),
    "1",
  );
  assert.equal(
    runtimeResult(`SELECT acquired || '|' || attempt_number FROM cloud_agents.claim_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
      'control-plane-a','incarnation-a','claim-side-effect-a',1)`),
    "true|1",
  );
  runtimeResult(`SELECT execution_state FROM cloud_agents.start_claimed_managed_agent_execution_v1(
    'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
    'execution-side-effect-start','${d}',1,'control-plane-a','incarnation-a','claim-side-effect-a')`);
  runtimeResult(`SELECT checkpoint_sequence FROM cloud_agents.checkpoint_managed_agent_execution_v1(
    'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,1,
    'control-plane-a','incarnation-a','claim-side-effect-a',1,'${sideEffectMessages}',
    '${sideEffectCheckpointDigest}','runtime-message-checkpoint-v1',NULL,true,0)`);
  assert.throws(
    () => runtimeResult(`SELECT cloud_agents.reconcile_managed_agent_execution_side_effect_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
      '${sideEffectCheckpointDigest}','confirmed','${reconciliationDigest}')`),
    /not awaiting reconciliation/,
  );
  await delay(1100);
  assert.equal(
    runtimeResult(`SELECT acquired || '|' || attempt_number || '|' || recovery_state FROM cloud_agents.claim_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-recovery','execution-recovery',7,
      'control-plane-b','incarnation-b','claim-b',30)`),
    "true|2|recovering",
  );
  assert.equal(
    runtimeResult(`SELECT recovery_mode FROM cloud_agents.managed_agent_executions
      WHERE project_uid='project' AND execution_uid='execution-recovery'`),
    "process-restart",
  );
  assert.equal(
    runtimeResult(`SELECT acquired || '|' || attempt_number || '|' || recovery_state || '|' || recovery_reason
      FROM cloud_agents.claim_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
      'control-plane-b','incarnation-b','claim-side-effect-b',30)`),
    "false|1|awaiting_reconciliation|side_effect_outcome_unknown",
  );
  runtimeResult(`SELECT cloud_agents.reconcile_managed_agent_execution_side_effect_v1(
    'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
    '${sideEffectCheckpointDigest}','confirmed','${reconciliationDigest}')`);
  assert.equal(
    runtimeResult(`SELECT pending_side_effect || '|' || recovery_state || '|' || side_effect_reconciliation_outcome
      FROM cloud_agents.managed_agent_executions WHERE project_uid='project'
      AND execution_uid='execution-side-effect'`),
    "false|none|confirmed",
  );
  assert.throws(
    () => runtimeResult(`SELECT cloud_agents.reconcile_managed_agent_execution_side_effect_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
      '${sideEffectCheckpointDigest}','not-applied','${reconciliationDigest}')`),
    /reconciliation conflicts/,
  );
  assert.equal(
    runtimeResult(`SELECT acquired || '|' || attempt_number || '|' || recovery_state FROM cloud_agents.claim_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,
      'control-plane-b','incarnation-b','claim-side-effect-b',30)`),
    "true|2|recovering",
  );
  assert.throws(
    () => runtimeResult(`SELECT checkpoint_sequence FROM cloud_agents.checkpoint_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-side-effect','execution-side-effect',7,1,
      'control-plane-a','incarnation-a','claim-side-effect-a',30,'${sideEffectMessages}',
      '${sideEffectCheckpointDigest}','runtime-message-checkpoint-v1',NULL,true,0)`),
    /claim is stale/,
  );
  assert.throws(
    () =>
      runtimeResult(`SELECT checkpoint_sequence FROM cloud_agents.checkpoint_managed_agent_execution_v1(
        'tenant','project','session-recovery','turn-recovery','execution-recovery',7,1,
        'control-plane-a','incarnation-a','claim-a',30,'${checkpointMessages}',
        '${digest(checkpointMessages)}','runtime-message-checkpoint-v1',NULL,false,0)`),
    /claim is stale/,
  );
  assert.throws(
    () =>
      runtimeResult(`SELECT execution_state FROM cloud_agents.settle_claimed_managed_agent_execution_v1(
        'tenant','project','session-recovery','turn-recovery','execution-recovery',7,'succeeded',
        '${digest(terminal)}',NULL,'execution-finish-key','${d}',NULL,'${terminal}',
        '${settledMessages}',1,'control-plane-a','incarnation-a','claim-a')`),
    /claim is stale/,
  );
  assert.equal(
    runtimeResult(`SELECT execution_state FROM cloud_agents.settle_claimed_managed_agent_execution_v1(
      'tenant','project','session-recovery','turn-recovery','execution-recovery',7,'succeeded',
      '${digest(terminal)}',NULL,'execution-finish-key','${d}',NULL,'${terminal}',
      '${settledMessages}',2,'control-plane-b','incarnation-b','claim-b')`),
    "succeeded",
  );
  assert.equal(
    runtimeResult(`SELECT state || '|' || attempt_number || '|' || recovery_state
      FROM cloud_agents.managed_agent_executions WHERE project_uid='project'
      AND execution_uid='execution-recovery'`),
    "succeeded|2|recovered",
  );
  const runServerTest = (testName) =>
    docker(
      "exec",
      "-e",
      "CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL=postgres://foundation_runtime@127.0.0.1/foundation_fresh?sslmode=disable",
      "-e",
      "CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL=postgres://foundation_migration@127.0.0.1/foundation_fresh?sslmode=disable",
      "-e",
      "CLOUD_AGENTS_REMOTE_WORKER_BINARY=/tmp/cloud-agents-remote-worker",
      name,
      "/tmp/server.test",
      "-test.run",
      `^${testName}$`,
      "-test.v",
    );
  const serverOutput = runServerTest("TestFoundationRuntimeProfilePostgres");
  assert.ok(serverOutput.includes("--- PASS: TestFoundationRuntimeProfilePostgres"));
  const enrollmentOutput = runServerTest("TestRemoteWorkerEnrollmentPostgres");
  assert.ok(enrollmentOutput.includes("--- PASS: TestRemoteWorkerEnrollmentPostgres"));
  assert.ok(enrollmentOutput.includes("outbound RemoteWorker process heartbeat passed"));
  const controllerOutput = docker(
    "exec",
    "-e",
    "CLOUD_AGENTS_FOUNDATION_CONTROLLER_DATABASE_URL=postgres://foundation_runtime@127.0.0.1/foundation_fresh?sslmode=disable",
    "-e",
    "CLOUD_AGENTS_FOUNDATION_CONTROLLER_OWNER_DATABASE_URL=postgres://foundation_migration@127.0.0.1/foundation_fresh?sslmode=disable",
    name,
    "/tmp/postgres.test",
    "-test.run",
    "^TestFoundationControllerPostgres$",
    "-test.v",
  );
  assert.ok(controllerOutput.includes("--- PASS: TestFoundationControllerPostgres"));
  process.stdout.write(
    JSON.stringify({
      postgres: psql("SHOW server_version;"),
      architecture,
      upgradeFrom: previousHead,
      upgradeTo: currentHead,
      fresh,
      replay,
      checks: [
        `product-${currentHead} fresh install`,
        `product-${previousHead} to product-${currentHead} exact upgrade`,
        `product-${currentHead} no-op replay`,
        `${migrationCount}-row immutable ledger with two bundle digests`,
        "foundation, runtime profile and RemoteWorker enrollment tables installed",
        "real Admin/User generated SDK and HTTP authorization",
        "RuntimeProfile create/publish/disable and public redaction",
        "durable Sandbox Operation/outbox acceptance and replay",
        "Controller claim renewal, retry, expired-claim recovery, terminal exhaustion and settlement",
        "Workspace volume usage generation claim, one-minute cadence, failed-observation retention and runtime-only settlement",
        "Sandbox network usage generation claim, one-minute cadence, monotonic aggregate counters, failed-observation retention and runtime-only settlement",
        "legacy project dispatcher cannot claim foundation operation effects",
        "ordinary user Admin 403 and direct intent bypass denial",
        "RemoteWorker Admin/bootstrap 403 separation and non-replayable no-store enrollment secret",
        "RemoteWorker CSR validation, 15-minute mTLS identity, verified rotation and old-certificate exact replay",
        "active RemoteWorker certificate revocation, ordinary-user Admin 403 and durable audit",
        "actual outbound RemoteWorker process Drain over mTLS with durable restart state and receipt",
        "generation/resource-version fencing, exact idempotency and receipt replay, deadline failure and Operation/Audit closure",
        "database-time RemoteWorker online, degraded and offline Admin projection without secret or certificate bytes",
        "server-owned RemoteWorker DeploymentTarget unprobed, ready, offline, reconnect and revoked projection",
        "managed Agent checkpoint recovery with expired-claim takeover, stale-writer fencing and side-effect reconciliation",
      ],
      boundary:
        "Disposable PostgreSQL, in-process Control Plane HTTPS/mTLS and short-lived outbound RemoteWorker processes; Drain/Resume changes node scheduling state only, and no customer-node Sandbox workload or external Controller is started",
    }) + "\n",
  );
} finally {
  rmSync(build, { recursive: true, force: true });
  if (started) {
    assert.equal(
      JSON.parse(docker("inspect", name))[0].Config.Labels[
        "cloud-agents-foundation-migration-test"
      ],
      name,
    );
    docker("rm", "-f", "-v", name);
    assert.equal(
      docker("ps", "-aq", "--filter", `label=cloud-agents-foundation-migration-test=${name}`),
      "",
    );
    process.stdout.write(
      "Removed only owned disposable PostgreSQL container and anonymous volume\n",
    );
  }
}
