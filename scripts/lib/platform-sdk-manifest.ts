import { createHash } from "node:crypto";
import { chmodSync, lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative, resolve, sep } from "node:path";

export const PLATFORM_SDK_MANIFEST_LIBRARY_PATH = "scripts/lib/platform-sdk-manifest.ts";
export const PLATFORM_SDK_MANIFEST_TEST_PATH = "test/scripts/platform-sdk-manifest.test.ts";
export const SDK_INPUT_MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-git-mode-v2";
export const SDK_OUTPUT_TREE_ALGORITHM = "sorted-path-nul-sha256-nul-size-v1";

export type GeneratedFileRecord = {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

export function digestBytes(value: string | Uint8Array): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

export function regularFileDigest(root: string, path: string): string {
  return digestBytes(readRegularFile(root, path));
}

export function writeSDKFiles(
  root: string,
  files: ReadonlyArray<{ readonly path: string; readonly bytes: string | Uint8Array }>,
): void {
  const targets = files.map((file) => regularFileTarget(root, file.path, true));
  for (const [index, file] of files.entries()) {
    const target = targets[index]!;
    mkdirSync(dirname(target), { recursive: true });
    writeFileSync(target, file.bytes, { mode: 0o644 });
    chmodSync(target, 0o644);
  }
}

export function dependencyFileRecords(
  root: string,
  entries: ReadonlyArray<readonly [string, string]>,
): Record<string, { path: string; sha256: string }> {
  return Object.fromEntries(
    entries.map(([name, path]) => [name, { path, sha256: regularFileDigest(root, path) }]),
  );
}

export function generatedFileRecord(path: string, bytes: string | Uint8Array): GeneratedFileRecord {
  const buffer = typeof bytes === "string" ? Buffer.from(bytes) : Buffer.from(bytes);
  return { path, sha256: digestBytes(buffer), sizeBytes: buffer.byteLength };
}

export function outputTreeDigest(files: ReadonlyArray<GeneratedFileRecord>): string {
  const hash = createHash("sha256");
  for (const file of sortedUniqueFiles(files)) {
    hash
      .update(file.path)
      .update("\0")
      .update(file.sha256)
      .update("\0")
      .update(String(file.sizeBytes))
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

export function normalizedFileManifestDigest(root: string, paths: ReadonlyArray<string>): string {
  const entries = paths.map((path) => {
    const target = regularFileTarget(root, path);
    const normalized = relative(resolve(root), target).split(sep).join("/");
    const stat = lstatSync(target);
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${path} must be a regular file.`);
    return {
      path: normalized,
      sha256: digestBytes(readFileSync(target)),
      mode: (stat.mode & 0o111) === 0 ? "100644" : "100755",
    };
  });
  if (new Set(entries.map((entry) => entry.path)).size !== entries.length) {
    throw new Error("Manifest inputs must have unique normalized paths.");
  }
  const hash = createHash("sha256");
  for (const entry of entries.toSorted(comparePaths)) {
    hash
      .update(entry.path)
      .update("\0")
      .update(entry.sha256)
      .update("\0")
      .update(entry.mode)
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

export function readRegularFile(root: string, path: string): Buffer {
  return readFileSync(regularFileTarget(root, path));
}

function regularFileTarget(root: string, path: string, allowMissing = false): string {
  const rootTarget = resolve(root);
  const target = resolve(rootTarget, path);
  const normalized = relative(rootTarget, target).split(sep).join("/");
  if (normalized === "" || normalized === ".." || normalized.startsWith("../")) {
    throw new Error(`Manifest input escapes the repository root: ${path}.`);
  }
  let cursor = rootTarget;
  for (const segment of normalized.split("/")) {
    cursor = resolve(cursor, segment);
    const stat = lstatSync(cursor, { throwIfNoEntry: false });
    if (stat === undefined && allowMissing) return target;
    if (
      stat === undefined ||
      stat.isSymbolicLink() ||
      (cursor === target ? !stat.isFile() : !stat.isDirectory())
    ) {
      throw new Error(`${path} must be a regular file with non-symlink parents.`);
    }
  }
  return target;
}

function sortedUniqueFiles(files: ReadonlyArray<GeneratedFileRecord>): GeneratedFileRecord[] {
  if (new Set(files.map((file) => file.path)).size !== files.length) {
    throw new Error("Generated output paths must be unique.");
  }
  return [...files].toSorted(comparePaths);
}

function comparePaths(left: { readonly path: string }, right: { readonly path: string }): number {
  return left.path < right.path ? -1 : left.path > right.path ? 1 : 0;
}
