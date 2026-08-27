import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";

import {
  buildDurableProjectCreateSuccessorCatalog,
  buildMigrationBundle,
  DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH,
  DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH,
  validateCatalogStatementBindings,
} from "./platform-migration-bundle";
import {
  canonicalizeMigrationJson,
  migrationDigest,
  parseStrictMigrationJson,
  type MigrationJson,
} from "./platform-migration-json";
import { type JsonObject } from "./platform-migration-projection";
import {
  createDeterministicUstar,
  readDeterministicUstar,
  type UstarEntry,
} from "./platform-migration-ustar";

/**
 * Versioned, read-only successor for the durable Project-create migration
 * bundle.  The canonical v4 bundle remains the 000013 predecessor; this
 * profile is the only source of truth for the optional 000014 runner input.
 */
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH =
  "services/control-plane/migrations/successor/000014/authority-source.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH =
  "services/control-plane/migrations/successor/000014/manifest.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH =
  "services/control-plane/migrations/successor/000014/schema-bundle.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH =
  "services/control-plane/migrations/successor/000014/profile.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH =
  "services/control-plane/migrations/archive/c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca.schema-bundle.json";

export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_ID = "D-053-MIG-000014";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_REVISION = "D-053-MIG-000014.r1";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/authority-source.schema.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH =
  "services/control-plane/migrations/successor/000014/profile.schema.json";
export const DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_REVIEW_PATH =
  "docs/plan/p1/durable-project-create-migration-bundle-successor-independent-review-20260827.md";

const SUCCESSOR_EVIDENCE_ROOT = "services/control-plane/migrations/successor/000014/evidence";
const RECEIPT_PATHS = [
  {
    kind: "projection",
    path: `${SUCCESSOR_EVIDENCE_ROOT}/projection.json`,
    stage: "projection",
  },
  {
    kind: "member-manifest",
    path: `${SUCCESSOR_EVIDENCE_ROOT}/projection.member-manifest.json`,
    stage: "projection",
  },
  {
    kind: "runner",
    path: `${SUCCESSOR_EVIDENCE_ROOT}/runner.json`,
    stage: "runner",
  },
  {
    kind: "replay-summary",
    path: `${SUCCESSOR_EVIDENCE_ROOT}/replay.json`,
    stage: "replay-summary",
  },
  {
    kind: "independent-review",
    path: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_REVIEW_PATH,
    stage: "independent-review",
  },
] as const;

const CANONICAL_MANIFEST_PATH = "services/control-plane/migrations/manifest.json";
const CANONICAL_SCHEMA_BUNDLE_PATH = "services/control-plane/migrations/schema-bundle.json";
const CANONICAL_CATALOG_000013_PATH =
  "services/control-plane/migrations/catalog/schema-000013.json";
const LOGICAL_RUNTIME_MANIFEST_PATH = CANONICAL_MANIFEST_PATH;
const LOGICAL_RUNTIME_SCHEMA_BUNDLE_PATH = CANONICAL_SCHEMA_BUNDLE_PATH;
const PREDECESSOR_MANIFEST_DIGEST =
  "sha256:56af03a65461e2009cf73c16ac2b1d74d856f68e3efc8b363ab84c537660c4d1";
const PREDECESSOR_SCHEMA_BUNDLE_DIGEST =
  "sha256:c7e08e81b463d04dd267438ac636811200586d5d84d8cb2e8d18799bd2c5faca";
const PREDECESSOR_SCHEMA_BUNDLE_SHA256 =
  "sha256:d5ce27597e2218240a276dbbec01431e4fe26774e195b70445078d8662a3826d";
const SUCCESSOR_PROFILE_FORMAT = "cloud-agents-platform-migration-successor/v1";
const SUCCESSOR_ID = "cloud-agents/platform/migrations/durable-project-create-identifiers";
const SUCCESSOR_SCHEMA_DOMAIN = "cloud-agents-platform-schema-bundle/v1";
const SUCCESSOR_PROFILE_DOMAIN = "cloud-agents-platform-migration-successor/v1";
const SOURCE_SCHEMA_URL =
  "https://schemas.cloud-agents.dev/platform/migrations/successor/000014/authority-source.schema.json";
const PROFILE_SCHEMA_URL =
  "https://schemas.cloud-agents.dev/platform/migrations/successor/000014/profile.schema.json";
const INPUT_SCOPE = "generator-and-focused-verification-closure/v1";
const EXPECTED_RUNNER = {
  entrypoint: "services/control-plane/internal/localmigration.Run",
  productionEntrypoint: "services/control-plane/internal/migration.Runner.Run",
  mode: "localdev_only",
  logicalManifestPath: CANONICAL_MANIFEST_PATH,
  logicalSchemaBundlePath: CANONICAL_SCHEMA_BUNDLE_PATH,
  completeLedger: "no-op",
  entryWriter: "NOT_IMPLEMENTED",
  recoveryWriter: "NOT_IMPLEMENTED",
  externalEffects: "forbidden",
} as const;
const EXPECTED_TOOLCHAIN = { go: "1.26.6", node: "24.18.1", bun: "1.3.14" } as const;
const EXPECTED_PLATFORMS = ["darwin-arm64", "linux-amd64"] as const;
const EXPECTED_LINEAGE_FENCE = {
  kind: "single-predecessor-append-only",
  predecessorHead: "000013",
  successorHead: "000014",
  reviewRule:
    "one fresh independent read-only review; explicit APPROVE or REQUEST_CHANGES with P0/P1/P2; review cannot mutate candidate or close a Gate",
  historicalEvidence: "retain-and-never-rewrite",
} as const;
const EXPECTED_REVIEW_RULES = {
  independentReadOnly: true,
  verdicts: ["APPROVE", "REQUEST_CHANGES"],
  candidateMutation: "forbidden",
  gateTransition: "forbidden",
} as const;
const EXPECTED_IMPLEMENTATION_BOUNDARY = {
  databaseWrites: "not_authorized",
  http: "forbidden",
  p2: "forbidden",
  provider: "forbidden",
  deployment: "forbidden",
  publication: "forbidden",
  gateTransition: "forbidden",
} as const;

const SQL_PATHS_000001_TO_000013 = [
  "services/control-plane/migrations/000001_expand_migration_kernel.sql",
  "services/control-plane/migrations/000002_expand_tenancy.sql",
  "services/control-plane/migrations/000003_expand_membership_rbac.sql",
  "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
  "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
  "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
  "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
  "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
  "services/control-plane/migrations/000009_redact_coordination_conflicts.sql",
  "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
  "services/control-plane/migrations/000011_add_compatibility_recovery_writer.sql",
  "services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql",
  "services/control-plane/migrations/000013_add_durable_project_create_writer.sql",
] as const;

const ARCHIVE_PATHS = [
  "services/control-plane/migrations/archive/52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d.schema-bundle.json",
  "services/control-plane/migrations/archive/54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907.schema-bundle.json",
  "services/control-plane/migrations/archive/6dfd3fed7ba473e6a119a8b6ec3544d88b1a97a4bc5189a6536c64b6fba98110.schema-bundle.json",
  "services/control-plane/migrations/archive/8592d8f96dfeffea9379b1588dddd78909cd558db50b0d40157b7b780581544c.schema-bundle.json",
  "services/control-plane/migrations/archive/9084475d8db1e74afeb0d77ffaf9e253c4e6b6c67c1ba09a7c45483a42cc15ab.schema-bundle.json",
  "services/control-plane/migrations/archive/a1673fcdf71fd49439ec9cefde2d02c627029799a700913653ed1f1f6fca7f09.schema-bundle.json",
  "services/control-plane/migrations/archive/c6652bef99a83b9a8a76739ef7d84e19321feaa80730c548bb7c50191aec3c23.schema-bundle.json",
  "services/control-plane/migrations/archive/efa8240997f191f6e1540897bf391d6ed3c0a921e5958ea97338aec9e3befeec.schema-bundle.json",
] as const;

const CATALOG_PATHS_000001_TO_000013 = [
  "services/control-plane/migrations/catalog/authority-v1.json",
  "services/control-plane/migrations/catalog/global-table-authority-v1.json",
  "services/control-plane/migrations/catalog/schema-000001.json",
  "services/control-plane/migrations/catalog/schema-000002.json",
  "services/control-plane/migrations/catalog/schema-000003.json",
  "services/control-plane/migrations/catalog/schema-000004.json",
  "services/control-plane/migrations/catalog/schema-000005.json",
  "services/control-plane/migrations/catalog/schema-000006.json",
  "services/control-plane/migrations/catalog/schema-000007.json",
  "services/control-plane/migrations/catalog/schema-000008.json",
  "services/control-plane/migrations/catalog/schema-000009.json",
  "services/control-plane/migrations/catalog/schema-000010.json",
  "services/control-plane/migrations/catalog/schema-000011.json",
  "services/control-plane/migrations/catalog/schema-000012.json",
  CANONICAL_CATALOG_000013_PATH,
  DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH,
] as const;

const GLOBAL_AUTHORITY_PATHS = [
  "services/control-plane/migrations/catalog/global-table-authority-v2.json",
  "services/control-plane/migrations/catalog/global-table-authority-v3.json",
  "services/control-plane/migrations/catalog/global-table-authority-v4.json",
] as const;

/**
 * Generated predecessor outputs are protected rather than re-read as mutable
 * semantic inputs.  This list is part of the versioned authority and must be
 * changed only in a new revision.
 */
const PROTECTED_PREDECESSOR_PATHS = [
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json",
  "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json",
  "contracts/generated/platform/v1alpha1/durable-coordination-registry-v2.json",
  "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
  "contracts/generated/platform/v1alpha1/durable-project-create-route-authority-v2.schema.json",
  "contracts/generated/platform/v1alpha1/durable-project-create-route-legacy-v1.schema.json",
  "services/control-plane/internal/coordination/registry_generated.go",
  "services/control-plane/internal/coordination/registry_v2_generated.go",
  "services/control-plane/migrations/catalog/authority-v1.json",
  "services/control-plane/migrations/catalog/global-table-authority-v1.json",
  "services/control-plane/migrations/catalog/global-table-authority-v2.json",
  "services/control-plane/migrations/catalog/global-table-authority-v3.json",
  "services/control-plane/migrations/catalog/global-table-authority-v4.json",
  "services/control-plane/migrations/catalog/schema-000001.json",
  "services/control-plane/migrations/catalog/schema-000002.json",
  "services/control-plane/migrations/catalog/schema-000003.json",
  "services/control-plane/migrations/catalog/schema-000004.json",
  "services/control-plane/migrations/catalog/schema-000005.json",
  "services/control-plane/migrations/catalog/schema-000006.json",
  "services/control-plane/migrations/catalog/schema-000007.json",
  "services/control-plane/migrations/catalog/schema-000008.json",
  "services/control-plane/migrations/catalog/schema-000009.json",
  "services/control-plane/migrations/catalog/schema-000010.json",
  "services/control-plane/migrations/catalog/schema-000011.json",
  "services/control-plane/migrations/catalog/schema-000012.json",
  "services/control-plane/migrations/catalog/schema-000013.json",
  CANONICAL_MANIFEST_PATH,
  CANONICAL_SCHEMA_BUNDLE_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH,
] as const;

/**
 * Complete generator and focused-verification closure.  The source JSON
 * stores the expanded, sorted snapshot; this code-side list is an independent
 * guard against silently dropping a transitive fixture or toolchain input.
 */
const SUCCESSOR_INPUT_PATHS = [
  ".mise.toml",
  ".oxfmtrc.json",
  ".oxlintrc.json",
  "bun.lock",
  "contracts/common/v1alpha1/schemas/identifier.schema.json",
  "contracts/common/v1alpha1/schemas/subject-ref.schema.json",
  "contracts/managed-agent/v1alpha1/openapi.json",
  "contracts/managed-host/v1alpha1/openapi.json",
  "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json",
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-durable-v1alpha1.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-v1alpha1.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v1.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v2.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-route-authority-v2.json",
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-route-v2.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-profile-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-profile-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-source-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-project-create-idempotency-projection.schema.json",
  "contracts/platform/v1alpha1/schemas/managed-agent-create-project-idempotency-projection.schema.json",
  "contracts/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json",
  "contracts/platform/v1alpha1/schemas/permission.schema.json",
  "contracts/platform/v1alpha1/schemas/project.schema.json",
  "docs/plan/adr/0013-p1-durable-coordination-contract.md",
  "docs/plan/adr/0015-p1-compatibility-recovery-contract.md",
  "docs/plan/adr/0017-p1-compatibility-recovery-v2-registry.md",
  "docs/plan/p1/compatibility-recovery-service-entry-blocker-20260820.md",
  "docs/plan/p1/durable-project-create-identifier-hardening-successor-entry-20260826.md",
  "docs/plan/p1/durable-project-create-migration-bundle-successor-20260827.md",
  "package.json",
  "scripts/check-platform-migration-bundle.ts",
  "scripts/generate-platform-compatibility-recovery-registry.ts",
  "scripts/generate-platform-durable-coordination-go-v2.ts",
  "scripts/generate-platform-durable-coordination-registry-v2.ts",
  "scripts/generate-platform-durable-coordination-registry.ts",
  "scripts/generate-platform-migration-bundle-successor.ts",
  "scripts/generate-platform-migration-bundle.ts",
  "scripts/lib/platform-compatibility-recovery-registry.test.ts",
  "scripts/lib/platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-durable-coordination-go-v2.ts",
  "scripts/lib/platform-durable-coordination-registry.test.ts",
  "scripts/lib/platform-durable-coordination-registry.ts",
  "scripts/lib/platform-durable-project-create-identifiers.test.ts",
  "scripts/lib/platform-durable-project-create-lineage-v2.test.ts",
  "scripts/lib/platform-durable-project-create-lineage-v2.ts",
  "scripts/lib/platform-json-semantics.test.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-migration-bundle-successor.test.ts",
  "scripts/lib/platform-migration-bundle-successor.ts",
  "scripts/lib/platform-migration-bundle.test.ts",
  "scripts/lib/platform-migration-bundle.ts",
  "scripts/lib/platform-migration-evidence.test.ts",
  "scripts/lib/platform-migration-evidence.ts",
  "scripts/lib/platform-migration-json.test.ts",
  "scripts/lib/platform-migration-json.ts",
  "scripts/lib/platform-migration-projection.test.ts",
  "scripts/lib/platform-migration-projection.ts",
  "scripts/lib/platform-migration-sql.test.ts",
  "scripts/lib/platform-migration-sql.ts",
  "scripts/lib/platform-migration-ustar.ts",
  "services/control-plane/dependency-lock.json",
  "services/control-plane/go.mod",
  "services/control-plane/go.sum",
  "services/control-plane/internal/coordination/managed_agent_create_project_durable.go",
  "services/control-plane/internal/coordination/managed_agent_create_project_durable_test.go",
  "services/control-plane/internal/coordination/registry.go",
  "services/control-plane/internal/localmigration/localmigration.go",
  "services/control-plane/internal/localmigration/localmigration_test.go",
  "services/control-plane/internal/localmigration/pgx.go",
  "services/control-plane/internal/migration/bundle.go",
  "services/control-plane/internal/migration/contracts.go",
  "services/control-plane/internal/migration/digest.go",
  "services/control-plane/internal/migration/ledger.go",
  "services/control-plane/internal/migration/model.go",
  "services/control-plane/internal/migration/runner.go",
  "services/control-plane/internal/migration/runner_ledger_consumer_profile.go",
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_test.go",
  "services/control-plane/internal/migration/runner_ledger_consumer_service.go",
  "services/control-plane/internal/migration/runner_ledger_consumer_service_test.go",
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile.go",
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_test.go",
  "services/control-plane/internal/migration/runner_ledger_recovery_profile.go",
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go",
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_test.go",
  "services/control-plane/migrations/000001_expand_migration_kernel.sql",
  "services/control-plane/migrations/000002_expand_tenancy.sql",
  "services/control-plane/migrations/000003_expand_membership_rbac.sql",
  "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
  "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
  "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
  "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
  "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
  "services/control-plane/migrations/000009_redact_coordination_conflicts.sql",
  "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
  "services/control-plane/migrations/000011_add_compatibility_recovery_writer.sql",
  "services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql",
  "services/control-plane/migrations/000013_add_durable_project_create_writer.sql",
  DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH,
  "services/control-plane/migrations/README.md",
  "services/control-plane/migrations/archive/52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d.schema-bundle.json",
  "services/control-plane/migrations/archive/54bd987183d6e2d8a7e3ba58a5fa5ee0666015a101193f363f671be294bb2907.schema-bundle.json",
  "services/control-plane/migrations/archive/6dfd3fed7ba473e6a119a8b6ec3544d88b1a97a4bc5189a6536c64b6fba98110.schema-bundle.json",
  "services/control-plane/migrations/archive/8592d8f96dfeffea9379b1588dddd78909cd558db50b0d40157b7b780581544c.schema-bundle.json",
  "services/control-plane/migrations/archive/9084475d8db1e74afeb0d77ffaf9e253c4e6b6c67c1ba09a7c45483a42cc15ab.schema-bundle.json",
  "services/control-plane/migrations/archive/a1673fcdf71fd49439ec9cefde2d02c627029799a700913653ed1f1f6fca7f09.schema-bundle.json",
  "services/control-plane/migrations/archive/c6652bef99a83b9a8a76739ef7d84e19321feaa80730c548bb7c50191aec3c23.schema-bundle.json",
  "services/control-plane/migrations/archive/efa8240997f191f6e1540897bf391d6ed3c0a921e5958ea97338aec9e3befeec.schema-bundle.json",
  "services/control-plane/migrations/bootstrap/database.sql",
  "services/control-plane/migrations/bootstrap/roles.sql",
  "services/control-plane/migrations/fixtures/bundle/golden/ancestor-ledger.json",
  "services/control-plane/migrations/fixtures/bundle/golden/rfc8785.json",
  "services/control-plane/migrations/fixtures/bundle/golden/signed-int64.json",
  "services/control-plane/migrations/fixtures/bundle/golden/sql-split.json",
  "services/control-plane/migrations/fixtures/bundle/golden/ustar.json",
  "services/control-plane/migrations/fixtures/bundle/manifest.json",
  "services/control-plane/migrations/fixtures/bundle/negative/ancestor-cycle.json",
  "services/control-plane/migrations/fixtures/bundle/negative/ancestor-descriptor-cases.json",
  "services/control-plane/migrations/fixtures/bundle/negative/duplicate-key.case.json",
  "services/control-plane/migrations/fixtures/bundle/negative/duplicate-key.raw",
  "services/control-plane/migrations/fixtures/bundle/negative/escaped-equivalent-key.case.json",
  "services/control-plane/migrations/fixtures/bundle/negative/escaped-equivalent-key.raw",
  "services/control-plane/migrations/fixtures/bundle/negative/ledger-rollback.json",
  "services/control-plane/migrations/fixtures/bundle/negative/unicode-whitespace.case.json",
  "services/control-plane/migrations/fixtures/bundle/negative/unicode-whitespace.raw",
  "services/control-plane/migrations/fixtures/projection/golden/attempt-terminal-state-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/authority-binding-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/catalog-projection-body-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/catalog-state-schema-absent-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/catalog-state-schema-present-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/decision-recovery-inputs-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/default-acl-scope-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/evidence-ambiguous-chain-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/evidence-frame-segments-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/evidence-record-chain-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/evidence-retry-chains-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/expected-statement-transition-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/intermediate-state-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/lineage-index-chain-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/numeric-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/recovery-policy-chain-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/supersession-outcomes-v1.json",
  "services/control-plane/migrations/fixtures/projection/golden/terminal-outcomes-v1.json",
  "services/control-plane/migrations/fixtures/projection/manifest.json",
  "services/control-plane/migrations/fixtures/projection/negative/authority-binding-duplicate.raw",
  "services/control-plane/migrations/fixtures/projection/negative/evidence-frame-duplicate.raw",
  "services/control-plane/migrations/fixtures/projection/negative/evidence-framing-faults-v1.json",
  "services/control-plane/migrations/fixtures/projection/negative/evidence-limits-faults-v1.json",
  "services/control-plane/migrations/fixtures/projection/negative/evidence-nested-record-duplicate.raw",
  "services/control-plane/migrations/fixtures/projection/negative/evidence-semantic-faults-v1.json",
  "services/control-plane/migrations/fixtures/projection/negative/faults-v1.json",
  "services/control-plane/migrations/fixtures/projection/negative/lineage-frame-duplicate.raw",
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_SCHEMA_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH,
  "services/control-plane/scripts/data-recovery-validator/main.go",
  "services/control-plane/scripts/data-recovery-validator/main_test.go",
  "tsconfig.base.json",
] as const;

const SUCCESSOR_EXCLUSION_PATHS = [
  "contracts/generated/platform/v1alpha1/durable-project-create-lineage-v2.json",
  "contracts/generation.lock.json",
  "docs/plan/p1/durable-project-create-identifier-hardening-independent-review-20260826.md",
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_REVIEW_PATH,
  "go.work",
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
  DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH,
  `${SUCCESSOR_EVIDENCE_ROOT}/projection.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/projection.member-manifest.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/replay.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/runner.json`,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH,
] as const;

const OPTIONAL_EXCLUSION_PATHS = new Set<string>([
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_REVIEW_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
  DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH,
  `${SUCCESSOR_EVIDENCE_ROOT}/projection.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/projection.member-manifest.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/replay.json`,
  `${SUCCESSOR_EVIDENCE_ROOT}/runner.json`,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH,
  DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH,
]);

export type MigrationSuccessorArtifact = Readonly<{
  path: string;
  mode: "100644";
  sizeBytes: number;
  sha256: string;
}>;

export type DurableProjectCreateMigrationSuccessor = Readonly<{
  source: JsonObject;
  manifest: JsonObject;
  schemaBundle: JsonObject;
  profile: JsonObject;
  catalog: JsonObject;
  runtimeTar: Uint8Array;
  generatedFiles: ReadonlyMap<string, Uint8Array>;
}>;

export class MigrationSuccessorValidationError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(`${code}: ${message}`);
    this.name = "MigrationSuccessorValidationError";
  }
}

/** Build the successor without writing any file or invoking a database. */
export function buildDurableProjectCreateMigrationSuccessor(
  root: string,
): DurableProjectCreateMigrationSuccessor {
  const predecessor = buildMigrationBundle(root);
  assertPredecessor(predecessor.manifest, predecessor.schemaBundleFile);
  const source = readJson(root, DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH);
  assertSource(root, source);

  const predecessorSchemaBytes = readRegular(root, CANONICAL_SCHEMA_BUNDLE_PATH);
  if (
    digestBytes(predecessorSchemaBytes) !== PREDECESSOR_SCHEMA_BUNDLE_SHA256 ||
    predecessorSchemaBytes.length !== 22443
  ) {
    throw new MigrationSuccessorValidationError(
      "PREDECESSOR_SCHEMA_DRIFT",
      CANONICAL_SCHEMA_BUNDLE_PATH,
    );
  }
  const archive = artifact(
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
    predecessorSchemaBytes,
  );
  const sqlBytes = readRegular(root, DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH);
  const sql = artifact(DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH, sqlBytes);
  const catalog = buildDurableProjectCreateSuccessorCatalog(root);
  const catalogBytes = prettyJson(catalog);
  const catalogSql = new Map<string, Uint8Array>([
    ...SQL_PATHS_000001_TO_000013.map((path) => [path, readRegular(root, path)] as const),
    [DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH, sqlBytes],
  ]);
  validateCatalogStatementBindings(catalog, catalogSql);
  const catalogArtifact = artifact(DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH, catalogBytes);
  const predecessorCatalogBytes = requirePredecessorFile(
    predecessor.files,
    CANONICAL_CATALOG_000013_PATH,
  );
  const predecessorCatalog = artifact(CANONICAL_CATALOG_000013_PATH, predecessorCatalogBytes);

  const predecessorSchema = requiredObject(predecessor.schemaBundleFile.schema_bundle);
  const successorMigration = {
    id: "000014",
    name: "harden_durable_project_create_identifiers",
    predecessor_id: "000013",
    phase: "expand",
    schema_from: "000013",
    schema_to: "000014",
    compatible_control_plane_min: "0.1.0-alpha.1",
    compatible_control_plane_max: "0.2.0-0",
    compatible_worker_min: "0.1.0-alpha.1",
    compatible_worker_max: "0.2.0-0",
    sql_artifact: toManifestArtifact(sql),
    transaction_mode: "transactional",
    reentrancy: "ledger_guarded",
    rollback_boundary: "retain_expanded_schema",
    requires_live_instance_preflight: false,
    requires_pitr_preflight: false,
    predecessor_catalog_contract: toManifestArtifact(predecessorCatalog),
    catalog_contract: toManifestArtifact(catalogArtifact),
  } satisfies JsonObject;
  const successorSchemaBody = {
    ...structuredClone(predecessorSchema),
    schema_head: "000014",
    predecessor_schema_bundle: {
      schema_bundle_digest: PREDECESSOR_SCHEMA_BUNDLE_DIGEST,
      ...toManifestArtifact(archive),
    },
    migrations: [...requiredArray(predecessorSchema.migrations), successorMigration],
  } satisfies JsonObject;
  const schemaBundle = {
    format_version: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: successorSchemaBody,
    schema_bundle_digest: migrationDigest({
      domain: SUCCESSOR_SCHEMA_DOMAIN,
      schema_bundle: successorSchemaBody,
    }),
  } satisfies JsonObject;
  const schemaBytes = prettyJson(schemaBundle);

  const predecessorRuntimeEntries = readDeterministicUstar(predecessor.runtimeTar);
  const runtimeWithoutManifest = new Map<string, Uint8Array>();
  for (const entry of predecessorRuntimeEntries) {
    if (entry.path === LOGICAL_RUNTIME_MANIFEST_PATH) continue;
    runtimeWithoutManifest.set(
      entry.path,
      entry.path === LOGICAL_RUNTIME_SCHEMA_BUNDLE_PATH ? schemaBytes : entry.data,
    );
  }
  runtimeWithoutManifest.set(DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH, sqlBytes);
  runtimeWithoutManifest.set(DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH, catalogBytes);
  runtimeWithoutManifest.set(
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
    predecessorSchemaBytes,
  );
  const runtimeArtifacts = [...runtimeWithoutManifest.entries()]
    .toSorted(([left], [right]) => compareAscii(left, right))
    .map(([path, data]) => artifact(path, data));
  const predecessorManifest = predecessor.manifest;
  const manifestBody = {
    ...withoutKey(predecessorManifest, "manifest_digest"),
    schema_bundle: successorSchemaBody,
    schema_bundle_digest: schemaBundle.schema_bundle_digest,
    runtime_artifacts: runtimeArtifacts.map(toManifestArtifact),
  } satisfies JsonObject;
  const manifest = {
    ...manifestBody,
    manifest_digest: migrationDigest(manifestBody),
  } satisfies JsonObject;
  const manifestBytes = prettyJson(manifest);
  const runtimeEntries: UstarEntry[] = [
    { path: LOGICAL_RUNTIME_MANIFEST_PATH, data: manifestBytes },
    ...[...runtimeWithoutManifest.entries()].map(([path, data]) => ({ path, data })),
  ];
  const runtimeTar = createDeterministicUstar(runtimeEntries);

  // The member manifest is a deterministic projection of the exact bytes in
  // the runtime tar.  It is intentionally kept in-memory: the receipt path is
  // ABSENT_PENDING and no projection/evidence writer is authorized by this
  // successor slice.
  const parsedRuntimeEntries = readDeterministicUstar(runtimeTar);
  const memberRecords = parsedRuntimeEntries.map(({ path, data }) => artifact(path, data));
  const memberManifestBody = {
    formatVersion: "cloud-agents-platform-runtime-member-manifest/v1",
    algorithm: "deterministic-ustar-v1",
    order: "ASCII-byte-path",
    records: memberRecords,
  } satisfies JsonObject;
  const memberManifestBytes = prettyJson(memberManifestBody);
  const memberManifest = {
    path: `${SUCCESSOR_EVIDENCE_ROOT}/projection.member-manifest.json`,
    state: "ABSENT_PENDING",
    formatVersion: "cloud-agents-platform-runtime-member-manifest/v1",
    algorithm: "deterministic-ustar-v1",
    order: "ASCII-byte-path",
    recordCount: memberRecords.length,
    sizeBytes: memberManifestBytes.length,
    sha256: digestBytes(memberManifestBytes),
    records: memberRecords,
  } satisfies JsonObject;
  const sourceBytes = readRegular(root, DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH);
  const sourceArtifact = artifact(
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH,
    sourceBytes,
  );
  const profileSchemaArtifact = artifact(
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH,
    readRegular(root, DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH),
  );
  const canonicalManifestBytes = readRegular(root, CANONICAL_MANIFEST_PATH);

  const profileBody = {
    $schema: PROFILE_SCHEMA_URL,
    formatVersion: SUCCESSOR_PROFILE_FORMAT,
    authorityId: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_ID,
    revision: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_REVISION,
    successorId: SUCCESSOR_ID,
    inputScope: INPUT_SCOPE,
    source: sourceArtifact,
    sourceDescriptor: sourceArtifact,
    profileDescriptor: profileSchemaArtifact,
    predecessor: {
      manifest: artifact(CANONICAL_MANIFEST_PATH, canonicalManifestBytes),
      manifestDigest: PREDECESSOR_MANIFEST_DIGEST,
      schemaBundle: artifact(CANONICAL_SCHEMA_BUNDLE_PATH, predecessorSchemaBytes),
      schemaBundleDigest: PREDECESSOR_SCHEMA_BUNDLE_DIGEST,
      schemaHead: "000013",
    },
    successor: {
      migrationId: "000014",
      sql: sql,
      catalog: catalogArtifact,
      archive,
      manifest: artifact(DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH, manifestBytes),
      schemaBundle: artifact(
        DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH,
        schemaBytes,
      ),
    },
    runtime: {
      manifestPath: LOGICAL_RUNTIME_MANIFEST_PATH,
      schemaBundlePath: LOGICAL_RUNTIME_SCHEMA_BUNDLE_PATH,
      memberCount: runtimeEntries.length,
      memberPaths: parsedRuntimeEntries.map(({ path }) => path),
      sizeBytes: runtimeTar.length,
      sha256: digestBytes(runtimeTar),
      compression: "none",
      memberAlgorithm: "deterministic-ustar-v1",
      memberManifest,
    },
    receiptPaths: expectedReceiptPaths(),
    receiptState: expectedReceiptState(),
    runner: EXPECTED_RUNNER,
    toolchain: EXPECTED_TOOLCHAIN,
    platforms: [...EXPECTED_PLATFORMS],
    lineageFence: EXPECTED_LINEAGE_FENCE,
    reviewRules: {
      ...EXPECTED_REVIEW_RULES,
      verdicts: [...EXPECTED_REVIEW_RULES.verdicts],
    },
    implementationBoundary: EXPECTED_IMPLEMENTATION_BOUNDARY,
  } satisfies JsonObject;
  const profile = {
    ...profileBody,
    profileDigest: domainDigest(SUCCESSOR_PROFILE_DOMAIN, profileBody),
  } satisfies JsonObject;
  validateAgainstSchema(
    root,
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH,
    profile,
    "PROFILE_SCHEMA_INVALID",
  );
  const generatedFiles = new Map<string, Uint8Array>([
    [DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH, manifestBytes],
    [DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH, schemaBytes],
    [DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_PATH, prettyJson(profile)],
    [DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH, catalogBytes],
    [DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH, predecessorSchemaBytes],
  ]);
  return { source, manifest, schemaBundle, profile, catalog, runtimeTar, generatedFiles };
}

export function validateCheckedInDurableProjectCreateMigrationSuccessor(
  root: string,
): DurableProjectCreateMigrationSuccessor {
  const expected = buildDurableProjectCreateMigrationSuccessor(root);
  for (const [path, bytes] of expected.generatedFiles) {
    const actual = readRegular(root, path);
    if (!Buffer.from(actual).equals(Buffer.from(bytes))) {
      throw new MigrationSuccessorValidationError("SUCCESSOR_STALE", path);
    }
  }
  return expected;
}

export function validateDurableProjectCreateMigrationSuccessorSource(
  root: string,
  source: JsonObject,
): void {
  assertSource(root, source);
}

function assertPredecessor(manifest: JsonObject, schemaBundleFile: JsonObject): void {
  if (
    requiredString(manifest.schema_bundle_digest) !== PREDECESSOR_SCHEMA_BUNDLE_DIGEST ||
    requiredString(manifest.manifest_digest) !== PREDECESSOR_MANIFEST_DIGEST ||
    requiredString(requiredObject(manifest.schema_bundle).schema_head) !== "000013" ||
    requiredString(schemaBundleFile.schema_bundle_digest) !== PREDECESSOR_SCHEMA_BUNDLE_DIGEST
  ) {
    throw new MigrationSuccessorValidationError("PREDECESSOR_FENCE", "canonical migration bundle");
  }
}

function assertSource(root: string, source: JsonObject): void {
  validateAgainstSchema(
    root,
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_SCHEMA_PATH,
    source,
    "SOURCE_SCHEMA_INVALID",
  );
  if (
    source.$schema !== SOURCE_SCHEMA_URL ||
    source.formatVersion !== "cloud-agents-platform-migration-successor-source/v1" ||
    source.authorityId !== DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_ID ||
    source.revision !== DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_AUTHORITY_REVISION ||
    source.successorId !== SUCCESSOR_ID ||
    source.inputScope !== INPUT_SCOPE
  ) {
    throw new MigrationSuccessorValidationError(
      "SOURCE_IDENTITY",
      DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_PATH,
    );
  }
  assertArtifactDescriptor(
    root,
    source.schemaDescriptor,
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SOURCE_SCHEMA_PATH,
    "SOURCE_SCHEMA_DESCRIPTOR",
  );
  assertArtifactDescriptor(
    root,
    source.profileDescriptor,
    DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_PROFILE_SCHEMA_PATH,
    "SOURCE_PROFILE_DESCRIPTOR",
  );
  const predecessor = requiredObject(source.predecessor);
  if (
    predecessor.manifestPath !== CANONICAL_MANIFEST_PATH ||
    predecessor.manifestDigest !== PREDECESSOR_MANIFEST_DIGEST ||
    predecessor.schemaBundlePath !== CANONICAL_SCHEMA_BUNDLE_PATH ||
    predecessor.schemaBundleDigest !== PREDECESSOR_SCHEMA_BUNDLE_DIGEST ||
    predecessor.schemaHead !== "000013"
  ) {
    throw new MigrationSuccessorValidationError("SOURCE_PREDECESSOR", "predecessor");
  }
  const successor = requiredObject(source.successor);
  const exactSuccessor = {
    migrationId: "000014",
    sqlPath: DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH,
    catalogPath: DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH,
    archivePath: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_ARCHIVE_PATH,
    manifestPath: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_MANIFEST_PATH,
    schemaBundlePath: DURABLE_PROJECT_CREATE_MIGRATION_SUCCESSOR_SCHEMA_BUNDLE_PATH,
  } as const;
  for (const [key, value] of Object.entries(exactSuccessor)) {
    if (successor[key] !== value) {
      throw new MigrationSuccessorValidationError(
        "SOURCE_SUCCESSOR",
        `${key}=${String(successor[key])}`,
      );
    }
  }
  const inputPaths = assertSortedUniquePaths(source.inputPaths, "inputPaths");
  const protectedPaths = assertSortedUniquePaths(source.protectedPaths, "protectedPaths");
  const exclusions = assertSortedUniquePaths(source.exclusionPaths, "exclusionPaths");
  assertExactPathSet(inputPaths, [...SUCCESSOR_INPUT_PATHS], "SOURCE_INPUT_SET");
  assertExactPathSet(protectedPaths, [...PROTECTED_PREDECESSOR_PATHS], "SOURCE_PROTECTED_SET");
  assertExactPathSet(exclusions, [...SUCCESSOR_EXCLUSION_PATHS], "SOURCE_EXCLUSION_SET");
  assertContainsPaths(
    inputPaths,
    [...SQL_PATHS_000001_TO_000013, DURABLE_PROJECT_CREATE_SUCCESSOR_SQL_PATH, ...ARCHIVE_PATHS],
    "SOURCE_INPUT_COMPONENTS",
  );
  assertContainsPaths(protectedPaths, GLOBAL_AUTHORITY_PATHS, "SOURCE_PROTECTED_COMPONENTS");
  for (const path of CATALOG_PATHS_000001_TO_000013) {
    const target =
      path === DURABLE_PROJECT_CREATE_SUCCESSOR_CATALOG_PATH ? exclusions : protectedPaths;
    assertContainsPaths(target, [path], "SOURCE_CATALOG_COMPONENTS");
  }
  assertDisjointPathSets("inputPaths", inputPaths, "protectedPaths", protectedPaths);
  assertDisjointPathSets("inputPaths", inputPaths, "exclusionPaths", exclusions);
  assertDisjointPathSets("protectedPaths", protectedPaths, "exclusionPaths", exclusions);
  for (const path of inputPaths) assertRegularPath(root, path, "SOURCE_INPUT_FILE");
  for (const path of protectedPaths) assertRegularPath(root, path, "SOURCE_PROTECTED_FILE");
  for (const path of exclusions) {
    if (OPTIONAL_EXCLUSION_PATHS.has(path) && !exists(root, path)) continue;
    assertRegularPath(root, path, "SOURCE_EXCLUSION_FILE", false);
  }

  if (!sameJson(requiredObject(source.runner), EXPECTED_RUNNER)) {
    throw new MigrationSuccessorValidationError("SOURCE_RUNNER", "runner");
  }
  if (!sameJson(requiredObject(source.toolchain), EXPECTED_TOOLCHAIN)) {
    throw new MigrationSuccessorValidationError("SOURCE_TOOLCHAIN", "toolchain");
  }
  if (!sameJson(requiredArray(source.platforms), [...EXPECTED_PLATFORMS])) {
    throw new MigrationSuccessorValidationError("SOURCE_PLATFORMS", "platforms");
  }
  if (!sameJson(requiredArray(source.receiptPaths), expectedReceiptPaths())) {
    throw new MigrationSuccessorValidationError("SOURCE_RECEIPTS", "receiptPaths");
  }
  const receiptPaths = requiredArray(source.receiptPaths).map((value) =>
    requiredString(requiredObject(value).path),
  );
  if (new Set(receiptPaths).size !== receiptPaths.length) {
    throw new MigrationSuccessorValidationError("SOURCE_RECEIPT_PATHS", "receiptPaths");
  }
  if (!sameJson(requiredObject(source.receiptState), expectedReceiptState())) {
    throw new MigrationSuccessorValidationError("SOURCE_RECEIPT_STATE", "receiptState");
  }
  if (!sameJson(requiredObject(source.lineageFence), EXPECTED_LINEAGE_FENCE)) {
    throw new MigrationSuccessorValidationError("SOURCE_LINEAGE_FENCE", "lineageFence");
  }
  if (!sameJson(requiredObject(source.reviewRules), EXPECTED_REVIEW_RULES)) {
    throw new MigrationSuccessorValidationError("SOURCE_REVIEW_RULES", "reviewRules");
  }
  if (!sameJson(requiredObject(source.implementationBoundary), EXPECTED_IMPLEMENTATION_BOUNDARY)) {
    throw new MigrationSuccessorValidationError(
      "SOURCE_IMPLEMENTATION_BOUNDARY",
      "implementationBoundary",
    );
  }
  const archiveAlgorithm = requiredObject(source.archiveAlgorithm);
  if (
    archiveAlgorithm.kind !== "exact-byte-copy" ||
    archiveAlgorithm.naming !== "sha256-logical-schema-bundle-digest" ||
    archiveAlgorithm.rewrite !== "forbidden"
  ) {
    throw new MigrationSuccessorValidationError("SOURCE_ARCHIVE_ALGORITHM", "archiveAlgorithm");
  }
  const memberAlgorithm = requiredObject(source.memberAlgorithm);
  if (
    memberAlgorithm.kind !== "deterministic-ustar-v1" ||
    memberAlgorithm.order !== "ASCII-byte-path" ||
    memberAlgorithm.mode !== "100644" ||
    memberAlgorithm.uid !== 0 ||
    memberAlgorithm.gid !== 0 ||
    memberAlgorithm.mtime !== 0 ||
    memberAlgorithm.compression !== "none" ||
    memberAlgorithm.duplicates !== "reject"
  ) {
    throw new MigrationSuccessorValidationError("SOURCE_MEMBER_ALGORITHM", "memberAlgorithm");
  }
}

function expectedReceiptPaths(): JsonObject[] {
  return RECEIPT_PATHS.map(({ kind, path, stage }) => ({
    kind,
    path,
    stage,
    state: "ABSENT_PENDING",
    writeMode: kind === "runner" ? "NO_WRITE" : "CREATE_ONCE_APPEND_ONLY",
  }));
}

function expectedReceiptState(): JsonObject {
  return { current: "AUTHORITY_FROZEN_REVIEW_PENDING", appendOnly: true };
}

function assertArtifactDescriptor(root: string, value: unknown, path: string, code: string): void {
  const expected = artifact(path, readRegular(root, path));
  if (!sameJson(requiredObject(value), expected)) {
    throw new MigrationSuccessorValidationError(code, path);
  }
}

function assertSortedUniquePaths(value: unknown, label: string): string[] {
  const paths = requiredArray(value).map(requiredString);
  const sorted = [...paths].toSorted(compareAscii);
  if (!sameJson(paths, sorted) || new Set(paths).size !== paths.length) {
    throw new MigrationSuccessorValidationError("SOURCE_PATH_ORDER", label);
  }
  return paths;
}

function assertExactPathSet(
  actual: ReadonlyArray<string>,
  expected: ReadonlyArray<string>,
  code: string,
): void {
  const sortedExpected = [...expected].toSorted(compareAscii);
  if (!sameJson(actual, sortedExpected)) {
    throw new MigrationSuccessorValidationError(code, "path set");
  }
}

function assertContainsPaths(
  actual: ReadonlyArray<string>,
  expected: ReadonlyArray<string>,
  code: string,
): void {
  for (const path of expected) {
    if (!actual.includes(path)) {
      throw new MigrationSuccessorValidationError(code, path);
    }
  }
}

function assertDisjointPathSets(
  leftLabel: string,
  left: ReadonlyArray<string>,
  rightLabel: string,
  right: ReadonlyArray<string>,
): void {
  const rightSet = new Set(right);
  const overlap = left.find((path) => rightSet.has(path));
  if (overlap !== undefined) {
    throw new MigrationSuccessorValidationError(
      "SOURCE_PATH_OVERLAP",
      `${leftLabel}/${rightLabel}:${overlap}`,
    );
  }
}

function assertRegularPath(root: string, path: string, code: string, requireMode = true): void {
  try {
    const stat = lstatSync(resolve(root, path));
    if (!stat.isFile() || stat.isSymbolicLink() || (requireMode && (stat.mode & 0o777) !== 0o644)) {
      throw new MigrationSuccessorValidationError(code, path);
    }
  } catch (error) {
    if (error instanceof MigrationSuccessorValidationError) {
      throw new MigrationSuccessorValidationError(code, path);
    }
    throw error;
  }
}

function exists(root: string, path: string): boolean {
  try {
    lstatSync(resolve(root, path));
    return true;
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return false;
    throw error;
  }
}

function requiredObject(value: unknown): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new MigrationSuccessorValidationError("EXPECTED_OBJECT", "object");
  }
  return value as JsonObject;
}

function requiredArray(value: unknown): MigrationJson[] {
  if (!Array.isArray(value)) {
    throw new MigrationSuccessorValidationError("EXPECTED_ARRAY", "array");
  }
  return value as MigrationJson[];
}

function requiredString(value: unknown): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new MigrationSuccessorValidationError("EXPECTED_STRING", "string");
  }
  return value;
}

function readJson(root: string, path: string): JsonObject {
  return requiredObject(parseStrictMigrationJson(readRegular(root, path)));
}

function readRegular(root: string, path: string): Uint8Array {
  const stat = lstatSync(resolve(root, path));
  if (!stat.isFile() || stat.isSymbolicLink() || (stat.mode & 0o777) !== 0o644) {
    throw new MigrationSuccessorValidationError("ARTIFACT_FILE", path);
  }
  return readFileSync(resolve(root, path));
}

function requirePredecessorFile(files: ReadonlyMap<string, Uint8Array>, path: string): Uint8Array {
  const bytes = files.get(path);
  if (!bytes) throw new MigrationSuccessorValidationError("PREDECESSOR_FILE", path);
  return bytes;
}

function artifact(path: string, bytes: Uint8Array): MigrationSuccessorArtifact {
  return { path, mode: "100644", sizeBytes: bytes.length, sha256: digestBytes(bytes) };
}

function toManifestArtifact(value: MigrationSuccessorArtifact): JsonObject {
  return {
    path: value.path,
    mode: value.mode,
    size_bytes: value.sizeBytes,
    sha256: value.sha256,
  };
}

function withoutKey(value: JsonObject, key: string): JsonObject {
  return Object.fromEntries(Object.entries(value).filter(([entry]) => entry !== key));
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

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function domainDigest(domain: string, value: MigrationJson): string {
  return `sha256:${createHash("sha256").update(domain).update("\0").update(canonicalizeMigrationJson(value)).digest("hex")}`;
}

function sameJson(left: unknown, right: unknown): boolean {
  try {
    const leftBytes = canonicalizeMigrationJson(left as MigrationJson);
    const rightBytes = canonicalizeMigrationJson(right as MigrationJson);
    return (
      leftBytes.byteLength === rightBytes.byteLength &&
      leftBytes.every((value, index) => value === rightBytes[index])
    );
  } catch {
    return false;
  }
}

function validateAgainstSchema(
  root: string,
  schemaPath: string,
  value: unknown,
  code: string,
): void {
  try {
    // AJV traverses schema metadata with Object.prototype helpers.  Keep the
    // strict parser for authority documents, but normalize the trusted schema
    // document to an ordinary JSON object before compiling it.
    const schema = JSON.parse(
      new TextDecoder("utf-8", { fatal: true }).decode(readRegular(root, schemaPath)),
    ) as Record<string, unknown>;
    const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });
    const validate = ajv.compile(schema);
    // parseStrictMigrationJson intentionally uses null-prototype records to
    // detect duplicate keys.  AJV expects ordinary records, so validate an
    // equivalent JSON round-trip without weakening the strict parse above.
    const ajvValue = JSON.parse(JSON.stringify(value)) as unknown;
    if (!validate(ajvValue)) {
      throw new MigrationSuccessorValidationError(code, ajv.errorsText(validate.errors));
    }
  } catch (error) {
    if (error instanceof MigrationSuccessorValidationError) throw error;
    throw new MigrationSuccessorValidationError(code, String(error));
  }
}

function compareAscii(left: string, right: string): number {
  return Buffer.compare(Buffer.from(left, "ascii"), Buffer.from(right, "ascii"));
}
