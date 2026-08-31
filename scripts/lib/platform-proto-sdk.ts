import { createHash } from "node:crypto";
import {
  chmodSync,
  existsSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, relative, resolve, sep } from "node:path";
import { spawnSync } from "node:child_process";

import { fromBinary } from "@bufbuild/protobuf";
import { FileDescriptorSetSchema } from "@bufbuild/protobuf/wkt";

import {
  formatWithOxfmt,
  PLATFORM_OXFMT_LIBRARY_PATH,
  PLATFORM_OXFMT_TEST_PATH,
} from "./platform-oxfmt";

export const PLATFORM_PROTO_PROFILE_PATH = "contracts/proto-generation.profile.json";
const ENTRY_PATH = "docs/plan/p1/sdk-identity-closure-entry-20260820.md";
export const PLATFORM_PROTO_GENERATOR_PATH = "scripts/generate-platform-proto-sdks.ts";
export const PLATFORM_PROTO_LIBRARY_PATH = "scripts/lib/platform-proto-sdk.ts";
export const PLATFORM_PROTO_TEST_PATH = "scripts/lib/platform-proto-sdk.test.ts";
export const PLATFORM_PROTO_DESCRIPTOR_MANIFEST_PATH = "contracts/generated/proto/manifest.json";
export const PLATFORM_PROTO_GO_MANIFEST_PATH = "sdk/go/proto-generated-manifest.json";
export const PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH =
  "sdk/typescript/proto-generated-manifest.json";
export const PLATFORM_PROTO_TYPESCRIPT_INDEX_PATH = "sdk/typescript/src/proto.ts";
export const PLATFORM_PROTO_DESCRIPTOR_PATH =
  "contracts/generated/proto/cloud-agents-v1alpha1.binpb";
export const PLATFORM_PROTO_BREAKING_BASELINE_PATH =
  "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb";
const WORKER_RUNTIME_PROTO_SOURCE = "contracts/worker/runtime/v1alpha1/runtime.proto";
const WORKER_RUNTIME_PROTO_DESCRIPTOR_PATH =
  "contracts/generated/proto/cloud-agents-worker-runtime-v1alpha1.binpb";

export const PLATFORM_PROTO_GO_OUTPUTS = [
  "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platform_adapter.pb.go",
  "sdk/go/gen/cloudagents/platformadapter/v1alpha1/platformadapterv1alpha1connect/platform_adapter.connect.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/worker_supervisor.pb.go",
  "sdk/go/gen/cloudagents/worker/v1alpha1/workerv1alpha1connect/worker_supervisor.connect.go",
] as const;

const WORKER_RUNTIME_PROTO_GO_OUTPUTS = [
  "sdk/go/gen/cloudagents/worker/runtime/v1alpha1/runtime.pb.go",
  "sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect/runtime.connect.go",
] as const;

export const PLATFORM_PROTO_TYPESCRIPT_OUTPUTS = [
  "sdk/typescript/src/gen/contracts/platform-adapter/v1alpha1/platform_adapter_pb.ts",
  "sdk/typescript/src/gen/contracts/worker/v1alpha1/kernel_pb.ts",
  "sdk/typescript/src/gen/contracts/worker/v1alpha1/worker_supervisor_pb.ts",
] as const;

type ToolArtifact = { readonly url: string; readonly sha256: string };
type PluginProfile = {
  readonly id: string;
  readonly version: string;
  readonly moduleSum?: string;
  readonly integrity?: string;
  readonly license: string;
  readonly options: ReadonlyArray<string>;
};
type ProtoGenerationProfile = {
  readonly formatVersion: string;
  readonly status: string;
  readonly authority: string;
  readonly sources: ReadonlyArray<string>;
  readonly compiler: {
    readonly id: string;
    readonly version: string;
    readonly license: string;
    readonly artifacts: Readonly<Record<string, ToolArtifact>>;
  };
  readonly plugins: ReadonlyArray<PluginProfile>;
  readonly descriptor: {
    readonly includeImports: boolean;
    readonly includeSourceInfo: boolean;
    readonly output: string;
    readonly breakingBaseline: string;
  };
  readonly outputs: { readonly goRoot: string; readonly typescriptRoot: string };
  readonly transportProfile: Readonly<Record<string, unknown>>;
  readonly implementationBoundary: Readonly<Record<string, unknown>>;
};
type GeneratedFile = { readonly path: string; readonly bytes: Buffer };
type Toolchain = {
  readonly protoc: string;
  readonly protocGenGo: string;
  readonly protocGenConnectGo: string;
  readonly protocGenES: string;
};

export function platformProtoGeneratorSources(): string[] {
  return [
    PLATFORM_PROTO_GENERATOR_PATH,
    PLATFORM_PROTO_LIBRARY_PATH,
    PLATFORM_PROTO_TEST_PATH,
    PLATFORM_OXFMT_LIBRARY_PATH,
    PLATFORM_OXFMT_TEST_PATH,
  ].toSorted();
}

export function platformProtoContractInputs(root: string): string[] {
  const profile = readProfile(root);
  return [
    ENTRY_PATH,
    PLATFORM_PROTO_PROFILE_PATH,
    "contracts/platform-adapter/v1alpha1/README.md",
    "contracts/platform-adapter/v1alpha1/fixtures/descriptor.golden.json",
    "contracts/worker/v1alpha1/README.md",
    "contracts/worker/v1alpha1/fixtures/descriptor.golden.json",
    ...profile.sources,
  ].toSorted();
}

export function writePlatformProtoSDKFiles(root: string): void {
  const profile = readProfile(root);
  const generated = generatePlatformProtoArtifacts(root, profile);
  const descriptor = requiredGenerated(generated, PLATFORM_PROTO_DESCRIPTOR_PATH).bytes;
  const baseline = existsSync(resolve(root, PLATFORM_PROTO_BREAKING_BASELINE_PATH))
    ? readRegularFile(root, PLATFORM_PROTO_BREAKING_BASELINE_PATH)
    : descriptor;
  assertExactBreakingBaseline(descriptor, baseline);

  for (const output of generated) writeGenerated(root, output);
  if (!existsSync(resolve(root, PLATFORM_PROTO_BREAKING_BASELINE_PATH))) {
    writeGenerated(root, { path: PLATFORM_PROTO_BREAKING_BASELINE_PATH, bytes: baseline });
  }
}

export function assertPlatformProtoSDKCurrent(root: string): void {
  const profile = readProfile(root);
  const generated = generatePlatformProtoArtifacts(root, profile);
  const baseline = readRegularFile(root, PLATFORM_PROTO_BREAKING_BASELINE_PATH);
  assertExactBreakingBaseline(
    requiredGenerated(generated, PLATFORM_PROTO_DESCRIPTOR_PATH).bytes,
    baseline,
  );
  for (const output of generated) assertCurrent(root, output);
}

function generatePlatformProtoArtifacts(
  root: string,
  profile: ProtoGenerationProfile,
): ReadonlyArray<GeneratedFile> {
  const tools = ensureToolchain(root, profile);
  const temporary = mkdtempSync(join(tmpdir(), "cloud-agents-proto-generate-"));
  try {
    const goRoot = resolve(temporary, "go");
    const typescriptRoot = resolve(temporary, "typescript");
    const descriptor = resolve(temporary, "cloud-agents-v1alpha1.binpb");
    const workerRuntimeDescriptor = resolve(
      temporary,
      "cloud-agents-worker-runtime-v1alpha1.binpb",
    );
    mkdirSync(goRoot, { recursive: true });
    mkdirSync(typescriptRoot, { recursive: true });
    const goPlugin = requiredPlugin(profile, "google.golang.org/protobuf/cmd/protoc-gen-go");
    const connectPlugin = requiredPlugin(
      profile,
      "connectrpc.com/connect/cmd/protoc-gen-connect-go",
    );
    const esPlugin = requiredPlugin(profile, "@bufbuild/protoc-gen-es");
    const arguments_ = [
      `-I${root}`,
      `-I${resolve(dirname(dirname(tools.protoc)), "include")}`,
      `--descriptor_set_out=${descriptor}`,
      "--include_imports",
      `--plugin=protoc-gen-go=${tools.protocGenGo}`,
      `--plugin=protoc-gen-connect-go=${tools.protocGenConnectGo}`,
      `--plugin=protoc-gen-es=${tools.protocGenES}`,
      `--go_out=${goRoot}`,
      `--go_opt=${goPlugin.options.join(",")}`,
      `--connect-go_out=${goRoot}`,
      `--connect-go_opt=${connectPlugin.options.join(",")}`,
      `--es_out=${typescriptRoot}`,
      `--es_opt=${esPlugin.options.join(",")}`,
      ...profile.sources,
    ];
    run(tools.protoc, arguments_, root, "protoc generation");
    run(
      tools.protoc,
      [
        `-I${root}`,
        `-I${resolve(dirname(dirname(tools.protoc)), "include")}`,
        `--descriptor_set_out=${workerRuntimeDescriptor}`,
        "--include_imports",
        `--plugin=protoc-gen-go=${tools.protocGenGo}`,
        `--plugin=protoc-gen-connect-go=${tools.protocGenConnectGo}`,
        `--go_out=${goRoot}`,
        `--go_opt=${goPlugin.options.join(",")}`,
        `--connect-go_out=${goRoot}`,
        `--connect-go_opt=${connectPlugin.options.join(",")}`,
        WORKER_RUNTIME_PROTO_SOURCE,
      ],
      root,
      "Worker Runtime protoc generation",
    );
    const descriptorBytes = readFileSync(descriptor);
    const workerRuntimeDescriptorBytes = readFileSync(workerRuntimeDescriptor);
    validateDescriptorSet(descriptorBytes, profile);
    validateWorkerRuntimeDescriptorSet(workerRuntimeDescriptorBytes);

    const outputs: GeneratedFile[] = [
      { path: PLATFORM_PROTO_DESCRIPTOR_PATH, bytes: descriptorBytes },
      { path: WORKER_RUNTIME_PROTO_DESCRIPTOR_PATH, bytes: workerRuntimeDescriptorBytes },
      ...PLATFORM_PROTO_GO_OUTPUTS.map((path) => ({
        path,
        bytes: readFileSync(resolve(goRoot, path.slice("sdk/go/".length))),
      })),
      ...WORKER_RUNTIME_PROTO_GO_OUTPUTS.map((path) => ({
        path,
        bytes: readFileSync(resolve(goRoot, path.slice("sdk/go/".length))),
      })),
      ...PLATFORM_PROTO_TYPESCRIPT_OUTPUTS.map((path) => ({
        path,
        bytes: Buffer.from(
          formatGeneratedText(
            root,
            path,
            readFileSync(
              resolve(typescriptRoot, path.slice("sdk/typescript/src/gen/".length)),
              "utf8",
            ),
          ),
        ),
      })),
      {
        path: PLATFORM_PROTO_TYPESCRIPT_INDEX_PATH,
        bytes: Buffer.from(
          formatGeneratedText(root, PLATFORM_PROTO_TYPESCRIPT_INDEX_PATH, typescriptIndexSource()),
        ),
      },
    ];
    assertGeneratedTree(
      goRoot,
      [...PLATFORM_PROTO_GO_OUTPUTS, ...WORKER_RUNTIME_PROTO_GO_OUTPUTS].map((path) =>
        path.slice("sdk/go/".length),
      ),
    );
    assertGeneratedTree(
      typescriptRoot,
      PLATFORM_PROTO_TYPESCRIPT_OUTPUTS.map((path) => path.slice("sdk/typescript/src/gen/".length)),
    );
    return outputs;
  } finally {
    rmSync(temporary, { force: true, recursive: true });
  }
}

function ensureToolchain(root: string, profile: ProtoGenerationProfile): Toolchain {
  const platform = platformIdentity();
  const artifact = profile.compiler.artifacts[platform];
  if (!artifact) throw new Error(`No protoc artifact is pinned for ${platform}.`);
  const cacheRoot =
    process.env.CLOUD_AGENTS_PROTO_TOOL_CACHE ??
    resolve(tmpdir(), "cloud-agents-proto-toolchain-v1", platform);
  mkdirSync(cacheRoot, { recursive: true });
  const protoc = process.env.CLOUD_AGENTS_PROTOC ?? ensureProtoc(cacheRoot, profile, artifact);
  assertVersion(protoc, ["--version"], `libprotoc ${profile.compiler.version}`, "protoc");

  const go = process.env.CLOUD_AGENTS_GO ?? "go";
  assertVersion(go, ["version"], "go version go1.26.6 ", "Go toolchain", true);
  const goPlugin = requiredPlugin(profile, "google.golang.org/protobuf/cmd/protoc-gen-go");
  const connectPlugin = requiredPlugin(profile, "connectrpc.com/connect/cmd/protoc-gen-connect-go");
  const pluginRoot = resolve(cacheRoot, "go-plugins");
  const protocGenGo =
    process.env.CLOUD_AGENTS_PROTOC_GEN_GO ?? ensureGoPlugin(go, pluginRoot, goPlugin);
  const protocGenConnectGo =
    process.env.CLOUD_AGENTS_PROTOC_GEN_CONNECT_GO ?? ensureGoPlugin(go, pluginRoot, connectPlugin);
  assertVersion(protocGenGo, ["--version"], `protoc-gen-go ${goPlugin.version}`, "protoc-gen-go");
  assertVersion(
    protocGenConnectGo,
    ["--version"],
    connectPlugin.version.slice(1),
    "protoc-gen-connect-go",
  );

  const node = process.env.CLOUD_AGENTS_NODE ?? "node";
  assertVersion(node, ["--version"], "v24.18.1", "Node.js");
  const protocGenES = resolve(root, "node_modules/.bin/protoc-gen-es");
  assertVersion(
    protocGenES,
    ["--version"],
    `protoc-gen-es v${requiredPlugin(profile, "@bufbuild/protoc-gen-es").version}`,
    "protoc-gen-es",
    false,
    { PATH: `${dirname(node)}:${process.env.PATH ?? ""}` },
  );
  return { protoc, protocGenGo, protocGenConnectGo, protocGenES };
}

function ensureProtoc(
  cacheRoot: string,
  profile: ProtoGenerationProfile,
  artifact: ToolArtifact,
): string {
  const installRoot = resolve(cacheRoot, `protoc-${profile.compiler.version}`);
  const binary = resolve(installRoot, "bin/protoc");
  if (platformProtocInstallationComplete(installRoot)) return binary;
  rmSync(installRoot, { force: true, recursive: true });
  const staging = mkdtempSync(join(cacheRoot, ".protoc-stage-"));
  try {
    const archive = resolve(staging, basename(new URL(artifact.url).pathname));
    run(
      "curl",
      ["--fail", "--location", "--silent", "--show-error", "--output", archive, artifact.url],
      cacheRoot,
      "download protoc",
    );
    if (digest(readFileSync(archive)).slice("sha256:".length) !== artifact.sha256) {
      throw new Error("Downloaded protoc archive digest does not match the pinned artifact.");
    }
    const expanded = resolve(staging, "expanded");
    mkdirSync(expanded);
    run("unzip", ["-q", archive, "-d", expanded], cacheRoot, "unpack protoc");
    renameSync(expanded, installRoot);
    chmodSync(binary, 0o755);
    return binary;
  } finally {
    rmSync(staging, { force: true, recursive: true });
  }
}

export function platformProtocInstallationComplete(installRoot: string): boolean {
  return ["bin/protoc", "include/google/protobuf/timestamp.proto"].every((path) =>
    existsSync(resolve(installRoot, path)),
  );
}

function ensureGoPlugin(go: string, pluginRoot: string, plugin: PluginProfile): string {
  const binaryName = plugin.id.endsWith("protoc-gen-go")
    ? "protoc-gen-go"
    : "protoc-gen-connect-go";
  const binary = resolve(pluginRoot, binaryName);
  if (existsSync(binary)) return binary;
  mkdirSync(pluginRoot, { recursive: true });
  run(go, ["install", `${plugin.id}@${plugin.version}`], pluginRoot, `build ${binaryName}`, {
    GOBIN: pluginRoot,
    GOFLAGS: "-trimpath -buildvcs=false",
    GOWORK: "off",
    GOTOOLCHAIN: "local",
  });
  return binary;
}

function validateDescriptorSet(bytes: Buffer, profile: ProtoGenerationProfile): void {
  const descriptor = fromBinary(FileDescriptorSetSchema, bytes);
  const names = descriptor.file.map((file) => file.name).toSorted();
  const expected = ["google/protobuf/timestamp.proto", ...profile.sources].toSorted();
  if (JSON.stringify(names) !== JSON.stringify(expected)) {
    throw new Error(`Descriptor file set drifted: ${names.join(", ")}.`);
  }
  const cloudFiles = descriptor.file.filter((file) => profile.sources.includes(file.name));
  if (cloudFiles.some((file) => file.sourceCodeInfo !== undefined)) {
    throw new Error("Generated descriptor must exclude source info.");
  }
  const services = cloudFiles.flatMap((file) => file.service);
  const methodCount = services.reduce((count, service) => count + service.method.length, 0);
  if (services.length !== 3 || methodCount !== 12) {
    throw new Error(
      `Descriptor service mapping drifted: ${services.length} services/${methodCount} methods.`,
    );
  }
  if (
    services.some((service) =>
      service.method.some((method) => method.clientStreaming || method.serverStreaming),
    )
  ) {
    throw new Error("The approved v1alpha1 compatibility profile permits unary RPCs only.");
  }
}

function validateWorkerRuntimeDescriptorSet(bytes: Buffer): void {
  const descriptor = fromBinary(FileDescriptorSetSchema, bytes);
  const names = descriptor.file.map((file) => file.name).toSorted();
  const expected = [
    "google/protobuf/timestamp.proto",
    "contracts/worker/v1alpha1/kernel.proto",
    WORKER_RUNTIME_PROTO_SOURCE,
  ].toSorted();
  if (JSON.stringify(names) !== JSON.stringify(expected)) {
    throw new Error(`Worker Runtime descriptor file set drifted: ${names.join(", ")}.`);
  }
  const runtime = descriptor.file.find((file) => file.name === WORKER_RUNTIME_PROTO_SOURCE);
  if (!runtime || runtime.sourceCodeInfo !== undefined) {
    throw new Error("Generated Worker Runtime descriptor must exist and exclude source info.");
  }
  const methods = runtime.service.flatMap((service) => service.method);
  if (
    runtime.service.length !== 1 ||
    methods.length !== 2 ||
    methods[0]?.name !== "OpenSession" ||
    !methods[0].clientStreaming ||
    !methods[0].serverStreaming ||
    methods[1]?.name !== "ReadArtifact" ||
    methods[1].clientStreaming ||
    !methods[1].serverStreaming
  ) {
    throw new Error(
      "Worker Runtime descriptor must expose OpenSession and server-streaming ReadArtifact RPCs.",
    );
  }
}

function assertExactBreakingBaseline(current: Buffer, baseline: Buffer): void {
  if (!current.equals(baseline)) {
    throw new Error(
      "Proto descriptor differs from the fixed v1alpha1 breaking baseline; a separately reviewed versioned baseline is required.",
    );
  }
}

function typescriptIndexSource(): string {
  return `// Code generated by scripts/generate-platform-proto-sdks.ts; DO NOT EDIT.\n\nexport * from "./gen/contracts/worker/v1alpha1/kernel_pb.js";\nexport * from "./gen/contracts/worker/v1alpha1/worker_supervisor_pb.js";\nexport * from "./gen/contracts/platform-adapter/v1alpha1/platform_adapter_pb.js";\n`;
}

function readProfile(root: string): ProtoGenerationProfile {
  const profile = JSON.parse(
    readRegularFile(root, PLATFORM_PROTO_PROFILE_PATH).toString("utf8"),
  ) as ProtoGenerationProfile;
  if (
    profile.formatVersion !== "cloud-agents-proto-generation-profile/v1" ||
    profile.status !== "GENERATED_NON_GATE_EVIDENCE" ||
    profile.authority !== "proto3" ||
    !profile.descriptor.includeImports ||
    profile.descriptor.includeSourceInfo ||
    profile.descriptor.output !== PLATFORM_PROTO_DESCRIPTOR_PATH ||
    profile.descriptor.breakingBaseline !== PLATFORM_PROTO_BREAKING_BASELINE_PATH
  ) {
    throw new Error("Proto generation profile boundary drifted.");
  }
  if (new Set(profile.sources).size !== 3 || profile.plugins.length !== 3) {
    throw new Error("Proto generation source/plugin set drifted.");
  }
  return profile;
}

function requiredPlugin(profile: ProtoGenerationProfile, id: string): PluginProfile {
  const plugin = profile.plugins.find((candidate) => candidate.id === id);
  if (!plugin) throw new Error(`Missing Proto plugin profile ${id}.`);
  return plugin;
}

function platformIdentity(): string {
  const os = process.platform === "darwin" ? "darwin" : process.platform === "linux" ? "linux" : "";
  const arch = process.arch === "arm64" ? "arm64" : process.arch === "x64" ? "amd64" : "";
  if (!os || !arch)
    throw new Error(`Unsupported Proto generator platform ${process.platform}/${process.arch}.`);
  return `${os}-${arch}`;
}

function assertVersion(
  command: string,
  arguments_: ReadonlyArray<string>,
  expected: string,
  label: string,
  prefix = false,
  environment: Readonly<Record<string, string>> = {},
): void {
  const result = spawnSync(command, arguments_, {
    encoding: "utf8",
    env: { ...process.env, ...environment },
  });
  if (result.status !== 0) throw new Error(`${label} is unavailable: ${result.stderr.trim()}.`);
  const actual = `${result.stdout}${result.stderr}`.trim();
  if (prefix ? !actual.startsWith(expected) : actual !== expected) {
    throw new Error(`${label} version mismatch: ${actual}; expected ${expected}.`);
  }
}

function run(
  command: string,
  arguments_: ReadonlyArray<string>,
  cwd: string,
  label: string,
  environment: Readonly<Record<string, string>> = {},
): void {
  const result = spawnSync(command, arguments_, {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...environment },
    maxBuffer: 16 * 1024 * 1024,
  });
  if (result.status !== 0) {
    throw new Error(`${label} failed: ${(result.stderr || result.stdout).trim()}`);
  }
}

function formatGeneratedText(root: string, path: string, source: string): string {
  return formatWithOxfmt(root, path, source);
}

function assertGeneratedTree(root: string, expected: ReadonlyArray<string>): void {
  const actual = walkFiles(root)
    .map((path) => relative(root, path).split(sep).join("/"))
    .toSorted();
  if (JSON.stringify(actual) !== JSON.stringify([...expected].toSorted())) {
    throw new Error(`Proto plugin output set drifted: ${actual.join(", ")}.`);
  }
}

function walkFiles(root: string): string[] {
  return readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const path = resolve(root, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`Generated output ${path} must not be a symlink.`);
    return entry.isDirectory() ? walkFiles(path) : entry.isFile() ? [path] : [];
  });
}

function requiredGenerated(outputs: ReadonlyArray<GeneratedFile>, path: string): GeneratedFile {
  const output = outputs.find((candidate) => candidate.path === path);
  if (!output) throw new Error(`Missing generated output ${path}.`);
  return output;
}

function writeGenerated(root: string, output: GeneratedFile): void {
  const target = resolve(root, output.path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, output.bytes, { mode: 0o644 });
  chmodSync(target, 0o644);
}

function assertCurrent(root: string, output: GeneratedFile): void {
  const target = resolve(root, output.path);
  if (!existsSync(target) || !readRegularFile(root, output.path).equals(output.bytes)) {
    throw new Error(
      `${output.path} is stale; run bun scripts/generate-platform-proto-sdks.ts --write with the pinned toolchain.`,
    );
  }
}

function readRegularFile(root: string, path: string): Buffer {
  const target = resolve(root, path);
  const stat = lstatSync(target);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${path} must be a regular file.`);
  return readFileSync(target);
}

function digest(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
