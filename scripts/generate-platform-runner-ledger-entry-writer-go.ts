import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent,
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH,
  RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION,
  RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH,
} from "./lib/platform-runner-ledger-entry-writer-registry";

export const RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go";

type Binding = {
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profileId: string;
  readonly profileDigest: string;
};

type Registry = {
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly entryAdmissionBinding?: Binding;
  readonly executionAdmissionBinding?: Binding;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly stateMachineId: string;
      readonly canonicalization: Record<string, string>;
      readonly identityBindings: Record<string, string>;
      readonly errorPrecedence: Record<string, string>;
      readonly transitionMatrix?: typeof RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX;
      readonly writerAction?: typeof RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION;
    };
  };
  readonly stateMachine: {
    readonly id: string;
    readonly initialState: string;
    readonly transitions: ReadonlyArray<{
      readonly from: string;
      readonly event: string;
      readonly to: string;
    }>;
  };
  readonly selector: Record<string, string>;
  readonly implementationBoundary: Record<string, string>;
};

const executionBindingKeys = [
  "entryAdmissionProfile",
  "consumedConsumerClaim",
  "currentEvidenceBoundary",
  "selectedEntry",
  "planClosure",
  "databaseSession",
  "ledgerPrefix",
  "catalogProjection",
  "advisoryLock",
  "executionPermit",
] as const;
const executionPrecedenceKeys = [
  "storedContradiction",
  "contextOrOperationalFailure",
  "recoveryRequired",
  "unsupportedTransition",
  "closeOrUnlockUnknown",
  "oneShotConsumption",
  "unknownOutcome",
] as const;
const executionBoundaryKeys = [
  "runnerConsumer",
  "entryAdmissionV1",
  "existingBrandNewWriter",
  "executionPermit",
  "entryWriter",
  "recoveryWriter",
  "databaseSession",
  "databaseTransaction",
  "beginMigration",
  "sqlExecution",
  "ledgerMutation",
  "evidenceMutation",
  "httpSurface",
  "p2Surface",
  "providerSideEffects",
  "productionDatabaseWrites",
  "deployment",
  "publication",
  "gateStatus",
] as const;
const writerBindingKeys = [
  "executionAdmissionProfile",
  "consumedExecutionPermit",
  "currentEvidenceBoundary",
  "selectedEntry",
  "planClosure",
  "statementChain",
  "databaseSession",
  "databaseTransaction",
  "ledgerPrefix",
  "catalogProjection",
  "commitOutcome",
] as const;
const writerPrecedenceKeys = [
  "storedContradiction",
  "contextOrOperationalFailure",
  "recoveryRequired",
  "preMutationFailure",
  "unknownOutcome",
  "cleanupUnknown",
  "oneShotConsumption",
] as const;
const writerBoundaryKeys = [
  "productionConsumer",
  "existingBrandNewWriter",
  "successWriter",
  "bundleLoop",
  "retryWriter",
  "abortWriter",
  "reconcileWriter",
  "failureWriter",
  "databaseSession",
  "databaseTransaction",
  "sqlExecution",
  "ledgerMutation",
  "evidenceMutation",
  "entryBoundary",
  "httpSurface",
  "p2Surface",
  "providerSideEffects",
  "productionDatabaseWrites",
  "deployment",
  "publication",
  "gateStatus",
] as const;

function goString(value: string): string {
  return JSON.stringify(value);
}

function requireExact(value: unknown, expected: unknown, name: string): void {
  if (JSON.stringify(value) !== JSON.stringify(expected)) {
    throw new Error(`Runner ledger entry writer generated Go boundary drifted at ${name}.`);
  }
}

function values(record: Record<string, string>, keys: ReadonlyArray<string>): string {
  const actualKeys = Object.keys(record).toSorted();
  requireExact(actualKeys, [...keys].toSorted(), "closed field set");
  return keys.map((key) => `\t\t${goString(record[key]!)},`).join("\n");
}

function transitions(registry: Registry): string {
  return registry.stateMachine.transitions
    .map(
      (transition) =>
        `\t{from: ${goString(transition.from)}, event: ${goString(transition.event)}, to: ${goString(transition.to)}},`,
    )
    .join("\n");
}

function bindingConstants(prefix: string, binding: Binding): string {
  const entries = [
    [`${prefix}RegistryID`, binding.registryId],
    [`${prefix}RegistryDigest`, binding.registryDigest],
    [`${prefix}StateMachineDigest`, binding.stateMachineDigest],
    [`${prefix}PolicyDigest`, binding.policyDigest],
    [`${prefix}ProfileID`, binding.profileId],
    [`${prefix}ProfileDigest`, binding.profileDigest],
  ] as const;
  const maximum = Math.max(...entries.map(([name]) => name.length));
  return entries
    .map(([name, value]) => `\t${name}${" ".repeat(maximum - name.length + 1)}= ${goString(value)}`)
    .join("\n");
}

function profileLiteral(
  typeName: string,
  registry: Registry,
  bindingKeys: ReadonlyArray<string>,
  precedenceKeys: ReadonlyArray<string>,
  boundaryKeys: ReadonlyArray<string>,
): string {
  const spec = registry.profile.spec;
  return `${typeName}{
\tprofileID:                 ${goString(spec.profileId)},
\tprofileDigest:             ${goString(registry.profile.profileDigest)},
\tstateMachineID:            ${goString(spec.stateMachineId)},
\tcanonicalizationProfile:   ${goString(spec.canonicalization.profile!)},
\tcanonicalizationAlgorithm: ${goString(spec.canonicalization.algorithm!)},
\tdigestAlgorithm:           ${goString(spec.canonicalization.digest!)},
\tcomparison:                ${goString(spec.canonicalization.comparison!)},
\tidentityBindings: [${bindingKeys.length}]string{
${values(spec.identityBindings, bindingKeys)}
\t},
\terrorPrecedence: [${precedenceKeys.length}]string{
${values(spec.errorPrecedence, precedenceKeys)}
\t},
\timplementationBoundary: [${boundaryKeys.length}]string{
${values(registry.implementationBoundary, boundaryKeys)}
\t},
}`;
}

export function serializeRunnerLedgerEntryWriterGo(execution: Registry, writer: Registry): string {
  requireExact(
    execution.profile.spec.profileId,
    "runner-ledger-entry-execution-admission/v1",
    "execution profileId",
  );
  requireExact(
    execution.profile.spec.stateMachineId,
    execution.profile.spec.profileId,
    "execution stateMachineId",
  );
  requireExact(
    execution.stateMachine.id,
    execution.profile.spec.profileId,
    "execution stateMachine.id",
  );
  requireExact(execution.stateMachine.initialState, "unclassified", "execution initialState");
  requireExact(
    execution.profile.spec.transitionMatrix,
    RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX,
    "execution matrix",
  );
  requireExact(
    writer.profile.spec.profileId,
    "runner-ledger-entry-success-writer/v1",
    "writer profileId",
  );
  requireExact(
    writer.profile.spec.stateMachineId,
    writer.profile.spec.profileId,
    "writer stateMachineId",
  );
  requireExact(writer.stateMachine.id, writer.profile.spec.profileId, "writer stateMachine.id");
  requireExact(writer.stateMachine.initialState, "unclassified", "writer initialState");
  requireExact(
    writer.profile.spec.writerAction,
    RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_ACTION,
    "writer action",
  );
  if (!execution.entryAdmissionBinding || !writer.executionAdmissionBinding) {
    throw new Error("Runner ledger entry writer generated Go binding is absent.");
  }
  const cases = Object.entries(RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_TRANSITION_MATRIX)
    .flatMap(([disposition, pairs]) =>
      pairs.map(
        (pair) =>
          `\tcase ${goString(`${disposition}\u0000${pair.state}\u0000${pair.action}`)}:\n\t\treturn runnerLedgerEntryExecutionAdmissionAction(${goString(pair.executionAction)}), true`,
      ),
    )
    .join("\n");

  return `// Code generated by scripts/generate-platform-runner-ledger-entry-writer-go.ts; DO NOT EDIT.

package migration

const (
\trunnerLedgerEntryExecutionAdmissionRegistryDigest     = ${goString(execution.registryDigest)}
\trunnerLedgerEntryExecutionAdmissionStateMachineDigest = ${goString(execution.stateMachineDigest)}
\trunnerLedgerEntryExecutionAdmissionPolicyDigest       = ${goString(execution.policyDigest)}
\trunnerLedgerEntryExecutionAdmissionProfileDigest      = ${goString(execution.profile.profileDigest)}

${bindingConstants("runnerLedgerEntryExecutionAdmissionBoundEntryAdmission", execution.entryAdmissionBinding)}

\trunnerLedgerEntrySuccessWriterRegistryDigest     = ${goString(writer.registryDigest)}
\trunnerLedgerEntrySuccessWriterStateMachineDigest = ${goString(writer.stateMachineDigest)}
\trunnerLedgerEntrySuccessWriterPolicyDigest       = ${goString(writer.policyDigest)}
\trunnerLedgerEntrySuccessWriterProfileDigest      = ${goString(writer.profile.profileDigest)}

${bindingConstants("runnerLedgerEntrySuccessWriterBoundExecutionAdmission", writer.executionAdmissionBinding)}
)

var generatedRunnerLedgerEntryExecutionAdmissionProfile = ${profileLiteral(
    "runnerLedgerEntryExecutionAdmissionProfile",
    execution,
    executionBindingKeys,
    executionPrecedenceKeys,
    executionBoundaryKeys,
  )}

var generatedRunnerLedgerEntrySuccessWriterProfile = ${profileLiteral(
    "runnerLedgerEntrySuccessWriterProfile",
    writer,
    writerBindingKeys,
    writerPrecedenceKeys,
    writerBoundaryKeys,
  )}

var generatedRunnerLedgerEntryExecutionAdmissionTransitions = [...]runnerLedgerEntryWriterTransition{
${transitions(execution)}
}

var generatedRunnerLedgerEntrySuccessWriterTransitions = [...]runnerLedgerEntryWriterTransition{
${transitions(writer)}
}

const generatedRunnerLedgerEntryExecutionAdmissionPairCount = 4

func generatedRunnerLedgerEntryExecutionAdmissionAction(disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) (runnerLedgerEntryExecutionAdmissionAction, bool) {
\tswitch string(disposition) + "\\x00" + string(state) + "\\x00" + string(action) {
${cases}
\tdefault:
\t\treturn "", false
\t}
}

func generatedRunnerLedgerEntrySuccessWriterAction(executionAction runnerLedgerEntryExecutionAdmissionAction) (runnerLedgerEntrySuccessWriterAction, bool) {
\tif executionAction == runnerLedgerEntryExecutionAdmissionPrepare {
\t\treturn runnerLedgerEntrySuccessWriterExecute, true
\t}
\treturn "", false
}
`;
}

function readRegistries(root: string): readonly [Registry, Registry] {
  assertRunnerLedgerEntryExecutionAdmissionRegistryCurrent(root);
  assertRunnerLedgerEntrySuccessWriterRegistryCurrent(root);
  return [
    JSON.parse(
      readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_EXECUTION_ADMISSION_OUTPUT_PATH), "utf8"),
    ) as Registry,
    JSON.parse(
      readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_SUCCESS_WRITER_OUTPUT_PATH), "utf8"),
    ) as Registry,
  ];
}

export function expectedRunnerLedgerEntryWriterGo(root: string): string {
  return serializeRunnerLedgerEntryWriterGo(...readRegistries(root));
}

export function assertRunnerLedgerEntryWriterGoCurrent(root: string): void {
  const expected = expectedRunnerLedgerEntryWriterGo(root);
  const actual = readFileSync(resolve(root, RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(
      `${RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH),
    expectedRunnerLedgerEntryWriterGo(root),
  );
  process.stdout.write(
    `platform-runner-ledger-entry-writer-go: wrote ${RUNNER_LEDGER_ENTRY_WRITER_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerEntryWriterGoCurrent(root);
  process.stdout.write("platform-runner-ledger-entry-writer-go: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-entry-writer-go.ts --write|--check",
  );
}
