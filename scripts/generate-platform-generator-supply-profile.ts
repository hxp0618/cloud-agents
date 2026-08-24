import { resolve } from "node:path";

import {
  assertGeneratorSupplyProfileCurrent,
  writeGeneratorSupplyProfile,
} from "./lib/platform-generator-supply-profile";
import {
  assertGeneratorSupplyV2SourceCurrent,
  writeGeneratorSupplyV2Source,
} from "./lib/platform-generator-supply-profile-v2";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeGeneratorSupplyProfile(root);
  process.stdout.write(
    "platform-generator-supply-profile: wrote raw-derived replay summary, v1 evidence manifest and profile\n",
  );
} else if (mode === "--check") {
  assertGeneratorSupplyProfileCurrent(root);
  process.stdout.write("platform-generator-supply-profile: v1 current; review pending\n");
} else if (mode === "--write-v2-source") {
  writeGeneratorSupplyV2Source(root);
  process.stdout.write(
    "platform-generator-supply-profile: v2 source declared pre-replay; no assembly written\n",
  );
} else if (mode === "--check-v2") {
  const state = assertGeneratorSupplyV2SourceCurrent(root);
  process.stdout.write(`platform-generator-supply-profile: v2 ${state}\n`);
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-generator-supply-profile.ts --write|--check|--write-v2-source|--check-v2",
  );
}
