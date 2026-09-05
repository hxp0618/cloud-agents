import assert from "node:assert/strict";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

// Read-only check against a pre-existing, caller-prepared disposable project.
const [output, adminTokenFile, userTokenFile, projectId] = process.argv.slice(2);
if (!output || !adminTokenFile || !userTokenFile || !projectId)
  throw new Error(
    "usage: capture-lease-states.mjs OUTPUT ADMIN_TOKEN_FILE USER_TOKEN_FILE PROJECT_ID",
  );
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE ?? "playwright");
const origin = "http://127.0.0.1:4174";
const token = readFileSync(adminTokenFile, "utf8").trim();
const path = `/v1/admin/tenants/tenant-local/projects/${encodeURIComponent(projectId)}/environment-leases`;
const leases = [],
  seen = new Set();
let pageToken;
do {
  const url = new URL(path, origin);
  url.searchParams.set("pageSize", "200");
  if (pageToken) url.searchParams.set("pageToken", pageToken);
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${token}`, "X-Request-ID": "lease-state-read" },
  });
  assert.equal(response.status, 200);
  const body = await response.json();
  leases.push(...body.environmentLeases);
  pageToken = body.nextPageToken;
  if (pageToken) {
    assert(!seen.has(pageToken));
    seen.add(pageToken);
  }
} while (pageToken);
assert(
  leases.length >= 2 && new Set(leases.map((x) => x.spec.observedPhase)).size >= 2,
  "Requires nonempty real Lease fixtures with at least two phases",
);
const denied = await fetch(new URL(path, origin), {
  headers: {
    Authorization: `Bearer ${readFileSync(userTokenFile, "utf8").trim()}`,
    "X-Request-ID": "lease-state-denied",
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
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("response", (r) => {
    if (r.status() >= 400) failures.push({ status: r.status(), path: new URL(r.url()).pathname });
  });
  page.on("request", (r) => {
    const u = new URL(r.url());
    if (u.protocol.startsWith("http")) origins.add(u.origin);
    if (u.pathname.startsWith("/v1/") && r.method() !== "GET") mutations.push(r.method());
  });
  await page.goto(origin);
  assert.equal(new URL(page.url()).origin, origin);
  assert.match(await page.title(), /Cloud Agents/);
  for (const [i, value] of [origin, "tenant-local", projectId, token].entries())
    await page.locator(".connect-form input").nth(i).fill(value);
  await page.locator(".connect-form button[type=submit]").click();
  await page.locator(".app-shell").waitFor();
  const navigate = async (name) => {
    if (page.viewportSize().width < 768) await page.locator(".mobile-nav-trigger").click();
    await page.locator(`.sidebar [data-page=${name}]`).click();
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
  const rowIds = () =>
    page.locator(".resource-list tbody tr td:first-child small").allTextContents();
  for (const locale of ["en-US", "zh-CN"])
    for (const theme of ["light", "dark"])
      for (const width of [1440, 390]) {
        const name = `${locale}-${theme}-${width}`;
        await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
        await page.locator(".profile-menu summary").click();
        await page.locator(".locale-picker select").selectOption(locale);
        await page.locator(".profile-menu summary").click();
        await page.evaluate((t) => (document.documentElement.dataset.theme = t), theme);
        const counts = {};
        for (const state of [
          "provisioning",
          "ready",
          "terminating",
          "terminated",
          "failed",
          "cleanup-blocked",
        ]) {
          await navigate("overview");
          const expected = leases.filter((x) =>
            state === "cleanup-blocked"
              ? x.spec.cleanupPhase === "blocked"
              : x.spec.observedPhase === state,
          );
          const button = page.locator(`[data-lease-state=${state}]`);
          assert((await button.innerText()).endsWith(`· ${expected.length}`));
          await button.scrollIntoViewIfNeeded();
          await button.focus();
          await page.keyboard.press("Enter");
          await page.locator(".lease-active-state").waitFor();
          assert.deepEqual((await rowIds()).sort(), expected.map((x) => x.metadata.uid).sort());
          if (expected.length === 0)
            assert.equal(
              await page.locator(".table-empty").innerText(),
              locale === "en-US" ? "No leases match these filters." : "没有符合筛选条件的租约。",
            );
          await capture(`${name}-${state}.png`);
          const search = page.locator(".list-toolbar input[type=search]");
          if (expected.length) {
            await search.fill(expected[0].metadata.uid);
            assert.deepEqual(await rowIds(), [expected[0].metadata.uid]);
          }
          await search.fill("no-matching-lease");
          assert.equal((await rowIds()).length, 0);
          await page.locator(".lease-active-state button").click();
          assert.equal(await search.inputValue(), "no-matching-lease");
          await search.fill("");
          assert.equal((await rowIds()).length, leases.length);
          counts[state] = expected.length;
        }
        await navigate("overview");
        await page.locator(".lease-state-summary").scrollIntoViewIfNeeded();
        await capture(`${name}-summary.png`);
        checks.push({
          name,
          counts,
          keyboardNavigation: true,
          searchIntersection: true,
          clearPreservesQuery: true,
        });
      }
  assert.deepEqual(errors, []);
  assert.deepEqual(mutations, []);
  assert.deepEqual([...origins], [origin]);
  assert(failures.every((x) => x.status === 404 && x.path.includes("/lease-quota")));
  assert.equal(
    await page.evaluate(
      (t) =>
        [...Object.values(localStorage), ...Object.values(sessionStorage)].some((v) =>
          v.includes(t),
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
        ordinary403: 403,
        leaseIds: leases.map((x) => x.metadata.uid),
      },
      null,
      2,
    ),
  );
} finally {
  await browser.close();
}
