import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { describe, expect, it } from "vitest";
import Ajv2020 from "ajv/dist/2020.js";

import {
  assertExternalConsumerV3ReplayAbsent,
  assertExternalConsumerV3PredecessorFrozen,
  assertExternalConsumerV3SourceCurrent,
  buildExternalConsumerV3ProjectionPlan,
  buildExternalConsumerV3ReplayPlan,
  buildExternalConsumerV3Source,
  EXTERNAL_CONSUMER_V3_RECEIPT_PATHS,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3";

const root = new URL("../..", import.meta.url).pathname;

describe("G-CONTRACT external-consumer authority v3", () => {
  it("binds current SDK inputs to the 47-output successor without replay claims", () => {
    const source = buildExternalConsumerV3Source(root);
    expect(source.$schema).toBe(
      "https://schemas.cloud-agents.dev/tools/g-contract-external-consumer/v3/source.schema.json",
    );
    expect(source.profileId).toBe("g-contract-external-consumer/v3");
    expect(source.status).toBe("AUTHORITY_FROZEN_REVIEW_PENDING");
    expect(source.contractSuccessor.outputCount).toBe(47);
    expect(new Set(source.semanticInputs.map((input) => input.path)).size).toBe(
      source.semanticInputs.length,
    );
    expect(
      source.semanticInputs.some(
        (input) => input.path === "scripts/generate-platform-proto-sdks.ts",
      ),
    ).toBe(true);
    expect(
      source.semanticInputs.some(
        (input) => input.path === "contracts/platform-adapter/v1alpha1/platform_adapter.proto",
      ),
    ).toBe(true);
    expect(source.evidenceContract.state).toBe("REPLAY_PENDING");
  });

  it("binds summary producers without introducing late-bound output cycles", () => {
    const paths = buildExternalConsumerV3Source(root).semanticInputs.map((input) => input.path);
    expect(paths).toContain("scripts/lib/platform-g-contract-external-consumer-v3-summary.ts");
    expect(paths).toContain(
      "test/scripts/platform-g-contract-external-consumer-v3-summary.test.ts",
    );
    for (const output of [
      "tools/g-contract-external-consumer/v3/source.json",
      "tools/g-contract-external-consumer/v3/profile.json",
      ...EXTERNAL_CONSUMER_V3_RECEIPT_PATHS,
    ])
      expect(paths).not.toContain(output);
  });

  it("keeps the checked-in source current and profile absent", () => {
    expect(() => assertExternalConsumerV3SourceCurrent(root)).not.toThrow();
    expect(() => assertExternalConsumerV3ReplayAbsent(root)).not.toThrow();
  });

  it("builds a read-only replay plan without creating evidence", () => {
    const plan = buildExternalConsumerV3ReplayPlan(root);
    expect(plan.formatVersion).toBe("cloud-agents-g-contract-external-consumer-replay-plan/v3");
    expect(plan.status).toBe("REPLAY_PENDING");
    expect(plan.writeMode).toBe("READ_ONLY");
    expect(plan.syntheticReceipts).toBe("FORBIDDEN");
    expect(plan.platformRuns).toEqual([
      { platform: "darwin-arm64", runId: "A" },
      { platform: "darwin-arm64", runId: "B" },
      { platform: "linux-amd64", runId: "A" },
      { platform: "linux-amd64", runId: "B" },
    ]);
    expect(plan.receiptPaths).toEqual(EXTERNAL_CONSUMER_V3_RECEIPT_PATHS);
    expect(() => assertExternalConsumerV3ReplayAbsent(root)).not.toThrow();
  });

  it("fails closed before projection when no frozen clean candidate is bound", () => {
    const plan = buildExternalConsumerV3ProjectionPlan(root);
    expect(plan.status).toBe("BLOCKED");
    expect(plan.blockerCode).toBe("FROZEN_CLEAN_CANDIDATE_REQUIRED");
    expect(plan.candidateBoundary).toBe("EXTERNAL_CLEAN_CANDIDATE");
    expect(plan.writeMode).toBe("READ_ONLY");
    expect(plan.syntheticReceipts).toBe("FORBIDDEN");
    expect(plan.exclusionPaths).toContain("contracts/generation.lock.successor-v3.json");
    expect(() => assertExternalConsumerV3ReplayAbsent(root)).not.toThrow();
  });

  it("keeps every required v3 schema property declared", () => {
    for (const path of [
      "tools/g-contract-external-consumer/v3/projection-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/native-replay-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/replay-summary-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/profile.schema.json",
      "tools/g-contract-external-consumer/v3/replay-plan.schema.json",
      "tools/g-contract-external-consumer/v3/projection-plan.schema.json",
    ]) {
      const schema = JSON.parse(readFileSync(join(root, path), "utf8")) as {
        properties?: Record<string, unknown>;
        required?: string[];
      };
      for (const required of schema.required ?? []) {
        expect(schema.properties ?? {}).toHaveProperty(required);
      }
    }
  });

  it("binds native receipts to the parameterized v3 runner and projection summary", () => {
    const nativeSchema = JSON.parse(
      readFileSync(
        join(root, "tools/g-contract-external-consumer/v3/native-replay-receipt.schema.json"),
        "utf8",
      ),
    ) as {
      properties?: {
        projectionBinding?: {
          required?: string[];
          properties?: Record<string, unknown>;
        };
        runner?: { properties?: { entrypoint?: { pattern?: string } } };
      };
    };
    const projectionBinding = nativeSchema.properties?.projectionBinding;
    expect(projectionBinding?.required).toEqual([
      "archivePath",
      "archiveSha256",
      "archiveSizeBytes",
      "memberManifestSha256",
      "inputManifestSha256",
      "memberCount",
    ]);
    for (const property of [
      "archiveSha256",
      "archiveSizeBytes",
      "memberManifestSha256",
      "inputManifestSha256",
      "memberCount",
    ]) {
      expect(projectionBinding?.properties ?? {}).toHaveProperty(property);
    }

    const entrypointPattern = nativeSchema.properties?.runner?.properties?.entrypoint?.pattern;
    expect(entrypointPattern).toBe(
      "^bun scripts/replay-platform-g-contract-external-consumer-v3\\.ts --native --projection-root <projection-root> --platform <platform> --run-id <run-id> --output-root <output-root>$",
    );
    const entrypoint =
      "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --native --projection-root <projection-root> --platform <platform> --run-id <run-id> --output-root <output-root>";
    expect(new RegExp(entrypointPattern ?? "$").test(entrypoint)).toBe(true);
    expect(
      new RegExp(entrypointPattern ?? "$").test(
        "bun scripts/replay-platform-g-contract-external-consumer-v3.ts --check-authority",
      ),
    ).toBe(false);
  });

  it("compiles all v3 schemas and validates the generated replay plan", () => {
    const ajv = new Ajv2020({ allErrors: true, strict: false });
    const schemaPaths = [
      "tools/g-contract-external-consumer/v3/projection-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/native-replay-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/replay-summary-receipt.schema.json",
      "tools/g-contract-external-consumer/v3/profile.schema.json",
      "tools/g-contract-external-consumer/v3/replay-plan.schema.json",
      "tools/g-contract-external-consumer/v3/projection-plan.schema.json",
    ];
    const schemas = schemaPaths.map((path) => {
      const schema = JSON.parse(readFileSync(join(root, path), "utf8")) as object;
      expect(() => ajv.compile(schema)).not.toThrow();
      return { path, schema };
    });
    const replayPlan = buildExternalConsumerV3ReplayPlan(root);
    const replayPlanSchema = schemas.find(({ path }) =>
      path.endsWith("replay-plan.schema.json"),
    )!.schema;
    expect(ajv.validate(replayPlanSchema, replayPlan)).toBe(true);
    expect(ajv.errors).toBeNull();
  });

  it("rejects tampered predecessors and any fake or symlinked replay receipt", () => {
    const fixture = mkdtempSync(join(tmpdir(), "external-consumer-v3-test-"));
    try {
      const predecessor = join(fixture, "tools/g-contract-external-consumer/v2/source.json");
      mkdirSync(join(fixture, "tools/g-contract-external-consumer/v2"), {
        recursive: true,
      });
      writeFileSync(
        predecessor,
        readFileSync(join(root, "tools/g-contract-external-consumer/v2/source.json")),
      );
      expect(() => assertExternalConsumerV3PredecessorFrozen(fixture)).not.toThrow();

      writeFileSync(predecessor, "tampered");
      expect(() => assertExternalConsumerV3PredecessorFrozen(fixture)).toThrow();

      const receipt = join(fixture, EXTERNAL_CONSUMER_V3_RECEIPT_PATHS[0]);
      mkdirSync(join(fixture, "tools/g-contract-external-consumer/v3/evidence/replay"), {
        recursive: true,
      });
      expect(() => assertExternalConsumerV3ReplayAbsent(fixture)).not.toThrow();
      writeFileSync(receipt, "fake");
      expect(() => assertExternalConsumerV3ReplayAbsent(fixture)).toThrow();
      rmSync(receipt);
      symlinkSync(join(root, "tools/g-contract-external-consumer/v2/source.json"), receipt);
      expect(() => assertExternalConsumerV3ReplayAbsent(fixture)).toThrow();
    } finally {
      rmSync(fixture, { recursive: true, force: true });
    }
  });
});
