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
  assertPlatformContractLockV3Document,
  PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION,
  type PlatformContractLockV3AssembledDocument,
} from "./platform-contract-lock-v3";

export const G_CONTRACT_PHASE_SOURCE_PATH = "tools/gate-phase-record/g-contract-p1/v1/source.json";
export const G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-source-v1.schema.json";
export const G_CONTRACT_PHASE_MODEL_SCHEMA_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-model-v1.schema.json";
export const G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-review-tuple-v1.schema.json";
export const G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-binding-registry-v1.schema.json";

export const G_CONTRACT_PHASE_RECORD_PATH =
  "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260825-R5.md";
export const G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH =
  "docs/plan/p1/g-contract-generator-supply-profile-v3-independent-review-20260825.md";
export const G_CONTRACT_PHASE_R5_REVIEW_PATH =
  "docs/plan/p1/g-contract-r5-current-source-independent-review-20260825.md";
export const G_CONTRACT_PHASE_REVIEW_TUPLE_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/review-tuple.json";
export const G_CONTRACT_PHASE_BINDING_REGISTRY_PATH =
  "tools/gate-phase-record/g-contract-p1/v1/registry.json";
export const G_CONTRACT_PHASE_FINAL_REVIEW_PATH =
  "docs/plan/p1/g-contract-r5-review-binding-independent-review-20260825.md";

export const G_CONTRACT_PHASE_EXACT17_PATHS = [
  "contracts/generation.lock.json",
  "tools/generator-supply/v3/evidence-manifest.json",
  "tools/generator-supply/v3/profile.json",
  "tools/generator-supply/v3/evidence/replay.json",
  "tools/generator-supply/v3/evidence/replay/darwin-a.json",
  "tools/generator-supply/v3/evidence/replay/darwin-b.json",
  "tools/generator-supply/v3/evidence/replay/darwin-isolation.json",
  "tools/generator-supply/v3/evidence/replay/linux-a.json",
  "tools/generator-supply/v3/evidence/replay/linux-b.json",
  "tools/generator-supply/v3/evidence/replay/linux-isolation.json",
  "tools/generator-supply/v3/evidence/replay/projection.json",
  G_CONTRACT_PHASE_SUPPLY_REVIEW_PATH,
  G_CONTRACT_PHASE_RECORD_PATH,
  G_CONTRACT_PHASE_R5_REVIEW_PATH,
  G_CONTRACT_PHASE_REVIEW_TUPLE_PATH,
  G_CONTRACT_PHASE_BINDING_REGISTRY_PATH,
  G_CONTRACT_PHASE_FINAL_REVIEW_PATH,
] as const;

export const G_CONTRACT_PHASE_STATES = [
  "PRE_CANDIDATE_ABSENT",
  "R5_CURRENT_REVIEW_ABSENT",
  "R5_REVIEW_CURRENT_BINDING_ABSENT",
  "COMPLETE_TUPLE_READY_TO_WRITE",
  "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
  "REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE",
] as const;

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-source-v1.schema.json";
const MODEL_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-record-model-v1.schema.json";
const TUPLE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-review-tuple-v1.schema.json";
const REGISTRY_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/tools/gate-phase-record/g-contract-p1/v1/g-contract-phase-binding-registry-v1.schema.json";
const AUTHORITY_ID = "cloud-agents/platform/gate-phase-record/g-contract-p1/v1";
const REQUIRED_VERDICT = "APPROVE_P0_0_P1_0_P2_0";
const CURRENT_CANDIDATE_AUTHORITY_STATUS = "REVIEW_BOUND_SATISFIED_CANDIDATE";

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

const FIXED_GIT_ARGS = [
  "-c",
  "core.attributesFile=/dev/null",
  "-c",
  "diff.external=",
  "-c",
  "diff.renames=false",
] as const;

export type Digest = `sha256:${string}`;
export type GitObject = string;
export type CriterionStatus =
  | "SATISFIED_CANDIDATE"
  | "REVIEW_PENDING"
  | "OPEN_NOT_CLAIMED"
  | "NOT_APPLICABLE";
export type ReviewSubject = "generator_supply_v3" | "g_contract_r5";

export type FileBinding = JsonRecord & {
  readonly path: string;
  readonly gitBlob: GitObject;
  readonly sha256: Digest;
  readonly sizeBytes: number;
  readonly mode: "100644";
};

export type CandidateGitBinding = JsonRecord & {
  readonly actorId: string;
  readonly commit: GitObject;
  readonly tree: GitObject;
  readonly parent: GitObject;
  readonly diffSha256: Digest;
};

export type ReviewGitBinding = JsonRecord & {
  readonly reviewerId: string;
  readonly commit: GitObject;
  readonly tree: GitObject;
  readonly parent: GitObject;
  readonly path: string;
  readonly gitBlob: GitObject;
  readonly sha256: Digest;
  readonly sizeBytes: number;
  readonly mode: "100644";
  readonly diffSha256: Digest;
  readonly verdict: typeof REQUIRED_VERDICT;
  readonly findings: JsonRecord & { readonly p0: 0; readonly p1: 0; readonly p2: 0 };
};

export type ReviewBinding = JsonRecord & {
  readonly subject: ReviewSubject;
  readonly candidateSubjectPath: string;
  readonly candidate: CandidateGitBinding;
  readonly review: ReviewGitBinding;
};

export type GContractPhaseRecordSource = JsonRecord & {
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly gateId: "G-CONTRACT";
  readonly phase: "P1";
  readonly record: JsonRecord & {
    readonly evidenceId: string;
    readonly path: string;
    readonly status: "IN_PROGRESS";
  };
  readonly criteriaAuthority: JsonRecord & {
    readonly path: string;
    readonly sha256: Digest;
    readonly criteria: readonly (JsonRecord & {
      readonly id: string;
      readonly statement: string;
      readonly formalCriteria: readonly string[];
      readonly missingStatus: "OPEN_NOT_CLAIMED" | "REVIEW_PENDING";
    })[];
  };
  readonly currentCandidateAuthority: JsonRecord & {
    readonly path: string;
    readonly sha256: Digest;
    readonly formatVersion: "cloud-agents-contract-review-binding-registry/v1";
    readonly registryId: "cloud-agents/platform/contract-review-binding";
    readonly bindingId: "g-contract-current-source-review-binding/v1";
    readonly effectiveStatus: typeof CURRENT_CANDIDATE_AUTHORITY_STATUS;
    readonly formalCriteria: readonly string[];
    readonly missing: readonly string[];
    readonly notGateClosure: true;
    readonly gateStatus: "ALL_GATES_OPEN";
  };
  readonly prerequisites: readonly (JsonRecord & {
    readonly evidenceId: string;
    readonly path: string;
    readonly sha256: Digest;
    readonly status: "VERIFIED";
  })[];
  readonly historicalRecords: readonly (JsonRecord & {
    readonly evidenceId: string;
    readonly path: string;
    readonly sha256: Digest;
  })[];
  readonly currentSourceInputPaths: readonly string[];
  readonly dynamicAuthorities: JsonRecord & {
    readonly projectionReceiptPath: string;
    readonly supplyManifestPath: string;
    readonly supplyProfilePath: string;
    readonly supplyReviewPath: string;
    readonly assembledLockPath: string;
  };
  readonly reviewSlots: readonly (JsonRecord & {
    readonly subject: ReviewSubject;
    readonly candidateSubjectPath: string;
    readonly reviewPath: string;
    readonly candidateDiffDomain: string;
    readonly reviewDiffDomain: string;
    readonly requiredVerdict: typeof REQUIRED_VERDICT;
  })[];
  readonly binding: JsonRecord & {
    readonly tuplePath: string;
    readonly registryPath: string;
    readonly registryState: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT";
    readonly finalReviewPath: string;
    readonly terminalState: "REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE";
  };
  readonly exactLateBoundPaths: readonly string[];
  readonly stateMachine: readonly string[];
  readonly implementationBoundary: JsonRecord;
};

export type GContractPhaseRecordModel = JsonRecord & {
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly sourceDigest: Digest;
  readonly evidenceId: string;
  readonly recordType: "PHASE";
  readonly gateId: "G-CONTRACT";
  readonly phase: "P1";
  readonly status: "IN_PROGRESS";
  readonly independentReviewer: "PENDING";
  readonly prerequisites: readonly JsonRecord[];
  readonly historicalRecords: readonly JsonRecord[];
  readonly projection: JsonRecord;
  readonly supply: JsonRecord;
  readonly currentSourceInputs: readonly FileBinding[];
  readonly assembledLock: JsonRecord;
  readonly criteriaAuthority: FileBinding;
  readonly currentCandidateAuthority: JsonRecord;
  readonly criteria: readonly JsonRecord[];
  readonly missing: readonly string[];
  readonly invalidationRules: readonly string[];
  readonly nonClaims: readonly string[];
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly modelDigest: Digest;
};

export type GContractPhaseReviewTuple = JsonRecord & {
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly sourceDigest: Digest;
  readonly reviews: readonly ReviewBinding[];
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly closureDecision: "NONE";
  readonly tupleDigest: Digest;
};

export type GContractPhaseBindingRegistry = JsonRecord & {
  readonly formatVersion: string;
  readonly authorityId: string;
  readonly sourceDigest: Digest;
  readonly tupleDigest: Digest;
  readonly bindingsDigest: Digest;
  readonly bindings: readonly JsonRecord[];
  readonly state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT";
  readonly terminalReview: JsonRecord & { readonly path: string; readonly state: "ABSENT" };
  readonly notGateClosure: true;
  readonly gateStatus: "ALL_GATES_OPEN";
  readonly closureDecision: "NONE";
  readonly registryDigest: Digest;
};

export type GContractPhaseRecordBuildInput = Readonly<{
  projectionCommit: GitObject;
  projectionTree: GitObject;
  projectionArchiveSha256: Digest;
  supplyCandidate: CandidateGitBinding;
  supplyReview: ReviewGitBinding;
}>;

export class GContractPhaseRecordError extends Error {
  constructor(
    readonly code:
      | "G_CONTRACT_PHASE_SOURCE_INVALID"
      | "G_CONTRACT_PHASE_SCHEMA_INVALID"
      | "G_CONTRACT_PHASE_IDENTITY_MISMATCH"
      | "G_CONTRACT_PHASE_ORDER_MISMATCH"
      | "G_CONTRACT_PHASE_DIGEST_MISMATCH"
      | "G_CONTRACT_PHASE_FILE_INVALID"
      | "G_CONTRACT_PHASE_SELF_REVIEW",
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "GContractPhaseRecordError";
  }
}

export function readGContractPhaseRecordSource(root: string): GContractPhaseRecordSource {
  const bytes = readStableContainedFile(root, G_CONTRACT_PHASE_SOURCE_PATH);
  const source = parseObject(bytes, G_CONTRACT_PHASE_SOURCE_PATH) as GContractPhaseRecordSource;
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (bytes.toString("utf8") !== serializeGContractPhaseJson(source)) {
    throw phaseError(
      "G_CONTRACT_PHASE_SOURCE_INVALID",
      "/source",
      "G-CONTRACT-P1 source must use canonical two-space JSON plus one trailing newline.",
    );
  }
  assertExactOrder(
    source.exactLateBoundPaths,
    G_CONTRACT_PHASE_EXACT17_PATHS,
    "/exactLateBoundPaths",
  );
  assertExactOrder(source.stateMachine, G_CONTRACT_PHASE_STATES, "/stateMachine");
  const mappedFormalCriteria = new Set(
    source.criteriaAuthority.criteria.flatMap(({ formalCriteria }) => formalCriteria),
  );
  const declaredFormalCriteria = source.currentCandidateAuthority.formalCriteria;
  if (
    mappedFormalCriteria.size !== declaredFormalCriteria.length ||
    declaredFormalCriteria.some((criterion) => !mappedFormalCriteria.has(criterion)) ||
    [...mappedFormalCriteria].some((criterion) => !declaredFormalCriteria.includes(criterion))
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_ORDER_MISMATCH",
      "/criteriaAuthority/criteria/formalCriteria",
      "Gate-row mapping must cover exactly the ordered formal-criterion authority universe.",
    );
  }
  if (
    source.authorityId !== AUTHORITY_ID ||
    source.gateId !== "G-CONTRACT" ||
    source.phase !== "P1" ||
    source.record.path !== G_CONTRACT_PHASE_RECORD_PATH ||
    source.binding.tuplePath !== G_CONTRACT_PHASE_REVIEW_TUPLE_PATH ||
    source.binding.registryPath !== G_CONTRACT_PHASE_BINDING_REGISTRY_PATH ||
    source.binding.finalReviewPath !== G_CONTRACT_PHASE_FINAL_REVIEW_PATH ||
    source.binding.registryState !== "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT" ||
    source.binding.terminalState !== "REVIEW_BOUND_CURRENT_SOURCE_CANDIDATE" ||
    source.currentCandidateAuthority.effectiveStatus !== CURRENT_CANDIDATE_AUTHORITY_STATUS ||
    source.currentCandidateAuthority.missing.length !== 0 ||
    source.currentCandidateAuthority.notGateClosure !== true ||
    source.currentCandidateAuthority.gateStatus !== "ALL_GATES_OPEN" ||
    source.implementationBoundary.notGateClosure !== true ||
    source.implementationBoundary.gateStatus !== "ALL_GATES_OPEN" ||
    source.implementationBoundary.terminalTrackedOutput !== "FORBIDDEN"
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/source",
      "Source drifted from the G-CONTRACT-P1, non-Gate, pre-terminal authority.",
    );
  }
  return source;
}

export function gContractPhaseSourceDigest(source: GContractPhaseRecordSource): Digest {
  return domainDigest("cloud-agents/g-contract-phase/source/v1", source);
}

export function buildGContractPhaseRecordModel(
  root: string,
  input: GContractPhaseRecordBuildInput,
): GContractPhaseRecordModel {
  const source = readGContractPhaseRecordSource(root);
  const prerequisites = source.prerequisites.map((declared) => {
    const bound = bindCurrentFile(root, declared.path);
    if (bound.sha256 !== declared.sha256) digestMismatch(declared.path);
    return { evidenceId: declared.evidenceId, status: declared.status, ...bound };
  });
  const historicalRecords = source.historicalRecords.map((declared) => {
    const bound = bindCurrentFile(root, declared.path);
    if (bound.sha256 !== declared.sha256) digestMismatch(declared.path);
    return { evidenceId: declared.evidenceId, ...bound };
  });
  const criteriaAuthority = bindCurrentFile(root, source.criteriaAuthority.path);
  if (criteriaAuthority.sha256 !== source.criteriaAuthority.sha256) {
    digestMismatch(source.criteriaAuthority.path);
  }
  const currentCandidateAuthority = readCurrentCandidateAuthority(root, source);
  const currentSourceInputs = source.currentSourceInputPaths.map((path) =>
    bindCurrentFile(root, path),
  );
  const projectionReceipt = bindCurrentFile(root, source.dynamicAuthorities.projectionReceiptPath);
  const supplyManifest = bindCurrentFile(root, source.dynamicAuthorities.supplyManifestPath);
  const supplyProfile = bindCurrentFile(root, source.dynamicAuthorities.supplyProfilePath);
  assertFreshSupplyV3Review(
    root,
    source,
    input.supplyCandidate,
    input.supplyReview,
    projectionReceipt,
    supplyManifest,
    supplyProfile,
  );
  const assembledLock = bindFileAtCommit(
    root,
    input.supplyCandidate.commit,
    source.dynamicAuthorities.assembledLockPath,
  );
  const assembledLockDocument = parseLockAtCommit(
    root,
    input.supplyCandidate.commit,
    source.dynamicAuthorities.assembledLockPath,
    assembledLock,
  );
  const criteria = deriveGateCriteria(source, currentCandidateAuthority.missing);
  const missing = deriveGateMissing(criteria);
  const body: JsonRecord = {
    formatVersion: "cloud-agents-g-contract-phase-record-model/v1",
    authorityId: AUTHORITY_ID,
    sourceDigest: gContractPhaseSourceDigest(source),
    evidenceId: source.record.evidenceId,
    recordType: "PHASE",
    gateId: "G-CONTRACT",
    phase: "P1",
    supersedes: source.record.supersedes,
    status: "IN_PROGRESS",
    independentReviewer: "PENDING",
    date: source.record.date,
    timezone: source.record.timezone,
    gateEffect: "NONE",
    closureDecision: "NONE",
    prerequisites,
    historicalRecords,
    projection: {
      commit: input.projectionCommit,
      tree: input.projectionTree,
      archiveSha256: input.projectionArchiveSha256,
      receipt: projectionReceipt,
    },
    supply: {
      candidate: input.supplyCandidate,
      manifest: supplyManifest,
      profile: supplyProfile,
      review: input.supplyReview,
    },
    currentSourceInputs,
    assembledLock: {
      ...assembledLock,
      state: assembledLockDocument.state,
      formatVersion: assembledLockDocument.formatVersion,
      candidateCommit: input.supplyCandidate.commit,
      candidateTree: input.supplyCandidate.tree,
    },
    criteriaAuthority,
    currentCandidateAuthority,
    criteria,
    missing,
    invalidationRules: [
      "Any prerequisite, historical record, Gate-criteria authority, reviewed current-candidate authority, projection, supply-v3 assembly or review, current source input, or assembled-lock identity drift invalidates R5.",
      "Any schema, Proto, OpenAPI, generated SDK, or mapping-fixture digest drift invalidates R5.",
      "The sole authorized ASSEMBLED-to-PHASE_BOUND lock successor does not invalidate the immutable assembled snapshot; every other lock transition does.",
      "R5 and every review remain append-only historical evidence after invalidation and must be superseded rather than overwritten.",
    ],
    nonClaims: [
      "G-CONTRACT closure",
      "G-SUPPLY-CHAIN closure",
      "production database or migration execution",
      "HTTP, OIDC, JWKS, P2, provider, workload, credential, or trust effects",
      "deployment, publication, external signing, release, Beta, or GA",
      "Linux arm64 replay",
    ],
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
  const model = {
    ...body,
    modelDigest: domainDigest("cloud-agents/g-contract-phase/model/v1", body),
  } as GContractPhaseRecordModel;
  validateGContractPhaseRecordModel(root, model);
  return model;
}

export function validateGContractPhaseRecordModel(
  root: string,
  model: GContractPhaseRecordModel,
): void {
  validateAgainstSchema(root, MODEL_SCHEMA_ID, model);
  const { modelDigest, ...body } = model;
  if (modelDigest !== domainDigest("cloud-agents/g-contract-phase/model/v1", body)) {
    throw phaseError(
      "G_CONTRACT_PHASE_DIGEST_MISMATCH",
      "/modelDigest",
      "Phase-record model digest does not bind its complete canonical body.",
    );
  }
  const source = readGContractPhaseRecordSource(root);
  const expectedAuthority = readCurrentCandidateAuthority(root, source);
  if (!canonicalEqual(model.currentCandidateAuthority, expectedAuthority)) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/currentCandidateAuthority",
      "Model current-candidate authority must match the fixed reviewed registry binding.",
    );
  }
  const authority = model.currentCandidateAuthority as JsonRecord;
  const authorityMissing = requiredStringArray(authority.missing, "authority missing");
  const expectedCriteria = deriveGateCriteria(source, authorityMissing);
  if (!canonicalEqual(model.criteria, expectedCriteria)) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/criteria",
      "Gate rows must be the exact ordered derivation of the fixed current candidate authority.",
    );
  }
  const derivedMissing = deriveGateMissing(model.criteria);
  assertExactOrder(model.missing, derivedMissing, "/missing");
}

export function renderGContractPhaseRecord(root: string, model: GContractPhaseRecordModel): string {
  validateGContractPhaseRecordModel(root, model);
  const projection = model.projection as JsonRecord;
  const supply = model.supply as JsonRecord;
  const supplyCandidate = supply.candidate as JsonRecord;
  const supplyReview = supply.review as JsonRecord;
  const assembledLock = model.assembledLock as JsonRecord;
  const currentCandidateAuthority = model.currentCandidateAuthority as JsonRecord;
  const lines = [
    "# Gate candidate record: `G-CONTRACT` / P1 / R5",
    "",
    `- Evidence ID: \`${model.evidenceId}\``,
    "- Record type: `PHASE`",
    "- Phase / Gate: P1 contract foundation / `G-CONTRACT`",
    `- Supersedes: \`${model.supersedes}\``,
    "- Status: `IN PROGRESS`",
    "- Independent reviewer: `PENDING`",
    `- Date: ${model.date} ${model.timezone}`,
    "- Gate effect: none; this is a current-source non-Gate candidate",
    "- Closure decision: `NONE`",
    "",
    "## Fixed semantic authority",
    "",
    `- Authority: \`${model.authorityId}\``,
    `- Source digest: \`${model.sourceDigest}\``,
    `- Model digest: \`${model.modelDigest}\``,
    `- Criteria authority: \`${model.criteriaAuthority.path}\` / \`${model.criteriaAuthority.sha256}\``,
    `- Current candidate authority: \`${requiredString(currentCandidateAuthority.path, "current candidate authority path")}\` / \`${requiredString(currentCandidateAuthority.sha256, "current candidate authority digest")}\``,
    `- Effective candidate status / formal missing: \`${requiredString(currentCandidateAuthority.effectiveStatus, "effective candidate status")}\` / \`${requiredStringArray(currentCandidateAuthority.missing, "authority missing").length}\``,
    "",
    "## Prerequisites and immutable history",
    "",
    "| Kind | Evidence ID | Path | SHA-256 |",
    "| --- | --- | --- | --- |",
    ...model.prerequisites.map((entry) => markdownEvidenceRow("prerequisite", entry)),
    ...model.historicalRecords.map((entry) => markdownEvidenceRow("historical", entry)),
    "",
    "## Current-source projection and supply review",
    "",
    `- Projection commit / tree: \`${projection.commit}\` / \`${projection.tree}\``,
    `- Projection archive SHA-256: \`${projection.archiveSha256}\``,
    `- Supply candidate commit / tree / parent: \`${supplyCandidate.commit}\` / \`${supplyCandidate.tree}\` / \`${supplyCandidate.parent}\``,
    `- Supply candidate diff: \`${supplyCandidate.diffSha256}\``,
    `- Supply review commit / tree / path: \`${supplyReview.commit}\` / \`${supplyReview.tree}\` / \`${supplyReview.path}\``,
    `- Supply review SHA-256 / verdict: \`${supplyReview.sha256}\` / \`${supplyReview.verdict}\``,
    `- Assembled lock commit / tree / blob: \`${assembledLock.candidateCommit}\` / \`${assembledLock.candidateTree}\` / \`${assembledLock.gitBlob}\``,
    `- Assembled lock SHA-256 / state: \`${assembledLock.sha256}\` / \`ASSEMBLED\``,
    "",
    "## Current contract, SDK, and descriptor inputs",
    "",
    "| Path | Git blob | SHA-256 | Size | Mode |",
    "| --- | --- | --- | ---: | --- |",
    ...model.currentSourceInputs.map(
      (entry) =>
        `| \`${escapeCell(entry.path)}\` | \`${entry.gitBlob}\` | \`${entry.sha256}\` | ${entry.sizeBytes} | \`${entry.mode}\` |`,
    ),
    "",
    "## G-CONTRACT exit criteria",
    "",
    "| Criterion | Candidate status | Formal criteria | Requirement |",
    "| --- | --- | --- | --- |",
    ...model.criteria.map(
      (criterion) =>
        `| \`${escapeCell(requiredString(criterion.id, "criterion id"))}\` | \`${escapeCell(requiredString(criterion.status, "criterion status"))}\` | ${requiredStringArray(
          criterion.formalCriteria,
          "criterion formal criteria",
        )
          .map((item) => `\`${escapeCell(item)}\``)
          .join(
            "<br>",
          )} | ${escapeCell(requiredString(criterion.statement, "criterion statement"))} |`,
    ),
    "",
    "The derived missing set remains:",
    "",
    ...model.missing.map((item, index) => `${index + 1}. \`${item}\``),
    "",
    "## Invalidation",
    "",
    ...model.invalidationRules.map((rule) => `- ${rule}`),
    "",
    "## Explicit non-claims",
    "",
    ...model.nonClaims.map((claim) => `- ${claim}`),
    "",
    "`notGateClosure=true`; `gateStatus=ALL_GATES_OPEN`; `G-CONTRACT` remains `IN PROGRESS`.",
    "",
  ];
  return lines.join("\n");
}

export function buildGContractPhaseReviewTuple(
  root: string,
  reviews: readonly [ReviewBinding, ReviewBinding],
): GContractPhaseReviewTuple {
  const source = readGContractPhaseRecordSource(root);
  const body = {
    formatVersion: "cloud-agents-g-contract-phase-review-tuple/v1",
    authorityId: AUTHORITY_ID,
    sourceDigest: gContractPhaseSourceDigest(source),
    reviews,
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
    closureDecision: "NONE",
  };
  const tuple = {
    ...body,
    tupleDigest: domainDigest("cloud-agents/g-contract-phase/review-tuple/v1", body),
  } as GContractPhaseReviewTuple;
  validateGContractPhaseReviewTuple(root, tuple);
  return tuple;
}

export function validateGContractPhaseReviewTuple(
  root: string,
  tuple: GContractPhaseReviewTuple,
): void {
  validateAgainstSchema(root, TUPLE_SCHEMA_ID, tuple);
  const source = readGContractPhaseRecordSource(root);
  if (
    tuple.authorityId !== AUTHORITY_ID ||
    tuple.sourceDigest !== gContractPhaseSourceDigest(source) ||
    tuple.notGateClosure !== true ||
    tuple.gateStatus !== "ALL_GATES_OPEN" ||
    tuple.closureDecision !== "NONE"
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/tuple",
      "Review tuple authority or non-Gate identity drifted.",
    );
  }
  for (const [index, slot] of source.reviewSlots.entries()) {
    const binding = tuple.reviews[index]!;
    if (
      binding.subject !== slot.subject ||
      binding.candidateSubjectPath !== slot.candidateSubjectPath ||
      binding.review.path !== slot.reviewPath ||
      binding.review.verdict !== slot.requiredVerdict ||
      binding.review.parent !== binding.candidate.commit
    ) {
      throw phaseError(
        "G_CONTRACT_PHASE_ORDER_MISMATCH",
        `/reviews/${index}`,
        "Review tuple order, subject, path, parent, or verdict drifted.",
      );
    }
    if (
      binding.candidate.actorId === binding.review.reviewerId ||
      binding.candidate.commit === binding.review.commit
    ) {
      throw phaseError(
        "G_CONTRACT_PHASE_SELF_REVIEW",
        `/reviews/${index}`,
        "Candidate actor and independent reviewer must be distinct.",
      );
    }
  }
  const { tupleDigest, ...body } = tuple;
  if (tupleDigest !== domainDigest("cloud-agents/g-contract-phase/review-tuple/v1", body)) {
    throw phaseError(
      "G_CONTRACT_PHASE_DIGEST_MISMATCH",
      "/tupleDigest",
      "Review tuple digest does not bind its complete canonical body.",
    );
  }
}

export function buildGContractPhaseBindingRegistry(
  root: string,
  tuple: GContractPhaseReviewTuple,
): GContractPhaseBindingRegistry {
  validateGContractPhaseReviewTuple(root, tuple);
  const source = readGContractPhaseRecordSource(root);
  const bindings = tuple.reviews.map((binding) => ({
    subject: binding.subject,
    candidateCommit: binding.candidate.commit,
    candidateTree: binding.candidate.tree,
    reviewCommit: binding.review.commit,
    reviewTree: binding.review.tree,
    reviewPath: binding.review.path,
    reviewSha256: binding.review.sha256,
    reviewDiffSha256: binding.review.diffSha256,
    verdict: binding.review.verdict,
  }));
  const bindingsDigest = domainDigest("cloud-agents/g-contract-phase/bindings/v1", bindings);
  const body = {
    formatVersion: "cloud-agents-g-contract-phase-binding-registry/v1",
    authorityId: AUTHORITY_ID,
    sourceDigest: tuple.sourceDigest,
    tupleDigest: tuple.tupleDigest,
    bindingsDigest,
    bindings,
    state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
    terminalReview: { path: source.binding.finalReviewPath, state: "ABSENT" },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
    closureDecision: "NONE",
  };
  const registry = {
    ...body,
    registryDigest: domainDigest("cloud-agents/g-contract-phase/binding-registry/v1", body),
  } as GContractPhaseBindingRegistry;
  validateGContractPhaseBindingRegistry(root, tuple, registry);
  return registry;
}

export function validateGContractPhaseBindingRegistry(
  root: string,
  tuple: GContractPhaseReviewTuple,
  registry: GContractPhaseBindingRegistry,
): void {
  validateAgainstSchema(root, REGISTRY_SCHEMA_ID, registry);
  const expected = buildRegistryWithoutValidation(root, tuple);
  if (!canonicalEqual(expected, registry)) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/registry",
      "Binding registry must be the exact pre-terminal derivation of the two-lineage tuple.",
    );
  }
}

function buildRegistryWithoutValidation(
  root: string,
  tuple: GContractPhaseReviewTuple,
): GContractPhaseBindingRegistry {
  validateGContractPhaseReviewTuple(root, tuple);
  const source = readGContractPhaseRecordSource(root);
  const bindings = tuple.reviews.map((binding) => ({
    subject: binding.subject,
    candidateCommit: binding.candidate.commit,
    candidateTree: binding.candidate.tree,
    reviewCommit: binding.review.commit,
    reviewTree: binding.review.tree,
    reviewPath: binding.review.path,
    reviewSha256: binding.review.sha256,
    reviewDiffSha256: binding.review.diffSha256,
    verdict: binding.review.verdict,
  }));
  const body = {
    formatVersion: "cloud-agents-g-contract-phase-binding-registry/v1",
    authorityId: AUTHORITY_ID,
    sourceDigest: tuple.sourceDigest,
    tupleDigest: tuple.tupleDigest,
    bindingsDigest: domainDigest("cloud-agents/g-contract-phase/bindings/v1", bindings),
    bindings,
    state: "PHASE_BINDING_CURRENT_FINAL_REVIEW_ABSENT",
    terminalReview: { path: source.binding.finalReviewPath, state: "ABSENT" },
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
    closureDecision: "NONE",
  };
  return {
    ...body,
    registryDigest: domainDigest("cloud-agents/g-contract-phase/binding-registry/v1", body),
  } as GContractPhaseBindingRegistry;
}

function readCurrentCandidateAuthority(
  root: string,
  source: GContractPhaseRecordSource,
): JsonRecord & {
  readonly missing: readonly string[];
} {
  const declared = source.currentCandidateAuthority;
  const bytes = readStableContainedFile(root, declared.path);
  const registry = parseObject(bytes, declared.path);
  const binding = bindCurrentFile(root, declared.path);
  const rawDigest = `sha256:${createHash("sha256").update(bytes).digest("hex")}` as Digest;
  assertExactObjectKeys(
    registry,
    [
      "formatVersion",
      "registryId",
      "bindingId",
      "sourceDigest",
      "tupleDigest",
      "bindingsDigest",
      "canonicalClosureReference",
      "supplyProfileReference",
      "effectiveCandidate",
      "registryDigest",
    ],
    `/${declared.path}`,
  );
  const effective = requiredObject(registry.effectiveCandidate, "effective candidate");
  assertExactObjectKeys(
    effective,
    ["status", "criteriaBindings", "missing", "notGateClosure", "gateStatus"],
    `/${declared.path}/effectiveCandidate`,
  );
  const missing = requiredStringArray(effective.missing, "effective candidate missing");
  if (
    registry.formatVersion !== declared.formatVersion ||
    registry.registryId !== declared.registryId ||
    registry.bindingId !== declared.bindingId ||
    effective.status !== declared.effectiveStatus ||
    effective.status !== CURRENT_CANDIDATE_AUTHORITY_STATUS ||
    missing.length !== 0 ||
    !canonicalEqual(missing, declared.missing) ||
    effective.notGateClosure !== true ||
    effective.gateStatus !== "ALL_GATES_OPEN"
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      `/${declared.path}/effectiveCandidate`,
      "Current reviewed authority must remain satisfied-candidate, missing-empty, and explicitly non-Gate.",
    );
  }
  if (binding.sha256 !== rawDigest || binding.sha256 !== declared.sha256) {
    digestMismatch(declared.path);
  }
  return {
    ...binding,
    formatVersion: registry.formatVersion,
    registryId: registry.registryId,
    bindingId: registry.bindingId,
    effectiveStatus: effective.status,
    formalCriteria: declared.formalCriteria,
    missing,
    notGateClosure: true,
    gateStatus: "ALL_GATES_OPEN",
  };
}

function assertFreshSupplyV3Review(
  root: string,
  source: GContractPhaseRecordSource,
  candidate: CandidateGitBinding,
  review: ReviewGitBinding,
  currentProjectionReceipt: FileBinding,
  currentSupplyManifest: FileBinding,
  currentSupplyProfile: FileBinding,
): void {
  const candidateTree = gitText(root, ["rev-parse", `${candidate.commit}^{tree}`]);
  const reviewTree = gitText(root, ["rev-parse", `${review.commit}^{tree}`]);
  const candidateParents = gitText(root, [
    "rev-list",
    "--parents",
    "-n",
    "1",
    candidate.commit,
  ]).split(" ");
  const reviewParents = gitText(root, ["rev-list", "--parents", "-n", "1", review.commit]).split(
    " ",
  );
  const slot = source.reviewSlots[0]!;
  const candidateDiff = domainSeparatedGitDiffDigest(
    slot.candidateDiffDomain,
    gitDiffBytes(root, candidate.parent, candidate.commit),
  );
  const reviewDiff = domainSeparatedGitDiffDigest(
    slot.reviewDiffDomain,
    gitDiffBytes(root, candidate.commit, review.commit),
  );
  const reviewFile = bindFileAtCommit(
    root,
    review.commit,
    source.dynamicAuthorities.supplyReviewPath,
  );
  const currentReviewFile = bindCurrentFile(root, source.dynamicAuthorities.supplyReviewPath);
  const candidateSupplyProfile = bindFileAtCommit(
    root,
    candidate.commit,
    source.dynamicAuthorities.supplyProfilePath,
  );
  const candidateSupplyManifest = bindFileAtCommit(
    root,
    candidate.commit,
    source.dynamicAuthorities.supplyManifestPath,
  );
  const candidateProjectionReceipt = bindFileAtCommit(
    root,
    candidate.commit,
    source.dynamicAuthorities.projectionReceiptPath,
  );
  if (
    candidateParents.length !== 2 ||
    candidateParents[1] !== candidate.parent ||
    candidateTree !== candidate.tree ||
    candidateDiff !== candidate.diffSha256 ||
    !canonicalEqual(candidateProjectionReceipt, currentProjectionReceipt) ||
    !canonicalEqual(candidateSupplyManifest, currentSupplyManifest) ||
    !canonicalEqual(candidateSupplyProfile, currentSupplyProfile) ||
    reviewTree !== review.tree ||
    reviewParents.length !== 2 ||
    reviewParents[1] !== candidate.commit ||
    review.parent !== candidate.commit ||
    review.path !== source.dynamicAuthorities.supplyReviewPath ||
    review.verdict !== REQUIRED_VERDICT ||
    !canonicalEqual(review.findings, { p0: 0, p1: 0, p2: 0 }) ||
    candidate.actorId === review.reviewerId ||
    review.commit === candidate.commit ||
    !canonicalEqual(currentReviewFile, reviewFile) ||
    review.gitBlob !== reviewFile.gitBlob ||
    review.sha256 !== reviewFile.sha256 ||
    review.sizeBytes !== reviewFile.sizeBytes ||
    review.mode !== reviewFile.mode ||
    reviewDiff !== review.diffSha256
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/supply/review",
      "R5 requires the exact direct-child, independent, zero-finding APPROVE supply-v3 review.",
    );
  }
}

function gitDiffBytes(root: string, from: string, to: string): Buffer {
  return gitBytes(root, [
    "diff",
    "--no-color",
    "--no-ext-diff",
    "--no-textconv",
    "--binary",
    "--no-renames",
    from,
    to,
  ]);
}

function parseLockAtCommit(
  root: string,
  commit: string,
  path: string,
  binding: FileBinding,
): PlatformContractLockV3AssembledDocument {
  const bytes = gitBytes(root, ["cat-file", "blob", `${commit}:${path}`]);
  const parsed = parseObject(bytes, `${commit}:${path}`);
  const observed = fileBinding(path, binding.gitBlob, bytes);
  if (!canonicalEqual(observed, binding)) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/assembledLock/${path}`,
      "Assembled lock binding and parsed candidate bytes diverged.",
    );
  }
  try {
    assertPlatformContractLockV3Document(parsed);
  } catch (error) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/assembledLock/${path}`,
      `Candidate generation-lock v3 is invalid: ${String(error)}.`,
    );
  }
  if (
    parsed.state !== "ASSEMBLED" ||
    parsed.formatVersion !== PLATFORM_CONTRACT_LOCK_V3_FORMAT_VERSION
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/assembledLock/state",
      "R5 may bind only the immutable Slice E ASSEMBLED lock-v3 document.",
    );
  }
  return parsed;
}

function deriveGateCriteria(
  source: GContractPhaseRecordSource,
  formalMissing: readonly string[],
): JsonRecord[] {
  const knownFormal = source.currentCandidateAuthority.formalCriteria;
  if (
    formalMissing.some((criterion) => !knownFormal.includes(criterion)) ||
    new Set(formalMissing).size !== formalMissing.length
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/currentCandidateAuthority/missing",
      "Formal missing set contains an unknown or duplicate criterion.",
    );
  }
  return source.criteriaAuthority.criteria.map((criterion) => {
    const unresolved = criterion.formalCriteria.filter((item) => formalMissing.includes(item));
    const status: CriterionStatus =
      criterion.formalCriteria.length === 0
        ? "NOT_APPLICABLE"
        : unresolved.length === 0
          ? "SATISFIED_CANDIDATE"
          : criterion.missingStatus;
    return {
      id: criterion.id,
      status,
      statement: criterion.statement,
      formalCriteria: criterion.formalCriteria,
    };
  });
}

function deriveGateMissing(criteria: readonly JsonRecord[]): string[] {
  return criteria
    .filter(({ status }) => status === "OPEN_NOT_CLAIMED" || status === "REVIEW_PENDING")
    .map(({ id }) => requiredString(id, "criterion id"));
}

export function serializeGContractPhaseJson(value: unknown): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function domainSeparatedGitDiffDigest(domain: string, bytes: Uint8Array): Digest {
  return `sha256:${createHash("sha256")
    .update(domain, "utf8")
    .update(Uint8Array.of(0))
    .update(bytes)
    .digest("hex")}`;
}

export function gContractPhaseAuthorityInputs(): string[] {
  return [
    G_CONTRACT_PHASE_SOURCE_PATH,
    G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
    G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
    G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
    G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
    "scripts/lib/platform-g-contract-phase-record.ts",
    "scripts/lib/platform-g-contract-phase-record.test.ts",
    "scripts/lib/platform-g-contract-phase-state.ts",
    "scripts/lib/platform-g-contract-phase-state.test.ts",
  ].toSorted();
}

function bindCurrentFile(root: string, path: string): FileBinding {
  const bytes = readStableContainedFile(root, path);
  const treeEntry = gitText(root, ["ls-tree", "HEAD", "--", path]);
  const match = /^100644 blob ([0-9a-f]{40})\t(.+)$/u.exec(treeEntry);
  if (!match || match[2] !== path) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/${path}`,
      `${path} must be an exact tracked regular 100644 path at HEAD.`,
    );
  }
  const headBytes = gitBytes(root, ["cat-file", "blob", `HEAD:${path}`]);
  if (!bytes.equals(headBytes)) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/${path}`,
      `${path} differs from its fixed HEAD blob.`,
    );
  }
  return fileBinding(path, match[1]!, bytes);
}

function bindFileAtCommit(root: string, commit: string, path: string): FileBinding {
  const treeEntry = gitText(root, ["ls-tree", commit, "--", path]);
  const match = /^100644 blob ([0-9a-f]{40})\t(.+)$/u.exec(treeEntry);
  if (!match || match[2] !== path) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/${path}`,
      `${path} must be an exact regular 100644 blob at ${commit}.`,
    );
  }
  return fileBinding(path, match[1]!, gitBytes(root, ["cat-file", "blob", `${commit}:${path}`]));
}

function fileBinding(path: string, gitBlob: string, bytes: Buffer): FileBinding {
  return {
    path,
    gitBlob,
    sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    sizeBytes: bytes.byteLength,
    mode: "100644",
  };
}

function readStableContainedFile(root: string, path: string): Buffer {
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
      throw phaseError(
        "G_CONTRACT_PHASE_FILE_INVALID",
        `/${path}`,
        "Input changed before it could be opened.",
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
      throw phaseError(
        "G_CONTRACT_PHASE_FILE_INVALID",
        `/${path}`,
        "Input changed while it was being read.",
      );
    }
    return bytes;
  } finally {
    closeSync(descriptor);
  }
}

function resolveContainedPath(root: string, path: string): string {
  const rootReal = realpathSync(resolve(root));
  if (
    path.length === 0 ||
    isAbsolute(path) ||
    path.includes("\\") ||
    path.split("/").some((segment) => segment.length === 0 || segment === "." || segment === "..")
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_FILE_INVALID",
      `/${path}`,
      "Paths must be canonical repository-relative paths without aliases.",
    );
  }
  const absolute = resolve(rootReal, ...path.split("/"));
  const lexical = relative(rootReal, absolute);
  if (lexical === "" || lexical === ".." || lexical.startsWith(`..${sep}`) || isAbsolute(lexical)) {
    throw phaseError("G_CONTRACT_PHASE_FILE_INVALID", `/${path}`, "Path escapes repository root.");
  }
  let current = rootReal;
  for (const component of path.split("/")) {
    current = resolve(current, component);
    const stat = lstatSync(current);
    if (stat.isSymbolicLink()) {
      throw phaseError(
        "G_CONTRACT_PHASE_FILE_INVALID",
        `/${path}`,
        "Symbolic links are forbidden in phase-record authority paths.",
      );
    }
  }
  return absolute;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({
    allErrors: true,
    strict: true,
    strictTypes: false,
    validateFormats: false,
  });
  for (const path of [
    G_CONTRACT_PHASE_SOURCE_SCHEMA_PATH,
    G_CONTRACT_PHASE_MODEL_SCHEMA_PATH,
    G_CONTRACT_PHASE_REVIEW_TUPLE_SCHEMA_PATH,
    G_CONTRACT_PHASE_BINDING_REGISTRY_SCHEMA_PATH,
  ]) {
    ajv.addSchema(parseObject(readStableContainedFile(root, path), path));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate || !validate(value)) {
    throw phaseError(
      "G_CONTRACT_PHASE_SCHEMA_INVALID",
      "/",
      `Strict G-CONTRACT-P1 schema validation failed: ${ajv.errorsText(validate?.errors)}.`,
    );
  }
}

function parseObject(bytes: Buffer, path: string): JsonRecord {
  try {
    const value: unknown = JSON.parse(bytes.toString("utf8"));
    if (typeof value !== "object" || value === null || Array.isArray(value)) {
      throw new Error("expected object");
    }
    return value as JsonRecord;
  } catch (error) {
    throw phaseError(
      "G_CONTRACT_PHASE_SOURCE_INVALID",
      `/${path}`,
      `Cannot parse strict phase-record JSON: ${String(error)}.`,
    );
  }
}

function domainDigest(domain: string, value: unknown): Digest {
  return `sha256:${createHash("sha256")
    .update(domain, "utf8")
    .update(Uint8Array.of(0))
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  return Buffer.from(canonicalizeJson(left)).equals(Buffer.from(canonicalizeJson(right)));
}

function gitText(root: string, args: readonly string[]): string {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_ARGS, ...args], {
    cwd: root,
    encoding: "utf8",
    env: FIXED_GIT_ENV,
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function gitBytes(root: string, args: readonly string[]): Buffer {
  return execFileSync("/usr/bin/git", [...FIXED_GIT_ARGS, ...args], {
    cwd: root,
    env: FIXED_GIT_ENV,
    maxBuffer: 128 * 1024 * 1024,
    stdio: ["ignore", "pipe", "pipe"],
  });
}

function assertExactOrder(
  actual: readonly string[],
  expected: readonly string[],
  path: string,
): void {
  if (
    actual.length !== expected.length ||
    actual.some((value, index) => value !== expected[index]) ||
    new Set(actual).size !== actual.length
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_ORDER_MISMATCH",
      path,
      "Ordered authority list differs by member, order, count, or uniqueness.",
    );
  }
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/model",
      `${label} must be a non-empty string.`,
    );
  }
  return value;
}

function requiredStringArray(value: unknown, label: string): string[] {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || item.length === 0)
  ) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/model",
      `${label} must be an array of non-empty strings.`,
    );
  }
  return value as string[];
}

function requiredObject(value: unknown, label: string): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw phaseError(
      "G_CONTRACT_PHASE_IDENTITY_MISMATCH",
      "/authority",
      `${label} must be an object.`,
    );
  }
  return value as JsonRecord;
}

function assertExactObjectKeys(value: JsonRecord, expected: readonly string[], path: string): void {
  const actual = Object.keys(value);
  if (!canonicalEqual(actual, expected)) {
    throw phaseError(
      "G_CONTRACT_PHASE_ORDER_MISMATCH",
      path,
      "Authority object contains unknown, omitted, or reordered fields.",
    );
  }
}

function markdownEvidenceRow(kind: string, entry: JsonRecord): string {
  return `| ${kind} | \`${escapeCell(requiredString(entry.evidenceId, "evidence id"))}\` | \`${escapeCell(requiredString(entry.path, "evidence path"))}\` | \`${escapeCell(requiredString(entry.sha256, "evidence digest"))}\` |`;
}

function escapeCell(value: string): string {
  return value.replaceAll("|", "\\|").replaceAll("\n", " ");
}

function digestMismatch(path: string): never {
  throw phaseError(
    "G_CONTRACT_PHASE_DIGEST_MISMATCH",
    `/${path}`,
    `Immutable phase-record input ${path} digest drifted.`,
  );
}

function phaseError(
  code: GContractPhaseRecordError["code"],
  path: string,
  message: string,
): GContractPhaseRecordError {
  return new GContractPhaseRecordError(code, path, message);
}
