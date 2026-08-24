import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  closeSync,
  constants,
  fstatSync,
  fsyncSync,
  linkSync,
  lstatSync,
  openSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import {
  assertContractClosureV3RegistrySemantics,
  CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
  type ContractClosureV3Source,
  validateContractClosureProfileV3Source,
} from "./platform-contract-closure-profile-v3";
import {
  assertGeneratorSupplyV2RegistryCurrent,
  GENERATOR_SUPPLY_V2_SOURCE_PATH,
  type GeneratorSupplyV2CurrentValidation,
} from "./platform-generator-supply-profile-v2";
import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";
import {
  SUCCESSOR_ASSEMBLED_REVIEW_PATHS,
  SUCCESSOR_BINDING_LATE_PATHS,
} from "./platform-successor-dag";

export const CONTRACT_REVIEW_BINDING_SOURCE_PATH = "tools/contract-review-binding/v1/source.json";
export const CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH =
  "tools/contract-review-binding/v1/review-binding-source-v1.schema.json";
export const CONTRACT_REVIEW_TUPLE_SCHEMA_PATH =
  "tools/contract-review-binding/v1/review-tuple-v1.schema.json";
export const CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH =
  "tools/contract-review-binding/v1/review-binding-registry-v1.schema.json";
export const CONTRACT_REVIEW_TUPLE_PATH = SUCCESSOR_BINDING_LATE_PATHS[0];
export const CONTRACT_REVIEW_BINDING_OUTPUT_PATH = SUCCESSOR_BINDING_LATE_PATHS[1];
export const CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH = SUCCESSOR_BINDING_LATE_PATHS[2];

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/contract-review-binding/v1/review-binding-source-v1.schema.json";
const TUPLE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/contract-review-binding/v1/review-tuple-v1.schema.json";
const REGISTRY_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/contract-review-binding/v1/review-binding-registry-v1.schema.json";
const REGISTRY_ID = "cloud-agents/platform/contract-review-binding";
const BINDING_ID = "g-contract-current-source-review-binding/v1";
const REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0";
const CANONICAL_CLOSURE_PATH =
  "contracts/generated/platform/v1alpha1/contract-closure-profile-v3.json";
const CANONICAL_CLOSURE_PROFILE_ID = "contract-closure-profile/v3";
const SUPPLY_PROFILE_PATH = "tools/generator-supply/v2/profile.json";
const SUPPLY_PROFILE_ID = "cloud-agents/generator-supply-profile/v2";
export const CONTRACT_CLOSURE_V3_REVIEW_PATH = SUCCESSOR_ASSEMBLED_REVIEW_PATHS[0];
export const GENERATOR_SUPPLY_V2_REVIEW_PATH = SUCCESSOR_ASSEMBLED_REVIEW_PATHS[1];
const REVIEW_IDENTITIES = [
  {
    criterionId: "runtime-server-path-and-tenant-authority-enforcement",
    subject: "canonical_contract_closure",
    reviewPath: CONTRACT_CLOSURE_V3_REVIEW_PATH,
  },
  {
    criterionId: "remaining-generator-supply-chain-review",
    subject: "generator_supply_profile",
    reviewPath: GENERATOR_SUPPLY_V2_REVIEW_PATH,
  },
] as const;

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

type SubjectKind = (typeof REVIEW_IDENTITIES)[number]["subject"];

export type ReviewBindingAuthority = JsonRecord & {
  readonly kind: SubjectKind;
  readonly path: string;
  readonly profileId: string;
};

export type BoundReviewBindingAuthority = ReviewBindingAuthority & {
  readonly fileSha256: string;
  readonly profileDigest: string;
  readonly registryDigest: string;
};

export type ContractReviewBindingSource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly bindingId: string;
  readonly canonicalClosureAuthority: ReviewBindingAuthority;
  readonly supplyProfileAuthority: ReviewBindingAuthority;
  readonly reviewSlots: readonly (JsonRecord & {
    readonly criterionId: string;
    readonly subject: SubjectKind;
    readonly reviewPath: string;
    readonly requiredVerdict: string;
  })[];
  readonly derivation: JsonRecord & {
    readonly manualMissingRemoval: string;
    readonly reviewTuplePath: string;
    readonly outputPath: string;
    readonly finalReviewPath: string;
  };
  readonly implementationBoundary: JsonRecord;
};

type ReviewBinding = JsonRecord & {
  readonly criterionId: string;
  readonly subject: SubjectKind;
  readonly candidate: JsonRecord & {
    readonly commit: string;
    readonly tree: string;
    readonly parent: string;
    readonly diffSha256: string;
  };
  readonly review: JsonRecord & {
    readonly commit: string;
    readonly tree: string;
    readonly parent: string;
    readonly path: string;
    readonly sha256: string;
    readonly verdict: string;
    readonly findings: JsonRecord & {
      readonly p0: number;
      readonly p1: number;
      readonly p2: number;
    };
  };
};

export type ContractReviewTuple = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly bindingId: string;
  readonly sourceDigest: string;
  readonly canonicalClosureAuthority: BoundReviewBindingAuthority;
  readonly supplyProfileAuthority: BoundReviewBindingAuthority;
  readonly reviews: readonly ReviewBinding[];
  readonly notGateClosure: boolean;
  readonly gateStatus: string;
  readonly tupleDigest: string;
};

export type ContractReviewBindingRegistry = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly bindingId: string;
  readonly sourceDigest: string;
  readonly tupleDigest: string;
  readonly bindingsDigest: string;
  readonly canonicalClosureReference: BoundReviewBindingAuthority;
  readonly supplyProfileReference: BoundReviewBindingAuthority;
  readonly effectiveCandidate: JsonRecord;
  readonly registryDigest: string;
};

export type ContractReviewBindingState =
  | { readonly kind: "PRE_REVIEW_ABSENT"; readonly source: ContractReviewBindingSource }
  | {
      readonly kind: "COMPLETE_TUPLE_READY_TO_WRITE";
      readonly source: ContractReviewBindingSource;
      readonly tuple: ContractReviewTuple;
    }
  | {
      readonly kind: "COMPLETE_TUPLE_OUTPUT_CURRENT";
      readonly source: ContractReviewBindingSource;
      readonly tuple: ContractReviewTuple;
      readonly registry: ContractReviewBindingRegistry;
    };

export class ContractReviewBindingError extends Error {
  constructor(
    readonly code:
      | "CONTRACT_REVIEW_BINDING_SOURCE_REQUIRED"
      | "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID"
      | "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH"
      | "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH"
      | "CONTRACT_REVIEW_BINDING_PARTIAL_STATE"
      | "CONTRACT_REVIEW_BINDING_OUTPUT_REQUIRED"
      | "CONTRACT_REVIEW_BINDING_OUTPUT_DRIFT",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "ContractReviewBindingError";
  }
}

export function inspectContractReviewBindingState(root: string): ContractReviewBindingState {
  const source = readAndValidateSource(root);
  const tuplePresent = pathExists(root, CONTRACT_REVIEW_TUPLE_PATH);
  const outputPresent = pathExists(root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH);

  if (!tuplePresent && !outputPresent) return { kind: "PRE_REVIEW_ABSENT", source };
  if (!tuplePresent && outputPresent) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
      "/state",
      "Detached review-binding output exists without its complete review tuple.",
    );
  }

  const tuple = readAndValidateTuple(root, source);
  if (!outputPresent) return { kind: "COMPLETE_TUPLE_READY_TO_WRITE", source, tuple };

  const registry = readJsonFile(
    root,
    CONTRACT_REVIEW_BINDING_OUTPUT_PATH,
    "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
  ) as ContractReviewBindingRegistry;
  validateAgainstSchema(root, REGISTRY_SCHEMA_ID, registry);
  const expected = buildContractReviewBindingRegistry(root, tuple, source);
  if (!canonicalEqual(registry, expected)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_OUTPUT_DRIFT",
      "/registryDigest",
      "Detached review-binding registry is stale or does not match the complete tuple.",
    );
  }
  return { kind: "COMPLETE_TUPLE_OUTPUT_CURRENT", source, tuple, registry };
}

export function writeContractReviewBinding(root: string): void {
  const state = inspectContractReviewBindingState(root);
  if (state.kind === "PRE_REVIEW_ABSENT" || state.kind === "COMPLETE_TUPLE_OUTPUT_CURRENT") return;

  const serialized = serializeContractReviewBinding(
    buildContractReviewBindingRegistry(root, state.tuple, state.source),
  );
  publishContractReviewBindingExclusive(root, serialized);
  const current = inspectContractReviewBindingState(root);
  if (current.kind !== "COMPLETE_TUPLE_OUTPUT_CURRENT") {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_OUTPUT_DRIFT",
      "/state",
      "Explicit detached review-binding write did not reach the current state.",
    );
  }
}

export function publishContractReviewBindingExclusiveForTest(
  root: string,
  serialized: string,
  beforePublish: () => void = () => {},
): void {
  publishContractReviewBindingExclusive(root, serialized, beforePublish);
}

function publishContractReviewBindingExclusive(
  root: string,
  serialized: string,
  beforePublish: () => void = () => {},
): void {
  const output = resolveContainedPath(root, CONTRACT_REVIEW_BINDING_OUTPUT_PATH, true);
  const token = `${process.pid}-${Date.now()}-${process.hrtime.bigint()}`;
  const temporary = resolve(dirname(output), `.registry.json.write-${token}`);
  let descriptor: number | undefined;
  try {
    descriptor = openSync(
      temporary,
      constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL,
      0o600,
    );
    writeFileSync(descriptor, serialized, { encoding: "utf8" });
    fsyncSync(descriptor);
    closeSync(descriptor);
    descriptor = undefined;
    beforePublish();
    try {
      linkSync(temporary, output);
    } catch (error) {
      if (error instanceof Error && "code" in error && error.code === "EEXIST") {
        throw bindingError(
          "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
          "/state",
          "Detached review-binding output appeared during the exclusive publish.",
        );
      }
      throw error;
    }
    fsyncDirectory(dirname(output));
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
    rmSync(temporary, { force: true });
  }
}

export function assertContractReviewBindingCurrentOrAbsent(root: string): void {
  const state = inspectContractReviewBindingState(root);
  if (state.kind === "COMPLETE_TUPLE_READY_TO_WRITE") {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_OUTPUT_REQUIRED",
      "/state",
      "A complete review tuple requires an explicit detached binding --write before --check.",
    );
  }
}

export function buildContractReviewBindingRegistry(
  root: string,
  tuple: ContractReviewTuple,
  suppliedSource?: ContractReviewBindingSource,
): ContractReviewBindingRegistry {
  const source = suppliedSource ?? readAndValidateSource(root);
  validateContractReviewTuple(root, source, tuple);
  const criteriaBindings = tuple.reviews.map((binding) => ({
    criterionId: binding.criterionId,
    subject: binding.subject,
    status: "SATISFIED_CANDIDATE",
    reviewSha256: binding.review.sha256,
  }));
  const bindingsDigest = domainDigest("cloud-agents/contract-review-binding/bindings/v1", {
    canonicalClosureAuthority: tuple.canonicalClosureAuthority,
    supplyProfileAuthority: tuple.supplyProfileAuthority,
    reviews: tuple.reviews,
  });
  const body: JsonRecord = {
    formatVersion: "cloud-agents-contract-review-binding-registry/v1",
    registryId: REGISTRY_ID,
    bindingId: BINDING_ID,
    sourceDigest: tuple.sourceDigest,
    tupleDigest: tuple.tupleDigest,
    bindingsDigest,
    canonicalClosureReference: tuple.canonicalClosureAuthority,
    supplyProfileReference: tuple.supplyProfileAuthority,
    effectiveCandidate: {
      status: "REVIEW_BOUND_SATISFIED_CANDIDATE",
      criteriaBindings,
      missing: [],
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
    },
  };
  const registry = {
    ...body,
    registryDigest: domainDigest("cloud-agents/contract-review-binding/registry/v1", body),
  } as ContractReviewBindingRegistry;
  validateAgainstSchema(root, REGISTRY_SCHEMA_ID, registry);
  return registry;
}

export function validateContractReviewTuple(
  root: string,
  source: ContractReviewBindingSource,
  tuple: ContractReviewTuple,
): void {
  validateAgainstSchema(root, TUPLE_SCHEMA_ID, tuple);
  if (
    tuple.registryId !== source.registryId ||
    tuple.bindingId !== source.bindingId ||
    tuple.sourceDigest !== contractReviewBindingSourceDigest(source) ||
    tuple.notGateClosure !== true ||
    tuple.gateStatus !== "ALL_GATES_OPEN"
  ) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      "/",
      "Review tuple identity, source digest, or non-Gate boundary drifted.",
    );
  }
  validateBoundAuthority(root, source.canonicalClosureAuthority, tuple.canonicalClosureAuthority);
  validateBoundAuthority(root, source.supplyProfileAuthority, tuple.supplyProfileAuthority);

  for (const [index, expected] of REVIEW_IDENTITIES.entries()) {
    const slot = source.reviewSlots[index]!;
    const binding = tuple.reviews[index]!;
    if (
      slot.criterionId !== expected.criterionId ||
      slot.subject !== expected.subject ||
      slot.reviewPath !== expected.reviewPath ||
      binding.criterionId !== slot.criterionId ||
      binding.subject !== slot.subject ||
      binding.review.path !== slot.reviewPath ||
      binding.review.verdict !== slot.requiredVerdict ||
      binding.review.parent !== binding.candidate.commit ||
      binding.review.commit === binding.candidate.commit ||
      binding.review.path === source.derivation.finalReviewPath ||
      binding.review.path === source.derivation.reviewTuplePath ||
      binding.review.path === source.derivation.outputPath
    ) {
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
        `/reviews/${index}`,
        "Review tuple ordering, subject, lineage, path, verdict, or self-review boundary drifted.",
      );
    }
    assertFileDigest(root, binding.review.path, binding.review.sha256, `/reviews/${index}/review`);
    validateReviewGitLineage(
      root,
      binding,
      binding.subject === "canonical_contract_closure"
        ? tuple.canonicalClosureAuthority
        : tuple.supplyProfileAuthority,
      index,
    );
  }

  const { tupleDigest: _tupleDigest, ...tupleBody } = tuple;
  const expectedTupleDigest = domainDigest(
    "cloud-agents/contract-review-binding/tuple/v1",
    tupleBody,
  );
  if (tuple.tupleDigest !== expectedTupleDigest) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
      "/tupleDigest",
      "Review tuple digest does not bind its complete canonical body.",
    );
  }
}

export function contractReviewBindingSourceDigest(source: ContractReviewBindingSource): string {
  return domainDigest("cloud-agents/contract-review-binding/source/v1", source);
}

export function serializeContractReviewBinding(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function contractReviewBindingAuthorityInputs(): string[] {
  return [
    CONTRACT_REVIEW_BINDING_SOURCE_PATH,
    CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
    CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
    CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
    "scripts/generate-platform-contract-review-binding.ts",
    "scripts/lib/platform-contract-closure-profile-v3.test.ts",
    "scripts/lib/platform-contract-closure-profile-v3.ts",
    "scripts/lib/platform-contract-review-binding.test.ts",
    "scripts/lib/platform-contract-review-binding.ts",
    "scripts/lib/platform-generator-supply-profile-v2.test.ts",
    "scripts/lib/platform-generator-supply-profile-v2.ts",
    "scripts/lib/platform-generator-supply-replay-v2.test.ts",
    "scripts/lib/platform-generator-supply-replay-v2.ts",
    "scripts/lib/platform-json-semantics.ts",
    "scripts/lib/platform-successor-dag.ts",
  ].toSorted();
}

export function buildContractReviewBindingTestSource(): ContractReviewBindingSource {
  return {
    formatVersion: "cloud-agents-contract-review-binding-source/v1",
    registryId: REGISTRY_ID,
    bindingId: BINDING_ID,
    canonicalClosureAuthority: {
      kind: "canonical_contract_closure",
      path: CANONICAL_CLOSURE_PATH,
      profileId: CANONICAL_CLOSURE_PROFILE_ID,
    },
    supplyProfileAuthority: {
      kind: "generator_supply_profile",
      path: SUPPLY_PROFILE_PATH,
      profileId: SUPPLY_PROFILE_ID,
    },
    reviewSlots: [
      {
        ...REVIEW_IDENTITIES[0],
        requiredVerdict: REQUIRED_VERDICT,
      },
      {
        ...REVIEW_IDENTITIES[1],
        requiredVerdict: REQUIRED_VERDICT,
      },
    ],
    derivation: {
      manualMissingRemoval: "forbidden",
      reviewTuplePath: CONTRACT_REVIEW_TUPLE_PATH,
      outputPath: CONTRACT_REVIEW_BINDING_OUTPUT_PATH,
      finalReviewPath: CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH,
    },
    implementationBoundary: {
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      bootstrapDiscovery: "FORBIDDEN",
      coreReplayOutput: "FORBIDDEN",
      selfReview: "FORBIDDEN",
      httpP2Provider: "NOT_AUTHORIZED",
      productionDatabaseWrites: "NOT_AUTHORIZED",
      deployment: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
      signing: "NOT_AUTHORIZED",
    },
  };
}

export function buildContractReviewBindingTestTuple(
  root: string,
  source: ContractReviewBindingSource,
): ContractReviewTuple {
  const reviewCommit = gitText(root, ["rev-parse", "HEAD"]);
  const candidateCommit = gitText(root, ["rev-parse", reviewCommit + "^"]);
  const candidateParent = gitText(root, ["rev-parse", candidateCommit + "^"]);
  const candidateTree = gitText(root, ["rev-parse", candidateCommit + "^{tree}"]);
  const reviewTree = gitText(root, ["rev-parse", reviewCommit + "^{tree}"]);
  const candidateDiffSha256 =
    "sha256:" +
    createHash("sha256")
      .update(
        gitBytes(root, [
          "diff",
          "--no-color",
          "--no-ext-diff",
          "--no-textconv",
          "--binary",
          "--no-renames",
          candidateParent,
          candidateCommit,
        ]),
      )
      .digest("hex");
  const reviews = source.reviewSlots.map((slot) => {
    return {
      criterionId: slot.criterionId,
      subject: slot.subject,
      candidate: {
        commit: candidateCommit,
        tree: candidateTree,
        parent: candidateParent,
        diffSha256: candidateDiffSha256,
      },
      review: {
        commit: reviewCommit,
        tree: reviewTree,
        parent: candidateCommit,
        path: slot.reviewPath,
        sha256: fileSha256(root, slot.reviewPath),
        verdict: REQUIRED_VERDICT,
        findings: { p0: 0, p1: 0, p2: 0 },
      },
    };
  });
  const body = {
    formatVersion: "cloud-agents-contract-review-tuple/v1",
    registryId: REGISTRY_ID,
    bindingId: BINDING_ID,
    sourceDigest: contractReviewBindingSourceDigest(source),
    canonicalClosureAuthority: bindAuthority(root, source.canonicalClosureAuthority),
    supplyProfileAuthority: bindAuthority(root, source.supplyProfileAuthority),
    reviews,
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  return {
    ...body,
    tupleDigest: domainDigest("cloud-agents/contract-review-binding/tuple/v1", body),
  } as ContractReviewTuple;
}

function readAndValidateSource(root: string): ContractReviewBindingSource {
  if (!pathExists(root, CONTRACT_REVIEW_BINDING_SOURCE_PATH)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_SOURCE_REQUIRED",
      "/source",
      `Detached review-binding authority source ${CONTRACT_REVIEW_BINDING_SOURCE_PATH} is required.`,
    );
  }
  const source = readJsonFile(
    root,
    CONTRACT_REVIEW_BINDING_SOURCE_PATH,
    "CONTRACT_REVIEW_BINDING_SOURCE_REQUIRED",
  ) as ContractReviewBindingSource;
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (
    source.registryId !== REGISTRY_ID ||
    source.bindingId !== BINDING_ID ||
    source.canonicalClosureAuthority.path !== CANONICAL_CLOSURE_PATH ||
    source.canonicalClosureAuthority.profileId !== CANONICAL_CLOSURE_PROFILE_ID ||
    source.supplyProfileAuthority.path !== SUPPLY_PROFILE_PATH ||
    source.supplyProfileAuthority.profileId !== SUPPLY_PROFILE_ID ||
    source.derivation.reviewTuplePath !== CONTRACT_REVIEW_TUPLE_PATH ||
    source.derivation.outputPath !== CONTRACT_REVIEW_BINDING_OUTPUT_PATH ||
    source.derivation.finalReviewPath !== CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH ||
    source.implementationBoundary.notGateClosure !== true ||
    source.implementationBoundary.gateStatus !== "ALL_GATES_OPEN" ||
    source.implementationBoundary.bootstrapDiscovery !== "FORBIDDEN" ||
    source.implementationBoundary.coreReplayOutput !== "FORBIDDEN" ||
    source.implementationBoundary.selfReview !== "FORBIDDEN"
  ) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      "/source",
      "Detached review-binding source identity, paths, or non-Gate/bootstrap boundary drifted.",
    );
  }
  const reserved = new Set([
    CONTRACT_REVIEW_BINDING_SOURCE_PATH,
    CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
    CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
    CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
    CONTRACT_REVIEW_TUPLE_PATH,
    CONTRACT_REVIEW_BINDING_OUTPUT_PATH,
    CONTRACT_REVIEW_BINDING_FINAL_REVIEW_PATH,
    CANONICAL_CLOSURE_PATH,
    CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
    SUPPLY_PROFILE_PATH,
    GENERATOR_SUPPLY_V2_SOURCE_PATH,
    "contracts/generation.lock.json",
    "scripts/generate-platform-contract-review-binding.ts",
    "scripts/lib/platform-contract-review-binding.ts",
    "scripts/lib/platform-contract-review-binding.test.ts",
    "scripts/lib/platform-json-semantics.ts",
  ]);
  for (const [index, expected] of REVIEW_IDENTITIES.entries()) {
    const slot = source.reviewSlots[index]!;
    if (
      slot.criterionId !== expected.criterionId ||
      slot.subject !== expected.subject ||
      slot.reviewPath !== expected.reviewPath ||
      slot.requiredVerdict !== REQUIRED_VERDICT ||
      reserved.has(slot.reviewPath)
    ) {
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
        `/reviewSlots/${index}`,
        "Review slot order, identity, verdict, or self-review path drifted.",
      );
    }
    reserved.add(slot.reviewPath);
  }
  return source;
}

function readAndValidateTuple(
  root: string,
  source: ContractReviewBindingSource,
): ContractReviewTuple {
  const tuple = readJsonFile(
    root,
    CONTRACT_REVIEW_TUPLE_PATH,
    "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
  ) as ContractReviewTuple;
  validateContractReviewTuple(root, source, tuple);
  return tuple;
}

function bindAuthority(
  root: string,
  authority: ReviewBindingAuthority,
): BoundReviewBindingAuthority {
  const document = readJsonFile(root, authority.path, "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH");
  const closure = authority.kind === "canonical_contract_closure";
  let generatorSupplyCurrent: GeneratorSupplyV2CurrentValidation | undefined;
  try {
    if (closure) {
      const source = readJsonFile(
        root,
        CONTRACT_CLOSURE_PROFILE_V3_SOURCE_PATH,
        "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
      ) as ContractClosureV3Source;
      validateContractClosureProfileV3Source(root, source);
      assertContractClosureV3RegistrySemantics(root, document);
    } else {
      generatorSupplyCurrent = assertGeneratorSupplyV2RegistryCurrent(root, document);
    }
  } catch (error) {
    if (error instanceof ContractReviewBindingError) throw error;
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${authority.kind}/semantics`,
      `Bound authority ${authority.path} failed its complete versioned semantic validator: ${String(error)}.`,
    );
  }
  const profile = requiredRecord(document.profile, `${authority.path} profile`);
  const spec = requiredRecord(profile.spec, `${authority.path} profile spec`);
  const expectedFormat = closure
    ? "cloud-agents-contract-closure-profile-registry/v3"
    : "cloud-agents-generator-supply-profile-registry/v2";
  const expectedRegistryId = closure
    ? "cloud-agents/platform/contract-closure-profile"
    : "cloud-agents/generator-supply-profile";
  if (
    document.formatVersion !== expectedFormat ||
    document.registryId !== expectedRegistryId ||
    spec.profileId !== authority.profileId ||
    spec.notGateClosure !== true
  ) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${authority.kind}/profileId`,
      `Bound authority ${authority.path} has the wrong format, registry, profile, digest, or non-Gate identity.`,
    );
  }
  const bound = {
    ...authority,
    fileSha256: fileSha256(root, authority.path),
    profileDigest: requiredDigest(profile.profileDigest, `${authority.path} profileDigest`),
    registryDigest: requiredDigest(document.registryDigest, `${authority.path} registryDigest`),
  };
  try {
    generatorSupplyCurrent?.assertCurrent();
  } catch (error) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${authority.kind}/snapshot`,
      `Bound authority ${authority.path} changed before its detached digest binding completed: ${String(error)}.`,
    );
  }
  return bound;
}

function validateBoundAuthority(
  root: string,
  declared: ReviewBindingAuthority,
  bound: BoundReviewBindingAuthority,
): void {
  if (
    bound.kind !== declared.kind ||
    bound.path !== declared.path ||
    bound.profileId !== declared.profileId
  ) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${declared.kind}`,
      "Review tuple authority identity differs from its source declaration.",
    );
  }
  const actual = bindAuthority(root, declared);
  if (!canonicalEqual(actual, bound)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
      `/${declared.kind}`,
      `Review tuple authority digests for ${declared.path} drifted.`,
    );
  }
}

function validateReviewGitLineage(
  root: string,
  binding: ReviewBinding,
  authority: BoundReviewBindingAuthority,
  index: number,
): void {
  const repositoryRoot = realpathSync(root);
  try {
    const topLevel = realpathSync(gitText(repositoryRoot, ["rev-parse", "--show-toplevel"]));
    const candidateType = gitText(repositoryRoot, ["cat-file", "-t", binding.candidate.commit]);
    const reviewType = gitText(repositoryRoot, ["cat-file", "-t", binding.review.commit]);
    const candidateTree = gitText(repositoryRoot, [
      "rev-parse",
      binding.candidate.commit + "^{tree}",
    ]);
    const candidateParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      binding.candidate.commit,
    ]);
    const reviewTree = gitText(repositoryRoot, ["rev-parse", binding.review.commit + "^{tree}"]);
    const reviewParents = gitText(repositoryRoot, [
      "show",
      "-s",
      "--format=%P",
      binding.review.commit,
    ]);
    const candidateDiff =
      "sha256:" +
      createHash("sha256")
        .update(
          gitBytes(repositoryRoot, [
            "diff",
            "--no-color",
            "--no-ext-diff",
            "--no-textconv",
            "--binary",
            "--no-renames",
            binding.candidate.parent,
            binding.candidate.commit,
          ]),
        )
        .digest("hex");
    const reviewBytes = gitBytes(repositoryRoot, [
      "cat-file",
      "blob",
      binding.review.commit + ":" + binding.review.path,
    ]);
    const authorityBytes = gitBytes(repositoryRoot, [
      "cat-file",
      "blob",
      binding.candidate.commit + ":" + authority.path,
    ]);
    const reviewPathBefore = gitText(repositoryRoot, [
      "ls-tree",
      "-r",
      "--name-only",
      binding.candidate.commit,
      "--",
      binding.review.path,
    ]);
    if (
      topLevel !== repositoryRoot ||
      candidateType !== "commit" ||
      reviewType !== "commit" ||
      candidateTree !== binding.candidate.tree ||
      candidateParents !== binding.candidate.parent ||
      reviewTree !== binding.review.tree ||
      reviewParents !== binding.review.parent ||
      binding.review.parent !== binding.candidate.commit ||
      candidateDiff !== binding.candidate.diffSha256 ||
      fileDigest(reviewBytes) !== binding.review.sha256 ||
      fileDigest(authorityBytes) !== authority.fileSha256 ||
      reviewPathBefore !== ""
    ) {
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
        `/reviews/${index}/gitLineage`,
        "Review tuple Git objects, parentage, diff, authority bytes, or late review path drifted.",
      );
    }
  } catch (error) {
    if (error instanceof ContractReviewBindingError) throw error;
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/reviews/${index}/gitLineage`,
      "Review tuple Git lineage is unavailable or invalid.",
    );
  }
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
  for (const path of [
    CONTRACT_REVIEW_BINDING_SOURCE_SCHEMA_PATH,
    CONTRACT_REVIEW_TUPLE_SCHEMA_PATH,
    CONTRACT_REVIEW_BINDING_REGISTRY_SCHEMA_PATH,
  ]) {
    const schema = readJsonFile(root, path, "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID");
    ajv.addSchema(schema);
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate || !validate(value)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_SCHEMA_INVALID",
      "/",
      `Detached review-binding schema validation failed: ${ajv.errorsText(validate?.errors)}.`,
    );
  }
}

function readJsonFile(
  root: string,
  path: string,
  code: ContractReviewBindingError["code"],
): JsonRecord {
  try {
    const parsed: unknown = JSON.parse(readStableContainedRegularFile(root, path).toString("utf8"));
    if (!isRecord(parsed)) throw new Error("expected a JSON object");
    return parsed;
  } catch (error) {
    if (error instanceof ContractReviewBindingError) throw error;
    throw bindingError(
      code,
      `/${path}`,
      `Cannot read strict detached binding JSON ${path}: ${String(error)}.`,
    );
  }
}

function pathExists(root: string, repositoryRelativePath: string): boolean {
  const candidate = resolveContainedPath(root, repositoryRelativePath, true);
  try {
    const stat = lstatSync(candidate);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
        `/${repositoryRelativePath}`,
        "Detached review-binding presence requires a regular non-symlink file.",
      );
    }
    return true;
  } catch (error) {
    if (error instanceof ContractReviewBindingError) throw error;
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
    throw error;
  }
}

function resolveContainedPath(
  root: string,
  repositoryRelativePath: string,
  allowMissing = false,
): string {
  const rootReal = realpathSync(resolve(root));
  if (
    repositoryRelativePath.length === 0 ||
    isAbsolute(repositoryRelativePath) ||
    repositoryRelativePath.includes("\\") ||
    repositoryRelativePath
      .split("/")
      .some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${repositoryRelativePath}`,
      "Detached review-binding path must be canonical and repository-relative.",
    );
  }
  const candidate = resolve(rootReal, ...repositoryRelativePath.split("/"));
  const lexical = relative(rootReal, candidate);
  if (lexical === "" || lexical === ".." || lexical.startsWith(`..${sep}`) || isAbsolute(lexical)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      `/${repositoryRelativePath}`,
      "Detached review-binding path escapes the repository root.",
    );
  }
  const components = repositoryRelativePath.split("/");
  let current = rootReal;
  for (const [index, component] of components.entries()) {
    current = resolve(current, component);
    const final = index === components.length - 1;
    try {
      const stat = lstatSync(current);
      if (stat.isSymbolicLink()) throw new Error("symbolic links are forbidden");
      if (!final && !stat.isDirectory()) throw new Error("parent is not a directory");
      if (final && !stat.isFile()) throw new Error("path is not a regular file");
    } catch (error) {
      if (allowMissing && error instanceof Error && "code" in error && error.code === "ENOENT") {
        return candidate;
      }
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
        `/${repositoryRelativePath}`,
        `Detached review-binding path is not a contained regular file: ${String(error)}.`,
      );
    }
  }
  return current;
}

function assertFileDigest(root: string, path: string, expected: string, errorPath: string): void {
  const actual = fileSha256(root, path);
  if (actual !== expected) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
      errorPath,
      `Detached review-binding evidence ${path} digest drifted.`,
    );
  }
}

function fileSha256(root: string, path: string): string {
  return fileDigest(readStableContainedRegularFile(root, path));
}

function readStableContainedRegularFile(root: string, path: string): Buffer {
  const absolute = resolveContainedPath(root, path);
  const pathBefore = lstatSync(absolute, { bigint: true });
  const descriptor = openSync(absolute, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const descriptorBefore = fstatSync(descriptor, { bigint: true });
    if (
      !descriptorBefore.isFile() ||
      descriptorBefore.dev !== pathBefore.dev ||
      descriptorBefore.ino !== pathBefore.ino
    ) {
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
        `/${path}`,
        "Detached review-binding input changed before it could be opened.",
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
      throw bindingError(
        "CONTRACT_REVIEW_BINDING_PARTIAL_STATE",
        `/${path}`,
        "Detached review-binding input changed while it was being read.",
      );
    }
    return bytes;
  } finally {
    closeSync(descriptor);
  }
}

function fileDigest(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
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
    maxBuffer: 128 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function fsyncDirectory(path: string): void {
  const descriptor = openSync(path, constants.O_RDONLY);
  try {
    fsyncSync(descriptor);
  } finally {
    closeSync(descriptor);
  }
}

function requiredRecord(value: unknown, label: string): JsonRecord {
  if (!isRecord(value)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_IDENTITY_MISMATCH",
      "/authority",
      `${label} must be an object.`,
    );
  }
  return value;
}

function requiredDigest(value: unknown, label: string): string {
  if (typeof value !== "string" || !/^sha256:[0-9a-f]{64}$/u.test(value)) {
    throw bindingError(
      "CONTRACT_REVIEW_BINDING_DIGEST_MISMATCH",
      "/authority",
      `${label} must be an exact SHA-256 digest.`,
    );
  }
  return value;
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update(Uint8Array.of(0));
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  return Buffer.from(canonicalizeJson(left)).equals(Buffer.from(canonicalizeJson(right)));
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function bindingError(
  code: ContractReviewBindingError["code"],
  path: string,
  message: string,
): ContractReviewBindingError {
  return new ContractReviewBindingError(code, path, message);
}
