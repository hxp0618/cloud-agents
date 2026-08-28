import { resolve } from "node:path";

import {
  assertWorkerSupervisorLocalDispatchCurrent,
  LOCAL_DISPATCH_GO_PATH,
  LOCAL_DISPATCH_PROFILE_PATH,
  LOCAL_DISPATCH_PROFILE_SCHEMA_PATH,
  LOCAL_DISPATCH_SOURCE_PATH,
  LOCAL_DISPATCH_SOURCE_SCHEMA_PATH,
  writeWorkerSupervisorLocalDispatchFiles,
} from "./lib/worker-supervisor-local-dispatch-profile";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode === "--write") {
  writeWorkerSupervisorLocalDispatchFiles(root);
  process.stdout.write(
    `worker-supervisor-local-dispatch-profile: wrote ${[
      LOCAL_DISPATCH_SOURCE_PATH,
      LOCAL_DISPATCH_SOURCE_SCHEMA_PATH,
      LOCAL_DISPATCH_PROFILE_PATH,
      LOCAL_DISPATCH_PROFILE_SCHEMA_PATH,
      LOCAL_DISPATCH_GO_PATH,
    ].join(", ")}\n`,
  );
} else if (mode === "--check") {
  assertWorkerSupervisorLocalDispatchCurrent(root);
  process.stdout.write("worker-supervisor-local-dispatch-profile: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-worker-supervisor-local-dispatch-profile.ts --write|--check",
  );
}
