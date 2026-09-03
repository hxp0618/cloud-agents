import { describe, expect, it } from "vitest";

import {
  listAdminLeases,
  listAdminTargets,
  readSavedAdminConnection,
  writeSavedAdminConnection,
  type AdminClient,
} from "./admin";

describe("Admin Web boundary", () => {
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
