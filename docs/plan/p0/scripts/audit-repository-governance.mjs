#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../../..");
const outputRoot = join(repositoryRoot, "docs/plan/p0/governance");
const repository = "hxp0618/cloud-agents";

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    cwd: options.cwd ?? repositoryRoot,
    encoding: "utf8",
    maxBuffer: 16 * 1024 * 1024,
    env: { ...process.env, LC_ALL: "C" },
  });
  return {
    ok: result.status === 0,
    status: result.status,
    stdout: (result.stdout ?? "").trim(),
    stderr: (result.stderr ?? "").trim(),
  };
}

function required(command, args, options) {
  const result = run(command, args, options);
  if (!result.ok) {
    throw new Error(`${command} ${args.join(" ")} failed: ${result.stderr}`);
  }
  return result.stdout;
}

function ghJson(endpoint) {
  return JSON.parse(required("gh", ["api", endpoint]));
}

function ghStatus(endpoint) {
  const result = run("gh", ["api", endpoint]);
  return {
    enabled: result.ok,
    status: result.status,
    error: result.ok ? null : result.stderr.split("\n")[0],
  };
}

function remoteContent(path) {
  const result = run("gh", ["api", `repos/${repository}/contents/${path}`]);
  if (!result.ok) return { path, present: false, sha: null, size: null };
  const value = JSON.parse(result.stdout);
  return { path: value.path, present: true, sha: value.sha, size: value.size };
}

function sha256File(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

const repo = ghJson(`repos/${repository}`);
const protection = ghJson(`repos/${repository}/branches/${repo.default_branch}/protection`);
const rulesets = ghJson(`repos/${repository}/rulesets`).map((ruleset) => {
  const detail = ghJson(`repos/${repository}/rulesets/${ruleset.id}`);
  return {
    id: detail.id,
    name: detail.name,
    target: detail.target,
    enforcement: detail.enforcement,
    conditions: detail.conditions,
    rules: detail.rules.map((rule) => ({ type: rule.type, parameters: rule.parameters ?? null })),
    bypassActorCount: detail.bypass_actors?.length ?? 0,
  };
});
const actionsPermissions = ghJson(`repos/${repository}/actions/permissions`);
const privateVulnerabilityReporting = ghJson(`repos/${repository}/private-vulnerability-reporting`);
const vulnerabilityAlerts = ghStatus(`repos/${repository}/vulnerability-alerts`);
const workflowPath = join(repositoryRoot, ".github/workflows/ci.yml");
const workflow = readFileSync(workflowPath, "utf8");
const actionUses = [...workflow.matchAll(/^\s*- uses:\s*([^\s#]+)/gm)].map((match) => match[1]);
const runnerLabels = [...workflow.matchAll(/^\s*runs-on:\s*([^\s#]+)/gm)].map((match) => match[1]);
const localCodeownersPath = join(repositoryRoot, ".github/CODEOWNERS");
const branch = required("git", ["branch", "--show-current"]);
const head = required("git", ["rev-parse", "HEAD"]);
const remoteHead = required("git", [
  "ls-remote",
  "https://github.com/hxp0618/cloud-agents.git",
  `refs/heads/${branch}`,
]).split(/\s+/)[0];

const requiredChecks = protection.required_status_checks?.contexts ?? [];
const rcTagRuleset = rulesets.find(
  (ruleset) =>
    ruleset.target === "tag" &&
    ruleset.enforcement === "active" &&
    ruleset.conditions?.ref_name?.include?.includes("refs/tags/cloud-agent-m1-rc.*"),
);
const rcRuleTypes = new Set(rcTagRuleset?.rules.map((rule) => rule.type) ?? []);
const checks = {
  mainStatusCheckStrict:
    protection.required_status_checks?.strict === true && requiredChecks.includes("verify"),
  mainAdminEnforced: protection.enforce_admins?.enabled === true,
  mainForcePushDisabled: protection.allow_force_pushes?.enabled === false,
  mainDeletionDisabled: protection.allow_deletions?.enabled === false,
  rcTagRewriteProtected:
    Boolean(rcTagRuleset) && rcRuleTypes.has("deletion") && rcRuleTypes.has("non_fast_forward"),
  vulnerabilityAlertsEnabled: vulnerabilityAlerts.enabled,
  privateVulnerabilityReportingEnabled: privateVulnerabilityReporting.enabled === true,
  secretScanningEnabled: repo.security_and_analysis?.secret_scanning?.status === "enabled",
  pushProtectionEnabled:
    repo.security_and_analysis?.secret_scanning_push_protection?.status === "enabled",
  candidateCodeownersPresent: sha256File(localCodeownersPath).length === 64,
  workflowActionsPinned:
    actionUses.length > 0 && actionUses.every((value) => /@[0-9a-f]{40}$/.test(value)),
  workflowRunnerNotLatest:
    runnerLabels.length > 0 && runnerLabels.every((value) => !value.endsWith("-latest")),
};

const snapshot = {
  schemaVersion: 1,
  observedAt: new Date().toISOString(),
  repository: {
    nameWithOwner: repo.full_name,
    visibility: repo.visibility,
    archived: repo.archived,
    defaultBranch: repo.default_branch,
    defaultBranchSha: ghJson(`repos/${repository}/git/ref/heads/${repo.default_branch}`).object.sha,
  },
  candidate: {
    branch,
    localHead: head,
    remoteHead: remoteHead || null,
    codeownersSha256: sha256File(localCodeownersPath),
    workflowSha256: sha256File(workflowPath),
    actionUses,
    runnerLabels,
  },
  defaultBranchContent: {
    codeowners: remoteContent(".github/CODEOWNERS"),
    rootCodeowners: remoteContent("CODEOWNERS"),
    securityPolicy: remoteContent("SECURITY.md"),
  },
  protection: {
    requiredStatusChecks: requiredChecks,
    strict: protection.required_status_checks?.strict ?? false,
    enforceAdmins: protection.enforce_admins?.enabled ?? false,
    requiredApprovingReviewCount:
      protection.required_pull_request_reviews?.required_approving_review_count ?? null,
    requireCodeOwnerReviews:
      protection.required_pull_request_reviews?.require_code_owner_reviews ?? null,
    allowForcePushes: protection.allow_force_pushes?.enabled ?? null,
    allowDeletions: protection.allow_deletions?.enabled ?? null,
    requiredSignatures: protection.required_signatures?.enabled ?? null,
  },
  rulesets,
  actionsPermissions: {
    enabled: actionsPermissions.enabled,
    allowedActions: actionsPermissions.allowed_actions,
    shaPinningRequired: actionsPermissions.sha_pinning_required ?? false,
  },
  security: {
    vulnerabilityAlerts,
    privateVulnerabilityReportingEnabled: privateVulnerabilityReporting.enabled,
    securityAndAnalysis: repo.security_and_analysis,
  },
  checks,
  result: Object.values(checks).every(Boolean) ? "PASS" : "FAIL",
  limitations: [
    "The CODEOWNERS and workflow hardening are candidate-branch state until reviewed and merged separately.",
    "GitHub-hosted runner image labels are versioned but not immutable OCI digests.",
    "A PASS here records repository governance only; it does not close license, provenance, or release Gates.",
  ],
};

mkdirSync(outputRoot, { recursive: true });
const jsonPath = join(outputRoot, "repository-governance.json");
writeFileSync(jsonPath, `${JSON.stringify(snapshot, null, 2)}\n`);
const checkRows = Object.entries(checks)
  .map(([name, passed]) => `| \`${name}\` | ${passed ? "PASS" : "FAIL"} |`)
  .join("\n");
const summary = `# P0 repository governance snapshot

- Status: ${snapshot.result}
- Observed at: ${snapshot.observedAt}
- Repository: \`${snapshot.repository.nameWithOwner}\`
- Default branch: \`${snapshot.repository.defaultBranch}@${snapshot.repository.defaultBranchSha}\`
- Candidate: \`${branch}@${head}\`
- Remote candidate before this evidence commit: \`${remoteHead || "absent"}\`

| Check | Result |
| --- | --- |
${checkRows}

## Boundaries

- Default-branch CODEOWNERS is currently ${snapshot.defaultBranchContent.codeowners.present ? "present" : "absent"}; the candidate adds it for separate review.
- GitHub repository-level Actions policy does ${snapshot.actionsPermissions.shaPinningRequired ? "" : "not "}force SHA pinning; this candidate pins every current workflow action in source.
- This evidence does not claim the candidate was merged, a release was published, or supply-chain attestation was completed.
`;
writeFileSync(join(outputRoot, "README.md"), summary);

process.stdout.write(
  `${JSON.stringify({ jsonPath, result: snapshot.result, checks, candidate: snapshot.candidate }, null, 2)}\n`,
);
if (snapshot.result !== "PASS") process.exitCode = 1;
