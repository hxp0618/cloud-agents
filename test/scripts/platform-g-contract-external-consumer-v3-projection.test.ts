import { mkdtempSync, mkdirSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";

import {
  EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_ALGORITHM,
  EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_FORMAT,
  EXTERNAL_CONSUMER_V3_INPUT_MANIFEST_ALGORITHM,
  EXTERNAL_CONSUMER_V3_MEMBER_MANIFEST_ALGORITHM,
  writeExternalConsumerV3Projection,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3-projection";

const root = new URL("../..", import.meta.url).pathname;

describe("G-CONTRACT v3 external candidate projection authority", () => {
  it("freezes the external manifest and projection algorithms", () => {
    expect(EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_FORMAT).toBe(
      "cloud-agents-g-contract-external-consumer-candidate-manifest/v1",
    );
    expect(EXTERNAL_CONSUMER_V3_CANDIDATE_MANIFEST_ALGORITHM).toBe(
      "utf8-bytewise-sorted-path-kind-mode-size-sha256-nul-v1",
    );
    expect(EXTERNAL_CONSUMER_V3_INPUT_MANIFEST_ALGORITHM).toBe(
      "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
    );
    expect(EXTERNAL_CONSUMER_V3_MEMBER_MANIFEST_ALGORITHM).toBe(
      "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
    );
  });

  it("requires candidate binding in the projection receipt schema", () => {
    const schema = JSON.parse(
      readFileSync(
        join(root, "tools/g-contract-external-consumer/v3/projection-receipt.schema.json"),
        "utf8",
      ),
    ) as {
      required?: string[];
      properties?: Record<string, unknown>;
      $defs?: Record<string, unknown>;
    };
    expect(schema.required).toContain("candidateBinding");
    expect(schema.properties).toHaveProperty("candidateBinding");
    expect(schema.$defs).toHaveProperty("candidate");
    const ajv = new Ajv2020({ allErrors: true, strict: false });
    expect(() => ajv.compile(schema)).not.toThrow();
  });

  it("keeps the candidate manifest schema strict and absolute-root bound", () => {
    const schema = JSON.parse(
      readFileSync(
        join(root, "tools/g-contract-external-consumer/v3/candidate-manifest.schema.json"),
        "utf8",
      ),
    ) as { properties?: Record<string, unknown>; required?: string[] };
    expect(schema.required).toEqual([
      "formatVersion",
      "source",
      "candidateRoot",
      "manifestAlgorithm",
      "candidateFileCount",
      "candidateEntryCount",
      "records",
    ]);
    expect(schema.properties).toHaveProperty("candidateRoot");
    const ajv = new Ajv2020({ allErrors: true, strict: false });
    expect(() => ajv.compile(schema)).not.toThrow();
  });

  it("rejects a nested candidate/output root before creating projection output", () => {
    const fixtureRoot = mkdtempSync(join(tmpdir(), "cloud-agents-v3-projection-isolation-"));
    const authorityRoot = join(fixtureRoot, "authority");
    const candidateRoot = join(fixtureRoot, "candidate");
    const outputRoot = join(candidateRoot, "output");
    mkdirSync(authorityRoot);
    mkdirSync(candidateRoot);
    mkdirSync(outputRoot);
    try {
      expect(() =>
        writeExternalConsumerV3Projection({
          authorityRoot,
          candidateRoot,
          candidateManifestPath: join(fixtureRoot, "manifest.json"),
          outputRoot,
        }),
      ).toThrow("Candidate and output roots must not be nested.");
      expect(readdirSync(outputRoot)).toEqual([]);
    } finally {
      rmSync(fixtureRoot, { recursive: true, force: true });
    }
  });
});
