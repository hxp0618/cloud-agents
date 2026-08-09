import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const allowlist = JSON.parse(
  readFileSync(resolve(root, ".secret-scan-allowlist.json"), "utf8"),
) as { readonly pathPatterns: ReadonlyArray<string> };
const rules = [
  { name: "private-key", pattern: /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/u },
  { name: "aws-access-key", pattern: /\bAKIA[0-9A-Z]{16}\b/u },
  { name: "github-token", pattern: /\bgh[pousr]_[A-Za-z0-9]{20,}\b/u },
  {
    name: "assigned-secret",
    pattern:
      /(?:api[_-]?key|auth[_-]?token|access[_-]?token|client[_-]?secret|password)\s*[=:]\s*["'][A-Za-z0-9_./+=-]{16,}["']/iu,
  },
] as const;
const findings: Array<{ revision: string; path: string; line: number; rule: string }> = [];

for (const path of lines(run("git", ["ls-files", "--cached", "--others", "--exclude-standard"]))) {
  scan("worktree", path, readFileSync(resolve(root, path)));
}
for (const revision of lines(run("git", ["rev-list", "--all"]))) {
  for (const path of lines(run("git", ["ls-tree", "-r", "--name-only", revision]))) {
    const content = runBuffer("git", ["show", `${revision}:${path}`]);
    scan(revision.slice(0, 12), path, content);
  }
}

if (findings.length > 0) {
  const locations = findings
    .map(({ revision, path, line, rule }) => `${revision}:${path}:${line} (${rule})`)
    .join("\n");
  throw new Error(
    `Secret-shaped tracked content was found. Values are redacted; move synthetic cases under an explicit fixture allowlist.\n${locations}`,
  );
}
process.stdout.write("secret-scan: no unallowlisted secret-shaped tracked content found\n");

function scan(revision: string, path: string, content: Uint8Array): void {
  if (content.includes(0) || allowed(path)) return;
  const text = Buffer.from(content).toString("utf8");
  for (const [index, line] of text.split("\n").entries()) {
    for (const rule of rules) {
      if (rule.pattern.test(line)) {
        findings.push({ revision, path, line: index + 1, rule: rule.name });
      }
    }
  }
}

function allowed(path: string): boolean {
  return allowlist.pathPatterns.some((pattern) => globPattern(pattern).test(path));
}

function globPattern(pattern: string): RegExp {
  const escaped = pattern
    .replaceAll(/[.+^${}()|[\]\\]/gu, "\\$&")
    .replaceAll("**/", "\u0001")
    .replaceAll("**", "\u0002")
    .replaceAll("*", "[^/]*")
    .replaceAll("\u0001", "(?:.*/)?")
    .replaceAll("\u0002", ".*");
  return new RegExp(`^${escaped}$`, "u");
}

function run(command: string, args: ReadonlyArray<string>): string {
  return runBuffer(command, args).toString("utf8");
}

function runBuffer(command: string, args: ReadonlyArray<string>): Buffer {
  const result = spawnSync(command, [...args], { cwd: root, maxBuffer: 64 * 1024 * 1024 });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed with status ${String(result.status)}.`);
  }
  return result.stdout;
}

function lines(value: string): string[] {
  return value.split("\n").filter(Boolean);
}
