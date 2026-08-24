import { createHash } from "node:crypto";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertContractClosureV3RegistrySemantics,
  assertContractClosureProfileV3Current,
  assertContractClosureProfileV3CurrentMutationForTest,
  assertContractClosureV3RepositoryLineageCurrent,
  buildContractClosureProfileV3Registry,
  buildContractClosureProfileV3TestSource,
  CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
  CONTRACT_CLOSURE_V3_CRITERIA,
  CONTRACT_CLOSURE_V3_MISSING,
  CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE,
  CONTRACT_CLOSURE_V3_RUNTIME_FILES,
  CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE,
  ContractClosureProfileV3Error,
  deriveContractClosureV3Missing,
  serializeContractClosureProfileV3Registry,
  type ContractClosureV3Source,
  validateContractClosureProfileV3Source,
} from "./platform-contract-closure-profile-v3";
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

function criteria(source: ContractClosureV3Source): MutableRecord[] {
  return source.profile.criteria as unknown as MutableRecord[];
}

function expectSourceFailure(
  mutate: (source: ContractClosureV3Source) => void,
  code?: ContractClosureProfileV3Error["code"],
): void {
  const source = clone(buildContractClosureProfileV3TestSource(repositoryRoot));
  mutate(source);
  const execute = (): void => validateContractClosureProfileV3Source(repositoryRoot, source);
  if (code) {
    expect(execute).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({ code }),
    );
  } else {
    expect(execute).toThrow(ContractClosureProfileV3Error);
  }
}

function createCurrentRoot(): string {
  const root = mkdtempSync(resolve(tmpdir(), "contract-closure-v3-current-"));
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
    ...GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path }) => path),
    ...manifest.files.map(({ path }) => path),
    ...CONTRACT_CLOSURE_V3_RUNTIME_FILES.map(({ path }) => path),
    CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE.path,
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
  ]);
  for (const path of paths) copy(root, path);
  const source = buildContractClosureProfileV3TestSource(root);
  const registry = buildContractClosureProfileV3Registry(root, source);
  writeJson(root, CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH, source);
  writeJson(root, CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH, registry);
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
  writeFileSync(target, serializeContractClosureProfileV3Registry(value));
}

describe("contract closure profile v3 Slice A authority", () => {
  it("builds a deterministic strict non-Gate registry with exactly one derived missing item", () => {
    const source = buildContractClosureProfileV3TestSource(repositoryRoot);
    expect(() => validateContractClosureProfileV3Source(repositoryRoot, source)).not.toThrow();
    expect(source.profile.criteria.map(({ id }) => id)).toEqual(CONTRACT_CLOSURE_V3_CRITERIA);
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
    expect(deriveContractClosureV3Missing(source.profile)).toEqual(CONTRACT_CLOSURE_V3_MISSING);

    const first = buildContractClosureProfileV3Registry(repositoryRoot, source);
    const second = buildContractClosureProfileV3Registry(repositoryRoot, source);
    expect(serializeContractClosureProfileV3Registry(first)).toBe(
      serializeContractClosureProfileV3Registry(second),
    );
    expect(first).toMatchObject({
      formatVersion: "cloud-agents-contract-closure-profile-registry/v3",
      registryId: "cloud-agents/platform/contract-closure-profile",
      missing: ["remaining-generator-supply-chain-review"],
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      profile: {
        spec: {
          profileId: "contract-closure-profile/v3",
          status: "BOOTSTRAP_VALIDATED",
          notGateClosure: true,
          gateStatus: "ALL_GATES_OPEN",
        },
      },
    });
    expect(() => assertContractClosureV3RegistrySemantics(repositoryRoot, first)).not.toThrow();
    expect(serializeContractClosureProfileV3Registry(first)).not.toMatch(
      /generatedAt|generated_at|\/Users\//u,
    );
  });

  it("binds the exact closure-v2, runtime, and generator-supply-v1 predecessor identities", () => {
    const source = buildContractClosureProfileV3TestSource(repositoryRoot);
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
        commit: "b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e",
        tree: "2165fd70efd097e7e1decb109cee31e9f6af8ee5",
        parent: "9fe7338d3c424731e0b9946f5252e3f61d5326a9",
        diffSha256: "sha256:d4e6e96595d9d1554356e30878ce4d57143efb579d5a369ebf97c085f3f67562",
      },
      review: {
        path: "docs/plan/p1/g-contract-runtime-current-lineage-integration-independent-review-20260824.md",
        sha256: "sha256:d75212ba6880f91b33fa52f20011e79af962cdb99cc29a27313685211f204ad2",
        verdict: "APPROVE_P0_0_P1_0_P2_0",
        commit: "fe59f0d4059632a102171d9c1eb77a4c147ae65e",
        tree: "7d6f7a65f36c89fadbe02e7e75e3b395bcca97f3",
        parent: "b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e",
      },
      implementationBoundary: {
        http: "NOT_IMPLEMENTED",
        oidc: "NOT_IMPLEMENTED",
        jwks: "NOT_IMPLEMENTED",
        projectWriter: "NOT_IMPLEMENTED",
        provider: "NOT_IMPLEMENTED",
      },
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
    expect(CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE.reviewParent).toBe(
      CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE.candidateCommit,
    );
    expect(() => assertContractClosureV3RepositoryLineageCurrent(repositoryRoot)).not.toThrow();
  });

  it("rejects inherited criterion drift, runtime overclaim, supply review fabrication, and manual missing removal", () => {
    expectSourceFailure((source) => {
      criteria(source)[0]!.authorityPaths = ["package.json"];
    }, "CONTRACT_CLOSURE_V3_IDENTITY_MISMATCH");
    expectSourceFailure((source) => {
      const boundary = source.profile.implementationBoundary as MutableRecord;
      boundary.http = "IMPLEMENTED";
    });
    expectSourceFailure((source) => {
      criteria(source)[6]!.status = "SATISFIED_CANDIDATE";
      criteria(source)[6]!.review = clone(criteria(source)[5]!.review);
      delete criteria(source)[6]!.reason;
    });
    expectSourceFailure((source) => {
      (source.profile.derivation as MutableRecord).missing = "manual";
    });
  });

  it("rejects successor and self-review references before they can affect canonical closure", () => {
    expectSourceFailure((source) => {
      criteria(source)[5]!.evidencePaths = [
        "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json",
      ];
    }, "CONTRACT_CLOSURE_V3_SELF_REFERENCE");
    expectSourceFailure((source) => {
      criteria(source)[6]!.evidencePaths = ["tools/generator-supply/v2/profile.json"];
    }, "CONTRACT_CLOSURE_V3_SELF_REFERENCE");
  });

  it("rejects additional source/output fields and digest drift", () => {
    expectSourceFailure((source) => {
      (source as MutableRecord).unexpected = true;
    }, "CONTRACT_CLOSURE_V3_SCHEMA_INVALID");

    const registry = clone(
      buildContractClosureProfileV3Registry(
        repositoryRoot,
        buildContractClosureProfileV3TestSource(repositoryRoot),
      ),
    );
    (registry as MutableRecord).unexpected = true;
    expect(() => assertContractClosureV3RegistrySemantics(repositoryRoot, registry)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({
        code: "CONTRACT_CLOSURE_V3_SCHEMA_INVALID",
      }),
    );

    const drifted = clone(registry) as unknown as MutableRecord;
    delete drifted.unexpected;
    drifted.registryDigest = `sha256:${"0".repeat(64)}`;
    expect(() => assertContractClosureV3RegistrySemantics(repositoryRoot, drifted)).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({
        code: "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH",
        path: "/registryDigest",
      }),
    );

    const sourceDrifted = clone(registry) as unknown as MutableRecord;
    delete sourceDrifted.unexpected;
    sourceDrifted.sourceDigest = `sha256:${"f".repeat(64)}`;
    sourceDrifted.registryDigest = `sha256:${"0".repeat(64)}`;
    expect(() =>
      assertContractClosureV3RegistrySemantics(repositoryRoot, sourceDrifted),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({
        code: "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH",
        path: "/sourceDigest",
      }),
    );
  });

  it("captures a current source/output pair and returns the exact output-file digest", () => {
    const root = createCurrentRoot();
    const current = assertContractClosureProfileV3Current(root);
    const outputBytes = readFileSync(resolve(root, CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH));
    expect(current.fileSha256).toBe(
      `sha256:${createHash("sha256").update(outputBytes).digest("hex")}`,
    );
    expect(current.registry.sourceDigest).toBe(
      buildContractClosureProfileV3Registry(root, current.source).sourceDigest,
    );
    expect(() => current.assertCurrent()).not.toThrow();
  });

  it("fails closed when the source changes after the source/output capture", () => {
    const root = createCurrentRoot();
    expect(() =>
      assertContractClosureProfileV3CurrentMutationForTest(root, () => {
        const source = resolve(root, CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH);
        writeFileSync(source, Buffer.concat([readFileSync(source), Buffer.from(" ")]));
      }),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({
        code: "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
        path: `/${CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH}`,
      }),
    );
  });

  it("fails closed when the output is atomically replaced after capture", () => {
    const root = createCurrentRoot();
    expect(() =>
      assertContractClosureProfileV3CurrentMutationForTest(root, () => {
        const output = resolve(root, CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH);
        const replacement = resolve(dirname(output), ".contract-closure-v3-output-replacement");
        writeFileSync(replacement, readFileSync(output));
        renameSync(replacement, output);
      }),
    ).toThrowError(
      expect.objectContaining<Partial<ContractClosureProfileV3Error>>({
        code: "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
        path: `/${CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH}`,
      }),
    );
  });
});
