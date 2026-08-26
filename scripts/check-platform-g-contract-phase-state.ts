/** Read-only G-CONTRACT/P1 phase-state checker.  It has no write mode. */
import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
} from "node:fs";
import { relative, resolve, sep } from "node:path";

import {
  classifyGContractPhaseTopology,
  inspectGContractPhaseState,
} from "./lib/platform-g-contract-phase-state";
import {
  buildGContractPhaseReviewTuple,
  type GContractPhaseRecordBuildInput,
  type ReviewGitBinding,
  type ReviewBinding,
} from "./lib/platform-g-contract-phase-record";

const root = resolve(import.meta.dirname, "..");

function usage(): never {
  throw new Error(
    "Usage: bun scripts/check-platform-g-contract-phase-state.ts --check | --check-record <build-input.json> | --check-terminal <build-input.json> <tuple.json> [terminal-review-input.json]",
  );
}

function readObject(path: string): Record<string, unknown> {
  const absolute = safeRepositoryPath(path);
  const pathBefore = lstatSync(absolute, { bigint: true });
  if (!pathBefore.isFile() || pathBefore.isSymbolicLink()) {
    throw new Error(`Input JSON must be a regular non-symlink file: ${path}`);
  }
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
  let bytes: Buffer | undefined;
  try {
    const before = fstatSync(descriptor, { bigint: true });
    if (!sameIdentity(pathBefore, before))
      throw new Error(`Input JSON changed before read: ${path}`);
    bytes = readFileSync(descriptor);
    const after = fstatSync(descriptor, { bigint: true });
    const pathAfter = lstatSync(absolute, { bigint: true });
    if (!sameIdentity(before, after) || !sameIdentity(before, pathAfter)) {
      throw new Error(`Input JSON changed during read: ${path}`);
    }
  } finally {
    closeSync(descriptor);
  }
  if (bytes === undefined) throw new Error(`Input JSON could not be read: ${path}`);
  const value: unknown = JSON.parse(bytes.toString("utf8"));
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path} must be a JSON object.`);
  }
  return value as Record<string, unknown>;
}

function sameIdentity(
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

function safeRepositoryPath(path: string): string {
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    relation.startsWith(sep) ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..")
  )
    throw new Error(`Input path must be canonical and contained: ${path}`);
  let current = rootReal;
  for (const [index, component] of relation.split(sep).entries()) {
    current = resolve(current, component);
    const stat = lstatSync(current);
    const final = index === relation.split(sep).length - 1;
    if (stat.isSymbolicLink() || (!final && !stat.isDirectory()) || (final && !stat.isFile()))
      throw new Error(`Input path must be a regular non-symlink file: ${path}`);
  }
  return absolute;
}

function buildInput(path: string): GContractPhaseRecordBuildInput {
  return readObject(path) as unknown as GContractPhaseRecordBuildInput;
}

function tuple(path: string) {
  const value = readObject(path);
  if (!Array.isArray(value.reviews) || value.reviews.length !== 2) {
    throw new Error("Tuple must contain exactly two reviews.");
  }
  const rebuilt = buildGContractPhaseReviewTuple(
    root,
    value.reviews as [ReviewBinding, ReviewBinding],
  );
  if (value.tupleDigest !== undefined && value.tupleDigest !== rebuilt.tupleDigest) {
    throw new Error("Tuple digest is stale.");
  }
  return rebuilt;
}

function terminalReviewInput(path: string): Readonly<{
  bindingActorId: string;
  expectedTerminalReview: ReviewGitBinding;
}> {
  const value = readObject(path);
  const expectedKeys = ["bindingActorId", "expectedTerminalReview"];
  if (!sameKeys(Object.keys(value), expectedKeys)) {
    throw new Error(`${path} must contain exactly bindingActorId and expectedTerminalReview.`);
  }
  if (typeof value.bindingActorId !== "string" || value.bindingActorId.length === 0) {
    throw new Error(`${path}.bindingActorId must be a non-empty string.`);
  }
  const review = value.expectedTerminalReview;
  if (typeof review !== "object" || review === null || Array.isArray(review)) {
    throw new Error(`${path}.expectedTerminalReview must be an object.`);
  }
  const reviewRecord = review as Record<string, unknown>;
  const reviewKeys = [
    "reviewerId",
    "commit",
    "tree",
    "parent",
    "path",
    "gitBlob",
    "sha256",
    "sizeBytes",
    "mode",
    "diffSha256",
    "verdict",
    "findings",
  ];
  if (!sameKeys(Object.keys(reviewRecord), reviewKeys)) {
    throw new Error(`${path}.expectedTerminalReview has an unexpected or missing field.`);
  }
  if (
    typeof reviewRecord.reviewerId !== "string" ||
    reviewRecord.reviewerId.length === 0 ||
    typeof reviewRecord.commit !== "string" ||
    typeof reviewRecord.tree !== "string" ||
    typeof reviewRecord.parent !== "string" ||
    typeof reviewRecord.path !== "string" ||
    typeof reviewRecord.gitBlob !== "string" ||
    typeof reviewRecord.sha256 !== "string" ||
    typeof reviewRecord.diffSha256 !== "string" ||
    reviewRecord.mode !== "100644" ||
    reviewRecord.verdict !== "APPROVE_P0_0_P1_0_P2_0" ||
    typeof reviewRecord.sizeBytes !== "number" ||
    !Number.isInteger(reviewRecord.sizeBytes) ||
    reviewRecord.sizeBytes < 1
  ) {
    throw new Error(`${path}.expectedTerminalReview has invalid identity fields.`);
  }
  const findings = reviewRecord.findings;
  if (
    typeof findings !== "object" ||
    findings === null ||
    Array.isArray(findings) ||
    !sameKeys(Object.keys(findings), ["p0", "p1", "p2"]) ||
    (findings as Record<string, unknown>).p0 !== 0 ||
    (findings as Record<string, unknown>).p1 !== 0 ||
    (findings as Record<string, unknown>).p2 !== 0
  ) {
    throw new Error(`${path}.expectedTerminalReview.findings must be zero P0/P1/P2.`);
  }
  return {
    bindingActorId: value.bindingActorId,
    expectedTerminalReview: reviewRecord as unknown as ReviewGitBinding,
  };
}

function sameKeys(actual: readonly string[], expected: readonly string[]): boolean {
  return actual.length === expected.length && actual.every((key, index) => key === expected[index]);
}

const args = process.argv.slice(2);
const mode = args.shift();
if (mode === "--check" && args.length === 0) {
  process.stdout.write(`g-contract-phase-state: ${classifyGContractPhaseTopology(root)}\n`);
} else if (mode === "--check-record" && args.length === 1) {
  const state = inspectGContractPhaseState(root, { recordBuildInput: buildInput(args[0]!) });
  process.stdout.write(`g-contract-phase-state: ${state}\n`);
} else if (mode === "--check-terminal" && (args.length === 2 || args.length === 3)) {
  const expectedTuple = tuple(args[1]!);
  const terminal = args.length === 3 ? terminalReviewInput(args[2]!) : undefined;
  const state = inspectGContractPhaseState(root, {
    recordBuildInput: buildInput(args[0]!),
    expectedTuple,
    ...(terminal === undefined
      ? {}
      : {
          bindingActorId: terminal.bindingActorId,
          expectedTerminalReview: terminal.expectedTerminalReview,
        }),
  });
  process.stdout.write(`g-contract-phase-state: ${state}\n`);
} else {
  usage();
}
