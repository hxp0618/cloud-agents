import assert from "node:assert/strict";
import { execFile, execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { readFileSync, readdirSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";
import { promisify } from "node:util";

// Disposable PostgreSQL authority test, not an HTTP/Controller/runtime acceptance.
const name = `foundation-intents-${randomUUID()}`;
const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 120000,
  }).trim();
const sql = (query, user = "postgres") =>
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
      "postgres",
    ],
    { input: query, encoding: "utf8", timeout: 120000, stdio: ["pipe", "pipe", "pipe"] },
  ).trim();
const runtime = (query) =>
  sql("SET cloud_agents.tenant_id='tenant';\n" + query, "foundation_runtime");
const owner = (query) => sql("SET ROLE cloud_agents_migration_owner;\n" + query);
const digest = "sha256:" + "a".repeat(64);
const intent = (key = "foundation-key-0001", workspace = "workspace", requestDigest = digest) =>
  `SELECT cloud_agents.accept_foundation_intent_v1('project','${workspace}','workspace','volume','target','sandbox','node@${digest}',500,536870912,'operation','event','${digest}','${key}','${requestDigest}');`;
const rejects = (query, user, expected) => {
  assert.throws(
    () => sql(query, user),
    (error) => String(error.stderr).includes(expected),
  );
};
const waitForPostgres = async (failure) => {
  for (let n = 0; ; n++) {
    try {
      assert.equal(sql("SELECT 1"), "1");
      await delay(100);
      assert.equal(sql("SELECT 1"), "1");
      return;
    } catch {
      if (n === 50) throw new Error(failure);
      await delay(100);
    }
  }
};
let started = false;
try {
  docker("image", "inspect", "postgres:17.6-bookworm");
  // No host port or host volume. Trust auth is confined to this disposable container.
  docker(
    "run",
    "-d",
    "--name",
    name,
    "--label",
    `cloud-agents-foundation-test=${name}`,
    "--network",
    "none",
    "-e",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "postgres:17.6-bookworm",
  );
  started = true;
  await waitForPostgres("PostgreSQL did not start");
  sql(readFileSync("services/control-plane/migrations/bootstrap/roles.sql", "utf8"));
  sql(
    "GRANT CREATE ON DATABASE postgres TO cloud_agents_migration_owner; CREATE ROLE foundation_runtime LOGIN INHERIT; GRANT cloud_agents_runtime TO foundation_runtime;",
  );
  for (const file of readdirSync("services/control-plane/migrations")
    .filter((p) => /^\d{6}_.*\.sql$/.test(p) && p.slice(0, 6) <= "000053")
    .sort()) {
    owner(readFileSync(`services/control-plane/migrations/${file}`, "utf8"));
  }
  owner(`BEGIN;
    INSERT INTO cloud_agents.platform_tenants (tenant_id,tenant_uid,tenant_name,display_name,state,resource_version,created_at,updated_at)
    VALUES ('tenant','tenant','tenant','test','active',1,now(),now());
    INSERT INTO cloud_agents.resource_changes VALUES ('tenant','tenant',1,'platform_tenant','tenant','created','fixture',now()),
      ('tenant','tenant',2,'organization','organization','created','fixture',now()),('tenant','tenant',3,'project','project','created','fixture',now());
    INSERT INTO cloud_agents.organizations (tenant_id,tenant_ref_id,organization_uid,organization_name,display_name,state,resource_version,created_at,updated_at)
      VALUES ('tenant','tenant','organization','organization','test','active',2,now(),now());
    INSERT INTO cloud_agents.projects (tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,resource_version,created_at,updated_at)
      VALUES ('tenant','tenant','project','project','organization','test','active',3,now(),now());
    INSERT INTO cloud_agents.deployment_targets (tenant_id,tenant_ref_id,project_uid,target_uid,target_name,target_kind,endpoint,credential_ref,generation,observed_phase,api_version,engine_version,target_os,target_arch,stable_error_code,last_probe_at,resource_version,create_idempotency_key,create_request_digest,created_at,updated_at)
      VALUES ('tenant','tenant','project','target','target','docker','https://127.0.0.1:1','fixture-only',1,'ready','fixture','fixture','linux','arm64','',now(),1,'fixture-target-0001','${digest}',now(),now());
    COMMIT;`);
  const simultaneous = await Promise.all(
    [1, 2].map(() =>
      promisify(execFile)(
        "docker",
        [
          "--context",
          "orbstack",
          "exec",
          name,
          "psql",
          "-XAt",
          "-v",
          "ON_ERROR_STOP=1",
          "-U",
          "foundation_runtime",
          "-d",
          "postgres",
          "-c",
          "SET cloud_agents.tenant_id='tenant';" + intent(),
        ],
        { encoding: "utf8", timeout: 15000 },
      ),
    ),
  );
  for (const result of simultaneous)
    assert.equal(result.stdout.trim().split("\n").at(-1), "operation");
  assert.equal(runtime(intent()).split("\n").at(-1), "operation");
  rejects(
    "SET cloud_agents.tenant_id='tenant';" + intent(undefined, "other"),
    "foundation_runtime",
    "idempotency conflict",
  );
  rejects(
    "SET cloud_agents.tenant_id='tenant';" +
      intent(undefined, "workspace", "sha256:" + "b".repeat(64)),
    "foundation_runtime",
    "idempotency conflict",
  );
  assert.equal(
    runtime(
      "SELECT (SELECT count(*) FROM cloud_agents.workspaces),(SELECT count(*) FROM cloud_agents.workspace_volumes),(SELECT count(*) FROM cloud_agents.sandbox_sessions),(SELECT count(*) FROM cloud_agents.outbox_events),(SELECT count(*) FROM cloud_agents.coordination_audit_facts);",
    )
      .split("\n")
      .at(-1),
    "1|1|1|1|1",
  );
  rejects(
    "SET cloud_agents.tenant_id='tenant'; DELETE FROM cloud_agents.workspace_volumes;",
    "foundation_runtime",
    "permission denied",
  );
  rejects(
    "SET cloud_agents.tenant_id='tenant';" + intent("foundation-key-0002", "other-workspace"),
    "foundation_runtime",
    "duplicate key",
  );
  assert.equal(
    runtime("SELECT count(*) FROM cloud_agents.idempotency_records;").split("\n").at(-1),
    "1",
  );
  assert.equal(
    owner(
      "SELECT cloud_agents.coordination_profile_creates_operation('managedAgentCreateProject/v1alpha1','sha256:cdd54c59c0c363687eeb0e0e48aff00221cc4d42780bdf935c4b47bcd297f308'), cloud_agents.coordination_profile_creates_operation('managedAgentCreateProjectDurable/v1alpha1','sha256:48d3afc5e78e7e7c537e528f74a510b91d08cffa4415eed3f6d651bd78deb81f'), cloud_agents.coordination_profile_is_registered('unknown','unknown');",
    )
      .split("\n")
      .at(-1),
    "f|t|f",
  );
  assert.equal(
    sql(
      "SET cloud_agents.tenant_id='other'; SELECT count(*) FROM cloud_agents.workspaces;",
      "foundation_runtime",
    )
      .split("\n")
      .at(-1),
    "0",
  );
  // Process loss/restart does not lose the accepted intent; no client resubmission.
  docker("restart", name);
  await waitForPostgres("PostgreSQL did not restart");
  assert.equal(
    runtime("SELECT state FROM cloud_agents.platform_operations;").split("\n").at(-1),
    "pending",
  );
  assert.equal(
    runtime(
      `SELECT event_id FROM cloud_agents.claim_outbox_event('tenant','controller','incarnation','claim',60,'${digest}','claim-audit');`,
    )
      .split("\n")
      .at(-1),
    "event",
  );
  assert.equal(
    runtime(
      `SELECT count(*) FROM cloud_agents.claim_outbox_event('tenant','other-controller','incarnation','other-claim',60,'${digest}','other-audit');`,
    )
      .split("\n")
      .at(-1),
    "0",
  );
  rejects(
    "SET ROLE cloud_agents_migration_owner; UPDATE cloud_agents.sandbox_sessions SET writer_released=true;",
    "postgres",
    "check constraint",
  );
  rejects(
    "SET ROLE cloud_agents_migration_owner; INSERT INTO cloud_agents.sandbox_sessions SELECT tenant_id,project_uid,'second',workspace_uid,generation,desired_state,observed_state,observed_generation,writer_released,image_uri,cpu_millis,memory_bytes,operation_id,operation_generation,spec_digest FROM cloud_agents.sandbox_sessions;",
    "postgres",
    "sandbox_sessions_single_writer",
  );
  assert.equal(
    runtime("SELECT retention FROM cloud_agents.workspace_volumes;").split("\n").at(-1),
    "retain",
  );
  process.stdout.write(
    JSON.stringify({
      name,
      postgres: sql("SHOW server_version;"),
      checks: [
        "atomic accepted intent",
        "idempotent replay",
        "concurrent accepts coalesce",
        "partial transaction rollback",
        "legacy profiles preserved",
        "changed input rejected",
        "one outbox and audit",
        "runtime direct delete denied",
        "tenant RLS",
        "restart durability",
        "exclusive outbox claim",
        "writer release guard",
        "single writer",
        "volume retention",
      ],
      boundary:
        "Database only; no HTTP, RuntimeProfile resolution, Controller effects or Admin acceptance",
    }) + "\n",
  );
} finally {
  if (started) {
    assert.equal(
      JSON.parse(docker("inspect", name))[0].Config.Labels["cloud-agents-foundation-test"],
      name,
    );
    docker("rm", "-f", "-v", name);
    assert.equal(docker("ps", "-aq", "--filter", `label=cloud-agents-foundation-test=${name}`), "");
    process.stdout.write(
      "Removed only owned disposable PostgreSQL container and anonymous volume\n",
    );
  }
}
