import { resolve } from "node:path";

import {
  assertGeneratorSupplyProfileCurrent,
  writeGeneratorSupplyProfile,
} from "./lib/platform-generator-supply-profile";

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
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-generator-supply-profile.ts --write|--check",
  );
}
