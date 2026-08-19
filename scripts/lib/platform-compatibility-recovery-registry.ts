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

export const COMPATIBILITY_RECOVERY_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json";
export const COMPATIBILITY_RECOVERY_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json";
const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json";
const ADR_PATH = "docs/plan/adr/0015-p1-compatibility-recovery-contract.md";
const GENERATOR_PATH = "scripts/generate-platform-compatibility-recovery-registry.ts";
const LIBRARY_PATH = "scripts/lib/platform-compatibility-recovery-registry.ts";
const TEST_PATH = "scripts/lib/platform-compatibility-recovery-registry.test.ts";
const JSON_SEMANTICS_PATH = "scripts/lib/platform-json-semantics.ts";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json";
const SOURCE_FORMAT = "cloud-agents-compatibility-recovery-source/v1";
const OUTPUT_FORMAT = "cloud-agents-compatibility-recovery-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/compatibility-recovery";
const SOURCE_DOMAIN = "cloud-agents/compatibility-recovery/source/v1";
const PROFILE_DOMAIN = "cloud-agents/compatibility-recovery/profile/v1";
const STATE_MACHINE_DOMAIN = "cloud-agents/compatibility-recovery/state-machines/v1";
const POLICY_DOMAIN = "cloud-agents/compatibility-recovery/policies/v1";
const REGISTRY_DOMAIN = "cloud-agents/compatibility-recovery/registry/v1";
const PROFILE_IDS = [
  "backfill/v1",
  "live-instance/v1",
  "migration-preflight/v1",
  "restore-evidence/v1",
  "retirement-receipt/v1",
] as const;
const STATE_MACHINE_IDS = PROFILE_IDS;
const PROFILE_KINDS = [
  "backfill",
  "live_instance",
  "migration_preflight",
  "restore_evidence",
  "retirement_receipt",
] as const;
const PREFLIGHT_REQUIRED_EVIDENCE = [
  "ledger_checksum",
  "live_instances",
  "postgres_major",
  "restore_evidence",
  "rollback_target",
  "target_bundle_and_phase",
] as const;
const EXPIRED_REGISTRATION_IDENTITY_FIELDS = [
  "incarnation",
  "instance_id",
  "rollout_generation",
] as const;
const EXPIRED_REGISTRATION_RECEIPT_FACTS = [
  "claim_released",
  "credential_revoked",
  "endpoint_revoked",
  "generation_fenced",
  "leader_released",
  "process_terminated",
] as const;

type StateTransition = { readonly from: string; readonly event: string; readonly to: string };
type StateMachine = {
  readonly id: string;
  readonly initialState: string;
  readonly states: ReadonlyArray<string>;
  readonly terminalStates: ReadonlyArray<string>;
  readonly transitions: ReadonlyArray<StateTransition>;
};
type CompatibilityProfile = {
  readonly profileId: string;
  readonly kind: string;
  readonly stateMachineId: string;
  readonly persistedFields: ReadonlyArray<string>;
  readonly requiredEvidence: ReadonlyArray<string>;
  readonly rules: ReadonlyArray<string>;
};
type RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly schemaRange: {
    readonly minInclusive: string;
    readonly maxInclusive: string;
    readonly comparison: string;
  };
  readonly profiles: ReadonlyArray<CompatibilityProfile>;
  readonly stateMachines: ReadonlyArray<StateMachine>;
  readonly policies: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

export class CompatibilityRecoveryContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "CompatibilityRecoveryContractError";
  }
}

export function buildCompatibilityRecoveryRegistry(root: string): JsonRecord {
  const source = readSource(root);
  validateCompatibilityRecoverySource(root, source);
  const sourceDigest = domainDigest(SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(STATE_MACHINE_DOMAIN, source.stateMachines);
  const policyDigest = domainDigest(POLICY_DOMAIN, source.policies);
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    registryId: REGISTRY_ID,
    sourceDigest,
    stateMachineDigest,
    policyDigest,
    schemaRange: source.schemaRange,
    profiles: source.profiles.map((profile) => ({
      profileDigest: domainDigest(PROFILE_DOMAIN, {
        registryId: REGISTRY_ID,
        schemaRange: source.schemaRange,
        stateMachineDigest,
        policyDigest,
        profile,
      }),
      spec: profile,
    })),
    stateMachines: source.stateMachines,
    policies: source.policies,
    implementationBoundary: source.implementationBoundary,
  };
  const generated = { ...body, registryDigest: domainDigest(REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeCompatibilityRecoveryRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertCompatibilityRecoveryRegistryCurrent(root: string): void {
  const expected = serializeCompatibilityRecoveryRegistry(buildCompatibilityRecoveryRegistry(root));
  const actual = readFileSync(resolve(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(
      `${COMPATIBILITY_RECOVERY_OUTPUT_PATH} is stale; run bun ${GENERATOR_PATH} --write.`,
    );
  }
}

export function compatibilityRecoveryRegistryInputs(root: string): string[] {
  const inputs = [
    ADR_PATH,
    COMPATIBILITY_RECOVERY_SOURCE_PATH,
    GENERATOR_PATH,
    JSON_SEMANTICS_PATH,
    LIBRARY_PATH,
    OUTPUT_SCHEMA_PATH,
    SOURCE_SCHEMA_PATH,
    TEST_PATH,
  ].toSorted();
  for (const path of inputs) {
    const stat = lstatSync(resolve(root, path));
    if (!stat.isFile() || stat.isSymbolicLink())
      throw new Error(`Compatibility recovery input is not a regular file: ${path}`);
  }
  return inputs;
}

export function validateCompatibilityRecoveryFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document) || typeof document.formatVersion !== "string")
    return { valid: true, errors: [] };
  try {
    if (document.formatVersion === SOURCE_FORMAT)
      validateCompatibilityRecoverySource(root, document as RegistrySource);
    if (document.formatVersion === OUTPUT_FORMAT) {
      const expected = buildCompatibilityRecoveryRegistry(root);
      if (!canonicalEqual(document, expected)) {
        throw contractError(
          "COMPATIBILITY_RECOVERY_REGISTRY_DIGEST_MISMATCH",
          "/registryDigest",
          "Generated compatibility recovery registry does not match source inputs.",
        );
      }
    }
    return { valid: true, errors: [] };
  } catch (error) {
    if (error instanceof CompatibilityRecoveryContractError)
      return { valid: false, errors: [{ code: error.code, path: error.path }] };
    throw error;
  }
}

export function validateCompatibilityRecoverySource(
  root: string,
  value: unknown,
): CompatibilityProfile[] {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, value);
  const source = requireRecord(value, "/") as RegistrySource;
  if (source.formatVersion !== SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/formatVersion",
      "Compatibility recovery source identity is not recognized.",
    );
  }
  if (source.schemaRange.minInclusive > source.schemaRange.maxInclusive) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/schemaRange",
      "Schema range is inverted.",
    );
  }
  if (source.schemaRange.minInclusive === "000000") {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/schemaRange/minInclusive",
      "Schema range cannot include the zero migration identity.",
    );
  }
  assertSortedUnique(
    source.profiles.map((profile) => profile.profileId),
    "/profiles",
  );
  if (
    !canonicalEqual(
      source.profiles.map((profile) => profile.profileId),
      PROFILE_IDS,
    )
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/profiles",
      "A2.4 profile catalog drifted from the approved five profiles.",
    );
  }
  validateStateMachines(source.stateMachines);
  for (const [index, profile] of source.profiles.entries()) {
    if (profile.stateMachineId !== profile.profileId || profile.kind !== PROFILE_KINDS[index]) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
        `/profiles/${index}/stateMachineId`,
        "Each profile must bind its same-id state machine.",
      );
    }
    assertSortedUnique(profile.persistedFields, `/profiles/${index}/persistedFields`);
    assertSortedUnique(profile.requiredEvidence, `/profiles/${index}/requiredEvidence`);
    assertSortedUnique(profile.rules, `/profiles/${index}/rules`);
  }
  const live = source.profiles.find((profile) => profile.profileId === "live-instance/v1");
  const retirement = source.profiles.find(
    (profile) => profile.profileId === "retirement-receipt/v1",
  );
  if (
    !live ||
    !retirement ||
    !live.persistedFields.includes("heartbeat_ttl_seconds") ||
    !live.requiredEvidence.includes("heartbeat_ttl_seconds") ||
    !retirement.requiredEvidence.includes("leader_released")
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/profiles",
      "Live-instance TTL or retirement proof is incomplete.",
    );
  }
  validatePolicies(source.policies);
  validateImplementationBoundary(source.implementationBoundary);
  return [...source.profiles];
}

function validateStateMachines(machines: ReadonlyArray<StateMachine>): void {
  assertSortedUnique(
    machines.map((machine) => machine.id),
    "/stateMachines",
  );
  if (
    !canonicalEqual(
      machines.map((machine) => machine.id),
      STATE_MACHINE_IDS,
    )
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
      "/stateMachines",
      "A2.4 state-machine catalog drifted from the approved five machines.",
    );
  }
  for (const [index, machine] of machines.entries()) {
    const path = `/stateMachines/${index}`;
    assertSortedUnique(machine.states, `${path}/states`);
    assertSortedUnique(machine.terminalStates, `${path}/terminalStates`);
    const states = new Set(machine.states);
    const terminals = new Set(machine.terminalStates);
    if (!states.has(machine.initialState) || [...terminals].some((state) => !states.has(state))) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
        path,
        "State machine references an unknown state.",
      );
    }
    const keys = machine.transitions.map(
      (transition) => `${transition.from}\0${transition.event}\0${transition.to}`,
    );
    assertSortedUnique(keys, `${path}/transitions`);
    const seen = new Set<string>();
    for (const [transitionIndex, transition] of machine.transitions.entries()) {
      if (
        !states.has(transition.from) ||
        !states.has(transition.to) ||
        terminals.has(transition.from)
      ) {
        throw contractError(
          "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
          `${path}/transitions/${transitionIndex}`,
          "Transition references unknown or terminal source state.",
        );
      }
      const key = `${transition.from}\0${transition.event}`;
      if (seen.has(key))
        throw contractError(
          "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
          `${path}/transitions/${transitionIndex}`,
          "State machine is nondeterministic.",
        );
      seen.add(key);
    }
    const reachable = new Set<string>([machine.initialState]);
    const pending = [machine.initialState];
    const outgoing = new Map<string, string[]>();
    const reverse = new Map<string, string[]>();
    for (const transition of machine.transitions) {
      outgoing.set(transition.from, [...(outgoing.get(transition.from) ?? []), transition.to]);
      reverse.set(transition.to, [...(reverse.get(transition.to) ?? []), transition.from]);
    }
    while (pending.length > 0) {
      const state = pending.shift() as string;
      for (const next of outgoing.get(state) ?? []) {
        if (!reachable.has(next)) {
          reachable.add(next);
          pending.push(next);
        }
      }
    }
    if (reachable.size !== states.size) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
        path,
        "Every state must be reachable from the initial state.",
      );
    }
    const canTerminate = new Set<string>(terminals);
    const terminalPending = [...terminals];
    while (terminalPending.length > 0) {
      const state = terminalPending.shift() as string;
      for (const previous of reverse.get(state) ?? []) {
        if (!canTerminate.has(previous)) {
          canTerminate.add(previous);
          terminalPending.push(previous);
        }
      }
    }
    if (canTerminate.size !== states.size) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
        path,
        "Every non-terminal state must have a path to a terminal state.",
      );
    }
  }
}

function validatePolicies(policies: JsonRecord): void {
  const preflight = requireRecord(policies.preflight, "/policies/preflight");
  const requiredEvidence = requireArray(
    preflight.requiredEvidence,
    "/policies/preflight/requiredEvidence",
  );
  assertStringArraySortedUnique(requiredEvidence, "/policies/preflight/requiredEvidence");
  if (!canonicalEqual(requiredEvidence, PREFLIGHT_REQUIRED_EVIDENCE)) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_POLICY_MISMATCH",
      "/policies/preflight/requiredEvidence",
      "Preflight evidence catalog drifted from the approved six facts.",
    );
  }
  const expired = requireRecord(policies.expiredRegistration, "/policies/expiredRegistration");
  const identityFields = requireArray(
    expired.identityFields,
    "/policies/expiredRegistration/identityFields",
  );
  assertStringArraySortedUnique(identityFields, "/policies/expiredRegistration/identityFields");
  if (!canonicalEqual(identityFields, EXPIRED_REGISTRATION_IDENTITY_FIELDS)) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_POLICY_MISMATCH",
      "/policies/expiredRegistration/identityFields",
      "Retirement identity catalog drifted from the approved three fields.",
    );
  }
  const facts = requireArray(
    expired.requiredReceiptFacts,
    "/policies/expiredRegistration/requiredReceiptFacts",
  );
  assertStringArraySortedUnique(facts, "/policies/expiredRegistration/requiredReceiptFacts");
  if (!canonicalEqual(facts, EXPIRED_REGISTRATION_RECEIPT_FACTS)) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_POLICY_MISMATCH",
      "/policies/expiredRegistration/requiredReceiptFacts",
      "Retirement proof catalog drifted from the approved six facts.",
    );
  }
  if (policies.failureMode !== "fail_closed")
    throw contractError(
      "COMPATIBILITY_RECOVERY_POLICY_MISMATCH",
      "/policies/failureMode",
      "Unknown compatibility evidence must fail closed.",
    );
}

function validateImplementationBoundary(boundary: JsonRecord): void {
  if (
    boundary.sqlMigration !== "not_implemented_no_000010" ||
    boundary.goConsumer !== "not_implemented" ||
    boundary.httpSurface !== "not_implemented" ||
    boundary.externalSideEffects !== "forbidden" ||
    boundary.gateStatus !== "non_gate_evidence_only"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BOUNDARY_MISMATCH",
      "/implementationBoundary",
      "A2.4 registry boundary must remain generated-contract-only.",
    );
  }
}

function domainDigest(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(new TextEncoder().encode(`${domain}\0`))
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}
function canonicalEqual(left: unknown, right: unknown): boolean {
  try {
    return (
      new TextDecoder().decode(canonicalizeJson(left)) ===
      new TextDecoder().decode(canonicalizeJson(right))
    );
  } catch {
    return false;
  }
}
function assertSortedUnique(values: ReadonlyArray<string>, path: string): void {
  const sorted = values.toSorted();
  if (!canonicalEqual(values, sorted) || new Set(values).size !== values.length)
    throw contractError(
      "COMPATIBILITY_RECOVERY_ORDER_MISMATCH",
      path,
      "Values must be sorted and unique.",
    );
}
function assertStringArraySortedUnique(values: ReadonlyArray<unknown>, path: string): void {
  if (!values.every((value): value is string => typeof value === "string")) {
    throw contractError("COMPATIBILITY_RECOVERY_SCHEMA_INVALID", path, "Expected string array.");
  }
  assertSortedUnique(values, path);
}
function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  ajv.addKeyword({ keyword: "x-cloud-agents-security", schemaType: "object" });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  const sourceSchema = JSON.parse(readFileSync(resolve(root, SOURCE_SCHEMA_PATH), "utf8"));
  const outputSchema = JSON.parse(readFileSync(resolve(root, OUTPUT_SCHEMA_PATH), "utf8"));
  ajv.addSchema(sourceSchema);
  ajv.addSchema(outputSchema);
  const validate = ajv.getSchema(schemaId);
  if (!validate || !validate(value))
    throw contractError(
      "COMPATIBILITY_RECOVERY_SCHEMA_INVALID",
      "/",
      `Compatibility recovery document violates ${schemaId}: ${ajv.errorsText(validate?.errors)}.`,
    );
}
function readSource(root: string): RegistrySource {
  const source = JSON.parse(
    readFileSync(resolve(root, COMPATIBILITY_RECOVERY_SOURCE_PATH), "utf8"),
  );
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  return source as RegistrySource;
}
function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function requireRecord(value: unknown, path: string): JsonRecord {
  if (!isRecord(value))
    throw contractError("COMPATIBILITY_RECOVERY_SCHEMA_INVALID", path, "Expected object.");
  return value;
}
function requireArray(value: unknown, path: string): unknown[] {
  if (!Array.isArray(value))
    throw contractError("COMPATIBILITY_RECOVERY_SCHEMA_INVALID", path, "Expected array.");
  return value;
}
function contractError(
  code: SemanticErrorCode,
  path: string,
  message: string,
): CompatibilityRecoveryContractError {
  return new CompatibilityRecoveryContractError(code, path, message);
}
