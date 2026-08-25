import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertDurableCoordinationRegistryV2Current,
  buildDurableCoordinationRegistryV2,
  DURABLE_COORDINATION_V2_OUTPUT_PATH,
  serializeDurableCoordinationRegistryV2,
} from "./lib/platform-durable-coordination-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check") {
  throw new Error(
    "usage: bun scripts/generate-platform-durable-coordination-registry-v2.ts [--write|--check]",
  );
}

if (mode === "--write") {
  const output = resolve(root, DURABLE_COORDINATION_V2_OUTPUT_PATH);
  mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
  writeFileSync(
    output,
    serializeDurableCoordinationRegistryV2(buildDurableCoordinationRegistryV2(root)),
    { mode: 0o644 },
  );
  process.stdout.write(
    `platform-durable-coordination-registry-v2: wrote ${DURABLE_COORDINATION_V2_OUTPUT_PATH}\n`,
  );
} else {
  // Keep currentness in the library so callers cannot validate only the JSON
  // shape and miss predecessor/route bindings.
  assertDurableCoordinationRegistryV2Current(root);
  // Fail closed if the output is missing or not readable as a regular file.
  readFileSync(resolve(root, DURABLE_COORDINATION_V2_OUTPUT_PATH));
  process.stdout.write("platform-durable-coordination-registry-v2: current\n");
}
