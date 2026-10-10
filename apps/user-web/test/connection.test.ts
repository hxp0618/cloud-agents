import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  JSONContractError,
  type BrowserSession,
  type BrowserSessionClient,
  type Client,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  listAllBrowserTenants,
  loadUserProjects,
  readSavedConnection,
  sessionErrorMessage,
  writeSavedConnection,
} from "../src/connection";

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

function project(
  uid: string,
  displayName: string,
  state: "active" | "suspended" = "active",
): Project {
  return {
    apiVersion: "platform.cloud-agents.dev/v1alpha1",
    kind: "Project",
    metadata: {
      uid,
      name: uid,
      tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
      resourceVersion: "1",
      createdAt: "2026-09-02T00:00:00Z",
    },
    spec: {
      tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-local" },
      organizationRef: { namespace: "cloud-agents", kind: "organization", id: "org-local" },
      displayName,
      state,
    },
  };
}

describe("session selection context", () => {
  it("stores only tenant and project identifiers", () => {
    const target = storage(
      '{"endpoint":"https://legacy.example.test","tenantId":"tenant-local","projectId":"project-alpha"}',
    );
    expect(readSavedConnection(target)).toEqual({
      tenantId: "tenant-local",
      projectId: "project-alpha",
    });
    expect(target.value()).toBe('{"tenantId":"tenant-local","projectId":"project-alpha"}');

    writeSavedConnection(target, { tenantId: "tenant-two", projectId: "project-two" });
    expect(target.value()).not.toContain("endpoint");
  });

  it("ignores invalid browser state", () => {
    const target = storage('{"tenantId":7}');
    expect(readSavedConnection(target)).toEqual({ tenantId: "", projectId: "" });
  });

  it("collects every session tenant page without accepting duplicate authority", async () => {
    const session = {
      application: "user",
      user: { id: "user-one", email: "user@example.test", displayName: "User", displayRoles: [] },
      tenants: [{ id: "tenant-one", name: "One", displayRoles: [] }],
      nextPageToken: "page-two",
      csrfToken: "C".repeat(43),
    } satisfies BrowserSession;
    const listBrowserTenants = vi.fn(async () => ({
      tenants: [{ id: "tenant-two", name: "Two", displayRoles: [] }],
    }));
    await expect(
      listAllBrowserTenants(
        { listBrowserTenants } as Pick<BrowserSessionClient, "listBrowserTenants">,
        session,
        new AbortController().signal,
      ),
    ).resolves.toHaveLength(2);
    expect(listBrowserTenants).toHaveBeenCalledWith(200, "page-two", expect.any(AbortSignal));
  });
});

describe("loadUserProjects", () => {
  it("uses my-projects pagination and keeps only sorted active projects", async () => {
    const listMyProjects = vi
      .fn()
      .mockResolvedValueOnce({
        value: { projects: [project("project-z", "Zulu")], nextPageToken: "page-two" },
        unknown: {},
      })
      .mockResolvedValueOnce({
        value: {
          projects: [project("project-a", "Alpha"), project("project-off", "Hidden", "suspended")],
        },
        unknown: {},
      });
    const result = await loadUserProjects(
      { listMyProjects } as Pick<Client, "listMyProjects">,
      "tenant-local",
      new AbortController().signal,
    );
    expect(result.map(({ metadata }) => metadata.uid)).toEqual(["project-a", "project-z"]);
    expect(listMyProjects).toHaveBeenCalledTimes(2);
  });
});

describe("sessionErrorMessage", () => {
  it("distinguishes authentication and authorization without mentioning pasted tokens", () => {
    expect(sessionErrorMessage(new ClientError("session", 401))).toContain("session");
    expect(sessionErrorMessage(new ClientError("session", 403))).toContain("account");
    expect(sessionErrorMessage(new JSONContractError("INVALID_JSON"))).toContain(
      "Platform API contract",
    );
  });
});
