import { mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  buildMigrationRunnerBinding,
  RUNNER_BINDING_GO_PATH,
  RUNNER_BINDING_PROFILE_PATH,
  RUNNER_BINDING_SOURCE_PATH,
  validateCheckedInMigrationRunnerBinding,
} from "./lib/platform-migration-runner-binding";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check") {
  throw new Error(
    "usage: bun scripts/generate-platform-migration-runner-binding.ts [--write|--check]",
  );
}
if (mode === "--write") {
  const output = buildMigrationRunnerBinding(root);
  for (const [path, bytes] of output.generatedFiles) {
    const target = resolve(root, path);
    mkdirSync(dirname(target), { recursive: true, mode: 0o755 });
    writeFileSync(target, bytes, { mode: 0o644 });
    chmodSync(target, 0o644);
  }
  process.stdout.write(`platform-migration-runner-binding: wrote ${RUNNER_BINDING_PROFILE_PATH}\n`);
} else {
  validateCheckedInMigrationRunnerBinding(root);
  process.stdout.write(
    `platform-migration-runner-binding: current ${RUNNER_BINDING_SOURCE_PATH}, ${RUNNER_BINDING_GO_PATH}\n`,
  );
}
