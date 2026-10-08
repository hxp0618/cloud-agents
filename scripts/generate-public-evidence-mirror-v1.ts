import { resolve } from "node:path";

import {
  assertPublicEvidenceMirrorV1Current,
  PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH,
  PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH,
  writePublicEvidenceMirrorV1,
} from "./lib/public-evidence-mirror-v1";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writePublicEvidenceMirrorV1(root);
  process.stdout.write(
    `public-evidence-mirror-v1: wrote ${PUBLIC_EVIDENCE_MIRROR_V1_OUTPUT_PATH} and ${PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH}\n`,
  );
} else if (mode === "--check") {
  assertPublicEvidenceMirrorV1Current(root);
  process.stdout.write("public-evidence-mirror-v1: current\n");
} else {
  throw new Error("Usage: bun scripts/generate-public-evidence-mirror-v1.ts --write|--check");
}
