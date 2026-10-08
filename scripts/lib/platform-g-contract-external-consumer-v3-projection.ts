import { createHash } from "node:crypto";
import {
  constants,
  closeSync,
  fstatSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readFileSync,
  realpathSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { execFileSync } from "node:child_process";
import { isAbsolute, join, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import {
  EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
  EXTERNAL_CONSUMER_V3_SOURCE_PATH,
  readCurrentExternalConsumerV3Source,
} from "./platform-g-contract-external-consumer-v3";
import {
  PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH,
  PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
} from "./platform-contract-successor-v3";
import {
  compareExternalConsumerV3UstarPath,
  decodeExternalConsumerV3Ustar,
  encodeExternalConsumerV3Ustar,
  ExternalConsumerV3UstarInput,
} from "./platform-g-contract-external-consumer-v3-ustar";

export const EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_FORMAT =
  "cloud-agents-g-contract-external-consumer-candidate-manifest/v1" as const;
export const EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-kind-mode-size-sha256-nul-v1" as const;
export const EXTERNAL_CONSUMER_V3_INPUT_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1" as const;
export const EXTERNAL_CONSUMER_V3_MEMBER_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1" as const;

const PROJECTION_OUTPUT_DIRECTORY = "tools/g-contract-external-consumer/v3/evidence/replay";
const PROJECTION_ARCHIVE_PATH = `${PROJECTION_OUTPUT_DIRECTORY}/projection.tar`;
const PROJECTION_MEMBER_MANIFEST_PATH = `${PROJECTION_OUTPUT_DIRECTORY}/projection.member-manifest.json`;
const PROJECTION_RECEIPT_PATH = `${PROJECTION_OUTPUT_DIRECTORY}/projection.json`;
const V3_PROFILE_PATH = "tools/g-contract-external-consumer/v3/profile.json";
const V3_RECEIPT_PATHS = [
  PROJECTION_RECEIPT_PATH,
  `${PROJECTION_OUTPUT_DIRECTORY}/darwin-arm64-a.json`,
  `${PROJECTION_OUTPUT_DIRECTORY}/darwin-arm64-b.json`,
  `${PROJECTION_OUTPUT_DIRECTORY}/linux-amd64-a.json`,
  `${PROJECTION_OUTPUT_DIRECTORY}/linux-amd64-b.json`,
  `${PROJECTION_OUTPUT_DIRECTORY}/replay.json`,
] as const;

type Digest = `sha256:${string}`;
type CandidateRecord = Readonly<{
  path: string;
  kind: "file";
  mode: "100644" | "100755";
  sizeBytes: number;
  sha256: Digest;
}>;

type CandidateManifest = Readonly<{
  formatVersion: typeof EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_FORMAT;
  source: "frozen-external-candidate-tree";
  candidateRoot: string;
  manifestAlgorithm: typeof EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_ALGORITHM;
  candidateFileCount: number;
  candidateEntryCount: number;
  records: readonly CandidateRecord[];
}>;

export type ExternalConsumerV3ProjectionResult = Readonly<{
  receipt: Readonly<Record<string, unknown>>;
  archivePath: string;
  memberManifestPath: string;
  receiptPath: string;
  archiveSha256: Digest;
  memberManifestSha256: Digest;
  candidateManifestSha256: Digest;
  candidateRootIdentity: Digest;
  memberCount: number;
}>;

export type ExternalConsumerV3ProjectionOptions = Readonly<{
  authorityRoot: string;
  candidateRoot: string;
  candidateManifestPath: string;
  outputRoot: string;
}>;

/**
 * Project an explicitly frozen external candidate. This function never reads
 * the authority root as the archive input and never writes beneath it.
 */
export function writeExternalConsumerV3Projection(
  options: ExternalConsumerV3ProjectionOptions,
): ExternalConsumerV3ProjectionResult {
  const authorityRoot = canonicalRoot(options.authorityRoot, "authority root");
  const candidateRoot = canonicalRoot(options.candidateRoot, "candidate root");
  const outputRoot = canonicalRoot(options.outputRoot, "output root");
  if (
    authorityRoot === candidateRoot ||
    authorityRoot === outputRoot ||
    candidateRoot === outputRoot
  ) {
    throw new Error("Authority, candidate, and output roots must be distinct.");
  }
  if (isContained(authorityRoot, candidateRoot) || isContained(authorityRoot, outputRoot)) {
    throw new Error(
      "Candidate and output roots must not be nested under the dirty authority root.",
    );
  }
  if (isContained(candidateRoot, outputRoot) || isContained(outputRoot, candidateRoot)) {
    throw new Error("Candidate and output roots must not be nested.");
  }

  const { source, sourceBinding } = readCurrentExternalConsumerV3Source(authorityRoot);
  const manifestPath = canonicalFile(options.candidateManifestPath, "candidate manifest");
  const { manifest, manifestBytes, records } = readCandidateManifest(manifestPath, candidateRoot);
  const sourceRecord = records.find((record) => record.path === EXTERNAL_CONSUMER_V3_SOURCE_PATH);
  const successorRecord = records.find(
    (record) => record.path === PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH,
  );
  if (!sourceRecord || !successorRecord) {
    throw new Error("Candidate manifest must bind the v3 source and successor lock.");
  }
  assertRecordBytes(candidateRoot, sourceRecord);
  assertRecordBytes(candidateRoot, successorRecord);
  if (
    sourceRecord.sha256 !== sourceBinding.sha256 ||
    sourceRecord.sizeBytes !== sourceBinding.sizeBytes
  ) {
    throw new Error("Candidate source binding differs from the current authority source.");
  }
  if (
    successorRecord.sha256 !== source.contractSuccessor.sha256 ||
    successorRecord.sizeBytes !== source.contractSuccessor.sizeBytes ||
    source.contractSuccessor.outputCount !== 47
  ) {
    throw new Error("Candidate successor binding differs from the current authority successor.");
  }
  assertCandidateGitInventory(candidateRoot, records);
  assertCandidateOutputsAbsent(candidateRoot);
  const firstSnapshot = readCandidateInputs(candidateRoot, records);
  assertCandidateGitInventory(candidateRoot, records);
  const secondSnapshot = readCandidateInputs(candidateRoot, records);
  if (!sameSnapshot(firstSnapshot, secondSnapshot)) {
    throw new Error("Candidate changed while projection inputs were read.");
  }

  const exclusions = new Set<string>(PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS);
  const selected = firstSnapshot.filter(({ record }) => !exclusions.has(record.path));
  const entries = projectionEntries(selected);
  const archive = encodeExternalConsumerV3Ustar(entries);
  const decoded = decodeExternalConsumerV3Ustar(archive);
  const memberManifestBytes = serializeMemberManifest(decoded);
  const archiveSha256 = digest(archive);
  const memberManifestSha256 = digest(memberManifestBytes);
  const candidateManifestSha256 = digest(manifestBytes);
  const candidateRootIdentity = digest(Buffer.from(candidateManifestRecords(records), "utf8"));
  const inputManifestSha256 = digest(Buffer.from(inputManifestRecords(records), "utf8"));
  const receipt: Record<string, unknown> = {
    formatVersion: "cloud-agents-g-contract-external-consumer-projection-receipt/v3",
    decisionId: source.decisionId,
    profileId: source.profileId,
    status: "CURRENT",
    sourceBinding,
    contractSuccessorBinding: source.contractSuccessor,
    candidateBinding: {
      manifestPath: manifestPath,
      manifestSha256: candidateManifestSha256,
      rootIdentity: candidateRootIdentity,
      candidateFileCount: manifest.candidateFileCount,
      candidateEntryCount: manifest.candidateEntryCount,
      manifestAlgorithm: manifest.manifestAlgorithm,
    },
    projection: {
      archivePath: PROJECTION_ARCHIVE_PATH,
      archiveSha256,
      archiveSizeBytes: archive.byteLength,
      memberManifestPath: PROJECTION_MEMBER_MANIFEST_PATH,
      memberManifestSha256,
      memberCount: decoded.length,
      memberManifestAlgorithm: EXTERNAL_CONSUMER_V3_MEMBER_MANIFEST_ALGORITHM,
      inputManifestAlgorithm: EXTERNAL_CONSUMER_V3_INPUT_MANIFEST_ALGORITHM,
      inputManifest: { sha256: inputManifestSha256, memberCount: records.length },
      pathOrdering: "UTF8_BYTE_LEXICOGRAPHIC",
      archiveFormat: "ustar",
      compression: "none",
      tar: {
        mtimeEpochSeconds: 0,
        uid: 0,
        gid: 0,
        uname: "",
        gname: "",
        paxHeaders: "forbidden",
        duplicateEntries: "forbidden",
      },
    },
    runner: {
      path: EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
      entrypoint:
        "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --projection --candidate-root <candidate-root> --candidate-manifest <candidate-manifest> --output-root <output-root>",
    },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  validateProjectionReceipt(authorityRoot, receipt);
  const outputDirectory = containedOutputDirectory(outputRoot);
  const outputFiles = [
    [PROJECTION_ARCHIVE_PATH, archive],
    [PROJECTION_MEMBER_MANIFEST_PATH, memberManifestBytes],
    [PROJECTION_RECEIPT_PATH, Buffer.from(`${JSON.stringify(receipt, null, 2)}\n`, "utf8")],
  ] as const;
  for (const [path] of outputFiles) {
    if (lstatSync(join(outputRoot, path), { throwIfNoEntry: false }) !== undefined) {
      throw new Error(`Projection output already exists: ${path}`);
    }
  }
  const temporaryDirectory = mkdtempSync(join(outputDirectory, ".ec3-projection-"));
  try {
    for (const [path, bytes] of outputFiles) {
      const temporaryPath = join(
        temporaryDirectory,
        path.slice(PROJECTION_OUTPUT_DIRECTORY.length + 1),
      );
      writeFileSync(temporaryPath, bytes, { flag: "wx", mode: 0o644 });
    }
    for (const [path] of outputFiles) {
      const temporaryPath = join(
        temporaryDirectory,
        path.slice(PROJECTION_OUTPUT_DIRECTORY.length + 1),
      );
      renameSync(temporaryPath, join(outputRoot, path));
    }
  } finally {
    rmSync(temporaryDirectory, { recursive: true, force: true });
  }
  return {
    receipt,
    archivePath: join(outputRoot, PROJECTION_ARCHIVE_PATH),
    memberManifestPath: join(outputRoot, PROJECTION_MEMBER_MANIFEST_PATH),
    receiptPath: join(outputRoot, PROJECTION_RECEIPT_PATH),
    archiveSha256,
    memberManifestSha256,
    candidateManifestSha256,
    candidateRootIdentity,
    memberCount: decoded.length,
  };
}

function canonicalRoot(value: string, label: string): string {
  if (!isAbsolute(value)) throw new Error(`${label} must be absolute.`);
  const real = realpathSync(value);
  if (!lstatSync(real).isDirectory()) throw new Error(`${label} must be a canonical directory.`);
  return real;
}

function canonicalFile(value: string, label: string): string {
  if (!isAbsolute(value)) throw new Error(`${label} must be absolute.`);
  const real = realpathSync(value);
  if (!lstatSync(real).isFile()) throw new Error(`${label} must be a canonical file.`);
  return real;
}

function isContained(parent: string, child: string): boolean {
  const relation = relative(parent, child);
  return (
    relation === "" ||
    (!isAbsolute(relation) && relation !== ".." && !relation.startsWith(`..${sep}`))
  );
}

function containedOutputDirectory(root: string): string {
  const directory = join(root, PROJECTION_OUTPUT_DIRECTORY);
  mkdirSync(directory, { recursive: true, mode: 0o755 });
  const replayDirectory = realpathSync(directory);
  if (replayDirectory !== directory)
    throw new Error("Projection output directory must not resolve through a symlink.");
  return replayDirectory;
}

function readCandidateManifest(
  path: string,
  candidateRoot: string,
): { manifest: CandidateManifest; manifestBytes: Buffer; records: readonly CandidateRecord[] } {
  const bytes = readFileSync(path);
  const value = JSON.parse(bytes.toString("utf8")) as Record<string, unknown>;
  if (
    value.formatVersion !== EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_FORMAT ||
    value.source !== "frozen-external-candidate-tree" ||
    value.candidateRoot !== candidateRoot ||
    value.manifestAlgorithm !== EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_ALGORITHM ||
    !Array.isArray(value.records) ||
    value.candidateFileCount !== value.records.length ||
    value.candidateEntryCount !== value.records.length
  ) {
    throw new Error("Candidate manifest authority is invalid or does not bind the candidate root.");
  }
  const records = value.records.map((entry, index) => candidateRecord(entry, index));
  for (let index = 1; index < records.length; index += 1) {
    if (compareExternalConsumerV3UstarPath(records[index - 1]!.path, records[index]!.path) >= 0) {
      throw new Error("Candidate manifest paths must be unique UTF-8 bytewise sorted records.");
    }
  }
  return {
    manifest: value as unknown as CandidateManifest,
    manifestBytes: bytes,
    records,
  };
}

function candidateRecord(value: unknown, index: number): CandidateRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`Candidate manifest record ${index} is not an object.`);
  }
  const record = value as Record<string, unknown>;
  if (
    typeof record.path !== "string" ||
    record.kind !== "file" ||
    (record.mode !== "100644" && record.mode !== "100755") ||
    typeof record.sizeBytes !== "number" ||
    !Number.isSafeInteger(record.sizeBytes) ||
    record.sizeBytes < 0 ||
    typeof record.sha256 !== "string" ||
    !/^sha256:[0-9a-f]{64}$/u.test(record.sha256)
  ) {
    throw new Error(`Candidate manifest record ${index} is invalid.`);
  }
  validateRelativePath(record.path);
  return record as CandidateRecord;
}

function assertCandidateGitInventory(root: string, records: readonly CandidateRecord[]): void {
  const actual = execFileSync("git", ["-C", root, "ls-files", "-co", "--exclude-standard", "-z"], {
    cwd: root,
    env: {
      ...process.env,
      GIT_CONFIG_NOSYSTEM: "1",
      GIT_CONFIG_GLOBAL: "/dev/null",
      GIT_NO_REPLACE_OBJECTS: "1",
      GIT_OPTIONAL_LOCKS: "0",
    },
  })
    .toString("utf8")
    .split("\0")
    .filter(Boolean)
    .sort(compareExternalConsumerV3UstarPath);
  const declared = records.map((record) => record.path);
  if (JSON.stringify(actual) !== JSON.stringify(declared)) {
    throw new Error("Candidate Git inventory differs from the frozen manifest.");
  }
}

function assertCandidateOutputsAbsent(root: string): void {
  for (const path of [V3_PROFILE_PATH, ...V3_RECEIPT_PATHS]) {
    if (lstatSync(join(root, path), { throwIfNoEntry: false }) !== undefined) {
      throw new Error(`Candidate replay output must be absent before projection: ${path}`);
    }
  }
}

function assertRecordBytes(root: string, record: CandidateRecord): void {
  const result = readStableFile(root, record.path);
  if (
    result.mode !== record.mode ||
    result.bytes.byteLength !== record.sizeBytes ||
    digest(result.bytes) !== record.sha256
  ) {
    throw new Error(`Candidate file drifted: ${record.path}`);
  }
}

type Snapshot = Readonly<{ record: CandidateRecord; bytes: Buffer }>;

function readCandidateInputs(
  root: string,
  records: readonly CandidateRecord[],
): readonly Snapshot[] {
  return records.map((record) => {
    const result = readStableFile(root, record.path);
    if (
      result.mode !== record.mode ||
      result.bytes.byteLength !== record.sizeBytes ||
      digest(result.bytes) !== record.sha256
    ) {
      throw new Error(`Candidate file drifted: ${record.path}`);
    }
    return { record, bytes: result.bytes };
  });
}

function sameSnapshot(left: readonly Snapshot[], right: readonly Snapshot[]): boolean {
  return (
    left.length === right.length &&
    left.every(
      (entry, index) =>
        entry.record.path === right[index]!.record.path &&
        entry.record.sha256 === right[index]!.record.sha256 &&
        Buffer.compare(entry.bytes, right[index]!.bytes) === 0,
    )
  );
}

function projectionEntries(selected: readonly Snapshot[]): ExternalConsumerV3UstarInput[] {
  const files = new Map<string, ExternalConsumerV3UstarInput>();
  const directories = new Set<string>();
  for (const { record, bytes } of selected) {
    if (files.has(record.path) || directories.has(record.path))
      throw new Error(`Candidate path collision: ${record.path}`);
    files.set(record.path, {
      path: record.path,
      type: "regular",
      mode: record.mode === "100755" ? 0o755 : 0o644,
      data: Uint8Array.from(bytes),
    });
    const parts = record.path.split("/");
    for (let index = 1; index < parts.length; index += 1) {
      const directory = parts.slice(0, index).join("/");
      if (files.has(directory))
        throw new Error(`Candidate file is also a parent directory: ${directory}`);
      directories.add(directory);
    }
  }
  return [
    ...[...directories].sort(compareExternalConsumerV3UstarPath).map((path) => ({
      path,
      type: "directory" as const,
      mode: 0o755 as const,
    })),
    ...files.values(),
  ];
}

function serializeMemberManifest(
  members: ReturnType<typeof decodeExternalConsumerV3Ustar>,
): Buffer {
  const body = {
    formatVersion: "cloud-agents-g-contract-external-consumer-member-manifest/v1",
    algorithm: EXTERNAL_CONSUMER_V3_MEMBER_MANIFEST_ALGORITHM,
    ordering: "UTF8_BYTE_LEXICOGRAPHIC",
    framing: "NUL_RECORDS",
    recordFields: ["path", "type", "mode", "sizeBytes", "sha256", "linkTarget"],
    members: members.map((member) => ({
      path: member.path,
      type: member.type,
      mode: `100${member.mode.toString(8)}`,
      sizeBytes: member.size,
      sha256: member.sha256,
      linkTarget: member.linkTarget,
    })),
  };
  return Buffer.from(`${JSON.stringify(body)}\n`, "utf8");
}

function candidateManifestRecords(records: readonly CandidateRecord[]): string {
  return records
    .map(
      (record) =>
        `${record.path}\0${record.kind}\0${record.mode}\0${record.sizeBytes}\0${record.sha256}\0`,
    )
    .join("");
}

function inputManifestRecords(records: readonly CandidateRecord[]): string {
  return records
    .map((record) => `${record.path}\0${record.mode}\0${record.sizeBytes}\0${record.sha256}\0`)
    .join("");
}

function validateProjectionReceipt(root: string, receipt: Record<string, unknown>): void {
  const schema = readJsonFile(join(root, EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH));
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, receipt))
    throw new Error(`Projection receipt schema validation failed: ${ajv.errorsText()}`);
}

function readJsonFile(path: string): unknown {
  return JSON.parse(readFileSync(path, "utf8"));
}

function readStableFile(root: string, path: string): { bytes: Buffer; mode: "100644" | "100755" } {
  validateRelativePath(path);
  const rootReal = realpathSync(root);
  const target = resolve(rootReal, path);
  const relation = relative(rootReal, target);
  if (relation === "" || relation.startsWith(`..${sep}`) || isAbsolute(relation))
    throw new Error(`Path escapes root: ${path}`);
  let cursor = rootReal;
  for (const component of path.split("/")) {
    cursor = join(cursor, component);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (!stat || stat.isSymbolicLink())
      throw new Error(`Candidate path is missing or symlinked: ${path}`);
  }
  const before = lstatSync(target);
  if (!before.isFile() || before.isSymbolicLink())
    throw new Error(`Candidate path is not a regular file: ${path}`);
  const fd = openSync(target, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const bytes = readFileSync(fd);
    const descriptor = fstatSync(fd);
    const after = lstatSync(target);
    if (
      !descriptor.isFile() ||
      !after.isFile() ||
      after.isSymbolicLink() ||
      descriptor.size !== before.size ||
      after.size !== before.size ||
      descriptor.mtimeMs !== before.mtimeMs ||
      after.mtimeMs !== before.mtimeMs
    ) {
      throw new Error(`Candidate path changed while reading: ${path}`);
    }
    const mode = (before.mode & 0o111) === 0 ? "100644" : "100755";
    return { bytes, mode };
  } finally {
    closeSync(fd);
  }
}

function validateRelativePath(path: string): void {
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.includes("\0") ||
    path.split("/").some((part) => part.length === 0 || part === "." || part === "..")
  ) {
    throw new Error(`Non-canonical candidate path: ${path}`);
  }
}

function digest(bytes: Uint8Array): Digest {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
