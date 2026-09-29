import type { ImmutableFileRecord } from "./platform-successor-predecessor";
import {
  CONTRACT_CLOSURE_CRITERIA,
  CONTRACT_CLOSURE_MISSING,
  CONTRACT_CLOSURE_REQUIRED_VERDICT as REQUIRED_VERDICT,
  createContractClosureProfile,
  type ContractClosureCriterion,
  type ContractClosureProfile,
  type ContractClosureProfileCurrent,
  type ContractClosureRegistry,
  type ContractClosureReview,
  type ContractClosureSource,
} from "./platform-contract-closure-profile-successor";

// v3 binds the closure-v2 predecessor, the reviewed runtime candidate and the
// generator-supply v1 predecessor; everything else lives in the shared kernel.

const RUNTIME_REVIEW_PATH =
  "docs/plan/p1/g-contract-runtime-current-lineage-integration-independent-review-20260824.md";

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

const closure = createContractClosureProfile({
  version: "v3",
  Error: ContractClosureProfileV3Error as new (
    code: string,
    path: string,
    message: string,
  ) => Error,
  runtimeGitLineage: CONTRACT_CLOSURE_V3_RUNTIME_GIT_LINEAGE,
  runtimeFiles: CONTRACT_CLOSURE_V3_RUNTIME_FILES,
  runtimeReviewFile: CONTRACT_CLOSURE_V3_RUNTIME_REVIEW_FILE,
});

export type ContractClosureV3Review = ContractClosureReview;
export type ContractClosureV3Criterion = ContractClosureCriterion;
export type ContractClosureV3Profile = ContractClosureProfile;
export type ContractClosureV3Source = ContractClosureSource;
export type ContractClosureV3Registry = ContractClosureRegistry;
export type ContractClosureProfileV3Current = ContractClosureProfileCurrent;

export const CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH = closure.paths.source;
export const CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_PATH = closure.paths.output;
export const CONTRACT_CLOSURE_PROFILE_V3_SOURCE_SCHEMA_PATH = closure.paths.sourceSchema;
export const CONTRACT_CLOSURE_PROFILE_V3_OUTPUT_SCHEMA_PATH = closure.paths.outputSchema;
export const CONTRACT_CLOSURE_V3_CRITERIA = CONTRACT_CLOSURE_CRITERIA;
export const CONTRACT_CLOSURE_V3_MISSING = CONTRACT_CLOSURE_MISSING;

export const buildContractClosureProfileV3Source = closure.buildSource;
/** @deprecated Use buildContractClosureProfileV3Source for canonical production generation. */
export const buildContractClosureProfileV3TestSource = buildContractClosureProfileV3Source;
export const validateContractClosureProfileV3Source = closure.validateSource;
export const assertContractClosureProfileV3Current = closure.assertCurrent;
export const assertContractClosureProfileV3SourceCurrent = closure.assertSourceCurrent;
export const assertContractClosureProfileV3CurrentMutationForTest =
  closure.assertCurrentMutationForTest;
export const assertContractClosureV3V2DependencyABAMutationForTest =
  closure.assertV2DependencyABAMutationForTest;
export const assertContractClosureV3RuntimeGitLineageCurrent =
  closure.assertRuntimeGitLineageCurrent;
export const assertContractClosureV3RepositoryLineageCurrent =
  closure.assertRepositoryLineageCurrent;
export const buildContractClosureProfileV3Registry = closure.buildRegistry;
export const assertContractClosureV3RegistrySemantics: (
  root: string,
  document: unknown,
) => asserts document is ContractClosureV3Registry = closure.assertRegistrySemantics;
export const deriveContractClosureV3Missing = closure.deriveMissing;
export const serializeContractClosureProfileV3Registry = closure.serializeRegistry;
export const serializeContractClosureProfileV3Source = closure.serializeSource;
export const writeContractClosureProfileV3Source = closure.writeSource;
export const writeContractClosureProfileV3 = closure.write;
