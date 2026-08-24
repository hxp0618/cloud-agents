import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  assertRunnerLedgerPreflightRegistryCurrent,
  buildRunnerLedgerPreflightRegistry,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
  serializeRunnerLedgerPreflightRegistry,
} from "./lib/platform-runner-ledger-preflight-registry";

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  mkdirSync(dirname(output), { recursive: true });
  writeFileSync(
    output,
    serializeRunnerLedgerPreflightRegistry(buildRunnerLedgerPreflightRegistry(root)),
  );
  process.stdout.write(
    `platform-runner-ledger-preflight-registry: wrote ${RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerPreflightRegistryCurrent(root);
  process.stdout.write("platform-runner-ledger-preflight-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-preflight-registry.ts --write|--check",
  );
}
