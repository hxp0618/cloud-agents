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
export const COMPATIBILITY_RECOVERY_V2_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json";
export const COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json";
const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json";
const V2_SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v2.schema.json";
const V2_OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v2.schema.json";
const ADR_PATH = "docs/plan/adr/0015-p1-compatibility-recovery-contract.md";
const V2_ADR_PATH = "docs/plan/adr/0017-p1-compatibility-recovery-v2-registry.md";
const V2_BLOCKER_PATH = "docs/plan/p1/compatibility-recovery-service-entry-blocker-20260820.md";
const V2_SCHEMA_CATALOG_PATH = "services/control-plane/migrations/catalog/schema-000010.json";
const V2_SCHEMA_MIGRATION_PATH =
  "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql";
const GENERATOR_PATH = "scripts/generate-platform-compatibility-recovery-registry.ts";
const LIBRARY_PATH = "scripts/lib/platform-compatibility-recovery-registry.ts";
const TEST_PATH = "scripts/lib/platform-compatibility-recovery-registry.test.ts";
const JSON_SEMANTICS_PATH = "scripts/lib/platform-json-semantics.ts";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json";
const V2_SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v2.schema.json";
const V2_OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/compatibility-recovery-registry-v2.schema.json";
const SOURCE_FORMAT = "cloud-agents-compatibility-recovery-source/v1";
const OUTPUT_FORMAT = "cloud-agents-compatibility-recovery-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/compatibility-recovery";
const SOURCE_DOMAIN = "cloud-agents/compatibility-recovery/source/v1";
const PROFILE_DOMAIN = "cloud-agents/compatibility-recovery/profile/v1";
const STATE_MACHINE_DOMAIN = "cloud-agents/compatibility-recovery/state-machines/v1";
const POLICY_DOMAIN = "cloud-agents/compatibility-recovery/policies/v1";
const REGISTRY_DOMAIN = "cloud-agents/compatibility-recovery/registry/v1";
const V2_SOURCE_FORMAT = "cloud-agents-compatibility-recovery-source/v2";
const V2_OUTPUT_FORMAT = "cloud-agents-compatibility-recovery-registry/v2";
const V2_SOURCE_DOMAIN = "cloud-agents/compatibility-recovery/source/v2";
const V2_PROFILE_DOMAIN = "cloud-agents/compatibility-recovery/profile/v2";
const V2_STATE_MACHINE_DOMAIN = "cloud-agents/compatibility-recovery/state-machines/v2";
const V2_POLICY_DOMAIN = "cloud-agents/compatibility-recovery/policies/v2";
const V2_REGISTRY_DOMAIN = "cloud-agents/compatibility-recovery/registry/v2";
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

type V2Operation = {
  readonly operationId: string;
  readonly sqlFunction: string;
  readonly serviceMethod: string;
  readonly mode: "read_only" | "mutation";
  readonly capability: string;
  readonly unknownOutcome: "not_applicable" | "reconcile_required_no_write_retry";
};
type V2Profile = CompatibilityProfile & {
  readonly accessMode: "read_only" | "mutation";
  readonly operations: ReadonlyArray<V2Operation>;
};
type V2RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly schemaRange: RegistrySource["schemaRange"];
  readonly schemaBinding: {
    readonly schemaHead: string;
    readonly schemaCatalogSha256: string;
    readonly schemaMigrationSha256: string;
  };
  readonly historicalCompatibility: {
    readonly priorFormatVersion: string;
    readonly priorRegistryDigest: string;
    readonly mode: string;
  };
  readonly selector: JsonRecord;
  readonly profiles: ReadonlyArray<V2Profile>;
  readonly stateMachines: ReadonlyArray<StateMachine>;
  readonly policies: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

const V2_PROFILE_IDS = [
  "backfill/v2",
  "live-instance/v2",
  "migration-preflight/v2",
  "restore-evidence/v2",
  "retirement-receipt/v2",
  "workload-principal/v2",
] as const;
const V2_PROFILE_KINDS = [
  "backfill",
  "live_instance",
  "migration_preflight",
  "restore_evidence",
  "retirement_receipt",
  "workload_principal",
] as const;
const V2_SCHEMA_CATALOG_SHA256 =
  "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236";
const V2_SCHEMA_MIGRATION_SHA256 =
  "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6";
const V1_REGISTRY_DIGEST =
  "sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d";

const V2_OPERATION_CATALOG: Record<string, ReadonlyArray<V2Operation>> = {
  "backfill/v2": [
    {
      operationId: "backfill-acquire-lease/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_acquire_lease_v2",
      serviceMethod: "AcquireBackfillLease",
      mode: "mutation",
      capability: "compatibility_recovery.backfill.acquire_lease",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "backfill-advance/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_advance_v2",
      serviceMethod: "AdvanceBackfill",
      mode: "mutation",
      capability: "compatibility_recovery.backfill.advance",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "backfill-complete/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_complete_v2",
      serviceMethod: "CompleteBackfill",
      mode: "mutation",
      capability: "compatibility_recovery.backfill.complete",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "backfill-heartbeat/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_heartbeat_v2",
      serviceMethod: "HeartbeatBackfill",
      mode: "mutation",
      capability: "compatibility_recovery.backfill.heartbeat",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "backfill-reconcile/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_reconcile_v2",
      serviceMethod: "ReconcileBackfill",
      mode: "read_only",
      capability: "compatibility_recovery.backfill.reconcile",
      unknownOutcome: "not_applicable",
    },
    {
      operationId: "backfill-start/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_backfill_start_v2",
      serviceMethod: "StartBackfill",
      mode: "mutation",
      capability: "compatibility_recovery.backfill.start",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
  ],
  "live-instance/v2": [
    {
      operationId: "live-instance-activate/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_activate_v2",
      serviceMethod: "ActivateLiveInstance",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.activate",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "live-instance-begin-drain/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_begin_drain_v2",
      serviceMethod: "BeginLiveInstanceDrain",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.begin_drain",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "live-instance-fence/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_fence_v2",
      serviceMethod: "FenceLiveInstance",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.fence",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "live-instance-finish-drain/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_finish_drain_v2",
      serviceMethod: "FinishLiveInstanceDrain",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.finish_drain",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "live-instance-heartbeat/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_heartbeat_v2",
      serviceMethod: "HeartbeatLiveInstance",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.heartbeat",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "live-instance-reconcile/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_reconcile_v2",
      serviceMethod: "ReconcileLiveInstance",
      mode: "read_only",
      capability: "compatibility_recovery.live_instance.reconcile",
      unknownOutcome: "not_applicable",
    },
    {
      operationId: "live-instance-register/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_live_instance_register_v2",
      serviceMethod: "RegisterLiveInstance",
      mode: "mutation",
      capability: "compatibility_recovery.live_instance.register",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
  ],
  "migration-preflight/v2": [
    {
      operationId: "migration-preflight-evaluate/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2",
      serviceMethod: "EvaluateMigrationPreflight",
      mode: "read_only",
      capability: "compatibility_recovery.migration_preflight.evaluate",
      unknownOutcome: "not_applicable",
    },
  ],
  "restore-evidence/v2": [
    {
      operationId: "restore-evidence-complete/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_restore_evidence_complete_v2",
      serviceMethod: "CompleteRestoreEvidence",
      mode: "mutation",
      capability: "compatibility_recovery.restore_evidence.complete",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "restore-evidence-reconcile/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_restore_evidence_reconcile_v2",
      serviceMethod: "ReconcileRestoreEvidence",
      mode: "read_only",
      capability: "compatibility_recovery.restore_evidence.reconcile",
      unknownOutcome: "not_applicable",
    },
    {
      operationId: "restore-evidence-record/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_restore_evidence_record_v2",
      serviceMethod: "RecordRestoreEvidence",
      mode: "mutation",
      capability: "compatibility_recovery.restore_evidence.record",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "restore-evidence-reject/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_restore_evidence_reject_v2",
      serviceMethod: "RejectRestoreEvidence",
      mode: "mutation",
      capability: "compatibility_recovery.restore_evidence.reject",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
  ],
  "retirement-receipt/v2": [
    {
      operationId: "retirement-receipt-collect/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_retirement_receipt_collect_v2",
      serviceMethod: "CollectRetirementReceipt",
      mode: "mutation",
      capability: "compatibility_recovery.retirement_receipt.collect",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "retirement-receipt-complete/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_retirement_receipt_complete_v2",
      serviceMethod: "CompleteRetirementReceipt",
      mode: "mutation",
      capability: "compatibility_recovery.retirement_receipt.complete",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "retirement-receipt-reconcile/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_retirement_receipt_reconcile_v2",
      serviceMethod: "ReconcileRetirementReceipt",
      mode: "read_only",
      capability: "compatibility_recovery.retirement_receipt.reconcile",
      unknownOutcome: "not_applicable",
    },
    {
      operationId: "retirement-receipt-reject/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_retirement_receipt_reject_v2",
      serviceMethod: "RejectRetirementReceipt",
      mode: "mutation",
      capability: "compatibility_recovery.retirement_receipt.reject",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
  ],
  "workload-principal/v2": [
    {
      operationId: "workload-principal-reconcile/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_workload_principal_reconcile_v2",
      serviceMethod: "ReconcileWorkloadPrincipal",
      mode: "read_only",
      capability: "compatibility_recovery.workload_principal.reconcile",
      unknownOutcome: "not_applicable",
    },
    {
      operationId: "workload-principal-register/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_workload_principal_register_v2",
      serviceMethod: "RegisterWorkloadPrincipal",
      mode: "mutation",
      capability: "compatibility_recovery.workload_principal.register",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "workload-principal-revoke/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_workload_principal_revoke_v2",
      serviceMethod: "RevokeWorkloadPrincipal",
      mode: "mutation",
      capability: "compatibility_recovery.workload_principal.revoke",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
    {
      operationId: "workload-principal-rotate/v2",
      sqlFunction: "cloud_agents.compatibility_recovery_workload_principal_rotate_v2",
      serviceMethod: "RotateWorkloadPrincipal",
      mode: "mutation",
      capability: "compatibility_recovery.workload_principal.rotate",
      unknownOutcome: "reconcile_required_no_write_retry",
    },
  ],
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

/**
 * Build the versioned A2.4 contract-only registry.  This is deliberately a
 * separate entry point: the v1 builder is consumed by the historical
 * migration bundle and must remain byte-for-byte stable.
 */
export function buildCompatibilityRecoveryRegistryV2(root: string): JsonRecord {
  const source = readSourceV2(root);
  validateCompatibilityRecoverySourceV2(root, source);
  const historicalRegistry = buildCompatibilityRecoveryRegistry(root);
  const historicalBytes = serializeCompatibilityRecoveryRegistry(historicalRegistry);
  if (
    historicalRegistry.registryDigest !== V1_REGISTRY_DIGEST ||
    readFileSync(resolve(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH), "utf8") !== historicalBytes
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/historicalCompatibility/priorRegistryDigest",
      "Compatibility recovery v2 must bind the exact same-bits generated v1 registry.",
    );
  }
  const sourceDigest = domainDigest(V2_SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(V2_STATE_MACHINE_DOMAIN, source.stateMachines);
  const policyDigest = domainDigest(V2_POLICY_DOMAIN, source.policies);
  const body: JsonRecord = {
    formatVersion: V2_OUTPUT_FORMAT,
    registryId: REGISTRY_ID,
    sourceDigest,
    stateMachineDigest,
    policyDigest,
    schemaBinding: source.schemaBinding,
    historicalCompatibility: source.historicalCompatibility,
    selector: source.selector,
    schemaRange: source.schemaRange,
    profiles: source.profiles.map((profile) => ({
      profileDigest: domainDigest(V2_PROFILE_DOMAIN, {
        registryId: REGISTRY_ID,
        schemaRange: source.schemaRange,
        schemaBinding: source.schemaBinding,
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
  const generated = { ...body, registryDigest: domainDigest(V2_REGISTRY_DOMAIN, body) };
  validateAgainstSchema(root, V2_OUTPUT_SCHEMA_ID, generated);
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

export function serializeCompatibilityRecoveryRegistryV2(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertCompatibilityRecoveryRegistryV2Current(root: string): void {
  const expected = serializeCompatibilityRecoveryRegistryV2(
    buildCompatibilityRecoveryRegistryV2(root),
  );
  const actual = readFileSync(resolve(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(
      `${COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH} is stale; run bun ${GENERATOR_PATH} --write.`,
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

export function compatibilityRecoveryRegistryV2Inputs(root: string): string[] {
  const inputs = [
    V2_ADR_PATH,
    V2_BLOCKER_PATH,
    COMPATIBILITY_RECOVERY_V2_SOURCE_PATH,
    V2_OUTPUT_SCHEMA_PATH,
    V2_SOURCE_SCHEMA_PATH,
    V2_SCHEMA_CATALOG_PATH,
    V2_SCHEMA_MIGRATION_PATH,
    GENERATOR_PATH,
    JSON_SEMANTICS_PATH,
    LIBRARY_PATH,
    TEST_PATH,
  ].toSorted();
  for (const path of inputs) {
    const stat = lstatSync(resolve(root, path));
    if (!stat.isFile() || stat.isSymbolicLink())
      throw new Error(`Compatibility recovery v2 input is not a regular file: ${path}`);
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
    if (document.formatVersion === V2_SOURCE_FORMAT)
      validateCompatibilityRecoverySourceV2(root, document as V2RegistrySource);
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
    if (document.formatVersion === V2_OUTPUT_FORMAT) {
      const expected = buildCompatibilityRecoveryRegistryV2(root);
      if (!canonicalEqual(document, expected)) {
        throw contractError(
          "COMPATIBILITY_RECOVERY_REGISTRY_DIGEST_MISMATCH",
          "/registryDigest",
          "Generated compatibility recovery v2 registry does not match source inputs.",
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

export function validateCompatibilityRecoverySourceV2(root: string, value: unknown): V2Profile[] {
  validateAgainstSchema(root, V2_SOURCE_SCHEMA_ID, value);
  const source = requireRecord(value, "/") as V2RegistrySource;
  if (source.formatVersion !== V2_SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/formatVersion",
      "Compatibility recovery v2 source identity is not recognized.",
    );
  }
  if (
    source.schemaRange.minInclusive !== "000001" ||
    source.schemaRange.maxInclusive !== "000010" ||
    source.schemaRange.comparison !== "zero-padded-migration-id-inclusive"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/schemaRange",
      "Compatibility recovery v2 must cover the exact 000010 schema head.",
    );
  }
  if (
    source.schemaBinding.schemaHead !== "000010" ||
    source.schemaBinding.schemaCatalogSha256 !== V2_SCHEMA_CATALOG_SHA256 ||
    source.schemaBinding.schemaMigrationSha256 !== V2_SCHEMA_MIGRATION_SHA256 ||
    source.schemaBinding.schemaCatalogSha256 !== fileSha256(root, V2_SCHEMA_CATALOG_PATH) ||
    source.schemaBinding.schemaMigrationSha256 !== fileSha256(root, V2_SCHEMA_MIGRATION_PATH)
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/schemaBinding",
      "Compatibility recovery v2 schema binding is not the approved 000010 input.",
    );
  }
  if (
    source.historicalCompatibility.priorFormatVersion !== SOURCE_FORMAT ||
    source.historicalCompatibility.priorRegistryDigest !== V1_REGISTRY_DIGEST ||
    source.historicalCompatibility.mode !== "historical_schema_only_non_authority"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/historicalCompatibility",
      "Compatibility recovery v2 must retain the exact v1 historical boundary.",
    );
  }
  if (
    source.selector.mode !== "generated_registry_only" ||
    source.selector.profileSelection !== "exact_profile_id_and_digest" ||
    source.selector.callerProvidedProfile !== "forbidden" ||
    source.selector.storedRowSelection !== "forbidden" ||
    source.selector.schemaBinding !== "exact_schema_head_catalog_and_migration_digest"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/selector",
      "Compatibility recovery v2 selector must be generated-only and exact.",
    );
  }
  assertSortedUnique(
    source.profiles.map((profile) => profile.profileId),
    "/profiles",
  );
  if (
    !canonicalEqual(
      source.profiles.map((profile) => profile.profileId),
      V2_PROFILE_IDS,
    )
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      "/profiles",
      "A2.4 v2 profile catalog drifted from the approved six profiles.",
    );
  }
  validateStateMachines(source.stateMachines, V2_PROFILE_IDS, "six");
  for (const [index, profile] of source.profiles.entries()) {
    if (profile.stateMachineId !== profile.profileId || profile.kind !== V2_PROFILE_KINDS[index]) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
        `/profiles/${index}/stateMachineId`,
        "Each v2 profile must bind its same-id state machine.",
      );
    }
    assertSortedUnique(profile.persistedFields, `/profiles/${index}/persistedFields`);
    assertSortedUnique(profile.requiredEvidence, `/profiles/${index}/requiredEvidence`);
    assertSortedUnique(profile.rules, `/profiles/${index}/rules`);
    validateV2Operations(profile.operations, index);
    if (
      profile.accessMode !==
      (profile.operations.some((op) => op.mode === "mutation") ? "mutation" : "read_only")
    ) {
      throw contractError(
        "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
        `/profiles/${index}/accessMode`,
        "A v2 profile access mode must match its operation catalog.",
      );
    }
  }
  validatePoliciesV2(source.policies);
  validateImplementationBoundaryV2(source.implementationBoundary);
  return [...source.profiles];
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

function validateStateMachines(
  machines: ReadonlyArray<StateMachine>,
  expectedIds: ReadonlyArray<string> = STATE_MACHINE_IDS,
  expectedCount = "five",
): void {
  assertSortedUnique(
    machines.map((machine) => machine.id),
    "/stateMachines",
  );
  if (
    !canonicalEqual(
      machines.map((machine) => machine.id),
      expectedIds,
    )
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_STATE_MACHINE_MISMATCH",
      "/stateMachines",
      `A2.4 state-machine catalog drifted from the approved ${expectedCount} machines.`,
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

function validateV2Operations(operations: ReadonlyArray<V2Operation>, profileIndex: number): void {
  const profileId = V2_PROFILE_IDS[profileIndex];
  const expected = V2_OPERATION_CATALOG[profileId];
  if (!expected || !canonicalEqual(operations, expected)) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BINDING_MISMATCH",
      `/profiles/${profileIndex}/operations`,
      "V2 operation identities must match the approved typed SQL and service catalog.",
    );
  }
  assertSortedUnique(
    operations.map((operation) => operation.operationId),
    `/profiles/${profileIndex}/operations`,
  );
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

function validatePoliciesV2(policies: JsonRecord): void {
  validatePolicies(policies);
  const mutation = requireRecord(policies.mutation, "/policies/mutation");
  if (
    mutation.timeSource !== "database_clock" ||
    mutation.unknownOutcome !== "reconcile_required_no_write_retry" ||
    mutation.lockOrder !== "tenant_profile_identity" ||
    mutation.externalCallWhileLocked !== "forbidden"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_POLICY_MISMATCH",
      "/policies/mutation",
      "V2 mutations must use database time, fixed lock order, and reconcile unknown outcomes.",
    );
  }
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

function validateImplementationBoundaryV2(boundary: JsonRecord): void {
  if (
    boundary.sqlWriterMigration !== "not_implemented_after_000010" ||
    boundary.goConsumer !== "not_implemented" ||
    boundary.httpSurface !== "not_implemented" ||
    boundary.externalSideEffects !== "forbidden" ||
    boundary.providerSideEffects !== "forbidden" ||
    boundary.productionDatabaseWrites !== "not_authorized" ||
    boundary.gateStatus !== "all_gates_open"
  ) {
    throw contractError(
      "COMPATIBILITY_RECOVERY_BOUNDARY_MISMATCH",
      "/implementationBoundary",
      "A2.4 v2 registry boundary must remain generated-contract-only.",
    );
  }
}

function domainDigest(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(new TextEncoder().encode(`${domain}\0`))
    .update(canonicalizeJson(value))
    .digest("hex")}`;
}
function fileSha256(root: string, path: string): string {
  return `sha256:${createHash("sha256")
    .update(readFileSync(resolve(root, path)))
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
  const sourceSchemaV2 = JSON.parse(readFileSync(resolve(root, V2_SOURCE_SCHEMA_PATH), "utf8"));
  const outputSchemaV2 = JSON.parse(readFileSync(resolve(root, V2_OUTPUT_SCHEMA_PATH), "utf8"));
  ajv.addSchema(sourceSchema);
  ajv.addSchema(outputSchema);
  ajv.addSchema(sourceSchemaV2);
  ajv.addSchema(outputSchemaV2);
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
function readSourceV2(root: string): V2RegistrySource {
  const source = JSON.parse(
    readFileSync(resolve(root, COMPATIBILITY_RECOVERY_V2_SOURCE_PATH), "utf8"),
  );
  validateAgainstSchema(root, V2_SOURCE_SCHEMA_ID, source);
  return source as V2RegistrySource;
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
