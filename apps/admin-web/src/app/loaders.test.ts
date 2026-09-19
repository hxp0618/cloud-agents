import { describe, expect, it } from "vitest";

import { type AdminClient } from "../admin";
import { loadAdminWorkspaceData } from "./loaders";

describe("Admin workspace loaders", () => {
  it("keeps the full resource request set in one abortable batch", async () => {
    const calls: Array<{ method: string; args: readonly unknown[] }> = [];
    const responses: Record<string, unknown> = {
      listAdminDeploymentTargets: { deploymentTargets: [] },
      listAdminEnvironmentLeases: { environmentLeases: [] },
      listAdminWorkers: { workers: [] },
      listAdminWorkerReleases: { workerReleases: [] },
      listAdminEnvironmentProfiles: { environmentProfiles: [] },
      listAdminRuntimeProfiles: { runtimeProfiles: [] },
      listAdminSandboxSessions: { sandboxSessions: [] },
      listAdminWorkspaceSnapshots: { workspaceSnapshots: [] },
      listAdminStoragePolicies: { storagePolicies: [] },
      listAdminNetworkPolicies: { networkPolicies: [] },
      listAdminMcpServers: { mcpServers: [] },
      listAdminSkillBundles: { skillBundles: [] },
      getAdminProjectLeaseQuota: undefined,
      listAdminProjectLeaseQuotaAuditEvents: { events: [] },
      listAdminMaintenanceOperations: { operations: [] },
    };
    const client = new Proxy(
      {},
      {
        get:
          (_target, property: string) =>
          (...args: readonly unknown[]) => {
            calls.push({ method: property, args });
            return Promise.resolve({ value: responses[property] });
          },
      },
    ) as unknown as AdminClient;
    const signal = new AbortController().signal;

    const loaded = await loadAdminWorkspaceData(
      client,
      { endpoint: "https://admin.example.test", tenantId: "tenant", projectId: "project" },
      "",
      "",
      "",
      signal,
    );

    expect(loaded.targets.targets).toEqual([]);
    expect(loaded.leases.leases).toEqual([]);
    expect(loaded.profiles.profiles).toEqual([]);
    expect(calls.map(({ method }) => method)).toEqual([
      "listAdminDeploymentTargets",
      "listAdminEnvironmentLeases",
      "listAdminWorkers",
      "listAdminWorkerReleases",
      "listAdminEnvironmentProfiles",
      "listAdminRuntimeProfiles",
      "listAdminSandboxSessions",
      "listAdminWorkspaceSnapshots",
      "listAdminStoragePolicies",
      "listAdminNetworkPolicies",
      "listAdminMcpServers",
      "listAdminSkillBundles",
      "getAdminProjectLeaseQuota",
      "listAdminProjectLeaseQuotaAuditEvents",
      "listAdminMaintenanceOperations",
    ]);
    expect(calls.every(({ args }) => args.includes(signal))).toBe(true);
    expect(
      calls.every(({ args }) => args.some((value) => String(value).startsWith("admin-"))),
    ).toBe(true);
  });
});
