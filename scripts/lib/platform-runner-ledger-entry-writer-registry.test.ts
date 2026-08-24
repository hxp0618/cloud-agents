import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, test } from "bun:test";

import { buildRunnerLedgerEntryAdmissionRegistry } from "./platform-runner-ledger-entry-admission-registry";
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
} from "./platform-runner-ledger-entry-writer-registry";

const root = resolve(import.meta.dirname, "../..");

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, any>;
}

function fileSha256(path: string): string {
  return createHash("sha256")
    .update(readFileSync(resolve(root, path)))
    .digest("hex");
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

  test("preserves every historical preflight, consumer, and close-only admission v1 artifact", () => {
    const expected: Record<string, string> = {
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json":
        "bd1a9e57fd5f1014a7afead056d6c03f1b0a8501e9767e1eb0308aef9065bd71",
      "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json":
        "2c48a4db4641de750336fb2cfb454da98998a494002342f29201c17dfdbc7204",
      "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json":
        "829b9e7aefaf16642090051b93babb311790a3be354f91ed91520cca39079c5c",
      "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json":
        "2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c",
      "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go":
        "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json":
        "3b81553a58077bb1e748f7f4f6474c59ac9d8dcfb5fdbffd1cab00d7d4361b64",
      "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json":
        "c1a82d48448a38c94d613d05b28a933ac95986ea038e85f35bac4d0590387120",
      "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json":
        "bb8f9557621825f45150a4f1b3b7708566f3a0e790077307406fd287b6a86ae6",
      "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json":
        "fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852",
      "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go":
        "afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json":
        "56fcaa4806731ff968c4614f710502b39dad66ee64784662bf962f58a37c3b88",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json":
        "5bd4c267e62d87287ae68a0f6f4e8a2d2dbf3e65af0a96d522019342e2abb17b",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json":
        "bbe0c63c2942b8286fca6daca546c54b4c43efd0ab7193cd0c4e10ef6e27d409",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json":
        "2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372",
      "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go":
        "c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6",
      "services/control-plane/internal/migration/runner_ledger_entry_admission_permit.go":
        "255088e37e40d897d76ba589dbf2afd9dbb7dcf3e9d17e6b9d752735f4306714",
    };
    for (const [path, digest] of Object.entries(expected)) {
      expect(fileSha256(path), path).toBe(digest);
    }
  });

  test("admits exactly four first-attempt pairs and excludes the historical retry pair", () => {
    const pairs = Object.values(RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX).flat();
    expect(pairs).toHaveLength(4);
    expect(pairs.every((pair) => pair.executionAction === "prepare_entry_execution")).toBeTrue();
    expect(
      pairs.some(
        (pair) => pair.state === "brand_new_inherited" && pair.action === "begin_next_attempt",
      ),
    ).toBeFalse();
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
      expect(result.valid, name).toBeFalse();
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
      expect(result.valid, name).toBeFalse();
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
      expect(result.valid, path).toBeFalse();
      if (!result.valid) expect(result.errors[0]?.code, path).toBe(code);
    }
  });
});
