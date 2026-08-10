#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  accessSync,
  chmodSync,
  constants,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  realpathSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative, resolve, sep } from "node:path";
import { gunzipSync } from "node:zlib";

const SOURCE_COMMIT = "2c50b1eb54ed3228719bb55cc8bdcd1b0babc8e0";
const SOURCE_TREE = "ba41fc168ea65978b1f17fdb8abc5afbc22ca9cc";
const INVENTORY_ROWS = 8625;
const INVENTORY_SHA256 = "bee237da890f4f3d62fd524fd11142a6b6c883e82790e5d455c415461ae7b4e5";
const LICENSE_BLOB = "960499447d8ea8f6ce86017893f132f0c3885fef";
const LICENSE_TEXT_SHA256 = "305724dd050ca7ded99c662de813d755bc4ec3887c4543a37159c6662ca36d1b";
const NODE_VERSION = "v24.13.1";
const GITLEAKS_VERSION = "8.30.1";
const LICENSE_PROVENANCE = `MIT@${SOURCE_COMMIT}:LICENSE#blob=${LICENSE_BLOB};sha256=${LICENSE_TEXT_SHA256}`;

const repositoryRoot = resolve(import.meta.dirname, "../../../..");
const sourceRoot = resolve(
  process.env.SYNARA_P0_SOURCE ??
    "/Users/huang/devel/project/huang/business/synara-cloud-agent-external-runtime",
);
const planRoot = resolve(
  process.env.CLOUD_AGENTS_P0_OUTPUT ?? join(repositoryRoot, "docs/plan/p0"),
);
const outputRoot = join(planRoot, "provenance");
const inventoryPath = resolve(
  process.env.SYNARA_P0_INVENTORY ?? join(planRoot, "synara-file-inventory.tsv"),
);
const decisionsPath = resolve(
  process.env.SYNARA_P0_DECISIONS ?? join(planRoot, "synara-inventory-decisions.tsv"),
);
const triagePath = resolve(
  process.env.SYNARA_P0_SECRET_TRIAGE ??
    join(planRoot, "provenance/synara-extraction-secret-triage.json"),
);
const secretTriageReference = "docs/plan/p0/provenance/synara-extraction-secret-triage.json";

const outputPaths = {
  audit: join(outputRoot, "synara-extraction-source-audit.json"),
  summary: join(outputRoot, "synara-extraction-source-audit.zh-CN.md"),
  secrets: join(outputRoot, "synara-extraction-secret-scan-sanitized.json"),
  toolchain: join(outputRoot, "synara-extraction-toolchain-lock.json"),
  hashes: join(outputRoot, "synara-extraction-generated-evidence.sha256"),
};

const gitleaksPlatforms = {
  "darwin-arm64": {
    url: "https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_darwin_arm64.tar.gz",
    archiveSha256: "b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5",
    binarySha256: "ba52fb1bfabbcde42f032afad3d6e0b19dff8ed105229a16e7caa338bbc0e84f",
    alternativeBinaries: [
      {
        sha256: "f414bc2fb952be6c9072b75cb411e3368614ef4b16d48dbd9ad238034afd2302",
        distribution: "homebrew-core/arm64_tahoe",
        containerSha256: "ea543daa28d39acc7af3aab4491ef53d62c0402b540d087008ff4dce7e2484b3",
      },
    ],
  },
  "linux-x64": {
    url: "https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_linux_x64.tar.gz",
    archiveSha256: "551f6fc83ea457d62a0d98237cbad105af8d557003051f41f3e7ca7b3f2470eb",
    binarySha256: "88f91962aa2f93ac6ab281d553b9e125f5197bbbce38f9f2437f7299c32e5509",
  },
};

const allowedClassifications = new Set([
  "move",
  "rewrite-public",
  "adapter",
  "synara-only",
  "retire",
  "deferred-public-extension",
]);
const allowedOwners = new Set([
  "public-core",
  "public-platform-adapter",
  "synara-host",
  "runtime-release",
  "deferred",
]);
const publicTargetPrefixes = [
  "contracts/",
  "sdk/go/",
  "services/control-plane/",
  "services/worker/",
  "deploy/",
  "conformance/",
  "tools/",
];

const temporaryRoot = mkdtempSync(join(tmpdir(), "cloud-agent-p0-synara-extraction-"));
chmodSync(temporaryRoot, 0o700);
mkdirSync(outputRoot, { recursive: true });

const checks = [];
const blockers = [];
let sourceEvidence = null;
let inventoryEvidence = null;
let decisionEvidence = null;
let licenseEvidence = null;
let toolchain = {
  schemaVersion: 1,
  status: "BLOCKED",
  requiredRuntime: { node: NODE_VERSION, gitleaks: GITLEAKS_VERSION },
  tools: {},
};
let secretEvidence = {
  schemaVersion: 1,
  status: "BLOCKED",
  scanner: null,
  configuration: {
    defaultRules: true,
    repositoryConfigUsed: false,
    wholeDirectoryAllowlistUsed: false,
  },
  scopes: [],
  note: "审计未完成；未持久化 raw finding。",
};

try {
  const nodeTool = auditNode();
  const gitTool = auditGitTool();
  toolchain.tools.node = nodeTool;
  toolchain.tools.git = gitTool;
  if (nodeTool.status !== "PASS") {
    throw new Error(`Node runtime must be exactly ${NODE_VERSION}`);
  }

  const gitleaksTool = await acquireGitleaks();
  toolchain.tools.gitleaks = publicGitleaksTool(gitleaksTool);
  toolchain.status = "PASS";
  pass("toolchain", `Node ${NODE_VERSION} and Gitleaks ${GITLEAKS_VERSION} verified.`);

  sourceEvidence = auditSource(gitTool.path);
  const triage = auditSecretTriage();
  const inventory = auditInventory(gitTool.path);
  inventoryEvidence = inventory.evidence;
  const decisions = auditDecisions(inventory.rows);
  decisionEvidence = decisions.evidence;
  licenseEvidence = auditLicense(inventory.blobs);

  const scanInputs = materializeScanInputs(gitTool.path, inventory, decisions);
  secretEvidence = auditSecrets(gitleaksTool.path, scanInputs, triage);
  if (secretEvidence.status === "BLOCKED") {
    block(
      "secret-provenance",
      `${secretEvidence.totalFindingCount} sanitized finding(s) or scanner failure(s) remain.`,
    );
  } else {
    pass(
      "secret-provenance",
      "All required Gitleaks scopes completed and every exact finding has an unexpired disposition.",
    );
  }
} catch (error) {
  block("audit-execution", safeError(error));
} finally {
  rmSync(temporaryRoot, { recursive: true, force: true });
}

const status = blockers.length === 0 ? "PASS" : "BLOCKED";
const generatedAt = new Date().toISOString();
const audit = {
  schemaVersion: 1,
  kind: "cloud-agent-p0-synara-extraction-source-provenance-audit",
  generatedAt,
  status,
  fixedInput: {
    sourceCommit: SOURCE_COMMIT,
    sourceTree: SOURCE_TREE,
    inventoryRows: INVENTORY_ROWS,
    inventorySha256: INVENTORY_SHA256,
    decisionSha256: decisionEvidence?.sha256 ?? null,
  },
  source: sourceEvidence,
  inventory: inventoryEvidence,
  decisions: decisionEvidence,
  license: licenseEvidence,
  secretScanning: {
    status: secretEvidence.status,
    scanner: secretEvidence.scanner,
    scopeCount: secretEvidence.scopes.length,
    totalFindingCount: secretEvidence.totalFindingCount ?? null,
    triage: secretEvidence.triage ?? null,
    rawReportsPersisted: false,
  },
  checks,
  blockers: [...new Set(blockers)].toSorted(),
  restrictions: [
    "Never graft or publish the Synara source Git history.",
    "Rewrite or delete static test private-key bytes before any selected source is published.",
    "Re-run extracted-tree and artifact scans after P1 rewrites; exact decisions expire on their recorded dates.",
  ],
  claimBoundary: {
    sourceLicenseProvenanceComplete: licenseEvidence?.status === "PASS",
    secretProvenanceComplete: ["PASS", "PASS_WITH_RESTRICTIONS"].includes(secretEvidence.status),
    sourceHistoryImportAuthorized: false,
    selectedSourceDirectCopyAuthorized: secretEvidence.selectedSourceDirectCopyAuthorized === true,
    thirdPartyDependencyLicenseCleared: false,
    publicationAuthorized: false,
  },
};

toolchain.status = toolchain.status === "PASS" && toolchain.tools.gitleaks ? "PASS" : "BLOCKED";
writeEvidence(audit, secretEvidence, toolchain);
process.exitCode = status === "PASS" ? 0 : 2;

function auditNode() {
  const digest = existsSync(process.execPath) ? sha256(readFileSync(process.execPath)) : null;
  const status = process.version === NODE_VERSION && digest ? "PASS" : "BLOCKED";
  return { status, version: process.version, sha256: digest };
}

function auditGitTool() {
  const path = findExecutable("git");
  if (!path) throw new Error("git executable not found");
  const version = run(path, ["--version"]).stdout.trim();
  return {
    status: "PASS",
    path,
    version,
    sha256: sha256(readFileSync(realpathSync(path))),
  };
}

async function acquireGitleaks() {
  const platformKey = `${process.platform}-${process.arch}`;
  const pinned = gitleaksPlatforms[platformKey];
  if (!pinned) {
    throw new Error(`unsupported Gitleaks platform: ${platformKey}`);
  }

  let path;
  let acquisition;
  let archiveSha256 = null;
  if (process.env.P0_GITLEAKS_BIN) {
    path = resolve(process.env.P0_GITLEAKS_BIN);
    acquisition = "P0_GITLEAKS_BIN";
  } else {
    const response = await fetch(pinned.url, {
      redirect: "follow",
      signal: AbortSignal.timeout(60_000),
    });
    if (!response.ok) {
      throw new Error(`Gitleaks download failed with HTTP ${response.status}`);
    }
    const archive = Buffer.from(await response.arrayBuffer());
    archiveSha256 = sha256(archive);
    if (archiveSha256 !== pinned.archiveSha256) {
      throw new Error("Gitleaks archive SHA-256 mismatch");
    }
    path = join(temporaryRoot, "gitleaks");
    writeFileSync(path, extractTarGzMember(archive, "gitleaks"), { mode: 0o700 });
    chmodSync(path, 0o700);
    acquisition = "pinned-download";
  }

  const binarySha256 = sha256(readFileSync(path));
  const binaryDistribution =
    binarySha256 === pinned.binarySha256
      ? "upstream-release"
      : pinned.alternativeBinaries?.find((value) => value.sha256 === binarySha256);
  if (!binaryDistribution) {
    throw new Error(`Gitleaks binary SHA-256 mismatch for ${platformKey}`);
  }
  const version = run(path, ["version"]).stdout.trim();
  if (version !== GITLEAKS_VERSION) {
    throw new Error(`Gitleaks version mismatch: ${version || "missing"}`);
  }
  return {
    path,
    version,
    binarySha256,
    binaryDistribution,
    expectedArchiveSha256: pinned.archiveSha256,
    verifiedArchiveSha256: archiveSha256,
    acquisition,
    source: acquisition === "pinned-download" ? pinned.url : "P0_GITLEAKS_BIN",
    platform: platformKey,
  };
}

function publicGitleaksTool(tool) {
  return {
    status: "PASS",
    version: tool.version,
    sha256: tool.binarySha256,
    binaryDistribution: tool.binaryDistribution,
    expectedArchiveSha256: tool.expectedArchiveSha256,
    verifiedArchiveSha256: tool.verifiedArchiveSha256,
    acquisition: tool.acquisition,
    source: tool.source,
    platform: tool.platform,
  };
}

function auditSource(gitPath) {
  const head = git(gitPath, ["rev-parse", "HEAD"]).stdout.trim();
  const tree = git(gitPath, ["rev-parse", "HEAD^{tree}"]).stdout.trim();
  const status = git(gitPath, ["status", "--porcelain=v1", "--untracked-files=all"]).stdout;
  const shallow = git(gitPath, ["rev-parse", "--is-shallow-repository"]).stdout.trim() === "true";
  git(gitPath, ["cat-file", "-e", `${SOURCE_COMMIT}^{commit}`]);
  const historyCommitCount = Number(
    git(gitPath, ["rev-list", "--count", SOURCE_COMMIT]).stdout.trim(),
  );
  if (head !== SOURCE_COMMIT) throw new Error(`source HEAD mismatch: ${head}`);
  if (tree !== SOURCE_TREE) throw new Error(`source tree mismatch: ${tree}`);
  if (status !== "") throw new Error("source checkout is not clean");
  if (shallow) throw new Error("source checkout is shallow; reachable history is incomplete");
  if (!Number.isSafeInteger(historyCommitCount) || historyCommitCount < 1) {
    throw new Error("fixed source reachable history count is invalid");
  }
  pass("source-identity", "Fixed source HEAD/tree and clean non-shallow checkout verified.");
  return {
    status: "PASS",
    commit: head,
    tree,
    clean: true,
    shallow: false,
    reachableHistoryCommitCount: historyCommitCount,
  };
}

function auditInventory(gitPath) {
  const encoded = readFileSync(inventoryPath);
  const digest = sha256(encoded);
  if (digest !== INVENTORY_SHA256) {
    throw new Error(`inventory SHA-256 mismatch: ${digest}`);
  }
  const parsed = parseTsv(encoded.toString("utf8"), "inventory");
  requireHeaders(parsed.headers, [
    "snapshotHead",
    "snapshotTree",
    "path",
    "gitMode",
    "gitBlobOid",
    "sha256",
    "bytes",
  ]);
  if (parsed.rows.length !== INVENTORY_ROWS) {
    throw new Error(`inventory row mismatch: ${parsed.rows.length}/${INVENTORY_ROWS}`);
  }

  const treeEntries = readTree(gitPath);
  if (treeEntries.size !== INVENTORY_ROWS) {
    throw new Error(`fixed tree tracked path mismatch: ${treeEntries.size}/${INVENTORY_ROWS}`);
  }
  const rowPaths = new Set();
  for (const row of parsed.rows) {
    validateSafeRelativePath(row.path, "inventory path");
    if (rowPaths.has(row.path)) throw new Error(`duplicate inventory path: ${row.path}`);
    rowPaths.add(row.path);
    if (row.snapshotHead !== SOURCE_COMMIT || row.snapshotTree !== SOURCE_TREE) {
      throw new Error(`inventory snapshot mismatch: ${row.path}`);
    }
    const entry = treeEntries.get(row.path);
    if (!entry) throw new Error(`inventory path absent from source tree: ${row.path}`);
    if (entry.type !== "blob" || entry.oid !== row.gitBlobOid || entry.mode !== row.gitMode) {
      throw new Error(`inventory Git identity mismatch: ${row.path}`);
    }
  }
  for (const path of treeEntries.keys()) {
    if (!rowPaths.has(path)) throw new Error(`tracked path absent from inventory: ${path}`);
  }

  const blobs = readBlobs(gitPath, [...new Set(parsed.rows.map((row) => row.gitBlobOid))]);
  for (const row of parsed.rows) {
    const blob = blobs.get(row.gitBlobOid);
    if (!blob) throw new Error(`source blob unavailable: ${row.gitBlobOid}`);
    if (sha256(blob) !== row.sha256) throw new Error(`blob SHA-256 mismatch: ${row.path}`);
    if (blob.length !== Number(row.bytes)) throw new Error(`blob byte count mismatch: ${row.path}`);
  }
  pass(
    "inventory-blob-closure",
    `${INVENTORY_ROWS} tracked paths and every blob OID/SHA-256/byte count verified.`,
  );
  return {
    rows: parsed.rows,
    blobs,
    evidence: {
      status: "PASS",
      rows: parsed.rows.length,
      sha256: digest,
      trackedPathSetEqualsInventoryPathSet: true,
      everyBlobOidVerified: true,
      everyContentSha256Verified: true,
      everyByteCountVerified: true,
    },
  };
}

function auditDecisions(inventoryRows) {
  const encoded = readFileSync(decisionsPath);
  const parsed = parseTsv(encoded.toString("utf8"), "decisions");
  requireHeaders(parsed.headers, [
    "snapshotHead",
    "snapshotTree",
    "path",
    "finalCapability",
    "finalClassification",
    "owner",
    "target",
    "provenance",
    "authorityDisposition",
    "licenseProvenance",
    "secretProvenance",
    "decisionSource",
    "reviewStatus",
    "reviewer",
    "reviewedAt",
    "reason",
  ]);
  if (parsed.rows.length !== INVENTORY_ROWS) {
    throw new Error(`decision row mismatch: ${parsed.rows.length}/${INVENTORY_ROWS}`);
  }
  const inventoryByPath = new Map(inventoryRows.map((row) => [row.path, row]));
  const paths = new Set();
  const targets = new Set();
  let selectedCount = 0;
  const classifications = {};
  for (const row of parsed.rows) {
    const inventory = inventoryByPath.get(row.path);
    if (!inventory) throw new Error(`decision path absent from inventory: ${row.path}`);
    if (paths.has(row.path)) throw new Error(`duplicate decision path: ${row.path}`);
    if (targets.has(row.target)) throw new Error(`duplicate decision target: ${row.target}`);
    paths.add(row.path);
    targets.add(row.target);
    if (row.snapshotHead !== SOURCE_COMMIT || row.snapshotTree !== SOURCE_TREE) {
      throw new Error(`decision snapshot mismatch: ${row.path}`);
    }
    if (!allowedClassifications.has(row.finalClassification)) {
      throw new Error(`invalid final classification: ${row.path}`);
    }
    if (!allowedOwners.has(row.owner)) throw new Error(`invalid decision owner: ${row.path}`);
    validateTarget(row.target, row.path);
    for (const field of [
      "finalCapability",
      "authorityDisposition",
      "licenseProvenance",
      "secretProvenance",
      "decisionSource",
      "reviewStatus",
      "reviewer",
      "reviewedAt",
      "reason",
    ]) {
      if (!row[field]?.trim() || row[field] === "-" || /unknown|unclassified/iu.test(row[field])) {
        throw new Error(`unresolved decision field ${field}: ${row.path}`);
      }
    }
    const expectedProvenance = `${SOURCE_COMMIT}:${row.path}#blob=${inventory.gitBlobOid};sha256=${inventory.sha256}`;
    if (row.provenance !== expectedProvenance) {
      throw new Error(`decision source provenance mismatch: ${row.path}`);
    }
    const selected = !["synara-only", "retire"].includes(row.finalClassification);
    if (selected) {
      selectedCount += 1;
      if (row.licenseProvenance !== LICENSE_PROVENANCE) {
        throw new Error(`selected decision license provenance mismatch: ${row.path}`);
      }
      const expectedSecretProvenance =
        row.path === "scripts/stage3-provider-acceptance/test_vault_audit_acceptance_sink.py"
          ? `REWRITE_REQUIRED_BEFORE_PUBLICATION: static test private-key bytes; ${secretTriageReference}`
          : `AUDITED_EXACT_FINDINGS: ${secretTriageReference}`;
      if (row.secretProvenance !== expectedSecretProvenance) {
        throw new Error(`selected decision secret provenance state mismatch: ${row.path}`);
      }
    }
    classifications[row.finalClassification] = (classifications[row.finalClassification] ?? 0) + 1;
  }
  if (paths.size !== INVENTORY_ROWS || inventoryRows.some((row) => !paths.has(row.path))) {
    throw new Error("decision path set does not equal inventory path set");
  }
  const digest = sha256(encoded);
  pass(
    "decision-closure",
    `${INVENTORY_ROWS} unique paths/targets and ${selectedCount} selected candidates verified.`,
  );
  return {
    rows: parsed.rows,
    evidence: {
      status: "PASS",
      rows: parsed.rows.length,
      sha256: digest,
      uniquePaths: true,
      uniqueTargets: true,
      completeClassificationAndProvenance: true,
      selectedCandidateRows: selectedCount,
      classifications,
    },
  };
}

function auditLicense(blobs) {
  const blob = blobs.get(LICENSE_BLOB);
  if (!blob) throw new Error("fixed LICENSE blob is absent from inventory closure");
  if (sha256(blob) !== LICENSE_TEXT_SHA256) throw new Error("fixed LICENSE text SHA-256 mismatch");
  const text = blob.toString("utf8");
  if (!text.includes("MIT License") || !text.includes("Permission is hereby granted")) {
    throw new Error("fixed LICENSE content is not recognizable MIT text");
  }
  pass("source-license", "Fixed source LICENSE blob and MIT text SHA-256 verified.");
  return {
    status: "PASS",
    spdxExpression: "MIT",
    path: "LICENSE",
    blobOid: LICENSE_BLOB,
    textSha256: LICENSE_TEXT_SHA256,
    provenance: LICENSE_PROVENANCE,
    scope: "fixed Synara extraction source only",
    thirdPartyDependencyLicenseCleared: false,
  };
}

function auditSecretTriage() {
  const encoded = readFileSync(triagePath);
  const value = JSON.parse(encoded.toString("utf8"));
  if (
    value.schemaVersion !== 1 ||
    value.kind !== "cloud-agent-p0-synara-extraction-secret-triage"
  ) {
    throw new Error("secret triage schema/kind mismatch");
  }
  if (value.sourceCommit !== SOURCE_COMMIT) throw new Error("secret triage source commit mismatch");
  if (
    value.policy?.wholeDirectoryAllowlist !== false ||
    value.policy?.wholeRuleAllowlist !== false ||
    value.policy?.rawSecretPersisted !== false ||
    value.policy?.publicHistoryImportAuthorized !== false ||
    value.policy?.unknownFindingAction !== "fail-closed"
  ) {
    throw new Error("secret triage policy is not fail-closed");
  }
  if (!Array.isArray(value.groups) || value.groups.length < 1) {
    throw new Error("secret triage groups are missing");
  }
  const allowedDispositions = new Set([
    "ACCEPT_EXACT_CONTEXT_FALSE_POSITIVE",
    "REWRITE_REQUIRED_BEFORE_PUBLICATION",
    "SOURCE_HISTORY_QUARANTINE",
  ]);
  const decisions = new Map();
  for (const group of value.groups) {
    for (const field of ["reasonCode", "reason", "disposition", "owner", "expiresAt"]) {
      if (!safeString(group[field])) throw new Error(`secret triage group missing ${field}`);
    }
    if (!allowedDispositions.has(group.disposition)) {
      throw new Error(`secret triage disposition is invalid: ${group.disposition}`);
    }
    const expiry = Date.parse(`${group.expiresAt}T23:59:59Z`);
    if (!Number.isFinite(expiry) || expiry < Date.now()) {
      throw new Error(`secret triage decision expired: ${group.reasonCode}`);
    }
    if (!Array.isArray(group.fingerprintSha256) || group.fingerprintSha256.length < 1) {
      throw new Error(`secret triage fingerprints missing: ${group.reasonCode}`);
    }
    for (const fingerprintSha256 of group.fingerprintSha256) {
      if (!/^[0-9a-f]{64}$/u.test(fingerprintSha256)) {
        throw new Error(`secret triage fingerprint hash is invalid: ${group.reasonCode}`);
      }
      if (decisions.has(fingerprintSha256)) {
        throw new Error(`duplicate secret triage fingerprint hash: ${fingerprintSha256}`);
      }
      decisions.set(fingerprintSha256, {
        reasonCode: group.reasonCode,
        reason: group.reason,
        disposition: group.disposition,
        owner: group.owner,
        expiresAt: group.expiresAt,
      });
    }
  }
  if (decisions.size !== 56) {
    throw new Error(`secret triage decision count mismatch: ${decisions.size}/56`);
  }
  pass("secret-triage", "56 exact sanitized finding hashes have reviewed dispositions.");
  return {
    sha256: sha256(encoded),
    reviewer: value.reviewer,
    reviewedAt: value.reviewedAt,
    policy: value.policy,
    decisions,
  };
}

function materializeScanInputs(gitPath, inventory, decisions) {
  const archivePath = join(temporaryRoot, "fixed-source-tree.tar");
  git(gitPath, ["archive", "--format=tar", `--output=${archivePath}`, SOURCE_COMMIT]);
  const archiveSha256 = sha256(readFileSync(archivePath));
  const fullTreeRoot = join(temporaryRoot, "fixed-source-tree-archive");
  const selectedTreeRoot = join(temporaryRoot, "public-candidate-selected-tree");
  mkdirSync(fullTreeRoot, { recursive: true, mode: 0o700 });
  mkdirSync(selectedTreeRoot, { recursive: true, mode: 0o700 });
  for (const row of inventory.rows) {
    writeMaterializedBlob(fullTreeRoot, row.path, inventory.blobs.get(row.gitBlobOid));
  }
  const selected = decisions.rows.filter(
    (row) => !["synara-only", "retire"].includes(row.finalClassification),
  );
  const inventoryByPath = new Map(inventory.rows.map((row) => [row.path, row]));
  for (const decision of selected) {
    const row = inventoryByPath.get(decision.path);
    writeMaterializedBlob(selectedTreeRoot, decision.path, inventory.blobs.get(row.gitBlobOid));
  }
  return {
    archivePath,
    archiveSha256,
    fullTreeRoot,
    selectedTreeRoot,
    selectedCount: selected.length,
    gitHistoryRoot: sourceRoot,
    historyCommit: SOURCE_COMMIT,
  };
}

function auditSecrets(gitleaksPath, inputs, triage) {
  const scopes = [
    {
      name: "fixed-source-tree-archive",
      mode: "dir",
      root: inputs.fullTreeRoot,
      expectedFileCount: INVENTORY_ROWS,
      archiveSha256: inputs.archiveSha256,
      materialization: "all fixed-tree tracked blobs as safe regular files",
    },
    {
      name: "fixed-commit-reachable-git-history",
      mode: "git",
      root: inputs.gitHistoryRoot,
      commit: inputs.historyCommit,
    },
    {
      name: "public-candidate-selected-tree",
      mode: "dir",
      root: inputs.selectedTreeRoot,
      expectedFileCount: inputs.selectedCount,
      materialization: "all selected candidate blobs at their original source paths",
    },
  ];
  const sanitizedScopes = [];
  let totalFindingCount = 0;
  for (const scope of scopes) {
    const rawPath = join(temporaryRoot, `raw-${scope.name}.json`);
    const args =
      scope.mode === "git"
        ? [
            "git",
            scope.root,
            `--log-opts=${scope.commit}`,
            "--redact=100",
            "--report-format=json",
            `--report-path=${rawPath}`,
            "--no-banner",
            "--exit-code=0",
          ]
        : [
            "dir",
            scope.root,
            "--redact=100",
            "--report-format=json",
            `--report-path=${rawPath}`,
            "--no-banner",
            "--exit-code=0",
          ];
    const scan = run(gitleaksPath, args, { allowFailure: true, timeout: 300_000 });
    try {
      if (scan.status !== 0 || !existsSync(rawPath)) {
        sanitizedScopes.push({
          name: scope.name,
          status: "BLOCKED",
          findingCount: null,
          findings: [],
          scannerExecutionCompleted: false,
        });
        continue;
      }
      const raw = JSON.parse(readFileSync(rawPath, "utf8"));
      if (!Array.isArray(raw)) throw new Error(`Gitleaks report is not an array: ${scope.name}`);
      const findings = raw
        .map((finding) => {
          const sanitized = sanitizeFinding(finding, scope);
          const decision = triage.decisions.get(sanitized.fingerprintSha256);
          return {
            ...sanitized,
            ...(decision ? { triage: decision } : { triage: { disposition: "UNREVIEWED" } }),
          };
        })
        .toSorted((left, right) => left.fingerprintSha256.localeCompare(right.fingerprintSha256));
      totalFindingCount += findings.length;
      const hasUnreviewed = findings.some((finding) => finding.triage.disposition === "UNREVIEWED");
      const hasRestriction = findings.some((finding) =>
        ["REWRITE_REQUIRED_BEFORE_PUBLICATION", "SOURCE_HISTORY_QUARANTINE"].includes(
          finding.triage.disposition,
        ),
      );
      sanitizedScopes.push({
        name: scope.name,
        status:
          findings.length === 0
            ? "PASS"
            : hasUnreviewed
              ? "BLOCKED"
              : hasRestriction
                ? "REVIEWED_WITH_RESTRICTIONS"
                : "REVIEWED",
        findingCount: findings.length,
        findings,
        scannerExecutionCompleted: true,
        ...(scope.expectedFileCount ? { selectedOrTrackedFileCount: scope.expectedFileCount } : {}),
        ...(scope.archiveSha256 ? { sourceArchiveSha256: scope.archiveSha256 } : {}),
        ...(scope.commit ? { reachableFromCommit: scope.commit } : {}),
        ...(scope.materialization ? { materialization: scope.materialization } : {}),
      });
    } finally {
      rmSync(rawPath, { force: true });
    }
  }
  const observedHashes = new Set(
    sanitizedScopes.flatMap((scope) => scope.findings.map((finding) => finding.fingerprintSha256)),
  );
  const missingTriageHashes = [...observedHashes].filter((value) => !triage.decisions.has(value));
  const unobservedTriageHashes = [...triage.decisions.keys()].filter(
    (value) => !observedHashes.has(value),
  );
  const scannerComplete =
    sanitizedScopes.length === 3 &&
    sanitizedScopes.every((scope) => scope.scannerExecutionCompleted === true);
  const dispositionCounts = {};
  for (const scope of sanitizedScopes) {
    for (const finding of scope.findings) {
      const disposition = finding.triage.disposition;
      dispositionCounts[disposition] = (dispositionCounts[disposition] ?? 0) + 1;
    }
  }
  const hasRestrictions = Object.keys(dispositionCounts).some((value) =>
    ["REWRITE_REQUIRED_BEFORE_PUBLICATION", "SOURCE_HISTORY_QUARANTINE"].includes(value),
  );
  const status =
    scannerComplete && missingTriageHashes.length === 0 && unobservedTriageHashes.length === 0
      ? hasRestrictions
        ? "PASS_WITH_RESTRICTIONS"
        : "PASS"
      : "BLOCKED";
  const selectedScope = sanitizedScopes.find(
    (scope) => scope.name === "public-candidate-selected-tree",
  );
  const selectedRequiresRewrite =
    selectedScope?.findings.some(
      (finding) => finding.triage.disposition === "REWRITE_REQUIRED_BEFORE_PUBLICATION",
    ) ?? true;
  return {
    schemaVersion: 1,
    status,
    scanner: {
      name: "gitleaks",
      version: toolchain.tools.gitleaks.version,
      sha256: toolchain.tools.gitleaks.sha256,
    },
    configuration: {
      defaultRules: true,
      repositoryConfigUsed: false,
      wholeDirectoryAllowlistUsed: false,
      wholeRuleAllowlistUsed: false,
    },
    triage: {
      sha256: triage.sha256,
      reviewer: triage.reviewer,
      reviewedAt: triage.reviewedAt,
      decisionCount: triage.decisions.size,
      dispositionCounts,
      missingTriageCount: missingTriageHashes.length,
      unobservedTriageCount: unobservedTriageHashes.length,
    },
    scopes: sanitizedScopes,
    totalFindingCount,
    rawReportsPersisted: false,
    sourceHistoryImportAuthorized: false,
    selectedSourceDirectCopyAuthorized: !selectedRequiresRewrite,
    note:
      status === "PASS"
        ? "三项固定范围均为零 finding。"
        : status === "PASS_WITH_RESTRICTIONS"
          ? "全部 finding 已按 exact fingerprint hash 独立复核；源历史禁止导入，静态测试私钥来源必须在公开前重写。"
          : "存在未复核 finding、triage 漂移或扫描器执行失败；仅保存安全字段。",
  };
}

function sanitizeFinding(finding, scope) {
  const ruleId = safeString(finding?.RuleID);
  const path = sanitizeFindingPath(safeString(finding?.File), scope);
  const line = Number.isSafeInteger(finding?.StartLine) ? finding.StartLine : null;
  const commit = /^[0-9a-f]{40}$/u.test(safeString(finding?.Commit)) ? finding.Commit : null;
  // Gitleaks directory fingerprints include the temporary materialization root.
  // Hash only the normalized, non-secret identity so replaying the same blobs in
  // another 0700 temp directory yields the same review key.
  const canonicalFingerprint = JSON.stringify({ scope: scope.name, ruleId, path, line, commit });
  return {
    ruleId,
    path,
    line,
    commit,
    fingerprintSha256: sha256Text(canonicalFingerprint),
  };
}

function sanitizeFindingPath(value, scope) {
  if (!value) return "(unknown)";
  if (!value.startsWith("/")) return value.replace(/^\.\//u, "");
  const absolute = resolve(value);
  const relativePath = relative(scope.root, absolute);
  if (relativePath && !relativePath.startsWith(`..${sep}`) && relativePath !== "..") {
    return relativePath.split(sep).join("/");
  }
  return `(external-path:${sha256Text(value)})`;
}

function writeEvidence(auditValue, secretsValue, toolchainValue) {
  atomicJson(outputPaths.toolchain, publicToolchain(toolchainValue));
  atomicJson(outputPaths.secrets, secretsValue);
  atomicJson(outputPaths.audit, auditValue);
  atomicText(outputPaths.summary, renderSummary(auditValue, secretsValue));
  const hashedPaths = [
    outputPaths.audit,
    outputPaths.summary,
    outputPaths.secrets,
    outputPaths.toolchain,
    inventoryPath,
    decisionsPath,
    triagePath,
    resolve(import.meta.dirname, "finalize-synara-secret-triage.mjs"),
    resolve(import.meta.filename),
  ];
  const manifest = `${hashedPaths
    .map((path) => `${sha256(readFileSync(path))}  ${relative(outputRoot, path)}`)
    .join("\n")}\n`;
  atomicText(outputPaths.hashes, manifest);
}

function publicToolchain(value) {
  return {
    schemaVersion: value.schemaVersion,
    status: value.status,
    requiredRuntime: value.requiredRuntime,
    tools: {
      ...(value.tools.node ? { node: value.tools.node } : {}),
      ...(value.tools.git
        ? {
            git: {
              status: value.tools.git.status,
              version: value.tools.git.version,
              sha256: value.tools.git.sha256,
            },
          }
        : {}),
      ...(value.tools.gitleaks ? { gitleaks: value.tools.gitleaks } : {}),
    },
  };
}

function renderSummary(auditValue, secretsValue) {
  const scopeLines =
    secretsValue.scopes.length === 0
      ? "- 尚未完成三个扫描范围。"
      : secretsValue.scopes
          .map(
            (scope) =>
              `- \`${scope.name}\`：${scope.status}，finding=${scope.findingCount ?? "unknown"}`,
          )
          .join("\n");
  return `# Synara extraction source P0 provenance 审计

## 结论

- 状态：**${auditValue.status}**
- 固定 source：\`${SOURCE_COMMIT}\` / tree \`${SOURCE_TREE}\`
- 全量 inventory：${auditValue.inventory?.rows ?? "未完成"} rows，固定 SHA-256 ${auditValue.inventory?.sha256 ?? INVENTORY_SHA256}
- 决策表：${auditValue.decisions?.rows ?? "未完成"} rows，运行时计算 SHA-256 ${auditValue.decisions?.sha256 ?? "未完成"}
- Source license provenance：${auditValue.claimBoundary.sourceLicenseProvenanceComplete ? "完整" : "未完成"}
- Secret provenance：${auditValue.claimBoundary.secretProvenanceComplete ? "完整" : "未完成"}

## Secret 扫描

${scopeLines}

raw Gitleaks report 已在临时目录删除；仓库内仅保留 rule、path、line、commit 与 fingerprint SHA-256。扫描使用 Gitleaks 默认规则，没有 repository config 或整目录 allowlist。

全部 ${secretsValue.totalFindingCount ?? "unknown"} 条 finding 均以 exact fingerprint SHA-256 映射到独立 triage；没有整目录/整规则豁免。源历史禁止导入，静态测试私钥来源必须删除或改为运行时生成后才能公开。triage SHA-256：\`${secretsValue.triage?.sha256 ?? "未完成"}\`。

## License 与声明边界

固定 \`LICENSE\` blob \`${LICENSE_BLOB}\` 的 MIT 文本 SHA-256 为 \`${LICENSE_TEXT_SHA256}\`。该结论只证明固定 Synara extraction source 的 license provenance；没有证明第三方 dependency/license 已 cleared，也不授权 publication。

- \`sourceLicenseProvenanceComplete=${auditValue.claimBoundary.sourceLicenseProvenanceComplete}\`
- \`secretProvenanceComplete=${auditValue.claimBoundary.secretProvenanceComplete}\`
- \`sourceHistoryImportAuthorized=false\`
- \`selectedSourceDirectCopyAuthorized=${auditValue.claimBoundary.selectedSourceDirectCopyAuthorized}\`
- \`thirdPartyDependencyLicenseCleared=false\`
- \`publicationAuthorized=false\`

## Blockers

${auditValue.blockers.length === 0 ? "- 无。" : auditValue.blockers.map((item) => `- ${item}`).join("\n")}

## Restrictions

${auditValue.restrictions.map((item) => `- ${item}`).join("\n")}
`;
}

function readTree(gitPath) {
  const output = git(gitPath, ["ls-tree", "-rz", "--full-tree", SOURCE_COMMIT], {
    encoding: null,
  }).stdout;
  const entries = new Map();
  for (const item of splitNull(Buffer.from(output))) {
    const tab = item.indexOf(0x09);
    if (tab < 0) throw new Error("invalid git ls-tree record");
    const metadata = item.subarray(0, tab).toString("ascii").split(" ");
    const path = item.subarray(tab + 1).toString("utf8");
    if (metadata.length !== 3) throw new Error(`invalid git ls-tree metadata: ${path}`);
    if (entries.has(path)) throw new Error(`duplicate fixed tree path: ${path}`);
    entries.set(path, { mode: metadata[0], type: metadata[1], oid: metadata[2] });
  }
  return entries;
}

function readBlobs(gitPath, oids) {
  const input = Buffer.from(`${oids.join("\n")}\n`, "ascii");
  const output = git(gitPath, ["cat-file", "--batch"], { encoding: null, input }).stdout;
  const buffer = Buffer.from(output);
  const blobs = new Map();
  let offset = 0;
  for (const requestedOid of oids) {
    const newline = buffer.indexOf(0x0a, offset);
    if (newline < 0) throw new Error(`truncated git cat-file header: ${requestedOid}`);
    const header = buffer.subarray(offset, newline).toString("ascii").split(" ");
    if (header.length !== 3 || header[0] !== requestedOid || header[1] !== "blob") {
      throw new Error(`invalid git cat-file header: ${requestedOid}`);
    }
    const size = Number(header[2]);
    const start = newline + 1;
    const end = start + size;
    if (!Number.isSafeInteger(size) || buffer[end] !== 0x0a) {
      throw new Error(`invalid git cat-file payload: ${requestedOid}`);
    }
    blobs.set(requestedOid, Buffer.from(buffer.subarray(start, end)));
    offset = end + 1;
  }
  if (offset !== buffer.length) throw new Error("unexpected trailing git cat-file output");
  return blobs;
}

function parseTsv(encoded, label) {
  if (encoded.includes("\r")) throw new Error(`${label} contains CR characters`);
  const lines = encoded.trimEnd().split("\n");
  if (lines.length < 2) throw new Error(`${label} is empty`);
  const headers = lines[0].split("\t");
  const rows = lines.slice(1).map((line, index) => {
    const values = line.split("\t");
    if (values.length !== headers.length) {
      throw new Error(`${label} row ${index + 2} column mismatch`);
    }
    return Object.fromEntries(headers.map((header, column) => [header, values[column]]));
  });
  return { headers, rows };
}

function requireHeaders(actual, required) {
  for (const header of required) {
    if (!actual.includes(header)) throw new Error(`required TSV header is missing: ${header}`);
  }
}

function validateTarget(target, sourcePath) {
  if (!target?.trim() || target === "-" || target.startsWith("/")) {
    throw new Error(`invalid decision target: ${sourcePath}`);
  }
  if (/(?:^|\/)\.\.(?:\/|$)/u.test(target) || /unknown|unclassified/iu.test(target)) {
    throw new Error(`unsafe or unresolved decision target: ${sourcePath}`);
  }
  if (
    !target.startsWith("hxp0618/synara:") &&
    !publicTargetPrefixes.some((prefix) => target.startsWith(prefix))
  ) {
    throw new Error(`decision target outside approved roots: ${sourcePath}`);
  }
}

function validateSafeRelativePath(path, label) {
  if (!path || path.startsWith("/") || path.includes("\0") || /(?:^|\/)\.\.(?:\/|$)/u.test(path)) {
    throw new Error(`unsafe ${label}: ${JSON.stringify(path)}`);
  }
}

function writeMaterializedBlob(root, path, blob) {
  validateSafeRelativePath(path, "materialization path");
  if (!Buffer.isBuffer(blob)) throw new Error(`materialization blob missing: ${path}`);
  const destination = resolve(root, path);
  if (destination !== root && !destination.startsWith(`${root}${sep}`)) {
    throw new Error(`materialization escaped root: ${path}`);
  }
  mkdirSync(dirname(destination), { recursive: true, mode: 0o700 });
  writeFileSync(destination, blob, { mode: 0o600 });
}

function extractTarGzMember(archive, memberName) {
  const tar = gunzipSync(archive);
  for (let offset = 0; offset + 512 <= tar.length;) {
    const header = tar.subarray(offset, offset + 512);
    if (header.every((byte) => byte === 0)) break;
    const name = readTarString(header.subarray(0, 100));
    const prefix = readTarString(header.subarray(345, 500));
    const fullName = prefix ? `${prefix}/${name}` : name;
    const sizeText = readTarString(header.subarray(124, 136)).trim();
    const size = sizeText ? Number.parseInt(sizeText, 8) : 0;
    if (!Number.isSafeInteger(size) || size < 0)
      throw new Error("invalid Gitleaks tar member size");
    const dataStart = offset + 512;
    const dataEnd = dataStart + size;
    if (dataEnd > tar.length) throw new Error("truncated Gitleaks tar archive");
    if ((fullName === memberName || fullName.endsWith(`/${memberName}`)) && header[156] !== 0x35) {
      return Buffer.from(tar.subarray(dataStart, dataEnd));
    }
    offset = dataStart + Math.ceil(size / 512) * 512;
  }
  throw new Error(`Gitleaks tar member not found: ${memberName}`);
}

function readTarString(buffer) {
  const end = buffer.indexOf(0);
  return buffer.subarray(0, end < 0 ? buffer.length : end).toString("utf8");
}

function splitNull(buffer) {
  const result = [];
  let start = 0;
  for (let index = 0; index < buffer.length; index += 1) {
    if (buffer[index] === 0) {
      result.push(buffer.subarray(start, index));
      start = index + 1;
    }
  }
  if (start !== buffer.length) throw new Error("NUL-delimited output is truncated");
  return result;
}

function git(gitPath, args, options = {}) {
  return run(gitPath, ["-C", sourceRoot, ...args], options);
}

function run(command, args, options = {}) {
  const result = spawnSync(command, args, {
    encoding: Object.hasOwn(options, "encoding") ? options.encoding : "utf8",
    input: options.input,
    maxBuffer: 1024 * 1024 * 1024,
    timeout: options.timeout ?? 120_000,
    env: { ...process.env, LC_ALL: "C", LANG: "C" },
  });
  if (result.error || (!options.allowFailure && result.status !== 0)) {
    throw new Error(`${commandLabel(command)} execution failed`);
  }
  return result;
}

function findExecutable(name) {
  for (const directory of (process.env.PATH ?? "").split(":").filter(Boolean)) {
    const candidate = join(directory, name);
    try {
      accessSync(candidate, constants.X_OK);
      return candidate;
    } catch {
      // Continue searching PATH.
    }
  }
  return null;
}

function atomicJson(path, value) {
  atomicText(path, `${JSON.stringify(value, null, 2)}\n`);
}

function atomicText(path, value) {
  const temporaryPath = `${path}.tmp-${process.pid}`;
  writeFileSync(temporaryPath, value, { mode: 0o644 });
  renameSync(temporaryPath, path);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function sha256Text(value) {
  return sha256(Buffer.from(value, "utf8"));
}

function safeString(value) {
  return typeof value === "string" ? value : "";
}

function safeError(error) {
  const message = error instanceof Error ? error.message : String(error);
  return message.replaceAll(sourceRoot, "<fixed-source>").slice(0, 500);
}

function commandLabel(command) {
  return command.split(sep).at(-1) || "command";
}

function pass(name, detail) {
  checks.push({ name, status: "PASS", detail });
}

function block(name, detail) {
  checks.push({ name, status: "BLOCKED", detail });
  blockers.push(`${name}: ${detail}`);
}
