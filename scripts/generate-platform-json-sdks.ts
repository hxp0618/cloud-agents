import { resolve } from "node:path";

import { assertPlatformJSONSDKCurrent, writePlatformJSONSDKFiles } from "./lib/platform-json-sdk";

const root = resolve(import.meta.dirname, "..");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writePlatformJSONSDKFiles(root);
  process.stdout.write("platform-json-sdks: wrote Go and TypeScript JSON contract outputs\n");
} else if (mode === "--check") {
  assertPlatformJSONSDKCurrent(root);
  process.stdout.write("platform-json-sdks: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-json-sdks.ts --write|--check");
}
