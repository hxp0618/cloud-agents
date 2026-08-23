import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertPlatformContractLockCurrent,
  buildPlatformContractLock,
  serializePlatformContractLock,
} from "./lib/platform-contract-lock";
import { writeIdentityVerifierGo } from "./lib/platform-identity-verifier-go";
import { writeIdentityVerifierRegistry } from "./lib/platform-identity-verifier-registry";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, "contracts/generation.lock.json");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeIdentityVerifierRegistry(root);
  writeIdentityVerifierGo(root);
  writeFileSync(output, serializePlatformContractLock(buildPlatformContractLock(root)));
  process.stdout.write(
    "platform-contract-lock: wrote identity verifier registry, Go profile, and contracts/generation.lock.json\n",
  );
} else if (mode === "--check") {
  assertPlatformContractLockCurrent(root);
  process.stdout.write("platform-contract-lock: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-contract-lock.ts --write|--check");
}
