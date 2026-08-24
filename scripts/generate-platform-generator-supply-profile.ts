import { resolve } from "node:path";

import {
  assertGeneratorSupplyProfileCurrent,
  writeGeneratorSupplyProfile,
} from "./lib/platform-generator-supply-profile";
import {
  assertGeneratorSupplyV2SourceCurrent,
  inspectGeneratorSupplyV2AuthorityState,
  writeGeneratorSupplyV2Assembly,
  writeGeneratorSupplyV2Source,
} from "./lib/platform-generator-supply-profile-v2";

const root = resolve(import.meta.dirname, "..");
const [, , mode, ...argumentsAfterMode] = process.argv;

function requireArgumentCount(expected: number): void {
  if (argumentsAfterMode.length !== expected) {
    throw new Error(`Mode ${String(mode)} requires exactly ${expected} argument(s).`);
  }
}

if (mode === "--write") {
  requireArgumentCount(0);
  writeGeneratorSupplyProfile(root);
  process.stdout.write(
    "platform-generator-supply-profile: wrote raw-derived replay summary, v1 evidence manifest and profile\n",
  );
} else if (mode === "--check") {
  requireArgumentCount(0);
  assertGeneratorSupplyProfileCurrent(root);
  process.stdout.write("platform-generator-supply-profile: v1 current; review pending\n");
} else if (mode === "--write-v2-source") {
  requireArgumentCount(0);
  writeGeneratorSupplyV2Source(root);
  process.stdout.write(
    "platform-generator-supply-profile: v2 source declared pre-replay; no assembly written\n",
  );
} else if (mode === "--check-v2") {
  requireArgumentCount(0);
  const state = assertGeneratorSupplyV2SourceCurrent(root);
  process.stdout.write(`platform-generator-supply-profile: v2 ${state}\n`);
} else if (mode === "--write-v2-assembly") {
  requireArgumentCount(3);
  const [projection, darwinOutputDirectory, linuxOutputDirectory] = argumentsAfterMode;
  writeGeneratorSupplyV2Assembly(root, {
    projection: projection!,
    darwinOutputDirectory: darwinOutputDirectory!,
    linuxOutputDirectory: linuxOutputDirectory!,
  });
  process.stdout.write(
    "platform-generator-supply-profile: v2 exact replay receipts and assembly current; review pending\n",
  );
} else if (mode === "--check-v2-assembly") {
  requireArgumentCount(0);
  const state = inspectGeneratorSupplyV2AuthorityState(root);
  if (state !== "ASSEMBLED_PROFILE_CURRENT") {
    throw new Error(`platform-generator-supply-profile: v2 assembly is not current (${state})`);
  }
  process.stdout.write("platform-generator-supply-profile: v2 ASSEMBLED_PROFILE_CURRENT\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-generator-supply-profile.ts --write|--check|--write-v2-source|--check-v2|--write-v2-assembly <projection.json> <darwin-output-dir> <linux-output-dir>|--check-v2-assembly",
  );
}
