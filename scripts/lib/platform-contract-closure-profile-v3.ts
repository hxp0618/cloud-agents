import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
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

export const CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/contract-closure-profile-source-v3.json";
export const CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json";
export const CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json";
export const CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json";

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-source-v3.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/contract-closure-profile-v3.schema.json";
const REGISTRY_ID = "cloud-agents/platform/contract-closure-profile";
const V2_OUTPUT_PATH = "contracts/generated/platform/v1alpha1/contract-closure-profile-v2.json";
const RUNTIME_REVIEW_PATH =
  "docs/plan/p1/g-contract-runtime-current-lineage-integration-independent-review-20260824.md";
const SUPPLY_V1_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-independent-review-20260824.md";
const FUTURE_SUPPLY_V2_PROFILE_PATH = "tools/generator-supply/v2/profile.json";
const FUTURE_SUPPLY_V2_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-v2-independent-review-20260824.md";
const REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0";

export const CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE = {
  candidateCommit: "b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e",
  candidateTree: "2165fd70efd097e7e1decb109cee31e9f6af8ee5",
  candidateParent: "9fe7338d3c424731e0b9946f5252e3f61d5326a9",
  candidateDiffSha256: "d4e6e96595d9d1554356e30878ce4d57143efb579d5a369ebf97c085f3f67562",
  reviewCommit: "fe59f0d4059632a102171d9c1eb77a4c147ae65e",
  reviewTree: "7d6f7a65f36c89fadbe02e7e75e3b395bcca97f3",
  reviewParent: "b3eda9e7cc97225c1e2256ee27e0c07c8dbd462e",
  reviewPath: RUNTIME_REVIEW_PATH,
  reviewSha256: "d75212ba6880f91b33fa52f20011e79af962cdb99cc29a27313685211f204ad2",
  verdict: REQUIRED_VERDICT,
} as const;

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
  "diff.external=",
  "-c",
  "diff.mnemonicPrefix=false",
  "-c",
  "diff.noprefix=false",
  "-c",
  "diff.renames=false",
] as const;

export const CONTRACT_CLOSURE_V3_CRITERIA = [
  "json-schema-2020-12-official-test-suite",
  "openapi-3.1-semantic-validation",
  "generated-sdk-replay",
  "n-minus-one-compatibility",
  "response-watch-unknown-field-preservation",
  "runtime-server-path-and-tenant-authority-enforcement",
  "remaining-generator-supply-chain-review",
] as const;

export const CONTRACT_CLOSURE_V3_MISSING = ["remaining-generator-supply-chain-review"] as const;

export const CONTRACT_CLOSURE_V3_RUNTIME_FILES = [
  {
    path: "services/control-plane/go.mod",
    sha256: "1664dce4a62ceca72a721690b80aa77d069372229b42aebade535c140499f4ad",
    sizeBytes: 519,
  },
  {
    path: "services/control-plane/go.sum",
    sha256: "f85e74742ea1cbbe7622488afabfa567445f2ad45bf75173840d699ef275dc65",
    sizeBytes: 2858,
  },
] as const satisfies readonly ImmutableFileRecord[];

export const CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE = {
  path: RUNTIME_REVIEW_PATH,
  sha256: "d75212ba6880f91b33fa52f20011e79af962cdb99cc29a27313685211f204ad2",
  sizeBytes: 8125,
} as const satisfies ImmutableFileRecord;

export type ContractClosureV3Review = JsonRecord & {
  readonly path: string;
  readonly sha256: string;
  readonly verdict: string;
};

export type ContractClosureV3Criterion = JsonRecord & {
  readonly id: (typeof CONTRACT_CLOSURE_V3_CRITERIA)[number];
  readonly status: "SATISFIED_CANDIDATE" | "REVIEW_PENDING";
  readonly authorityPaths: readonly string[];
  readonly evidencePaths: readonly string[];
  readonly review?: ContractClosureV3Review;
  readonly reason?: string;
};

export type ContractClosureV3Profile = JsonRecord & {
  readonly profileId: "contract-closure-profile/v3";
  readonly status: "BOOTSTRAP_VALIDATED";
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly criteria: readonly ContractClosureV3Criterion[];
};

export type ContractClosureV3Source = JsonRecord & {
  readonly formatVersion: "cloud-agents-contract-closure-profile-source/v3";
  readonly registryId: typeof REGISTRY_ID;
  readonly predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly profile: ContractClosureV3Profile;
};

export type ContractClosureV3Registry = JsonRecord & {
  readonly formatVersion: "cloud-agents-contract-closure-profile-registry/v3";
  readonly registryId: typeof REGISTRY_ID;
  readonly sourceDigest: string;
  readonly predecessor: JsonRecord;
  readonly runtimeReviewedCandidate: JsonRecord;
  readonly generatorSupplyV1Predecessor: JsonRecord;
  readonly profile: JsonRecord & {
    readonly profileDigest: string;
    readonly spec: ContractClosureV3Profile;
  };
  readonly missing: readonly string[];
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly registryDigest: string;
};

export class ContractClosureProfileV3Error extends Error {
  constructor(
    readonly code:
      | "CONTRACT_CLOSURE_V3_SCHEMA_INVALID"
      | "CONTRACT_CLOSURE_V3_IDENTITY_MISMATCH"
      | "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH"
      | "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH"
      | "CONTRACT_CLOSURE_V3_GIT_MISMATCH"
      | "CONTRACT_CLOSURE_V3_SELF_REFERENCE",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ContractClosureProfileV3Error";
  }
}

export function buildContractClosureProfileV3TestSource(root: string): ContractClosureV3Source {
  assertContractClosureV2Immutable(root);
  return buildContractClosureProfileV3TestSourceWithoutEvidence(readV2Registry(root));
}

export function validateContractClosureProfileV3Source(
  root: string,
  source: ContractClosureV3Source,
): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  assertPredecessorEvidence(root);
  assertCanonicalEqual(
    source.predecessor,
    expectedClosureV2Predecessor(),
    "/predecessor",
    "Contract closure v3 must bind the exact four-file closure-v2 predecessor map.",
  );
  assertCanonicalEqual(
    source.runtimeReviewedCandidate,
    expectedRuntimeReviewedCandidate(),
    "/runtimeReviewedCandidate",
    "Runtime reviewed-candidate identity, module bytes, review, or boundary drifted.",
  );
  assertCanonicalEqual(
    source.generatorSupplyV1Predecessor,
    expectedGeneratorSupplyV1Predecessor(),
    "/generatorSupplyV1Predecessor",
    "Generator-supply v1 predecessor, 39-member policy, lineage, review, or identities drifted.",
  );
  assertProfileSemantics(root, source.profile);
}

export function assertContractClosureV3RuntimeGitLineageCurrent(root: string): void {
  const repositoryRoot = realpathSync(root);
  const lineage = CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE;
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
    const moduleBytesCurrent = CONTRACT_CLOSURE_V3_RUNTIME_FILES.map((file) =>
      readStableContainedRegularFile(repositoryRoot, file.path),
    );
    const moduleBytesAtCandidate = CONTRACT_CLOSURE_V3_RUNTIME_FILES.map((file) =>
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
          sha256(bytes) !== CONTRACT_CLOSURE_V3_RUNTIME_FILES[index]!.sha256,
      )
    ) {
      throw v3Error(
        "CONTRACT_CLOSURE_V3_GIT_MISMATCH",
        `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
        "Contract closure v3 runtime candidate or closed-pair review Git lineage drifted.",
      );
    }
  } catch (error) {
    if (error instanceof ContractClosureProfileV3Error) throw error;
    throw v3Error(
      "CONTRACT_CLOSURE_V3_GIT_MISMATCH",
      `/runtimeReviewedCandidate/candidate/${lineage.candidateCommit}`,
      `Contract closure v3 runtime Git lineage is unavailable or invalid: ${String(error)}.`,
    );
  }
}

export function assertContractClosureV3RepositoryLineageCurrent(root: string): void {
  assertContractClosureV3RuntimeGitLineageCurrent(root);
  assertGeneratorSupplyV1GitLineageCurrent(root);
}

export function buildContractClosureProfileV3Registry(
  root: string,
  source: ContractClosureV3Source,
): ContractClosureV3Registry {
  validateContractClosureProfileV3Source(root, source);
  const sourceDigest = domainDigest("cloud-agents/contract-closure-profile/source/v3", source);
  const profileDigest = domainDigest(
    "cloud-agents/contract-closure-profile/profile/v3",
    source.profile,
  );
  const body: JsonRecord = {
    formatVersion: "cloud-agents-contract-closure-profile-registry/v3",
    registryId: REGISTRY_ID,
    sourceDigest,
    predecessor: source.predecessor,
    runtimeReviewedCandidate: source.runtimeReviewedCandidate,
    generatorSupplyV1Predecessor: source.generatorSupplyV1Predecessor,
    profile: { profileDigest, spec: source.profile },
    missing: deriveContractClosureV3Missing(source.profile),
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  const registry = {
    ...body,
    registryDigest: domainDigest("cloud-agents/contract-closure-profile/registry/v3", body),
  } as ContractClosureV3Registry;
  assertContractClosureV3RegistrySemantics(root, registry);
  return registry;
}

export function assertContractClosureV3RegistrySemantics(
  root: string,
  document: unknown,
): asserts document is ContractClosureV3Registry {
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, document);
  if (!isRecord(document)) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_SCHEMA_INVALID",
      "/",
      "Contract closure v3 registry must be an object.",
    );
  }
  const registry = document as ContractClosureV3Registry;
  assertPredecessorEvidence(root);
  const expectedSource = buildContractClosureProfileV3TestSourceWithoutEvidence(
    readV2Registry(root),
  );
  if (
    registry.sourceDigest !==
    domainDigest("cloud-agents/contract-closure-profile/source/v3", expectedSource)
  ) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH",
      "/sourceDigest",
      "Contract closure v3 source digest does not bind the complete canonical source authority.",
    );
  }
  assertCanonicalEqual(
    registry.predecessor,
    expectedSource.predecessor,
    "/predecessor",
    "Generated closure v3 predecessor binding drifted.",
  );
  assertCanonicalEqual(
    registry.runtimeReviewedCandidate,
    expectedSource.runtimeReviewedCandidate,
    "/runtimeReviewedCandidate",
    "Generated closure v3 runtime binding drifted.",
  );
  assertCanonicalEqual(
    registry.generatorSupplyV1Predecessor,
    expectedSource.generatorSupplyV1Predecessor,
    "/generatorSupplyV1Predecessor",
    "Generated closure v3 generator-supply predecessor binding drifted.",
  );
  assertProfileSemantics(root, registry.profile.spec);
  const expectedMissing = deriveContractClosureV3Missing(registry.profile.spec);
  assertCanonicalEqual(
    registry.missing,
    expectedMissing,
    "/missing",
    "Contract closure v3 missing must be derived from criterion status.",
  );
  if (
    registry.profile.profileDigest !==
    domainDigest("cloud-agents/contract-closure-profile/profile/v3", registry.profile.spec)
  ) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH",
      "/profile/profileDigest",
      "Contract closure v3 profile digest does not bind the canonical profile.",
    );
  }
  const { registryDigest: _registryDigest, ...body } = registry;
  if (
    registry.registryDigest !==
    domainDigest("cloud-agents/contract-closure-profile/registry/v3", body)
  ) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_DIGEST_MISMATCH",
      "/registryDigest",
      "Contract closure v3 registry digest does not bind the canonical registry body.",
    );
  }
}

export function deriveContractClosureV3Missing(profile: ContractClosureV3Profile): string[] {
  return profile.criteria
    .filter((criterion) => criterion.status !== "SATISFIED_CANDIDATE")
    .map((criterion) => criterion.id);
}

export function serializeContractClosureProfileV3Registry(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

function assertProfileSemantics(root: string, profile: ContractClosureV3Profile): void {
  const ids = profile.criteria.map(({ id }) => id);
  assertCanonicalEqual(
    ids,
    CONTRACT_CLOSURE_V3_CRITERIA,
    "/profile/criteria",
    "Contract closure v3 must retain the exact ordered seven-item inventory.",
  );
  assertNoSelfReference(profile);
  const v2 = readV2Registry(root);
  assertCanonicalEqual(
    profile.criteria.slice(0, 5),
    v2.profile.spec.criteria.slice(0, 5),
    "/profile/criteria/0-4",
    "Contract closure v3 criteria 0-4 must carry forward v2 satisfied semantics exactly.",
  );
  const runtime = profile.criteria[5];
  const supply = profile.criteria[6];
  const expectedRuntime = (buildContractClosureProfileV3TestSourceWithoutEvidence(v2).profile
    .criteria[5] ?? {}) as ContractClosureV3Criterion;
  const expectedSupply = (buildContractClosureProfileV3TestSourceWithoutEvidence(v2).profile
    .criteria[6] ?? {}) as ContractClosureV3Criterion;
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
    "Contract closure v3 implementation and all-Gates-open boundary drifted.",
  );
  const missing = deriveContractClosureV3Missing(profile);
  assertCanonicalEqual(
    missing,
    CONTRACT_CLOSURE_V3_MISSING,
    "/profile/criteria",
    "Contract closure v3 must derive exactly the one review-pending supply criterion.",
  );
}

function buildContractClosureProfileV3TestSourceWithoutEvidence(
  v2: V2Registry,
): ContractClosureV3Source {
  const inheritedCriteria = v2.profile.spec.criteria.slice(0, 5).map(cloneJson);
  return {
    formatVersion: "cloud-agents-contract-closure-profile-source/v3",
    registryId: REGISTRY_ID,
    predecessor: expectedClosureV2Predecessor(),
    runtimeReviewedCandidate: expectedRuntimeReviewedCandidate(),
    generatorSupplyV1Predecessor: expectedGeneratorSupplyV1Predecessor(),
    profile: {
      profileId: "contract-closure-profile/v3",
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
          authorityPaths: CONTRACT_CLOSURE_V3_RUNTIME_FILES.map(({ path }) => path),
          evidencePaths: [RUNTIME_REVIEW_PATH],
          review: {
            path: RUNTIME_REVIEW_PATH,
            sha256: `sha256:${CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE.sha256}`,
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
  } as ContractClosureV3Source;
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

function expectedRuntimeReviewedCandidate(): JsonRecord {
  const lineage = CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE;
  return {
    criterionId: "runtime-server-path-and-tenant-authority-enforcement",
    candidate: {
      commit: lineage.candidateCommit,
      tree: lineage.candidateTree,
      parent: lineage.candidateParent,
      diffSha256: `sha256:${lineage.candidateDiffSha256}`,
    },
    moduleFiles: CONTRACT_CLOSURE_V3_RUNTIME_FILES.map(({ path, sha256, sizeBytes }) => ({
      path,
      sha256,
      sizeBytes,
    })),
    review: {
      path: RUNTIME_REVIEW_PATH,
      sha256: `sha256:${CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE.sha256}`,
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
      reviewParent: GENERATOR_SUPPLY_V1_GIT_LINEAGE.candidateCommit,
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

function assertPredecessorEvidence(root: string): void {
  assertContractClosureV1Immutable(root);
  assertContractClosureV2Immutable(root);
  assertGeneratorSupplyV1PredecessorImmutable(root);
  assertImmutableFileMap(root, CONTRACT_CLOSURE_V3_RUNTIME_FILES, "runtime reviewed candidate");
  assertImmutableFileMap(root, [CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE], "runtime review");
}

function assertNoSelfReference(profile: ContractClosureV3Profile): void {
  const forbidden = new Set([
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
    "scripts/lib/platform-contract-closure-profile-v3.ts",
    "scripts/lib/platform-contract-closure-profile-v3.test.ts",
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
        throw v3Error(
          "CONTRACT_CLOSURE_V3_SELF_REFERENCE",
          `/profile/criteria/${index}`,
          `Contract closure v3 criterion must not read successor, late-review, or self-referential path ${path}.`,
        );
      }
    }
  }
}

type V2Registry = {
  readonly profile: {
    readonly spec: {
      readonly criteria: readonly ContractClosureV3Criterion[];
    };
  };
};

function readV2Registry(root: string): V2Registry {
  try {
    return JSON.parse(
      readStableContainedRegularFile(root, V2_OUTPUT_PATH).toString("utf8"),
    ) as V2Registry;
  } catch (error) {
    if (error instanceof ContractClosureProfileV3Error) throw error;
    throw v3Error(
      "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
      `/${V2_OUTPUT_PATH}`,
      `Contract closure v2 registry is missing or invalid: ${String(error)}.`,
    );
  }
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  for (const path of [
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(JSON.parse(readStableContainedRegularFile(root, path).toString("utf8")));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_SCHEMA_INVALID",
      "/",
      `Contract closure v3 schema ${schemaId} is not registered.`,
    );
  }
  if (!validate(value)) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_SCHEMA_INVALID",
      "/",
      `Contract closure v3 schema validation failed: ${ajv.errorsText(validate.errors)}.`,
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
    throw v3Error("CONTRACT_CLOSURE_V3_IDENTITY_MISMATCH", path, message);
  }
}

function readStableContainedRegularFile(root: string, path: string): Buffer {
  const rootReal = realpathSync(root);
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw v3Error(
      "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v3 evidence path must be canonical and repository-relative.",
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
    throw v3Error(
      "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
      `/${path}`,
      "Contract closure v3 evidence path escapes its repository root.",
    );
  }
  try {
    const pathBefore = lstatSync(absolute, { bigint: true });
    if (
      !pathBefore.isFile() ||
      pathBefore.isSymbolicLink() ||
      realpathSync(absolute) !== absolute
    ) {
      throw v3Error(
        "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
        `/${path}`,
        "Contract closure v3 evidence must be a regular non-symlink file.",
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
        throw v3Error(
          "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
          `/${path}`,
          "Contract closure v3 evidence changed before it could be opened.",
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
        throw v3Error(
          "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
          `/${path}`,
          "Contract closure v3 evidence changed while it was being read.",
        );
      }
      return bytes;
    } finally {
      closeSync(descriptor);
    }
  } catch (error) {
    if (error instanceof ContractClosureProfileV3Error) throw error;
    throw v3Error(
      "CONTRACT_CLOSURE_V3_EVIDENCE_MISMATCH",
      `/${path}`,
      `Contract closure v3 evidence is missing or unreadable: ${String(error)}.`,
    );
  }
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

function v3Error(
  code: ContractClosureProfileV3Error["code"],
  path: string,
  message: string,
): ContractClosureProfileV3Error {
  return new ContractClosureProfileV3Error(code, path, message);
}
