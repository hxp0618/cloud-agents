import { resolve } from "node:path";

import {
  assertPlatformProtoSDKCurrent,
  writePlatformProtoSDKFiles,
} from "./lib/platform-proto-sdk";

const root = resolve(import.meta.dirname, "..");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writePlatformProtoSDKFiles(root);
  process.stdout.write(
    "platform-proto-sdks: wrote descriptor, Go, Connect and TypeScript outputs\n",
  );
} else if (mode === "--check") {
  assertPlatformProtoSDKCurrent(root);
  process.stdout.write("platform-proto-sdks: current and exact-baseline compatible\n");
} else {
  throw new Error("Usage: bun scripts/generate-platform-proto-sdks.ts --write|--check");
}
