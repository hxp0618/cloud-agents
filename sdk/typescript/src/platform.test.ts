import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  Client,
  createHTTPClient,
  decodeDeploymentTargetPage,
  decodeEnvironmentLeasePage,
  decodeIdempotency,
  decodeMembership,
  decodeMembershipPage,
  decodeOrganization,
  decodeOrganizationPage,
  decodePlatformTenant,
  decodeProject,
  decodeProjectPage,
  decodeProjectCreateRequest,
  decodeManagedAgentSession,
  decodeManagedAgentSessionPage,
  decodeManagedAgentTurnPage,
  decodeManagedAgentExecution,
  decodeManagedAgentExecutionPage,
  decodeRole,
  decodeRolePage,
  decodeRoleBinding,
  decodeRoleBindingPage,
  decodeWatchCursor,
  encodeResponse,
  parseProblem,
  parseDeploymentTargetPage,
  parseEnvironmentLeasePage,
  parseProject,
  parseProjectCreateRequest,
  parseManagedAgentSession,
  parseManagedAgentSessionPage,
  parseManagedAgentTurnPage,
  parseManagedAgentExecution,
  parseManagedAgentExecutionPage,
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
          request.method === "GET" && request.path.includes("/sessions?") ? sessionPage : session,
      };
    });
    await client.createManagedAgentSession(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
      { sessionId: "session-alpha", providerKind: "codex", environmentLeaseId: "lease-alpha" },
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
    expect(seen[0]?.body).toBe(
      '{"sessionId":"session-alpha","providerKind":"codex","environmentLeaseId":"lease-alpha"}',
    );
    expect(seen[2]?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/sessions?pageSize=1&pageToken=session-page-token-1",
    );
    expect(seen[3]?.body).toBeUndefined();
  });
  it("lists managed-host environment leases with project-bound pagination", async () => {
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "EnvironmentLeasePage",
      environmentLeases: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "CloudEnvironmentLease",
          metadata: {
            uid: "lease-alpha",
            name: "lease-alpha",
            tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-alpha" },
            resourceVersion: "1",
            createdAt: "2026-08-31T08:00:00Z",
            updatedAt: "2026-08-31T08:00:00Z",
          },
          spec: {
            projectRef: { namespace: "cloud-agents", kind: "project", id: "project-alpha" },
            generation: 1,
            desiredPhase: "active",
            observedPhase: "provisioning",
            cleanupPhase: "none",
            environmentId: "lease-alpha",
            releaseDigest: `sha256:${"a".repeat(64)}`,
            targetId: "docker-alpha",
            targetGeneration: 1,
            expiresAt: "2026-08-31T09:00:00Z",
          },
        },
      ],
      nextPageToken: "lease-page-token-2",
    });
    expect(decodeEnvironmentLeasePage(JSON.parse(page)).environmentLeases).toHaveLength(1);
    expect(parseEnvironmentLeasePage(page).value.nextPageToken).toBe("lease-page-token-2");
    let seen: FixtureRequest | undefined;
    const client = new Client(async (request) => {
      seen = request;
      return { status: 200, headers: {}, body: page };
    });
    const result = await client.listManagedHostEnvironmentLeases(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "lease-page-token-1",
    );
    expect(result.value.environmentLeases[0]?.metadata.uid).toBe("lease-alpha");
    expect(seen?.path).toBe(
      "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1&pageToken=lease-page-token-1",
    );
  });
  it("lists deployment targets with project-bound pagination", async () => {
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "DeploymentTargetPage",
      deploymentTargets: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "DeploymentTarget",
          metadata: {
            uid: "docker-alpha",
            name: "docker-alpha",
            tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-alpha" },
            resourceVersion: "1",
            createdAt: "2026-09-02T08:00:00Z",
            updatedAt: "2026-09-02T08:00:00Z",
          },
          spec: {
            projectRef: { namespace: "cloud-agents", kind: "project", id: "project-alpha" },
            generation: 1,
            targetKind: "docker",
            endpoint: "https://docker.example.test:2376",
            credentialRef: "docker-alpha",
            observedPhase: "unprobed",
            apiVersion: "",
            engineVersion: "",
            os: "",
            architecture: "",
            stableErrorCode: "",
          },
        },
      ],
      nextPageToken: "target-page-token-2",
    });
    expect(decodeDeploymentTargetPage(JSON.parse(page)).deploymentTargets).toHaveLength(1);
    expect(parseDeploymentTargetPage(page).value.nextPageToken).toBe("target-page-token-2");
    let seen: FixtureRequest | undefined;
    const client = new Client(async (request) => {
      seen = request;
      return { status: 200, headers: {}, body: page };
    });
    const result = await client.listDeploymentTargets(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "target-page-token-1",
    );
    expect(result.value.deploymentTargets[0]?.metadata.uid).toBe("docker-alpha");
    expect(seen?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=1&pageToken=target-page-token-1",
    );
  });
  it("replays the managed-agent Turn contract and client lifecycle", async () => {
    const turn = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "Turn",
      metadata: {
        uid: "turn-alpha",
        projectId: "project-alpha",
        sessionId: "session-alpha",
        resourceVersion: "2",
        createdAt: "2026-08-29T08:00:00Z",
        updatedAt: "2026-08-29T08:01:00Z",
      },
      spec: { inputDigest: `sha256:${"a".repeat(64)}`, state: "queued" },
    });
    const turnPage = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "TurnPage",
      turns: [JSON.parse(turn)],
      nextPageToken: "turn-page-token-1",
    });
    expect(decodeManagedAgentTurnPage(JSON.parse(turnPage)).turns).toHaveLength(1);
    expect(parseManagedAgentTurnPage(turnPage).value.nextPageToken).toBe("turn-page-token-1");
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status: request.method === "POST" ? 201 : 200,
        headers: { "X-Resource-Version": "2" },
        body: request.method === "GET" && request.path.includes("/turns?") ? turnPage : turn,
      };
    });
    await client.createManagedAgentTurn(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R4",
      { turnId: "turn-alpha", inputText: "hello" },
    );
    await client.getManagedAgentTurn(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "request-alpha",
    );
    await client.listManagedAgentTurns(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
      1,
      "turn-page-token-1",
    );
    expect(seen).toHaveLength(3);
    expect(seen[2]?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns?pageSize=1&pageToken=turn-page-token-1",
    );
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
    const executionPage = JSON.stringify({
      apiVersion: "managed-agent.cloud-agents.dev/v1alpha1",
      kind: "ExecutionPage",
      executions: [{ ...JSON.parse(execution), messages: undefined }],
      nextPageToken: "execution-page-token-2",
    });
    expect(decodeManagedAgentExecutionPage(JSON.parse(executionPage)).executions).toHaveLength(1);
    expect(parseManagedAgentExecutionPage(executionPage).value.nextPageToken).toBe(
      "execution-page-token-2",
    );
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status:
          request.path.endsWith(":resolveApproval") || request.path.endsWith(":resolveUserInput")
            ? 204
            : 200,
        headers: { "X-Resource-Version": "3" },
        body:
          request.method === "GET" && request.path.includes("/executions?")
            ? executionPage
            : execution,
      };
    });
    await client.executeManagedAgent(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-alpha",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R2",
      {
        turnId: "turn-alpha",
        executionId: "execution-alpha",
        model: "codex",
        runtimeMode: "approval-required",
        interactionMode: "plan",
        inputText: "hello",
      },
    );
    await client.getManagedAgentExecution(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-alpha",
    );
    await client.listManagedAgentExecutions(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "request-list",
      1,
      "execution-page-token-1",
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
    await client.resolveManagedAgentApproval(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-approval",
      { generation: 1, requestId: "codex:generation-1:approval:1", decision: "accept" },
    );
    await client.resolveManagedAgentUserInput(
      "tenant-alpha",
      "project-alpha",
      "session-alpha",
      "turn-alpha",
      "execution-alpha",
      "request-user-input",
      {
        generation: 1,
        requestId: "claude:generation-1:user-input:2",
        answers: Object.fromEntries([["__proto__", ["one", "two"]]]),
      },
    );
    await expect(
      client.resolveManagedAgentUserInput(
        "tenant-alpha",
        "project-alpha",
        "session-alpha",
        "turn-alpha",
        "execution-alpha",
        "request-user-input-invalid",
        {
          generation: 1,
          requestId: "claude:generation-1:user-input:2",
          answers: { "question-1": ["bad\u0000answer"] },
        },
      ),
    ).rejects.toThrow();
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions",
      "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha",
      "GET /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/executions?pageSize=1&pageToken=execution-page-token-1",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:cancel",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:interrupt",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveApproval",
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/sessions/session-alpha/turns/turn-alpha/executions/execution-alpha:resolveUserInput",
    ]);
    expect(seen[0]?.body).toBe(
      '{"turnId":"turn-alpha","executionId":"execution-alpha","inputText":"hello","model":"codex","runtimeMode":"approval-required","interactionMode":"plan"}',
    );
    expect(seen[3]?.body).toBe('{"generation":1}');
    expect(seen[4]?.body).toBe('{"generation":1}');
    expect(seen[5]?.body).toBe(
      '{"generation":1,"requestId":"codex:generation-1:approval:1","decision":"accept"}',
    );
    expect(seen[6]?.body).toBe(
      '{"generation":1,"requestId":"claude:generation-1:user-input:2","answers":{"__proto__":["one","two"]}}',
    );
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
    expect(
      decodeProjectPage(readJSON(platformFixtureRoot, "golden/project-page.json")).projects,
    ).toHaveLength(1);
    expect(
      decodeProjectCreateRequest(
        readJSON(platformFixtureRoot, "golden/project-create-request.json"),
      ).name,
    ).toBe("project-alpha");
    expect(decodeMembership(readJSON(platformFixtureRoot, "golden/membership.json")).kind).toBe(
      "Membership",
    );
    expect(
      decodeMembershipPage({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "MembershipPage",
        memberships: [readJSON(platformFixtureRoot, "golden/membership.json")],
        nextPageToken: "membership-page-token-1",
      }).memberships,
    ).toHaveLength(1);
    expect(decodeRole(readJSON(platformFixtureRoot, "golden/role.json")).spec.name).toBe(
      "project.viewer",
    );
    expect(
      decodeRolePage({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "RolePage",
        roles: [readJSON(platformFixtureRoot, "golden/role.json")],
        nextPageToken: "role-page-token-1",
      }).roles,
    ).toHaveLength(1);
    expect(decodeRoleBinding(readJSON(platformFixtureRoot, "golden/role-binding.json")).kind).toBe(
      "RoleBinding",
    );
    expect(
      decodeRoleBindingPage({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "RoleBindingPage",
        roleBindings: [readJSON(platformFixtureRoot, "golden/role-binding.json")],
        nextPageToken: "role-binding-page-token-1",
      }).roleBindings,
    ).toHaveLength(1);
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

describe("generated platform client", () => {
  it("calls the Control Plane with bearer auth without following redirects", async () => {
    const project = readFixture(platformFixtureRoot, "golden/project.json");
    let redirectTargetCalls = 0;
    const fixture = createServer((request, response) => {
      if (request.url === "/redirect-target") {
        redirectTargetCalls += 1;
        response.end();
        return;
      }
      if (
        request.headers.authorization !== "Bearer token-alpha" ||
        request.headers["x-request-id"] !== "request-alpha"
      ) {
        response.statusCode = 401;
        response.end();
        return;
      }
      if (request.url?.endsWith("/project-redirect")) {
        response.statusCode = 307;
        response.setHeader("Location", "/redirect-target");
        response.end();
        return;
      }
      if (request.url?.endsWith("/project-oversize")) {
        response.statusCode = 200;
        response.end(Buffer.alloc(2 * 1024 * 1024 + 1, 0x20));
        return;
      }
      if (request.url?.includes("/execution-artifact-oversize/messages/0/artifact")) {
        response.statusCode = 200;
        response.setHeader("Content-Type", "application/octet-stream");
        response.end(Buffer.alloc(16 * 1024 * 1024 + 1, 0x80));
        return;
      }
      if (request.url?.includes("/execution-artifact/messages/0/artifact")) {
        response.statusCode = 200;
        response.setHeader("Content-Type", "application/octet-stream");
        response.setHeader("ETag", '"sha256:artifact"');
        response.end(Buffer.alloc(16 * 1024 * 1024, 0x80));
        return;
      }
      response.statusCode = 200;
      response.setHeader("Content-Type", "application/json");
      response.setHeader("X-Resource-Version", "3");
      response.end(project);
    });
    await new Promise<void>((resolveListen, reject) => {
      fixture.once("error", reject);
      fixture.listen(0, "127.0.0.1", resolveListen);
    });
    try {
      const address = fixture.address();
      if (address === null || typeof address === "string") throw new Error("fixture did not bind");
      const baseURL = `http://127.0.0.1:${address.port}/control-plane`;
      const client = createHTTPClient(baseURL, "token-alpha");
      const result = await client.getProject("tenant-alpha", "project-alpha", "request-alpha");
      expect(result.value.metadata.uid).toBe("project-alpha");
      await expect(
        client.getProject("tenant-alpha", "project-redirect", "request-alpha"),
      ).rejects.toMatchObject({ status: 307 });
      expect(redirectTargetCalls).toBe(0);
      await expect(
        client.getProject("tenant-alpha", "project-oversize", "request-alpha"),
      ).rejects.toThrow("Cloud Agents HTTP response exceeds the SDK limit");
      const artifact = await client.downloadManagedAgentArtifact(
        "tenant-alpha",
        "project-alpha",
        "session-alpha",
        "turn-alpha",
        "execution-artifact",
        "request-alpha",
        0,
      );
      expect(artifact.data).toHaveLength(16 * 1024 * 1024);
      expect([artifact.data[0], artifact.data.at(-1), artifact.contentType, artifact.etag]).toEqual(
        [0x80, 0x80, "application/octet-stream", '"sha256:artifact"'],
      );
      await expect(
        client.downloadManagedAgentArtifact(
          "tenant-alpha",
          "project-alpha",
          "session-alpha",
          "turn-alpha",
          "execution-artifact-oversize",
          "request-alpha",
          0,
        ),
      ).rejects.toThrow("Cloud Agents HTTP response exceeds the SDK limit");
      expect(() => createHTTPClient(`${baseURL}/`, "token-alpha")).toThrow(TypeError);
      expect(() => createHTTPClient(baseURL, "token alpha")).toThrow(TypeError);
      expect(() => createHTTPClient("ftp://127.0.0.1", "token-alpha")).toThrow(TypeError);
      expect(() => createHTTPClient("http://example.com", "token-alpha")).toThrow(TypeError);
      expect(() => createHTTPClient("http://localhost", "token-alpha")).toThrow(TypeError);
      expect(() => createHTTPClient("https://example.com", "token-alpha")).not.toThrow();
      expect(() => createHTTPClient(undefined as unknown as string, "token-alpha")).toThrow(
        TypeError,
      );
    } finally {
      await new Promise<void>((resolveClose) => fixture.close(() => resolveClose()));
    }
  });

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
      "GET /v1/tenants/tenant-alpha/projects?organizationId=organization-alpha&pageSize=1&pageToken=project-page-token-1":
        {
          status: 200,
          headers: {},
          body: readFixture(platformFixtureRoot, "golden/project-page.json"),
        },
      "GET /v1/tenants/tenant-alpha/memberships/membership-alpha": fixtureResponse(
        platformFixtureRoot,
        "membership",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/memberships?pageSize=1&pageToken=membership-page-token-1": {
        status: 200,
        headers: {},
        body: JSON.stringify({
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "MembershipPage",
          memberships: [readJSON(platformFixtureRoot, "golden/membership.json")],
          nextPageToken: "membership-page-token-2",
        }),
      },
      "GET /v1/tenants/tenant-alpha/roles/role-project-viewer-v1": fixtureResponse(
        platformFixtureRoot,
        "role",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/roles?pageSize=1&pageToken=role-page-token-1": {
        status: 200,
        headers: {},
        body: JSON.stringify({
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "RolePage",
          roles: [readJSON(platformFixtureRoot, "golden/role.json")],
          nextPageToken: "role-page-token-2",
        }),
      },
      "GET /v1/tenants/tenant-alpha/role-bindings/role-binding-alpha": fixtureResponse(
        platformFixtureRoot,
        "role-binding",
        200,
      ),
      "GET /v1/tenants/tenant-alpha/role-bindings?pageSize=1&pageToken=role-binding-page-token-1": {
        status: 200,
        headers: {},
        body: JSON.stringify({
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "RoleBindingPage",
          roleBindings: [readJSON(platformFixtureRoot, "golden/role-binding.json")],
          nextPageToken: "role-binding-page-token-2",
        }),
      },
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
    await client.listOrganizations("tenant-alpha", "req-alpha", 1, "organization-page-token-1");
    await client.getProject("tenant-alpha", "project-alpha", "req-alpha");
    await client.listProjects(
      "tenant-alpha",
      "organization-alpha",
      "req-alpha",
      1,
      "project-page-token-1",
    );
    await client.getMembership("tenant-alpha", "membership-alpha", "req-alpha");
    await client.listMemberships("tenant-alpha", "req-alpha", 1, "membership-page-token-1");
    await client.getRole("tenant-alpha", "role-project-viewer-v1", "req-alpha");
    await client.listRoles("tenant-alpha", "req-alpha", 1, "role-page-token-1");
    await client.getRoleBinding("tenant-alpha", "role-binding-alpha", "req-alpha");
    await client.listRoleBindings("tenant-alpha", "req-alpha", 1, "role-binding-page-token-1");
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
    expect(seen).toHaveLength(15);
    expect(JSON.parse(seen[14]!.body!)).toEqual(request);
  });

  it("resumes a membership through the transition contract", async () => {
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status: 200,
        headers: { "X-Resource-Version": "9" },
        body: JSON.stringify({
          resourceUid: "membership-alpha",
          resourceVersion: "9",
          state: "active",
        }),
      };
    });
    const result = await client.resumeMembership("tenant-alpha", "membership-alpha", "req-alpha", {
      expectedTenantRevision: 8,
      expectedResourceVersion: 8,
      auditFactUid: "audit-resume",
      reasonCode: "operator-request",
    });
    expect(result.value.state).toBe("active");
    expect(seen).toEqual([
      {
        method: "POST",
        path: "/v1/tenants/tenant-alpha/memberships/membership-alpha:resume",
        headers: { "X-Request-ID": "req-alpha" },
        body: JSON.stringify({
          expectedTenantRevision: 8,
          auditFactUid: "audit-resume",
          reasonCode: "operator-request",
          expectedResourceVersion: 8,
        }),
      },
    ]);
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
