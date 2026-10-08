import { describe, expect, it } from "vitest";
import { type AdminClient } from "../../src/admin";
import { loadAdminPollData, loadAdminWorkspaceData } from "../../src/app/loaders";

const connection = {
  endpoint: "https://admin.example.test",
  tenantId: "tenant",
  projectId: "project",
};

function fixture(failing = "") {
  const calls: string[] = [];
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
    listAdminProjectLeaseQuotaAuditEvents: { events: [] },
    listAdminMaintenanceOperations: { operations: [] },
  };
  const client = new Proxy(
    {},
    {
      get: (_target, method: string) => async () => {
        calls.push(method);
        if (method === failing) throw new Error(method);
        return { value: responses[method] };
      },
    },
  ) as unknown as AdminClient;
  return { calls, client };
}

describe("Admin page loaders", () => {
  it("connects and refreshes overview/targets without unrelated quota audit", async () => {
    const { calls, client } = fixture("listAdminProjectLeaseQuotaAuditEvents");
    for (const page of ["overview", "targets"] as const) {
      const loaded = await loadAdminWorkspaceData(
        client,
        connection,
        "",
        "",
        "",
        new AbortController().signal,
        page,
      );
      expect(loaded.data.targets?.targets).toEqual([]);
      expect(loaded.errors).toEqual({});
    }
    expect(calls).toEqual([
      "listAdminDeploymentTargets",
      "listAdminEnvironmentLeases",
      "listAdminWorkers",
      "listAdminMaintenanceOperations",
      "listAdminDeploymentTargets",
    ]);
  });

  it("loads a newly selected page, isolates resource failure and preserves previous authority on refresh", async () => {
    const { calls, client } = fixture("listAdminProjectLeaseQuotaAuditEvents");
    const previousAudit = [{ kind: "AdminAuditEvent" }];
    const loaded = await loadAdminWorkspaceData(
      client,
      connection,
      "",
      "",
      "",
      new AbortController().signal,
      "quotas",
    );
    expect(calls).toEqual(["getAdminProjectLeaseQuota", "listAdminProjectLeaseQuotaAuditEvents"]);
    expect(Object.hasOwn(loaded.data, "quota")).toBe(true);
    expect(Object.hasOwn(loaded.data, "quotaAudit")).toBe(false);
    expect(loaded.errors.quotaAudit).toBeInstanceOf(Error);
    expect({ quotaAudit: previousAudit, ...loaded.data }.quotaAudit).toBe(previousAudit);
  });

  it("loads Profile editor dependencies without workspace histories", async () => {
    const { calls, client } = fixture();
    await loadAdminWorkspaceData(
      client,
      connection,
      "",
      "",
      "",
      new AbortController().signal,
      "profiles",
    );
    expect(calls).toEqual([
      "listAdminEnvironmentProfiles",
      "listAdminWorkerReleases",
      "listAdminStoragePolicies",
      "listAdminNetworkPolicies",
    ]);
  });

  it("polls only pending resources and keeps successful authority when a peer fails", async () => {
    const { calls, client } = fixture("listAdminDeploymentTargets");
    const loaded = await loadAdminPollData(
      client,
      connection,
      "",
      "",
      {
        targets: true,
        leases: false,
        workers: false,
        sandboxes: true,
        workspaceSnapshots: false,
      },
      new AbortController().signal,
    );
    expect(calls).toEqual(["listAdminDeploymentTargets", "listAdminSandboxSessions"]);
    expect(loaded.data.sandboxes).toEqual([]);
    expect(loaded.errors.targets).toBeInstanceOf(Error);
  });
});
