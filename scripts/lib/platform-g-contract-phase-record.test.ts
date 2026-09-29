import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildGContractPhaseBindingRegistry,
  buildGContractPhaseReviewTuple,
  G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  G_CONTRACT_PHASE_EXACT17_PATHS,
  G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
  G_CONTRACT_PHASE_RECORD_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
  G_CONTRACT_PHASE_SOURCE_PATH,
  G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
  readGContractPhaseRecordSource,
  serializeGContractPhaseJson,
  validateGContractPhaseReviewTuple,
  type GContractPhaseReviewTuple,
  type ReviewBinding,
} from "./platform-g-contract-phase-record";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("G-CONTRACT-P1 phase-record authority", () => {
  it("fixes the Gate-specific exact17 source and rejects unknown or reordered input", () => {
    const source = readGContractPhaseRecordSource(repositoryRoot);
    expect(source.gateId).toBe("G-CONTRACT");
    expect(source.record.status).toBe("IN_PROGRESS");
    expect(source.exactLateBoundPaths).toEqual(G_CONTRACT_PHASE_EXACT17_PATHS);
    expect(source.implementationBoundary.notGateClosure).toBe(true);
    expect(source.implementationBoundary.gateStatus).toBe("ALL_GATES_OPEN");

    const unknownRoot = sourceFixture();
    const unknown = readJson(unknownRoot, G_CONTRACT_PHASE_SOURCE_PATH);
    unknown.unexpected = true;
    writeJson(unknownRoot, G_CONTRACT_PHASE_SOURCE_PATH, unknown);
    expectCode(
      () => readGContractPhaseRecordSource(unknownRoot),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );

    const reorderedRoot = sourceFixture();
    const reordered = readJson(reorderedRoot, G_CONTRACT_PHASE_SOURCE_PATH);
    const paths = reordered.exactLateBoundPaths as unknown[];
    [paths[0], paths[1]] = [paths[1], paths[0]];
    writeJson(reorderedRoot, G_CONTRACT_PHASE_SOURCE_PATH, reordered);
    expectCode(
      () => readGContractPhaseRecordSource(reorderedRoot),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );
  });

  it("keeps the two lineages ordered, rejects self-review, and derives only a pre-terminal registry", () => {
    const supply = fakeBinding("generator_supply_v3", "1", "2");
    const r5 = fakeBinding("g_contract_r5", "3", "4");
    const tuple = buildGContractPhaseReviewTuple(repositoryRoot, [supply, r5]);
    const registry = buildGContractPhaseBindingRegistry(repositoryRoot, tuple);
    expect(tuple.reviews.map(({ subject }) => subject)).toEqual([
      "generator_supply_v3",
      "g_contract_r5",
    ]);
    expect(registry.state).toBe("PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT");
    expect(registry.terminalReview).toEqual({
      path: "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
      state: "ABSENT",
    });
    expect(JSON.stringify(registry)).not.toContain("REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE");

    const selfReviewed = structuredClone(tuple) as unknown as {
      reviews: Array<{ candidate: { actorId: string }; review: { reviewerId: string } }>;
    };
    selfReviewed.reviews[0]!.review.reviewerId = selfReviewed.reviews[0]!.candidate.actorId;
    expectCode(
      () =>
        validateGContractPhaseReviewTuple(
          repositoryRoot,
          selfReviewed as unknown as GContractPhaseReviewTuple,
        ),
      "G_CONTRACT_PHASE_SELF_REVIEW",
    );

    const reordered = structuredClone(tuple) as unknown as { reviews: ReviewBinding[] };
    reordered.reviews.reverse();
    expectCode(
      () =>
        validateGContractPhaseReviewTuple(
          repositoryRoot,
          reordered as unknown as GContractPhaseReviewTuple,
        ),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );
  });
});

function sourceFixture(): string {
  const root = mkdtempSync(join(tmpdir(), "g-contract-phase-source-"));
  temporaryRoots.push(root);
  for (const path of authorityFiles()) copyFromRepository(root, path);
  return root;
}

function fakeBinding(
  subject: "generator_supply_v3" | "g_contract_r5",
  c: string,
  r: string,
): ReviewBinding {
  const supply = subject === "generator_supply_v3";
  return {
    subject,
    candidateSubjectPath: supply
      ? "tools/generator-supply/v3/profile.json"
      : G_CONTRACT_PHASE_RECORD_PATH,
    candidate: {
      actorId: `candidate-${c}`,
      commit: c.repeat(40),
      tree: "a".repeat(40),
      parent: "b".repeat(40),
      diffSha256: sha("c"),
    },
    review: {
      reviewerId: `reviewer-${r}`,
      commit: r.repeat(40),
      tree: "d".repeat(40),
      parent: c.repeat(40),
      path: supply
        ? "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md"
        : "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md",
      gitBlob: "e".repeat(40),
      sha256: sha("d"),
      sizeBytes: 1,
      mode: "100644",
      diffSha256: sha("e"),
      verdict: "APPROVE_P0_0_P1_0_P2_0",
      findings: { p0: 0, p1: 0, p2: 0 },
    },
  };
}

function authorityFiles(): string[] {
  return [
    G_CONTRACT_PHASE_SOURCE_PATH,
    G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
    G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
    G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
    G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
    "tools/contract-review-binding/v1/registry.json",
  ];
}

function copyFromRepository(root: string, path: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  cpSync(resolve(repositoryRoot, path), resolve(root, path));
}

function writeText(root: string, path: string, value: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  writeFileSync(resolve(root, path), value);
}

function writeJson(root: string, path: string, value: unknown): void {
  writeText(root, path, serializeGContractPhaseJson(value));
}

function readJson(root: string, path: string): Record<string, unknown> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, unknown>;
}

function sha(character: string): `sha256:${string}` {
  return `sha256:${character.repeat(64)}`;
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}
