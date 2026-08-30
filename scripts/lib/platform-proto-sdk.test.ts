import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  assertPlatformProtoSDKCurrent,
  platformProtoContractInputs,
  platformProtoGeneratorSources,
  platformProtocInstallationComplete,
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

  it("rejects a cached protoc installation without well-known types", () => {
    const installRoot = mkdtempSync(resolve(root, ".platform-protoc-test-"));
    try {
      mkdirSync(resolve(installRoot, "bin"));
      writeFileSync(resolve(installRoot, "bin/protoc"), "");
      expect(platformProtocInstallationComplete(installRoot)).toBe(false);

      mkdirSync(resolve(installRoot, "include/google/protobuf"), { recursive: true });
      writeFileSync(resolve(installRoot, "include/google/protobuf/timestamp.proto"), "");
      expect(platformProtocInstallationComplete(installRoot)).toBe(true);
    } finally {
      rmSync(installRoot, { force: true, recursive: true });
    }
  });
});
