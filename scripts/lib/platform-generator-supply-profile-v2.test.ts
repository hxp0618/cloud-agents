import { createHash } from "node:crypto";
import {
  constants,
  copyFileSync,
  lstatSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  realpathSync,
  renameSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildGeneratorSupplyReplayV2TestFixture,
  type GeneratorSupplyReplayV2PreparedReceipts,
} from "./platform-generator-supply-replay-v2";
import {
  assertGeneratorSupplyV2CurrentSnapshotMutationForTest,
  assertGeneratorSupplyV2RegistryCurrent,
  assertGeneratorSupplyV2RegistrySemantics,
  assertGeneratorSupplyV2SourceCurrent,
  assertStableGeneratorSupplyV2ReadMutationForTest,
  buildGeneratorSupplyV2EvidenceManifest,
  buildGeneratorSupplyV2Registry,
  buildGeneratorSupplyV2Source,
  buildGeneratorSupplyV2TestSource,
  GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_PATH,
  GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
  GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  GENERATOR_SUPPLY_V2_SOURCE_PATH,
  GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
  inspectGeneratorSupplyV2AuthorityState,
  serializeGeneratorSupplyV2Source,
  validateGeneratorSupplyV2Source,
  writeGeneratorSupplyV2Assembly,
  writeGeneratorSupplyV2AssemblyForTest,
  writeGeneratorSupplyV2Source,
  type GeneratorSupplyV2AssemblyInputs,
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
  const paths = new Set<string>(GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map((record) => record.path));
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

function createAssemblyInputs(root: string): GeneratorSupplyV2AssemblyInputs {
  const rawRoot = realpathSync(mkdtempSync(join(tmpdir(), "generator-supply-v2-raw-")));
  temporaryRoots.push(rawRoot);
  const darwinOutputDirectory = resolve(rawRoot, "darwin-output");
  const linuxOutputDirectory = resolve(rawRoot, "linux-output");
  mkdirSync(darwinOutputDirectory);
  mkdirSync(linuxOutputDirectory);
  const fixture = buildGeneratorSupplyReplayV2TestFixture(
    root,
    GENERATOR_SUPPLY_V2_REPLAY_CONTRACT,
  );
  const [, darwinA, darwinB, darwinIsolation, linuxA, linuxB, linuxIsolation, projectionPath] =
    SUCCESSOR_REPLAY_RECEIPT_PATHS;
  for (const [path, target] of [
    [darwinA, resolve(darwinOutputDirectory, "darwin-a.json")],
    [darwinB, resolve(darwinOutputDirectory, "darwin-b.json")],
    [darwinIsolation, resolve(darwinOutputDirectory, "darwin-isolation.json")],
    [linuxA, resolve(linuxOutputDirectory, "linux-a.json")],
    [linuxB, resolve(linuxOutputDirectory, "linux-b.json")],
    [linuxIsolation, resolve(linuxOutputDirectory, "linux-isolation.json")],
  ] as const) {
    writeFileSync(target, `${JSON.stringify(fixture.receipts[path], null, 2)}\n`);
  }
  const projection = resolve(rawRoot, "projection.json");
  writeFileSync(projection, `${JSON.stringify(fixture.receipts[projectionPath], null, 2)}\n`);
  return { projection, darwinOutputDirectory, linuxOutputDirectory };
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
  const receipts = SUCCESSOR_REPLAY_RECEIPT_PATHS.map((path) => {
    const bytes = readFileSync(resolve(root, path));
    return { ...fileRecord(root, path), path, bytes };
  });
  const prepared = {
    receipts: new Map(receipts.map((receipt) => [receipt.path, receipt.bytes])),
    receiptRecords: receipts,
    projection: {} as never,
    candidateManifestSha256: "",
    outputFiles: 49,
    assertInputSnapshotCurrent: () => {},
    assertPreparedSnapshotCurrent: () => {},
  } satisfies GeneratorSupplyReplayV2PreparedReceipts;
  const evidenceManifest = buildGeneratorSupplyV2EvidenceManifest(prepared);
  writeJson(root, GENERATOR_SUPPLY_V2_EVIDENCE_MANIFEST_PATH, evidenceManifest);
  return buildGeneratorSupplyV2Registry(source, prepared, evidenceManifest);
}

function writeJson(root: string, path: string, value: unknown): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, `${JSON.stringify(value, null, 2)}\n`);
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

  it("writes only the canonical v2 source before replay and refuses a later rewrite", () => {
    const fixture = createRoot({ predecessor: true });
    writeGeneratorSupplyV2Source(fixture.root);
    expect(assertGeneratorSupplyV2SourceCurrent(fixture.root)).toBe("DECLARED_PRE_REPLAY");
    expect(readFileSync(resolve(fixture.root, GENERATOR_SUPPLY_V2_SOURCE_PATH), "utf8")).toBe(
      serializeGeneratorSupplyV2Source(buildGeneratorSupplyV2Source()),
    );
    writeJson(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0], {});
    expectCode(
      () => writeGeneratorSupplyV2Source(fixture.root),
      "GENERATOR_SUPPLY_V2_PARTIAL_STATE",
    );
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
    const replayRun = JSON.parse(
      readFileSync(resolve(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[1]), "utf8"),
    ) as { candidateManifestSha256: string; outputFiles: number };
    expect(current.candidateManifestSha256).toBe(replayRun.candidateManifestSha256);
    expect(current.outputFiles).toBe(49);
    expect(current.outputFiles).toBe(replayRun.outputFiles);
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

  it("assembles exactly eight receipts plus manifest/profile and is an exact no-op on resume", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    const inputs = createAssemblyInputs(fixture.root);
    const lockPath = resolve(fixture.root, "contracts/generation.lock.json");
    mkdirSync(dirname(lockPath), { recursive: true });
    writeFileSync(lockPath, "immutable-lock-sentinel\n");
    writeGeneratorSupplyV2Assembly(fixture.root, inputs);
    expect(inspectGeneratorSupplyV2AuthorityState(fixture.root)).toBe("ASSEMBLED_PROFILE_CURRENT");
    const exactLatePaths = [...SUCCESSOR_REPLAY_RECEIPT_PATHS, ...SUCCESSOR_ASSEMBLY_PATHS];
    expect(exactLatePaths).toHaveLength(10);
    const snapshot = (path: string) => {
      const stat = lstatSync(resolve(fixture.root, path), { bigint: true });
      return { record: fileRecord(fixture.root, path), ino: stat.ino, mtimeNs: stat.mtimeNs };
    };
    const before = new Map(exactLatePaths.map((path) => [path, snapshot(path)] as const));
    writeGeneratorSupplyV2Assembly(fixture.root, inputs);
    expect(new Map(exactLatePaths.map((path) => [path, snapshot(path)] as const))).toEqual(before);
    expect(readFileSync(lockPath, "utf8")).toBe("immutable-lock-sentinel\n");
    expect(
      readdirSync(resolve(fixture.root, "tools/generator-supply/v2/evidence/replay")).toSorted(),
    ).toEqual(
      [
        "darwin-a.json",
        "darwin-b.json",
        "darwin-isolation.json",
        "linux-a.json",
        "linux-b.json",
        "linux-isolation.json",
        "projection.json",
      ].toSorted(),
    );
  }, 20_000);

  it("rejects missing, extra, exchanged, repository-local, and symlinked raw inputs", () => {
    for (const mutate of [
      (inputs: GeneratorSupplyV2AssemblyInputs) =>
        unlinkSync(resolve(inputs.darwinOutputDirectory, "darwin-b.json")),
      (inputs: GeneratorSupplyV2AssemblyInputs) =>
        writeFileSync(resolve(inputs.linuxOutputDirectory, "extra.json"), "{}\n"),
      (inputs: GeneratorSupplyV2AssemblyInputs) => {
        const a = resolve(inputs.darwinOutputDirectory, "darwin-a.json");
        const b = resolve(inputs.darwinOutputDirectory, "darwin-b.json");
        const temporary = resolve(inputs.darwinOutputDirectory, "swap.tmp");
        renameSync(a, temporary);
        renameSync(b, a);
        renameSync(temporary, b);
      },
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      const inputs = createAssemblyInputs(fixture.root);
      mutate(inputs);
      expect(() => writeGeneratorSupplyV2Assembly(fixture.root, inputs)).toThrow();
      expect(SUCCESSOR_REPLAY_RECEIPT_PATHS.some((path) => exists(fixture.root, path))).toBe(false);
    }

    const local = createRoot({ source: true, predecessor: true });
    const localInputs = createAssemblyInputs(local.root);
    expectCode(
      () =>
        writeGeneratorSupplyV2Assembly(local.root, {
          ...localInputs,
          projection: resolve(local.root, GENERATOR_SUPPLY_V2_SOURCE_PATH),
        }),
      "GENERATOR_SUPPLY_V2_RAW_INPUT_INVALID",
    );

    const linked = createRoot({ source: true, predecessor: true });
    const linkedInputs = createAssemblyInputs(linked.root);
    const actualDarwin = `${linkedInputs.darwinOutputDirectory}-actual`;
    renameSync(linkedInputs.darwinOutputDirectory, actualDarwin);
    symlinkSync(actualDarwin, linkedInputs.darwinOutputDirectory, "dir");
    expectCode(
      () => writeGeneratorSupplyV2Assembly(linked.root, linkedInputs),
      "GENERATOR_SUPPLY_V2_RAW_INPUT_INVALID",
    );
  }, 30_000);

  it("fences raw file and ancestor ABA or atomic replacement after the one stable read", () => {
    for (const mutate of [
      (inputs: GeneratorSupplyV2AssemblyInputs) => {
        const path = resolve(inputs.darwinOutputDirectory, "darwin-a.json");
        const replacement = resolve(dirname(path), ".darwin-a-replacement.json");
        writeFileSync(replacement, readFileSync(path));
        renameSync(replacement, path);
      },
      (inputs: GeneratorSupplyV2AssemblyInputs) => {
        const original = inputs.darwinOutputDirectory;
        const displaced = `${original}-displaced`;
        renameSync(original, displaced);
        mkdirSync(original);
        for (const name of ["darwin-a.json", "darwin-b.json", "darwin-isolation.json"]) {
          writeFileSync(resolve(original, name), readFileSync(resolve(displaced, name)));
        }
      },
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      const inputs = createAssemblyInputs(fixture.root);
      expectCode(
        () =>
          writeGeneratorSupplyV2AssemblyForTest(fixture.root, inputs, {
            afterRawSnapshot: () => mutate(inputs),
          }),
        "GENERATOR_SUPPLY_V2_RAW_INPUT_INVALID",
      );
      expect(SUCCESSOR_REPLAY_RECEIPT_PATHS.some((path) => exists(fixture.root, path))).toBe(false);
    }
  }, 20_000);

  it("uses no-replace publication, rejects divergent winners, and resumes after failure", () => {
    const exactRace = createRoot({ source: true, predecessor: true });
    const exactInputs = createAssemblyInputs(exactRace.root);
    writeGeneratorSupplyV2AssemblyForTest(exactRace.root, exactInputs, {
      beforePublish: (_path, index, temporary, output) => {
        if (index === 0) writeFileSync(output, readFileSync(temporary));
      },
    });
    expect(inspectGeneratorSupplyV2AuthorityState(exactRace.root)).toBe(
      "ASSEMBLED_PROFILE_CURRENT",
    );

    const divergent = createRoot({ source: true, predecessor: true });
    const divergentInputs = createAssemblyInputs(divergent.root);
    expectCode(
      () =>
        writeGeneratorSupplyV2AssemblyForTest(divergent.root, divergentInputs, {
          beforePublish: (_path, index, _temporary, output) => {
            if (index === 0) writeFileSync(output, "divergent-winner\n");
          },
        }),
      "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
    );
    expect(readFileSync(resolve(divergent.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0]), "utf8")).toBe(
      "divergent-winner\n",
    );

    const resumable = createRoot({ source: true, predecessor: true });
    const resumableInputs = createAssemblyInputs(resumable.root);
    expect(() =>
      writeGeneratorSupplyV2AssemblyForTest(resumable.root, resumableInputs, {
        afterPublish: (_path, index) => {
          if (index === 2) throw new Error("injected after-publish failure");
        },
      }),
    ).toThrow(/injected after-publish failure/u);
    expect(
      SUCCESSOR_REPLAY_RECEIPT_PATHS.slice(0, 3).every((path) => exists(resumable.root, path)),
    ).toBe(true);
    expect(exists(resumable.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[3])).toBe(false);
    writeGeneratorSupplyV2Assembly(resumable.root, resumableInputs);
    expect(inspectGeneratorSupplyV2AuthorityState(resumable.root)).toBe(
      "ASSEMBLED_PROFILE_CURRENT",
    );
  }, 30_000);

  it("detects destination parent and same-byte output replacement across the ten-file transaction", () => {
    const replacement = createRoot({ source: true, predecessor: true });
    const inputs = createAssemblyInputs(replacement.root);
    expectCode(
      () =>
        writeGeneratorSupplyV2AssemblyForTest(replacement.root, inputs, {
          afterPublish: (_path, index) => {
            if (index !== 1) return;
            const first = resolve(replacement.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0]);
            const temporary = resolve(dirname(first), ".same-bytes-replacement");
            writeFileSync(temporary, readFileSync(first));
            renameSync(temporary, first);
          },
        }),
      "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
    );

    const parentRace = createRoot({ source: true, predecessor: true });
    const parentInputs = createAssemblyInputs(parentRace.root);
    expectCode(
      () =>
        writeGeneratorSupplyV2AssemblyForTest(parentRace.root, parentInputs, {
          afterPublish: (_path, index) => {
            if (index !== 0) return;
            const replayParent = resolve(
              parentRace.root,
              "tools/generator-supply/v2/evidence/replay",
            );
            const displaced = `${replayParent}-displaced`;
            renameSync(replayParent, displaced);
            mkdirSync(replayParent);
          },
        }),
      "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
    );

    const cleanupRace = createRoot({ source: true, predecessor: true });
    const cleanupInputs = createAssemblyInputs(cleanupRace.root);
    let attackerTemporary = "";
    let displacedParent = "";
    expectCode(
      () =>
        writeGeneratorSupplyV2AssemblyForTest(cleanupRace.root, cleanupInputs, {
          beforePublish: (_path, index, temporary) => {
            if (index !== 0) return;
            const parent = dirname(temporary);
            displacedParent = `${parent}-displaced`;
            renameSync(parent, displacedParent);
            mkdirSync(parent);
            attackerTemporary = temporary;
            writeFileSync(attackerTemporary, "attacker-sentinel\n");
          },
        }),
      "GENERATOR_SUPPLY_V2_WRITE_CONFLICT",
    );
    expect(readFileSync(attackerTemporary, "utf8")).toBe("attacker-sentinel\n");
    expect(readdirSync(displacedParent).some((name) => name.includes(".write-"))).toBe(true);
  }, 20_000);

  it("stops after the current item when raw or source authority drifts between publications", () => {
    for (const mutate of [
      (fixtureRoot: string, inputs: GeneratorSupplyV2AssemblyInputs) =>
        writeFileSync(
          inputs.projection,
          Buffer.concat([readFileSync(inputs.projection), Buffer.from(" ")]),
        ),
      (fixtureRoot: string) => {
        const source = resolve(fixtureRoot, GENERATOR_SUPPLY_V2_SOURCE_PATH);
        writeFileSync(source, Buffer.concat([readFileSync(source), Buffer.from(" ")]));
      },
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      const inputs = createAssemblyInputs(fixture.root);
      expect(() =>
        writeGeneratorSupplyV2AssemblyForTest(fixture.root, inputs, {
          afterPublish: (_path, index) => {
            if (index === 0) mutate(fixture.root, inputs);
          },
        }),
      ).toThrow();
      expect(exists(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0])).toBe(true);
      expect(exists(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[1])).toBe(false);
    }
  }, 20_000);

  it("stops after the first item when an otherwise unconsumed outer v1 file loses identity", () => {
    const outerPath = GENERATOR_SUPPLY_V1_IMMUTABLE_FILES[1].path;
    for (const mutate of [
      (target: string) =>
        writeFileSync(target, Buffer.concat([readFileSync(target), Buffer.from(" ")])),
      (target: string) => {
        const replacement = resolve(dirname(target), ".v1-outer-same-bytes-replacement");
        writeFileSync(replacement, readFileSync(target));
        renameSync(replacement, target);
      },
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      const inputs = createAssemblyInputs(fixture.root);
      expect(() =>
        writeGeneratorSupplyV2AssemblyForTest(fixture.root, inputs, {
          afterPublish: (_path, index) => {
            if (index === 0) mutate(resolve(fixture.root, outerPath));
          },
        }),
      ).toThrowError(
        expect.objectContaining({
          code: "PREDECESSOR_FILE_MISMATCH",
          path: outerPath,
        }),
      );
      expect(exists(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[0])).toBe(true);
      expect(exists(fixture.root, SUCCESSOR_REPLAY_RECEIPT_PATHS[1])).toBe(false);
    }
  }, 20_000);

  it("validates source and output only with the two captured schema byte snapshots", () => {
    const fixture = createRoot({ source: true, predecessor: true });
    const inputs = createAssemblyInputs(fixture.root);
    const schemaParent = dirname(resolve(fixture.root, GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH));
    const displacedParent = `${schemaParent}-captured-authority`;
    const phases: Array<"source" | "output"> = [];
    writeGeneratorSupplyV2AssemblyForTest(fixture.root, inputs, {
      beforeCapturedSchemaValidation: (phase) => {
        phases.push(phase);
        renameSync(schemaParent, displacedParent);
        mkdirSync(schemaParent);
        for (const path of [
          GENERATOR_SUPPLY_V2_SOURCE_SCHEMA_PATH,
          GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH,
        ]) {
          const schema = JSON.parse(
            readFileSync(resolve(displacedParent, basename(path)), "utf8"),
          ) as Record<string, unknown>;
          schema.not = {};
          writeFileSync(resolve(schemaParent, basename(path)), `${JSON.stringify(schema)}\n`);
        }
      },
      afterCapturedSchemaValidation: () => {
        rmSync(schemaParent, { recursive: true });
        renameSync(displacedParent, schemaParent);
      },
    });
    expect(phases).toEqual(["source", "output"]);
    expect(inspectGeneratorSupplyV2AuthorityState(fixture.root)).toBe("ASSEMBLED_PROFILE_CURRENT");
    expect(() => assertGeneratorSupplyV2RegistryCurrent(fixture.root)).not.toThrow();
  }, 20_000);

  it("rejects invalid or canonical-rejecting captured output schema before any output", () => {
    for (const mutateSchema of [
      (path: string) => writeFileSync(path, "{\n"),
      (path: string) => {
        const schema = JSON.parse(readFileSync(path, "utf8")) as Record<string, unknown>;
        schema.not = {};
        writeFileSync(path, `${JSON.stringify(schema)}\n`);
      },
    ]) {
      const fixture = createRoot({ source: true, predecessor: true });
      const inputs = createAssemblyInputs(fixture.root);
      mutateSchema(resolve(fixture.root, GENERATOR_SUPPLY_V2_OUTPUT_SCHEMA_PATH));
      expectCode(
        () => writeGeneratorSupplyV2Assembly(fixture.root, inputs),
        "GENERATOR_SUPPLY_V2_SCHEMA_INVALID",
      );
      expect(
        [...SUCCESSOR_REPLAY_RECEIPT_PATHS, ...SUCCESSOR_ASSEMBLY_PATHS].some((path) =>
          exists(fixture.root, path),
        ),
      ).toBe(false);
    }
  }, 20_000);
});

function exists(root: string, path: string): boolean {
  try {
    readFileSync(resolve(root, path));
    return true;
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
    throw error;
  }
}
