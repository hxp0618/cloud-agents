import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertCompatibilityRecoveryRegistryCurrent,
  buildCompatibilityRecoveryRegistry,
  COMPATIBILITY_RECOVERY_OUTPUT_PATH,
  serializeCompatibilityRecoveryRegistry,
} from "./lib/platform-compatibility-recovery-registry";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(
    output,
    serializeCompatibilityRecoveryRegistry(buildCompatibilityRecoveryRegistry(root)),
  );
  process.stdout.write(
    `platform-compatibility-recovery-registry: wrote ${COMPATIBILITY_RECOVERY_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertCompatibilityRecoveryRegistryCurrent(root);
  process.stdout.write("platform-compatibility-recovery-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-compatibility-recovery-registry.ts --write|--check",
  );
}
