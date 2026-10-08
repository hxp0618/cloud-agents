import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, test } from "vitest";

import { buildRunnerLedgerEntryAdmissionRegistry } from "../../scripts/lib/platform-runner-ledger-entry-admission-registry";
import {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent,
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent,
  buildRunnerLedgerEntryExecutionAdmissionRegistry,
  buildRunnerLedgerEntrySuccessWriterRegistry,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_SOURCE_PATH,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_SOURCE_PATH,
  validateRunnerLedgerEntryWriterFixture,
} from "../../scripts/lib/platform-runner-ledger-entry-writer-registry";

const root = resolve(import.meta.dirname, "../..");

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, any>;
}

describe("runner ledger entry execution/success-writer generated registries", () => {
  test("are current and cross-bind exact generated predecessor identities", () => {
    expect(() => assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root)).not.toThrow();
    expect(() => assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root)).not.toThrow();
    const entryAdmission = buildRunnerLedgerEntryAdmissionRegistry(root) as Record<string, any>;
    const execution = buildRunnerLedgerEntryExecutionAdmissionRegistry(root) as Record<string, any>;
    const writer = buildRunnerLedgerEntrySuccessWriterRegistry(root) as Record<string, any>;
    expect(execution.entryAdmissionBinding).toEqual({
      registryId: entryAdmission.registryId,
      registryDigest: entryAdmission.registryDigest,
      stateMachineDigest: entryAdmission.stateMachineDigest,
      policyDigest: entryAdmission.policyDigest,
      profileId: entryAdmission.profile.spec.profileId,
      profileDigest: entryAdmission.profile.profileDigest,
    });
    expect(writer.executionAdmissionBinding).toEqual({
      registryId: execution.registryId,
      registryDigest: execution.registryDigest,
      stateMachineDigest: execution.stateMachineDigest,
      policyDigest: execution.policyDigest,
      profileId: execution.profile.spec.profileId,
      profileDigest: execution.profile.profileDigest,
    });
    expect(readJson(RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH)).toEqual(execution);
    expect(readJson(RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH)).toEqual(writer);
  });

  test("admits exactly four first-attempt pairs and excludes the historical retry pair", () => {
    const pairs = Object.values(RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX).flat();
    expect(pairs).toHaveLength(4);
    expect(pairs.every((pair) => pair.executionAction === "prepare_entry_execution")).toBe(true);
    expect(
      pairs.some(
        (pair) => pair.state === "brand_new_inherited" && pair.action === "begin_next_attempt",
      ),
    ).toBe(false);
    expect(RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION).toEqual({
      executionAction: "prepare_entry_execution",
      action: "execute_one_entry_known_success",
    });
  });

  test("rejects execution pair, selector, boundary, and state-machine drift", () => {
    const source = readJson(RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_SOURCE_PATH);
    const cases: Array<[string, (candidate: Record<string, any>) => void, string]> = [
      [
        "retry pair",
        (candidate) => {
          candidate.profile.transitionMatrix.empty_brand_new.push({
            state: "brand_new_inherited",
            action: "begin_next_attempt",
            executionAction: "prepare_entry_execution",
          });
        },
        "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      ],
      [
        "selector",
        (candidate) => {
          candidate.selector.closeOnlyPermitAsExecutionPermit = "allowed";
        },
        "RUNNER_LEDGER_ENTRY_EXECUTION_BINDING_MISMATCH",
      ],
      [
        "boundary",
        (candidate) => {
          candidate.implementationBoundary.beginMigration = "allowed";
        },
        "RUNNER_LEDGER_ENTRY_EXECUTION_BOUNDARY_MISMATCH",
      ],
      [
        "state machine",
        (candidate) => {
          candidate.stateMachine.transitions[4].event = "begin_transaction";
        },
        "RUNNER_LEDGER_ENTRY_EXECUTION_STATE_MACHINE_INVALID",
      ],
    ];
    for (const [name, mutate, code] of cases) {
      const candidate = structuredClone(source);
      mutate(candidate);
      const result = validateRunnerLedgerEntryWriterFixture(candidate, root);
      expect(result.valid, name).toBe(false);
      if (!result.valid) expect(result.errors[0]?.code, name).toBe(code);
    }
  });

  test("rejects writer action, selector, boundary, and state-machine drift", () => {
    const source = readJson(RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_SOURCE_PATH);
    const cases: Array<[string, (candidate: Record<string, any>) => void, string]> = [
      [
        "writer action",
        (candidate) => {
          candidate.profile.writerAction.action = "retry_entry";
        },
        "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BINDING_MISMATCH",
      ],
      [
        "selector",
        (candidate) => {
          candidate.selector.callerProvidedAction = "allowed";
        },
        "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BINDING_MISMATCH",
      ],
      [
        "boundary",
        (candidate) => {
          candidate.implementationBoundary.retryWriter = "implemented";
        },
        "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_BOUNDARY_MISMATCH",
      ],
      [
        "state machine",
        (candidate) => {
          candidate.stateMachine.transitions[0].event = "caller_selected";
        },
        "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_STATE_MACHINE_INVALID",
      ],
    ];
    for (const [name, mutate, code] of cases) {
      const candidate = structuredClone(source);
      mutate(candidate);
      const result = validateRunnerLedgerEntryWriterFixture(candidate, root);
      expect(result.valid, name).toBe(false);
      if (!result.valid) expect(result.errors[0]?.code, name).toBe(code);
    }
  });

  test("rejects edited generated registries as ordinary JSON", () => {
    for (const [path, code] of [
      [
        RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
        "RUNNER_LEDGER_ENTRY_EXECUTION_REGISTRY_DIGEST_MISMATCH",
      ],
      [
        RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
        "RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_REGISTRY_DIGEST_MISMATCH",
      ],
    ] as const) {
      const generated = readJson(path);
      generated.registryDigest = "sha256:" + "0".repeat(64);
      const result = validateRunnerLedgerEntryWriterFixture(generated, root);
      expect(result.valid, path).toBe(false);
      if (!result.valid) expect(result.errors[0]?.code, path).toBe(code);
    }
  });
});
