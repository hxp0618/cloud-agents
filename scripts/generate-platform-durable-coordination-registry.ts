import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertDurableCoordinationRegistryCurrent,
  buildDurableCoordinationRegistry,
  DURABLE_COORDINATION_OUTPUT_PATH,
  serializeDurableCoordinationRegistry,
} from "./lib/platform-durable-coordination-registry";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, DURABLE_COORDINATION_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(
    output,
    serializeDurableCoordinationRegistry(buildDurableCoordinationRegistry(root)),
  );
  process.stdout.write(
    `platform-durable-coordination-registry: wrote ${DURABLE_COORDINATION_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertDurableCoordinationRegistryCurrent(root);
  process.stdout.write("platform-durable-coordination-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-durable-coordination-registry.ts --write|--check",
  );
}
