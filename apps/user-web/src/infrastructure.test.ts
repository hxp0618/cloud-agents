import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  JSONContractError,
  type DeploymentTarget,
  type EnvironmentLease,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  infrastructureErrorMessage,
  loadInfrastructure,
  readInfrastructureSelection,
  writeInfrastructureSelection,
  type InfrastructureClient,
} from "./infrastructure";

function storage(initial: string | null = null) {
  let value = initial;
  return {
    getItem: vi.fn(() => value),
    setItem: vi.fn((_key: string, next: string) => {
      value = next;
    }),
    value: () => value,
  };
}

function target(uid: string): DeploymentTarget {
  return { metadata: { uid, name: uid } } as DeploymentTarget;
}

function lease(uid: string): EnvironmentLease {
  return { metadata: { uid, name: uid } } as EnvironmentLease;
}

describe("infrastructure resources", () => {
  it("loads target and lease pages concurrently and sorts them", async () => {
    const listDeploymentTargets = vi
      .fn()
      .mockResolvedValueOnce({
        value: { deploymentTargets: [target("target-zulu")], nextPageToken: "target-next" },
      })
      .mockResolvedValueOnce({ value: { deploymentTargets: [target("target-alpha")] } });
    const listManagedHostEnvironmentLeases = vi.fn().mockResolvedValue({
      value: { environmentLeases: [lease("lease-zulu"), lease("lease-alpha")] },
    });
    const client = {
      listDeploymentTargets,
      listManagedHostEnvironmentLeases,
    } as unknown as InfrastructureClient;

    const resources = await loadInfrastructure(
      client,
      "tenant-local",
      "project-alpha",
      new AbortController().signal,
    );

    expect(resources.targets.map(({ metadata }) => metadata.uid)).toEqual([
      "target-alpha",
      "target-zulu",
    ]);
    expect(resources.leases.map(({ metadata }) => metadata.uid)).toEqual([
      "lease-alpha",
      "lease-zulu",
    ]);
    expect(listDeploymentTargets).toHaveBeenCalledTimes(2);
    expect(listManagedHostEnvironmentLeases.mock.invocationCallOrder[0]).toBeLessThan(
      listDeploymentTargets.mock.invocationCallOrder[1]!,
    );
  });

  it("rejects a repeated page token", async () => {
    const client = {
      listDeploymentTargets: vi.fn().mockResolvedValue({
        value: { deploymentTargets: [], nextPageToken: "repeated-target-page" },
      }),
      listManagedHostEnvironmentLeases: vi
        .fn()
        .mockResolvedValue({ value: { environmentLeases: [] } }),
    } as unknown as InfrastructureClient;

    await expect(
      loadInfrastructure(client, "tenant-local", "project-alpha", new AbortController().signal),
    ).rejects.toThrow("repeated a deployment target page token");
  });
});

describe("infrastructure recovery", () => {
  it("restores only a project-bound target and lease selection", () => {
    const targetStorage = storage();
    writeInfrastructureSelection(targetStorage, {
      tenantId: "tenant-local",
      projectId: "project-alpha",
      targetId: "docker-alpha",
      leaseId: "lease-alpha",
    });

    expect(readInfrastructureSelection(targetStorage, "tenant-local", "project-alpha")).toEqual({
      tenantId: "tenant-local",
      projectId: "project-alpha",
      targetId: "docker-alpha",
      leaseId: "lease-alpha",
    });
    expect(readInfrastructureSelection(targetStorage, "tenant-local", "project-other")).toEqual({
      tenantId: "tenant-local",
      projectId: "project-other",
      targetId: "",
      leaseId: "",
    });
    expect(targetStorage.value()).not.toContain("token");
  });

  it("distinguishes fencing conflicts from contract drift", () => {
    expect(infrastructureErrorMessage(new ClientError("upgrade", 409))).toContain(
      "generation changed",
    );
    expect(infrastructureErrorMessage(new JSONContractError("INVALID_JSON"))).toContain(
      "Platform API contract",
    );
  });
});
