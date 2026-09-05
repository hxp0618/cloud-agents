import { describe, expect, it } from "vitest";
import { ClientError } from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  adminFailure,
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
