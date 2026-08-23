import { resolve } from "node:path";

import {
  assertContractClosureProfileRegistryCurrent,
  CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH,
  writeContractClosureProfileRegistry,
} from "./lib/platform-contract-closure-profile";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeContractClosureProfileRegistry(root);
  process.stdout.write(
    `platform-contract-closure-profile: wrote ${CONTRACT_CLOSURE_PROFILE_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertContractClosureProfileRegistryCurrent(root);
  process.stdout.write("platform-contract-closure-profile: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-closure-profile.ts --write|--check",
  );
}
