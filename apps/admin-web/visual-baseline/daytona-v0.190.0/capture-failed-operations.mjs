import assert from "node:assert/strict";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Read-only browser check. Prepare >=7 real failed Operations and one success via Admin API first.
const [output, adminTokenFile, userTokenFile, projectId] = process.argv.slice(2);
if (!output || !adminTokenFile || !userTokenFile || !projectId)
  throw new Error(
    "usage: capture-failed-operations.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_ID",
  );
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? "playwright");
const origin = "http://127.0.0.1:4174";
const token = readFileSync(adminTokenFile, "utf8").trim();
const path = `/v1/admin/tenants/tenant-local/projects/${encodeURIComponent(projectId)}/maintenance-operations`;
const operations = [];
let pageToken;
do {
  const url = new URL(path, origin);
  url.searchParams.set("pageSize", "200");
  if (pageToken) url.searchParams.set("pageToken", pageToken);
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${token}`, "X-Request-ID": "failed-overview-read" },
  });
  assert.equal(response.status, 200);
  const body = await response.json();
  operations.push(...body.operations);
  pageToken = body.nextPageToken;
} while (pageToken);
const failed = operations
  .filter((x) => x.state === "failed")
  .sort(
    (a, b) =>
      Date.parse(b.updatedAt) - Date.parse(a.updatedAt) ||
      (a.operationId < b.operationId ? -1 : a.operationId > b.operationId ? 1 : 0),
  );
assert.ok(
  failed.length >= 7 && operations.some((x) => x.state === "succeeded"),
  "Requires real failed and succeeded Operation fixtures",
);
const denied = await fetch(new URL(path, origin), {
  headers: {
    Authorization: `Bearer ${readFileSync(userTokenFile, "utf8").trim()}`,
    "X-Request-ID": "failed-overview-denied",
  },
});
assert.equal(denied.status, 403);
mkdirSync(output, { recursive: true });
const browser = await chromium.launch({
  executablePath:
    process.env.BROWSER_PATH ?? "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  headless: true,
});
const checks = [],
  errors = [],
  failures = [],
  mutations = [],
  screenshots = [];
const origins = new Set();
try {
  const page = await browser.newPage({
    viewport: { width: 1440, height: 900 },
    locale: "en-US",
    reducedMotion: "reduce",
  });
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("response", (response) => {
    if (response.status() >= 400)
      failures.push({ status: response.status(), path: new URL(response.url()).pathname });
  });
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.protocol === "http:" || url.protocol === "https:") origins.add(url.origin);
    if (url.pathname.startsWith("/v1/") && request.method() !== "GET")
      mutations.push({ method: request.method(), path: url.pathname });
  });
  await page.goto(origin);
  assert.equal(new URL(page.url()).origin, origin);
  assert.match(await page.title(), /Cloud Agents/);
  const inputs = [origin, "tenant-local", projectId, token];
  for (let i = 0; i < inputs.length; i++)
    await page.locator(".connect-form input").nth(i).fill(inputs[i]);
  await page.locator(".connect-form button[type=submit]").click();
  await page.locator(".app-shell").waitFor();
  const navigate = async (name) => {
    if (page.viewportSize().width < 768) await page.locator(".mobile-nav-trigger").click();
    await page.locator(`.sidebar [data-page=${name}]`).click();
    await page
      .locator(`.sidebar [data-page=${name}][aria-current=page]`)
      .waitFor({ state: "attached" });
  };
  const capture = async (filename) => {
    assert.equal(await page.locator("vite-error-overlay").count(), 0);
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth === document.documentElement.clientWidth,
      ),
    );
    await page.screenshot({ path: join(output, filename) });
    screenshots.push(filename);
  };
  const rowIds = (selector) =>
    page.locator(`${selector} tbody tr td:first-child small`).allTextContents();
  for (const locale of ["en-US", "zh-CN"])
    for (const theme of ["light", "dark"])
      for (const width of [1440, 390]) {
        await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
        await page.locator(".profile-menu summary").click();
        await page.locator(".locale-picker select").selectOption(locale);
        await page.locator(".profile-menu summary").click();
        await page.evaluate((theme) => (document.documentElement.dataset.theme = theme), theme);
        await navigate("overview");
        const recent = page.locator(".recent-failed-operations");
        assert.deepEqual(
          await rowIds(".recent-failed-operations"),
          failed.slice(0, 6).map((x) => x.operationId),
        );
        await recent.scrollIntoViewIfNeeded();
        const name = `${locale}-${theme}-${width}`;
        await capture(`${name}-recent.png`);
        const first = recent.locator("tbody tr td:first-child button").first();
        await first.focus();
        await page.keyboard.press("Enter");
        const sheet = page.locator(".admin-sheet");
        await sheet.waitFor();
        const detail = await sheet.innerText();
        for (const value of [failed[0].operationId, failed[0].requestId, failed[0].stableErrorCode])
          assert(detail.includes(value));
        await capture(`${name}-detail.png`);
        await page.keyboard.press("Escape");
        await sheet.waitFor({ state: "detached" });
        assert(await first.evaluate((element) => document.activeElement === element));
        await recent.locator(".panel-heading button").click();
        await page.locator(".maintenance-failed-filter[aria-pressed=true]").waitFor();
        assert.deepEqual(
          await rowIds(".resource-list"),
          failed.map((x) => x.operationId),
        );
        await capture(`${name}-all-failed.png`);
        const search = page.locator(".list-toolbar input[type=search]");
        await search.fill(failed[0].stableErrorCode);
        assert.deepEqual(
          await rowIds(".resource-list"),
          failed
            .filter((x) => x.stableErrorCode === failed[0].stableErrorCode)
            .map((x) => x.operationId),
        );
        await search.fill("no-matching-operation");
        assert.equal(await page.locator("tbody tr").count(), 0);
        assert.equal(
          await page.locator(".table-empty").innerText(),
          locale === "en-US" ? "No operations match these filters." : "没有符合筛选条件的操作。",
        );
        await page.locator(".maintenance-failed-filter").click();
        assert.equal(await search.inputValue(), "no-matching-operation");
        await search.fill("");
        assert.equal(await page.locator("tbody tr").count(), operations.length);
        await navigate("overview");
        assert.deepEqual(
          await rowIds(".recent-failed-operations"),
          failed.slice(0, 6).map((x) => x.operationId),
        );
        checks.push({
          name,
          recent: 6,
          failed: failed.length,
          all: operations.length,
          detailFocusRestore: true,
          stableCodeSearch: true,
          filterClear: true,
        });
      }
  assert.deepEqual(errors, []);
  assert.deepEqual(mutations, []);
  assert.deepEqual([...origins], [origin]);
  assert.ok(failures.every((x) => x.status === 404 && x.path.includes("/lease-quota")));
  assert.equal(
    await page.evaluate(
      (token) =>
        [...Object.values(localStorage), ...Object.values(sessionStorage)].some((x) =>
          x.includes(token),
        ),
      token,
    ),
    false,
  );
  writeFileSync(
    join(output, "browser-evidence.json"),
    JSON.stringify(
      {
        checks,
        screenshots,
        errors,
        failures,
        mutations,
        origins: [...origins],
        ordinary403: denied.status,
        operationIds: failed.map((x) => x.operationId),
      },
      null,
      2,
    ),
  );
} finally {
  await browser.close();
}
