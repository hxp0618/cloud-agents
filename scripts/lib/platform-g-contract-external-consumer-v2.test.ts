import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  EC2_EXCLUSION_PATHS,
  EC2_SEMANTIC_INPUT_PATHS,
  EC2_PROFILE_PATH,
  checkExternalConsumerV2Source,
  checkExternalConsumerV2IndependentReview,
  assertExternalConsumerV2ProfileAbsent,
} from "./platform-g-contract-external-consumer-v2";

const root = resolve(import.meta.dirname, "../..");

describe("D-053-EC-2 versioned authority", () => {
  it("checks the committed source and keeps the authority-only state pending", () => {
    const result = checkExternalConsumerV2Source(root);
    expect(result.source.authorityRevision).toBe("D-053-EC-2.r1");
    expect(result.source.supersedesCandidate).toEqual({
      commit: "74f5ad620f5061adde2da14adce5b2032d4399bb",
      tree: "322332a93e712dc400e6e2bc4616c3430dce8c4c",
      parent: "8ffc2c86df6d0d6a02677bec0790b30de233a71a",
    });
    expect(result.source.candidateBinding.diff).toEqual([
      {
        status: "M",
        path: "tools/g-contract-external-consumer/v2/source.json",
        mode: "100644",
      },
    ]);
    expect(result.source.status).toBe("AUTHORITY_FROZEN_REVIEW_PENDING");
    expect(result.source.receiptState).toMatchObject({
      syntheticReceipts: "FORBIDDEN",
      generatedProfile: "ABSENT_PENDING",
      authorityReview: "ABSENT_PENDING",
    });
    expect(result.source.implementationBoundary).toMatchObject({
      replay: false,
      profileGeneration: false,
      receiptGeneration: false,
      gateStatus: "ALL_GATES_OPEN",
    });
  });

  it("freezes complete ordered inputs and exact late-bound exclusions", () => {
    const source = JSON.parse(
      readFileSync(resolve(root, "tools/g-contract-external-consumer/v2/source.json"), "utf8"),
    );
    expect(source.semanticInputs.map((entry: { path: string }) => entry.path)).toEqual([
      ...EC2_SEMANTIC_INPUT_PATHS,
    ]);
    expect(source.semanticInputs).toHaveLength(18);
    expect(source.exclusions.patterns).toEqual([]);
    expect(source.exclusions.paths).toEqual([...EC2_EXCLUSION_PATHS]);
    expect(new Set(source.exclusions.paths).size).toBe(source.exclusions.paths.length);
  });

  it("freezes deterministic archive and both manifest algorithms", () => {
    const source = JSON.parse(
      readFileSync(resolve(root, "tools/g-contract-external-consumer/v2/source.json"), "utf8"),
    );
    expect(source.projection).toMatchObject({
      archiveFormat: "ustar",
      outputDirectory: "tools/g-contract-external-consumer/v2/evidence/replay",
      archivePath: "tools/g-contract-external-consumer/v2/evidence/replay/projection.tar",
      compression: "none",
      pathOrdering: "UTF8_BYTE_LEXICOGRAPHIC",
      memberManifestAlgorithm: "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      regularFileManifestAlgorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
      manifestFraming: "NUL_RECORDS",
      receiptPath: "tools/g-contract-external-consumer/v2/evidence/replay/projection.json",
      memberManifestPath:
        "tools/g-contract-external-consumer/v2/evidence/replay/projection.member-manifest.json",
    });
    expect(source.projection.tar).toEqual({
      mtimeEpochSeconds: 0,
      uid: 0,
      gid: 0,
      uname: "",
      gname: "",
      paxHeaders: "forbidden",
      duplicateEntries: "forbidden",
    });
  });

  it("does not expose a profile or receipt writer in the authority runner", () => {
    expect(() => assertExternalConsumerV2ProfileAbsent(root)).not.toThrow();
    const source = JSON.parse(
      readFileSync(resolve(root, "tools/g-contract-external-consumer/v2/source.json"), "utf8"),
    );
    expect(source.runner.entrypoint).toBe(
      "bun scripts/generate-platform-g-contract-external-consumer-v2.ts --check-source",
    );
    expect(source.runner.replay).toBe(false);
    expect(source.runner.profileWriter).toBe(false);
    expect(
      source.receiptPaths.some((entry: { path: string }) => entry.path === EC2_PROFILE_PATH),
    ).toBe(true);
  });

  it("validates the independent review child when its late-bound file exists", () => {
    const reviewPath = resolve(
      root,
      "docs/plan/p1/g-contract-external-consumer-v2-independent-review-20260826.md",
    );
    if (!existsSync(reviewPath)) return;
    const result = checkExternalConsumerV2IndependentReview(root);
    expect(result.decision).toBe("APPROVE");
    expect(result.findings).toEqual({ P0: 0, P1: 0, P2: 0 });
  });
});
