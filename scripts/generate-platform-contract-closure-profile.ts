import { resolve } from "node:path";

import {
  assertContractClosureProfileV3Current,
  assertContractClosureProfileV3SourceCurrent,
  CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
  writeContractClosureProfileV3,
  writeContractClosureProfileV3Source,
} from "./lib/platform-contract-closure-profile-v3";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write-source") {
  writeContractClosureProfileV3Source(root);
  process.stdout.write(
    `platform-contract-closure-profile: v1/v2 immutable; wrote ${CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH}\n`,
  );
} else if (mode === "--check-source") {
  assertContractClosureProfileV3SourceCurrent(root);
  process.stdout.write("platform-contract-closure-profile: v1/v2 immutable; v3 source current\n");
} else if (mode === "--write") {
  writeContractClosureProfileV3(root);
  process.stdout.write(
    `platform-contract-closure-profile: v1/v2 immutable; source unchanged; wrote ${CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertContractClosureProfileV3Current(root);
  process.stdout.write("platform-contract-closure-profile: v1/v2 immutable; v3 current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-closure-profile.ts --write-source|--check-source|--write|--check",
  );
}
