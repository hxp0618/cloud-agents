import assert from "node:assert/strict";
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import {
  chmodSync,
  closeSync,
  existsSync,
  lstatSync,
  mkdtempSync,
  openSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { request as httpsRequest } from "node:https";
import { tmpdir } from "node:os";
import { isAbsolute, join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const root = resolve(import.meta.dirname, "../..");
const runID = randomBytes(6).toString("hex");
const databaseContainer = `cloud-agents-identity-web-db-${runID}`;
const databaseName = "identity_web_flow_test";
const databasePort = 35695;
const keycloakContainer = `cloud-agents-identity-keycloak-e2e-${runID}`;
const keycloakImage =
  "quay.io/keycloak/keycloak@sha256:b0f60d489d51c5d113390bdf5461d4c06e6051be026c05549f2e1e10ec352bcc";
const gitlabContainer = `cloud-agents-identity-gitlab-e2e-${runID}`;
const gitlabImage =
  "gitlab/gitlab-ce@sha256:9b33b45b9f42d176bada85ee5ecb81ddab7e506c435f44cd582206e284b2809c";
const gitlabVolumes = Object.freeze([
  `cloud-agents-identity-gitlab-e2e-config-${runID}`,
  `cloud-agents-identity-gitlab-e2e-logs-${runID}`,
  `cloud-agents-identity-gitlab-e2e-data-${runID}`,
]);
const providerFixture = process.env.CLOUD_AGENTS_IDENTITY_E2E_PROVIDER ?? "keycloak";
assert.ok(
  providerFixture === "keycloak" ||
    providerFixture === "gitlab" ||
    providerFixture === "feishu" ||
    providerFixture === "github",
  "CLOUD_AGENTS_IDENTITY_E2E_PROVIDER must be keycloak, gitlab, feishu or github",
);
const humanProvider = providerFixture === "feishu" || providerFixture === "github";
const ports = Object.freeze({
  identity: 35711,
  controlPlane: 35712,
  admin: 35713,
  user: 35714,
  chromium: 35715,
  keycloak: 35716,
  gitlab: 35717,
});
const origins = Object.freeze({
  identity: `https://identity.localhost:${ports.identity}`,
  controlPlane: `https://control-plane.localhost:${ports.controlPlane}`,
  admin: `https://admin.localhost:${ports.admin}`,
  user: `https://user.localhost:${ports.user}`,
  keycloak: `https://keycloak.localhost:${ports.keycloak}`,
  gitlab: `https://gitlab.localhost:${ports.gitlab}`,
});
const password = "identity web correct horse battery staple";
const invitedPassword = "invited account correct horse battery staple";
const changedPassword = `${password} changed`;
const resetPassword = `${password} reset`;
const providerHumanMarker = join(root, "test/e2e/.tmp", `identity-provider-human-${runID}.json`);
const providerPassword = randomBytes(24).toString("base64url");
const enabledProviderPassword = randomBytes(24).toString("base64url");
const evidenceDirectory = join(root, "test/e2e/.tmp/identity-web");
const providerEvidenceDirectory = join(root, "test/e2e/.tmp/identity-provider");
const providerChromeProxy = humanProvider ? configuredLocalHTTPSProxy(process.env) : undefined;
const directory = mkdtempSync(join(tmpdir(), "cloud-agents-identity-web-"));
const memberRequestIDsFile = join(directory, "member-request-ids.json");
const children = [];
const ownedContainers = [];
const ownedVolumes = [];
const secretValues = [];
let keepHumanProviderEvidence = false;
let ownsProviderEvidence = false;
const oauthCallbackProofPattern =
  /(?:[?&]|&amp;|%3f|%26|\\u0026)(?:code|state)(?:=|%3d)[^&\s"'<>]+/iu;
const oauthJSONProofPattern = /"(?:code|state)"\s*:\s*"[A-Za-z0-9._~-]{16,}"/u;
const githubCredentialPattern =
  /\b(?:gh[opurs]_[A-Za-z0-9]{20,255}|github_pat_[A-Za-z0-9_]{20,255})\b/u;
const proxyEnvironment = Object.fromEntries(
  ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"]
    .filter((name) => process.env[name] !== undefined)
    .map((name) => [name, process.env[name]]),
);

function privateProviderFailureMessage(error) {
  const output =
    typeof error === "object" && error !== null
      ? ["stdout", "stderr"]
          .map((field) => (typeof error[field] === "string" ? error[field] : ""))
          .join("\n")
      : "";
  const location = /identity-provider\.e2e\.ts:(\d{1,6}):(\d{1,6})/u.exec(output);
  const failureClasses = [
    ["ASSERTION_FAILED", /\bASSERTION_FAILED\b/u],
    ["ACTION_FAILED", /\bACTION_FAILED\b/u],
    ["TIMEOUT", /\b(?:TIMEOUT|TimeoutError)\b/u],
    ["AssertionError", /\bAssertionError\b/u],
    ["TypeError", /\bTypeError\b/u],
    ["SyntaxError", /\bSyntaxError\b/u],
  ];
  const assertions = [
    "toBeDefined",
    "toBeDisabled",
    "toBeEnabled",
    "toBeVisible",
    "toHaveAttribute",
    "toHaveValue",
    "toBeNull",
    "toBe",
    "waitForResponse",
    "waitForURL",
    "selectOption",
    "fill",
    "tap",
    "goto",
  ];
  const failureClass = failureClasses.find(([, pattern]) => pattern.test(output))?.[0];
  const assertion = assertions.find((name) => output.includes(name));
  const stages = [
    ...output.matchAll(
      /PROVIDER_STAGE provider=(feishu|github) stage=(invitation|reauthentication|link|login|rejected-login)/gu,
    ),
  ];
  const callbacks = [
    ...output.matchAll(
      /PROVIDER_CALLBACK provider=(feishu|github) stage=(invitation|reauthentication|link|login|rejected-login) outcome=(signed-in|reauthenticated|linked|failed)/gu,
    ),
  ];
  const activeStage = stages.at(-1);
  const completedCallback = callbacks
    .filter(
      (match) =>
        activeStage !== undefined &&
        match.index > activeStage.index &&
        match[1] === activeStage[1] &&
        match[2] === activeStage[2],
    )
    .at(-1);
  const callbackRequest = [
    ...output.matchAll(
      /PROVIDER_CALLBACK_REQUEST provider=(feishu|github) stage=(invitation|reauthentication|link|login|rejected-login) has_code=(true|false) has_state=(true|false) has_error=(true|false) has_authCode=(true|false) has_iss=(true|false) returned_to_expected_origin=(true|false)/gu,
    ),
  ]
    .filter(
      (match) =>
        activeStage !== undefined &&
        match.index > activeStage.index &&
        match[1] === activeStage[1] &&
        match[2] === activeStage[2],
    )
    .at(-1);
  return [
    "private provider subprocess failed; output withheld",
    location === null
      ? "source=unavailable"
      : `source=identity-provider.e2e.ts:${location[1]}:${location[2]}`,
    `category=${failureClass ?? "UNCLASSIFIED"}`,
    `assertion=${assertion ?? "UNAVAILABLE"}`,
    `provider=${activeStage?.[1] ?? "UNAVAILABLE"}`,
    `stage=${activeStage?.[2] ?? "UNAVAILABLE"}`,
    `outcome=${completedCallback?.[3] ?? "UNAVAILABLE"}`,
    ...(callbackRequest === undefined
      ? []
      : [
          `has_code=${callbackRequest[3]}`,
          `has_state=${callbackRequest[4]}`,
          `has_error=${callbackRequest[5]}`,
          `has_authCode=${callbackRequest[6]}`,
          `has_iss=${callbackRequest[7]}`,
          `expected_origin=${callbackRequest[8]}`,
        ]),
  ].join("; ");
}

assert.equal(
  privateProviderFailureMessage({
    stdout:
      "PROVIDER_STAGE provider=github stage=invitation\nPROVIDER_CALLBACK provider=github stage=invitation outcome=signed-in\nAssertionError toHaveValue identity-provider.e2e.ts:321:9 private-payload-must-not-copy",
    stderr: "",
  }),
  "private provider subprocess failed; output withheld; source=identity-provider.e2e.ts:321:9; category=AssertionError; assertion=toHaveValue; provider=github; stage=invitation; outcome=signed-in",
);
assert.equal(
  privateProviderFailureMessage({
    stdout:
      "PROVIDER_STAGE provider=feishu stage=reauthentication\nPROVIDER_CALLBACK provider=feishu stage=reauthentication outcome=reauthenticated\nPROVIDER_STAGE provider=feishu stage=rejected-login\nACTION_FAILED private-payload-must-not-copy",
    stderr: "",
  }),
  "private provider subprocess failed; output withheld; source=unavailable; category=ACTION_FAILED; assertion=UNAVAILABLE; provider=feishu; stage=rejected-login; outcome=UNAVAILABLE",
);
assert.equal(
  privateProviderFailureMessage({
    stdout:
      "PROVIDER_STAGE provider=github stage=invitation\nPROVIDER_CALLBACK_REQUEST provider=github stage=invitation has_code=true has_state=true has_error=false has_authCode=false has_iss=false returned_to_expected_origin=true\nsecret-must-not-copy",
    stderr: "",
  }),
  "private provider subprocess failed; output withheld; source=unavailable; category=UNCLASSIFIED; assertion=UNAVAILABLE; provider=github; stage=invitation; outcome=UNAVAILABLE; has_code=true; has_state=true; has_error=false; has_authCode=false; has_iss=false; expected_origin=true",
);

function run(file, args, options = {}) {
  const { privateFailure = false, ...commandOptions } = options;
  try {
    return execFileSync(file, args, {
      cwd: root,
      encoding: "utf8",
      timeout: 180_000,
      stdio: [commandOptions.input === undefined ? "ignore" : "pipe", "pipe", "pipe"],
      ...commandOptions,
    }).trim();
  } catch (error) {
    if (privateFailure) throw new Error(privateProviderFailureMessage(error));
    throw error;
  }
}

function dockerPSQL(query, database = "postgres", user = "postgres") {
  return run(
    "docker",
    [
      "exec",
      "-i",
      databaseContainer,
      "psql",
      "-XAt",
      "-v",
      "ON_ERROR_STOP=1",
      "-U",
      user,
      "-d",
      database,
    ],
    { input: query },
  );
}

async function startDatabase() {
  ownedContainers.push(databaseContainer);
  const container = run("docker", [
    "run",
    "--detach",
    "--name",
    databaseContainer,
    "--label",
    "cloud-agents-identity-web-e2e=owned",
    "--publish",
    `127.0.0.1:${databasePort}:5432`,
    "--env",
    "POSTGRES_HOST_AUTH_METHOD=trust",
    "postgres:17.6-bookworm",
  ]);
  assert.ok(container.length >= 12, "owned PostgreSQL container did not start");
  for (let attempt = 0; attempt < 200; attempt++) {
    const ready = spawnSync(
      "docker",
      [
        "exec",
        databaseContainer,
        "pg_isready",
        "--host=127.0.0.1",
        "--port=5432",
        "--username=postgres",
        "--dbname=postgres",
        "--quiet",
      ],
      { cwd: root, stdio: "ignore" },
    );
    if (ready.status === 0) {
      assert.equal(dockerPSQL("SELECT 1"), "1", "owned PostgreSQL query check failed");
      break;
    }
    if (attempt === 199) throw new Error("owned PostgreSQL container did not become ready");
    await delay(100);
  }
  for (const roleBootstrap of ["roles.sql", "roles_identity_service.sql"])
    run(
      "docker",
      [
        "exec",
        "-i",
        databaseContainer,
        "psql",
        "-X",
        "-v",
        "ON_ERROR_STOP=1",
        "-U",
        "postgres",
        "-d",
        "postgres",
      ],
      {
        input: readFileSync(
          join(root, "services/control-plane/migrations/bootstrap", roleBootstrap),
        ),
      },
    );
  dockerPSQL(`
CREATE ROLE identity_engine_migration LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE identity_bootstrap_test LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE identity_service_test LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE identity_runtime_test LOGIN INHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT cloud_agents_migration_owner TO identity_engine_migration;
GRANT cloud_agents_bootstrap_admin TO identity_bootstrap_test;
GRANT cloud_agents_identity_service TO identity_service_test;
GRANT cloud_agents_runtime TO identity_runtime_test;
`);
}

function writePrivate(name, contents) {
  const path = join(directory, name);
  writeFileSync(path, contents, { mode: 0o600 });
  chmodSync(path, 0o600);
  return path;
}

function writeJSON(name, value) {
  return writePrivate(name, JSON.stringify(value));
}

function readPrivateFile(path, label, maximumBytes) {
  assert.ok(isAbsolute(path), `${label} path must be absolute`);
  const metadata = lstatSync(path);
  assert.ok(metadata.isFile() && !metadata.isSymbolicLink(), `${label} must be a regular file`);
  assert.equal(metadata.mode & 0o777, 0o600, `${label} mode must be 0600`);
  assert.ok(metadata.size > 0 && metadata.size <= maximumBytes, `${label} size is invalid`);
  return readFileSync(path, "utf8");
}

function exactObject(value, keys, label) {
  if (value === null || typeof value !== "object" || Array.isArray(value))
    throw new Error(`${label} must be an object`);
  const actual = Object.keys(value).sort();
  const expected = [...keys].sort();
  assert.deepEqual(actual, expected, `${label} fields are invalid`);
  return value;
}

function providerIdentifier(value, label) {
  assert.ok(
    typeof value === "string" &&
      value.length > 0 &&
      value.length <= 512 &&
      value.trim() === value &&
      !/[\s\p{Cc}]/u.test(value),
    `${label} is invalid`,
  );
  return value;
}

function localHTTPProxy(value) {
  const invalid = () => {
    throw new Error("local HTTPS proxy configuration is invalid");
  };
  if (typeof value !== "string" || value === "" || value.trim() !== value) return invalid();
  let parsed;
  try {
    parsed = new URL(value);
  } catch {
    return invalid();
  }
  if (
    parsed.protocol !== "http:" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    !["localhost", "127.0.0.1", "[::1]"].includes(parsed.hostname) ||
    parsed.pathname !== "/" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    !/^http:\/\/(?:localhost|127\.0\.0\.1|\[::1\])(?::[1-9][0-9]{0,4})?\/?$/iu.test(value)
  )
    return invalid();
  return parsed.origin;
}

function configuredLocalHTTPSProxy(environment) {
  const upper = environment.HTTPS_PROXY;
  const lower = environment.https_proxy;
  if (upper !== undefined && lower !== undefined && upper !== lower)
    throw new Error("local HTTPS proxy configuration is invalid");
  return localHTTPProxy(upper ?? lower);
}

assert.equal(localHTTPProxy("http://localhost:7890"), "http://localhost:7890");
assert.equal(localHTTPProxy("http://127.0.0.1:7890/"), "http://127.0.0.1:7890");
assert.equal(localHTTPProxy("http://[::1]:7890"), "http://[::1]:7890");
for (const invalid of [
  "https://127.0.0.1:7890",
  "http://user@127.0.0.1:7890",
  "http://127.0.0.1:7890/proxy",
  "http://127.0.0.1:7890/%2e",
  "http://127.0.0.1:7890/?proxy=true",
  "http://example.test:7890",
  "not-a-url",
])
  assert.throws(
    () => localHTTPProxy(invalid),
    /^Error: local HTTPS proxy configuration is invalid$/u,
  );
assert.throws(
  () =>
    configuredLocalHTTPSProxy({
      HTTPS_PROXY: "http://127.0.0.1:7890",
      https_proxy: "http://127.0.0.1:7891",
    }),
  /^Error: local HTTPS proxy configuration is invalid$/u,
);

function sqlTextLiteral(value) {
  return `'${value.replaceAll("'", "''")}'`;
}

assert.equal(sqlTextLiteral("member'$email$@example.test"), "'member''$email$@example.test'");

function readHumanProviderFixture(kind) {
  const isFeishu = kind === "feishu";
  const label = isFeishu ? "Feishu" : "GitHub";
  const environmentName = isFeishu
    ? "CLOUD_AGENTS_IDENTITY_E2E_FEISHU_CONFIG_FILE"
    : "CLOUD_AGENTS_IDENTITY_E2E_GITHUB_CONFIG_FILE";
  const configPath = process.env[environmentName];
  assert.ok(configPath, `${environmentName} is required for ${label}`);
  const config = exactObject(
    JSON.parse(readPrivateFile(configPath, `${label} fixture`, 16 * 1024)),
    isFeishu ? ["admin", "user", "verifiedEmail", "tenantKey"] : ["admin", "user", "verifiedEmail"],
    `${label} fixture`,
  );
  const readClient = (name) => {
    const client = exactObject(config[name], ["appId", "secretFile"], `${label} ${name} client`);
    const appId = providerIdentifier(client.appId, `${label} ${name} App ID`);
    assert.ok(
      typeof client.secretFile === "string" && isAbsolute(client.secretFile),
      `${label} ${name} secret file path is invalid`,
    );
    const secret = readPrivateFile(client.secretFile, `${label} ${name} secret`, 64 * 1024);
    assert.ok(
      secret.length <= 4096 && secret.trim() === secret && !secret.includes("\0"),
      `${label} ${name} secret is invalid`,
    );
    return { appId, secret };
  };
  const verifiedEmail = providerIdentifier(config.verifiedEmail, `${label} verified email`);
  assert.ok(
    verifiedEmail === verifiedEmail.toLowerCase() &&
      /^[^@\s]{1,64}@[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/u.test(verifiedEmail),
    `${label} verified email is invalid`,
  );
  return {
    admin: readClient("admin"),
    user: readClient("user"),
    verifiedEmail,
    tenantKey: isFeishu ? providerIdentifier(config.tenantKey, "Feishu tenant key") : "",
  };
}

function proof(name) {
  const value = randomBytes(32).toString("base64url");
  secretValues.push(value);
  return writePrivate(name, value);
}

function databaseURL(user) {
  return `postgres://${user}@127.0.0.1:${databasePort}/${databaseName}?sslmode=disable`;
}

function spawnService(name, executable, args, env = {}) {
  const logPath = join(directory, `${name}.log`);
  const output = openSync(logPath, "wx", 0o600);
  let child;
  try {
    child = spawn(executable, args, {
      cwd: root,
      env: {
        PATH: process.env.PATH,
        HOME: process.env.HOME,
        ...(humanProvider ? proxyEnvironment : {}),
        NO_PROXY: "127.0.0.1,localhost,.localhost",
        no_proxy: "127.0.0.1,localhost,.localhost",
        ...env,
      },
      stdio: ["ignore", output, output],
    });
  } finally {
    closeSync(output);
  }
  children.push({ child, logPath, name });
  return child;
}

async function waitForHTTPS(url, ca, expectedStatuses = [200], attempts = 100) {
  for (let attempt = 0; attempt < attempts; attempt++) {
    try {
      const status = await new Promise((resolveStatus, reject) => {
        const target = new URL(url);
        const request = httpsRequest(
          {
            ca,
            hostname: target.hostname,
            method: "GET",
            path: `${target.pathname}${target.search}`,
            port: target.port,
            rejectUnauthorized: true,
            servername: target.hostname,
            timeout: 1_000,
          },
          (response) => {
            response.resume();
            response.once("end", () => resolveStatus(response.statusCode));
          },
        );
        request.once("error", reject);
        request.once("timeout", () => request.destroy(new Error("readiness timeout")));
        request.end();
      });
      if (expectedStatuses.includes(status)) return;
    } catch {
      // The owned process is still starting.
    }
    await delay(100);
  }
  throw new Error(`owned HTTPS service did not become ready: ${new URL(url).origin}`);
}

async function chromiumEndpoint() {
  const url = `http://127.0.0.1:${ports.chromium}/json/version`;
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(1_000) });
      if (response.ok) {
        const value = await response.json();
        if (typeof value.webSocketDebuggerUrl === "string") return value.webSocketDebuggerUrl;
      }
    } catch {
      // The owned browser is still starting.
    }
    await delay(100);
  }
  throw new Error("owned Chromium CDP endpoint did not become ready");
}

function pinnedChromiumExecutable() {
  const cache = join(process.env.HOME, "Library/Caches/ms-playwright");
  const installations = readdirSync(cache)
    .filter((entry) => /^chromium_headless_shell-\d+$/.test(entry))
    .sort()
    .toReversed();
  for (const installation of installations) {
    const directory = join(cache, installation);
    const architecture = readdirSync(directory).find((entry) =>
      entry.startsWith("chrome-headless-shell-"),
    );
    if (architecture !== undefined) return join(directory, architecture, "chrome-headless-shell");
  }
  throw new Error("pinned tester-army Chromium is not installed");
}

function visibleChromeExecutable() {
  const executable = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
  assert.ok(existsSync(executable), "Google Chrome is required for interactive provider OAuth");
  return executable;
}

async function stopChildren() {
  for (const { child } of children.toReversed()) {
    if (child.exitCode === null && child.signalCode === null) child.kill("SIGTERM");
  }
  await Promise.all(
    children.map(
      ({ child }) =>
        new Promise((resolveExit) => {
          if (child.exitCode !== null || child.signalCode !== null) return resolveExit();
          const force = setTimeout(() => child.kill("SIGKILL"), 5_000);
          child.once("exit", () => {
            clearTimeout(force);
            resolveExit();
          });
        }),
    ),
  );
}

function stopContainers() {
  for (const name of ownedContainers.toReversed())
    spawnSync("docker", ["rm", "--force", "--volumes", name], {
      cwd: root,
      stdio: "ignore",
    });
  for (const name of ownedVolumes.toReversed())
    spawnSync("docker", ["volume", "rm", "--force", name], { cwd: root, stdio: "ignore" });
}

function startKeycloak(realmPath, certificatePath, privateKeyPath, bootstrapPassword) {
  ownedContainers.push(keycloakContainer);
  const container = run("docker", [
    "run",
    "--detach",
    "--rm",
    "--name",
    keycloakContainer,
    "--user",
    "0",
    "--publish",
    `127.0.0.1:${ports.keycloak}:8443`,
    "--volume",
    `${realmPath}:/opt/keycloak/data/import/cloud-agents-realm.json:ro`,
    "--volume",
    `${certificatePath}:/run/keycloak/tls.crt:ro`,
    "--volume",
    `${privateKeyPath}:/run/keycloak/tls.key:ro`,
    "--env",
    "KC_BOOTSTRAP_ADMIN_USERNAME=fixture-admin",
    "--env",
    `KC_BOOTSTRAP_ADMIN_PASSWORD=${bootstrapPassword}`,
    keycloakImage,
    "start-dev",
    "--import-realm",
    "--http-enabled=false",
    "--https-port=8443",
    "--https-certificate-file=/run/keycloak/tls.crt",
    "--https-certificate-key-file=/run/keycloak/tls.key",
    `--hostname=${origins.keycloak}`,
    "--hostname-strict=true",
  ]);
  assert.ok(container.length >= 12, "Keycloak container did not start");
}

function startGitLab(certificatePath, privateKeyPath, fixtureDirectory) {
  for (const volume of gitlabVolumes) {
    ownedVolumes.push(volume);
    run("docker", ["volume", "create", volume]);
  }
  const omnibus = [
    `external_url '${origins.gitlab}'`,
    "letsencrypt['enable'] = false",
    "nginx['listen_port'] = 443",
    "nginx['listen_https'] = true",
    "nginx['ssl_certificate'] = '/run/cloud-agents/tls.crt'",
    "nginx['ssl_certificate_key'] = '/run/cloud-agents/tls.key'",
    "gitlab_rails['gitlab_email_enabled'] = false",
    "prometheus_monitoring['enable'] = false",
  ].join("\n");
  ownedContainers.push(gitlabContainer);
  const container = run(
    "docker",
    [
      "run",
      "--detach",
      "--name",
      gitlabContainer,
      "--hostname",
      "gitlab.localhost",
      "--cpus",
      "4",
      "--memory",
      "8g",
      "--shm-size",
      "256m",
      "--publish",
      `127.0.0.1:${ports.gitlab}:443`,
      "--volume",
      `${gitlabVolumes[0]}:/etc/gitlab`,
      "--volume",
      `${gitlabVolumes[1]}:/var/log/gitlab`,
      "--volume",
      `${gitlabVolumes[2]}:/var/opt/gitlab`,
      "--volume",
      `${certificatePath}:/run/cloud-agents/tls.crt:ro`,
      "--volume",
      `${privateKeyPath}:/run/cloud-agents/tls.key:ro`,
      "--volume",
      `${fixtureDirectory}:/run/cloud-agents/fixture`,
      "--env",
      `GITLAB_OMNIBUS_CONFIG=${omnibus}`,
      gitlabImage,
    ],
    { timeout: 300_000 },
  );
  assert.ok(container.length >= 12, "GitLab container did not start");
}

function provisionGitLab(fixtureDirectory) {
  run(
    "docker",
    ["exec", gitlabContainer, "gitlab-rails", "runner", "/run/cloud-agents/fixture/bootstrap.rb"],
    { timeout: 600_000 },
  );
  const readFixture = (name) => {
    const path = join(fixtureDirectory, name);
    chmodSync(path, 0o600);
    const value = readFileSync(path, "utf8");
    assert.ok(value.length > 0 && value.length <= 65_536, `GitLab ${name} was invalid`);
    return { path, value };
  };
  return {
    adminClientId: readFixture("admin-client-id").value,
    adminSecret: readFixture("admin-client-secret"),
    userClientId: readFixture("user-client-id").value,
    userSecret: readFixture("user-client-secret"),
  };
}

function currentMigrationHead() {
  const heads = readdirSync(join(root, "services/control-plane/migrations/product"))
    .filter(
      (entry) =>
        /^\d{6}$/.test(entry) &&
        existsSync(join(root, "services/control-plane/migrations/product", entry, "manifest.json")),
    )
    .sort();
  const head = heads.at(-1);
  assert.ok(
    head !== undefined && Number(head) >= 113,
    "identity email-policy migration is required",
  );
  return head;
}

function createFixture(passwordHash, issuer) {
  dockerPSQL(
    `
INSERT INTO cloud_agents_identity.users(id,email,display_name,email_verified_at)
VALUES
  ('tenant-admin','tenant-admin@identity.test','Tenant Administrator',clock_timestamp()),
  ('project-member','project-member@identity.test','Project Member',clock_timestamp()),
  ('account-security','account-security@identity.test','Account Security',clock_timestamp());
INSERT INTO cloud_agents_identity.password_credentials(user_id,password_hash)
VALUES
  ('tenant-admin',$hash$${passwordHash}$hash$),
  ('project-member',$hash$${passwordHash}$hash$),
  ('account-security',$hash$${passwordHash}$hash$);
`,
    databaseName,
  );

  dockerPSQL(
    `
SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
  'tenant-a','tenant-a','Tenant A','tenant-a-org','tenant-a-org','Tenant A Organization',
  'user',$issuer$${issuer}$issuer$,'user-tenant-admin',
  'tenant-a-member','tenant-a-member','tenant-a-binding','tenant-a-binding',
  'tenant-a-audit','tenant-a-member-audit','tenant-a-binding-audit','identity-web-e2e');
SELECT * FROM cloud_agents.bootstrap_tenant_administrator_v1(
  'tenant-b','tenant-b','Tenant B','tenant-b-org','tenant-b-org','Tenant B Organization',
  'user',$issuer$${issuer}$issuer$,'user-unrelated',
  'tenant-b-member','tenant-b-member','tenant-b-binding','tenant-b-binding',
  'tenant-b-audit','tenant-b-member-audit','tenant-b-binding-audit','identity-web-e2e');
`,
    databaseName,
    "identity_bootstrap_test",
  );

  dockerPSQL(
    `
DO $fixture$
DECLARE
  revision bigint;
  project_record record;
  subject_digest text := cloud_agents.subject_ref_digest('user',$issuer$${issuer}$issuer$,'user-project-member');
BEGIN
  FOR project_record IN SELECT * FROM (VALUES
    ('tenant-a','tenant-a-org','project-a1','Project A One'),
    ('tenant-a','tenant-a-org','project-a2','Project A Two'),
    ('tenant-b','tenant-b-org','project-b1','Project B One')
  ) AS projects(tenant_id,organization_id,project_id,display_name)
  LOOP
    UPDATE cloud_agents.tenant_resource_versions
      SET current_revision=current_revision+1,updated_at=clock_timestamp()
      WHERE tenant_id=project_record.tenant_id AND tenant_uid=project_record.tenant_id
      RETURNING current_revision INTO revision;
    INSERT INTO cloud_agents.resource_changes(
      tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at
    ) VALUES (
      project_record.tenant_id,project_record.tenant_id,revision,'project',project_record.project_id,
      'created',session_user,clock_timestamp()
    );
    INSERT INTO cloud_agents.projects(
      tenant_id,tenant_ref_id,project_uid,project_name,organization_uid,display_name,state,
      resource_version,created_at,updated_at
    ) VALUES (
      project_record.tenant_id,project_record.tenant_id,project_record.project_id,project_record.project_id,
      project_record.organization_id,project_record.display_name,'active',revision,clock_timestamp(),clock_timestamp()
    );
  END LOOP;

  UPDATE cloud_agents.tenant_resource_versions
    SET current_revision=current_revision+1,updated_at=clock_timestamp()
    WHERE tenant_id='tenant-a' AND tenant_uid='tenant-a'
    RETURNING current_revision INTO revision;
  INSERT INTO cloud_agents.resource_changes(
    tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at
  ) VALUES ('tenant-a','tenant-a',revision,'membership','project-member-record','created',session_user,clock_timestamp());
  INSERT INTO cloud_agents.memberships(
    tenant_id,tenant_ref_id,membership_uid,membership_name,subject_kind,subject_issuer,subject_value,
    subject_digest,scope_level,scope_project_uid,state,resource_version,created_at,updated_at
  ) VALUES (
    'tenant-a','tenant-a','project-member-record','project-member-record','user',$issuer$${issuer}$issuer$,
    'user-project-member',subject_digest,'project','project-a1','active',revision,clock_timestamp(),clock_timestamp()
  );

  UPDATE cloud_agents.tenant_resource_versions
    SET current_revision=current_revision+1,updated_at=clock_timestamp()
    WHERE tenant_id='tenant-a' AND tenant_uid='tenant-a'
    RETURNING current_revision INTO revision;
  INSERT INTO cloud_agents.resource_changes(
    tenant_id,tenant_uid,resource_version,resource_kind,resource_uid,change_kind,actor_database_principal,occurred_at
  ) VALUES ('tenant-a','tenant-a',revision,'role_binding','project-member-binding','created',session_user,clock_timestamp());
  INSERT INTO cloud_agents.role_bindings(
    tenant_id,tenant_ref_id,role_binding_uid,role_binding_name,subject_kind,subject_issuer,subject_value,
    subject_digest,role_name,role_version,scope_level,scope_project_uid,state,resource_version,created_at,updated_at
  ) VALUES (
    'tenant-a','tenant-a','project-member-binding','project-member-binding','user',$issuer$${issuer}$issuer$,
    'user-project-member',subject_digest,'project.viewer',1,'project','project-a1','active',revision,
    clock_timestamp(),clock_timestamp()
  );
END
$fixture$;
`,
    databaseName,
  );
}

function assertBackendState() {
  const state = dockerPSQL(
    `SELECT
      (SELECT count(*) FROM cloud_agents_identity.issued_tokens),
      (SELECT count(DISTINCT tenant_id) FROM cloud_agents_identity.issued_tokens),
      (SELECT count(DISTINCT project_id) FROM cloud_agents_identity.issued_tokens WHERE project_id IS NOT NULL),
      (SELECT count(*) FROM cloud_agents_identity.tenant_email_policies WHERE tenant_id='tenant-a' AND allowed_domains=ARRAY['example.test','identity.test']::text[]),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='token_issued' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents_identity.invitations WHERE email='invited-member@identity.test' AND state='accepted'),
      (SELECT count(*) FROM cloud_agents_identity.invitations WHERE email='revoked-member@identity.test' AND state='revoked'),
      (SELECT count(*) FROM cloud_agents_identity.users AS account JOIN cloud_agents.memberships AS membership ON membership.subject_value='user-' || account.id WHERE account.email='invited-member@identity.test' AND membership.tenant_id='tenant-a' AND membership.scope_level='project' AND membership.scope_project_uid='project-a1' AND membership.state='active'),
      (SELECT count(*) FROM cloud_agents_identity.users AS account JOIN cloud_agents.role_bindings AS binding ON binding.subject_value='user-' || account.id WHERE account.email='invited-member@identity.test' AND binding.tenant_id='tenant-a' AND binding.role_name='project.developer' AND binding.scope_level='project' AND binding.scope_project_uid='project-a1' AND binding.state='active'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='invitation_created' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='invitation_accepted' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE event_kind='invitation_revoked' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents.audit_facts WHERE tenant_id='tenant-a' AND action='membership.suspend' AND reason_code='admin.member.suspend'),
      (SELECT count(*) FROM cloud_agents.audit_facts WHERE tenant_id='tenant-a' AND action='membership.resume' AND reason_code='admin.member.resume'),
      (SELECT count(*) FROM cloud_agents.audit_facts WHERE tenant_id='tenant-a' AND action='role_binding.bind' AND reason_code='admin.member.role-bind'),
      (SELECT count(*) FROM cloud_agents.audit_facts WHERE tenant_id='tenant-a' AND action='role_binding.revoke' AND reason_code='admin.member.role-revoke'),
      (SELECT count(*) FROM cloud_agents_identity.users AS account JOIN cloud_agents.role_bindings AS binding ON binding.subject_value='user-' || account.id WHERE account.email='invited-member@identity.test' AND binding.tenant_id='tenant-a' AND binding.role_name='project.viewer' AND binding.scope_level='project' AND binding.scope_project_uid='project-a1' AND binding.state='revoked'),
      (SELECT count(*) FROM cloud_agents_identity.users AS account JOIN cloud_agents.memberships AS membership ON membership.subject_value='user-' || account.id WHERE account.email='invited-member@identity.test' AND membership.tenant_id='tenant-b'),
      (SELECT count(*) FROM cloud_agents_identity.users AS account JOIN cloud_agents.role_bindings AS binding ON binding.subject_value='user-' || account.id WHERE account.email='invited-member@identity.test' AND binding.tenant_id='tenant-b'),
      (SELECT count(*) FROM cloud_agents_identity.users WHERE id='account-security' AND disabled_at IS NOT NULL),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id='account-security' AND event_kind='password_changed' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id='account-security' AND event_kind='password_reset_accepted' AND decision='allow'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id='account-security' AND event_kind='account_disabled' AND decision='allow');`,
    databaseName,
  );
  const [
    tokens,
    tenants,
    projects,
    policies,
    audits,
    acceptedInvitations,
    revokedInvitations,
    memberships,
    roleBindings,
    invitationCreatedAudits,
    invitationAcceptedAudits,
    invitationRevokedAudits,
    membershipSuspendAudits,
    membershipResumeAudits,
    roleBindAudits,
    roleRevokeAudits,
    revokedRoleBindings,
    crossTenantMemberships,
    crossTenantRoleBindings,
    disabledAccounts,
    passwordChangedAudits,
    passwordResetAcceptedAudits,
    accountDisabledAudits,
  ] = state.split("|").map(Number);
  assert.ok(tokens >= 5, "tenant/project switching did not issue enough scoped tokens");
  assert.equal(tenants, 2, "platform administrator switching did not reach both tenants");
  assert.ok(projects >= 3, "project switching did not issue distinct project tokens");
  assert.equal(policies, 1, "email policy did not persist in PostgreSQL");
  assert.equal(audits, tokens, "issued token audit count did not match durable token records");
  assert.equal(acceptedInvitations, 1, "accepted invitation state did not persist");
  assert.equal(revokedInvitations, 1, "revoked invitation state did not persist");
  assert.equal(memberships, 1, "accepted invitation did not create its project membership");
  assert.equal(roleBindings, 1, "accepted invitation did not create its project role binding");
  assert.equal(invitationCreatedAudits, 2, "invitation creation audits are incomplete");
  assert.equal(invitationAcceptedAudits, 1, "invitation acceptance audit is incomplete");
  assert.equal(invitationRevokedAudits, 1, "invitation revocation audit is incomplete");
  assert.equal(membershipSuspendAudits, 1, "membership suspension audit is incomplete");
  assert.equal(membershipResumeAudits, 1, "membership resume audit is incomplete");
  assert.equal(roleBindAudits, 1, "role grant audit is incomplete");
  assert.equal(roleRevokeAudits, 1, "role revoke audit is incomplete");
  assert.equal(revokedRoleBindings, 1, "revoked project role did not persist");
  assert.equal(crossTenantMemberships, 0, "member escaped into another tenant");
  assert.equal(crossTenantRoleBindings, 0, "member role escaped into another tenant");
  assert.equal(disabledAccounts, 1, "account disable did not persist");
  assert.ok(passwordChangedAudits >= 1, "password change audit is incomplete");
  assert.ok(passwordResetAcceptedAudits >= 1, "password reset acceptance audit is incomplete");
  assert.ok(accountDisabledAudits >= 1, "account disable audit is incomplete");
  const memberRequestIDs = JSON.parse(readFileSync(memberRequestIDsFile, "utf8"));
  assert.deepEqual(Object.keys(memberRequestIDs).sort(), ["bind", "resume", "revoke", "suspend"]);
  for (const requestID of Object.values(memberRequestIDs))
    assert.match(requestID, /^web-[0-9a-f-]{36}$/u, "member mutation request ID was invalid");
  const expectedRequestIDs = new Map([
    ["membership.resume", memberRequestIDs.resume],
    ["membership.suspend", memberRequestIDs.suspend],
    ["role_binding.bind", memberRequestIDs.bind],
    ["role_binding.revoke", memberRequestIDs.revoke],
  ]);
  const expectedCorrelationIDs = new Set(expectedRequestIDs.values());
  const mutationFacts = dockerPSQL(
    `SELECT action,actor_subject_kind,actor_subject_issuer,actor_subject_value,application,correlation_id
      FROM cloud_agents.audit_facts
      WHERE tenant_id='tenant-a' AND action IN (
        'membership.suspend','membership.resume','role_binding.bind','role_binding.revoke'
      ) ORDER BY action;`,
    databaseName,
  )
    .split("\n")
    .filter(Boolean)
    .map((row) => row.split("|"))
    .filter((row) => expectedCorrelationIDs.has(row[5]));
  assert.equal(mutationFacts.length, 4, "member mutation audit actor rows are incomplete");
  for (const [action, kind, actorIssuer, actor, application, correlationID] of mutationFacts) {
    assert.equal(kind, "user", `${action} audit actor kind was not user`);
    assert.equal(actorIssuer, origins.identity, `${action} audit actor issuer was incorrect`);
    assert.equal(actor, "user-platform-admin", `${action} audit actor was incorrect`);
    assert.equal(application, "admin", `${action} audit application was incorrect`);
    assert.equal(
      correlationID,
      expectedRequestIDs.get(action),
      `${action} audit request ID differed`,
    );
  }
  return {
    tokens,
    tenants,
    projects,
    policies,
    audits,
    acceptedInvitations,
    revokedInvitations,
    memberships,
    roleBindings,
    memberMutations:
      membershipSuspendAudits + membershipResumeAudits + roleBindAudits + roleRevokeAudits,
    accountSecurityAudits:
      passwordChangedAudits + passwordResetAcceptedAudits + accountDisabledAudits,
  };
}

function assertProviderBackendState(provider) {
  const email = sqlTextLiteral(provider.email);
  const emailDomain = sqlTextLiteral(provider.email.slice(provider.email.lastIndexOf("@") + 1));
  const fixedScopePolicy =
    provider.kind === "feishu"
      ? ` AND scopes=ARRAY['contact:user.email:readonly']::text[] AND trust_provider_email AND allowed_organization_ids=ARRAY[${sqlTextLiteral(provider.organizationID)}]::text[]`
      : provider.kind === "github"
        ? ` AND scopes=ARRAY['read:user','user:email']::text[] AND NOT trust_provider_email AND cardinality(allowed_organization_ids)=0`
        : "";
  const state = dockerPSQL(
    `SELECT
      (SELECT count(*) FROM cloud_agents_identity.provider_clients WHERE id=$provider$${provider.id}$provider$ AND enabled),
      (SELECT count(*) FROM cloud_agents_identity.provider_clients WHERE id=$provider$${provider.id}$provider$${fixedScopePolicy}),
      (SELECT count(*) FROM cloud_agents_identity.tenant_email_policies WHERE tenant_id='tenant-a' AND ${emailDomain}=ANY(allowed_domains)),
      (SELECT count(*) FROM cloud_agents_identity.invitations WHERE email=${email} AND verification='provider-required' AND state='accepted'),
      (SELECT count(*) FROM cloud_agents_identity.users WHERE email=${email} AND disabled_at IS NULL),
      (SELECT count(*) FROM cloud_agents_identity.login_methods AS method JOIN cloud_agents_identity.users AS account ON account.id=method.user_id WHERE account.email=${email} AND method.issuer=$issuer$${provider.issuer}$issuer$ AND method.revoked_at IS NOT NULL),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id=(SELECT id FROM cloud_agents_identity.users WHERE email=${email}) AND event_kind='provider_login_succeeded'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id=(SELECT id FROM cloud_agents_identity.users WHERE email=${email}) AND event_kind='provider_reauth_succeeded'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id=(SELECT id FROM cloud_agents_identity.users WHERE email=${email}) AND event_kind='password_reauth_succeeded'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id=(SELECT id FROM cloud_agents_identity.users WHERE email=${email}) AND event_kind='login_method_linked'),
      (SELECT count(*) FROM cloud_agents_identity.audit_events WHERE target_user_id=(SELECT id FROM cloud_agents_identity.users WHERE email=${email}) AND event_kind='login_method_unlinked');`,
    databaseName,
  );
  const [
    providerClients,
    providerClientsWithExpectedScopes,
    providerEmailDomainAllowed,
    acceptedInvitations,
    accounts,
    revokedMethods,
    providerLogins,
    providerReauthentications,
    passwordReauthentications,
    linkedMethods,
    unlinkedMethods,
  ] = state.split("|").map(Number);
  assert.equal(providerClients, 2, "Admin and User provider clients did not persist");
  if (fixedScopePolicy !== "")
    assert.equal(
      providerClientsWithExpectedScopes,
      2,
      "Admin and User provider scope/trust policy was not fixed",
    );
  if (provider.kind === "feishu" || provider.kind === "github")
    assert.equal(providerEmailDomainAllowed, 1, "provider email domain policy did not persist");
  assert.equal(acceptedInvitations, 1, "provider-required invitation was not accepted");
  assert.equal(accounts, 1, "provider invitation did not create one account");
  assert.equal(revokedMethods, 1, "final provider unlink did not persist");
  assert.ok(providerLogins >= 1, "linked provider login audit is incomplete");
  assert.ok(providerReauthentications >= 2, "provider reauthentication audits are incomplete");
  assert.ok(passwordReauthentications >= 2, "password reauthentication audits are incomplete");
  assert.ok(linkedMethods >= 1, "provider link audit is incomplete");
  assert.ok(unlinkedMethods >= 2, "provider unlink audits are incomplete");
  return {
    providerClients,
    ...(fixedScopePolicy === "" ? {} : { providerClientsWithExpectedScopes }),
    ...(provider.kind === "feishu" || provider.kind === "github"
      ? { providerEmailDomainAllowed }
      : {}),
    acceptedInvitations,
    accounts,
    revokedMethods,
    providerLogins,
    providerReauthentications,
    passwordReauthentications,
    linkedMethods,
    unlinkedMethods,
  };
}

function normalizePublicMembershipRequestPaths(log, allowedPaths) {
  return log
    .split("\n")
    .map((line) => {
      try {
        const entry = JSON.parse(line);
        if (
          entry !== null &&
          typeof entry === "object" &&
          !Array.isArray(entry) &&
          entry.msg === "http request" &&
          entry.method === "POST" &&
          entry.status === 200 &&
          typeof entry.path === "string" &&
          allowedPaths.has(entry.path)
        )
          return JSON.stringify({ ...entry, path: "<public-membership-path>" });
      } catch {
        // Non-JSON log lines retain their original bytes for the secret scan.
      }
      return line;
    })
    .join("\n");
}

const membershipPathFixtureID = `membership-${"a".repeat(32)}`;
const membershipPathFixture = `/v1/admin/tenants/tenant-a/memberships/${membershipPathFixtureID}:suspend`;
const membershipPathFixtures = new Set([membershipPathFixture]);
assert.equal(
  normalizePublicMembershipRequestPaths(
    JSON.stringify({
      msg: "http request",
      method: "POST",
      status: 200,
      path: membershipPathFixture,
    }),
    membershipPathFixtures,
  ),
  JSON.stringify({
    msg: "http request",
    method: "POST",
    status: 200,
    path: "<public-membership-path>",
  }),
);
const unknownMembershipPathLog = JSON.stringify({
  msg: "http request",
  method: "POST",
  status: 200,
  path: `/v1/admin/tenants/tenant-b/memberships/${membershipPathFixtureID}:suspend`,
});
assert.equal(
  normalizePublicMembershipRequestPaths(unknownMembershipPathLog, membershipPathFixtures),
  unknownMembershipPathLog,
);
const membershipIDInAnotherField = JSON.stringify({
  msg: "http request",
  method: "POST",
  status: 200,
  path: membershipPathFixture,
  detail: membershipPathFixtureID,
});
assert.ok(
  normalizePublicMembershipRequestPaths(
    membershipIDInAnotherField,
    membershipPathFixtures,
  ).includes(membershipPathFixtureID),
);

function assertLogsContainNoSecrets() {
  const publicMembershipPaths = new Set(
    dockerPSQL(
      `SELECT membership_uid FROM cloud_agents.memberships
        WHERE tenant_id='tenant-a' AND membership_uid ~ '^membership-[a-f0-9]{32}$';`,
      databaseName,
    )
      .split("\n")
      .filter(Boolean)
      .flatMap((membershipID) => [
        `/v1/admin/tenants/tenant-a/memberships/${membershipID}:suspend`,
        `/v1/admin/tenants/tenant-a/memberships/${membershipID}:resume`,
      ]),
  );
  const forbidden = [
    password,
    invitedPassword,
    changedPassword,
    resetPassword,
    providerPassword,
    enabledProviderPassword,
    ...secretValues,
  ];
  for (const { logPath, name } of children) {
    const log = readFileSync(logPath, "utf8");
    for (const value of forbidden)
      assert.equal(log.includes(value), false, `${name} logged a secret`);
    const opaqueScanLog = normalizePublicMembershipRequestPaths(log, publicMembershipPaths);
    assert.ok(
      !/(?<![A-Za-z0-9_-])[A-Za-z0-9_-]{43}(?![A-Za-z0-9_-])/u.test(opaqueScanLog),
      `${name} logged an opaque one-time credential`,
    );
    assert.ok(
      !/eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/u.test(log),
      `${name} logged an access token`,
    );
    assert.ok(!oauthCallbackProofPattern.test(log), `${name} logged an OAuth callback proof`);
    assert.ok(!oauthJSONProofPattern.test(log), `${name} logged an OAuth callback proof`);
    assert.ok(!githubCredentialPattern.test(log), `${name} logged a GitHub credential`);
  }
}

function assertProviderLogsContainNoSecrets(provider) {
  if (provider.container === undefined) return;
  const log = run("docker", ["logs", provider.container], { timeout: 300_000 });
  for (const value of secretValues)
    assert.equal(log.includes(value), false, `${provider.displayName} logged a secret`);
  assert.ok(
    !oauthCallbackProofPattern.test(log),
    `${provider.displayName} logged an OAuth callback proof`,
  );
  assert.ok(
    !oauthJSONProofPattern.test(log),
    `${provider.displayName} logged an OAuth callback proof`,
  );
  assert.ok(
    !githubCredentialPattern.test(log),
    `${provider.displayName} logged a GitHub credential`,
  );
}

async function main() {
  const humanFixture = humanProvider ? readHumanProviderFixture(providerFixture) : undefined;
  const providerEmail = humanFixture?.verifiedEmail ?? "oidc-member@identity.test";
  await startDatabase();
  const binaryDirectory = join(directory, "bin");
  run("mkdir", ["-p", binaryDirectory]);
  const binaries = {
    migrate: join(binaryDirectory, "cloud-agents-product-migrate"),
    identity: join(binaryDirectory, "cloud-agents-identity"),
    controlPlane: join(binaryDirectory, "cloud-agents-control-plane"),
    cli: join(binaryDirectory, "cloud-agentsctl"),
  };
  run("go", [
    "-C",
    "services/control-plane",
    "build",
    "-trimpath",
    "-o",
    binaries.migrate,
    "./cmd/cloud-agents-product-migrate",
  ]);
  run("go", [
    "-C",
    "services/control-plane",
    "build",
    "-trimpath",
    "-o",
    binaries.identity,
    "./cmd/cloud-agents-identity",
  ]);
  run("go", [
    "-C",
    "services/control-plane",
    "build",
    "-trimpath",
    "-o",
    binaries.controlPlane,
    "./cmd/cloud-agents-control-plane",
  ]);
  run("go", [
    "-C",
    "services/control-plane",
    "build",
    "-trimpath",
    "-o",
    binaries.cli,
    "./cmd/cloud-agentsctl",
  ]);
  if (process.env.CLOUD_AGENTS_IDENTITY_E2E_SKIP_WEB_BUILD !== "1") {
    run("bun", ["run", "--cwd", "apps/admin-web", "build"]);
    run("bun", ["run", "--cwd", "apps/user-web", "build"]);
  }

  const certPath = join(directory, "tls.crt");
  const keyPath = join(directory, "tls.key");
  run("mkcert", [
    "-cert-file",
    certPath,
    "-key-file",
    keyPath,
    "admin.localhost",
    "user.localhost",
    "identity.localhost",
    "control-plane.localhost",
    "keycloak.localhost",
    "gitlab.localhost",
    "127.0.0.1",
    "::1",
  ]);
  chmodSync(keyPath, 0o600);
  const caRoot = join(run("mkcert", ["-CAROOT"]), "rootCA.pem");
  const ca = readFileSync(caRoot);
  secretValues.push(providerPassword, enabledProviderPassword);
  let provider;
  if (providerFixture === "keycloak") {
    const adminSecret = randomBytes(32).toString("base64url");
    const userSecret = randomBytes(32).toString("base64url");
    const bootstrapPassword = randomBytes(24).toString("base64url");
    secretValues.push(adminSecret, userSecret, bootstrapPassword);
    const adminSecretFile = writePrivate("keycloak-admin-client.secret", adminSecret);
    const userSecretFile = writePrivate("keycloak-user-client.secret", userSecret);
    const realm = writeJSON("keycloak-realm.json", {
      realm: "cloud-agents",
      enabled: true,
      registrationAllowed: false,
      resetPasswordAllowed: false,
      rememberMe: false,
      verifyEmail: false,
      eventsEnabled: true,
      eventsListeners: ["jboss-logging"],
      loginWithEmailAllowed: true,
      duplicateEmailsAllowed: false,
      clients: [
        {
          clientId: "cloud-agents-admin",
          name: "Cloud Agents Admin",
          enabled: true,
          protocol: "openid-connect",
          publicClient: false,
          secret: adminSecret,
          standardFlowEnabled: true,
          directAccessGrantsEnabled: false,
          redirectUris: [`${origins.admin}/auth/provider/callback`],
          webOrigins: [origins.admin],
          attributes: { "pkce.code.challenge.method": "S256" },
          defaultClientScopes: ["web-origins", "acr", "roles", "profile", "email"],
        },
        {
          clientId: "cloud-agents-user",
          name: "Cloud Agents User",
          enabled: true,
          protocol: "openid-connect",
          publicClient: false,
          secret: userSecret,
          standardFlowEnabled: true,
          directAccessGrantsEnabled: false,
          redirectUris: [`${origins.user}/auth/provider/callback`],
          webOrigins: [origins.user],
          attributes: { "pkce.code.challenge.method": "S256" },
          defaultClientScopes: ["web-origins", "acr", "roles", "profile", "email"],
        },
      ],
      users: [
        {
          id: "oidc-member-subject",
          username: providerEmail,
          email: providerEmail,
          firstName: "OIDC",
          lastName: "Member",
          enabled: true,
          emailVerified: true,
          credentials: [{ type: "password", value: providerPassword, temporary: false }],
        },
      ],
    });
    startKeycloak(realm, certPath, keyPath, bootstrapPassword);
    await waitForHTTPS(
      `${origins.keycloak}/realms/cloud-agents/.well-known/openid-configuration`,
      ca,
      [200],
      600,
    );
    provider = {
      id: "keycloak",
      displayName: "Keycloak",
      kind: "oidc",
      issuer: `${origins.keycloak}/realms/cloud-agents`,
      adminClientId: "cloud-agents-admin",
      userClientId: "cloud-agents-user",
      adminSecretRef: "keycloak-admin-secret",
      userSecretRef: "keycloak-user-secret",
      adminSecretFile,
      userSecretFile,
      rootCARef: "keycloak-root-ca",
      container: keycloakContainer,
      email: providerEmail,
      organizationID: "",
    };
  } else if (providerFixture === "gitlab") {
    const fixtureDirectory = join(directory, "gitlab-fixture");
    run("mkdir", ["-p", fixtureDirectory]);
    chmodSync(fixtureDirectory, 0o700);
    const bootstrap = `
email = ${JSON.stringify(providerEmail)}
password = ${JSON.stringify(providerPassword)}
admin = User.find_by!(username: "root")
created = Users::CreateService.new(admin, {
  username: "oidc-member",
  name: "OIDC Member",
  email: email,
  password: password,
  password_confirmation: password,
  password_automatically_set: false,
  skip_confirmation: true,
  public_email: email,
  organization_id: Organizations::Organization.default_organization.id
}).execute
raise created.message unless created.success?
user = created.payload.fetch(:user)
raise "fixture email is not public" unless user.public_email == email

write_private = lambda do |name, value|
  raise "empty fixture value" if value.nil? || value.empty?
  File.open("/run/cloud-agents/fixture/#{name}", File::WRONLY | File::CREAT | File::TRUNC, 0600) do |file|
    file.write(value)
  end
end

create_application = lambda do |name, redirect_uri|
  application = Doorkeeper::Application.new(
    name: name,
    redirect_uri: redirect_uri,
    scopes: "openid profile email",
    confidential: true,
    organization_id: Organizations::Organization.default_organization.id
  )
  application.trusted = true if application.respond_to?(:trusted=)
  application.save!
  secret = application.respond_to?(:plaintext_secret) ? application.plaintext_secret : nil
  secret = application.secret if secret.nil? || secret.empty?
  [application.uid, secret]
end

admin_id, admin_secret = create_application.call("Cloud Agents Admin", ${JSON.stringify(`${origins.admin}/auth/provider/callback`)})
user_id, user_secret = create_application.call("Cloud Agents User", ${JSON.stringify(`${origins.user}/auth/provider/callback`)})
write_private.call("admin-client-id", admin_id)
write_private.call("admin-client-secret", admin_secret)
write_private.call("user-client-id", user_id)
write_private.call("user-client-secret", user_secret)
`;
    writePrivate("gitlab-fixture/bootstrap.rb", bootstrap);
    startGitLab(certPath, keyPath, fixtureDirectory);
    await waitForHTTPS(`${origins.gitlab}/.well-known/openid-configuration`, ca, [200], 6_000);
    const provisioned = provisionGitLab(fixtureDirectory);
    secretValues.push(provisioned.adminSecret.value, provisioned.userSecret.value);
    provider = {
      id: "gitlab",
      displayName: "GitLab",
      kind: "gitlab",
      issuer: origins.gitlab,
      adminClientId: provisioned.adminClientId,
      userClientId: provisioned.userClientId,
      adminSecretRef: "gitlab-admin-secret",
      userSecretRef: "gitlab-user-secret",
      adminSecretFile: provisioned.adminSecret.path,
      userSecretFile: provisioned.userSecret.path,
      rootCARef: "gitlab-root-ca",
      container: gitlabContainer,
      email: providerEmail,
      organizationID: "",
    };
  } else {
    assert.ok(humanFixture, "interactive provider fixture was not loaded");
    assert.ok(providerChromeProxy, "local HTTPS proxy configuration is invalid");
    secretValues.push(
      humanFixture.admin.secret,
      humanFixture.user.secret,
      humanFixture.verifiedEmail,
      humanFixture.verifiedEmail.slice(humanFixture.verifiedEmail.lastIndexOf("@") + 1),
      providerChromeProxy,
      ...(humanFixture.tenantKey === "" ? [] : [humanFixture.tenantKey]),
    );
    provider = {
      id: providerFixture,
      displayName: providerFixture === "feishu" ? "Feishu" : "GitHub",
      kind: providerFixture,
      issuer: providerFixture === "feishu" ? "https://open.feishu.cn" : "https://github.com",
      adminClientId: humanFixture.admin.appId,
      userClientId: humanFixture.user.appId,
      adminSecretRef: `${providerFixture}-admin-secret`,
      userSecretRef: `${providerFixture}-user-secret`,
      adminSecretFile: writePrivate(
        `${providerFixture}-admin-client.secret`,
        humanFixture.admin.secret,
      ),
      userSecretFile: writePrivate(
        `${providerFixture}-user-client.secret`,
        humanFixture.user.secret,
      ),
      rootCARef: "",
      email: providerEmail,
      organizationID: humanFixture.tenantKey,
    };
  }
  const publicKey = run("openssl", ["x509", "-in", certPath, "-pubkey", "-noout"]);
  const publicKeyDER = execFileSync("openssl", ["pkey", "-pubin", "-outform", "DER"], {
    input: publicKey,
  });
  const certificateSPKI = createHash("sha256").update(publicKeyDER).digest("base64");
  const signingKey = join(directory, "signing.key");
  run("openssl", [
    "genpkey",
    "-algorithm",
    "RSA",
    "-pkeyopt",
    "rsa_keygen_bits:2048",
    "-out",
    signingKey,
  ]);
  chmodSync(signingKey, 0o600);

  const head = currentMigrationHead();
  dockerPSQL(
    `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${databaseName}' AND pid<>pg_backend_pid();
DROP DATABASE IF EXISTS ${databaseName};
CREATE DATABASE ${databaseName};
GRANT CREATE ON DATABASE ${databaseName} TO cloud_agents_migration_owner;`,
  );
  const migration = JSON.parse(
    run(binaries.migrate, [
      "--database-url",
      databaseURL("identity_engine_migration"),
      "--repository-root",
      root,
      "--manifest",
      `services/control-plane/migrations/product/${head}/manifest.json`,
      "--selector",
      `product-${head}`,
    ]),
  );
  assert.equal(migration.schema_head, head);
  assert.equal(migration.applied, Number(head));

  const passwordFile = writePrivate("password", password);
  secretValues.push(password);
  const passwordHash = join(directory, "password.hash");
  run(binaries.identity, [
    "hash-password",
    "--password-file",
    passwordFile,
    "--output-file",
    passwordHash,
  ]);
  const adminWebCredential = proof("admin-web.proof");
  const userWebCredential = proof("user-web.proof");
  const controlPlaneCredential = proof("control-plane.proof");
  const authorizationCredential = proof("identity-control-plane.proof");
  const setupProof = proof("setup.proof");
  const csrfKey = writePrivate("csrf.key", randomBytes(32));
  const providerFlowKey = writePrivate("provider-flow.key", randomBytes(32));
  const accessGrantKey = writePrivate("access-grant.key", randomBytes(32));
  const bootstrapDatabase = writePrivate(
    "bootstrap-database-url",
    databaseURL("identity_bootstrap_test"),
  );
  const identityDatabase = writePrivate(
    "identity-database-url",
    databaseURL("identity_service_test"),
  );
  const issuer = origins.identity;
  const adminAudience = origins.admin;
  const userAudience = origins.user;
  const keyNotBefore = new Date(Date.now() - 60_000).toISOString().replace(/\.\d{3}Z$/, "Z");
  const keyNotAfter = new Date(Date.now() + 365 * 24 * 60 * 60 * 1000)
    .toISOString()
    .replace(/\.\d{3}Z$/, "Z");
  const initializeConfig = writeJSON("identity-initialize.json", {
    bootstrapDatabaseUrlFile: bootstrapDatabase,
    issuer,
    adminAudience,
    userAudience,
    signingKeyId: "identity-web-key-1",
    signingPrivateKeyFile: signingKey,
    setupProofFile: setupProof,
    userId: "platform-admin",
    email: "platform-admin@identity.test",
    displayName: "Platform Administrator",
    passwordHashFile: passwordHash,
    keyNotBefore,
    keyNotAfter,
  });
  const initialized = JSON.parse(
    run(binaries.identity, ["initialize", "--config", initializeConfig]),
  );
  assert.deepEqual(initialized, { realmCreated: true, signingAuthorityCreated: true });
  createFixture(readFileSync(passwordHash, "utf8"), issuer);

  const controlPlaneAuth = writeJSON("control-plane-identity.json", {
    issuer,
    userAudience,
    adminAudience,
    identityBaseUrl: origins.identity,
    caFile: caRoot,
    controlPlaneCredentialFile: controlPlaneCredential,
    identityCredentialFile: authorizationCredential,
  });
  const identityRunConfig = writeJSON("identity-run.json", {
    listen: `127.0.0.1:${ports.identity}`,
    databaseUrlFile: identityDatabase,
    tlsCertificateFile: certPath,
    tlsPrivateKeyFile: keyPath,
    issuer,
    adminAudience,
    userAudience,
    signingKeyId: "identity-web-key-1",
    signingPrivateKeyFile: signingKey,
    csrfKeyFile: csrfKey,
    adminWebCredentialFile: adminWebCredential,
    userWebCredentialFile: userWebCredential,
    controlPlaneCredentialFile: controlPlaneCredential,
    controlPlaneUrl: origins.controlPlane,
    controlPlaneRootCaFile: caRoot,
    controlPlaneAuthorizationCredentialFile: authorizationCredential,
    providerFlowKeyFile: providerFlowKey,
    providerSecretFiles: {
      [provider.adminSecretRef]: provider.adminSecretFile,
      [provider.userSecretRef]: provider.userSecretFile,
    },
    providerRootCaFiles: provider.rootCARef === "" ? {} : { [provider.rootCARef]: caRoot },
  });

  spawnService("identity", binaries.identity, ["run", "--config", identityRunConfig]);
  await waitForHTTPS(`${origins.identity}/.well-known/jwks.json`, ca);
  spawnService("control-plane", binaries.controlPlane, [
    "--listen",
    `127.0.0.1:${ports.controlPlane}`,
    "--database-url",
    databaseURL("identity_runtime_test"),
    "--auth-config",
    controlPlaneAuth,
    "--tls-cert",
    certPath,
    "--tls-key",
    keyPath,
    "--access-grant-key-file",
    accessGrantKey,
  ]);
  await waitForHTTPS(`${origins.controlPlane}/readyz`, ca);

  const webEnvironment = (scope, port, peerOrigin, credentialFile, webRoot) => ({
    CLOUD_AGENTS_WEB_SCOPE: scope,
    CLOUD_AGENTS_WEB_PORT: String(port),
    CLOUD_AGENTS_WEB_ROOT: webRoot,
    CLOUD_AGENTS_WEB_ORIGIN: origins[scope],
    CLOUD_AGENTS_WEB_PEER_ORIGIN: peerOrigin,
    CLOUD_AGENTS_WEB_TLS_CERT_FILE: certPath,
    CLOUD_AGENTS_WEB_TLS_KEY_FILE: keyPath,
    CLOUD_AGENTS_WEB_IDENTITY_URL: origins.identity,
    CLOUD_AGENTS_WEB_IDENTITY_CA_FILE: caRoot,
    CLOUD_AGENTS_WEB_IDENTITY_CREDENTIAL_FILE: credentialFile,
    CLOUD_AGENTS_WEB_CONTROL_PLANE_URL: origins.controlPlane,
    CLOUD_AGENTS_WEB_CONTROL_PLANE_CA_FILE: caRoot,
  });
  spawnService(
    "admin-web",
    "node",
    [join(root, "deploy/web/server.mjs")],
    webEnvironment(
      "admin",
      ports.admin,
      origins.user,
      adminWebCredential,
      join(root, "apps/admin-web/dist"),
    ),
  );
  spawnService(
    "user-web",
    "node",
    [join(root, "deploy/web/server.mjs")],
    webEnvironment(
      "user",
      ports.user,
      origins.admin,
      userWebCredential,
      join(root, "apps/user-web/dist"),
    ),
  );
  await Promise.all([waitForHTTPS(origins.admin, ca), waitForHTTPS(origins.user, ca)]);
  const cliProfile = join(directory, "cli-profile.json");
  const cliVerificationURL = join(directory, "cli-verification-url");
  const cliOpenerDirectory = join(directory, "cli-opener");
  run("mkdir", ["-p", cliOpenerDirectory]);
  chmodSync(cliOpenerDirectory, 0o700);
  const cliOpener = join(cliOpenerDirectory, "open");
  writeFileSync(
    cliOpener,
    `#!/bin/sh
set -eu
[ "$#" -eq 1 ]
case "$1" in
  ${origins.admin}/auth/cli/authorize\\?*) ;;
  *) exit 1 ;;
esac
umask 077
temporary="\${CLOUD_AGENTS_IDENTITY_E2E_CLI_URL_FILE}.tmp.$$"
printf '%s' "$1" > "$temporary"
chmod 600 "$temporary"
mv "$temporary" "$CLOUD_AGENTS_IDENTITY_E2E_CLI_URL_FILE"
`,
    { mode: 0o700 },
  );
  chmodSync(cliOpener, 0o700);
  spawnService(
    "cloud-agentsctl-login",
    binaries.cli,
    [
      "login",
      "--web-endpoint",
      origins.admin,
      "--control-plane-endpoint",
      origins.controlPlane,
      "--application",
      "admin",
      "--ca-file",
      caRoot,
      "--profile",
      cliProfile,
    ],
    {
      PATH: `${cliOpenerDirectory}:${process.env.PATH}`,
      CLOUD_AGENTS_IDENTITY_E2E_CLI_URL_FILE: cliVerificationURL,
    },
  );
  if (humanProvider) run("mkdir", ["-p", join(root, "test/e2e/.tmp")]);
  spawnService("chromium", humanProvider ? visibleChromeExecutable() : pinnedChromiumExecutable(), [
    ...(humanProvider
      ? ["--no-default-browser-check", "--lang=en-US", "--accept-lang=en-US"]
      : ["--headless"]),
    ...(humanProvider
      ? [
          `--proxy-server=${providerChromeProxy}`,
          "--proxy-bypass-list=localhost;127.0.0.1;[::1];*.localhost",
        ]
      : []),
    "--disable-gpu",
    "--no-first-run",
    `--remote-debugging-address=127.0.0.1`,
    `--remote-debugging-port=${ports.chromium}`,
    `--user-data-dir=${join(directory, "chromium")}`,
    `--ignore-certificate-errors-spki-list=${certificateSPKI}`,
  ]);
  const cdpEndpoint = await chromiumEndpoint();

  rmSync(evidenceDirectory, { recursive: true, force: true });
  run(
    join(root, "node_modules/.bin/e2e"),
    ["run", "--config", "test/e2e/identity-web.e2e.config.ts", "--reporter", "list"],
    {
      env: {
        PATH: process.env.PATH,
        HOME: process.env.HOME,
        NODE_EXTRA_CA_CERTS: caRoot,
        NO_PROXY: "127.0.0.1,localhost,.localhost",
        no_proxy: "127.0.0.1,localhost,.localhost",
        E2E_TELEMETRY_DISABLED: "1",
        CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL: origins.admin,
        CLOUD_AGENTS_IDENTITY_E2E_USER_URL: origins.user,
        CLOUD_AGENTS_IDENTITY_E2E_CONTROL_PLANE_URL: origins.controlPlane,
        CLOUD_AGENTS_IDENTITY_E2E_PASSWORD: password,
        CLOUD_AGENTS_IDENTITY_E2E_INVITED_PASSWORD: invitedPassword,
        CLOUD_AGENTS_IDENTITY_E2E_MEMBER_REQUEST_IDS_FILE: memberRequestIDsFile,
        CLOUD_AGENTS_IDENTITY_E2E_CLI_BINARY: binaries.cli,
        CLOUD_AGENTS_IDENTITY_E2E_CLI_PROFILE: cliProfile,
        CLOUD_AGENTS_IDENTITY_E2E_CLI_VERIFICATION_URL_FILE: cliVerificationURL,
        CLOUD_AGENTS_IDENTITY_E2E_CDP_ENDPOINT: cdpEndpoint,
      },
    },
  );
  const backend = assertBackendState();
  rmSync(providerEvidenceDirectory, { recursive: true, force: true });
  ownsProviderEvidence = true;
  run(
    join(root, "node_modules/.bin/e2e"),
    ["run", "--config", "test/e2e/identity-provider.e2e.config.ts", "--reporter", "list"],
    {
      timeout: humanProvider ? 1_500_000 : 180_000,
      privateFailure: humanProvider,
      env: {
        PATH: process.env.PATH,
        HOME: process.env.HOME,
        NODE_EXTRA_CA_CERTS: caRoot,
        NO_PROXY: "127.0.0.1,localhost,.localhost",
        no_proxy: "127.0.0.1,localhost,.localhost",
        E2E_TELEMETRY_DISABLED: "1",
        CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL: origins.admin,
        CLOUD_AGENTS_IDENTITY_E2E_USER_URL: origins.user,
        CLOUD_AGENTS_IDENTITY_E2E_PASSWORD: password,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_PASSWORD: providerPassword,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ENABLED_PASSWORD: enabledProviderPassword,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_EMAIL: provider.email,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ORGANIZATION_ID: provider.organizationID,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_HUMAN_MARKER_FILE: providerHumanMarker,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ISSUER: provider.issuer,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ID: provider.id,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_DISPLAY_NAME: provider.displayName,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_KIND: provider.kind,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ADMIN_CLIENT_ID: provider.adminClientId,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_USER_CLIENT_ID: provider.userClientId,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ADMIN_SECRET_REF: provider.adminSecretRef,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_USER_SECRET_REF: provider.userSecretRef,
        CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ROOT_CA_REF: provider.rootCARef,
        CLOUD_AGENTS_IDENTITY_E2E_CDP_ENDPOINT: cdpEndpoint,
      },
    },
  );
  const providerBackend = assertProviderBackendState(provider);
  assertLogsContainNoSecrets();
  assertProviderLogsContainNoSecrets(provider);
  const report = readFileSync(join(evidenceDirectory, "report.json"), "utf8");
  const providerReport = readFileSync(join(providerEvidenceDirectory, "report.json"), "utf8");
  for (const value of new Set([invitedPassword, ...secretValues]))
    assert.equal(report.includes(value), false, "tester report retained a sensitive value");
  assert.ok(
    !/#invitation=[A-Za-z0-9_-]{43}/u.test(report),
    "tester report retained an invitation proof",
  );
  assert.ok(
    !/#password-reset=[A-Za-z0-9_-]{43}/u.test(report),
    "tester report retained a password-reset proof",
  );
  assert.ok(!oauthCallbackProofPattern.test(report), "tester report retained a callback proof");
  assert.ok(!oauthJSONProofPattern.test(report), "tester report retained a callback proof");
  assert.ok(!githubCredentialPattern.test(report), "tester report retained a GitHub credential");
  assert.ok(
    !/"[A-Za-z0-9_-]{43}"/u.test(report),
    "tester report retained an opaque one-time credential",
  );
  assert.ok(
    !/eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/u.test(report),
    "tester report retained a tenant access token",
  );
  for (const value of new Set(secretValues))
    assert.equal(
      providerReport.includes(value),
      false,
      "provider tester report retained a sensitive value",
    );
  assert.ok(
    !/#invitation=[A-Za-z0-9_-]{43}/u.test(providerReport),
    "provider tester report retained an invitation proof",
  );
  assert.ok(
    !oauthCallbackProofPattern.test(providerReport),
    "provider tester report retained an OAuth callback proof",
  );
  assert.ok(
    !oauthJSONProofPattern.test(providerReport),
    "provider tester report retained an OAuth callback proof",
  );
  assert.ok(
    !githubCredentialPattern.test(providerReport),
    "provider tester report retained a GitHub credential",
  );
  if (humanProvider) keepHumanProviderEvidence = true;
  process.stdout.write(
    JSON.stringify({
      status: "PASS",
      runner: "tester-army/e2e@0.18.0",
      database: databaseName,
      migrationHead: head,
      flows: 8,
      backend,
      providerFlows: 1,
      provider: provider.id,
      fixtureRunId: runID,
      providerBackend,
    }) + "\n",
  );
}

try {
  await main();
} finally {
  await stopChildren();
  stopContainers();
  rmSync(providerHumanMarker, { force: true });
  rmSync(`${providerHumanMarker}.tmp`, { force: true });
  if (humanProvider && ownsProviderEvidence && !keepHumanProviderEvidence)
    rmSync(providerEvidenceDirectory, { recursive: true, force: true });
  rmSync(directory, { recursive: true, force: true });
}
