import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
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

import {
  assertExactSuccessorV3ProjectionExclusions,
  assertSuccessorV3CoreGeneratorOutputAuthority,
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_REPLAY_AUTHORITY_FILES,
} from "./platform-successor-dag-v3";

export const SUCCESSOR_V3_SOURCE_PATH = "tools/generator-supply/v3/source.json";
export const SUCCESSOR_V3_BASELINE_COMMIT = "16275f6cbf390c343a9ac00f9193e75eaad0094e";
export const SUCCESSOR_V3_BASELINE_TREE = "ca595b8e1258a8b78c4da3a545b2a31d8f62b531";

export type SuccessorV3FileRecord = Readonly<{
  path: string;
  mode: "100644";
  gitBlob: string;
  sha256: string;
  sizeBytes: number;
}>;

type HistoricalCoreGeneratorOutput = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
  gitMode: "100644";
}>;

type SuccessorV3Group = Readonly<{
  id: string;
  files: readonly SuccessorV3FileRecord[];
}>;

type SuccessorV3EvidenceManifest = Readonly<{
  id: string;
  manifest: SuccessorV3FileRecord;
  algorithm: string;
  memberCount: number;
  memberPathPrefix: string;
}>;

type SuccessorV3GitChange = Readonly<{
  operation: "A" | "M";
  mode: "100644";
  path: string;
}>;

type SuccessorV3GitStep = Readonly<{
  role: string;
  commit: string;
  tree: string;
  parent: string;
  diffSha256: string;
  changes: readonly SuccessorV3GitChange[];
}>;

export type SuccessorV3Source = Readonly<{
  formatVersion: string;
  registryId: string;
  decisionId: string;
  baseline: Readonly<{
    commit: string;
    tree: string;
    gateCriteria: Readonly<{ path: string; sha256: string }>;
    mutation: string;
  }>;
  predecessorClosure: Readonly<{
    groups: readonly SuccessorV3Group[];
    evidenceManifests: readonly SuccessorV3EvidenceManifest[];
    legacySupplyV1Lineage: Readonly<Record<string, unknown>>;
    gitChain: readonly SuccessorV3GitStep[];
  }>;
  replayContract: Readonly<{
    authorityFiles: Readonly<
      Record<
        keyof typeof SUCCESSOR_V3_REPLAY_AUTHORITY_FILES,
        Readonly<{
          path: string;
          sha256: string;
          sizeBytes: number;
        }>
      >
    >;
    coreGeneratorOutputs: readonly SuccessorV3FileRecord[];
    projectionExclusions: readonly string[];
    preReplayExclusionPolicy: string;
    authoritativeReplayScope: string;
    wrapperPolicy: string;
    algorithms: Readonly<Record<string, unknown>>;
    receiptFormats: Readonly<Record<string, unknown>>;
  }>;
  declaredProfile: Readonly<Record<string, unknown>>;
  replayEvidence: Readonly<Record<string, unknown>>;
}>;

export class SuccessorV3PredecessorError extends Error {
  constructor(
    readonly code:
      | "SUCCESSOR_V3_SOURCE_INVALID"
      | "SUCCESSOR_V3_FILE_MISMATCH"
      | "SUCCESSOR_V3_MANIFEST_MISMATCH"
      | "SUCCESSOR_V3_GIT_MISMATCH"
      | "SUCCESSOR_V3_PATH_INVALID",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "SuccessorV3PredecessorError";
  }
}

type StableIdentity = Readonly<{
  absolute: string;
  dev: bigint;
  ino: bigint;
  mode: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

type Snapshot = {
  readonly rootReal: string;
  readonly identities: Map<string, StableIdentity>;
  readonly mutationHook?: {
    readonly afterPath: string;
    readonly mutate: () => void;
    fired: boolean;
  };
};

export type SuccessorV3PredecessorSnapshot = Readonly<{
  source: SuccessorV3Source;
  assertCurrent: () => void;
}>;

const GROUP_AUTHORITIES = [
  {
    id: "contract-closure-v1",
    paths: [
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json",
      "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json",
    ],
  },
  {
    id: "contract-closure-v2",
    paths: [
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json",
      "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v2.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v2.schema.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-v2.schema.json",
    ],
  },
  {
    id: "contract-closure-v3",
    paths: [
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
      "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json",
      "contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json",
      "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md",
    ],
  },
  {
    id: "generator-supply-v1",
    paths: [
      "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
      "tools/generator-supply/v1/evidence-manifest.json",
      "tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json",
      "tools/generator-supply/v1/generator-supply-profile-v1.schema.json",
      "tools/generator-supply/v1/profile.json",
      "tools/generator-supply/v1/source.json",
    ],
  },
  {
    id: "generator-supply-v2",
    paths: [
      "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md",
      "tools/generator-supply/v2/evidence-manifest.json",
      "tools/generator-supply/v2/generator-supply-profile-source-v2.schema.json",
      "tools/generator-supply/v2/generator-supply-profile-v2.schema.json",
      "tools/generator-supply/v2/profile.json",
      "tools/generator-supply/v2/source.json",
    ],
  },
  {
    id: "contract-review-binding-v1",
    paths: [
      "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md",
      "tools/contract-review-binding/v1/registry.json",
      "tools/contract-review-binding/v1/review-binding-registry-v1.schema.json",
      "tools/contract-review-binding/v1/review-binding-source-v1.schema.json",
      "tools/contract-review-binding/v1/review-tuple-v1.schema.json",
      "tools/contract-review-binding/v1/review-tuple.json",
      "tools/contract-review-binding/v1/source.json",
    ],
  },
  {
    id: "g-contract-r1-r4-history",
    paths: [
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R1.md",
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R2.md",
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R3.md",
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md",
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4.md",
      "docs/plan/p1/g-contract-standards-independent-review-r3-20260823.md",
    ],
  },
  {
    id: "generation-lock-v2",
    paths: ["contracts/generation.lock.json"],
  },
] as const;

const MANIFEST_AUTHORITIES = [
  {
    id: "generator-supply-v1",
    path: "tools/generator-supply/v1/evidence-manifest.json",
    algorithm: "sorted-path-nul-sha256-nul-size-v1",
    memberCount: 39,
    memberPathPrefix: "tools/generator-supply/v1/evidence/",
  },
  {
    id: "generator-supply-v2",
    path: "tools/generator-supply/v2/evidence-manifest.json",
    algorithm: "sorted-path-nul-sha256-nul-size-v1",
    memberCount: 8,
    memberPathPrefix: "tools/generator-supply/v2/evidence/",
  },
] as const;

const HISTORICAL_GENERATION_LOCK_V2 = Object.freeze({
  groupId: "generation-lock-v2",
  path: "contracts/generation.lock.json",
  formatVersion: "cloud-agents-platform-contract-generation-lock/v2",
  status: "SUCCESSOR_ASSEMBLED_REVIEW_BOUND",
  lockDigest: "sha256:b0d08160c2c7cf35f91940fc4b644160d715acd3d6c5796ea456b2c005dcfa4f",
  digestDomain: "cloud-agents/platform-contract-generation-lock/document/v2",
  coreOutputManifestSha256:
    "sha256:d0136124f1f760ae60c34e3b0e47161cb528fa3222f3330a440338f6a47da50e",
} as const);

const LEGACY_SUPPLY_V1_LINEAGE = {
  candidateCommit: "e5f981c8197cea7527a57c391e7198570f61b92c",
  candidateTree: "7fb98abf71066e8009581c658b41a299ae1a5c2c",
  candidateParent: "0a331fde18a909d37b64f11efe879df7bbc09d25",
  candidateDiffSha256: "d012683bf1a13dda79a8393afdf44ff20088711b9ccce1c608cd74db5843587e",
  reviewCommit: "129e9bc128de971b9f9623e82832e80830331126",
  reviewTree: "b30835163d757e236781af8c16c61736e1d452da",
  reviewParent: "e5f981c8197cea7527a57c391e7198570f61b92c",
  reviewPath: "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
  reviewSha256: "86ec054debf15de71481d6f9ab965ca5c8f24a4f5a98f9e5e155e24df261cd47",
  verdict: "APPROVE_P0_0_P1_0_P2_0",
} as const;

const GIT_CHAIN_AUTHORITY = [
  {
    role: "GENERATOR_SUPPLY_V2_ASSEMBLED",
    commit: "1ba7eda5ad6241ad8a065408d787e73cd7013ce0",
    tree: "5a73a8edd4aee56a38aeb37c37b8009e481dfeae",
    parent: "5def3ad5deb157264429dc5178f57ec916c66dc7",
    diffSha256: "c5d11be264e6b9bc1e0a5c16c5b320c2d987a26b12ab2f7b4fc16efb594c92e2",
    changes: [
      ["M", "contracts/generation.lock.json"],
      ["A", "tools/generator-supply/v2/evidence-manifest.json"],
      ["A", "tools/generator-supply/v2/evidence/replay.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/darwin-a.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/darwin-b.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/darwin-isolation.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/linux-a.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/linux-b.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/linux-isolation.json"],
      ["A", "tools/generator-supply/v2/evidence/replay/projection.json"],
      ["A", "tools/generator-supply/v2/profile.json"],
    ],
  },
  {
    role: "CLOSURE_V3_AND_SUPPLY_V2_REVIEWED",
    commit: "d7c7468a72facc091b8a42be54d5af5c6a5785c4",
    tree: "92d44592cc39b513ffcbd47088a6560ff87c67ec",
    parent: "1ba7eda5ad6241ad8a065408d787e73cd7013ce0",
    diffSha256: "fc8c57e55fc213bfa31b4b009023506b522edd19f987841a142a7df3a5ffac3e",
    changes: [
      ["A", "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md"],
      ["A", "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md"],
    ],
  },
  {
    role: "DETACHED_REVIEW_BINDING_ASSEMBLED",
    commit: "a595bd93ceee9d352645b9be66db92517fffb092",
    tree: "9249e43bae506acef702fe70cdde798b37bd5148",
    parent: "d7c7468a72facc091b8a42be54d5af5c6a5785c4",
    diffSha256: "2807c531a949afad97252f8bb5dfd738885f34e7d4ad93484c9411b146a846ef",
    changes: [
      ["M", "contracts/generation.lock.json"],
      ["A", "tools/contract-review-binding/v1/registry.json"],
      ["A", "tools/contract-review-binding/v1/review-tuple.json"],
    ],
  },
  {
    role: "DETACHED_REVIEW_BINDING_FINAL_REVIEW",
    commit: SUCCESSOR_V3_BASELINE_COMMIT,
    tree: SUCCESSOR_V3_BASELINE_TREE,
    parent: "a595bd93ceee9d352645b9be66db92517fffb092",
    diffSha256: "6bab51ddaa07bacb1fe13f3e572f6ff31ba28903065cc9257269c9b67960b475",
    changes: [
      ["A", "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md"],
    ],
  },
] as const;

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
  "diff.external=",
  "-c",
  "diff.mnemonicPrefix=false",
  "-c",
  "diff.noprefix=false",
  "-c",
  "diff.renames=false",
] as const;

export function loadAndAssertSuccessorV3Source(root: string): SuccessorV3Source {
  const snapshot: Snapshot = {
    rootReal: realpathSync(root),
    identities: new Map(),
  };
  const bytes = readStableFile(root, SUCCESSOR_V3_SOURCE_PATH, snapshot);
  return parseAndAssertSource(bytes);
}

export function assertSuccessorV3SourceForTest(value: unknown): asserts value is SuccessorV3Source {
  assertSource(value);
}

export function assertSuccessorV3PredecessorsCurrent(root: string): void {
  captureSuccessorV3PredecessorSnapshot(root);
}

export function captureSuccessorV3PredecessorSnapshot(
  root: string,
): SuccessorV3PredecessorSnapshot {
  return captureSnapshot(root);
}

export function assertSuccessorV3SnapshotMutationForTest(
  root: string,
  afterPath: string,
  mutate: () => void,
): void {
  captureSnapshot(root, { afterPath, mutate, fired: false });
}

export function assertSuccessorV3StableFileMapForTest(
  root: string,
  records: readonly Pick<SuccessorV3FileRecord, "path" | "sha256" | "sizeBytes">[],
  mutationHook?: { readonly afterPath: string; readonly mutate: () => void; fired: boolean },
): void {
  const snapshot: Snapshot = {
    rootReal: realpathSync(root),
    identities: new Map(),
    mutationHook,
  };
  for (const record of records) {
    const bytes = readStableFile(root, record.path, snapshot);
    if (bytes.byteLength !== record.sizeBytes || digest(bytes) !== record.sha256) {
      fail("SUCCESSOR_V3_FILE_MISMATCH", record.path, "Stable file bytes do not match.");
    }
  }
  if (mutationHook !== undefined && !mutationHook.fired) {
    fail("SUCCESSOR_V3_FILE_MISMATCH", mutationHook.afterPath, "Mutation hook did not fire.");
  }
  assertSnapshotCurrent(root, snapshot);
}

export function assertSuccessorV3HistoricalGenerationLockV2ForTest(
  record: SuccessorV3FileRecord,
  entry: Readonly<{ mode: string; type: string; object: string }>,
  bytes: Uint8Array,
): void {
  assertHistoricalGenerationLockV2(record, entry, Buffer.from(bytes));
}

/** Test-only parser for the immutable v2 core-output map. */
export function assertSuccessorV3HistoricalCoreGeneratorOutputFenceForTest(
  root: string,
  value: unknown,
): void {
  const records = assertHistoricalCoreGeneratorOutputFenceDocument(value);
  verifyHistoricalCoreGeneratorOutputFenceRecords(root, records);
}

function captureSnapshot(
  root: string,
  mutationHook?: Snapshot["mutationHook"],
): SuccessorV3PredecessorSnapshot {
  const snapshot: Snapshot = {
    rootReal: realpathSync(root),
    identities: new Map(),
    mutationHook,
  };
  const sourceBytes = readStableFile(root, SUCCESSOR_V3_SOURCE_PATH, snapshot);
  const source = parseAndAssertSource(sourceBytes);

  assertGitTopLevelAndBaseline(root);
  verifyReplayAuthorityFiles(root, source.replayContract.authorityFiles, snapshot);
  for (const group of source.predecessorClosure.groups) {
    for (const record of group.files) {
      if (group.id === HISTORICAL_GENERATION_LOCK_V2.groupId) {
        verifyHistoricalGenerationLockV2(root, record);
      } else {
        verifyFixedFile(root, record, snapshot);
      }
    }
  }
  verifyHistoricalCoreGeneratorOutputFence(root);
  for (const manifest of source.predecessorClosure.evidenceManifests) {
    verifyEvidenceManifest(root, manifest, snapshot);
  }
  for (const record of source.replayContract.coreGeneratorOutputs) {
    verifyCurrentCoreGeneratorOutput(root, record, snapshot);
  }
  verifyGateCriteria(root, source, snapshot);
  verifyClosedDirectoryTrees(root, source);
  verifyLegacySupplyV1Lineage(root, source.predecessorClosure.legacySupplyV1Lineage);
  verifyGitChain(root, source.predecessorClosure.gitChain);

  if (mutationHook !== undefined && !mutationHook.fired) {
    fail("SUCCESSOR_V3_FILE_MISMATCH", mutationHook.afterPath, "Mutation hook did not fire.");
  }
  assertSnapshotCurrent(root, snapshot);
  return Object.freeze({
    source,
    assertCurrent: (): void => assertSnapshotCurrent(root, snapshot),
  });
}

function parseAndAssertSource(bytes: Buffer): SuccessorV3Source {
  let value: unknown;
  try {
    value = JSON.parse(bytes.toString("utf8"));
  } catch {
    fail("SUCCESSOR_V3_SOURCE_INVALID", SUCCESSOR_V3_SOURCE_PATH, "Source is not valid JSON.");
  }
  assertSource(value);
  return value;
}

function assertSource(value: unknown): asserts value is SuccessorV3Source {
  const source = requireRecord(value, "/");
  exactKeys(
    source,
    [
      "formatVersion",
      "registryId",
      "decisionId",
      "baseline",
      "predecessorClosure",
      "replayContract",
      "declaredProfile",
      "replayEvidence",
    ],
    "/",
  );
  if (
    source.formatVersion !== "cloud-agents-generator-supply-profile-source/v3" ||
    source.registryId !== "cloud-agents/generator-supply-profile" ||
    source.decisionId !== "D-053"
  ) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", "/", "Source identity drifted.");
  }

  const baseline = requireRecord(source.baseline, "/baseline");
  exactKeys(baseline, ["commit", "tree", "gateCriteria", "mutation"], "/baseline");
  const criteria = requireRecord(baseline.gateCriteria, "/baseline/gateCriteria");
  exactKeys(criteria, ["path", "sha256"], "/baseline/gateCriteria");
  if (
    baseline.commit !== SUCCESSOR_V3_BASELINE_COMMIT ||
    baseline.tree !== SUCCESSOR_V3_BASELINE_TREE ||
    baseline.mutation !== "forbidden" ||
    criteria.path !== "docs/plan/cloud-agents-platform/05-gates-and-acceptance.md" ||
    criteria.sha256 !== "4a3d0b3c184e9673944411adbc5c8ea933883c855d5aada67862dad8e4dcc994"
  ) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", "/baseline", "Fixed baseline authority drifted.");
  }

  const closure = requireRecord(source.predecessorClosure, "/predecessorClosure");
  exactKeys(
    closure,
    ["groups", "evidenceManifests", "legacySupplyV1Lineage", "gitChain"],
    "/predecessorClosure",
  );
  if (!Array.isArray(closure.groups) || closure.groups.length !== GROUP_AUTHORITIES.length) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", "/predecessorClosure/groups", "Group count drifted.");
  }
  for (const [index, authority] of GROUP_AUTHORITIES.entries()) {
    const group = requireRecord(closure.groups[index], `/predecessorClosure/groups/${index}`);
    exactKeys(group, ["id", "files"], `/predecessorClosure/groups/${index}`);
    if (group.id !== authority.id || !Array.isArray(group.files)) {
      fail(
        "SUCCESSOR_V3_SOURCE_INVALID",
        `/predecessorClosure/groups/${index}`,
        "Group identity drifted.",
      );
    }
    assertFileRecordArray(
      group.files,
      authority.paths,
      `/predecessorClosure/groups/${index}/files`,
    );
  }

  if (!Array.isArray(closure.evidenceManifests) || closure.evidenceManifests.length !== 2) {
    fail(
      "SUCCESSOR_V3_SOURCE_INVALID",
      "/predecessorClosure/evidenceManifests",
      "Manifest authority count drifted.",
    );
  }
  for (const [index, authority] of MANIFEST_AUTHORITIES.entries()) {
    const manifest = requireRecord(
      closure.evidenceManifests[index],
      `/predecessorClosure/evidenceManifests/${index}`,
    );
    exactKeys(
      manifest,
      ["id", "manifest", "algorithm", "memberCount", "memberPathPrefix"],
      `/predecessorClosure/evidenceManifests/${index}`,
    );
    const record = assertFileRecord(
      manifest.manifest,
      `/predecessorClosure/evidenceManifests/${index}/manifest`,
    );
    if (
      manifest.id !== authority.id ||
      manifest.algorithm !== authority.algorithm ||
      manifest.memberCount !== authority.memberCount ||
      manifest.memberPathPrefix !== authority.memberPathPrefix ||
      record.path !== authority.path
    ) {
      fail(
        "SUCCESSOR_V3_SOURCE_INVALID",
        `/predecessorClosure/evidenceManifests/${index}`,
        "Manifest authority drifted.",
      );
    }
  }
  assertExactObject(
    closure.legacySupplyV1Lineage,
    LEGACY_SUPPLY_V1_LINEAGE,
    "/predecessorClosure/legacySupplyV1Lineage",
  );
  assertGitChainSource(closure.gitChain);

  const replay = requireRecord(source.replayContract, "/replayContract");
  exactKeys(
    replay,
    [
      "authorityFiles",
      "coreGeneratorOutputs",
      "projectionExclusions",
      "preReplayExclusionPolicy",
      "authoritativeReplayScope",
      "wrapperPolicy",
      "algorithms",
      "receiptFormats",
    ],
    "/replayContract",
  );
  assertReplayAuthoritySource(replay.authorityFiles);
  if (!Array.isArray(replay.coreGeneratorOutputs)) {
    fail(
      "SUCCESSOR_V3_SOURCE_INVALID",
      "/replayContract/coreGeneratorOutputs",
      "Core output records are absent.",
    );
  }
  assertFileRecordArray(
    replay.coreGeneratorOutputs,
    SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
    "/replayContract/coreGeneratorOutputs",
  );
  assertSuccessorV3CoreGeneratorOutputAuthority(
    replay.coreGeneratorOutputs.map(({ path }) => path),
  );
  if (!Array.isArray(replay.projectionExclusions)) {
    fail(
      "SUCCESSOR_V3_SOURCE_INVALID",
      "/replayContract/projectionExclusions",
      "Projection exclusions are absent.",
    );
  }
  assertExactSuccessorV3ProjectionExclusions(replay.projectionExclusions as string[]);
  if (
    replay.preReplayExclusionPolicy !==
      "EXACT17_ONLY_NO_WILDCARD_ALL_OTHER_TRACKED_BYTES_INCLUDED" ||
    replay.authoritativeReplayScope !==
      "EXACT49_CORE_OUTPUTS_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY" ||
    replay.wrapperPolicy !== "VERSIONED_ISOLATION_WRAPPER_V3"
  ) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", "/replayContract", "Replay boundary drifted.");
  }
  assertExactObject(
    replay.algorithms,
    {
      nodeModulesManifest: "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1",
      projectionArchiveMemberManifest:
        "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      inputTreeManifest: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
    },
    "/replayContract/algorithms",
  );
  assertExactObject(
    replay.receiptFormats,
    {
      summary: "cloud-agents-generator-supply-replay-summary/v3",
      run: "cloud-agents-generator-replay-run/v1",
      isolation: "cloud-agents-generator-replay-isolation/v1",
      projection: "cloud-agents-core-generator-projection/v1",
    },
    "/replayContract/receiptFormats",
  );

  assertDeclaredProfile(source.declaredProfile);
  assertReplayEvidence(source.replayEvidence);
}

function assertDeclaredProfile(value: unknown): void {
  const profile = requireRecord(value, "/declaredProfile");
  exactKeys(
    profile,
    ["profileId", "status", "notGateClosure", "platforms", "boundaries"],
    "/declaredProfile",
  );
  if (
    profile.profileId !== "cloud-agents/generator-supply-profile/v3" ||
    profile.status !== "REPLAY_VERIFIED_REVIEW_PENDING" ||
    profile.notGateClosure !== true ||
    !Array.isArray(profile.platforms) ||
    profile.platforms.length !== 3
  ) {
    fail(
      "SUCCESSOR_V3_SOURCE_INVALID",
      "/declaredProfile",
      "Profile identity or non-Gate status drifted.",
    );
  }
  const platforms = [
    ["darwin-arm64", "NATIVE_REPLAY_VERIFIED", true],
    ["linux-amd64", "NATIVE_REPLAY_VERIFIED", true],
    ["linux-arm64", "NOT_CLAIMED", false],
  ] as const;
  for (const [index, expected] of platforms.entries()) {
    const platform = requireRecord(profile.platforms[index], `/declaredProfile/platforms/${index}`);
    exactKeys(platform, ["id", "status", "nativeExecution"], `/declaredProfile/platforms/${index}`);
    if (
      platform.id !== expected[0] ||
      platform.status !== expected[1] ||
      platform.nativeExecution !== expected[2]
    ) {
      fail(
        "SUCCESSOR_V3_SOURCE_INVALID",
        `/declaredProfile/platforms/${index}`,
        "Platform declaration drifted.",
      );
    }
  }
  assertExactObject(
    profile.boundaries,
    {
      gate: "ALL_GATES_OPEN",
      productionDatabase: "NOT_AUTHORIZED",
      http: "NOT_IMPLEMENTED",
      p2: "NOT_IMPLEMENTED",
      provider: "NOT_AUTHORIZED",
      deployment: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
      release: "NOT_AUTHORIZED",
      legalApproval: "NOT_CLAIMED",
      currentVulnerabilityClosure: "NOT_CLAIMED",
      externalSignatureTrust: "NOT_PRODUCED_NOT_AUTHORIZED",
      fullDistributionCoverage: "NOT_CLAIMED",
      bootstrapDiscovery: "FORBIDDEN",
    },
    "/declaredProfile/boundaries",
  );
}

function assertReplayEvidence(value: unknown): void {
  const evidence = requireRecord(value, "/replayEvidence");
  exactKeys(evidence, ["state", "authority", "receiptPaths"], "/replayEvidence");
  if (
    evidence.state !== "DECLARED_PRE_REPLAY" ||
    evidence.authority !== "EXTERNAL_LATE_BOUND" ||
    !Array.isArray(evidence.receiptPaths) ||
    !sameStrings(evidence.receiptPaths, SUCCESSOR_V3_PROJECTION_EXCLUSIONS.slice(3, 11))
  ) {
    fail(
      "SUCCESSOR_V3_SOURCE_INVALID",
      "/replayEvidence",
      "Late replay evidence authority drifted.",
    );
  }
}

function assertFileRecordArray(value: unknown[], paths: readonly string[], pointer: string): void {
  if (value.length !== paths.length) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", pointer, "File record count drifted.");
  }
  const seen = new Set<string>();
  for (const [index, expectedPath] of paths.entries()) {
    const record = assertFileRecord(value[index], `${pointer}/${index}`);
    if (record.path !== expectedPath || seen.has(record.path)) {
      fail(
        "SUCCESSOR_V3_SOURCE_INVALID",
        `${pointer}/${index}`,
        "File record path order or uniqueness drifted.",
      );
    }
    seen.add(record.path);
  }
}

function assertFileRecord(value: unknown, pointer: string): SuccessorV3FileRecord {
  const record = requireRecord(value, pointer);
  exactKeys(record, ["path", "mode", "gitBlob", "sha256", "sizeBytes"], pointer);
  if (
    typeof record.path !== "string" ||
    record.mode !== "100644" ||
    typeof record.gitBlob !== "string" ||
    !/^[0-9a-f]{40}$/u.test(record.gitBlob) ||
    typeof record.sha256 !== "string" ||
    !/^[0-9a-f]{64}$/u.test(record.sha256) ||
    typeof record.sizeBytes !== "number" ||
    !Number.isSafeInteger(record.sizeBytes) ||
    record.sizeBytes < 1
  ) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", pointer, "File record fields are invalid.");
  }
  validatePath(record.path);
  return record as SuccessorV3FileRecord;
}

type SuccessorV3ReplayAuthorityRecord = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
}>;

function assertReplayAuthoritySource(value: unknown): void {
  const authority = requireRecord(value, "/replayContract/authorityFiles");
  const names = Object.keys(SUCCESSOR_V3_REPLAY_AUTHORITY_FILES) as Array<
    keyof typeof SUCCESSOR_V3_REPLAY_AUTHORITY_FILES
  >;
  exactKeys(authority, names, "/replayContract/authorityFiles");
  for (const name of names) {
    const pointer = `/replayContract/authorityFiles/${name}`;
    const record = requireRecord(authority[name], pointer);
    exactKeys(record, ["path", "sha256", "sizeBytes"], pointer);
    if (
      record.path !== SUCCESSOR_V3_REPLAY_AUTHORITY_FILES[name] ||
      typeof record.sha256 !== "string" ||
      !/^[0-9a-f]{64}$/u.test(record.sha256) ||
      typeof record.sizeBytes !== "number" ||
      !Number.isSafeInteger(record.sizeBytes) ||
      record.sizeBytes < 1
    ) {
      fail("SUCCESSOR_V3_SOURCE_INVALID", pointer, "Replay authority record drifted.");
    }
    validatePath(record.path);
  }
}

function verifyReplayAuthorityFiles(
  root: string,
  value: Readonly<Record<string, SuccessorV3ReplayAuthorityRecord>>,
  snapshot: Snapshot,
): void {
  for (const name of Object.keys(SUCCESSOR_V3_REPLAY_AUTHORITY_FILES) as Array<
    keyof typeof SUCCESSOR_V3_REPLAY_AUTHORITY_FILES
  >) {
    const record = value[name];
    const bytes = readStableFile(root, record.path, snapshot);
    if (bytes.byteLength !== record.sizeBytes || digest(bytes) !== record.sha256) {
      fail("SUCCESSOR_V3_FILE_MISMATCH", record.path, "Replay authority bytes drifted.");
    }
  }
}

function assertGitChainSource(value: unknown): void {
  if (!Array.isArray(value) || value.length !== GIT_CHAIN_AUTHORITY.length) {
    fail("SUCCESSOR_V3_SOURCE_INVALID", "/predecessorClosure/gitChain", "Git chain count drifted.");
  }
  for (const [index, authority] of GIT_CHAIN_AUTHORITY.entries()) {
    const step = requireRecord(value[index], `/predecessorClosure/gitChain/${index}`);
    exactKeys(
      step,
      ["role", "commit", "tree", "parent", "diffSha256", "changes"],
      `/predecessorClosure/gitChain/${index}`,
    );
    if (
      step.role !== authority.role ||
      step.commit !== authority.commit ||
      step.tree !== authority.tree ||
      step.parent !== authority.parent ||
      step.diffSha256 !== authority.diffSha256 ||
      !Array.isArray(step.changes) ||
      step.changes.length !== authority.changes.length
    ) {
      fail(
        "SUCCESSOR_V3_SOURCE_INVALID",
        `/predecessorClosure/gitChain/${index}`,
        "Fixed Git step drifted.",
      );
    }
    for (const [changeIndex, expected] of authority.changes.entries()) {
      const change = requireRecord(
        step.changes[changeIndex],
        `/predecessorClosure/gitChain/${index}/changes/${changeIndex}`,
      );
      exactKeys(
        change,
        ["operation", "mode", "path"],
        `/predecessorClosure/gitChain/${index}/changes/${changeIndex}`,
      );
      if (
        change.operation !== expected[0] ||
        change.mode !== "100644" ||
        change.path !== expected[1]
      ) {
        fail(
          "SUCCESSOR_V3_SOURCE_INVALID",
          `/predecessorClosure/gitChain/${index}/changes/${changeIndex}`,
          "Git change order drifted.",
        );
      }
    }
  }
}

function verifyFixedFile(root: string, record: SuccessorV3FileRecord, snapshot: Snapshot): void {
  const bytes = readStableFile(root, record.path, snapshot);
  if (bytes.byteLength !== record.sizeBytes || digest(bytes) !== record.sha256) {
    fail("SUCCESSOR_V3_FILE_MISMATCH", record.path, "Predecessor bytes drifted.");
  }
  const entry = gitTreeEntry(root, SUCCESSOR_V3_BASELINE_COMMIT, record.path);
  if (entry.mode !== record.mode || entry.type !== "blob" || entry.object !== record.gitBlob) {
    fail("SUCCESSOR_V3_GIT_MISMATCH", record.path, "Baseline mode or Git blob drifted.");
  }
}

function verifyCurrentCoreGeneratorOutput(
  root: string,
  record: SuccessorV3FileRecord,
  snapshot: Snapshot,
): void {
  const bytes = readStableFile(root, record.path, snapshot, { requireNonExecutable: true });
  if (bytes.byteLength !== record.sizeBytes || digest(bytes) !== record.sha256) {
    fail("SUCCESSOR_V3_FILE_MISMATCH", record.path, "Current core output bytes drifted.");
  }
  if (gitBlobObjectId(bytes) !== record.gitBlob) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Current core output record does not bind its current Git blob.",
    );
  }
}

function verifyHistoricalCoreGeneratorOutputFence(root: string): void {
  let bytes: Buffer;
  try {
    bytes = gitBytes(root, [
      "cat-file",
      "blob",
      `${SUCCESSOR_V3_BASELINE_COMMIT}:${HISTORICAL_GENERATION_LOCK_V2.path}`,
    ]);
  } catch {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs`,
      "Historical generation-lock v2 core-output map is unavailable.",
    );
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs`,
      "Historical generation-lock v2 core-output map is not valid JSON.",
    );
  }
  const records = assertHistoricalCoreGeneratorOutputFenceDocument(parsed);
  verifyHistoricalCoreGeneratorOutputFenceRecords(root, records);
}

function verifyHistoricalCoreGeneratorOutputFenceRecords(
  root: string,
  records: readonly HistoricalCoreGeneratorOutput[],
): void {
  for (const record of records) {
    const entry = gitTreeEntry(root, SUCCESSOR_V3_BASELINE_COMMIT, record.path);
    if (entry.mode !== record.gitMode || entry.type !== "blob") {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        record.path,
        "Historical core output mode or object type drifted.",
      );
    }
    let output: Buffer;
    try {
      output = gitBytes(root, [
        "cat-file",
        "blob",
        `${SUCCESSOR_V3_BASELINE_COMMIT}:${record.path}`,
      ]);
    } catch {
      fail("SUCCESSOR_V3_GIT_MISMATCH", record.path, "Historical core output blob is unavailable.");
    }
    if (
      output.byteLength !== record.sizeBytes ||
      `sha256:${digest(output)}` !== record.sha256 ||
      gitBlobObjectId(output) !== entry.object
    ) {
      fail(
        "SUCCESSOR_V3_FILE_MISMATCH",
        record.path,
        "Historical core output bytes or Git blob drifted.",
      );
    }
  }
}

function assertHistoricalCoreGeneratorOutputFenceDocument(
  value: unknown,
): readonly HistoricalCoreGeneratorOutput[] {
  const lock = requireRecord(
    value,
    `${HISTORICAL_GENERATION_LOCK_V2.path}`,
    "SUCCESSOR_V3_GIT_MISMATCH",
  );
  const core = requireRecord(
    lock.coreGeneratorOutputs,
    `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs`,
    "SUCCESSOR_V3_GIT_MISMATCH",
  );
  exactKeys(
    core,
    ["algorithm", "count", "replayCandidateManifestSha256", "files"],
    `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs`,
    "SUCCESSOR_V3_GIT_MISMATCH",
  );
  if (
    core.algorithm !== "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1" ||
    core.count !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length ||
    core.replayCandidateManifestSha256 !== HISTORICAL_GENERATION_LOCK_V2.coreOutputManifestSha256 ||
    !Array.isArray(core.files)
  ) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs`,
      "Historical core-output map metadata drifted.",
    );
  }
  if (core.files.length !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs/files`,
      "Historical core-output map cardinality drifted.",
    );
  }
  const records: HistoricalCoreGeneratorOutput[] = [];
  for (const [index, expectedPath] of SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.entries()) {
    const pointer = `${HISTORICAL_GENERATION_LOCK_V2.path}#/coreGeneratorOutputs/files/${index}`;
    const record = requireRecord(core.files[index], pointer, "SUCCESSOR_V3_GIT_MISMATCH");
    exactKeys(
      record,
      ["path", "sha256", "sizeBytes", "gitMode"],
      pointer,
      "SUCCESSOR_V3_GIT_MISMATCH",
    );
    if (
      record.path !== expectedPath ||
      record.gitMode !== "100644" ||
      typeof record.sha256 !== "string" ||
      !/^sha256:[0-9a-f]{64}$/u.test(record.sha256) ||
      typeof record.sizeBytes !== "number" ||
      !Number.isSafeInteger(record.sizeBytes) ||
      record.sizeBytes < 1
    ) {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        pointer,
        "Historical core-output path, mode, digest, or size drifted.",
      );
    }
    records.push(record as HistoricalCoreGeneratorOutput);
  }
  return records;
}

function verifyHistoricalGenerationLockV2(root: string, record: SuccessorV3FileRecord): void {
  const entry = gitTreeEntry(root, SUCCESSOR_V3_BASELINE_COMMIT, record.path);
  let bytes: Buffer;
  try {
    bytes = gitBytes(root, ["cat-file", "blob", `${SUCCESSOR_V3_BASELINE_COMMIT}:${record.path}`]);
  } catch {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Historical generation-lock v2 blob is unavailable.",
    );
  }
  assertHistoricalGenerationLockV2(record, entry, bytes);
}

function assertHistoricalGenerationLockV2(
  record: SuccessorV3FileRecord,
  entry: Readonly<{ mode: string; type: string; object: string }>,
  bytes: Buffer,
): void {
  if (
    record.path !== HISTORICAL_GENERATION_LOCK_V2.path ||
    entry.mode !== record.mode ||
    entry.type !== "blob" ||
    entry.object !== record.gitBlob ||
    gitBlobObjectId(bytes) !== entry.object
  ) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Historical generation-lock v2 path, mode, or Git blob drifted.",
    );
  }
  if (bytes.byteLength !== record.sizeBytes || digest(bytes) !== record.sha256) {
    fail("SUCCESSOR_V3_FILE_MISMATCH", record.path, "Historical generation-lock v2 bytes drifted.");
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Historical generation-lock v2 is not valid JSON.",
    );
  }
  const document = requireRecord(parsed, record.path, "SUCCESSOR_V3_GIT_MISMATCH");
  if (
    document.formatVersion !== HISTORICAL_GENERATION_LOCK_V2.formatVersion ||
    document.status !== HISTORICAL_GENERATION_LOCK_V2.status ||
    document.lockDigest !== HISTORICAL_GENERATION_LOCK_V2.lockDigest
  ) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Historical generation-lock v2 format, status, or digest drifted.",
    );
  }
  const body = Object.fromEntries(Object.entries(document).filter(([key]) => key !== "lockDigest"));
  const computedDigest = `sha256:${createHash("sha256")
    .update(HISTORICAL_GENERATION_LOCK_V2.digestDomain)
    .update("\0")
    .update(JSON.stringify(body))
    .digest("hex")}`;
  if (document.lockDigest !== computedDigest) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      record.path,
      "Historical generation-lock v2 self digest is invalid.",
    );
  }
}

function verifyEvidenceManifest(
  root: string,
  authority: SuccessorV3EvidenceManifest,
  snapshot: Snapshot,
): void {
  verifyFixedFile(root, authority.manifest, snapshot);
  let parsed: unknown;
  try {
    parsed = JSON.parse(readStableFile(root, authority.manifest.path, snapshot).toString("utf8"));
  } catch (error) {
    if (error instanceof SuccessorV3PredecessorError) throw error;
    fail("SUCCESSOR_V3_MANIFEST_MISMATCH", authority.manifest.path, "Manifest is not valid JSON.");
  }
  const manifest = requireRecord(parsed, authority.manifest.path);
  exactKeys(
    manifest,
    ["algorithm", "files"],
    authority.manifest.path,
    "SUCCESSOR_V3_MANIFEST_MISMATCH",
  );
  if (
    manifest.algorithm !== authority.algorithm ||
    !Array.isArray(manifest.files) ||
    manifest.files.length !== authority.memberCount
  ) {
    fail(
      "SUCCESSOR_V3_MANIFEST_MISMATCH",
      authority.manifest.path,
      "Manifest shape or member count drifted.",
    );
  }
  let previous: string | undefined;
  for (const [index, candidate] of manifest.files.entries()) {
    const pointer = `${authority.manifest.path}#/files/${index}`;
    const member = requireRecord(candidate, pointer, "SUCCESSOR_V3_MANIFEST_MISMATCH");
    exactKeys(member, ["path", "sha256", "sizeBytes"], pointer, "SUCCESSOR_V3_MANIFEST_MISMATCH");
    if (
      typeof member.path !== "string" ||
      !member.path.startsWith(authority.memberPathPrefix) ||
      typeof member.sha256 !== "string" ||
      !/^sha256:[0-9a-f]{64}$/u.test(member.sha256) ||
      typeof member.sizeBytes !== "number" ||
      !Number.isSafeInteger(member.sizeBytes) ||
      member.sizeBytes < 0 ||
      (previous !== undefined && bytewiseCompare(previous, member.path) >= 0)
    ) {
      fail("SUCCESSOR_V3_MANIFEST_MISMATCH", pointer, "Manifest member order or fields drifted.");
    }
    validatePath(member.path);
    previous = member.path;
    const bytes = readStableFile(root, member.path, snapshot);
    if (bytes.byteLength !== member.sizeBytes || "sha256:" + digest(bytes) !== member.sha256) {
      fail("SUCCESSOR_V3_MANIFEST_MISMATCH", member.path, "Manifest member bytes drifted.");
    }
    const entry = gitTreeEntry(root, SUCCESSOR_V3_BASELINE_COMMIT, member.path);
    if (entry.mode !== "100644" || entry.type !== "blob") {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        member.path,
        "Manifest member mode or object type drifted.",
      );
    }
    if (gitBlobObjectId(bytes) !== entry.object) {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        member.path,
        "Manifest member differs from the fixed Git blob.",
      );
    }
  }
}

function verifyGateCriteria(root: string, source: SuccessorV3Source, snapshot: Snapshot): void {
  const bytes = readStableFile(root, source.baseline.gateCriteria.path, snapshot);
  if (digest(bytes) !== source.baseline.gateCriteria.sha256) {
    fail(
      "SUCCESSOR_V3_FILE_MISMATCH",
      source.baseline.gateCriteria.path,
      "Gate criteria bytes drifted.",
    );
  }
  const entry = gitTreeEntry(root, SUCCESSOR_V3_BASELINE_COMMIT, source.baseline.gateCriteria.path);
  if (entry.mode !== "100644" || entry.type !== "blob" || gitBlobObjectId(bytes) !== entry.object) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      source.baseline.gateCriteria.path,
      "Gate criteria Git binding drifted.",
    );
  }
}

function verifyClosedDirectoryTrees(root: string, source: SuccessorV3Source): void {
  const prefixes = [
    "tools/generator-supply/v1/",
    "tools/generator-supply/v2/",
    "tools/contract-review-binding/v1/",
  ] as const;
  const declared = new Set<string>();
  for (const group of source.predecessorClosure.groups) {
    for (const record of group.files) declared.add(record.path);
  }
  for (const manifest of source.predecessorClosure.evidenceManifests) {
    const parsed = JSON.parse(
      gitBytes(root, [
        "cat-file",
        "blob",
        `${SUCCESSOR_V3_BASELINE_COMMIT}:${manifest.manifest.path}`,
      ]).toString("utf8"),
    ) as { files: Array<{ path: string }> };
    for (const member of parsed.files) declared.add(member.path);
  }
  for (const prefix of prefixes) {
    const actual = gitText(root, [
      "ls-tree",
      "-r",
      "--name-only",
      SUCCESSOR_V3_BASELINE_COMMIT,
      "--",
      prefix,
    ])
      .split("\n")
      .filter(Boolean);
    const expected = [...declared]
      .filter((candidate) => candidate.startsWith(prefix))
      .toSorted(bytewiseCompare);
    if (!sameStrings(actual, expected)) {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        prefix,
        "Unknown, missing, or reordered predecessor tree path.",
      );
    }
  }
}

function verifyLegacySupplyV1Lineage(root: string, value: Readonly<Record<string, unknown>>): void {
  const lineage = value as typeof LEGACY_SUPPLY_V1_LINEAGE;
  try {
    if (
      gitText(root, ["cat-file", "-t", lineage.candidateCommit]) !== "commit" ||
      gitText(root, ["cat-file", "-t", lineage.reviewCommit]) !== "commit" ||
      gitText(root, ["rev-parse", `${lineage.candidateCommit}^{tree}`]) !== lineage.candidateTree ||
      gitText(root, ["show", "-s", "--format=%P", lineage.candidateCommit]) !==
        lineage.candidateParent ||
      gitText(root, ["rev-parse", `${lineage.reviewCommit}^{tree}`]) !== lineage.reviewTree ||
      gitText(root, ["show", "-s", "--format=%P", lineage.reviewCommit]) !== lineage.reviewParent ||
      lineage.reviewParent !== lineage.candidateCommit ||
      gitText(root, [
        "ls-tree",
        "-r",
        "--name-only",
        lineage.candidateCommit,
        "--",
        lineage.reviewPath,
      ]) !== "" ||
      digest(
        gitBytes(root, [
          "diff",
          "--no-color",
          "--no-ext-diff",
          "--no-textconv",
          "--binary",
          "--no-renames",
          lineage.candidateParent,
          lineage.candidateCommit,
        ]),
      ) !== lineage.candidateDiffSha256 ||
      digest(
        gitBytes(root, ["cat-file", "blob", `${lineage.reviewCommit}:${lineage.reviewPath}`]),
      ) !== lineage.reviewSha256
    ) {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        lineage.candidateCommit,
        "Legacy supply-v1 lineage drifted.",
      );
    }
  } catch (error) {
    if (error instanceof SuccessorV3PredecessorError) throw error;
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      lineage.candidateCommit,
      "Legacy supply-v1 lineage is unavailable.",
    );
  }
}

function verifyGitChain(root: string, chain: readonly SuccessorV3GitStep[]): void {
  for (const step of chain) {
    try {
      if (
        gitText(root, ["cat-file", "-t", step.commit]) !== "commit" ||
        gitText(root, ["rev-parse", `${step.commit}^{tree}`]) !== step.tree ||
        gitText(root, ["show", "-s", "--format=%P", step.commit]) !== step.parent ||
        digest(
          gitBytes(root, [
            "diff",
            "--no-color",
            "--no-ext-diff",
            "--no-textconv",
            "--binary",
            "--no-renames",
            step.parent,
            step.commit,
          ]),
        ) !== step.diffSha256
      ) {
        fail("SUCCESSOR_V3_GIT_MISMATCH", step.commit, "Fixed successor Git identity drifted.");
      }
      const changes = gitText(root, [
        "diff-tree",
        "--no-commit-id",
        "--name-status",
        "-r",
        "--no-renames",
        step.commit,
      ])
        .split("\n")
        .filter(Boolean)
        .map((line) => line.split("\t"));
      if (
        changes.length !== step.changes.length ||
        changes.some(
          (change, index) =>
            change[0] !== step.changes[index]?.operation || change[1] !== step.changes[index]?.path,
        )
      ) {
        fail("SUCCESSOR_V3_GIT_MISMATCH", step.commit, "Fixed successor changed-path set drifted.");
      }
      for (const change of step.changes) {
        const entry = gitTreeEntry(root, step.commit, change.path);
        if (entry.mode !== change.mode || entry.type !== "blob") {
          fail("SUCCESSOR_V3_GIT_MISMATCH", change.path, "Fixed successor path mode drifted.");
        }
      }
    } catch (error) {
      if (error instanceof SuccessorV3PredecessorError) throw error;
      fail("SUCCESSOR_V3_GIT_MISMATCH", step.commit, "Fixed successor Git chain is unavailable.");
    }
  }
}

function assertGitTopLevelAndBaseline(root: string): void {
  const rootReal = realpathSync(root);
  try {
    if (
      realpathSync(gitText(rootReal, ["rev-parse", "--show-toplevel"])) !== rootReal ||
      gitText(rootReal, ["cat-file", "-t", SUCCESSOR_V3_BASELINE_COMMIT]) !== "commit" ||
      gitText(rootReal, ["rev-parse", `${SUCCESSOR_V3_BASELINE_COMMIT}^{tree}`]) !==
        SUCCESSOR_V3_BASELINE_TREE
    ) {
      fail(
        "SUCCESSOR_V3_GIT_MISMATCH",
        SUCCESSOR_V3_BASELINE_COMMIT,
        "Baseline Git object drifted.",
      );
    }
  } catch (error) {
    if (error instanceof SuccessorV3PredecessorError) throw error;
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      SUCCESSOR_V3_BASELINE_COMMIT,
      "Baseline Git object is unavailable.",
    );
  }
}

function gitTreeEntry(
  root: string,
  commit: string,
  repositoryPath: string,
): { mode: string; type: string; object: string } {
  const line = gitText(root, ["ls-tree", commit, "--", repositoryPath]);
  const match = /^(\d{6}) (\w+) ([0-9a-f]{40})\t(.+)$/u.exec(line);
  if (match === null || match[4] !== repositoryPath) {
    fail(
      "SUCCESSOR_V3_GIT_MISMATCH",
      repositoryPath,
      "Fixed Git tree entry is absent or ambiguous.",
    );
  }
  return { mode: match[1]!, type: match[2]!, object: match[3]! };
}

function readStableFile(
  root: string,
  repositoryPath: string,
  snapshot: Snapshot,
  options: Readonly<{ requireNonExecutable?: boolean }> = {},
): Buffer {
  const rootReal = realpathSync(root);
  if (rootReal !== snapshot.rootReal) {
    fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Snapshot root changed.");
  }
  const components = validatePath(repositoryPath);
  const absolute = resolve(rootReal, ...components);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(".." + sep) ||
    isAbsolute(relation)
  ) {
    fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Path escapes the repository root.");
  }
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      fail(
        "SUCCESSOR_V3_PATH_INVALID",
        repositoryPath,
        "Path must be a contained regular non-symlink file.",
      );
    }
    if (options.requireNonExecutable === true && (pathBefore.mode & 0o111n) !== 0n) {
      fail(
        "SUCCESSOR_V3_PATH_INVALID",
        repositoryPath,
        "Current core output must be a non-executable regular file.",
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
        fail("SUCCESSOR_V3_FILE_MISMATCH", repositoryPath, "Path changed before stable open.");
      }
      const bytes = readFileSync(descriptor);
      const descriptorAfter = fstatSync(descriptor, { bigint: true });
      const pathAfter = lstatSync(absolute, { bigint: true });
      if (
        descriptorAfter.dev !== descriptorBefore.dev ||
        descriptorAfter.ino !== descriptorBefore.ino ||
        descriptorAfter.mode !== descriptorBefore.mode ||
        descriptorAfter.size !== descriptorBefore.size ||
        descriptorAfter.mtimeNs !== descriptorBefore.mtimeNs ||
        descriptorAfter.ctimeNs !== descriptorBefore.ctimeNs ||
        pathAfter.dev !== descriptorAfter.dev ||
        pathAfter.ino !== descriptorAfter.ino ||
        !pathAfter.isFile() ||
        pathAfter.isSymbolicLink() ||
        realpathSync(absolute) !== absolute ||
        (options.requireNonExecutable === true && (pathAfter.mode & 0o111n) !== 0n)
      ) {
        fail("SUCCESSOR_V3_FILE_MISMATCH", repositoryPath, "Path changed during stable read.");
      }
      captureIdentity(snapshot, repositoryPath, {
        absolute,
        dev: descriptorAfter.dev,
        ino: descriptorAfter.ino,
        mode: descriptorAfter.mode,
        size: descriptorAfter.size,
        mtimeNs: descriptorAfter.mtimeNs,
        ctimeNs: descriptorAfter.ctimeNs,
      });
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof SuccessorV3PredecessorError) throw error;
    fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Path is missing or unreadable.");
  }
}

function captureIdentity(
  snapshot: Snapshot,
  repositoryPath: string,
  identity: StableIdentity,
): void {
  const prior = snapshot.identities.get(repositoryPath);
  if (prior !== undefined && !sameIdentity(prior, identity)) {
    fail(
      "SUCCESSOR_V3_FILE_MISMATCH",
      repositoryPath,
      "Repeated read observed an ABA replacement.",
    );
  }
  if (prior === undefined) snapshot.identities.set(repositoryPath, identity);
  const hook = snapshot.mutationHook;
  if (hook !== undefined && !hook.fired && hook.afterPath === repositoryPath) {
    hook.fired = true;
    hook.mutate();
  }
}

function assertSnapshotCurrent(root: string, snapshot: Snapshot): void {
  const rootReal = realpathSync(root);
  if (rootReal !== snapshot.rootReal) {
    fail("SUCCESSOR_V3_PATH_INVALID", root, "Snapshot root changed before completion.");
  }
  for (const [repositoryPath, identity] of snapshot.identities) {
    try {
      const absolute = resolve(rootReal, ...validatePath(repositoryPath));
      const metadata = lstatSync(absolute, { bigint: true });
      const current: StableIdentity = {
        absolute,
        dev: metadata.dev,
        ino: metadata.ino,
        mode: metadata.mode,
        size: metadata.size,
        mtimeNs: metadata.mtimeNs,
        ctimeNs: metadata.ctimeNs,
      };
      if (
        !metadata.isFile() ||
        metadata.isSymbolicLink() ||
        realpathSync(absolute) !== absolute ||
        !sameIdentity(identity, current)
      ) {
        fail(
          "SUCCESSOR_V3_FILE_MISMATCH",
          repositoryPath,
          "Snapshot detected an ABA or later mutation.",
        );
      }
    } catch (error) {
      if (error instanceof SuccessorV3PredecessorError) throw error;
      fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Snapshot path became unavailable.");
    }
  }
}

function sameIdentity(left: StableIdentity, right: StableIdentity): boolean {
  return (
    left.absolute === right.absolute &&
    left.dev === right.dev &&
    left.ino === right.ino &&
    left.mode === right.mode &&
    left.size === right.size &&
    left.mtimeNs === right.mtimeNs &&
    left.ctimeNs === right.ctimeNs
  );
}

function validatePath(repositoryPath: string): string[] {
  if (
    repositoryPath.length === 0 ||
    isAbsolute(repositoryPath) ||
    repositoryPath.includes("\\") ||
    repositoryPath.includes(String.fromCharCode(0))
  ) {
    fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Path is not repository-relative.");
  }
  const components = repositoryPath.split("/");
  if (components.some((component) => component === "" || component === "." || component === "..")) {
    fail("SUCCESSOR_V3_PATH_INVALID", repositoryPath, "Path contains a normalization alias.");
  }
  return components;
}

function requireRecord(
  value: unknown,
  path: string,
  code: SuccessorV3PredecessorError["code"] = "SUCCESSOR_V3_SOURCE_INVALID",
): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    fail(code, path, "Expected an object.");
  }
  return value as Record<string, unknown>;
}

function exactKeys(
  value: Record<string, unknown>,
  expected: readonly string[],
  path: string,
  code: SuccessorV3PredecessorError["code"] = "SUCCESSOR_V3_SOURCE_INVALID",
): void {
  const actual = Object.keys(value).toSorted(bytewiseCompare);
  const wanted = [...expected].toSorted(bytewiseCompare);
  if (!sameStrings(actual, wanted))
    fail(code, path, "Unknown, missing, or duplicate semantic field.");
}

function assertExactObject(value: unknown, expected: Record<string, unknown>, path: string): void {
  const record = requireRecord(value, path);
  exactKeys(record, Object.keys(expected), path);
  for (const [key, wanted] of Object.entries(expected)) {
    if (record[key] !== wanted)
      fail("SUCCESSOR_V3_SOURCE_INVALID", `${path}/${key}`, "Fixed value drifted.");
  }
}

function sameStrings(left: readonly unknown[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

function bytewiseCompare(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function digest(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function gitBlobObjectId(bytes: Uint8Array): string {
  return createHash("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
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

function fail(code: SuccessorV3PredecessorError["code"], path: string, message: string): never {
  throw new SuccessorV3PredecessorError(code, path, message);
}
