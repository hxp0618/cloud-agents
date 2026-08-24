import { resolve } from "node:path";

import {
  assertContractReviewBindingCurrentOrAbsent,
  assertContractReviewBindingSourceCurrent,
  inspectContractReviewBindingState,
  writeContractReviewBinding,
  writeContractReviewBindingSource,
} from "./lib/platform-contract-review-binding";

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write-source") {
  writeContractReviewBindingSource(root);
  process.stdout.write("platform-contract-review-binding-source: CURRENT\n");
} else if (mode === "--check-source") {
  assertContractReviewBindingSourceCurrent(root);
  process.stdout.write("platform-contract-review-binding-source: CURRENT\n");
} else if (mode === "--write") {
  writeContractReviewBinding(root);
  const state = inspectContractReviewBindingState(root);
  process.stdout.write(`platform-contract-review-binding: ${state.kind}\n`);
} else if (mode === "--check") {
  assertContractReviewBindingCurrentOrAbsent(root);
  const state = inspectContractReviewBindingState(root);
  process.stdout.write(`platform-contract-review-binding: ${state.kind}\n`);
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-contract-review-binding.ts --write-source|--check-source|--write|--check",
  );
}
