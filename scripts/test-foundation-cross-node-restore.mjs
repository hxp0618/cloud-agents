import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readdirSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const [output] = process.argv.slice(2);
assert.ok(output, "usage: node scripts/test-foundation-cross-node-restore.mjs OUTPUT_DIRECTORY");
const root = resolve(import.meta.dirname, "..");
const evidenceDirectory = resolve(output);
mkdirSync(evidenceDirectory, { recursive: true, mode: 0o700 });
const build = mkdtempSync(resolve(tmpdir(), "cloud-agents-cross-node-restore-"));
const snapshotDirectory = resolve(build, "snapshots");
mkdirSync(snapshotDirectory, { mode: 0o700 });
const run = `cloud-agents-cross-node-${process.pid}`;
const sourceNode = `${run}-source`;
const dindImage = "docker@sha256:5efed980cba3fc126cf54e21a5a6ff8849d05b6e0623d6e7612f48e9cd6cd17e";
const actualSnapshotImage =
  "node@sha256:83f487e0a63425e5b4d146fb5e5be574bcbe1b7b843d3ebafdd95eaf7767a7e5";
const docker = (...args) =>
  execFileSync("docker", ["--context", "orbstack", ...args], {
    encoding: "utf8",
    timeout: 180_000,
  }).trim();
const mappedPort = (name, port) => {
  const address = docker("port", name, `${port}/tcp`).split("\n")[0];
  return Number(address.slice(address.lastIndexOf(":") + 1));
};
const parseMarker = (text, marker) => {
  const match = text.match(new RegExp(`${marker}=(\\{[^\\n]+\\})`));
  assert.ok(match, `${marker} missing from ${text}`);
  return JSON.parse(match[1]);
};
const checkedOutput = (file, args, options) => {
  const result = spawnSync(file, args, { ...options, encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(
      `${file} failed (${result.status})\n${result.stdout ?? ""}${result.stderr ?? ""}`,
    );
  }
  return result.stdout;
};

let sourceStarted = false;
try {
  docker("image", "inspect", dindImage);
  docker("image", "inspect", actualSnapshotImage);
  docker(
    "run",
    "-d",
    "--privileged",
    "--name",
    sourceNode,
    "--label",
    `cloud-agents-cross-node-restore=${run}`,
    "-e",
    "DOCKER_TLS_CERTDIR=",
    "-p",
    "127.0.0.1::2375",
    dindImage,
    "--host=tcp://0.0.0.0:2375",
    "--tls=false",
  );
  sourceStarted = true;
  const sourceAddress = `127.0.0.1:${mappedPort(sourceNode, 2375)}`;
  const innerDocker = (...args) =>
    execFileSync("docker", ["-H", `tcp://${sourceAddress}`, ...args], {
      encoding: "utf8",
      timeout: 180_000,
    }).trim();
  for (let attempt = 0; ; attempt++) {
    try {
      innerDocker("info", "--format", "{{.ServerVersion}}");
      break;
    } catch {
      if (attempt === 300) throw new Error("source Docker node did not start");
      await delay(100);
    }
  }
  const imageArchive = resolve(build, "snapshot-image.tar");
  docker("image", "save", "-o", imageArchive, actualSnapshotImage);
  const loaded = innerDocker("image", "load", "-i", imageArchive);
  const sourceHelperImage = loaded.match(/sha256:[0-9a-f]{64}/u)?.[0] ?? "";
  assert.match(sourceHelperImage, /^sha256:[0-9a-f]{64}$/u, loaded);
  const preflight = innerDocker("create", sourceHelperImage);
  innerDocker("rm", preflight);
  const testBinary = resolve(build, "dockertarget.test");
  execFileSync(
    "go",
    ["test", "-c", "-o", testBinary, "./services/control-plane/internal/dockertarget"],
    {
      cwd: root,
      env: { ...process.env, GOTOOLCHAIN: "local", GOFLAGS: "-mod=readonly" },
      timeout: 180_000,
    },
  );
  const sourceOutput = checkedOutput(
    testBinary,
    ["-test.run", "^TestLivePortableSnapshotSourceNode$", "-test.v"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_PORTABLE_SOURCE_DOCKER_ADDRESS: sourceAddress,
        CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY: snapshotDirectory,
        CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE: actualSnapshotImage,
        CLOUD_AGENTS_PORTABLE_SOURCE_HELPER_IMAGE: sourceHelperImage,
      },
      timeout: 120_000,
    },
  );
  const source = parseMarker(sourceOutput, "PORTABLE_SOURCE_SNAPSHOT");
  assert.equal(readdirSync(snapshotDirectory).length, 1);
  const failoverStarted = BigInt(Date.now()) * 1_000_000n;
  docker("rm", "-f", "-v", sourceNode);
  sourceStarted = false;
  const unavailable = spawnSync("docker", ["-H", `tcp://${sourceAddress}`, "info"], {
    encoding: "utf8",
    timeout: 10_000,
  });
  assert.notEqual(unavailable.status, 0, "removed source Docker node remained reachable");
  const archivePath = resolve(snapshotDirectory, `${source.physicalSnapshotId}.tar`);
  const negativeOutputs = [];
  for (const mode of ["missing", "corrupt"]) {
    const backupPath = `${archivePath}.${mode}.backup`;
    renameSync(archivePath, backupPath);
    if (mode === "corrupt") writeFileSync(archivePath, Buffer.from("corrupted"), { mode: 0o600 });
    try {
      negativeOutputs.push(`${mode}\n${checkedOutput(
        testBinary,
        ["-test.run", "^TestLivePortableSnapshotDestinationRejectsUnavailableArchive$", "-test.v"],
        {
          encoding: "utf8",
          env: {
            ...process.env,
            CLOUD_AGENTS_PORTABLE_DESTINATION_DOCKER_SOCKET: docker(
              "context",
              "inspect",
              "orbstack",
              "--format",
              "{{.Endpoints.docker.Host}}",
            ).replace(/^unix:\/\//u, ""),
            CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY: snapshotDirectory,
            CLOUD_AGENTS_PORTABLE_PHYSICAL_SNAPSHOT_ID: source.physicalSnapshotId,
            CLOUD_AGENTS_PORTABLE_CONTENT_DIGEST: source.contentDigest,
            CLOUD_AGENTS_PORTABLE_NEGATIVE_MODE: mode,
            CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE: actualSnapshotImage,
          },
          timeout: 120_000,
        },
      )}`);
    } finally {
      renameSync(backupPath, archivePath);
    }
  }
  const destinationSocket = docker(
    "context",
    "inspect",
    "orbstack",
    "--format",
    "{{.Endpoints.docker.Host}}",
  ).replace(/^unix:\/\//u, "");
  const destinationOutput = checkedOutput(
    testBinary,
    ["-test.run", "^TestLivePortableSnapshotDestinationNode$", "-test.v"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        CLOUD_AGENTS_PORTABLE_DESTINATION_DOCKER_SOCKET: destinationSocket,
        CLOUD_AGENTS_PORTABLE_SNAPSHOT_DIRECTORY: snapshotDirectory,
        CLOUD_AGENTS_PORTABLE_PHYSICAL_SNAPSHOT_ID: source.physicalSnapshotId,
        CLOUD_AGENTS_PORTABLE_CONTENT_DIGEST: source.contentDigest,
        CLOUD_AGENTS_PORTABLE_FAILOVER_STARTED_UNIX_NANO: String(failoverStarted),
        CLOUD_AGENTS_PORTABLE_SNAPSHOT_IMAGE: actualSnapshotImage,
      },
      timeout: 120_000,
    },
  );
  const destination = parseMarker(destinationOutput, "PORTABLE_DESTINATION_RESTORE");
  assert.equal(destination.contentDigest, source.contentDigest);
  assert.equal(destination.rpoBytes, 0);
  assert.deepEqual(readdirSync(snapshotDirectory), []);
  assert.equal(
    docker("volume", "ls", "-q", "--filter", "label=cloud-agents.dev/target=destination-node"),
    "",
  );
  const evidence = {
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
    nodes: {
      source: { runtime: "disposable Docker-in-Docker", removedBeforeRestore: true },
      destination: { runtime: "OrbStack Docker", distinctDaemon: true },
    },
    snapshot: source,
    restore: destination,
    checks: [
      "source Workspace bytes were copied into a content-verified portable archive",
      "the source Docker daemon and its local volume were removed before destination restore began",
      "the destination rejected a missing portable archive before creating a Workspace volume",
      "the destination rejected a corrupted portable archive before creating a Workspace volume",
      "the destination daemon restored the exact normalized archive digest without a source endpoint",
      "destination volume, helpers, source node and portable archive were precisely removed",
    ],
  };
  writeFileSync(
    resolve(evidenceDirectory, "evidence.json"),
    JSON.stringify(evidence, null, 2) + "\n",
  );
  writeFileSync(resolve(evidenceDirectory, "source.log"), sourceOutput);
  writeFileSync(resolve(evidenceDirectory, "destination.log"), destinationOutput);
  writeFileSync(resolve(evidenceDirectory, "negative.log"), negativeOutputs.join("\n"));
  process.stdout.write(
    `Verified physical cross-node Workspace restore; evidence ${resolve(evidenceDirectory, "evidence.json")}\n`,
  );
} finally {
  if (sourceStarted) spawnSync("docker", ["--context", "orbstack", "rm", "-f", "-v", sourceNode]);
  rmSync(build, { recursive: true, force: true });
  assert.equal(docker("ps", "-aq", "--filter", `label=cloud-agents-cross-node-restore=${run}`), "");
}
