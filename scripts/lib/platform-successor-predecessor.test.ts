import { createHash } from "node:crypto";
import {
  cpSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertGeneratorSupplyV1GitLineageCurrent,
  assertGeneratorSupplyV1SemanticBindingsForTest,
  assertImmutableEvidenceManifest,
  assertImmutableFileMap,
  assertStablePredecessorReadMutationForTest,
  assertSuccessorPredecessorsImmutable,
  CONTRACT_CLOSURE_V1_IMMUTABLE_FILES,
  CONTRACT_CLOSURE_V2_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_GIT_LINEAGE,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
  type ImmutableEvidenceManifestSpec,
  type ImmutableFileRecord,
  SuccessorPredecessorError,
} from "./platform-successor-predecessor";

type EvidenceEntry = {
  readonly path: string;
  readonly bytes: string;
};

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) {
    rmSync(root, { force: true, recursive: true });
  }
});

function digest(bytes: string | Buffer): string {
  return createHash("sha256").update(bytes).digest("hex");
}

function createRoot(prefix: string): string {
  const root = mkdtempSync(resolve(tmpdir(), prefix));
  temporaryRoots.push(root);
  return root;
}

function write(root: string, path: string, bytes: string): void {
  mkdirSync(dirname(resolve(root, path)), { recursive: true });
  writeFileSync(resolve(root, path), bytes);
}

function writeManifest(
  root: string,
  entries: readonly EvidenceEntry[],
): ImmutableEvidenceManifestSpec {
  for (const entry of entries) write(root, entry.path, entry.bytes);
  const manifest = {
    algorithm: "test-evidence-v1",
    files: entries.map((entry) => ({
      path: entry.path,
      sha256: "sha256:" + digest(entry.bytes),
      sizeBytes: Buffer.byteLength(entry.bytes),
    })),
  };
  const manifestBytes = JSON.stringify(manifest, null, 2) + "\n";
  write(root, "manifest.json", manifestBytes);
  return {
    manifestPath: "manifest.json",
    manifestSha256: digest(manifestBytes),
    manifestSizeBytes: Buffer.byteLength(manifestBytes),
    algorithm: "test-evidence-v1",
    memberCount: entries.length,
    memberPathPrefix: "evidence/",
  };
}

describe("successor immutable predecessor authority", () => {
  it("verifies closure v1/v2, supply v1 outer bytes, all 39 evidence members, and Git lineage", () => {
    expect(() => assertSuccessorPredecessorsImmutable(repositoryRoot)).not.toThrow();
    expect(() => assertGeneratorSupplyV1GitLineageCurrent(repositoryRoot)).not.toThrow();
  });

  it("keeps file predecessor checks usable in an archive with no Git metadata", () => {
    const root = createRoot("successor-predecessor-archive-");
    const manifest = JSON.parse(
      readFileSync(
        resolve(repositoryRoot, GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath),
        "utf8",
      ),
    ) as { files: Array<{ path: string }> };
    const paths = new Set([
      ...CONTRACT_CLOSURE_V1_IMMUTABLE_FILES.map(({ path }) => path),
      ...CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.map(({ path }) => path),
      ...GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path }) => path),
      ...manifest.files.map(({ path }) => path),
    ]);
    for (const path of paths) {
      mkdirSync(dirname(resolve(root, path)), { recursive: true });
      cpSync(resolve(repositoryRoot, path), resolve(root, path));
    }
    expect(() => assertSuccessorPredecessorsImmutable(root)).not.toThrow();
    expect(() => assertGeneratorSupplyV1GitLineageCurrent(root)).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_GIT_MISMATCH",
      }),
    );
  });

  it("ignores hostile caller Git configuration and rejects a non-top-level root", () => {
    const root = createRoot("successor-predecessor-hostile-git-");
    const hostileConfig = resolve(root, "hostile.gitconfig");
    writeFileSync(hostileConfig, "[diff]\n\texternal = /usr/bin/false\n");
    const priorConfig = process.env.GIT_CONFIG_GLOBAL;
    const priorExternalDiff = process.env.GIT_EXTERNAL_DIFF;
    process.env.GIT_CONFIG_GLOBAL = hostileConfig;
    process.env.GIT_EXTERNAL_DIFF = "/usr/bin/false";
    try {
      expect(() => assertGeneratorSupplyV1GitLineageCurrent(repositoryRoot)).not.toThrow();
      expect(() =>
        assertGeneratorSupplyV1GitLineageCurrent(resolve(repositoryRoot, "scripts")),
      ).toThrowError(
        expect.objectContaining<Partial<SuccessorPredecessorError>>({
          code: "PREDECESSOR_GIT_MISMATCH",
        }),
      );
    } finally {
      if (priorConfig === undefined) delete process.env.GIT_CONFIG_GLOBAL;
      else process.env.GIT_CONFIG_GLOBAL = priorConfig;
      if (priorExternalDiff === undefined) delete process.env.GIT_EXTERNAL_DIFF;
      else process.env.GIT_EXTERNAL_DIFF = priorExternalDiff;
    }
  });

  it("binds exported profile identities and normalized review verdict to immutable bytes", () => {
    expect(() =>
      assertGeneratorSupplyV1SemanticBindingsForTest(
        repositoryRoot,
        {
          ...GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
          sourceDigest: "sha256:" + "0".repeat(64),
        },
        GENERATOR_SUPPLY_V1_GIT_LINEAGE.verdict,
      ),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_FILE_MISMATCH",
        path: "tools/generator-supply/v1/profile.json",
      }),
    );
    expect(() =>
      assertGeneratorSupplyV1SemanticBindingsForTest(
        repositoryRoot,
        GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
        "REQUEST_CHANGES_P0_0_P1_0_P2_0",
      ),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_FILE_MISMATCH",
        path: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewPath,
      }),
    );
  });

  it("fails closed when a fixed immutable file changes", () => {
    const root = createRoot("successor-predecessor-file-");
    const original = "fixed\n";
    write(root, "authority/fixed.txt", original);
    const files: readonly ImmutableFileRecord[] = [
      {
        path: "authority/fixed.txt",
        sha256: digest(original),
        sizeBytes: Buffer.byteLength(original),
      },
    ];
    expect(() => assertImmutableFileMap(root, files)).not.toThrow();
    write(root, "authority/fixed.txt", "drift\n");
    expect(() => assertImmutableFileMap(root, files)).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_FILE_MISMATCH",
        path: "authority/fixed.txt",
      }),
    );
  });

  it("verifies every manifest member instead of trusting only manifest bytes", () => {
    const root = createRoot("successor-predecessor-members-");
    const entries = [
      { path: "evidence/a.txt", bytes: "a\n" },
      { path: "evidence/b.txt", bytes: "b\n" },
    ] as const;
    const spec = writeManifest(root, entries);
    expect(() => assertImmutableEvidenceManifest(root, spec)).not.toThrow();
    write(root, "evidence/b.txt", "changed\n");
    expect(() => assertImmutableEvidenceManifest(root, spec)).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_MANIFEST_MISMATCH",
        path: "evidence/b.txt",
      }),
    );
  });

  it("rejects reordered manifest paths even when the changed manifest digest is supplied", () => {
    const root = createRoot("successor-predecessor-order-");
    const spec = writeManifest(root, [
      { path: "evidence/b.txt", bytes: "same\n" },
      { path: "evidence/a.txt", bytes: "same\n" },
    ]);
    expect(() => assertImmutableEvidenceManifest(root, spec)).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_MANIFEST_MISMATCH",
        path: "manifest.json#/files/1/path",
      }),
    );
  });

  it("rejects a symlinked member even when its target bytes match the manifest", () => {
    const root = createRoot("successor-predecessor-symlink-");
    const spec = writeManifest(root, [
      { path: "evidence/a.txt", bytes: "same\n" },
      { path: "evidence/b.txt", bytes: "same\n" },
    ]);
    unlinkSync(resolve(root, "evidence/b.txt"));
    symlinkSync("a.txt", resolve(root, "evidence/b.txt"));
    expect(() => assertImmutableEvidenceManifest(root, spec)).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_INVALID_PATH",
        path: "evidence/b.txt",
      }),
    );
  });

  it("fails closed when a regular file mutates during its stable descriptor read", () => {
    const root = createRoot("successor-predecessor-race-");
    write(root, "authority/fixed.txt", "before\n");
    expect(() =>
      assertStablePredecessorReadMutationForTest(root, "authority/fixed.txt", () => {
        write(root, "authority/fixed.txt", "after\n");
      }),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_FILE_MISMATCH",
        path: "authority/fixed.txt",
      }),
    );
  });

  it("rejects non-canonical and escaping repository-relative paths", () => {
    const root = createRoot("successor-predecessor-path-");
    write(root, "fixed.txt", "fixed\n");
    expect(() =>
      assertImmutableFileMap(root, [
        {
          path: "../fixed.txt",
          sha256: digest("fixed\n"),
          sizeBytes: Buffer.byteLength("fixed\n"),
        },
      ]),
    ).toThrowError(
      expect.objectContaining<Partial<SuccessorPredecessorError>>({
        code: "PREDECESSOR_INVALID_PATH",
        path: "../fixed.txt",
      }),
    );
  });
});
