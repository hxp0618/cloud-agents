import { createHash } from "node:crypto";
import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  validateCanonicalExpectation,
  validateJsonSchemaDocument,
  validateOpenApiDocument,
  validatePlatformContractTree,
  validateProtoSource,
  validateProtoSourceSet,
} from "./platform-contracts";

describe("Platform contract bootstrap checks", () => {
  it("requires strict JSON Schema objects", () => {
    expect(() =>
      validateJsonSchemaDocument(
        {
          $schema: "https://json-schema.org/draft/2020-12/schema",
          $id: "https://schemas.cloud-agents.dev/test.schema.json",
          type: "object",
          properties: {},
        },
        "test.schema.json",
      ),
    ).toThrow(/additionalProperties=false/);
  });

  it("rejects inline OpenAPI models and duplicate operations", () => {
    const base = {
      openapi: "3.1.1",
      jsonSchemaDialect: "https://json-schema.org/draft/2020-12/schema",
      components: {
        securitySchemes: { BearerAuth: { type: "http", scheme: "bearer" } },
      },
      security: [{ BearerAuth: [] }],
      paths: {
        "/v1/a": {
          get: {
            operationId: "readThing",
            responses: { "200": { description: "OK" } },
          },
        },
      },
    };
    expect(validateOpenApiDocument(base, "openapi.json")).toBe(1);
    expect(() =>
      validateOpenApiDocument(
        {
          ...base,
          paths: {
            "/v1/a": {
              get: {
                operationId: "readThing",
                responses: {
                  "200": {
                    description: "OK",
                    content: {
                      "application/json": { schema: { type: "object", properties: {} } },
                    },
                  },
                },
              },
            },
          },
        },
        "openapi.json",
      ),
    ).toThrow(/external \$ref/);
  });

  it("fails closed on missing OpenAPI refs, security schemes, and path bindings", () => {
    const base = {
      openapi: "3.1.1",
      jsonSchemaDialect: "https://json-schema.org/draft/2020-12/schema",
      components: {
        securitySchemes: { BearerAuth: { type: "http", scheme: "bearer" } },
        parameters: {
          TenantId: {
            name: "tenantId",
            in: "path",
            required: true,
            schema: { $ref: "../identifier.schema.json" },
          },
        },
      },
      security: [{ BearerAuth: [] }],
      paths: {
        "/v1/tenants/{tenantId}": {
          get: {
            operationId: "getTenant",
            parameters: [{ $ref: "#/components/parameters/TenantId" }],
            responses: { "200": { description: "OK" } },
          },
        },
      },
    };
    expect(validateOpenApiDocument(base, "openapi.json")).toBe(1);
    expect(() =>
      validateOpenApiDocument({ ...base, security: [{ MissingScheme: [] }] }, "openapi.json"),
    ).toThrow(/missing security scheme/);
    expect(() =>
      validateOpenApiDocument(
        {
          ...base,
          paths: {
            "/v1/tenants/{tenantId}": {
              get: {
                operationId: "getTenant",
                parameters: [{ $ref: "#/components/parameters/Missing" }],
                responses: { "200": { description: "OK" } },
              },
            },
          },
        },
        "openapi.json",
      ),
    ).toThrow(/missing segment Missing/);
    expect(() =>
      validateOpenApiDocument(
        {
          ...base,
          paths: {
            "/v1/tenants/{tenantId}": {
              get: {
                operationId: "getTenant",
                responses: { "200": { description: "OK" } },
              },
            },
          },
        },
        "openapi.json",
      ),
    ).toThrow(/does not bind path parameter tenantId/);
  });

  it("rejects duplicate OpenAPI operation IDs across surfaces", () => {
    const operationIds = new Set<string>();
    const document = {
      openapi: "3.1.1",
      jsonSchemaDialect: "https://json-schema.org/draft/2020-12/schema",
      components: {
        securitySchemes: { BearerAuth: { type: "http", scheme: "bearer" } },
      },
      security: [{ BearerAuth: [] }],
      paths: {
        "/v1/a": {
          get: { operationId: "sameOperation", responses: { "200": { description: "OK" } } },
        },
      },
    };
    expect(validateOpenApiDocument(document, "one.json", { operationIds })).toBe(1);
    expect(() => validateOpenApiDocument(document, "two.json", { operationIds })).toThrow(
      /duplicates sameOperation/,
    );
  });

  it("checks canonical NamespaceRef digests and key order", () => {
    const canonicalUtf8 = '{"id":"project-123","kind":"project","namespace":"cloud-agents"}';
    const instance = { namespace: "cloud-agents", kind: "project", id: "project-123" };
    const digest = createHash("sha256").update(canonicalUtf8).digest("hex");
    expect(() =>
      validateCanonicalExpectation(
        {
          canonicalUtf8,
          digest: `sha256:${digest}`,
          urn: `urn:cloud-agents:ref:sha256:${digest}`,
        },
        "fixture.json",
        instance,
      ),
    ).not.toThrow();
    expect(() =>
      validateCanonicalExpectation(
        {
          canonicalUtf8: '{"namespace":"cloud-agents","kind":"project","id":"project-123"}',
          digest: `sha256:${digest}`,
          urn: `urn:cloud-agents:ref:sha256:${digest}`,
        },
        "fixture.json",
        instance,
      ),
    ).toThrow();
  });

  it("rejects malformed or wrongly targeted Proto sources", () => {
    expect(() =>
      validateProtoSource(
        'syntax = "proto3";\npackage cloudagents.worker.v1alpha1;\noption go_package = "example.invalid/private";\n',
        "worker.proto",
      ),
    ).toThrow(/public Go SDK/);

    const validHeader = [
      'syntax = "proto3";',
      "package cloudagents.worker.v1alpha1;",
      'option go_package = "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1;workerv1alpha1";',
    ].join("\n");
    expect(() =>
      validateProtoSource(
        `${validHeader}\nmessage Broken { string first = 1; uint32 second = 1; }`,
        "worker.proto",
      ),
    ).toThrow(/duplicates field number 1/);
    expect(() =>
      validateProtoSource(
        `${validHeader}\n// braces in comments do not count: {}}\nmessage Healthy { string value = 1; }`,
        "worker.proto",
      ),
    ).not.toThrow();
  });

  it("requires imported, resolvable Proto types", () => {
    const header = [
      'syntax = "proto3";',
      "package cloudagents.worker.v1alpha1;",
      'option go_package = "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/v1alpha1;workerv1alpha1";',
    ].join("\n");
    const sources = {
      "contracts/worker/v1alpha1/kernel.proto": `${header}\nmessage Shared { string value = 1; }`,
      "contracts/worker/v1alpha1/service.proto": `${header}\nmessage Consumer { Shared value = 1; }`,
    };
    expect(() => validateProtoSourceSet(sources)).toThrow(/unknown Proto type Shared/);
    expect(() =>
      validateProtoSourceSet({
        ...sources,
        "contracts/worker/v1alpha1/service.proto": `${header}\nimport "contracts/worker/v1alpha1/kernel.proto";\nmessage Consumer { Shared value = 1; }`,
      }),
    ).not.toThrow();
    expect(() =>
      validateProtoSourceSet({
        "contracts/worker/v1alpha1/broken.proto": `${header}\nmessage Consumer { Missing value = 1; }`,
      }),
    ).toThrow(/unknown Proto type Missing/);
  });

  it("binds Proto descriptor signatures and rejects unknown fixture fields", () => {
    const root = resolve(import.meta.dirname, "../..");
    const temporary = mkdtempSync(join(tmpdir(), "cloud-agents-contract-fault-"));
    try {
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), { recursive: true });
      const protoPath = resolve(temporary, "contracts/worker/v1alpha1/worker_supervisor.proto");
      writeFileSync(
        protoPath,
        readFileSync(protoPath, "utf8").replace(
          "rpc ExecuteOperation(OperationAttemptEnvelope)",
          "rpc ExecuteOperation(NamespaceRef)",
        ),
      );
      expect(() => validatePlatformContractTree(temporary)).toThrow(/signature mismatch/);

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const fixturePath = resolve(temporary, "contracts/worker/v1alpha1/fixtures/negative.json");
      const fixture = JSON.parse(readFileSync(fixturePath, "utf8")) as {
        cases: Array<Record<string, unknown>>;
      };
      fixture.cases[0]!.unexpected = true;
      writeFileSync(fixturePath, `${JSON.stringify(fixture, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(/unknown fields: unexpected/);

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const adapterFixturePath = resolve(
        temporary,
        "contracts/platform-adapter/v1alpha1/fixtures/golden.json",
      );
      const adapterFixture = JSON.parse(readFileSync(adapterFixturePath, "utf8")) as {
        cases: Array<{ protoJson?: Record<string, unknown> }>;
      };
      adapterFixture.cases[1]!.protoJson!.canonicalRegistrationSha256 =
        "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=";
      writeFileSync(adapterFixturePath, `${JSON.stringify(adapterFixture, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /canonicalRegistrationSha256 does not match/,
      );

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const negative = JSON.parse(readFileSync(fixturePath, "utf8")) as {
        cases: Array<{ expected: Record<string, unknown> }>;
      };
      negative.cases[0]!.expected.code = "TOTALLY_WRONG_CODE";
      writeFileSync(fixturePath, `${JSON.stringify(negative, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(/unknown stable error code/);

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const workerNegative = JSON.parse(readFileSync(fixturePath, "utf8")) as {
        cases: Array<{ id: string; protoJson?: Record<string, unknown> }>;
      };
      workerNegative.cases.find(
        (entry) => entry.id === "namespace-ref-uppercase-namespace",
      )!.protoJson!.namespace = "cloud-agents";
      writeFileSync(fixturePath, `${JSON.stringify(workerNegative, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /does not demonstrate expected cause invalid_namespace_ref/,
      );

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const versionNegative = JSON.parse(readFileSync(fixturePath, "utf8")) as {
        cases: Array<{
          id: string;
          protoJson?: { supportedVersions?: Array<Record<string, unknown>> };
        }>;
      };
      versionNegative.cases.find(
        (entry) => entry.id === "unknown-protocol-major",
      )!.protoJson!.supportedVersions![0]!.major = 1;
      writeFileSync(fixturePath, `${JSON.stringify(versionNegative, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /does not demonstrate expected cause unsupported_protocol_version/,
      );

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const capabilityNegative = JSON.parse(readFileSync(fixturePath, "utf8")) as {
        cases: Array<{ id: string; protoJson?: Record<string, unknown> }>;
      };
      capabilityNegative.cases.find(
        (entry) => entry.id === "unknown-required-capability",
      )!.protoJson!.requiredCapabilities = ["CAPABILITY_HEALTH"];
      writeFileSync(fixturePath, `${JSON.stringify(capabilityNegative, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /does not demonstrate expected cause unknown_required_capability/,
      );

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const recovery = JSON.parse(readFileSync(adapterFixturePath, "utf8")) as {
        cases: Array<{ id: string; admissionState?: Record<string, unknown> }>;
      };
      recovery.cases.find(
        (entry) => entry.id === "adapter-registration-receipt-recovered-after-response-loss",
      )!.admissionState!.registrationRecord = "PRESENT_WITH_DIFFERENT_DIGEST";
      writeFileSync(adapterFixturePath, `${JSON.stringify(recovery, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /accepted receipt recovery is not bound/,
      );

      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const adapterNegativePath = resolve(
        temporary,
        "contracts/platform-adapter/v1alpha1/fixtures/negative.json",
      );
      const receiptMismatch = JSON.parse(readFileSync(adapterNegativePath, "utf8")) as {
        cases: Array<{ id: string; protoJson?: Record<string, unknown> }>;
      };
      receiptMismatch.cases.find(
        (entry) => entry.id === "adapter-registration-receipt-lookup-digest-mismatch",
      )!.protoJson!.canonicalRegistrationSha256 = "m6EBpRwDcwWgDdFTh2gr4+7jZB+H5+P4IRhcG3tox1I=";
      writeFileSync(adapterNegativePath, `${JSON.stringify(receiptMismatch, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /does not demonstrate a registration receipt digest mismatch/,
      );
    } finally {
      rmSync(temporary, { force: true, recursive: true });
    }
  });

  it("fails closed when P1-A1 SubjectRef or HTTP idempotency evidence drifts", () => {
    const root = resolve(import.meta.dirname, "../..");
    const temporary = mkdtempSync(join(tmpdir(), "cloud-agents-p1-a1-faults-"));
    try {
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const subjectPath = resolve(
        temporary,
        "contracts/common/v1alpha1/fixtures/golden/subject-ref-canonical.json",
      );
      const subject = JSON.parse(readFileSync(subjectPath, "utf8")) as Record<string, unknown>;
      subject.digest = `sha256:${"0".repeat(64)}`;
      writeFileSync(subjectPath, `${JSON.stringify(subject, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /Expected semantic valid=true, got false/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const subjectWithoutCanonical = JSON.parse(readFileSync(subjectPath, "utf8")) as Record<
        string,
        unknown
      >;
      delete subjectWithoutCanonical.canonicalUtf8;
      writeFileSync(subjectPath, `${JSON.stringify(subjectWithoutCanonical, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /Expected semantic valid=true, got false/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const operationPath = resolve(
        temporary,
        "contracts/platform/v1alpha1/fixtures/golden/managed-agent-create-project-idempotency.json",
      );
      const operation = JSON.parse(readFileSync(operationPath, "utf8")) as {
        request: Record<string, unknown>;
      };
      operation.request.operationId = "managedAgentCreateProjectRenamed";
      writeFileSync(operationPath, `${JSON.stringify(operation, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /Expected semantic valid=true, got false/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const projectionSchemaPath = resolve(
        temporary,
        "contracts/platform/v1alpha1/schemas/managed-agent-create-project-idempotency-projection.schema.json",
      );
      const projectionSchema = JSON.parse(readFileSync(projectionSchemaPath, "utf8")) as {
        properties: { operationId: { const: string } };
      };
      projectionSchema.properties.operationId.const = "managedAgentCreateProjectRenamed";
      writeFileSync(projectionSchemaPath, `${JSON.stringify(projectionSchema, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /idempotency projection is not bound to managedAgentCreateProject/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const projection = JSON.parse(readFileSync(operationPath, "utf8")) as {
        projection: Record<string, unknown>;
      };
      projection.projection.idempotencyKey = "must-not-enter-projection";
      writeFileSync(operationPath, `${JSON.stringify(projection, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(/expected schema valid=true/);

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const commonManifestPath = resolve(
        temporary,
        "contracts/common/v1alpha1/fixtures/manifest.json",
      );
      const commonManifest = JSON.parse(readFileSync(commonManifestPath, "utf8")) as {
        cases: Array<{ name: string }>;
      };
      commonManifest.cases = commonManifest.cases.filter(
        (fixture) => fixture.name !== "subject-ref-canonical",
      );
      writeFileSync(commonManifestPath, `${JSON.stringify(commonManifest, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /P1-A1 fixture inventory is missing subject-ref-canonical/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const platformManifestPath = resolve(
        temporary,
        "contracts/platform/v1alpha1/fixtures/manifest.json",
      );
      const platformManifest = JSON.parse(readFileSync(platformManifestPath, "utf8")) as {
        cases: Array<Record<string, unknown>>;
      };
      const canonicalFixture = platformManifest.cases.find(
        (fixture) => fixture.name === "managed-agent-create-project-idempotency",
      );
      if (!canonicalFixture) throw new Error("test setup fixture missing");
      delete canonicalFixture.expectedSemanticValid;
      writeFileSync(platformManifestPath, `${JSON.stringify(platformManifest, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /P1-A1 fixture inventory metadata drifted for managed-agent-create-project-idempotency/,
      );

      rmSync(resolve(temporary, "contracts"), { force: true, recursive: true });
      cpSync(resolve(root, "contracts"), resolve(temporary, "contracts"), {
        force: true,
        recursive: true,
      });
      const openApiPath = resolve(temporary, "contracts/managed-agent/v1alpha1/openapi.json");
      const openApi = JSON.parse(readFileSync(openApiPath, "utf8")) as {
        paths: Record<string, unknown>;
      };
      openApi.paths["/v1/p1-a1-second-idempotent"] = {
        parameters: [
          {
            name: "iDeMpOtEnCy-KeY",
            in: "header",
            required: true,
            schema: {
              $ref: "../../common/v1alpha1/schemas/idempotency-key.schema.json",
            },
          },
        ],
        post: {
          operationId: "managedAgentSecondIdempotentMutation",
          responses: { "204": { description: "No Content" } },
        },
      };
      writeFileSync(openApiPath, `${JSON.stringify(openApi, null, 2)}\n`);
      expect(() => validatePlatformContractTree(temporary)).toThrow(
        /must bind exactly one idempotent HTTP mutation/,
      );
    } finally {
      rmSync(temporary, { force: true, recursive: true });
    }
  });
});
