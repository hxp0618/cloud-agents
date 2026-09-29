import { rmSync } from "node:fs";
import { resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildContractClosureProfileV4Source,
  buildContractClosureProfileV4TestSource,
  CONTRACT_CLOSURE_V4_AUTHORITY_FILE,
  CONTRACT_CLOSURE_V4_CRITERIA,
  CONTRACT_CLOSURE_V4_MISSING,
  CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE,
  CONTRACT_CLOSURE_V4_RUNTIME_FILES,
  CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE,
  CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES,
  serializeContractClosureProfileV4Registry,
  serializeContractClosureProfileV4Source,
} from "./platform-contract-closure-profile-v4";

type MutableRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

describe("contract closure profile v4 Slice A authority", () => {
  it("promotes the deterministic test builder to the canonical production source serializer", () => {
    const source = buildContractClosureProfileV4Source(repositoryRoot);
    expect(source).toEqual(buildContractClosureProfileV4TestSource(repositoryRoot));
    expect(serializeContractClosureProfileV4Source(source)).toBe(
      serializeContractClosureProfileV4Registry(source),
    );
  });

  it("keeps exported authority and runtime selectors immutable against caller replacement", () => {
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_AUTHORITY_FILE)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_RUNTIME_FILES)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_RUNTIME_FILES[0])).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES[0])).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_CRITERIA)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_MISSING)).toBe(true);
    expect(() => {
      (CONTRACT_CLOSURE_V4_AUTHORITY_FILE as unknown as MutableRecord).gitBlob = "0".repeat(40);
    }).toThrow(TypeError);
    expect(() => {
      (CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE as unknown as MutableRecord).candidateCommit =
        "0".repeat(40);
    }).toThrow(TypeError);
    expect(() => {
      (CONTRACT_CLOSURE_V4_RUNTIME_FILES[0] as unknown as MutableRecord).sha256 = "0".repeat(64);
    }).toThrow(TypeError);
    expect(() => {
      (CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES[0] as unknown as MutableRecord).gitBlob =
        "0".repeat(40);
    }).toThrow(TypeError);
  });
});
