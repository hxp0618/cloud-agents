import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  buildPlatformJSONSDKOutputs,
  expectedPlatformJSONSDKFiles,
  platformJSONSDKContractInputs,
  platformJSONSDKGeneratorSources,
} from "./platform-json-sdk";

const root = resolve(import.meta.dirname, "../..");

describe("platform JSON SDK generator", () => {
  it("binds sorted unique JSON Schema, OpenAPI, fixture, generator, and entry inputs", () => {
    const inputs = platformJSONSDKContractInputs(root);
    expect(inputs).toEqual([...inputs].toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("contracts/managed-agent/v1alpha1/openapi.json");
    expect(inputs).toContain("contracts/managed-host/v1alpha1/openapi.json");
    expect(inputs).toContain(
      "contracts/managed-agent/v1alpha1/schemas/execution-user-input-resolution-request.schema.json",
    );
    expect(inputs).toContain("contracts/platform/v1alpha1/fixtures/golden/project.json");
    expect(inputs).toContain(
      "contracts/common/v1alpha1/fixtures/negative/problem-secret-field.json",
    );
    expect(platformJSONSDKGeneratorSources()).toEqual(
      [...platformJSONSDKGeneratorSources()].toSorted(),
    );
  });

  it("renders deterministic language outputs without internal provenance", () => {
    const first = expectedPlatformJSONSDKFiles(root);
    const second = expectedPlatformJSONSDKFiles(root);
    expect(second).toEqual(first);
    expect(first).toHaveLength(4);
    for (const output of first) {
      expect(output.source.length, output.path).toBeGreaterThan(100);
      expect(output.source, output.path).not.toContain("Contract manifest:");
      expect(output.source, output.path).not.toContain("Generator source manifest:");
      expect(output.source, output.path).not.toContain("Generation config:");
    }
    expect(buildPlatformJSONSDKOutputs(root).map(({ path }) => path)).toEqual([
      "sdk/go/gen/common/v1alpha1/json_generated.go",
      "sdk/go/gen/platform/v1alpha1/json_generated.go",
      "sdk/go/gen/openapi/v1alpha1/client_generated.go",
      "sdk/typescript/src/platform.ts",
    ]);
  }, 120_000);

  it("does not import a server, service, or database runtime", () => {
    for (const source of platformJSONSDKGeneratorSources().filter(
      (source) => !source.endsWith(".test.ts") && !source.endsWith("_test.go"),
    )) {
      const text = readFileSync(join(root, source), "utf8");
      expect(text, source).not.toMatch(
        /internal\/store|internal\/coordination|from ["']node:(?:http|https|net)|\bDeno\.serve|\bBun\.serve/iu,
      );
    }
  });
});
