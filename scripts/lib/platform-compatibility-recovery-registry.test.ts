import { readFileSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import {
  buildCompatibilityRecoveryRegistry,
  COMPATIBILITY_RECOVERY_OUTPUT_PATH,
  validateCompatibilityRecoveryFixture,
  validateCompatibilityRecoverySource,
} from "./platform-compatibility-recovery-registry";

const root = join(import.meta.dirname, "../..");

describe("A2.4 compatibility and recovery generated registry", () => {
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
});
