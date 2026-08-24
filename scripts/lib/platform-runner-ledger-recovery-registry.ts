import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

import {
  canonicalizeJson,
  type JsonRecord,
  type SemanticErrorCode,
  type SemanticResult,
} from "./platform-json-semantics";
import {
  assertRunnerLedgerPreflightRegistryCurrent,
  buildRunnerLedgerPreflightRegistry,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
} from "./platform-runner-ledger-preflight-registry";
import {
  assertRunnerLedgerConsumerRegistryCurrent,
  buildRunnerLedgerConsumerRegistry,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
} from "./platform-runner-ledger-consumer-registry";
import {
  assertRunnerLedgerEntryAdmissionRegistryCurrent,
  buildRunnerLedgerEntryAdmissionRegistry,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
} from "./platform-runner-ledger-entry-admission-registry";
import {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent,
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent,
  buildRunnerLedgerEntryExecutionAdmissionRegistry,
  buildRunnerLedgerEntrySuccessWriterRegistry,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATES,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TERMINAL_STATES,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TRANSITIONS,
} from "./platform-runner-ledger-entry-writer-registry";

export const RUNNER_LEDGER_RECOVERY_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-recovery-registry-suite-source-v1.json";
export const RUNNER_LEDGER_RECOVERY_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-recovery-registry-suite-source-v1.schema.json";
export const RUNNER_LEDGER_RECOVERY_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-recovery-registry-v1.schema.json";

const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-recovery-registry-suite-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-recovery-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-runner-ledger-recovery-registry-suite-source/v1";
const OUTPUT_FORMAT = "cloud-agents-runner-ledger-recovery-registry/v1";
const SUITE_ID = "cloud-agents/platform/runner-ledger-recovery";

export const RUNNER_LEDGER_RECOVERY_FAMILIES = [
  "recovery_admission",
  "abort_terminal_writer",
  "commit_observation_writer",
  "ambiguous_resolution_writer",
  "retry_handoff",
  "recovery_execution_admission",
  "recovery_success_writer",
  "return_failure",
] as const;

export type RunnerLedgerRecoveryFamily = (typeof RUNNER_LEDGER_RECOVERY_FAMILIES)[number];

export type RunnerLedgerRecoveryPairBinding = {
  readonly disposition: string;
  readonly state: string;
  readonly action: string;
  readonly consumerAction: "entry_not_implemented" | "recovery_not_implemented";
  readonly profileAction: string;
};

type StateMachine = {
  readonly id: string;
  readonly initialState: "unclassified";
  readonly states: ReadonlyArray<string>;
  readonly terminalStates: ReadonlyArray<string>;
  readonly transitions: ReadonlyArray<{
    readonly from: string;
    readonly event: string;
    readonly to: string;
  }>;
};

type SourceProfile = JsonRecord & {
  readonly family: RunnerLedgerRecoveryFamily;
  readonly registryId: string;
  readonly profileId: string;
  readonly action: string;
  readonly predecessorProfileId: string;
  readonly permitFromProfileId: string | null;
  readonly pairBindings: ReadonlyArray<RunnerLedgerRecoveryPairBinding>;
};

type SourceSuite = JsonRecord & {
  readonly formatVersion: typeof SOURCE_FORMAT;
  readonly suiteId: typeof SUITE_ID;
  readonly profiles: ReadonlyArray<SourceProfile>;
};

type BoundRegistry = JsonRecord & {
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly transitionMatrix?: Record<string, ReadonlyArray<Record<string, string>>>;
    };
  };
};

type ProfileDefinition = {
  readonly family: RunnerLedgerRecoveryFamily;
  readonly slug: string;
  readonly action: string;
  readonly predecessorProfileId: string;
  readonly permitFromProfileId: string | null;
  readonly pairBindings: ReadonlyArray<RunnerLedgerRecoveryPairBinding>;
  readonly stateMachine: StateMachine;
};

const RECOVERY_PAIR_BINDINGS: ReadonlyArray<RunnerLedgerRecoveryPairBinding> = [
  pair(
    "empty_brand_new",
    "brand_new_inherited",
    "begin_next_attempt",
    "entry_not_implemented",
    "prepare_recovery_execution",
  ),
  pair(
    "partial_retry_or_recovery",
    "brand_new_inherited",
    "begin_first_attempt",
    "recovery_not_implemented",
    "prepare_recovery_execution",
  ),
  pair(
    "partial_retry_or_recovery",
    "brand_new_inherited",
    "begin_next_attempt",
    "recovery_not_implemented",
    "prepare_recovery_execution",
  ),
  pair(
    "partial_retry_or_recovery",
    "dangling_statement_intent",
    "append_aborted_retryable",
    "recovery_not_implemented",
    "append_abort_terminal",
  ),
  pair(
    "partial_retry_or_recovery",
    "dangling_statement_intent",
    "append_aborted_terminal",
    "recovery_not_implemented",
    "append_abort_terminal",
  ),
  pair(
    "partial_retry_or_recovery",
    "dangling_intermediate",
    "append_aborted_retryable",
    "recovery_not_implemented",
    "append_abort_terminal",
  ),
  pair(
    "partial_retry_or_recovery",
    "dangling_intermediate",
    "append_aborted_terminal",
    "recovery_not_implemented",
    "append_abort_terminal",
  ),
  pair(
    "partial_retry_or_recovery",
    "dangling_commit_intent",
    "reconcile_commit",
    "recovery_not_implemented",
    "append_commit_observation_terminal",
  ),
  pair(
    "partial_retry_or_recovery",
    "ambiguous_unresolved",
    "reconcile_commit",
    "recovery_not_implemented",
    "append_ambiguous_resolution",
  ),
  pair(
    "partial_retry_or_recovery",
    "terminal",
    "begin_next_attempt",
    "recovery_not_implemented",
    "prepare_retry_handoff",
  ),
  pair(
    "partial_retry_or_recovery",
    "terminal",
    "return_failure",
    "recovery_not_implemented",
    "return_typed_failure",
  ),
  pair(
    "partial_retry_or_recovery",
    "divergent",
    "return_failure",
    "recovery_not_implemented",
    "return_typed_failure",
  ),
] as const;

export const RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS = RECOVERY_PAIR_BINDINGS;

const COMMON_PROFILE_ID = "runner-ledger-recovery-admission/v1";
const EXECUTION_PROFILE_ID = "runner-ledger-recovery-execution-admission/v1";

const PROFILE_DEFINITIONS: ReadonlyArray<ProfileDefinition> = [
  definition(
    "recovery_admission",
    "runner-ledger-recovery-admission",
    "prepare_recovery_admission",
    "runner-ledger-consumer/v1",
    null,
    RECOVERY_PAIR_BINDINGS,
    machine(
      "runner-ledger-recovery-admission/v1",
      [
        "admission_closed",
        "action_permit_ready",
        "recovery_required_closed",
        "replay_revalidating",
        "unclassified",
        "unknown_rejected",
      ],
      ["admission_closed", "recovery_required_closed", "unknown_rejected"],
      [
        transition("unclassified", "select_supported_pair", "replay_revalidating"),
        transition("unclassified", "select_unknown", "unknown_rejected"),
        transition("replay_revalidating", "revalidate_exact_boundary", "action_permit_ready"),
        transition("replay_revalidating", "revalidate_failed", "recovery_required_closed"),
        transition("action_permit_ready", "close_without_mutation", "admission_closed"),
      ],
    ),
  ),
  definition(
    "abort_terminal_writer",
    "runner-ledger-abort-terminal-writer",
    "append_abort_terminal",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("append_abort_terminal"),
    appendMachine("runner-ledger-abort-terminal-writer/v1", "receipt", "terminal"),
  ),
  definition(
    "commit_observation_writer",
    "runner-ledger-commit-observation-writer",
    "append_commit_observation_terminal",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("append_commit_observation_terminal"),
    appendMachine("runner-ledger-commit-observation-writer/v1", "observation", "terminal"),
  ),
  definition(
    "ambiguous_resolution_writer",
    "runner-ledger-ambiguous-resolution-writer",
    "append_ambiguous_resolution",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("append_ambiguous_resolution"),
    appendMachine("runner-ledger-ambiguous-resolution-writer/v1", "observation", "resolution"),
  ),
  definition(
    "retry_handoff",
    "runner-ledger-retry-handoff",
    "prepare_retry_handoff",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("prepare_retry_handoff"),
    machine(
      "runner-ledger-retry-handoff/v1",
      [
        "handoff_ready",
        "handoff_revalidating",
        "recovery_required_closed",
        "successor_generation_durable",
        "unclassified",
        "unknown_rejected",
      ],
      ["recovery_required_closed", "successor_generation_durable", "unknown_rejected"],
      [
        transition("unclassified", "consume_retry_handoff_permit", "handoff_revalidating"),
        transition("unclassified", "select_unknown", "unknown_rejected"),
        transition("handoff_revalidating", "revalidate_exact_boundary", "handoff_ready"),
        transition("handoff_revalidating", "revalidate_failed", "recovery_required_closed"),
        transition(
          "handoff_ready",
          "append_successor_chain_durable",
          "successor_generation_durable",
        ),
        transition("handoff_ready", "append_unknown", "recovery_required_closed"),
      ],
    ),
  ),
  definition(
    "recovery_execution_admission",
    "runner-ledger-recovery-execution-admission",
    "prepare_recovery_execution",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("prepare_recovery_execution"),
    machine(
      "runner-ledger-recovery-execution-admission/v1",
      [
        "execution_admission_closed",
        "execution_admission_ready",
        "recovery_required_closed",
        "session_revalidating",
        "unclassified",
        "unknown_rejected",
      ],
      ["execution_admission_closed", "recovery_required_closed", "unknown_rejected"],
      [
        transition("unclassified", "consume_recovery_execution_permit", "session_revalidating"),
        transition("unclassified", "select_unknown", "unknown_rejected"),
        transition(
          "session_revalidating",
          "revalidate_exact_boundary",
          "execution_admission_ready",
        ),
        transition("session_revalidating", "revalidate_failed", "recovery_required_closed"),
        transition(
          "execution_admission_ready",
          "close_without_mutation",
          "execution_admission_closed",
        ),
      ],
    ),
  ),
  definition(
    "recovery_success_writer",
    "runner-ledger-recovery-success-writer",
    "execute_one_recovery_attempt",
    EXECUTION_PROFILE_ID,
    EXECUTION_PROFILE_ID,
    [],
    machine(
      "runner-ledger-recovery-success-writer/v1",
      RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATES,
      RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TERMINAL_STATES,
      RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TRANSITIONS.map((item, index) =>
        index === 0
          ? transition(item.from, "consume_recovery_execution_permit", item.to)
          : transition(item.from, item.event, item.to),
      ),
    ),
  ),
  definition(
    "return_failure",
    "runner-ledger-return-failure",
    "return_typed_failure",
    COMMON_PROFILE_ID,
    COMMON_PROFILE_ID,
    pairsFor("return_typed_failure"),
    machine(
      "runner-ledger-return-failure/v1",
      [
        "failure_returned",
        "recovery_required_closed",
        "result_ready",
        "result_revalidating",
        "unclassified",
        "unknown_rejected",
      ],
      ["failure_returned", "recovery_required_closed", "unknown_rejected"],
      [
        transition("unclassified", "consume_return_failure_permit", "result_revalidating"),
        transition("unclassified", "select_unknown", "unknown_rejected"),
        transition("result_revalidating", "revalidate_exact_boundary", "result_ready"),
        transition("result_revalidating", "revalidate_failed", "recovery_required_closed"),
        transition("result_ready", "return_exact_typed_failure", "failure_returned"),
      ],
    ),
  ),
] as const;

export const RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS = Object.fromEntries(
  PROFILE_DEFINITIONS.map((item) => [
    item.family,
    `contracts/generated/platform/v1alpha1/${item.slug}-registry-v1.json`,
  ]),
) as Readonly<Record<RunnerLedgerRecoveryFamily, string>>;

export const RUNNER_LEDGER_RECOVERY_GENERATOR_SOURCES = [
  RUNNER_LEDGER_RECOVERY_SOURCE_PATH,
  RUNNER_LEDGER_RECOVERY_SOURCE_SCHEMA_PATH,
  RUNNER_LEDGER_RECOVERY_OUTPUT_SCHEMA_PATH,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
  "docs/plan/adr/0023-p1-runner-ledger-recovery-writer-contract.md",
  "docs/plan/p1/runner-ledger-recovery-contract-audit-20260822.md",
  "docs/plan/p1/runner-ledger-recovery-contract-decision-20260822.md",
  "scripts/generate-platform-runner-ledger-recovery-registries.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
  "scripts/lib/platform-runner-ledger-recovery-registry.test.ts",
  "scripts/lib/platform-runner-ledger-recovery-registry.ts",
] as const;

export class RunnerLedgerRecoveryContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "RunnerLedgerRecoveryContractError";
  }
}

export function expectedRunnerLedgerRecoverySourceSuite(): SourceSuite {
  return {
    formatVersion: SOURCE_FORMAT,
    suiteId: SUITE_ID,
    profiles: PROFILE_DEFINITIONS.map((item) => ({
      family: item.family,
      registryId: `cloud-agents/platform/${item.slug}`,
      profileId: `${item.slug}/v1`,
      action: item.action,
      predecessorProfileId: item.predecessorProfileId,
      permitFromProfileId: item.permitFromProfileId,
      pairBindings: item.pairBindings,
    })),
  };
}

export function buildRunnerLedgerRecoveryRegistries(root: string): ReadonlyArray<JsonRecord> {
  const source = readSource(root);
  validateRunnerLedgerRecoverySource(root, source);
  const historical = buildHistoricalRegistries(root);
  validateConsumerPairBinding(source, historical.consumer);

  const built = new Map<string, BoundRegistry>();
  return PROFILE_DEFINITIONS.map((definition, index) => {
    const sourceProfile = source.profiles[index];
    if (sourceProfile === undefined) throw new Error("Recovery source profile is missing.");
    const predecessor =
      definition.predecessorProfileId === "runner-ledger-consumer/v1"
        ? historical.consumer
        : built.get(definition.predecessorProfileId);
    if (predecessor === undefined) {
      throw contractError(
        "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
        `/profiles/${index}/predecessorProfileId`,
        `Recovery predecessor ${definition.predecessorProfileId} is unavailable.`,
      );
    }
    const generated = buildRegistry({
      root,
      definition,
      sourceProfile,
      suiteSource: source,
      predecessor,
      historicalBindings: definition.family === "recovery_admission" ? historical.bindings : [],
    });
    built.set(
      (generated.profile as { spec: { profileId: string } }).spec.profileId,
      generated as BoundRegistry,
    );
    return generated;
  });
}

export function buildRunnerLedgerRecoveryRegistry(
  root: string,
  family: RunnerLedgerRecoveryFamily,
): JsonRecord {
  const index = RUNNER_LEDGER_RECOVERY_FAMILIES.indexOf(family);
  const registry = buildRunnerLedgerRecoveryRegistries(root)[index];
  if (registry === undefined) throw new Error(`Unknown recovery family ${family}.`);
  return registry;
}

export function assertRunnerLedgerRecoveryRegistriesCurrent(root: string): void {
  for (const [index, registry] of buildRunnerLedgerRecoveryRegistries(root).entries()) {
    const family = RUNNER_LEDGER_RECOVERY_FAMILIES[index];
    if (family === undefined) throw new Error("Recovery family is missing.");
    const path = RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family];
    const actual = readFileSync(resolve(root, path), "utf8");
    if (actual !== serializeRunnerLedgerRecoveryRegistry(registry)) {
      throw contractError(
        "RUNNER_LEDGER_RECOVERY_REGISTRY_DIGEST_MISMATCH",
        "/registryDigest",
        `${path} is stale; run the recovery registry generator.`,
      );
    }
  }
}

export function serializeRunnerLedgerRecoveryRegistry(registry: JsonRecord): string {
  const serialized = JSON.stringify(registry, null, 2);
  const formatted = serialized.replace(
    /^([ ]*)"terminalStates": \[\n((?:[ ]+"[^"]+",?\n)+)\1\](,?)$/gmu,
    (block, indent: string, body: string, suffix: string) => {
      const values = body
        .trim()
        .split("\n")
        .map((line) => JSON.parse(line.trim().replace(/,$/u, "")) as string);
      const terminalStates = `[${values.map((value) => JSON.stringify(value)).join(", ")}]`;
      const inline = `${indent}"terminalStates": ${terminalStates}${suffix}`;
      return inline.length <= 100 ? inline : block;
    },
  );
  return `${formatted}\n`;
}

export function runnerLedgerRecoveryRegistryInputs(_root: string): string[] {
  return [...RUNNER_LEDGER_RECOVERY_GENERATOR_SOURCES].toSorted();
}

export function validateRunnerLedgerRecoveryFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document)) return success();
  try {
    if (document.formatVersion === SOURCE_FORMAT) {
      validateRunnerLedgerRecoverySource(root, document as SourceSuite);
      validateConsumerPairBinding(
        document as SourceSuite,
        buildRunnerLedgerConsumerRegistry(root) as BoundRegistry,
      );
      return success();
    }
    if (document.formatVersion !== OUTPUT_FORMAT) return success();
    const expected = buildRunnerLedgerRecoveryRegistries(root).find(
      (candidate) => candidate.registryId === document.registryId,
    );
    return expected !== undefined && canonicalEqual(document, expected)
      ? success()
      : failure("RUNNER_LEDGER_RECOVERY_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  } catch (error) {
    return failure(
      error instanceof RunnerLedgerRecoveryContractError
        ? error.code
        : "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
      error instanceof RunnerLedgerRecoveryContractError ? error.path : "/",
    );
  }
}

export function validateRunnerLedgerRecoverySource(root: string, source: SourceSuite): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (!canonicalEqual(source, expectedRunnerLedgerRecoverySourceSuite())) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
      "/profiles",
      "Runner ledger recovery source suite drifted from the closed ADR-0023 mapping.",
    );
  }
  for (const [index, definition] of PROFILE_DEFINITIONS.entries()) {
    validateStateMachine(definition.stateMachine, `/profiles/${index}/stateMachine`);
  }
}

function buildHistoricalRegistries(root: string): {
  readonly consumer: BoundRegistry;
  readonly bindings: ReadonlyArray<JsonRecord>;
} {
  assertRunnerLedgerPreflightRegistryCurrent(root);
  assertRunnerLedgerConsumerRegistryCurrent(root);
  assertRunnerLedgerEntryAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root);
  const registries = [
    buildRunnerLedgerPreflightRegistry(root),
    buildRunnerLedgerConsumerRegistry(root),
    buildRunnerLedgerEntryAdmissionRegistry(root),
    buildRunnerLedgerEntryExecutionAdmissionRegistry(root),
    buildRunnerLedgerEntrySuccessWriterRegistry(root),
  ] as ReadonlyArray<BoundRegistry>;
  const consumer = registries[1];
  if (consumer === undefined) throw new Error("Historical consumer registry is missing.");
  return { consumer, bindings: registries.map(registryBinding) };
}

function validateConsumerPairBinding(source: SourceSuite, consumer: BoundRegistry): void {
  const matrix = consumer.profile.spec.transitionMatrix;
  if (consumer.profile.spec.profileId !== "runner-ledger-consumer/v1" || matrix === undefined) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
      "/profiles/0/predecessorProfileId",
      "Recovery admission must bind immutable runner-ledger-consumer/v1.",
    );
  }
  const expected = Object.entries(matrix).flatMap(([disposition, rawPairs]) =>
    rawPairs
      .filter(
        (item) =>
          item.consumerAction === "recovery_not_implemented" ||
          (item.consumerAction === "entry_not_implemented" &&
            disposition === "empty_brand_new" &&
            item.state === "brand_new_inherited" &&
            item.action === "begin_next_attempt"),
      )
      .map((item) => ({
        disposition,
        state: item.state!,
        action: item.action!,
        consumerAction: item.consumerAction! as
          | "entry_not_implemented"
          | "recovery_not_implemented",
        profileAction: actionForPair(item.state!, item.action!),
      })),
  );
  if (!canonicalEqual(source.profiles[0]?.pairBindings, expected)) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
      "/profiles/0/pairBindings",
      "Recovery admission must cover exactly the immutable twelve unsupported pairs.",
    );
  }
}

function buildRegistry(options: {
  readonly root: string;
  readonly definition: ProfileDefinition;
  readonly sourceProfile: SourceProfile;
  readonly suiteSource: SourceSuite;
  readonly predecessor: BoundRegistry;
  readonly historicalBindings: ReadonlyArray<JsonRecord>;
}): JsonRecord {
  const { definition, sourceProfile } = options;
  const predecessorBinding = registryBinding(options.predecessor);
  const profile = {
    profileId: sourceProfile.profileId,
    stateMachineId: sourceProfile.profileId,
    action: sourceProfile.action,
    canonicalization: {
      profile: `cloud-agents-${definition.slug}/v1-rfc8785-sha256`,
      algorithm: "RFC8785",
      digest: "SHA-256",
      comparison: "exact_string_no_rewrite",
    },
    identityBindings: identityBindings(definition),
    errorPrecedence: errorPrecedence(),
    pairBindings: sourceProfile.pairBindings,
    permitFromProfileId: sourceProfile.permitFromProfileId,
  };
  const stateMachine = definition.stateMachine;
  const profileSelector = recoverySelector(definition);
  const boundary = recoveryImplementationBoundary(definition);
  const sourceDigest = domainDigest(`cloud-agents/${definition.slug}/source/v1`, sourceProfile);
  const suiteSourceDigest = domainDigest(
    "cloud-agents/runner-ledger-recovery/source-suite/v1",
    options.suiteSource,
  );
  const stateMachineDigest = domainDigest(
    `cloud-agents/${definition.slug}/state-machine/v1`,
    stateMachine,
  );
  const policyDigest = domainDigest(`cloud-agents/${definition.slug}/policy/v1`, {
    predecessorBinding,
    historicalBindings: options.historicalBindings,
    selector: profileSelector,
    implementationBoundary: boundary,
    errorPrecedence: profile.errorPrecedence,
    pairBindings: profile.pairBindings,
    permitFromProfileId: profile.permitFromProfileId,
  });
  const profileDigest = domainDigest(`cloud-agents/${definition.slug}/profile/v1`, {
    registryId: sourceProfile.registryId,
    predecessorBinding,
    historicalBindings: options.historicalBindings,
    stateMachineDigest,
    policyDigest,
    profile,
  });
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    suiteId: SUITE_ID,
    family: definition.family,
    registryId: sourceProfile.registryId,
    sourceDigest,
    suiteSourceDigest,
    stateMachineDigest,
    policyDigest,
    predecessorBinding,
    historicalBindings: options.historicalBindings,
    profile: { profileDigest, spec: profile },
    stateMachine,
    selector: profileSelector,
    implementationBoundary: boundary,
  };
  const generated = {
    ...body,
    registryDigest: domainDigest(`cloud-agents/${definition.slug}/registry/v1`, body),
  };
  validateAgainstSchema(options.root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

function identityBindings(definition: ProfileDefinition): JsonRecord {
  const consumed = definition.permitFromProfileId ?? definition.predecessorProfileId;
  return {
    predecessorProfile: `exact_generated_${definition.predecessorProfileId}`,
    consumedAuthority: `exact_registry_backed_one_shot_${consumed}`,
    currentEvidenceBoundary:
      "exact_same_verifier_full_root_generation_journal_cursor_before_and_after_action",
    generationCursor: "exact_generation_journal_checkpoint_terminal_resolution_and_continuation",
    ledgerPrefix: "exact_length_head_rows_and_domain_separated_digest",
    catalogProjection: "exact_verified_before_after_and_cumulative_catalog",
    executionPolicy: "exact_ordered_migrations_statement_plans_and_max_attempts",
    actionReceipt: `exact_${definition.action}_closed_receipt_or_read_only_result`,
    oneShotAuthority: "registry_backed_noncopyable_pointer_owner_bound_single_consume",
    crossProfileRejection: "literal_copy_stale_foreign_replaced_and_other_profile_fail_closed",
  };
}

function errorPrecedence(): JsonRecord {
  return {
    storedContradiction: "MIGRATION_EVIDENCE_JOURNAL_CORRUPT_BEFORE_ACTION",
    contextOrOperationalFailure: "STABLE_CONTEXT_OR_OPERATIONAL_FAILURE_BEFORE_ACTION",
    recoveryRequired: "MIGRATION_EVIDENCE_RECOVERY_REQUIRED_BEFORE_UNSUPPORTED",
    unsupportedTransition: "MIGRATION_PROJECTION_NOT_IMPLEMENTED",
    mutationOutcomeUnknown: "UNKNOWN_MUTATION_REVOKES_OLD_CURSOR_AND_REQUIRES_RECOVERY",
    cleanupUnknown: "CLEANUP_UNCERTAINTY_DOMINATES_ORDINARY_RESULT",
    oneShotConsumption: "OLD_PERMIT_AND_SECOND_TRANSITION_FAIL_CLOSED",
  };
}

function recoverySelector(definition: ProfileDefinition): JsonRecord {
  return {
    mode: "generated_registry_only",
    profileSelection: "exact_profile_id_and_digest",
    predecessorProfileSelection: `exact_${definition.predecessorProfileId}`,
    callerProvidedProfile: "forbidden",
    callerProvidedAction: "forbidden",
    ordinaryFactAsPermit: "forbidden",
    crossProfilePermit: "forbidden",
    permitSource:
      definition.permitFromProfileId === null
        ? "consumed_same_verifier_runner_consumer_claim_only"
        : `consumed_exact_one_shot_permit_from_${definition.permitFromProfileId}`,
  };
}

function recoveryImplementationBoundary(definition: ProfileDefinition): JsonRecord {
  return {
    sliceA: "generated_contract_and_ordinary_profile_only",
    productionConsumer: "none_in_slice_a",
    claim: "not_implemented_in_slice_a",
    databaseSession: "not_opened_in_slice_a",
    databaseTransaction: "forbidden_in_slice_a",
    sqlExecution: "forbidden_in_slice_a",
    ledgerMutation: "forbidden_in_slice_a",
    evidenceMutation: "forbidden_in_slice_a",
    lineageMutation: "forbidden_in_slice_a",
    writerAction: `${definition.action}_not_implemented_in_slice_a`,
    caller: "not_implemented_in_slice_a",
    httpSurface: "not_implemented",
    p2Surface: "not_implemented",
    providerSideEffects: "forbidden",
    productionDatabaseWrites: "not_authorized",
    deployment: "not_authorized",
    publication: "not_authorized",
    gateStatus: "all_gates_open",
  };
}

function registryBinding(registry: BoundRegistry): JsonRecord {
  return {
    registryId: registry.registryId,
    registryDigest: registry.registryDigest,
    stateMachineDigest: registry.stateMachineDigest,
    policyDigest: registry.policyDigest,
    profileId: registry.profile.spec.profileId,
    profileDigest: registry.profile.profileDigest,
  };
}

function validateStateMachine(machine: StateMachine, path: string): void {
  const states = new Set(machine.states);
  const terminal = new Set(machine.terminalStates);
  const edges = new Set<string>();
  if (
    machine.initialState !== "unclassified" ||
    states.size !== machine.states.length ||
    terminal.size !== machine.terminalStates.length ||
    !states.has(machine.initialState) ||
    machine.terminalStates.some((item) => !states.has(item))
  ) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_STATE_MACHINE_INVALID",
      path,
      "Recovery state machine identity set is invalid.",
    );
  }
  for (const item of machine.transitions) {
    const edge = `${item.from}\u0000${item.event}`;
    if (
      edges.has(edge) ||
      !states.has(item.from) ||
      !states.has(item.to) ||
      terminal.has(item.from)
    ) {
      throw contractError(
        "RUNNER_LEDGER_RECOVERY_STATE_MACHINE_INVALID",
        `${path}/transitions`,
        "Recovery state machine is not closed.",
      );
    }
    edges.add(edge);
  }
  const reachable = new Set<string>([machine.initialState]);
  for (;;) {
    const before = reachable.size;
    for (const item of machine.transitions) if (reachable.has(item.from)) reachable.add(item.to);
    if (reachable.size === before) break;
  }
  if (machine.states.some((item) => !reachable.has(item))) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_STATE_MACHINE_INVALID",
      `${path}/states`,
      "Every recovery state must be reachable.",
    );
  }
}

function validateAgainstSchema(root: string, schemaID: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const path of [
    RUNNER_LEDGER_RECOVERY_SOURCE_SCHEMA_PATH,
    RUNNER_LEDGER_RECOVERY_OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaID);
  if (!validate) throw new Error(`Runner ledger recovery schema ${schemaID} is missing.`);
  if (!validate(value)) {
    const paths = (validate.errors ?? []).map((error) => error.instancePath);
    const code = paths.every((path) => path.startsWith("/implementationBoundary"))
      ? "RUNNER_LEDGER_RECOVERY_BOUNDARY_MISMATCH"
      : paths.every((path) => path.startsWith("/stateMachine"))
        ? "RUNNER_LEDGER_RECOVERY_STATE_MACHINE_INVALID"
        : "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH";
    throw contractError(
      code,
      "/",
      `Runner ledger recovery schema validation failed: ${ajv.errorsText(validate.errors)}.`,
    );
  }
}

function readSource(root: string): SourceSuite {
  return parseJsonFile(resolve(root, RUNNER_LEDGER_RECOVERY_SOURCE_PATH)) as SourceSuite;
}

function parseJsonFile(path: string): JsonRecord {
  const stat = lstatSync(path);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`Expected regular file: ${path}.`);
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (!isRecord(parsed)) throw new Error(`Expected JSON object: ${path}.`);
  return parsed;
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(`${domain}\n`);
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  const leftBytes = canonicalizeJson(left);
  const rightBytes = canonicalizeJson(right);
  return (
    leftBytes.byteLength === rightBytes.byteLength &&
    leftBytes.every((value, index) => value === rightBytes[index])
  );
}

function pair(
  disposition: string,
  state: string,
  action: string,
  consumerAction: "entry_not_implemented" | "recovery_not_implemented",
  profileAction: string,
): RunnerLedgerRecoveryPairBinding {
  return { disposition, state, action, consumerAction, profileAction };
}

function pairsFor(action: string): ReadonlyArray<RunnerLedgerRecoveryPairBinding> {
  return RECOVERY_PAIR_BINDINGS.filter((item) => item.profileAction === action);
}

function actionForPair(state: string, action: string): string {
  const found = RECOVERY_PAIR_BINDINGS.find(
    (item) => item.state === state && item.action === action,
  );
  if (found === undefined) {
    throw contractError(
      "RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH",
      "/profiles/0/pairBindings",
      `Unsupported recovery pair ${state}/${action}.`,
    );
  }
  return found.profileAction;
}

function definition(
  family: RunnerLedgerRecoveryFamily,
  slug: string,
  action: string,
  predecessorProfileId: string,
  permitFromProfileId: string | null,
  pairBindings: ReadonlyArray<RunnerLedgerRecoveryPairBinding>,
  stateMachine: StateMachine,
): ProfileDefinition {
  return {
    family,
    slug,
    action,
    predecessorProfileId,
    permitFromProfileId,
    pairBindings,
    stateMachine,
  };
}

function machine(
  id: string,
  states: ReadonlyArray<string>,
  terminalStates: ReadonlyArray<string>,
  transitions: StateMachine["transitions"],
): StateMachine {
  return { id, initialState: "unclassified", states, terminalStates, transitions };
}

function appendMachine(id: string, proof: string, record: string): StateMachine {
  return machine(
    id,
    [
      "append_ready",
      `${record}_durable`,
      "recovery_required_closed",
      `${proof}_revalidating`,
      "unclassified",
      "unknown_rejected",
    ],
    [`${record}_durable`, "recovery_required_closed", "unknown_rejected"],
    [
      transition("unclassified", "consume_exact_action_permit", `${proof}_revalidating`),
      transition("unclassified", "select_unknown", "unknown_rejected"),
      transition(`${proof}_revalidating`, "revalidate_exact_boundary", "append_ready"),
      transition(`${proof}_revalidating`, "revalidate_failed", "recovery_required_closed"),
      transition("append_ready", `append_${record}_durable`, `${record}_durable`),
      transition("append_ready", "append_unknown", "recovery_required_closed"),
    ],
  );
}

function transition(from: string, event: string, to: string): StateMachine["transitions"][number] {
  return { from, event, to };
}

function contractError(
  code: SemanticErrorCode,
  path: string,
  message: string,
): RunnerLedgerRecoveryContractError {
  return new RunnerLedgerRecoveryContractError(code, path, message);
}

function success(): SemanticResult {
  return { valid: true, errors: [] };
}

function failure(code: SemanticErrorCode, path: string): SemanticResult {
  return { valid: false, errors: [{ code, path }] };
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
