import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { format } from "oxfmt";

const [source, output, playwrightModule] = process.argv.slice(2);
assert.ok(
  source && output && playwrightModule,
  "usage: node capture-reference.mjs DAYTONA_CHECKOUT NEW_OUTPUT PLAYWRIGHT_MODULE",
);
const commit = "01c502bb1f1ff8f2885d0cd490e043736083dca8";
const git = (...args) => execFileSync("git", ["-C", source, ...args], { encoding: "utf8" }).trim();
assert.equal(git("rev-parse", "HEAD"), commit, "Wrong upstream commit");
git("diff", "--exit-code", "HEAD", "--", "apps/dashboard", "package.json", "yarn.lock");
const composition = readFileSync(new URL("reference-composition.stories.tsx", import.meta.url));
assert.deepEqual(
  readFileSync(
    join(source, "apps/dashboard/src/components/ui/stories/visual-baseline.stories.tsx"),
  ),
  composition,
  "Install the exact reference composition first",
);
// Never overwrite the historical references or merge a partial run into an existing result.
mkdirSync(output);
const { chromium } = await import(pathToFileURL(resolve(playwrightModule)).href);
const browser = await chromium.launch({
  executablePath: "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
  headless: true,
  args: ["--disable-gpu", "--hide-scrollbars"],
});
const errors = [];
const warnings = [];
const origins = new Set();
const captures = [];
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
try {
  for (const [width, height, viewport] of [
    [1440, 900, "desktop"],
    [390, 844, "mobile"],
  ]) {
    const page = await browser.newPage({
      viewport: { width, height },
      deviceScaleFactor: 1,
      isMobile: width === 390,
      reducedMotion: "reduce",
    });
    page.on("pageerror", (error) => errors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") errors.push(message.text());
      if (message.type() === "warning") warnings.push(message.text());
    });
    page.on("request", (request) => {
      if (/^https?:/.test(request.url())) origins.add(new URL(request.url()).origin);
    });
    for (const theme of ["light", "dark"]) {
      for (const [story, selector] of [
        ["list", "[data-slot=table]"],
        ["detail", "[data-slot=sheet-content]"],
        ["create-form", "[data-slot=sheet-content]"],
        ["confirm-dialog", "[role=dialog]"],
        ["dropdown", "[role=menu]"],
        ["empty-state", "[data-slot=empty]"],
        ["loading", "[data-slot=table-cell] > div"],
        ["error-state", "[role=alert]"],
        ["permission-denied", "[data-slot=empty]"],
      ]) {
        await page.goto(
          `http://127.0.0.1:6006/iframe.html?id=reference-visual-baseline--${story}&viewMode=story&globals=theme:${theme}`,
        );
        await page.locator(selector).first().waitFor();
        await page.waitForFunction(
          (dark) => document.documentElement.classList.contains("dark") === dark,
          theme === "dark",
        );
        await page.evaluate(async () => {
          await document.fonts.ready;
          await Promise.all(
            document
              .getAnimations()
              .filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity)
              .map((animation) => animation.finished.catch(() => {})),
          );
        });
        const geometry = await page.evaluate(() => {
          const rect = (selector) =>
            document.querySelector(selector)?.getBoundingClientRect().toJSON();
          return {
            viewport: [innerWidth, innerHeight],
            clientWidth: document.documentElement.clientWidth,
            scrollWidth: document.documentElement.scrollWidth,
            sheet: rect("[data-slot=sheet-content]"),
            footer: rect("[data-slot=sheet-footer]"),
          };
        });
        assert.deepEqual(geometry.viewport, [width, height], `${story}: layout viewport drift`);
        assert.equal(geometry.scrollWidth, geometry.clientWidth, `${story}: document overflow`);
        if (story === "detail" || story === "create-form") {
          assert.equal(geometry.sheet.x, width - Math.min(500, width));
          assert.equal(geometry.sheet.width, Math.min(500, width));
          assert.equal(geometry.sheet.bottom, height);
          assert.equal(geometry.footer.bottom, height);
        }
        const filename = `${story}-${theme}-${viewport}.png`;
        const bytes = await page.screenshot({
          path: join(output, filename),
          animations: "disabled",
          caret: "hide",
        });
        captures.push({ filename, theme, ...geometry, sha256: hash(bytes) });
      }
    }
    await page.close();
  }
  assert.deepEqual(errors, [], "Reference runtime errors");
  assert.deepEqual([...origins], ["http://127.0.0.1:6006"]);
  assert.equal(captures.length, 36);
  const evidence = await format(
    "reference-evidence.json",
    JSON.stringify(
      {
        commit,
        compositionSHA256: hash(composition),
        capturedAt: new Date().toISOString(),
        browser: browser.version(),
        captures,
        errors,
        warnings,
        origins: [...origins],
        scope:
          "Neutral upstream component composition; not Cloud Agents runtime evidence or full visual approval",
      },
      null,
      2,
    ) + "\n",
  );
  assert.equal(evidence.errors.length, 0, "Reference evidence formatting failed");
  writeFileSync(join(output, "reference-evidence.json"), evidence.code);
  process.stdout.write(`Captured ${captures.length} verified reference states in ${output}\n`);
} finally {
  await browser.close();
}
