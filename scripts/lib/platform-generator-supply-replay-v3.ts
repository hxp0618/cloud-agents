import { createHash } from "node:crypto";
import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
} from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_REPLAY_AUTHORITY_FILES,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag-v3";
import {
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
} from "./platform-successor-predecessor";

const SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0];
const SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.slice(1);

type Platform = "darwin-arm64" | "linux-amd64";
type Run = "A" | "B";

export type GeneratorSupplyReplayV3AuthorityFile = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
}>;

export type GeneratorSupplyReplayV3Contract = Readonly<{
  authorityFiles: Readonly<{
    wrapper: GeneratorSupplyReplayV3AuthorityFile;
    runner: GeneratorSupplyReplayV3AuthorityFile;
    pathHelper: GeneratorSupplyReplayV3AuthorityFile;
    archiveInspector: GeneratorSupplyReplayV3AuthorityFile;
  }>;
  coreGeneratorOutputs: readonly Readonly<{
    path: string;
    mode: "100644";
    gitBlob: string;
    sha256: string;
    sizeBytes: number;
  }>[];
  preReplayExclusionPolicy: string;
  wrapperPolicy: string;
  authoritativeReplayScope: string;
  algorithms: Readonly<{
    nodeModulesManifest: string;
    projectionArchiveMemberManifest: string;
    inputTreeManifest: string;
  }>;
  projectionExclusions: readonly string[];
  receiptFormats: Readonly<{
    summary: string;
    run: string;
    isolation: string;
    projection: string;
  }>;
}>;

export const GENERATOR_SUPPLY_V3_REPLAY_AUTHORITY_PATHS = SUCCESSOR_V3_REPLAY_AUTHORITY_FILES;

export type GeneratorSupplyReplayV3Projection = Readonly<{
  projectionTreeSha: string;
  projectionArchiveSha256: string;
  projectionArchiveSizeBytes: number;
  projectionArchiveMemberManifestAlgorithm: string;
  projectionArchiveMemberManifestSha256: string;
  projectionArchiveMembers: number;
  inputTreeManifestAlgorithm: string;
  inputTreeManifestSha256: string;
  inputTreeFiles: number;
}>;

export type GeneratorSupplyReplayV3PlatformMaterial = Readonly<{
  nodeModulesManifestSha256: string;
  nodeModulesFiles: number;
  wheelhouseManifestSha256: string;
  externalExecutableSetSha256: string;
  loadedOxfmtBinding: string;
  versions: Readonly<{
    node: string;
    bun: string;
    go: string;
    python: string;
    uv: string;
    protoc: string;
    protocGenGo: string;
    protocGenConnectGo: string;
  }>;
}>;

export type GeneratorSupplyReplayV3Expected = Readonly<{
  replayContract: GeneratorSupplyReplayV3Contract;
  predecessorProjectionTreeSha: string;
  platforms: Readonly<Record<Platform, GeneratorSupplyReplayV3PlatformMaterial>>;
  linuxRootfs: Readonly<{
    registryIndexDigest: string;
    platformManifestDigest: string;
    configImageId: string;
    rootfsLayerDigest: string;
    exportTarSha256: string;
    exportTarSizeBytes: number;
    inspectionManifestSha256: string;
    entries: number;
    regularFiles: number;
    directories: number;
    symlinks: number;
    hardlinks: number;
  }>;
}>;

export type GeneratorSupplyReplayV3Validation = Readonly<{
  receiptSha256: Readonly<Record<(typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number], string>>;
  receiptRecords: readonly Readonly<{ path: string; sha256: string; sizeBytes: number }>[];
  projection: GeneratorSupplyReplayV3Projection;
  candidateManifestSha256: string;
  assertSnapshotCurrent: () => void;
}>;

export type GeneratorSupplyReplayV3PreparedReceipt = Readonly<{
  path: (typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number];
  sha256: string;
  sizeBytes: number;
}>;

export type GeneratorSupplyReplayV3PreparedReceipts = Readonly<{
  receipts: ReadonlyMap<(typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number], Buffer>;
  receiptRecords: readonly GeneratorSupplyReplayV3PreparedReceipt[];
  projection: GeneratorSupplyReplayV3Projection;
  candidateManifestSha256: string;
  outputFiles: number;
  assertInputSnapshotCurrent: () => void;
  assertPreparedSnapshotCurrent: () => void;
}>;

export class GeneratorSupplyReplayV3Error extends Error {
  readonly code = "GENERATOR_SUPPLY_REPLAY_V3_INVALID";

  constructor(
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyReplayV3Error";
  }
}

export function assertGeneratorSupplyReplayV3ContractCurrent(
  root: string,
  contract: GeneratorSupplyReplayV3Contract,
): void {
  validateContract(contract);
  const identities: StableFileIdentity[] = [];
  validateAuthorityFiles(root, contract, identities);
  assertStableSnapshotCurrent(root, identities);
}

type StableFileIdentity = Readonly<{
  rootReal: string;
  path: string;
  absolute: string;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

type GeneratorSupplyV2DerivedFileRecord = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
}>;

type GeneratorSupplyV2DerivedReadMutation = {
  readonly path: string;
  readonly beforeDerivedRead: () => void;
  readonly afterDerivedRead: () => void;
  fired: boolean;
};

type ReadReceipt = Readonly<{
  path: string;
  bytes: Buffer;
  sha256: string;
  value: JsonRecord;
  identity?: StableFileIdentity;
}>;

const RUN_KEYS = [
  "formatVersion",
  "platform",
  "replayRun",
  "manifestAlgorithm",
  "perCommandTimeoutMilliseconds",
  "archiveHasGitDirectory",
  "projectionTreeSha",
  "projectionArchiveSha256",
  "projectionArchiveSizeBytes",
  "projectionArchiveMemberManifestAlgorithm",
  "projectionArchiveMemberManifestSha256",
  "projectionArchiveMembers",
  "inputTreeManifestAlgorithm",
  "inputTreeManifestSha256",
  "inputTreeFiles",
  "freshExtractionRoot",
  "extractionRootInitiallyAbsent",
  "ambientNodeModules",
  "nodeModulesManifestSha256",
  "nodeModulesFiles",
  "wheelhouseManifestSha256",
  "externalExecutableSetSha256",
  "isolation",
  "isolationEvidenceAuthority",
  "environmentPolicy",
  "runnerEnvironmentPolicy",
  "runnerEnvironmentSanitized",
  "freshPerReplayCaches",
  "ephemeralCachePolicy",
  "homeDirectory",
  "temporaryDirectory",
  "uvCacheDirectory",
  "xdgCacheHome",
  "versions",
  "loadedOxfmtBinding",
  "candidateManifestSha256",
  "replayManifestSha256",
  "outputFiles",
  "candidateOutputsEqual",
  "nonAllowlistedChanges",
  "replayAuthoritySha256",
] as const;

const PROJECTION_KEYS = [
  "projectionTreeSha",
  "projectionArchiveSha256",
  "projectionArchiveSizeBytes",
  "projectionArchiveMemberManifestAlgorithm",
  "projectionArchiveMemberManifestSha256",
  "projectionArchiveMembers",
  "inputTreeManifestAlgorithm",
  "inputTreeManifestSha256",
  "inputTreeFiles",
] as const;

const VERSIONS_KEYS = [
  "node",
  "bun",
  "go",
  "python",
  "uv",
  "protoc",
  "protocGenGo",
  "protocGenConnectGo",
] as const;

const AUTHORITY_KEYS = ["wrapper", "runner", "pathHelper", "archiveInspector"] as const;
const PROBE_KEYS = ["command", "exitCode", "stdout", "stderr"] as const;

/**
 * Reads every one of the exact eight late-bound receipts once, then validates
 * their complete closed semantic graph. Hashes are always computed from those
 * stable-read bytes; validation never reopens a receipt.
 */
export function assertGeneratorSupplyReplayV3Receipts(
  root: string,
  expected: GeneratorSupplyReplayV3Expected,
): GeneratorSupplyReplayV3Validation {
  return assertGeneratorSupplyReplayV3ReceiptsInternal(root, expected);
}

/**
 * Validates the exact seven caller-owned native replay receipts, derives the
 * canonical summary, and returns the complete ordered eight-receipt set.
 * Each caller Buffer is immediately copied into an independent stable
 * snapshot. Public map reads return fresh copies, never the internal authority.
 */
export function buildGeneratorSupplyReplayV3PreparedReceipts(
  root: string,
  replayContract: GeneratorSupplyReplayV3Contract,
  rawReceiptBytes: ReadonlyMap<string, Buffer>,
): GeneratorSupplyReplayV3PreparedReceipts {
  const rawSnapshots = snapshotRawReceiptBytes(rawReceiptBytes);
  validateContract(replayContract);
  const inputIdentities: StableFileIdentity[] = [];
  validateAuthorityFiles(root, replayContract, inputIdentities);
  const expected = buildGeneratorSupplyReplayV3ExpectedFromImmutableV2Internal(
    root,
    replayContract,
    inputIdentities,
  );
  validateExpected(expected);

  const receipts = new Map<string, ReadReceipt>();
  for (const path of SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS) {
    const bytes = rawSnapshots.get(path);
    if (!bytes) fail(`/${path}`, "Stable raw receipt snapshot is absent.");
    receipts.set(path, parseReceiptBytes(path, bytes));
  }
  const rawGraph = validateReceiptGraph(receipts, expected, "derive-summary");
  const summaryValue = buildSummary(receipts, expected, rawGraph.projection, rawGraph.output);
  const summaryBytes = serializeDerivedSummary(summaryValue);
  receipts.set(
    SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH,
    parseReceiptBytes(SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH, summaryBytes),
  );
  const validatedGraph = validateReceiptGraph(receipts, expected, "require-summary");
  const completeGraph: ReceiptGraphValidation = Object.freeze({
    projection: Object.freeze({ ...validatedGraph.projection }),
    output: Object.freeze({ ...validatedGraph.output }),
  });
  const assertInputSnapshotCurrent = (): void => assertStableSnapshotCurrent(root, inputIdentities);
  const assertPreparedSnapshotCurrent = (): void => {
    assertInputSnapshotCurrent();
    assertPreparedReceiptSetCurrent(receipts, expected, completeGraph);
  };
  assertPreparedSnapshotCurrent();

  const get = receiptGetter(receipts);
  const publicReceipts = Object.freeze(
    new CopyingReadonlyBufferMap(
      SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => [path, get(path).bytes] as const),
    ),
  );
  const receiptRecords = Object.freeze(
    SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) =>
      Object.freeze({
        path,
        sha256: get(path).sha256,
        sizeBytes: get(path).bytes.byteLength,
      }),
    ),
  );
  return Object.freeze({
    receipts: publicReceipts,
    receiptRecords,
    projection: completeGraph.projection,
    candidateManifestSha256: completeGraph.output.candidateManifestSha256,
    outputFiles: completeGraph.output.outputFiles,
    assertInputSnapshotCurrent,
    assertPreparedSnapshotCurrent,
  });
}

export function assertGeneratorSupplyReplayV3SnapshotMutationForTest(
  root: string,
  expected: GeneratorSupplyReplayV3Expected,
  mutateAfterSnapshot: () => void,
): void {
  assertGeneratorSupplyReplayV3ReceiptsInternal(root, expected, {
    phase: "receipts",
    mutate: mutateAfterSnapshot,
  });
}

export function assertGeneratorSupplyReplayV3InputSnapshotMutationForTest(
  root: string,
  expected: GeneratorSupplyReplayV3Expected,
  phase: "authority" | "v1",
  mutateAfterSnapshot: () => void,
): void {
  assertGeneratorSupplyReplayV3ReceiptsInternal(root, expected, {
    phase,
    mutate: mutateAfterSnapshot,
  });
}

function assertGeneratorSupplyReplayV3ReceiptsInternal(
  root: string,
  expected: GeneratorSupplyReplayV3Expected,
  mutation?: Readonly<{
    phase: "authority" | "v1" | "receipts";
    mutate: () => void;
  }>,
): GeneratorSupplyReplayV3Validation {
  validateExpected(expected);
  const inputIdentities: StableFileIdentity[] = [];
  validateAuthorityFiles(root, expected.replayContract, inputIdentities);
  if (mutation?.phase === "authority") mutation.mutate();
  const currentExpected = buildGeneratorSupplyReplayV3ExpectedFromImmutableV2Internal(
    root,
    expected.replayContract,
    inputIdentities,
  );
  if (mutation?.phase === "v1") mutation.mutate();
  if (!canonicalEqual(currentExpected, expected)) {
    fail(
      "/expected",
      "Replay expectations do not match the same stable snapshot of immutable v2 material.",
    );
  }
  const receipts = new Map<string, ReadReceipt>();
  for (const path of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS) {
    const snapshot = readContainedRegularFileSnapshot(root, path);
    receipts.set(path, { ...parseReceiptBytes(path, snapshot.bytes), identity: snapshot.identity });
  }
  if (mutation?.phase === "receipts") mutation.mutate();
  const graph = validateReceiptGraph(receipts, expected, "require-summary");
  const get = receiptGetter(receipts);
  const snapshotIdentities = [
    ...inputIdentities,
    ...SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => {
      const identity = get(path).identity;
      if (!identity) fail(`/${path}`, "Receipt snapshot identity is absent.");
      return identity;
    }),
  ];
  const assertSnapshotCurrent = (): void => assertStableSnapshotCurrent(root, snapshotIdentities);
  assertSnapshotCurrent();

  return {
    receiptSha256: Object.fromEntries(
      SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => [path, get(path).sha256]),
    ) as Record<(typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number], string>,
    receiptRecords: SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => ({
      path,
      sha256: get(path).sha256,
      sizeBytes: get(path).bytes.byteLength,
    })),
    projection: graph.projection,
    candidateManifestSha256: graph.output.candidateManifestSha256,
    assertSnapshotCurrent,
  };
}

type ReceiptGraphValidation = Readonly<{
  projection: GeneratorSupplyReplayV3Projection;
  output: Readonly<{ candidateManifestSha256: string; outputFiles: number }>;
}>;

function validateReceiptGraph(
  receipts: ReadonlyMap<string, ReadReceipt>,
  expected: GeneratorSupplyReplayV3Expected,
  summaryPolicy: "derive-summary" | "require-summary",
): ReceiptGraphValidation {
  const get = receiptGetter(receipts);
  const [
    summaryPath,
    darwinAPath,
    darwinBPath,
    darwinIsolationPath,
    linuxAPath,
    linuxBPath,
    linuxIsolationPath,
    projectionPath,
  ] = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS;
  const requiredPaths =
    summaryPolicy === "require-summary"
      ? SUCCESSOR_V3_REPLAY_RECEIPT_PATHS
      : SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS;
  assertExactReceiptMapKeys(receipts, requiredPaths);

  const projection = validateProjection(get(projectionPath), expected);
  const runs = {
    "darwin-arm64": [
      validateRun(get(darwinAPath), "darwin-arm64", "A", expected, projection),
      validateRun(get(darwinBPath), "darwin-arm64", "B", expected, projection),
    ],
    "linux-amd64": [
      validateRun(get(linuxAPath), "linux-amd64", "A", expected, projection),
      validateRun(get(linuxBPath), "linux-amd64", "B", expected, projection),
    ],
  } as const;
  for (const [platform, pair] of Object.entries(runs) as [Platform, readonly JsonRecord[]][]) {
    if (!canonicalEqual(stableRun(pair[0]!), stableRun(pair[1]!)))
      fail(`/replay/${platform}`, "A/B reports differ outside the exact run-specific fields.");
  }
  const allRuns = [...runs["darwin-arm64"], ...runs["linux-amd64"]];
  const output = {
    candidateManifestSha256: String(allRuns[0]!.candidateManifestSha256),
    outputFiles: Number(allRuns[0]!.outputFiles),
  };
  for (const report of allRuns) {
    if (
      !canonicalEqual(projectionFrom(report), projection) ||
      report.candidateManifestSha256 !== output.candidateManifestSha256 ||
      report.replayManifestSha256 !== output.candidateManifestSha256 ||
      report.outputFiles !== output.outputFiles
    )
      fail("/replay/runs", "All platforms must bind the fixed projection and identical output.");
  }
  validateIsolation(
    get(darwinIsolationPath),
    "darwin-arm64",
    [get(darwinAPath), get(darwinBPath)],
    expected,
    projection,
  );
  validateIsolation(
    get(linuxIsolationPath),
    "linux-amd64",
    [get(linuxAPath), get(linuxBPath)],
    expected,
    projection,
  );
  if (summaryPolicy === "require-summary")
    validateSummary(get(summaryPath), receipts, expected, projection, output);
  return { projection, output };
}

function parseReceiptBytes(path: string, bytes: Buffer): ReadReceipt {
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch (error) {
    fail(`/${path}`, `Receipt is not valid JSON: ${String(error)}.`);
  }
  return {
    path,
    bytes,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    value: object(parsed, `/${path}`),
  };
}

function receiptGetter(
  receipts: ReadonlyMap<string, ReadReceipt>,
): (path: (typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number]) => ReadReceipt {
  return (path) => {
    const receipt = receipts.get(path);
    if (!receipt) fail(`/${path}`, "Exact receipt is absent from the fixed receipt set.");
    return receipt;
  };
}

function assertExactRawReceiptByteSet(rawReceiptBytes: ReadonlyMap<string, Buffer>): void {
  assertExactReceiptMapKeys(rawReceiptBytes, SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS);
}

function snapshotRawReceiptBytes(
  rawReceiptBytes: ReadonlyMap<string, Buffer>,
): ReadonlyMap<(typeof SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS)[number], Buffer> {
  assertExactRawReceiptByteSet(rawReceiptBytes);
  const snapshots = new Map<(typeof SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS)[number], Buffer>();
  for (const path of SUCCESSOR_V3_RAW_REPLAY_RECEIPT_PATHS) {
    const callerBytes = rawReceiptBytes.get(path);
    if (!Buffer.isBuffer(callerBytes)) fail(`/${path}`, "Raw receipt value must be a Buffer.");
    if (typeof SharedArrayBuffer !== "undefined" && callerBytes.buffer instanceof SharedArrayBuffer)
      fail(`/${path}`, "SharedArrayBuffer-backed raw receipt bytes are not stable authority.");
    const snapshot = Buffer.allocUnsafeSlow(callerBytes.byteLength);
    const copied = Buffer.prototype.copy.call(callerBytes, snapshot, 0, 0, callerBytes.byteLength);
    if (copied !== callerBytes.byteLength)
      fail(`/${path}`, "Raw receipt bytes changed while the stable snapshot was copied.");
    snapshots.set(path, snapshot);
  }
  return snapshots;
}

function assertPreparedReceiptSetCurrent(
  receipts: ReadonlyMap<string, ReadReceipt>,
  expected: GeneratorSupplyReplayV3Expected,
  fixedGraph: ReceiptGraphValidation,
): void {
  assertExactReceiptMapKeys(receipts, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS);
  const get = receiptGetter(receipts);
  for (const path of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS) {
    const receipt = get(path);
    const currentSha256 = `sha256:${createHash("sha256").update(receipt.bytes).digest("hex")}`;
    if (currentSha256 !== receipt.sha256)
      fail(`/${path}`, "Prepared receipt bytes changed after stable capture.");
    const current = parseReceiptBytes(path, receipt.bytes);
    if (!canonicalEqual(current.value, receipt.value))
      fail(`/${path}`, "Prepared receipt bytes and semantic value are no longer identical.");
    if (
      path === SUCCESSOR_V3_DERIVED_REPLAY_SUMMARY_PATH &&
      !receipt.bytes.equals(serializeDerivedSummary(receipt.value))
    )
      fail(`/${path}`, "Derived summary bytes are no longer canonical two-space JSON.");
  }
  const currentGraph = validateReceiptGraph(receipts, expected, "require-summary");
  if (!canonicalEqual(currentGraph, fixedGraph))
    fail("/receipts", "Prepared receipt graph changed after stable capture.");
}

class CopyingReadonlyBufferMap<K extends string> implements ReadonlyMap<K, Buffer> {
  readonly #bytes: ReadonlyMap<K, Buffer>;

  constructor(entries: readonly (readonly [K, Buffer])[]) {
    this.#bytes = new Map(entries);
  }

  get size(): number {
    return this.#bytes.size;
  }

  get(key: K): Buffer | undefined {
    const bytes = this.#bytes.get(key);
    return bytes === undefined ? undefined : Buffer.from(bytes);
  }

  has(key: K): boolean {
    return this.#bytes.has(key);
  }

  *entries(): MapIterator<[K, Buffer]> {
    for (const [path, bytes] of this.#bytes) yield [path, Buffer.from(bytes)];
  }

  keys(): MapIterator<K> {
    return this.#bytes.keys();
  }

  *values(): MapIterator<Buffer> {
    for (const bytes of this.#bytes.values()) yield Buffer.from(bytes);
  }

  forEach(
    callbackfn: (value: Buffer, key: K, map: ReadonlyMap<K, Buffer>) => void,
    thisArg?: unknown,
  ): void {
    for (const [path, bytes] of this.#bytes)
      callbackfn.call(thisArg, Buffer.from(bytes), path, this);
  }

  [Symbol.iterator](): MapIterator<[K, Buffer]> {
    return this.entries();
  }

  readonly [Symbol.toStringTag] = "CopyingReadonlyBufferMap";
}

function assertExactReceiptMapKeys(
  receipts: ReadonlyMap<string, unknown>,
  expectedPaths: readonly string[],
): void {
  const actual = [...receipts.keys()].toSorted((left, right) =>
    Buffer.compare(Buffer.from(left), Buffer.from(right)),
  );
  const expected = [...expectedPaths].toSorted((left, right) =>
    Buffer.compare(Buffer.from(left), Buffer.from(right)),
  );
  if (!canonicalEqual(actual, expected))
    fail("/receipts", "Receipt paths must be the exact closed path set.");
}

function serializeDerivedSummary(value: JsonRecord): Buffer {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}

function validateRun(
  receipt: ReadReceipt,
  platform: Platform,
  run: Run,
  expected: GeneratorSupplyReplayV3Expected,
  projection: GeneratorSupplyReplayV3Projection,
): JsonRecord {
  const path = `/${receipt.path}`;
  const report = receipt.value;
  exactKeys(report, path, RUN_KEYS);
  const versions = object(report.versions, `${path}/versions`);
  exactKeys(versions, `${path}/versions`, VERSIONS_KEYS);
  const authority = object(report.replayAuthoritySha256, `${path}/replayAuthoritySha256`);
  exactKeys(authority, `${path}/replayAuthoritySha256`, AUTHORITY_KEYS);
  const material = expected.platforms[platform];
  const lowerRun = run.toLowerCase();
  const expectedFields: JsonRecord = {
    formatVersion: expected.replayContract.receiptFormats.run,
    platform,
    replayRun: run,
    manifestAlgorithm: expected.replayContract.algorithms.nodeModulesManifest,
    perCommandTimeoutMilliseconds: 600_000,
    archiveHasGitDirectory: false,
    freshExtractionRoot: `generator-supply://core-projection/${lowerRun}`,
    extractionRootInitiallyAbsent: true,
    ambientNodeModules: false,
    nodeModulesManifestSha256: material.nodeModulesManifestSha256,
    nodeModulesFiles: material.nodeModulesFiles,
    wheelhouseManifestSha256: material.wheelhouseManifestSha256,
    externalExecutableSetSha256: material.externalExecutableSetSha256,
    isolation:
      platform === "darwin-arm64"
        ? "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES"
        : "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS",
    isolationEvidenceAuthority: "VERSIONED_WRAPPER_SAME_BOUNDARY_RECEIPT",
    environmentPolicy: "MINIMAL_EXACT_V1",
    runnerEnvironmentPolicy: "ENV_I_MINIMAL_V1",
    runnerEnvironmentSanitized: true,
    freshPerReplayCaches: true,
    ephemeralCachePolicy:
      platform === "darwin-arm64"
        ? "FRESH_PER_REPLAY_SANDBOX_TASK_OWNED"
        : "FRESH_PER_REPLAY_TMPFS_ONLY",
    homeDirectory: `generator-supply://ephemeral/${lowerRun}/home`,
    temporaryDirectory: `generator-supply://ephemeral/${lowerRun}/tmp`,
    uvCacheDirectory: `generator-supply://ephemeral/${lowerRun}/uv-cache`,
    xdgCacheHome: `generator-supply://ephemeral/${lowerRun}/xdg-cache`,
    loadedOxfmtBinding: material.loadedOxfmtBinding,
    candidateOutputsEqual: true,
    nonAllowlistedChanges: 0,
  };
  exactValues(report, path, expectedFields);
  if (!canonicalEqual(versions, material.versions)) {
    fail(`${path}/versions`, "Runtime versions do not match the fixed platform material.");
  }
  if (!canonicalEqual(authority, expectedAuthority(expected.replayContract))) {
    fail(`${path}/replayAuthoritySha256`, "Run report authority digests drifted.");
  }
  digest(report.candidateManifestSha256, `${path}/candidateManifestSha256`);
  if (report.replayManifestSha256 !== report.candidateManifestSha256) {
    fail(
      `${path}/replayManifestSha256`,
      "Candidate and replay output manifests must be identical.",
    );
  }
  positiveInteger(report.outputFiles, `${path}/outputFiles`);
  if (!canonicalEqual(projectionFrom(report), projection)) {
    fail(`${path}/projection`, "Run report projection nine-tuple drifted.");
  }
  return report;
}

function validateProjection(
  receipt: ReadReceipt,
  expected: GeneratorSupplyReplayV3Expected,
): GeneratorSupplyReplayV3Projection {
  const path = `/${receipt.path}`;
  const value = receipt.value;
  exactKeys(value, path, [
    "formatVersion",
    "treeSha",
    "archiveSha256",
    "archiveSizeBytes",
    "archiveInspection",
    "excluded",
  ]);
  const inspection = object(value.archiveInspection, `${path}/archiveInspection`);
  exactKeys(inspection, `${path}/archiveInspection`, [
    "formatVersion",
    "profile",
    "manifestAlgorithm",
    "manifestSha256",
    "entries",
    "regularFiles",
    "directories",
    "symlinks",
    "hardlinks",
    "unsafeEntries",
    "duplicateEntries",
    "specialEntries",
    "linkPrefixDescendants",
    "linkCycles",
    "regularFileManifestAlgorithm",
    "regularFileManifestSha256",
    "reconstructedGitTreeSha",
  ]);
  exactValues(value, path, {
    formatVersion: expected.replayContract.receiptFormats.projection,
    treeSha: value.treeSha,
    archiveSha256: value.archiveSha256,
    archiveSizeBytes: value.archiveSizeBytes,
  });
  exactValues(inspection, `${path}/archiveInspection`, {
    formatVersion: "cloud-agents-generator-replay-archive-inspection/v1",
    profile: "core-projection",
    manifestAlgorithm: expected.replayContract.algorithms.projectionArchiveMemberManifest,
    regularFileManifestAlgorithm: expected.replayContract.algorithms.inputTreeManifest,
    reconstructedGitTreeSha: value.treeSha,
    symlinks: 0,
    hardlinks: 0,
    unsafeEntries: 0,
    duplicateEntries: 0,
    specialEntries: 0,
    linkPrefixDescendants: 0,
    linkCycles: 0,
  });
  positiveInteger(inspection.directories, `${path}/archiveInspection/directories`);
  positiveInteger(value.archiveSizeBytes, `${path}/archiveSizeBytes`);
  positiveInteger(inspection.entries, `${path}/archiveInspection/entries`);
  positiveInteger(inspection.regularFiles, `${path}/archiveInspection/regularFiles`);
  if (
    !/^[0-9a-f]{40}$/u.test(String(value.treeSha)) ||
    value.treeSha === expected.predecessorProjectionTreeSha
  ) {
    fail(
      `${path}/treeSha`,
      "Projection tree must be a valid new tree, not the immutable v2 predecessor.",
    );
  }
  digest(value.archiveSha256, `${path}/archiveSha256`);
  bareDigest(inspection.manifestSha256, `${path}/archiveInspection/manifestSha256`);
  bareDigest(
    inspection.regularFileManifestSha256,
    `${path}/archiveInspection/regularFileManifestSha256`,
  );
  if (!canonicalEqual(value.excluded, expected.replayContract.projectionExclusions)) {
    fail(`${path}/excluded`, "Projection exclusions must be the exact ordered 17-path set.");
  }
  return {
    projectionTreeSha: value.treeSha,
    projectionArchiveSha256: value.archiveSha256,
    projectionArchiveSizeBytes: value.archiveSizeBytes,
    projectionArchiveMemberManifestAlgorithm: inspection.manifestAlgorithm,
    projectionArchiveMemberManifestSha256: `sha256:${String(inspection.manifestSha256)}`,
    projectionArchiveMembers: inspection.entries,
    inputTreeManifestAlgorithm: inspection.regularFileManifestAlgorithm,
    inputTreeManifestSha256: `sha256:${String(inspection.regularFileManifestSha256)}`,
    inputTreeFiles: inspection.regularFiles,
  } as GeneratorSupplyReplayV3Projection;
}

function validateIsolation(
  receipt: ReadReceipt,
  platform: Platform,
  runs: readonly [ReadReceipt, ReadReceipt],
  expected: GeneratorSupplyReplayV3Expected,
  projection: GeneratorSupplyReplayV3Projection,
): void {
  const path = `/${receipt.path}`;
  const value = receipt.value;
  const commonKeys = [
    "formatVersion",
    "platform",
    "executor",
    "mechanism",
    "wrapperPolicy",
    "boundaryModel",
    "wrapperSha256",
    "replayAuthoritySha256",
    "authorityDigestsCapturedBeforeChild",
    "authorityFilesReadOnlyToCandidate",
    "sameBoundaryProbesAndReplay",
    "candidateReportFilesystemAccess",
    "reportsWrittenByTrustedParent",
    "runnerStdoutFrame",
    "runnerChildStdoutRedirectedToStderr",
    "runnerStderrCaptureBoundBytes",
    "runnerEnvironmentPolicy",
    "runnerEnvironmentSanitized",
    "freshPerReplayCaches",
    "extractionRootsInitiallyAbsent",
    "archiveSnapshotValidatedBeforeExtraction",
    "independentArchiveExtractions",
    "runAWriteRootDestroyedBeforeRunB",
    "siblingReplayRootAbsentWithinEachBoundary",
    "finalEvidenceUnavailableWithinEachBoundary",
    "sameProcessGroupEmptyAfterExit",
    "detachedDescendantCrossBoundaryReadDenied",
    "detachedDescendantsCrossRunReadDenied",
    "detachedDescendantResourceLeakNotClaimed",
    "processLifetimeClosure",
    ...PROJECTION_KEYS,
    "reportsEqualInputProjection",
    "runReportSha256",
    "probes",
    "networkDenied",
    "nodeModulesBindingReadOnly",
    "notGateClosure",
  ];
  const platformKeys =
    platform === "darwin-arm64"
      ? [
          "denyDefaultSandbox",
          "writeAuthorityLimitedToDisposableRunRoot",
          "separateProcessGroupPerReplay",
          "externalSupplyReadOnly",
          "projectionArchiveReadOnly",
        ]
      : [
          "separateNetworkMountPidNamespacePerReplay",
          "pidNamespaceKillsDescendants",
          "rootfsFreshExtractionPerReplay",
          "rootfsReadOnly",
          "inputReadOnly",
          "projectionReadOnly",
          "tmpfsTmp",
          "tmpfsEphemeral",
          "tmpfsDeviceTree",
          "candidateUid",
          "candidateGid",
          "candidateSupplementaryGroups",
          "candidateCapabilities",
          "noNewPrivileges",
          "nodeModulesReadOnlyBind",
          "ubuntuRootfs",
        ];
  exactKeys(value, path, [...commonKeys, ...platformKeys]);
  exactValues(value, path, {
    formatVersion: expected.replayContract.receiptFormats.isolation,
    platform,
    executor:
      platform === "darwin-arm64"
        ? "authorized_darwin_arm64_executor"
        : "authorized_linux_amd64_executor",
    mechanism:
      platform === "darwin-arm64"
        ? "SANDBOX_EXEC_DENY_DEFAULT_PER_RUN_BOUNDARY_V1"
        : "UNSHARE_NET_MOUNT_PID_FRESH_ROOTFS_SETPRIV_V1",
    wrapperPolicy: expected.replayContract.wrapperPolicy,
    boundaryModel: "SEPARATE_RUN_BOUNDARY_STDOUT_TRUSTED_PARENT_V1",
    wrapperSha256: prefixed(expected.replayContract.authorityFiles.wrapper.sha256),
    authorityDigestsCapturedBeforeChild: true,
    authorityFilesReadOnlyToCandidate: true,
    sameBoundaryProbesAndReplay: true,
    candidateReportFilesystemAccess: false,
    reportsWrittenByTrustedParent: true,
    runnerStdoutFrame: "CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1_LENGTH_PREFIXED_MAX_1M_RAW_FILE",
    runnerChildStdoutRedirectedToStderr: true,
    runnerStderrCaptureBoundBytes: 1_048_576,
    runnerEnvironmentPolicy: "ENV_I_MINIMAL_V1",
    runnerEnvironmentSanitized: true,
    freshPerReplayCaches: true,
    extractionRootsInitiallyAbsent: true,
    archiveSnapshotValidatedBeforeExtraction: true,
    independentArchiveExtractions: 2,
    runAWriteRootDestroyedBeforeRunB: true,
    siblingReplayRootAbsentWithinEachBoundary: true,
    finalEvidenceUnavailableWithinEachBoundary: true,
    sameProcessGroupEmptyAfterExit: "NOT_CLAIMED",
    detachedDescendantCrossBoundaryReadDenied: platform === "darwin-arm64",
    detachedDescendantsCrossRunReadDenied: false,
    detachedDescendantResourceLeakNotClaimed: platform === "darwin-arm64",
    processLifetimeClosure:
      platform === "darwin-arm64"
        ? "NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL"
        : "PID_NAMESPACE_KILL_CHILD",
    reportsEqualInputProjection: true,
    networkDenied: true,
    nodeModulesBindingReadOnly: true,
    notGateClosure: true,
  });
  if (!canonicalEqual(projectionFrom(value), projection)) {
    fail(`${path}/projection`, "Isolation projection nine-tuple drifted.");
  }
  const authority = object(value.replayAuthoritySha256, `${path}/replayAuthoritySha256`);
  exactKeys(authority, `${path}/replayAuthoritySha256`, AUTHORITY_KEYS);
  if (!canonicalEqual(authority, expectedAuthority(expected.replayContract))) {
    fail(`${path}/replayAuthoritySha256`, "Isolation authority digests drifted.");
  }
  const runHashes = object(value.runReportSha256, `${path}/runReportSha256`);
  exactKeys(runHashes, `${path}/runReportSha256`, ["a", "b"]);
  if (!canonicalEqual(runHashes, { a: runs[0].sha256, b: runs[1].sha256 })) {
    fail(`${path}/runReportSha256`, "Isolation receipt does not bind exact A/B raw bytes.");
  }
  const probes = object(value.probes, `${path}/probes`);
  exactKeys(probes, `${path}/probes`, ["a", "b"]);
  const a = object(probes.a, `${path}/probes/a`);
  const b = object(probes.b, `${path}/probes/b`);
  validateProbes(a, `${path}/probes/a`, platform);
  validateProbes(b, `${path}/probes/b`, platform);
  if (!canonicalEqual(a, b)) fail(`${path}/probes`, "A/B isolation probes must be stable.");
  if (platform === "darwin-arm64") validateDarwinBoundary(value, path);
  else validateLinuxBoundary(value, path, expected.linuxRootfs, expected.replayContract);
}

function validateDarwinBoundary(value: JsonRecord, path: string): void {
  exactValues(value, path, {
    denyDefaultSandbox: true,
    writeAuthorityLimitedToDisposableRunRoot: true,
    separateProcessGroupPerReplay: true,
    externalSupplyReadOnly: true,
    projectionArchiveReadOnly: true,
  });
}

function validateLinuxBoundary(
  value: JsonRecord,
  path: string,
  expectedRootfs: GeneratorSupplyReplayV3Expected["linuxRootfs"],
  contract: GeneratorSupplyReplayV3Contract,
): void {
  exactValues(value, path, {
    separateNetworkMountPidNamespacePerReplay: true,
    pidNamespaceKillsDescendants: true,
    rootfsFreshExtractionPerReplay: true,
    rootfsReadOnly: true,
    inputReadOnly: true,
    projectionReadOnly: true,
    tmpfsTmp: true,
    tmpfsEphemeral: true,
    tmpfsDeviceTree: true,
    candidateUid: 65534,
    candidateGid: 65534,
    noNewPrivileges: true,
    nodeModulesReadOnlyBind: true,
  });
  if (
    !Array.isArray(value.candidateSupplementaryGroups) ||
    value.candidateSupplementaryGroups.length !== 0
  ) {
    fail(
      `${path}/candidateSupplementaryGroups`,
      "Linux candidate must have no supplementary groups.",
    );
  }
  const capabilities = object(value.candidateCapabilities, `${path}/candidateCapabilities`);
  exactKeys(capabilities, `${path}/candidateCapabilities`, [
    "effective",
    "permitted",
    "bounding",
    "ambient",
  ]);
  exactValues(capabilities, `${path}/candidateCapabilities`, {
    effective: "0000000000000000",
    permitted: "0000000000000000",
    bounding: "0000000000000000",
    ambient: "0000000000000000",
  });
  const rootfs = object(value.ubuntuRootfs, `${path}/ubuntuRootfs`);
  exactKeys(rootfs, `${path}/ubuntuRootfs`, [
    "registryIndexDigest",
    "platformManifestDigest",
    "configImageId",
    "rootfsLayerDigest",
    "exportTarSha256",
    "exportTarSizeBytes",
    "archiveInspection",
  ]);
  exactValues(rootfs, `${path}/ubuntuRootfs`, {
    registryIndexDigest: expectedRootfs.registryIndexDigest,
    platformManifestDigest: expectedRootfs.platformManifestDigest,
    configImageId: expectedRootfs.configImageId,
    rootfsLayerDigest: expectedRootfs.rootfsLayerDigest,
    exportTarSha256: expectedRootfs.exportTarSha256,
    exportTarSizeBytes: expectedRootfs.exportTarSizeBytes,
  });
  const inspection = object(rootfs.archiveInspection, `${path}/ubuntuRootfs/archiveInspection`);
  exactKeys(inspection, `${path}/ubuntuRootfs/archiveInspection`, [
    "formatVersion",
    "profile",
    "manifestAlgorithm",
    "manifestSha256",
    "entries",
    "regularFiles",
    "directories",
    "symlinks",
    "hardlinks",
    "unsafeEntries",
    "duplicateEntries",
    "specialEntries",
    "linkPrefixDescendants",
    "linkCycles",
  ]);
  exactValues(inspection, `${path}/ubuntuRootfs/archiveInspection`, {
    formatVersion: "cloud-agents-generator-replay-archive-inspection/v1",
    profile: "rootfs",
    manifestAlgorithm: contract.algorithms.projectionArchiveMemberManifest,
    manifestSha256: expectedRootfs.inspectionManifestSha256,
    entries: expectedRootfs.entries,
    regularFiles: expectedRootfs.regularFiles,
    directories: expectedRootfs.directories,
    symlinks: expectedRootfs.symlinks,
    hardlinks: expectedRootfs.hardlinks,
    unsafeEntries: 0,
    duplicateEntries: 0,
    specialEntries: 0,
    linkPrefixDescendants: 0,
    linkCycles: 0,
  });
}

function validateProbes(probes: JsonRecord, path: string, platform: Platform): void {
  const keys =
    platform === "darwin-arm64"
      ? [
          "node",
          "python",
          "supply",
          "archive",
          "nodeModules",
          "nodeModulesRelink",
          "sibling",
          "final",
          "detachedDescendant",
          "posixSpawnDetached",
        ]
      : [
          "node",
          "python",
          "supply",
          "archive",
          "nodeModules",
          "stdoutChannel",
          "sibling",
          "final",
          "rootfs",
          "input",
          "projection",
          "route",
          "identity",
        ];
  exactKeys(probes, path, keys);
  const checks: ReadonlyArray<readonly [string, string, number, string]> =
    platform === "darwin-arm64"
      ? [
          ["node", "node net.connect 1.1.1.1:443", 1, "EPERM"],
          ["python", "python socket.connect 1.1.1.1:443", 1, "Operation not permitted"],
          [
            "supply",
            "touch generator-supply://external-supply/codex-generator-supply-read-only-probe",
            1,
            "Operation not permitted",
          ],
          [
            "archive",
            "touch generator-supply://core-projection/archive",
            1,
            "Operation not permitted",
          ],
          ["nodeModules", "unlink or write the bound node_modules authority", 1, "errno=1"],
          ["nodeModulesRelink", "replace the bound node_modules symlink", 1, "errno=1"],
          ["sibling", "test sibling replay root absent", 0, ""],
          ["final", "read trusted-parent final evidence sentinel", 1, "Operation not permitted"],
          [
            "detachedDescendant",
            "fork setsid fork then read trusted-parent sentinel",
            0,
            "errno=1",
          ],
          [
            "posixSpawnDetached",
            "posix_spawn setsid child then read trusted-parent sentinel",
            0,
            "errno=1",
          ],
        ]
      : [
          ["node", "node net.connect 1.1.1.1:443", 1, "ENETUNREACH"],
          ["python", "python socket.connect 1.1.1.1:443", 1, "Network is unreachable"],
          [
            "supply",
            "touch generator-supply://external-supply/codex-generator-supply-read-only-probe",
            1,
            "Read-only file system",
          ],
          [
            "archive",
            "touch generator-supply://core-projection/archive",
            1,
            "Read-only file system",
          ],
          [
            "nodeModules",
            "unlink or write the bound node_modules authority",
            1,
            "Read-only file system",
          ],
          [
            "stdoutChannel",
            "read /proc/1/fd/1 trusted runner stdout channel",
            1,
            "No such file or directory",
          ],
          ["sibling", "test sibling replay root absent", 0, ""],
          ["final", "read trusted-parent final evidence sentinel", 0, ""],
          [
            "rootfs",
            "touch /etc/codex-generator-supply-read-only-probe",
            1,
            "Read-only file system",
          ],
          [
            "input",
            "touch /input/codex-generator-supply-read-only-probe",
            1,
            "Read-only file system",
          ],
          [
            "projection",
            "touch /projection/core-generator-input-projection.tar",
            1,
            "Read-only file system",
          ],
          ["route", "awk default route /proc/net/route", 0, ""],
        ];
  for (const [key, command, exitCode, output] of checks)
    validateProbe(
      object(probes[key], `${path}/${key}`),
      `${path}/${key}`,
      command,
      exitCode,
      output,
    );
  if (platform === "linux-amd64")
    validateIdentity(object(probes.identity, `${path}/identity`), `${path}/identity`);
}

function validateProbe(
  probe: JsonRecord,
  path: string,
  command: string,
  exitCode: number,
  output: string,
): void {
  exactKeys(probe, path, PROBE_KEYS);
  if (
    probe.command !== command ||
    probe.exitCode !== exitCode ||
    typeof probe.stdout !== "string" ||
    typeof probe.stderr !== "string" ||
    (output === ""
      ? probe.stdout !== "" || probe.stderr !== ""
      : !`${probe.stdout}\n${probe.stderr}`.includes(output))
  ) {
    fail(path, `Isolation probe did not prove ${command}.`);
  }
}

function validateIdentity(probe: JsonRecord, path: string): void {
  validateProbe(
    probe,
    path,
    "read uid gid groups capabilities and no-new-privileges",
    0,
    "NoNewPrivs:\t1",
  );
  const text = String(probe.stdout).replaceAll("\r", "");
  if (
    !/^Uid:[\t ]*65534[\t ]+65534[\t ]+65534[\t ]+65534$/mu.test(text) ||
    !/^Gid:[\t ]*65534[\t ]+65534[\t ]+65534[\t ]+65534$/mu.test(text) ||
    !/^Groups:[\t ]*$/mu.test(text) ||
    ["CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"].some(
      (field) => !new RegExp(`^${field}:[\\t ]*0000000000000000$`, "mu").test(text),
    )
  ) {
    fail(
      path,
      "Linux identity probe did not bind uid/gid, empty groups, zero capabilities, and NNP.",
    );
  }
}

function validateSummary(
  receipt: ReadReceipt,
  receipts: ReadonlyMap<string, ReadReceipt>,
  expected: GeneratorSupplyReplayV3Expected,
  projection: GeneratorSupplyReplayV3Projection,
  output: Readonly<{ candidateManifestSha256: string; outputFiles: number }>,
): void {
  const derived = buildSummary(receipts, expected, projection, output);
  exactKeys(receipt.value, `/${receipt.path}`, Object.keys(derived));
  if (!canonicalEqual(receipt.value, derived)) {
    fail(
      `/${receipt.path}`,
      "Replay summary must be exactly derived from the other seven receipt bytes.",
    );
  }
  if (Object.hasOwn(receipt.value, "rejectedExecutorSha256")) {
    fail(
      `/${receipt.path}/rejectedExecutorSha256`,
      "V3 summary cannot bind rejected-executor evidence.",
    );
  }
}

function buildSummary(
  receipts: ReadonlyMap<string, ReadReceipt>,
  expected: GeneratorSupplyReplayV3Expected,
  projection: GeneratorSupplyReplayV3Projection,
  output: Readonly<{ candidateManifestSha256: string; outputFiles: number }>,
): JsonRecord {
  const get = (path: string): ReadReceipt => {
    const receipt = receipts.get(path);
    if (!receipt) fail(`/${path}`, "Derived summary input is absent.");
    return receipt;
  };
  const [, darwinA, darwinB, darwinIsolation, linuxA, linuxB, linuxIsolation, projectionPath] =
    SUCCESSOR_V3_REPLAY_RECEIPT_PATHS;
  return {
    formatVersion: expected.replayContract.receiptFormats.summary,
    status: "DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED",
    wrapperPolicy: expected.replayContract.wrapperPolicy,
    wrapperSha256: prefixed(expected.replayContract.authorityFiles.wrapper.sha256),
    authoritativeReplayScope: expected.replayContract.authoritativeReplayScope,
    ...projection,
    candidateManifestSha256: output.candidateManifestSha256,
    darwinNetworkIsolation: "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES",
    linuxNetworkIsolation: "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS",
    candidateOutputsEqual: true,
    nonAllowlistedChanges: 0,
    runReportSha256: {
      "darwin-a": get(darwinA).sha256,
      "darwin-b": get(darwinB).sha256,
      "linux-a": get(linuxA).sha256,
      "linux-b": get(linuxB).sha256,
    },
    darwinIsolationSha256: get(darwinIsolation).sha256,
    linuxIsolationSha256: get(linuxIsolation).sha256,
    projectionReceiptSha256: get(projectionPath).sha256,
    notGateClosure: true,
  };
}

/** Test-only helper for constructing the one derived receipt after seven raw receipts are fixed. */
export function buildGeneratorSupplyReplayV3SummaryForTest(
  rawReceipts: Readonly<Record<string, Readonly<{ bytes: Buffer; value: JsonRecord }>>>,
  expected: GeneratorSupplyReplayV3Expected,
): JsonRecord {
  const receipts = new Map<string, ReadReceipt>();
  for (const [path, receipt] of Object.entries(rawReceipts)) {
    receipts.set(path, {
      path,
      bytes: receipt.bytes,
      value: receipt.value,
      sha256: `sha256:${createHash("sha256").update(receipt.bytes).digest("hex")}`,
    });
  }
  const projectionPath = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[7];
  const darwinAPath = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[1];
  const projectionReceipt = receipts.get(projectionPath)?.value;
  const darwinA = receipts.get(darwinAPath)?.value;
  if (!projectionReceipt || !darwinA)
    fail("/test-fixture", "Raw fixture needs projection and Darwin A.");
  const inspection = object(
    projectionReceipt.archiveInspection,
    "/test-fixture/projection/archiveInspection",
  );
  const projection: GeneratorSupplyReplayV3Projection = {
    projectionTreeSha: String(projectionReceipt.treeSha),
    projectionArchiveSha256: String(projectionReceipt.archiveSha256),
    projectionArchiveSizeBytes: Number(projectionReceipt.archiveSizeBytes),
    projectionArchiveMemberManifestAlgorithm: String(inspection.manifestAlgorithm),
    projectionArchiveMemberManifestSha256: `sha256:${String(inspection.manifestSha256)}`,
    projectionArchiveMembers: Number(inspection.entries),
    inputTreeManifestAlgorithm: String(inspection.regularFileManifestAlgorithm),
    inputTreeManifestSha256: `sha256:${String(inspection.regularFileManifestSha256)}`,
    inputTreeFiles: Number(inspection.regularFiles),
  };
  return buildSummary(receipts, expected, projection, {
    candidateManifestSha256: String(darwinA.candidateManifestSha256),
    outputFiles: Number(darwinA.outputFiles),
  });
}

/**
 * Builds the non-replay expectations only from the immutable v2 supply set.
 * Callers must run the v1 predecessor fence before using this helper.
 */
export function buildGeneratorSupplyReplayV3ExpectedFromImmutableV2(
  root: string,
  replayContract: GeneratorSupplyReplayV3Contract,
): GeneratorSupplyReplayV3Expected {
  return buildGeneratorSupplyReplayV3ExpectedFromImmutableV2Internal(root, replayContract);
}

export function assertGeneratorSupplyReplayV3V2DerivedABAMutationForTest(
  root: string,
  replayContract: GeneratorSupplyReplayV3Contract,
  path: string,
  beforeDerivedRead: () => void,
  afterDerivedRead: () => void,
): void {
  buildGeneratorSupplyReplayV3ExpectedFromImmutableV2Internal(root, replayContract, undefined, {
    path,
    beforeDerivedRead,
    afterDerivedRead,
    fired: false,
  });
}

function buildGeneratorSupplyReplayV3ExpectedFromImmutableV2Internal(
  root: string,
  replayContract: GeneratorSupplyReplayV3Contract,
  identities?: StableFileIdentity[],
  mutation?: GeneratorSupplyV2DerivedReadMutation,
): GeneratorSupplyReplayV3Expected {
  const fixedRecords = readGeneratorSupplyV2DerivedFileRecords(root, identities);
  const readDerived = (path: string): JsonRecord => {
    const record = fixedRecords.get(path);
    if (record === undefined)
      fail(`/${path}`, "Immutable v2 derived material is absent from fixed authority.");
    if (mutation?.path === path && !mutation.fired) {
      mutation.fired = true;
      mutation.beforeDerivedRead();
      try {
        return parseStableJson(root, path, identities, record);
      } finally {
        mutation.afterDerivedRead();
      }
    }
    return parseStableJson(root, path, identities, record);
  };
  const source = readDerived("tools/generator-supply/v1/source.json");
  const npm = readDerived("tools/generator-supply/v1/evidence/npm.json");
  const artifacts = readDerived("tools/generator-supply/v1/evidence/artifacts.json");
  const wheels = readDerived("tools/generator-supply/v1/evidence/wheels.json");
  const ubuntu = readDerived("tools/generator-supply/v1/evidence/ubuntu-image-binding.json");
  const linuxIsolation = readDerived(
    "tools/generator-supply/v1/evidence/replay/linux-isolation.json",
  );
  const v1Projection = readDerived("tools/generator-supply/v1/evidence/replay/projection.json");
  if (mutation !== undefined && !mutation.fired)
    fail(`/${mutation.path}`, "V1 derived-read mutation test path was not captured.");
  const profile = object(source.profile, "/v1/source/profile");
  const runtimes = object(profile.runtimes, "/v1/source/profile/runtimes");
  const installed = arrayOfObjects(npm.installed, "/v1/npm/installed");
  const executables = object(artifacts.executables, "/v1/artifacts/executables");
  const wheelPlatforms = arrayOfObjects(wheels.platforms, "/v1/wheels/platforms");

  const material = (platform: Platform): GeneratorSupplyReplayV3PlatformMaterial => {
    const installedPlatform = installed.find((entry) => entry.platform === platform);
    const nodeModules = object(
      installedPlatform?.nodeModules,
      `/v1/npm/installed/${platform}/nodeModules`,
    );
    if (nodeModules.algorithm !== replayContract.algorithms.nodeModulesManifest) {
      fail(
        `/v1/npm/installed/${platform}/nodeModules/algorithm`,
        "V1 material algorithm drifted from replay contract.",
      );
    }
    const executableRows = arrayOfObjects(
      executables[platform],
      `/v1/artifacts/executables/${platform}`,
    ).toSorted((left, right) =>
      Buffer.compare(Buffer.from(String(left.id)), Buffer.from(String(right.id))),
    );
    const executableHash = createHash("sha256");
    for (const row of executableRows)
      executableHash.update(String(row.id)).update("\0").update(String(row.sha256)).update("\0");
    const wheelPlatform = wheelPlatforms.find((entry) => entry.platform === platform);
    const wheelRows = arrayOfObjects(
      wheelPlatform?.wheels,
      `/v1/wheels/platforms/${platform}/wheels`,
    );
    const wheelHash = createHash("sha256");
    for (const row of wheelRows) {
      wheelHash
        .update(String(row.filename))
        .update("\0")
        .update(String(row.sizeBytes))
        .update("\0")
        .update(String(row.sha256))
        .update("\0");
    }
    return {
      nodeModulesManifestSha256: prefixed(String(nodeModules.sha256)),
      nodeModulesFiles: positiveIntegerValue(
        nodeModules.files,
        `/v1/npm/${platform}/nodeModules/files`,
      ),
      wheelhouseManifestSha256: `sha256:${wheelHash.digest("hex")}`,
      externalExecutableSetSha256: `sha256:${executableHash.digest("hex")}`,
      loadedOxfmtBinding: String(
        platform === "darwin-arm64" ? npm.darwinLoadedBinding : npm.linuxLoadedBinding,
      ),
      versions: {
        node: String(runtimes.node),
        bun: String(runtimes.bun),
        go: `go version go${String(runtimes.go)} ${platform === "darwin-arm64" ? "darwin/arm64" : "linux/amd64"}`,
        python: String(runtimes.python),
        uv: String(runtimes.uv),
        protoc: String(runtimes.protoc),
        protocGenGo: String(runtimes.protocGenGo),
        protocGenConnectGo: String(runtimes.protocGenConnectGo),
      },
    };
  };
  const rootfs = object(linuxIsolation.ubuntuRootfs, "/v1/linuxIsolation/ubuntuRootfs");
  const inspection = object(
    rootfs.archiveInspection,
    "/v1/linuxIsolation/ubuntuRootfs/archiveInspection",
  );
  for (const key of [
    "registryIndexDigest",
    "platformManifestDigest",
    "configImageId",
    "rootfsLayerDigest",
    "exportTarSha256",
  ]) {
    if (rootfs[key] !== ubuntu[key])
      fail(
        `/v1/ubuntu-image-binding/${key}`,
        "V1 isolation rootfs does not bind immutable image identity.",
      );
  }
  return {
    replayContract,
    predecessorProjectionTreeSha: String(v1Projection.treeSha),
    platforms: {
      "darwin-arm64": material("darwin-arm64"),
      "linux-amd64": material("linux-amd64"),
    },
    linuxRootfs: {
      registryIndexDigest: String(ubuntu.registryIndexDigest),
      platformManifestDigest: String(ubuntu.platformManifestDigest),
      configImageId: String(ubuntu.configImageId),
      rootfsLayerDigest: String(ubuntu.rootfsLayerDigest),
      exportTarSha256: String(ubuntu.exportTarSha256),
      exportTarSizeBytes: positiveIntegerValue(
        rootfs.exportTarSizeBytes,
        "/v1/linuxIsolation/ubuntuRootfs/exportTarSizeBytes",
      ),
      inspectionManifestSha256: String(inspection.manifestSha256),
      entries: positiveIntegerValue(
        inspection.entries,
        "/v1/linuxIsolation/ubuntuRootfs/archiveInspection/entries",
      ),
      regularFiles: positiveIntegerValue(
        inspection.regularFiles,
        "/v1/linuxIsolation/ubuntuRootfs/archiveInspection/regularFiles",
      ),
      directories: positiveIntegerValue(
        inspection.directories,
        "/v1/linuxIsolation/ubuntuRootfs/archiveInspection/directories",
      ),
      symlinks: positiveIntegerValue(
        inspection.symlinks,
        "/v1/linuxIsolation/ubuntuRootfs/archiveInspection/symlinks",
      ),
      hardlinks: positiveIntegerValue(
        inspection.hardlinks,
        "/v1/linuxIsolation/ubuntuRootfs/archiveInspection/hardlinks",
      ),
    },
  };
}

/** Test-only complete semantic fixture assembled from immutable v2 material. */
export function buildGeneratorSupplyReplayV3TestFixture(
  root: string,
  replayContract: GeneratorSupplyReplayV3Contract,
): Readonly<{
  expected: GeneratorSupplyReplayV3Expected;
  receipts: Readonly<Record<(typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number], JsonRecord>>;
}> {
  const expected = buildGeneratorSupplyReplayV3ExpectedFromImmutableV2(root, replayContract);
  const v1Root = "tools/generator-supply/v1/evidence/replay";
  const [
    ,
    darwinAPath,
    darwinBPath,
    darwinIsolationPath,
    linuxAPath,
    linuxBPath,
    linuxIsolationPath,
    projectionPath,
  ] = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS;
  const paths = {
    [darwinAPath]: `${v1Root}/darwin-a.json`,
    [darwinBPath]: `${v1Root}/darwin-b.json`,
    [darwinIsolationPath]: `${v1Root}/darwin-isolation.json`,
    [linuxAPath]: `${v1Root}/linux-a.json`,
    [linuxBPath]: `${v1Root}/linux-b.json`,
    [linuxIsolationPath]: `${v1Root}/linux-isolation.json`,
    [projectionPath]: `${v1Root}/projection.json`,
  } as const;
  const receipts: Record<string, JsonRecord> = {};
  for (const [target, source] of Object.entries(paths))
    receipts[target] = clone(parseStableJson(root, source));
  const newTree =
    expected.predecessorProjectionTreeSha === "b".repeat(40) ? "c".repeat(40) : "b".repeat(40);
  const updateProjection = (value: JsonRecord): void => {
    value.projectionTreeSha = newTree;
  };
  for (const path of [
    darwinAPath,
    darwinBPath,
    linuxAPath,
    linuxBPath,
    darwinIsolationPath,
    linuxIsolationPath,
  ]) {
    updateProjection(receipts[path]!);
    receipts[path]!.replayAuthoritySha256 = expectedAuthority(replayContract);
  }
  for (const path of [darwinIsolationPath, linuxIsolationPath]) {
    // The v1 replay bytes are used only as deterministic semantic fixture
    // material; the v3 contract must still expose its own wrapper authority.
    receipts[path]!.wrapperPolicy = replayContract.wrapperPolicy;
    receipts[path]!.wrapperSha256 = prefixed(replayContract.authorityFiles.wrapper.sha256);
  }
  for (const path of [darwinAPath, darwinBPath, linuxAPath, linuxBPath]) {
    receipts[path]!.outputFiles = SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length;
  }
  const projection = receipts[projectionPath]!;
  projection.treeSha = newTree;
  projection.excluded = [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS];
  object(
    projection.archiveInspection,
    "/test/projection/archiveInspection",
  ).reconstructedGitTreeSha = newTree;
  for (const [isolationPath, aPath, bPath] of [
    [darwinIsolationPath, darwinAPath, darwinBPath],
    [linuxIsolationPath, linuxAPath, linuxBPath],
  ] as const) {
    receipts[isolationPath]!.runReportSha256 = {
      a: serializedSha256(receipts[aPath]!),
      b: serializedSha256(receipts[bPath]!),
    };
  }
  const raw = Object.fromEntries(
    Object.entries(receipts).map(([path, value]) => [
      path,
      { value, bytes: serializeFixture(value) },
    ]),
  );
  const summary = buildGeneratorSupplyReplayV3SummaryForTest(raw, expected);
  receipts[SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]] = summary;
  return {
    expected,
    receipts: receipts as Record<(typeof SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)[number], JsonRecord>,
  };
}

function projectionFrom(value: JsonRecord): GeneratorSupplyReplayV3Projection {
  return Object.fromEntries(
    PROJECTION_KEYS.map((key) => [key, value[key]]),
  ) as GeneratorSupplyReplayV3Projection;
}

function stableRun(value: JsonRecord): JsonRecord {
  const {
    replayRun: _run,
    freshExtractionRoot: _root,
    homeDirectory: _home,
    temporaryDirectory: _tmp,
    uvCacheDirectory: _uv,
    xdgCacheHome: _xdg,
    ...stable
  } = value;
  return stable;
}

function expectedAuthority(contract: GeneratorSupplyReplayV3Contract): JsonRecord {
  return {
    wrapper: prefixed(contract.authorityFiles.wrapper.sha256),
    runner: prefixed(contract.authorityFiles.runner.sha256),
    pathHelper: prefixed(contract.authorityFiles.pathHelper.sha256),
    archiveInspector: prefixed(contract.authorityFiles.archiveInspector.sha256),
  };
}

function validateExpected(expected: GeneratorSupplyReplayV3Expected): void {
  const contract = expected.replayContract;
  validateContract(contract);
  if (!/^[0-9a-f]{40}$/u.test(expected.predecessorProjectionTreeSha)) {
    fail(
      "/expected/predecessorProjectionTreeSha",
      "Expected predecessor projection must be a Git tree SHA.",
    );
  }
}

function validateContract(contract: GeneratorSupplyReplayV3Contract): void {
  exactKeys(contract as JsonRecord, "/expected/replayContract", [
    "authorityFiles",
    "coreGeneratorOutputs",
    "preReplayExclusionPolicy",
    "wrapperPolicy",
    "authoritativeReplayScope",
    "algorithms",
    "projectionExclusions",
    "receiptFormats",
  ]);
  exactKeys(
    contract.authorityFiles as JsonRecord,
    "/expected/replayContract/authorityFiles",
    AUTHORITY_KEYS,
  );
  exactKeys(contract.algorithms as JsonRecord, "/expected/replayContract/algorithms", [
    "nodeModulesManifest",
    "projectionArchiveMemberManifest",
    "inputTreeManifest",
  ]);
  exactKeys(contract.receiptFormats as JsonRecord, "/expected/replayContract/receiptFormats", [
    "summary",
    "run",
    "isolation",
    "projection",
  ]);
  if (
    contract.preReplayExclusionPolicy !==
    "EXACT17_ONLY_NO_WILDCARD_ALL_OTHER_TRACKED_BYTES_INCLUDED"
  ) {
    fail(
      "/expected/replayContract/preReplayExclusionPolicy",
      "Replay contract exclusion policy drifted.",
    );
  }
  if (
    contract.coreGeneratorOutputs.length !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length ||
    contract.coreGeneratorOutputs.some(
      (record, index) => record.path !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS[index],
    )
  ) {
    fail(
      "/expected/replayContract/coreGeneratorOutputs",
      "Replay contract core output order drifted.",
    );
  }
  for (const [index, record] of contract.coreGeneratorOutputs.entries()) {
    const pointer = `/expected/replayContract/coreGeneratorOutputs/${index}`;
    exactKeys(record as JsonRecord, pointer, ["path", "mode", "gitBlob", "sha256", "sizeBytes"]);
    if (
      record.mode !== "100644" ||
      !/^[0-9a-f]{40}$/u.test(record.gitBlob) ||
      !/^[0-9a-f]{64}$/u.test(record.sha256) ||
      !Number.isSafeInteger(record.sizeBytes) ||
      record.sizeBytes < 1
    ) {
      fail(pointer, "Replay contract core output record is malformed.");
    }
  }
  for (const [name, record] of Object.entries(contract.authorityFiles)) {
    exactKeys(record as JsonRecord, `/expected/replayContract/authorityFiles/${name}`, [
      "path",
      "sha256",
      "sizeBytes",
    ]);
    bareDigest(record.sha256, `/expected/replayContract/authorityFiles/${name}/sha256`);
    positiveInteger(record.sizeBytes, `/expected/replayContract/authorityFiles/${name}/sizeBytes`);
  }
  if (!canonicalEqual(contract.projectionExclusions, [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS])) {
    fail(
      "/expected/replayContract/projectionExclusions",
      "Replay contract must bind the exact ordered 17 exclusions.",
    );
  }
  if (
    contract.wrapperPolicy !== "VERSIONED_ISOLATION_WRAPPER_V3" ||
    contract.authoritativeReplayScope !==
      "EXACT49_CORE_OUTPUTS_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY"
  ) {
    fail(
      "/expected/replayContract",
      "Replay contract must bind the exact v3 wrapper and replay scope.",
    );
  }
  exactValues(contract.receiptFormats as JsonRecord, "/expected/replayContract/receiptFormats", {
    summary: "cloud-agents-generator-supply-replay-summary/v3",
    run: "cloud-agents-generator-replay-run/v1",
    isolation: "cloud-agents-generator-replay-isolation/v1",
    projection: "cloud-agents-core-generator-projection/v1",
  });
}

function validateAuthorityFiles(
  root: string,
  contract: GeneratorSupplyReplayV3Contract,
  identities?: StableFileIdentity[],
): void {
  const seen = new Set<string>();
  for (const [name, record] of Object.entries(contract.authorityFiles)) {
    const expectedPath =
      SUCCESSOR_V3_REPLAY_AUTHORITY_FILES[name as keyof typeof SUCCESSOR_V3_REPLAY_AUTHORITY_FILES];
    if (record.path !== expectedPath) {
      fail(
        `/expected/replayContract/authorityFiles/${name}/path`,
        "Authority file path is not the exact v3 authority path.",
      );
    }
    if (seen.has(record.path)) {
      fail(
        `/expected/replayContract/authorityFiles/${name}/path`,
        "Authority file paths must be unique.",
      );
    }
    seen.add(record.path);
    let snapshot: Readonly<{ bytes: Buffer; identity: StableFileIdentity }>;
    try {
      snapshot = readContainedRegularFileSnapshot(root, record.path);
    } catch (error) {
      if (error instanceof GeneratorSupplyReplayV3Error) throw error;
      fail(`/${record.path}`, `Authority file is missing or unsafe: ${String(error)}.`);
    }
    const { bytes } = snapshot;
    identities?.push(snapshot.identity);
    const actual = createHash("sha256").update(bytes).digest("hex");
    if (bytes.byteLength !== record.sizeBytes || actual !== record.sha256) {
      fail(`/${record.path}`, "Source-bound authority file size or SHA-256 drifted.");
    }
  }
}

function parseStableJson(
  root: string,
  path: string,
  identities?: StableFileIdentity[],
  authority?: GeneratorSupplyV2DerivedFileRecord,
): JsonRecord {
  let parsed: unknown;
  try {
    const snapshot = readContainedRegularFileSnapshot(root, path);
    identities?.push(snapshot.identity);
    const actualSha256 = `sha256:${createHash("sha256").update(snapshot.bytes).digest("hex")}`;
    if (
      authority !== undefined &&
      (authority.path !== path ||
        snapshot.bytes.byteLength !== authority.sizeBytes ||
        actualSha256 !== authority.sha256)
    ) {
      fail(`/${path}`, "Immutable v2 derived-read bytes do not match fixed authority.");
    }
    parsed = JSON.parse(snapshot.bytes.toString("utf8"));
  } catch (error) {
    if (error instanceof GeneratorSupplyReplayV3Error) throw error;
    fail(`/${path}`, `Immutable v2 material is missing or invalid: ${String(error)}.`);
  }
  return object(parsed, `/${path}`);
}

function readGeneratorSupplyV2DerivedFileRecords(
  root: string,
  identities?: StableFileIdentity[],
): ReadonlyMap<string, GeneratorSupplyV2DerivedFileRecord> {
  const manifestPath = GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath;
  let snapshot: Readonly<{ bytes: Buffer; identity: StableFileIdentity }>;
  let manifest: JsonRecord;
  try {
    snapshot = readContainedRegularFileSnapshot(root, manifestPath);
    identities?.push(snapshot.identity);
    const actualSha256 = createHash("sha256").update(snapshot.bytes).digest("hex");
    if (
      snapshot.bytes.byteLength !== GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSizeBytes ||
      actualSha256 !== GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSha256
    ) {
      fail(`/${manifestPath}`, "Immutable v2 evidence manifest does not match fixed authority.");
    }
    manifest = object(JSON.parse(snapshot.bytes.toString("utf8")), `/${manifestPath}`);
  } catch (error) {
    if (error instanceof GeneratorSupplyReplayV3Error) throw error;
    fail(
      `/${manifestPath}`,
      `Immutable v2 evidence manifest is missing or invalid: ${String(error)}.`,
    );
  }
  exactKeys(manifest, `/${manifestPath}`, ["algorithm", "files"]);
  if (manifest.algorithm !== GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.algorithm)
    fail(`/${manifestPath}/algorithm`, "Immutable v2 evidence manifest algorithm drifted.");
  const files = arrayOfObjects(manifest.files, `/${manifestPath}/files`);
  if (files.length !== GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberCount)
    fail(`/${manifestPath}/files`, "Immutable v2 evidence manifest member count drifted.");
  const records = new Map<string, GeneratorSupplyV2DerivedFileRecord>();
  for (const [index, value] of files.entries()) {
    const recordPath = `/${manifestPath}/files/${index}`;
    exactKeys(value, recordPath, ["path", "sha256", "sizeBytes"]);
    if (
      typeof value.path !== "string" ||
      !value.path.startsWith(GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberPathPrefix)
    ) {
      fail(`${recordPath}/path`, "Immutable v2 evidence member path is outside fixed prefix.");
    }
    digest(value.sha256, `${recordPath}/sha256`);
    positiveInteger(value.sizeBytes, `${recordPath}/sizeBytes`);
    if (records.has(value.path))
      fail(`${recordPath}/path`, "Immutable v2 evidence manifest contains a duplicate path.");
    records.set(value.path, {
      path: value.path,
      sha256: String(value.sha256),
      sizeBytes: Number(value.sizeBytes),
    });
  }
  const sourcePath = "tools/generator-supply/v1/source.json";
  const sourceAuthority = GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.find(
    (record) => record.path === sourcePath,
  );
  if (sourceAuthority === undefined)
    fail(`/${sourcePath}`, "Immutable v2 source is absent from fixed outer authority.");
  records.set(sourcePath, {
    ...sourceAuthority,
    sha256: `sha256:${sourceAuthority.sha256}`,
  });
  return records;
}

function readContainedRegularFileSnapshot(
  root: string,
  path: string,
): Readonly<{ bytes: Buffer; identity: StableFileIdentity }> {
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation) ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  ) {
    fail(`/${path}`, "Receipt path is not canonical and repository-contained.");
  }
  let current = rootReal;
  for (const [index, component] of path.split("/").entries()) {
    current = resolve(current, component);
    const stat = lstatSync(current);
    if (
      stat.isSymbolicLink() ||
      (index < path.split("/").length - 1 ? !stat.isDirectory() : !stat.isFile())
    ) {
      fail(`/${path}`, "Receipt path must be a regular non-symlink file with safe ancestors.");
    }
  }
  const before = lstatSync(absolute, { bigint: true });
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const fdBefore = fstatSync(descriptor, { bigint: true });
    if (!fdBefore.isFile() || fdBefore.dev !== before.dev || fdBefore.ino !== before.ino)
      fail(`/${path}`, "Receipt changed before open.");
    const bytes = readFileSync(descriptor);
    const fdAfter = fstatSync(descriptor, { bigint: true });
    const after = lstatSync(absolute, { bigint: true });
    if (
      fdAfter.dev !== fdBefore.dev ||
      fdAfter.ino !== fdBefore.ino ||
      fdAfter.size !== fdBefore.size ||
      fdAfter.mtimeNs !== fdBefore.mtimeNs ||
      fdAfter.ctimeNs !== fdBefore.ctimeNs ||
      after.dev !== fdBefore.dev ||
      after.ino !== fdBefore.ino ||
      after.size !== fdBefore.size ||
      realpathSync(absolute) !== absolute
    )
      fail(`/${path}`, "Receipt changed during stable read.");
    return {
      bytes,
      identity: {
        rootReal,
        path,
        absolute,
        dev: fdAfter.dev,
        ino: fdAfter.ino,
        size: fdAfter.size,
        mtimeNs: fdAfter.mtimeNs,
        ctimeNs: fdAfter.ctimeNs,
      },
    };
  } finally {
    closeSync(descriptor);
  }
}

function assertStableSnapshotCurrent(
  root: string,
  identities: readonly StableFileIdentity[],
): void {
  if (identities.length === 0) fail("/snapshot", "Stable input snapshot is empty.");
  const rootReal = identities[0]!.rootReal;
  if (realpathSync(root) !== rootReal) fail("/snapshot", "Repository root changed after capture.");
  const unique = new Map<string, StableFileIdentity>();
  for (const identity of identities) {
    const previous = unique.get(identity.path);
    if (previous) {
      if (!sameStableFileIdentity(previous, identity)) {
        fail(`/${identity.path}`, "A repeated snapshot path changed between capture phases.");
      }
      continue;
    }
    unique.set(identity.path, identity);
  }
  for (const identity of unique.values()) {
    try {
      if (
        identity.rootReal !== rootReal ||
        resolve(rootReal, identity.path) !== identity.absolute
      ) {
        fail(`/${identity.path}`, "Stable snapshot root or absolute path binding drifted.");
      }
      let current = rootReal;
      const components = identity.path.split("/");
      for (const [index, component] of components.entries()) {
        current = resolve(current, component);
        const stat = lstatSync(current, { bigint: true });
        if (
          stat.isSymbolicLink() ||
          (index < components.length - 1 ? !stat.isDirectory() : !stat.isFile())
        ) {
          fail(`/${identity.path}`, "Input path topology changed after stable capture.");
        }
      }
      const after = lstatSync(identity.absolute, { bigint: true });
      if (
        after.dev !== identity.dev ||
        after.ino !== identity.ino ||
        after.size !== identity.size ||
        after.mtimeNs !== identity.mtimeNs ||
        after.ctimeNs !== identity.ctimeNs ||
        realpathSync(identity.absolute) !== identity.absolute
      ) {
        fail(`/${identity.path}`, "Input changed after the stable snapshot was captured.");
      }
    } catch (error) {
      if (error instanceof GeneratorSupplyReplayV3Error) throw error;
      fail(`/${identity.path}`, `Input snapshot is no longer current: ${String(error)}.`);
    }
  }
}

function sameStableFileIdentity(left: StableFileIdentity, right: StableFileIdentity): boolean {
  return (
    left.rootReal === right.rootReal &&
    left.path === right.path &&
    left.absolute === right.absolute &&
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs
  );
}

function exactKeys(value: JsonRecord, path: string, keys: readonly string[]): void {
  const actual = Object.keys(value).toSorted();
  const expected = [...keys].toSorted();
  if (new Set(keys).size !== keys.length || !canonicalEqual(actual, expected))
    fail(path, "Object keys are not the exact closed set.");
}

function exactValues(value: JsonRecord, path: string, expected: JsonRecord): void {
  for (const [key, wanted] of Object.entries(expected)) {
    if (!canonicalEqual(value[key], wanted))
      fail(`${path}/${key}`, "Field does not match fixed receipt semantics.");
  }
}

function object(value: unknown, path: string): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    fail(path, "Expected a JSON object.");
  return value as JsonRecord;
}

function positiveInteger(value: unknown, path: string): void {
  if (!Number.isInteger(value) || Number(value) <= 0) fail(path, "Expected a positive integer.");
}

function positiveIntegerValue(value: unknown, path: string): number {
  positiveInteger(value, path);
  return Number(value);
}

function arrayOfObjects(value: unknown, path: string): JsonRecord[] {
  if (!Array.isArray(value)) fail(path, "Expected an array.");
  return value.map((entry, index) => object(entry, `${path}/${index}`));
}

function digest(value: unknown, path: string): void {
  if (!/^sha256:[0-9a-f]{64}$/u.test(String(value)))
    fail(path, "Expected a prefixed SHA-256 digest.");
}

function bareDigest(value: unknown, path: string): void {
  if (!/^[0-9a-f]{64}$/u.test(String(value))) fail(path, "Expected a bare SHA-256 digest.");
}

function prefixed(value: string): string {
  bareDigest(value, "/digest");
  return `sha256:${value}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  return Buffer.from(canonicalizeJson(left)).equals(Buffer.from(canonicalizeJson(right)));
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function serializeFixture(value: unknown): Buffer {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}

function serializedSha256(value: unknown): string {
  return `sha256:${createHash("sha256").update(serializeFixture(value)).digest("hex")}`;
}

function fail(path: string, message: string): never {
  throw new GeneratorSupplyReplayV3Error(path, message);
}
