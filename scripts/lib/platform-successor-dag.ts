import { lstatSync, realpathSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

export const SUCCESSOR_GENERATION_LOCK_PATH = "contracts/generation.lock.json";

export const SUCCESSOR_REPLAY_RECEIPT_PATHS = [
  "tools/generator-supply/v2/evidence/replay.json",
  "tools/generator-supply/v2/evidence/replay/darwin-a.json",
  "tools/generator-supply/v2/evidence/replay/darwin-b.json",
  "tools/generator-supply/v2/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v2/evidence/replay/linux-a.json",
  "tools/generator-supply/v2/evidence/replay/linux-b.json",
  "tools/generator-supply/v2/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v2/evidence/replay/projection.json",
] as const;

export const SUCCESSOR_ASSEMBLY_PATHS = [
  "tools/generator-supply/v2/evidence-manifest.json",
  "tools/generator-supply/v2/profile.json",
] as const;

export const SUCCESSOR_ASSEMBLED_REVIEW_PATHS = [
  "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md",
  "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md",
] as const;

export const SUCCESSOR_BINDING_LATE_PATHS = [
  "tools/contract-review-binding/v1/review-tuple.json",
  "tools/contract-review-binding/v1/registry.json",
  "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md",
] as const;

export const SUCCESSOR_PROJECTION_EXCLUSIONS = [
  SUCCESSOR_GENERATION_LOCK_PATH,
  SUCCESSOR_ASSEMBLY_PATHS[0],
  SUCCESSOR_ASSEMBLY_PATHS[1],
  ...SUCCESSOR_REPLAY_RECEIPT_PATHS,
  ...SUCCESSOR_ASSEMBLED_REVIEW_PATHS,
  ...SUCCESSOR_BINDING_LATE_PATHS,
] as const;

export const SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS = [
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json",
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json",
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json",
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
  "tools/generator-supply/v2/source.json",
  "tools/generator-supply/v2/generator-supply-profile-source-v2.schema.json",
  "tools/generator-supply/v2/generator-supply-profile-v2.schema.json",
  "tools/contract-review-binding/v1/source.json",
  "tools/contract-review-binding/v1/review-binding-source-v1.schema.json",
  "tools/contract-review-binding/v1/review-tuple-v1.schema.json",
  "tools/contract-review-binding/v1/review-binding-registry-v1.schema.json",
  "scripts/generate-platform-contract-review-binding.ts",
  "scripts/lib/platform-contract-review-binding.ts",
  "scripts/lib/platform-contract-review-binding.test.ts",
  "scripts/lib/platform-contract-closure-profile-v3.ts",
  "scripts/lib/platform-contract-closure-profile-v3.test.ts",
  "scripts/lib/platform-generator-supply-profile-v2.ts",
  "scripts/lib/platform-generator-supply-profile-v2.test.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-successor-dag.ts",
  "scripts/lib/platform-successor-predecessor.ts",
] as const;

const FORBIDDEN_V1_EXCLUSIONS = [
  "tools/generator-supply/v1/evidence-manifest.json",
  "tools/generator-supply/v1/profile.json",
  "tools/generator-supply/v1/evidence/replay.json",
  "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
] as const;

export type SuccessorLateBoundState =
  | "PRE_REPLAY_ABSENT"
  | "REPLAY_RECEIPTS_PRESENT_UNVERIFIED"
  | "ASSEMBLY_PRESENT_UNVERIFIED"
  | "ASSEMBLED_REVIEWS_PRESENT_UNVERIFIED"
  | "BINDING_TUPLE_PRESENT_UNVERIFIED"
  | "BINDING_OUTPUT_PRESENT_UNVERIFIED"
  | "FINAL_REVIEW_PRESENT_UNVERIFIED";

export class SuccessorDagError extends Error {
  constructor(
    readonly code: "SUCCESSOR_DAG_INVALID" | "SUCCESSOR_LATE_STATE_PARTIAL",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "SuccessorDagError";
  }
}

export function assertSuccessorDagAuthority(): void {
  assertExactSuccessorProjectionExclusions(SUCCESSOR_PROJECTION_EXCLUSIONS);
}

export function assertExactSuccessorProjectionExclusions(paths: readonly string[]): void {
  if (
    paths.length !== SUCCESSOR_PROJECTION_EXCLUSIONS.length ||
    paths.some((path, index) => path !== SUCCESSOR_PROJECTION_EXCLUSIONS[index])
  ) {
    throw dagError(
      "SUCCESSOR_DAG_INVALID",
      "/projectionExclusions",
      "Successor projection exclusions must match the exact 16-path authority and order.",
    );
  }
  const unique = new Set(paths);
  if (unique.size !== paths.length) {
    throw dagError(
      "SUCCESSOR_DAG_INVALID",
      "/projectionExclusions",
      "Successor projection exclusions must be unique.",
    );
  }
  const preReplay = new Set<string>(SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS);
  for (const path of paths) {
    assertCanonicalFilePath(path);
    if ([..."*?[]{}"].some((character) => path.includes(character)) || preReplay.has(path)) {
      throw dagError(
        "SUCCESSOR_DAG_INVALID",
        path,
        "Projection exclusions forbid wildcards and all pre-replay semantic authority paths.",
      );
    }
  }
  for (const path of paths) {
    if (path.startsWith("tools/generator-supply/v1/")) {
      throw dagError(
        "SUCCESSOR_DAG_INVALID",
        path,
        "Generator-supply v1 evidence is an immutable predecessor input, not an exclusion.",
      );
    }
  }
  for (const path of FORBIDDEN_V1_EXCLUSIONS) {
    if (unique.has(path)) {
      throw dagError(
        "SUCCESSOR_DAG_INVALID",
        path,
        "Generator-supply v1 review authority is an immutable predecessor input.",
      );
    }
  }
}

/**
 * Classifies path topology only. Every non-absent state is intentionally
 * UNVERIFIED; v3, v2, binding, and independent-review validators own semantics.
 */
export function inspectSuccessorLateBoundTopology(root: string): SuccessorLateBoundState {
  assertSuccessorDagAuthority();
  const replay = presence(root, SUCCESSOR_REPLAY_RECEIPT_PATHS);
  const assembly = presence(root, SUCCESSOR_ASSEMBLY_PATHS);
  const reviews = presence(root, SUCCESSOR_ASSEMBLED_REVIEW_PATHS);
  const tuple = filePresence(root, SUCCESSOR_BINDING_LATE_PATHS[0]);
  const output = filePresence(root, SUCCESSOR_BINDING_LATE_PATHS[1]);
  const finalReview = filePresence(root, SUCCESSOR_BINDING_LATE_PATHS[2]);

  if (
    replay === "NONE" &&
    assembly === "NONE" &&
    reviews === "NONE" &&
    !tuple &&
    !output &&
    !finalReview
  ) {
    return "PRE_REPLAY_ABSENT";
  }
  if (replay !== "ALL") {
    throw partial("/replay", "Replay receipts must be either all absent or all present.");
  }
  if (assembly === "NONE" && reviews === "NONE" && !tuple && !output && !finalReview) {
    return "REPLAY_RECEIPTS_PRESENT_UNVERIFIED";
  }
  if (assembly !== "ALL") {
    throw partial("/assembly", "Supply-v2 manifest and profile must be complete together.");
  }
  if (reviews === "NONE" && !tuple && !output && !finalReview) {
    return "ASSEMBLY_PRESENT_UNVERIFIED";
  }
  if (reviews !== "ALL") {
    throw partial("/reviews", "Closure-v3 and supply-v2 reviews must be complete together.");
  }
  if (!tuple && !output && !finalReview) return "ASSEMBLED_REVIEWS_PRESENT_UNVERIFIED";
  if (tuple && !output && !finalReview) return "BINDING_TUPLE_PRESENT_UNVERIFIED";
  if (!tuple && output) {
    throw partial("/binding", "Detached binding output cannot exist without its complete tuple.");
  }
  if (tuple && output && !finalReview) return "BINDING_OUTPUT_PRESENT_UNVERIFIED";
  if (tuple && output && finalReview) return "FINAL_REVIEW_PRESENT_UNVERIFIED";
  throw partial(
    "/binding",
    "Detached binding tuple, output, and final review are in a partial state.",
  );
}

function presence(root: string, paths: readonly string[]): "NONE" | "ALL" {
  const present = paths.filter((path) => filePresence(root, path)).length;
  if (present === 0) return "NONE";
  if (present === paths.length) return "ALL";
  throw partial("/" + paths[0], "Late-bound path group is only partially present.");
}

function filePresence(root: string, path: string): boolean {
  const rootReal = realpathSync(root);
  const components = path.split("/");
  const absolute = resolve(rootReal, ...components);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(".." + sep) ||
    isAbsolute(relation)
  ) {
    throw partial(path, "Late-bound artifact escapes the repository root.");
  }
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    const final = index === components.length - 1;
    try {
      const stats = lstatSync(current);
      if (
        stats.isSymbolicLink() ||
        (!final && !stats.isDirectory()) ||
        (final && !stats.isFile())
      ) {
        throw partial(path, "Late-bound paths require real directories and regular files.");
      }
    } catch (error) {
      if (error instanceof SuccessorDagError) throw error;
      if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
      throw partial(path, "Late-bound artifact cannot be inspected.");
    }
  }
  if (realpathSync(absolute) !== absolute) {
    throw partial(path, "Late-bound artifact resolves through a symbolic link.");
  }
  return true;
}

function assertCanonicalFilePath(path: string): void {
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path
      .split("/")
      .some((segment) => segment.length === 0 || segment === "." || segment === "..") ||
    !/\.[A-Za-z0-9]+$/u.test(path)
  ) {
    throw dagError(
      "SUCCESSOR_DAG_INVALID",
      path,
      "Successor late-bound authority requires canonical repository-relative file paths.",
    );
  }
}

function partial(path: string, message: string): SuccessorDagError {
  return dagError("SUCCESSOR_LATE_STATE_PARTIAL", path, message);
}

function dagError(
  code: SuccessorDagError["code"],
  path: string,
  message: string,
): SuccessorDagError {
  return new SuccessorDagError(code, path, message);
}
