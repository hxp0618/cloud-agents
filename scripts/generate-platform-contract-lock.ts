import { resolve } from "node:path";

import {
  assertPlatformContractLockCurrent,
  assertPlatformSuccessorContractLockCurrent,
  buildPlatformContractLock,
  buildPlatformSuccessorContractLock,
  writePlatformContractLockDocument,
} from "./lib/platform-contract-lock";

const root = resolve(import.meta.dirname, "..");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writePlatformContractLockDocument(root, buildPlatformContractLock(root));
  process.stdout.write("platform-contract-lock: wrote contracts/generation.lock.json only\n");
} else if (mode === "--check") {
  assertPlatformContractLockCurrent(root);
  process.stdout.write("platform-contract-lock: current\n");
} else if (mode === "--write-successor") {
  writePlatformContractLockDocument(root, buildPlatformSuccessorContractLock(root));
  process.stdout.write(
    "platform-contract-lock: wrote versioned successor contracts/generation.lock.json only\n",
  );
} else if (mode === "--check-successor") {
  assertPlatformSuccessorContractLockCurrent(root);
  process.stdout.write("platform-contract-lock: versioned successor current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-lock.ts --write|--check|--write-successor|--check-successor",
  );
}
