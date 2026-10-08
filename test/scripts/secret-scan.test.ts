import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

import {
  formatSecretScanResult,
  scanSecretRepository,
  scanSecretWorktree,
  selectSecretScanRevisions,
} from "../../scripts/secret-scan";

const head = "a".repeat(40);
const base = "b".repeat(40);

describe("secret scan revision selection", () => {
  it("scans only HEAD for empty or all-zero event bases", () => {
    expect(selectSecretScanRevisions({ head })).toEqual([head]);
    expect(selectSecretScanRevisions({ head, base: "0".repeat(40) })).toEqual([head]);
  });

  it("rejects a base that is not a full commit SHA", () => {
    expect(() => selectSecretScanRevisions({ head, base: "not-a-sha" })).toThrow(
      "CLOUD_AGENT_SECRET_SCAN_BASE must be a full commit SHA.",
    );
  });

  it("uses the requested push range when it is non-empty", () => {
    expect(selectSecretScanRevisions({ head, base, range: ["c".repeat(40)] })).toEqual([
      "c".repeat(40),
    ]);
    expect(selectSecretScanRevisions({ head, base })).toEqual([head]);
  });

  it("deduplicates HEAD from pickaxe history candidates", () => {
    expect(selectSecretScanRevisions({ head, allHistory: true, candidates: [head, base] })).toEqual(
      [head, base],
    );
  });
});

describe("secret scan worktree enumeration", () => {
  const temporaryRoots: string[] = [];

  afterEach(() => {
    for (const root of temporaryRoots.splice(0)) {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("preserves Git paths containing Unicode and newlines", () => {
    const root = createTemporaryGitRepository(temporaryRoots);
    const content = `${"password"}=${JSON.stringify("0".repeat(16))}\n`;
    const unicodePath = join(root, "unicode-é.txt");
    const newlinePath = join(root, "newline\nname.txt");
    writeFileSync(unicodePath, content);
    writeFileSync(newlinePath, content);
    git(root, ["add", "--", "unicode-é.txt", "newline\nname.txt"]);

    const findings = scanSecretWorktree(root);

    expect(findings).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "unicode-é.txt", revision: "worktree" }),
        expect.objectContaining({ path: "newline\nname.txt", revision: "worktree" }),
      ]),
    );
    expect(findings).toHaveLength(2);
  });

  it("scans symlink text without following an external target", () => {
    const root = createTemporaryGitRepository(temporaryRoots);
    const outside = join(root, "..", "secret-scan-outside-target.txt");
    writeFileSync(outside, `${"password"}=${JSON.stringify("1".repeat(16))}\n`);
    try {
      mkdirSync(join(root, "links"));
      symlinkSync(outside, join(root, "links/outside-target"));
      symlinkSync("missing-target", join(root, "links/dangling"));
      symlinkSync(`${"password"}=${JSON.stringify("2".repeat(16))}`, join(root, "links/link-text"));
      git(root, ["add", "--", "links/outside-target", "links/dangling", "links/link-text"]);

      expect(scanSecretWorktree(root)).toEqual([
        expect.objectContaining({ path: "links/link-text", revision: "worktree" }),
      ]);
    } finally {
      rmSync(outside, { force: true });
    }
  });

  it("reports disjoint worktree coverage counts", () => {
    const root = createTemporaryGitRepository(temporaryRoots);
    writeFileSync(join(root, "tracked.txt"), "tracked text\n");
    writeFileSync(join(root, "deleted.txt"), "deleted text\n");
    writeFileSync(join(root, "binary.bin"), Buffer.from([1, 0, 2]));
    git(root, ["add", "--", "tracked.txt", "deleted.txt", "binary.bin"]);
    git(root, ["commit", "--quiet", "-m", "add worktree fixtures"]);
    rmSync(join(root, "deleted.txt"));
    writeFileSync(join(root, "untracked.txt"), "untracked text\n");
    mkdirSync(join(root, "fixtures"));
    writeFileSync(join(root, "fixtures", "synthetic.txt"), "allowlisted text\n");

    const result = scanSecretRepository(root, { allowlistPatterns: ["fixtures/**"] });

    expect(result.coverage.worktree).toEqual({
      tracked: 3,
      untracked: 2,
      enumerated: 5,
      text: 2,
      allowlisted: 1,
      binary: 1,
      deleted: 1,
    });
    expect(result.coverage.history).toEqual({
      mode: "head",
      reachableCommits: null,
      pickaxeCandidates: null,
      scannedRevisionTrees: 1,
    });
  });

  it("finds a mixed-case assigned secret committed and then deleted", () => {
    const root = createTemporaryGitRepository(temporaryRoots);
    const secretValue = "history-only-value".repeat(2);
    writeFileSync(join(root, "deleted-secret.txt"), `Api_Key=${JSON.stringify(secretValue)}\n`);
    git(root, ["add", "--", "deleted-secret.txt"]);
    git(root, ["commit", "--quiet", "-m", "add deleted fixture"]);
    rmSync(join(root, "deleted-secret.txt"));
    git(root, ["add", "--update"]);
    git(root, ["commit", "--quiet", "-m", "remove deleted fixture"]);

    const result = scanSecretRepository(root, { allHistory: true, allowlistPatterns: [] });
    const output = formatSecretScanResult(result);

    expect(result.findings).toEqual([
      expect.objectContaining({
        path: "deleted-secret.txt",
        line: 1,
        rule: "assigned-secret",
      }),
    ]);
    expect(result.coverage.history).toEqual({
      mode: "all-history",
      reachableCommits: 2,
      pickaxeCandidates: 2,
      scannedRevisionTrees: 2,
    });
    expect(output).toContain("Secret-shaped repository content was found");
    expect(output).toContain("deleted-secret.txt:1 (assigned-secret)");
    expect(output).not.toContain(secretValue);
  });

  it("selects a secret introduced only by a merge result", () => {
    const root = createTemporaryGitRepository(temporaryRoots);
    writeFileSync(join(root, "base.txt"), "base\n");
    git(root, ["add", "--", "base.txt"]);
    git(root, ["commit", "--quiet", "-m", "base"]);
    const baseRevision = gitOutput(root, ["rev-parse", "HEAD"]);
    git(root, ["checkout", "--quiet", "-b", "left"]);
    writeFileSync(join(root, "left.txt"), "left\n");
    git(root, ["add", "--", "left.txt"]);
    git(root, ["commit", "--quiet", "-m", "left"]);
    git(root, ["checkout", "--quiet", "-b", "right", baseRevision]);
    writeFileSync(join(root, "right.txt"), "right\n");
    git(root, ["add", "--", "right.txt"]);
    git(root, ["commit", "--quiet", "-m", "right"]);
    git(root, ["checkout", "--quiet", "left"]);
    git(root, ["merge", "--quiet", "--no-ff", "right", "-m", "merge"]);
    const secretValue = "merge-result-value".repeat(2);
    writeFileSync(join(root, "merge-secret.txt"), `Auth_Token=${JSON.stringify(secretValue)}\n`);
    git(root, ["add", "--", "merge-secret.txt"]);
    git(root, ["commit", "--quiet", "--amend", "--no-edit"]);

    const result = scanSecretRepository(root, { allHistory: true, allowlistPatterns: [] });

    expect(result.findings).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: "merge-secret.txt",
          revision: expect.not.stringMatching(/^worktree$/u),
          rule: "assigned-secret",
        }),
      ]),
    );
    expect(result.findings.filter(({ revision }) => revision !== "worktree")).toHaveLength(1);
    expect(result.coverage.history).toEqual({
      mode: "all-history",
      reachableCommits: 4,
      pickaxeCandidates: 1,
      scannedRevisionTrees: 1,
    });
    expect(formatSecretScanResult(result)).not.toContain(secretValue);
  });
});

function createTemporaryGitRepository(roots: string[]): string {
  const root = mkdtempSync(join(tmpdir(), "cloud-agents-secret-scan-"));
  roots.push(root);
  git(root, ["init", "--quiet"]);
  git(root, ["config", "user.name", "Secret Scan Test"]);
  git(root, ["config", "user.email", "secret-scan@example.invalid"]);
  return root;
}

function git(cwd: string, args: ReadonlyArray<string>): void {
  execFileSync("git", args, { cwd, stdio: "ignore" });
}

function gitOutput(cwd: string, args: ReadonlyArray<string>): string {
  return execFileSync("git", args, { cwd, encoding: "utf8" }).trim();
}
