import { spawnSync } from "node:child_process";

export const PLATFORM_OXFMT_LIBRARY_PATH = "scripts/lib/platform-oxfmt.ts";
export const PLATFORM_OXFMT_TEST_PATH = "scripts/lib/platform-oxfmt.test.ts";

const OXFMT_IN_PROCESS_DRIVER = String.raw`
import { format } from "oxfmt";

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const result = await format(process.argv[1], Buffer.concat(chunks).toString("utf8"));
if (result.errors.length > 0) {
  process.stderr.write(JSON.stringify(result.errors));
  process.exit(1);
}
process.stdout.write(result.code);
`;

export function formatWithOxfmt(root: string, path: string, source: string): string {
  const runtime = process.env.CLOUD_AGENTS_NODE ?? process.execPath;
  const result = spawnSync(runtime, ["--input-type=module", "-e", OXFMT_IN_PROCESS_DRIVER, path], {
    cwd: root,
    encoding: "utf8",
    input: source,
    maxBuffer: 16 * 1024 * 1024,
  });
  if (result.error !== undefined || result.status !== 0) {
    throw new Error(
      `Formatter failed for ${path}: ${(result.error?.message ?? result.stderr ?? result.stdout).trim()}`,
    );
  }
  return result.stdout;
}
