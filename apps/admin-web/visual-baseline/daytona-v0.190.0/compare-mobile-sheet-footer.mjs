import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

// Component-only pixel check. It cannot qualify the whole page or validate reference provenance.
// PNG_MODULE uses an existing PNG decoder (pngjs or the bundled Playwright utilsBundle).
const [referencePath, actualPath, pngModule] = process.argv.slice(2);
assert.ok(
  referencePath && actualPath && pngModule,
  "usage: node compare-mobile-sheet-footer.mjs REFERENCE ACTUAL PNG_MODULE",
);
const { PNG } = await import(pathToFileURL(pngModule).href);
const referenceBytes = readFileSync(referencePath);
const actualBytes = readFileSync(actualPath);
const reference = PNG.sync.read(referenceBytes);
const actual = PNG.sync.read(actualBytes);
for (const picture of [reference, actual]) {
  assert.equal(picture.width, 390);
  assert.equal(picture.height, 844);
}
let checked = 0;
let changed = 0;
// Fixed full-width 105px SheetFooter: border, background, padding, gap and button outlines.
// Only the two button-label interiors are masked; never mask edges or realign the images.
for (let y = 739; y < 844; y++) {
  for (let x = 0; x < 390; x++) {
    if (x >= 40 && x < 350 && ((y >= 760 && y < 784) || (y >= 800 && y < 824))) continue;
    checked++;
    const offset = (y * 390 + x) * 4;
    if (
      [0, 1, 2, 3].some(
        (channel) => reference.data[offset + channel] !== actual.data[offset + channel],
      )
    )
      changed++;
  }
}
const digest = (bytes) => createHash("sha256").update(bytes).digest("hex");
process.stdout.write(
  JSON.stringify({
    reference: digest(referenceBytes),
    actual: digest(actualBytes),
    checked,
    changed,
    scope: "390x844 mobile Sheet footer only; button labels masked",
  }) + "\n",
);
assert.equal(changed, 0, "Sheet footer pixel mismatch");
