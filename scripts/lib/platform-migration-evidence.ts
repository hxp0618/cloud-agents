import { createHash } from "node:crypto";

import {
  canonicalizeMigrationJson,
  type MigrationJson,
  migrationDigest,
  MigrationValidationError,
  parseStrictMigrationJson,
} from "./platform-migration-json";
import {
  type JsonObject,
  validateAttemptTerminalState,
  validateIntermediateState,
  validateProjectionScope,
} from "./platform-migration-projection";

export const EVIDENCE_LIMITS = {
  frameBytes: 1_048_576,
  segmentBytes: 16_777_216,
  segmentRecords: 4_096,
  journalSegments: 16,
  recordBytes: {
    header: 32_768,
    statement_intent: 65_536,
    intermediate: 262_144,
    commit_intent: 65_536,
    attempt_terminal: 65_536,
    ambiguous_resolution: 65_536,
  },
} as const;

export const LINEAGE_LIMITS = {
  frameBytes: 262_144,
  indexBytes: 16_777_216,
  indexRecords: 16_384,
  rootIndexes: 64,
  rootIndexBytes: 1_073_741_824,
  recordBytes: {
    header: 32_768,
    generation_reserved: 65_536,
    generation_activated: 65_536,
    generation_checkpoint: 16_384,
    generation_superseded: 131_072,
  },
} as const;

const UINT16_MAX = 65_535;
const UINT32_MAX = 4_294_967_295;
const JSON_SAFE_UINT64_MAX = 9_007_199_254_740_991;
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const MIGRATION_ID = /^[0-9]{6}$/u;
const SQL_ARTIFACT_PATH = /^services\/control-plane\/migrations\/[a-z0-9_./-]+\.sql$/u;

const EVIDENCE_RECORD_KINDS = [
  "header",
  "statement_intent",
  "intermediate",
  "commit_intent",
  "attempt_terminal",
  "ambiguous_resolution",
] as const;
const LINEAGE_RECORD_KINDS = [
  "header",
  "generation_reserved",
  "generation_activated",
  "generation_checkpoint",
  "generation_superseded",
] as const;
const RECOVERY_STATES = [
  "brand_new",
  "brand_new_inherited",
  "completed",
  "dangling_statement_intent",
  "dangling_intermediate",
  "dangling_commit_intent",
  "ambiguous_unresolved",
  "terminal",
  "divergent",
] as const;
export const RECOVERY_OUTCOMES = [
  "exact_committed_bundle_complete",
  "exact_committed_continue_successor",
  "precommit_aborted_retryable",
  "exact_pending",
  "resolved_pending",
  "confirmed_abort_terminal",
  "terminal_failure",
  "divergent_terminal",
  "activated_no_migration_progress",
] as const;

export type FramedCanonicalEvidence = {
  readonly canonical_size_bytes: number;
  readonly length_prefix_hex: string;
  readonly framed_size_bytes: number;
  readonly framed_sha256: string;
  readonly bytes: Uint8Array;
};

/** Fixture-only external oracle. It is not a persisted DTO or a runtime authority constructor. */
export type EvidenceChainFixtureWitness = {
  readonly maxAttemptsByMigration: ReadonlyMap<string, number>;
  readonly finalStatementIndexByMigration: ReadonlyMap<string, number>;
  readonly finalCatalogDigestByMigration: ReadonlyMap<string, string>;
  readonly signedPlans: ReadonlyMap<string, JsonObject>;
  readonly runtimeReceipt: JsonObject;
  readonly decisionRecoveryReceipt: JsonObject;
  readonly ownedRetryReceiptOracles: ReadonlyMap<string, JsonObject>;
  readonly ownedAmbiguousBoundaryOracles: ReadonlyMap<string, JsonObject>;
};

export type LineageChainWitness = {
  readonly executionLineageDigest: string;
  readonly deploymentId: string;
  readonly databaseName: string;
  readonly repositoryIdentity: string;
  readonly actualSegment0Frames: ReadonlyMap<string, JsonObject>;
  readonly journalFramesByIdentity: ReadonlyMap<string, readonly JsonObject[]>;
  readonly supersessionAuthorities: ReadonlyMap<string, JsonObject>;
};

export function executionLineageDigest(input: JsonObject): string {
  keys(input, ["deployment_id", "expected_database_identity", "repository_identity"]);
  boundedString(input.deployment_id, "deployment id", 1_024);
  const database = object(input.expected_database_identity, "database identity");
  keys(database, ["database_name"]);
  boundedString(database.database_name, "database name", 1_024);
  boundedString(input.repository_identity, "repository identity", 1_024);
  return flatDigest("cloud-agents-platform-execution-lineage/v1", input);
}

export function decisionRecoveryArtifactProfileDigest(): string {
  return migrationDigest({
    domain: "cloud-agents-platform-decision-recovery-artifact-profile/v1",
    format_version: "cloud-agents-platform-decision-recovery-artifact/v1",
    canonicalization: "RFC8785",
    base64url: "unpadded-canonical",
    identity_max_bytes: 1_024,
    encoded_field_max_bytes: 1_048_576,
    projection_inputs_max: 4_099,
    catalog_inputs_max: 4_096,
    kind_rank: ["release", "authority_profile", "authority_binding", "catalog"],
    max_size_bytes: 4_194_304,
  });
}

/** Verifier-owned content ABI oracle; never an EvidenceRecord decoder. */
export function validateDecisionRecoveryVerificationInputs(input: JsonObject): void {
  keys(input, [
    "format_version",
    "profile_digest",
    "old_runner_projection_decision_digest",
    "repository_identity",
    "release_identity",
    "candidate_subject_base64url_no_padding",
    "candidate_detached_envelope_base64url_no_padding",
    "projection_subject_inputs",
  ]);
  literal(
    input.format_version,
    ["cloud-agents-platform-decision-recovery-artifact/v1"],
    "decision recovery format",
  );
  if (input.profile_digest !== decisionRecoveryArtifactProfileDigest()) {
    fail("DECISION_RECOVERY_PROFILE", String(input.profile_digest));
  }
  digest(input.old_runner_projection_decision_digest, "old runner decision");
  const repositoryIdentity = boundedString(input.repository_identity, "repository identity", 1_024);
  const releaseIdentity = boundedString(input.release_identity, "release identity", 1_024);
  if (
    repositoryIdentity.normalize("NFC") !== repositoryIdentity ||
    releaseIdentity.normalize("NFC") !== releaseIdentity
  ) {
    fail("DECISION_RECOVERY_IDENTITY_NFC", "identity");
  }
  canonicalBase64url(input.candidate_subject_base64url_no_padding, "candidate subject");
  canonicalBase64url(input.candidate_detached_envelope_base64url_no_padding, "candidate envelope");
  const entries = array(input.projection_subject_inputs, "projection inputs").map((entry) =>
    object(entry, "projection input"),
  );
  if (entries.length < 3 || entries.length > 4_099)
    fail("DECISION_RECOVERY_INPUT_COUNT", String(entries.length));
  const ranks: Record<string, number> = {
    release: 0,
    authority_profile: 1,
    authority_binding: 2,
    catalog: 3,
  };
  const counts = new Map<string, number>();
  let previousKey = "";
  for (const entry of entries) {
    keys(entry, [
      "kind",
      "subject_digest",
      "subject_base64url_no_padding",
      "detached_envelope_base64url_no_padding",
    ]);
    const kind = literal(
      entry.kind,
      ["release", "authority_profile", "authority_binding", "catalog"] as const,
      "projection input kind",
    );
    const subject = canonicalBase64url(entry.subject_base64url_no_padding, "subject");
    canonicalBase64url(entry.detached_envelope_base64url_no_padding, "detached envelope");
    const claimed = digest(entry.subject_digest, "subject digest");
    if (claimed !== rawSha256(subject)) fail("DECISION_RECOVERY_SUBJECT_DIGEST", kind);
    const key = `${String(ranks[kind]).padStart(2, "0")}:${claimed}`;
    if (previousKey !== "" && Buffer.compare(Buffer.from(previousKey), Buffer.from(key)) >= 0) {
      fail("DECISION_RECOVERY_SORT", key);
    }
    previousKey = key;
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
  }
  for (const kind of ["release", "authority_profile", "authority_binding"]) {
    if (counts.get(kind) !== 1) fail("DECISION_RECOVERY_REQUIRED_KIND", kind);
  }
  if ((counts.get("catalog") ?? 0) > 4_096) fail("DECISION_RECOVERY_INPUT_COUNT", "catalog");
  if (canonicalizeMigrationJson(input).length > 4_194_304)
    fail("DECISION_RECOVERY_SIZE", "artifact");
}

export function recoveryExecutionBindingsDigest(input: JsonObject): string {
  keys(input, [
    "historical_recovery_policy_digest",
    "execution_lineage_digest",
    "current_runner_projection_decision_digest",
    "old_runner_projection_decision_digest",
    "old_journal_identity_digest",
    "old_schema_bundle_digest",
    "old_decision_recovery_artifact_sha256",
    "old_decision_recovery_artifact_size_bytes",
    "old_journal_replay_tail_digest",
    "old_recovery_state",
    "actions_profile",
  ]);
  digests(input, [
    "historical_recovery_policy_digest",
    "execution_lineage_digest",
    "current_runner_projection_decision_digest",
    "old_runner_projection_decision_digest",
    "old_journal_identity_digest",
    "old_schema_bundle_digest",
    "old_decision_recovery_artifact_sha256",
    "old_journal_replay_tail_digest",
  ]);
  uint64(input.old_decision_recovery_artifact_size_bytes, "old recovery artifact size");
  literal(input.old_recovery_state, RECOVERY_STATES, "old recovery state");
  literal(
    input.actions_profile,
    ["cloud-agents-platform-old-attempt-exact-recovery/v1"],
    "recovery actions",
  );
  return flatDigest("cloud-agents-platform-recovery-execution-bindings/v1", input);
}

export function recoveryPolicySubjectDigest(input: JsonObject): string {
  keys(input, [
    "issuer_key_identity_digest",
    "expires_at",
    "security_epoch",
    "minimum_old_security_epoch",
    "old_revocation_policy_digest",
    "old_decision_authorizations",
  ]);
  digest(input.issuer_key_identity_digest, "policy issuer");
  canonicalRfc3339Utc(input.expires_at, "policy expiry");
  uint64(input.security_epoch, "policy epoch", 1);
  uint64(input.minimum_old_security_epoch, "minimum old epoch", 1);
  digest(input.old_revocation_policy_digest, "revocation policy");
  const authorizations = array(input.old_decision_authorizations, "old authorizations").map(
    (entry) => object(entry, "old authorization"),
  );
  let previous = "";
  for (const authorization of authorizations) {
    keys(authorization, [
      "old_runner_projection_decision_digest",
      "allow_expired",
      "allow_revoked",
      "allow_compromised",
    ]);
    const decision = digest(
      authorization.old_runner_projection_decision_digest,
      "old decision authorization",
    );
    if (previous !== "" && previous >= decision) fail("RECOVERY_POLICY_SORT", decision);
    previous = decision;
    boolean(authorization.allow_expired, "allow expired");
    boolean(authorization.allow_revoked, "allow revoked");
    boolean(authorization.allow_compromised, "allow compromised");
  }
  return flatDigest("cloud-agents-platform-recovery-policy-subject/v1", input);
}

export function historicalRecoveryPolicyDigest(input: JsonObject): string {
  keys(input, [
    "recovery_policy_subject_digest",
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "old_decision_recovery_artifact_sha256",
    "old_decision_recovery_artifact_size_bytes",
    "successor_runner_projection_decision_digest",
    "successor_schema_bundle_digest",
    "allowed_outcomes",
    "outcome_constraints",
  ]);
  digests(input, [
    "recovery_policy_subject_digest",
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "old_decision_recovery_artifact_sha256",
    "successor_runner_projection_decision_digest",
    "successor_schema_bundle_digest",
  ]);
  uint64(input.old_decision_recovery_artifact_size_bytes, "old recovery artifact size", 1);
  const allowed = array(input.allowed_outcomes, "allowed outcomes").map((entry) =>
    literal(entry, RECOVERY_OUTCOMES, "allowed outcome"),
  );
  const constraints = array(input.outcome_constraints, "outcome constraints").map((entry) =>
    object(entry, "outcome constraint"),
  );
  if (
    allowed.length === 0 ||
    new Set(allowed).size !== allowed.length ||
    allowed.join("\0") !== [...allowed].toSorted().join("\0") ||
    constraints.length !== allowed.length
  )
    fail("HISTORICAL_POLICY_OUTCOMES", "set");
  const constraintOutcomes: string[] = [];
  for (const constraint of constraints) {
    keys(constraint, ["outcome", "continuation"]);
    const outcome = literal(constraint.outcome, RECOVERY_OUTCOMES, "constraint outcome");
    constraintOutcomes.push(outcome);
    const continuation = object(constraint.continuation, "constraint continuation");
    const kind = literal(
      continuation.kind,
      ["must_be_null", "exact_identity", "exact_carry_old_generation"] as const,
      "constraint kind",
    );
    if (kind === "exact_identity") {
      keys(continuation, ["kind", "identity"]);
      validateLineageContinuationIdentity(object(continuation.identity, "continuation identity"));
    } else keys(continuation, ["kind"]);
    const expectedKind = historicalContinuationConstraintKind(outcome);
    if (kind !== expectedKind) {
      fail("HISTORICAL_POLICY_OUTCOMES", `${outcome}/${kind}`);
    }
  }
  if (constraintOutcomes.join("\0") !== allowed.join("\0")) {
    fail("HISTORICAL_POLICY_OUTCOMES", "constraints");
  }
  return flatDigest("cloud-agents-platform-historical-recovery-policy/v1", input);
}

export function lineageSupersessionAuthorityDigest(input: JsonObject): string {
  keys(input, [
    "historical_recovery_policy_digest",
    "recovery_execution_bindings_digest",
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "old_checkpoint_record_digest",
    "old_activation_record_digest",
    "old_initial_journal_tail_digest",
    "old_terminal_digest",
    "old_resolution_digest",
    "observed_outcome",
    "successor_runner_projection_decision_digest",
    "successor_schema_bundle_digest",
    "continuation",
  ]);
  digests(input, [
    "historical_recovery_policy_digest",
    "recovery_execution_bindings_digest",
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "successor_runner_projection_decision_digest",
    "successor_schema_bundle_digest",
  ]);
  for (const field of [
    "old_checkpoint_record_digest",
    "old_activation_record_digest",
    "old_initial_journal_tail_digest",
    "old_terminal_digest",
    "old_resolution_digest",
  ] as const)
    nullableDigest(input[field], field);
  literal(input.observed_outcome, RECOVERY_OUTCOMES, "observed outcome");
  const continuation = nullableObject(input.continuation, "authority continuation");
  if (continuation !== null) validateLineageContinuationContext(continuation);
  return flatDigest("cloud-agents-platform-lineage-supersession-authority/v1", input);
}

export function validateRecoveryPolicyChainFixture(fixture: JsonObject): void {
  keys(fixture, [
    "format_version",
    "current_decision",
    "current_signed_policy_subject",
    "durable_artifact_receipts",
    "transitions",
  ]);
  literal(
    fixture.format_version,
    ["cloud-agents-platform-recovery-policy-chain-fixture/v1"],
    "recovery chain fixture",
  );
  const currentDecision = digest(fixture.current_decision, "current decision");
  const policyVector = object(fixture.current_signed_policy_subject, "policy vector");
  const policyInput = validateSameBitsVector(policyVector, recoveryPolicySubjectDigest);
  const policyDigest = String(policyVector.digest);
  const authorizations = new Set(
    array(policyInput.old_decision_authorizations, "old authorizations").map((entry) =>
      String(object(entry, "authorization").old_runner_projection_decision_digest),
    ),
  );
  if (authorizations.has(currentDecision))
    fail("RECOVERY_CHAIN_AUTHORITY", "current self authorization");
  const receipts = new Map(
    array(fixture.durable_artifact_receipts, "artifact receipts").map((entry) => {
      const receipt = object(entry, "artifact receipt");
      keys(receipt, [
        "decision",
        "runtime_sha256",
        "runtime_size_bytes",
        "recovery_sha256",
        "recovery_size_bytes",
      ]);
      const decision = digest(receipt.decision, "receipt decision");
      digest(receipt.runtime_sha256, "runtime receipt");
      uint64(receipt.runtime_size_bytes, "runtime receipt size", 1);
      digest(receipt.recovery_sha256, "recovery receipt");
      uint64(receipt.recovery_size_bytes, "recovery receipt size", 1);
      return [decision, receipt] as const;
    }),
  );
  const transitions = array(fixture.transitions, "recovery transitions").map((entry) =>
    object(entry, "recovery transition"),
  );
  if (transitions.length !== 2) fail("RECOVERY_CHAIN", "A to B to C");
  let expectedOld: string | null = null;
  for (const [index, transition] of transitions.entries()) {
    keys(transition, [
      "old_decision",
      "successor_decision",
      "historical_policy",
      "recovery_execution_bindings",
      "supersession_authority",
      "planned_generation_reserved",
      "planned_generation_reserved_digest",
    ]);
    const oldDecision = digest(transition.old_decision, "old transition decision");
    const successor = digest(transition.successor_decision, "successor decision");
    if (
      !authorizations.has(oldDecision) ||
      !receipts.has(oldDecision) ||
      (expectedOld !== null && oldDecision !== expectedOld) ||
      (index === transitions.length - 1 && successor !== currentDecision)
    )
      fail("RECOVERY_CHAIN_AUTHORITY", `${oldDecision}/${successor}`);
    const historical = validateSameBitsVector(
      object(transition.historical_policy, "historical vector"),
      historicalRecoveryPolicyDigest,
    );
    if (
      historical.recovery_policy_subject_digest !== policyDigest ||
      historical.old_runner_projection_decision_digest !== oldDecision ||
      historical.successor_runner_projection_decision_digest !== successor
    )
      fail("RECOVERY_CHAIN_AUTHORITY", "historical policy");
    const oldReceipt = receipts.get(oldDecision)!;
    if (
      historical.old_decision_recovery_artifact_sha256 !== oldReceipt.recovery_sha256 ||
      historical.old_decision_recovery_artifact_size_bytes !== oldReceipt.recovery_size_bytes
    ) {
      fail("RECOVERY_CHAIN_ARTIFACT", oldDecision);
    }
    const execution = validateSameBitsVector(
      object(transition.recovery_execution_bindings, "execution vector"),
      recoveryExecutionBindingsDigest,
    );
    const authority = validateSameBitsVector(
      object(transition.supersession_authority, "authority vector"),
      lineageSupersessionAuthorityDigest,
    );
    if (
      execution.historical_recovery_policy_digest !==
        object(transition.historical_policy, "historical vector").digest ||
      execution.current_runner_projection_decision_digest !== currentDecision ||
      execution.old_runner_projection_decision_digest !== oldDecision ||
      execution.execution_lineage_digest !== historical.execution_lineage_digest ||
      execution.old_journal_identity_digest !== historical.old_journal_identity_digest ||
      execution.old_schema_bundle_digest !== historical.old_schema_bundle_digest ||
      execution.old_decision_recovery_artifact_sha256 !==
        historical.old_decision_recovery_artifact_sha256 ||
      execution.old_decision_recovery_artifact_size_bytes !==
        historical.old_decision_recovery_artifact_size_bytes ||
      authority.historical_recovery_policy_digest !==
        object(transition.historical_policy, "historical vector").digest ||
      authority.recovery_execution_bindings_digest !==
        object(transition.recovery_execution_bindings, "execution vector").digest ||
      authority.execution_lineage_digest !== execution.execution_lineage_digest ||
      authority.old_journal_identity_digest !== execution.old_journal_identity_digest ||
      authority.old_runner_projection_decision_digest !==
        execution.old_runner_projection_decision_digest ||
      authority.old_schema_bundle_digest !== execution.old_schema_bundle_digest ||
      authority.old_initial_journal_tail_digest !== execution.old_journal_replay_tail_digest ||
      authority.successor_runner_projection_decision_digest !== successor ||
      authority.successor_schema_bundle_digest !== historical.successor_schema_bundle_digest
    )
      fail("RECOVERY_CHAIN_AUTHORITY", "digest linkage");
    const allowed = array(historical.allowed_outcomes, "historical allowed outcomes").map(String);
    const constraint = array(historical.outcome_constraints, "historical constraints")
      .map((entry) => object(entry, "historical constraint"))
      .find((entry) => entry.outcome === authority.observed_outcome);
    const selectedConstraint =
      constraint === undefined
        ? null
        : object(constraint.continuation, "historical continuation constraint");
    const selectedKind = selectedConstraint?.kind;
    const authorityContinuation = nullableObject(authority.continuation, "authority continuation");
    if (
      !allowed.includes(String(authority.observed_outcome)) ||
      constraint === undefined ||
      selectedKind !==
        (authority.observed_outcome === "activated_no_migration_progress"
          ? "exact_carry_old_generation"
          : authorityContinuation === null
            ? "must_be_null"
            : "exact_identity")
    ) {
      fail("RECOVERY_CHAIN_AUTHORITY", "observed outcome constraint");
    }
    if (
      selectedKind === "exact_identity" &&
      (authorityContinuation === null ||
        canonicalText(object(selectedConstraint!.identity, "selected continuation identity")) !==
          canonicalText(continuationIdentityFromContext(authorityContinuation)))
    ) {
      fail("RECOVERY_CHAIN_AUTHORITY", "exact continuation identity");
    }
    const planned = object(transition.planned_generation_reserved, "planned generation");
    validateGenerationReserved(planned);
    if (
      transition.planned_generation_reserved_digest !== migrationDigest(planned) ||
      planned.runner_projection_decision_digest !== successor ||
      planned.schema_bundle_digest !== authority.successor_schema_bundle_digest ||
      canonicalText(planned.continuation!) !== canonicalText(authority.continuation!)
    ) {
      fail("RECOVERY_CHAIN_PLANNED", oldDecision);
    }
    const successorReceipt = receipts.get(successor);
    if (successorReceipt !== undefined) {
      const plannedHeader = object(planned.planned_segment0_header, "planned header");
      if (
        plannedHeader.outer_artifact_digest !== successorReceipt.runtime_sha256 ||
        plannedHeader.outer_artifact_size_bytes !== successorReceipt.runtime_size_bytes ||
        plannedHeader.decision_recovery_artifact_sha256 !== successorReceipt.recovery_sha256 ||
        plannedHeader.decision_recovery_artifact_size_bytes !== successorReceipt.recovery_size_bytes
      )
        fail("RECOVERY_CHAIN_ARTIFACT", successor);
    }
    expectedOld = successor;
  }
}

function validateSameBitsVector(
  vector: JsonObject,
  digestBuilder: (input: JsonObject) => string,
): JsonObject {
  keys(vector, ["input", "canonical_rfc8785_utf8", "digest"]);
  const input = object(vector.input, "same bits input");
  if (
    vector.canonical_rfc8785_utf8 !== canonicalText(input) ||
    vector.digest !== digestBuilder(input)
  )
    fail("SAME_BITS_VECTOR", String(vector.digest));
  return input;
}

export function terminalDigest(stateWithoutDigest: JsonObject): string {
  return flatDigest("cloud-agents-platform-attempt-terminal/v1", stateWithoutDigest);
}

export function ambiguousResolutionDigest(stateWithoutDigest: JsonObject): string {
  return flatDigest("cloud-agents-platform-ambiguous-resolution/v1", stateWithoutDigest);
}

export function evidenceRecordDigest(frameWithoutDigest: JsonObject): string {
  return flatDigest("cloud-agents-platform-evidence-journal-record/v1", frameWithoutDigest);
}

export function lineageRecordDigest(frameWithoutDigest: JsonObject): string {
  return flatDigest("cloud-agents-platform-lineage-index-record/v1", frameWithoutDigest);
}

export function quotaReservationDigest(input: JsonObject): string {
  keys(input, [
    "limits_profile",
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "reserved_records",
    "reserved_bytes",
    "reserved_segments",
    "continuation",
  ]);
  validateQuotaReservationInput(input);
  return flatDigest("cloud-agents-platform-evidence-quota-reservation/v1", input);
}

export function journalIdentityDigest(input: JsonObject): string {
  keys(input, [
    "release_trust_decision_digest",
    "runner_projection_decision_digest",
    "outer_artifact_digest",
    "outer_artifact_size_bytes",
    "decision_recovery_artifact_sha256",
    "decision_recovery_artifact_size_bytes",
    "schema_bundle_digest",
    "authority_profile_digest",
    "authority_binding_digest",
  ]);
  digests(input, [
    "release_trust_decision_digest",
    "runner_projection_decision_digest",
    "outer_artifact_digest",
    "decision_recovery_artifact_sha256",
    "schema_bundle_digest",
    "authority_profile_digest",
    "authority_binding_digest",
  ]);
  uint64(input.outer_artifact_size_bytes, "outer artifact size");
  uint64(input.decision_recovery_artifact_size_bytes, "decision recovery size");
  return flatDigest("cloud-agents-platform-evidence-journal-identity/v1", input);
}

export function ledgerPrefixDigest(rows: readonly JsonObject[]): string {
  rows.forEach(validateLedgerRow);
  return migrationDigest({ domain: "cloud-agents-platform-ledger-prefix/v1", rows: [...rows] });
}

export function canonicalLengthPrefixed(value: MigrationJson): FramedCanonicalEvidence {
  const canonical = canonicalizeMigrationJson(value);
  const prefix = new Uint8Array(8);
  const view = new DataView(prefix.buffer);
  view.setBigUint64(0, BigInt(canonical.length), false);
  const bytes = new Uint8Array(prefix.length + canonical.length);
  bytes.set(prefix, 0);
  bytes.set(canonical, prefix.length);
  return {
    canonical_size_bytes: canonical.length,
    length_prefix_hex: Buffer.from(prefix).toString("hex"),
    framed_size_bytes: bytes.length,
    framed_sha256: rawSha256(bytes),
    bytes,
  };
}

export function decodeCanonicalLengthPrefixedFrame(
  bytes: Uint8Array,
  kind: "evidence" | "lineage",
): JsonObject {
  if (bytes.length < 8) fail("FRAME_PREFIX", "short prefix");
  const declared = new DataView(bytes.buffer, bytes.byteOffset, 8).getBigUint64(0, false);
  if (declared > BigInt(JSON_SAFE_UINT64_MAX) || declared !== BigInt(bytes.length - 8)) {
    fail("FRAME_PREFIX", `${String(declared)}/${bytes.length - 8}`);
  }
  const payload = bytes.subarray(8);
  const parsed = object(parseStrictMigrationJson(payload), "framed record");
  const canonical = canonicalizeMigrationJson(parsed);
  if (!Buffer.from(payload).equals(Buffer.from(canonical))) fail("FRAME_NON_CANONICAL", kind);
  if (kind === "evidence") validateEvidenceFrame(parsed);
  else validateLineageIndexFrame(parsed);
  return parsed;
}

export function validateEvidenceFramedSizeForKind(
  kind: keyof typeof EVIDENCE_LIMITS.recordBytes,
  bytes: number,
): void {
  validateBoundedUsage(bytes, EVIDENCE_LIMITS.frameBytes, "EVIDENCE_FRAME_LIMIT");
  validateBoundedUsage(bytes, EVIDENCE_LIMITS.recordBytes[kind], "FRAMED_RECORD_LIMIT");
}

export function validateLineageFramedSizeForKind(
  kind: keyof typeof LINEAGE_LIMITS.recordBytes,
  bytes: number,
): void {
  validateBoundedUsage(bytes, LINEAGE_LIMITS.frameBytes, "LINEAGE_FRAME_LIMIT");
  validateBoundedUsage(bytes, LINEAGE_LIMITS.recordBytes[kind], "FRAMED_RECORD_LIMIT");
}

export function validateEvidenceSegmentUsage(records: number, bytes: number): void {
  validateBoundedUsage(records, EVIDENCE_LIMITS.segmentRecords, "EVIDENCE_SEGMENT_LIMIT");
  validateBoundedUsage(bytes, EVIDENCE_LIMITS.segmentBytes, "EVIDENCE_SEGMENT_LIMIT");
}

function validateBoundedUsage(value: number, maximum: number, code: string): void {
  if (!Number.isSafeInteger(value) || value < 0 || value > maximum) {
    fail(code, `${value}/${maximum}`);
  }
}

export function validateLineageIndexUsage(records: number, bytes: number): void {
  validateBoundedUsage(records, LINEAGE_LIMITS.indexRecords, "LINEAGE_INDEX_LIMIT");
  validateBoundedUsage(bytes, LINEAGE_LIMITS.indexBytes, "LINEAGE_INDEX_LIMIT");
}

export function validateProjectionResultEvidence(
  evidence: JsonObject,
  expected?: {
    readonly digest: string | null;
    readonly kind: "authority" | "catalog" | "catalog_state";
    readonly migrationId: string;
    readonly statementIndex: number | null;
    readonly preledger: boolean;
  },
): void {
  keys(evidence, ["digest", "metadata"]);
  digest(evidence.digest, "projection result digest");
  const metadata = object(evidence.metadata, "projection metadata");
  validateProjectionMetadata(metadata);
  if (expected === undefined) return;
  if (
    (expected.digest !== null && evidence.digest !== expected.digest) ||
    metadata.projection_kind !== expected.kind
  ) {
    fail("EVIDENCE_RESULT_MAPPING", expected.kind);
  }
  const snapshot = object(metadata.snapshot, "projection snapshot");
  if (
    snapshot.migration_id !== expected.migrationId ||
    snapshot.statement_index !== expected.statementIndex ||
    (expected.preledger && snapshot.statement_index !== null)
  )
    fail("EVIDENCE_RESULT_MAPPING", "snapshot identity");
}

export function validateStatementIntent(intent: JsonObject): void {
  keys(intent, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "migration_id",
    "attempt_index",
    "statement_index",
    "sql_path",
    "sql_artifact_sha256",
    "sql_artifact_size_bytes",
    "start_offset",
    "end_offset",
    "statement_sha256",
    "classification",
    "previous_attempt_terminal_digest",
    "previous_intermediate_state_digest",
    "expected_transition_digest",
    "authority_before_digest",
    "catalog_before_digest",
    "authority_before_result",
    "catalog_before_result",
  ]);
  digests(intent, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "sql_artifact_sha256",
    "statement_sha256",
    "expected_transition_digest",
    "authority_before_digest",
    "catalog_before_digest",
  ]);
  migrationId(intent.migration_id, "intent migration");
  const attempt = uint32(intent.attempt_index, "intent attempt", 1);
  const statement = uint32(intent.statement_index, "intent statement");
  const previousAttempt = nullableDigest(
    intent.previous_attempt_terminal_digest,
    "previous terminal",
  );
  const previousIntermediate = nullableDigest(
    intent.previous_intermediate_state_digest,
    "previous intermediate",
  );
  if ((attempt === 1) !== (previousAttempt === null)) fail("INTENT_ATTEMPT_LINK", String(attempt));
  if ((statement === 0) !== (previousIntermediate === null)) {
    fail("INTENT_STATEMENT_LINK", String(statement));
  }
  const sqlPath = boundedString(intent.sql_path, "sql path", 512);
  if (!SQL_ARTIFACT_PATH.test(sqlPath) || sqlPath.includes("..")) fail("INTENT_SQL_PATH", sqlPath);
  const size = uint64(intent.sql_artifact_size_bytes, "sql artifact size");
  const start = uint64(intent.start_offset, "statement start");
  const end = uint64(intent.end_offset, "statement end");
  if (start >= end || end > size) fail("INTENT_SQL_RANGE", `${start}/${end}/${size}`);
  validateStatementClassification(object(intent.classification, "classification"));
  validateProjectionResultEvidence(object(intent.authority_before_result, "authority before"), {
    digest: String(intent.authority_before_digest),
    kind: "authority",
    migrationId: String(intent.migration_id),
    statementIndex: statement,
    preledger: false,
  });
  validateProjectionResultEvidence(object(intent.catalog_before_result, "catalog before"), {
    digest: String(intent.catalog_before_digest),
    kind: "catalog_state",
    migrationId: String(intent.migration_id),
    statementIndex: statement,
    preledger: false,
  });
}

export function validateStatementIntermediateEvidence(evidence: JsonObject): void {
  keys(evidence, [
    "state",
    "authority_before_result",
    "catalog_before_result",
    "authority_after_result",
    "catalog_after_result",
    "preledger_authority_result",
    "preledger_catalog_result",
  ]);
  const state = object(evidence.state, "intermediate state");
  validateIntermediateState(state);
  const migration = String(state.migration_id);
  const statement = Number(state.statement_index);
  validateProjectionResultEvidence(object(evidence.authority_before_result, "authority before"), {
    digest: String(state.authority_before_digest),
    kind: "authority",
    migrationId: migration,
    statementIndex: statement,
    preledger: false,
  });
  validateProjectionResultEvidence(object(evidence.catalog_before_result, "catalog before"), {
    digest: String(state.catalog_before_digest),
    kind: "catalog_state",
    migrationId: migration,
    statementIndex: statement,
    preledger: false,
  });
  validateProjectionResultEvidence(object(evidence.authority_after_result, "authority after"), {
    digest: String(state.authority_after_digest),
    kind: "authority",
    migrationId: migration,
    statementIndex: statement,
    preledger: false,
  });
  validateProjectionResultEvidence(object(evidence.catalog_after_result, "catalog after"), {
    digest: String(state.catalog_after_digest),
    kind: "catalog_state",
    migrationId: migration,
    statementIndex: statement,
    preledger: false,
  });
  const preAuthority = nullableObject(evidence.preledger_authority_result, "preledger authority");
  const preCatalog = nullableObject(evidence.preledger_catalog_result, "preledger catalog");
  if ((preAuthority === null) !== (preCatalog === null)) fail("PRELEDGER_PAIR", "partial pair");
  if (preAuthority !== null && preCatalog !== null) {
    validateProjectionResultEvidence(preAuthority, {
      digest: String(state.authority_after_digest),
      kind: "authority",
      migrationId: migration,
      statementIndex: null,
      preledger: true,
    });
    validateProjectionResultEvidence(preCatalog, {
      digest: null,
      kind: "catalog",
      migrationId: migration,
      statementIndex: null,
      preledger: true,
    });
  }
}

export function validateCommitIntent(intent: JsonObject): void {
  keys(intent, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "migration_id",
    "attempt_index",
    "previous_attempt_terminal_digest",
    "attempt_predecessor_catalog_digest",
    "last_intermediate_state_digest",
    "expected_ledger_length",
    "expected_ledger_head",
    "ledger_row",
  ]);
  digests(intent, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "attempt_predecessor_catalog_digest",
    "last_intermediate_state_digest",
  ]);
  const migration = migrationId(intent.migration_id, "commit migration");
  const attempt = uint32(intent.attempt_index, "commit attempt", 1);
  const previous = nullableDigest(
    intent.previous_attempt_terminal_digest,
    "commit previous terminal",
  );
  if ((attempt === 1) !== (previous === null)) fail("COMMIT_ATTEMPT_LINK", String(attempt));
  const expectedLength = uint32(intent.expected_ledger_length, "expected ledger length", 1);
  if (expectedLength < 1) fail("COMMIT_LEDGER_LENGTH", String(expectedLength));
  if (intent.expected_ledger_head !== migration)
    fail("COMMIT_LEDGER_HEAD", String(intent.expected_ledger_head));
  const row = object(intent.ledger_row, "ledger row");
  validateLedgerRow(row);
  if (row.migration_id !== migration) fail("COMMIT_LEDGER_ROW", "migration id");
}

export function validateAmbiguousResolutionState(state: JsonObject): void {
  keys(state, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "migration_id",
    "attempt_index",
    "unresolved_terminal_digest",
    "outcome",
    "reconcile_result",
    "stable_error_code",
    "resolution_digest",
  ]);
  digests(state, [
    "schema_bundle_digest",
    "catalog_contract_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "unresolved_terminal_digest",
  ]);
  migrationId(state.migration_id, "resolution migration");
  uint32(state.attempt_index, "resolution attempt", 1);
  const outcome = literal(
    state.outcome,
    ["resolved_committed", "resolved_pending", "resolved_divergent"] as const,
    "resolution outcome",
  );
  const reconcile = literal(
    state.reconcile_result,
    ["exact_committed", "exact_pending", "divergent"] as const,
    "resolution reconcile",
  );
  const expected = {
    resolved_committed: "exact_committed",
    resolved_pending: "exact_pending",
    resolved_divergent: "divergent",
  }[outcome];
  if (reconcile !== expected) fail("RESOLUTION_COMBINATION", `${outcome}/${reconcile}`);
  literal(
    state.stable_error_code,
    [
      "MIGRATION_AMBIGUOUS_COMMIT",
      "MIGRATION_UNTRUSTED",
      "MIGRATION_EVIDENCE_JOURNAL_FAILED",
      "MIGRATION_EVIDENCE_RECOVERY_REQUIRED",
      "MIGRATION_CONTEXT_CANCELED",
      "MIGRATION_DEADLINE_EXCEEDED",
    ] as const,
    "resolution stable code",
  );
  const claimed = digest(state.resolution_digest, "resolution digest");
  const body = without(state, "resolution_digest");
  if (claimed !== ambiguousResolutionDigest(body)) fail("RESOLUTION_DIGEST", claimed);
}

export function validateJournalHeader(header: JsonObject): void {
  keys(header, [
    "format_version",
    "journal_identity_digest",
    "release_trust_decision_digest",
    "runner_projection_decision_digest",
    "execution_lineage_digest",
    "outer_artifact_digest",
    "outer_artifact_size_bytes",
    "decision_recovery_artifact_sha256",
    "decision_recovery_artifact_size_bytes",
    "manifest_digest",
    "runner_release_digest",
    "schema_bundle_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "segment_index",
    "previous_segment_record_digest",
    "limits_profile",
    "quota_reservation_digest",
    "reserved_records",
    "reserved_bytes",
    "reserved_segments",
  ]);
  literal(header.format_version, ["cloud-agents-platform-evidence-journal/v1"], "journal format");
  literal(
    header.limits_profile,
    ["cloud-agents-platform-evidence-journal-limits/v1"],
    "journal limits",
  );
  digests(header, [
    "journal_identity_digest",
    "release_trust_decision_digest",
    "runner_projection_decision_digest",
    "execution_lineage_digest",
    "outer_artifact_digest",
    "decision_recovery_artifact_sha256",
    "manifest_digest",
    "runner_release_digest",
    "schema_bundle_digest",
    "authority_profile_digest",
    "authority_binding_digest",
    "quota_reservation_digest",
  ]);
  uint64(header.outer_artifact_size_bytes, "outer artifact size");
  const recoverySize = uint64(
    header.decision_recovery_artifact_size_bytes,
    "decision recovery size",
  );
  if (recoverySize > 4_194_304) fail("DECISION_RECOVERY_SIZE", String(recoverySize));
  const segment = uint32(header.segment_index, "segment index");
  nullableDigest(header.previous_segment_record_digest, "previous segment record");
  if ((segment === 0) !== (header.previous_segment_record_digest === null)) {
    fail("JOURNAL_SEGMENT_LINK", String(segment));
  }
  const records = uint64(header.reserved_records, "reserved records", 1);
  const bytes = uint64(header.reserved_bytes, "reserved bytes", 1);
  const segments = uint32(header.reserved_segments, "reserved segments", 1);
  if (records > 65_536 || bytes > 268_435_456 || segments > EVIDENCE_LIMITS.journalSegments) {
    fail("JOURNAL_SEGMENT_LIMIT", `${records}/${bytes}/${segments}`);
  }
  if (segment >= segments) fail("JOURNAL_SEGMENT_LIMIT", `${segment}/${segments}`);
  const identity = journalIdentityDigest({
    release_trust_decision_digest: header.release_trust_decision_digest!,
    runner_projection_decision_digest: header.runner_projection_decision_digest!,
    outer_artifact_digest: header.outer_artifact_digest!,
    outer_artifact_size_bytes: header.outer_artifact_size_bytes!,
    decision_recovery_artifact_sha256: header.decision_recovery_artifact_sha256!,
    decision_recovery_artifact_size_bytes: header.decision_recovery_artifact_size_bytes!,
    schema_bundle_digest: header.schema_bundle_digest!,
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
  });
  if (header.journal_identity_digest !== identity) fail("JOURNAL_IDENTITY_DIGEST", identity);
}

export function validateEvidenceFrame(frame: JsonObject): void {
  keys(frame, [
    "format_version",
    "sequence",
    "previous_record_digest",
    "record_kind",
    "record",
    "record_digest",
  ]);
  literal(
    frame.format_version,
    ["cloud-agents-platform-evidence-journal-frame/v1"],
    "evidence frame format",
  );
  uint64(frame.sequence, "evidence sequence");
  nullableDigest(frame.previous_record_digest, "previous record");
  const kind = literal(frame.record_kind, EVIDENCE_RECORD_KINDS, "evidence record kind");
  const record = object(frame.record, "evidence record");
  validateEvidenceRecord(kind, record);
  const claimed = digest(frame.record_digest, "evidence record digest");
  const body = without(frame, "record_digest");
  if (claimed !== evidenceRecordDigest(body)) fail("EVIDENCE_RECORD_DIGEST", claimed);
  validateFramedLimit(kind, canonicalLengthPrefixed(frame).framed_size_bytes, false);
}

export function validateLineageContinuationContext(context: JsonObject): void {
  keys(context, [
    "start_action",
    "migration_id",
    "attempt_index",
    "previous_attempt_terminal_digest",
    "source_journal_identity_digest",
    "source_checkpoint_record_digest",
    "source_terminal_digest",
  ]);
  const action = literal(
    context.start_action,
    ["begin_first_attempt_next_entry", "begin_next_attempt"] as const,
    "continuation action",
  );
  migrationId(context.migration_id, "continuation migration");
  const attempt = uint32(context.attempt_index, "continuation attempt", 1);
  const previous = nullableDigest(
    context.previous_attempt_terminal_digest,
    "continuation terminal",
  );
  digests(context, [
    "source_journal_identity_digest",
    "source_checkpoint_record_digest",
    "source_terminal_digest",
  ]);
  if (
    (action === "begin_first_attempt_next_entry" && (attempt !== 1 || previous !== null)) ||
    (action === "begin_next_attempt" && (attempt < 2 || previous === null))
  )
    fail("LINEAGE_CONTINUATION", `${action}/${attempt}`);
  if (previous !== null && previous !== context.source_terminal_digest) {
    fail("LINEAGE_CONTINUATION", "previous/source terminal");
  }
}

function validateLineageContinuationIdentity(identity: JsonObject): void {
  keys(identity, ["start_action", "migration_id", "attempt_index", "previous_attempt"]);
  const action = literal(
    identity.start_action,
    ["begin_first_attempt_next_entry", "begin_next_attempt"] as const,
    "continuation identity action",
  );
  migrationId(identity.migration_id, "continuation identity migration");
  const attempt = uint32(identity.attempt_index, "continuation identity attempt", 1);
  const previous = literal(
    identity.previous_attempt,
    ["null", "owned_old_terminal"] as const,
    "continuation previous attempt",
  );
  if (
    (action === "begin_first_attempt_next_entry" && (attempt !== 1 || previous !== "null")) ||
    (action === "begin_next_attempt" && (attempt < 2 || previous !== "owned_old_terminal"))
  )
    fail("LINEAGE_CONTINUATION_IDENTITY", action);
}

export function validateLineageIndexHeader(header: JsonObject): void {
  keys(header, [
    "format_version",
    "execution_lineage_digest",
    "deployment_id",
    "expected_database_identity",
    "repository_identity",
    "limits_profile",
  ]);
  literal(header.format_version, ["cloud-agents-platform-lineage-index/v1"], "lineage format");
  digest(header.execution_lineage_digest, "lineage digest");
  boundedString(header.deployment_id, "deployment id", 1_024);
  const database = object(header.expected_database_identity, "database identity");
  keys(database, ["database_name"]);
  boundedString(database.database_name, "database name", 1_024);
  boundedString(header.repository_identity, "repository identity", 1_024);
  literal(
    header.limits_profile,
    ["cloud-agents-platform-lineage-index-limits/v1"],
    "lineage limits",
  );
}

export function validateGenerationReserved(reserved: JsonObject): void {
  keys(reserved, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "quota_reservation_digest",
    "reserved_records",
    "reserved_bytes",
    "reserved_segments",
    "planned_segment0_header",
    "expected_segment0_header_digest",
    "continuation",
  ]);
  digests(reserved, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "quota_reservation_digest",
    "expected_segment0_header_digest",
  ]);
  uint64(reserved.reserved_records, "reserved records");
  uint64(reserved.reserved_bytes, "reserved bytes");
  uint32(reserved.reserved_segments, "reserved segments", 1);
  const continuation = nullableObject(reserved.continuation, "continuation");
  if (continuation !== null) validateLineageContinuationContext(continuation);
  const quota = quotaReservationDigest({
    limits_profile: "cloud-agents-platform-evidence-journal-limits/v1",
    execution_lineage_digest: reserved.execution_lineage_digest!,
    journal_identity_digest: reserved.journal_identity_digest!,
    runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
    schema_bundle_digest: reserved.schema_bundle_digest!,
    reserved_records: reserved.reserved_records!,
    reserved_bytes: reserved.reserved_bytes!,
    reserved_segments: reserved.reserved_segments!,
    continuation: reserved.continuation!,
  });
  if (reserved.quota_reservation_digest !== quota) fail("QUOTA_RESERVATION_DIGEST", quota);
  const header = object(reserved.planned_segment0_header, "planned segment0 header");
  validateJournalHeader(header);
  for (const field of [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "quota_reservation_digest",
    "reserved_records",
    "reserved_bytes",
    "reserved_segments",
  ] as const) {
    if (header[field] !== reserved[field]) fail("PLANNED_HEADER_BINDING", field);
  }
  if (header.segment_index !== 0 || header.previous_segment_record_digest !== null) {
    fail("PLANNED_HEADER_BINDING", "segment0");
  }
  const headerFrame: JsonObject = {
    format_version: "cloud-agents-platform-evidence-journal-frame/v1",
    sequence: 0,
    previous_record_digest: null,
    record_kind: "header",
    record: header,
  };
  if (reserved.expected_segment0_header_digest !== evidenceRecordDigest(headerFrame)) {
    fail("PLANNED_HEADER_DIGEST", String(reserved.expected_segment0_header_digest));
  }
}

export function validateGenerationActivated(activated: JsonObject): void {
  keys(activated, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "quota_reservation_digest",
    "generation_reserved_record_digest",
    "segment0_header_digest",
    "initial_journal_tail_digest",
  ]);
  digests(activated, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "quota_reservation_digest",
    "generation_reserved_record_digest",
    "segment0_header_digest",
    "initial_journal_tail_digest",
  ]);
  if (activated.segment0_header_digest !== activated.initial_journal_tail_digest) {
    fail("ACTIVATION_HEADER_TAIL", "digest mismatch");
  }
}

export function validateGenerationCheckpoint(checkpoint: JsonObject): void {
  keys(checkpoint, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "journal_next_sequence",
    "journal_tail_digest",
    "recovery_state",
    "migration_id",
    "attempt_index",
    "last_statement_intent_record_digest",
    "last_intermediate_evidence_record_digest",
    "last_commit_intent_record_digest",
    "last_terminal_digest",
    "last_resolution_digest",
    "previous_attempt_terminal_digest",
    "last_intermediate_state_digest",
    "previous_checkpoint_record_digest",
  ]);
  digests(checkpoint, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
    "journal_tail_digest",
  ]);
  uint64(checkpoint.journal_next_sequence, "journal next sequence", 1);
  literal(checkpoint.recovery_state, RECOVERY_STATES, "recovery state");
  nullableMigrationId(checkpoint.migration_id, "checkpoint migration");
  const attempt = nullableUint32(checkpoint.attempt_index, "checkpoint attempt", 1);
  for (const field of [
    "last_statement_intent_record_digest",
    "last_intermediate_evidence_record_digest",
    "last_commit_intent_record_digest",
    "last_terminal_digest",
    "last_resolution_digest",
    "previous_attempt_terminal_digest",
    "last_intermediate_state_digest",
    "previous_checkpoint_record_digest",
  ] as const)
    nullableDigest(checkpoint[field], field);
  if ((checkpoint.migration_id === null) !== (attempt === null)) {
    fail("CHECKPOINT_IDENTITY", "migration/attempt");
  }
}

export function validateGenerationSuperseded(superseded: JsonObject): void {
  keys(superseded, [
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "old_checkpoint_record_digest",
    "old_activation_record_digest",
    "old_initial_journal_tail_digest",
    "lineage_supersession_authority_digest",
    "outcome",
    "planned_generation_reserved",
  ]);
  digests(superseded, [
    "execution_lineage_digest",
    "old_journal_identity_digest",
    "old_runner_projection_decision_digest",
    "old_schema_bundle_digest",
    "lineage_supersession_authority_digest",
  ]);
  const checkpoint = nullableDigest(superseded.old_checkpoint_record_digest, "old checkpoint");
  const activation = nullableDigest(superseded.old_activation_record_digest, "old activation");
  const initialTail = nullableDigest(
    superseded.old_initial_journal_tail_digest,
    "old initial tail",
  );
  const outcome = literal(superseded.outcome, RECOVERY_OUTCOMES, "supersession outcome");
  const planned = nullableObject(superseded.planned_generation_reserved, "planned reserved");
  if (planned !== null) validateGenerationReserved(planned);
  const requiresPlanned = [
    "exact_committed_continue_successor",
    "precommit_aborted_retryable",
    "exact_pending",
    "resolved_pending",
    "activated_no_migration_progress",
  ].includes(outcome);
  if (requiresPlanned !== (planned !== null)) fail("SUPERSESSION_PLANNED", outcome);
  if (outcome === "activated_no_migration_progress") {
    if (checkpoint !== null || activation === null || initialTail === null) {
      fail("SUPERSESSION_BOUNDARY", outcome);
    }
  } else if (checkpoint === null || activation !== null || initialTail !== null) {
    fail("SUPERSESSION_BOUNDARY", outcome);
  }
}

export function validateLineageIndexFrame(frame: JsonObject): void {
  keys(frame, [
    "format_version",
    "sequence",
    "previous_record_digest",
    "record_kind",
    "record",
    "record_digest",
  ]);
  literal(
    frame.format_version,
    ["cloud-agents-platform-lineage-index-frame/v1"],
    "lineage frame format",
  );
  uint64(frame.sequence, "lineage sequence");
  nullableDigest(frame.previous_record_digest, "lineage previous record");
  const kind = literal(frame.record_kind, LINEAGE_RECORD_KINDS, "lineage record kind");
  const record = object(frame.record, "lineage record");
  switch (kind) {
    case "header":
      validateLineageIndexHeader(record);
      break;
    case "generation_reserved":
      validateGenerationReserved(record);
      break;
    case "generation_activated":
      validateGenerationActivated(record);
      break;
    case "generation_checkpoint":
      validateGenerationCheckpoint(record);
      break;
    case "generation_superseded":
      validateGenerationSuperseded(record);
      break;
  }
  const claimed = digest(frame.record_digest, "lineage record digest");
  if (claimed !== lineageRecordDigest(without(frame, "record_digest"))) {
    fail("LINEAGE_RECORD_DIGEST", claimed);
  }
  validateFramedLimit(kind, canonicalLengthPrefixed(frame).framed_size_bytes, true);
}

export function validateEvidenceChain(
  frames: readonly JsonObject[],
  witness: EvidenceChainFixtureWitness,
): void {
  validateOrderedEvidenceFramesStructural(frames);
  let previous: string | null = null;
  let currentHeader: JsonObject | null = null;
  const lastIntent = new Map<string, JsonObject>();
  const intentIdentities = new Set<string>();
  const lastIntermediate = new Map<string, JsonObject>();
  const lastIntermediateFrame = new Map<string, JsonObject>();
  const lastCommit = new Map<string, JsonObject>();
  const lastCommitFrame = new Map<string, JsonObject>();
  const terminals = new Map<string, JsonObject>();
  const terminalByMigration = new Map<string, JsonObject>();
  const resolvedPendingByMigration = new Map<string, string>();
  let framedBytes = 0;
  for (let index = 0; index < frames.length; index += 1) {
    const frame = frames[index]!;
    validateEvidenceFrame(frame);
    if (frame.sequence !== index || frame.previous_record_digest !== previous) {
      fail("EVIDENCE_FRAME_CHAIN", String(index));
    }
    const kind = String(frame.record_kind);
    const record = object(frame.record, "record");
    if (index === 0 && kind !== "header") fail("EVIDENCE_FRAME_CHAIN", "first header");
    if (kind === "header") {
      if (index !== 0) fail("EVIDENCE_DUPLICATE_HEADER", String(index));
      currentHeader = record;
      validateHeaderReceipt(record, witness.runtimeReceipt, witness.decisionRecoveryReceipt);
    } else {
      if (currentHeader === null) fail("EVIDENCE_FRAME_CHAIN", "missing header");
      validateRecordIdentityAgainstHeader(record, currentHeader);
      const key = attemptKey(record);
      if (terminals.has(key) && kind !== "ambiguous_resolution") {
        fail(
          kind === "attempt_terminal" ? "ATTEMPT_SECOND_TERMINAL" : "ATTEMPT_TERMINAL_CLOSED",
          `${key}/${kind}`,
        );
      }
      if (kind === "statement_intent") {
        const intentIdentity = statementKey(record);
        if (intentIdentities.has(intentIdentity)) fail("STATEMENT_SECOND_INTENT", intentIdentity);
        const plan = witness.signedPlans.get(statementKey(record));
        if (plan === undefined) fail("INTENT_PLAN_WITNESS", statementKey(record));
        validateIntentAgainstPlan(record, plan);
        const predecessor = terminalByMigration.get(String(record.migration_id));
        const predecessorOutcome = predecessor === undefined ? null : String(predecessor.outcome);
        const predecessorDigest =
          predecessor === undefined ? null : String(predecessor.terminal_digest);
        const permitsNext =
          predecessorOutcome === "aborted_retryable" ||
          predecessorOutcome === "ambiguous_reconciled_pending" ||
          (predecessorOutcome === "ambiguous_unresolved" &&
            resolvedPendingByMigration.get(String(record.migration_id)) === predecessorDigest);
        if (
          record.previous_attempt_terminal_digest !== (predecessor?.terminal_digest ?? null) ||
          (predecessor === undefined && record.attempt_index !== 1) ||
          (predecessor !== undefined &&
            (record.attempt_index !== Number(predecessor.attempt_index) + 1 || !permitsNext))
        ) {
          fail("INTENT_ATTEMPT_LINK", key);
        }
        lastIntent.set(key, record);
        intentIdentities.add(intentIdentity);
      } else if (kind === "intermediate") {
        const state = object(record.state, "intermediate state");
        const intent = lastIntent.get(key);
        if (intent === undefined) fail("INTERMEDIATE_INTENT_LINK", key);
        if (
          canonicalText(record.authority_before_result!) !==
            canonicalText(intent.authority_before_result!) ||
          canonicalText(record.catalog_before_result!) !==
            canonicalText(intent.catalog_before_result!)
        )
          fail("INTERMEDIATE_BEFORE_LINK", key);
        if (state.statement_sha256 !== intent.statement_sha256)
          fail("INTERMEDIATE_INTENT_LINK", "sha");
        lastIntermediate.set(key, record);
        lastIntermediateFrame.set(key, frame);
      } else if (kind === "commit_intent") {
        const intermediate = lastIntermediate.get(key);
        if (
          intermediate === undefined ||
          record.last_intermediate_state_digest !==
            object(intermediate.state, "state").intermediate_state_digest
        )
          fail("COMMIT_INTERMEDIATE_LINK", key);
        lastCommit.set(key, record);
        lastCommitFrame.set(key, frame);
      } else if (kind === "attempt_terminal") {
        const migration = String(record.migration_id);
        const finalIndex = witness.finalStatementIndexByMigration.get(migration);
        const intermediate = lastIntermediate.get(key);
        const state = intermediate === undefined ? null : object(intermediate.state, "state");
        const finalDigest =
          state !== null && state.statement_index === finalIndex
            ? String(state.intermediate_state_digest)
            : null;
        if (state !== null && state.statement_index === finalIndex) {
          const preledgerCatalog = nullableObject(
            intermediate!.preledger_catalog_result,
            "preledger catalog",
          );
          const expectedFinalCatalog = witness.finalCatalogDigestByMigration.get(migration);
          if (
            preledgerCatalog === null ||
            expectedFinalCatalog === undefined ||
            preledgerCatalog.digest !== expectedFinalCatalog
          )
            fail("FINAL_CATALOG_WITNESS", migration);
        }
        if (terminals.has(key)) fail("ATTEMPT_SECOND_TERMINAL", key);
        if (
          (String(record.outcome) === "committed" ||
            String(record.outcome).startsWith("ambiguous_")) &&
          (lastCommitFrame.get(key) === undefined ||
            frames[index - 1]?.record_digest !== lastCommitFrame.get(key)!.record_digest)
        ) {
          fail("ATTEMPT_COMMIT_BOUNDARY", key);
        }
        validateAttemptTerminalWithExternalWitness(record, {
          maxAttempts: witness.maxAttemptsByMigration.get(migration) ?? 0,
          finalIntermediateStateDigest: finalDigest,
          finalIntermediateFrame: lastIntermediateFrame.get(key) ?? null,
          commitIntentFrame: lastCommitFrame.get(key) ?? null,
          ownedRetryReceiptOracle:
            witness.ownedRetryReceiptOracles.get(String(record.terminal_digest)) ?? null,
          ownedAmbiguousBoundaryOracle:
            witness.ownedAmbiguousBoundaryOracles.get(String(record.terminal_digest)) ?? null,
          journalHeader: currentHeader,
        });
        terminals.set(key, record);
        terminalByMigration.set(migration, record);
      } else if (kind === "ambiguous_resolution") {
        const terminal = terminals.get(key);
        if (
          terminal === undefined ||
          terminal.outcome !== "ambiguous_unresolved" ||
          record.unresolved_terminal_digest !== terminal.terminal_digest ||
          record.stable_error_code !== terminal.stable_error_code ||
          index === 0 ||
          frames[index - 1]!.record_kind !== "attempt_terminal" ||
          canonicalText(frames[index - 1]!.record!) !== canonicalText(terminal)
        )
          fail("RESOLUTION_ADJACENCY", key);
        if (record.outcome === "resolved_pending") {
          resolvedPendingByMigration.set(
            String(record.migration_id),
            String(terminal.terminal_digest),
          );
        }
      }
    }
    framedBytes += canonicalLengthPrefixed(frame).framed_size_bytes;
    validateEvidenceSegmentUsage(frames.length, framedBytes);
    previous = String(frame.record_digest);
  }
}

export function validateLineageChain(
  frames: readonly JsonObject[],
  witness: LineageChainWitness,
): void {
  if (frames.length === 0) fail("LINEAGE_CHAIN_EMPTY", "frames");
  let previous: string | null = null;
  let header: JsonObject | null = null;
  let reservedFrame: JsonObject | null = null;
  let activated: JsonObject | null = null;
  let checkpointFrame: JsonObject | null = null;
  let superseded: JsonObject | null = null;
  let supersededFrame: JsonObject | null = null;
  let generationCount = 0;
  let indexBytes = 0;
  for (let index = 0; index < frames.length; index += 1) {
    const frame = frames[index]!;
    validateLineageIndexFrame(frame);
    if (frame.sequence !== index || frame.previous_record_digest !== previous) {
      fail("LINEAGE_FRAME_CHAIN", String(index));
    }
    const record = object(frame.record, "lineage record");
    const kind = String(frame.record_kind);
    if (index === 0) {
      if (kind !== "header") fail("LINEAGE_FRAME_CHAIN", "first header");
      header = record;
      const database = object(record.expected_database_identity, "database identity");
      if (
        record.execution_lineage_digest !== witness.executionLineageDigest ||
        record.deployment_id !== witness.deploymentId ||
        database.database_name !== witness.databaseName ||
        record.repository_identity !== witness.repositoryIdentity
      )
        fail("LINEAGE_HEADER_WITNESS", "identity");
      const computedLineage = executionLineageDigest({
        deployment_id: record.deployment_id!,
        expected_database_identity: record.expected_database_identity!,
        repository_identity: record.repository_identity!,
      });
      if (computedLineage !== record.execution_lineage_digest) {
        fail("LINEAGE_HEADER_WITNESS", "constituent digest");
      }
    } else {
      if (kind === "header") fail("LINEAGE_SECOND_HEADER", String(index));
      if (superseded !== null && kind !== "generation_reserved") {
        fail("LINEAGE_SUPERSEDED_CLOSED", `${index}/${kind}`);
      }
      if (header === null || record.execution_lineage_digest !== header.execution_lineage_digest) {
        fail("LINEAGE_IDENTITY_LINK", kind);
      }
      if (kind === "generation_reserved") {
        if (generationCount > 0 && superseded === null) {
          fail("LINEAGE_RESERVED_WITHOUT_SUPERSESSION", String(index));
        }
        if (superseded !== null) {
          if (
            supersededFrame === null ||
            frame.sequence !== Number(supersededFrame.sequence) + 1 ||
            canonicalText(record) !== canonicalText(superseded.planned_generation_reserved!)
          )
            fail("SUPERSESSION_RESERVED_ADJACENCY", String(index));
        }
        reservedFrame = frame;
        generationCount += 1;
        activated = null;
        checkpointFrame = null;
        superseded = null;
        supersededFrame = null;
      } else if (kind === "generation_activated") {
        if (reservedFrame === null || activated !== null) {
          fail("ACTIVATION_RESERVED_LINK", String(index));
        }
        if (
          record.generation_reserved_record_digest !== reservedFrame.record_digest ||
          record.segment0_header_digest !==
            object(reservedFrame.record, "reserved").expected_segment0_header_digest ||
          record.execution_lineage_digest !==
            object(reservedFrame.record, "reserved").execution_lineage_digest ||
          record.journal_identity_digest !==
            object(reservedFrame.record, "reserved").journal_identity_digest ||
          record.runner_projection_decision_digest !==
            object(reservedFrame.record, "reserved").runner_projection_decision_digest ||
          record.schema_bundle_digest !==
            object(reservedFrame.record, "reserved").schema_bundle_digest ||
          record.quota_reservation_digest !==
            object(reservedFrame.record, "reserved").quota_reservation_digest
        )
          fail("ACTIVATION_RESERVED_LINK", String(index));
        const journal = String(record.journal_identity_digest);
        const actualFrame = witness.actualSegment0Frames.get(journal);
        if (actualFrame !== undefined) validateEvidenceFrame(actualFrame);
        if (
          actualFrame === undefined ||
          actualFrame.sequence !== 0 ||
          actualFrame.previous_record_digest !== null ||
          actualFrame.record_kind !== "header" ||
          actualFrame.record_digest !== record.segment0_header_digest ||
          canonicalText(actualFrame) !==
            canonicalText({
              format_version: "cloud-agents-platform-evidence-journal-frame/v1",
              sequence: 0,
              previous_record_digest: null,
              record_kind: "header",
              record: object(reservedFrame.record, "reserved").planned_segment0_header!,
              record_digest: object(reservedFrame.record, "reserved")
                .expected_segment0_header_digest!,
            })
        )
          fail("ACTIVATION_HEADER_WITNESS", journal);
        activated = record;
      } else if (kind === "generation_checkpoint") {
        if (activated === null) fail("CHECKPOINT_ACTIVATION_LINK", String(index));
        if (
          record.journal_identity_digest !== activated.journal_identity_digest ||
          (checkpointFrame !== null &&
            record.previous_checkpoint_record_digest !== checkpointFrame.record_digest)
        )
          fail("CHECKPOINT_CHAIN", String(index));
        const journalFrames = witness.journalFramesByIdentity.get(
          String(record.journal_identity_digest),
        );
        if (
          journalFrames === undefined ||
          journalFrames.length !== record.journal_next_sequence ||
          journalFrames.at(-1)?.record_digest !== record.journal_tail_digest
        )
          fail("CHECKPOINT_JOURNAL_TAIL", String(index));
        const summary = summarizeJournalFrames(journalFrames);
        for (const field of [
          "recovery_state",
          "migration_id",
          "attempt_index",
          "last_statement_intent_record_digest",
          "last_intermediate_evidence_record_digest",
          "last_commit_intent_record_digest",
          "last_terminal_digest",
          "last_resolution_digest",
          "previous_attempt_terminal_digest",
          "last_intermediate_state_digest",
        ] as const) {
          if (record[field] !== summary[field]) fail("CHECKPOINT_SUMMARY", field);
        }
        checkpointFrame = frame;
      } else if (kind === "generation_superseded") {
        const authority = witness.supersessionAuthorities.get(
          String(record.lineage_supersession_authority_digest),
        );
        if (
          authority === undefined ||
          authority.domain !== "cloud-agents-platform-lineage-supersession-authority/v1" ||
          lineageSupersessionAuthorityDigest(without(authority, "domain")) !==
            record.lineage_supersession_authority_digest ||
          authority.observed_outcome !== record.outcome ||
          authority.execution_lineage_digest !== record.execution_lineage_digest ||
          authority.old_journal_identity_digest !== record.old_journal_identity_digest ||
          authority.old_runner_projection_decision_digest !==
            record.old_runner_projection_decision_digest ||
          authority.old_schema_bundle_digest !== record.old_schema_bundle_digest ||
          authority.old_checkpoint_record_digest !== record.old_checkpoint_record_digest ||
          authority.old_activation_record_digest !== record.old_activation_record_digest ||
          authority.old_initial_journal_tail_digest !== record.old_initial_journal_tail_digest ||
          canonicalText(authority.continuation!) !==
            canonicalText(
              record.planned_generation_reserved === null
                ? null
                : object(record.planned_generation_reserved, "planned").continuation!,
            )
        ) {
          fail("SUPERSESSION_AUTHORITY_WITNESS", String(index));
        }
        if (
          record.outcome === "activated_no_migration_progress"
            ? activated === null ||
              checkpointFrame !== null ||
              record.old_activation_record_digest !== frames[index - 1]!.record_digest ||
              record.old_initial_journal_tail_digest !== activated.initial_journal_tail_digest
            : checkpointFrame === null ||
              record.old_checkpoint_record_digest !== checkpointFrame.record_digest ||
              authority.old_terminal_digest !==
                object(checkpointFrame.record, "checkpoint").last_terminal_digest ||
              authority.old_resolution_digest !==
                object(checkpointFrame.record, "checkpoint").last_resolution_digest
        )
          fail("SUPERSESSION_BOUNDARY", String(record.outcome));
        superseded = record;
        supersededFrame = frame;
      }
    }
    indexBytes += canonicalLengthPrefixed(frame).framed_size_bytes;
    validateLineageIndexUsage(frames.length, indexBytes);
    previous = String(frame.record_digest);
  }
}

function summarizeJournalFrames(frames: readonly JsonObject[]): JsonObject {
  validateOrderedEvidenceFramesStructural(frames);
  let lastIntent: JsonObject | null = null;
  let lastIntentFrame: JsonObject | null = null;
  let lastIntermediate: JsonObject | null = null;
  let lastIntermediateFrame: JsonObject | null = null;
  let lastCommitFrame: JsonObject | null = null;
  let lastTerminal: JsonObject | null = null;
  let lastResolution: JsonObject | null = null;
  for (const frame of frames) {
    validateEvidenceFrame(frame);
    const record = object(frame.record, "journal record");
    switch (frame.record_kind) {
      case "statement_intent":
        lastIntent = record;
        lastIntentFrame = frame;
        break;
      case "intermediate":
        lastIntermediate = object(record.state, "intermediate state");
        lastIntermediateFrame = frame;
        break;
      case "commit_intent":
        lastCommitFrame = frame;
        break;
      case "attempt_terminal":
        lastTerminal = record;
        break;
      case "ambiguous_resolution":
        lastResolution = record;
        break;
    }
  }
  const identity = lastResolution ?? lastTerminal ?? lastIntermediate ?? lastIntent;
  let state = "brand_new";
  if (lastResolution !== null) {
    state =
      lastResolution.outcome === "resolved_committed"
        ? "completed"
        : lastResolution.outcome === "resolved_divergent"
          ? "divergent"
          : "terminal";
  } else if (lastTerminal !== null) {
    state =
      lastTerminal.outcome === "committed" ||
      lastTerminal.outcome === "ambiguous_reconciled_committed"
        ? "completed"
        : lastTerminal.outcome === "ambiguous_divergent"
          ? "divergent"
          : lastTerminal.outcome === "ambiguous_unresolved"
            ? "ambiguous_unresolved"
            : "terminal";
  } else if (lastCommitFrame !== null) state = "dangling_commit_intent";
  else if (lastIntermediate !== null) state = "dangling_intermediate";
  else if (lastIntent !== null) state = "dangling_statement_intent";
  return {
    recovery_state: state,
    migration_id: identity?.migration_id ?? null,
    attempt_index: identity?.attempt_index ?? null,
    last_statement_intent_record_digest: lastIntentFrame?.record_digest ?? null,
    last_intermediate_evidence_record_digest: lastIntermediateFrame?.record_digest ?? null,
    last_commit_intent_record_digest: lastCommitFrame?.record_digest ?? null,
    last_terminal_digest: lastTerminal?.terminal_digest ?? null,
    last_resolution_digest: lastResolution?.resolution_digest ?? null,
    previous_attempt_terminal_digest:
      lastResolution?.unresolved_terminal_digest ??
      lastTerminal?.previous_attempt_terminal_digest ??
      lastIntermediate?.previous_attempt_terminal_digest ??
      lastIntent?.previous_attempt_terminal_digest ??
      null,
    last_intermediate_state_digest: lastIntermediate?.intermediate_state_digest ?? null,
  };
}

function validateOrderedEvidenceFramesStructural(frames: readonly JsonObject[]): void {
  if (frames.length === 0) fail("EVIDENCE_CHAIN_EMPTY", "frames");
  let previous: string | null = null;
  for (let index = 0; index < frames.length; index += 1) {
    const frame = frames[index]!;
    validateEvidenceFrame(frame);
    if (index !== 0 && frame.record_kind === "header") {
      fail("EVIDENCE_DUPLICATE_HEADER", String(index));
    }
    if (
      frame.sequence !== index ||
      frame.previous_record_digest !== previous ||
      (index === 0 && frame.record_kind !== "header")
    ) {
      fail("EVIDENCE_FRAME_CHAIN", String(index));
    }
    previous = String(frame.record_digest);
  }
}

type TerminalExternalWitness = {
  readonly maxAttempts: number;
  readonly finalIntermediateStateDigest: string | null;
  readonly finalIntermediateFrame: JsonObject | null;
  readonly commitIntentFrame: JsonObject | null;
  readonly ownedRetryReceiptOracle: JsonObject | null;
  readonly ownedAmbiguousBoundaryOracle: JsonObject | null;
  readonly journalHeader: JsonObject;
};

function validateAttemptTerminalWithExternalWitness(
  terminal: JsonObject,
  witness: TerminalExternalWitness,
): void {
  validateAttemptTerminalState(terminal);
  if (
    !Number.isInteger(witness.maxAttempts) ||
    witness.maxAttempts < 1 ||
    witness.maxAttempts > UINT32_MAX
  )
    fail("ATTEMPT_TERMINAL_WITNESS", "max attempts");
  const attempt = uint32(terminal.attempt_index, "terminal attempt", 1);
  const outcome = String(terminal.outcome);
  if (outcome === "aborted_retryable" && attempt >= witness.maxAttempts) {
    fail("ATTEMPT_TERMINAL_RETRY_BUDGET", `${attempt}/${witness.maxAttempts}`);
  }
  if (outcome === "committed" || outcome.startsWith("ambiguous_")) {
    if (
      witness.finalIntermediateStateDigest === null ||
      terminal.last_intermediate_state_digest !== witness.finalIntermediateStateDigest ||
      witness.finalIntermediateFrame === null
    )
      fail("ATTEMPT_TERMINAL_FINAL_WITNESS", outcome);
  }
  if (terminal.retry_proof === null) {
    if (witness.ownedRetryReceiptOracle !== null) fail("RETRY_RECEIPT_ORACLE", "unexpected");
  } else {
    if (witness.ownedRetryReceiptOracle === null) fail("RETRY_RECEIPT_ORACLE", "missing");
    validateRetryReceiptOracle(
      witness.ownedRetryReceiptOracle,
      object(terminal.retry_proof, "retry proof"),
      terminal,
      witness.journalHeader,
    );
    const oracle = witness.ownedRetryReceiptOracle;
    if (
      object(terminal.retry_proof, "retry proof").proof_kind === "commit_rejected_exact_predecessor"
    ) {
      if (
        witness.commitIntentFrame === null ||
        oracle.commit_intent_record_digest !== witness.commitIntentFrame.record_digest
      ) {
        fail("RETRY_RECEIPT_ORACLE", "commit intent record digest");
      }
    } else if (witness.commitIntentFrame !== null) {
      fail("RETRY_RECEIPT_ORACLE", "unexpected commit intent");
    }
  }
  if (outcome.startsWith("ambiguous_")) {
    if (
      witness.ownedAmbiguousBoundaryOracle === null ||
      witness.finalIntermediateFrame === null ||
      witness.commitIntentFrame === null
    )
      fail("AMBIGUOUS_BOUNDARY_ORACLE", "missing");
    validateAmbiguousBoundaryOracle(
      witness.ownedAmbiguousBoundaryOracle,
      witness.finalIntermediateFrame,
      witness.commitIntentFrame,
      terminal,
    );
  } else if (witness.ownedAmbiguousBoundaryOracle !== null) {
    fail("AMBIGUOUS_BOUNDARY_ORACLE", "unexpected");
  }
}

function validateRetryReceiptOracle(
  oracle: JsonObject,
  proof: JsonObject,
  terminal: JsonObject,
  header: JsonObject,
): void {
  keys(oracle, [
    "oracle_kind",
    "old_receipt_kind",
    "proof_kind",
    "migration_id",
    "attempt_index",
    "execution_lineage_digest",
    "journal_identity_digest",
    "old_connection_lifecycle_id",
    "new_connection_lifecycle_id",
    "old_before_new",
    "commit_called",
    "rollback_succeeded",
    "old_handle_irrevocably_closed",
    "ready_for_query",
    "commit_rejected_reason",
    "commit_intent_record_digest",
    "recovery_predecessor",
  ]);
  literal(oracle.oracle_kind, ["owned_retry_receipt_pair/v1"], "retry oracle kind");
  if (
    oracle.proof_kind !== proof.proof_kind ||
    oracle.migration_id !== terminal.migration_id ||
    oracle.attempt_index !== terminal.attempt_index ||
    oracle.execution_lineage_digest !== header.execution_lineage_digest ||
    oracle.journal_identity_digest !== header.journal_identity_digest
  )
    fail("RETRY_RECEIPT_ORACLE", "identity");
  const oldLifecycle = boundedString(oracle.old_connection_lifecycle_id, "old lifecycle", 128);
  const newLifecycle = boundedString(oracle.new_connection_lifecycle_id, "new lifecycle", 128);
  if (oldLifecycle === newLifecycle || oracle.old_before_new !== true) {
    fail("RETRY_RECEIPT_ORACLE", "lifecycle ordering");
  }
  const proofKind = String(proof.proof_kind);
  const expectedReceiptKind =
    proofKind === "precommit_connection_terminated_exact_predecessor"
      ? "owned_precommit_connection_terminated"
      : proofKind === "commit_rejected_exact_predecessor"
        ? "owned_commit_rejected"
        : "owned_rollback";
  if (oracle.old_receipt_kind !== expectedReceiptKind) {
    fail("RETRY_RECEIPT_ORACLE", "old receipt kind");
  }
  const commitCalled = boolean(oracle.commit_called, "commit called");
  const rollbackSucceeded =
    oracle.rollback_succeeded === null
      ? null
      : boolean(oracle.rollback_succeeded, "rollback succeeded");
  const closed = boolean(oracle.old_handle_irrevocably_closed, "old handle closed");
  const ready = oracle.ready_for_query === null ? null : boolean(oracle.ready_for_query, "ready");
  if (
    (["projection_transient_exact_predecessor", "precommit_rollback_exact_predecessor"].includes(
      proofKind,
    ) &&
      (commitCalled || !closed || ready !== null || rollbackSucceeded !== true)) ||
    (proofKind === "precommit_connection_terminated_exact_predecessor" &&
      (commitCalled || !closed || ready !== null || rollbackSucceeded !== null)) ||
    (proofKind === "commit_rejected_exact_predecessor" &&
      (!commitCalled || !closed || ready !== true || rollbackSucceeded !== null))
  )
    fail("RETRY_RECEIPT_ORACLE", proofKind);
  const recovery = object(oracle.recovery_predecessor, "recovery predecessor");
  keys(recovery, [
    "ordered_ledger_rows",
    "ledger_prefix_digest",
    "attempt_predecessor_catalog_digest",
    "observed_catalog_digest",
    "authority_result_digest",
  ]);
  const rows = array(recovery.ordered_ledger_rows, "ordered ledger rows").map((row) =>
    object(row, "ledger row"),
  );
  if (recovery.ledger_prefix_digest !== ledgerPrefixDigest(rows)) {
    fail("RETRY_RECEIPT_ORACLE", "ledger prefix digest");
  }
  for (const field of [
    "attempt_predecessor_catalog_digest",
    "observed_catalog_digest",
    "ledger_prefix_digest",
    "authority_result_digest",
  ] as const) {
    if (recovery[field] !== proof[field]) fail("RETRY_RECEIPT_ORACLE", field);
  }
  if (oracle.commit_rejected_reason !== proof.commit_rejected_reason) {
    fail("RETRY_RECEIPT_ORACLE", "commit rejected reason");
  }
  if (
    proofKind === "commit_rejected_exact_predecessor"
      ? oracle.commit_intent_record_digest === null
      : oracle.commit_intent_record_digest !== null
  ) {
    fail("RETRY_RECEIPT_ORACLE", "commit intent boundary");
  }
}

function validateAmbiguousBoundaryOracle(
  oracle: JsonObject,
  intermediateFrame: JsonObject,
  commitFrame: JsonObject,
  terminal: JsonObject,
): void {
  keys(oracle, [
    "oracle_kind",
    "migration_id",
    "attempt_index",
    "commit_called",
    "final_intermediate_record_digest",
    "commit_intent_record_digest",
  ]);
  literal(oracle.oracle_kind, ["owned_ambiguous_commit_boundary/v1"], "ambiguous oracle kind");
  if (
    oracle.migration_id !== terminal.migration_id ||
    oracle.attempt_index !== terminal.attempt_index ||
    oracle.commit_called !== true ||
    oracle.final_intermediate_record_digest !== intermediateFrame.record_digest ||
    oracle.commit_intent_record_digest !== commitFrame.record_digest
  )
    fail("AMBIGUOUS_BOUNDARY_ORACLE", "identity/link");
  if (
    Number(commitFrame.sequence) !== Number(intermediateFrame.sequence) + 1 ||
    commitFrame.previous_record_digest !== intermediateFrame.record_digest
  )
    fail("AMBIGUOUS_BOUNDARY_ORACLE", "adjacency");
}

function validateProjectionMetadata(metadata: JsonObject): void {
  keys(metadata, [
    "projection_kind",
    "digest_domain",
    "adapter_profile",
    "snapshot",
    "verified_subject_digest",
    "scope",
    "limits_profile",
    "query_count",
    "row_count",
    "total_bytes",
    "redaction_profile",
  ]);
  const kind = literal(
    metadata.projection_kind,
    ["authority", "catalog", "catalog_state"] as const,
    "projection kind",
  );
  const mapping = {
    authority: ["cloud-agents-platform-authority-projection/v1", "postgresql-authority-v1"],
    catalog: ["cloud-agents-platform-catalog-projection/v1", "postgresql-catalog-v1"],
    catalog_state: ["cloud-agents-platform-catalog-state/v1", "postgresql-catalog-v1"],
  } as const;
  if (
    metadata.digest_domain !== mapping[kind][0] ||
    metadata.adapter_profile !== mapping[kind][1]
  ) {
    fail("PROJECTION_METADATA_MAPPING", kind);
  }
  const scope = nullableObject(metadata.scope, "projection scope");
  if (scope !== null) validateProjectionScope(scope);
  if (
    (kind === "authority" && scope !== null) ||
    (kind !== "authority" && scope === null) ||
    (kind === "catalog" && scope?.scope_kind !== "final")
  )
    fail("PROJECTION_METADATA_MAPPING", "scope");
  validateSnapshotMetadata(object(metadata.snapshot, "snapshot"));
  digest(metadata.verified_subject_digest, "verified subject");
  literal(
    metadata.limits_profile,
    ["cloud-agents-platform-projection-limits/v1"],
    "projection limits",
  );
  const queryCount = uint32(metadata.query_count, "query count");
  const rows = uint64(metadata.row_count, "row count");
  const bytes = uint64(metadata.total_bytes, "total bytes");
  if (queryCount > 128 || rows > 8_192 || bytes > 8_388_608)
    fail("PROJECTION_METADATA_LIMIT", kind);
  literal(
    metadata.redaction_profile,
    ["cloud-agents-platform-projection-redaction/v1"],
    "redaction profile",
  );
}

function validateSnapshotMetadata(snapshot: JsonObject): void {
  keys(snapshot, [
    "mode",
    "ownership",
    "postgres_major",
    "server_version_num",
    "database_name",
    "authority_phase",
    "session_user",
    "current_user",
    "isolation_level",
    "access_mode",
    "deferrable",
    "tx_status",
    "migration_id",
    "statement_index",
  ]);
  const mode = literal(
    snapshot.mode,
    ["idle_read_repeatable_read_only", "migration_serializable_read_write"] as const,
    "snapshot mode",
  );
  const ownership = literal(
    snapshot.ownership,
    ["owned_idle", "borrowed_migration"] as const,
    "snapshot ownership",
  );
  const major = uint16(snapshot.postgres_major, "postgres major", 1);
  if (major !== 15 && major !== 16 && major !== 17) fail("SNAPSHOT_MAJOR", String(major));
  uint32(snapshot.server_version_num, "server version", 1);
  boundedString(snapshot.database_name, "database name", 1_024);
  const phase = literal(
    snapshot.authority_phase,
    ["connected_session", "migration_role", "migration_transaction"] as const,
    "authority phase",
  );
  boundedString(snapshot.session_user, "session user", 1_024);
  boundedString(snapshot.current_user, "current user", 1_024);
  literal(snapshot.tx_status, ["T"], "tx status");
  const migration = nullableMigrationId(snapshot.migration_id, "snapshot migration");
  const statement = nullableUint32(snapshot.statement_index, "snapshot statement");
  if (mode === "idle_read_repeatable_read_only") {
    if (
      ownership !== "owned_idle" ||
      snapshot.isolation_level !== "repeatable_read" ||
      snapshot.access_mode !== "read_only" ||
      snapshot.deferrable !== false ||
      phase === "migration_transaction" ||
      migration !== null ||
      statement !== null
    )
      fail("SNAPSHOT_METADATA_MAPPING", mode);
  } else if (
    ownership !== "borrowed_migration" ||
    snapshot.isolation_level !== "serializable" ||
    snapshot.access_mode !== "read_write" ||
    snapshot.deferrable !== false ||
    phase !== "migration_transaction" ||
    migration === null
  )
    fail("SNAPSHOT_METADATA_MAPPING", mode);
}

function validateStatementClassification(classification: JsonObject): void {
  keys(classification, [
    "profile",
    "command",
    "object_kind",
    "target_identity",
    "grantee",
    "special_case",
  ]);
  literal(classification.profile, ["postgresql-ddl-v1"], "classification profile");
  boundedString(classification.command, "classification command", 64);
  boundedString(classification.object_kind, "classification kind", 64);
  boundedString(classification.target_identity, "classification target", 1_024);
  nullableBoundedString(classification.grantee, "classification grantee", 256);
  nullableBoundedString(classification.special_case, "classification special case", 256);
}

function validateLedgerRow(row: JsonObject): void {
  keys(row, [
    "migration_id",
    "migration_name",
    "predecessor_id",
    "phase",
    "schema_from",
    "schema_to",
    "compatible_binary_min",
    "compatible_binary_max",
    "sql_path",
    "sql_size_bytes",
    "sql_sha256",
    "bundle_digest",
    "transaction_mode",
    "reentrancy",
    "rollback_boundary",
    "requires_live_instance_preflight",
    "requires_pitr_preflight",
  ]);
  migrationId(row.migration_id, "ledger migration");
  nullableMigrationId(row.predecessor_id, "ledger predecessor");
  for (const field of [
    "migration_name",
    "phase",
    "schema_from",
    "schema_to",
    "compatible_binary_min",
    "compatible_binary_max",
    "transaction_mode",
    "reentrancy",
    "rollback_boundary",
  ] as const)
    boundedString(row[field], field, 256);
  const path = boundedString(row.sql_path, "ledger sql path", 512);
  if (!SQL_ARTIFACT_PATH.test(path) || path.includes("..")) fail("LEDGER_SQL_PATH", path);
  uint64(row.sql_size_bytes, "ledger sql size");
  digest(row.sql_sha256, "ledger sql digest");
  digest(row.bundle_digest, "ledger bundle digest");
  boolean(row.requires_live_instance_preflight, "live preflight");
  boolean(row.requires_pitr_preflight, "pitr preflight");
}

function validateEvidenceRecord(
  kind: (typeof EVIDENCE_RECORD_KINDS)[number],
  record: JsonObject,
): void {
  switch (kind) {
    case "header":
      validateJournalHeader(record);
      break;
    case "statement_intent":
      validateStatementIntent(record);
      break;
    case "intermediate":
      validateStatementIntermediateEvidence(record);
      break;
    case "commit_intent":
      validateCommitIntent(record);
      break;
    case "attempt_terminal":
      validateAttemptTerminalState(record);
      break;
    case "ambiguous_resolution":
      validateAmbiguousResolutionState(record);
      break;
  }
}

function validateQuotaReservationInput(input: JsonObject): void {
  literal(
    input.limits_profile,
    ["cloud-agents-platform-evidence-journal-limits/v1"],
    "quota limits",
  );
  digests(input, [
    "execution_lineage_digest",
    "journal_identity_digest",
    "runner_projection_decision_digest",
    "schema_bundle_digest",
  ]);
  const records = uint64(input.reserved_records, "reserved records", 1);
  const bytes = uint64(input.reserved_bytes, "reserved bytes");
  const segments = uint32(input.reserved_segments, "reserved segments", 1);
  if (records > 65_536 || bytes < 1 || bytes > 268_435_456 || segments > 16) {
    fail("QUOTA_RESERVATION_LIMIT", `${records}/${bytes}/${segments}`);
  }
  const continuation = nullableObject(input.continuation, "quota continuation");
  if (continuation !== null) validateLineageContinuationContext(continuation);
}

function validateHeaderReceipt(
  header: JsonObject,
  runtime: JsonObject,
  recovery: JsonObject,
): void {
  keys(runtime, ["kind", "sha256", "size_bytes"]);
  keys(recovery, ["kind", "sha256", "size_bytes"]);
  if (
    runtime.kind !== "runtime" ||
    recovery.kind !== "decision_recovery" ||
    header.outer_artifact_digest !== runtime.sha256 ||
    header.outer_artifact_size_bytes !== runtime.size_bytes ||
    header.decision_recovery_artifact_sha256 !== recovery.sha256 ||
    header.decision_recovery_artifact_size_bytes !== recovery.size_bytes
  )
    fail("CONTENT_RECEIPT_WITNESS", "header");
}

function validateRecordIdentityAgainstHeader(record: JsonObject, header: JsonObject): void {
  const identity = Object.hasOwn(record, "state") ? object(record.state, "record state") : record;
  for (const field of [
    "schema_bundle_digest",
    "authority_profile_digest",
    "authority_binding_digest",
  ] as const) {
    if (identity[field] !== header[field]) fail("EVIDENCE_HEADER_BINDING", field);
  }
}

function validateIntentAgainstPlan(intent: JsonObject, plan: JsonObject): void {
  keys(plan, [
    "migration_id",
    "statement_index",
    "sql_artifact_sha256",
    "sql_artifact_size_bytes",
    "start_offset",
    "end_offset",
    "statement_sha256",
    "classification",
    "expected_transition_digest",
  ]);
  for (const field of Object.keys(plan)) {
    if (canonicalText(intent[field]!) !== canonicalText(plan[field]!)) {
      fail("INTENT_PLAN_WITNESS", field);
    }
  }
}

function attemptKey(record: JsonObject): string {
  const identity = Object.hasOwn(record, "state") ? object(record.state, "record state") : record;
  return `${String(identity.migration_id)}:${String(identity.attempt_index)}`;
}

function statementKey(record: JsonObject): string {
  const identity = Object.hasOwn(record, "state") ? object(record.state, "record state") : record;
  return `${attemptKey(record)}:${String(identity.statement_index)}`;
}

function validateFramedLimit(kind: string, bytes: number, lineage: boolean): void {
  if (lineage) {
    if (!Object.hasOwn(LINEAGE_LIMITS.recordBytes, kind)) fail("FRAMED_RECORD_LIMIT", kind);
    validateLineageFramedSizeForKind(kind as keyof typeof LINEAGE_LIMITS.recordBytes, bytes);
  } else {
    if (!Object.hasOwn(EVIDENCE_LIMITS.recordBytes, kind)) fail("FRAMED_RECORD_LIMIT", kind);
    validateEvidenceFramedSizeForKind(kind as keyof typeof EVIDENCE_LIMITS.recordBytes, bytes);
  }
}

function historicalContinuationConstraintKind(
  outcome: (typeof RECOVERY_OUTCOMES)[number],
): "must_be_null" | "exact_identity" | "exact_carry_old_generation" {
  if (outcome === "activated_no_migration_progress") return "exact_carry_old_generation";
  if (
    outcome === "exact_committed_continue_successor" ||
    outcome === "precommit_aborted_retryable" ||
    outcome === "exact_pending" ||
    outcome === "resolved_pending"
  ) {
    return "exact_identity";
  }
  return "must_be_null";
}

function continuationIdentityFromContext(context: JsonObject): JsonObject {
  return {
    start_action: context.start_action!,
    migration_id: context.migration_id!,
    attempt_index: context.attempt_index!,
    previous_attempt:
      context.previous_attempt_terminal_digest === null ? "null" : "owned_old_terminal",
  };
}

function canonicalRfc3339Utc(value: MigrationJson, label: string): string {
  const text = boundedString(value, label, 64);
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/u.test(text)) {
    fail("RFC3339_UTC", label);
  }
  const parsed = new Date(text);
  if (Number.isNaN(parsed.getTime()) || parsed.toISOString().replace(".000Z", "Z") !== text) {
    fail("RFC3339_UTC", label);
  }
  return text;
}

function flatDigest(domain: string, body: JsonObject): string {
  if (Object.hasOwn(body, "domain")) fail("DIGEST_DOMAIN_FIELD", domain);
  return migrationDigest({ domain, ...body });
}

function rawSha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function canonicalText(value: MigrationJson): string {
  return new TextDecoder().decode(canonicalizeMigrationJson(value));
}

function without(value: JsonObject, key: string): JsonObject {
  const copy = { ...value };
  delete copy[key];
  return copy;
}

function keys(value: JsonObject, expected: readonly string[]): void {
  const actual = Object.keys(value).toSorted();
  const wanted = [...expected].toSorted();
  if (actual.join("\0") !== wanted.join("\0")) {
    fail("UNKNOWN_OR_MISSING_FIELD", `${actual.join(",")} != ${wanted.join(",")}`);
  }
}

function object(value: MigrationJson, label: string): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    fail("EXPECTED_OBJECT", label);
  return value as JsonObject;
}

function array(value: MigrationJson, label: string): MigrationJson[] {
  if (!Array.isArray(value)) fail("EXPECTED_ARRAY", label);
  return value as MigrationJson[];
}

function nullableObject(value: MigrationJson, label: string): JsonObject | null {
  return value === null ? null : object(value, label);
}

function boundedString(value: MigrationJson, label: string, maxBytes: number): string {
  if (
    typeof value !== "string" ||
    value.length === 0 ||
    Buffer.byteLength(value, "utf8") > maxBytes
  ) {
    fail("EXPECTED_BOUNDED_STRING", label);
  }
  return value as string;
}

function nullableBoundedString(
  value: MigrationJson,
  label: string,
  maxBytes: number,
): string | null {
  return value === null ? null : boundedString(value, label, maxBytes);
}

function canonicalBase64url(value: MigrationJson, label: string): Uint8Array {
  const encoded = boundedString(value, label, 1_048_576);
  if (!/^[A-Za-z0-9_-]+$/u.test(encoded) || encoded.includes("=")) {
    fail("BASE64URL_CANONICAL", label);
  }
  const decoded = Buffer.from(encoded, "base64url");
  if (decoded.length === 0 || decoded.toString("base64url") !== encoded) {
    fail("BASE64URL_CANONICAL", label);
  }
  return decoded;
}

function boolean(value: MigrationJson, label: string): boolean {
  if (typeof value !== "boolean") fail("EXPECTED_BOOLEAN", label);
  return value as boolean;
}

function literal<const T extends readonly MigrationJson[]>(
  value: MigrationJson,
  expected: T,
  label: string,
): T[number] {
  if (!expected.includes(value)) fail("UNEXPECTED_VALUE", `${label}: ${String(value)}`);
  return value as T[number];
}

function uint64(value: MigrationJson, label: string, minimum = 0): number {
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value < minimum ||
    value > JSON_SAFE_UINT64_MAX
  )
    fail("UINT64_SAFE_RANGE", label);
  return value as number;
}

function uint32(value: MigrationJson, label: string, minimum = 0): number {
  const result = uint64(value, label, minimum);
  if (result > UINT32_MAX) fail("UINT32_RANGE", label);
  return result;
}

function uint16(value: MigrationJson, label: string, minimum = 0): number {
  const result = uint64(value, label, minimum);
  if (result > UINT16_MAX) fail("UINT16_RANGE", label);
  return result;
}

function nullableUint32(value: MigrationJson, label: string, minimum = 0): number | null {
  return value === null ? null : uint32(value, label, minimum);
}

function digest(value: MigrationJson, label: string): string {
  const result = boundedString(value, label, 71);
  if (!DIGEST.test(result)) fail("DIGEST_FORMAT", label);
  return result;
}

function nullableDigest(value: MigrationJson, label: string): string | null {
  return value === null ? null : digest(value, label);
}

function digests(value: JsonObject, fields: readonly string[]): void {
  for (const field of fields) digest(value[field], field);
}

function migrationId(value: MigrationJson, label: string): string {
  const result = boundedString(value, label, 6);
  if (!MIGRATION_ID.test(result)) fail("MIGRATION_ID", label);
  return result;
}

function nullableMigrationId(value: MigrationJson, label: string): string | null {
  return value === null ? null : migrationId(value, label);
}

function fail(code: string, message: string): never {
  throw new MigrationValidationError(code, message);
}

export type GeneratedEvidenceContractFixtures = {
  readonly documents: ReadonlyMap<string, JsonObject>;
  readonly duplicateEvidenceFrame: Uint8Array;
  readonly duplicateEvidenceNestedRecord: Uint8Array;
  readonly duplicateLineageFrame: Uint8Array;
};

export function buildEvidenceContractFixtures(): GeneratedEvidenceContractFixtures {
  const d = (character: string): string => `sha256:${character.repeat(64)}`;
  const lineageInput: JsonObject = {
    deployment_id: "fixture_deployment",
    expected_database_identity: { database_name: "cloud_agents_fixture" },
    repository_identity: "github.com/hxp0618/cloud-agents",
  };
  const lineageDigest = executionLineageDigest(lineageInput);
  const continuation: JsonObject = {
    start_action: "begin_next_attempt",
    migration_id: "000001",
    attempt_index: 2,
    previous_attempt_terminal_digest: d("a"),
    source_journal_identity_digest: d("b"),
    source_checkpoint_record_digest: d("c"),
    source_terminal_digest: d("a"),
  };
  const header = fixtureJournalHeader(lineageDigest, null);
  const beforeAuthority = fixtureProjectionResult("authority", "000001", 0, d("1"), null);
  const beforeCatalog = fixtureProjectionResult(
    "catalog_state",
    "000001",
    0,
    d("2"),
    fixtureScope("statement_prefix", null, "000001", 0),
  );
  const afterAuthority = fixtureProjectionResult("authority", "000001", 0, d("1"), null);
  const afterCatalog = fixtureProjectionResult(
    "catalog_state",
    "000001",
    0,
    d("3"),
    fixtureScope("statement_prefix", null, "000001", 0),
  );
  const finalCatalog = fixtureProjectionResult(
    "catalog",
    "000001",
    null,
    d("4"),
    fixtureScope("final", "000001", null, null),
  );
  const stateWithoutDigest: JsonObject = {
    schema_bundle_digest: header.schema_bundle_digest!,
    catalog_contract_digest: d("5"),
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
    migration_id: "000001",
    attempt_index: 1,
    statement_index: 0,
    statement_sha256: d("6"),
    previous_attempt_terminal_digest: null,
    previous_intermediate_state_digest: null,
    control_plane_states: {
      tx_status: "T",
      session_user: "cloud_agents_migration_login_fixture",
      current_user: "cloud_agents_migration_owner",
      migration_role: "cloud_agents_migration_owner",
      advisory_lock: {
        domain: "cloud-agents-platform:migrations:v1",
        key_int64_decimal: "-1047838957622507638",
        held: true,
      },
      verified_authority_decision_digest: header.runner_projection_decision_digest!,
      schema_owner: "cloud_agents_migration_owner",
      schema_explicit_acl_digest: d("7"),
      schema_effective_acl_digest: d("8"),
      default_acl_digest: d("9"),
      expected_transition_digest: d("0"),
    },
    authority_before_digest: beforeAuthority.digest!,
    authority_after_digest: afterAuthority.digest!,
    catalog_before_digest: beforeCatalog.digest!,
    catalog_after_digest: afterCatalog.digest!,
  };
  const state: JsonObject = {
    ...stateWithoutDigest,
    intermediate_state_digest: flatDigest(
      "cloud-agents-platform-intermediate-state/v1",
      stateWithoutDigest,
    ),
  };
  const classification: JsonObject = {
    profile: "postgresql-ddl-v1",
    command: "CREATE",
    object_kind: "SCHEMA",
    target_identity: "cloud_agents",
    grantee: null,
    special_case: null,
  };
  const plan: JsonObject = {
    migration_id: "000001",
    statement_index: 0,
    sql_artifact_sha256: d("d"),
    sql_artifact_size_bytes: 4_096,
    start_offset: 0,
    end_offset: 64,
    statement_sha256: state.statement_sha256!,
    classification,
    expected_transition_digest: d("0"),
  };
  const intent: JsonObject = {
    schema_bundle_digest: header.schema_bundle_digest!,
    catalog_contract_digest: state.catalog_contract_digest!,
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
    migration_id: "000001",
    attempt_index: 1,
    statement_index: 0,
    sql_path: "services/control-plane/migrations/000001_expand_migration_kernel.sql",
    sql_artifact_sha256: plan.sql_artifact_sha256!,
    sql_artifact_size_bytes: plan.sql_artifact_size_bytes!,
    start_offset: plan.start_offset!,
    end_offset: plan.end_offset!,
    statement_sha256: plan.statement_sha256!,
    classification,
    previous_attempt_terminal_digest: null,
    previous_intermediate_state_digest: null,
    expected_transition_digest: plan.expected_transition_digest!,
    authority_before_digest: beforeAuthority.digest!,
    catalog_before_digest: beforeCatalog.digest!,
    authority_before_result: beforeAuthority,
    catalog_before_result: beforeCatalog,
  };
  const intermediate: JsonObject = {
    state,
    authority_before_result: beforeAuthority,
    catalog_before_result: beforeCatalog,
    authority_after_result: afterAuthority,
    catalog_after_result: afterCatalog,
    preledger_authority_result: fixtureProjectionResult("authority", "000001", null, d("1"), null),
    preledger_catalog_result: finalCatalog,
  };
  const ledgerRow: JsonObject = {
    migration_id: "000001",
    migration_name: "expand_migration_kernel",
    predecessor_id: null,
    phase: "expand",
    schema_from: "absent",
    schema_to: "000001",
    compatible_binary_min: "0.1.0-alpha.1",
    compatible_binary_max: "0.2.0-0",
    sql_path: intent.sql_path!,
    sql_size_bytes: plan.sql_artifact_size_bytes!,
    sql_sha256: plan.sql_artifact_sha256!,
    bundle_digest: header.schema_bundle_digest!,
    transaction_mode: "serializable_read_write",
    reentrancy: "single_writer",
    rollback_boundary: "transaction",
    requires_live_instance_preflight: true,
    requires_pitr_preflight: true,
  };
  const commitIntent: JsonObject = {
    schema_bundle_digest: header.schema_bundle_digest!,
    catalog_contract_digest: state.catalog_contract_digest!,
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
    migration_id: "000001",
    attempt_index: 1,
    previous_attempt_terminal_digest: null,
    attempt_predecessor_catalog_digest: beforeCatalog.digest!,
    last_intermediate_state_digest: state.intermediate_state_digest!,
    expected_ledger_length: 1,
    expected_ledger_head: "000001",
    ledger_row: ledgerRow,
  };
  const committed = terminalFixtureFrom(state, "committed", null, null, "not_run", null);
  const recordFrames = frameEvidenceRecords([
    header,
    intent,
    intermediate,
    commitIntent,
    committed,
  ]);

  const unresolved = terminalFixtureFrom(
    state,
    "ambiguous_unresolved",
    "MIGRATION_AMBIGUOUS_COMMIT",
    fixtureFailure("MIGRATION_AMBIGUOUS_COMMIT", null, "commit", "transaction", 15, false),
    "unresolved",
    null,
  );
  const resolutions = [
    ["resolved_committed", "exact_committed"],
    ["resolved_pending", "exact_pending"],
    ["resolved_divergent", "divergent"],
  ].map(([outcome, reconcile]) => {
    const body: JsonObject = {
      schema_bundle_digest: header.schema_bundle_digest!,
      catalog_contract_digest: state.catalog_contract_digest!,
      authority_profile_digest: header.authority_profile_digest!,
      authority_binding_digest: header.authority_binding_digest!,
      migration_id: "000001",
      attempt_index: 1,
      unresolved_terminal_digest: unresolved.terminal_digest!,
      outcome: outcome!,
      reconcile_result: reconcile!,
      stable_error_code: "MIGRATION_AMBIGUOUS_COMMIT",
    };
    return { ...body, resolution_digest: ambiguousResolutionDigest(body) };
  });
  const ambiguousFrames = frameEvidenceRecords([
    header,
    intent,
    intermediate,
    commitIntent,
    unresolved,
    resolutions[1]!,
  ]);

  const retryProofs = fixtureRetryProofs(beforeCatalog.digest as string, ledgerRow, d("1"));
  const terminalOutcomes = fixtureTerminalOutcomes(state, retryProofs);
  const retryChains = fixtureRetryChains(
    header,
    intent,
    intermediate,
    commitIntent,
    state,
    retryProofs,
    ledgerRow,
  );
  const headerFrame = frameEvidenceRecords([header])[0]!;
  const reserved = fixtureReserved(header, headerFrame, null);
  const lineageChain = fixtureLineageFrames(lineageInput, lineageDigest, reserved, recordFrames);
  const lineageFrames = lineageChain.frames;
  const supersessionOutcomes = fixtureSupersessionOutcomes(lineageDigest, reserved, continuation);
  const recoveryPolicyChain = fixtureRecoveryPolicyChain(
    lineageDigest,
    object(
      supersessionOutcomes.find((entry) => entry.outcome === "activated_no_migration_progress")!
        .planned_generation_reserved,
      "recovery planned generation",
    ),
  );

  const frameSegments: JsonObject = {
    format_version: "cloud-agents-platform-evidence-frame-segments-fixture/v1",
    frames: recordFrames.map(frameVector),
    inclusive_maxima: {
      evidence_frame_bytes: EVIDENCE_LIMITS.frameBytes,
      evidence_segment_bytes: EVIDENCE_LIMITS.segmentBytes,
      evidence_segment_records: EVIDENCE_LIMITS.segmentRecords,
      evidence_journal_segments: EVIDENCE_LIMITS.journalSegments,
      lineage_frame_bytes: LINEAGE_LIMITS.frameBytes,
      lineage_index_bytes: LINEAGE_LIMITS.indexBytes,
      lineage_index_records: LINEAGE_LIMITS.indexRecords,
      uint16_max: UINT16_MAX,
      uint32_max: UINT32_MAX,
      uint64_json_safe_max: JSON_SAFE_UINT64_MAX,
    },
  };
  const documents = new Map<string, JsonObject>([
    [
      "golden/evidence-record-chain-v1.json",
      {
        format_version: "cloud-agents-platform-evidence-record-chain-fixture/v1",
        validation_context: {
          fixture_only: true,
          max_attempts_by_migration: { "000001": 3 },
          final_statement_index_by_migration: { "000001": 0 },
          final_catalog_digest_by_migration: { "000001": finalCatalog.digest! },
          signed_statement_plans: [plan],
          owned_runtime_receipt_oracle: {
            kind: "runtime",
            sha256: header.outer_artifact_digest!,
            size_bytes: header.outer_artifact_size_bytes!,
          },
          owned_decision_recovery_receipt_oracle: {
            kind: "decision_recovery",
            sha256: header.decision_recovery_artifact_sha256!,
            size_bytes: header.decision_recovery_artifact_size_bytes!,
          },
        },
        records: [header, intent, intermediate, commitIntent, committed],
        frames: recordFrames,
      },
    ],
    [
      "golden/evidence-ambiguous-chain-v1.json",
      {
        format_version: "cloud-agents-platform-evidence-ambiguous-chain-fixture/v1",
        unresolved_terminal: unresolved,
        resolutions,
        frames: ambiguousFrames,
        owned_ambiguous_boundary_oracle: {
          oracle_kind: "owned_ambiguous_commit_boundary/v1",
          migration_id: "000001",
          attempt_index: 1,
          commit_called: true,
          final_intermediate_record_digest: ambiguousFrames[2]!.record_digest!,
          commit_intent_record_digest: ambiguousFrames[3]!.record_digest!,
        },
      },
    ],
    ["golden/evidence-frame-segments-v1.json", frameSegments],
    [
      "golden/terminal-outcomes-v1.json",
      {
        format_version: "cloud-agents-platform-terminal-outcomes-fixture/v1",
        retry_proofs: retryProofs,
        outcomes: terminalOutcomes,
      },
    ],
    [
      "golden/evidence-retry-chains-v1.json",
      {
        format_version: "cloud-agents-platform-evidence-retry-chains-fixture/v1",
        chains: retryChains,
      },
    ],
    [
      "golden/lineage-index-chain-v1.json",
      {
        format_version: "cloud-agents-platform-lineage-index-chain-fixture/v1",
        lineage_input: lineageInput,
        journal_header_frame: headerFrame,
        journal_frames: recordFrames,
        supersession_authority_oracle: lineageChain.supersessionAuthority,
        frames: lineageFrames,
      },
    ],
    [
      "golden/supersession-outcomes-v1.json",
      {
        format_version: "cloud-agents-platform-supersession-outcomes-fixture/v1",
        outcomes: supersessionOutcomes,
      },
    ],
    ["golden/recovery-policy-chain-v1.json", recoveryPolicyChain],
    [
      "golden/decision-recovery-inputs-v1.json",
      {
        format_version: "cloud-agents-platform-decision-recovery-inputs-fixture/v1",
        verifier_owned_content_abi_not_evidence_record: true,
        same_bits_input: {
          format_version: "cloud-agents-platform-decision-recovery-artifact/v1",
          profile_digest: decisionRecoveryArtifactProfileDigest(),
          old_runner_projection_decision_digest: header.runner_projection_decision_digest!,
          repository_identity: lineageInput.repository_identity!,
          release_identity: "fixture_release",
          candidate_subject_base64url_no_padding: "Zml4dHVyZQ",
          candidate_detached_envelope_base64url_no_padding: "c2lnbmF0dXJl",
          projection_subject_inputs: [
            {
              kind: "release",
              subject_digest: rawSha256(Buffer.from("release")),
              subject_base64url_no_padding: "cmVsZWFzZQ",
              detached_envelope_base64url_no_padding: "c2ln",
            },
            {
              kind: "authority_profile",
              subject_digest: rawSha256(Buffer.from("authority-profile")),
              subject_base64url_no_padding: "YXV0aG9yaXR5LXByb2ZpbGU",
              detached_envelope_base64url_no_padding: "c2ln",
            },
            {
              kind: "authority_binding",
              subject_digest: rawSha256(Buffer.from("authority-binding")),
              subject_base64url_no_padding: "YXV0aG9yaXR5LWJpbmRpbmc",
              detached_envelope_base64url_no_padding: "c2ln",
            },
            {
              kind: "catalog",
              subject_digest: rawSha256(Buffer.from("catalog")),
              subject_base64url_no_padding: "Y2F0YWxvZw",
              detached_envelope_base64url_no_padding: "c2ln",
            },
          ],
        },
      },
    ],
    ["negative/evidence-semantic-faults-v1.json", fixtureSemanticFaults()],
    ["negative/evidence-framing-faults-v1.json", fixtureFramingFaults(recordFrames[0]!)],
    ["negative/evidence-limits-faults-v1.json", fixtureLimitFaults()],
  ]);
  return {
    documents,
    duplicateEvidenceFrame: duplicateRaw(recordFrames[0]!, "record_kind"),
    duplicateEvidenceNestedRecord: duplicateRaw(recordFrames[0]!, "schema_bundle_digest"),
    duplicateLineageFrame: duplicateRaw(lineageFrames[0]!, "record_kind"),
  };
}

function fixtureJournalHeader(lineageDigest: string, continuation: JsonObject | null): JsonObject {
  const d = (character: string): string => `sha256:${character.repeat(64)}`;
  const identityInput: JsonObject = {
    release_trust_decision_digest: d("1"),
    runner_projection_decision_digest: d("2"),
    outer_artifact_digest: d("3"),
    outer_artifact_size_bytes: 65_536,
    decision_recovery_artifact_sha256: d("4"),
    decision_recovery_artifact_size_bytes: 4_096,
    schema_bundle_digest: d("5"),
    authority_profile_digest: d("6"),
    authority_binding_digest: d("7"),
  };
  const journalIdentity = journalIdentityDigest(identityInput);
  const quotaInput: JsonObject = {
    limits_profile: "cloud-agents-platform-evidence-journal-limits/v1",
    execution_lineage_digest: lineageDigest,
    journal_identity_digest: journalIdentity,
    runner_projection_decision_digest: identityInput.runner_projection_decision_digest!,
    schema_bundle_digest: identityInput.schema_bundle_digest!,
    reserved_records: 64,
    reserved_bytes: 1_048_576,
    reserved_segments: 1,
    continuation,
  };
  return {
    format_version: "cloud-agents-platform-evidence-journal/v1",
    journal_identity_digest: journalIdentity,
    release_trust_decision_digest: identityInput.release_trust_decision_digest!,
    runner_projection_decision_digest: identityInput.runner_projection_decision_digest!,
    execution_lineage_digest: lineageDigest,
    outer_artifact_digest: identityInput.outer_artifact_digest!,
    outer_artifact_size_bytes: identityInput.outer_artifact_size_bytes!,
    decision_recovery_artifact_sha256: identityInput.decision_recovery_artifact_sha256!,
    decision_recovery_artifact_size_bytes: identityInput.decision_recovery_artifact_size_bytes!,
    manifest_digest: d("8"),
    runner_release_digest: d("9"),
    schema_bundle_digest: identityInput.schema_bundle_digest!,
    authority_profile_digest: identityInput.authority_profile_digest!,
    authority_binding_digest: identityInput.authority_binding_digest!,
    segment_index: 0,
    previous_segment_record_digest: null,
    limits_profile: "cloud-agents-platform-evidence-journal-limits/v1",
    quota_reservation_digest: quotaReservationDigest(quotaInput),
    reserved_records: quotaInput.reserved_records!,
    reserved_bytes: quotaInput.reserved_bytes!,
    reserved_segments: quotaInput.reserved_segments!,
  };
}

function fixtureScope(
  kind: "statement_prefix" | "final",
  schemaHead: string | null,
  migration: string | null,
  statement: number | null,
): JsonObject {
  return {
    scope_kind: kind,
    schema_head: schemaHead,
    migration_id: migration,
    through_statement_index: statement,
    declared_objects: [],
  };
}

function fixtureProjectionResult(
  kind: "authority" | "catalog" | "catalog_state",
  migration: string,
  statement: number | null,
  resultDigest: string,
  scope: JsonObject | null,
): JsonObject {
  const mapping = {
    authority: ["cloud-agents-platform-authority-projection/v1", "postgresql-authority-v1"],
    catalog: ["cloud-agents-platform-catalog-projection/v1", "postgresql-catalog-v1"],
    catalog_state: ["cloud-agents-platform-catalog-state/v1", "postgresql-catalog-v1"],
  } as const;
  return {
    digest: resultDigest,
    metadata: {
      projection_kind: kind,
      digest_domain: mapping[kind][0],
      adapter_profile: mapping[kind][1],
      snapshot: {
        mode: "migration_serializable_read_write",
        ownership: "borrowed_migration",
        postgres_major: 15,
        server_version_num: 150_018,
        database_name: "cloud_agents_fixture",
        authority_phase: "migration_transaction",
        session_user: "cloud_agents_migration_login_fixture",
        current_user: "cloud_agents_migration_owner",
        isolation_level: "serializable",
        access_mode: "read_write",
        deferrable: false,
        tx_status: "T",
        migration_id: migration,
        statement_index: statement,
      },
      verified_subject_digest: `sha256:${"b".repeat(64)}`,
      scope,
      limits_profile: "cloud-agents-platform-projection-limits/v1",
      query_count: 1,
      row_count: 1,
      total_bytes: 128,
      redaction_profile: "cloud-agents-platform-projection-redaction/v1",
    },
  };
}

function terminalFixtureFrom(
  state: JsonObject,
  outcome: string,
  code: string | null,
  failure: JsonObject | null,
  reconcile: string,
  proof: JsonObject | null,
): JsonObject {
  const body: JsonObject = {
    schema_bundle_digest: state.schema_bundle_digest!,
    catalog_contract_digest: state.catalog_contract_digest!,
    authority_profile_digest: state.authority_profile_digest!,
    authority_binding_digest: state.authority_binding_digest!,
    migration_id: state.migration_id!,
    attempt_index: state.attempt_index!,
    previous_attempt_terminal_digest: state.previous_attempt_terminal_digest!,
    last_intermediate_state_digest:
      outcome === "aborted_terminal" || outcome === "aborted_retryable"
        ? null
        : state.intermediate_state_digest!,
    outcome,
    stable_error_code: code,
    failure_evidence: failure,
    retry_proof: proof,
    reconcile_result: reconcile,
  };
  return { ...body, terminal_digest: terminalDigest(body) };
}

function fixtureFailure(
  code: string,
  projectionKind: string | null,
  phase: string,
  path: string,
  major: number | null,
  retryable: boolean,
): JsonObject {
  return { code, projection_kind: projectionKind, phase, path, major, retryable };
}

function fixtureRetryProofs(
  predecessor: string,
  ledgerRow: JsonObject,
  authority: string,
): JsonObject[] {
  const ledger = ledgerPrefixDigest([ledgerRow]);
  return [
    ["projection_transient_exact_predecessor", null],
    ["precommit_rollback_exact_predecessor", null],
    ["precommit_connection_terminated_exact_predecessor", null],
    ["commit_rejected_exact_predecessor", "serialization_failure"],
  ].map(([kind, reason]) => ({
    proof_kind: kind!,
    attempt_predecessor_catalog_digest: predecessor,
    observed_catalog_digest: predecessor,
    ledger_prefix_digest: ledger,
    authority_result_digest: authority,
    commit_rejected_reason: reason!,
  }));
}

function fixtureTerminalOutcomes(state: JsonObject, proofs: JsonObject[]): JsonObject[] {
  const failure = (code: string, retryable: boolean): JsonObject =>
    code.startsWith("MIGRATION_PROJECTION_")
      ? fixtureFailure(code, "catalog", "migration_transaction", "catalog", 15, retryable)
      : fixtureFailure(code, null, "commit", "transaction", 15, retryable);
  return [
    terminalFixtureFrom(state, "committed", null, null, "not_run", null),
    terminalFixtureFrom(
      state,
      "aborted_retryable",
      "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
      failure("MIGRATION_PROJECTION_CATALOG_QUERY_FAILED", true),
      "not_run",
      proofs[0]!,
    ),
    terminalFixtureFrom(
      state,
      "aborted_terminal",
      "MIGRATION_TRANSACTION_BOUNDARY",
      failure("MIGRATION_TRANSACTION_BOUNDARY", false),
      "not_run",
      { ...proofs[3]!, commit_rejected_reason: "other_confirmed_postgres_error" },
    ),
    ...[
      ["ambiguous_reconciled_committed", "exact_committed"],
      ["ambiguous_reconciled_pending", "exact_pending"],
      ["ambiguous_divergent", "divergent"],
      ["ambiguous_unresolved", "unresolved"],
    ].map(([outcome, reconcile]) =>
      terminalFixtureFrom(
        state,
        outcome!,
        "MIGRATION_AMBIGUOUS_COMMIT",
        failure("MIGRATION_AMBIGUOUS_COMMIT", false),
        reconcile!,
        null,
      ),
    ),
  ];
}

function fixtureRetryChains(
  header: JsonObject,
  intent: JsonObject,
  intermediate: JsonObject,
  commitIntent: JsonObject,
  state: JsonObject,
  proofs: JsonObject[],
  ledgerRow: JsonObject,
): JsonObject[] {
  const cases = [
    {
      name: "projection_transient",
      proof: proofs[0]!,
      code: "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
      failure: fixtureFailure(
        "MIGRATION_PROJECTION_CATALOG_QUERY_FAILED",
        "catalog",
        "migration_transaction",
        "catalog",
        15,
        true,
      ),
      outcome: "aborted_retryable",
      oldReceiptKind: "owned_rollback",
      rollbackSucceeded: true,
      commitCalled: false,
      readyForQuery: null,
      includeCommitIntent: false,
    },
    {
      name: "precommit_rollback",
      proof: proofs[1]!,
      code: "MIGRATION_TRANSACTION_BOUNDARY",
      failure: fixtureFailure(
        "MIGRATION_TRANSACTION_BOUNDARY",
        null,
        "migration_transaction",
        "transaction",
        15,
        true,
      ),
      outcome: "aborted_retryable",
      oldReceiptKind: "owned_rollback",
      rollbackSucceeded: true,
      commitCalled: false,
      readyForQuery: null,
      includeCommitIntent: false,
    },
    {
      name: "precommit_connection_terminated",
      proof: proofs[2]!,
      code: "MIGRATION_TRANSACTION_BOUNDARY",
      failure: fixtureFailure(
        "MIGRATION_TRANSACTION_BOUNDARY",
        null,
        "migration_transaction",
        "transaction",
        15,
        true,
      ),
      outcome: "aborted_retryable",
      oldReceiptKind: "owned_precommit_connection_terminated",
      rollbackSucceeded: null,
      commitCalled: false,
      readyForQuery: null,
      includeCommitIntent: false,
    },
    {
      name: "commit_rejected_other_confirmed",
      proof: { ...proofs[3]!, commit_rejected_reason: "other_confirmed_postgres_error" },
      code: "MIGRATION_TRANSACTION_BOUNDARY",
      failure: fixtureFailure(
        "MIGRATION_TRANSACTION_BOUNDARY",
        null,
        "commit",
        "transaction",
        15,
        false,
      ),
      outcome: "aborted_terminal",
      oldReceiptKind: "owned_commit_rejected",
      rollbackSucceeded: null,
      commitCalled: true,
      readyForQuery: true,
      includeCommitIntent: true,
    },
  ] as const;
  return cases.map((entry, index) => {
    const terminal = terminalFixtureFrom(
      state,
      entry.outcome,
      entry.code,
      entry.failure,
      "not_run",
      entry.proof,
    );
    if (entry.includeCommitIntent) {
      terminal.last_intermediate_state_digest = state.intermediate_state_digest!;
      terminal.terminal_digest = terminalDigest(without(terminal, "terminal_digest"));
    }
    const records = entry.includeCommitIntent
      ? [header, intent, intermediate, commitIntent, terminal]
      : [header, intent, intermediate, terminal];
    const frames = frameEvidenceRecords(records);
    const commitFrame = entry.includeCommitIntent ? frames[3]! : null;
    const recovery = {
      ordered_ledger_rows: [ledgerRow],
      ledger_prefix_digest: entry.proof.ledger_prefix_digest!,
      attempt_predecessor_catalog_digest: entry.proof.attempt_predecessor_catalog_digest!,
      observed_catalog_digest: entry.proof.observed_catalog_digest!,
      authority_result_digest: entry.proof.authority_result_digest!,
    };
    return {
      name: entry.name,
      frames,
      owned_retry_receipt_pair_oracle: {
        oracle_kind: "owned_retry_receipt_pair/v1",
        old_receipt_kind: entry.oldReceiptKind,
        proof_kind: entry.proof.proof_kind!,
        migration_id: "000001",
        attempt_index: 1,
        execution_lineage_digest: header.execution_lineage_digest!,
        journal_identity_digest: header.journal_identity_digest!,
        old_connection_lifecycle_id: `old-${index}`,
        new_connection_lifecycle_id: `new-${index}`,
        old_before_new: true,
        commit_called: entry.commitCalled,
        rollback_succeeded: entry.rollbackSucceeded,
        old_handle_irrevocably_closed: true,
        ready_for_query: entry.readyForQuery,
        commit_rejected_reason: entry.proof.commit_rejected_reason!,
        commit_intent_record_digest: commitFrame?.record_digest ?? null,
        recovery_predecessor: recovery,
      },
    };
  });
}

function frameEvidenceRecords(records: readonly JsonObject[]): JsonObject[] {
  let previous: string | null = null;
  return records.map((record, sequence) => {
    const kind =
      sequence === 0 && record.format_version === "cloud-agents-platform-evidence-journal/v1"
        ? "header"
        : Object.hasOwn(record, "authority_before_result") && Object.hasOwn(record, "sql_path")
          ? "statement_intent"
          : Object.hasOwn(record, "state")
            ? "intermediate"
            : Object.hasOwn(record, "ledger_row")
              ? "commit_intent"
              : Object.hasOwn(record, "terminal_digest")
                ? "attempt_terminal"
                : "ambiguous_resolution";
    const body: JsonObject = {
      format_version: "cloud-agents-platform-evidence-journal-frame/v1",
      sequence,
      previous_record_digest: previous,
      record_kind: kind,
      record,
    };
    const frame = { ...body, record_digest: evidenceRecordDigest(body) };
    previous = frame.record_digest;
    return frame;
  });
}

function frameVector(frame: JsonObject): JsonObject {
  const vector = canonicalLengthPrefixed(frame);
  return {
    sequence: frame.sequence!,
    record_digest: frame.record_digest!,
    canonical_size_bytes: vector.canonical_size_bytes,
    length_prefix_hex: vector.length_prefix_hex,
    framed_size_bytes: vector.framed_size_bytes,
    framed_sha256: vector.framed_sha256,
  };
}

function fixtureReserved(
  header: JsonObject,
  headerFrame: JsonObject,
  continuation: JsonObject | null,
): JsonObject {
  const quotaInput: JsonObject = {
    limits_profile: header.limits_profile!,
    execution_lineage_digest: header.execution_lineage_digest!,
    journal_identity_digest: header.journal_identity_digest!,
    runner_projection_decision_digest: header.runner_projection_decision_digest!,
    schema_bundle_digest: header.schema_bundle_digest!,
    reserved_records: header.reserved_records!,
    reserved_bytes: header.reserved_bytes!,
    reserved_segments: header.reserved_segments!,
    continuation,
  };
  const quota = quotaReservationDigest(quotaInput);
  const plannedHeader = { ...header, quota_reservation_digest: quota };
  const plannedFrameBody: JsonObject = {
    format_version: "cloud-agents-platform-evidence-journal-frame/v1",
    sequence: 0,
    previous_record_digest: null,
    record_kind: "header",
    record: plannedHeader,
  };
  return {
    execution_lineage_digest: header.execution_lineage_digest!,
    journal_identity_digest: header.journal_identity_digest!,
    runner_projection_decision_digest: header.runner_projection_decision_digest!,
    schema_bundle_digest: header.schema_bundle_digest!,
    quota_reservation_digest: quota,
    reserved_records: header.reserved_records!,
    reserved_bytes: header.reserved_bytes!,
    reserved_segments: header.reserved_segments!,
    planned_segment0_header: plannedHeader,
    expected_segment0_header_digest: evidenceRecordDigest(plannedFrameBody),
    continuation,
  };
}

function fixtureLineageFrames(
  lineageInput: JsonObject,
  lineageDigest: string,
  reserved: JsonObject,
  journalFrames: readonly JsonObject[],
): { readonly frames: JsonObject[]; readonly supersessionAuthority: JsonObject } {
  const journalSummary = summarizeJournalFrames(journalFrames);
  const records: Array<{ kind: string; record: JsonObject }> = [
    {
      kind: "header",
      record: {
        format_version: "cloud-agents-platform-lineage-index/v1",
        execution_lineage_digest: lineageDigest,
        deployment_id: lineageInput.deployment_id!,
        expected_database_identity: lineageInput.expected_database_identity!,
        repository_identity: lineageInput.repository_identity!,
        limits_profile: "cloud-agents-platform-lineage-index-limits/v1",
      },
    },
    { kind: "generation_reserved", record: reserved },
  ];
  const preliminary = frameLineageRecords(records);
  const activated: JsonObject = {
    execution_lineage_digest: reserved.execution_lineage_digest!,
    journal_identity_digest: reserved.journal_identity_digest!,
    runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
    schema_bundle_digest: reserved.schema_bundle_digest!,
    quota_reservation_digest: reserved.quota_reservation_digest!,
    generation_reserved_record_digest: preliminary[1]!.record_digest!,
    segment0_header_digest: reserved.expected_segment0_header_digest!,
    initial_journal_tail_digest: reserved.expected_segment0_header_digest!,
  };
  const checkpoint: JsonObject = {
    execution_lineage_digest: reserved.execution_lineage_digest!,
    journal_identity_digest: reserved.journal_identity_digest!,
    runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
    schema_bundle_digest: reserved.schema_bundle_digest!,
    journal_next_sequence: journalFrames.length,
    journal_tail_digest: journalFrames.at(-1)!.record_digest!,
    recovery_state: journalSummary.recovery_state!,
    migration_id: journalSummary.migration_id!,
    attempt_index: journalSummary.attempt_index!,
    last_statement_intent_record_digest: journalSummary.last_statement_intent_record_digest!,
    last_intermediate_evidence_record_digest:
      journalSummary.last_intermediate_evidence_record_digest!,
    last_commit_intent_record_digest: journalSummary.last_commit_intent_record_digest!,
    last_terminal_digest: journalSummary.last_terminal_digest!,
    last_resolution_digest: journalSummary.last_resolution_digest!,
    previous_attempt_terminal_digest: journalSummary.previous_attempt_terminal_digest!,
    last_intermediate_state_digest: journalSummary.last_intermediate_state_digest!,
    previous_checkpoint_record_digest: null,
  };
  records.push({ kind: "generation_activated", record: activated });
  records.push({ kind: "generation_checkpoint", record: checkpoint });
  const withCheckpoint = frameLineageRecords(records);
  const supersessionAuthority: JsonObject = {
    domain: "cloud-agents-platform-lineage-supersession-authority/v1",
    historical_recovery_policy_digest: `sha256:${"1".repeat(64)}`,
    recovery_execution_bindings_digest: `sha256:${"2".repeat(64)}`,
    execution_lineage_digest: lineageDigest,
    old_journal_identity_digest: reserved.journal_identity_digest!,
    old_runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
    old_schema_bundle_digest: reserved.schema_bundle_digest!,
    old_checkpoint_record_digest: withCheckpoint[3]!.record_digest!,
    old_activation_record_digest: null,
    old_initial_journal_tail_digest: null,
    old_terminal_digest: checkpoint.last_terminal_digest!,
    old_resolution_digest: null,
    observed_outcome: "exact_committed_bundle_complete",
    successor_runner_projection_decision_digest: `sha256:${"4".repeat(64)}`,
    successor_schema_bundle_digest: `sha256:${"5".repeat(64)}`,
    continuation: null,
  };
  records.push({
    kind: "generation_superseded",
    record: {
      execution_lineage_digest: lineageDigest,
      old_journal_identity_digest: reserved.journal_identity_digest!,
      old_runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
      old_schema_bundle_digest: reserved.schema_bundle_digest!,
      old_checkpoint_record_digest: withCheckpoint[3]!.record_digest!,
      old_activation_record_digest: null,
      old_initial_journal_tail_digest: null,
      lineage_supersession_authority_digest: lineageSupersessionAuthorityDigest(
        without(supersessionAuthority, "domain"),
      ),
      outcome: "exact_committed_bundle_complete",
      planned_generation_reserved: null,
    },
  });
  return { frames: frameLineageRecords(records), supersessionAuthority };
}

function frameLineageRecords(
  records: readonly { kind: string; record: JsonObject }[],
): JsonObject[] {
  let previous: string | null = null;
  return records.map(({ kind, record }, sequence) => {
    const body: JsonObject = {
      format_version: "cloud-agents-platform-lineage-index-frame/v1",
      sequence,
      previous_record_digest: previous,
      record_kind: kind,
      record,
    };
    const frame = { ...body, record_digest: lineageRecordDigest(body) };
    previous = frame.record_digest;
    return frame;
  });
}

function fixtureSupersessionOutcomes(
  lineageDigest: string,
  reserved: JsonObject,
  continuation: JsonObject,
): JsonObject[] {
  const nullPlanned = new Set([
    "exact_committed_bundle_complete",
    "confirmed_abort_terminal",
    "terminal_failure",
    "divergent_terminal",
  ]);
  return RECOVERY_OUTCOMES.map((outcome) => {
    let planned: JsonObject | null = null;
    if (!nullPlanned.has(outcome)) {
      const selectedContinuation =
        outcome === "exact_committed_continue_successor"
          ? {
              ...continuation,
              start_action: "begin_first_attempt_next_entry",
              migration_id: "000002",
              attempt_index: 1,
              previous_attempt_terminal_digest: null,
            }
          : continuation;
      const plannedHeader = fixtureJournalHeader(lineageDigest, selectedContinuation);
      planned = fixtureReserved(
        plannedHeader,
        frameEvidenceRecords([plannedHeader])[0]!,
        selectedContinuation,
      );
    }
    return {
      execution_lineage_digest: lineageDigest,
      old_journal_identity_digest: reserved.journal_identity_digest!,
      old_runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
      old_schema_bundle_digest: reserved.schema_bundle_digest!,
      old_checkpoint_record_digest:
        outcome === "activated_no_migration_progress" ? null : `sha256:${"c".repeat(64)}`,
      old_activation_record_digest:
        outcome === "activated_no_migration_progress" ? `sha256:${"d".repeat(64)}` : null,
      old_initial_journal_tail_digest:
        outcome === "activated_no_migration_progress"
          ? reserved.expected_segment0_header_digest!
          : null,
      lineage_supersession_authority_digest: `sha256:${"e".repeat(64)}`,
      outcome,
      planned_generation_reserved: planned,
    };
  });
}

function fixtureRecoveryPolicyChain(
  lineageDigest: string,
  plannedGeneration: JsonObject,
): JsonObject {
  const d = (character: string): string => `sha256:${character.repeat(64)}`;
  const decisionA = d("a");
  const decisionB = d("b");
  const decisionC = d("c");
  const policySubjectInput: JsonObject = {
    issuer_key_identity_digest: d("1"),
    expires_at: "2030-01-01T00:00:00Z",
    security_epoch: 3,
    minimum_old_security_epoch: 1,
    old_revocation_policy_digest: d("2"),
    old_decision_authorizations: [
      {
        old_runner_projection_decision_digest: decisionA,
        allow_expired: true,
        allow_revoked: false,
        allow_compromised: false,
      },
      {
        old_runner_projection_decision_digest: decisionB,
        allow_expired: true,
        allow_revoked: false,
        allow_compromised: false,
      },
    ],
  };
  const policyDigest = recoveryPolicySubjectDigest(policySubjectInput);
  const plannedHeader = object(plannedGeneration.planned_segment0_header, "planned header");
  const artifacts = [
    {
      decision: decisionA,
      runtime_sha256: d("3"),
      runtime_size_bytes: 65_536,
      recovery_sha256: d("4"),
      recovery_size_bytes: 4_096,
    },
    {
      decision: decisionB,
      runtime_sha256: d("5"),
      runtime_size_bytes: plannedHeader.outer_artifact_size_bytes!,
      recovery_sha256: d("6"),
      recovery_size_bytes: plannedHeader.decision_recovery_artifact_size_bytes!,
    },
    {
      decision: decisionC,
      runtime_sha256: d("7"),
      runtime_size_bytes: plannedHeader.outer_artifact_size_bytes!,
      recovery_sha256: d("8"),
      recovery_size_bytes: plannedHeader.decision_recovery_artifact_size_bytes!,
    },
  ];
  const transition = (oldDecision: string, successor: string, ordinal: number): JsonObject => {
    const historicalInput: JsonObject = {
      recovery_policy_subject_digest: policyDigest,
      execution_lineage_digest: lineageDigest,
      old_journal_identity_digest: d(String(5 + ordinal)),
      old_runner_projection_decision_digest: oldDecision,
      old_schema_bundle_digest: d(String(7 + ordinal)),
      old_decision_recovery_artifact_sha256:
        artifacts.find((artifact) => artifact.decision === oldDecision)?.recovery_sha256 ?? d("9"),
      old_decision_recovery_artifact_size_bytes: 4_096,
      successor_runner_projection_decision_digest: successor,
      successor_schema_bundle_digest: d(String(9 - ordinal)),
      allowed_outcomes: ["activated_no_migration_progress"],
      outcome_constraints: [
        {
          outcome: "activated_no_migration_progress",
          continuation: { kind: "exact_carry_old_generation" },
        },
      ],
    };
    const historicalDigest = historicalRecoveryPolicyDigest(historicalInput);
    const executionInput: JsonObject = {
      historical_recovery_policy_digest: historicalDigest,
      execution_lineage_digest: lineageDigest,
      current_runner_projection_decision_digest: decisionC,
      old_runner_projection_decision_digest: oldDecision,
      old_journal_identity_digest: historicalInput.old_journal_identity_digest!,
      old_schema_bundle_digest: historicalInput.old_schema_bundle_digest!,
      old_decision_recovery_artifact_sha256: historicalInput.old_decision_recovery_artifact_sha256!,
      old_decision_recovery_artifact_size_bytes: 4_096,
      old_journal_replay_tail_digest: d(String(4 + ordinal)),
      old_recovery_state: "brand_new_inherited",
      actions_profile: "cloud-agents-platform-old-attempt-exact-recovery/v1",
    };
    const executionDigest = recoveryExecutionBindingsDigest(executionInput);
    const authorityInput: JsonObject = {
      historical_recovery_policy_digest: historicalDigest,
      recovery_execution_bindings_digest: executionDigest,
      execution_lineage_digest: lineageDigest,
      old_journal_identity_digest: historicalInput.old_journal_identity_digest!,
      old_runner_projection_decision_digest: oldDecision,
      old_schema_bundle_digest: historicalInput.old_schema_bundle_digest!,
      old_checkpoint_record_digest: null,
      old_activation_record_digest: d(String(3 + ordinal)),
      old_initial_journal_tail_digest: executionInput.old_journal_replay_tail_digest!,
      old_terminal_digest: null,
      old_resolution_digest: null,
      observed_outcome: "activated_no_migration_progress",
      successor_runner_projection_decision_digest: successor,
      successor_schema_bundle_digest: historicalInput.successor_schema_bundle_digest!,
      continuation: plannedGeneration.continuation!,
    };
    const successorReceipt = artifacts.find((artifact) => artifact.decision === successor)!;
    const planned = retargetPlannedGeneration(
      plannedGeneration,
      successor,
      String(historicalInput.successor_schema_bundle_digest),
      successorReceipt,
    );
    authorityInput.continuation = planned.continuation!;
    return {
      old_decision: oldDecision,
      successor_decision: successor,
      historical_policy: sameBitsVector(
        historicalInput,
        historicalRecoveryPolicyDigest(historicalInput),
      ),
      recovery_execution_bindings: sameBitsVector(executionInput, executionDigest),
      supersession_authority: sameBitsVector(
        authorityInput,
        lineageSupersessionAuthorityDigest(authorityInput),
      ),
      planned_generation_reserved: planned,
      planned_generation_reserved_digest: migrationDigest(planned),
    };
  };
  return {
    format_version: "cloud-agents-platform-recovery-policy-chain-fixture/v1",
    current_decision: decisionC,
    current_signed_policy_subject: sameBitsVector(policySubjectInput, policyDigest),
    durable_artifact_receipts: artifacts,
    transitions: [transition(decisionA, decisionB, 0), transition(decisionB, decisionC, 1)],
  };
}

function retargetPlannedGeneration(
  source: JsonObject,
  runnerDecision: string,
  schemaBundle: string,
  receipt: JsonObject,
): JsonObject {
  const planned = structuredClone(source);
  const header = object(planned.planned_segment0_header, "planned header");
  header.runner_projection_decision_digest = runnerDecision;
  header.schema_bundle_digest = schemaBundle;
  header.outer_artifact_digest = receipt.runtime_sha256!;
  header.outer_artifact_size_bytes = receipt.runtime_size_bytes!;
  header.decision_recovery_artifact_sha256 = receipt.recovery_sha256!;
  header.decision_recovery_artifact_size_bytes = receipt.recovery_size_bytes!;
  header.journal_identity_digest = journalIdentityDigest({
    release_trust_decision_digest: header.release_trust_decision_digest!,
    runner_projection_decision_digest: runnerDecision,
    outer_artifact_digest: header.outer_artifact_digest!,
    outer_artifact_size_bytes: header.outer_artifact_size_bytes!,
    decision_recovery_artifact_sha256: header.decision_recovery_artifact_sha256!,
    decision_recovery_artifact_size_bytes: header.decision_recovery_artifact_size_bytes!,
    schema_bundle_digest: schemaBundle,
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
  });
  planned.runner_projection_decision_digest = runnerDecision;
  planned.schema_bundle_digest = schemaBundle;
  planned.journal_identity_digest = header.journal_identity_digest!;
  const quota = quotaReservationDigest({
    limits_profile: header.limits_profile!,
    execution_lineage_digest: planned.execution_lineage_digest!,
    journal_identity_digest: planned.journal_identity_digest!,
    runner_projection_decision_digest: runnerDecision,
    schema_bundle_digest: schemaBundle,
    reserved_records: planned.reserved_records!,
    reserved_bytes: planned.reserved_bytes!,
    reserved_segments: planned.reserved_segments!,
    continuation: planned.continuation!,
  });
  planned.quota_reservation_digest = quota;
  header.quota_reservation_digest = quota;
  const frameBody: JsonObject = {
    format_version: "cloud-agents-platform-evidence-journal-frame/v1",
    sequence: 0,
    previous_record_digest: null,
    record_kind: "header",
    record: header,
  };
  planned.expected_segment0_header_digest = evidenceRecordDigest(frameBody);
  validateGenerationReserved(planned);
  return planned;
}

function sameBitsVector(input: JsonObject, digestValue: string): JsonObject {
  return {
    input,
    canonical_rfc8785_utf8: canonicalText(input),
    digest: digestValue,
  };
}

function fixtureSemanticFaults(): JsonObject {
  return {
    format_version: "cloud-agents-platform-evidence-semantic-faults/v1",
    cases: [
      "unknown_member",
      "missing_member",
      "digest_mutation",
      "duplicate_statement_intent",
      "committed_without_commit_intent",
      "stable_failure_tuple",
      "retry_proof_predecessor",
      "duplicate_segment0_header",
      "second_terminal_same_attempt",
      "post_terminal_intermediate_same_attempt",
      "second_lineage_header",
      "activation_schema_mismatch",
      "duplicate_generation_activation",
      "checkpoint_after_superseded",
      "duplicate_generation_superseded",
      "old_handle_still_usable",
      "retry_receipt_cross_lineage",
      "connection_lifecycle_same",
      "connection_lifecycle_reversed",
      "rollback_not_succeeded",
      "other_error_missing_ready_for_query",
      "checkpoint_tail",
      "segment0_wrong_format",
      "forged_commit_rejected_without_commit_intent",
      "historical_artifact_a_missing",
      "historical_artifact_b_missing",
      "current_policy_authorizes_only_a",
      "historical_authority_rebuild_mismatch",
      "historical_authority_policy_digest_mismatch_transition_0",
      "historical_authority_policy_digest_mismatch_transition_1",
      "historical_planned_body_mismatch",
      "historical_execution_current_decision_mismatch",
      "historical_execution_old_journal_mismatch",
      "historical_artifact_receipt_mismatch",
      "historical_policy_disallows_observed_outcome",
      "historical_policy_wrong_constraint",
      "historical_policy_closed_matrix",
      "historical_exact_identity_mismatch",
    ].map((name) => ({
      name,
      body: {
        target_fixture:
          name.startsWith("historical_") || name.startsWith("current_policy_")
            ? "golden/recovery-policy-chain-v1.json"
            : name.includes("lineage") ||
                name.includes("activation") ||
                name.includes("checkpoint") ||
                name.includes("generation_") ||
                name.includes("superseded")
              ? "golden/lineage-index-chain-v1.json"
              : name.includes("receipt") ||
                  name.includes("lifecycle") ||
                  name.includes("rollback") ||
                  name.includes("commit_rejected")
                ? "golden/evidence-retry-chains-v1.json"
                : "golden/evidence-record-chain-v1.json",
        mutation: name,
      },
      expected: "reject",
    })),
  };
}

function fixtureFramingFaults(frame: JsonObject): JsonObject {
  const canonical = canonicalLengthPrefixed(frame);
  const withDeclared = (declared: number): Uint8Array => {
    const bytes = canonical.bytes.slice();
    new DataView(bytes.buffer).setBigUint64(0, BigInt(declared), false);
    return bytes;
  };
  const canonicalPayload = new TextDecoder().decode(canonical.bytes.subarray(8));
  const noncanonicalPayload = new TextEncoder().encode(canonicalPayload.replace(/\}$/u, " }"));
  const noncanonical = new Uint8Array(8 + noncanonicalPayload.length);
  new DataView(noncanonical.buffer).setBigUint64(0, BigInt(noncanonicalPayload.length), false);
  noncanonical.set(noncanonicalPayload, 8);
  const framedRaw = (payload: string): Uint8Array => {
    const encoded = new TextEncoder().encode(payload);
    const bytes = new Uint8Array(8 + encoded.length);
    new DataView(bytes.buffer).setBigUint64(0, BigInt(encoded.length), false);
    bytes.set(encoded, 8);
    return bytes;
  };
  return {
    format_version: "cloud-agents-platform-evidence-framing-faults/v1",
    cases: [
      {
        name: "short_prefix",
        raw_hex: Buffer.from(canonical.bytes.subarray(0, 4)).toString("hex"),
        expected_error: "FRAME_PREFIX",
      },
      {
        name: "length_short",
        raw_hex: Buffer.from(withDeclared(canonical.canonical_size_bytes - 1)).toString("hex"),
        expected_error: "FRAME_PREFIX",
      },
      {
        name: "length_long",
        raw_hex: Buffer.from(withDeclared(canonical.canonical_size_bytes + 1)).toString("hex"),
        expected_error: "FRAME_PREFIX",
      },
      {
        name: "noncanonical_whitespace",
        raw_hex: Buffer.from(noncanonical).toString("hex"),
        expected_error: "FRAME_NON_CANONICAL",
      },
      {
        name: "noncanonical_negative_zero",
        raw_hex: Buffer.from(
          framedRaw(canonicalPayload.replace('"sequence":0', '"sequence":-0')),
        ).toString("hex"),
        expected_error: "INVALID_JSON_NUMBER",
      },
      {
        name: "noncanonical_exponent",
        raw_hex: Buffer.from(
          framedRaw(canonicalPayload.replace('"sequence":0', '"sequence":0e0')),
        ).toString("hex"),
        expected_error: "INVALID_JSON_NUMBER",
      },
      {
        name: "canonical_reference",
        raw_hex: Buffer.from(canonical.bytes).toString("hex"),
        expected_error: null,
        expected_canonical_size_bytes: canonical.canonical_size_bytes,
        expected_length_prefix_hex: canonical.length_prefix_hex,
        expected_framed_size_bytes: canonical.framed_size_bytes,
        expected_framed_sha256: canonical.framed_sha256,
      },
    ],
  };
}

function fixtureLimitFaults(): JsonObject {
  return {
    format_version: "cloud-agents-platform-evidence-limits-faults/v1",
    boundaries: [
      ["stable_failure_major", "stable_failure", UINT16_MAX, UINT16_MAX + 1],
      ["terminal_attempt_index", "attempt_terminal", UINT32_MAX, UINT32_MAX + 1],
      ["journal_outer_artifact_size", "journal_header", JSON_SAFE_UINT64_MAX, "9007199254740992"],
      ["quota_reserved_records", "generation_reserved", 65_536, 65_537],
      ["quota_reserved_bytes", "generation_reserved", 268_435_456, 268_435_457],
      [
        "evidence_intermediate_framed_bytes",
        "evidence_frame_profile",
        EVIDENCE_LIMITS.recordBytes.intermediate,
        EVIDENCE_LIMITS.recordBytes.intermediate + 1,
      ],
      [
        "evidence_segment_bytes",
        "evidence_segment_usage",
        EVIDENCE_LIMITS.segmentBytes,
        EVIDENCE_LIMITS.segmentBytes + 1,
      ],
      [
        "evidence_segment_records",
        "evidence_segment_usage",
        EVIDENCE_LIMITS.segmentRecords,
        EVIDENCE_LIMITS.segmentRecords + 1,
      ],
      ["journal_reserved_segments", "journal_header", 16, 17],
      [
        "lineage_checkpoint_framed_bytes",
        "lineage_frame_profile",
        LINEAGE_LIMITS.recordBytes.generation_checkpoint,
        LINEAGE_LIMITS.recordBytes.generation_checkpoint + 1,
      ],
      [
        "lineage_superseded_framed_bytes",
        "lineage_frame_profile",
        LINEAGE_LIMITS.recordBytes.generation_superseded,
        LINEAGE_LIMITS.recordBytes.generation_superseded + 1,
      ],
      [
        "lineage_index_bytes",
        "lineage_index_usage",
        LINEAGE_LIMITS.indexBytes,
        LINEAGE_LIMITS.indexBytes + 1,
      ],
      [
        "lineage_index_records",
        "lineage_index_usage",
        LINEAGE_LIMITS.indexRecords,
        LINEAGE_LIMITS.indexRecords + 1,
      ],
      ["decision_recovery_identity_bytes", "decision_recovery_inputs", 1_024, 1_025],
      ["decision_recovery_encoded_bytes", "decision_recovery_inputs", 1_048_576, 1_048_577],
      ["decision_recovery_input_count", "decision_recovery_inputs", 4_099, 4_100],
    ].map(([name, validator, exact_max, max_plus_one]) => ({
      name: name!,
      validator: validator!,
      exact_max: exact_max!,
      max_plus_one: max_plus_one!,
    })),
    invalid_cases: [
      { name: "negative_usage", validator: "evidence_segment_usage", value: "-1" },
      { name: "fractional_usage", validator: "lineage_index_usage", value: "1.5" },
      { name: "non_nfc_identity", validator: "decision_recovery_inputs", value: "e\u0301" },
      { name: "invalid_policy_expiry", validator: "recovery_policy", value: "not-rfc3339" },
    ],
  };
}

function duplicateRaw(value: JsonObject, field: string): Uint8Array {
  const canonical = canonicalText(value);
  const marker = `"${field}":`;
  const start = canonical.indexOf(marker);
  if (start < 0) fail("DUPLICATE_FIXTURE", field);
  const valueStart = start + marker.length;
  let valueEnd = valueStart;
  if (canonical[valueStart] === '"') {
    valueEnd = canonical.indexOf('"', valueStart + 1) + 1;
  } else {
    while (
      valueEnd < canonical.length &&
      canonical[valueEnd] !== "," &&
      canonical[valueEnd] !== "}"
    ) {
      valueEnd += 1;
    }
  }
  const member = canonical.slice(start, valueEnd);
  return new TextEncoder().encode(
    `${canonical.slice(0, start)}${member},${canonical.slice(start)}`,
  );
}
