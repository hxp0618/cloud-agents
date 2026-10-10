import { describe, expect, it, vi } from "vitest";
import {
  ClientError,
  type BrowserSession,
  type BrowserSessionClient,
  type Client,
  type Organization,
  type Project,
} from "@cloud-agents/cloud-agent-platform-sdk/platform";

import {
  listAllAdminProjects,
  listAllBrowserTenants,
  loginMessage,
  sessionMessage,
} from "../../src/app/connection";

function metadata(uid: string) {
  return {
    uid,
    name: uid,
    tenantRef: { namespace: "cloud-agents" as const, kind: "tenant" as const, id: "tenant-one" },
    resourceVersion: "1",
    createdAt: "2026-10-08T00:00:00Z",
  };
}

function organization(uid: string): Organization {
  return {
    apiVersion: "platform.cloud-agents.dev/v1alpha1",
    kind: "Organization",
    metadata: metadata(uid),
    spec: {
      tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-one" },
      displayName: uid,
      state: "active",
    },
  };
}

function project(
  uid: string,
  displayName: string,
  state: "active" | "archived" = "active",
): Project {
  return {
    apiVersion: "platform.cloud-agents.dev/v1alpha1",
    kind: "Project",
    metadata: metadata(uid),
    spec: {
      tenantRef: { namespace: "cloud-agents", kind: "tenant", id: "tenant-one" },
      organizationRef: { namespace: "cloud-agents", kind: "organization", id: "org-one" },
      displayName,
      state,
    },
  };
}

describe("Admin session discovery", () => {
  it("returns message keys for localized session failures", () => {
    expect(sessionMessage(new Error("session_wrong_console"))).toBe("auth.wrongConsole");
    expect(sessionMessage(new ClientError("session", 401))).toBe("auth.sessionExpired");
    expect(sessionMessage(new ClientError("session", 403))).toBe("auth.accountDenied");
    expect(sessionMessage(new ClientError("session", 429))).toBe("error.rateLimited");
    expect(sessionMessage(new Error("network"))).toBe("auth.accountLoadFailed");
  });

  it("uses login-specific wording for rejected credentials", () => {
    expect(loginMessage(new ClientError("login", 401))).toBe("auth.invalidCredentials");
    expect(loginMessage(new ClientError("login", 403))).toBe("auth.accountDenied");
    expect(loginMessage(new Error("network"))).toBe("auth.loginFailed");
  });

  it("continues tenant pagination from the session projection", async () => {
    const session = {
      application: "admin",
      user: {
        id: "user-admin",
        email: "admin@example.test",
        displayName: "Admin",
        displayRoles: ["platform.admin"],
      },
      tenants: [{ id: "tenant-one", name: "One", displayRoles: ["tenant.admin"] }],
      nextPageToken: "page-two",
      csrfToken: "C".repeat(43),
    } satisfies BrowserSession;
    const listBrowserTenants = vi.fn(async () => ({
      tenants: [{ id: "tenant-two", name: "Two", displayRoles: [] }],
    }));
    const tenants = await listAllBrowserTenants(
      { listBrowserTenants } as Pick<BrowserSessionClient, "listBrowserTenants">,
      session,
      new AbortController().signal,
    );
    expect(tenants.map(({ id }) => id)).toEqual(["tenant-one", "tenant-two"]);
  });

  it("uses explicit Admin organization/project routes and sorts active projects", async () => {
    const listAdminOrganizations = vi
      .fn()
      .mockResolvedValueOnce({
        value: { organizations: [organization("org-one")], nextPageToken: "page-two" },
      })
      .mockResolvedValueOnce({ value: { organizations: [organization("org-two")] } });
    const listAdminProjects = vi.fn(async (_tenant: string, organizationId: string) => ({
      value: {
        projects:
          organizationId === "org-one"
            ? [project("project-z", "Zulu"), project("project-old", "Old", "archived")]
            : [project("project-a", "Alpha")],
      },
    }));
    const projects = await listAllAdminProjects(
      { listAdminOrganizations, listAdminProjects } as unknown as Pick<
        Client,
        "listAdminOrganizations" | "listAdminProjects"
      >,
      "tenant-one",
      new AbortController().signal,
    );
    expect(projects.map(({ metadata }) => metadata.uid)).toEqual(["project-a", "project-z"]);
    expect(listAdminOrganizations).toHaveBeenCalledTimes(2);
    expect(listAdminProjects).toHaveBeenCalledTimes(2);
  });
});
