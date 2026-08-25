import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertDurableProjectCreateLineageV2Current,
  buildDurableProjectCreateLineageV2,
  DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH,
  serializeDurableProjectCreateLineageV2,
} from "./lib/platform-durable-project-create-lineage-v2";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode !== "--write" && mode !== "--check") {
  throw new Error(
    "usage: bun scripts/generate-platform-durable-project-create-lineage-v2.ts [--write|--check]",
  );
}

const output = resolve(root, DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH);
if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true, mode: 0o755 });
  writeFileSync(
    output,
    serializeDurableProjectCreateLineageV2(buildDurableProjectCreateLineageV2(root)),
    { mode: 0o644 },
  );
  process.stdout.write(
    `platform-durable-project-create-lineage-v2: wrote ${DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH}\n`,
  );
} else {
  assertDurableProjectCreateLineageV2Current(root);
  readFileSync(output);
  process.stdout.write("platform-durable-project-create-lineage-v2: current\n");
}
