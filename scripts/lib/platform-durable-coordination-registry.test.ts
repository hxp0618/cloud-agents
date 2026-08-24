import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  assertDurableCoordinationRegistryCurrent,
  buildDurableCoordinationRegistry,
  durableCoordinationRegistryInputs,
  DurableCoordinationContractError,
  serializeDurableCoordinationRegistry,
  validateDurableCoordinationFixture,
  validateDurableCoordinationSource,
} from "./platform-durable-coordination-registry";

type JsonRecord = Record<string, unknown>;
type StateMachine = {
  id: string;
  initialState: string;
  states: string[];
  terminalStates: string[];
  transitions: Array<{ from: string; event: string; to: string }>;
};

const repositoryRoot = resolve(import.meta.dirname, "../..");
const temporaryRoots: string[] = [];

afterEach(() => {
  for (const root of temporaryRoots.splice(0)) rmSync(root, { force: true, recursive: true });
});

function temporaryContractRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "durable-coordination-registry-"));
  temporaryRoots.push(root);
  cpSync(resolve(repositoryRoot, "contracts"), resolve(root, "contracts"), { recursive: true });
  return root;
}

function readJson(path: string): JsonRecord {
  return JSON.parse(readFileSync(path, "utf8")) as JsonRecord;
}

function writeJson(path: string, value: unknown): void {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

function sourcePath(root: string): string {
  return resolve(
    root,
    "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v1.json",
  );
}

function sourceMachines(source: JsonRecord): StateMachine[] {
  return source.stateMachines as StateMachine[];
}

function expectCoordinationError(action: () => unknown, code: string): void {
  try {
    action();
    throw new Error(`Expected ${code}.`);
  } catch (error) {
    expect(error).toBeInstanceOf(DurableCoordinationContractError);
    expect((error as DurableCoordinationContractError).code).toBe(code);
  }
}

describe("durable coordination generated contract registry", () => {
  it("is byte-current, deterministic, and free of host or timestamp metadata", () => {
    expect(() => assertDurableCoordinationRegistryCurrent(repositoryRoot)).not.toThrow();
    const first = serializeDurableCoordinationRegistry(
      buildDurableCoordinationRegistry(repositoryRoot),
    );
    const second = serializeDurableCoordinationRegistry(
      buildDurableCoordinationRegistry(repositoryRoot),
    );
    expect(first).toBe(second);
    expect(first).not.toMatch(/"generatedAt"|generated_at|\/Users\//u);
  });

  it("binds exactly the approved profile and seven state-machine IDs", () => {
    const registry = buildDurableCoordinationRegistry(repositoryRoot) as {
      profiles: Array<{ profileDigest: string; spec: JsonRecord }>;
      stateMachines: StateMachine[];
      policies: JsonRecord;
      registryDigest: string;
    };
    expect(registry.profiles).toHaveLength(1);
    expect(registry.profiles[0]?.profileDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(registry.profiles[0]?.spec.profileId).toBe("managedAgentCreateProject/v1alpha1");
    expect((registry.profiles[0]?.spec.coordination as JsonRecord).externalSideEffect).toBe(
      "forbidden",
    );
    expect(registry.stateMachines.map((machine) => machine.id)).toEqual([
      "cleanup/v1",
      "finalizer/v1",
      "idempotency/v1",
      "operation_attempt/v1",
      "outbox/v1",
      "platform_operation/v1",
      "terminal_receipt/v1",
    ]);
    expect(registry.policies.operation).toEqual({
      cancelRule: "pending_before_attempt_only",
      generationRule: "positive_monotonic_per_operation_identity",
      identityFields: ["tenant_id", "operation_id", "operation_generation"],
      retryRule: "create_new_attempt_identity_only_after_proved_retry",
      stateMachineId: "platform_operation/v1",
      unknownRule: "reconciliation_required_no_direct_retry",
    });
    expect(registry.policies.operationAttempt).toMatchObject({
      identityFields: ["tenant_id", "operation_id", "operation_generation", "attempt_number"],
      stateMachineId: "operation_attempt/v1",
      terminalTransitionRule: "persist_immutable_terminal_receipt_in_same_transaction",
    });
    expect(registry.policies.terminalReceipt).toEqual({
      appendOnly: true,
      identityFields: [
        "tenant_id",
        "operation_id",
        "operation_generation",
        "attempt_number",
        "receipt_id",
      ],
      outcomes: ["canceled", "failed", "succeeded"],
      stateMachineId: "terminal_receipt/v1",
      unknownAttemptRule: "forbidden",
    });
    expect(registry.registryDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
  });

  it("rejects terminal outgoing transitions, nondeterminism, and unreachable states", () => {
    const root = temporaryContractRoot();
    const source = readJson(sourcePath(root));
    const idempotency = sourceMachines(source).find((machine) => machine.id === "idempotency/v1");
    if (!idempotency) throw new Error("test state machine missing");
    idempotency.transitions.unshift({ from: "failed", event: "restart", to: "pending" });
    expectCoordinationError(
      () => validateDurableCoordinationSource(root, source),
      "COORDINATION_STATE_MACHINE_INVALID",
    );

    const duplicateSource = readJson(sourcePath(root));
    const outbox = sourceMachines(duplicateSource).find((machine) => machine.id === "outbox/v1");
    if (!outbox) throw new Error("test state machine missing");
    outbox.transitions.push({
      from: "claimed",
      event: "delivery_failed_retryable",
      to: "dead_letter",
    });
    outbox.transitions.sort((left, right) =>
      `${left.from}\0${left.event}\0${left.to}`.localeCompare(
        `${right.from}\0${right.event}\0${right.to}`,
        "en",
      ),
    );
    expectCoordinationError(
      () => validateDurableCoordinationSource(root, duplicateSource),
      "COORDINATION_STATE_MACHINE_INVALID",
    );

    const unreachableSource = readJson(sourcePath(root));
    const receipts = sourceMachines(unreachableSource).find(
      (machine) => machine.id === "terminal_receipt/v1",
    );
    if (!receipts) throw new Error("test state machine missing");
    receipts.states.push("unreachable");
    receipts.states.sort();
    expectCoordinationError(
      () => validateDurableCoordinationSource(root, unreachableSource),
      "COORDINATION_STATE_MACHINE_INVALID",
    );
  });

  it("rejects profile drift from the checked-in OpenAPI operation authority", () => {
    const root = temporaryContractRoot();
    const profilePath = resolve(
      root,
      "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-v1alpha1.json",
    );
    const profile = readJson(profilePath);
    (profile.http as JsonRecord).method = "PUT";
    writeJson(profilePath, profile);
    expectCoordinationError(
      () => buildDurableCoordinationRegistry(root),
      "COORDINATION_PROFILE_BINDING_MISMATCH",
    );
  });

  it("rejects any mutation of the generated registry", () => {
    const generated = buildDurableCoordinationRegistry(repositoryRoot) as JsonRecord & {
      profiles: Array<{ spec: JsonRecord }>;
    };
    (generated.profiles[0]!.spec.idempotency as JsonRecord).replayTtlSeconds = 1;
    expect(validateDurableCoordinationFixture(generated, repositoryRoot)).toEqual({
      valid: false,
      errors: [
        {
          code: "COORDINATION_REGISTRY_DIGEST_MISMATCH",
          path: "/registryDigest",
        },
      ],
    });
  });

  it("binds every generator input and returns an owned registry graph", () => {
    const inputs = durableCoordinationRegistryInputs(repositoryRoot);
    expect(inputs).toEqual(inputs.toSorted());
    expect(new Set(inputs).size).toBe(inputs.length);
    expect(inputs).toContain("docs/plan/adr/0013-p1-durable-coordination-contract.md");
    expect(inputs).toContain("scripts/generate-platform-durable-coordination-registry.ts");
    expect(inputs).toContain(
      "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-v1alpha1.json",
    );

    const first = buildDurableCoordinationRegistry(repositoryRoot) as {
      stateMachines: StateMachine[];
    };
    const second = buildDurableCoordinationRegistry(repositoryRoot) as {
      stateMachines: StateMachine[];
    };
    first.stateMachines[0]!.states[0] = "mutated";
    expect(second.stateMachines[0]!.states[0]).not.toBe("mutated");
  });
});
