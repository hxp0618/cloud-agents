import { createHash } from "node:crypto";
import {
  closeSync,
  constants,
  fsyncSync,
  fstatSync,
  linkSync,
  lstatSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  realpathSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { basename, dirname, isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  assertGeneratorSupplyReplayV2ContractCurrent,
  assertGeneratorSupplyReplayV2Receipts,
  buildGeneratorSupplyReplayV2PreparedReceipts,
  buildGeneratorSupplyReplayV2ExpectedFromImmutableV1,
  type GeneratorSupplyReplayV2Contract,
  type GeneratorSupplyReplayV2PreparedReceipts,
  type GeneratorSupplyReplayV2Validation,
} from "./platform-generator-supply-replay-v2";
import {
  SUCCESSOR_ASSEMBLY_PATHS,
  SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag";
import {
  assertGeneratorSupplyV1PredecessorImmutable,
  captureGeneratorSupplyV1PredecessorSnapshot,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_GIT_LINEAGE,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
} from "./platform-successor-predecessor";

export const GENERATOR_SUPPLY_V2_SOURCE_PATH = "tools/generator-supply/v2/source.json";
export const GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH =
  "tools/generator-supply/v2/generator-supply-profile-source-v2.schema.json";
export const GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH =
  "tools/generator-supply/v2/generator-supply-profile-v2.schema.json";
export const GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH = SUCCESSOR_ASSEMBLY_PATHS[0];
export const GENERATOR_SUPPLY_V2_OUTPUT_PATH = SUCCESSOR_ASSEMBLY_PATHS[1];

export const GENERATOR_SUPPLY_V2_REPLAY_CONTRACT = {
  authorityFiles: {
    wrapper: {
      path: "scripts/replay-platform-generators-isolated.sh",
      sha256: "b4c0f23c45c2a3a1a391daadcc44554793fda948168f35f3ffaf4d32cedd9070",
      sizeBytes: 83_800,
    },
    runner: {
      path: "scripts/replay-platform-generators.ts",
      sha256: "96bc41cd702a35b0c4febfd62c48e0e261fc0656f6f91583522eb47e96cf07a1",
      sizeBytes: 41_526,
    },
    pathHelper: {
      path: "scripts/lib/generator-replay-path-authority.ts",
      sha256: "4cde1599b7e909ef0070b81090d40c2e2c7c1c43af64cebd3c53391489a6fccf",
      sizeBytes: 4_282,
    },
    archiveInspector: {
      path: "scripts/lib/inspect-generator-replay-archive.py",
      sha256: "db932a113dda469367f25c71b56ff28ee8f2245821fceb840c49340ef6c10f31",
      sizeBytes: 10_860,
    },
  },
  wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V1",
  authoritativeReplayScope: "CORE_GENERATORS_ONLY_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY",
  algorithms: {
    nodeModulesManifest: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
    projectionArchiveMemberManifest:
      "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
    inputTreeManifest: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
  },
  projectionExclusions: [...SUCCESSOR_PROJECTION_EXCLUSIONS],
  receiptFormats: {
    summary: "cloud-agents-generator-supply-replay-summary/v2",
    run: "cloud-agents-generator-replay-run/v1",
    isolation: "cloud-agents-generator-replay-isolation/v1",
    projection: "cloud-agents-core-generator-projection/v1",
  },
} as const;

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v2/generator-supply-profile-source-v2.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v2/generator-supply-profile-v2.schema.json";
const REGISTRY_ID = "cloud-agents/generator-supply-profile";
const PROFILE_ID = "cloud-agents/generator-supply-profile/v2";
const EVIDENCE_MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-size-v1";

type FileRecord = JsonRecord & {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

type EvidenceManifest = JsonRecord & {
  readonly algorithm: string;
  readonly files: readonly FileRecord[];
};

export type GeneratorSupplyV2Source = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly predecessor: JsonRecord & {
    readonly profileId: string;
    readonly predecessorMutation: string;
    readonly outerFiles: readonly FileRecord[];
    readonly evidenceManifestPolicy: JsonRecord;
    readonly profileIdentities: JsonRecord;
    readonly fixedLineage: JsonRecord;
    readonly projection: JsonRecord;
  };
  readonly inheritance: JsonRecord;
  readonly replayContract: GeneratorSupplyReplayV2Contract;
  readonly declaredProfile: JsonRecord & {
    readonly profileId: string;
    readonly status: string;
    readonly notGateClosure: boolean;
    readonly platforms: readonly JsonRecord[];
    readonly boundaries: JsonRecord;
  };
  readonly replayEvidence: JsonRecord & {
    readonly state: string;
    readonly authority: string;
    readonly receiptPaths: readonly string[];
  };
};

export type GeneratorSupplyV2Registry = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly predecessor: JsonRecord;
  readonly sourceDigest: string;
  readonly artifactSetDigest: string;
  readonly evidenceManifestDigest: string;
  readonly profile: JsonRecord & {
    readonly profileDigest: string;
    readonly spec: JsonRecord;
    readonly evidence: JsonRecord & {
      readonly state: string;
      readonly inheritance: JsonRecord;
      readonly receipts: readonly FileRecord[];
      readonly evidenceManifest: EvidenceManifest;
    };
  };
  readonly registryDigest: string;
};

export type GeneratorSupplyV2AuthorityState =
  | "SCHEMA_ONLY"
  | "DECLARED_PRE_REPLAY"
  | "REPLAY_RECEIPTS_PRESENT_UNVERIFIED"
  | "ASSEMBLED_PROFILE_CURRENT";

export type GeneratorSupplyV2CurrentValidation = Readonly<{
  registry: GeneratorSupplyV2Registry;
  fileSha256: string;
  candidateManifestSha256: string;
  outputFiles: number;
  assertCurrent: () => void;
}>;

type GeneratorSupplyV2SemanticAuthority = Readonly<{
  candidateManifestSha256: string;
  outputFiles: number;
  assertCurrent: () => void;
}>;

type GeneratorSupplyV2StableFileIdentity = Readonly<{
  rootReal: string;
  path: string;
  absolute: string;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

export type GeneratorSupplyV2AssemblyInputs = Readonly<{
  projection: string;
  darwinOutputDirectory: string;
  linuxOutputDirectory: string;
}>;

export type GeneratorSupplyV2AssemblyWriteHooks = Readonly<{
  afterRawSnapshot?: () => void;
  beforeCapturedSchemaValidation?: (phase: "source" | "output") => void;
  afterCapturedSchemaValidation?: (phase: "source" | "output") => void;
  beforePublish?: (path: string, index: number, temporary: string, output: string) => void;
  afterPublish?: (path: string, index: number) => void;
}>;

type ExternalRawIdentity = Readonly<{
  path: string;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

type ParentDirectoryIdentity = Readonly<{
  path: string;
  dev: bigint;
  ino: bigint;
}>;

type ExternalTopologyIdentity = Readonly<{
  path: string;
  kind: "file" | "directory";
  dev: bigint;
  ino: bigint;
}>;

type OwnedTemporaryIdentity = Readonly<{
  path: string;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

export class GeneratorSupplyV2Error extends Error {
  constructor(
    readonly code:
      | "GENERATOR_SUPPLY_V2_SCHEMA_INVALID"
      | "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH"
      | "GENERATOR_SUPPLY_V2_PREDECESSOR_MISMATCH"
      | "GENERATOR_SUPPLY_V2_PARTIAL_STATE"
      | "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH"
      | "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH"
      | "GENERATOR_SUPPLY_V2_RAW_INPUT_INVALID"
      | "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyV2Error";
  }
}

export function inspectGeneratorSupplyV2AuthorityState(
  root: string,
): GeneratorSupplyV2AuthorityState {
  schemaValidator(root);
  const sourcePresent = filePresence(root, GENERATOR_SUPPLY_V2_SOURCE_PATH);
  const receipts = groupPresence(root, SUCCESSOR_REPLAY_RECEIPT_PATHS, "/replayEvidence");
  const assembly = groupPresence(root, SUCCESSOR_ASSEMBLY_PATHS, "/assembly");

  if (!sourcePresent) {
    if (receipts !== "NONE" || assembly !== "NONE") {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
        "/source",
        "Generator-supply v2 late-bound artifacts cannot exist before the authority source.",
      );
    }
    return "SCHEMA_ONLY";
  }

  const source = readSource(root);
  validateGeneratorSupplyV2Source(root, source);
  if (receipts === "NONE" && assembly === "NONE") return "DECLARED_PRE_REPLAY";
  if (receipts !== "ALL") {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      "/replayEvidence",
      "All eight exact fresh replay receipts must be present together.",
    );
  }
  if (assembly === "NONE") return "REPLAY_RECEIPTS_PRESENT_UNVERIFIED";
  if (assembly !== "ALL") {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      "/assembly",
      "The v2 evidence manifest and profile must be present together.",
    );
  }
  assertGeneratorSupplyV2RegistryCurrent(root);
  return "ASSEMBLED_PROFILE_CURRENT";
}

export function assertGeneratorSupplyV2SourceCurrent(
  root: string,
): GeneratorSupplyV2AuthorityState {
  const state = inspectGeneratorSupplyV2AuthorityState(root);
  if (state === "SCHEMA_ONLY") {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      `/${GENERATOR_SUPPLY_V2_SOURCE_PATH}`,
      "Generator-supply v2 pre-replay source authority is absent.",
    );
  }
  return state;
}

export function writeGeneratorSupplyV2Source(root: string): void {
  const state = inspectGeneratorSupplyV2AuthorityState(root);
  if (state !== "SCHEMA_ONLY" && state !== "DECLARED_PRE_REPLAY") {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      `/${GENERATOR_SUPPLY_V2_SOURCE_PATH}`,
      "Generator-supply v2 source cannot be rewritten after replay or assembly begins.",
    );
  }
  const output = resolveContainedPath(root, GENERATOR_SUPPLY_V2_SOURCE_PATH, true);
  writeFileSync(output, serializeGeneratorSupplyV2Source(buildGeneratorSupplyV2Source()));
  if (assertGeneratorSupplyV2SourceCurrent(root) !== "DECLARED_PRE_REPLAY") {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      `/${GENERATOR_SUPPLY_V2_SOURCE_PATH}`,
      "Generator-supply v2 source write did not reach declared pre-replay state.",
    );
  }
}

export function validateGeneratorSupplyV2Source(
  root: string,
  source: GeneratorSupplyV2Source,
): void {
  validateGeneratorSupplyV2SourceInternal(root, source, schemaValidator(root));
}

function validateGeneratorSupplyV2SourceInternal(
  root: string,
  source: GeneratorSupplyV2Source,
  validator: Ajv2020,
): void {
  validateAgainstCompiledSchema(validator, SOURCE_SCHEMA_ID, source);
  const expected = buildGeneratorSupplyV2TestSource();
  if (!canonicalEqual(source, expected)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      "/source",
      "Generator-supply v2 source must match the exact typed predecessor, inheritance, replay, and non-Gate authority.",
    );
  }
  try {
    assertGeneratorSupplyV1PredecessorImmutable(root);
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PREDECESSOR_MISMATCH",
      "/predecessor",
      `Generator-supply v1 predecessor verification failed: ${String(error)}.`,
    );
  }
  try {
    assertGeneratorSupplyReplayV2ContractCurrent(root, source.replayContract);
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      "/replayContract",
      `Generator-supply v2 replay contract or source-bound authority drifted: ${String(error)}.`,
    );
  }
  assertNoV2AuthorityIsExcluded();
}

export function assertGeneratorSupplyV2RegistrySemantics(
  root: string,
  document: unknown,
): asserts document is GeneratorSupplyV2Registry {
  const source = readSource(root);
  const standaloneManifest = readJsonFile(
    root,
    GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
  ) as EvidenceManifest;
  assertGeneratorSupplyV2RegistrySemanticsInternal(root, document, source, standaloneManifest);
}

export function assertGeneratorSupplyV2RegistryCurrent(
  root: string,
  document?: unknown,
): GeneratorSupplyV2CurrentValidation {
  return assertGeneratorSupplyV2RegistryCurrentInternal(root, document);
}

export function assertGeneratorSupplyV2CurrentSnapshotMutationForTest(
  root: string,
  document: unknown,
  mutateAfterOuterSnapshot: () => void,
): void {
  assertGeneratorSupplyV2RegistryCurrentInternal(root, document, mutateAfterOuterSnapshot);
}

function assertGeneratorSupplyV2RegistryCurrentInternal(
  root: string,
  suppliedDocument?: unknown,
  mutateAfterOuterSnapshot?: () => void,
): GeneratorSupplyV2CurrentValidation {
  const identities: GeneratorSupplyV2StableFileIdentity[] = [];
  const source = readSource(root, identities);
  const standaloneManifest = readJsonFile(
    root,
    GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
    identities,
  ) as EvidenceManifest;
  const outputSnapshot = readJsonFileSnapshot(root, GENERATOR_SUPPLY_V2_OUTPUT_PATH, identities);
  const registry = outputSnapshot.value as GeneratorSupplyV2Registry;
  if (suppliedDocument !== undefined && !canonicalEqual(suppliedDocument, registry)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/registry",
      "Supplied generator-supply v2 registry does not match the current output snapshot.",
    );
  }
  mutateAfterOuterSnapshot?.();
  const semanticAuthority = assertGeneratorSupplyV2RegistrySemanticsInternal(
    root,
    registry,
    source,
    standaloneManifest,
  );
  const assertCurrent = (): void => {
    try {
      assertGeneratorSupplyV1PredecessorImmutable(root);
    } catch (error) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_PREDECESSOR_MISMATCH",
        "/predecessor",
        `Generator-supply v1 predecessor changed before the current profile could be accepted: ${String(error)}.`,
      );
    }
    assertGeneratorSupplyV2OuterSnapshotCurrent(root, identities);
    semanticAuthority.assertCurrent();
  };
  assertCurrent();
  return {
    registry,
    fileSha256: outputSnapshot.fileSha256,
    candidateManifestSha256: semanticAuthority.candidateManifestSha256,
    outputFiles: semanticAuthority.outputFiles,
    assertCurrent,
  };
}

function assertGeneratorSupplyV2RegistrySemanticsInternal(
  root: string,
  document: unknown,
  source: GeneratorSupplyV2Source,
  standaloneManifest: EvidenceManifest,
): GeneratorSupplyV2SemanticAuthority {
  validateGeneratorSupplyV2Source(root, source);
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, document);
  if (!isRecord(document)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/",
      "Generator-supply v2 registry must be an object.",
    );
  }
  const registry = document as GeneratorSupplyV2Registry;
  if (
    registry.formatVersion !== "cloud-agents-generator-supply-profile-registry/v2" ||
    registry.registryId !== REGISTRY_ID ||
    !canonicalEqual(registry.predecessor, source.predecessor) ||
    !canonicalEqual(registry.profile.spec, source.declaredProfile) ||
    registry.profile.evidence.state !== "ASSEMBLED_LATE_BOUND" ||
    !canonicalEqual(registry.profile.evidence.inheritance, source.inheritance)
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/",
      "Generator-supply v2 registry identity, predecessor, profile spec, or inheritance drifted.",
    );
  }

  const receipts = registry.profile.evidence.receipts;
  let semanticReceiptRecords: readonly {
    readonly path: string;
    readonly sha256: string;
    readonly sizeBytes: number;
  }[];
  let semanticValidation: GeneratorSupplyReplayV2Validation;
  try {
    const expected = buildGeneratorSupplyReplayV2ExpectedFromImmutableV1(
      root,
      source.replayContract,
    );
    semanticValidation = assertGeneratorSupplyReplayV2Receipts(root, expected);
    semanticReceiptRecords = semanticValidation.receiptRecords;
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/receipts",
      `Generator-supply v2 replay receipt semantics failed closed: ${String(error)}.`,
    );
  }
  assertExactReceiptRecords(receipts, semanticReceiptRecords);
  const embeddedManifest = registry.profile.evidence.evidenceManifest;
  if (
    embeddedManifest.algorithm !== EVIDENCE_MANIFEST_ALGORITHM ||
    !canonicalEqual(embeddedManifest.files, receipts) ||
    !canonicalEqual(standaloneManifest, embeddedManifest)
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/evidenceManifest",
      "The standalone and embedded v2 evidence manifests must bind the exact eight receipts.",
    );
  }

  const expectedSourceDigest = domainDigest("cloud-agents/generator-supply/source/v2", source);
  const expectedArtifactSetDigest = domainDigest("cloud-agents/generator-supply/artifact-set/v2", {
    predecessor: source.predecessor,
    inheritance: source.inheritance,
    receipts,
  });
  const expectedEvidenceManifestDigest = domainDigest(
    "cloud-agents/generator-supply/evidence-manifest/v2",
    embeddedManifest,
  );
  const expectedProfileDigest = domainDigest("cloud-agents/generator-supply/profile/v2", {
    sourceDigest: registry.sourceDigest,
    artifactSetDigest: registry.artifactSetDigest,
    evidenceManifestDigest: registry.evidenceManifestDigest,
    spec: registry.profile.spec,
    evidence: registry.profile.evidence,
  });
  if (
    registry.sourceDigest !== expectedSourceDigest ||
    registry.artifactSetDigest !== expectedArtifactSetDigest ||
    registry.evidenceManifestDigest !== expectedEvidenceManifestDigest ||
    registry.profile.profileDigest !== expectedProfileDigest
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/profile",
      "Generator-supply v2 domain digests do not bind the exact source, evidence, and profile.",
    );
  }
  const { registryDigest: _registryDigest, ...body } = registry;
  const expectedRegistryDigest = domainDigest("cloud-agents/generator-supply/registry/v2", body);
  if (registry.registryDigest !== expectedRegistryDigest) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/registryDigest",
      "Generator-supply v2 registry digest does not bind the complete registry body.",
    );
  }
  try {
    assertGeneratorSupplyV1PredecessorImmutable(root);
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PREDECESSOR_MISMATCH",
      "/predecessor",
      `Generator-supply v1 predecessor changed before the assembled profile could be accepted: ${String(error)}.`,
    );
  }
  try {
    semanticValidation.assertSnapshotCurrent();
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/receipts",
      `Generator-supply v2 semantic input snapshot changed before the assembled profile could be accepted: ${String(error)}.`,
    );
  }
  const runPath = SUCCESSOR_REPLAY_RECEIPT_PATHS[1];
  const runSnapshot = readJsonFileSnapshot(root, runPath);
  const semanticRunRecord = semanticValidation.receiptRecords.find(
    (record) => record.path === runPath,
  );
  const outputFiles = Number(runSnapshot.value.outputFiles);
  if (
    semanticRunRecord?.sha256 !== runSnapshot.fileSha256 ||
    runSnapshot.value.candidateManifestSha256 !== semanticValidation.candidateManifestSha256 ||
    outputFiles !== SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS.length
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/receipts/run",
      "The exact semantic replay run must bind the candidate manifest and all 49 core outputs.",
    );
  }
  return {
    candidateManifestSha256: semanticValidation.candidateManifestSha256,
    outputFiles,
    assertCurrent: semanticValidation.assertSnapshotCurrent,
  };
}

export function buildGeneratorSupplyV2EvidenceManifest(
  prepared: GeneratorSupplyReplayV2PreparedReceipts,
): EvidenceManifest {
  prepared.assertPreparedSnapshotCurrent();
  assertPreparedReceiptSet(prepared);
  return {
    algorithm: EVIDENCE_MANIFEST_ALGORITHM,
    files: prepared.receiptRecords.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
  };
}

export function buildGeneratorSupplyV2Registry(
  source: GeneratorSupplyV2Source,
  prepared: GeneratorSupplyReplayV2PreparedReceipts,
  evidenceManifest: EvidenceManifest = buildGeneratorSupplyV2EvidenceManifest(prepared),
): GeneratorSupplyV2Registry {
  prepared.assertPreparedSnapshotCurrent();
  assertPreparedReceiptSet(prepared);
  const receipts = evidenceManifest.files;
  if (
    evidenceManifest.algorithm !== EVIDENCE_MANIFEST_ALGORITHM ||
    !canonicalEqual(evidenceManifest, buildGeneratorSupplyV2EvidenceManifest(prepared))
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/assembly/evidenceManifest",
      "Generator-supply v2 assembly must bind the exact prepared receipt bytes.",
    );
  }
  const sourceDigest = domainDigest("cloud-agents/generator-supply/source/v2", source);
  const artifactSetDigest = domainDigest("cloud-agents/generator-supply/artifact-set/v2", {
    predecessor: source.predecessor,
    inheritance: source.inheritance,
    receipts,
  });
  const evidenceManifestDigest = domainDigest(
    "cloud-agents/generator-supply/evidence-manifest/v2",
    evidenceManifest,
  );
  const evidence = {
    state: "ASSEMBLED_LATE_BOUND",
    inheritance: source.inheritance,
    receipts,
    evidenceManifest,
  } as const;
  const body = {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v2",
    registryId: REGISTRY_ID,
    predecessor: source.predecessor,
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    profile: {
      profileDigest: domainDigest("cloud-agents/generator-supply/profile/v2", {
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
    registryDigest: domainDigest("cloud-agents/generator-supply/registry/v2", body),
  } as GeneratorSupplyV2Registry;
}

export function writeGeneratorSupplyV2Assembly(
  root: string,
  inputs: GeneratorSupplyV2AssemblyInputs,
): void {
  writeGeneratorSupplyV2AssemblyInternal(root, inputs, {});
}

export function writeGeneratorSupplyV2AssemblyForTest(
  root: string,
  inputs: GeneratorSupplyV2AssemblyInputs,
  hooks: GeneratorSupplyV2AssemblyWriteHooks,
): void {
  writeGeneratorSupplyV2AssemblyInternal(root, inputs, hooks);
}

function writeGeneratorSupplyV2AssemblyInternal(
  root: string,
  inputs: GeneratorSupplyV2AssemblyInputs,
  hooks: GeneratorSupplyV2AssemblyWriteHooks,
): void {
  const rootReal = realpathSync(root);
  const authorityIdentities: GeneratorSupplyV2StableFileIdentity[] = [];
  const source = readSource(rootReal, authorityIdentities);
  const sourceSchemaBytes = Buffer.from(
    readContainedRegularFile(
      rootReal,
      GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
      undefined,
      authorityIdentities,
    ),
  );
  const outputSchemaBytes = Buffer.from(
    readContainedRegularFile(
      rootReal,
      GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
      undefined,
      authorityIdentities,
    ),
  );
  const capturedSchemaValidator = schemaValidatorFromBytes(sourceSchemaBytes, outputSchemaBytes);
  const predecessorSnapshot = captureGeneratorSupplyV1PredecessorSnapshot(rootReal);
  withCapturedSchemaValidationHooks(hooks, "source", () =>
    validateGeneratorSupplyV2SourceInternal(rootReal, source, capturedSchemaValidator),
  );
  predecessorSnapshot.assertCurrent();

  const raw = readGeneratorSupplyV2RawInputs(rootReal, inputs);
  hooks.afterRawSnapshot?.();
  raw.assertCurrent();
  const prepared = buildGeneratorSupplyReplayV2PreparedReceipts(
    rootReal,
    GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
    raw.bytes,
  );
  prepared.assertPreparedSnapshotCurrent();
  assertPreparedReceiptSet(prepared);
  prepared.assertInputSnapshotCurrent();
  predecessorSnapshot.assertCurrent();
  raw.assertCurrent();
  assertGeneratorSupplyV2AuthoritySnapshotCurrent(rootReal, authorityIdentities);

  const evidenceManifest = buildGeneratorSupplyV2EvidenceManifest(prepared);
  const registry = buildGeneratorSupplyV2Registry(source, prepared, evidenceManifest);
  prepared.assertPreparedSnapshotCurrent();
  predecessorSnapshot.assertCurrent();
  withCapturedSchemaValidationHooks(hooks, "output", () =>
    validateAgainstCompiledSchema(capturedSchemaValidator, OUTPUT_SCHEMA_ID, registry),
  );
  const outputs = new Map<string, Buffer>();
  for (const path of SUCCESSOR_REPLAY_RECEIPT_PATHS) {
    const bytes = prepared.receipts.get(path);
    if (!bytes) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
        `/${path}`,
        "Prepared generator-supply v2 receipt bytes are absent.",
      );
    }
    outputs.set(path, Buffer.from(bytes));
  }
  outputs.set(
    GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
    Buffer.from(serializeGeneratorSupplyV2Source(evidenceManifest), "utf8"),
  );
  outputs.set(
    GENERATOR_SUPPLY_V2_OUTPUT_PATH,
    Buffer.from(serializeGeneratorSupplyV2Source(registry), "utf8"),
  );
  const orderedPaths = [...SUCCESSOR_REPLAY_RECEIPT_PATHS, ...SUCCESSOR_ASSEMBLY_PATHS];
  if (
    outputs.size !== 10 ||
    orderedPaths.length !== 10 ||
    orderedPaths.some((path) => !outputs.has(path))
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
      "/assembly",
      "Generator-supply v2 writer must publish exactly the ten DAG late-bound paths.",
    );
  }

  const parentIdentityMap = new Map<string, ParentDirectoryIdentity>();
  for (const path of orderedPaths) {
    for (const identity of ensureGeneratorSupplyV2ParentDirectories(
      rootReal,
      dirname(resolve(rootReal, path)),
    )) {
      const previous = parentIdentityMap.get(identity.path);
      if (previous && (previous.dev !== identity.dev || previous.ino !== identity.ino)) {
        throw writeConflict(identity.path, "Destination parent changed during initial capture.");
      }
      parentIdentityMap.set(identity.path, identity);
    }
  }
  const parentIdentities = [...parentIdentityMap.values()];
  assertParentDirectoriesCurrent(parentIdentities);
  const outputIdentities: GeneratorSupplyV2StableFileIdentity[] = [];
  for (const [index, path] of orderedPaths.entries()) {
    prepared.assertPreparedSnapshotCurrent();
    prepared.assertInputSnapshotCurrent();
    predecessorSnapshot.assertCurrent();
    raw.assertCurrent();
    assertGeneratorSupplyV2AuthoritySnapshotCurrent(rootReal, authorityIdentities);
    assertParentDirectoriesCurrent(parentIdentities);
    assertPublishedOutputsCurrent(rootReal, outputIdentities);
    outputIdentities.push(
      publishGeneratorSupplyV2FileAppendOnly(
        rootReal,
        path,
        outputs.get(path)!,
        index,
        hooks,
        parentIdentities,
      ),
    );
    prepared.assertPreparedSnapshotCurrent();
    prepared.assertInputSnapshotCurrent();
    predecessorSnapshot.assertCurrent();
    raw.assertCurrent();
    assertGeneratorSupplyV2AuthoritySnapshotCurrent(rootReal, authorityIdentities);
    assertParentDirectoriesCurrent(parentIdentities);
    assertPublishedOutputsCurrent(rootReal, outputIdentities);
  }

  prepared.assertPreparedSnapshotCurrent();
  prepared.assertInputSnapshotCurrent();
  predecessorSnapshot.assertCurrent();
  raw.assertCurrent();
  assertGeneratorSupplyV2AuthoritySnapshotCurrent(rootReal, authorityIdentities);
  assertParentDirectoriesCurrent(parentIdentities);
  assertPublishedOutputsCurrent(rootReal, outputIdentities);
  const current = assertGeneratorSupplyV2RegistryCurrent(rootReal, registry);
  current.assertCurrent();
}

function withCapturedSchemaValidationHooks(
  hooks: GeneratorSupplyV2AssemblyWriteHooks,
  phase: "source" | "output",
  validate: () => void,
): void {
  try {
    hooks.beforeCapturedSchemaValidation?.(phase);
    validate();
  } finally {
    hooks.afterCapturedSchemaValidation?.(phase);
  }
}

function assertPreparedReceiptSet(prepared: GeneratorSupplyReplayV2PreparedReceipts): void {
  if (
    prepared.receipts.size !== SUCCESSOR_REPLAY_RECEIPT_PATHS.length ||
    prepared.receiptRecords.length !== SUCCESSOR_REPLAY_RECEIPT_PATHS.length
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/assembly/receipts",
      "Prepared generator-supply v2 receipts must contain exactly eight entries.",
    );
  }
  for (const [index, path] of SUCCESSOR_REPLAY_RECEIPT_PATHS.entries()) {
    const bytes = prepared.receipts.get(path);
    const record = prepared.receiptRecords[index];
    if (
      !bytes ||
      record?.path !== path ||
      record.sizeBytes !== bytes.byteLength ||
      record.sha256 !== `sha256:${createHash("sha256").update(bytes).digest("hex")}`
    ) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
        `/assembly/receipts/${index}`,
        "Prepared generator-supply v2 receipt order, digest, or size drifted.",
      );
    }
  }
}

function readGeneratorSupplyV2RawInputs(
  rootReal: string,
  inputs: GeneratorSupplyV2AssemblyInputs,
): Readonly<{
  bytes: ReadonlyMap<string, Buffer>;
  assertCurrent: () => void;
}> {
  const topology = new Map<string, ExternalTopologyIdentity>();
  const projection = assertCanonicalExternalPath(rootReal, inputs.projection, "file", topology);
  const darwin = assertCanonicalExternalPath(
    rootReal,
    inputs.darwinOutputDirectory,
    "directory",
    topology,
  );
  const linux = assertCanonicalExternalPath(
    rootReal,
    inputs.linuxOutputDirectory,
    "directory",
    topology,
  );
  if (new Set([projection, darwin, linux]).size !== 3) {
    throw rawInputError("/raw", "Projection and platform output roots must be distinct.");
  }
  const expectedDirectoryEntries = {
    [darwin]: ["darwin-a.json", "darwin-b.json", "darwin-isolation.json"],
    [linux]: ["linux-a.json", "linux-b.json", "linux-isolation.json"],
  } as const;
  for (const [directory, expected] of Object.entries(expectedDirectoryEntries)) {
    let actual: string[];
    try {
      actual = readdirSync(directory).toSorted((left, right) =>
        Buffer.compare(Buffer.from(left), Buffer.from(right)),
      );
    } catch (error) {
      throw rawInputError(
        directory,
        `Raw output directory cannot be enumerated: ${String(error)}.`,
      );
    }
    const orderedExpected = [...expected].toSorted((left, right) =>
      Buffer.compare(Buffer.from(left), Buffer.from(right)),
    );
    if (!canonicalEqual(actual, orderedExpected)) {
      throw rawInputError(
        directory,
        "Raw output directory must contain exactly its three named platform receipts.",
      );
    }
  }
  const [, darwinA, darwinB, darwinIsolation, linuxA, linuxB, linuxIsolation, projectionPath] =
    SUCCESSOR_REPLAY_RECEIPT_PATHS;
  const mapping = [
    [darwinA, resolve(darwin, "darwin-a.json")],
    [darwinB, resolve(darwin, "darwin-b.json")],
    [darwinIsolation, resolve(darwin, "darwin-isolation.json")],
    [linuxA, resolve(linux, "linux-a.json")],
    [linuxB, resolve(linux, "linux-b.json")],
    [linuxIsolation, resolve(linux, "linux-isolation.json")],
    [projectionPath, projection],
  ] as const;
  const identities: ExternalRawIdentity[] = [];
  const bytes = new Map<string, Buffer>();
  for (const [receiptPath, externalPath] of mapping) {
    const snapshot = readExternalRawFileOnce(rootReal, externalPath);
    bytes.set(receiptPath, snapshot.bytes);
    identities.push(snapshot.identity);
  }
  const assertCurrent = (): void => {
    assertExternalTopologyCurrent(topology);
    for (const identity of identities) assertExternalRawIdentityCurrent(rootReal, identity);
    for (const [directory, expected] of Object.entries(expectedDirectoryEntries)) {
      const actual = readdirSync(directory).toSorted((left, right) =>
        Buffer.compare(Buffer.from(left), Buffer.from(right)),
      );
      const orderedExpected = [...expected].toSorted((left, right) =>
        Buffer.compare(Buffer.from(left), Buffer.from(right)),
      );
      if (!canonicalEqual(actual, orderedExpected)) {
        throw rawInputError(directory, "Raw output directory entries changed after snapshot.");
      }
    }
  };
  assertCurrent();
  return { bytes, assertCurrent };
}

function assertCanonicalExternalPath(
  rootReal: string,
  value: string,
  kind: "file" | "directory",
  identities?: Map<string, ExternalTopologyIdentity>,
): string {
  if (!isAbsolute(value) || resolve(value) !== value || value.includes("\\")) {
    throw rawInputError(value, "Raw input paths must be absolute canonical paths.");
  }
  const relation = relative(rootReal, value);
  if (relation === "" || (!relation.startsWith(`..${sep}`) && relation !== "..")) {
    throw rawInputError(value, "Raw input paths must remain external to the repository root.");
  }
  let current: string = sep;
  const components = value.split(sep).filter(Boolean);
  try {
    for (const [index, component] of components.entries()) {
      current = resolve(current, component);
      const stat = lstatSync(current);
      const final = index === components.length - 1;
      if (
        stat.isSymbolicLink() ||
        (!final && !stat.isDirectory()) ||
        (final && (kind === "file" ? !stat.isFile() : !stat.isDirectory()))
      ) {
        throw new Error("path topology is not the required no-symlink type");
      }
      const topologyIdentity: ExternalTopologyIdentity = {
        path: current,
        kind: final ? kind : "directory",
        dev: BigInt(stat.dev),
        ino: BigInt(stat.ino),
      };
      const previous = identities?.get(current);
      if (
        previous &&
        (previous.kind !== topologyIdentity.kind ||
          previous.dev !== topologyIdentity.dev ||
          previous.ino !== topologyIdentity.ino)
      ) {
        throw new Error("shared raw input ancestor identity drifted");
      }
      identities?.set(current, topologyIdentity);
    }
    if (realpathSync(value) !== value) throw new Error("real path is not canonical");
  } catch (error) {
    throw rawInputError(value, `Raw input path is unsafe or unavailable: ${String(error)}.`);
  }
  return value;
}

function assertExternalTopologyCurrent(
  identities: ReadonlyMap<string, ExternalTopologyIdentity>,
): void {
  for (const identity of identities.values()) {
    try {
      const stat = lstatSync(identity.path, { bigint: true });
      if (
        stat.isSymbolicLink() ||
        (identity.kind === "file" ? !stat.isFile() : !stat.isDirectory()) ||
        stat.dev !== identity.dev ||
        stat.ino !== identity.ino ||
        realpathSync(identity.path) !== identity.path
      ) {
        throw new Error("raw topology identity changed");
      }
    } catch (error) {
      throw rawInputError(
        identity.path,
        `Raw input ancestor or directory snapshot is no longer current: ${String(error)}.`,
      );
    }
  }
}

function readExternalRawFileOnce(
  rootReal: string,
  path: string,
): Readonly<{ bytes: Buffer; identity: ExternalRawIdentity }> {
  assertCanonicalExternalPath(rootReal, path, "file");
  let descriptor: number | undefined;
  try {
    const pathBefore = lstatSync(path, { bigint: true });
    descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (
      !descriptorBefore.isFile() ||
      descriptorBefore.dev !== pathBefore.dev ||
      descriptorBefore.ino !== pathBefore.ino
    ) {
      throw new Error("raw input changed before descriptor binding");
    }
    const bytes = readFileSync(descriptor);
    const descriptorAfter = fstatSync(descriptor, { bigint: true });
    const identity: ExternalRawIdentity = {
      path,
      dev: descriptorAfter.dev,
      ino: descriptorAfter.ino,
      size: descriptorAfter.size,
      mtimeNs: descriptorAfter.mtimeNs,
      ctimeNs: descriptorAfter.ctimeNs,
    };
    if (
      descriptorAfter.dev !== descriptorBefore.dev ||
      descriptorAfter.ino !== descriptorBefore.ino ||
      descriptorAfter.size !== descriptorBefore.size ||
      descriptorAfter.mtimeNs !== descriptorBefore.mtimeNs ||
      descriptorAfter.ctimeNs !== descriptorBefore.ctimeNs
    ) {
      throw new Error("raw input changed during its one stable read");
    }
    assertExternalRawIdentityCurrent(rootReal, identity);
    return { bytes, identity };
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw rawInputError(path, `Raw input cannot be read stably: ${String(error)}.`);
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

function assertExternalRawIdentityCurrent(rootReal: string, identity: ExternalRawIdentity): void {
  assertCanonicalExternalPath(rootReal, identity.path, "file");
  try {
    const current = lstatSync(identity.path, { bigint: true });
    if (
      !current.isFile() ||
      current.isSymbolicLink() ||
      current.dev !== identity.dev ||
      current.ino !== identity.ino ||
      current.size !== identity.size ||
      current.mtimeNs !== identity.mtimeNs ||
      current.ctimeNs !== identity.ctimeNs
    ) {
      throw new Error("raw input identity changed after snapshot");
    }
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw rawInputError(
      identity.path,
      `Raw input snapshot is no longer current: ${String(error)}.`,
    );
  }
}

function publishGeneratorSupplyV2FileAppendOnly(
  rootReal: string,
  path: string,
  bytes: Buffer,
  index: number,
  hooks: GeneratorSupplyV2AssemblyWriteHooks,
  parents: readonly ParentDirectoryIdentity[],
): GeneratorSupplyV2StableFileIdentity {
  const output = resolve(rootReal, path);
  const existing = readExistingDestination(rootReal, path);
  if (existing) {
    if (!existing.bytes.equals(bytes))
      throw writeConflict(path, "Existing late-bound file diverges.");
    assertParentDirectoriesCurrent(parents);
    hooks.afterPublish?.(path, index);
    return existing.identity;
  }
  const token = `${process.pid}-${Date.now()}-${process.hrtime.bigint()}-${index}`;
  const temporary = resolve(dirname(output), `.${basename(output)}.write-${token}`);
  let descriptor: number | undefined;
  let temporaryIdentity: OwnedTemporaryIdentity | undefined;
  let temporaryOwned = false;
  try {
    descriptor = openSync(
      temporary,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW,
      0o600,
    );
    writeFileSync(descriptor, bytes);
    fsyncSync(descriptor);
    const temporaryStat = fstatSync(descriptor, { bigint: true });
    temporaryIdentity = {
      path: temporary,
      dev: temporaryStat.dev,
      ino: temporaryStat.ino,
      size: temporaryStat.size,
      mtimeNs: temporaryStat.mtimeNs,
      ctimeNs: temporaryStat.ctimeNs,
    };
    temporaryOwned = true;
    closeSync(descriptor);
    descriptor = undefined;
    assertParentDirectoriesCurrent(parents);
    hooks.beforePublish?.(path, index, temporary, output);
    assertParentDirectoriesCurrent(parents);
    assertOwnedTemporaryCurrent(temporaryIdentity);
    let linkedByWriter = false;
    try {
      linkSync(temporary, output);
      linkedByWriter = true;
      fsyncDirectory(dirname(output));
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "EEXIST")) throw error;
      const winner = readExistingDestination(rootReal, path);
      if (!winner || !winner.bytes.equals(bytes)) {
        throw writeConflict(path, "A divergent destination won the no-replace publish race.");
      }
    }
    if (linkedByWriter) temporaryIdentity = refreshOwnedTemporaryAfterLink(temporaryIdentity);
    cleanupGeneratorSupplyV2Temporary(temporaryIdentity, parents);
    temporaryOwned = false;
    fsyncDirectory(dirname(output));
    assertParentDirectoriesCurrent(parents);
    const published = readExistingDestination(rootReal, path);
    if (!published || !published.bytes.equals(bytes)) {
      throw writeConflict(path, "Published late-bound bytes are not exact.");
    }
    hooks.afterPublish?.(path, index);
    return published.identity;
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw writeConflict(path, `Append-only publish failed: ${String(error)}.`);
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
    if (temporaryOwned && temporaryIdentity) {
      cleanupGeneratorSupplyV2Temporary(temporaryIdentity, parents);
      temporaryOwned = false;
      fsyncDirectory(dirname(output));
    }
  }
}

function refreshOwnedTemporaryAfterLink(identity: OwnedTemporaryIdentity): OwnedTemporaryIdentity {
  try {
    const current = lstatSync(identity.path, { bigint: true });
    if (
      !current.isFile() ||
      current.isSymbolicLink() ||
      current.dev !== identity.dev ||
      current.ino !== identity.ino ||
      current.size !== identity.size ||
      current.mtimeNs !== identity.mtimeNs
    ) {
      throw new Error("owned temporary changed across no-replace link");
    }
    return {
      path: identity.path,
      dev: current.dev,
      ino: current.ino,
      size: current.size,
      mtimeNs: current.mtimeNs,
      ctimeNs: current.ctimeNs,
    };
  } catch (error) {
    throw writeConflict(
      identity.path,
      `Owned temporary could not be rebound after publish: ${String(error)}.`,
    );
  }
}

function cleanupGeneratorSupplyV2Temporary(
  identity: OwnedTemporaryIdentity,
  parents: readonly ParentDirectoryIdentity[],
): void {
  assertParentDirectoriesCurrent(parents);
  assertOwnedTemporaryCurrent(identity);
  unlinkSync(identity.path);
}

function assertOwnedTemporaryCurrent(identity: OwnedTemporaryIdentity): void {
  try {
    const current = lstatSync(identity.path, { bigint: true });
    if (
      !current.isFile() ||
      current.isSymbolicLink() ||
      current.dev !== identity.dev ||
      current.ino !== identity.ino ||
      current.size !== identity.size ||
      current.mtimeNs !== identity.mtimeNs ||
      current.ctimeNs !== identity.ctimeNs
    ) {
      throw writeConflict(
        identity.path,
        "Owned temporary identity changed; cleanup refused without unlinking the lexical path.",
      );
    }
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw writeConflict(
      identity.path,
      `Owned temporary disappeared before identity-safe cleanup: ${String(error)}.`,
    );
  }
}

function ensureGeneratorSupplyV2ParentDirectories(
  rootReal: string,
  targetParent: string,
): ParentDirectoryIdentity[] {
  const relation = relative(rootReal, targetParent);
  if (
    relation === "" ||
    relation.startsWith(`..${sep}`) ||
    relation === ".." ||
    isAbsolute(relation)
  ) {
    throw writeConflict(targetParent, "Destination parent escaped the repository root.");
  }
  const identities: ParentDirectoryIdentity[] = [];
  let current = rootReal;
  for (const component of relation.split(sep)) {
    current = resolve(current, component);
    try {
      mkdirSync(current, { mode: 0o700 });
      fsyncDirectory(dirname(current));
    } catch (error) {
      if (!(error instanceof Error && "code" in error && error.code === "EEXIST")) {
        throw writeConflict(current, `Destination parent cannot be created: ${String(error)}.`);
      }
    }
    const stat = lstatSync(current, { bigint: true });
    if (!stat.isDirectory() || stat.isSymbolicLink() || realpathSync(current) !== current) {
      throw writeConflict(current, "Destination parent must be a regular no-symlink directory.");
    }
    identities.push({ path: current, dev: stat.dev, ino: stat.ino });
  }
  assertParentDirectoriesCurrent(identities);
  return identities;
}

function assertParentDirectoriesCurrent(identities: readonly ParentDirectoryIdentity[]): void {
  for (const identity of identities) {
    try {
      const stat = lstatSync(identity.path, { bigint: true });
      if (
        !stat.isDirectory() ||
        stat.isSymbolicLink() ||
        stat.dev !== identity.dev ||
        stat.ino !== identity.ino ||
        realpathSync(identity.path) !== identity.path
      ) {
        throw new Error("parent identity changed");
      }
    } catch (error) {
      throw writeConflict(identity.path, `Destination parent fence failed: ${String(error)}.`);
    }
  }
}

function readExistingDestination(
  rootReal: string,
  path: string,
): Readonly<{ bytes: Buffer; identity: GeneratorSupplyV2StableFileIdentity }> | undefined {
  const absolute = resolve(rootReal, path);
  try {
    const stat = lstatSync(absolute);
    if (!stat.isFile() || stat.isSymbolicLink() || realpathSync(absolute) !== absolute) {
      throw writeConflict(path, "Existing destination must be a regular non-symlink file.");
    }
    const identities: GeneratorSupplyV2StableFileIdentity[] = [];
    const bytes = readContainedRegularFile(rootReal, path, undefined, identities);
    const identity = identities[0];
    if (!identity) throw new Error("destination identity was not captured");
    return { bytes, identity };
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return undefined;
    throw writeConflict(path, `Existing destination cannot be inspected: ${String(error)}.`);
  }
}

function assertPublishedOutputsCurrent(
  root: string,
  identities: readonly GeneratorSupplyV2StableFileIdentity[],
): void {
  for (const identity of identities) {
    try {
      const current = lstatSync(identity.absolute, { bigint: true });
      if (
        realpathSync(root) !== identity.rootReal ||
        current.dev !== identity.dev ||
        current.ino !== identity.ino ||
        current.size !== identity.size ||
        current.mtimeNs !== identity.mtimeNs ||
        current.ctimeNs !== identity.ctimeNs ||
        !current.isFile() ||
        current.isSymbolicLink() ||
        realpathSync(identity.absolute) !== identity.absolute
      ) {
        throw new Error("published output identity changed");
      }
    } catch (error) {
      throw writeConflict(identity.path, `Published output snapshot changed: ${String(error)}.`);
    }
  }
}

function assertGeneratorSupplyV2AuthoritySnapshotCurrent(
  root: string,
  identities: readonly GeneratorSupplyV2StableFileIdentity[],
): void {
  if (identities.length !== 3) {
    throw writeConflict(
      "/authority",
      "Assembly authority snapshot must contain source and schemas.",
    );
  }
  for (const identity of identities) {
    try {
      const current = lstatSync(identity.absolute, { bigint: true });
      if (
        realpathSync(root) !== identity.rootReal ||
        current.dev !== identity.dev ||
        current.ino !== identity.ino ||
        current.size !== identity.size ||
        current.mtimeNs !== identity.mtimeNs ||
        current.ctimeNs !== identity.ctimeNs ||
        !current.isFile() ||
        current.isSymbolicLink() ||
        realpathSync(identity.absolute) !== identity.absolute
      ) {
        throw new Error("authority identity changed");
      }
    } catch (error) {
      throw writeConflict(identity.path, `Assembly authority snapshot changed: ${String(error)}.`);
    }
  }
}

function fsyncDirectory(path: string): void {
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    fsyncSync(descriptor);
  } finally {
    closeSync(descriptor);
  }
}

function rawInputError(path: string, message: string): GeneratorSupplyV2Error {
  return v2Error("GENERATOR_SUPPLY_V2_RAW_INPUT_INVALID", path, message);
}

function writeConflict(path: string, message: string): GeneratorSupplyV2Error {
  return v2Error("GENERATOR_SUPPLY_V2_WRITE_CONFLICT", `/${path}`, message);
}

export function buildGeneratorSupplyV2Source(): GeneratorSupplyV2Source {
  return {
    formatVersion: "cloud-agents-generator-supply-profile-source/v2",
    registryId: REGISTRY_ID,
    predecessor: {
      profileId: "cloud-agents/generator-supply-profile/v1",
      predecessorMutation: "forbidden",
      outerFiles: GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map((record) => ({ ...record })),
      evidenceManifestPolicy: { ...GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST },
      profileIdentities: { ...GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES },
      fixedLineage: {
        ...GENERATOR_SUPPLY_V1_GIT_LINEAGE,
      },
      projection: {
        treeSha: "4a70fb8b1e18801f4f02a753668ffe91b63b6275",
        archiveSha256: "36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478",
        archiveSizeBytes: 46_008_320,
        receiptPath: "tools/generator-supply/v1/evidence/replay/projection.json",
        receiptSha256: "1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4",
        receiptSizeBytes: 1_708,
      },
    },
    inheritance: {
      material: "EXACT_V1_39_MEMBER_MANIFEST",
      security: "EXACT_V1_TIME_BOUND_SECURITY_EVIDENCE",
      mutation: "forbidden",
      freshEvidenceAuthority: "V2_NATIVE_REPLAY_RECEIPTS_ONLY",
      legalApproval: "NOT_CLAIMED",
      signature: "NOT_PRODUCED_NOT_AUTHORIZED",
    },
    replayContract: structuredClone(GENERATOR_SUPPLY_V2_REPLAY_CONTRACT),
    declaredProfile: {
      profileId: PROFILE_ID,
      status: "REPLAY_VERIFIED_REVIEW_PENDING",
      notGateClosure: true,
      platforms: [
        { id: "darwin-arm64", status: "NATIVE_REPLAY_VERIFIED", nativeExecution: true },
        { id: "linux-amd64", status: "NATIVE_REPLAY_VERIFIED", nativeExecution: true },
        { id: "linux-arm64", status: "NOT_CLAIMED", nativeExecution: false },
      ],
      boundaries: {
        criterion: "REVIEW_PENDING_NOT_CLOSED",
        gate: "ALL_GATES_OPEN",
        productionDatabase: "NOT_AUTHORIZED",
        http: "NOT_IMPLEMENTED",
        p2: "NOT_IMPLEMENTED",
        provider: "NOT_AUTHORIZED",
        deployment: "NOT_AUTHORIZED",
        release: "NOT_AUTHORIZED",
        legalApproval: "NOT_CLAIMED",
        signature: "NOT_PRODUCED_NOT_AUTHORIZED",
        bootstrapDiscovery: "FORBIDDEN",
      },
    },
    replayEvidence: {
      state: "DECLARED_PRE_REPLAY",
      authority: "EXTERNAL_LATE_BOUND",
      receiptPaths: [...SUCCESSOR_REPLAY_RECEIPT_PATHS],
    },
  };
}

export function buildGeneratorSupplyV2TestSource(): GeneratorSupplyV2Source {
  return buildGeneratorSupplyV2Source();
}

export function serializeGeneratorSupplyV2Source(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function assertStableGeneratorSupplyV2ReadMutationForTest(
  root: string,
  path: string,
  mutateAfterRead: () => void,
): void {
  readContainedRegularFile(root, path, mutateAfterRead);
}

function assertExactReceiptRecords(
  records: readonly FileRecord[],
  semanticRecords: readonly {
    readonly path: string;
    readonly sha256: string;
    readonly sizeBytes: number;
  }[],
): void {
  if (
    records.length !== SUCCESSOR_REPLAY_RECEIPT_PATHS.length ||
    semanticRecords.length !== SUCCESSOR_REPLAY_RECEIPT_PATHS.length ||
    records.some((record, index) => record.path !== SUCCESSOR_REPLAY_RECEIPT_PATHS[index]) ||
    semanticRecords.some((record, index) => record.path !== SUCCESSOR_REPLAY_RECEIPT_PATHS[index])
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/receipts",
      "Generator-supply v2 receipts must match the exact DAG path order.",
    );
  }
  for (const [index, record] of records.entries()) {
    if (!canonicalEqual(record, semanticRecords[index])) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
        `/profile/evidence/receipts/${index}`,
        `Generator-supply v2 receipt ${record.path} does not match the one stable-read semantic snapshot.`,
      );
    }
  }
}

function readSource(
  root: string,
  identities?: GeneratorSupplyV2StableFileIdentity[],
): GeneratorSupplyV2Source {
  return readJsonFile(root, GENERATOR_SUPPLY_V2_SOURCE_PATH, identities) as GeneratorSupplyV2Source;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  validateAgainstCompiledSchema(schemaValidator(root), schemaId, value);
}

function validateAgainstCompiledSchema(validator: Ajv2020, schemaId: string, value: unknown): void {
  const validate = validator.getSchema(schemaId);
  if (!validate || !validate(value)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      "/",
      `Generator-supply v2 schema validation failed: ${validator.errorsText(validate?.errors)}.`,
    );
  }
}

function schemaValidator(root: string): Ajv2020 {
  return schemaValidatorFromBytes(
    readContainedRegularFile(root, GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH),
    readContainedRegularFile(root, GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH),
  );
}

function schemaValidatorFromBytes(sourceSchemaBytes: Buffer, outputSchemaBytes: Buffer): Ajv2020 {
  try {
    const sourceSchema = parseSchemaBytes(
      sourceSchemaBytes,
      GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
    );
    const outputSchema = parseSchemaBytes(
      outputSchemaBytes,
      GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
    );
    const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
    ajv.addSchema(sourceSchema);
    ajv.addSchema(outputSchema);
    ajv.getSchema(SOURCE_SCHEMA_ID);
    ajv.getSchema(OUTPUT_SCHEMA_ID);
    return ajv;
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      "/schemas",
      `Generator-supply v2 captured schemas are invalid or cannot be compiled: ${String(error)}.`,
    );
  }
}

function parseSchemaBytes(bytes: Buffer, path: string): JsonRecord {
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch (error) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      `/${path}`,
      `Generator-supply v2 captured schema is not valid JSON: ${String(error)}.`,
    );
  }
  if (!isRecord(parsed)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      `/${path}`,
      "Generator-supply v2 captured schema must be an object.",
    );
  }
  return parsed;
}

function groupPresence(
  root: string,
  paths: readonly string[],
  errorPath: string,
): "NONE" | "ALL" | "PARTIAL" {
  const count = paths.filter((path) => filePresence(root, path)).length;
  if (count === 0) return "NONE";
  if (count === paths.length) return "ALL";
  if (count > 0) return "PARTIAL";
  throw v2Error("GENERATOR_SUPPLY_V2_PARTIAL_STATE", errorPath, "Unreachable path state.");
}

function filePresence(root: string, path: string): boolean {
  const absolute = resolveContainedPath(root, path, true);
  try {
    const stat = lstatSync(absolute);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
        `/${path}`,
        "Generator-supply v2 authority requires regular non-symlink files.",
      );
    }
    return true;
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      `/${path}`,
      "Generator-supply v2 late-bound path cannot be inspected.",
    );
  }
}

function readJsonFile(
  root: string,
  path: string,
  identities?: GeneratorSupplyV2StableFileIdentity[],
): JsonRecord {
  return readJsonFileSnapshot(root, path, identities).value;
}

function readJsonFileSnapshot(
  root: string,
  path: string,
  identities?: GeneratorSupplyV2StableFileIdentity[],
): Readonly<{ value: JsonRecord; fileSha256: string }> {
  try {
    const bytes = readContainedRegularFile(root, path, undefined, identities);
    const parsed: unknown = JSON.parse(bytes.toString("utf8"));
    if (!isRecord(parsed)) throw new Error("expected an object");
    return {
      value: parsed,
      fileSha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    };
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      `/${path}`,
      `Generator-supply v2 JSON file is missing or invalid: ${String(error)}.`,
    );
  }
}

function readContainedRegularFile(
  root: string,
  path: string,
  mutateAfterRead?: () => void,
  identities?: GeneratorSupplyV2StableFileIdentity[],
): Buffer {
  const rootReal = realpathSync(root);
  const absolute = resolveContainedPath(rootReal, path);
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
        `/${path}`,
        "Generator-supply v2 path must be a contained regular non-symlink file.",
      );
    }
    const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      const descriptorBefore = fstatSync(descriptor, { bigint: true });
      if (
        !descriptorBefore.isFile() ||
        descriptorBefore.dev !== pathBefore.dev ||
        descriptorBefore.ino !== pathBefore.ino
      ) {
        throw v2Error(
          "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
          `/${path}`,
          "Generator-supply v2 input changed before it could be opened.",
        );
      }
      const bytes = readFileSync(descriptor);
      mutateAfterRead?.();
      const descriptorAfter = fstatSync(descriptor, { bigint: true });
      const pathAfter = lstatSync(absolute, { bigint: true });
      if (
        descriptorAfter.dev !== descriptorBefore.dev ||
        descriptorAfter.ino !== descriptorBefore.ino ||
        descriptorAfter.size !== descriptorBefore.size ||
        descriptorAfter.mtimeNs !== descriptorBefore.mtimeNs ||
        descriptorAfter.ctimeNs !== descriptorBefore.ctimeNs ||
        pathAfter.dev !== descriptorBefore.dev ||
        pathAfter.ino !== descriptorBefore.ino ||
        !pathAfter.isFile() ||
        pathAfter.isSymbolicLink() ||
        realpathSync(absolute) !== absolute
      ) {
        throw v2Error(
          "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
          `/${path}`,
          "Generator-supply v2 input changed while it was being read.",
        );
      }
      identities?.push({
        rootReal,
        path,
        absolute,
        dev: descriptorAfter.dev,
        ino: descriptorAfter.ino,
        size: descriptorAfter.size,
        mtimeNs: descriptorAfter.mtimeNs,
        ctimeNs: descriptorAfter.ctimeNs,
      });
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof GeneratorSupplyV2Error) throw error;
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      `/${path}`,
      `Generator-supply v2 input is missing or unreadable: ${String(error)}.`,
    );
  }
}

function assertGeneratorSupplyV2OuterSnapshotCurrent(
  root: string,
  identities: readonly GeneratorSupplyV2StableFileIdentity[],
): void {
  if (identities.length !== 3) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/snapshot",
      "Generator-supply v2 current validation requires source, manifest, and output snapshots.",
    );
  }
  const rootReal = identities[0]!.rootReal;
  if (realpathSync(root) !== rootReal) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/snapshot",
      "Generator-supply v2 repository root changed after outer snapshot capture.",
    );
  }
  const unique = new Map<string, GeneratorSupplyV2StableFileIdentity>();
  for (const identity of identities) {
    const previous = unique.get(identity.path);
    if (previous) {
      if (!sameGeneratorSupplyV2StableFileIdentity(previous, identity)) {
        throw v2Error(
          "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
          `/${identity.path}`,
          "Generator-supply v2 outer path changed between snapshot phases.",
        );
      }
      continue;
    }
    unique.set(identity.path, identity);
  }
  if (unique.size !== 3) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/snapshot",
      "Generator-supply v2 outer snapshot paths must be distinct.",
    );
  }
  for (const identity of unique.values()) {
    try {
      if (
        identity.rootReal !== rootReal ||
        resolve(rootReal, identity.path) !== identity.absolute
      ) {
        throw new Error("root or absolute path binding drifted");
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
          throw new Error("path topology drifted");
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
        throw new Error("file identity drifted");
      }
    } catch (error) {
      if (error instanceof GeneratorSupplyV2Error) throw error;
      throw v2Error(
        "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
        `/${identity.path}`,
        `Generator-supply v2 outer snapshot is no longer current: ${String(error)}.`,
      );
    }
  }
}

function sameGeneratorSupplyV2StableFileIdentity(
  left: GeneratorSupplyV2StableFileIdentity,
  right: GeneratorSupplyV2StableFileIdentity,
): boolean {
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

function resolveContainedPath(root: string, path: string, allowMissing = false): string {
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment === "" || segment === "." || segment === "..")
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
      `/${path}`,
      "Generator-supply v2 path must be canonical and repository-relative.",
    );
  }
  let current = rootReal;
  const components = path.split("/");
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    const final = index === components.length - 1;
    try {
      const stat = lstatSync(current);
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory())) {
        throw new Error("symbolic links and non-directory parents are forbidden");
      }
      if (final && !stat.isFile()) throw new Error("final path is not a regular file");
    } catch (error) {
      if (allowMissing && error instanceof Error && "code" in error && error.code === "ENOENT") {
        return absolute;
      }
      throw v2Error(
        "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
        `/${path}`,
        `Generator-supply v2 path contains an invalid component: ${String(error)}.`,
      );
    }
  }
  return absolute;
}

function assertNoV2AuthorityIsExcluded(): void {
  const exclusions = new Set<string>(SUCCESSOR_PROJECTION_EXCLUSIONS);
  const preReplayAuthority = new Set<string>(SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS);
  for (const path of [
    GENERATOR_SUPPLY_V2_SOURCE_PATH,
    GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  ]) {
    if (!preReplayAuthority.has(path) || exclusions.has(path)) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
        `/${path}`,
        "Generator-supply v2 source and schemas must be pre-replay authority, never exclusions.",
      );
    }
  }
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

function v2Error(
  code: GeneratorSupplyV2Error["code"],
  path: string,
  message: string,
): GeneratorSupplyV2Error {
  return new GeneratorSupplyV2Error(code, path, message);
}
