import { resolve } from "node:path";

import {
  AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH,
  assertAjvOfficialSuiteAuditCurrent,
  requireAjvOfficialSuiteConformance,
  writeAjvOfficialSuiteAudit,
} from "./lib/platform-ajv-official-suite";

const root = resolve(import.meta.dirname, "..");
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeAjvOfficialSuiteAudit(root);
  process.stdout.write(
    `platform-ajv-official-suite: wrote ${AJV_OFFICIAL_SUITE_AUDIT_OUTPUT_PATH}\n`,
  );
} else if (mode === "--require-conformance") {
  assertAjvOfficialSuiteAuditCurrent(root);
  requireAjvOfficialSuiteConformance(root);
} else if (mode === undefined || mode === "--check") {
  assertAjvOfficialSuiteAuditCurrent(root);
  process.stdout.write(
    "platform-ajv-official-suite: current EXECUTED_NONCONFORMANT non-Gate audit\n",
  );
} else {
  throw new Error(
    "Usage: bun scripts/check-platform-ajv-official-suite.ts [--check|--write|--require-conformance]",
  );
}
