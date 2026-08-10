#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");

const businessRoot =
  process.env.CLOUD_AGENTS_BUSINESS_ROOT ?? "/Users/huang/devel/project/huang/business";
const output =
  process.env.CLOUD_AGENTS_BASELINE_OUTPUT ??
  join(process.cwd(), "docs/plan/p0/frozen-baseline.json");

const worktrees = [
  {
    id: "synara-primary-concurrent",
    path: join(businessRoot, "synara"),
    role: "concurrent source; excluded from P0 extraction while dirty",
  },
  {
    id: "synara-external-runtime-source",
    path: join(businessRoot, "synara-cloud-agent-external-runtime"),
    role: "authoritative clean P0 extraction source",
  },
  {
    id: "synara-stage8-concurrent",
    path: join(businessRoot, "synara-stage-8"),
    role: "concurrent product work; excluded from P0 extraction",
  },
  {
    id: "t3-main",
    path: join(businessRoot, "t3code"),
    role: "fresh-main embedded baseline",
  },
  {
    id: "t3-cloud-agent-fresh",
    path: join(businessRoot, "t3code-cloud-agent-fresh"),
    role: "Cloud Agents embedded consumer baseline",
  },
  {
    id: "cloud-agents-platform",
    path: join(businessRoot, "cloud-agents"),
    role: "public source-of-truth and P0 plan branch",
  },
  {
    id: "cloud-agents-codex-fix",
    path: join(businessRoot, "cloud-agents-portable-runtime"),
    role: "uncommitted M1 fix; excluded from P0 baseline",
  },
];

const liveRemoteQueries = [
  {
    repository: "synara",
    path: join(businessRoot, "synara"),
    refs: [
      "refs/heads/main",
      "refs/heads/codex/saas-tenancy-user",
      "refs/heads/codex/cloud-agent-external-runtime",
    ],
  },
  {
    repository: "t3code",
    path: join(businessRoot, "t3code"),
    refs: ["refs/heads/main", "refs/heads/feat/cloud-agent"],
  },
  {
    repository: "cloud-agents",
    path: join(businessRoot, "cloud-agents"),
    refs: [
      "refs/heads/main",
      "refs/heads/feat/portable-runtime",
      "refs/heads/codex/cloud-agents-platform-p0",
      "refs/tags/cloud-agent-m1-rc.1",
    ],
  },
];

function command(commandName, args, options = {}) {
  const result = spawnSync(commandName, args, {
    cwd: options.cwd,
    encoding: "utf8",
    maxBuffer: 32 * 1024 * 1024,
    timeout: options.timeout ?? 30_000,
    env: { ...process.env, LC_ALL: "C" },
  });
  return {
    ok: result.status === 0,
    status: result.status,
    signal: result.signal,
    stdout: (result.stdout ?? "").trim(),
    stderr: (result.stderr ?? "").trim(),
    error: result.error ? String(result.error.message ?? result.error) : null,
  };
}

function git(cwd, ...args) {
  const result = command("git", ["-C", cwd, ...args]);
  if (!result.ok) {
    throw new Error(`git ${args.join(" ")} failed in ${cwd}: ${result.stderr || result.error}`);
  }
  return result.stdout;
}

function optionalGit(cwd, ...args) {
  const result = command("git", ["-C", cwd, ...args]);
  return result.ok ? result.stdout : null;
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function statusSnapshot(path) {
  const lines = git(path, "status", "--porcelain=v1", "--untracked-files=all")
    .split("\n")
    .filter(Boolean);
  const counts = { staged: 0, unstaged: 0, untracked: 0 };
  for (const line of lines) {
    if (line.startsWith("??")) {
      counts.untracked += 1;
      continue;
    }
    if (line[0] && line[0] !== " ") counts.staged += 1;
    if (line[1] && line[1] !== " ") counts.unstaged += 1;
  }
  return { clean: lines.length === 0, counts, entries: lines };
}

function worktreeSnapshot(spec) {
  const upstream = optionalGit(spec.path, "rev-parse", "--abbrev-ref", "@{upstream}");
  return {
    ...spec,
    branch: git(spec.path, "branch", "--show-current"),
    head: git(spec.path, "rev-parse", "HEAD"),
    tree: git(spec.path, "rev-parse", "HEAD^{tree}"),
    upstream,
    upstreamOid: upstream ? optionalGit(spec.path, "rev-parse", upstream) : null,
    origin: optionalGit(spec.path, "remote", "get-url", "origin"),
    status: statusSnapshot(spec.path),
  };
}

function localRefSnapshot(path, ref) {
  const oid = optionalGit(path, "rev-parse", "--verify", `${ref}^{}`);
  return oid
    ? { ref, oid, tree: optionalGit(path, "rev-parse", `${ref}^{tree}`) }
    : { ref, oid: null };
}

function liveRemoteSnapshot(spec) {
  const result = command("git", ["-C", spec.path, "ls-remote", "origin", ...spec.refs], {
    timeout: 15_000,
  });
  const values = {};
  if (result.ok) {
    for (const line of result.stdout.split("\n").filter(Boolean)) {
      const [oid, ref] = line.split(/\s+/, 2);
      values[ref] = oid;
    }
  }
  return {
    repository: spec.repository,
    queriedRefs: spec.refs,
    status: result.ok ? "verified" : "unavailable",
    values,
    error: result.ok
      ? null
      : [result.error, result.stderr.split("\n")[0]].filter(Boolean).join(": "),
  };
}

const cloudAgentsPath = join(businessRoot, "cloud-agents");
const synaraSourcePath = join(businessRoot, "synara-cloud-agent-external-runtime");
const t3Path = join(businessRoot, "t3code");

const snapshot = {
  schemaVersion: 1,
  generatedAt: new Date().toISOString(),
  scope: "Platform P0 freeze only; no implementation, publication, deployment, or data mutation",
  worktrees: worktrees.map(worktreeSnapshot),
  repositoryWorktreeRegistries: {
    synara: git(join(businessRoot, "synara"), "worktree", "list", "--porcelain"),
    t3code: git(t3Path, "worktree", "list", "--porcelain"),
    cloudAgents: git(cloudAgentsPath, "worktree", "list", "--porcelain"),
  },
  sourceTrees: {
    synaraRoot: git(synaraSourcePath, "rev-parse", "HEAD^{tree}"),
    synaraControlPlane: git(synaraSourcePath, "rev-parse", "HEAD:services/control-plane"),
    synaraDeploy: git(synaraSourcePath, "rev-parse", "HEAD:deploy"),
    synaraScripts: git(synaraSourcePath, "rev-parse", "HEAD:scripts"),
  },
  localRefs: {
    synara: [
      "refs/heads/main",
      "refs/heads/codex/saas-tenancy-user",
      "refs/heads/codex/cloud-agent-external-runtime",
      "refs/remotes/origin/main",
      "refs/remotes/origin/codex/saas-tenancy-user",
      "refs/remotes/origin/codex/cloud-agent-external-runtime",
    ].map((ref) => localRefSnapshot(join(businessRoot, "synara"), ref)),
    t3code: [
      "refs/heads/main",
      "refs/heads/feat/cloud-agent",
      "refs/remotes/origin/main",
      "refs/remotes/origin/feat/cloud-agent",
    ].map((ref) => localRefSnapshot(t3Path, ref)),
    cloudAgents: [
      "refs/heads/main",
      "refs/heads/feat/portable-runtime",
      "refs/heads/codex/cloud-agents-platform-p0",
      "refs/remotes/origin/main",
      "refs/remotes/origin/feat/portable-runtime",
      "refs/remotes/origin/codex/cloud-agents-platform-p0",
      "refs/tags/cloud-agent-m1-rc.1",
    ].map((ref) => localRefSnapshot(cloudAgentsPath, ref)),
  },
  liveRemotes: liveRemoteQueries.map(liveRemoteSnapshot),
  toolchains: {
    git: command("git", ["--version"]).stdout,
    node: command(process.execPath, ["--version"]).stdout,
    bun: command("bun", ["--version"]).stdout,
    go: command("go", ["version"]).stdout,
  },
};

const canonical = `${JSON.stringify(snapshot, null, 2)}\n`;
writeFileSync(output, canonical);

const formatResult = command(join(repositoryRoot, "node_modules/.bin/oxfmt"), [output, "--write"], {
  cwd: repositoryRoot,
});
if (!formatResult.ok) {
  throw new Error(`failed to format ${output}: ${formatResult.stderr || formatResult.error}`);
}
const formatted = readFileSync(output);
process.stdout.write(
  `${JSON.stringify({ output, sha256: sha256(formatted), worktrees: snapshot.worktrees.length, liveRemotes: snapshot.liveRemotes }, null, 2)}\n`,
);
