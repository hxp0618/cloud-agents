import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent,
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent,
  buildRunnerLedgerEntryExecutionAdmissionRegistry,
  buildRunnerLedgerEntrySuccessWriterRegistry,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
  serializeRunnerLedgerEntryWriterRegistry,
} from "./lib/platform-runner-ledger-entry-writer-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH),
    serializeRunnerLedgerEntryWriterRegistry(
      buildRunnerLedgerEntryExecutionAdmissionRegistry(root),
    ),
  );
  writeFileSync(
    resolve(root, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH),
    serializeRunnerLedgerEntryWriterRegistry(buildRunnerLedgerEntrySuccessWriterRegistry(root)),
  );
  process.stdout.write(
    `platform-runner-ledger-entry-writer-registries: wrote ${RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH} and ${RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root);
  process.stdout.write("platform-runner-ledger-entry-writer-registries: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-entry-writer-registries.ts --write|--check",
  );
}
