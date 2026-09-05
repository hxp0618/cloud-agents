import assert from "node:assert/strict";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? "playwright");
// Read-only browser check while TestWorkerHealthPostgresAndProcess owns its disposable Lease.
const [root, adminTokenFile, userTokenFile, project] = process.argv.slice(2);
if (!root || !adminTokenFile || !userTokenFile || !project)
  throw new Error(
    "usage: capture-worker-health.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_ID",
  );
const origin = "http://127.0.0.1:4174";
const token = readFileSync(adminTokenFile, "utf8").trim();
const apiPath = `/v1/admin/tenants/tenant-local/projects/${encodeURIComponent(project)}/workers`;
const denied = await fetch(new URL(apiPath, origin), {
  headers: {
    Authorization: `Bearer ${readFileSync(userTokenFile, "utf8").trim()}`,
    "X-Request-ID": "worker-health-denied",
  },
});
assert.equal(denied.status, 403);
mkdirSync(`${root}/browser`, { recursive: true });
const browser = await chromium.launch({
  executablePath:
    process.env.BROWSER_PATH ?? "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  headless: true,
});
const errors = [],
  failures = [],
  requests = [],
  checks = [];
try {
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    locale: "en-US",
    reducedMotion: "reduce",
  });
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("response", (r) => {
    if (r.status() >= 400) failures.push({ status: r.status(), path: new URL(r.url()).pathname });
  });
  page.on("request", (r) => {
    if (new URL(r.url()).pathname.startsWith("/v1/"))
      requests.push({ method: r.method(), origin: new URL(r.url()).origin });
  });
  await page.goto(origin);
  await page.locator(".connect-form").waitFor();
  for (const [i, value] of [origin, "tenant-local", project, token].entries())
    await page.locator(".connect-form input").nth(i).fill(value);
  await page.locator(".connect-form button[type=submit]").click();
  await page.locator(".app-shell").waitFor();
  for (const locale of ["en-US", "zh-CN"])
    for (const theme of ["light", "dark"])
      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
        await page.locator(".profile-menu summary").click();
        await page.locator(".locale-picker select").selectOption(locale);
        await page.locator(".profile-menu summary").click();
        await page.evaluate((t) => (document.documentElement.dataset.theme = t), theme);
        if (width === 390) await page.locator(".mobile-nav-trigger").click();
        await page.locator(".sidebar [data-page=overview]").click();
        const summary = page.locator(".worker-state-summary");
        await summary.waitFor();
        assert.equal(await summary.locator("button").count(), 5);
        await summary.scrollIntoViewIfNeeded();
        assert(
          await summary
            .locator("button")
            .evaluateAll((buttons) =>
              buttons.every((button) => button.scrollWidth <= button.clientWidth),
            ),
        );
        assert(
          await page.evaluate(
            () => document.documentElement.scrollWidth === document.documentElement.clientWidth,
          ),
        );
        await page.screenshot({ path: `${root}/browser/${locale}-${theme}-${width}-overview.png` });
        await summary.locator("[data-worker-state=not-observed]").click();
        assert.equal(await page.locator(".worker-table").count(), 0);
        await page
          .getByRole("combobox", {
            name: locale === "zh-CN" ? "筛选工作节点健康状态" : "Filter Worker health",
            exact: true,
          })
          .selectOption("");
        const trigger = page
          .locator(".worker-table tbody tr")
          .filter({ hasText: "lease-alpha" })
          .getByRole("button")
          .first();
        await trigger.focus();
        await page.keyboard.press("Enter");
        const sheet = page.locator("dialog[open]");
        await sheet.waitFor();
        const text = await sheet.innerText();
        assert(text.includes(locale === "zh-CN" ? "观测过期时间" : "Observation expires"));
        assert(text.includes(locale === "zh-CN" ? "配置上限" : "configured limits"));
        assert.equal(await page.locator("vite-error-overlay").count(), 0);
        assert(
          await page.evaluate(
            () => document.documentElement.scrollWidth === document.documentElement.clientWidth,
          ),
        );
        await page.screenshot({ path: `${root}/browser/${locale}-${theme}-${width}-worker.png` });
        const healthStateText = await sheet.locator(".detail-list .phase").first().innerText();
        await page.keyboard.press("Escape");
        assert(await trigger.evaluate((e) => document.activeElement === e));
        checks.push({
          locale,
          theme,
          width,
          healthStateText,
          summaryFilter: true,
          observedHealthDates: true,
          keyboardFocusRestored: true,
        });
      }
  assert.equal(errors.length, 0);
  assert(requests.every((r) => r.method === "GET" && r.origin === origin));
  assert(
    failures.every((f) => f.status === 404 && /\/lease-quota(?:\/audit-events)?$/.test(f.path)),
  );
  const storage = await page.evaluate(() =>
    JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }),
  );
  assert(!storage.includes(token));
  writeFileSync(
    `${root}/browser/evidence.json`,
    JSON.stringify(
      {
        checks,
        errors,
        failures,
        apiRequests: requests.length,
        boundary:
          "Live Admin API, real PostgreSQL health written by mTLS observer and standalone Worker. Ready Lease is an isolated SQL fixture, not target deployment or provider E2E.",
      },
      null,
      2,
    ),
  );
  console.log(
    JSON.stringify({ checks: checks.length, errors: errors.length, apiRequests: requests.length }),
  );
} finally {
  await browser.close();
}
