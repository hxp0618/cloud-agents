import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { randomUUID } from "node:crypto";

// Real product migration runner against one owned, network-disabled PostgreSQL.
const root = resolve(import.meta.dirname, "..");
const name = `foundation-migrate-${randomUUID()}`;
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-foundation-migrate-"));
const binary = resolve(build, "cloud-agents-product-migrate");
const testBinary = resolve(build, "localmigration.test");
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
GRANT cloud_agents_migration_owner TO foundation_migration;
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
  docker("cp", binary, `${name}:/tmp/cloud-agents-product-migrate`);
  docker("cp", testBinary, `${name}:/tmp/localmigration.test`);
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
  const fresh = migrate("000053", "foundation_fresh");
  assert.deepEqual(
    { applied: fresh.applied, no_op: fresh.no_op, schema_head: fresh.schema_head },
    { applied: 53, no_op: false, schema_head: "000053" },
  );
  const replay = migrate("000053", "foundation_fresh");
  assert.deepEqual(
    { applied: replay.applied, no_op: replay.no_op, schema_head: replay.schema_head },
    { applied: 0, no_op: true, schema_head: "000053" },
  );
  const schemaBundleDigest = JSON.parse(
    readFileSync(
      resolve(root, "services/control-plane/migrations/product/000053/manifest.json"),
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
    "53|000001|000053|2",
  );
  assert.equal(
    psql(
      "SET ROLE cloud_agents_migration_owner; SELECT bundle_digest FROM cloud_agents.schema_migrations WHERE migration_id='000053';",
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
    "53|000001|000053|1",
  );
  assert.equal(
    psql(
      `SET ROLE cloud_agents_migration_owner;
SELECT count(*) FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace_row ON namespace_row.oid = relation.relnamespace
WHERE namespace_row.nspname = 'cloud_agents' AND relation.relname IN ('workspaces','workspace_volumes','sandbox_sessions');`,
      "foundation_migration",
      "foundation_fresh",
    )
      .split("\n")
      .at(-1),
    "3",
  );
  process.stdout.write(
    JSON.stringify({
      postgres: psql("SHOW server_version;"),
      architecture,
      upgradeFrom: "000052",
      upgradeTo: "000053",
      fresh,
      replay,
      checks: [
        "product-000052 fresh install",
        "product-000052 to product-000053 exact upgrade",
        "product-000053 no-op replay",
        "53-row immutable ledger with two bundle digests",
        "foundation tables installed",
      ],
      boundary:
        "Disposable installer validation; no application API, Controller or runtime side effect",
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
