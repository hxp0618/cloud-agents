import { resolve } from "node:path";

import {
  assertExternalConsumerV3ReplayAbsent,
  assertExternalConsumerV3SourceCurrent,
  EXTERNAL_CONSUMER_V3_SOURCE_PATH,
  writeExternalConsumerV3Source,
} from "./lib/platform-g-contract-external-consumer-v3";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write-source") {
  writeExternalConsumerV3Source(root);
  process.stdout.write(
    `g-contract-external-consumer-v3: wrote ${EXTERNAL_CONSUMER_V3_SOURCE_PATH}\n`,
  );
} else if (mode === "--check-source") {
  assertExternalConsumerV3SourceCurrent(root);
  assertExternalConsumerV3ReplayAbsent(root);
  process.stdout.write("g-contract-external-consumer-v3: authority current; replay pending\n");
} else if (mode === "--check-document") {
  assertExternalConsumerV3SourceCurrent(root);
  assertExternalConsumerV3ReplayAbsent(root);
  process.stdout.write("g-contract-external-consumer-v3: document current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-g-contract-external-consumer-v3.ts --write-source|--check-source|--check-document",
  );
}
