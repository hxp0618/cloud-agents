import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, test } from "vitest";
import { format } from "oxfmt";

import { buildRunnerLedgerConsumerRegistry } from "../../scripts/lib/platform-runner-ledger-consumer-registry";
import {
  assertRunnerLedgerRecoveryRegistriesCurrent,
  buildRunnerLedgerRecoveryRegistries,
  expectedRunnerLedgerRecoverySourceSuite,
  RUNNER_LEDGER_RECOVERY_FAMILIES,
  RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS,
  RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS,
  RUNNER_LEDGER_RECOVERY_SOURCE_PATH,
  serializeRunnerLedgerRecoveryRegistry,
  validateRunnerLedgerRecoveryFixture,
} from "../../scripts/lib/platform-runner-ledger-recovery-registry";

const root = resolve(import.meta.dirname, "../..");

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, any>;
}

describe("runner ledger recovery generated registry suite", () => {
  test("is current, ordered, and bound to the exact immutable predecessor identities", () => {
    expect(() => assertRunnerLedgerRecoveryRegistriesCurrent(root)).not.toThrow();
    expect(readJson(RUNNER_LEDGER_RECOVERY_SOURCE_PATH)).toEqual(
      expectedRunnerLedgerRecoverySourceSuite(),
    );
    const consumer = buildRunnerLedgerConsumerRegistry(root) as Record<string, any>;
    const registries = buildRunnerLedgerRecoveryRegistries(root) as Array<Record<string, any>>;
    expect(registries).toHaveLength(8);
    expect(registries.map((item) => item.family)).toEqual(RUNNER_LEDGER_RECOVERY_FAMILIES);
    expect(registries[0]?.predecessorBinding).toMatchObject({
      registryId: consumer.registryId,
      registryDigest: consumer.registryDigest,
      profileId: "runner-ledger-consumer/v1",
      profileDigest: consumer.profile.profileDigest,
    });
    expect(registries[0]?.historicalBindings.map((item: any) => item.profileId)).toEqual([
      "runner-ledger-preflight/v1",
      "runner-ledger-consumer/v1",
      "runner-ledger-entry-admission/v1",
      "runner-ledger-entry-execution-admission/v1",
      "runner-ledger-entry-success-writer/v1",
    ]);
    for (const [index, family] of RUNNER_LEDGER_RECOVERY_FAMILIES.entries()) {
      expect(readJson(RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family])).toEqual(registries[index]);
    }
  }, 30_000);

  test("emits repository-formatter-clean deterministic registry bytes", async () => {
    const registries = buildRunnerLedgerRecoveryRegistries(root) as Array<Record<string, any>>;
    for (const [index, registry] of registries.entries()) {
      const family = RUNNER_LEDGER_RECOVERY_FAMILIES[index]!;
      const serialized = serializeRunnerLedgerRecoveryRegistry(registry);
      const result = await format(`${family}.json`, serialized, { printWidth: 100 });
      expect(result.errors, family).toEqual([]);
      expect(result.code, family).toBe(serialized);
    }
  });

  test("closes the exact twelve-pair mapping without a union writer", () => {
    const registries = buildRunnerLedgerRecoveryRegistries(root) as Array<Record<string, any>>;
    const counts = registries.map((item) => item.profile.spec.pairBindings.length);
    expect(counts).toEqual([12, 4, 1, 1, 1, 3, 0, 2]);
    expect(registries[0]?.profile.spec.pairBindings).toEqual(RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS);
    const uniquePairs = new Set(
      RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS.map(
        (item) => `${item.disposition}\u0000${item.state}\u0000${item.action}`,
      ),
    );
    expect(uniquePairs.size).toBe(12);
    const execution = registries[5]!;
    const writer = registries[6]!;
    expect(execution.profile.spec.profileId).toBe("runner-ledger-recovery-execution-admission/v1");
    expect(writer.profile.spec.profileId).toBe("runner-ledger-recovery-success-writer/v1");
    expect(writer.predecessorBinding.profileId).toBe(execution.profile.spec.profileId);
    expect(writer.predecessorBinding.registryDigest).toBe(execution.registryDigest);
    expect(writer.profile.spec.permitFromProfileId).toBe(execution.profile.spec.profileId);
    expect(writer.profile.spec.pairBindings).toEqual([]);
    expect(writer.registryId).not.toBe(execution.registryId);
    expect(writer.registryDigest).not.toBe(execution.registryDigest);
    expect(writer.profile.profileDigest).not.toBe(execution.profile.profileDigest);
  });

  test("rejects source order, pair, predecessor, union-writer, and profile drift", () => {
    const source = readJson(RUNNER_LEDGER_RECOVERY_SOURCE_PATH);
    const cases: Array<[string, (candidate: Record<string, any>) => void]> = [
      [
        "profile order",
        (candidate) => {
          [candidate.profiles[0], candidate.profiles[1]] = [
            candidate.profiles[1],
            candidate.profiles[0],
          ];
        },
      ],
      [
        "pair action",
        (candidate) => {
          candidate.profiles[0].pairBindings[0].profileAction = "prepare_retry_handoff";
        },
      ],
      [
        "predecessor",
        (candidate) => {
          candidate.profiles[5].predecessorProfileId = "runner-ledger-entry-admission/v1";
        },
      ],
      [
        "union writer",
        (candidate) => {
          candidate.profiles[6].pairBindings.push(candidate.profiles[0].pairBindings[0]);
        },
      ],
      [
        "permit binding",
        (candidate) => {
          candidate.profiles[6].permitFromProfileId = "runner-ledger-recovery-admission/v1";
        },
      ],
      [
        "profile identity",
        (candidate) => {
          candidate.profiles[7].profileId = "runner-ledger-return-failure/v2";
        },
      ],
    ];
    for (const [name, mutate] of cases) {
      const candidate = structuredClone(source);
      mutate(candidate);
      const result = validateRunnerLedgerRecoveryFixture(candidate, root);
      expect(result.valid, name).toBe(false);
      if (!result.valid) {
        expect(result.errors[0]?.code, name).toBe("RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH");
      }
    }
  });

  test("rejects edited generated registry identity and policy bytes", () => {
    for (const family of RUNNER_LEDGER_RECOVERY_FAMILIES) {
      const candidate = readJson(RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family]);
      candidate.profile.spec.identityBindings.crossProfileRejection = "allowed";
      const result = validateRunnerLedgerRecoveryFixture(candidate, root);
      expect(result.valid, family).toBe(false);
      if (!result.valid) {
        expect(result.errors[0]?.code, family).toBe(
          "RUNNER_LEDGER_RECOVERY_REGISTRY_DIGEST_MISMATCH",
        );
      }
    }
  }, 30_000);
});
