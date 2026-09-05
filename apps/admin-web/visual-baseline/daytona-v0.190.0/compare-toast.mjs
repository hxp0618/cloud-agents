import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const [referencePath, actualPath, pngModule] = process.argv.slice(2);
assert.ok(
  referencePath && actualPath && pngModule,
  "usage: node compare-toast.mjs REFERENCE ACTUAL PNG_MODULE",
);
const { PNG } = await import(pathToFileURL(pngModule).href);
const referenceBytes = readFileSync(referencePath);
const actualBytes = readFileSync(actualPath);
const reference = PNG.sync.read(referenceBytes);
const actual = PNG.sync.read(actualBytes);
assert.ok(reference.width === 390 || reference.width === 1440);
assert.equal(reference.height, reference.width === 390 ? 844 : 900);
assert.equal(actual.width, reference.width);
assert.equal(actual.height, reference.height);
const mobile = reference.width === 390;
const right = reference.width - (mobile ? 16 : 32);
const left = right - (mobile ? reference.width - 32 : 356);
const bottom = reference.height - (mobile ? 16 : 32);
const top = bottom - 53.5;
let checked = 0;
let changed = 0;
// Fixed viewport coordinates; no translation or pixel tolerance. Mask only the title/icon
// interior. Keep edges, background, shadow and the complete close control under comparison.
// This single-line Toast region does not qualify the underlying page or other Toast states.
for (let y = Math.floor(top) - 10; y < reference.height; y++) {
  for (let x = Math.max(0, left - 15); x < reference.width; x++) {
    if (x >= left + 10 && x < right - 12 && y >= top + 10 && y < bottom - 10) continue;
    checked++;
    const offset = (y * reference.width + x) * 4;
    if (
      [0, 1, 2, 3].some(
        (channel) => reference.data[offset + channel] !== actual.data[offset + channel],
      )
    )
      changed++;
  }
}
const hash = (bytes) => createHash("sha256").update(bytes).digest("hex");
process.stdout.write(
  JSON.stringify({
    reference: hash(referenceBytes),
    actual: hash(actualBytes),
    checked,
    changed,
    scope: "Single-line success Toast exterior; title and icon masked",
  }) + "\n",
);
assert.equal(changed, 0, "Toast exterior pixel mismatch");
