import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, test } from "vitest";

import { buildRunnerLedgerConsumerRegistry } from "./platform-runner-ledger-consumer-registry";
import {
  assertRunnerLedgerEntryAdmissionRegistryCurrent,
  buildRunnerLedgerEntryAdmissionRegistry,
  RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_ADMISSION_SOURCE_PATH,
  RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX,
  validateRunnerLedgerEntryAdmissionFixture,
} from "./platform-runner-ledger-entry-admission-registry";

const root = resolve(import.meta.dirname, "../..");

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, any>;
}

describe("runner ledger entry-admission generated registry", () => {
  test("is current and binds the exact immutable consumer-v1 identity", () => {
    expect(() => assertRunnerLedgerEntryAdmissionRegistryCurrent(root)).not.toThrow();
    const registry = buildRunnerLedgerEntryAdmissionRegistry(root) as Record<string, any>;
    const consumer = buildRunnerLedgerConsumerRegistry(root) as Record<string, any>;
    expect(registry.consumerBinding).toEqual({
      registryId: consumer.registryId,
      registryDigest: consumer.registryDigest,
      stateMachineDigest: consumer.stateMachineDigest,
      policyDigest: consumer.policyDigest,
      profileId: consumer.profile.spec.profileId,
      profileDigest: consumer.profile.profileDigest,
    });
    expect(registry.profile.spec.transitionMatrix).toEqual(
      RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX,
    );
    expect(readJson(RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH)).toEqual(registry);
  });

  test("admits exactly the five immutable entry pairs", () => {
    const matrix = RUNNER_LEDGER_ENTRY_ADMISSION_TRANSITION_MATRIX;
    expect(Object.keys(matrix)).toEqual(["empty_brand_new", "partial_next_entry"]);
    const pairs = Object.values(matrix).flat();
    expect(pairs).toHaveLength(5);
    expect(pairs.every((pair) => pair.admissionAction === "prepare_entry_admission")).toBe(true);
    const consumer = buildRunnerLedgerConsumerRegistry(root) as Record<string, any>;
    const expected = Object.entries(consumer.profile.spec.transitionMatrix).flatMap(
      ([disposition, values]) =>
        (values as Array<Record<string, string>>)
          .filter((pair) => pair.consumerAction === "entry_not_implemented")
          .map((pair) => ({ disposition, state: pair.state, action: pair.action })),
    );
    const actual = Object.entries(matrix).flatMap(([disposition, values]) =>
      values.map((pair) => ({ disposition, state: pair.state, action: pair.action })),
    );
    expect(actual).toEqual(expected);
  });

  test("rejects pair, selector, boundary, and state-machine drift", () => {
    const source = readJson(RUNNER_LEDGER_ENTRY_ADMISSION_SOURCE_PATH);
    const cases: Array<[string, (candidate: Record<string, any>) => void, string]> = [
      [
        "pair",
        (candidate) => {
          candidate.profile.transitionMatrix.empty_brand_new[0].admissionAction = "execute_entry";
        },
        "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      ],
      [
        "selector",
        (candidate) => {
          candidate.selector.ordinaryFactAsPermit = "allowed";
        },
        "RUNNER_LEDGER_ENTRY_ADMISSION_BINDING_MISMATCH",
      ],
      [
        "boundary",
        (candidate) => {
          candidate.implementationBoundary.beginMigration = "allowed";
        },
        "RUNNER_LEDGER_ENTRY_ADMISSION_BOUNDARY_MISMATCH",
      ],
      [
        "state machine",
        (candidate) => {
          candidate.stateMachine.transitions[4].event = "execute_entry";
        },
        "RUNNER_LEDGER_ENTRY_ADMISSION_STATE_MACHINE_INVALID",
      ],
    ];
    for (const [name, mutate, code] of cases) {
      const candidate = structuredClone(source);
      mutate(candidate);
      const result = validateRunnerLedgerEntryAdmissionFixture(candidate, root);
      expect(result.valid, name).toBe(false);
      if (!result.valid) expect(result.errors[0]?.code, name).toBe(code);
    }
  });

  test("rejects an edited generated registry and never treats ordinary JSON as authority", () => {
    const generated = readJson(RUNNER_LEDGER_ENTRY_ADMISSION_OUTPUT_PATH);
    generated.registryDigest = "sha256:" + "0".repeat(64);
    const result = validateRunnerLedgerEntryAdmissionFixture(generated, root);
    expect(result.valid).toBe(false);
    if (!result.valid) {
      expect(result.errors).toEqual([
        {
          code: "RUNNER_LEDGER_ENTRY_ADMISSION_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ]);
    }
  });
});
