import { test, type Browser } from "@e2e-dev/web";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { setTimeout as delay } from "node:timers/promises";
import {
  CLIIdentityClient,
  ClientError,
  parseIdentityAccountPage,
  parseMembershipPage,
  parseOrganizationPage,
  parsePlatformTenant,
  parseProject,
  parseRoleBindingPage,
  parseRolePage,
  type FixtureTransport,
} from "../../sdk/typescript/dist/platform.mjs";
import { expect, type Screen } from "e2e";

const adminURL = requiredURL("CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL");
const userURL = requiredURL("CLOUD_AGENTS_IDENTITY_E2E_USER_URL");
const controlPlaneURL = requiredURL("CLOUD_AGENTS_IDENTITY_E2E_CONTROL_PLANE_URL");
const password = required("CLOUD_AGENTS_IDENTITY_E2E_PASSWORD");
const invitedPassword = required("CLOUD_AGENTS_IDENTITY_E2E_INVITED_PASSWORD");
const memberRequestIDsFile = required("CLOUD_AGENTS_IDENTITY_E2E_MEMBER_REQUEST_IDS_FILE");
const cliBinary = required("CLOUD_AGENTS_IDENTITY_E2E_CLI_BINARY");
const cliProfile = required("CLOUD_AGENTS_IDENTITY_E2E_CLI_PROFILE");
const cliVerificationURLFile = required("CLOUD_AGENTS_IDENTITY_E2E_CLI_VERIFICATION_URL_FILE");

const accounts = Object.freeze({
  platform: "platform-admin@identity.test",
  tenant: "tenant-admin@identity.test",
  member: "project-member@identity.test",
});

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") throw new Error(`${name} is required`);
  return value;
}

function requiredURL(name: string): string {
  const value = new URL(required(name));
  if (value.protocol !== "https:" || !value.hostname.endsWith(".localhost"))
    throw new Error(`${name} must be an owned HTTPS .localhost origin`);
  return value.origin;
}

async function passwordLogin(
  browser: Browser,
  screen: Screen,
  email: string,
  accountPassword = password,
) {
  const responsePromise = browser.waitForResponse("**/v1/identity/login/password");
  await screen.getByLabel("Email").fill(email);
  await screen.getByLabel("Password").fill(accountPassword);
  await screen.getByRole("button", "Sign in").tap();
  const response = await responsePromise;
  expect(response.status).toBe(200);
  expect(response.headers["x-cloud-agents-session"]).toBeUndefined();
  expect(response.headers.authorization).toBeUndefined();
  const body = (await response.json()) as Record<string, unknown>;
  expect(body.accessToken).toBeUndefined();
  expect(body.refreshToken).toBeUndefined();
  expect(body.idToken).toBeUndefined();
  expect(body.sessionHandle).toBeUndefined();
  expect(typeof body.csrfToken).toBe("string");
  return body as Readonly<{ csrfToken: string }>;
}

async function assertBrowserHasNoReusableCredential(browser: Browser, cookieName: string) {
  const storage = await browser.evaluate<{
    cookie: string;
    local: Record<string, string>;
    session: Record<string, string>;
  }>(() => ({
    cookie: document.cookie,
    local: Object.fromEntries(Object.entries(localStorage)),
    session: Object.fromEntries(Object.entries(sessionStorage)),
  }));
  expect(storage.cookie).toBe("");
  for (const [key, value] of Object.entries({ ...storage.local, ...storage.session })) {
    expect(/token|bearer|authorization|session.?handle/i.test(key)).toBe(false);
    expect(/^Bearer\s/i.test(value)).toBe(false);
    expect(value.split(".").length === 3).toBe(false);
  }
  const cookies = await browser.cookies();
  const sessionCookie = cookies.find(({ name }) => name === cookieName);
  expect(sessionCookie).toBeDefined();
  expect(sessionCookie?.httpOnly).toBe(true);
  expect(sessionCookie?.secure).toBe(true);
  expect(sessionCookie?.sameSite).toBe("Lax");
}

async function assertOneTimeCredentialNotStored(browser: Browser, credential: string) {
  const retained = await browser.evaluate(
    (secret) => ({
      cookie: document.cookie.includes(secret),
      local: Object.values(localStorage).some((value) => value.includes(secret)),
      session: Object.values(sessionStorage).some((value) => value.includes(secret)),
    }),
    credential,
  );
  expect(retained).toEqual({ cookie: false, local: false, session: false });
  expect((await browser.cookies()).some(({ value }) => value.includes(credential))).toBe(false);
}

function e2eRequestId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID()}`;
}

function cliClient(origin: string): CLIIdentityClient {
  const transport: FixtureTransport = async (request, signal) => {
    const headers = new Headers(request.headers);
    headers.set("accept", "application/json");
    if (request.body !== undefined) headers.set("content-type", "application/json");
    const response = await fetch(new URL(request.path, origin), {
      method: request.method,
      headers,
      ...(request.body === undefined ? {} : { body: request.body }),
      redirect: "error",
      signal,
    });
    return {
      status: response.status,
      headers: Object.fromEntries(response.headers.entries()),
      body: await response.text(),
    };
  };
  return new CLIIdentityClient(transport);
}

async function expectClientErrorStatus(operation: Promise<unknown>, status: number) {
  let observed = 0;
  try {
    await operation;
  } catch (cause) {
    if (cause instanceof ClientError) observed = cause.status;
  }
  expect(observed).toBe(status);
}

async function waitForPrivateFile(path: string, maximumBytes: number): Promise<string> {
  for (let attempt = 0; attempt < 150; attempt++) {
    if (existsSync(path)) {
      const info = statSync(path);
      if (!info.isFile() || (info.mode & 0o077) !== 0 || info.size < 1 || info.size > maximumBytes)
        throw new Error("CLI private file boundary was invalid");
      return readFileSync(path, "utf8");
    }
    await delay(100);
  }
  throw new Error("CLI private file was not created");
}

test("a platform administrator signs in, sees every tenant, switches scope, and persists email policy", async ({
  app,
  browser,
  screen,
}) => {
  const proxiedPaths: string[] = [];
  await browser.route("**/v1/**", async (route) => {
    const request = route.request;
    expect(request.headers.authorization).toBeUndefined();
    expect(request.headers["x-cloud-agents-session"]).toBeUndefined();
    proxiedPaths.push(new URL(request.url).pathname);
    await route.continue();
  });
  await app.open();
  await expect(screen.getByRole("heading", "Sign in to your tenants")).toBeVisible();
  const login = await passwordLogin(browser, screen, accounts.platform);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();

  const tenant = screen.getByLabel("Tenant");
  const project = screen.getByLabel("Project");
  expect(await browser.locator('select[aria-label="Tenant"] option').allTextContents()).toEqual([
    "Tenant A",
    "Tenant B",
  ]);
  await expect(tenant).toHaveValue("tenant-a");
  expect(await browser.locator('select[aria-label="Project"] option').allTextContents()).toEqual([
    "Project A One",
    "Project A Two",
  ]);
  await expect(project).toHaveValue("project-a1");
  await assertBrowserHasNoReusableCredential(browser, "__Host-cloud-agents-admin-session");
  await expect(screen.getByLabel("Tenant ID")).toHaveCount(0);
  await expect(screen.getByLabel("Project ID")).toHaveCount(0);

  const tabScopes = await browser.evaluate(async (csrfToken) => {
    const peer = window.open(location.href, "identity-scope-peer");
    if (!peer) throw new Error("Second tab did not open");
    const waitForScope = async (tenantId: string, projectId: string) => {
      const deadline = Date.now() + 10_000;
      while (Date.now() < deadline) {
        const tenant = peer.document.querySelector<HTMLSelectElement>(
          'select[aria-label="Tenant"]',
        );
        const project = peer.document.querySelector<HTMLSelectElement>(
          'select[aria-label="Project"]',
        );
        if (tenant?.value === tenantId && project?.value === projectId) return;
        await new Promise<void>((resolve) => setTimeout(resolve, 25));
      }
      throw new Error("Second tab did not resolve its authorized scope");
    };
    try {
      await waitForScope("tenant-a", "project-a1");
      const selector = peer.document.querySelector<HTMLSelectElement>(
        'select[aria-label="Tenant"]',
      );
      if (!selector) throw new Error("Second tab has no tenant selector");
      selector.value = "tenant-b";
      const change = peer.document.createEvent("Event");
      change.initEvent("change", true, false);
      selector.dispatchEvent(change);
      await waitForScope("tenant-b", "project-b1");
      const readProject = async (page: Window, tenantId: string, projectId: string) => {
        const response = await page.fetch(`/v1/admin/tenants/${tenantId}/projects/${projectId}`, {
          credentials: "same-origin",
          headers: { "x-request-id": crypto.randomUUID(), "x-csrf-token": csrfToken },
          signal: AbortSignal.timeout(10_000),
        });
        return { status: response.status, body: await response.text() };
      };
      const [original, second] = await Promise.all([
        readProject(window, "tenant-a", "project-a1"),
        readProject(peer, "tenant-b", "project-b1"),
      ]);
      return { original, second };
    } finally {
      peer.close();
    }
  }, login.csrfToken);
  expect(tabScopes.original.status).toBe(200);
  expect(tabScopes.second.status).toBe(200);
  expect(parseProject(tabScopes.original.body).value.metadata.uid).toBe("project-a1");
  expect(parseProject(tabScopes.second.body).value.metadata.uid).toBe("project-b1");
  await expect(tenant).toHaveValue("tenant-a");
  await expect(project).toHaveValue("project-a1");

  await project.selectOption({ value: "project-a2" });
  await expect(project).toHaveValue("project-a2");
  await tenant.selectOption({ value: "tenant-b" });
  await expect(tenant).toHaveValue("tenant-b");
  await expect(project).toHaveValue("project-b1");
  expect(proxiedPaths.some((path) => path.includes("/tenant-a/projects/project-a2/"))).toBe(true);
  expect(proxiedPaths.some((path) => path.includes("/tenant-b/projects/project-b1/"))).toBe(true);

  await tenant.selectOption({ value: "tenant-a" });
  await browser.locator("details.profile-menu summary").tap();
  const domains = screen.getByRole("textbox", /^Allowed email domains/);
  await domains.fill("identity.test\nexample.test");
  const saveEmailDomains = screen.getByRole("button", "Save email domains");
  await expect(saveEmailDomains).toBeEnabled();
  const saveResponse = browser.waitForResponse("**/v1/identity/tenants/tenant-a/email-policy");
  await saveEmailDomains.tap();
  expect((await saveResponse).status).toBe(200);
  await browser.reload();
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await expect(screen.getByRole("textbox", /^Allowed email domains/)).toHaveValue(
    "example.test\nidentity.test",
  );
});

test("a tenant administrator sees only the administered tenant", async ({
  app,
  browser,
  screen,
}) => {
  await app.open();
  await passwordLogin(browser, screen, accounts.tenant);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  expect(await browser.locator('select[aria-label="Tenant"] option').allTextContents()).toEqual([
    "Tenant A",
  ]);
  await expect(screen.getByLabel("Tenant")).toHaveValue("tenant-a");
  await assertBrowserHasNoReusableCredential(browser, "__Host-cloud-agents-admin-session");
});

test("an administrator creates, rotates, and permanently disables a service account without retaining its credentials", async ({
  app,
  browser,
  screen,
}) => {
  const displayName = "Identity Automation";
  const automation = cliClient(userURL);
  const useToken = (token: string) =>
    fetch(`${controlPlaneURL}/v1/tenants/tenant-a/projects/project-a1`, {
      headers: {
        accept: "application/json",
        authorization: `Bearer ${token}`,
        "x-request-id": e2eRequestId("service-account-control-plane"),
      },
    });
  await app.open();
  await passwordLogin(browser, screen, accounts.platform);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Service accounts").tap();
  await expect(screen.getByRole("heading", "Service accounts")).toBeVisible();

  await screen.getByLabel("Display name").fill(displayName);
  await screen.getByLabel("Application").selectOption({ value: "user" });
  await screen.getByLabel("Role").selectOption({ value: "project.viewer" });
  await screen.getByLabel("Access scope").selectOption({ label: "Project A One" });
  const createdResponse = browser.waitForResponse("**/v1/admin/tenants/tenant-a/service-accounts");
  const createdList = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/service-accounts?pageSize=200",
  );
  await screen.getByRole("button", "Create service account").tap();
  expect((await createdResponse).status).toBe(201);
  const createdPage = (await (await createdList).json()) as {
    serviceAccounts?: readonly Record<string, unknown>[];
  };
  expect(
    createdPage.serviceAccounts?.every(
      (account) => account.credential === undefined && account.credentialExpiresAt === undefined,
    ),
  ).toBe(true);
  await expect(screen.getByText("Service account created.", { exact: true })).toBeVisible();
  const createdCredential = await browser.evaluate<string>(() => {
    const credential = document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="One-time service-account credential"]',
    )?.value;
    if (credential === undefined || !/^[A-Za-z0-9_-]{43}$/u.test(credential))
      throw new Error("created service-account credential is unavailable");
    return credential;
  });
  await assertOneTimeCredentialNotStored(browser, createdCredential);
  const createdToken = await automation.issueAutomationTenantToken(
    e2eRequestId("service-account-created"),
    createdCredential,
    { tenantId: "tenant-a", projectId: "project-a1" },
    AbortSignal.timeout(15_000),
  );
  expect(createdToken.tokenType).toBe("Bearer");
  expect((await useToken(createdToken.accessToken)).status).toBe(200);
  await screen.getByRole("button", "I saved the credential").tap();
  expect(
    await browser.evaluate(
      (secret) => !document.body.textContent?.includes(secret),
      createdCredential,
    ),
  ).toBe(true);

  const rotatedResponse = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/service-accounts/*:rotate-credential",
  );
  await screen.getByRole("button", `Rotate the credential for ${displayName}`).tap();
  expect((await rotatedResponse).status).toBe(200);
  const rotatedCredential = await browser.evaluate<string>(() => {
    const credential = document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="One-time service-account credential"]',
    )?.value;
    if (credential === undefined || !/^[A-Za-z0-9_-]{43}$/u.test(credential))
      throw new Error("rotated service-account credential is unavailable");
    return credential;
  });
  expect(rotatedCredential === createdCredential).toBe(false);
  await assertOneTimeCredentialNotStored(browser, rotatedCredential);
  await expectClientErrorStatus(
    automation.issueAutomationTenantToken(
      e2eRequestId("service-account-old-credential"),
      createdCredential,
      { tenantId: "tenant-a", projectId: "project-a1" },
      AbortSignal.timeout(15_000),
    ),
    403,
  );
  expect((await useToken(createdToken.accessToken)).status).toBe(401);
  const rotatedToken = await automation.issueAutomationTenantToken(
    e2eRequestId("service-account-rotated"),
    rotatedCredential,
    { tenantId: "tenant-a", projectId: "project-a1" },
    AbortSignal.timeout(15_000),
  );
  expect((await useToken(rotatedToken.accessToken)).status).toBe(200);
  await screen.getByRole("button", "I saved the credential").tap();

  await screen.getByRole("button", `Disable ${displayName} permanently`).tap();
  await expect(
    screen.getByText(
      `Disable ${displayName} permanently? Its credentials and active tokens will stop working immediately.`,
      { exact: true },
    ),
  ).toBeVisible();
  const disabledResponse = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/service-accounts/*:disable",
  );
  await screen.getByRole("button", "Disable permanently", { exact: true }).tap();
  expect((await disabledResponse).status).toBe(204);
  await expect(screen.getByText("Permanently disabled", { exact: true })).toBeVisible();
  await expectClientErrorStatus(
    automation.issueAutomationTenantToken(
      e2eRequestId("service-account-disabled"),
      rotatedCredential,
      { tenantId: "tenant-a", projectId: "project-a1" },
      AbortSignal.timeout(15_000),
    ),
    403,
  );
  expect((await useToken(rotatedToken.accessToken)).status).toBe(401);
  expect(
    await browser.evaluate(
      (secrets) =>
        secrets.every(
          (secret) =>
            !document.body.textContent?.includes(secret) &&
            !Object.values(localStorage).some((value) => value.includes(secret)) &&
            !Object.values(sessionStorage).some((value) => value.includes(secret)),
        ),
      [createdCredential, rotatedCredential],
    ),
  ).toBe(true);
});

test("cloud-agentsctl uses browser approval, a private profile, current tenant access, and logout revocation", async ({
  app,
  browser,
  screen,
}) => {
  const client = cliClient(adminURL);
  let approvalOrigin = "unseen";
  await browser.route("**/v1/auth/cli/approve", async (route) => {
    approvalOrigin = route.request.headers.origin ?? "missing";
    await route.continue();
  });
  await app.open();
  await passwordLogin(browser, screen, accounts.platform);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();

  const verification = new URL(await waitForPrivateFile(cliVerificationURLFile, 2_048));
  expect(verification.origin).toBe(adminURL);
  expect(verification.pathname).toBe("/auth/cli/authorize");
  expect([...verification.searchParams.keys()].sort()).toEqual(["id", "state"]);
  const state = verification.searchParams.get("state");
  expect(state?.length).toBe(43);
  const cliPageResponse = browser.waitForResponse(new RegExp(`^${adminURL}/$`, "u"));
  await browser.evaluate((url) => {
    window.location.assign(url);
    return true;
  }, verification.href);
  expect((await cliPageResponse).headers["referrer-policy"]).toBe("same-origin");
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents CLI")).toBeVisible();
  const approvalResponse = browser.waitForResponse("**/v1/auth/cli/approve");
  await screen.getByRole("button", "Approve CLI login").tap();
  const approval = await approvalResponse;
  if (approval.status !== 303)
    throw new Error(
      `CLI approval failed with ${approval.status} (origin=${approvalOrigin}): ${await approval.text()}`,
    );
  expect(approvalOrigin).toBe(adminURL);
  expect(approval.headers.location?.startsWith("http://127.0.0.1:")).toBe(true);
  expect(approval.headers["content-security-policy"]?.includes("http://127.0.0.1:*")).toBe(true);
  await expect(
    screen.getByText("CLI login completed. You can close this window.", { exact: true }),
  ).toBeVisible();
  expect(await browser.evaluate(() => window.location.pathname + window.location.search)).toBe(
    "/complete",
  );

  const profile = JSON.parse(await waitForPrivateFile(cliProfile, 8_192)) as Record<
    string,
    unknown
  >;
  expect(profile.application).toBe("admin");
  expect(profile.credentialKind).toBe("cliGrant");
  expect(profile.defaultTenantId).toBe("tenant-a");
  expect(profile.defaultProjectId).toBe("project-a1");
  const grantCredential = profile.credential;
  if (typeof grantCredential !== "string" || !/^[A-Za-z0-9_-]{43}$/u.test(grantCredential))
    throw new Error("CLI profile credential was invalid");
  const tenantOutput = execFileSync(cliBinary, ["--profile", cliProfile, "tenant", "get"], {
    encoding: "utf8",
    timeout: 15_000,
  });
  expect(tenantOutput.includes(grantCredential)).toBe(false);
  const tenant = JSON.parse(tenantOutput) as { metadata?: { uid?: string } };
  expect(tenant.metadata?.uid).toBe("tenant-a");

  const logoutOutput = execFileSync(cliBinary, ["profile", "logout", "--profile", cliProfile], {
    encoding: "utf8",
    timeout: 15_000,
  });
  expect(JSON.parse(logoutOutput)).toEqual({ loggedOut: true });
  expect(existsSync(cliProfile)).toBe(false);
  await expectClientErrorStatus(
    client.listTenants(
      e2eRequestId("cli-revoked"),
      grantCredential,
      200,
      undefined,
      AbortSignal.timeout(15_000),
    ),
    401,
  );

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  expect(await browser.evaluate(() => window.location.hash)).toBe("");
  await assertOneTimeCredentialNotStored(browser, grantCredential);
  if (state !== null) await assertOneTimeCredentialNotStored(browser, state);
});

test("a User member sees only authorized projects and its session is rejected by Admin Web", async ({
  browser,
  screen,
}) => {
  await browser.goto(userURL);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await passwordLogin(browser, screen, accounts.member);
  await expect(browser.locator('span.connection-state[role="status"]')).toHaveText(
    "Project Member",
  );
  expect(
    await browser.locator('select[aria-label="Current tenant"] option').allTextContents(),
  ).toEqual(["Tenant A"]);
  expect(
    await browser.locator('select[aria-label="Current project"] option').allTextContents(),
  ).toEqual(["Project A One"]);
  await assertBrowserHasNoReusableCredential(browser, "__Host-cloud-agents-user-session");

  const userSession = (await browser.cookies()).find(
    ({ name }) => name === "__Host-cloud-agents-user-session",
  );
  expect(userSession).toBeDefined();
  await browser.setCookies([
    {
      url: adminURL,
      name: "__Host-cloud-agents-admin-session",
      value: userSession?.value ?? "",
      httpOnly: true,
      secure: true,
      sameSite: "Lax",
    },
  ]);
  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Sign in to your tenants")).toBeVisible();
  const cookies = await browser.cookies();
  expect(cookies.some(({ name }) => name === "__Host-cloud-agents-admin-session")).toBe(false);
});

test("an administrator creates an invitation that is accepted once while a second invitation is revoked", async ({
  app,
  browser,
  screen,
}) => {
  const invitedEmail = "invited-member@identity.test";
  const revokedEmail = "revoked-member@identity.test";

  await app.open();
  await passwordLogin(browser, screen, accounts.platform);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Members and invitations").tap();
  await expect(screen.getByRole("heading", "Members and invitations")).toBeVisible();
  await screen.getByLabel("Email").fill(invitedEmail);
  await expect(screen.getByLabel("Role")).toHaveValue("project.developer");
  await expect(screen.getByLabel("Access scope")).toHaveValue("project-a1");
  await screen.getByLabel("I have verified that this person controls this email address.").tap();
  const createResponse = browser.waitForResponse("**/v1/identity/tenants/tenant-a/invitations");
  await screen.getByRole("button", "Create invitation").tap();
  expect((await createResponse).status).toBe(201);
  await expect(screen.getByText("Invitation created")).toBeVisible();
  const invitationURL = await browser.evaluate<string>(() => {
    const link = document.querySelector<HTMLTextAreaElement>('textarea[readonly][rows="3"]')?.value;
    if (
      link === undefined ||
      !/^https:\/\/admin\.localhost:\d+\/user-console#invitation=[A-Za-z0-9_-]{43}$/u.test(link)
    )
      throw new Error("invitation link is unavailable");
    return link;
  });
  await browser.evaluate((url) => {
    window.location.assign(url);
    return true;
  }, invitationURL);
  await expect(screen.getByRole("heading", "Accept your invitation")).toBeVisible();
  expect(await browser.evaluate(() => window.location.hash)).toBe("");
  await screen.getByLabel("Display name").fill("Invited Member");
  await screen.getByLabel("New password").fill(invitedPassword);
  await screen.getByLabel("Confirm password").fill(invitedPassword);
  const acceptResponse = browser.waitForResponse("**/v1/identity/invitations/accept");
  await screen.getByRole("button", "Accept invitation").tap();
  expect((await acceptResponse).status).toBe(204);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await passwordLogin(browser, screen, invitedEmail, invitedPassword);
  await expect(browser.locator('span.connection-state[role="status"]')).toHaveText(
    "Invited Member",
  );
  expect(
    await browser.locator('select[aria-label="Current project"] option').allTextContents(),
  ).toEqual(["Project A One"]);

  await browser.evaluate((url) => {
    window.location.assign(url);
    return true;
  }, invitationURL);
  await expect(screen.getByRole("heading", "Accept your invitation")).toBeVisible();
  expect(await browser.evaluate(() => window.location.hash)).toBe("");
  const replayButton = screen.getByRole("button", "Accept invitation");
  await expect(replayButton).toBeEnabled();
  const replayResponse = browser.waitForResponse("**/v1/identity/invitations/accept");
  await replayButton.tap();
  expect((await replayResponse).status).toBe(409);
  await expect(screen.getByRole("alert")).toHaveText(
    "This invitation cannot be accepted. It may have expired, been used or revoked, or no longer match your account or the tenant’s policy. Ask the administrator for a new invitation.",
  );

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Members and invitations").tap();
  await expect(screen.getByRole("heading", "Members and invitations")).toBeVisible();
  await screen.getByLabel("Email").fill(revokedEmail);
  await screen.getByLabel("I have verified that this person controls this email address.").tap();
  const secondCreateResponse = browser.waitForResponse(
    "**/v1/identity/tenants/tenant-a/invitations",
  );
  await screen.getByRole("button", "Create invitation").tap();
  expect((await secondCreateResponse).status).toBe(201);
  await expect(screen.getByText(revokedEmail)).toBeVisible();
  const revokeResponse = browser.waitForResponse("**/v1/identity/tenants/tenant-a/invitations/*");
  await screen.getByRole("button", "Revoke").tap();
  expect((await revokeResponse).status).toBe(204);
  await expect(screen.getByText("project.developer · Revoked")).toBeVisible();
});

test("an administrator suspends and restores a member, then adds and removes a bounded role", async ({
  browser,
  screen,
}) => {
  const mutationRequestIDs: Record<"suspend" | "resume" | "bind" | "revoke", string> = {
    suspend: "",
    resume: "",
    bind: "",
    revoke: "",
  };
  const recordRequestID = (
    operation: keyof typeof mutationRequestIDs,
    response: Readonly<{ headers: Readonly<Record<string, string>> }>,
  ) => {
    const requestID = response.headers["x-request-id"] ?? "";
    expect(/^web-[0-9a-f-]{36}$/u.test(requestID)).toBe(true);
    mutationRequestIDs[operation] = requestID;
  };
  const invitedEmail = "invited-member@identity.test";
  const member = "Invited Member — Project A One";

  await browser.goto(userURL);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  const session = await passwordLogin(browser, screen, invitedEmail, invitedPassword);
  await expect(browser.locator('span.connection-state[role="status"]')).toHaveText(
    "Invited Member",
  );

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Sign in to your tenants")).toBeVisible();
  const adminSession = await passwordLogin(browser, screen, accounts.platform);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  const memberAPIs = await browser.evaluate<
    Record<string, Readonly<{ status: number; body: string; resourceVersion: string | null }>>,
    string
  >(async (csrfToken) => {
    const paths = {
      tenant: "/v1/admin/tenants/tenant-a",
      accounts: "/v1/identity/tenants/tenant-a/accounts?pageSize=200",
      memberships: "/v1/admin/tenants/tenant-a/memberships?pageSize=200",
      roleBindings: "/v1/admin/tenants/tenant-a/role-bindings?pageSize=200",
      roles: "/v1/admin/tenants/tenant-a/roles?pageSize=200",
      organizations: "/v1/admin/tenants/tenant-a/organizations?pageSize=200",
    };
    return Object.fromEntries(
      await Promise.all(
        Object.entries(paths).map(async ([name, path]) => {
          const response = await fetch(path, {
            credentials: "same-origin",
            headers: { accept: "application/json", "x-csrf-token": csrfToken },
          });
          return [
            name,
            {
              status: response.status,
              body: await response.text(),
              resourceVersion: response.headers.get("x-resource-version"),
            },
          ] as const;
        }),
      ),
    );
  }, adminSession.csrfToken);
  expect(
    Object.fromEntries(Object.entries(memberAPIs).map(([key, value]) => [key, value.status])),
  ).toEqual({
    tenant: 200,
    accounts: 200,
    memberships: 200,
    roleBindings: 200,
    roles: 200,
    organizations: 200,
  });
  const decodedMemberAPIs = {
    tenant: parsePlatformTenant(memberAPIs.tenant.body),
    accounts: parseIdentityAccountPage(memberAPIs.accounts.body),
    memberships: parseMembershipPage(memberAPIs.memberships.body),
    roleBindings: parseRoleBindingPage(memberAPIs.roleBindings.body),
    roles: parseRolePage(memberAPIs.roles.body),
    organizations: parseOrganizationPage(memberAPIs.organizations.body),
  };
  expect(memberAPIs.tenant.resourceVersion).toBe(
    decodedMemberAPIs.tenant.value.metadata.resourceVersion,
  );
  expect(decodedMemberAPIs.accounts.accounts.some(({ email }) => email === invitedEmail)).toBe(
    true,
  );
  expect({
    tenant:
      decodedMemberAPIs.tenant.value.metadata.uid === "tenant-a" &&
      decodedMemberAPIs.tenant.value.metadata.tenantRef.id === "tenant-a",
    memberships: decodedMemberAPIs.memberships.value.memberships.every(
      ({ metadata }) => metadata.tenantRef.id === "tenant-a",
    ),
    roleBindings: decodedMemberAPIs.roleBindings.value.roleBindings.every(
      ({ metadata }) => metadata.tenantRef.id === "tenant-a",
    ),
    roles: decodedMemberAPIs.roles.value.roles.every(
      ({ metadata }) => metadata.tenantRef.id === "tenant-a",
    ),
    organizations: decodedMemberAPIs.organizations.value.organizations.every(
      ({ metadata }) => metadata.tenantRef.id === "tenant-a",
    ),
  }).toEqual({
    tenant: true,
    memberships: true,
    roleBindings: true,
    roles: true,
    organizations: true,
  });
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Members and invitations").tap();
  await screen.getByRole("tab", "Members").tap();
  await expect(screen.getByRole("heading", "Tenant members")).toBeVisible();
  await expect(screen.getByText(invitedEmail)).toBeVisible();

  const role = screen.getByLabel(`Role for ${member}`);
  const scope = screen.getByLabel(`Access scope for ${member}`);
  const [roleOptions, scopeOptions] = await browser.evaluate<
    readonly [string[], string[]],
    { role: string; scope: string }
  >(
    (labels) => {
      const options = (labelText: string) => {
        const label = [...document.querySelectorAll("label")].find(
          (candidate) => candidate.querySelector(":scope > span")?.textContent === labelText,
        );
        return [...(label?.querySelectorAll("option") ?? [])].map(
          (option) => option.textContent ?? "",
        );
      };
      return [options(labels.role), options(labels.scope)];
    },
    { role: `Role for ${member}`, scope: `Access scope for ${member}` },
  );
  expect(roleOptions).toContain("project.viewer");
  expect(roleOptions).not.toContain("tenant.admin");
  expect(roleOptions).not.toContain("organization.admin");
  expect(scopeOptions).toEqual(["Project A One"]);

  const suspendResponse = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/memberships/*:suspend",
  );
  await screen.getByRole("button", `Suspend ${member}`).tap();
  const suspended = await suspendResponse;
  expect(suspended.status).toBe(200);
  recordRequestID("suspend", suspended);
  await expect(screen.getByText(`Suspended ${member} for this tenant.`)).toBeVisible();

  await browser.goto(userURL);
  const denied = await browser.evaluate<number, string>(async (csrfToken) => {
    const response = await fetch("/v1/tenants/tenant-a/my-projects?pageSize=200", {
      credentials: "same-origin",
      headers: { accept: "application/json", "x-csrf-token": csrfToken },
    });
    return response.status;
  }, session.csrfToken);
  expect(denied).toBe(403);
  await expect(screen.getByRole("heading", "No tenants are available")).toBeVisible();

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Members and invitations").tap();
  await screen.getByRole("tab", "Members").tap();
  const resumeResponse = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/memberships/*:resume",
  );
  await screen.getByRole("button", `Resume ${member}`).tap();
  const resumed = await resumeResponse;
  expect(resumed.status).toBe(200);
  recordRequestID("resume", resumed);
  await expect(screen.getByText(`Restored ${member} for this tenant.`)).toBeVisible();

  await role.selectOption({ value: "project.viewer" });
  await scope.selectOption({ label: "Project A One" });
  const bindResponse = browser.waitForResponse("**/v1/admin/tenants/tenant-a/role-bindings");
  await screen.getByRole("button", `Add role for ${member}`).tap();
  const bound = await bindResponse;
  expect(bound.status).toBe(201);
  recordRequestID("bind", bound);
  await expect(screen.getByText(`Added a role for ${member}.`)).toBeVisible();

  const revokeResponse = browser.waitForResponse(
    "**/v1/admin/tenants/tenant-a/role-bindings/*:revoke",
  );
  await screen.getByRole("button", `Remove project.viewer from ${member}`).tap();
  const revoked = await revokeResponse;
  expect(revoked.status).toBe(200);
  recordRequestID("revoke", revoked);
  await expect(screen.getByText(`Removed a role from ${member}.`)).toBeVisible();

  await screen.getByRole("button", "Close").tap();
  await screen.getByLabel("Tenant").selectOption({ value: "tenant-b" });
  await browser.locator("details.profile-menu summary").tap();
  await screen.getByRole("button", "Members and invitations").tap();
  await screen.getByRole("tab", "Members").tap();
  await expect(screen.getByRole("heading", "Tenant members")).toBeVisible();
  await expect(screen.getByText(invitedEmail)).toHaveCount(0);

  await browser.goto(userURL);
  await expect(browser.locator('span.connection-state[role="status"]')).toHaveText(
    "Invited Member",
  );
  expect(
    await browser.locator('select[aria-label="Current project"] option').allTextContents(),
  ).toEqual(["Project A One"]);
  writeFileSync(memberRequestIDsFile, JSON.stringify(mutationRequestIDs), {
    encoding: "utf8",
    mode: 0o600,
  });
});
