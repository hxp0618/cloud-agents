import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  JSONContractError,
  type Client,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  connectionErrorMessage,
  loadConnectionData,
  readSavedConnection,
  writeSavedConnection,
} from "./connection";

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

function metadata(uid: string, name = uid) {
  return {
    uid,
    name,
    tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
    resourceVersion: "1",
    createdAt: "2026-09-02T00:00:00Z",
  };
}

describe("connection context", () => {
  it("stores only non-secret connection identifiers", () => {
    const target = storage();
    writeSavedConnection(target, {
      endpoint: "https://agents.example.test",
      tenantId: "tenant-local",
      projectId: "project-alpha",
    });

    expect(target.value()).toBe(
      '{"endpoint":"https://agents.example.test","tenantId":"tenant-local","projectId":"project-alpha"}',
    );
    expect(readSavedConnection(target)).toEqual({
      endpoint: "https://agents.example.test",
      tenantId: "tenant-local",
      projectId: "project-alpha",
    });
  });

  it("ignores invalid browser state", () => {
    expect(readSavedConnection(storage('{"endpoint":42}'))).toEqual({
      endpoint: "",
      tenantId: "",
      projectId: "",
    });
  });
});

describe("loadConnectionData", () => {
  it("loads paginated organizations and projects, then sorts projects", async () => {
    const listOrganizations = vi
      .fn()
      .mockResolvedValueOnce({
        value: {
          organizations: [
            {
              apiVersion: "platform.cloud-agents.dev/v1alpha1",
              kind: "Organization",
              metadata: metadata("org-a"),
              spec: {
                tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
                displayName: "A",
                state: "active",
              },
            },
          ],
          nextPageToken: "organization-page-two",
        },
        unknown: {},
      })
      .mockResolvedValueOnce({
        value: {
          organizations: [
            {
              apiVersion: "platform.cloud-agents.dev/v1alpha1",
              kind: "Organization",
              metadata: metadata("org-b"),
              spec: {
                tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
                displayName: "B",
                state: "active",
              },
            },
          ],
        },
        unknown: {},
      });
    const listProjects = vi.fn(async (_tenantId: string, organizationId: string) => ({
      value: {
        projects: [
          {
            apiVersion: "platform.cloud-agents.dev/v1alpha1",
            kind: "Project",
            metadata: metadata(`project-${organizationId}`),
            spec: {
              tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
              organizationRef: {
                namespace: "cloud-agents",
                kind: "organization",
                id: organizationId,
              },
              displayName: organizationId === "org-a" ? "Zulu" : "Alpha",
              state: "active",
            },
          },
        ],
      },
      unknown: {},
    }));
    const client = {
      getPlatformTenant: vi.fn(async () => ({
        value: {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "PlatformTenant",
          metadata: metadata("tenant-local"),
          spec: { displayName: "Local Tenant", state: "active" },
        },
        unknown: {},
      })),
      listOrganizations,
      listProjects,
    } as unknown as Pick<Client, "getPlatformTenant" | "listOrganizations" | "listProjects">;

    const result = await loadConnectionData(client, "tenant-local", new AbortController().signal);

    expect(result.tenant.spec.displayName).toBe("Local Tenant");
    expect(result.projects.map(({ spec }) => spec.displayName)).toEqual(["Alpha", "Zulu"]);
    expect(listOrganizations).toHaveBeenCalledTimes(2);
    expect(listProjects).toHaveBeenCalledTimes(2);
  });
});

describe("connectionErrorMessage", () => {
  it("distinguishes authentication from authorization", () => {
    expect(connectionErrorMessage(new ClientError("connect", 401))).toContain("rejected");
    expect(connectionErrorMessage(new ClientError("connect", 403))).toContain("cannot access");
  });

  it("does not report a contract violation as an endpoint format error", () => {
    expect(connectionErrorMessage(new JSONContractError("INVALID_JSON"))).toContain(
      "Platform API contract",
    );
  });
});
