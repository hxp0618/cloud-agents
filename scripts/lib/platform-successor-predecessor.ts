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

export type ImmutableFileRecord = {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

export type ImmutableEvidenceManifestSpec = {
  readonly manifestPath: string;
  readonly manifestSha256: string;
  readonly manifestSizeBytes: number;
  readonly algorithm: string;
  readonly memberCount: number;
  readonly memberPathPrefix: string;
};

export type GeneratorSupplyV1GitLineage = {
  readonly candidateCommit: string;
  readonly candidateTree: string;
  readonly candidateParent: string;
  readonly candidateDiffSha256: string;
  readonly reviewCommit: string;
  readonly reviewTree: string;
  readonly reviewParent: string;
  readonly reviewPath: string;
  readonly reviewSha256: string;
  readonly verdict: string;
};

export class SuccessorPredecessorError extends Error {
  constructor(
    readonly code:
      | "PREDECESSOR_FILE_MISMATCH"
      | "PREDECESSOR_GIT_MISMATCH"
      | "PREDECESSOR_INVALID_PATH"
      | "PREDECESSOR_MANIFEST_MISMATCH",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "SuccessorPredecessorError";
  }
}

export const CONTRACT_CLOSURE_V1_IMMUTABLE_FILES = [
  {
    path: "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v1.json",
    sha256: "411e4b649c5b812339817b5836c25a6a2f27c9aa0e24497b7aa65da8fe2baa49",
    sizeBytes: 5589,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v1.schema.json",
    sha256: "8b87a0e24e42db87987a1dc1b4931b7ff2b8edef6bef6ccd184e0586a7bdc4af",
    sizeBytes: 4711,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-v1.schema.json",
    sha256: "107dbc21f240cd912f567ef1e0a6bfaf78d2e6171f0f4189ca9812c225630bc0",
    sizeBytes: 1548,
  },
  {
    path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json",
    sha256: "823e9356342511b611538fb669e8af99962555b153324d09c7208f3f00b51e68",
    sizeBytes: 6364,
  },
] as const satisfies readonly ImmutableFileRecord[];

export const CONTRACT_CLOSURE_V2_IMMUTABLE_FILES = [
  {
    path: "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v2.json",
    sha256: "cb0c2f9efe9f54ef5ed2eb0868ff604d8fe06b8f40bd4fdc01be07c0cb160032",
    sizeBytes: 7932,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v2.schema.json",
    sha256: "2bba53f094b1b01eabc3becb769ce00bfaee140f90ced21e21a22f352eb192ad",
    sizeBytes: 8122,
  },
  {
    path: "contracts/platform/v1alpha1/schemas/contract-closure-profile-v2.schema.json",
    sha256: "7e6e9f35c000e0620d8719a31102f6b4fd1cd7450892be0b815e5b7c25307146",
    sizeBytes: 1833,
  },
  {
    path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json",
    sha256: "5069f0f1bdca9b7b7c161cb36870c00be254acb315cafab45adcc944b19e33fe",
    sizeBytes: 8690,
  },
] as const satisfies readonly ImmutableFileRecord[];

export const GENERATOR_SUPPLY_V1_IMMUTABLE_FILES = [
  {
    path: "tools/generator-supply/v1/source.json",
    sha256: "a14e177c72afb699b47446232625ba638c68da2bc7731e213ab432244924a2f9",
    sizeBytes: 12111,
  },
  {
    path: "tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json",
    sha256: "6204f24913dccc98e80e415f00fce74e2bfa99b68df691b1669f0c00592002ab",
    sizeBytes: 8938,
  },
  {
    path: "tools/generator-supply/v1/generator-supply-profile-v1.schema.json",
    sha256: "6f51389646fdbcf8633b56495d1d128b92bec1958dbc1acb96afaf2d75ea2d64",
    sizeBytes: 2077,
  },
  {
    path: "tools/generator-supply/v1/evidence-manifest.json",
    sha256: "4e6ec3c1b89a40c6dd9ee989997c7ec28d44730eac8387e065d8cc524b973bc7",
    sizeBytes: 8186,
  },
  {
    path: "tools/generator-supply/v1/profile.json",
    sha256: "dcd9c9da7cd28a254dbeb419a388875b843033c0ca522fc603cd29b30295f93b",
    sizeBytes: 22188,
  },
  {
    path: "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
    sha256: "86ec054debf15de71481d6f9ab965ca5c8f24a4f5a98f9e5e155e24df261cd47",
    sizeBytes: 8426,
  },
] as const satisfies readonly ImmutableFileRecord[];

export const GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST = {
  manifestPath: "tools/generator-supply/v1/evidence-manifest.json",
  manifestSha256: "4e6ec3c1b89a40c6dd9ee989997c7ec28d44730eac8387e065d8cc524b973bc7",
  manifestSizeBytes: 8186,
  algorithm: "sorted-path-nul-sha256-nul-size-v1",
  memberCount: 39,
  memberPathPrefix: "tools/generator-supply/v1/evidence/",
} as const satisfies ImmutableEvidenceManifestSpec;

export const GENERATOR_SUPPLY_V1_GIT_LINEAGE = {
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
} as const satisfies GeneratorSupplyV1GitLineage;

export const GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES = {
  sourceDigest: "sha256:8c2f462e30baefdf420179b66399461a22a0de71efcefca99e1ff3134bd62b3c",
  artifactSetDigest: "sha256:f307e4c73f56e62c5d38b928acff5db284cd5a1706bb76fa7ba55b1437faa0c5",
  evidenceManifestDigest: "sha256:59e7dfe0d85d7fd2cb9ad069037019c484a4c8039c897e5d5cabf45f517f64ab",
  profileDigest: "sha256:b1201cd3d22398fa808a05190ef4ce49422db665277e8cf8c936938cb5cd741c",
  registryDigest: "sha256:86452d655cd05a73211e52c28107e93d38a244026648e28a8369cefd4e4eed9c",
} as const;

type GeneratorSupplyV1ProfileIdentities = {
  readonly sourceDigest: string;
  readonly artifactSetDigest: string;
  readonly evidenceManifestDigest: string;
  readonly profileDigest: string;
  readonly registryDigest: string;
};

type StablePredecessorIdentity = Readonly<{
  rootReal: string;
  path: string;
  absolute: string;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

type PredecessorSnapshot = {
  readonly rootReal: string;
  readonly identities: Map<string, StablePredecessorIdentity>;
  readonly mutationHook?: {
    readonly afterPath: string;
    readonly mutate: () => void;
    fired: boolean;
  };
};

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

export function assertContractClosureV1Immutable(root: string): void {
  assertImmutableFileMap(root, CONTRACT_CLOSURE_V1_IMMUTABLE_FILES, "contract closure v1");
}

export function assertContractClosureV2Immutable(root: string): void {
  assertImmutableFileMap(root, CONTRACT_CLOSURE_V2_IMMUTABLE_FILES, "contract closure v2");
}

export function assertGeneratorSupplyV1PredecessorImmutable(root: string): void {
  assertGeneratorSupplyV1PredecessorImmutableInternal(root);
}

export function assertGeneratorSupplyV1SnapshotMutationForTest(
  root: string,
  afterPath: string,
  mutate: () => void,
): void {
  assertGeneratorSupplyV1PredecessorImmutableInternal(root, { afterPath, mutate, fired: false });
}

function assertGeneratorSupplyV1PredecessorImmutableInternal(
  root: string,
  mutationHook?: PredecessorSnapshot["mutationHook"],
): void {
  const snapshot: PredecessorSnapshot = {
    rootReal: realpathSync(root),
    identities: new Map(),
    mutationHook,
  };
  assertImmutableFileMapInternal(
    root,
    GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
    "generator supply v1",
    snapshot,
  );
  assertImmutableEvidenceManifestInternal(root, GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST, snapshot);
  assertGeneratorSupplyV1SemanticBindingsInternal(
    root,
    GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
    GENERATOR_SUPPLY_V1_GIT_LINEAGE.verdict,
    snapshot,
  );
  if (mutationHook !== undefined && !mutationHook.fired) {
    fail(
      "PREDECESSOR_FILE_MISMATCH",
      mutationHook.afterPath,
      "Predecessor snapshot mutation test path was not captured.",
    );
  }
  assertPredecessorSnapshotCurrent(root, snapshot);
}

export function assertSuccessorPredecessorsImmutable(root: string): void {
  assertContractClosureV1Immutable(root);
  assertContractClosureV2Immutable(root);
  assertGeneratorSupplyV1PredecessorImmutable(root);
}

export function assertImmutableFileMap(
  root: string,
  files: readonly ImmutableFileRecord[],
  label = "immutable predecessor",
): void {
  assertImmutableFileMapInternal(root, files, label);
}

function assertImmutableFileMapInternal(
  root: string,
  files: readonly ImmutableFileRecord[],
  label: string,
  snapshot?: PredecessorSnapshot,
): void {
  const seen = new Set<string>();
  for (const file of files) {
    if (seen.has(file.path)) {
      fail("PREDECESSOR_FILE_MISMATCH", file.path, label + " file map contains a duplicate path.");
    }
    seen.add(file.path);
    const bytes = readContainedRegularFile(root, file.path, undefined, snapshot);
    if (bytes.byteLength !== file.sizeBytes) {
      fail(
        "PREDECESSOR_FILE_MISMATCH",
        file.path,
        label +
          " immutable file size drifted: expected " +
          file.sizeBytes +
          ", got " +
          bytes.byteLength +
          ".",
      );
    }
    const actualSha256 = sha256(bytes);
    if (actualSha256 !== file.sha256) {
      fail(
        "PREDECESSOR_FILE_MISMATCH",
        file.path,
        label +
          " immutable file digest drifted: expected " +
          file.sha256 +
          ", got " +
          actualSha256 +
          ".",
      );
    }
  }
}

export function assertImmutableEvidenceManifest(
  root: string,
  spec: ImmutableEvidenceManifestSpec,
): void {
  assertImmutableEvidenceManifestInternal(root, spec);
}

function assertImmutableEvidenceManifestInternal(
  root: string,
  spec: ImmutableEvidenceManifestSpec,
  snapshot?: PredecessorSnapshot,
): void {
  assertImmutableFileMapInternal(
    root,
    [
      {
        path: spec.manifestPath,
        sha256: spec.manifestSha256,
        sizeBytes: spec.manifestSizeBytes,
      },
    ],
    "evidence manifest",
    snapshot,
  );
  const bytes = readContainedRegularFile(root, spec.manifestPath, undefined, snapshot);
  let parsed: unknown;
  try {
    parsed = JSON.parse(bytes.toString("utf8"));
  } catch {
    fail(
      "PREDECESSOR_MANIFEST_MISMATCH",
      spec.manifestPath,
      "Immutable evidence manifest is not valid JSON.",
    );
  }
  if (!isRecord(parsed)) {
    fail(
      "PREDECESSOR_MANIFEST_MISMATCH",
      spec.manifestPath,
      "Immutable evidence manifest must be an object.",
    );
  }
  assertExactKeys(parsed, ["algorithm", "files"], spec.manifestPath);
  if (parsed.algorithm !== spec.algorithm || !Array.isArray(parsed.files)) {
    fail(
      "PREDECESSOR_MANIFEST_MISMATCH",
      spec.manifestPath,
      "Immutable evidence manifest algorithm or files collection drifted.",
    );
  }
  if (parsed.files.length !== spec.memberCount) {
    fail(
      "PREDECESSOR_MANIFEST_MISMATCH",
      spec.manifestPath,
      "Immutable evidence manifest must contain exactly " + spec.memberCount + " members.",
    );
  }
  let previousPath: string | undefined;
  for (const [index, candidate] of parsed.files.entries()) {
    const pointer = spec.manifestPath + "#/files/" + index;
    if (!isRecord(candidate)) {
      fail(
        "PREDECESSOR_MANIFEST_MISMATCH",
        pointer,
        "Immutable evidence manifest member must be an object.",
      );
    }
    assertExactKeys(candidate, ["path", "sha256", "sizeBytes"], pointer);
    if (
      typeof candidate.path !== "string" ||
      typeof candidate.sha256 !== "string" ||
      !/^sha256:[0-9a-f]{64}$/u.test(candidate.sha256) ||
      typeof candidate.sizeBytes !== "number" ||
      !Number.isSafeInteger(candidate.sizeBytes) ||
      candidate.sizeBytes < 0
    ) {
      fail(
        "PREDECESSOR_MANIFEST_MISMATCH",
        pointer,
        "Immutable evidence manifest member fields are invalid.",
      );
    }
    if (!candidate.path.startsWith(spec.memberPathPrefix)) {
      fail(
        "PREDECESSOR_MANIFEST_MISMATCH",
        pointer + "/path",
        "Immutable evidence member is outside the authorized path prefix.",
      );
    }
    if (previousPath !== undefined && bytewiseCompare(previousPath, candidate.path) >= 0) {
      fail(
        "PREDECESSOR_MANIFEST_MISMATCH",
        pointer + "/path",
        "Immutable evidence member paths must be unique and UTF-8 bytewise sorted.",
      );
    }
    previousPath = candidate.path;
    const memberBytes = readContainedRegularFile(root, candidate.path, undefined, snapshot);
    if (
      memberBytes.byteLength !== candidate.sizeBytes ||
      "sha256:" + sha256(memberBytes) !== candidate.sha256
    ) {
      fail(
        "PREDECESSOR_MANIFEST_MISMATCH",
        candidate.path,
        "Immutable evidence member bytes do not match the fixed manifest.",
      );
    }
  }
}

export function assertGeneratorSupplyV1GitLineageCurrent(root: string): void {
  assertGeneratorSupplyV1GitLineage(root, GENERATOR_SUPPLY_V1_GIT_LINEAGE);
}

export function assertGeneratorSupplyV1GitLineageForTest(
  root: string,
  lineage: GeneratorSupplyV1GitLineage,
): void {
  assertGeneratorSupplyV1GitLineage(root, lineage);
}

function assertGeneratorSupplyV1GitLineage(
  root: string,
  lineage: GeneratorSupplyV1GitLineage,
): void {
  const repositoryRoot = realpathSync(root);
  try {
    const topLevel = realpathSync(gitText(repositoryRoot, ["rev-parse", "--show-toplevel"]));
    const candidateType = gitText(repositoryRoot, ["cat-file", "-t", lineage.candidateCommit]);
    const reviewType = gitText(repositoryRoot, ["cat-file", "-t", lineage.reviewCommit]);
    const candidateTree = gitText(repositoryRoot, [
      "rev-parse",
      lineage.candidateCommit + "^{tree}",
    ]);
    const candidateParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      lineage.candidateCommit,
    ]);
    const reviewTree = gitText(repositoryRoot, ["rev-parse", lineage.reviewCommit + "^{tree}"]);
    const reviewParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      lineage.reviewCommit,
    ]);
    const reviewPathBefore = gitText(repositoryRoot, [
      "ls-tree",
      "-r",
      "--name-only",
      lineage.candidateCommit,
      "--",
      lineage.reviewPath,
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
      lineage.reviewCommit + ":" + lineage.reviewPath,
    ]);
    if (
      topLevel !== repositoryRoot ||
      candidateType !== "commit" ||
      reviewType !== "commit" ||
      candidateTree !== lineage.candidateTree ||
      candidateParents !== lineage.candidateParent ||
      reviewTree !== lineage.reviewTree ||
      reviewParents !== lineage.reviewParent ||
      lineage.reviewParent !== lineage.candidateCommit ||
      lineage.reviewCommit === lineage.candidateCommit ||
      reviewPathBefore !== "" ||
      sha256(diff) !== lineage.candidateDiffSha256 ||
      sha256(reviewBytes) !== lineage.reviewSha256
    ) {
      fail(
        "PREDECESSOR_GIT_MISMATCH",
        lineage.candidateCommit,
        "Generator supply v1 fixed Git lineage or review binding drifted.",
      );
    }
  } catch (error) {
    if (error instanceof SuccessorPredecessorError) throw error;
    fail(
      "PREDECESSOR_GIT_MISMATCH",
      lineage.candidateCommit,
      "Generator supply v1 fixed Git lineage is unavailable or invalid.",
    );
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

export function assertGeneratorSupplyV1SemanticBindingsForTest(
  root: string,
  identities: GeneratorSupplyV1ProfileIdentities,
  expectedVerdict: string,
): void {
  assertGeneratorSupplyV1SemanticBindingsInternal(root, identities, expectedVerdict);
}

function assertGeneratorSupplyV1SemanticBindingsInternal(
  root: string,
  identities: GeneratorSupplyV1ProfileIdentities,
  expectedVerdict: string,
  snapshot?: PredecessorSnapshot,
): void {
  let registry: unknown;
  try {
    registry = JSON.parse(
      readContainedRegularFile(
        root,
        "tools/generator-supply/v1/profile.json",
        undefined,
        snapshot,
      ).toString("utf8"),
    );
  } catch (error) {
    if (error instanceof SuccessorPredecessorError) throw error;
    fail(
      "PREDECESSOR_FILE_MISMATCH",
      "tools/generator-supply/v1/profile.json",
      "Generator supply v1 profile is not valid JSON.",
    );
  }
  if (!isRecord(registry) || !isRecord(registry.profile) || !isRecord(registry.profile.spec)) {
    fail(
      "PREDECESSOR_FILE_MISMATCH",
      "tools/generator-supply/v1/profile.json",
      "Generator supply v1 profile identity structure drifted.",
    );
  }
  if (
    registry.formatVersion !== "cloud-agents-generator-supply-profile-registry/v1" ||
    registry.registryId !== "cloud-agents/generator-supply-profile" ||
    registry.sourceDigest !== identities.sourceDigest ||
    registry.artifactSetDigest !== identities.artifactSetDigest ||
    registry.evidenceManifestDigest !== identities.evidenceManifestDigest ||
    registry.profile.profileDigest !== identities.profileDigest ||
    registry.registryDigest !== identities.registryDigest ||
    registry.profile.spec.profileId !== "cloud-agents/generator-supply-profile/v1" ||
    registry.profile.spec.status !== "REPLAY_VERIFIED_REVIEW_PENDING" ||
    registry.profile.spec.notGateClosure !== true
  ) {
    fail(
      "PREDECESSOR_FILE_MISMATCH",
      "tools/generator-supply/v1/profile.json",
      "Generator supply v1 fixed profile identities or non-Gate semantics drifted.",
    );
  }
  const reviewPath = GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewPath;
  const review = readContainedRegularFile(root, reviewPath, undefined, snapshot).toString("utf8");
  const codeQuote = String.fromCharCode(96);
  const normalizedVerdict =
    review.includes("## Verdict\n\n" + codeQuote + "APPROVE" + codeQuote) &&
    /^\| P0\s+\|\s+0 \|$/mu.test(review) &&
    /^\| P1\s+\|\s+0 \|$/mu.test(review) &&
    /^\| P2\s+\|\s+0 \|$/mu.test(review)
      ? "APPROVE_P0_0_P1_0_P2_0"
      : "UNRECOGNIZED";
  if (normalizedVerdict !== expectedVerdict) {
    fail(
      "PREDECESSOR_FILE_MISMATCH",
      reviewPath,
      "Generator supply v1 normalized independent-review verdict drifted.",
    );
  }
}

export function assertStablePredecessorReadMutationForTest(
  root: string,
  relativePath: string,
  mutateAfterRead: () => void,
): void {
  readContainedRegularFile(root, relativePath, mutateAfterRead);
}

function readContainedRegularFile(
  root: string,
  relativePath: string,
  mutateAfterRead?: () => void,
  snapshot?: PredecessorSnapshot,
): Buffer {
  const segments = validateRelativePath(relativePath);
  const rootReal = realpathSync(root);
  if (snapshot !== undefined && rootReal !== snapshot.rootReal) {
    fail(
      "PREDECESSOR_INVALID_PATH",
      relativePath,
      "Immutable predecessor root changed during the shared snapshot.",
    );
  }
  const absolute = resolve(rootReal, ...segments);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(".." + sep) ||
    isAbsolute(relation)
  ) {
    fail("PREDECESSOR_INVALID_PATH", relativePath, "Immutable predecessor path escapes its root.");
  }
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      fail(
        "PREDECESSOR_INVALID_PATH",
        relativePath,
        "Immutable predecessor paths must resolve to regular non-symlink files.",
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
        fail(
          "PREDECESSOR_FILE_MISMATCH",
          relativePath,
          "Immutable predecessor changed before it could be opened.",
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
        fail(
          "PREDECESSOR_FILE_MISMATCH",
          relativePath,
          "Immutable predecessor changed while it was being read.",
        );
      }
      if (snapshot !== undefined) {
        capturePredecessorIdentity(snapshot, {
          rootReal,
          path: relativePath,
          absolute,
          dev: descriptorAfter.dev,
          ino: descriptorAfter.ino,
          size: descriptorAfter.size,
          mtimeNs: descriptorAfter.mtimeNs,
          ctimeNs: descriptorAfter.ctimeNs,
        });
      }
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof SuccessorPredecessorError) throw error;
    fail(
      "PREDECESSOR_INVALID_PATH",
      relativePath,
      "Immutable predecessor path is missing or unreadable.",
    );
  }
}

function capturePredecessorIdentity(
  snapshot: PredecessorSnapshot,
  identity: StablePredecessorIdentity,
): void {
  const existing = snapshot.identities.get(identity.path);
  if (existing !== undefined) {
    if (!samePredecessorIdentity(existing, identity)) {
      fail(
        "PREDECESSOR_FILE_MISMATCH",
        identity.path,
        "Repeated immutable predecessor read observed a different file identity.",
      );
    }
    return;
  }
  snapshot.identities.set(identity.path, identity);
  const hook = snapshot.mutationHook;
  if (hook !== undefined && !hook.fired && hook.afterPath === identity.path) {
    hook.fired = true;
    hook.mutate();
  }
}

function assertPredecessorSnapshotCurrent(root: string, snapshot: PredecessorSnapshot): void {
  let rootReal: string;
  try {
    rootReal = realpathSync(root);
  } catch {
    fail(
      "PREDECESSOR_INVALID_PATH",
      root,
      "Immutable predecessor root is unavailable at snapshot completion.",
    );
  }
  if (rootReal !== snapshot.rootReal) {
    fail(
      "PREDECESSOR_INVALID_PATH",
      root,
      "Immutable predecessor root changed before snapshot completion.",
    );
  }
  for (const identity of snapshot.identities.values()) {
    try {
      const segments = validateRelativePath(identity.path);
      let current = rootReal;
      for (const [index, segment] of segments.entries()) {
        current = resolve(current, segment);
        const stat = lstatSync(current, { bigint: true });
        if (
          stat.isSymbolicLink() ||
          (index < segments.length - 1 ? !stat.isDirectory() : !stat.isFile())
        ) {
          fail(
            "PREDECESSOR_INVALID_PATH",
            identity.path,
            "Immutable predecessor path topology changed after capture.",
          );
        }
      }
      if (current !== identity.absolute || realpathSync(current) !== identity.absolute) {
        fail(
          "PREDECESSOR_INVALID_PATH",
          identity.path,
          "Immutable predecessor path resolved to a different location after capture.",
        );
      }
      const after = lstatSync(current, { bigint: true });
      const currentIdentity: StablePredecessorIdentity = {
        rootReal,
        path: identity.path,
        absolute: current,
        dev: after.dev,
        ino: after.ino,
        size: after.size,
        mtimeNs: after.mtimeNs,
        ctimeNs: after.ctimeNs,
      };
      if (!samePredecessorIdentity(identity, currentIdentity)) {
        fail(
          "PREDECESSOR_FILE_MISMATCH",
          identity.path,
          "Immutable predecessor changed after its shared snapshot capture.",
        );
      }
    } catch (error) {
      if (error instanceof SuccessorPredecessorError) throw error;
      fail(
        "PREDECESSOR_INVALID_PATH",
        identity.path,
        "Immutable predecessor path is unavailable at snapshot completion.",
      );
    }
  }
}

function samePredecessorIdentity(
  left: StablePredecessorIdentity,
  right: StablePredecessorIdentity,
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

function validateRelativePath(path: string): string[] {
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes(String.fromCharCode(0)) ||
    path.includes("\\")
  ) {
    fail(
      "PREDECESSOR_INVALID_PATH",
      path,
      "Immutable predecessor path is not repository-relative.",
    );
  }
  const segments = path.split("/");
  if (segments.some((segment) => segment.length === 0 || segment === "." || segment === "..")) {
    fail("PREDECESSOR_INVALID_PATH", path, "Immutable predecessor path is not canonical.");
  }
  return segments;
}

function assertExactKeys(
  record: Record<string, unknown>,
  expected: readonly string[],
  path: string,
): void {
  const actual = Object.keys(record).toSorted();
  const wanted = [...expected].toSorted();
  if (actual.length !== wanted.length || actual.some((key, index) => key !== wanted[index])) {
    fail("PREDECESSOR_MANIFEST_MISMATCH", path, "Immutable evidence manifest object keys drifted.");
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function bytewiseCompare(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function sha256(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function fail(code: SuccessorPredecessorError["code"], path: string, message: string): never {
  throw new SuccessorPredecessorError(code, path, message);
}
