import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertPlatformContractLockCurrent,
  buildPlatformContractLock,
  serializePlatformContractLock,
} from "./lib/platform-contract-lock";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, "contracts/generation.lock.json");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeFileSync(output, serializePlatformContractLock(buildPlatformContractLock(root)));
  process.stdout.write("platform-contract-lock: wrote contracts/generation.lock.json\n");
} else if (mode === "--check") {
  assertPlatformContractLockCurrent(root);
  process.stdout.write("platform-contract-lock: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-contract-lock.ts --write|--check");
}
