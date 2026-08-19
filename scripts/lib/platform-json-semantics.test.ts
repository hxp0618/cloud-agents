import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { validateCompatibilityRecoveryFixture } from "./platform-compatibility-recovery-registry";
import { validateDurableCoordinationFixture } from "./platform-durable-coordination-registry";

import {
  assertExpectedSemanticResult,
  canonicalizeJson,
  canonicalizeNamespaceRef,
  canonicalizeSubjectRef,
  managedAgentCreateProjectIdempotencyDigest,
  managedAgentCreateProjectIdempotencyProjection,
  namespaceRefDigest,
  validateCanonicalNamespaceRefFixture,
  validateCanonicalSubjectRefFixture,
  validateManagedAgentCreateProjectIdempotencyFixture,
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
  it("freezes the v1 built-in role catalog without implicit permission expansion", () => {
    const catalog = JSON.parse(
      readFileSync(
        "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json",
        "utf8",
      ),
    ) as { roles: Array<Record<string, unknown>> };
    expect(validatePlatformSemantics(catalog).valid).toBe(true);

    const reordered = structuredClone(catalog);
    [reordered.roles[0], reordered.roles[1]] = [reordered.roles[1]!, reordered.roles[0]!];
    expect(validatePlatformSemantics(reordered)).toEqual({
      valid: false,
      errors: [{ code: "BUILTIN_ROLE_CATALOG_ORDER_MISMATCH", path: "/roles" }],
    });

    const expanded = structuredClone(catalog);
    const viewer = expanded.roles.find((role) => role.name === "project.viewer");
    (viewer?.permissions as string[]).push("projects.bind");
    expect(validatePlatformSemantics(expanded)).toEqual({
      valid: false,
      errors: [
        {
          code: "BUILTIN_ROLE_PERMISSION_SET_MISMATCH",
          path: `/roles/${expanded.roles.indexOf(viewer!)}/permissions`,
        },
      ],
    });

    const wrongScope = structuredClone(catalog);
    const operator = wrongScope.roles.find((role) => role.name === "project.operator");
    if (operator) operator.scopeLevel = "organization";
    expect(validatePlatformSemantics(wrongScope)).toEqual({
      valid: false,
      errors: [
        {
          code: "ROLE_SCOPE_MISMATCH",
          path: `/roles/${wrongScope.roles.indexOf(operator!)}/scopeLevel`,
        },
      ],
    });
  });

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
        const canonicalResults = [
          validateCanonicalNamespaceRefFixture(document),
          validateCanonicalSubjectRefFixture(document),
          validateManagedAgentCreateProjectIdempotencyFixture(document),
          validateDurableCoordinationFixture(document, resolve(import.meta.dirname, "../..")),
          validateCompatibilityRecoveryFixture(document, resolve(import.meta.dirname, "../..")),
        ];
        const result =
          canonicalResults.find((candidate) => !candidate.valid) ??
          validatePlatformSemantics(instance, document);
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

describe("P1-A1 idempotency canonical authority", () => {
  const subject = {
    kind: "user" as const,
    issuer: "https://Issuer.Example/%7Etenant",
    subject: "Jose\u0301/用户",
  };
  const request = {
    operationId: "managedAgentCreateProject",
    path: { tenantId: "Tenant-A" },
    headers: { idempotencyKey: "idem-one", requestId: "request-one" },
    body: {
      name: "project-alpha",
      organizationRef: {
        namespace: "cloud-agents",
        kind: "organization",
        id: "organization-cafe",
      },
      displayName: "项目 Café",
    },
  };

  it("keeps SubjectRef issuer and subject exact while sorting only keys", () => {
    expect(new TextDecoder().decode(canonicalizeSubjectRef(subject))).toBe(
      '{"issuer":"https://Issuer.Example/%7Etenant","kind":"user","subject":"José/用户"}',
    );
    expect(
      new TextDecoder().decode(
        canonicalizeSubjectRef({ ...subject, issuer: subject.issuer.toLowerCase() }),
      ),
    ).not.toBe(new TextDecoder().decode(canonicalizeSubjectRef(subject)));
    expect(() => canonicalizeSubjectRef({ ...subject, displayName: "unknown" })).toThrow(
      /must contain exactly/,
    );
    expect(validateCanonicalSubjectRefFixture({ instance: subject })).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_SUBJECT_REF_MISMATCH", path: "/canonicalUtf8" }],
    });
    expect(
      validateCanonicalSubjectRefFixture({
        instance: subject,
        canonicalUtf8:
          '{"issuer":"https://Issuer.Example/%7Etenant","kind":"user","subject":"José/用户"}',
      }),
    ).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_SUBJECT_REF_DIGEST_MISMATCH", path: "/digest" }],
    });
    expect(
      validateCanonicalSubjectRefFixture({
        instance: subject,
        canonicalUtf8:
          '{"issuer":"https://Issuer.Example/%7Etenant","kind":"user","subject":"José/用户"}',
        digest: "SHA256:NOT-CANONICAL",
      }),
    ).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_SUBJECT_REF_DIGEST_MISMATCH", path: "/digest" }],
    });
  });

  it("projects only operation, authoritative path, and strict body", () => {
    const projection = managedAgentCreateProjectIdempotencyProjection(request);
    expect(projection).toEqual({
      operationId: "managedAgentCreateProject",
      path: { tenantId: "Tenant-A" },
      body: request.body,
    });
    expect(managedAgentCreateProjectIdempotencyDigest(request)).toBe(
      "sha256:9e6ff0d726c44d07fc37097e8b85893a64033efb094a5514de5b400ff4963e20",
    );
    expect(
      managedAgentCreateProjectIdempotencyDigest({
        ...request,
        headers: {
          idempotencyKey: "different",
          requestId: "different",
          Authorization: "test-placeholder-not-a-credential",
          "Content-Type": "application/json; charset=utf-8",
          traceparent: "00-00000000000000000000000000000001-0000000000000001-01",
          traceMetadata: { sampled: true, source: "fixture" },
        },
      }),
    ).toBe(managedAgentCreateProjectIdempotencyDigest(request));
    expect(() =>
      managedAgentCreateProjectIdempotencyProjection({ ...request, headers: [] }),
    ).toThrow(/headers must be an object/);
    expect(() =>
      managedAgentCreateProjectIdempotencyProjection({
        ...request,
        headers: { idempotencyKey: "present", Authorization: "placeholder" },
      }),
    ).toThrow(/must carry string idempotencyKey and requestId/);
  });

  it("fails closed on operation, authority, canonical bytes, number marker, and digest drift", () => {
    const projection = managedAgentCreateProjectIdempotencyProjection(request);
    const canonicalUtf8 = new TextDecoder().decode(canonicalizeJson(projection));
    const base = {
      request,
      authority: { resolvedOrganizationTenantId: "Tenant-A" },
      projection,
      canonicalUtf8,
      digest: managedAgentCreateProjectIdempotencyDigest(request),
      numberHandling: "NOT_APPLICABLE_NO_NUMBER_FIELDS",
    };
    expect(validateManagedAgentCreateProjectIdempotencyFixture(base).valid).toBe(true);
    expect(
      validateManagedAgentCreateProjectIdempotencyFixture({
        ...base,
        authority: { resolvedOrganizationTenantId: "Tenant-B" },
      }),
    ).toEqual({
      valid: false,
      errors: [
        {
          code: "IDEMPOTENCY_PATH_BODY_AUTHORITY_MISMATCH",
          path: "/authority/resolvedOrganizationTenantId",
        },
      ],
    });
    expect(
      validateManagedAgentCreateProjectIdempotencyFixture({ ...base, numberHandling: "IEEE754" }),
    ).toEqual({
      valid: false,
      errors: [{ code: "IDEMPOTENCY_NUMBER_RULE_MISMATCH", path: "/numberHandling" }],
    });
    expect(
      validateManagedAgentCreateProjectIdempotencyFixture({
        ...base,
        canonicalUtf8: `${canonicalUtf8}\n`,
      }),
    ).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_IDEMPOTENCY_REQUEST_MISMATCH", path: "/canonicalUtf8" }],
    });
    expect(
      validateManagedAgentCreateProjectIdempotencyFixture({
        ...base,
        digest: `sha256:${"0".repeat(64)}`,
      }),
    ).toEqual({
      valid: false,
      errors: [{ code: "CANONICAL_IDEMPOTENCY_REQUEST_DIGEST_MISMATCH", path: "/digest" }],
    });
    expect(() =>
      managedAgentCreateProjectIdempotencyProjection({ ...request, operationId: "renamed" }),
    ).toThrow(/IDEMPOTENCY_OPERATION_ID_MISMATCH/);
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
