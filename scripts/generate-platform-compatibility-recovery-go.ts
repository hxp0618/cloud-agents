import { readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { resolve } from "node:path";

import {
  assertCompatibilityRecoveryRegistryV2Current,
  COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH,
} from "./lib/platform-compatibility-recovery-registry";

export const COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH =
  "services/control-plane/internal/compatibility/registry_generated.go";

type GeneratedOperation = {
  readonly operationId: string;
  readonly sqlFunction: string;
  readonly serviceMethod: string;
  readonly mode: "read_only" | "mutation";
  readonly capability: string;
  readonly unknownOutcome: "not_applicable" | "reconcile_required_no_write_retry";
};

type GeneratedProfile = {
  readonly profileDigest: string;
  readonly spec: {
    readonly profileId: string;
    readonly operations: ReadonlyArray<GeneratedOperation>;
  };
};

type GeneratedRegistry = {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly registryDigest: string;
  readonly stateMachineDigest: string;
  readonly policyDigest: string;
  readonly schemaBinding: {
    readonly schemaHead: string;
    readonly schemaCatalogSha256: string;
    readonly schemaMigrationSha256: string;
  };
  readonly selector: {
    readonly mode: string;
    readonly profileSelection: string;
    readonly callerProvidedProfile: string;
    readonly storedRowSelection: string;
    readonly schemaBinding: string;
  };
  readonly profiles: ReadonlyArray<GeneratedProfile>;
  readonly implementationBoundary: {
    readonly httpSurface: string;
    readonly externalSideEffects: string;
    readonly providerSideEffects: string;
    readonly productionDatabaseWrites: string;
    readonly gateStatus: string;
  };
};

type BoundOperation = GeneratedOperation & {
  readonly profileId: string;
  readonly profileDigest: string;
};

function goString(value: string): string {
  return JSON.stringify(value);
}

function operationVariable(serviceMethod: string): string {
  return `${serviceMethod[0]!.toLowerCase()}${serviceMethod.slice(1)}OperationProfile`;
}

function formatGo(source: string): string {
  const result = spawnSync("gofmt", [], {
    encoding: "utf8",
    input: source,
  });
  if (result.status !== 0) {
    throw new Error(`gofmt failed: ${result.stderr.trim()}`);
  }
  return result.stdout;
}

function validateRegistry(registry: GeneratedRegistry): ReadonlyArray<BoundOperation> {
  if (
    registry.formatVersion !== "cloud-agents-compatibility-recovery-registry/v2" ||
    registry.registryId !== "cloud-agents/platform/compatibility-recovery" ||
    registry.registryDigest !==
      "sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973" ||
    registry.stateMachineDigest !==
      "sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863" ||
    registry.policyDigest !==
      "sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708" ||
    registry.schemaBinding.schemaHead !== "000010" ||
    registry.schemaBinding.schemaCatalogSha256 !==
      "sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236" ||
    registry.schemaBinding.schemaMigrationSha256 !==
      "sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6" ||
    registry.selector.mode !== "generated_registry_only" ||
    registry.selector.profileSelection !== "exact_profile_id_and_digest" ||
    registry.selector.callerProvidedProfile !== "forbidden" ||
    registry.selector.storedRowSelection !== "forbidden" ||
    registry.selector.schemaBinding !== "exact_schema_head_catalog_and_migration_digest" ||
    registry.implementationBoundary.httpSurface !== "not_implemented" ||
    registry.implementationBoundary.externalSideEffects !== "forbidden" ||
    registry.implementationBoundary.providerSideEffects !== "forbidden" ||
    registry.implementationBoundary.productionDatabaseWrites !== "not_authorized" ||
    registry.implementationBoundary.gateStatus !== "all_gates_open"
  ) {
    throw new Error("The compatibility/recovery Go registry boundary drifted.");
  }

  const operations = registry.profiles
    .flatMap(({ profileDigest, spec }) =>
      spec.operations.map((operation) => ({
        ...operation,
        profileId: spec.profileId,
        profileDigest,
      })),
    )
    .toSorted((left, right) => left.serviceMethod.localeCompare(right.serviceMethod));
  if (registry.profiles.length !== 6 || operations.length !== 26) {
    throw new Error(
      "The compatibility/recovery Go port requires exactly 6 profiles and 26 operations.",
    );
  }
  const identities = [
    operations.map((operation) => operation.operationId),
    operations.map((operation) => operation.sqlFunction),
    operations.map((operation) => operation.serviceMethod),
    operations.map((operation) => operation.capability),
  ];
  if (identities.some((values) => new Set(values).size !== operations.length)) {
    throw new Error("The compatibility/recovery operation identities must be unique.");
  }
  for (const operation of operations) {
    if (
      !/^[A-Z][A-Za-z0-9]+$/u.test(operation.serviceMethod) ||
      !/^cloud_agents\.compatibility_recovery_[a-z0-9_]+_v2$/u.test(operation.sqlFunction) ||
      (operation.mode === "mutation" &&
        operation.unknownOutcome !== "reconcile_required_no_write_retry") ||
      (operation.mode === "read_only" && operation.unknownOutcome !== "not_applicable")
    ) {
      throw new Error(
        `Invalid generated compatibility/recovery operation ${operation.operationId}.`,
      );
    }
  }
  return operations;
}

export function serializeCompatibilityRecoveryGo(registry: GeneratedRegistry): string {
  const operations = validateRegistry(registry);
  const variables = operations
    .map(
      (operation) => `var ${operationVariable(operation.serviceMethod)} = operationProfile{
\tprofileID: ${goString(operation.profileId)},
\tprofileDigest: ${goString(operation.profileDigest)},
\toperationID: ${goString(operation.operationId)},
\tsqlFunction: ${goString(operation.sqlFunction)},
\tserviceMethod: ${goString(operation.serviceMethod)},
\tmode: ${goString(operation.mode)},
\tcapability: ${goString(operation.capability)},
\tunknownOutcome: ${goString(operation.unknownOutcome)},
}`,
    )
    .join("\n\n");
  const operationList = operations
    .map((operation) => `\t${operationVariable(operation.serviceMethod)},`)
    .join("\n");
  const getters = operations
    .map(
      (operation) => `func ${operation.serviceMethod}Operation() Operation {
\treturn Operation{profile: ${operationVariable(operation.serviceMethod)}}
}`,
    )
    .join("\n\n");

  return formatGo(`// Code generated by scripts/generate-platform-compatibility-recovery-go.ts; DO NOT EDIT.

package compatibility

const (
\tRegistryFormatVersion = ${goString(registry.formatVersion)}
\tRegistryID = ${goString(registry.registryId)}
\tRegistryDigest = ${goString(registry.registryDigest)}
\tStateMachineDigest = ${goString(registry.stateMachineDigest)}
\tPolicyDigest = ${goString(registry.policyDigest)}
\tSchemaHead = ${goString(registry.schemaBinding.schemaHead)}
\tSchemaCatalogDigest = ${goString(registry.schemaBinding.schemaCatalogSha256)}
\tSchemaMigrationDigest = ${goString(registry.schemaBinding.schemaMigrationSha256)}
)

${variables}

var generatedOperationProfiles = [...]operationProfile{
${operationList}
}

${getters}
`);
}

function readRegistry(root: string): GeneratedRegistry {
  assertCompatibilityRecoveryRegistryV2Current(root);
  return JSON.parse(
    readFileSync(resolve(root, COMPATIBILITY_RECOVERY_V2_OUTPUT_PATH), "utf8"),
  ) as GeneratedRegistry;
}

export function expectedCompatibilityRecoveryGo(root: string): string {
  return serializeCompatibilityRecoveryGo(readRegistry(root));
}

const root = resolve(import.meta.dirname, "..");
const output = resolve(root, COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH);
const [mode] = process.argv.slice(2);

if (mode === "--write") {
  writeFileSync(output, expectedCompatibilityRecoveryGo(root));
  process.stdout.write(
    `platform-compatibility-recovery-go: wrote ${COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH}\n`,
  );
} else if (mode === "--check") {
  const expected = expectedCompatibilityRecoveryGo(root);
  const actual = readFileSync(output, "utf8");
  if (actual !== expected) {
    throw new Error(
      `${COMPATIBILITY_RECOVERY_GO_OUTPUT_PATH} is stale; run bun scripts/generate-platform-compatibility-recovery-go.ts --write.`,
    );
  }
  process.stdout.write("platform-compatibility-recovery-go: current\n");
} else {
  throw new Error(
    "Usage: bun scripts/generate-platform-compatibility-recovery-go.ts --write|--check",
  );
}
