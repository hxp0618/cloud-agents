import { chmodSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  buildDurableProjectCreateMigrationSuccessor,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH,
  validateCheckedInDurableProjectCreateMigrationSuccessor,
} from "./lib/platform-migration-bundle-successor";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check") {
  throw new Error(
    "usage: bun scripts/generate-platform-migration-bundle-successor.ts [--write|--check]",
  );
}

if (mode === "--write") {
  const successor = buildDurableProjectCreateMigrationSuccessor(root);
  for (const [path, bytes] of successor.generatedFiles) {
    const output = resolve(root, path);
    mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
    writeFileSync(output, bytes, { mode: 0o644 });
    chmodSync(output, 0o644);
  }
  process.stdout.write(
    `platform-migration-bundle-successor: wrote ${DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH}\n`,
  );
} else {
  const successor = validateCheckedInDurableProjectCreateMigrationSuccessor(root);
  readFileSync(resolve(root, DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH));
  process.stdout.write(
    `platform-migration-bundle-successor: current head=${String(successor.manifest.schema_bundle && (successor.manifest.schema_bundle as Record<string, unknown>).schema_head)}\n`,
  );
}
