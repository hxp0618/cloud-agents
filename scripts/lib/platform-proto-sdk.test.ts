import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertPlatformProtoSDKCurrent,
  platformProtoContractInputs,
  platformProtoGeneratorSources,
} from "./platform-proto-sdk";

const root = resolve(import.meta.dirname, "../..");

describe("platform Proto SDK generation", () => {
  it("keeps the checked-in descriptor and language outputs current", () => {
    expect(() => assertPlatformProtoSDKCurrent(root)).not.toThrow();
  }, 120_000);

  it("binds a unique, regular contract and generator input set", () => {
    for (const paths of [platformProtoContractInputs(root), platformProtoGeneratorSources()]) {
      expect(new Set(paths).size).toBe(paths.length);
      for (const path of paths)
        expect(readFileSync(resolve(root, path)).byteLength).toBeGreaterThan(0);
    }
  });
});
