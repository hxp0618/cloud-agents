import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  assertRunnerLedgerConsumerRegistryCurrent,
  RUNNER_LEDGER_CONSUMER_OUTPUT_PATH,
  RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX,
} from "./lib/platform-runner-ledger-consumer-registry";

export const RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH =
  "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go";

type GeneratedRegistry = {
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly preflightBinding: {
    readonly registryId: string;
    readonly registryDigest: string;
    readonly stateMachineDigest: string;
    readonly policyDigest: string;
    readonly profileId: string;
    readonly profileDigest: string;
  };
  readonly profile: {
    readonly profileDigest: string;
    readonly spec: {
      readonly profileId: string;
      readonly stateMachineId: string;
      readonly canonicalization: Record<string, string>;
      readonly identityBindings: Record<string, string>;
      readonly errorPrecedence: Record<string, string>;
      readonly transitionMatrix: typeof RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX;
    };
  };
  readonly stateMachine: {
    readonly id: string;
    readonly initialState: string;
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
  "complete_return_success_noop",
  "entry_not_implemented",
  "recovery_not_implemented",
  "unknown_not_implemented",
] as const;

function goString(value: string): string {
  return JSON.stringify(value);
}

function requireExact(value: unknown, expected: unknown, name: string): void {
  if (JSON.stringify(value) !== JSON.stringify(expected)) {
    throw new Error(`Runner ledger consumer generated Go boundary drifted at ${name}.`);
  }
}

function goAssignments(
  entries: ReadonlyArray<readonly [string, string]>,
  separator: "=" | ":",
): string {
  const maximum = Math.max(...entries.map(([name]) => name.length));
  return entries
    .map(([name, value]) => {
      const padding = " ".repeat(maximum - name.length + 1);
      return separator === "=" ? `\t${name}${padding}= ${value}` : `\t${name}:${padding}${value},`;
    })
    .join("\n");
}

export function serializeRunnerLedgerConsumerGo(registry: GeneratedRegistry): string {
  requireExact(registry.profile.spec.profileId, "runner-ledger-consumer/v1", "profileId");
  requireExact(registry.profile.spec.stateMachineId, "runner-ledger-consumer/v1", "stateMachineId");
  requireExact(registry.stateMachine.id, "runner-ledger-consumer/v1", "stateMachine.id");
  requireExact(registry.stateMachine.initialState, "unclassified", "stateMachine.initialState");
  requireExact(registry.stateMachine.terminalStates, terminalStates, "stateMachine.terminalStates");
  requireExact(
    registry.profile.spec.transitionMatrix,
    RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX,
    "profile.transitionMatrix",
  );
  requireExact(
    registry.selector,
    {
      mode: "generated_registry_only",
      profileSelection: "exact_profile_id_and_digest",
      preflightProfileSelection: "exact_runner_ledger_preflight_v1_generated_identity",
      callerProvidedProfile: "forbidden",
      callerProvidedDispatch: "forbidden",
      ordinaryDispatchAsWriterAuthority: "forbidden",
      noOpSource: "consumed_same_verifier_claim_only",
    },
    "selector",
  );
  requireExact(
    registry.implementationBoundary,
    {
      runnerConsumer: "complete_return_success_noop_only",
      existingBrandNewWriter: "separate_existing_authority_chain",
      entryWriter: "not_implemented",
      recoveryWriter: "not_implemented",
      databaseSession: "read_only_preflight_closed_before_result",
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

  const identity = registry.profile.spec.identityBindings;
  const precedence = registry.profile.spec.errorPrecedence;
  const boundary = registry.implementationBoundary;
  const transitions = registry.stateMachine.transitions
    .map(
      (transition) =>
        `\t{from: ${goString(transition.from)}, event: ${goString(transition.event)}, to: ${goString(transition.to)}},`,
    )
    .join("\n");
  const cases = Object.entries(registry.profile.spec.transitionMatrix)
    .flatMap(([disposition, pairs]) =>
      pairs.map(
        (pair) =>
          `\tcase ${goString(`${disposition}\u0000${pair.state}\u0000${pair.action}`)}:\n\t\treturn runnerLedgerConsumerAction(${goString(pair.consumerAction)}), true`,
      ),
    )
    .join("\n");
  const pairCount = Object.values(RUNNER_LEDGER_CONSUMER_TRANSITION_MATRIX).reduce(
    (count, pairs) => count + pairs.length,
    0,
  );
  const boundPreflightConstants = goAssignments(
    [
      [
        "runnerLedgerConsumerBoundPreflightRegistryID",
        goString(registry.preflightBinding.registryId),
      ],
      [
        "runnerLedgerConsumerBoundPreflightRegistryDigest",
        goString(registry.preflightBinding.registryDigest),
      ],
      [
        "runnerLedgerConsumerBoundPreflightStateMachineDigest",
        goString(registry.preflightBinding.stateMachineDigest),
      ],
      [
        "runnerLedgerConsumerBoundPreflightPolicyDigest",
        goString(registry.preflightBinding.policyDigest),
      ],
      [
        "runnerLedgerConsumerBoundPreflightProfileID",
        goString(registry.preflightBinding.profileId),
      ],
      [
        "runnerLedgerConsumerBoundPreflightProfileDigest",
        goString(registry.preflightBinding.profileDigest),
      ],
    ],
    "=",
  );
  const profileFields = goAssignments(
    [
      ["profileID", goString(registry.profile.spec.profileId)],
      ["profileDigest", goString(registry.profile.profileDigest)],
      ["stateMachineID", goString(registry.profile.spec.stateMachineId)],
      ["canonicalizationProfile", goString(registry.profile.spec.canonicalization.profile)],
      ["canonicalizationAlgorithm", goString(registry.profile.spec.canonicalization.algorithm)],
      ["digestAlgorithm", goString(registry.profile.spec.canonicalization.digest)],
      ["comparison", goString(registry.profile.spec.canonicalization.comparison)],
      ["preflightProfileBinding", goString(identity.preflightProfile)],
      ["preflightDispatchBinding", goString(identity.preflightDispatchSubject)],
      ["currentEvidenceBinding", goString(identity.currentEvidenceBoundary)],
      ["orderedMigrationPrefixBinding", goString(identity.orderedMigrationPrefix)],
      ["runtimeResultBinding", goString(identity.runtimeResult)],
      ["storedContradictionPrecedence", goString(precedence.storedContradiction)],
      ["contextOperationalPrecedence", goString(precedence.contextOrOperationalFailure)],
      ["recoveryRequiredPrecedence", goString(precedence.recoveryRequired)],
      ["unsupportedTransitionPrecedence", goString(precedence.unsupportedTransition)],
      ["oneShotConsumptionPrecedence", goString(precedence.oneShotConsumption)],
      ["unknownOutcomePrecedence", goString(precedence.unknownOutcome)],
      ["runnerConsumerBoundary", goString(boundary.runnerConsumer)],
      ["existingBrandNewWriterBoundary", goString(boundary.existingBrandNewWriter)],
      ["entryWriterBoundary", goString(boundary.entryWriter)],
      ["recoveryWriterBoundary", goString(boundary.recoveryWriter)],
      ["databaseSessionBoundary", goString(boundary.databaseSession)],
      ["databaseTransactionBoundary", goString(boundary.databaseTransaction)],
      ["ledgerMutationBoundary", goString(boundary.ledgerMutation)],
      ["evidenceMutationBoundary", goString(boundary.evidenceMutation)],
      ["httpSurfaceBoundary", goString(boundary.httpSurface)],
      ["p2SurfaceBoundary", goString(boundary.p2Surface)],
      ["providerSideEffectsBoundary", goString(boundary.providerSideEffects)],
      ["productionDatabaseWritesBoundary", goString(boundary.productionDatabaseWrites)],
      ["deploymentBoundary", goString(boundary.deployment)],
      ["publicationBoundary", goString(boundary.publication)],
      ["gateStatusBoundary", goString(boundary.gateStatus)],
    ],
    ":",
  );

  return `// Code generated by scripts/generate-platform-runner-ledger-consumer-go.ts; DO NOT EDIT.

package migration

const (
\trunnerLedgerConsumerRegistryDigest     = ${goString(registry.registryDigest)}
\trunnerLedgerConsumerStateMachineDigest = ${goString(registry.stateMachineDigest)}
\trunnerLedgerConsumerPolicyDigest       = ${goString(registry.policyDigest)}

${boundPreflightConstants}
)

var generatedRunnerLedgerConsumerProfile = runnerLedgerConsumerProfile{
${profileFields}
}

var generatedRunnerLedgerConsumerTransitions = [...]runnerLedgerConsumerTransition{
${transitions}
}

const generatedRunnerLedgerConsumerPairCount = ${pairCount}

func generatedRunnerLedgerConsumerAction(disposition runnerLedgerPreflightDisposition, state RecoveryState, action RecoveryAction) (runnerLedgerConsumerAction, bool) {
\tswitch string(disposition) + "\\x00" + string(state) + "\\x00" + string(action) {
${cases}
\tdefault:
\t\treturn "", false
\t}
}
`;
}

function readRegistry(root: string): GeneratedRegistry {
  assertRunnerLedgerConsumerRegistryCurrent(root);
  return JSON.parse(
    readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_OUTPUT_PATH), "utf8"),
  ) as GeneratedRegistry;
}

export function expectedRunnerLedgerConsumerGo(root: string): string {
  return serializeRunnerLedgerConsumerGo(readRegistry(root));
}

export function assertRunnerLedgerConsumerGoCurrent(root: string): void {
  const expected = expectedRunnerLedgerConsumerGo(root);
  const actual = readFileSync(resolve(root, RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(
      `${RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH} is stale; run its generator with --write.`,
    );
  }
}

const root = resolve(import.meta.dirname, "..");
const mode = process.argv[2];

if (mode === "--write") {
  writeFileSync(
    resolve(root, RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH),
    expectedRunnerLedgerConsumerGo(root),
  );
  process.stdout.write(
    `platform-runner-ledger-consumer-go: wrote ${RUNNER_LEDGER_CONSUMER_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  assertRunnerLedgerConsumerGoCurrent(root);
  process.stdout.write("platform-runner-ledger-consumer-go: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-runner-ledger-consumer-go.ts --write|--check",
  );
}
