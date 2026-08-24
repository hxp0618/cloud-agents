import { cpSync, lstatSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertRunnerLedgerConsumerRegistryCurrent,
  buildRunnerLedgerConsumerRegistry,
  runnerLedgerConsumerRegistryInputs,
  RunnerLedgerConsumerContractError,
  RUNNER_LEDGER_CONSUMER_SOURCE_PATH,
  RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX,
  serializeRunnerLedgerConsumerRegistry,
  validateRunnerLedgerConsumerFixture,
  validateRunnerLedgerConsumerSource,
} from "./platform-runner-ledger-consumer-registry";
import {
  buildRunnerLedgerPreflightRegistry,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
} from "./platform-runner-ledger-preflight-registry";

type JsonRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryContractRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "runner-ledger-consumer-registry-"));
  temporaryRoots.push(root);
  cpSync(resolve(repositoryRoot, "contracts"), resolve(root, "contracts"), { recursive: true });
  return root;
}

function readSource(root: string): JsonRecord {
  return JSON.parse(
    readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_SOURCE_PATH), "utf8"),
  ) as JsonRecord;
}

function writeSource(root: string, value: unknown): void {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_CONSUMER_SOURCE_PATH),
    `${JSON.stringify(value, null, 2)}\n`,
  );
}

function expectContractError(action: () => unknown, code: string): void {
  try {
    action();
    throw new Error(`Expected ${code}.`);
  } catch (error) {
    expect(error).toBeInstanceOf(RunnerLedgerConsumerContractError);
    expect((error as RunnerLedgerConsumerContractError).code).toBe(code);
  }
}

describe("runner ledger consumer generated contract registry", () => {
  it("is byte-current, deterministic, and host independent", () => {
    expect(() => assertRunnerLedgerConsumerRegistryCurrent(repositoryRoot)).not.toThrow();
    const first = serializeRunnerLedgerConsumerRegistry(
      buildRunnerLedgerConsumerRegistry(repositoryRoot),
    );
    const second = serializeRunnerLedgerConsumerRegistry(
      buildRunnerLedgerConsumerRegistry(repositoryRoot),
    );
    expect(first).toBe(second);
    expect(first).not.toMatch(/generatedAt|generated_at|\/Users\//u);
  });

  it("binds immutable preflight v1 and maps exactly one, five, and eleven pairs", () => {
    const registry = buildRunnerLedgerConsumerRegistry(repositoryRoot) as JsonRecord & {
      preflightBinding: JsonRecord;
      profile: { profileDigest: string; spec: JsonRecord };
      implementationBoundary: JsonRecord;
    };
    const preflight = buildRunnerLedgerPreflightRegistry(repositoryRoot) as JsonRecord & {
      profile: { profileDigest: string; spec: JsonRecord };
    };
    expect(registry.preflightBinding).toEqual({
      registryId: preflight.registryId,
      registryDigest: preflight.registryDigest,
      stateMachineDigest: preflight.stateMachineDigest,
      policyDigest: preflight.policyDigest,
      profileId: preflight.profile.spec.profileId,
      profileDigest: preflight.profile.profileDigest,
    });
    expect(registry.profile.spec.transitionMatrix).toEqual(
      RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX,
    );
    const actions = Object.values(RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX)
      .flat()
      .map((pair) => pair.consumerAction);
    expect(actions.filter((action) => action === "return_success_noop")).toHaveLength(1);
    expect(actions.filter((action) => action === "entry_not_implemented")).toHaveLength(5);
    expect(actions.filter((action) => action === "recovery_not_implemented")).toHaveLength(11);
    expect(registry.implementationBoundary).toMatchObject({
      runnerConsumer: "complete_return_success_noop_only",
      existingBrandNewWriter: "separate_existing_authority_chain",
      entryWriter: "not_implemented",
      recoveryWriter: "not_implemented",
      databaseTransaction: "forbidden",
      ledgerMutation: "forbidden",
      evidenceMutation: "forbidden",
      productionDatabaseWrites: "not_authorized",
      gateStatus: "all_gates_open",
    });
  });

  it("does not change the checked-in preflight v1 bytes", () => {
    const bytes = readFileSync(resolve(repositoryRoot, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH));
    expect(createHash("sha256").update(bytes).digest("hex")).toBe(
      "2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c",
    );
  });

  it("rejects selector, state-machine, boundary, and pair drift", () => {
    const selectorRoot = temporaryContractRoot();
    const selector = readSource(selectorRoot);
    (selector.selector as JsonRecord).callerProvidedDispatch = "allowed";
    writeSource(selectorRoot, selector);
    expectContractError(
      () => validateRunnerLedgerConsumerSource(selectorRoot, readSource(selectorRoot) as never),
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
    );

    const stateRoot = temporaryContractRoot();
    const state = readSource(stateRoot);
    ((state.stateMachine as JsonRecord).transitions as unknown[]).pop();
    writeSource(stateRoot, state);
    expectContractError(
      () => validateRunnerLedgerConsumerSource(stateRoot, readSource(stateRoot) as never),
      "RUNNER_LEDGER_CONSUMER_STATE_MACHINE_INVALID",
    );

    const boundaryRoot = temporaryContractRoot();
    const boundary = readSource(boundaryRoot);
    (boundary.implementationBoundary as JsonRecord).entryWriter = "implemented";
    writeSource(boundaryRoot, boundary);
    expectContractError(
      () => validateRunnerLedgerConsumerSource(boundaryRoot, readSource(boundaryRoot) as never),
      "RUNNER_LEDGER_CONSUMER_BOUNDARY_MISMATCH",
    );

    const matrixRoot = temporaryContractRoot();
    const matrix = readSource(matrixRoot);
    const transitions = (matrix.profile as JsonRecord).transitionMatrix as JsonRecord;
    ((transitions.partial_next_entry as JsonRecord[])[0] as JsonRecord).consumerAction =
      "return_success_noop";
    writeSource(matrixRoot, matrix);
    expectContractError(
      () => validateRunnerLedgerConsumerSource(matrixRoot, readSource(matrixRoot) as never),
      "RUNNER_LEDGER_CONSUMER_BINDING_MISMATCH",
    );
  });

  it("rejects every mutation of a generated registry", () => {
    const generated = buildRunnerLedgerConsumerRegistry(repositoryRoot) as JsonRecord & {
      selector: JsonRecord;
    };
    generated.selector.callerProvidedDispatch = "allowed";
    expect(validateRunnerLedgerConsumerFixture(generated, repositoryRoot)).toEqual({
      valid: false,
      errors: [
        {
          code: "RUNNER_LEDGER_CONSUMER_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ],
    });
  });

  it("binds a sorted, unique, regular-file input set", () => {
    const inputs = runnerLedgerConsumerRegistryInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("docs/plan/adr/0020-p1-runner-ledger-consumer-contract.md");
    expect(inputs).toContain("docs/plan/p1/runner-ledger-consumer-entry-blocker-20260821.md");
    expect(inputs).toContain("scripts/generate-platform-runner-ledger-consumer-registry.ts");
    expect(inputs).toContain(RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH);
    for (const input of inputs) {
      const stat = lstatSync(resolve(repositoryRoot, input));
      expect(stat.isFile()).toBe(true);
      expect(stat.isSymbolicLink()).toBe(false);
    }
  });
});
