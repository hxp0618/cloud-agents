#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const root = resolve(scriptDirectory, "../../../..");
const baselineDirectory = resolve(scriptDirectory, "../baseline");

const synara = readJson(join(baselineDirectory, "synara-legacy.json"));
const t3 = readJson(join(baselineDirectory, "t3-embedded.json"));
const referenceHost = readJson(join(baselineDirectory, "reference-host-negative.json"));
const requiredProfileCoverage = {
  "synara-legacy-managed-agent": [
    "provider-host-protocol-2.2/2.3-history",
    "execution-generation-fencing",
    "worker-claim-concurrency",
    "workspace-restore-generation",
    "workspace-lifecycle-and-containment",
    "kubernetes-allocation-backend",
    "provider-credential-broker",
    "leadership-write-fence",
    "kubernetes-pod-deletion-fence",
    "candidate-and-worker-image-identity",
  ],
  "t3-embedded-and-cloud-agent-consumer": [
    "t3-thread-and-provider-command-authority",
    "workspace-filesystem-authority",
    "git-hidden-ref-diff-revert-authority",
    "checkpoint-store-and-reactor",
    "pairing-session-proof-revoke-spec",
    "pairing-grant-store-single-use-and-revoke-tests",
    "dpop-proof-verify-consume-and-replay-tests",
    "relay-managed-endpoint-allocation-tests",
    "digest-and-runtime-probe-fail-close",
    "adapter-correlation-drain-and-text-generation-unit-oracles",
  ],
  "reference-host-negative-and-public-runtime": [
    "protocol-2.2/2.3-frame-characterization",
    "all-command-and-message-variant-coverage",
    "correlation-and-generation-negative-oracles",
    "ordering-late-and-duplicate-terminal-negative-oracles",
    "four-stop-outcome-oracles",
    "packed-binary-illegal-frame-and-bounded-multiplexing-source-oracles",
    "greenfield-reference-host-lifecycle-spec-oracle",
  ],
};
const requiredFixedPaths = {
  "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0": [
    "services/control-plane/internal/executiontargets/kubernetes_allocation_backend_test.go",
    "services/control-plane/internal/agentd/workspace_test.go",
    "services/control-plane/internal/agentd/workspace_restore_generation_test.go",
    "services/control-plane/internal/agentd/provider_credential_broker_test.go",
  ],
  "8101cd044911c7dc2a2adf7c7a9ba7962abf57b6": [
    "apps/server/src/auth/PairingGrantStore.test.ts",
    "apps/server/src/auth/dpop.test.ts",
    "infra/relay/src/auth/DpopProofs.verifyAndConsume.test.ts",
    "infra/relay/src/environments/ManagedEndpointAllocations.test.ts",
  ],
};

const repositories = [synara, ...t3.repositories, referenceHost];
let gitFiles = 0;
for (const repository of repositories) {
  auditRepository(repository);
  gitFiles += repository.files.length;
}

const fixtureRoot = resolve(root, referenceHost.fixtureCorpus.root);
for (const fixture of referenceHost.fixtureCorpus.files) {
  const path = resolveContained(fixtureRoot, fixture.path);
  const bytes = readFileSync(path);
  equal(sha256(bytes), fixture.sha256, `fixture SHA-256 ${fixture.path}`);
}
auditFixtureCoverage(fixtureRoot, referenceHost.fixtureCorpus.version);

for (const manifest of [synara, t3, referenceHost]) {
  equal(
    manifest.schemaVersion,
    "cloud-agents.p0.baseline-manifest/v2",
    `${manifest.profile} schema version`,
  );
  equal(
    manifest.platformP0CharacterizationClosure.complete,
    true,
    `${manifest.profile} Platform P0 characterization closure`,
  );
  equal(
    manifest.platformP0CharacterizationClosure.doesNotDecideAggregateGate,
    true,
    `${manifest.profile} aggregate Gate boundary`,
  );
  equal(manifest.m1BehaviorClosure.complete, false, `${manifest.profile} M1 behavior closure`);
  equal(manifest.m1BehaviorClosure.status, "NOT_RUN", `${manifest.profile} M1 status`);
  equal(manifest.aggregateGateDecision, "NOT_CLAIMED", `${manifest.profile} aggregate Gate claim`);
  equal(manifest.realProviderExecution, "NOT_RUN", `${manifest.profile} real Provider boundary`);
  requireArray(manifest.coverage, `${manifest.profile} coverage`);
  requireArray(manifest.limitations, `${manifest.profile} limitations`);
  equalSet(
    manifest.coverage,
    requiredProfileCoverage[manifest.profile],
    `${manifest.profile} complete coverage`,
  );
}

process.stdout.write(
  `${JSON.stringify(
    {
      status: "PASS",
      audit: "p0-baseline-evidence-v2",
      repositories: repositories.length,
      gitFiles,
      fixtureFiles: referenceHost.fixtureCorpus.files.length,
      platformP0CharacterizationClosure: true,
      m1BehaviorClosure: false,
      m1BehaviorStatus: "NOT_RUN",
      aggregateGateDecision: "NOT_CLAIMED",
    },
    null,
    2,
  )}\n`,
);

function auditRepository(manifest) {
  requireString(manifest.ref, `${manifest.profile ?? manifest.role} ref`);
  requireString(manifest.tree, `${manifest.profile ?? manifest.role} tree`);
  requireArray(manifest.repoCandidates, `${manifest.profile ?? manifest.role} repoCandidates`);
  requireArray(manifest.files, `${manifest.profile ?? manifest.role} files`);
  const fixedPaths = new Set(manifest.files.map((value) => value.path));
  for (const required of requiredFixedPaths[manifest.ref] ?? []) {
    if (!fixedPaths.has(required))
      fail(`${manifest.ref} omitted required baseline path ${required}`);
  }
  const repo = manifest.repoCandidates.find(
    (candidate) =>
      existsSync(candidate) &&
      gitSucceeds(candidate, ["cat-file", "-e", `${manifest.ref}^{commit}`]),
  );
  if (!repo) fail(`no local repository contains fixed ref ${manifest.ref}`);
  equal(
    gitText(repo, ["rev-parse", `${manifest.ref}^{commit}`]),
    manifest.ref,
    `commit ${manifest.ref}`,
  );
  equal(
    gitText(repo, ["rev-parse", `${manifest.ref}^{tree}`]),
    manifest.tree,
    `tree ${manifest.ref}`,
  );
  for (const file of manifest.files) {
    const spec = `${manifest.ref}:${file.path}`;
    equal(gitText(repo, ["rev-parse", spec]), file.blob, `blob ${spec}`);
    const content = gitBytes(repo, ["show", spec]);
    equal(content.length, file.bytes, `bytes ${spec}`);
    equal(sha256(content), file.sha256, `SHA-256 ${spec}`);
  }
}

function auditFixtureCoverage(fixtureRoot, expectedVersion) {
  const manifest = readJson(join(fixtureRoot, "corpus-manifest.json"));
  equal(manifest.corpusVersion, expectedVersion, "fixture corpus version");
  equalSet(
    manifest.files,
    referenceHost.fixtureCorpus.files
      .map((value) => value.path)
      .filter((value) => value !== "corpus-manifest.json"),
    "fixture corpus file manifest",
  );
  const v22Commands = jsonLines(join(fixtureRoot, "v2.2/commands.jsonl"));
  const v23Commands = jsonLines(join(fixtureRoot, "v2.3/commands.jsonl"));
  const v22Messages = jsonLines(join(fixtureRoot, "v2.2/messages.jsonl"));
  const v23Messages = jsonLines(join(fixtureRoot, "v2.3/messages.jsonl"));
  const negatives = jsonLines(join(fixtureRoot, "negative/frames.jsonl"));
  const transcripts = readJson(join(fixtureRoot, "transcripts.json"));
  const semanticOracles = readJson(join(fixtureRoot, "semantic-oracles.json"));
  const lifecycle = readJson(join(fixtureRoot, "reference-host-lifecycle-v1.json"));

  equalSet(
    v22Commands.map((value) => value.commandType),
    manifest.requiredCoverage.commandsV22,
    "2.2 commands",
  );
  equalSet(
    v23Commands.map((value) => value.commandType),
    manifest.requiredCoverage.commandsV23,
    "2.3 commands",
  );
  equalSet(
    v22Messages.map((value) => value.messageType),
    manifest.requiredCoverage.messageVariants,
    "2.2 messages",
  );
  equalSet(
    v23Messages.map((value) => value.messageType),
    manifest.requiredCoverage.messageVariants,
    "2.3 messages",
  );
  equalSet(
    negatives.map((value) => value.category),
    manifest.requiredCoverage.negativeCategories,
    "negative categories",
  );
  equalSet(
    transcripts.cases.flatMap((value) => value.coverage),
    manifest.requiredCoverage.transcriptSemantics,
    "transcript semantics",
  );
  equalSet(
    semanticOracles.cases.map((value) => value.coverage),
    manifest.requiredCoverage.characterizationOnly,
    "characterization-only semantics",
  );
  if (!semanticOracles.cases.some((value) => value.status === "NOT_RUN")) {
    fail("semantic oracles must retain an explicit NOT_RUN real Provider case");
  }
  if (!semanticOracles.cases.some((value) => value.status === "NOT_ENFORCED")) {
    fail("semantic oracles must retain an explicit NOT_ENFORCED capability case");
  }
  auditReferenceHostLifecycle(lifecycle, manifest.requiredCoverage.referenceHostLifecycle);
}

function auditReferenceHostLifecycle(lifecycle, expectedCoverage) {
  equal(
    lifecycle.schemaVersion,
    "cloud-agents.reference-host-lifecycle-trace/v1",
    "reference-host lifecycle schema",
  );
  equal(lifecycle.oracleType, "GREENFIELD_SPEC_ORACLE", "reference-host lifecycle oracle type");
  equal(
    lifecycle.implementationStatus,
    "NOT_IMPLEMENTED_BY_FIXTURE",
    "reference-host lifecycle implementation boundary",
  );
  equal(lifecycle.productionExecution, "NOT_RUN", "reference-host production execution boundary");
  requireArray(lifecycle.cases, "reference-host lifecycle cases");
  equalSet(
    lifecycle.cases.flatMap((value) => value.coverage),
    expectedCoverage,
    "reference-host lifecycle coverage",
  );

  const byId = new Map(lifecycle.cases.map((value) => [value.id, value]));
  const happy = requiredCase(byId, "create-admit-provision-ready-terminate-reap");
  equal(
    JSON.stringify(happy.trace.map((value) => value.event)),
    JSON.stringify([
      "create.requested",
      "admission.accepted",
      "provision.started",
      "workload.ready",
      "terminate.requested",
      "ingress.fenced",
      "sessions.revoked",
      "cleanup.completed",
      "lease.reaped",
    ]),
    "reference-host happy lifecycle ordering",
  );
  equal(happy.expected.terminalState, "reaped", "reference-host happy terminal state");

  const failed = requiredCase(byId, "failed-provision-cleanup-reap");
  requireEvents(failed, [
    "provision.failed",
    "cleanup.requested",
    "cleanup.completed",
    "lease.reaped",
  ]);
  equal(failed.expected.orphanedResources, 0, "failed lifecycle orphan count");

  const stale = requiredCase(byId, "stale-generation-rejected");
  equal(stale.expected.decision, "REJECT", "stale generation decision");
  equal(stale.expected.stateMutation, false, "stale generation mutation");

  const duplicate = requiredCase(byId, "duplicate-receipt-idempotency");
  equal(
    JSON.stringify(duplicate.receipts.map((value) => value.decision)),
    JSON.stringify(["APPLY", "DEDUPLICATE", "REJECT_CONFLICT"]),
    "duplicate receipt decisions",
  );
  equal(duplicate.expected.durableMutations, 1, "idempotent durable mutations");

  const pairing = requiredCase(byId, "pairing-secret-ephemeral-only");
  for (const forbidden of pairing.forbiddenDurableKeys) {
    if (Object.hasOwn(pairing.durableReceipt, forbidden)) {
      fail(`pairing durable receipt contains forbidden key ${forbidden}`);
    }
  }
  equal(pairing.expected.ephemeralResponsePersisted, false, "pairing secret persistence");

  const dpop = requiredCase(byId, "dpop-replay-revoke-fence");
  requireEvents(dpop, [
    "proof.replayed",
    "revocation.requested",
    "old-session.requested",
    "generation-heartbeat",
    "revocation.acknowledged",
  ]);
  equal(dpop.expected.proofReplayAccepted, false, "DPoP replay decision");
  equal(dpop.expected.oldSessionAccepted, false, "revoked session decision");
  equal(dpop.expected.staleGenerationAccepted, false, "stale heartbeat decision");
  equal(dpop.expected.endpointUnfenced, false, "endpoint fence decision");

  for (const fixture of lifecycle.cases) {
    if (!fixture.trace) continue;
    equal(
      JSON.stringify(fixture.trace.map((value) => value.sequence)),
      JSON.stringify(fixture.trace.map((_, index) => index + 1)),
      `contiguous lifecycle sequence ${fixture.id}`,
    );
  }
}

function requiredCase(byId, id) {
  const fixture = byId.get(id);
  if (!fixture) fail(`reference-host lifecycle omitted case ${id}`);
  return fixture;
}

function requireEvents(fixture, events) {
  const observed = new Set(fixture.trace.map((value) => value.event));
  for (const event of events) {
    if (!observed.has(event)) fail(`${fixture.id} omitted event ${event}`);
  }
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function jsonLines(path) {
  return readFileSync(path, "utf8")
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line));
}

function resolveContained(base, path) {
  const resolved = resolve(base, path);
  if (resolved !== base && !resolved.startsWith(`${base}/`))
    fail(`path escapes fixture root: ${path}`);
  return resolved;
}

function gitSucceeds(repo, arguments_) {
  return spawnSync("git", ["-C", repo, ...arguments_], { stdio: "ignore" }).status === 0;
}

function gitText(repo, arguments_) {
  return gitBytes(repo, arguments_).toString("utf8").trim();
}

function gitBytes(repo, arguments_) {
  const result = spawnSync("git", ["-C", repo, ...arguments_], {
    encoding: "buffer",
    maxBuffer: 64 * 1024 * 1024,
  });
  if (result.status !== 0) {
    fail(`git ${arguments_.join(" ")} failed in ${repo}: ${result.stderr.toString("utf8").trim()}`);
  }
  return result.stdout;
}

function sha256(bytes) {
  return createHash("sha256").update(bytes).digest("hex");
}

function equal(actual, expected, label) {
  if (actual !== expected)
    fail(`${label}: expected ${String(expected)}, received ${String(actual)}`);
}

function equalSet(actual, expected, label) {
  const left = [...new Set(actual)].sort();
  const right = [...new Set(expected)].sort();
  equal(JSON.stringify(left), JSON.stringify(right), label);
}

function requireArray(value, label) {
  if (!Array.isArray(value) || value.length === 0) fail(`${label} must be a non-empty array`);
}

function requireString(value, label) {
  if (typeof value !== "string" || value.length === 0) fail(`${label} must be a non-empty string`);
}

function fail(message) {
  throw new Error(`P0 baseline audit failed: ${message}`);
}
