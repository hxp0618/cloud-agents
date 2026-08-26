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

import Ajv2020 from "ajv/dist/2020.js";

export const EC2_SOURCE_PATH = "tools/g-contract-external-consumer/v2/source.json";
export const EC2_SOURCE_SCHEMA_PATH = "tools/g-contract-external-consumer/v2/source.schema.json";
export const EC2_PROFILE_PATH = "tools/g-contract-external-consumer/v2/profile.json";
export const EC2_PROFILE_SCHEMA_PATH = "tools/g-contract-external-consumer/v2/profile.schema.json";
export const EC2_REVIEW_PATH =
  "docs/plan/p1/g-contract-external-consumer-v2-independent-review-20260826.md";

const EC2_SOURCE_FORMAT = "cloud-agents-g-contract-external-consumer-source/v2";
const EC2_REGISTRY_ID = "cloud-agents/g-contract-external-consumer";
const EC2_PROFILE_ID = "g-contract-external-consumer/v2";
const EC2_DECISION_ID = "D-053-EC-2";
const SHA256 = /^sha256:[0-9a-f]{64}$/u;
const SHA1 = /^[0-9a-f]{40}$/u;

/** The declaration order is frozen; generated manifests sort by UTF-8 bytes. */
export const EC2_SEMANTIC_INPUT_PATHS = [
  "package.json",
  "contracts/generated/proto/cloud-agents-v1alpha1.binpb",
  "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb",
  "contracts/generated/proto/manifest.json",
  "contracts/proto-generation.profile.json",
  "docs/plan/cloud-agents-platform/05-gates-and-acceptance.md",
  "scripts/generate-platform-g-contract-external-consumer.ts",
  "scripts/lib/platform-g-contract-external-consumer.ts",
  "scripts/lib/platform-g-contract-external-consumer.test.ts",
  "tools/g-contract-external-consumer/v1/source.schema.json",
  "tools/g-contract-external-consumer/v1/profile.schema.json",
  "sdk/typescript/package.json",
  "sdk/typescript/generated-manifest.json",
  "sdk/typescript/proto-generated-manifest.json",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/generated-manifest.json",
  "sdk/go/proto-generated-manifest.json",
] as const;

/** Late-bound files are exact paths; no wildcard is permitted. */
export const EC2_EXCLUSION_PATHS = [
  "tools/g-contract-external-consumer/v2/evidence/replay/projection.tar",
  "tools/g-contract-external-consumer/v2/evidence/replay/projection.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/projection.member-manifest.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/darwin-arm64-a.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/darwin-arm64-b.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/darwin-arm64-isolation.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/linux-amd64-a.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/linux-amd64-b.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/linux-amd64-isolation.json",
  "tools/g-contract-external-consumer/v2/evidence/replay/replay.json",
  EC2_PROFILE_PATH,
  EC2_REVIEW_PATH,
  "docs/plan/p1/g-contract-external-consumer-v2-replay-review-20260826.md",
] as const;

/** Files in the authority base. source.json is deliberately added by C. */
export const EC2_AUTHORITY_FILE_PATHS = [
  "docs/plan/p1/g-contract-current-source-external-consumer-successor-v2-authorization-20260826.md",
  EC2_SOURCE_SCHEMA_PATH,
  EC2_PROFILE_SCHEMA_PATH,
  "tools/g-contract-external-consumer/v2/projection-receipt.schema.json",
  "tools/g-contract-external-consumer/v2/native-replay-receipt.schema.json",
  "tools/g-contract-external-consumer/v2/replay-summary-receipt.schema.json",
  "tools/g-contract-external-consumer/v2/review.schema.json",
  "scripts/lib/platform-g-contract-external-consumer-v2.ts",
  "scripts/generate-platform-g-contract-external-consumer-v2.ts",
  "scripts/lib/platform-g-contract-external-consumer-v2.test.ts",
  "docs/plan/p1/g-contract-external-consumer-successor-v2-authority-implementation-20260826.md",
] as const;

const EC2_PREDECESSOR_CANDIDATE = {
  commit: "f8d44568b0f64b31f466dbc47e0a17b15b96e659",
  tree: "50662a40d175aa18f3d5eaf6f1c60d0a58c816db",
  parent: "f3a058291ba6fbae53bc8dc96c695944426b2fb4",
} as const;

const EC2_PREDECESSOR_REVIEW = {
  commit: "9f9815e72cf108972a6fd12627cdeaad8cb71449",
  tree: "77bdf44bdb1c6e1ee2893ca13d54eb8d90967342",
  parent: EC2_PREDECESSOR_CANDIDATE.commit,
  path: "docs/plan/p1/g-contract-external-consumer-successor-independent-review-20260826.md",
  gitBlob: "69d7c4e2566dfcc79b70b5b10d971516ea7d785a",
  sha256: "sha256:585c175bdeedb1d396ffef52a58c17b88a344a103622029a5e83defe6b9d66b5",
  sizeBytes: 3724,
  mode: "100644",
} as const;

const AUTHORIZATION_COMMIT = "7c52dcd746637a63e0544690ac7435dedaaf628b";

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

const FIXED_GIT_ARGS = [
  "-c",
  "core.attributesFile=/dev/null",
  "-c",
  "core.abbrev=40",
  "-c",
  "diff.external=",
  "-c",
  "diff.renames=false",
] as const;

export type Ec2CheckResult = Readonly<{
  source: Record<string, unknown>;
  candidateCommit: string;
  candidateTree: string;
  candidateParent: string;
  sourceGitBlob: string;
  sourceSha256: string;
}>;

export class ExternalConsumerV2Error extends Error {
  constructor(
    readonly code: string,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ExternalConsumerV2Error";
  }
}

/** Read-only source authority check; no profile, receipt, archive, or network write. */
export function checkExternalConsumerV2Source(root: string): Ec2CheckResult {
  const repositoryRoot = realpathSync(resolve(root));
  assertGitRoot(repositoryRoot);
  const sourceAbsolute = resolveContained(repositoryRoot, EC2_SOURCE_PATH);
  const sourceBytes = readStableFile(sourceAbsolute);
  assertCanonicalJson(sourceBytes, EC2_SOURCE_PATH);
  const source = parseJson(sourceBytes, EC2_SOURCE_PATH);
  validateSchema(repositoryRoot, source, EC2_SOURCE_SCHEMA_PATH);
  assertSourceIdentity(source);

  const authorityParent = record(source.authorityParent, "/authorityParent");
  const authorityParentCommit = stringField(authorityParent, "commit", "/authorityParent");
  const authorityParentTree = stringField(authorityParent, "tree", "/authorityParent");
  const authorityParentParent = stringField(authorityParent, "parent", "/authorityParent");
  assertSha1(authorityParentCommit, "/authorityParent/commit");
  assertSha1(authorityParentTree, "/authorityParent/tree");
  assertSha1(authorityParentParent, "/authorityParent/parent");
  if (gitTree(repositoryRoot, authorityParentCommit) !== authorityParentTree) {
    throw error(
      "EC2_AUTHORITY_PARENT_DRIFT",
      "/authorityParent/tree",
      "Authority parent tree drifted.",
    );
  }
  if (gitParents(repositoryRoot, authorityParentCommit) !== authorityParentParent) {
    throw error(
      "EC2_AUTHORITY_PARENT_DRIFT",
      "/authorityParent/parent",
      "Authority parent topology drifted.",
    );
  }
  if (authorityParentParent !== AUTHORIZATION_COMMIT) {
    throw error(
      "EC2_AUTHORITY_PARENT_DRIFT",
      "/authorityParent/parent",
      "Authority base must be a direct child of the approved v2 authorization.",
    );
  }

  const candidate = discoverCandidate(repositoryRoot, authorityParentCommit);
  assertAuthorityBaseDiff(repositoryRoot, authorityParentCommit, candidate.commit);
  assertCandidateDiff(repositoryRoot, authorityParentCommit, candidate.commit);
  assertSourceBlob(repositoryRoot, sourceBytes, candidate.commit);

  const authorization = record(source.authorization, "/authorization");
  assertFileRecord(repositoryRoot, authorization, authorityParentCommit);
  const authorityFiles = arrayField(source, "authorityFiles", "/authorityFiles");
  for (const [index, file] of authorityFiles.entries()) {
    assertFileRecord(
      repositoryRoot,
      record(file, `/authorityFiles/${index}`),
      authorityParentCommit,
    );
  }

  assertPredecessorFence(repositoryRoot, source);
  assertSemanticInputs(repositoryRoot, source);
  assertProjectionContract(source);
  assertReceiptContract(source);
  assertTrackedTreeSafe(repositoryRoot, candidate.commit, source);
  assertNoUntrackedNonignoredFiles(repositoryRoot);

  return {
    source,
    candidateCommit: candidate.commit,
    candidateTree: candidate.tree,
    candidateParent: authorityParentCommit,
    sourceGitBlob: gitBlobAtCommit(repositoryRoot, candidate.commit, EC2_SOURCE_PATH),
    sourceSha256: sha256(sourceBytes),
  };
}

/** Profile generation is intentionally forbidden until the replay stages are complete. */
export function assertExternalConsumerV2ProfileAbsent(root: string): void {
  const profile = resolveContained(root, EC2_PROFILE_PATH, true);
  try {
    lstatSync(profile);
  } catch (cause) {
    if (isMissing(cause)) return;
    throw cause;
  }
  throw error(
    "EC2_PROFILE_FORBIDDEN",
    `/${EC2_PROFILE_PATH}`,
    "Authority-only slice must not create a profile.",
  );
}

function assertSourceIdentity(source: Record<string, unknown>): void {
  if (
    source.$schema !==
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v2/source.schema.json" ||
    source.formatVersion !== EC2_SOURCE_FORMAT ||
    source.registryId !== EC2_REGISTRY_ID ||
    source.profileId !== EC2_PROFILE_ID ||
    source.decisionId !== EC2_DECISION_ID ||
    source.status !== "AUTHORITY_FROZEN_REVIEW_PENDING"
  ) {
    throw error(
      "EC2_SOURCE_IDENTITY_DRIFT",
      "/",
      "D-053-EC-2 source identity or initial state drifted.",
    );
  }
}

function assertPredecessorFence(root: string, source: Record<string, unknown>): void {
  const fence = record(source.lineageFence, "/lineageFence");
  if (
    fence.predecessorDecisionId !== "D-053-EC-1" ||
    fence.predecessorProfileId !== "g-contract-external-consumer/v1"
  ) {
    throw error("EC2_LINEAGE_DRIFT", "/lineageFence", "EC-1 predecessor identity drifted.");
  }
  const predecessorCandidate = record(
    fence.predecessorCandidate,
    "/lineageFence/predecessorCandidate",
  );
  const predecessorReview = record(fence.predecessorReview, "/lineageFence/predecessorReview");
  assertExactObject(
    predecessorCandidate,
    EC2_PREDECESSOR_CANDIDATE,
    "/lineageFence/predecessorCandidate",
  );
  assertExactObject(predecessorReview, EC2_PREDECESSOR_REVIEW, "/lineageFence/predecessorReview");
  if (
    gitTree(root, EC2_PREDECESSOR_CANDIDATE.commit) !== EC2_PREDECESSOR_CANDIDATE.tree ||
    gitParents(root, EC2_PREDECESSOR_CANDIDATE.commit) !== EC2_PREDECESSOR_CANDIDATE.parent ||
    gitTree(root, EC2_PREDECESSOR_REVIEW.commit) !== EC2_PREDECESSOR_REVIEW.tree ||
    gitParents(root, EC2_PREDECESSOR_REVIEW.commit) !== EC2_PREDECESSOR_REVIEW.parent
  ) {
    throw error("EC2_LINEAGE_DRIFT", "/lineageFence", "EC-1 candidate/review topology drifted.");
  }
  assertSingleAddedPath(
    root,
    EC2_PREDECESSOR_REVIEW.parent,
    EC2_PREDECESSOR_REVIEW.commit,
    EC2_PREDECESSOR_REVIEW.path,
  );

  const files = arrayField(fence, "immutableFiles", "/lineageFence/immutableFiles");
  const expectedPaths = [
    "tools/g-contract-external-consumer/v1/source.json",
    "tools/g-contract-external-consumer/v1/profile.json",
    "tools/g-contract-external-consumer/v1/evidence/consumer.json",
    "tools/g-contract-external-consumer/v1/source.schema.json",
    "tools/g-contract-external-consumer/v1/profile.schema.json",
    EC2_PREDECESSOR_REVIEW.path,
    "docs/plan/p1/g-contract-current-source-external-consumer-successor-v2-authorization-20260826.md",
  ];
  assertPathOrder(files, expectedPaths, "/lineageFence/immutableFiles");
  for (const [index, value] of files.entries()) {
    const file = record(value, `/lineageFence/immutableFiles/${index}`);
    const commit =
      file.path === EC2_PREDECESSOR_REVIEW.path
        ? EC2_PREDECESSOR_REVIEW.commit
        : file.path === expectedPaths[6]
          ? authorityParentForAuth(source)
          : EC2_PREDECESSOR_CANDIDATE.commit;
    assertFileRecord(root, file, commit);
  }
}

function authorityParentForAuth(source: Record<string, unknown>): string {
  return stringField(
    record(source.authorityParent, "/authorityParent"),
    "commit",
    "/authorityParent",
  );
}

function assertSemanticInputs(root: string, source: Record<string, unknown>): void {
  const inputs = arrayField(source, "semanticInputs", "/semanticInputs");
  assertPathOrder(inputs, [...EC2_SEMANTIC_INPUT_PATHS], "/semanticInputs");
  const manifest = record(source.inputManifest, "/inputManifest");
  if (
    manifest.bindingCommit !== EC2_PREDECESSOR_CANDIDATE.commit ||
    manifest.algorithm !== "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1" ||
    manifest.ordering !== "UTF8_BYTE_LEXICOGRAPHIC" ||
    manifest.framing !== "NUL" ||
    manifest.duplicatePaths !== "FORBIDDEN" ||
    manifest.declarationOrder !== "FROZEN" ||
    !Array.isArray(manifest.fields) ||
    JSON.stringify(manifest.fields) !== JSON.stringify(["path", "mode", "sizeBytes", "sha256"])
  ) {
    throw error(
      "EC2_INPUT_MANIFEST_DRIFT",
      "/inputManifest",
      "Input manifest algorithm or binding drifted.",
    );
  }
  for (const [index, value] of inputs.entries()) {
    assertFileRecord(
      root,
      record(value, `/semanticInputs/${index}`),
      EC2_PREDECESSOR_CANDIDATE.commit,
    );
  }
}

function assertProjectionContract(source: Record<string, unknown>): void {
  if (source.projectionScope !== "FULL_TRACKED_CANDIDATE_TREE_MINUS_EXACT_EXCLUSIONS") {
    throw error("EC2_PROJECTION_SCOPE_DRIFT", "/projectionScope", "Projection scope drifted.");
  }
  const exclusions = record(source.exclusions, "/exclusions");
  const paths = arrayField(exclusions, "paths", "/exclusions/paths");
  assertPathOrder(paths, [...EC2_EXCLUSION_PATHS], "/exclusions/paths");
  if (
    !Array.isArray(exclusions.patterns) ||
    exclusions.patterns.length !== 0 ||
    exclusions.pathMatching !== "EXACT_UTF8_BYTE_PATH" ||
    exclusions.duplicatePaths !== "FORBIDDEN" ||
    JSON.stringify(exclusions.rejectIfPresent) !==
      JSON.stringify([
        ".git",
        "node_modules",
        ".idea",
        "migration.test",
        "untracked",
        "special",
        "symlink",
        "submodule",
      ])
  ) {
    throw error(
      "EC2_EXCLUSION_DRIFT",
      "/exclusions",
      "Projection exclusions are not the exact fail-closed set.",
    );
  }
  const projection = record(source.projection, "/projection");
  if (
    projection.mode !== "VERSIONED_BOUNDED_PROJECTION" ||
    projection.archiveFormat !== "ustar" ||
    projection.compression !== "none" ||
    projection.pathOrdering !== "UTF8_BYTE_LEXICOGRAPHIC" ||
    projection.memberManifestAlgorithm !==
      "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1" ||
    projection.regularFileManifestAlgorithm !==
      "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1" ||
    projection.selection !== "GIT_TRACKED_CANDIDATE_TREE_REGULAR_FILES_AND_DIRECTORIES" ||
    projection.symlinks !== "forbidden" ||
    projection.submodules !== "forbidden" ||
    projection.specialFiles !== "forbidden" ||
    projection.manifestFraming !== "NUL_RECORDS" ||
    JSON.stringify(projection.manifestRecordFields) !==
      JSON.stringify(["path", "type", "mode", "sizeBytes", "sha256", "linkTarget"])
  ) {
    throw error("EC2_PROJECTION_DRIFT", "/projection", "Projection/archive algorithm drifted.");
  }
  const tar = record(projection.tar, "/projection/tar");
  if (
    tar.mtimeEpochSeconds !== 0 ||
    tar.uid !== 0 ||
    tar.gid !== 0 ||
    tar.uname !== "" ||
    tar.gname !== "" ||
    tar.paxHeaders !== "forbidden" ||
    tar.duplicateEntries !== "forbidden"
  ) {
    throw error("EC2_TAR_DRIFT", "/projection/tar", "Deterministic tar settings drifted.");
  }
}

function assertReceiptContract(source: Record<string, unknown>): void {
  const runner = record(source.runner, "/runner");
  if (
    runner.path !== "scripts/generate-platform-g-contract-external-consumer-v2.ts" ||
    runner.mode !== "AUTHORITY_CHECK_ONLY" ||
    runner.replay !== false ||
    runner.profileWriter !== false ||
    runner.receiptWriter !== false ||
    runner.timeoutSeconds !== 1800 ||
    runner.entrypoint !==
      "bun scripts/generate-platform-g-contract-external-consumer-v2.ts --check-source"
  ) {
    throw error("EC2_RUNNER_DRIFT", "/runner", "Authority runner contract drifted.");
  }
  const toolchain = record(source.toolchain, "/toolchain");
  if (
    toolchain.bun !== "1.4.0" ||
    toolchain.typescript !== "5.7.3" ||
    toolchain.go !== "go version go1.27.0 darwin/arm64" ||
    toolchain.goFlags !== "-mod=readonly" ||
    toolchain.goWork !== "off"
  ) {
    throw error("EC2_TOOLCHAIN_DRIFT", "/toolchain", "Runner toolchain binding drifted.");
  }
  const platforms = arrayField(source, "platformMatrix", "/platformMatrix");
  const expectedPlatforms = [
    {
      id: "darwin-arm64",
      os: "darwin",
      arch: "arm64",
      status: "REQUIRED_PENDING",
      runs: ["A", "B"],
    },
    { id: "linux-amd64", os: "linux", arch: "amd64", status: "REQUIRED_PENDING", runs: ["A", "B"] },
    { id: "linux-arm64", os: "linux", arch: "arm64", status: "NOT_CLAIMED", runs: [] },
  ];
  if (JSON.stringify(platforms) !== JSON.stringify(expectedPlatforms))
    throw error("EC2_PLATFORM_DRIFT", "/platformMatrix", "Platform matrix drifted.");

  const paths = arrayField(source, "receiptPaths", "/receiptPaths");
  if (paths.length !== 11)
    throw error("EC2_RECEIPT_PATH_DRIFT", "/receiptPaths", "Receipt path count drifted.");
  const seen = new Set<string>();
  for (const [index, value] of paths.entries()) {
    const item = record(value, `/receiptPaths/${index}`);
    const path = stringField(item, "path", `/receiptPaths/${index}`);
    if (
      seen.has(path) ||
      !EC2_EXCLUSION_PATHS.includes(path as (typeof EC2_EXCLUSION_PATHS)[number])
    )
      throw error(
        "EC2_RECEIPT_PATH_DRIFT",
        `/receiptPaths/${index}/path`,
        "Receipt path is not an exact late-bound path.",
      );
    seen.add(path);
    if (item.state !== "ABSENT_PENDING" || item.writeMode !== "CREATE_ONCE_APPEND_ONLY")
      throw error(
        "EC2_RECEIPT_STATE_DRIFT",
        `/receiptPaths/${index}`,
        "Receipt must start absent and be append-only.",
      );
  }
  const state = record(source.receiptState, "/receiptState");
  for (const key of [
    "projection",
    "darwin-arm64-a",
    "darwin-arm64-b",
    "darwin-arm64-isolation",
    "linux-amd64-a",
    "linux-amd64-b",
    "linux-amd64-isolation",
    "replaySummary",
    "generatedProfile",
    "authorityReview",
    "finalReplayReview",
  ]) {
    if (state[key] !== "ABSENT_PENDING")
      throw error(
        "EC2_RECEIPT_STATE_DRIFT",
        `/receiptState/${key}`,
        "Receipt state is not absent/pending.",
      );
  }
  if (state.syntheticReceipts !== "FORBIDDEN")
    throw error(
      "EC2_RECEIPT_STATE_DRIFT",
      "/receiptState/syntheticReceipts",
      "Synthetic receipts must be forbidden.",
    );
}

function assertTrackedTreeSafe(
  root: string,
  candidateCommit: string,
  source: Record<string, unknown>,
): void {
  const exclusions = new Set(
    arrayField(record(source.exclusions, "/exclusions"), "paths", "/exclusions/paths").map(String),
  );
  const records = gitBytes(root, ["ls-tree", "-r", "-z", "--full-tree", candidateCommit])
    .toString("utf8")
    .split("\0")
    .filter(Boolean);
  const paths = new Set<string>();
  for (const line of records) {
    const tab = line.indexOf("\t");
    if (tab < 0) throw error("EC2_TREE_INVALID", "/", "Candidate tree entry is malformed.");
    const [mode, type, object] = line.slice(0, tab).split(" ");
    const path = line.slice(tab + 1);
    assertCanonicalPath(path);
    if (paths.has(path))
      throw error("EC2_TREE_INVALID", `/${path}`, "Candidate tree contains a duplicate path.");
    paths.add(path);
    if (exclusions.has(path))
      throw error(
        "EC2_TREE_INVALID",
        `/${path}`,
        "Late-bound output is present in candidate tree.",
      );
    if (type !== "blob" || !object || (mode !== "100644" && mode !== "100755"))
      throw error(
        "EC2_TREE_INVALID",
        `/${path}`,
        "Candidate projection contains a symlink, submodule, or special mode.",
      );
    if (
      [".git", "node_modules", ".idea", "migration.test"].some(
        (part) => path === part || path.startsWith(`${part}/`) || path.includes(`/${part}/`),
      )
    )
      throw error(
        "EC2_TREE_INVALID",
        `/${path}`,
        "Forbidden ambient path is tracked in candidate tree.",
      );
  }
}

function assertAuthorityBaseDiff(root: string, parent: string, candidate: string): void {
  const paths = gitDiffPaths(root, parent, candidate);
  const expected = EC2_AUTHORITY_FILE_PATHS.filter(
    (path) =>
      !path.startsWith(
        "docs/plan/p1/g-contract-current-source-external-consumer-successor-v2-authorization",
      ),
  );
  if (JSON.stringify(paths) !== JSON.stringify([...expected].sort(compareUtf8)))
    throw error(
      "EC2_AUTHORITY_TOPOLOGY_DRIFT",
      "/authorityParent",
      "Authority base does not add exactly the declared base files.",
    );
}

function assertCandidateDiff(root: string, parent: string, candidate: string): void {
  assertSingleAddedPath(root, parent, candidate, EC2_SOURCE_PATH);
}

function discoverCandidate(
  root: string,
  authorityParent: string,
): { commit: string; tree: string } {
  const commit = gitText(root, ["log", "-1", "--format=%H", "--", EC2_SOURCE_PATH]);
  if (!SHA1.test(commit))
    throw error(
      "EC2_CANDIDATE_NOT_COMMITTED",
      `/${EC2_SOURCE_PATH}`,
      "Source must be committed before review.",
    );
  if (gitParents(root, commit) !== authorityParent)
    throw error(
      "EC2_CANDIDATE_TOPOLOGY_DRIFT",
      "/candidate",
      "Candidate is not a direct child of authority base.",
    );
  return { commit, tree: gitTree(root, commit) };
}

function assertSingleAddedPath(
  root: string,
  parent: string,
  commit: string,
  expectedPath: string,
): void {
  const paths = gitDiffPaths(root, parent, commit);
  if (paths.length !== 1 || paths[0] !== expectedPath)
    throw error(
      "EC2_REVIEW_TOPOLOGY_DRIFT",
      `/${expectedPath}`,
      "Append-only child must add exactly one declared path.",
    );
  const entry = gitTreeEntry(root, commit, expectedPath);
  if (!entry || entry.type !== "blob" || entry.mode !== "100644")
    throw error(
      "EC2_REVIEW_TOPOLOGY_DRIFT",
      `/${expectedPath}`,
      "Declared child path must be a regular 100644 blob.",
    );
}

function assertSourceBlob(root: string, bytes: Buffer, commit: string): void {
  const blob = gitBlobAtCommit(root, commit, EC2_SOURCE_PATH);
  if (blob !== gitBlobSha1(bytes))
    throw error(
      "EC2_SOURCE_BLOB_DRIFT",
      `/${EC2_SOURCE_PATH}`,
      "Source bytes differ from candidate Git blob.",
    );
}

function assertFileRecord(root: string, value: Record<string, unknown>, commit: string): void {
  const path = stringField(value, "path", "/file/path");
  assertCanonicalPath(path);
  const declaredBlob = stringField(value, "gitBlob", `/${path}/gitBlob`);
  const declaredSha = stringField(value, "sha256", `/${path}/sha256`);
  const declaredSize = numberField(value, "sizeBytes", `/${path}/sizeBytes`);
  const declaredMode = stringField(value, "mode", `/${path}/mode`);
  assertSha1(declaredBlob, `/${path}/gitBlob`);
  if (!SHA256.test(declaredSha) || declaredSize < 1 || declaredMode !== "100644")
    throw error(
      "EC2_FILE_RECORD_INVALID",
      `/${path}`,
      "File record has invalid digest, size, or mode.",
    );
  const absolute = resolveContained(root, path);
  const bytes = readStableFile(absolute);
  const stat = lstatSync(absolute);
  if (
    stat.mode.toString(8).slice(-6) !== declaredMode ||
    bytes.byteLength !== declaredSize ||
    sha256(bytes) !== declaredSha
  )
    throw error("EC2_FILE_DRIFT", `/${path}`, "Live file bytes differ from the frozen record.");
  const entry = gitTreeEntry(root, commit, path);
  if (
    !entry ||
    entry.type !== "blob" ||
    entry.mode !== declaredMode ||
    entry.object !== declaredBlob
  )
    throw error("EC2_GIT_BLOB_DRIFT", `/${path}`, "Git blob/mode differs from the frozen record.");
  const committedBytes = gitBytes(root, ["cat-file", "blob", declaredBlob]);
  if (committedBytes.byteLength !== declaredSize || sha256(committedBytes) !== declaredSha)
    throw error(
      "EC2_GIT_BYTES_DRIFT",
      `/${path}`,
      "Committed bytes differ from the frozen record.",
    );
}

function gitTreeEntry(
  root: string,
  commit: string,
  path: string,
): { mode: string; type: string; object: string } | undefined {
  const records = gitBytes(root, ["ls-tree", "-z", "--full-tree", commit, "--", path])
    .toString("utf8")
    .split("\0")
    .filter(Boolean);
  if (records.length !== 1) return undefined;
  const tab = records[0]!.indexOf("\t");
  if (tab < 0 || records[0]!.slice(tab + 1) !== path) return undefined;
  const [mode, type, object] = records[0]!.slice(0, tab).split(" ");
  return mode && type && object ? { mode, type, object } : undefined;
}

function gitBlobAtCommit(root: string, commit: string, path: string): string {
  const entry = gitTreeEntry(root, commit, path);
  return entry?.type === "blob" ? entry.object : "";
}

function gitDiffPaths(root: string, parent: string, commit: string): string[] {
  const output = gitText(root, [
    "diff",
    "--no-ext-diff",
    "--name-status",
    "--no-renames",
    parent,
    commit,
  ]);
  if (output.length === 0) return [];
  return output
    .split("\n")
    .map((line) => {
      const tab = line.indexOf("\t");
      if (tab < 0) throw error("EC2_GIT_DIFF_INVALID", "/", "Git diff status is malformed.");
      const status = line.slice(0, tab);
      if (status !== "A" && status !== "M")
        throw error("EC2_GIT_DIFF_INVALID", "/", "Only add/modify statuses are allowed.");
      return line.slice(tab + 1);
    })
    .sort(compareUtf8);
}

function assertNoUntrackedNonignoredFiles(root: string): void {
  const output = gitText(root, ["ls-files", "--others", "--exclude-standard"]);
  if (output.trim() !== "")
    throw error("EC2_UNTRACKED_INPUT", "/", `Untracked files are not admissible: ${output.trim()}`);
}

function assertGitRoot(root: string): void {
  const top = gitText(root, ["rev-parse", "--show-toplevel"]);
  if (realpathSync(top) !== root)
    throw error("EC2_ROOT_INVALID", "/", "Checker root is not the Git worktree root.");
}

function resolveContained(root: string, path: string, allowMissingFinal = false): string {
  assertCanonicalPath(path);
  const base = realpathSync(resolve(root));
  const target = resolve(base, path);
  const relation = relative(base, target);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation)
  )
    throw error("EC2_PATH_ESCAPE", `/${path}`, "Path escapes repository root.");
  let current = base;
  for (const [index, component] of path.split("/").entries()) {
    current = resolve(current, component);
    const final = index === path.split("/").length - 1;
    try {
      const stat = lstatSync(current);
      if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile()))
        throw error(
          "EC2_PATH_ESCAPE",
          `/${path}`,
          "Path traverses a symlink or non-regular component.",
        );
    } catch (cause) {
      if (allowMissingFinal && final && isMissing(cause)) return current;
      throw cause;
    }
  }
  if (realpathSync(current) !== current)
    throw error("EC2_PATH_ESCAPE", `/${path}`, "Path resolves through a symlink.");
  return current;
}

function assertCanonicalPath(path: string): void {
  if (
    path.length === 0 ||
    path.includes("\\") ||
    path.includes("\0") ||
    isAbsolute(path) ||
    path.split("/").some((part) => part.length === 0 || part === "." || part === "..")
  )
    throw error(
      "EC2_PATH_INVALID",
      `/${path}`,
      "Path must be canonical UTF-8 repository-relative text.",
    );
}

function readStableFile(path: string): Buffer {
  const before = lstatSync(path, { bigint: true });
  if (!before.isFile() || before.isSymbolicLink() || realpathSync(path) !== path)
    throw new Error("not a canonical regular file");
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const descriptorBefore = fstatSync(fd, { bigint: true });
    if (
      !descriptorBefore.isFile() ||
      descriptorBefore.dev !== before.dev ||
      descriptorBefore.ino !== before.ino
    )
      throw new Error("file changed before read");
    const bytes = readFileSync(fd);
    const after = fstatSync(fd, { bigint: true });
    const pathAfter = lstatSync(path, { bigint: true });
    if (
      !after.isFile() ||
      after.dev !== descriptorBefore.dev ||
      after.ino !== descriptorBefore.ino ||
      after.size !== descriptorBefore.size ||
      after.mtimeNs !== descriptorBefore.mtimeNs ||
      after.ctimeNs !== descriptorBefore.ctimeNs ||
      !pathAfter.isFile() ||
      pathAfter.isSymbolicLink() ||
      pathAfter.dev !== descriptorBefore.dev ||
      pathAfter.ino !== descriptorBefore.ino ||
      realpathSync(path) !== path
    )
      throw new Error("file changed while read");
    return bytes;
  } finally {
    closeSync(fd);
  }
}

function assertCanonicalJson(bytes: Buffer, path: string): void {
  const parsed = parseJson(bytes, path);
  const canonical = `${JSON.stringify(parsed, null, 2)}\n`;
  if (Buffer.compare(bytes, Buffer.from(canonical, "utf8")) !== 0)
    throw error(
      "EC2_JSON_CANONICAL",
      `/${path}`,
      "JSON must use canonical two-space formatting and one trailing newline.",
    );
}

function parseJson(bytes: Buffer, path: string): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(bytes.toString("utf8"));
    if (typeof value !== "object" || value === null || Array.isArray(value))
      throw new Error("expected object");
    return value as Record<string, unknown>;
  } catch (cause) {
    throw error("EC2_JSON_INVALID", `/${path}`, String(cause));
  }
}

function validateSchema(root: string, value: unknown, schemaPath: string): void {
  const schema = parseJson(readStableFile(resolveContained(root, schemaPath)), schemaPath);
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  const validate = ajv.compile(schema);
  if (!validate(value))
    throw error("EC2_SCHEMA_INVALID", "/", ajv.errorsText(validate.errors ?? undefined));
}

function record(value: unknown, path: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw error("EC2_SHAPE_INVALID", path, "Expected an object.");
  return value as Record<string, unknown>;
}

function arrayField(value: Record<string, unknown>, key: string, path: string): unknown[] {
  if (!Array.isArray(value[key])) throw error("EC2_SHAPE_INVALID", path, "Expected an array.");
  return value[key] as unknown[];
}

function stringField(value: Record<string, unknown>, key: string, path: string): string {
  if (typeof value[key] !== "string")
    throw error("EC2_SHAPE_INVALID", `${path}/${key}`, "Expected a string.");
  return value[key] as string;
}

function numberField(value: Record<string, unknown>, key: string, path: string): number {
  if (typeof value[key] !== "number" || !Number.isSafeInteger(value[key]))
    throw error("EC2_SHAPE_INVALID", `${path}/${key}`, "Expected a safe integer.");
  return value[key] as number;
}

function assertSha1(value: string, path: string): void {
  if (!SHA1.test(value)) throw error("EC2_SHA_INVALID", path, "Expected a lowercase SHA-1.");
}

function assertPathOrder(values: unknown[], expected: readonly string[], path: string): void {
  if (values.length !== expected.length)
    throw error("EC2_INVENTORY_DRIFT", path, `Expected exactly ${expected.length} entries.`);
  const actual = values.map((value, index) =>
    stringField(record(value, `${path}/${index}`), "path", `${path}/${index}`),
  );
  if (JSON.stringify(actual) !== JSON.stringify(expected))
    throw error("EC2_INVENTORY_DRIFT", path, "Ordered path inventory drifted.");
}

function assertExactObject(
  actual: Record<string, unknown>,
  expected: Record<string, unknown>,
  path: string,
): void {
  if (JSON.stringify(actual) !== JSON.stringify(expected))
    throw error("EC2_LINEAGE_DRIFT", path, "Exact lineage tuple drifted.");
}

function gitText(root: string, command: readonly string[]): string {
  return execFileSync("git", [...FIXED_GIT_ARGS, ...command], {
    cwd: root,
    env: FIXED_GIT_ENV,
    encoding: "utf8",
  }).trim();
}

function gitBytes(root: string, command: readonly string[]): Buffer {
  return execFileSync("git", [...FIXED_GIT_ARGS, ...command], {
    cwd: root,
    env: FIXED_GIT_ENV,
    maxBuffer: 128 * 1024 * 1024,
  });
}

function gitTree(root: string, commit: string): string {
  return gitText(root, ["show", "-s", "--format=%T", commit]);
}

function gitParents(root: string, commit: string): string {
  return gitText(root, ["show", "-s", "--format=%P", commit]);
}

function sha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function gitBlobSha1(bytes: Uint8Array): string {
  return createHash("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
}

function compareUtf8(left: string, right: string): number {
  return Buffer.from(left, "utf8").compare(Buffer.from(right, "utf8"));
}

function isMissing(value: unknown): boolean {
  return (
    typeof value === "object" &&
    value !== null &&
    "code" in value &&
    (value as { code?: unknown }).code === "ENOENT"
  );
}

function error(code: string, path: string, message: string): ExternalConsumerV2Error {
  return new ExternalConsumerV2Error(code, path, message);
}
