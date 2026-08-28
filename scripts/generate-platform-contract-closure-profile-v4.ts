import { resolve } from "node:path";

import { assertContractClosureProfileRegistryCurrent } from "./lib/platform-contract-closure-profile";
import {
  assertContractClosureProfileV4Current,
  assertContractClosureProfileV4SourceCurrent,
  CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
  CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
  writeContractClosureProfileV4,
  writeContractClosureProfileV4Source,
} from "./lib/platform-contract-closure-profile-v4";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write-source") {
  assertContractClosureProfileRegistryCurrent(root);
  writeContractClosureProfileV4Source(root);
  process.stdout.write(
    `platform-contract-closure-profile: v1/v2 immutable and current; wrote ${CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH}\n`,
  );
} else if (mode === "--check-source") {
  assertContractClosureProfileRegistryCurrent(root);
  assertContractClosureProfileV4SourceCurrent(root);
  process.stdout.write(
    "platform-contract-closure-profile: v1/v2 immutable and current; v4 source current\n",
  );
} else if (mode === "--write") {
  assertContractClosureProfileRegistryCurrent(root);
  writeContractClosureProfileV4(root);
  process.stdout.write(
    `platform-contract-closure-profile: v1/v2 immutable and current; source unchanged; wrote ${CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertContractClosureProfileRegistryCurrent(root);
  assertContractClosureProfileV4Current(root);
  process.stdout.write(
    "platform-contract-closure-profile: v1/v2 immutable and current; v4 current\n",
  );
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-closure-profile.ts --write-source|--check-source|--write|--check",
  );
}
