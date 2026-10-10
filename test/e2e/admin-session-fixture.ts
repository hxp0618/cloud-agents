import type { Browser } from "@e2e-dev/web";
import type {
  BrowserSession,
  OrganizationPage,
  ProjectPage,
} from "../../sdk/typescript/src/platform";

export async function mockAdminSession(browser: Browser) {
  let signedIn = false;
  const session: BrowserSession = {
    application: "admin",
    user: {
      id: "admin-test",
      email: "admin@example.test",
      displayName: "Test Admin",
      displayRoles: [],
    },
    tenants: [{ id: "tenant-test", name: "Test tenant", displayRoles: ["tenant.admin"] }],
    csrfToken: "C".repeat(43),
  };
  await browser.route("**/v1/identity/**", async (route) => {
    const path = new URL(route.request.url).pathname;
    if (path === "/v1/identity/login/providers") return route.fulfill({ json: { providers: [] } });
    if (path === "/v1/identity/login/password") signedIn = true;
    if (signedIn && (path === "/v1/identity/session" || path === "/v1/identity/login/password"))
      return route.fulfill({ json: session });
    return route.fulfill({ status: 401, body: "Authentication required" });
  });
}

export function adminScopePage(path: string): OrganizationPage | ProjectPage | undefined {
  const tenantRef = { namespace: "cloud-agents", kind: "tenant", id: "tenant-test" } as const;
  const metadata = {
    uid: "org-test",
    name: "org-test",
    tenantRef,
    resourceVersion: "1",
    createdAt: "2026-10-09T00:00:00Z",
  };
  if (path === "/v1/admin/tenants/tenant-test/organizations")
    return {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "OrganizationPage",
      organizations: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "Organization",
          metadata,
          spec: { tenantRef, displayName: "Test organization", state: "active" },
        },
      ],
    };
  if (path === "/v1/admin/tenants/tenant-test/projects")
    return {
      apiVersion: "platform.cloud-agents.dev/v1alpha1",
      kind: "ProjectPage",
      projects: [
        {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: "Project",
          metadata: { ...metadata, uid: "project-test", name: "project-test" },
          spec: {
            tenantRef,
            organizationRef: { namespace: "cloud-agents", kind: "organization", id: "org-test" },
            displayName: "Test project",
            state: "active",
          },
        },
      ],
    };
}
