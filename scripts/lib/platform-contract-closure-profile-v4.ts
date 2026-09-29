import type { JsonRecord } from "./platform-json-semantics";
import type { ImmutableFileRecord } from "./platform-successor-predecessor";
import { SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS } from "./platform-successor-dag";
import {
  CONTRACT_CLOSURE_CRITERIA,
  CONTRACT_CLOSURE_MISSING,
  CONTRACT_CLOSURE_REQUIRED_VERDICT as REQUIRED_VERDICT,
  createContractClosureProfile,
  type ContractClosureCriterion,
  type ContractClosureProfile,
  type ContractClosureProfileCurrent,
  type ContractClosureRegistry,
  type ContractClosureReview,
  type ContractClosureSource,
} from "./platform-contract-closure-profile-successor";

// v4 adds the authority markdown, the immutable v3 closure fence and the replay
// authority to the v3 bindings; everything else lives in the shared kernel.

const RUNTIME_REVIEW_PATH =
  "docs/plan/p1/g-contract-runtime-current-lineage-rebind-independent-review-20260828.md";

const V4_AUTHORITY_PATH =
  "docs/plan/p1/g-contract-runtime-closure-profile-v4-authority-20260828.md";

// The authority markdown is a predecessor input to the generated v4 objects.
// Its identity is pinned to the authority-lineage commit that records the
// complete decision and candidate binding; later commits may carry the same
// blob but may not silently replace it.
export const CONTRACT_CLOSURE_V4_AUTHORITY_FILE = Object.freeze({
  path: V4_AUTHORITY_PATH,
  mode: "100644",
  gitBlob: "307dfbbe6a9f2f6696ae435996b447d595c1f9ac",
  sha256: "0a61c6d3029d595d2ea8a573b1130d77d0756e0b1739a030d52f7f6b8eac2e9a",
  sizeBytes: 16_991,
  commit: "41ffbc665d51e29aa988de6be1d01bb29954f149",
  tree: "1b00a41b907f1a7f586d8b9f65ee491aeb59c6c5",
} as const);

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

export const CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES = Object.freeze([
  Object.freeze({
    path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
    gitBlob: "d714424ac6b42a44ee775a6edde6327d87f2d7c3",
    sha256: "e8384fb25f3828dfafeecf0040110df3a51cd64ce5877e966ecec12769099bf4",
    sizeBytes: 14_215,
  }),
  Object.freeze({
    path: "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json",
    gitBlob: "58f651367aea31c5662423b602bf293d085a8afa",
    sha256: "face6b9f01732255d4f3ae3aebb040d0af19efae416bad074a2f84510e385862",
    sizeBytes: 13_451,
  }),
  Object.freeze({
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json",
    gitBlob: "eb2c46f916ac52b13a6d225685bf48064cf35836",
    sha256: "3fbc85313f2195860b6211f8c31fc185d825469146f703b7e442c34b0612ed25",
    sizeBytes: 23_642,
  }),
  Object.freeze({
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json",
    gitBlob: "ccdef422ab3ef6a61cb2be8ff1e071572cd99374",
    sha256: "3a98b5558cf7d359e4854a46ab95a1a14fb3cc1298a304954d4033a092f8fcb2",
    sizeBytes: 2_080,
  }),
  Object.freeze({
    path: "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md",
    gitBlob: "95cd52d4074852f1792620bcac8cf6bf6ffc0853",
    sha256: "83975f780dbcaed587988155f680c33e3b1a42ee10776af2a3077a5482d13001",
    sizeBytes: 10_102,
  }),
] as const satisfies readonly V4PredecessorFileRecord[]);

const CONTRACT_CLOSURE_V4_V3_BASELINE = {
  commit: "16275f6cbf390c343a9ac00f9193e75eaad0094e",
  tree: "ca595b8e1258a8b78c4da3a545b2a31d8f62b531",
} as const;

export const CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE = Object.freeze({
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
} as const);

export const CONTRACT_CLOSURE_V4_RUNTIME_FILES = Object.freeze([
  Object.freeze({
    path: "services/control-plane/go.mod",
    sha256: "d27871e7d4d8788d455ac2a5b9d512b0b6628903fad05213a9e227c0f0883d3d",
    sizeBytes: 672,
  }),
  Object.freeze({
    path: "services/control-plane/go.sum",
    sha256: "4b870f580591894010f0762c8d04b83cba95a5c09eabc4ffc2631e41290abfbc",
    sizeBytes: 3634,
  }),
] as const satisfies readonly ImmutableFileRecord[]);

export const CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE = Object.freeze({
  path: RUNTIME_REVIEW_PATH,
  sha256: "46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6",
  sizeBytes: 5030,
} as const satisfies ImmutableFileRecord);

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

const closure = createContractClosureProfile<ContractClosureV4Source, ContractClosureV4Registry>({
  version: "v4",
  Error: ContractClosureProfileV4Error as new (
    code: string,
    path: string,
    message: string,
  ) => Error,
  runtimeGitLineage: CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE,
  runtimeFiles: CONTRACT_CLOSURE_V4_RUNTIME_FILES,
  runtimeReviewFile: CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE,
  successor: {
    authorityRevision: "D-053-EC-2.r4",
    authorityFile: CONTRACT_CLOSURE_V4_AUTHORITY_FILE,
    supersededVersion: "v3",
    supersededBaseline: CONTRACT_CLOSURE_V4_V3_BASELINE,
    supersededFiles: CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES,
    replayAuthority: V4_REPLAY_AUTHORITY as unknown as JsonRecord,
  },
});

export type ContractClosureV4Review = ContractClosureReview;
export type ContractClosureV4Criterion = ContractClosureCriterion;
export type ContractClosureV4Profile = ContractClosureProfile;
export type ContractClosureV4Source = ContractClosureSource & {
  readonly authorityRevision: "D-053-EC-2.r4";
  readonly authorityBinding: JsonRecord;
  readonly supersededV3Predecessor: JsonRecord;
  readonly replayAuthority: JsonRecord;
};
export type ContractClosureV4Registry = ContractClosureRegistry & {
  readonly authorityRevision: "D-053-EC-2.r4";
  readonly authorityBinding: JsonRecord;
  readonly supersededV3Predecessor: JsonRecord;
  readonly replayAuthority: JsonRecord;
};
export type ContractClosureProfileV4Current = ContractClosureProfileCurrent<
  ContractClosureV4Source,
  ContractClosureV4Registry
>;

export const CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH = closure.paths.source;
export const CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH = closure.paths.output;
export const CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH = closure.paths.sourceSchema;
export const CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH = closure.paths.outputSchema;
export const CONTRACT_CLOSURE_V4_CRITERIA = CONTRACT_CLOSURE_CRITERIA;
export const CONTRACT_CLOSURE_V4_MISSING = CONTRACT_CLOSURE_MISSING;

export const buildContractClosureProfileV4Source = closure.buildSource;
/** @deprecated Use buildContractClosureProfileV4Source for canonical production generation. */
export const buildContractClosureProfileV4TestSource = buildContractClosureProfileV4Source;
export const validateContractClosureProfileV4Source = closure.validateSource;
export const assertContractClosureProfileV4Current = closure.assertCurrent;
export const assertContractClosureProfileV4SourceCurrent = closure.assertSourceCurrent;
export const assertContractClosureProfileV4CurrentMutationForTest =
  closure.assertCurrentMutationForTest;
export const assertContractClosureV4V2DependencyABAMutationForTest =
  closure.assertV2DependencyABAMutationForTest;
export const assertContractClosureV4RuntimeGitLineageCurrent =
  closure.assertRuntimeGitLineageCurrent;
export const assertContractClosureV4RepositoryLineageCurrent =
  closure.assertRepositoryLineageCurrent;
export const buildContractClosureProfileV4Registry = closure.buildRegistry;
export const assertContractClosureV4RegistrySemantics: (
  root: string,
  document: unknown,
) => asserts document is ContractClosureV4Registry = closure.assertRegistrySemantics;
export const deriveContractClosureV4Missing = closure.deriveMissing;
export const serializeContractClosureProfileV4Registry = closure.serializeRegistry;
export const serializeContractClosureProfileV4Source = closure.serializeSource;
export const writeContractClosureProfileV4Source = closure.writeSource;
export const writeContractClosureProfileV4 = closure.write;
