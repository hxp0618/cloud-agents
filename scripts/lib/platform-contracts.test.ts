import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";

import {
  validateCanonicalExpectation,
  validateJsonSchemaDocument,
  validateOpenApiDocument,
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
});
