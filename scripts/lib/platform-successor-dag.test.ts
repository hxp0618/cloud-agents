import { mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertExactSuccessorProjectionExclusions,
  assertSuccessorCoreGeneratorOutputAuthority,
  assertSuccessorCoreGeneratorOutputsCurrent,
  assertSuccessorDagAuthority,
  inspectSuccessorLateBoundTopology,
  SUCCESSOR_ASSEMBLED_REVIEW_PATHS,
  SUCCESSOR_ASSEMBLY_PATHS,
  SUCCESSOR_BINDING_LATE_PATHS,
  SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
  SUCCESSOR_DERIVED_REPLAY_SUMMARY_PATH,
  SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_RAW_REPLAY_RECEIPT_PATHS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
  SuccessorDagError,
} from "./platform-successor-dag";

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function root(): string {
  const path = mkdtempSync(resolve(tmpdir(), "successor-dag-"));
  temporaryRoots.push(path);
  return path;
}

function touch(root: string, path: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  writeFileSync(resolve(root, path), path + "\n");
}

describe("successor DAG authority", () => {
  it("fixes 16 exact late paths and excludes no semantic source, schema, output, or v1 evidence", () => {
    const expected = [
      "contracts/generation.lock.json",
      "tools/generator-supply/v2/evidence-manifest.json",
      "tools/generator-supply/v2/profile.json",
      "tools/generator-supply/v2/evidence/replay.json",
      "tools/generator-supply/v2/evidence/replay/darwin-a.json",
      "tools/generator-supply/v2/evidence/replay/darwin-b.json",
      "tools/generator-supply/v2/evidence/replay/darwin-isolation.json",
      "tools/generator-supply/v2/evidence/replay/linux-a.json",
      "tools/generator-supply/v2/evidence/replay/linux-b.json",
      "tools/generator-supply/v2/evidence/replay/linux-isolation.json",
      "tools/generator-supply/v2/evidence/replay/projection.json",
      "docs/plan/p1/g-contract-closure-profile-v3-independent-review-20260824.md",
      "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md",
      "tools/contract-review-binding/v1/review-tuple.json",
      "tools/contract-review-binding/v1/registry.json",
      "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md",
    ] as const;
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).toHaveLength(16);
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).toEqual(expected);
    expect(SUCCESSOR_DERIVED_REPLAY_SUMMARY_PATH).toBe(expected[3]);
    expect(SUCCESSOR_RAW_REPLAY_RECEIPT_PATHS).toEqual(expected.slice(4, 11));
    expect(SUCCESSOR_REPLAY_RECEIPT_PATHS).toEqual([
      SUCCESSOR_DERIVED_REPLAY_SUMMARY_PATH,
      ...SUCCESSOR_RAW_REPLAY_RECEIPT_PATHS,
    ]);
    expect(() => assertSuccessorDagAuthority()).not.toThrow();
    for (const path of SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS) {
      expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain(path);
    }
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain(
      "tools/generator-supply/v1/evidence-manifest.json",
    );
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain("tools/generator-supply/v1/profile.json");
  });

  it("fixes one sorted unique 49-path core generator output authority", () => {
    expect(SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS).toHaveLength(49);
    expect(new Set(SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS).size).toBe(49);
    expect([...SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS].toSorted()).toEqual(
      SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS,
    );
    expect(SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS).toContain(
      "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
    );
    for (const path of [
      "contracts/generation.lock.json",
      ...SUCCESSOR_ASSEMBLY_PATHS,
      ...SUCCESSOR_REPLAY_RECEIPT_PATHS,
      SUCCESSOR_BINDING_LATE_PATHS[1],
    ]) {
      expect(SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS).not.toContain(path);
    }
    expect(() =>
      assertSuccessorCoreGeneratorOutputAuthority(SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS),
    ).not.toThrow();
    expect(() =>
      assertSuccessorCoreGeneratorOutputsCurrent(resolve(import.meta.dirname, "../..")),
    ).not.toThrow();
    expect(() =>
      assertSuccessorCoreGeneratorOutputAuthority([
        ...SUCCESSOR_CORE_GENERATOR_OUTPUT_PATHS.slice(0, -1),
        "tools/generator-supply/v2/profile.json",
      ]),
    ).toThrowError(expect.objectContaining({ code: "SUCCESSOR_DAG_INVALID" }));
  });

  it("rejects wildcard, extra, reordered, and pre-replay exclusions", () => {
    for (const paths of [
      [...SUCCESSOR_PROJECTION_EXCLUSIONS, "tools/generator-supply/v2/evidence/replay/*"],
      [...SUCCESSOR_PROJECTION_EXCLUSIONS].reverse(),
      SUCCESSOR_PROJECTION_EXCLUSIONS.map((path, index) =>
        index === 1 ? SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS[0] : path,
      ),
    ]) {
      expect(() => assertExactSuccessorProjectionExclusions(paths)).toThrowError(
        expect.objectContaining<Partial<SuccessorDagError>>({
          code: "SUCCESSOR_DAG_INVALID",
        }),
      );
    }
  });

  it("classifies the complete ordered late-bound lifecycle", () => {
    const path = root();
    expect(inspectSuccessorLateBoundTopology(path)).toBe("PRE_REPLAY_ABSENT");
    for (const receipt of SUCCESSOR_REPLAY_RECEIPT_PATHS) touch(path, receipt);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("REPLAY_RECEIPTS_PRESENT_UNVERIFIED");
    for (const output of SUCCESSOR_ASSEMBLY_PATHS) touch(path, output);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("ASSEMBLY_PRESENT_UNVERIFIED");
    for (const review of SUCCESSOR_ASSEMBLED_REVIEW_PATHS) touch(path, review);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("ASSEMBLED_REVIEWS_PRESENT_UNVERIFIED");
    touch(path, SUCCESSOR_BINDING_LATE_PATHS[0]);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("BINDING_TUPLE_PRESENT_UNVERIFIED");
    touch(path, SUCCESSOR_BINDING_LATE_PATHS[1]);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("BINDING_OUTPUT_PRESENT_UNVERIFIED");
    touch(path, SUCCESSOR_BINDING_LATE_PATHS[2]);
    expect(inspectSuccessorLateBoundTopology(path)).toBe("FINAL_REVIEW_PRESENT_UNVERIFIED");
  });

  it("fails closed on every partial group and a symlinked late artifact", () => {
    const partialReplay = root();
    touch(partialReplay, SUCCESSOR_REPLAY_RECEIPT_PATHS[0]);
    expect(() => inspectSuccessorLateBoundTopology(partialReplay)).toThrowError(
      expect.objectContaining<Partial<SuccessorDagError>>({
        code: "SUCCESSOR_LATE_STATE_PARTIAL",
      }),
    );

    const outputOnly = root();
    for (const path of [
      ...SUCCESSOR_REPLAY_RECEIPT_PATHS,
      ...SUCCESSOR_ASSEMBLY_PATHS,
      ...SUCCESSOR_ASSEMBLED_REVIEW_PATHS,
    ]) {
      touch(outputOnly, path);
    }
    touch(outputOnly, SUCCESSOR_BINDING_LATE_PATHS[1]);
    expect(() => inspectSuccessorLateBoundTopology(outputOnly)).toThrowError(
      expect.objectContaining<Partial<SuccessorDagError>>({
        code: "SUCCESSOR_LATE_STATE_PARTIAL",
      }),
    );

    const linked = root();
    mkdirSync(dirname(resolve(linked, SUCCESSOR_REPLAY_RECEIPT_PATHS[0])), {
      recursive: true,
    });
    symlinkSync("/etc/passwd", resolve(linked, SUCCESSOR_REPLAY_RECEIPT_PATHS[0]));
    expect(() => inspectSuccessorLateBoundTopology(linked)).toThrowError(
      expect.objectContaining<Partial<SuccessorDagError>>({
        code: "SUCCESSOR_LATE_STATE_PARTIAL",
      }),
    );
  });

  it("rejects a symlinked ancestor even when the final target is a regular file", () => {
    const linkedRoot = root();
    const external = root();
    touch(external, SUCCESSOR_REPLAY_RECEIPT_PATHS[0].replace(/^tools\//u, ""));
    symlinkSync(external, resolve(linkedRoot, "tools"), "dir");
    expect(() => inspectSuccessorLateBoundTopology(linkedRoot)).toThrowError(
      expect.objectContaining<Partial<SuccessorDagError>>({
        code: "SUCCESSOR_LATE_STATE_PARTIAL",
      }),
    );
  });

  it("rejects a symlinked ancestor before treating a missing final target as absent", () => {
    const linkedRoot = root();
    const external = root();
    symlinkSync(external, resolve(linkedRoot, "tools"), "dir");
    expect(() => inspectSuccessorLateBoundTopology(linkedRoot)).toThrowError(
      expect.objectContaining<Partial<SuccessorDagError>>({
        code: "SUCCESSOR_LATE_STATE_PARTIAL",
      }),
    );
  });
});
