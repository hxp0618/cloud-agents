import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

import { validatePlatformContractTree } from "./lib/platform-contracts";

const root = resolve(import.meta.dirname, "..");

const GENERATORS = [
  "generate-platform-durable-coordination-registry.ts",
  "generate-platform-durable-coordination-go.ts",
  "generate-foundation-coordination.ts",
  "generate-platform-compatibility-recovery-registry.ts",
  "generate-platform-compatibility-recovery-go.ts",
  "generate-platform-runner-ledger-preflight-registry.ts",
  "generate-platform-runner-ledger-consumer-registry.ts",
  "generate-platform-runner-ledger-entry-admission-registry.ts",
  "generate-platform-runner-ledger-entry-writer-registries.ts",
  "generate-platform-runner-ledger-recovery-registries.ts",
  "generate-platform-identity-sdks.ts",
  "generate-platform-json-sdks.ts",
  "generate-platform-proto-sdks.ts",
] as const;

function run(script: string, mode: "--check" | "--write"): void {
  const result = spawnSync("bun", [`scripts/${script}`, mode], {
    cwd: root,
    stdio: "inherit",
  });
  if (result.error !== undefined || result.status !== 0) process.exit(result.status ?? 1);
}

function runGenerators(mode: "--check" | "--write"): void {
  run("check-platform-ajv-official-suite.ts", mode);
  if (mode === "--check") run("check-platform-contract-standards.ts", mode);
  for (const generator of GENERATORS) run(generator, mode);
}

const [mode] = process.argv.slice(2);
if (mode === "--check" || mode === "--write") {
  runGenerators(mode);
} else {
  const summary = validatePlatformContractTree(root);
  process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);
}
