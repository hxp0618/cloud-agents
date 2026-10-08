import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync, readlinkSync } from "node:fs";
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
type Finding = { revision: string; path: string; line: number; rule: string };
type WorktreeCoverage = {
  tracked: number;
  untracked: number;
  enumerated: number;
  text: number;
  allowlisted: number;
  binary: number;
  deleted: number;
};
type HistoryCoverage = {
  mode: "head" | "range" | "all-history";
  reachableCommits: number | null;
  pickaxeCandidates: number | null;
  scannedRevisionTrees: number;
};
export type SecretScanResult = {
  findings: Finding[];
  coverage: { worktree: WorktreeCoverage; history: HistoryCoverage };
};
type SecretScanOptions = {
  readonly allHistory?: boolean;
  readonly base?: string;
  readonly allowlistPatterns?: ReadonlyArray<string>;
};

function main(): void {
  const result = scanSecretRepository(root, {
    allHistory: process.env.CLOUD_AGENT_SECRET_SCAN_ALL_HISTORY === "1",
    base: process.env.CLOUD_AGENT_SECRET_SCAN_BASE,
  });
  process.stdout.write(`${formatSecretScanResult(result)}\n`);
  if (result.findings.length > 0) process.exitCode = 1;
}

export function scanSecretRepository(
  repositoryRoot: string,
  options: SecretScanOptions = {},
): SecretScanResult {
  const findings: Finding[] = [];
  const isAllowed = allowlistMatcher(options.allowlistPatterns ?? allowlist.pathPatterns);
  const worktree = scanSecretWorktreeWithCoverage(repositoryRoot, findings, isAllowed);
  const revisionSelection = secretScanRevisions(repositoryRoot, options);
  // ponytail: one argv entry per revision; batch the history if it ever approaches ARG_MAX.
  scanGitGrep(
    runGitGrep(
      [
        "grep",
        "-z",
        "-n",
        "-I",
        "-E",
        "-i",
        "-e",
        gitGrepPattern,
        ...revisionSelection.revisions,
        "--",
      ],
      repositoryRoot,
    ),
    findings,
    isAllowed,
  );
  return {
    findings,
    coverage: {
      worktree,
      history: revisionSelection.coverage,
    },
  };
}

export function formatSecretScanResult(result: SecretScanResult): string {
  const coverage = `secret-scan coverage: ${JSON.stringify(result.coverage)}`;
  if (result.findings.length > 0) return `${coverage}\n${formatFindings(result.findings)}`;
  return `${coverage}\nsecret-scan: no unallowlisted secret-shaped repository content found`;
}

export function scanSecretWorktree(repositoryRoot: string): Finding[] {
  const findings: Finding[] = [];
  scanSecretWorktreeWithCoverage(
    repositoryRoot,
    findings,
    allowlistMatcher(allowlist.pathPatterns),
  );
  return findings;
}

function scanSecretWorktreeWithCoverage(
  repositoryRoot: string,
  findings: Finding[],
  isAllowed: (path: string) => boolean,
): WorktreeCoverage {
  const trackedPaths = nulRecords(runBuffer("git", ["ls-files", "-z", "--cached"], repositoryRoot));
  const untrackedPaths = nulRecords(
    runBuffer("git", ["ls-files", "-z", "--others", "--exclude-standard"], repositoryRoot),
  );
  const paths = [...trackedPaths, ...untrackedPaths];
  const coverage: WorktreeCoverage = {
    tracked: trackedPaths.length,
    untracked: untrackedPaths.length,
    enumerated: paths.length,
    text: 0,
    allowlisted: 0,
    binary: 0,
    deleted: 0,
  };
  for (const path of paths) {
    const absolutePath = resolve(repositoryRoot, path);
    const stat = lstatSync(absolutePath, { throwIfNoEntry: false });
    if (!stat) {
      coverage.deleted += 1; // Deleted tracked files remain covered by the revision scan.
      continue;
    }
    let content: Buffer;
    if (stat.isSymbolicLink()) {
      content = Buffer.from(readlinkSync(absolutePath));
    } else if (stat.isFile()) {
      content = readFileSync(absolutePath);
    } else {
      throw new Error(`secret-scan cannot inspect non-file worktree entry: ${path}`);
    }
    if (isAllowed(path)) {
      coverage.allowlisted += 1;
      continue;
    }
    if (content.includes(0)) {
      coverage.binary += 1;
      continue;
    }
    coverage.text += 1;
    scanText("worktree", path, content, findings);
  }
  return coverage;
}

function scanText(revision: string, path: string, content: Uint8Array, findings: Finding[]): void {
  const text = Buffer.from(content).toString("utf8");
  for (const [index, line] of text.split("\n").entries()) {
    scanLine(revision, path, index + 1, line, findings);
  }
}

function scanGitGrep(
  output: Buffer,
  findings: Finding[],
  isAllowed: (path: string) => boolean,
): void {
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
    if (!isAllowed(path)) {
      scanLine(
        revision.slice(0, 12),
        path,
        lineNumber,
        output.subarray(lineEnd + 1, contentEnd).toString("utf8"),
        findings,
      );
    }
    offset = contentEnd + 1;
  }
}

function scanLine(
  revision: string,
  path: string,
  line: number,
  content: string,
  findings: Finding[],
): void {
  for (const rule of rules) {
    if (rule.pattern.test(content)) findings.push({ revision, path, line, rule: rule.name });
  }
}

function allowlistMatcher(patterns: ReadonlyArray<string>): (path: string) => boolean {
  const matchers = patterns.map(globPattern);
  return (path) => matchers.some((pattern) => pattern.test(path));
}

export function selectSecretScanRevisions(input: {
  readonly head: string;
  readonly base?: string;
  readonly allHistory?: boolean;
  readonly candidates?: ReadonlyArray<string>;
  readonly range?: ReadonlyArray<string>;
}): string[] {
  const base = input.base?.trim();
  if (input.allHistory) return [...new Set([input.head, ...(input.candidates ?? [])])];
  if (!base || /^0+$/u.test(base)) return [input.head];
  validateBase(base);
  const revisions = [...(input.range ?? [])];
  return revisions.length === 0 ? [input.head] : revisions;
}

function secretScanRevisions(
  repositoryRoot: string,
  options: SecretScanOptions,
): { revisions: string[]; coverage: HistoryCoverage } {
  const head = run("git", ["rev-parse", "HEAD"], repositoryRoot).trim();
  if (options.allHistory) {
    // Scan commits that introduce or remove a matching line; the first such
    // commit covers every reachable historical occurrence without walking
    // every unchanged tree in the repository.
    const candidates = lines(
      run(
        "git",
        [
          "log",
          "--all",
          "--full-history",
          "--root",
          "-m",
          "--regexp-ignore-case",
          "--format=%H",
          "--no-renames",
          `-G${gitGrepPattern}`,
          "--",
        ],
        repositoryRoot,
      ),
    );
    const uniqueCandidates = [...new Set(candidates)];
    const revisions = selectSecretScanRevisions({
      head,
      allHistory: true,
      candidates: uniqueCandidates,
    });
    return {
      revisions,
      coverage: {
        mode: "all-history",
        reachableCommits: parseCount(
          run("git", ["rev-list", "--all", "--count"], repositoryRoot),
          "reachable commit",
        ),
        pickaxeCandidates: uniqueCandidates.length,
        scannedRevisionTrees: revisions.length,
      },
    };
  }
  const base = options.base?.trim();
  if (!base || /^0+$/u.test(base)) {
    return {
      revisions: [head],
      coverage: {
        mode: "head",
        reachableCommits: null,
        pickaxeCandidates: null,
        scannedRevisionTrees: 1,
      },
    };
  }
  validateBase(base);
  const revisions = selectSecretScanRevisions({
    head,
    base,
    range: lines(run("git", ["rev-list", `${base}..${head}`], repositoryRoot)),
  });
  return {
    revisions,
    coverage: {
      mode: "range",
      reachableCommits: null,
      pickaxeCandidates: null,
      scannedRevisionTrees: revisions.length,
    },
  };
}

function validateBase(base: string): void {
  if (!/^[0-9a-f]{40}$/u.test(base)) {
    throw new Error("CLOUD_AGENT_SECRET_SCAN_BASE must be a full commit SHA.");
  }
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

function run(command: string, args: ReadonlyArray<string>, cwd = root): string {
  return runBuffer(command, args, cwd).toString("utf8");
}

function runBuffer(command: string, args: ReadonlyArray<string>, cwd = root): Buffer {
  const result = spawnSync(command, [...args], { cwd, maxBuffer: 64 * 1024 * 1024 });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed with status ${String(result.status)}.`);
  }
  return result.stdout;
}

function runGitGrep(args: ReadonlyArray<string>, cwd = root): Buffer {
  const result = spawnSync("git", [...args], { cwd, maxBuffer: 64 * 1024 * 1024 });
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

function nulRecords(value: Buffer): string[] {
  return value.toString("utf8").split("\0").filter(Boolean);
}

function parseCount(value: string, label: string): number {
  const count = Number(value.trim());
  if (!Number.isSafeInteger(count) || count < 0) {
    throw new Error(`git returned an invalid ${label} count.`);
  }
  return count;
}

function formatFindings(findings: ReadonlyArray<Finding>): string {
  const locations = findings
    .map(({ revision, path, line, rule }) => `${revision}:${path}:${line} (${rule})`)
    .join("\n");
  return `Secret-shaped repository content was found. Values are redacted; move synthetic cases under an explicit fixture allowlist.\n${locations}`;
}

if (import.meta.main) main();
