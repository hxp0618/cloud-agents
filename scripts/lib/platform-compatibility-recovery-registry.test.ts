import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  buildCompatibilityRecoveryRegistry,
  buildCompatibilityRecoveryRegistryV2,
  COMPATIBILITY_RECOVERY_OUTPUT_PATH,
  COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
  validateCompatibilityRecoveryFixture,
  validateCompatibilityRecoverySource,
  validateCompatibilityRecoverySourceV2,
} from "./platform-compatibility-recovery-registry";

const root = join(import.meta.dirname, "../..");

describe("A2.4 compatibility and recovery generated registry", () => {
  it("keeps the historical v1 artifact byte-exact", () => {
    const bytes = readFileSync(join(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH));
    expect(createHash("sha256").update(bytes).digest("hex")).toBe(
      "f8a0ff0ebc91bab93b1bacf5ec6241f44c8639ae8a11dc0712b485f88156e812",
    );
    expect(buildCompatibilityRecoveryRegistry(root).registryDigest).toBe(
      "sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d",
    );
  });

  it("binds the five profiles, five closed machines, and fail-closed boundary", () => {
    const registry = buildCompatibilityRecoveryRegistry(root);
    const profiles = registry.profiles as Array<{ readonly spec: { readonly profileId: string } }>;
    const stateMachines = registry.stateMachines as Array<{ readonly id: string }>;
    expect(profiles.map((entry) => entry.spec.profileId)).toEqual([
      "backfill/v1",
      "live-instance/v1",
      "migration-preflight/v1",
      "restore-evidence/v1",
      "retirement-receipt/v1",
    ]);
    expect(stateMachines.map((machine) => machine.id)).toEqual([
      "backfill/v1",
      "live-instance/v1",
      "migration-preflight/v1",
      "restore-evidence/v1",
      "retirement-receipt/v1",
    ]);
    expect(registry.implementationBoundary).toEqual({
      sqlMigration: "not_implemented_no_000010",
      goConsumer: "not_implemented",
      httpSurface: "not_implemented",
      externalSideEffects: "forbidden",
      gateStatus: "non_gate_evidence_only",
    });
    expect(registry.registryDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(readFileSync(join(root, COMPATIBILITY_RECOVERY_OUTPUT_PATH), "utf8")).toContain(
      registry.registryDigest,
    );
  });

  it("rejects profile, state-machine, and retirement-proof drift before generation", () => {
    const source = JSON.parse(
      readFileSync(
        join(
          root,
          "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
        ),
        "utf8",
      ),
    );
    const missingProfile = structuredClone(source);
    missingProfile.profiles[1].requiredEvidence =
      missingProfile.profiles[1].requiredEvidence.filter(
        (field: string) => field !== "heartbeat_ttl_seconds",
      );
    expect(() => validateCompatibilityRecoverySource(root, missingProfile)).toThrow(
      /TTL|incomplete/u,
    );

    const nondeterministic = structuredClone(source);
    nondeterministic.stateMachines[0].transitions[0].from = "running";
    expect(() => validateCompatibilityRecoverySource(root, nondeterministic)).toThrow(
      /sorted and unique|nondeterministic|drifted/u,
    );

    const unsafeBoundary = structuredClone(source);
    unsafeBoundary.implementationBoundary.httpSurface = "enabled";
    expect(() => validateCompatibilityRecoverySource(root, unsafeBoundary)).toThrow(
      /boundary|constant/u,
    );

    const missingPolicyFact = structuredClone(source);
    missingPolicyFact.policies.preflight.requiredEvidence[0] = "unapproved_fact";
    missingPolicyFact.policies.preflight.requiredEvidence.sort();
    expect(() => validateCompatibilityRecoverySource(root, missingPolicyFact)).toThrow(
      /evidence catalog/u,
    );

    const unreachableState = structuredClone(source);
    unreachableState.stateMachines[0].states.push("orphaned");
    unreachableState.stateMachines[0].states.sort();
    expect(() => validateCompatibilityRecoverySource(root, unreachableState)).toThrow(/reachable/u);

    const deadEndState = structuredClone(source);
    deadEndState.stateMachines[0].states.push("stuck");
    deadEndState.stateMachines[0].states.sort();
    deadEndState.stateMachines[0].transitions.push({
      from: "pending",
      event: "stuck",
      to: "stuck",
    });
    deadEndState.stateMachines[0].transitions.sort(
      (
        left: { from: string; event: string; to: string },
        right: { from: string; event: string; to: string },
      ) =>
        `${left.from}\0${left.event}\0${left.to}`.localeCompare(
          `${right.from}\0${right.event}\0${right.to}`,
        ),
    );
    expect(() => validateCompatibilityRecoverySource(root, deadEndState)).toThrow(
      /path to a terminal/u,
    );
  });

  it("rejects a generated registry whose policy changed without regeneration", () => {
    const registry = buildCompatibilityRecoveryRegistry(root);
    const mutated = structuredClone(registry);
    (mutated.policies as { failureMode: string }).failureMode = "fail_open";

    expect(validateCompatibilityRecoveryFixture(mutated, root)).toEqual({
      valid: false,
      errors: [
        {
          code: "COMPATIBILITY_RECOVERY_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ],
    });
  });

  it("binds the versioned six-profile registry to 000010 and closed operation contracts", () => {
    const registry = buildCompatibilityRecoveryRegistryV2(root);
    const profiles = registry.profiles as Array<{
      readonly spec: {
        readonly profileId: string;
        readonly accessMode: string;
        readonly operations: ReadonlyArray<{
          readonly operationId: string;
          readonly sqlFunction: string;
          readonly serviceMethod: string;
          readonly mode: string;
          readonly unknownOutcome: string;
        }>;
      };
    }>;
    expect(profiles.map((entry) => entry.spec.profileId)).toEqual([
      "backfill/v2",
      "live-instance/v2",
      "migration-preflight/v2",
      "restore-evidence/v2",
      "retirement-receipt/v2",
      "workload-principal/v2",
    ]);
    expect(profiles.map((entry) => entry.spec.operations.length)).toEqual([6, 7, 1, 4, 4, 4]);
    expect(profiles[2]?.spec.accessMode).toBe("read_only");
    expect(
      profiles
        .flatMap((entry) => entry.spec.operations)
        .every(
          (operation) =>
            operation.operationId.endsWith("/v2") &&
            operation.sqlFunction.startsWith("cloud_agents.compatibility_recovery_") &&
            /^[A-Z][A-Za-z]+$/u.test(operation.serviceMethod) &&
            (operation.mode === "read_only"
              ? operation.unknownOutcome === "not_applicable"
              : operation.unknownOutcome === "reconcile_required_no_write_retry"),
        ),
    ).toBe(true);
    expect(registry.schemaBinding).toEqual({
      schemaHead: "000010",
      schemaCatalogSha256:
        "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236",
      schemaMigrationSha256:
        "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6",
    });
    expect(registry.historicalCompatibility).toEqual({
      priorFormatVersion: "cloud-agents-compatibility-recovery-source/v1",
      priorRegistryDigest:
        "sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d",
      mode: "historical_schema_only_non_authority",
    });
    expect(registry.implementationBoundary).toEqual({
      sqlWriterMigration: "not_implemented_after_000010",
      goConsumer: "not_implemented",
      httpSurface: "not_implemented",
      externalSideEffects: "forbidden",
      providerSideEffects: "forbidden",
      productionDatabaseWrites: "not_authorized",
      gateStatus: "all_gates_open",
    });
    expect(readFileSync(join(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH), "utf8")).toContain(
      registry.registryDigest,
    );
  });

  it("rejects v2 schema, selector, operation, and side-effect drift", () => {
    const source = JSON.parse(
      readFileSync(
        join(
          root,
          "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json",
        ),
        "utf8",
      ),
    );

    const schemaDrift = structuredClone(source);
    schemaDrift.schemaBinding.schemaHead = "000009";
    expect(() => validateCompatibilityRecoverySourceV2(root, schemaDrift)).toThrow(
      /schema binding|constant/u,
    );

    const selectorDrift = structuredClone(source);
    selectorDrift.selector.callerProvidedProfile = "allowed";
    expect(() => validateCompatibilityRecoverySourceV2(root, selectorDrift)).toThrow(
      /selector|constant/u,
    );

    const operationDrift = structuredClone(source);
    operationDrift.profiles[0].operations[0].serviceMethod = "LooseWriter";
    expect(() => validateCompatibilityRecoverySourceV2(root, operationDrift)).toThrow(
      /operation identities/u,
    );

    const boundaryDrift = structuredClone(source);
    boundaryDrift.implementationBoundary.providerSideEffects = "enabled";
    expect(() => validateCompatibilityRecoverySourceV2(root, boundaryDrift)).toThrow(
      /boundary|constant/u,
    );
  });

  it("rejects a generated v2 registry whose typed operation changed", () => {
    const registry = buildCompatibilityRecoveryRegistryV2(root);
    const mutated = structuredClone(registry);
    mutated.profiles[0].spec.operations[0].serviceMethod = "LooseWriter";
    expect(validateCompatibilityRecoveryFixture(mutated, root)).toEqual({
      valid: false,
      errors: [
        {
          code: "COMPATIBILITY_RECOVERY_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ],
    });
  });

  it("rejects v2 generation when the historical v1 registry drifts", () => {
    const temporaryRoot = mkdtempSync(join(tmpdir(), "compatibility-recovery-v2-"));
    const files = [
      "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
      "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v2.json",
      "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json",
      "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v1.schema.json",
      "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-source-v2.schema.json",
      "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v1.schema.json",
      "contracts/platform/v1alpha1/schemas/compatibility-recovery-registry-v2.schema.json",
      "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
      "services/control-plane/migrations/catalog/schema-000010.json",
    ];
    for (const file of files) {
      const target = join(temporaryRoot, file);
      mkdirSync(dirname(target), { recursive: true });
      copyFileSync(join(root, file), target);
    }
    const historicalSourcePath = join(
      temporaryRoot,
      "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
    );
    const historicalSource = JSON.parse(readFileSync(historicalSourcePath, "utf8"));
    historicalSource.schemaRange.maxInclusive = "000008";
    writeFileSync(historicalSourcePath, `${JSON.stringify(historicalSource, null, 2)}\n`);

    expect(() => buildCompatibilityRecoveryRegistryV2(temporaryRoot)).toThrow(
      /exact same-bits generated v1 registry/u,
    );

    copyFileSync(
      join(
        root,
        "contracts/platform/v1alpha1/fixtures/golden/compatibility-recovery-registry-source-v1.json",
      ),
      historicalSourcePath,
    );
    const historicalOutputPath = join(
      temporaryRoot,
      "contracts/generated/platform/v1alpha1/compatibility-recovery-registry.json",
    );
    writeFileSync(historicalOutputPath, `${readFileSync(historicalOutputPath, "utf8")} `);
    expect(() => buildCompatibilityRecoveryRegistryV2(temporaryRoot)).toThrow(
      /exact same-bits generated v1 registry/u,
    );
  });
});
