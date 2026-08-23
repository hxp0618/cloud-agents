import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  IDENTITY_VERIFIER_RUNTIME_SOURCES,
  listRegularMigrationInputFiles,
  normalizedSourceManifestDigest,
  platformMigrationInputs,
  serializePlatformContractLock,
} from "./platform-contract-lock";

const SLICE_B_IDENTITY_VERIFIER_RUNTIME_INPUTS = [
  "services/control-plane/internal/authn/canonical.go",
  "services/control-plane/internal/authn/errors.go",
  "services/control-plane/internal/authn/lexical.go",
  "services/control-plane/internal/authn/principal.go",
  "services/control-plane/internal/authn/strict_json.go",
  "services/control-plane/internal/authn/surface_test.go",
  "services/control-plane/internal/authn/trust.go",
  "services/control-plane/internal/authn/trust_test.go",
  "services/control-plane/internal/authn/verifier.go",
  "services/control-plane/internal/authn/verifier_test.go",
] as const;

const SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS = [
  "services/control-plane/internal/authn/authz_callgraph_test.go",
  "services/control-plane/internal/authn/binder_external_test.go",
  "services/control-plane/internal/authn/export_test.go",
  "services/control-plane/internal/authn/postgres_external_test.go",
  "services/control-plane/internal/authz/rbac.go",
  "services/control-plane/internal/authz/rbac_test.go",
  "services/control-plane/internal/store/postgres/data_recovery_integration_test.go",
  "services/control-plane/internal/store/postgres/durable_coordination.go",
  "services/control-plane/internal/store/postgres/durable_coordination_integration_test.go",
  "services/control-plane/internal/store/postgres/durable_coordination_test.go",
  "services/control-plane/internal/store/postgres/rbac.go",
  "services/control-plane/internal/store/postgres/rbac_integration_test.go",
  "services/control-plane/internal/store/postgres/rbac_mutation.go",
  "services/control-plane/internal/store/postgres/rbac_mutation_integration_test.go",
  "services/control-plane/internal/store/postgres/rbac_mutation_test.go",
  "services/control-plane/internal/store/postgres/rbac_test.go",
  "services/control-plane/internal/store/postgres/tenant_transaction.go",
  "services/control-plane/internal/store/postgres/tenant_transaction_integration_test.go",
  "services/control-plane/internal/store/postgres/tenant_transaction_test.go",
  "services/control-plane/internal/store/postgres/verified_operation_structure_test.go",
  "services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh",
  "services/control-plane/scripts/test-membership-rbac-postgres-matrix.sh",
] as const;

const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "platform-contract-lock-"));
  temporaryRoots.push(root);
  return root;
}

describe("Platform contract generation lock", () => {
  it("keeps the Slice C mutable identity-verifier runtime closure exact, sorted, and unique", () => {
    expect(IDENTITY_VERIFIER_RUNTIME_SOURCES).toEqual(
      [...IDENTITY_VERIFIER_RUNTIME_SOURCES].toSorted(),
    );
    expect(new Set(IDENTITY_VERIFIER_RUNTIME_SOURCES).size).toBe(
      IDENTITY_VERIFIER_RUNTIME_SOURCES.length,
    );
    expect(SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS).toEqual(
      [...SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS].toSorted(),
    );
    expect(IDENTITY_VERIFIER_RUNTIME_SOURCES).toEqual(
      [
        ...SLICE_B_IDENTITY_VERIFIER_RUNTIME_INPUTS,
        ...SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS,
      ].toSorted(),
    );
  });

  it("serializes without timestamps or host paths", () => {
    const serialized = serializePlatformContractLock({
      lockVersion: 1,
      status: "BOOTSTRAP_VALIDATED",
    });
    expect(serialized).toBe('{\n  "lockVersion": 1,\n  "status": "BOOTSTRAP_VALIDATED"\n}\n');
    expect(serialized).not.toMatch(/generatedAt|\/Users\//u);
  });

  it("normalizes non-executable permissions to the Git 100644 mode", () => {
    const root = temporaryRoot();
    const source = join(root, "input.txt");
    writeFileSync(source, "same bytes\n");
    chmodSync(source, 0o644);
    const ordinary = normalizedSourceManifestDigest(root, ["input.txt"]);
    chmodSync(source, 0o600);
    expect(normalizedSourceManifestDigest(root, ["input.txt"])).toBe(ordinary);
    chmodSync(source, 0o755);
    expect(normalizedSourceManifestDigest(root, ["input.txt"])).not.toBe(ordinary);
  });

  it("binds source bytes and normalized paths", () => {
    const root = temporaryRoot();
    const source = join(root, "module.go");
    writeFileSync(source, "package module\n");
    const initial = normalizedSourceManifestDigest(root, ["module.go"]);
    writeFileSync(source, "package changed\n");
    expect(normalizedSourceManifestDigest(root, ["module.go"])).not.toBe(initial);
  });

  it("recursively binds every catalog and fixture file", () => {
    const repositoryRoot = join(import.meta.dirname, "../..");
    const inputs = platformMigrationInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("docs/plan/adr/0010-p1-postgres-projection-contract.md");
    expect(inputs).toContain("docs/plan/adr/0011-p1-membership-rbac-contract.md");
    expect(inputs).toContain("docs/plan/adr/0013-p1-durable-coordination-contract.md");
    expect(inputs).toContain(
      "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
    );
    expect(inputs).toContain("scripts/lib/platform-migration-projection.ts");
    expect(inputs).toContain("scripts/lib/platform-migration-projection.test.ts");
    expect(inputs).toContain("services/control-plane/migrations/catalog/authority-v1.json");
    expect(inputs).toContain(
      "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/scripts/test-durable-coordination-service-postgres-matrix.sh",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql",
    );
    expect(inputs).toContain(
      "services/control-plane/scripts/test-compatibility-recovery-preflight-retirement-postgres-matrix.sh",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/fixtures/bundle/negative/ancestor-descriptor-cases.json",
    );
  });

  it("records the independent standards runner as a pending non-Gate candidate", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      dialects: {
        jsonSchema: { productionAjvOfficialSuiteStatus?: string };
      };
      tools: Array<Record<string, unknown>>;
      pipelines: Array<{
        id?: string;
        inputs?: string[];
        outputStatus?: string;
        notGateClosure?: boolean;
        outputSummary?: Record<string, unknown>;
      }>;
      missing: string[];
    };
    expect(lock.dialects.jsonSchema.productionAjvOfficialSuiteStatus).toBe("NOT_RUN_NOT_CLAIMED");
    expect(lock.tools).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: "jsonschema-rs",
          version: "0.50.1",
          sourceBuild: "FORBIDDEN",
          productionRuntimeDependency: "FORBIDDEN",
        }),
        expect.objectContaining({
          id: "openapi-spec-validator",
          version: "0.9.0",
          sourceBuild: "FORBIDDEN",
          productionRuntimeDependency: "FORBIDDEN",
        }),
      ]),
    );
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "independent-contract-standards-validation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "IMPLEMENTED_CANDIDATE_INDEPENDENT_REVIEW_PENDING",
      notGateClosure: true,
      outputSummary: {
        sourceContractManifestSha256:
          "sha256:0aee6843cd09fe301bcc3caa55c55686917531e38a1f683b73a8a4308f183de4",
        toolchain: { bun: "1.3.14", python: "3.14.7", uv: "0.12.5" },
        officialJsonSchemaSuite: {
          files: 46,
          cases: 383,
          assertions: 1299,
          expectedFailures: 0,
        },
        productionAjvOfficialSuiteAudit: {
          validator: "Ajv 8.20.0",
          status: "NOT_RUN_NOT_CLAIMED",
        },
        currentJsonSchema: {
          schemas: 54,
          fixtureManifests: 2,
          fixtureCases: 73,
          crossEngineExactFixtureResults: true,
        },
        openapi31: { documents: 2, operations: 9, expectedFailures: 0 },
        productionRuntimeDependency: "FORBIDDEN",
        independentReview: "PENDING",
        gateStatus: "ALL_GATES_OPEN",
      },
    });
    expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
    expect(pipeline?.inputs).toContain(
      "tools/contract-standards/vendor/json-schema-test-suite/tests/draft2020-12/ref.json",
    );
    expect(lock.missing).toEqual([
      "json-schema-2020-12-official-test-suite",
      "runtime-server-path-and-tenant-authority-enforcement",
      "remaining-generator-supply-chain-review",
    ]);
  });

  it("records the generated formal closure profile without closing G-CONTRACT", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      status: string;
      notGateClosure: boolean;
      missing: string[];
      pipelines: Array<{
        id?: string;
        inputs?: string[];
        generatedOutputs?: Array<{ path?: string }>;
        outputSummary?: Record<string, unknown>;
      }>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "contract-closure-profile-generation",
    );
    expect(lock).toMatchObject({ status: "BOOTSTRAP_VALIDATED", notGateClosure: true });
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_CONTRACT_CLOSURE_PROFILE",
      notGateClosure: true,
      generatedOutputs: [
        { path: "contracts/generated/platform/v1alpha1/contract-closure-profile-v1.json" },
      ],
      outputSummary: {
        profileId: "contract-closure-profile/v1",
        status: "BOOTSTRAP_VALIDATED",
        missing: lock.missing,
        manualMissingRemoval: "FORBIDDEN",
        officialAjvSuiteRunner: "NOT_IMPLEMENTED",
        runtimeTrustAndHttp: "NOT_IMPLEMENTED",
        supplyScanner: "NOT_IMPLEMENTED",
        gateStatus: "ALL_GATES_OPEN",
      },
    });
    expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
    for (const evidence of [
      "docs/plan/cloud-agents-platform/evidence/G-CONTRACT/CAG-G-CONTRACT-P1-20260823-R4-independent-review.md",
      "docs/plan/p1/sdk-identity-closure-independent-review-20260821.md",
      "sdk/go/proto_conformance_test.go",
      "sdk/typescript/src/proto.test.ts",
    ]) {
      expect(pipeline?.inputs).toContain(evidence);
    }
  });

  it("records the generated durable coordination registry as a non-Gate pipeline", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "durable-coordination-registry-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_CONTRACT_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        profileCount: 1,
        runtimeConsumer: "GENERATED_GO_PROFILE_TYPED_SERVICE_000008",
        sqlConsumer: "GENERATED_PROFILE_TYPED_FUNCTIONS_000008",
        httpSurface: "NOT_IMPLEMENTED",
        externalSideEffects: "FORBIDDEN",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/durable-coordination-registry.json",
        },
      ],
    });
  });

  it("records the generated Go profile without enabling HTTP or side effects", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "durable-coordination-go-profile-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_GO_PROFILE",
      notGateClosure: true,
      outputSummary: {
        profileId: "managedAgentCreateProject/v1alpha1",
        handWrittenProfileFallback: "FORBIDDEN",
        httpSurface: "NOT_IMPLEMENTED",
        externalSideEffects: "FORBIDDEN",
      },
      generatedOutputs: [
        {
          path: "services/control-plane/internal/coordination/registry_generated.go",
        },
      ],
    });
  });

  it("records the compatibility and recovery registry without enabling consumers or Gates", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "compatibility-recovery-registry-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_COMPATIBILITY_RECOVERY_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        profileCount: 5,
        runtimeConsumer: "NOT_IMPLEMENTED",
        sqlConsumer: "NOT_IMPLEMENTED_NO_000010",
        httpSurface: "NOT_IMPLEMENTED",
        externalSideEffects: "FORBIDDEN",
        localLogicalRestore: "CONTRACT_ONLY_NOT_IMPLEMENTED",
        pitrHa: "P4_NOT_IMPLEMENTED",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json",
        },
      ],
    });
    const inputs = pipeline?.inputs as string[];
    expect(inputs).toContain("docs/plan/adr/0015-p1-compatibility-recovery-contract.md");
    expect(inputs).toContain(
      "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
    );
    expect(inputs).not.toContain(
      "services/control-plane/migrations/000010_add_compatibility_recovery_registry.sql",
    );
  });

  it("records the versioned compatibility and recovery registry without enabling writers or Gates", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const pipeline = lock.pipelines.find(
      (candidate) => candidate.id === "compatibility-recovery-registry-v2-generation",
    );
    expect(pipeline).toMatchObject({
      outputStatus: "GENERATED_COMPATIBILITY_RECOVERY_REGISTRY_V2",
      notGateClosure: true,
      outputSummary: {
        profileCount: 6,
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
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/compatibility-recovery-registry-v2.json",
        },
      ],
    });
    const inputs = pipeline?.inputs as string[];
    expect(inputs).toContain("docs/plan/adr/0017-p1-compatibility-recovery-v2-registry.md");
    expect(inputs).toContain(
      "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json",
    );
    expect(inputs).toContain(
      "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
    );
    expect(inputs).not.toContain("services/control-plane/migrations/000011_add_writer.sql");
  });

  it("records the runner ledger consumer registry and Go profile without enabling writers or Gates", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const registry = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-consumer-registry-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-consumer-go-profile-generation",
    );
    expect(registry).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_CONSUMER_REGISTRY",
      notGateClosure: true,
      outputSummary: {
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
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json",
        },
      ],
    });
    expect(go).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_CONSUMER_GO_PROFILE",
      notGateClosure: true,
      outputSummary: {
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
      generatedOutputs: [
        {
          path: "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go",
        },
      ],
    });
    for (const pipeline of [registry, go]) {
      expect(pipeline?.inputs).toEqual([...((pipeline?.inputs ?? []) as string[])].toSorted());
    }
    expect(registry?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json",
    );
    expect(go?.inputs).toContain(
      "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go",
    );
  });

  it("records the runner ledger entry-admission registry without enabling a permit consumer or writer", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const registry = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-entry-admission-registry-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-entry-admission-go-profile-generation",
    );
    expect(registry).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        entryAdmissionPairs: 5,
        databaseSession: "FRESH_DEDICATED_LOCKED_READ_ONLY_UNTIL_EXACT_CLOSE",
        beginMigration: "FORBIDDEN",
        entryWriter: "NOT_IMPLEMENTED",
        recoveryWriter: "NOT_IMPLEMENTED",
        productionDatabaseWrites: "NOT_AUTHORIZED",
        gateStatus: "ALL_GATES_OPEN",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json",
        },
      ],
    });
    expect(go).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_ADMISSION_GO_PROFILE",
      notGateClosure: true,
      outputSummary: {
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
      generatedOutputs: [
        {
          path: "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go",
        },
      ],
    });
    for (const pipeline of [registry, go]) {
      expect(pipeline?.inputs).toEqual([...((pipeline?.inputs ?? []) as string[])].toSorted());
    }
    expect(registry?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json",
    );
    expect(go?.inputs).toContain(
      "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go",
    );
  });

  it("records generated execution-admission and success-writer profiles without enabling runtime writers", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const execution = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-entry-execution-admission-registry-generation",
    );
    const writer = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-entry-success-writer-registry-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-entry-writer-go-profile-generation",
    );
    expect(execution).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        executionAdmissionPairs: 4,
        retryPair: "EXCLUDED",
        databaseTransaction: "NOT_OPENED_BY_ADMISSION",
        beginMigration: "FORBIDDEN_IN_ADMISSION",
        sqlExecution: "FORBIDDEN_IN_ADMISSION",
        productionConsumer: "NONE_IN_SLICE_A",
        productionDatabaseWrites: "NOT_AUTHORIZED",
        gateStatus: "ALL_GATES_OPEN",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json",
        },
      ],
    });
    expect(writer).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        writerAction: "EXECUTE_ONE_ENTRY_KNOWN_SUCCESS",
        multiStatement: "REQUIRED",
        retryWriter: "NOT_IMPLEMENTED",
        recoveryWriters: "NOT_IMPLEMENTED",
        productionConsumer: "NONE_IN_SLICE_A",
        productionDatabaseWrites: "NOT_AUTHORIZED",
        gateStatus: "ALL_GATES_OPEN",
      },
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json",
        },
      ],
    });
    expect(go).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_ENTRY_WRITER_GO_PROFILES",
      notGateClosure: true,
      outputSummary: {
        generatedSelectorOnly: true,
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
      generatedOutputs: [
        {
          path: "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go",
        },
      ],
    });
    for (const pipeline of [execution, writer, go]) {
      expect(pipeline?.inputs).toEqual([...((pipeline?.inputs ?? []) as string[])].toSorted());
    }
    expect(execution?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json",
    );
    expect(writer?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json",
    );
    expect(go?.inputs).toContain(
      "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go",
    );
  });

  it("records the closed recovery registry and Go profile suites without enabling consumers", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, any>>;
    };
    const registry = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-recovery-registry-suite-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "runner-ledger-recovery-go-profile-suite-generation",
    );
    expect(registry).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_RECOVERY_REGISTRY_SUITE",
      notGateClosure: true,
      outputSummary: {
        registryCount: 8,
        pairCounts: [12, 4, 1, 1, 1, 3, 0, 2],
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
    });
    expect(registry?.generatedOutputs).toHaveLength(8);
    expect(registry?.generatedOutputs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          path: "contracts/generated/platform/v1alpha1/runner-ledger-recovery-admission-registry-v1.json",
        }),
        expect.objectContaining({
          path: "contracts/generated/platform/v1alpha1/runner-ledger-recovery-execution-admission-registry-v1.json",
        }),
        expect.objectContaining({
          path: "contracts/generated/platform/v1alpha1/runner-ledger-recovery-success-writer-registry-v1.json",
        }),
      ]),
    );
    expect(go).toMatchObject({
      outputStatus: "GENERATED_RUNNER_LEDGER_RECOVERY_GO_PROFILE_SUITE",
      notGateClosure: true,
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
      generatedOutputs: [
        {
          path: "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go",
        },
      ],
    });
    for (const pipeline of [registry, go]) {
      expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
    }
    expect(registry?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json",
    );
    expect(go?.inputs).toContain(
      "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go",
    );
    expect(go?.inputs).toContain(
      "services/control-plane/internal/migration/runner_ledger_recovery_profile_test.go",
    );
  });

  it("records the identity verifier registry and callback-scoped Slice C runtime closure", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      tools: Array<Record<string, unknown>>;
      pipelines: Array<Record<string, any>>;
    };
    expect(lock.tools).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          id: "platform-identity-verifier-registry-generator",
          entrypoint: "scripts/generate-platform-identity-verifier-registry.ts",
        }),
        expect.objectContaining({
          id: "platform-identity-verifier-go-generator",
          entrypoint: "scripts/generate-platform-identity-verifier-go.ts",
        }),
      ]),
    );

    const registry = lock.pipelines.find(
      (candidate) => candidate.id === "identity-verifier-registry-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "identity-verifier-go-profile-generation",
    );
    expect(registry).toMatchObject({
      outputStatus: "GENERATED_IDENTITY_VERIFIER_REGISTRY",
      notGateClosure: true,
      outputSummary: {
        registryId: "cloud-agents/platform/identity-verifier",
        profileId: "platform-identity-verifier/v1",
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
      generatedOutputs: [
        {
          path: "contracts/generated/platform/v1alpha1/identity-verifier-registry-v1.json",
        },
      ],
    });
    expect(go).toMatchObject({
      outputStatus: "GENERATED_IDENTITY_VERIFIER_GO_PROFILE",
      notGateClosure: true,
      outputSummary: {
        generatedFactsOnly: true,
        packagePrivate: true,
        handWrittenProfileFallback: "FORBIDDEN",
        productionConstructor: "NONE_PRIVATE_TEST_BUILDERS_ONLY",
        runtimeVerifier: "IMPLEMENTED_OFFLINE_SLICE_B",
        mutableRuntimeConformanceInputs: true,
        principalConsumeSeam: "OPAQUE_ONE_SHOT_CALLBACK_SCOPED",
        productionConsumer: "STORE_POSTGRES_EIGHT_VERIFIED_PRINCIPAL_PATHS_SLICE_C",
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
      generatedOutputs: [
        {
          path: "services/control-plane/internal/authn/profile_generated.go",
        },
      ],
    });
    for (const pipeline of [registry, go]) {
      expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
    }
    const goInputs = (go?.inputs ?? []) as string[];
    expect(new Set(goInputs).size).toBe(goInputs.length);
    expect(
      goInputs.filter((input) =>
        (SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS as readonly string[]).includes(input),
      ),
    ).toEqual(SLICE_C_IDENTITY_VERIFIER_RUNTIME_INPUTS);
    expect(registry?.inputs).toContain(
      "tools/platform-identity-verifier/v1/fixtures/manifest.json",
    );
    expect(registry?.inputs).not.toContain("contracts/generation.lock.json");
    expect(go?.inputs).toContain(
      "contracts/generated/platform/v1alpha1/identity-verifier-registry-v1.json",
    );
    expect(go?.inputs).toContain("services/control-plane/internal/authn/profile_test.go");
    const authnDirectory = "services/control-plane/internal/authn";
    const authnConformanceInputs = readdirSync(join(root, authnDirectory), {
      withFileTypes: true,
    })
      .filter(
        (entry) =>
          entry.isFile() && entry.name.endsWith(".go") && entry.name !== "profile_generated.go",
      )
      .map((entry) => `${authnDirectory}/${entry.name}`)
      .toSorted();
    expect(
      go?.inputs.filter(
        (input: string) =>
          input.startsWith(`${authnDirectory}/`) &&
          input !== `${authnDirectory}/profile_generated.go`,
      ),
    ).toEqual(authnConformanceInputs);
  });

  it("records both generated common identity SDK profiles as non-Gate evidence", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<Record<string, unknown>>;
    };
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "common-identity-go-sdk-generation",
    );
    const typescript = lock.pipelines.find(
      (candidate) => candidate.id === "common-identity-typescript-sdk-generation",
    );
    for (const pipeline of [go, typescript]) {
      expect(pipeline).toMatchObject({
        notGateClosure: true,
        outputStatus: expect.stringMatching(/^GENERATED_COMMON_IDENTITY_/u),
        outputSummary: {
          profile: "cloud-agents-common-identity/v1alpha1",
          httpSurface: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          publication: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      });
      expect(pipeline?.inputs).toEqual([...((pipeline?.inputs ?? []) as string[])].toSorted());
      expect(pipeline?.generatedOutputs).toHaveLength(2);
    }
    expect(go?.outputSummary).toMatchObject({
      packageIdentity: "github.com/hxp0618/cloud-agents/sdk/go/gen/common/v1alpha1",
      runtimeDependency: "golang.org/x/text v0.39.0",
    });
    expect(typescript?.outputSummary).toMatchObject({
      packageIdentity: "@synara/cloud-agent-platform-sdk",
      packagePrivate: true,
      runtimeDependencies: 0,
    });
    expect(go?.inputs).toContain(
      "docs/plan/p1/dependency-reviews/x-text-v0.39.0-go-sdk-use-20260820.md",
    );
    expect(go?.inputs).toContain("sdk/go/gen/common/v1alpha1/identity_generated_test.go");
    expect(typescript?.inputs).toContain("sdk/typescript/src/identity.test.ts");
    expect(lock.pipelines.some((candidate) => candidate.id === "generated-sdk-replay")).toBe(false);
  });

  it("records the JSON SDK and fixture server seam without enabling routes or side effects", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<{
        id?: string;
        inputs?: string[];
        generatedOutputs?: unknown[];
        outputSummary?: Record<string, unknown>;
      }>;
    };
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "platform-json-contract-go-sdk-generation",
    );
    const typescript = lock.pipelines.find(
      (candidate) => candidate.id === "platform-json-contract-typescript-sdk-generation",
    );
    expect(go).toMatchObject({
      outputStatus: "GENERATED_PLATFORM_JSON_GO_SDK",
      notGateClosure: true,
      outputSummary: {
        profile: "cloud-agents-json-contract-sdk/v1alpha1",
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
    });
    expect(go?.generatedOutputs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "sdk/go/gen/common/v1alpha1/json_generated.go" }),
        expect.objectContaining({ path: "sdk/go/gen/openapi/v1alpha1/client_generated.go" }),
      ]),
    );
    expect(typescript).toMatchObject({
      outputStatus: "GENERATED_PLATFORM_JSON_TYPESCRIPT_SDK",
      notGateClosure: true,
      outputSummary: {
        profile: "cloud-agents-json-contract-sdk/v1alpha1",
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
    });
    expect(typescript?.generatedOutputs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "sdk/typescript/src/platform.ts" }),
        expect.objectContaining({ path: "sdk/typescript/json-generated-manifest.json" }),
      ]),
    );
    for (const pipeline of [go, typescript]) {
      expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
      expect(pipeline?.inputs).toContain("scripts/generate-platform-json-sdks.ts");
      expect(pipeline?.inputs).toContain("sdk/typescript/src/platform.test.ts");
    }
  });

  it("records the generated Proto descriptor and SDKs as non-Gate evidence", () => {
    const root = join(import.meta.dirname, "../..");
    const lock = JSON.parse(readFileSync(join(root, "contracts/generation.lock.json"), "utf8")) as {
      pipelines: Array<{
        id?: string;
        inputs?: string[];
        generatedOutputs?: unknown[];
        outputStatus?: string;
        notGateClosure?: boolean;
        outputSummary?: Record<string, unknown>;
      }>;
    };
    const descriptor = lock.pipelines.find(
      (candidate) => candidate.id === "platform-proto-descriptor-generation",
    );
    const go = lock.pipelines.find(
      (candidate) => candidate.id === "platform-proto-go-sdk-generation",
    );
    const typescript = lock.pipelines.find(
      (candidate) => candidate.id === "platform-proto-typescript-sdk-generation",
    );
    expect(descriptor).toMatchObject({
      outputStatus: "GENERATED_EXACT_V1ALPHA1_DESCRIPTOR_BASELINE",
      notGateClosure: true,
      outputSummary: {
        profile: "cloud-agents-proto-generation/v1alpha1",
        serviceCount: 3,
        unaryMethodCount: 12,
        breakingPolicy: "EXACT_V1ALPHA1_BASELINE_NO_UNREVIEWED_DELTA",
        httpSurface: "NOT_IMPLEMENTED",
        p2Surface: "NOT_IMPLEMENTED",
        providerSideEffects: "FORBIDDEN",
        productionDatabaseWrites: "NOT_AUTHORIZED",
        gateStatus: "ALL_GATES_OPEN",
      },
      generatedOutputs: expect.arrayContaining([
        expect.objectContaining({
          path: "contracts/generated/proto/cloud-agents-v1alpha1.binpb",
        }),
        expect.objectContaining({
          path: "contracts/generated/proto/cloud-agents-v1alpha1-breaking-baseline.binpb",
        }),
      ]),
    });
    for (const pipeline of [go, typescript]) {
      expect(pipeline).toMatchObject({
        outputStatus: expect.stringMatching(/^GENERATED_PROTO_/u),
        notGateClosure: true,
        outputSummary: {
          profile: "cloud-agents-proto-sdk/v1alpha1",
          transport: "INJECTED_CONNECT_FIXTURE_TRANSPORT_ONLY",
          protocols: ["connect", "grpc"],
          serverRouteRegistration: "NOT_IMPLEMENTED",
          p2Surface: "NOT_IMPLEMENTED",
          providerSideEffects: "FORBIDDEN",
          productionDatabaseWrites: "NOT_AUTHORIZED",
          gateStatus: "ALL_GATES_OPEN",
        },
      });
      expect(pipeline?.inputs).toEqual([...(pipeline?.inputs ?? [])].toSorted());
      expect(pipeline?.inputs).toContain(
        "docs/plan/p1/dependency-reviews/proto-sdk-toolchain-20260821.md",
      );
      expect(pipeline?.inputs).toContain("scripts/test-platform-sdk-consumers.ts");
    }
    expect(go?.generatedOutputs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "sdk/go/proto-generated-manifest.json" }),
        expect.objectContaining({ path: "sdk/go/gen/cloudagents/worker/v1alpha1/kernel.pb.go" }),
      ]),
    );
    expect(typescript?.generatedOutputs).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ path: "sdk/typescript/proto-generated-manifest.json" }),
        expect.objectContaining({ path: "sdk/typescript/src/proto.ts" }),
      ]),
    );
    expect(go?.inputs).toContain("sdk/go/proto_conformance_test.go");
    expect(typescript?.inputs).toContain("sdk/typescript/src/proto.test.ts");
  });

  it("rejects symbolic links in recursive migration inputs", () => {
    const root = temporaryRoot();
    mkdirSync(join(root, "catalog"));
    writeFileSync(join(root, "outside.json"), "{}\n");
    symlinkSync(join(root, "outside.json"), join(root, "catalog", "linked.json"));
    expect(() => listRegularMigrationInputFiles(root, "catalog")).toThrow(/symbolic links/u);
  });

  it("sorts nested migration inputs and rejects an escaping root", () => {
    const root = temporaryRoot();
    mkdirSync(join(root, "fixtures", "z"), { recursive: true });
    mkdirSync(join(root, "fixtures", "a"), { recursive: true });
    writeFileSync(join(root, "fixtures", "z", "second.json"), "{}\n");
    writeFileSync(join(root, "fixtures", "a", "first.json"), "{}\n");
    expect(listRegularMigrationInputFiles(root, "fixtures")).toEqual([
      "fixtures/a/first.json",
      "fixtures/z/second.json",
    ]);
    expect(() => listRegularMigrationInputFiles(root, "../outside")).toThrow(/escapes/u);
  });
});
