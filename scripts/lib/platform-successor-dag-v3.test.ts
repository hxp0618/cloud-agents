import { mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertExactSuccessorV3ProjectionExclusions,
  assertSuccessorV3CoreGeneratorOutputAuthority,
  assertSuccessorV3CoreGeneratorOutputsPresent,
  assertSuccessorV3DagAuthority,
  inspectSuccessorV3LateBoundTopology,
  SUCCESSOR_V3_ASSEMBLY_PATHS,
  SUCCESSOR_V3_BINDING_PATHS,
  SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_V3_FINAL_REVIEW_PATH,
  SUCCESSOR_V3_GENERATION_LOCK_PATH,
  SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
  SUCCESSOR_V3_R5_PATH,
  SUCCESSOR_V3_R5_REVIEW_PATH,
  SUCCESSOR_V3_REPLAY_RECEIPT_PATHS,
  SUCCESSOR_V3_SUPPLY_REVIEW_PATH,
  type SuccessorV3DagError,
} from "./platform-successor-dag-v3";

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const temporaryRoot of temporaryRoots.splice(0)) {
    rmSync(temporaryRoot, { force: true, recursive: true });
  }
});

function createRoot(): string {
  const temporaryRoot = mkdtempSync(resolve(tmpdir(), "successor-v3-dag-"));
  temporaryRoots.push(temporaryRoot);
  touch(temporaryRoot, SUCCESSOR_V3_GENERATION_LOCK_PATH);
  return temporaryRoot;
}

function touch(root: string, repositoryPath: string): void {
  mkdirSync(dirname(resolve(root, repositoryPath)), { recursive: true });
  writeFileSync(resolve(root, repositoryPath), repositoryPath + "\n");
}

describe("successor v3 DAG", () => {
  it("fixes exact17 and keeps every named pre-replay authority included", () => {
    expect(SUCCESSOR_V3_PROJECTION_EXCLUSIONS).toHaveLength(17);
    expect(SUCCESSOR_V3_PROJECTION_EXCLUSIONS).toEqual([
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
    ]);
    expect(() => assertSuccessorV3DagAuthority()).not.toThrow();
    expect(SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS).toEqual(
      expect.arrayContaining([
        "tools/gate-phase-record/g-contract-p1/v1/source.json",
        "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-source-v1.schema.json",
        "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-model-v1.schema.json",
        "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-review-tuple-v1.schema.json",
        "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-binding-registry-v1.schema.json",
        "scripts/lib/platform-g-contract-phase-record.ts",
        "scripts/lib/platform-g-contract-phase-record.test.ts",
      ]),
    );
    for (const repositoryPath of SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS) {
      expect(SUCCESSOR_V3_PROJECTION_EXCLUSIONS).not.toContain(repositoryPath);
    }
  });

  it("rejects an extra, missing, reordered, wildcard, alias, or pre-replay exclusion", () => {
    const mutations: readonly (readonly string[])[] = [
      SUCCESSOR_V3_PROJECTION_EXCLUSIONS.slice(0, -1),
      [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS, "late/extra.json"],
      [...SUCCESSOR_V3_PROJECTION_EXCLUSIONS].reverse(),
      SUCCESSOR_V3_PROJECTION_EXCLUSIONS.map((candidate, index) =>
        index === 3 ? "tools/generator-supply/v3/evidence/replay/*.json" : candidate,
      ),
      SUCCESSOR_V3_PROJECTION_EXCLUSIONS.map((candidate, index) =>
        index === 3 ? "tools/generator-supply/v3/evidence/../evidence/replay.json" : candidate,
      ),
      SUCCESSOR_V3_PROJECTION_EXCLUSIONS.map((candidate, index) =>
        index === 3 ? SUCCESSOR_V3_PRE_REPLAY_AUTHORITY_PATHS[0] : candidate,
      ),
    ];
    for (const mutation of mutations) {
      expect(() => assertExactSuccessorV3ProjectionExclusions(mutation)).toThrowError(
        expect.objectContaining<Partial<SuccessorV3DagError>>({
          code: "SUCCESSOR_V3_DAG_INVALID",
        }),
      );
    }
  });

  it("reuses exactly the sorted 49-path core set without late outputs", () => {
    expect(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS).toHaveLength(49);
    expect(new Set(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS).size).toBe(49);
    expect([...SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS].toSorted()).toEqual(
      SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS,
    );
    for (const repositoryPath of SUCCESSOR_V3_PROJECTION_EXCLUSIONS) {
      expect(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS).not.toContain(repositoryPath);
    }
    expect(() =>
      assertSuccessorV3CoreGeneratorOutputAuthority(SUCCESSOR_V3_CORE_GENERATOR_OUTPUT_PATHS),
    ).not.toThrow();
    expect(() =>
      assertSuccessorV3CoreGeneratorOutputsPresent(resolve(import.meta.dirname, "../..")),
    ).not.toThrow();
  });

  it("classifies the complete acyclic presence topology", () => {
    const root = createRoot();
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("PRE_REPLAY_LEGACY_LOCK_ONLY");
    for (const repositoryPath of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS) touch(root, repositoryPath);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("RAW_RECEIPTS_PRESENT_UNVERIFIED");
    for (const repositoryPath of SUCCESSOR_V3_ASSEMBLY_PATHS) touch(root, repositoryPath);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("ASSEMBLY_PRESENT_UNVERIFIED");
    touch(root, SUCCESSOR_V3_SUPPLY_REVIEW_PATH);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("SUPPLY_REVIEW_PRESENT_UNVERIFIED");
    touch(root, SUCCESSOR_V3_R5_PATH);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("R5_PRESENT_UNVERIFIED");
    touch(root, SUCCESSOR_V3_R5_REVIEW_PATH);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("R5_REVIEW_PRESENT_UNVERIFIED");
    for (const repositoryPath of SUCCESSOR_V3_BINDING_PATHS) touch(root, repositoryPath);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("PHASE_BINDING_PRESENT_UNVERIFIED");
    touch(root, SUCCESSOR_V3_FINAL_REVIEW_PATH);
    expect(inspectSuccessorV3LateBoundTopology(root)).toBe("FINAL_REVIEW_PRESENT_UNVERIFIED");
  });

  it("fails closed for every partial group, out-of-order artifact, and symlink", () => {
    const partialReplay = createRoot();
    touch(partialReplay, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]);
    expect(() => inspectSuccessorV3LateBoundTopology(partialReplay)).toThrowError(
      expect.objectContaining({ code: "SUCCESSOR_V3_LATE_STATE_PARTIAL" }),
    );

    const partialAssembly = createRoot();
    for (const repositoryPath of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS) {
      touch(partialAssembly, repositoryPath);
    }
    touch(partialAssembly, SUCCESSOR_V3_ASSEMBLY_PATHS[0]);
    expect(() => inspectSuccessorV3LateBoundTopology(partialAssembly)).toThrowError(
      expect.objectContaining({ code: "SUCCESSOR_V3_LATE_STATE_PARTIAL" }),
    );

    const orphanReview = createRoot();
    touch(orphanReview, SUCCESSOR_V3_R5_REVIEW_PATH);
    expect(() => inspectSuccessorV3LateBoundTopology(orphanReview)).toThrowError(
      expect.objectContaining({ code: "SUCCESSOR_V3_LATE_STATE_PARTIAL" }),
    );

    const partialBinding = createRoot();
    for (const repositoryPath of SUCCESSOR_V3_REPLAY_RECEIPT_PATHS)
      touch(partialBinding, repositoryPath);
    for (const repositoryPath of SUCCESSOR_V3_ASSEMBLY_PATHS) touch(partialBinding, repositoryPath);
    touch(partialBinding, SUCCESSOR_V3_SUPPLY_REVIEW_PATH);
    touch(partialBinding, SUCCESSOR_V3_R5_PATH);
    touch(partialBinding, SUCCESSOR_V3_R5_REVIEW_PATH);
    touch(partialBinding, SUCCESSOR_V3_BINDING_PATHS[0]);
    expect(() => inspectSuccessorV3LateBoundTopology(partialBinding)).toThrowError(
      expect.objectContaining({ code: "SUCCESSOR_V3_LATE_STATE_PARTIAL" }),
    );

    const symlinkRoot = createRoot();
    mkdirSync(dirname(resolve(symlinkRoot, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0])), {
      recursive: true,
    });
    symlinkSync(
      resolve(symlinkRoot, SUCCESSOR_V3_GENERATION_LOCK_PATH),
      resolve(symlinkRoot, SUCCESSOR_V3_REPLAY_RECEIPT_PATHS[0]),
    );
    expect(() => inspectSuccessorV3LateBoundTopology(symlinkRoot)).toThrowError(
      expect.objectContaining({ code: "SUCCESSOR_V3_LATE_STATE_PARTIAL" }),
    );
  });
});
