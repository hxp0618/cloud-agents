import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  OPEN_SANDBOX_EXECD_BUILD_EVIDENCE_FILENAME,
  buildOpenSandboxExecdSuccessor,
} from "./lib/opensandbox-execd-successor.ts";

export function main(argv = process.argv.slice(2)) {
  let outputRoot = "";
  let tag = "";
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--output-root") {
      outputRoot = argv[++index] ?? "";
    } else if (argument === "--tag") {
      tag = argv[++index] ?? "";
    } else {
      throw new Error(`unknown argument: ${argument}`);
    }
  }
  if (outputRoot === "" || tag === "") {
    throw new Error("usage: --output-root <absolute-empty-directory> --tag <local-image-tag>");
  }
  const evidence = buildOpenSandboxExecdSuccessor({ outputRoot, tag });
  process.stdout.write(
    JSON.stringify({
      evidencePath: resolve(outputRoot, OPEN_SANDBOX_EXECD_BUILD_EVIDENCE_FILENAME),
      imageId: evidence.image.id,
      immutableImageRef: evidence.image.immutableRef,
    }) + "\n",
  );
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    process.stderr.write(`execd successor build failed: ${message}\n`);
    process.exitCode = 1;
  }
}
