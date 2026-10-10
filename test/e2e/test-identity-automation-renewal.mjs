import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { createServer } from "node:https";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const root = resolve(import.meta.dirname, "../..");
const directory = mkdtempSync(join(tmpdir(), "cloud-agents-automation-renewal-"));
const certificatePath = join(directory, "tls.crt");
const privateKeyPath = join(directory, "tls.key");
const adminCredential = "A".repeat(43);
const userCredential = "U".repeat(43);
const deniedCredential = "D".repeat(43);
const requests = [];
const errors = [];

function shellQuote(value) {
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

function privateFile(name, value) {
  const path = join(directory, name);
  writeFileSync(path, value, { mode: 0o600 });
  chmodSync(path, 0o600);
  return path;
}

function responseJSON(response, status, value) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(value));
}

function startPurposeServer(application, acceptedCredentials) {
  const server = createServer(
    {
      cert: readFileSync(certificatePath),
      key: readFileSync(privateKeyPath),
      minVersion: "TLSv1.2",
    },
    (request, response) => {
      const parts = [];
      request.on("data", (part) => parts.push(part));
      request.on("end", () => {
        try {
          const credential = request.headers.authorization?.replace(/^Bearer /u, "") ?? "";
          const label = acceptedCredentials.get(credential);
          if (
            request.method !== "POST" ||
            request.url !== "/v1/auth/automation/tenant-token" ||
            label === undefined
          ) {
            responseJSON(response, 403, {});
            return;
          }
          const body = JSON.parse(Buffer.concat(parts).toString("utf8"));
          assert.deepEqual(body, {
            tenantId: "tenant-compose-smoke",
            projectId: "project-renewal",
          });
          requests.push({ application, label });
          responseJSON(response, 200, {
            accessToken: `${label}.${requests.length.toString(36)}.signature`,
          });
        } catch (error) {
          errors.push(error);
          responseJSON(response, 500, {});
        }
      });
    },
  );
  return new Promise((resolveServer, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolveServer(server));
  });
}

function serverPort(server) {
  const address = server.address();
  assert.ok(address && typeof address === "object");
  return address.port;
}

function exactRenewalFunctions() {
  const source = readFileSync(join(root, "test/e2e/test-platform-compose.sh"), "utf8");
  const startMarker = "# identity-automation-renewal-functions:start\n";
  const endMarker = "# identity-automation-renewal-functions:end\n";
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  assert.notEqual(start, -1, "renewal function start marker is missing");
  assert.notEqual(end, -1, "renewal function end marker is missing");
  const functions = source.slice(start + startMarker.length, end);
  assert.equal(functions.includes("while automation_refresh_wait 240"), true);
  assert.equal(functions.includes("automation_refresh_wait 15"), true);
  return functions
    .replace("while automation_refresh_wait 240", "while automation_refresh_wait 0.05")
    .replace("automation_refresh_wait 15", "automation_refresh_wait 0.02");
}

function assertPrivateCompleteOutputs() {
  const tokenFiles = [
    "token",
    "admin-token",
    "remote-worker-bootstrap-token",
    "user-token",
    "admin-denied-token",
  ];
  const curlFiles = ["admin-curl.conf", "user-curl.conf", "admin-denied-curl.conf"];
  for (const name of tokenFiles) {
    const path = join(directory, name);
    assert.equal(statSync(path).mode & 0o077, 0, `${name} permissions changed`);
    assert.match(readFileSync(path, "utf8"), /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\n$/u);
  }
  for (const name of curlFiles) {
    const path = join(directory, name);
    assert.equal(statSync(path).mode & 0o077, 0, `${name} permissions changed`);
    assert.match(
      readFileSync(path, "utf8"),
      /^header = "Authorization: Bearer [A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+"\n$/u,
    );
  }
}

function waitForExit(child) {
  return new Promise((resolveExit, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolveExit({ code, signal }));
  });
}

execFileSync(
  "openssl",
  [
    "req",
    "-x509",
    "-newkey",
    "rsa:2048",
    "-nodes",
    "-days",
    "1",
    "-subj",
    "/CN=admin.localhost",
    "-addext",
    "subjectAltName=DNS:admin.localhost,DNS:user.localhost,IP:127.0.0.1",
    "-keyout",
    privateKeyPath,
    "-out",
    certificatePath,
  ],
  { stdio: "ignore" },
);
privateFile("ca.crt", readFileSync(certificatePath));

let adminServer;
let userServer;
let shell;
let watcher;
let shellStderr = "";
try {
  adminServer = await startPurposeServer(
    "admin",
    new Map([
      [adminCredential, "admin"],
      [deniedCredential, "denied"],
    ]),
  );
  userServer = await startPurposeServer("user", new Map([[userCredential, "user"]]));
  privateFile("automation-admin.credential", `${adminCredential}\n`);
  privateFile("automation-user.credential", `${userCredential}\n`);
  privateFile("automation-denied.credential", `${deniedCredential}\n`);
  for (const name of [
    "token",
    "admin-token",
    "remote-worker-bootstrap-token",
    "user-token",
    "admin-denied-token",
  ])
    privateFile(name, "old.header.signature\n");
  for (const name of ["admin-curl.conf", "user-curl.conf", "admin-denied-curl.conf"])
    privateFile(name, 'header = "Authorization: Bearer old.header.signature"\n');

  const cleanupMarker = join(directory, "cleanup-complete");
  const pidFile = join(directory, "refresh.pid");
  const shellSource = `set -eu
smoke_directory=${shellQuote(directory)}
script_directory=${shellQuote(join(root, "test/e2e"))}
admin_web_origin=${shellQuote(`https://127.0.0.1:${serverPort(adminServer)}`)}
user_web_origin=${shellQuote(`https://127.0.0.1:${serverPort(userServer)}`)}
project_id=project-renewal
${exactRenewalFunctions()}
refresh_automation_tokens
automation_token_refresh_loop &
automation_refresh_pid=$!
printf '%s\n' "$automation_refresh_pid" >${shellQuote(pidFile)}
cleanup() {
  kill "$automation_refresh_pid" >/dev/null 2>&1 || true
  wait "$automation_refresh_pid" 2>/dev/null || true
  : >${shellQuote(cleanupMarker)}
}
trap 'cleanup; exit 0' HUP INT TERM
wait "$automation_refresh_pid"
`;
  const shellPath = privateFile("renewal.sh", shellSource);
  shell = spawn("sh", [shellPath], {
    cwd: root,
    env: { PATH: process.env.PATH, HOME: process.env.HOME },
    stdio: ["ignore", "pipe", "pipe"],
  });
  shell.stderr.on("data", (chunk) => {
    shellStderr = `${shellStderr}${chunk}`.slice(-4_096);
  });
  const shellExit = waitForExit(shell);
  let atomicFailure;
  watcher = setInterval(() => {
    try {
      assertPrivateCompleteOutputs();
    } catch (error) {
      atomicFailure ??= error;
    }
  }, 1);

  for (let attempt = 0; requests.length < 9 && attempt < 200; attempt++) await delay(25);
  assert.equal(
    requests.length >= 9,
    true,
    `renewal loop did not complete two shortened cycles: ${shellStderr.trim()}`,
  );
  assert.deepEqual(errors, []);
  assert.equal(
    requests.filter(({ application, label }) => application === "admin" && label === "admin")
      .length >= 3,
    true,
  );
  assert.equal(
    requests.filter(({ application, label }) => application === "admin" && label === "denied")
      .length >= 3,
    true,
  );
  assert.equal(
    requests.filter(({ application, label }) => application === "user" && label === "user")
      .length >= 3,
    true,
  );
  assertPrivateCompleteOutputs();
  assert.equal(atomicFailure, undefined, "an atomically replaced output was missing or partial");
  assert.match(readFileSync(join(directory, "admin-token"), "utf8"), /^admin\./u);
  assert.match(readFileSync(join(directory, "user-token"), "utf8"), /^user\./u);
  assert.match(readFileSync(join(directory, "admin-denied-token"), "utf8"), /^denied\./u);

  const refreshPID = Number(readFileSync(pidFile, "utf8"));
  assert.equal(Number.isSafeInteger(refreshPID) && refreshPID > 1, true);
  shell.kill("SIGTERM");
  const exited = await shellExit;
  assert.deepEqual(exited, { code: 0, signal: null });
  assert.equal(readFileSync(cleanupMarker, "utf8"), "");
  assert.throws(() => process.kill(refreshPID, 0), { code: "ESRCH" });
  console.log("Identity automation TLS purpose routing, atomic renewal, and cleanup: PASS");
} finally {
  clearInterval(watcher);
  if (shell?.exitCode === null && shell?.signalCode === null) shell.kill("SIGKILL");
  await Promise.all(
    [adminServer, userServer]
      .filter(Boolean)
      .map((server) => new Promise((resolveClose) => server.close(resolveClose))),
  );
  rmSync(directory, { recursive: true, force: true });
}
