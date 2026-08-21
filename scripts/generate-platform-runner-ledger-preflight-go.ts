import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerPreflightRegistryCurrent,
  RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH,
} from "./lib/platform-runner-ledger-preflight-registry";

export const RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go";

type GeneratedRegistry = {
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly stateMachineId: string;
      readonly canonicalization: {
        readonly profile: string;
        readonly algorithm: string;
        readonly digest: string;
        readonly comparison: string;
      };
      readonly identityBindings: Record<string, string>;
      readonly errorPrecedence: Record<string, string>;
      readonly recoveryDispositionMatrix: Record<
        string,
        ReadonlyArray<{ readonly state: string; readonly action: string }>
      >;
    };
  };
  readonly stateMachine: {
    readonly id: string;
    readonly initialState: string;
    readonly states: ReadonlyArray<string>;
    readonly terminalStates: ReadonlyArray<string>;
    readonly transitions: ReadonlyArray<{
      readonly from: string;
      readonly event: string;
      readonly to: string;
    }>;
  };
  readonly selector: Record<string, string>;
  readonly implementationBoundary: Record<string, string>;
};

const terminalStates = [
  "complete_return_success",
  "empty_brand_new",
  "partial_next_entry",
  "partial_retry_or_recovery",
  "unknown_or_failed",
] as const;

const recoveryDispositionMatrix = {
  complete_return_success: [{ state: "completed", action: "return_success" }],
  empty_brand_new: [
    { state: "brand_new", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_next_attempt" },
  ],
  partial_next_entry: [
    { state: "brand_new_inherited", action: "begin_first_attempt_next_entry" },
    { state: "terminal", action: "begin_first_attempt_next_entry" },
  ],
  partial_retry_or_recovery: [
    { state: "brand_new_inherited", action: "begin_first_attempt" },
    { state: "brand_new_inherited", action: "begin_next_attempt" },
    { state: "dangling_statement_intent", action: "append_aborted_retryable" },
    { state: "dangling_statement_intent", action: "append_aborted_terminal" },
    { state: "dangling_intermediate", action: "append_aborted_retryable" },
    { state: "dangling_intermediate", action: "append_aborted_terminal" },
    { state: "dangling_commit_intent", action: "reconcile_commit" },
    { state: "ambiguous_unresolved", action: "reconcile_commit" },
    { state: "terminal", action: "begin_next_attempt" },
    { state: "terminal", action: "return_failure" },
    { state: "divergent", action: "return_failure" },
  ],
} as const;

function goString(value: string): string {
  return JSON.stringify(value);
}

function requireExact(value: unknown, expected: unknown, name: string): void {
  if (JSON.stringify(value) !== JSON.stringify(expected)) {
    throw new Error(`Runner ledger preflight generated Go boundary drifted at ${name}.`);
  }
}

export function serializeRunnerLedgerPreflightGo(registry: GeneratedRegistry): string {
  requireExact(registry.profile.spec.profileId, "runner-ledger-preflight/v1", "profileId");
  requireExact(
    registry.profile.spec.stateMachineId,
    "runner-ledger-preflight/v1",
    "stateMachineId",
  );
  requireExact(registry.stateMachine.id, "runner-ledger-preflight/v1", "stateMachine.id");
  requireExact(registry.stateMachine.initialState, "unclassified", "stateMachine.initialState");
  requireExact(registry.stateMachine.terminalStates, terminalStates, "stateMachine.terminalStates");
  requireExact(
    registry.profile.spec.recoveryDispositionMatrix,
    recoveryDispositionMatrix,
    "profile.recoveryDispositionMatrix",
  );
  requireExact(
    registry.selector,
    {
      mode: "generated_registry_only",
      profileSelection: "exact_profile_id_and_digest",
      callerProvidedProfile: "forbidden",
      guessedMigrationIdentity: "forbidden",
      lossyIdentityMapping: "forbidden",
    },
    "selector",
  );
  requireExact(
    registry.implementationBoundary,
    {
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
    },
    "implementationBoundary",
  );

  const canonicalization = registry.profile.spec.canonicalization;
  const identity = registry.profile.spec.identityBindings;
  const precedence = registry.profile.spec.errorPrecedence;
  const boundary = registry.implementationBoundary;
  const transitions = registry.stateMachine.transitions
    .map(
      (transition) =>
        `\t{from: ${goString(transition.from)}, event: ${goString(transition.event)}, to: runnerLedgerPreflightDisposition(${goString(transition.to)})},`,
    )
    .join("\n");
  const recoveryPairs = Object.entries(registry.profile.spec.recoveryDispositionMatrix)
    .flatMap(([disposition, pairs]) =>
      pairs.map((pair) => goString(`${disposition}\u0000${pair.state}\u0000${pair.action}`)),
    )
    .join(",\n\t\t");
  const recoveryPairCount = Object.values(recoveryDispositionMatrix).reduce(
    (count, pairs) => count + pairs.length,
    0,
  );

  return `// Code generated by scripts/generate-platform-runner-ledger-preflight-go.ts; DO NOT EDIT.

package migration

const (
\trunnerLedgerPreflightRegistryDigest     = ${goString(registry.registryDigest)}
\trunnerLedgerPreflightStateMachineDigest = ${goString(registry.stateMachineDigest)}
\trunnerLedgerPreflightPolicyDigest       = ${goString(registry.policyDigest)}
)

var generatedRunnerLedgerPreflightProfile = runnerLedgerPreflightProfile{
\tprofileID:                         ${goString(registry.profile.spec.profileId)},
\tprofileDigest:                     ${goString(registry.profile.profileDigest)},
\tstateMachineID:                    ${goString(registry.profile.spec.stateMachineId)},
\tcanonicalizationProfile:           ${goString(canonicalization.profile)},
\tcanonicalizationAlgorithm:         ${goString(canonicalization.algorithm)},
\tdigestAlgorithm:                   ${goString(canonicalization.digest)},
\tcomparison:                        ${goString(canonicalization.comparison)},
\tschemaBundleBinding:               ${goString(identity.schemaBundleDigest)},
\texecutionLineageBinding:           ${goString(identity.executionLineageDigest)},
\torderedMigrationPrefixBinding:     ${goString(identity.orderedMigrationPrefix)},
\tlastAppliedCatalogBinding:         ${goString(identity.lastAppliedCatalogContractDigest)},
\tnextEntryBinding:                  ${goString(identity.nextEntryIdentity)},
\tevidenceRecoveryBinding:           ${goString(identity.evidenceRecoveryDisposition)},
\tstoredContradictionPrecedence:     ${goString(precedence.storedContradiction)},
\tcontextOrOperationalPrecedence:    ${goString(precedence.contextOrOperationalFailure)},
\trecoveryRequiredPrecedence:        ${goString(precedence.recoveryRequired)},
\tclassifiedWithoutBinderPrecedence: ${goString(precedence.classifiedWithoutTypedBinder)},
\tunknownOutcomePrecedence:          ${goString(precedence.unknownOutcome)},
\trunnerConsumerBoundary:            ${goString(boundary.runnerConsumer)},
\tdatabaseSessionBoundary:           ${goString(boundary.databaseSession)},
\tdatabaseTransactionBoundary:       ${goString(boundary.databaseTransaction)},
\tledgerMutationBoundary:            ${goString(boundary.ledgerMutation)},
\tevidenceMutationBoundary:          ${goString(boundary.evidenceMutation)},
\thttpSurfaceBoundary:               ${goString(boundary.httpSurface)},
\tp2SurfaceBoundary:                 ${goString(boundary.p2Surface)},
\tproviderSideEffectsBoundary:       ${goString(boundary.providerSideEffects)},
\tproductionDatabaseWritesBoundary:  ${goString(boundary.productionDatabaseWrites)},
\tdeploymentBoundary:                ${goString(boundary.deployment)},
\tpublicationBoundary:               ${goString(boundary.publication)},
\tgateStatusBoundary:                ${goString(boundary.gateStatus)},
}

var generatedRunnerLedgerPreflightTransitions = [...]runnerLedgerPreflightTransition{
${transitions}
}

const generatedRunnerLedgerPreflightRecoveryPairCount = ${recoveryPairCount}

func generatedRunnerLedgerPreflightRecoveryPairAllowed(disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) bool {
	switch string(disposition) + "\\x00" + string(state) + "\\x00" + string(action) {
	case ${recoveryPairs}:
		return true
	default:
		return false
	}
}
`;
}

function readRegistry(root: string): GeneratedRegistry {
  assertRunnerLedgerPreflightRegistryCurrent(root);
  return JSON.parse(
    readFileSync(resolve(root, RUNNER_LEDGER_PREFLIGHT_OUTPUT_PATH), "utf8"),
  ) as GeneratedRegistry;
}

export function expectedRunnerLedgerPreflightGo(root: string): string {
  return serializeRunnerLedgerPreflightGo(readRegistry(root));
}

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeFileSync(output, expectedRunnerLedgerPreflightGo(root));
  process.stdout.write(
    `platform-runner-ledger-preflight-go: wrote ${RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  const expected = expectedRunnerLedgerPreflightGo(root);
  if (readFileSync(output, "utf8") !== expected) {
    throw new Error(
      `${RUNNER_LEDGER_PREFLIGHT_GO_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
  process.stdout.write("platform-runner-ledger-preflight-go: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-preflight-go.ts --write|--check",
  );
}
