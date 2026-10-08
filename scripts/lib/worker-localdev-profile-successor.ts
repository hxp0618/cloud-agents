import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export type ImmutablePredecessor = Readonly<{
  commit: string;
  tree: string;
  blobs: Readonly<Record<string, string>>;
}>;

export function successorDigest(domain: string, value: JsonRecord): `sha256:${string}` {
  return `sha256:${createHash("sha256")
    .update(domain)
    .update("\0")
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}

function strictValue(value: unknown): JsonRecord {
  if (Array.isArray(value)) return { type: "array", const: value };
  if (value && typeof value === "object") {
    const entries = Object.entries(value as JsonRecord);
    return {
      type: "object",
      additionalProperties: false,
      required: entries.map(([key]) => key),
      properties: Object.fromEntries(entries.map(([key, child]) => [key, strictValue(child)])),
    };
  }
  return { const: value };
}

export function successorSchema(
  value: JsonRecord,
  id: string,
  title: string,
): JsonRecord {
  return {
    $schema: "https://json-schema.org/draft/2020-12/schema",
    $id: id,
    title,
    type: "object",
    additionalProperties: false,
    required: Object.keys(value),
    properties: Object.fromEntries(Object.entries(value).map(([key, child]) => [key, strictValue(child)])),
  };
}

export function serializeSuccessor(value: JsonRecord): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function assertImmutablePredecessor(root: string, predecessor: ImmutablePredecessor): void {
  const commit = execFileSync("git", ["rev-parse", predecessor.commit], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  if (commit !== predecessor.commit) {
    throw new Error(`immutable predecessor commit mismatch: expected ${predecessor.commit}`);
  }
  const tree = execFileSync("git", ["show", "-s", "--format=%T", predecessor.commit], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  if (tree !== predecessor.tree) {
    throw new Error(`immutable predecessor tree mismatch: expected ${predecessor.tree}`);
  }
  for (const [path, expected] of Object.entries(predecessor.blobs)) {
    const actual = execFileSync("git", ["rev-parse", `${predecessor.commit}:${path}`], {
      cwd: root,
      encoding: "utf8",
    }).trim();
    if (actual !== expected) {
      throw new Error(`immutable predecessor blob mismatch: ${path}`);
    }
  }
}

export function assertSuccessorFiles(
  root: string,
  files: ReadonlyArray<readonly [string, string]>,
): void {
  for (const [path, expected] of files) {
    const output = resolve(root, path);
    const stat = lstatSync(output);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw new Error(`${path} must be a regular file and not a symlink`);
    }
    if (readFileSync(output, "utf8") !== expected) {
      throw new Error(`${path} is stale; run generator --write.`);
    }
  }
}

export function writeSuccessorFiles(
  root: string,
  files: ReadonlyArray<readonly [string, string]>,
): void {
  for (const [path, data] of files) {
    const output = resolve(root, path);
    mkdirSync(dirname(output), { recursive: true });
    try {
      const stat = lstatSync(output);
      if (!stat.isFile() || stat.isSymbolicLink()) {
        throw new Error(`${path} must not be a symlink or directory`);
      }
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    writeFileSync(output, data, { mode: 0o644 });
  }
}
