import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import {
  Client,
  createHTTPClient,
  decodeAdminAuditEventPage,
  decodeDeploymentTargetCleanupPreview,
  decodeDeploymentTargetPage,
  decodeDeploymentTargetSchedulingPreview,
  decodeEnvironmentLeasePage,
  decodeEnvironmentLeaseUpgradePreview,
  decodeEnvironmentProfile,
  decodeEnvironmentProfilePage,
  decodeEnvironmentProfileSummary,
  decodeEnvironmentProfileSummaryPage,
  decodeIdempotency,
  decodeMembership,
  decodeMembershipPage,
  decodeMaintenanceOperationPage,
  decodeOrganization,
  decodeOrganizationPage,
  decodePlatformTenant,
  decodeProject,
  decodeProjectPage,
  decodeProjectCreateRequest,
  decodeProjectLeaseQuota,
  decodeProjectLeaseQuotaSummary,
  decodeStoragePolicy,
  decodeStoragePolicyPage,
  decodeUserEnvironment,
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
  decodeWorkerPage,
  decodeWorkerRelease,
  decodeWorkerReleasePage,
  encodeResponse,
  parseProblem,
  parseDeploymentTargetCleanupPreview,
  parseDeploymentTargetPage,
  parseDeploymentTargetSchedulingPreview,
  parseEnvironmentLeasePage,
  parseEnvironmentLeaseUpgradePreview,
  parseEnvironmentProfile,
  parseEnvironmentProfilePage,
  parseEnvironmentProfileSummaryPage,
  parseAdminAuditEventPage,
  parseMaintenanceOperationPage,
  parseProject,
  parseProjectCreateRequest,
  parseProjectLeaseQuota,
  parseProjectLeaseQuotaSummary,
  parseStoragePolicy,
  parseStoragePolicyPage,
  parseUserEnvironment,
  parseManagedAgentSession,
  parseManagedAgentSessionPage,
  parseManagedAgentTurnPage,
  parseManagedAgentExecution,
  parseManagedAgentExecutionPage,
  parseWatchCursor,
  parseWorkerPage,
  parseWorkerRelease,
  parseWorkerReleasePage,
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
      {
        sessionId: "session-alpha",
        providerKind: "codex",
        environmentLeaseId: "lease-alpha",
      },
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
            tenantRef: {
              namespace: "cloud-agents",
              kind: "tenant",
              id: "tenant-alpha",
            },
            resourceVersion: "1",
            createdAt: "2026-08-31T08:00:00Z",
            updatedAt: "2026-08-31T08:00:00Z",
          },
          spec: {
            projectRef: {
              namespace: "cloud-agents",
              kind: "project",
              id: "project-alpha",
            },
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
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
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
    await client.listAdminEnvironmentLeases(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "lease-page-token-1",
    );
    expect(seen[0]?.path).toBe(
      "/v1/managed-host/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1&pageToken=lease-page-token-1",
    );
    expect(seen[1]?.path).toBe(
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases?pageSize=1&pageToken=lease-page-token-1",
    );
  });
  it("lists lease-backed Workers only through the Admin API", async () => {
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "WorkerPage",
      workers: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "Worker",
          metadata: {
            uid: "lease-alpha",
            name: "worker-alpha",
            tenantRef: {
              namespace: "cloud-agents",
              kind: "tenant",
              id: "tenant-alpha",
            },
            resourceVersion: "4",
            createdAt: "2026-09-04T08:00:00Z",
            updatedAt: "2026-09-04T08:01:00Z",
          },
          spec: {
            projectRef: {
              namespace: "cloud-agents",
              kind: "project",
              id: "project-alpha",
            },
            leaseId: "lease-alpha",
            targetId: "docker-alpha",
            targetKind: "docker",
            targetGeneration: 2,
            generation: 3,
            releaseDigest: `sha256:${"a".repeat(64)}`,
            state: "ready",
            cleanupPhase: "none",
            cpuLimitMillis: 1000,
            memoryLimitBytes: 536870912,
            workerSpiffeId: "spiffe://cloud-agents.test/worker/lease-alpha",
            workerServerName: "worker-alpha.test",
            lastHealthAt: "2026-09-04T08:01:00Z",
            readyAt: "2026-09-04T08:01:00Z",
            stableErrorCode: "",
          },
        },
      ],
      nextPageToken: "worker-page-token-2",
    });
    expect(decodeWorkerPage(JSON.parse(page)).workers[0]?.spec.state).toBe("ready");
    expect(parseWorkerPage(page).value.nextPageToken).toBe("worker-page-token-2");
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return { status: 200, headers: {}, body: page };
    });
    await client.listAdminWorkers(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "worker-page-token-1",
    );
    expect(seen[0]?.path).toBe(
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/workers?pageSize=1&pageToken=worker-page-token-1",
    );
    const withEndpoint = JSON.parse(page) as {
      workers: Array<{ spec: Record<string, unknown> }>;
    };
    withEndpoint.workers[0]!.spec.workerEndpoint = "https://worker.test";
    expect(() => decodeWorkerPage(withEndpoint)).toThrow();
  });
  it("keeps project Lease quota authority split between Admin and User APIs", async () => {
    const quota = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "ProjectLeaseQuota",
      metadata: {
        uid: "quota-project-alpha",
        name: "project-lease-quota",
        tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-alpha" },
        resourceVersion: "1",
        createdAt: "2026-09-05T01:00:00Z",
        updatedAt: "2026-09-05T01:00:00Z",
      },
      spec: {
        projectRef: { namespace: "cloud-agents", kind: "project", id: "project-alpha" },
        maxConcurrentLeases: 2,
        maxCpuMillis: 4000,
        maxMemoryBytes: 8589934592,
        maxLeaseTtlSeconds: 3600,
      },
      status: { activeLeases: 1, usedCpuMillis: 2000, usedMemoryBytes: 4294967296 },
    });
    const summary = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "ProjectLeaseQuotaSummary",
      projectRef: { namespace: "cloud-agents", kind: "project", id: "project-alpha" },
      maxConcurrentLeases: 2,
      activeLeases: 1,
      maxCpuMillis: 4000,
      usedCpuMillis: 2000,
      maxMemoryBytes: 8589934592,
      usedMemoryBytes: 4294967296,
      maxLeaseTtlSeconds: 3600,
    });
    expect(decodeProjectLeaseQuota(JSON.parse(quota)).status.activeLeases).toBe(1);
    expect(parseProjectLeaseQuota(quota).value.metadata.resourceVersion).toBe("1");
    expect(decodeProjectLeaseQuotaSummary(JSON.parse(summary)).maxConcurrentLeases).toBe(2);
    expect(parseProjectLeaseQuotaSummary(summary).value.maxLeaseTtlSeconds).toBe(3600);
    expect(() =>
      decodeProjectLeaseQuotaSummary({ ...JSON.parse(summary), credentialRef: "secret" }),
    ).toThrow();

    const audit = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "AdminAuditEventPage",
      events: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "AdminAuditEvent",
          eventId: "event-quota-set",
          actor: `sha256:${"a".repeat(64)}`,
          action: "quota.set",
          resourceKind: "ProjectLeaseQuota",
          resourceId: "quota-project-alpha",
          resourceGeneration: 1,
          result: "succeeded",
          occurredAt: "2026-09-05T01:00:00Z",
          requestId: "request-quota-set",
          operationId: "operation-quota-set",
        },
      ],
    });
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status: 200,
        headers:
          request.path.includes("/admin/") && !request.path.includes("/audit-events")
            ? { "X-Resource-Version": "1" }
            : {},
        body: request.path.includes("/audit-events")
          ? audit
          : request.path.includes("/admin/")
            ? quota
            : summary,
      };
    });
    await client.getAdminProjectLeaseQuota("tenant-alpha", "project-alpha", "request-quota-get");
    await client.setAdminProjectLeaseQuota(
      "tenant-alpha",
      "project-alpha",
      "request-quota-set",
      "quota-set-key-0001",
      {
        expectedResourceVersion: "0",
        maxConcurrentLeases: 2,
        maxCpuMillis: 4000,
        maxMemoryBytes: 8589934592,
        maxLeaseTtlSeconds: 3600,
      },
    );
    const auditResult = await client.listAdminProjectLeaseQuotaAuditEvents(
      "tenant-alpha",
      "project-alpha",
      "request-quota-audit",
      50,
    );
    expect(auditResult.value.events[0]?.action).toBe("quota.set");
    await client.getProjectLeaseQuota("tenant-alpha", "project-alpha", "request-user-quota");
    expect(seen.map(({ path }) => path)).toEqual([
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota",
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota",
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/lease-quota/audit-events?pageSize=50",
      "/v1/tenants/tenant-alpha/projects/project-alpha/lease-quota",
    ]);
    expect(seen[1]?.body).toBe(
      '{"expectedResourceVersion":"0","maxConcurrentLeases":2,"maxCpuMillis":4000,"maxMemoryBytes":8589934592,"maxLeaseTtlSeconds":3600}',
    );
  });
  it("registers and lists approved Worker releases", async () => {
    const digest = `sha256:${"a".repeat(64)}` as const;
    const release = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "WorkerRelease",
      metadata: {
        uid: "worker-v1",
        name: "worker-v1",
        tenantRef: {
          namespace: "cloud-agents",
          kind: "tenant",
          id: "tenant-alpha",
        },
        resourceVersion: "1",
        createdAt: "2026-09-04T08:00:00Z",
        updatedAt: "2026-09-04T08:00:00Z",
      },
      spec: {
        projectRef: {
          namespace: "cloud-agents",
          kind: "project",
          id: "project-alpha",
        },
        imageRepository: "registry.example.test/cloud-agents/worker",
        releaseDigest: digest,
        platformVersion: "platform-v1",
        runtimeVersion: "runtime-v1",
        codexVersion: "codex-v1",
        claudeCodeVersion: "claude-v1",
        architectures: ["linux/amd64"],
        status: "approved",
        verificationState: "attested",
        verificationEvidenceDigest: digest,
        approvedAt: "2026-09-04T08:00:00Z",
      },
    });
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "WorkerReleasePage",
      workerReleases: [JSON.parse(release)],
      nextPageToken: "release-page-token-2",
    });
    expect(decodeWorkerRelease(JSON.parse(release)).spec.status).toBe("approved");
    expect(parseWorkerRelease(release).value.spec.verificationState).toBe("attested");
    expect(decodeWorkerReleasePage(JSON.parse(page)).workerReleases).toHaveLength(1);
    expect(parseWorkerReleasePage(page).value.nextPageToken).toBe("release-page-token-2");

    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return request.method === "POST"
        ? { status: 201, headers: { "X-Resource-Version": "1" }, body: release }
        : { status: 200, headers: {}, body: page };
    });
    const body = {
      releaseId: "worker-v1",
      releaseName: "worker-v1",
      imageRepository: "registry.example.test/cloud-agents/worker",
      releaseDigest: digest,
      platformVersion: "platform-v1",
      runtimeVersion: "runtime-v1",
      codexVersion: "codex-v1",
      claudeCodeVersion: "claude-v1",
      architectures: ["linux/amd64"] as const,
      verificationEvidenceDigest: digest,
    };
    await client.registerAdminWorkerRelease(
      "tenant-alpha",
      "project-alpha",
      "request-release-create",
      "release-create-key",
      body,
    );
    await client.listAdminWorkerReleases(
      "tenant-alpha",
      "project-alpha",
      "request-release-list",
      1,
      "release-page-token-1",
    );
    expect(seen[0]?.body).toBe(JSON.stringify(body));
    expect(seen[1]?.path).toBe(
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/worker-releases?pageSize=1&pageToken=release-page-token-1",
    );
    expect(() =>
      decodeWorkerRelease({ ...JSON.parse(release), credentialRef: "secret" }),
    ).toThrow();
  });

  it("drives the Admin environment profile lifecycle", async () => {
    const profile = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "EnvironmentProfile",
      metadata: {
        uid: "ep-0123456789abcdef0123456789abcdef",
        name: "development",
        tenantRef: {
          namespace: "cloud-agents",
          kind: "tenant",
          id: "tenant-alpha",
        },
        resourceVersion: "1",
        createdAt: "2026-09-03T08:00:00Z",
        updatedAt: "2026-09-03T08:00:00Z",
      },
      spec: {
        projectRef: {
          namespace: "cloud-agents",
          kind: "project",
          id: "project-alpha",
        },
        profileId: "development",
        version: 1,
        description: "Daily coding workspace",
        status: "draft",
        providerKinds: ["codex", "claudeAgent"],
        cpuLimitMillis: 2000,
        memoryLimitBytes: 4294967296,
        storagePolicyRef: "storage-standard",
        networkPolicyRef: "network-egress",
        releaseDigest: `sha256:${"a".repeat(64)}`,
        targetRefs: ["docker-primary"],
        providerCredentialRef: "provider-default",
      },
    });
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "EnvironmentProfilePage",
      environmentProfiles: [JSON.parse(profile)],
    });
    const profileValue = JSON.parse(profile);
    const publishedProfile = JSON.stringify({
      ...profileValue,
      metadata: { ...profileValue.metadata, resourceVersion: "2" },
      spec: {
        ...profileValue.spec,
        status: "published",
        publishedAt: "2026-09-03T08:01:00Z",
      },
    });
    const disabledProfile = JSON.stringify({
      ...profileValue,
      metadata: { ...profileValue.metadata, resourceVersion: "3" },
      spec: {
        ...profileValue.spec,
        status: "disabled",
        publishedAt: "2026-09-03T08:01:00Z",
        disabledAt: "2026-09-03T08:02:00Z",
      },
    });
    const audit = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "AdminAuditEventPage",
      events: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "AdminAuditEvent",
          eventId: "profile-create-succeeded",
          actor: `sha256:${"b".repeat(64)}`,
          action: "profile.create",
          resourceKind: "EnvironmentProfile",
          resourceId: "ep-0123456789abcdef0123456789abcdef",
          resourceGeneration: 1,
          result: "succeeded",
          occurredAt: "2026-09-03T08:00:00Z",
          requestId: "request-profile-create",
          operationId: "operation-profile-create",
        },
      ],
    });
    expect(decodeEnvironmentProfile(JSON.parse(profile)).spec.status).toBe("draft");
    expect(parseEnvironmentProfile(profile).value.spec.profileId).toBe("development");
    expect(decodeEnvironmentProfilePage(JSON.parse(page)).environmentProfiles).toHaveLength(1);
    expect(parseEnvironmentProfilePage(page).value.environmentProfiles).toHaveLength(1);
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      if (request.path.endsWith("/audit-events?pageSize=1"))
        return { status: 200, headers: {}, body: audit };
      if (request.method === "GET" && request.path.includes("environment-profiles?"))
        return { status: 200, headers: {}, body: page };
      if (request.path.endsWith(":publish"))
        return seen.some(({ path }) => path.endsWith(":disable"))
          ? {
              status: 200,
              headers: { "X-Resource-Version": "3" },
              body: disabledProfile,
            }
          : {
              status: 200,
              headers: { "X-Resource-Version": "2" },
              body: publishedProfile,
            };
      if (request.path.endsWith(":disable"))
        return {
          status: 200,
          headers: { "X-Resource-Version": "3" },
          body: disabledProfile,
        };
      return {
        status: request.method === "POST" ? 201 : 200,
        headers: { "X-Resource-Version": "1" },
        body: profile,
      };
    });
    const body = {
      profileId: "development",
      profileName: "development",
      version: 1,
      description: "Daily coding workspace",
      providerKinds: ["codex", "claudeAgent"] as const,
      cpuLimitMillis: 2000,
      memoryLimitBytes: 4294967296,
      storagePolicyRef: "storage-standard",
      networkPolicyRef: "network-egress",
      releaseDigest: `sha256:${"a".repeat(64)}` as const,
      targetRefs: ["docker-primary"],
      providerCredentialRef: "provider-default",
    };
    await client.createAdminEnvironmentProfile(
      "tenant-alpha",
      "project-alpha",
      "request-profile-create",
      "profile-create-idempotency",
      body,
    );
    await client.publishAdminEnvironmentProfile(
      "tenant-alpha",
      "project-alpha",
      "development",
      1,
      "request-profile-publish",
      "profile-publish-idempotency",
      { expectedResourceVersion: "1" },
    );
    await client.disableAdminEnvironmentProfile(
      "tenant-alpha",
      "project-alpha",
      "development",
      1,
      "request-profile-disable",
      "profile-disable-idempotency",
      { expectedResourceVersion: "2" },
    );
    await client.publishAdminEnvironmentProfile(
      "tenant-alpha",
      "project-alpha",
      "development",
      1,
      "request-profile-publish-replay",
      "profile-publish-idempotency",
      { expectedResourceVersion: "1" },
    );
    await client.listAdminEnvironmentProfiles(
      "tenant-alpha",
      "project-alpha",
      "request-profile-list",
      1,
    );
    await client.getAdminEnvironmentProfile(
      "tenant-alpha",
      "project-alpha",
      "development",
      1,
      "request-profile-get",
    );
    await client.listAdminEnvironmentProfileAuditEvents(
      "tenant-alpha",
      "project-alpha",
      "development",
      1,
      "request-profile-audit",
      1,
    );
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/development/versions/1:publish",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/development/versions/1:disable",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/development/versions/1:publish",
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=1",
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/development/versions/1",
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-profiles/development/versions/1/audit-events?pageSize=1",
    ]);
  });
  it("lists only strict User API environment profile summaries", async () => {
    const summary = {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "EnvironmentProfileSummary",
      projectRef: {
        namespace: "cloud-agents",
        kind: "project",
        id: "project-alpha",
      },
      profileId: "development",
      name: "development",
      version: 1,
      description: "Daily coding workspace",
      status: "published",
      availability: "available",
      providerKinds: ["codex", "claudeAgent"],
      cpuLimitMillis: 2000,
      memoryLimitBytes: 4294967296,
      storageSummary: "20 GiB managed workspace",
    };
    const page = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "EnvironmentProfileSummaryPage",
      environmentProfiles: [summary],
    });
    expect(decodeEnvironmentProfileSummary(summary).status).toBe("published");
    expect(decodeEnvironmentProfileSummaryPage(JSON.parse(page)).environmentProfiles).toHaveLength(
      1,
    );
    expect(parseEnvironmentProfileSummaryPage(page).value.environmentProfiles).toHaveLength(1);
    expect(() =>
      decodeEnvironmentProfileSummary({
        ...summary,
        targetRefs: ["docker-primary"],
      }),
    ).toThrow();
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return { status: 200, headers: {}, body: page };
    });
    await client.listEnvironmentProfiles(
      "tenant-alpha",
      "project-alpha",
      "request-profile-summary-list",
      1,
    );
    expect(seen[0]?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/environment-profiles?pageSize=1",
    );
  });
  it("manages only the supported Storage Policy lifecycle", async () => {
    const policy = {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "StoragePolicy",
      metadata: {
        uid: "storage-standard",
        name: "storage-standard",
        tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-alpha" },
        resourceVersion: "1",
        createdAt: "2026-09-05T03:00:00Z",
        updatedAt: "2026-09-05T03:00:00Z",
      },
      spec: {
        projectRef: { namespace: "cloud-agents", kind: "project", id: "project-alpha" },
        userSummary: "20 GiB managed workspace",
        workspaceType: "managed-volume",
        workspaceCapacityBytes: 21474836480,
        retentionSeconds: 0,
        cleanupOnLeaseTermination: true,
        allowWorkspaceReuse: true,
      },
    };
    const page = {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "StoragePolicyPage",
      storagePolicies: [policy],
    };
    expect(decodeStoragePolicy(policy).spec.workspaceCapacityBytes).toBe(21474836480);
    expect(decodeStoragePolicyPage(page).storagePolicies).toHaveLength(1);
    expect(parseStoragePolicy(JSON.stringify(policy)).value.metadata.uid).toBe("storage-standard");
    expect(parseStoragePolicyPage(JSON.stringify(page)).value.storagePolicies).toHaveLength(1);
    expect(() =>
      decodeStoragePolicy({ ...policy, spec: { ...policy.spec, retentionSeconds: 1 } }),
    ).toThrow();

    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      if (request.path.endsWith("/audit-events?pageSize=1")) {
        return {
          status: 200,
          headers: {},
          body: JSON.stringify({
            apiVersion: policy.apiVersion,
            kind: "AdminAuditEventPage",
            events: [],
          }),
        };
      }
      if (request.path.endsWith("/storage-policies?pageSize=1")) {
        return { status: 200, headers: {}, body: JSON.stringify(page) };
      }
      return { status: 200, headers: { "X-Resource-Version": "1" }, body: JSON.stringify(policy) };
    });
    await client.listAdminStoragePolicies(
      "tenant-alpha",
      "project-alpha",
      "request-storage-list",
      1,
    );
    await client.getAdminStoragePolicy(
      "tenant-alpha",
      "project-alpha",
      "storage-standard",
      "request-storage-get",
    );
    await client.setAdminStoragePolicy(
      "tenant-alpha",
      "project-alpha",
      "storage-standard",
      "request-storage-set",
      "storage-set-key-0001",
      {
        expectedResourceVersion: "0",
        policyName: "storage-standard",
        userSummary: "20 GiB managed workspace",
        workspaceType: "managed-volume",
        workspaceCapacityBytes: 21474836480,
        retentionSeconds: 0,
        cleanupOnLeaseTermination: true,
        allowWorkspaceReuse: true,
      },
    );
    await client.listAdminStoragePolicyAuditEvents(
      "tenant-alpha",
      "project-alpha",
      "storage-standard",
      "request-storage-audit",
      1,
    );
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies?pageSize=1",
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard",
      "PUT /v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard",
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/storage-policies/storage-standard/audit-events?pageSize=1",
    ]);
  });
  it("creates and reads an environment using only immutable Profile identity", async () => {
    const value = {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "UserEnvironment",
      projectRef: {
        namespace: "cloud-agents",
        kind: "project",
        id: "project-alpha",
      },
      environmentId: "environment-alpha",
      profileId: "development",
      profileVersion: 1,
      observedPhase: "provisioning",
      expiresAt: "2026-09-04T12:00:00Z",
    };
    const body = JSON.stringify(value);
    expect(decodeUserEnvironment(value).profileId).toBe("development");
    expect(parseUserEnvironment(body).value.environmentId).toBe("environment-alpha");
    expect(() =>
      decodeUserEnvironment({
        ...value,
        providerCredentialRef: "provider-secret",
      }),
    ).toThrow();
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status: request.method === "POST" ? 201 : 200,
        headers: {},
        body,
      };
    });
    await client.createEnvironment(
      "tenant-alpha",
      "project-alpha",
      "request-environment-create",
      "idem-01JZ4X7PGQFHZ2YJR37QRYZ9R9",
      { profileId: "development", profileVersion: 1 },
    );
    await client.getEnvironment(
      "tenant-alpha",
      "project-alpha",
      "environment-alpha",
      "request-environment-get",
    );
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "POST /v1/tenants/tenant-alpha/projects/project-alpha/environments",
      "GET /v1/tenants/tenant-alpha/projects/project-alpha/environments/environment-alpha",
    ]);
    expect(seen[0]?.body).toBe('{"profileId":"development","profileVersion":1}');
    expect(seen[0]?.body).not.toMatch(/target|credential|release|cpu|memory|storage|network/i);
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
            tenantRef: {
              namespace: "cloud-agents",
              kind: "tenant",
              id: "tenant-alpha",
            },
            resourceVersion: "1",
            createdAt: "2026-09-02T08:00:00Z",
            updatedAt: "2026-09-02T08:00:00Z",
          },
          spec: {
            projectRef: {
              namespace: "cloud-agents",
              kind: "project",
              id: "project-alpha",
            },
            generation: 1,
            targetKind: "docker",
            endpoint: "https://docker.example.test:2376",
            credentialRef: "docker-alpha",
            schedulingState: "active",
            observedPhase: "unprobed",
            apiVersion: "",
            engineVersion: "",
            os: "",
            architecture: "",
            stableErrorCode: "",
          },
        },
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "DeploymentTarget",
          metadata: {
            uid: "ssh-alpha",
            name: "ssh-alpha",
            tenantRef: {
              namespace: "cloud-agents",
              kind: "tenant",
              id: "tenant-alpha",
            },
            resourceVersion: "1",
            createdAt: "2026-09-02T08:00:00Z",
            updatedAt: "2026-09-02T08:00:00Z",
          },
          spec: {
            projectRef: {
              namespace: "cloud-agents",
              kind: "project",
              id: "project-alpha",
            },
            generation: 1,
            targetKind: "ssh",
            endpoint: "ssh://ssh.example.test:22",
            credentialRef: "ssh-alpha",
            schedulingState: "active",
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
    expect(decodeDeploymentTargetPage(JSON.parse(page)).deploymentTargets).toHaveLength(2);
    expect(parseDeploymentTargetPage(page).value.nextPageToken).toBe("target-page-token-2");
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
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
    await client.listAdminDeploymentTargets(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "target-page-token-1",
    );
    expect(seen[0]?.path).toBe(
      "/v1/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=1&pageToken=target-page-token-1",
    );
    expect(seen[1]?.path).toBe(
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets?pageSize=1&pageToken=target-page-token-1",
    );
  });
  it("lists durable Admin target operations and audit events", async () => {
    const operation = {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "MaintenanceOperation",
      operationId: "operation-alpha",
      idempotencyKey: "operation-key-123~",
      action: "target.probe",
      resourceKind: "DeploymentTarget",
      resourceId: "docker-alpha",
      resourceGeneration: 2,
      requestedBy: `sha256:${"a".repeat(64)}`,
      requestId: "request-alpha",
      requestedAt: "2026-09-03T08:00:00Z",
      updatedAt: "2026-09-03T08:01:00Z",
      state: "succeeded",
      currentStep: "complete",
      impactSummary: "Probe deployment target connectivity and capabilities",
      retryable: false,
    };
    const operationPage = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "MaintenanceOperationPage",
      operations: [operation],
      nextPageToken: "operation-page-token-2",
    });
    const auditPage = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "AdminAuditEventPage",
      events: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "AdminAuditEvent",
          eventId: "event-alpha",
          actor: `sha256:${"a".repeat(64)}`,
          action: "target.probe",
          resourceKind: "DeploymentTarget",
          resourceId: "docker-alpha",
          resourceGeneration: 2,
          result: "succeeded",
          occurredAt: "2026-09-03T08:01:00Z",
          requestId: "request-alpha",
          operationId: "operation-alpha",
        },
      ],
      nextPageToken: "audit-page-token-2",
    });
    expect(decodeMaintenanceOperationPage(JSON.parse(operationPage)).operations).toHaveLength(1);
    expect(parseMaintenanceOperationPage(operationPage).value.nextPageToken).toBe(
      "operation-page-token-2",
    );
    expect(decodeAdminAuditEventPage(JSON.parse(auditPage)).events).toHaveLength(1);
    expect(parseAdminAuditEventPage(auditPage).value.nextPageToken).toBe("audit-page-token-2");
    expect(() =>
      decodeMaintenanceOperationPage({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "MaintenanceOperationPage",
        operations: [{ ...operation, updatedAt: "2026-09-03T07:59:59Z" }],
      }),
    ).toThrow(/INVALID_MAINTENANCE_OPERATION/u);
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return {
        status: 200,
        headers: {},
        body: request.path.includes("/audit-events") ? auditPage : operationPage,
      };
    });
    await client.listAdminDeploymentTargetOperations(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
      1,
      "operation-page-token-1",
    );
    await client.listAdminMaintenanceOperations(
      "tenant-alpha",
      "project-alpha",
      "request-alpha",
      1,
      "maintenance-page-token-1",
    );
    await client.listAdminDeploymentTargetAuditEvents(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
      1,
      "audit-page-token-1",
    );
    expect(seen.map(({ path }) => path)).toEqual([
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/operations?pageSize=1&pageToken=operation-page-token-1",
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/maintenance-operations?pageSize=1&pageToken=maintenance-page-token-1",
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha/audit-events?pageSize=1&pageToken=audit-page-token-1",
    ]);
  });
  it("previews Admin cleanup impact with generation authority", async () => {
    const body = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "DeploymentTargetCleanupPreview",
      metadata: {
        uid: "docker-alpha",
        name: "docker-alpha",
        tenantRef: {
          namespace: "cloud-agents",
          kind: "tenant",
          id: "tenant-alpha",
        },
        resourceVersion: "7",
        createdAt: "2026-09-03T08:00:00Z",
        updatedAt: "2026-09-03T08:01:00Z",
      },
      spec: {
        projectRef: {
          namespace: "cloud-agents",
          kind: "project",
          id: "project-alpha",
        },
        targetKind: "docker",
        expectedGeneration: 2,
        expectedResourceVersion: "7",
        impactDigest: `sha256:${"a".repeat(64)}`,
        canCleanup: false,
        workers: [
          {
            workerName: "cloud-agents-worker-alpha",
            leaseId: "lease-alpha",
            leaseGeneration: 3,
            disposition: "blocked",
            resources: [
              {
                resourceKind: "container",
                resourceName: "cloud-agents-worker-alpha",
              },
              {
                resourceKind: "workspace-volume",
                resourceName: "workspace-alpha",
              },
            ],
          },
        ],
      },
    });
    expect(decodeDeploymentTargetCleanupPreview(JSON.parse(body)).spec.canCleanup).toBe(false);
    expect(parseDeploymentTargetCleanupPreview(body).value.spec.workers).toHaveLength(1);
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return { status: 200, headers: { "X-Resource-Version": "7" }, body };
    });
    await client.previewAdminDeploymentTargetCleanup(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
    );
    expect(seen[0]?.method).toBe("GET");
    expect(seen[0]?.path).toBe(
      "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup-preview",
    );
  });
  it("executes Admin cleanup through the generated contract", async () => {
    const impactDigest = `sha256:${"a".repeat(64)}` as const;
    const operation = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "MaintenanceOperation",
      operationId: "operation-alpha",
      idempotencyKey: "cleanup-key-1234~",
      action: "target.cleanup",
      resourceKind: "DeploymentTarget",
      resourceId: "docker-alpha",
      resourceGeneration: 2,
      requestedBy: `sha256:${"b".repeat(64)}`,
      requestId: "request-alpha",
      requestedAt: "2026-09-03T08:00:00Z",
      updatedAt: "2026-09-03T08:01:00Z",
      state: "succeeded",
      currentStep: "complete",
      impactSummary: "Cleaned 0 orphan workers and 0 resources",
      retryable: false,
    });
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return { status: 200, headers: {}, body: operation };
    });
    const result = await client.cleanupAdminDeploymentTarget(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
      "cleanup-key-1234~",
      { expectedGeneration: 2, expectedResourceVersion: "7", impactDigest },
    );
    expect(result.value.action).toBe("target.cleanup");
    expect(seen[0]).toMatchObject({
      method: "POST",
      path: "/v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:cleanup",
      headers: { "Idempotency-Key": "cleanup-key-1234~" },
    });
  });
  it("previews and transitions Admin target scheduling through the generated contract", async () => {
    const impactDigest = `sha256:${"c".repeat(64)}` as const;
    const preview = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "DeploymentTargetSchedulingPreview",
      metadata: {
        uid: "docker-alpha",
        name: "docker-alpha",
        tenantRef: {
          namespace: "cloud-agents",
          kind: "tenant",
          id: "tenant-alpha",
        },
        resourceVersion: "7",
        createdAt: "2026-09-04T08:00:00Z",
        updatedAt: "2026-09-04T08:01:00Z",
      },
      spec: {
        projectRef: {
          namespace: "cloud-agents",
          kind: "project",
          id: "project-alpha",
        },
        currentState: "active",
        desiredState: "drained",
        expectedGeneration: 2,
        expectedResourceVersion: "7",
        impactDigest,
        activeLeases: [
          {
            leaseId: "lease-alpha",
            leaseName: "lease-alpha",
            generation: 3,
            observedPhase: "ready",
          },
        ],
      },
    });
    const operation = JSON.stringify({
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "MaintenanceOperation",
      operationId: "operation-drain-alpha",
      idempotencyKey: "scheduling-key-1234~",
      action: "target.drain",
      resourceKind: "DeploymentTarget",
      resourceId: "docker-alpha",
      resourceGeneration: 2,
      requestedBy: `sha256:${"b".repeat(64)}`,
      requestId: "request-alpha",
      requestedAt: "2026-09-04T08:00:00Z",
      updatedAt: "2026-09-04T08:01:00Z",
      state: "succeeded",
      currentStep: "complete",
      impactSummary: "Drained target with 1 active lease",
      retryable: false,
    });
    expect(
      decodeDeploymentTargetSchedulingPreview(JSON.parse(preview)).spec.activeLeases,
    ).toHaveLength(1);
    expect(parseDeploymentTargetSchedulingPreview(preview).value.spec.desiredState).toBe("drained");
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      return request.method === "GET"
        ? { status: 200, headers: { "X-Resource-Version": "7" }, body: preview }
        : { status: 200, headers: {}, body: operation };
    });
    await client.previewAdminDeploymentTargetScheduling(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
    );
    const result = await client.transitionAdminDeploymentTargetScheduling(
      "tenant-alpha",
      "project-alpha",
      "docker-alpha",
      "request-alpha",
      "scheduling-key-1234~",
      {
        expectedGeneration: 2,
        expectedResourceVersion: "7",
        desiredState: "drained",
        impactDigest,
      },
    );
    expect(result.value.action).toBe("target.drain");
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:scheduling-preview",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/deployment-targets/docker-alpha:scheduling",
    ]);
    expect(seen[1]?.body).toContain('"desiredState":"drained"');
  });
  it("previews and executes Admin lease upgrade and rollback through the generated contract", async () => {
    const upgradeDigest = `sha256:${"d".repeat(64)}` as const;
    const rollbackDigest = `sha256:${"e".repeat(64)}` as const;
    const impactDigest = `sha256:${"f".repeat(64)}` as const;
    const preview = (action: "upgrade" | "rollback") =>
      JSON.stringify({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "EnvironmentLeaseUpgradePreview",
        metadata: {
          uid: "lease-alpha",
          name: "lease-alpha",
          tenantRef: {
            namespace: "cloud-agents",
            kind: "tenant",
            id: "tenant-alpha",
          },
          resourceVersion: "9",
          createdAt: "2026-09-04T08:00:00Z",
          updatedAt: "2026-09-04T08:01:00Z",
        },
        spec: {
          projectRef: {
            namespace: "cloud-agents",
            kind: "project",
            id: "project-alpha",
          },
          action,
          targetId: "docker-alpha",
          targetKind: "docker",
          currentReleaseDigest: action === "upgrade" ? rollbackDigest : upgradeDigest,
          targetReleaseDigest: action === "upgrade" ? upgradeDigest : rollbackDigest,
          rollbackReleaseDigest: rollbackDigest,
          rollbackGeneration: 3,
          expectedGeneration: 4,
          expectedResourceVersion: "9",
          affectedTargets: 1,
          affectedWorkers: 1,
          affectedLeases: 1,
          impactDigest,
        },
      });
    const operation = (action: "upgrade" | "rollback") =>
      JSON.stringify({
        apiVersion: "platform.cloud-agents.dev/v1alpha1",
        kind: "MaintenanceOperation",
        operationId: `operation-${action}-alpha`,
        idempotencyKey: `${action}-key-1234~`,
        action: `target.${action}`,
        resourceKind: "DeploymentTarget",
        resourceId: "docker-alpha",
        resourceGeneration: 2,
        requestedBy: `sha256:${"a".repeat(64)}`,
        requestId: `request-${action}`,
        requestedAt: "2026-09-04T08:00:00Z",
        updatedAt: "2026-09-04T08:01:00Z",
        state: "succeeded",
        currentStep: "complete",
        impactSummary: `${action} environment lease generation 4`,
        retryable: false,
      });
    expect(decodeEnvironmentLeaseUpgradePreview(JSON.parse(preview("upgrade"))).spec.action).toBe(
      "upgrade",
    );
    expect(parseEnvironmentLeaseUpgradePreview(preview("rollback")).value.spec.action).toBe(
      "rollback",
    );
    const seen: FixtureRequest[] = [];
    const client = new Client(async (request) => {
      seen.push(request);
      const action = request.path.includes("rollback") ? "rollback" : "upgrade";
      return request.method === "GET"
        ? {
            status: 200,
            headers: { "X-Resource-Version": "9" },
            body: preview(action),
          }
        : { status: 200, headers: {}, body: operation(action) };
    });
    await client.previewAdminEnvironmentLeaseUpgrade(
      "tenant-alpha",
      "project-alpha",
      "lease-alpha",
      upgradeDigest,
      "request-upgrade-preview",
    );
    await client.previewAdminEnvironmentLeaseRollback(
      "tenant-alpha",
      "project-alpha",
      "lease-alpha",
      "request-rollback-preview",
    );
    const request = {
      releaseDigest: upgradeDigest,
      expectedGeneration: 4,
      expectedResourceVersion: "9",
      impactDigest,
    };
    await client.upgradeAdminEnvironmentLease(
      "tenant-alpha",
      "project-alpha",
      "lease-alpha",
      "request-upgrade",
      "upgrade-key-1234~",
      request,
    );
    await client.rollbackAdminEnvironmentLease(
      "tenant-alpha",
      "project-alpha",
      "lease-alpha",
      "request-rollback",
      "rollback-key-1234~",
      { ...request, releaseDigest: rollbackDigest },
    );
    expect(seen.map(({ method, path }) => `${method} ${path}`)).toEqual([
      `GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade-preview?releaseDigest=${encodeURIComponent(upgradeDigest)}`,
      "GET /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:rollback-preview",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:upgrade",
      "POST /v1/admin/tenants/tenant-alpha/projects/project-alpha/environment-leases/lease-alpha:rollback",
    ]);
    expect(seen[2]?.body).toContain(`"releaseDigest":"${upgradeDigest}"`);
    expect(seen[3]?.body).toContain(`"releaseDigest":"${rollbackDigest}"`);
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
      spec: {
        generation: 1,
        state: "succeeded",
        resultDigest: `sha256:${"a".repeat(64)}`,
      },
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
      {
        generation: 1,
        requestId: "codex:generation-1:approval:1",
        decision: "accept",
      },
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
    expect(watchEnvelope.unknown).toEqual({
      "/tenantId": '"cross-tenant-leak"',
    });
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
      if (request.url?.includes("/execution-artifact-invalid/messages/0/artifact")) {
        response.statusCode = 200;
        response.setHeader("Content-Type", "text/plain");
        response.end("missing disposition");
        return;
      }
      if (request.url?.includes("/execution-artifact/messages/0/artifact")) {
        response.statusCode = 200;
        response.setHeader("Content-Type", "application/octet-stream");
        response.setHeader(
          "Content-Disposition",
          "attachment; filename*=utf-8''result%20%E2%9C%93.bin",
        );
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
      expect([
        artifact.data[0],
        artifact.data.at(-1),
        artifact.fileName,
        artifact.contentType,
        artifact.etag,
      ]).toEqual([0x80, 0x80, "result ✓.bin", "application/octet-stream", '"sha256:artifact"']);
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
      await expect(
        client.downloadManagedAgentArtifact(
          "tenant-alpha",
          "project-alpha",
          "session-alpha",
          "turn-alpha",
          "execution-artifact-invalid",
          "request-alpha",
          0,
        ),
      ).rejects.toThrow("artifact Content-Disposition is invalid");
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
    const errorClient = new Client(async () => ({
      status: 404,
      headers: {},
      body: problem,
    }));
    await expect(
      errorClient.getProject("tenant-alpha", "project-alpha", "req-alpha"),
    ).rejects.toMatchObject({
      operation: "managedAgentGetProject",
      status: 404,
    });
    const mismatchClient = new Client(async () => ({
      status: 500,
      headers: {},
      body: problem,
    }));
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
    ).rejects.toMatchObject({
      code: "PATH_BODY_AUTHORITY_MISMATCH",
      path: "/metadata/uid",
    });
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
  return {
    status,
    headers: { "X-Resource-Version": value.metadata?.resourceVersion ?? "" },
    body,
  };
}
