import { describe, expect, it } from "vitest";

import {
  cleanupRequestFromPreview,
  listAdminLeases,
  listAdminProfiles,
  listAdminMaintenanceOperations,
  listAdminTargetAuditEvents,
  listAdminTargetOperations,
  listAdminTargets,
  listAdminWorkers,
  readSavedAdminConnection,
  schedulingRequestFromPreview,
  summarizeClusterHosts,
  writeSavedAdminConnection,
  type AdminClient,
} from "./admin";

describe("Admin Web boundary", () => {
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
