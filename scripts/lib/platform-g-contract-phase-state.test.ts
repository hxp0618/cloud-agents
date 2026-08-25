import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildGContractPhaseRecordModel,
  buildGContractPhaseBindingRegistry,
  buildGContractPhaseReviewTuple,
  G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
  G_CONTRACT_PHASE_SOURCE_PATH,
  G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
  readGContractPhaseRecordSource,
  renderGContractPhaseRecord,
  serializeGContractPhaseJson,
  type GContractPhaseBindingRegistry,
  type GContractPhaseRecordBuildInput,
  type GContractPhaseReviewTuple,
  type ReviewGitBinding,
} from "./platform-g-contract-phase-record";
import {
  buildPlatformContractLockV3Assembled,
  buildPlatformContractLockV3PhaseBound,
  derivePlatformContractLockV3AssembledSnapshotIdentity,
  PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS,
  serializePlatformContractLockV3,
  type PlatformContractLockV3ArtifactIdentity,
  type PlatformContractLockV3AssembledAuthority,
  type PlatformContractLockV3PhaseBinding,
} from "./platform-contract-lock-v3";
import {
  captureGContractPhaseTerminalReviewBinding,
  assertSingleAddedRegularPathCommit,
  captureGContractPhaseReviewBinding,
  classifyGContractPhaseTopology,
  inspectGContractPhaseState,
} from "./platform-g-contract-phase-state";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("G-CONTRACT-P1 read-only phase state", () => {
  it("classifies the absent state without writing and fails closed on orphan topology", () => {
    const root = sourceFixture();
    const before = listFiles(root);
    expect(inspectGContractPhaseState(root)).toBe("PRE_CANDIDATE_ABSENT");
    expect(listFiles(root)).toEqual(before);

    const source = readGContractPhaseRecordSource(root);
    writeText(root, source.reviewSlots[1]!.reviewPath, "orphan\n");
    expectCode(() => classifyGContractPhaseTopology(root), "G_CONTRACT_PHASE_PARTIAL_STATE");
  });

  it("captures and reproduces a single-parent exact-one-added 100644 review with a bound diff", () => {
    const fixture = reviewFixture();
    const binding = captureGContractPhaseReviewBinding(
      fixture.root,
      "generator_supply_v3",
      fixture.candidate,
      fixture.review,
      "assembler",
      "independent-reviewer",
    );
    expect(binding.review.parent).toBe(fixture.candidate);
    expect(binding.review.mode).toBe("100644");
    expect(binding.review.diffSha256).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(binding.review.path).toBe(
      "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md",
    );
  });

  it("rejects extra paths, rename-like diffs, symlink modes, merge reviews, and self-review", () => {
    const extra = reviewFixture({ extraReviewPath: true });
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          extra.root,
          "generator_supply_v3",
          extra.candidate,
          extra.review,
          "assembler",
          "reviewer",
        ),
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
    );

    const renamed = reviewFixture({ renameReviewPath: true });
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          renamed.root,
          "generator_supply_v3",
          renamed.candidate,
          renamed.review,
          "assembler",
          "reviewer",
        ),
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
    );

    const symlink = reviewFixture({ symlinkReviewPath: true });
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          symlink.root,
          "generator_supply_v3",
          symlink.candidate,
          symlink.review,
          "assembler",
          "reviewer",
        ),
      "G_CONTRACT_PHASE_PATH_INVALID",
    );

    const merge = mergeReviewFixture();
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          merge.root,
          "generator_supply_v3",
          merge.candidate,
          merge.review,
          "assembler",
          "reviewer",
        ),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );

    const valid = reviewFixture();
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          valid.root,
          "generator_supply_v3",
          valid.candidate,
          valid.review,
          "same-actor",
          "same-actor",
        ),
      "G_CONTRACT_PHASE_SELF_REVIEW",
    );

    const conflicting = reviewFixture({
      reviewBody:
        "# Review\n\n## Verdict\n\n`REQUEST_CHANGES - P0=0 / P1=1 / P2=0`\n\nThe rejected candidate had text APPROVE - P0=0 / P1=0 / P2=0.\n",
    });
    expectCode(
      () =>
        captureGContractPhaseReviewBinding(
          conflicting.root,
          "generator_supply_v3",
          conflicting.candidate,
          conflicting.review,
          "assembler",
          "reviewer",
        ),
      "G_CONTRACT_PHASE_REVIEW_VERDICT_INVALID",
    );
  });

  it("accepts only an exact one-path R5 candidate and rejects an extra path", () => {
    const root = gitFixture();
    const source = readGContractPhaseRecordSource(root);
    writeText(root, source.record.path, "# R5\n");
    const exact = commitAll(root, "R5 only");
    expect(() => assertSingleAddedRegularPathCommit(root, exact, source.record.path)).not.toThrow();

    writeText(root, "extra.txt", "extra\n");
    writeText(root, source.reviewSlots[1]!.reviewPath, "APPROVE - P0=0 / P1=0 / P2=0\n");
    const extra = commitAll(root, "review plus extra");
    expectCode(
      () => assertSingleAddedRegularPathCommit(root, extra, source.reviewSlots[1]!.reviewPath),
      "G_CONTRACT_PHASE_REVIEW_DIFF_INVALID",
    );
  });

  it("validates the fixed R5 review Git/live bytes before reporting its pre-binding topology", () => {
    const fixture = terminalFixture();
    git(fixture.root, ["checkout", "-q", "--detach", fixture.r5Review]);
    expect(
      inspectGContractPhaseState(fixture.root, {
        recordBuildInput: fixture.recordBuildInput,
      }),
    ).toBe("R5_REVIEW_CURRENT_BINDING_ABSENT");

    const source = readGContractPhaseRecordSource(fixture.root);
    writeText(fixture.root, source.reviewSlots[1]!.reviewPath, `${reviewText()}dirty byte\n`);
    expectCode(
      () =>
        inspectGContractPhaseState(fixture.root, {
          recordBuildInput: fixture.recordBuildInput,
        }),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );
  });

  it("internally verifies PHASE_BOUND and requires an exact terminal-review identity", () => {
    const fixture = terminalFixture();
    expect(
      inspectGContractPhaseState(fixture.root, {
        recordBuildInput: fixture.recordBuildInput,
        expectedTuple: fixture.tuple,
        expectedRegistry: fixture.registry,
        bindingActorId: "slice-i-binder",
        expectedTerminalReview: fixture.terminalReview,
      }),
    ).toBe("REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE");

    expectCode(
      () =>
        inspectGContractPhaseState(fixture.root, {
          recordBuildInput: fixture.recordBuildInput,
          expectedTuple: fixture.tuple,
          expectedRegistry: fixture.registry,
          bindingActorId: "slice-i-binder",
          expectedTerminalReview: {
            ...fixture.terminalReview,
            sha256: `sha256:${"f".repeat(64)}`,
          },
        }),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );

    const fixtureSource = readGContractPhaseRecordSource(fixture.root);
    writeText(
      fixture.root,
      fixtureSource.reviewSlots[1]!.reviewPath,
      `${reviewText()}dirty working-tree byte\n`,
    );
    expectCode(
      () =>
        inspectGContractPhaseState(fixture.root, {
          recordBuildInput: fixture.recordBuildInput,
          expectedTuple: fixture.tuple,
          expectedRegistry: fixture.registry,
          bindingActorId: "slice-i-binder",
          expectedTerminalReview: fixture.terminalReview,
        }),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );
    writeText(fixture.root, fixtureSource.reviewSlots[1]!.reviewPath, reviewText());
    writeText(
      fixture.root,
      fixtureSource.binding.finalReviewPath,
      `${reviewText()}dirty working-tree byte\n`,
    );
    expectCode(
      () =>
        inspectGContractPhaseState(fixture.root, {
          recordBuildInput: fixture.recordBuildInput,
          expectedTuple: fixture.tuple,
          expectedRegistry: fixture.registry,
          bindingActorId: "slice-i-binder",
          expectedTerminalReview: fixture.terminalReview,
        }),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );

    const invalidLock = terminalFixture({ invalidPhaseBound: true });
    expectCode(
      () =>
        inspectGContractPhaseState(invalidLock.root, {
          recordBuildInput: invalidLock.recordBuildInput,
          expectedTuple: invalidLock.tuple,
          expectedRegistry: invalidLock.registry,
          bindingActorId: "slice-i-binder",
          expectedTerminalReview: invalidLock.terminalReview,
        }),
      "G_CONTRACT_PHASE_GIT_LINEAGE_INVALID",
    );
  }, 30_000);
});

function reviewFixture(
  options: {
    extraReviewPath?: boolean;
    renameReviewPath?: boolean;
    symlinkReviewPath?: boolean;
    reviewBody?: string;
  } = {},
): { root: string; candidate: string; review: string } {
  const root = gitFixture();
  const source = readGContractPhaseRecordSource(root);
  writeText(root, source.reviewSlots[0]!.candidateSubjectPath, '{"profile":"v3"}\n');
  if (options.renameReviewPath) writeText(root, "review-source.md", reviewText());
  const candidate = commitAll(root, "supply candidate");
  if (options.renameReviewPath) {
    mkdirSync(dirname(resolve(root, source.reviewSlots[0]!.reviewPath)), { recursive: true });
    git(root, ["mv", "review-source.md", source.reviewSlots[0]!.reviewPath]);
  } else if (options.symlinkReviewPath) {
    const absolute = resolve(root, source.reviewSlots[0]!.reviewPath);
    mkdirSync(dirname(absolute), { recursive: true });
    symlinkSync("../../../../base.txt", absolute);
  } else {
    writeText(root, source.reviewSlots[0]!.reviewPath, options.reviewBody ?? reviewText());
  }
  if (options.extraReviewPath) writeText(root, "extra-review-byte.txt", "extra\n");
  const review = commitAll(root, "supply review");
  return { root, candidate, review };
}

function mergeReviewFixture(): { root: string; candidate: string; review: string } {
  const root = gitFixture();
  const source = readGContractPhaseRecordSource(root);
  writeText(root, source.reviewSlots[0]!.candidateSubjectPath, '{"profile":"v3"}\n');
  const candidate = commitAll(root, "supply candidate");
  git(root, ["checkout", "-q", "-b", "review-side"]);
  writeText(root, source.reviewSlots[0]!.reviewPath, reviewText());
  commitAll(root, "review side");
  git(root, ["checkout", "-q", "master"]);
  writeText(root, "main-side.txt", "main\n");
  commitAll(root, "main side");
  git(root, ["merge", "-q", "--no-ff", "review-side", "-m", "merge review"]);
  return { root, candidate, review: git(root, ["rev-parse", "HEAD"]) };
}

function sourceFixture(): string {
  const root = mkdtempSync(join(tmpdir(), "g-contract-phase-state-"));
  temporaryRoots.push(root);
  for (const path of [
    G_CONTRACT_PHASE_SOURCE_PATH,
    G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
    G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
    G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
    G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  ]) {
    mkdirSync(dirname(resolve(root, path)), { recursive: true });
    cpSync(resolve(repositoryRoot, path), resolve(root, path));
  }
  return root;
}

function gitFixture(): string {
  const root = sourceFixture();
  git(root, ["init", "-q"]);
  writeText(root, "base.txt", "base\n");
  commitAll(root, "base");
  return root;
}

function reviewText(): string {
  return "# Independent review\n\n## Verdict\n\n`APPROVE - P0=0 / P1=0 / P2=0`\n";
}

function terminalFixture(options: { invalidPhaseBound?: boolean } = {}): {
  root: string;
  recordBuildInput: GContractPhaseRecordBuildInput;
  tuple: GContractPhaseReviewTuple;
  registry: GContractPhaseBindingRegistry;
  terminalReview: ReviewGitBinding;
  supplyCandidate: string;
  r5Review: string;
} {
  const root = gitFixture();
  const source = readGContractPhaseRecordSource(root);
  const projectionCommit = git(root, ["rev-parse", "HEAD"]);
  const projectionTree = git(root, ["rev-parse", `${projectionCommit}^{tree}`]);

  populateRecordAuthorityInputs(root, source);
  writeText(root, source.dynamicAuthorities.projectionReceiptPath, '{"state":"PROJECTED"}\n');
  writeText(root, source.dynamicAuthorities.supplyManifestPath, '{"files":[]}\n');
  writeText(
    root,
    source.dynamicAuthorities.supplyProfilePath,
    '{"formatVersion":"cloud-agents-generator-supply-profile-registry/v3","notGateClosure":true}\n',
  );
  const assembled = buildPlatformContractLockV3Assembled(lockAuthority());
  writeText(
    root,
    source.dynamicAuthorities.assembledLockPath,
    serializePlatformContractLockV3(assembled),
  );
  const supplyCandidate = commitAll(root, "supply candidate");
  writeText(root, source.reviewSlots[0]!.reviewPath, reviewText());
  const supplyReview = commitAll(root, "supply review");
  const supplyBinding = captureGContractPhaseReviewBinding(
    root,
    "generator_supply_v3",
    supplyCandidate,
    supplyReview,
    "slice-e-assembler",
    "supply-reviewer",
  );

  const recordBuildInput: GContractPhaseRecordBuildInput = {
    projectionCommit,
    projectionTree,
    projectionArchiveSha256: repeatedSha256("a"),
    supplyCandidate: supplyBinding.candidate,
    supplyReview: supplyBinding.review,
  };
  const recordBytes = renderGContractPhaseRecord(
    root,
    buildGContractPhaseRecordModel(root, recordBuildInput),
  );
  writeText(root, source.record.path, recordBytes);
  const r5Candidate = commitAll(root, "R5 candidate");
  writeText(root, source.reviewSlots[1]!.reviewPath, reviewText());
  const r5Review = commitAll(root, "R5 review");
  const r5Binding = captureGContractPhaseReviewBinding(
    root,
    "g_contract_r5",
    r5Candidate,
    r5Review,
    "r5-writer",
    "r5-reviewer",
  );

  const tuple = buildGContractPhaseReviewTuple(root, [supplyBinding, r5Binding]);
  const registry = buildGContractPhaseBindingRegistry(root, tuple);
  writeText(root, source.binding.tuplePath, serializeGContractPhaseJson(tuple));
  writeText(root, source.binding.registryPath, serializeGContractPhaseJson(registry));
  const assembledSnapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
    commitSha1: supplyCandidate,
    treeSha1: git(root, ["rev-parse", `${supplyCandidate}^{tree}`]),
  });
  const binding: PlatformContractLockV3PhaseBinding = {
    state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
    artifacts: PLATFORM_CONTRACT_LOCK_V3_PHASE_ARTIFACTS.map((entry) => ({
      role: entry.role,
      artifact: worktreeArtifact(root, entry.path),
    })),
  };
  const phaseBound = buildPlatformContractLockV3PhaseBound(assembled, assembledSnapshot, binding);
  writeText(
    root,
    source.dynamicAuthorities.assembledLockPath,
    options.invalidPhaseBound
      ? '{"formatVersion":"invalid","state":"PHASE_BOUND"}\n'
      : serializePlatformContractLockV3(phaseBound),
  );
  const bindingCandidate = commitAll(root, "Slice I binding candidate");

  writeText(root, source.binding.finalReviewPath, reviewText());
  const terminalCommit = commitAll(root, "terminal binding review");
  const terminalReview = captureGContractPhaseTerminalReviewBinding(
    root,
    bindingCandidate,
    terminalCommit,
    "terminal-reviewer",
  );
  return {
    root,
    recordBuildInput,
    tuple,
    registry,
    terminalReview,
    supplyCandidate,
    r5Review,
  };
}

function populateRecordAuthorityInputs(
  root: string,
  source: ReturnType<typeof readGContractPhaseRecordSource>,
): void {
  const paths = [
    source.criteriaAuthority.path,
    source.currentCandidateAuthority.path,
    ...source.prerequisites.map(({ path }) => path),
    ...source.historicalRecords.map(({ path }) => path),
    ...source.currentSourceInputPaths,
  ];
  for (const path of new Set(paths)) {
    const destination = resolve(root, path);
    mkdirSync(dirname(destination), { recursive: true });
    cpSync(resolve(repositoryRoot, path), destination);
  }
}

function lockAuthority(): PlatformContractLockV3AssembledAuthority {
  return {
    generatorSupply: {
      formatVersion: "cloud-agents-generator-supply-profile-registry/v3",
      profileId: "cloud-agents/generator-supply-profile/v3",
      profileDigest: repeatedSha256("1"),
      registryDigest: repeatedSha256("2"),
      candidateManifestSha256: repeatedSha256("3"),
      outputFiles: 49,
      evidenceManifest: syntheticArtifact("tools/generator-supply/v3/evidence-manifest.json", "4"),
      profile: syntheticArtifact("tools/generator-supply/v3/profile.json", "5"),
    },
    projection: {
      algorithm: "exact-ordered-paths-v1",
      exclusionCount: 17,
      exclusionsDigest: repeatedSha256("6"),
      receipt: syntheticArtifact("tools/generator-supply/v3/evidence/replay/projection.json", "7"),
    },
    contractStandards: {
      formatVersion: "cloud-agents-contract-standards-profile/v3",
      profile: syntheticArtifact("tools/contract-standards/profile-v3.json", "8"),
      predecessor: {
        ...syntheticArtifact("tools/contract-standards/profile-v2.json", "9"),
        gitBlobSha1: "0c73cdf771ddcf0d46c43d52abf5b622507e8e1b",
        sha256: "sha256:9457d4bdc12f16b366d9c56a25a107103f5b2b64650de20f509f3ef96d0d4d01",
        sizeBytes: 3539,
      },
    },
  };
}

function syntheticArtifact(
  path: string,
  character: string,
): PlatformContractLockV3ArtifactIdentity {
  return {
    path,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: character.repeat(40),
    sha256: repeatedSha256(character),
    sizeBytes: 100,
  };
}

function worktreeArtifact(root: string, path: string): PlatformContractLockV3ArtifactIdentity {
  const bytes = readFileSync(resolve(root, path));
  return {
    path,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: createHash("sha1")
      .update(`blob ${bytes.byteLength}\0`, "utf8")
      .update(bytes)
      .digest("hex"),
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
  };
}

function repeatedSha256(character: string): `sha256:${string}` {
  return `sha256:${character.repeat(64)}`;
}

function listFiles(root: string): string[] {
  const walk = (directory: string): string[] =>
    readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
      const absolute = resolve(directory, entry.name);
      return entry.isDirectory() ? walk(absolute) : [absolute.slice(root.length + 1)];
    });
  return walk(root).toSorted();
}

function writeText(root: string, path: string, value: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  writeFileSync(resolve(root, path), value);
}

function git(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", args, {
    cwd: root,
    encoding: "utf8",
    env: {
      PATH: "/usr/bin:/bin",
      LANG: "C",
      LC_ALL: "C",
      GIT_CONFIG_NOSYSTEM: "1",
      GIT_CONFIG_GLOBAL: "/dev/null",
      GIT_AUTHOR_NAME: "Phase State Test",
      GIT_AUTHOR_EMAIL: "phase-state-test@example.invalid",
      GIT_COMMITTER_NAME: "Phase State Test",
      GIT_COMMITTER_EMAIL: "phase-state-test@example.invalid",
      GIT_AUTHOR_DATE: "2026-08-25T00:00:00Z",
      GIT_COMMITTER_DATE: "2026-08-25T00:00:00Z",
    },
  }).trim();
}

function commitAll(root: string, message: string): string {
  git(root, ["add", "-A"]);
  git(root, ["commit", "-q", "-m", message]);
  return git(root, ["rev-parse", "HEAD"]);
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}
