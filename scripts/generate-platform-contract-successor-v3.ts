import { resolve } from "node:path";

import {
  assertPlatformContractSuccessorV3Current,
  assertPlatformContractSuccessorV3ReplayAbsent,
  PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH,
  writePlatformContractSuccessorV3,
} from "./lib/platform-contract-successor-v3";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writePlatformContractSuccessorV3(root);
  process.stdout.write(
    `platform-contract-successor-v3: wrote ${PLATFORM_CONTRACT_SUCCESSOR_V3_LOCK_PATH}\n`,
  );
} else if (mode === "--check") {
  assertPlatformContractSuccessorV3Current(root);
  assertPlatformContractSuccessorV3ReplayAbsent(root);
  process.stdout.write("platform-contract-successor-v3: current\n");
} else if (mode === "--check-document") {
  assertPlatformContractSuccessorV3Current(root);
  assertPlatformContractSuccessorV3ReplayAbsent(root);
  process.stdout.write("platform-contract-successor-v3: document current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-successor-v3.ts --write|--check|--check-document",
  );
}
