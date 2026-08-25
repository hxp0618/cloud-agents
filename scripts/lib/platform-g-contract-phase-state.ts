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
  buildGContractPhaseRecordModel,
  buildGContractPhaseBindingRegistry,
  domainSeparatedGitDiffDigest,
  G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  G_CONTRACT_PHASE_FINAL_REVIEW_PATH,
  G_CONTRACT_PHASE_R5_REVIEW_PATH,
  G_CONTRACT_PHASE_RECORD_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_PATH,
  readGContractPhaseRecordSource,
  renderGContractPhaseRecord,
  serializeGContractPhaseJson,
  validateGContractPhaseBindingRegistry,
  validateGContractPhaseReviewTuple,
  type CandidateGitBinding,
  type Digest,
  type GContractPhaseBindingRegistry,
  type GContractPhaseRecordBuildInput,
  type GContractPhaseReviewTuple,
  type ReviewBinding,
  type ReviewGitBinding,
  type ReviewSubject,
} from "./platform-g-contract-phase-record";
import {
  buildPlatformContractLockV3PhaseBound,
  derivePlatformContractLockV3AssembledSnapshotIdentity,
  PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS,
  serializePlatformContractLockV3,
  type PlatformContractLockV3ArtifactIdentity,
  type PlatformContractLockV3AssembledDocument,
  type PlatformContractLockV3PhaseBoundDocument,
} from "./platform-contract-lock-v3";
import { canonicalizeJson } from "./platform-json-semantics";

const PHASE_LOCK_PATH = "contracts/generation.lock.json";
const REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0" as const;
const TERMINAL_REVIEW_DIFF_DOMAIN = "cloud-agents/g-contract-phase/terminal-binding-review-diff/v1";
const VERDICT_HEADING = /^## Verdict[ \t]*$/gmu;
const APPROVE_VERDICT_LINE = /^`?APPROVE\s*(?:-|—)\s*P0=0\s*\/\s*P1=0\s*\/\s*P2=0`?$/u;

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
  "diff.external=",
  "-c",
  "diff.renames=false",
] as const;

export type GContractPhaseTopologyState =
  | "PRE_CANDIDATE_ABSENT"
  | "R5_CURRENT_REVIEW_ABSENT"
  | "R5_REVIEW_CURRENT_BINDING_ABSENT"
  | "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT"
  | "FINAL_REVIEW_PRESENT_UNVERIFIED";

export type GContractPhaseEffectiveState =
  | GContractPhaseTopologyState
  | "COMPLETE_TUPLE_READY_TO_WRITE"
  | "REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE";

export type GContractPhaseInspectionOptions = Readonly<{
  recordBuildInput?: GContractPhaseRecordBuildInput;
  expectedTuple?: GContractPhaseReviewTuple;
  expectedRegistry?: GContractPhaseBindingRegistry;
  bindingActorId?: string;
  expectedTerminalReview?: ReviewGitBinding;
}>;

export class GContractPhaseStateError extends Error {
  constructor(
    readonly code:
      | "G_CONTRACT_PHASE_PARTIAL_STATE"
      | "G_CONTRACT_PHASE_PATH_INVALID"
      | "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID"
      | "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID"
      | "G_CONTRACT_PHASE_REVIEW_VERDICT_INVALID"
      | "G_CONTRACT_PHASE_SELF_REVIEW"
      | "G_CONTRACT_PHASE_RECORD_DRIFT"
      | "G_CONTRACT_PHASE_BINDING_DRIFT"
      | "G_CONTRACT_PHASE_TERMINAL_INPUT_REQUIRED",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GContractPhaseStateError";
  }
}

export function classifyGContractPhaseTopology(root: string): GContractPhaseTopologyState {
  const r5 = regularPathExists(root, G_CONTRACT_PHASE_RECORD_PATH);
  const r5Review = regularPathExists(root, G_CONTRACT_PHASE_R5_REVIEW_PATH);
  const tuple = regularPathExists(root, G_CONTRACT_PHASE_REVIEW_TUPLE_PATH);
  const registry = regularPathExists(root, G_CONTRACT_PHASE_BINDING_REGISTRY_PATH);
  const finalReview = regularPathExists(root, G_CONTRACT_PHASE_FINAL_REVIEW_PATH);

  if (!r5) {
    if (r5Review || tuple || registry || finalReview)
      partial("R5 is absent while a downstream path exists.");
    return "PRE_CANDIDATE_ABSENT";
  }
  if (!r5Review) {
    if (tuple || registry || finalReview)
      partial("R5 review is absent while a binding path exists.");
    return "R5_CURRENT_REVIEW_ABSENT";
  }
  if (!tuple && !registry) {
    if (finalReview) partial("Terminal review cannot precede tuple and registry.");
    return "R5_REVIEW_CURRENT_BINDING_ABSENT";
  }
  if (tuple !== registry) partial("Tuple and registry must appear in the same Slice I candidate.");
  return finalReview
    ? "FINAL_REVIEW_PRESENT_UNVERIFIED"
    : "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT";
}

export function inspectGContractPhaseState(
  root: string,
  options: GContractPhaseInspectionOptions = {},
): GContractPhaseEffectiveState {
  readGContractPhaseRecordSource(root);
  const topology = classifyGContractPhaseTopology(root);

  if (topology === "PRE_CANDIDATE_ABSENT") return topology;
  if (options.recordBuildInput === undefined) {
    throw stateError(
      "G_CONTRACT_PHASE_TERMINAL_INPUT_REQUIRED",
      "/record",
      "The exact typed R5 build input is required once R5 exists.",
    );
  }
  const expectedRecordBytes = renderGContractPhaseRecord(
    root,
    buildGContractPhaseRecordModel(root, options.recordBuildInput),
  );
  assertExactFileBytes(root, G_CONTRACT_PHASE_RECORD_PATH, expectedRecordBytes);

  if (topology === "R5_CURRENT_REVIEW_ABSENT") {
    assertSingleAddedRegularPathCommit(root, "HEAD", G_CONTRACT_PHASE_RECORD_PATH);
    return topology;
  }

  if (options.expectedTuple === undefined) {
    if (topology === "R5_REVIEW_CURRENT_BINDING_ABSENT") {
      assertCurrentR5ReviewTopology(root);
      return topology;
    }
    throw stateError(
      "G_CONTRACT_PHASE_TERMINAL_INPUT_REQUIRED",
      "/tuple",
      "The exact two-lineage tuple is required once binding outputs exist.",
    );
  }
  validateGContractPhaseReviewTuple(root, options.expectedTuple);
  assertGContractPhaseReviewLineages(root, options.expectedTuple);

  if (topology === "R5_REVIEW_CURRENT_BINDING_ABSENT") {
    return "COMPLETE_TUPLE_READY_TO_WRITE";
  }

  const tuple = readCanonicalJson<GContractPhaseReviewTuple>(
    root,
    G_CONTRACT_PHASE_REVIEW_TUPLE_PATH,
  );
  const registry = readCanonicalJson<GContractPhaseBindingRegistry>(
    root,
    G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  );
  if (!canonicalEqual(tuple, options.expectedTuple)) {
    bindingDrift("Persisted tuple differs from the exact caller-rebuilt tuple.");
  }
  validateGContractPhaseBindingRegistry(root, tuple, registry);
  const expectedRegistry =
    options.expectedRegistry ?? buildGContractPhaseBindingRegistry(root, options.expectedTuple);
  if (!canonicalEqual(registry, expectedRegistry)) {
    bindingDrift("Persisted registry differs from the exact pre-terminal derivation.");
  }

  if (topology === "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT") {
    assertSliceIBindingCandidate(root, "HEAD", tuple);
    return topology;
  }

  if (options.bindingActorId === undefined || options.expectedTerminalReview === undefined) {
    throw stateError(
      "G_CONTRACT_PHASE_TERMINAL_INPUT_REQUIRED",
      "/terminalReview",
      "The binding actor and complete expected terminal-review identity are required for terminal verification.",
    );
  }
  if (options.bindingActorId === options.expectedTerminalReview.reviewerId) {
    selfReview("terminalReview");
  }
  const terminalCommit = gitText(root, ["rev-parse", "HEAD"]);
  if (terminalCommit !== options.expectedTerminalReview.commit) {
    lineageInvalid("terminalReview", "HEAD is not the fixed expected terminal-review commit.");
  }
  const bindingCommit = gitText(root, ["rev-parse", `${terminalCommit}^`]);
  assertSliceIBindingCandidate(root, bindingCommit, tuple);
  assertTerminalReviewBinding(root, bindingCommit, options.expectedTerminalReview);
  return "REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE";
}

export function captureGContractPhaseTerminalReviewBinding(
  root: string,
  bindingCommit: string,
  reviewCommit: string,
  reviewerId: string,
): ReviewGitBinding {
  return captureReviewOnlyCommit(
    root,
    bindingCommit,
    reviewCommit,
    G_CONTRACT_PHASE_FINAL_REVIEW_PATH,
    TERMINAL_REVIEW_DIFF_DOMAIN,
    reviewerId,
  );
}

export function captureGContractPhaseReviewBinding(
  root: string,
  subject: ReviewSubject,
  candidateCommit: string,
  reviewCommit: string,
  candidateActorId: string,
  reviewerId: string,
): ReviewBinding {
  const source = readGContractPhaseRecordSource(root);
  const slot = source.reviewSlots.find((candidate) => candidate.subject === subject);
  if (!slot) lineageInvalid(subject, "Unknown or reordered review subject.");
  if (candidateActorId === reviewerId) selfReview(subject);
  const candidateParent = singleParent(root, candidateCommit, `/${subject}/candidate`);
  const candidateTree = commitTree(root, candidateCommit);
  const candidateDiff = gitDiff(root, candidateParent, candidateCommit);
  const candidate: CandidateGitBinding = {
    actorId: candidateActorId,
    commit: candidateCommit,
    tree: candidateTree,
    parent: candidateParent,
    diffSha256: domainSeparatedGitDiffDigest(slot.candidateDiffDomain, candidateDiff),
  };
  const review = captureReviewOnlyCommit(
    root,
    candidateCommit,
    reviewCommit,
    slot.reviewPath,
    slot.reviewDiffDomain,
    reviewerId,
  );
  const binding: ReviewBinding = {
    subject,
    candidateSubjectPath: slot.candidateSubjectPath,
    candidate,
    review,
  };
  assertReviewBinding(root, binding, source.reviewSlots.indexOf(slot));
  return binding;
}

export function assertGContractPhaseReviewLineages(
  root: string,
  tuple: GContractPhaseReviewTuple,
): void {
  validateGContractPhaseReviewTuple(root, tuple);
  for (const [index, binding] of tuple.reviews.entries()) {
    assertReviewBinding(root, binding, index);
  }
}

export function assertSingleAddedRegularPathCommit(
  root: string,
  commit: string,
  path: string,
): void {
  const parent = singleParent(root, commit, `/${path}/candidate`);
  assertExactChangedOperations(root, parent, commit, [{ status: "A", path }]);
  assertRegularTreeEntry(root, commit, path);
  assertPathAbsentAtCommit(root, parent, path);
}

export function assertReviewOnlyCommit(
  root: string,
  candidateCommit: string,
  reviewCommit: string,
  reviewPath: string,
  reviewDiffDomain: string,
  expectedDiffSha256?: Digest,
): void {
  const parent = singleParent(root, reviewCommit, `/${reviewPath}/review`);
  if (parent !== candidateCommit) {
    lineageInvalid(reviewPath, "Review is not the direct child of its candidate.");
  }
  assertPathAbsentAtCommit(root, candidateCommit, reviewPath);
  assertExactChangedOperations(root, candidateCommit, reviewCommit, [
    { status: "A", path: reviewPath },
  ]);
  assertRegularTreeEntry(root, reviewCommit, reviewPath);
  const diff = domainSeparatedGitDiffDigest(
    reviewDiffDomain,
    gitDiff(root, candidateCommit, reviewCommit),
  );
  if (expectedDiffSha256 !== undefined && diff !== expectedDiffSha256) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
      `/${reviewPath}`,
      "Domain-separated review diff digest drifted.",
    );
  }
  assertReviewVerdict(
    gitBytes(root, ["cat-file", "blob", `${reviewCommit}:${reviewPath}`]),
    reviewPath,
  );
}

function assertReviewBinding(root: string, binding: ReviewBinding, index: number): void {
  const source = readGContractPhaseRecordSource(root);
  const slot = source.reviewSlots[index];
  if (
    !slot ||
    binding.subject !== slot.subject ||
    binding.candidateSubjectPath !== slot.candidateSubjectPath ||
    binding.review.path !== slot.reviewPath ||
    binding.review.verdict !== REQUIRED_VERDICT ||
    binding.review.findings.p0 !== 0 ||
    binding.review.findings.p1 !== 0 ||
    binding.review.findings.p2 !== 0
  ) {
    lineageInvalid(
      String(index),
      "Review tuple slot identity, order, path, verdict, or findings drifted.",
    );
  }
  if (
    binding.candidate.actorId === binding.review.reviewerId ||
    binding.candidate.commit === binding.review.commit
  ) {
    selfReview(String(index));
  }
  if (
    singleParent(root, binding.candidate.commit, `/reviews/${index}/candidate`) !==
    binding.candidate.parent
  ) {
    lineageInvalid(String(index), "Candidate parent drifted or candidate is a merge.");
  }
  if (commitTree(root, binding.candidate.commit) !== binding.candidate.tree) {
    lineageInvalid(String(index), "Candidate tree drifted.");
  }
  const candidateDiff = domainSeparatedGitDiffDigest(
    slot.candidateDiffDomain,
    gitDiff(root, binding.candidate.parent, binding.candidate.commit),
  );
  if (candidateDiff !== binding.candidate.diffSha256) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
      `/reviews/${index}/candidateDiffSha256`,
      "Candidate diff digest drifted.",
    );
  }
  assertRegularTreeEntry(root, binding.candidate.commit, binding.candidateSubjectPath);
  assertReviewOnlyCommit(
    root,
    binding.candidate.commit,
    binding.review.commit,
    binding.review.path,
    slot.reviewDiffDomain,
    binding.review.diffSha256,
  );
  if (
    commitTree(root, binding.review.commit) !== binding.review.tree ||
    binding.review.parent !== binding.candidate.commit
  ) {
    lineageInvalid(String(index), "Review tree or parent drifted.");
  }
  const entry = treeEntry(root, binding.review.commit, binding.review.path);
  const bytes = gitBytes(root, [
    "cat-file",
    "blob",
    `${binding.review.commit}:${binding.review.path}`,
  ]);
  assertLiveBytesMatchGit(root, binding.review.path, bytes, "review");
  const sha = fileDigest(bytes);
  if (
    entry.blob !== binding.review.gitBlob ||
    binding.review.mode !== "100644" ||
    sha !== binding.review.sha256 ||
    bytes.byteLength !== binding.review.sizeBytes
  ) {
    lineageInvalid(String(index), "Review blob, mode, digest, or size drifted.");
  }
}

function assertCurrentR5ReviewTopology(root: string): void {
  const source = readGContractPhaseRecordSource(root);
  const slot = source.reviewSlots[1];
  if (!slot || slot.subject !== "g_contract_r5") {
    lineageInvalid("g_contract_r5", "R5 review slot is missing or reordered.");
  }
  const reviewCommit = gitText(root, ["rev-parse", "HEAD"]);
  const candidateCommit = singleParent(root, reviewCommit, "/g_contract_r5/review");
  assertSingleAddedRegularPathCommit(root, candidateCommit, G_CONTRACT_PHASE_RECORD_PATH);
  assertReviewOnlyCommit(
    root,
    candidateCommit,
    reviewCommit,
    slot.reviewPath,
    slot.reviewDiffDomain,
  );
  assertLiveBytesMatchGit(
    root,
    slot.reviewPath,
    gitBytes(root, ["cat-file", "blob", `${reviewCommit}:${slot.reviewPath}`]),
    "R5 review",
  );
}

function captureReviewOnlyCommit(
  root: string,
  candidateCommit: string,
  reviewCommit: string,
  reviewPath: string,
  diffDomain: string,
  reviewerId: string,
): ReviewGitBinding {
  assertReviewOnlyCommit(root, candidateCommit, reviewCommit, reviewPath, diffDomain);
  const entry = treeEntry(root, reviewCommit, reviewPath);
  const bytes = gitBytes(root, ["cat-file", "blob", `${reviewCommit}:${reviewPath}`]);
  return {
    reviewerId,
    commit: reviewCommit,
    tree: commitTree(root, reviewCommit),
    parent: candidateCommit,
    path: reviewPath,
    gitBlob: entry.blob,
    sha256: fileDigest(bytes),
    sizeBytes: bytes.byteLength,
    mode: "100644",
    diffSha256: domainSeparatedGitDiffDigest(
      diffDomain,
      gitDiff(root, candidateCommit, reviewCommit),
    ),
    verdict: REQUIRED_VERDICT,
    findings: { p0: 0, p1: 0, p2: 0 },
  };
}

function assertSliceIBindingCandidate(
  root: string,
  candidateCommit: string,
  tuple: GContractPhaseReviewTuple,
): void {
  const parent = singleParent(root, candidateCommit, "/sliceI");
  const r5ReviewCommit = tuple.reviews[1]!.review.commit;
  if (parent !== r5ReviewCommit) {
    lineageInvalid("sliceI", "Slice I is not the direct child of the fixed R5 review.");
  }
  assertExactChangedOperations(root, parent, candidateCommit, [
    { status: "M", path: PHASE_LOCK_PATH },
    { status: "A", path: G_CONTRACT_PHASE_BINDING_REGISTRY_PATH },
    { status: "A", path: G_CONTRACT_PHASE_REVIEW_TUPLE_PATH },
  ]);
  assertRegularTreeEntry(root, candidateCommit, PHASE_LOCK_PATH);
  assertRegularTreeEntry(root, candidateCommit, G_CONTRACT_PHASE_BINDING_REGISTRY_PATH);
  assertRegularTreeEntry(root, candidateCommit, G_CONTRACT_PHASE_REVIEW_TUPLE_PATH);
  assertPathAbsentAtCommit(root, parent, G_CONTRACT_PHASE_BINDING_REGISTRY_PATH);
  assertPathAbsentAtCommit(root, parent, G_CONTRACT_PHASE_REVIEW_TUPLE_PATH);
  assertPathAbsentAtCommit(root, candidateCommit, G_CONTRACT_PHASE_FINAL_REVIEW_PATH);
  assertPhaseBoundLockCurrent(root, candidateCommit, tuple.reviews[0]!.candidate.commit);
}

function assertTerminalReviewBinding(
  root: string,
  bindingCommit: string,
  review: ReviewGitBinding,
): void {
  if (
    review.parent !== bindingCommit ||
    review.path !== G_CONTRACT_PHASE_FINAL_REVIEW_PATH ||
    review.mode !== "100644" ||
    review.verdict !== REQUIRED_VERDICT ||
    review.findings.p0 !== 0 ||
    review.findings.p1 !== 0 ||
    review.findings.p2 !== 0
  ) {
    lineageInvalid(
      "terminalReview",
      "Expected terminal review parent, path, mode, verdict, or findings drifted.",
    );
  }
  assertReviewOnlyCommit(
    root,
    bindingCommit,
    review.commit,
    review.path,
    TERMINAL_REVIEW_DIFF_DOMAIN,
    review.diffSha256,
  );
  if (commitTree(root, review.commit) !== review.tree) {
    lineageInvalid("terminalReview", "Expected terminal-review tree drifted.");
  }
  const entry = treeEntry(root, review.commit, review.path);
  const bytes = gitBytes(root, ["cat-file", "blob", `${review.commit}:${review.path}`]);
  assertLiveBytesMatchGit(root, review.path, bytes, "terminal review");
  if (
    entry.blob !== review.gitBlob ||
    fileDigest(bytes) !== review.sha256 ||
    bytes.byteLength !== review.sizeBytes
  ) {
    lineageInvalid("terminalReview", "Expected terminal-review blob, digest, or size drifted.");
  }
}

function assertPhaseBoundLockCurrent(
  root: string,
  phaseBoundCommit: string,
  assembledCommit: string,
): void {
  try {
    const assembledBytes = gitBytes(root, [
      "cat-file",
      "blob",
      `${assembledCommit}:${PHASE_LOCK_PATH}`,
    ]);
    const phaseBoundBytes = gitBytes(root, [
      "cat-file",
      "blob",
      `${phaseBoundCommit}:${PHASE_LOCK_PATH}`,
    ]);
    const assembled = parseLockDocument<PlatformContractLockV3AssembledDocument>(
      assembledBytes,
      "ASSEMBLED",
    );
    const phaseBound = parseLockDocument<PlatformContractLockV3PhaseBoundDocument>(
      phaseBoundBytes,
      "PHASE_BOUND",
    );
    if (assembled.state !== "ASSEMBLED" || phaseBound.state !== "PHASE_BOUND") {
      lineageInvalid("phaseBoundLock", "Lock Git objects do not contain the ordered v3 states.");
    }
    const assembledSnapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
      commitSha1: assembledCommit,
      treeSha1: commitTree(root, assembledCommit),
    });
    const expected = buildPlatformContractLockV3PhaseBound(
      assembled,
      assembledSnapshot,
      phaseBound.phaseBinding,
    );
    if (!canonicalEqual(expected, phaseBound)) {
      lineageInvalid(
        "phaseBoundLock",
        "PHASE_BOUND lock is not the exact successor of the fixed ASSEMBLED Git object.",
      );
    }
    assertPhaseArtifactIdentities(root, phaseBoundCommit, phaseBound);
    if (!readStableFile(root, PHASE_LOCK_PATH).equals(phaseBoundBytes)) {
      lineageInvalid("phaseBoundLock", "The live lock differs from the fixed Slice I Git blob.");
    }
  } catch (error) {
    if (error instanceof GContractPhaseStateError) throw error;
    lineageInvalid(
      "phaseBoundLock",
      `Cannot verify the exact ASSEMBLED -> PHASE_BOUND lock relation: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

function parseLockDocument<T>(bytes: Buffer, expectedState: "ASSEMBLED" | "PHASE_BOUND"): T {
  const value: unknown = JSON.parse(bytes.toString("utf8"));
  if (serializePlatformContractLockV3(value) !== bytes.toString("utf8")) {
    lineageInvalid(
      "phaseBoundLock",
      `${expectedState} lock is not canonical versioned lock-v3 JSON.`,
    );
  }
  return value as T;
}

function assertPhaseArtifactIdentities(
  root: string,
  phaseBoundCommit: string,
  document: PlatformContractLockV3PhaseBoundDocument,
): void {
  for (const [index, declared] of document.phaseBinding.artifacts.entries()) {
    const expected = PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS[index];
    if (expected === undefined || declared.role !== expected.role) {
      lineageInvalid("phaseBoundLock", "Phase-bound lock artifact order or role drifted.");
    }
    const entry = treeEntry(root, phaseBoundCommit, expected.path);
    const bytes = gitBytes(root, ["cat-file", "blob", `${phaseBoundCommit}:${expected.path}`]);
    assertLiveBytesMatchGit(root, expected.path, bytes, "phase-bound artifact");
    const actual: PlatformContractLockV3ArtifactIdentity = {
      path: expected.path,
      fileType: "REGULAR_FILE",
      gitMode: "100644",
      gitBlobSha1: entry.blob,
      sha256: fileDigest(bytes),
      sizeBytes: bytes.byteLength,
    };
    if (!canonicalEqual(actual, declared.artifact)) {
      lineageInvalid(
        "phaseBoundLock",
        `Phase-bound lock artifact identity drifted for ${expected.path}.`,
      );
    }
  }
}

function assertLiveBytesMatchGit(root: string, path: string, expected: Buffer, role: string): void {
  if (!readStableFile(root, path).equals(expected)) {
    lineageInvalid(path, `Current ${role} bytes differ from the fixed Git blob.`);
  }
}

function assertExactChangedOperations(
  root: string,
  from: string,
  to: string,
  expected: readonly { readonly status: "A" | "M"; readonly path: string }[],
): void {
  const raw = gitBytes(root, ["diff", "--name-status", "-z", "--no-renames", from, to]);
  const fields = raw.toString("utf8").split("\0");
  if (fields.at(-1) === "") fields.pop();
  if (fields.length % 2 !== 0) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
      "/gitDiff",
      "Git name-status output is malformed.",
    );
  }
  const actual: { status: string; path: string }[] = [];
  for (let index = 0; index < fields.length; index += 2) {
    actual.push({ status: fields[index]!, path: fields[index + 1]! });
  }
  if (
    actual.length !== expected.length ||
    actual.some(
      (operation, index) =>
        operation.status !== expected[index]!.status || operation.path !== expected[index]!.path,
    )
  ) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
      "/gitDiff",
      "Commit operations differ by status, path, count, order, or contain rename/copy/extra paths.",
    );
  }
}

function assertReviewVerdict(bytes: Buffer, path: string): void {
  const text = bytes.toString("utf8");
  const headings = [...text.matchAll(VERDICT_HEADING)];
  if (headings.length !== 1) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_VERDICT_INVALID",
      `/${path}`,
      "Review must contain exactly one level-two Verdict section.",
    );
  }
  const afterHeading = text.slice(headings[0]!.index! + headings[0]![0].length);
  const verdictLine = afterHeading
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .find((line) => line.length > 0);
  if (verdictLine === undefined || !APPROVE_VERDICT_LINE.test(verdictLine)) {
    throw stateError(
      "G_CONTRACT_PHASE_REVIEW_VERDICT_INVALID",
      `/${path}`,
      "The Verdict section must begin with the exact APPROVE P0=0/P1=0/P2=0 value.",
    );
  }
}

function singleParent(root: string, commit: string, path: string): string {
  try {
    if (gitText(root, ["cat-file", "-t", commit]) !== "commit") {
      lineageInvalid(path, "Expected a commit object.");
    }
    const parents = gitText(root, ["show", "-s", "--format=%P", commit]).split(" ").filter(Boolean);
    if (parents.length !== 1) lineageInvalid(path, "Commit must have exactly one parent.");
    return parents[0]!;
  } catch (error) {
    if (error instanceof GContractPhaseStateError) throw error;
    lineageInvalid(path, "Commit object or parent is unavailable.");
  }
}

function commitTree(root: string, commit: string): string {
  try {
    return gitText(root, ["rev-parse", `${commit}^{tree}`]);
  } catch {
    lineageInvalid(commit, "Commit tree is unavailable.");
  }
}

function treeEntry(
  root: string,
  commit: string,
  path: string,
): { readonly mode: "100644"; readonly blob: string } {
  const output = gitText(root, ["ls-tree", commit, "--", path]);
  const match = /^100644 blob ([0-9a-f]{40})\t(.+)$/u.exec(output);
  if (!match || match[2] !== path) {
    throw stateError(
      "G_CONTRACT_PHASE_PATH_INVALID",
      `/${path}`,
      "Expected an exact regular non-symlink 100644 Git blob.",
    );
  }
  return { mode: "100644", blob: match[1]! };
}

function assertRegularTreeEntry(root: string, commit: string, path: string): void {
  treeEntry(root, commit, path);
}

function assertPathAbsentAtCommit(root: string, commit: string, path: string): void {
  if (gitText(root, ["ls-tree", "-r", "--name-only", commit, "--", path]) !== "") {
    throw stateError(
      "G_CONTRACT_PHASE_PATH_INVALID",
      `/${path}`,
      "Late review or binding path must be absent in its candidate parent tree.",
    );
  }
}

function regularPathExists(root: string, path: string): boolean {
  const absolute = resolveContainedPath(root, path, true);
  try {
    const stat = lstatSync(absolute);
    if (!stat.isFile() || stat.isSymbolicLink() || realpathSync(absolute) !== absolute) {
      throw stateError(
        "G_CONTRACT_PHASE_PATH_INVALID",
        `/${path}`,
        "Phase topology accepts only contained regular non-symlink paths.",
      );
    }
    return true;
  } catch (error) {
    if (error instanceof GContractPhaseStateError) throw error;
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
    throw error;
  }
}

function assertExactFileBytes(root: string, path: string, expected: string): void {
  if (readStableFile(root, path).toString("utf8") !== expected) {
    throw stateError(
      "G_CONTRACT_PHASE_RECORD_DRIFT",
      `/${path}`,
      "Persisted R5 bytes differ from the deterministic typed renderer.",
    );
  }
}

function readCanonicalJson<T>(root: string, path: string): T {
  const bytes = readStableFile(root, path);
  try {
    const value: unknown = JSON.parse(bytes.toString("utf8"));
    if (bytes.toString("utf8") !== serializeGContractPhaseJson(value)) {
      bindingDrift(`${path} is not canonical two-space JSON with one trailing newline.`);
    }
    return value as T;
  } catch (error) {
    if (error instanceof GContractPhaseStateError) throw error;
    bindingDrift(`Cannot parse ${path} as strict JSON.`);
  }
}

function readStableFile(root: string, path: string): Buffer {
  const absolute = resolveContainedPath(root, path);
  const pathBefore = lstatSync(absolute, { bigint: true });
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (
      !descriptorBefore.isFile() ||
      descriptorBefore.dev !== pathBefore.dev ||
      descriptorBefore.ino !== pathBefore.ino
    ) {
      throw stateError(
        "G_CONTRACT_PHASE_PATH_INVALID",
        `/${path}`,
        "Input changed before it could be opened.",
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
      throw stateError(
        "G_CONTRACT_PHASE_PATH_INVALID",
        `/${path}`,
        "Input changed while it was being read.",
      );
    }
    return bytes;
  } finally {
    closeSync(descriptor);
  }
}

function resolveContainedPath(root: string, path: string, allowMissing = false): string {
  const rootReal = realpathSync(resolve(root));
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw stateError(
      "G_CONTRACT_PHASE_PATH_INVALID",
      `/${path}`,
      "Path must be canonical and repository-relative.",
    );
  }
  const absolute = resolve(rootReal, ...path.split("/"));
  const lexical = relative(rootReal, absolute);
  if (lexical === "" || lexical === ".." || lexical.startsWith(`..${sep}`) || isAbsolute(lexical)) {
    throw stateError("G_CONTRACT_PHASE_PATH_INVALID", `/${path}`, "Path escapes repository root.");
  }
  let current = rootReal;
  for (const [index, component] of path.split("/").entries()) {
    current = resolve(current, component);
    try {
      const stat = lstatSync(current);
      if (stat.isSymbolicLink()) {
        throw stateError(
          "G_CONTRACT_PHASE_PATH_INVALID",
          `/${path}`,
          "Symlinks are forbidden in phase-state paths.",
        );
      }
      if (index < path.split("/").length - 1 && !stat.isDirectory()) {
        throw stateError(
          "G_CONTRACT_PHASE_PATH_INVALID",
          `/${path}`,
          "A phase-state parent component is not a directory.",
        );
      }
    } catch (error) {
      if (allowMissing && error instanceof Error && "code" in error && error.code === "ENOENT") {
        return absolute;
      }
      throw error;
    }
  }
  return absolute;
}

function gitDiff(root: string, from: string, to: string): Buffer {
  return gitBytes(root, [
    "diff",
    "--no-color",
    "--no-ext-diff",
    "--no-textconv",
    "--binary",
    "--no-renames",
    from,
    to,
  ]);
}

function gitText(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_ARGS, ...args], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function gitBytes(root: string, args: readonly string[]): Buffer {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_ARGS, ...args], {
    cwd: root,
    env: FIXED_GIT_ENV,
    maxBuffer: 128 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function fileDigest(bytes: Uint8Array): Digest {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  return Buffer.from(canonicalizeJson(left)).equals(Buffer.from(canonicalizeJson(right)));
}

function partial(message: string): never {
  throw stateError("G_CONTRACT_PHASE_PARTIAL_STATE", "/topology", message);
}

function bindingDrift(message: string): never {
  throw stateError("G_CONTRACT_PHASE_BINDING_DRIFT", "/binding", message);
}

function lineageInvalid(path: string, message: string): never {
  throw stateError("G_CONTRACT_PHASE_GIT_LINEAGE_INVALID", `/${path}`, message);
}

function selfReview(path: string): never {
  throw stateError(
    "G_CONTRACT_PHASE_SELF_REVIEW",
    `/${path}`,
    "Candidate actor and independent reviewer identities must differ.",
  );
}

function stateError(
  code: GContractPhaseStateError["code"],
  path: string,
  message: string,
): GContractPhaseStateError {
  return new GContractPhaseStateError(code, path, message);
}
