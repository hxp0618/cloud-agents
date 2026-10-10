import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  validateCanonicalExpectation,
  validateJsonSchemaDocument,
  validateOpenApiDocument,
  validateP1A1HttpIdempotencyBinding,
  validateProtoSource,
  validateProtoSourceSet,
} from "../../scripts/lib/platform-contracts";

describe("Platform contract bootstrap checks", () => {
  const repositoryRoot = resolve(import.meta.dirname, "../..");

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
    expect(() =>
      validateOpenApiDocument(
        {
          ...base,
          paths: {
            "/v1/a": {
              get: {
                ...base.paths["/v1/a"].get,
                parameters: [
                  { name: "limit", in: "query", schema: { type: "string", $ref: "#/bad" } },
                ],
              },
            },
          },
        },
        "openapi.json",
      ),
    ).toThrow(/external \$ref/);
  });

  it("allows scalar transport schemas while retaining the external model boundary", () => {
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
            parameters: [{ name: "limit", in: "query", schema: { type: "integer", minimum: 1 } }],
            responses: {
              "200": {
                description: "OK",
                headers: { "X-Rate-Limit": { schema: { type: "integer" } } },
              },
            },
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
                ...base.paths["/v1/a"].get,
                responses: {
                  "200": {
                    description: "OK",
                    content: { "text/plain": { schema: { type: "string" } } },
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

  it("scopes P1-A1 idempotency binding to managedAgentCreateProject", () => {
    expect(() =>
      validateP1A1HttpIdempotencyBinding(
        [
          resolve(repositoryRoot, "contracts/managed-host/v1alpha1/openapi.json"),
          resolve(repositoryRoot, "contracts/managed-agent/v1alpha1/openapi.json"),
        ],
        [
          resolve(
            repositoryRoot,
            "contracts/platform/v1alpha1/schemas/managed-agent-create-project-idempotency-projection.schema.json",
          ),
        ],
      ),
    ).not.toThrow();
  });

  it("keeps the Admin identity-management HTTP surface closed to existing operations", () => {
    const document = JSON.parse(
      readFileSync(
        resolve(repositoryRoot, "contracts/managed-agent/v1alpha1/openapi.json"),
        "utf8",
      ),
    ) as { paths: Record<string, Record<string, { operationId: string }>> };
    const actual = Object.entries(document.paths)
      .filter(
        ([path]) =>
          path.startsWith("/v1/admin/tenants/") && !path.includes("/projects/{projectId}/"),
      )
      .flatMap(([path, methods]) =>
        Object.entries(methods).map(
          ([method, operation]) => `${method.toUpperCase()} ${path} ${operation.operationId}`,
        ),
      )
      .sort();
    expect(actual).toEqual(
      [
        "GET /v1/admin/tenants/{tenantId} adminGetPlatformTenant",
        "GET /v1/admin/tenants/{tenantId}/memberships adminListMemberships",
        "GET /v1/admin/tenants/{tenantId}/memberships/{membershipId} adminGetMembership",
        "GET /v1/admin/tenants/{tenantId}/organizations adminListOrganizations",
        "GET /v1/admin/tenants/{tenantId}/organizations/{organizationId} adminGetOrganization",
        "GET /v1/admin/tenants/{tenantId}/projects adminListProjects",
        "GET /v1/admin/tenants/{tenantId}/projects/{projectId} adminGetProject",
        "GET /v1/admin/tenants/{tenantId}/role-bindings adminListRoleBindings",
        "GET /v1/admin/tenants/{tenantId}/role-bindings/{roleBindingId} adminGetRoleBinding",
        "GET /v1/admin/tenants/{tenantId}/roles adminListRoles",
        "GET /v1/admin/tenants/{tenantId}/roles/{roleId} adminGetRole",
        "GET /v1/admin/tenants/{tenantId}/service-accounts adminListServiceAccounts",
        "POST /v1/admin/tenants/{tenantId}/memberships adminCreateMembership",
        "POST /v1/admin/tenants/{tenantId}/memberships/{membershipId}:resume adminResumeMembership",
        "POST /v1/admin/tenants/{tenantId}/memberships/{membershipId}:revoke adminRevokeMembership",
        "POST /v1/admin/tenants/{tenantId}/memberships/{membershipId}:suspend adminSuspendMembership",
        "POST /v1/admin/tenants/{tenantId}/organizations adminCreateOrganization",
        "POST /v1/admin/tenants/{tenantId}/projects adminCreateProject",
        "POST /v1/admin/tenants/{tenantId}/role-bindings adminBindRole",
        "POST /v1/admin/tenants/{tenantId}/role-bindings/{roleBindingId}:revoke adminRevokeRoleBinding",
        "POST /v1/admin/tenants/{tenantId}/service-accounts adminCreateServiceAccount",
        "POST /v1/admin/tenants/{tenantId}/service-accounts/{serviceAccountId}:disable adminDisableServiceAccount",
        "POST /v1/admin/tenants/{tenantId}/service-accounts/{serviceAccountId}:rotate-credential adminRotateServiceAccountCredential",
      ].sort(),
    );
  });

  it("keeps project discovery on the narrow tenant-bound selector route", () => {
    const document = JSON.parse(
      readFileSync(
        resolve(repositoryRoot, "contracts/managed-agent/v1alpha1/openapi.json"),
        "utf8",
      ),
    ) as {
      paths: Record<
        string,
        Record<string, { operationId: string; parameters?: Array<{ $ref?: string }> }>
      >;
    };
    const operation = document.paths["/v1/tenants/{tenantId}/my-projects"]?.get;
    expect(operation?.operationId).toBe("managedAgentListMyProjects");
    expect(operation?.parameters?.map((parameter) => parameter.$ref)).toEqual([
      "#/components/parameters/TenantId",
      "#/components/parameters/RequestId",
      "#/components/parameters/ProjectPageSize",
      "#/components/parameters/ProjectPageToken",
    ]);
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

  it("allows only the pinned public identity JWKS operation to override security", () => {
    const publicJWKS = {
      openapi: "3.1.1",
      jsonSchemaDialect: "https://json-schema.org/draft/2020-12/schema",
      components: {
        securitySchemes: { ServiceAuth: { type: "http", scheme: "bearer" } },
      },
      security: [{ ServiceAuth: [] }],
      paths: {
        "/.well-known/jwks.json": {
          get: {
            operationId: "identityGetJWKS",
            security: [],
            responses: { "200": { description: "OK" } },
          },
        },
      },
    };
    expect(validateOpenApiDocument(publicJWKS, "identity/openapi.json")).toBe(1);
    expect(() =>
      validateOpenApiDocument(
        {
          ...publicJWKS,
          paths: {
            "/v1/identity/session": {
              get: {
                operationId: "identityGetBrowserSession",
                security: [],
                responses: { "200": { description: "OK" } },
              },
            },
          },
        },
        "identity/openapi.json",
      ),
    ).toThrow(/must not allow anonymous access/);
    expect(() =>
      validateOpenApiDocument(
        {
          ...publicJWKS,
          paths: {
            "/wrong-jwks": {
              get: {
                operationId: "identityGetJWKS",
                security: [],
                responses: { "200": { description: "OK" } },
              },
            },
          },
        },
        "identity/openapi.json",
      ),
    ).toThrow(/must not allow anonymous access/);
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
    expect(() =>
      validateProtoSource(
        [
          'syntax = "proto3";',
          "package cloudagents.worker.runtime.v1alpha1;",
          'option go_package = "github.com/hxp0618/cloud-agents/sdk/go/gen/cloudagents/worker/runtime/v1alpha1;workerruntimev1alpha1";',
          "message Runtime { string value = 1; }",
        ].join("\n"),
        "runtime.proto",
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
