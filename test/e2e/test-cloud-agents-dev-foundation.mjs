import { execFileSync, spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { buildOpenSandboxServerSuccessor } from "../../scripts/build-opensandbox-server-successor.ts";

const [directory] = process.argv.slice(2);
if (!directory || !directory.startsWith("/"))
  throw new Error("absolute fixture directory is required");
const state = resolve(directory);
mkdirSync(state, { recursive: true, mode: 0o700 });
const run = `cloud-agents-dev-foundation-${process.pid}-${randomUUID()}`;
const serverName = `${run}-opensandbox`;
const registryName = `${run}-registry`;
const serverBuildDirectory = mkdtempSync(join(tmpdir(), "cloud-agents-dev-server-build-"));
const serverBuild = buildOpenSandboxServerSuccessor({
  outputRoot: serverBuildDirectory,
  tag: `cloud-agents/${run}-server:v1`,
});
const serverImage = serverBuild.image.immutableRef;
const execdImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/execd@sha256:1dc98c7de10b9a73450ac75aa0f200ad7972f2c40f5225f6a8998e166b45d6dd";
const egressImage =
  "sandbox-registry.cn-zhangjiakou.cr.aliyuncs.com/opensandbox/egress@sha256:973130e01bf76e8e686e2853ebf47b21741bc8781919bb4a7cf60af09a3c6e8a";
const runtimeSourceImage = "cloud-agents-r496-worker-proxy:latest";
const docker = (...args) =>
  execFileSync("docker", args, { encoding: "utf8", timeout: 120_000 }).trim();
const openssl = (...args) => execFileSync("openssl", args, { stdio: "ignore", timeout: 120_000 });
const waitFor = async (predicate, message, attempts = 100) => {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (await predicate()) return;
    await delay(100);
  }
  throw new Error(message);
};

for (const image of [serverImage, execdImage, egressImage, runtimeSourceImage, "registry:2"])
  docker("image", "inspect", image);
const dockerHost = docker(
  "context",
  "inspect",
  "orbstack",
  "--format",
  "{{.Endpoints.docker.Host}}",
);
if (!dockerHost.startsWith("unix://"))
  throw new Error("Docker context must expose a local Unix socket");
const dockerSocket = dockerHost.slice("unix://".length);
const dockerGateway = docker(
  "network",
  "inspect",
  "bridge",
  "--format",
  "{{(index .IPAM.Config 0).Gateway}}",
);
if (!dockerGateway) throw new Error("Docker bridge gateway is required");

const dockerCredentials = join(state, "docker-credentials");
const targetCredential = join(dockerCredentials, "local-docker-target");
const providerCredentials = join(state, "provider-credentials");
mkdirSync(targetCredential, { recursive: true, mode: 0o700 });
mkdirSync(providerCredentials, { recursive: true, mode: 0o700 });
writeFileSync(join(providerCredentials, "tenant-local.codex.json"), '{"payload":{}}\n', {
  mode: 0o600,
});

const caKey = join(state, "ca.key");
const caCert = join(state, "ca.pem");
const serverKey = join(state, "server.key");
const serverCSR = join(state, "server.csr");
const serverCert = join(state, "server.pem");
const clientKey = join(targetCredential, "key.pem");
const clientCSR = join(state, "client.csr");
const clientCert = join(targetCredential, "cert.pem");
const serial = join(state, "ca.srl");
const ext = join(state, "server.ext");
writeFileSync(ext, "subjectAltName=IP:127.0.0.1\n");
openssl(
  "req",
  "-x509",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-keyout",
  caKey,
  "-out",
  caCert,
  "-subj",
  "/CN=cloud-agents-dev-foundation-ca",
  "-days",
  "1",
);
openssl(
  "req",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-keyout",
  serverKey,
  "-out",
  serverCSR,
  "-subj",
  "/CN=127.0.0.1",
);
openssl(
  "x509",
  "-req",
  "-in",
  serverCSR,
  "-CA",
  caCert,
  "-CAkey",
  caKey,
  "-CAcreateserial",
  "-CAserial",
  serial,
  "-out",
  serverCert,
  "-days",
  "1",
  "-sha256",
  "-extfile",
  ext,
);
openssl(
  "req",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-keyout",
  clientKey,
  "-out",
  clientCSR,
  "-subj",
  "/CN=cloud-agents-dev-foundation-client",
);
openssl(
  "x509",
  "-req",
  "-in",
  clientCSR,
  "-CA",
  caCert,
  "-CAkey",
  caKey,
  "-CAcreateserial",
  "-CAserial",
  serial,
  "-out",
  clientCert,
  "-days",
  "1",
  "-sha256",
);
writeFileSync(join(targetCredential, "ca.pem"), readFileSync(caCert), { mode: 0o600 });
for (const path of [caKey, caCert, serverKey, serverCert, clientKey, clientCert])
  chmodSync(path, 0o600);

const proxyScript = join(state, "docker-proxy.mjs");
writeFileSync(
  proxyScript,
  `import { chmodSync, readFileSync, writeFileSync } from "node:fs";
import net from "node:net";
import tls from "node:tls";
const [socketPath, caPath, certPath, keyPath, portPath] = process.argv.slice(2);
const server = tls.createServer({ ca: readFileSync(caPath), cert: readFileSync(certPath), key: readFileSync(keyPath), minVersion: "TLSv1.2", requestCert: true, rejectUnauthorized: true }, (client) => {
  const upstream = net.createConnection(socketPath);
  const close = () => { client.destroy(); upstream.destroy(); };
  client.on("error", close); upstream.on("error", close);
  client.pipe(upstream); upstream.pipe(client);
});
server.on("tlsClientError", () => {});
server.listen(0, "127.0.0.1", () => { writeFileSync(portPath, String(server.address().port)); chmodSync(portPath, 0o600); });
process.on("SIGTERM", () => process.exit(0));
`,
);
chmodSync(proxyScript, 0o700);
const proxyPortFile = join(state, "docker-proxy.port");
const proxy = spawn(
  process.execPath,
  [proxyScript, dockerSocket, caCert, serverCert, serverKey, proxyPortFile],
  { stdio: "ignore" },
);
await waitFor(
  () => existsSync(proxyPortFile) && readFileSync(proxyPortFile, "utf8").trim(),
  "Docker mTLS proxy did not start",
);
const proxyPort = Number(readFileSync(proxyPortFile, "utf8"));
if (!Number.isInteger(proxyPort) || proxyPort < 1)
  throw new Error("Docker mTLS proxy returned an invalid port");

const apiKey = randomUUID();
const config = join(state, "opensandbox.toml");
writeFileSync(
  config,
  `[server]\nhost="0.0.0.0"\neip="127.0.0.1"\nport=8080\napi_key="${apiKey}"\n[runtime]\ntype="docker"\nexecd_image="${execdImage}"\n[egress]\nimage="${egressImage}"\nmode="dns+nft"\n[docker]\nnetwork_mode="bridge"\nhost_ip="${dockerGateway}"\nport_range_min=49530\nport_range_max=49740\n[storage]\nallowed_host_paths=[]\n[store]\ntype="sqlite"\npath="/tmp/opensandbox.db"\n`,
  { mode: 0o600 },
);
docker(
  "run",
  "-d",
  "--name",
  registryName,
  "--label",
  `cloud-agents.dev/run=${run}`,
  "-p",
  "127.0.0.1::5000",
  "registry:2",
);
const registryPort = Number(docker("port", registryName, "5000/tcp").split(":").at(-1));
if (!Number.isInteger(registryPort) || registryPort < 1)
  throw new Error("local registry port is invalid");
const runtimeTag = `127.0.0.1:${registryPort}/cloud-agents-runtime:dev`;
docker("tag", runtimeSourceImage, runtimeTag);
docker("push", runtimeTag);
const repoDigest = docker("image", "inspect", runtimeTag, "--format", "{{index .RepoDigests 0}}");
if (!repoDigest || !repoDigest.includes("@sha256:"))
  throw new Error("local runtime image has no content digest");
const releaseDigest = repoDigest.slice(repoDigest.indexOf("@") + 1);
const imageURI = repoDigest;
docker(
  "run",
  "-d",
  "--name",
  serverName,
  "--label",
  `cloud-agents.dev/run=${run}`,
  "-p",
  "127.0.0.1::8080",
  "--mount",
  `type=bind,src=${config},dst=/etc/opensandbox/config.toml,readonly`,
  "--mount",
  `type=bind,src=${dockerSocket},dst=/var/run/docker.sock`,
  serverImage,
);
const sandboxPort = Number(docker("port", serverName, "8080/tcp").split(":").at(-1));
if (!Number.isInteger(sandboxPort) || sandboxPort < 1)
  throw new Error("OpenSandbox port is invalid");
await waitFor(
  async () => {
    try {
      return (await fetch(`http://127.0.0.1:${sandboxPort}/health`)).ok;
    } catch {
      return false;
    }
  },
  "OpenSandbox did not become healthy",
  300,
);
writeFileSync(
  join(targetCredential, "opensandbox.json"),
  `${JSON.stringify({ endpoint: `http://127.0.0.1:${sandboxPort}`, apiKey })}\n`,
  { mode: 0o600 },
);
writeFileSync(
  join(state, "fixture.json"),
  `${JSON.stringify({ dockerCredentials, providerCredentials, dockerEndpoint: `https://127.0.0.1:${proxyPort}`, credentialRef: "local-docker-target", imageURI, releaseDigest, run, serverBinding: { evidencePath: join(serverBuildDirectory, "build-evidence.json"), recipeSha256: serverBuild.recipeSha256, image: serverBuild.image.immutableRef, architecture: serverBuild.image.architecture } })}\n`,
  { mode: 0o600 },
);
writeFileSync(join(state, "ready"), "ready\n", { mode: 0o600 });

const baselineContainers = new Set(
  docker("ps", "-aq", "--filter", "label=opensandbox.io/id").split("\n").filter(Boolean),
);
const baselineEgressContainers = new Set(
  docker("ps", "-aq", "--filter", "label=opensandbox.io/egress-sidecar-for")
    .split("\n")
    .filter(Boolean),
);
const baselineVolumes = new Set(
  docker("volume", "ls", "-q", "--filter", "label=opensandbox.io/volume-managed-by=server")
    .split("\n")
    .filter(Boolean),
);
writeFileSync(
  join(state, "baseline.json"),
  JSON.stringify({
    containers: [...baselineContainers],
    egressContainers: [...baselineEgressContainers],
    volumes: [...baselineVolumes],
  }),
);
const cleanup = () => {
  proxy.kill("SIGTERM");
  for (const container of docker("ps", "-aq", "--filter", "label=opensandbox.io/id")
    .split("\n")
    .filter(Boolean)) {
    if (!baselineContainers.has(container)) {
      try {
        docker("rm", "-f", container);
      } catch {}
    }
  }
  for (const container of docker("ps", "-aq", "--filter", "label=opensandbox.io/egress-sidecar-for")
    .split("\n")
    .filter(Boolean)) {
    if (!baselineEgressContainers.has(container)) {
      try {
        docker("rm", "-f", container);
      } catch {}
    }
  }
  try {
    docker("rm", "-f", serverName);
  } catch {}
  try {
    docker("rm", "-f", registryName);
  } catch {}
  try {
    docker("image", "rm", runtimeTag);
  } catch {}
  for (const volume of docker(
    "volume",
    "ls",
    "-q",
    "--filter",
    "label=opensandbox.io/volume-managed-by=server",
  )
    .split("\n")
    .filter(Boolean)) {
    if (!baselineVolumes.has(volume)) {
      try {
        docker("volume", "rm", volume);
      } catch {}
    }
  }
};
process.on("SIGTERM", () => {
  cleanup();
  process.exit(0);
});
process.on("SIGINT", () => {
  cleanup();
  process.exit(130);
});
await new Promise(() => {});
