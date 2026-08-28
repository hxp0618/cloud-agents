import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  existsSync,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  assertContractClosureV1Immutable,
  assertContractClosureV2Immutable,
  assertGeneratorSupplyV1GitLineageCurrent,
  assertGeneratorSupplyV1PredecessorImmutable,
  assertImmutableFileMap,
  CONTRACT_CLOSURE_V2_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_GIT_LINEAGE,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
  type ImmutableFileRecord,
} from "./platform-successor-predecessor";
import { SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS } from "./platform-successor-dag";

export const CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v4.json";
export const CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v4.json";
export const CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v4.schema.json";
export const CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v4.schema.json";

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-source-v4.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-v4.schema.json";
const REGISTRY_ID = "cloud-agents/platform/contract-closure-profile";
const V2_OUTPUT_PATH = "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json";
const RUNTIME_REVIEW_PATH =
  "docs/plan/p1/g-contract-runtime-current-lineage-rebind-independent-review-20260828.md";
const SUPPLY_V1_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md";
const FUTURE_SUPPLY_V2_PROFILE_PATH = "tools/generator-supply/v2/profile.json";
const FUTURE_SUPPLY_V2_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md";
const REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0";
const V4_AUTHORITY_PATH =
  "docs/plan/p1/g-contract-runtime-closure-profile-v4-authority-20260828.md";

const V4_BASELINE = {
  commit: "6ff645bbea150602226dc0cb727d21579a54f0a7",
  tree: "24a0198cdf551e7834b3e1ebb924aca4249edcda",
} as const;

const V4_C_PROJECTION = {
  candidateCommit: "fa0a687729d62e2e69f7c7923f1e3d3d430f19a8",
  candidateTree: "f29bebbefd8f8a4e2bd09eee5191f83059f3bde6",
  reconstructedTree: "21cc7f741262f1e3b5059a2457772cc49bf31888",
  archiveSha256: "sha256:7e9d44d5e288a98e0572cadfebe8ae4ca898d051b02947e82a4a260f21f2500c",
  archiveSizeBytes: 50_708_480,
  memberManifestSha256: "sha256:7994311a83fc541d8c9c1064b10f0fea94a7a460c29247ef9e14b32f3dafc7e5",
  regularManifestSha256: "sha256:3b2e7eaefb3f51e35f9fbf9c82d25dd727b19578ab130bd4aa9efb5d3b06f6f9",
  archiveEntries: 1_842,
  regularFiles: 1_626,
  symlinks: 0,
  specialEntries: 0,
  unsafeEntries: 0,
} as const;

const V4_PROJECTION_EXCLUSIONS = [
  "contracts/generation.lock.json",
  "tools/generator-supply/v3/evidence-manifest.json",
  "tools/generator-supply/v3/profile.json",
  "tools/generator-supply/v3/evidence/replay.json",
  "tools/generator-supply/v3/evidence/replay/darwin-a.json",
  "tools/generator-supply/v3/evidence/replay/darwin-b.json",
  "tools/generator-supply/v3/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v3/evidence/replay/linux-a.json",
  "tools/generator-supply/v3/evidence/replay/linux-b.json",
  "tools/generator-supply/v3/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v3/evidence/replay/projection.json",
  "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md",
  "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md",
  "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md",
  "tools/gate-phase-record/g-contract-p1/v1/review-tuple.json",
  "tools/gate-phase-record/g-contract-p1/v1/registry.json",
  "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
] as const;

const V4_RECEIPT_PATHS = [
  "tools/generator-supply/v4/evidence/replay/projection.json",
  "tools/generator-supply/v4/evidence/replay/darwin-a.json",
  "tools/generator-supply/v4/evidence/replay/darwin-b.json",
  "tools/generator-supply/v4/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v4/evidence/replay/linux-a.json",
  "tools/generator-supply/v4/evidence/replay/linux-b.json",
  "tools/generator-supply/v4/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v4/evidence/replay.json",
] as const;

const V4_REPLAY_AUTHORITY = {
  baseline: V4_BASELINE,
  projection: {
    ...V4_C_PROJECTION,
    archiveMemberManifestAlgorithm:
      "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
    regularManifestAlgorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
    nodeModulesManifestAlgorithm: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
    exclusions: V4_PROJECTION_EXCLUSIONS,
  },
  sourceSelectors: {
    sourcePath: "tools/generator-supply/v3/source.json",
    sourceSchemaPath: "tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json",
    outputSchemaPath: "tools/generator-supply/v3/generator-supply-profile-v3.schema.json",
    sourceSha256: "sha256:e483a297c20149f34d1a3ad0efc8446a131d3553af114ec319c13a6a3949cfc1",
    sourceSchemaSha256: "sha256:13c11ffd9c6c8628d59f046ac678b6341f5ea5e694d9a8eefff3f9cd48211464",
    outputSchemaSha256: "sha256:0b500db662990bc80e3cbaef2063ae9c1e72030f0111957803d8315959eb7e57",
  },
  coreGeneratorOutputs: SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  receipts: V4_RECEIPT_PATHS,
  runner: {
    runnerPath: "scripts/replay-platform-generators-v4.ts",
    wrapperPath: "scripts/replay-platform-generators-isolated-v4.sh",
    policy: "VERSIONED_ISOLATION_WRAPPER_V4",
    trustedExecutables: ["/usr/bin/git", "/usr/bin/python3"],
    platforms: ["darwin-arm64", "linux-amd64"],
    unclaimedPlatforms: ["linux-arm64"],
    toolchain: {
      node: "24.18.1",
      bun: "1.3.14",
      python: "3.14.7",
      uv: "0.12.5",
      protoc: "35.1",
      protocGenGo: "1.36.12",
      protocGenConnectGo: "1.20.0",
    },
  },
  lineageFence: {
    candidateMode: "single-parent",
    reviewMode: "direct-single-parent-child",
    noSelfReference: true,
    oldReceiptsReusable: false,
  },
  reviewRules: {
    requiredVerdict: "APPROVE",
    p0p1RepairAllowance: "one-repair-within-same-v4-candidate",
    p2Handling: "record-and-defer",
    gateTransition: "FORBIDDEN",
  },
} as const;

type V4PredecessorFileRecord = Readonly<ImmutableFileRecord & { gitBlob: string }>;

export const CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES = [
  {
    path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
    gitBlob: "d714424ac6b42a44ee775a6edde6327d87f2d7c3",
    sha256: "e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4",
    sizeBytes: 14_215,
  },
  {
    path: "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json",
    gitBlob: "58f651367aea31c5662423b602bf293d085a8afa",
    sha256: "face6b9f01732255d4f3ae3aebb040d0af19efae416bad074a2f84510e385862",
    sizeBytes: 13_451,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json",
    gitBlob: "eb2c46f916ac52b13a6d225685bf48064cf35836",
    sha256: "3fbc85313f2195860b6211f8c31fc185d825469146f703b7e442c34b0612ed25",
    sizeBytes: 23_642,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json",
    gitBlob: "ccdef422ab3ef6a61cb2be8ff1e071572cd99374",
    sha256: "3a98b5558cf7d359e4854a46ab95a1a14fb3cc1298a304954d4033a092f8fcb2",
    sizeBytes: 2_080,
  },
  {
    path: "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md",
    gitBlob: "95cd52d4074852f1792620bcac8cf6bf6ffc0853",
    sha256: "83975f780dbcaed587988155f680c33e3b1a42ee10776af2a3077a5482d13001",
    sizeBytes: 10_102,
  },
] as const satisfies readonly V4PredecessorFileRecord[];

const CONTRACT_CLOSURE_V4_V3_BASELINE = {
  commit: "16275f6cbf390c343a9ac00f9193e75eaad0094e",
  tree: "ca595b8e1258a8b78c4da3a545b2a31d8f62b531",
} as const;

export const CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE = {
  candidateCommit: "b79d01028c652d004e67a00fdcbdf204e04dc946",
  candidateTree: "289c7c2ff7ab39b0af1ea0bac84a902d461de8dc",
  candidateParent: "4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97",
  candidateDiffSha256: "e967207e24167e8461fbffbbc98df41103e06eacc508f1bc9baca289433b639c",
  reviewCommit: "62da35c546b3a53659315b6873e6dadbe29fb2d3",
  reviewTree: "d77b068399b42e13fbf0f0337f0fc94f49556dbb",
  reviewParent: "b79d01028c652d004e67a00fdcbdf204e04dc946",
  reviewPath: RUNTIME_REVIEW_PATH,
  reviewSha256: "46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6",
  verdict: REQUIRED_VERDICT,
} as const;

const FIXED_GIT_ENV = {
  PATH: "/usr/bin:/bin",
  LANG: "C",
  LC_ALL: "C",
  TZ: "UTC",
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_CONFIG_GLOBAL: "/dev/null",
  GIT_EXTERNAL_DIFF: "",
  GIT_NO_REPLACE_OBJECTS: "1",
  GIT_OPTIONAL_LOCKS: "0",
  GIT_PAGER: "cat",
} as const;

const FIXED_GIT_CONFIG_ARGS = [
  "-c",
  "core.attributesFile=/dev/null",
  "-c",
  "core.abbrev=7",
  "-c",
  "diff.external=",
  "-c",
  "diff.mnemonicPrefix=false",
  "-c",
  "diff.noprefix=false",
  "-c",
  "diff.renames=false",
] as const;

export const CONTRACT_CLOSURE_V4_CRITERIA = [
  "json-schema-2020-12-official-test-suite",
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const;

export const CONTRACT_CLOSURE_V4_MISSING = ["remaining-generator-supply-chain-review"] as const;

export const CONTRACT_CLOSURE_V4_RUNTIME_FILES = [
  {
    path: "services/control-plane/go.mod",
    sha256: "d27871e7d4d8788d455ac2a5b9d512b0b6628903fad05213a9e227c0f0883d3d",
    sizeBytes: 672,
  },
  {
    path: "services/control-plane/go.sum",
    sha256: "4b870f580591894010f0762c8d04b83cba95a5c09eabc4ffc2631e41290abfbc",
    sizeBytes: 3634,
  },
] as const satisfies readonly ImmutableFileRecord[];

export const CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE = {
  path: RUNTIME_REVIEW_PATH,
  sha256: "46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6",
  sizeBytes: 5030,
} as const satisfies ImmutableFileRecord;

export type ContractClosureV4Review = JsonRecord & {
  readonly path: string;
  readonly sha256: string;
  readonly verdict: string;
};

export type ContractClosureV4Criterion = JsonRecord & {
  readonly id: (typeof CONTRACT_CLOSURE_V4_CRITERIA)[number];
  readonly status: "SATISFIED_CANDIDATE" | "REVIEW_PENDING";
  readonly authorityPaths: readonly string[];
  readonly evidencePaths: readonly string[];
  readonly review?: ContractClosureV4Review;
  readonly reason?: string;
};

export type ContractClosureV4Profile = JsonRecord & {
  readonly profileId: "contract-closure-profile/v4";
  readonly status: "BOOTSTRAP_VALIDATED";
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly criteria: readonly ContractClosureV4Criterion[];
};

export type ContractClosureV4Source = JsonRecord & {
  readonly formatVersion: "cloud-agents-contract-closure-profile-source/v4";
  readonly registryId: typeof REGISTRY_ID;
  readonly authorityRevision: "D-053-EC-2.r4";
  readonly predecessor: JsonRecord;
  readonly supersededV3Predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly replayAuthority: JsonRecord;
  readonly profile: ContractClosureV4Profile;
};

export type ContractClosureV4Registry = JsonRecord & {
  readonly formatVersion: "cloud-agents-contract-closure-profile-registry/v4";
  readonly registryId: typeof REGISTRY_ID;
  readonly authorityRevision: "D-053-EC-2.r4";
  readonly sourceDigest: string;
  readonly predecessor: JsonRecord;
  readonly supersededV3Predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly replayAuthority: JsonRecord;
  readonly profile: JsonRecord & {
    readonly profileDigest: string;
    readonly spec: ContractClosureV4Profile;
  };
  readonly missing: readonly string[];
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly registryDigest: string;
};

export type ContractClosureProfileV4Current = Readonly<{
  source: ContractClosureV4Source;
  registry: ContractClosureV4Registry;
  fileSha256: string;
  assertCurrent: () => void;
}>;

type ContractClosureV4FileSnapshot = Readonly<{
  rootReal: string;
  path: string;
  absolute: string;
  bytes: Buffer;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

export class ContractClosureProfileV4Error extends Error {
  constructor(
    readonly code:
      | "CONTRACT_CLOSURE_V4_SCHEMA_INVALID"
      | "CONTRACT_CLOSURE_V4_IDENTITY_MISMATCH"
      | "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH"
      | "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH"
      | "CONTRACT_CLOSURE_V4_GIT_MISMATCH"
      | "CONTRACT_CLOSURE_V4_SELF_REFERENCE",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ContractClosureProfileV4Error";
  }
}

export function buildContractClosureProfileV4Source(root: string): ContractClosureV4Source {
  assertContractClosureV2Immutable(root);
  return buildContractClosureProfileV4TestSourceWithoutEvidence(readV2Registry(root));
}

/** @deprecated Use buildContractClosureProfileV4Source for canonical production generation. */
export const buildContractClosureProfileV4TestSource = buildContractClosureProfileV4Source;

export function validateContractClosureProfileV4Source(
  root: string,
  source: ContractClosureV4Source,
): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  assertPredecessorEvidence(root);
  assertCanonicalEqual(
    source.predecessor,
    expectedClosureV2Predecessor(),
    "/predecessor",
    "Contract closure v4 must bind the exact four-file closure-v2 predecessor map.",
  );
  assertCanonicalEqual(
    source.supersededV3Predecessor,
    expectedV3Predecessor(),
    "/supersededV3Predecessor",
    "Contract closure v4 must retain the exact immutable v3 closure predecessor fence.",
  );
  assertCanonicalEqual(
    source.runtimeReviewedCandidate,
    expectedRuntimeReviewedCandidate(),
    "/runtimeReviewedCandidate",
    "Runtime reviewed-candidate identity, module bytes, review, or boundary drifted.",
  );
  assertCanonicalEqual(
    source.replayAuthority,
    V4_REPLAY_AUTHORITY,
    "/replayAuthority",
    "Contract closure v4 replay source, projection, exclusion, receipt, and review authority drifted.",
  );
  assertCanonicalEqual(
    source.generatorSupplyV1Predecessor,
    expectedGeneratorSupplyV1Predecessor(),
    "/generatorSupplyV1Predecessor",
    "Generator-supply v1 predecessor, 39-member policy, lineage, review, or identities drifted.",
  );
  assertProfileSemantics(root, source.profile);
}

export function assertContractClosureProfileV4Current(
  root: string,
): ContractClosureProfileV4Current {
  return assertContractClosureProfileV4CurrentInternal(root);
}

export function assertContractClosureProfileV4SourceCurrent(root: string): ContractClosureV4Source {
  const rootReal = realpathSync(root);
  const snapshot = readStableContainedRegularFileSnapshot(
    root,
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
    rootReal,
  );
  const source = parseV4Object<ContractClosureV4Source>(snapshot);
  validateContractClosureProfileV4Source(root, source);
  const expected = buildContractClosureProfileV4Source(root);
  if (
    !Buffer.from(canonicalizeJson(source)).equals(Buffer.from(canonicalizeJson(expected))) ||
    snapshot.bytes.toString("utf8") !== serializeContractClosureProfileV4Source(expected)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH}`,
      "Contract closure v4 source is not the exact canonical pre-replay authority.",
    );
  }
  assertContractClosureV4SnapshotsCurrent(root, [snapshot]);
  return source;
}

export function assertContractClosureProfileV4CurrentMutationForTest(
  root: string,
  mutateAfterCapture: () => void,
): void {
  assertContractClosureProfileV4CurrentInternal(root, mutateAfterCapture);
}

export function assertContractClosureV4V2DependencyABAMutationForTest(
  root: string,
  beforeDerivedRead: () => void,
  afterDerivedRead: () => void,
): void {
  assertContractClosureV2Immutable(root);
  beforeDerivedRead();
  try {
    readV2Registry(root);
  } finally {
    afterDerivedRead();
  }
}

function assertContractClosureProfileV4CurrentInternal(
  root: string,
  mutateAfterCapture?: () => void,
): ContractClosureProfileV4Current {
  const rootReal = realpathSync(root);
  const sourceSnapshot = readStableContainedRegularFileSnapshot(
    root,
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
    rootReal,
  );
  const outputSnapshot = readStableContainedRegularFileSnapshot(
    root,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
    rootReal,
  );
  const source = parseV4Object<ContractClosureV4Source>(sourceSnapshot);
  const registry = parseV4Object<ContractClosureV4Registry>(outputSnapshot);
  const snapshots = [sourceSnapshot, outputSnapshot] as const;
  const assertCurrent = (): void => {
    validateContractClosureProfileV4Source(root, source);
    assertContractClosureV4RegistrySemantics(root, registry);
    const expectedSource = buildContractClosureProfileV4Source(root);
    const expectedRegistry = buildContractClosureProfileV4Registry(root, expectedSource);
    if (
      sourceSnapshot.bytes.toString("utf8") !==
        serializeContractClosureProfileV4Source(expectedSource) ||
      outputSnapshot.bytes.toString("utf8") !==
        serializeContractClosureProfileV4Registry(expectedRegistry)
    ) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        "/source-output",
        "Contract closure v4 source/output must be exact canonical bytes bound in one snapshot.",
      );
    }
    assertContractClosureV4SnapshotsCurrent(root, snapshots);
  };
  mutateAfterCapture?.();
  assertCurrent();
  return {
    source,
    registry,
    fileSha256: `sha256:${sha256(outputSnapshot.bytes)}`,
    assertCurrent,
  };
}

export function assertContractClosureV4RuntimeGitLineageCurrent(root: string): void {
  const repositoryRoot = realpathSync(root);
  const lineage = CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE;
  try {
    const topLevel = realpathSync(gitText(repositoryRoot, ["rev-parse", "--show-toplevel"]));
    const candidateType = gitText(repositoryRoot, ["cat-file", "-t", lineage.candidateCommit]);
    const reviewType = gitText(repositoryRoot, ["cat-file", "-t", lineage.reviewCommit]);
    const candidateTree = gitText(repositoryRoot, [
      "rev-parse",
      `${lineage.candidateCommit}^{tree}`,
    ]);
    const candidateParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      lineage.candidateCommit,
    ]);
    const reviewTree = gitText(repositoryRoot, ["rev-parse", `${lineage.reviewCommit}^{tree}`]);
    const reviewParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      lineage.reviewCommit,
    ]);
    const diff = gitBytes(repositoryRoot, [
      "diff",
      "--no-color",
      "--no-ext-diff",
      "--no-textconv",
      "--binary",
      "--no-renames",
      lineage.candidateParent,
      lineage.candidateCommit,
    ]);
    const reviewBytes = gitBytes(repositoryRoot, [
      "cat-file",
      "blob",
      `${lineage.reviewCommit}:${lineage.reviewPath}`,
    ]);
    const moduleBytesCurrent = CONTRACT_CLOSURE_V4_RUNTIME_FILES.map((file) =>
      readStableContainedRegularFile(repositoryRoot, file.path),
    );
    const moduleBytesAtCandidate = CONTRACT_CLOSURE_V4_RUNTIME_FILES.map((file) =>
      gitBytes(repositoryRoot, ["cat-file", "blob", `${lineage.candidateCommit}:${file.path}`]),
    );
    if (
      topLevel !== repositoryRoot ||
      candidateType !== "commit" ||
      reviewType !== "commit" ||
      candidateTree !== lineage.candidateTree ||
      candidateParents !== lineage.candidateParent ||
      reviewTree !== lineage.reviewTree ||
      reviewParents !== lineage.reviewParent ||
      sha256(diff) !== lineage.candidateDiffSha256 ||
      sha256(reviewBytes) !== lineage.reviewSha256 ||
      moduleBytesAtCandidate.some(
        (bytes, index) =>
          !bytes.equals(moduleBytesCurrent[index]!) ||
          sha256(bytes) !== CONTRACT_CLOSURE_V4_RUNTIME_FILES[index]!.sha256,
      )
    ) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_GIT_MISMATCH",
        `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
        "Contract closure v4 runtime candidate or closed-pair review Git lineage drifted.",
      );
    }
  } catch (error) {
    if (error instanceof ContractClosureProfileV4Error) throw error;
    throw v4Error(
      "CONTRACT_CLOSURE_V4_GIT_MISMATCH",
      `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
      `Contract closure v4 runtime Git lineage is unavailable or invalid: ${String(error)}.`,
    );
  }
}

export function assertContractClosureV4RepositoryLineageCurrent(root: string): void {
  assertContractClosureV4RuntimeGitLineageCurrent(root);
  assertGeneratorSupplyV1GitLineageCurrent(root);
}

export function buildContractClosureProfileV4Registry(
  root: string,
  source: ContractClosureV4Source,
): ContractClosureV4Registry {
  validateContractClosureProfileV4Source(root, source);
  const sourceDigest = domainDigest("cloud-agents/contract-closure-profile/source/v4", source);
  const profileDigest = domainDigest(
    "cloud-agents/contract-closure-profile/profile/v4",
    source.profile,
  );
  const body: JsonRecord = {
    formatVersion: "cloud-agents-contract-closure-profile-registry/v4",
    registryId: REGISTRY_ID,
    authorityRevision: "D-053-EC-2.r4",
    sourceDigest,
    predecessor: source.predecessor,
    supersededV3Predecessor: source.supersededV3Predecessor,
    runtimeReviewedCandidate: source.runtimeReviewedCandidate,
    generatorSupplyV1Predecessor: source.generatorSupplyV1Predecessor,
    replayAuthority: source.replayAuthority,
    profile: { profileDigest, spec: source.profile },
    missing: deriveContractClosureV4Missing(source.profile),
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  const registry = {
    ...body,
    registryDigest: domainDigest("cloud-agents/contract-closure-profile/registry/v4", body),
  } as ContractClosureV4Registry;
  assertContractClosureV4RegistrySemantics(root, registry);
  return registry;
}

export function assertContractClosureV4RegistrySemantics(
  root: string,
  document: unknown,
): asserts document is ContractClosureV4Registry {
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, document);
  if (!isRecord(document)) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      "/",
      "Contract closure v4 registry must be an object.",
    );
  }
  const registry = document as ContractClosureV4Registry;
  if (registry.authorityRevision !== "D-053-EC-2.r4") {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_IDENTITY_MISMATCH",
      "/authorityRevision",
      "Contract closure v4 authority revision drifted.",
    );
  }
  assertPredecessorEvidence(root);
  const expectedSource = buildContractClosureProfileV4TestSourceWithoutEvidence(
    readV2Registry(root),
  );
  if (
    registry.sourceDigest !==
    domainDigest("cloud-agents/contract-closure-profile/source/v4", expectedSource)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH",
      "/sourceDigest",
      "Contract closure v4 source digest does not bind the complete canonical source authority.",
    );
  }
  assertCanonicalEqual(
    registry.predecessor,
    expectedSource.predecessor,
    "/predecessor",
    "Generated closure v4 predecessor binding drifted.",
  );
  assertCanonicalEqual(
    registry.supersededV3Predecessor,
    expectedSource.supersededV3Predecessor,
    "/supersededV3Predecessor",
    "Generated closure v4 v3 predecessor binding drifted.",
  );
  assertCanonicalEqual(
    registry.runtimeReviewedCandidate,
    expectedSource.runtimeReviewedCandidate,
    "/runtimeReviewedCandidate",
    "Generated closure v4 runtime binding drifted.",
  );
  assertCanonicalEqual(
    registry.generatorSupplyV1Predecessor,
    expectedSource.generatorSupplyV1Predecessor,
    "/generatorSupplyV1Predecessor",
    "Generated closure v4 generator-supply predecessor binding drifted.",
  );
  assertCanonicalEqual(
    registry.replayAuthority,
    expectedSource.replayAuthority,
    "/replayAuthority",
    "Generated closure v4 replay authority binding drifted.",
  );
  assertProfileSemantics(root, registry.profile.spec);
  const expectedMissing = deriveContractClosureV4Missing(registry.profile.spec);
  assertCanonicalEqual(
    registry.missing,
    expectedMissing,
    "/missing",
    "Contract closure v4 missing must be derived from criterion status.",
  );
  if (
    registry.profile.profileDigest !==
    domainDigest("cloud-agents/contract-closure-profile/profile/v4", registry.profile.spec)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH",
      "/profile/profileDigest",
      "Contract closure v4 profile digest does not bind the canonical profile.",
    );
  }
  const { registryDigest: _registryDigest, ...body } = registry;
  if (
    registry.registryDigest !==
    domainDigest("cloud-agents/contract-closure-profile/registry/v4", body)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH",
      "/registryDigest",
      "Contract closure v4 registry digest does not bind the canonical registry body.",
    );
  }
}

export function deriveContractClosureV4Missing(profile: ContractClosureV4Profile): string[] {
  return profile.criteria
    .filter((criterion) => criterion.status !== "SATISFIED_CANDIDATE")
    .map((criterion) => criterion.id);
}

export function serializeContractClosureProfileV4Registry(value: unknown): string {
  return `${formatContractClosureProfileV4Json(value, 0, 0)}\n`;
}

export function serializeContractClosureProfileV4Source(value: unknown): string {
  return `${formatContractClosureProfileV4Json(value, 0, 0)}\n`;
}

export function writeContractClosureProfileV4Source(root: string): void {
  const source = buildContractClosureProfileV4Source(root);
  writeContainedRegularFile(
    root,
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
    serializeContractClosureProfileV4Source(source),
  );
  assertContractClosureProfileV4SourceCurrent(root);
}

export function writeContractClosureProfileV4(root: string): void {
  const source = assertContractClosureProfileV4SourceCurrent(root);
  const registry = buildContractClosureProfileV4Registry(root, source);
  writeContainedRegularFile(
    root,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
    serializeContractClosureProfileV4Registry(registry),
  );
  assertContractClosureProfileV4Current(root);
}

function assertProfileSemantics(root: string, profile: ContractClosureV4Profile): void {
  const ids = profile.criteria.map(({ id }) => id);
  assertCanonicalEqual(
    ids,
    CONTRACT_CLOSURE_V4_CRITERIA,
    "/profile/criteria",
    "Contract closure v4 must retain the exact ordered seven-item inventory.",
  );
  assertNoSelfReference(profile);
  const v2 = readV2Registry(root);
  assertCanonicalEqual(
    profile.criteria.slice(0, 5),
    v2.profile.spec.criteria.slice(0, 5),
    "/profile/criteria/0-4",
    "Contract closure v4 criteria 0-4 must carry forward v2 satisfied semantics exactly.",
  );
  const runtime = profile.criteria[5];
  const supply = profile.criteria[6];
  const expectedRuntime = (buildContractClosureProfileV4TestSourceWithoutEvidence(v2).profile
    .criteria[5] ?? {}) as ContractClosureV4Criterion;
  const expectedSupply = (buildContractClosureProfileV4TestSourceWithoutEvidence(v2).profile
    .criteria[6] ?? {}) as ContractClosureV4Criterion;
  assertCanonicalEqual(
    runtime,
    expectedRuntime,
    "/profile/criteria/5",
    "Runtime criterion must be the exact reviewed bounded SATISFIED_CANDIDATE.",
  );
  assertCanonicalEqual(
    supply,
    expectedSupply,
    "/profile/criteria/6",
    "Supply criterion must remain exact REVIEW_PENDING with no criterion review.",
  );
  assertCanonicalEqual(
    profile.implementationBoundary,
    expectedImplementationBoundary(),
    "/profile/implementationBoundary",
    "Contract closure v4 implementation and all-Gates-open boundary drifted.",
  );
  const missing = deriveContractClosureV4Missing(profile);
  assertCanonicalEqual(
    missing,
    CONTRACT_CLOSURE_V4_MISSING,
    "/profile/criteria",
    "Contract closure v4 must derive exactly the one review-pending supply criterion.",
  );
}

function buildContractClosureProfileV4TestSourceWithoutEvidence(
  v2: V2Registry,
): ContractClosureV4Source {
  const inheritedCriteria = v2.profile.spec.criteria.slice(0, 5).map(cloneJson);
  return {
    formatVersion: "cloud-agents-contract-closure-profile-source/v4",
    registryId: REGISTRY_ID,
    authorityRevision: "D-053-EC-2.r4",
    predecessor: expectedClosureV2Predecessor(),
    supersededV3Predecessor: expectedV3Predecessor(),
    runtimeReviewedCandidate: expectedRuntimeReviewedCandidate(),
    generatorSupplyV1Predecessor: expectedGeneratorSupplyV1Predecessor(),
    replayAuthority: V4_REPLAY_AUTHORITY,
    profile: {
      profileId: "contract-closure-profile/v4",
      status: "BOOTSTRAP_VALIDATED",
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      derivation: {
        missing: "criteria_status_not_satisfied_candidate",
        manualRemoval: "forbidden",
        lateReviewConsumer: "detached_non_bootstrap_registry_only",
      },
      criteria: [
        ...inheritedCriteria,
        {
          id: "runtime-server-path-and-tenant-authority-enforcement",
          status: "SATISFIED_CANDIDATE",
          authorityPaths: CONTRACT_CLOSURE_V4_RUNTIME_FILES.map(({ path }) => path),
          evidencePaths: [RUNTIME_REVIEW_PATH, V4_AUTHORITY_PATH],
          review: {
            path: RUNTIME_REVIEW_PATH,
            sha256: `sha256:${CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE.sha256}`,
            verdict: REQUIRED_VERDICT,
          },
        },
        {
          id: "remaining-generator-supply-chain-review",
          status: "REVIEW_PENDING",
          authorityPaths: [
            "tools/generator-supply/v1/source.json",
            "tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json",
            "tools/generator-supply/v1/generator-supply-profile-v1.schema.json",
          ],
          evidencePaths: [
            "tools/generator-supply/v1/evidence-manifest.json",
            "tools/generator-supply/v1/profile.json",
            SUPPLY_V1_REVIEW_PATH,
          ],
          reason:
            "The immutable reviewed generator-supply v1 predecessor is historical; the declared v2 successor review does not yet exist and is consumed only by the detached non-bootstrap registry.",
        },
      ],
      implementationBoundary: expectedImplementationBoundary(),
    },
  } as ContractClosureV4Source;
}

function expectedClosureV2Predecessor(): JsonRecord {
  return {
    profileId: "contract-closure-profile/v2",
    predecessorMutation: "forbidden",
    files: CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
  };
}

function expectedV3Predecessor(): JsonRecord {
  return {
    profileId: "contract-closure-profile/v3",
    predecessorMutation: "forbidden",
    baseline: CONTRACT_CLOSURE_V4_V3_BASELINE,
    files: CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES.map(({ path, gitBlob, sha256, sizeBytes }) => ({
      path,
      gitBlob,
      sha256,
      sizeBytes,
    })),
  };
}

function expectedRuntimeReviewedCandidate(): JsonRecord {
  const lineage = CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE;
  return {
    criterionId: "runtime-server-path-and-tenant-authority-enforcement",
    candidate: {
      commit: lineage.candidateCommit,
      tree: lineage.candidateTree,
      parent: lineage.candidateParent,
      diffSha256: `sha256:${lineage.candidateDiffSha256}`,
    },
    moduleFiles: CONTRACT_CLOSURE_V4_RUNTIME_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
    review: {
      path: RUNTIME_REVIEW_PATH,
      sha256: `sha256:${CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE.sha256}`,
      verdict: REQUIRED_VERDICT,
      commit: lineage.reviewCommit,
      tree: lineage.reviewTree,
      parent: lineage.reviewParent,
    },
    implementationBoundary: {
      transport: "TRANSPORT_NEUTRAL_CLAIM_ONLY",
      http: "NOT_IMPLEMENTED",
      oidc: "NOT_IMPLEMENTED",
      jwks: "NOT_IMPLEMENTED",
      projectWriter: "NOT_IMPLEMENTED",
      provider: "NOT_IMPLEMENTED",
      externalEffects: "NOT_IMPLEMENTED",
    },
  };
}

function expectedGeneratorSupplyV1Predecessor(): JsonRecord {
  return {
    profileId: "cloud-agents/generator-supply-profile/v1",
    predecessorMutation: "forbidden",
    outerFiles: GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
    evidenceManifest: {
      path: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath,
      sha256: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSha256,
      sizeBytes: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSizeBytes,
      algorithm: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.algorithm,
      memberCount: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberCount,
      memberPathPrefix: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberPathPrefix,
      memberVerification: "EXACT_PATH_SHA256_SIZE_REQUIRED",
    },
    candidate: {
      commit: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateCommit,
      tree: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateTree,
      parent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateParent,
      diffSha256: `sha256:${GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateDiffSha256}`,
      reviewCommit: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewCommit,
      reviewTree: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewTree,
      reviewParent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewParent,
      reviewPath: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewPath,
      reviewSha256: `sha256:${GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewSha256}`,
      reviewVerdict: GENERATOR_SUPPLY_V1_GIT_LINEAGE.verdict,
    },
    identities: GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
    projection: {
      treeSha: "4a70fb8b1e18801f4f02a753668ffe91b63b6275",
      archiveSha256: "36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478",
      archiveSizeBytes: 46_008_320,
      receiptPath: "tools/generator-supply/v1/evidence/replay/projection.json",
      receiptSha256: "1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4",
      receiptSizeBytes: 1_708,
    },
    futureSuccessor: {
      profileId: "cloud-agents/generator-supply-profile/v2",
      path: FUTURE_SUPPLY_V2_PROFILE_PATH,
      reviewPath: FUTURE_SUPPLY_V2_REVIEW_PATH,
      reviewStatus: "REVIEW_PENDING",
      canonicalBuildRead: "FORBIDDEN",
    },
  };
}

function expectedImplementationBoundary(): JsonRecord {
  return {
    runtimeCriterion: "SATISFIED_CANDIDATE_BOUNDED_TRANSPORT_NEUTRAL",
    supplyCriterion: "REVIEW_PENDING",
    http: "NOT_IMPLEMENTED",
    oidc: "NOT_IMPLEMENTED",
    jwks: "NOT_IMPLEMENTED",
    projectWriter: "NOT_IMPLEMENTED",
    provider: "NOT_IMPLEMENTED",
    productionDatabaseWrites: "NOT_AUTHORIZED",
    deployment: "NOT_AUTHORIZED",
    publication: "NOT_AUTHORIZED",
    gateStatus: "ALL_GATES_OPEN",
  };
}

function assertPredecessorEvidence(root: string): void {
  assertContractClosureV1Immutable(root);
  assertContractClosureV2Immutable(root);
  assertV3PredecessorEvidence(root);
  assertGeneratorSupplyV1PredecessorImmutable(root);
  assertImmutableFileMap(root, CONTRACT_CLOSURE_V4_RUNTIME_FILES, "runtime reviewed candidate");
  assertImmutableFileMap(root, [CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE], "runtime review");
}

function assertV3PredecessorEvidence(root: string): void {
  for (const record of CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES) {
    const bytes = readStableContainedRegularFile(root, record.path);
    if (
      bytes.byteLength !== record.sizeBytes ||
      sha256(bytes) !== record.sha256 ||
      gitBlobSha1(bytes) !== record.gitBlob
    ) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/supersededV3Predecessor/${record.path}`,
        "Contract closure v4 immutable v3 predecessor bytes drifted.",
      );
    }
  }
  if (!existsSync(resolve(root, ".git"))) return;
  try {
    if (
      gitText(root, ["rev-parse", `${CONTRACT_CLOSURE_V4_V3_BASELINE.commit}^{tree}`]) !==
      CONTRACT_CLOSURE_V4_V3_BASELINE.tree
    ) {
      throw new Error("v3 baseline tree drifted");
    }
    for (const record of CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES) {
      if (
        gitText(root, ["rev-parse", `${CONTRACT_CLOSURE_V4_V3_BASELINE.commit}:${record.path}`]) !==
        record.gitBlob
      ) {
        throw new Error(`v3 baseline blob drifted for ${record.path}`);
      }
    }
  } catch (error) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_GIT_MISMATCH",
      `/supersededV3Predecessor/baseline/${CONTRACT_CLOSURE_V4_V3_BASELINE.commit}`,
      `Contract closure v4 immutable v3 baseline is unavailable or invalid: ${String(error)}.`,
    );
  }
}

function assertNoSelfReference(profile: ContractClosureV4Profile): void {
  const forbidden = new Set([
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH,
    "scripts/lib/platform-contract-closure-profile-v4.ts",
    "scripts/lib/platform-contract-closure-profile-v4.test.ts",
    FUTURE_SUPPLY_V2_PROFILE_PATH,
    FUTURE_SUPPLY_V2_REVIEW_PATH,
    "tools/contract-review-binding/v1/review-tuple.json",
    "tools/contract-review-binding/v1/registry.json",
    "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md",
  ]);
  for (const [index, criterion] of profile.criteria.entries()) {
    for (const path of [
      ...criterion.authorityPaths,
      ...criterion.evidencePaths,
      ...(criterion.review ? [criterion.review.path] : []),
    ]) {
      if (forbidden.has(path)) {
        throw v4Error(
          "CONTRACT_CLOSURE_V4_SELF_REFERENCE",
          `/profile/criteria/${index}`,
          `Contract closure v4 criterion must not read successor, late-review, or self-referential path ${path}.`,
        );
      }
    }
  }
}

type V2Registry = {
  readonly profile: {
    readonly spec: {
      readonly criteria: readonly ContractClosureV4Criterion[];
    };
  };
};

function readV2Registry(root: string): V2Registry {
  try {
    const bytes = readStableContainedRegularFile(root, V2_OUTPUT_PATH);
    const authority = CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.find(
      (record) => record.path === V2_OUTPUT_PATH,
    );
    if (
      authority === undefined ||
      bytes.byteLength !== authority.sizeBytes ||
      sha256(bytes) !== authority.sha256
    ) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/${V2_OUTPUT_PATH}`,
        "Contract closure v2 derived-read bytes do not match the fixed immutable output authority.",
      );
    }
    return JSON.parse(bytes.toString("utf8")) as V2Registry;
  } catch (error) {
    if (error instanceof ContractClosureProfileV4Error) throw error;
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${V2_OUTPUT_PATH}`,
      `Contract closure v2 registry is missing or invalid: ${String(error)}.`,
    );
  }
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(JSON.parse(readStableContainedRegularFile(root, path).toString("utf8")));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      "/",
      `Contract closure v4 schema ${schemaId} is not registered.`,
    );
  }
  if (!validate(value)) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      "/",
      `Contract closure v4 schema validation failed: ${ajv.errorsText(validate.errors)}.`,
    );
  }
}

function assertCanonicalEqual(
  actual: unknown,
  expected: unknown,
  path: string,
  message: string,
): void {
  if (!Buffer.from(canonicalizeJson(actual)).equals(Buffer.from(canonicalizeJson(expected)))) {
    throw v4Error("CONTRACT_CLOSURE_V4_IDENTITY_MISMATCH", path, message);
  }
}

function readStableContainedRegularFile(root: string, path: string): Buffer {
  return readStableContainedRegularFileSnapshot(root, path).bytes;
}

function writeContainedRegularFile(root: string, path: string, contents: string): void {
  const rootReal = realpathSync(root);
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v4 write path must be canonical and repository-relative.",
    );
  }
  const absolute = resolve(rootReal, ...path.split("/"));
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v4 write path escapes its repository root.",
    );
  }

  let current = rootReal;
  const segments = path.split("/");
  for (const [index, segment] of segments.entries()) {
    current = resolve(current, segment);
    const final = index === segments.length - 1;
    try {
      const stat = lstatSync(current);
      if (
        stat.isSymbolicLink() ||
        (!final && !stat.isDirectory()) ||
        (final && !stat.isFile()) ||
        realpathSync(current) !== current
      ) {
        throw new Error("path topology is not a contained regular destination");
      }
    } catch (error) {
      if (final && error instanceof Error && "code" in error && error.code === "ENOENT") break;
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure v4 write destination is unsafe: ${String(error)}.`,
      );
    }
  }

  let descriptor: number | undefined;
  try {
    descriptor = openSync(
      absolute,
      constants.O_WRONLY | constants.O_CREAT | constants.O_TRUNC | constants.O_NOFOLLOW,
      0o644,
    );
    writeFileSync(descriptor, contents, { encoding: "utf8" });
  } catch (error) {
    if (error instanceof ContractClosureProfileV4Error) throw error;
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      `Contract closure v4 write destination could not be opened safely: ${String(error)}.`,
    );
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

function readStableContainedRegularFileSnapshot(
  root: string,
  path: string,
  expectedRootReal?: string,
): ContractClosureV4FileSnapshot {
  const rootReal = realpathSync(root);
  if (expectedRootReal !== undefined && rootReal !== expectedRootReal) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v4 repository root changed during source/output capture.",
    );
  }
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v4 evidence path must be canonical and repository-relative.",
    );
  }
  const absolute = resolve(rootReal, ...path.split("/"));
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation)
  ) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v4 evidence path escapes its repository root.",
    );
  }
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/${path}`,
        "Contract closure v4 evidence must be a regular non-symlink file.",
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
        throw v4Error(
          "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          `/${path}`,
          "Contract closure v4 evidence changed before it could be opened.",
        );
      }
      const bytes = readFileSync(descriptor);
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
        throw v4Error(
          "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          `/${path}`,
          "Contract closure v4 evidence changed while it was being read.",
        );
      }
      return {
        rootReal,
        path,
        absolute,
        bytes,
        dev: descriptorAfter.dev,
        ino: descriptorAfter.ino,
        size: descriptorAfter.size,
        mtimeNs: descriptorAfter.mtimeNs,
        ctimeNs: descriptorAfter.ctimeNs,
      };
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof ContractClosureProfileV4Error) throw error;
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      `/${path}`,
      `Contract closure v4 evidence is missing or unreadable: ${String(error)}.`,
    );
  }
}

function parseV4Object<T extends JsonRecord>(snapshot: ContractClosureV4FileSnapshot): T {
  let parsed: unknown;
  try {
    parsed = JSON.parse(snapshot.bytes.toString("utf8"));
  } catch (error) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      `/${snapshot.path}`,
      `Contract closure v4 file is not valid JSON: ${String(error)}.`,
    );
  }
  if (!isRecord(parsed)) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      `/${snapshot.path}`,
      "Contract closure v4 file must contain a JSON object.",
    );
  }
  return parsed as T;
}

function assertContractClosureV4SnapshotsCurrent(
  root: string,
  snapshots: readonly ContractClosureV4FileSnapshot[],
): void {
  let rootReal: string;
  try {
    rootReal = realpathSync(root);
  } catch (error) {
    throw v4Error(
      "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
      "/",
      `Contract closure v4 repository root is unavailable: ${String(error)}.`,
    );
  }
  for (const snapshot of snapshots) {
    if (rootReal !== snapshot.rootReal) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/${snapshot.path}`,
        "Contract closure v4 repository root changed after capture.",
      );
    }
    try {
      let current = rootReal;
      const segments = snapshot.path.split("/");
      for (const [index, segment] of segments.entries()) {
        current = resolve(current, segment);
        const stat = lstatSync(current, { bigint: true });
        if (
          stat.isSymbolicLink() ||
          (index < segments.length - 1 ? !stat.isDirectory() : !stat.isFile())
        ) {
          throw v4Error(
            "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
            `/${snapshot.path}`,
            "Contract closure v4 source/output topology changed after capture.",
          );
        }
      }
      if (current !== snapshot.absolute || realpathSync(current) !== snapshot.absolute) {
        throw v4Error(
          "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          `/${snapshot.path}`,
          "Contract closure v4 source/output resolved location changed after capture.",
        );
      }
      const after = lstatSync(current, { bigint: true });
      if (
        after.dev !== snapshot.dev ||
        after.ino !== snapshot.ino ||
        after.size !== snapshot.size ||
        after.mtimeNs !== snapshot.mtimeNs ||
        after.ctimeNs !== snapshot.ctimeNs
      ) {
        throw v4Error(
          "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          `/${snapshot.path}`,
          "Contract closure v4 source/output changed after capture.",
        );
      }
    } catch (error) {
      if (error instanceof ContractClosureProfileV4Error) throw error;
      throw v4Error(
        "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        `/${snapshot.path}`,
        `Contract closure v4 source/output is unavailable after capture: ${String(error)}.`,
      );
    }
  }
}

function gitText(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_CONFIG_ARGS, ...args], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function gitBytes(root: string, args: readonly string[]): Buffer {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_CONFIG_ARGS, ...args], {
    cwd: root,
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function gitBlobSha1(value: Uint8Array): string {
  return createHash("sha1").update(`blob ${value.byteLength}\0`).update(value).digest("hex");
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain);
  hash.update(String.fromCharCode(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function cloneJson<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function formatContractClosureProfileV4Json(
  value: unknown,
  indent: number,
  prefixLength: number,
): string {
  if (value === null || typeof value !== "object") {
    const encoded = JSON.stringify(value);
    if (encoded === undefined) {
      throw v4Error(
        "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
        "/",
        "Contract closure v4 serialization accepts JSON values only.",
      );
    }
    return encoded;
  }
  const padding = " ".repeat(indent);
  const childPadding = " ".repeat(indent + 2);
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    if (value.every((entry) => entry === null || typeof entry !== "object")) {
      const inline = `[${value.map((entry) => JSON.stringify(entry)).join(", ")}]`;
      if (indent + prefixLength + inline.length <= 100) return inline;
    }
    return `[\n${value
      .map((entry) => `${childPadding}${formatContractClosureProfileV4Json(entry, indent + 2, 0)}`)
      .join(",\n")}\n${padding}]`;
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return "{}";
  return `{\n${entries
    .map(([key, entry]) => {
      const encodedKey = JSON.stringify(key);
      const prefix = `${childPadding}${encodedKey}: `;
      return `${prefix}${formatContractClosureProfileV4Json(
        entry,
        indent + 2,
        encodedKey.length + 2,
      )}`;
    })
    .join(",\n")}\n${padding}}`;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function v4Error(
  code: ContractClosureProfileV4Error["code"],
  path: string,
  message: string,
): ContractClosureProfileV4Error {
  return new ContractClosureProfileV4Error(code, path, message);
}
