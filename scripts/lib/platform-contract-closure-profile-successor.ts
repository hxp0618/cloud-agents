import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  existsSync,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
  writeFileSync,
} from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  assertContractClosureV1Immutable,
  assertContractClosureV2Immutable,
  assertGeneratorSupplyV1GitLineageCurrent,
  assertGeneratorSupplyV1PredecessorImmutable,
  assertImmutableFileMap,
  CONTRACT_CLOSURE_V2_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST,
  GENERATOR_SUPPLY_V1_GIT_LINEAGE,
  GENERATOR_SUPPLY_V1_IMMUTABLE_FILES,
  GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
  type ImmutableFileRecord,
} from "./platform-successor-predecessor";

// Shared kernel of the successor contract-closure profiles (v3 and later). A version
// module supplies only its runtime reviewed candidate and, from v4 on, the authority,
// superseded-closure and replay binding it adds; source/registry construction,
// serialization and every validation rule are defined here once.

const REGISTRY_ID = "cloud-agents/platform/contract-closure-profile";
const V2_OUTPUT_PATH = "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json";
const SUPPLY_V1_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md";
const FUTURE_SUPPLY_V2_PROFILE_PATH = "tools/generator-supply/v2/profile.json";
const FUTURE_SUPPLY_V2_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md";
export const CONTRACT_CLOSURE_REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0";
const REQUIRED_VERDICT = CONTRACT_CLOSURE_REQUIRED_VERDICT;

export const CONTRACT_CLOSURE_CRITERIA = Object.freeze([
  "json-schema-2020-12-official-test-suite",
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const);

export const CONTRACT_CLOSURE_MISSING = Object.freeze([
  "remaining-generator-supply-chain-review",
] as const);

export function contractClosureProfilePaths(version: string) {
  const schemas = "contracts/platform/v1alpha1/schemas";
  const schemaIds = "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas";
  return {
    source: `contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-${version}.json`,
    output: `contracts/generated/platform/v1alpha1/contract-closure-profile-${version}.json`,
    sourceSchema: `${schemas}/contract-closure-profile-source-${version}.schema.json`,
    outputSchema: `${schemas}/contract-closure-profile-${version}.schema.json`,
    sourceSchemaId: `${schemaIds}/contract-closure-profile-source-${version}.schema.json`,
    outputSchemaId: `${schemaIds}/contract-closure-profile-${version}.schema.json`,
  } as const;
}

export type ContractClosureErrorSuffix =
  | "SCHEMA_INVALID"
  | "IDENTITY_MISMATCH"
  | "EVIDENCE_MISMATCH"
  | "DIGEST_MISMATCH"
  | "GIT_MISMATCH"
  | "SELF_REFERENCE";

export type ContractClosureRuntimeGitLineage = Readonly<{
  candidateCommit: string;
  candidateTree: string;
  candidateParent: string;
  candidateDiffSha256: string;
  reviewCommit: string;
  reviewTree: string;
  reviewParent: string;
  reviewPath: string;
  reviewSha256: string;
  verdict: string;
}>;

/** What a successor closure (v4) binds on top of the runtime reviewed candidate. */
export type ContractClosureSuccessorBinding = Readonly<{
  authorityRevision: string;
  authorityFile: Readonly<{
    path: string;
    mode: string;
    gitBlob: string;
    sha256: string;
    sizeBytes: number;
    commit: string;
    tree: string;
  }>;
  supersededVersion: string;
  supersededBaseline: Readonly<{ commit: string; tree: string }>;
  supersededFiles: readonly Readonly<ImmutableFileRecord & { gitBlob: string }>[];
  replayAuthority: JsonRecord;
}>;

export type ContractClosureProfileSpec = Readonly<{
  version: string;
  Error: new (code: string, path: string, message: string) => Error;
  runtimeGitLineage: ContractClosureRuntimeGitLineage;
  runtimeFiles: readonly ImmutableFileRecord[];
  runtimeReviewFile: ImmutableFileRecord;
  successor?: ContractClosureSuccessorBinding;
}>;

export type ContractClosureReview = JsonRecord & {
  readonly path: string;
  readonly sha256: string;
  readonly verdict: string;
};

export type ContractClosureCriterion = JsonRecord & {
  readonly id: (typeof CONTRACT_CLOSURE_CRITERIA)[number];
  readonly status: "SATISFIED_CANDIDATE" | "REVIEW_PENDING";
  readonly authorityPaths: readonly string[];
  readonly evidencePaths: readonly string[];
  readonly review?: ContractClosureReview;
  readonly reason?: string;
};

export type ContractClosureProfile = JsonRecord & {
  readonly profileId: string;
  readonly status: "BOOTSTRAP_VALIDATED";
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly criteria: readonly ContractClosureCriterion[];
};

/** Source fields shared by every successor closure; v4 adds the successor binding fields. */
export type ContractClosureSource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: typeof REGISTRY_ID;
  readonly predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly profile: ContractClosureProfile;
};

export type ContractClosureRegistry = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: typeof REGISTRY_ID;
  readonly sourceDigest: string;
  readonly predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly profile: JsonRecord & {
    readonly profileDigest: string;
    readonly spec: ContractClosureProfile;
  };
  readonly missing: readonly string[];
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly registryDigest: string;
};

export type ContractClosureProfileCurrent<
  S extends ContractClosureSource = ContractClosureSource,
  R extends ContractClosureRegistry = ContractClosureRegistry,
> = Readonly<{
  source: S;
  registry: R;
  fileSha256: string;
  assertCurrent: () => void;
}>;

type FileSnapshot = Readonly<{
  rootReal: string;
  path: string;
  absolute: string;
  bytes: Buffer;
  dev: bigint;
  ino: bigint;
  size: bigint;
  mtimeNs: bigint;
  ctimeNs: bigint;
}>;

type V2Registry = {
  readonly profile: {
    readonly spec: {
      readonly criteria: readonly ContractClosureCriterion[];
    };
  };
};

const FIXED_GIT_ENV = {
  PATH: "/usr/bin:/bin",
  LANG: "C",
  LC_ALL: "C",
  TZ: "UTC",
  GIT_CONFIG_NOSYSTEM: "1",
  GIT_CONFIG_GLOBAL: "/dev/null",
  GIT_EXTERNAL_DIFF: "",
  GIT_NO_REPLACE_OBJECTS: "1",
  GIT_OPTIONAL_LOCKS: "0",
  GIT_PAGER: "cat",
} as const;

const FIXED_GIT_CONFIG_ARGS = [
  "-c",
  "core.attributesFile=/dev/null",
  "-c",
  "core.abbrev=7",
  "-c",
  "diff.external=",
  "-c",
  "diff.mnemonicPrefix=false",
  "-c",
  "diff.noprefix=false",
  "-c",
  "diff.renames=false",
] as const;

function deriveMissing(profile: ContractClosureProfile): string[] {
  return profile.criteria
    .filter((criterion) => criterion.status !== "SATISFIED_CANDIDATE")
    .map((criterion) => criterion.id);
}

function expectedClosureV2Predecessor(): JsonRecord {
  return {
    profileId: "contract-closure-profile/v2",
    predecessorMutation: "forbidden",
    files: CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
  };
}

function expectedGeneratorSupplyV1Predecessor(): JsonRecord {
  return {
    profileId: "cloud-agents/generator-supply-profile/v1",
    predecessorMutation: "forbidden",
    outerFiles: GENERATOR_SUPPLY_V1_IMMUTABLE_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
    evidenceManifest: {
      path: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestPath,
      sha256: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSha256,
      sizeBytes: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.manifestSizeBytes,
      algorithm: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.algorithm,
      memberCount: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberCount,
      memberPathPrefix: GENERATOR_SUPPLY_V1_EVIDENCE_MANIFEST.memberPathPrefix,
      memberVerification: "EXACT_PATH_SHA256_SIZE_REQUIRED",
    },
    candidate: {
      commit: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateCommit,
      tree: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateTree,
      parent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateParent,
      diffSha256: `sha256:${GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateDiffSha256}`,
      reviewCommit: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewCommit,
      reviewTree: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewTree,
      reviewParent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewParent,
      reviewPath: GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewPath,
      reviewSha256: `sha256:${GENERATOR_SUPPLY_V1_GIT_LINEAGE.reviewSha256}`,
      reviewVerdict: GENERATOR_SUPPLY_V1_GIT_LINEAGE.verdict,
    },
    identities: GENERATOR_SUPPLY_V1_PROFILE_IDENTITIES,
    projection: {
      treeSha: "4a70fb8b1e18801f4f02a753668ffe91b63b6275",
      archiveSha256: "36070cced3f7b7088f990b46a60b67fcabf742733782533bdfcbd46317950478",
      archiveSizeBytes: 46_008_320,
      receiptPath: "tools/generator-supply/v1/evidence/replay/projection.json",
      receiptSha256: "1587c7715157aaab99c2276b1adbe85fe070aeeb238c054b479edfd1ae1b5cf4",
      receiptSizeBytes: 1_708,
    },
    futureSuccessor: {
      profileId: "cloud-agents/generator-supply-profile/v2",
      path: FUTURE_SUPPLY_V2_PROFILE_PATH,
      reviewPath: FUTURE_SUPPLY_V2_REVIEW_PATH,
      reviewStatus: "REVIEW_PENDING",
      canonicalBuildRead: "FORBIDDEN",
    },
  };
}

function expectedImplementationBoundary(): JsonRecord {
  return {
    runtimeCriterion: "SATISFIED_CANDIDATE_BOUNDED_TRANSPORT_NEUTRAL",
    supplyCriterion: "REVIEW_PENDING",
    http: "NOT_IMPLEMENTED",
    oidc: "NOT_IMPLEMENTED",
    jwks: "NOT_IMPLEMENTED",
    projectWriter: "NOT_IMPLEMENTED",
    provider: "NOT_IMPLEMENTED",
    productionDatabaseWrites: "NOT_AUTHORIZED",
    deployment: "NOT_AUTHORIZED",
    publication: "NOT_AUTHORIZED",
    gateStatus: "ALL_GATES_OPEN",
  };
}

function gitText(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_CONFIG_ARGS, ...args], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function gitBytes(root: string, args: readonly string[]): Buffer {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_CONFIG_ARGS, ...args], {
    cwd: root,
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function sha256(value: Uint8Array): string {
  return createHash("sha256").update(value).digest("hex");
}

function gitBlobSha1(value: Uint8Array): string {
  return createHash("sha1").update(`blob ${value.byteLength}\0`).update(value).digest("hex");
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain);
  hash.update(String.fromCharCode(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function cloneJson<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function createContractClosureProfile<
  S extends ContractClosureSource = ContractClosureSource,
  R extends ContractClosureRegistry = ContractClosureRegistry,
>(spec: ContractClosureProfileSpec) {
  const paths = contractClosureProfilePaths(spec.version);
  const successor = spec.successor;
  const supersededField = `superseded${(successor?.supersededVersion ?? "").toUpperCase()}Predecessor`;

  function buildSource(root: string): S {
    assertContractClosureV2Immutable(root);
    return buildSourceWithoutEvidence(readV2Registry(root));
  }

  function assertCurrent(root: string): ContractClosureProfileCurrent<S, R> {
    return assertCurrentInternal(root);
  }

  function assertSourceCurrent(root: string): S {
    const rootReal = realpathSync(root);
    const snapshot = readStableContainedRegularFileSnapshot(root, paths.source, rootReal);
    const source = parseObject<S>(snapshot);
    validateSource(root, source);
    const expected = buildSource(root);
    if (
      !Buffer.from(canonicalizeJson(source)).equals(Buffer.from(canonicalizeJson(expected))) ||
      snapshot.bytes.toString("utf8") !== serializeSource(expected)
    ) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${paths.source}`,
        `Contract closure ${spec.version} source is not the exact canonical pre-replay authority.`,
      );
    }
    assertSnapshotsCurrent(root, [snapshot]);
    return source;
  }

  function assertCurrentMutationForTest(root: string, mutateAfterCapture: () => void): void {
    assertCurrentInternal(root, mutateAfterCapture);
  }

  function assertV2DependencyABAMutationForTest(
    root: string,
    beforeDerivedRead: () => void,
    afterDerivedRead: () => void,
  ): void {
    assertContractClosureV2Immutable(root);
    beforeDerivedRead();
    try {
      readV2Registry(root);
    } finally {
      afterDerivedRead();
    }
  }

  function assertCurrentInternal(
    root: string,
    mutateAfterCapture?: () => void,
  ): ContractClosureProfileCurrent<S, R> {
    const rootReal = realpathSync(root);
    const sourceSnapshot = readStableContainedRegularFileSnapshot(root, paths.source, rootReal);
    const outputSnapshot = readStableContainedRegularFileSnapshot(root, paths.output, rootReal);
    const source = parseObject<S>(sourceSnapshot);
    const registry = parseObject<R>(outputSnapshot);
    const snapshots = [sourceSnapshot, outputSnapshot] as const;
    const assertCurrent = (): void => {
      validateSource(root, source);
      assertRegistrySemantics(root, registry);
      const expectedSource = buildSource(root);
      const expectedRegistry = buildRegistry(root, expectedSource);
      if (
        sourceSnapshot.bytes.toString("utf8") !== serializeSource(expectedSource) ||
        outputSnapshot.bytes.toString("utf8") !== serializeRegistry(expectedRegistry)
      ) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          "/source-output",
          `Contract closure ${spec.version} source/output must be exact canonical bytes bound in one snapshot.`,
        );
      }
      assertSnapshotsCurrent(root, snapshots);
    };
    mutateAfterCapture?.();
    assertCurrent();
    return {
      source,
      registry,
      fileSha256: `sha256:${sha256(outputSnapshot.bytes)}`,
      assertCurrent,
    };
  }

  function assertRuntimeGitLineageCurrent(root: string): void {
    const repositoryRoot = realpathSync(root);
    const lineage = spec.runtimeGitLineage;
    try {
      const topLevel = realpathSync(gitText(repositoryRoot, ["rev-parse", "--show-toplevel"]));
      const candidateType = gitText(repositoryRoot, ["cat-file", "-t", lineage.candidateCommit]);
      const reviewType = gitText(repositoryRoot, ["cat-file", "-t", lineage.reviewCommit]);
      const candidateTree = gitText(repositoryRoot, [
        "rev-parse",
        `${lineage.candidateCommit}^{tree}`,
      ]);
      const candidateParents = gitText(repositoryRoot, [
        "show",
        "-s",
        "--format=%P",
        lineage.candidateCommit,
      ]);
      const reviewTree = gitText(repositoryRoot, ["rev-parse", `${lineage.reviewCommit}^{tree}`]);
      const reviewParents = gitText(repositoryRoot, [
        "show",
        "-s",
        "--format=%P",
        lineage.reviewCommit,
      ]);
      const diff = gitBytes(repositoryRoot, [
        "diff",
        "--no-color",
        "--no-ext-diff",
        "--no-textconv",
        "--binary",
        "--no-renames",
        lineage.candidateParent,
        lineage.candidateCommit,
      ]);
      const reviewBytes = gitBytes(repositoryRoot, [
        "cat-file",
        "blob",
        `${lineage.reviewCommit}:${lineage.reviewPath}`,
      ]);
      const moduleBytesCurrent = spec.runtimeFiles.map((file) =>
        readStableContainedRegularFile(repositoryRoot, file.path),
      );
      const moduleBytesAtCandidate = spec.runtimeFiles.map((file) =>
        gitBytes(repositoryRoot, ["cat-file", "blob", `${lineage.candidateCommit}:${file.path}`]),
      );
      if (
        topLevel !== repositoryRoot ||
        candidateType !== "commit" ||
        reviewType !== "commit" ||
        candidateTree !== lineage.candidateTree ||
        candidateParents !== lineage.candidateParent ||
        reviewTree !== lineage.reviewTree ||
        reviewParents !== lineage.reviewParent ||
        sha256(diff) !== lineage.candidateDiffSha256 ||
        sha256(reviewBytes) !== lineage.reviewSha256 ||
        moduleBytesAtCandidate.some(
          (bytes, index) =>
            !bytes.equals(moduleBytesCurrent[index]!) ||
            sha256(bytes) !== spec.runtimeFiles[index]!.sha256,
        )
      ) {
        throw closureError(
          "GIT_MISMATCH",
          `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
          `Contract closure ${spec.version} runtime candidate or closed-pair review Git lineage drifted.`,
        );
      }
    } catch (error) {
      if (error instanceof spec.Error) throw error;
      throw closureError(
        "GIT_MISMATCH",
        `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
        `Contract closure ${spec.version} runtime Git lineage is unavailable or invalid: ${String(error)}.`,
      );
    }
  }

  function assertRepositoryLineageCurrent(root: string): void {
    assertRuntimeGitLineageCurrent(root);
    assertGeneratorSupplyV1GitLineageCurrent(root);
  }

  function serializeRegistry(value: unknown): string {
    return `${formatJson(value, 0, 0)}\n`;
  }

  function serializeSource(value: unknown): string {
    return `${formatJson(value, 0, 0)}\n`;
  }

  function writeSource(root: string): void {
    const source = buildSource(root);
    writeContainedRegularFile(root, paths.source, serializeSource(source));
    assertSourceCurrent(root);
  }

  function write(root: string): void {
    const source = assertSourceCurrent(root);
    const registry = buildRegistry(root, source);
    writeContainedRegularFile(root, paths.output, serializeRegistry(registry));
    assertCurrent(root);
  }

  function assertProfileSemantics(root: string, profile: ContractClosureProfile): void {
    const ids = profile.criteria.map(({ id }) => id);
    assertCanonicalEqual(
      ids,
      CONTRACT_CLOSURE_CRITERIA,
      "/profile/criteria",
      `Contract closure ${spec.version} must retain the exact ordered seven-item inventory.`,
    );
    assertNoSelfReference(profile);
    const v2 = readV2Registry(root);
    assertCanonicalEqual(
      profile.criteria.slice(0, 5),
      v2.profile.spec.criteria.slice(0, 5),
      "/profile/criteria/0-4",
      `Contract closure ${spec.version} criteria 0-4 must carry forward v2 satisfied semantics exactly.`,
    );
    const runtime = profile.criteria[5];
    const supply = profile.criteria[6];
    const expectedRuntime = (buildSourceWithoutEvidence(v2).profile.criteria[5] ??
      {}) as ContractClosureCriterion;
    const expectedSupply = (buildSourceWithoutEvidence(v2).profile.criteria[6] ??
      {}) as ContractClosureCriterion;
    assertCanonicalEqual(
      runtime,
      expectedRuntime,
      "/profile/criteria/5",
      "Runtime criterion must be the exact reviewed bounded SATISFIED_CANDIDATE.",
    );
    assertCanonicalEqual(
      supply,
      expectedSupply,
      "/profile/criteria/6",
      "Supply criterion must remain exact REVIEW_PENDING with no criterion review.",
    );
    assertCanonicalEqual(
      profile.implementationBoundary,
      expectedImplementationBoundary(),
      "/profile/implementationBoundary",
      `Contract closure ${spec.version} implementation and all-Gates-open boundary drifted.`,
    );
    const missing = deriveMissing(profile);
    assertCanonicalEqual(
      missing,
      CONTRACT_CLOSURE_MISSING,
      "/profile/criteria",
      `Contract closure ${spec.version} must derive exactly the one review-pending supply criterion.`,
    );
  }

  function expectedRuntimeReviewedCandidate(): JsonRecord {
    const lineage = spec.runtimeGitLineage;
    return {
      criterionId: "runtime-server-path-and-tenant-authority-enforcement",
      candidate: {
        commit: lineage.candidateCommit,
        tree: lineage.candidateTree,
        parent: lineage.candidateParent,
        diffSha256: `sha256:${lineage.candidateDiffSha256}`,
      },
      moduleFiles: spec.runtimeFiles.map(({ path, sha256, sizeBytes }) => ({
        path,
        sha256,
        sizeBytes,
      })),
      review: {
        path: spec.runtimeReviewFile.path,
        sha256: `sha256:${spec.runtimeReviewFile.sha256}`,
        verdict: REQUIRED_VERDICT,
        commit: lineage.reviewCommit,
        tree: lineage.reviewTree,
        parent: lineage.reviewParent,
      },
      implementationBoundary: {
        transport: "TRANSPORT_NEUTRAL_CLAIM_ONLY",
        http: "NOT_IMPLEMENTED",
        oidc: "NOT_IMPLEMENTED",
        jwks: "NOT_IMPLEMENTED",
        projectWriter: "NOT_IMPLEMENTED",
        provider: "NOT_IMPLEMENTED",
        externalEffects: "NOT_IMPLEMENTED",
      },
    };
  }

  function assertNoSelfReference(profile: ContractClosureProfile): void {
    const forbidden = new Set([
      paths.source,
      paths.output,
      paths.sourceSchema,
      paths.outputSchema,
      `scripts/lib/platform-contract-closure-profile-${spec.version}.ts`,
      `scripts/lib/platform-contract-closure-profile-${spec.version}.test.ts`,
      FUTURE_SUPPLY_V2_PROFILE_PATH,
      FUTURE_SUPPLY_V2_REVIEW_PATH,
      "tools/contract-review-binding/v1/review-tuple.json",
      "tools/contract-review-binding/v1/registry.json",
      "docs/plan/p1/g-contract-detached-review-binding-independent-review-20260824.md",
    ]);
    for (const [index, criterion] of profile.criteria.entries()) {
      for (const path of [
        ...criterion.authorityPaths,
        ...criterion.evidencePaths,
        ...(criterion.review ? [criterion.review.path] : []),
      ]) {
        if (forbidden.has(path)) {
          throw closureError(
            "SELF_REFERENCE",
            `/profile/criteria/${index}`,
            `Contract closure ${spec.version} criterion must not read successor, late-review, or self-referential path ${path}.`,
          );
        }
      }
    }
  }

  function readV2Registry(root: string): V2Registry {
    try {
      const bytes = readStableContainedRegularFile(root, V2_OUTPUT_PATH);
      const authority = CONTRACT_CLOSURE_V2_IMMUTABLE_FILES.find(
        (record) => record.path === V2_OUTPUT_PATH,
      );
      if (
        authority === undefined ||
        bytes.byteLength !== authority.sizeBytes ||
        sha256(bytes) !== authority.sha256
      ) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${V2_OUTPUT_PATH}`,
          "Contract closure v2 derived-read bytes do not match the fixed immutable output authority.",
        );
      }
      return JSON.parse(bytes.toString("utf8")) as V2Registry;
    } catch (error) {
      if (error instanceof spec.Error) throw error;
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${V2_OUTPUT_PATH}`,
        `Contract closure v2 registry is missing or invalid: ${String(error)}.`,
      );
    }
  }

  function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
    const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
    ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
    for (const path of [paths.sourceSchema, paths.outputSchema]) {
      ajv.addSchema(JSON.parse(readStableContainedRegularFile(root, path).toString("utf8")));
    }
    const validate = ajv.getSchema(schemaId);
    if (!validate) {
      throw closureError(
        "SCHEMA_INVALID",
        "/",
        `Contract closure ${spec.version} schema ${schemaId} is not registered.`,
      );
    }
    if (!validate(value)) {
      throw closureError(
        "SCHEMA_INVALID",
        "/",
        `Contract closure ${spec.version} schema validation failed: ${ajv.errorsText(validate.errors)}.`,
      );
    }
  }

  function assertCanonicalEqual(
    actual: unknown,
    expected: unknown,
    path: string,
    message: string,
  ): void {
    if (!Buffer.from(canonicalizeJson(actual)).equals(Buffer.from(canonicalizeJson(expected)))) {
      throw closureError("IDENTITY_MISMATCH", path, message);
    }
  }

  function readStableContainedRegularFile(root: string, path: string): Buffer {
    return readStableContainedRegularFileSnapshot(root, path).bytes;
  }

  function writeContainedRegularFile(root: string, path: string, contents: string): void {
    const rootReal = realpathSync(root);
    if (
      path.length === 0 ||
      isAbsolute(path) ||
      path.includes("\\") ||
      path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
    ) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} write path must be canonical and repository-relative.`,
      );
    }
    const absolute = resolve(rootReal, ...path.split("/"));
    const relation = relative(rootReal, absolute);
    if (
      relation === "" ||
      relation === ".." ||
      relation.startsWith(`..${sep}`) ||
      isAbsolute(relation)
    ) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} write path escapes its repository root.`,
      );
    }

    let current = rootReal;
    const segments = path.split("/");
    for (const [index, segment] of segments.entries()) {
      current = resolve(current, segment);
      const final = index === segments.length - 1;
      try {
        const stat = lstatSync(current);
        if (
          stat.isSymbolicLink() ||
          (!final && !stat.isDirectory()) ||
          (final && !stat.isFile()) ||
          realpathSync(current) !== current
        ) {
          throw new Error("path topology is not a contained regular destination");
        }
      } catch (error) {
        if (final && error instanceof Error && "code" in error && error.code === "ENOENT") break;
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${path}`,
          `Contract closure ${spec.version} write destination is unsafe: ${String(error)}.`,
        );
      }
    }

    let descriptor: number | undefined;
    try {
      descriptor = openSync(
        absolute,
        constants.O_WRONLY | constants.O_CREAT | constants.O_TRUNC | constants.O_NOFOLLOW,
        0o644,
      );
      writeFileSync(descriptor, contents, { encoding: "utf8" });
    } catch (error) {
      if (error instanceof spec.Error) throw error;
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} write destination could not be opened safely: ${String(error)}.`,
      );
    } finally {
      if (descriptor !== undefined) closeSync(descriptor);
    }
  }

  function readStableContainedRegularFileSnapshot(
    root: string,
    path: string,
    expectedRootReal?: string,
  ): FileSnapshot {
    const rootReal = realpathSync(root);
    if (expectedRootReal !== undefined && rootReal !== expectedRootReal) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} repository root changed during source/output capture.`,
      );
    }
    if (
      path.length === 0 ||
      isAbsolute(path) ||
      path.includes("\\") ||
      path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
    ) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} evidence path must be canonical and repository-relative.`,
      );
    }
    const absolute = resolve(rootReal, ...path.split("/"));
    const relation = relative(rootReal, absolute);
    if (
      relation === "" ||
      relation === ".." ||
      relation.startsWith(`..${sep}`) ||
      isAbsolute(relation)
    ) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} evidence path escapes its repository root.`,
      );
    }
    try {
      const pathBefore = lstatSync(absolute, { bigint: true });
      if (
        !pathBefore.isFile() ||
        pathBefore.isSymbolicLink() ||
        realpathSync(absolute) !== absolute
      ) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${path}`,
          `Contract closure ${spec.version} evidence must be a regular non-symlink file.`,
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
          throw closureError(
            "EVIDENCE_MISMATCH",
            `/${path}`,
            `Contract closure ${spec.version} evidence changed before it could be opened.`,
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
          throw closureError(
            "EVIDENCE_MISMATCH",
            `/${path}`,
            `Contract closure ${spec.version} evidence changed while it was being read.`,
          );
        }
        return {
          rootReal,
          path,
          absolute,
          bytes,
          dev: descriptorAfter.dev,
          ino: descriptorAfter.ino,
          size: descriptorAfter.size,
          mtimeNs: descriptorAfter.mtimeNs,
          ctimeNs: descriptorAfter.ctimeNs,
        };
      } finally {
        closeSync(descriptor);
      }
    } catch (error) {
      if (error instanceof spec.Error) throw error;
      throw closureError(
        "EVIDENCE_MISMATCH",
        `/${path}`,
        `Contract closure ${spec.version} evidence is missing or unreadable: ${String(error)}.`,
      );
    }
  }

  function parseObject<T extends JsonRecord>(snapshot: FileSnapshot): T {
    let parsed: unknown;
    try {
      parsed = JSON.parse(snapshot.bytes.toString("utf8"));
    } catch (error) {
      throw closureError(
        "SCHEMA_INVALID",
        `/${snapshot.path}`,
        `Contract closure ${spec.version} file is not valid JSON: ${String(error)}.`,
      );
    }
    if (!isRecord(parsed)) {
      throw closureError(
        "SCHEMA_INVALID",
        `/${snapshot.path}`,
        `Contract closure ${spec.version} file must contain a JSON object.`,
      );
    }
    return parsed as T;
  }

  function assertSnapshotsCurrent(root: string, snapshots: readonly FileSnapshot[]): void {
    let rootReal: string;
    try {
      rootReal = realpathSync(root);
    } catch (error) {
      throw closureError(
        "EVIDENCE_MISMATCH",
        "/",
        `Contract closure ${spec.version} repository root is unavailable: ${String(error)}.`,
      );
    }
    for (const snapshot of snapshots) {
      if (rootReal !== snapshot.rootReal) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${snapshot.path}`,
          `Contract closure ${spec.version} repository root changed after capture.`,
        );
      }
      try {
        let current = rootReal;
        const segments = snapshot.path.split("/");
        for (const [index, segment] of segments.entries()) {
          current = resolve(current, segment);
          const stat = lstatSync(current, { bigint: true });
          if (
            stat.isSymbolicLink() ||
            (index < segments.length - 1 ? !stat.isDirectory() : !stat.isFile())
          ) {
            throw closureError(
              "EVIDENCE_MISMATCH",
              `/${snapshot.path}`,
              `Contract closure ${spec.version} source/output topology changed after capture.`,
            );
          }
        }
        if (current !== snapshot.absolute || realpathSync(current) !== snapshot.absolute) {
          throw closureError(
            "EVIDENCE_MISMATCH",
            `/${snapshot.path}`,
            `Contract closure ${spec.version} source/output resolved location changed after capture.`,
          );
        }
        const after = lstatSync(current, { bigint: true });
        if (
          after.dev !== snapshot.dev ||
          after.ino !== snapshot.ino ||
          after.size !== snapshot.size ||
          after.mtimeNs !== snapshot.mtimeNs ||
          after.ctimeNs !== snapshot.ctimeNs
        ) {
          throw closureError(
            "EVIDENCE_MISMATCH",
            `/${snapshot.path}`,
            `Contract closure ${spec.version} source/output changed after capture.`,
          );
        }
      } catch (error) {
        if (error instanceof spec.Error) throw error;
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${snapshot.path}`,
          `Contract closure ${spec.version} source/output is unavailable after capture: ${String(error)}.`,
        );
      }
    }
  }

  function formatJson(value: unknown, indent: number, prefixLength: number): string {
    if (value === null || typeof value !== "object") {
      const encoded = JSON.stringify(value);
      if (encoded === undefined) {
        throw closureError(
          "SCHEMA_INVALID",
          "/",
          `Contract closure ${spec.version} serialization accepts JSON values only.`,
        );
      }
      return encoded;
    }
    const padding = " ".repeat(indent);
    const childPadding = " ".repeat(indent + 2);
    if (Array.isArray(value)) {
      if (value.length === 0) return "[]";
      if (value.every((entry) => entry === null || typeof entry !== "object")) {
        const inline = `[${value.map((entry) => JSON.stringify(entry)).join(", ")}]`;
        if (indent + prefixLength + inline.length <= 100) return inline;
      }
      return `[\n${value
        .map((entry) => `${childPadding}${formatJson(entry, indent + 2, 0)}`)
        .join(",\n")}\n${padding}]`;
    }
    const entries = Object.entries(value);
    if (entries.length === 0) return "{}";
    return `{\n${entries
      .map(([key, entry]) => {
        const encodedKey = JSON.stringify(key);
        const prefix = `${childPadding}${encodedKey}: `;
        return `${prefix}${formatJson(entry, indent + 2, encodedKey.length + 2)}`;
      })
      .join(",\n")}\n${padding}}`;
  }

  function validateSource(root: string, source: S): void {
    validateAgainstSchema(root, paths.sourceSchemaId, source);
    assertPredecessorEvidence(root);
    if (successor) {
      assertCanonicalEqual(
        source.authorityBinding,
        expectedAuthorityBinding(successor),
        "/authorityBinding",
        `Contract closure ${spec.version} authority markdown identity drifted.`,
      );
    }
    assertCanonicalEqual(
      source.predecessor,
      expectedClosureV2Predecessor(),
      "/predecessor",
      `Contract closure ${spec.version} must bind the exact four-file closure-v2 predecessor map.`,
    );
    if (successor) {
      assertCanonicalEqual(
        source[supersededField],
        expectedSupersededPredecessor(successor),
        `/${supersededField}`,
        `Contract closure ${spec.version} must retain the exact immutable ${successor.supersededVersion} closure predecessor fence.`,
      );
    }
    assertCanonicalEqual(
      source.runtimeReviewedCandidate,
      expectedRuntimeReviewedCandidate(),
      "/runtimeReviewedCandidate",
      "Runtime reviewed-candidate identity, module bytes, review, or boundary drifted.",
    );
    if (successor) {
      assertCanonicalEqual(
        source.replayAuthority,
        successor.replayAuthority,
        "/replayAuthority",
        `Contract closure ${spec.version} replay source, projection, exclusion, receipt, and review authority drifted.`,
      );
    }
    assertCanonicalEqual(
      source.generatorSupplyV1Predecessor,
      expectedGeneratorSupplyV1Predecessor(),
      "/generatorSupplyV1Predecessor",
      "Generator-supply v1 predecessor, 39-member policy, lineage, review, or identities drifted.",
    );
    assertProfileSemantics(root, source.profile);
  }

  function buildRegistry(root: string, source: S): R {
    validateSource(root, source);
    const sourceDigest = domainDigest(
      `cloud-agents/contract-closure-profile/source/${spec.version}`,
      source,
    );
    const profileDigest = domainDigest(
      `cloud-agents/contract-closure-profile/profile/${spec.version}`,
      source.profile,
    );
    // Field order is serialized as-is; successor fields keep their v4 positions.
    const body: JsonRecord = {
      formatVersion: `cloud-agents-contract-closure-profile-registry/${spec.version}`,
      registryId: REGISTRY_ID,
      ...(successor
        ? {
            authorityRevision: successor.authorityRevision,
            authorityBinding: source.authorityBinding,
          }
        : {}),
      sourceDigest,
      predecessor: source.predecessor,
      ...(successor ? { [supersededField]: source[supersededField] } : {}),
      runtimeReviewedCandidate: source.runtimeReviewedCandidate,
      generatorSupplyV1Predecessor: source.generatorSupplyV1Predecessor,
      ...(successor ? { replayAuthority: source.replayAuthority } : {}),
      profile: { profileDigest, spec: source.profile },
      missing: deriveMissing(source.profile),
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
    };
    const registry = {
      ...body,
      registryDigest: domainDigest(
        `cloud-agents/contract-closure-profile/registry/${spec.version}`,
        body,
      ),
    } as R;
    assertRegistrySemantics(root, registry);
    return registry;
  }

  function assertRegistrySemantics(root: string, document: unknown): asserts document is R {
    validateAgainstSchema(root, paths.outputSchemaId, document);
    if (!isRecord(document)) {
      throw closureError(
        "SCHEMA_INVALID",
        "/",
        `Contract closure ${spec.version} registry must be an object.`,
      );
    }
    const registry = document as R;
    if (successor) {
      if (registry.authorityRevision !== successor.authorityRevision) {
        throw closureError(
          "IDENTITY_MISMATCH",
          "/authorityRevision",
          `Contract closure ${spec.version} authority revision drifted.`,
        );
      }
      assertCanonicalEqual(
        registry.authorityBinding,
        expectedAuthorityBinding(successor),
        "/authorityBinding",
        `Generated closure ${spec.version} authority markdown identity drifted.`,
      );
    }
    assertPredecessorEvidence(root);
    const expectedSource = buildSourceWithoutEvidence(readV2Registry(root));
    if (
      registry.sourceDigest !==
      domainDigest(`cloud-agents/contract-closure-profile/source/${spec.version}`, expectedSource)
    ) {
      throw closureError(
        "DIGEST_MISMATCH",
        "/sourceDigest",
        `Contract closure ${spec.version} source digest does not bind the complete canonical source authority.`,
      );
    }
    assertCanonicalEqual(
      registry.predecessor,
      expectedSource.predecessor,
      "/predecessor",
      `Generated closure ${spec.version} predecessor binding drifted.`,
    );
    if (successor) {
      assertCanonicalEqual(
        registry[supersededField],
        expectedSource[supersededField],
        `/${supersededField}`,
        `Generated closure ${spec.version} ${successor.supersededVersion} predecessor binding drifted.`,
      );
    }
    assertCanonicalEqual(
      registry.runtimeReviewedCandidate,
      expectedSource.runtimeReviewedCandidate,
      "/runtimeReviewedCandidate",
      `Generated closure ${spec.version} runtime binding drifted.`,
    );
    assertCanonicalEqual(
      registry.generatorSupplyV1Predecessor,
      expectedSource.generatorSupplyV1Predecessor,
      "/generatorSupplyV1Predecessor",
      `Generated closure ${spec.version} generator-supply predecessor binding drifted.`,
    );
    if (successor) {
      assertCanonicalEqual(
        registry.replayAuthority,
        expectedSource.replayAuthority,
        "/replayAuthority",
        `Generated closure ${spec.version} replay authority binding drifted.`,
      );
    }
    assertProfileSemantics(root, registry.profile.spec);
    const expectedMissing = deriveMissing(registry.profile.spec);
    assertCanonicalEqual(
      registry.missing,
      expectedMissing,
      "/missing",
      `Contract closure ${spec.version} missing must be derived from criterion status.`,
    );
    if (
      registry.profile.profileDigest !==
      domainDigest(
        `cloud-agents/contract-closure-profile/profile/${spec.version}`,
        registry.profile.spec,
      )
    ) {
      throw closureError(
        "DIGEST_MISMATCH",
        "/profile/profileDigest",
        `Contract closure ${spec.version} profile digest does not bind the canonical profile.`,
      );
    }
    const { registryDigest: _registryDigest, ...body } = registry;
    if (
      registry.registryDigest !==
      domainDigest(`cloud-agents/contract-closure-profile/registry/${spec.version}`, body)
    ) {
      throw closureError(
        "DIGEST_MISMATCH",
        "/registryDigest",
        `Contract closure ${spec.version} registry digest does not bind the canonical registry body.`,
      );
    }
  }

  function assertPredecessorEvidence(root: string): void {
    assertContractClosureV1Immutable(root);
    assertContractClosureV2Immutable(root);
    if (successor) assertSupersededPredecessorEvidence(successor, root);
    assertGeneratorSupplyV1PredecessorImmutable(root);
    if (successor) assertAuthorityEvidence(successor, root);
    assertImmutableFileMap(root, spec.runtimeFiles, "runtime reviewed candidate");
    assertImmutableFileMap(root, [spec.runtimeReviewFile], "runtime review");
  }

  function buildSourceWithoutEvidence(v2: V2Registry): S {
    const inheritedCriteria = v2.profile.spec.criteria.slice(0, 5).map(cloneJson);
    return {
      formatVersion: `cloud-agents-contract-closure-profile-source/${spec.version}`,
      registryId: REGISTRY_ID,
      ...(successor
        ? {
            authorityRevision: successor.authorityRevision,
            authorityBinding: expectedAuthorityBinding(successor),
          }
        : {}),
      predecessor: expectedClosureV2Predecessor(),
      ...(successor ? { [supersededField]: expectedSupersededPredecessor(successor) } : {}),
      runtimeReviewedCandidate: expectedRuntimeReviewedCandidate(),
      generatorSupplyV1Predecessor: expectedGeneratorSupplyV1Predecessor(),
      ...(successor ? { replayAuthority: successor.replayAuthority } : {}),
      profile: {
        profileId: `contract-closure-profile/${spec.version}`,
        status: "BOOTSTRAP_VALIDATED",
        notGateClosure: true,
        gateStatus: "ALL_GATES_OPEN",
        derivation: {
          missing: "criteria_status_not_satisfied_candidate",
          manualRemoval: "forbidden",
          lateReviewConsumer: "detached_non_bootstrap_registry_only",
        },
        criteria: [
          ...inheritedCriteria,
          {
            id: "runtime-server-path-and-tenant-authority-enforcement",
            status: "SATISFIED_CANDIDATE",
            authorityPaths: spec.runtimeFiles.map(({ path }) => path),
            evidencePaths: [
              spec.runtimeReviewFile.path,
              ...(successor ? [successor.authorityFile.path] : []),
            ],
            review: {
              path: spec.runtimeReviewFile.path,
              sha256: `sha256:${spec.runtimeReviewFile.sha256}`,
              verdict: REQUIRED_VERDICT,
            },
          },
          {
            id: "remaining-generator-supply-chain-review",
            status: "REVIEW_PENDING",
            authorityPaths: [
              "tools/generator-supply/v1/source.json",
              "tools/generator-supply/v1/generator-supply-profile-source-v1.schema.json",
              "tools/generator-supply/v1/generator-supply-profile-v1.schema.json",
            ],
            evidencePaths: [
              "tools/generator-supply/v1/evidence-manifest.json",
              "tools/generator-supply/v1/profile.json",
              SUPPLY_V1_REVIEW_PATH,
            ],
            reason:
              "The immutable reviewed generator-supply v1 predecessor is historical; the declared v2 successor review does not yet exist and is consumed only by the detached non-bootstrap registry.",
          },
        ],
        implementationBoundary: expectedImplementationBoundary(),
      },
    } as ContractClosureSource as S;
  }

  function expectedAuthorityBinding(binding: ContractClosureSuccessorBinding): JsonRecord {
    const authority = binding.authorityFile;
    return {
      path: authority.path,
      mode: authority.mode,
      gitBlob: authority.gitBlob,
      sha256: authority.sha256,
      sizeBytes: authority.sizeBytes,
      commit: authority.commit,
      tree: authority.tree,
    };
  }

  function expectedSupersededPredecessor(binding: ContractClosureSuccessorBinding): JsonRecord {
    return {
      profileId: `contract-closure-profile/${binding.supersededVersion}`,
      predecessorMutation: "forbidden",
      baseline: binding.supersededBaseline,
      files: binding.supersededFiles.map(({ path, gitBlob, sha256, sizeBytes }) => ({
        path,
        gitBlob,
        sha256,
        sizeBytes,
      })),
    };
  }

  function assertAuthorityEvidence(binding: ContractClosureSuccessorBinding, root: string): void {
    const authority = binding.authorityFile;
    try {
      const bytes = readStableContainedRegularFile(root, authority.path);
      if (
        bytes.byteLength !== authority.sizeBytes ||
        sha256(bytes) !== authority.sha256 ||
        gitBlobSha1(bytes) !== authority.gitBlob
      ) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/authorityBinding/${authority.path}`,
          `Contract closure ${spec.version} authority markdown bytes drifted.`,
        );
      }
      if (!existsSync(resolve(root, ".git"))) return;
      const topLevel = realpathSync(gitText(root, ["rev-parse", "--show-toplevel"]));
      const commitType = gitText(root, ["cat-file", "-t", authority.commit]);
      const tree = gitText(root, ["rev-parse", `${authority.commit}^{tree}`]);
      const blob = gitText(root, ["rev-parse", `${authority.commit}:${authority.path}`]);
      const mode = gitText(root, ["ls-tree", authority.commit, "--", authority.path]).split(
        /\s+/u,
      )[0];
      if (
        topLevel !== realpathSync(root) ||
        commitType !== "commit" ||
        tree !== authority.tree ||
        blob !== authority.gitBlob ||
        mode !== authority.mode
      ) {
        throw closureError(
          "GIT_MISMATCH",
          `/authorityBinding/${authority.path}`,
          `Contract closure ${spec.version} authority markdown Git identity drifted.`,
        );
      }
    } catch (error) {
      if (error instanceof spec.Error) throw error;
      throw closureError(
        "GIT_MISMATCH",
        `/authorityBinding/${authority.path}`,
        `Contract closure ${spec.version} authority markdown identity is unavailable or invalid: ${String(error)}.`,
      );
    }
  }

  function assertSupersededPredecessorEvidence(
    binding: ContractClosureSuccessorBinding,
    root: string,
  ): void {
    const superseded = binding.supersededVersion;
    const baseline = binding.supersededBaseline;
    for (const record of binding.supersededFiles) {
      const bytes = readStableContainedRegularFile(root, record.path);
      if (
        bytes.byteLength !== record.sizeBytes ||
        sha256(bytes) !== record.sha256 ||
        gitBlobSha1(bytes) !== record.gitBlob
      ) {
        throw closureError(
          "EVIDENCE_MISMATCH",
          `/${supersededField}/${record.path}`,
          `Contract closure ${spec.version} immutable ${superseded} predecessor bytes drifted.`,
        );
      }
    }
    if (!existsSync(resolve(root, ".git"))) return;
    try {
      if (gitText(root, ["rev-parse", `${baseline.commit}^{tree}`]) !== baseline.tree) {
        throw new Error(`${superseded} baseline tree drifted`);
      }
      for (const record of binding.supersededFiles) {
        if (gitText(root, ["rev-parse", `${baseline.commit}:${record.path}`]) !== record.gitBlob) {
          throw new Error(`${superseded} baseline blob drifted for ${record.path}`);
        }
      }
    } catch (error) {
      throw closureError(
        "GIT_MISMATCH",
        `/${supersededField}/baseline/${baseline.commit}`,
        `Contract closure ${spec.version} immutable ${superseded} baseline is unavailable or invalid: ${String(error)}.`,
      );
    }
  }

  function closureError(suffix: ContractClosureErrorSuffix, path: string, message: string): Error {
    return new spec.Error(
      `CONTRACT_CLOSURE_${spec.version.toUpperCase()}_${suffix}`,
      path,
      message,
    );
  }

  return {
    paths,
    buildSource,
    validateSource,
    assertCurrent,
    assertSourceCurrent,
    assertCurrentMutationForTest,
    assertV2DependencyABAMutationForTest,
    assertRuntimeGitLineageCurrent,
    assertRepositoryLineageCurrent,
    buildRegistry,
    assertRegistrySemantics,
    deriveMissing,
    serializeRegistry,
    serializeSource,
    writeSource,
    write,
  };
}
