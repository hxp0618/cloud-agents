import { createHash } from "node:crypto";
import { lstatSync, readFileSync, realpathSync, writeFileSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import {
  assertPlatformContractSuccessorV3Current,
  PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH,
} from "./platform-contract-successor-v3";
import { identitySDKContractInputs, identitySDKGeneratorSources } from "./platform-identity-sdk";
import {
  platformJSONSDKContractInputs,
  platformJSONSDKGeneratorSources,
} from "./platform-json-sdk";
import { platformProtoContractInputs, platformProtoGeneratorSources } from "./platform-proto-sdk";

export const EXTERNAL_CONSUMER_V3_SOURCE_PATH =
  "tools/g-contract-external-consumer/v3/source.json" as const;
export const EXTERNAL_CONSUMER_V3_SOURCE_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/source.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_PROFILE_PATH =
  "tools/g-contract-external-consumer/v3/profile.json" as const;
export const EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH =
  "scripts/replay-platform-g-contract-external-consumer-v3.ts" as const;
export const EXTERNAL_CONSUMER_V3_REPLAY_PLAN_FORMAT =
  "cloud-agents-g-contract-external-consumer-replay-plan/v3" as const;
export const EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/projection-receipt.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/native-replay-receipt.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_REPLAY_SUMMARY_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/replay-summary-receipt.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_PROFILE_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/profile.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_REPLAY_PLAN_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/replay-plan.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_SCHEMA_PATH =
  "tools/g-contract-external-consumer/v3/projection-plan.schema.json" as const;
export const EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_FORMAT =
  "cloud-agents-g-contract-external-consumer-projection-plan/v3" as const;
export const EXTERNAL_CONSUMER_V3_FORMAT =
  "cloud-agents-g-contract-external-consumer-source/v3" as const;
const EXTERNAL_CONSUMER_V3_SCHEMA_URI =
  "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/source.schema.json" as const;

const V2_SOURCE_PATH = "tools/g-contract-external-consumer/v2/source.json" as const;
const V2_PREDECESSOR_FROZEN = {
  sha256: "sha256:c1184e72ae09290e0994d3a3727d7ba75e5fc06fc23c7edfec2393beb147e595",
  sizeBytes: 24867,
} as const;
const STATIC_SEMANTIC_INPUT_PATHS = [
  "contracts/generated/proto/cloud-agents-v1alpha1.binpb",
  "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb",
  "contracts/generated/proto/manifest.json",
  "contracts/proto-generation.profile.json",
  "contracts/generation.lock.successor-v3.json",
  "docs/plan/cloud-agents-platform/05-gates-and-acceptance.md",
  "package.json",
  "scripts/generate-platform-contract-successor-v3.ts",
  "scripts/generate-platform-g-contract-external-consumer-v3.ts",
  EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
  "test/scripts/platform-contract-successor-v3.test.ts",
  "scripts/lib/platform-contract-successor-v3.ts",
  "test/scripts/platform-g-contract-external-consumer-v3.test.ts",
  "scripts/lib/platform-g-contract-external-consumer-v3.ts",
  "scripts/lib/platform-g-contract-external-consumer-v3-projection.ts",
  "test/scripts/platform-g-contract-external-consumer-v3-projection.test.ts",
  "scripts/lib/platform-g-contract-external-consumer-v3-native.ts",
  "test/scripts/platform-g-contract-external-consumer-v3-native.test.ts",
  "scripts/lib/platform-g-contract-external-consumer-v3-summary.ts",
  "test/scripts/platform-g-contract-external-consumer-v3-summary.test.ts",
  "scripts/lib/platform-g-contract-external-consumer-v3-ustar.ts",
  "test/scripts/platform-g-contract-external-consumer-v3-ustar.test.ts",
  "scripts/lib/platform-json-semantics.ts",
  "test/scripts/platform-successor-dag.test.ts",
  "scripts/lib/platform-successor-dag.ts",
  "test/scripts/test-platform-sdk-consumers.ts",
  "sdk/go/generated-manifest.json",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/proto-generated-manifest.json",
  "sdk/go/THIRD_PARTY_NOTICES.md",
  "sdk/go/test/common/json_test.go",
  "sdk/go/test/openapi/client_test.go",
  "sdk/go/test/platform/json_test.go",
  "sdk/typescript/generated-manifest.json",
  "sdk/typescript/package.json",
  "sdk/typescript/proto-generated-manifest.json",
  "sdk/typescript/THIRD_PARTY_NOTICES.md",
  "sdk/typescript/test/platform.test.ts",
  ".mise.toml",
  "bun.lock",
  "tools/g-contract-external-consumer/v3/source.schema.json",
  "tools/g-contract-external-consumer/v3/candidate-manifest.schema.json",
  EXTERNAL_CONSUMER_V3_PROJECTION_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_NATIVE_REPLAY_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_SUMMARY_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_PROFILE_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_REPLAY_PLAN_SCHEMA_PATH,
  EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_SCHEMA_PATH,
  "docs/plan/p1/g-contract-external-consumer-v3-replay-implementation-20261006.md",
] as const;

const RECEIPT_PATHS = [
  "tools/g-contract-external-consumer/v3/evidence/replay/projection.json",
  "tools/g-contract-external-consumer/v3/evidence/replay/darwin-arm64-a.json",
  "tools/g-contract-external-consumer/v3/evidence/replay/darwin-arm64-b.json",
  "tools/g-contract-external-consumer/v3/evidence/replay/linux-amd64-a.json",
  "tools/g-contract-external-consumer/v3/evidence/replay/linux-amd64-b.json",
  "tools/g-contract-external-consumer/v3/evidence/replay/replay.json",
] as const;

type FileRecord = Readonly<{ path: string; sha256: string; sizeBytes: number }>;

const REPLAY_PLATFORM_RUNS = [
  { platform: "darwin-arm64", runId: "A" },
  { platform: "darwin-arm64", runId: "B" },
  { platform: "linux-amd64", runId: "A" },
  { platform: "linux-amd64", runId: "B" },
] as const;

export type ExternalConsumerV3ReplayPlan = Readonly<{
  $schema: "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/replay-plan.schema.json";
  formatVersion: typeof EXTERNAL_CONSUMER_V3_REPLAY_PLAN_FORMAT;
  decisionId: "D-053-EC-3";
  profileId: "g-contract-external-consumer/v3";
  status: "REPLAY_PENDING";
  sourceBinding: FileRecord;
  successorBinding: ExternalConsumerV3Source["contractSuccessor"];
  receiptPaths: readonly string[];
  platformRuns: typeof REPLAY_PLATFORM_RUNS;
  writeMode: "READ_ONLY";
  syntheticReceipts: "FORBIDDEN";
  network: "LOOPBACK_ONLY";
  externalEgress: "DENIED";
  notGateClosure: true;
  gateStatus: "ALL_GATES_OPEN";
}>;

export type ExternalConsumerV3ProjectionPlan = Readonly<{
  $schema: "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/projection-plan.schema.json";
  formatVersion: typeof EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_FORMAT;
  decisionId: "D-053-EC-3";
  profileId: "g-contract-external-consumer/v3";
  status: "BLOCKED";
  blockerCode: "FROZEN_CLEAN_CANDIDATE_REQUIRED";
  candidateBoundary: "EXTERNAL_CLEAN_CANDIDATE";
  sourceBinding: FileRecord;
  successorBinding: ExternalConsumerV3Source["contractSuccessor"];
  exclusionPaths: readonly string[];
  receiptPath: string;
  writeMode: "READ_ONLY";
  syntheticReceipts: "FORBIDDEN";
  notGateClosure: true;
  gateStatus: "ALL_GATES_OPEN";
}>;

export type ExternalConsumerV3Source = Readonly<{
  $schema: string;
  formatVersion: typeof EXTERNAL_CONSUMER_V3_FORMAT;
  registryId: "cloud-agents/g-contract-external-consumer-current";
  profileId: "g-contract-external-consumer/v3";
  decisionId: "D-053-EC-3";
  status: "AUTHORITY_FROZEN_REVIEW_PENDING";
  predecessor: FileRecord & {
    formatVersion: "cloud-agents-g-contract-external-consumer-source/v2";
  };
  contractSuccessor: FileRecord & { outputCount: 47; outputManifestSha256: string };
  semanticInputs: readonly FileRecord[];
  receiptPaths: readonly string[];
  evidenceContract: Readonly<{
    state: "REPLAY_PENDING";
    syntheticReceipts: "FORBIDDEN";
    loopbackOnly: true;
    externalEgress: "DENIED";
  }>;
  implementationBoundary: Readonly<{
    productionDatabaseWrites: "NOT_AUTHORIZED";
    providerSideEffects: "FORBIDDEN";
    deployment: "NOT_AUTHORIZED";
    publication: "NOT_AUTHORIZED";
    gateStatus: "ALL_GATES_OPEN";
  }>;
}>;

function safePath(root: string, path: string, allowMissing = false): string {
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
  const segments = path.split("/");
  let cursor = rootReal;
  for (const [index, segment] of segments.entries()) {
    cursor = resolve(cursor, segment);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (stat === undefined && allowMissing) return absolute;
    if (
      stat === undefined ||
      stat.isSymbolicLink() ||
      (index === segments.length - 1 ? !stat.isFile() : !stat.isDirectory())
    ) {
      throw new Error(`${path} must be a regular file with non-symlink parents.`);
    }
  }
  return absolute;
}

export function fileRecord(root: string, path: string): FileRecord {
  const absolute = safePath(root, path);
  const bytes = readFileSync(absolute);
  return {
    path,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
  };
}

function semanticInputPaths(root: string): string[] {
  const paths = [
    ...STATIC_SEMANTIC_INPUT_PATHS,
    ...identitySDKContractInputs(root),
    ...identitySDKGeneratorSources(),
    ...platformJSONSDKContractInputs(root),
    ...platformJSONSDKGeneratorSources(),
    ...platformProtoContractInputs(root),
    ...platformProtoGeneratorSources(),
  ];
  return [...new Set(paths)].toSorted();
}

function assertFrozenPredecessor(root: string): void {
  const bytes = readFileSync(safePath(root, V2_SOURCE_PATH));
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  if (
    bytes.byteLength !== V2_PREDECESSOR_FROZEN.sizeBytes ||
    digest !== V2_PREDECESSOR_FROZEN.sha256
  ) {
    throw new Error(`${V2_SOURCE_PATH} does not match the frozen v2 predecessor.`);
  }
}

function validateSchema(root: string, value: unknown): void {
  const schema = JSON.parse(
    readFileSync(safePath(root, EXTERNAL_CONSUMER_V3_SOURCE_SCHEMA_PATH), "utf8"),
  ) as object;
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, value)) {
    throw new Error(`External v3 source schema validation failed: ${ajv.errorsText()}`);
  }
}

function validateReplayPlan(root: string, value: unknown): void {
  const schema = JSON.parse(
    readFileSync(safePath(root, EXTERNAL_CONSUMER_V3_REPLAY_PLAN_SCHEMA_PATH), "utf8"),
  ) as object;
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, value)) {
    throw new Error(`External v3 replay plan schema validation failed: ${ajv.errorsText()}`);
  }
}

function validateProjectionPlan(root: string, value: unknown): void {
  const schema = JSON.parse(
    readFileSync(safePath(root, EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_SCHEMA_PATH), "utf8"),
  ) as object;
  const ajv = new Ajv2020({ allErrors: true, strict: false });
  if (!ajv.validate(schema, value)) {
    throw new Error(`External v3 projection plan schema validation failed: ${ajv.errorsText()}`);
  }
}

function sourceBody(root: string): Omit<ExternalConsumerV3Source, "$schema"> {
  assertPlatformContractSuccessorV3Current(root);
  assertFrozenPredecessor(root);
  const successor = fileRecord(root, PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH);
  const lock = JSON.parse(
    readFileSync(safePath(root, PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH), "utf8"),
  ) as { currentOutputSet: { count: number; manifestSha256: string } };
  if (lock.currentOutputSet.count !== 47)
    throw new Error("External v3 requires the 47-output successor.");
  return {
    formatVersion: EXTERNAL_CONSUMER_V3_FORMAT,
    registryId: "cloud-agents/g-contract-external-consumer-current",
    profileId: "g-contract-external-consumer/v3",
    decisionId: "D-053-EC-3",
    status: "AUTHORITY_FROZEN_REVIEW_PENDING",
    predecessor: {
      path: V2_SOURCE_PATH,
      sha256: V2_PREDECESSOR_FROZEN.sha256,
      sizeBytes: V2_PREDECESSOR_FROZEN.sizeBytes,
      formatVersion: "cloud-agents-g-contract-external-consumer-source/v2",
    },
    contractSuccessor: {
      ...successor,
      outputCount: lock.currentOutputSet.count,
      outputManifestSha256: lock.currentOutputSet.manifestSha256,
    },
    semanticInputs: semanticInputPaths(root).map((path) => fileRecord(root, path)),
    receiptPaths: [...RECEIPT_PATHS],
    evidenceContract: {
      state: "REPLAY_PENDING",
      syntheticReceipts: "FORBIDDEN",
      loopbackOnly: true,
      externalEgress: "DENIED",
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

export function buildExternalConsumerV3Source(root: string): ExternalConsumerV3Source {
  const source = { $schema: EXTERNAL_CONSUMER_V3_SCHEMA_URI, ...sourceBody(root) };
  validateSchema(root, source);
  return source;
}

export function serializeExternalConsumerV3Source(source: ExternalConsumerV3Source): string {
  return `${JSON.stringify(source, null, 2)}\n`;
}

export function writeExternalConsumerV3Source(root: string): void {
  writeFileSync(
    safePath(root, EXTERNAL_CONSUMER_V3_SOURCE_PATH, true),
    serializeExternalConsumerV3Source(buildExternalConsumerV3Source(root)),
  );
}

/** Capture once: all later evidence must bind the bytes that passed current validation. */
export function readCurrentExternalConsumerV3Source(root: string) {
  const sourceBytes = readFileSync(safePath(root, EXTERNAL_CONSUMER_V3_SOURCE_PATH));
  const sourceText = sourceBytes.toString("utf8");
  const source = JSON.parse(sourceText) as ExternalConsumerV3Source;
  validateSchema(root, source);
  const expected = buildExternalConsumerV3Source(root);
  if (!sourceBytes.equals(Buffer.from(serializeExternalConsumerV3Source(expected), "utf8"))) {
    throw new Error(`${EXTERNAL_CONSUMER_V3_SOURCE_PATH} is stale.`);
  }
  return {
    source,
    sourceBytes,
    sourceBinding: {
      path: EXTERNAL_CONSUMER_V3_SOURCE_PATH,
      sha256: `sha256:${createHash("sha256").update(sourceBytes).digest("hex")}` as const,
      sizeBytes: sourceBytes.byteLength,
    },
  };
}

export function assertExternalConsumerV3SourceCurrent(root: string): void {
  readCurrentExternalConsumerV3Source(root);
}

export function assertExternalConsumerV3PredecessorFrozen(root: string): void {
  assertFrozenPredecessor(root);
}

export function assertExternalConsumerV3ProfileAbsent(root: string): void {
  const absolute = safePath(root, EXTERNAL_CONSUMER_V3_PROFILE_PATH, true);
  if (lstatSync(absolute, { throwIfNoEntry: false }) !== undefined) {
    throw new Error(
      `${EXTERNAL_CONSUMER_V3_PROFILE_PATH} must remain absent until replay evidence exists.`,
    );
  }
}

export function assertExternalConsumerV3ReceiptsAbsent(root: string): void {
  for (const path of RECEIPT_PATHS) {
    const absolute = safePath(root, path, true);
    if (lstatSync(absolute, { throwIfNoEntry: false }) !== undefined) {
      throw new Error(`${path} must remain absent until replay evidence exists.`);
    }
  }
}

export function assertExternalConsumerV3ReplayAbsent(root: string): void {
  assertExternalConsumerV3ProfileAbsent(root);
  assertExternalConsumerV3ReceiptsAbsent(root);
}

/**
 * Validate authority and describe the replay stages without writing evidence.
 * This deliberately has no child process, provider, network, or filesystem
 * writer so a plan cannot be mistaken for a replay receipt.
 */
export function buildExternalConsumerV3ReplayPlan(root: string): ExternalConsumerV3ReplayPlan {
  const repositoryRoot = realpathSync(resolve(root));
  const { source, sourceBinding } = readCurrentExternalConsumerV3Source(repositoryRoot);
  assertExternalConsumerV3ReplayAbsent(repositoryRoot);
  const plan: ExternalConsumerV3ReplayPlan = {
    $schema:
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/replay-plan.schema.json",
    formatVersion: EXTERNAL_CONSUMER_V3_REPLAY_PLAN_FORMAT,
    decisionId: source.decisionId,
    profileId: source.profileId,
    status: "REPLAY_PENDING",
    sourceBinding,
    successorBinding: source.contractSuccessor,
    receiptPaths: [...RECEIPT_PATHS],
    platformRuns: REPLAY_PLATFORM_RUNS,
    writeMode: "READ_ONLY",
    syntheticReceipts: "FORBIDDEN",
    network: "LOOPBACK_ONLY",
    externalEgress: "DENIED",
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  validateReplayPlan(repositoryRoot, plan);
  return plan;
}

/**
 * Bind the projection stage to the successor authority while refusing to
 * project the current dirty worktree. A clean candidate must be supplied and
 * reviewed separately before an archive writer is introduced.
 */
export function buildExternalConsumerV3ProjectionPlan(
  root: string,
): ExternalConsumerV3ProjectionPlan {
  const repositoryRoot = realpathSync(resolve(root));
  const { source, sourceBinding } = readCurrentExternalConsumerV3Source(repositoryRoot);
  assertExternalConsumerV3ReplayAbsent(repositoryRoot);
  const plan: ExternalConsumerV3ProjectionPlan = {
    $schema:
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/projection-plan.schema.json",
    formatVersion: EXTERNAL_CONSUMER_V3_PROJECTION_PLAN_FORMAT,
    decisionId: source.decisionId,
    profileId: source.profileId,
    status: "BLOCKED",
    blockerCode: "FROZEN_CLEAN_CANDIDATE_REQUIRED",
    candidateBoundary: "EXTERNAL_CLEAN_CANDIDATE",
    sourceBinding,
    successorBinding: source.contractSuccessor,
    exclusionPaths: [...PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS],
    receiptPath: RECEIPT_PATHS[0],
    writeMode: "READ_ONLY",
    syntheticReceipts: "FORBIDDEN",
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  validateProjectionPlan(repositoryRoot, plan);
  return plan;
}

export {
  RECEIPT_PATHS as EXTERNAL_CONSUMER_V3_RECEIPT_PATHS,
  REPLAY_PLATFORM_RUNS as EXTERNAL_CONSUMER_V3_REPLAY_PLATFORM_RUNS,
  semanticInputPaths as externalConsumerV3SemanticInputPaths,
};
