#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const root = resolve(scriptDirectory, "../../../..");
const baselineDirectory = resolve(scriptDirectory, "../baseline");
const baselineReadmePath = join(baselineDirectory, "README.md");

const synara = readJson(join(baselineDirectory, "synara-legacy.json"));
const t3 = readJson(join(baselineDirectory, "t3-embedded.json"));
const referenceHost = readJson(join(baselineDirectory, "reference-host-negative.json"));
const synaraLinux = readJson(join(baselineDirectory, "synara-linux-amd64-execution.json"));
const t3Linux = readJson(join(baselineDirectory, "t3-linux-amd64-execution.json"));
const runtimeLinux = readJson(join(baselineDirectory, "runtime-linux-amd64-execution.json"));
auditBaselineReadme(baselineReadmePath);
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
const requiredCriterionMapping = {
  "synara-legacy-managed-agent": {
    "legacy-synara-mechanisms-indexed": "PASS",
    "fixed-linux-characterization": "EXECUTED_WITH_KNOWN_PRECONDITION_FAILURES",
    "real-provider-failure-resume-immutable": "NOT_RUN",
  },
  "t3-embedded-and-cloud-agent-consumer": {
    "t3-embedded-authority-indexed": "PASS",
    "fixed-linux-characterization": "PASS",
    "real-t3-turn-workspace-checkpoint-restart": "NOT_RUN",
  },
  "reference-host-negative-and-public-runtime": {
    "managed-host-greenfield-baseline": "SPEC_ONLY",
    "protocol-2.2-golden-corpus": "LOCAL_PASS_BOUND",
    "fixed-linux-characterization": "PASS",
    "raw-evidence-retained-immutable": "BOUND",
  },
};

const repositories = [synara, ...t3.repositories, referenceHost];
let gitFiles = 0;
for (const repository of repositories) {
  auditRepository(repository);
  gitFiles += repository.files.length;
}

const fixtureRoot = resolve(root, referenceHost.fixtureCorpus.root);
const fixtureBinding = referenceHost.fixtureCorpus.sourceBinding;
equal(fixtureBinding.status, "BOUND", "fixture corpus source binding");
equal(
  gitText(root, ["rev-parse", `${fixtureBinding.commit}^{commit}`]),
  fixtureBinding.commit,
  "fixture binding commit",
);
equal(
  gitText(root, ["rev-parse", `${fixtureBinding.commit}^{tree}`]),
  fixtureBinding.tree,
  "fixture binding tree",
);
requireArray(fixtureBinding.blobs, "fixture corpus blob map");
equalSet(
  fixtureBinding.blobs.map((value) => value.path),
  referenceHost.fixtureCorpus.files.map((value) => value.path),
  "fixture corpus blob paths",
);
const boundBlobs = new Map(fixtureBinding.blobs.map((value) => [value.path, value.blob]));
for (const fixture of referenceHost.fixtureCorpus.files) {
  const path = resolveContained(fixtureRoot, fixture.path);
  const bytes = readFileSync(path);
  const spec = `${fixtureBinding.commit}:${referenceHost.fixtureCorpus.root}/${fixture.path}`;
  equal(gitText(root, ["rev-parse", spec]), boundBlobs.get(fixture.path), `fixture blob ${spec}`);
  const committedBytes = gitBytes(root, ["show", spec]);
  equal(sha256(committedBytes), fixture.sha256, `committed fixture SHA-256 ${fixture.path}`);
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
    manifest.platformP0CharacterizationClosure.status,
    "COMPLETE",
    `${manifest.profile} Platform P0 characterization status`,
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
  auditCriterionMapping(manifest);
}

auditExecutionBinding(synara, synaraLinux);
auditExecutionBinding(t3, t3Linux);
auditExecutionBinding(referenceHost, runtimeLinux);
auditSynaraLinuxExecution(synaraLinux);
auditT3LinuxExecution(t3Linux);
auditRuntimeLinuxExecution(runtimeLinux);

process.stdout.write(
  `${JSON.stringify(
    {
      status: "PASS",
      audit: "p0-baseline-evidence-v3",
      repositories: repositories.length,
      gitFiles,
      fixtureFiles: referenceHost.fixtureCorpus.files.length,
      linuxExecutionReports: 3,
      fixedLinuxCharacterization: {
        synara: synaraLinux.status,
        t3: t3Linux.status,
        runtime: runtimeLinux.status,
      },
      platformP0CharacterizationClosure: true,
      m1BehaviorClosure: false,
      m1BehaviorStatus: "NOT_RUN",
      aggregateGateDecision: "NOT_CLAIMED",
    },
    null,
    2,
  )}\n`,
);

function auditBaselineReadme(path) {
  const text = readFileSync(path, "utf8");
  const required = [
    "platformP0CharacterizationClosure=true/status=COMPLETE",
    "platformP0CharacterizationClosure.complete=true/status=COMPLETE",
    "platformP0CharacterizationClosure.doesNotDecideAggregateGate=true",
    "m1BehaviorClosure.complete=false/status=NOT_RUN",
    "aggregateGateDecision=NOT_CLAIMED",
  ];
  for (const marker of required) {
    if (!text.includes(marker)) fail(`baseline README omitted current marker ${marker}`);
  }
  const stale = [
    "platformP0CharacterizationClosure=false",
    "platformP0CharacterizationClosure.complete=false/status=INCOMPLETE",
  ];
  for (const marker of stale) {
    if (text.includes(marker)) fail(`baseline README retained stale marker ${marker}`);
  }
}

function auditExecutionBinding(manifest, execution) {
  const binding = manifest.linuxExecutionEvidence;
  if (!binding) fail(`${manifest.profile} omitted linuxExecutionEvidence`);
  const path = resolveContained(root, binding.path);
  equal(
    sha256(readFileSync(path)),
    binding.sha256,
    `${manifest.profile} execution evidence SHA-256`,
  );
  equal(execution.status, binding.status, `${manifest.profile} execution evidence status`);
  equal(binding.rawLogsCommitted, false, `${manifest.profile} raw log retention boundary`);
  equal(execution.environment.host, "root@103.217.189.80", `${manifest.profile} Linux host`);
  equal(execution.environment.architecture, "amd64", `${manifest.profile} Linux architecture`);
  equal(
    execution.claimBoundary.realProviderExecuted,
    false,
    `${manifest.profile} Provider boundary`,
  );
  equal(
    execution.claimBoundary.platformP0BaselineClosed,
    false,
    `${manifest.profile} pre-review P0 closure boundary`,
  );
  equal(execution.claimBoundary.m1BaselineClosed, false, `${manifest.profile} M1 closure boundary`);
}

function auditSynaraLinuxExecution(execution) {
  equal(
    execution.schemaVersion,
    "cloud-agents.p0.synara-linux-characterization/v1",
    "Synara Linux schema",
  );
  equal(execution.status, "EXECUTED_WITH_KNOWN_PRECONDITION_FAILURES", "Synara Linux status");
  equal(
    execution.fixedSource.commit,
    "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0",
    "Synara Linux commit",
  );
  equal(
    execution.fixedSource.tree,
    "ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc",
    "Synara Linux tree",
  );
  equal(
    execution.fixedSource.archiveSha256,
    "cdcd4dd2d9571d45ab73f9cce36d43b4560f94d41aabc1291a7606d4d7427380",
    "Synara Linux archive",
  );
  equal(execution.fixedSource.remoteMaterializationHasGitMetadata, false, "Synara archive tree");
  equal(execution.fixedSourceModified, undefined, "Synara top-level mutation marker omitted");
  equal(
    execution.claimBoundary.fixedSourceModified,
    false,
    "Synara fixed source mutation boundary",
  );
  equal(execution.claimBoundary.temporaryPostgresSchemasRemaining, 0, "Synara database cleanup");
  const evidenceIndexPath = resolveContained(root, execution.claimBoundary.evidenceIndexPath);
  const evidenceIndexBytes = readFileSync(evidenceIndexPath);
  equal(
    sha256(evidenceIndexBytes),
    execution.claimBoundary.rawEvidenceIndexSha256,
    "Synara raw evidence index",
  );
  equal(execution.claimBoundary.remoteIndexVerified, true, "Synara remote evidence verification");
  const evidenceIndex = parseSha256Index(evidenceIndexBytes.toString("utf8"));
  equal(evidenceIndex.size, 21, "Synara evidence index entries");
  requireArray(execution.runs, "Synara Linux runs");
  const byId = new Map(execution.runs.map((value) => [value.id, value]));
  equal(byId.size, execution.runs.length, "Synara Linux unique run ids");
  for (const [id, passedTests] of [
    ["agentd-characterization", 118],
    ["kubernetes-allocation", 18],
    ["leadership-write-fence", 1],
    ["sqlite-deletion-fence", 1],
  ]) {
    const run = requiredCase(byId, id);
    equal(run.result, "PASS", `${id} result`);
    equal(run.passedTests, passedTests, `${id} passed tests`);
    equal(run.failedTests, 0, `${id} failed tests`);
    requireSha256(run.logSha256, `${id} log`);
    requireIndexedDigest(evidenceIndex, run.logSha256, `${id} log`);
  }
  const broad = requiredCase(byId, "legacy-broad-five-package-suite");
  equal(broad.result, "FAIL_KNOWN_PRECONDITION", "Synara broad suite result");
  equal(broad.passedTests, 934, "Synara broad suite passed tests");
  equal(broad.failedTests, 1, "Synara broad suite failed tests");
  requireIndexedDigest(evidenceIndex, broad.logSha256, "Synara broad suite log");
  for (const id of [
    "concurrent-claim-single-winner",
    "workspace-cleanup-concurrent-fence",
    "tenant-pinned-worker-claim",
    "suspend-completion-concurrent-single-winner",
  ]) {
    const run = requiredCase(byId, id);
    equal(run.result, "FAIL_KNOWN_PRECONDITION", `${id} result`);
    equal(run.passedTests, 0, `${id} passed tests`);
    equal(run.failedTests, 1, `${id} failed tests`);
    requireString(run.failure, `${id} failure boundary`);
    requireSha256(run.logSha256, `${id} log`);
    requireIndexedDigest(evidenceIndex, run.logSha256, `${id} log`);
  }
  requireIndexedDigest(
    evidenceIndex,
    requiredCase(byId, "tenant-pinned-worker-claim").postgresDiagnosticSha256,
    "Synara Postgres diagnostic",
  );
}

function auditT3LinuxExecution(execution) {
  equal(execution.schemaVersion, "cloud-agents.p0.t3-linux-characterization/v1", "T3 Linux schema");
  equal(execution.status, "PASS_CHARACTERIZATION_ONLY", "T3 Linux status");
  equal(execution.environment.node, "v24.13.1", "T3 Linux Node");
  equal(execution.environment.pnpm, "11.10.0", "T3 Linux pnpm");
  for (const [role, expected] of Object.entries({
    main: {
      commit: "8101cd044911c7dc2a2adf7c7a9ba7962abf57b6",
      tree: "e98f5650379f428bf5dcc6e7cae287c68fb8b080",
      archive: "87d78b9d2e9631ba10b84bad697afe271a0dac37d2b950aecc440577e61be1c2",
      packages: 1782,
    },
    feature: {
      commit: "9584a266e91fa94354e8c07f79af3a5e01755d16",
      tree: "171624a2dbfb68f1d91f0a67175cbaf68f2947c2",
      archive: "dd9d7ab0c174dcf82ca377d96b8a5531e3dd166b401c414ad490bcfa97bc4dc1",
      packages: 1790,
    },
  })) {
    const source = execution.fixedSources[role];
    equal(source.commit, expected.commit, `T3 ${role} commit`);
    equal(source.tree, expected.tree, `T3 ${role} tree`);
    equal(source.archiveSha256, expected.archive, `T3 ${role} archive`);
    equal(
      source.finalSourceMatchesArchiveExcludingNodeModules,
      true,
      `T3 ${role} final source identity`,
    );
    for (const field of [
      "canonicalFilesSha256",
      "canonicalModesSha256",
      "canonicalLinksSha256",
      "canonicalShapeSha256",
      "packageJsonSha256",
      "pnpmLockSha256",
      "pnpmWorkspaceSha256",
    ]) {
      requireSha256(source[field], `T3 ${role} ${field}`);
    }
    equal(execution.install[role].result, "PASS", `T3 ${role} install`);
    equal(execution.install[role].packages, expected.packages, `T3 ${role} install packages`);
    equal(execution.install[role].frozenLockfile, true, `T3 ${role} frozen install`);
    equal(execution.install[role].ignoreScripts, true, `T3 ${role} install scripts`);
    requireSha256(execution.install[role].logSha256, `T3 ${role} install log`);
  }
  equal(execution.runs.mainFocusedTests.result, "PASS", "T3 main focused tests");
  equal(execution.runs.mainFocusedTests.files, 7, "T3 main focused files");
  equal(execution.runs.mainFocusedTests.tests, 30, "T3 main focused tests count");
  equal(execution.runs.mainTypecheck.result, "PASS", "T3 main typecheck");
  equal(execution.runs.mainTypecheck.filters.length, 5, "T3 main typecheck filters");
  equal(execution.runs.featureFocusedTests.result, "PASS", "T3 feature focused tests");
  equal(execution.runs.featureFocusedTests.files, 5, "T3 feature focused files");
  equal(execution.runs.featureFocusedTests.tests, 39, "T3 feature focused tests count");
  equal(execution.runs.featureTypecheck.result, "PASS", "T3 feature typecheck");
  for (const run of Object.values(execution.runs)) {
    requireArray(run.logSha256, `T3 ${run.result} log hashes`);
    for (const digest of run.logSha256) requireSha256(digest, "T3 run log");
  }
  equal(execution.claimBoundary.characterizationOnly, true, "T3 characterization boundary");
  equal(execution.claimBoundary.credentialsUsed, false, "T3 credential boundary");
  for (const field of [
    "remoteResultsSha256",
    "commandResultsSha256",
    "logIndexSha256",
    "reportSha256",
    "finalEvidenceValidationLogSha256",
  ]) {
    requireSha256(execution.evidence[field], `T3 ${field}`);
  }
}

function auditRuntimeLinuxExecution(execution) {
  equal(
    execution.schemaVersion,
    "cloud-agents.p0.runtime-linux-characterization/v1",
    "Runtime Linux schema",
  );
  equal(execution.status, "PASS", "Runtime Linux status");
  equal(execution.environment.node, "v24.13.1", "Runtime Linux Node");
  equal(execution.environment.bun, "1.3.14", "Runtime Linux Bun");
  equal(
    execution.fixedSource.commit,
    "c2c03584656a0db04de2f6b84113ac932459eae6",
    "Runtime Linux commit",
  );
  equal(
    execution.fixedSource.tree,
    "2b3e3dbded35d97565f387abff497e31cc498126",
    "Runtime Linux tree",
  );
  equal(
    gitText(root, ["rev-parse", `${execution.fixedSource.commit}^{tree}`]),
    execution.fixedSource.tree,
    "Runtime Linux local Git binding",
  );
  equal(execution.fixedSource.fileCount, 209, "Runtime Linux file count");
  equal(execution.fixedSource.sourceCleanEquivalent, true, "Runtime Linux source identity");
  equal(execution.install.result, "PASS", "Runtime Linux install");
  equal(execution.install.frozenLockfile, true, "Runtime Linux frozen install");
  equal(execution.install.ignoreScripts, true, "Runtime Linux install scripts");
  const byId = new Map(execution.runs.map((value) => [value.id, value]));
  equal(requiredCase(byId, "protocol-tests").passedTests, 13, "Runtime protocol tests");
  equal(requiredCase(byId, "testkit-tests").passedTests, 5, "Runtime testkit tests");
  equal(requiredCase(byId, "runtime-tests").passedTests, 33, "Runtime tests");
  for (const run of execution.runs) {
    equal(run.result, "PASS", `Runtime ${run.id} result`);
    requireSha256(run.logSha256, `Runtime ${run.id} log`);
  }
  equal(
    execution.claimBoundary.existingRemoteCloudAgentsP0Modified,
    false,
    "Runtime remote worktree boundary",
  );
  for (const field of ["commandsSha256", "logIndexSha256", "remoteSummarySha256"]) {
    requireSha256(execution.evidence[field], `Runtime ${field}`);
  }
}

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
    manifest.requiredCoverage.correlationFields,
    ["requestId", "executionId", "generation", "commandId"],
    "correlation fields",
  );
  const transcriptById = new Map(transcripts.cases.map((value) => [value.id, value]));
  for (const [id, coverage] of [
    ["request-correlation-mismatch", "correlation-request-id"],
    ["execution-correlation-mismatch", "correlation-execution-id"],
    ["generation-correlation-mismatch", "correlation-generation"],
    ["command-correlation-mismatch", "correlation-command-id"],
  ]) {
    const fixture = requiredCase(transcriptById, id);
    equal(fixture.expected, "REJECT", `${id} decision`);
    if (!fixture.coverage.includes(coverage)) fail(`${id} omitted ${coverage}`);
  }
  const errorTerminal = requiredCase(transcriptById, "v23-valid-error-terminal");
  equal(errorTerminal.expected, "ACCEPT", "positive Error terminal decision");
  if (!errorTerminal.messages.some((value) => value.messageType === "Error")) {
    fail("positive Error terminal case omitted Error message");
  }
  const stopOutcomes = requiredCase(transcriptById, "v23-stop-four-outcomes");
  equal(
    JSON.stringify(stopOutcomes.messages.map((value) => value.payload)),
    JSON.stringify([
      { outcome: "quiesced", quiesced: true, graceful: true },
      { outcome: "forced", quiesced: false, graceful: false },
      { outcome: "timed-out", quiesced: false, graceful: false },
      { outcome: "failed", quiesced: false, graceful: false },
    ]),
    "StopSession outcome matrix",
  );
  equal(
    requiredCase(transcriptById, "stop-invalid-outcome").expected,
    "REJECT",
    "invalid StopSession outcome",
  );
  equal(
    requiredCase(transcriptById, "stop-contradictory-flags").expected,
    "REJECT",
    "contradictory StopSession flags",
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

  const restart = requiredCase(byId, "controller-restart-replays-durable-receipts");
  requireEvents(restart, [
    "controller.stopped",
    "controller.started",
    "journal.replayed",
    "receipt.deduplicated",
    "reconciliation.completed",
  ]);
  equal(restart.expected.durableMutationsAfterReplay, 0, "restart replay durable mutations");
  equal(restart.expected.duplicateSideEffects, 0, "restart replay duplicate side effects");

  const partial = requiredCase(byId, "partial-allocation-reconciles-orphans");
  requireEvents(partial, [
    "workload.allocated",
    "volume.allocated",
    "allocation.failed",
    "orphan.scan.completed",
    "volume.deleted",
    "workload.deleted",
    "reconciliation.completed",
  ]);
  equal(partial.expected.orphanedResources, 0, "partial allocation orphan count");

  const resources = requiredCase(byId, "resources-have-independent-lifecycles");
  requireArray(resources.resourceLifecycles, "independent resource lifecycles");
  equalSet(
    resources.resourceLifecycles.map((value) => value.resourceKind),
    ["workload", "volume", "endpoint", "grant"],
    "independent resource kinds",
  );
  equal(
    new Set(resources.resourceLifecycles.map((value) => value.lifecycleId)).size,
    resources.resourceLifecycles.length,
    "independent resource lifecycle ids",
  );
  for (const resource of resources.resourceLifecycles) {
    equal(resource.states.at(-1), "deleted", `${resource.resourceKind} terminal lifecycle state`);
  }

  for (const fixture of lifecycle.cases) {
    if (!fixture.trace) continue;
    equal(
      JSON.stringify(fixture.trace.map((value) => value.sequence)),
      JSON.stringify(fixture.trace.map((_, index) => index + 1)),
      `contiguous lifecycle sequence ${fixture.id}`,
    );
  }
}

function auditCriterionMapping(manifest) {
  requireArray(manifest.criterionMapping, `${manifest.profile} criterion mapping`);
  const observed = Object.fromEntries(
    manifest.criterionMapping.map((value) => {
      requireString(value.criterion, `${manifest.profile} criterion`);
      requireString(value.result, `${manifest.profile} criterion ${value.criterion} result`);
      requireString(value.evidence, `${manifest.profile} criterion ${value.criterion} evidence`);
      return [value.criterion, value.result];
    }),
  );
  equal(
    JSON.stringify(observed),
    JSON.stringify(requiredCriterionMapping[manifest.profile]),
    `${manifest.profile} criterion mapping`,
  );
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

function parseSha256Index(text) {
  const entries = new Map();
  for (const line of text.trim().split("\n")) {
    const match = /^([0-9a-f]{64}) {2}(.+)$/.exec(line);
    if (!match) fail(`invalid SHA-256 index line: ${line}`);
    const [, digest, path] = match;
    if (entries.has(path)) fail(`duplicate SHA-256 index path: ${path}`);
    entries.set(path, digest);
  }
  return entries;
}

function requireIndexedDigest(index, digest, label) {
  requireSha256(digest, label);
  if (![...index.values()].includes(digest)) fail(`${label} is absent from the SHA-256 index`);
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

function requireSha256(value, label) {
  if (typeof value !== "string" || !/^[0-9a-f]{64}$/.test(value)) {
    fail(`${label} must be a lowercase SHA-256 digest`);
  }
}

function fail(message) {
  throw new Error(`P0 baseline audit failed: ${message}`);
}
