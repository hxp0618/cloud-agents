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
  assertRunnerLedgerEntryAdmissionRegistryCurrent,
  buildRunnerLedgerEntryAdmissionRegistry,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX,
} from "./platform-runner-ledger-entry-admission-registry";

export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-execution-admission-registry-source-v1.json";
export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json";
export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-success-writer-registry-source-v1.json";
export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json";

const EXECUTION_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json";
const EXECUTION_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json";
const WRITER_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json";
const WRITER_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json";

const EXECUTION_SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json";
const EXECUTION_OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json";
const WRITER_SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json";
const WRITER_OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json";

const EXECUTION_SOURCE_FORMAT = "cloud-agents-runner-ledger-entry-execution-admission-source/v1";
const EXECUTION_OUTPUT_FORMAT = "cloud-agents-runner-ledger-entry-execution-admission-registry/v1";
const EXECUTION_REGISTRY_ID = "cloud-agents/platform/runner-ledger-entry-execution-admission";
const EXECUTION_PROFILE_ID = "runner-ledger-entry-execution-admission/v1";
const EXECUTION_SOURCE_DOMAIN = "cloud-agents/runner-ledger-entry-execution-admission/source/v1";
const EXECUTION_PROFILE_DOMAIN = "cloud-agents/runner-ledger-entry-execution-admission/profile/v1";
const EXECUTION_STATE_MACHINE_DOMAIN =
  "cloud-agents/runner-ledger-entry-execution-admission/state-machine/v1";
const EXECUTION_POLICY_DOMAIN = "cloud-agents/runner-ledger-entry-execution-admission/policy/v1";
const EXECUTION_REGISTRY_DOMAIN =
  "cloud-agents/runner-ledger-entry-execution-admission/registry/v1";

const WRITER_SOURCE_FORMAT = "cloud-agents-runner-ledger-entry-success-writer-source/v1";
const WRITER_OUTPUT_FORMAT = "cloud-agents-runner-ledger-entry-success-writer-registry/v1";
const WRITER_REGISTRY_ID = "cloud-agents/platform/runner-ledger-entry-success-writer";
const WRITER_PROFILE_ID = "runner-ledger-entry-success-writer/v1";
const WRITER_SOURCE_DOMAIN = "cloud-agents/runner-ledger-entry-success-writer/source/v1";
const WRITER_PROFILE_DOMAIN = "cloud-agents/runner-ledger-entry-success-writer/profile/v1";
const WRITER_STATE_MACHINE_DOMAIN =
  "cloud-agents/runner-ledger-entry-success-writer/state-machine/v1";
const WRITER_POLICY_DOMAIN = "cloud-agents/runner-ledger-entry-success-writer/policy/v1";
const WRITER_REGISTRY_DOMAIN = "cloud-agents/runner-ledger-entry-success-writer/registry/v1";

export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX = {
  empty_brand_new: [
    {
      state: "brand_new",
      action: "begin_first_attempt",
      executionAction: "prepare_entry_execution",
    },
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt",
      executionAction: "prepare_entry_execution",
    },
  ],
  partial_next_entry: [
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt_next_entry",
      executionAction: "prepare_entry_execution",
    },
    {
      state: "terminal",
      action: "begin_first_attempt_next_entry",
      executionAction: "prepare_entry_execution",
    },
  ],
} as const;

export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION = {
  executionAction: "prepare_entry_execution",
  action: "execute_one_entry_known_success",
} as const;

export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_STATES = [
  "execution_admission_closed",
  "execution_admission_ready",
  "session_revalidating",
  "unclassified",
  "unknown_rejected",
] as const;
export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TERMINAL_STATES = [
  "execution_admission_closed",
  "unknown_rejected",
] as const;
export const RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITIONS = [
  { from: "unclassified", event: "select_first_attempt_entry", to: "session_revalidating" },
  { from: "unclassified", event: "select_unknown", to: "unknown_rejected" },
  {
    from: "session_revalidating",
    event: "revalidate_exact_boundary",
    to: "execution_admission_ready",
  },
  {
    from: "session_revalidating",
    event: "revalidate_failed",
    to: "execution_admission_closed",
  },
  {
    from: "execution_admission_ready",
    event: "close_without_mutation",
    to: "execution_admission_closed",
  },
] as const;

export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATES = [
  "closed_failure",
  "commit_intent_durable",
  "commit_known_committed",
  "entry_committed_complete",
  "entry_committed_next_entry",
  "execution_ready",
  "final_intermediate_durable",
  "intent_durable",
  "intermediate_durable",
  "ledger_readback_ready",
  "recovery_required_closed",
  "statement_executed",
  "statement_ready",
  "terminal_durable",
  "transaction_ready",
  "unclassified",
  "unknown_rejected",
] as const;
export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TERMINAL_STATES = [
  "closed_failure",
  "entry_committed_complete",
  "entry_committed_next_entry",
  "recovery_required_closed",
  "unknown_rejected",
] as const;
export const RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TRANSITIONS = [
  { from: "unclassified", event: "consume_execution_permit", to: "execution_ready" },
  { from: "unclassified", event: "select_unknown", to: "unknown_rejected" },
  { from: "execution_ready", event: "begin_transaction", to: "transaction_ready" },
  { from: "execution_ready", event: "fail_before_mutation", to: "closed_failure" },
  { from: "transaction_ready", event: "prepare_statement", to: "statement_ready" },
  { from: "transaction_ready", event: "fail_before_mutation", to: "closed_failure" },
  { from: "statement_ready", event: "append_intent_durable", to: "intent_durable" },
  { from: "statement_ready", event: "fail_before_mutation", to: "closed_failure" },
  { from: "intent_durable", event: "execute_exact_statement", to: "statement_executed" },
  { from: "intent_durable", event: "fail_after_intent", to: "recovery_required_closed" },
  {
    from: "statement_executed",
    event: "append_intermediate_nonfinal",
    to: "intermediate_durable",
  },
  {
    from: "statement_executed",
    event: "append_intermediate_final",
    to: "final_intermediate_durable",
  },
  {
    from: "statement_executed",
    event: "mutation_outcome_unknown",
    to: "recovery_required_closed",
  },
  { from: "intermediate_durable", event: "advance_statement", to: "statement_ready" },
  {
    from: "intermediate_durable",
    event: "fail_after_intermediate",
    to: "recovery_required_closed",
  },
  {
    from: "final_intermediate_durable",
    event: "insert_and_readback_ledger",
    to: "ledger_readback_ready",
  },
  {
    from: "final_intermediate_durable",
    event: "fail_after_intermediate",
    to: "recovery_required_closed",
  },
  { from: "ledger_readback_ready", event: "append_commit_intent", to: "commit_intent_durable" },
  {
    from: "ledger_readback_ready",
    event: "mutation_outcome_unknown",
    to: "recovery_required_closed",
  },
  { from: "commit_intent_durable", event: "commit_known", to: "commit_known_committed" },
  {
    from: "commit_intent_durable",
    event: "commit_rejected_or_unknown",
    to: "recovery_required_closed",
  },
  {
    from: "commit_known_committed",
    event: "append_terminal_durable",
    to: "terminal_durable",
  },
  {
    from: "commit_known_committed",
    event: "terminal_append_failed_or_unknown",
    to: "recovery_required_closed",
  },
  {
    from: "terminal_durable",
    event: "classify_bundle_complete",
    to: "entry_committed_complete",
  },
  {
    from: "terminal_durable",
    event: "classify_next_entry",
    to: "entry_committed_next_entry",
  },
] as const;

type StateMachine = {
  readonly id: string;
  readonly initialState: string;
  readonly states: ReadonlyArray<string>;
  readonly terminalStates: ReadonlyArray<string>;
  readonly transitions: ReadonlyArray<{
    readonly from: string;
    readonly event: string;
    readonly to: string;
  }>;
};

type RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly profile: JsonRecord & {
    readonly profileId: string;
    readonly stateMachineId: string;
    readonly errorPrecedence: JsonRecord;
  };
  readonly stateMachine: StateMachine;
  readonly selector: JsonRecord;
  readonly implementationBoundary: JsonRecord;
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
      readonly transitionMatrix?: typeof RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX;
    };
  };
};

export class RunnerLedgerEntryWriterContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "RunnerLedgerEntryWriterContractError";
  }
}

export function buildRunnerLedgerEntryExecutionAdmissionRegistry(root: string): JsonRecord {
  assertRunnerLedgerEntryAdmissionRegistryCurrent(root);
  const entryAdmission = buildRunnerLedgerEntryAdmissionRegistry(root) as BoundRegistry;
  const source = readSource(root, RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_SOURCE_PATH);
  validateRunnerLedgerEntryExecutionAdmissionSource(root, source);
  validateExecutionPairBinding(source, entryAdmission);
  const entryAdmissionBinding = registryBinding(entryAdmission);
  return buildRegistry({
    source,
    bindingName: "entryAdmissionBinding",
    binding: entryAdmissionBinding,
    outputFormat: EXECUTION_OUTPUT_FORMAT,
    sourceDomain: EXECUTION_SOURCE_DOMAIN,
    stateMachineDomain: EXECUTION_STATE_MACHINE_DOMAIN,
    policyDomain: EXECUTION_POLICY_DOMAIN,
    profileDomain: EXECUTION_PROFILE_DOMAIN,
    registryDomain: EXECUTION_REGISTRY_DOMAIN,
    outputSchemaID: EXECUTION_OUTPUT_SCHEMA_ID,
    schemaKind: "execution",
    root,
  });
}

export function buildRunnerLedgerEntrySuccessWriterRegistry(root: string): JsonRecord {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root);
  const executionAdmission = buildRunnerLedgerEntryExecutionAdmissionRegistry(
    root,
  ) as BoundRegistry;
  const source = readSource(root, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_SOURCE_PATH);
  validateRunnerLedgerEntrySuccessWriterSource(root, source);
  const executionAdmissionBinding = registryBinding(executionAdmission);
  return buildRegistry({
    source,
    bindingName: "executionAdmissionBinding",
    binding: executionAdmissionBinding,
    outputFormat: WRITER_OUTPUT_FORMAT,
    sourceDomain: WRITER_SOURCE_DOMAIN,
    stateMachineDomain: WRITER_STATE_MACHINE_DOMAIN,
    policyDomain: WRITER_POLICY_DOMAIN,
    profileDomain: WRITER_PROFILE_DOMAIN,
    registryDomain: WRITER_REGISTRY_DOMAIN,
    outputSchemaID: WRITER_OUTPUT_SCHEMA_ID,
    schemaKind: "writer",
    root,
  });
}

export function serializeRunnerLedgerEntryWriterRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root: string): void {
  assertCurrent(
    root,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
    buildRunnerLedgerEntryExecutionAdmissionRegistry(root),
    "RUNNER_LEDGER_ENTRY_EXECUTION_REGISTRY_DIGEST_MISMATCH",
  );
}

export function assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root: string): void {
  assertCurrent(
    root,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
    buildRunnerLedgerEntrySuccessWriterRegistry(root),
    "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_REGISTRY_DIGEST_MISMATCH",
  );
}

export function runnerLedgerEntryExecutionAdmissionRegistryInputs(_root: string): string[] {
  return [
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_SOURCE_PATH,
    RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
    EXECUTION_SOURCE_SCHEMA_PATH,
    EXECUTION_OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0022-p1-runner-ledger-entry-success-writer-contract.md",
    "docs/plan/p1/runner-ledger-entry-writer-contract-audit-20260822.md",
    "scripts/generate-platform-runner-ledger-entry-writer-registries.ts",
    "scripts/lib/platform-json-semantics.ts",
    "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
    "scripts/lib/platform-runner-ledger-entry-writer-registry.test.ts",
    "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
  ].toSorted();
}

export function runnerLedgerEntrySuccessWriterRegistryInputs(_root: string): string[] {
  return [
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_SOURCE_PATH,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
    WRITER_SOURCE_SCHEMA_PATH,
    WRITER_OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0022-p1-runner-ledger-entry-success-writer-contract.md",
    "docs/plan/p1/runner-ledger-entry-writer-contract-audit-20260822.md",
    "scripts/generate-platform-runner-ledger-entry-writer-registries.ts",
    "scripts/lib/platform-json-semantics.ts",
    "scripts/lib/platform-runner-ledger-entry-writer-registry.test.ts",
    "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
  ].toSorted();
}

export function validateRunnerLedgerEntryWriterFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document)) return success();
  try {
    switch (document.formatVersion) {
      case EXECUTION_SOURCE_FORMAT:
        validateRunnerLedgerEntryExecutionAdmissionSource(root, document as RegistrySource);
        validateExecutionPairBinding(
          document as RegistrySource,
          buildRunnerLedgerEntryAdmissionRegistry(root) as BoundRegistry,
        );
        return success();
      case EXECUTION_OUTPUT_FORMAT:
        return canonicalEqual(document, buildRunnerLedgerEntryExecutionAdmissionRegistry(root))
          ? success()
          : failure("RUNNER_LEDGER_ENTRY_EXECUTION_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
      case WRITER_SOURCE_FORMAT:
        validateRunnerLedgerEntrySuccessWriterSource(root, document as RegistrySource);
        return success();
      case WRITER_OUTPUT_FORMAT:
        return canonicalEqual(document, buildRunnerLedgerEntrySuccessWriterRegistry(root))
          ? success()
          : failure(
              "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_REGISTRY_DIGEST_MISMATCH",
              "/registryDigest",
            );
      default:
        return success();
    }
  } catch (error) {
    return failure(
      error instanceof RunnerLedgerEntryWriterContractError
        ? error.code
        : document.formatVersion === WRITER_SOURCE_FORMAT
          ? "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BINDING_MISMATCH"
          : "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      error instanceof RunnerLedgerEntryWriterContractError ? error.path : "/",
    );
  }
}

export function validateRunnerLedgerEntryExecutionAdmissionSource(
  root: string,
  source: RegistrySource,
): void {
  validateAgainstSchema(root, EXECUTION_SOURCE_SCHEMA_ID, source, "execution");
  if (
    source.formatVersion !== EXECUTION_SOURCE_FORMAT ||
    source.registryId !== EXECUTION_REGISTRY_ID ||
    source.profile.profileId !== EXECUTION_PROFILE_ID ||
    source.profile.stateMachineId !== EXECUTION_PROFILE_ID
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      "/profile",
      "Runner ledger entry execution-admission identity drifted.",
    );
  }
  validateStateMachine(
    source.stateMachine,
    EXECUTION_PROFILE_ID,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_STATES,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TERMINAL_STATES,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITIONS,
    "RUNNER_LEDGER_ENTRY_EXECUTION_STATE_MACHINE_INVALID",
  );
  if (
    !canonicalEqual(
      source.profile.transitionMatrix,
      RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX,
    )
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Runner ledger entry execution-admission pair matrix drifted.",
    );
  }
}

export function validateRunnerLedgerEntrySuccessWriterSource(
  root: string,
  source: RegistrySource,
): void {
  validateAgainstSchema(root, WRITER_SOURCE_SCHEMA_ID, source, "writer");
  if (
    source.formatVersion !== WRITER_SOURCE_FORMAT ||
    source.registryId !== WRITER_REGISTRY_ID ||
    source.profile.profileId !== WRITER_PROFILE_ID ||
    source.profile.stateMachineId !== WRITER_PROFILE_ID
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BINDING_MISMATCH",
      "/profile",
      "Runner ledger entry success-writer identity drifted.",
    );
  }
  validateStateMachine(
    source.stateMachine,
    WRITER_PROFILE_ID,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATES,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TERMINAL_STATES,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_TRANSITIONS,
    "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATE_MACHINE_INVALID",
  );
  if (!canonicalEqual(source.profile.writerAction, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION)) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BINDING_MISMATCH",
      "/profile/writerAction",
      "Runner ledger entry success-writer action drifted.",
    );
  }
}

function validateExecutionPairBinding(source: RegistrySource, entryAdmission: BoundRegistry): void {
  if (
    entryAdmission.registryId !== "cloud-agents/platform/runner-ledger-entry-admission" ||
    entryAdmission.profile.spec.profileId !== "runner-ledger-entry-admission/v1" ||
    entryAdmission.profile.spec.transitionMatrix === undefined
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      "/entryAdmissionBinding",
      "Execution admission must bind immutable entry-admission v1.",
    );
  }
  const expected = Object.fromEntries(
    Object.entries(entryAdmission.profile.spec.transitionMatrix)
      .map(([disposition, pairs]) => [
        disposition,
        pairs
          .filter(
            (pair) =>
              pair.admissionAction === "prepare_entry_admission" &&
              (pair.action === "begin_first_attempt" ||
                pair.action === "begin_first_attempt_next_entry"),
          )
          .map(({ state, action }) => ({
            state,
            action,
            executionAction: "prepare_entry_execution",
          })),
      ])
      .filter(([, pairs]) => (pairs as ReadonlyArray<unknown>).length > 0),
  );
  if (!canonicalEqual(source.profile.transitionMatrix, expected)) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Execution admission must cover exactly the four immutable first-attempt pairs.",
    );
  }
}

function buildRegistry(options: {
  readonly source: RegistrySource;
  readonly bindingName: string;
  readonly binding: JsonRecord;
  readonly outputFormat: string;
  readonly sourceDomain: string;
  readonly stateMachineDomain: string;
  readonly policyDomain: string;
  readonly profileDomain: string;
  readonly registryDomain: string;
  readonly outputSchemaID: string;
  readonly schemaKind: "execution" | "writer";
  readonly root: string;
}): JsonRecord {
  const sourceDigest = domainDigest(options.sourceDomain, options.source);
  const stateMachineDigest = domainDigest(options.stateMachineDomain, options.source.stateMachine);
  const policyDigest = domainDigest(options.policyDomain, {
    [options.bindingName]: options.binding,
    selector: options.source.selector,
    implementationBoundary: options.source.implementationBoundary,
    errorPrecedence: options.source.profile.errorPrecedence,
    transitionMatrix:
      options.source.profile.transitionMatrix ?? options.source.profile.writerAction,
  });
  const profileDigest = domainDigest(options.profileDomain, {
    registryId: options.source.registryId,
    [options.bindingName]: options.binding,
    stateMachineDigest,
    policyDigest,
    profile: options.source.profile,
  });
  const body: JsonRecord = {
    formatVersion: options.outputFormat,
    registryId: options.source.registryId,
    sourceDigest,
    stateMachineDigest,
    policyDigest,
    [options.bindingName]: options.binding,
    profile: { profileDigest, spec: options.source.profile },
    stateMachine: options.source.stateMachine,
    selector: options.source.selector,
    implementationBoundary: options.source.implementationBoundary,
  };
  const generated = {
    ...body,
    registryDigest: domainDigest(options.registryDomain, body),
  };
  validateAgainstSchema(options.root, options.outputSchemaID, generated, options.schemaKind);
  return generated;
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

function validateStateMachine(
  machine: StateMachine,
  id: string,
  states: ReadonlyArray<string>,
  terminalStates: ReadonlyArray<string>,
  transitions: ReadonlyArray<{
    readonly from: string;
    readonly event: string;
    readonly to: string;
  }>,
  code: SemanticErrorCode,
): void {
  if (
    machine.id !== id ||
    machine.initialState !== "unclassified" ||
    !canonicalEqual(machine.states, states) ||
    !canonicalEqual(machine.terminalStates, terminalStates) ||
    !canonicalEqual(machine.transitions, transitions)
  ) {
    throw contractError(code, "/stateMachine", "Generated runner ledger state machine drifted.");
  }
  const terminal = new Set(machine.terminalStates);
  const identities = new Set<string>();
  for (const transition of machine.transitions) {
    const identity = `${transition.from}\u0000${transition.event}`;
    if (
      identities.has(identity) ||
      !machine.states.includes(transition.from) ||
      !machine.states.includes(transition.to) ||
      terminal.has(transition.from)
    ) {
      throw contractError(code, "/stateMachine/transitions", "State machine is not closed.");
    }
    identities.add(identity);
  }
  const reachable = new Set<string>([machine.initialState]);
  for (;;) {
    const before = reachable.size;
    for (const transition of machine.transitions) {
      if (reachable.has(transition.from)) reachable.add(transition.to);
    }
    if (before === reachable.size) break;
  }
  if (machine.states.some((state) => !reachable.has(state))) {
    throw contractError(code, "/stateMachine/states", "Every state must be reachable.");
  }
}

function assertCurrent(
  root: string,
  path: string,
  registry: JsonRecord,
  code: SemanticErrorCode,
): void {
  const expected = serializeRunnerLedgerEntryWriterRegistry(registry);
  const actual = readFileSync(resolve(root, path), "utf8");
  if (actual !== expected) {
    throw contractError(code, "/registryDigest", `${path} is stale; run its generator.`);
  }
}

function validateAgainstSchema(
  root: string,
  schemaID: string,
  value: unknown,
  kind: "execution" | "writer",
): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const path of [
    EXECUTION_SOURCE_SCHEMA_PATH,
    EXECUTION_OUTPUT_SCHEMA_PATH,
    WRITER_SOURCE_SCHEMA_PATH,
    WRITER_OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaID);
  if (!validate) throw new Error(`Runner ledger entry writer schema ${schemaID} is missing.`);
  if (!validate(value)) {
    const paths = (validate.errors ?? []).map((error) => error.instancePath);
    const prefix =
      kind === "writer" ? "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER" : "RUNNER_LEDGER_ENTRY_EXECUTION";
    const suffix = paths.every((path) => path.startsWith("/stateMachine"))
      ? "STATE_MACHINE_INVALID"
      : paths.every((path) => path.startsWith("/implementationBoundary"))
        ? "BOUNDARY_MISMATCH"
        : "BINDING_MISMATCH";
    throw contractError(
      `${prefix}_${suffix}` as SemanticErrorCode,
      "/",
      `Runner ledger entry writer schema validation failed: ${ajv.errorsText(validate.errors)}.`,
    );
  }
}

function readSource(root: string, path: string): RegistrySource {
  return parseJsonFile(resolve(root, path)) as RegistrySource;
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

function contractError(
  code: SemanticErrorCode,
  path: string,
  message: string,
): RunnerLedgerEntryWriterContractError {
  return new RunnerLedgerEntryWriterContractError(code, path, message);
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
