import { execFileSync } from "node:child_process";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildGContractPhaseBindingRegistry,
  buildGContractPhaseRecordModel,
  buildGContractPhaseReviewTuple,
  G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  G_CONTRACT_PHASE_EXACT17_PATHS,
  G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
  G_CONTRACT_PHASE_RECORD_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
  G_CONTRACT_PHASE_SOURCE_PATH,
  G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
  readGContractPhaseRecordSource,
  renderGContractPhaseRecord,
  serializeGContractPhaseJson,
  validateGContractPhaseReviewTuple,
  type CandidateGitBinding,
  type GContractPhaseReviewTuple,
  type ReviewGitBinding,
  type ReviewBinding,
} from "./platform-g-contract-phase-record";
import { captureGContractPhaseReviewBinding } from "./platform-g-contract-phase-state";
import {
  buildPlatformContractLockV3Assembled,
  buildPlatformContractLockV3PhaseBound,
  derivePlatformContractLockV3AssembledSnapshotIdentity,
  type PlatformContractLockV3ArtifactIdentity,
} from "./platform-contract-lock-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

describe("G-CONTRACT-P1 phase-record authority", () => {
  it("fixes the Gate-specific exact17 source and rejects unknown or reordered input", () => {
    const source = readGContractPhaseRecordSource(repositoryRoot);
    expect(source.gateId).toBe("G-CONTRACT");
    expect(source.record.status).toBe("IN_PROGRESS");
    expect(source.exactLateBoundPaths).toEqual(G_CONTRACT_PHASE_EXACT17_PATHS);
    expect(source.implementationBoundary.notGateClosure).toBe(true);
    expect(source.implementationBoundary.gateStatus).toBe("ALL_GATES_OPEN");

    const unknownRoot = sourceFixture();
    const unknown = readJson(unknownRoot, G_CONTRACT_PHASE_SOURCE_PATH);
    unknown.unexpected = true;
    writeJson(unknownRoot, G_CONTRACT_PHASE_SOURCE_PATH, unknown);
    expectCode(
      () => readGContractPhaseRecordSource(unknownRoot),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );

    const reorderedRoot = sourceFixture();
    const reordered = readJson(reorderedRoot, G_CONTRACT_PHASE_SOURCE_PATH);
    const paths = reordered.exactLateBoundPaths as unknown[];
    [paths[0], paths[1]] = [paths[1], paths[0]];
    writeJson(reorderedRoot, G_CONTRACT_PHASE_SOURCE_PATH, reordered);
    expectCode(
      () => readGContractPhaseRecordSource(reorderedRoot),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );
  });

  it("builds a schema-validated typed model and renders byte-deterministic non-Gate Markdown", () => {
    const fixture = modelFixture();
    const supply = captureGContractPhaseReviewBinding(
      fixture.root,
      "generator_supply_v3",
      fixture.supplyCandidate,
      fixture.supplyReview,
      "slice-e-assembler",
      "independent-supply-reviewer",
    );
    const model = buildGContractPhaseRecordModel(fixture.root, {
      projectionCommit: fixture.base,
      projectionTree: git(fixture.root, ["rev-parse", `${fixture.base}^{tree}`]),
      projectionArchiveSha256: sha("a"),
      supplyCandidate: supply.candidate,
      supplyReview: supply.review,
    });
    const first = renderGContractPhaseRecord(fixture.root, model);
    const second = renderGContractPhaseRecord(fixture.root, model);
    expect(first).toBe(second);
    expect(first).toContain("# Gate candidate record: `G-CONTRACT` / P1 / R5");
    expect(first).toContain("- Status: `IN PROGRESS`");
    expect(first).toContain("- Independent reviewer: `PENDING`");
    expect(first).toContain("`notGateClosure=true`; `gateStatus=ALL_GATES_OPEN`");
    expect(first).toContain("`REVIEW_BOUND_SATISFIED_CANDIDATE` / `0`");
    expect(model.criteria.map(({ status }) => status)).toEqual(
      Array.from({ length: 5 }, () => "SATISFIED_CANDIDATE"),
    );
    expect(model.missing).toEqual([]);
    expect(model.notGateClosure).toBe(true);
    expect(model.gateStatus).toBe("ALL_GATES_OPEN");
  });

  it("fails closed when the bound supply review bytes drift in the live worktree", () => {
    const fixture = modelFixture();
    const source = readGContractPhaseRecordSource(fixture.root);
    const supply = captureGContractPhaseReviewBinding(
      fixture.root,
      "generator_supply_v3",
      fixture.supplyCandidate,
      fixture.supplyReview,
      "slice-e-assembler",
      "independent-supply-reviewer",
    );
    writeText(
      fixture.root,
      source.dynamicAuthorities.supplyReviewPath,
      `${reviewText()}dirty working-tree byte\n`,
    );
    expectCode(
      () =>
        buildGContractPhaseRecordModel(fixture.root, {
          projectionCommit: fixture.base,
          projectionTree: git(fixture.root, ["rev-parse", `${fixture.base}^{tree}`]),
          projectionArchiveSha256: sha("a"),
          supplyCandidate: supply.candidate,
          supplyReview: supply.review,
        }),
      "G_CONTRACT_PHASE_FILE_INVALID",
    );
  });

  it("fails closed on current authority digest, status, or missing drift", () => {
    const digestFixture = modelFixture();
    const registryPath = "tools/contract-review-binding/v1/registry.json";
    const exact = readFileSync(resolve(digestFixture.root, registryPath), "utf8");
    writeText(digestFixture.root, registryPath, exact.replace(/\n$/u, " \n"));
    commitAll(digestFixture.root, "authority whitespace drift");
    expectCode(() => buildFixtureModel(digestFixture), "G_CONTRACT_PHASE_DIGEST_MISMATCH");

    const statusFixture = modelFixture();
    const statusRegistry = readJson(statusFixture.root, registryPath);
    (statusRegistry.effectiveCandidate as Record<string, unknown>).status = "REVIEW_PENDING";
    writeJson(statusFixture.root, registryPath, statusRegistry);
    commitAll(statusFixture.root, "authority status drift");
    expectCode(() => buildFixtureModel(statusFixture), "G_CONTRACT_PHASE_IDENTITY_MISMATCH");

    const missingFixture = modelFixture();
    const missingRegistry = readJson(missingFixture.root, registryPath);
    (missingRegistry.effectiveCandidate as Record<string, unknown>).missing = [
      "remaining-generator-supply-chain-review",
    ];
    writeJson(missingFixture.root, registryPath, missingRegistry);
    commitAll(missingFixture.root, "authority missing drift");
    expectCode(() => buildFixtureModel(missingFixture), "G_CONTRACT_PHASE_IDENTITY_MISMATCH");
  });

  it("derives the lock identity from a valid ASSEMBLED v3 document and rejects drift or PHASE_BOUND", () => {
    const valid = modelFixture();
    const model = buildFixtureModel(valid);
    expect(model.assembledLock).toMatchObject({
      state: "ASSEMBLED",
      formatVersion: "cloud-agents-platform-contract-generation-lock/v3",
    });

    const drifted = modelFixture("DRIFTED");
    expectCode(() => buildFixtureModel(drifted), "G_CONTRACT_PHASE_FILE_INVALID");

    const phaseBound = modelFixture("PHASE_BOUND");
    expectCode(() => buildFixtureModel(phaseBound), "G_CONTRACT_PHASE_IDENTITY_MISMATCH");
  });

  it.each([
    ["extra-path", "extra-path"],
    ["existing-path", "existing-path"],
    ["reject-verdict", "reject-verdict"],
    ["ambiguous-verdict", "ambiguous-verdict"],
  ] as const)("independently rejects a mutated supply review child (%s)", (_label, mode) => {
    const fixture = modelFixture();
    expectCode(
      () => buildBoundaryMutationModel(fixture, mode),
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
    );
  });

  it("keeps the two lineages ordered, rejects self-review, and derives only a pre-terminal registry", () => {
    const supply = fakeBinding("generator_supply_v3", "1", "2");
    const r5 = fakeBinding("g_contract_r5", "3", "4");
    const tuple = buildGContractPhaseReviewTuple(repositoryRoot, [supply, r5]);
    const registry = buildGContractPhaseBindingRegistry(repositoryRoot, tuple);
    expect(tuple.reviews.map(({ subject }) => subject)).toEqual([
      "generator_supply_v3",
      "g_contract_r5",
    ]);
    expect(registry.state).toBe("PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT");
    expect(registry.terminalReview).toEqual({
      path: "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
      state: "ABSENT",
    });
    expect(JSON.stringify(registry)).not.toContain("REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE");

    const selfReviewed = structuredClone(tuple) as unknown as {
      reviews: Array<{ candidate: { actorId: string }; review: { reviewerId: string } }>;
    };
    selfReviewed.reviews[0]!.review.reviewerId = selfReviewed.reviews[0]!.candidate.actorId;
    expectCode(
      () =>
        validateGContractPhaseReviewTuple(
          repositoryRoot,
          selfReviewed as unknown as GContractPhaseReviewTuple,
        ),
      "G_CONTRACT_PHASE_SELF_REVIEW",
    );

    const reordered = structuredClone(tuple) as unknown as { reviews: ReviewBinding[] };
    reordered.reviews.reverse();
    expectCode(
      () =>
        validateGContractPhaseReviewTuple(
          repositoryRoot,
          reordered as unknown as GContractPhaseReviewTuple,
        ),
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
    );
  });
});

function sourceFixture(): string {
  const root = mkdtempSync(join(tmpdir(), "g-contract-phase-source-"));
  temporaryRoots.push(root);
  for (const path of authorityFiles()) copyFromRepository(root, path);
  return root;
}

type ModelFixture = {
  root: string;
  base: string;
  supplyCandidate: string;
  supplyReview: string;
};

function modelFixture(
  lockState: "ASSEMBLED" | "DRIFTED" | "PHASE_BOUND" = "ASSEMBLED",
): ModelFixture {
  const root = sourceFixture();
  git(root, ["init", "-q"]);
  const source = readGContractPhaseRecordSource(root);
  for (const entry of [...source.prerequisites, ...source.historicalRecords]) {
    copyFromRepository(root, entry.path);
  }
  copyFromRepository(root, source.criteriaAuthority.path);
  for (const path of source.currentSourceInputPaths) copyFromRepository(root, path);
  writeText(root, "base.txt", "base\n");
  const base = commitAll(root, "base authority");

  writeJson(root, source.dynamicAuthorities.projectionReceiptPath, { state: "PROJECTED" });
  writeJson(root, source.dynamicAuthorities.supplyManifestPath, { files: [] });
  writeJson(root, source.dynamicAuthorities.supplyProfilePath, {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v3",
    notGateClosure: true,
  });
  const assembled = buildPlatformContractLockV3Assembled(lockAuthority());
  writeJson(root, source.dynamicAuthorities.assembledLockPath, assembled);
  let supplyCandidate = commitAll(root, "assembled supply candidate");
  if (lockState === "DRIFTED") {
    const drifted = structuredClone(assembled) as unknown as Record<string, unknown>;
    drifted.formatVersion = "cloud-agents-platform-contract-generation-lock/v999";
    writeJson(root, source.dynamicAuthorities.assembledLockPath, drifted);
    supplyCandidate = commitAll(root, "drifted supply lock candidate");
  } else if (lockState === "PHASE_BOUND") {
    const snapshot = derivePlatformContractLockV3AssembledSnapshotIdentity(assembled, {
      commitSha1: supplyCandidate,
      treeSha1: git(root, ["rev-parse", `${supplyCandidate}^{tree}`]),
    });
    const phaseBound = buildPlatformContractLockV3PhaseBound(assembled, snapshot, {
      state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
      artifacts: [
        phaseArtifact("R5_CANDIDATE", G_CONTRACT_PHASE_RECORD_PATH, "1"),
        phaseArtifact(
          "R5_REVIEW",
          "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md",
          "2",
        ),
        phaseArtifact(
          "REVIEW_TUPLE",
          "tools/gate-phase-record/g-contract-p1/v1/review-tuple.json",
          "3",
        ),
        phaseArtifact(
          "BINDING_REGISTRY",
          "tools/gate-phase-record/g-contract-p1/v1/registry.json",
          "4",
        ),
      ],
    });
    writeJson(root, source.dynamicAuthorities.assembledLockPath, phaseBound);
    supplyCandidate = commitAll(root, "phase-bound supply lock candidate");
  }
  writeText(
    root,
    source.dynamicAuthorities.supplyReviewPath,
    "# Supply v3 independent review\n\n## Verdict\n\n`APPROVE - P0=0 / P1=0 / P2=0`\n",
  );
  const supplyReview = commitAll(root, "supply review");
  return { root, base, supplyCandidate, supplyReview };
}

function buildFixtureModel(fixture: ModelFixture) {
  const supply = captureGContractPhaseReviewBinding(
    fixture.root,
    "generator_supply_v3",
    fixture.supplyCandidate,
    fixture.supplyReview,
    "slice-e-assembler",
    "independent-supply-reviewer",
  );
  return buildGContractPhaseRecordModel(fixture.root, {
    projectionCommit: fixture.base,
    projectionTree: git(fixture.root, ["rev-parse", `${fixture.base}^{tree}`]),
    projectionArchiveSha256: sha("a"),
    supplyCandidate: supply.candidate,
    supplyReview: supply.review,
  });
}

type BoundaryMutation = "extra-path" | "existing-path" | "reject-verdict" | "ambiguous-verdict";

function buildBoundaryMutationModel(fixture: ModelFixture, mutation: BoundaryMutation) {
  const source = readGContractPhaseRecordSource(fixture.root);
  const captured = captureGContractPhaseReviewBinding(
    fixture.root,
    "generator_supply_v3",
    fixture.supplyCandidate,
    fixture.supplyReview,
    "slice-e-assembler",
    "independent-supply-reviewer",
  );
  git(fixture.root, ["checkout", "-q", "--detach", fixture.supplyCandidate]);
  let candidate: CandidateGitBinding = captured.candidate;
  let review: ReviewGitBinding = captured.review;
  const reviewPath = source.dynamicAuthorities.supplyReviewPath;
  if (mutation === "existing-path") {
    writeText(fixture.root, reviewPath, "draft review\n");
    const mutatedCandidate = commitAll(fixture.root, "mutated candidate review path");
    writeText(fixture.root, reviewPath, reviewText());
    const mutatedReview = commitAll(fixture.root, "mutated review child");
    candidate = { ...candidate, commit: mutatedCandidate };
    review = { ...review, commit: mutatedReview };
  } else {
    const bytes =
      mutation === "reject-verdict"
        ? "# Supply v3 independent review\n\n## Verdict\n\n`REQUEST_CHANGES - P0=0 / P1=1 / P2=0`\n"
        : mutation === "ambiguous-verdict"
          ? "# Supply v3 independent review\n\n## Verdict\n\n`APPROVE - P0=0 / P1=0 / P2=0`\n\n## Verdict\n\n`APPROVE - P0=0 / P1=0 / P2=0`\n"
          : reviewText();
    writeText(fixture.root, reviewPath, bytes);
    if (mutation === "extra-path") writeText(fixture.root, "unexpected-review-extra.txt", "x\n");
    const mutatedReview = commitAll(fixture.root, `mutated ${mutation}`);
    review = { ...review, commit: mutatedReview };
  }
  return buildGContractPhaseRecordModel(fixture.root, {
    projectionCommit: fixture.base,
    projectionTree: git(fixture.root, ["rev-parse", `${fixture.base}^{tree}`]),
    projectionArchiveSha256: sha("a"),
    supplyCandidate: candidate,
    supplyReview: review,
  });
}

function reviewText(): string {
  return "# Supply v3 independent review\n\n## Verdict\n\n`APPROVE - P0=0 / P1=0 / P2=0`\n";
}

function lockAuthority() {
  return {
    generatorSupply: {
      formatVersion: "cloud-agents-generator-supply-profile-registry/v3" as const,
      profileId: "cloud-agents/generator-supply-profile/v3" as const,
      profileDigest: sha("1"),
      registryDigest: sha("2"),
      candidateManifestSha256: sha("3"),
      outputFiles: 49 as const,
      evidenceManifest: lockArtifact("tools/generator-supply/v3/evidence-manifest.json", "4"),
      profile: lockArtifact("tools/generator-supply/v3/profile.json", "5"),
    },
    projection: {
      algorithm: "exact-ordered-paths-v1" as const,
      exclusionCount: 17 as const,
      exclusionsDigest: sha("6"),
      receipt: lockArtifact("tools/generator-supply/v3/evidence/replay/projection.json", "7"),
    },
  };
}

function lockArtifact(path: string, character: string): PlatformContractLockV3ArtifactIdentity {
  return {
    path,
    fileType: "REGULAR_FILE",
    gitMode: "100644",
    gitBlobSha1: character.repeat(40),
    sha256: sha(character),
    sizeBytes: 1,
  };
}

function phaseArtifact(
  role: "R5_CANDIDATE" | "R5_REVIEW" | "REVIEW_TUPLE" | "BINDING_REGISTRY",
  path: string,
  character: string,
) {
  return { role, artifact: lockArtifact(path, character) };
}

function fakeBinding(
  subject: "generator_supply_v3" | "g_contract_r5",
  c: string,
  r: string,
): ReviewBinding {
  const supply = subject === "generator_supply_v3";
  return {
    subject,
    candidateSubjectPath: supply
      ? "tools/generator-supply/v3/profile.json"
      : G_CONTRACT_PHASE_RECORD_PATH,
    candidate: {
      actorId: `candidate-${c}`,
      commit: c.repeat(40),
      tree: "a".repeat(40),
      parent: "b".repeat(40),
      diffSha256: sha("c"),
    },
    review: {
      reviewerId: `reviewer-${r}`,
      commit: r.repeat(40),
      tree: "d".repeat(40),
      parent: c.repeat(40),
      path: supply
        ? "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md"
        : "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md",
      gitBlob: "e".repeat(40),
      sha256: sha("d"),
      sizeBytes: 1,
      mode: "100644",
      diffSha256: sha("e"),
      verdict: "APPROVE_P0_0_P1_0_P2_0",
      findings: { p0: 0, p1: 0, p2: 0 },
    },
  };
}

function authorityFiles(): string[] {
  return [
    G_CONTRACT_PHASE_SOURCE_PATH,
    G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
    G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
    G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
    G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
    "tools/contract-review-binding/v1/registry.json",
  ];
}

function copyFromRepository(root: string, path: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  cpSync(resolve(repositoryRoot, path), resolve(root, path));
}

function writeText(root: string, path: string, value: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  writeFileSync(resolve(root, path), value);
}

function writeJson(root: string, path: string, value: unknown): void {
  writeText(root, path, serializeGContractPhaseJson(value));
}

function readJson(root: string, path: string): Record<string, unknown> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, unknown>;
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
      GIT_AUTHOR_NAME: "Phase Contract Test",
      GIT_AUTHOR_EMAIL: "phase-contract-test@example.invalid",
      GIT_COMMITTER_NAME: "Phase Contract Test",
      GIT_COMMITTER_EMAIL: "phase-contract-test@example.invalid",
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

function sha(character: string): `sha256:${string}` {
  return `sha256:${character.repeat(64)}`;
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}
