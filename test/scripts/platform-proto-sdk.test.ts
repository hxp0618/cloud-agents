import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  PLATFORM_PROTO_GO_MANIFEST_PATH,
  PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH,
  WORKER_RUNTIME_PROTO_GO_OUTPUTS,
  platformProtoContractInputs,
  platformProtoGeneratorSources,
  platformProtocInstallationComplete,
} from "../../scripts/lib/platform-proto-sdk";

const root = resolve(import.meta.dirname, "../..");

describe("platform Proto SDK generation", () => {
  it("binds a unique, regular contract and generator input set", () => {
    for (const paths of [platformProtoContractInputs(root), platformProtoGeneratorSources()]) {
      expect(new Set(paths).size).toBe(paths.length);
      for (const path of paths)
        expect(readFileSync(resolve(root, path)).byteLength).toBeGreaterThan(0);
    }
    expect(platformProtoGeneratorSources().every((path) => !path.includes(".test."))).toBe(true);
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

  it("binds the current Worker Runtime descriptor and Go outputs", () => {
    expect(platformProtoContractInputs(root)).toContain(
      "contracts/worker/runtime/v1alpha1/runtime.proto",
    );
    for (const manifestPath of [
      PLATFORM_PROTO_GO_MANIFEST_PATH,
      PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH,
    ]) {
      const manifest = JSON.parse(readFileSync(resolve(root, manifestPath), "utf8")) as {
        packagePrivate?: boolean;
        runtimeDependencies?: Array<{ package: string }>;
        testDependencies?: Array<{ package: string }>;
        outputTreeAlgorithm: string;
        outputs: Array<{ path: string; sha256: string; sizeBytes: number }>;
      };
      expect(manifest.outputTreeAlgorithm).toBe("sorted-path-nul-sha256-nul-size-v1");
      if (manifestPath === PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH) {
        expect(manifest.packagePrivate).toBe(true);
        expect(manifest.runtimeDependencies?.map(({ package: name }) => name)).not.toContain(
          "@connectrpc/connect",
        );
        expect(manifest.testDependencies?.map(({ package: name }) => name)).toContain(
          "@connectrpc/connect",
        );
      }
      if (manifestPath === PLATFORM_PROTO_GO_MANIFEST_PATH) {
        expect(manifest.runtimeDependencies?.map(({ package: name }) => name)).not.toContain(
          "golang.org/x/text",
        );
      }
      expect(
        manifest.outputs.some((output) => output.path === WORKER_RUNTIME_PROTO_GO_OUTPUTS[0]),
      ).toBe(manifestPath === PLATFORM_PROTO_GO_MANIFEST_PATH);
      for (const output of manifest.outputs) {
        const bytes = readFileSync(resolve(root, output.path));
        expect(output.sizeBytes).toBe(bytes.byteLength);
      }
    }
  });
});
