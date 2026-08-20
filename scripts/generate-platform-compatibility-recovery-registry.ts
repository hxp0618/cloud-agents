import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertCompatibilityRecoveryRegistryCurrent,
  assertCompatibilityRecoveryRegistryV2Current,
  buildCompatibilityRecoveryRegistry,
  buildCompatibilityRecoveryRegistryV2,
  COMPATIBILITY_RECOVERY_OUTPUT_PATH,
  COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
  serializeCompatibilityRecoveryRegistry,
  serializeCompatibilityRecoveryRegistryV2,
} from "./lib/platform-compatibility-recovery-registry";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH);
const outputV2 = resolve(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(
    output,
    serializeCompatibilityRecoveryRegistry(buildCompatibilityRecoveryRegistry(root)),
  );
  mkdirSync(dirname(outputV2), { recursive: true });
  writeFileSync(
    outputV2,
    serializeCompatibilityRecoveryRegistryV2(buildCompatibilityRecoveryRegistryV2(root)),
  );
  process.stdout.write(
    `platform-compatibility-recovery-registry: wrote ${COMPATIBILITY_RECOVERY_OUTPUT_PATH} and ${COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertCompatibilityRecoveryRegistryCurrent(root);
  assertCompatibilityRecoveryRegistryV2Current(root);
  process.stdout.write("platform-compatibility-recovery-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-compatibility-recovery-registry.ts --write|--check",
  );
}
