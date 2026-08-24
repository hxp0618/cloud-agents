import { resolve } from "node:path";

import { assertIdentitySDKCurrent, writeIdentitySDKFiles } from "./lib/platform-identity-sdk";

const root = resolve(import.meta.dirname, "..");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeIdentitySDKFiles(root);
  process.stdout.write("platform-identity-sdks: wrote Go and TypeScript identity outputs\n");
} else if (mode === "--check") {
  assertIdentitySDKCurrent(root);
  process.stdout.write("platform-identity-sdks: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-identity-sdks.ts --write|--check");
}
