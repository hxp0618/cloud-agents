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
import { PLATFORM_GO_TOOLCHAIN } from "./platform-go-modules";
import { validateCheckedInMigrationBundle } from "./platform-migration-bundle";

const NODE_VERSION = "24.13.1";
const BUN_VERSION = "1.3.14";
const AJV_REVIEW = "docs/plan/p1/dependency-reviews/ajv-8.20.0.md";
const TOOLCHAIN_AUTHORITY_FILES = [".mise.toml", "package.json"] as const;
const PLATFORM_GO_INPUTS = [
  "go.work",
  "sdk/go/go.mod",
  "sdk/go/doc.go",
  "services/control-plane/go.mod",
  "services/control-plane/doc.go",
  "services/worker/go.mod",
  "services/worker/doc.go",
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
  "services/control-plane/migrations/README.md",
  "services/control-plane/migrations/bootstrap/database.sql",
  "services/control-plane/migrations/bootstrap/roles.sql",
  "services/control-plane/migrations/manifest.json",
  "services/control-plane/migrations/schema-bundle.json",
  "services/control-plane/scripts/test-durable-coordination-kernel-postgres-matrix.sh",
  "services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh",
  "services/control-plane/scripts/test-compatibility-recovery-kernel-postgres-matrix.sh",
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
      "scripts/lib/platform-contract-lock.ts",
      "scripts/lib/platform-contracts.ts",
      "scripts/lib/platform-compatibility-recovery-registry.ts",
      "scripts/lib/platform-durable-coordination-registry.ts",
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
] as const;

export function buildPlatformContractLock(root: string): Record<string, unknown> {
  const summary = validatePlatformContractTree(root);
  const runtimes = validatePlatformToolchains(root);
  const migration = validateCheckedInMigrationBundle(root);
  const migrationInputs = platformMigrationInputs(root);
  assertDurableCoordinationRegistryCurrent(root);
  assertCompatibilityRecoveryRegistryCurrent(root);
  assertCompatibilityRecoveryRegistryV2Current(root);
  const durableCoordinationInputs = durableCoordinationRegistryInputs(root);
  const compatibilityRecoveryInputs = compatibilityRecoveryRegistryInputs(root);
  const compatibilityRecoveryV2Inputs = compatibilityRecoveryRegistryV2Inputs(root);
  const compatibilityRecoveryGoInputs = [
    ...COMPATIBILITY_RECOVERY_GO_GENERATOR_SOURCES,
    COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
  ].toSorted();
  const durableCoordinationGoInputs = [
    ...DURABLE_COORDINATION_GO_GENERATOR_SOURCES,
    DURABLE_COORDINATION_OUTPUT_PATH,
  ].toSorted();
  const durableCoordinationRegistry = buildDurableCoordinationRegistry(root);
  const compatibilityRecoveryRegistry = buildCompatibilityRecoveryRegistry(root);
  const compatibilityRecoveryRegistryV2 = buildCompatibilityRecoveryRegistryV2(root);
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
      },
      openapi: {
        documentVersion: "3.1.1",
        semanticValidation: "BOOTSTRAP_FAIL_CLOSED_SUBSET",
      },
      proto: {
        syntax: "proto3",
        descriptorStatus: "NOT_GENERATED",
        sourceValidation: "BOOTSTRAP_FAIL_CLOSED_SUBSET",
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
    ],
    pipelines: [
      {
        id: "bootstrap-contract-validation",
        inputManifestSha256: summary.contractManifestSha256,
        outputStatus: "BOOTSTRAP_VALIDATED",
        generatedOutputs: [],
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
    missing: [...summary.missing],
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
