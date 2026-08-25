import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertDurableCoordinationGoV2Current,
  buildDurableCoordinationGoV2,
  DURABLE_COORDINATION_V2_GO_OUTPUT_PATH,
} from "./lib/platform-durable-coordination-go-v2";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check") {
  throw new Error(
    "usage: bun scripts/generate-platform-durable-coordination-go-v2.ts [--write|--check]",
  );
}

const output = resolve(root, DURABLE_COORDINATION_V2_GO_OUTPUT_PATH);
if (mode === "--write") {
  writeFileSync(output, buildDurableCoordinationGoV2(root), { mode: 0o644 });
  process.stdout.write(
    `platform-durable-coordination-go-v2: wrote ${DURABLE_COORDINATION_V2_GO_OUTPUT_PATH}\n`,
  );
} else {
  assertDurableCoordinationGoV2Current(root);
  readFileSync(output);
  process.stdout.write("platform-durable-coordination-go-v2: current\n");
}
