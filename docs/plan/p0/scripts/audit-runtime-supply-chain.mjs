#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  chmodSync,
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, relative, resolve } from "node:path";

const SOURCE_COMMIT = "49e8cdc6a3a4f88c7324d055ce519e9f25a8ca8a";
const SOURCE_TAG = "cloud-agent-m1-rc.1";
const TAG_OBJECT = "ac64d6f2fd29f3a1b9d2e514efe2c72eb6118d62";
const CANDIDATE_DIGEST = "sha256:b9931233d46aeaf1392197095483c2e3409f628a47b2ba92c8e57bb38b444676";
const repositoryRoot = resolve(import.meta.dirname, "../../../..");
const DEFAULT_RELEASE_DIRECTORY = resolve(
  repositoryRoot,
  "../_releases/cloud-agent-m1-rc.1-49e8cdc",
);
const PUBLIC_PACKAGES = new Set([
  "@synara/cloud-agent-protocol",
  "@synara/cloud-agent-provider-api",
  "@synara/cloud-agent-runtime",
  "@synara/cloud-agent-provider-codex",
  "@synara/cloud-agent-provider-claude",
  "@synara/cloud-agent-testkit",
  "@synara/cloud-agent-distribution",
]);
const LICENSE_ALLOWLIST = new Set([
  "0BSD",
  "Apache-2.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "CC0-1.0",
  "ISC",
  "MIT",
  "Unlicense",
]);
const LOCAL_DEPENDENCY = /^(?:workspace|catalog|file|link|portal):/u;
const EXACT_SEMVER =
  /^(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/u;

const PINNED_LOCAL_TOOLS = {
  node: {
    path: process.execPath,
    version: "v24.13.1",
    sha256: "d36b3d980963d44bd2c5e844fac4cfeee26a167b744287a4e74a9575af9d0559",
  },
  bun: {
    path: "/opt/homebrew/bin/bun",
    version: "1.3.14",
    sha256: "e0c90ec15d33363e6b70713d56bc3b2c7585c17f40a0fe0f8fd9305901d4e233",
  },
  git: {
    path: "/opt/homebrew/bin/git",
    version: "git version 2.55.0",
    sha256: "9048038886ac36210fbb616b49b0707465f63683cb04e33a2013baf95f746938",
  },
  gh: {
    path: "/opt/homebrew/bin/gh",
    version: "gh version 2.97.0 (2026-07-31)",
    sha256: "6a2ab5fa89553eac1f0df50a26a5eaeea9a665d8971f5a51b32487b72c708f5c",
  },
  oxfmt: {
    path: join(repositoryRoot, "node_modules/.bin/oxfmt"),
    version: "Version: 0.62.0",
    sha256: "ecab4e2f1bebaab1a3306620bee562ec16e8cb83dd5fb739b94a5aae3c9e34bd",
  },
};

const PINNED_DOWNLOAD_TOOLS = {
  gitleaks: {
    version: "8.30.1",
    environment: "P0_GITLEAKS_BIN",
    url: "https://github.com/gitleaks/gitleaks/releases/download/v8.30.1/gitleaks_8.30.1_darwin_arm64.tar.gz",
    archiveSha256: "b40ab0ae55c505963e365f271a8d3846efbc170aa17f2607f13df610a9aeb6a5",
    binarySha256: "ba52fb1bfabbcde42f032afad3d6e0b19dff8ed105229a16e7caa338bbc0e84f",
    archive: true,
    member: "gitleaks",
  },
  syft: {
    version: "1.50.0",
    environment: "P0_SYFT_BIN",
    url: "https://github.com/anchore/syft/releases/download/v1.50.0/syft_1.50.0_darwin_arm64.tar.gz",
    archiveSha256: "e32fdb9d47823fa633748a1efca2528fd77c37469ea93c9e40ab835da44e4cce",
    binarySha256: "5d59c9e6fa641793ddb48bc90b5b7ad63bf7303a52835b75b1beee3757463998",
    archive: true,
    member: "syft",
  },
  osv: {
    version: "2.5.0",
    environment: "P0_OSV_SCANNER_BIN",
    url: "https://github.com/google/osv-scanner/releases/download/v2.5.0/osv-scanner_darwin_arm64",
    archiveSha256: "fff5a2e351b7f0a60001e87cbf862e82fb82e2792d368b533fec7a5865a73da2",
    binarySha256: "fff5a2e351b7f0a60001e87cbf862e82fb82e2792d368b533fec7a5865a73da2",
    archive: false,
    member: "osv-scanner",
  },
};

const options = parseOptions(process.argv.slice(2));
const temporaryRoot = mkdtempSync(join(tmpdir(), "cloud-agent-p0-supply-chain-"));
chmodSync(temporaryRoot, 0o700);
const checks = [];
const blockers = [];
const warnings = [];

let secretReport = {
  schemaVersion: 1,
  status: "BLOCKED",
  scanner: null,
  scopes: [],
  note: "Scanner did not run.",
};
let licenseInventory = {
  schemaVersion: 1,
  sourceCommit: SOURCE_COMMIT,
  status: "BLOCKED",
  packages: [],
  missingLicenseText: [],
  blockedPackages: [],
};
let productionSbom = emptySbom();
let notices = "# THIRD_PARTY_NOTICES\n\nAudit did not complete.\n";
let toolchain = { schemaVersion: 1, status: "BLOCKED", tools: {} };
let audit;

try {
  assertDarwinArm64();
  mkdirSync(options.outputDirectory, { recursive: true, mode: 0o755 });

  const localTools = verifyLocalTools();
  const downloadedTools = acquireDownloadTools();
  toolchain = {
    schemaVersion: 1,
    status:
      Object.values(localTools).every((item) => item.status === "PASS") &&
      Object.values(downloadedTools).every((item) => item.status === "PASS")
        ? "PASS"
        : "BLOCKED",
    tools: { ...localTools, ...downloadedTools },
  };

  const source = auditSourceIdentity(localTools.git?.path);
  const release = auditReleaseAssets(source.sourceDirectory);
  const dependency = auditDependencyClosure(source.sourceDirectory, release, localTools.bun?.path);
  ({ licenseInventory, notices } = auditLicenses(dependency, release));
  productionSbom = buildProductionSbom(dependency, release, licenseInventory, source);

  const sbomPath = join(temporaryRoot, "sbom-production.spdx.json");
  writeFileSync(sbomPath, `${JSON.stringify(productionSbom, null, 2)}\n`, { mode: 0o600 });

  secretReport = auditSecrets(
    downloadedTools.gitleaks?.path,
    source.sourceDirectory,
    release.extractedDirectory,
  );
  const syft = auditWithSyft(downloadedTools.syft?.path, release.extractedDirectory);
  const vulnerabilities = auditVulnerabilities(downloadedTools.osv?.path, sbomPath);
  const workflow = auditWorkflow(source.sourceDirectory);
  const signatures = auditGitSignatures(localTools.git?.path);
  const provenance = auditProvenance(release);
  const attestations = auditGitHubAttestations(localTools.gh?.path, localTools.node?.path, release);

  const overallStatus = blockers.length === 0 ? "PASS" : "BLOCKED";
  audit = {
    schemaVersion: 1,
    kind: "cloud-agent-runtime-p0-supply-chain-audit",
    generatedAt: new Date().toISOString(),
    status: overallStatus,
    fixedInput: {
      repository: "hxp0618/cloud-agents",
      sourceCommit: SOURCE_COMMIT,
      sourceTree: source.sourceTree,
      tag: SOURCE_TAG,
      tagObject: TAG_OBJECT,
      candidateDigest: CANDIDATE_DIGEST,
      releaseDirectoryLabel: basename(options.releaseDirectory),
      releaseDirectorySha256: release.directoryDigest,
    },
    tools: publicToolchain(toolchain),
    source: publicSourceReport(source),
    release: publicReleaseReport(release),
    dependency: publicDependencyReport(dependency),
    licenses: {
      status: licenseInventory.status,
      packageCount: licenseInventory.packages.length,
      blockedPackageCount: licenseInventory.blockedPackages.length,
      missingLicenseTextCount: licenseInventory.missingLicenseText.length,
      thirdPartyNoticeGenerated: true,
    },
    sbom: {
      status: checkStatus("production-sbom"),
      packageCount: productionSbom.packages.length,
      relationshipCount: productionSbom.relationships.length,
      existingCandidatePackageCount: release.existingSbomPackageCount,
      existingCandidateRelationshipCount: release.existingSbomRelationshipCount,
      standaloneCompositionStatus: checkStatus("standalone-bundle-composition"),
    },
    secretScanning: secretReport,
    independentInventory: syft,
    vulnerabilities,
    workflow,
    signatures,
    provenance,
    attestations,
    checks,
    blockers: [...new Set(blockers)].toSorted(),
    warnings: [...new Set(warnings)].toSorted(),
    claimBoundary: {
      runtimeSupplyChainInventoryComplete: true,
      sourceAndEightLocalArtifactHashesVerified: checkStatus("release-asset-integrity") === "PASS",
      productionDependencyGraphResolved: checkStatus("production-dependency-closure") === "PASS",
      hostPlatformExternalPackagesMaterialized:
        dependency.materializedPackageCount === dependency.externalPackageCount - 7,
      licenseCleared: false,
      trustedAttestationVerified: false,
      publicationOrDeploymentAuthorized: false,
      releaseAuthorization: false,
    },
  };
} catch (error) {
  block("audit-execution", errorMessage(error));
  audit = {
    schemaVersion: 1,
    kind: "cloud-agent-runtime-p0-supply-chain-audit",
    generatedAt: new Date().toISOString(),
    status: "BLOCKED",
    fixedInput: {
      repository: "hxp0618/cloud-agents",
      sourceCommit: SOURCE_COMMIT,
      tag: SOURCE_TAG,
      tagObject: TAG_OBJECT,
      candidateDigest: CANDIDATE_DIGEST,
      releaseDirectoryLabel: basename(options.releaseDirectory),
    },
    tools: publicToolchain(toolchain),
    checks,
    blockers: [...new Set(blockers)].toSorted(),
    warnings: [...new Set(warnings)].toSorted(),
    fatalError: sanitizeError(errorMessage(error)),
    claimBoundary: {
      runtimeSupplyChainInventoryComplete: false,
      licenseCleared: false,
      trustedAttestationVerified: false,
      publicationOrDeploymentAuthorized: false,
      releaseAuthorization: false,
    },
  };
} finally {
  writeEvidence();
  rmSync(temporaryRoot, { recursive: true, force: true });
}

process.stdout.write(
  `${JSON.stringify(
    {
      status: audit.status,
      outputDirectory: options.outputDirectory,
      checks: checks.length,
      blockers: audit.blockers.length,
    },
    null,
    2,
  )}\n`,
);
if (audit.status !== "PASS") process.exitCode = 2;

function parseOptions(args) {
  let releaseDirectory = DEFAULT_RELEASE_DIRECTORY;
  let outputDirectory = resolve(repositoryRoot, "docs/plan/p0/provenance");
  for (let index = 0; index < args.length; index += 1) {
    const option = args[index];
    if (option !== "--release-dir" && option !== "--output-dir") {
      throw new Error(`Unknown option: ${String(option)}`);
    }
    const value = args[index + 1];
    if (!value || value.startsWith("--")) throw new Error(`${option} requires a path.`);
    if (option === "--release-dir") releaseDirectory = resolve(value);
    else outputDirectory = resolve(value);
    index += 1;
  }
  return { releaseDirectory, outputDirectory };
}

function assertDarwinArm64() {
  if (process.platform !== "darwin" || process.arch !== "arm64") {
    throw new Error(
      `Pinned audit binaries only cover darwin-arm64; found ${process.platform}-${process.arch}.`,
    );
  }
}

function verifyLocalTools() {
  const result = {};
  for (const [name, expected] of Object.entries(PINNED_LOCAL_TOOLS)) {
    try {
      if (!existsSync(expected.path)) throw new Error(`${expected.path} is missing.`);
      const digest = sha256File(expected.path);
      if (digest !== expected.sha256) {
        throw new Error(`${name} binary digest is ${digest}; expected ${expected.sha256}.`);
      }
      const version =
        name === "node"
          ? run(expected.path, ["--version"]).stdout.trim()
          : name === "bun"
            ? run(expected.path, ["--version"]).stdout.trim()
            : name === "git"
              ? run(expected.path, ["--version"]).stdout.trim()
              : run(expected.path, ["--version"]).stdout.split("\n")[0].trim();
      if (version !== expected.version) {
        throw new Error(`${name} version is ${version}; expected ${expected.version}.`);
      }
      result[name] = { status: "PASS", path: expected.path, version, sha256: digest };
      pass(`tool-${name}`, `${name} ${version} binary digest is pinned.`);
    } catch (error) {
      result[name] = {
        status: "BLOCKED",
        path: expected.path,
        version: expected.version,
        sha256: expected.sha256,
        reason: sanitizeError(errorMessage(error)),
      };
      block(`tool-${name}`, errorMessage(error));
    }
  }
  return result;
}

function acquireDownloadTools() {
  const result = {};
  for (const [name, expected] of Object.entries(PINNED_DOWNLOAD_TOOLS)) {
    try {
      const supplied = process.env[expected.environment];
      let binaryPath;
      let acquisition;
      if (supplied) {
        binaryPath = resolve(supplied);
        acquisition = `environment:${expected.environment}`;
      } else {
        const toolRoot = join(temporaryRoot, `tool-${name}`);
        mkdirSync(toolRoot, { mode: 0o700 });
        const downloadPath = join(toolRoot, expected.archive ? "download.tar.gz" : expected.member);
        downloadPinned(expected.url, downloadPath);
        const archiveDigest = sha256File(downloadPath);
        if (archiveDigest !== expected.archiveSha256) {
          throw new Error(
            `${name} download digest is ${archiveDigest}; expected ${expected.archiveSha256}.`,
          );
        }
        if (expected.archive) {
          run("tar", ["-xzf", downloadPath, "-C", toolRoot, expected.member]);
          binaryPath = join(toolRoot, expected.member);
        } else {
          binaryPath = downloadPath;
        }
        chmodSync(binaryPath, 0o700);
        acquisition = "pinned-download";
      }
      const binaryDigest = sha256File(binaryPath);
      if (binaryDigest !== expected.binarySha256) {
        throw new Error(
          `${name} binary digest is ${binaryDigest}; expected ${expected.binarySha256}.`,
        );
      }
      result[name] = {
        status: "PASS",
        path: binaryPath,
        version: expected.version,
        sha256: binaryDigest,
        archiveSha256: expected.archiveSha256,
        source: expected.url,
        acquisition,
      };
      pass(`tool-${name}`, `${name} ${expected.version} binary and archive digests are pinned.`);
    } catch (error) {
      result[name] = {
        status: "BLOCKED",
        version: expected.version,
        sha256: expected.binarySha256,
        archiveSha256: expected.archiveSha256,
        source: expected.url,
        reason: sanitizeError(errorMessage(error)),
      };
      block(`tool-${name}`, errorMessage(error));
    }
  }
  return result;
}

function downloadPinned(url, target) {
  const result = run(
    "curl",
    [
      "--fail",
      "--silent",
      "--show-error",
      "--location",
      "--connect-timeout",
      "10",
      "--max-time",
      "180",
      "--retry",
      "3",
      "--retry-all-errors",
      "--output",
      target,
      url,
    ],
    { allowFailure: true, timeout: 240_000 },
  );
  if (result.status !== 0) {
    throw new Error(`Pinned download failed with status ${result.status}.`);
  }
}

function auditSourceIdentity(gitPath) {
  if (!gitPath) throw new Error("Pinned git is unavailable.");
  const commitType = run(gitPath, ["cat-file", "-t", SOURCE_COMMIT], {
    cwd: repositoryRoot,
  }).stdout;
  if (commitType.trim() !== "commit") throw new Error(`${SOURCE_COMMIT} is not a commit.`);
  const sourceTree = run(gitPath, ["rev-parse", `${SOURCE_COMMIT}^{tree}`], {
    cwd: repositoryRoot,
  }).stdout.trim();
  const tagType = run(gitPath, ["cat-file", "-t", SOURCE_TAG], { cwd: repositoryRoot }).stdout;
  const tagObject = run(gitPath, ["rev-parse", SOURCE_TAG], { cwd: repositoryRoot }).stdout.trim();
  const peeledTag = run(gitPath, ["rev-parse", `${SOURCE_TAG}^{commit}`], {
    cwd: repositoryRoot,
  }).stdout.trim();
  if (tagType.trim() !== "tag" || tagObject !== TAG_OBJECT || peeledTag !== SOURCE_COMMIT) {
    throw new Error("RC tag object/type/peeled commit does not match the fixed input.");
  }

  const sourceDirectory = join(temporaryRoot, "source-49e8cdc");
  mkdirSync(sourceDirectory, { mode: 0o700 });
  const archive = run(gitPath, ["archive", "--format=tar", SOURCE_COMMIT], {
    cwd: repositoryRoot,
    encoding: null,
    maxBuffer: 64 * 1024 * 1024,
  }).stdout;
  const archivePath = join(temporaryRoot, "source.tar");
  writeFileSync(archivePath, archive, { mode: 0o600 });
  run("tar", ["-xf", archivePath, "-C", sourceDirectory]);

  const refs = run(
    gitPath,
    [
      "for-each-ref",
      "--format=%(refname) %(objectname)",
      "refs/heads",
      "refs/remotes",
      "refs/tags",
    ],
    { cwd: repositoryRoot },
  )
    .stdout.split("\n")
    .filter(Boolean)
    .toSorted();
  pass("source-identity", `Fixed source ${SOURCE_COMMIT} and annotated tag object are present.`);

  const provenanceText = readFileSync(join(sourceDirectory, "SOURCE_PROVENANCE.md"), "utf8");
  const provenanceRows = [...provenanceText.matchAll(/`([^`]+)`\s*\|\s*`([0-9a-f]{40})`/gu)];
  const sourceRepo = resolve(repositoryRoot, "../synara");
  let provenanceObjectsVerified = 0;
  if (existsSync(join(sourceRepo, ".git")) || existsSync(sourceRepo)) {
    for (const [, , object] of provenanceRows) {
      const probe = run(gitPath, ["cat-file", "-e", `${object}^{object}`], {
        cwd: sourceRepo,
        allowFailure: true,
      });
      if (probe.status === 0) provenanceObjectsVerified += 1;
    }
  }
  if (provenanceRows.length === 11 && provenanceObjectsVerified === 11) {
    pass(
      "source-provenance-objects",
      "All 11 declared import objects exist in the Synara object store.",
    );
  } else {
    block(
      "source-provenance-objects",
      `Verified ${provenanceObjectsVerified}/${provenanceRows.length} declared import objects.`,
    );
  }

  return {
    status: checkStatus("source-identity"),
    sourceCommit: SOURCE_COMMIT,
    sourceTree,
    tag: SOURCE_TAG,
    tagType: tagType.trim(),
    tagObject,
    peeledCommit: peeledTag,
    sourceArchiveSha256: sha256File(archivePath),
    localRefCount: refs.length,
    provenanceObjectsDeclared: provenanceRows.length,
    provenanceObjectsVerified,
    sourceDirectory,
  };
}

function auditReleaseAssets(sourceDirectory) {
  if (!existsSync(options.releaseDirectory)) {
    throw new Error(`Release directory is missing: ${options.releaseDirectory}`);
  }
  const candidate = readJson(join(options.releaseDirectory, "candidate-manifest.json"));
  const provenance = readJson(join(options.releaseDirectory, "provenance.json"));
  const existingSbom = readJson(join(options.releaseDirectory, "sbom.spdx.json"));
  if (
    candidate.sourceCommit !== SOURCE_COMMIT ||
    candidate.sourceDirty !== false ||
    candidate.candidateDigest !== CANDIDATE_DIGEST
  ) {
    throw new Error(
      "Candidate manifest does not bind the fixed clean source and candidate digest.",
    );
  }
  if (!Array.isArray(candidate.packages) || candidate.packages.length !== 7) {
    throw new Error("Candidate manifest must list exactly seven packages.");
  }

  const expectedArtifacts = [];
  const packageManifests = new Map();
  const extractedDirectory = join(temporaryRoot, "release-extracted");
  mkdirSync(extractedDirectory, { mode: 0o700 });
  for (const item of candidate.packages) {
    if (!record(item)) throw new Error("Candidate package record is invalid.");
    const filename = string(item.filename, "candidate package filename");
    const expectedDigest = string(item.sha256, `${filename} sha256`).replace(/^sha256:/u, "");
    const artifactPath = join(options.releaseDirectory, filename);
    if (sha256File(artifactPath) !== expectedDigest) {
      throw new Error(`${filename} digest does not match candidate manifest.`);
    }
    validateTarEntries(artifactPath);
    const packageRoot = join(extractedDirectory, basename(filename, ".tgz"));
    mkdirSync(packageRoot, { mode: 0o700 });
    run("tar", ["-xzf", artifactPath, "-C", packageRoot, "--strip-components=1"]);
    const manifest = readJson(join(packageRoot, "package.json"));
    const packageName = string(manifest.name, `${filename} package name`);
    if (!PUBLIC_PACKAGES.has(packageName) || item.name !== packageName) {
      throw new Error(`${filename} has an unexpected public package name.`);
    }
    if (item.version !== manifest.version || !EXACT_SEMVER.test(String(manifest.version))) {
      throw new Error(`${filename} manifest version is not the fixed exact candidate version.`);
    }
    auditPackedManifest(packageName, manifest);
    packageManifests.set(packageName, { manifest, packageRoot, item, artifactPath });
    expectedArtifacts.push({
      filename,
      path: artifactPath,
      sha256: expectedDigest,
      kind: "npm-tarball",
    });
  }
  if (packageManifests.size !== 7) throw new Error("Packed public package set is incomplete.");

  const standalone = candidate.standaloneRuntime;
  if (!record(standalone)) throw new Error("Candidate standaloneRuntime record is invalid.");
  const standaloneFilename = string(standalone.filename, "standalone filename");
  const standalonePath = join(options.releaseDirectory, standaloneFilename);
  const standaloneDigest = string(standalone.sha256, "standalone sha256").replace(/^sha256:/u, "");
  if (sha256File(standalonePath) !== standaloneDigest) {
    throw new Error("Standalone Runtime digest does not match candidate manifest.");
  }
  expectedArtifacts.push({
    filename: standaloneFilename,
    path: standalonePath,
    sha256: standaloneDigest,
    kind: "standalone-runtime",
  });
  cpSync(standalonePath, join(extractedDirectory, standaloneFilename));
  for (const filename of [
    "candidate-manifest.json",
    "checksums.sha256",
    "provenance.json",
    "sbom.spdx.json",
  ]) {
    cpSync(join(options.releaseDirectory, filename), join(extractedDirectory, filename));
  }

  const checksums = parseChecksums(join(options.releaseDirectory, "checksums.sha256"));
  if (
    checksums.size !== expectedArtifacts.length ||
    expectedArtifacts.some((item) => checksums.get(item.filename) !== item.sha256)
  ) {
    throw new Error(
      "checksums.sha256 does not cover exactly the seven tarballs and standalone Runtime.",
    );
  }
  const recomputedCandidate = candidateDigest(candidate.packages);
  if (recomputedCandidate !== CANDIDATE_DIGEST) {
    throw new Error(`Candidate digest recomputed as ${recomputedCandidate}.`);
  }

  const sourceLicense = readFileSync(join(sourceDirectory, "LICENSE"));
  for (const { packageRoot } of packageManifests.values()) {
    const packedLicense = readFileSync(join(packageRoot, "LICENSE"));
    if (!sourceLicense.equals(packedLicense))
      throw new Error("Packed MIT LICENSE differs from source.");
  }
  pass("first-party-license", "All seven tarballs carry the exact source MIT LICENSE bytes.");

  const distribution = packageManifests.get("@synara/cloud-agent-distribution");
  const packedStdio = join(distribution.packageRoot, "dist/stdio.mjs");
  if (sha256File(packedStdio) !== standaloneDigest) {
    throw new Error(
      "Standalone Runtime is not the same bytes as packed Distribution dist/stdio.mjs.",
    );
  }
  const standaloneText = readFileSync(standalonePath, "utf8");
  const bundledSdkMarker =
    standaloneText.includes("@anthropic-ai+claude-agent-sdk@0.3.207") &&
    standaloneText.includes("node_modules/@anthropic-ai/claude-agent-sdk/sdk.mjs");
  if (!bundledSdkMarker)
    throw new Error("Standalone Runtime lacks the pinned Claude Agent SDK marker.");
  pass(
    "bundled-anthropic-sdk",
    "Standalone embeds the @anthropic-ai/claude-agent-sdk@0.3.207 module marker.",
  );
  block(
    "standalone-bundle-composition",
    "The RC has no retained bundler metafile, so the exact file-level standalone bundle closure cannot be proven.",
  );
  pass(
    "release-asset-integrity",
    "Seven tarballs and standalone Runtime match manifest/checksum/same-bits inputs.",
  );

  const directoryFiles = readdirSync(options.releaseDirectory)
    .filter((name) => statSync(join(options.releaseDirectory, name)).isFile())
    .toSorted();
  const directoryIdentity = directoryFiles
    .map((name) => `${sha256File(join(options.releaseDirectory, name))}  ${name}`)
    .join("\n");
  const existingSbomPackages = Array.isArray(existingSbom.packages) ? existingSbom.packages : [];
  const existingSbomRelationships = Array.isArray(existingSbom.relationships)
    ? existingSbom.relationships
    : [];

  return {
    candidate,
    provenance,
    existingSbom,
    existingSbomPackageCount: existingSbomPackages.length,
    existingSbomRelationshipCount: existingSbomRelationships.length,
    expectedArtifacts,
    packageManifests,
    standalonePath,
    standaloneDigest,
    bundledSdkMarker,
    extractedDirectory,
    directoryFiles,
    directoryDigest: sha256Text(`${directoryIdentity}\n`),
  };
}

function auditPackedManifest(name, manifest) {
  if (manifest.license !== "MIT") throw new Error(`${name} must declare MIT.`);
  const repository = record(manifest.repository) ? manifest.repository.url : undefined;
  if (
    repository !== "git+https://github.com/hxp0618/cloud-agents.git" ||
    manifest.homepage !== "https://github.com/hxp0618/cloud-agents#readme" ||
    !record(manifest.bugs) ||
    manifest.bugs.url !== "https://github.com/hxp0618/cloud-agents/issues"
  ) {
    throw new Error(`${name} repository metadata does not point to hxp0618/cloud-agents.`);
  }
  for (const section of [
    "dependencies",
    "optionalDependencies",
    "peerDependencies",
    "devDependencies",
  ]) {
    const dependencies = record(manifest[section]) ? manifest[section] : {};
    for (const [dependency, specifier] of Object.entries(dependencies)) {
      if (typeof specifier !== "string" || LOCAL_DEPENDENCY.test(specifier)) {
        throw new Error(`${name} ${section}.${dependency} is not portable.`);
      }
      if (PUBLIC_PACKAGES.has(dependency) && !EXACT_SEMVER.test(specifier)) {
        throw new Error(`${name} does not exact-pin ${dependency}.`);
      }
    }
  }
}

function auditDependencyClosure(sourceDirectory, release, bunPath) {
  if (!bunPath) throw new Error("Pinned Bun is unavailable.");
  const lockPath = join(sourceDirectory, "bun.lock");
  const lock = parseBunLock(lockPath);
  const externalRoots = new Set();
  const publicRelationships = [];
  for (const [name, { manifest }] of release.packageManifests) {
    for (const section of ["dependencies", "optionalDependencies", "peerDependencies"]) {
      const dependencies = record(manifest[section]) ? manifest[section] : {};
      for (const dependency of Object.keys(dependencies)) {
        if (PUBLIC_PACKAGES.has(dependency)) {
          publicRelationships.push({ from: name, to: dependency, section });
        } else if (section !== "peerDependencies" || !optionalPeer(manifest, dependency)) {
          externalRoots.add(dependency);
        }
      }
    }
  }
  if (externalRoots.size !== 1 || !externalRoots.has("@anthropic-ai/claude-agent-sdk")) {
    throw new Error(`Unexpected external production roots: ${[...externalRoots].join(", ")}.`);
  }

  const reachable = new Set();
  const relationships = [];
  const queue = [...externalRoots];
  const unresolved = [];
  while (queue.length > 0) {
    const name = queue.shift();
    if (reachable.has(name)) continue;
    reachable.add(name);
    const tuple = lock.packages[name];
    if (!Array.isArray(tuple)) {
      unresolved.push(name);
      continue;
    }
    const metadata = record(tuple[2]) ? tuple[2] : {};
    const optionalPeers = new Set(
      Array.isArray(metadata.optionalPeers) ? metadata.optionalPeers : [],
    );
    for (const section of ["dependencies", "optionalDependencies", "peerDependencies"]) {
      const dependencies = record(metadata[section]) ? metadata[section] : {};
      for (const dependency of Object.keys(dependencies)) {
        if (section === "peerDependencies" && optionalPeers.has(dependency)) continue;
        relationships.push({ from: name, to: dependency, section });
        if (!reachable.has(dependency)) queue.push(dependency);
      }
    }
  }
  if (unresolved.length > 0) {
    throw new Error(`Production lock graph has unresolved nodes: ${unresolved.join(", ")}.`);
  }

  const install = run(
    bunPath,
    ["install", "--production", "--frozen-lockfile", "--ignore-scripts"],
    { cwd: sourceDirectory, allowFailure: true, timeout: 180_000, maxBuffer: 32 * 1024 * 1024 },
  );
  if (install.status !== 0) {
    throw new Error(`Frozen production install failed with status ${install.status}.`);
  }
  const installedIndex = indexBunPackages(join(sourceDirectory, "node_modules", ".bun"));
  const packages = [];
  for (const name of [...reachable].toSorted()) {
    const tuple = lock.packages[name];
    const resolution = string(tuple[0], `${name} lock resolution`);
    const version = resolution.slice(resolution.lastIndexOf("@") + 1);
    const url = string(tuple[1], `${name} lock URL`);
    const integrity = string(tuple[3], `${name} lock integrity`);
    const key = `${name}@${version}`;
    const installedRoots = installedIndex.get(key) ?? [];
    let manifest = null;
    let packageRoot = null;
    if (installedRoots.length > 0) {
      packageRoot = installedRoots.toSorted()[0];
      manifest = readJson(join(packageRoot, "package.json"));
      if (manifest.name !== name || manifest.version !== version) {
        throw new Error(`${key} installed manifest mismatch.`);
      }
    }
    packages.push({
      name,
      version,
      url,
      integrity,
      metadata: record(tuple[2]) ? tuple[2] : {},
      installed: manifest !== null,
      packageRoot,
      manifest,
    });
  }
  const notMaterialized = packages.filter((item) => !item.installed).map((item) => item.name);
  const expectedNotMaterialized = packages
    .filter(
      (item) =>
        item.name.startsWith("@anthropic-ai/claude-agent-sdk-") &&
        item.name !== "@anthropic-ai/claude-agent-sdk-darwin-arm64",
    )
    .map((item) => item.name)
    .toSorted();
  if (JSON.stringify(notMaterialized.toSorted()) !== JSON.stringify(expectedNotMaterialized)) {
    throw new Error(
      `Unexpected unmaterialized production packages: ${notMaterialized.join(", ")}.`,
    );
  }
  if (reachable.size !== 107 || relationships.length !== 175) {
    throw new Error(
      `Production closure drifted to ${reachable.size} packages/${relationships.length} edges.`,
    );
  }
  pass(
    "production-dependency-closure",
    "Exact bun.lock resolves 107 external production packages and 175 required/optional edges.",
  );

  return {
    status: "PASS",
    sourceLockSha256: sha256File(lockPath),
    externalRoots: [...externalRoots].toSorted(),
    externalPackageCount: packages.length,
    externalRelationshipCount: relationships.length,
    internalPackageCount: release.packageManifests.size,
    publicRelationships,
    relationships,
    packages,
    materializedPackageCount: packages.filter((item) => item.installed).length,
    unmaterializedPlatformPackages: expectedNotMaterialized,
    frozenInstall: {
      bunVersion: PINNED_LOCAL_TOOLS.bun.version,
      ignoreScripts: true,
      production: true,
      status: "PASS",
    },
  };
}

function parseBunLock(path) {
  const source = readFileSync(path, "utf8");
  const strictJson = source.replace(/,(\s*[}\]])/gu, "$1");
  const parsed = JSON.parse(strictJson);
  if (!record(parsed) || !record(parsed.packages) || !record(parsed.workspaces)) {
    throw new Error("bun.lock has an unexpected structure.");
  }
  return parsed;
}

function indexBunPackages(bunDirectory) {
  const index = new Map();
  if (!existsSync(bunDirectory)) return index;
  for (const entry of readdirSync(bunDirectory)) {
    const nodeModules = join(bunDirectory, entry, "node_modules");
    if (!existsSync(nodeModules)) continue;
    for (const root of packageRoots(nodeModules)) {
      const manifestPath = join(root, "package.json");
      if (!existsSync(manifestPath)) continue;
      const manifest = readJson(manifestPath);
      if (typeof manifest.name !== "string" || typeof manifest.version !== "string") continue;
      const key = `${manifest.name}@${manifest.version}`;
      if (!index.has(key)) index.set(key, []);
      index.get(key).push(root);
    }
  }
  return index;
}

function packageRoots(nodeModules) {
  const result = [];
  for (const name of readdirSync(nodeModules)) {
    const path = join(nodeModules, name);
    if (name.startsWith("@")) {
      if (!statSync(path).isDirectory()) continue;
      for (const child of readdirSync(path)) result.push(join(path, child));
    } else {
      result.push(path);
    }
  }
  return result;
}

function auditLicenses(dependency, release) {
  const packages = [];
  const licenseTexts = new Map();
  const missingLicenseText = [];
  const blockedPackages = [];
  for (const [name, { manifest, packageRoot, item }] of release.packageManifests) {
    const licensePath = join(packageRoot, "LICENSE");
    const text = readFileSync(licensePath, "utf8");
    const digest = sha256Text(text);
    addLicenseText(licenseTexts, digest, text, `${name}@${manifest.version}`);
    packages.push({
      name,
      version: manifest.version,
      kind: "first-party-public-package",
      licenseDeclared: "MIT",
      licensePolicy: "ALLOWED",
      licenseFiles: [{ filename: "LICENSE", sha256: digest }],
      artifactSha256: item.sha256,
    });
  }
  for (const item of dependency.packages) {
    const declared =
      item.manifest && typeof item.manifest.license === "string"
        ? item.manifest.license
        : "NOASSERTION";
    const files = [];
    if (item.packageRoot) {
      for (const filename of readdirSync(item.packageRoot).toSorted()) {
        if (!/^(?:license|licence|copying|notice)(?:\..*)?$/iu.test(filename)) continue;
        const path = join(item.packageRoot, filename);
        if (!statSync(path).isFile()) continue;
        const text = readFileSync(path, "utf8");
        const digest = sha256Text(text);
        files.push({ filename, sha256: digest });
        addLicenseText(licenseTexts, digest, text, `${item.name}@${item.version}`);
      }
      if (files.length === 0 && /SEE LICENSE IN README/iu.test(declared)) {
        const readme = readdirSync(item.packageRoot).find((name) =>
          /^readme(?:\..*)?$/iu.test(name),
        );
        if (readme) {
          const text = readFileSync(join(item.packageRoot, readme), "utf8");
          const digest = sha256Text(text);
          files.push({ filename: readme, sha256: digest });
          addLicenseText(licenseTexts, digest, text, `${item.name}@${item.version}`);
        }
      }
    }
    const authorized = LICENSE_ALLOWLIST.has(declared);
    const licensePolicy = authorized && files.length > 0 ? "ALLOWED" : "BLOCKED";
    if (files.length === 0) missingLicenseText.push(`${item.name}@${item.version}`);
    if (licensePolicy === "BLOCKED") blockedPackages.push(`${item.name}@${item.version}`);
    packages.push({
      name: item.name,
      version: item.version,
      kind: "external-production-dependency",
      licenseDeclared: declared,
      licensePolicy,
      licenseFiles: files,
      integrity: item.integrity,
      downloadLocation: item.url,
      materialized: item.installed,
    });
  }

  const anthropicPackages = packages.filter((item) =>
    item.name.startsWith("@anthropic-ai/claude-agent-sdk"),
  );
  const status =
    blockedPackages.length === 0 && missingLicenseText.length === 0 ? "PASS" : "BLOCKED";
  if (status === "PASS") {
    pass("third-party-license", "All production dependency licenses are allowlisted with text.");
  } else {
    block(
      "third-party-license",
      "Anthropic Claude Agent SDK uses external Legal Agreements/All rights reserved and no redistribution authorization is recorded; non-host native package license texts are not materialized.",
    );
  }
  const inventory = {
    schemaVersion: 1,
    sourceCommit: SOURCE_COMMIT,
    status,
    policy: {
      allowlist: [...LICENSE_ALLOWLIST].toSorted(),
      unknownOrSeeLicense: "BLOCKED",
      authorizationRequiredForNonOpenTerms: true,
    },
    packages: packages.toSorted((left, right) =>
      `${left.name}@${left.version}`.localeCompare(`${right.name}@${right.version}`),
    ),
    missingLicenseText: missingLicenseText.toSorted(),
    blockedPackages: blockedPackages.toSorted(),
    anthropicSdkPackages: anthropicPackages.map((item) => ({
      name: item.name,
      version: item.version,
      licenseDeclared: item.licenseDeclared,
      licensePolicy: item.licensePolicy,
      materialized: item.materialized,
    })),
  };
  return { licenseInventory: inventory, notices: renderNotices(inventory, licenseTexts) };
}

function addLicenseText(index, digest, text, packageIdentity) {
  if (!index.has(digest)) index.set(digest, { text, packages: [] });
  index.get(digest).packages.push(packageIdentity);
}

function renderNotices(inventory, texts) {
  const lines = [
    "# THIRD_PARTY_NOTICES",
    "",
    `Fixed source: \`${SOURCE_COMMIT}\``,
    "",
    "> P0 audit output only. This file does not grant redistribution rights. Packages marked BLOCKED require legal/owner approval before release.",
    "",
    "## Production dependency inventory",
    "",
    "| Package | Version | Declared license | Policy | License text SHA-256 |",
    "| --- | --- | --- | --- | --- |",
  ];
  for (const item of inventory.packages.filter(
    (entry) => entry.kind === "external-production-dependency",
  )) {
    lines.push(
      `| \`${item.name}\` | \`${item.version}\` | ${escapeTable(item.licenseDeclared)} | ${item.licensePolicy} | ${
        item.licenseFiles.length > 0
          ? item.licenseFiles.map((file) => `\`${file.sha256}\``).join("<br>")
          : "MISSING"
      } |`,
    );
  }
  lines.push("", "## License texts grouped by exact SHA-256", "");
  for (const [digest, entry] of [...texts.entries()].toSorted(([left], [right]) =>
    left.localeCompare(right),
  )) {
    const thirdParty = entry.packages.filter((name) => !name.startsWith("@synara/"));
    if (thirdParty.length === 0) continue;
    lines.push(
      `### \`${digest}\``,
      "",
      `Packages: ${thirdParty.map((name) => `\`${name}\``).join(", ")}`,
      "",
      "```text",
      entry.text.trimEnd().replaceAll("```", "` ` `"),
      "```",
      "",
    );
  }
  if (inventory.missingLicenseText.length > 0) {
    lines.push(
      "## Missing text — fail closed",
      "",
      ...inventory.missingLicenseText.map((name) => `- \`${name}\``),
      "",
    );
  }
  return `${lines.join("\n")}\n`;
}

function buildProductionSbom(dependency, release, inventory, source) {
  const packages = [];
  const relationships = [];
  const identifiers = new Map();
  const licenseByIdentity = new Map(
    inventory.packages.map((item) => [`${item.name}@${item.version}`, item]),
  );
  const spdxId = (name, version) => {
    const key = `${name}@${version}`;
    if (!identifiers.has(key))
      identifiers.set(key, `SPDXRef-Package-${sha256Text(key).slice(0, 20)}`);
    return identifiers.get(key);
  };
  for (const [name, { manifest, item }] of release.packageManifests) {
    const version = String(manifest.version);
    packages.push({
      SPDXID: spdxId(name, version),
      name,
      versionInfo: version,
      downloadLocation: "NOASSERTION",
      filesAnalyzed: false,
      checksums: [{ algorithm: "SHA256", checksumValue: item.sha256.replace(/^sha256:/u, "") }],
      licenseConcluded: "NOASSERTION",
      licenseDeclared: "MIT",
      supplier: "Organization: hxp0618/cloud-agents contributors",
      externalRefs: [purlRef(name, version)],
    });
  }
  for (const item of dependency.packages) {
    const license = licenseByIdentity.get(`${item.name}@${item.version}`);
    const declared = spdxLicense(license?.licenseDeclared, item.installed);
    packages.push({
      SPDXID: spdxId(item.name, item.version),
      name: item.name,
      versionInfo: item.version,
      downloadLocation: item.url,
      filesAnalyzed: false,
      checksums: [{ algorithm: "SHA512", checksumValue: sriToHex(item.integrity) }],
      licenseConcluded: "NOASSERTION",
      licenseDeclared: declared,
      supplier: "NOASSERTION",
      externalRefs: [purlRef(item.name, item.version)],
    });
  }
  const versionByName = new Map(packages.map((item) => [item.name, String(item.versionInfo)]));
  const edgeSet = new Set();
  for (const edge of [...dependency.publicRelationships, ...dependency.relationships]) {
    const fromVersion = versionByName.get(edge.from);
    const toVersion = versionByName.get(edge.to);
    if (!fromVersion || !toVersion)
      throw new Error(`SBOM relationship is unresolved: ${edge.from} -> ${edge.to}.`);
    const key = `${edge.from}@${fromVersion}->${edge.to}@${toVersion}`;
    if (edgeSet.has(key)) continue;
    edgeSet.add(key);
    relationships.push({
      spdxElementId: spdxId(edge.from, fromVersion),
      relationshipType: "DEPENDS_ON",
      relatedSpdxElement: spdxId(edge.to, toVersion),
      comment: `Source section: ${edge.section}`,
    });
  }
  for (const [name, { manifest }] of release.packageManifests) {
    relationships.push({
      spdxElementId: "SPDXRef-DOCUMENT",
      relationshipType: "DESCRIBES",
      relatedSpdxElement: spdxId(name, String(manifest.version)),
    });
  }
  const sbom = {
    spdxVersion: "SPDX-2.3",
    dataLicense: "CC0-1.0",
    SPDXID: "SPDXRef-DOCUMENT",
    name: "cloud-agent-runtime-49e8cdc-production-closure",
    documentNamespace: `https://github.com/hxp0618/cloud-agents/sbom/${SOURCE_COMMIT}/${CANDIDATE_DIGEST.slice(7)}`,
    creationInfo: {
      created: run(PINNED_LOCAL_TOOLS.git.path, ["show", "-s", "--format=%cI", SOURCE_COMMIT], {
        cwd: repositoryRoot,
      }).stdout.trim(),
      creators: ["Tool: docs/plan/p0/scripts/audit-runtime-supply-chain.mjs"],
      comment: `Fixed source tree ${source.sourceTree}; candidate ${CANDIDATE_DIGEST}.`,
    },
    packages: packages.toSorted((left, right) => left.name.localeCompare(right.name)),
    relationships: relationships.toSorted((left, right) =>
      `${left.spdxElementId}:${left.relatedSpdxElement}`.localeCompare(
        `${right.spdxElementId}:${right.relatedSpdxElement}`,
      ),
    ),
    hasExtractedLicensingInfos: [
      {
        licenseId: "LicenseRef-Anthropic-Legal-Terms",
        extractedText:
          "Copyright Anthropic PBC. All rights reserved. Use is subject to Anthropic Legal Agreements referenced by the distributed package.",
        seeAls: ["https://code.claude.com/docs/en/legal-and-compliance"],
      },
    ],
  };
  if (sbom.packages.length !== 114 || sbom.relationships.length < 182) {
    throw new Error(
      `Production SBOM coverage drifted to ${sbom.packages.length} packages/${sbom.relationships.length} relationships.`,
    );
  }
  pass(
    "production-sbom",
    "Generated SPDX 2.3 covers seven public packages plus 107 external production nodes.",
  );
  return sbom;
}

function auditSecrets(gitleaksPath, sourceDirectory, extractedDirectory) {
  if (!gitleaksPath) {
    block("secret-full-scan", "Pinned Gitleaks is unavailable.");
    return {
      schemaVersion: 1,
      status: "BLOCKED",
      scanner: null,
      scopes: [],
      note: "No raw report was persisted.",
    };
  }
  const scopes = [
    { name: "fixed-source-tree", mode: "dir", target: sourceDirectory },
    { name: "extracted-release-artifacts", mode: "dir", target: extractedDirectory },
    { name: "all-local-git-refs", mode: "git", target: repositoryRoot },
  ];
  const sanitized = [];
  let findingCount = 0;
  for (const scope of scopes) {
    const reportPath = join(temporaryRoot, `gitleaks-${scope.name}.json`);
    const args =
      scope.mode === "git"
        ? [
            "git",
            scope.target,
            "--log-opts=--all",
            "--redact=100",
            "--report-format=json",
            `--report-path=${reportPath}`,
            "--no-banner",
            "--exit-code=0",
          ]
        : [
            "dir",
            scope.target,
            "--redact=100",
            "--report-format=json",
            `--report-path=${reportPath}`,
            "--no-banner",
            "--exit-code=0",
          ];
    const scan = run(gitleaksPath, args, { allowFailure: true, timeout: 180_000 });
    if (scan.status !== 0 || !existsSync(reportPath)) {
      block("secret-full-scan", `${scope.name} Gitleaks execution failed.`);
      sanitized.push({ name: scope.name, status: "BLOCKED", findingCount: null });
      continue;
    }
    const raw = readJsonArray(reportPath);
    findingCount += raw.length;
    sanitized.push({
      name: scope.name,
      status: raw.length === 0 ? "PASS" : "BLOCKED",
      findingCount: raw.length,
      findings: raw.map((finding) => ({
        ruleId: safeString(finding.RuleID),
        path: sanitizeFindingPath(safeString(finding.File), scope.target),
        startLine: Number.isInteger(finding.StartLine) ? finding.StartLine : null,
        commit: /^[0-9a-f]{40}$/u.test(safeString(finding.Commit)) ? finding.Commit : null,
        fingerprintSha256: sha256Text(safeString(finding.Fingerprint)),
      })),
    });
    rmSync(reportPath, { force: true });
  }
  const status =
    sanitized.length === 3 && sanitized.every((item) => item.status === "PASS")
      ? "PASS"
      : "BLOCKED";
  if (status === "PASS") {
    pass(
      "secret-full-scan",
      "Gitleaks scanned fixed source, safely extracted artifacts, and all local Git refs with zero findings.",
    );
  } else {
    block(
      "secret-full-scan",
      `${findingCount} sanitized secret finding(s) or scanner failures remain.`,
    );
  }
  return {
    schemaVersion: 1,
    status,
    scanner: {
      name: "gitleaks",
      version: PINNED_DOWNLOAD_TOOLS.gitleaks.version,
      binarySha256: PINNED_DOWNLOAD_TOOLS.gitleaks.binarySha256,
      configuration: "embedded-default; no path allowlist",
    },
    scopes: sanitized,
    rawReportsPersisted: false,
    redaction: 100,
  };
}

function auditWithSyft(syftPath, extractedDirectory) {
  if (!syftPath) {
    block("syft-independent-inventory", "Pinned Syft is unavailable.");
    return { status: "BLOCKED", packageCount: null };
  }
  const staging = join(temporaryRoot, "syft-release-node-modules");
  const stagingNodeModules = join(staging, "node_modules");
  mkdirSync(stagingNodeModules, { recursive: true, mode: 0o700 });
  for (const entry of readdirSync(extractedDirectory)) {
    const packageRoot = join(extractedDirectory, entry);
    const manifestPath = join(packageRoot, "package.json");
    if (!existsSync(manifestPath) || !statSync(packageRoot).isDirectory()) continue;
    const manifest = readJson(manifestPath);
    const name = safeString(manifest.name);
    if (!PUBLIC_PACKAGES.has(name)) continue;
    const target = join(stagingNodeModules, ...name.split("/"));
    mkdirSync(dirname(target), { recursive: true, mode: 0o700 });
    cpSync(packageRoot, target, { recursive: true });
  }
  cpSync(
    join(extractedDirectory, "cloud-agent-runtime-standalone.mjs"),
    join(staging, "cloud-agent-runtime-standalone.mjs"),
  );
  const output = join(temporaryRoot, "syft-artifacts.json");
  const result = run(
    syftPath,
    [
      "scan",
      `dir:${staging}`,
      "--quiet",
      "--select-catalogers",
      "+javascript-package-cataloger",
      "-o",
      `syft-json=${output}`,
    ],
    {
      allowFailure: true,
      timeout: 180_000,
      maxBuffer: 16 * 1024 * 1024,
    },
  );
  if (result.status !== 0 || !existsSync(output)) {
    block("syft-independent-inventory", "Syft artifact inventory failed.");
    return { status: "BLOCKED", packageCount: null };
  }
  const report = readJson(output);
  const artifacts = Array.isArray(report.artifacts) ? report.artifacts : [];
  const identities = artifacts
    .filter((item) => record(item) && typeof item.name === "string")
    .map((item) => `${item.name}@${safeString(item.version)}`)
    .toSorted();
  const detectedPublic = [...PUBLIC_PACKAGES].filter((name) =>
    identities.some((identity) => identity.startsWith(`${name}@`)),
  );
  if (detectedPublic.length === 7) {
    pass(
      "syft-independent-inventory",
      "Syft independently identifies all seven extracted public packages.",
    );
    return {
      status: "PASS",
      scanner: "syft",
      version: PINNED_DOWNLOAD_TOOLS.syft.version,
      binarySha256: PINNED_DOWNLOAD_TOOLS.syft.binarySha256,
      packageCount: artifacts.length,
      publicPackagesDetected: detectedPublic.toSorted(),
      standaloneSdkDetected: identities.some((identity) =>
        identity.startsWith("@anthropic-ai/claude-agent-sdk@0.3.207"),
      ),
    };
  }
  block(
    "syft-independent-inventory",
    `Syft identified ${detectedPublic.length}/7 public packages.`,
  );
  return {
    status: "BLOCKED",
    packageCount: artifacts.length,
    publicPackagesDetected: detectedPublic.toSorted(),
  };
}

function auditVulnerabilities(osvPath, sbomPath) {
  if (!osvPath) {
    block("dependency-vulnerability-snapshot", "Pinned OSV-Scanner is unavailable.");
    return { status: "BLOCKED", vulnerabilities: [] };
  }
  const reportPath = join(temporaryRoot, "osv.json");
  const result = run(
    osvPath,
    [
      "scan",
      "source",
      `--sbom=${sbomPath}`,
      "--format=json",
      `--output-file=${reportPath}`,
      "--verbosity=error",
      "--all-packages",
    ],
    { allowFailure: true, timeout: 180_000, maxBuffer: 16 * 1024 * 1024 },
  );
  if (!existsSync(reportPath)) {
    block("dependency-vulnerability-snapshot", `OSV-Scanner failed with status ${result.status}.`);
    return { status: "BLOCKED", vulnerabilities: [] };
  }
  const report = readJson(reportPath);
  const findings = [];
  for (const resultItem of Array.isArray(report.results) ? report.results : []) {
    for (const packageItem of Array.isArray(resultItem.packages) ? resultItem.packages : []) {
      const packageRecord = record(packageItem.package) ? packageItem.package : {};
      for (const vulnerability of Array.isArray(packageItem.vulnerabilities)
        ? packageItem.vulnerabilities
        : []) {
        findings.push({
          id: safeString(vulnerability.id),
          package: safeString(packageRecord.name),
          version: safeString(packageRecord.version),
          ecosystem: safeString(packageRecord.ecosystem),
        });
      }
    }
  }
  const uniqueFindings = uniqueObjects(findings);
  const status = result.status === 0 && uniqueFindings.length === 0 ? "PASS" : "BLOCKED";
  if (status === "PASS") {
    pass(
      "dependency-vulnerability-snapshot",
      "OSV found no known vulnerabilities in the production SBOM snapshot.",
    );
  } else {
    block(
      "dependency-vulnerability-snapshot",
      `OSV returned status ${result.status} with ${uniqueFindings.length} vulnerability finding(s).`,
    );
  }
  return {
    status,
    scanner: "osv-scanner",
    version: PINNED_DOWNLOAD_TOOLS.osv.version,
    binarySha256: PINNED_DOWNLOAD_TOOLS.osv.binarySha256,
    databaseMode: "online OSV API snapshot",
    vulnerabilityCount: uniqueFindings.length,
    vulnerabilities: uniqueFindings,
  };
}

function auditWorkflow(sourceDirectory) {
  const workflowDirectory = join(sourceDirectory, ".github", "workflows");
  const workflowFiles = existsSync(workflowDirectory)
    ? readdirSync(workflowDirectory)
        .filter((name) => /\.ya?ml$/u.test(name))
        .toSorted()
    : [];
  const uses = [];
  let hasOidcPermission = false;
  let hasAttestation = false;
  let hasImmediateVerify = false;
  for (const filename of workflowFiles) {
    const text = readFileSync(join(workflowDirectory, filename), "utf8");
    hasOidcPermission ||= /id-token\s*:\s*write/u.test(text);
    hasAttestation ||= /attest-build-provenance|attest-sbom|cosign\s+attest/iu.test(text);
    hasImmediateVerify ||= /attestation\s+verify|verify-attestation|cosign\s+verify/iu.test(text);
    for (const match of text.matchAll(/^\s*-?\s*uses:\s*([^\s#]+)(?:\s*#.*)?$/gmu)) {
      const specifier = match[1];
      const at = specifier.lastIndexOf("@");
      const reference = at === -1 ? "" : specifier.slice(at + 1);
      uses.push({ filename, specifier, pinnedToFullCommit: /^[0-9a-f]{40}$/u.test(reference) });
    }
  }
  if (uses.length > 0 && uses.every((item) => item.pinnedToFullCommit)) {
    pass(
      "workflow-action-pinning",
      "All GitHub Action uses in the fixed source are pinned to full commit SHA.",
    );
  } else {
    block(
      "workflow-action-pinning",
      "One or more GitHub Actions are not pinned to a full commit SHA.",
    );
  }
  if (!hasOidcPermission || !hasAttestation || !hasImmediateVerify) {
    block(
      "workflow-trusted-attestation",
      "Fixed source workflow lacks the complete OIDC attest plus immediate verification chain.",
    );
  } else {
    pass(
      "workflow-trusted-attestation",
      "Workflow declares OIDC attestation and immediate verification.",
    );
  }
  return {
    actionPinningStatus: checkStatus("workflow-action-pinning"),
    trustedAttestationStatus: checkStatus("workflow-trusted-attestation"),
    workflowFiles: workflowFiles.map((filename) => ({
      filename,
      sha256: sha256File(join(workflowDirectory, filename)),
    })),
    uses,
    oidcIdTokenWrite: hasOidcPermission,
    attestationStep: hasAttestation,
    immediateVerificationStep: hasImmediateVerify,
  };
}

function auditGitSignatures(gitPath) {
  if (!gitPath) {
    block("git-signatures", "Pinned Git is unavailable.");
    return { status: "BLOCKED", commitVerified: false, tagVerified: false };
  }
  const commit = run(gitPath, ["verify-commit", "--raw", SOURCE_COMMIT], {
    cwd: repositoryRoot,
    allowFailure: true,
  });
  const tag = run(gitPath, ["verify-tag", "--raw", SOURCE_TAG], {
    cwd: repositoryRoot,
    allowFailure: true,
  });
  const commitVerified = commit.status === 0;
  const tagVerified = tag.status === 0;
  const commitSignatureCode = run(gitPath, ["show", "-s", "--format=%G?", SOURCE_COMMIT], {
    cwd: repositoryRoot,
    allowFailure: true,
  }).stdout.trim();
  if (commitVerified && tagVerified)
    pass("git-signatures", "Source commit and RC tag signatures verify.");
  else block("git-signatures", "Source commit and/or RC tag has no trusted verifiable signature.");
  return {
    status: checkStatus("git-signatures"),
    commitVerified,
    tagVerified,
    commitFailureClass:
      commitVerified || commitSignatureCode !== "N"
        ? commitVerified
          ? null
          : signatureFailureClass(commit)
        : "NO_SIGNATURE",
    tagFailureClass: tagVerified ? null : signatureFailureClass(tag),
  };
}

function auditProvenance(release) {
  const statement = release.provenance;
  const subjects = Array.isArray(statement.subject) ? statement.subject : [];
  const subjectNames = subjects
    .map((item) => safeString(item.name))
    .filter(Boolean)
    .toSorted();
  const expectedNames = release.expectedArtifacts.map((item) => item.filename).toSorted();
  const missingSubjects = expectedNames.filter((name) => !subjectNames.includes(name));
  const predicate = record(statement.predicate) ? statement.predicate : {};
  const definition = record(predicate.buildDefinition) ? predicate.buildDefinition : {};
  const resolvedDependencies = Array.isArray(definition.resolvedDependencies)
    ? definition.resolvedDependencies
    : [];
  const builder =
    record(predicate.runDetails) && record(predicate.runDetails.builder)
      ? safeString(predicate.runDetails.builder.id)
      : "";
  const releaseFiles = new Set(release.directoryFiles);
  const signatureFiles = [...releaseFiles].filter((name) =>
    /(?:\.sig|\.pem|\.bundle|\.intoto\.jsonl)$/u.test(name),
  );
  const complete =
    missingSubjects.length === 0 &&
    resolvedDependencies.length > 0 &&
    signatureFiles.length > 0 &&
    builder !== "local-cloud-agent-release-smoke";
  if (complete)
    pass(
      "provenance-coverage",
      "Provenance covers all fixed artifacts/materials and has signature material.",
    );
  else {
    block(
      "provenance-coverage",
      "Existing local unsigned provenance covers only seven tarballs, has resolvedDependencies=[], and omits standalone/metadata evidence.",
    );
  }
  const candidateBoundNames = new Set(
    release.candidate.packages.map((item) => safeString(item.filename)),
  );
  const candidateDigestCoversStandalone = candidateBoundNames.has(
    safeString(release.candidate.standaloneRuntime?.filename),
  );
  return {
    status: checkStatus("provenance-coverage"),
    predicateType: safeString(statement.predicateType),
    subjectCount: subjects.length,
    subjectNames,
    missingEightBitSubjects: missingSubjects,
    missingMetadataSubjects: [
      "candidate-manifest.json",
      "checksums.sha256",
      "sbom.spdx.json",
      "provenance.json",
      "THIRD_PARTY_NOTICES.md",
    ].filter((name) => !subjectNames.includes(name)),
    resolvedDependencyCount: resolvedDependencies.length,
    builderId: builder,
    signatureFiles,
    candidateDigestCoversStandalone,
  };
}

function auditGitHubAttestations(ghPath, nodePath, release) {
  if (!ghPath || !nodePath) {
    block("github-attestation-verification", "Pinned gh and/or Node is unavailable.");
    return { status: "BLOCKED", artifacts: [] };
  }
  const artifacts = [];
  for (const artifact of release.expectedArtifacts) {
    const endpoint = `https://api.github.com/repos/hxp0618/cloud-agents/attestations/sha256:${artifact.sha256}?per_page=30&predicate_type=${encodeURIComponent("https://slsa.dev/provenance/v1")}`;
    const availability = run(
      nodePath,
      [
        "-e",
        'const response=await fetch(process.argv[1],{headers:{Accept:"application/vnd.github+json","X-GitHub-Api-Version":"2022-11-28"},signal:AbortSignal.timeout(15000)});process.stdout.write(String(response.status));',
        endpoint,
      ],
      { allowFailure: true, timeout: 20_000, maxBuffer: 1024 * 1024 },
    );
    const httpStatus = Number.parseInt(String(availability.stdout).trim(), 10);
    let verified = false;
    let resultClass;
    if (availability.status === 0 && httpStatus === 404) {
      resultClass = "NO_ATTESTATION_FOUND";
    } else if (availability.status === 0 && httpStatus === 200) {
      const verification = run(
        ghPath,
        ["attestation", "verify", artifact.path, "--repo", "hxp0618/cloud-agents", "--format=json"],
        {
          allowFailure: true,
          timeout: 20_000,
          maxBuffer: 8 * 1024 * 1024,
          env: { GH_HTTP_TIMEOUT: "15" },
        },
      );
      verified = verification.status === 0;
      resultClass = verified ? "VERIFIED" : "VERIFICATION_ERROR";
    } else {
      resultClass = "VERIFICATION_ERROR";
    }
    artifacts.push({
      filename: artifact.filename,
      sha256: artifact.sha256,
      verified,
      resultClass,
      apiStatus: Number.isInteger(httpStatus) ? httpStatus : null,
    });
  }
  if (artifacts.every((item) => item.verified)) {
    pass(
      "github-attestation-verification",
      "GitHub attestations verify for all seven tarballs and standalone.",
    );
  } else {
    block(
      "github-attestation-verification",
      `${artifacts.filter((item) => !item.verified).length}/8 local artifact attestations are absent or unverifiable.`,
    );
  }
  return {
    status: checkStatus("github-attestation-verification"),
    verifier: {
      name: "gh",
      version: PINNED_LOCAL_TOOLS.gh.version,
      binarySha256: PINNED_LOCAL_TOOLS.gh.sha256,
    },
    artifacts,
  };
}

function publicReleaseReport(release) {
  return {
    status: checkStatus("release-asset-integrity"),
    candidateManifestSha256: sha256File(join(options.releaseDirectory, "candidate-manifest.json")),
    checksumsSha256: sha256File(join(options.releaseDirectory, "checksums.sha256")),
    provenanceSha256: sha256File(join(options.releaseDirectory, "provenance.json")),
    existingSbomSha256: sha256File(join(options.releaseDirectory, "sbom.spdx.json")),
    artifactCount: release.expectedArtifacts.length,
    artifacts: release.expectedArtifacts.map(({ filename, sha256, kind }) => ({
      filename,
      sha256,
      kind,
    })),
    directoryFiles: release.directoryFiles,
    directoryDigest: release.directoryDigest,
    bundledAnthropicSdk: release.bundledSdkMarker,
    candidateBuildToolchain: {
      node: release.candidate.nodeVersion,
      npm: release.candidate.npmVersion,
      bun: release.candidate.bunVersion,
      platform: release.candidate.platform,
      sourceDirty: release.candidate.sourceDirty,
    },
  };
}

function publicDependencyReport(dependency) {
  return {
    status: dependency.status,
    sourceLockSha256: dependency.sourceLockSha256,
    externalRoots: dependency.externalRoots,
    externalPackageCount: dependency.externalPackageCount,
    externalRelationshipCount: dependency.externalRelationshipCount,
    internalPackageCount: dependency.internalPackageCount,
    materializedPackageCount: dependency.materializedPackageCount,
    unmaterializedPlatformPackages: dependency.unmaterializedPlatformPackages,
    frozenInstall: dependency.frozenInstall,
    packages: dependency.packages.map((item) => ({
      name: item.name,
      version: item.version,
      downloadLocation: item.url,
      integrity: item.integrity,
      materialized: item.installed,
    })),
    relationships: dependency.relationships,
    publicRelationships: dependency.publicRelationships,
  };
}

function publicSourceReport(source) {
  const { sourceDirectory: _sourceDirectory, ...report } = source;
  return report;
}

function publicToolchain(value) {
  const tools = {};
  for (const [name, item] of Object.entries(value.tools ?? {})) {
    const { path: _path, ...report } = item;
    tools[name] = report;
  }
  return { ...value, tools };
}

function writeEvidence() {
  mkdirSync(options.outputDirectory, { recursive: true, mode: 0o755 });
  const outputs = new Map([
    ["runtime-supply-chain-audit.json", `${JSON.stringify(audit, null, 2)}\n`],
    ["runtime-supply-chain-audit.zh-CN.md", renderSummary(audit)],
    ["secret-scan-sanitized.json", `${JSON.stringify(secretReport, null, 2)}\n`],
    ["license-inventory.json", `${JSON.stringify(licenseInventory, null, 2)}\n`],
    ["sbom-production.spdx.json", `${JSON.stringify(productionSbom, null, 2)}\n`],
    ["THIRD_PARTY_NOTICES.md", notices],
    ["toolchain-lock.json", `${JSON.stringify(publicToolchain(toolchain), null, 2)}\n`],
  ]);
  for (const [filename, content] of outputs)
    atomicWrite(join(options.outputDirectory, filename), content);
  const formatter = toolchain.tools?.oxfmt;
  if (formatter?.status === "PASS" && formatter.path) {
    run(
      formatter.path,
      [...outputs.keys()]
        .map((filename) => join(options.outputDirectory, filename))
        .concat("--write"),
      { timeout: 120_000 },
    );
  }
  const scriptPath = resolve(import.meta.filename);
  const digestLines = [
    ...[...outputs.keys()].map(
      (filename) => `${sha256File(join(options.outputDirectory, filename))}  ${filename}`,
    ),
    `${sha256File(scriptPath)}  ../scripts/${basename(scriptPath)}`,
  ].toSorted();
  atomicWrite(
    join(options.outputDirectory, "generated-evidence.sha256"),
    `${digestLines.join("\n")}\n`,
  );
}

function renderSummary(report) {
  const source = report.source ?? {};
  const dependency = report.dependency ?? {};
  const sbom = report.sbom ?? {};
  const secret = report.secretScanning ?? {};
  const vulnerabilities = report.vulnerabilities ?? {};
  const lines = [
    "# Cloud Agents Runtime P0 供应链审计",
    "",
    `- 总状态：**${report.status}**`,
    `- 固定 source：\`${SOURCE_COMMIT}\``,
    `- 固定 tag：\`${SOURCE_TAG}\`（tag object \`${TAG_OBJECT}\`）`,
    `- 固定 candidate：\`${CANDIDATE_DIGEST}\``,
    "- 权限边界：只读审计；未发布、未生成可信 attestation、未修改 Release 或远端",
    "",
    "## 已验证",
    "",
    `- source tree：\`${safeString(source.sourceTree) || "未取得"}\`；SOURCE_PROVENANCE 对象：${source.provenanceObjectsVerified ?? 0}/${source.provenanceObjectsDeclared ?? 0}。`,
    `- 本地不可变 bits：七个 tgz + standalone ${report.release?.status === "PASS" ? "全部匹配 manifest/checksums" : "未完整验证"}。`,
    `- 生产依赖闭包：${dependency.externalPackageCount ?? 0} 个外部节点、${dependency.externalRelationshipCount ?? 0} 条边；七个公共包另计。`,
    `- SPDX 2.3：${sbom.packageCount ?? 0} 个 package、${sbom.relationshipCount ?? 0} 条 relationship。`,
    `- Gitleaks：${secret.status ?? "BLOCKED"}；固定 source、解包制品、local \`--all\` history 均不使用目录 allowlist；原始报告未入库。`,
    `- OSV snapshot：${vulnerabilities.status ?? "BLOCKED"}；已知漏洞数 ${vulnerabilities.vulnerabilityCount ?? "未知"}。`,
    `- 固定 source workflow action pinning：${report.workflow?.actionPinningStatus ?? "BLOCKED"}。`,
    "",
    "## 阻塞项（fail closed）",
    "",
    ...report.blockers.map((item) => `- ${item}`),
    "",
    "## 结论边界",
    "",
    "本记录可以支持“固定 source 与本地八个 Runtime bits 的哈希、生产依赖图和 secret scan 已复核”。它不能支持 license cleared、可信 provenance/attestation、已发布、已部署、Platform RC、Beta 或 GA。",
    "",
    "生成文件：",
    "",
    "- `runtime-supply-chain-audit.json`：机器可读总报告；",
    "- `secret-scan-sanitized.json`：仅规则/路径/行号/commit/fingerprint hash；",
    "- `license-inventory.json` 与 `THIRD_PARTY_NOTICES.md`：许可证清单、文本哈希和 fail-closed 项；",
    "- `sbom-production.spdx.json`：生产闭包 SPDX 2.3；",
    "- `toolchain-lock.json`：固定工具版本、下载源和 binary/archive SHA-256；",
    "- `generated-evidence.sha256`：上述输出与生成脚本的 SHA-256。",
    "",
  ];
  return `${lines.join("\n")}\n`;
}

function emptySbom() {
  return {
    spdxVersion: "SPDX-2.3",
    dataLicense: "CC0-1.0",
    SPDXID: "SPDXRef-DOCUMENT",
    name: "cloud-agent-runtime-p0-audit-incomplete",
    documentNamespace: `https://github.com/hxp0618/cloud-agents/sbom/incomplete/${SOURCE_COMMIT}`,
    creationInfo: {
      created: new Date().toISOString(),
      creators: ["Tool: docs/plan/p0/scripts/audit-runtime-supply-chain.mjs"],
    },
    packages: [],
    relationships: [],
  };
}

function candidateDigest(packages) {
  const identity = packages
    .map((item) => `${item.name}@${item.version} ${item.sha256}`)
    .toSorted()
    .join("\n");
  return `sha256:${sha256Text(`${identity}\n`)}`;
}

function validateTarEntries(path) {
  const list = run("tar", ["-tzf", path]).stdout.split("\n").filter(Boolean);
  if (list.length === 0) throw new Error(`${basename(path)} is empty.`);
  for (const entry of list) {
    if (entry.startsWith("/") || entry.split("/").includes("..")) {
      throw new Error(`${basename(path)} contains unsafe path ${entry}.`);
    }
  }
  const verbose = run("tar", ["-tvzf", path]).stdout.split("\n").filter(Boolean);
  if (verbose.some((line) => /^[lh]/u.test(line))) {
    throw new Error(`${basename(path)} contains a link entry; extraction is denied.`);
  }
}

function parseChecksums(path) {
  const result = new Map();
  for (const line of readFileSync(path, "utf8").split("\n").filter(Boolean)) {
    const match = /^([0-9a-f]{64})\s{2}([^/]+)$/u.exec(line);
    if (!match || result.has(match[2])) throw new Error("checksums.sha256 is malformed.");
    result.set(match[2], match[1]);
  }
  return result;
}

function optionalPeer(manifest, name) {
  return (
    record(manifest.peerDependenciesMeta) && manifest.peerDependenciesMeta[name]?.optional === true
  );
}

function spdxLicense(value, materialized) {
  if (!materialized) return "NOASSERTION";
  if (LICENSE_ALLOWLIST.has(value)) return value;
  if (/SEE LICENSE|Anthropic/iu.test(value)) return "LicenseRef-Anthropic-Legal-Terms";
  return "NOASSERTION";
}

function purlRef(name, version) {
  const locator = `pkg:npm/${encodeURIComponent(name).replace("%2F", "/")}@${encodeURIComponent(version)}`;
  return {
    referenceCategory: "PACKAGE-MANAGER",
    referenceType: "purl",
    referenceLocator: locator,
  };
}

function sriToHex(integrity) {
  const match = /^sha512-(.+)$/u.exec(integrity);
  if (!match) throw new Error(`Expected sha512 SRI, found ${integrity}.`);
  return Buffer.from(match[1], "base64").toString("hex");
}

function signatureFailureClass(result) {
  const text = `${result.stdout}\n${result.stderr}`;
  if (/no signature found/iu.test(text)) return "NO_SIGNATURE";
  if (/unknown|no public key/iu.test(text)) return "UNTRUSTED_OR_UNKNOWN_KEY";
  return "VERIFICATION_FAILED";
}

function sanitizeFindingPath(value, root) {
  if (!value) return null;
  const absolute = resolve(value);
  const rootAbsolute = resolve(root);
  if (absolute.startsWith(`${rootAbsolute}/`)) return relative(rootAbsolute, absolute);
  if (value.startsWith(rootAbsolute)) return relative(rootAbsolute, value);
  return basename(value);
}

function uniqueObjects(items) {
  return [...new Map(items.map((item) => [JSON.stringify(item), item])).values()].toSorted(
    (left, right) => JSON.stringify(left).localeCompare(JSON.stringify(right)),
  );
}

function escapeTable(value) {
  return String(value).replaceAll("|", "\\|").replaceAll("\n", "<br>");
}

function atomicWrite(path, content) {
  const temporary = `${path}.tmp-${process.pid}`;
  writeFileSync(temporary, content, { mode: 0o644 });
  renameSync(temporary, path);
}

function readJson(path) {
  const parsed = JSON.parse(readFileSync(path, "utf8"));
  if (!record(parsed)) throw new Error(`${path} must contain a JSON object.`);
  return parsed;
}

function readJsonArray(path) {
  const parsed = JSON.parse(readFileSync(path, "utf8"));
  if (!Array.isArray(parsed)) throw new Error(`${path} must contain a JSON array.`);
  return parsed;
}

function sha256File(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function sha256Text(value) {
  return createHash("sha256").update(value).digest("hex");
}

function run(command, args, settings = {}) {
  const result = spawnSync(command, args, {
    cwd: settings.cwd ?? repositoryRoot,
    encoding: settings.encoding === null ? null : "utf8",
    maxBuffer: settings.maxBuffer ?? 64 * 1024 * 1024,
    timeout: settings.timeout ?? 60_000,
    env: {
      ...process.env,
      NO_COLOR: "1",
      GITLEAKS_NO_BANNER: "1",
      ...settings.env,
    },
  });
  const status = result.status ?? (result.error ? -1 : 0);
  const normalized = {
    status,
    stdout: result.stdout ?? (settings.encoding === null ? Buffer.alloc(0) : ""),
    stderr: result.stderr ?? (settings.encoding === null ? Buffer.alloc(0) : ""),
  };
  if (status !== 0 && !settings.allowFailure) {
    throw new Error(`${basename(command)} ${args[0] ?? ""} failed with status ${status}.`);
  }
  return normalized;
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function string(value, label) {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`${label} is missing.`);
  return value.trim();
}

function safeString(value) {
  return typeof value === "string" ? value : "";
}

function sanitizeError(value) {
  return value
    .replaceAll(options.releaseDirectory, "<release-dir>")
    .replaceAll(repositoryRoot, "<repository>")
    .replaceAll(temporaryRoot, "<temporary-dir>")
    .replaceAll(/[A-Za-z0-9_./+=-]{48,}/gu, "<redacted-long-token>")
    .slice(0, 500);
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

function pass(id, detail) {
  checks.push({ id, status: "PASS", detail });
}

function block(id, detail) {
  const sanitized = sanitizeError(detail);
  checks.push({ id, status: "BLOCKED", detail: sanitized });
  blockers.push(`${id}: ${sanitized}`);
}

function checkStatus(id) {
  const entries = checks.filter((item) => item.id === id);
  if (entries.length === 0 || entries.some((item) => item.status === "BLOCKED")) return "BLOCKED";
  return "PASS";
}
