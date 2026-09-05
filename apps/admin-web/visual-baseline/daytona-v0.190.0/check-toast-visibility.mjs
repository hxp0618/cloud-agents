import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { pathToFileURL } from "node:url";

const [tokenFile, project, output, playwrightModule] = process.argv.slice(2);
assert.ok(
  tokenFile && project && output && playwrightModule,
  "usage: node check-toast-visibility.mjs ADMIN_TOKEN_FILE PROJECT NEW_OUTPUT PLAYWRIGHT_MODULE",
);
mkdirSync(output);
const { chromium } = await import(pathToFileURL(resolve(playwrightModule)).href);
const profile = mkdtempSync(join(tmpdir(), "cloud-agents-visibility-browser-"));
const child = spawn(
  "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  [
    "--headless=new",
    "--no-first-run",
    "--no-default-browser-check",
    "--remote-debugging-port=0",
    `--user-data-dir=${profile}`,
    "about:blank",
  ],
  { stdio: "ignore" },
);
const origin = "http://127.0.0.1:4174";
let browser;
const checks = [],
  exceptions = [],
  httpFailures = [],
  mutations = [];
const origins = new Set();
try {
  let port;
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      port = readFileSync(join(profile, "DevToolsActivePort"), "utf8").split("\n")[0];
      break;
    } catch {
      await delay(50);
    }
  }
  assert.ok(port, "Owned browser did not expose CDP");
  // Default focus emulation keeps background pages visible. Use native browser state instead.
  browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`, { noDefaults: true });
  const context = browser.contexts()[0];
  const other = context.pages()[0];
  for (const locale of ["en-US", "zh-CN"])
    for (const theme of ["light", "dark"])
      for (const width of [1440, 390]) {
        const page = await context.newPage();
        page.on("pageerror", (error) => exceptions.push(error.message));
        page.on("request", (request) => {
          origins.add(new URL(request.url()).origin);
          if (!["GET", "HEAD"].includes(request.method())) mutations.push(request.method());
        });
        page.on("response", (response) => {
          if (response.status() >= 400)
            httpFailures.push({
              status: response.status(),
              path: new URL(response.url()).pathname,
            });
        });
        await page.setViewportSize({ width, height: width === 390 ? 844 : 900 });
        await page.addInitScript(
          ({ locale, theme }) => {
            localStorage.setItem("cloud-agents-admin-locale", locale);
            localStorage.setItem("cloud-agents-admin-theme", theme);
          },
          { locale, theme },
        );
        await page.goto(origin);
        assert.match(await page.title(), /Cloud Agents/);
        const values = [origin, "tenant-local", project, readFileSync(tokenFile, "utf8").trim()];
        for (let i = 0; i < values.length; i++)
          await page.locator(".connect-form input").nth(i).fill(values[i]);
        await page.locator(".connect-form button[type=submit]").click();
        await page.locator(".app-shell").waitFor();
        await page.bringToFront();
        assert.equal(await page.evaluate(() => document.visibilityState), "visible");
        await page.locator(".heading-actions button:first-child").click();
        const toast = page.locator(".success-toast");
        await toast.waitFor();
        await delay(700);
        assert.equal(
          await toast.evaluate((e) => e.contains(document.activeElement) || e.matches(":hover")),
          false,
        );
        await page.evaluate(() => {
          window.visibilityEvidence = [];
          document.addEventListener("visibilitychange", () =>
            window.visibilityEvidence.push(document.visibilityState),
          );
        });
        await other.bringToFront();
        await page.waitForFunction(() => document.hidden, undefined, { polling: 100 });
        const hiddenAt = Date.now();
        await delay(4500);
        assert.equal(await page.evaluate(() => document.visibilityState), "hidden");
        assert.deepEqual(await page.evaluate(() => window.visibilityEvidence), ["hidden"]);
        assert.equal(await toast.getAttribute("data-leaving"), "false");
        assert.equal(await toast.evaluate((e) => e.matches(":popover-open")), true);
        const hiddenMs = Date.now() - hiddenAt;
        await page.bringToFront();
        await page.waitForFunction(() => !document.hidden, undefined, { polling: 100 });
        const resumedAt = Date.now();
        await page.screenshot({ path: join(output, `${locale}-${theme}-${width}-resumed.png`) });
        await toast.waitFor({ state: "detached", timeout: 5000 });
        const resumedMs = Date.now() - resumedAt;
        const visibilityEvents = await page.evaluate(() => window.visibilityEvidence);
        assert.deepEqual(visibilityEvents, ["hidden", "visible"]);
        assert.ok(
          resumedMs > 2000 && resumedMs < 4300,
          `Unexpected remaining lifetime: ${resumedMs}`,
        );
        checks.push({
          locale,
          theme,
          width,
          hiddenMs,
          resumedMs,
          visibilityEvents,
          nativeHidden: true,
          survivedBackground: true,
          expiredAfterResume: true,
        });
        await page.close();
      }
  assert.deepEqual(exceptions, []);
  assert.deepEqual(mutations, []);
  assert.deepEqual([...origins], [origin]);
  const quotaPath = `/v1/admin/tenants/tenant-local/projects/${project}/lease-quota`;
  assert.ok(
    httpFailures.every(
      (f) => f.status === 404 && [quotaPath, `${quotaPath}/audit-events`].includes(f.path),
    ),
    JSON.stringify(httpFailures),
  );
  writeFileSync(
    join(output, "visibility-evidence.json"),
    JSON.stringify(
      {
        checks,
        exceptions,
        httpFailures,
        mutations,
        origins: [...origins],
        scope:
          "Native browser background pause after real Admin refresh; not Provider or infrastructure E2E",
      },
      null,
      2,
    ),
  );
  process.stdout.write(`Verified ${checks.length} native visibility states\n`);
} finally {
  await browser?.close();
  child.kill("SIGTERM");
  if (child.exitCode === null) await new Promise((resolve) => child.once("exit", resolve));
  renameSync(profile, join(homedir(), ".Trash", profile.split("/").at(-1)));
}
