import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerConsumerRegistryCurrent,
  buildRunnerLedgerConsumerRegistry,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
  serializeRunnerLedgerConsumerRegistry,
} from "./lib/platform-runner-ledger-consumer-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_CONSUMER_OUTPUT_PATH),
    serializeRunnerLedgerConsumerRegistry(buildRunnerLedgerConsumerRegistry(root)),
  );
  process.stdout.write(
    `platform-runner-ledger-consumer-registry: wrote ${RUNNER_LEDGER_CONSUMER_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerConsumerRegistryCurrent(root);
  process.stdout.write("platform-runner-ledger-consumer-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-consumer-registry.ts --write|--check",
  );
}
