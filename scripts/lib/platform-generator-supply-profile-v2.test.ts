import { createHash } from "node:crypto";
import {
  constants,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import { canonicalizeJson } from "./platform-json-semantics";
import { buildGeneratorSupplyReplayV2TestFixture } from "./platform-generator-supply-replay-v2";
import {
  assertGeneratorSupplyV2CurrentSnapshotMutationForTest,
  assertGeneratorSupplyV2RegistryCurrent,
  assertGeneratorSupplyV2RegistrySemantics,
  assertStableGeneratorSupplyV2ReadMutationForTest,
  buildGeneratorSupplyV2TestSource,
  GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  GENERATOR_SUPPLY_V2_SOURCE_PATH,
  GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
  inspectGeneratorSupplyV2AuthorityState,
  validateGeneratorSupplyV2Source,
  type GeneratorSupplyV2Registry,
  type GeneratorSupplyV2Source,
} from "./platform-generator-supply-profile-v2";
import {
  SUCCESSOR_ASSEMBLY_PATHS,
  SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS,
  SUCCESSOR_PROJECTION_EXCLUSIONS,
  SUCCESSOR_REPLAY_RECEIPT_PATHS,
} from "./platform-successor-dag";
import {
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_GIT_LINEAGE,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
} from "./platform-successor-predecessor";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { recursive: true, force: true });
});

function createRoot(options: { readonly source?: boolean; readonly predecessor?: boolean } = {}): {
  readonly root: string;
  readonly source: GeneratorSupplyV2Source;
} {
  const root = mkdtempSync(join(tmpdir(), "generator-supply-v2-"));
  temporaryRoots.push(root);
  for (const schema of [
    GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
    GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  ]) {
    copyFromRepository(root, schema);
  }
  if (options.predecessor) materializeV1Predecessor(root);
  const source = buildGeneratorSupplyV2TestSource();
  if (options.source) writeJson(root, GENERATOR_SUPPLY_V2_SOURCE_PATH, source);
  return { root, source };
}

function materializeV1Predecessor(root: string): void {
  const paths = new Set(GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map((record) => record.path));
  for (const authority of Object.values(GENERATOR_SUPPLY_V2_REPLAY_CONTRACT.authorityFiles)) {
    paths.add(authority.path);
  }
  const manifest = JSON.parse(
    readFileSync(
      resolve(repositoryRoot, GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath),
      "utf8",
    ),
  ) as { files: Array<{ path: string }> };
  for (const member of manifest.files) paths.add(member.path);
  for (const path of paths) copyFromRepository(root, path);
}

function copyFromRepository(root: string, path: string): void {
  const source = resolve(repositoryRoot, path);
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  copyFileSync(source, target, constants.COPYFILE_FICLONE);
}

function writeReceipts(root: string): void {
  const fixture = buildGeneratorSupplyReplayV2TestFixture(
    root,
    GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  );
  for (const path of SUCCESSOR_REPLAY_RECEIPT_PATHS) {
    writeJson(root, path, fixture.receipts[path]);
  }
}

function fileRecord(
  root: string,
  path: string,
): {
  path: string;
  sha256: string;
  sizeBytes: number;
} {
  const bytes = readFileSync(resolve(root, path));
  return {
    path,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
  };
}

function assembleTestRegistry(
  root: string,
  source: GeneratorSupplyV2Source,
): GeneratorSupplyV2Registry {
  const receipts = SUCCESSOR_REPLAY_RECEIPT_PATHS.map((path) => fileRecord(root, path));
  const evidenceManifest = {
    algorithm: "sorted-path-nul-sha256-nul-size-v1",
    files: receipts,
  };
  writeJson(root, GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH, evidenceManifest);
  const profileSpec = source.declaredProfile;
  const sourceDigest = domainDigest("cloud-agents/generator-supply/source/v2", source);
  const artifactSetDigest = domainDigest("cloud-agents/generator-supply/artifact-set/v2", {
    predecessor: source.predecessor,
    inheritance: source.inheritance,
    receipts,
  });
  const evidenceManifestDigest = domainDigest(
    "cloud-agents/generator-supply/evidence-manifest/v2",
    evidenceManifest,
  );
  const evidence = {
    state: "ASSEMBLED_LATE_BOUND",
    inheritance: source.inheritance,
    receipts,
    evidenceManifest,
  };
  const body = {
    formatVersion: "cloud-agents-generator-supply-profile-registry/v2",
    registryId: "cloud-agents/generator-supply-profile",
    predecessor: source.predecessor,
    sourceDigest,
    artifactSetDigest,
    evidenceManifestDigest,
    profile: {
      profileDigest: domainDigest("cloud-agents/generator-supply/profile/v2", {
        sourceDigest,
        artifactSetDigest,
        evidenceManifestDigest,
        spec: profileSpec,
        evidence,
      }),
      spec: profileSpec,
      evidence,
    },
  };
  return {
    ...body,
    registryDigest: domainDigest("cloud-agents/generator-supply/registry/v2", body),
  } as GeneratorSupplyV2Registry;
}

function writeJson(root: string, path: string, value: unknown): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, `${JSON.stringify(value, null, 2)}\n`);
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function expectCode(action: () => unknown, code: string): void {
  expect(action).toThrowError(expect.objectContaining({ code }));
}

describe("generator-supply v2 typed pre-replay authority", () => {
  it("classifies schema-only and rejects any late artifact without a source", () => {
    const fixture = createRoot();
    expect(inspectGeneratorSupplyV2AuthorityState(fixture.root)).toBe("SCHEMA_ONLY");
    writeJson(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0], {});
    expectCode(
      () => inspectGeneratorSupplyV2AuthorityState(fixture.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );
  });

  it("binds the exact v1 predecessor, material/security inheritance, and DAG receipts", () => {
    const source = buildGeneratorSupplyV2TestSource();
    expect(source.predecessor).toEqual({
      profileId: "cloud-agents/generator-supply-profile/v1",
      predecessorMutation: "forbidden",
      outerFiles: GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map((record) => ({ ...record })),
      evidenceManifestPolicy: { ...GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST },
      profileIdentities: { ...GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES },
      fixedLineage: {
        ...GENERATOR_SUPPLY_V1_GIT_LINEAGE,
      },
      projection: {
        treeSha: "4a70fb8b1e18801f4f02a753668ffe91b63b6275",
        archiveSha256: "36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478",
        archiveSizeBytes: 46_008_320,
        receiptPath: "tools/generator-supply/v1/evidence/replay/projection.json",
        receiptSha256: "1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4",
        receiptSizeBytes: 1_708,
      },
    });
    expect(source.replayEvidence).toEqual({
      state: "DECLARED_PRE_REPLAY",
      authority: "EXTERNAL_LATE_BOUND",
      receiptPaths: [...SUCCESSOR_REPLAY_RECEIPT_PATHS],
    });
    expect(source.replayContract).toEqual(GENERATOR_SUPPLY_V2_REPLAY_CONTRACT);
    expect(source.declaredProfile).toMatchObject({
      profileId: "cloud-agents/generator-supply-profile/v2",
      status: "REPLAY_VERIFIED_REVIEW_PENDING",
      notGateClosure: true,
      boundaries: { gate: "ALL_GATES_OPEN", bootstrapDiscovery: "FORBIDDEN" },
    });
    expect(SUCCESSOR_REPLAY_RECEIPT_PATHS).toHaveLength(8);
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain(GENERATOR_SUPPLY_V2_SOURCE_PATH);
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain(GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH);
    expect(SUCCESSOR_PROJECTION_EXCLUSIONS).not.toContain(GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH);
    expect(SUCCESSOR_PRE_REPLAY_AUTHORITY_PATHS).toEqual(
      expect.arrayContaining([
        GENERATOR_SUPPLY_V2_SOURCE_PATH,
        GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
        GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
      ]),
    );
  });

  it("accepts source plus absent late outputs only as DECLARED_PRE_REPLAY", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    expect(inspectGeneratorSupplyV2AuthorityState(fixture.root)).toBe("DECLARED_PRE_REPLAY");
    expect(() => validateGeneratorSupplyV2Source(fixture.root, fixture.source)).not.toThrow();
  });

  it("rejects source predecessor, inheritance, status, and boundary drift", () => {
    const fixture = createRoot({ predecessor: true });
    for (const [mutate, expectedCode] of [
      [
        (source: Record<string, any>) => (source.predecessor.outerFiles[0].sha256 = "0".repeat(64)),
        "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      ],
      [
        (source: Record<string, any>) =>
          (source.predecessor.evidenceManifestPolicy.memberCount = 38),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
      [
        (source: Record<string, any>) =>
          (source.predecessor.profileIdentities.profileDigest = `sha256:${"0".repeat(64)}`),
        "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      ],
      [
        (source: Record<string, any>) =>
          (source.predecessor.fixedLineage.candidateTree = "0".repeat(40)),
        "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      ],
      [
        (source: Record<string, any>) => (source.predecessor.projection.receiptSizeBytes = 1_707),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
      [
        (source: Record<string, any>) => (source.inheritance.security = "UNBOUND"),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
      [
        (source: Record<string, any>) =>
          (source.replayContract.projectionExclusions[0] = "contracts/other.lock.json"),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
      [
        (source: Record<string, any>) =>
          (source.replayContract.authorityFiles.runner.sha256 = "0".repeat(64)),
        "GENERATOR_SUPPLY_V2_SOURCE_MISMATCH",
      ],
      [
        (source: Record<string, any>) => (source.declaredProfile.status = "APPROVED"),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
      [
        (source: Record<string, any>) => (source.declaredProfile.boundaries.gate = "CLOSED"),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      ],
    ] as const) {
      const source = clone(fixture.source) as Record<string, any>;
      mutate(source);
      expectCode(
        () => validateGeneratorSupplyV2Source(fixture.root, source as GeneratorSupplyV2Source),
        expectedCode,
      );
    }
  });

  it("fails closed on partial receipts and accepts the exact complete eight", () => {
    const partial = createRoot({ source: true, predecessor: true });
    writeJson(partial.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0], {});
    expectCode(
      () => inspectGeneratorSupplyV2AuthorityState(partial.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );

    const complete = createRoot({ source: true, predecessor: true });
    writeReceipts(complete.root);
    expect(inspectGeneratorSupplyV2AuthorityState(complete.root)).toBe(
      "REPLAY_RECEIPTS_PRESENT_UNVERIFIED",
    );
  });

  it("fails closed when manifest/profile assembly is partial", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    writeReceipts(fixture.root);
    writeJson(fixture.root, SUCCESSOR_ASSEMBLY_PATHS[0], {
      algorithm: "sorted-path-nul-sha256-nul-size-v1",
      files: [],
    });
    expectCode(
      () => inspectGeneratorSupplyV2AuthorityState(fixture.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );
  });

  it("rejects symlinked late-bound paths and a file mutated during a stable read", () => {
    const ancestor = createRoot({ source: true, predecessor: true });
    const outside = mkdtempSync(join(tmpdir(), "generator-supply-v2-outside-"));
    temporaryRoots.push(outside);
    symlinkSync(outside, resolve(ancestor.root, "tools/generator-supply/v2/evidence"), "dir");
    expectCode(
      () => inspectGeneratorSupplyV2AuthorityState(ancestor.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );

    const final = createRoot({ source: true, predecessor: true });
    const finalReceipt = resolve(final.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0]);
    mkdirSync(dirname(finalReceipt), { recursive: true });
    symlinkSync(resolve(final.root, GENERATOR_SUPPLY_V2_SOURCE_PATH), finalReceipt);
    expectCode(
      () => inspectGeneratorSupplyV2AuthorityState(final.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );

    const mutation = createRoot({ source: true, predecessor: true });
    expectCode(
      () =>
        assertStableGeneratorSupplyV2ReadMutationForTest(
          mutation.root,
          GENERATOR_SUPPLY_V2_SOURCE_PATH,
          () => {
            const path = resolve(mutation.root, GENERATOR_SUPPLY_V2_SOURCE_PATH);
            writeFileSync(path, `${readFileSync(path, "utf8")} `);
          },
        ),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );
  });

  it("validates the complete output spec, evidence, predecessor, digests, and boundaries", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    writeReceipts(fixture.root);
    const registry = assembleTestRegistry(fixture.root, fixture.source);
    writeJson(fixture.root, GENERATOR_SUPPLY_V2_OUTPUT_PATH, registry);
    expect(() => assertGeneratorSupplyV2RegistrySemantics(fixture.root, registry)).not.toThrow();
    const current = assertGeneratorSupplyV2RegistryCurrent(fixture.root, registry);
    expect(current.fileSha256).toBe(
      fileRecord(fixture.root, GENERATOR_SUPPLY_V2_OUTPUT_PATH).sha256,
    );
    expect(inspectGeneratorSupplyV2AuthorityState(fixture.root)).toBe("ASSEMBLED_PROFILE_CURRENT");
  });

  it("rejects source, standalone manifest, and output drift after the current outer snapshot", () => {
    for (const path of [
      GENERATOR_SUPPLY_V2_SOURCE_PATH,
      GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
      GENERATOR_SUPPLY_V2_OUTPUT_PATH,
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      writeReceipts(fixture.root);
      const registry = assembleTestRegistry(fixture.root, fixture.source);
      writeJson(fixture.root, GENERATOR_SUPPLY_V2_OUTPUT_PATH, registry);
      expectCode(
        () =>
          assertGeneratorSupplyV2CurrentSnapshotMutationForTest(fixture.root, registry, () => {
            const absolute = resolve(fixture.root, path);
            writeFileSync(absolute, Buffer.concat([readFileSync(absolute), Buffer.from("\n")]));
          }),
        "GENERATOR_SUPPLY_V2_REGISTRY_MISMATCH",
      );
    }
  });

  it("rejects placeholder receipts even after all outer records and digests are recomputed", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    for (const [index, path] of SUCCESSOR_REPLAY_RECEIPT_PATHS.entries()) {
      writeJson(fixture.root, path, {
        formatVersion: "placeholder-generator-supply-v2-receipt/v1",
        receiptIndex: index,
        notGateClosure: true,
      });
    }
    const registry = assembleTestRegistry(fixture.root, fixture.source);
    writeJson(fixture.root, GENERATOR_SUPPLY_V2_OUTPUT_PATH, registry);
    expectCode(
      () => assertGeneratorSupplyV2RegistrySemantics(fixture.root, registry),
      "GENERATOR_SUPPLY_V2_EVIDENCE_MISMATCH",
    );
  });

  it("rejects output evidence, predecessor, boundary, and registry digest tampering", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    writeReceipts(fixture.root);
    const registry = assembleTestRegistry(fixture.root, fixture.source);
    for (const mutate of [
      (value: Record<string, any>) => (value.predecessor.outerFiles[0].sizeBytes += 1),
      (value: Record<string, any>) => (value.profile.spec.boundaries.gate = "CLOSED"),
      (value: Record<string, any>) => (value.profile.evidence.receipts[0].sizeBytes += 1),
      (value: Record<string, any>) => (value.registryDigest = `sha256:${"f".repeat(64)}`),
    ]) {
      const tampered = clone(registry) as Record<string, any>;
      mutate(tampered);
      expect(() => assertGeneratorSupplyV2RegistrySemantics(fixture.root, tampered)).toThrow();
    }
  });

  it("keeps source and output schemas strict against unknown fields", () => {
    const fixture = createRoot({ predecessor: true });
    const source = clone(fixture.source) as GeneratorSupplyV2Source & { unexpected?: boolean };
    source.unexpected = true;
    expectCode(
      () => validateGeneratorSupplyV2Source(fixture.root, source),
      "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
    );
  });
});
