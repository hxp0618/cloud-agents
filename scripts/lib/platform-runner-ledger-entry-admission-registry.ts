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
  assertRunnerLedgerConsumerRegistryCurrent,
  buildRunnerLedgerConsumerRegistry,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
  RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX,
} from "./platform-runner-ledger-consumer-registry";

export const RUNNER_LEDGER_ENTRY_ADMISSION_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json";
export const RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json";

const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-runner-ledger-entry-admission-source/v1";
const OUTPUT_FORMAT = "cloud-agents-runner-ledger-entry-admission-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/runner-ledger-entry-admission";
const SOURCE_DOMAIN = "cloud-agents/runner-ledger-entry-admission/source/v1";
const PROFILE_DOMAIN = "cloud-agents/runner-ledger-entry-admission/profile/v1";
const STATE_MACHINE_DOMAIN = "cloud-agents/runner-ledger-entry-admission/state-machine/v1";
const POLICY_DOMAIN = "cloud-agents/runner-ledger-entry-admission/policy/v1";
const REGISTRY_DOMAIN = "cloud-agents/runner-ledger-entry-admission/registry/v1";
const PROFILE_ID = "runner-ledger-entry-admission/v1";
const STATE_MACHINE_ID = PROFILE_ID;

const STATES = [
  "admission_closed",
  "admission_ready",
  "session_revalidating",
  "unclassified",
  "unknown_rejected",
] as const;
const TERMINAL_STATES = ["admission_closed", "unknown_rejected"] as const;
const TRANSITIONS = [
  { from: "unclassified", event: "select_entry", to: "session_revalidating" },
  { from: "unclassified", event: "select_unknown", to: "unknown_rejected" },
  {
    from: "session_revalidating",
    event: "revalidate_exact_boundary",
    to: "admission_ready",
  },
  { from: "session_revalidating", event: "revalidate_failed", to: "admission_closed" },
  { from: "admission_ready", event: "close_without_mutation", to: "admission_closed" },
] as const;

export const RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX = {
  empty_brand_new: [
    {
      state: "brand_new",
      action: "begin_first_attempt",
      admissionAction: "prepare_entry_admission",
    },
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt",
      admissionAction: "prepare_entry_admission",
    },
    {
      state: "brand_new_inherited",
      action: "begin_next_attempt",
      admissionAction: "prepare_entry_admission",
    },
  ],
  partial_next_entry: [
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt_next_entry",
      admissionAction: "prepare_entry_admission",
    },
    {
      state: "terminal",
      action: "begin_first_attempt_next_entry",
      admissionAction: "prepare_entry_admission",
    },
  ],
} as const;

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
    readonly transitionMatrix: typeof RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX;
  };
  readonly stateMachine: StateMachine;
  readonly selector: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

type ConsumerRegistry = JsonRecord & {
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly transitionMatrix: typeof RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX;
    };
  };
};

export class RunnerLedgerEntryAdmissionContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "RunnerLedgerEntryAdmissionContractError";
  }
}

export function buildRunnerLedgerEntryAdmissionRegistry(root: string): JsonRecord {
  assertRunnerLedgerConsumerRegistryCurrent(root);
  const consumer = buildRunnerLedgerConsumerRegistry(root) as ConsumerRegistry;
  const source = readSource(root);
  validateRunnerLedgerEntryAdmissionSource(root, source);
  validateConsumerPairBinding(source, consumer);
  const consumerBinding = {
    registryId: consumer.registryId,
    registryDigest: consumer.registryDigest,
    stateMachineDigest: consumer.stateMachineDigest,
    policyDigest: consumer.policyDigest,
    profileId: consumer.profile.spec.profileId,
    profileDigest: consumer.profile.profileDigest,
  };
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(STATE_MACHINE_DOMAIN, source.stateMachine);
  const policyDigest = domainDigest(POLICY_DOMAIN, {
    consumerBinding,
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
    errorPrecedence: source.profile.errorPrecedence,
    transitionMatrix: source.profile.transitionMatrix,
  });
  const profileDigest = domainDigest(PROFILE_DOMAIN, {
    registryId: source.registryId,
    consumerBinding,
    stateMachineDigest,
    policyDigest,
    profile: source.profile,
  });
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    registryId: REGISTRY_ID,
    sourceDigest,
    stateMachineDigest,
    policyDigest,
    consumerBinding,
    profile: { profileDigest, spec: source.profile },
    stateMachine: source.stateMachine,
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
  };
  const generated = { ...body, registryDigest: domainDigest(REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeRunnerLedgerEntryAdmissionRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertRunnerLedgerEntryAdmissionRegistryCurrent(root: string): void {
  const expected = serializeRunnerLedgerEntryAdmissionRegistry(
    buildRunnerLedgerEntryAdmissionRegistry(root),
  );
  const actual = readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function runnerLedgerEntryAdmissionRegistryInputs(_root: string): string[] {
  return [
    RUNNER_LEDGER_ENTRY_ADMISSION_SOURCE_PATH,
    RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0021-p1-runner-ledger-entry-admission-contract.md",
    "docs/plan/p1/runner-ledger-consumer-entry-blocker-20260821.md",
    "scripts/generate-platform-runner-ledger-entry-admission-registry.ts",
    "scripts/lib/platform-json-semantics.ts",
    "scripts/lib/platform-runner-ledger-consumer-registry.ts",
    "scripts/lib/platform-runner-ledger-entry-admission-registry.test.ts",
    "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
  ].toSorted();
}

export function validateRunnerLedgerEntryAdmissionFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document)) return success();
  if (document.formatVersion === SOURCE_FORMAT) {
    try {
      const source = document as RegistrySource;
      validateRunnerLedgerEntryAdmissionSource(root, source);
      validateConsumerPairBinding(
        source,
        buildRunnerLedgerConsumerRegistry(root) as ConsumerRegistry,
      );
      return success();
    } catch (error) {
      return failure(
        error instanceof RunnerLedgerEntryAdmissionContractError
          ? error.code
          : "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
        error instanceof RunnerLedgerEntryAdmissionContractError ? error.path : "/",
      );
    }
  }
  if (document.formatVersion !== OUTPUT_FORMAT) return success();
  try {
    const expected = buildRunnerLedgerEntryAdmissionRegistry(root);
    return canonicalEqual(document, expected)
      ? success()
      : failure("RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  } catch {
    return failure("RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  }
}

export function validateRunnerLedgerEntryAdmissionSource(
  root: string,
  source: RegistrySource,
): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (source.formatVersion !== SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/formatVersion",
      "Runner ledger entry-admission source identity drifted.",
    );
  }
  if (
    source.profile.profileId !== PROFILE_ID ||
    source.profile.stateMachineId !== STATE_MACHINE_ID
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/profile",
      "Runner ledger entry-admission profile must bind its generated state machine.",
    );
  }
  validateStateMachine(source.stateMachine);
  if (
    !canonicalEqual(
      source.profile.transitionMatrix,
      RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX,
    )
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Runner ledger entry-admission transition matrix drifted.",
    );
  }
  const expectedSelector = {
    mode: "generated_registry_only",
    profileSelection: "exact_profile_id_and_digest",
    consumerProfileSelection: "exact_runner_ledger_consumer_v1_generated_identity",
    callerProvidedProfile: "forbidden",
    callerProvidedDispatch: "forbidden",
    ordinaryFactAsPermit: "forbidden",
    admissionSource: "consumed_same_verifier_entry_consumer_fact_only",
  };
  if (!canonicalEqual(source.selector, expectedSelector)) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/selector",
      "Runner ledger entry-admission selector must accept only a consumed entry fact.",
    );
  }
  const expectedBoundary = {
    runnerConsumer: "entry_read_only_admission_only",
    existingBrandNewWriter: "separate_existing_authority_chain",
    entryWriter: "not_implemented",
    recoveryWriter: "not_implemented",
    databaseSession: "fresh_dedicated_locked_read_only_until_exact_close",
    databaseTransaction: "migration_and_read_write_forbidden",
    beginMigration: "forbidden",
    ledgerMutation: "forbidden",
    evidenceMutation: "forbidden",
    permitConsumer: "none",
    httpSurface: "not_implemented",
    p2Surface: "not_implemented",
    providerSideEffects: "forbidden",
    productionDatabaseWrites: "not_authorized",
    deployment: "not_authorized",
    publication: "not_authorized",
    gateStatus: "all_gates_open",
  };
  if (!canonicalEqual(source.implementationBoundary, expectedBoundary)) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BOUNDARY_MISMATCH",
      "/implementationBoundary",
      "Runner ledger entry-admission implementation boundary drifted.",
    );
  }
}

function validateConsumerPairBinding(source: RegistrySource, consumer: ConsumerRegistry): void {
  if (
    consumer.registryId !== "cloud-agents/platform/runner-ledger-consumer" ||
    consumer.profile.spec.profileId !== "runner-ledger-consumer/v1"
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/consumerBinding",
      "Runner ledger entry admission must bind immutable consumer v1.",
    );
  }
  const expected = Object.fromEntries(
    Object.entries(consumer.profile.spec.transitionMatrix)
      .map(([disposition, pairs]) => [
        disposition,
        pairs
          .filter((pair) => pair.consumerAction === "entry_not_implemented")
          .map(({ state, action }) => ({
            state,
            action,
            admissionAction: "prepare_entry_admission",
          })),
      ])
      .filter(([, pairs]) => (pairs as ReadonlyArray<unknown>).length > 0),
  );
  if (!canonicalEqual(source.profile.transitionMatrix, expected)) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Runner ledger entry admission must cover exactly the five immutable consumer-v1 entry pairs.",
    );
  }
}

function validateStateMachine(machine: StateMachine): void {
  if (
    machine.id !== STATE_MACHINE_ID ||
    machine.initialState !== "unclassified" ||
    !canonicalEqual(machine.states, STATES) ||
    !canonicalEqual(machine.terminalStates, TERMINAL_STATES) ||
    !canonicalEqual(machine.transitions, TRANSITIONS)
  ) {
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_STATE_MACHINE_INVALID",
      "/stateMachine",
      "Runner ledger entry-admission state machine must be closed, sorted, and deterministic.",
    );
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
    throw contractError(
      "RUNNER_LEDGER_ENTRY_ADMISSION_STATE_MACHINE_INVALID",
      "/stateMachine/states",
      "Every runner ledger entry-admission state must be reachable.",
    );
  }
}

function readSource(root: string): RegistrySource {
  return parseJsonFile(resolve(root, RUNNER_LEDGER_ENTRY_ADMISSION_SOURCE_PATH)) as RegistrySource;
}

function validateAgainstSchema(root: string, schemaID: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaID);
  if (!validate) {
    throw new Error(`Runner ledger entry-admission schema ${schemaID} was not registered.`);
  }
  if (!validate(value)) {
    const paths = (validate.errors ?? []).map((error) => error.instancePath);
    const code: SemanticErrorCode = paths.every((path) => path.startsWith("/stateMachine"))
      ? "RUNNER_LEDGER_ENTRY_ADMISSION_STATE_MACHINE_INVALID"
      : paths.every((path) => path.startsWith("/implementationBoundary"))
        ? "RUNNER_LEDGER_ENTRY_ADMISSION_BOUNDARY_MISMATCH"
        : "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH";
    throw contractError(
      code,
      "/",
      `Runner ledger entry-admission schema validation failed: ${ajv.errorsText(validate.errors)}.`,
    );
  }
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

function parseJsonFile(path: string): JsonRecord {
  const stat = lstatSync(path);
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`Expected a regular file: ${path}.`);
  const parsed: unknown = JSON.parse(readFileSync(path, "utf8"));
  if (!isRecord(parsed)) throw new Error(`Expected JSON object: ${path}.`);
  return parsed;
}

function contractError(
  code: SemanticErrorCode,
  path: string,
  message: string,
): RunnerLedgerEntryAdmissionContractError {
  return new RunnerLedgerEntryAdmissionContractError(code, path, message);
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
