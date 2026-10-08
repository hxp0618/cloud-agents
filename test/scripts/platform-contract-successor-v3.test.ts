import { createHash } from "node:crypto";
import {
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertPlatformContractSuccessorV3Current,
  assertPlatformContractSuccessorV3PredecessorFrozen,
  assertPlatformContractSuccessorV3ReplayAbsent,
  buildPlatformContractSuccessorV3,
  PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS,
  PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
} from "../../scripts/lib/platform-contract-successor-v3";

const root = new URL("../..", import.meta.url).pathname;
const PRODUCER_MANIFEST_PATHS = [
  "contracts/generated/proto/manifest.json",
  "sdk/go/generated-manifest.json",
  "sdk/go/json-generated-manifest.json",
  "sdk/go/proto-generated-manifest.json",
  "sdk/typescript/generated-manifest.json",
  "sdk/typescript/json-generated-manifest.json",
  "sdk/typescript/proto-generated-manifest.json",
] as const;

describe("platform contract successor v3", () => {
  it("binds the exact current 47-output set and leaves replay pending", () => {
    const document = buildPlatformContractSuccessorV3(root);
    expect(PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS).toHaveLength(47);
    expect(document.currentOutputSet.count).toBe(47);
    expect(document.currentOutputSet.files).toHaveLength(47);
    const coreOutputs = new Set(PLATFORM_CONTRACT_SUCCESSOR_V3_CORE_OUTPUTS);
    const missingProducerOutputs = PRODUCER_MANIFEST_PATHS.flatMap((manifestPath) => {
      const manifest = JSON.parse(readFileSync(join(root, manifestPath), "utf8")) as {
        outputs: ReadonlyArray<{ path: string }>;
      };
      return [manifestPath, ...manifest.outputs.map((output) => output.path)].filter(
        (path) => !coreOutputs.has(path),
      );
    });
    expect(missingProducerOutputs).toEqual([]);
    expect(document.projection.exclusions).toEqual(
      PLATFORM_CONTRACT_SUCCESSOR_V3_PROJECTION_EXCLUSIONS,
    );
    expect(document.status).toBe("SUCCESSOR_CURRENT_PRE_REPLAY");
    expect(document.projection.replayState).toBe("REPLAY_PENDING");
    const manifest = createHash("sha256");
    for (const file of document.currentOutputSet.files) {
      manifest
        .update(file.path)
        .update("\0")
        .update(file.sha256)
        .update("\0")
        .update(file.gitMode)
        .update("\0");
    }
    expect(document.currentOutputSet.manifestSha256).toBe(`sha256:${manifest.digest("hex")}`);
  });

  it("matches the generated successor lock", () => {
    expect(() => assertPlatformContractSuccessorV3Current(root)).not.toThrow();
  });

  it("rejects a tampered, extra, or symlinked pre-replay fixture", () => {
    const fixture = mkdtempSync(join(tmpdir(), "successor-v3-test-"));
    try {
      const predecessor = join(fixture, "contracts/generation.lock.json");
      mkdirSync(join(fixture, "contracts"), { recursive: true });
      writeFileSync(predecessor, readFileSync(join(root, "contracts/generation.lock.json")));
      expect(() => assertPlatformContractSuccessorV3PredecessorFrozen(fixture)).not.toThrow();

      writeFileSync(predecessor, "tampered");
      expect(() => assertPlatformContractSuccessorV3PredecessorFrozen(fixture)).toThrow();

      rmSync(predecessor);
      symlinkSync(join(root, "contracts/generation.lock.json"), predecessor);
      expect(() => assertPlatformContractSuccessorV3PredecessorFrozen(fixture)).toThrow();

      rmSync(predecessor);
      expect(() => assertPlatformContractSuccessorV3ReplayAbsent(fixture)).not.toThrow();
      const replay = join(fixture, "tools/contract-successor/v3/evidence/replay.json");
      mkdirSync(join(fixture, "tools/contract-successor/v3/evidence"), { recursive: true });
      writeFileSync(replay, "fake");
      expect(() => assertPlatformContractSuccessorV3ReplayAbsent(fixture)).toThrow();
      expect(lstatSync(replay).isSymbolicLink()).toBe(false);
    } finally {
      rmSync(fixture, { recursive: true, force: true });
    }
  });
});
