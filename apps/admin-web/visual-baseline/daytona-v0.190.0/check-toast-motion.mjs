import assert from "node:assert/strict";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [tokenFile, project, output, playwrightModule] = process.argv.slice(2);
assert.ok(
  tokenFile && project && output && playwrightModule,
  "usage: node check-toast-motion.mjs ADMIN_TOKEN_FILE PROJECT NEW_OUTPUT PLAYWRIGHT_MODULE",
);
mkdirSync(output);
const { chromium } = await import(pathToFileURL(resolve(playwrightModule)).href);
const browser = await chromium.launch({
  executablePath: "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  headless: true,
  args: ["--disable-gpu", "--hide-scrollbars"],
});
const errors = [];
const checks = [];
const origin = "http://127.0.0.1:4174";
try {
  for (const locale of ["en-US", "zh-CN"])
    for (const theme of ["light", "dark"])
      for (const width of [1440, 390])
        for (const reducedMotion of ["no-preference", "reduce"]) {
          const page = await browser.newPage({
            viewport: { width, height: width === 390 ? 844 : 900 },
            locale,
            reducedMotion,
          });
          page.on("pageerror", (error) => errors.push(error.message));
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
          const refresh = page.locator(".heading-actions button:first-child");
          const toast = page.locator(".success-toast");
          await refresh.click();
          await toast.waitFor();
          const animations = await toast.evaluate((element) =>
            element.getAnimations().map((animation) => ({
              property: animation.transitionProperty,
              duration: animation.effect.getTiming().duration,
            })),
          );
          if (reducedMotion === "reduce") assert.deepEqual(animations, []);
          else {
            assert.deepEqual(animations.map((a) => a.property).sort(), ["opacity", "transform"]);
            assert.ok(animations.every((a) => a.duration === 400));
            await toast.evaluate(async (element) =>
              Promise.all(element.getAnimations().map((a) => a.finished)),
            );
          }
          assert.equal(await toast.evaluate((element) => getComputedStyle(element).opacity), "1");
          await page.screenshot({
            path: join(output, `${locale}-${theme}-${width}-${reducedMotion}.png`),
          });
          const started = Date.now();
          await page.locator(".toast-close").click();
          if (reducedMotion === "no-preference") {
            assert.equal(await toast.getAttribute("data-leaving"), "true");
            assert.ok(await toast.evaluate((element) => element.getAnimations().length > 0));
          }
          await toast.waitFor({ state: "detached" });
          const elapsed = Date.now() - started;
          if (reducedMotion === "no-preference")
            assert.ok(elapsed >= 180, `Exit was removed too early: ${elapsed}ms`);
          assert.equal(
            await refresh.evaluate((element) => element === document.activeElement),
            true,
          );
          checks.push({ locale, theme, width, reducedMotion, animations, exitElapsedMs: elapsed });
          await page.close();
        }
  assert.deepEqual(errors, []);
  writeFileSync(
    join(output, "motion-evidence.json"),
    JSON.stringify(
      {
        checks,
        errors,
        scope: "Real Admin refresh Toast motion; not infrastructure or Provider E2E",
      },
      null,
      2,
    ),
  );
  process.stdout.write(`Verified ${checks.length} live motion states\n`);
} finally {
  await browser.close();
}
