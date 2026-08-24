import { resolve } from "node:path";

import {
  assertIdentityVerifierRegistryCurrent,
  IDENTITY_VERIFIER_OUTPUT_PATH,
  writeIdentityVerifierRegistry,
} from "./lib/platform-identity-verifier-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeIdentityVerifierRegistry(root);
  process.stdout.write(
    `platform-identity-verifier-registry: wrote ${IDENTITY_VERIFIER_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertIdentityVerifierRegistryCurrent(root);
  process.stdout.write("platform-identity-verifier-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-identity-verifier-registry.ts --write|--check",
  );
}
