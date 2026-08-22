import { readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

import {
  assertRunnerLedgerRecoveryRegistriesCurrent,
  buildRunnerLedgerRecoveryRegistries,
  RUNNER_LEDGER_RECOVERY_FAMILIES,
} from "./lib/platform-runner-ledger-recovery-registry";

export const RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_recovery_profile_generated.go";

type Binding = {
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profileId: string;
  readonly profileDigest: string;
};

type PairBinding = {
  readonly disposition: string;
  readonly state: string;
  readonly action: string;
  readonly consumerAction: string;
  readonly profileAction: string;
};

type Registry = {
  readonly family: string;
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly predecessorBinding: Binding;
  readonly historicalBindings: ReadonlyArray<Binding>;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly stateMachineId: string;
      readonly action: string;
      readonly canonicalization: Record<string, string>;
      readonly identityBindings: Record<string, string>;
      readonly errorPrecedence: Record<string, string>;
      readonly pairBindings: ReadonlyArray<PairBinding>;
      readonly permitFromProfileId: string | null;
    };
  };
  readonly stateMachine: {
    readonly transitions: ReadonlyArray<{
      readonly from: string;
      readonly event: string;
      readonly to: string;
    }>;
  };
  readonly implementationBoundary: Record<string, string>;
};

const identityKeys = [
  "predecessorProfile",
  "consumedAuthority",
  "currentEvidenceBoundary",
  "generationCursor",
  "ledgerPrefix",
  "catalogProjection",
  "executionPolicy",
  "actionReceipt",
  "oneShotAuthority",
  "crossProfileRejection",
] as const;
const precedenceKeys = [
  "storedContradiction",
  "contextOrOperationalFailure",
  "recoveryRequired",
  "unsupportedTransition",
  "mutationOutcomeUnknown",
  "cleanupUnknown",
  "oneShotConsumption",
] as const;
const boundaryKeys = [
  "sliceA",
  "productionConsumer",
  "claim",
  "databaseSession",
  "databaseTransaction",
  "sqlExecution",
  "ledgerMutation",
  "evidenceMutation",
  "lineageMutation",
  "writerAction",
  "caller",
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
    throw new Error(`Runner ledger recovery generated Go boundary drifted at ${name}.`);
  }
}

function stringArray(record: Record<string, string>, keys: ReadonlyArray<string>): string {
  requireExact(Object.keys(record).toSorted(), [...keys].toSorted(), "closed field set");
  return keys.map((key) => `\t\t${goString(record[key]!)},`).join("\n");
}

function bindingLiteral(binding: Binding): string {
  return `runnerLedgerRecoveryRegistryBinding{
\t\tregistryID:         ${goString(binding.registryId)},
\t\tregistryDigest:     ${goString(binding.registryDigest)},
\t\tstateMachineDigest: ${goString(binding.stateMachineDigest)},
\t\tpolicyDigest:       ${goString(binding.policyDigest)},
\t\tprofileID:          ${goString(binding.profileId)},
\t\tprofileDigest:      ${goString(binding.profileDigest)},
\t}`;
}

function pairLiteral(pair: PairBinding): string {
  return `{disposition: runnerLedgerPreflightDisposition(${goString(pair.disposition)}), state: RecoveryState(${goString(pair.state)}), action: RecoveryAction(${goString(pair.action)}), consumerAction: ${goString(pair.consumerAction)}, profileAction: runnerLedgerRecoveryAction(${goString(pair.profileAction)})}`;
}

function profileLiteral(registry: Registry): string {
  const profile = registry.profile.spec;
  if (profile.pairBindings.length > 12) throw new Error("Recovery profile has too many pairs.");
  if (registry.stateMachine.transitions.length > 25) {
    throw new Error("Recovery profile has too many transitions.");
  }
  if (registry.historicalBindings.length > 5) {
    throw new Error("Recovery profile has too many historical bindings.");
  }
  const permit = profile.permitFromProfileId ?? "";
  return `runnerLedgerRecoveryProfile{
\tfamily:                    ${goString(registry.family)},
\taction:                    runnerLedgerRecoveryAction(${goString(profile.action)}),
\tregistryID:                ${goString(registry.registryId)},
\tregistryDigest:            ${goString(registry.registryDigest)},
\tprofileID:                 ${goString(profile.profileId)},
\tprofileDigest:             ${goString(registry.profile.profileDigest)},
\tstateMachineID:            ${goString(profile.stateMachineId)},
\tstateMachineDigest:        ${goString(registry.stateMachineDigest)},
\tpolicyDigest:              ${goString(registry.policyDigest)},
\tcanonicalizationProfile:   ${goString(profile.canonicalization.profile!)},
\tcanonicalizationAlgorithm: ${goString(profile.canonicalization.algorithm!)},
\tdigestAlgorithm:           ${goString(profile.canonicalization.digest!)},
\tcomparison:                ${goString(profile.canonicalization.comparison!)},
\tpredecessor: ${bindingLiteral(registry.predecessorBinding)},
\tpermitFromProfileID: ${goString(permit)},
\tidentityBindings: [10]string{
${stringArray(profile.identityBindings, identityKeys)}
\t},
\terrorPrecedence: [7]string{
${stringArray(profile.errorPrecedence, precedenceKeys)}
\t},
\timplementationBoundary: [18]string{
${stringArray(registry.implementationBoundary, boundaryKeys)}
\t},
\tpairCount: ${profile.pairBindings.length},
\tpairs: [12]runnerLedgerRecoveryPair{
${profile.pairBindings.map((pair) => `\t\t${pairLiteral(pair)},`).join("\n")}
\t},
\ttransitionCount: ${registry.stateMachine.transitions.length},
\ttransitions: [25]runnerLedgerRecoveryTransition{
${registry.stateMachine.transitions
  .map(
    (item) =>
      `\t\t{from: ${goString(item.from)}, event: ${goString(item.event)}, to: ${goString(item.to)}},`,
  )
  .join("\n")}
\t},
\thistoricalBindingCount: ${registry.historicalBindings.length},
\thistoricalBindings: [5]runnerLedgerRecoveryRegistryBinding{
${registry.historicalBindings.map((item) => `\t\t${bindingLiteral(item)},`).join("\n")}
\t},
}`;
}

export function serializeRunnerLedgerRecoveryGo(registries: ReadonlyArray<Registry>): string {
  requireExact(
    registries.map((item) => item.family),
    RUNNER_LEDGER_RECOVERY_FAMILIES,
    "profile family order",
  );
  const common = registries[0];
  if (common === undefined) throw new Error("Recovery admission registry is missing.");
  return formatGo(`// Code generated by scripts/generate-platform-runner-ledger-recovery-go.ts; DO NOT EDIT.

package migration

var generatedRunnerLedgerRecoveryProfiles = [8]runnerLedgerRecoveryProfile{
${registries.map((item) => `\t${profileLiteral(item)},`).join("\n")}
}

func generatedRunnerLedgerRecoveryAdmissionAction(disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) (runnerLedgerRecoveryAction, bool) {
\tswitch string(disposition) + "\\x00" + string(state) + "\\x00" + string(action) {
${common.profile.spec.pairBindings
  .map(
    (item) =>
      `\tcase ${goString(`${item.disposition}\u0000${item.state}\u0000${item.action}`)}:\n\t\treturn runnerLedgerRecoveryAction(${goString(item.profileAction)}), true`,
  )
  .join("\n")}
\tdefault:
\t\treturn "", false
\t}
}

func generatedRunnerLedgerRecoveryProfileAllows(profileID string, disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) bool {
\tfor i := range generatedRunnerLedgerRecoveryProfiles {
\t\tprofile := &generatedRunnerLedgerRecoveryProfiles[i]
\t\tif profile.profileID != profileID {
\t\t\tcontinue
\t\t}
\t\tfor j := uint8(0); j < profile.pairCount; j++ {
\t\t\tpair := profile.pairs[j]
\t\t\tif pair.disposition == disposition && pair.state == state && pair.action == action {
\t\t\t\treturn true
\t\t\t}
\t\t}
\t\treturn false
\t}
\treturn false
}

func generatedRunnerLedgerRecoverySuccessWriterAction(admissionAction runnerLedgerRecoveryAction) (runnerLedgerRecoveryAction, bool) {
\tif admissionAction == runnerLedgerRecoveryAction("prepare_recovery_execution") {
\t\treturn runnerLedgerRecoveryAction("execute_one_recovery_attempt"), true
\t}
\treturn "", false
}
`);
}

function formatGo(source: string): string {
  const result = spawnSync("gofmt", [], { input: source, encoding: "utf8" });
  if (result.status !== 0 || result.signal !== null) {
    throw new Error(`gofmt failed: ${result.stderr || result.signal || result.status}`);
  }
  return result.stdout;
}

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];
assertRunnerLedgerRecoveryRegistriesCurrent(root);
const generated = serializeRunnerLedgerRecoveryGo(
  buildRunnerLedgerRecoveryRegistries(root) as ReadonlyArray<Registry>,
);
const output = resolve(root, RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH);

if (mode === "--write") {
  writeFileSync(output, generated);
  process.stdout.write(
    `platform-runner-ledger-recovery-go: wrote ${RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  if (readFileSync(output, "utf8") !== generated) {
    throw new Error(
      `${RUNNER_LEDGER_RECOVERY_GO_OUTPUT_PATH} is stale; run the generator with --write.`,
    );
  }
  process.stdout.write("platform-runner-ledger-recovery-go: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-recovery-go.ts --write|--check",
  );
}
