import { createHash } from "node:crypto";
import { lstatSync, readdirSync, readFileSync } from "node:fs";
import { dirname, extname, join, relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import type { ErrorObject } from "ajv";

import {
  assertExpectedSemanticResult,
  canonicalizeJson,
  canonicalJsonDigest,
  validateCanonicalNamespaceRefFixture,
  validateCanonicalSubjectRefFixture,
  validateManagedAgentCreateProjectIdempotencyFixture,
  validatePlatformSemantics,
} from "./platform-json-semantics";

type JsonRecord = Record<string, unknown>;

const JSON_SCHEMA_2020_12 = "https://json-schema.org/draft/2020-12/schema";
const FIXTURE_MANIFEST_SCHEMA_ID =
  "https://schemas.cloud-agents.dev/common/v1alpha1/schemas/fixture-manifest.schema.json";
const OPENAPI_VERSION = "3.1.1";
const OPERATION_METHODS = new Set([
  "delete",
  "get",
  "head",
  "options",
  "patch",
  "post",
  "put",
  "trace",
]);

const P1_A1_FIXTURE_INVENTORY: Readonly<Record<string, ReadonlyArray<JsonRecord>>> = {
  "common/v1alpha1/fixtures/manifest.json": [
    {
      name: "subject-ref",
      schema: "../schemas/subject-ref.schema.json",
      instance: "golden/subject-ref.json",
      expectedSchemaValid: true,
    },
    {
      name: "subject-ref-canonical",
      schema: "../schemas/subject-ref.schema.json",
      document: "golden/subject-ref-canonical.json",
      instancePointer: "/instance",
      expectedSchemaValid: true,
      expectedSemanticValid: true,
    },
    {
      name: "subject-ref-canonical-escape",
      schema: "../schemas/subject-ref.schema.json",
      document: "negative/subject-ref-canonical-escape.json",
      instancePointer: "/instance",
      expectedSchemaValid: true,
      expectedSemanticValid: false,
      expectedError: "CANONICAL_SUBJECT_REF_MISMATCH",
    },
    {
      name: "subject-ref-digest-mismatch",
      schema: "../schemas/subject-ref.schema.json",
      document: "negative/subject-ref-digest-mismatch.json",
      instancePointer: "/instance",
      expectedSchemaValid: true,
      expectedSemanticValid: false,
      expectedError: "CANONICAL_SUBJECT_REF_DIGEST_MISMATCH",
    },
    {
      name: "subject-ref-extra-field",
      schema: "../schemas/subject-ref.schema.json",
      instance: "negative/subject-ref-extra-field.json",
      expectedSchemaValid: false,
      expectedError: "UNKNOWN_FIELD",
    },
  ],
  "platform/v1alpha1/fixtures/manifest.json": [
    {
      name: "project-create-request",
      schema: "../schemas/project-create-request.schema.json",
      instance: "golden/project-create-request.json",
      expectedSchemaValid: true,
      expectedSemanticValid: true,
    },
    {
      name: "project-create-server-owned-field",
      schema: "../schemas/project-create-request.schema.json",
      instance: "negative/project-create-server-owned-field.json",
      expectedSchemaValid: false,
      expectedError: "UNKNOWN_FIELD",
    },
    {
      name: "managed-agent-create-project-idempotency",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      document: "golden/managed-agent-create-project-idempotency.json",
      instancePointer: "/projection",
      expectedSchemaValid: true,
      expectedSemanticValid: true,
    },
    {
      name: "managed-agent-create-project-idempotency-excluded-headers",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      document: "golden/managed-agent-create-project-idempotency-excluded-headers.json",
      instancePointer: "/projection",
      expectedSchemaValid: true,
      expectedSemanticValid: true,
    },
    {
      name: "managed-agent-create-project-body-tenant-authority",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      instance: "negative/managed-agent-create-project-body-tenant-authority.json",
      expectedSchemaValid: false,
      expectedError: "UNKNOWN_FIELD",
    },
    {
      name: "managed-agent-create-project-number-field",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      instance: "negative/managed-agent-create-project-number-field.json",
      expectedSchemaValid: false,
      expectedError: "UNKNOWN_FIELD",
    },
    {
      name: "managed-agent-create-project-authority-mismatch",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      document: "negative/managed-agent-create-project-authority-mismatch.json",
      instancePointer: "/projection",
      expectedSchemaValid: true,
      expectedSemanticValid: false,
      expectedError: "IDEMPOTENCY_PATH_BODY_AUTHORITY_MISMATCH",
    },
    {
      name: "managed-agent-create-project-digest-mismatch",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      document: "negative/managed-agent-create-project-digest-mismatch.json",
      instancePointer: "/projection",
      expectedSchemaValid: true,
      expectedSemanticValid: false,
      expectedError: "CANONICAL_IDEMPOTENCY_REQUEST_DIGEST_MISMATCH",
    },
    {
      name: "managed-agent-create-project-canonical-escape",
      schema: "../schemas/managed-agent-create-project-idempotency-projection.schema.json",
      document: "negative/managed-agent-create-project-canonical-escape.json",
      instancePointer: "/projection",
      expectedSchemaValid: true,
      expectedSemanticValid: false,
      expectedError: "CANONICAL_IDEMPOTENCY_REQUEST_MISMATCH",
    },
  ],
};

export type PlatformContractBootstrapSummary = {
  readonly status: "BOOTSTRAP_VALIDATED";
  readonly notGateClosure: true;
  readonly jsonFiles: number;
  readonly schemaFiles: number;
  readonly openApiFiles: number;
  readonly protoFiles: number;
  readonly fixtureManifests: number;
  readonly fixtureCases: number;
  readonly operationIds: number;
  readonly jsonSchemaValidation: "AJV_2020_AND_IN_REPO_SEMANTICS_PASS";
  readonly contractManifestSha256: string;
  readonly missing: ReadonlyArray<string>;
};

type OpenApiValidationContext = {
  readonly operationIds?: Set<string>;
};

export function validatePlatformContractTree(root: string): PlatformContractBootstrapSummary {
  const contractRoot = resolve(root, "contracts");
  const files = walkFiles(contractRoot).filter((file) => {
    const path = relativePath(contractRoot, file);
    return path !== "generation.lock.json" && !path.startsWith("generated/");
  });
  const jsonFiles = files.filter((file) => extname(file) === ".json");
  const protoFiles = files.filter((file) => extname(file) === ".proto");
  const schemaFiles = jsonFiles.filter((file) => file.endsWith(".schema.json"));
  const openApiFiles = jsonFiles.filter((file) => file.endsWith("/openapi.json"));
  const fixtureManifests = jsonFiles.filter((file) => file.endsWith("/fixtures/manifest.json"));
  const identifiers = new Map<string, string>();
  const operationIdRegistry = new Set<string>();
  let operationIds = 0;

  for (const file of jsonFiles) {
    const document = parseJsonFile(file);
    if (file.endsWith(".schema.json")) {
      validateJsonSchemaDocument(document, relativePath(root, file));
      const identifier = requiredString(document.$id, `${relativePath(root, file)} $id`);
      const previous = identifiers.get(identifier);
      if (previous)
        throw new Error(`Duplicate JSON Schema $id ${identifier}: ${previous} and ${file}.`);
      identifiers.set(identifier, file);
    }
    validateDocumentReferences(document, file, contractRoot);
    if (file.endsWith("/openapi.json")) {
      operationIds += validateOpenApiDocument(document, relativePath(root, file), {
        operationIds: operationIdRegistry,
      });
    }
    if (file.endsWith("/fixtures/manifest.json")) {
      validateFixtureManifest(document, file, contractRoot);
    }
  }

  for (const file of protoFiles) {
    validateProtoSource(readFileSync(file, "utf8"), relativePath(root, file));
  }
  validateProtoContractTree(protoFiles, root, contractRoot);

  validateP1A1HttpIdempotencyBinding(openApiFiles, schemaFiles);

  const fixtureCases = validateJsonSchemaFixtures(schemaFiles, fixtureManifests, contractRoot);

  if (schemaFiles.length === 0 || openApiFiles.length !== 2 || protoFiles.length < 3) {
    throw new Error(
      `Contract roots incomplete: ${schemaFiles.length} schemas, ${openApiFiles.length} OpenAPI, ${protoFiles.length} Proto.`,
    );
  }
  if (fixtureManifests.length !== 2) {
    throw new Error(`Expected two JSON fixture manifests, found ${fixtureManifests.length}.`);
  }

  return {
    status: "BOOTSTRAP_VALIDATED",
    notGateClosure: true,
    jsonFiles: jsonFiles.length,
    schemaFiles: schemaFiles.length,
    openApiFiles: openApiFiles.length,
    protoFiles: protoFiles.length,
    fixtureManifests: fixtureManifests.length,
    fixtureCases,
    operationIds,
    jsonSchemaValidation: "AJV_2020_AND_IN_REPO_SEMANTICS_PASS",
    contractManifestSha256: contractManifestDigest(contractRoot, files),
    missing: [
      "json-schema-2020-12-official-test-suite",
      "openapi-3.1-semantic-validation",
      "proto-descriptor-and-breaking",
      "generated-sdk-replay",
      "n-minus-one-compatibility",
      "response-watch-unknown-field-preservation",
      "runtime-server-path-and-tenant-authority-enforcement",
      "remaining-generator-supply-chain-review",
    ],
  };
}

function validateP1A1HttpIdempotencyBinding(
  openApiFiles: ReadonlyArray<string>,
  schemaFiles: ReadonlyArray<string>,
): void {
  const operationId = "managedAgentCreateProject";
  const bindings: Array<{
    readonly operationId: string;
    readonly method: string;
    readonly path: string;
  }> = [];
  for (const file of openApiFiles) {
    const document = parseJsonFile(file);
    const paths = requiredRecord(document.paths, `${file} paths`);
    for (const [path, rawPathItem] of Object.entries(paths)) {
      const pathItem = requiredRecord(rawPathItem, `${file} ${path}`);
      const pathParameters = collectOpenApiParameters(
        pathItem.parameters,
        document,
        `${file} path ${path}`,
      );
      for (const [method, rawOperation] of Object.entries(pathItem)) {
        if (!OPERATION_METHODS.has(method)) continue;
        const operation = requiredRecord(rawOperation, `${file} ${method} ${path}`);
        const operationParameters = collectOpenApiParameters(
          operation.parameters,
          document,
          `${file} ${method} ${path}`,
        );
        const hasIdempotencyKey = [...pathParameters, ...operationParameters].some(
          (parameter) =>
            parameter.in === "header" &&
            requiredString(
              parameter.name,
              `${file} ${method} ${path} parameter name`,
            ).toLowerCase() === "idempotency-key",
        );
        if (hasIdempotencyKey) {
          bindings.push({
            operationId: requiredString(
              operation.operationId,
              `${file} ${method} ${path} operationId`,
            ),
            method,
            path,
          });
        }
      }
    }
  }
  if (
    bindings.length !== 1 ||
    bindings[0]?.operationId !== operationId ||
    bindings[0]?.method !== "post" ||
    bindings[0]?.path !== "/v1/tenants/{tenantId}/projects"
  ) {
    throw new Error(
      `P1-A1 must bind exactly one idempotent HTTP mutation: post /v1/tenants/{tenantId}/projects (${operationId}).`,
    );
  }

  const projectionFile = schemaFiles.find((file) =>
    file.endsWith("/managed-agent-create-project-idempotency-projection.schema.json"),
  );
  if (!projectionFile) throw new Error("P1-A1 idempotency projection schema is missing.");
  const schema = parseJsonFile(projectionFile);
  const properties = requiredRecord(schema.properties, `${projectionFile} properties`);
  const operationIdSchema = requiredRecord(properties.operationId, `${projectionFile} operationId`);
  if (operationIdSchema.const !== operationId) {
    throw new Error(`P1-A1 idempotency projection is not bound to ${operationId}.`);
  }
}

function validateJsonSchemaFixtures(
  schemaFiles: ReadonlyArray<string>,
  manifestFiles: ReadonlyArray<string>,
  contractRoot: string,
): number {
  const ajv = new Ajv2020({
    $data: false,
    allErrors: true,
    strict: true,
    validateFormats: true,
  });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const keyword of [
    "x-cloud-agents-canonicalization",
    "x-cloud-agents-normalization",
    "x-cloud-agents-security",
    "x-cloud-agents-semantic-constraints",
  ]) {
    ajv.addKeyword(keyword);
  }
  const schemas = new Map<string, JsonRecord>();
  for (const file of schemaFiles) {
    const schema = parseJsonFile(file);
    schemas.set(file, schema);
    ajv.addSchema(schema);
  }

  let count = 0;
  const validateManifest = ajv.getSchema(FIXTURE_MANIFEST_SCHEMA_ID);
  if (!validateManifest) throw new Error(`Ajv did not register ${FIXTURE_MANIFEST_SCHEMA_ID}.`);
  for (const manifestFile of manifestFiles) {
    const manifest = parseJsonFile(manifestFile);
    if (!validateManifest(manifest)) {
      throw new Error(
        `${manifestFile} violates fixture manifest schema: ${ajv.errorsText(validateManifest.errors)}.`,
      );
    }
    const cases = requiredArray(manifest.cases, `${manifestFile} cases`);
    for (const [index, caseValue] of cases.entries()) {
      count += 1;
      const fixture = requiredRecord(caseValue, `${manifestFile} cases/${index}`);
      const name = requiredString(fixture.name, `${manifestFile} cases/${index}/name`);
      const schemaFile = resolve(
        dirname(manifestFile),
        requiredString(fixture.schema, `${manifestFile} ${name} schema`),
      );
      if (!isWithin(contractRoot, schemaFile))
        throw new Error(`${name} schema escapes contracts/.`);
      const schema = schemas.get(schemaFile);
      if (!schema) throw new Error(`${name} references an unregistered schema ${schemaFile}.`);
      const identifier = requiredString(schema.$id, `${schemaFile} $id`);
      const validate = ajv.getSchema(identifier);
      if (!validate) throw new Error(`Ajv did not register ${identifier}.`);

      const documentFile = resolve(
        dirname(manifestFile),
        requiredString(fixture.document ?? fixture.instance, `${manifestFile} ${name} instance`),
      );
      const document = parseJsonFile(documentFile);
      const instance =
        fixture.instancePointer === undefined
          ? document
          : resolveJsonPointer(
              document,
              requiredString(fixture.instancePointer, `${manifestFile} ${name} instancePointer`),
            );
      const actualSchemaValid = validate(instance) as boolean;
      if (actualSchemaValid !== fixture.expectedSchemaValid) {
        throw new Error(
          `${name} expected schema valid=${String(fixture.expectedSchemaValid)}, got ${String(actualSchemaValid)}: ${ajv.errorsText(validate.errors)}.`,
        );
      }
      if (!actualSchemaValid) {
        assertExpectedSchemaError(
          validate.errors ?? [],
          requiredString(fixture.expectedError, `${manifestFile} ${name} expectedError`),
          identifier,
          name,
        );
      }
      if (fixture.expectedSemanticValid !== undefined) {
        const canonicalResults = [
          validateCanonicalNamespaceRefFixture(document),
          validateCanonicalSubjectRefFixture(document),
          validateManagedAgentCreateProjectIdempotencyFixture(document),
        ];
        const semanticResult =
          canonicalResults.find((result) => !result.valid) ??
          validatePlatformSemantics(instance, document);
        assertExpectedSemanticResult(
          semanticResult,
          fixture.expectedSemanticValid as boolean,
          fixture.expectedError,
        );
      }
    }
  }
  return count;
}

function assertExpectedSchemaError(
  errors: ReadonlyArray<ErrorObject>,
  expected: string,
  schemaId: string,
  fixtureName: string,
): void {
  const matches = errors.some((error) => {
    switch (expected) {
      case "INVALID_IDEMPOTENCY_KEY":
        return (
          error.instancePath === "/key" &&
          ["maxLength", "minLength", "pattern"].includes(error.keyword)
        );
      case "INVALID_NAMESPACE_REF_GRAMMAR":
        return (
          error.keyword === "pattern" &&
          (error.instancePath.endsWith("/kind") || error.instancePath.endsWith("/namespace"))
        );
      case "PROBLEM_SECRET_FIELD_FORBIDDEN":
        return (
          schemaId.endsWith("/problem.schema.json") && error.keyword === "additionalProperties"
        );
      case "ROLE_SCOPE_MISMATCH":
        return (
          error.instancePath.startsWith("/spec/scope") &&
          ["const", "if", "oneOf"].includes(error.keyword)
        );
      case "SCOPE_KIND_MISMATCH":
        return error.instancePath.endsWith("/ref/kind") && error.keyword === "const";
      case "UNKNOWN_FIELD":
        return error.keyword === "additionalProperties";
      case "UNKNOWN_ROLE":
        return error.instancePath === "/spec/roleName" && error.keyword === "enum";
      case "WILDCARD_PERMISSION_FORBIDDEN":
        return error.instancePath.startsWith("/spec/permissions/") && error.keyword === "pattern";
      default:
        return false;
    }
  });
  if (!matches) {
    const observed = errors
      .map((error) => `${error.keyword}@${error.instancePath || "/"}`)
      .join(", ");
    throw new Error(
      `${fixtureName} expected schema error ${expected}, got ${observed || "NO_ERROR"}.`,
    );
  }
}

export function validateJsonSchemaDocument(document: JsonRecord, file: string): void {
  if (document.$schema !== JSON_SCHEMA_2020_12) {
    throw new Error(`${file} must declare JSON Schema 2020-12.`);
  }
  requiredString(document.$id, `${file} $id`);
  visit(document, (value, pointer) => {
    if (value.type === "object" && value.additionalProperties !== false) {
      throw new Error(`${file}${pointer} object schema must set additionalProperties=false.`);
    }
  });
}

export function validateOpenApiDocument(
  document: JsonRecord,
  file: string,
  context: OpenApiValidationContext = {},
): number {
  if (document.openapi !== OPENAPI_VERSION) {
    throw new Error(`${file} must use OpenAPI ${OPENAPI_VERSION}.`);
  }
  if (document.jsonSchemaDialect !== JSON_SCHEMA_2020_12) {
    throw new Error(`${file} must use JSON Schema 2020-12 dialect.`);
  }
  const components = optionalRecord(document.components, `${file} components`);
  if (components.schemas !== undefined) {
    throw new Error(`${file} must not define components.schemas.`);
  }
  const paths = requiredRecord(document.paths, `${file} paths`);
  const operationIds = context.operationIds ?? new Set<string>();
  const operationCountBefore = operationIds.size;
  const securitySchemes = optionalRecord(
    components.securitySchemes,
    `${file} components/securitySchemes`,
  );
  validateSecurityRequirements(document.security, securitySchemes, `${file} security`, true);
  for (const [path, pathItemValue] of Object.entries(paths)) {
    if (!path.startsWith("/")) throw new Error(`${file} path ${path} must start with '/'.`);
    const pathItem = requiredRecord(pathItemValue, `${file} path ${path}`);
    const pathParameters = collectOpenApiParameters(
      pathItem.parameters,
      document,
      `${file} path ${path}`,
    );
    for (const [method, operationValue] of Object.entries(pathItem)) {
      if (!OPERATION_METHODS.has(method)) continue;
      const operation = requiredRecord(operationValue, `${file} ${method} ${path}`);
      const operationId = requiredString(
        operation.operationId,
        `${file} ${method} ${path} operationId`,
      );
      if (operationIds.has(operationId)) throw new Error(`${file} duplicates ${operationId}.`);
      operationIds.add(operationId);
      requiredRecord(operation.responses, `${file} ${operationId} responses`);
      validateSecurityRequirements(
        operation.security,
        securitySchemes,
        `${file} ${operationId} security`,
      );
      const operationParameters = collectOpenApiParameters(
        operation.parameters,
        document,
        `${file} ${operationId}`,
      );
      validatePathParameterBindings(path, [...pathParameters, ...operationParameters], {
        file,
        operationId,
      });
    }
  }
  visit(document, (value, pointer) => {
    const schema = value.schema;
    if (schema === undefined) return;
    const schemaObject = requiredRecord(schema, `${file}${pointer}/schema`);
    const keys = Object.keys(schemaObject);
    if (keys.length !== 1 || keys[0] !== "$ref") {
      throw new Error(`${file}${pointer}/schema must contain only an external $ref.`);
    }
    const reference = requiredString(schemaObject.$ref, `${file}${pointer}/schema/$ref`);
    if (reference.startsWith("#")) {
      throw new Error(`${file}${pointer}/schema must reference external JSON Schema authority.`);
    }
  });
  return operationIds.size - operationCountBefore;
}

function collectOpenApiParameters(
  value: unknown,
  document: JsonRecord,
  label: string,
): ReadonlyArray<JsonRecord> {
  if (value === undefined) return [];
  const entries = requiredArray(value, `${label} parameters`);
  const parameters: JsonRecord[] = [];
  const identities = new Set<string>();
  for (const [index, entry] of entries.entries()) {
    let parameter = requiredRecord(entry, `${label} parameters/${index}`);
    if (parameter.$ref !== undefined) {
      const reference = requiredString(parameter.$ref, `${label} parameters/${index}/$ref`);
      if (!reference.startsWith("#/")) {
        throw new Error(`${label} parameter references must be local OpenAPI component refs.`);
      }
      parameter = requiredRecord(
        resolveJsonPointer(document, decodeOpenApiFragment(reference)),
        `${label} parameters/${index} target`,
      );
    }
    const location = requiredString(parameter.in, `${label} parameters/${index}/in`);
    const name = requiredString(parameter.name, `${label} parameters/${index}/name`);
    const identity = `${location}\0${name}`;
    if (identities.has(identity)) {
      throw new Error(`${label} duplicates ${location} parameter ${name}.`);
    }
    identities.add(identity);
    parameters.push(parameter);
  }
  return parameters;
}

function validatePathParameterBindings(
  path: string,
  parameters: ReadonlyArray<JsonRecord>,
  context: { readonly file: string; readonly operationId: string },
): void {
  const templateNames = new Set([...path.matchAll(/\{([^{}]+)\}/gu)].map((match) => match[1]!));
  const bound = new Set<string>();
  for (const parameter of parameters) {
    if (parameter.in !== "path") continue;
    const name = requiredString(parameter.name, `${context.file} ${context.operationId} path name`);
    if (parameter.required !== true) {
      throw new Error(
        `${context.file} ${context.operationId} path parameter ${name} must be required.`,
      );
    }
    if (!templateNames.has(name)) {
      throw new Error(
        `${context.file} ${context.operationId} declares path parameter ${name} absent from ${path}.`,
      );
    }
    if (bound.has(name)) {
      throw new Error(`${context.file} ${context.operationId} duplicates path parameter ${name}.`);
    }
    bound.add(name);
  }
  for (const name of templateNames) {
    if (!bound.has(name)) {
      throw new Error(
        `${context.file} ${context.operationId} does not bind path parameter ${name}.`,
      );
    }
  }
}

function validateSecurityRequirements(
  value: unknown,
  securitySchemes: JsonRecord,
  label: string,
  required = false,
): void {
  if (value === undefined) {
    if (required) throw new Error(`${label} is required and must fail closed.`);
    return;
  }
  const requirements = requiredArray(value, label);
  if (requirements.length === 0) throw new Error(`${label} must not allow anonymous access.`);
  for (const [index, entry] of requirements.entries()) {
    const requirement = requiredRecord(entry, `${label}/${index}`);
    if (Object.keys(requirement).length === 0) {
      throw new Error(`${label}/${index} must not allow anonymous access.`);
    }
    for (const [scheme, scopes] of Object.entries(requirement)) {
      if (!Object.hasOwn(securitySchemes, scheme)) {
        throw new Error(`${label}/${index} references missing security scheme ${scheme}.`);
      }
      if (!Array.isArray(scopes) || !scopes.every((scope) => typeof scope === "string")) {
        throw new Error(`${label}/${index}/${scheme} scopes must be a string array.`);
      }
    }
  }
}

function decodeOpenApiFragment(reference: string): string {
  const fragment = reference.slice(1);
  try {
    return decodeURIComponent(fragment);
  } catch {
    throw new Error(`Invalid percent encoding in OpenAPI reference ${reference}.`);
  }
}

export function validateProtoSource(source: string, file: string): void {
  const uncommented = stripProtoComments(source, file);
  if (!/^\s*syntax\s*=\s*"proto3"\s*;/mu.test(uncommented)) {
    throw new Error(`${file} must declare proto3 syntax.`);
  }
  if (!/^\s*package\s+cloudagents\.[a-z]+\.v1alpha1\s*;/mu.test(uncommented)) {
    throw new Error(`${file} must declare a Cloud Agents v1alpha1 package.`);
  }
  if (
    !/^\s*option\s+go_package\s*=\s*"github\.com\/hxp0618\/cloud-agents\/sdk\/go\/gen\//mu.test(
      uncommented,
    )
  ) {
    throw new Error(`${file} must generate only into the public Go SDK.`);
  }
  if (braceBalance(maskQuotedText(uncommented)) !== 0) {
    throw new Error(`${file} has unbalanced braces.`);
  }
  if (/\bsyntax\s*=\s*"proto2"/u.test(uncommented)) {
    throw new Error(`${file} must not use proto2.`);
  }
  validateProtoFieldNumbers(uncommented, file);
}

function validateProtoContractTree(
  protoFiles: ReadonlyArray<string>,
  root: string,
  contractRoot: string,
): void {
  const sources = protoFiles.map((file) =>
    parseProtoContractSource(readFileSync(file, "utf8"), relativePath(root, file)),
  );
  validateProtoTypeGraph(sources);
  const symbols = new Set<string>([
    "google.protobuf.Timestamp",
    ...sources.flatMap((source) =>
      [...source.declarations].map((name) => `${source.packageName}.${name}`),
    ),
  ]);

  for (const descriptorFile of walkFiles(contractRoot).filter((file) =>
    file.endsWith("/fixtures/descriptor.golden.json"),
  )) {
    validateProtoDescriptorExpectation(
      parseJsonFile(descriptorFile),
      descriptorFile,
      sources,
      root,
    );
  }
  for (const fixtureFile of walkFiles(contractRoot).filter(
    (file) => file.endsWith("/fixtures/golden.json") || file.endsWith("/fixtures/negative.json"),
  )) {
    validateProtoFixtureMessages(parseJsonFile(fixtureFile), fixtureFile, symbols, sources);
  }
}

export function validateProtoSourceSet(sourceSet: Readonly<Record<string, string>>): void {
  const sources = Object.entries(sourceSet).map(([file, source]) => {
    validateProtoSource(source, file);
    return parseProtoContractSource(source, file);
  });
  validateProtoTypeGraph(sources);
}

function validateProtoTypeGraph(sources: ReadonlyArray<ProtoContractFile>): void {
  const byImportPath = new Map(sources.map((source) => [source.relativeFile, source]));
  const symbols = new Set<string>([
    "google.protobuf.Timestamp",
    ...sources.flatMap((source) =>
      [...source.declarations].map((name) => `${source.packageName}.${name}`),
    ),
  ]);
  for (const source of sources) {
    const availableSymbols = new Set<string>(
      [...source.declarations].map((name) => `${source.packageName}.${name}`),
    );
    for (const importPath of source.imports) {
      if (importPath.startsWith("google/protobuf/")) {
        if (importPath !== "google/protobuf/timestamp.proto") {
          throw new Error(
            `${source.relativeFile} imports unapproved well-known type ${importPath}.`,
          );
        }
        availableSymbols.add("google.protobuf.Timestamp");
        continue;
      }
      if (!importPath.startsWith("contracts/")) {
        throw new Error(`${source.relativeFile} import ${importPath} must stay under contracts/.`);
      }
      const imported = byImportPath.get(importPath);
      if (!imported)
        throw new Error(`${source.relativeFile} imports missing source ${importPath}.`);
      for (const name of imported.declarations) {
        availableSymbols.add(`${imported.packageName}.${name}`);
      }
    }
    for (const reference of source.typeReferences) {
      if (PROTO_BUILTIN_TYPES.has(reference)) continue;
      const normalized = reference.startsWith(".") ? reference.slice(1) : reference;
      if (normalized.includes(".")) {
        if (!symbols.has(normalized) || !availableSymbols.has(normalized)) {
          throw new Error(
            `${source.relativeFile} references unavailable Proto type ${reference}; add its source import.`,
          );
        }
        continue;
      }
      const local = `${source.packageName}.${normalized}`;
      const importedMatches = [...availableSymbols].filter((symbol) =>
        symbol.endsWith(`.${normalized}`),
      );
      if (!availableSymbols.has(local) && importedMatches.length !== 1) {
        throw new Error(`${source.relativeFile} references unknown Proto type ${reference}.`);
      }
    }
  }
}

const PROTO_BUILTIN_TYPES = new Set([
  "bool",
  "bytes",
  "double",
  "fixed32",
  "fixed64",
  "float",
  "int32",
  "int64",
  "sfixed32",
  "sfixed64",
  "sint32",
  "sint64",
  "string",
  "uint32",
  "uint64",
]);

type ProtoContractFile = {
  readonly relativeFile: string;
  readonly packageName: string;
  readonly imports: ReadonlyArray<string>;
  readonly declarations: ReadonlySet<string>;
  readonly messages: ReadonlyMap<string, ReadonlyArray<ProtoFieldDefinition>>;
  readonly services: ReadonlyMap<string, ReadonlyArray<ProtoRpcSignature>>;
  readonly typeReferences: ReadonlyArray<string>;
};

type ProtoFieldDefinition = {
  readonly name: string;
  readonly number: number;
  readonly repeated: boolean;
  readonly type: string;
};

type ProtoRpcSignature = {
  readonly name: string;
  readonly inputType: string;
  readonly outputType: string;
  readonly clientStreaming: boolean;
  readonly serverStreaming: boolean;
};

function parseProtoContractSource(rawSource: string, relativeFile: string): ProtoContractFile {
  const source = stripProtoComments(rawSource, relativeFile);
  const packageName = requiredRegexCapture(
    source,
    /\bpackage\s+(cloudagents\.[a-z]+\.v1alpha1)\s*;/u,
    `${relativeFile} package`,
  );
  const imports = [...source.matchAll(/\bimport\s+(?:(?:public|weak)\s+)?"([^"]+)"\s*;/gu)].map(
    (match) => match[1]!,
  );
  const declarations = new Set(
    [...source.matchAll(/\b(?:message|enum)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{/gu)].map(
      (match) => match[1]!,
    ),
  );
  const services = new Map<string, ReadonlyArray<ProtoRpcSignature>>();
  for (const block of protoNamedBlocks(source, "service", relativeFile)) {
    const methods = [
      ...block.body.matchAll(
        /\brpc\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(\s*(?:(stream)\s+)?([.]?[A-Za-z_][A-Za-z0-9_.]*)\s*\)\s*returns\s*\(\s*(?:(stream)\s+)?([.]?[A-Za-z_][A-Za-z0-9_.]*)\s*\)/gu,
      ),
    ].map((match) => ({
      name: match[1]!,
      clientStreaming: match[2] === "stream",
      inputType: match[3]!,
      serverStreaming: match[4] === "stream",
      outputType: match[5]!,
    }));
    if (services.has(block.name)) {
      throw new Error(`${relativeFile} duplicates service ${block.name}.`);
    }
    services.set(block.name, methods);
  }
  const typeReferences: string[] = [];
  const messages = new Map<string, ReadonlyArray<ProtoFieldDefinition>>();
  for (const block of protoNamedBlocks(source, "message", relativeFile)) {
    const body = maskNestedProtoTypeBlocks(block.body, relativeFile);
    const fields = protoFieldDefinitions(body);
    if (messages.has(block.name))
      throw new Error(`${relativeFile} duplicates message ${block.name}.`);
    messages.set(block.name, fields);
    for (const field of fields) {
      const type = field.type;
      if (type.startsWith("map<")) {
        const entries = type.slice(4, -1).split(",");
        if (entries.length !== 2)
          throw new Error(`${relativeFile} has malformed map field type ${type}.`);
        typeReferences.push(entries[0]!, entries[1]!);
      } else {
        typeReferences.push(type);
      }
    }
  }
  for (const methods of services.values()) {
    for (const method of methods) {
      typeReferences.push(method.inputType, method.outputType);
    }
  }
  return {
    relativeFile,
    packageName,
    imports,
    declarations,
    messages,
    services,
    typeReferences,
  };
}

function validateProtoFieldNumbers(source: string, file: string): void {
  for (const block of protoNamedBlocks(source, "message", file)) {
    const body = maskNestedProtoTypeBlocks(block.body, file);
    const numbers = new Map<number, string>();
    const names = new Set<string>();
    for (const field of protoFieldDefinitions(body)) {
      const { name, number } = field;
      if (!Number.isSafeInteger(number) || number < 1 || number > 536_870_911) {
        throw new Error(`${file} message ${block.name} has invalid field number ${number}.`);
      }
      if (number >= 19_000 && number <= 19_999) {
        throw new Error(`${file} message ${block.name} uses reserved field number ${number}.`);
      }
      const previous = numbers.get(number);
      if (previous) {
        throw new Error(
          `${file} message ${block.name} duplicates field number ${number}: ${previous}, ${name}.`,
        );
      }
      if (names.has(name))
        throw new Error(`${file} message ${block.name} duplicates field ${name}.`);
      numbers.set(number, name);
      names.add(name);
    }
  }
}

function protoFieldDefinitions(source: string): ReadonlyArray<ProtoFieldDefinition> {
  return [
    ...source.matchAll(
      /(?:^|(?<=[;{}]))\s*(?:(optional|required|repeated)\s+)?((?:\.?[A-Za-z_][A-Za-z0-9_.]*)|(?:map\s*<[^>]+>))\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(\d+)\s*(?:\[[^\]]*\])?\s*;/gmu,
    ),
  ].map((match) => ({
    repeated: match[1] === "repeated",
    type: match[2]!.replaceAll(/\s+/gu, ""),
    name: match[3]!,
    number: Number(match[4]),
  }));
}

function validateProtoDescriptorExpectation(
  descriptor: JsonRecord,
  descriptorFile: string,
  sources: ReadonlyArray<ProtoContractFile>,
  root: string,
): void {
  if (descriptor.generationStatus !== "NOT_GENERATED" || descriptor.authority !== "proto3") {
    throw new Error(`${descriptorFile} must remain explicit NOT_GENERATED Proto authority.`);
  }
  const packageName = requiredString(descriptor.package, `${descriptorFile} package`);
  const selected = sources.filter((source) => source.packageName === packageName);
  if (selected.length === 0)
    throw new Error(`${descriptorFile} names unknown package ${packageName}.`);
  const expectedSources = requiredArray(descriptor.sources, `${descriptorFile} sources`).map(
    (value) => requiredString(value, `${descriptorFile} source`),
  );
  const actualSources = selected.map((source) => source.relativeFile).toSorted();
  assertStringArrayEqual(expectedSources.toSorted(), actualSources, `${descriptorFile} sources`);
  const expectedImports = requiredArray(descriptor.imports, `${descriptorFile} imports`).map(
    (value) => requiredString(value, `${descriptorFile} import`),
  );
  const selectedSourcePaths = new Set(selected.map((source) => source.relativeFile));
  const actualImports = [
    ...new Set(
      selected
        .flatMap((source) => source.imports)
        .filter((value) => !selectedSourcePaths.has(value)),
    ),
  ].toSorted();
  assertStringArrayEqual(expectedImports.toSorted(), actualImports, `${descriptorFile} imports`);
  const expectedServices = requiredRecord(descriptor.services, `${descriptorFile} services`);
  const actualServices = new Map(
    selected.flatMap((source) =>
      [...source.services].map(([name, methods]) => [name, { methods, source }] as const),
    ),
  );
  if (
    Object.keys(expectedServices).toSorted().join("\0") !==
    [...actualServices.keys()].toSorted().join("\0")
  ) {
    throw new Error(`${descriptorFile} service names do not match Proto source.`);
  }
  for (const [service, methodsValue] of Object.entries(expectedServices)) {
    const actualService = actualServices.get(service);
    if (!actualService) throw new Error(`${descriptorFile} is missing service ${service}.`);
    const expectedMethods = requiredArray(methodsValue, `${descriptorFile} ${service}`);
    if (expectedMethods.length !== actualService.methods.length) {
      throw new Error(`${descriptorFile} ${service} method count does not match Proto source.`);
    }
    expectedMethods.forEach((value, index) => {
      const expected = requiredRecord(value, `${descriptorFile} ${service}/${index}`);
      assertKnownKeys(
        expected,
        ["clientStreaming", "input", "name", "output", "serverStreaming"],
        `${descriptorFile} ${service}/${index}`,
      );
      const actual = actualService.methods[index]!;
      const actualSignature = {
        name: actual.name,
        input: qualifyProtoReference(actual.inputType, actualService.source, sources),
        output: qualifyProtoReference(actual.outputType, actualService.source, sources),
        clientStreaming: actual.clientStreaming,
        serverStreaming: actual.serverStreaming,
      };
      if (
        new TextDecoder().decode(canonicalizeJson(expected)) !==
        new TextDecoder().decode(canonicalizeJson(actualSignature))
      ) {
        throw new Error(
          `${descriptorFile} ${service}/${index} signature mismatch: ${JSON.stringify(expected)} != ${JSON.stringify(actualSignature)}.`,
        );
      }
    });
  }
  const declaredMessages = new Set(selected.flatMap((source) => [...source.declarations]));
  for (const value of requiredArray(descriptor.requiredMessages, `${descriptorFile} messages`)) {
    const message = requiredString(value, `${descriptorFile} required message`);
    if (!declaredMessages.has(message)) {
      throw new Error(`${descriptorFile} requires missing message ${packageName}.${message}.`);
    }
  }
  if (!isWithin(resolve(root, "contracts"), descriptorFile)) {
    throw new Error(`${descriptorFile} escapes contracts/.`);
  }
}

function qualifyProtoReference(
  reference: string,
  source: ProtoContractFile,
  sources: ReadonlyArray<ProtoContractFile>,
): string {
  const normalized = reference.startsWith(".") ? reference.slice(1) : reference;
  if (normalized.includes(".")) return normalized;
  if (source.declarations.has(normalized)) return `${source.packageName}.${normalized}`;
  const imported = sources.filter(
    (candidate) =>
      source.imports.includes(candidate.relativeFile) && candidate.declarations.has(normalized),
  );
  if (imported.length === 1) return `${imported[0]!.packageName}.${normalized}`;
  throw new Error(`${source.relativeFile} cannot qualify Proto reference ${reference}.`);
}

const PROTO_FIXTURE_ERROR_CODES = new Set([
  "canonical_registration_digest_mismatch",
  "canonical_registration_digest_required",
  "canonical_request_digest_mismatch",
  "canonical_request_digest_required",
  "capability_descriptor_mismatch",
  "capability_not_negotiated",
  "deadline_exceeded",
  "deadline_required",
  "durable_receipt_contains_secret",
  "idempotency_conflict",
  "invalid_adapter_endpoint",
  "invalid_namespace_ref",
  "mtls_identity_mismatch",
  "mtls_identity_required",
  "negotiated_version_mismatch",
  "negotiation_expired",
  "negotiation_required",
  "operation_id_required",
  "payload_too_large",
  "registration_idempotency_conflict",
  "registration_receipt_digest_mismatch",
  "repeated_field_limit_exceeded",
  "stale_generation",
  "unknown_required_capability",
  "unsupported_protocol_version",
  "wire_message_too_large",
]);

function validateProtoFixtureMessages(
  fixture: JsonRecord,
  fixtureFile: string,
  symbols: ReadonlySet<string>,
  sources: ReadonlyArray<ProtoContractFile>,
): void {
  assertKnownKeys(
    fixture,
    [
      "cases",
      "fixtureVersion",
      "sharedMessageAuthority",
      "sharedNegativeCases",
      "transport",
      "wireAuthority",
    ],
    fixtureFile,
  );
  if (fixture.fixtureVersion !== 1) throw new Error(`${fixtureFile} fixtureVersion must be 1.`);
  const messageRegistry = protoMessageRegistry(sources);
  const serviceRegistry = protoServiceRegistry(sources);
  const ids = new Set<string>();
  for (const [index, value] of requiredArray(fixture.cases, `${fixtureFile} cases`).entries()) {
    const entry = requiredRecord(value, `${fixtureFile} cases/${index}`);
    assertKnownKeys(
      entry,
      [
        "admissionState",
        "contractMessage",
        "expected",
        "id",
        "protoJson",
        "rpcService",
        "semanticInput",
        "syntheticProtoValue",
        "transportEvidence",
      ],
      `${fixtureFile} cases/${index}`,
    );
    const id = requiredString(entry.id, `${fixtureFile} cases/${index}/id`);
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(id)) {
      throw new Error(`${fixtureFile} cases/${index} has invalid id ${id}.`);
    }
    if (ids.has(id)) throw new Error(`${fixtureFile} duplicates fixture id ${id}.`);
    ids.add(id);
    const message = requiredString(
      entry.contractMessage,
      `${fixtureFile} cases/${index}/contractMessage`,
    );
    if (!symbols.has(message) || !messageRegistry.has(message)) {
      throw new Error(`${fixtureFile} references missing message ${message}.`);
    }
    if (entry.protoJson !== undefined) {
      validateProtoJsonMessage(
        requiredRecord(entry.protoJson, `${fixtureFile} cases/${index}/protoJson`),
        message,
        messageRegistry,
        `${fixtureFile} cases/${index}/protoJson`,
      );
    }
    if (entry.rpcService !== undefined) {
      const serviceName = requiredString(
        entry.rpcService,
        `${fixtureFile} cases/${index}/rpcService`,
      );
      const methods = serviceRegistry.get(serviceName);
      if (!methods) throw new Error(`${fixtureFile} references missing service ${serviceName}.`);
      if (!methods.some((method) => method.input === message || method.output === message)) {
        throw new Error(`${fixtureFile} ${id} message ${message} is not bound to ${serviceName}.`);
      }
    }
    if (entry.transportEvidence !== undefined) {
      assertKnownKeys(
        requiredRecord(entry.transportEvidence, `${fixtureFile} ${id} transportEvidence`),
        ["authenticatedClientSpiffeId", "authenticatedServerSpiffeId", "tls"],
        `${fixtureFile} ${id} transportEvidence`,
      );
    }
    if (entry.admissionState !== undefined) {
      assertKnownKeys(
        requiredRecord(entry.admissionState, `${fixtureFile} ${id} admissionState`),
        [
          "authenticatedClientSpiffeId",
          "authenticatedServerSpiffeId",
          "capabilityRegistered",
          "currentGeneration",
          "fencingTokenValid",
          "hardMaxRepeatedItems",
          "hardMaxWireMessageBytes",
          "idempotencyRecord",
          "negotiatedCapabilities",
          "negotiatedMaxPayloadBytes",
          "now",
          "registrationIdempotencyKey",
          "registrationRecord",
          "selectedVersion",
          "storedCanonicalRegistrationDigest",
        ],
        `${fixtureFile} ${id} admissionState`,
      );
    }
    const expected = requiredRecord(entry.expected, `${fixtureFile} cases/${index}/expected`);
    assertKnownKeys(
      expected,
      [
        "canonicalRegistrationDigest",
        "canonicalRegistrationUtf8",
        "canonicalRequestDigest",
        "canonicalRequestUtf8",
        "canonicalUtf8",
        "code",
        "containsRawFencingToken",
        "containsSecret",
        "decision",
        "digest",
        "expiresAt",
        "negotiationId",
        "normalizedProtoJson",
        "registrationId",
        "selectedVersion",
        "sideEffect",
        "urn",
      ],
      `${fixtureFile} cases/${index}/expected`,
    );
    const decision = requiredString(expected.decision, `${fixtureFile} cases/${index}/decision`);
    if (decision !== "ACCEPT" && !decision.startsWith("REJECT")) {
      throw new Error(`${fixtureFile} cases/${index} has invalid decision ${decision}.`);
    }
    if (decision.startsWith("REJECT")) {
      const code = requiredString(expected.code, `${fixtureFile} cases/${index}/expected/code`);
      if (!PROTO_FIXTURE_ERROR_CODES.has(code)) {
        throw new Error(`${fixtureFile} ${id} uses unknown stable error code ${code}.`);
      }
    } else if (expected.code !== undefined) {
      throw new Error(`${fixtureFile} ${id} accepted fixture must not carry an error code.`);
    }
    if (expected.canonicalUtf8 !== undefined || expected.canonicalJson !== undefined) {
      const instance = expected.normalizedProtoJson ?? entry.protoJson ?? entry.semanticInput;
      validateCanonicalExpectation(expected, `${fixtureFile} cases/${index}`, instance);
    }
    validateOperationCanonicalFixture(entry, expected, `${fixtureFile} cases/${index}`);
    validateRegistrationCanonicalFixture(entry, expected, `${fixtureFile} cases/${index}`);
    validateProtoNegativeFixtureSemantics(entry, expected, `${fixtureFile} cases/${index}`);
  }
}

type ProtoMessageShape = {
  readonly fields: ReadonlyMap<string, ProtoFieldDefinition>;
  readonly source: ProtoContractFile;
};

type QualifiedProtoRpc = {
  readonly name: string;
  readonly input: string;
  readonly output: string;
};

function protoMessageRegistry(
  sources: ReadonlyArray<ProtoContractFile>,
): ReadonlyMap<string, ProtoMessageShape> {
  const registry = new Map<string, ProtoMessageShape>();
  for (const source of sources) {
    for (const [name, fields] of source.messages) {
      const qualified = `${source.packageName}.${name}`;
      registry.set(qualified, {
        source,
        fields: new Map(fields.map((field) => [protoJsonFieldName(field.name), field])),
      });
    }
  }
  return registry;
}

function protoServiceRegistry(
  sources: ReadonlyArray<ProtoContractFile>,
): ReadonlyMap<string, ReadonlyArray<QualifiedProtoRpc>> {
  return new Map(
    sources.flatMap((source) =>
      [...source.services].map(
        ([service, methods]) =>
          [
            `${source.packageName}.${service}`,
            methods.map((method) => ({
              name: method.name,
              input: qualifyProtoReference(method.inputType, source, sources),
              output: qualifyProtoReference(method.outputType, source, sources),
            })),
          ] as const,
      ),
    ),
  );
}

function validateProtoJsonMessage(
  value: JsonRecord,
  messageName: string,
  registry: ReadonlyMap<string, ProtoMessageShape>,
  label: string,
): void {
  const shape = registry.get(messageName);
  if (!shape) throw new Error(`${label} references missing message shape ${messageName}.`);
  for (const [name, fieldValue] of Object.entries(value)) {
    const field = shape.fields.get(name);
    if (!field) throw new Error(`${label} has unknown ProtoJSON field ${name}.`);
    if (field.repeated) {
      if (!Array.isArray(fieldValue)) throw new Error(`${label}/${name} must be an array.`);
      fieldValue.forEach((entry, index) =>
        validateProtoJsonValue(
          entry,
          field.type,
          shape.source,
          registry,
          `${label}/${name}/${index}`,
        ),
      );
    } else {
      validateProtoJsonValue(fieldValue, field.type, shape.source, registry, `${label}/${name}`);
    }
  }
}

function validateProtoJsonValue(
  value: unknown,
  type: string,
  source: ProtoContractFile,
  registry: ReadonlyMap<string, ProtoMessageShape>,
  label: string,
): void {
  const normalized = type.startsWith(".") ? type.slice(1) : type;
  if (normalized.startsWith("map<")) {
    requiredRecord(value, label);
    return;
  }
  if (normalized === "google.protobuf.Timestamp") {
    requiredString(value, label);
    return;
  }
  if (normalized === "bytes") {
    if (typeof value !== "string") throw new Error(`${label} must be base64 ProtoJSON bytes.`);
    const text = value;
    if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/u.test(text)) {
      throw new Error(`${label} must be canonical base64 ProtoJSON bytes.`);
    }
    return;
  }
  if (normalized === "string") {
    if (typeof value !== "string") throw new Error(`${label} must be a string.`);
    return;
  }
  if (normalized === "bool") {
    if (typeof value !== "boolean") throw new Error(`${label} must be a boolean.`);
    return;
  }
  if (["fixed64", "int64", "sfixed64", "sint64", "uint64"].includes(normalized)) {
    if (typeof value !== "string" || !/^-?(?:0|[1-9][0-9]*)$/u.test(value)) {
      throw new Error(`${label} must be a decimal-string ProtoJSON 64-bit integer.`);
    }
    return;
  }
  if (
    ["double", "fixed32", "float", "int32", "sfixed32", "sint32", "uint32"].includes(normalized)
  ) {
    if (typeof value !== "number" || !Number.isFinite(value)) {
      throw new Error(`${label} must be a finite ProtoJSON number.`);
    }
    return;
  }
  const qualified = resolveProtoMessageReference(normalized, source, registry);
  if (qualified) {
    validateProtoJsonMessage(requiredRecord(value, label), qualified, registry, label);
    return;
  }
  if (typeof value !== "string" && !Number.isInteger(value)) {
    throw new Error(`${label} must be a ProtoJSON enum name or integer.`);
  }
}

function resolveProtoMessageReference(
  reference: string,
  source: ProtoContractFile,
  registry: ReadonlyMap<string, ProtoMessageShape>,
): string | undefined {
  if (reference.includes(".")) return registry.has(reference) ? reference : undefined;
  const local = `${source.packageName}.${reference}`;
  if (registry.has(local)) return local;
  const candidates = [...registry.keys()].filter((name) => name.endsWith(`.${reference}`));
  return candidates.length === 1 ? candidates[0] : undefined;
}

function protoJsonFieldName(name: string): string {
  return name.replaceAll(/_([a-z])/gu, (_match, letter: string) => letter.toUpperCase());
}

function validateProtoNegativeFixtureSemantics(
  entry: JsonRecord,
  expected: JsonRecord,
  label: string,
): void {
  const code = typeof expected.code === "string" ? expected.code : undefined;
  if (!code) return;
  const protoJson = isRecord(entry.protoJson) ? entry.protoJson : undefined;
  const synthetic = isRecord(entry.syntheticProtoValue) ? entry.syntheticProtoValue : undefined;
  const cause = protoJson ?? synthetic;
  const admission = isRecord(entry.admissionState) ? entry.admissionState : undefined;
  const transport = isRecord(entry.transportEvidence) ? entry.transportEvidence : undefined;
  const operation = protoOperation(protoJson);
  switch (code) {
    case "invalid_namespace_ref": {
      const semantic = protoJson ? validatePlatformSemantics(protoJson) : undefined;
      const syntheticLength = synthetic?.idUnicodeCodePoints;
      assertNegativeCause(
        semantic?.valid === false ||
          (typeof syntheticLength === "number" && (syntheticLength < 1 || syntheticLength > 256)),
        label,
        code,
      );
      break;
    }
    case "unsupported_protocol_version": {
      const versions = Array.isArray(protoJson?.supportedVersions)
        ? protoJson.supportedVersions
        : [];
      assertNegativeCause(
        versions.length > 0 && versions.every((value) => !isRecord(value) || value.major !== 1),
        label,
        code,
      );
      break;
    }
    case "unknown_required_capability":
      assertNegativeCause(containsUnknownCapability(cause), label, code);
      break;
    case "negotiation_required":
      assertNegativeCause(findRecordWithKey(cause, "negotiation") === undefined, label, code);
      break;
    case "negotiation_expired": {
      const negotiation = findRecordWithKey(cause, "negotiation");
      assertNegativeCause(
        validDate(negotiation?.expiresAt) <= validDate(admission?.now),
        label,
        code,
      );
      break;
    }
    case "negotiated_version_mismatch": {
      const negotiation = findRecordWithKey(cause, "negotiation");
      assertNegativeCause(
        !sameProtocolVersion(negotiation?.protocolVersion, admission?.selectedVersion),
        label,
        code,
      );
      break;
    }
    case "capability_not_negotiated": {
      const required = requiredCapabilities(cause);
      const negotiated = new Set(
        Array.isArray(admission?.negotiatedCapabilities)
          ? admission.negotiatedCapabilities.filter(
              (value): value is string => typeof value === "string",
            )
          : [],
      );
      assertNegativeCause(
        required.some((value) => !negotiated.has(value)),
        label,
        code,
      );
      break;
    }
    case "stale_generation": {
      const fencing = findRecordWithKey(protoJson, "fencing");
      assertNegativeCause(
        typeof fencing?.generation === "string" &&
          typeof admission?.currentGeneration === "string" &&
          fencing.generation !== admission.currentGeneration,
        label,
        code,
      );
      break;
    }
    case "operation_id_required":
      assertNegativeCause(
        typeof operation?.operationId !== "string" || operation.operationId === "",
        label,
        code,
      );
      break;
    case "deadline_required":
      assertNegativeCause(
        typeof operation?.deadline !== "string" || operation.deadline === "",
        label,
        code,
      );
      break;
    case "deadline_exceeded":
      assertNegativeCause(validDate(operation?.deadline) <= validDate(admission?.now), label, code);
      break;
    case "durable_receipt_contains_secret":
      assertNegativeCause(
        typeof protoJson?.redactedSummary === "string" &&
          /(?:authorization\s*:\s*bearer|api[_-]?key|access[_-]?token|client[_-]?secret|password)/iu.test(
            protoJson.redactedSummary,
          ),
        label,
        code,
      );
      break;
    case "payload_too_large":
      assertNegativeCause(
        typeof synthetic?.declaredSizeBytes === "number" &&
          typeof admission?.negotiatedMaxPayloadBytes === "number" &&
          synthetic.declaredSizeBytes > admission.negotiatedMaxPayloadBytes,
        label,
        code,
      );
      break;
    case "idempotency_conflict":
      assertNegativeCause(
        admission?.idempotencyRecord === "PRESENT_WITH_DIFFERENT_DIGEST",
        label,
        code,
      );
      break;
    case "repeated_field_limit_exceeded":
      assertNegativeCause(
        typeof synthetic?.finalizerCount === "number" &&
          typeof admission?.hardMaxRepeatedItems === "number" &&
          synthetic.finalizerCount > admission.hardMaxRepeatedItems,
        label,
        code,
      );
      break;
    case "wire_message_too_large":
      assertNegativeCause(
        typeof synthetic?.serializedSizeBytes === "number" &&
          typeof admission?.hardMaxWireMessageBytes === "number" &&
          synthetic.serializedSizeBytes > admission.hardMaxWireMessageBytes,
        label,
        code,
      );
      break;
    case "mtls_identity_required": {
      const evidence = [transport, admission].filter(
        (value): value is JsonRecord => value !== undefined,
      );
      const missing =
        transport?.tls === "plaintext-h2c" ||
        evidence.some(
          (value) =>
            (Object.hasOwn(value, "authenticatedClientSpiffeId") &&
              value.authenticatedClientSpiffeId == null) ||
            (Object.hasOwn(value, "authenticatedServerSpiffeId") &&
              value.authenticatedServerSpiffeId == null),
        );
      assertNegativeCause(missing, label, code);
      break;
    }
    case "mtls_identity_mismatch": {
      const expectedClient =
        nestedString(protoJson?.expectedClientIdentity, "spiffeId") ??
        (typeof synthetic?.expectedAuthenticatedClientSpiffeId === "string"
          ? synthetic.expectedAuthenticatedClientSpiffeId
          : undefined);
      const expectedServer = nestedString(protoJson?.expectedServerIdentity, "spiffeId");
      const actualClient =
        transport?.authenticatedClientSpiffeId ?? admission?.authenticatedClientSpiffeId;
      const actualServer =
        transport?.authenticatedServerSpiffeId ?? admission?.authenticatedServerSpiffeId;
      assertNegativeCause(
        (typeof expectedClient === "string" && actualClient !== expectedClient) ||
          (typeof expectedServer === "string" && actualServer !== expectedServer),
        label,
        code,
      );
      break;
    }
    case "capability_descriptor_mismatch": {
      const protocol = isRecord(protoJson?.protocol) ? protoJson.protocol : undefined;
      assertNegativeCause(
        !sameStringSet(protocol?.capabilities, protoJson?.capabilities),
        label,
        code,
      );
      break;
    }
    case "invalid_adapter_endpoint": {
      const endpoint = isRecord(protoJson?.endpoint) ? protoJson.endpoint : undefined;
      assertNegativeCause(!validAdapterEndpoint(endpoint?.connectUri), label, code);
      break;
    }
    case "registration_idempotency_conflict":
      assertNegativeCause(
        typeof admission?.storedCanonicalRegistrationDigest === "string" &&
          admission?.registrationIdempotencyKey === protoJson?.registrationIdempotencyKey,
        label,
        code,
      );
      break;
    case "registration_receipt_digest_mismatch": {
      const supplied =
        typeof protoJson?.canonicalRegistrationSha256 === "string"
          ? base64DigestToText(protoJson.canonicalRegistrationSha256, `${label} digest`)
          : undefined;
      assertNegativeCause(
        admission?.registrationRecord === "PRESENT_WITH_DIFFERENT_DIGEST" &&
          typeof admission?.storedCanonicalRegistrationDigest === "string" &&
          supplied !== admission.storedCanonicalRegistrationDigest,
        label,
        code,
      );
      break;
    }
    case "canonical_request_digest_mismatch":
    case "canonical_request_digest_required":
    case "canonical_registration_digest_mismatch":
    case "canonical_registration_digest_required":
      break;
    default:
      throw new Error(`${label} has no cause validator for stable code ${code}.`);
  }
}

function assertNegativeCause(condition: boolean, label: string, code: string): void {
  if (!condition) throw new Error(`${label} does not demonstrate expected cause ${code}.`);
}

function protoOperation(value: JsonRecord | undefined): JsonRecord | undefined {
  if (!value) return undefined;
  const attempt = isRecord(value.attempt) ? value.attempt : value;
  return isRecord(attempt.operation)
    ? attempt.operation
    : Object.hasOwn(value, "operationId") || Object.hasOwn(value, "idempotencyKey")
      ? value
      : undefined;
}

function containsUnknownCapability(value: unknown): boolean {
  if (Array.isArray(value)) return value.some((entry) => containsUnknownCapability(entry));
  if (!isRecord(value)) return false;
  for (const [key, entry] of Object.entries(value)) {
    if (key === "capabilities" || key === "requiredCapabilities" || key === "requiredCapability") {
      const entries = Array.isArray(entry) ? entry : [entry];
      if (
        entries.some(
          (capability) => typeof capability !== "string" || !CAPABILITY_NUMBERS.has(capability),
        )
      ) {
        return true;
      }
    }
    if (containsUnknownCapability(entry)) return true;
  }
  return false;
}

function requiredCapabilities(value: unknown): ReadonlyArray<string> {
  const result: string[] = [];
  if (Array.isArray(value)) {
    value.forEach((entry) => result.push(...requiredCapabilities(entry)));
  } else if (isRecord(value)) {
    for (const [key, entry] of Object.entries(value)) {
      if (key === "requiredCapabilities" && Array.isArray(entry)) {
        result.push(...entry.filter((item): item is string => typeof item === "string"));
      } else if (key === "requiredCapability" && typeof entry === "string") {
        result.push(entry);
      } else {
        result.push(...requiredCapabilities(entry));
      }
    }
  }
  return result;
}

function sameProtocolVersion(left: unknown, right: unknown): boolean {
  return (
    isRecord(left) &&
    isRecord(right) &&
    typeof left.major === "number" &&
    typeof left.minor === "number" &&
    left.major === right.major &&
    left.minor === right.minor
  );
}

function validDate(value: unknown): number {
  if (typeof value !== "string") return Number.NaN;
  return Date.parse(value);
}

function nestedString(value: unknown, key: string): string | undefined {
  return isRecord(value) && typeof value[key] === "string" ? value[key] : undefined;
}

function sameStringSet(left: unknown, right: unknown): boolean {
  if (!Array.isArray(left) || !Array.isArray(right)) return false;
  const normalized = (value: ReadonlyArray<unknown>) =>
    [...new Set(value.filter((entry): entry is string => typeof entry === "string"))].toSorted();
  return normalized(left).join("\0") === normalized(right).join("\0");
}

function validAdapterEndpoint(value: unknown): boolean {
  if (typeof value !== "string") return false;
  try {
    const parsed = new URL(value);
    return (
      parsed.protocol === "https:" &&
      parsed.username === "" &&
      parsed.password === "" &&
      parsed.search === "" &&
      parsed.hash === ""
    );
  } catch {
    return false;
  }
}

function findRecordWithKey(value: unknown, key: string): JsonRecord | undefined {
  if (Array.isArray(value)) {
    for (const entry of value) {
      const found = findRecordWithKey(entry, key);
      if (found) return found;
    }
    return undefined;
  }
  if (!isRecord(value)) return undefined;
  if (isRecord(value[key])) return value[key] as JsonRecord;
  for (const child of Object.values(value)) {
    const found = findRecordWithKey(child, key);
    if (found) return found;
  }
  return undefined;
}

function validateOperationCanonicalFixture(
  entry: JsonRecord,
  expected: JsonRecord,
  label: string,
): void {
  const protoJson = isRecord(entry.protoJson) ? entry.protoJson : undefined;
  if (!protoJson) return;
  const attempt = isRecord(protoJson.attempt) ? protoJson.attempt : protoJson;
  const operation = isRecord(attempt.operation)
    ? attempt.operation
    : typeof protoJson.operationId === "string"
      ? protoJson
      : undefined;
  if (!operation) return;
  const expectedCode = typeof expected.code === "string" ? expected.code : undefined;
  const suppliedDigest = operation.canonicalRequestSha256;
  if (expectedCode === "canonical_request_digest_required") {
    if (suppliedDigest !== undefined && suppliedDigest !== "") {
      throw new Error(
        `${label} must omit canonicalRequestSha256 for the required-digest negative.`,
      );
    }
    return;
  }
  const projection = operationCanonicalProjection(operation);
  if (!projection) return;
  const canonicalUtf8 = new TextDecoder().decode(canonicalizeJson(projection));
  const digest = canonicalJsonDigest(projection);
  const rawDigestBase64 = Buffer.from(digest.slice("sha256:".length), "hex").toString("base64");
  if (expectedCode === "canonical_request_digest_mismatch") {
    if (suppliedDigest === rawDigestBase64) {
      throw new Error(
        `${label} digest-mismatch negative unexpectedly contains the correct digest.`,
      );
    }
    return;
  }
  if (suppliedDigest !== undefined && suppliedDigest !== rawDigestBase64) {
    throw new Error(`${label} canonicalRequestSha256 does not match its operation projection.`);
  }
  if (expected.decision === "ACCEPT" && suppliedDigest !== rawDigestBase64) {
    throw new Error(`${label} accepted operation must carry its canonical request digest.`);
  }
  if (
    expected.canonicalRequestUtf8 !== undefined &&
    expected.canonicalRequestUtf8 !== canonicalUtf8
  ) {
    throw new Error(`${label} canonicalRequestUtf8 does not match its operation projection.`);
  }
  if (expected.canonicalRequestDigest !== undefined && expected.canonicalRequestDigest !== digest) {
    throw new Error(`${label} canonicalRequestDigest does not match its operation projection.`);
  }
}

const CAPABILITY_NUMBERS = new Map<string, number>([
  ["CAPABILITY_NEGOTIATION", 1],
  ["CAPABILITY_HEALTH", 2],
  ["CAPABILITY_OPERATION_DISPATCH", 3],
  ["CAPABILITY_DURABLE_RECEIPTS", 4],
  ["CAPABILITY_FINALIZERS", 5],
  ["CAPABILITY_ADAPTER_REGISTRATION", 6],
]);

function validateRegistrationCanonicalFixture(
  entry: JsonRecord,
  expected: JsonRecord,
  label: string,
): void {
  const protoJson = isRecord(entry.protoJson) ? entry.protoJson : undefined;
  if (!protoJson) return;
  const message = entry.contractMessage;
  const supplied = protoJson.canonicalRegistrationSha256;
  const expectedCode = typeof expected.code === "string" ? expected.code : undefined;
  if (expectedCode === "unknown_required_capability") return;
  if (message === "cloudagents.platformadapter.v1alpha1.AdapterRegistrationReceipt") {
    const digest =
      typeof expected.canonicalRegistrationDigest === "string"
        ? expected.canonicalRegistrationDigest
        : undefined;
    if (digest && supplied !== digestTextToBase64(digest)) {
      throw new Error(`${label} registration receipt does not bind its expected canonical digest.`);
    }
    return;
  }
  if (message === "cloudagents.platformadapter.v1alpha1.AdapterRegistrationReceiptRequest") {
    const admission = requiredRecord(entry.admissionState, `${label} admissionState`);
    if (typeof supplied !== "string") throw new Error(`${label} receipt lookup requires a digest.`);
    const suppliedDigest = base64DigestToText(supplied, `${label} canonicalRegistrationSha256`);
    if (expectedCode === "registration_receipt_digest_mismatch") {
      if (
        admission.registrationRecord !== "PRESENT_WITH_DIFFERENT_DIGEST" ||
        admission.storedCanonicalRegistrationDigest === suppliedDigest
      ) {
        throw new Error(`${label} does not demonstrate a registration receipt digest mismatch.`);
      }
      return;
    }
    if (expected.decision === "ACCEPT") {
      const negotiated = requiredArray(
        admission.negotiatedCapabilities,
        `${label} negotiatedCapabilities`,
      );
      if (
        admission.registrationRecord !== "PRESENT_WITH_SAME_DIGEST" ||
        admission.registrationIdempotencyKey !== protoJson.registrationIdempotencyKey ||
        admission.storedCanonicalRegistrationDigest !== suppliedDigest ||
        !negotiated.includes(protoJson.requiredCapability)
      ) {
        throw new Error(
          `${label} accepted receipt recovery is not bound to same key/digest/capability.`,
        );
      }
    }
    return;
  }
  if (message !== "cloudagents.platformadapter.v1alpha1.AdapterRegistrationRequest") return;
  if (expectedCode === "canonical_registration_digest_required") {
    if (supplied !== undefined && supplied !== "") {
      throw new Error(`${label} must omit canonicalRegistrationSha256 for the required negative.`);
    }
    return;
  }
  const projection = registrationCanonicalProjection(protoJson);
  if (!projection) return;
  const canonicalUtf8 = new TextDecoder().decode(canonicalizeJson(projection));
  const digest = canonicalJsonDigest(projection);
  const expectedBase64 = digestTextToBase64(digest);
  if (expectedCode === "canonical_registration_digest_mismatch") {
    if (supplied === expectedBase64) {
      throw new Error(`${label} registration digest-mismatch fixture carries the correct digest.`);
    }
    return;
  }
  if (supplied === undefined && String(expected.decision).startsWith("REJECT")) return;
  if (supplied !== expectedBase64) {
    throw new Error(`${label} canonicalRegistrationSha256 does not match registration intent.`);
  }
  if (
    expected.canonicalRegistrationUtf8 !== undefined &&
    expected.canonicalRegistrationUtf8 !== canonicalUtf8
  ) {
    throw new Error(`${label} canonicalRegistrationUtf8 does not match registration intent.`);
  }
  if (
    expected.canonicalRegistrationDigest !== undefined &&
    expected.canonicalRegistrationDigest !== digest
  ) {
    throw new Error(`${label} canonicalRegistrationDigest does not match registration intent.`);
  }
  if (expectedCode === "registration_idempotency_conflict") {
    const admission = requiredRecord(entry.admissionState, `${label} admissionState`);
    if (
      admission.registrationIdempotencyKey !== protoJson.registrationIdempotencyKey ||
      admission.storedCanonicalRegistrationDigest === digest
    ) {
      throw new Error(`${label} does not demonstrate same-key/different-registration-intent.`);
    }
  }
}

function registrationCanonicalProjection(value: JsonRecord): JsonRecord | undefined {
  if (
    !isRecord(value.adapterInstance) ||
    !Array.isArray(value.capabilities) ||
    !isRecord(value.expectedClientIdentity) ||
    !isRecord(value.expectedServerIdentity) ||
    !isRecord(value.endpoint)
  ) {
    return undefined;
  }
  const capabilities = [
    ...new Set(
      value.capabilities.map((capability) => {
        if (typeof capability !== "string" || !CAPABILITY_NUMBERS.has(capability)) {
          throw new Error(`Unknown registration capability ${String(capability)}.`);
        }
        return capability;
      }),
    ),
  ].toSorted((left, right) => CAPABILITY_NUMBERS.get(left)! - CAPABILITY_NUMBERS.get(right)!);
  return {
    adapterInstance: value.adapterInstance,
    capabilities,
    expectedClientIdentity: value.expectedClientIdentity,
    expectedServerIdentity: value.expectedServerIdentity,
    endpoint: value.endpoint,
  };
}

function digestTextToBase64(digest: string): string {
  if (!/^sha256:[a-f0-9]{64}$/u.test(digest)) throw new Error(`Invalid SHA-256 digest ${digest}.`);
  return Buffer.from(digest.slice("sha256:".length), "hex").toString("base64");
}

function base64DigestToText(value: string, label: string): string {
  if (!/^[A-Za-z0-9+/]{43}=$/u.test(value)) throw new Error(`${label} must encode 32 bytes.`);
  const bytes = Buffer.from(value, "base64");
  if (bytes.length !== 32 || bytes.toString("base64") !== value) {
    throw new Error(`${label} must be canonical base64 SHA-256 bytes.`);
  }
  return `sha256:${bytes.toString("hex")}`;
}

function operationCanonicalProjection(operation: JsonRecord): JsonRecord | undefined {
  const fencing = isRecord(operation.fencing) ? operation.fencing : undefined;
  if (
    typeof operation.operationId !== "string" ||
    typeof operation.idempotencyKey !== "string" ||
    !isRecord(operation.scope) ||
    !fencing ||
    typeof fencing.leaseId !== "string" ||
    typeof fencing.generation !== "string" ||
    typeof operation.deadline !== "string" ||
    typeof operation.requiredCapability !== "string" ||
    !isRecord(operation.command)
  ) {
    return undefined;
  }
  return {
    operationId: operation.operationId,
    idempotencyKey: operation.idempotencyKey,
    scope: operation.scope,
    fencing: { leaseId: fencing.leaseId, generation: fencing.generation },
    deadline: operation.deadline,
    requiredCapability: operation.requiredCapability,
    command: operation.command,
    finalizers: Array.isArray(operation.finalizers) ? operation.finalizers : [],
  };
}

function protoNamedBlocks(
  source: string,
  kind: "enum" | "message" | "service",
  file: string,
): ReadonlyArray<{ readonly name: string; readonly body: string }> {
  const blocks: Array<{ name: string; body: string }> = [];
  const expression = new RegExp(`\\b${kind}\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*\\{`, "gu");
  for (const match of source.matchAll(expression)) {
    const open = match.index + match[0].lastIndexOf("{");
    const close = matchingBrace(source, open, file);
    blocks.push({ name: match[1]!, body: source.slice(open + 1, close) });
  }
  return blocks;
}

function maskNestedProtoTypeBlocks(source: string, file: string): string {
  const characters = source.split("");
  const expression = /\b(?:enum|message)\s+[A-Za-z_][A-Za-z0-9_]*\s*\{/gu;
  for (const match of source.matchAll(expression)) {
    const open = match.index + match[0].lastIndexOf("{");
    const close = matchingBrace(source, open, file);
    for (let index = match.index; index <= close; index += 1) characters[index] = " ";
  }
  return characters.join("");
}

function matchingBrace(source: string, open: number, file: string): number {
  let depth = 0;
  const structural = maskQuotedText(source);
  for (let index = open; index < structural.length; index += 1) {
    if (structural[index] === "{") depth += 1;
    else if (structural[index] === "}") depth -= 1;
    if (depth === 0) return index;
  }
  throw new Error(`${file} has an unterminated Proto block.`);
}

function stripProtoComments(source: string, file: string): string {
  let output = "";
  let mode: "block" | "code" | "line" | "single" | "double" = "code";
  for (let index = 0; index < source.length; index += 1) {
    const current = source[index]!;
    const next = source[index + 1];
    if (mode === "line") {
      if (current === "\n") {
        mode = "code";
        output += current;
      } else output += " ";
      continue;
    }
    if (mode === "block") {
      if (current === "*" && next === "/") {
        output += "  ";
        index += 1;
        mode = "code";
      } else output += current === "\n" ? "\n" : " ";
      continue;
    }
    if (mode === "single" || mode === "double") {
      output += current;
      if (current === "\\" && next !== undefined) {
        output += next;
        index += 1;
      } else if ((mode === "single" && current === "'") || (mode === "double" && current === '"')) {
        mode = "code";
      }
      continue;
    }
    if (current === "/" && next === "/") {
      output += "  ";
      index += 1;
      mode = "line";
    } else if (current === "/" && next === "*") {
      output += "  ";
      index += 1;
      mode = "block";
    } else {
      output += current;
      if (current === "'") mode = "single";
      else if (current === '"') mode = "double";
    }
  }
  if (mode === "block") throw new Error(`${file} has an unterminated block comment.`);
  if (mode === "single" || mode === "double")
    throw new Error(`${file} has an unterminated string.`);
  return output;
}

function maskQuotedText(source: string): string {
  let output = "";
  let quote: "'" | '"' | undefined;
  for (let index = 0; index < source.length; index += 1) {
    const current = source[index]!;
    if (quote === undefined) {
      if (current === "'" || current === '"') {
        quote = current;
        output += current;
      } else output += current;
      continue;
    }
    if (current === "\\" && source[index + 1] !== undefined) {
      output += "  ";
      index += 1;
    } else if (current === quote) {
      output += current;
      quote = undefined;
    } else output += current === "\n" ? "\n" : " ";
  }
  return output;
}

function requiredRegexCapture(source: string, expression: RegExp, label: string): string {
  const match = expression.exec(source);
  if (!match?.[1]) throw new Error(`${label} is missing.`);
  return match[1];
}

function assertStringArrayEqual(
  actual: ReadonlyArray<string>,
  expected: ReadonlyArray<string>,
  label: string,
): void {
  if (actual.join("\0") !== expected.join("\0")) {
    throw new Error(`${label} mismatch: ${actual.join(", ")} != ${expected.join(", ")}.`);
  }
}

export function validateCanonicalExpectation(
  value: JsonRecord,
  file: string,
  instance: unknown = value.instance,
): void {
  const canonicalUtf8 = requiredString(
    value.canonicalUtf8 ?? value.canonicalJson,
    `${file} canonicalUtf8/canonicalJson`,
  );
  const digest = requiredString(value.digest, `${file} digest`);
  const urn = requiredString(value.urn, `${file} urn`);
  const result = validateCanonicalNamespaceRefFixture({ instance, canonicalUtf8, digest, urn });
  if (!result.valid) {
    throw new Error(`${file} canonical NamespaceRef failed: ${result.errors[0]?.code}.`);
  }
}

function validateDocumentReferences(value: unknown, file: string, contractRoot: string): void {
  visit(value, (record, pointer) => {
    if (record.$ref === undefined) return;
    const reference = requiredString(record.$ref, `${file}${pointer}/$ref`);
    if (reference.startsWith("#")) {
      resolveJsonPointer(value, decodeOpenApiFragment(reference));
      return;
    }
    if (/^[a-z][a-z0-9+.-]*:/iu.test(reference)) {
      throw new Error(`${file}${pointer}/$ref must be offline and relative, found ${reference}.`);
    }
    const [relativeReference, fragment] = reference.split("#", 2);
    if (!relativeReference) {
      throw new Error(`${file}${pointer}/$ref has an empty external path.`);
    }
    const target = resolve(dirname(file), relativeReference);
    if (!isWithin(contractRoot, target))
      throw new Error(`${file}${pointer}/$ref escapes contracts/.`);
    const stat = lstatSync(target, { throwIfNoEntry: false });
    if (!stat?.isFile() || stat.isSymbolicLink()) {
      throw new Error(`${file}${pointer}/$ref target is not a regular file: ${target}.`);
    }
    if (fragment !== undefined && fragment.length > 0) {
      const targetDocument = parseJsonFile(target);
      resolveJsonPointer(targetDocument, decodeOpenApiFragment(`#${fragment}`));
    }
  });
}

function validateFixtureManifest(document: JsonRecord, file: string, contractRoot: string): void {
  assertKnownKeys(document, ["cases", "version"], file);
  if (document.version !== "v1alpha1")
    throw new Error(`${file} must use fixture version v1alpha1.`);
  const cases = requiredArray(document.cases, `${file} cases`);
  const names = new Set<string>();
  for (const [index, caseValue] of cases.entries()) {
    const fixture = requiredRecord(caseValue, `${file} cases/${index}`);
    assertKnownKeys(
      fixture,
      [
        "document",
        "expectedError",
        "expectedSchemaValid",
        "expectedSemanticValid",
        "instance",
        "instancePointer",
        "name",
        "schema",
      ],
      `${file} cases/${index}`,
    );
    const name = requiredString(fixture.name, `${file} cases/${index}/name`);
    if (names.has(name)) throw new Error(`${file} duplicates fixture ${name}.`);
    names.add(name);
    for (const field of ["schema", "instance", "document"] as const) {
      if (fixture[field] === undefined) continue;
      const target = resolve(
        dirname(file),
        requiredString(fixture[field], `${file} ${name} ${field}`),
      );
      if (
        !isWithin(contractRoot, target) ||
        !lstatSync(target, { throwIfNoEntry: false })?.isFile()
      ) {
        throw new Error(`${file} fixture ${name} has missing or escaped ${field}.`);
      }
    }
    if (typeof fixture.expectedSchemaValid !== "boolean") {
      throw new Error(`${file} fixture ${name} must declare expectedSchemaValid.`);
    }
    if ((fixture.instance === undefined) === (fixture.document === undefined)) {
      throw new Error(`${file} fixture ${name} must declare exactly one of instance/document.`);
    }
    if (fixture.instancePointer !== undefined && fixture.document === undefined) {
      throw new Error(`${file} fixture ${name} instancePointer requires document.`);
    }
    if (
      fixture.expectedSemanticValid !== undefined &&
      typeof fixture.expectedSemanticValid !== "boolean"
    ) {
      throw new Error(`${file} fixture ${name} expectedSemanticValid must be boolean.`);
    }
    const expectsFailure =
      fixture.expectedSchemaValid === false || fixture.expectedSemanticValid === false;
    if (expectsFailure) {
      const code = requiredString(fixture.expectedError, `${file} fixture ${name} expectedError`);
      if (!/^[A-Z][A-Z0-9_]{2,63}$/u.test(code)) {
        throw new Error(`${file} fixture ${name} expectedError must be a stable uppercase code.`);
      }
    } else if (fixture.expectedError !== undefined) {
      throw new Error(`${file} fixture ${name} must not declare expectedError for a passing case.`);
    }
  }
  validateP1A1FixtureInventory(cases, file, contractRoot);
}

function validateP1A1FixtureInventory(
  cases: ReadonlyArray<unknown>,
  file: string,
  contractRoot: string,
): void {
  const manifestPath = relativePath(contractRoot, file);
  const requiredFixtures = P1_A1_FIXTURE_INVENTORY[manifestPath];
  if (!requiredFixtures) return;
  const fixturesByName = new Map(
    cases.map((value, index) => {
      const fixture = requiredRecord(value, `${file} cases/${index}`);
      return [requiredString(fixture.name, `${file} cases/${index}/name`), fixture] as const;
    }),
  );
  for (const expected of requiredFixtures) {
    const name = requiredString(expected.name, `${manifestPath} required fixture name`);
    const actual = fixturesByName.get(name);
    if (!actual) {
      throw new Error(`${manifestPath} P1-A1 fixture inventory is missing ${name}.`);
    }
    if (
      new TextDecoder().decode(canonicalizeJson(actual)) !==
      new TextDecoder().decode(canonicalizeJson(expected))
    ) {
      throw new Error(`${manifestPath} P1-A1 fixture inventory metadata drifted for ${name}.`);
    }
  }
}

function resolveJsonPointer(document: unknown, pointer: string): unknown {
  if (pointer === "") return document;
  if (!pointer.startsWith("/")) throw new Error(`Invalid JSON Pointer ${pointer}.`);
  let value = document;
  for (const segment of pointer.slice(1).split("/")) {
    const key = segment.replaceAll("~1", "/").replaceAll("~0", "~");
    if (Array.isArray(value)) {
      const index = Number(key);
      if (!Number.isSafeInteger(index) || index < 0 || index >= value.length) {
        throw new Error(`JSON Pointer ${pointer} has invalid array index ${key}.`);
      }
      value = value[index];
    } else if (isRecord(value) && Object.hasOwn(value, key)) {
      value = value[key];
    } else {
      throw new Error(`JSON Pointer ${pointer} is missing segment ${key}.`);
    }
  }
  return value;
}

function contractManifestDigest(root: string, files: ReadonlyArray<string>): string {
  const hash = createHash("sha256");
  for (const file of files) {
    const path = relativePath(root, file);
    const stat = lstatSync(file);
    const digest = createHash("sha256").update(readFileSync(file)).digest("hex");
    hash
      .update(path)
      .update("\0")
      .update(digest)
      .update("\0")
      .update(stat.mode & 0o111 ? "100755" : "100644")
      .update("\0");
  }
  return `sha256:${hash.digest("hex")}`;
}

function walkFiles(directory: string): ReadonlyArray<string> {
  const files: string[] = [];
  for (const name of readdirSync(directory).toSorted()) {
    const target = join(directory, name);
    const stat = lstatSync(target);
    if (stat.isSymbolicLink()) throw new Error(`Contract tree must not contain symlink ${target}.`);
    if (stat.isDirectory()) files.push(...walkFiles(target));
    else if (stat.isFile()) files.push(target);
  }
  return files.toSorted();
}

function parseJsonFile(file: string): JsonRecord {
  const value = JSON.parse(readFileSync(file, "utf8")) as unknown;
  return requiredRecord(value, file);
}

function visit(
  value: unknown,
  callback: (record: JsonRecord, pointer: string) => void,
  pointer = "",
): void {
  if (Array.isArray(value)) {
    value.forEach((entry, index) => visit(entry, callback, `${pointer}/${index}`));
    return;
  }
  if (!isRecord(value)) return;
  callback(value, pointer);
  for (const [key, child] of Object.entries(value)) {
    visit(child, callback, `${pointer}/${escapePointer(key)}`);
  }
}

function requiredRecord(value: unknown, label: string): JsonRecord {
  if (!isRecord(value)) throw new Error(`${label} must be an object.`);
  return value;
}

function optionalRecord(value: unknown, label: string): JsonRecord {
  return value === undefined ? {} : requiredRecord(value, label);
}

function requiredArray(value: unknown, label: string): ReadonlyArray<unknown> {
  if (!Array.isArray(value)) throw new Error(`${label} must be an array.`);
  return value;
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value.length === 0)
    throw new Error(`${label} must be a string.`);
  return value;
}

function assertKnownKeys(value: JsonRecord, allowed: ReadonlyArray<string>, label: string): void {
  const allowlist = new Set(allowed);
  const unknown = Object.keys(value).filter((key) => !allowlist.has(key));
  if (unknown.length > 0) throw new Error(`${label} has unknown fields: ${unknown.join(", ")}.`);
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isWithin(parent: string, target: string): boolean {
  const candidate = relative(parent, target);
  return candidate !== ".." && !candidate.startsWith(`..${sep}`) && !candidate.startsWith(sep);
}

function relativePath(root: string, file: string): string {
  return relative(root, file).split(sep).join("/");
}

function escapePointer(value: string): string {
  return value.replaceAll("~", "~0").replaceAll("/", "~1");
}

function braceBalance(source: string): number {
  let balance = 0;
  for (const character of source) {
    if (character === "{") balance += 1;
    else if (character === "}") balance -= 1;
    if (balance < 0) return balance;
  }
  return balance;
}
