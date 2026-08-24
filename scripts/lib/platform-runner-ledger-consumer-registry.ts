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

export const RUNNER_LEDGER_CONSUMER_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json";
export const RUNNER_LEDGER_CONSUMER_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json";

const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-runner-ledger-consumer-source/v1";
const OUTPUT_FORMAT = "cloud-agents-runner-ledger-consumer-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/runner-ledger-consumer";
const SOURCE_DOMAIN = "cloud-agents/runner-ledger-consumer/source/v1";
const PROFILE_DOMAIN = "cloud-agents/runner-ledger-consumer/profile/v1";
const STATE_MACHINE_DOMAIN = "cloud-agents/runner-ledger-consumer/state-machine/v1";
const POLICY_DOMAIN = "cloud-agents/runner-ledger-consumer/policy/v1";
const REGISTRY_DOMAIN = "cloud-agents/runner-ledger-consumer/registry/v1";
const PROFILE_ID = "runner-ledger-consumer/v1";
const STATE_MACHINE_ID = PROFILE_ID;

const STATES = [
  "complete_return_success_noop",
  "entry_not_implemented",
  "recovery_not_implemented",
  "unclassified",
  "unknown_not_implemented",
] as const;
const TERMINAL_STATES = STATES.filter((state) => state !== "unclassified");
const TRANSITIONS = [
  {
    from: "unclassified",
    event: "consume_complete_return_success",
    to: "complete_return_success_noop",
  },
  { from: "unclassified", event: "consume_entry", to: "entry_not_implemented" },
  { from: "unclassified", event: "consume_recovery", to: "recovery_not_implemented" },
  { from: "unclassified", event: "consume_unknown", to: "unknown_not_implemented" },
] as const;

export const RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX = {
  complete_return_success: [
    { state: "completed", action: "return_success", consumerAction: "return_success_noop" },
  ],
  empty_brand_new: [
    {
      state: "brand_new",
      action: "begin_first_attempt",
      consumerAction: "entry_not_implemented",
    },
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt",
      consumerAction: "entry_not_implemented",
    },
    {
      state: "brand_new_inherited",
      action: "begin_next_attempt",
      consumerAction: "entry_not_implemented",
    },
  ],
  partial_next_entry: [
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt_next_entry",
      consumerAction: "entry_not_implemented",
    },
    {
      state: "terminal",
      action: "begin_first_attempt_next_entry",
      consumerAction: "entry_not_implemented",
    },
  ],
  partial_retry_or_recovery: [
    {
      state: "brand_new_inherited",
      action: "begin_first_attempt",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "brand_new_inherited",
      action: "begin_next_attempt",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "dangling_statement_intent",
      action: "append_aborted_retryable",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "dangling_statement_intent",
      action: "append_aborted_terminal",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "dangling_intermediate",
      action: "append_aborted_retryable",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "dangling_intermediate",
      action: "append_aborted_terminal",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "dangling_commit_intent",
      action: "reconcile_commit",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "ambiguous_unresolved",
      action: "reconcile_commit",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "terminal",
      action: "begin_next_attempt",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "terminal",
      action: "return_failure",
      consumerAction: "recovery_not_implemented",
    },
    {
      state: "divergent",
      action: "return_failure",
      consumerAction: "recovery_not_implemented",
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
    readonly transitionMatrix: typeof RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX;
  };
  readonly stateMachine: StateMachine;
  readonly selector: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

type PreflightRegistry = JsonRecord & {
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly recoveryDispositionMatrix: Record<
        string,
        ReadonlyArray<{ readonly state: string; readonly action: string }>
      >;
    };
  };
};

export class RunnerLedgerConsumerContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "RunnerLedgerConsumerContractError";
  }
}

export function buildRunnerLedgerConsumerRegistry(root: string): JsonRecord {
  assertRunnerLedgerPreflightRegistryCurrent(root);
  const preflight = buildRunnerLedgerPreflightRegistry(root) as PreflightRegistry;
  const source = readSource(root);
  validateRunnerLedgerConsumerSource(root, source);
  validatePreflightPairBinding(source, preflight);
  const preflightBinding = {
    registryId: preflight.registryId,
    registryDigest: preflight.registryDigest,
    stateMachineDigest: preflight.stateMachineDigest,
    policyDigest: preflight.policyDigest,
    profileId: preflight.profile.spec.profileId,
    profileDigest: preflight.profile.profileDigest,
  };
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(STATE_MACHINE_DOMAIN, source.stateMachine);
  const policyDigest = domainDigest(POLICY_DOMAIN, {
    preflightBinding,
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
    errorPrecedence: source.profile.errorPrecedence,
    transitionMatrix: source.profile.transitionMatrix,
  });
  const profileDigest = domainDigest(PROFILE_DOMAIN, {
    registryId: source.registryId,
    preflightBinding,
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
    preflightBinding,
    profile: { profileDigest, spec: source.profile },
    stateMachine: source.stateMachine,
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
  };
  const generated = { ...body, registryDigest: domainDigest(REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeRunnerLedgerConsumerRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertRunnerLedgerConsumerRegistryCurrent(root: string): void {
  const expected = serializeRunnerLedgerConsumerRegistry(buildRunnerLedgerConsumerRegistry(root));
  const actual = readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${RUNNER_LEDGER_CONSUMER_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function runnerLedgerConsumerRegistryInputs(_root: string): string[] {
  return [
    RUNNER_LEDGER_CONSUMER_SOURCE_PATH,
    RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    "docs/plan/adr/0020-p1-runner-ledger-consumer-contract.md",
    "docs/plan/p1/runner-ledger-consumer-entry-blocker-20260821.md",
    "scripts/generate-platform-runner-ledger-consumer-registry.ts",
    "scripts/lib/platform-json-semantics.ts",
    "scripts/lib/platform-runner-ledger-consumer-registry.test.ts",
    "scripts/lib/platform-runner-ledger-consumer-registry.ts",
    "scripts/lib/platform-runner-ledger-preflight-registry.ts",
  ].toSorted();
}

export function validateRunnerLedgerConsumerFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document)) return success();
  if (document.formatVersion === SOURCE_FORMAT) {
    try {
      validateRunnerLedgerConsumerSource(root, document as RegistrySource);
      validatePreflightPairBinding(
        document as RegistrySource,
        buildRunnerLedgerPreflightRegistry(root) as PreflightRegistry,
      );
      return success();
    } catch (error) {
      return failure(
        error instanceof RunnerLedgerConsumerContractError
          ? error.code
          : "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
        error instanceof RunnerLedgerConsumerContractError ? error.path : "/",
      );
    }
  }
  if (document.formatVersion !== OUTPUT_FORMAT) return success();
  try {
    const expected = buildRunnerLedgerConsumerRegistry(root);
    if (!canonicalEqual(document, expected)) {
      return failure("RUNNER_LEDGER_CONSUMER_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
    }
    return success();
  } catch {
    return failure("RUNNER_LEDGER_CONSUMER_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  }
}

export function validateRunnerLedgerConsumerSource(root: string, source: RegistrySource): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (source.formatVersion !== SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/formatVersion",
      "Runner ledger consumer source identity drifted.",
    );
  }
  if (
    source.profile.profileId !== PROFILE_ID ||
    source.profile.stateMachineId !== STATE_MACHINE_ID
  ) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/profile",
      "Runner ledger consumer profile must bind its generated state machine.",
    );
  }
  validateStateMachine(source.stateMachine);
  if (!canonicalEqual(source.profile.transitionMatrix, RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX)) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Runner ledger consumer transition matrix drifted.",
    );
  }
  const expectedSelector = {
    mode: "generated_registry_only",
    profileSelection: "exact_profile_id_and_digest",
    preflightProfileSelection: "exact_runner_ledger_preflight_v1_generated_identity",
    callerProvidedProfile: "forbidden",
    callerProvidedDispatch: "forbidden",
    ordinaryDispatchAsWriterAuthority: "forbidden",
    noOpSource: "consumed_same_verifier_claim_only",
  };
  if (!canonicalEqual(source.selector, expectedSelector)) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/selector",
      "Runner ledger consumer selector must accept only a consumed same-verifier claim.",
    );
  }
  const expectedBoundary = {
    runnerConsumer: "complete_return_success_noop_only",
    existingBrandNewWriter: "separate_existing_authority_chain",
    entryWriter: "not_implemented",
    recoveryWriter: "not_implemented",
    databaseSession: "read_only_preflight_closed_before_result",
    databaseTransaction: "forbidden",
    ledgerMutation: "forbidden",
    evidenceMutation: "forbidden",
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
      "RUNNER_LEDGER_CONSUMER_BOUNDARY_MISMATCH",
      "/implementationBoundary",
      "Runner ledger consumer implementation boundary drifted.",
    );
  }
}

function validatePreflightPairBinding(source: RegistrySource, preflight: PreflightRegistry): void {
  if (
    preflight.registryId !== "cloud-agents/platform/runner-ledger-preflight" ||
    preflight.profile.spec.profileId !== "runner-ledger-preflight/v1"
  ) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/preflightBinding",
      "Runner ledger consumer must bind the immutable preflight v1 identity.",
    );
  }
  const consumerPairs = Object.fromEntries(
    Object.entries(source.profile.transitionMatrix).map(([disposition, pairs]) => [
      disposition,
      pairs.map(({ state, action }) => ({ state, action })),
    ]),
  );
  if (!canonicalEqual(consumerPairs, preflight.profile.spec.recoveryDispositionMatrix)) {
    throw contractError(
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
      "/profile/transitionMatrix",
      "Runner ledger consumer pairs must exactly cover immutable preflight v1.",
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
      "RUNNER_LEDGER_CONSUMER_STATE_MACHINE_INVALID",
      "/stateMachine",
      "Runner ledger consumer state machine must be closed, sorted, and deterministic.",
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
      "RUNNER_LEDGER_CONSUMER_STATE_MACHINE_INVALID",
      "/stateMachine/states",
      "Every runner ledger consumer state must be reachable.",
    );
  }
}

function readSource(root: string): RegistrySource {
  return parseJsonFile(resolve(root, RUNNER_LEDGER_CONSUMER_SOURCE_PATH)) as RegistrySource;
}

function validateAgainstSchema(root: string, schemaID: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaID);
  if (!validate) throw new Error(`Runner ledger consumer schema ${schemaID} was not registered.`);
  if (!validate(value)) {
    const paths = (validate.errors ?? []).map((error) => error.instancePath);
    const code: SemanticErrorCode = paths.every((path) => path.startsWith("/stateMachine"))
      ? "RUNNER_LEDGER_CONSUMER_STATE_MACHINE_INVALID"
      : paths.every((path) => path.startsWith("/implementationBoundary"))
        ? "RUNNER_LEDGER_CONSUMER_BOUNDARY_MISMATCH"
        : "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH";
    throw contractError(
      code,
      "/",
      `Runner ledger consumer schema validation failed: ${ajv.errorsText(validate.errors)}.`,
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
): RunnerLedgerConsumerContractError {
  return new RunnerLedgerConsumerContractError(code, path, message);
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
