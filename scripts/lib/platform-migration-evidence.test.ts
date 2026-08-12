import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";

import {
  ambiguousResolutionDigest,
  canonicalLengthPrefixed,
  decodeCanonicalLengthPrefixedFrame,
  decisionRecoveryArtifactProfileDigest,
  evidenceRecordDigest,
  executionLineageDigest,
  type EvidenceChainFixtureWitness,
  journalIdentityDigest,
  historicalRecoveryPolicyDigest,
  type LineageChainWitness,
  lineageRecordDigest,
  lineageSupersessionAuthorityDigest,
  quotaReservationDigest,
  recoveryExecutionBindingsDigest,
  recoveryPolicySubjectDigest,
  terminalDigest,
  validateAmbiguousResolutionState,
  validateDecisionRecoveryVerificationInputs,
  validateEvidenceChain,
  validateEvidenceFrame,
  validateEvidenceFramedSizeForKind,
  validateEvidenceSegmentUsage,
  validateGenerationReserved,
  validateGenerationSuperseded,
  validateJournalHeader,
  validateLineageChain,
  validateLineageFramedSizeForKind,
  validateLineageIndexUsage,
  validateLineageIndexFrame,
  validateRecoveryPolicyChainFixture,
} from "./platform-migration-evidence";
import { canonicalizeMigrationJson, parseStrictMigrationJson } from "./platform-migration-json";
import {
  type JsonObject,
  validateAttemptTerminalState,
  validateRetryProofEvidence,
  validateStableFailureEvidence,
} from "./platform-migration-projection";

const root = resolve(import.meta.dirname, "../..");
const fixtureRoot = resolve(root, "services/control-plane/migrations/fixtures/projection");

describe("P1-A2.1a impl-3 persisted evidence contract", () => {
  it("validates all six evidence record branches and flat self digests", () => {
    const recordChain = fixture("golden/evidence-record-chain-v1.json");
    const ambiguous = fixture("golden/evidence-ambiguous-chain-v1.json");
    const frames = [...(recordChain.frames as JsonObject[]), ...(ambiguous.frames as JsonObject[])];
    expect(new Set(frames.map((frame) => frame.record_kind))).toEqual(
      new Set([
        "header",
        "statement_intent",
        "intermediate",
        "commit_intent",
        "attempt_terminal",
        "ambiguous_resolution",
      ]),
    );
    for (const frame of frames) {
      expect(() => validateEvidenceFrame(frame)).not.toThrow();
      const body = structuredClone(frame);
      delete body.record_digest;
      expect(frame.record_digest).toBe(evidenceRecordDigest(body));
    }
  });

  it("validates committed and ambiguous chains only with fixture-only external oracles", () => {
    const recordChain = fixture("golden/evidence-record-chain-v1.json");
    const context = recordChain.validation_context as JsonObject;
    const frames = recordChain.frames as JsonObject[];
    const plan = (context.signed_statement_plans as JsonObject[])[0]!;
    const baseWitness: EvidenceChainFixtureWitness = {
      maxAttemptsByMigration: new Map([["000001", 3]]),
      finalStatementIndexByMigration: new Map([["000001", 0]]),
      finalCatalogDigestByMigration: new Map([
        ["000001", String((context.final_catalog_digest_by_migration as JsonObject)["000001"])],
      ]),
      signedPlans: new Map([["000001:1:0", plan]]),
      runtimeReceipt: context.owned_runtime_receipt_oracle as JsonObject,
      decisionRecoveryReceipt: context.owned_decision_recovery_receipt_oracle as JsonObject,
      ownedRetryReceiptOracles: new Map(),
      ownedAmbiguousBoundaryOracles: new Map(),
    };
    expect(() => validateEvidenceChain(frames, baseWitness)).not.toThrow();

    const ambiguous = fixture("golden/evidence-ambiguous-chain-v1.json");
    const ambiguousFrames = ambiguous.frames as JsonObject[];
    const terminal = ambiguous.unresolved_terminal as JsonObject;
    const ambiguousWitness: EvidenceChainFixtureWitness = {
      ...baseWitness,
      ownedAmbiguousBoundaryOracles: new Map([
        [String(terminal.terminal_digest), ambiguous.owned_ambiguous_boundary_oracle as JsonObject],
      ]),
    };
    expect(() => validateEvidenceChain(ambiguousFrames, ambiguousWitness)).not.toThrow();
    expect(() =>
      validateEvidenceChain(ambiguousFrames, {
        ...ambiguousWitness,
        ownedAmbiguousBoundaryOracles: new Map(),
      }),
    ).toThrow(/AMBIGUOUS_BOUNDARY_ORACLE/u);
  });

  it("covers seven terminal outcomes, four retry proofs, and three resolutions", () => {
    const terminals = fixture("golden/terminal-outcomes-v1.json");
    const outcomes = terminals.outcomes as JsonObject[];
    const proofs = terminals.retry_proofs as JsonObject[];
    expect(new Set(outcomes.map((terminal) => terminal.outcome)).size).toBe(7);
    expect(new Set(proofs.map((proof) => proof.proof_kind)).size).toBe(4);
    for (const terminal of outcomes)
      expect(() => validateAttemptTerminalState(terminal)).not.toThrow();
    for (const proof of proofs) expect(() => validateRetryProofEvidence(proof)).not.toThrow();

    const ambiguous = fixture("golden/evidence-ambiguous-chain-v1.json");
    const resolutions = ambiguous.resolutions as JsonObject[];
    expect(new Set(resolutions.map((resolution) => resolution.outcome)).size).toBe(3);
    for (const resolution of resolutions) {
      expect(() => validateAmbiguousResolutionState(resolution)).not.toThrow();
      const body = structuredClone(resolution);
      delete body.resolution_digest;
      expect(resolution.resolution_digest).toBe(ambiguousResolutionDigest(body));
    }
  });

  it("validates four complete retry chains against owned lifecycle and predecessor oracles", () => {
    const base = fixture("golden/evidence-record-chain-v1.json");
    const context = base.validation_context as JsonObject;
    const plan = (context.signed_statement_plans as JsonObject[])[0]!;
    const document = fixture("golden/evidence-retry-chains-v1.json");
    const chains = document.chains as JsonObject[];
    expect(chains).toHaveLength(4);
    for (const chain of chains) {
      const frames = chain.frames as JsonObject[];
      const terminal = frames.at(-1)!.record as JsonObject;
      const oracle = chain.owned_retry_receipt_pair_oracle as JsonObject;
      const witness: EvidenceChainFixtureWitness = {
        maxAttemptsByMigration: new Map([["000001", 3]]),
        finalStatementIndexByMigration: new Map([["000001", 0]]),
        finalCatalogDigestByMigration: new Map([
          ["000001", String((context.final_catalog_digest_by_migration as JsonObject)["000001"])],
        ]),
        signedPlans: new Map([["000001:1:0", plan]]),
        runtimeReceipt: context.owned_runtime_receipt_oracle as JsonObject,
        decisionRecoveryReceipt: context.owned_decision_recovery_receipt_oracle as JsonObject,
        ownedRetryReceiptOracles: new Map([[String(terminal.terminal_digest), oracle]]),
        ownedAmbiguousBoundaryOracles: new Map(),
      };
      expect(() => validateEvidenceChain(frames, witness), String(chain.name)).not.toThrow();

      for (const mutate of [
        (copy: JsonObject) => {
          copy.new_connection_lifecycle_id = copy.old_connection_lifecycle_id!;
        },
        (copy: JsonObject) => {
          copy.old_before_new = false;
        },
        (copy: JsonObject) => {
          copy.old_handle_irrevocably_closed = false;
        },
        (copy: JsonObject) => {
          (copy.recovery_predecessor as JsonObject).ledger_prefix_digest = digest("f");
        },
      ]) {
        const badOracle = structuredClone(oracle);
        mutate(badOracle);
        expect(() =>
          validateEvidenceChain(frames, {
            ...witness,
            ownedRetryReceiptOracles: new Map([[String(terminal.terminal_digest), badOracle]]),
          }),
        ).toThrow(/RETRY_RECEIPT_ORACLE/u);
      }
      if (oracle.old_receipt_kind === "owned_rollback") {
        const noRollback = structuredClone(oracle);
        noRollback.rollback_succeeded = false;
        expect(() =>
          validateEvidenceChain(frames, {
            ...witness,
            ownedRetryReceiptOracles: new Map([[String(terminal.terminal_digest), noRollback]]),
          }),
        ).toThrow(/RETRY_RECEIPT_ORACLE/u);
      }
      if (oracle.old_receipt_kind === "owned_commit_rejected") {
        const noReady = structuredClone(oracle);
        noReady.ready_for_query = false;
        expect(() =>
          validateEvidenceChain(frames, {
            ...witness,
            ownedRetryReceiptOracles: new Map([[String(terminal.terminal_digest), noReady]]),
          }),
        ).toThrow(/RETRY_RECEIPT_ORACLE/u);
        const forged = structuredClone(frames).filter(
          (frame) => frame.record_kind !== "commit_intent",
        );
        redigestEvidence(forged, 0);
        expect(() => validateEvidenceChain(forged, witness)).toThrow(/RETRY_RECEIPT_ORACLE/u);
      }
    }
  });

  it("validates all five lineage record branches and checkpoint/header/authority linkage", () => {
    const fixtureDocument = fixture("golden/lineage-index-chain-v1.json");
    const frames = fixtureDocument.frames as JsonObject[];
    expect(new Set(frames.map((frame) => frame.record_kind))).toEqual(
      new Set([
        "header",
        "generation_reserved",
        "generation_activated",
        "generation_checkpoint",
        "generation_superseded",
      ]),
    );
    for (const frame of frames) {
      expect(() => validateLineageIndexFrame(frame)).not.toThrow();
      const body = structuredClone(frame);
      delete body.record_digest;
      expect(frame.record_digest).toBe(lineageRecordDigest(body));
    }
    const input = fixtureDocument.lineage_input as JsonObject;
    const indexHeader = frames[0]!.record as JsonObject;
    const reserved = frames[1]!.record as JsonObject;
    const segment0 = fixtureDocument.journal_header_frame as JsonObject;
    const journalFrames = fixtureDocument.journal_frames as JsonObject[];
    const authority = fixtureDocument.supersession_authority_oracle as JsonObject;
    const witness: LineageChainWitness = {
      executionLineageDigest: executionLineageDigest(input),
      deploymentId: String(input.deployment_id),
      databaseName: String((input.expected_database_identity as JsonObject).database_name),
      repositoryIdentity: String(input.repository_identity),
      actualSegment0Frames: new Map([[String(reserved.journal_identity_digest), segment0]]),
      journalFramesByIdentity: new Map([[String(reserved.journal_identity_digest), journalFrames]]),
      supersessionAuthorities: new Map([
        [
          String((frames.at(-1)!.record as JsonObject).lineage_supersession_authority_digest),
          authority,
        ],
      ]),
    };
    expect(indexHeader.execution_lineage_digest).toBe(witness.executionLineageDigest);
    expect(() => validateLineageChain(frames, witness)).not.toThrow();

    const badTail = structuredClone(frames);
    (badTail[3]!.record as JsonObject).journal_tail_digest = digest("f");
    redigestLineage(badTail, 3);
    expect(() => validateLineageChain(badTail, witness)).toThrow(/CHECKPOINT_JOURNAL_TAIL/u);

    const swappedJournal = [
      journalFrames[0]!,
      journalFrames[2]!,
      journalFrames[1]!,
      ...journalFrames.slice(3),
    ];
    expect(() =>
      validateLineageChain(frames, {
        ...witness,
        journalFramesByIdentity: new Map([
          [String(reserved.journal_identity_digest), swappedJournal],
        ]),
      }),
    ).toThrow(/EVIDENCE_FRAME_CHAIN/u);

    const secondHeader = structuredClone(frames);
    secondHeader.splice(1, 0, structuredClone(secondHeader[0]!));
    redigestLineage(secondHeader, 0);
    expect(() => validateLineageChain(secondHeader, witness)).toThrow(/LINEAGE_SECOND_HEADER/u);

    const activationMismatch = structuredClone(frames);
    (activationMismatch[2]!.record as JsonObject).schema_bundle_digest = digest("f");
    redigestLineage(activationMismatch, 2);
    expect(() => validateLineageChain(activationMismatch, witness)).toThrow(
      /ACTIVATION_RESERVED_LINK/u,
    );

    const duplicateActivation = structuredClone(frames);
    duplicateActivation.splice(3, 0, structuredClone(duplicateActivation[2]!));
    redigestLineage(duplicateActivation, 0);
    expect(() => validateLineageChain(duplicateActivation, witness)).toThrow(
      /ACTIVATION_RESERVED_LINK/u,
    );

    const badSegment0 = structuredClone(segment0);
    badSegment0.format_version = "wrong";
    const badSegmentWitness: LineageChainWitness = {
      ...witness,
      actualSegment0Frames: new Map([[String(reserved.journal_identity_digest), badSegment0]]),
    };
    expect(() => validateLineageChain(frames, badSegmentWitness)).toThrow(/UNEXPECTED_VALUE/u);
  });

  it("covers all nine supersession outcomes and null/non-null continuation", () => {
    const document = fixture("golden/supersession-outcomes-v1.json");
    const outcomes = document.outcomes as JsonObject[];
    expect(new Set(outcomes.map((entry) => entry.outcome)).size).toBe(9);
    expect(outcomes.some((entry) => entry.planned_generation_reserved === null)).toBe(true);
    expect(
      outcomes.some(
        (entry) =>
          entry.planned_generation_reserved !== null &&
          (entry.planned_generation_reserved as JsonObject).continuation !== null,
      ),
    ).toBe(true);
    for (const outcome of outcomes)
      expect(() => validateGenerationSuperseded(outcome)).not.toThrow();
  });

  it("keeps decision recovery bytes as verifier-owned content ABI, not EvidenceRecord", () => {
    const document = fixture("golden/decision-recovery-inputs-v1.json");
    expect(document.verifier_owned_content_abi_not_evidence_record).toBe(true);
    const input = document.same_bits_input as JsonObject;
    expect(input.profile_digest).toBe(decisionRecoveryArtifactProfileDigest());
    expect(() => validateDecisionRecoveryVerificationInputs(input)).not.toThrow();
    const badDigest = structuredClone(input);
    (badDigest.projection_subject_inputs as JsonObject[])[0]!.subject_digest = digest("f");
    expect(() => validateDecisionRecoveryVerificationInputs(badDigest)).toThrow(
      /DECISION_RECOVERY_SUBJECT_DIGEST/u,
    );
  });

  it("validates A to B to C historical recovery same-bits and rejects missing authority", () => {
    const chain = fixture("golden/recovery-policy-chain-v1.json");
    expect(() => validateRecoveryPolicyChainFixture(chain)).not.toThrow();

    for (const decisionIndex of [0, 1]) {
      const missingArtifact = structuredClone(chain);
      (missingArtifact.durable_artifact_receipts as JsonObject[]).splice(decisionIndex, 1);
      expect(() => validateRecoveryPolicyChainFixture(missingArtifact)).toThrow(
        /RECOVERY_CHAIN_AUTHORITY/u,
      );
    }
    const policyOnlyA = structuredClone(chain);
    const policyVector = policyOnlyA.current_signed_policy_subject as JsonObject;
    ((policyVector.input as JsonObject).old_decision_authorizations as JsonObject[]).splice(1, 1);
    expect(() => validateRecoveryPolicyChainFixture(policyOnlyA)).toThrow(/SAME_BITS_VECTOR/u);

    const authorityMismatch = structuredClone(chain);
    const firstTransition = (authorityMismatch.transitions as JsonObject[])[0]!;
    (firstTransition.supersession_authority as JsonObject).digest = digest("f");
    expect(() => validateRecoveryPolicyChainFixture(authorityMismatch)).toThrow(
      /SAME_BITS_VECTOR/u,
    );

    const plannedMismatch = structuredClone(chain);
    (plannedMismatch.transitions as JsonObject[])[0]!.planned_generation_reserved_digest =
      digest("f");
    expect(() => validateRecoveryPolicyChainFixture(plannedMismatch)).toThrow(
      /RECOVERY_CHAIN_PLANNED/u,
    );
  });

  it("separates canonical frame construction from raw strict on-disk decoding", () => {
    const frame = (fixture("golden/evidence-record-chain-v1.json").frames as JsonObject[])[0]!;
    const canonical = canonicalLengthPrefixed(frame);
    expect(() => decodeCanonicalLengthPrefixedFrame(canonical.bytes, "evidence")).not.toThrow();

    const wrongLength = canonical.bytes.slice();
    new DataView(wrongLength.buffer).setBigUint64(
      0,
      BigInt(canonical.canonical_size_bytes + 1),
      false,
    );
    expect(() => decodeCanonicalLengthPrefixedFrame(wrongLength, "evidence")).toThrow(
      /FRAME_PREFIX/u,
    );

    const payload = new TextDecoder().decode(canonical.bytes.subarray(8));
    const noncanonicalPayload = new TextEncoder().encode(payload.replace(/\}$/u, " }"));
    const noncanonical = new Uint8Array(8 + noncanonicalPayload.length);
    new DataView(noncanonical.buffer).setBigUint64(0, BigInt(noncanonicalPayload.length), false);
    noncanonical.set(noncanonicalPayload, 8);
    expect(() => decodeCanonicalLengthPrefixedFrame(noncanonical, "evidence")).toThrow(
      /FRAME_NON_CANONICAL/u,
    );
  });

  it("executes every checked-in framing and inclusive-limit vector", () => {
    const framing = fixture("negative/evidence-framing-faults-v1.json");
    for (const testCase of framing.cases as JsonObject[]) {
      const bytes = Buffer.from(String(testCase.raw_hex), "hex");
      if (testCase.expected_error === null) {
        const decoded = decodeCanonicalLengthPrefixedFrame(bytes, "evidence");
        const canonical = canonicalLengthPrefixed(decoded);
        expect(canonical.canonical_size_bytes).toBe(testCase.expected_canonical_size_bytes);
        expect(canonical.length_prefix_hex).toBe(testCase.expected_length_prefix_hex);
        expect(canonical.framed_size_bytes).toBe(testCase.expected_framed_size_bytes);
        expect(`sha256:${createHash("sha256").update(bytes).digest("hex")}`).toBe(
          testCase.expected_framed_sha256,
        );
      } else {
        expect(() => decodeCanonicalLengthPrefixedFrame(bytes, "evidence")).toThrow(
          new RegExp(String(testCase.expected_error), "u"),
        );
      }
    }
    const limits = fixture("negative/evidence-limits-faults-v1.json");
    for (const boundary of limits.boundaries as JsonObject[]) {
      expect(
        () => executeLimitBoundary(boundary, Number(boundary.exact_max)),
        `${String(boundary.name)} exact max`,
      ).not.toThrow();
      expect(
        () => executeLimitBoundary(boundary, Number(boundary.max_plus_one)),
        `${String(boundary.name)} max plus one`,
      ).toThrow();
    }
    for (const invalidCase of limits.invalid_cases as JsonObject[]) {
      expect(() => executeInvalidLimitCase(invalidCase), String(invalidCase.name)).toThrow();
    }
  });

  it("rejects raw duplicate keys at both outer frame boundaries", () => {
    for (const relative of [
      "negative/evidence-frame-duplicate.raw",
      "negative/evidence-nested-record-duplicate.raw",
      "negative/lineage-frame-duplicate.raw",
    ]) {
      expect(() => parseStrictMigrationJson(readFileSync(resolve(fixtureRoot, relative)))).toThrow(
        /DUPLICATE_JSON_KEY/u,
      );
    }
  });

  it("executes semantic mutations for shape, digest, link, tuple, proof, and reservation faults", () => {
    const semanticInventory = fixture("negative/evidence-semantic-faults-v1.json");
    for (const testCase of semanticInventory.cases as JsonObject[]) {
      const body = testCase.body as JsonObject;
      expect(body.mutation).toBe(testCase.name);
      expect(String(body.target_fixture)).toMatch(/^(?:golden)\//u);
      expect(testCase.expected).toBe("reject");
    }
    const base = (fixture("golden/evidence-record-chain-v1.json").frames as JsonObject[])[0]!;
    const unknown = structuredClone(base);
    unknown.untrusted = true;
    expect(() => validateEvidenceFrame(unknown)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);
    const missing = structuredClone(base);
    delete missing.record;
    expect(() => validateEvidenceFrame(missing)).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);
    const digestMutation = structuredClone(base);
    digestMutation.record_digest = digest("f");
    expect(() => validateEvidenceFrame(digestMutation)).toThrow(/EVIDENCE_RECORD_DIGEST/u);

    const terminal = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").outcomes as JsonObject[])[1]!,
    );
    (terminal.failure_evidence as JsonObject).path = "transaction";
    terminal.terminal_digest = terminalDigest(without(terminal, "terminal_digest"));
    expect(() => validateAttemptTerminalState(terminal)).toThrow(/STABLE_FAILURE_TUPLE/u);

    const wrongProof = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").retry_proofs as JsonObject[])[0]!,
    );
    wrongProof.observed_catalog_digest = digest("f");
    expect(() => validateRetryProofEvidence(wrongProof)).toThrow(/RETRY_PROOF_PREDECESSOR/u);

    const superseded = structuredClone(
      (fixture("golden/supersession-outcomes-v1.json").outcomes as JsonObject[])[0]!,
    );
    superseded.planned_generation_reserved = (
      fixture("golden/supersession-outcomes-v1.json").outcomes as JsonObject[]
    )[1]!.planned_generation_reserved!;
    expect(() => validateGenerationSuperseded(superseded)).toThrow(/SUPERSESSION_PLANNED/u);

    const chainDocument = fixture("golden/evidence-record-chain-v1.json");
    const chainFrames = chainDocument.frames as JsonObject[];
    const context = chainDocument.validation_context as JsonObject;
    const chainWitness: EvidenceChainFixtureWitness = {
      maxAttemptsByMigration: new Map([["000001", 3]]),
      finalStatementIndexByMigration: new Map([["000001", 0]]),
      finalCatalogDigestByMigration: new Map([
        ["000001", String((context.final_catalog_digest_by_migration as JsonObject)["000001"])],
      ]),
      signedPlans: new Map([["000001:1:0", (context.signed_statement_plans as JsonObject[])[0]!]]),
      runtimeReceipt: context.owned_runtime_receipt_oracle as JsonObject,
      decisionRecoveryReceipt: context.owned_decision_recovery_receipt_oracle as JsonObject,
      ownedRetryReceiptOracles: new Map(),
      ownedAmbiguousBoundaryOracles: new Map(),
    };
    const duplicateHeader = structuredClone(chainFrames);
    duplicateHeader.splice(1, 0, structuredClone(duplicateHeader[0]!));
    redigestEvidence(duplicateHeader, 0);
    expect(() => validateEvidenceChain(duplicateHeader, chainWitness)).toThrow(
      /EVIDENCE_DUPLICATE_HEADER/u,
    );
    const secondTerminal = structuredClone(chainFrames);
    secondTerminal.push(structuredClone(secondTerminal.at(-1)!));
    redigestEvidence(secondTerminal, secondTerminal.length - 1);
    expect(() => validateEvidenceChain(secondTerminal, chainWitness)).toThrow(
      /ATTEMPT_SECOND_TERMINAL/u,
    );
    const postTerminalIntermediate = structuredClone(chainFrames);
    postTerminalIntermediate.push(structuredClone(postTerminalIntermediate[2]!));
    redigestEvidence(postTerminalIntermediate, postTerminalIntermediate.length - 1);
    expect(() => validateEvidenceChain(postTerminalIntermediate, chainWitness)).toThrow(
      /ATTEMPT_TERMINAL_CLOSED/u,
    );
  });

  it("dispatches every checked-in semantic mutation body to a production validator", () => {
    const inventory = fixture("negative/evidence-semantic-faults-v1.json");
    for (const testCase of inventory.cases as JsonObject[]) {
      expect(() => executeSemanticMutationCase(testCase), String(testCase.name)).toThrow();
    }
  });

  it("enforces uint16/uint32/uint64 safe intersections at exact max and max+1", () => {
    const unsupported = {
      code: "MIGRATION_PROJECTION_UNSUPPORTED_MAJOR",
      projection_kind: "snapshot",
      phase: "connected_session",
      path: "transaction",
      major: 65_535,
      retryable: false,
    } as JsonObject;
    expect(() => validateStableFailureEvidence(unsupported)).not.toThrow();
    unsupported.major = 65_536;
    expect(() => validateStableFailureEvidence(unsupported)).toThrow(/UINT16_RANGE/u);

    const maxAttempt = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").outcomes as JsonObject[])[2]!,
    );
    maxAttempt.attempt_index = 4_294_967_295;
    maxAttempt.previous_attempt_terminal_digest = digest("a");
    maxAttempt.terminal_digest = terminalDigest(without(maxAttempt, "terminal_digest"));
    expect(() => validateAttemptTerminalState(maxAttempt)).not.toThrow();
    maxAttempt.attempt_index = 4_294_967_296;
    maxAttempt.terminal_digest = terminalDigest(without(maxAttempt, "terminal_digest"));
    expect(() => validateAttemptTerminalState(maxAttempt)).toThrow(/UINT32_RANGE/u);

    const header = structuredClone(
      (fixture("golden/evidence-record-chain-v1.json").frames as JsonObject[])[0]!
        .record as JsonObject,
    );
    header.outer_artifact_size_bytes = Number.MAX_SAFE_INTEGER;
    header.journal_identity_digest = journalIdentityDigest(headerIdentityInput(header));
    expect(() => validateJournalHeader(header)).not.toThrow();
    header.outer_artifact_size_bytes = Number.MAX_SAFE_INTEGER + 1;
    expect(() => validateJournalHeader(header)).toThrow(/UINT64_SAFE_RANGE/u);
  });
});

function fixture(relative: string): JsonObject {
  return JSON.parse(readFileSync(resolve(fixtureRoot, relative), "utf8")) as JsonObject;
}

function digest(character: string): string {
  return `sha256:${character.repeat(64)}`;
}

function without(value: JsonObject, key: string): JsonObject {
  const copy = structuredClone(value);
  delete copy[key];
  return copy;
}

function headerIdentityInput(header: JsonObject): JsonObject {
  return {
    release_trust_decision_digest: header.release_trust_decision_digest!,
    runner_projection_decision_digest: header.runner_projection_decision_digest!,
    outer_artifact_digest: header.outer_artifact_digest!,
    outer_artifact_size_bytes: header.outer_artifact_size_bytes!,
    decision_recovery_artifact_sha256: header.decision_recovery_artifact_sha256!,
    decision_recovery_artifact_size_bytes: header.decision_recovery_artifact_size_bytes!,
    schema_bundle_digest: header.schema_bundle_digest!,
    authority_profile_digest: header.authority_profile_digest!,
    authority_binding_digest: header.authority_binding_digest!,
  };
}

function redigestLineage(frames: JsonObject[], start: number): void {
  for (let index = start; index < frames.length; index += 1) {
    frames[index]!.sequence = index;
    frames[index]!.previous_record_digest = index === 0 ? null : frames[index - 1]!.record_digest!;
    frames[index]!.record_digest = lineageRecordDigest(without(frames[index]!, "record_digest"));
  }
}

function redigestEvidence(frames: JsonObject[], start: number): void {
  for (let index = start; index < frames.length; index += 1) {
    frames[index]!.sequence = index;
    frames[index]!.previous_record_digest = index === 0 ? null : frames[index - 1]!.record_digest!;
    frames[index]!.record_digest = evidenceRecordDigest(without(frames[index]!, "record_digest"));
  }
}

function executeLimitBoundary(boundary: JsonObject, value: number): void {
  const name = String(boundary.name);
  if (name === "stable_failure_major") {
    validateStableFailureEvidence({
      code: "MIGRATION_PROJECTION_UNSUPPORTED_MAJOR",
      projection_kind: "snapshot",
      phase: "connected_session",
      path: "transaction",
      major: value,
      retryable: false,
    });
    return;
  }
  if (name === "terminal_attempt_index") {
    const terminal = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").outcomes as JsonObject[])[2]!,
    );
    terminal.attempt_index = value;
    terminal.previous_attempt_terminal_digest = digest("a");
    terminal.terminal_digest = terminalDigest(without(terminal, "terminal_digest"));
    validateAttemptTerminalState(terminal);
    return;
  }
  if (name === "journal_outer_artifact_size") {
    const header = structuredClone(
      (fixture("golden/evidence-record-chain-v1.json").frames as JsonObject[])[0]!
        .record as JsonObject,
    );
    header.outer_artifact_size_bytes = value;
    header.journal_identity_digest = journalIdentityDigest(headerIdentityInput(header));
    validateJournalHeader(header);
    return;
  }
  if (
    name === "quota_reserved_records" ||
    name === "quota_reserved_bytes" ||
    name === "journal_reserved_segments"
  ) {
    const reserved = structuredClone(
      (fixture("golden/lineage-index-chain-v1.json").frames as JsonObject[])[1]!
        .record as JsonObject,
    );
    const field =
      name === "quota_reserved_records"
        ? "reserved_records"
        : name === "quota_reserved_bytes"
          ? "reserved_bytes"
          : "reserved_segments";
    reserved[field] = value;
    const header = reserved.planned_segment0_header as JsonObject;
    header[field] = value;
    const quotaInput: JsonObject = {
      limits_profile: "cloud-agents-platform-evidence-journal-limits/v1",
      execution_lineage_digest: reserved.execution_lineage_digest!,
      journal_identity_digest: reserved.journal_identity_digest!,
      runner_projection_decision_digest: reserved.runner_projection_decision_digest!,
      schema_bundle_digest: reserved.schema_bundle_digest!,
      reserved_records: reserved.reserved_records!,
      reserved_bytes: reserved.reserved_bytes!,
      reserved_segments: reserved.reserved_segments!,
      continuation: reserved.continuation!,
    };
    const quota = quotaReservationDigest(quotaInput);
    reserved.quota_reservation_digest = quota;
    header.quota_reservation_digest = quota;
    reserved.expected_segment0_header_digest = evidenceRecordDigest({
      format_version: "cloud-agents-platform-evidence-journal-frame/v1",
      sequence: 0,
      previous_record_digest: null,
      record_kind: "header",
      record: header,
    });
    validateGenerationReserved(reserved);
    return;
  }
  if (name === "evidence_intermediate_framed_bytes") {
    validateEvidenceFramedSizeForKind("intermediate", value);
    return;
  }
  if (name === "evidence_segment_bytes") {
    validateEvidenceSegmentUsage(1, value);
    return;
  }
  if (name === "evidence_segment_records") {
    validateEvidenceSegmentUsage(value, 1);
    return;
  }
  if (name === "lineage_superseded_framed_bytes") {
    validateLineageFramedSizeForKind("generation_superseded", value);
    return;
  }
  if (name === "lineage_index_bytes") {
    validateLineageIndexUsage(1, value);
    return;
  }
  if (name === "lineage_index_records") {
    validateLineageIndexUsage(value, 1);
    return;
  }
  if (name.startsWith("decision_recovery_")) {
    const input = structuredClone(
      fixture("golden/decision-recovery-inputs-v1.json").same_bits_input as JsonObject,
    );
    if (name === "decision_recovery_identity_bytes") {
      input.repository_identity = "a".repeat(value);
    } else if (name === "decision_recovery_encoded_bytes") {
      input.candidate_subject_base64url_no_padding = "A".repeat(value);
    } else {
      const required = (input.projection_subject_inputs as JsonObject[]).filter(
        (entry) => entry.kind !== "catalog",
      );
      const catalogs = Array.from({ length: value - required.length }, (_, index) => {
        const subject = Buffer.from(`catalog-limit-${String(index).padStart(4, "0")}`);
        return {
          kind: "catalog",
          subject_digest: `sha256:${createHash("sha256").update(subject).digest("hex")}`,
          subject_base64url_no_padding: subject.toString("base64url"),
          detached_envelope_base64url_no_padding: "c2ln",
        } as JsonObject;
      }).sort((left, right) =>
        String(left.subject_digest).localeCompare(String(right.subject_digest)),
      );
      input.projection_subject_inputs = [...required, ...catalogs];
    }
    validateDecisionRecoveryVerificationInputs(input);
    return;
  }
  throw new Error(`unrouted limit boundary: ${name}`);
}

function executeInvalidLimitCase(testCase: JsonObject): void {
  const name = String(testCase.name);
  if (name === "negative_usage") {
    validateEvidenceSegmentUsage(Number(testCase.value), 1);
    return;
  }
  if (name === "fractional_usage") {
    validateLineageIndexUsage(Number(testCase.value), 1);
    return;
  }
  if (name === "non_nfc_identity") {
    const input = structuredClone(
      fixture("golden/decision-recovery-inputs-v1.json").same_bits_input as JsonObject,
    );
    input.repository_identity = testCase.value!;
    validateDecisionRecoveryVerificationInputs(input);
    return;
  }
  if (name === "invalid_policy_expiry") {
    const fixtureDocument = fixture("golden/recovery-policy-chain-v1.json");
    const input = structuredClone(
      (fixtureDocument.current_signed_policy_subject as JsonObject).input as JsonObject,
    );
    input.expires_at = testCase.value!;
    recoveryPolicySubjectDigest(input);
    return;
  }
  throw new Error(`unrouted invalid limit case: ${name}`);
}

function executeSemanticMutationCase(testCase: JsonObject): void {
  const body = testCase.body as JsonObject;
  const mutation = String(body.mutation);
  const target = fixture(String(body.target_fixture));
  const recordDocument = fixture("golden/evidence-record-chain-v1.json");
  const recordFrames = recordDocument.frames as JsonObject[];
  const evidenceWitness = fixtureEvidenceWitness(recordDocument);
  if (
    mutation === "unknown_member" ||
    mutation === "missing_member" ||
    mutation === "digest_mutation"
  ) {
    const frame = structuredClone((target.frames as JsonObject[])[0]!);
    if (mutation === "unknown_member") frame.unknown = true;
    else if (mutation === "missing_member") delete frame.record;
    else frame.record_digest = digest("f");
    validateEvidenceFrame(frame);
    return;
  }
  if (mutation === "stable_failure_tuple") {
    const terminal = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").outcomes as JsonObject[])[1]!,
    );
    (terminal.failure_evidence as JsonObject).path = "transaction";
    terminal.terminal_digest = terminalDigest(without(terminal, "terminal_digest"));
    validateAttemptTerminalState(terminal);
    return;
  }
  if (mutation === "retry_proof_predecessor") {
    const proof = structuredClone(
      (fixture("golden/terminal-outcomes-v1.json").retry_proofs as JsonObject[])[0]!,
    );
    proof.observed_catalog_digest = digest("f");
    validateRetryProofEvidence(proof);
    return;
  }
  if (
    mutation === "duplicate_segment0_header" ||
    mutation === "second_terminal_same_attempt" ||
    mutation === "post_terminal_intermediate_same_attempt" ||
    mutation === "duplicate_statement_intent" ||
    mutation === "committed_without_commit_intent"
  ) {
    const frames = structuredClone(recordFrames);
    if (mutation === "duplicate_segment0_header") frames.splice(1, 0, structuredClone(frames[0]!));
    else if (mutation === "second_terminal_same_attempt")
      frames.push(structuredClone(frames.at(-1)!));
    else if (mutation === "post_terminal_intermediate_same_attempt") {
      frames.push(structuredClone(frames[2]!));
    } else if (mutation === "duplicate_statement_intent") {
      frames.splice(2, 0, structuredClone(frames[1]!));
    } else {
      frames.splice(
        frames.findIndex((frame) => frame.record_kind === "commit_intent"),
        1,
      );
    }
    redigestEvidence(frames, 0);
    validateEvidenceChain(frames, evidenceWitness);
    return;
  }
  if (
    mutation === "second_lineage_header" ||
    mutation === "activation_schema_mismatch" ||
    mutation === "duplicate_generation_activation" ||
    mutation === "checkpoint_after_superseded" ||
    mutation === "duplicate_generation_superseded" ||
    mutation === "checkpoint_tail" ||
    mutation === "segment0_wrong_format"
  ) {
    const lineage = fixture("golden/lineage-index-chain-v1.json");
    const frames = structuredClone(lineage.frames as JsonObject[]);
    let witness = fixtureLineageWitness(lineage);
    if (mutation === "second_lineage_header") frames.splice(1, 0, structuredClone(frames[0]!));
    else if (mutation === "activation_schema_mismatch") {
      (frames[2]!.record as JsonObject).schema_bundle_digest = digest("f");
    } else if (mutation === "duplicate_generation_activation") {
      frames.splice(3, 0, structuredClone(frames[2]!));
    } else if (mutation === "checkpoint_after_superseded") {
      frames.push(structuredClone(frames[3]!));
    } else if (mutation === "duplicate_generation_superseded") {
      frames.push(structuredClone(frames.at(-1)!));
    } else if (mutation === "checkpoint_tail") {
      (frames[3]!.record as JsonObject).journal_tail_digest = digest("f");
    } else {
      const reserved = frames[1]!.record as JsonObject;
      const actual = structuredClone(lineage.journal_header_frame as JsonObject);
      actual.format_version = "wrong";
      witness = {
        ...witness,
        actualSegment0Frames: new Map([[String(reserved.journal_identity_digest), actual]]),
      };
    }
    redigestLineage(frames, 0);
    validateLineageChain(frames, witness);
    return;
  }
  if (
    mutation === "old_handle_still_usable" ||
    mutation === "retry_receipt_cross_lineage" ||
    mutation === "connection_lifecycle_same" ||
    mutation === "connection_lifecycle_reversed" ||
    mutation === "rollback_not_succeeded" ||
    mutation === "other_error_missing_ready_for_query" ||
    mutation === "forged_commit_rejected_without_commit_intent"
  ) {
    const retryDocument = fixture("golden/evidence-retry-chains-v1.json");
    const chains = retryDocument.chains as JsonObject[];
    const useCommit =
      mutation === "other_error_missing_ready_for_query" ||
      mutation === "forged_commit_rejected_without_commit_intent";
    const chain = structuredClone(
      useCommit
        ? chains.find((entry) => entry.name === "commit_rejected_other_confirmed")!
        : mutation === "rollback_not_succeeded"
          ? chains.find((entry) => entry.name === "precommit_rollback")!
          : chains[0]!,
    );
    const frames = chain.frames as JsonObject[];
    const terminal = frames.at(-1)!.record as JsonObject;
    const oracle = chain.owned_retry_receipt_pair_oracle as JsonObject;
    if (mutation === "old_handle_still_usable") oracle.old_handle_irrevocably_closed = false;
    else if (mutation === "retry_receipt_cross_lineage") {
      oracle.execution_lineage_digest = digest("f");
    } else if (mutation === "connection_lifecycle_same") {
      oracle.new_connection_lifecycle_id = oracle.old_connection_lifecycle_id!;
    } else if (mutation === "connection_lifecycle_reversed") oracle.old_before_new = false;
    else if (mutation === "rollback_not_succeeded") oracle.rollback_succeeded = false;
    else if (mutation === "other_error_missing_ready_for_query") oracle.ready_for_query = false;
    else {
      const withoutCommit = frames.filter((frame) => frame.record_kind !== "commit_intent");
      redigestEvidence(withoutCommit, 0);
      chain.frames = withoutCommit;
    }
    const witness = fixtureEvidenceWitness(recordDocument);
    witness.ownedRetryReceiptOracles = new Map([[String(terminal.terminal_digest), oracle]]);
    validateEvidenceChain(chain.frames as JsonObject[], witness);
    return;
  }
  if (
    mutation === "historical_artifact_a_missing" ||
    mutation === "historical_artifact_b_missing" ||
    mutation === "current_policy_authorizes_only_a" ||
    mutation === "historical_authority_rebuild_mismatch" ||
    mutation === "historical_authority_policy_digest_mismatch_transition_0" ||
    mutation === "historical_authority_policy_digest_mismatch_transition_1" ||
    mutation === "historical_planned_body_mismatch" ||
    mutation === "historical_execution_current_decision_mismatch" ||
    mutation === "historical_execution_old_journal_mismatch" ||
    mutation === "historical_artifact_receipt_mismatch" ||
    mutation === "historical_policy_disallows_observed_outcome" ||
    mutation === "historical_policy_wrong_constraint" ||
    mutation === "historical_policy_closed_matrix" ||
    mutation === "historical_exact_identity_mismatch"
  ) {
    const chain = structuredClone(target);
    if (mutation === "historical_artifact_a_missing") {
      (chain.durable_artifact_receipts as JsonObject[]).splice(0, 1);
    } else if (mutation === "historical_artifact_b_missing") {
      (chain.durable_artifact_receipts as JsonObject[]).splice(1, 1);
    } else if (mutation === "current_policy_authorizes_only_a") {
      const vector = chain.current_signed_policy_subject as JsonObject;
      ((vector.input as JsonObject).old_decision_authorizations as JsonObject[]).splice(1, 1);
    } else if (mutation === "historical_authority_rebuild_mismatch") {
      ((chain.transitions as JsonObject[])[0]!.supersession_authority as JsonObject).digest =
        digest("f");
    } else if (mutation.startsWith("historical_authority_policy_digest_mismatch_transition_")) {
      const index = Number(mutation.at(-1));
      const authority = (chain.transitions as JsonObject[])[index]!
        .supersession_authority as JsonObject;
      (authority.input as JsonObject).historical_recovery_policy_digest = digest("f");
      rewriteSameBitsVector(authority, lineageSupersessionAuthorityDigest);
    } else if (mutation === "historical_planned_body_mismatch") {
      (chain.transitions as JsonObject[])[0]!.planned_generation_reserved_digest = digest("f");
    } else if (mutation === "historical_execution_current_decision_mismatch") {
      const transition = (chain.transitions as JsonObject[])[0]!;
      const vector = transition.recovery_execution_bindings as JsonObject;
      (vector.input as JsonObject).current_runner_projection_decision_digest = digest("f");
      rewriteSameBitsVector(vector, recoveryExecutionBindingsDigest);
    } else if (mutation === "historical_execution_old_journal_mismatch") {
      const transition = (chain.transitions as JsonObject[])[0]!;
      const execution = transition.recovery_execution_bindings as JsonObject;
      (execution.input as JsonObject).old_journal_identity_digest = digest("f");
      rewriteSameBitsVector(execution, recoveryExecutionBindingsDigest);
      const authority = transition.supersession_authority as JsonObject;
      (authority.input as JsonObject).old_journal_identity_digest = digest("f");
      (authority.input as JsonObject).recovery_execution_bindings_digest = execution.digest!;
      rewriteSameBitsVector(authority, lineageSupersessionAuthorityDigest);
    } else if (mutation === "historical_artifact_receipt_mismatch") {
      (chain.durable_artifact_receipts as JsonObject[])[0]!.recovery_sha256 = digest("f");
    } else if (mutation === "historical_policy_disallows_observed_outcome") {
      const vector = (chain.transitions as JsonObject[])[0]!.supersession_authority as JsonObject;
      (vector.input as JsonObject).observed_outcome = "terminal_failure";
      (vector.input as JsonObject).continuation = null;
      rewriteSameBitsVector(vector, lineageSupersessionAuthorityDigest);
    } else if (mutation === "historical_policy_wrong_constraint") {
      const transition = (chain.transitions as JsonObject[])[0]!;
      const historical = transition.historical_policy as JsonObject;
      const constraint = ((historical.input as JsonObject).outcome_constraints as JsonObject[])[0]!;
      constraint.continuation = { kind: "must_be_null" };
      rewriteSameBitsVector(historical, historicalRecoveryPolicyDigest);
      const execution = transition.recovery_execution_bindings as JsonObject;
      (execution.input as JsonObject).historical_recovery_policy_digest = historical.digest!;
      rewriteSameBitsVector(execution, recoveryExecutionBindingsDigest);
      const authority = transition.supersession_authority as JsonObject;
      (authority.input as JsonObject).historical_recovery_policy_digest = historical.digest!;
      (authority.input as JsonObject).recovery_execution_bindings_digest = execution.digest!;
      rewriteSameBitsVector(authority, lineageSupersessionAuthorityDigest);
    } else if (mutation === "historical_policy_closed_matrix") {
      const transition = (chain.transitions as JsonObject[])[0]!;
      const historical = transition.historical_policy as JsonObject;
      const historicalInput = historical.input as JsonObject;
      historicalInput.allowed_outcomes = [
        "activated_no_migration_progress",
        "confirmed_abort_terminal",
      ];
      historicalInput.outcome_constraints = [
        (historicalInput.outcome_constraints as JsonObject[])[0]!,
        {
          outcome: "confirmed_abort_terminal",
          continuation: {
            kind: "exact_identity",
            identity: {
              start_action: "begin_next_attempt",
              migration_id: "000001",
              attempt_index: 2,
              previous_attempt: "owned_old_terminal",
            },
          },
        },
      ];
      rewriteSameBitsVector(historical, historicalRecoveryPolicyDigest);
    } else {
      const transition = (chain.transitions as JsonObject[])[0]!;
      const historical = transition.historical_policy as JsonObject;
      const historicalInput = historical.input as JsonObject;
      historicalInput.allowed_outcomes = ["precommit_aborted_retryable"];
      historicalInput.outcome_constraints = [
        {
          outcome: "precommit_aborted_retryable",
          continuation: {
            kind: "exact_identity",
            identity: {
              start_action: "begin_next_attempt",
              migration_id: "000002",
              attempt_index: 2,
              previous_attempt: "owned_old_terminal",
            },
          },
        },
      ];
      rewriteSameBitsVector(historical, historicalRecoveryPolicyDigest);
      const execution = transition.recovery_execution_bindings as JsonObject;
      (execution.input as JsonObject).historical_recovery_policy_digest = historical.digest!;
      rewriteSameBitsVector(execution, recoveryExecutionBindingsDigest);
      const authority = transition.supersession_authority as JsonObject;
      (authority.input as JsonObject).historical_recovery_policy_digest = historical.digest!;
      (authority.input as JsonObject).recovery_execution_bindings_digest = execution.digest!;
      (authority.input as JsonObject).observed_outcome = "precommit_aborted_retryable";
      rewriteSameBitsVector(authority, lineageSupersessionAuthorityDigest);
    }
    validateRecoveryPolicyChainFixture(chain);
    return;
  }
  throw new Error(`unrouted semantic mutation: ${mutation}`);
}

function fixtureEvidenceWitness(document: JsonObject): EvidenceChainFixtureWitness {
  const context = document.validation_context as JsonObject;
  return {
    maxAttemptsByMigration: new Map([["000001", 3]]),
    finalStatementIndexByMigration: new Map([["000001", 0]]),
    finalCatalogDigestByMigration: new Map([
      ["000001", String((context.final_catalog_digest_by_migration as JsonObject)["000001"])],
    ]),
    signedPlans: new Map([["000001:1:0", (context.signed_statement_plans as JsonObject[])[0]!]]),
    runtimeReceipt: context.owned_runtime_receipt_oracle as JsonObject,
    decisionRecoveryReceipt: context.owned_decision_recovery_receipt_oracle as JsonObject,
    ownedRetryReceiptOracles: new Map(),
    ownedAmbiguousBoundaryOracles: new Map(),
  };
}

function fixtureLineageWitness(document: JsonObject): LineageChainWitness {
  const frames = document.frames as JsonObject[];
  const input = document.lineage_input as JsonObject;
  const reserved = frames[1]!.record as JsonObject;
  const authority = document.supersession_authority_oracle as JsonObject;
  return {
    executionLineageDigest: executionLineageDigest(input),
    deploymentId: String(input.deployment_id),
    databaseName: String((input.expected_database_identity as JsonObject).database_name),
    repositoryIdentity: String(input.repository_identity),
    actualSegment0Frames: new Map([
      [String(reserved.journal_identity_digest), document.journal_header_frame as JsonObject],
    ]),
    journalFramesByIdentity: new Map([
      [String(reserved.journal_identity_digest), document.journal_frames as JsonObject[]],
    ]),
    supersessionAuthorities: new Map([
      [
        String((frames.at(-1)!.record as JsonObject).lineage_supersession_authority_digest),
        authority,
      ],
    ]),
  };
}

function rewriteSameBitsVector(
  vector: JsonObject,
  digestBuilder: (input: JsonObject) => string,
): void {
  const input = vector.input as JsonObject;
  vector.canonical_rfc8785_utf8 = new TextDecoder().decode(canonicalizeMigrationJson(input));
  vector.digest = digestBuilder(input);
}
