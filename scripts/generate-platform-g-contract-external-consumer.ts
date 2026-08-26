import { resolve } from "node:path";
import {
  assertExternalConsumerProfileCurrent,
  readExternalConsumerSource,
  writeExternalConsumerProfile,
} from "./lib/platform-g-contract-external-consumer";

const root = resolve(import.meta.dirname, "..");
const [mode, evidence] = process.argv.slice(2);
if (mode === "--check-source" && !evidence) {
  readExternalConsumerSource(root);
  process.stdout.write("g-contract-external-consumer: source current\n");
} else if (mode === "--write-profile" && evidence) {
  writeExternalConsumerProfile(root, evidence);
  process.stdout.write("g-contract-external-consumer: profile written\n");
} else if (mode === "--check-profile" && evidence) {
  assertExternalConsumerProfileCurrent(root, evidence);
  process.stdout.write("g-contract-external-consumer: profile current\n");
} else {
  throw new Error(
    "Usage: --check-source | --write-profile <evidence.json> | --check-profile <evidence.json>",
  );
}
