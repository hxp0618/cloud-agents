import { createHash } from "node:crypto";
import { existsSync, lstatSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  canonicalizeMigrationJson,
  deriveSignedInt64,
  type MigrationJson,
  migrationDigest,
  MigrationValidationError,
  parseSignedInt64Decimal,
  parseStrictMigrationJson,
} from "./platform-migration-json";
import {
  authorityProjectionDigest,
  catalogStateDigest,
  type JsonObject,
  objectIdentityKey,
  rawSha256,
  validateAttemptTerminalState,
  validateAuthorityBinding,
  validateAuthorityProfile,
  validateCatalogProjectionBody,
  validateCatalogState,
  validateDefaultACLScopeFixture,
  validateExpectedStatementTransition,
  validateIntermediateState,
  validateNumericFixture,
  validateObjectIdentity,
} from "./platform-migration-projection";
import { classifyMigrationStatement, splitPostgresStatements } from "./platform-migration-sql";
import {
  createDeterministicUstar,
  readDeterministicUstar,
  type UstarEntry,
} from "./platform-migration-ustar";
import {
  buildEvidenceContractFixtures,
  validateAmbiguousResolutionState,
  validateDecisionRecoveryVerificationInputs,
  validateEvidenceFrame,
  validateGenerationSuperseded,
  validateLineageIndexFrame,
  validateRecoveryPolicyChainFixture,
} from "./platform-migration-evidence";
import {
  assertDurableCoordinationRegistryCurrent,
  buildDurableCoordinationRegistry,
} from "./platform-durable-coordination-registry";
import {
  assertCompatibilityRecoveryRegistryCurrent,
  assertCompatibilityRecoveryRegistryV2Current,
  buildCompatibilityRecoveryRegistry,
  buildCompatibilityRecoveryRegistryV2,
} from "./platform-compatibility-recovery-registry";

export type GeneratedMigrationBundle = {
  readonly files: ReadonlyMap<string, Uint8Array>;
  readonly manifest: JsonObject;
  readonly schemaBundleFile: JsonObject;
  readonly runtimeTar: Uint8Array;
  readonly bootstrapTar: Uint8Array;
};
export type SchemaAncestorArtifact = {
  readonly path: string;
  readonly bytes: Uint8Array;
};

const ROOT = "services/control-plane/migrations";
const MANIFEST_PATH = `${ROOT}/manifest.json`;
const SCHEMA_BUNDLE_PATH = `${ROOT}/schema-bundle.json`;
const SQL_PATHS = [
  `${ROOT}/000001_expand_migration_kernel.sql`,
  `${ROOT}/000002_expand_tenancy.sql`,
  `${ROOT}/000003_expand_membership_rbac.sql`,
  `${ROOT}/000004_expand_membership_rbac_mutations.sql`,
  `${ROOT}/000005_close_membership_binding_authority.sql`,
  `${ROOT}/000006_close_subject_issuer_validation.sql`,
  `${ROOT}/000007_expand_durable_coordination_kernel.sql`,
  `${ROOT}/000008_add_durable_coordination_service.sql`,
  `${ROOT}/000009_redact_coordination_conflicts.sql`,
  `${ROOT}/000010_expand_compatibility_recovery_kernel.sql`,
  `${ROOT}/000011_add_compatibility_recovery_writer.sql`,
  `${ROOT}/000012_fix_compatibility_recovery_preflight.sql`,
  `${ROOT}/000013_add_durable_project_create_writer.sql`,
] as const;
const BOOTSTRAP_PATHS = [`${ROOT}/bootstrap/database.sql`, `${ROOT}/bootstrap/roles.sql`] as const;
const CATALOG_PATHS = [
  `${ROOT}/catalog/authority-v1.json`,
  `${ROOT}/catalog/global-table-authority-v1.json`,
  `${ROOT}/catalog/schema-000001.json`,
  `${ROOT}/catalog/schema-000002.json`,
  `${ROOT}/catalog/schema-000003.json`,
  `${ROOT}/catalog/schema-000004.json`,
  `${ROOT}/catalog/schema-000005.json`,
  `${ROOT}/catalog/schema-000006.json`,
  `${ROOT}/catalog/schema-000007.json`,
  `${ROOT}/catalog/schema-000008.json`,
  `${ROOT}/catalog/schema-000009.json`,
  `${ROOT}/catalog/schema-000010.json`,
  `${ROOT}/catalog/schema-000011.json`,
  `${ROOT}/catalog/schema-000012.json`,
  `${ROOT}/catalog/schema-000013.json`,
] as const;
const GLOBAL_TABLE_AUTHORITY_V2_PATH = `${ROOT}/catalog/global-table-authority-v2.json`;
const GLOBAL_TABLE_AUTHORITY_V3_PATH = `${ROOT}/catalog/global-table-authority-v3.json`;
const GLOBAL_TABLE_AUTHORITY_V4_PATH = `${ROOT}/catalog/global-table-authority-v4.json`;
const PREDECESSOR_SCHEMA_BUNDLE_DIGEST =
  "sha256:54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907";
const PREDECESSOR_SCHEMA_BUNDLE_PATH = `${ROOT}/archive/${PREDECESSOR_SCHEMA_BUNDLE_DIGEST.slice("sha256:".length)}.schema-bundle.json`;
const PREDECESSOR_SCHEMA_BUNDLE_SIZE = 20932;
const PREDECESSOR_SCHEMA_BUNDLE_SHA256 =
  "sha256:948e504b77c409065d2160056f45356d84d136d2512f35a4c4fe9e16e575aaaf";
const ANCESTOR_SCHEMA_BUNDLES = [
  {
    digest: "sha256:6dfd3fed7ba473e6a119a8b6ec3544d88b1a97a4bc5189a6536c64b6fba98110",
    size: 19416,
    sha256: "sha256:a01a22e09c7301aeafc87eb1f09b67cb844e5ac5bc5b3c6dd1e66827e348b90f",
  },
  {
    digest: "sha256:a1673fcdf71fd49439ec9cefde2d02c627029799a700913653ed1f1f6fca7f09",
    size: 17904,
    sha256: "sha256:ca5fea1b9f0056439fd2b58af4a796616d9be3e7ec483869f1cb5bb4f5bfdbb8",
  },
  {
    digest: "sha256:9084475d8db1e74afeb0d77ffaf9e253c4e6b6c67c1ba09a7c45483a42cc15ab",
    size: 14883,
    sha256: "sha256:68efef5dc192323c4ec31cb46e7dda3aecbeb5dba4032876f6f85138d6a80dcd",
  },
  {
    digest: "sha256:8592d8f96dfeffea9379b1588dddd78909cd558db50b0d40157b7b780581544c",
    size: 13374,
    sha256: "sha256:3a2e4ef3cab7227a527831e03f7a85a9bcdbf2963e076c6bb764fe15e1fb194d",
  },
  {
    digest: "sha256:efa8240997f191f6e1540897bf391d6ed3c0a921e5958ea97338aec9e3befeec",
    size: 11860,
    sha256: "sha256:8088b2ff98a7077ec98ca4f925c076501f9478b5b3aa1d8f976582d956884336",
  },
  {
    digest: "sha256:c6652bef99a83b9a8a76739ef7d84e19321feaa80730c548bb7c50191aec3c23",
    size: 7334,
    sha256: "sha256:a4bd9503c1c11c7bcfc48249f501fd258ff09ad2354d4c042f298bb20c705820",
  },
  {
    digest: "sha256:52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d",
    size: 5456,
    sha256: "sha256:d938ca1dc174816d1ccb719d82e57505ed2f9d8e5836dfe4109ab82ae20905ae",
  },
].map((artifact) => ({
  ...artifact,
  path: `${ROOT}/archive/${artifact.digest.slice("sha256:".length)}.schema-bundle.json`,
}));
const BUILTIN_ROLE_CATALOG_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json";
const PROJECTION_FIXTURE_ROOT = `${ROOT}/fixtures/projection`;
const PROJECTION_FIXTURE_PATHS = [
  `${PROJECTION_FIXTURE_ROOT}/manifest.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/authority-binding-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-state-schema-absent-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-state-schema-present-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-projection-body-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/expected-statement-transition-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/numeric-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/intermediate-state-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/attempt-terminal-state-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/negative/faults-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/default-acl-scope-v1.json`,
] as const;
const AUTHORITY_DUPLICATE_RAW_PATH = `${PROJECTION_FIXTURE_ROOT}/negative/authority-binding-duplicate.raw`;
const EVIDENCE_DUPLICATE_RAW_PATH = `${PROJECTION_FIXTURE_ROOT}/negative/evidence-frame-duplicate.raw`;
const EVIDENCE_NESTED_DUPLICATE_RAW_PATH = `${PROJECTION_FIXTURE_ROOT}/negative/evidence-nested-record-duplicate.raw`;
const LINEAGE_DUPLICATE_RAW_PATH = `${PROJECTION_FIXTURE_ROOT}/negative/lineage-frame-duplicate.raw`;
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const HISTORICAL_DURABLE_COORDINATION_REGISTRY_DIGEST =
  "sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30";
const HISTORICAL_DURABLE_COORDINATION_PROFILE_DIGEST =
  "sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a";
const MAX_PROJECTION_SCOPE_PRINCIPALS = 256;
const INITIAL_PROJECTION_SCOPE_AUTHORITY = {
  default_acl_owners: ["cloud_agents_migration_owner"],
  object_creator_closure: ["cloud_agents_migration_owner"],
} as const;
const REQUIRED_FIXTURES = [
  "ancestor-cycle",
  "ancestor-descriptor-cases",
  "ancestor-ledger",
  "duplicate-key",
  "escaped-equivalent-key",
  "ledger-rollback",
  "rfc8785",
  "signed-int64",
  "sql-split",
  "unicode-whitespace",
  "ustar",
] as const;
const LEDGER_BACKED_KEYS = [
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
] as const;
const DECLARED_IDENTITIES_000001 = [
  "schema:unquoted:cloud_agents",
  "table:unquoted:cloud_agents/unquoted:schema_migrations",
  "function:unquoted:cloud_agents/unquoted:is_valid_identifier(unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:require_tenant_id()",
] as const;
const DECLARED_IDENTITIES_000002 = [
  ...DECLARED_IDENTITIES_000001,
  "table:unquoted:cloud_agents/unquoted:platform_tenants",
  "table:unquoted:cloud_agents/unquoted:tenant_resource_versions",
  "index:unquoted:cloud_agents/unquoted:tenant_resource_versions_tenant_fk_idx",
  "table:unquoted:cloud_agents/unquoted:resource_changes",
  "index:unquoted:cloud_agents/unquoted:resource_changes_resource_history_idx",
  "index:unquoted:cloud_agents/unquoted:resource_changes_tenant_fk_idx",
  "table:unquoted:cloud_agents/unquoted:audit_facts",
  "index:unquoted:cloud_agents/unquoted:audit_facts_resource_idx",
  "index:unquoted:cloud_agents/unquoted:audit_facts_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:audit_facts_change_fk_idx",
  "table:unquoted:cloud_agents/unquoted:organizations",
  "table:unquoted:cloud_agents/unquoted:projects",
  "index:unquoted:cloud_agents/unquoted:platform_tenants_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:organizations_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:organizations_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_organization_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_change_fk_idx",
  "policy:unquoted:cloud_agents/unquoted:platform_tenants_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:platform_tenants_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:tenant_resource_versions_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:tenant_resource_versions_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:resource_changes_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:resource_changes_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:audit_facts_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:audit_facts_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:organizations_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:organizations_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:projects_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:projects_migration_owner",
  "function:unquoted:cloud_agents/unquoted:bootstrap_platform_tenant(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
] as const;
const DECLARED_IDENTITIES_000003 = [
  ...DECLARED_IDENTITIES_000002,
  "table:unquoted:cloud_agents/unquoted:builtin_roles",
  "table:unquoted:cloud_agents/unquoted:builtin_role_permissions",
  "index:unquoted:cloud_agents/unquoted:builtin_role_permissions_role_fk_idx",
  "table:unquoted:cloud_agents/unquoted:memberships",
  "index:unquoted:cloud_agents/unquoted:memberships_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:memberships_scope_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:memberships_scope_organization_fk_idx",
  "index:unquoted:cloud_agents/unquoted:memberships_scope_project_fk_idx",
  "index:unquoted:cloud_agents/unquoted:memberships_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:memberships_subject_lookup_idx",
  "table:unquoted:cloud_agents/unquoted:role_bindings",
  "index:unquoted:cloud_agents/unquoted:role_bindings_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_scope_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_scope_organization_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_scope_project_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_role_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:role_bindings_subject_lookup_idx",
  "policy:unquoted:cloud_agents/unquoted:memberships_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:memberships_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:role_bindings_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:role_bindings_migration_owner",
] as const;
const DECLARED_IDENTITIES_000004 = [
  ...DECLARED_IDENTITIES_000003,
  "function:unquoted:cloud_agents/unquoted:subject_ref_digest(unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:require_runtime_mutation_principal()",
  "function:unquoted:cloud_agents/unquoted:allocate_tenant_revision(unquoted:text,unquoted:bigint,unquoted:timestamptz)",
  "function:unquoted:cloud_agents/unquoted:create_membership(unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:transition_membership(unquoted:text,unquoted:bigint,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:suspend_membership(unquoted:text,unquoted:bigint,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:revoke_membership(unquoted:text,unquoted:bigint,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:bind_role(unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:revoke_role_binding(unquoted:text,unquoted:bigint,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
] as const;
const DECLARED_IDENTITIES_000005 = DECLARED_IDENTITIES_000004;
const DECLARED_IDENTITIES_000006 = DECLARED_IDENTITIES_000005;
const DECLARED_IDENTITIES_000007 = [
  ...DECLARED_IDENTITIES_000006,
  "function:unquoted:cloud_agents/unquoted:coordination_registry_digest()",
  "function:unquoted:cloud_agents/unquoted:coordination_state_machine_digest()",
  "function:unquoted:cloud_agents/unquoted:coordination_policy_digest()",
  "function:unquoted:cloud_agents/unquoted:coordination_profile_is_registered(unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:coordination_profile_creates_operation(unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:coordination_profile_outbox_class(unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:coordination_profile_replay_ttl_seconds(unquoted:text,unquoted:text)",
  "table:unquoted:cloud_agents/unquoted:platform_operations",
  "index:unquoted:cloud_agents/unquoted:platform_operations_profile_state_idx",
  "index:unquoted:cloud_agents/unquoted:platform_operations_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:platform_operations_terminal_resource_fk_idx",
  "table:unquoted:cloud_agents/unquoted:operation_attempts",
  "index:unquoted:cloud_agents/unquoted:operation_attempts_claim_idx",
  "index:unquoted:cloud_agents/unquoted:operation_attempts_operation_fk_idx",
  "table:unquoted:cloud_agents/unquoted:terminal_receipts",
  "index:unquoted:cloud_agents/unquoted:terminal_receipts_attempt_fk_idx",
  "index:unquoted:cloud_agents/unquoted:terminal_receipts_resource_fk_idx",
  "table:unquoted:cloud_agents/unquoted:operation_finalizers",
  "index:unquoted:cloud_agents/unquoted:operation_finalizers_claim_idx",
  "index:unquoted:cloud_agents/unquoted:operation_finalizers_operation_fk_idx",
  "table:unquoted:cloud_agents/unquoted:idempotency_records",
  "index:unquoted:cloud_agents/unquoted:idempotency_records_expiry_idx",
  "index:unquoted:cloud_agents/unquoted:idempotency_records_operation_fk_idx",
  "index:unquoted:cloud_agents/unquoted:idempotency_records_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:idempotency_records_resource_fk_idx",
  "table:unquoted:cloud_agents/unquoted:outbox_events",
  "index:unquoted:cloud_agents/unquoted:outbox_events_operation_effect_unique_idx",
  "index:unquoted:cloud_agents/unquoted:outbox_events_claim_idx",
  "index:unquoted:cloud_agents/unquoted:outbox_events_operation_fk_idx",
  "index:unquoted:cloud_agents/unquoted:outbox_events_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:outbox_events_resource_change_fk_idx",
  "table:unquoted:cloud_agents/unquoted:coordination_audit_facts",
  "index:unquoted:cloud_agents/unquoted:coordination_audit_facts_subject_idx",
  "index:unquoted:cloud_agents/unquoted:coordination_audit_facts_operation_fk_idx",
  "index:unquoted:cloud_agents/unquoted:coordination_audit_facts_attempt_fk_idx",
  "index:unquoted:cloud_agents/unquoted:coordination_audit_facts_resource_fk_idx",
  "index:unquoted:cloud_agents/unquoted:coordination_audit_facts_tenant_fk_idx",
  "table:unquoted:cloud_agents/unquoted:leader_leases",
  "index:unquoted:cloud_agents/unquoted:leader_leases_expiry_idx",
  "policy:unquoted:cloud_agents/unquoted:platform_operations_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:platform_operations_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:operation_attempts_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:operation_attempts_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:terminal_receipts_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:terminal_receipts_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:operation_finalizers_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:operation_finalizers_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:idempotency_records_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:idempotency_records_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:outbox_events_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:outbox_events_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:coordination_audit_facts_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:coordination_audit_facts_migration_owner",
] as const;
const DECLARED_IDENTITIES_000008 = [
  ...DECLARED_IDENTITIES_000007,
  "function:unquoted:cloud_agents/unquoted:append_coordination_audit(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:timestamptz)",
  "function:unquoted:cloud_agents/unquoted:claim_managed_agent_create_project_idempotency(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_success(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_failure(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:acquire_coordination_leader(unquoted:text,unquoted:text,unquoted:text,unquoted:integer)",
  "function:unquoted:cloud_agents/unquoted:renew_coordination_leader(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:integer)",
  "function:unquoted:cloud_agents/unquoted:claim_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:transition_outbox_claim(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:acknowledge_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:retry_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:dead_letter_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:reap_expired_outbox_claim(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
] as const;
const DECLARED_IDENTITIES_000009 = [
  ...DECLARED_IDENTITIES_000008,
  "function:unquoted:cloud_agents/unquoted:coordination_current_registry_digest()",
  "function:unquoted:cloud_agents/unquoted:coordination_registry_profile_is_registered(unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:coordination_registry_digest_for_profile(unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:claim_managed_agent_create_project_idempotency_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_success_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_failure_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
] as const;
const DECLARED_IDENTITIES_000010 = [
  ...DECLARED_IDENTITIES_000009,
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_registry_digest()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_state_machine_digest()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_policy_digest()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_profile_digest(unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_profile_is_registered(unquoted:text,unquoted:text)",
  "table:unquoted:cloud_agents/unquoted:workload_database_principals",
  "table:unquoted:cloud_agents/unquoted:migration_backfills",
  "table:unquoted:cloud_agents/unquoted:schema_restore_evidence",
  "table:unquoted:cloud_agents/unquoted:live_instances",
  "table:unquoted:cloud_agents/unquoted:instance_retirement_receipts",
  "index:unquoted:cloud_agents/unquoted:workload_database_principals_instance_idx",
  "index:unquoted:cloud_agents/unquoted:workload_database_principals_expiry_idx",
  "index:unquoted:cloud_agents/unquoted:migration_backfills_state_idx",
  "index:unquoted:cloud_agents/unquoted:schema_restore_evidence_target_idx",
  "index:unquoted:cloud_agents/unquoted:live_instances_schema_range_idx",
  "index:unquoted:cloud_agents/unquoted:live_instances_heartbeat_idx",
  "index:unquoted:cloud_agents/unquoted:instance_retirement_receipts_state_idx",
] as const;
const DECLARED_IDENTITIES_000011 = [
  ...DECLARED_IDENTITIES_000010,
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_registry_digest_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipt_collect_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipt_complete_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipt_reject_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipt_reconcile_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_state_machine_digest_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_policy_digest_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_schema_head_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_schema_catalog_digest_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_schema_migration_digest_v2()",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_profile_digest_v2(unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_profile_is_registered_v2(unquoted:text,unquoted:text)",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principals_v2",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_backfills_v2",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_v2",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instances_v2",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipts_v2",
  "table:unquoted:cloud_agents/unquoted:compatibility_recovery_transition_facts_v2",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_identity_digest_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_lock_v2(unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_transition_lock_v2(unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_require_principal_v2(unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:integer,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_record_v2(unquoted:text,unquoted:text,unquoted:integer,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_complete_v2(unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_reject_v2(unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_reconcile_v2(unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipt_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:boolean,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_start_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_acquire_lease_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_advance_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_heartbeat_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_complete_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_reconcile_v2(unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_record_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:timestamptz)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principal_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principal_register_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principal_rotate_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principal_revoke_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principal_reconcile_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_backfill_transition_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_register_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_activate_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_heartbeat_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:integer,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_begin_drain_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_finish_drain_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_fence_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instance_reconcile_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:compatibility_recovery_migration_preflight_evaluate_v2(unquoted:text,unquoted:integer,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:boolean,unquoted:text)",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principals_v2_state_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_backfills_v2_state_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_v2_target_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instances_v2_preflight_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instances_v2_writer_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipts_v2_state_idx",
  "index:unquoted:cloud_agents/unquoted:compatibility_recovery_transition_facts_v2_identity_idx",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_workload_principals_v2_tenant",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_backfills_v2_tenant",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_restore_evidence_v2_tenant",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_live_instances_v2_tenant",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_retirement_receipts_v2_tenant",
  "policy:unquoted:cloud_agents/unquoted:compatibility_recovery_transition_facts_v2_tenant",
] as const;
const DECLARED_IDENTITIES_000012 = [...DECLARED_IDENTITIES_000011] as const;
const DECLARED_IDENTITIES_000013 = [
  ...DECLARED_IDENTITIES_000012,
  "function:unquoted:cloud_agents/unquoted:coordination_project_create_registry_digest()",
  "function:unquoted:cloud_agents/unquoted:create_managed_agent_project_durable_v1(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
] as const;

export function buildMigrationBundle(root: string): GeneratedMigrationBundle {
  const files = new Map<string, Uint8Array>();
  const predecessorSourcePath = existsSync(resolve(root, PREDECESSOR_SCHEMA_BUNDLE_PATH))
    ? PREDECESSOR_SCHEMA_BUNDLE_PATH
    : SCHEMA_BUNDLE_PATH;
  const predecessorSchemaBundleBytes = readExactFile(root, predecessorSourcePath);
  if (
    predecessorSchemaBundleBytes.length !== PREDECESSOR_SCHEMA_BUNDLE_SIZE ||
    digestBytes(predecessorSchemaBundleBytes) !== PREDECESSOR_SCHEMA_BUNDLE_SHA256
  ) {
    throw new MigrationValidationError("ANCESTOR_DESCRIPTOR", PREDECESSOR_SCHEMA_BUNDLE_PATH);
  }
  const predecessorSchemaBundle = requiredObject(
    parseStrictMigrationJson(predecessorSchemaBundleBytes),
  );
  validateSchemaBundleSelf(predecessorSchemaBundle);
  if (predecessorSchemaBundle.schema_bundle_digest !== PREDECESSOR_SCHEMA_BUNDLE_DIGEST) {
    throw new MigrationValidationError(
      "ANCESTOR_SELF_DIGEST",
      String(predecessorSchemaBundle.schema_bundle_digest),
    );
  }
  files.set(PREDECESSOR_SCHEMA_BUNDLE_PATH, predecessorSchemaBundleBytes);
  for (const ancestor of ANCESTOR_SCHEMA_BUNDLES) {
    const bytes = readExactFile(root, ancestor.path);
    if (bytes.length !== ancestor.size || digestBytes(bytes) !== ancestor.sha256) {
      throw new MigrationValidationError("ANCESTOR_DESCRIPTOR", ancestor.path);
    }
    const bundle = requiredObject(parseStrictMigrationJson(bytes));
    validateSchemaBundleSelf(bundle);
    if (bundle.schema_bundle_digest !== ancestor.digest) {
      throw new MigrationValidationError(
        "ANCESTOR_SELF_DIGEST",
        String(bundle.schema_bundle_digest),
      );
    }
    files.set(ancestor.path, bytes);
  }
  assertDurableCoordinationRegistryCurrent(root);
  const durableCoordinationRegistry = requiredObject(buildDurableCoordinationRegistry(root));
  const historicalDurableCoordinationRegistry = durableCoordinationHistoricalRegistrySnapshot(
    durableCoordinationRegistry,
  );
  assertCompatibilityRecoveryRegistryCurrent(root);
  const compatibilityRecoveryRegistry = requiredObject(buildCompatibilityRecoveryRegistry(root));
  assertCompatibilityRecoveryRegistryV2Current(root);
  const compatibilityRecoveryRegistryV2 = requiredObject(
    buildCompatibilityRecoveryRegistryV2(root),
  );
  const sqlBytes = new Map(SQL_PATHS.map((path) => [path, readExactFile(root, path)] as const));
  validateDurableCoordinationKernel(
    sqlBytes.get(SQL_PATHS[6])!,
    historicalDurableCoordinationRegistry,
  );
  validateDurableCoordinationService(
    sqlBytes.get(SQL_PATHS[7])!,
    historicalDurableCoordinationRegistry,
  );
  validateDurableCoordinationRepair(sqlBytes.get(SQL_PATHS[8])!, durableCoordinationRegistry);
  validateCompatibilityRecoveryKernel(sqlBytes.get(SQL_PATHS[9])!, compatibilityRecoveryRegistry);
  validateCompatibilityRecoveryWriterKernel(
    sqlBytes.get(SQL_PATHS[10])!,
    compatibilityRecoveryRegistryV2,
  );
  validateCompatibilityRecoveryPreflightRepair(sqlBytes.get(SQL_PATHS[11])!);
  validateBuiltinRoleSeedFixture(
    sqlBytes.get(SQL_PATHS[2])!,
    readExactFile(root, BUILTIN_ROLE_CATALOG_PATH),
  );
  const generatedProjection = buildProjectionDocuments(sqlBytes);
  for (const [path, document] of generatedProjection.catalogDocuments) {
    files.set(path, prettyJson(document));
  }
  for (const [path, document] of generatedProjection.fixtureDocuments) {
    files.set(path, prettyJson(document));
  }
  for (const [path, bytes] of generatedProjection.rawFixtureFiles) files.set(path, bytes);

  const sqlArtifacts = new Map(
    SQL_PATHS.map((path) => [path, artifactRecord(path, sqlBytes.get(path)!)] as const),
  );
  const catalogArtifacts = new Map(
    CATALOG_PATHS.map((path) => [path, artifactRecord(path, files.get(path)!)] as const),
  );
  const schemaBundle: JsonObject = {
    lineage: "cloud-agents-platform",
    schema_head: "000013",
    advisory_lock: {
      domain: "cloud-agents-platform:migrations:v1",
      derivation: "sha256-first-8-bytes-signed-big-endian-int64",
      key_int64_decimal: "-1047838957622507638",
    },
    global_table_authority: artifactRecord(
      GLOBAL_TABLE_AUTHORITY_V4_PATH,
      files.get(GLOBAL_TABLE_AUTHORITY_V4_PATH)!,
    ),
    projection_scope_authority: INITIAL_PROJECTION_SCOPE_AUTHORITY,
    predecessor_schema_bundle: {
      schema_bundle_digest: PREDECESSOR_SCHEMA_BUNDLE_DIGEST,
      ...artifactRecord(PREDECESSOR_SCHEMA_BUNDLE_PATH, predecessorSchemaBundleBytes),
    },
    migrations: [
      migrationEntry({
        id: "000001",
        name: "expand_migration_kernel",
        predecessor: null,
        schemaFrom: "absent",
        sql: sqlArtifacts.get(SQL_PATHS[0])!,
        predecessorCatalog: initialPredecessorContract(),
        catalog: catalogArtifacts.get(CATALOG_PATHS[2])!,
      }),
      migrationEntry({
        id: "000002",
        name: "expand_tenancy",
        predecessor: "000001",
        schemaFrom: "000001",
        sql: sqlArtifacts.get(SQL_PATHS[1])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[2])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[3])!,
      }),
      migrationEntry({
        id: "000003",
        name: "expand_membership_rbac",
        predecessor: "000002",
        schemaFrom: "000002",
        sql: sqlArtifacts.get(SQL_PATHS[2])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[3])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[4])!,
      }),
      migrationEntry({
        id: "000004",
        name: "expand_membership_rbac_mutations",
        predecessor: "000003",
        schemaFrom: "000003",
        sql: sqlArtifacts.get(SQL_PATHS[3])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[4])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[5])!,
      }),
      migrationEntry({
        id: "000005",
        name: "close_membership_binding_authority",
        predecessor: "000004",
        schemaFrom: "000004",
        sql: sqlArtifacts.get(SQL_PATHS[4])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[5])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[6])!,
      }),
      migrationEntry({
        id: "000006",
        name: "close_subject_issuer_validation",
        predecessor: "000005",
        schemaFrom: "000005",
        sql: sqlArtifacts.get(SQL_PATHS[5])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[6])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[7])!,
      }),
      migrationEntry({
        id: "000007",
        name: "expand_durable_coordination_kernel",
        predecessor: "000006",
        schemaFrom: "000006",
        sql: sqlArtifacts.get(SQL_PATHS[6])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[7])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[8])!,
      }),
      migrationEntry({
        id: "000008",
        name: "add_durable_coordination_service",
        predecessor: "000007",
        schemaFrom: "000007",
        sql: sqlArtifacts.get(SQL_PATHS[7])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[8])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[9])!,
      }),
      migrationEntry({
        id: "000009",
        name: "redact_coordination_conflicts",
        predecessor: "000008",
        schemaFrom: "000008",
        sql: sqlArtifacts.get(SQL_PATHS[8])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[9])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[10])!,
      }),
      migrationEntry({
        id: "000010",
        name: "expand_compatibility_recovery_kernel",
        predecessor: "000009",
        schemaFrom: "000009",
        sql: sqlArtifacts.get(SQL_PATHS[9])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[10])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[11])!,
      }),
      migrationEntry({
        id: "000011",
        name: "add_compatibility_recovery_writer",
        predecessor: "000010",
        schemaFrom: "000010",
        sql: sqlArtifacts.get(SQL_PATHS[10])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[11])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[12])!,
      }),
      migrationEntry({
        id: "000012",
        name: "fix_compatibility_recovery_preflight",
        predecessor: "000011",
        schemaFrom: "000011",
        sql: sqlArtifacts.get(SQL_PATHS[11])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[12])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[13])!,
      }),
      migrationEntry({
        id: "000013",
        name: "add_durable_project_create_writer",
        predecessor: "000012",
        schemaFrom: "000012",
        sql: sqlArtifacts.get(SQL_PATHS[12])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[13])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[14])!,
      }),
    ],
  };
  const schemaBundleDigest = migrationDigest({
    domain: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: schemaBundle,
  });
  const schemaBundleFile: JsonObject = {
    format_version: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: schemaBundle,
    schema_bundle_digest: schemaBundleDigest,
  };
  files.set(SCHEMA_BUNDLE_PATH, prettyJson(schemaBundleFile));

  const bootstrapArtifacts = BOOTSTRAP_PATHS.map((path) =>
    artifactRecord(path, readExactFile(root, path)),
  ).toSorted(compareArtifactPath);
  const bootstrapBundle: JsonObject = { artifacts: bootstrapArtifacts };
  const bootstrapBundleDigest = migrationDigest({
    domain: "cloud-agents-platform-bootstrap-bundle/v1",
    bootstrap_bundle: bootstrapBundle,
  });
  const runtimePaths = [
    SCHEMA_BUNDLE_PATH,
    PREDECESSOR_SCHEMA_BUNDLE_PATH,
    ...ANCESTOR_SCHEMA_BUNDLES.map((artifact) => artifact.path),
    ...SQL_PATHS,
    ...CATALOG_PATHS,
    GLOBAL_TABLE_AUTHORITY_V2_PATH,
    GLOBAL_TABLE_AUTHORITY_V3_PATH,
    GLOBAL_TABLE_AUTHORITY_V4_PATH,
  ].toSorted();
  const runtimeArtifacts = runtimePaths.map((path) =>
    artifactRecord(path, files.get(path) ?? sqlBytes.get(path)!),
  );
  const manifestWithoutDigest: JsonObject = {
    format_version: "cloud-agents-platform-migration-manifest/v4",
    schema_bundle: schemaBundle,
    schema_bundle_digest: schemaBundleDigest,
    bootstrap_bundle: bootstrapBundle,
    bootstrap_bundle_digest: bootstrapBundleDigest,
    execution_policy: {
      statement_profile: "postgresql-ddl-v1",
      catalog_profile: "cloud-agents-platform-catalog/v1",
      authority_contract: catalogArtifacts.get(CATALOG_PATHS[0])!,
      isolation_level: "serializable",
      access_mode: "read_write",
      postgres_major_min: 15,
      postgres_major_max: 17,
      statement_timeout_ms: 300000,
      lock_timeout_ms: 30000,
      idle_in_transaction_session_timeout_ms: 60000,
      max_attempts: 3,
      lineage_quota_profile: "cloud-agents-platform-lineage-quota-profile/v4",
    },
    runtime_artifacts: runtimeArtifacts,
  };
  const manifest: JsonObject = {
    ...manifestWithoutDigest,
    manifest_digest: migrationDigest(manifestWithoutDigest),
  };
  files.set(MANIFEST_PATH, prettyJson(manifest));
  const runtimeTar = createDeterministicUstar(
    [MANIFEST_PATH, ...runtimePaths].map((path) => ({
      path,
      data: path === MANIFEST_PATH ? files.get(path)! : (files.get(path) ?? sqlBytes.get(path)!),
    })),
  );
  const bootstrapTar = createDeterministicUstar(
    BOOTSTRAP_PATHS.map((path) => ({ path, data: readExactFile(root, path) })),
  );
  return { files, manifest, schemaBundleFile, runtimeTar, bootstrapTar };
}

export function validateCheckedInMigrationBundle(root: string): GeneratedMigrationBundle {
  const expected = buildMigrationBundle(root);
  for (const [path, bytes] of expected.files) {
    const actual = readExactFile(root, path);
    if (!Buffer.from(actual).equals(Buffer.from(bytes))) {
      throw new MigrationValidationError("MIGRATION_BUNDLE_STALE", path);
    }
    if (path.endsWith(".json")) parseStrictMigrationJson(actual);
  }
  validateManifestShape(expected.manifest);
  validateSchemaAncestorChain(
    expected.schemaBundleFile,
    new Map([
      [
        PREDECESSOR_SCHEMA_BUNDLE_DIGEST,
        {
          path: PREDECESSOR_SCHEMA_BUNDLE_PATH,
          bytes: readExactFile(root, PREDECESSOR_SCHEMA_BUNDLE_PATH),
        },
      ],
      ...ANCESTOR_SCHEMA_BUNDLES.map(
        (artifact) =>
          [
            artifact.digest,
            { path: artifact.path, bytes: readExactFile(root, artifact.path) },
          ] as const,
      ),
    ]),
  );
  const schemaBundleDocument = requiredObject(
    parseStrictMigrationJson(readExactFile(root, SCHEMA_BUNDLE_PATH)),
  );
  if (canonicalText(schemaBundleDocument) !== canonicalText(expected.schemaBundleFile)) {
    throw new MigrationValidationError("SCHEMA_BUNDLE_PROJECTION", "manifest/file mismatch");
  }
  validateSharedFixtureInventory(root);
  validateProjectionFixtureInventory(root, expected.files);
  validateAdvisoryLock(requiredObject(expected.manifest.schema_bundle).advisory_lock);
  const checkedInSql = new Map(SQL_PATHS.map((path) => [path, readExactFile(root, path)] as const));
  for (const path of CATALOG_PATHS.slice(2)) {
    validateCatalogStatementBindings(
      requiredObject(parseStrictMigrationJson(readExactFile(root, path))),
      checkedInSql,
    );
  }
  for (const [index, path] of SQL_PATHS.entries()) {
    const statements = splitPostgresStatements(readExactFile(root, path));
    if (statements.length === 0) throw new MigrationValidationError("SQL_EMPTY", path);
    const migrationId = String(index + 1).padStart(6, "0");
    statements.forEach((statement) => classifyMigrationStatement(statement, migrationId));
  }
  const parsedTar = readDeterministicUstar(expected.runtimeTar);
  validateRuntimeTarClosure(expected.manifest, parsedTar);
  const replay = createDeterministicUstar(parsedTar);
  if (!Buffer.from(replay).equals(Buffer.from(expected.runtimeTar))) {
    throw new MigrationValidationError("USTAR_SAME_BITS", "producer/consumer mismatch");
  }
  const bootstrapReplay = createDeterministicUstar(readDeterministicUstar(expected.bootstrapTar));
  validateBootstrapTarClosure(expected.manifest, readDeterministicUstar(expected.bootstrapTar));
  if (!Buffer.from(bootstrapReplay).equals(Buffer.from(expected.bootstrapTar))) {
    throw new MigrationValidationError("USTAR_SAME_BITS", "bootstrap producer/consumer mismatch");
  }
  if (expected.runtimeTar.length > 64 * 1024 * 1024) {
    throw new MigrationValidationError("USTAR_SIZE", String(expected.runtimeTar.length));
  }
  return expected;
}

export function validateCatalogStatementBindings(
  catalog: JsonObject,
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): void {
  const head = requiredString(catalog.schema_head, "catalog schema_head");
  const generatedCatalogs = buildProjectionDocuments(sqlBytes).catalogDocuments;
  const expectedSourcesByHead = new Map<string, MigrationJson>([
    ["000001", generatedCatalogs.get(CATALOG_PATHS[2])!.source_descriptors!],
    ["000002", generatedCatalogs.get(CATALOG_PATHS[3])!.source_descriptors!],
    ["000003", generatedCatalogs.get(CATALOG_PATHS[4])!.source_descriptors!],
    ["000004", generatedCatalogs.get(CATALOG_PATHS[5])!.source_descriptors!],
    ["000005", generatedCatalogs.get(CATALOG_PATHS[6])!.source_descriptors!],
    ["000006", generatedCatalogs.get(CATALOG_PATHS[7])!.source_descriptors!],
    ["000007", generatedCatalogs.get(CATALOG_PATHS[8])!.source_descriptors!],
    ["000008", generatedCatalogs.get(CATALOG_PATHS[9])!.source_descriptors!],
    ["000009", generatedCatalogs.get(CATALOG_PATHS[10])!.source_descriptors!],
    ["000010", generatedCatalogs.get(CATALOG_PATHS[11])!.source_descriptors!],
    ["000011", generatedCatalogs.get(CATALOG_PATHS[12])!.source_descriptors!],
    ["000012", generatedCatalogs.get(CATALOG_PATHS[13])!.source_descriptors!],
    ["000013", generatedCatalogs.get(CATALOG_PATHS[14])!.source_descriptors!],
  ]);
  const declaredByHead = new Map<string, ReadonlyArray<string>>([
    ["000001", DECLARED_IDENTITIES_000001],
    ["000002", DECLARED_IDENTITIES_000002],
    ["000003", DECLARED_IDENTITIES_000003],
    ["000004", DECLARED_IDENTITIES_000004],
    ["000005", DECLARED_IDENTITIES_000005],
    ["000006", DECLARED_IDENTITIES_000006],
    ["000007", DECLARED_IDENTITIES_000007],
    ["000008", DECLARED_IDENTITIES_000008],
    ["000009", DECLARED_IDENTITIES_000009],
    ["000010", DECLARED_IDENTITIES_000010],
    ["000011", DECLARED_IDENTITIES_000011],
    ["000012", DECLARED_IDENTITIES_000012],
    ["000013", DECLARED_IDENTITIES_000013],
  ]);
  const expectedSources = expectedSourcesByHead.get(head);
  const expectedDeclared = declaredByHead.get(head);
  if (!expectedSources || !expectedDeclared) {
    throw new MigrationValidationError("CATALOG_DESCRIPTOR", `unknown schema_head ${head}`);
  }
  const actualSources = catalog.source_descriptors;
  if (
    actualSources === undefined ||
    canonicalText(actualSources) !== canonicalText(expectedSources)
  ) {
    throw new MigrationValidationError("CATALOG_STATEMENT_DESCRIPTOR_MISMATCH", head);
  }
  const declared = requiredArray(catalog.declared_object_identities).map((identity) =>
    requiredObject(identity),
  );
  declared.forEach((identity) => validateObjectIdentity(identity));
  const declaredKeys = declared.map(objectIdentityKey);
  if (new Set(declaredKeys).size !== declaredKeys.length) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITY_DUPLICATE", head);
  }
  const expectedTyped = typedIdentities(expectedDeclared);
  if (canonicalText(declared) !== canonicalText(expectedTyped)) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITIES_MISMATCH", head);
  }
  const allowlist = new Set(expectedDeclared);
  for (const source of requiredArray(actualSources).map(requiredObject)) {
    for (const statement of requiredArray(source.statements).map(requiredObject)) {
      const classificationDocument = requiredObject(statement.classification);
      const target = requiredString(
        classificationDocument.target_identity,
        "catalog statement target_identity",
      );
      if (!allowlist.has(target)) {
        throw new MigrationValidationError("CATALOG_TARGET_NOT_DECLARED", target);
      }
    }
  }
  if (
    Object.hasOwn(catalog, "expected_projection") ||
    catalog.executable_expected_projection_status !== "NOT_IMPLEMENTED_A2_1B_REQUIRED" ||
    catalog.runtime_introspection_status !== "NOT_IMPLEMENTED" ||
    catalog.publication_status !== "UNPUBLISHED_BOOTSTRAP_MUTABLE"
  ) {
    throw new MigrationValidationError("CATALOG_EXECUTABLE_PROJECTION_BOUNDARY", head);
  }
}

function validateRuntimeTarClosure(manifest: JsonObject, entries: ReadonlyArray<UstarEntry>): void {
  const byPath = new Map(entries.map((entry) => [entry.path, entry] as const));
  const records = requiredArray(manifest.runtime_artifacts).map(requiredObject);
  const expectedPaths = [
    MANIFEST_PATH,
    ...records.map((record) => requiredString(record.path, "runtime path")),
  ].toSorted();
  if ([...byPath.keys()].toSorted().join("\0") !== expectedPaths.join("\0")) {
    throw new MigrationValidationError("RUNTIME_TAR_CLOSURE", [...byPath.keys()].join(","));
  }
  const manifestBytes = byPath.get(MANIFEST_PATH)?.data;
  if (
    !manifestBytes ||
    canonicalText(requiredObject(parseStrictMigrationJson(manifestBytes))) !==
      canonicalText(manifest)
  ) {
    throw new MigrationValidationError("RUNTIME_TAR_MANIFEST", "member mismatch");
  }
  for (const record of records) {
    const path = requiredString(record.path, "runtime path");
    const data = byPath.get(path)?.data;
    if (!data || record.size_bytes !== data.length || record.sha256 !== digestBytes(data)) {
      throw new MigrationValidationError("RUNTIME_TAR_ARTIFACT", path);
    }
  }
}

function validateBootstrapTarClosure(
  manifest: JsonObject,
  entries: ReadonlyArray<UstarEntry>,
): void {
  const records = requiredArray(requiredObject(manifest.bootstrap_bundle).artifacts).map(
    requiredObject,
  );
  const byPath = new Map(entries.map((entry) => [entry.path, entry.data] as const));
  const expectedPaths = records
    .map((record) => requiredString(record.path, "bootstrap path"))
    .toSorted();
  if ([...byPath.keys()].toSorted().join("\0") !== expectedPaths.join("\0")) {
    throw new MigrationValidationError("BOOTSTRAP_TAR_CLOSURE", [...byPath.keys()].join(","));
  }
  for (const record of records) {
    const path = requiredString(record.path, "bootstrap path");
    const data = byPath.get(path);
    if (!data || record.size_bytes !== data.length || record.sha256 !== digestBytes(data)) {
      throw new MigrationValidationError("BOOTSTRAP_TAR_ARTIFACT", path);
    }
  }
}

function validateSharedFixtureInventory(root: string): void {
  const manifestPath = `${ROOT}/fixtures/bundle/manifest.json`;
  const manifest = requiredObject(parseStrictMigrationJson(readExactFile(root, manifestPath)));
  assertKeys(manifest, ["format_version", "cases"]);
  if (manifest.format_version !== "cloud-agents-platform-migration-fixtures/v1") {
    throw new MigrationValidationError("FIXTURE_VERSION", String(manifest.format_version));
  }
  const cases = requiredArray(manifest.cases).map(requiredObject);
  const names = cases.map((fixture) => requiredString(fixture.name, "fixture name"));
  if (new Set(names).size !== names.length)
    throw new MigrationValidationError("FIXTURE_DUPLICATE", names.join(","));
  if (names.toSorted().join("\0") !== [...REQUIRED_FIXTURES].toSorted().join("\0")) {
    throw new MigrationValidationError("FIXTURE_INVENTORY", names.join(","));
  }
  for (const fixture of cases) {
    const expected = requiredString(fixture.expected, "fixture expected");
    assertKeys(
      fixture,
      expected === "reject"
        ? ["name", "kind", "path", "expected", "expected_error"]
        : ["name", "kind", "path", "expected"],
    );
    const relative = requiredString(fixture.path, "fixture path");
    if (relative.startsWith("/") || relative.split("/").some((segment) => segment === "..")) {
      throw new MigrationValidationError("FIXTURE_PATH", relative);
    }
    const path = `${ROOT}/fixtures/bundle/${relative}`;
    const document = requiredObject(parseStrictMigrationJson(readExactFile(root, path)));
    if (fixture.kind === "negative_raw_json") {
      assertKeys(document, ["payload", "raw_sha256", "expected", "expected_error"]);
      const payload = requiredString(document.payload, "raw payload");
      if (payload.includes("/") || payload.includes("\\") || payload === "." || payload === "..") {
        throw new MigrationValidationError("FIXTURE_PATH", payload);
      }
      const raw = readExactFile(root, `${dirname(path)}/${payload}`);
      if (document.raw_sha256 !== digestBytes(raw)) {
        throw new MigrationValidationError("FIXTURE_RAW_DIGEST", relative);
      }
      try {
        parseStrictMigrationJson(raw);
      } catch (error) {
        if (
          !(error instanceof MigrationValidationError) ||
          error.code !== requiredString(document.expected_error, "raw expected_error")
        ) {
          throw new MigrationValidationError("FIXTURE_ERROR_MISMATCH", relative);
        }
        continue;
      }
      throw new MigrationValidationError("FIXTURE_EXPECTED_REJECT", relative);
    }
  }
}

function validateProjectionFixtureInventory(
  root: string,
  generatedFiles: ReadonlyMap<string, Uint8Array>,
): void {
  const manifest = requiredObject(
    parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[0])),
  );
  assertKeys(manifest, [
    "format_version",
    "runtime_authority",
    "publication_status",
    "runtime_introspection_status",
    "files",
  ]);
  if (
    manifest.format_version !== "cloud-agents-platform-projection-fixtures/v1" ||
    manifest.runtime_authority !== false ||
    manifest.publication_status !== "UNPUBLISHED_BOOTSTRAP_MUTABLE" ||
    manifest.runtime_introspection_status !== "NOT_IMPLEMENTED"
  ) {
    throw new MigrationValidationError("PROJECTION_FIXTURE_BOUNDARY", "status");
  }
  const records = requiredArray(manifest.files).map(requiredObject);
  const paths = records.map((record) => requiredString(record.path, "projection fixture path"));
  if (new Set(paths).size !== paths.length || paths.join("\0") !== paths.toSorted().join("\0")) {
    throw new MigrationValidationError("PROJECTION_FIXTURE_INVENTORY", paths.join(","));
  }
  for (const record of records) {
    assertKeys(record, ["path", "size_bytes", "sha256"]);
    const relative = requiredString(record.path, "projection fixture path");
    if (relative.startsWith("/") || relative.split("/").some((part) => part === "..")) {
      throw new MigrationValidationError("PROJECTION_FIXTURE_PATH", relative);
    }
    const path = `${PROJECTION_FIXTURE_ROOT}/${relative}`;
    const bytes = readExactFile(root, path);
    if (
      record.size_bytes !== bytes.length ||
      record.sha256 !== rawSha256(bytes) ||
      !generatedFiles.has(path)
    ) {
      throw new MigrationValidationError("PROJECTION_FIXTURE_DIGEST", relative);
    }
  }
  const binding = requiredObject(
    parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[1])),
  );
  validateAuthorityBinding(binding);
  validateCatalogState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[2]))),
  );
  validateCatalogState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[3]))),
  );
  validateCatalogProjectionBody(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[4]))),
  );
  validateExpectedStatementTransition(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[5]))),
  );
  validateNumericFixture(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[6]))),
  );
  validateIntermediateState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[7]))),
  );
  validateAttemptTerminalState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[8]))),
  );
  validateDefaultACLScopeFixture(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[10]))),
  );
  const evidenceRecordChain = fixtureDocument(root, "golden/evidence-record-chain-v1.json");
  for (const frame of requiredArray(evidenceRecordChain.frames).map(requiredObject)) {
    validateEvidenceFrame(frame);
  }
  const ambiguousChain = fixtureDocument(root, "golden/evidence-ambiguous-chain-v1.json");
  for (const resolution of requiredArray(ambiguousChain.resolutions).map(requiredObject)) {
    validateAmbiguousResolutionState(resolution);
  }
  for (const frame of requiredArray(ambiguousChain.frames).map(requiredObject)) {
    validateEvidenceFrame(frame);
  }
  const terminalOutcomes = fixtureDocument(root, "golden/terminal-outcomes-v1.json");
  for (const terminal of requiredArray(terminalOutcomes.outcomes).map(requiredObject)) {
    validateAttemptTerminalState(terminal);
  }
  const retryChains = fixtureDocument(root, "golden/evidence-retry-chains-v1.json");
  for (const chain of requiredArray(retryChains.chains).map(requiredObject)) {
    for (const frame of requiredArray(chain.frames).map(requiredObject))
      validateEvidenceFrame(frame);
  }
  const lineageChain = fixtureDocument(root, "golden/lineage-index-chain-v1.json");
  for (const frame of requiredArray(lineageChain.frames).map(requiredObject)) {
    validateLineageIndexFrame(frame);
  }
  const supersessionOutcomes = fixtureDocument(root, "golden/supersession-outcomes-v1.json");
  for (const outcome of requiredArray(supersessionOutcomes.outcomes).map(requiredObject)) {
    validateGenerationSuperseded(outcome);
  }
  validateDecisionRecoveryVerificationInputs(
    requiredObject(
      fixtureDocument(root, "golden/decision-recovery-inputs-v1.json").same_bits_input,
    ),
  );
  validateRecoveryPolicyChainFixture(fixtureDocument(root, "golden/recovery-policy-chain-v1.json"));
  for (const rawPath of [
    AUTHORITY_DUPLICATE_RAW_PATH,
    EVIDENCE_DUPLICATE_RAW_PATH,
    EVIDENCE_NESTED_DUPLICATE_RAW_PATH,
    LINEAGE_DUPLICATE_RAW_PATH,
  ]) {
    try {
      parseStrictMigrationJson(readExactFile(root, rawPath));
    } catch (error) {
      if (error instanceof MigrationValidationError && error.code === "DUPLICATE_JSON_KEY") {
        continue;
      }
      throw error;
    }
    throw new MigrationValidationError("PROJECTION_DUPLICATE_FIXTURE", rawPath);
  }
}

function fixtureDocument(root: string, relative: string): JsonObject {
  return requiredObject(
    parseStrictMigrationJson(readExactFile(root, `${PROJECTION_FIXTURE_ROOT}/${relative}`)),
  );
}

export function validateSchemaAncestorChain(
  current: JsonObject,
  ancestors: ReadonlyMap<string, SchemaAncestorArtifact>,
): ReadonlyArray<JsonObject> {
  const chain: JsonObject[] = [current];
  const seen = new Set<string>();
  const seenPaths = new Set<string>();
  let cursor = current;
  for (let depth = 0; ; depth += 1) {
    if (depth > 128) throw new MigrationValidationError("ANCESTOR_LIMIT", String(depth));
    const digest = requiredString(cursor.schema_bundle_digest, "schema_bundle_digest");
    if (seen.has(digest)) throw new MigrationValidationError("ANCESTOR_CYCLE", digest);
    seen.add(digest);
    const bundle = requiredObject(cursor.schema_bundle);
    validateSchemaBundleSelf(cursor);
    const descriptor = bundle.predecessor_schema_bundle;
    if (descriptor === null) break;
    const descriptorObject = requiredObject(descriptor);
    assertKeys(descriptorObject, ["schema_bundle_digest", "path", "mode", "size_bytes", "sha256"]);
    const predecessorDigest = requiredString(
      descriptorObject.schema_bundle_digest,
      "predecessor digest",
    );
    if (seen.has(predecessorDigest)) {
      throw new MigrationValidationError("ANCESTOR_CYCLE", predecessorDigest);
    }
    const artifact = ancestors.get(predecessorDigest);
    if (!artifact) throw new MigrationValidationError("ANCESTOR_MISSING", digest);
    const path = requiredString(descriptorObject.path, "predecessor path");
    const expectedPath = `${ROOT}/archive/${predecessorDigest.slice("sha256:".length)}.schema-bundle.json`;
    if (
      !DIGEST.test(predecessorDigest) ||
      path !== expectedPath ||
      artifact.path !== path ||
      descriptorObject.mode !== "100644" ||
      descriptorObject.size_bytes !== artifact.bytes.length ||
      descriptorObject.sha256 !== digestBytes(artifact.bytes)
    ) {
      throw new MigrationValidationError("ANCESTOR_DESCRIPTOR", path);
    }
    if (seenPaths.has(path)) throw new MigrationValidationError("ANCESTOR_DUPLICATE_PATH", path);
    seenPaths.add(path);
    const predecessor = requiredObject(parseStrictMigrationJson(artifact.bytes));
    validateSchemaBundleSelf(predecessor);
    const predecessorMigrations = requiredArray(
      requiredObject(predecessor.schema_bundle).migrations,
    );
    const migrations = requiredArray(bundle.migrations);
    if (predecessorMigrations.length >= migrations.length) {
      throw new MigrationValidationError("ANCESTOR_NOT_STRICT_PREFIX", digest);
    }
    for (const [index, entry] of predecessorMigrations.entries()) {
      if (canonicalText(entry) !== canonicalText(migrations[index]!)) {
        throw new MigrationValidationError("ANCESTOR_PREFIX_MUTATION", `${digest}:${index}`);
      }
    }
    chain.push(predecessor);
    cursor = predecessor;
  }
  return chain.toReversed();
}

function validateSchemaBundleSelf(bundleFile: JsonObject): void {
  assertKeys(bundleFile, ["format_version", "schema_bundle", "schema_bundle_digest"]);
  if (bundleFile.format_version !== "cloud-agents-platform-schema-bundle/v1") {
    throw new MigrationValidationError(
      "ANCESTOR_FORMAT_VERSION",
      String(bundleFile.format_version),
    );
  }
  const digest = requiredString(bundleFile.schema_bundle_digest, "schema bundle self digest");
  const schemaBundle = requiredObject(bundleFile.schema_bundle);
  assertKeys(schemaBundle, [
    "lineage",
    "schema_head",
    "advisory_lock",
    "global_table_authority",
    "projection_scope_authority",
    "predecessor_schema_bundle",
    "migrations",
  ]);
  validateProjectionScopeAuthority(schemaBundle.projection_scope_authority);
  if (
    !DIGEST.test(digest) ||
    digest !==
      migrationDigest({
        domain: "cloud-agents-platform-schema-bundle/v1",
        schema_bundle: schemaBundle,
      })
  ) {
    throw new MigrationValidationError("ANCESTOR_SELF_DIGEST", digest);
  }
}

export function validateLedgerPrefix(
  rows: ReadonlyArray<JsonObject>,
  chain: ReadonlyArray<JsonObject>,
): void {
  const digestIndex = new Map<string, number>();
  const migrationsByDigest = new Map<string, ReadonlyArray<MigrationJson>>();
  for (const [index, bundleFile] of chain.entries()) {
    const digest = requiredString(bundleFile.schema_bundle_digest, "bundle digest");
    digestIndex.set(digest, index);
    migrationsByDigest.set(
      digest,
      requiredArray(requiredObject(bundleFile.schema_bundle).migrations),
    );
  }
  let previousIndex = -1;
  for (const [index, row] of rows.entries()) {
    const expectedId = String(index + 1).padStart(6, "0");
    if (row.migration_id !== expectedId)
      throw new MigrationValidationError("LEDGER_NOT_PREFIX", expectedId);
    const digest = requiredString(row.bundle_digest, "ledger bundle_digest");
    const chainIndex = digestIndex.get(digest);
    if (chainIndex === undefined)
      throw new MigrationValidationError("LEDGER_UNKNOWN_DIGEST", digest);
    if (chainIndex < previousIndex)
      throw new MigrationValidationError("LEDGER_BUNDLE_ROLLBACK", digest);
    const entry = migrationsByDigest.get(digest)?.[index];
    if (!entry || requiredObject(entry).id !== row.migration_id) {
      throw new MigrationValidationError("LEDGER_ENTRY_MISMATCH", expectedId);
    }
    const entryObject = requiredObject(entry);
    if (
      entryObject.sql_artifact !== undefined &&
      canonicalText(projectLedgerBackedColumns(row)) !==
        canonicalText(migrationLedgerProjection(entryObject, digest))
    ) {
      throw new MigrationValidationError("LEDGER_ENTRY_MISMATCH", expectedId);
    }
    previousIndex = chainIndex;
  }
}

export function migrationLedgerProjection(entry: JsonObject, bundleDigest: string): JsonObject {
  const sql = requiredObject(entry.sql_artifact);
  return {
    migration_id: entry.id!,
    migration_name: entry.name!,
    predecessor_id: entry.predecessor_id!,
    phase: entry.phase!,
    schema_from: entry.schema_from!,
    schema_to: entry.schema_to!,
    compatible_binary_min: entry.compatible_control_plane_min!,
    compatible_binary_max: entry.compatible_control_plane_max!,
    sql_path: sql.path!,
    sql_size_bytes: sql.size_bytes!,
    sql_sha256: sql.sha256!,
    bundle_digest: bundleDigest,
    transaction_mode: entry.transaction_mode!,
    reentrancy: entry.reentrancy!,
    rollback_boundary: entry.rollback_boundary!,
    requires_live_instance_preflight: entry.requires_live_instance_preflight!,
    requires_pitr_preflight: entry.requires_pitr_preflight!,
  };
}

function projectLedgerBackedColumns(row: JsonObject): JsonObject {
  const allowed = new Set<string>([...LEDGER_BACKED_KEYS, "applied_at", "applied_by"]);
  for (const key of Object.keys(row)) {
    if (!allowed.has(key)) throw new MigrationValidationError("LEDGER_UNKNOWN_COLUMN", key);
  }
  const projection = Object.create(null) as JsonObject;
  for (const key of LEDGER_BACKED_KEYS) {
    if (!Object.hasOwn(row, key)) throw new MigrationValidationError("LEDGER_MISSING_COLUMN", key);
    Object.defineProperty(projection, key, {
      value: row[key]!,
      enumerable: true,
      configurable: true,
      writable: true,
    });
  }
  return projection;
}

export function migrationBundlePaths(): ReadonlyArray<string> {
  return [
    MANIFEST_PATH,
    SCHEMA_BUNDLE_PATH,
    PREDECESSOR_SCHEMA_BUNDLE_PATH,
    ...ANCESTOR_SCHEMA_BUNDLES.map((artifact) => artifact.path),
    GLOBAL_TABLE_AUTHORITY_V2_PATH,
    GLOBAL_TABLE_AUTHORITY_V3_PATH,
    ...CATALOG_PATHS,
  ];
}

export function migrationStatementSourceDescriptors(
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): MigrationJson[] {
  return SQL_PATHS.map((path, migrationIndex) => ({
    migration_id: String(migrationIndex + 1).padStart(6, "0"),
    sql_sha256: digestBytes(sqlBytes.get(path)!),
    statements: splitPostgresStatements(sqlBytes.get(path)!).map((statement) => ({
      index: statement.index,
      start: statement.start,
      end: statement.end,
      sha256: statement.sha256,
      classification: classifyMigrationStatement(
        statement,
        String(migrationIndex + 1).padStart(6, "0"),
      ),
    })),
  }));
}

export function durableCoordinationHistoricalRegistrySnapshot(registry: JsonObject): JsonObject {
  const historical = structuredClone(registry);
  const profiles = requiredArray(historical.profiles).map(requiredObject);
  if (profiles.length !== 1) {
    throw new MigrationValidationError("COORDINATION_KERNEL_PROFILE", String(profiles.length));
  }
  const profile = profiles[0]!;
  const spec = requiredObject(profile.spec);
  if (
    requiredString(spec.profileId, "coordination profile id") !==
    "managedAgentCreateProject/v1alpha1"
  ) {
    throw new MigrationValidationError("COORDINATION_KERNEL_PROFILE", String(spec.profileId));
  }
  historical.registryDigest = HISTORICAL_DURABLE_COORDINATION_REGISTRY_DIGEST;
  profile.profileDigest = HISTORICAL_DURABLE_COORDINATION_PROFILE_DIGEST;
  return historical;
}

export function validateDurableCoordinationKernel(
  sqlBytes: Uint8Array,
  registry: JsonObject,
): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const profiles = requiredArray(registry.profiles).map(requiredObject);
  if (profiles.length !== 1) {
    throw new MigrationValidationError("COORDINATION_KERNEL_PROFILE", String(profiles.length));
  }
  const profile = profiles[0]!;
  const spec = requiredObject(profile.spec);
  const coordination = requiredObject(spec.coordination);
  const idempotency = requiredObject(spec.idempotency);
  const registryDigest = requiredString(registry.registryDigest, "coordination registry digest");
  const stateMachineDigest = requiredString(
    registry.stateMachineDigest,
    "coordination state-machine digest",
  );
  const policyDigest = requiredString(registry.policyDigest, "coordination policy digest");
  const profileId = requiredString(spec.profileId, "coordination profile id");
  const profileDigest = requiredString(profile.profileDigest, "coordination profile digest");
  const outboxClass = requiredString(
    coordination.outboxEventClass,
    "coordination profile outbox class",
  );
  const replayTtl = idempotency.replayTtlSeconds;
  if (
    profileId !== "managedAgentCreateProject/v1alpha1" ||
    coordination.createsPlatformOperation !== false ||
    coordination.externalSideEffect !== "forbidden" ||
    outboxClass !== "resource_change" ||
    requiredArray(coordination.requiredFinalizers).length !== 0 ||
    replayTtl !== 86400
  ) {
    throw new MigrationValidationError("COORDINATION_KERNEL_PROFILE", profileId);
  }

  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 89) {
    throw new MigrationValidationError(
      "COORDINATION_KERNEL_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  const helperLiterals = statements
    .slice(0, 7)
    .map((statement) => sqlStringLiterals(statement.bytes));
  const expectedHelperLiterals = [
    [registryDigest],
    [stateMachineDigest],
    [policyDigest],
    [profileId, profileDigest],
    [],
    [outboxClass],
    [],
  ];
  if (canonicalText(helperLiterals) !== canonicalText(expectedHelperLiterals)) {
    throw new MigrationValidationError("COORDINATION_KERNEL_PROFILE", "helper literals");
  }
  for (const helper of statements.slice(0, 7)) {
    const helperSql = new TextDecoder("utf-8", { fatal: true }).decode(helper.bytes);
    const createOffset = helperSql.indexOf("CREATE FUNCTION");
    const helperDefinition = createOffset < 0 ? "" : helperSql.slice(createOffset);
    if (
      !helperDefinition.includes("LANGUAGE sql") ||
      !helperDefinition.includes("IMMUTABLE") ||
      !helperDefinition.includes("PARALLEL SAFE") ||
      !helperDefinition.includes("SET search_path = pg_catalog, cloud_agents") ||
      /\b(?:INSERT|UPDATE|DELETE|TRUNCATE|MERGE|CALL|COPY|EXECUTE|PERFORM)\b/iu.test(
        helperDefinition,
      )
    ) {
      throw new MigrationValidationError("COORDINATION_KERNEL_HELPER_PURITY", String(helper.index));
    }
  }
  requireSqlFragment(sql, "coordination operation creation", "AND false");
  requireSqlFragment(sql, "coordination replay TTL", `THEN ${String(replayTtl)}::bigint`);
  requireSqlFragment(sql, "coordination database clock", "pg_catalog.transaction_timestamp()");
  for (const forbidden of ["clock_timestamp()", "SECURITY DEFINER", "pg_notify", "dblink"]) {
    if (sql.toLowerCase().includes(forbidden.toLowerCase())) {
      throw new MigrationValidationError("COORDINATION_KERNEL_FORBIDDEN", forbidden);
    }
  }
  if (
    /\bGRANT\s+(?:ALL|INSERT|UPDATE|DELETE|TRUNCATE)\b[^;]*\bTO\s+CLOUD_AGENTS_RUNTIME\b/isu.test(
      sql,
    )
  ) {
    throw new MigrationValidationError("COORDINATION_KERNEL_RUNTIME_WRITE", "raw table DML");
  }
  for (const statement of statements) {
    const classificationDocument = classifyMigrationStatement(statement, "000007");
    if (new Set(["INSERT", "UPDATE", "DELETE"]).has(classificationDocument.command)) {
      throw new MigrationValidationError(
        "COORDINATION_KERNEL_RUNTIME_WRITE",
        classificationDocument.command,
      );
    }
  }

  const stateMachines = new Map(
    requiredArray(registry.stateMachines).map((value) => {
      const machine = requiredObject(value);
      return [requiredString(machine.id, "state-machine id"), machine] as const;
    }),
  );
  const machineStates = (id: string): string[] =>
    requiredArray(requiredObject(stateMachines.get(id)).states).map((value) =>
      requiredString(value, `${id} state`),
    );
  assertSqlCheckLiteralSet(
    sql,
    "platform_operations_state",
    "state",
    machineStates("platform_operation/v1"),
  );
  assertSqlCheckLiteralSet(
    sql,
    "platform_operations_cleanup_phase",
    "cleanup_phase",
    machineStates("cleanup/v1"),
  );
  assertSqlCheckLiteralSet(
    sql,
    "operation_attempts_state",
    "state",
    machineStates("operation_attempt/v1"),
  );
  assertSqlCheckLiteralSet(
    sql,
    "operation_finalizers_state",
    "state",
    machineStates("finalizer/v1"),
  );
  assertSqlCheckLiteralSet(
    sql,
    "idempotency_records_state",
    "state",
    machineStates("idempotency/v1"),
  );
  assertSqlCheckLiteralSet(sql, "outbox_events_state", "state", machineStates("outbox/v1"));

  const policies = requiredObject(registry.policies);
  const receiptPolicy = requiredObject(policies.terminalReceipt);
  assertSqlCheckLiteralSet(
    sql,
    "terminal_receipts_outcome",
    "outcome",
    requiredArray(receiptPolicy.outcomes).map((value) =>
      requiredString(value, "terminal receipt outcome"),
    ),
  );
  const outboxPolicy = requiredObject(policies.outbox);
  const leaderPolicy = requiredObject(policies.leader);
  requireSqlFragment(
    sql,
    "outbox delivery budget",
    `delivery_attempts BETWEEN 0 AND ${String(outboxPolicy.maxDeliveryAttempts)}`,
  );
  requireSqlFragment(
    sql,
    "leader minimum lease",
    `lease_started_at + interval '${String(leaderPolicy.minLeaseSeconds)} second'`,
  );
  requireSqlFragment(
    sql,
    "leader maximum lease",
    `lease_started_at + interval '${String(leaderPolicy.maxLeaseSeconds)} seconds'`,
  );
  const storedDefinitions = [
    "terminal_receipts",
    "idempotency_records",
    "outbox_events",
    "coordination_audit_facts",
  ]
    .map((table) => sqlTableDefinition(sql, table))
    .join("\n");
  for (const forbidden of requiredArray(requiredObject(policies.audit).forbiddenFields).map(
    (value) => requiredString(value, "forbidden audit field"),
  )) {
    if (new RegExp(`\\b${escapeRegExp(forbidden)}\\b`, "iu").test(storedDefinitions)) {
      throw new MigrationValidationError("COORDINATION_KERNEL_SECRET_FIELD", forbidden);
    }
  }
}

export function validateDurableCoordinationService(
  sqlBytes: Uint8Array,
  registry: JsonObject,
): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const profiles = requiredArray(registry.profiles).map(requiredObject);
  if (profiles.length !== 1) {
    throw new MigrationValidationError("COORDINATION_SERVICE_PROFILE", String(profiles.length));
  }
  const profile = profiles[0]!;
  const spec = requiredObject(profile.spec);
  const authorization = requiredObject(spec.authorization);
  const coordination = requiredObject(spec.coordination);
  const profileId = requiredString(spec.profileId, "coordination service profile id");
  const profileDigest = requiredString(
    profile.profileDigest,
    "coordination service profile digest",
  );
  if (
    profileId !== "managedAgentCreateProject/v1alpha1" ||
    requiredString(spec.operationId, "coordination service operation id") !==
      "managedAgentCreateProject" ||
    requiredString(authorization.tenantSource, "coordination service tenant source") !==
      "path.tenantId" ||
    requiredString(authorization.scopeSource, "coordination service scope source") !==
      "body.organizationRef" ||
    requiredString(authorization.requiredPermission, "coordination service required permission") !==
      "projects.create" ||
    coordination.createsPlatformOperation !== false ||
    coordination.externalSideEffect !== "forbidden" ||
    requiredString(coordination.outboxEventClass, "coordination service outbox class") !==
      "resource_change" ||
    requiredArray(coordination.requiredFinalizers).length !== 0
  ) {
    throw new MigrationValidationError("COORDINATION_SERVICE_PROFILE", profileId);
  }

  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 34) {
    throw new MigrationValidationError(
      "COORDINATION_SERVICE_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  requireSqlFragment(sql, "coordination service profile id", profileId);
  requireSqlFragment(sql, "coordination service profile digest", profileDigest);
  requireSqlFragment(
    sql,
    "coordination service runtime principal",
    "cloud_agents.require_runtime_mutation_principal()",
  );
  requireSqlFragment(
    sql,
    "coordination service database clock",
    "pg_catalog.transaction_timestamp()",
  );
  requireSqlFragment(sql, "coordination service serial rejection", "ERRCODE = '40001'");
  requireSqlFragment(sql, "coordination service full claim token", "stored.claim_token");
  requireSqlFragment(
    sql,
    "coordination service full claim incarnation",
    "stored.claim_incarnation",
  );
  requireSqlFragment(sql, "coordination service leader fence", "lease.fencing_token");

  const expectedRuntimeFunctions = [
    "function:unquoted:cloud_agents/unquoted:claim_managed_agent_create_project_idempotency(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_success(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_failure(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:acquire_coordination_leader(unquoted:text,unquoted:text,unquoted:text,unquoted:integer)",
    "function:unquoted:cloud_agents/unquoted:renew_coordination_leader(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:integer)",
    "function:unquoted:cloud_agents/unquoted:claim_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:integer,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:acknowledge_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:retry_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:dead_letter_outbox_event(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:reap_expired_outbox_claim(unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text)",
  ].toSorted();
  const runtimeFunctions = statements
    .map((statement) => classifyMigrationStatement(statement, "000008"))
    .filter(
      (classification) =>
        classification.command === "GRANT" &&
        classification.object_kind === "FUNCTION" &&
        classification.grantee === "CLOUD_AGENTS_RUNTIME",
    )
    .map((classification) => classification.target_identity)
    .toSorted();
  if (canonicalText(runtimeFunctions) !== canonicalText(expectedRuntimeFunctions)) {
    throw new MigrationValidationError(
      "COORDINATION_SERVICE_RUNTIME_EXECUTE",
      runtimeFunctions.join(","),
    );
  }
  if (
    /\bGRANT\s+(?:ALL|INSERT|UPDATE|DELETE|TRUNCATE)\b[^;]*\bTO\s+CLOUD_AGENTS_RUNTIME\b/isu.test(
      sql,
    )
  ) {
    throw new MigrationValidationError("COORDINATION_SERVICE_RUNTIME_WRITE", "raw table DML");
  }
  if (
    /\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+CLOUD_AGENTS\.(?:PLATFORM_OPERATIONS|OPERATION_ATTEMPTS|TERMINAL_RECEIPTS|OPERATION_FINALIZERS)\b/iu.test(
      sql,
    )
  ) {
    throw new MigrationValidationError(
      "COORDINATION_SERVICE_PROFILE",
      "current profile cannot create operations or finalizers",
    );
  }
  for (const forbidden of [
    "pg_notify",
    "dblink",
    "http_post",
    "net.http",
    "aws_lambda",
    "COPY ",
    "CALL ",
  ]) {
    if (sql.toLowerCase().includes(forbidden.toLowerCase())) {
      throw new MigrationValidationError("COORDINATION_SERVICE_EXTERNAL_EFFECT", forbidden);
    }
  }
}

export function validateDurableCoordinationRepair(
  sqlBytes: Uint8Array,
  registry: JsonObject,
): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const profiles = requiredArray(registry.profiles).map(requiredObject);
  if (profiles.length !== 1) {
    throw new MigrationValidationError("COORDINATION_REPAIR_PROFILE", String(profiles.length));
  }
  const profile = profiles[0]!;
  const spec = requiredObject(profile.spec);
  const profileId = requiredString(spec.profileId, "coordination repair profile id");
  const profileDigest = requiredString(profile.profileDigest, "coordination repair profile digest");
  const registryDigest = requiredString(
    registry.registryDigest,
    "coordination repair registry digest",
  );
  if (
    profileId !== "managedAgentCreateProject/v1alpha1" ||
    registryDigest === HISTORICAL_DURABLE_COORDINATION_REGISTRY_DIGEST ||
    profileDigest === HISTORICAL_DURABLE_COORDINATION_PROFILE_DIGEST
  ) {
    throw new MigrationValidationError("COORDINATION_REPAIR_PROFILE", profileId);
  }

  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 30) {
    throw new MigrationValidationError(
      "COORDINATION_REPAIR_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  const registryHelpers = [
    "function:unquoted:cloud_agents/unquoted:coordination_current_registry_digest()",
    "function:unquoted:cloud_agents/unquoted:coordination_registry_profile_is_registered(unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:coordination_registry_digest_for_profile(unquoted:text,unquoted:text)",
  ] as const;
  const replacedFunctions = [
    "function:unquoted:cloud_agents/unquoted:coordination_profile_is_registered(unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:coordination_profile_creates_operation(unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:coordination_profile_outbox_class(unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:coordination_profile_replay_ttl_seconds(unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:append_coordination_audit(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:bigint,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:timestamptz)",
    "function:unquoted:cloud_agents/unquoted:claim_managed_agent_create_project_idempotency(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:transition_outbox_claim(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  ] as const;
  const runtimeV2Functions = [
    "function:unquoted:cloud_agents/unquoted:claim_managed_agent_create_project_idempotency_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_success_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text)",
    "function:unquoted:cloud_agents/unquoted:complete_managed_agent_create_project_failure_v2(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
  ] as const;
  const registryTables = [
    "table:unquoted:cloud_agents/unquoted:platform_operations",
    "table:unquoted:cloud_agents/unquoted:idempotency_records",
    "table:unquoted:cloud_agents/unquoted:outbox_events",
    "table:unquoted:cloud_agents/unquoted:coordination_audit_facts",
  ] as const;
  const classificationKey = (value: {
    command: string;
    object_kind: string;
    target_identity: string;
    grantee: string | null;
    special_case: string | null;
  }): string =>
    [
      value.command,
      value.object_kind,
      value.target_identity,
      value.grantee ?? "",
      value.special_case ?? "",
    ].join("\0");
  const expectedClassifications = [
    ...registryHelpers.map((target_identity) => ({
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...replacedFunctions.map((target_identity) => ({
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...runtimeV2Functions.map((target_identity) => ({
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...registryTables.flatMap((target_identity) =>
      Array.from({ length: 2 }, () => ({
        command: "ALTER",
        object_kind: "TABLE",
        target_identity,
        grantee: null,
        special_case: null,
      })),
    ),
    ...registryHelpers.map((target_identity) => ({
      command: "REVOKE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: "PUBLIC",
      special_case: null,
    })),
    ...runtimeV2Functions.map((target_identity) => ({
      command: "REVOKE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: "PUBLIC",
      special_case: null,
    })),
    ...runtimeV2Functions.map((target_identity) => ({
      command: "GRANT",
      object_kind: "FUNCTION",
      target_identity,
      grantee: "CLOUD_AGENTS_RUNTIME",
      special_case: null,
    })),
  ]
    .map(classificationKey)
    .toSorted();
  const actualClassifications = statements
    .map((statement) => classifyMigrationStatement(statement, "000009"))
    .map(classificationKey)
    .toSorted();
  if (canonicalText(actualClassifications) !== canonicalText(expectedClassifications)) {
    throw new MigrationValidationError(
      "COORDINATION_REPAIR_SURFACE",
      actualClassifications.join(","),
    );
  }

  for (const helper of statements.slice(0, 7)) {
    const helperSql = new TextDecoder("utf-8", { fatal: true }).decode(helper.bytes);
    const createOffset = helperSql.indexOf("CREATE FUNCTION");
    const replaceOffset = helperSql.indexOf("CREATE OR REPLACE FUNCTION");
    const helperDefinition = helperSql.slice(
      createOffset >= 0 ? createOffset : replaceOffset >= 0 ? replaceOffset : helperSql.length,
    );
    if (
      !helperDefinition.includes("LANGUAGE sql") ||
      !helperDefinition.includes("IMMUTABLE") ||
      !helperDefinition.includes("PARALLEL SAFE") ||
      !helperDefinition.includes("SET search_path = pg_catalog, cloud_agents") ||
      /\b(?:INSERT|UPDATE|DELETE|TRUNCATE|MERGE|CALL|COPY|EXECUTE|PERFORM)\b/iu.test(
        helperDefinition,
      )
    ) {
      throw new MigrationValidationError("COORDINATION_REPAIR_HELPER_PURITY", String(helper.index));
    }
  }

  for (const replaced of [
    "coordination_profile_is_registered",
    "coordination_profile_creates_operation",
    "coordination_profile_outbox_class",
    "coordination_profile_replay_ttl_seconds",
    "append_coordination_audit",
    "claim_managed_agent_create_project_idempotency",
    "transition_outbox_claim",
  ]) {
    requireSqlFragment(
      sql,
      `coordination replacement ${replaced}`,
      `CREATE OR REPLACE FUNCTION cloud_agents.${replaced}`,
    );
  }
  for (const [table, constraint] of [
    ["platform_operations", "platform_operations_registry_digest"],
    ["idempotency_records", "idempotency_records_registry_digest"],
    ["outbox_events", "outbox_events_registry_digest"],
    ["coordination_audit_facts", "coordination_audit_facts_registry_digest"],
  ] as const) {
    requireSqlFragment(
      sql,
      `coordination registry constraint drop ${table}`,
      `ALTER TABLE cloud_agents.${table}\n    DROP CONSTRAINT ${constraint};`,
    );
    requireSqlFragment(
      sql,
      `coordination registry constraint add ${table}`,
      `ALTER TABLE cloud_agents.${table}\n    ADD CONSTRAINT ${constraint}\n        CHECK (cloud_agents.coordination_registry_profile_is_registered(`,
    );
  }
  for (const fragment of [
    HISTORICAL_DURABLE_COORDINATION_REGISTRY_DIGEST,
    HISTORICAL_DURABLE_COORDINATION_PROFILE_DIGEST,
    registryDigest,
    profileId,
    profileDigest,
    "cloud_agents.coordination_current_registry_digest()",
    "cloud_agents.coordination_registry_profile_is_registered(",
    "cloud_agents.coordination_registry_digest_for_profile(",
    "claim_disposition := 'conflict'",
    "replay_state := NULL",
    "resource_id := NULL",
    "stable_error_code := NULL",
    "expires_at := NULL",
    "WHEN 'retry_wait' THEN 'pending'",
  ]) {
    requireSqlFragment(sql, "coordination repair closed result", fragment);
  }
  if (/\b(?:CREATE\s+TABLE|DROP\s+(?:TABLE|FUNCTION)|TRUNCATE)\b/iu.test(sql)) {
    throw new MigrationValidationError("COORDINATION_REPAIR_SURFACE", "destructive drift");
  }
  if (
    /\bGRANT\s+(?:ALL|INSERT|UPDATE|DELETE|TRUNCATE)\b[^;]*\bTO\s+CLOUD_AGENTS_RUNTIME\b/isu.test(
      sql,
    )
  ) {
    throw new MigrationValidationError("COORDINATION_REPAIR_SURFACE", "raw table DML");
  }
  for (const forbidden of [
    "pg_notify",
    "dblink",
    "http_post",
    "net.http",
    "aws_lambda",
    "COPY ",
    "CALL ",
  ]) {
    if (sql.toLowerCase().includes(forbidden.toLowerCase())) {
      throw new MigrationValidationError("COORDINATION_SERVICE_EXTERNAL_EFFECT", forbidden);
    }
  }
}

export function validateCompatibilityRecoveryKernel(
  sqlBytes: Uint8Array,
  registry: JsonObject,
): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const registryDigest = requiredString(registry.registryDigest, "compatibility registry digest");
  const stateMachineDigest = requiredString(
    registry.stateMachineDigest,
    "compatibility state machine digest",
  );
  const policyDigest = requiredString(registry.policyDigest, "compatibility policy digest");
  const boundary = requiredObject(registry.implementationBoundary);
  if (
    requiredString(boundary.sqlMigration, "compatibility sql boundary") !==
      "not_implemented_no_000010" ||
    requiredString(boundary.goConsumer, "compatibility go boundary") !== "not_implemented" ||
    requiredString(boundary.httpSurface, "compatibility http boundary") !== "not_implemented" ||
    requiredString(boundary.externalSideEffects, "compatibility external boundary") !==
      "forbidden" ||
    requiredString(boundary.gateStatus, "compatibility gate boundary") !== "non_gate_evidence_only"
  ) {
    throw new MigrationValidationError("COMPATIBILITY_KERNEL_BOUNDARY", "registry");
  }
  const profiles = requiredArray(registry.profiles).map(requiredObject);
  const expectedProfiles = [
    ["backfill/v1", "sha256:779791352f9ba77f1f75c3cd6e5b4a846ee00687217eb3489ec8877513809047"],
    ["live-instance/v1", "sha256:aeb12441bc83a110047a1a69a413d2672cf5ba8c82747d52a842ab91c4840790"],
    [
      "migration-preflight/v1",
      "sha256:0ef86c85d7878202ac16f06c6b32a7bd84d642433a7098a25fc09b5f7f8599ba",
    ],
    [
      "restore-evidence/v1",
      "sha256:d095186e6f70205f9c842acc8e7232ff658c4aecbed06436ad91532e6cf4042e",
    ],
    [
      "retirement-receipt/v1",
      "sha256:cf2e57dcf51bfea35e7ca82875acb04225e5a050fcf3d394cb6f1bc457d2a3ac",
    ],
  ] as const;
  if (profiles.length !== expectedProfiles.length) {
    throw new MigrationValidationError("COMPATIBILITY_KERNEL_PROFILE", String(profiles.length));
  }
  for (const [index, [profileId, profileDigest]] of expectedProfiles.entries()) {
    const profile = profiles[index]!;
    const spec = requiredObject(profile.spec);
    if (
      requiredString(profile.profileDigest, "compatibility profile digest") !== profileDigest ||
      requiredString(spec.profileId, "compatibility profile id") !== profileId ||
      requiredString(spec.stateMachineId, "compatibility state machine id") !== profileId
    ) {
      throw new MigrationValidationError("COMPATIBILITY_KERNEL_PROFILE", profileId);
    }
  }
  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 52) {
    throw new MigrationValidationError(
      "COMPATIBILITY_KERNEL_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  const classificationKey = (value: {
    command: string;
    object_kind: string;
    target_identity: string;
    grantee: string | null;
    special_case: string | null;
  }): string =>
    [
      value.command,
      value.object_kind,
      value.target_identity,
      value.grantee ?? "",
      value.special_case ?? "",
    ].join("\0");
  const helperIdentities = DECLARED_IDENTITIES_000010.slice(
    DECLARED_IDENTITIES_000009.length,
    DECLARED_IDENTITIES_000009.length + 5,
  );
  const tableIdentities = [
    "table:unquoted:cloud_agents/unquoted:workload_database_principals",
    "table:unquoted:cloud_agents/unquoted:migration_backfills",
    "table:unquoted:cloud_agents/unquoted:schema_restore_evidence",
    "table:unquoted:cloud_agents/unquoted:live_instances",
    "table:unquoted:cloud_agents/unquoted:instance_retirement_receipts",
  ] as const;
  const indexIdentities = DECLARED_IDENTITIES_000010.slice(
    DECLARED_IDENTITIES_000009.length + 5 + tableIdentities.length,
  );
  const expectedClassification = [
    ...helperIdentities.map((target_identity) => ({
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...tableIdentities.map((target_identity) => ({
      command: "CREATE",
      object_kind: "TABLE",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...indexIdentities.map((target_identity) => ({
      command: "CREATE",
      object_kind: "INDEX",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...helperIdentities.map((target_identity) => ({
      command: "ALTER",
      object_kind: "FUNCTION",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...tableIdentities.map((target_identity) => ({
      command: "ALTER",
      object_kind: "TABLE",
      target_identity,
      grantee: null,
      special_case: null,
    })),
    ...helperIdentities.map((target_identity) => ({
      command: "REVOKE",
      object_kind: "FUNCTION",
      target_identity,
      grantee: "PUBLIC",
      special_case: null,
    })),
    ...tableIdentities.flatMap((target_identity) =>
      ["PUBLIC", "CLOUD_AGENTS_RUNTIME", "CLOUD_AGENTS_BOOTSTRAP_ADMIN"].map((grantee) => ({
        command: "REVOKE",
        object_kind: "TABLE",
        target_identity,
        grantee,
        special_case: null,
      })),
    ),
    ...helperIdentities.map((target_identity) => ({
      command: "GRANT",
      object_kind: "FUNCTION",
      target_identity,
      grantee: "CLOUD_AGENTS_RUNTIME",
      special_case: null,
    })),
  ]
    .map(classificationKey)
    .toSorted();
  const actualClassification = statements
    .map((statement) => classifyMigrationStatement(statement, "000010"))
    .map(classificationKey)
    .toSorted();
  if (canonicalText(actualClassification) !== canonicalText(expectedClassification)) {
    throw new MigrationValidationError(
      "COMPATIBILITY_KERNEL_SURFACE",
      actualClassification.join(","),
    );
  }
  const helperLiterals = [
    registryDigest,
    stateMachineDigest,
    policyDigest,
    ...expectedProfiles.flatMap(([profileId, profileDigest]) => [profileId, profileDigest]),
  ];
  for (const literal of helperLiterals)
    requireSqlFragment(sql, "compatibility digest literal", literal);
  for (const helper of statements.slice(0, helperIdentities.length)) {
    const helperSql = new TextDecoder("utf-8", { fatal: true }).decode(helper.bytes);
    const createOffset = helperSql.indexOf("CREATE FUNCTION");
    const helperDefinition = helperSql.slice(createOffset < 0 ? 0 : createOffset);
    if (
      createOffset < 0 ||
      !/\bLANGUAGE\s+sql\b/iu.test(helperDefinition) ||
      !/\bIMMUTABLE\b/iu.test(helperDefinition) ||
      !/\bPARALLEL\s+SAFE\b/iu.test(helperDefinition) ||
      !/SET\s+search_path\s*=\s*pg_catalog\s*,\s*cloud_agents/iu.test(helperDefinition) ||
      /\b(?:SECURITY\s+DEFINER|INSERT|UPDATE|DELETE|TRUNCATE|MERGE|CALL|COPY|EXECUTE|PERFORM)\b/iu.test(
        helperDefinition,
      )
    ) {
      throw new MigrationValidationError(
        "COMPATIBILITY_KERNEL_HELPER_PURITY",
        String(helper.index),
      );
    }
  }
  for (const [constraint, column, values] of [
    ["workload_database_principals_state", "state", ["active", "expired", "revoked"]],
    ["migration_backfills_state", "state", ["failed", "paused", "pending", "running", "succeeded"]],
    [
      "live_instances_drain_state",
      "drain_state",
      ["active", "drained", "draining", "expired_unproven", "registered", "retired"],
    ],
    [
      "schema_restore_evidence_state",
      "state",
      ["drill_verified", "eligible", "invalidated", "recorded"],
    ],
    ["instance_retirement_receipts_state", "state", ["collecting", "complete", "rejected"]],
  ] as const) {
    assertSqlCheckLiteralSet(sql, constraint, column, values);
  }
  for (const fragment of [
    "profile_id = 'backfill/v1'",
    "profile_id = 'live-instance/v1'",
    "profile_id = 'restore-evidence/v1'",
    "FOREIGN KEY (service_kind, instance_id, incarnation)",
    "REFERENCES cloud_agents.live_instances",
    "GRANT EXECUTE ON FUNCTION",
  ])
    requireSqlFragment(sql, "compatibility kernel fragment", fragment);
  const executableSql = sql.slice(sql.indexOf("CREATE FUNCTION"));
  if (
    /\b(?:SECURITY\s+DEFINER|CREATE\s+OR\s+REPLACE|INSERT|UPDATE|DELETE|TRUNCATE|MERGE|CALL|COPY|pg_notify|dblink|http_post|net\.http|aws_lambda)\b/iu.test(
      executableSql,
    ) ||
    /\bGRANT\s+(?:ALL|SELECT|INSERT|UPDATE|DELETE|TRUNCATE|REFERENCES|TRIGGER)\b/iu.test(
      executableSql,
    )
  ) {
    throw new MigrationValidationError("COMPATIBILITY_KERNEL_SIDE_EFFECT", "forbidden SQL");
  }
  for (const table of tableIdentities) {
    const name = table.slice(table.lastIndexOf("/") + "/unquoted:".length);
    if (!new RegExp(`CREATE\\s+TABLE\\s+cloud_agents\\.${escapeRegExp(name)}\\b`, "iu").test(sql)) {
      throw new MigrationValidationError("COMPATIBILITY_KERNEL_TABLE", name);
    }
  }
}

export function validateCompatibilityRecoveryWriterKernel(
  sqlBytes: Uint8Array,
  registry: JsonObject,
): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const registryDigest = requiredString(registry.registryDigest, "writer registry digest");
  const stateMachineDigest = requiredString(
    registry.stateMachineDigest,
    "writer state machine digest",
  );
  const policyDigest = requiredString(registry.policyDigest, "writer policy digest");
  if (
    registryDigest !== "sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973" ||
    stateMachineDigest !==
      "sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863" ||
    policyDigest !== "sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708"
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_REGISTRY", registryDigest);
  }
  const schemaBinding = requiredObject(registry.schemaBinding);
  const exactSchemaBinding = [
    requiredString(schemaBinding.schemaHead, "writer schema head"),
    requiredString(schemaBinding.schemaCatalogSha256, "writer schema catalog"),
    requiredString(schemaBinding.schemaMigrationSha256, "writer schema migration"),
  ];
  if (
    exactSchemaBinding.join("\0") !==
    [
      "000010",
      "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236",
      "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6",
    ].join("\0")
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_SCHEMA_BINDING", "000010");
  }
  const boundary = requiredObject(registry.implementationBoundary);
  if (
    requiredString(boundary.sqlWriterMigration, "writer SQL boundary") !==
      "not_implemented_after_000010" ||
    requiredString(boundary.goConsumer, "writer Go boundary") !== "not_implemented" ||
    requiredString(boundary.httpSurface, "writer HTTP boundary") !== "not_implemented" ||
    requiredString(boundary.externalSideEffects, "writer external boundary") !== "forbidden" ||
    requiredString(boundary.providerSideEffects, "writer provider boundary") !== "forbidden" ||
    requiredString(boundary.productionDatabaseWrites, "writer production boundary") !==
      "not_authorized" ||
    requiredString(boundary.gateStatus, "writer gate boundary") !== "all_gates_open"
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_BOUNDARY", "registry");
  }

  const expectedProfiles = new Map([
    ["backfill/v2", "sha256:c5d96407e0c0003689faa9e5526098b57e8b40d9ef67c76f9318e2b0326e6145"],
    ["live-instance/v2", "sha256:0b2362b300f48a58160d5f9b754c865194f2a4ca14d6012fe361b340dd1b8ff8"],
    [
      "migration-preflight/v2",
      "sha256:e02302ea60eca9855d362d8bcab7efc0466adab6d3a486d828adccdbc5411d7a",
    ],
    [
      "restore-evidence/v2",
      "sha256:c9a3376afb9e90717dc4191f88723488cf79bd5ee5df9ef24db1be8eb9a01106",
    ],
    [
      "retirement-receipt/v2",
      "sha256:b789a28be60a340f49662cd5c1570f29b30abd3b4b27ef76d7e6a2666833876f",
    ],
    [
      "workload-principal/v2",
      "sha256:7208b25e051ce6cb298d8f88190365a950bc0ac48a669fbf7ab93de35cee6878",
    ],
  ] as const);
  const operationProfiles = new Map<string, string>();
  const operationFunctions = new Set<string>();
  const profiles = requiredArray(registry.profiles).map(requiredObject);
  if (profiles.length !== expectedProfiles.size) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_PROFILE", String(profiles.length));
  }
  for (const profile of profiles) {
    const spec = requiredObject(profile.spec);
    const profileId = requiredString(spec.profileId, "writer profile id");
    if (
      requiredString(profile.profileDigest, "writer profile digest") !==
      expectedProfiles.get(profileId)
    ) {
      throw new MigrationValidationError("COMPATIBILITY_WRITER_PROFILE", profileId);
    }
    for (const operation of requiredArray(spec.operations).map(requiredObject)) {
      const sqlFunction = requiredString(operation.sqlFunction, "writer SQL function");
      const functionName = sqlFunction.replace(/^cloud_agents\./u, "");
      if (
        functionName === sqlFunction ||
        operationFunctions.has(functionName) ||
        !/^[a-z][a-z0-9_]*_v2$/u.test(functionName)
      ) {
        throw new MigrationValidationError("COMPATIBILITY_WRITER_OPERATION", sqlFunction);
      }
      operationFunctions.add(functionName);
      operationProfiles.set(functionName, profileId);
    }
  }
  if (operationFunctions.size !== 26) {
    throw new MigrationValidationError(
      "COMPATIBILITY_WRITER_OPERATION",
      String(operationFunctions.size),
    );
  }

  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 161) {
    throw new MigrationValidationError(
      "COMPATIBILITY_WRITER_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  const classifications = statements.map((statement) =>
    classifyMigrationStatement(statement, "000011"),
  );
  const newDeclared = new Set(DECLARED_IDENTITIES_000011.slice(DECLARED_IDENTITIES_000010.length));
  const created = classifications
    .filter((classification) => classification.command === "CREATE")
    .map((classification) => classification.target_identity);
  if (
    created.length !== newDeclared.size ||
    created.some((identity) => !newDeclared.has(identity)) ||
    [...newDeclared].some((identity) => !created.includes(identity))
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_OBJECT_SET", created.join(","));
  }
  const summary = new Map<string, number>();
  for (const classification of classifications) {
    const key = [
      classification.command,
      classification.object_kind,
      classification.grantee ?? "",
    ].join("|");
    summary.set(key, (summary.get(key) ?? 0) + 1);
  }
  const expectedSummary = new Map([
    ["ALTER|TABLE|", 12],
    ["CREATE|FUNCTION|", 44],
    ["CREATE|INDEX|", 7],
    ["CREATE|POLICY|", 6],
    ["CREATE|TABLE|", 6],
    ["GRANT|FUNCTION|CLOUD_AGENTS_BOOTSTRAP_ADMIN", 4],
    ["GRANT|FUNCTION|CLOUD_AGENTS_RUNTIME", 20],
    ["REVOKE|FUNCTION|PUBLIC", 44],
    ["REVOKE|TABLE|CLOUD_AGENTS_BOOTSTRAP_ADMIN", 6],
    ["REVOKE|TABLE|CLOUD_AGENTS_RUNTIME", 6],
    ["REVOKE|TABLE|PUBLIC", 6],
  ]);
  if (
    summary.size !== expectedSummary.size ||
    [...expectedSummary].some(([key, count]) => summary.get(key) !== count)
  ) {
    throw new MigrationValidationError(
      "COMPATIBILITY_WRITER_SURFACE",
      JSON.stringify(Object.fromEntries(summary)),
    );
  }

  const createdFunctionIdentityByName = new Map<string, string>();
  for (const identity of created.filter((value) => value.startsWith("function:"))) {
    const match = /\/unquoted:(?<name>[a-z0-9_]+)\(/u.exec(identity);
    if (!match?.groups)
      throw new MigrationValidationError("COMPATIBILITY_WRITER_FUNCTION", identity);
    createdFunctionIdentityByName.set(match.groups.name!, identity);
  }
  const publicRevokes = new Set(
    classifications
      .filter(
        (classification) =>
          classification.command === "REVOKE" &&
          classification.object_kind === "FUNCTION" &&
          classification.grantee === "PUBLIC",
      )
      .map((classification) => classification.target_identity),
  );
  if (
    publicRevokes.size !== createdFunctionIdentityByName.size ||
    [...createdFunctionIdentityByName.values()].some((identity) => !publicRevokes.has(identity))
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_PUBLIC_ACL", "functions");
  }

  const runtimeHelpers = new Set([
    "compatibility_recovery_registry_digest_v2",
    "compatibility_recovery_state_machine_digest_v2",
    "compatibility_recovery_policy_digest_v2",
    "compatibility_recovery_schema_head_v2",
    "compatibility_recovery_schema_catalog_digest_v2",
    "compatibility_recovery_schema_migration_digest_v2",
    "compatibility_recovery_profile_digest_v2",
    "compatibility_recovery_profile_is_registered_v2",
  ]);
  const expectedRuntimeNames = new Set(runtimeHelpers);
  const expectedBootstrapNames = new Set<string>();
  for (const [functionName, profileId] of operationProfiles) {
    if (
      profileId === "live-instance/v2" ||
      profileId === "retirement-receipt/v2" ||
      profileId === "migration-preflight/v2"
    ) {
      expectedRuntimeNames.add(functionName);
    } else if (profileId === "workload-principal/v2") {
      expectedBootstrapNames.add(functionName);
    }
  }
  const grantNames = (grantee: string): Set<string> =>
    new Set(
      classifications
        .filter(
          (classification) =>
            classification.command === "GRANT" && classification.grantee === grantee,
        )
        .map((classification) => {
          const match = /\/unquoted:(?<name>[a-z0-9_]+)\(/u.exec(classification.target_identity);
          return match?.groups?.name ?? "";
        }),
    );
  const runtimeNames = grantNames("CLOUD_AGENTS_RUNTIME");
  const bootstrapNames = grantNames("CLOUD_AGENTS_BOOTSTRAP_ADMIN");
  if (
    runtimeNames.size !== expectedRuntimeNames.size ||
    [...expectedRuntimeNames].some((name) => !runtimeNames.has(name)) ||
    bootstrapNames.size !== expectedBootstrapNames.size ||
    [...expectedBootstrapNames].some((name) => !bootstrapNames.has(name))
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_GRANT_SET", "functions");
  }

  for (const literal of [
    registryDigest,
    stateMachineDigest,
    policyDigest,
    ...exactSchemaBinding,
    ...expectedProfiles.keys(),
    ...expectedProfiles.values(),
  ]) {
    requireSqlFragment(sql, "compatibility writer binding", literal);
  }
  for (const functionName of operationFunctions) {
    requireSqlFragment(sql, "compatibility writer operation", `cloud_agents.${functionName}(`);
  }
  for (const fragment of [
    "PRIMARY KEY (tenant_id, transition_digest)",
    "cloud_agents.compatibility_recovery_transition_lock_v2(",
    "cloud_agents.compatibility_recovery_lock_v2(",
    "ENABLE ROW LEVEL SECURITY",
    "FORCE ROW LEVEL SECURITY",
    "CONSTRAINT compatibility_recovery_transition_facts_v2_result",
    "CHECK (result_code = 'applied'",
    "result_code := 'observed'",
    "result_code := 'not_observed'",
  ]) {
    requireSqlFragment(sql, "compatibility writer invariant", fragment);
  }
  const executableSql = sql.slice(sql.indexOf("CREATE FUNCTION"));
  if (
    /\b(?:CREATE\s+OR\s+REPLACE|DROP|TRUNCATE|MERGE|CALL|COPY|pg_notify|dblink|http_post|net\.http|aws_lambda)\b/iu.test(
      executableSql,
    ) ||
    /\bGRANT\s+(?:ALL|SELECT|INSERT|UPDATE|DELETE|TRUNCATE|REFERENCES|TRIGGER)\b/iu.test(
      executableSql,
    )
  ) {
    throw new MigrationValidationError("COMPATIBILITY_WRITER_EXTERNAL_EFFECT", "forbidden SQL");
  }
}

export function validateCompatibilityRecoveryPreflightRepair(sqlBytes: Uint8Array): void {
  const sql = new TextDecoder("utf-8", { fatal: true }).decode(sqlBytes);
  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 1) {
    throw new MigrationValidationError(
      "COMPATIBILITY_PREFLIGHT_REPAIR_STATEMENT_COUNT",
      String(statements.length),
    );
  }
  const classification = classifyMigrationStatement(statements[0]!, "000012");
  const expectedIdentity = DECLARED_IDENTITIES_000012.find((identity) =>
    identity.includes("/unquoted:compatibility_recovery_migration_preflight_evaluate_v2("),
  );
  if (
    !expectedIdentity ||
    classification.command !== "CREATE" ||
    classification.object_kind !== "FUNCTION" ||
    classification.target_identity !== expectedIdentity
  ) {
    throw new MigrationValidationError(
      "COMPATIBILITY_PREFLIGHT_REPAIR_TARGET",
      classification.target_identity,
    );
  }
  for (const fragment of [
    "CREATE OR REPLACE FUNCTION cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2(",
    "SECURITY DEFINER",
    "SET search_path = pg_catalog, cloud_agents",
    "preflight_unexpired_instance_incompatible",
    "preflight_expired_instance_unretired",
    "sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973",
    "sha256:e02302ea60eca9855d362d8bcab7efc0466adab6d3a486d828adccdbc5411d7a",
  ]) {
    requireSqlFragment(sql, "compatibility preflight repair", fragment);
  }
  if (sql.includes("instance.drain_state <> 'fenced'")) {
    throw new MigrationValidationError(
      "COMPATIBILITY_PREFLIGHT_REPAIR_FENCED_SHORTCUT",
      "drain_state",
    );
  }
  const exactTwice = [
    "AND NOT EXISTS (",
    "receipt.tenant_id = instance.tenant_id",
    "receipt.service_kind = instance.service_kind",
    "receipt.instance_id = instance.instance_id",
    "receipt.incarnation = instance.incarnation",
    "receipt.rollout_generation = instance.rollout_generation",
    "receipt.writer_epoch = instance.writer_epoch",
    "receipt.state = 'complete'",
    "receipt.credential_revoked",
    "receipt.endpoint_revoked",
    "receipt.process_terminated",
    "receipt.leader_released",
    "receipt.claim_released",
    "receipt.generation_fenced",
    "receipt.receipt_digest IS NOT NULL",
  ];
  for (const fragment of exactTwice) {
    if (sql.split(fragment).length - 1 !== 2) {
      throw new MigrationValidationError(
        "COMPATIBILITY_PREFLIGHT_REPAIR_RECEIPT_BINDING",
        fragment,
      );
    }
  }
  if (
    /\b(?:INSERT|UPDATE|DELETE|TRUNCATE|MERGE|CALL|COPY|DROP|ALTER|GRANT|REVOKE|pg_notify|dblink|http_post|net\.http|aws_lambda)\b/iu.test(
      sql,
    )
  ) {
    throw new MigrationValidationError(
      "COMPATIBILITY_PREFLIGHT_REPAIR_SIDE_EFFECT",
      "forbidden SQL",
    );
  }
}

function assertSqlCheckLiteralSet(
  sql: string,
  constraint: string,
  column: string,
  expected: ReadonlyArray<string>,
): void {
  const pattern = new RegExp(
    `CONSTRAINT\\s+${escapeRegExp(constraint)}\\s+CHECK\\s*\\(\\s*${escapeRegExp(column)}\\s+IN\\s*\\(([^)]*)\\)\\s*\\)`,
    "isu",
  );
  const match = pattern.exec(sql);
  if (!match) throw new MigrationValidationError("COORDINATION_KERNEL_STATE_SET", constraint);
  const body = match[1]!;
  const actual = [...body.matchAll(/'([^']+)'/gu)].map((entry) => entry[1]!).toSorted();
  if (body.replace(/'[^']+'/gu, "").replace(/[\s,]/gu, "") !== "") {
    throw new MigrationValidationError("COORDINATION_KERNEL_STATE_SET", constraint);
  }
  const wanted = [...expected].toSorted();
  if (actual.join("\0") !== wanted.join("\0")) {
    throw new MigrationValidationError("COORDINATION_KERNEL_STATE_SET", constraint);
  }
}

function requireSqlFragment(sql: string, label: string, fragment: string): void {
  if (!sql.includes(fragment)) {
    throw new MigrationValidationError("COORDINATION_KERNEL_BINDING", label);
  }
}

function sqlTableDefinition(sql: string, table: string): string {
  const start = sql.indexOf(`CREATE TABLE cloud_agents.${table} (`);
  if (start < 0) throw new MigrationValidationError("COORDINATION_KERNEL_TABLE", table);
  const end = sql.indexOf("\n);", start);
  if (end < 0) throw new MigrationValidationError("COORDINATION_KERNEL_TABLE", table);
  return sql.slice(start, end + 3);
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/gu, "\\$&");
}

export function validateBuiltinRoleSeedFixture(
  sqlBytes: Uint8Array,
  catalogBytes: Uint8Array,
): void {
  const statements = splitPostgresStatements(sqlBytes);
  if (statements.length !== 46) {
    throw new MigrationValidationError("BUILTIN_ROLE_SEED_MISMATCH", "statement count");
  }
  const catalog = requiredObject(parseStrictMigrationJson(catalogBytes));
  if (
    catalog.apiVersion !== "platform.cloud-agents.dev/v1alpha1" ||
    catalog.kind !== "BuiltinRoleCatalog" ||
    catalog.catalogRevision !== "1" ||
    catalog.publishedAt !== "2026-08-17T00:00:00Z"
  ) {
    throw new MigrationValidationError("BUILTIN_ROLE_SEED_MISMATCH", "catalog identity");
  }
  const roles = requiredArray(catalog.roles).map(requiredObject);
  const expectedRoleStrings: string[] = [];
  const expectedPermissionStrings: string[] = [];
  for (const role of roles) {
    const name = requiredString(role.name, "role name");
    const scope = requiredString(role.scopeLevel, "role scope");
    const state = requiredString(role.state, "role state");
    if (role.version !== 1 || state !== "active") {
      throw new MigrationValidationError("BUILTIN_ROLE_SEED_MISMATCH", name);
    }
    expectedRoleStrings.push(name, scope, state, String(catalog.publishedAt));
    expectedPermissionStrings.push(
      name,
      ...requiredArray(role.permissions).map((permission) =>
        requiredString(permission, "role permission"),
      ),
    );
  }
  if (
    sqlStringLiterals(statements[44]!.bytes).join("\0") !== expectedRoleStrings.join("\0") ||
    sqlStringLiterals(statements[45]!.bytes).join("\0") !== expectedPermissionStrings.join("\0")
  ) {
    throw new MigrationValidationError("BUILTIN_ROLE_SEED_MISMATCH", "seed rows");
  }
}

function sqlStringLiterals(bytes: Uint8Array): string[] {
  const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  return [...text.matchAll(/'([^']*)'/gu)].map((match) => match[1]!);
}

type ProjectionDocuments = {
  readonly catalogDocuments: ReadonlyMap<string, JsonObject>;
  readonly fixtureDocuments: ReadonlyMap<string, JsonObject>;
  readonly rawFixtureFiles: ReadonlyMap<string, Uint8Array>;
};

function buildProjectionDocuments(sqlBytes: ReadonlyMap<string, Uint8Array>): ProjectionDocuments {
  const rawSources = migrationStatementSourceDescriptors(sqlBytes);
  const declared000001 = typedIdentities(DECLARED_IDENTITIES_000001);
  const declared000002 = typedIdentities(DECLARED_IDENTITIES_000002);
  const declared000003 = typedIdentities(DECLARED_IDENTITIES_000003);
  const declared000004 = typedIdentities(DECLARED_IDENTITIES_000004);
  const declared000005 = typedIdentities(DECLARED_IDENTITIES_000005);
  const declared000006 = typedIdentities(DECLARED_IDENTITIES_000006);
  const declared000007 = typedIdentities(DECLARED_IDENTITIES_000007);
  const declared000008 = typedIdentities(DECLARED_IDENTITIES_000008);
  const declared000009 = typedIdentities(DECLARED_IDENTITIES_000009);
  const declared000010 = typedIdentities(DECLARED_IDENTITIES_000010);
  const declared000011 = typedIdentities(DECLARED_IDENTITIES_000011);
  const declared000012 = typedIdentities(DECLARED_IDENTITIES_000012);
  const declared000013 = typedIdentities(DECLARED_IDENTITIES_000013);
  const initialAbsent = initialCatalogState("schema_absent");
  const initialPresent = initialCatalogState("schema_present");
  const namespaceBody = namespaceProjectionBody([
    legacyIdentityToTyped("schema:unquoted:cloud_agents"),
  ]);
  const namespaceAfter: JsonObject = {
    state: "schema_present",
    scope: projectionScope("statement_prefix", null, "000001", 0, [
      legacyIdentityToTyped("schema:unquoted:cloud_agents"),
    ]),
    body: namespaceBody,
  };
  validateCatalogState(namespaceAfter);
  const namespaceTransition: JsonObject = {
    profile: "cloud-agents-platform-statement-transition/v1",
    catalog_before: catalogStateRef(initialAbsent),
    catalog_after: catalogStateRef(namespaceAfter),
    authority_relation: "unchanged_relative_to_verified_binding",
    control_plane_delta: [
      {
        change_kind: "create",
        object: legacyIdentityToTyped("schema:unquoted:cloud_agents"),
        grantee: null,
      },
    ],
  };
  validateExpectedStatementTransition(namespaceTransition);
  const authority = authorityProfile();
  const authorityDigest = migrationDigest(authority);
  const binding = authorityBindingFixture(authorityDigest);
  const projectionModel = catalogProjectionModel();
  const contract = (
    head: string,
    sources: ReadonlyArray<MigrationJson>,
    objects: ReadonlyArray<JsonObject>,
  ): JsonObject => ({
    format_version: "cloud-agents-platform-catalog/v1",
    contract_kind: "cumulative_schema_catalog",
    schema_head: head,
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    source_descriptors: sources,
    projection_model: projectionModel,
    declared_object_identities: objects,
    executable_expected_projection_status: "NOT_IMPLEMENTED_A2_1B_REQUIRED",
  });
  const schema000001 = contract("000001", rawSources.slice(0, 1), declared000001);
  const schema000002 = contract("000002", rawSources.slice(0, 2), declared000002);
  const schema000003 = contract("000003", rawSources.slice(0, 3), declared000003);
  const schema000004 = contract("000004", rawSources.slice(0, 4), declared000004);
  const schema000005 = contract("000005", rawSources.slice(0, 5), declared000005);
  const schema000006 = contract("000006", rawSources.slice(0, 6), declared000006);
  const schema000007 = contract("000007", rawSources.slice(0, 7), declared000007);
  const schema000008 = contract("000008", rawSources.slice(0, 8), declared000008);
  const schema000009 = contract("000009", rawSources.slice(0, 9), declared000009);
  const schema000010 = contract("000010", rawSources.slice(0, 10), declared000010);
  const schema000011 = contract("000011", rawSources.slice(0, 11), declared000011);
  const schema000012 = contract("000012", rawSources.slice(0, 12), declared000012);
  const schema000013 = contract("000013", rawSources, declared000013);
  validateAuthorityProfile(authority);
  validateAuthorityBinding(binding);

  const intermediate = intermediateFixture(authority, binding, namespaceTransition);
  const terminal = terminalFixture(intermediate);
  const numeric = numericFixture();
  validateNumericFixture(numeric);
  validateIntermediateState(intermediate);
  validateAttemptTerminalState(terminal);
  const duplicateAuthorityBinding = duplicateBindingBytes(binding);
  const faults = projectionFaultFixture();
  const defaultACLScope: JsonObject = {
    format_version: "cloud-agents-platform-default-acl-scope-fixture/v1",
    default_acl_owners: ["cloud_agents_migration_owner"],
    object_creator_closure: ["cloud_agents_migration_owner"],
    rows: namespaceBody.default_acl!,
  };
  validateDefaultACLScopeFixture(defaultACLScope);
  const fixtureDocuments = new Map<string, JsonObject>([
    [PROJECTION_FIXTURE_PATHS[1], binding],
    [PROJECTION_FIXTURE_PATHS[2], initialAbsent],
    [PROJECTION_FIXTURE_PATHS[3], initialPresent],
    [PROJECTION_FIXTURE_PATHS[4], namespaceBody],
    [PROJECTION_FIXTURE_PATHS[5], namespaceTransition],
    [PROJECTION_FIXTURE_PATHS[6], numeric],
    [PROJECTION_FIXTURE_PATHS[7], intermediate],
    [PROJECTION_FIXTURE_PATHS[8], terminal],
    [PROJECTION_FIXTURE_PATHS[9], faults],
    [PROJECTION_FIXTURE_PATHS[10], defaultACLScope],
  ]);
  const evidenceFixtures = buildEvidenceContractFixtures();
  for (const [relative, document] of evidenceFixtures.documents) {
    fixtureDocuments.set(`${PROJECTION_FIXTURE_ROOT}/${relative}`, document);
  }
  const rawFixtureFiles = new Map<string, Uint8Array>([
    [AUTHORITY_DUPLICATE_RAW_PATH, duplicateAuthorityBinding],
    [EVIDENCE_DUPLICATE_RAW_PATH, evidenceFixtures.duplicateEvidenceFrame],
    [EVIDENCE_NESTED_DUPLICATE_RAW_PATH, evidenceFixtures.duplicateEvidenceNestedRecord],
    [LINEAGE_DUPLICATE_RAW_PATH, evidenceFixtures.duplicateLineageFrame],
  ]);
  const manifest = projectionFixtureManifest(fixtureDocuments, rawFixtureFiles);
  fixtureDocuments.set(PROJECTION_FIXTURE_PATHS[0], manifest);
  const catalogDocuments = new Map<string, JsonObject>([
    [CATALOG_PATHS[0], authority],
    [CATALOG_PATHS[1], globalAuthorityContractV1()],
    [GLOBAL_TABLE_AUTHORITY_V2_PATH, globalAuthorityContractV2()],
    [GLOBAL_TABLE_AUTHORITY_V3_PATH, globalAuthorityContractV3()],
    [GLOBAL_TABLE_AUTHORITY_V4_PATH, globalAuthorityContractV4()],
    [CATALOG_PATHS[2], schema000001],
    [CATALOG_PATHS[3], schema000002],
    [CATALOG_PATHS[4], schema000003],
    [CATALOG_PATHS[5], schema000004],
    [CATALOG_PATHS[6], schema000005],
    [CATALOG_PATHS[7], schema000006],
    [CATALOG_PATHS[8], schema000007],
    [CATALOG_PATHS[9], schema000008],
    [CATALOG_PATHS[10], schema000009],
    [CATALOG_PATHS[11], schema000010],
    [CATALOG_PATHS[12], schema000011],
    [CATALOG_PATHS[13], schema000012],
    [CATALOG_PATHS[14], schema000013],
  ]);
  return { catalogDocuments, fixtureDocuments, rawFixtureFiles };
}

function authorityProfile(): JsonObject {
  return {
    format_version: "cloud-agents-platform-authority-contract/v1",
    contract_kind: "database_role_authority",
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    database: {
      encoding: "UTF8",
      locale_provider: "libc",
      datcollate: "C",
      datctype: "C",
      icu_locale: null,
      icu_rules: null,
      collation_version: null,
    },
    group_roles: [
      "cloud_agents_migration_owner",
      "cloud_agents_runtime",
      "cloud_agents_bootstrap_admin",
    ],
    required_projection_fields: [
      "phase",
      "session_user",
      "current_user",
      "database_name",
      "database_owner",
      "database_encoding",
      "locale_provider",
      "datcollate",
      "datctype",
      "icu_locale",
      "icu_rules",
      "collation_version",
      "database_acl",
      "roles",
      "direct_memberships",
      "membership_reachability",
      "database_role_settings",
      "effective_create",
      "effective_temporary",
    ],
    required_binding_fields: [
      "authority_profile_digest",
      "deployment_id",
      "issued_at",
      "expires_at",
      "security_epoch",
      "expected_projections",
    ],
  };
}

function authorityBindingFixture(authorityProfileDigest: string): JsonObject {
  const expectedProjections: JsonObject = {};
  for (const phase of ["connected_session", "migration_role", "migration_transaction"] as const) {
    expectedProjections[phase] = authorityProjectionFixture(phase);
  }
  return {
    format_version: "cloud-agents-platform-authority-binding/v1",
    authority_profile_digest: authorityProfileDigest,
    deployment_id: "fixture_pg15_17",
    issued_at: "2026-08-11T00:00:00Z",
    expires_at: "2036-08-11T00:00:00Z",
    security_epoch: 1,
    expected_projections: expectedProjections,
  };
}

function authorityProjectionFixture(phase: string): JsonObject {
  const session = "cloud_agents_migration_login_fixture";
  const bootstrapWorkload = "cloud_agents_bootstrap_login_fixture";
  const runtimeWorkload = "cloud_agents_runtime_login_fixture";
  const owner = "cloud_agents_database_owner_fixture";
  const migration = "cloud_agents_migration_owner";
  const current = phase === "connected_session" ? session : migration;
  const role = (name: string, login: boolean, inherit: boolean, superuser = false): JsonObject => ({
    name,
    login,
    inherit,
    superuser,
    create_role: false,
    create_db: false,
    replication: false,
    bypass_rls: false,
    connection_limit_int32_decimal: "-1",
    valid_until: null,
    config: [],
  });
  const roles = [
    role("cloud_agents_bootstrap_admin", false, false),
    role(bootstrapWorkload, true, true),
    role(owner, false, false),
    role(session, true, false),
    role(migration, false, false),
    role("cloud_agents_runtime", false, false),
    role(runtimeWorkload, true, true),
    role("fixture_cluster_superuser", true, true, true),
  ];
  const effectiveCreate: JsonObject = {
    cloud_agents_bootstrap_admin: false,
    cloud_agents_bootstrap_login_fixture: false,
    cloud_agents_database_owner_fixture: true,
    cloud_agents_migration_login_fixture: false,
    cloud_agents_migration_owner: true,
    cloud_agents_runtime: false,
    cloud_agents_runtime_login_fixture: false,
    fixture_cluster_superuser: true,
  };
  const effectiveTemporary: JsonObject = {
    cloud_agents_bootstrap_admin: false,
    cloud_agents_bootstrap_login_fixture: false,
    cloud_agents_database_owner_fixture: true,
    cloud_agents_migration_login_fixture: false,
    cloud_agents_migration_owner: false,
    cloud_agents_runtime: false,
    cloud_agents_runtime_login_fixture: false,
    fixture_cluster_superuser: true,
  };
  return {
    phase,
    session_user: session,
    current_user: current,
    database_name: "cloud_agents_fixture",
    database_owner: owner,
    database_encoding: "UTF8",
    locale_provider: "libc",
    datcollate: "C",
    datctype: "C",
    icu_locale: null,
    icu_rules: null,
    collation_version: null,
    database_acl: {
      catalog_value: "explicit",
      entries: [
        aclEntry(
          owner,
          owner,
          ["CONNECT", "CREATE", "TEMPORARY"],
          ["CONNECT", "CREATE", "TEMPORARY"],
        ),
        aclEntry(owner, migration, ["CREATE"], []),
      ],
    },
    roles,
    direct_memberships: [
      {
        role: "cloud_agents_bootstrap_admin",
        member: bootstrapWorkload,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: true,
        set_option: true,
      },
      {
        role: migration,
        member: session,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: false,
        set_option: true,
      },
      {
        role: "cloud_agents_runtime",
        member: runtimeWorkload,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: true,
        set_option: true,
      },
    ],
    membership_reachability: [
      {
        role: "cloud_agents_bootstrap_admin",
        member: bootstrapWorkload,
        privileges: [
          reachabilityPrivilege("member", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
          reachabilityPrivilege("usage", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
          reachabilityPrivilege("set", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
        ],
      },
      {
        role: migration,
        member: session,
        privileges: [
          reachabilityPrivilege("member", [session, migration], 3),
          reachabilityPrivilege("usage", null, 3),
          reachabilityPrivilege("set", [session, migration], 3),
        ],
      },
      {
        role: "cloud_agents_runtime",
        member: runtimeWorkload,
        privileges: [
          reachabilityPrivilege("member", [runtimeWorkload, "cloud_agents_runtime"], 3),
          reachabilityPrivilege("usage", [runtimeWorkload, "cloud_agents_runtime"], 3),
          reachabilityPrivilege("set", [runtimeWorkload, "cloud_agents_runtime"], 3),
        ],
      },
    ],
    database_role_settings: [],
    effective_create: effectiveCreate,
    effective_temporary: effectiveTemporary,
  };
}

function reachabilityPrivilege(
  kind: string,
  witness: ReadonlyArray<string> | null,
  edgeCount: number,
): JsonObject {
  return {
    privilege_kind: kind,
    reachable: witness !== null,
    min_depth: witness === null ? null : witness.length - 1,
    canonical_witness: witness,
    edge_count: edgeCount,
  };
}

function aclEntry(
  grantor: string,
  grantee: string,
  privileges: ReadonlyArray<string>,
  grantable: ReadonlyArray<string>,
  origin = "catalog_explicit",
): JsonObject {
  return { grantor, grantee, privileges, grantable, origin };
}

function initialCatalogState(kind: "schema_absent" | "schema_present"): JsonObject {
  const scope = projectionScope("predecessor", null, "000001", null, []);
  if (kind === "schema_absent") {
    return { state: kind, scope, schema: "cloud_agents" };
  }
  return {
    state: kind,
    scope,
    body: {
      schema: {
        name: "cloud_agents",
        owner: "cloud_agents_migration_owner",
        explicit_acl: { catalog_value: "null", entries: [] },
        effective_acl: [
          aclEntry(
            "cloud_agents_migration_owner",
            "cloud_agents_migration_owner",
            ["CREATE", "USAGE"],
            ["CREATE", "USAGE"],
            "owner_implicit",
          ),
        ],
        comment: null,
        security_labels: [],
      },
      default_acl: [],
      relations: [],
      functions: [],
      dependencies: [],
      object_count: 0,
      declared_objects: [],
      denied_objects: [],
    },
  };
}

function namespaceProjectionBody(declared: ReadonlyArray<JsonObject>): JsonObject {
  const owner = "cloud_agents_migration_owner";
  return {
    schema: {
      name: "cloud_agents",
      owner,
      explicit_acl: {
        catalog_value: "explicit",
        entries: [
          aclEntry(owner, "cloud_agents_bootstrap_admin", ["USAGE"], []),
          aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"]),
          aclEntry(owner, "cloud_agents_runtime", ["USAGE"], []),
        ],
      },
      effective_acl: [
        aclEntry(owner, "cloud_agents_bootstrap_admin", ["USAGE"], []),
        aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"], "owner_implicit"),
        aclEntry(owner, "cloud_agents_runtime", ["USAGE"], []),
      ],
      comment: null,
      security_labels: [],
    },
    default_acl: [
      defaultACLProjection(null, "function", [aclEntry(owner, owner, ["EXECUTE"], ["EXECUTE"])]),
      defaultACLProjection(null, "schema", [
        aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"]),
      ]),
      defaultACLProjection(null, "sequence", [
        aclEntry(owner, owner, ["SELECT", "UPDATE", "USAGE"], ["SELECT", "UPDATE", "USAGE"]),
      ]),
      defaultACLProjection(null, "table", [
        aclEntry(
          owner,
          owner,
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
        ),
      ]),
      defaultACLProjection(null, "type", [aclEntry(owner, owner, ["USAGE"], ["USAGE"])]),
      defaultACLProjection("cloud_agents", "function", [
        aclEntry(owner, "cloud_agents_bootstrap_admin", ["EXECUTE"], []),
        aclEntry(owner, owner, ["EXECUTE"], ["EXECUTE"]),
      ]),
      defaultACLProjection("cloud_agents", "sequence", [
        aclEntry(owner, owner, ["SELECT", "UPDATE", "USAGE"], ["SELECT", "UPDATE", "USAGE"]),
      ]),
      defaultACLProjection("cloud_agents", "table", [
        aclEntry(
          owner,
          owner,
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
        ),
        aclEntry(owner, "cloud_agents_runtime", ["SELECT"], []),
      ]),
      defaultACLProjection("cloud_agents", "type", []),
    ],
    relations: [],
    functions: [],
    dependencies: [],
    object_count: declared.length,
    declared_objects: declared,
    denied_objects: [],
  };
}

function defaultACLProjection(
  schema: string | null,
  kind: string,
  entries: ReadonlyArray<JsonObject>,
): JsonObject {
  return {
    owner: "cloud_agents_migration_owner",
    schema,
    object_kind: kind,
    acl: {
      catalog_value: "explicit",
      entries: entries.map((entry) => ({ ...entry, origin: "default_acl_catalog" })),
    },
  };
}

function projectionScope(
  kind: "predecessor" | "statement_prefix" | "final",
  head: string | null,
  migration: string | null,
  index: number | null,
  declared: ReadonlyArray<JsonObject>,
): JsonObject {
  return {
    scope_kind: kind,
    schema_head: head,
    migration_id: migration,
    through_statement_index: index,
    declared_objects: declared,
  };
}

function catalogStateRef(state: JsonObject): JsonObject {
  return {
    scope: requiredObject(state.scope),
    state_kind: state.state!,
    digest: catalogStateDigest(state),
  };
}

function catalogProjectionModel(): JsonObject {
  return {
    projection_slice: "A2.1a_namespace_only",
    catalog_projection_fields: ["schema_head", "body"],
    body_fields: [
      "schema",
      "default_acl",
      "relations",
      "functions",
      "dependencies",
      "object_count",
      "declared_objects",
      "denied_objects",
    ],
    schema_fields: ["name", "owner", "explicit_acl", "effective_acl", "comment", "security_labels"],
    default_acl_fields: ["owner", "schema", "object_kind", "acl"],
    acl_set_fields: ["catalog_value", "entries"],
    acl_entry_fields: ["grantor", "grantee", "privileges", "grantable", "origin"],
    deferred_to_a2_1b: [
      "relation_projection",
      "function_projection",
      "dependency_projection",
      "expression_projection",
    ],
  };
}

function globalAuthorityContractV1(): JsonObject {
  return {
    format_version: "cloud-agents-platform-global-table-authority/v1",
    contract_kind: "global_table_writer_authority",
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    tables: [
      { name: "schema_migrations", writers: ["cloud_agents_migration_owner"] },
      { name: "workload_database_principals", writers: ["audited_bootstrap_function"] },
      { name: "builtin_roles", writers: ["cloud_agents_migration_owner"] },
      { name: "builtin_role_permissions", writers: ["cloud_agents_migration_owner"] },
    ],
  };
}

function globalAuthorityContractV2(): JsonObject {
  const v1 = globalAuthorityContractV1();
  return {
    ...v1,
    format_version: "cloud-agents-platform-global-table-authority/v2",
    tables: [
      ...requiredArray(v1.tables),
      { name: "leader_leases", writers: ["typed_control_plane_coordination_function"] },
    ],
  };
}

function globalAuthorityContractV3(): JsonObject {
  const v2 = globalAuthorityContractV2();
  return {
    ...v2,
    format_version: "cloud-agents-platform-global-table-authority/v3",
    tables: [
      ...requiredArray(v2.tables),
      { name: "migration_backfills", writers: ["migration_backfill_job"] },
      { name: "schema_restore_evidence", writers: ["audited_migration_admin_job"] },
      { name: "live_instances", writers: ["typed_live_instance_registration_function"] },
      {
        name: "instance_retirement_receipts",
        writers: ["typed_instance_reconciler_function"],
      },
    ],
  };
}

function globalAuthorityContractV4(): JsonObject {
  const v3 = globalAuthorityContractV3();
  const historicalV1 = new Set([
    "workload_database_principals",
    "migration_backfills",
    "schema_restore_evidence",
    "live_instances",
    "instance_retirement_receipts",
  ]);
  return {
    ...v3,
    format_version: "cloud-agents-platform-global-table-authority/v4",
    tables: [
      ...requiredArray(v3.tables).map((value) => {
        const row = requiredObject(value);
        return historicalV1.has(requiredString(row.name, "global authority table"))
          ? { ...row, writers: ["historical_v1_no_writer"] }
          : row;
      }),
      {
        name: "compatibility_recovery_workload_principals_v2",
        writers: ["typed_bootstrap_workload_principal_function"],
      },
      {
        name: "compatibility_recovery_backfills_v2",
        writers: ["typed_migration_backfill_function"],
      },
      {
        name: "compatibility_recovery_restore_evidence_v2",
        writers: ["typed_migration_restore_evidence_function"],
      },
      {
        name: "compatibility_recovery_live_instances_v2",
        writers: ["typed_runtime_live_instance_function"],
      },
      {
        name: "compatibility_recovery_retirement_receipts_v2",
        writers: ["typed_runtime_retirement_receipt_function"],
      },
      {
        name: "compatibility_recovery_transition_facts_v2",
        writers: ["typed_compatibility_recovery_function"],
      },
    ],
  };
}

function typedIdentities(identities: ReadonlyArray<string>): JsonObject[] {
  return identities
    .map(legacyIdentityToTyped)
    .toSorted((left, right) =>
      Buffer.compare(
        Buffer.from(objectIdentityKey(left), "utf8"),
        Buffer.from(objectIdentityKey(right), "utf8"),
      ),
    );
}

function legacyIdentityToTyped(value: string): JsonObject {
  const parsed = /^(?<kind>schema|table|function|index|policy):unquoted:(?<payload>.+)$/u.exec(
    value,
  );
  if (!parsed?.groups) throw new MigrationValidationError("OBJECT_IDENTITY", value);
  const kind = parsed.groups.kind!;
  const payload = parsed.groups.payload!;
  if (kind === "schema") return { kind: "schema", name: stripLegacyName(payload) };
  const parts = payload.split("/unquoted:");
  const schema = stripLegacyName(parts[0]!);
  const nameWithSignature = parts[1]!;
  if (kind === "table") {
    return { kind: "relation", identity: { schema, name: nameWithSignature } };
  }
  if (kind === "function") {
    const match = /^(?<name>[a-z0-9_]+)\((?<arguments>.*)\)$/u.exec(nameWithSignature);
    if (!match?.groups) throw new MigrationValidationError("OBJECT_IDENTITY", value);
    const argumentsText = match.groups.arguments!;
    return {
      kind: "function",
      identity: {
        schema,
        name: match.groups.name!,
        arguments:
          argumentsText.length === 0
            ? []
            : argumentsText.split(",unquoted:").map((argument) => ({
                schema: "pg_catalog",
                name: stripLegacyName(argument),
              })),
      },
    };
  }
  if (kind === "index") {
    const relation = indexOwningRelation(nameWithSignature);
    return {
      kind: "index",
      identity: { schema, name: nameWithSignature },
      relation: { schema, name: relation },
    };
  }
  if (kind === "policy") {
    const relation = nameWithSignature.replace(/_(?:runtime_tenant|migration_owner|tenant)$/u, "");
    return { kind: "policy", relation: { schema, name: relation }, name: nameWithSignature };
  }
  throw new MigrationValidationError("OBJECT_IDENTITY", value);
}

function stripLegacyName(value: string): string {
  return value.replace(/^unquoted:/u, "");
}

function indexOwningRelation(index: string): string {
  const prefixes = [
    "tenant_resource_versions",
    "resource_changes",
    "audit_facts",
    "platform_tenants",
    "organizations",
    "projects",
    "builtin_role_permissions",
    "memberships",
    "role_bindings",
    "platform_operations",
    "operation_attempts",
    "terminal_receipts",
    "operation_finalizers",
    "idempotency_records",
    "outbox_events",
    "coordination_audit_facts",
    "leader_leases",
    "workload_database_principals",
    "migration_backfills",
    "schema_restore_evidence",
    "live_instances",
    "instance_retirement_receipts",
    "compatibility_recovery_workload_principals_v2",
    "compatibility_recovery_backfills_v2",
    "compatibility_recovery_restore_evidence_v2",
    "compatibility_recovery_live_instances_v2",
    "compatibility_recovery_retirement_receipts_v2",
    "compatibility_recovery_transition_facts_v2",
  ];
  const relation = prefixes.find((prefix) => index.startsWith(`${prefix}_`));
  if (!relation) throw new MigrationValidationError("INDEX_OWNING_RELATION", index);
  return relation;
}

function numericFixture(): JsonObject {
  return {
    format_version: "cloud-agents-platform-projection-numeric-fixtures/v1",
    signed_integer: [
      numericCase({ bits: 16, input: "-32768", expected: "-32768" }),
      numericCase({ bits: 16, input: "32767", expected: "32767" }),
      numericCase({ bits: 32, input: "-2147483648", expected: "-2147483648" }),
      numericCase({ bits: 32, input: "2147483647", expected: "2147483647" }),
      numericCase({ bits: 64, input: "-9223372036854775808", expected: "-9223372036854775808" }),
      numericCase({ bits: 64, input: "0", expected: "0" }),
      numericCase({ bits: 64, input: "9223372036854775807", expected: "9223372036854775807" }),
      numericCase({
        bits: 64,
        input: "9223372036854775808",
        expected_error: "SIGNED_INT64_OUT_OF_RANGE",
      }),
      numericCase({ bits: 64, input: "-0", expected_error: "INVALID_SIGNED_INT64" }),
    ],
    exact_numeric: [
      numericCase({ input: "0", expected: "0" }),
      numericCase({ input: "0.0", expected: "0" }),
      numericCase({ input: "123.4500", expected: "123.45" }),
      numericCase({ input: "-19.125", expected: "-19.125" }),
      numericCase({ input: "-0.125", expected: "-0.125" }),
      numericCase({ input: "1e3", expected_error: "NUMERIC_FORMAT" }),
      numericCase({ input: "-0.0", expected_error: "NUMERIC_NEGATIVE_ZERO" }),
    ],
    float: [
      numericCase({ kind: "float4", input: "0", expected: "0" }),
      numericCase({ kind: "float4", input: "0.1", expected: "0.1" }),
      numericCase({ kind: "float4", input: "1.1754944e-38", expected: "1.1754944e-38" }),
      numericCase({ kind: "float8", input: "5e-324", expected: "5e-324" }),
      numericCase({ kind: "float8", input: "1.0000000000000002", expected: "1.0000000000000002" }),
      numericCase({
        kind: "float8",
        input: "1.7976931348623157e308",
        expected: "1.7976931348623157e308",
      }),
      numericCase({ kind: "float8", input: "1e+21", expected_error: "FLOAT_FORMAT" }),
      numericCase({ kind: "float8", input: "-0", expected_error: "FLOAT_VALUE" }),
      numericCase({ kind: "float8", input: "NaN", expected_error: "FLOAT_FORMAT" }),
    ],
  };
}

function numericCase(input: JsonObject): JsonObject {
  return {
    ...input,
    expected: input.expected ?? null,
    expected_error: input.expected_error ?? null,
  };
}

function intermediateFixture(
  authority: JsonObject,
  binding: JsonObject,
  transition: JsonObject,
): JsonObject {
  const migrationDigestPlaceholder = `sha256:${"1".repeat(64)}`;
  const catalogDigestPlaceholder = `sha256:${"2".repeat(64)}`;
  const authorityProfileDigest = migrationDigest(authority);
  const authorityBindingDigest = migrationDigest(binding);
  const projections = requiredObject(binding.expected_projections);
  const authorityDigest = authorityProjectionDigest(
    requiredObject(projections.migration_transaction),
  );
  const before = requiredObject(transition.catalog_before);
  const after = requiredObject(transition.catalog_after);
  const body = namespaceProjectionBody([]);
  const stateWithoutDigest: JsonObject = {
    schema_bundle_digest: migrationDigestPlaceholder,
    catalog_contract_digest: catalogDigestPlaceholder,
    authority_profile_digest: authorityProfileDigest,
    authority_binding_digest: authorityBindingDigest,
    migration_id: "000001",
    attempt_index: 1,
    statement_index: 0,
    statement_sha256: `sha256:${"3".repeat(64)}`,
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
      verified_authority_decision_digest: `sha256:${"4".repeat(64)}`,
      schema_owner: "cloud_agents_migration_owner",
      schema_explicit_acl_digest: migrationDigest(
        requiredObject(requiredObject(body.schema).explicit_acl),
      ),
      schema_effective_acl_digest: migrationDigest(requiredObject(body.schema).effective_acl!),
      default_acl_digest: migrationDigest(body.default_acl!),
      expected_transition_digest: migrationDigest(transition),
    },
    authority_before_digest: authorityDigest,
    authority_after_digest: authorityDigest,
    catalog_before_digest: before.digest!,
    catalog_after_digest: after.digest!,
  };
  return {
    ...stateWithoutDigest,
    intermediate_state_digest: migrationDigest({
      domain: "cloud-agents-platform-intermediate-state/v1",
      ...stateWithoutDigest,
    }),
  };
}

function terminalFixture(intermediate: JsonObject): JsonObject {
  const withoutDigest: JsonObject = {
    schema_bundle_digest: intermediate.schema_bundle_digest!,
    catalog_contract_digest: intermediate.catalog_contract_digest!,
    authority_profile_digest: intermediate.authority_profile_digest!,
    authority_binding_digest: intermediate.authority_binding_digest!,
    migration_id: "000001",
    attempt_index: 1,
    previous_attempt_terminal_digest: null,
    last_intermediate_state_digest: intermediate.intermediate_state_digest!,
    outcome: "committed",
    stable_error_code: null,
    failure_evidence: null,
    retry_proof: null,
    reconcile_result: "not_run",
  };
  return {
    ...withoutDigest,
    terminal_digest: migrationDigest({
      domain: "cloud-agents-platform-attempt-terminal/v1",
      ...withoutDigest,
    }),
  };
}

function duplicateBindingBytes(binding: JsonObject): Uint8Array {
  const valid = new TextDecoder().decode(prettyJson(binding));
  return new TextEncoder().encode(
    valid.replace(
      '  "format_version": "cloud-agents-platform-authority-binding/v1",',
      '  "format_version": "cloud-agents-platform-authority-binding/v1",\n  "format_version": "cloud-agents-platform-authority-binding/v1",',
    ),
  );
}

function projectionFaultFixture(): JsonObject {
  return {
    format_version: "cloud-agents-platform-projection-faults/v1",
    cases: [
      {
        name: "authority_unknown",
        target: "authority_binding",
        mutation: "unknown_field",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "authority_missing",
        target: "authority_binding",
        mutation: "missing_expires_at",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "authority_duplicate",
        target: "authority_binding_raw",
        mutation: "duplicate_format_version",
        expected_error: "DUPLICATE_JSON_KEY",
      },
      {
        name: "authority_phase",
        target: "authority_binding",
        mutation: "phase_mismatch",
        expected_error: "AUTHORITY_PHASE",
      },
      {
        name: "authority_digest",
        target: "authority_binding",
        mutation: "bad_profile_digest",
        expected_error: "DIGEST_FORMAT",
      },
      {
        name: "authority_acl",
        target: "authority_binding",
        mutation: "null_acl_with_entries",
        expected_error: "ACL_NULL_ENTRIES",
      },
      {
        name: "catalog_scope",
        target: "catalog_state",
        mutation: "final_without_head",
        expected_error: "PROJECTION_SCOPE",
      },
      {
        name: "predecessor_scope_head",
        target: "catalog_state",
        mutation: "predecessor_with_head",
        expected_error: "PROJECTION_SCOPE",
      },
      {
        name: "absent_schema",
        target: "catalog_state",
        mutation: "wrong_schema_name",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "present_closure",
        target: "catalog_state",
        mutation: "scope_body_declared_mismatch",
        expected_error: "CATALOG_STATE_DECLARED_CLOSURE",
      },
      {
        name: "a21a_relation",
        target: "catalog_body",
        mutation: "relation_nonempty",
        expected_error: "A21A_RELATIONS_NOT_IMPLEMENTED",
      },
      {
        name: "dependency_duplicate",
        target: "catalog_body",
        mutation: "duplicate_dependency",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "denied_duplicate",
        target: "catalog_body",
        mutation: "duplicate_denied_object",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "trigger_owner_variant",
        target: "object_identity",
        mutation: "trigger_owner_not_constraint",
        expected_error: "TRIGGER_OWNING_CONSTRAINT",
      },
      {
        name: "transition_digest",
        target: "expected_transition",
        mutation: "bad_after_digest",
        expected_error: "DIGEST_FORMAT",
      },
      {
        name: "transition_unknown",
        target: "expected_transition",
        mutation: "unknown_field",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "transition_object",
        target: "expected_transition",
        mutation: "open_object_identity",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "numeric_signed",
        target: "numeric",
        mutation: "signed_overflow",
        expected_error: "SIGNED_INT64_OUT_OF_RANGE",
      },
      {
        name: "uint32_overflow",
        target: "intermediate",
        mutation: "statement_index_overflow",
        expected_error: "UINT32_RANGE",
      },
      {
        name: "numeric_exact",
        target: "numeric",
        mutation: "numeric_exponent",
        expected_error: "NUMERIC_FORMAT",
      },
      {
        name: "numeric_float",
        target: "numeric",
        mutation: "float_nan",
        expected_error: "FLOAT_FORMAT",
      },
      {
        name: "intermediate_digest",
        target: "intermediate",
        mutation: "bad_digest",
        expected_error: "INTERMEDIATE_DIGEST",
      },
      {
        name: "intermediate_attempt_link",
        target: "intermediate",
        mutation: "attempt_two_without_previous_terminal",
        expected_error: "INTERMEDIATE_ATTEMPT_LINK",
      },
      {
        name: "intermediate_statement_link",
        target: "intermediate",
        mutation: "statement_one_without_previous_intermediate",
        expected_error: "INTERMEDIATE_STATEMENT_LINK",
      },
      {
        name: "advisory_identity",
        target: "intermediate",
        mutation: "wrong_advisory_domain",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "attempt_outcome",
        target: "attempt_terminal",
        mutation: "illegal_combination",
        expected_error: "ATTEMPT_TERMINAL_COMBINATION",
      },
      {
        name: "attempt_link",
        target: "attempt_terminal",
        mutation: "attempt_two_without_previous_terminal",
        expected_error: "ATTEMPT_TERMINAL_LINK",
      },
      {
        name: "ambiguous_committed_last_digest",
        target: "attempt_terminal",
        mutation: "ambiguous_committed_without_last_digest",
        expected_error: "ATTEMPT_TERMINAL_COMBINATION",
      },
      {
        name: "authority_icu",
        target: "authority_profile",
        mutation: "icu_locale_nonnull",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "acl_surface_origin",
        target: "catalog_body",
        mutation: "default_acl_catalog_explicit_origin",
        expected_error: "ACL_ORIGIN",
      },
      {
        name: "acl_surface_privilege",
        target: "catalog_body",
        mutation: "schema_select_privilege",
        expected_error: "ACL_PRIVILEGE",
      },
      {
        name: "role_config_secret",
        target: "authority_binding",
        mutation: "password_setting",
        expected_error: "ROLE_CONFIG_UNSAFE",
      },
      {
        name: "reachability_complete_edge_count",
        target: "authority_binding",
        mutation: "unreachable_edge_count_zero",
        expected_error: "REACHABILITY_EDGE_COUNT",
      },
      {
        name: "reachability_member_to_role_order",
        target: "canonical_membership_witness_synthetic",
        mutation: "reverse_member_role_witness",
        expected_error: "REACHABILITY_WITNESS",
      },
      {
        name: "reachability_equal_length_noncanonical",
        target: "canonical_membership_witness_synthetic",
        mutation: "select_utf8_later_shortest_path",
        expected_error: "REACHABILITY_WITNESS",
      },
      {
        name: "reachability_duplicate_logical_edge",
        target: "canonical_membership_witness_synthetic",
        mutation: "duplicate_member_role_endpoint",
        expected_error: "DIRECT_MEMBERSHIP_DUPLICATE",
      },
      {
        name: "default_acl_invalid_schema_kind_scope",
        target: "default_acl_scope",
        mutation: "schema_kind_scoped_to_cloud_agents",
        expected_error: "DEFAULT_ACL_SCHEMA_KIND_SCOPE",
      },
      {
        name: "default_acl_unknown_schema",
        target: "default_acl_scope",
        mutation: "schema_outside_closed_scope",
        expected_error: "DEFAULT_ACL_SCOPE",
      },
      {
        name: "default_acl_catalog_value",
        target: "default_acl_scope",
        mutation: "catalog_value_null",
        expected_error: "DEFAULT_ACL_CATALOG_VALUE",
      },
      {
        name: "default_acl_owner_creator_closure",
        target: "default_acl_scope",
        mutation: "owner_outside_creator_closure",
        expected_error: "DEFAULT_ACL_OWNER_CLOSURE",
      },
      {
        name: "default_acl_outer_order",
        target: "default_acl_scope",
        mutation: "reverse_rows",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "stable_error_unknown",
        target: "attempt_terminal",
        mutation: "unknown_projection_error_code",
        expected_error: "STABLE_ERROR_CODE",
      },
      {
        name: "stable_error_runner_code",
        target: "attempt_terminal",
        mutation: "legacy_runner_error_code",
        expected_error: "STABLE_ERROR_CODE",
      },
    ],
  };
}

function projectionFixtureManifest(
  documents: ReadonlyMap<string, JsonObject>,
  rawFiles: ReadonlyMap<string, Uint8Array>,
): JsonObject {
  const cases = [...documents.entries()]
    .map(([path, document]) => ({
      path: path.slice(`${PROJECTION_FIXTURE_ROOT}/`.length),
      size_bytes: prettyJson(document).length,
      sha256: rawSha256(prettyJson(document)),
    }))
    .toSorted((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  for (const [path, bytes] of rawFiles) {
    cases.push({
      path: path.slice(`${PROJECTION_FIXTURE_ROOT}/`.length),
      size_bytes: bytes.length,
      sha256: rawSha256(bytes),
    });
  }
  cases.sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  return {
    format_version: "cloud-agents-platform-projection-fixtures/v1",
    runtime_authority: false,
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    files: cases,
  };
}

function migrationEntry(input: {
  readonly id: string;
  readonly name: string;
  readonly predecessor: string | null;
  readonly schemaFrom: string;
  readonly sql: JsonObject;
  readonly predecessorCatalog: JsonObject;
  readonly catalog: JsonObject;
}): JsonObject {
  return {
    id: input.id,
    name: input.name,
    predecessor_id: input.predecessor,
    phase: "expand",
    schema_from: input.schemaFrom,
    schema_to: input.id,
    compatible_control_plane_min: "0.1.0-alpha.1",
    compatible_control_plane_max: "0.2.0-0",
    compatible_worker_min: "0.1.0-alpha.1",
    compatible_worker_max: "0.2.0-0",
    sql_artifact: input.sql,
    transaction_mode: "transactional",
    reentrancy: "ledger_guarded",
    rollback_boundary: "retain_expanded_schema",
    requires_live_instance_preflight: false,
    requires_pitr_preflight: false,
    predecessor_catalog_contract: input.predecessorCatalog,
    catalog_contract: input.catalog,
  };
}

function initialPredecessorContract(): JsonObject {
  return {
    accepted_states: [initialCatalogState("schema_absent"), initialCatalogState("schema_present")],
  };
}

function validateManifestShape(manifest: JsonObject): void {
  assertKeys(manifest, [
    "format_version",
    "schema_bundle",
    "schema_bundle_digest",
    "bootstrap_bundle",
    "bootstrap_bundle_digest",
    "execution_policy",
    "runtime_artifacts",
    "manifest_digest",
  ]);
  if (manifest.format_version !== "cloud-agents-platform-migration-manifest/v4")
    throw new MigrationValidationError("MANIFEST_VERSION", String(manifest.format_version));
  const policy = requiredObject(manifest.execution_policy);
  assertKeys(policy, [
    "statement_profile",
    "catalog_profile",
    "authority_contract",
    "isolation_level",
    "access_mode",
    "postgres_major_min",
    "postgres_major_max",
    "statement_timeout_ms",
    "lock_timeout_ms",
    "idle_in_transaction_session_timeout_ms",
    "max_attempts",
    "lineage_quota_profile",
  ]);
  if (policy.lineage_quota_profile !== "cloud-agents-platform-lineage-quota-profile/v4") {
    throw new MigrationValidationError(
      "LINEAGE_QUOTA_PROFILE",
      String(policy.lineage_quota_profile),
    );
  }
  for (const field of [
    "schema_bundle_digest",
    "bootstrap_bundle_digest",
    "manifest_digest",
  ] as const) {
    if (typeof manifest[field] !== "string" || !DIGEST.test(manifest[field]))
      throw new MigrationValidationError("DIGEST_FORMAT", field);
  }
  const withoutDigest = { ...manifest };
  delete withoutDigest.manifest_digest;
  if (manifest.manifest_digest !== migrationDigest(withoutDigest))
    throw new MigrationValidationError("MANIFEST_DIGEST", "mismatch");
  const schema = requiredObject(manifest.schema_bundle);
  assertKeys(schema, [
    "lineage",
    "schema_head",
    "advisory_lock",
    "global_table_authority",
    "projection_scope_authority",
    "predecessor_schema_bundle",
    "migrations",
  ]);
  validateProjectionScopeAuthority(schema.projection_scope_authority);
  if (
    manifest.schema_bundle_digest !==
    migrationDigest({ domain: "cloud-agents-platform-schema-bundle/v1", schema_bundle: schema })
  )
    throw new MigrationValidationError("SCHEMA_BUNDLE_DIGEST", "mismatch");
  const bootstrap = requiredObject(manifest.bootstrap_bundle);
  if (
    manifest.bootstrap_bundle_digest !==
    migrationDigest({
      domain: "cloud-agents-platform-bootstrap-bundle/v1",
      bootstrap_bundle: bootstrap,
    })
  ) {
    throw new MigrationValidationError("BOOTSTRAP_BUNDLE_DIGEST", "mismatch");
  }
  const runtime = requiredArray(manifest.runtime_artifacts).map(requiredObject);
  validateRuntimeArtifactSafety(runtime);
  const paths = runtime.map((record) => requiredString(record.path, "artifact path"));
  if (paths.join("\0") !== [...paths].toSorted().join("\0") || new Set(paths).size !== paths.length)
    throw new MigrationValidationError("RUNTIME_ARTIFACT_ORDER", paths.join(","));
  runtime.forEach(validateArtifactShape);
}

export function validateRuntimeArtifactSafety(records: ReadonlyArray<JsonObject>): void {
  for (const record of records) {
    const path = requiredString(record.path, "runtime path");
    if (/authority-binding|\/fixtures\/|secret|credential/iu.test(path)) {
      throw new MigrationValidationError(
        "RUNTIME_TAR_DEPLOYMENT_AUTHORITY",
        `deployment authority or secret material is forbidden: ${path}`,
      );
    }
  }
}

export function validateProjectionScopeAuthority(value: MigrationJson): void {
  const authority = requiredObject(value);
  assertKeys(authority, ["default_acl_owners", "object_creator_closure"]);
  const owners = validateProjectionScopeRoles(
    authority.default_acl_owners,
    "projection_scope_authority.default_acl_owners",
  );
  const creators = validateProjectionScopeRoles(
    authority.object_creator_closure,
    "projection_scope_authority.object_creator_closure",
  );
  const creatorSet = new Set(creators);
  for (const owner of owners) {
    if (!creatorSet.has(owner)) {
      throw new MigrationValidationError(
        "PROJECTION_SCOPE_AUTHORITY_CLOSURE",
        `${owner} is outside object_creator_closure`,
      );
    }
  }
}

function validateProjectionScopeRoles(value: MigrationJson, label: string): string[] {
  const roles = requiredArray(value).map((role) => requiredString(role, label));
  if (roles.length === 0 || roles.length > MAX_PROJECTION_SCOPE_PRINCIPALS) {
    throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_LIMIT", label);
  }
  for (const role of roles) {
    if (role.length === 0 || !role.isWellFormed() || role.includes("\0")) {
      throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_ROLE", `${label}:${role}`);
    }
  }
  const sorted = [...roles].toSorted((left, right) =>
    Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8")),
  );
  if (new Set(roles).size !== roles.length || roles.join("\0") !== sorted.join("\0")) {
    throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_ORDER", label);
  }
  return roles;
}

function validateAdvisoryLock(value: MigrationJson): void {
  const lock = requiredObject(value);
  assertKeys(lock, ["domain", "derivation", "key_int64_decimal"]);
  const domain = requiredString(lock.domain, "advisory domain");
  if (lock.derivation !== "sha256-first-8-bytes-signed-big-endian-int64")
    throw new MigrationValidationError("ADVISORY_DERIVATION", String(lock.derivation));
  const key = parseSignedInt64Decimal(requiredString(lock.key_int64_decimal, "advisory key"));
  if (key !== deriveSignedInt64(domain) || key !== -1047838957622507638n)
    throw new MigrationValidationError("ADVISORY_KEY", key.toString());
}

function artifactRecord(path: string, bytes: Uint8Array): JsonObject {
  return { path, mode: "100644", size_bytes: bytes.length, sha256: digestBytes(bytes) };
}

function validateArtifactShape(record: JsonObject): void {
  assertKeys(record, ["path", "mode", "size_bytes", "sha256"]);
  if (
    record.mode !== "100644" ||
    typeof record.size_bytes !== "number" ||
    typeof record.sha256 !== "string" ||
    !DIGEST.test(record.sha256)
  )
    throw new MigrationValidationError("ARTIFACT_RECORD", String(record.path));
}

function readExactFile(root: string, path: string): Uint8Array {
  const absolute = resolve(root, path);
  const stat = lstatSync(absolute, { throwIfNoEntry: false });
  if (!stat?.isFile() || stat.isSymbolicLink() || (stat.mode & 0o111) !== 0)
    throw new MigrationValidationError("ARTIFACT_FILE", path);
  return readFileSync(absolute);
}

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function prettyJson(value: MigrationJson): Uint8Array {
  return new TextEncoder().encode(`${formatJson(value, 0, 0)}\n`);
}

function formatJson(value: MigrationJson, indent: number, prefixLength: number): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  const padding = " ".repeat(indent);
  const childPadding = " ".repeat(indent + 2);
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    if (value.every((entry) => entry === null || typeof entry !== "object")) {
      const inline = `[${value.map((entry) => JSON.stringify(entry)).join(", ")}]`;
      if (indent + prefixLength + inline.length <= 100) return inline;
    }
    return `[\n${value
      .map((entry) => `${childPadding}${formatJson(entry, indent + 2, 0)}`)
      .join(",\n")}\n${padding}]`;
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return "{}";
  return `{\n${entries
    .map(([key, entry]) => {
      const prefix = `${childPadding}${JSON.stringify(key)}: `;
      return `${prefix}${formatJson(entry, indent + 2, JSON.stringify(key).length + 2)}`;
    })
    .join(",\n")}\n${padding}}`;
}

function compareArtifactPath(left: JsonObject, right: JsonObject): number {
  return Buffer.compare(
    Buffer.from(requiredString(left.path, "path"), "ascii"),
    Buffer.from(requiredString(right.path, "path"), "ascii"),
  );
}

function assertKeys(value: JsonObject, expected: ReadonlyArray<string>): void {
  const actual = Object.keys(value).toSorted();
  const wanted = [...expected].toSorted();
  if (actual.join("\0") !== wanted.join("\0"))
    throw new MigrationValidationError(
      "UNKNOWN_OR_MISSING_FIELD",
      `${actual.join(",")} != ${wanted.join(",")}`,
    );
}

function requiredObject(value: MigrationJson): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new MigrationValidationError("EXPECTED_OBJECT", String(value));
  return value;
}

function requiredArray(value: MigrationJson): MigrationJson[] {
  if (!Array.isArray(value)) throw new MigrationValidationError("EXPECTED_ARRAY", String(value));
  return value;
}

function requiredString(value: MigrationJson, label: string): string {
  if (typeof value !== "string") throw new MigrationValidationError("EXPECTED_STRING", label);
  return value;
}

function canonicalText(value: MigrationJson): string {
  return new TextDecoder().decode(canonicalizeMigrationJson(value));
}
