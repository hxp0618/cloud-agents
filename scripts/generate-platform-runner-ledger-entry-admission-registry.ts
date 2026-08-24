import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerEntryAdmissionRegistryCurrent,
  buildRunnerLedgerEntryAdmissionRegistry,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
  serializeRunnerLedgerEntryAdmissionRegistry,
} from "./lib/platform-runner-ledger-entry-admission-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH),
    serializeRunnerLedgerEntryAdmissionRegistry(buildRunnerLedgerEntryAdmissionRegistry(root)),
  );
  process.stdout.write(
    `platform-runner-ledger-entry-admission-registry: wrote ${RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerEntryAdmissionRegistryCurrent(root);
  process.stdout.write("platform-runner-ledger-entry-admission-registry: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-entry-admission-registry.ts --write|--check",
  );
}
