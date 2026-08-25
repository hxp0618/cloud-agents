import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync } from "node:fs";
import { relative, resolve, sep } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";

import {
  assertDurableCoordinationRegistryV2Current,
  durableCoordinationGoV2Inputs,
  durableCoordinationRegistryV2Inputs,
  DURABLE_COORDINATION_V2_GO_OUTPUT_PATH,
  DURABLE_COORDINATION_V2_OUTPUT_PATH,
} from "./platform-durable-coordination-registry";
import { assertDurableCoordinationGoV2Current } from "./platform-durable-coordination-go-v2";
import {
  durableProjectCreateMigrationClosure,
  type DurableProjectCreateMigrationClosure,
} from "./platform-migration-bundle";
import { canonicalizeJson, type JsonRecord } from "./platform-json-semantics";

export const DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-lineage-source-v2.json";
export const DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH =
  "contracts/generated/platform/v1alpha1/durable-project-create-lineage-v2.json";
export const DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_SCHEMA_PATH =
  "contracts/generated/platform/v1alpha1/durable-project-create-lineage-source-v2.schema.json";
export const DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_SCHEMA_PATH =
  "contracts/generated/platform/v1alpha1/durable-project-create-lineage-v2.schema.json";
export const DURABLE_PROJECT_CREATE_LINEAGE_FIXTURE_MANIFEST_PATH =
  "contracts/platform/v1alpha1/fixtures/manifest-v2.json";
export const DURABLE_PROJECT_CREATE_LINEAGE_GENERATOR_PATH =
  "scripts/generate-platform-durable-project-create-lineage-v2.ts";
export const DURABLE_PROJECT_CREATE_LINEAGE_LIBRARY_PATH =
  "scripts/lib/platform-durable-project-create-lineage-v2.ts";
export const DURABLE_PROJECT_CREATE_LINEAGE_TEST_PATH =
  "scripts/lib/platform-durable-project-create-lineage-v2.test.ts";

const DURABLE_REGISTRY_SOURCE_V2_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v2.json";
const DURABLE_PROFILE_V2_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-durable-v1alpha1.json";
const DURABLE_LEGACY_ROUTE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-route-v2.json";
const DURABLE_ROUTE_AUTHORITY_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-route-authority-v2.json";
const DURABLE_PROJECTION_FIXTURE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-project-create-idempotency-projection.json";
const V1_REGISTRY_PATH = "contracts/generated/platform/v1alpha1/durable-coordination-registry.json";
const V1_PROFILE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-profile-managed-agent-create-project-v1alpha1.json";
const V1_SOURCE_PATH =
  "contracts/platform/v1alpha1/fixtures/golden/durable-coordination-registry-source-v1.json";
const MIGRATION_LIBRARY_PATH = "scripts/lib/platform-migration-bundle.ts";
const MIGRATION_TEST_PATH = "scripts/lib/platform-migration-bundle.test.ts";

const EXPECTED_FIXTURE_CASE_NAMES = [
  "durable-coordination-profile-managed-agent-create-project-durable-v1alpha1",
  "durable-coordination-registry-source-v2",
  "durable-coordination-registry-v2",
  "durable-project-create-route-v2",
  "durable-project-create-route-authority-v2",
  "durable-project-create-idempotency-projection",
] as const;

const EXPECTED_PREDECESSOR_MIGRATION_PATHS = [
  "services/control-plane/migrations/000001_expand_migration_kernel.sql",
  "services/control-plane/migrations/000002_expand_tenancy.sql",
  "services/control-plane/migrations/000003_expand_membership_rbac.sql",
  "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
  "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
  "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
  "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
  "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
  "services/control-plane/migrations/000009_redact_coordination_conflicts.sql",
  "services/control-plane/migrations/000010_expand_compatibility_recovery_kernel.sql",
  "services/control-plane/migrations/000011_add_compatibility_recovery_writer.sql",
  "services/control-plane/migrations/000012_fix_compatibility_recovery_preflight.sql",
] as const;

const EXPECTED_DURABLE_SCHEMA_PATHS = [
  "contracts/platform/v1alpha1/schemas/durable-coordination-profile-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-source-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-coordination-registry-v2.schema.json",
  "contracts/platform/v1alpha1/schemas/durable-project-create-idempotency-projection.schema.json",
  "contracts/platform/v1alpha1/schemas/project.schema.json",
  "contracts/platform/v1alpha1/schemas/managed-agent-create-project-organization-ref.schema.json",
  "contracts/generated/platform/v1alpha1/durable-project-create-route-authority-v2.schema.json",
  "contracts/generated/platform/v1alpha1/durable-project-create-route-legacy-v1.schema.json",
] as const;

const EXPECTED_DURABLE_GENERATOR_PATHS = [
  "scripts/generate-platform-durable-coordination-registry-v2.ts",
  "scripts/generate-platform-durable-coordination-go-v2.ts",
  "scripts/lib/platform-durable-coordination-go-v2.ts",
  "scripts/lib/platform-durable-coordination-registry.ts",
  "scripts/generate-platform-durable-project-create-lineage-v2.ts",
  "scripts/lib/platform-durable-project-create-lineage-v2.ts",
  "scripts/lib/platform-durable-project-create-lineage-v2.test.ts",
] as const;

const EXPECTED_FIXTURE_CASES = [
  {
    name: "durable-coordination-profile-managed-agent-create-project-durable-v1alpha1",
    schema: "../schemas/durable-coordination-profile-v2.schema.json",
    instance:
      "golden/durable-coordination-profile-managed-agent-create-project-durable-v1alpha1.json",
  },
  {
    name: "durable-coordination-registry-source-v2",
    schema: "../schemas/durable-coordination-registry-source-v2.schema.json",
    instance: "golden/durable-coordination-registry-source-v2.json",
  },
  {
    name: "durable-coordination-registry-v2",
    schema: "../schemas/durable-coordination-registry-v2.schema.json",
    instance: "../../../generated/platform/v1alpha1/durable-coordination-registry-v2.json",
  },
  {
    name: "durable-project-create-route-v2",
    schema:
      "../../../generated/platform/v1alpha1/durable-project-create-route-legacy-v1.schema.json",
    instance: "golden/durable-project-create-route-v2.json",
  },
  {
    name: "durable-project-create-route-authority-v2",
    schema:
      "../../../generated/platform/v1alpha1/durable-project-create-route-authority-v2.schema.json",
    instance: "golden/durable-project-create-route-authority-v2.json",
  },
  {
    name: "durable-project-create-idempotency-projection",
    schema: "../schemas/durable-project-create-idempotency-projection.schema.json",
    instance: "golden/durable-project-create-idempotency-projection.json",
  },
] as const;

type Artifact = Readonly<{
  path: string;
  sha256: string;
  sizeBytes: number;
  mode: "100644" | "100755";
}>;

type SourceDocument = JsonRecord & {
  readonly predecessor: JsonRecord;
  readonly fixtureManifestPath: string;
  readonly durableAuthority: JsonRecord;
  readonly migration: JsonRecord;
  readonly implementationBoundary: JsonRecord;
};

export class DurableProjectCreateLineageError extends Error {
  constructor(
    readonly code: string,
    readonly path: string,
    message: string,
  ) {
    super(message);
    this.name = "DurableProjectCreateLineageError";
  }
}

export function buildDurableProjectCreateLineageV2(root: string): JsonRecord {
  const source = readJson(root, DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_PATH) as SourceDocument;
  validateJson(root, DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_SCHEMA_PATH, source, true);
  assertDurableProjectCreateLineagePredecessor(root, source.predecessor);
  assertDurableCoordinationRegistryV2Current(root);
  assertDurableCoordinationGoV2Current(root);
  const fixtureManifest = validateVersionedFixtureManifest(root);
  const migration = durableProjectCreateMigrationClosure(root);

  const durableAuthority = source.durableAuthority;
  assertExactPathList(
    durableAuthority.schemaPaths,
    EXPECTED_DURABLE_SCHEMA_PATHS,
    "/durableAuthority/schemaPaths",
  );
  assertExactPathList(
    durableAuthority.generatorPaths,
    EXPECTED_DURABLE_GENERATOR_PATHS,
    "/durableAuthority/generatorPaths",
  );
  const durable = {
    source: artifact(
      root,
      requireString(durableAuthority.sourcePath, "/durableAuthority/sourcePath"),
    ),
    profile: artifact(
      root,
      requireString(durableAuthority.profilePath, "/durableAuthority/profilePath"),
    ),
    legacyRoute: artifact(
      root,
      requireString(durableAuthority.legacyRoutePath, "/durableAuthority/legacyRoutePath"),
    ),
    routeAuthority: artifact(
      root,
      requireString(durableAuthority.routeAuthorityPath, "/durableAuthority/routeAuthorityPath"),
    ),
    projectionFixture: artifact(
      root,
      requireString(
        durableAuthority.projectionFixturePath,
        "/durableAuthority/projectionFixturePath",
      ),
    ),
    schemas: requiredArray(durableAuthority.schemaPaths, "/durableAuthority/schemaPaths").map(
      (path, index) =>
        artifact(root, requireString(path, `/durableAuthority/schemaPaths/${index}`)),
    ),
    generators: requiredArray(
      durableAuthority.generatorPaths,
      "/durableAuthority/generatorPaths",
    ).map((path, index) =>
      artifact(root, requireString(path, `/durableAuthority/generatorPaths/${index}`)),
    ),
    output: artifact(root, DURABLE_COORDINATION_V2_OUTPUT_PATH),
    goOutput: artifact(root, DURABLE_COORDINATION_V2_GO_OUTPUT_PATH),
    priorRegistry: predecessorArtifact(root, V1_REGISTRY_PATH),
  };

  const body: JsonRecord = {
    formatVersion: "cloud-agents-durable-project-create-lineage/v2",
    lineageId: "cloud-agents/platform/durable-project-create-lineage",
    sourceDigest: domainDigest("cloud-agents/durable-project-create-lineage/source/v2", source),
    predecessor: source.predecessor,
    fixtureManifest: {
      path: DURABLE_PROJECT_CREATE_LINEAGE_FIXTURE_MANIFEST_PATH,
      sha256: fixtureManifest.sha256,
      sizeBytes: fixtureManifest.sizeBytes,
      mode: "100644",
      caseCount: fixtureManifest.caseNames.length,
      caseNames: fixtureManifest.caseNames,
    },
    durableAuthority: durable,
    migration: migrationOutput(migration),
    implementationBoundary: source.implementationBoundary,
  };
  const generated = {
    ...body,
    lineageDigest: domainDigest("cloud-agents/durable-project-create-lineage/document/v2", body),
  };
  validateJson(root, DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_SCHEMA_PATH, generated, true);
  return generated;
}

export function serializeDurableProjectCreateLineageV2(value: JsonRecord): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

export function assertDurableProjectCreateLineageV2Current(root: string): void {
  const expected = serializeDurableProjectCreateLineageV2(buildDurableProjectCreateLineageV2(root));
  const actual = readFileSync(resolve(root, DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH), "utf8");
  if (actual !== expected) {
    throw new Error(`${DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_PATH} is stale.`);
  }
}

/** The complete versioned source closure consumed by the future v3 lock writer. */
export function durableProjectCreateLineageV2Inputs(root: string): string[] {
  const source = readJson(root, DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_PATH) as SourceDocument;
  const predecessor = source.predecessor;
  const durableAuthority = source.durableAuthority;
  const migration = source.migration;
  const migrations = requiredArray(predecessor.migrationsV1, "/predecessor/migrationsV1");
  assertExactPathList(
    durableAuthority.schemaPaths,
    EXPECTED_DURABLE_SCHEMA_PATHS,
    "/durableAuthority/schemaPaths",
  );
  assertExactPathList(
    durableAuthority.generatorPaths,
    EXPECTED_DURABLE_GENERATOR_PATHS,
    "/durableAuthority/generatorPaths",
  );
  assertExactPathList(
    migrations.map((entry, index) =>
      requireString(requiredObject(entry).path, `/predecessor/migrationsV1/${index}/path`),
    ),
    EXPECTED_PREDECESSOR_MIGRATION_PATHS,
    "/predecessor/migrationsV1",
  );
  const paths = [
    DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_PATH,
    DURABLE_PROJECT_CREATE_LINEAGE_SOURCE_SCHEMA_PATH,
    DURABLE_PROJECT_CREATE_LINEAGE_OUTPUT_SCHEMA_PATH,
    DURABLE_PROJECT_CREATE_LINEAGE_FIXTURE_MANIFEST_PATH,
    DURABLE_REGISTRY_SOURCE_V2_PATH,
    DURABLE_PROFILE_V2_PATH,
    DURABLE_LEGACY_ROUTE_PATH,
    DURABLE_ROUTE_AUTHORITY_PATH,
    DURABLE_PROJECTION_FIXTURE_PATH,
    DURABLE_COORDINATION_V2_OUTPUT_PATH,
    DURABLE_COORDINATION_V2_GO_OUTPUT_PATH,
    MIGRATION_LIBRARY_PATH,
    MIGRATION_TEST_PATH,
    V1_REGISTRY_PATH,
    V1_PROFILE_PATH,
    V1_SOURCE_PATH,
    requireString(predecessor.generationLock?.path, "/predecessor/generationLock/path"),
    requireString(migration.sqlPath, "/migration/sqlPath"),
    requireString(migration.manifestPath, "/migration/manifestPath"),
    requireString(migration.schemaBundlePath, "/migration/schemaBundlePath"),
    requireString(migration.catalogPath, "/migration/catalogPath"),
    requireString(migration.archivePath, "/migration/archivePath"),
    ...migrations.map((entry, index) =>
      requireString(requiredObject(entry).path, `/predecessor/migrationsV1/${index}/path`),
    ),
    ...requiredArray(durableAuthority.schemaPaths, "/durableAuthority/schemaPaths").map(
      (path, index) => requireString(path, `/durableAuthority/schemaPaths/${index}`),
    ),
    ...requiredArray(durableAuthority.generatorPaths, "/durableAuthority/generatorPaths").map(
      (path, index) => requireString(path, `/durableAuthority/generatorPaths/${index}`),
    ),
    ...durableCoordinationRegistryV2Inputs(root),
    ...durableCoordinationGoV2Inputs(root),
  ];
  const unique = [...new Set(paths)].toSorted();
  for (const path of unique) requireRegularFile(root, path);
  return unique;
}

export function validateVersionedFixtureManifest(root: string): Readonly<{
  sha256: string;
  sizeBytes: number;
  caseNames: readonly string[];
}> {
  const manifest = readJson(root, DURABLE_PROJECT_CREATE_LINEAGE_FIXTURE_MANIFEST_PATH);
  validateVersionedFixtureManifestShape(manifest);
  const cases = requiredArray(manifest.cases, "/cases");
  const names = cases.map((value, index) =>
    requireString(requiredObject(value).name, `/cases/${index}/name`),
  );
  if (JSON.stringify(names) !== JSON.stringify(EXPECTED_FIXTURE_CASE_NAMES)) {
    throw new Error("Versioned durable fixture manifest order or membership drifted.");
  }
  const schemas = loadContractSchemas(root);
  const ajv = schemas.ajv;
  for (const [index, value] of cases.entries()) {
    const fixture = requiredObject(value);
    const schemaPath = resolve(
      root,
      "contracts/platform/v1alpha1/fixtures",
      requireString(fixture.schema, `/cases/${index}/schema`),
    );
    const schema = schemas.byPath.get(schemaPath);
    if (!schema) throw new Error(`Versioned fixture schema is not registered: ${schemaPath}`);
    const schemaId = requireString(schema.$id, `${schemaPath} $id`);
    const validate = ajv.getSchema(schemaId);
    if (!validate) throw new Error(`Versioned fixture schema is not compiled: ${schemaId}`);
    const instancePath = fixture.instance;
    if (typeof instancePath !== "string")
      throw new Error(`Versioned fixture case ${names[index]} has no instance.`);
    const instance = readJson(root, `contracts/platform/v1alpha1/fixtures/${instancePath}`);
    if (!validate(instance))
      throw new Error(`Versioned fixture case ${names[index]} violates ${schemaId}.`);
  }
  const bytes = readFileSync(resolve(root, DURABLE_PROJECT_CREATE_LINEAGE_FIXTURE_MANIFEST_PATH));
  return {
    sha256: digestBytes(bytes),
    sizeBytes: bytes.byteLength,
    caseNames: names,
  };
}

/**
 * The historical common fixture-manifest schema only permits generated
 * instance paths ending in `.json`; this versioned manifest deliberately
 * binds generated `.schema.json` authorities.  Keep its shape check local so
 * the immutable v1 schema does not have to be widened.
 */
function validateVersionedFixtureManifestShape(manifest: JsonRecord): void {
  if (manifest.version !== "v1alpha1")
    throw new Error("Versioned fixture manifest version drifted.");
  const cases = requiredArray(manifest.cases, "/cases");
  if (cases.length !== EXPECTED_FIXTURE_CASE_NAMES.length) {
    throw new Error("Versioned durable fixture manifest case count drifted.");
  }
  for (const [index, value] of cases.entries()) {
    const fixture = requiredObject(value);
    const expected = EXPECTED_FIXTURE_CASES[index];
    if (!expected) throw new Error(`Versioned fixture case ${index} is unexpected.`);
    const allowed = new Set([
      "name",
      "schema",
      "instance",
      "expectedSchemaValid",
      "expectedSemanticValid",
    ]);
    for (const key of Object.keys(fixture)) {
      if (!allowed.has(key))
        throw new Error(`Versioned fixture case ${index} has unknown field ${key}.`);
    }
    requireString(fixture.name, `/cases/${index}/name`);
    const schema = requireString(fixture.schema, `/cases/${index}/schema`);
    const instance = requireString(fixture.instance, `/cases/${index}/instance`);
    if (
      fixture.name !== expected.name ||
      schema !== expected.schema ||
      instance !== expected.instance
    ) {
      throw new Error(`Versioned fixture case ${index} schema/instance mapping drifted.`);
    }
    if (!schema.startsWith("../schemas/") && !schema.startsWith("../../../generated/")) {
      throw new Error(
        `Versioned fixture schema path is outside the versioned contract roots: ${schema}`,
      );
    }
    if (
      !instance.startsWith("golden/") &&
      !instance.startsWith("../../../generated/platform/v1alpha1/")
    ) {
      throw new Error(
        `Versioned fixture instance path is outside the versioned fixture roots: ${instance}`,
      );
    }
    if (fixture.expectedSchemaValid !== true || fixture.expectedSemanticValid !== true) {
      throw new Error(`Versioned fixture case ${index} must be an expected-valid case.`);
    }
  }
}

function assertExactPathList(value: unknown, expected: ReadonlyArray<string>, path: string): void {
  const actual = requiredArray(value, path).map((entry, index) =>
    requireString(entry, `${path}/${index}`),
  );
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    throw new Error(`${path} must match the exact versioned authority path set.`);
  }
}

function migrationOutput(closure: DurableProjectCreateMigrationClosure): JsonRecord {
  const convert = (value: {
    path: string;
    mode: "100644";
    sizeBytes: number;
    sha256: string;
  }): Artifact => ({
    path: value.path,
    mode: value.mode,
    sizeBytes: value.sizeBytes,
    sha256: value.sha256,
  });
  return {
    id: closure.migrationId,
    sql: convert(closure.sql),
    predecessorCatalog: convert(closure.predecessorCatalog),
    catalog: convert(closure.catalog),
    manifest: { ...convert(closure.manifest), manifestDigest: closure.manifest.manifestDigest },
    schemaBundle: {
      ...convert(closure.schemaBundle),
      schemaBundleDigest: closure.schemaBundle.schemaBundleDigest,
    },
    predecessorArchive: {
      ...convert(closure.predecessorArchive),
      schemaBundleDigest: closure.predecessorArchive.schemaBundleDigest,
    },
  };
}

export function assertDurableProjectCreateLineagePredecessor(
  root: string,
  predecessor: JsonRecord,
): void {
  const lock = requiredObject(predecessor.generationLock);
  const lockPath = requireString(lock.path, "/predecessor/generationLock/path");
  if (lockPath !== "contracts/generation.lock.json") {
    throw new Error(`Durable lineage generation-lock path drifted: ${lockPath}`);
  }
  requireRegularFile(root, lockPath);
  const lockBytes = readFileSync(resolve(root, lockPath));
  assertRecordMatches(lock, lockPath, lockBytes);
  if (
    lock.formatVersion !== "cloud-agents-platform-contract-generation-lock/v2" ||
    lock.status !== "SUCCESSOR_ASSEMBLED_REVIEW_BOUND" ||
    lock.commitSha1 !== "16275f6cbf390c343a9ac00f9193e75eaad0094e" ||
    lock.treeSha1 !== "ca595b8e1258a8b78c4da3a545b2a31d8f62b531"
  ) {
    throw new Error("Durable lineage generation-lock predecessor identity drifted.");
  }
  if (lock.mode !== "100644") throw new Error("Durable lineage generation-lock mode drifted.");
  for (const key of ["durableRegistryV1", "durableProfileV1", "durableSourceV1"] as const) {
    const record = requiredObject(predecessor[key]);
    const path = requireString(record.path, `/predecessor/${key}/path`);
    const expectedPath = {
      durableRegistryV1: V1_REGISTRY_PATH,
      durableProfileV1: V1_PROFILE_PATH,
      durableSourceV1: V1_SOURCE_PATH,
    }[key];
    if (path !== expectedPath) throw new Error(`Durable lineage predecessor path drifted: ${path}`);
    requireRegularFile(root, path);
    assertRecordMatches(record, path, readFileSync(resolve(root, path)));
  }
  const migrationRecords = requiredArray(predecessor.migrationsV1, "/predecessor/migrationsV1");
  if (migrationRecords.length !== EXPECTED_PREDECESSOR_MIGRATION_PATHS.length) {
    throw new Error("Durable lineage predecessor migration count drifted.");
  }
  for (const [index, value] of migrationRecords.entries()) {
    const record = requiredObject(value);
    const path = requireString(record.path, `/predecessor/migrationsV1/${index}/path`);
    if (path !== EXPECTED_PREDECESSOR_MIGRATION_PATHS[index]) {
      throw new Error(`Durable lineage predecessor migration path drifted: ${path}`);
    }
    requireRegularFile(root, path);
    assertRecordMatches(record, path, readFileSync(resolve(root, path)));
  }
}

function assertRecordMatches(record: JsonRecord, path: string, bytes: Uint8Array): void {
  const expectedSha = requireString(record.sha256, `${path} sha256`);
  const expectedSize = record.sizeBytes;
  if (
    record.mode !== "100644" ||
    expectedSha !== digestBytes(bytes) ||
    expectedSize !== bytes.byteLength
  ) {
    throw new Error(`Durable lineage predecessor artifact drifted: ${path}`);
  }
  if (requireString(record.gitBlobSha1, `${path} gitBlobSha1`) !== gitBlobSha1(bytes)) {
    throw new Error(`Durable lineage predecessor Git blob drifted: ${path}`);
  }
}

function predecessorArtifact(root: string, path: string): JsonRecord {
  requireRegularFile(root, path);
  const bytes = readFileSync(resolve(root, path));
  return {
    path,
    sha256: digestBytes(bytes),
    sizeBytes: bytes.byteLength,
    mode: "100644",
    gitBlobSha1: gitBlobSha1(bytes),
  };
}

function artifact(root: string, path: string): Artifact {
  requireRegularFile(root, path);
  const stat = lstatSync(resolve(root, path));
  const bytes = readFileSync(resolve(root, path));
  return {
    path,
    sha256: digestBytes(bytes),
    sizeBytes: bytes.byteLength,
    mode: (stat.mode & 0o111) === 0 ? "100644" : "100755",
  };
}

function validateJson(root: string, schemaPath: string, value: unknown, strict: boolean): void {
  const ajv = new Ajv2020({ allErrors: true, strict, validateFormats: true });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  const schema = readJson(root, schemaPath);
  ajv.addSchema(schema);
  const validate = ajv.getSchema(requireString(schema.$id, `${schemaPath} $id`));
  if (!validate || !validate(value)) {
    throw new Error(
      `Durable lineage document violates ${schemaPath}: ${ajv.errorsText(validate?.errors)}`,
    );
  }
}

function loadContractSchemas(root: string): { ajv: Ajv2020; byPath: Map<string, JsonRecord> } {
  const ajv = new Ajv2020({ allErrors: true, strict: false, validateFormats: true });
  addFormats(ajv, { formats: ["date-time", "uri"] });
  for (const keyword of [
    "x-cloud-agents-canonicalization",
    "x-cloud-agents-normalization",
    "x-cloud-agents-security",
    "x-cloud-agents-semantic-constraints",
  ])
    ajv.addKeyword(keyword);
  const byPath = new Map<string, JsonRecord>();
  for (const path of walkFiles(resolve(root, "contracts"))) {
    if (!path.endsWith(".schema.json")) continue;
    const document = JSON.parse(readFileSync(path, "utf8")) as JsonRecord;
    byPath.set(path, document);
    try {
      ajv.addSchema(document);
    } catch (error) {
      if (!(error instanceof Error) || !/already exists/u.test(error.message)) throw error;
    }
  }
  return { ajv, byPath };
}

function walkFiles(directory: string): string[] {
  const result: string[] = [];
  for (const entry of readdirSync(directory).toSorted()) {
    const path = resolve(directory, entry);
    const stat = lstatSync(path);
    if (stat.isSymbolicLink()) throw new Error(`Lineage schema closure rejects symlink: ${path}`);
    if (stat.isDirectory()) result.push(...walkFiles(path));
    else if (stat.isFile()) result.push(path);
  }
  return result;
}

function readJson(root: string, path: string): JsonRecord {
  requireRegularFile(root, path);
  const value = JSON.parse(readFileSync(resolve(root, path), "utf8")) as unknown;
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`${path} must be a JSON object.`);
  }
  return value as JsonRecord;
}

function requireRegularFile(root: string, path: string): void {
  const target = resolve(root, path);
  const candidate = relative(root, target);
  if (candidate === ".." || candidate.startsWith(`..${sep}`) || candidate.startsWith(sep)) {
    throw new Error(`Lineage input escapes repository root: ${path}`);
  }
  const stat = lstatSync(target);
  if (!stat.isFile() || stat.isSymbolicLink())
    throw new Error(`Lineage input must be a regular file: ${path}`);
}

function requiredObject(value: unknown): JsonRecord {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new TypeError("Expected object.");
  return value as JsonRecord;
}

function requiredArray(value: unknown, path: string): ReadonlyArray<unknown> {
  if (!Array.isArray(value)) throw new TypeError(`${path} must be an array.`);
  return value;
}

function requireString(value: unknown, path: string): string {
  if (typeof value !== "string" || value.length === 0)
    throw new TypeError(`${path} must be a non-empty string.`);
  return value;
}

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function gitBlobSha1(bytes: Uint8Array): string {
  return createHash("sha1").update(`blob ${bytes.byteLength}\0`).update(bytes).digest("hex");
}

function domainDigest(domain: string, value: unknown): string {
  return `sha256:${createHash("sha256").update(domain).update("\0").update(canonicalizeJson(value)).digest("hex")}`;
}
