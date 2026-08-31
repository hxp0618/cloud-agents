import { spawn, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  cpSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  utimesSync,
  writeFileSync,
} from "node:fs";
import { join, resolve } from "node:path";

import { buildPlatformTypeScriptSDKPackage } from "./lib/platform-release";

const repositoryRoot = resolve(import.meta.dirname, "..");
const toolchainRoot = process.env.CLOUD_AGENTS_A24_TOOLCHAIN;
const bun = toolchainRoot ? join(toolchainRoot, "bun") : "bun";
const go = toolchainRoot ? join(toolchainRoot, "go") : "go";
const packageVersion = "0.0.0-a3.2";
const version = `v${packageVersion}`;
const modulePath = "github.com/hxp0618/cloud-agents/sdk/go";
const sdkPackage = "@synara/cloud-agent-platform-sdk";
const npmPackages = [
  {
    name: "@bufbuild/protobuf",
    version: "2.14.0",
    packageRoot: resolve(repositoryRoot, "node_modules/@bufbuild/protobuf"),
    filename: "bufbuild-protobuf-2.14.0.tgz",
  },
  {
    name: "@connectrpc/connect",
    version: "2.1.2",
    packageRoot: findBunPackage("@connectrpc+connect@2.1.2", "@connectrpc/connect"),
    filename: "connectrpc-connect-2.1.2.tgz",
  },
  {
    name: "@types/node",
    version: "24.10.13",
    packageRoot: resolve(repositoryRoot, "node_modules/@types/node"),
    filename: "types-node-24.10.13.tgz",
  },
  {
    name: "undici-types",
    version: "7.16.0",
    packageRoot: findBunPackage("undici-types@7.16.0", "undici-types"),
    filename: "undici-types-7.16.0.tgz",
  },
  {
    name: "typescript",
    version: "5.7.3",
    packageRoot: resolve(
      repositoryRoot,
      "node_modules/.bun/typescript@5.7.3/node_modules/typescript",
    ),
    filename: "typescript-5.7.3.tgz",
  },
] as const;

type Artifact = Readonly<{
  path: string;
  sha256: string;
  integrity: string;
}>;

type ModuleProxy = Readonly<{
  zip: Artifact;
  goModSha256: string;
}>;

type NpmDependencyArtifact = Readonly<{
  version: string;
  artifactPath: string;
  integrity: string;
}>;
type NpmRegistry = Readonly<Record<string, NpmDependencyArtifact>>;

type FixtureServer = Readonly<{
  baseUrl: string;
  requestLogPath: string;
  stop: () => Promise<void>;
}>;

async function main(): Promise<void> {
  const temporaryBase = resolve(repositoryRoot, ".tmp");
  mkdirSync(temporaryBase, { recursive: true });
  const temporaryRoot = mkdtempSync(join(temporaryBase, "platform-sdk-consumer-"));
  let fixtureServer: FixtureServer | undefined;
  try {
    const typescriptArtifact = packTypeScriptSDK(temporaryRoot);
    const npmRegistry = prepareNpmRegistry(temporaryRoot);
    const goProxy = buildGoModuleProxy(temporaryRoot);
    fixtureServer = await startArtifactServer(temporaryRoot);
    runFreshTypeScriptConsumer(
      temporaryRoot,
      typescriptArtifact,
      npmRegistry,
      fixtureServer.baseUrl,
      fixtureServer.requestLogPath,
    );
    runFreshGoConsumer(temporaryRoot, goProxy, fixtureServer.baseUrl, fixtureServer.requestLogPath);
    process.stdout.write("platform-sdk-consumers: fresh TypeScript and Go consumers passed\n");
    process.stdout.write(
      `platform-sdk-consumers: typescriptArtifactSha256=${typescriptArtifact.sha256}\n`,
    );
    process.stdout.write(
      `platform-sdk-consumers: typescriptArtifactIntegrity=${typescriptArtifact.integrity}\n`,
    );
    process.stdout.write(`platform-sdk-consumers: goModuleZipSha256=${goProxy.zip.sha256}\n`);
    process.stdout.write(`platform-sdk-consumers: goModuleGoModSha256=${goProxy.goModSha256}\n`);
  } finally {
    await fixtureServer?.stop();
    try {
      makeWritableForCleanup(temporaryRoot);
      rmSync(temporaryRoot, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
    } catch (error) {
      process.stderr.write(
        `platform-sdk-consumers: temporary cleanup deferred: ${String(error)}\n`,
      );
    }
  }
}

function makeWritableForCleanup(path: string): void {
  const stat = lstatSync(path);
  if (stat.isSymbolicLink()) return;
  chmodSync(path, stat.isDirectory() ? 0o700 : 0o600);
  if (stat.isDirectory()) {
    for (const child of readdirSync(path)) makeWritableForCleanup(join(path, child));
  }
}

void main().catch((error: unknown) => {
  process.stderr.write(
    `${error instanceof Error ? (error.stack ?? error.message) : String(error)}\n`,
  );
  process.exitCode = 1;
});

function packTypeScriptSDK(root: string): Artifact {
  run(bun, ["run", "--cwd", "sdk/typescript", "build"], repositoryRoot);
  const output = join(root, "typescript-pack");
  mkdirSync(output, { recursive: true });
  const filename = `synara-cloud-agent-platform-sdk-${packageVersion}.tgz`;
  const path = join(output, filename);
  writeFileSync(path, buildPlatformTypeScriptSDKPackage(repositoryRoot, packageVersion));
  return artifact(path);
}

function prepareNpmRegistry(root: string): NpmRegistry {
  const registry = join(root, "npm-registry");
  mkdirSync(registry, { recursive: true });
  const artifacts: Record<string, NpmDependencyArtifact> = {};
  for (const { name, version, packageRoot, filename: expectedFilename } of npmPackages) {
    const manifest = JSON.parse(readFileSync(join(packageRoot, "package.json"), "utf8")) as Record<
      string,
      unknown
    >;
    if (manifest.name !== name || manifest.version !== version)
      throw new Error(`Offline npm dependency drifted before packing: ${name}`);
    const output = join(registry, `${name.replace("/", "__")}.pack`);
    mkdirSync(output, { recursive: true });
    const packed = JSON.parse(
      run(
        "npm",
        ["pack", "--ignore-scripts", "--json", "--pack-destination", output, packageRoot],
        repositoryRoot,
      ),
    ) as Array<Record<string, unknown>>;
    const filename = packed[0]?.filename;
    if (filename !== expectedFilename)
      throw new Error(`Offline npm dependency filename drifted: ${name}`);
    const packedArtifact = artifact(join(output, filename));
    artifacts[name] = {
      version,
      artifactPath: `npm-registry-tar/${name}/${filename}`,
      integrity: packedArtifact.integrity,
    };
    writeFileSync(
      join(registry, `${name.replace("/", "__")}.json`),
      JSON.stringify(
        {
          name,
          version,
          filename,
          integrity: packedArtifact.integrity,
        },
        null,
        2,
      ),
    );
  }
  return artifacts;
}

function findBunPackage(prefix: string, relativePackage: string): string {
  const entry = readdirSync(resolve(repositoryRoot, "node_modules/.bun")).find((name) =>
    name.startsWith(prefix),
  );
  if (!entry) throw new Error(`Bun cache package not found: ${prefix}`);
  return resolve(repositoryRoot, "node_modules/.bun", entry, "node_modules", relativePackage);
}

function npmTarballUrl(root: string, baseUrl: string, name: string): string {
  const metadata = JSON.parse(
    readFileSync(join(root, "npm-registry", `${name.replace("/", "__")}.json`), "utf8"),
  ) as { filename: string };
  return `${baseUrl}/npm-registry-tar/${name}/${metadata.filename}`;
}

function npmTarballPath(root: string, name: string): string {
  const metadata = JSON.parse(
    readFileSync(join(root, "npm-registry", `${name.replace("/", "__")}.json`), "utf8"),
  ) as { filename: string };
  return `/npm-registry-tar/${name}/${metadata.filename}`;
}

function runFreshTypeScriptConsumer(
  root: string,
  sdk: Artifact,
  npmRegistry: NpmRegistry,
  baseUrl: string,
  requestLogPath: string,
): void {
  const consumer = join(root, "typescript-consumer");
  mkdirSync(consumer, { recursive: true });
  const filename = sdk.path.split("/").pop();
  if (!filename) throw new Error("TypeScript artifact filename is empty.");
  const tarballUrl = `${baseUrl}/typescript-pack/${encodeURIComponent(filename)}`;
  const projectFixture = readFileSync(
    resolve(repositoryRoot, "contracts/platform/v1alpha1/fixtures/golden/project.json"),
    "utf8",
  ).trim();
  writeFileSync(
    join(consumer, "package.json"),
    `${JSON.stringify(
      {
        name: "fresh-cloud-agents-sdk-consumer",
        private: true,
        type: "module",
        scripts: { check: "tsc --noEmit" },
        dependencies: {
          "@bufbuild/protobuf": npmTarballUrl(root, baseUrl, "@bufbuild/protobuf"),
          "@connectrpc/connect": npmTarballUrl(root, baseUrl, "@connectrpc/connect"),
          [sdkPackage]: tarballUrl,
        },
        devDependencies: {
          "@types/node": npmTarballUrl(root, baseUrl, "@types/node"),
          "undici-types": npmTarballUrl(root, baseUrl, "undici-types"),
          typescript: npmTarballUrl(root, baseUrl, "typescript"),
        },
      },
      null,
      2,
    )}\n`,
  );
  writeFileSync(
    join(consumer, "main.ts"),
    `import { createServer } from "node:http";
import { createClient } from "@connectrpc/connect";
import { createFetchClient } from "@connectrpc/connect/protocol";
import { createTransport } from "@connectrpc/connect/protocol-connect";
import { create, toBinary } from "@bufbuild/protobuf";
import { createHTTPClient } from "${sdkPackage}/platform";
import { NegotiationResponseSchema, WorkerExecutionService } from "${sdkPackage}/proto";

const project = ${projectFixture};
let controlPlaneRequests = 0;
let workerRequests = 0;
const fixture = createServer(async (request, response) => {
  if (
    request.method === "GET" &&
    request.url === "/v1/tenants/tenant-alpha/projects/project-alpha"
  ) {
    if (
      request.headers.authorization !== "Bearer token-alpha" ||
      request.headers["x-request-id"] !== "request-alpha"
    ) {
      response.statusCode = 401;
      response.end();
      return;
    }
    controlPlaneRequests += 1;
    response.statusCode = 200;
    response.setHeader("Content-Type", "application/json");
    response.setHeader("X-Resource-Version", "3");
    response.end(JSON.stringify(project));
    return;
  }
  const chunks: Buffer[] = [];
  for await (const chunk of request) chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  if (
    request.method !== "POST" ||
    request.url !== "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate" ||
    request.headers["content-type"] !== "application/proto"
  ) {
    response.statusCode = 404;
    response.end();
    return;
  }
  if (Buffer.concat(chunks).byteLength !== 0) throw new Error("fixture received a non-empty negotiation request");
  workerRequests += 1;
  response.statusCode = 200;
  response.setHeader("Content-Type", "application/proto");
  response.end(Buffer.from(toBinary(NegotiationResponseSchema, create(NegotiationResponseSchema))));
});

await new Promise<void>((resolve, reject) => {
  fixture.once("error", reject);
  fixture.listen(0, "127.0.0.1", () => resolve());
});
try {
  const address = fixture.address();
  if (!address || typeof address === "string") throw new Error("fixture did not bind a loopback port");
  const baseUrl = "http://127.0.0.1:" + address.port;
  const platformClient = createHTTPClient(baseUrl, "token-alpha");
  const loadedProject = await platformClient.getProject("tenant-alpha", "project-alpha", "request-alpha");
  if (loadedProject.value.metadata.uid !== "project-alpha") throw new Error("fresh TypeScript Control Plane client returned the wrong project");
  const transport = createTransport({
    baseUrl,
    httpClient: createFetchClient(fetch),
    useBinaryFormat: true,
    interceptors: [],
    acceptCompression: [],
    sendCompression: null,
    compressMinBytes: 1024,
    readMaxBytes: 1024 * 1024,
    writeMaxBytes: 1024 * 1024,
  });
  const client = createClient(WorkerExecutionService, transport);
  await client.negotiate({});
  if (controlPlaneRequests !== 1 || workerRequests !== 1) throw new Error("fresh TypeScript consumer did not make both loopback calls exactly once");
  console.log("fresh-typescript-consumer-ok");
} finally {
  await new Promise<void>((resolve) => fixture.close(() => resolve()));
}
`,
  );
  writeFileSync(
    join(consumer, "tsconfig.json"),
    `${JSON.stringify(
      {
        compilerOptions: {
          strict: true,
          target: "ES2022",
          module: "NodeNext",
          moduleResolution: "NodeNext",
          noEmit: true,
          skipLibCheck: false,
        },
        include: ["main.ts"],
      },
      null,
      2,
    )}\n`,
  );
  const bunCache = join(root, "bun-cache");
  mkdirSync(bunCache, { recursive: true });
  try {
    run(
      bun,
      [
        "install",
        "--no-progress",
        "--ignore-scripts",
        `--cache-dir=${bunCache}`,
        `--registry=${baseUrl}/npm-registry/`,
      ],
      consumer,
      undefined,
      {
        npm_config_registry: `${baseUrl}/npm-registry/`,
        BUN_CONFIG_REGISTRY: `${baseUrl}/npm-registry/`,
        NO_PROXY: "127.0.0.1,localhost",
        no_proxy: "127.0.0.1,localhost",
      },
    );
  } catch (cause) {
    throw new Error(
      `${String(cause)}\nfixture request log:\n${readFileSync(requestLogPath, "utf8")}`,
    );
  }
  const installedManifest = join(consumer, "node_modules", sdkPackage, "package.json");
  const manifest = JSON.parse(readFileSync(installedManifest, "utf8")) as Record<string, unknown>;
  if (manifest.name !== sdkPackage || manifest.version !== packageVersion) {
    throw new Error("Fresh TypeScript consumer did not install the exact SDK package.");
  }
  assertExactDependency(manifest, "@bufbuild/protobuf", "2.14.0");
  assertExactDependency(manifest, "@connectrpc/connect", "2.1.2");
  assertNoLocalDependency(manifest, "packed TypeScript SDK");
  const lockPath = join(consumer, "bun.lock");
  const lockText = readFileSync(lockPath, "utf8");
  if (!lockText.includes(tarballUrl) || !lockText.includes(sdk.integrity)) {
    throw new Error("Fresh TypeScript lock did not bind the exact HTTP artifact and integrity.");
  }
  assertNoLocalDependencyText(lockText, "TypeScript consumer lock");
  const lockUrls = lockText.match(/https?:\/\/[^\s",)]+/gu) ?? [];
  if (lockUrls.some((url) => !url.startsWith("http://127.0.0.1:"))) {
    throw new Error("TypeScript consumer lock contains a non-loopback URL.");
  }
  for (const [name, dependency] of Object.entries(npmRegistry)) {
    const installedManifest = join(consumer, "node_modules", ...name.split("/"), "package.json");
    const installed = JSON.parse(readFileSync(installedManifest, "utf8")) as Record<
      string,
      unknown
    >;
    if (installed.name !== name || installed.version !== dependency.version) {
      throw new Error(`Fresh TypeScript dependency drifted: ${name}`);
    }
    const url = `${baseUrl}/${dependency.artifactPath}`;
    if (!lockText.includes(url) || !lockText.includes(dependency.integrity)) {
      throw new Error(`Fresh TypeScript lock did not bind ${name} artifact and integrity.`);
    }
  }
  assertRequestLogged(requestLogPath, `/typescript-pack/${encodeURIComponent(filename)}`);
  for (const dependency of [
    "@bufbuild/protobuf",
    "@connectrpc/connect",
    "@types/node",
    "undici-types",
    "typescript",
  ])
    assertRequestLogged(requestLogPath, npmTarballPath(root, dependency));
  run(bun, ["run", "check"], consumer);
  run(bun, ["run", "main.ts"], consumer);
}

function buildGoModuleProxy(root: string): ModuleProxy {
  const staging = join(root, "go-stage");
  const proxy = join(root, "go-proxy");
  const moduleRoot = join(staging, `${modulePath}@${version}`);
  mkdirSync(staging, { recursive: true });
  cpSync(resolve(repositoryRoot, "sdk/go"), moduleRoot, { recursive: true });
  normalizeArchiveTimestamps(moduleRoot);
  const moduleVersionRoot = join(proxy, modulePath, "@v");
  mkdirSync(moduleVersionRoot, { recursive: true });
  const zipPath = join(moduleVersionRoot, `${version}.zip`);
  run("zip", ["-X", "-q", "-r", zipPath, `${modulePath}@${version}`], staging);
  const modPath = join(moduleVersionRoot, `${version}.mod`);
  writeFileSync(modPath, readFileSync(join(moduleRoot, "go.mod")));
  writeFileSync(
    join(moduleVersionRoot, `${version}.info`),
    `${JSON.stringify({ Version: version, Time: "2026-08-21T00:00:00Z" })}\n`,
  );
  return {
    zip: artifact(zipPath),
    goModSha256: sha256File(modPath),
  };
}

function normalizeArchiveTimestamps(path: string): void {
  const fixed = new Date("2000-01-01T00:00:00Z");
  const stat = lstatSync(path);
  if (stat.isDirectory()) {
    for (const child of [...readdirSync(path)].sort()) {
      normalizeArchiveTimestamps(join(path, child));
    }
  }
  utimesSync(path, fixed, fixed);
}

function runFreshGoConsumer(
  root: string,
  module: ModuleProxy,
  baseUrl: string,
  requestLogPath: string,
): void {
  const consumer = join(root, "go-consumer");
  mkdirSync(consumer, { recursive: true });
  run(go, ["mod", "init", "example.com/fresh-cloud-agents-sdk-consumer"], consumer);
  run(go, ["mod", "edit", `-require=${modulePath}@${version}`], consumer);
  const projectFixture = readFileSync(
    resolve(repositoryRoot, "contracts/platform/v1alpha1/fixtures/golden/project.json"),
    "utf8",
  ).trim();
  writeFileSync(
    join(consumer, "main.go"),
    `package main

import (
  "context"
  "fmt"
  "net/http"
  "net/http/httptest"
  "sync/atomic"

  connect "connectrpc.com/connect"
  workerv1alpha1 "${modulePath}/gen/cloudagents/worker/v1alpha1"
  workerv1alpha1connect "${modulePath}/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
  platformv1alpha1 "${modulePath}/gen/openapi/v1alpha1"
)

const projectResponse = \`${projectFixture}\`

type fixtureService struct {
  workerv1alpha1connect.UnimplementedWorkerExecutionServiceHandler
  calls atomic.Int32
}

func (f *fixtureService) Negotiate(_ context.Context, request *connect.Request[workerv1alpha1.NegotiationRequest]) (*connect.Response[workerv1alpha1.NegotiationResponse], error) {
  if request.Header().Get("Content-Type") != "application/proto" {
    panic(fmt.Sprintf("unexpected Go request content type: %s", request.Header().Get("Content-Type")))
  }
  f.calls.Add(1)
  response := connect.NewResponse(&workerv1alpha1.NegotiationResponse{})
  response.Header().Set("Content-Type", "application/proto")
  return response, nil
}

func main() {
  service := &fixtureService{}
  path, handler := workerv1alpha1connect.NewWorkerExecutionServiceHandler(service)
  mux := http.NewServeMux()
  mux.Handle(path, handler)
  var controlPlaneCalls atomic.Int32
  mux.HandleFunc("/v1/tenants/tenant-alpha/projects/project-alpha", func(response http.ResponseWriter, request *http.Request) {
    if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer token-alpha" || request.Header.Get("X-Request-ID") != "request-alpha" {
      http.Error(response, "unauthorized", http.StatusUnauthorized)
      return
    }
    controlPlaneCalls.Add(1)
    response.Header().Set("Content-Type", "application/json")
    response.Header().Set("X-Resource-Version", "3")
    _, _ = response.Write([]byte(projectResponse))
  })
  fixture := httptest.NewServer(mux)
  defer fixture.Close()
  platformClient, err := platformv1alpha1.NewHTTPClient(fixture.URL, "token-alpha")
  if err != nil {
    panic(fmt.Sprintf("generated Go Control Plane client construction failed: %v", err))
  }
  project, err := platformClient.GetProject(context.Background(), "tenant-alpha", "project-alpha", "request-alpha")
  if err != nil {
    panic(fmt.Sprintf("generated Go Control Plane client loopback call failed: %v", err))
  }
  if project.Value.Metadata.UID != "project-alpha" || controlPlaneCalls.Load() != 1 {
    panic("generated Go Control Plane client returned the wrong project")
  }
  client := workerv1alpha1connect.NewWorkerExecutionServiceClient(http.DefaultClient, fixture.URL)
  response, err := client.Negotiate(context.Background(), connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
  if err != nil {
    panic(fmt.Sprintf("generated Go consumer loopback call failed: %v", err))
  }
  if response.Header().Get("Content-Type") != "application/proto" {
    panic(fmt.Sprintf("unexpected Go response content type: %s", response.Header().Get("Content-Type")))
  }
  if got := service.calls.Load(); got != 1 {
    panic(fmt.Sprintf("generated Go consumer made %d loopback calls", got))
  }
  fmt.Println("fresh-go-consumer-ok")
}
`,
  );
  const goModuleCache = join(root, "go-mod-cache");
  mkdirSync(goModuleCache, { recursive: true });
  const env = {
    GOPROXY: `${baseUrl}/go-proxy,https://proxy.golang.org,direct`,
    GOSUMDB: "sum.golang.org",
    GONOSUMDB: modulePath,
    GOWORK: "off",
    GOTOOLCHAIN: "local",
    GOMODCACHE: goModuleCache,
  };
  run(go, ["mod", "tidy"], consumer, undefined, {
    ...env,
    GOFLAGS: "-mod=mod",
  });
  run(go, ["run", "."], consumer, undefined, {
    ...env,
    GOFLAGS: "-mod=readonly",
  });
  const consumerModule = readFileSync(join(consumer, "go.mod"), "utf8");
  if (
    /^replace\s/mu.test(consumerModule) ||
    /(?:file:|git(?:\+|$)|workspace:)/u.test(consumerModule)
  ) {
    throw new Error("Fresh Go consumer contains a replace, file, git, or workspace dependency.");
  }
  if (!consumerModule.includes(`${modulePath} ${version}`)) {
    throw new Error("Fresh Go consumer did not resolve the exact packed SDK module version.");
  }
  const download = JSON.parse(
    run(go, ["mod", "download", "-json", `${modulePath}@${version}`], consumer, undefined, {
      ...env,
      GOFLAGS: "-mod=readonly",
    }),
  ) as Record<string, unknown>;
  if (typeof download.Zip !== "string" || typeof download.GoMod !== "string") {
    throw new Error("Fresh Go consumer module download omitted module paths.");
  }
  if (typeof download.GoMod !== "string" || sha256File(download.GoMod) !== module.goModSha256) {
    throw new Error("Fresh Go consumer downloaded go.mod bytes differ from the served artifact.");
  }
  const proxyPrefix = `/go-proxy/${modulePath}/@v/${version}`;
  for (const suffix of [".info", ".mod", ".zip"] as const) {
    assertRequestLogged(requestLogPath, `${proxyPrefix}${suffix}`);
  }
  if (typeof download.Sum !== "string" || typeof download.GoModSum !== "string") {
    throw new Error("Fresh Go consumer module download omitted exact checksums.");
  }
  const consumerSums = readFileSync(join(consumer, "go.sum"), "utf8");
  if (
    !consumerSums.includes(`${modulePath} ${version} ${download.Sum}`) ||
    !consumerSums.includes(`${modulePath} ${version}/go.mod ${download.GoModSum}`)
  ) {
    throw new Error("Fresh Go consumer go.sum did not bind the exact module and go.mod checksums.");
  }
}

function startArtifactServer(root: string): Promise<FixtureServer> {
  const serverScript = join(root, "artifact-server.mjs");
  const requestLogPath = join(root, "artifact-requests.log");
  writeFileSync(requestLogPath, "");
  writeFileSync(
    serverScript,
    `import { appendFileSync, createReadStream, lstatSync, readFileSync, realpathSync } from "node:fs";
import { createServer } from "node:http";
import { resolve, sep } from "node:path";

const root = realpathSync(resolve(process.env.FIXTURE_ROOT));
const requestLog = process.env.REQUEST_LOG;
function loopbackPort() {
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("artifact fixture is not listening");
  return address.port;
}
function safeFile(path) {
  const stat = lstatSync(path);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error("fixture path is not a regular file");
  const real = realpathSync(path);
  if (real !== root && !real.startsWith(root + sep)) throw new Error("fixture path escapes root");
  return stat;
}
function validPackageName(name) {
  return /^(?:@[A-Za-z0-9._-]+\\/[A-Za-z0-9._-]+|[A-Za-z0-9._-]+)$/.test(name);
}
const server = createServer((request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url ?? "/", "http://127.0.0.1").pathname);
    if (requestLog) appendFileSync(requestLog, pathname + "\\n");
    if (request.method !== "GET") {
      response.statusCode = 405;
      response.end();
      return;
    }
    if (pathname.startsWith("/npm-registry/")) {
      const packageName = pathname.slice("/npm-registry/".length);
      if (!validPackageName(packageName)) throw new Error("invalid npm package name");
      const metadataPath = resolve(root, "npm-registry", packageName.replace("/", "__") + ".json");
      const metadata = JSON.parse(readFileSync(metadataPath, "utf8"));
      const packagePath = "/npm-registry-tar/" + packageName + "/" + metadata.filename;
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify({ name: metadata.name, "dist-tags": { latest: metadata.version }, versions: { [metadata.version]: { name: metadata.name, version: metadata.version, dist: { tarball: "http://127.0.0.1:" + loopbackPort() + packagePath, integrity: metadata.integrity } } } }));
      return;
    }
    if (pathname.startsWith("/npm-registry-tar/")) {
      const rest = pathname.slice("/npm-registry-tar/".length);
      const split = rest.lastIndexOf("/");
      const packageName = rest.slice(0, split);
      const filename = rest.slice(split + 1);
      if (split <= 0 || !validPackageName(packageName) || !/^[A-Za-z0-9._-]+\\.tgz$/.test(filename)) throw new Error("invalid npm tarball path");
      const path = resolve(root, "npm-registry", packageName.replace("/", "__") + ".pack", filename);
      const stat = safeFile(path);
      response.statusCode = 200;
      response.setHeader("Content-Length", stat.size);
      createReadStream(path).pipe(response);
      return;
    }
    const path = resolve(root, "." + pathname);
    if (path !== root && !path.startsWith(root + sep)) {
      response.statusCode = 403;
      response.end();
      return;
    }
    const stat = safeFile(path);
    response.statusCode = 200;
    response.setHeader("Content-Length", stat.size);
    createReadStream(path).pipe(response);
  } catch (error) {
    if (requestLog) appendFileSync(requestLog, "ERROR " + String(error) + "\\n");
    response.statusCode = 404;
    response.end();
  }
});
server.listen(0, "127.0.0.1", () => {
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("artifact fixture did not bind");
  process.stdout.write("READY " + address.port + "\\n");
});
process.on("SIGTERM", () => server.close(() => process.exit(0)));
`,
  );
  const child = spawn(bun, [serverScript], {
    cwd: repositoryRoot,
    env: { ...process.env, FIXTURE_ROOT: root, REQUEST_LOG: requestLogPath },
    stdio: ["ignore", "pipe", "pipe"],
  });
  return new Promise<FixtureServer>((resolveServer, reject) => {
    let stdout = "";
    let stderr = "";
    let ready = false;
    child.stdout?.on("data", (chunk: Buffer) => {
      stdout += chunk.toString("utf8");
      const match = stdout.match(/(?:^|\n)READY (\d+)\n?/u);
      if (!match || ready) return;
      ready = true;
      const port = Number(match[1]);
      resolveServer({
        baseUrl: `http://127.0.0.1:${port}`,
        requestLogPath,
        stop: () =>
          new Promise<void>((resolveStop) => {
            child.once("exit", () => resolveStop());
            child.kill("SIGTERM");
          }),
      });
    });
    child.stderr?.on("data", (chunk: Buffer) => {
      stderr += chunk.toString("utf8");
    });
    child.once("error", (error) => {
      if (!ready) reject(error);
    });
    child.once("exit", (code) => {
      if (!ready)
        reject(new Error(`artifact fixture exited before ready (${String(code)}): ${stderr}`));
    });
  });
}

function assertRequestLogged(logPath: string, path: string): void {
  const requests = readFileSync(logPath, "utf8").split("\n");
  if (!requests.includes(path)) {
    throw new Error(`artifact request was not served from the loopback fixture: ${path}`);
  }
}

function run(
  command: string,
  args: ReadonlyArray<string>,
  cwd: string,
  input?: string,
  environment?: Record<string, string>,
): string {
  const result = spawnSync(command, [...args], {
    cwd,
    encoding: "utf8",
    input,
    maxBuffer: 32 * 1024 * 1024,
    env: { ...process.env, npm_config_update_notifier: "false", ...environment },
  });
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}).\n${result.stdout}\n${result.stderr}`,
    );
  }
  return result.stdout;
}

function artifact(path: string): Artifact {
  return {
    path,
    sha256: sha256File(path),
    integrity: `sha512-${createHash("sha512").update(readFileSync(path)).digest("base64")}`,
  };
}

function sha256File(path: string): string {
  return `sha256:${createHash("sha256").update(readFileSync(path)).digest("hex")}`;
}

function assertExactDependency(
  manifest: Record<string, unknown>,
  name: string,
  expectedVersion: string,
): void {
  const dependencies = manifest.dependencies;
  if (!isRecord(dependencies) || dependencies[name] !== expectedVersion) {
    throw new Error(`Packed TypeScript SDK dependency drifted: ${name}`);
  }
}

function assertNoLocalDependency(value: Record<string, unknown>, label: string): void {
  const dependencyFields = [
    "dependencies",
    "devDependencies",
    "optionalDependencies",
    "peerDependencies",
    "bundledDependencies",
    "packages",
    "snapshots",
  ];
  const candidates: unknown[] = [];
  for (const field of dependencyFields) {
    const candidate = value[field];
    if (candidate !== undefined) candidates.push(candidate);
  }
  const text = JSON.stringify(candidates);
  if (/(?:workspace:|file:|git(?:\+|:|$)|github:)/u.test(text)) {
    throw new Error(`${label} contains a workspace, file, git, or GitHub dependency.`);
  }
}

function assertNoLocalDependencyText(value: string, label: string): void {
  if (/(?:workspace:|file:|git(?:\+|:|$)|github:)/u.test(value)) {
    throw new Error(`${label} contains a workspace, file, git, or GitHub dependency.`);
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
