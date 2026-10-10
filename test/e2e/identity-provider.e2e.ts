import { chmodSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { test, type Browser } from "@e2e-dev/web";
import { expect, type Screen } from "e2e";

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === "") throw new Error(`${name} is required`);
  return value;
}

const adminURL = required("CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL");
const userURL = required("CLOUD_AGENTS_IDENTITY_E2E_USER_URL");
const issuer = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ISSUER");
const providerId = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ID");
const providerDisplayName = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_DISPLAY_NAME");
const providerKind = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_KIND");
const providerAdminClientId = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ADMIN_CLIENT_ID");
const providerUserClientId = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_USER_CLIENT_ID");
const providerAdminSecretRef = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ADMIN_SECRET_REF");
const providerUserSecretRef = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_USER_SECRET_REF");
const providerRootCARef = process.env.CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ROOT_CA_REF ?? "";
const providerOrganizationId = process.env.CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ORGANIZATION_ID ?? "";
const password = required("CLOUD_AGENTS_IDENTITY_E2E_PASSWORD");
const providerPassword = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_PASSWORD");
const enabledPassword = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_ENABLED_PASSWORD");
const providerEmail = required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_EMAIL");
const humanAuthorization = providerKind === "feishu" || providerKind === "github";
const humanMarker = humanAuthorization
  ? required("CLOUD_AGENTS_IDENTITY_E2E_PROVIDER_HUMAN_MARKER_FILE")
  : "";
const humanAuthorizationTimeout = 480_000;
const githubCredentialPattern =
  /\b(?:gh[opurs]_[A-Za-z0-9]{20,255}|github_pat_[A-Za-z0-9_]{20,255})\b/u;
const githubCredentialFixture = `gho_${"a".repeat(24)}`;
if (
  !githubCredentialPattern.test(githubCredentialFixture) ||
  !githubCredentialPattern.test(JSON.stringify({ access_token: githubCredentialFixture })) ||
  githubCredentialPattern.test("github_pat_short")
)
  throw new Error("GitHub credential scanner self-check failed");

function providerEmailDomain(): string {
  const separator = providerEmail.lastIndexOf("@");
  if (separator < 1 || separator === providerEmail.length - 1)
    throw new Error("provider email domain is invalid");
  const domain = providerEmail.slice(separator + 1);
  if (
    !/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/u.test(domain)
  )
    throw new Error("provider email domain is invalid");
  return domain;
}

async function passwordLogin(browser: Browser, screen: Screen, email: string, credential: string) {
  await screen.getByLabel("Email").fill(email);
  await screen.getByLabel("Password").fill(credential);
  const response = browser.waitForResponse("**/v1/identity/login/password");
  await screen.getByRole("button", "Sign in", { exact: true }).tap();
  expect((await response).status).toBe(200);
}

type HumanCallback = Readonly<{
  response: ReturnType<Browser["waitForResponse"]>;
  deadline: string;
}>;

function prepareHumanCallback(browser: Browser): HumanCallback | undefined {
  if (!humanAuthorization) return undefined;
  return Object.freeze({
    response: browser.waitForResponse("**/auth/provider/callback*", {
      timeout: humanAuthorizationTimeout,
    }),
    deadline: new Date(Date.now() + humanAuthorizationTimeout).toISOString(),
  });
}

async function providerLogin(browser: Browser, callback: HumanCallback | undefined) {
  if (humanAuthorization) {
    await completeHumanAuthorization(browser, "invitation", userURL, callback);
    return;
  }
  if (providerKind === "gitlab") {
    await browser.locator("#user_login").fill(providerEmail);
    await browser.locator("#user_password").fill(providerPassword);
    await browser.locator('[data-testid="sign-in-button"]').tap();
    return;
  }
  await browser.locator("#username").fill(providerEmail);
  await browser.locator("#password").fill(providerPassword);
  await browser.locator("#kc-login").tap();
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

async function completeHumanAuthorization(
  browser: Browser,
  stage: "invitation" | "reauthentication" | "link" | "login" | "rejected-login",
  expectedOrigin: string,
  callback: HumanCallback | undefined,
) {
  if (!humanAuthorization) return;
  if (callback === undefined) throw new Error(`PROVIDER_STAGE stage=${stage}`);
  const expectedOutcome = {
    invitation: "signed-in",
    reauthentication: "reauthenticated",
    link: "linked",
    login: "signed-in",
    "rejected-login": "failed",
  }[stage];
  process.stdout.write(`PROVIDER_STAGE provider=${providerKind} stage=${stage}\n`);
  const temporary = `${humanMarker}.tmp`;
  writeFileSync(
    temporary,
    JSON.stringify({
      status: "WAITING_FOR_HUMAN_AUTHORIZATION",
      stage,
      deadline: callback.deadline,
      callbackOrigins: [adminURL, userURL],
    }),
    { mode: 0o600 },
  );
  chmodSync(temporary, 0o600);
  renameSync(temporary, humanMarker);
  try {
    const response = await callback.response;
    const callbackURL = new URL(response.url);
    process.stdout.write(
      `PROVIDER_CALLBACK_REQUEST provider=${providerKind} stage=${stage} has_code=${callbackURL.searchParams.has("code")} has_state=${callbackURL.searchParams.has("state")} has_error=${callbackURL.searchParams.has("error")} has_authCode=${callbackURL.searchParams.has("authCode")} has_iss=${callbackURL.searchParams.has("iss")} returned_to_expected_origin=${callbackURL.origin === expectedOrigin}\n`,
    );
    const outcome = /^\/#identity=(signed-in|reauthenticated|linked|failed)$/u.exec(
      response.headers.location ?? "",
    )?.[1];
    if (outcome !== undefined)
      process.stdout.write(
        `PROVIDER_CALLBACK provider=${providerKind} stage=${stage} outcome=${outcome}\n`,
      );
    expect(response.status).toBe(303);
    expect(outcome).toBe(expectedOutcome);
    await browser.waitForURL(
      new RegExp(`^${escapeRegExp(expectedOrigin)}/(?:#identity=${expectedOutcome})?$`, "u"),
      { timeout: 10_000 },
    );
  } finally {
    rmSync(humanMarker, { force: true });
    rmSync(temporary, { force: true });
  }
}

async function clearApplicationSession(browser: Browser, origin: string) {
  await browser.goto(origin);
  await browser.evaluate(async () => {
    const response = await fetch("/v1/identity/session", {
      credentials: "same-origin",
      headers: { accept: "application/json" },
    });
    if (!response.ok) return false;
    const session = (await response.json()) as { csrfToken?: unknown };
    if (typeof session.csrfToken !== "string") return false;
    await fetch("/v1/identity/session", {
      method: "DELETE",
      credentials: "same-origin",
      headers: { "x-csrf-token": session.csrfToken },
    });
    return true;
  });
}

async function assertProviderScopeRendering(screen: Screen) {
  const kind = screen.getByLabel("Provider kind");
  const scopes = screen.getByLabel(/^Scopes/u);
  const fixedCases = [
    ["github", "read:user\nuser:email"],
    ["feishu", "contact:user.email:readonly"],
    ["dingtalk", "corpid\nopenid"],
    ["wecom", ""],
  ] as const;
  for (const [fixedKind, expectedScopes] of fixedCases) {
    await kind.selectOption({ value: fixedKind });
    await expect(scopes).toHaveValue(expectedScopes);
    await expect(scopes).toHaveAttribute("readonly");
  }
  for (const editableKind of ["oidc", "gitlab"] as const) {
    await kind.selectOption({ value: "wecom" });
    await kind.selectOption({ value: editableKind });
    await expect(scopes).toHaveValue("openid\nprofile\nemail");
    expect(await scopes.getAttribute("readonly")).toBeNull();
    await scopes.fill("openid\noffline_access");
    await expect(scopes).toHaveValue("openid\noffline_access");
  }
  await kind.selectOption({ value: "wecom" });
  await kind.selectOption({ value: providerKind });
  const restoredFixedScopes = fixedCases.find(([fixedKind]) => fixedKind === providerKind)?.[1];
  await expect(scopes).toHaveValue(restoredFixedScopes ?? "openid\nprofile\nemail");
  if (restoredFixedScopes === undefined) expect(await scopes.getAttribute("readonly")).toBeNull();
  else await expect(scopes).toHaveAttribute("readonly");
}

async function configureProvider(
  browser: Browser,
  screen: Screen,
  application: "admin" | "user",
  clientId: string,
  secretRef: string,
) {
  await screen.getByLabel("Provider configuration").selectOption({ label: "New provider client" });
  await screen.getByLabel("Application").selectOption({ value: application });
  await screen.getByLabel("Display name").fill(providerDisplayName);
  if (application === "admin") await assertProviderScopeRendering(screen);
  else await screen.getByLabel("Provider kind").selectOption({ value: providerKind });
  await screen.getByLabel("Issuer").fill(issuer);
  await screen.getByLabel("Client ID").fill(clientId);
  if (application === "user") await screen.getByLabel("User Web origin").fill(userURL);
  await screen.getByLabel("Client secret reference").fill(secretRef);
  await screen.getByLabel("Root CA reference (optional)").fill(providerRootCARef);
  if (providerKind === "feishu" || providerKind === "github") {
    const scopes = screen.getByLabel(/^Scopes/u);
    await expect(scopes).toHaveValue(
      providerKind === "feishu" ? "contact:user.email:readonly" : "read:user\nuser:email",
    );
    await expect(scopes).toHaveAttribute("readonly");
  }
  if (providerOrganizationId !== "") {
    await screen.getByLabel("Treat this provider's email assertion as verified").tap();
    await screen.getByLabel(/^Allowed organization IDs/u).fill(providerOrganizationId);
  }
  const save = browser.waitForResponse(
    `**/v1/identity/providers/${providerId}/applications/${application}`,
  );
  await screen.getByRole("button", "Save provider").tap();
  expect((await save).status).toBe(200);
  await expect(screen.getByText("Provider configuration saved.", { exact: true })).toBeVisible();
}

async function allowExternalProviderEmailDomain(browser: Browser, screen: Screen) {
  const expected = [...new Set(["example.test", "identity.test", providerEmailDomain()])]
    .sort()
    .join("\n");
  const domains = screen.getByRole("textbox", /^Allowed email domains/);
  await expect(domains).toHaveValue("example.test\nidentity.test");
  await domains.fill(expected);
  await expect(screen.getByRole("button", "Save email domains")).toBeEnabled();
  const save = browser.waitForResponse("**/v1/identity/tenants/tenant-a/email-policy");
  await screen.getByRole("button", "Save email domains").tap();
  expect((await save).status).toBe(200);
  await expect(domains).toHaveValue(expected);
  await browser.reload();
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  await expect(screen.getByRole("textbox", /^Allowed email domains/)).toHaveValue(expected);
}

async function createProviderInvitation(browser: Browser): Promise<string> {
  return browser.evaluate<string, string>(async (email) => {
    const sessionResponse = await fetch("/v1/identity/session", {
      credentials: "same-origin",
      headers: { accept: "application/json" },
    });
    if (!sessionResponse.ok) throw new Error("provider invitation session unavailable");
    const session = (await sessionResponse.json()) as { csrfToken?: unknown };
    if (typeof session.csrfToken !== "string")
      throw new Error("provider invitation CSRF unavailable");
    const response = await fetch("/v1/identity/tenants/tenant-a/invitations", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        accept: "application/json",
        "content-type": "application/json",
        "x-csrf-token": session.csrfToken,
      },
      body: JSON.stringify({
        email,
        roleName: "project.viewer",
        scopeLevel: "project",
        scopeId: "project-a1",
        verification: "provider-required",
      }),
    });
    if (response.status !== 201) throw new Error("provider invitation was not created");
    const created = (await response.json()) as { invitationCode?: unknown };
    if (
      typeof created.invitationCode !== "string" ||
      !/^[A-Za-z0-9_-]{43}$/u.test(created.invitationCode)
    )
      throw new Error("provider invitation proof unavailable");
    return created.invitationCode;
  }, providerEmail);
}

async function openLinkedLogins(browser: Browser, screen: Screen) {
  const accountOpen = await browser.evaluate(
    () => document.querySelector("#account-title") !== null,
  );
  if (!accountOpen) await screen.getByRole("button", "Account").tap();
  const linkedOpen = await browser.evaluate(
    () => document.querySelector('section[aria-labelledby="linked-logins-title"]') !== null,
  );
  if (!linkedOpen) await browser.locator("main details > summary").tap();
  await expect(screen.getByRole("heading", "Linked sign-ins")).toBeVisible();
}

async function passwordReauthenticate(browser: Browser, screen: Screen, credential: string) {
  const before = (await browser.cookies()).find(
    ({ name }) => name === "__Host-cloud-agents-user-session",
  );
  expect(before).toBeDefined();
  await browser.locator("#linked-login-current-password").fill(credential);
  const response = browser.waitForResponse("**/v1/identity/me/reauthenticate/password");
  await screen.getByRole("button", "Reauthenticate", { exact: true }).tap();
  expect((await response).status).toBe(200);
  await expect(
    screen.getByText("Reauthentication complete. Choose one account change below."),
  ).toBeVisible();

  const rotatedCookies = (await browser.cookies()).filter(({ name }) =>
    ["__Host-cloud-agents-user-session", "__Host-cloud-agents-user-reauth"].includes(name),
  );
  const after = rotatedCookies.find(({ name }) => name === "__Host-cloud-agents-user-session");
  expect(after).toBeDefined();
  expect(after?.value === before?.value).toBe(false);

  await browser.setCookies([
    {
      url: userURL,
      name: "__Host-cloud-agents-user-session",
      value: before?.value ?? "",
      httpOnly: true,
      secure: true,
      sameSite: "Lax",
    },
  ]);
  const oldSessionStatus = await browser.evaluate<number>(async () =>
    fetch("/v1/identity/session", {
      credentials: "same-origin",
      headers: { accept: "application/json" },
    }).then(({ status }) => status),
  );
  expect(oldSessionStatus).toBe(401);
  await browser.setCookies(
    rotatedCookies.map(({ name, value, httpOnly, secure, sameSite }) => ({
      url: userURL,
      name,
      value,
      ...(httpOnly === undefined ? {} : { httpOnly }),
      ...(secure === undefined ? {} : { secure }),
      ...(sameSite === undefined ? {} : { sameSite }),
    })),
  );
  await expect(
    screen.getByText("Reauthentication complete. Choose one account change below."),
  ).toBeVisible();
}

async function assertNoProviderSecrets(browser: Browser) {
  const state = await browser.evaluate(() => ({
    hash: window.location.hash,
    cookie: document.cookie,
    local: Object.fromEntries(Object.entries(localStorage)),
    session: Object.fromEntries(Object.entries(sessionStorage)),
  }));
  expect(state.hash).toBe("");
  expect(state.cookie).toBe("");
  for (const [key, value] of Object.entries({ ...state.local, ...state.session })) {
    expect(/token|proof|oauth|state|authorization|session.?handle/i.test(key)).toBe(false);
    expect(/Bearer\s|^[A-Za-z0-9_-]{43}$|^[^.]+\.[^.]+\.[^.]+$/u.test(value)).toBe(false);
    expect(githubCredentialPattern.test(value)).toBe(false);
  }
}

test(`${providerDisplayName} invitation, login, reauthentication, linking and unlinking keep credentials server-side`, async ({
  browser,
  screen,
}) => {
  await clearApplicationSession(browser, userURL);
  await clearApplicationSession(browser, adminURL);
  await browser.goto(adminURL);
  await passwordLogin(browser, screen, "platform-admin@identity.test", password);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await browser.locator("details.profile-menu summary").tap();
  if (humanAuthorization) await allowExternalProviderEmailDomain(browser, screen);
  await screen.getByRole("button", "Login providers").tap();
  await configureProvider(browser, screen, "admin", providerAdminClientId, providerAdminSecretRef);
  await configureProvider(browser, screen, "user", providerUserClientId, providerUserSecretRef);
  const invitationCode = await createProviderInvitation(browser);

  await browser.evaluate((url) => {
    window.location.assign(url);
    return true;
  }, `${userURL}/#invitation=${invitationCode}`);
  await expect(screen.getByRole("heading", "Accept your invitation")).toBeVisible();
  expect(await browser.evaluate(() => window.location.hash)).toBe("");
  await screen.getByLabel("Display name").fill("OIDC Member");
  const invitationCallback = prepareHumanCallback(browser);
  const invitationStart = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Continue with ${providerDisplayName}`).tap();
  expect((await invitationStart).status).toBe(200);
  await providerLogin(browser, invitationCallback);
  await expect(screen.getByText("OIDC Member", { exact: true })).toBeVisible();
  await assertNoProviderSecrets(browser);

  await openLinkedLogins(browser, screen);
  const reauthCallback = prepareHumanCallback(browser);
  const reauthStart = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Reauthenticate with ${providerDisplayName}`).tap();
  expect((await reauthStart).status).toBe(200);
  await completeHumanAuthorization(browser, "reauthentication", userURL, reauthCallback);
  await expect(
    screen.getByText("Reauthentication complete. Choose one account change below."),
  ).toBeVisible();
  await browser.locator("#linked-login-new-password").fill(enabledPassword);
  await browser.locator("#linked-login-confirm-password").fill(enabledPassword);
  const enabled = browser.waitForResponse("**/v1/identity/me/password");
  await screen.getByRole("button", "Enable password").tap();
  expect((await enabled).status).toBe(204);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();

  await passwordLogin(browser, screen, providerEmail, enabledPassword);
  await openLinkedLogins(browser, screen);
  await expect(screen.getByRole("button", `Unlink ${providerDisplayName}`)).toBeDisabled();
  await passwordReauthenticate(browser, screen, enabledPassword);
  const unlinked = browser.waitForResponse("**/v1/identity/me/login-methods/*");
  await screen.getByRole("button", `Unlink ${providerDisplayName}`).tap();
  expect((await unlinked).status).toBe(204);
  await expect(screen.getByText("The sign-in method was removed.", { exact: true })).toBeVisible();

  await passwordReauthenticate(browser, screen, enabledPassword);
  const linkCallback = prepareHumanCallback(browser);
  const linkStart = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Link ${providerDisplayName}`).tap();
  expect((await linkStart).status).toBe(200);
  await completeHumanAuthorization(browser, "link", userURL, linkCallback);
  await expect(screen.getByText("The sign-in method was linked.", { exact: true })).toBeVisible();
  await screen.getByRole("button", "Sign out").tap();

  const loginCallback = prepareHumanCallback(browser);
  const loginStart = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Continue with ${providerDisplayName}`).tap();
  expect((await loginStart).status).toBe(200);
  await completeHumanAuthorization(browser, "login", userURL, loginCallback);
  await expect(screen.getByText("OIDC Member", { exact: true })).toBeVisible();
  await openLinkedLogins(browser, screen);
  const providerReauthCallback = prepareHumanCallback(browser);
  const providerReauth = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Reauthenticate with ${providerDisplayName}`).tap();
  expect((await providerReauth).status).toBe(200);
  await completeHumanAuthorization(browser, "reauthentication", userURL, providerReauthCallback);
  await expect(
    screen.getByText("Reauthentication complete. Choose one account change below."),
  ).toBeVisible();
  const finalUnlink = browser.waitForResponse("**/v1/identity/me/login-methods/*");
  await screen.getByRole("button", `Unlink ${providerDisplayName}`).tap();
  expect((await finalUnlink).status).toBe(204);
  await screen.getByRole("button", "Sign out").tap();

  const rejectedCallback = prepareHumanCallback(browser);
  const rejectedStart = browser.waitForResponse("**/v1/identity/login/provider/start");
  await screen.getByRole("button", `Continue with ${providerDisplayName}`).tap();
  expect((await rejectedStart).status).toBe(200);
  await completeHumanAuthorization(browser, "rejected-login", userURL, rejectedCallback);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await expect(
    screen.getByText("External sign-in could not be completed. Try again.", { exact: true }),
  ).toBeVisible();
  await assertNoProviderSecrets(browser);
});
