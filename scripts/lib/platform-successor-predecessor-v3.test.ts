import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertSuccessorV3PredecessorsCurrent,
  assertSuccessorV3HistoricalGenerationLockV2ForTest,
  assertSuccessorV3SourceForTest,
  assertSuccessorV3StableFileMapForTest,
  loadAndAssertSuccessorV3Source,
  type SuccessorV3PredecessorError,
} from "./platform-successor-predecessor-v3";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const temporaryRoot of temporaryRoots.splice(0)) {
    rmSync(temporaryRoot, { force: true, recursive: true });
  }
});

function createRoot(): string {
  const temporaryRoot = mkdtempSync(resolve(tmpdir(), "successor-v3-predecessor-"));
  temporaryRoots.push(temporaryRoot);
  return temporaryRoot;
}

function write(root: string, repositoryPath: string, bytes: string): void {
  mkdirSync(dirname(resolve(root, repositoryPath)), { recursive: true });
  writeFileSync(resolve(root, repositoryPath), bytes);
}

function sha256(bytes: string): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function gitBlob(bytes: string): string {
  return createHash("sha1")
    .update(`blob ${Buffer.byteLength(bytes)}\0`)
    .update(bytes)
    .digest("hex");
}

function cloneSource(): Record<string, any> {
  return JSON.parse(
    readFileSync(resolve(repositoryRoot, "tools/generator-supply/v3/source.json"), "utf8"),
  ) as Record<string, any>;
}

describe("successor v3 immutable predecessor fence", () => {
  it("reproduces every direct predecessor, both complete manifests, exact49, and fixed Git chain", () => {
    const source = loadAndAssertSuccessorV3Source(repositoryRoot);
    expect(source.predecessorClosure.groups).toHaveLength(8);
    expect(
      source.predecessorClosure.evidenceManifests.map(({ memberCount }) => memberCount),
    ).toEqual([39, 8]);
    expect(source.replayContract.coreGeneratorOutputs).toHaveLength(49);
    expect(source.replayContract.projectionExclusions).toHaveLength(17);
    expect(source.predecessorClosure.gitChain.map(({ commit }) => commit)).toEqual([
      "1ba7eda5ad6241ad8a065408d787e73cd7013ce0",
      "d7c7468a72facc091b8a42be54d5af5c6a5785c4",
      "a595bd93ceee9d352645b9be66db92517fffb092",
      "16275f6cbf390c343a9ac00f9193e75eaad0094e",
    ]);
    expect(() => assertSuccessorV3PredecessorsCurrent(repositoryRoot)).not.toThrow();
  });

  it("keeps the fixed post-H v2 lock valid after the live path becomes ASSEMBLED or PHASE_BOUND", () => {
    const root = createRoot();
    execFileSync("/usr/bin/git", ["clone", "--quiet", "--no-hardlinks", repositoryRoot, root], {
      env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" },
    });
    execFileSync(
      "/usr/bin/git",
      ["checkout", "--quiet", "16275f6cbf390c343a9ac00f9193e75eaad0094e"],
      {
        cwd: root,
        env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" },
      },
    );
    write(
      root,
      "tools/generator-supply/v3/source.json",
      readFileSync(resolve(repositoryRoot, "tools/generator-supply/v3/source.json"), "utf8"),
    );
    for (const path of [
      "scripts/replay-platform-generators-isolated-v3.sh",
      "scripts/replay-platform-generators-v3.ts",
      "scripts/lib/generator-replay-path-authority.ts",
      "scripts/lib/inspect-generator-replay-archive.py",
    ]) {
      mkdirSync(dirname(resolve(root, path)), { recursive: true });
      copyFileSync(resolve(repositoryRoot, path), resolve(root, path));
    }
    for (const state of ["ASSEMBLED", "PHASE_BOUND"]) {
      write(
        root,
        "contracts/generation.lock.json",
        JSON.stringify({
          formatVersion: "cloud-agents-platform-contract-generation-lock/v3",
          lockVersion: 3,
          state,
          lockDigest: `sha256:${"0".repeat(64)}`,
        }),
      );
      expect(() => assertSuccessorV3PredecessorsCurrent(root)).not.toThrow();
    }
  });

  it("fails closed when the historical v2 blob, status, or digest drifts", () => {
    const source = cloneSource();
    const record = source.predecessorClosure.groups[7].files[0];
    const originalBytes = execFileSync("/usr/bin/git", [
      "show",
      "16275f6cbf390c343a9ac00f9193e75eaad0094e:contracts/generation.lock.json",
    ]).toString("utf8");
    const originalEntry = { mode: "100644", type: "blob", object: record.gitBlob };
    expect(() =>
      assertSuccessorV3HistoricalGenerationLockV2ForTest(
        record,
        originalEntry,
        Buffer.from(originalBytes),
      ),
    ).not.toThrow();

    const blobDrift = Buffer.from(originalBytes + "\n");
    expect(() =>
      assertSuccessorV3HistoricalGenerationLockV2ForTest(record, originalEntry, blobDrift),
    ).toThrow();

    for (const field of ["status", "lockDigest"] as const) {
      const document = JSON.parse(originalBytes) as Record<string, unknown>;
      document[field] =
        field === "status" ? "SUCCESSOR_ASSEMBLED_PRE_REVIEW" : `sha256:${"f".repeat(64)}`;
      const bytes = JSON.stringify(document);
      const adjustedRecord = {
        ...record,
        gitBlob: gitBlob(bytes),
        sha256: sha256(bytes),
        sizeBytes: Buffer.byteLength(bytes),
      };
      expect(() =>
        assertSuccessorV3HistoricalGenerationLockV2ForTest(
          adjustedRecord,
          { mode: "100644", type: "blob", object: adjustedRecord.gitBlob },
          Buffer.from(bytes),
        ),
      ).toThrow(/format, status, or digest drifted/u);
    }
  });

  it("keeps both strict schema documents valid JSON", () => {
    for (const repositoryPath of [
      "tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json",
      "tools/generator-supply/v3/generator-supply-profile-v3.schema.json",
    ]) {
      expect(() =>
        JSON.parse(readFileSync(resolve(repositoryRoot, repositoryPath), "utf8")),
      ).not.toThrow();
    }
  });

  it("rejects unknown fields and every authoritative array reorder", () => {
    const mutations = [
      () => {
        const source = cloneSource();
        source.unknown = true;
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.groups.reverse();
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.groups[0].files.reverse();
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.evidenceManifests.reverse();
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.gitChain.reverse();
        return source;
      },
      () => {
        const source = cloneSource();
        source.replayContract.coreGeneratorOutputs.reverse();
        return source;
      },
      () => {
        const source = cloneSource();
        source.replayContract.projectionExclusions.reverse();
        return source;
      },
    ];
    for (const mutate of mutations) {
      expect(() => assertSuccessorV3SourceForTest(mutate())).toThrow();
    }
  });

  it("rejects wildcard, alias, duplicate, and altered predecessor records", () => {
    const mutations = [
      () => {
        const source = cloneSource();
        source.replayContract.projectionExclusions[3] =
          "tools/generator-supply/v3/evidence/replay/*.json";
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.groups[0].files[0].path =
          "contracts/generated/../generated/platform/v1alpha1/contract-closure-profile-v1.json";
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.groups[0].files[1] = source.predecessorClosure.groups[0].files[0];
        return source;
      },
      () => {
        const source = cloneSource();
        source.predecessorClosure.groups[0].files[0].sha256 = "0".repeat(63);
        return source;
      },
    ];
    for (const mutate of mutations) {
      expect(() => assertSuccessorV3SourceForTest(mutate())).toThrow();
    }
  });

  it("rejects a symlink even when target bytes match", () => {
    const root = createRoot();
    const bytes = "same\n";
    write(root, "authority/real.txt", bytes);
    write(root, "authority/link.txt", bytes);
    unlinkSync(resolve(root, "authority/link.txt"));
    symlinkSync("real.txt", resolve(root, "authority/link.txt"));
    expect(() =>
      assertSuccessorV3StableFileMapForTest(root, [
        { path: "authority/link.txt", sha256: sha256(bytes), sizeBytes: Buffer.byteLength(bytes) },
      ]),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorV3PredecessorError>>({
        code: "SUCCESSOR_V3_PATH_INVALID",
      }),
    );
  });

  it("rejects an ABA replacement after an early shared-snapshot read", () => {
    const root = createRoot();
    const first = "first\n";
    const second = "second\n";
    write(root, "authority/a.txt", first);
    write(root, "authority/b.txt", second);
    expect(() =>
      assertSuccessorV3StableFileMapForTest(
        root,
        [
          { path: "authority/a.txt", sha256: sha256(first), sizeBytes: Buffer.byteLength(first) },
          { path: "authority/b.txt", sha256: sha256(second), sizeBytes: Buffer.byteLength(second) },
        ],
        {
          afterPath: "authority/a.txt",
          fired: false,
          mutate: () => {
            const replacement = resolve(root, "authority/.replacement");
            writeFileSync(replacement, first);
            renameSync(replacement, resolve(root, "authority/a.txt"));
          },
        },
      ),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorV3PredecessorError>>({
        code: "SUCCESSOR_V3_FILE_MISMATCH",
        path: "authority/a.txt",
      }),
    );
  });
});
