import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  buildMigrationBundle,
  validateCheckedInMigrationBundle,
} from "./lib/platform-migration-bundle";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (!new Set(["--check", "--write"]).has(mode)) {
  throw new Error("usage: bun scripts/generate-platform-migration-bundle.ts [--check|--write]");
}

if (mode === "--write") {
  const bundle = buildMigrationBundle(root);
  for (const [path, bytes] of bundle.files) {
    const output = resolve(root, path);
    mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
    writeFileSync(output, bytes, { mode: 0o644 });
  }
  process.stdout.write(`platform-migration-bundle: wrote ${bundle.files.size} generated files\n`);
} else {
  validateCheckedInMigrationBundle(root);
  process.stdout.write("platform-migration-bundle: current\n");
}
