import { writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerRecoveryRegistriesCurrent,
  buildRunnerLedgerRecoveryRegistries,
  RUNNER_LEDGER_RECOVERY_FAMILIES,
  RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS,
  serializeRunnerLedgerRecoveryRegistry,
} from "./lib/platform-runner-ledger-recovery-registry";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  for (const [index, registry] of buildRunnerLedgerRecoveryRegistries(root).entries()) {
    const family = RUNNER_LEDGER_RECOVERY_FAMILIES[index];
    if (family === undefined) throw new Error("Runner ledger recovery family is missing.");
    writeFileSync(
      resolve(root, RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family]),
      serializeRunnerLedgerRecoveryRegistry(registry),
    );
  }
  process.stdout.write("platform-runner-ledger-recovery-registries: wrote 8 registries\n");
} else if (mode === "--check") {
  assertRunnerLedgerRecoveryRegistriesCurrent(root);
  process.stdout.write("platform-runner-ledger-recovery-registries: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-recovery-registries.ts --write|--check",
  );
}
