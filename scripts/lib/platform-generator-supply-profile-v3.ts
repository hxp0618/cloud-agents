import { createHash } from "node:crypto";
import {
  closeSync,
  constants,
  fsyncSync,
  fstatSync,
  existsSync,
  lstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  assertGeneratorSupplyReplayV3ContractCurrent,
  buildGeneratorSupplyReplayV3PreparedReceipts,
  type GeneratorSupplyReplayV3Contract,
  type GeneratorSupplyReplayV3PreparedReceipts,
} from "./platform-generator-supply-replay-v3";
import {
  SUCCESSOR_V3_ASSEMBLY_PATHS,
  SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag-v3";
import { assertSuccessorV3PredecessorsCurrent } from "./platform-successor-predecessor-v3";

export const GENERATOR_SUPPLY_V3_SOURCE_PATH = "tools/generator-supply/v3/source.json";
export const GENERATOR_SUPPLY_V3_SOURCE_SCHEMA_PATH =
  "tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json";
export const GENERATOR_SUPPLY_V3_OUTPUT_SCHEMA_PATH =
  "tools/generator-supply/v3/generator-supply-profile-v3.schema.json";
export const GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH = SUCCESSOR_V3_ASSEMBLY_PATHS[0];
export const GENERATOR_SUPPLY_V3_OUTPUT_PATH = SUCCESSOR_V3_ASSEMBLY_PATHS[1];

const REGISTRY_ID = "cloud-agents/generator-supply-profile";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v3/generator-supply-profile-source-v3.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v3/generator-supply-profile-v3.schema.json";
const EVIDENCE_MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-size-v1";
const V3_SOURCE_DOMAIN = "cloud-agents/generator-supply/source/v3";
const V3_PREDECESSOR_DOMAIN = "cloud-agents/generator-supply/predecessor-closure/v3";
const V3_ARTIFACT_DOMAIN = "cloud-agents/generator-supply/artifact-set/v3";
const V3_EVIDENCE_DOMAIN = "cloud-agents/generator-supply/evidence-manifest/v3";
const V3_PROFILE_DOMAIN = "cloud-agents/generator-supply/profile/v3";
const V3_REGISTRY_DOMAIN = "cloud-agents/generator-supply/registry/v3";

type FileRecord = Readonly<{ path: string; sha256: string; sizeBytes: number }>;
type EvidenceManifest = Readonly<{ algorithm: string; files: readonly FileRecord[] }>;

export type GeneratorSupplyV3Source = JsonRecord & {
  readonly formatVersion: "cloud-agents-generator-supply-profile-source/v3";
  readonly registryId: "cloud-agents/generator-supply-profile";
  readonly decisionId: "D-053";
  readonly baseline: JsonRecord & { readonly commit: string; readonly tree: string };
  readonly predecessorClosure: JsonRecord;
  readonly replayContract: GeneratorSupplyReplayV3Contract;
  readonly declaredProfile: JsonRecord;
  readonly replayEvidence: JsonRecord;
};

export type GeneratorSupplyV3Registry = JsonRecord & {
  readonly formatVersion: "cloud-agents-generator-supply-profile-registry/v3";
  readonly registryId: "cloud-agents/generator-supply-profile";
  readonly predecessor: JsonRecord;
  readonly sourceDigest: string;
  readonly artifactSetDigest: string;
  readonly evidenceManifestDigest: string;
  readonly profile: JsonRecord & {
    readonly profileDigest: string;
    readonly spec: JsonRecord;
    readonly evidence: JsonRecord & {
      readonly state: "ASSEMBLED_LATE_BOUND";
      readonly receipts: readonly FileRecord[];
      readonly evidenceManifest: EvidenceManifest;
    };
  };
  readonly registryDigest: string;
};

export type GeneratorSupplyV3AuthorityState =
  | "SCHEMA_ONLY"
  | "DECLARED_PRE_REPLAY"
  | "REPLAY_RECEIPTS_PRESENT_UNVERIFIED"
  | "ASSEMBLED_PROFILE_CURRENT";

export class GeneratorSupplyV3Error extends Error {
  constructor(
    readonly code:
      | "GENERATOR_SUPPLY_V3_SCHEMA_INVALID"
      | "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH"
      | "GENERATOR_SUPPLY_V3_PARTIAL_STATE"
      | "GENERATOR_SUPPLY_V3_EVIDENCE_MISMATCH"
      | "GENERATOR_SUPPLY_V3_REGISTRY_MISMATCH"
      | "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyV3Error";
  }
}

export function inspectGeneratorSupplyV3AuthorityState(
  root: string,
): GeneratorSupplyV3AuthorityState {
  schemaValidator(root);
  const source = filePresence(root, GENERATOR_SUPPLY_V3_SOURCE_PATH);
  // Slice C left a projection receipt behind before native Slice D replay
  // began. It is a late-bound output, but by itself it is not evidence that
  // replay has started. Treat that historical singleton as pre-replay while
  // still requiring all eight receipts once any native receipt appears.
  const projectionPath = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.at(-1)!;
  const nativeReceiptPaths = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.slice(0, -1);
  const receipts = groupPresence(root, nativeReceiptPaths, "/replayEvidence");
  const projectionPresent = filePresence(root, projectionPath);
  const assembly = groupPresence(root, SUCCESSOR_V3_ASSEMBLY_PATHS, "/assembly");
  if (!source) {
    if (receipts !== "NONE" || assembly !== "NONE") {
      throw v3Error(
        "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
        "/source",
        "Late-bound v3 artifacts require source authority.",
      );
    }
    return "SCHEMA_ONLY";
  }
  validateGeneratorSupplyV3Source(root, readSource(root));
  if (receipts === "NONE" && assembly === "NONE") return "DECLARED_PRE_REPLAY";
  if (receipts !== "ALL" || !projectionPresent) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      "/replayEvidence",
      "All eight v3 receipts are required together once native replay begins.",
    );
  }
  if (assembly === "NONE") return "REPLAY_RECEIPTS_PRESENT_UNVERIFIED";
  if (assembly !== "ALL") {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      "/assembly",
      "The v3 evidence manifest and profile are required together.",
    );
  }
  assertGeneratorSupplyV3RegistryCurrent(root);
  return "ASSEMBLED_PROFILE_CURRENT";
}

export function assertGeneratorSupplyV3SourceCurrent(
  root: string,
): GeneratorSupplyV3AuthorityState {
  const state = inspectGeneratorSupplyV3AuthorityState(root);
  if (state === "SCHEMA_ONLY") {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
      `/${GENERATOR_SUPPLY_V3_SOURCE_PATH}`,
      "v3 source authority is absent.",
    );
  }
  if (existsSync(resolve(root, ".git"))) assertSuccessorV3PredecessorsCurrent(root);
  return state;
}

export function writeGeneratorSupplyV3Source(root: string): void {
  const state = inspectGeneratorSupplyV3AuthorityState(root);
  if (state !== "SCHEMA_ONLY" && state !== "DECLARED_PRE_REPLAY") {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      `/${GENERATOR_SUPPLY_V3_SOURCE_PATH}`,
      "v3 source cannot be rewritten after replay begins.",
    );
  }
  const bytes = serializeGeneratorSupplyV3Source(buildGeneratorSupplyV3Source(root));
  writeExclusiveOrNoop(root, GENERATOR_SUPPLY_V3_SOURCE_PATH, bytes);
  assertGeneratorSupplyV3SourceCurrent(root);
}

export function validateGeneratorSupplyV3Source(
  root: string,
  source: GeneratorSupplyV3Source,
): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  const expected = buildGeneratorSupplyV3Source(root);
  if (!canonicalEqual(source, expected)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
      "/source",
      "v3 source bytes do not match the checked-in typed authority.",
    );
  }
  assertGeneratorSupplyReplayV3ContractCurrent(root, source.replayContract);
  const replayEvidence = source.replayEvidence as {
    readonly state?: unknown;
    readonly authority?: unknown;
    readonly receiptPaths?: unknown;
  };
  if (
    replayEvidence.state !== "DECLARED_PRE_REPLAY" ||
    replayEvidence.authority !== "EXTERNAL_LATE_BOUND"
  ) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
      "/replayEvidence",
      "v3 source must remain declared pre-replay.",
    );
  }
  const receiptPaths = replayEvidence.receiptPaths;
  if (!canonicalEqual(receiptPaths, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
      "/replayEvidence/receiptPaths",
      "v3 receipt order drifted.",
    );
  }
  const preReplayAuthorityPaths: readonly string[] = SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS;
  const projectionExclusions: readonly string[] = SUCCESSOR_V3_PROJECTION_EXCLUSIONS;
  for (const path of [GENERATOR_SUPPLY_V3_SOURCE_PATH, GENERATOR_SUPPLY_V3_SOURCE_SCHEMA_PATH]) {
    if (!preReplayAuthorityPaths.includes(path)) {
      throw v3Error(
        "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
        `/${path}`,
        "v3 source/schema must be pre-replay authority.",
      );
    }
    if (projectionExclusions.includes(path)) {
      throw v3Error(
        "GENERATOR_SUPPLY_V3_SOURCE_MISMATCH",
        `/${path}`,
        "v3 source/schema cannot be a late-bound exclusion.",
      );
    }
  }
}

export function assertGeneratorSupplyV3RegistryCurrent(
  root: string,
  supplied?: unknown,
): GeneratorSupplyV3Registry {
  const source = readSource(root);
  validateGeneratorSupplyV3Source(root, source);
  const manifest = readJson(root, GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH) as EvidenceManifest;
  const registry = readJson(root, GENERATOR_SUPPLY_V3_OUTPUT_PATH) as GeneratorSupplyV3Registry;
  if (supplied !== undefined && !canonicalEqual(supplied, registry)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_REGISTRY_MISMATCH",
      "/registry",
      "Supplied v3 registry differs from output bytes.",
    );
  }
  assertGeneratorSupplyV3RegistrySemantics(root, registry, manifest);
  return registry;
}

export function assertGeneratorSupplyV3RegistrySemantics(
  root: string,
  document: unknown,
  standaloneManifest?: EvidenceManifest,
): asserts document is GeneratorSupplyV3Registry {
  const source = readSource(root);
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, document);
  if (!isRecord(document))
    throw v3Error("GENERATOR_SUPPLY_V3_REGISTRY_MISMATCH", "/", "v3 registry must be an object.");
  const registry = document as GeneratorSupplyV3Registry;
  const manifest =
    standaloneManifest ??
    (readJson(root, GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH) as EvidenceManifest);
  const receipts = readReceiptRecords(root);
  const expectedManifest = { algorithm: EVIDENCE_MANIFEST_ALGORITHM, files: receipts };
  if (
    !canonicalEqual(manifest, expectedManifest) ||
    !canonicalEqual(registry.profile.evidence.evidenceManifest, expectedManifest)
  ) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_EVIDENCE_MISMATCH",
      "/profile/evidence/evidenceManifest",
      "v3 manifest must bind exact receipt bytes.",
    );
  }
  const predecessor = {
    baselineCommit: source.baseline.commit,
    baselineTree: source.baseline.tree,
    predecessorClosureDigest: domainDigest(V3_PREDECESSOR_DOMAIN, source.predecessorClosure),
  };
  const evidence = registry.profile.evidence;
  const expectedSourceDigest = domainDigest(V3_SOURCE_DOMAIN, source);
  const expectedArtifactDigest = domainDigest(V3_ARTIFACT_DOMAIN, { predecessor, receipts });
  const expectedEvidenceDigest = domainDigest(V3_EVIDENCE_DOMAIN, expectedManifest);
  const expectedProfileDigest = domainDigest(V3_PROFILE_DOMAIN, {
    sourceDigest: expectedSourceDigest,
    artifactSetDigest: expectedArtifactDigest,
    evidenceManifestDigest: expectedEvidenceDigest,
    spec: source.declaredProfile,
    evidence,
  });
  if (
    registry.formatVersion !== "cloud-agents-generator-supply-profile-registry/v3" ||
    registry.registryId !== REGISTRY_ID ||
    !canonicalEqual(registry.predecessor, predecessor) ||
    registry.sourceDigest !== expectedSourceDigest ||
    registry.artifactSetDigest !== expectedArtifactDigest ||
    registry.evidenceManifestDigest !== expectedEvidenceDigest ||
    evidence.state !== "ASSEMBLED_LATE_BOUND" ||
    !canonicalEqual(evidence.receipts, receipts) ||
    !canonicalEqual(registry.profile.spec, source.declaredProfile) ||
    registry.profile.profileDigest !== expectedProfileDigest
  ) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_REGISTRY_MISMATCH",
      "/",
      "v3 registry identity or domain digests drifted.",
    );
  }
  const { registryDigest: _registryDigest, ...body } = registry;
  if (registry.registryDigest !== domainDigest(V3_REGISTRY_DOMAIN, body)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_REGISTRY_MISMATCH",
      "/registryDigest",
      "v3 registry digest drifted.",
    );
  }
}

export function buildGeneratorSupplyV3EvidenceManifest(
  prepared: GeneratorSupplyReplayV3PreparedReceipts,
): EvidenceManifest {
  prepared.assertPreparedSnapshotCurrent();
  assertPreparedReceipts(prepared);
  return {
    algorithm: EVIDENCE_MANIFEST_ALGORITHM,
    files: prepared.receiptRecords.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
  };
}

export function buildGeneratorSupplyV3Registry(
  source: GeneratorSupplyV3Source,
  prepared: GeneratorSupplyReplayV3PreparedReceipts,
  evidenceManifest = buildGeneratorSupplyV3EvidenceManifest(prepared),
): GeneratorSupplyV3Registry {
  prepared.assertPreparedSnapshotCurrent();
  assertPreparedReceipts(prepared);
  const expectedManifest = buildGeneratorSupplyV3EvidenceManifest(prepared);
  if (!canonicalEqual(evidenceManifest, expectedManifest)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_EVIDENCE_MISMATCH",
      "/assembly/evidenceManifest",
      "Prepared v3 receipt bytes changed.",
    );
  }
  const predecessor = {
    baselineCommit: source.baseline.commit,
    baselineTree: source.baseline.tree,
    predecessorClosureDigest: domainDigest(V3_PREDECESSOR_DOMAIN, source.predecessorClosure),
  };
  const sourceDigest = domainDigest(V3_SOURCE_DOMAIN, source);
  const artifactSetDigest = domainDigest(V3_ARTIFACT_DOMAIN, {
    predecessor,
    receipts: evidenceManifest.files,
  });
  const evidenceManifestDigest = domainDigest(V3_EVIDENCE_DOMAIN, evidenceManifest);
  const evidence = {
    state: "ASSEMBLED_LATE_BOUND",
    receipts: evidenceManifest.files,
    evidenceManifest,
  } as const;
  const body = {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v3" as const,
    registryId: REGISTRY_ID,
    predecessor,
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    profile: {
      profileDigest: domainDigest(V3_PROFILE_DOMAIN, {
        sourceDigest,
        artifactSetDigest,
        evidenceManifestDigest,
        spec: source.declaredProfile,
        evidence,
      }),
      spec: source.declaredProfile,
      evidence,
    },
  };
  return {
    ...body,
    registryDigest: domainDigest(V3_REGISTRY_DOMAIN, body),
  } as GeneratorSupplyV3Registry;
}

export type GeneratorSupplyV3AssemblyInputs = Readonly<{
  projection: string;
  darwinOutputDirectory: string;
  linuxOutputDirectory: string;
}>;

export function writeGeneratorSupplyV3Assembly(
  root: string,
  inputs: GeneratorSupplyV3AssemblyInputs,
): void {
  const state = inspectGeneratorSupplyV3AuthorityState(root);
  if (state === "ASSEMBLED_PROFILE_CURRENT") {
    assertSuccessorV3PredecessorsCurrent(root);
    assertGeneratorSupplyV3RegistryCurrent(root);
    return;
  }
  if (state !== "REPLAY_RECEIPTS_PRESENT_UNVERIFIED") {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      "/assembly",
      `v3 assembly requires complete replay receipts and no assembly outputs (state=${state}).`,
    );
  }
  assertSuccessorV3PredecessorsCurrent(root);
  const source = readSource(root);
  validateGeneratorSupplyV3Source(root, source);
  const raw = new Map<string, Buffer>();
  const outputNames = [
    "darwin-a.json",
    "darwin-b.json",
    "darwin-isolation.json",
    "linux-a.json",
    "linux-b.json",
    "linux-isolation.json",
  ];
  const rawPaths = SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.slice(1);
  for (let i = 0; i < outputNames.length; i++) {
    const dir = i < 3 ? inputs.darwinOutputDirectory : inputs.linuxOutputDirectory;
    raw.set(
      rawPaths[i]!,
      readExternalStableRegularFile(resolve(dir, outputNames[i]!), outputNames[i]!),
    );
  }
  raw.set(
    rawPaths[6]!,
    readExternalStableRegularFile(resolve(inputs.projection), "projection receipt"),
  );
  const prepared = buildGeneratorSupplyReplayV3PreparedReceipts(root, source.replayContract, raw);
  const manifest = buildGeneratorSupplyV3EvidenceManifest(prepared);
  const registry = buildGeneratorSupplyV3Registry(source, prepared, manifest);
  const outputs = new Map<string, Buffer>();
  for (const path of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)
    outputs.set(path, Buffer.from(prepared.receipts.get(path)!));
  outputs.set(
    GENERATOR_SUPPLY_V3_EVIDENCE_MANIFEST_PATH,
    Buffer.from(serializeGeneratorSupplyV3Source(manifest)),
  );
  outputs.set(
    GENERATOR_SUPPLY_V3_OUTPUT_PATH,
    Buffer.from(serializeGeneratorSupplyV3Source(registry)),
  );
  for (const [path, bytes] of outputs) {
    writeExclusiveOrNoop(root, path, bytes.toString("utf8"));
  }
  assertGeneratorSupplyV3RegistryCurrent(root);
}

export function buildGeneratorSupplyV3Source(
  _repositoryRoot = resolve(import.meta.dirname, "../.."),
): GeneratorSupplyV3Source {
  const canonicalRoot = resolve(import.meta.dirname, "../..");
  return readJson(canonicalRoot, GENERATOR_SUPPLY_V3_SOURCE_PATH) as GeneratorSupplyV3Source;
}

export function serializeGeneratorSupplyV3Source(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

function assertPreparedReceipts(prepared: GeneratorSupplyReplayV3PreparedReceipts): void {
  if (
    prepared.receiptRecords.length !== SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.length ||
    prepared.receiptRecords.some(
      (record, index) => record.path !== SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[index],
    )
  ) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_EVIDENCE_MISMATCH",
      "/receipts",
      "Prepared v3 receipts must contain exact ordered eight paths.",
    );
  }
}

function readReceiptRecords(root: string): readonly FileRecord[] {
  return SUCCESSOR_V3_REPLAY_RECEIPT_PATHS.map((path) => {
    const bytes = readContainedRegularFile(root, path);
    return {
      path,
      sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      sizeBytes: bytes.byteLength,
    };
  });
}

function schemaValidator(root: string): Ajv2020 {
  try {
    const source = readJson(root, GENERATOR_SUPPLY_V3_SOURCE_SCHEMA_PATH);
    const output = readJson(root, GENERATOR_SUPPLY_V3_OUTPUT_SCHEMA_PATH);
    const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
    ajv.addSchema(source);
    ajv.addSchema(output);
    return ajv;
  } catch (error) {
    throw v3Error("GENERATOR_SUPPLY_V3_SCHEMA_INVALID", "/schemas", String(error));
  }
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const validator = schemaValidator(root).getSchema(schemaId);
  if (!validator || !validator(value)) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_SCHEMA_INVALID",
      `/${schemaId}`,
      "v3 schema validation failed.",
    );
  }
}

function readSource(root: string): GeneratorSupplyV3Source {
  return readJson(root, GENERATOR_SUPPLY_V3_SOURCE_PATH) as GeneratorSupplyV3Source;
}

function readJson(root: string, path: string): JsonRecord {
  try {
    const value = JSON.parse(readContainedRegularFile(root, path).toString("utf8"));
    if (!isRecord(value)) throw new Error("expected object");
    return value;
  } catch (error) {
    if (error instanceof GeneratorSupplyV3Error) throw error;
    throw v3Error("GENERATOR_SUPPLY_V3_SCHEMA_INVALID", `/${path}`, String(error));
  }
}

function readContainedRegularFile(root: string, path: string): Buffer {
  const target = resolveContainedPath(root, path);
  try {
    const before = lstatSync(target, { bigint: true });
    if (!before.isFile() || before.isSymbolicLink())
      throw v3Error(
        "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
        `/${path}`,
        "v3 authority requires regular non-symlink files.",
      );
    const descriptor = openSync(target, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const descriptorBefore = fstatSync(descriptor, { bigint: true });
      if (!sameStableFileIdentity(before, descriptorBefore))
        throw v3Error(
          "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
          `/${path}`,
          "v3 authority changed before stable read.",
        );
      const bytes = readFileSync(descriptor);
      const descriptorAfter = fstatSync(descriptor, { bigint: true });
      const after = lstatSync(target, { bigint: true });
      if (
        !sameStableFileIdentity(descriptorBefore, descriptorAfter) ||
        !sameStableFileIdentity(descriptorBefore, after)
      )
        throw v3Error(
          "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
          `/${path}`,
          "v3 authority changed during stable read.",
        );
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof GeneratorSupplyV3Error) throw error;
    throw v3Error("GENERATOR_SUPPLY_V3_PARTIAL_STATE", `/${path}`, String(error));
  }
}

function readExternalStableRegularFile(path: string, label: string): Buffer {
  const absolute = resolve(path);
  let canonical: string;
  try {
    canonical = realpathSync(absolute);
  } catch (error) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      "/" + label,
      "External receipt is unavailable: " + String(error),
    );
  }
  if (canonical !== absolute) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
      "/" + label,
      "External receipt must not resolve through a symlink or alias.",
    );
  }
  let descriptor: number | undefined;
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (!pathBefore.isFile() || pathBefore.isSymbolicLink())
      throw new Error("External receipt must be a regular non-symlink file.");
    descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (!sameStableFileIdentity(pathBefore, descriptorBefore))
      throw new Error("External receipt changed before stable read.");
    const bytes = readFileSync(descriptor);
    const descriptorAfter = fstatSync(descriptor, { bigint: true });
    const pathAfter = lstatSync(absolute, { bigint: true });
    if (
      !sameStableFileIdentity(descriptorBefore, descriptorAfter) ||
      !sameStableFileIdentity(descriptorBefore, pathAfter) ||
      realpathSync(absolute) !== absolute
    ) {
      throw new Error("External receipt changed during stable read.");
    }
    return bytes;
  } catch (error) {
    if (error instanceof GeneratorSupplyV3Error) throw error;
    throw v3Error("GENERATOR_SUPPLY_V3_PARTIAL_STATE", "/" + label, String(error));
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

function sameStableFileIdentity(
  left: { dev: bigint; ino: bigint; size: bigint; mtimeNs: bigint; ctimeNs: bigint },
  right: { dev: bigint; ino: bigint; size: bigint; mtimeNs: bigint; ctimeNs: bigint },
): boolean {
  return (
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs
  );
}

function resolveContainedPath(root: string, path: string): string {
  const rootReal = realpathSync(root);
  const target = resolve(rootReal, path);
  const rel = relative(rootReal, target).split(sep).join("/");
  if (
    rel === "" ||
    rel === ".." ||
    rel.startsWith("../") ||
    rel.startsWith("/") ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  )
    throw v3Error("GENERATOR_SUPPLY_V3_PARTIAL_STATE", `/${path}`, "Path escapes repository root.");
  const components = rel.split("/");
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      const final = index === components.length - 1;
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory())) {
        throw v3Error(
          "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
          `/${path}`,
          "v3 authority path has an unsafe ancestor.",
        );
      }
    } catch (error) {
      if (error instanceof GeneratorSupplyV3Error) throw error;
      if (error instanceof Error && "code" in error && error.code === "ENOENT") {
        if (index === components.length - 1) return target;
      }
      throw error;
    }
  }
  return target;
}

function ensureContainedParent(root: string, output: string): string {
  const rootReal = realpathSync(root);
  const parent = dirname(output);
  const relation = relative(rootReal, parent).split(sep).join("/");
  if (relation === ".." || relation.startsWith("../") || relation.startsWith("/")) {
    throw v3Error(
      "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
      `/${relation}`,
      "Output parent escaped the repository root.",
    );
  }
  let current = rootReal;
  for (const component of relation === "" ? [] : relation.split("/")) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      if (!stat.isDirectory() || stat.isSymbolicLink()) {
        throw v3Error(
          "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
          `/${relation}`,
          "Output parent must be a regular non-symlink directory.",
        );
      }
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
      mkdirSync(current, { mode: 0o755 });
      const created = lstatSync(current);
      if (!created.isDirectory() || created.isSymbolicLink())
        throw v3Error(
          "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
          `/${relation}`,
          "Output parent creation was not a regular directory.",
        );
    }
  }
  return parent;
}

function writeExclusiveOrNoop(root: string, path: string, text: string): "written" | "current" {
  const output = resolveContainedPath(root, path);
  const parent = ensureContainedParent(root, output);
  const bytes = Buffer.from(text, "utf8");
  try {
    const existing = lstatSync(output, { bigint: true });
    if (!existing.isFile() || existing.isSymbolicLink())
      throw v3Error(
        "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
        `/${path}`,
        "Existing output must be a regular non-symlink file.",
      );
    if (readContainedRegularFile(root, path).equals(bytes)) return "current";
    throw v3Error(
      "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
      `/${path}`,
      "Existing v3 output differs; in-place replacement is forbidden.",
    );
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) throw error;
  }

  let descriptor: number | undefined;
  try {
    descriptor = openSync(
      output,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
      0o644,
    );
    writeFileSync(descriptor, bytes);
    fsyncSync(descriptor);
    closeSync(descriptor);
    descriptor = undefined;
    const parentDescriptor = openSync(parent, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      fsyncSync(parentDescriptor);
    } finally {
      closeSync(parentDescriptor);
    }
    return "written";
  } catch (error) {
    if (descriptor !== undefined) closeSync(descriptor);
    if (error instanceof Error && "code" in error && error.code === "EEXIST") {
      const current = readContainedRegularFile(root, path);
      if (current.equals(bytes)) return "current";
      throw v3Error(
        "GENERATOR_SUPPLY_V3_WRITE_CONFLICT",
        `/${path}`,
        "Concurrent output bytes differ; refusing overwrite.",
      );
    }
    throw error;
  }
}

function filePresence(root: string, path: string): boolean {
  try {
    readContainedRegularFile(root, path);
    return true;
  } catch (error) {
    if (
      error instanceof Error &&
      "code" in error &&
      (error as NodeJS.ErrnoException).code === "ENOENT"
    )
      return false;
    if (error instanceof GeneratorSupplyV3Error && String(error.message).includes("ENOENT"))
      return false;
    throw error;
  }
}

function groupPresence(
  root: string,
  paths: readonly string[],
  errorPath: string,
): "NONE" | "ALL" | "PARTIAL" {
  const count = paths.filter((path) => filePresence(root, path)).length;
  if (count === 0) return "NONE";
  if (count === paths.length) return "ALL";
  throw v3Error(
    "GENERATOR_SUPPLY_V3_PARTIAL_STATE",
    errorPath,
    "Late-bound v3 path group is partial.",
  );
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  return Buffer.from(canonicalizeJson(left)).equals(Buffer.from(canonicalizeJson(right)));
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function v3Error(
  code: GeneratorSupplyV3Error["code"],
  path: string,
  message: string,
): GeneratorSupplyV3Error {
  return new GeneratorSupplyV3Error(code, path, message);
}
