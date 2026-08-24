import { resolve } from "node:path";

import { validatePlatformContractTree } from "./lib/platform-contracts";

const root = resolve(import.meta.dirname, "..");
const summary = validatePlatformContractTree(root);
process.stdout.write(`${JSON.stringify(summary, null, 2)}\n`);
