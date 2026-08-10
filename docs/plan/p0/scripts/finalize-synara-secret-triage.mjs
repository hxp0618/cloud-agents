#!/usr/bin/env node

import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

const SOURCE_COMMIT = "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0";
const EXPECTED_FINDING_SET_SHA256 =
  "5f74f2af7a9468e56fcd65bf3f6cc2ee168bf19189cbec92d0f75832f0860724";
const EXPECTED_SCOPE_COUNTS = {
  "fixed-source-tree-archive": 17,
  "fixed-commit-reachable-git-history": 33,
  "public-candidate-selected-tree": 6,
};
const EXPECTED_GROUP_COUNTS = {
  R1: 2,
  R2: 2,
  R3: 4,
  R4: 18,
  R5: 2,
  R6: 2,
  R7: 2,
  R8: 9,
  R9: 2,
  R10: 6,
  R11: 3,
  R12: 1,
  R13: 1,
  R14: 2,
};

const planRoot = resolve(import.meta.dirname, "..");
const inputPath = join(planRoot, "provenance/synara-extraction-secret-scan-sanitized.json");
const outputPath = join(planRoot, "provenance/synara-extraction-secret-triage.json");
const input = JSON.parse(readFileSync(inputPath, "utf8"));
const findings = input.scopes
  .flatMap((scope) => scope.findings.map((finding) => ({ scope: scope.name, ...finding })))
  .toSorted((left, right) => left.fingerprintSha256.localeCompare(right.fingerprintSha256));

if (findings.length !== 56) throw new Error(`finding count mismatch: ${findings.length}/56`);
const scopeCounts = Object.fromEntries(
  input.scopes.map((scope) => [scope.name, scope.findingCount]),
);
if (JSON.stringify(scopeCounts) !== JSON.stringify(EXPECTED_SCOPE_COUNTS)) {
  throw new Error(`scope count mismatch: ${JSON.stringify(scopeCounts)}`);
}
const identityLines = findings.map((finding) =>
  [
    finding.scope,
    finding.ruleId,
    finding.path,
    finding.line ?? "",
    finding.commit ?? "",
    finding.fingerprintSha256,
  ].join("\t"),
);
const findingSetSha256 = sha256(`${identityLines.join("\n")}\n`);
if (findingSetSha256 !== EXPECTED_FINDING_SET_SHA256) {
  throw new Error(`reviewed finding set drifted: ${findingSetSha256}`);
}

const definitions = {
  R1: definition(
    "token alphabet constant, not a token instance",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Server Auth",
    "2026-11-08",
  ),
  R2: definition(
    "HTTP or WebSocket test protocol input, not a credential",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Server Transport",
    "2026-11-08",
  ),
  R3: definition(
    "External MCP unit-test credential sentinel",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "External MCP",
    "2026-11-08",
  ),
  R4: definition(
    "keybinding key or command enum, not an API key",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Desktop/Keybindings",
    "2026-11-08",
  ),
  R5: definition(
    "browser annotation example placeholder",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Web/Annotations",
    "2026-11-08",
  ),
  R6: definition(
    "protected-environment test sentinel",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Release Engineering + Security",
    "2026-11-08",
  ),
  R7: definition(
    "rotation test sentinel",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Release Engineering + Security",
    "2026-11-08",
  ),
  R8: definition(
    "database enum, default, trigger, or CHECK constraint",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Control Plane DB/Schema",
    "2026-11-08",
  ),
  R9: definition(
    "incident business identifier, not authentication material",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Incident Governance",
    "2026-11-08",
  ),
  R10: definition(
    "static test private-key material with no production reference",
    "REWRITE_REQUIRED_BEFORE_PUBLICATION",
    "Stage 3 Acceptance + Security",
    "2026-09-09",
  ),
  R11: definition(
    "private-key format-header assertion without private-key body",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Stage 3 Acceptance + Security",
    "2026-11-08",
  ),
  R12: definition(
    "synthetic private-key-shaped redactor test fixture",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Stage 3 Acceptance + Security",
    "2026-11-08",
  ),
  R13: definition(
    "Kubernetes secretKeyRef key name, not a Secret value",
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "Platform/Kubernetes",
    "2026-11-08",
  ),
  R14: definition(
    "copied enum context inside a historical log; the history itself remains quarantined",
    "SOURCE_HISTORY_QUARANTINE",
    "Synara Repo Security",
    "2026-09-09",
  ),
};

for (const finding of findings) {
  const reasonCode = reasonCodeFor(finding);
  definitions[reasonCode].fingerprintSha256.push(finding.fingerprintSha256);
}
for (const [reasonCode, expected] of Object.entries(EXPECTED_GROUP_COUNTS)) {
  const observed = definitions[reasonCode].fingerprintSha256.length;
  if (observed !== expected)
    throw new Error(`${reasonCode} count mismatch: ${observed}/${expected}`);
  definitions[reasonCode].fingerprintSha256.sort();
}

const output = {
  schemaVersion: 1,
  kind: "cloud-agent-p0-synara-extraction-secret-triage",
  sourceCommit: SOURCE_COMMIT,
  findingSetSha256,
  reviewedAt: "2026-08-10",
  reviewer: "Codex P0 secret-triage explorer (independent read-only review)",
  policy: {
    wholeDirectoryAllowlist: false,
    wholeRuleAllowlist: false,
    rawSecretPersisted: false,
    publicHistoryImportAuthorized: false,
    staticPrivateKeyAction: "rewrite-before-publication",
    unknownFindingAction: "fail-closed",
  },
  groups: Object.entries(definitions).map(([reasonCode, value]) => ({ reasonCode, ...value })),
};
const encoded = `${JSON.stringify(output, null, 2)}\n`.replace(
  /"fingerprintSha256": \[\n        "([0-9a-f]{64})"\n      \]/gu,
  '"fingerprintSha256": ["$1"]',
);
writeFileSync(outputPath, encoded);
process.stdout.write(
  `${JSON.stringify({ outputPath, findingSetSha256, findings: findings.length }, null, 2)}\n`,
);

function definition(reason, disposition, owner, expiresAt) {
  return { reason, disposition, owner, expiresAt, fingerprintSha256: [] };
}

function reasonCodeFor(finding) {
  const { path, line, ruleId } = finding;
  if (path === ".codex-log-increment-b.txt") return "R14";
  if (path === "scripts/stage3-provider-acceptance/test_vault_audit_acceptance_sink.py") {
    return "R10";
  }
  if (path.includes("BootstrapCredentialService")) return "R1";
  if (path.includes("nodeHttpServer.test")) return "R2";
  if (path.includes("externalMcp/bridge.test")) return "R3";
  if (path.includes("keybindings")) return "R4";
  if (path.includes("browserAnnotations")) return "R5";
  if (path.includes("stage6-protected-environment")) return "R6";
  if (path.includes("stage6-rotation")) return "R7";
  if (
    path.includes("database/store.go") ||
    path.includes("000054_kubernetes_terminal_suspend_proof.sql")
  ) {
    return "R8";
  }
  if (path.includes("incidentgovernance")) return "R9";
  if (path.includes("test_acceptance_runner.py") && ruleId === "private-key") {
    return line === 460 ? "R12" : "R11";
  }
  if (path === "deploy/kubernetes/deployment.yaml") return "R13";
  throw new Error(`unreviewed finding identity: ${JSON.stringify(finding)}`);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}
