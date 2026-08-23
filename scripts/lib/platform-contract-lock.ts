import { createHash } from "node:crypto";
import { spawnSync } from "node:child_process";
import { lstatSync, readFileSync, readdirSync } from "node:fs";
import { relative, resolve, sep } from "node:path";

import { validatePlatformContractTree } from "./platform-contracts";
import {
  assertCompatibilityRecoveryRegistryCurrent,
  assertCompatibilityRecoveryRegistryV2Current,
  buildCompatibilityRecoveryRegistry,
  buildCompatibilityRecoveryRegistryV2,
  compatibilityRecoveryRegistryInputs,
  compatibilityRecoveryRegistryV2Inputs,
  COMPATIBILITY_RECOVERY_OUTPUT_PATH,
  COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
} from "./platform-compatibility-recovery-registry";
import {
  assertDurableCoordinationRegistryCurrent,
  buildDurableCoordinationRegistry,
  durableCoordinationRegistryInputs,
  DURABLE_COORDINATION_OUTPUT_PATH,
} from "./platform-durable-coordination-registry";
import {
  assertRunnerLedgerPreflightRegistryCurrent,
  buildRunnerLedgerPreflightRegistry,
  runnerLedgerPreflightRegistryInputs,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
} from "./platform-runner-ledger-preflight-registry";
import {
  assertRunnerLedgerConsumerRegistryCurrent,
  buildRunnerLedgerConsumerRegistry,
  runnerLedgerConsumerRegistryInputs,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
} from "./platform-runner-ledger-consumer-registry";
import {
  assertRunnerLedgerEntryAdmissionRegistryCurrent,
  buildRunnerLedgerEntryAdmissionRegistry,
  runnerLedgerEntryAdmissionRegistryInputs,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
} from "./platform-runner-ledger-entry-admission-registry";
import {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent,
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent,
  buildRunnerLedgerEntryExecutionAdmissionRegistry,
  buildRunnerLedgerEntrySuccessWriterRegistry,
  runnerLedgerEntryExecutionAdmissionRegistryInputs,
  runnerLedgerEntrySuccessWriterRegistryInputs,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
} from "./platform-runner-ledger-entry-writer-registry";
import {
  assertRunnerLedgerRecoveryRegistriesCurrent,
  buildRunnerLedgerRecoveryRegistries,
  runnerLedgerRecoveryRegistryInputs,
  RUNNER_LEDGER_RECOVERY_FAMILIES,
  RUNNER_LEDGER_RECOVERY_GENERATOR_SOURCES,
  RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS,
} from "./platform-runner-ledger-recovery-registry";
import {
  assertIdentityVerifierRegistryCurrent,
  buildIdentityVerifierRegistry,
  identityVerifierRegistryInputs,
  IDENTITY_VERIFIER_OUTPUT_PATH,
} from "./platform-identity-verifier-registry";
import {
  assertIdentityVerifierGoCurrent,
  IDENTITY_VERIFIER_GO_OUTPUT_PATH,
} from "./platform-identity-verifier-go";
import {
  assertIdentitySDKCurrent,
  GO_IDENTITY_MANIFEST_PATH,
  GO_IDENTITY_OUTPUT_PATH,
  identitySDKContractInputs,
  identitySDKGeneratorSources,
  TYPESCRIPT_IDENTITY_MANIFEST_PATH,
  TYPESCRIPT_IDENTITY_OUTPUT_PATH,
} from "./platform-identity-sdk";
import {
  assertPlatformJSONSDKCurrent,
  GO_COMMON_JSON_OUTPUT_PATH,
  GO_JSON_MANIFEST_PATH,
  GO_OPENAPI_OUTPUT_PATH,
  GO_PLATFORM_JSON_OUTPUT_PATH,
  platformJSONSDKContractInputs,
  platformJSONSDKGeneratorSources,
  TYPESCRIPT_JSON_MANIFEST_PATH,
  TYPESCRIPT_PLATFORM_OUTPUT_PATH,
} from "./platform-json-sdk";
import {
  assertPlatformProtoSDKCurrent,
  PLATFORM_PROTO_BREAKING_BASELINE_PATH,
  PLATFORM_PROTO_DESCRIPTOR_MANIFEST_PATH,
  PLATFORM_PROTO_DESCRIPTOR_PATH,
  PLATFORM_PROTO_GO_MANIFEST_PATH,
  PLATFORM_PROTO_GO_OUTPUTS,
  PLATFORM_PROTO_TYPESCRIPT_INDEX_PATH,
  PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH,
  PLATFORM_PROTO_TYPESCRIPT_OUTPUTS,
  platformProtoContractInputs,
  platformProtoGeneratorSources,
} from "./platform-proto-sdk";
import { PLATFORM_GO_TOOLCHAIN } from "./platform-go-modules";
import { validateCheckedInMigrationBundle } from "./platform-migration-bundle";

const NODE_VERSION = "24.13.1";
const BUN_VERSION = "1.3.14";
const PYTHON_VERSION = "3.14.7";
const UV_VERSION = "0.12.5";
const AJV_REVIEW = "docs/plan/p1/dependency-reviews/ajv-8.20.0.md";
const CONTRACT_STANDARDS_REVIEW =
  "docs/plan/p1/dependency-reviews/contract-standards-toolchain-20260823.md";
const CONTRACT_STANDARDS_PROFILE = "tools/contract-standards/profile.json";
const CONTRACT_STANDARDS_CORPUS = "tools/contract-standards/vendor/json-schema-test-suite";
const CONTRACT_STANDARDS_FIXED_INPUTS = [
  ".gitattributes",
  ".mise.toml",
  "package.json",
  CONTRACT_STANDARDS_PROFILE,
  CONTRACT_STANDARDS_REVIEW,
  "tools/contract-standards/pyproject.toml",
  "tools/contract-standards/uv.lock",
  "tools/contract-standards/check_contract_standards.py",
  "tools/contract-standards/test_contract_standards.py",
  "scripts/check-platform-contract-standards.ts",
] as const;
type ContractStandardsProfile = {
  readonly formatVersion: string;
  readonly status: string;
  readonly notGateClosure: boolean;
  readonly toolchain: {
    readonly bun: string;
    readonly python: string;
    readonly uv: string;
    readonly pyproject: { readonly path: string; readonly sha256: string };
    readonly lock: { readonly path: string; readonly sha256: string };
  };
  readonly packages: Record<string, string>;
  readonly jsonSchemaOfficialSuite: {
    readonly commit: string;
    readonly tree: string;
    readonly mandatoryTree: string;
    readonly localRoot: string;
    readonly corpusManifestAlgorithm: string;
    readonly corpusManifestSha256: string;
    readonly corpusFiles: number;
    readonly licenseSha256: string;
    readonly mandatoryFiles: number;
    readonly cases: number;
    readonly assertions: number;
    readonly remoteFiles: number;
    readonly expectedFailures: number;
    readonly productionAjvOfficialSuiteAudit: {
      readonly validator: string;
      readonly status: string;
    };
  };
  readonly currentContracts: {
    readonly schemaFiles: number;
    readonly fixtureManifests: number;
    readonly fixtureCases: number;
    readonly crossEngineExactFixtureResults: boolean;
  };
  readonly openapi: {
    readonly documentVersion: string;
    readonly documents: readonly string[];
    readonly documentCount: number;
    readonly operationCount: number;
    readonly expectedFailures: number;
  };
  readonly implementationBoundary: Record<string, string>;
};
const TOOLCHAIN_AUTHORITY_FILES = [".mise.toml", "package.json"] as const;
const PLATFORM_GO_INPUTS = [
  "go.work",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/doc.go",
  "sdk/go/THIRD_PARTY_NOTICES.md",
  GO_IDENTITY_MANIFEST_PATH,
  GO_IDENTITY_OUTPUT_PATH,
  PLATFORM_PROTO_GO_MANIFEST_PATH,
  ...PLATFORM_PROTO_GO_OUTPUTS,
  "services/control-plane/go.mod",
  "services/control-plane/doc.go",
  "services/worker/go.mod",
  "services/worker/doc.go",
] as const;
const IDENTITY_GO_ENVELOPE_INPUTS = [
  "docs/plan/p1/dependency-reviews/x-text-v0.39.0-go-sdk-use-20260820.md",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/doc.go",
  "sdk/go/THIRD_PARTY_NOTICES.md",
  "sdk/go/gen/common/v1alpha1/identity_generated_test.go",
] as const;
const IDENTITY_TYPESCRIPT_ENVELOPE_INPUTS = [
  "package.json",
  "bun.lock",
  "sdk/typescript/package.json",
  "sdk/typescript/tsconfig.json",
  "sdk/typescript/LICENSE",
  "sdk/typescript/README.md",
  "sdk/typescript/src/identity.test.ts",
] as const;
const PROTO_GO_ENVELOPE_INPUTS = [
  "docs/plan/p1/dependency-reviews/proto-sdk-toolchain-20260821.md",
  "docs/plan/p1/sdk-proto-consumer-closure-20260821.md",
  "scripts/test-platform-sdk-consumers.ts",
  "sdk/go/go.mod",
  "sdk/go/go.sum",
  "sdk/go/THIRD_PARTY_NOTICES.md",
  "sdk/go/proto_conformance_test.go",
] as const;
const PROTO_TYPESCRIPT_ENVELOPE_INPUTS = [
  "docs/plan/p1/dependency-reviews/proto-sdk-toolchain-20260821.md",
  "docs/plan/p1/sdk-proto-consumer-closure-20260821.md",
  "package.json",
  "bun.lock",
  "scripts/test-platform-sdk-consumers.ts",
  "sdk/typescript/package.json",
  "sdk/typescript/tsconfig.json",
  "sdk/typescript/THIRD_PARTY_NOTICES.md",
  "sdk/typescript/README.md",
  "sdk/typescript/src/proto.test.ts",
] as const;
const PLATFORM_MIGRATION_FIXED_INPUTS = [
  "docs/plan/adr/0009-p1-migration-bundle-runner.md",
  "docs/plan/adr/0010-p1-postgres-projection-contract.md",
  "docs/plan/adr/0011-p1-membership-rbac-contract.md",
  "docs/plan/adr/0014-p1-lineage-quota-profile-v3.md",
  "docs/plan/adr/0016-p1-compatibility-recovery-postgres-kernel.md",
  "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json",
  "scripts/check-platform-migration-bundle.ts",
  "scripts/generate-platform-migration-bundle.ts",
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
  "services/control-plane/migrations/README.md",
  "services/control-plane/migrations/bootstrap/database.sql",
  "services/control-plane/migrations/bootstrap/roles.sql",
  "services/control-plane/migrations/manifest.json",
  "services/control-plane/migrations/schema-bundle.json",
  "services/control-plane/scripts/test-durable-coordination-kernel-postgres-matrix.sh",
  "services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh",
  "services/control-plane/scripts/test-compatibility-recovery-kernel-postgres-matrix.sh",
  "services/control-plane/scripts/test-compatibility-recovery-preflight-retirement-postgres-matrix.sh",
  "services/control-plane/scripts/test-compatibility-recovery-service-postgres-matrix.sh",
] as const;
const PLATFORM_MIGRATION_INPUT_DIRECTORIES = [
  "services/control-plane/migrations/archive",
  "services/control-plane/migrations/catalog",
  "services/control-plane/migrations/fixtures",
] as const;
const NORMALIZED_MANIFEST_ALGORITHM = "sorted-path-nul-sha256-nul-git-mode-v1";
const DURABLE_COORDINATION_GENERATOR_SOURCES = [
  "scripts/generate-platform-durable-coordination-registry.ts",
  "scripts/lib/platform-durable-coordination-registry.test.ts",
  "scripts/lib/platform-durable-coordination-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const DURABLE_COORDINATION_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-durable-coordination-go.ts",
  "scripts/lib/platform-durable-coordination-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const DURABLE_COORDINATION_GO_OUTPUT_PATH =
  "services/control-plane/internal/coordination/registry_generated.go";
const COMPATIBILITY_RECOVERY_GENERATOR_SOURCES = [
  "docs/plan/adr/0015-p1-compatibility-recovery-contract.md",
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json",
  "scripts/generate-platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-compatibility-recovery-registry.test.ts",
  "scripts/lib/platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const COMPATIBILITY_RECOVERY_V2_GENERATOR_SOURCES = [
  "docs/plan/adr/0017-p1-compatibility-recovery-v2-registry.md",
  "docs/plan/p1/compatibility-recovery-service-entry-blocker-20260820.md",
  "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v2.schema.json",
  "services/control-plane/migrations/catalog/schema-000010.json",
  "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
  "scripts/generate-platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-compatibility-recovery-registry.test.ts",
  "scripts/lib/platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const COMPATIBILITY_RECOVERY_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-compatibility-recovery-go.ts",
  "scripts/lib/platform-compatibility-recovery-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH =
  "services/control-plane/internal/compatibility/registry_generated.go";
const RUNNER_LEDGER_PREFLIGHT_GENERATOR_SOURCES = [
  "docs/plan/adr/0019-p1-runner-ledger-preflight-contract.md",
  "docs/plan/p1/migration-ledger-preflight-entry-blocker-20260821.md",
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json",
  "scripts/generate-platform-runner-ledger-preflight-registry.ts",
  "scripts/lib/platform-runner-ledger-preflight-registry.test.ts",
  "scripts/lib/platform-runner-ledger-preflight-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const RUNNER_LEDGER_PREFLIGHT_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-runner-ledger-preflight-go.ts",
  "scripts/lib/platform-runner-ledger-preflight-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go";
const RUNNER_LEDGER_CONSUMER_GENERATOR_SOURCES = [
  "docs/plan/adr/0020-p1-runner-ledger-consumer-contract.md",
  "docs/plan/p1/runner-ledger-consumer-entry-blocker-20260821.md",
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json",
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
  "scripts/generate-platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.test.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-preflight-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const RUNNER_LEDGER_CONSUMER_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-runner-ledger-consumer-go.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-preflight-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go";
const RUNNER_LEDGER_ENTRY_ADMISSION_GENERATOR_SOURCES = [
  "docs/plan/adr/0021-p1-runner-ledger-entry-admission-contract.md",
  "docs/plan/p1/runner-ledger-consumer-entry-blocker-20260821.md",
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json",
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
  "scripts/generate-platform-runner-ledger-entry-admission-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-entry-admission-registry.test.ts",
  "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
] as const;
const RUNNER_LEDGER_ENTRY_ADMISSION_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-runner-ledger-entry-admission-go.ts",
  "scripts/lib/platform-runner-ledger-consumer-registry.ts",
  "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go";
const RUNNER_LEDGER_ENTRY_WRITER_REGISTRY_GENERATOR_SOURCES = [
  "docs/plan/adr/0022-p1-runner-ledger-entry-success-writer-contract.md",
  "docs/plan/p1/runner-ledger-entry-writer-contract-audit-20260822.md",
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-execution-admission-registry-source-v1.json",
  "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-success-writer-registry-source-v1.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json",
  "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json",
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
  "scripts/generate-platform-runner-ledger-entry-writer-registries.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
  "scripts/lib/platform-runner-ledger-entry-writer-registry.test.ts",
  "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
] as const;
const RUNNER_LEDGER_ENTRY_WRITER_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-runner-ledger-entry-writer-go.ts",
  "scripts/lib/platform-json-semantics.ts",
  "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
  "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
] as const;
const RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go";
const RUNNER_LEDGER_RECOVERY_GO_GENERATOR_SOURCES = [
  ...RUNNER_LEDGER_RECOVERY_GENERATOR_SOURCES,
  "scripts/generate-platform-runner-ledger-recovery-go.ts",
] as const;
const RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go";
const IDENTITY_VERIFIER_REGISTRY_GENERATOR_SOURCES = [
  "scripts/generate-platform-identity-verifier-registry.ts",
  "scripts/lib/platform-identity-verifier-registry.test.ts",
  "scripts/lib/platform-identity-verifier-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;
const IDENTITY_VERIFIER_GO_GENERATOR_SOURCES = [
  "scripts/generate-platform-identity-verifier-go.ts",
  "scripts/lib/platform-identity-verifier-go.ts",
  "scripts/lib/platform-identity-verifier-registry.ts",
  "scripts/lib/platform-json-semantics.ts",
] as const;

const IN_REPO_TOOLS = [
  {
    id: "platform-contract-bootstrap-checker",
    kind: "in-repo-typescript-ajv",
    entrypoint: "scripts/check-platform-contracts.ts",
    sources: [
      "scripts/check-platform-contracts.ts",
      "scripts/lib/platform-contracts.ts",
      "scripts/lib/platform-compatibility-recovery-registry.ts",
      "scripts/lib/platform-durable-coordination-registry.ts",
      "scripts/lib/platform-runner-ledger-preflight-registry.ts",
      "scripts/lib/platform-runner-ledger-consumer-registry.ts",
      "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
      "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
      "scripts/lib/platform-runner-ledger-recovery-registry.ts",
      "scripts/lib/platform-json-semantics.ts",
    ],
  },
  {
    id: "platform-go-module-boundary-checker",
    kind: "in-repo-typescript-go-ast-xmod-test-policy",
    entrypoint: "scripts/check-platform-go-modules.ts",
    sources: [
      "scripts/check-platform-go-modules.ts",
      "scripts/go/importcheck/main.go",
      "scripts/lib/platform-go-modules.ts",
      "services/control-plane/internal/modpolicy/policy_test.go",
    ],
  },
  {
    id: "platform-contract-lock-writer",
    kind: "in-repo-typescript",
    entrypoint: "scripts/generate-platform-contract-lock.ts",
    sources: [
      "scripts/generate-platform-contract-lock.ts",
      "scripts/lib/platform-contract-lock.test.ts",
      "scripts/lib/platform-contract-lock.ts",
      "scripts/lib/platform-contracts.ts",
      "scripts/lib/platform-compatibility-recovery-registry.ts",
      "scripts/lib/platform-durable-coordination-registry.ts",
      "scripts/lib/platform-runner-ledger-preflight-registry.ts",
      "scripts/lib/platform-runner-ledger-consumer-registry.ts",
      "scripts/lib/platform-runner-ledger-entry-admission-registry.ts",
      "scripts/lib/platform-runner-ledger-entry-writer-registry.ts",
      "scripts/lib/platform-runner-ledger-recovery-registry.ts",
      "scripts/lib/platform-identity-verifier-go.ts",
      "scripts/lib/platform-identity-verifier-registry.ts",
      "scripts/lib/platform-identity-sdk.ts",
      "scripts/lib/platform-proto-sdk.ts",
      "scripts/lib/platform-go-modules.ts",
      "scripts/lib/platform-json-semantics.ts",
    ],
  },
  {
    id: "platform-durable-coordination-registry-generator",
    kind: "in-repo-typescript-deterministic-contract-registry",
    entrypoint: "scripts/generate-platform-durable-coordination-registry.ts",
    sources: DURABLE_COORDINATION_GENERATOR_SOURCES,
  },
  {
    id: "platform-durable-coordination-go-generator",
    kind: "in-repo-typescript-deterministic-go-profile",
    entrypoint: "scripts/generate-platform-durable-coordination-go.ts",
    sources: DURABLE_COORDINATION_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-compatibility-recovery-registry-generator",
    kind: "in-repo-typescript-deterministic-contract-registry",
    entrypoint: "scripts/generate-platform-compatibility-recovery-registry.ts",
    sources: COMPATIBILITY_RECOVERY_GENERATOR_SOURCES,
  },
  {
    id: "platform-compatibility-recovery-registry-v2-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-compatibility-recovery-registry.ts",
    sources: COMPATIBILITY_RECOVERY_V2_GENERATOR_SOURCES,
  },
  {
    id: "platform-compatibility-recovery-go-generator",
    kind: "in-repo-typescript-deterministic-versioned-go-profile",
    entrypoint: "scripts/generate-platform-compatibility-recovery-go.ts",
    sources: COMPATIBILITY_RECOVERY_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-preflight-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-runner-ledger-preflight-registry.ts",
    sources: RUNNER_LEDGER_PREFLIGHT_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-preflight-go-generator",
    kind: "in-repo-typescript-deterministic-go-ordinary-fact-profile",
    entrypoint: "scripts/generate-platform-runner-ledger-preflight-go.ts",
    sources: RUNNER_LEDGER_PREFLIGHT_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-consumer-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-runner-ledger-consumer-registry.ts",
    sources: RUNNER_LEDGER_CONSUMER_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-consumer-go-generator",
    kind: "in-repo-typescript-deterministic-go-ordinary-fact-profile",
    entrypoint: "scripts/generate-platform-runner-ledger-consumer-go.ts",
    sources: RUNNER_LEDGER_CONSUMER_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-entry-admission-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-runner-ledger-entry-admission-registry.ts",
    sources: RUNNER_LEDGER_ENTRY_ADMISSION_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-entry-admission-go-generator",
    kind: "in-repo-typescript-deterministic-go-ordinary-profile",
    entrypoint: "scripts/generate-platform-runner-ledger-entry-admission-go.ts",
    sources: RUNNER_LEDGER_ENTRY_ADMISSION_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-entry-writer-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-runner-ledger-entry-writer-registries.ts",
    sources: RUNNER_LEDGER_ENTRY_WRITER_REGISTRY_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-entry-writer-go-generator",
    kind: "in-repo-typescript-deterministic-go-ordinary-profile",
    entrypoint: "scripts/generate-platform-runner-ledger-entry-writer-go.ts",
    sources: RUNNER_LEDGER_ENTRY_WRITER_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-recovery-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry-suite",
    entrypoint: "scripts/generate-platform-runner-ledger-recovery-registries.ts",
    sources: RUNNER_LEDGER_RECOVERY_GENERATOR_SOURCES,
  },
  {
    id: "platform-runner-ledger-recovery-go-generator",
    kind: "in-repo-typescript-deterministic-gofmt-go-ordinary-profile-suite",
    entrypoint: "scripts/generate-platform-runner-ledger-recovery-go.ts",
    sources: RUNNER_LEDGER_RECOVERY_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-identity-verifier-registry-generator",
    kind: "in-repo-typescript-deterministic-versioned-contract-registry",
    entrypoint: "scripts/generate-platform-identity-verifier-registry.ts",
    sources: IDENTITY_VERIFIER_REGISTRY_GENERATOR_SOURCES,
  },
  {
    id: "platform-identity-verifier-go-generator",
    kind: "in-repo-typescript-deterministic-gofmt-go-profile",
    entrypoint: "scripts/generate-platform-identity-verifier-go.ts",
    sources: IDENTITY_VERIFIER_GO_GENERATOR_SOURCES,
  },
  {
    id: "platform-common-identity-sdk-generator",
    kind: "in-repo-typescript-deterministic-go-typescript-sdk",
    entrypoint: "scripts/generate-platform-identity-sdks.ts",
    sources: identitySDKGeneratorSources(),
  },
  {
    id: "platform-json-contract-sdk-generator",
    kind: "in-repo-typescript-deterministic-go-typescript-fixture-sdk",
    entrypoint: "scripts/generate-platform-json-sdks.ts",
    sources: platformJSONSDKGeneratorSources(),
  },
  {
    id: "platform-proto-sdk-generator",
    kind: "in-repo-typescript-deterministic-proto-go-typescript-sdk",
    entrypoint: "scripts/generate-platform-proto-sdks.ts",
    sources: platformProtoGeneratorSources(),
  },
] as const;

export function buildPlatformContractLock(root: string): Record<string, unknown> {
  const summary = validatePlatformContractTree(root);
  const runtimes = validatePlatformToolchains(root);
  const contractStandardsProfile = assertContractStandardsProfileCurrent(root);
  const contractStandardsInputs = [
    ...CONTRACT_STANDARDS_FIXED_INPUTS,
    ...listRegularMigrationInputFiles(root, CONTRACT_STANDARDS_CORPUS),
  ].toSorted();
  const migration = validateCheckedInMigrationBundle(root);
  const migrationInputs = platformMigrationInputs(root);
  assertDurableCoordinationRegistryCurrent(root);
  assertCompatibilityRecoveryRegistryCurrent(root);
  assertCompatibilityRecoveryRegistryV2Current(root);
  assertRunnerLedgerPreflightRegistryCurrent(root);
  assertRunnerLedgerConsumerRegistryCurrent(root);
  assertRunnerLedgerEntryAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root);
  assertRunnerLedgerRecoveryRegistriesCurrent(root);
  assertIdentityVerifierRegistryCurrent(root);
  assertIdentityVerifierGoCurrent(root);
  assertIdentitySDKCurrent(root);
  assertPlatformJSONSDKCurrent(root);
  assertPlatformProtoSDKCurrent(root);
  const durableCoordinationInputs = durableCoordinationRegistryInputs(root);
  const compatibilityRecoveryInputs = compatibilityRecoveryRegistryInputs(root);
  const compatibilityRecoveryV2Inputs = compatibilityRecoveryRegistryV2Inputs(root);
  const runnerLedgerPreflightInputs = runnerLedgerPreflightRegistryInputs(root);
  const runnerLedgerConsumerInputs = runnerLedgerConsumerRegistryInputs(root);
  const runnerLedgerEntryAdmissionInputs = runnerLedgerEntryAdmissionRegistryInputs(root);
  const runnerLedgerEntryExecutionAdmissionInputs =
    runnerLedgerEntryExecutionAdmissionRegistryInputs(root);
  const runnerLedgerEntrySuccessWriterInputs = runnerLedgerEntrySuccessWriterRegistryInputs(root);
  const runnerLedgerRecoveryInputs = runnerLedgerRecoveryRegistryInputs(root);
  const identityVerifierInputs = identityVerifierRegistryInputs(root);
  const identityVerifierGoInputs = [
    ...identityVerifierInputs,
    IDENTITY_VERIFIER_OUTPUT_PATH,
    "scripts/generate-platform-identity-verifier-go.ts",
    "scripts/lib/platform-identity-verifier-go.ts",
    "services/control-plane/internal/authn/profile.go",
    "services/control-plane/internal/authn/profile_test.go",
  ].toSorted();
  const identityContractInputs = identitySDKContractInputs(root);
  const identityGeneratorInputs = identitySDKGeneratorSources();
  const identityGoInputs = [
    ...identityContractInputs,
    ...identityGeneratorInputs,
    ...IDENTITY_GO_ENVELOPE_INPUTS,
  ].toSorted();
  const identityTypeScriptInputs = [
    ...identityContractInputs,
    ...identityGeneratorInputs,
    ...IDENTITY_TYPESCRIPT_ENVELOPE_INPUTS,
  ].toSorted();
  const jsonSDKContractInputs = platformJSONSDKContractInputs(root);
  const jsonSDKGeneratorInputs = platformJSONSDKGeneratorSources();
  const jsonSDKGoInputs = [
    ...jsonSDKContractInputs,
    ...jsonSDKGeneratorInputs,
    "sdk/go/go.mod",
    "sdk/go/go.sum",
  ].toSorted();
  const jsonSDKTypeScriptInputs = [
    ...jsonSDKContractInputs,
    ...jsonSDKGeneratorInputs,
    "sdk/typescript/package.json",
    "sdk/typescript/tsconfig.json",
  ].toSorted();
  const protoContractInputs = platformProtoContractInputs(root);
  const protoGeneratorInputs = platformProtoGeneratorSources();
  const protoDescriptorInputs = [
    ...protoContractInputs,
    ...protoGeneratorInputs,
    "docs/plan/p1/dependency-reviews/proto-sdk-toolchain-20260821.md",
    "package.json",
    "bun.lock",
    "sdk/typescript/THIRD_PARTY_NOTICES.md",
  ].toSorted();
  const protoGoInputs = [
    ...protoContractInputs,
    ...protoGeneratorInputs,
    ...PROTO_GO_ENVELOPE_INPUTS,
  ].toSorted();
  const protoTypeScriptInputs = [
    ...protoContractInputs,
    ...protoGeneratorInputs,
    ...PROTO_TYPESCRIPT_ENVELOPE_INPUTS,
  ].toSorted();
  const compatibilityRecoveryGoInputs = [
    ...COMPATIBILITY_RECOVERY_GO_GENERATOR_SOURCES,
    COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
  ].toSorted();
  const durableCoordinationGoInputs = [
    ...DURABLE_COORDINATION_GO_GENERATOR_SOURCES,
    DURABLE_COORDINATION_OUTPUT_PATH,
  ].toSorted();
  const runnerLedgerPreflightGoInputs = [
    ...RUNNER_LEDGER_PREFLIGHT_GO_GENERATOR_SOURCES,
    RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
    "services/control-plane/internal/migration/runner_ledger_preflight_profile.go",
    "services/control-plane/internal/migration/runner_ledger_preflight_profile_test.go",
  ].toSorted();
  const runnerLedgerConsumerGoInputs = [
    ...RUNNER_LEDGER_CONSUMER_GO_GENERATOR_SOURCES,
    RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
    RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH,
    "services/control-plane/internal/migration/runner_ledger_consumer_profile.go",
    "services/control-plane/internal/migration/runner_ledger_consumer_profile_test.go",
  ].toSorted();
  const runnerLedgerEntryAdmissionGoInputs = [
    ...RUNNER_LEDGER_ENTRY_ADMISSION_GO_GENERATOR_SOURCES,
    RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
    RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH,
    "services/control-plane/internal/migration/runner_ledger_entry_admission_profile.go",
    "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_test.go",
  ].toSorted();
  const runnerLedgerEntryWriterGoInputs = [
    ...RUNNER_LEDGER_ENTRY_WRITER_GO_GENERATOR_SOURCES,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
    RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH,
    "services/control-plane/internal/migration/runner_ledger_entry_writer_profile.go",
    "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_test.go",
  ].toSorted();
  const runnerLedgerRecoveryGoInputs = [
    ...RUNNER_LEDGER_RECOVERY_GO_GENERATOR_SOURCES,
    ...Object.values(RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS),
    RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH,
    RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH,
    RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH,
    RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH,
    "services/control-plane/internal/migration/runner_ledger_recovery_profile.go",
    "services/control-plane/internal/migration/runner_ledger_recovery_profile_test.go",
  ].toSorted();
  const durableCoordinationRegistry = buildDurableCoordinationRegistry(root);
  const compatibilityRecoveryRegistry = buildCompatibilityRecoveryRegistry(root);
  const compatibilityRecoveryRegistryV2 = buildCompatibilityRecoveryRegistryV2(root);
  const runnerLedgerPreflightRegistry = buildRunnerLedgerPreflightRegistry(root);
  const runnerLedgerConsumerRegistry = buildRunnerLedgerConsumerRegistry(root);
  const runnerLedgerEntryAdmissionRegistry = buildRunnerLedgerEntryAdmissionRegistry(root);
  const runnerLedgerEntryExecutionAdmissionRegistry =
    buildRunnerLedgerEntryExecutionAdmissionRegistry(root);
  const runnerLedgerEntrySuccessWriterRegistry = buildRunnerLedgerEntrySuccessWriterRegistry(root);
  const runnerLedgerRecoveryRegistries = buildRunnerLedgerRecoveryRegistries(root);
  const identityVerifierRegistry = buildIdentityVerifierRegistry(root);
  const identityVerifierProfile = identityVerifierRegistry.profile as {
    readonly profileId: string;
    readonly profileDigest: string;
  };
  const durableCoordinationProfile = (
    durableCoordinationRegistry.profiles as ReadonlyArray<{
      readonly profileDigest: string;
      readonly spec: { readonly profileId: string };
    }>
  )[0];
  if (durableCoordinationProfile === undefined) {
    throw new Error("The durable coordination registry must contain its generated profile.");
  }
  return {
    lockVersion: 1,
    status: "BOOTSTRAP_VALIDATED",
    notGateClosure: true,
    sourceContract: {
      manifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
      manifestSha256: summary.contractManifestSha256,
      sourceTreeBinding: "REQUIRED_AT_GATE",
      excludes: ["contracts/generation.lock.json", "contracts/generated/**"],
    },
    dialects: {
      jsonSchema: {
        identity: "https://json-schema.org/draft/2020-12/schema",
        semanticValidation: "BOOTSTRAP_AJV_AND_IN_REPO_SEMANTIC_FIXTURES",
        independentStandardsCandidate:
          "JSONSCHEMA_RS_0_50_1_OFFICIAL_MANDATORY_SUITE_AND_CURRENT_FIXTURES",
        productionAjvOfficialSuiteStatus: "NOT_RUN_NOT_CLAIMED",
      },
      openapi: {
        documentVersion: "3.1.1",
        semanticValidation: "BOOTSTRAP_FAIL_CLOSED_SUBSET",
        independentStandardsCandidate: "OPENAPI_SPEC_VALIDATOR_0_9_0",
      },
      proto: {
        syntax: "proto3",
        descriptorStatus: "GENERATED_EXACT_V1ALPHA1_BASELINE",
        sourceValidation: "PINNED_PROTOC_DESCRIPTOR_AND_IN_REPO_SEMANTIC_FIXTURES",
        descriptorSet: {
          path: PLATFORM_PROTO_DESCRIPTOR_PATH,
          sha256: fileSha256(root, PLATFORM_PROTO_DESCRIPTOR_PATH),
        },
        breakingBaseline: {
          path: PLATFORM_PROTO_BREAKING_BASELINE_PATH,
          sha256: fileSha256(root, PLATFORM_PROTO_BREAKING_BASELINE_PATH),
          policy: "EXACT_BYTES_NO_UNREVIEWED_DELTA",
        },
      },
    },
    runtimes,
    toolchainAuthority: {
      manifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
      manifestSha256: normalizedSourceManifestDigest(root, TOOLCHAIN_AUTHORITY_FILES),
      sources: [...TOOLCHAIN_AUTHORITY_FILES],
      actualRuntimeVerified: true,
    },
    dependencyLock: {
      path: "bun.lock",
      sha256: fileSha256(root, "bun.lock"),
    },
    tools: [
      ...IN_REPO_TOOLS.map((tool) => ({
        id: tool.id,
        kind: tool.kind,
        entrypoint: tool.entrypoint,
        sourceManifestSha256: sourceManifestDigest(root, tool.sources),
        sources: [...tool.sources],
        license: "MIT",
      })),
      {
        id: "platform-migration-bundle-checker",
        kind: "in-repo-typescript-strict-postgresql-bundle",
        entrypoint: "scripts/check-platform-migration-bundle.ts",
        sourceManifestSha256: sourceManifestDigest(root, migrationInputs),
        sources: migrationInputs,
        license: "MIT",
      },
      {
        id: "ajv-2020",
        kind: "npm",
        version: "8.20.0",
        integrity:
          "sha512-Thbli+OlOj+iMPYFBVBfJ3OmCAnaSyNn4M1vz9T6Gka5Jt9ba/HIR56joy65tY6kx/FCF5VXNB819Y7/GUrBGA==",
        license: "MIT",
        reviewEvidence: {
          path: AJV_REVIEW,
          sha256: fileSha256(root, AJV_REVIEW),
          status: "APPROVED",
        },
      },
      {
        id: "ajv-formats",
        kind: "npm",
        version: "3.0.1",
        integrity:
          "sha512-8iUql50EUR+uUcdRQ3HDqa6EVyo3docL8g5WJ3FNcWmu62IbkGUue/pEyLBW8VGKKucTPgqeks4fIU1DA4yowQ==",
        registeredFormats: ["date-time", "uri"],
        license: "MIT",
        reviewEvidence: {
          path: AJV_REVIEW,
          sha256: fileSha256(root, AJV_REVIEW),
          status: "APPROVED",
        },
      },
      {
        id: "platform-contract-standards-checker",
        kind: "in-repo-python-independent-json-schema-openapi-checker",
        entrypoint: "tools/contract-standards/check_contract_standards.py",
        sourceManifestSha256: normalizedSourceManifestDigest(root, contractStandardsInputs),
        sources: contractStandardsInputs,
        license: "MIT",
        status: "IMPLEMENTED_CANDIDATE_INDEPENDENT_REVIEW_PENDING",
      },
      {
        id: "jsonschema-rs",
        kind: "python-wheel-test-only",
        version: contractStandardsProfile.packages["jsonschema-rs"],
        license: "MIT",
        dependencyLock: "tools/contract-standards/uv.lock",
        sourceBuild: "FORBIDDEN",
        productionRuntimeDependency: "FORBIDDEN",
        reviewEvidence: {
          path: CONTRACT_STANDARDS_REVIEW,
          sha256: fileSha256(root, CONTRACT_STANDARDS_REVIEW),
          status: "IMPLEMENTED_CANDIDATE_INDEPENDENT_REVIEW_PENDING",
        },
      },
      {
        id: "openapi-spec-validator",
        kind: "python-wheel-test-only",
        version: contractStandardsProfile.packages["openapi-spec-validator"],
        license: "Apache-2.0",
        dependencyLock: "tools/contract-standards/uv.lock",
        sourceBuild: "FORBIDDEN",
        productionRuntimeDependency: "FORBIDDEN",
        reviewEvidence: {
          path: CONTRACT_STANDARDS_REVIEW,
          sha256: fileSha256(root, CONTRACT_STANDARDS_REVIEW),
          status: "IMPLEMENTED_CANDIDATE_INDEPENDENT_REVIEW_PENDING",
        },
      },
    ],
    pipelines: [
      {
        id: "bootstrap-contract-validation",
        inputManifestSha256: summary.contractManifestSha256,
        outputStatus: "BOOTSTRAP_VALIDATED",
        generatedOutputs: [],
      },
      {
        id: "independent-contract-standards-validation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, contractStandardsInputs),
        inputs: contractStandardsInputs,
        outputStatus: "IMPLEMENTED_CANDIDATE_INDEPENDENT_REVIEW_PENDING",
        notGateClosure: true,
        generatedOutputs: [],
        outputSummary: {
          profile: contractStandardsProfile.formatVersion,
          sourceContractManifestSha256: summary.contractManifestSha256,
          toolchain: {
            bun: contractStandardsProfile.toolchain.bun,
            python: contractStandardsProfile.toolchain.python,
            uv: contractStandardsProfile.toolchain.uv,
          },
          officialJsonSchemaSuite: {
            commit: contractStandardsProfile.jsonSchemaOfficialSuite.commit,
            tree: contractStandardsProfile.jsonSchemaOfficialSuite.tree,
            mandatoryTree: contractStandardsProfile.jsonSchemaOfficialSuite.mandatoryTree,
            files: contractStandardsProfile.jsonSchemaOfficialSuite.mandatoryFiles,
            cases: contractStandardsProfile.jsonSchemaOfficialSuite.cases,
            assertions: contractStandardsProfile.jsonSchemaOfficialSuite.assertions,
            expectedFailures: contractStandardsProfile.jsonSchemaOfficialSuite.expectedFailures,
          },
          productionAjvOfficialSuiteAudit:
            contractStandardsProfile.jsonSchemaOfficialSuite.productionAjvOfficialSuiteAudit,
          currentJsonSchema: {
            schemas: contractStandardsProfile.currentContracts.schemaFiles,
            fixtureManifests: contractStandardsProfile.currentContracts.fixtureManifests,
            fixtureCases: contractStandardsProfile.currentContracts.fixtureCases,
            crossEngineExactFixtureResults:
              contractStandardsProfile.currentContracts.crossEngineExactFixtureResults,
          },
          openapi31: {
            documents: contractStandardsProfile.openapi.documentCount,
            operations: contractStandardsProfile.openapi.operationCount,
            expectedFailures: contractStandardsProfile.openapi.expectedFailures,
          },
          productionRuntimeDependency: "FORBIDDEN",
          independentReview: "PENDING",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "durable-coordination-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, durableCoordinationInputs),
        inputs: durableCoordinationInputs,
        outputStatus: "GENERATED_CONTRACT_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: DURABLE_COORDINATION_OUTPUT_PATH,
            sha256: fileSha256(root, DURABLE_COORDINATION_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, DURABLE_COORDINATION_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryId: durableCoordinationRegistry.registryId,
          registryDigest: durableCoordinationRegistry.registryDigest,
          sourceDigest: durableCoordinationRegistry.sourceDigest,
          stateMachineDigest: durableCoordinationRegistry.stateMachineDigest,
          policyDigest: durableCoordinationRegistry.policyDigest,
          profileCount: (durableCoordinationRegistry.profiles as ReadonlyArray<unknown>).length,
          runtimeConsumer: "GENERATED_GO_PROFILE_TYPED_SERVICE_000008",
          sqlConsumer: "GENERATED_PROFILE_TYPED_FUNCTIONS_000008",
          httpSurface: "NOT_IMPLEMENTED",
          externalSideEffects: "FORBIDDEN",
        },
      },
      {
        id: "durable-coordination-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, durableCoordinationGoInputs),
        inputs: durableCoordinationGoInputs,
        outputStatus: "GENERATED_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: DURABLE_COORDINATION_GO_OUTPUT_PATH,
            sha256: fileSha256(root, DURABLE_COORDINATION_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, DURABLE_COORDINATION_GO_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryDigest: durableCoordinationRegistry.registryDigest,
          stateMachineDigest: durableCoordinationRegistry.stateMachineDigest,
          policyDigest: durableCoordinationRegistry.policyDigest,
          profileId: durableCoordinationProfile.spec.profileId,
          profileDigest: durableCoordinationProfile.profileDigest,
          handWrittenProfileFallback: "FORBIDDEN",
          httpSurface: "NOT_IMPLEMENTED",
          externalSideEffects: "FORBIDDEN",
        },
      },
      {
        id: "compatibility-recovery-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, compatibilityRecoveryInputs),
        inputs: compatibilityRecoveryInputs,
        outputStatus: "GENERATED_COMPATIBILITY_RECOVERY_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: COMPATIBILITY_RECOVERY_OUTPUT_PATH,
            sha256: fileSha256(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryId: compatibilityRecoveryRegistry.registryId,
          registryDigest: compatibilityRecoveryRegistry.registryDigest,
          sourceDigest: compatibilityRecoveryRegistry.sourceDigest,
          stateMachineDigest: compatibilityRecoveryRegistry.stateMachineDigest,
          policyDigest: compatibilityRecoveryRegistry.policyDigest,
          profileCount: (compatibilityRecoveryRegistry.profiles as ReadonlyArray<unknown>).length,
          runtimeConsumer: "NOT_IMPLEMENTED",
          sqlConsumer: "NOT_IMPLEMENTED_NO_000010",
          httpSurface: "NOT_IMPLEMENTED",
          externalSideEffects: "FORBIDDEN",
          localLogicalRestore: "CONTRACT_ONLY_NOT_IMPLEMENTED",
          pitrHa: "P4_NOT_IMPLEMENTED",
        },
      },
      {
        id: "compatibility-recovery-registry-v2-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, compatibilityRecoveryV2Inputs),
        inputs: compatibilityRecoveryV2Inputs,
        outputStatus: "GENERATED_COMPATIBILITY_RECOVERY_REGISTRY_V2",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
            sha256: fileSha256(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryId: compatibilityRecoveryRegistryV2.registryId,
          registryDigest: compatibilityRecoveryRegistryV2.registryDigest,
          sourceDigest: compatibilityRecoveryRegistryV2.sourceDigest,
          stateMachineDigest: compatibilityRecoveryRegistryV2.stateMachineDigest,
          policyDigest: compatibilityRecoveryRegistryV2.policyDigest,
          profileCount: (compatibilityRecoveryRegistryV2.profiles as ReadonlyArray<unknown>).length,
          schemaHead: "000010",
          historicalCompatibility: "V1_SAME_BITS_NON_AUTHORITY",
          runtimeConsumer: "GENERATED_GO_PROFILE_TYPED_SERVICE_000011",
          sqlWriterConsumer: "GENERATED_PROFILE_TYPED_FUNCTIONS_000011",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          externalSideEffects: "FORBIDDEN",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "compatibility-recovery-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, compatibilityRecoveryGoInputs),
        inputs: compatibilityRecoveryGoInputs,
        outputStatus: "GENERATED_VERSIONED_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH,
            sha256: fileSha256(root, COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryDigest: compatibilityRecoveryRegistryV2.registryDigest,
          stateMachineDigest: compatibilityRecoveryRegistryV2.stateMachineDigest,
          policyDigest: compatibilityRecoveryRegistryV2.policyDigest,
          profileCount: (compatibilityRecoveryRegistryV2.profiles as ReadonlyArray<unknown>).length,
          operationCount: 26,
          handWrittenProfileFallback: "FORBIDDEN",
          callerProvidedProfile: "FORBIDDEN",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          externalSideEffects: "FORBIDDEN",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-preflight-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerPreflightInputs),
        inputs: runnerLedgerPreflightInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_PREFLIGHT_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryId: runnerLedgerPreflightRegistry.registryId,
          registryDigest: runnerLedgerPreflightRegistry.registryDigest,
          sourceDigest: runnerLedgerPreflightRegistry.sourceDigest,
          stateMachineDigest: runnerLedgerPreflightRegistry.stateMachineDigest,
          policyDigest: runnerLedgerPreflightRegistry.policyDigest,
          profileId: (runnerLedgerPreflightRegistry.profile as { spec: { profileId: string } }).spec
            .profileId,
          runtimeConsumer: "NOT_IMPLEMENTED",
          databaseSession: "NONE",
          databaseTransaction: "FORBIDDEN",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-preflight-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerPreflightGoInputs),
        inputs: runnerLedgerPreflightGoInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_PREFLIGHT_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryDigest: runnerLedgerPreflightRegistry.registryDigest,
          stateMachineDigest: runnerLedgerPreflightRegistry.stateMachineDigest,
          policyDigest: runnerLedgerPreflightRegistry.policyDigest,
          handWrittenProfileFallback: "FORBIDDEN",
          productionConsumer: "NONE_IN_SLICE_A",
          databaseHandle: "FORBIDDEN",
          writerAuthority: "NONE",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-consumer-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerConsumerInputs),
        inputs: runnerLedgerConsumerInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_CONSUMER_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_CONSUMER_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryId: runnerLedgerConsumerRegistry.registryId,
          registryDigest: runnerLedgerConsumerRegistry.registryDigest,
          sourceDigest: runnerLedgerConsumerRegistry.sourceDigest,
          stateMachineDigest: runnerLedgerConsumerRegistry.stateMachineDigest,
          policyDigest: runnerLedgerConsumerRegistry.policyDigest,
          profileId: (runnerLedgerConsumerRegistry.profile as { spec: { profileId: string } }).spec
            .profileId,
          boundPreflightRegistryDigest: (
            runnerLedgerConsumerRegistry.preflightBinding as { registryDigest: string }
          ).registryDigest,
          returnSuccessNoopPairs: 1,
          entryNotImplementedPairs: 5,
          recoveryNotImplementedPairs: 11,
          databaseTransaction: "FORBIDDEN",
          ledgerMutation: "FORBIDDEN",
          evidenceMutation: "FORBIDDEN",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-consumer-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerConsumerGoInputs),
        inputs: runnerLedgerConsumerGoInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_CONSUMER_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryDigest: runnerLedgerConsumerRegistry.registryDigest,
          stateMachineDigest: runnerLedgerConsumerRegistry.stateMachineDigest,
          policyDigest: runnerLedgerConsumerRegistry.policyDigest,
          handWrittenProfileFallback: "FORBIDDEN",
          productionConsumer: "ONE_CLOSED_NOOP_IN_SLICE_B",
          preflightV1Mutation: "FORBIDDEN",
          databaseHandle: "FORBIDDEN",
          writerAuthority: "NONE",
          entryWriter: "NOT_IMPLEMENTED",
          recoveryWriter: "NOT_IMPLEMENTED",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-entry-admission-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerEntryAdmissionInputs),
        inputs: runnerLedgerEntryAdmissionInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryId: runnerLedgerEntryAdmissionRegistry.registryId,
          registryDigest: runnerLedgerEntryAdmissionRegistry.registryDigest,
          sourceDigest: runnerLedgerEntryAdmissionRegistry.sourceDigest,
          stateMachineDigest: runnerLedgerEntryAdmissionRegistry.stateMachineDigest,
          policyDigest: runnerLedgerEntryAdmissionRegistry.policyDigest,
          profileId: (runnerLedgerEntryAdmissionRegistry.profile as { spec: { profileId: string } })
            .spec.profileId,
          boundConsumerRegistryDigest: (
            runnerLedgerEntryAdmissionRegistry.consumerBinding as { registryDigest: string }
          ).registryDigest,
          entryAdmissionPairs: 5,
          databaseSession: "FRESH_DEDICATED_LOCKED_READ_ONLY_UNTIL_EXACT_CLOSE",
          beginMigration: "FORBIDDEN",
          entryWriter: "NOT_IMPLEMENTED",
          recoveryWriter: "NOT_IMPLEMENTED",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-entry-admission-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(
          root,
          runnerLedgerEntryAdmissionGoInputs,
        ),
        inputs: runnerLedgerEntryAdmissionGoInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_ADMISSION_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_ADMISSION_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryDigest: runnerLedgerEntryAdmissionRegistry.registryDigest,
          stateMachineDigest: runnerLedgerEntryAdmissionRegistry.stateMachineDigest,
          policyDigest: runnerLedgerEntryAdmissionRegistry.policyDigest,
          handWrittenProfileFallback: "FORBIDDEN",
          productionConsumer: "NONE_IN_SLICE_A",
          consumerV1Mutation: "FORBIDDEN",
          databaseHandle: "FORBIDDEN",
          permitConsumer: "NONE",
          beginMigration: "FORBIDDEN",
          entryWriter: "NOT_IMPLEMENTED",
          recoveryWriter: "NOT_IMPLEMENTED",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-entry-execution-admission-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(
          root,
          runnerLedgerEntryExecutionAdmissionInputs,
        ),
        inputs: runnerLedgerEntryExecutionAdmissionInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH),
            sizeBytes: readFileSync(
              resolve(root, RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH),
            ).byteLength,
          },
        ],
        outputSummary: {
          registryId: runnerLedgerEntryExecutionAdmissionRegistry.registryId,
          registryDigest: runnerLedgerEntryExecutionAdmissionRegistry.registryDigest,
          sourceDigest: runnerLedgerEntryExecutionAdmissionRegistry.sourceDigest,
          stateMachineDigest: runnerLedgerEntryExecutionAdmissionRegistry.stateMachineDigest,
          policyDigest: runnerLedgerEntryExecutionAdmissionRegistry.policyDigest,
          profileId: (
            runnerLedgerEntryExecutionAdmissionRegistry.profile as {
              spec: { profileId: string };
            }
          ).spec.profileId,
          boundEntryAdmissionRegistryDigest: (
            runnerLedgerEntryExecutionAdmissionRegistry.entryAdmissionBinding as {
              registryDigest: string;
            }
          ).registryDigest,
          executionAdmissionPairs: 4,
          retryPair: "EXCLUDED",
          databaseTransaction: "NOT_OPENED_BY_ADMISSION",
          beginMigration: "FORBIDDEN_IN_ADMISSION",
          sqlExecution: "FORBIDDEN_IN_ADMISSION",
          productionConsumer: "NONE_IN_SLICE_A",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-entry-success-writer-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(
          root,
          runnerLedgerEntrySuccessWriterInputs,
        ),
        inputs: runnerLedgerEntrySuccessWriterInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          registryId: runnerLedgerEntrySuccessWriterRegistry.registryId,
          registryDigest: runnerLedgerEntrySuccessWriterRegistry.registryDigest,
          sourceDigest: runnerLedgerEntrySuccessWriterRegistry.sourceDigest,
          stateMachineDigest: runnerLedgerEntrySuccessWriterRegistry.stateMachineDigest,
          policyDigest: runnerLedgerEntrySuccessWriterRegistry.policyDigest,
          profileId: (
            runnerLedgerEntrySuccessWriterRegistry.profile as { spec: { profileId: string } }
          ).spec.profileId,
          boundExecutionAdmissionRegistryDigest: (
            runnerLedgerEntrySuccessWriterRegistry.executionAdmissionBinding as {
              registryDigest: string;
            }
          ).registryDigest,
          writerAction: "EXECUTE_ONE_ENTRY_KNOWN_SUCCESS",
          multiStatement: "REQUIRED",
          retryWriter: "NOT_IMPLEMENTED",
          recoveryWriters: "NOT_IMPLEMENTED",
          productionConsumer: "NONE_IN_SLICE_A",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-entry-writer-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerEntryWriterGoInputs),
        inputs: runnerLedgerEntryWriterGoInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_WRITER_GO_PROFILES",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          executionRegistryDigest: runnerLedgerEntryExecutionAdmissionRegistry.registryDigest,
          writerRegistryDigest: runnerLedgerEntrySuccessWriterRegistry.registryDigest,
          generatedSelectorOnly: true,
          handWrittenProfileFallback: "FORBIDDEN",
          historicalEntryAdmissionV1Mutation: "FORBIDDEN",
          executionAdmissionPairs: 4,
          retryPair: "EXCLUDED",
          productionConsumer: "NONE_IN_SLICE_A",
          runtimeWriter: "NOT_IMPLEMENTED_IN_SLICE_A",
          recoveryWriters: "NOT_IMPLEMENTED",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-recovery-registry-suite-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerRecoveryInputs),
        inputs: runnerLedgerRecoveryInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_RECOVERY_REGISTRY_SUITE",
        notGateClosure: true,
        generatedOutputs: RUNNER_LEDGER_RECOVERY_FAMILIES.map((family) => {
          const path = RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family];
          return {
            path,
            sha256: fileSha256(root, path),
            sizeBytes: readFileSync(resolve(root, path)).byteLength,
          };
        }),
        outputSummary: {
          registryCount: runnerLedgerRecoveryRegistries.length,
          profileIds: runnerLedgerRecoveryRegistries.map(
            (registry) => (registry.profile as { spec: { profileId: string } }).spec.profileId,
          ),
          registryDigests: runnerLedgerRecoveryRegistries.map(
            (registry) => registry.registryDigest,
          ),
          pairCounts: runnerLedgerRecoveryRegistries.map(
            (registry) =>
              (
                registry.profile as {
                  spec: { pairBindings: ReadonlyArray<unknown> };
                }
              ).spec.pairBindings.length,
          ),
          closedPairCount: 12,
          entryNotImplementedPairs: 1,
          recoveryNotImplementedPairs: 11,
          recoveryExecutionAdmissionProfile: "runner-ledger-recovery-execution-admission/v1",
          recoverySuccessWriterProfile: "runner-ledger-recovery-success-writer/v1",
          recoverySuccessWriterDirectPairs: 0,
          unionWriter: "FORBIDDEN",
          productionConsumer: "NONE_IN_SLICE_A",
          runtimeClaims: "NOT_IMPLEMENTED_IN_SLICE_A",
          entryWriter: "NOT_IMPLEMENTED",
          recoveryWriters: "NOT_IMPLEMENTED",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "runner-ledger-recovery-go-profile-suite-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, runnerLedgerRecoveryGoInputs),
        inputs: runnerLedgerRecoveryGoInputs,
        outputStatus: "GENERATED_RUNNER_LEDGER_RECOVERY_GO_PROFILE_SUITE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH,
            sha256: fileSha256(root, RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH))
              .byteLength,
          },
        ],
        outputSummary: {
          profileCount: 8,
          closedPairCount: 12,
          generatedSelectorOnly: true,
          goFormatter: "GOFMT_FROM_EXACT_GO_1_26_6_TOOLCHAIN",
          handWrittenProfileFallback: "FORBIDDEN",
          historicalRunnerV1Mutation: "FORBIDDEN",
          recoverySuccessWriterDirectPairs: 0,
          productionConsumer: "NONE_IN_SLICE_A",
          runtimeClaims: "NOT_IMPLEMENTED_IN_SLICE_A",
          entryWriter: "NOT_IMPLEMENTED",
          recoveryWriters: "NOT_IMPLEMENTED",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "identity-verifier-registry-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, identityVerifierInputs),
        inputs: identityVerifierInputs,
        outputStatus: "GENERATED_IDENTITY_VERIFIER_REGISTRY",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: IDENTITY_VERIFIER_OUTPUT_PATH,
            sha256: fileSha256(root, IDENTITY_VERIFIER_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, IDENTITY_VERIFIER_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryId: identityVerifierRegistry.registryId,
          registryDigest: identityVerifierRegistry.registryDigest,
          profileId: identityVerifierProfile.profileId,
          profileDigest: identityVerifierProfile.profileDigest,
          algorithm: "RS256_ONLY",
          audienceCardinality: "EXACTLY_ONE_SNAPSHOT_OWNED",
          generatedProfileOnly: true,
          runtimeVerifier: "NOT_IMPLEMENTED_IN_SLICE_A",
          productionTrustProvisioning: "NOT_IMPLEMENTED",
          httpSurface: "NOT_IMPLEMENTED",
          oidcDiscovery: "NOT_IMPLEMENTED",
          remoteJwks: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "identity-verifier-go-profile-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, identityVerifierGoInputs),
        inputs: identityVerifierGoInputs,
        outputStatus: "GENERATED_IDENTITY_VERIFIER_GO_PROFILE",
        notGateClosure: true,
        generatedOutputs: [
          {
            path: IDENTITY_VERIFIER_GO_OUTPUT_PATH,
            sha256: fileSha256(root, IDENTITY_VERIFIER_GO_OUTPUT_PATH),
            sizeBytes: readFileSync(resolve(root, IDENTITY_VERIFIER_GO_OUTPUT_PATH)).byteLength,
          },
        ],
        outputSummary: {
          registryDigest: identityVerifierRegistry.registryDigest,
          profileDigest: identityVerifierProfile.profileDigest,
          generatedFactsOnly: true,
          packagePrivate: true,
          handWrittenProfileFallback: "FORBIDDEN",
          productionConstructor: "NONE_IN_SLICE_A",
          runtimeVerifier: "NOT_IMPLEMENTED_IN_SLICE_A",
          httpSurface: "NOT_IMPLEMENTED",
          oidcDiscovery: "NOT_IMPLEMENTED",
          remoteJwks: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "common-identity-go-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, identityGoInputs),
        inputs: identityGoInputs,
        outputStatus: "GENERATED_COMMON_IDENTITY_GO_SDK",
        notGateClosure: true,
        generatedOutputs: [GO_IDENTITY_OUTPUT_PATH, GO_IDENTITY_MANIFEST_PATH].map((path) => ({
          path,
          sha256: fileSha256(root, path),
          sizeBytes: readFileSync(resolve(root, path)).byteLength,
        })),
        outputSummary: {
          profile: "cloud-agents-common-identity/v1alpha1",
          packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1",
          runtimeDependency: "golang.org/x/text v0.39.0",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "common-identity-typescript-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, identityTypeScriptInputs),
        inputs: identityTypeScriptInputs,
        outputStatus: "GENERATED_COMMON_IDENTITY_TYPESCRIPT_SDK",
        notGateClosure: true,
        generatedOutputs: [TYPESCRIPT_IDENTITY_OUTPUT_PATH, TYPESCRIPT_IDENTITY_MANIFEST_PATH].map(
          (path) => ({
            path,
            sha256: fileSha256(root, path),
            sizeBytes: readFileSync(resolve(root, path)).byteLength,
          }),
        ),
        outputSummary: {
          profile: "cloud-agents-common-identity/v1alpha1",
          packageIdentity: "@synara/cloud-agent-platform-sdk",
          packagePrivate: true,
          runtimeDependencies: 0,
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-json-contract-go-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, jsonSDKGoInputs),
        inputs: jsonSDKGoInputs,
        outputStatus: "GENERATED_PLATFORM_JSON_GO_SDK",
        notGateClosure: true,
        generatedOutputs: [
          GO_COMMON_JSON_OUTPUT_PATH,
          GO_PLATFORM_JSON_OUTPUT_PATH,
          GO_OPENAPI_OUTPUT_PATH,
          GO_JSON_MANIFEST_PATH,
        ].map((path) => ({
          path,
          sha256: fileSha256(root, path),
          sizeBytes: readFileSync(resolve(root, path)).byteLength,
        })),
        outputSummary: {
          profile: "cloud-agents-json-contract-sdk/v1alpha1",
          packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go",
          responseUnknownFields: "EXPLICIT_SIDECAR_ONLY",
          nMinusOneReader: true,
          transport: "INJECTED_FIXTURE_TRANSPORT_ONLY",
          serverRouteRegistration: "NOT_IMPLEMENTED",
          httpSurface: "FIXTURE_ONLY_NO_PRODUCTION_ROUTE",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-json-contract-typescript-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, jsonSDKTypeScriptInputs),
        inputs: jsonSDKTypeScriptInputs,
        outputStatus: "GENERATED_PLATFORM_JSON_TYPESCRIPT_SDK",
        notGateClosure: true,
        generatedOutputs: [TYPESCRIPT_PLATFORM_OUTPUT_PATH, TYPESCRIPT_JSON_MANIFEST_PATH].map(
          (path) => ({
            path,
            sha256: fileSha256(root, path),
            sizeBytes: readFileSync(resolve(root, path)).byteLength,
          }),
        ),
        outputSummary: {
          profile: "cloud-agents-json-contract-sdk/v1alpha1",
          packageIdentity: "@synara/cloud-agent-platform-sdk/platform",
          responseUnknownFields: "EXPLICIT_SIDECAR_ONLY",
          nMinusOneReader: true,
          transport: "INJECTED_FIXTURE_TRANSPORT_ONLY",
          packagePrivate: true,
          httpSurface: "FIXTURE_ONLY_NO_PRODUCTION_ROUTE",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-proto-descriptor-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, protoDescriptorInputs),
        inputs: protoDescriptorInputs,
        outputStatus: "GENERATED_EXACT_V1ALPHA1_DESCRIPTOR_BASELINE",
        notGateClosure: true,
        generatedOutputs: [
          PLATFORM_PROTO_DESCRIPTOR_PATH,
          PLATFORM_PROTO_BREAKING_BASELINE_PATH,
          PLATFORM_PROTO_DESCRIPTOR_MANIFEST_PATH,
        ].map((path) => ({
          path,
          sha256: fileSha256(root, path),
          sizeBytes: readFileSync(resolve(root, path)).byteLength,
        })),
        outputSummary: {
          profile: "cloud-agents-proto-generation/v1alpha1",
          compiler: "protocolbuffers/protoc 35.1",
          includeImports: true,
          includeSourceInfo: false,
          serviceCount: 3,
          unaryMethodCount: 12,
          breakingPolicy: "EXACT_V1ALPHA1_BASELINE_NO_UNREVIEWED_DELTA",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-proto-go-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, protoGoInputs),
        inputs: protoGoInputs,
        outputStatus: "GENERATED_PROTO_GO_SDK",
        notGateClosure: true,
        generatedOutputs: [PLATFORM_PROTO_GO_MANIFEST_PATH, ...PLATFORM_PROTO_GO_OUTPUTS].map(
          (path) => ({
            path,
            sha256: fileSha256(root, path),
            sizeBytes: readFileSync(resolve(root, path)).byteLength,
          }),
        ),
        outputSummary: {
          profile: "cloud-agents-proto-sdk/v1alpha1",
          packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go",
          transport: "INJECTED_CONNECT_FIXTURE_TRANSPORT_ONLY",
          protocols: ["connect", "grpc"],
          productionCrossRepository: "HTTP2_MTLS_REQUIRED_NOT_REGISTERED",
          serverRouteRegistration: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-proto-typescript-sdk-generation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, protoTypeScriptInputs),
        inputs: protoTypeScriptInputs,
        outputStatus: "GENERATED_PROTO_TYPESCRIPT_SDK",
        notGateClosure: true,
        generatedOutputs: [
          PLATFORM_PROTO_TYPESCRIPT_MANIFEST_PATH,
          PLATFORM_PROTO_TYPESCRIPT_INDEX_PATH,
          ...PLATFORM_PROTO_TYPESCRIPT_OUTPUTS,
        ].map((path) => ({
          path,
          sha256: fileSha256(root, path),
          sizeBytes: readFileSync(resolve(root, path)).byteLength,
        })),
        outputSummary: {
          profile: "cloud-agents-proto-sdk/v1alpha1",
          packageIdentity: "@synara/cloud-agent-platform-sdk/proto",
          packagePrivate: true,
          transport: "INJECTED_CONNECT_FIXTURE_TRANSPORT_ONLY",
          protocols: ["connect", "grpc"],
          productionCrossRepository: "HTTP2_MTLS_REQUIRED_NOT_REGISTERED",
          serverRouteRegistration: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          deployment: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      },
      {
        id: "platform-migration-bundle-validation",
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, migrationInputs),
        inputs: migrationInputs,
        outputStatus: "BOOTSTRAP_VALIDATED",
        notGateClosure: true,
        generatedOutputs: [...migration.files.keys()].toSorted().map((path) => ({
          path,
          sha256: fileSha256(root, path),
          sizeBytes: readFileSync(resolve(root, path)).byteLength,
        })),
        outputSummary: {
          schemaBundleDigest: migration.manifest.schema_bundle_digest,
          bootstrapBundleDigest: migration.manifest.bootstrap_bundle_digest,
          manifestDigest: migration.manifest.manifest_digest,
          runtimeTar: {
            sizeBytes: migration.runtimeTar.byteLength,
            sha256: digestBytes(migration.runtimeTar),
          },
          bootstrapTar: {
            sizeBytes: migration.bootstrapTar.byteLength,
            sha256: digestBytes(migration.bootstrapTar),
          },
          schemaPublicationStatus: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
          runtimeIntrospectionStatus: "NOT_IMPLEMENTED",
          signingAndPublication: "NOT_IMPLEMENTED",
        },
      },
      {
        id: "go-module-boundary-validation",
        modules: [
          "github.com/hxp0618/cloud-agents/sdk/go",
          "github.com/hxp0618/cloud-agents/services/control-plane",
          "github.com/hxp0618/cloud-agents/services/worker",
        ],
        inputManifestAlgorithm: NORMALIZED_MANIFEST_ALGORITHM,
        inputManifestSha256: normalizedSourceManifestDigest(root, PLATFORM_GO_INPUTS),
        inputs: [...PLATFORM_GO_INPUTS],
        generatedOutputs: [],
      },
    ],
    missing: summary.missing.filter((item) => item !== "proto-descriptor-and-breaking"),
  };
}

export function serializePlatformContractLock(lock: Record<string, unknown>): string {
  return `${JSON.stringify(lock, null, 2)}\n`;
}

export function assertPlatformContractLockCurrent(root: string): void {
  const expected = serializePlatformContractLock(buildPlatformContractLock(root));
  const actual = readFileSync(resolve(root, "contracts/generation.lock.json"), "utf8");
  if (actual !== expected) {
    throw new Error(
      "contracts/generation.lock.json is stale; run bun scripts/generate-platform-contract-lock.ts --write.",
    );
  }
}

function assertContractStandardsProfileCurrent(root: string): ContractStandardsProfile {
  const profile = JSON.parse(
    readFileSync(resolve(root, CONTRACT_STANDARDS_PROFILE), "utf8"),
  ) as ContractStandardsProfile;
  if (
    profile.formatVersion !== "cloud-agents-contract-standards-profile/v1" ||
    profile.status !== "GENERATED_NON_GATE_EVIDENCE" ||
    profile.notGateClosure !== true
  ) {
    throw new Error("Contract standards profile identity or non-Gate status is invalid.");
  }
  if (
    profile.toolchain.bun !== BUN_VERSION ||
    profile.toolchain.python !== PYTHON_VERSION ||
    profile.toolchain.uv !== UV_VERSION
  ) {
    throw new Error("Contract standards profile toolchain versions are stale.");
  }
  for (const [label, fact, expectedPath] of [
    ["pyproject", profile.toolchain.pyproject, "tools/contract-standards/pyproject.toml"],
    ["lock", profile.toolchain.lock, "tools/contract-standards/uv.lock"],
  ] as const) {
    if (
      fact.path !== expectedPath ||
      fact.sha256 !== fileSha256(root, expectedPath).replace(/^sha256:/u, "")
    ) {
      throw new Error(`Contract standards ${label} binding is stale.`);
    }
  }
  if (
    profile.packages["jsonschema-rs"] !== "0.50.1" ||
    profile.packages["openapi-spec-validator"] !== "0.9.0"
  ) {
    throw new Error("Contract standards primary package versions are stale.");
  }
  const suite = profile.jsonSchemaOfficialSuite;
  const corpusFiles = listRegularMigrationInputFiles(root, CONTRACT_STANDARDS_CORPUS);
  if (
    suite.localRoot !== CONTRACT_STANDARDS_CORPUS ||
    suite.corpusManifestAlgorithm !== "sorted-path-nul-sha256-nul-size-v1" ||
    suite.corpusManifestSha256 !== standardsCorpusManifestDigest(root, CONTRACT_STANDARDS_CORPUS) ||
    suite.corpusFiles !== corpusFiles.length ||
    suite.licenseSha256 !==
      fileSha256(root, `${CONTRACT_STANDARDS_CORPUS}/LICENSE`).replace(/^sha256:/u, "") ||
    suite.mandatoryFiles !== 46 ||
    suite.cases !== 383 ||
    suite.assertions !== 1299 ||
    suite.remoteFiles !== 79 ||
    suite.expectedFailures !== 0 ||
    suite.productionAjvOfficialSuiteAudit.validator !== "Ajv 8.20.0" ||
    suite.productionAjvOfficialSuiteAudit.status !== "NOT_RUN_NOT_CLAIMED" ||
    Object.keys(suite.productionAjvOfficialSuiteAudit).length !== 2
  ) {
    throw new Error("Contract standards official JSON Schema suite binding is stale.");
  }
  if (
    profile.currentContracts.schemaFiles !== 52 ||
    profile.currentContracts.fixtureManifests !== 2 ||
    profile.currentContracts.fixtureCases !== 71 ||
    profile.currentContracts.crossEngineExactFixtureResults !== true
  ) {
    throw new Error("Contract standards current-contract cardinalities are stale.");
  }
  const expectedOpenApiDocuments = [
    "contracts/managed-agent/v1alpha1/openapi.json",
    "contracts/managed-host/v1alpha1/openapi.json",
  ];
  if (
    profile.openapi.documentVersion !== "3.1.1" ||
    JSON.stringify(profile.openapi.documents) !== JSON.stringify(expectedOpenApiDocuments) ||
    profile.openapi.documentCount !== 2 ||
    profile.openapi.operationCount !== 9 ||
    profile.openapi.expectedFailures !== 0
  ) {
    throw new Error("Contract standards OpenAPI 3.1 binding is stale.");
  }
  const boundary = {
    productionRuntimeDependency: "FORBIDDEN",
    productionDatabaseWrites: "NOT_AUTHORIZED",
    httpSurface: "NOT_IMPLEMENTED",
    p2Surface: "NOT_IMPLEMENTED",
    providerSideEffects: "FORBIDDEN",
    deployment: "NOT_AUTHORIZED",
    publication: "NOT_AUTHORIZED",
    gateStatus: "ALL_GATES_OPEN",
    independentReview: "PENDING",
  };
  if (
    Object.keys(profile.implementationBoundary).length !== Object.keys(boundary).length ||
    Object.entries(boundary).some(([key, value]) => profile.implementationBoundary[key] !== value)
  ) {
    throw new Error("Contract standards implementation boundary is invalid.");
  }
  return profile;
}

function standardsCorpusManifestDigest(root: string, directory: string): string {
  const hash = createHash("sha256");
  const prefix = `${directory}/`;
  for (const file of listRegularMigrationInputFiles(root, directory).toSorted()) {
    if (!file.startsWith(prefix)) {
      throw new Error(`Contract standards corpus path escapes its root: ${file}.`);
    }
    const bytes = readFileSync(resolve(root, file));
    hash
      .update(file.slice(prefix.length))
      .update("\0")
      .update(createHash("sha256").update(bytes).digest("hex"))
      .update("\0")
      .update(String(bytes.byteLength))
      .update("\0");
  }
  return hash.digest("hex");
}

export function platformMigrationInputs(root: string): string[] {
  const discovered = PLATFORM_MIGRATION_INPUT_DIRECTORIES.flatMap((directory) =>
    listRegularMigrationInputFiles(root, directory),
  );
  const coordination = [
    ...durableCoordinationRegistryInputs(root),
    DURABLE_COORDINATION_OUTPUT_PATH,
  ];
  const inputs = [...PLATFORM_MIGRATION_FIXED_INPUTS, ...coordination, ...discovered].toSorted();
  if (new Set(inputs).size !== inputs.length) {
    throw new Error("Platform migration inputs must have unique repository-relative paths.");
  }
  return inputs;
}

export function listRegularMigrationInputFiles(root: string, directory: string): string[] {
  const target = resolve(root, directory);
  const normalizedDirectory = relative(root, target).split(sep).join("/");
  if (
    normalizedDirectory === ".." ||
    normalizedDirectory.startsWith("../") ||
    normalizedDirectory.startsWith("/")
  ) {
    throw new Error(`Migration input directory escapes the repository root: ${directory}.`);
  }
  const stat = lstatSync(target);
  if (!stat.isDirectory() || stat.isSymbolicLink()) {
    throw new Error(`Migration input root must be a real directory: ${directory}.`);
  }

  const files: string[] = [];
  for (const entry of readdirSync(target).toSorted()) {
    const path = `${normalizedDirectory}/${entry}`;
    const entryStat = lstatSync(resolve(root, path));
    if (entryStat.isSymbolicLink()) {
      throw new Error(`Migration input closure rejects symbolic links: ${path}.`);
    }
    if (entryStat.isDirectory()) files.push(...listRegularMigrationInputFiles(root, path));
    else if (entryStat.isFile()) files.push(path);
    else throw new Error(`Migration input closure only accepts regular files: ${path}.`);
  }
  return files;
}

export function normalizedSourceManifestDigest(root: string, files: ReadonlyArray<string>): string {
  const hash = createHash("sha256");
  const entries = files.map((file) => {
    const target = resolve(root, file);
    const path = relative(root, target).split(sep).join("/");
    if (path === ".." || path.startsWith("../") || path.startsWith("/")) {
      throw new Error(`Manifest input escapes the repository root: ${file}.`);
    }
    const stat = lstatSync(target);
    if (!stat.isFile() || stat.isSymbolicLink()) {
      throw new Error(`Manifest input must be a regular file: ${file}.`);
    }
    const digest = createHash("sha256").update(readFileSync(target)).digest("hex");
    const gitMode = (stat.mode & 0o111) === 0 ? "100644" : "100755";
    return { digest, gitMode, path };
  });
  const paths = entries.map((entry) => entry.path);
  if (new Set(paths).size !== paths.length) {
    throw new Error("Manifest inputs must have unique normalized paths.");
  }
  for (const { digest, gitMode, path } of entries.toSorted((left, right) =>
    left.path < right.path ? -1 : left.path > right.path ? 1 : 0,
  )) {
    hash.update(path).update("\0").update(digest).update("\0").update(gitMode).update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function sourceManifestDigest(root: string, files: ReadonlyArray<string>): string {
  return normalizedSourceManifestDigest(root, files);
}

function validatePlatformToolchains(root: string): {
  readonly node: string;
  readonly bun: string;
  readonly go: string;
} {
  const mise = readFileSync(resolve(root, ".mise.toml"), "utf8");
  const packageDocument = JSON.parse(readFileSync(resolve(root, "package.json"), "utf8")) as {
    engines?: { node?: unknown };
    packageManager?: unknown;
  };
  const declared = {
    node: singleMiseTool(mise, "node"),
    bun: singleMiseTool(mise, "bun"),
    go: singleMiseTool(mise, "go"),
  };
  if (
    declared.node !== NODE_VERSION ||
    packageDocument.engines?.node !== NODE_VERSION ||
    declared.bun !== BUN_VERSION ||
    packageDocument.packageManager !== `bun@${BUN_VERSION}` ||
    declared.go !== PLATFORM_GO_TOOLCHAIN.slice(2)
  ) {
    throw new Error(
      `Platform toolchain declarations mismatch: mise=${JSON.stringify(declared)}, package node=${String(packageDocument.engines?.node)}, package manager=${String(packageDocument.packageManager)}.`,
    );
  }

  const actual = {
    node: runVersion(root, "node", ["--version"]).replace(/^v/u, ""),
    bun: runVersion(root, "bun", ["--version"]),
    go: parseGoVersion(runVersion(root, "go", ["version"])),
  };
  const executingBun = process.versions.bun;
  if (actual.node !== declared.node || actual.bun !== declared.bun || actual.go !== declared.go) {
    throw new Error(
      `Platform toolchain runtime mismatch: declared=${JSON.stringify(declared)}, actual=${JSON.stringify(actual)}.`,
    );
  }
  if (executingBun !== declared.bun) {
    throw new Error(
      `Platform lock writer must execute under Bun ${declared.bun}, found ${String(executingBun)}.`,
    );
  }
  return actual;
}

function singleMiseTool(source: string, tool: string): string {
  const matches = [...source.matchAll(new RegExp(`^\\s*${tool}\\s*=\\s*"([^"]+)"\\s*$`, "gmu"))];
  if (matches.length !== 1) throw new Error(`.mise.toml must pin exactly one ${tool} version.`);
  return matches[0]![1]!;
}

function runVersion(root: string, command: string, args: ReadonlyArray<string>): string {
  const result = spawnSync(command, args, {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, GOTOOLCHAIN: "local" },
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}):\n${result.stdout}${result.stderr}`,
    );
  }
  return result.stdout.trim();
}

function parseGoVersion(source: string): string {
  const match = /^go version go([^\s]+)\s/u.exec(source);
  if (!match) throw new Error(`Unexpected go version output: ${source}.`);
  return match[1]!;
}

function fileSha256(root: string, file: string): string {
  return `sha256:${createHash("sha256")
    .update(readFileSync(resolve(root, file)))
    .digest("hex")}`;
}

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}
