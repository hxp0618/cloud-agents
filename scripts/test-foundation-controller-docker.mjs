import assert from "node:assert/strict";
import { createHash, randomUUID } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { isIP } from "node:net";
import { homedir, tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [output] = process.argv.slice(2);
assert.ok(output, "usage: node scripts/test-foundation-controller-docker.mjs NEW_OUTPUT_DIRECTORY");
const remoteWorkerOnly = process.argv.includes("--remote-worker-only");
const gvisorOnly = process.argv.includes("--gvisor-only");
const snapshotOnly = process.argv.includes("--snapshot-only");
const snapshotRestoreOnly = process.argv.includes("--snapshot-restore-only");
const snapshotCleanupOnly = process.argv.includes("--snapshot-cleanup-only");
const faultSoakOnly = process.argv.includes("--fault-soak-only");
const sshProbeOnly = process.argv.includes("--ssh-probe-only");
assert.ok(
  [
    remoteWorkerOnly,
    gvisorOnly,
    snapshotOnly,
    snapshotRestoreOnly,
    snapshotCleanupOnly,
    faultSoakOnly,
    sshProbeOnly,
  ].filter(Boolean).length <= 1,
  "choose one focused mode",
);
const remoteWorkerComplete = Symbol("remote-worker-complete");
const snapshotComplete = Symbol("snapshot-complete");
const sshProbeComplete = Symbol("ssh-probe-complete");
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
const dindName = `${run}-dind`;
const dindSocketVolume = `${run}-dind-run`;
const gvisorNetwork = `${run}-internal-deny`;
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
const dindImage = "docker@sha256:5efed980cba3fc126cf54e21a5a6ff8849d05b6e0623d6e7612f48e9cd6cd17e";
const registryImage = "registry:2";
const registryImageID = "sha256:33eeff39e0aaabe61ca826fd7502396183462451be0783133e1a8fa944fc7350";
const runscPath =
  process.env.CLOUD_AGENTS_GVISOR_RUNSC ??
  "/tmp/cloud-agents-gvisor-release-20260622.0-aarch64/runsc";
const runscSHA512 =
  "6d43f7c99dc182ad3750390c811537ee312fca5938245c65f0c5a23b9d38e8b7fddc1f49a816e6696d708582d813ac2d87ee98f7b46b8a6f5e6241c9eba69443";
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
    {
      encoding: "utf8",
      input: query,
      timeout: 120_000,
      stdio: ["pipe", "pipe", "pipe"],
    },
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
let dindStarted = false;
let sandboxBase = "";
let dindAddress = "";
let innerDocker;
let remoteSandboxImage = sandboxImage;
let remoteExecdImage = execdImage;
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
    ...(gvisorOnly ? [dindImage, registryImage] : []),
  ]) {
    docker("image", "inspect", image);
  }
  if (gvisorOnly) {
    assert.equal(docker("image", "inspect", registryImage, "--format", "{{.Id}}"), registryImageID);
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
        env: {
          ...process.env,
          CLOUD_AGENTS_PLATFORM_DATABASE_URL: migrationURL,
        },
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

  if (sshProbeOnly) {
    const aliases = (process.env.CLOUD_AGENTS_FOUNDATION_SSH_ALIASES ?? "")
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean);
    assert.equal(aliases.length, 2, "set exactly two comma-separated SSH aliases");
    const credentialDirectory = resolve(build, "ssh-live-credentials");
    mkdirSync(credentialDirectory, { mode: 0o700 });
    const expandSSHPath = (value) => value.replace(/^~/u, homedir()).replaceAll("%d", homedir());
    const targets = aliases.map((alias, index) => {
      const config = new Map();
      for (const line of execFileSync("ssh", ["-G", alias], {
        encoding: "utf8",
        timeout: 10_000,
      }).split("\n")) {
        const separator = line.indexOf(" ");
        if (separator > 0 && !config.has(line.slice(0, separator))) {
          config.set(line.slice(0, separator), line.slice(separator + 1).trim());
        }
      }
      const user = config.get("user");
      const host = config.get("hostname");
      const port = config.get("port") ?? "22";
      const identity = expandSSHPath(config.get("identityfile") ?? "");
      assert.match(user ?? "", /^[A-Za-z0-9._-]+$/u);
      assert.match(host ?? "", /^[A-Za-z0-9.:-]+$/u);
      assert.match(port, /^[1-9][0-9]{0,4}$/u);
      assert.ok(existsSync(identity));
      const remoteFacts = execFileSync(
        "ssh",
        [
          "-T",
          "-o",
          "BatchMode=yes",
          "-o",
          "ConnectTimeout=10",
          alias,
          "uname -s; uname -m; if command -v docker >/dev/null; then echo docker; else echo no-docker; fi",
        ],
        { encoding: "utf8", timeout: 30_000 },
      )
        .trim()
        .split("\n");
      assert.deepEqual(remoteFacts, ["Linux", "x86_64", "no-docker"]);
      const scan = execFileSync("ssh-keyscan", ["-T", "10", "-p", port, "-t", "ed25519", host], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
        timeout: 30_000,
      })
        .trim()
        .split("\n")
        .find((line) => !line.startsWith("#"));
      assert.ok(scan);
      const scanParts = scan.trim().split(/\s+/u);
      assert.equal(scanParts[1], "ssh-ed25519");
      const pinnedKey = `${scanParts[1]} ${scanParts[2]}`;
      const lookups = [port === "22" ? host : `[${host}]:${port}`, host, alias];
      const knownHostFiles = [
        ...(config.get("userknownhostsfile") ?? "").split(/\s+/u),
        ...(config.get("globalknownhostsfile") ?? "").split(/\s+/u),
      ]
        .filter(Boolean)
        .map(expandSSHPath)
        .filter(existsSync);
      const pinned = knownHostFiles.some((file) =>
        lookups.some((lookup) => {
          const found = spawnSync("ssh-keygen", ["-F", lookup, "-f", file], {
            encoding: "utf8",
            timeout: 10_000,
          });
          return (found.stdout ?? "")
            .split("\n")
            .some((line) => line.trim().split(/\s+/u).slice(1, 3).join(" ") === pinnedKey);
        }),
      );
      assert.ok(pinned, `scanned host key for SSH alias ${index + 1} was not already pinned`);
      const credentialRef = `ssh-live-${index + 1}`;
      copyFileSync(identity, resolve(credentialDirectory, `${credentialRef}.key`));
      chmodSync(resolve(credentialDirectory, `${credentialRef}.key`), 0o600);
      writeFileSync(resolve(credentialDirectory, `${credentialRef}.user`), `${user}\n`, {
        mode: 0o600,
      });
      writeFileSync(
        resolve(credentialDirectory, `${credentialRef}.host-key.pub`),
        `${pinnedKey}\n`,
        { mode: 0o600 },
      );
      return {
        targetId: `ssh-live-${index + 1}`,
        targetName: `ssh-live-${index + 1}`,
        endpoint: `ssh://${host.includes(":") ? `[${host}]` : host}:${port}`,
        credentialRef,
        pinnedKey,
      };
    });
    assert.notEqual(targets[0].pinnedKey, targets[1].pinnedKey);
    const mismatch = {
      targetId: "ssh-host-key-negative",
      targetName: "ssh-host-key-negative",
      endpoint: targets[0].endpoint,
      credentialRef: "ssh-host-key-negative",
    };
    copyFileSync(
      resolve(credentialDirectory, `${targets[0].credentialRef}.key`),
      resolve(credentialDirectory, `${mismatch.credentialRef}.key`),
    );
    writeFileSync(
      resolve(credentialDirectory, `${mismatch.credentialRef}.user`),
      readFileSync(resolve(credentialDirectory, `${targets[0].credentialRef}.user`)),
      { mode: 0o600 },
    );
    writeFileSync(
      resolve(credentialDirectory, `${mismatch.credentialRef}.host-key.pub`),
      `${targets[1].pinnedKey}\n`,
      { mode: 0o600 },
    );
    chmodSync(resolve(credentialDirectory, `${mismatch.credentialRef}.key`), 0o600);
    const input = JSON.stringify({
      targets: targets.map(({ pinnedKey: _pinnedKey, ...target }) => target),
      mismatch,
    });
    const runSSHSoak = (phase) =>
      execFileSync(
        serverTestBinary,
        ["-test.run", "^TestFoundationSSHProbeSoakPostgres$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_FOUNDATION_SSH_RUNTIME_DATABASE_URL: runtimeURL,
            CLOUD_AGENTS_FOUNDATION_SSH_CREDENTIAL_DIRECTORY: credentialDirectory,
            CLOUD_AGENTS_FOUNDATION_SSH_TARGETS: input,
            CLOUD_AGENTS_FOUNDATION_SSH_PHASE: phase,
          },
          timeout: 180_000,
        },
      );
    const beforeOutput = runSSHSoak("before-restart");
    const before = parseMarker(beforeOutput, "FOUNDATION_SSH_SOAK");
    const afterOutput = runSSHSoak("after-restart");
    const after = parseMarker(afterOutput, "FOUNDATION_SSH_SOAK");
    for (const receipt of [before, after]) {
      assert.equal(receipt.cycles, 16);
      assert.equal(receipt.targetCount, 2);
      assert.equal(receipt.probeRequests, 32);
      assert.equal(receipt.readRequests, 32);
      assert.equal(receipt.deniedRequests, 16);
      assert.equal(receipt.hostKeyMismatchStableError, "ssh-host-key-mismatch");
    }
    assert.deepEqual(after.facts, before.facts);
    const evidence = {
      run,
      source: {
        branch: execFileSync("git", ["branch", "--show-current"], {
          cwd: root,
          encoding: "utf8",
        }).trim(),
        head: execFileSync("git", ["rev-parse", "HEAD"], {
          cwd: root,
          encoding: "utf8",
        }).trim(),
        dirty: true,
        migrationHead: migration.schema_head,
      },
      backend: {
        postgres: psql("SHOW server_version;"),
        externalHostCount: targets.length,
        remoteOS: "linux",
        remoteArchitecture: "amd64",
        remoteDockerAvailable: false,
      },
      controlPlaneRestart: {
        before,
        after,
        totalRequests: 160,
        recoveryToFirstSuccessfulProbeMilliseconds: after.recoveryToFirstSuccessMilliseconds,
      },
      checks: [
        `product migration ${migration.schema_head} applied to disposable PostgreSQL`,
        "two external SSH servers accepted only their existing key-only aliases and already-pinned host keys",
        "two separate Control Plane test processes used generated SDK and production Admin handlers against one PostgreSQL authority",
        "64 real SSH probes, 64 Admin reads and 32 ordinary-user 403 responses completed with measured P50/P95/max latency",
        "the external OS, architecture and SSH server facts remained stable across the Control Plane process restart",
        "a real external endpoint with the other host's pinned key failed closed and persisted ssh-host-key-mismatch",
        "Operation and Audit pages retained every probe; no remote file, package, service or container was changed",
      ],
      boundary:
        "Two preconfigured external SSH servers and disposable local PostgreSQL only; this proves old SSH Target registration/probe compatibility, host-key fencing and bounded reconnection across Control Plane process restart, not remote Docker Worker deployment, Provider execution, SSH-server restart, throughput SLO or multi-Region recovery",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    writeFileSync(resolve(evidenceDirectory, "before-restart.log"), beforeOutput);
    writeFileSync(resolve(evidenceDirectory, "after-restart.log"), afterOutput);
    process.stdout.write(
      `Verified external SSH Target probe/soak; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw sshProbeComplete;
  }

  let allowedSinkIP = "";
  let blockedSinkIP = "";
  if (!gvisorOnly) {
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
    allowedSinkIP = docker(
      "inspect",
      "--format",
      "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
      allowedSinkName,
    );
    blockedSinkIP = docker(
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
  }

  let sandboxDockerGateway = dockerGateway;
  if (gvisorOnly) {
    const runsc = readFileSync(runscPath);
    assert.equal(createHash("sha512").update(runsc).digest("hex"), runscSHA512);
    docker("volume", "create", dindSocketVolume);
    docker(
      "run",
      "-d",
      "--privileged",
      "--name",
      dindName,
      "--label",
      `cloud-agents-foundation-controller-test=${run}`,
      "-e",
      "DOCKER_TLS_CERTDIR=",
      "-p",
      "127.0.0.1::2375",
      "-p",
      `127.0.0.1:${sandboxServerPort}:8080`,
      "--mount",
      `type=bind,src=${runscPath},dst=/usr/local/bin/runsc,readonly`,
      "--mount",
      `type=volume,src=${dindSocketVolume},dst=/var/run`,
      dindImage,
      "--host=tcp://0.0.0.0:2375",
      "--host=unix:///var/run/docker.sock",
      "--tls=false",
      "--add-runtime=runsc=/usr/local/bin/runsc",
    );
    dindStarted = true;
    dindAddress = `127.0.0.1:${mappedPort(dindName, 2375)}`;
    innerDocker = (...args) =>
      execFileSync("docker", ["-H", `tcp://${dindAddress}`, ...args], {
        encoding: "utf8",
        timeout: 180_000,
      }).trim();
    for (let attempt = 0; ; attempt++) {
      try {
        assert.equal(innerDocker("info", "--format", "{{json .Runtimes.runsc}}") !== "null", true);
        break;
      } catch {
        if (attempt === 300) throw new Error("gVisor DinD did not start");
        await delay(100);
      }
    }
    const archive = resolve(build, "gvisor-images.tar");
    docker(
      "image",
      "save",
      "-o",
      archive,
      "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd:v1.0.21",
      "node:22-bookworm-slim",
      registryImage,
    );
    innerDocker("image", "load", "-i", archive);
    innerDocker("run", "-d", "--name", `${run}-registry`, "-p", "5000:5000", registryImage);
    await delay(300);
    innerDocker(
      "tag",
      "node:22-bookworm-slim",
      "localhost:5000/cloud-agents/node:22-bookworm-slim",
    );
    innerDocker("push", "localhost:5000/cloud-agents/node:22-bookworm-slim");
    remoteSandboxImage = innerDocker(
      "image",
      "inspect",
      "localhost:5000/cloud-agents/node:22-bookworm-slim",
      "--format",
      "{{index .RepoDigests 0}}",
    );
    innerDocker(
      "tag",
      "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd:v1.0.21",
      "localhost:5000/cloud-agents/execd:v1.0.21",
    );
    innerDocker("push", "localhost:5000/cloud-agents/execd:v1.0.21");
    remoteExecdImage = innerDocker(
      "image",
      "inspect",
      "localhost:5000/cloud-agents/execd:v1.0.21",
      "--format",
      "{{index .RepoDigests 0}}",
    );
    innerDocker(
      "network",
      "create",
      "--internal",
      "--opt",
      "com.docker.network.bridge.enable_icc=false",
      gvisorNetwork,
    );
    const network = JSON.parse(innerDocker("network", "inspect", gvisorNetwork))[0];
    assert.equal(network.Internal, true);
    assert.equal(network.Options["com.docker.network.bridge.enable_icc"], "false");
    sandboxDockerGateway = "127.0.0.1";
  }

  const configPath = resolve(build, "opensandbox.toml");
  writeFileSync(
    configPath,
    gvisorOnly
      ? `[server]\nhost="0.0.0.0"\neip="127.0.0.1"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${remoteExecdImage}"\n[docker]\nnetwork_mode="${gvisorNetwork}"\nhost_ip="${sandboxDockerGateway}"\nport_range_min=49310\nport_range_max=49520\n[secure_runtime]\ntype="gvisor"\ndocker_runtime="runsc"\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`
      : `[server]\nhost="0.0.0.0"\neip="127.0.0.1"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${execdImage}"\n[docker]\nnetwork_mode="bridge"\nhost_ip="${dockerGateway}"\nport_range_min=49310\nport_range_max=49520\n[egress]\nimage="${egressImage}"\nmode="dns+nft"\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
    { mode: 0o600 },
  );

  docker(
    "run",
    "-d",
    "--name",
    sandboxServerName,
    "--label",
    `cloud-agents-foundation-controller-test=${run}`,
    ...(gvisorOnly
      ? ["--network", `container:${dindName}`]
      : ["-p", `127.0.0.1:${sandboxServerPort}:8080`]),
    "--mount",
    `type=bind,src=${configPath},dst=/etc/opensandbox/config.toml,readonly`,
    "--mount",
    gvisorOnly
      ? `type=volume,src=${dindSocketVolume},dst=/var/run`
      : "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
    serverImage,
  );
  sandboxServerStarted = true;
  sandboxBase = `http://127.0.0.1:${sandboxServerPort}`;
  for (let attempt = 0; ; attempt++) {
    try {
      if (
        (
          await fetch(sandboxBase + "/health", {
            signal: AbortSignal.timeout(1000),
          })
        ).ok
      )
        break;
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
          CLOUD_AGENTS_REMOTE_WORKER_DOCKER_SOCKET: gvisorOnly ? "" : dockerSocket,
          CLOUD_AGENTS_REMOTE_WORKER_DOCKER_ADDRESS: gvisorOnly ? dindAddress : "",
          CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_DIRECTORY: credentialDirectory,
          CLOUD_AGENTS_REMOTE_WORKER_CREDENTIAL_REF: "fixture-only",
          CLOUD_AGENTS_REMOTE_WORKER_SUCCESS_IMAGE_URI: remoteSandboxImage,
          CLOUD_AGENTS_REMOTE_WORKER_ALLOWED_EGRESS: gvisorOnly ? "" : `${allowedSinkIP}/32`,
          CLOUD_AGENTS_REMOTE_WORKER_ISOLATION_RUNTIME: gvisorOnly ? "gvisor" : "runc",
        },
        timeout: 180_000,
      },
    ),
    gvisorOnly ? "REMOTE_WORKER_GVISOR" : "REMOTE_WORKER_SANDBOX",
  );
  if (gvisorOnly) {
    assert.equal(remoteWorkerReceipt.workloadTrust, "shared-untrusted");
    assert.equal(remoteWorkerReceipt.isolationRuntime, "gvisor");
    assert.equal(remoteWorkerReceipt.capabilityRejected, true);
    assert.equal(remoteWorkerReceipt.publicProfileRedacted, true);
    assert.equal(remoteWorkerReceipt.physicalIsolationVerified, true);
    assert.equal(remoteWorkerReceipt.outboundEgressBlocked, true);
    assert.match(remoteWorkerReceipt.kernelAndDmesg, /4\.19\.0-gvisor/u);
    assert.match(remoteWorkerReceipt.kernelAndDmesg, /gVisor/u);
    assert.equal(remoteWorkerReceipt.observedState, "stopped");
    assert.equal(innerDocker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
    innerDocker("volume", "rm", remoteWorkerReceipt.volumeName);
    assert.equal(
      innerDocker(
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
        head: execFileSync("git", ["rev-parse", "HEAD"], {
          cwd: root,
          encoding: "utf8",
        }).trim(),
        dirty: true,
      },
      backend: {
        hostDockerContext: "orbstack",
        hostDockerVersion: docker("version", "--format", "{{.Server.Version}}"),
        isolatedDockerVersion: innerDocker("version", "--format", "{{.Server.Version}}"),
        isolatedDockerArchitecture: innerDocker("info", "--format", "{{.Architecture}}"),
        postgres: psql("SHOW server_version;"),
        schemaHead: migration.schema_head,
      },
      candidate: {
        dindImage,
        runscRelease: "release-20260622.0",
        runscSHA512,
        serverImage,
        registryImageID,
        sandboxImage: remoteSandboxImage,
        execdImage: remoteExecdImage,
      },
      remoteWorker: remoteWorkerReceipt,
      checks: [
        "shared-untrusted RuntimeProfile is accepted only for a Remote Worker reporting isolation-gvisor and network-internal-deny",
        "generated User API exposes only the published Profile summary and strips trust, runtime, Target and Network Policy authority",
        "the outbound Remote Worker receives shared-untrusted plus gvisor in the durable Sandbox command",
        "OpenSandbox creates the real workload with Docker runtime runsc on exactly one internal network with inter-container communication disabled",
        "the workload reports Linux 4.19.0-gvisor and gVisor startup through its emulated kernel",
        "the internal deny network blocks outbound HTTP without an OpenSandbox egress sidecar or browser-direct infrastructure connection",
        "Admin Sandbox metadata reports workload trust and isolation runtime without Prompt, file, Artifact or credential bytes",
        "generated Admin Stop deletes compute, releases the writer, retains then explicitly removes the owned Workspace volume",
        "zero test-owned inner runtime containers and Workspace volumes remain",
      ],
      boundary:
        "Disposable arm64 DinD customer node under OrbStack with fixed gVisor runsc, internal deny-only Docker network and PostgreSQL; outbound egress and Private Preview are intentionally unsupported for shared-untrusted workloads, and this does not qualify Kubernetes or external SSH strong isolation",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified shared-untrusted gVisor Sandbox and cleanup; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw remoteWorkerComplete;
  }
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
  assert.equal(remoteWorkerReceipt.deterministicPlacement, true);
  assert.match(remoteWorkerReceipt.selectedTargetId, /^rwt-[0-9a-f]{64}$/u);
  assert.equal(remoteWorkerReceipt.otherNodeSkipped, true);
  assert.equal(remoteWorkerReceipt.selectorArchitecture, "arm64");
  assert.equal(remoteWorkerReceipt.selectorUnavailableRejected, true);
  assert.equal(remoteWorkerReceipt.longClaimRenewed, true);
  assert.equal(remoteWorkerReceipt.renewWrongCommandStatus, 409);
  assert.equal(remoteWorkerReceipt.reconnectOriginalCommandNotReplayed, true);
  assert.equal(remoteWorkerReceipt.reconnectAttempt, 2);
  assert.equal(remoteWorkerReceipt.remoteWorkerFaultSoak.heartbeatCycles, 64);
  assert.equal(remoteWorkerReceipt.remoteWorkerFaultSoak.successfulHeartbeats, 64);
  assert.equal(remoteWorkerReceipt.remoteWorkerFaultSoak.successfulAdminReads, 64);
  assert.equal(remoteWorkerReceipt.remoteWorkerFaultSoak.rpo.operationRowsLost, 0);
  assert.equal(remoteWorkerReceipt.remoteWorkerFaultSoak.rpo.duplicateRuntimes, 0);
  assert.ok(remoteWorkerReceipt.remoteWorkerFaultSoak.heartbeatLatencyMilliseconds.p95 > 0);
  assert.ok(remoteWorkerReceipt.remoteWorkerFaultSoak.reconnectToSettlementMilliseconds > 0);
  assert.equal(remoteWorkerReceipt.gatewayRestart.protocolRecoveries, 4);
  assert.equal(remoteWorkerReceipt.gatewayRestart.rpo.fileBytesLost, 0);
  assert.equal(remoteWorkerReceipt.gatewayRestart.rpo.ptyOutputBytesLost, 0);
  assert.equal(remoteWorkerReceipt.gatewayRestart.rpo.grantRowsLost, 0);
  assert.ok(remoteWorkerReceipt.gatewayRestart.latencyMilliseconds.p95 > 0);
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
        head: execFileSync("git", ["rev-parse", "HEAD"], {
          cwd: root,
          encoding: "utf8",
        }).trim(),
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
        "two real enrolled arm64 nodes report different capacity; the fixed selector chooses the higher-capacity target, leaves the other node command-free, and keeps rebuild on the retained-volume target",
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
        "64 authenticated outbound heartbeats and 64 matching Admin reads completed with measured P50/P95/max latency after the dropped-connection recovery",
        "Preview, Files, PTY and SSH each recovered through a new Access Gateway process with measured recovery latency and zero verified Grant/file/PTY state loss",
        "create, stop, retained-volume rebuild and final cleanup leave zero test-owned runtime containers and Workspace volumes",
      ],
      boundary:
        "Local OrbStack Docker RemoteWorker customer nodes and disposable PostgreSQL only; deterministic selection, bounded 64-cycle mTLS heartbeat/Admin soak, dropped-connection reconciliation and four protocol Gateway restarts are covered with current-machine measurements, not an SLO; image-manifest architecture, strong-isolation runtime, streaming Preview, Kubernetes and external SSH customer nodes are not covered",
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
      env: {
        ...commonEnvironment,
        CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "prepare",
      },
      timeout: 180_000,
    },
  );
  prepareReceipt = parseMarker(prepareOutput, "FOUNDATION_LIVE_PREPARE");
  const controllerFaultStarted = process.hrtime.bigint();
  await delay(1200);
  const controllerRestartStarted = process.hrtime.bigint();
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
  const controllerFaultToRecoveryMilliseconds =
    Number(process.hrtime.bigint() - controllerFaultStarted) / 1e6;
  const controllerRestartToRecoveryMilliseconds =
    Number(process.hrtime.bigint() - controllerRestartStarted) / 1e6;
  const recoverReceipt = parseMarker(recoverOutput, "FOUNDATION_LIVE_RECOVER");
  assert.equal(recoverReceipt.runtimeId, prepareReceipt.runtimeId);
  assert.equal(recoverReceipt.operationId, prepareReceipt.operationId);
  assert.equal(
    recoverReceipt.cleanup,
    "failed runtime and volume removed; successful runtime retained for lifecycle",
  );
  assert.ok(recoverReceipt.workspaceVolumeUsage.usedBytes > 0);
  assert.equal(recoverReceipt.workspaceVolumeUsage.measurementGeneration, 1);
  assert.equal(recoverReceipt.networkUsage.source, "docker-container-stats-v1");
  assert.equal(recoverReceipt.networkUsage.state, "ready");
  assert.equal(recoverReceipt.networkUsage.measurementGeneration, 2);
  assert.ok(recoverReceipt.networkUsage.initialReceivedBytes > 0);
  assert.ok(recoverReceipt.networkUsage.initialTransmittedBytes > 0);
  assert.ok(
    recoverReceipt.networkUsage.receivedBytes >= recoverReceipt.networkUsage.initialReceivedBytes,
  );
  assert.ok(
    recoverReceipt.networkUsage.transmittedBytes >=
      recoverReceipt.networkUsage.initialTransmittedBytes,
  );
  assert.ok(
    recoverReceipt.networkUsage.receivedBytes > recoverReceipt.networkUsage.initialReceivedBytes ||
      recoverReceipt.networkUsage.transmittedBytes >
        recoverReceipt.networkUsage.initialTransmittedBytes,
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
  assert.ok(stopReceipt.usage.allocatedMilliseconds >= 120_000);
  assert.equal(
    stopReceipt.usage.cpuMillisMilliseconds,
    stopReceipt.usage.allocatedMilliseconds * 500,
  );
  assert.equal(
    stopReceipt.usage.memoryByteMilliseconds,
    stopReceipt.usage.allocatedMilliseconds * 536_870_912,
  );
  assert.equal(stopReceipt.usage.checkpointedAt, stopReceipt.usage.finalizedAt);
  assert.equal(stopReceipt.workspaceVolumeUsage.source, "docker-system-df-v1");
  assert.equal(stopReceipt.workspaceVolumeUsage.state, "ready");
  assert.ok(stopReceipt.workspaceVolumeUsage.usedBytes > 0);
  assert.ok(stopReceipt.workspaceVolumeUsage.measurementGeneration >= 2);
  assert.ok(
    Date.parse(stopReceipt.workspaceVolumeUsage.checkpointedAt) >
      Date.parse(recoverReceipt.workspaceVolumeUsage.checkpointedAt),
  );
  assert.equal(stopReceipt.networkUsage.source, "docker-container-stats-v1");
  assert.equal(stopReceipt.networkUsage.state, "ready");
  assert.equal(stopReceipt.networkUsage.latestRuntimeGeneration, 1);
  assert.equal(stopReceipt.networkUsage.measurementGeneration, 2);
  assert.equal(stopReceipt.networkUsage.receivedBytes, recoverReceipt.networkUsage.receivedBytes);
  assert.equal(
    stopReceipt.networkUsage.transmittedBytes,
    recoverReceipt.networkUsage.transmittedBytes,
  );
  assert.equal(stopReceipt.networkUsage.checkpointedAt, recoverReceipt.networkUsage.checkpointedAt);
  const snapshotAPIReceipt = parseMarker(
    execFileSync(
      serverTestBinary,
      ["-test.run", "^TestFoundationWorkspaceSnapshotPostgres$", "-test.v"],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
          CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
        },
        timeout: 120_000,
      },
    ),
    "FOUNDATION_SNAPSHOT_API",
  );
  const snapshotControllerReceipt = parseMarker(
    execFileSync(
      controllerTestBinary,
      ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
      {
        encoding: "utf8",
        env: { ...commonEnvironment, CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "snapshot" },
        timeout: 180_000,
      },
    ),
    "FOUNDATION_LIVE_SNAPSHOT",
  );
  assert.equal(snapshotControllerReceipt.operationId, snapshotAPIReceipt.operationId);
  assert.equal(snapshotControllerReceipt.status, "available");
  if (faultSoakOnly) {
    const runAdminSoak = (phase) =>
      parseMarker(
        execFileSync(
          serverTestBinary,
          ["-test.run", "^TestFoundationAdminReadSoakPostgres$", "-test.v"],
          {
            encoding: "utf8",
            env: {
              ...process.env,
              CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
              CLOUD_AGENTS_FOUNDATION_ADMIN_SOAK_PHASE: phase,
            },
            timeout: 120_000,
          },
        ),
        "FOUNDATION_ADMIN_SOAK",
      );
    const beforeRestart = runAdminSoak("before-restart");
    const afterRestart = runAdminSoak("after-restart");
    assert.equal(beforeRestart.cycles, 64);
    assert.equal(beforeRestart.successfulRequests, 384);
    assert.equal(beforeRestart.deniedRequests, 64);
    assert.equal(afterRestart.cycles, 64);
    assert.equal(afterRestart.successfulRequests, 384);
    assert.equal(afterRestart.deniedRequests, 64);
    assert.equal(afterRestart.stateDigest, beforeRestart.stateDigest);
    docker("volume", "rm", snapshotControllerReceipt.physicalVolume);
    docker("volume", "rm", prepareReceipt.volumeName);
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
    assert.equal(
      docker(
        "volume",
        "ls",
        "-q",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace-snapshot",
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
        head: execFileSync("git", ["rev-parse", "HEAD"], {
          cwd: root,
          encoding: "utf8",
        }).trim(),
        dirty: true,
        migrationHead: currentHead,
      },
      backend: {
        dockerContext: "orbstack",
        dockerVersion: docker("version", "--format", "{{.Server.Version}}"),
        postgres: psql("SHOW server_version;"),
      },
      controllerFault: {
        injected: "Controller process exited after physical create and before settlement",
        claimExpiryWaitMilliseconds: 1200,
        faultToRecoveryUpperBoundMilliseconds: controllerFaultToRecoveryMilliseconds,
        restartToRecoveryUpperBoundMilliseconds: controllerRestartToRecoveryMilliseconds,
        inProcessRecoveryUpperBoundMilliseconds: recoverReceipt.recoveryMilliseconds,
        rpo: {
          operationRowsLost: 0,
          workspaceBytesLost: 0,
          operationId: recoverReceipt.operationId,
          workspaceDigest: recoverReceipt.workspaceDigest,
        },
        deliveryAttempts: recoverReceipt.deliveryAttempts,
        failedRuntimeCompensated: recoverReceipt.failureCompensated,
      },
      controlPlaneSoak: {
        beforeRestart,
        afterRestart,
        totalRequests: 896,
        rpo: { resourceChanges: 0, stateDigest: afterRestart.stateDigest },
        rtoMilliseconds: afterRestart.recoveryToFirstSuccessMilliseconds,
      },
      snapshot: { api: snapshotAPIReceipt, controller: snapshotControllerReceipt },
      checks: [
        `product migration ${currentHead} applied to disposable PostgreSQL`,
        "a Controller process exited after physical create and before settlement; a new process reaped the expired claim, adopted the exact runtime and operation, preserved Workspace bytes, and compensated a separate failed runtime",
        "two separate Control Plane test processes each served 64 cycles of six generated-SDK Admin reads plus an ordinary-user 403 against the same PostgreSQL authority",
        "all 768 successful Admin reads and 128 denied reads completed; P50/P95/max and process-start-to-first-success recovery were measured for each process",
        "the persisted Profile, Sandbox and Snapshot projection digest was stable within the soak and unchanged across the Control Plane restart",
        "Admin responses excluded endpoint, credential references, physical snapshot identifiers, Prompt and Workspace content markers",
        "the stopped real Docker Workspace was copied to an exact-owned Snapshot before the read soak",
        "zero test-owned runtime containers, Workspace volumes and Snapshot volumes remain",
      ],
      boundary:
        "Local OrbStack Docker, disposable PostgreSQL and in-process HTTP servers using production handlers/generated SDK only; this bounded 896-request read soak measures current-machine latency and Docker Controller/Control Plane restart recovery, not an SLO, write saturation, Kubernetes/SSH/RemoteWorker faults, external load balancers or multi-Region recovery",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified Docker Controller and Control Plane fault/soak; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw snapshotComplete;
  }
  if (snapshotCleanupOnly) {
    const cleanupAPIOutput = execFileSync(
      serverTestBinary,
      ["-test.run", "^TestFoundationWorkspaceSnapshotCleanupPostgres$", "-test.v"],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
          CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          CLOUD_AGENTS_FOUNDATION_BROWSER_OUTPUT:
            process.env.CLOUD_AGENTS_FOUNDATION_BROWSER_OUTPUT ?? evidenceDirectory,
        },
        timeout: 120_000,
      },
    );
    const cleanupAPIReceipt = parseMarker(cleanupAPIOutput, "FOUNDATION_SNAPSHOT_CLEANUP_API");
    const browserReceipt = process.env.CLOUD_AGENTS_FOUNDATION_BROWSER_SCRIPT
      ? parseMarker(cleanupAPIOutput, "FOUNDATION_SNAPSHOT_BROWSER")
      : undefined;
    const cleanupControllerReceipt = parseMarker(
      execFileSync(
        controllerTestBinary,
        ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
        {
          encoding: "utf8",
          env: { ...commonEnvironment, CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "snapshot-cleanup" },
          timeout: 180_000,
        },
      ),
      "FOUNDATION_LIVE_SNAPSHOT_CLEANUP",
    );
    assert.equal(cleanupControllerReceipt.operationId, cleanupAPIReceipt.operationId);
    assert.equal(cleanupControllerReceipt.status, "deleted");
    const expiryAPIReceipt = parseMarker(
      execFileSync(
        serverTestBinary,
        ["-test.run", "^TestFoundationWorkspaceSnapshotExpiryPostgres$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
            CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          },
          timeout: 120_000,
        },
      ),
      "FOUNDATION_SNAPSHOT_EXPIRY_API",
    );
    const expiryControllerReceipt = parseMarker(
      execFileSync(
        controllerTestBinary,
        ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
        {
          encoding: "utf8",
          env: { ...commonEnvironment, CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "snapshot-expiry" },
          timeout: 180_000,
        },
      ),
      "FOUNDATION_LIVE_SNAPSHOT_EXPIRY",
    );
    assert.equal(expiryControllerReceipt.status, "deleted");
    assert.equal(expiryControllerReceipt.trigger, "retention");
    docker("volume", "rm", prepareReceipt.volumeName);
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
    assert.equal(
      docker(
        "volume",
        "ls",
        "-q",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace-snapshot",
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
        migrationHead: currentHead,
      },
      backend: {
        dockerContext: "orbstack",
        dockerVersion: docker("version", "--format", "{{.Server.Version}}"),
        postgres: psql("SHOW server_version;"),
      },
      stop: { api: stopAPIReceipt, controller: stopReceipt },
      snapshot: { api: snapshotAPIReceipt, controller: snapshotControllerReceipt },
      manualCleanup: { api: cleanupAPIReceipt, controller: cleanupControllerReceipt },
      retentionCleanup: { api: expiryAPIReceipt, controller: expiryControllerReceipt },
      ...(browserReceipt === undefined ? {} : { adminBrowser: browserReceipt }),
      checks: [
        `product migration ${currentHead} applied to disposable PostgreSQL`,
        "new Snapshot retention is fixed at database-clock acceptance and exposed only as metadata",
        "Admin usage correction required admin scope and immutable fences; replay was stable, stale writes returned 409 and raw facts remained unchanged",
        "ordinary user token received 403 and stale resourceVersion received 409 from Admin cleanup",
        "manual cleanup required exact logical Snapshot and source Workspace confirmation without physical volume disclosure",
        "manual cleanup replay returned the same durable Operation",
        "terminal cleanup failure was claim-renewal fenced and retried with a new resource version and outbox sequence",
        "database-clock expiry entered the same cleanup Operation state machine",
        "Controller deleted only exact platform-owned Docker snapshot volumes under renewable claims",
        "manual and retention cleanup settled Snapshot deleted, Operation succeeded/complete, terminal receipt and Audit",
        ...(browserReceipt === undefined
          ? []
          : [
              "real Chromium connected through the Vite Admin origin and rendered persisted retention plus Sandbox usage-correction metadata and its fenced form",
              browserReceipt.fullCapture === undefined
                ? "Admin locale and theme switched and persisted without storing the bearer token; desktop English and mobile Chinese screenshots were captured with no console errors"
                : `${browserReceipt.fullCapture.screenshots} live screenshots covered eight locale/theme/viewport accessibility matrices and ${browserReceipt.fullCapture.referenceMatches} fixed Daytona structural matches; only ${browserReceipt.fullCapture.expectedPreviewFailures} deliberate cleanup-preview failures occurred`,
              "all browser HTTP requests remained on the Admin Web origin and reached infrastructure authority only through the Control Plane proxy",
            ]),
        "zero test-owned runtime containers, Workspace volumes and Snapshot volumes",
      ],
      boundary:
        browserReceipt === undefined
          ? "Local OrbStack Docker and disposable PostgreSQL only; manual and database-clock retention cleanup are verified, while Kubernetes/SSH snapshot backends and browser visual QA remain unverified"
          : browserReceipt.fullCapture === undefined
            ? "Local OrbStack Docker, disposable PostgreSQL and Chromium Admin Web only; immutable usage correction, manual/database-clock retention cleanup and this flow's desktop/mobile UI are verified, while Kubernetes/SSH snapshot backends and full BASE-ADMIN-V1 visual/accessibility regression remain unverified"
            : "Local OrbStack Docker, disposable PostgreSQL and Chromium Admin Web only; immutable usage correction, manual/database-clock retention cleanup and the full bilingual light/dark desktop/mobile Admin visual/accessibility matrix are verified against fixed Daytona references, while Kubernetes/SSH snapshot backends and aggregate BASE-ADMIN-V1 infrastructure acceptance remain unverified",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified Docker Workspace Snapshot retention cleanup; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw snapshotComplete;
  }
  if (snapshotOnly) {
    const snapshotInfo = JSON.parse(
      docker("volume", "inspect", snapshotControllerReceipt.physicalVolume),
    )[0];
    assert.equal(
      snapshotInfo.Labels?.["cloud-agents.dev/resource"],
      "foundation-workspace-snapshot",
    );
    assert.equal(snapshotInfo.Labels?.["cloud-agents.dev/snapshot"], "snapshot");
    docker("volume", "rm", snapshotControllerReceipt.physicalVolume);
    docker("volume", "rm", prepareReceipt.volumeName);
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
    assert.equal(
      docker(
        "volume",
        "ls",
        "-q",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace-snapshot",
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
        migrationHead: currentHead,
      },
      backend: {
        dockerContext: "orbstack",
        dockerVersion: docker("version", "--format", "{{.Server.Version}}"),
        postgres: psql("SHOW server_version;"),
      },
      prepare: prepareReceipt,
      recover: recoverReceipt,
      stop: { api: stopAPIReceipt, controller: stopReceipt },
      snapshot: { api: snapshotAPIReceipt, controller: snapshotControllerReceipt },
      checks: [
        `product migration ${currentHead} applied to disposable PostgreSQL`,
        "ordinary user token received 403 from Admin Workspace Snapshot API",
        "Admin create replay returned the same durable Operation and metadata-only responses excluded physical volume, digest, endpoint and credentials",
        "source Sandbox was stopped, observed stopped and writer-released before acceptance",
        "database-clock CPU and memory allocation facts caught up from a stale checkpoint after Controller process restart and froze in the Stop transaction",
        "Controller sampled exact-owned Docker backend occupied bytes without opening Workspace files and durably caught up a stale measurement after process restart",
        "Controller sampled only aggregate cumulative RX/TX bytes from exact-owned running Docker container stats and durably caught up generation 1 to 2 after process restart",
        "generated Admin Sandbox detail exposed only Workspace usage source, state, generation, byte count and timestamps; ordinary User APIs remained unchanged",
        "generated Admin Sandbox detail exposed only network source, runtime and measurement generations, aggregate byte counters, state and timestamps; no packet, address, destination or payload data entered the contract",
        "generated Admin usage correction required admin scope, exact generation/resource version, target confirmation and idempotency; replay was stable, stale writes returned 409 and raw facts remained unchanged",
        "acceptance reserved the existing single-writer slot until terminal settlement; Admin rebuild returned 409 while the snapshot was pending",
        "Controller claimed the operation and copied the real Docker Workspace through a never-started helper",
        "normalized path, type, mode, link target and file-byte archive digests matched after Docker unpack and repack",
        "snapshot Operation settled succeeded/complete and the snapshot metadata settled available",
        "terminal settlement released the source Workspace writer fence",
        "exact-owned helper, snapshot volume, runtime containers and Workspace volume were removed after verification",
      ],
      boundary:
        "Local OrbStack Docker and disposable PostgreSQL only; usage corrections are operational deltas, not prices or billable amounts; stopped runtime counters are last successful samples, and Kubernetes/RemoteWorker storage or network measurement, restore, retention policy and browser visual QA remain unverified",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified offline Docker Workspace Snapshot; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw snapshotComplete;
  }
  if (snapshotRestoreOnly) {
    const restoreAPIReceipt = parseMarker(
      execFileSync(
        serverTestBinary,
        ["-test.run", "^TestFoundationWorkspaceRestorePostgres$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
            CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
          },
          timeout: 120_000,
        },
      ),
      "FOUNDATION_RESTORE_API",
    );
    const restoreControllerReceipt = parseMarker(
      execFileSync(
        controllerTestBinary,
        ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...commonEnvironment,
            CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "restore",
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_PROOF_DIGEST: prepareReceipt.proofDigest,
          },
          timeout: 180_000,
        },
      ),
      "FOUNDATION_LIVE_RESTORE",
    );
    assert.equal(restoreControllerReceipt.operationId, restoreAPIReceipt.operationId);
    assert.equal(restoreControllerReceipt.workspaceDigest, prepareReceipt.proofDigest);
    assert.equal(restoreControllerReceipt.status, "running");
    assert.equal(
      docker(
        "ps",
        "-aq",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace-restore",
      ),
      "",
    );
    const restoreStopAPIReceipt = parseMarker(
      execFileSync(
        serverTestBinary,
        ["-test.run", "^TestFoundationSandboxLifecyclePostgres$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_FOUNDATION_PROFILE_RUNTIME_DATABASE_URL: runtimeURL,
            CLOUD_AGENTS_FOUNDATION_PROFILE_OWNER_DATABASE_URL: migrationURL,
            CLOUD_AGENTS_FOUNDATION_LIFECYCLE_ACTION: "stop",
            CLOUD_AGENTS_FOUNDATION_LIFECYCLE_SANDBOX_ID: "sandbox-restored",
            CLOUD_AGENTS_FOUNDATION_LIFECYCLE_MARKER: "FOUNDATION_RESTORE_STOP_API",
          },
          timeout: 120_000,
        },
      ),
      "FOUNDATION_RESTORE_STOP_API",
    );
    const restoreStopControllerReceipt = parseMarker(
      execFileSync(
        controllerTestBinary,
        ["-test.run", "^TestLiveFoundationControllerRestart$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...commonEnvironment,
            CLOUD_AGENTS_FOUNDATION_LIVE_PHASE: "restore-stop",
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_RUNTIME_ID: restoreControllerReceipt.runtimeId,
            CLOUD_AGENTS_FOUNDATION_LIVE_EXPECTED_VOLUME_NAME: restoreControllerReceipt.volumeName,
            CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_OPERATION_ID: restoreControllerReceipt.operationId,
            CLOUD_AGENTS_FOUNDATION_LIVE_PRIOR_SPEC_DIGEST: restoreControllerReceipt.specDigest,
          },
          timeout: 180_000,
        },
      ),
      "FOUNDATION_LIVE_RESTORE_STOP",
    );
    assert.equal(restoreStopControllerReceipt.generation, restoreStopAPIReceipt.generation);
    docker("volume", "rm", snapshotControllerReceipt.physicalVolume);
    docker("volume", "rm", prepareReceipt.volumeName);
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
    assert.equal(
      docker(
        "volume",
        "ls",
        "-q",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace-snapshot",
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
        migrationHead: currentHead,
      },
      backend: {
        dockerContext: "orbstack",
        dockerVersion: docker("version", "--format", "{{.Server.Version}}"),
        postgres: psql("SHOW server_version;"),
      },
      snapshot: { api: snapshotAPIReceipt, controller: snapshotControllerReceipt },
      restore: { api: restoreAPIReceipt, controller: restoreControllerReceipt },
      stop: { api: restoreStopAPIReceipt, controller: restoreStopControllerReceipt },
      checks: [
        `product migration ${currentHead} applied to disposable PostgreSQL`,
        "ordinary user token received 403 and stale snapshot resourceVersion received 409 from the Admin restore API",
        "Admin restore replay returned the same durable Sandbox Operation and response excluded snapshot internals, endpoint and credentials",
        "restore acceptance bound the available snapshot, fixed same-Target published RuntimeProfile, new Workspace and new Sandbox under the existing single-writer fence",
        "Controller verified exact snapshot ownership and content digest, copied through a never-started helper, and mounted only the new deterministic Workspace volume",
        "the restored real Sandbox returned the original proof-file SHA-256 from /workspace",
        "the existing Sandbox stop lifecycle deleted restored compute, released its writer and retained the restored volume until explicit test cleanup",
        "exact-owned restore helper, snapshot volume, source/restored Workspace volumes and runtime containers were removed after verification",
      ],
      boundary:
        "Local OrbStack Docker and disposable PostgreSQL only; offline restore to a new same-Target Workspace/Sandbox is verified, while retention cleanup policy, Kubernetes/SSH snapshot backends and browser visual QA remain unverified",
    };
    writeFileSync(
      resolve(evidenceDirectory, "evidence.json"),
      JSON.stringify(evidence, null, 2) + "\n",
    );
    process.stdout.write(
      `Verified offline Docker Workspace Snapshot restore; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
    );
    throw snapshotComplete;
  }
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
  const snapshotInfo = JSON.parse(
    docker("volume", "inspect", snapshotControllerReceipt.physicalVolume),
  )[0];
  assert.equal(snapshotInfo.Labels?.["cloud-agents.dev/resource"], "foundation-workspace-snapshot");
  assert.equal(snapshotInfo.Labels?.["cloud-agents.dev/snapshot"], "snapshot");
  docker("volume", "rm", snapshotControllerReceipt.physicalVolume);
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
      head: execFileSync("git", ["rev-parse", "HEAD"], {
        cwd: root,
        encoding: "utf8",
      }).trim(),
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
    snapshot: { api: snapshotAPIReceipt, controller: snapshotControllerReceipt },
    rebuild: { api: rebuildAPIReceipt, controller: rebuildReceipt },
    exec: execReceipt,
    pty: ptyReceipt,
    ttl: ttlReceipt,
    finalRebuild: {
      api: finalRebuildAPIReceipt,
      controller: finalRebuildReceipt,
    },
    checks: [
      "public RuntimeProfile and Sandbox admission",
      "outbound customer-node RemoteWorker claims and physically creates a real Sandbox through the existing durable Operation",
      "RemoteWorker settlement and exact receipt replay persist Running state without endpoint or credential bytes in the command",
      "Admin Stop dispatches the exact prior runtime and retained Workspace volume to the authenticated RemoteWorker",
      "RemoteWorker physically deletes compute, settles Stopped with writer release, and accepts exact Stop receipt replay",
      "Admin Rebuild dispatches the retained Workspace volume and creates a new customer-node runtime",
      "RemoteWorker Rebuild preserves Workspace bytes, settles Running, accepts exact receipt replay, and is stopped without compute residue",
      "generated Admin Sandbox list/detail, ordinary-user 403, and response redaction",
      "generated Admin Workspace Snapshot create/list/detail, ordinary-user 403, idempotent replay, and metadata-only response",
      "offline writer fence, durable claim, stopped helper, Docker archive copy, byte-for-byte archive verification, and exact-owned snapshot volume",
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
      "database-clock Sandbox CPU and memory allocation facts catch up from a stale checkpoint after Controller process restart and freeze in the Stop transaction",
      "Controller reads exact-owned Docker backend occupied bytes without opening Workspace files, durably checkpoints them, and catches up a stale checkpoint after process restart",
      "generated Admin Sandbox detail exposes only Workspace measurement source, state, generation, byte count and timestamps; ordinary User APIs remain unchanged",
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
      "Local OrbStack Docker customer-node process and disposable PostgreSQL only; Workspace occupied-byte measurement is not an enforced quota or billable amount; Kubernetes and RemoteWorker storage measurement, browser visual QA, deployment, image publication and external SSH nodes are not covered",
  };
  writeFileSync(
    resolve(evidenceDirectory, "evidence.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  process.stdout.write(
    `Verified Controller restart/adoption and failure compensation; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
  );
} catch (error) {
  if (error !== remoteWorkerComplete && error !== snapshotComplete && error !== sshProbeComplete)
    throw error;
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
      [
        "workspace",
        "workspace-restored",
        "workspace-terminal",
        "workspace-foreign",
        "remote-workspace",
      ].includes(info.Labels?.["cloud-agents.dev/workspace"])
    ) {
      docker("volume", "rm", volume);
    }
  }
  for (const volume of docker(
    "volume",
    "ls",
    "-q",
    "--filter",
    "label=cloud-agents.dev/resource=foundation-workspace-snapshot",
  )
    .split("\n")
    .filter(Boolean)) {
    const info = JSON.parse(docker("volume", "inspect", volume))[0];
    if (
      info.Labels?.["cloud-agents.dev/tenant"] === "tenant" &&
      info.Labels?.["cloud-agents.dev/project"] === "project" &&
      ["snapshot", "snapshot-expiring"].includes(info.Labels?.["cloud-agents.dev/snapshot"])
    ) {
      docker("volume", "rm", volume);
    }
  }
  if (dindStarted && innerDocker !== undefined) {
    try {
      for (const container of innerDocker("ps", "-aq", "--filter", "label=opensandbox.io/id")
        .split("\n")
        .filter(Boolean)) {
        innerDocker("rm", "-f", container);
      }
      for (const volume of innerDocker(
        "volume",
        "ls",
        "-q",
        "--filter",
        "label=cloud-agents.dev/resource=foundation-workspace",
      )
        .split("\n")
        .filter(Boolean)) {
        const info = JSON.parse(innerDocker("volume", "inspect", volume))[0];
        if (
          info.Labels?.["cloud-agents.dev/tenant"] === "tenant" &&
          info.Labels?.["cloud-agents.dev/project"] === "project"
        ) {
          innerDocker("volume", "rm", volume);
        }
      }
    } catch {}
  }
  if (sandboxServerStarted) docker("rm", "-f", "-v", sandboxServerName);
  if (dindStarted) {
    docker("rm", "-f", "-v", dindName);
    docker("volume", "rm", dindSocketVolume);
  }
  for (const sink of startedSinks) docker("rm", "-f", sink);
  if (postgresStarted) docker("rm", "-f", "-v", postgresName);
  rmSync(build, { recursive: true, force: true });
  assert.equal(
    docker("ps", "-aq", "--filter", `label=cloud-agents-foundation-controller-test=${run}`),
    "",
  );
}
