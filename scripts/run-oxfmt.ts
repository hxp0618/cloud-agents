import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const targets = [
  ".github/workflows",
  "apps",
  "deploy/helm/cloud-agents/values.yaml",
  "deploy/helm/cloud-agents/values.schema.json",
  "packages",
  "sdk/typescript",
  "scripts/check-platform-go-modules.ts",
  "scripts/cloud-agents-platform-release.ts",
  "scripts/generate-sbom.ts",
  "scripts/secret-scan.ts",
  "scripts/lib/platform-go-modules.ts",
  "scripts/lib/platform-go-modules.test.ts",
  "scripts/lib/platform-proto-sdk.ts",
  "scripts/lib/platform-proto-sdk.test.ts",
  "scripts/lib/platform-release.ts",
  "scripts/lib/platform-release.test.ts",
  "scripts/lib/platform-migration-ustar.ts",
] as const;

const [mode] = process.argv.slice(2);
if (mode !== "--check" && mode !== "--write") {
  throw new Error("Usage: bun scripts/run-oxfmt.ts --check|--write");
}
const result = spawnSync("oxfmt", [...targets, mode], { cwd: root, stdio: "inherit" });
process.exit(result.status ?? 1);
