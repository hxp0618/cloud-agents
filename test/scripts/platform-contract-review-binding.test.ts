import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";
import {
  assertContractReviewBindingSourceCurrent,
  buildContractReviewBindingSource,
  contractReviewBindingAuthorityInputs,
  CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH,
  CONTRACT_REVIEW_BINDING_OUTPUT_PATH,
  CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
  CONTRACT_REVIEW_BINDING_SOURCE_PATH,
  CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
  CONTRACT_REVIEW_TUPLE_PATH,
  CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
  inspectContractReviewBindingState,
  serializeContractReviewBinding,
  writeContractReviewBindingSource,
} from "../../scripts/lib/platform-contract-review-binding";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

function writeParent(root: string, path: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}

describe("detached contract review-binding state machine", () => {
  it("requires a strict authority source before any absent-state classification", () => {
    const root = mkdtempSync(join(tmpdir(), "contract-review-binding-no-source-"));
    temporaryRoots.push(root);
    expectCode(
      () => inspectContractReviewBindingState(root),
      "CONTRACT_REVIEW_BINDING_SOURCE_REQUIRED",
    );
  });

  it("exclusively creates and checks the canonical authority source before review", () => {
    const root = mkdtempSync(join(tmpdir(), "contract-review-binding-source-"));
    temporaryRoots.push(root);
    for (const schemaPath of [
      CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
      CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
      CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
    ]) {
      writeParent(root, schemaPath);
      cpSync(resolve(repositoryRoot, schemaPath), resolve(root, schemaPath));
    }

    expect(() => writeContractReviewBindingSource(root)).not.toThrow();
    expect(readFileSync(resolve(root, CONTRACT_REVIEW_BINDING_SOURCE_PATH), "utf8")).toBe(
      serializeContractReviewBinding(buildContractReviewBindingSource()),
    );
    expect(assertContractReviewBindingSourceCurrent(root)).toEqual(
      buildContractReviewBindingSource(),
    );
    expect(() => writeContractReviewBindingSource(root)).not.toThrow();

    writeFileSync(
      resolve(root, CONTRACT_REVIEW_BINDING_SOURCE_PATH),
      `${serializeContractReviewBinding(buildContractReviewBindingSource())} `,
    );
    expectCode(
      () => assertContractReviewBindingSourceCurrent(root),
      "CONTRACT_REVIEW_BINDING_SOURCE_DRIFT",
    );
    expectCode(
      () => writeContractReviewBindingSource(root),
      "CONTRACT_REVIEW_BINDING_SOURCE_DRIFT",
    );
  });

  it("keeps authority inputs deterministic and excludes every late-bound artifact", () => {
    const inputs = contractReviewBindingAuthorityInputs();
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(CONTRACT_REVIEW_BINDING_SOURCE_PATH);
    expect(inputs).toContain("scripts/lib/platform-contract-closure-profile-v3.ts");
    expect(inputs).toContain("test/scripts/platform-contract-closure-profile-v3.test.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-profile-v2.ts");
    expect(inputs).toContain("test/scripts/platform-generator-supply-profile-v2.test.ts");
    expect(inputs).toContain("scripts/lib/platform-generator-supply-replay-v2.ts");
    expect(inputs).toContain("test/scripts/platform-generator-supply-replay-v2.test.ts");
    expect(inputs).not.toContain(CONTRACT_REVIEW_TUPLE_PATH);
    expect(inputs).not.toContain(CONTRACT_REVIEW_BINDING_OUTPUT_PATH);
    expect(inputs).not.toContain(CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH);
    expect(inputs.every((path) => !path.includes("fixtures/manifest.json"))).toBe(true);
  });
});
