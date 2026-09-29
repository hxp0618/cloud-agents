import { createHash } from "node:crypto";
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import { MigrationValidationError } from "./platform-migration-json";

// Product catalogs are cumulative, so each product version stores only a zero-context
// unified line patch against the catalog its manifest names as predecessor. The frozen
// manifest descriptors (size and SHA-256) stay the authority for every reconstruction.

export const PRODUCT_MIGRATIONS = "services/control-plane/migrations/product";

type Descriptor = {
  readonly path: string;
  readonly mode: string;
  readonly size_bytes: number;
  readonly sha256: string;
};
type Hunk = {
  readonly oldStart: number;
  readonly oldCount: number;
  readonly newStart: number;
  readonly newCount: number;
};

export function productCatalogPatchPath(catalogPath: string): string {
  const match = new RegExp(`^${PRODUCT_MIGRATIONS}/[0-9]{6}/catalog/schema-[0-9]{6}\\.json$`, "u");
  if (!match.test(catalogPath))
    throw new MigrationValidationError("CATALOG_PATCH_PATH", catalogPath);
  return `${catalogPath.slice(0, -".json".length)}.patch`;
}

export function formatCatalogPatch(
  basePath: string,
  targetPath: string,
  base: Uint8Array,
  target: Uint8Array,
): Buffer {
  const oldLines = patchLines(base, basePath);
  const newLines = patchLines(target, targetPath);
  const output = [`--- ${basePath}`, `+++ ${targetPath}`];
  for (const hunk of diffLines(oldLines, newLines)) {
    output.push(
      `@@ -${hunkPosition(hunk.oldStart, hunk.oldCount)},${hunk.oldCount} +${hunkPosition(hunk.newStart, hunk.newCount)},${hunk.newCount} @@`,
    );
    for (let index = 0; index < hunk.oldCount; index += 1)
      output.push(`-${oldLines[hunk.oldStart + index]}`);
    for (let index = 0; index < hunk.newCount; index += 1)
      output.push(`+${newLines[hunk.newStart + index]}`);
  }
  const patch = Buffer.from(`${output.join("\n")}\n`);
  if (!applyCatalogPatch(patch, basePath, targetPath, base).equals(target))
    throw new MigrationValidationError("CATALOG_PATCH_ROUND_TRIP", targetPath);
  return patch;
}

export function applyCatalogPatch(
  patch: Uint8Array,
  basePath: string,
  targetPath: string,
  base: Uint8Array,
): Buffer {
  const oldLines = patchLines(base, basePath);
  const lines = patchLines(patch, `${targetPath} patch`);
  if (lines[0] !== `--- ${basePath}` || lines[1] !== `+++ ${targetPath}`)
    throw new MigrationValidationError("CATALOG_PATCH_HEADER", targetPath);
  const result: string[] = [];
  let oldIndex = 0;
  let index = 2;
  while (index < lines.length) {
    const header = /^@@ -([0-9]+),([0-9]+) \+([0-9]+),([0-9]+) @@$/u.exec(lines[index]!);
    if (!header) throw new MigrationValidationError("CATALOG_PATCH_HUNK", `${targetPath}:${index}`);
    index += 1;
    const [oldPosition, oldCount, newPosition, newCount] = header.slice(1).map(Number) as [
      number,
      number,
      number,
      number,
    ];
    const oldStart = oldCount === 0 ? oldPosition : oldPosition - 1;
    const newStart = newCount === 0 ? newPosition : newPosition - 1;
    if (
      oldCount + newCount === 0 ||
      oldStart < oldIndex ||
      oldStart + oldCount > oldLines.length ||
      newStart !== result.length + oldStart - oldIndex
    ) {
      throw new MigrationValidationError("CATALOG_PATCH_RANGE", `${targetPath}:${index - 1}`);
    }
    while (oldIndex < oldStart) result.push(oldLines[oldIndex++]!);
    for (let count = 0; count < oldCount; count += 1) {
      if (lines[index++] !== `-${oldLines[oldIndex++]}`)
        throw new MigrationValidationError("CATALOG_PATCH_CONTEXT", `${targetPath}:${index - 1}`);
    }
    for (let count = 0; count < newCount; count += 1) {
      const line = lines[index++];
      if (line?.[0] !== "+")
        throw new MigrationValidationError("CATALOG_PATCH_INSERT", `${targetPath}:${index - 1}`);
      result.push(line.slice(1));
    }
  }
  while (oldIndex < oldLines.length) result.push(oldLines[oldIndex++]!);
  return Buffer.from(`${result.join("\n")}\n`);
}

// Reconstructs the requested product catalogs by replaying every product patch from the
// first product version, verifying each step against its frozen manifest descriptors.
export function readProductCatalogs(root: string, versions: Iterable<string>): Map<string, Buffer> {
  const wanted = new Set(versions);
  const last = [...wanted].toSorted().at(-1);
  const catalogs = new Map<string, Buffer>();
  let previous: { readonly descriptor: Descriptor; readonly bytes: Buffer } | undefined;
  for (const version of productVersions(root)) {
    if (last === undefined || version > last) break;
    const directory = `${PRODUCT_MIGRATIONS}/${version}`;
    const manifest = JSON.parse(readFileSync(resolve(root, `${directory}/manifest.json`), "utf8"));
    const entry = manifest.schema_bundle?.migrations?.at(-1);
    const base: Descriptor = entry?.predecessor_catalog_contract;
    const target: Descriptor = entry?.catalog_contract;
    if (
      manifest.schema_bundle?.schema_head !== version ||
      typeof base?.path !== "string" ||
      typeof target?.path !== "string" ||
      !target.path.startsWith(`${directory}/catalog/`)
    ) {
      throw new MigrationValidationError("PRODUCT_CATALOG_BINDING", directory);
    }
    const patchPath = productCatalogPatchPath(target.path);
    assertExactEntries(root, directory, ["catalog", "manifest.json", "schema-bundle.json"]);
    assertExactEntries(root, `${directory}/catalog`, [patchPath.split("/").at(-1)!]);
    let baseBytes: Buffer;
    if (previous) {
      if (JSON.stringify(base) !== JSON.stringify(previous.descriptor))
        throw new MigrationValidationError("PRODUCT_CATALOG_PREDECESSOR", directory);
      baseBytes = previous.bytes;
    } else {
      // The first product version starts from a frozen catalog outside the product tree.
      if (base.path.startsWith(`${PRODUCT_MIGRATIONS}/`))
        throw new MigrationValidationError("PRODUCT_CATALOG_BASELINE", directory);
      baseBytes = readFileSync(resolve(root, base.path));
      verifyDescriptor(base, baseBytes);
    }
    const bytes = applyCatalogPatch(
      readFileSync(resolve(root, patchPath)),
      base.path,
      target.path,
      baseBytes,
    );
    verifyDescriptor(target, bytes);
    if (wanted.has(version)) catalogs.set(version, bytes);
    previous = { descriptor: target, bytes };
  }
  for (const version of wanted) {
    if (!catalogs.has(version))
      throw new MigrationValidationError("PRODUCT_CATALOG_UNKNOWN", version);
  }
  return catalogs;
}

function productVersions(root: string): string[] {
  return readdirSync(resolve(root, PRODUCT_MIGRATIONS))
    .filter((name) => /^[0-9]{6}$/u.test(name))
    .toSorted();
}

function assertExactEntries(root: string, directory: string, expected: ReadonlyArray<string>) {
  // Hidden entries (for example editor or Finder metadata) are never read or committed.
  const actual = readdirSync(resolve(root, directory))
    .filter((name) => !name.startsWith("."))
    .toSorted();
  if (JSON.stringify(actual) !== JSON.stringify([...expected].toSorted()))
    throw new MigrationValidationError(
      "PRODUCT_CATALOG_FILES",
      `${directory}: ${actual.join(",")}`,
    );
}

function verifyDescriptor(descriptor: Descriptor, bytes: Uint8Array) {
  const digest = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  if (
    descriptor.mode !== "100644" ||
    descriptor.size_bytes !== bytes.length ||
    descriptor.sha256 !== digest
  ) {
    throw new MigrationValidationError("PRODUCT_CATALOG_DIGEST", descriptor.path);
  }
}

function patchLines(bytes: Uint8Array, label: string): string[] {
  let text: string;
  try {
    text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw new MigrationValidationError("CATALOG_PATCH_UTF8", label);
  }
  if (!text.endsWith("\n") || text.includes("\r"))
    throw new MigrationValidationError("CATALOG_PATCH_LINES", label);
  return text.slice(0, -1).split("\n");
}

function hunkPosition(start: number, count: number): number {
  return count === 0 ? start : start + 1;
}

// Myers O(ND) shortest edit script over lines; trace[d] keeps diagonals -d..d.
function diffLines(a: ReadonlyArray<string>, b: ReadonlyArray<string>): Hunk[] {
  let prefix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) prefix += 1;
  let suffix = 0;
  while (
    suffix < a.length - prefix &&
    suffix < b.length - prefix &&
    a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
  ) {
    suffix += 1;
  }
  const n = a.length - prefix - suffix;
  const m = b.length - prefix - suffix;
  const trace: Int32Array[] = [];
  const moveDown = (previous: Int32Array, d: number, k: number) =>
    k === -d || (k !== d && previous[k - 1 + d - 1]! < previous[k + 1 + d - 1]!);
  let depth = -1;
  for (let d = 0; depth < 0; d += 1) {
    const previous = trace[d - 1]!;
    const v = new Int32Array(2 * d + 1);
    for (let k = -d; k <= d; k += 2) {
      let x =
        d === 0
          ? 0
          : moveDown(previous, d, k)
            ? previous[k + 1 + d - 1]!
            : previous[k - 1 + d - 1]! + 1;
      let y = x - k;
      while (x < n && y < m && a[prefix + x] === b[prefix + y]) {
        x += 1;
        y += 1;
      }
      v[k + d] = x;
      if (x >= n && y >= m) {
        depth = d;
        break;
      }
    }
    trace.push(v);
  }
  const deleted = new Uint8Array(a.length);
  const inserted = new Uint8Array(b.length);
  for (let d = depth, x = n, y = m; d > 0; d -= 1) {
    const previous = trace[d - 1]!;
    const down = moveDown(previous, d, x - y);
    const previousK = down ? x - y + 1 : x - y - 1;
    const previousX = previous[previousK + d - 1]!;
    const previousY = previousX - previousK;
    if (down) inserted[prefix + previousY] = 1;
    else deleted[prefix + previousX] = 1;
    x = previousX;
    y = previousY;
  }
  const hunks: Hunk[] = [];
  for (let i = 0, j = 0; i < a.length || j < b.length;) {
    if (i < a.length && j < b.length && !deleted[i] && !inserted[j]) {
      i += 1;
      j += 1;
      continue;
    }
    const oldStart = i;
    const newStart = j;
    while ((i < a.length && deleted[i]) || (j < b.length && inserted[j])) {
      while (i < a.length && deleted[i]) i += 1;
      while (j < b.length && inserted[j]) j += 1;
    }
    if (i === oldStart && j === newStart)
      throw new MigrationValidationError("CATALOG_PATCH_DIFF", "edit script is not aligned");
    hunks.push({ oldStart, oldCount: i - oldStart, newStart, newCount: j - newStart });
  }
  return hunks;
}
