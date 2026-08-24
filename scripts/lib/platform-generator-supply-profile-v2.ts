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
  const document = readJsonFile(root, GENERATOR_SUPPLY_V2_OUTPUT_PATH) as GeneratorSupplyV2Registry;
  assertGeneratorSupplyV2RegistrySemantics(root, document);
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
  assertNoV2AuthorityIsExcluded();
}

export function assertGeneratorSupplyV2RegistrySemantics(
  root: string,
  document: unknown,
): asserts document is GeneratorSupplyV2Registry {
  const source = readSource(root);
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
  assertExactReceiptRecords(root, receipts);
  const embeddedManifest = registry.profile.evidence.evidenceManifest;
  const standaloneManifest = readJsonFile(
    root,
    GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
  ) as EvidenceManifest;
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
        reviewParent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateCommit,
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

function assertExactReceiptRecords(root: string, records: readonly FileRecord[]): void {
  if (
    records.length !== SUCCESSOR_REPLAY_RECEIPT_PATHS.length ||
    records.some((record, index) => record.path !== SUCCESSOR_REPLAY_RECEIPT_PATHS[index])
  ) {
    throw v2Error(
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
      "/profile/evidence/receipts",
      "Generator-supply v2 receipts must match the exact DAG path order.",
    );
  }
  for (const [index, record] of records.entries()) {
    const bytes = readContainedRegularFile(root, record.path);
    if (
      record.sizeBytes !== bytes.byteLength ||
      record.sha256 !== `sha256:${createHash("sha256").update(bytes).digest("hex")}`
    ) {
      throw v2Error(
        "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
        `/profile/evidence/receipts/${index}`,
        `Generator-supply v2 receipt ${record.path} digest or size drifted.`,
      );
    }
  }
}

function readSource(root: string): GeneratorSupplyV2Source {
  return readJsonFile(root, GENERATOR_SUPPLY_V2_SOURCE_PATH) as GeneratorSupplyV2Source;
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

function readJsonFile(root: string, path: string): JsonRecord {
  try {
    const parsed: unknown = JSON.parse(readContainedRegularFile(root, path).toString("utf8"));
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
): Buffer {
  const absolute = resolveContainedPath(root, path);
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
