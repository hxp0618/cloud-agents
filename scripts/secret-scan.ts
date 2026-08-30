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
const gitGrepPattern = String.raw`-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|(^|[^[:alnum:]_])AKIA[0-9A-Z]{16}([^[:alnum:]_]|$)|(^|[^[:alnum:]_])gh[pousr]_[A-Za-z0-9]{20,}([^[:alnum:]_]|$)|(api[_-]?key|auth[_-]?token|access[_-]?token|client[_-]?secret|password)[[:space:]]*[=:][[:space:]]*["'][A-Za-z0-9_./+=-]{16,}["']`;
const findings: Array<{ revision: string; path: string; line: number; rule: string }> = [];

for (const path of lines(run("git", ["ls-files", "--cached", "--others", "--exclude-standard"]))) {
  scan("worktree", path, readFileSync(resolve(root, path)));
}
const revisions = secretScanRevisions();
// ponytail: one argv entry per revision; batch the history if it ever approaches ARG_MAX.
scanGitGrep(
  runGitGrep(["grep", "-z", "-n", "-I", "-E", "-i", "-e", gitGrepPattern, ...revisions, "--"]),
);

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
    scanLine(revision, path, index + 1, line);
  }
}

function scanGitGrep(output: Buffer): void {
  let offset = 0;
  while (offset < output.length) {
    const pathEnd = output.indexOf(0, offset);
    const lineEnd = output.indexOf(0, pathEnd + 1);
    const contentEnd = output.indexOf(10, lineEnd + 1);
    if (pathEnd < 0 || lineEnd < 0 || contentEnd < 0) {
      throw new Error("git grep returned malformed NUL-delimited output.");
    }
    const source = output.subarray(offset, pathEnd).toString("utf8");
    const separator = source.indexOf(":");
    const lineNumber = Number(output.subarray(pathEnd + 1, lineEnd).toString("ascii"));
    if (separator !== 40 || !Number.isSafeInteger(lineNumber) || lineNumber <= 0) {
      throw new Error("git grep returned an invalid revision, path, or line number.");
    }
    const revision = source.slice(0, separator);
    const path = source.slice(separator + 1);
    if (!allowed(path)) {
      scanLine(
        revision.slice(0, 12),
        path,
        lineNumber,
        output.subarray(lineEnd + 1, contentEnd).toString("utf8"),
      );
    }
    offset = contentEnd + 1;
  }
}

function scanLine(revision: string, path: string, line: number, content: string): void {
  for (const rule of rules) {
    if (rule.pattern.test(content)) findings.push({ revision, path, line, rule: rule.name });
  }
}

function allowed(path: string): boolean {
  return allowlist.pathPatterns.some((pattern) => globPattern(pattern).test(path));
}

function secretScanRevisions(): string[] {
  const head = run("git", ["rev-parse", "HEAD"]).trim();
  const base = process.env.CLOUD_AGENT_SECRET_SCAN_BASE?.trim();
  if (!base || /^0+$/u.test(base)) return [head];
  if (!/^[0-9a-f]{40}$/u.test(base)) {
    throw new Error("CLOUD_AGENT_SECRET_SCAN_BASE must be a full commit SHA.");
  }
  const revisions = lines(run("git", ["rev-list", `${base}..${head}`]));
  return revisions.length === 0 ? [head] : revisions;
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

function runGitGrep(args: ReadonlyArray<string>): Buffer {
  const result = spawnSync("git", [...args], { cwd: root, maxBuffer: 64 * 1024 * 1024 });
  if (result.status !== 0 && result.status !== 1) {
    throw new Error(
      `git grep failed with status ${String(result.status)}: ${result.stderr.toString("utf8").trim()}`,
    );
  }
  return result.stdout;
}

function lines(value: string): string[] {
  return value.split("\n").filter(Boolean);
}
