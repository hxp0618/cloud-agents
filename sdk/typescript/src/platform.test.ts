import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  Client,
  decodeIdempotency,
  decodeMembership,
  decodeOrganization,
  decodeOrganizationPage,
  decodePlatformTenant,
  decodeProject,
  decodeProjectPage,
  decodeProjectCreateRequest,
  decodeManagedAgentSession,
  decodeManagedAgentSessionPage,
  decodeManagedAgentExecution,
  decodeRole,
  decodeRoleBinding,
  decodeWatchCursor,
  encodeResponse,
  parseProblem,
  parseProject,
  parseProjectCreateRequest,
  parseManagedAgentSession,
  parseManagedAgentSessionPage,
  parseManagedAgentExecution,
  parseWatchCursor,
  validateProjectResolvedOrganization,
  type FixtureResponse,
  type FixtureRequest,
} from "./platform";

const commonFixtureRoot = resolve(
  import.meta.dirname,
  "../../../contracts/common/v1alpha1/fixtures",
);
const platformFixtureRoot = resolve(
  import.meta.dirname,
  "../../../contracts/platform/v1alpha1/fixtures",
);

describe("generated platform JSON models", () => {
  it("replays the managed-agent Session contract and client lifecycle", async () => {
    const session = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "Session",
      metadata: {
        uid: "session-alpha",
        projectId: "project-alpha",
        resourceVersion: "2",
        createdAt: "2026-08-29T08:00:00Z",
        updatedAt: "2026-08-29T08:01:00Z",
      },
      spec: { providerKind: "codex", state: "active" },
    });
    const sessionPage = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "SessionPage",
      sessions: [JSON.parse(session)],
      nextPageToken: "session-page-token-1",
    });
    expect(decodeManagedAgentSession(JSON.parse(session)).spec.providerKind).toBe("codex");
    expect(parseManagedAgentSession(session).value.kind).toBe("Session");
    expect(decodeManagedAgentSessionPage(JSON.parse(sessionPage)).sessions).toHaveLength(1);
    expect(parseManagedAgentSessionPage(sessionPage).value.nextPageToken).toBe(
      "session-page-token-1",
    );
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status:
          request.method === "POST" && request.path.endsWith("/sessions")
            ? 201
            : request.method === "GET"
              ? 200
              : 200,
        headers: { "X-Resource-Version": "2" },
        body:
          request.method === "GET" && request.path.includes("/sessions?")
            ? sessionPage
            : session,
      };
    });
    await client.createManagedAgentSession(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
      { sessionId: "session-alpha", providerKind: "codex" },
    );
    await client.getManagedAgentSession(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
    );
    await client.listManagedAgentSessions(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "session-page-token-1",
    );
    await client.closeManagedAgentSession(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R3",
    );
    expect(seen).toHaveLength(4);
    expect(seen[0]?.body).toBe('{"sessionId":"session-alpha","providerKind":"codex"}');
    expect(seen[2]?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1",
    );
    expect(seen[3]?.body).toBeUndefined();
  });
  it("replays the managed-agent Execution contract and client lifecycle", async () => {
    const execution = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "Execution",
      metadata: {
        uid: "execution-alpha",
        projectId: "project-alpha",
        sessionId: "session-alpha",
        turnId: "turn-alpha",
        resourceVersion: "3",
        createdAt: "2026-08-29T08:00:00Z",
        updatedAt: "2026-08-29T08:01:00Z",
      },
      spec: { generation: 1, state: "succeeded", resultDigest: `sha256:${"a".repeat(64)}` },
      messages: [
        {
          requestId: "request-alpha",
          protocolVersion: { major: 2, minor: 3 },
          executionId: "execution-alpha",
          generation: 1,
          commandId: "command-alpha",
          occurredAt: "2026-08-29T08:01:00Z",
          messageType: "Result",
          payload: { text: "done" },
        },
      ],
    });
    expect(decodeManagedAgentExecution(JSON.parse(execution)).messages?.[0]?.messageType).toBe(
      "Result",
    );
    expect(parseManagedAgentExecution(execution).value.kind).toBe("Execution");
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return { status: 200, headers: { "X-Resource-Version": "3" }, body: execution };
    });
    await client.executeManagedAgent(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
      { turnId: "turn-alpha", executionId: "execution-alpha", model: "codex", inputText: "hello" },
    );
    await client.getManagedAgentExecution(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-alpha",
    );
    await client.cancelManagedAgentExecution(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-cancel",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EX",
      { generation: 1 },
    );
    await client.interruptManagedAgentExecution(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-interrupt",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9EY",
      { generation: 1 },
    );
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions",
      "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt",
    ]);
    expect(seen[0]?.body).toBe(
      '{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":"hello","model":"codex"}',
    );
    expect(seen[2]?.body).toBe('{"generation":1}');
    expect(seen[3]?.body).toBe('{"generation":1}');
  });
  it("replays common and platform golden fixtures", () => {
    expect(parseProblem(readFixture(commonFixtureRoot, "golden/problem.json")).status).toBe(404);
    expect(decodeIdempotency(readJSON(commonFixtureRoot, "golden/idempotency.json")).key).toContain(
      "idem-",
    );
    expect(
      decodeWatchCursor(readJSON(commonFixtureRoot, "golden/watch-cursor.json")).resourceVersion,
    ).toBe("42");
    expect(
      decodePlatformTenant(readJSON(platformFixtureRoot, "golden/platform-tenant.json")).kind,
    ).toBe("PlatformTenant");
    expect(decodeOrganization(readJSON(platformFixtureRoot, "golden/organization.json")).kind).toBe(
      "Organization",
    );
    expect(
      decodeOrganizationPage(readJSON(platformFixtureRoot, "golden/organization-page.json"))
        .organizations,
    ).toHaveLength(1);
    expect(decodeProject(readJSON(platformFixtureRoot, "golden/project.json")).kind).toBe(
      "Project",
    );
    expect(decodeProjectPage(readJSON(platformFixtureRoot, "golden/project-page.json")).projects).toHaveLength(1);
    expect(
      decodeProjectCreateRequest(
        readJSON(platformFixtureRoot, "golden/project-create-request.json"),
      ).name,
    ).toBe("project-alpha");
    expect(decodeMembership(readJSON(platformFixtureRoot, "golden/membership.json")).kind).toBe(
      "Membership",
    );
    expect(decodeRole(readJSON(platformFixtureRoot, "golden/role.json")).spec.name).toBe(
      "project.viewer",
    );
    expect(decodeRoleBinding(readJSON(platformFixtureRoot, "golden/role-binding.json")).kind).toBe(
      "RoleBinding",
    );
  });

  it("rejects mutation, Unicode identity, duplicate, and trailing input", () => {
    expect(() =>
      decodeProjectCreateRequest(
        readJSON(platformFixtureRoot, "negative/project-create-server-owned-field.json"),
      ),
    ).toThrow(expect.objectContaining({ code: "UNKNOWN_FIELD" }));
    expect(() =>
      decodeOrganization(
        readJSON(platformFixtureRoot, "negative/organization-tenant-ref-mismatch.json"),
      ),
    ).toThrow(expect.objectContaining({ code: "TENANT_AUTHORITY_MISMATCH" }));
    expect(() =>
      decodeRoleBinding(readJSON(platformFixtureRoot, "negative/role-binding-scope-mismatch.json")),
    ).toThrow(expect.objectContaining({ code: "ROLE_SCOPE_MISMATCH" }));
    expect(() =>
      decodeRoleBinding(readJSON(platformFixtureRoot, "negative/role-binding-unknown-role.json")),
    ).toThrow(expect.objectContaining({ code: "UNKNOWN_ROLE" }));
    expect(() =>
      decodeRole(readJSON(platformFixtureRoot, "negative/role-wildcard-permission.json")),
    ).toThrow(expect.objectContaining({ code: "WILDCARD_PERMISSION_FORBIDDEN" }));
    expect(() =>
      decodeProjectCreateRequest({
        name: "project-alpha",
        organizationRef: {
          namespace: "cloud-agents",
          kind: "organization",
          id: "organization-café",
        },
        displayName: "Project Alpha",
      }),
    ).toThrow(expect.objectContaining({ code: "INVALID_IDENTIFIER" }));
    expect(() =>
      parseProjectCreateRequest(
        '{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha","name":"again"}',
      ),
    ).toThrow(expect.objectContaining({ code: "DUPLICATE_FIELD" }));
    expect(() =>
      parseProjectCreateRequest(
        '{"name":"project-alpha","organizationRef":{"namespace":"cloud-agents","kind":"organization","id":"organization-alpha"},"displayName":"Project Alpha"}[]',
      ),
    ).toThrow(expect.objectContaining({ code: "TRAILING_JSON" }));
  });

  it("preserves response-only unknown fields in the sidecar", () => {
    const project = readFixture(platformFixtureRoot, "negative/project-response-n-minus-one.json");
    const projectEnvelope = parseProject(project);
    expect(projectEnvelope.unknown).toEqual({
      "/futureField": '{\n    "version": 2\n  }',
      "/spec/future~1field~0v2": "9007199254740993",
    });
    expect(JSON.parse(encodeResponse(projectEnvelope))).toEqual(JSON.parse(project));
    expect(() => decodeProject(JSON.parse(project))).toThrow(
      expect.objectContaining({ code: "UNKNOWN_FIELD" }),
    );

    const watch = readFixture(commonFixtureRoot, "negative/watch-cursor-extra-field.json");
    const watchEnvelope = parseWatchCursor(watch);
    expect(watchEnvelope.unknown).toEqual({ "/tenantId": '"cross-tenant-leak"' });
    expect(JSON.parse(encodeResponse(watchEnvelope))).toEqual(JSON.parse(watch));
    expect(() => decodeWatchCursor(JSON.parse(watch))).toThrow(
      expect.objectContaining({ code: "UNKNOWN_FIELD" }),
    );

    expect(encodeResponse(projectEnvelope)).toContain('"future/field~v2":9007199254740993');
    expect(() =>
      encodeResponse({
        value: projectEnvelope.value,
        unknown: { "/spec/state": '"future"' },
      }),
    ).toThrow(expect.objectContaining({ code: "SIDECAR_FIELD_COLLISION" }));
    expect(() =>
      encodeResponse({
        value: projectEnvelope.value,
        unknown: { "spec/future": "true" },
      }),
    ).toThrow(expect.objectContaining({ code: "INVALID_SIDECAR_POINTER" }));
    expect(() =>
      encodeResponse({
        value: projectEnvelope.value,
        unknown: { "/future": "{}", "/future/nested": "true" },
      }),
    ).toThrow(expect.objectContaining({ code: "SIDECAR_POINTER_OVERLAP" }));
  });

  it("rejects a resolved organization from another tenant", () => {
    const wrapper = readJSON(platformFixtureRoot, "negative/cross-tenant-project.json") as {
      instance: unknown;
      resolvedReferences: Record<
        string,
        { tenantRef: { namespace: string; kind: string; id: string } }
      >;
    };
    const project = decodeProject(wrapper.instance);
    expect(() =>
      validateProjectResolvedOrganization(
        project,
        wrapper.resolvedReferences["organization-beta"]!.tenantRef,
      ),
    ).toThrow(expect.objectContaining({ code: "CROSS_TENANT_REFERENCE" }));
  });
});

describe("generated fixture client", () => {
  it("drives all declared operations through an injected transport", async () => {
    const projectBody = readFixture(platformFixtureRoot, "golden/project.json");
    const projectGetBody = projectBody.replace(
      `"name": "project-alpha"`,
      `"name": "project-roundtrip"`,
    );
    expect(projectGetBody).not.toBe(projectBody);
    const projectGetResponse = {
      ...fixtureResponse(platformFixtureRoot, "project", 200),
      body: projectGetBody,
    };
    const request = decodeProjectCreateRequest(
      readJSON(platformFixtureRoot, "golden/project-create-request.json"),
    );
    const responses: Record<string, FixtureResponse> = {
      "GET /v1/tenants/tenant-alpha": fixtureResponse(platformFixtureRoot, "platform-tenant", 200),
      "GET /v1/tenants/tenant-alpha/organizations/organization-alpha": fixtureResponse(
        platformFixtureRoot,
        "organization",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/organizations?pageSize=1&pageToken=organization-page-token-1": {
        status: 200,
        headers: {},
        body: readFixture(platformFixtureRoot, "golden/organization-page.json"),
      },
      "GET /v1/tenants/tenant-alpha/projects/project-alpha": projectGetResponse,
      "GET /v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1": {
        status: 200,
        headers: {},
        body: readFixture(platformFixtureRoot, "golden/project-page.json"),
      },
      "GET /v1/tenants/tenant-alpha/memberships/membership-alpha": fixtureResponse(
        platformFixtureRoot,
        "membership",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/roles/role-project-viewer-v1": fixtureResponse(
        platformFixtureRoot,
        "role",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/role-bindings/role-binding-alpha": fixtureResponse(
        platformFixtureRoot,
        "role-binding",
        200,
      ),
      "GET /v1/managed-host/tenants/tenant-alpha/projects/project-alpha": projectGetResponse,
      "GET /v1/managed-host/tenants/tenant-alpha/role-bindings/role-binding-alpha": fixtureResponse(
        platformFixtureRoot,
        "role-binding",
        200,
      ),
      "POST /v1/tenants/tenant-alpha/projects": {
        status: 201,
        headers: { "X-Resource-Version": "3" },
        body: projectBody,
      },
      "POST /v1/tenants/tenant-alpha/organizations": fixtureResponse(
        platformFixtureRoot,
        "organization",
        201,
      ),
    };
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return responses[`${request.method} ${request.path}`]!;
    });
    const args = ["tenant-alpha", "req-alpha"] as const;
    await client.getPlatformTenant(...args);
    await client.getOrganization("tenant-alpha", "organization-alpha", "req-alpha");
    await client.listOrganizations(
      "tenant-alpha",
      "req-alpha",
      1,
      "organization-page-token-1",
    );
    await client.getProject("tenant-alpha", "project-alpha", "req-alpha");
    await client.listProjects(
      "tenant-alpha",
      "organization-alpha",
      "req-alpha",
      1,
      "project-page-token-1",
    );
    await client.getMembership("tenant-alpha", "membership-alpha", "req-alpha");
    await client.getRole("tenant-alpha", "role-project-viewer-v1", "req-alpha");
    await client.getRoleBinding("tenant-alpha", "role-binding-alpha", "req-alpha");
    await client.getProjectContext("tenant-alpha", "project-alpha", "req-alpha");
    await client.getManagedHostRoleBinding("tenant-alpha", "role-binding-alpha", "req-alpha");
    await client.createOrganization("tenant-alpha", "req-alpha", {
      expectedTenantRevision: 1,
      organizationId: "organization-alpha",
      name: "organization-alpha",
      displayName: "Organization Alpha",
      auditFactUid: "audit-organization",
      reasonCode: "operator-request",
    });
    await client.createProject(
      "tenant-alpha",
      "req-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
      request,
    );
    expect(seen).toHaveLength(12);
    expect(JSON.parse(seen[11]!.body!)).toEqual(request);
  });

  it("keeps problem status and abort semantics stable", async () => {
    const problem = readFixture(commonFixtureRoot, "golden/problem.json");
    const errorClient = new Client(async () => ({ status: 404, headers: {}, body: problem }));
    await expect(
      errorClient.getProject("tenant-alpha", "project-alpha", "req-alpha"),
    ).rejects.toMatchObject({
      operation: "managedAgentGetProject",
      status: 404,
    });
    const mismatchClient = new Client(async () => ({ status: 500, headers: {}, body: problem }));
    await expect(
      mismatchClient.getProject("tenant-alpha", "project-alpha", "req-alpha"),
    ).rejects.toMatchObject({
      cause: expect.objectContaining({ code: "PROBLEM_STATUS_MISMATCH" }),
    });

    const controller = new AbortController();
    const slowClient = new Client(
      async () =>
        new Promise<FixtureResponse>((resolve) => {
          setTimeout(() => resolve(fixtureResponse(platformFixtureRoot, "project", 200)), 20);
        }),
    );
    const pending = slowClient.getProject(
      "tenant-alpha",
      "project-alpha",
      "req-alpha",
      controller.signal,
    );
    controller.abort();
    await expect(pending).rejects.toBeDefined();

    let transportCalls = 0;
    const guardedClient = new Client(async () => {
      transportCalls++;
      return fixtureResponse(platformFixtureRoot, "project", 200);
    });
    await expect(
      guardedClient.getProject("tenant-alpha", "wrong/id", "req-alpha"),
    ).rejects.toMatchObject({ code: "INVALID_IDENTIFIER" });
    expect(transportCalls).toBe(0);

    const authorityBody = readFixture(platformFixtureRoot, "golden/project.json").replace(
      `"uid": "project-alpha"`,
      `"uid": "project-other"`,
    );
    const authorityClient = new Client(async () => ({
      status: 200,
      headers: { "X-Resource-Version": "3" },
      body: authorityBody,
    }));
    await expect(
      authorityClient.getProject("tenant-alpha", "project-alpha", "req-alpha"),
    ).rejects.toMatchObject({ code: "PATH_BODY_AUTHORITY_MISMATCH", path: "/metadata/uid" });
  });
});

function readFixture(root: string, name: string): string {
  return readFileSync(resolve(root, name), "utf8");
}

function readJSON(root: string, name: string): unknown {
  return JSON.parse(readFixture(root, name)) as unknown;
}

function fixtureResponse(root: string, name: string, status: number): FixtureResponse {
  const body = readFixture(root, `golden/${name}.json`);
  const value = JSON.parse(body) as { metadata?: { resourceVersion?: string } };
  return { status, headers: { "X-Resource-Version": value.metadata?.resourceVersion ?? "" }, body };
}
