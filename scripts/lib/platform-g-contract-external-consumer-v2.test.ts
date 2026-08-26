import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  EC2_EXCLUSION_PATHS,
  EC2_SEMANTIC_INPUT_PATHS,
  EC2_PROFILE_PATH,
  checkExternalConsumerV2Source,
  assertExternalConsumerV2ProfileAbsent,
} from "./platform-g-contract-external-consumer-v2";

const root = resolve(import.meta.dirname, "../..");

describe("D-053-EC-2 versioned authority", () => {
  it("checks the committed source and keeps the authority-only state pending", () => {
    const result = checkExternalConsumerV2Source(root);
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
      compression: "none",
      pathOrdering: "UTF8_BYTE_LEXICOGRAPHIC",
      memberManifestAlgorithm: "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
      regularFileManifestAlgorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
      manifestFraming: "NUL_RECORDS",
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
});
