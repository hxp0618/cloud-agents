import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  assertExpectedSemanticResult,
  canonicalizeJson,
  canonicalizeNamespaceRef,
  namespaceRefDigest,
  validateCanonicalNamespaceRefFixture,
  validatePlatformSemantics,
} from "./platform-json-semantics";

const ref = { namespace: "cloud-agents", kind: "project", id: "café" };
const canonicalUtf8 = '{"id":"café","kind":"project","namespace":"cloud-agents"}';

describe("NamespaceRef RFC 8785 profile", () => {
  it("derives canonical bytes and digest from the instance", () => {
    expect(new TextDecoder().decode(canonicalizeNamespaceRef(ref))).toBe(canonicalUtf8);
    expect(namespaceRefDigest(ref)).toBe(
      `sha256:${createHash("sha256").update(canonicalUtf8).digest("hex")}`,
    );
  });

  it("canonicalizes the bounded generic JSON subset deterministically", () => {
    expect(
      new TextDecoder().decode(
        canonicalizeJson({ z: [true, null, 1], a: { value: "café", enabled: false } }),
      ),
    ).toBe('{"a":{"enabled":false,"value":"café"},"z":[true,null,1]}');
    expect(() => canonicalizeJson({ invalid: undefined })).toThrow(/undefined/);
    expect(() => canonicalizeJson({ invalid: "\ud800" })).toThrow(/INVALID_UNICODE_SCALAR/);
  });

  it.each([
    [`${canonicalUtf8}\n`, "trailing whitespace"],
    ['{"id":"caf\\u00e9","kind":"project","namespace":"cloud-agents"}', "non-canonical escape"],
  ])("rejects %s (%s)", (candidate) => {
    expect(
      validateCanonicalNamespaceRefFixture({ instance: ref, canonicalUtf8: candidate }),
    ).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_NAMESPACE_REF_MISMATCH", path: "/canonicalUtf8" }],
    });
  });

  it("rejects lone surrogates and non-NFC identifiers", () => {
    expect(
      validateCanonicalNamespaceRefFixture({ instance: { ...ref, id: "\ud800" }, canonicalUtf8 }),
    ).toEqual({
      valid: false,
      errors: [{ code: "INVALID_UNICODE_SCALAR", path: "/instance/id" }],
    });
    expect(validatePlatformSemantics({ ...ref, id: "cafe\u0301" })).toEqual({
      valid: false,
      errors: [{ code: "NON_NFC_NAMESPACE_REF_ID", path: "/id" }],
    });
  });

  it("rejects invalid NamespaceRef grammar and Unicode code-point bounds", () => {
    expect(validatePlatformSemantics({ ...ref, namespace: "Cloud-Agents" })).toEqual({
      valid: false,
      errors: [{ code: "INVALID_NAMESPACE_REF_GRAMMAR", path: "/namespace" }],
    });
    expect(validatePlatformSemantics({ ...ref, kind: "" })).toEqual({
      valid: false,
      errors: [{ code: "INVALID_NAMESPACE_REF_LENGTH", path: "/kind" }],
    });
    expect(validatePlatformSemantics({ ...ref, id: "😀".repeat(256) }).valid).toBe(true);
    expect(validatePlatformSemantics({ ...ref, id: "😀".repeat(257) })).toEqual({
      valid: false,
      errors: [{ code: "INVALID_NAMESPACE_REF_LENGTH", path: "/id" }],
    });
  });
});

describe("platform semantic constraints", () => {
  it("compares normalized NamespaceRef tuples by canonical digest", () => {
    const instance = {
      kind: "Project",
      metadata: { tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-a" } },
      spec: {
        tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-a" },
        organizationRef: { namespace: "cloud-agents", kind: "organization", id: "org-b" },
      },
    };
    const document = {
      instance,
      resolvedReferences: {
        "org-b": { tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-b" } },
      },
    };
    const result = validatePlatformSemantics(instance, document);
    expect(result.valid).toBe(false);
    if (!result.valid) expect(result.errors[0]?.code).toBe("CROSS_TENANT_REFERENCE");
    expect(() =>
      assertExpectedSemanticResult(result, false, "CROSS_TENANT_REFERENCE"),
    ).not.toThrow();
  });

  it("returns stable codes for scope, role, and wildcard violations", () => {
    expect(
      validatePlatformSemantics({
        kind: "RoleBinding",
        spec: {
          roleName: "project.viewer",
          scope: {
            level: "organization",
            ref: { namespace: "cloud-agents", kind: "project", id: "project-a" },
          },
        },
      }),
    ).toEqual({
      valid: false,
      errors: [
        { code: "SCOPE_KIND_MISMATCH", path: "/spec/scope/ref/kind" },
        { code: "ROLE_SCOPE_MISMATCH", path: "/spec/scope/level" },
      ],
    });
    expect(
      validatePlatformSemantics({
        kind: "Role",
        spec: { name: "project.admin", permissions: ["projects.*"] },
      }),
    ).toEqual({
      valid: false,
      errors: [{ code: "WILDCARD_PERMISSION_FORBIDDEN", path: "/spec/permissions/0" }],
    });
  });

  it("matches every semantic fixture's stable expectedError exactly", () => {
    for (const manifestPath of [
      "contracts/common/v1alpha1/fixtures/manifest.json",
      "contracts/platform/v1alpha1/fixtures/manifest.json",
    ]) {
      const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as {
        cases: Array<Record<string, unknown>>;
      };
      for (const fixture of manifest.cases) {
        if (typeof fixture.expectedSemanticValid !== "boolean") continue;
        const fixturePath = resolve(
          dirname(manifestPath),
          String(fixture.document ?? fixture.instance),
        );
        const document = JSON.parse(readFileSync(fixturePath, "utf8")) as unknown;
        const instance =
          typeof fixture.instancePointer === "string"
            ? resolvePointer(document, fixture.instancePointer)
            : document;
        const canonicalResult = validateCanonicalNamespaceRefFixture(document);
        const result = canonicalResult.valid
          ? validatePlatformSemantics(instance, document)
          : canonicalResult;
        expect(
          () =>
            assertExpectedSemanticResult(
              result,
              fixture.expectedSemanticValid as boolean,
              fixture.expectedError,
            ),
          String(fixture.name),
        ).not.toThrow();
      }
    }
  });
});

function resolvePointer(document: unknown, pointer: string): unknown {
  if (pointer === "") return document;
  let value = document;
  for (const segment of pointer.slice(1).split("/")) {
    const key = segment.replaceAll("~1", "/").replaceAll("~0", "~");
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      throw new Error(`Invalid fixture pointer ${pointer}.`);
    }
    value = (value as Record<string, unknown>)[key];
  }
  return value;
}
