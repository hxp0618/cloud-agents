import { resolve } from "node:path";

import {
  assertManagedAgentLocalExecutionCurrent,
  MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
  writeManagedAgentLocalExecutionFiles,
} from "./lib/managed-agent-local-execution-profile";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";

if (mode === "--write") {
  writeManagedAgentLocalExecutionFiles(root);
  process.stdout.write(
    `managed-agent-local-execution-profile: wrote ${[
      MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH,
      MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
      MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH,
      MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
      MANAGED_AGENT_LOCAL_EXECUTION_GO_PATH,
    ].join(", ")}\n`,
  );
} else if (mode === "--check") {
  assertManagedAgentLocalExecutionCurrent(root);
  process.stdout.write("managed-agent-local-execution-profile: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-managed-agent-local-execution-profile.ts --write|--check",
  );
}
