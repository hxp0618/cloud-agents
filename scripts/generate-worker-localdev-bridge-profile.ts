import { resolve } from "node:path";
import {
  assertWorkerLocalDevBridgeCurrent,
  writeWorkerLocalDevBridgeFiles,
  WORKER_LOCALDEV_BRIDGE_GO_PATH,
  WORKER_LOCALDEV_BRIDGE_PROFILE_PATH,
  WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH,
  WORKER_LOCALDEV_BRIDGE_SOURCE_PATH,
  WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH,
} from "./lib/worker-localdev-bridge-profile";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2] ?? "--check";
if (mode === "--write") {
  writeWorkerLocalDevBridgeFiles(root);
  process.stdout.write(
    `worker-localdev-bridge-profile: wrote ${[
      WORKER_LOCALDEV_BRIDGE_SOURCE_PATH,
      WORKER_LOCALDEV_BRIDGE_SOURCE_SCHEMA_PATH,
      WORKER_LOCALDEV_BRIDGE_PROFILE_PATH,
      WORKER_LOCALDEV_BRIDGE_PROFILE_SCHEMA_PATH,
      WORKER_LOCALDEV_BRIDGE_GO_PATH,
    ].join(", ")}\n`,
  );
} else if (mode === "--check") {
  assertWorkerLocalDevBridgeCurrent(root);
  process.stdout.write("worker-localdev-bridge-profile: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-worker-localdev-bridge-profile.ts --write|--check");
}
