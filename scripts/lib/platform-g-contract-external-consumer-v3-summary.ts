import { createHash } from "node:crypto";
import { lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { isDeepStrictEqual } from "node:util";

import Ajv2020 from "ajv/dist/2020.js";

import {
  EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_PROFILE_PATH,
  EXTERNAL_CONSUMER_V3_PROFILE_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_PLATFORM_RUNS,
  EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_SUMMARY_SCHEMA_PATH,
  ExternalConsumerV3Source,
  fileRecord,
  readCurrentExternalConsumerV3Source,
} from "./platform-g-contract-external-consumer-v3";
import {
  canonicalRoot,
  isContained,
  readVerifiedExternalConsumerV3Projection,
  safePath,
} from "./platform-g-contract-external-consumer-v3-native";

const EVIDENCE_DIRECTORY = "tools/g-contract-external-consumer/v3/evidence/replay";
const PROJECTION_RECEIPT = `${EVIDENCE_DIRECTORY}/projection.json`;
const PROJECTION_ARCHIVE = `${EVIDENCE_DIRECTORY}/projection.tar`;
const PROJECTION_MEMBER_MANIFEST = `${EVIDENCE_DIRECTORY}/projection.member-manifest.json`;
const SUMMARY_RECEIPT = `${EVIDENCE_DIRECTORY}/replay.json`;
const SUMMARY_ENTRYPOINT =
  "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --summary --projection-root <projection-root> --darwin-a-root <darwin-a-root> --darwin-b-root <darwin-b-root> --linux-a-root <linux-a-root> --linux-b-root <linux-b-root> --output-root <output-root>";

type PlatformRun = (typeof EXTERNAL_CONSUMER_V3_REPLAY_PLATFORM_RUNS)[number];
type RunKey = `${PlatformRun["platform"]}-${PlatformRun["runId"]}`;
const RUNS = EXTERNAL_CONSUMER_V3_REPLAY_PLATFORM_RUNS.map((run) => ({
  ...run,
  key: `${run.platform}-${run.runId}` as RunKey,
  receiptPath: `${EVIDENCE_DIRECTORY}/${run.platform}-${run.runId.toLowerCase()}.json`,
}));
type NativeRoots = Readonly<Record<RunKey, string>>;
type JsonRecord = Record<string, any>;

export type ExternalConsumerV3SummaryOptions = Readonly<{
  authorityRoot: string;
  projectionRoot: string;
  nativeRoots: NativeRoots;
  outputRoot: string;
}>;

export type ExternalConsumerV3SummaryResult = Readonly<{
  summaryPath: string;
  profilePath: string;
  summary: Readonly<Record<string, unknown>>;
  profile: Readonly<Record<string, unknown>>;
}>;

/** Validate four native replays, then create one independent external evidence bundle. */
export function writeExternalConsumerV3Summary(
  options: ExternalConsumerV3SummaryOptions,
): ExternalConsumerV3SummaryResult {
  const roots = canonicalWriterRoots(options);
  const verified = verifyEvidence(roots.authorityRoot, roots.projectionRoot, roots.nativeRoots);
  const summary = buildSummary(verified);
  validateSchema(
    roots.authorityRoot,
    EXTERNAL_CONSUMER_V3_REPLAY_SUMMARY_SCHEMA_PATH,
    summary,
    "Replay summary",
  );
  const summaryBytes = jsonBytes(summary);
  const receiptBytes = buildReceiptBytes(verified, summaryBytes);
  const profile = buildProfile(
    verified.source,
    verified.sourceBinding,
    verified.source.receiptPaths.map((path) => binding(path, receiptBytes.get(path))),
  );
  validateSchema(
    roots.authorityRoot,
    EXTERNAL_CONSUMER_V3_PROFILE_SCHEMA_PATH,
    profile,
    "External consumer profile",
  );

  const targets = [
    PROJECTION_RECEIPT,
    PROJECTION_ARCHIVE,
    PROJECTION_MEMBER_MANIFEST,
    ...RUNS.map((run) => run.receiptPath),
    SUMMARY_RECEIPT,
    EXTERNAL_CONSUMER_V3_PROFILE_PATH,
  ];
  assertTargetsAbsent(roots.outputRoot, targets);
  writeExclusive(roots.outputRoot, PROJECTION_RECEIPT, verified.projectionReceiptBytes);
  writeExclusive(roots.outputRoot, PROJECTION_ARCHIVE, verified.archiveBytes);
  writeExclusive(roots.outputRoot, PROJECTION_MEMBER_MANIFEST, verified.memberManifestBytes);
  for (const run of verified.runs) {
    writeExclusive(roots.outputRoot, run.spec.receiptPath, run.bytes);
  }
  writeExclusive(roots.outputRoot, SUMMARY_RECEIPT, summaryBytes);
  writeExclusive(roots.outputRoot, EXTERNAL_CONSUMER_V3_PROFILE_PATH, jsonBytes(profile));
  return {
    summaryPath: join(roots.outputRoot, SUMMARY_RECEIPT),
    profilePath: join(roots.outputRoot, EXTERNAL_CONSUMER_V3_PROFILE_PATH),
    summary,
    profile,
  };
}

/** Reconstruct the summary and profile from the bundle without writing any files. */
export function assertExternalConsumerV3SummaryCurrent(options: {
  readonly authorityRoot: string;
  readonly outputRoot: string;
}): void {
  const authorityRoot = canonicalRoot(options.authorityRoot, "authority root");
  const outputRoot = canonicalRoot(options.outputRoot, "output root");
  assertExternalRoot(authorityRoot, outputRoot, "output root");
  const nativeRoots = Object.fromEntries(RUNS.map((run) => [run.key, outputRoot])) as Record<
    RunKey,
    string
  >;
  const verified = verifyEvidence(authorityRoot, outputRoot, nativeRoots);
  const summary = buildSummary(verified);
  validateSchema(
    authorityRoot,
    EXTERNAL_CONSUMER_V3_REPLAY_SUMMARY_SCHEMA_PATH,
    summary,
    "Replay summary",
  );
  const summaryBytes = assertBytesEqual(outputRoot, SUMMARY_RECEIPT, jsonBytes(summary));
  const receiptBytes = buildReceiptBytes(verified, summaryBytes);
  const profile = buildProfile(
    verified.source,
    verified.sourceBinding,
    verified.source.receiptPaths.map((path) => binding(path, receiptBytes.get(path))),
  );
  validateSchema(
    authorityRoot,
    EXTERNAL_CONSUMER_V3_PROFILE_SCHEMA_PATH,
    profile,
    "External consumer profile",
  );
  assertBytesEqual(outputRoot, EXTERNAL_CONSUMER_V3_PROFILE_PATH, jsonBytes(profile));
}

function canonicalWriterRoots(options: ExternalConsumerV3SummaryOptions) {
  const authorityRoot = canonicalRoot(options.authorityRoot, "authority root");
  const projectionRoot = canonicalRoot(options.projectionRoot, "projection root");
  const outputRoot = canonicalRoot(options.outputRoot, "output root");
  const nativeRoots = Object.fromEntries(
    RUNS.map((run) => [
      run.key,
      canonicalRoot(options.nativeRoots[run.key], `${run.key} native root`),
    ]),
  ) as Record<RunKey, string>;
  const inputs = [
    { label: "projection root", root: projectionRoot },
    ...RUNS.map((run) => ({ label: `${run.key} native root`, root: nativeRoots[run.key] })),
  ];
  for (const input of inputs) assertExternalRoot(authorityRoot, input.root, input.label);
  assertExternalRoot(authorityRoot, outputRoot, "output root");
  for (const input of inputs) {
    if (isContained(input.root, outputRoot) || isContained(outputRoot, input.root)) {
      throw new Error(`${input.label} and output root must be distinct and non-nested.`);
    }
  }
  return { authorityRoot, projectionRoot, outputRoot, nativeRoots };
}

function assertExternalRoot(authorityRoot: string, candidate: string, label: string): void {
  if (isContained(authorityRoot, candidate) || isContained(candidate, authorityRoot)) {
    throw new Error(`${label} must be external to and non-nested with the authority root.`);
  }
}

function verifyEvidence(authorityRoot: string, projectionRoot: string, nativeRoots: NativeRoots) {
  const { source, sourceBinding } = readCurrentExternalConsumerV3Source(authorityRoot);
  const projectionResult = readVerifiedExternalConsumerV3Projection(
    authorityRoot,
    projectionRoot,
    source,
    sourceBinding,
  );
  const projection = projectionResult.projection as unknown as JsonRecord;
  if (!isDeepStrictEqual(projection.sourceBinding, sourceBinding)) {
    throw new Error("Projection source binding does not exactly match current authority.");
  }
  if (!isDeepStrictEqual(projection.contractSuccessorBinding, source.contractSuccessor)) {
    throw new Error("Projection successor binding does not exactly match current authority.");
  }
  const expectedProjectionBinding = {
    archivePath: projection.projection.archivePath,
    archiveSha256: projection.projection.archiveSha256,
    archiveSizeBytes: projection.projection.archiveSizeBytes,
    memberManifestSha256: projection.projection.memberManifestSha256,
    inputManifestSha256: projection.projection.inputManifest.sha256,
    memberCount: projection.projection.memberCount,
  };
  const seen = new Set<string>();
  const outputs: Record<"typescript" | "go", string[]> = { typescript: [], go: [] };
  const runs = RUNS.map((spec) => {
    const bytes = readFileSync(safePath(nativeRoots[spec.key], spec.receiptPath));
    const receipt = JSON.parse(bytes.toString("utf8")) as JsonRecord;
    validateSchema(
      authorityRoot,
      EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH,
      receipt,
      "Native replay receipt",
    );
    const identity = `${String(receipt.platform)}/${String(receipt.runId)}`;
    if (seen.has(identity)) throw new Error(`Duplicate native replay identity: ${identity}.`);
    seen.add(identity);
    if (
      receipt.decisionId !== source.decisionId ||
      receipt.profileId !== source.profileId ||
      receipt.status !== "CURRENT" ||
      receipt.platform !== spec.platform ||
      receipt.runId !== spec.runId ||
      receipt.isolation.receiptPath !== spec.receiptPath
    ) {
      throw new Error(`Native replay identity does not match ${spec.key}.`);
    }
    if (!isDeepStrictEqual(receipt.sourceBinding, sourceBinding)) {
      throw new Error(`${spec.key} source binding does not exactly match current authority.`);
    }
    if (!isDeepStrictEqual(receipt.projectionBinding, expectedProjectionBinding)) {
      throw new Error(`${spec.key} projection binding is incomplete or mismatched.`);
    }
    for (const consumer of ["typescript", "go"] as const) {
      outputs[consumer].push(receipt.consumers[consumer].outputSha256);
    }
    return { spec, receipt, bytes, sha256: digest(bytes) };
  });
  for (const consumer of ["typescript", "go"] as const) {
    if (new Set(outputs[consumer]).size !== 1) {
      throw new Error(`${consumer} consumer output differs across native replays.`);
    }
  }
  return {
    source,
    sourceBinding,
    projection,
    projectionReceiptBytes: Buffer.from(projectionResult.projectionReceiptBytes),
    archiveBytes: Buffer.from(projectionResult.archiveBytes),
    memberManifestBytes: Buffer.from(projectionResult.memberManifestBytes),
    runs,
  };
}

function buildSummary(verified: ReturnType<typeof verifyEvidence>) {
  return {
    formatVersion: "cloud-agents-g-contract-external-consumer-replay-summary-receipt/v3",
    decisionId: verified.source.decisionId,
    profileId: verified.source.profileId,
    status: "CURRENT",
    sourceBinding: verified.sourceBinding,
    projection: {
      archiveSha256: verified.projection.projection.archiveSha256,
      memberManifestSha256: verified.projection.projection.memberManifestSha256,
      inputManifestSha256: verified.projection.projection.inputManifest.sha256,
    },
    platformRuns: verified.runs.map((run) => ({
      platform: run.spec.platform,
      runId: run.spec.runId,
      receiptPath: run.spec.receiptPath,
      receiptSha256: run.sha256,
      status: "CURRENT",
    })),
    equality: {
      archive: "ALL_FOUR_RUNS_EQUAL",
      memberManifest: "ALL_FOUR_RUNS_EQUAL",
      inputManifest: "ALL_FOUR_RUNS_EQUAL",
      consumerOutputs: "A_EQUALS_B_PER_PLATFORM_AND_CROSS_PLATFORM",
    },
    runner: { path: EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH, entrypoint: SUMMARY_ENTRYPOINT },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
}

function buildProfile(
  source: ExternalConsumerV3Source,
  sourceBinding: ReturnType<typeof fileRecord>,
  receiptBindings: readonly ReturnType<typeof fileRecord>[],
) {
  return {
    $schema:
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/profile.schema.json",
    formatVersion: "cloud-agents-g-contract-external-consumer-registry/v3",
    registryId: "cloud-agents/g-contract-external-consumer-replay/v3",
    profileId: source.profileId,
    decisionId: source.decisionId,
    status: "PROFILE_CURRENT_FINAL_REVIEW_PENDING",
    sourceBinding,
    successorBinding: source.contractSuccessor,
    inputBindings: source.semanticInputs,
    receiptPaths: source.receiptPaths,
    receiptBindings,
    stateMachine: "PROFILE_CURRENT",
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
    implementationBoundary: {
      productionDatabaseWrites: source.implementationBoundary.productionDatabaseWrites,
      providerSideEffects: source.implementationBoundary.providerSideEffects,
      deployment: source.implementationBoundary.deployment,
      publication: source.implementationBoundary.publication,
    },
  };
}

function buildReceiptBytes(
  verified: ReturnType<typeof verifyEvidence>,
  summaryBytes: Uint8Array,
): Map<string, Uint8Array> {
  return new Map([
    [PROJECTION_RECEIPT, verified.projectionReceiptBytes],
    ...verified.runs.map((run) => [run.spec.receiptPath, run.bytes] as const),
    [SUMMARY_RECEIPT, summaryBytes],
  ]);
}

function binding(path: string, bytes: Uint8Array | undefined) {
  if (bytes === undefined) throw new Error(`No validated receipt bytes exist for ${path}.`);
  return { path, sha256: digest(bytes), sizeBytes: bytes.byteLength };
}

function validateSchema(root: string, schemaPath: string, value: unknown, label: string): void {
  const schema = readJson(safePath(root, schemaPath));
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, value)) {
    throw new Error(`${label} schema validation failed: ${ajv.errorsText()}`);
  }
}

function assertTargetsAbsent(root: string, paths: readonly string[]): void {
  for (const path of paths) {
    const absolute = safePath(root, path, true);
    if (lstatSync(absolute, { throwIfNoEntry: false }) !== undefined) {
      throw new Error(`Refusing to overwrite existing output: ${path}.`);
    }
  }
}

function writeExclusive(root: string, path: string, bytes: Uint8Array): void {
  const absolute = safePath(root, path, true);
  mkdirSync(resolve(absolute, ".."), { recursive: true, mode: 0o755 });
  writeFileSync(absolute, bytes, { flag: "wx", mode: 0o644 });
}

function assertBytesEqual(root: string, path: string, expected: Uint8Array): Buffer {
  const actual = readFileSync(safePath(root, path));
  if (!actual.equals(expected))
    throw new Error(`${path} is not the deterministic current document.`);
  return actual;
}

function jsonBytes(value: unknown): Buffer {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`, "utf8");
}

function digest(bytes: Uint8Array): `sha256:${string}` {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function readJson(path: string): any {
  return JSON.parse(readFileSync(path, "utf8"));
}
