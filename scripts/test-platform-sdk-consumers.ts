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
const packageVersion = process.env.CLOUD_AGENTS_SDK_PACKAGE_VERSION ?? "0.0.0-a3.2";
const version = `v${packageVersion}`;
const modulePath = "github.com/hxp0618/cloud-agents/sdk/go";
const sdkPackage = "@cloud-agents/cloud-agent-platform-sdk";
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

type LiveConfig = Readonly<{
  endpoint: string;
  caFile?: string;
  token: string;
  tenantId: string;
  projectId: string;
  workspaceId: string;
  sandboxId: string;
  sandboxGeneration: number;
  environmentProfileId: string;
  environmentProfileVersion: number;
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
    const typescriptConsumer = runFreshTypeScriptConsumer(
      temporaryRoot,
      typescriptArtifact,
      npmRegistry,
      fixtureServer.baseUrl,
      fixtureServer.requestLogPath,
    );
    const goConsumer = runFreshGoConsumer(
      temporaryRoot,
      goProxy,
      fixtureServer.baseUrl,
      fixtureServer.requestLogPath,
    );
    const live = readLiveConfig();
    if (live !== undefined) {
      runLiveTypeScriptConsumer(
        typescriptConsumer,
        live,
      );
      runLiveGoConsumer(goConsumer, live, fixtureServer.baseUrl);
    }
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
  const output = join(root, "typescript-pack");
  mkdirSync(output, { recursive: true });
  const filename = `cloud-agents-cloud-agent-platform-sdk-${packageVersion}.tgz`;
  const path = join(output, filename);
  const configured = process.env.CLOUD_AGENTS_SDK_TYPESCRIPT_ARTIFACT;
  if (configured === undefined) {
    run(bun, ["run", "--cwd", "sdk/typescript", "build"], repositoryRoot);
    writeFileSync(path, buildPlatformTypeScriptSDKPackage(repositoryRoot, packageVersion));
  } else {
    cpSync(resolve(configured), path);
  }
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
    if (typeof filename !== "string" || filename !== expectedFilename)
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
): string {
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
  return consumer;
}

function buildGoModuleProxy(root: string): ModuleProxy {
  const staging = join(root, "go-stage");
  const proxy = join(root, "go-proxy");
  const moduleRoot = join(staging, `${modulePath}@${version}`);
  mkdirSync(staging, { recursive: true });
  mkdirSync(moduleRoot, { recursive: true });
  const configured = process.env.CLOUD_AGENTS_SDK_GO_ARTIFACT;
  if (configured === undefined) {
    cpSync(resolve(repositoryRoot, "sdk/go"), moduleRoot, { recursive: true });
  } else {
    run("tar", ["-xf", resolve(configured), "-C", moduleRoot], repositoryRoot);
  }
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

function readLiveConfig(): LiveConfig | undefined {
  const endpoint = process.env.CLOUD_AGENTS_SDK_LIVE_ENDPOINT;
  if (endpoint === undefined) return undefined;
  const tokenFile = process.env.CLOUD_AGENTS_SDK_LIVE_TOKEN_FILE;
  const tenantId = process.env.CLOUD_AGENTS_SDK_LIVE_TENANT;
  const projectId = process.env.CLOUD_AGENTS_SDK_LIVE_PROJECT;
  const workspaceId = process.env.CLOUD_AGENTS_SDK_LIVE_WORKSPACE;
  const sandboxId = process.env.CLOUD_AGENTS_SDK_LIVE_SANDBOX;
  const sandboxGeneration = Number(process.env.CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION);
  const environmentProfileId = process.env.CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE;
  const environmentProfileVersion = Number(
    process.env.CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION ?? "1",
  );
  if (
    tokenFile === undefined ||
    tenantId === undefined ||
    projectId === undefined ||
    workspaceId === undefined ||
    sandboxId === undefined ||
    environmentProfileId === undefined ||
    !Number.isSafeInteger(sandboxGeneration) ||
    sandboxGeneration < 1 ||
    !Number.isSafeInteger(environmentProfileVersion) ||
    environmentProfileVersion < 1
  )
    throw new Error("live SDK mode requires endpoint, token, tenant/project and Sandbox binding");
  const token = readFileSync(resolve(tokenFile), "utf8").trim();
  if (token === "" || token.includes("\n") || token.includes("\r"))
    throw new Error("live SDK token file is empty or malformed");
  const caFile = process.env.CLOUD_AGENTS_SDK_LIVE_CA_FILE;
  return Object.freeze({
    endpoint,
    ...(caFile === undefined ? {} : { caFile: resolve(caFile) }),
    token,
    tenantId,
    projectId,
    workspaceId,
    sandboxId,
    sandboxGeneration,
    environmentProfileId,
    environmentProfileVersion,
  });
}

function runLiveTypeScriptConsumer(consumer: string, live: LiveConfig): void {
  writeFileSync(
    join(consumer, "main-live.ts"),
    `import { createHTTPClient } from "${sdkPackage}/platform";

const endpoint = process.env.CLOUD_AGENTS_SDK_LIVE_ENDPOINT!;
const token = process.env.CLOUD_AGENTS_SDK_LIVE_TOKEN!;
const tenantId = process.env.CLOUD_AGENTS_SDK_LIVE_TENANT!;
const projectId = process.env.CLOUD_AGENTS_SDK_LIVE_PROJECT!;
const workspaceId = process.env.CLOUD_AGENTS_SDK_LIVE_WORKSPACE!;
const sandboxId = process.env.CLOUD_AGENTS_SDK_LIVE_SANDBOX!;
const sandboxGeneration = Number(process.env.CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION);
const environmentProfileId = process.env.CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE!;
const environmentProfileVersion = Number(process.env.CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION);
const client = createHTTPClient(endpoint, token);
const sleep = (milliseconds: number) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const ids = (name: string) => \`sdk-ts-\${name}-\${Date.now()}\`;

async function waitForMessage(sessionId: string, turnId: string, executionId: string, type: string) {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    let execution: Awaited<ReturnType<typeof client.getManagedAgentExecution>>;
    try {
      execution = await client.getManagedAgentExecution(tenantId, projectId, sessionId, turnId, executionId, \`\${executionId}-poll-\${attempt}\`);
    } catch (error) {
      if (typeof error !== "object" || error === null || (error as { status?: unknown }).status !== 404) throw error;
      await sleep(500);
      continue;
    }
    const message = (execution.value.messages ?? []).find((candidate) => {
      const payload = candidate.payload;
      return candidate.messageType === "InteractionRequest" && typeof payload === "object" && payload !== null && (payload as { interactionType?: unknown }).interactionType === type;
    });
    if (message !== undefined) return { execution: execution.value, message };
    if (execution.value.spec.state !== "queued" && execution.value.spec.state !== "running") throw new Error(\`execution ended before \${type}: \${execution.value.spec.state}\`);
    await sleep(500);
  }
  throw new Error(\`timed out waiting for \${type}\`);
}

async function waitForRunning(sessionId: string, turnId: string, executionId: string) {
  for (let attempt = 0; attempt < 120; attempt += 1) {
    let execution: Awaited<ReturnType<typeof client.getManagedAgentExecution>>;
    try {
      execution = await client.getManagedAgentExecution(tenantId, projectId, sessionId, turnId, executionId, \`\${executionId}-running-\${attempt}\`);
    } catch (error) {
      if (typeof error !== "object" || error === null || (error as { status?: unknown }).status !== 404) throw error;
      await sleep(500);
      continue;
    }
    if (execution.value.spec.state === "running") return execution.value.spec.generation;
    if (execution.value.spec.state !== "queued") throw new Error(\`execution ended before cancel: \${execution.value.spec.state}\`);
    await sleep(500);
  }
  throw new Error("timed out waiting for a running execution");
}

async function main() {
  const sessionId = ids("session");
  const turnId = ids("turn");
  const executionId = ids("execution");
  const artifactSessionId = ids("artifact-session");
  const artifactTurnId = ids("artifact-turn");
  const artifactExecutionId = ids("artifact-execution");
  const cancelSessionId = ids("cancel-session");
  const cancelTurnId = ids("cancel-turn");
  const cancelExecutionId = ids("cancel-execution");
  try {
    await client.createManagedAgentSession(tenantId, projectId, \`\${sessionId}-create\`, \`\${sessionId}-idempotency\`, { sessionId, providerKind: "codex", workspaceId, sandboxId, sandboxGeneration, environmentProfileId, environmentProfileVersion });
    const prompt = "Reply with exactly SDK TypeScript live ok.";
    await client.createManagedAgentTurn(tenantId, projectId, sessionId, \`\${turnId}-create\`, \`\${turnId}-idempotency\`, { turnId, inputText: prompt });
    const replayedTurn = await client.createManagedAgentTurn(tenantId, projectId, sessionId, \`\${turnId}-replay\`, \`\${turnId}-idempotency\`, { turnId, inputText: prompt });
    if (replayedTurn.value.metadata.uid !== turnId) throw new Error("TypeScript SDK idempotent turn retry changed the Turn");
    const execution = await client.executeManagedAgent(tenantId, projectId, sessionId, \`\${executionId}-run\`, \`\${executionId}-idempotency\`, { turnId, executionId, runtimeMode: "full-access", interactionMode: "default", inputText: prompt });
    if (execution.value.spec.state !== "succeeded" || !(execution.value.messages ?? []).some((message) => message.messageType === "Result")) throw new Error(\`TypeScript SDK execution did not succeed: \${execution.value.spec.state}\`);
    const firstEvents = await client.listManagedAgentEvents(tenantId, projectId, sessionId, \`\${sessionId}-events-1\`, undefined, 1);
    if (firstEvents.value.events.length !== 1) throw new Error("TypeScript SDK event page did not return one persisted event");
    const resumedEvents = await client.listManagedAgentEvents(tenantId, projectId, sessionId, \`\${sessionId}-events-2\`, firstEvents.value.nextCursor, 64);
    const firstUIDs = new Set(firstEvents.value.events.map((event) => event.metadata.uid));
    if (resumedEvents.value.events.some((event) => firstUIDs.has(event.metadata.uid))) throw new Error("TypeScript SDK event resume duplicated an event");

    const artifactPath = \`.cloud-agents-acceptance/\${artifactExecutionId}.txt\`;
    await client.createManagedAgentSession(tenantId, projectId, \`\${artifactSessionId}-create\`, \`\${artifactSessionId}-idempotency\`, { sessionId: artifactSessionId, providerKind: "claudeAgent", workspaceId, sandboxId, sandboxGeneration, environmentProfileId, environmentProfileVersion });
    const artifactPrompt = \`Use the Write tool to create exactly one file at \${artifactPath} containing the single line 'sdk typescript artifact' followed by a newline. Wait for approval when requested, then reply done.\`;
    await client.createManagedAgentTurn(tenantId, projectId, artifactSessionId, \`\${artifactTurnId}-create\`, \`\${artifactTurnId}-idempotency\`, { turnId: artifactTurnId, inputText: artifactPrompt });
    const artifactExecutionPromise = client.executeManagedAgent(tenantId, projectId, artifactSessionId, \`\${artifactExecutionId}-run\`, \`\${artifactExecutionId}-idempotency\`, { turnId: artifactTurnId, executionId: artifactExecutionId, runtimeMode: "approval-required", interactionMode: "default", inputText: artifactPrompt }).then((value) => ({ value }), (error: unknown) => ({ error }));
    const interaction = await waitForMessage(artifactSessionId, artifactTurnId, artifactExecutionId, "approval");
    const payload = interaction.message.payload as { requestId?: unknown };
    if (typeof payload.requestId !== "string") throw new Error("TypeScript SDK approval request is missing requestId");
    await client.resolveManagedAgentApproval(tenantId, projectId, artifactSessionId, artifactTurnId, artifactExecutionId, \`\${artifactExecutionId}-resolve\`, { generation: interaction.execution.spec.generation, requestId: payload.requestId, decision: "accept" });
    const artifactOutcome = await artifactExecutionPromise;
    if ("error" in artifactOutcome) throw artifactOutcome.error;
    const artifactExecution = artifactOutcome.value;
    const artifactIndex = (artifactExecution.value.messages ?? []).findIndex((message) => {
      const candidate = message.payload;
      const artifact = typeof candidate === "object" && candidate !== null ? (candidate as { artifact?: Record<string, unknown> }).artifact : undefined;
      return message.messageType === "ArtifactCandidate" && artifact?.sourceRoot === "workspace" && artifact.path === artifactPath;
    });
    if (artifactIndex < 0) throw new Error("TypeScript SDK approval execution did not produce an ArtifactCandidate");
    const artifact = await client.downloadManagedAgentArtifact(tenantId, projectId, artifactSessionId, artifactTurnId, artifactExecutionId, \`\${artifactExecutionId}-artifact\`, artifactIndex);
    if (new TextDecoder().decode(artifact.data) !== "sdk typescript artifact\\n") throw new Error("TypeScript SDK artifact bytes changed");

    await client.createManagedAgentSession(tenantId, projectId, \`\${cancelSessionId}-create\`, \`\${cancelSessionId}-idempotency\`, { sessionId: cancelSessionId, providerKind: "codex", workspaceId, sandboxId, sandboxGeneration, environmentProfileId, environmentProfileVersion });
    const cancelPrompt = "Use the shell tool now to run exactly: sleep 120. Do not finish until it completes.";
    await client.createManagedAgentTurn(tenantId, projectId, cancelSessionId, \`\${cancelTurnId}-create\`, \`\${cancelTurnId}-idempotency\`, { turnId: cancelTurnId, inputText: cancelPrompt });
    const cancelPromise = client.executeManagedAgent(tenantId, projectId, cancelSessionId, \`\${cancelExecutionId}-run\`, \`\${cancelExecutionId}-idempotency\`, { turnId: cancelTurnId, executionId: cancelExecutionId, runtimeMode: "full-access", interactionMode: "default", inputText: cancelPrompt }).then((value) => ({ value }), (error: unknown) => ({ error }));
    const generation = await waitForRunning(cancelSessionId, cancelTurnId, cancelExecutionId);
    const cancelled = await client.cancelManagedAgentExecution(tenantId, projectId, cancelSessionId, cancelTurnId, cancelExecutionId, \`\${cancelExecutionId}-cancel\`, \`\${cancelExecutionId}-cancel-idempotency\`, { generation });
    const cancelOutcome = await cancelPromise;
    const executeCancelled = "error" in cancelOutcome
      ? typeof cancelOutcome.error === "object" && cancelOutcome.error !== null
        && (cancelOutcome.error as { status?: unknown }).status === 499
        && (cancelOutcome.error as { problem?: { error?: { code?: unknown } } }).problem?.error?.code === "CANCELLED"
      : cancelOutcome.value.value.spec.state === "cancelled";
    if (cancelled.value.spec.state !== "cancelled" || cancelled.value.spec.errorCode !== "cancelled" || !executeCancelled) throw new Error("TypeScript SDK cancellation did not produce a cancelled execution");
    console.log("live-typescript-consumer-ok session-turn-events-resume-approval-artifact-cancel");
  } finally {
    for (const [activeSessionId, name] of [[sessionId, "session"], [artifactSessionId, "artifact"], [cancelSessionId, "cancel"]] as const) {
      await client.closeManagedAgentSession(tenantId, projectId, activeSessionId, \`\${activeSessionId}-close-\${name}\`, \`\${activeSessionId}-close-idempotency\`).catch(() => undefined);
    }
  }
}

await main();
`,
  );
  const tsconfig = join(consumer, "tsconfig.live.json");
  writeFileSync(
    tsconfig,
    `${JSON.stringify({ compilerOptions: { strict: true, target: "ES2022", module: "NodeNext", moduleResolution: "NodeNext", outDir: "live-dist", rootDir: ".", skipLibCheck: false }, include: ["main-live.ts"] }, null, 2)}\n`,
  );
  run(join(consumer, "node_modules/.bin/tsc"), ["--project", tsconfig], consumer);
  process.stdout.write(run("node", [join(consumer, "live-dist/main-live.js")], consumer, undefined, {
    CLOUD_AGENTS_SDK_LIVE_ENDPOINT: live.endpoint,
    CLOUD_AGENTS_SDK_LIVE_TOKEN: live.token,
    CLOUD_AGENTS_SDK_LIVE_TENANT: live.tenantId,
    CLOUD_AGENTS_SDK_LIVE_PROJECT: live.projectId,
    CLOUD_AGENTS_SDK_LIVE_WORKSPACE: live.workspaceId,
    CLOUD_AGENTS_SDK_LIVE_SANDBOX: live.sandboxId,
    CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION: String(live.sandboxGeneration),
    CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE: live.environmentProfileId,
    CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION: String(live.environmentProfileVersion),
    ...(live.caFile === undefined ? {} : { NODE_EXTRA_CA_CERTS: live.caFile }),
  }));
}

function runLiveGoConsumer(consumer: string, live: LiveConfig, artifactBaseUrl: string): void {
  writeFileSync(
    join(consumer, "main.go"),
    `package main

import (
  "context"
  "crypto/tls"
  "crypto/x509"
  "fmt"
  "net/http"
  "os"
  "strconv"
  "time"

  platformv1alpha1 "${modulePath}/gen/openapi/v1alpha1"
)

func main() {
  ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
  defer cancel()
  endpoint := os.Getenv("CLOUD_AGENTS_SDK_LIVE_ENDPOINT")
  token := os.Getenv("CLOUD_AGENTS_SDK_LIVE_TOKEN")
  tenantID := os.Getenv("CLOUD_AGENTS_SDK_LIVE_TENANT")
  projectID := os.Getenv("CLOUD_AGENTS_SDK_LIVE_PROJECT")
  workspaceID := os.Getenv("CLOUD_AGENTS_SDK_LIVE_WORKSPACE")
  sandboxID := os.Getenv("CLOUD_AGENTS_SDK_LIVE_SANDBOX")
  sandboxGeneration, err := strconv.ParseInt(os.Getenv("CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION"), 10, 64); if err != nil { panic(err) }
  environmentProfileVersion, err := strconv.ParseInt(os.Getenv("CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION"), 10, 64); if err != nil { panic(err) }
  var httpClient *http.Client
  if caFile := os.Getenv("CLOUD_AGENTS_SDK_LIVE_CA_FILE"); caFile != "" {
    caBytes, err := os.ReadFile(caFile); if err != nil { panic(err) }
    roots := x509.NewCertPool(); if !roots.AppendCertsFromPEM(caBytes) { panic("invalid SDK CA") }
    httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
  } else { httpClient = http.DefaultClient }
  client, err := platformv1alpha1.NewHTTPClientWithClient(endpoint, token, httpClient); if err != nil { panic(err) }
  suffix := fmt.Sprintf("%d", time.Now().UnixNano())
  sessionID, turnID, executionID := "sdk-go-session-"+suffix, "sdk-go-turn-"+suffix, "sdk-go-execution-"+suffix
  defer func() { _, _ = client.CloseManagedAgentSession(context.Background(), tenantID, projectID, sessionID, sessionID+"-close", sessionID+"-close-idempotency") }()
  _, err = client.CreateManagedAgentSession(ctx, tenantID, projectID, sessionID+"-create", sessionID+"-idempotency", platformv1alpha1.ManagedAgentSessionCreateRequest{SessionID: sessionID, ProviderKind: "claudeAgent", WorkspaceID: workspaceID, SandboxID: sandboxID, SandboxGeneration: sandboxGeneration, EnvironmentProfileID: os.Getenv("CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE"), EnvironmentProfileVersion: environmentProfileVersion}); if err != nil { panic(err) }
  prompt := "Reply with exactly SDK Go live ok."
  _, err = client.CreateManagedAgentTurn(ctx, tenantID, projectID, sessionID, turnID+"-create", turnID+"-idempotency", platformv1alpha1.ManagedAgentTurnCreateRequest{TurnID: turnID, InputText: prompt}); if err != nil { panic(err) }
  replay, err := client.CreateManagedAgentTurn(ctx, tenantID, projectID, sessionID, turnID+"-replay", turnID+"-idempotency", platformv1alpha1.ManagedAgentTurnCreateRequest{TurnID: turnID, InputText: prompt}); if err != nil || replay.Value.Metadata.UID != turnID { panic("Go SDK idempotent Turn retry changed the Turn") }
  executionCh := make(chan struct { state string; err error }, 1)
  go func() { value, err := client.ExecuteManagedAgent(ctx, tenantID, projectID, sessionID, executionID+"-run", executionID+"-idempotency", platformv1alpha1.ManagedAgentExecutionCreateRequest{TurnID: turnID, ExecutionID: executionID, RuntimeMode: "full-access", InteractionMode: "default", InputText: prompt}); executionCh <- struct { state string; err error }{value.Value.Spec.State, err} }()
  var generation uint64
  for attempt := 0; attempt < 120; attempt++ { current, err := client.GetManagedAgentExecution(ctx, tenantID, projectID, sessionID, turnID, executionID, executionID+"-poll"); if err == nil && current.Value.Spec.State == "running" { generation = current.Value.Spec.Generation; break }; if err == nil && current.Value.Spec.State != "queued" { panic("Go SDK execution ended before cancel: "+current.Value.Spec.State) }; time.Sleep(500*time.Millisecond) }
  if generation == 0 { panic("Go SDK execution did not become running") }
  cancelled, err := client.CancelManagedAgentExecution(ctx, tenantID, projectID, sessionID, turnID, executionID, executionID+"-cancel", executionID+"-cancel-idempotency", platformv1alpha1.ManagedAgentExecutionCancelRequest{Generation: generation}); if err != nil { panic(err) }
  completed := <-executionCh
  executeCancelled := completed.state == "cancelled"
  if completed.err != nil {
    clientErr, ok := completed.err.(*platformv1alpha1.ClientError)
    if !ok || clientErr.Status != 499 || clientErr.Problem == nil || clientErr.Problem.Error.Code != "CANCELLED" { panic(completed.err) }
    executeCancelled = true
  }
  if cancelled.Value.Spec.State != "cancelled" || cancelled.Value.Spec.ErrorCode != "cancelled" || !executeCancelled { panic("Go SDK cancellation did not produce a cancelled execution") }
  events, err := client.ListManagedAgentEvents(ctx, tenantID, projectID, sessionID, sessionID+"-events", "", 1); if err != nil || len(events.Value.Events) != 1 { panic("Go SDK did not list one persisted event") }
  resumed, err := client.ListManagedAgentEvents(ctx, tenantID, projectID, sessionID, sessionID+"-events-resume", events.Value.NextCursor, 64); if err != nil { panic(err) }; if len(resumed.Value.Events) > 0 && resumed.Value.Events[0].Metadata.UID == events.Value.Events[0].Metadata.UID { panic("Go SDK event resume duplicated an event") }
  fmt.Println("live-go-consumer-ok session-turn-events-resume-cancel")
}
`,
  );
  process.stdout.write(run(go, ["run", "."], consumer, undefined, {
    CLOUD_AGENTS_SDK_LIVE_ENDPOINT: live.endpoint,
    CLOUD_AGENTS_SDK_LIVE_TOKEN: live.token,
    CLOUD_AGENTS_SDK_LIVE_TENANT: live.tenantId,
    CLOUD_AGENTS_SDK_LIVE_PROJECT: live.projectId,
    CLOUD_AGENTS_SDK_LIVE_WORKSPACE: live.workspaceId,
    CLOUD_AGENTS_SDK_LIVE_SANDBOX: live.sandboxId,
    CLOUD_AGENTS_SDK_LIVE_SANDBOX_GENERATION: String(live.sandboxGeneration),
    CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE: live.environmentProfileId,
    CLOUD_AGENTS_SDK_LIVE_ENVIRONMENT_PROFILE_VERSION: String(live.environmentProfileVersion),
    CLOUD_AGENTS_SDK_LIVE_CA_FILE: live.caFile ?? "",
    GOPROXY: `${artifactBaseUrl}/go-proxy,https://proxy.golang.org`,
    GOSUMDB: "sum.golang.org",
    GONOSUMDB: modulePath,
    GOMODCACHE: join(resolve(consumer, ".."), "go-mod-cache"),
    GOWORK: "off",
    GOTOOLCHAIN: "local",
    GOFLAGS: "-mod=readonly",
  }));
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
): string {
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
  return consumer;
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
