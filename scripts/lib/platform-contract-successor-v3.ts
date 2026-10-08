import { createHash } from "node:crypto";
import { lstatSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import { canonicalizeJson } from "./platform-json-semantics";
import { SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS } from "./platform-successor-dag";

/**
 * The v2 successor is immutable and still names five outputs removed by the
 * open-source cleanup.  v3 carries that authority forward as a predecessor
 * and binds the exact current output set instead of restoring dead files.
 */
export const PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH =
  "contracts/generation.lock.successor-v3.json" as const;
export const PLATFORM_CONTRACT_SUCCESSOR_V3_FORMAT =
  "cloud-agents-platform-contract-generation-lock/v3" as const;
export const PLATFORM_CONTRACT_SUCCESSOR_V3_DOMAIN =
  "cloud-agents/platform-contract-generation-lock/document/v3" as const;

const CURRENT_PROTO_GENERATOR_OUTPUTS = [
  "contracts/generated/proto/cloud-agents-worker-runtime-v1alpha1.binpb",
  "sdk/go/gen/cloudagents/worker/runtime/v1alpha1/runtime.pb.go",
  "sdk/go/gen/cloudagents/worker/runtime/v1alpha1/workerruntimev1alpha1connect/runtime.connect.go",
] as const;

const PREDECESSOR_PATH = "contracts/generation.lock.json" as const;
const PREDECESSOR_FROZEN = {
  sha256: "sha256:de62a85390e58736a5fef8272878b821415b4180948437e54edb8628e005ff53",
  sizeBytes: 17377,
  gitBlob: "39ee20e035d8770340d46a8663633c6519830de1",
} as const;
const CURRENT_OUTPUT_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1" as const;

const REMOVED_PREDECESSOR_OUTPUTS = new Set([
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go",
]);

export const PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS = [
  ...SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS.filter((path) => !REMOVED_PREDECESSOR_OUTPUTS.has(path)),
  ...CURRENT_PROTO_GENERATOR_OUTPUTS,
].toSorted((left, right) =>
  Buffer.compare(Buffer.from(left), Buffer.from(right)),
) as readonly string[];

export const PLATFORM_CONTRACT_SUCCESSOR_V3_REPLAY_PATHS = [
  "tools/contract-successor/v3/evidence/replay.json",
  "tools/contract-successor/v3/evidence/replay/projection.json",
  "tools/contract-successor/v3/profile.json",
] as const;

export const PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS = [
  PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH,
  ...PLATFORM_CONTRACT_SUCCESSOR_V3_REPLAY_PATHS,
] as const;

type FileRecord = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
  gitMode: "100644" | "100755";
}>;

export type PlatformContractSuccessorV3Document = Readonly<{
  formatVersion: typeof PLATFORM_CONTRACT_SUCCESSOR_V3_FORMAT;
  lockVersion: 3;
  status: "SUCCESSOR_CURRENT_PRE_REPLAY";
  notGateClosure: true;
  gateStatus: "ALL_GATES_OPEN";
  predecessor: Readonly<{
    formatVersion: "cloud-agents-platform-contract-generation-lock/v2";
    path: "contracts/generation.lock.json";
    sha256: string;
    sizeBytes: number;
    gitBlob: string;
  }>;
  currentOutputSet: Readonly<{
    algorithm: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1";
    count: 47;
    manifestSha256: string;
    files: readonly FileRecord[];
  }>;
  projection: Readonly<{
    exclusions: readonly string[];
    replayState: "REPLAY_PENDING";
  }>;
  implementationBoundary: Readonly<{
    productionDatabaseWrites: "NOT_AUTHORIZED";
    providerSideEffects: "FORBIDDEN";
    deployment: "NOT_AUTHORIZED";
    publication: "NOT_AUTHORIZED";
    gateStatus: "ALL_GATES_OPEN";
  }>;
  lockDigest: string;
}>;

const SHA256 = /^sha256:[0-9a-f]{64}$/u;
const GIT_BLOB = /^[0-9a-f]{40}$/u;

function contained(root: string, path: string, allowMissing = false): string {
  const rootReal = realpathSync(root);
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw new Error(`Path is not a canonical repository-relative path: ${path}`);
  }
  const absolute = resolve(rootReal, path);
  const relativePath = relative(rootReal, absolute).split(sep).join("/");
  if (relativePath !== path || relativePath === "") {
    throw new Error(`Path escapes repository root: ${path}`);
  }
  let cursor = rootReal;
  for (const [index, segment] of path.split("/").entries()) {
    cursor = resolve(cursor, segment);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (stat === undefined && allowMissing) return absolute;
    if (
      stat === undefined ||
      stat.isSymbolicLink() ||
      (index === path.split("/").length - 1 ? !stat.isFile() : !stat.isDirectory())
    ) {
      throw new Error(`${path} must be a regular file with non-symlink parents.`);
    }
  }
  return absolute;
}

function sha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function assertFrozenPredecessor(root: string): void {
  const bytes = readFileSync(contained(root, PREDECESSOR_PATH));
  if (
    bytes.byteLength !== PREDECESSOR_FROZEN.sizeBytes ||
    sha256(bytes) !== PREDECESSOR_FROZEN.sha256
  ) {
    throw new Error(`${PREDECESSOR_PATH} does not match the frozen v2 predecessor.`);
  }
}

function fileRecord(root: string, path: string): FileRecord {
  const absolute = contained(root, path);
  const stat = lstatSync(absolute);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`Successor v3 output must be a regular file: ${path}`);
  }
  const bytes = readFileSync(absolute);
  return {
    path,
    sha256: sha256(bytes),
    sizeBytes: bytes.byteLength,
    gitMode: (stat.mode & 0o111) === 0 ? "100644" : "100755",
  };
}

function manifestDigest(files: readonly FileRecord[]): string {
  const sorted = [...files].toSorted((a, b) =>
    Buffer.compare(Buffer.from(a.path), Buffer.from(b.path)),
  );
  const body = sorted.map((file) => `${file.path}\0${file.sha256}\0${file.gitMode}\0`).join("");
  return sha256(Buffer.from(body, "utf8"));
}

function domainDigest(body: unknown): string {
  return sha256(
    Buffer.concat([
      Buffer.from(PLATFORM_CONTRACT_SUCCESSOR_V3_DOMAIN),
      Buffer.from("\0"),
      Buffer.from(canonicalizeJson(body)),
    ]),
  );
}

function lockBody(root: string): Omit<PlatformContractSuccessorV3Document, "lockDigest"> {
  if (PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS.length !== 47) {
    throw new Error("Successor v3 must bind exactly 47 current generator outputs.");
  }
  const files = PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS.map((path) => fileRecord(root, path));
  const sorted = [...files].toSorted((a, b) =>
    Buffer.compare(Buffer.from(a.path), Buffer.from(b.path)),
  );
  if (JSON.stringify(files) !== JSON.stringify(sorted)) {
    throw new Error("Successor v3 output paths must be UTF-8 bytewise sorted.");
  }
  assertFrozenPredecessor(root);
  return {
    formatVersion: PLATFORM_CONTRACT_SUCCESSOR_V3_FORMAT,
    lockVersion: 3,
    status: "SUCCESSOR_CURRENT_PRE_REPLAY",
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
    predecessor: {
      formatVersion: "cloud-agents-platform-contract-generation-lock/v2",
      path: PREDECESSOR_PATH,
      sha256: PREDECESSOR_FROZEN.sha256,
      sizeBytes: PREDECESSOR_FROZEN.sizeBytes,
      gitBlob: PREDECESSOR_FROZEN.gitBlob,
    },
    currentOutputSet: {
      algorithm: CURRENT_OUTPUT_MANIFEST_ALGORITHM,
      count: files.length as 47,
      manifestSha256: manifestDigest(files),
      files,
    },
    projection: {
      exclusions: [...PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
      replayState: "REPLAY_PENDING",
    },
    implementationBoundary: {
      productionDatabaseWrites: "NOT_AUTHORIZED",
      providerSideEffects: "FORBIDDEN",
      deployment: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
      gateStatus: "ALL_GATES_OPEN",
    },
  };
}

export function buildPlatformContractSuccessorV3(
  root: string,
): PlatformContractSuccessorV3Document {
  const body = lockBody(root);
  return { ...body, lockDigest: domainDigest(body) };
}

export function serializePlatformContractSuccessorV3(
  document: PlatformContractSuccessorV3Document,
): string {
  return `${JSON.stringify(document, null, 2)}\n`;
}

export function assertPlatformContractSuccessorV3Current(root: string): void {
  const expected = serializePlatformContractSuccessorV3(buildPlatformContractSuccessorV3(root));
  const actual = readFileSync(contained(root, PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(`${PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH} is stale.`);
  }
}

export function writePlatformContractSuccessorV3(root: string): void {
  const path = contained(root, PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH, true);
  writeFileSync(path, serializePlatformContractSuccessorV3(buildPlatformContractSuccessorV3(root)));
}

export function assertPlatformContractSuccessorV3PredecessorFrozen(root: string): void {
  assertFrozenPredecessor(root);
}

export function assertPlatformContractSuccessorV3ReplayAbsent(root: string): void {
  for (const path of PLATFORM_CONTRACT_SUCCESSOR_V3_REPLAY_PATHS) {
    const absolute = contained(root, path, true);
    if (lstatSync(absolute, { throwIfNoEntry: false }) !== undefined) {
      throw new Error(`${path} must remain absent until replay evidence exists.`);
    }
  }
}

export function assertPlatformContractSuccessorV3Document(
  root: string,
  document: PlatformContractSuccessorV3Document,
): void {
  const expected = buildPlatformContractSuccessorV3(root);
  if (JSON.stringify(document) !== JSON.stringify(expected)) {
    throw new Error("Successor v3 document does not match the current output set.");
  }
  if (!SHA256.test(document.lockDigest) || document.lockDigest !== expected.lockDigest) {
    throw new Error("Successor v3 lock digest is invalid.");
  }
  if (!GIT_BLOB.test(document.predecessor.gitBlob)) {
    throw new Error("Successor v3 predecessor Git blob is invalid.");
  }
  if (
    document.predecessor.sha256 !== PREDECESSOR_FROZEN.sha256 ||
    document.predecessor.sizeBytes !== PREDECESSOR_FROZEN.sizeBytes ||
    document.predecessor.gitBlob !== PREDECESSOR_FROZEN.gitBlob
  ) {
    throw new Error("Successor v3 predecessor is not the frozen v2 authority.");
  }
}
