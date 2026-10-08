import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import {
  EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
  ExternalConsumerV3Source,
  readCurrentExternalConsumerV3Source,
} from "./platform-g-contract-external-consumer-v3";
import { decodeExternalConsumerV3Ustar } from "./platform-g-contract-external-consumer-v3-ustar";
import { canonicalJsonDigest } from "./platform-json-semantics";

const PROJECTION_DIRECTORY = "tools/g-contract-external-consumer/v3/evidence/replay";
const PROJECTION_ARCHIVE = `${PROJECTION_DIRECTORY}/projection.tar`;
const PROJECTION_MEMBER_MANIFEST = `${PROJECTION_DIRECTORY}/projection.member-manifest.json`;
const PROJECTION_RECEIPT = `${PROJECTION_DIRECTORY}/projection.json`;
const NATIVE_ENTRYPOINT =
  "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --native --projection-root <projection-root> --platform <platform> --run-id <run-id> --output-root <output-root>";
const WORKER_NEGOTIATE_PATH = "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate";
const PROJECT_PATH = "/v1/tenants/tenant-alpha/projects/project-alpha";

type SourceBinding = ReturnType<typeof readCurrentExternalConsumerV3Source>["sourceBinding"];

type Platform = "darwin-arm64" | "linux-amd64";
type RunId = "A" | "B";
type Digest = `sha256:${string}`;

export type ExternalConsumerV3NativeReplayOptions = Readonly<{
  authorityRoot: string;
  projectionRoot: string;
  outputRoot: string;
  platform: Platform;
  runId: RunId;
}>;

export type ExternalConsumerV3NativeReplayResult = Readonly<{
  receiptPath: string;
  receipt: Readonly<Record<string, unknown>>;
}>;

type ProjectionReceipt = Readonly<{
  formatVersion: string;
  decisionId: string;
  profileId: string;
  status: string;
  sourceBinding: FileBinding;
  projection: Readonly<{
    archivePath: string;
    archiveSha256: Digest;
    archiveSizeBytes: number;
    memberManifestPath: string;
    memberManifestSha256: Digest;
    memberCount: number;
    inputManifest: Readonly<{ sha256: Digest; memberCount: number }>;
  }>;
}>;

type FileBinding = Readonly<{ path: string; sha256: Digest; sizeBytes: number }>;

/**
 * Replay one real TypeScript/Go SDK consumer pair from the frozen projection.
 * The run root is disposable and all dependency/network operations are forced
 * through the existing harness' offline and loopback modes.
 */
export function runExternalConsumerV3NativeReplay(
  options: ExternalConsumerV3NativeReplayOptions,
): ExternalConsumerV3NativeReplayResult {
  assertPlatformRuntime(options.platform);
  const authorityRoot = canonicalRoot(options.authorityRoot, "authority root");
  const projectionRoot = canonicalRoot(options.projectionRoot, "projection root");
  const outputRoot = canonicalRoot(options.outputRoot, "output root");
  if (authorityRoot === projectionRoot || authorityRoot === outputRoot) {
    throw new Error("Projection and output roots must be external to the authority root.");
  }
  if (isContained(authorityRoot, projectionRoot) || isContained(authorityRoot, outputRoot)) {
    throw new Error("Projection and output roots must not be nested under the authority root.");
  }
  if (
    projectionRoot === outputRoot ||
    isContained(projectionRoot, outputRoot) ||
    isContained(outputRoot, projectionRoot)
  ) {
    throw new Error("Projection and output roots must be distinct and non-nested.");
  }
  const { source, sourceBinding } = readCurrentExternalConsumerV3Source(authorityRoot);
  const { projection, members } = readVerifiedExternalConsumerV3Projection(
    authorityRoot,
    projectionRoot,
    source,
    sourceBinding,
  );
  const runRoot = realpathSync(
    mkdtempSync(join(tmpdir(), "cloud-agents-ec3-native-replay-"), {
      encoding: "utf8",
    }),
  );
  const requestSummary = join(runRoot, "request-summary.jsonl");
  try {
    extractMembers(runRoot, members);
    runCommand(
      bunCommand(),
      ["install", "--frozen-lockfile", "--ignore-scripts", "--offline"],
      runRoot,
      {
        BUN_CONFIG_REGISTRY: "https://registry.npmjs.org/",
        NO_PROXY: "127.0.0.1,localhost",
        no_proxy: "127.0.0.1,localhost",
      },
    );
    runCommand(bunCommand(), ["run", "--cwd", "sdk/typescript", "build"], runRoot, {
      NO_PROXY: "127.0.0.1,localhost",
      no_proxy: "127.0.0.1,localhost",
    });
    runCommand(bunCommand(), ["test/scripts/test-platform-sdk-consumers.ts"], runRoot, {
      CLOUD_AGENTS_SDK_CONSUMER_ROOT: runRoot,
      CLOUD_AGENTS_SDK_CONSUMER_OFFLINE: "1",
      CLOUD_AGENTS_SDK_CONSUMER_REQUEST_SUMMARY: requestSummary,
      NO_PROXY: "127.0.0.1,localhost",
      no_proxy: "127.0.0.1,localhost",
    });
    const consumers = buildExternalConsumerV3ConsumerBindings(readFileSync(requestSummary, "utf8"));
    const receipt = buildNativeReceipt(options, source, projection, sourceBinding, consumers);
    validateNativeReceipt(authorityRoot, receipt);
    const receiptPath = join(
      outputRoot,
      `${PROJECTION_DIRECTORY}/${options.platform}-${options.runId.toLowerCase()}.json`,
    );
    writeExclusive(receiptPath, Buffer.from(`${JSON.stringify(receipt, null, 2)}\n`, "utf8"));
    return { receiptPath, receipt };
  } finally {
    rmSync(runRoot, { recursive: true, force: true });
  }
}

/** Shared projection-byte validation for native replay and summary assembly. */
export function readVerifiedExternalConsumerV3Projection(
  authorityRoot: string,
  projectionRoot: string,
  source: ExternalConsumerV3Source,
  sourceBinding: SourceBinding,
) {
  const { projection, projectionReceiptBytes } = readProjectionReceipt(
    authorityRoot,
    projectionRoot,
    source,
    sourceBinding,
  );
  const archive = readBoundFile(projectionRoot, projection.projection.archivePath);
  const memberManifest = readBoundFile(projectionRoot, projection.projection.memberManifestPath);
  if (
    digest(archive.bytes) !== projection.projection.archiveSha256 ||
    archive.bytes.byteLength !== projection.projection.archiveSizeBytes
  ) {
    throw new Error("Projection archive bytes do not match the projection receipt.");
  }
  if (digest(memberManifest.bytes) !== projection.projection.memberManifestSha256) {
    throw new Error("Projection member manifest bytes do not match the projection receipt.");
  }
  const members = decodeExternalConsumerV3Ustar(new Uint8Array(archive.bytes));
  if (members.length !== projection.projection.memberCount) {
    throw new Error("Projection archive member count differs from the projection receipt.");
  }
  return {
    projection,
    projectionReceiptBytes,
    archiveBytes: archive.bytes,
    memberManifestBytes: memberManifest.bytes,
    members,
  };
}

function readProjectionReceipt(
  authorityRoot: string,
  projectionRoot: string,
  source: ExternalConsumerV3Source,
  sourceBinding: SourceBinding,
): { projection: ProjectionReceipt; projectionReceiptBytes: Buffer } {
  const receiptPath = safePath(projectionRoot, PROJECTION_RECEIPT);
  const projectionReceiptBytes = readFileSync(receiptPath);
  const receipt = JSON.parse(projectionReceiptBytes.toString("utf8")) as ProjectionReceipt;
  validateProjectionReceipt(authorityRoot, receipt);
  validateProjectionShape(receipt);
  if (
    receipt.status !== "CURRENT" ||
    receipt.decisionId !== source.decisionId ||
    receipt.profileId !== source.profileId
  ) {
    throw new Error("Projection receipt is not a current EC-3 v3 receipt.");
  }
  if (
    receipt.sourceBinding.path !== sourceBinding.path ||
    receipt.sourceBinding.sha256 !== sourceBinding.sha256 ||
    receipt.sourceBinding.sizeBytes !== sourceBinding.sizeBytes
  ) {
    throw new Error("Projection receipt source binding differs from current authority.");
  }
  if (
    receipt.projection.archivePath !== PROJECTION_ARCHIVE ||
    receipt.projection.memberManifestPath !== PROJECTION_MEMBER_MANIFEST
  ) {
    throw new Error("Projection receipt paths are not canonical.");
  }
  return { projection: receipt, projectionReceiptBytes };
}

function validateProjectionShape(receipt: ProjectionReceipt): void {
  if (
    !receipt ||
    typeof receipt !== "object" ||
    receipt.formatVersion !== "cloud-agents-g-contract-external-consumer-projection-receipt/v3"
  ) {
    throw new Error("Projection receipt format is invalid.");
  }
  const projection = receipt.projection;
  if (
    !projection ||
    typeof projection !== "object" ||
    !projection.inputManifest ||
    typeof projection.inputManifest !== "object" ||
    !/^sha256:[0-9a-f]{64}$/u.test(projection.archiveSha256) ||
    !/^sha256:[0-9a-f]{64}$/u.test(projection.memberManifestSha256) ||
    !/^sha256:[0-9a-f]{64}$/u.test(projection.inputManifest.sha256) ||
    !Number.isSafeInteger(projection.archiveSizeBytes) ||
    !Number.isSafeInteger(projection.memberCount) ||
    !Number.isSafeInteger(projection.inputManifest.memberCount)
  ) {
    throw new Error("Projection receipt binding is invalid.");
  }
}

function buildNativeReceipt(
  options: ExternalConsumerV3NativeReplayOptions,
  source: ExternalConsumerV3Source,
  projection: ProjectionReceipt,
  sourceBinding: SourceBinding,
  consumers: ConsumerBindings,
): Record<string, unknown> {
  const receiptPath = `${PROJECTION_DIRECTORY}/${options.platform}-${options.runId.toLowerCase()}.json`;
  return {
    formatVersion: "cloud-agents-g-contract-external-consumer-native-replay-receipt/v3",
    decisionId: source.decisionId,
    profileId: source.profileId,
    platform: options.platform,
    runId: options.runId,
    status: "CURRENT",
    sourceBinding,
    projectionBinding: {
      archivePath: projection.projection.archivePath,
      archiveSha256: projection.projection.archiveSha256,
      archiveSizeBytes: projection.projection.archiveSizeBytes,
      memberManifestSha256: projection.projection.memberManifestSha256,
      inputManifestSha256: projection.projection.inputManifest.sha256,
      memberCount: projection.projection.memberCount,
    },
    runner: { path: EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH, entrypoint: NATIVE_ENTRYPOINT },
    consumers,
    isolation: {
      receiptPath,
      network: "LOOPBACK_ONLY",
      filesystem: "DISPOSABLE_ROOT_ONLY",
      ambientNodeModules: false,
    },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
}

function consumerBinding(observedCallCount: number, outputSha256: string) {
  return {
    requiredCallCount: 1,
    observedCallCount,
    outputSha256,
    requestContentType: "application/proto",
    responseContentType: "application/proto",
    url: "http://127.0.0.1:<ephemeral-port>/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate",
  };
}

type RequestRecord = Readonly<{
  consumer: string;
  method: string;
  path: string;
  contentType: string | null;
  status: number;
}>;

type Consumer = "typescript" | "go";
type ConsumerBindings = Readonly<Record<Consumer, ReturnType<typeof consumerBinding>>>;

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** Consume the existing fixture JSONL; aggregate totals cannot establish attribution. */
export function buildExternalConsumerV3ConsumerBindings(summary: string): ConsumerBindings {
  const records: RequestRecord[] = [];
  const outcomes: Record<Consumer, string[]> = { typescript: [], go: [] };
  for (const line of summary.split("\n").filter(Boolean)) {
    const value: unknown = JSON.parse(line);
    if (!isRecord(value)) throw new Error("SDK consumer summary contains an invalid record.");
    if (value.kind === "outcome") {
      if (value.consumer !== "typescript" && value.consumer !== "go") {
        throw new Error("SDK consumer outcome has an unknown consumer.");
      }
      const result = value.result;
      if (
        !isRecord(result) ||
        !isRecord(result.project) ||
        typeof result.negotiationProtoHex !== "string" ||
        !/^(?:[0-9a-f]{2})*$/u.test(result.negotiationProtoHex) ||
        Object.keys(result).length !== 2
      ) {
        throw new Error("SDK consumer outcome must contain decoded project and protobuf bytes.");
      }
      outcomes[value.consumer].push(canonicalJsonDigest(result));
      continue;
    }
    if (
      value.kind !== undefined ||
      (value.consumer !== "typescript" &&
        value.consumer !== "go" &&
        value.consumer !== "unknown") ||
      typeof value.method !== "string" ||
      typeof value.path !== "string" ||
      (value.contentType !== null && typeof value.contentType !== "string") ||
      !Number.isSafeInteger(value.status)
    ) {
      throw new Error("SDK consumer request summary contains an invalid record.");
    }
    records.push(value as RequestRecord);
  }
  if (records.some((record) => !record.path.startsWith("/"))) {
    throw new Error("SDK consumer request summary contains a non-canonical path.");
  }
  if (records.some((record) => record.status >= 400)) {
    throw new Error("SDK consumer request summary contains a failed fixture request.");
  }
  if (
    records.some(
      (record) =>
        record.consumer === "unknown" &&
        (record.path === PROJECT_PATH || record.path === WORKER_NEGOTIATE_PATH),
    )
  ) {
    throw new Error("SDK API request has no named consumer.");
  }
  function observedBinding(consumer: Consumer): ReturnType<typeof consumerBinding> {
    const calls = records.filter((record) => record.consumer === consumer);
    const projects = calls.filter(
      (record) => record.method === "GET" && record.path === PROJECT_PATH && record.status === 200,
    );
    const negotiations = calls.filter(
      (record) =>
        record.method === "POST" &&
        record.path === WORKER_NEGOTIATE_PATH &&
        record.status === 200 &&
        record.contentType === "application/proto",
    );
    if (calls.length !== 2 || projects.length !== 1 || negotiations.length !== 1) {
      throw new Error(
        `SDK calls must occur exactly once for ${consumer}: project=${projects.length} negotiate=${negotiations.length}`,
      );
    }
    if (outcomes[consumer].length !== 1) {
      throw new Error(`SDK outcome count must be one for ${consumer}.`);
    }
    return consumerBinding(negotiations.length, outcomes[consumer][0]!);
  }
  return { typescript: observedBinding("typescript"), go: observedBinding("go") };
}

function validateProjectionReceipt(authorityRoot: string, receipt: unknown): void {
  const schema = readJson(safePath(authorityRoot, EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH));
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, receipt)) {
    throw new Error(`Projection receipt schema validation failed: ${ajv.errorsText()}`);
  }
}

function validateNativeReceipt(authorityRoot: string, receipt: Record<string, unknown>): void {
  const schema = readJson(safePath(authorityRoot, EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH));
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, receipt))
    throw new Error(`Native replay receipt schema validation failed: ${ajv.errorsText()}`);
}

function extractMembers(
  root: string,
  members: ReturnType<typeof decodeExternalConsumerV3Ustar>,
): void {
  for (const member of members) {
    const path = safePath(root, member.path, true);
    if (member.type === "directory") {
      mkdirSync(path, { recursive: false, mode: member.mode });
      chmodSync(path, member.mode);
      continue;
    }
    mkdirSync(resolve(path, ".."), { recursive: true, mode: 0o755 });
    writeFileSync(path, member.data, { flag: "wx", mode: member.mode });
    chmodSync(path, member.mode);
  }
}

function readBoundFile(root: string, path: string): { readonly bytes: Buffer } {
  return { bytes: readFileSync(safePath(root, path)) };
}

function digest(bytes: Uint8Array): Digest {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

export function safePath(root: string, path: string, allowMissing = false): string {
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((part) => part.length === 0 || part === "." || part === "..")
  ) {
    throw new Error(`Unsafe repository-relative path: ${path}`);
  }
  const realRoot = realpathSync(root);
  const absolute = resolve(realRoot, path);
  if (relative(realRoot, absolute).split(sep).join("/") !== path)
    throw new Error(`Path escapes root: ${path}`);
  let cursor = realRoot;
  for (const [index, part] of path.split("/").entries()) {
    cursor = join(cursor, part);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (stat === undefined && allowMissing) return absolute;
    if (
      stat === undefined ||
      stat.isSymbolicLink() ||
      (index === path.split("/").length - 1 ? !stat.isFile() : !stat.isDirectory())
    ) {
      throw new Error(`${path} must not contain symlink or non-file parent.`);
    }
  }
  return absolute;
}

export function canonicalRoot(value: string, label: string): string {
  if (!isAbsolute(value)) throw new Error(`${label} must be absolute.`);
  const absolute = resolve(value);
  const stat = lstatSync(absolute, { throwIfNoEntry: false });
  if (stat === undefined || stat.isSymbolicLink() || !stat.isDirectory())
    throw new Error(`${label} must be a real directory.`);
  const real = realpathSync(absolute);
  if (real !== absolute) throw new Error(`${label} must not resolve through a symlink.`);
  return real;
}

export function isContained(parent: string, child: string): boolean {
  const relation = relative(parent, child);
  return (
    relation === "" ||
    (!isAbsolute(relation) && relation !== ".." && !relation.startsWith(`..${sep}`))
  );
}

function writeExclusive(path: string, bytes: Uint8Array): void {
  mkdirSync(resolve(path, ".."), { recursive: true, mode: 0o755 });
  writeFileSync(path, bytes, { flag: "wx", mode: 0o644 });
}

function readJson(path: string): unknown {
  return JSON.parse(readFileSync(path, "utf8"));
}

function runCommand(
  command: string,
  args: readonly string[],
  cwd: string,
  extraEnv: Record<string, string>,
): void {
  const result = spawnSync(command, [...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...extraEnv },
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.status !== 0)
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}):\n${result.stdout}\n${result.stderr}`,
    );
}

function bunCommand(): string {
  const toolchainRoot = process.env.CLOUD_AGENTS_A24_TOOLCHAIN;
  return toolchainRoot ? join(toolchainRoot, "bun") : "bun";
}

function assertPlatformRuntime(platform: Platform): void {
  const expected = platform === "darwin-arm64" ? ["darwin", "arm64"] : ["linux", "x64"];
  if (process.platform !== expected[0] || process.arch !== expected[1]) {
    throw new Error(
      `Native replay platform ${platform} does not match the current runtime ${process.platform}/${process.arch}.`,
    );
  }
}
