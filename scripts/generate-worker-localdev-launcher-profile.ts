import { resolve } from "node:path";
import {
  assertWorkerLocalDevLauncherCurrent,
  writeWorkerLocalDevLauncherFiles,
  WORKER_LOCALDEV_LAUNCHER_GO_PATH,
  WORKER_LOCALDEV_LAUNCHER_PROFILE_PATH,
  WORKER_LOCALDEV_LAUNCHER_PROFILE_SCHEMA_PATH,
  WORKER_LOCALDEV_LAUNCHER_SOURCE_PATH,
  WORKER_LOCALDEV_LAUNCHER_SOURCE_SCHEMA_PATH,
} from "./lib/worker-localdev-launcher-profile";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode === "--write") {
  writeWorkerLocalDevLauncherFiles(root);
  process.stdout.write(
    `worker-localdev-launcher-profile: wrote ${[
      WORKER_LOCALDEV_LAUNCHER_SOURCE_PATH,
      WORKER_LOCALDEV_LAUNCHER_SOURCE_SCHEMA_PATH,
      WORKER_LOCALDEV_LAUNCHER_PROFILE_PATH,
      WORKER_LOCALDEV_LAUNCHER_PROFILE_SCHEMA_PATH,
      WORKER_LOCALDEV_LAUNCHER_GO_PATH,
    ].join(", ")}\n`,
  );
} else if (mode === "--check") {
  assertWorkerLocalDevLauncherCurrent(root);
  process.stdout.write("worker-localdev-launcher-profile: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-worker-localdev-launcher-profile.ts --write|--check",
  );
}
