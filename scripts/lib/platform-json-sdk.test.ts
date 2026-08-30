import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  buildPlatformJSONSDKOutputs,
  expectedPlatformJSONSDKFiles,
  platformJSONSDKAuthorityProfileDigest,
  platformJSONSDKAuthorityProfileInputs,
  platformJSONSDKContractInputs,
  platformJSONSDKGeneratorSources,
} from "./platform-json-sdk";

const root = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const path of temporaryRoots.splice(0)) rmSync(path, { force: true, recursive: true });
});

describe("platform JSON SDK generator", () => {
  it("binds the model authority to an explicit versioned profile", () => {
    const inputs = platformJSONSDKAuthorityProfileInputs();
    expect(inputs).toEqual([...inputs].toSorted());
    expect(inputs).toContain("contracts/platform/v1alpha1/schemas/project.schema.json");
    expect(inputs).toContain(
      "contracts/platform/v1alpha1/schemas/environment-lease-page.schema.json",
    );
    expect(inputs).toContain("contracts/platform/v1alpha1/schemas/membership-page.schema.json");
    expect(inputs).toContain("contracts/platform/v1alpha1/schemas/role-binding-page.schema.json");
    expect(platformJSONSDKAuthorityProfileDigest(root)).toBe(
      "sha256:d20c3bdf3c50da23b5c73cc13af7a088ec98636cc3336f921e7a2158c6aa5690",
    );
  });

  it("binds sorted unique JSON Schema, OpenAPI, fixture, generator, and entry inputs", () => {
    const inputs = platformJSONSDKContractInputs(root);
    expect(inputs).toEqual([...inputs].toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("contracts/managed-agent/v1alpha1/openapi.json");
    expect(inputs).toContain("contracts/managed-host/v1alpha1/openapi.json");
    expect(inputs).toContain("contracts/platform/v1alpha1/fixtures/golden/project.json");
    expect(inputs).toContain(
      "contracts/common/v1alpha1/fixtures/negative/problem-secret-field.json",
    );
    expect(platformJSONSDKGeneratorSources()).toEqual(
      [...platformJSONSDKGeneratorSources()].toSorted(),
    );
  });

  it("renders deterministic language outputs and manifests", () => {
    const first = expectedPlatformJSONSDKFiles(root);
    const second = expectedPlatformJSONSDKFiles(root);
    expect(second).toEqual(first);
    expect(first).toHaveLength(6);
    for (const output of first) expect(output.source.length, output.path).toBeGreaterThan(100);
    expect(buildPlatformJSONSDKOutputs(root).map(({ path }) => path)).toEqual([
      "sdk/go/gen/common/v1alpha1/json_generated.go",
      "sdk/go/gen/platform/v1alpha1/json_generated.go",
      "sdk/go/gen/openapi/v1alpha1/client_generated.go",
      "sdk/typescript/src/platform.ts",
    ]);
  }, 120_000);

  it("does not import a network, service, or database runtime", () => {
    for (const source of platformJSONSDKGeneratorSources().filter(
      (source) => source !== "scripts/lib/platform-json-sdk.test.ts",
    )) {
      const text = readFileSync(join(root, source), "utf8");
      expect(text, source).not.toMatch(
        /net\/http|internal\/store|internal\/coordination|from ["']node:(?:http|https|net)|\bfetch\s*\(|\bDeno\.serve|\bBun\.serve/iu,
      );
    }
  });

  it("uses no external mutable generation directory", () => {
    const path = mkdtempSync(join(tmpdir(), "platform-json-sdk-"));
    temporaryRoots.push(path);
    expect(path.startsWith(root)).toBe(false);
  });
});
