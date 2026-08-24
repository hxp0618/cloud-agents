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

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  assertGeneratorSupplyReplayV2ContractCurrent,
  assertGeneratorSupplyReplayV2Receipts,
  buildGeneratorSupplyReplayV2ExpectedFromImmutableV1,
  type GeneratorSupplyReplayV2Contract,
  type GeneratorSupplyReplayV2Validation,
} from "./platform-generator-supply-replay-v2";
import {
  SUCCESSOR_ASSEMBLY_PATHS,
  SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag";
import {
  assertGeneratorSupplyV1PredecessorImmutable,
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
      sha256: "f85ab149a6a2daf36b3eb1a06a00f0258829ff71a3aae2dc6cec9f3d0601b250",
      sizeBytes: 83_140,
    },
    runner: {
      path: "scripts/replay-platform-generators.ts",
      sha256: "2e07df97c7ca646b365a9090ee0d98af2ede386d56c6647c450c778f4147f58f",
      sizeBytes: 44_958,
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

export class GeneratorSupplyV2Error extends Error {
  constructor(
    readonly code:
      | "GENERATOR_SUPPLY_V2_SCHEMA_INVALID"
      | "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH"
      | "GENERATOR_SUPPLY_V2_PREDECESSOR_MISMATCH"
      | "GENERATOR_SUPPLY_V2_PARTIAL_STATE"
      | "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH"
      | "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
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

export function validateGeneratorSupplyV2Source(
  root: string,
  source: GeneratorSupplyV2Source,
): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
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
  const registry = readJsonFile(
    root,
    GENERATOR_SUPPLY_V2_OUTPUT_PATH,
    identities,
  ) as GeneratorSupplyV2Registry;
  if (suppliedDocument !== undefined && !canonicalEqual(suppliedDocument, registry)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      "/registry",
      "Supplied generator-supply v2 registry does not match the current output snapshot.",
    );
  }
  mutateAfterOuterSnapshot?.();
  const assertReplaySnapshotCurrent = assertGeneratorSupplyV2RegistrySemanticsInternal(
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
    assertReplaySnapshotCurrent();
  };
  assertCurrent();
  return { registry, assertCurrent };
}

function assertGeneratorSupplyV2RegistrySemanticsInternal(
  root: string,
  document: unknown,
  source: GeneratorSupplyV2Source,
  standaloneManifest: EvidenceManifest,
): () => void {
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
  return semanticValidation.assertSnapshotCurrent;
}

export function buildGeneratorSupplyV2TestSource(): GeneratorSupplyV2Source {
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
  const validate = schemaValidator(root).getSchema(schemaId);
  if (!validate || !validate(value)) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      "/",
      `Generator-supply v2 schema validation failed: ${schemaValidator(root).errorsText(validate?.errors)}.`,
    );
  }
}

function schemaValidator(root: string): Ajv2020 {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  for (const path of [
    GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(readJsonFile(root, path));
  }
  ajv.getSchema(SOURCE_SCHEMA_ID);
  ajv.getSchema(OUTPUT_SCHEMA_ID);
  return ajv;
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
  try {
    const parsed: unknown = JSON.parse(
      readContainedRegularFile(root, path, undefined, identities).toString("utf8"),
    );
    if (!isRecord(parsed)) throw new Error("expected an object");
    return parsed;
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
