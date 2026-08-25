import { lstatSync, realpathSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import { SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS } from "./platform-successor-dag";

export const SUCCESSOR_V3_GENERATION_LOCK_PATH = "contracts/generation.lock.json";

export const SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS = SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS;

export const SUCCESSOR_V3_REPLAY_RECEIPT_PATHS = [
  "tools/generator-supply/v3/evidence/replay.json",
  "tools/generator-supply/v3/evidence/replay/darwin-a.json",
  "tools/generator-supply/v3/evidence/replay/darwin-b.json",
  "tools/generator-supply/v3/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v3/evidence/replay/linux-a.json",
  "tools/generator-supply/v3/evidence/replay/linux-b.json",
  "tools/generator-supply/v3/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v3/evidence/replay/projection.json",
] as const;

export const SUCCESSOR_V3_ASSEMBLY_PATHS = [
  "tools/generator-supply/v3/evidence-manifest.json",
  "tools/generator-supply/v3/profile.json",
] as const;

export const SUCCESSOR_V3_SUPPLY_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md";

export const SUCCESSOR_V3_R5_PATH =
  "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md";

export const SUCCESSOR_V3_R5_REVIEW_PATH =
  "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md";

export const SUCCESSOR_V3_BINDING_PATHS = [
  "tools/gate-phase-record/g-contract-p1/v1/review-tuple.json",
  "tools/gate-phase-record/g-contract-p1/v1/registry.json",
] as const;

export const SUCCESSOR_V3_FINAL_REVIEW_PATH =
  "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md";

/**
 * D-053 exact17 is deliberately ordered by the state machine rather than by
 * path. It is the only authorized projection exclusion set.
 */
export const SUCCESSOR_V3_PROJECTION_EXCLUSIONS = [
  SUCCESSOR_V3_GENERATION_LOCK_PATH,
  ...SUCCESSOR_V3_ASSEMBLY_PATHS,
  ...SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
  SUCCESSOR_V3_SUPPLY_REVIEW_PATH,
  SUCCESSOR_V3_R5_PATH,
  SUCCESSOR_V3_R5_REVIEW_PATH,
  ...SUCCESSOR_V3_BINDING_PATHS,
  SUCCESSOR_V3_FINAL_REVIEW_PATH,
] as const;

/**
 * These are semantic inputs, never late-bound outputs. The list is not a
 * discovery mechanism: exact17 equality already rejects every other
 * exclusion. It provides a second, explicit self-reference guard for the
 * authorities named by ADR-0030.
 */
export const SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS = [
  "docs/plan/adr/0030-p1-g-contract-current-source-phase-successor.md",
  "docs/plan/p1/g-contract-post-h-current-source-successor-entry-audit-20260825.md",
  "docs/plan/p1/g-contract-current-source-phase-successor-design-independent-review-20260825.md",
  "docs/plan/p1/g-contract-current-source-phase-successor-repair-independent-review-20260825.md",
  "tools/generator-supply/v3/source.json",
  "tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json",
  "tools/generator-supply/v3/generator-supply-profile-v3.schema.json",
  "scripts/lib/platform-successor-dag-v3.ts",
  "scripts/lib/platform-successor-dag-v3.test.ts",
  "scripts/lib/platform-successor-predecessor-v3.ts",
  "scripts/lib/platform-successor-predecessor-v3.test.ts",
  "scripts/lib/platform-generator-supply-replay-v3.ts",
  "scripts/lib/platform-generator-supply-replay-v3.test.ts",
  "scripts/lib/platform-generator-supply-profile-v3.ts",
  "scripts/lib/platform-generator-supply-profile-v3.test.ts",
  "scripts/generate-platform-generator-supply-profile-v3.ts",
  "scripts/replay-platform-generators-isolated-v3.sh",
  "scripts/lib/platform-contract-lock-v3.ts",
  "scripts/lib/platform-contract-lock-v3.test.ts",
  "scripts/generate-platform-contract-lock-v3.ts",
  "scripts/lib/platform-g-contract-phase-state.ts",
  "scripts/lib/platform-g-contract-phase-state.test.ts",
  "scripts/check-platform-g-contract-phase-state.ts",
  "scripts/generate-platform-g-contract-phase-record.ts",
] as const;

export type SuccessorV3LateBoundState =
  | "PRE_REPLAY_LEGACY_LOCK_ONLY"
  | "RAW_RECEIPTS_PRESENT_UNVERIFIED"
  | "ASSEMBLY_PRESENT_UNVERIFIED"
  | "SUPPLY_REVIEW_PRESENT_UNVERIFIED"
  | "R5_PRESENT_UNVERIFIED"
  | "R5_REVIEW_PRESENT_UNVERIFIED"
  | "PHASE_BINDING_PRESENT_UNVERIFIED"
  | "FINAL_REVIEW_PRESENT_UNVERIFIED";

export class SuccessorV3DagError extends Error {
  constructor(
    readonly code: "SUCCESSOR_V3_DAG_INVALID" | "SUCCESSOR_V3_LATE_STATE_PARTIAL",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "SuccessorV3DagError";
  }
}

export function assertSuccessorV3DagAuthority(): void {
  assertExactSuccessorV3ProjectionExclusions(SUCCESSOR_V3_PROJECTION_EXCLUSIONS);
  assertSuccessorV3CoreGeneratorOutputAuthority(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS);
}

export function assertExactSuccessorV3ProjectionExclusions(paths: readonly string[]): void {
  if (
    paths.length !== SUCCESSOR_V3_PROJECTION_EXCLUSIONS.length ||
    paths.some((candidate, index) => candidate !== SUCCESSOR_V3_PROJECTION_EXCLUSIONS[index])
  ) {
    throw invalid(
      "/projectionExclusions",
      "Projection exclusions must equal the exact ordered D-053 17-path authority.",
    );
  }
  if (paths.length !== 17 || new Set(paths).size !== 17) {
    throw invalid("/projectionExclusions", "Projection exclusions must contain 17 unique paths.");
  }
  const preReplayPaths = new Set<string>(SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS);
  for (const candidate of paths) {
    assertCanonicalFilePath(candidate);
    if ([..."*?[]{}"].some((character) => candidate.includes(character))) {
      throw invalid(candidate, "Projection exclusions cannot contain a wildcard.");
    }
    if (preReplayPaths.has(candidate)) {
      throw invalid(candidate, "A pre-replay authority path cannot be excluded.");
    }
    if (
      candidate.startsWith("tools/generator-supply/v1/") ||
      candidate.startsWith("tools/generator-supply/v2/") ||
      candidate.startsWith("tools/contract-review-binding/v1/")
    ) {
      throw invalid(candidate, "An immutable predecessor cannot be a v3 exclusion.");
    }
  }
}

export function assertSuccessorV3CoreGeneratorOutputAuthority(paths: readonly string[]): void {
  if (
    paths.length !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS.length ||
    paths.some((candidate, index) => candidate !== SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS[index])
  ) {
    throw invalid(
      "/coreGeneratorOutputs",
      "The v3 core output set must reuse the exact ordered v2 49-path authority.",
    );
  }
  if (paths.length !== 49 || new Set(paths).size !== 49) {
    throw invalid("/coreGeneratorOutputs", "The core output authority must contain 49 paths.");
  }
  const sorted = [...paths].toSorted(bytewiseCompare);
  if (paths.some((candidate, index) => candidate !== sorted[index])) {
    throw invalid("/coreGeneratorOutputs", "Core paths must use UTF-8 bytewise order.");
  }
  const late = new Set<string>(SUCCESSOR_V3_PROJECTION_EXCLUSIONS);
  for (const candidate of paths) {
    assertCanonicalFilePath(candidate);
    if (late.has(candidate)) {
      throw invalid(candidate, "A late-bound output cannot enter the core generator output set.");
    }
  }
}

export function assertSuccessorV3CoreGeneratorOutputsPresent(root: string): void {
  assertSuccessorV3CoreGeneratorOutputAuthority(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS);
  for (const candidate of SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS) {
    if (!safeRegularFilePresence(root, candidate)) {
      throw invalid(candidate, "A fixed core generator output is absent.");
    }
  }
}

/**
 * Presence is topology only. Every non-initial state remains UNVERIFIED until
 * its owning semantic checker reproduces bytes, Git lineage, and review facts.
 */
export function inspectSuccessorV3LateBoundTopology(root: string): SuccessorV3LateBoundState {
  assertSuccessorV3DagAuthority();
  if (!safeRegularFilePresence(root, SUCCESSOR_V3_GENERATION_LOCK_PATH)) {
    throw partial(SUCCESSOR_V3_GENERATION_LOCK_PATH, "The single live lock must remain present.");
  }

  const replay = groupPresence(root, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS);
  const assembly = groupPresence(root, SUCCESSOR_V3_ASSEMBLY_PATHS);
  const supplyReview = safeRegularFilePresence(root, SUCCESSOR_V3_SUPPLY_REVIEW_PATH);
  const r5 = safeRegularFilePresence(root, SUCCESSOR_V3_R5_PATH);
  const r5Review = safeRegularFilePresence(root, SUCCESSOR_V3_R5_REVIEW_PATH);
  const binding = groupPresence(root, SUCCESSOR_V3_BINDING_PATHS);
  const finalReview = safeRegularFilePresence(root, SUCCESSOR_V3_FINAL_REVIEW_PATH);

  if (replay === "PARTIAL") throw partial("/replay", "Replay receipts are partial.");
  if (assembly === "PARTIAL") throw partial("/assembly", "Supply assembly is partial.");
  if (binding === "PARTIAL") throw partial("/binding", "Phase binding is partial.");

  const laterThanReplay =
    assembly !== "NONE" || supplyReview || r5 || r5Review || binding !== "NONE" || finalReview;
  if (replay === "NONE") {
    if (laterThanReplay) throw partial("/replay", "A later output exists before replay completes.");
    return "PRE_REPLAY_LEGACY_LOCK_ONLY";
  }

  const laterThanAssembly = supplyReview || r5 || r5Review || binding !== "NONE" || finalReview;
  if (assembly === "NONE") {
    if (laterThanAssembly) throw partial("/assembly", "A later output exists before assembly.");
    return "RAW_RECEIPTS_PRESENT_UNVERIFIED";
  }
  if (!supplyReview) {
    if (r5 || r5Review || binding !== "NONE" || finalReview) {
      throw partial("/supplyReview", "A later output exists before the supply review.");
    }
    return "ASSEMBLY_PRESENT_UNVERIFIED";
  }
  if (!r5) {
    if (r5Review || binding !== "NONE" || finalReview) {
      throw partial("/r5", "A later output exists before the R5 candidate.");
    }
    return "SUPPLY_REVIEW_PRESENT_UNVERIFIED";
  }
  if (!r5Review) {
    if (binding !== "NONE" || finalReview) {
      throw partial("/r5Review", "A later output exists before the R5 review.");
    }
    return "R5_PRESENT_UNVERIFIED";
  }
  if (binding === "NONE") {
    if (finalReview) throw partial("/binding", "Final review exists before phase binding.");
    return "R5_REVIEW_PRESENT_UNVERIFIED";
  }
  if (!finalReview) return "PHASE_BINDING_PRESENT_UNVERIFIED";
  return "FINAL_REVIEW_PRESENT_UNVERIFIED";
}

function groupPresence(root: string, paths: readonly string[]): "NONE" | "ALL" | "PARTIAL" {
  const count = paths.filter((candidate) => safeRegularFilePresence(root, candidate)).length;
  if (count === 0) return "NONE";
  if (count === paths.length) return "ALL";
  return "PARTIAL";
}

function safeRegularFilePresence(root: string, repositoryPath: string): boolean {
  const rootReal = realpathSync(root);
  const components = canonicalSegments(repositoryPath);
  const absolute = resolve(rootReal, ...components);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(".." + sep) ||
    isAbsolute(relation)
  ) {
    throw partial(repositoryPath, "Late-bound path escapes the repository root.");
  }
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    const final = index === components.length - 1;
    try {
      const metadata = lstatSync(current);
      if (
        metadata.isSymbolicLink() ||
        (!final && !metadata.isDirectory()) ||
        (final && !metadata.isFile())
      ) {
        throw partial(repositoryPath, "Late-bound paths must not contain aliases or symlinks.");
      }
    } catch (error) {
      if (error instanceof SuccessorV3DagError) throw error;
      if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
      throw partial(repositoryPath, "Late-bound path cannot be inspected.");
    }
  }
  if (realpathSync(absolute) !== absolute) {
    throw partial(repositoryPath, "Late-bound path resolves through an alias.");
  }
  return true;
}

function assertCanonicalFilePath(repositoryPath: string): void {
  canonicalSegments(repositoryPath);
  if (!/\.[A-Za-z0-9]+$/u.test(repositoryPath)) {
    throw invalid(repositoryPath, "Authority paths must name files.");
  }
}

function canonicalSegments(repositoryPath: string): string[] {
  if (
    repositoryPath.length === 0 ||
    isAbsolute(repositoryPath) ||
    repositoryPath.includes("\\") ||
    repositoryPath.includes(String.fromCharCode(0))
  ) {
    throw invalid(repositoryPath, "Authority path is not repository-relative and canonical.");
  }
  const components = repositoryPath.split("/");
  if (components.some((component) => component === "" || component === "." || component === "..")) {
    throw invalid(repositoryPath, "Authority path contains a normalization alias.");
  }
  return components;
}

function bytewiseCompare(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8"));
}

function invalid(path: string, message: string): SuccessorV3DagError {
  return new SuccessorV3DagError("SUCCESSOR_V3_DAG_INVALID", path, message);
}

function partial(path: string, message: string): SuccessorV3DagError {
  return new SuccessorV3DagError("SUCCESSOR_V3_LATE_STATE_PARTIAL", path, message);
}
