import { test, type Browser } from "@e2e-dev/web";
import { expect, type Screen } from "e2e";
import { openAccountMenu } from "./admin-account-menu";

function required(name: string) {
  const value = process.env[name];
  if (value === undefined || value === "") throw new Error(`${name} is required`);
  return value;
}

const adminURL = required("CLOUD_AGENTS_IDENTITY_E2E_ADMIN_URL");
const userURL = required("CLOUD_AGENTS_IDENTITY_E2E_USER_URL");
const password = required("CLOUD_AGENTS_IDENTITY_E2E_PASSWORD");
const email = "account-security@identity.test";

async function login(
  browser: Browser,
  screen: Screen,
  accountEmail: string,
  credential: string,
  status = 200,
) {
  await screen.getByLabel("Email").fill(accountEmail);
  await screen.getByLabel("Password").fill(credential);
  const response = browser.waitForResponse("**/v1/identity/login/password");
  await screen.getByRole("button", "Sign in").tap();
  expect((await response).status).toBe(status);
}

test("account password change, admin reset, and disable revoke existing sessions", async ({
  browser,
  screen,
}) => {
  const changedPassword = `${password} changed`;
  const resetPassword = `${password} reset`;
  await browser.goto(userURL);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await login(browser, screen, email, password);
  await screen.getByRole("button", "Account").tap();
  await expect(screen.getByRole("heading", "Your account")).toBeVisible();
  await screen.getByLabel("Current password").fill(password);
  await screen.getByLabel("New password").fill(changedPassword);
  await screen.getByLabel("Confirm password").fill(changedPassword);
  const change = browser.waitForResponse("**/v1/identity/me/password");
  await screen.getByRole("button", "Save password").tap();
  expect((await change).status).toBe(204);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await login(browser, screen, email, password, 401);
  await login(browser, screen, email, changedPassword);
  await expect(screen.getByRole("status")).toHaveText("Account Security");

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Sign in to your tenants")).toBeVisible();
  await login(browser, screen, "platform-admin@identity.test", password);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await openAccountMenu(browser);
  await screen.getByRole("button", "Account").tap();
  await expect(screen.getByRole("heading", "Platform accounts")).toBeVisible();
  const resetButton = screen.getByRole("button", `Create reset link · ${email}`);
  await expect(resetButton).toBeEnabled();
  const resetIssued = browser.waitForResponse(
    "**/v1/identity/accounts/account-security/password-reset",
  );
  await resetButton.tap();
  expect((await resetIssued).status).toBe(201);
  await expect(screen.getByLabel("Password reset link")).toBeVisible();
  const resetURL = await browser.evaluate<string>(() => {
    const link = document.querySelector<HTMLTextAreaElement>('textarea[readonly][rows="3"]')?.value;
    if (
      link === undefined ||
      !/^https:\/\/admin\.localhost:\d+\/user-console#password-reset=[A-Za-z0-9_-]{43}$/u.test(link)
    )
      throw new Error("reset link is unavailable");
    return link;
  });
  await browser.evaluate((url) => {
    window.location.assign(url);
    return true;
  }, resetURL);
  await expect(screen.getByRole("heading", "Reset your password")).toBeVisible();
  expect(await browser.evaluate(() => window.location.hash)).toBe("");
  await screen.getByLabel("New password").fill(resetPassword);
  await screen.getByLabel("Confirm password").fill(resetPassword);
  const resetAccepted = browser.waitForResponse("**/v1/identity/password-resets/accept");
  await screen.getByRole("button", "Save password").tap();
  expect((await resetAccepted).status).toBe(204);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await login(browser, screen, email, resetPassword);
  await expect(screen.getByRole("status")).toHaveText("Account Security");
  const safeStorage = await browser.evaluate(() =>
    Object.entries({ ...localStorage, ...sessionStorage }).every(
      ([key, value]) =>
        !/password|token|proof|bearer|session.?handle/i.test(key) &&
        !/^[A-Za-z0-9_-]{43}$/u.test(value),
    ),
  );
  expect(safeStorage).toBe(true);

  await browser.goto(adminURL);
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  await openAccountMenu(browser);
  await screen.getByRole("button", "Account").tap();
  const disable = screen.getByRole("button", `Disable account · ${email}`);
  await expect(disable).toBeEnabled();
  await disable.tap();
  const disabled = browser.waitForResponse("**/v1/identity/accounts/account-security/disable");
  await screen.getByRole("button", "Disable account", { exact: true }).tap();
  expect((await disabled).status).toBe(204);
  await expect(browser.locator('[data-account-id="account-security"]')).toContainText("Disabled");
  await expect(screen.getByRole("heading", "Identity audit")).toBeVisible();
  await expect(screen.getByText("account disabled", { exact: true })).toBeVisible();

  const endedSession = browser.waitForResponse("**/v1/identity/session");
  await browser.goto(userURL);
  expect((await endedSession).status).toBe(401);
  await expect(screen.getByRole("heading", "Sign in to Cloud Agents")).toBeVisible();
  await login(browser, screen, email, resetPassword, 401);
});
