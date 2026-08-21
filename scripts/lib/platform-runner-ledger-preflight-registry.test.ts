import { cpSync, lstatSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertRunnerLedgerPreflightRegistryCurrent,
  buildRunnerLedgerPreflightRegistry,
  runnerLedgerPreflightRegistryInputs,
  RunnerLedgerPreflightContractError,
  RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH,
  serializeRunnerLedgerPreflightRegistry,
  validateRunnerLedgerPreflightFixture,
  validateRunnerLedgerPreflightSource,
} from "./platform-runner-ledger-preflight-registry";

type JsonRecord = Record<string, unknown>;

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryContractRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "runner-ledger-preflight-registry-"));
  temporaryRoots.push(root);
  cpSync(resolve(repositoryRoot, "contracts"), resolve(root, "contracts"), { recursive: true });
  return root;
}

function readSource(root: string): JsonRecord {
  return JSON.parse(
    readFileSync(resolve(root, RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH), "utf8"),
  ) as JsonRecord;
}

function writeSource(root: string, value: unknown): void {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_PREFLIGHT_SOURCE_PATH),
    `${JSON.stringify(value, null, 2)}\n`,
  );
}

function expectContractError(action: () => unknown, code: string): void {
  try {
    action();
    throw new Error(`Expected ${code}.`);
  } catch (error) {
    expect(error).toBeInstanceOf(RunnerLedgerPreflightContractError);
    expect((error as RunnerLedgerPreflightContractError).code).toBe(code);
  }
}

describe("runner ledger preflight generated contract registry", () => {
  it("is byte-current, deterministic, and host independent", () => {
    expect(() => assertRunnerLedgerPreflightRegistryCurrent(repositoryRoot)).not.toThrow();
    const first = serializeRunnerLedgerPreflightRegistry(
      buildRunnerLedgerPreflightRegistry(repositoryRoot),
    );
    const second = serializeRunnerLedgerPreflightRegistry(
      buildRunnerLedgerPreflightRegistry(repositoryRoot),
    );
    expect(first).toBe(second);
    expect(first).not.toMatch(/generatedAt|generated_at|\/Users\//u);
  });

  it("binds exactly five closed dispositions and no runtime authority", () => {
    const registry = buildRunnerLedgerPreflightRegistry(repositoryRoot) as {
      registryDigest: string;
      stateMachineDigest: string;
      policyDigest: string;
      profile: { profileDigest: string; spec: JsonRecord };
      stateMachine: { terminalStates: string[] };
      implementationBoundary: JsonRecord;
    };
    expect(registry.stateMachine.terminalStates).toEqual([
      "complete_return_success",
      "empty_brand_new",
      "partial_next_entry",
      "partial_retry_or_recovery",
      "unknown_or_failed",
    ]);
    expect(registry.profile.spec.profileId).toBe("runner-ledger-preflight/v1");
    expect(registry.implementationBoundary).toEqual({
      runnerConsumer: "not_implemented",
      databaseSession: "none",
      databaseTransaction: "forbidden",
      ledgerMutation: "forbidden",
      evidenceMutation: "forbidden",
      httpSurface: "not_implemented",
      p2Surface: "not_implemented",
      providerSideEffects: "forbidden",
      productionDatabaseWrites: "not_authorized",
      deployment: "not_authorized",
      publication: "not_authorized",
      gateStatus: "all_gates_open",
    });
    for (const digest of [
      registry.registryDigest,
      registry.stateMachineDigest,
      registry.policyDigest,
      registry.profile.profileDigest,
    ]) {
      expect(digest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    }
    expect(
      new Set([
        registry.registryDigest,
        registry.stateMachineDigest,
        registry.policyDigest,
        registry.profile.profileDigest,
      ]).size,
    ).toBe(4);
  });

  it("rejects selector, state-machine, and implementation-boundary drift", () => {
    const selectorRoot = temporaryContractRoot();
    const selector = readSource(selectorRoot);
    (selector.selector as JsonRecord).callerProvidedProfile = "allowed";
    writeSource(selectorRoot, selector);
    expectContractError(
      () => validateRunnerLedgerPreflightSource(selectorRoot, readSource(selectorRoot) as never),
      "RUNNER_LEDGER_PREFLIGHT_BINDING_MISMATCH",
    );

    const stateRoot = temporaryContractRoot();
    const state = readSource(stateRoot);
    const machine = state.stateMachine as JsonRecord;
    (machine.transitions as unknown[]).pop();
    writeSource(stateRoot, state);
    expectContractError(
      () => validateRunnerLedgerPreflightSource(stateRoot, readSource(stateRoot) as never),
      "RUNNER_LEDGER_PREFLIGHT_STATE_MACHINE_INVALID",
    );

    const boundaryRoot = temporaryContractRoot();
    const boundary = readSource(boundaryRoot);
    (boundary.implementationBoundary as JsonRecord).runnerConsumer = "implemented";
    writeSource(boundaryRoot, boundary);
    expectContractError(
      () => validateRunnerLedgerPreflightSource(boundaryRoot, readSource(boundaryRoot) as never),
      "RUNNER_LEDGER_PREFLIGHT_BOUNDARY_MISMATCH",
    );
  });

  it("rejects every mutation of a generated registry", () => {
    const generated = buildRunnerLedgerPreflightRegistry(repositoryRoot) as JsonRecord & {
      selector: JsonRecord;
    };
    generated.selector.guessedMigrationIdentity = "allowed";
    expect(validateRunnerLedgerPreflightFixture(generated, repositoryRoot)).toEqual({
      valid: false,
      errors: [
        {
          code: "RUNNER_LEDGER_PREFLIGHT_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ],
    });
  });

  it("binds a sorted, unique, regular-file input set", () => {
    const inputs = runnerLedgerPreflightRegistryInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("docs/plan/adr/0019-p1-runner-ledger-preflight-contract.md");
    expect(inputs).toContain("docs/plan/p1/migration-ledger-preflight-entry-blocker-20260821.md");
    expect(inputs).toContain("scripts/generate-platform-runner-ledger-preflight-registry.ts");
    for (const input of inputs) {
      const stat = lstatSync(resolve(repositoryRoot, input));
      expect(stat.isFile()).toBe(true);
      expect(stat.isSymbolicLink()).toBe(false);
    }
  });
});
