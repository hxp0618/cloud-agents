import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { randomUUID, createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

// Candidate qualification only; not a Control Plane authority or production adapter.
const [output] = process.argv.slice(2);
assert.ok(output, "usage: node scripts/test-base-opensandbox-docker.mjs NEW_OUTPUT_DIRECTORY");
const directory = resolve(output);
mkdirSync(directory, { mode: 0o700 });
const serverImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/server@sha256:8f8762af7565ed9c6f9dbcf009dd56727aa1fef8ce58a17f2b007b88cfe542bb";
const execdImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd";
const sandboxImage = "node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5";
const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 120_000,
  }).trim();
assert.equal(
  docker("ps", "-aq", "--filter", "label=opensandbox.io/id"),
  "",
  "Refusing to start candidate recovery alongside existing OpenSandbox objects",
);
for (const image of [serverImage, execdImage, sandboxImage]) docker("image", "inspect", image);
const run = `base-m0-${randomUUID()}`,
  volume = `${run}-workspace`,
  server = `${run}-server`;
const apiKey = randomUUID(),
  base = "http://127.0.0.1:18890";
const checks = [];
let volumeCreated = false,
  serverCreated = false;
writeFileSync(
  `${directory}/config.toml`,
  `[server]\nhost="0.0.0.0"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${execdImage}"\n[docker]\nnetwork_mode="bridge"\nhost_ip="127.0.0.1"\nport_range_min=49100\nport_range_max=49300\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
  { mode: 0o600 },
);
const request = async (path, method = "GET", body, expected = 200) => {
  const response = await fetch(base + path, {
    method,
    headers: { "OPEN-SANDBOX-API-KEY": apiKey, "Content-Type": "application/json" },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(120_000),
  });
  const text = await response.text();
  assert.equal(response.status, expected, `${method} ${path}: ${text.slice(0, 600)}`);
  return text ? JSON.parse(text) : undefined;
};
const spec = (operation) => ({
  image: { uri: sandboxImage },
  entrypoint: ["sleep", "infinity"],
  timeout: 600,
  resourceLimits: { cpu: "500m", memory: "512Mi" },
  metadata: {
    "cloud-agents-poc": run,
    operation,
    "cloud-agents-receipt-version": "2",
    ...Object.fromEntries(
      Object.entries({
        tenant: "A" + "~".repeat(126) + "Z",
        project: "project-poc",
        workspace: volume,
        sandbox: run,
        operation,
      }).flatMap(([name, value]) => {
        const digest = createHash("sha256").update(value).digest("hex");
        return [
          [`cloud-agents-${name}-sha256-a`, digest.slice(0, 32)],
          [`cloud-agents-${name}-sha256-b`, digest.slice(32)],
        ];
      }),
    ),
    "cloud-agents-generation": "1",
    "cloud-agents-spec-sha256-a": "a".repeat(32),
    "cloud-agents-spec-sha256-b": "a".repeat(32),
  },
  volumes: [
    {
      name: "workspace",
      pvc: { claimName: volume, createIfNotExists: false, deleteOnSandboxTermination: false },
      mountPath: "/workspace",
    },
  ],
});
const goReceipt = (operation, runtimeID, state, duplicate = false, remove = false) => {
  const result = execFileSync(
    "go",
    [
      "test",
      "./services/control-plane/internal/opensandbox",
      "-run",
      "^TestLiveDiscovery$",
      "-count=1",
      "-v",
    ],
    {
      encoding: "utf8",
      timeout: 120000,
      env: {
        ...process.env,
        CA_BASE_ENDPOINT: base,
        CA_BASE_KEY: apiKey,
        CA_BASE_RUN: run,
        CA_BASE_VOLUME: volume,
        CA_BASE_OPERATION: operation,
        CA_BASE_RUNTIME_ID: runtimeID,
        CA_BASE_STATE: state,
        CA_BASE_DUPLICATE: duplicate ? "yes" : "no",
        CA_BASE_DELETE: remove ? "yes" : "no",
      },
    },
  );
  assert.ok(result.includes("--- PASS: TestLiveDiscovery"));
  checks.push({
    name: "Go receipt discovery and ownership guard",
    operation,
    duplicateRejected: duplicate,
    cleanupReplayed: remove,
    state,
  });
};
const create = async (operation) => {
  const result = await request("/v1/sandboxes", "POST", spec(operation), 202);
  for (let i = 0; i < 100; i++) {
    const state = await request(`/v1/sandboxes/${result.id}`);
    if (state.status.state === "Running") return result.id;
    await delay(100);
  }
  throw new Error("Sandbox did not become Running");
};
const command = async (id, command) => {
  const endpoint = await request(`/v1/sandboxes/${id}/endpoints/44772`);
  const address = new URL(`http://${endpoint.endpoint}`);
  assert.equal(address.hostname, "127.0.0.1");
  const response = await fetch(new URL("/command", address), {
    method: "POST",
    headers: { "Content-Type": "application/json", ...endpoint.headers },
    body: JSON.stringify({ command, cwd: "/workspace", timeout: 10000 }),
    signal: AbortSignal.timeout(15000),
  });
  const body = await response.text();
  assert.equal(response.status, 200, body.slice(0, 600));
  return body;
};
try {
  docker("volume", "create", "--label", `cloud-agents-poc=${run}`, volume);
  volumeCreated = true;
  docker(
    "run",
    "-d",
    "--name",
    server,
    "--label",
    `cloud-agents-poc=${run}`,
    "-p",
    "127.0.0.1:18890:8080",
    "--mount",
    `type=bind,src=${directory}/config.toml,dst=/etc/opensandbox/config.toml,readonly`,
    "--mount",
    "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
    serverImage,
  );
  serverCreated = true;
  let healthy = false;
  for (let i = 0; i < 300; i++) {
    try {
      if ((await fetch(base + "/health", { signal: AbortSignal.timeout(1000) })).ok) {
        healthy = true;
        break;
      }
    } catch {}
    await delay(100);
  }
  assert.ok(healthy, "Candidate server health timeout; inspect server.log");
  const denied = await fetch(base + "/v1/sandboxes");
  assert.ok([401, 403].includes(denied.status));
  checks.push({ name: "missing API key denied", status: denied.status });
  const bad = spec("missing-volume");
  bad.volumes[0].pvc.claimName = `${volume}-missing`;
  const failure = await request("/v1/sandboxes", "POST", bad, 400);
  assert.ok(JSON.stringify(failure).includes("VOLUME::PVC_NOT_FOUND"), JSON.stringify(failure));
  assert.equal(docker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
  docker("volume", "inspect", volume);
  checks.push({
    name: "missing volume rejected without compute allocation or deleting owned volume",
  });
  const first = await create("create-1");
  goReceipt("create-1", first, "Running");
  const duplicate = await create("create-1");
  assert.notEqual(duplicate, first);
  goReceipt("create-1", first, "Running", true);
  await request(`/v1/sandboxes/${duplicate}`, "DELETE", undefined, 204);
  checks.push({
    name: "native replay creates duplicate: CP idempotency and single-writer authority required",
    first,
    duplicate,
  });
  const payload = "cloud-agents-no-agent-workspace";
  const digest = createHash("sha256").update(payload).digest("hex");
  const written = await command(
    first,
    `test -z "$(command -v codex)" && test -z "$(command -v claude)" && printf %s ${payload} > /workspace/proof.txt && sha256sum /workspace/proof.txt`,
  );
  assert.ok(written.includes(digest), written);
  checks.push({ name: "no-Agent create ready exec write", digest });
  // Kill compute through the candidate, then verify the separately owned volume survives.
  goReceipt("create-1", first, "Running", false, true);
  docker("volume", "inspect", volume);
  await request(`/v1/sandboxes/${first}`, "GET", undefined, 404);
  const second = await create("create-2");
  const restored = await command(second, "sha256sum /workspace/proof.txt");
  assert.ok(restored.includes(digest), restored);
  const endpoint = await request(`/v1/sandboxes/${second}/endpoints/44772`);
  const address = new URL(`http://${endpoint.endpoint}`);
  assert.equal(address.hostname, "127.0.0.1");
  const download = await fetch(new URL("/files/download?path=/workspace/proof.txt", address), {
    headers: endpoint.headers,
    signal: AbortSignal.timeout(10000),
  });
  assert.equal(download.status, 200);
  assert.equal(await download.text(), payload);
  checks.push({ name: "delete compute and rebuild retains workspace", digest });
  checks.push({ name: "Files API reads restored bytes", digest });
  const found = await request("/v1/sandboxes");
  assert.ok(JSON.stringify(found).includes(second));
  checks.push({ name: "candidate resource discovery", id: second });
  goReceipt("create-2", second, "Running", false, true);
  docker("volume", "inspect", volume);
  const broken = spec("start-failure");
  broken.entrypoint = ["/definitely-not-a-program"];
  const acceptedBroken = await request("/v1/sandboxes", "POST", broken, 202);
  await delay(1000);
  const brokenState = await request(`/v1/sandboxes/${acceptedBroken.id}`);
  checks.push({
    name: "invalid entrypoint accepted: candidate Running alone is not workload readiness",
    initial: acceptedBroken.status,
    observed: brokenState.status,
  });
  assert.equal(brokenState.status.state, "Failed");
  goReceipt("start-failure", acceptedBroken.id, "Failed", false, true);
  assert.equal(docker("ps", "-aq", "--filter", "label=opensandbox.io/id"), "");
  docker("volume", "inspect", volume);
  checks.push({ name: "explicit cleanup of invalid-entrypoint sandbox retains workspace" });
} finally {
  if (serverCreated) {
    const log = spawnSync("docker", ["--context", "orbstack", "logs", server], {
      encoding: "utf8",
      timeout: 10000,
    });
    writeFileSync(
      `${directory}/server.log`,
      `${log.stdout ?? ""}${log.stderr ?? ""}`.replaceAll(apiKey, "[REDACTED]"),
      { mode: 0o600 },
    );
  }
  // Only IDs bearing this freshly generated ownership label can be forcibly compensated.
  const owned = docker("ps", "-aq", "--filter", `label=cloud-agents-poc=${run}`)
    .split("\n")
    .filter(Boolean);
  for (const id of owned) docker("rm", "-f", id);
  if (volumeCreated) {
    const info = JSON.parse(docker("volume", "inspect", volume))[0];
    assert.equal(info.Labels["cloud-agents-poc"], run);
    docker("volume", "rm", volume);
  }
  assert.equal(docker("ps", "-aq", "--filter", `label=cloud-agents-poc=${run}`), "");
  assert.equal(docker("volume", "ls", "-q", "--filter", `label=cloud-agents-poc=${run}`), "");
  // The private generated config may contain an API key; never copy it into evidence.
}
writeFileSync(
  `${directory}/evidence.json`,
  JSON.stringify(
    {
      run,
      serverImage,
      execdImage,
      sandboxImage,
      backend: "orbstack",
      checks,
      cleanup: "zero owned containers and volumes",
      boundary:
        "Candidate PoC only; no CP authority, Admin lifecycle, idempotency/fencing or multi-tenant qualification",
    },
    null,
    2,
  ),
);
process.stdout.write(
  `Verified ${checks.length} candidate checks; evidence ${directory}/evidence.json\n`,
);
