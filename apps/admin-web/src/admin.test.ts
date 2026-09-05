import { describe, expect, it } from "vitest";
import { ClientError } from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminFailure,
  filterAdminTargets,
  filterAdminMaintenanceOperations,
  filterAdminLeases,
  filterAdminWorkers,
  leaseNeedsAttention,
  cleanupRequestFromPreview,
  leaseReleaseRequestFromPreview,
  listAdminProjectLeaseQuotaAuditEvents,
  listAdminLeases,
  listAdminProfiles,
  listAdminStoragePolicies,
  listAdminNetworkPolicies,
  listAdminStoragePolicyAuditEvents,
  listAdminReleases,
  listAdminMaintenanceOperations,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  listAdminWorkers,
  loadAdminProjectLeaseQuota,
  readSavedAdminConnection,
  schedulingRequestFromPreview,
  summarizeClusterHosts,
  writeSavedAdminConnection,
  type AdminClient,
} from "./admin";

describe("Admin Web boundary", () => {
  it("filters exact failed state and sorts by actual update instant without mutating the snapshot", () => {
    const operations = [
      { operationId: "b", state: "failed", updatedAt: "2026-09-05T10:00:00+08:00" },
      { operationId: "running", state: "running", updatedAt: "2026-09-05T04:00:00Z" },
      { operationId: "a", state: "failed", updatedAt: "2026-09-05T02:00:00Z" },
      { operationId: "new", state: "failed", updatedAt: "2026-09-05T03:00:00Z" },
      {
        operationId: "succeeded-failed-name",
        state: "succeeded",
        updatedAt: "2026-09-05T05:00:00Z",
      },
    ].map((operation) =>
      Object.freeze({
        ...operation,
        action: "target.probe",
        resourceKind: "DeploymentTarget",
        resourceId: "target-1",
        currentStep: "probe",
        requestId: "request-1",
        stableErrorCode: operation.state === "failed" ? "docker-probe-unconfigured" : undefined,
        impactSummary: "do-not-search-this",
        idempotencyKey: "do-not-search-this",
      }),
    ) as unknown as Parameters<typeof filterAdminMaintenanceOperations>[0];
    const before = [...operations];
    Object.freeze(operations);
    expect(
      filterAdminMaintenanceOperations(operations, "", true).map((x) => x.operationId),
    ).toEqual(["new", "a", "b"]);
    expect(filterAdminMaintenanceOperations(operations, " DOCKER-PROBE ", true)).toHaveLength(3);
    expect(filterAdminMaintenanceOperations(operations, "succeeded", true)).toEqual([]);
    expect(filterAdminMaintenanceOperations(operations, "request-1", false)).toHaveLength(5);
    expect(filterAdminMaintenanceOperations(operations, "do-not-search-this", false)).toEqual([]);
    expect(filterAdminMaintenanceOperations(operations, "", false)[0]?.operationId).toBe(
      "succeeded-failed-name",
    );
    expect(operations).toEqual(before);
  });
  it("uses the same failed-or-blocked lease predicate for overview counts and filtered search", () => {
    const leases = [
      ["ready", "pending"],
      ["failed", "pending"],
      ["terminating", "blocked"],
      ["failed", "blocked"],
      ["terminated", "complete"],
    ].map(([observedPhase, cleanupPhase], index) => ({
      metadata: { uid: `lease-${index}`, name: `Lease ${index}` },
      spec: {
        observedPhase,
        cleanupPhase,
        environmentId: `env-${index}`,
        providerCredentialRef: "private-credential",
      },
    })) as unknown as Parameters<typeof filterAdminLeases>[0];
    const attention = leases.filter(leaseNeedsAttention);
    expect(attention).toEqual([leases[1], leases[2], leases[3]]);
    expect(filterAdminLeases(leases, "", true)).toEqual(attention);
    expect(filterAdminLeases(leases, " FAILED ", true)).toEqual([leases[1], leases[3]]);
    expect(filterAdminLeases(leases, "env-2", true)).toEqual([leases[2]]);
    expect(filterAdminLeases(leases, "lease-0", true)).toEqual([]);
    expect(filterAdminLeases(leases, "lease-0", false)).toEqual([leases[0]]);
    expect(filterAdminLeases(leases, "", false)).toEqual(leases);
    expect(filterAdminLeases(leases, "private-credential", false)).toEqual([]);
    expect(filterAdminLeases(leases, "", false, "failed")).toEqual([leases[1], leases[3]]);
    expect(filterAdminLeases(leases, "", false, "", true)).toEqual([leases[2], leases[3]]);
    expect(filterAdminLeases(leases, "", false, "failed", true)).toEqual([leases[3]]);
    expect(filterAdminLeases(leases, "env-2", true, "terminating", true)).toEqual([leases[2]]);
    expect(filterAdminLeases(leases, "env-0", false, "failed")).toEqual([]);
    expect(filterAdminLeases(leases, "", true, "ready")).toEqual([]);
  });
  it("combines target search, kind and phase without searching endpoint or credentials", () => {
    const targets = (["docker", "kubernetes", "ssh"] as const).map((targetKind, index) => ({
      metadata: { uid: `target-${index}`, name: `Host ${targetKind}` },
      spec: {
        targetKind,
        observedPhase: index === 0 ? "ready" : "unprobed",
        schedulingState: "active",
        engineVersion: index === 0 ? "29.4.0" : "",
        apiVersion: index === 0 ? "1.54" : "",
        os: index === 0 ? "linux" : "",
        architecture: index === 0 ? "arm64" : "",
        endpoint: "https://private-endpoint.test",
        credentialRef: "private-credential",
      },
    })) as unknown as Parameters<typeof filterAdminTargets>[0];
    expect(filterAdminTargets(targets, "", [], [])).toEqual(targets);
    expect(filterAdminTargets(targets, " HOST ", ["docker"], ["ready"])).toEqual([targets[0]]);
    expect(filterAdminTargets(targets, "target-1", [], ["unprobed"])).toEqual([targets[1]]);
    expect(filterAdminTargets(targets, "", ["docker"], ["unprobed"])).toEqual([]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], [])).toEqual([
      targets[0],
      targets[2],
    ]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], ["ready", "unprobed"])).toEqual([
      targets[0],
      targets[2],
    ]);
    expect(filterAdminTargets(targets, "", ["docker", "ssh"], ["unprobed"])).toEqual([targets[2]]);
    for (const fact of ["29.4.0", "1.54", "LINUX", "arm64"])
      expect(filterAdminTargets(targets, fact, [], [])).toEqual([targets[0]]);
    for (const secret of ["private-endpoint", "private-credential"])
      expect(filterAdminTargets(targets, secret, [], [])).toEqual([]);
    expect(targets).toHaveLength(3);
  });
  it("shows only validated stable error codes and localized messages, never raw diagnostics", () => {
    const problem = {
      type: "https://problems.cloud-agents.dev/environment-cleanup-unavailable",
      title: "untrusted diagnostics must not render",
      status: 503,
      error: { code: "ENVIRONMENT_CLEANUP_UNAVAILABLE", retryable: true },
      requestId: "test-safe-feedback",
    };
    expect(adminFailure(new ClientError("cleanup", 503, problem))).toEqual({
      key: "error.actuatorUnavailable",
      code: "ENVIRONMENT_CLEANUP_UNAVAILABLE",
    });
    for (const invalid of [
      { ...problem, credential: "secret-bytes" },
      { ...problem, error: { ...problem.error, code: "secret-bytes" } },
      { ...problem, status: 500 },
      null,
    ])
      expect(adminFailure(new ClientError("cleanup", 503, invalid))).toEqual({
        key: "error.actuatorUnavailable",
        code: null,
      });
    expect(adminFailure(new Error("secret-bytes"))).toEqual({ key: "error.generic", code: null });
  });
  it("submits the exact cleanup fences returned by the preview", () => {
    expect(
      cleanupRequestFromPreview({
        spec: {
          expectedGeneration: 7,
          expectedResourceVersion: "42",
          impactDigest: `sha256:${"a".repeat(64)}`,
        },
      } as unknown as Parameters<typeof cleanupRequestFromPreview>[0]),
    ).toEqual({
      expectedGeneration: 7,
      expectedResourceVersion: "42",
      impactDigest: `sha256:${"a".repeat(64)}`,
    });
  });

  it("submits the exact scheduling transition returned by the preview", () => {
    expect(
      schedulingRequestFromPreview({
        spec: {
          expectedGeneration: 7,
          expectedResourceVersion: "42",
          desiredState: "drained",
          impactDigest: `sha256:${"b".repeat(64)}`,
        },
      } as unknown as Parameters<typeof schedulingRequestFromPreview>[0]),
    ).toEqual({
      expectedGeneration: 7,
      expectedResourceVersion: "42",
      desiredState: "drained",
      impactDigest: `sha256:${"b".repeat(64)}`,
    });
  });

  it("submits the exact release transition fences returned by the preview", () => {
    expect(
      leaseReleaseRequestFromPreview({
        spec: {
          targetReleaseDigest: `sha256:${"c".repeat(64)}`,
          expectedGeneration: 8,
          expectedResourceVersion: "43",
          impactDigest: `sha256:${"d".repeat(64)}`,
        },
      } as unknown as Parameters<typeof leaseReleaseRequestFromPreview>[0]),
    ).toEqual({
      releaseDigest: `sha256:${"c".repeat(64)}`,
      expectedGeneration: 8,
      expectedResourceVersion: "43",
      impactDigest: `sha256:${"d".repeat(64)}`,
    });
  });

  it("uses only Admin API target pagination", async () => {
    const paths: Array<string | undefined> = [];
    const client = {
      listAdminDeploymentTargets: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        paths.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "DeploymentTargetPage" as const,
            deploymentTargets: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminTargets(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(paths).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API lease pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminEnvironmentLeases: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "EnvironmentLeasePage" as const,
            environmentLeases: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminLeases(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API Worker pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminWorkers: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "WorkerPage" as const,
            workers: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminWorkers(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API release pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminWorkerReleases: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "WorkerReleasePage" as const,
            workerReleases: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminReleases(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("filters persisted Worker health separately from lifecycle failures without browser-clock guesses", () => {
    const workers = ["online", "expired", "unavailable", undefined, undefined].map((health, i) => ({
      metadata: { uid: `worker-${i}`, name: `worker-${i}` },
      spec: {
        leaseId: `worker-${i}`,
        targetId: "docker-alpha",
        targetKind: "docker",
        releaseDigest: "sha256:test",
        state: i === 4 ? "failed" : "ready",
        ...(health ? { health: { state: health, checkedAt: "2000-01-01T00:00:00Z" } } : {}),
      },
    })) as unknown as Parameters<typeof filterAdminWorkers>[0];
    expect(filterAdminWorkers(workers, "", "online").map((w) => w.metadata.uid)).toEqual([
      "worker-0",
    ]);
    expect(filterAdminWorkers(workers, "", "expired")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "", "unavailable")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "", "not-observed")).toHaveLength(2);
    expect(filterAdminWorkers(workers, "", "failed").map((w) => w.metadata.uid)).toEqual([
      "worker-4",
    ]);
    expect(filterAdminWorkers(workers, " Docker-Alpha ", "online")).toHaveLength(1);
    expect(filterAdminWorkers(workers, "worker-1", "online")).toHaveLength(0);
  });

  it("groups current Worker authority by its Deployment Target", () => {
    const targets = [
      { metadata: { uid: "docker-alpha" } },
      { metadata: { uid: "kubernetes-alpha" } },
    ] as unknown as Parameters<typeof summarizeClusterHosts>[0];
    const workers = [
      {
        spec: {
          targetId: "docker-alpha",
          state: "ready",
          lastHealthAt: "2026-09-04T01:00:00Z",
        },
      },
      {
        spec: {
          targetId: "docker-alpha",
          state: "failed",
          lastHealthAt: "2026-09-04T02:00:00Z",
        },
      },
      { spec: { targetId: "removed-target", state: "ready" } },
    ] as unknown as Parameters<typeof summarizeClusterHosts>[1];

    expect(
      summarizeClusterHosts(targets, workers).map(
        ({ target, workerCount, readyWorkerCount, latestHealthAt }) => ({
          targetId: target.metadata.uid,
          workerCount,
          readyWorkerCount,
          latestHealthAt,
        }),
      ),
    ).toEqual([
      {
        targetId: "docker-alpha",
        workerCount: 2,
        readyWorkerCount: 1,
        latestHealthAt: "2026-09-04T02:00:00Z",
      },
      {
        targetId: "kubernetes-alpha",
        workerCount: 0,
        readyWorkerCount: 0,
        latestHealthAt: undefined,
      },
    ]);
  });

  it("uses only Admin API profile pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminEnvironmentProfiles: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "EnvironmentProfilePage" as const,
            environmentProfiles: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminProfiles(client, "tenant-alpha", "project-alpha", new AbortController().signal);
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("uses only Admin API storage policy and audit pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminStoragePolicies: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`policies:${pageToken ?? "first"}`);
        return {
          value: {
            storagePolicies: [],
            ...(pageToken === undefined ? { nextPageToken: "next-policy-page" } : {}),
          },
        };
      },
      listAdminStoragePolicyAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        policyId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        calls.push(`audit:${policyId}:${pageToken ?? "first"}`);
        return {
          value: {
            events: [],
            ...(pageToken === undefined ? { nextPageToken: "next-audit-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    await listAdminStoragePolicies(client, "tenant-alpha", "project-alpha", signal);
    await listAdminStoragePolicyAuditEvents(
      client,
      "tenant-alpha",
      "project-alpha",
      "storage-standard",
      signal,
    );
    expect(calls).toEqual([
      "policies:first",
      "policies:next-policy-page",
      "audit:storage-standard:first",
      "audit:storage-standard:next-audit-page",
    ]);
  });

  it("stops repeated network-policy cursors instead of looping forever", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminNetworkPolicies: async (
        _tenant: string,
        _project: string,
        _request: string,
        _size: number,
        cursor?: string,
      ) => {
        tokens.push(cursor);
        return { value: { networkPolicies: [], nextPageToken: "repeated" } };
      },
    } as unknown as AdminClient;
    await expect(
      listAdminNetworkPolicies(
        client,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    expect(tokens).toEqual([undefined, "repeated"]);
  });

  it("loads project Lease quota from Admin API and treats no policy as optional", async () => {
    const quota = { kind: "ProjectLeaseQuota" };
    const getAdminProjectLeaseQuota = async () => ({ value: quota });
    await expect(
      loadAdminProjectLeaseQuota(
        { getAdminProjectLeaseQuota } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toBe(quota);

    await expect(
      loadAdminProjectLeaseQuota(
        {
          getAdminProjectLeaseQuota: async () => {
            throw new ClientError("quota", 404);
          },
        } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toBeUndefined();
  });

  it("uses only Admin API quota audit pagination", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminProjectLeaseQuotaAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            events: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminProjectLeaseQuotaAuditEvents(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );
    expect(tokens).toEqual([undefined, "next-page"]);

    await expect(
      listAdminProjectLeaseQuotaAuditEvents(
        {
          listAdminProjectLeaseQuotaAuditEvents: async () => {
            throw new ClientError("quota", 404);
          },
        } as unknown as AdminClient,
        "tenant-alpha",
        "project-alpha",
        new AbortController().signal,
      ),
    ).resolves.toEqual([]);
  });

  it("reads target activity only through scoped Admin API pagination", async () => {
    const calls: string[] = [];
    const client = {
      listAdminDeploymentTargetOperations: async (
        _tenantId: string,
        _projectId: string,
        targetId: string,
        _requestId: string,
        _pageSize?: number,
        _pageToken?: string,
      ) => {
        calls.push(`operations:${targetId}`);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "MaintenanceOperationPage" as const,
            operations: [],
          },
        };
      },
      listAdminDeploymentTargetAuditEvents: async (
        _tenantId: string,
        _projectId: string,
        targetId: string,
      ) => {
        calls.push(`audit:${targetId}`);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "AdminAuditEventPage" as const,
            events: [],
          },
        };
      },
    } as unknown as AdminClient;
    const signal = new AbortController().signal;
    await Promise.all([
      listAdminTargetOperations(client, "tenant-alpha", "project-alpha", "docker-alpha", signal),
      listAdminTargetAuditEvents(client, "tenant-alpha", "project-alpha", "docker-alpha", signal),
    ]);
    expect(calls).toEqual(["operations:docker-alpha", "audit:docker-alpha"]);
  });

  it("reads project maintenance operations through the generated Admin API", async () => {
    const tokens: Array<string | undefined> = [];
    const client = {
      listAdminMaintenanceOperations: async (
        _tenantId: string,
        _projectId: string,
        _requestId: string,
        _pageSize?: number,
        pageToken?: string,
      ) => {
        tokens.push(pageToken);
        return {
          value: {
            apiVersion: "platform.cloud-agents.dev/v1alpha1" as const,
            kind: "MaintenanceOperationPage" as const,
            operations: [],
            ...(pageToken === undefined ? { nextPageToken: "next-page" } : {}),
          },
        };
      },
    } as unknown as AdminClient;

    await listAdminMaintenanceOperations(
      client,
      "tenant-alpha",
      "project-alpha",
      new AbortController().signal,
    );
    expect(tokens).toEqual([undefined, "next-page"]);
  });

  it("persists context but never bearer or target credentials", () => {
    let value = "";
    const storage = {
      getItem: () => value || null,
      setItem: (_key: string, next: string) => {
        value = next;
      },
    };
    writeSavedAdminConnection(storage, {
      endpoint: "https://control-plane.example.test",
      tenantId: "tenant-alpha",
      projectId: "project-alpha",
      token: "secret-token",
      credentialRef: "docker-secret",
    } as unknown as Parameters<typeof writeSavedAdminConnection>[1]);
    expect(value).not.toContain("secret-token");
    expect(value).not.toContain("docker-secret");
    expect(readSavedAdminConnection(storage)).toEqual({
      endpoint: "https://control-plane.example.test",
      tenantId: "tenant-alpha",
      projectId: "project-alpha",
    });
  });
});
