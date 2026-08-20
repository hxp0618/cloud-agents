import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  assertIdentitySDKCurrent,
  buildIdentitySDKManifests,
  buildIdentitySDKOutputs,
  GO_IDENTITY_MANIFEST_PATH,
  GO_IDENTITY_OUTPUT_PATH,
  identitySDKConfigDigest,
  identitySDKContractInputs,
  TYPESCRIPT_IDENTITY_MANIFEST_PATH,
  TYPESCRIPT_IDENTITY_OUTPUT_PATH,
} from "./platform-identity-sdk";

const root = resolve(import.meta.dirname, "../..");

describe("generated common identity SDK", () => {
  it("uses only the approved common identity authorities", () => {
    const inputs = identitySDKContractInputs(root);
    expect(inputs).toEqual(
      [
        "contracts/common/v1alpha1/fixtures/golden/namespace-ref-canonical.json",
        "contracts/common/v1alpha1/fixtures/golden/namespace-ref-nfc.json",
        "contracts/common/v1alpha1/fixtures/golden/subject-ref-canonical.json",
        "contracts/common/v1alpha1/fixtures/golden/subject-ref.json",
        "contracts/common/v1alpha1/fixtures/manifest.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-canonical-escape.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-canonical-trailing-whitespace.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-decomposed.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-extra-field.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-lone-surrogate.json",
        "contracts/common/v1alpha1/fixtures/negative/namespace-ref-uppercase.json",
        "contracts/common/v1alpha1/fixtures/negative/subject-ref-canonical-escape.json",
        "contracts/common/v1alpha1/fixtures/negative/subject-ref-digest-mismatch.json",
        "contracts/common/v1alpha1/fixtures/negative/subject-ref-extra-field.json",
        "contracts/common/v1alpha1/schemas/namespace-ref.schema.json",
        "contracts/common/v1alpha1/schemas/subject-ref.schema.json",
        "docs/plan/p1/sdk-identity-closure-entry-20260820.md",
      ].toSorted(),
    );
    expect(inputs.some((path) => path.includes("managed-agent"))).toBe(false);
    expect(inputs).toEqual([...inputs].toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
  });

  it("generates closed Go and TypeScript source without service imports", () => {
    const outputs = buildIdentitySDKOutputs(root);
    expect(outputs.map((output) => output.path)).toEqual([
      GO_IDENTITY_OUTPUT_PATH,
      TYPESCRIPT_IDENTITY_OUTPUT_PATH,
    ]);
    for (const output of outputs) {
      expect(output.source).not.toMatch(/\{\{[A-Z0-9_]+\}\}/u);
      expect(output.source).not.toContain("cloud-agents/services/");
      expect(output.source).not.toContain("workspace:");
      expect(output.source).not.toContain("file:");
    }
  });

  it("binds each language output to a deterministic non-Gate manifest", () => {
    const manifests = buildIdentitySDKManifests(root);
    expect(manifests.map((manifest) => manifest.path)).toEqual([
      GO_IDENTITY_MANIFEST_PATH,
      TYPESCRIPT_IDENTITY_MANIFEST_PATH,
    ]);
    expect(identitySDKConfigDigest()).toMatch(/^sha256:[0-9a-f]{64}$/u);
    for (const manifest of manifests) {
      const document = JSON.parse(manifest.source) as {
        language: string;
        notGateClosure: boolean;
        outputTreeSha256: string;
        outputs: Array<{ path: string; sha256: string; sizeBytes: number }>;
        implementationBoundary: Record<string, string | boolean>;
        runtimeDependencies: Array<Record<string, string>>;
      };
      expect(document.notGateClosure).toBe(true);
      expect(document.outputTreeSha256).toMatch(/^sha256:[0-9a-f]{64}$/u);
      expect(document.outputs).toHaveLength(1);
      expect(document.outputs[0]?.sha256).toMatch(/^sha256:[0-9a-f]{64}$/u);
      expect(document.outputs[0]?.sizeBytes).toBeGreaterThan(1_000);
      expect(document.implementationBoundary).toEqual(
        expect.objectContaining({
          gateClosure: false,
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
        }),
      );
      if (document.language === "go") {
        expect(document.runtimeDependencies).toEqual([
          expect.objectContaining({
            module: "golang.org/x/text",
            version: "v0.39.0",
            package: "golang.org/x/text/unicode/norm",
            license: "BSD-3-Clause",
            review: "docs/plan/p1/dependency-reviews/x-text-v0.39.0-go-sdk-use-20260820.md",
            bitsReview: "docs/plan/p1/dependency-reviews/x-text-v0.39.0.md",
          }),
        ]);
      } else {
        expect(document.runtimeDependencies).toEqual([]);
      }
    }
  });

  it("keeps every checked-in generated file byte exact", () => {
    expect(() => assertIdentitySDKCurrent(root)).not.toThrow();
    for (const path of [
      GO_IDENTITY_OUTPUT_PATH,
      GO_IDENTITY_MANIFEST_PATH,
      TYPESCRIPT_IDENTITY_OUTPUT_PATH,
      TYPESCRIPT_IDENTITY_MANIFEST_PATH,
    ]) {
      expect(readFileSync(resolve(root, path), "utf8").length).toBeGreaterThan(0);
    }
  });
});
