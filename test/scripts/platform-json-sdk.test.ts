import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  buildPlatformJSONSDKOutputs,
  expectedPlatformJSONSDKFiles,
  platformJSONSDKContractInputs,
  platformJSONSDKGeneratorSources,
} from "../../scripts/lib/platform-json-sdk";

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
    for (const path of [
      "contracts/common/v1alpha1/schemas/namespace-ref.schema.json",
      "contracts/platform/v1alpha1/schemas/sandbox-file-entry.schema.json",
      "contracts/platform/v1alpha1/schemas/sandbox-file-page.schema.json",
      "contracts/platform/v1alpha1/schemas/sandbox-file-read-page.schema.json",
      "contracts/platform/v1alpha1/schemas/sandbox-file-write-request.schema.json",
      "contracts/managed-agent/v1alpha1/schemas/event-cursor.schema.json",
      "contracts/managed-agent/v1alpha1/schemas/event-limit.schema.json",
    ]) {
      expect(inputs).toContain(path);
    }
    expect(platformJSONSDKGeneratorSources()).toEqual(
      [...platformJSONSDKGeneratorSources()].toSorted(),
    );
    expect(platformJSONSDKGeneratorSources().every((path) => !path.includes(".test."))).toBe(true);
  });

  it("renders deterministic language outputs without internal provenance", () => {
    const first = expectedPlatformJSONSDKFiles(root);
    const second = expectedPlatformJSONSDKFiles(root);
    expect(second).toEqual(first);
    expect(first).toHaveLength(6);
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
    const typescriptManifest = JSON.parse(
      first.find((output) => output.path === "sdk/typescript/json-generated-manifest.json")!.source,
    ) as {
      packageIdentity: string;
      packagePrivate: boolean;
      outputs: Array<{ path: string; sha256: string; sizeBytes: number }>;
      dependencyFiles: { package: { path: string; sha256: string } };
      generator: { dependencies: Record<string, unknown> };
    };
    expect(typescriptManifest.packageIdentity).toBe(
      "@cloud-agents/cloud-agent-platform-sdk/platform",
    );
    expect(typescriptManifest.packagePrivate).toBe(true);
    expect(typescriptManifest.dependencyFiles.package.path).toBe("sdk/typescript/package.json");
    expect(Object.keys(typescriptManifest.generator.dependencies)).toEqual(
      expect.arrayContaining([
        "identityGoOutput",
        "identityTypeScriptOutput",
        "identityGoManifest",
        "identityTypeScriptManifest",
      ]),
    );
    for (const file of typescriptManifest.outputs) {
      const output = first.find((candidate) => candidate.path === file.path)!;
      expect(file.sizeBytes).toBe(Buffer.byteLength(output.source));
      expect(file.sha256).toBe(
        `sha256:${createHash("sha256").update(output.source).digest("hex")}`,
      );
    }
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
