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

export const RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json";
export const RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json";

const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-runner-ledger-preflight-source/v1";
const OUTPUT_FORMAT = "cloud-agents-runner-ledger-preflight-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/runner-ledger-preflight";
const SOURCE_DOMAIN = "cloud-agents/runner-ledger-preflight/source/v1";
const PROFILE_DOMAIN = "cloud-agents/runner-ledger-preflight/profile/v1";
const STATE_MACHINE_DOMAIN = "cloud-agents/runner-ledger-preflight/state-machine/v1";
const POLICY_DOMAIN = "cloud-agents/runner-ledger-preflight/policy/v1";
const REGISTRY_DOMAIN = "cloud-agents/runner-ledger-preflight/registry/v1";

const PROFILE_ID = "runner-ledger-preflight/v1";
const STATE_MACHINE_ID = PROFILE_ID;
const STATES = [
  "complete_return_success",
  "empty_brand_new",
  "partial_next_entry",
  "partial_retry_or_recovery",
  "unclassified",
  "unknown_or_failed",
] as const;
const TERMINAL_STATES = STATES.filter((state) => state !== "unclassified");
const TRANSITIONS = [
  { from: "unclassified", event: "observe_complete", to: "complete_return_success" },
  { from: "unclassified", event: "observe_empty", to: "empty_brand_new" },
  {
    from: "unclassified",
    event: "observe_partial_next_entry",
    to: "partial_next_entry",
  },
  {
    from: "unclassified",
    event: "observe_partial_retry_or_recovery",
    to: "partial_retry_or_recovery",
  },
  {
    from: "unclassified",
    event: "observe_unknown_or_failed",
    to: "unknown_or_failed",
  },
] as const;

const RECOVERY_DISPOSITION_MATRIX = {
  complete_return_success: [{ state: "completed", action: "return_success" }],
  empty_brand_new: [
    { state: "brand_new", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_next_attempt" },
  ],
  partial_next_entry: [
    { state: "brand_new_inherited", action: "begin_first_attempt_next_entry" },
    { state: "terminal", action: "begin_first_attempt_next_entry" },
  ],
  partial_retry_or_recovery: [
    { state: "brand_new_inherited", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_next_attempt" },
    { state: "dangling_statement_intent", action: "append_aborted_retryable" },
    { state: "dangling_statement_intent", action: "append_aborted_terminal" },
    { state: "dangling_intermediate", action: "append_aborted_retryable" },
    { state: "dangling_intermediate", action: "append_aborted_terminal" },
    { state: "dangling_commit_intent", action: "reconcile_commit" },
    { state: "ambiguous_unresolved", action: "reconcile_commit" },
    { state: "terminal", action: "begin_next_attempt" },
    { state: "terminal", action: "return_failure" },
    { state: "divergent", action: "return_failure" },
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
    readonly recoveryDispositionMatrix: typeof RECOVERY_DISPOSITION_MATRIX;
  };
  readonly stateMachine: StateMachine;
  readonly selector: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

export class RunnerLedgerPreflightContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "RunnerLedgerPreflightContractError";
  }
}

export function buildRunnerLedgerPreflightRegistry(root: string): JsonRecord {
  const source = readSource(root);
  validateRunnerLedgerPreflightSource(root, source);
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(STATE_MACHINE_DOMAIN, source.stateMachine);
  const policyDigest = domainDigest(POLICY_DOMAIN, {
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
    errorPrecedence: source.profile.errorPrecedence,
    recoveryDispositionMatrix: source.profile.recoveryDispositionMatrix,
  });
  const profileDigest = domainDigest(PROFILE_DOMAIN, {
    registryId: source.registryId,
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
    profile: { profileDigest, spec: source.profile },
    stateMachine: source.stateMachine,
    selector: source.selector,
    implementationBoundary: source.implementationBoundary,
  };
  const generated = { ...body, registryDigest: domainDigest(REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeRunnerLedgerPreflightRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertRunnerLedgerPreflightRegistryCurrent(root: string): void {
  const expected = serializeRunnerLedgerPreflightRegistry(buildRunnerLedgerPreflightRegistry(root));
  const actual = readFileSync(resolve(root, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw contractError(
      "RUNNER_LEDGER_PREFLIGHT_REGISTRY_DIGEST_MISMATCH",
      "/registryDigest",
      `${RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

export function runnerLedgerPreflightRegistryInputs(_root: string): string[] {
  return [
    "docs/plan/adr/0019-p1-runner-ledger-preflight-contract.md",
    "docs/plan/p1/migration-ledger-preflight-entry-blocker-20260821.md",
    RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
    "scripts/generate-platform-runner-ledger-preflight-registry.ts",
    "scripts/lib/platform-runner-ledger-preflight-registry.test.ts",
    "scripts/lib/platform-runner-ledger-preflight-registry.ts",
    "scripts/lib/platform-json-semantics.ts",
  ].toSorted();
}

export function validateRunnerLedgerPreflightFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document)) return success();
  if (document.formatVersion === SOURCE_FORMAT) {
    try {
      validateRunnerLedgerPreflightSource(root, document as RegistrySource);
      return success();
    } catch (error) {
      return failure(
        error instanceof RunnerLedgerPreflightContractError
          ? error.code
          : "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
        error instanceof RunnerLedgerPreflightContractError ? error.path : "/",
      );
    }
  }
  if (document.formatVersion !== OUTPUT_FORMAT) return success();
  try {
    const expected = buildRunnerLedgerPreflightRegistry(root);
    if (!canonicalEqual(document, expected)) {
      return failure("RUNNER_LEDGER_PREFLIGHT_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
    }
    return success();
  } catch {
    return failure("RUNNER_LEDGER_PREFLIGHT_REGISTRY_DIGEST_MISMATCH", "/registryDigest");
  }
}

export function validateRunnerLedgerPreflightSource(root: string, source: RegistrySource): void {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  if (source.formatVersion !== SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw contractError(
      "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
      "/formatVersion",
      "Runner ledger preflight source identity drifted.",
    );
  }
  if (
    source.profile.profileId !== PROFILE_ID ||
    source.profile.stateMachineId !== STATE_MACHINE_ID
  ) {
    throw contractError(
      "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
      "/profile",
      "Runner ledger preflight profile must bind its generated state machine.",
    );
  }
  validateStateMachine(source.stateMachine);
  if (!canonicalEqual(source.profile.recoveryDispositionMatrix, RECOVERY_DISPOSITION_MATRIX)) {
    throw contractError(
      "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
      "/profile/recoveryDispositionMatrix",
      "Runner ledger preflight recovery state/action matrix drifted.",
    );
  }
  const expectedSelector = {
    mode: "generated_registry_only",
    profileSelection: "exact_profile_id_and_digest",
    callerProvidedProfile: "forbidden",
    guessedMigrationIdentity: "forbidden",
    lossyIdentityMapping: "forbidden",
  };
  if (!canonicalEqual(source.selector, expectedSelector)) {
    throw contractError(
      "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
      "/selector",
      "Runner ledger preflight selector must not accept caller-selected or lossy identity input.",
    );
  }
  const expectedBoundary = {
    runnerConsumer: "not_implemented",
    databaseSession: "none",
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
      "RUNNER_LEDGER_PREFLIGHT_BOUNDARY_MISMATCH",
      "/implementationBoundary",
      "Runner ledger preflight contract boundary drifted.",
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
      "RUNNER_LEDGER_PREFLIGHT_STATE_MACHINE_INVALID",
      "/stateMachine",
      "Runner ledger preflight state machine must be closed, sorted, and deterministic.",
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
      "RUNNER_LEDGER_PREFLIGHT_STATE_MACHINE_INVALID",
      "/stateMachine/states",
      "Every runner ledger preflight state must be reachable.",
    );
  }
}

function readSource(root: string): RegistrySource {
  return parseJsonFile(resolve(root, RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH)) as RegistrySource;
}

function validateAgainstSchema(root: string, schemaID: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const path of [SOURCE_SCHEMA_PATH, OUTPUT_SCHEMA_PATH]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaID);
  if (!validate) throw new Error(`Runner ledger preflight schema ${schemaID} was not registered.`);
  if (!validate(value)) {
    const paths = (validate.errors ?? []).map((error) => error.instancePath);
    const code: SemanticErrorCode = paths.every((path) => path.startsWith("/stateMachine"))
      ? "RUNNER_LEDGER_PREFLIGHT_STATE_MACHINE_INVALID"
      : paths.every((path) => path.startsWith("/implementationBoundary"))
        ? "RUNNER_LEDGER_PREFLIGHT_BOUNDARY_MISMATCH"
        : "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH";
    throw contractError(
      code,
      "/",
      `Runner ledger preflight schema validation failed: ${ajv.errorsText(validate.errors)}.`,
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
): RunnerLedgerPreflightContractError {
  return new RunnerLedgerPreflightContractError(code, path, message);
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
