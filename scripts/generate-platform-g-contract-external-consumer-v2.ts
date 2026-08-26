import { resolve } from "node:path";

import {
  checkExternalConsumerV2Source,
  assertExternalConsumerV2ProfileAbsent,
} from "./lib/platform-g-contract-external-consumer-v2";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--check-source") {
  const result = checkExternalConsumerV2Source(root);
  assertExternalConsumerV2ProfileAbsent(root);
  process.stdout.write(
    `g-contract-external-consumer-v2: authority current; candidate=${result.candidateCommit}; tree=${result.candidateTree}; receipts absent/pending\n`,
  );
} else {
  throw new Error(
    "D-053-EC-2 authority-only runner accepts --check-source only; profile, receipt, projection, replay, and network writers are disabled",
  );
}
