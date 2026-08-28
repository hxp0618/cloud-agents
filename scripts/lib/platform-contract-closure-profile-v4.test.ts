import { createHash } from "node:crypto";
import {
  chmodSync,
  cpSync,
  copyFileSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertContractClosureV4RegistrySemantics,
  assertContractClosureProfileV4Current,
  assertContractClosureProfileV4CurrentMutationForTest,
  assertContractClosureProfileV4SourceCurrent,
  assertContractClosureV4V2DependencyABAMutationForTest,
  assertContractClosureV4RepositoryLineageCurrent,
  buildContractClosureProfileV4Registry,
  buildContractClosureProfileV4Source,
  buildContractClosureProfileV4TestSource,
  CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
  CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH,
  CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
  CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH,
  CONTRACT_CLOSURE_V4_AUTHORITY_FILE,
  CONTRACT_CLOSURE_V4_CRITERIA,
  CONTRACT_CLOSURE_V4_MISSING,
  CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE,
  CONTRACT_CLOSURE_V4_RUNTIME_FILES,
  CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE,
  CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES,
  ContractClosureProfileV4Error,
  deriveContractClosureV4Missing,
  serializeContractClosureProfileV4Registry,
  serializeContractClosureProfileV4Source,
  type ContractClosureV4Source,
  validateContractClosureProfileV4Source,
  writeContractClosureProfileV4,
  writeContractClosureProfileV4Source,
} from "./platform-contract-closure-profile-v4";
import {
  CONTRACT_CLOSURE_V1_IMMUTABLE_FILES,
  CONTRACT_CLOSURE_V2_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
} from "./platform-successor-predecessor";

type MutableRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function criteria(source: ContractClosureV4Source): MutableRecord[] {
  return source.profile.criteria as unknown as MutableRecord[];
}

function expectSourceFailure(
  mutate: (source: ContractClosureV4Source) => void,
  code?: ContractClosureProfileV4Error["code"],
): void {
  const source = clone(buildContractClosureProfileV4TestSource(repositoryRoot));
  mutate(source);
  const execute = (): void => validateContractClosureProfileV4Source(repositoryRoot, source);
  if (code) {
    expect(execute).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({ code }),
    );
  } else {
    expect(execute).toThrow(ContractClosureProfileV4Error);
  }
}

function createCurrentRoot(): string {
  const root = mkdtempSync(resolve(tmpdir(), "contract-closure-v4-current-"));
  temporaryRoots.push(root);
  const manifest = JSON.parse(
    readFileSync(
      resolve(repositoryRoot, GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath),
      "utf8",
    ),
  ) as { files: Array<{ path: string }> };
  const paths = new Set([
    ...CONTRACT_CLOSURE_V1_IMMUTABLE_FILES.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES.map(({ path }) => path),
    ...GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path }) => path),
    ...manifest.files.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V4_RUNTIME_FILES.map(({ path }) => path),
    CONTRACT_CLOSURE_V4_RUNTIME_REVIEW_FILE.path,
    CONTRACT_CLOSURE_V4_AUTHORITY_FILE.path,
    CONTRACT_CLOSURE_PROFILE_V4_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_SCHEMA_PATH,
  ]);
  for (const path of paths) copy(root, path);
  const source = buildContractClosureProfileV4TestSource(root);
  const registry = buildContractClosureProfileV4Registry(root, source);
  writeJson(root, CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH, source);
  writeJson(root, CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH, registry);
  return root;
}

function copy(root: string, path: string): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  copyFileSync(resolve(repositoryRoot, path), target);
}

function writeJson(root: string, path: string, value: unknown): void {
  const target = resolve(root, path);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target, serializeContractClosureProfileV4Registry(value));
}

describe("contract closure profile v4 Slice A authority", () => {
  it("promotes the deterministic test builder to the canonical production source serializer", () => {
    const source = buildContractClosureProfileV4Source(repositoryRoot);
    expect(source).toEqual(buildContractClosureProfileV4TestSource(repositoryRoot));
    expect(serializeContractClosureProfileV4Source(source)).toBe(
      serializeContractClosureProfileV4Registry(source),
    );
  });

  it("builds a deterministic strict non-Gate registry with exactly one derived missing item", () => {
    const source = buildContractClosureProfileV4TestSource(repositoryRoot);
    expect(() => validateContractClosureProfileV4Source(repositoryRoot, source)).not.toThrow();
    expect(source.profile.criteria.map(({ id }) => id)).toEqual(CONTRACT_CLOSURE_V4_CRITERIA);
    expect(source.profile.criteria.map(({ status }) => status)).toEqual([
      "SATISFIED_CANDIDATE",
      "SATISFIED_CANDIDATE",
      "SATISFIED_CANDIDATE",
      "SATISFIED_CANDIDATE",
      "SATISFIED_CANDIDATE",
      "SATISFIED_CANDIDATE",
      "REVIEW_PENDING",
    ]);
    expect(source.profile.criteria[6]).not.toHaveProperty("review");
    expect(deriveContractClosureV4Missing(source.profile)).toEqual(CONTRACT_CLOSURE_V4_MISSING);

    const first = buildContractClosureProfileV4Registry(repositoryRoot, source);
    const second = buildContractClosureProfileV4Registry(repositoryRoot, source);
    expect(serializeContractClosureProfileV4Registry(first)).toBe(
      serializeContractClosureProfileV4Registry(second),
    );
    expect(first).toMatchObject({
      formatVersion: "cloud-agents-contract-closure-profile-registry/v4",
      registryId: "cloud-agents/platform/contract-closure-profile",
      missing: ["remaining-generator-supply-chain-review"],
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      profile: {
        spec: {
          profileId: "contract-closure-profile/v4",
          status: "BOOTSTRAP_VALIDATED",
          notGateClosure: true,
          gateStatus: "ALL_GATES_OPEN",
        },
      },
    });
    expect(() => assertContractClosureV4RegistrySemantics(repositoryRoot, first)).not.toThrow();
    expect(serializeContractClosureProfileV4Registry(first)).not.toMatch(
      /generatedAt|generated_at|\/Users\//u,
    );
  });

  it("binds the exact closure-v2, runtime, and generator-supply-v1 predecessor identities", () => {
    const source = buildContractClosureProfileV4TestSource(repositoryRoot);
    expect(source.predecessor).toMatchObject({
      profileId: "contract-closure-profile/v2",
      predecessorMutation: "forbidden",
      files: expect.arrayContaining([
        expect.objectContaining({
          path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json",
          sha256: "5069f0f1bdca9b7b7c161cb36870c00be254acb315cafab45adcc944b19e33fe",
          sizeBytes: 8690,
        }),
      ]),
    });
    expect(source.runtimeReviewedCandidate).toMatchObject({
      candidate: {
        commit: "b79d01028c652d004e67a00fdcbdf204e04dc946",
        tree: "289c7c2ff7ab39b0af1ea0bac84a902d461de8dc",
        parent: "4ee0e847a7c8e6d0c7313f0f359acc7002ec9d97",
        diffSha256: "sha256:e967207e24167e8461fbffbbc98df41103e06eacc508f1bc9baca289433b639c",
      },
      review: {
        path: "docs/plan/p1/g-contract-runtime-current-lineage-rebind-independent-review-20260828.md",
        sha256: "sha256:46bd55af8d0bb6983062cba7c104fd6432785adbf7db24b046a92e4b39b4fcd6",
        verdict: "APPROVE_P0_0_P1_0_P2_0",
        commit: "62da35c546b3a53659315b6873e6dadbe29fb2d3",
        tree: "d77b068399b42e13fbf0f0337f0fc94f49556dbb",
        parent: "b79d01028c652d004e67a00fdcbdf204e04dc946",
      },
      implementationBoundary: {
        http: "NOT_IMPLEMENTED",
        oidc: "NOT_IMPLEMENTED",
        jwks: "NOT_IMPLEMENTED",
        projectWriter: "NOT_IMPLEMENTED",
        provider: "NOT_IMPLEMENTED",
      },
    });
    expect(source.authorityBinding).toEqual({
      ...CONTRACT_CLOSURE_V4_AUTHORITY_FILE,
    });
    expect(source.generatorSupplyV1Predecessor).toMatchObject({
      profileId: "cloud-agents/generator-supply-profile/v1",
      evidenceManifest: {
        memberCount: 39,
        memberVerification: "EXACT_PATH_SHA256_SIZE_REQUIRED",
      },
      candidate: {
        commit: "e5f981c8197cea7527a57c391e7198570f61b92c",
        reviewParent: "e5f981c8197cea7527a57c391e7198570f61b92c",
        reviewSha256: "sha256:86ec054debf15de71481d6f9ab965ca5c8f24a4f5a98f9e5e155e24df261cd47",
      },
      projection: {
        treeSha: "4a70fb8b1e18801f4f02a753668ffe91b63b6275",
        archiveSha256: "36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478",
        archiveSizeBytes: 46_008_320,
        receiptSha256: "1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4",
        receiptSizeBytes: 1_708,
      },
      futureSuccessor: {
        path: "tools/generator-supply/v2/profile.json",
        reviewStatus: "REVIEW_PENDING",
        canonicalBuildRead: "FORBIDDEN",
      },
    });
    expect(CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE.reviewParent).toBe(
      CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE.candidateCommit,
    );
    expect(() => assertContractClosureV4RepositoryLineageCurrent(repositoryRoot)).not.toThrow();
  });

  it("freezes the v3 predecessor, projection algorithms, complete exclusion set, and receipt order", () => {
    const source = buildContractClosureProfileV4TestSource(repositoryRoot);
    expect(source.authorityRevision).toBe("D-053-EC-2.r4");
    expect(source.supersededV3Predecessor).toMatchObject({
      profileId: "contract-closure-profile/v3",
      predecessorMutation: "forbidden",
      baseline: {
        commit: "16275f6cbf390c343a9ac00f9193e75eaad0094e",
        tree: "ca595b8e1258a8b78c4da3a545b2a31d8f62b531",
      },
      files: expect.arrayContaining([
        expect.objectContaining({
          path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
          gitBlob: "d714424ac6b42a44ee775a6edde6327d87f2d7c3",
          sizeBytes: 14_215,
        }),
      ]),
    });
    expect(source.replayAuthority).toMatchObject({
      projection: {
        archiveMemberManifestAlgorithm:
          "utf8-bytewise-sorted-path-type-mode-size-sha256-linktarget-nul-v1",
        regularManifestAlgorithm: "utf8-bytewise-sorted-path-mode-size-sha256-nul-v1",
        exclusions: expect.arrayContaining([
          "contracts/generation.lock.json",
          "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md",
        ]),
      },
      runner: {
        policy: "VERSIONED_ISOLATION_WRAPPER_V4",
        platforms: ["darwin-arm64", "linux-amd64"],
      },
      receipts: [
        "tools/generator-supply/v4/evidence/replay/projection.json",
        "tools/generator-supply/v4/evidence/replay/darwin-a.json",
        "tools/generator-supply/v4/evidence/replay/darwin-b.json",
        "tools/generator-supply/v4/evidence/replay/darwin-isolation.json",
        "tools/generator-supply/v4/evidence/replay/linux-a.json",
        "tools/generator-supply/v4/evidence/replay/linux-b.json",
        "tools/generator-supply/v4/evidence/replay/linux-isolation.json",
        "tools/generator-supply/v4/evidence/replay.json",
      ],
    });

    const drifted = clone(source);
    (drifted.replayAuthority as MutableRecord).receipts = ["foreign-receipt.json"];
    expect(() => validateContractClosureProfileV4Source(repositoryRoot, drifted)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_IDENTITY_MISMATCH",
        path: "/replayAuthority",
      }),
    );
  });

  it("rejects v3 predecessor byte, path, and symlink substitution", () => {
    const root = createCurrentRoot();
    const record = CONTRACT_CLOSURE_V4_V3_PREDECESSOR_FILES[0]!;
    const target = resolve(root, record.path);
    writeFileSync(target, Buffer.concat([readFileSync(target), Buffer.from("\n")]));
    expect(() =>
      validateContractClosureProfileV4Source(root, buildContractClosureProfileV4Source(root)),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        path: `/supersededV3Predecessor/${record.path}`,
      }),
    );

    const cleanRoot = createCurrentRoot();
    const external = mkdtempSync(resolve(tmpdir(), "contract-closure-v4-v3-link-"));
    temporaryRoots.push(external);
    const sentinel = resolve(external, "sentinel");
    writeFileSync(sentinel, readFileSync(resolve(cleanRoot, record.path)));
    rmSync(resolve(cleanRoot, record.path));
    symlinkSync(sentinel, resolve(cleanRoot, record.path));
    expect(() =>
      validateContractClosureProfileV4Source(
        cleanRoot,
        buildContractClosureProfileV4Source(cleanRoot),
      ),
    ).toThrow(ContractClosureProfileV4Error);
  });

  it("rejects inherited criterion drift, runtime overclaim, supply review fabrication, and manual missing removal", () => {
    expectSourceFailure((source) => {
      criteria(source)[0]!.authorityPaths = ["package.json"];
    }, "CONTRACT_CLOSURE_V4_IDENTITY_MISMATCH");
    expectSourceFailure((source) => {
      const boundary = source.profile.implementationBoundary as MutableRecord;
      boundary.http = "IMPLEMENTED";
    });
    expectSourceFailure((source) => {
      const candidate = (source.runtimeReviewedCandidate as MutableRecord)
        .candidate as MutableRecord;
      candidate.diffSha256 = `sha256:${"0".repeat(64)}`;
    }, "CONTRACT_CLOSURE_V4_SCHEMA_INVALID");
    expectSourceFailure((source) => {
      criteria(source)[6]!.status = "SATISFIED_CANDIDATE";
      criteria(source)[6]!.review = clone(criteria(source)[5]!.review);
      delete criteria(source)[6]!.reason;
    });
    expectSourceFailure((source) => {
      (source.profile.derivation as MutableRecord).missing = "manual";
    });
  });

  it("rejects authority markdown byte, raw digest, and Git blob drift", () => {
    const root = createCurrentRoot();
    const authority = resolve(root, CONTRACT_CLOSURE_V4_AUTHORITY_FILE.path);
    const source = buildContractClosureProfileV4TestSource(root);
    writeFileSync(authority, Buffer.concat([readFileSync(authority), Buffer.from("\n")]));
    expect(() => validateContractClosureProfileV4Source(root, source)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        path: `/authorityBinding/${CONTRACT_CLOSURE_V4_AUTHORITY_FILE.path}`,
      }),
    );
  });

  it("keeps exported authority and runtime selectors immutable against caller replacement", () => {
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_AUTHORITY_FILE)).toBe(true);
    expect(Object.isFrozen(CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE)).toBe(true);
    expect(() => {
      (CONTRACT_CLOSURE_V4_AUTHORITY_FILE as unknown as MutableRecord).gitBlob = "0".repeat(40);
    }).toThrow(TypeError);
    expect(() => {
      (CONTRACT_CLOSURE_V4_RUNTIME_GIT_LINEAGE as unknown as MutableRecord).candidateCommit =
        "0".repeat(40);
    }).toThrow(TypeError);
  });

  it("rejects successor and self-review references before they can affect canonical closure", () => {
    expectSourceFailure((source) => {
      criteria(source)[5]!.evidencePaths = [
        "contracts/generated/platform/v1alpha1/contract-closure-profile-v4.json",
      ];
    }, "CONTRACT_CLOSURE_V4_SELF_REFERENCE");
    expectSourceFailure((source) => {
      criteria(source)[6]!.evidencePaths = ["tools/generator-supply/v2/profile.json"];
    }, "CONTRACT_CLOSURE_V4_SELF_REFERENCE");
  });

  it("rejects additional source/output fields and digest drift", () => {
    expectSourceFailure((source) => {
      (source as MutableRecord).unexpected = true;
    }, "CONTRACT_CLOSURE_V4_SCHEMA_INVALID");

    const registry = clone(
      buildContractClosureProfileV4Registry(
        repositoryRoot,
        buildContractClosureProfileV4TestSource(repositoryRoot),
      ),
    );
    (registry as MutableRecord).unexpected = true;
    expect(() => assertContractClosureV4RegistrySemantics(repositoryRoot, registry)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_SCHEMA_INVALID",
      }),
    );

    const drifted = clone(registry) as unknown as MutableRecord;
    delete drifted.unexpected;
    drifted.registryDigest = `sha256:${"0".repeat(64)}`;
    expect(() => assertContractClosureV4RegistrySemantics(repositoryRoot, drifted)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH",
        path: "/registryDigest",
      }),
    );

    const sourceDrifted = clone(registry) as unknown as MutableRecord;
    delete sourceDrifted.unexpected;
    sourceDrifted.sourceDigest = `sha256:${"f".repeat(64)}`;
    sourceDrifted.registryDigest = `sha256:${"0".repeat(64)}`;
    expect(() =>
      assertContractClosureV4RegistrySemantics(repositoryRoot, sourceDrifted),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_DIGEST_MISMATCH",
        path: "/sourceDigest",
      }),
    );
  });

  it("captures a current source/output pair and returns the exact output-file digest", () => {
    const root = createCurrentRoot();
    const current = assertContractClosureProfileV4Current(root);
    const outputBytes = readFileSync(resolve(root, CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH));
    expect(current.fileSha256).toBe(
      `sha256:${createHash("sha256").update(outputBytes).digest("hex")}`,
    );
    expect(current.registry.sourceDigest).toBe(
      buildContractClosureProfileV4Registry(root, current.source).sourceDigest,
    );
    expect(() => current.assertCurrent()).not.toThrow();
  });

  it("rejects source or output formatting drift even when parsed semantics are unchanged", () => {
    for (const path of [
      CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
      CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
    ]) {
      const root = createCurrentRoot();
      writeFileSync(
        resolve(root, path),
        Buffer.concat([readFileSync(resolve(root, path)), Buffer.from(" ")]),
      );
      expect(() => assertContractClosureProfileV4Current(root)).toThrowError(
        expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
          code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          path: "/source-output",
        }),
      );
    }
  });

  it("rejects source or output key-order drift instead of reserializing captured order", () => {
    for (const path of [
      CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH,
      CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH,
    ]) {
      const root = createCurrentRoot();
      const document = JSON.parse(readFileSync(resolve(root, path), "utf8")) as MutableRecord;
      const reordered = Object.fromEntries(Object.entries(document).reverse());
      writeFileSync(resolve(root, path), serializeContractClosureProfileV4Registry(reordered));
      expect(() => assertContractClosureProfileV4Current(root)).toThrowError(
        expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
          code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          path: "/source-output",
        }),
      );
    }
  });

  it("writes the source explicitly, then replay-writes only the output without Git metadata", () => {
    const root = createCurrentRoot();
    const v1v2 = [...CONTRACT_CLOSURE_V1_IMMUTABLE_FILES, ...CONTRACT_CLOSURE_V2_IMMUTABLE_FILES];
    const before = new Map(
      v1v2.map(({ path }) => [path, readFileSync(resolve(root, path)).toString("hex")]),
    );
    rmSync(resolve(root, CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH));
    rmSync(resolve(root, CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH));

    writeContractClosureProfileV4Source(root);
    const sourcePath = resolve(root, CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH);
    const sourceBeforeReplay = readFileSync(sourcePath);
    chmodSync(sourcePath, 0o444);
    try {
      writeContractClosureProfileV4(root);
    } finally {
      chmodSync(sourcePath, 0o644);
    }

    expect(() => assertContractClosureProfileV4Current(root)).not.toThrow();
    expect(assertContractClosureProfileV4SourceCurrent(root)).toEqual(
      buildContractClosureProfileV4Source(root),
    );
    expect(readFileSync(sourcePath)).toEqual(sourceBeforeReplay);
    expect(existsSync(resolve(root, ".git"))).toBe(false);
    for (const { path } of v1v2) {
      expect(readFileSync(resolve(root, path)).toString("hex")).toBe(before.get(path));
    }
  });

  it("rejects source/output symlink destinations before touching their targets", () => {
    for (const [path, write] of [
      [CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH, writeContractClosureProfileV4Source],
      [CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH, writeContractClosureProfileV4],
    ] as const) {
      const root = createCurrentRoot();
      const externalRoot = mkdtempSync(resolve(tmpdir(), "contract-closure-v4-external-"));
      temporaryRoots.push(externalRoot);
      const sentinel = resolve(externalRoot, "sentinel.json");
      writeFileSync(sentinel, "outside\n");
      rmSync(resolve(root, path));
      symlinkSync(sentinel, resolve(root, path));

      expect(() => write(root)).toThrowError(
        expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
          code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
          path: `/${path}`,
        }),
      );
      expect(readFileSync(sentinel, "utf8")).toBe("outside\n");
    }
  });

  it("fails closed when the source changes after the source/output capture", () => {
    const root = createCurrentRoot();
    expect(() =>
      assertContractClosureProfileV4CurrentMutationForTest(root, () => {
        const source = resolve(root, CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH);
        writeFileSync(source, Buffer.concat([readFileSync(source), Buffer.from(" ")]));
      }),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        path: `/${CONTRACT_CLOSURE_PROFILE_V4_SOURCE_PATH}`,
      }),
    );
  });

  it("fails closed when the output is atomically replaced after capture", () => {
    const root = createCurrentRoot();
    expect(() =>
      assertContractClosureProfileV4CurrentMutationForTest(root, () => {
        const output = resolve(root, CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH);
        const replacement = resolve(dirname(output), ".contract-closure-v4-output-replacement");
        writeFileSync(replacement, readFileSync(output));
        renameSync(replacement, output);
      }),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        path: `/${CONTRACT_CLOSURE_PROFILE_V4_OUTPUT_PATH}`,
      }),
    );
  });

  it("rejects a closure-v2 parent-directory ABA between immutable and derived reads", () => {
    const root = createCurrentRoot();
    const v2Path = CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.at(-1)!.path;
    const live = dirname(resolve(root, v2Path));
    const parked = `${live}.original`;
    const alternate = `${live}.alternate`;
    cpSync(live, alternate, { recursive: true });
    const alternateOutput = resolve(alternate, v2Path.split("/").at(-1)!);
    writeFileSync(
      alternateOutput,
      Buffer.concat([readFileSync(alternateOutput), Buffer.from(" ")]),
    );
    expect(() =>
      assertContractClosureV4V2DependencyABAMutationForTest(
        root,
        () => {
          renameSync(live, parked);
          renameSync(alternate, live);
        },
        () => {
          renameSync(live, alternate);
          renameSync(parked, live);
        },
      ),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV4Error>>({
        code: "CONTRACT_CLOSURE_V4_EVIDENCE_MISMATCH",
        path: `/${v2Path}`,
      }),
    );
  });
});
