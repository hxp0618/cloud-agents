import { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

import { afterEach, describe, expect, it } from "vitest";

import {
  buildDurableCoordinationRegistry,
  DurableCoordinationContractError,
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

  it("does not treat an unknown coordination annotation as an idempotency owner", () => {
    const root = temporaryContractRoot();
    const openapiPath = resolve(root, "contracts/managed-host/v1alpha1/openapi.json");
    const openapi = readJson(openapiPath);
    const paths = openapi.paths as Record<string, JsonRecord>;
    const operation = paths[
      "/v1/tenants/{tenantId}/projects/{projectId}/sandbox-sessions/{sandboxId}/access-grants"
    ]?.post as JsonRecord;
    operation["x-cloud-agents-coordination"] = "durable-access-grant-typo";
    writeJson(openapiPath, openapi);
    expectCoordinationError(
      () => buildDurableCoordinationRegistry(root),
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
    );
  });
});
