import { resolve } from "node:path";

import {
  assertGeneratorSupplyV3SourceCurrent,
  inspectGeneratorSupplyV3AuthorityState,
  writeGeneratorSupplyV3Assembly,
  writeGeneratorSupplyV3Source,
} from "./lib/platform-generator-supply-profile-v3";

const root = resolve(import.meta.dirname, "..");
const [, , mode, ...argumentsAfterMode] = process.argv;

function requireArgumentCount(expected: number): void {
  if (argumentsAfterMode.length !== expected) {
    throw new Error(`Mode ${String(mode)} requires exactly ${expected} argument(s).`);
  }
}

if (mode === "--write-source") {
  requireArgumentCount(0);
  writeGeneratorSupplyV3Source(root);
  process.stdout.write("platform-generator-supply-profile: v3 source declared pre-replay\n");
} else if (mode === "--check-source" || mode === "--check") {
  requireArgumentCount(0);
  const state = assertGeneratorSupplyV3SourceCurrent(root);
  process.stdout.write(`platform-generator-supply-profile: v3 ${state}\n`);
} else if (mode === "--write-assembly") {
  requireArgumentCount(3);
  const [projection, darwinOutputDirectory, linuxOutputDirectory] = argumentsAfterMode;
  writeGeneratorSupplyV3Assembly(root, {
    projection: projection!,
    darwinOutputDirectory: darwinOutputDirectory!,
    linuxOutputDirectory: linuxOutputDirectory!,
  });
  process.stdout.write(
    "platform-generator-supply-profile: v3 exact replay receipts and assembly current; review pending\n",
  );
} else if (mode === "--check-assembly") {
  requireArgumentCount(0);
  const state = inspectGeneratorSupplyV3AuthorityState(root);
  if (state !== "ASSEMBLED_PROFILE_CURRENT") {
    throw new Error(`platform-generator-supply-profile: v3 assembly is not current (${state})`);
  }
  process.stdout.write("platform-generator-supply-profile: v3 ASSEMBLED_PROFILE_CURRENT\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-generator-supply-profile-v3.ts --write-source|--check-source|--write-assembly <projection.json> <darwin-output-dir> <linux-output-dir>|--check-assembly",
  );
}
