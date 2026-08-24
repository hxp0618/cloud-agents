import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import {
  AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH,
  AJV_OFFICIAL_SUITE_OPTIONS,
  AjvOfficialSuiteAuditError,
  ajvOfficialSuiteAuditInputs,
  assertResolvedAjvPackageIdentity,
  assertAjvOfficialSuiteAuditCurrent,
  assertObservedMatchesExpected,
  buildAjvOfficialSuiteAudit,
  requireAjvOfficialSuiteConformance,
  serializeAjvOfficialSuiteAudit,
} from "./platform-ajv-official-suite";

const repositoryRoot = resolve(import.meta.dirname, "../..");

describe("Ajv 8.20.0 Draft 2020-12 official mandatory-suite audit", () => {
  it("is current, deterministic, and records the exact nonconformant result", () => {
    expect(() => assertAjvOfficialSuiteAuditCurrent(repositoryRoot)).not.toThrow();
    const first = buildAjvOfficialSuiteAudit(repositoryRoot) as Record<string, any>;
    const second = buildAjvOfficialSuiteAudit(repositoryRoot);

    expect(serializeAjvOfficialSuiteAudit(first)).toBe(serializeAjvOfficialSuiteAudit(second));
    expect(first).toMatchObject({
      status: "EXECUTED_NONCONFORMANT",
      conformanceClaim: false,
      notGateClosure: true,
      summary: {
        files: 46,
        cases: 383,
        assertions: 1299,
        remotes: 79,
        passedAssertions: 1241,
        compileFailedCases: 7,
        notRunAssertions: 20,
        validityMismatches: 30,
        runtimeErrors: 8,
        discrepancyRecords: 45,
        nonPassingAssertions: 58,
      },
      implementationBoundary: {
        closureCriterion: "remains_missing",
        gateStatus: "all_gates_open",
      },
    });
    expect(first.discrepancies).toHaveLength(45);
    expect(new Set(first.discrepancies.map((item: any) => item.id)).size).toBe(45);
    expect(first.categories.map((item: any) => item.category)).toEqual([
      "dynamicRef",
      "enum",
      "properties",
      "ref",
      "unevaluatedItems",
      "unevaluatedProperties",
      "vocabulary",
    ]);
    expect(
      [
        ...new Set(first.discrepancies.map((item: any) => item.boundary).filter(Boolean)),
      ].toSorted(),
    ).toEqual([
      "DUNDER_PROTO_PROPERTY_FILTERED",
      "EMPTY_ENUM_REJECTED",
      "NON_HASH_DYNAMIC_REF_REJECTED",
      "VOCABULARY_REGISTRATION_BEHAVIOR",
    ]);
    expect(serializeAjvOfficialSuiteAudit(first)).not.toMatch(/generatedAt|\/Users\//u);
  });

  it("locks the fresh-per-case Ajv options and offline remote registry", () => {
    expect(AJV_OFFICIAL_SUITE_OPTIONS).toEqual({
      allErrors: true,
      strict: false,
      validateSchema: true,
      validateFormats: false,
      ownProperties: true,
      removeAdditional: false,
      useDefaults: false,
      coerceTypes: false,
    });
    const audit = JSON.parse(
      readFileSync(resolve(repositoryRoot, AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH), "utf8"),
    );
    expect(audit.execution).toMatchObject({
      freshValidatorPerCase: true,
      remoteRegistry: {
        baseUri: "http://localhost:1234/",
        files: 79,
        registration: "addSchema(schema, uri, false, false)",
        networkFetch: "forbidden",
        loadSchema: "forbidden",
      },
    });
    expect(audit.validator).toMatchObject({
      package: "ajv",
      version: "8.20.0",
      packageManifestSha256: "1f9033ee5a6515e7d76938b7072941862d1ed228a6879cc7fe10cdeb75107989",
      dependencyAuthority: ["package.json", "bun.lock"],
    });
    expect(() =>
      assertResolvedAjvPackageIdentity(repositoryRoot, {
        name: "ajv",
        version: "8.19.0",
        packageManifestSha256: "1f9033ee5a6515e7d76938b7072941862d1ed228a6879cc7fe10cdeb75107989",
      }),
    ).toThrow(/identity mismatch|pin the audited Ajv version/u);
  });

  it("fails closed when the expected totals are mutated", () => {
    const audit = buildAjvOfficialSuiteAudit(repositoryRoot) as Record<string, any>;
    const expected = {
      status: audit.status,
      conformanceClaim: audit.conformanceClaim,
      summary: structuredClone(audit.summary),
      categories: audit.categories,
    };
    expected.summary.passedAssertions -= 1;
    expect(() => assertObservedMatchesExpected(audit.summary, audit.categories, expected)).toThrow(
      AjvOfficialSuiteAuditError,
    );
  });

  it("makes require-conformance fail while normal evidence checking remains usable", () => {
    expect(() => requireAjvOfficialSuiteConformance(repositoryRoot)).toThrow(
      /EXECUTED_NONCONFORMANT/u,
    );
  });

  it("binds every corpus and generator input in sorted order", () => {
    const inputs = ajvOfficialSuiteAuditInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain(
      "tools/contract-standards/vendor/json-schema-test-suite/tests/draft2020-12/dynamicRef.json",
    );
    expect(inputs).toContain(
      "tools/contract-standards/vendor/json-schema-test-suite/remotes/draft2020-12/tree.json",
    );
  });
});
