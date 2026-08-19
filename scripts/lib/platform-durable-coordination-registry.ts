import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

import {
  canonicalizeJson,
  type JsonRecord,
  type SemanticErrorCode,
  type SemanticResult,
} from "./platform-json-semantics";

export const DURABLE_COORDINATION_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v1.json";
export const DURABLE_COORDINATION_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/durable-coordination-registry.json";

const PROFILE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/durable-coordination-profile-v1.schema.json";
const SOURCE_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_PATH =
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-v1.schema.json";
const PERMISSION_SCHEMA_PATH = "contracts/platform/v1alpha1/schemas/permission.schema.json";
const SUBJECT_SCHEMA_PATH = "contracts/common/v1alpha1/schemas/subject-ref.schema.json";
const MANAGED_AGENT_OPENAPI_PATH = "contracts/managed-agent/v1alpha1/openapi.json";
const MANAGED_HOST_OPENAPI_PATH = "contracts/managed-host/v1alpha1/openapi.json";
const ADR_PATH = "docs/plan/adr/0013-p1-durable-coordination-contract.md";
const GENERATOR_PATH = "scripts/generate-platform-durable-coordination-registry.ts";
const LIBRARY_PATH = "scripts/lib/platform-durable-coordination-registry.ts";
const LIBRARY_TEST_PATH = "scripts/lib/platform-durable-coordination-registry.test.ts";
const JSON_SEMANTICS_PATH = "scripts/lib/platform-json-semantics.ts";

const PROFILE_FORMAT = "cloud-agents-durable-coordination-profile/v1";
const SOURCE_FORMAT = "cloud-agents-durable-coordination-source/v1";
const OUTPUT_FORMAT = "cloud-agents-durable-coordination-registry/v1";
const REGISTRY_ID = "cloud-agents/platform/durable-coordination";
const PROFILE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/durable-coordination-profile-v1.schema.json";
const SOURCE_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/durable-coordination-registry-source-v1.schema.json";
const OUTPUT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/durable-coordination-registry-v1.schema.json";
const SUBJECT_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/common/v1alpha1/schemas/subject-ref.schema.json";
const PLATFORM_SCHEMA_PREFIX = "https://schemas.cloud-agents.dev/platform/v1alpha1/schemas/";
const GENERATED_PROFILE_DOMAIN = "cloud-agents/durable-coordination/profile/v1";
const GENERATED_POLICY_DOMAIN = "cloud-agents/durable-coordination/policies/v1";
const GENERATED_REGISTRY_DOMAIN = "cloud-agents/durable-coordination/registry/v1";
const GENERATED_SOURCE_DOMAIN = "cloud-agents/durable-coordination/source/v1";
const GENERATED_STATE_MACHINE_DOMAIN = "cloud-agents/durable-coordination/state-machines/v1";
const OPERATION_METHODS = new Set(["delete", "patch", "post", "put"]);
const STATE_MACHINE_IDS = [
  "cleanup/v1",
  "finalizer/v1",
  "idempotency/v1",
  "operation_attempt/v1",
  "outbox/v1",
  "platform_operation/v1",
  "terminal_receipt/v1",
] as const;

type StateTransition = {
  readonly from: string;
  readonly event: string;
  readonly to: string;
};

type StateMachine = {
  readonly id: string;
  readonly initialState: string;
  readonly states: ReadonlyArray<string>;
  readonly terminalStates: ReadonlyArray<string>;
  readonly transitions: ReadonlyArray<StateTransition>;
};

type IdempotentHttpOperation = {
  readonly operationId: string;
  readonly method: string;
  readonly path: string;
  readonly idempotencyHeader: string;
};

type ProfileDocument = JsonRecord & {
  readonly formatVersion: string;
  readonly profileId: string;
  readonly operationId: string;
  readonly http: JsonRecord;
  readonly subjectIdentity: JsonRecord;
  readonly projection: JsonRecord;
  readonly authorization: JsonRecord;
  readonly coordination: JsonRecord;
  readonly idempotency: JsonRecord;
  readonly result: JsonRecord;
};

type RegistrySource = JsonRecord & {
  readonly formatVersion: string;
  readonly registryId: string;
  readonly profileDocuments: ReadonlyArray<string>;
  readonly policies: JsonRecord;
  readonly stateMachines: ReadonlyArray<StateMachine>;
};

export class DurableCoordinationContractError extends Error {
  constructor(
    readonly code: SemanticErrorCode,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "DurableCoordinationContractError";
  }
}

export function buildDurableCoordinationRegistry(root: string): JsonRecord {
  const source = readRegistrySource(root);
  const profiles = validateDurableCoordinationSource(root, source);
  const sourceDigest = domainDigest(GENERATED_SOURCE_DOMAIN, source);
  const stateMachineDigest = domainDigest(GENERATED_STATE_MACHINE_DOMAIN, source.stateMachines);
  const policyDigest = domainDigest(GENERATED_POLICY_DOMAIN, source.policies);
  const generatedProfiles = profiles.map((spec) => ({
    profileDigest: domainDigest(GENERATED_PROFILE_DOMAIN, {
      registryId: source.registryId,
      stateMachineDigest,
      policyDigest,
      spec,
    }),
    spec,
  }));
  const body: JsonRecord = {
    formatVersion: OUTPUT_FORMAT,
    registryId: source.registryId,
    sourceDigest,
    stateMachineDigest,
    policyDigest,
    stateMachines: source.stateMachines,
    policies: source.policies,
    profiles: generatedProfiles,
  };
  const generated = {
    ...body,
    registryDigest: domainDigest(GENERATED_REGISTRY_DOMAIN, body),
  };
  validateAgainstSchema(root, OUTPUT_SCHEMA_ID, generated);
  return generated;
}

export function serializeDurableCoordinationRegistry(registry: JsonRecord): string {
  return `${JSON.stringify(registry, null, 2)}\n`;
}

export function assertDurableCoordinationRegistryCurrent(root: string): void {
  const expected = serializeDurableCoordinationRegistry(buildDurableCoordinationRegistry(root));
  const output = resolve(root, DURABLE_COORDINATION_OUTPUT_PATH);
  const actual = readFileSync(output, "utf8");
  if (actual !== expected) {
    throw new Error(
      `${DURABLE_COORDINATION_OUTPUT_PATH} is stale; run bun ${GENERATOR_PATH} --write.`,
    );
  }
}

export function durableCoordinationRegistryInputs(root: string): string[] {
  const source = readRegistrySource(root);
  const profilePaths = source.profileDocuments.map((path) => profileRepositoryPath(root, path));
  const projectionPaths = profilePaths.map((path) => {
    const profile = readProfile(root, path);
    return schemaRepositoryPath(profile.projection.schemaId);
  });
  const resultPaths = profilePaths.map((path) => {
    const profile = readProfile(root, path);
    return schemaRepositoryPath(profile.result.schemaId);
  });
  const inputs = [
    ADR_PATH,
    DURABLE_COORDINATION_SOURCE_PATH,
    GENERATOR_PATH,
    JSON_SEMANTICS_PATH,
    LIBRARY_PATH,
    LIBRARY_TEST_PATH,
    MANAGED_AGENT_OPENAPI_PATH,
    MANAGED_HOST_OPENAPI_PATH,
    OUTPUT_SCHEMA_PATH,
    PERMISSION_SCHEMA_PATH,
    PROFILE_SCHEMA_PATH,
    SOURCE_SCHEMA_PATH,
    SUBJECT_SCHEMA_PATH,
    ...profilePaths,
    ...projectionPaths,
    ...resultPaths,
  ];
  const uniqueInputs = [...new Set(inputs)].toSorted();
  for (const path of uniqueInputs) requireRegularRepositoryFile(root, path);
  return uniqueInputs;
}

export function validateDurableCoordinationFixture(
  document: unknown,
  root: string,
): SemanticResult {
  if (!isRecord(document) || typeof document.formatVersion !== "string") return success();
  try {
    if (document.formatVersion === PROFILE_FORMAT) {
      validateProfileAgainstContracts(root, document as ProfileDocument, discoverOperations(root));
    } else if (document.formatVersion === SOURCE_FORMAT) {
      validateDurableCoordinationSource(root, document as RegistrySource);
    } else if (document.formatVersion === OUTPUT_FORMAT) {
      const expected = buildDurableCoordinationRegistry(root);
      if (!canonicalEqual(document, expected)) {
        throw coordinationError(
          "COORDINATION_REGISTRY_DIGEST_MISMATCH",
          "/registryDigest",
          "Generated durable coordination registry does not match its source inputs.",
        );
      }
    }
    return success();
  } catch (error) {
    if (error instanceof DurableCoordinationContractError) {
      return failure(error.code, error.path);
    }
    throw error;
  }
}

export function validateDurableCoordinationSource(
  root: string,
  sourceValue: unknown,
): ProfileDocument[] {
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, sourceValue);
  const source = requireRecord(sourceValue, "/") as RegistrySource;
  if (source.formatVersion !== SOURCE_FORMAT || source.registryId !== REGISTRY_ID) {
    throw coordinationError(
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
      "/formatVersion",
      "Durable coordination source identity is not recognized.",
    );
  }
  assertSortedUniqueStrings(
    source.profileDocuments,
    "/profileDocuments",
    "COORDINATION_REGISTRY_BINDING_MISMATCH",
  );
  validateStateMachines(source.stateMachines);

  const operations = discoverOperations(root);
  const operationById = new Map(operations.map((operation) => [operation.operationId, operation]));
  const profiles = source.profileDocuments.map((path, index) => {
    const repositoryPath = profileRepositoryPath(root, path);
    const profile = readProfile(root, repositoryPath);
    validateProfileAgainstContracts(root, profile, operations);
    if (!operationById.has(profile.operationId)) {
      throw coordinationError(
        "COORDINATION_PROFILE_BINDING_MISMATCH",
        `/profileDocuments/${index}`,
        `Profile ${profile.profileId} is not bound to an idempotent OpenAPI operation.`,
      );
    }
    return profile;
  });
  const profileIds = profiles.map((profile) => profile.profileId);
  assertSortedUniqueStrings(profileIds, "/profiles", "COORDINATION_REGISTRY_BINDING_MISMATCH");
  const profileOperationIds = profiles.map((profile) => profile.operationId).toSorted();
  const operationIds = operations.map((operation) => operation.operationId).toSorted();
  if (!canonicalEqual(profileOperationIds, operationIds)) {
    throw coordinationError(
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
      "/profileDocuments",
      "Generated profile sources must exactly cover idempotent OpenAPI operations.",
    );
  }
  return profiles;
}

function readRegistrySource(root: string): RegistrySource {
  const source = parseJsonFile(resolve(root, DURABLE_COORDINATION_SOURCE_PATH));
  validateAgainstSchema(root, SOURCE_SCHEMA_ID, source);
  return source as RegistrySource;
}

function readProfile(root: string, repositoryPath: string): ProfileDocument {
  requireRegularRepositoryFile(root, repositoryPath);
  const profile = parseJsonFile(resolve(root, repositoryPath));
  validateAgainstSchema(root, PROFILE_SCHEMA_ID, profile);
  return profile as ProfileDocument;
}

function validateProfileAgainstContracts(
  root: string,
  profileValue: unknown,
  operations: ReadonlyArray<IdempotentHttpOperation>,
): void {
  validateAgainstSchema(root, PROFILE_SCHEMA_ID, profileValue);
  const profile = profileValue as ProfileDocument;
  const operation = operations.find((candidate) => candidate.operationId === profile.operationId);
  if (!operation) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/operationId",
      `No idempotent OpenAPI operation matches ${profile.operationId}.`,
    );
  }
  if (
    profile.profileId !== `${profile.operationId}/v1alpha1` ||
    profile.http.method !== operation.method ||
    profile.http.path !== operation.path ||
    profile.http.idempotencyHeader !== operation.idempotencyHeader
  ) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/http",
      `Profile ${profile.profileId} does not match its OpenAPI route authority.`,
    );
  }
  if (profile.subjectIdentity.schemaId !== SUBJECT_SCHEMA_ID) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/subjectIdentity/schemaId",
      "Profile SubjectRef schema identity drifted.",
    );
  }
  const subjectSchema = parseJsonFile(resolve(root, SUBJECT_SCHEMA_PATH));
  if (subjectSchema.$id !== profile.subjectIdentity.schemaId) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/subjectIdentity/schemaId",
      "Profile SubjectRef schema is not present at the checked-in authority path.",
    );
  }
  const projectionSchemaPath = schemaRepositoryPath(profile.projection.schemaId);
  requireRegularRepositoryFile(root, projectionSchemaPath);
  const projectionSchema = parseJsonFile(resolve(root, projectionSchemaPath));
  const projectionProperties = requireRecord(projectionSchema.properties, "/projection/properties");
  const operationIdSchema = requireRecord(
    projectionProperties.operationId,
    "/projection/properties/operationId",
  );
  const canonicalization = requireRecord(
    projectionSchema["x-cloud-agents-canonicalization"],
    "/projection/x-cloud-agents-canonicalization",
  );
  if (
    projectionSchema.$id !== profile.projection.schemaId ||
    operationIdSchema.const !== profile.operationId ||
    canonicalization.profile !== profile.projection.canonicalizationProfile ||
    canonicalization.algorithm !== profile.projection.canonicalizationAlgorithm ||
    canonicalization.digest !== profile.projection.digestAlgorithm ||
    canonicalization.numberHandling !== profile.projection.numberHandling
  ) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/projection",
      `Profile ${profile.profileId} does not match its canonical projection schema.`,
    );
  }
  const resultSchemaPath = schemaRepositoryPath(profile.result.schemaId);
  requireRegularRepositoryFile(root, resultSchemaPath);
  const resultSchema = parseJsonFile(resolve(root, resultSchemaPath));
  if (resultSchema.$id !== profile.result.schemaId) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/result/schemaId",
      `Profile ${profile.profileId} result schema identity drifted.`,
    );
  }
  validateCurrentManagedAgentProfile(profile);
}

function validateCurrentManagedAgentProfile(profile: ProfileDocument): void {
  if (profile.operationId !== "managedAgentCreateProject") return;
  const expected = {
    profileId: "managedAgentCreateProject/v1alpha1",
    authorization: {
      tenantSource: "path.tenantId",
      scopeSource: "body.organizationRef",
      requiredPermission: "projects.create",
    },
    coordination: {
      class: "resource_change",
      createsPlatformOperation: false,
      externalSideEffect: "forbidden",
      outboxEventClass: "resource_change",
      requiredFinalizers: [],
    },
    idempotency: {
      replayTtlSeconds: 86400,
      pendingReplay: "in_progress_reference",
      terminalReplay: "redacted_terminal_envelope",
      conflict: "same_key_different_request_digest",
    },
    result: {
      kind: "resource_reference",
      resourceKind: "project",
      schemaId: `${PLATFORM_SCHEMA_PREFIX}project.schema.json`,
      allowedFields: ["resourceId", "resourceKind", "resourceVersion", "stableErrorCode"],
    },
  };
  if (
    profile.profileId !== expected.profileId ||
    !canonicalEqual(profile.authorization, expected.authorization) ||
    !canonicalEqual(profile.coordination, expected.coordination) ||
    !canonicalEqual(profile.idempotency, expected.idempotency) ||
    !canonicalEqual(profile.result, expected.result)
  ) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/",
      "managedAgentCreateProject/v1alpha1 drifted from the approved A2.3 profile.",
    );
  }
}

function validateStateMachines(values: ReadonlyArray<StateMachine>): void {
  const ids = values.map((machine) => machine.id);
  assertSortedUniqueStrings(ids, "/stateMachines");
  if (!canonicalEqual(ids, STATE_MACHINE_IDS)) {
    throw stateMachineError(
      "/stateMachines",
      "Durable coordination state-machine catalog must contain the approved seven IDs exactly.",
    );
  }
  for (const [machineIndex, machine] of values.entries()) {
    const path = `/stateMachines/${machineIndex}`;
    assertSortedUniqueStrings(machine.states, `${path}/states`);
    assertSortedUniqueStrings(machine.terminalStates, `${path}/terminalStates`);
    const states = new Set(machine.states);
    const terminals = new Set(machine.terminalStates);
    if (!states.has(machine.initialState)) {
      throw stateMachineError(`${path}/initialState`, `${machine.id} initial state is unknown.`);
    }
    for (const terminal of terminals) {
      if (!states.has(terminal)) {
        throw stateMachineError(`${path}/terminalStates`, `${machine.id} terminal is unknown.`);
      }
    }
    const transitionKeys = machine.transitions.map(
      (transition) => `${transition.from}\0${transition.event}\0${transition.to}`,
    );
    assertSortedUniqueStrings(transitionKeys, `${path}/transitions`);
    const deterministic = new Set<string>();
    for (const [transitionIndex, transition] of machine.transitions.entries()) {
      const transitionPath = `${path}/transitions/${transitionIndex}`;
      if (!states.has(transition.from) || !states.has(transition.to)) {
        throw stateMachineError(
          transitionPath,
          `${machine.id} transition references unknown state.`,
        );
      }
      if (terminals.has(transition.from)) {
        throw stateMachineError(
          transitionPath,
          `${machine.id} terminal state ${transition.from} has an outgoing transition.`,
        );
      }
      const key = `${transition.from}\0${transition.event}`;
      if (deterministic.has(key)) {
        throw stateMachineError(
          transitionPath,
          `${machine.id} has multiple destinations for ${transition.from}/${transition.event}.`,
        );
      }
      deterministic.add(key);
    }
    const reachable = graphClosure(
      new Set([machine.initialState]),
      machine.transitions.map((transition) => [transition.from, transition.to] as const),
    );
    if (reachable.size !== states.size) {
      throw stateMachineError(path, `${machine.id} contains an unreachable state.`);
    }
    const canReachTerminal = graphClosure(
      terminals,
      machine.transitions.map((transition) => [transition.to, transition.from] as const),
    );
    if (canReachTerminal.size !== states.size) {
      throw stateMachineError(path, `${machine.id} contains a state with no terminal path.`);
    }
  }
}

function discoverOperations(root: string): IdempotentHttpOperation[] {
  const operations: IdempotentHttpOperation[] = [];
  for (const repositoryPath of [MANAGED_AGENT_OPENAPI_PATH, MANAGED_HOST_OPENAPI_PATH]) {
    const document = parseJsonFile(resolve(root, repositoryPath));
    const paths = requireRecord(document.paths, `/${repositoryPath}/paths`);
    for (const path of Object.keys(paths).toSorted()) {
      const pathItem = requireRecord(paths[path], `/${repositoryPath}/paths/${path}`);
      const pathParameters = collectParameters(pathItem.parameters, document);
      for (const method of Object.keys(pathItem).toSorted()) {
        if (!OPERATION_METHODS.has(method)) continue;
        const operation = requireRecord(pathItem[method], `/${repositoryPath}/${method}/${path}`);
        const parameters = [
          ...pathParameters,
          ...collectParameters(operation.parameters, document),
        ];
        const idempotency = parameters.find(
          (parameter) =>
            parameter.in === "header" &&
            typeof parameter.name === "string" &&
            parameter.name.toLowerCase() === "idempotency-key",
        );
        if (!idempotency) continue;
        operations.push({
          operationId: requireString(operation.operationId, "/operationId"),
          method: method.toUpperCase(),
          path,
          idempotencyHeader: requireString(idempotency.name, "/parameters/Idempotency-Key"),
        });
      }
    }
  }
  operations.sort((left, right) => compareStrings(left.operationId, right.operationId));
  const ids = operations.map((operation) => operation.operationId);
  if (new Set(ids).size !== ids.length || operations.length === 0) {
    throw coordinationError(
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
      "/operations",
      "Idempotent OpenAPI operation identities must be non-empty and unique.",
    );
  }
  return operations;
}

function collectParameters(value: unknown, document: JsonRecord): JsonRecord[] {
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new TypeError("OpenAPI parameters must be an array.");
  return value.map((parameter) => {
    const record = requireRecord(parameter, "/parameters");
    if (typeof record.$ref !== "string") return record;
    if (!record.$ref.startsWith("#/")) throw new TypeError("OpenAPI parameter ref must be local.");
    return requireRecord(resolveJsonPointer(document, record.$ref.slice(1)), record.$ref);
  });
}

function graphClosure(
  initial: ReadonlySet<string>,
  edges: ReadonlyArray<readonly [string, string]>,
): Set<string> {
  const result = new Set(initial);
  let changed = true;
  while (changed) {
    changed = false;
    for (const [from, to] of edges) {
      if (result.has(from) && !result.has(to)) {
        result.add(to);
        changed = true;
      }
    }
  }
  return result;
}

function profileRepositoryPath(root: string, path: string): string {
  const fixturesRoot = resolve(root, "contracts/platform/v1alpha1/fixtures");
  const target = resolve(fixturesRoot, path);
  if (!isWithin(fixturesRoot, target)) {
    throw coordinationError(
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
      "/profileDocuments",
      `Profile document escapes the platform fixture root: ${path}.`,
    );
  }
  return relative(root, target).split(sep).join("/");
}

function schemaRepositoryPath(schemaId: unknown): string {
  if (typeof schemaId !== "string" || !schemaId.startsWith(PLATFORM_SCHEMA_PREFIX)) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/schemaId",
      `Unsupported platform schema ID ${String(schemaId)}.`,
    );
  }
  const name = schemaId.slice(PLATFORM_SCHEMA_PREFIX.length);
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*[.]schema[.]json$/u.test(name)) {
    throw coordinationError(
      "COORDINATION_PROFILE_BINDING_MISMATCH",
      "/schemaId",
      `Invalid platform schema ID ${schemaId}.`,
    );
  }
  return `contracts/platform/v1alpha1/schemas/${name}`;
}

function validateAgainstSchema(root: string, schemaId: string, value: unknown): void {
  const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: true });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const keyword of [
    "x-cloud-agents-canonicalization",
    "x-cloud-agents-normalization",
    "x-cloud-agents-security",
    "x-cloud-agents-semantic-constraints",
  ]) {
    ajv.addKeyword(keyword);
  }
  for (const path of [
    PERMISSION_SCHEMA_PATH,
    PROFILE_SCHEMA_PATH,
    SOURCE_SCHEMA_PATH,
    OUTPUT_SCHEMA_PATH,
  ]) {
    ajv.addSchema(parseJsonFile(resolve(root, path)));
  }
  const validate = ajv.getSchema(schemaId);
  if (!validate || !validate(value)) {
    throw coordinationError(
      "COORDINATION_REGISTRY_BINDING_MISMATCH",
      "/",
      `Durable coordination document violates ${schemaId}: ${ajv.errorsText(validate?.errors)}.`,
    );
  }
}

function domainDigest(domain: string, value: unknown): string {
  const hash = createHash("sha256");
  hash.update(domain, "utf8");
  hash.update("\0", "utf8");
  hash.update(canonicalizeJson(value));
  return `sha256:${hash.digest("hex")}`;
}

function assertSortedUniqueStrings(
  values: ReadonlyArray<string>,
  path: string,
  code: SemanticErrorCode = "COORDINATION_STATE_MACHINE_INVALID",
): void {
  const sorted = [...values].toSorted();
  if (new Set(values).size !== values.length || !canonicalEqual(values, sorted)) {
    throw coordinationError(code, path, `${path} must be sorted and unique.`);
  }
}

function requireRegularRepositoryFile(root: string, repositoryPath: string): void {
  const target = resolve(root, repositoryPath);
  if (!isWithin(root, target)) throw new Error(`Repository input escapes root: ${repositoryPath}.`);
  const stat = lstatSync(target);
  if (!stat.isFile() || stat.isSymbolicLink()) {
    throw new Error(`Repository input must be a regular file: ${repositoryPath}.`);
  }
}

function parseJsonFile(path: string): JsonRecord {
  const value: unknown = JSON.parse(readFileSync(path, "utf8"));
  return requireRecord(value, path);
}

function resolveJsonPointer(document: unknown, pointer: string): unknown {
  if (pointer === "") return document;
  if (!pointer.startsWith("/")) throw new TypeError(`Invalid JSON pointer ${pointer}.`);
  let value = document;
  for (const segment of pointer.slice(1).split("/")) {
    const key = segment.replaceAll("~1", "/").replaceAll("~0", "~");
    if (Array.isArray(value)) {
      const index = Number(key);
      if (!Number.isSafeInteger(index) || index < 0 || index >= value.length) {
        throw new TypeError(`JSON pointer index is invalid: ${pointer}.`);
      }
      value = value[index];
    } else if (isRecord(value) && Object.hasOwn(value, key)) {
      value = value[key];
    } else {
      throw new TypeError(`JSON pointer does not resolve: ${pointer}.`);
    }
  }
  return value;
}

function requireRecord(value: unknown, path: string): JsonRecord {
  if (!isRecord(value)) throw new TypeError(`${path} must be an object.`);
  return value;
}

function requireString(value: unknown, path: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new TypeError(`${path} must be a non-empty string.`);
  }
  return value;
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isWithin(root: string, target: string): boolean {
  const path = relative(root, target);
  return path === "" || (path !== ".." && !path.startsWith(`..${sep}`) && !path.startsWith(sep));
}

function canonicalEqual(left: unknown, right: unknown): boolean {
  const leftBytes = canonicalizeJson(left);
  const rightBytes = canonicalizeJson(right);
  if (leftBytes.length !== rightBytes.length) return false;
  return leftBytes.every((value, index) => value === rightBytes[index]);
}

function compareStrings(left: string, right: string): number {
  if (left < right) return -1;
  if (left > right) return 1;
  return 0;
}

function success(): SemanticResult {
  return { valid: true, errors: [] };
}

function failure(code: SemanticErrorCode, path: string): SemanticResult {
  return { valid: false, errors: [{ code, path }] };
}

function stateMachineError(path: string, message: string): DurableCoordinationContractError {
  return coordinationError("COORDINATION_STATE_MACHINE_INVALID", path, message);
}

function coordinationError(
  code: SemanticErrorCode,
  path: string,
  message: string,
): DurableCoordinationContractError {
  return new DurableCoordinationContractError(code, path, message);
}
