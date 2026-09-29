import { execFileSync } from "node:child_process";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertGContractPhaseReviewVerdict,
  LEGACY_SUPPLY_REVIEW_VERDICT_MODE,
  G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
  G_CONTRACT_PHASE_SOURCE_PATH,
  G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
  G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
  readGContractPhaseRecordSource,
} from "./platform-g-contract-phase-record";
import {
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
  it("accepts only the fixed supply legacy zero-finding severity table", () => {
    expect(() =>
      assertGContractPhaseReviewVerdict(
        Buffer.from(supplyReviewText()),
        G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
        LEGACY_SUPPLY_REVIEW_VERDICT_MODE,
      ),
    ).not.toThrow();
    expect(() =>
      assertGContractPhaseReviewVerdict(
        Buffer.from(`${supplyReviewText()}\n| P0 | 0 |\n`),
        G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
        LEGACY_SUPPLY_REVIEW_VERDICT_MODE,
      ),
    ).toThrow();
    expect(() =>
      assertGContractPhaseReviewVerdict(
        Buffer.from(
          supplyReviewText().replace("| P1       |        0 |", "| P1       |        1 |"),
        ),
        G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
        LEGACY_SUPPLY_REVIEW_VERDICT_MODE,
      ),
    ).toThrow();
    expect(() =>
      assertGContractPhaseReviewVerdict(
        Buffer.from(reviewText()),
        G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
        LEGACY_SUPPLY_REVIEW_VERDICT_MODE,
      ),
    ).not.toThrow();
  });

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
    writeText(root, source.reviewSlots[0]!.reviewPath, options.reviewBody ?? supplyReviewText());
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

function supplyReviewText(): string {
  return "# Supply v3 independent review\n\n## Verdict\n\n`APPROVE`\n\n| Severity | Findings |\n| -------- | -------: |\n| P0       |        0 |\n| P1       |        0 |\n| P2       |        0 |\n";
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
