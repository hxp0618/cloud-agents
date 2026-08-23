import { resolve } from "node:path";

import {
  assertIdentityVerifierGoCurrent,
  IDENTITY_VERIFIER_GO_OUTPUT_PATH,
  writeIdentityVerifierGo,
} from "./lib/platform-identity-verifier-go";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeIdentityVerifierGo(root);
  process.stdout.write(
    `platform-identity-verifier-go: wrote ${IDENTITY_VERIFIER_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertIdentityVerifierGoCurrent(root);
  process.stdout.write("platform-identity-verifier-go: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-identity-verifier-go.ts --write|--check");
}
