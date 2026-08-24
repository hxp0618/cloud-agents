import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const repositoryRoot = resolve(import.meta.dirname, "..");
const toolchainRoot = process.env.CLOUD_AGENTS_A24_TOOLCHAIN;
const bun = toolchainRoot ? join(toolchainRoot, "bun") : "bun";
const go = toolchainRoot ? join(toolchainRoot, "go") : "go";
const version = "v0.0.0-a3.2";
const modulePath = "github.com/hxp0618/cloud-agents/sdk/go";
const sdkPackage = "@synara/cloud-agent-platform-sdk";

const temporaryRoot = mkdtempSync(join(tmpdir(), "cloud-agents-a24-sdk-consumer-"));
try {
  const typescriptTarball = packTypeScriptSDK(temporaryRoot);
  runFreshTypeScriptConsumer(temporaryRoot, typescriptTarball);
  const goProxy = buildGoModuleProxy(temporaryRoot);
  runFreshGoConsumer(temporaryRoot, goProxy);
  process.stdout.write("platform-sdk-consumers: fresh TypeScript and Go consumers passed\n");
} finally {
  rmSync(temporaryRoot, { recursive: true, force: true });
}

function packTypeScriptSDK(root: string): string {
  run(bun, ["run", "--cwd", "sdk/typescript", "build"], repositoryRoot);
  const output = join(root, "typescript-pack");
  mkdirSync(output, { recursive: true });
  const packed = run(
    "npm",
    ["pack", "--json", "--pack-destination", output, resolve(repositoryRoot, "sdk/typescript")],
    repositoryRoot,
  );
  const records: unknown = JSON.parse(packed);
  if (!Array.isArray(records) || records.length !== 1 || !isRecord(records[0])) {
    throw new Error("TypeScript SDK pack did not return exactly one artifact.");
  }
  const filename = records[0].filename;
  if (typeof filename !== "string" || filename.length === 0) {
    throw new Error("TypeScript SDK pack omitted its filename.");
  }
  const tarball = join(output, filename);
  if (!existsSync(tarball)) throw new Error(`TypeScript SDK tarball is missing: ${tarball}`);
  return tarball;
}

function runFreshTypeScriptConsumer(root: string, tarball: string): void {
  const consumer = join(root, "typescript-consumer");
  mkdirSync(consumer, { recursive: true });
  writeFileSync(
    join(consumer, "package.json"),
    `${JSON.stringify(
      {
        name: "fresh-cloud-agents-sdk-consumer",
        private: true,
        type: "module",
        scripts: { check: "tsc --noEmit" },
        dependencies: {
          "@connectrpc/connect": "2.1.2",
          [sdkPackage]: `file:${tarball}`,
        },
        devDependencies: { typescript: "5.7.3" },
      },
      null,
      2,
    )}\n`,
  );
  writeFileSync(
    join(consumer, "main.ts"),
    `import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { WorkerExecutionService } from "${sdkPackage}/proto";

const controller = new AbortController();
controller.abort();
let transportCalls = 0;
const transport = {
  unary() {
    transportCalls += 1;
    return Promise.reject(new ConnectError("fixture cancellation", Code.Canceled));
  },
  stream() {
    throw new Error("fresh consumer attempted a streaming side effect");
  },
};
const client = createClient(WorkerExecutionService, transport);
try {
  await client.negotiate({}, { signal: controller.signal });
  throw new Error("aborted generated call unexpectedly succeeded");
} catch (error) {
  if (!(error instanceof ConnectError) || error.code !== Code.Canceled) throw error;
}
if (transportCalls !== 1) throw new Error("fresh consumer did not exercise the generated transport exactly once");
console.log("fresh-typescript-consumer-ok");
`,
  );
  run(bun, ["install", "--no-progress"], consumer);
  const installedManifest = join(consumer, "node_modules", sdkPackage, "package.json");
  const manifest = JSON.parse(readFileSync(installedManifest, "utf8")) as Record<string, unknown>;
  if (manifest.name !== sdkPackage || manifest.version !== "0.0.0-a3.2") {
    throw new Error("Fresh TypeScript consumer did not install the exact SDK package.");
  }
  for (const section of ["dependencies", "devDependencies", "peerDependencies"] as const) {
    const values = manifest[section];
    if (!values || typeof values !== "object") continue;
    for (const [name, specifier] of Object.entries(values)) {
      if (typeof specifier === "string" && /^(?:workspace:|file:|git(?:\+|$))/u.test(specifier)) {
        throw new Error(`Packed TypeScript SDK contains a local dependency: ${name}=${specifier}`);
      }
    }
  }
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
  run(bun, ["run", "check"], consumer);
  run(bun, ["run", "main.ts"], consumer);
}

function buildGoModuleProxy(root: string): string {
  const staging = join(root, "go-stage");
  const proxy = join(root, "go-proxy");
  const moduleRoot = join(staging, `${modulePath}@${version}`);
  mkdirSync(staging, { recursive: true });
  cpSync(resolve(repositoryRoot, "sdk/go"), moduleRoot, { recursive: true });
  const moduleVersionRoot = join(proxy, modulePath, "@v");
  mkdirSync(moduleVersionRoot, { recursive: true });
  const zip = join(moduleVersionRoot, `${version}.zip`);
  run("zip", ["-q", "-r", zip, `${modulePath}@${version}`], staging);
  writeFileSync(
    join(moduleVersionRoot, `${version}.mod`),
    readFileSync(join(moduleRoot, "go.mod")),
  );
  writeFileSync(
    join(moduleVersionRoot, `${version}.info`),
    `${JSON.stringify({ Version: version, Time: "2026-08-21T00:00:00Z" })}\n`,
  );
  return proxy;
}

function runFreshGoConsumer(root: string, proxy: string): void {
  const consumer = join(root, "go-consumer");
  mkdirSync(consumer, { recursive: true });
  run(go, ["mod", "init", "example.com/fresh-cloud-agents-sdk-consumer"], consumer);
  run(go, ["mod", "edit", `-require=${modulePath}@${version}`], consumer);
  writeFileSync(
    join(consumer, "main.go"),
    `package main

import (
  "context"
  "fmt"
  "net/http"

  connect "connectrpc.com/connect"
  workerv1alpha1 "${modulePath}/gen/cloudagents/worker/v1alpha1"
  workerv1alpha1connect "${modulePath}/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect"
)

func main() {
  ctx, cancel := context.WithCancel(context.Background())
  cancel()
  client := workerv1alpha1connect.NewWorkerExecutionServiceClient(http.DefaultClient, "https://fixture.invalid")
  _, err := client.Negotiate(ctx, connect.NewRequest(&workerv1alpha1.NegotiationRequest{}))
  if connect.CodeOf(err) != connect.CodeCanceled {
    panic(fmt.Sprintf("generated Go consumer cancellation code = %v, err = %v", connect.CodeOf(err), err))
  }
  fmt.Println("fresh-go-consumer-ok")
}
`,
  );
  const env = {
    GOPROXY: `file://${proxy},https://proxy.golang.org`,
    GOSUMDB: "off",
    GOWORK: "off",
    GOTOOLCHAIN: "local",
    GOFLAGS: "-mod=mod",
  };
  run(go, ["run", "."], consumer, undefined, env);
  const consumerModule = readFileSync(join(consumer, "go.mod"), "utf8");
  if (/^replace\s/mu.test(consumerModule) || /(?:file:|git\+)/u.test(consumerModule)) {
    throw new Error("Fresh Go consumer contains a replace, file, or git dependency.");
  }
  if (!consumerModule.includes(`${modulePath} ${version}`)) {
    throw new Error("Fresh Go consumer did not resolve the exact packed SDK module version.");
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
