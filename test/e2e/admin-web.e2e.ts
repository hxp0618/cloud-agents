import { test } from "@e2e-dev/web";
import { expect } from "e2e";
import { adminScopePage, mockAdminSession } from "./admin-session-fixture";

test("disconnected access preserves the selected language after reload", async ({
  app,
  browser,
  screen,
}) => {
  await mockAdminSession(browser);
  await app.open();
  await expect(screen.getByRole("heading", "Sign in to your tenants")).toBeVisible();
  await expect(screen.getByRole("button", "Sign in")).toBeVisible();
  await screen.getByLabel("Language").selectOption({ value: "zh-CN" });
  await expect(screen.getByRole("heading", "登录并管理你的租户")).toBeVisible();
  expect(
    await browser.evaluate(() => ({
      language: document.documentElement.lang,
      saved: localStorage.getItem("cloud-agents-admin-locale"),
    })),
  ).toEqual({ language: "zh-CN", saved: "zh-CN" });
  await browser.reload();
  await expect(screen.getByRole("heading", "登录并管理你的租户")).toBeVisible();
  await expect(screen.getByRole("button", "登录")).toBeVisible();
});

test("a quota audit failure stays on its page without blocking connection or target refresh", async ({
  app,
  browser,
  screen,
}) => {
  const requests: string[] = [];
  await mockAdminSession(browser);
  const pages: Record<string, readonly [string, string]> = {
    "deployment-targets": ["DeploymentTargetPage", "deploymentTargets"],
    "environment-leases": ["EnvironmentLeasePage", "environmentLeases"],
    workers: ["WorkerPage", "workers"],
    "maintenance-operations": ["MaintenanceOperationPage", "operations"],
  };
  await browser.route("**/v1/admin/**", async (route) => {
    const path = new URL(route.request.url).pathname;
    requests.push(path);
    const scope = adminScopePage(path);
    if (scope !== undefined) return route.fulfill({ json: scope });
    const page = pages[path.split("/").at(-1) ?? ""];
    if (page) {
      await route.fulfill({
        json: {
          apiVersion: "platform.cloud-agents.dev/v1alpha1",
          kind: page[0],
          [page[1]]: [],
        },
      });
    } else {
      await route.fulfill({
        status: path.endsWith("/lease-quota") ? 404 : 503,
        body: "Test API unavailable",
      });
    }
  });
  await app.open();
  await screen.getByLabel("Email").fill("admin@example.test");
  await screen.getByLabel("Password", { exact: true }).fill("fixture password value");
  await screen.getByRole("button", "Sign in").click();
  await expect(screen.getByRole("heading", "Operations overview")).toBeVisible();
  expect(requests.some((path) => path.includes("/lease-quota"))).toBe(false);

  await screen.getByRole("button", "Quotas").click();
  await expect(screen.getByRole("heading", "Project quotas")).toBeVisible();
  await expect(screen.getByRole("alert")).toBeVisible();
  expect(requests.some((path) => path.endsWith("/lease-quota/audit-events"))).toBe(true);

  await screen.getByRole("button", /^Deployment Targets(?: \d+)?$/).click();
  await expect(screen.getByRole("heading", "Deployment targets")).toBeVisible();
  await expect(screen.getByRole("button", "Refresh")).toBeEnabled();
  const beforeRefresh = requests.length;
  await screen.getByRole("button", "Refresh").click();
  await expect(screen.getByRole("button", "Refresh")).toBeEnabled();
  expect(requests.slice(beforeRefresh).some((path) => path.endsWith("/deployment-targets"))).toBe(
    true,
  );
  expect(requests.slice(beforeRefresh).some((path) => path.includes("/lease-quota"))).toBe(false);
});
