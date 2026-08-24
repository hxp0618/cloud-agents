import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  mkdtempSync,
  mkdirSync,
  openSync,
  readFileSync,
  readdirSync,
  renameSync,
  realpathSync,
  rmSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const GENERATOR_SUPPLY_SOURCE_PATH = "tools/generator-supply/v1/source.json";
export const GENERATOR_SUPPLY_SOURCE_SCHEMA_PATH =
  "tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json";
export const GENERATOR_SUPPLY_OUTPUT_SCHEMA_PATH =
  "tools/generator-supply/v1/generator-supply-profile-v1.schema.json";
export const GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH =
  "tools/generator-supply/v1/evidence-manifest.json";
export const GENERATOR_SUPPLY_OUTPUT_PATH = "tools/generator-supply/v1/profile.json";

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v1/generator-supply-profile-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tooling/generator-supply/v1/generator-supply-profile-v1.schema.json";
const EVIDENCE_ALGORITHM = "sorted-path-nul-sha256-nul-size-v1";
const PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1";
const PROJECTION_ARCHIVE_REGULAR_FILE_MANIFEST_ALGORITHM =
  "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1";
const INPUT_TREE_MANIFEST_ALGORITHM = PROJECTION_ARCHIVE_REGULAR_FILE_MANIFEST_ALGORITHM;
const NODE_MODULES_MANIFEST_ALGORITHM = "utf8-bytewise-sorted-path-nul-sha256-nul-git-mode-v1";
const EVIDENCE_ROOT = "tools/generator-supply/v1/evidence";
const FORMATTED_SBOM_NON_PURL_AUTHORITY = {
  "darwin-bundle": {
    cyclonedxRecords: 15,
    cyclonedxSha256: "48a97a8142dfb7b8773caef67c8013e37c5d27f19ab05f42835e7fa89d7ae033",
    spdxRecords: 1,
    spdxSha256: "c7fc95520bc46ac048fd8683dca5efc6658368ace3c74f215b08a15364a4497e",
  },
  "linux-bundle": {
    cyclonedxRecords: 15,
    cyclonedxSha256: "ed7995855e38224134893b86de41ca37dbbc320b5320da04149bdcb26f54a501",
    spdxRecords: 1,
    spdxSha256: "2c7367b0b430b16d0d14ba6ce9769e8cac4657655457520d8317e87562260442",
  },
  "ubuntu-image": {
    cyclonedxRecords: 2266,
    cyclonedxSha256: "c7e690ceb036facaa9aa72b8c38379572753388dbe01b2e2b4beef4c0de8de0a",
    spdxRecords: 0,
    spdxSha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
  },
} as const;
const UBUNTU_IMAGE_IDENTITY = {
  registryIndexDigest: "sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517",
  platformManifestDigest: "sha256:fd225d3a1c5cecb1374f0d09c37a127d1f6f70e665941d6dab888c38b36c2131",
  configImageId: "sha256:a6f81fb630d51837271b89f8193810a5fc493fa4f30a55d7ebcdb3a66f3cc63a",
  rootfsLayerDigest: "sha256:b9a65b3c65ab22d490085bd0bf5490e2409da8748b406870f2463bdc41cd6795",
  exportTarSha256: "25ecc117cd77a289cc25006605dcf4ec8b137fec326db766d0abcd4147f6093e",
} as const;
type FormattedSbomScope = keyof typeof FORMATTED_SBOM_NON_PURL_AUTHORITY;
const PROJECTION_EXCLUSIONS = [
  "contracts/generation.lock.json",
  "tools/generator-supply/v1/evidence-manifest.json",
  "tools/generator-supply/v1/profile.json",
  "tools/generator-supply/v1/evidence/replay.json",
  "tools/generator-supply/v1/evidence/replay/darwin-a.json",
  "tools/generator-supply/v1/evidence/replay/darwin-b.json",
  "tools/generator-supply/v1/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v1/evidence/replay/linux-a.json",
  "tools/generator-supply/v1/evidence/replay/linux-b.json",
  "tools/generator-supply/v1/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v1/evidence/replay/projection.json",
  "tools/generator-supply/v1/evidence/replay/rejected-executor.json",
  "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md",
] as const;
const REPLAY_SUMMARY_PATH = `${EVIDENCE_ROOT}/replay.json`;
const SEMANTIC_EVIDENCE_PATHS = [
  `${EVIDENCE_ROOT}/THIRD_PARTY_NOTICES.md`,
  `${EVIDENCE_ROOT}/artifacts.json`,
  `${EVIDENCE_ROOT}/go-plugins.json`,
  `${EVIDENCE_ROOT}/notice-summary.json`,
  `${EVIDENCE_ROOT}/npm.json`,
  `${EVIDENCE_ROOT}/replay.json`,
  `${EVIDENCE_ROOT}/replay/darwin-a.json`,
  `${EVIDENCE_ROOT}/replay/darwin-b.json`,
  `${EVIDENCE_ROOT}/replay/darwin-isolation.json`,
  `${EVIDENCE_ROOT}/replay/linux-a.json`,
  `${EVIDENCE_ROOT}/replay/linux-b.json`,
  `${EVIDENCE_ROOT}/replay/linux-isolation.json`,
  `${EVIDENCE_ROOT}/replay/projection.json`,
  `${EVIDENCE_ROOT}/replay/rejected-executor.json`,
  `${EVIDENCE_ROOT}/sbom-summary.json`,
  `${EVIDENCE_ROOT}/sbom/darwin-bundle.cdx.json`,
  `${EVIDENCE_ROOT}/sbom/darwin-bundle.spdx.json`,
  `${EVIDENCE_ROOT}/sbom/darwin-bundle.syft.json`,
  `${EVIDENCE_ROOT}/sbom/linux-bundle.cdx.json`,
  `${EVIDENCE_ROOT}/sbom/linux-bundle.spdx.json`,
  `${EVIDENCE_ROOT}/sbom/linux-bundle.syft.json`,
  `${EVIDENCE_ROOT}/sbom/node-24.13.1-rejected-darwin.syft.json`,
  `${EVIDENCE_ROOT}/sbom/node-24.13.1-rejected-linux.syft.json`,
  `${EVIDENCE_ROOT}/sbom/ubuntu-image.cdx.json`,
  `${EVIDENCE_ROOT}/sbom/ubuntu-image.spdx.json`,
  `${EVIDENCE_ROOT}/sbom/ubuntu-image.syft.json`,
  `${EVIDENCE_ROOT}/security-repair.json`,
  `${EVIDENCE_ROOT}/ubuntu-image-binding.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-darwin.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-db-status.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-linux.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-node-24.13.1-rejected-darwin.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-node-24.13.1-rejected-linux.json`,
  `${EVIDENCE_ROOT}/vulnerability/grype-ubuntu.json`,
  `${EVIDENCE_ROOT}/vulnerability/osv.json`,
  `${EVIDENCE_ROOT}/vulnerability/osv-scanner-receipt.json`,
  `${EVIDENCE_ROOT}/vulnerability/summary.json`,
  `${EVIDENCE_ROOT}/wheelhouse-repair-lineage.json`,
  `${EVIDENCE_ROOT}/wheels.json`,
] as const;

type SupplySource = JsonRecord & {
  readonly profile: JsonRecord & {
    readonly officialArtifacts: readonly (JsonRecord & {
      readonly filename: string;
      readonly sha256: string;
      readonly sizeBytes: number;
    })[];
    readonly evidence: Readonly<Record<string, readonly string[]>>;
  };
};

type EvidenceFile = {
  readonly path: string;
  readonly sha256: string;
  readonly sizeBytes: number;
};

type JsonOverrides = ReadonlyMap<string, JsonRecord>;

type GeneratorSupplyInputSnapshot = {
  readonly originalRoot: string;
  readonly snapshotRoot: string;
  readonly files: ReadonlyMap<string, Buffer>;
  readonly evidencePaths: readonly string[];
  readonly excludedEvidencePaths: ReadonlySet<string>;
};

type GeneratorSupplyInputSnapshotOptions = {
  readonly excludedEvidencePaths?: readonly string[];
  readonly additionalPaths?: readonly string[];
};

export class GeneratorSupplyProfileError extends Error {
  constructor(
    readonly code:
      | "GENERATOR_SUPPLY_BINDING_MISMATCH"
      | "GENERATOR_SUPPLY_EVIDENCE_MISMATCH"
      | "GENERATOR_SUPPLY_PROFILE_STALE",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GeneratorSupplyProfileError";
  }
}

export function generatorSupplyEvidencePaths(root: string): string[] {
  return withGeneratorSupplyInputSnapshot(root, {}, (snapshot) =>
    generatorSupplyEvidencePathsInternal(snapshot.snapshotRoot),
  );
}

function generatorSupplyEvidencePathsInternal(root: string, overrides?: JsonOverrides): string[] {
  const source = readAndValidateSource(root);
  const paths = Object.values(source.profile.evidence)
    .flatMap((value) => [...value])
    .toSorted(bytewiseCompare);
  if (paths.length !== new Set(paths).size) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/evidence",
      "Generator supply evidence paths must be globally unique.",
    );
  }
  const forbidden = new Set([
    GENERATOR_SUPPLY_SOURCE_PATH,
    GENERATOR_SUPPLY_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_OUTPUT_SCHEMA_PATH,
    GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH,
    GENERATOR_SUPPLY_OUTPUT_PATH,
    "contracts/generation.lock.json",
  ]);
  if (paths.some((path) => forbidden.has(path))) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/evidence",
      "Generator supply evidence must not self-reference generated or lock outputs.",
    );
  }
  for (const path of paths) {
    if (!overrides?.has(path)) resolveContainedRegularFile(root, path);
  }
  validateEvidenceClosure(root, paths, overrides);
  return paths;
}

export function buildGeneratorSupplyEvidenceManifest(root: string): JsonRecord {
  return withGeneratorSupplyInputSnapshot(root, {}, (snapshot) =>
    buildGeneratorSupplyEvidenceManifestInternal(snapshot.snapshotRoot),
  );
}

function buildGeneratorSupplyEvidenceManifestInternal(
  root: string,
  overrides?: JsonOverrides,
): JsonRecord {
  const files: EvidenceFile[] = generatorSupplyEvidencePathsInternal(root, overrides).map(
    (path) => {
      const bytes = evidenceBytes(root, path, overrides);
      return {
        path,
        sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
        sizeBytes: bytes.byteLength,
      };
    },
  );
  return { algorithm: EVIDENCE_ALGORITHM, files };
}

export function generatorSupplyEvidenceManifestDigest(manifest: JsonRecord): string {
  return domainDigest("cloud-agents/generator-supply/evidence-manifest/v1", manifest);
}

export function buildGeneratorSupplyProfile(root: string): JsonRecord {
  return withGeneratorSupplyInputSnapshot(root, {}, (snapshot) =>
    buildGeneratorSupplyProfileInternal(snapshot.snapshotRoot),
  );
}

function buildGeneratorSupplyProfileInternal(root: string, overrides?: JsonOverrides): JsonRecord {
  const source = readAndValidateSource(root);
  validateEvidenceSemantics(root, overrides);
  const evidenceManifest = buildGeneratorSupplyEvidenceManifestInternal(root, overrides);
  const sourceDigest = domainDigest("cloud-agents/generator-supply/source/v1", source);
  const materialEvidence = source.profile.evidence.material.map((path) =>
    parseJsonFile(root, path, overrides),
  );
  const artifactSetDigest = domainDigest(
    "cloud-agents/generator-supply/artifact-set/v1",
    materialEvidence,
  );
  const evidenceManifestDigest = generatorSupplyEvidenceManifestDigest(evidenceManifest);
  const profileProjection = {
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    spec: source.profile,
    evidenceManifest,
  };
  const profileDigest = domainDigest("cloud-agents/generator-supply/profile/v1", profileProjection);
  const body = {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v1",
    registryId: "cloud-agents/generator-supply-profile",
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    profile: { profileDigest, spec: source.profile, evidenceManifest },
  };
  const generated = {
    ...body,
    registryDigest: domainDigest("cloud-agents/generator-supply/registry/v1", body),
  };
  validateOutput(root, generated);
  return generated;
}

export function serializeGeneratorSupplyJson(value: JsonRecord): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function writeGeneratorSupplyProfile(root: string): void {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, {
    excludedEvidencePaths: [REPLAY_SUMMARY_PATH],
  });
  try {
    const replaySummary = buildGeneratorSupplyReplaySummaryInternal(snapshot.snapshotRoot);
    const overrides = new Map<string, JsonRecord>([[REPLAY_SUMMARY_PATH, replaySummary]]);
    const manifest = buildGeneratorSupplyEvidenceManifestInternal(snapshot.snapshotRoot, overrides);
    const profile = buildGeneratorSupplyProfileInternal(snapshot.snapshotRoot, overrides);
    writeGeneratedSet(
      root,
      [
        { path: REPLAY_SUMMARY_PATH, value: replaySummary },
        { path: GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH, value: manifest },
        { path: GENERATOR_SUPPLY_OUTPUT_PATH, value: profile },
      ],
      renameSync,
      unlinkSync,
      () => assertGeneratorSupplyInputSnapshotCurrent(snapshot),
    );
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

/** Test-only transaction seam used to prove rollback on a caught rename error. */
export function writeGeneratorSupplyOutputsForTest(
  root: string,
  outputs: readonly { path: string; value: JsonRecord }[],
  failOnRename: number,
  failOnBackupRemoval = -1,
): void {
  let renameCount = 0;
  let backupRemovalCount = 0;
  writeGeneratedSet(
    root,
    outputs,
    (source, destination) => {
      renameCount += 1;
      if (renameCount === failOnRename) {
        throw new Error("injected generator output rename failure");
      }
      renameSync(source, destination);
    },
    (path) => {
      backupRemovalCount += 1;
      if (backupRemovalCount === failOnBackupRemoval) {
        throw new Error("injected generated-output backup cleanup failure");
      }
      unlinkSync(path);
    },
  );
}

/** Test-only seam proving that output assembly is bound to one input snapshot. */
export function assertGeneratorSupplyInputSnapshotMutationForTest(
  root: string,
  mutate: () => void,
): void {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, {
    excludedEvidencePaths: [REPLAY_SUMMARY_PATH],
  });
  try {
    mutate();
    assertGeneratorSupplyInputSnapshotCurrent(snapshot);
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

/** Test-only seam proving the read-only gate also snapshots generated outputs. */
export function assertGeneratorSupplyReadSnapshotMutationForTest(
  root: string,
  mutate: () => void,
): void {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, {
    additionalPaths: [GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH, GENERATOR_SUPPLY_OUTPUT_PATH],
  });
  try {
    mutate();
    assertGeneratorSupplyInputSnapshotCurrent(snapshot);
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

export function assertGeneratorSupplyProfileCurrent(root: string): void {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, {
    additionalPaths: [GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH, GENERATOR_SUPPLY_OUTPUT_PATH],
  });
  try {
    const snapshotRoot = snapshot.snapshotRoot;
    assertGeneratedBytes(
      snapshotRoot,
      REPLAY_SUMMARY_PATH,
      serializeGeneratorSupplyJson(buildGeneratorSupplyReplaySummaryInternal(snapshotRoot)),
    );
    assertGeneratedBytes(
      snapshotRoot,
      GENERATOR_SUPPLY_EVIDENCE_MANIFEST_PATH,
      serializeGeneratorSupplyJson(buildGeneratorSupplyEvidenceManifestInternal(snapshotRoot)),
    );
    assertGeneratedBytes(
      snapshotRoot,
      GENERATOR_SUPPLY_OUTPUT_PATH,
      serializeGeneratorSupplyJson(buildGeneratorSupplyProfileInternal(snapshotRoot)),
    );
    const projection = parseJsonFile(snapshotRoot, `${EVIDENCE_ROOT}/replay/projection.json`);
    assertGeneratorSupplyCoreProjectionCurrent(root, projection);
    assertGeneratorSupplyInputSnapshotCurrent(snapshot);
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

/**
 * Verifies projection freshness against the staged Git index and deterministic
 * archive bytes.  The replay receipt is only the expected value here; it is
 * not treated as an authority for the current checkout.  Generated evidence,
 * profile, lock and review outputs are intentionally late-bound exclusions,
 * matching the wrapper's projection contract.
 */
export function assertGeneratorSupplyCoreProjectionCurrent(
  root: string,
  receipt: JsonRecord,
): void {
  const expectedTree = receipt.treeSha;
  const expectedArchive = receipt.archiveSha256;
  const expectedSize = receipt.archiveSizeBytes;
  if (
    typeof expectedTree !== "string" ||
    !/^[0-9a-f]{40}$/u.test(expectedTree) ||
    typeof expectedArchive !== "string" ||
    !/^sha256:[0-9a-f]{64}$/u.test(expectedArchive) ||
    !Number.isInteger(expectedSize) ||
    Number(expectedSize) <= 0
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_PROFILE_STALE",
      "/replay/projection/currentness",
      "Projection receipt does not contain a valid tree, archive digest, and archive size authority.",
    );
  }
  let actual: {
    treeSha: string;
    archiveSha256: string;
    archiveSizeBytes: number;
    archiveInspection: JsonRecord;
  };
  try {
    actual = buildCurrentStagedCoreProjection(root);
  } catch (error) {
    throw supplyError(
      "GENERATOR_SUPPLY_PROFILE_STALE",
      "/replay/projection/currentness",
      `Current staged core projection is unavailable: ${String(error)}`,
    );
  }
  if (
    actual.treeSha !== expectedTree ||
    actual.archiveSha256 !== expectedArchive ||
    actual.archiveSizeBytes !== expectedSize ||
    canonicalJsonString(actual.archiveInspection) !==
      canonicalJsonString(
        recordValue(receipt.archiveInspection, "/replay/projection/archiveInspection"),
      )
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_PROFILE_STALE",
      "/replay/projection/currentness",
      `Replay projection is stale for the current staged checkout: expected=${JSON.stringify({ treeSha: expectedTree, archiveSha256: expectedArchive, archiveSizeBytes: expectedSize })} actual=${JSON.stringify(actual)}.`,
    );
  }
}

/**
 * Builds the exact projection authority from a read-only copy of the current
 * Git index.  Exported for adversarial tests and review tooling; callers must
 * still compare its result to a separately supplied receipt.
 */
export function buildCurrentStagedCoreProjection(root: string): {
  treeSha: string;
  archiveSha256: string;
  archiveSizeBytes: number;
  archiveInspection: JsonRecord;
} {
  const repositoryRoot = realpathSync(root);
  const git = "/usr/bin/git";
  const environment = projectionGitEnvironment();
  const topLevel = gitOutput(
    git,
    ["-C", repositoryRoot, "rev-parse", "--show-toplevel"],
    environment,
  );
  if (realpathSync(topLevel) !== repositoryRoot) {
    throw new Error("projection root is not the Git worktree root");
  }
  const untracked = gitPathList(
    git,
    repositoryRoot,
    ["ls-files", "--others", "--exclude-standard", "-z"],
    environment,
  ).filter((path) => !isProjectionExcludedPath(path));
  if (untracked.length !== 0) {
    throw new Error(
      `projection has untracked non-excluded core paths=${JSON.stringify(untracked.toSorted(bytewiseCompare))}`,
    );
  }
  execFileSync(git, ["-C", repositoryRoot, "diff", "--quiet", "--exit-code"], {
    env: environment,
    stdio: ["ignore", "ignore", "pipe"],
  });

  const fullTreeBefore = gitOutput(git, ["-C", repositoryRoot, "write-tree"], environment);
  if (!/^[0-9a-f]{40}$/u.test(fullTreeBefore)) {
    throw new Error("full staged Git tree SHA is invalid");
  }
  const temporaryRoot = mkdtempSync(join(tmpdir(), "cloud-agents-generator-projection-"));
  const temporaryIndex = join(temporaryRoot, "index");
  const archivePath = join(temporaryRoot, "projection.tar");
  try {
    const temporaryEnvironment = { ...environment, GIT_INDEX_FILE: temporaryIndex };
    execFileSync(git, ["-C", repositoryRoot, "read-tree", fullTreeBefore], {
      env: temporaryEnvironment,
      stdio: ["ignore", "ignore", "pipe"],
    });
    for (const pathspec of PROJECTION_EXCLUSIONS) {
      const paths = gitPathList(
        git,
        repositoryRoot,
        ["ls-files", "-z", "--", pathspec],
        temporaryEnvironment,
      );
      for (const path of paths) {
        execFileSync(git, ["-C", repositoryRoot, "update-index", "--force-remove", "--", path], {
          env: temporaryEnvironment,
          stdio: ["ignore", "ignore", "pipe"],
        });
      }
    }
    const stagedRecords = execFileSync(git, ["-C", repositoryRoot, "ls-files", "--stage", "-z"], {
      env: temporaryEnvironment,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    })
      .split("\0")
      .filter((record) => record !== "");
    for (const record of stagedRecords) {
      const separator = record.indexOf("\t");
      const fields = separator === -1 ? [] : record.slice(0, separator).split(" ");
      const path = separator === -1 ? "" : record.slice(separator + 1);
      if (
        fields.length !== 3 ||
        !["100644", "100755"].includes(fields[0]!) ||
        !/^[0-9a-f]{40}$/u.test(fields[1]!) ||
        fields[2] !== "0" ||
        path === "" ||
        path.includes("\n")
      ) {
        throw new Error(`projection rejects non-regular or ambiguous staged entry ${record}`);
      }
    }
    const treeSha = gitOutput(git, ["-C", repositoryRoot, "write-tree"], temporaryEnvironment);
    if (!/^[0-9a-f]{40}$/u.test(treeSha)) throw new Error("staged projection tree SHA is invalid");
    execFileSync(
      git,
      [
        "-c",
        "tar.umask=0022",
        "-C",
        repositoryRoot,
        "archive",
        "--format=tar",
        "--mtime=1970-01-01T00:00:00Z",
        `--output=${archivePath}`,
        treeSha,
      ],
      { env: temporaryEnvironment, stdio: ["ignore", "ignore", "pipe"] },
    );
    const fullTreeAfter = gitOutput(git, ["-C", repositoryRoot, "write-tree"], environment);
    if (fullTreeAfter !== fullTreeBefore) {
      throw new Error("Git index changed while building the core projection");
    }
    const inspectorPath = join(temporaryRoot, "inspect-generator-replay-archive.py");
    const inspectorBytes = execFileSync(
      git,
      ["-C", repositoryRoot, "show", ":scripts/lib/inspect-generator-replay-archive.py"],
      { env: temporaryEnvironment, stdio: ["ignore", "pipe", "pipe"] },
    );
    const stagedSourceBytes = execFileSync(
      git,
      ["-C", repositoryRoot, "show", `:${GENERATOR_SUPPLY_SOURCE_PATH}`],
      { env: temporaryEnvironment, stdio: ["ignore", "pipe", "pipe"] },
    );
    const stagedSource = recordValue(
      JSON.parse(stagedSourceBytes.toString("utf8")) as unknown,
      "/replay/projection/stagedSource",
    );
    const stagedProfile = recordValue(
      stagedSource.profile,
      "/replay/projection/stagedSource/profile",
    );
    const stagedReplayAuthority = recordValue(
      stagedProfile.replayAuthority,
      "/replay/projection/stagedSource/profile/replayAuthority",
    );
    const expectedInspectorSha256 = stagedReplayAuthority.replayArchiveInspectorSha256;
    const actualInspectorSha256 = createHash("sha256").update(inspectorBytes).digest("hex");
    if (
      typeof expectedInspectorSha256 !== "string" ||
      !/^[0-9a-f]{64}$/u.test(expectedInspectorSha256) ||
      actualInspectorSha256 !== expectedInspectorSha256
    ) {
      throw new Error("staged projection archive inspector is not source-authorized");
    }
    writeFileSync(inspectorPath, inspectorBytes, { flag: "wx", mode: 0o500 });
    const inspectionOutput = execFileSync(
      "/usr/bin/python3",
      [inspectorPath, "core-projection", archivePath],
      {
        env: { ...environment, PYTHONDONTWRITEBYTECODE: "1" },
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    const archiveInspection = recordValue(
      JSON.parse(inspectionOutput) as unknown,
      "/replay/projection/currentArchiveInspection",
    );
    requireExactRecordKeys(archiveInspection, "/replay/projection/currentArchiveInspection", [
      "formatVersion",
      "profile",
      "manifestAlgorithm",
      "manifestSha256",
      "entries",
      "regularFiles",
      "directories",
      "symlinks",
      "hardlinks",
      "unsafeEntries",
      "duplicateEntries",
      "specialEntries",
      "linkPrefixDescendants",
      "linkCycles",
      "regularFileManifestAlgorithm",
      "regularFileManifestSha256",
      "reconstructedGitTreeSha",
    ]);
    if (
      archiveInspection.formatVersion !== "cloud-agents-generator-replay-archive-inspection/v1" ||
      archiveInspection.profile !== "core-projection" ||
      archiveInspection.manifestAlgorithm !== PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM ||
      archiveInspection.regularFileManifestAlgorithm !==
        PROJECTION_ARCHIVE_REGULAR_FILE_MANIFEST_ALGORITHM ||
      archiveInspection.reconstructedGitTreeSha !== treeSha ||
      archiveInspection.symlinks !== 0 ||
      archiveInspection.hardlinks !== 0 ||
      archiveInspection.unsafeEntries !== 0 ||
      archiveInspection.duplicateEntries !== 0 ||
      archiveInspection.specialEntries !== 0 ||
      archiveInspection.linkPrefixDescendants !== 0 ||
      archiveInspection.linkCycles !== 0 ||
      !Number.isInteger(archiveInspection.entries) ||
      Number(archiveInspection.entries) <= 0 ||
      !Number.isInteger(archiveInspection.regularFiles) ||
      Number(archiveInspection.regularFiles) <= 0
    ) {
      throw new Error("current staged projection archive inspection failed closed");
    }
    const archiveBytes = readFileSync(archivePath);
    return {
      treeSha,
      archiveSha256: `sha256:${createHash("sha256").update(archiveBytes).digest("hex")}`,
      archiveSizeBytes: archiveBytes.byteLength,
      archiveInspection,
    };
  } finally {
    rmSync(temporaryRoot, { force: true, recursive: true });
  }
}

export function buildGeneratorSupplyReplaySummary(root: string): JsonRecord {
  return withGeneratorSupplyInputSnapshot(
    root,
    { excludedEvidencePaths: [REPLAY_SUMMARY_PATH] },
    (snapshot) => buildGeneratorSupplyReplaySummaryInternal(snapshot.snapshotRoot),
  );
}

function buildGeneratorSupplyReplaySummaryInternal(root: string): JsonRecord {
  const runPaths = [
    `${EVIDENCE_ROOT}/replay/darwin-a.json`,
    `${EVIDENCE_ROOT}/replay/darwin-b.json`,
    `${EVIDENCE_ROOT}/replay/linux-a.json`,
    `${EVIDENCE_ROOT}/replay/linux-b.json`,
  ] as const;
  const reports = runPaths.map((path) => ({ path, report: parseJsonFile(root, path) }));
  const first = reports[0]!.report;
  const projectionKeys = [
    "projectionTreeSha",
    "projectionArchiveSha256",
    "projectionArchiveSizeBytes",
    "projectionArchiveMemberManifestAlgorithm",
    "projectionArchiveMemberManifestSha256",
    "projectionArchiveMembers",
    "inputTreeManifestAlgorithm",
    "inputTreeManifestSha256",
    "inputTreeFiles",
  ] as const;
  const projection = Object.fromEntries(projectionKeys.map((key) => [key, first[key]]));
  for (const { path, report } of reports) {
    const candidate = Object.fromEntries(projectionKeys.map((key) => [key, report[key]]));
    if (
      canonicalJsonString(candidate) !== canonicalJsonString(projection) ||
      report.candidateManifestSha256 !== first.candidateManifestSha256 ||
      report.replayManifestSha256 !== first.replayManifestSha256 ||
      report.candidateOutputsEqual !== true ||
      report.nonAllowlistedChanges !== 0
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${path}`,
        "Replay summary requires four reports with one exact projection and output manifest.",
      );
    }
  }
  const replayAuthority = recordValue(
    readAndValidateSource(root).profile.replayAuthority,
    "/profile/replayAuthority",
  );
  const darwinIsolationPath = `${EVIDENCE_ROOT}/replay/darwin-isolation.json`;
  const linuxIsolationPath = `${EVIDENCE_ROOT}/replay/linux-isolation.json`;
  const rejectedExecutorPath = `${EVIDENCE_ROOT}/replay/rejected-executor.json`;
  const projectionReceiptPath = `${EVIDENCE_ROOT}/replay/projection.json`;
  return {
    formatVersion: "cloud-agents-generator-supply-replay-summary/v1",
    status: "DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED",
    wrapperPolicy: replayAuthority.wrapperPolicy,
    wrapperSha256: `sha256:${String(replayAuthority.wrapperSha256)}`,
    authoritativeReplayScope: replayAuthority.authoritativeReplayScope,
    ...projection,
    candidateManifestSha256: first.candidateManifestSha256,
    darwinNetworkIsolation: reports[0]!.report.isolation,
    linuxNetworkIsolation: reports[2]!.report.isolation,
    candidateOutputsEqual: true,
    nonAllowlistedChanges: 0,
    runReportSha256: Object.fromEntries(
      reports.map(({ path }) => [
        path.split("/").at(-1)!.replace(".json", ""),
        fileSha256(root, path),
      ]),
    ),
    darwinIsolationSha256: fileSha256(root, darwinIsolationPath),
    linuxIsolationSha256: fileSha256(root, linuxIsolationPath),
    projectionReceiptSha256: fileSha256(root, projectionReceiptPath),
    rejectedExecutorSha256: fileSha256(root, rejectedExecutorPath),
    notGateClosure: true,
  };
}

export function assertGeneratorSupplyReplaySummaryCurrent(root: string): void {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, {});
  try {
    assertGeneratedBytes(
      snapshot.snapshotRoot,
      REPLAY_SUMMARY_PATH,
      serializeGeneratorSupplyJson(
        buildGeneratorSupplyReplaySummaryInternal(snapshot.snapshotRoot),
      ),
    );
    assertGeneratorSupplyInputSnapshotCurrent(snapshot);
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

export function generatorSupplyProfileInputs(root: string): string[] {
  return [
    GENERATOR_SUPPLY_SOURCE_PATH,
    GENERATOR_SUPPLY_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0028-p1-generator-supply-profile.md",
    "docs/plan/p1/g-contract-generator-supply-profile-implementation-20260824.md",
    "docs/plan/p1/g-contract-generator-supply-offline-wheelhouse-implementation-20260824.md",
    "docs/plan/p1/g-contract-generator-supply-offline-wheelhouse-independent-review-20260824.md",
    "tools/generator-supply/npm/.npmrc",
    "tools/generator-supply/npm/package.json",
    "tools/generator-supply/npm/package-lock.json",
    "tools/generator-supply/go/go.mod",
    "tools/generator-supply/go/go.sum",
    "tools/generator-supply/go/tools.go",
    "tools/contract-standards/uv.lock",
    "scripts/check-generator-supply-evidence.ts",
    "scripts/check-platform-contract-standards.ts",
    "scripts/generate-platform-generator-supply-profile.ts",
    "scripts/lib/inspect-generator-supply-archive.py",
    "scripts/lib/inspect-generator-supply-archive.test.py",
    "scripts/lib/generator-replay-path-authority.ts",
    "scripts/lib/inspect-generator-replay-archive.py",
    "scripts/lib/inspect-generator-replay-archive.test.py",
    "scripts/replay-platform-generators-isolated.sh",
    "scripts/replay-platform-generators.test.ts",
    "scripts/replay-platform-generators.ts",
    "scripts/lib/platform-generator-supply-profile.test.ts",
    "scripts/lib/platform-generator-supply-profile.ts",
    "scripts/lib/platform-json-semantics.ts",
    ...generatorSupplyEvidencePaths(root),
  ].toSorted(bytewiseCompare);
}

function readAndValidateSource(root: string): SupplySource {
  const source = parseJsonFile(root, GENERATOR_SUPPLY_SOURCE_PATH) as SupplySource;
  const ajv = createAjv(root);
  const validate = ajv.getSchema(SOURCE_SCHEMA_ID);
  if (validate === undefined || !validate(source)) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/source",
      `Generator supply source schema validation failed: ${JSON.stringify(validate?.errors ?? [])}`,
    );
  }
  const platforms = source.profile.platforms as readonly JsonRecord[];
  const expectedPlatforms = [
    ["darwin-arm64", "NATIVE_REPLAY_VERIFIED", true, 16],
    ["linux-amd64", "NATIVE_REPLAY_VERIFIED", true, 17],
    ["linux-arm64", "NOT_CLAIMED", false, null],
  ];
  if (
    JSON.stringify(
      platforms.map((platform) => [
        platform.id,
        platform.status,
        platform.nativeExecution,
        platform.npmInstalledPackages,
      ]),
    ) !== JSON.stringify(expectedPlatforms)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/platforms",
      "Generator supply claimed-platform matrix drifted.",
    );
  }
  const artifacts = source.profile.officialArtifacts;
  const ids = artifacts.map((artifact) => String(artifact.id));
  if (ids.length !== new Set(ids).size) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/officialArtifacts",
      "Official artifacts must have unique ids.",
    );
  }
  const replayAuthority = recordValue(source.profile.replayAuthority, "/profile/replayAuthority");
  requireEvidence(replayAuthority, "/profile/replayAuthority", {
    wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V1",
    wrapperPath: "scripts/replay-platform-generators-isolated.sh",
    runnerPath: "scripts/replay-platform-generators.ts",
    pathAuthorityPath: "scripts/lib/generator-replay-path-authority.ts",
    replayArchiveInspectorPath: "scripts/lib/inspect-generator-replay-archive.py",
    projectionPolicy: "ACYCLIC_CORE_PROJECTION_V1",
    projectionArchiveFormat: "GIT_ARCHIVE_TAR_FIXED_MTIME_1970",
    projectionArchiveByteBinding: "SHA256_AND_SIZE_BYTES_EXACT",
    projectionArchiveMemberManifestAlgorithm: PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM,
    inputTreeManifestAlgorithm: INPUT_TREE_MANIFEST_ALGORITHM,
    nodeModulesManifestAlgorithm: NODE_MODULES_MANIFEST_ALGORITHM,
    authoritativeReplayScope: "CORE_GENERATORS_ONLY_SUPPLY_PROFILE_AND_LOCK_POST_ASSEMBLY",
    isolationEvidenceAuthority: "VERSIONED_WRAPPER_SAME_BOUNDARY_RECEIPT",
  });
  for (const [pathKey, digestKey] of [
    ["wrapperPath", "wrapperSha256"],
    ["runnerPath", "runnerSha256"],
    ["pathAuthorityPath", "pathAuthoritySha256"],
    ["replayArchiveInspectorPath", "replayArchiveInspectorSha256"],
  ] as const) {
    if (replayAuthority[digestKey] === fileSha256(root, String(replayAuthority[pathKey]))) {
      continue;
    }
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/profile/replayAuthority/${digestKey}`,
      `Replay authority must bind the current bytes of ${String(replayAuthority[pathKey])}.`,
    );
  }
  return source;
}

function validateEvidenceSemantics(root: string, overrides?: JsonOverrides): void {
  const artifacts = parseJsonFile(root, "tools/generator-supply/v1/evidence/artifacts.json");
  const wheels = parseJsonFile(root, "tools/generator-supply/v1/evidence/wheels.json");
  const npm = parseJsonFile(root, "tools/generator-supply/v1/evidence/npm.json");
  const goPlugins = parseJsonFile(root, "tools/generator-supply/v1/evidence/go-plugins.json");
  const lineage = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/wheelhouse-repair-lineage.json",
  );
  const replay = parseJsonFile(root, "tools/generator-supply/v1/evidence/replay.json", overrides);
  const sbom = parseJsonFile(root, "tools/generator-supply/v1/evidence/sbom-summary.json");
  const notice = parseJsonFile(root, "tools/generator-supply/v1/evidence/notice-summary.json");
  const vulnerability = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/vulnerability/summary.json",
  );
  const securityRepair = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/security-repair.json",
  );

  requireEvidence(artifacts, "/artifacts", {
    status: "OFFICIAL_DIGEST_AND_EXECUTABLE_BYTES_VERIFIED",
    officialArtifacts: 15,
    darwinExecutables: 9,
    linuxExecutables: 9,
    evidenceExecutables: 3,
  });
  requireEvidence(wheels, "/wheels", {
    status: "EXACT_WHEELHOUSE_BYTES_VERIFIED",
    darwinWheelCount: 21,
    linuxWheelCount: 21,
    sourceBuild: "FORBIDDEN",
  });
  requireEvidence(npm, "/npm", {
    formatVersion: "cloud-agents-generator-supply-npm/v1",
    status: "STERILE_OFFICIAL_REGISTRY_LOCK_AND_NATIVE_LOAD_VERIFIED",
    darwinInstalledPackages: 16,
    linuxInstalledPackages: 17,
    linuxLoadedBinding: "@oxfmt/binding-linux-x64-gnu",
    rootBunLockAuthority: "LEGACY_CONTEXT_ONLY",
  });
  validateNpmMaterialEvidence(root, npm);
  requireEvidence(goPlugins, "/goPlugins", {
    status: "EXACT_MODULE_SUM_AND_BUILD_RECEIPT_VERIFIED",
    platforms: 2,
    binaries: 4,
  });
  requireEvidence(lineage, "/wheelhouseRepairLineage", {
    status: "APPROVE_P0_0_P1_0_P2_0",
    implementationCommit: "51ce2d6b9faa71a5e89ccf709864f4d570454a38",
    reviewCommit: "2a8600b5694b45e39b0c209ae97cbe8f03561339",
  });
  if (
    lineage.currentRunnerPath !== "scripts/check-platform-contract-standards.ts" ||
    lineage.currentRunnerSha256 !== fileSha256(root, String(lineage.currentRunnerPath))
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/wheelhouseRepairLineage/currentRunner",
      "Wheelhouse lineage must bind the current runner path and its current bytes.",
    );
  }
  requireEvidence(replay, "/replay", {
    status: "DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED",
    darwinNetworkIsolation: "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES",
    linuxNetworkIsolation: "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS",
    candidateOutputsEqual: true,
    nonAllowlistedChanges: 0,
  });
  requireEvidence(sbom, "/sbom", {
    status: "SYFT_1_51_0_DUAL_FORMAT_EFFECTIVE_BUNDLES_AND_IMAGE",
    cyclonedx: "1.6",
    spdx: "2.3",
    formattedDocuments: 6,
    rawDocuments: 3,
    documents: 9,
  });
  requireEvidence(notice, "/notice", {
    status: "INVENTORY_AND_TEXT_COMPLETE",
    legalApproval: "NOT_CLAIMED",
  });
  requireEvidence(vulnerability, "/vulnerability", {
    status: "CURRENT_SCAN_NO_UNEXPLAINED_HIGH_CRITICAL",
    critical: 0,
    high: 0,
    parseFailures: 0,
    timelessCleanClaim: false,
  });
  requireEvidence(securityRepair, "/securityRepair", {
    status: "NODE_24_13_1_REJECTED_NODE_24_18_1_CURRENT",
    decision: "VERSIONED_SECURITY_REPAIR_NOT_WAIVER",
    predecessorProfile: "NOT_CREATED_REJECTED_BEFORE_FIRST_PROFILE",
    rejectedHighPerPlatform: 6,
    currentCritical: 0,
    currentHigh: 0,
    waiver: "NOT_PRODUCED_NOT_AUTHORIZED",
  });
  validateRawArtifactEvidence(root, artifacts);
  validateRawSbomEvidence(root);
  validateRawVulnerabilityEvidence(root, vulnerability, securityRepair);
  validateNoticeEvidence(root, notice);
  validateRawReplayEvidence(root, replay);
}

function validateNpmMaterialEvidence(root: string, npm: JsonRecord): void {
  requireExactRecordKeys(npm, "/npm", [
    "formatVersion",
    "status",
    "npm",
    "npmRuntime",
    "lockPackageRecordsIncludingRoot",
    "officialResolvedDependencyRecords",
    "nonOfficialResolvedRecords",
    "directExactDependencies",
    "packageLock",
    "darwinInstalledPackages",
    "linuxInstalledPackages",
    "darwinLoadedBinding",
    "linuxInstalledBindings",
    "linuxLoadedBinding",
    "rootBunLockAuthority",
    "rootBunLock",
    "installed",
  ]);
  const lock = parseJsonFile(root, "tools/generator-supply/npm/package-lock.json");
  const lockPackages = recordValue(lock.packages, "/npm/packageLock/packages");
  if (
    lock.name !== "cloud-agents-contract-generator-supply" ||
    lock.version !== "0.0.0" ||
    lock.lockfileVersion !== 3 ||
    lock.requires !== true ||
    Object.keys(lockPackages).length !== 35
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock",
      "Generator npm package-lock must be the exact lockfile v3 closure with 35 package records including root.",
    );
  }
  const rootPackage = recordValue(lockPackages[""], "/npm/packageLock/packages/root");
  const directDependencies = recordValue(
    rootPackage.dependencies,
    "/npm/packageLock/root/dependencies",
  );
  const expectedDirectDependencies = {
    "@bufbuild/protobuf": "2.14.0",
    "@bufbuild/protoc-gen-es": "2.14.0",
    ajv: "8.20.0",
    "ajv-formats": "3.0.1",
    oxfmt: "0.62.0",
  };
  if (canonicalJsonString(directDependencies) !== canonicalJsonString(expectedDirectDependencies)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock/root/dependencies",
      "Generator npm direct dependency closure drifted.",
    );
  }
  const lockRecords = Object.entries(lockPackages)
    .filter(([path]) => path !== "")
    .map(([path, value]) => {
      const packageValue = recordValue(value, `/npm/packageLock/packages/${path}`);
      if (
        !path.startsWith("node_modules/") ||
        path === "node_modules/" ||
        typeof packageValue.version !== "string" ||
        typeof packageValue.integrity !== "string" ||
        typeof packageValue.resolved !== "string" ||
        !packageValue.resolved.startsWith("https://registry.npmjs.org/")
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/npm/packageLock/packages/${path}`,
          "Every resolved npm package record must bind an official registry URL and integrity.",
        );
      }
      return { path, packageValue };
    });
  if (lockRecords.length !== 34) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock/packages",
      "Generator npm package-lock must contain exactly 34 resolved dependency records.",
    );
  }
  if (
    npm.lockPackageRecordsIncludingRoot !== 35 ||
    npm.officialResolvedDependencyRecords !== 34 ||
    npm.nonOfficialResolvedRecords !== 0 ||
    npm.directExactDependencies !== Object.keys(expectedDirectDependencies).length ||
    npm.packageLock?.path !== "tools/generator-supply/npm/package-lock.json" ||
    npm.packageLock?.sha256 !== fileSha256(root, "tools/generator-supply/npm/package-lock.json") ||
    npm.packageLock?.sizeBytes !==
      readFileSync(
        resolveContainedRegularFile(root, "tools/generator-supply/npm/package-lock.json"),
      ).byteLength
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock",
      "npm evidence counters and package-lock identity must be derived from the current repository lock.",
    );
  }
  const packageLockIdentity = recordValue(npm.packageLock, "/npm/packageLock");
  requireExactRecordKeys(packageLockIdentity, "/npm/packageLock", ["path", "sha256", "sizeBytes"]);
  for (const installation of recordArray(npm.installed, "/npm/installed")) {
    requireExactRecordKeys(installation, `/npm/installed/${String(installation.platform)}`, [
      "platform",
      "installedPackageCount",
      "hiddenLock",
      "nodeModules",
      "packages",
    ]);
    const platform = String(installation.platform);
    if (platform !== "darwin-arm64" && platform !== "linux-amd64") {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/npm/installed/${platform}`,
        "Only the two claimed native npm installations may be present.",
      );
    }
    const packages = recordArray(installation.packages, `/npm/installed/${platform}/packages`);
    for (const [index, packageEntry] of packages.entries()) {
      requireExactRecordKeys(packageEntry, `/npm/installed/${platform}/packages/${index}`, [
        "path",
        "version",
        "integrity",
        "resolved",
      ]);
    }
    const expectedPackages = lockRecords
      .filter((entry) => lockPackageCompatible(entry.packageValue, platform))
      .map((entry) => ({
        path: entry.path,
        version: entry.packageValue.version,
        integrity: entry.packageValue.integrity,
        resolved: entry.packageValue.resolved,
      }))
      .toSorted((left, right) => bytewiseCompare(left.path, right.path));
    const actualPackages = packages
      .map((entry) => ({
        path: entry.path,
        version: entry.version,
        integrity: entry.integrity,
        resolved: entry.resolved,
      }))
      .toSorted((left, right) => bytewiseCompare(String(left.path), String(right.path)));
    if (
      packages.length !== expectedPackages.length ||
      canonicalJsonString({ values: actualPackages }) !==
        canonicalJsonString({ values: expectedPackages })
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/npm/installed/${platform}/packages`,
        "Installed npm records must exactly equal the current package-lock platform closure.",
      );
    }
    const hiddenLock = recordValue(
      installation.hiddenLock,
      `/npm/installed/${platform}/hiddenLock`,
    );
    requireExactRecordKeys(hiddenLock, `/npm/installed/${platform}/hiddenLock`, [
      "sha256",
      "sizeBytes",
    ]);
    const hiddenPackages = Object.fromEntries(
      lockRecords
        .filter((entry) => lockPackageCompatible(entry.packageValue, platform))
        .map(({ path, packageValue }) => [path, packageValue]),
    );
    const expectedHiddenLock = {
      name: lock.name,
      version: lock.version,
      lockfileVersion: lock.lockfileVersion,
      requires: lock.requires,
      packages: hiddenPackages,
    };
    const expectedHiddenLockBytes = Buffer.from(
      `${JSON.stringify(expectedHiddenLock, null, 2)}\n`,
      "utf8",
    );
    const expectedHiddenLockSha256 = createHash("sha256")
      .update(expectedHiddenLockBytes)
      .digest("hex");
    if (
      hiddenLock.sha256 !== expectedHiddenLockSha256 ||
      hiddenLock.sizeBytes !== expectedHiddenLockBytes.byteLength ||
      installation.installedPackageCount !== expectedPackages.length
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/npm/installed/${platform}/hiddenLock`,
        "Hidden npm lock identity and installed count must bind the exact platform-filtered lock closure.",
      );
    }
    const nodeModules = recordValue(
      installation.nodeModules,
      `/npm/installed/${platform}/nodeModules`,
    );
    requireExactRecordKeys(nodeModules, `/npm/installed/${platform}/nodeModules`, [
      "algorithm",
      "sha256",
      "files",
      "symlinks",
      "topLevelEntries",
      "declaredPackageRoots",
      "undeclaredEntries",
      "cacheEntries",
    ]);
    const declaredPackageRoots = packages
      .map((entry) => String(entry.path))
      .map((path) => {
        if (!path.startsWith("node_modules/") || path === "node_modules/") {
          throw supplyError(
            "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
            `/npm/installed/${platform}/packages`,
            `Invalid hidden-lock package path ${path}.`,
          );
        }
        return path.slice("node_modules/".length);
      })
      .toSorted(bytewiseCompare);
    const expectedTopLevel = [
      ".bin",
      ".package-lock.json",
      ...new Set(declaredPackageRoots.map((path) => path.split("/")[0]!)),
    ].toSorted(bytewiseCompare);
    const expectedSymlinks = [
      { path: ".bin/oxfmt", target: "../oxfmt/bin/oxfmt" },
      { path: ".bin/protoc-gen-es", target: "../@bufbuild/protoc-gen-es/bin/protoc-gen-es" },
      { path: ".bin/tsc", target: "../typescript/bin/tsc" },
      { path: ".bin/tsserver", target: "../typescript/bin/tsserver" },
    ];
    if (
      nodeModules.algorithm !== NODE_MODULES_MANIFEST_ALGORITHM ||
      !/^[0-9a-f]{64}$/u.test(String(nodeModules.sha256)) ||
      !Number.isInteger(nodeModules.files) ||
      Number(nodeModules.files) <= 0 ||
      canonicalJsonString({ values: nodeModules.symlinks }) !==
        canonicalJsonString({ values: expectedSymlinks }) ||
      nodeModules.undeclaredEntries !== 0 ||
      nodeModules.cacheEntries !== 0 ||
      canonicalJsonString({ values: nodeModules.declaredPackageRoots }) !==
        canonicalJsonString({ values: declaredPackageRoots }) ||
      canonicalJsonString({ values: nodeModules.topLevelEntries }) !==
        canonicalJsonString({ values: expectedTopLevel })
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/npm/installed/${platform}/nodeModules`,
        "node_modules evidence must be the exact hidden-lock top-level closure with no runtime cache or undeclared entry.",
      );
    }
  }
  const installations = recordArray(npm.installed, "/npm/installed");
  if (
    installations.length !== 2 ||
    new Set(installations.map((entry) => String(entry.platform))).size !== 2 ||
    npm.darwinInstalledPackages !==
      installations.find((entry) => entry.platform === "darwin-arm64")?.installedPackageCount ||
    npm.linuxInstalledPackages !==
      installations.find((entry) => entry.platform === "linux-amd64")?.installedPackageCount
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/installed",
      "Npm platform counts must be derived from the two exact hidden-lock closures.",
    );
  }
  const npmRuntime = recordValue(npm.npmRuntime, "/npm/npmRuntime");
  const npmRuntimePackage = recordValue(npmRuntime.package, "/npm/npmRuntime/package");
  const npmRuntimeCli = recordValue(npmRuntime.cli, "/npm/npmRuntime/cli");
  const rootBunLock = recordValue(npm.rootBunLock, "/npm/rootBunLock");
  requireExactRecordKeys(npmRuntime, "/npm/npmRuntime", ["package", "cli"]);
  requireExactRecordKeys(npmRuntimePackage, "/npm/npmRuntime/package", ["sha256", "sizeBytes"]);
  requireExactRecordKeys(npmRuntimeCli, "/npm/npmRuntime/cli", ["sha256", "sizeBytes"]);
  requireExactRecordKeys(rootBunLock, "/npm/rootBunLock", [
    "path",
    "sha256",
    "registryContext",
    "generatorExecutionAuthority",
  ]);
  if (
    npm.npm !== "11.8.0" ||
    npm.darwinLoadedBinding !== "@oxfmt/binding-darwin-arm64" ||
    canonicalJsonString({ values: npm.linuxInstalledBindings }) !==
      canonicalJsonString({
        values: ["@oxfmt/binding-linux-x64-gnu", "@oxfmt/binding-linux-x64-musl"],
      }) ||
    npm.linuxLoadedBinding !== "@oxfmt/binding-linux-x64-gnu" ||
    !/^[0-9a-f]{64}$/u.test(String(npmRuntimePackage.sha256)) ||
    npmRuntimePackage.sizeBytes !== 6620 ||
    !/^[0-9a-f]{64}$/u.test(String(npmRuntimeCli.sha256)) ||
    npmRuntimeCli.sizeBytes !== 54 ||
    rootBunLock.path !== "bun.lock" ||
    rootBunLock.sha256 !== fileSha256(root, "bun.lock") ||
    rootBunLock.registryContext !== "registry.npmmirror.com" ||
    rootBunLock.generatorExecutionAuthority !== false
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/npmRuntime",
      "Npm runtime and legacy root Bun lock identity must be explicitly bound.",
    );
  }
}

function lockPackageCompatible(packageValue: JsonRecord, platform: string): boolean {
  const [os, cpu] = platform === "darwin-arm64" ? ["darwin", "arm64"] : ["linux", "x64"];
  const allowedOs = packageValue.os;
  const allowedCpu = packageValue.cpu;
  if (
    allowedOs !== undefined &&
    (!Array.isArray(allowedOs) || !allowedOs.every((value) => typeof value === "string"))
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock/packages",
      "Npm package-lock os constraints must be arrays of strings.",
    );
  }
  if (
    allowedCpu !== undefined &&
    (!Array.isArray(allowedCpu) || !allowedCpu.every((value) => typeof value === "string"))
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/npm/packageLock/packages",
      "Npm package-lock cpu constraints must be arrays of strings.",
    );
  }
  return (
    matchesNpmConstraint(allowedOs as string[] | undefined, os) &&
    matchesNpmConstraint(allowedCpu as string[] | undefined, cpu)
  );
}

function matchesNpmConstraint(values: readonly string[] | undefined, target: string): boolean {
  if (values === undefined) return true;
  if (values.length === 0) return false;
  const positives = values.filter((value) => !value.startsWith("!"));
  const negatives = values.filter((value) => value.startsWith("!")).map((value) => value.slice(1));
  return !negatives.includes(target) && (positives.length === 0 || positives.includes(target));
}

function validateRawArtifactEvidence(root: string, artifacts: JsonRecord): void {
  const source = readAndValidateSource(root);
  const expectedArchives = new Map(
    source.profile.officialArtifacts.map((artifact) => [String(artifact.id), artifact]),
  );
  const archives = recordArray(artifacts.archives, "/artifacts/archives");
  const archiveBindings: JsonRecord[] = [];
  if (archives.length !== expectedArchives.size) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/artifacts/archives",
      "Every official artifact must have exactly one collected archive or bare-binary record.",
    );
  }
  for (const archive of archives) {
    const id = String(archive.id);
    const expected = expectedArchives.get(id);
    if (
      expected === undefined ||
      archive.filename !== expected.filename ||
      archive.sha256 !== expected.sha256 ||
      archive.sizeBytes !== expected.sizeBytes ||
      archive.verifiedSha256 !== expected.sha256 ||
      archive.verifiedSizeBytes !== expected.sizeBytes
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/artifacts/archives/${id}`,
        "Collected official artifact identity drifted from the source registry.",
      );
    }
    const bindings = recordArray(
      archive.effectiveExecutables,
      `/artifacts/archives/${id}/effectiveExecutables`,
    );
    archiveBindings.push(...bindings);
    if (id === "osv-scanner-darwin-arm64") {
      if (
        archive.distribution !== "BARE_OFFICIAL_BINARY" ||
        archive.extractionAudit !== "NOT_APPLICABLE_BARE_BINARY" ||
        bindings.length !== 1 ||
        bindings[0]?.memberPath !== null ||
        bindings[0]?.memberSha256 !== expected.sha256 ||
        bindings[0]?.effectiveSha256 !== expected.sha256 ||
        bindings[0]?.memberSizeBytes !== expected.sizeBytes ||
        bindings[0]?.effectiveSizeBytes !== expected.sizeBytes ||
        bindings[0]?.provenance !== "OFFICIAL_BARE_BINARY_BYTES_EXACT"
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/artifacts/archives/${id}/bareBinary`,
          "The official bare OSV-Scanner artifact must equal the effective executable bytes.",
        );
      }
      continue;
    }
    const audit = recordValue(archive.extractionAudit, `/artifacts/archives/${id}/extractionAudit`);
    requireEvidence(audit, `/artifacts/archives/${id}/extractionAudit`, {
      formatVersion: "cloud-agents-generator-supply-archive-inspection/v1",
      inventoryAlgorithm: "sorted-path-nul-type-nul-link-target-nul-v1",
      unsafeEntries: 0,
      duplicateEntries: 0,
      specialEntries: 0,
      linkTargetsResolveToRegularFiles: true,
      linkCycles: 0,
      linkPrefixDescendants: 0,
    });
    if (
      archive.distribution !== "ARCHIVE" ||
      !["tar", "zip"].includes(String(audit.archiveFormat)) ||
      !/^[0-9a-f]{64}$/u.test(String(audit.inventorySha256)) ||
      !Number.isInteger(audit.entries) ||
      Number(audit.entries) <= 0 ||
      Number(audit.regularFiles) <= 0
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/artifacts/archives/${id}/extractionAudit`,
        "Archive inventory must be non-empty and use the fail-closed safe-member audit.",
      );
    }
    const selected = new Map(
      recordArray(audit.selectedMembers, `/artifacts/archives/${id}/selectedMembers`).map(
        (member) => [String(member.path), member],
      ),
    );
    if (selected.size !== bindings.length) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/artifacts/archives/${id}/selectedMembers`,
        "Every selected regular archive member must map to one effective executable.",
      );
    }
    for (const binding of bindings) {
      const member = selected.get(String(binding.memberPath));
      if (
        member === undefined ||
        binding.provenance !== "SAFE_ARCHIVE_REGULAR_MEMBER_BYTES_EXACT" ||
        binding.memberSha256 !== member.sha256 ||
        binding.memberSizeBytes !== member.sizeBytes ||
        binding.effectiveSha256 !== member.sha256 ||
        binding.effectiveSizeBytes !== member.sizeBytes ||
        !/^[0-9a-f]{64}$/u.test(String(member.sha256)) ||
        !Number.isInteger(member.sizeBytes) ||
        Number(member.sizeBytes) <= 0
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/artifacts/archives/${id}/effectiveExecutables`,
          "Archive member bytes must exactly equal the effective executable bytes.",
        );
      }
    }
  }

  const effectiveRecords = [
    ...Object.entries(recordValue(artifacts.executables, "/artifacts/executables")).flatMap(
      ([platform, value]) =>
        recordArray(value, `/artifacts/executables/${platform}`)
          .filter((entry) => !String(entry.id).startsWith("protoc-gen-"))
          .map((entry) => ({ ...entry, platform })),
    ),
    ...recordArray(artifacts.scanners, "/artifacts/scanners").map((entry) => ({
      ...entry,
      platform: "evidence-darwin-arm64",
    })),
  ];
  const key = (value: JsonRecord): string =>
    `${String(value.platform)}\0${String(value.id)}\0${String(value.path ?? value.effectivePath)}`;
  const expectedByKey = new Map(effectiveRecords.map((entry) => [key(entry), entry]));
  const actualByKey = new Map(archiveBindings.map((entry) => [key(entry), entry]));
  if (
    expectedByKey.size !== effectiveRecords.length ||
    actualByKey.size !== archiveBindings.length
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/artifacts/effectiveExecutableClosure",
      "Effective executable provenance keys must be globally unique.",
    );
  }
  for (const [identity, expected] of expectedByKey) {
    const binding = actualByKey.get(identity);
    if (
      binding === undefined ||
      binding.effectiveSha256 !== expected.sha256 ||
      binding.effectiveSizeBytes !== expected.sizeBytes
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        "/artifacts/effectiveExecutableClosure",
        `Effective executable has no exact official archive provenance: ${identity}.`,
      );
    }
  }
  if (expectedByKey.size !== actualByKey.size) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/artifacts/effectiveExecutableClosure",
      "Archive provenance must not contain undeclared effective executables.",
    );
  }
}

function validateRawSbomEvidence(root: string): void {
  const summary = parseJsonFile(root, `${EVIDENCE_ROOT}/sbom-summary.json`);
  const summaryPlatforms = recordValue(summary.platforms, "/sbom/summary/platforms");
  const expected = [
    ["darwin-bundle", "darwin-arm64", 583, "24.18.1"],
    ["linux-bundle", "linux-amd64", 596, "24.18.1"],
    ["ubuntu-image", "ubuntu-amd64", 92, null],
  ] as const;
  const expectedPlatformSummaries: Record<string, JsonRecord> = {};
  for (const [scope, platform, packageCount, nodeVersion] of expected) {
    const sbom = parseJsonFile(root, `tools/generator-supply/v1/evidence/sbom/${scope}.syft.json`);
    const cdx = parseJsonFile(root, `tools/generator-supply/v1/evidence/sbom/${scope}.cdx.json`);
    const spdx = parseJsonFile(root, `tools/generator-supply/v1/evidence/sbom/${scope}.spdx.json`);
    validateRawSyftDocument(sbom, `/sbom/${scope}.syft`);
    requireExactRecordKeys(cdx, `/sbom/${scope}.cdx`, [
      "$schema",
      "bomFormat",
      "specVersion",
      "serialNumber",
      "version",
      "metadata",
      "components",
      "dependencies",
    ]);
    requireExactRecordKeys(spdx, `/sbom/${scope}.spdx`, [
      "spdxVersion",
      "dataLicense",
      "SPDXID",
      "name",
      "documentNamespace",
      "creationInfo",
      "packages",
      "files",
      "hasExtractedLicensingInfos",
      "relationships",
    ]);
    const packages = recordArray(sbom.artifacts, `/sbom/${scope}/artifacts`);
    if (packages.length !== packageCount) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/${scope}/artifacts`,
        `Expected ${packageCount} packages, received ${packages.length}.`,
      );
    }
    if (nodeVersion !== null) {
      const versions = packages
        .filter((entry) => entry.name === "node")
        .map((entry) => entry.version)
        .toSorted((left, right) => bytewiseCompare(String(left), String(right)));
      if (JSON.stringify(versions) !== JSON.stringify([nodeVersion])) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/sbom/${scope}/node`,
          `Expected only Node ${nodeVersion}, received ${JSON.stringify(versions)}.`,
        );
      }
    }
    const descriptor = recordValue(sbom.descriptor, `/sbom/${scope}/descriptor`);
    if (descriptor.name !== "syft" || descriptor.version !== "1.51.0") {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/${scope}/descriptor`,
        "Raw SBOM must use exact Syft 1.51.0.",
      );
    }
    if (cdx.specVersion !== "1.6" || spdx.spdxVersion !== "SPDX-2.3") {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/${scope}/formats`,
        "Formatted SBOMs must be CycloneDX 1.6 and SPDX 2.3.",
      );
    }
    const rawPurls = strictPurlMultiset(
      packages.map((entry) => entry.purl),
      `/sbom/${scope}/artifacts/purl`,
    );
    const cdxComponents = recordArray(cdx.components, `/sbom/${scope}/components`);
    const spdxPackages = recordArray(spdx.packages, `/sbom/${scope}/packages`);
    const cdxPurls = formattedPurlMultiset(cdxComponents, `/sbom/${scope}/components`);
    const spdxPurls = formattedSpdxPurlMultiset(spdxPackages, `/sbom/${scope}/packages`);
    validateFormattedNonPurlClosure(scope, cdxComponents, spdxPackages);
    const spdxOnly = multisetDifference(spdxPurls, rawPurls);
    if (
      JSON.stringify(rawPurls) !== JSON.stringify(cdxPurls) ||
      JSON.stringify(multisetDifference(rawPurls, spdxPurls)) !== "[]" ||
      (scope === "ubuntu-image"
        ? spdxOnly.length !== 1 || !spdxOnly[0]!.startsWith("pkg:oci/sha256@")
        : spdxOnly.length !== 0)
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/${scope}/crossFormat`,
        "Raw Syft package PURLs must be the authority for CDX/SPDX package PURLs.",
      );
    }
    const platformSummary = recordValue(
      summaryPlatforms[platform],
      `/sbom/summary/platforms/${platform}`,
    );
    const projection = {
      packages: packageCount,
      ...(nodeVersion === null ? {} : { node: nodeVersion }),
      syftSha256: fileSha256(root, `${EVIDENCE_ROOT}/sbom/${scope}.syft.json`),
      cyclonedxSha256: fileSha256(root, `${EVIDENCE_ROOT}/sbom/${scope}.cdx.json`),
      spdxSha256: fileSha256(root, `${EVIDENCE_ROOT}/sbom/${scope}.spdx.json`),
      rawPurlCount: rawPurls.length,
      rawUniquePurlCount: new Set(rawPurls).size,
      rawPurlMultisetSha256: nulMultisetSha256(rawPurls),
      cyclonedxPurlMultisetSha256: nulMultisetSha256(cdxPurls),
      spdxPurlCount: spdxPurls.length,
      spdxPurlMultisetSha256: nulMultisetSha256(spdxPurls),
      spdxAdditionalImageIdentityPurls: spdxOnly,
    };
    if (canonicalJsonString(platformSummary) !== canonicalJsonString(projection)) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/summary/platforms/${platform}`,
        "SBOM summary must be the complete canonical projection of raw document bytes and PURL multisets.",
      );
    }
    expectedPlatformSummaries[platform] = projection;
    const cdxMetadata = recordValue(cdx.metadata, `/sbom/${scope}/cdxMetadata`);
    const spdxCreation = recordValue(spdx.creationInfo, `/sbom/${scope}/spdxCreation`);
    if (!isNonFutureDate(cdxMetadata.timestamp) || !isNonFutureDate(spdxCreation.created)) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/sbom/${scope}/timestamp`,
        "SBOM evidence must not be future-dated.",
      );
    }
  }

  // Historical rejected Node SBOMs are not part of the current three-document
  // summary, but they are still raw evidence and must have a complete,
  // well-formed PURL identity set before Grype can bind to them.
  for (const platform of ["darwin", "linux"] as const) {
    const path = `${EVIDENCE_ROOT}/sbom/node-24.13.1-rejected-${platform}.syft.json`;
    validateRawSyftDocument(parseJsonFile(root, path), `/${path}`);
  }

  const ubuntu = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/sbom/ubuntu-image.syft.json",
  );
  const ubuntuSource = recordValue(ubuntu.source, "/sbom/ubuntu/source");
  const ubuntuMetadata = recordValue(ubuntuSource.metadata, "/sbom/ubuntu/source/metadata");
  const ubuntuLayers = recordArray(ubuntuMetadata.layers, "/sbom/ubuntu/source/layers");
  const binding = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/ubuntu-image-binding.json",
  );
  const expectedUbuntu = UBUNTU_IMAGE_IDENTITY;
  requireEvidence(binding, "/sbom/ubuntuBinding", expectedUbuntu);
  if (
    ubuntuMetadata.imageID !== expectedUbuntu.configImageId ||
    ubuntuMetadata.manifestDigest !== expectedUbuntu.platformManifestDigest ||
    !stringArray(ubuntuMetadata.repoDigests, "/sbom/ubuntu/repoDigests").some((digest) =>
      digest.endsWith(expectedUbuntu.registryIndexDigest),
    ) ||
    ubuntuLayers.length !== 1 ||
    ubuntuLayers[0]?.digest !== expectedUbuntu.rootfsLayerDigest
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/sbom/ubuntuBinding",
      "Ubuntu index, platform manifest, config, layer, and export binding drifted.",
    );
  }
  const expectedSummary = {
    formatVersion: "cloud-agents-generator-supply-sbom-summary/v1",
    status: "SYFT_1_51_0_DUAL_FORMAT_EFFECTIVE_BUNDLES_AND_IMAGE",
    syft: "1.51.0",
    cyclonedx: "1.6",
    spdx: "2.3",
    formattedDocuments: 6,
    rawDocuments: 3,
    documents: 9,
    platforms: expectedPlatformSummaries,
    ubuntuImage: {
      registryIndexDigest: expectedUbuntu.registryIndexDigest,
      platformManifestDigest: expectedUbuntu.platformManifestDigest,
      localConfigImageId: expectedUbuntu.configImageId,
      rootfsLayerDigest: expectedUbuntu.rootfsLayerDigest,
      exportTarSha256: expectedUbuntu.exportTarSha256,
    },
  };
  if (canonicalJsonString(summary) !== canonicalJsonString(expectedSummary)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/sbom/summary",
      "SBOM summary must be the complete canonical projection of all current raw documents and image identity.",
    );
  }
}

function validateRawVulnerabilityEvidence(
  root: string,
  summary: JsonRecord,
  securityRepair: JsonRecord,
): void {
  const database = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/vulnerability/grype-db-status.json",
  );
  requireExactRecordKeys(database, "/vulnerability/grypeDatabase", [
    "schemaVersion",
    "from",
    "built",
    "path",
    "valid",
  ]);
  const currentScopes = [
    {
      id: "darwin-arm64",
      target: "generator-supply://effective-bundle/darwin-arm64",
      report: "tools/generator-supply/v1/evidence/vulnerability/grype-darwin.json",
      sbom: "tools/generator-supply/v1/evidence/sbom/darwin-bundle.syft.json",
    },
    {
      id: "linux-amd64",
      target: "generator-supply://effective-bundle/linux-amd64",
      report: "tools/generator-supply/v1/evidence/vulnerability/grype-linux.json",
      sbom: "tools/generator-supply/v1/evidence/sbom/linux-bundle.syft.json",
    },
    {
      id: "ubuntu-amd64",
      target: "sha256:a6f81fb630d51837271b89f8193810a5fc493fa4f30a55d7ebcdb3a66f3cc63a",
      report: "tools/generator-supply/v1/evidence/vulnerability/grype-ubuntu.json",
      sbom: "tools/generator-supply/v1/evidence/sbom/ubuntu-image.syft.json",
    },
  ] as const;
  const grypeSummary = recordValue(summary.grype, "/vulnerability/summary/grype");
  const summaryScopes = recordValue(grypeSummary.scopes, "/vulnerability/summary/grype/scopes");
  const totals = { Critical: 0, High: 0, Medium: 0, Low: 0, Negligible: 0 };
  const expectedScopeSummaries: Record<string, JsonRecord> = {};
  for (const scope of currentScopes) {
    const path = scope.report;
    const report = parseJsonFile(root, path);
    validateRawGrypeDocument(report, `/${path}`);
    const matches = recordArray(report.matches, `/${path}/matches`);
    validateGrypeArtifactIdentity(root, path, matches, scope.sbom);
    const scopeTotals = { Critical: 0, High: 0, Medium: 0, Low: 0, Negligible: 0 };
    for (const match of matches) {
      const vulnerability = recordValue(match.vulnerability, `/${path}/vulnerability`);
      const severity = String(vulnerability.severity) as keyof typeof totals;
      if (!(severity in totals)) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/${path}/severity`,
          `Unknown Grype severity ${severity}.`,
        );
      }
      totals[severity] += 1;
      scopeTotals[severity] += 1;
    }
    const descriptor = recordValue(report.descriptor, `/${path}/descriptor`);
    const configuration = recordValue(descriptor.configuration, `/${path}/configuration`);
    if (descriptor.name !== "grype" || descriptor.version !== "0.117.0") {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${path}/descriptor`,
        "Current Grype raw evidence must use exactly 0.117.0.",
      );
    }
    const source = recordValue(report.source, `/${path}/source`);
    const descriptorDatabase = recordValue(
      recordValue(descriptor.db, `/${path}/descriptor/db`).status,
      `/${path}/descriptor/db/status`,
    );
    const scopeSummary = recordValue(summaryScopes[scope.id], `/vulnerability/scopes/${scope.id}`);
    if (
      !grypeSourceMatchesScope(source, `/${path}/source`, scope.id, scope.target) ||
      configuration["show-suppressed"] !== true ||
      JSON.stringify(descriptorDatabase) !== JSON.stringify(database) ||
      !isNonFutureDate(descriptor.timestamp) ||
      recordArray(report.ignoredMatches ?? [], `/${path}/ignoredMatches`).length !== 0 ||
      recordArray(report.suppressedMatches ?? [], `/${path}/suppressedMatches`).length !== 0 ||
      scopeSummary.sbomSha256 !== fileSha256(root, scope.sbom) ||
      scopeSummary.reportSha256 !== fileSha256(root, scope.report) ||
      scopeSummary.matches !== matches.length ||
      canonicalJsonString(
        recordValue(
          scopeSummary.severityCounts,
          `/vulnerability/scopes/${scope.id}/severityCounts`,
        ),
      ) !== canonicalJsonString(scopeTotals) ||
      scopeSummary.ignoredMatches !== 0 ||
      scopeSummary.suppressedMatches !== 0
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${path}/binding`,
        "Grype source, SBOM/report digest, shared database, timestamp, or suppression binding drifted.",
      );
    }
    expectedScopeSummaries[scope.id] = {
      sbomSha256: fileSha256(root, scope.sbom),
      reportSha256: fileSha256(root, scope.report),
      matches: matches.length,
      severityCounts: scopeTotals,
      ignoredMatches: 0,
      suppressedMatches: 0,
    };
  }
  if (
    totals.Critical !== 0 ||
    totals.High !== 0 ||
    summary.critical !== totals.Critical ||
    summary.high !== totals.High ||
    summary.medium !== totals.Medium ||
    summary.low !== totals.Low ||
    summary.negligible !== totals.Negligible
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/raw",
      `Current raw Grype severity totals are not fail-closed clean: ${JSON.stringify(totals)}.`,
    );
  }

  const expectedRejected = [
    "CVE-2026-21710",
    "CVE-2026-48617",
    "CVE-2026-48937",
    "CVE-2026-56846",
    "CVE-2026-56848",
    "CVE-2026-58043",
  ];
  const expectedFixes = new Map([
    ["CVE-2026-21710", "24.14.1"],
    ["CVE-2026-48617", "24.17.0"],
    ["CVE-2026-48937", "24.17.0"],
    ["CVE-2026-56846", "24.18.1"],
    ["CVE-2026-56848", "24.18.1"],
    ["CVE-2026-58043", "24.18.1"],
  ]);
  for (const platform of ["darwin", "linux"]) {
    const path = `tools/generator-supply/v1/evidence/vulnerability/grype-node-24.13.1-rejected-${platform}.json`;
    const sbomPath = `tools/generator-supply/v1/evidence/sbom/node-24.13.1-rejected-${platform}.syft.json`;
    const report = parseJsonFile(root, path);
    const rejectedSbom = parseJsonFile(root, sbomPath);
    validateRawGrypeDocument(report, `/${path}`);
    const rejectedNodeVersions = recordArray(rejectedSbom.artifacts, `/${sbomPath}/artifacts`)
      .filter((artifact) => artifact.name === "node")
      .map((artifact) => artifact.version);
    const rejectedDescriptor = recordValue(report.descriptor, `/${path}/descriptor`);
    const rejectedConfiguration = recordValue(
      rejectedDescriptor.configuration,
      `/${path}/configuration`,
    );
    const rejectedDatabase = recordValue(
      recordValue(rejectedDescriptor.db, `/${path}/db`).status,
      `/${path}/db/status`,
    );
    const rejectedMatches = recordArray(report.matches, `/${path}/matches`);
    validateGrypeArtifactIdentity(root, path, rejectedMatches, sbomPath);
    const rejectedEvidence = recordValue(
      recordValue(securityRepair.rejectedEvidence, "/securityRepair/rejectedEvidence")[
        `${platform}-arm64`
      ] ??
        recordValue(securityRepair.rejectedEvidence, "/securityRepair/rejectedEvidence")[
          `${platform}-amd64`
        ],
      `/securityRepair/rejectedEvidence/${platform}`,
    );
    const highRecords = rejectedMatches
      .map((match) => recordValue(match.vulnerability, `/${path}/vulnerability`))
      .filter((vulnerability) => vulnerability.severity === "High");
    const high = highRecords
      .map((vulnerability) => String(vulnerability.id))
      .toSorted(bytewiseCompare);
    for (const vulnerability of highRecords) {
      const id = String(vulnerability.id);
      const fix = recordValue(vulnerability.fix, `/${path}/${id}/fix`);
      if (
        !stringArray(fix.versions, `/${path}/${id}/fix/versions`).includes(
          expectedFixes.get(id) ?? "",
        )
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/${path}/${id}/fix`,
          `Rejected finding ${id} does not bind its exact Node 24 fixed version.`,
        );
      }
    }
    if (JSON.stringify(high) !== JSON.stringify(expectedRejected)) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${path}/high`,
        `Rejected historical evidence must bind the fixed six-High set, received ${JSON.stringify(high)}.`,
      );
    }
    if (
      JSON.stringify(rejectedNodeVersions) !== JSON.stringify(["24.13.1"]) ||
      rejectedConfiguration["show-suppressed"] !== true ||
      recordArray(report.ignoredMatches ?? [], `/${path}/ignoredMatches`).length !== 0 ||
      JSON.stringify(rejectedDatabase) !== JSON.stringify(database) ||
      !isNonFutureDate(rejectedDescriptor.timestamp) ||
      rejectedEvidence.sbomSha256 !== fileSha256(root, sbomPath) ||
      rejectedEvidence.grypeReportSha256 !== fileSha256(root, path)
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${path}/rejectedBinding`,
        "Rejected Node evidence must bind exact SBOM/report bytes, shared DB, and visible suppressions.",
      );
    }
  }
  if (
    JSON.stringify(securityRepair.rejectedHighVulnerabilities) !==
      JSON.stringify(expectedRejected) ||
    JSON.stringify(securityRepair.findings) !==
      JSON.stringify(
        [...expectedFixes].map(([id, firstFixedNode24]) => ({ id, firstFixedNode24 })),
      ) ||
    JSON.stringify(securityRepair.baselineRepository) !==
      JSON.stringify({
        commit: "5599f9d20e761532e08906eab1fc8384d48e5b8e",
        tree: "3a9c5274bf9779b50720c20f39b61fe29228b84c",
      }) ||
    JSON.stringify(securityRepair.rejectedArtifacts) !==
      JSON.stringify({
        "darwin-arm64": {
          filename: "node-v24.13.1-darwin-arm64.tar.xz",
          sha256: "d82a321541d65109c696505135be3b7dd46e3358f0f04d664f50f0d1e1ccb8a6",
        },
        "linux-amd64": {
          filename: "node-v24.13.1-linux-x64.tar.xz",
          sha256: "30215f90ea3cd04dfbc06e762c021393fa173a1d392974298bbc871a8e461089",
        },
      }) ||
    JSON.stringify(securityRepair.currentArtifacts) !==
      JSON.stringify({
        "darwin-arm64": {
          filename: "node-v24.18.1-darwin-arm64.tar.xz",
          sha256: "1d60b703fe5d7e7072489be8187f430f1a095a658c31e5e1e281331a5873fac3",
        },
        "linux-amd64": {
          filename: "node-v24.18.1-linux-x64.tar.xz",
          sha256: "d6c664df3f3f61458e8c277585571328522d705166723a7c7823a9253a4d15a0",
        },
      })
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/securityRepair/rejectedHighVulnerabilities",
      "Security repair summary does not bind the rejected six-High set.",
    );
  }

  const osv = parseJsonFile(root, "tools/generator-supply/v1/evidence/vulnerability/osv.json");
  validateRawOsvDocument(osv);
  const results = recordArray(osv.results, "/vulnerability/osv/results");
  const expectedOsvSources = new Map<string, readonly [string, number]>([
    ["generator-supply://isolated-scan-input/go/go.mod", ["go.mod", 2]],
    ["generator-supply://isolated-scan-input/npm/package-lock.json", ["package-lock.json", 34]],
    ["generator-supply://isolated-scan-input/python/uv.lock", ["uv.lock", 21]],
  ] as const);
  const packageCounts = new Map<string, number>();
  const packageIdentities = new Set<string>();
  let osvPackages = 0;
  let osvVulnerabilities = 0;
  for (const result of results) {
    const source = recordValue(result.source, "/vulnerability/osv/source");
    const packages = recordArray(result.packages, "/vulnerability/osv/packages");
    const expectedSource = expectedOsvSources.get(String(source.path));
    if (
      expectedSource === undefined ||
      source.type !== "lockfile" ||
      packages.length !== expectedSource[1]
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        "/vulnerability/osv/source",
        "OSV raw evidence must contain the exact Go, npm, and Python source identities.",
      );
    }
    packageCounts.set(String(source.path).split("/").at(-1) ?? "", packages.length);
    for (const entry of packages) {
      const packageValue = recordValue(entry.package, "/vulnerability/osv/package");
      packageIdentities.add(
        `${String(packageValue.ecosystem)}:${String(packageValue.name)}@${String(packageValue.version)}`,
      );
      osvPackages += 1;
      osvVulnerabilities += recordArray(
        entry.vulnerabilities ?? [],
        "/vulnerability/osv/vulnerabilities",
      ).length;
    }
  }
  if (
    results.length !== 3 ||
    packageCounts.get("package-lock.json") !== 34 ||
    packageCounts.get("uv.lock") !== 21 ||
    packageCounts.get("go.mod") !== 2 ||
    osvPackages !== 57 ||
    packageIdentities.size !== 57 ||
    osvVulnerabilities !== 0
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/osv",
      "OSV raw evidence must parse npm 34, Python 21, Go 2, total 57 and report zero vulnerabilities.",
    );
  }
  requireEvidence(
    recordValue(summary.osv, "/vulnerability/summary/osv"),
    "/vulnerability/summary/osv",
    {
      version: "2.5.1",
      sources: 3,
      npmPackages: 34,
      pythonPackages: 21,
      goPackages: 2,
      packages: 57,
      vulnerabilities: 0,
    },
  );
  validateOsvPackageProjection(root, results);
  const receipt = parseJsonFile(
    root,
    "tools/generator-supply/v1/evidence/vulnerability/osv-scanner-receipt.json",
  );
  requireExactRecordKeys(receipt, "/vulnerability/osvScannerReceipt", [
    "formatVersion",
    "scanner",
    "version",
    "scalibrVersion",
    "commit",
    "builtAt",
    "versionCommandOutput",
    "executableSha256",
    "reportSha256",
    "sources",
    "sourceReceipts",
  ]);
  requireEvidence(receipt, "/vulnerability/osvScannerReceipt", {
    formatVersion: "cloud-agents-generator-supply-osv-scanner-receipt/v1",
    scanner: "osv-scanner",
    version: "2.5.1",
    scalibrVersion: "0.5.2",
    commit: "c84fa4568f2526d0333e9a914ea8a0a5f74ad68b",
    builtAt: "2026-08-17T03:44:26Z",
    versionCommandOutput:
      "osv-scanner version: 2.5.1\nosv-scalibr version: 0.5.2\ncommit: c84fa4568f2526d0333e9a914ea8a0a5f74ad68b\nbuilt at: 2026-08-17T03:44:26Z",
    executableSha256: "75c44d6332f892a1e56286f4105a98ed751ae28d215ca0a8b65cc00d84103054",
    reportSha256: fileSha256(root, "tools/generator-supply/v1/evidence/vulnerability/osv.json"),
    sources: 3,
  });
  const receiptSources = recordArray(receipt.sourceReceipts, "/vulnerability/osv/sourceReceipts");
  for (const [index, source] of receiptSources.entries()) {
    requireExactRecordKeys(source, `/vulnerability/osv/sourceReceipts/${index}`, [
      "repositoryPath",
      "rawSource",
      "sha256",
    ]);
  }
  const expectedReceiptSources = [
    {
      repositoryPath: "tools/generator-supply/go/go.mod",
      rawSource: "generator-supply://isolated-scan-input/go/go.mod",
    },
    {
      repositoryPath: "tools/generator-supply/npm/package-lock.json",
      rawSource: "generator-supply://isolated-scan-input/npm/package-lock.json",
    },
    {
      repositoryPath: "tools/contract-standards/uv.lock",
      rawSource: "generator-supply://isolated-scan-input/python/uv.lock",
    },
  ].map((entry) => ({
    ...entry,
    sha256: fileSha256(root, entry.repositoryPath),
  }));
  if (
    !isNonFutureDate(receipt.builtAt) ||
    canonicalJsonString({ values: receiptSources }) !==
      canonicalJsonString({ values: expectedReceiptSources })
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/osvScannerReceipt",
      "OSV receipt must bind exact scanner version bytes, raw report bytes, and all three current source bytes.",
    );
  }

  if (
    database.valid !== true ||
    database.schemaVersion !== "v6.1.9" ||
    grypeSummary.databaseSchema !== database.schemaVersion ||
    grypeSummary.databaseBuilt !== database.built ||
    grypeSummary.databaseSha256 !==
      "ba668ff9b18de6af2db1df8192520c8cc7744b92397f1b72af64489a5a239d6d" ||
    !isNonFutureDate(database.built)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/grypeDatabase",
      "Grype database must be valid, exact, and not future-dated.",
    );
  }

  const expectedVulnerabilitySummary = {
    formatVersion: "cloud-agents-generator-supply-vulnerability-summary/v1",
    status: "CURRENT_SCAN_NO_UNEXPLAINED_HIGH_CRITICAL",
    critical: totals.Critical,
    high: totals.High,
    medium: totals.Medium,
    low: totals.Low,
    negligible: totals.Negligible,
    parseFailures: 0,
    timelessCleanClaim: false,
    grype: {
      version: "0.117.0",
      databaseSchema: database.schemaVersion,
      databaseBuilt: database.built,
      databaseSha256: "ba668ff9b18de6af2db1df8192520c8cc7744b92397f1b72af64489a5a239d6d",
      darwinMatches: expectedScopeSummaries["darwin-arm64"]!.matches,
      linuxMatches: expectedScopeSummaries["linux-amd64"]!.matches,
      ubuntuMatches: expectedScopeSummaries["ubuntu-amd64"]!.matches,
      scopes: expectedScopeSummaries,
    },
    osv: {
      version: "2.5.1",
      sources: 3,
      npmPackages: 34,
      pythonPackages: 21,
      goPackages: 2,
      packages: 57,
      vulnerabilities: 0,
    },
    historicalRejected: {
      runtime: "Node 24.13.1",
      darwinHigh: 6,
      linuxHigh: 6,
      waiver: "NOT_PRODUCED_NOT_AUTHORIZED",
    },
  };
  if (canonicalJsonString(summary) !== canonicalJsonString(expectedVulnerabilitySummary)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/summary",
      "Vulnerability summary must be the complete canonical projection of raw Grype, OSV, and historical evidence.",
    );
  }
}

function grypeSourceMatchesScope(
  source: JsonRecord,
  path: string,
  scopeId: "darwin-arm64" | "linux-amd64" | "ubuntu-amd64",
  expectedTarget: string,
): boolean {
  if (scopeId !== "ubuntu-amd64") {
    return source.type === "directory" && source.target === expectedTarget;
  }

  const target = recordValue(source.target, `${path}/target`);
  requireExactRecordKeys(target, `${path}/target`, [
    "userInput",
    "imageID",
    "manifestDigest",
    "mediaType",
    "tags",
    "imageSize",
    "layers",
    "manifest",
    "config",
    "repoDigests",
    "architecture",
    "os",
    "labels",
  ]);
  const layers = recordArray(target.layers, `${path}/target/layers`);
  if (layers.length !== 1) return false;
  const layer = layers[0]!;
  requireExactRecordKeys(layer, `${path}/target/layers/0`, ["mediaType", "digest", "size"]);
  const labels = recordValue(target.labels, `${path}/target/labels`);
  requireExactRecordKeys(labels, `${path}/target/labels`, ["org.opencontainers.image.version"]);
  const tags = stringArray(target.tags, `${path}/target/tags`);
  const repoDigests = stringArray(target.repoDigests, `${path}/target/repoDigests`);
  return (
    source.type === "image" &&
    target.userInput === expectedTarget &&
    target.imageID === UBUNTU_IMAGE_IDENTITY.configImageId &&
    target.manifestDigest === UBUNTU_IMAGE_IDENTITY.platformManifestDigest &&
    target.mediaType === "application/vnd.docker.distribution.manifest.v2+json" &&
    typeof target.imageSize === "number" &&
    target.imageSize === layer.size &&
    typeof target.manifest === "string" &&
    target.manifest !== "" &&
    typeof target.config === "string" &&
    target.config !== "" &&
    tags.some((tag) => tag.endsWith("ubuntu:24.04")) &&
    repoDigests.some((digest) => digest.endsWith(UBUNTU_IMAGE_IDENTITY.registryIndexDigest)) &&
    target.architecture === "amd64" &&
    target.os === "linux" &&
    labels["org.opencontainers.image.version"] === "24.04" &&
    layer.mediaType === "application/vnd.docker.image.rootfs.diff.tar.gzip" &&
    layer.digest === UBUNTU_IMAGE_IDENTITY.rootfsLayerDigest &&
    typeof layer.size === "number" &&
    layer.size > 0
  );
}

function validateRawGrypeDocument(document: JsonRecord, path: string): void {
  requireExactRecordKeys(document, path, ["matches", "source", "distro", "descriptor"]);
  const source = recordValue(document.source, `${path}/source`);
  requireExactRecordKeys(source, `${path}/source`, ["type", "target"]);
  const distro = recordValue(document.distro, `${path}/distro`);
  requireExactRecordKeys(distro, `${path}/distro`, ["name", "version", "idLike"]);
  const descriptor = recordValue(document.descriptor, `${path}/descriptor`);
  requireExactRecordKeys(descriptor, `${path}/descriptor`, [
    "name",
    "version",
    "configuration",
    "db",
    "timestamp",
  ]);
  const configuration = recordValue(descriptor.configuration, `${path}/descriptor/configuration`);
  requireExactRecordKeys(configuration, `${path}/descriptor/configuration`, [
    "output",
    "file",
    "pretty",
    "distro",
    "add-cpes-if-none",
    "output-template-file",
    "check-for-app-update",
    "only-fixed",
    "only-notfixed",
    "ignore-wontfix",
    "platform",
    "search",
    "ignore",
    "exclude",
    "externalSources",
    "match",
    "fail-on-severity",
    "registry",
    "show-suppressed",
    "by-cve",
    "SortBy",
    "name",
    "default-image-pull-source",
    "from",
    "vex-documents",
    "vex-add",
    "match-upstream-kernel-headers",
    "fix-channel",
    "timestamp",
    "alerts",
    "db",
    "exp",
    "dev",
  ]);
  const database = recordValue(descriptor.db, `${path}/descriptor/db`);
  requireExactRecordKeys(database, `${path}/descriptor/db`, ["status", "providers"]);
  const databaseStatus = recordValue(database.status, `${path}/descriptor/db/status`);
  requireExactRecordKeys(databaseStatus, `${path}/descriptor/db/status`, [
    "schemaVersion",
    "from",
    "built",
    "path",
    "valid",
  ]);
  for (const [index, match] of recordArray(document.matches, `${path}/matches`).entries()) {
    const matchPath = `${path}/matches/${index}`;
    requireExactRecordKeys(match, matchPath, [
      "vulnerability",
      "relatedVulnerabilities",
      "matchDetails",
      "artifact",
    ]);
    const vulnerability = recordValue(match.vulnerability, `${matchPath}/vulnerability`);
    const vulnerabilityKeys = Object.hasOwn(vulnerability, "description")
      ? [
          "id",
          "dataSource",
          "namespace",
          "severity",
          "urls",
          "description",
          "cvss",
          "epss",
          "cwes",
          "fix",
          "advisories",
          "risk",
        ]
      : Object.hasOwn(vulnerability, "epss") || Object.hasOwn(vulnerability, "cwes")
        ? [
            "id",
            "dataSource",
            "namespace",
            "severity",
            "urls",
            "cvss",
            "epss",
            "cwes",
            "fix",
            "advisories",
            "risk",
          ]
        : [
            "id",
            "dataSource",
            "namespace",
            "severity",
            "urls",
            "cvss",
            "fix",
            "advisories",
            "risk",
          ];
    requireExactRecordKeys(vulnerability, `${matchPath}/vulnerability`, vulnerabilityKeys);
    for (const [relatedIndex, related] of recordArray(
      match.relatedVulnerabilities,
      `${matchPath}/relatedVulnerabilities`,
    ).entries()) {
      const relatedKeys = Object.hasOwn(related, "description")
        ? [
            "id",
            "dataSource",
            "namespace",
            "severity",
            "urls",
            "description",
            "cvss",
            "epss",
            "cwes",
          ]
        : ["id", "dataSource", "namespace", "severity", "urls", "cvss"];
      requireExactRecordKeys(
        related,
        `${matchPath}/relatedVulnerabilities/${relatedIndex}`,
        relatedKeys,
      );
    }
    for (const [detailIndex, detail] of recordArray(
      match.matchDetails,
      `${matchPath}/matchDetails`,
    ).entries()) {
      const detailKeys = Object.hasOwn(detail, "fix")
        ? ["type", "matcher", "searchedBy", "found", "fix"]
        : ["type", "matcher", "searchedBy", "found"];
      requireExactRecordKeys(detail, `${matchPath}/matchDetails/${detailIndex}`, detailKeys);
    }
  }
}

function validateGrypeArtifactIdentity(
  root: string,
  reportPath: string,
  matches: readonly JsonRecord[],
  sbomPath: string,
): void {
  const sbom = parseJsonFile(root, sbomPath);
  const artifacts = recordArray(sbom.artifacts, `/${sbomPath}/artifacts`);
  const byPurl = new Map<string, JsonRecord[]>();
  for (const artifact of artifacts) {
    const purl = artifact.purl;
    if (typeof purl !== "string" || purl === "") continue;
    const entries = byPurl.get(purl) ?? [];
    entries.push(artifact);
    byPurl.set(purl, entries);
  }
  for (const [index, match] of matches.entries()) {
    const artifact = recordValue(match.artifact, `/${reportPath}/matches/${index}/artifact`);
    requireExactRecordKeys(artifact, `/${reportPath}/matches/${index}/artifact`, [
      "id",
      "name",
      "version",
      "type",
      "locations",
      "language",
      "licenses",
      "cpes",
      "purl",
      "upstreams",
    ]);
    const purl = artifact.purl;
    const candidates = typeof purl === "string" ? (byPurl.get(purl) ?? []) : [];
    if (
      candidates.length === 0 ||
      !candidates.some(
        (candidate) =>
          candidate.name === artifact.name &&
          candidate.version === artifact.version &&
          candidate.type === artifact.type,
      )
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${reportPath}/matches/${index}/artifact`,
        "Grype artifact identity must project to a current Syft artifact with the same PURL, name, version, and type.",
      );
    }
  }
}

function validateRawOsvDocument(document: JsonRecord): void {
  requireExactRecordKeys(document, "/vulnerability/osv", ["results", "experimental_config"]);
  const configuration = recordValue(
    document.experimental_config,
    "/vulnerability/osv/experimental_config",
  );
  requireExactRecordKeys(configuration, "/vulnerability/osv/experimental_config", ["licenses"]);
  const licenses = recordValue(
    configuration.licenses,
    "/vulnerability/osv/experimental_config/licenses",
  );
  requireExactRecordKeys(licenses, "/vulnerability/osv/experimental_config/licenses", [
    "summary",
    "allowlist",
  ]);
  if (licenses.summary !== false || licenses.allowlist !== null) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/osv/experimental_config/licenses",
      "OSV scanner metadata must bind the exact disabled license-summary configuration.",
    );
  }
  for (const [index, result] of recordArray(
    document.results,
    "/vulnerability/osv/results",
  ).entries()) {
    const path = `/vulnerability/osv/results/${index}`;
    requireExactRecordKeys(result, path, ["source", "packages"]);
    const source = recordValue(result.source, `${path}/source`);
    requireExactRecordKeys(source, `${path}/source`, ["path", "type"]);
    for (const [packageIndex, entry] of recordArray(
      result.packages,
      `${path}/packages`,
    ).entries()) {
      const packagePath = `${path}/packages/${packageIndex}`;
      // Clean OSV output has only the package object.  A vulnerability array
      // or any other extra member must not be silently ignored; the exact
      // projection below will also reject it after package identity binding.
      const expectedKeys = Object.hasOwn(entry, "dependency_groups")
        ? ["package", "dependency_groups"]
        : ["package"];
      requireExactRecordKeys(entry, packagePath, expectedKeys);
      if (Object.hasOwn(entry, "dependency_groups")) {
        if (
          JSON.stringify(
            stringArray(entry.dependency_groups, `${packagePath}/dependency_groups`),
          ) !== JSON.stringify(["optional"])
        ) {
          throw supplyError(
            "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
            `${packagePath}/dependency_groups`,
            "OSV dependency groups must bind the exact optional closure.",
          );
        }
      }
      const packageValue = recordValue(entry.package, `${packagePath}/package`);
      requireExactRecordKeys(packageValue, `${packagePath}/package`, [
        "name",
        "version",
        "ecosystem",
      ]);
    }
  }
}

function validateOsvPackageProjection(root: string, results: readonly JsonRecord[]): void {
  const lock = parseJsonFile(root, "tools/generator-supply/npm/package-lock.json");
  const lockPackages = recordValue(lock.packages, "/vulnerability/osv/npm/packageLock");
  const npmExpected = Object.entries(lockPackages)
    .filter(([path]) => path !== "")
    .map(([path, value]) => {
      const packageValue = recordValue(value, `/vulnerability/osv/npm/${path}`);
      const name = path.slice("node_modules/".length);
      return {
        package: {
          name,
          version: String(packageValue.version),
          ecosystem: "npm",
        },
        ...(packageValue.optional === true ? { dependency_groups: ["optional"] } : {}),
      };
    });
  const goText = readFileSync(
    resolveContainedRegularFile(root, "tools/generator-supply/go/go.mod"),
    "utf8",
  );
  const goExpected = [...goText.matchAll(/^\s*([^\s]+)\s+v([^\s]+)\s*$/gmu)].map((match) => ({
    package: { name: match[1]!, version: match[2]!, ecosystem: "Go" },
  }));
  const uvText = readFileSync(
    resolveContainedRegularFile(root, "tools/contract-standards/uv.lock"),
    "utf8",
  );
  const uvExpected: JsonRecord[] = [];
  let uvName: string | undefined;
  let uvVersion: string | undefined;
  let uvSourceKind: "registry" | "virtual" | undefined;
  let uvVirtualRoots = 0;
  const flushUv = (): void => {
    if (uvName === undefined && uvVersion === undefined && uvSourceKind === undefined) return;
    if (uvName === undefined || uvVersion === undefined || uvSourceKind === undefined) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        "/vulnerability/osv/sourceProjection/python/uv.lock",
        "Every uv package block must have an exact name, version, and source.",
      );
    }
    if (uvSourceKind === "registry") {
      uvExpected.push({
        package: { name: uvName, version: uvVersion, ecosystem: "PyPI" },
      });
    } else if (uvName === "cloud-agents-contract-standards" && uvVersion === "0.0.0") {
      uvVirtualRoots += 1;
    } else {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        "/vulnerability/osv/sourceProjection/python/uv.lock",
        "Only the exact contract-standards virtual root may be omitted from the PyPI scan closure.",
      );
    }
    uvName = undefined;
    uvVersion = undefined;
    uvSourceKind = undefined;
  };
  for (const line of uvText.split("\n")) {
    if (line === "[[package]]") {
      flushUv();
      continue;
    }
    const nameMatch = /^name = "([^"]+)"$/u.exec(line);
    const versionMatch = /^version = "([^"]+)"$/u.exec(line);
    if (nameMatch) uvName = nameMatch[1];
    if (versionMatch) uvVersion = versionMatch[1];
    if (line === 'source = { registry = "https://pypi.org/simple" }') uvSourceKind = "registry";
    if (line === 'source = { virtual = "." }') uvSourceKind = "virtual";
  }
  flushUv();
  if (
    goExpected.length !== 2 ||
    uvExpected.length !== 21 ||
    uvVirtualRoots !== 1 ||
    npmExpected.length !== 34
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/vulnerability/osv/sourceProjection",
      "OSV source projections must derive the exact current Go, npm, and Python lock closures.",
    );
  }
  const expectedBySource = new Map<string, readonly JsonRecord[]>([
    ["generator-supply://isolated-scan-input/go/go.mod", goExpected],
    ["generator-supply://isolated-scan-input/npm/package-lock.json", npmExpected],
    ["generator-supply://isolated-scan-input/python/uv.lock", uvExpected],
  ]);
  for (const result of results) {
    requireExactRecordKeys(result, "/vulnerability/osv/result", ["source", "packages"]);
    const source = recordValue(result.source, "/vulnerability/osv/source");
    requireExactRecordKeys(source, "/vulnerability/osv/source", ["path", "type"]);
    const sourcePath = String(source.path);
    const expected = expectedBySource.get(sourcePath);
    if (expected === undefined) continue;
    const packages = recordArray(result.packages, `/vulnerability/osv/${sourcePath}/packages`);
    const actual = packages.map((entry, index) => {
      const packageValue = recordValue(
        entry.package,
        `/vulnerability/osv/${sourcePath}/${index}/package`,
      );
      const expectedEntry = expected[index];
      const hasGroups = Object.hasOwn(entry, "dependency_groups");
      if (expectedEntry === undefined) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/vulnerability/osv/${sourcePath}/packages`,
          "OSV package output contains an undeclared lockfile package.",
        );
      }
      const expectedPackage = recordValue(
        expectedEntry.package,
        "/vulnerability/osv/expected/package",
      );
      const expectedGroups = expectedEntry.dependency_groups;
      if (
        canonicalJsonString(packageValue) !== canonicalJsonString(expectedPackage) ||
        (expectedGroups === undefined && hasGroups) ||
        (expectedGroups !== undefined &&
          canonicalJsonString({ values: entry.dependency_groups }) !==
            canonicalJsonString({ values: expectedGroups }))
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/vulnerability/osv/${sourcePath}/${index}`,
          "OSV package identity or optional dependency group drifted from the current lockfile.",
        );
      }
      requireExactRecordKeys(
        entry,
        `/vulnerability/osv/${sourcePath}/${index}`,
        expectedGroups === undefined ? ["package"] : ["package", "dependency_groups"],
      );
      return expectedEntry;
    });
    if (
      actual.length !== expected.length ||
      canonicalJsonString({ values: actual }) !== canonicalJsonString({ values: expected })
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/vulnerability/osv/${sourcePath}/packages`,
        "OSV package projection must equal the complete current lockfile closure.",
      );
    }
  }
}

function validateEvidenceClosure(
  root: string,
  declaredPaths: readonly string[],
  overrides?: JsonOverrides,
): void {
  for (const path of overrides?.keys() ?? []) {
    if (path !== REPLAY_SUMMARY_PATH || !declaredPaths.includes(path)) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/profile/evidence/${path}`,
        "In-memory evidence overrides are restricted to the derived replay summary path.",
      );
    }
  }
  const actualPaths: string[] = [];
  const visit = (relativeDirectory: string): void => {
    const absoluteDirectory = resolve(root, relativeDirectory);
    for (const name of readdirSync(absoluteDirectory).toSorted(bytewiseCompare)) {
      const path = `${relativeDirectory}/${name}`;
      const metadata = lstatSync(resolve(root, path));
      if (metadata.isDirectory()) {
        visit(path);
      } else if (metadata.isFile() && !metadata.isSymbolicLink()) {
        actualPaths.push(path);
      } else {
        throw supplyError(
          "GENERATOR_SUPPLY_BINDING_MISMATCH",
          `/${path}`,
          "Generator supply evidence closure permits only regular non-symlink files.",
        );
      }
    }
  };
  visit(EVIDENCE_ROOT);
  for (const path of overrides?.keys() ?? []) {
    if (!actualPaths.includes(path)) actualPaths.push(path);
  }
  const declared = [...declaredPaths].toSorted(bytewiseCompare);
  const actual = actualPaths.toSorted(bytewiseCompare);
  const semantic = [...SEMANTIC_EVIDENCE_PATHS].toSorted(bytewiseCompare);
  if (JSON.stringify(declared) !== JSON.stringify(actual)) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/evidence",
      `Declared evidence must exactly equal directory bytes; declared-only=${JSON.stringify(declared.filter((path) => !actual.includes(path)))} actual-only=${JSON.stringify(actual.filter((path) => !declared.includes(path)))}.`,
    );
  }
  if (JSON.stringify(declared) !== JSON.stringify(semantic)) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile/evidence",
      `Declared evidence must exactly equal the semantic-reader closure; unread=${JSON.stringify(declared.filter((path) => !semantic.includes(path)))} missing=${JSON.stringify(semantic.filter((path) => !declared.includes(path)))}.`,
    );
  }
}

function validateNoticeEvidence(root: string, summary: JsonRecord): void {
  const noticePath = `${EVIDENCE_ROOT}/THIRD_PARTY_NOTICES.md`;
  const noticeBytes = readFileSync(resolveContainedRegularFile(root, noticePath));
  const noticeText = noticeBytes.toString("utf8");
  const inventoryRows = noticeText
    .split("\n")
    .filter(
      (line) => line.startsWith("| ") && !line.startsWith("| Scope |") && !line.startsWith("| ---"),
    );
  const expectedRows: string[] = [];
  for (const [scope, sbomName] of [
    ["darwin-arm64", "darwin-bundle"],
    ["linux-amd64", "linux-bundle"],
    ["ubuntu-amd64-executor", "ubuntu-image"],
  ] as const) {
    const raw = parseJsonFile(root, `${EVIDENCE_ROOT}/sbom/${sbomName}.syft.json`);
    for (const artifact of recordArray(raw.artifacts, `/notice/${scope}/artifacts`)) {
      const licenses = recordArray(artifact.licenses ?? [], `/notice/${scope}/licenses`)
        .map((license) => {
          if (typeof license.value === "string" && license.value !== "") return license.value;
          return typeof license.spdxExpression === "string" ? license.spdxExpression : "";
        })
        .filter((license) => license !== "")
        .toSorted(bytewiseCompare);
      expectedRows.push(
        noticeRow([
          scope,
          String(artifact.type),
          String(artifact.name),
          String(artifact.version),
          licenses.length === 0 ? "NOASSERTION" : licenses.join(" AND "),
          String(artifact.purl ?? "NOASSERTION"),
        ]),
      );
    }
  }
  const counts = (rows: readonly string[]): Map<string, number> => {
    const result = new Map<string, number>();
    for (const row of rows) result.set(row, (result.get(row) ?? 0) + 1);
    return result;
  };
  const actualCounts = counts(inventoryRows);
  const expectedCounts = counts(expectedRows);
  if (
    actualCounts.size !== expectedCounts.size ||
    [...expectedCounts].some(([row, count]) => actualCounts.get(row) !== count)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/notice/inventory",
      "NOTICE inventory must be the exact multiset derived from all three current raw Syft SBOMs.",
    );
  }
  const inventoryDigest = createHash("sha256")
    .update(`${inventoryRows.join("\n")}\n`, "utf8")
    .digest("hex");
  const textSections = noticeText.match(/^### /gmu)?.length ?? 0;
  if (
    summary.inventoryRecords !== inventoryRows.length ||
    summary.uniqueInventoryRecords !== new Set(inventoryRows).size ||
    summary.inventoryManifestSha256 !== inventoryDigest ||
    summary.includedTextSections !== textSections ||
    summary.noticeSha256 !== createHash("sha256").update(noticeBytes).digest("hex") ||
    summary.noAssertionPreserved !== true ||
    summary.legalConclusion !== "NOT_PRODUCED_NOT_AUTHORIZED"
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/notice/summary",
      "NOTICE summary must bind the exact raw-derived inventory and included text bytes.",
    );
  }
}

function noticeRow(fields: readonly string[]): string {
  return `| ${fields.map((field) => field.replaceAll("|", "\\|")).join(" | ")} |`;
}

function validateProjectionReceipt(root: string, receipt: JsonRecord, report: JsonRecord): void {
  requireExactRecordKeys(receipt, "/replay/projection", [
    "formatVersion",
    "treeSha",
    "archiveSha256",
    "archiveSizeBytes",
    "archiveInspection",
    "excluded",
  ]);
  const archiveInspection = recordValue(
    receipt.archiveInspection,
    "/replay/projection/archiveInspection",
  );
  requireExactRecordKeys(archiveInspection, "/replay/projection/archiveInspection", [
    "formatVersion",
    "profile",
    "manifestAlgorithm",
    "manifestSha256",
    "entries",
    "regularFiles",
    "directories",
    "symlinks",
    "hardlinks",
    "unsafeEntries",
    "duplicateEntries",
    "specialEntries",
    "linkPrefixDescendants",
    "linkCycles",
    "regularFileManifestAlgorithm",
    "regularFileManifestSha256",
    "reconstructedGitTreeSha",
  ]);
  if (
    receipt.formatVersion !== "cloud-agents-core-generator-projection/v1" ||
    !/^[0-9a-f]{40}$/u.test(String(receipt.treeSha)) ||
    !/^sha256:[0-9a-f]{64}$/u.test(String(receipt.archiveSha256)) ||
    !Number.isInteger(receipt.archiveSizeBytes) ||
    Number(receipt.archiveSizeBytes) <= 0 ||
    archiveInspection.formatVersion !== "cloud-agents-generator-replay-archive-inspection/v1" ||
    archiveInspection.profile !== "core-projection" ||
    archiveInspection.manifestAlgorithm !== PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM ||
    !/^[0-9a-f]{64}$/u.test(String(archiveInspection.manifestSha256)) ||
    !Number.isInteger(archiveInspection.entries) ||
    Number(archiveInspection.entries) <= 0 ||
    !Number.isInteger(archiveInspection.regularFiles) ||
    Number(archiveInspection.regularFiles) <= 0 ||
    archiveInspection.symlinks !== 0 ||
    archiveInspection.hardlinks !== 0 ||
    archiveInspection.unsafeEntries !== 0 ||
    archiveInspection.duplicateEntries !== 0 ||
    archiveInspection.specialEntries !== 0 ||
    archiveInspection.linkPrefixDescendants !== 0 ||
    archiveInspection.linkCycles !== 0 ||
    archiveInspection.regularFileManifestAlgorithm !==
      PROJECTION_ARCHIVE_REGULAR_FILE_MANIFEST_ALGORITHM ||
    !/^[0-9a-f]{64}$/u.test(String(archiveInspection.regularFileManifestSha256)) ||
    archiveInspection.reconstructedGitTreeSha !== receipt.treeSha ||
    canonicalJsonString({ values: receipt.excluded }) !==
      canonicalJsonString({ values: [...PROJECTION_EXCLUSIONS] })
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/replay/projection",
      "Projection receipt must be the exact validated archive inspection, reconstructed tree, and exclusion set.",
    );
  }
  if (
    receipt.treeSha !== report.projectionTreeSha ||
    receipt.archiveSha256 !== report.projectionArchiveSha256 ||
    receipt.archiveSizeBytes !== report.projectionArchiveSizeBytes ||
    archiveInspection.manifestAlgorithm !== report.projectionArchiveMemberManifestAlgorithm ||
    `sha256:${String(archiveInspection.manifestSha256)}` !==
      report.projectionArchiveMemberManifestSha256 ||
    archiveInspection.entries !== report.projectionArchiveMembers ||
    archiveInspection.regularFileManifestAlgorithm !== report.inputTreeManifestAlgorithm ||
    `sha256:${String(archiveInspection.regularFileManifestSha256)}` !==
      report.inputTreeManifestSha256 ||
    archiveInspection.regularFiles !== report.inputTreeFiles ||
    report.inputTreeManifestAlgorithm !== INPUT_TREE_MANIFEST_ALGORITHM ||
    !/^sha256:[0-9a-f]{64}$/u.test(String(report.inputTreeManifestSha256)) ||
    !Number.isInteger(report.inputTreeFiles) ||
    Number(report.inputTreeFiles) <= 0
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/replay/projection/binding",
      "Projection receipt fields must exactly equal the common replay report projection.",
    );
  }
  // Keep this call intentional: the receipt path itself is part of the
  // declared evidence closure and must remain a regular repository file.
  fileSha256(root, `${EVIDENCE_ROOT}/replay/projection.json`);
}

function validateRawReplayEvidence(root: string, summary: JsonRecord): void {
  const replayAuthority = recordValue(
    readAndValidateSource(root).profile.replayAuthority,
    "/profile/replayAuthority",
  );
  const runPaths = {
    "darwin-arm64": [
      `${EVIDENCE_ROOT}/replay/darwin-a.json`,
      `${EVIDENCE_ROOT}/replay/darwin-b.json`,
    ],
    "linux-amd64": [`${EVIDENCE_ROOT}/replay/linux-a.json`, `${EVIDENCE_ROOT}/replay/linux-b.json`],
  } as const;
  const reports = Object.fromEntries(
    Object.entries(runPaths).map(([platform, paths]) => [
      platform,
      paths.map((path) => ({ path, report: parseJsonFile(root, path) })),
    ]),
  ) as Record<string, { path: string; report: JsonRecord }[]>;
  const projectionReceiptPath = `${EVIDENCE_ROOT}/replay/projection.json`;
  const projectionReceipt = parseJsonFile(root, projectionReceiptPath);
  const npm = parseJsonFile(root, `${EVIDENCE_ROOT}/npm.json`);
  const installed = recordArray(npm.installed, "/replay/npm/installed");
  const artifacts = parseJsonFile(root, `${EVIDENCE_ROOT}/artifacts.json`);
  const artifactExecutables = recordValue(artifacts.executables, "/replay/artifacts/executables");
  const wheels = parseJsonFile(root, `${EVIDENCE_ROOT}/wheels.json`);
  const wheelPlatforms = recordArray(wheels.platforms, "/replay/wheels/platforms");
  let commonOutputDigest: string | undefined;
  let commonProjection: string | undefined;
  const firstReplayReport = reports["darwin-arm64"]![0]!.report;
  validateProjectionReceipt(root, projectionReceipt, firstReplayReport);
  for (const [platform, runs] of Object.entries(reports)) {
    const expectedNodeModules = recordValue(
      installed.find((entry) => entry.platform === platform)?.nodeModules,
      `/replay/npm/${platform}/nodeModules`,
    );
    if (expectedNodeModules.algorithm !== replayAuthority.nodeModulesManifestAlgorithm) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/replay/npm/${platform}/nodeModules/algorithm`,
        "Collected and replayed node_modules manifests must use the source-bound algorithm.",
      );
    }
    const executableDigest = createHash("sha256");
    for (const executable of recordArray(
      artifactExecutables[platform],
      `/replay/artifacts/executables/${platform}`,
    ).toSorted((left, right) => bytewiseCompare(String(left.id), String(right.id)))) {
      executableDigest
        .update(String(executable.id))
        .update("\0")
        .update(String(executable.sha256))
        .update("\0");
    }
    const expectedExecutableSetSha256 = `sha256:${executableDigest.digest("hex")}`;
    const wheelPlatform = wheelPlatforms.find((entry) => entry.platform === platform);
    const wheelDigest = createHash("sha256");
    for (const wheel of recordArray(wheelPlatform?.wheels, `/replay/wheels/${platform}`)) {
      wheelDigest
        .update(String(wheel.filename))
        .update("\0")
        .update(String(wheel.sizeBytes))
        .update("\0")
        .update(String(wheel.sha256))
        .update("\0");
    }
    const expectedWheelhouseManifestSha256 = `sha256:${wheelDigest.digest("hex")}`;
    for (const [index, { path, report }] of runs.entries()) {
      requireExactRecordKeys(report, `/${path}`, [
        "formatVersion",
        "platform",
        "replayRun",
        "manifestAlgorithm",
        "perCommandTimeoutMilliseconds",
        "archiveHasGitDirectory",
        "projectionTreeSha",
        "projectionArchiveSha256",
        "projectionArchiveSizeBytes",
        "projectionArchiveMemberManifestAlgorithm",
        "projectionArchiveMemberManifestSha256",
        "projectionArchiveMembers",
        "inputTreeManifestAlgorithm",
        "inputTreeManifestSha256",
        "inputTreeFiles",
        "freshExtractionRoot",
        "extractionRootInitiallyAbsent",
        "ambientNodeModules",
        "nodeModulesManifestSha256",
        "nodeModulesFiles",
        "wheelhouseManifestSha256",
        "externalExecutableSetSha256",
        "isolation",
        "isolationEvidenceAuthority",
        "environmentPolicy",
        "runnerEnvironmentPolicy",
        "runnerEnvironmentSanitized",
        "freshPerReplayCaches",
        "ephemeralCachePolicy",
        "homeDirectory",
        "temporaryDirectory",
        "uvCacheDirectory",
        "xdgCacheHome",
        "versions",
        "loadedOxfmtBinding",
        "candidateManifestSha256",
        "replayManifestSha256",
        "outputFiles",
        "candidateOutputsEqual",
        "nonAllowlistedChanges",
        "replayAuthoritySha256",
      ]);
      const versions = recordValue(report.versions, `/replay/${platform}/${index}/versions`);
      requireExactRecordKeys(versions, `/replay/${platform}/${index}/versions`, [
        "node",
        "bun",
        "go",
        "python",
        "uv",
        "protoc",
        "protocGenGo",
        "protocGenConnectGo",
      ]);
      requireEvidence(report, `/replay/${platform}/${index}`, {
        formatVersion: "cloud-agents-generator-replay-run/v1",
        platform,
        replayRun: index === 0 ? "A" : "B",
        manifestAlgorithm: NODE_MODULES_MANIFEST_ALGORITHM,
        perCommandTimeoutMilliseconds: 600_000,
        archiveHasGitDirectory: false,
        projectionArchiveMemberManifestAlgorithm: PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM,
        inputTreeManifestAlgorithm: INPUT_TREE_MANIFEST_ALGORITHM,
        freshExtractionRoot: `generator-supply://core-projection/${index === 0 ? "a" : "b"}`,
        extractionRootInitiallyAbsent: true,
        ambientNodeModules: false,
        nodeModulesManifestSha256: `sha256:${String(expectedNodeModules.sha256)}`,
        nodeModulesFiles: Number(expectedNodeModules.files),
        externalExecutableSetSha256: expectedExecutableSetSha256,
        wheelhouseManifestSha256: expectedWheelhouseManifestSha256,
        isolation:
          platform === "darwin-arm64"
            ? "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES"
            : "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS",
        isolationEvidenceAuthority: "VERSIONED_WRAPPER_SAME_BOUNDARY_RECEIPT",
        environmentPolicy: "MINIMAL_EXACT_V1",
        runnerEnvironmentPolicy: "ENV_I_MINIMAL_V1",
        runnerEnvironmentSanitized: true,
        freshPerReplayCaches: true,
        ephemeralCachePolicy:
          platform === "linux-amd64"
            ? "FRESH_PER_REPLAY_TMPFS_ONLY"
            : "FRESH_PER_REPLAY_SANDBOX_TASK_OWNED",
        homeDirectory: `generator-supply://ephemeral/${index === 0 ? "a" : "b"}/home`,
        temporaryDirectory: `generator-supply://ephemeral/${index === 0 ? "a" : "b"}/tmp`,
        uvCacheDirectory: `generator-supply://ephemeral/${index === 0 ? "a" : "b"}/uv-cache`,
        xdgCacheHome: `generator-supply://ephemeral/${index === 0 ? "a" : "b"}/xdg-cache`,
        loadedOxfmtBinding:
          platform === "darwin-arm64"
            ? "@oxfmt/binding-darwin-arm64"
            : "@oxfmt/binding-linux-x64-gnu",
        candidateOutputsEqual: true,
        nonAllowlistedChanges: 0,
      });
      requireEvidence(versions, `/replay/${platform}/${index}/versions`, {
        node: "24.18.1",
        bun: "1.3.14",
        python: "3.14.7",
        uv: "0.12.5",
        protoc: "35.1",
        protocGenGo: "1.36.12",
        protocGenConnectGo: "1.20.0",
      });
      validateProjectionArchiveMembers(
        report.projectionArchiveMembers,
        `/${path}/projectionArchiveMembers`,
      );
      if (
        !String(versions.go).startsWith("go version go1.26.6 ") ||
        !/^[0-9a-f]{40}$/u.test(String(report.projectionTreeSha)) ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.projectionArchiveSha256)) ||
        !Number.isInteger(report.projectionArchiveSizeBytes) ||
        Number(report.projectionArchiveSizeBytes) <= 0 ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.projectionArchiveMemberManifestSha256)) ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.inputTreeManifestSha256)) ||
        !Number.isInteger(report.inputTreeFiles) ||
        Number(report.inputTreeFiles) <= 0 ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.externalExecutableSetSha256)) ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.wheelhouseManifestSha256)) ||
        report.candidateManifestSha256 !== report.replayManifestSha256 ||
        !/^sha256:[0-9a-f]{64}$/u.test(String(report.replayManifestSha256)) ||
        !Number.isInteger(report.outputFiles) ||
        Number(report.outputFiles) <= 0
      ) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/${path}`,
          "Replay run must bind exact tools, a non-empty output manifest, and byte-identical candidate output.",
        );
      }
      commonOutputDigest ??= String(report.replayManifestSha256);
      if (report.replayManifestSha256 !== commonOutputDigest) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/${path}/replayManifestSha256`,
          "Both fresh archives on both native platforms must produce the same output manifest.",
        );
      }
      const projection = canonicalJsonString({
        projectionTreeSha: report.projectionTreeSha,
        projectionArchiveSha256: report.projectionArchiveSha256,
        projectionArchiveSizeBytes: report.projectionArchiveSizeBytes,
        projectionArchiveMemberManifestAlgorithm: report.projectionArchiveMemberManifestAlgorithm,
        projectionArchiveMemberManifestSha256: report.projectionArchiveMemberManifestSha256,
        projectionArchiveMembers: report.projectionArchiveMembers,
        inputTreeManifestAlgorithm: report.inputTreeManifestAlgorithm,
        inputTreeManifestSha256: report.inputTreeManifestSha256,
        inputTreeFiles: report.inputTreeFiles,
      });
      commonProjection ??= projection;
      if (projection !== commonProjection) {
        throw supplyError(
          "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
          `/${path}/projection`,
          "All four replay reports must bind one identical projection archive and input tree.",
        );
      }
    }
    const stableReport = (report: JsonRecord): JsonRecord => {
      const {
        replayRun: _replayRun,
        homeDirectory: _homeDirectory,
        temporaryDirectory: _temporaryDirectory,
        uvCacheDirectory: _uvCacheDirectory,
        xdgCacheHome: _xdgCacheHome,
        freshExtractionRoot: _freshExtractionRoot,
        ...stable
      } = report;
      return stable;
    };
    if (
      canonicalJsonString(stableReport(runs[0]!.report)) !==
      canonicalJsonString(stableReport(runs[1]!.report))
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/replay/${platform}/sameBits`,
        "Fresh archive replay A and B must produce byte-identical reports.",
      );
    }
  }

  const darwinIsolation = parseJsonFile(root, `${EVIDENCE_ROOT}/replay/darwin-isolation.json`);
  const linuxIsolation = parseJsonFile(root, `${EVIDENCE_ROOT}/replay/linux-isolation.json`);
  validateIsolationReceipt(
    root,
    darwinIsolation,
    "darwin-arm64",
    replayAuthority,
    reports["darwin-arm64"]!,
    commonProjection!,
  );
  validateIsolationReceipt(
    root,
    linuxIsolation,
    "linux-amd64",
    replayAuthority,
    reports["linux-amd64"]!,
    commonProjection!,
  );
  for (const [platform, isolation] of [
    ["darwin-arm64", darwinIsolation],
    ["linux-amd64", linuxIsolation],
  ] as const) {
    const runDigests = recordValue(
      isolation.runReportSha256,
      `/replay/${platform}/runReportSha256`,
    );
    const platformRuns = reports[platform]!;
    const expectedDigests = {
      a: `sha256:${fileSha256(root, platformRuns[0]!.path)}`,
      b: `sha256:${fileSha256(root, platformRuns[1]!.path)}`,
    };
    const receiptProjection = canonicalJsonString({
      projectionTreeSha: isolation.projectionTreeSha,
      projectionArchiveSha256: isolation.projectionArchiveSha256,
      projectionArchiveSizeBytes: isolation.projectionArchiveSizeBytes,
      projectionArchiveMemberManifestAlgorithm: isolation.projectionArchiveMemberManifestAlgorithm,
      projectionArchiveMemberManifestSha256: isolation.projectionArchiveMemberManifestSha256,
      projectionArchiveMembers: isolation.projectionArchiveMembers,
      inputTreeManifestAlgorithm: isolation.inputTreeManifestAlgorithm,
      inputTreeManifestSha256: isolation.inputTreeManifestSha256,
      inputTreeFiles: isolation.inputTreeFiles,
    });
    if (
      canonicalJsonString(runDigests) !== canonicalJsonString(expectedDigests) ||
      receiptProjection !== commonProjection
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/replay/${platform}/isolationBinding`,
        "Same-boundary isolation receipt must bind exact A/B report bytes and their shared projection.",
      );
    }
  }

  const rejected = parseJsonFile(root, `${EVIDENCE_ROOT}/replay/rejected-executor.json`);
  requireExactRecordKeys(rejected, "/replay/rejectedExecutor", [
    "formatVersion",
    "executor",
    "status",
    "failureStage",
    "failureSignal",
    "countedAsReplay",
    "replacementExecutor",
    "correctiveEvidence",
    "cpuIncompatibility",
  ]);
  requireEvidence(rejected, "/replay/rejectedExecutor", {
    formatVersion: "cloud-agents-generator-replay-rejected-executor/v1",
    executor: "initial_linux_executor_configuration",
    status: "REJECTED_EMPTY_DEV_ISOLATION_MISCONFIGURATION",
    failureStage: "BUN_EXECUTABLE_START",
    failureSignal: "SIGILL",
    countedAsReplay: false,
    replacementExecutor: "authorized_linux_executor_with_tmpfs_device_tree",
    correctiveEvidence: "SAME_BOUND_BUN_AND_NODE_PASS_AFTER_TMPFS_DEVICE_TREE",
    cpuIncompatibility: "NOT_CLAIMED",
  });

  const expectedRunDigests = Object.fromEntries(
    Object.values(runPaths)
      .flat()
      .map((path) => [path.split("/").at(-1)!.replace(".json", ""), fileSha256(root, path)]),
  );
  const summaryRunDigests = recordValue(summary.runReportSha256, "/replay/runReportSha256");
  requireExactRecordKeys(summary, "/replay/summary", [
    "formatVersion",
    "status",
    "wrapperPolicy",
    "wrapperSha256",
    "authoritativeReplayScope",
    "projectionTreeSha",
    "projectionArchiveSha256",
    "projectionArchiveSizeBytes",
    "projectionArchiveMemberManifestAlgorithm",
    "projectionArchiveMemberManifestSha256",
    "projectionArchiveMembers",
    "inputTreeManifestAlgorithm",
    "inputTreeManifestSha256",
    "inputTreeFiles",
    "candidateManifestSha256",
    "darwinNetworkIsolation",
    "linuxNetworkIsolation",
    "candidateOutputsEqual",
    "nonAllowlistedChanges",
    "runReportSha256",
    "darwinIsolationSha256",
    "linuxIsolationSha256",
    "projectionReceiptSha256",
    "rejectedExecutorSha256",
    "notGateClosure",
  ]);
  requireExactRecordKeys(summaryRunDigests, "/replay/runReportSha256", [
    "darwin-a",
    "darwin-b",
    "linux-a",
    "linux-b",
  ]);
  const summaryProjection = canonicalJsonString({
    projectionTreeSha: summary.projectionTreeSha,
    projectionArchiveSha256: summary.projectionArchiveSha256,
    projectionArchiveSizeBytes: summary.projectionArchiveSizeBytes,
    projectionArchiveMemberManifestAlgorithm: summary.projectionArchiveMemberManifestAlgorithm,
    projectionArchiveMemberManifestSha256: summary.projectionArchiveMemberManifestSha256,
    projectionArchiveMembers: summary.projectionArchiveMembers,
    inputTreeManifestAlgorithm: summary.inputTreeManifestAlgorithm,
    inputTreeManifestSha256: summary.inputTreeManifestSha256,
    inputTreeFiles: summary.inputTreeFiles,
  });
  if (
    summary.formatVersion !== "cloud-agents-generator-supply-replay-summary/v1" ||
    summary.status !== "DUAL_PLATFORM_TWO_ARCHIVES_EXACT_REPLAY_VERIFIED" ||
    summary.wrapperPolicy !== replayAuthority.wrapperPolicy ||
    summary.wrapperSha256 !== `sha256:${String(replayAuthority.wrapperSha256)}` ||
    summary.authoritativeReplayScope !== replayAuthority.authoritativeReplayScope ||
    summaryProjection !== commonProjection ||
    summary.candidateManifestSha256 !== commonOutputDigest ||
    summary.darwinNetworkIsolation !== "SANDBOX_EXEC_DENY_NETWORK_WITH_NEGATIVE_PROBES" ||
    summary.linuxNetworkIsolation !== "UNSHARE_NETWORK_MOUNT_PID_PINNED_UBUNTU_READ_ONLY_ROOTFS" ||
    summary.candidateOutputsEqual !== true ||
    summary.nonAllowlistedChanges !== 0 ||
    summary.notGateClosure !== true ||
    JSON.stringify(summaryRunDigests) !== JSON.stringify(expectedRunDigests) ||
    summary.darwinIsolationSha256 !==
      fileSha256(root, `${EVIDENCE_ROOT}/replay/darwin-isolation.json`) ||
    summary.linuxIsolationSha256 !==
      fileSha256(root, `${EVIDENCE_ROOT}/replay/linux-isolation.json`) ||
    summary.projectionReceiptSha256 !==
      fileSha256(root, `${EVIDENCE_ROOT}/replay/projection.json`) ||
    summary.rejectedExecutorSha256 !==
      fileSha256(root, `${EVIDENCE_ROOT}/replay/rejected-executor.json`)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      "/replay/summary",
      "Replay summary must bind both native A/B reports, raw isolation probes, and rejected executor evidence.",
    );
  }
}

function validateProjectionArchiveMembers(value: unknown, path: string): void {
  if (!Number.isInteger(value) || Number(value) <= 0) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "Projection archive member count must be a positive integer.",
    );
  }
}

/** Test-only fixed-policy seam for the replay projection member count. */
export function validateGeneratorSupplyProjectionArchiveMembersForTest(value: unknown): void {
  validateProjectionArchiveMembers(
    value,
    "/tools/generator-supply/v1/evidence/replay/linux-a.json/projectionArchiveMembers",
  );
}

function validateIsolationReceipt(
  root: string,
  isolation: JsonRecord,
  platform: "darwin-arm64" | "linux-amd64",
  replayAuthority: JsonRecord,
  runs: readonly { path: string; report: JsonRecord }[],
  commonProjection: string,
): void {
  const path = `/replay/${platform}/isolation`;
  const expectedMechanism =
    platform === "darwin-arm64"
      ? "SANDBOX_EXEC_DENY_DEFAULT_PER_RUN_BOUNDARY_V1"
      : "UNSHARE_NET_MOUNT_PID_FRESH_ROOTFS_SETPRIV_V1";
  const expectedReceiptKeys = [
    "formatVersion",
    "platform",
    "executor",
    "mechanism",
    "wrapperPolicy",
    "boundaryModel",
    "wrapperSha256",
    "replayAuthoritySha256",
    "authorityDigestsCapturedBeforeChild",
    "authorityFilesReadOnlyToCandidate",
    "sameBoundaryProbesAndReplay",
    "candidateReportFilesystemAccess",
    "reportsWrittenByTrustedParent",
    "runnerStdoutFrame",
    "runnerChildStdoutRedirectedToStderr",
    "runnerStderrCaptureBoundBytes",
    "runnerEnvironmentPolicy",
    "runnerEnvironmentSanitized",
    "freshPerReplayCaches",
    "extractionRootsInitiallyAbsent",
    "archiveSnapshotValidatedBeforeExtraction",
    "independentArchiveExtractions",
    "runAWriteRootDestroyedBeforeRunB",
    "siblingReplayRootAbsentWithinEachBoundary",
    "finalEvidenceUnavailableWithinEachBoundary",
    "sameProcessGroupEmptyAfterExit",
    "detachedDescendantCrossBoundaryReadDenied",
    "detachedDescendantsCrossRunReadDenied",
    "detachedDescendantResourceLeakNotClaimed",
    "processLifetimeClosure",
    "projectionTreeSha",
    "projectionArchiveSha256",
    "projectionArchiveSizeBytes",
    "projectionArchiveMemberManifestAlgorithm",
    "projectionArchiveMemberManifestSha256",
    "projectionArchiveMembers",
    "inputTreeManifestAlgorithm",
    "inputTreeManifestSha256",
    "inputTreeFiles",
    "reportsEqualInputProjection",
    "runReportSha256",
    "probes",
    "networkDenied",
    "nodeModulesBindingReadOnly",
    "notGateClosure",
    ...(platform === "darwin-arm64"
      ? [
          "denyDefaultSandbox",
          "writeAuthorityLimitedToDisposableRunRoot",
          "separateProcessGroupPerReplay",
          "externalSupplyReadOnly",
          "projectionArchiveReadOnly",
        ]
      : [
          "separateNetworkMountPidNamespacePerReplay",
          "pidNamespaceKillsDescendants",
          "rootfsFreshExtractionPerReplay",
          "rootfsReadOnly",
          "inputReadOnly",
          "projectionReadOnly",
          "tmpfsTmp",
          "tmpfsEphemeral",
          "tmpfsDeviceTree",
          "candidateUid",
          "candidateGid",
          "candidateSupplementaryGroups",
          "candidateCapabilities",
          "noNewPrivileges",
          "nodeModulesReadOnlyBind",
          "ubuntuRootfs",
        ]),
  ];
  requireExactRecordKeys(isolation, path, expectedReceiptKeys);
  requireEvidence(isolation, path, {
    formatVersion: "cloud-agents-generator-replay-isolation/v1",
    platform,
    executor:
      platform === "darwin-arm64"
        ? "authorized_darwin_arm64_executor"
        : "authorized_linux_amd64_executor",
    mechanism: expectedMechanism,
    wrapperPolicy: "VERSIONED_ISOLATION_WRAPPER_V1",
    wrapperSha256: `sha256:${String(replayAuthority.wrapperSha256)}`,
    sameBoundaryProbesAndReplay: true,
    authorityDigestsCapturedBeforeChild: true,
    authorityFilesReadOnlyToCandidate: true,
    networkDenied: true,
    candidateReportFilesystemAccess: false,
    reportsWrittenByTrustedParent: true,
    runnerStdoutFrame: "CLOUD_AGENTS_GENERATOR_REPLAY_REPORT_V1_LENGTH_PREFIXED_MAX_1M_RAW_FILE",
    runnerChildStdoutRedirectedToStderr: true,
    runnerStderrCaptureBoundBytes: 1048576,
    runnerEnvironmentPolicy: "ENV_I_MINIMAL_V1",
    runnerEnvironmentSanitized: true,
    freshPerReplayCaches: true,
    extractionRootsInitiallyAbsent: true,
    independentArchiveExtractions: 2,
    archiveSnapshotValidatedBeforeExtraction: true,
    reportsEqualInputProjection: true,
    nodeModulesBindingReadOnly: true,
    notGateClosure: true,
    boundaryModel: "SEPARATE_RUN_BOUNDARY_STDOUT_TRUSTED_PARENT_V1",
    runAWriteRootDestroyedBeforeRunB: true,
    siblingReplayRootAbsentWithinEachBoundary: true,
    finalEvidenceUnavailableWithinEachBoundary: true,
    sameProcessGroupEmptyAfterExit: "NOT_CLAIMED",
    detachedDescendantCrossBoundaryReadDenied: platform === "darwin-arm64",
    detachedDescendantsCrossRunReadDenied: false,
    detachedDescendantResourceLeakNotClaimed: platform === "darwin-arm64",
    processLifetimeClosure:
      platform === "darwin-arm64"
        ? "NOT_CLAIMED_RESOURCE_ONLY_RESIDUAL"
        : "PID_NAMESPACE_KILL_CHILD",
  });
  const expectedAuthority = {
    wrapper: `sha256:${String(replayAuthority.wrapperSha256)}`,
    runner: `sha256:${String(replayAuthority.runnerSha256)}`,
    pathHelper: `sha256:${String(replayAuthority.pathAuthoritySha256)}`,
    archiveInspector: `sha256:${String(replayAuthority.replayArchiveInspectorSha256)}`,
  };
  const receiptAuthority = recordValue(
    isolation.replayAuthoritySha256,
    `${path}/replayAuthoritySha256`,
  );
  requireExactRecordKeys(receiptAuthority, `${path}/replayAuthoritySha256`, [
    "wrapper",
    "runner",
    "pathHelper",
    "archiveInspector",
  ]);
  if (canonicalJsonString(receiptAuthority) !== canonicalJsonString(expectedAuthority)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `${path}/replayAuthoritySha256`,
      "Isolation receipt must bind all four current replay authority file digests.",
    );
  }
  for (const run of runs) {
    const reportAuthority = recordValue(
      run.report.replayAuthoritySha256,
      `/${run.path}/replayAuthoritySha256`,
    );
    requireExactRecordKeys(reportAuthority, `/${run.path}/replayAuthoritySha256`, [
      "wrapper",
      "runner",
      "pathHelper",
      "archiveInspector",
    ]);
    if (canonicalJsonString(reportAuthority) !== canonicalJsonString(expectedAuthority)) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `/${run.path}/replayAuthoritySha256`,
        "Each replay report must bind the same four authority file digests as its receipt.",
      );
    }
  }
  const runDigests = recordValue(isolation.runReportSha256, `${path}/runReportSha256`);
  requireExactRecordKeys(runDigests, `${path}/runReportSha256`, ["a", "b"]);
  const expectedDigests = {
    a: `sha256:${fileSha256(root, runs[0]!.path)}`,
    b: `sha256:${fileSha256(root, runs[1]!.path)}`,
  };
  if (canonicalJsonString(runDigests) !== canonicalJsonString(expectedDigests)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `${path}/runReportSha256`,
      "Isolation receipt must bind the exact trusted-parent A/B report bytes.",
    );
  }
  const receiptProjection = canonicalJsonString({
    projectionTreeSha: isolation.projectionTreeSha,
    projectionArchiveSha256: isolation.projectionArchiveSha256,
    projectionArchiveSizeBytes: isolation.projectionArchiveSizeBytes,
    projectionArchiveMemberManifestAlgorithm: isolation.projectionArchiveMemberManifestAlgorithm,
    projectionArchiveMemberManifestSha256: isolation.projectionArchiveMemberManifestSha256,
    projectionArchiveMembers: isolation.projectionArchiveMembers,
    inputTreeManifestAlgorithm: isolation.inputTreeManifestAlgorithm,
    inputTreeManifestSha256: isolation.inputTreeManifestSha256,
    inputTreeFiles: isolation.inputTreeFiles,
  });
  if (receiptProjection !== commonProjection) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `${path}/projection`,
      "Isolation receipt projection must equal the common four-run projection.",
    );
  }
  const probes = recordValue(isolation.probes, `${path}/probes`);
  requireExactRecordKeys(probes, `${path}/probes`, ["a", "b"]);
  const probeA = recordValue(probes.a, `${path}/probes/a`);
  const probeB = recordValue(probes.b, `${path}/probes/b`);
  validateIsolationProbeSet(probeA, `${path}/probes/a`, platform);
  validateIsolationProbeSet(probeB, `${path}/probes/b`, platform);
  if (canonicalJsonString(probeA) !== canonicalJsonString(probeB)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `${path}/probes`,
      "A/B same-boundary isolation probes must be byte-identical.",
    );
  }

  if (platform === "darwin-arm64") {
    requireEvidence(isolation, path, {
      denyDefaultSandbox: true,
      writeAuthorityLimitedToDisposableRunRoot: true,
      separateProcessGroupPerReplay: true,
      externalSupplyReadOnly: true,
      projectionArchiveReadOnly: true,
      detachedDescendantsCrossRunReadDenied: false,
      detachedDescendantResourceLeakNotClaimed: true,
    });
  } else {
    requireEvidence(isolation, path, {
      rootfsReadOnly: true,
      inputReadOnly: true,
      projectionReadOnly: true,
      tmpfsTmp: true,
      tmpfsEphemeral: true,
      tmpfsDeviceTree: true,
      separateNetworkMountPidNamespacePerReplay: true,
      pidNamespaceKillsDescendants: true,
      rootfsFreshExtractionPerReplay: true,
      nodeModulesReadOnlyBind: true,
      candidateUid: 65534,
      candidateGid: 65534,
      noNewPrivileges: true,
    });
    const groups = isolation.candidateSupplementaryGroups;
    if (!Array.isArray(groups) || groups.length !== 0) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `${path}/candidateSupplementaryGroups`,
        "Linux replay candidate must have no supplementary groups.",
      );
    }
    const capabilities = recordValue(
      isolation.candidateCapabilities,
      `${path}/candidateCapabilities`,
    );
    requireExactRecordKeys(capabilities, `${path}/candidateCapabilities`, [
      "effective",
      "permitted",
      "bounding",
      "ambient",
    ]);
    requireEvidence(capabilities, `${path}/candidateCapabilities`, {
      effective: "0000000000000000",
      permitted: "0000000000000000",
      bounding: "0000000000000000",
      ambient: "0000000000000000",
    });
    const rootfs = recordValue(isolation.ubuntuRootfs, `${path}/ubuntuRootfs`);
    requireExactRecordKeys(rootfs, `${path}/ubuntuRootfs`, [
      "registryIndexDigest",
      "platformManifestDigest",
      "configImageId",
      "rootfsLayerDigest",
      "exportTarSha256",
      "exportTarSizeBytes",
      "archiveInspection",
    ]);
    requireEvidence(rootfs, `${path}/ubuntuRootfs`, {
      registryIndexDigest:
        "sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517",
      platformManifestDigest:
        "sha256:fd225d3a1c5cecb1374f0d09c37a127d1f6f70e665941d6dab888c38b36c2131",
      configImageId: "sha256:a6f81fb630d51837271b89f8193810a5fc493fa4f30a55d7ebcdb3a66f3cc63a",
      rootfsLayerDigest: "sha256:b9a65b3c65ab22d490085bd0bf5490e2409da8748b406870f2463bdc41cd6795",
      exportTarSha256: "25ecc117cd77a289cc25006605dcf4ec8b137fec326db766d0abcd4147f6093e",
      exportTarSizeBytes: 80669696,
    });
    validateRootfsInspection(
      recordValue(rootfs.archiveInspection, `${path}/ubuntuRootfs/archiveInspection`),
      `${path}/ubuntuRootfs/archiveInspection`,
    );
  }
}

function validateIsolationProbeSet(
  probes: JsonRecord,
  path: string,
  platform: "darwin-arm64" | "linux-amd64",
): void {
  const keys =
    platform === "darwin-arm64"
      ? [
          "node",
          "python",
          "supply",
          "archive",
          "nodeModules",
          "nodeModulesRelink",
          "sibling",
          "final",
          "detachedDescendant",
          "posixSpawnDetached",
        ]
      : [
          "node",
          "python",
          "supply",
          "archive",
          "nodeModules",
          "stdoutChannel",
          "sibling",
          "final",
          "rootfs",
          "input",
          "projection",
          "route",
          "identity",
        ];
  requireExactRecordKeys(probes, path, keys);
  const networkError = platform === "darwin-arm64" ? "EPERM" : "ENETUNREACH";
  validateProbeEvidence(
    recordValue(probes.node, `${path}/node`),
    `${path}/node`,
    "node net.connect 1.1.1.1:443",
    1,
    networkError,
  );
  validateProbeEvidence(
    recordValue(probes.python, `${path}/python`),
    `${path}/python`,
    "python socket.connect 1.1.1.1:443",
    1,
    platform === "darwin-arm64" ? "Operation not permitted" : "Network is unreachable",
  );
  validateProbeEvidence(
    recordValue(probes.supply, `${path}/supply`),
    `${path}/supply`,
    platform === "darwin-arm64"
      ? "touch generator-supply://external-supply/codex-generator-supply-read-only-probe"
      : "touch generator-supply://external-supply/codex-generator-supply-read-only-probe",
    1,
    platform === "darwin-arm64" ? "Operation not permitted" : "Read-only file system",
  );
  validateProbeEvidence(
    recordValue(probes.nodeModules, `${path}/nodeModules`),
    `${path}/nodeModules`,
    "unlink or write the bound node_modules authority",
    1,
    platform === "darwin-arm64" ? "errno=1" : "Read-only file system",
  );
  validateProbeEvidence(
    recordValue(probes.archive, `${path}/archive`),
    `${path}/archive`,
    platform === "darwin-arm64"
      ? "touch generator-supply://core-projection/archive"
      : "touch generator-supply://core-projection/archive",
    1,
    platform === "darwin-arm64" ? "Operation not permitted" : "Read-only file system",
  );
  validateProbeEvidence(
    recordValue(probes.final, `${path}/final`),
    `${path}/final`,
    "read trusted-parent final evidence sentinel",
    platform === "darwin-arm64" ? 1 : 0,
    platform === "darwin-arm64" ? "Operation not permitted" : "",
  );
  const sibling = recordValue(probes.sibling, `${path}/sibling`);
  validateProbeEvidence(sibling, `${path}/sibling`, "test sibling replay root absent", 0, "");
  if (platform === "linux-amd64") {
    validateProbeEvidence(
      recordValue(probes.stdoutChannel, `${path}/stdoutChannel`),
      `${path}/stdoutChannel`,
      "read /proc/1/fd/1 trusted runner stdout channel",
      1,
      "No such file or directory",
    );
    validateProbeEvidence(
      recordValue(probes.rootfs, `${path}/rootfs`),
      `${path}/rootfs`,
      "touch /etc/codex-generator-supply-read-only-probe",
      1,
      "Read-only file system",
    );
    validateProbeEvidence(
      recordValue(probes.input, `${path}/input`),
      `${path}/input`,
      "touch /input/codex-generator-supply-read-only-probe",
      1,
      "Read-only file system",
    );
    validateProbeEvidence(
      recordValue(probes.projection, `${path}/projection`),
      `${path}/projection`,
      "touch /projection/core-generator-input-projection.tar",
      1,
      "Read-only file system",
    );
    validateProbeEvidence(
      recordValue(probes.route, `${path}/route`),
      `${path}/route`,
      "awk default route /proc/net/route",
      0,
      "",
    );
    const identity = recordValue(probes.identity, `${path}/identity`);
    validateLinuxIdentityProbe(identity, `${path}/identity`);
  } else {
    validateProbeEvidence(
      recordValue(probes.detachedDescendant, `${path}/detachedDescendant`),
      `${path}/detachedDescendant`,
      "fork setsid fork then read trusted-parent sentinel",
      0,
      "errno=1",
    );
    validateProbeEvidence(
      recordValue(probes.posixSpawnDetached, `${path}/posixSpawnDetached`),
      `${path}/posixSpawnDetached`,
      "posix_spawn setsid child then read trusted-parent sentinel",
      0,
      "errno=1",
    );
    validateProbeEvidence(
      recordValue(probes.nodeModulesRelink, `${path}/nodeModulesRelink`),
      `${path}/nodeModulesRelink`,
      "replace the bound node_modules symlink",
      1,
      "errno=1",
    );
  }
}

function validateLinuxIdentityProbe(identity: JsonRecord, path: string): void {
  requireExactRecordKeys(identity, path, ["command", "exitCode", "stdout", "stderr"]);
  if (
    identity.command !== "read uid gid groups capabilities and no-new-privileges" ||
    identity.exitCode !== 0 ||
    identity.stderr !== ""
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "Linux identity probe shape drifted.",
    );
  }
  const text = String(identity.stdout).replaceAll("\r", "");
  for (const field of ["Uid", "Gid"]) {
    if (
      !new RegExp(`^${field}:[\\t ]*65534[\\t ]+65534[\\t ]+65534[\\t ]+65534$`, "mu").test(text)
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        path,
        "Linux identity probe did not bind uid/gid 65534.",
      );
    }
  }
  if (
    !/^Groups:[\t ]*$/mu.test(text) ||
    ["CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"].some(
      (field) => !new RegExp(`^${field}:[\\t ]*0000000000000000$`, "mu").test(text),
    ) ||
    !/^NoNewPrivs:[\t ]*1$/mu.test(text)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "Linux identity probe did not bind empty groups, capabilities, and no-new-privileges.",
    );
  }
}

function validateRootfsInspection(inspection: JsonRecord, path: string): void {
  requireExactRecordKeys(inspection, path, [
    "formatVersion",
    "profile",
    "manifestAlgorithm",
    "manifestSha256",
    "entries",
    "regularFiles",
    "directories",
    "symlinks",
    "hardlinks",
    "unsafeEntries",
    "duplicateEntries",
    "specialEntries",
    "linkPrefixDescendants",
    "linkCycles",
  ]);
  if (
    inspection.formatVersion !== "cloud-agents-generator-replay-archive-inspection/v1" ||
    inspection.profile !== "rootfs" ||
    inspection.manifestAlgorithm !== PROJECTION_ARCHIVE_MEMBER_MANIFEST_ALGORITHM ||
    inspection.manifestSha256 !==
      "b2f581777b04657540dffa9b4f6ba98e6e0d310ea11b100cd84e6fcf19ec4af6" ||
    inspection.entries !== 3448 ||
    inspection.regularFiles !== 2587 ||
    inspection.directories !== 661 ||
    inspection.symlinks !== 198 ||
    inspection.hardlinks !== 2 ||
    inspection.unsafeEntries !== 0 ||
    inspection.duplicateEntries !== 0 ||
    inspection.specialEntries !== 0 ||
    inspection.linkPrefixDescendants !== 0 ||
    inspection.linkCycles !== 0
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "Ubuntu rootfs inspection is not exact fail-closed v1.",
    );
  }
}

function validateProbeEvidence(
  probe: JsonRecord,
  path: string,
  command: string,
  exitCode: number,
  requiredOutput: string | undefined,
): void {
  requireExactRecordKeys(probe, path, ["command", "exitCode", "stdout", "stderr"]);
  if (
    probe.command !== command ||
    probe.exitCode !== exitCode ||
    typeof probe.stdout !== "string" ||
    typeof probe.stderr !== "string" ||
    (requiredOutput === undefined
      ? false
      : requiredOutput === ""
        ? probe.stdout !== "" || probe.stderr !== ""
        : !`${String(probe.stdout)}\n${String(probe.stderr)}`.includes(requiredOutput))
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      `Isolation probe must fail with bound output containing ${requiredOutput}.`,
    );
  }
}

/** Test-only fixed-policy seam for the Linux trusted-stdout negative probe. */
export function validateGeneratorSupplyLinuxStdoutProbeForTest(probe: JsonRecord): void {
  validateProbeEvidence(
    probe,
    "/replay/linux-amd64/isolation/probes/a/stdoutChannel",
    "read /proc/1/fd/1 trusted runner stdout channel",
    1,
    "No such file or directory",
  );
}

/** Test-only fixed-policy seam for the Linux uid/capability/NNP probe. */
export function validateGeneratorSupplyLinuxIdentityProbeForTest(identity: JsonRecord): void {
  validateLinuxIdentityProbe(identity, "/replay/linux-amd64/isolation/probes/a/identity");
}

/** Test-only fixed-policy seam for the immutable Ubuntu rootfs inspection. */
export function validateGeneratorSupplyRootfsInspectionForTest(inspection: JsonRecord): void {
  validateRootfsInspection(
    inspection,
    "/replay/linux-amd64/isolation/ubuntuRootfs/archiveInspection",
  );
}

function recordArray(value: unknown, path: string): JsonRecord[] {
  if (!Array.isArray(value) || value.some((entry) => typeof entry !== "object" || entry === null)) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "Expected an array of JSON records.",
    );
  }
  return value as JsonRecord[];
}

function recordValue(value: unknown, path: string): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw supplyError("GENERATOR_SUPPLY_EVIDENCE_MISMATCH", path, "Expected a JSON record.");
  }
  return value as JsonRecord;
}

function requireExactRecordKeys(
  value: JsonRecord,
  path: string,
  expectedKeys: readonly string[],
): void {
  const actual = Object.keys(value).toSorted(bytewiseCompare);
  const expected = [...expectedKeys].toSorted(bytewiseCompare);
  if (canonicalJsonString({ values: actual }) !== canonicalJsonString({ values: expected })) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      `Record keys drifted: expected=${JSON.stringify(expected)} actual=${JSON.stringify(actual)}.`,
    );
  }
}

function stringArray(value: unknown, path: string): string[] {
  if (!Array.isArray(value) || value.some((entry) => typeof entry !== "string")) {
    throw supplyError("GENERATOR_SUPPLY_EVIDENCE_MISMATCH", path, "Expected an array of strings.");
  }
  return value;
}

function validateRawSyftDocument(document: JsonRecord, path: string): void {
  requireExactRecordKeys(document, path, [
    "artifacts",
    "artifactRelationships",
    "files",
    "source",
    "distro",
    "descriptor",
    "schema",
  ]);
  for (const [index, artifact] of recordArray(document.artifacts, `${path}/artifacts`).entries()) {
    requireExactRecordKeys(artifact, `${path}/artifacts/${index}`, [
      "id",
      "name",
      "version",
      "type",
      "foundBy",
      "locations",
      "licenses",
      "language",
      "cpes",
      "purl",
      "metadataType",
      "metadata",
    ]);
    requirePurl(artifact.purl, `${path}/artifacts/${index}/purl`);
  }
}

function strictPurlMultiset(values: readonly unknown[], path: string): string[] {
  return values
    .map((value, index) => requirePurl(value, `${path}/${index}`))
    .toSorted(bytewiseCompare);
}

function formattedPurlMultiset(entries: readonly JsonRecord[], path: string): string[] {
  const values: string[] = [];
  for (const [index, entry] of entries.entries()) {
    if (!Object.hasOwn(entry, "purl")) continue;
    values.push(requirePurl(entry.purl, `${path}/${index}/purl`));
  }
  return values.toSorted(bytewiseCompare);
}

function formattedSpdxPurlMultiset(entries: readonly JsonRecord[], path: string): string[] {
  const values: string[] = [];
  for (const [index, entry] of entries.entries()) {
    for (const [referenceIndex, reference] of recordArray(
      entry.externalRefs ?? [],
      `${path}/${index}/externalRefs`,
    ).entries()) {
      if (reference.referenceType !== "purl") continue;
      values.push(
        requirePurl(
          reference.referenceLocator,
          `${path}/${index}/externalRefs/${referenceIndex}/referenceLocator`,
        ),
      );
    }
  }
  return values.toSorted(bytewiseCompare);
}

function requirePurl(value: unknown, path: string): string {
  if (
    typeof value !== "string" ||
    !/^pkg:[a-z][a-z0-9+.-]*\/[^\s\u0000-\u001f\u007f]+$/u.test(value)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      path,
      "SBOM PURL must be a non-empty package URL with a type and encoded name/version path.",
    );
  }
  return value;
}

function multisetDifference(left: readonly string[], right: readonly string[]): string[] {
  const counts = new Map<string, number>();
  for (const value of right) counts.set(value, (counts.get(value) ?? 0) + 1);
  const difference: string[] = [];
  for (const value of left) {
    const remaining = counts.get(value) ?? 0;
    if (remaining === 0) difference.push(value);
    else counts.set(value, remaining - 1);
  }
  return difference;
}

function nulMultisetSha256(values: readonly string[]): string {
  const digest = createHash("sha256");
  for (const value of values) digest.update(value, "utf8").update("\0");
  return digest.digest("hex");
}

function canonicalRecordMultisetSha256(values: readonly JsonRecord[]): string {
  return nulMultisetSha256(
    values.map((value) => canonicalJsonString(value)).toSorted(bytewiseCompare),
  );
}

function validateFormattedNonPurlClosure(
  scope: FormattedSbomScope,
  cdxComponents: readonly JsonRecord[],
  spdxPackages: readonly JsonRecord[],
): void {
  const cdxNonPurl = cdxComponents.filter((entry) => !Object.hasOwn(entry, "purl"));
  const spdxNonPurl = spdxPackages.filter(
    (entry, index) =>
      !recordArray(entry.externalRefs ?? [], `/sbom/${scope}/packages/${index}/externalRefs`).some(
        (reference) => reference.referenceType === "purl",
      ),
  );
  const authority = FORMATTED_SBOM_NON_PURL_AUTHORITY[scope];
  if (
    cdxNonPurl.length !== authority.cyclonedxRecords ||
    canonicalRecordMultisetSha256(cdxNonPurl) !== authority.cyclonedxSha256 ||
    spdxNonPurl.length !== authority.spdxRecords ||
    canonicalRecordMultisetSha256(spdxNonPurl) !== authority.spdxSha256
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `/sbom/${scope}/formattedNonPurlClosure`,
      "Formatted SBOM non-PURL records must equal the reviewed v1 canonical multisets.",
    );
  }
}

/** Test-only fixed-policy seam for the reviewed formatted non-PURL multisets. */
export function validateGeneratorSupplyFormattedNonPurlClosureForTest(
  scope: FormattedSbomScope,
  cdxComponents: readonly JsonRecord[],
  spdxPackages: readonly JsonRecord[],
): void {
  validateFormattedNonPurlClosure(scope, cdxComponents, spdxPackages);
}

function canonicalJsonString(value: unknown): string {
  return Buffer.from(canonicalizeJson(value)).toString("utf8");
}

function bytewiseCompare(left: string, right: string): number {
  return Buffer.from(left, "utf8").compare(Buffer.from(right, "utf8"));
}

function gitOutput(
  git: string,
  args: readonly string[],
  environment: NodeJS.ProcessEnv = process.env,
): string {
  return execFileSync(git, [...args], {
    env: environment,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function projectionGitEnvironment(): NodeJS.ProcessEnv {
  return {
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_NOSYSTEM: "1",
    GIT_CONFIG_SYSTEM: "/dev/null",
    GIT_NO_REPLACE_OBJECTS: "1",
    GIT_OPTIONAL_LOCKS: "0",
    HOME: "/var/empty",
    LANG: "C",
    LC_ALL: "C",
    PATH: "/usr/bin:/bin",
    TZ: "UTC",
  };
}

function gitPathList(
  git: string,
  repositoryRoot: string,
  args: readonly string[],
  environment: NodeJS.ProcessEnv = process.env,
): string[] {
  const output = execFileSync(git, ["-C", repositoryRoot, ...args], {
    env: environment,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  return output.split("\0").filter((path) => path !== "");
}

function isProjectionExcludedPath(path: string): boolean {
  return PROJECTION_EXCLUSIONS.includes(path as (typeof PROJECTION_EXCLUSIONS)[number]);
}

function fileSha256(root: string, path: string): string {
  return createHash("sha256")
    .update(readFileSync(resolveContainedRegularFile(root, path)))
    .digest("hex");
}

function isNonFutureDate(value: unknown): boolean {
  const timestamp = Date.parse(String(value));
  return Number.isFinite(timestamp) && timestamp <= Date.now();
}

function requireEvidence(
  value: JsonRecord,
  path: string,
  expected: Readonly<Record<string, string | number | boolean>>,
): void {
  for (const [key, expectedValue] of Object.entries(expected)) {
    if (value[key] !== expectedValue) {
      throw supplyError(
        "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
        `${path}/${key}`,
        `Expected ${JSON.stringify(expectedValue)}, received ${JSON.stringify(value[key])}.`,
      );
    }
  }
}

function validateOutput(root: string, value: JsonRecord): void {
  const ajv = createAjv(root);
  const validate = ajv.getSchema(OUTPUT_SCHEMA_ID);
  if (validate === undefined || !validate(value)) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/profile",
      `Generated supply profile schema validation failed: ${JSON.stringify(validate?.errors ?? [])}`,
    );
  }
}

function createAjv(root: string): Ajv2020 {
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  for (const path of [GENERATOR_SUPPLY_SOURCE_SCHEMA_PATH, GENERATOR_SUPPLY_OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(root, path));
  }
  return ajv;
}

function parseJsonFile(root: string, path: string, overrides?: JsonOverrides): JsonRecord {
  const override = overrides?.get(path);
  if (override !== undefined) {
    if (path !== REPLAY_SUMMARY_PATH) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${path}`,
        "In-memory evidence overrides are restricted to the derived replay summary path.",
      );
    }
    return override;
  }
  try {
    return JSON.parse(readFileSync(resolveContainedRegularFile(root, path), "utf8")) as JsonRecord;
  } catch (error) {
    throw supplyError(
      "GENERATOR_SUPPLY_EVIDENCE_MISMATCH",
      `/${path}`,
      `Generator supply JSON is invalid: ${String(error)}`,
    );
  }
}

function evidenceBytes(root: string, path: string, overrides?: JsonOverrides): Buffer {
  const override = overrides?.get(path);
  if (override === undefined) return readFileSync(resolveContainedRegularFile(root, path));
  if (path !== REPLAY_SUMMARY_PATH) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "In-memory evidence overrides are restricted to the derived replay summary path.",
    );
  }
  return Buffer.from(serializeGeneratorSupplyJson(override), "utf8");
}

function resolveContainedRegularFile(root: string, path: string): string {
  if (isAbsolute(path) || path.includes("\\") || path.split("/").includes("..")) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply paths must be contained repository-relative POSIX paths.",
    );
  }
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply path escapes the repository root.",
    );
  }
  const metadata = lstatSync(absolute);
  if (!metadata.isFile() || metadata.isSymbolicLink() || realpathSync(absolute) !== absolute) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply inputs must be regular non-symlink files.",
    );
  }
  return absolute;
}

function withGeneratorSupplyInputSnapshot<T>(
  root: string,
  options: GeneratorSupplyInputSnapshotOptions,
  operation: (snapshot: GeneratorSupplyInputSnapshot) => T,
): T {
  const snapshot = captureGeneratorSupplyInputSnapshot(root, options);
  try {
    const result = operation(snapshot);
    assertGeneratorSupplyInputSnapshotCurrent(snapshot);
    return result;
  } finally {
    rmSync(snapshot.snapshotRoot, { force: true, recursive: true });
  }
}

function captureGeneratorSupplyInputSnapshot(
  root: string,
  options: GeneratorSupplyInputSnapshotOptions,
): GeneratorSupplyInputSnapshot {
  const originalRoot = realpathSync(root);
  const snapshotRoot = mkdtempSync(join(tmpdir(), "cloud-agents-generator-supply-inputs-"));
  try {
    const excludedEvidencePaths = new Set(options.excludedEvidencePaths ?? []);
    for (const path of excludedEvidencePaths) {
      if (path !== REPLAY_SUMMARY_PATH) {
        throw supplyError(
          "GENERATOR_SUPPLY_BINDING_MISMATCH",
          "/profile/inputSnapshot/excludedEvidencePaths",
          "Only the derived replay summary may be excluded while assembling generated outputs.",
        );
      }
    }
    const sourceBytes = readStableContainedRegularFile(originalRoot, GENERATOR_SUPPLY_SOURCE_PATH);
    let source: JsonRecord;
    try {
      source = recordValue(
        JSON.parse(sourceBytes.toString("utf8")) as unknown,
        "/generatorSupplyInputSnapshot/source",
      );
    } catch (error) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${GENERATOR_SUPPLY_SOURCE_PATH}`,
        `Generator supply source cannot seed an immutable input snapshot: ${String(error)}`,
      );
    }
    const profile = recordValue(source.profile, "/generatorSupplyInputSnapshot/source/profile");
    const replayAuthority = recordValue(
      profile.replayAuthority,
      "/generatorSupplyInputSnapshot/source/profile/replayAuthority",
    );
    const paths = new Set<string>([
      GENERATOR_SUPPLY_SOURCE_PATH,
      GENERATOR_SUPPLY_SOURCE_SCHEMA_PATH,
      GENERATOR_SUPPLY_OUTPUT_SCHEMA_PATH,
      "tools/generator-supply/npm/package-lock.json",
      "tools/generator-supply/go/go.mod",
      "tools/contract-standards/uv.lock",
      "scripts/check-platform-contract-standards.ts",
      "bun.lock",
    ]);
    for (const key of [
      "wrapperPath",
      "runnerPath",
      "pathAuthorityPath",
      "replayArchiveInspectorPath",
    ]) {
      const value = replayAuthority[key];
      if (typeof value !== "string" || value === "") {
        throw supplyError(
          "GENERATOR_SUPPLY_BINDING_MISMATCH",
          `/profile/replayAuthority/${key}`,
          "Replay authority paths must be non-empty strings before snapshot capture.",
        );
      }
      paths.add(value);
    }
    const evidencePaths = listGeneratorSupplyEvidenceFiles(originalRoot).filter(
      (path) => !excludedEvidencePaths.has(path),
    );
    for (const path of evidencePaths) paths.add(path);
    for (const path of options.additionalPaths ?? []) paths.add(path);

    const files = new Map<string, Buffer>();
    for (const path of [...paths].toSorted(bytewiseCompare)) {
      const bytes =
        path === GENERATOR_SUPPLY_SOURCE_PATH
          ? sourceBytes
          : readStableContainedRegularFile(originalRoot, path);
      files.set(path, bytes);
      const destination = resolve(snapshotRoot, path);
      mkdirSync(dirname(destination), { recursive: true });
      writeFileSync(destination, bytes, { flag: "wx", mode: 0o600 });
    }
    return { originalRoot, snapshotRoot, files, evidencePaths, excludedEvidencePaths };
  } catch (error) {
    rmSync(snapshotRoot, { force: true, recursive: true });
    throw error;
  }
}

function assertGeneratorSupplyInputSnapshotCurrent(snapshot: GeneratorSupplyInputSnapshot): void {
  const currentEvidencePaths = listGeneratorSupplyEvidenceFiles(snapshot.originalRoot).filter(
    (path) => !snapshot.excludedEvidencePaths.has(path),
  );
  if (JSON.stringify(currentEvidencePaths) !== JSON.stringify(snapshot.evidencePaths)) {
    throw supplyError(
      "GENERATOR_SUPPLY_PROFILE_STALE",
      "/profile/inputSnapshot/evidenceClosure",
      "Generator supply evidence closure changed while outputs were being assembled.",
    );
  }
  for (const [path, expected] of snapshot.files) {
    const actual = readStableContainedRegularFile(snapshot.originalRoot, path);
    if (!actual.equals(expected)) {
      throw supplyError(
        "GENERATOR_SUPPLY_PROFILE_STALE",
        `/profile/inputSnapshot/${path}`,
        "Generator supply input bytes changed while outputs were being assembled.",
      );
    }
  }
}

function listGeneratorSupplyEvidenceFiles(root: string): string[] {
  const rootReal = realpathSync(root);
  const paths: string[] = [];
  const visit = (relativeDirectory: string): void => {
    const absoluteDirectory = resolve(rootReal, relativeDirectory);
    const directoryMetadata = lstatSync(absoluteDirectory);
    if (
      !directoryMetadata.isDirectory() ||
      directoryMetadata.isSymbolicLink() ||
      realpathSync(absoluteDirectory) !== absoluteDirectory
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${relativeDirectory}`,
        "Generator supply evidence directories must be real, contained directories.",
      );
    }
    for (const name of readdirSync(absoluteDirectory).toSorted(bytewiseCompare)) {
      const path = `${relativeDirectory}/${name}`;
      const metadata = lstatSync(resolve(rootReal, path));
      if (metadata.isDirectory() && !metadata.isSymbolicLink()) visit(path);
      else if (metadata.isFile() && !metadata.isSymbolicLink()) paths.push(path);
      else {
        throw supplyError(
          "GENERATOR_SUPPLY_BINDING_MISMATCH",
          `/${path}`,
          "Generator supply evidence closure permits only real directories and regular files.",
        );
      }
    }
  };
  visit(EVIDENCE_ROOT);
  return paths.toSorted(bytewiseCompare);
}

function readStableContainedRegularFile(root: string, path: string): Buffer {
  if (isAbsolute(path) || path.includes("\\") || path.split("/").includes("..")) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply snapshot paths must be contained repository-relative POSIX paths.",
    );
  }
  const rootReal = realpathSync(root);
  const absolute = resolve(rootReal, path);
  const relation = relative(rootReal, absolute);
  if (
    relation === "" ||
    relation === ".." ||
    relation.startsWith(`..${sep}`) ||
    isAbsolute(relation)
  ) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply snapshot path escapes the repository root.",
    );
  }
  const pathBefore = lstatSync(absolute, { bigint: true });
  if (!pathBefore.isFile() || pathBefore.isSymbolicLink() || realpathSync(absolute) !== absolute) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      `/${path}`,
      "Generator supply snapshot inputs must be regular non-symlink files.",
    );
  }
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (
      !descriptorBefore.isFile() ||
      descriptorBefore.dev !== pathBefore.dev ||
      descriptorBefore.ino !== pathBefore.ino
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${path}`,
        "Generator supply snapshot input changed before it could be opened.",
      );
    }
    const bytes = readFileSync(descriptor);
    const descriptorAfter = fstatSync(descriptor, { bigint: true });
    const pathAfter = lstatSync(absolute, { bigint: true });
    if (
      descriptorAfter.dev !== descriptorBefore.dev ||
      descriptorAfter.ino !== descriptorBefore.ino ||
      descriptorAfter.size !== descriptorBefore.size ||
      descriptorAfter.mtimeNs !== descriptorBefore.mtimeNs ||
      descriptorAfter.ctimeNs !== descriptorBefore.ctimeNs ||
      pathAfter.dev !== descriptorBefore.dev ||
      pathAfter.ino !== descriptorBefore.ino ||
      !pathAfter.isFile() ||
      pathAfter.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_PROFILE_STALE",
        `/${path}`,
        "Generator supply snapshot input changed while it was being read.",
      );
    }
    return bytes;
  } finally {
    closeSync(descriptor);
  }
}

type RenameFile = (source: string, destination: string) => void;
type RemoveFile = (path: string) => void;

function writeGeneratedSet(
  root: string,
  outputs: readonly { path: string; value: JsonRecord }[],
  renameFile: RenameFile = renameSync,
  removeBackup: RemoveFile = unlinkSync,
  assertInputsCurrent: () => void = () => {},
): void {
  const rootReal = realpathSync(root);
  const transactionToken = `${process.pid}-${Date.now()}-${process.hrtime.bigint()}`;
  const transactionRoot = join(rootReal, `.generator-supply-transaction-${transactionToken}`);
  if (outputs.length === 0 || new Set(outputs.map(({ path }) => path)).size !== outputs.length) {
    throw supplyError(
      "GENERATOR_SUPPLY_BINDING_MISMATCH",
      "/generatorSupplyOutputs",
      "Generated output transactions require a non-empty unique path set.",
    );
  }
  const prepared = outputs.map(({ path, value }, index) => {
    const absolute = resolve(rootReal, path);
    const relation = relative(rootReal, absolute);
    if (
      relation === "" ||
      relation === ".." ||
      relation.startsWith(`..${sep}`) ||
      isAbsolute(relation)
    ) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${path}`,
        "Output path escapes root.",
      );
    }
    mkdirSync(dirname(absolute), { recursive: true });
    if (realpathSync(dirname(absolute)) !== dirname(absolute)) {
      throw supplyError(
        "GENERATOR_SUPPLY_BINDING_MISMATCH",
        `/${path}`,
        "Generated output parent directories must not traverse symlinks.",
      );
    }
    return {
      absolute,
      temporary: join(transactionRoot, `${index}.new`),
      backup: join(transactionRoot, `${index}.old`),
      contents: serializeGeneratorSupplyJson(value),
    };
  });
  mkdirSync(transactionRoot, { mode: 0o700 });
  const backups: { absolute: string; backup: string }[] = [];
  const installed: string[] = [];
  try {
    // Prepare every new byte before touching an existing generated output.
    for (const entry of prepared) {
      writeFileSync(entry.temporary, entry.contents, { flag: "wx", mode: 0o600 });
    }
    assertInputsCurrent();
    for (const entry of prepared) {
      if (pathExists(entry.absolute)) {
        renameFile(entry.absolute, entry.backup);
        backups.push({ absolute: entry.absolute, backup: entry.backup });
      }
    }
    for (const entry of prepared) {
      renameFile(entry.temporary, entry.absolute);
      installed.push(entry.absolute);
    }
    // The final freshness check occurs while every original output is still
    // recoverable.  Passing it is the transaction commit point.
    assertInputsCurrent();
  } catch (error) {
    const rollbackErrors: string[] = [];
    for (const absolute of installed.reverse()) {
      try {
        if (pathExists(absolute)) unlinkSync(absolute);
      } catch (rollbackError) {
        rollbackErrors.push(`remove ${absolute}: ${String(rollbackError)}`);
      }
    }
    for (const { absolute, backup } of backups.reverse()) {
      try {
        if (pathExists(backup)) renameSync(backup, absolute);
      } catch (rollbackError) {
        rollbackErrors.push(`restore ${absolute}: ${String(rollbackError)}`);
      }
    }
    for (const entry of prepared) {
      try {
        if (pathExists(entry.temporary)) unlinkSync(entry.temporary);
      } catch (cleanupError) {
        rollbackErrors.push(`cleanup ${entry.temporary}: ${String(cleanupError)}`);
      }
    }
    const suffix =
      rollbackErrors.length === 0
        ? ""
        : ` rollback failed; generated output is fail-closed and requires manual recovery: ${rollbackErrors.join("; ")}`;
    if (rollbackErrors.length === 0) {
      try {
        if (pathExists(transactionRoot)) {
          rmSync(transactionRoot, { force: true, recursive: true });
        }
      } catch (cleanupError) {
        throw new Error(
          `Generator supply output transaction failed: ${String(error)}${suffix}; transaction-directory cleanup failed: ${String(cleanupError)}`,
        );
      }
    }
    throw new Error(`Generator supply output transaction failed: ${String(error)}${suffix}`);
  }

  // Output installation is already committed.  Backup cleanup must never
  // enter the destructive rollback path because an earlier backup may have
  // been irreversibly removed.
  const cleanupErrors: string[] = [];
  for (const entry of backups) {
    try {
      removeBackup(entry.backup);
    } catch (error) {
      cleanupErrors.push(`remove backup for ${entry.absolute}: ${String(error)}`);
    }
  }
  if (cleanupErrors.length === 0) {
    try {
      rmSync(transactionRoot, { force: true, recursive: true });
    } catch (error) {
      throw new Error(
        `Generator supply outputs committed consistently, but transaction-directory cleanup failed closed: ${String(error)}`,
      );
    }
    return;
  }
  throw new Error(
    `Generator supply outputs committed consistently, but backup cleanup failed closed: ${cleanupErrors.join("; ")}`,
  );
}

function pathExists(path: string): boolean {
  try {
    lstatSync(path);
    return true;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return false;
    throw error;
  }
}

function assertGeneratedBytes(root: string, path: string, expected: string): void {
  const actual = readFileSync(resolveContainedRegularFile(root, path), "utf8");
  if (actual !== expected) {
    throw supplyError(
      "GENERATOR_SUPPLY_PROFILE_STALE",
      `/${path}`,
      `${path} is stale; run the generator with --write.`,
    );
  }
}

function domainDigest(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(domain, "utf8")
    .update("\0")
    .update(canonicalizeJson(value as JsonRecord))
    .digest("hex")}`;
}

function supplyError(
  code: GeneratorSupplyProfileError["code"],
  path: string,
  message: string,
): GeneratorSupplyProfileError {
  return new GeneratorSupplyProfileError(code, path, message);
}
