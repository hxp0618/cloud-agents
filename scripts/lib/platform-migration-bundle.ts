import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

import {
  canonicalizeMigrationJson,
  deriveSignedInt64,
  type MigrationJson,
  migrationDigest,
  MigrationValidationError,
  parseSignedInt64Decimal,
  parseStrictMigrationJson,
} from "./platform-migration-json";
import {
  authorityProjectionDigest,
  catalogStateDigest,
  type JsonObject,
  objectIdentityKey,
  rawSha256,
  validateAttemptTerminalState,
  validateAuthorityBinding,
  validateAuthorityProfile,
  validateCatalogProjectionBody,
  validateCatalogState,
  validateDefaultACLScopeFixture,
  validateExpectedStatementTransition,
  validateIntermediateState,
  validateNumericFixture,
  validateObjectIdentity,
} from "./platform-migration-projection";
import { classifyMigrationStatement, splitPostgresStatements } from "./platform-migration-sql";
import {
  createDeterministicUstar,
  readDeterministicUstar,
  type UstarEntry,
} from "./platform-migration-ustar";

export type GeneratedMigrationBundle = {
  readonly files: ReadonlyMap<string, Uint8Array>;
  readonly manifest: JsonObject;
  readonly schemaBundleFile: JsonObject;
  readonly runtimeTar: Uint8Array;
  readonly bootstrapTar: Uint8Array;
};
export type SchemaAncestorArtifact = {
  readonly path: string;
  readonly bytes: Uint8Array;
};

const ROOT = "services/control-plane/migrations";
const MANIFEST_PATH = `${ROOT}/manifest.json`;
const SCHEMA_BUNDLE_PATH = `${ROOT}/schema-bundle.json`;
const SQL_PATHS = [
  `${ROOT}/000001_expand_migration_kernel.sql`,
  `${ROOT}/000002_expand_tenancy.sql`,
] as const;
const BOOTSTRAP_PATHS = [`${ROOT}/bootstrap/database.sql`, `${ROOT}/bootstrap/roles.sql`] as const;
const CATALOG_PATHS = [
  `${ROOT}/catalog/authority-v1.json`,
  `${ROOT}/catalog/global-table-authority-v1.json`,
  `${ROOT}/catalog/schema-000001.json`,
  `${ROOT}/catalog/schema-000002.json`,
] as const;
const PROJECTION_FIXTURE_ROOT = `${ROOT}/fixtures/projection`;
const PROJECTION_FIXTURE_PATHS = [
  `${PROJECTION_FIXTURE_ROOT}/manifest.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/authority-binding-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-state-schema-absent-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-state-schema-present-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/catalog-projection-body-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/expected-statement-transition-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/numeric-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/intermediate-state-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/attempt-terminal-state-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/negative/faults-v1.json`,
  `${PROJECTION_FIXTURE_ROOT}/golden/default-acl-scope-v1.json`,
] as const;
const AUTHORITY_DUPLICATE_RAW_PATH = `${PROJECTION_FIXTURE_ROOT}/negative/authority-binding-duplicate.raw`;
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
const MAX_PROJECTION_SCOPE_PRINCIPALS = 256;
const INITIAL_PROJECTION_SCOPE_AUTHORITY = {
  default_acl_owners: ["cloud_agents_migration_owner"],
  object_creator_closure: ["cloud_agents_migration_owner"],
} as const;
const REQUIRED_FIXTURES = [
  "ancestor-cycle",
  "ancestor-descriptor-cases",
  "ancestor-ledger",
  "duplicate-key",
  "escaped-equivalent-key",
  "ledger-rollback",
  "rfc8785",
  "signed-int64",
  "sql-split",
  "unicode-whitespace",
  "ustar",
] as const;
const LEDGER_BACKED_KEYS = [
  "migration_id",
  "migration_name",
  "predecessor_id",
  "phase",
  "schema_from",
  "schema_to",
  "compatible_binary_min",
  "compatible_binary_max",
  "sql_path",
  "sql_size_bytes",
  "sql_sha256",
  "bundle_digest",
  "transaction_mode",
  "reentrancy",
  "rollback_boundary",
  "requires_live_instance_preflight",
  "requires_pitr_preflight",
] as const;
const DECLARED_IDENTITIES_000001 = [
  "schema:unquoted:cloud_agents",
  "table:unquoted:cloud_agents/unquoted:schema_migrations",
  "function:unquoted:cloud_agents/unquoted:is_valid_identifier(unquoted:text)",
  "function:unquoted:cloud_agents/unquoted:require_tenant_id()",
] as const;
const DECLARED_IDENTITIES_000002 = [
  ...DECLARED_IDENTITIES_000001,
  "table:unquoted:cloud_agents/unquoted:platform_tenants",
  "table:unquoted:cloud_agents/unquoted:tenant_resource_versions",
  "index:unquoted:cloud_agents/unquoted:tenant_resource_versions_tenant_fk_idx",
  "table:unquoted:cloud_agents/unquoted:resource_changes",
  "index:unquoted:cloud_agents/unquoted:resource_changes_resource_history_idx",
  "index:unquoted:cloud_agents/unquoted:resource_changes_tenant_fk_idx",
  "table:unquoted:cloud_agents/unquoted:audit_facts",
  "index:unquoted:cloud_agents/unquoted:audit_facts_resource_idx",
  "index:unquoted:cloud_agents/unquoted:audit_facts_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:audit_facts_change_fk_idx",
  "table:unquoted:cloud_agents/unquoted:organizations",
  "table:unquoted:cloud_agents/unquoted:projects",
  "index:unquoted:cloud_agents/unquoted:platform_tenants_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:organizations_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:organizations_change_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_tenant_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_organization_fk_idx",
  "index:unquoted:cloud_agents/unquoted:projects_change_fk_idx",
  "policy:unquoted:cloud_agents/unquoted:platform_tenants_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:platform_tenants_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:tenant_resource_versions_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:tenant_resource_versions_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:resource_changes_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:resource_changes_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:audit_facts_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:audit_facts_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:organizations_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:organizations_migration_owner",
  "policy:unquoted:cloud_agents/unquoted:projects_runtime_tenant",
  "policy:unquoted:cloud_agents/unquoted:projects_migration_owner",
  "function:unquoted:cloud_agents/unquoted:bootstrap_platform_tenant(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
] as const;

export function buildMigrationBundle(root: string): GeneratedMigrationBundle {
  const files = new Map<string, Uint8Array>();
  const sqlBytes = new Map(SQL_PATHS.map((path) => [path, readExactFile(root, path)] as const));
  const generatedProjection = buildProjectionDocuments(sqlBytes);
  for (const [path, document] of generatedProjection.catalogDocuments) {
    files.set(path, prettyJson(document));
  }
  for (const [path, document] of generatedProjection.fixtureDocuments) {
    files.set(path, prettyJson(document));
  }
  files.set(AUTHORITY_DUPLICATE_RAW_PATH, generatedProjection.duplicateAuthorityBinding);

  const sqlArtifacts = new Map(
    SQL_PATHS.map((path) => [path, artifactRecord(path, sqlBytes.get(path)!)] as const),
  );
  const catalogArtifacts = new Map(
    CATALOG_PATHS.map((path) => [path, artifactRecord(path, files.get(path)!)] as const),
  );
  const schemaBundle: JsonObject = {
    lineage: "cloud-agents-platform",
    schema_head: "000002",
    advisory_lock: {
      domain: "cloud-agents-platform:migrations:v1",
      derivation: "sha256-first-8-bytes-signed-big-endian-int64",
      key_int64_decimal: "-1047838957622507638",
    },
    global_table_authority: catalogArtifacts.get(CATALOG_PATHS[1])!,
    projection_scope_authority: INITIAL_PROJECTION_SCOPE_AUTHORITY,
    predecessor_schema_bundle: null,
    migrations: [
      migrationEntry({
        id: "000001",
        name: "expand_migration_kernel",
        predecessor: null,
        schemaFrom: "absent",
        sql: sqlArtifacts.get(SQL_PATHS[0])!,
        predecessorCatalog: initialPredecessorContract(),
        catalog: catalogArtifacts.get(CATALOG_PATHS[2])!,
      }),
      migrationEntry({
        id: "000002",
        name: "expand_tenancy",
        predecessor: "000001",
        schemaFrom: "000001",
        sql: sqlArtifacts.get(SQL_PATHS[1])!,
        predecessorCatalog: catalogArtifacts.get(CATALOG_PATHS[2])!,
        catalog: catalogArtifacts.get(CATALOG_PATHS[3])!,
      }),
    ],
  };
  const schemaBundleDigest = migrationDigest({
    domain: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: schemaBundle,
  });
  const schemaBundleFile: JsonObject = {
    format_version: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: schemaBundle,
    schema_bundle_digest: schemaBundleDigest,
  };
  files.set(SCHEMA_BUNDLE_PATH, prettyJson(schemaBundleFile));

  const bootstrapArtifacts = BOOTSTRAP_PATHS.map((path) =>
    artifactRecord(path, readExactFile(root, path)),
  ).toSorted(compareArtifactPath);
  const bootstrapBundle: JsonObject = { artifacts: bootstrapArtifacts };
  const bootstrapBundleDigest = migrationDigest({
    domain: "cloud-agents-platform-bootstrap-bundle/v1",
    bootstrap_bundle: bootstrapBundle,
  });
  const runtimePaths = [SCHEMA_BUNDLE_PATH, ...SQL_PATHS, ...CATALOG_PATHS].toSorted();
  const runtimeArtifacts = runtimePaths.map((path) =>
    artifactRecord(path, files.get(path) ?? sqlBytes.get(path)!),
  );
  const manifestWithoutDigest: JsonObject = {
    format_version: "cloud-agents-platform-migration-manifest/v1",
    schema_bundle: schemaBundle,
    schema_bundle_digest: schemaBundleDigest,
    bootstrap_bundle: bootstrapBundle,
    bootstrap_bundle_digest: bootstrapBundleDigest,
    execution_policy: {
      statement_profile: "postgresql-ddl-v1",
      catalog_profile: "cloud-agents-platform-catalog/v1",
      authority_contract: catalogArtifacts.get(CATALOG_PATHS[0])!,
      isolation_level: "serializable",
      access_mode: "read_write",
      postgres_major_min: 15,
      postgres_major_max: 17,
      statement_timeout_ms: 300000,
      lock_timeout_ms: 30000,
      idle_in_transaction_session_timeout_ms: 60000,
      max_attempts: 3,
    },
    runtime_artifacts: runtimeArtifacts,
  };
  const manifest: JsonObject = {
    ...manifestWithoutDigest,
    manifest_digest: migrationDigest(manifestWithoutDigest),
  };
  files.set(MANIFEST_PATH, prettyJson(manifest));
  const runtimeTar = createDeterministicUstar(
    [MANIFEST_PATH, ...runtimePaths].map((path) => ({
      path,
      data: path === MANIFEST_PATH ? files.get(path)! : (files.get(path) ?? sqlBytes.get(path)!),
    })),
  );
  const bootstrapTar = createDeterministicUstar(
    BOOTSTRAP_PATHS.map((path) => ({ path, data: readExactFile(root, path) })),
  );
  return { files, manifest, schemaBundleFile, runtimeTar, bootstrapTar };
}

export function validateCheckedInMigrationBundle(root: string): GeneratedMigrationBundle {
  const expected = buildMigrationBundle(root);
  for (const [path, bytes] of expected.files) {
    const actual = readExactFile(root, path);
    if (!Buffer.from(actual).equals(Buffer.from(bytes))) {
      throw new MigrationValidationError("MIGRATION_BUNDLE_STALE", path);
    }
    if (path.endsWith(".json")) parseStrictMigrationJson(actual);
  }
  validateManifestShape(expected.manifest);
  const schemaBundleDocument = requiredObject(
    parseStrictMigrationJson(readExactFile(root, SCHEMA_BUNDLE_PATH)),
  );
  if (canonicalText(schemaBundleDocument) !== canonicalText(expected.schemaBundleFile)) {
    throw new MigrationValidationError("SCHEMA_BUNDLE_PROJECTION", "manifest/file mismatch");
  }
  validateSharedFixtureInventory(root);
  validateProjectionFixtureInventory(root, expected.files);
  validateAdvisoryLock(requiredObject(expected.manifest.schema_bundle).advisory_lock);
  const checkedInSql = new Map(SQL_PATHS.map((path) => [path, readExactFile(root, path)] as const));
  for (const path of CATALOG_PATHS.slice(2)) {
    validateCatalogStatementBindings(
      requiredObject(parseStrictMigrationJson(readExactFile(root, path))),
      checkedInSql,
    );
  }
  for (const [index, path] of SQL_PATHS.entries()) {
    const statements = splitPostgresStatements(readExactFile(root, path));
    if (statements.length === 0) throw new MigrationValidationError("SQL_EMPTY", path);
    statements.forEach((statement) => classifyMigrationStatement(statement, `00000${index + 1}`));
  }
  const parsedTar = readDeterministicUstar(expected.runtimeTar);
  validateRuntimeTarClosure(expected.manifest, parsedTar);
  const replay = createDeterministicUstar(parsedTar);
  if (!Buffer.from(replay).equals(Buffer.from(expected.runtimeTar))) {
    throw new MigrationValidationError("USTAR_SAME_BITS", "producer/consumer mismatch");
  }
  const bootstrapReplay = createDeterministicUstar(readDeterministicUstar(expected.bootstrapTar));
  validateBootstrapTarClosure(expected.manifest, readDeterministicUstar(expected.bootstrapTar));
  if (!Buffer.from(bootstrapReplay).equals(Buffer.from(expected.bootstrapTar))) {
    throw new MigrationValidationError("USTAR_SAME_BITS", "bootstrap producer/consumer mismatch");
  }
  if (expected.runtimeTar.length > 64 * 1024 * 1024) {
    throw new MigrationValidationError("USTAR_SIZE", String(expected.runtimeTar.length));
  }
  return expected;
}

export function validateCatalogStatementBindings(
  catalog: JsonObject,
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): void {
  const head = requiredString(catalog.schema_head, "catalog schema_head");
  const generatedCatalogs = buildProjectionDocuments(sqlBytes).catalogDocuments;
  const expectedSourcesByHead = new Map<string, MigrationJson>([
    ["000001", generatedCatalogs.get(CATALOG_PATHS[2])!.source_descriptors!],
    ["000002", generatedCatalogs.get(CATALOG_PATHS[3])!.source_descriptors!],
  ]);
  const declaredByHead = new Map<string, ReadonlyArray<string>>([
    ["000001", DECLARED_IDENTITIES_000001],
    ["000002", DECLARED_IDENTITIES_000002],
  ]);
  const expectedSources = expectedSourcesByHead.get(head);
  const expectedDeclared = declaredByHead.get(head);
  if (!expectedSources || !expectedDeclared) {
    throw new MigrationValidationError("CATALOG_DESCRIPTOR", `unknown schema_head ${head}`);
  }
  const actualSources = catalog.source_descriptors;
  if (
    actualSources === undefined ||
    canonicalText(actualSources) !== canonicalText(expectedSources)
  ) {
    throw new MigrationValidationError("CATALOG_STATEMENT_DESCRIPTOR_MISMATCH", head);
  }
  const declared = requiredArray(catalog.declared_object_identities).map((identity) =>
    requiredObject(identity),
  );
  declared.forEach((identity) => validateObjectIdentity(identity));
  const declaredKeys = declared.map(objectIdentityKey);
  if (new Set(declaredKeys).size !== declaredKeys.length) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITY_DUPLICATE", head);
  }
  const expectedTyped = typedIdentities(expectedDeclared);
  if (canonicalText(declared) !== canonicalText(expectedTyped)) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITIES_MISMATCH", head);
  }
  const allowlist = new Set(expectedDeclared);
  for (const source of requiredArray(actualSources).map(requiredObject)) {
    for (const statement of requiredArray(source.statements).map(requiredObject)) {
      const classificationDocument = requiredObject(statement.classification);
      const target = requiredString(
        classificationDocument.target_identity,
        "catalog statement target_identity",
      );
      if (!allowlist.has(target)) {
        throw new MigrationValidationError("CATALOG_TARGET_NOT_DECLARED", target);
      }
    }
  }
  if (
    Object.hasOwn(catalog, "expected_projection") ||
    catalog.executable_expected_projection_status !== "NOT_IMPLEMENTED_A2_1B_REQUIRED" ||
    catalog.runtime_introspection_status !== "NOT_IMPLEMENTED" ||
    catalog.publication_status !== "UNPUBLISHED_BOOTSTRAP_MUTABLE"
  ) {
    throw new MigrationValidationError("CATALOG_EXECUTABLE_PROJECTION_BOUNDARY", head);
  }
}

function validateRuntimeTarClosure(manifest: JsonObject, entries: ReadonlyArray<UstarEntry>): void {
  const byPath = new Map(entries.map((entry) => [entry.path, entry] as const));
  const records = requiredArray(manifest.runtime_artifacts).map(requiredObject);
  const expectedPaths = [
    MANIFEST_PATH,
    ...records.map((record) => requiredString(record.path, "runtime path")),
  ].toSorted();
  if ([...byPath.keys()].toSorted().join("\0") !== expectedPaths.join("\0")) {
    throw new MigrationValidationError("RUNTIME_TAR_CLOSURE", [...byPath.keys()].join(","));
  }
  const manifestBytes = byPath.get(MANIFEST_PATH)?.data;
  if (
    !manifestBytes ||
    canonicalText(requiredObject(parseStrictMigrationJson(manifestBytes))) !==
      canonicalText(manifest)
  ) {
    throw new MigrationValidationError("RUNTIME_TAR_MANIFEST", "member mismatch");
  }
  for (const record of records) {
    const path = requiredString(record.path, "runtime path");
    const data = byPath.get(path)?.data;
    if (!data || record.size_bytes !== data.length || record.sha256 !== digestBytes(data)) {
      throw new MigrationValidationError("RUNTIME_TAR_ARTIFACT", path);
    }
  }
}

function validateBootstrapTarClosure(
  manifest: JsonObject,
  entries: ReadonlyArray<UstarEntry>,
): void {
  const records = requiredArray(requiredObject(manifest.bootstrap_bundle).artifacts).map(
    requiredObject,
  );
  const byPath = new Map(entries.map((entry) => [entry.path, entry.data] as const));
  const expectedPaths = records
    .map((record) => requiredString(record.path, "bootstrap path"))
    .toSorted();
  if ([...byPath.keys()].toSorted().join("\0") !== expectedPaths.join("\0")) {
    throw new MigrationValidationError("BOOTSTRAP_TAR_CLOSURE", [...byPath.keys()].join(","));
  }
  for (const record of records) {
    const path = requiredString(record.path, "bootstrap path");
    const data = byPath.get(path);
    if (!data || record.size_bytes !== data.length || record.sha256 !== digestBytes(data)) {
      throw new MigrationValidationError("BOOTSTRAP_TAR_ARTIFACT", path);
    }
  }
}

function validateSharedFixtureInventory(root: string): void {
  const manifestPath = `${ROOT}/fixtures/bundle/manifest.json`;
  const manifest = requiredObject(parseStrictMigrationJson(readExactFile(root, manifestPath)));
  assertKeys(manifest, ["format_version", "cases"]);
  if (manifest.format_version !== "cloud-agents-platform-migration-fixtures/v1") {
    throw new MigrationValidationError("FIXTURE_VERSION", String(manifest.format_version));
  }
  const cases = requiredArray(manifest.cases).map(requiredObject);
  const names = cases.map((fixture) => requiredString(fixture.name, "fixture name"));
  if (new Set(names).size !== names.length)
    throw new MigrationValidationError("FIXTURE_DUPLICATE", names.join(","));
  if (names.toSorted().join("\0") !== [...REQUIRED_FIXTURES].toSorted().join("\0")) {
    throw new MigrationValidationError("FIXTURE_INVENTORY", names.join(","));
  }
  for (const fixture of cases) {
    const expected = requiredString(fixture.expected, "fixture expected");
    assertKeys(
      fixture,
      expected === "reject"
        ? ["name", "kind", "path", "expected", "expected_error"]
        : ["name", "kind", "path", "expected"],
    );
    const relative = requiredString(fixture.path, "fixture path");
    if (relative.startsWith("/") || relative.split("/").some((segment) => segment === "..")) {
      throw new MigrationValidationError("FIXTURE_PATH", relative);
    }
    const path = `${ROOT}/fixtures/bundle/${relative}`;
    const document = requiredObject(parseStrictMigrationJson(readExactFile(root, path)));
    if (fixture.kind === "negative_raw_json") {
      assertKeys(document, ["payload", "raw_sha256", "expected", "expected_error"]);
      const payload = requiredString(document.payload, "raw payload");
      if (payload.includes("/") || payload.includes("\\") || payload === "." || payload === "..") {
        throw new MigrationValidationError("FIXTURE_PATH", payload);
      }
      const raw = readExactFile(root, `${dirname(path)}/${payload}`);
      if (document.raw_sha256 !== digestBytes(raw)) {
        throw new MigrationValidationError("FIXTURE_RAW_DIGEST", relative);
      }
      try {
        parseStrictMigrationJson(raw);
      } catch (error) {
        if (
          !(error instanceof MigrationValidationError) ||
          error.code !== requiredString(document.expected_error, "raw expected_error")
        ) {
          throw new MigrationValidationError("FIXTURE_ERROR_MISMATCH", relative);
        }
        continue;
      }
      throw new MigrationValidationError("FIXTURE_EXPECTED_REJECT", relative);
    }
  }
}

function validateProjectionFixtureInventory(
  root: string,
  generatedFiles: ReadonlyMap<string, Uint8Array>,
): void {
  const manifest = requiredObject(
    parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[0])),
  );
  assertKeys(manifest, [
    "format_version",
    "runtime_authority",
    "publication_status",
    "runtime_introspection_status",
    "files",
  ]);
  if (
    manifest.format_version !== "cloud-agents-platform-projection-fixtures/v1" ||
    manifest.runtime_authority !== false ||
    manifest.publication_status !== "UNPUBLISHED_BOOTSTRAP_MUTABLE" ||
    manifest.runtime_introspection_status !== "NOT_IMPLEMENTED"
  ) {
    throw new MigrationValidationError("PROJECTION_FIXTURE_BOUNDARY", "status");
  }
  const records = requiredArray(manifest.files).map(requiredObject);
  const paths = records.map((record) => requiredString(record.path, "projection fixture path"));
  if (new Set(paths).size !== paths.length || paths.join("\0") !== paths.toSorted().join("\0")) {
    throw new MigrationValidationError("PROJECTION_FIXTURE_INVENTORY", paths.join(","));
  }
  for (const record of records) {
    assertKeys(record, ["path", "size_bytes", "sha256"]);
    const relative = requiredString(record.path, "projection fixture path");
    if (relative.startsWith("/") || relative.split("/").some((part) => part === "..")) {
      throw new MigrationValidationError("PROJECTION_FIXTURE_PATH", relative);
    }
    const path = `${PROJECTION_FIXTURE_ROOT}/${relative}`;
    const bytes = readExactFile(root, path);
    if (
      record.size_bytes !== bytes.length ||
      record.sha256 !== rawSha256(bytes) ||
      !generatedFiles.has(path)
    ) {
      throw new MigrationValidationError("PROJECTION_FIXTURE_DIGEST", relative);
    }
  }
  const binding = requiredObject(
    parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[1])),
  );
  validateAuthorityBinding(binding);
  validateCatalogState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[2]))),
  );
  validateCatalogState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[3]))),
  );
  validateCatalogProjectionBody(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[4]))),
  );
  validateExpectedStatementTransition(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[5]))),
  );
  validateNumericFixture(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[6]))),
  );
  validateIntermediateState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[7]))),
  );
  validateAttemptTerminalState(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[8]))),
  );
  validateDefaultACLScopeFixture(
    requiredObject(parseStrictMigrationJson(readExactFile(root, PROJECTION_FIXTURE_PATHS[10]))),
  );
  try {
    parseStrictMigrationJson(readExactFile(root, AUTHORITY_DUPLICATE_RAW_PATH));
  } catch (error) {
    if (error instanceof MigrationValidationError && error.code === "DUPLICATE_JSON_KEY") return;
    throw error;
  }
  throw new MigrationValidationError("PROJECTION_DUPLICATE_FIXTURE", "accepted");
}

export function validateSchemaAncestorChain(
  current: JsonObject,
  ancestors: ReadonlyMap<string, SchemaAncestorArtifact>,
): ReadonlyArray<JsonObject> {
  const chain: JsonObject[] = [current];
  const seen = new Set<string>();
  const seenPaths = new Set<string>();
  let cursor = current;
  for (let depth = 0; ; depth += 1) {
    if (depth > 128) throw new MigrationValidationError("ANCESTOR_LIMIT", String(depth));
    const digest = requiredString(cursor.schema_bundle_digest, "schema_bundle_digest");
    if (seen.has(digest)) throw new MigrationValidationError("ANCESTOR_CYCLE", digest);
    seen.add(digest);
    const bundle = requiredObject(cursor.schema_bundle);
    validateSchemaBundleSelf(cursor);
    const descriptor = bundle.predecessor_schema_bundle;
    if (descriptor === null) break;
    const descriptorObject = requiredObject(descriptor);
    assertKeys(descriptorObject, ["schema_bundle_digest", "path", "mode", "size_bytes", "sha256"]);
    const predecessorDigest = requiredString(
      descriptorObject.schema_bundle_digest,
      "predecessor digest",
    );
    if (seen.has(predecessorDigest)) {
      throw new MigrationValidationError("ANCESTOR_CYCLE", predecessorDigest);
    }
    const artifact = ancestors.get(predecessorDigest);
    if (!artifact) throw new MigrationValidationError("ANCESTOR_MISSING", digest);
    const path = requiredString(descriptorObject.path, "predecessor path");
    const expectedPath = `${ROOT}/archive/${predecessorDigest.slice("sha256:".length)}.schema-bundle.json`;
    if (
      !DIGEST.test(predecessorDigest) ||
      path !== expectedPath ||
      artifact.path !== path ||
      descriptorObject.mode !== "100644" ||
      descriptorObject.size_bytes !== artifact.bytes.length ||
      descriptorObject.sha256 !== digestBytes(artifact.bytes)
    ) {
      throw new MigrationValidationError("ANCESTOR_DESCRIPTOR", path);
    }
    if (seenPaths.has(path)) throw new MigrationValidationError("ANCESTOR_DUPLICATE_PATH", path);
    seenPaths.add(path);
    const predecessor = requiredObject(parseStrictMigrationJson(artifact.bytes));
    validateSchemaBundleSelf(predecessor);
    const predecessorMigrations = requiredArray(
      requiredObject(predecessor.schema_bundle).migrations,
    );
    const migrations = requiredArray(bundle.migrations);
    if (predecessorMigrations.length >= migrations.length) {
      throw new MigrationValidationError("ANCESTOR_NOT_STRICT_PREFIX", digest);
    }
    for (const [index, entry] of predecessorMigrations.entries()) {
      if (canonicalText(entry) !== canonicalText(migrations[index]!)) {
        throw new MigrationValidationError("ANCESTOR_PREFIX_MUTATION", `${digest}:${index}`);
      }
    }
    chain.push(predecessor);
    cursor = predecessor;
  }
  return chain.toReversed();
}

function validateSchemaBundleSelf(bundleFile: JsonObject): void {
  assertKeys(bundleFile, ["format_version", "schema_bundle", "schema_bundle_digest"]);
  if (bundleFile.format_version !== "cloud-agents-platform-schema-bundle/v1") {
    throw new MigrationValidationError(
      "ANCESTOR_FORMAT_VERSION",
      String(bundleFile.format_version),
    );
  }
  const digest = requiredString(bundleFile.schema_bundle_digest, "schema bundle self digest");
  const schemaBundle = requiredObject(bundleFile.schema_bundle);
  assertKeys(schemaBundle, [
    "lineage",
    "schema_head",
    "advisory_lock",
    "global_table_authority",
    "projection_scope_authority",
    "predecessor_schema_bundle",
    "migrations",
  ]);
  validateProjectionScopeAuthority(schemaBundle.projection_scope_authority);
  if (
    !DIGEST.test(digest) ||
    digest !==
      migrationDigest({
        domain: "cloud-agents-platform-schema-bundle/v1",
        schema_bundle: schemaBundle,
      })
  ) {
    throw new MigrationValidationError("ANCESTOR_SELF_DIGEST", digest);
  }
}

export function validateLedgerPrefix(
  rows: ReadonlyArray<JsonObject>,
  chain: ReadonlyArray<JsonObject>,
): void {
  const digestIndex = new Map<string, number>();
  const migrationsByDigest = new Map<string, ReadonlyArray<MigrationJson>>();
  for (const [index, bundleFile] of chain.entries()) {
    const digest = requiredString(bundleFile.schema_bundle_digest, "bundle digest");
    digestIndex.set(digest, index);
    migrationsByDigest.set(
      digest,
      requiredArray(requiredObject(bundleFile.schema_bundle).migrations),
    );
  }
  let previousIndex = -1;
  for (const [index, row] of rows.entries()) {
    const expectedId = String(index + 1).padStart(6, "0");
    if (row.migration_id !== expectedId)
      throw new MigrationValidationError("LEDGER_NOT_PREFIX", expectedId);
    const digest = requiredString(row.bundle_digest, "ledger bundle_digest");
    const chainIndex = digestIndex.get(digest);
    if (chainIndex === undefined)
      throw new MigrationValidationError("LEDGER_UNKNOWN_DIGEST", digest);
    if (chainIndex < previousIndex)
      throw new MigrationValidationError("LEDGER_BUNDLE_ROLLBACK", digest);
    const entry = migrationsByDigest.get(digest)?.[index];
    if (!entry || requiredObject(entry).id !== row.migration_id) {
      throw new MigrationValidationError("LEDGER_ENTRY_MISMATCH", expectedId);
    }
    const entryObject = requiredObject(entry);
    if (
      entryObject.sql_artifact !== undefined &&
      canonicalText(projectLedgerBackedColumns(row)) !==
        canonicalText(migrationLedgerProjection(entryObject, digest))
    ) {
      throw new MigrationValidationError("LEDGER_ENTRY_MISMATCH", expectedId);
    }
    previousIndex = chainIndex;
  }
}

export function migrationLedgerProjection(entry: JsonObject, bundleDigest: string): JsonObject {
  const sql = requiredObject(entry.sql_artifact);
  return {
    migration_id: entry.id!,
    migration_name: entry.name!,
    predecessor_id: entry.predecessor_id!,
    phase: entry.phase!,
    schema_from: entry.schema_from!,
    schema_to: entry.schema_to!,
    compatible_binary_min: entry.compatible_control_plane_min!,
    compatible_binary_max: entry.compatible_control_plane_max!,
    sql_path: sql.path!,
    sql_size_bytes: sql.size_bytes!,
    sql_sha256: sql.sha256!,
    bundle_digest: bundleDigest,
    transaction_mode: entry.transaction_mode!,
    reentrancy: entry.reentrancy!,
    rollback_boundary: entry.rollback_boundary!,
    requires_live_instance_preflight: entry.requires_live_instance_preflight!,
    requires_pitr_preflight: entry.requires_pitr_preflight!,
  };
}

function projectLedgerBackedColumns(row: JsonObject): JsonObject {
  const allowed = new Set<string>([...LEDGER_BACKED_KEYS, "applied_at", "applied_by"]);
  for (const key of Object.keys(row)) {
    if (!allowed.has(key)) throw new MigrationValidationError("LEDGER_UNKNOWN_COLUMN", key);
  }
  const projection = Object.create(null) as JsonObject;
  for (const key of LEDGER_BACKED_KEYS) {
    if (!Object.hasOwn(row, key)) throw new MigrationValidationError("LEDGER_MISSING_COLUMN", key);
    Object.defineProperty(projection, key, {
      value: row[key]!,
      enumerable: true,
      configurable: true,
      writable: true,
    });
  }
  return projection;
}

export function migrationBundlePaths(): ReadonlyArray<string> {
  return [MANIFEST_PATH, SCHEMA_BUNDLE_PATH, ...CATALOG_PATHS];
}

export function migrationStatementSourceDescriptors(
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): MigrationJson[] {
  return SQL_PATHS.map((path, migrationIndex) => ({
    migration_id: String(migrationIndex + 1).padStart(6, "0"),
    sql_sha256: digestBytes(sqlBytes.get(path)!),
    statements: splitPostgresStatements(sqlBytes.get(path)!).map((statement) => ({
      index: statement.index,
      start: statement.start,
      end: statement.end,
      sha256: statement.sha256,
      classification: classifyMigrationStatement(
        statement,
        String(migrationIndex + 1).padStart(6, "0"),
      ),
    })),
  }));
}

type ProjectionDocuments = {
  readonly catalogDocuments: ReadonlyMap<string, JsonObject>;
  readonly fixtureDocuments: ReadonlyMap<string, JsonObject>;
  readonly duplicateAuthorityBinding: Uint8Array;
};

function buildProjectionDocuments(sqlBytes: ReadonlyMap<string, Uint8Array>): ProjectionDocuments {
  const rawSources = migrationStatementSourceDescriptors(sqlBytes);
  const declared000001 = typedIdentities(DECLARED_IDENTITIES_000001);
  const declared000002 = typedIdentities(DECLARED_IDENTITIES_000002);
  const initialAbsent = initialCatalogState("schema_absent");
  const initialPresent = initialCatalogState("schema_present");
  const namespaceBody = namespaceProjectionBody([
    legacyIdentityToTyped("schema:unquoted:cloud_agents"),
  ]);
  const namespaceAfter: JsonObject = {
    state: "schema_present",
    scope: projectionScope("statement_prefix", null, "000001", 0, [
      legacyIdentityToTyped("schema:unquoted:cloud_agents"),
    ]),
    body: namespaceBody,
  };
  validateCatalogState(namespaceAfter);
  const namespaceTransition: JsonObject = {
    profile: "cloud-agents-platform-statement-transition/v1",
    catalog_before: catalogStateRef(initialAbsent),
    catalog_after: catalogStateRef(namespaceAfter),
    authority_relation: "unchanged_relative_to_verified_binding",
    control_plane_delta: [
      {
        change_kind: "create",
        object: legacyIdentityToTyped("schema:unquoted:cloud_agents"),
        grantee: null,
      },
    ],
  };
  validateExpectedStatementTransition(namespaceTransition);
  const authority = authorityProfile();
  const authorityDigest = migrationDigest(authority);
  const binding = authorityBindingFixture(authorityDigest);
  const projectionModel = catalogProjectionModel();
  const contract = (
    head: string,
    sources: ReadonlyArray<MigrationJson>,
    objects: ReadonlyArray<JsonObject>,
  ): JsonObject => ({
    format_version: "cloud-agents-platform-catalog/v1",
    contract_kind: "cumulative_schema_catalog",
    schema_head: head,
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    source_descriptors: sources,
    projection_model: projectionModel,
    declared_object_identities: objects,
    executable_expected_projection_status: "NOT_IMPLEMENTED_A2_1B_REQUIRED",
  });
  const schema000001 = contract("000001", rawSources.slice(0, 1), declared000001);
  const schema000002 = contract("000002", rawSources, declared000002);
  validateAuthorityProfile(authority);
  validateAuthorityBinding(binding);

  const intermediate = intermediateFixture(authority, binding, namespaceTransition);
  const terminal = terminalFixture(intermediate);
  const numeric = numericFixture();
  validateNumericFixture(numeric);
  validateIntermediateState(intermediate);
  validateAttemptTerminalState(terminal);
  const duplicateAuthorityBinding = duplicateBindingBytes(binding);
  const faults = projectionFaultFixture();
  const defaultACLScope: JsonObject = {
    format_version: "cloud-agents-platform-default-acl-scope-fixture/v1",
    default_acl_owners: ["cloud_agents_migration_owner"],
    object_creator_closure: ["cloud_agents_migration_owner"],
    rows: namespaceBody.default_acl!,
  };
  validateDefaultACLScopeFixture(defaultACLScope);
  const fixtureDocuments = new Map<string, JsonObject>([
    [PROJECTION_FIXTURE_PATHS[1], binding],
    [PROJECTION_FIXTURE_PATHS[2], initialAbsent],
    [PROJECTION_FIXTURE_PATHS[3], initialPresent],
    [PROJECTION_FIXTURE_PATHS[4], namespaceBody],
    [PROJECTION_FIXTURE_PATHS[5], namespaceTransition],
    [PROJECTION_FIXTURE_PATHS[6], numeric],
    [PROJECTION_FIXTURE_PATHS[7], intermediate],
    [PROJECTION_FIXTURE_PATHS[8], terminal],
    [PROJECTION_FIXTURE_PATHS[9], faults],
    [PROJECTION_FIXTURE_PATHS[10], defaultACLScope],
  ]);
  const manifest = projectionFixtureManifest(fixtureDocuments, duplicateAuthorityBinding);
  fixtureDocuments.set(PROJECTION_FIXTURE_PATHS[0], manifest);
  const catalogDocuments = new Map<string, JsonObject>([
    [CATALOG_PATHS[0], authority],
    [CATALOG_PATHS[1], globalAuthorityContract()],
    [CATALOG_PATHS[2], schema000001],
    [CATALOG_PATHS[3], schema000002],
  ]);
  return { catalogDocuments, fixtureDocuments, duplicateAuthorityBinding };
}

function authorityProfile(): JsonObject {
  return {
    format_version: "cloud-agents-platform-authority-contract/v1",
    contract_kind: "database_role_authority",
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    database: {
      encoding: "UTF8",
      locale_provider: "libc",
      datcollate: "C",
      datctype: "C",
      icu_locale: null,
      icu_rules: null,
      collation_version: null,
    },
    group_roles: [
      "cloud_agents_migration_owner",
      "cloud_agents_runtime",
      "cloud_agents_bootstrap_admin",
    ],
    required_projection_fields: [
      "phase",
      "session_user",
      "current_user",
      "database_name",
      "database_owner",
      "database_encoding",
      "locale_provider",
      "datcollate",
      "datctype",
      "icu_locale",
      "icu_rules",
      "collation_version",
      "database_acl",
      "roles",
      "direct_memberships",
      "membership_reachability",
      "database_role_settings",
      "effective_create",
      "effective_temporary",
    ],
    required_binding_fields: [
      "authority_profile_digest",
      "deployment_id",
      "issued_at",
      "expires_at",
      "security_epoch",
      "expected_projections",
    ],
  };
}

function authorityBindingFixture(authorityProfileDigest: string): JsonObject {
  const expectedProjections: JsonObject = {};
  for (const phase of ["connected_session", "migration_role", "migration_transaction"] as const) {
    expectedProjections[phase] = authorityProjectionFixture(phase);
  }
  return {
    format_version: "cloud-agents-platform-authority-binding/v1",
    authority_profile_digest: authorityProfileDigest,
    deployment_id: "fixture_pg15_17",
    issued_at: "2026-08-11T00:00:00Z",
    expires_at: "2036-08-11T00:00:00Z",
    security_epoch: 1,
    expected_projections: expectedProjections,
  };
}

function authorityProjectionFixture(phase: string): JsonObject {
  const session = "cloud_agents_migration_login_fixture";
  const bootstrapWorkload = "cloud_agents_bootstrap_login_fixture";
  const runtimeWorkload = "cloud_agents_runtime_login_fixture";
  const owner = "cloud_agents_database_owner_fixture";
  const migration = "cloud_agents_migration_owner";
  const current = phase === "connected_session" ? session : migration;
  const role = (name: string, login: boolean, inherit: boolean, superuser = false): JsonObject => ({
    name,
    login,
    inherit,
    superuser,
    create_role: false,
    create_db: false,
    replication: false,
    bypass_rls: false,
    connection_limit_int32_decimal: "-1",
    valid_until: null,
    config: [],
  });
  const roles = [
    role("cloud_agents_bootstrap_admin", false, false),
    role(bootstrapWorkload, true, true),
    role(owner, false, false),
    role(session, true, false),
    role(migration, false, false),
    role("cloud_agents_runtime", false, false),
    role(runtimeWorkload, true, true),
    role("fixture_cluster_superuser", true, true, true),
  ];
  const effectiveCreate: JsonObject = {
    cloud_agents_bootstrap_admin: false,
    cloud_agents_bootstrap_login_fixture: false,
    cloud_agents_database_owner_fixture: true,
    cloud_agents_migration_login_fixture: false,
    cloud_agents_migration_owner: true,
    cloud_agents_runtime: false,
    cloud_agents_runtime_login_fixture: false,
    fixture_cluster_superuser: true,
  };
  const effectiveTemporary: JsonObject = {
    cloud_agents_bootstrap_admin: false,
    cloud_agents_bootstrap_login_fixture: false,
    cloud_agents_database_owner_fixture: true,
    cloud_agents_migration_login_fixture: false,
    cloud_agents_migration_owner: false,
    cloud_agents_runtime: false,
    cloud_agents_runtime_login_fixture: false,
    fixture_cluster_superuser: true,
  };
  return {
    phase,
    session_user: session,
    current_user: current,
    database_name: "cloud_agents_fixture",
    database_owner: owner,
    database_encoding: "UTF8",
    locale_provider: "libc",
    datcollate: "C",
    datctype: "C",
    icu_locale: null,
    icu_rules: null,
    collation_version: null,
    database_acl: {
      catalog_value: "explicit",
      entries: [
        aclEntry(
          owner,
          owner,
          ["CONNECT", "CREATE", "TEMPORARY"],
          ["CONNECT", "CREATE", "TEMPORARY"],
        ),
        aclEntry(owner, migration, ["CREATE"], []),
      ],
    },
    roles,
    direct_memberships: [
      {
        role: "cloud_agents_bootstrap_admin",
        member: bootstrapWorkload,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: true,
        set_option: true,
      },
      {
        role: migration,
        member: session,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: false,
        set_option: true,
      },
      {
        role: "cloud_agents_runtime",
        member: runtimeWorkload,
        grantor: "fixture_cluster_superuser",
        admin_option: false,
        inherit_option: true,
        set_option: true,
      },
    ],
    membership_reachability: [
      {
        role: "cloud_agents_bootstrap_admin",
        member: bootstrapWorkload,
        privileges: [
          reachabilityPrivilege("member", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
          reachabilityPrivilege("usage", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
          reachabilityPrivilege("set", [bootstrapWorkload, "cloud_agents_bootstrap_admin"], 3),
        ],
      },
      {
        role: migration,
        member: session,
        privileges: [
          reachabilityPrivilege("member", [session, migration], 3),
          reachabilityPrivilege("usage", null, 3),
          reachabilityPrivilege("set", [session, migration], 3),
        ],
      },
      {
        role: "cloud_agents_runtime",
        member: runtimeWorkload,
        privileges: [
          reachabilityPrivilege("member", [runtimeWorkload, "cloud_agents_runtime"], 3),
          reachabilityPrivilege("usage", [runtimeWorkload, "cloud_agents_runtime"], 3),
          reachabilityPrivilege("set", [runtimeWorkload, "cloud_agents_runtime"], 3),
        ],
      },
    ],
    database_role_settings: [],
    effective_create: effectiveCreate,
    effective_temporary: effectiveTemporary,
  };
}

function reachabilityPrivilege(
  kind: string,
  witness: ReadonlyArray<string> | null,
  edgeCount: number,
): JsonObject {
  return {
    privilege_kind: kind,
    reachable: witness !== null,
    min_depth: witness === null ? null : witness.length - 1,
    canonical_witness: witness,
    edge_count: edgeCount,
  };
}

function aclEntry(
  grantor: string,
  grantee: string,
  privileges: ReadonlyArray<string>,
  grantable: ReadonlyArray<string>,
  origin = "catalog_explicit",
): JsonObject {
  return { grantor, grantee, privileges, grantable, origin };
}

function initialCatalogState(kind: "schema_absent" | "schema_present"): JsonObject {
  const scope = projectionScope("predecessor", null, "000001", null, []);
  if (kind === "schema_absent") {
    return { state: kind, scope, schema: "cloud_agents" };
  }
  return {
    state: kind,
    scope,
    body: {
      schema: {
        name: "cloud_agents",
        owner: "cloud_agents_migration_owner",
        explicit_acl: { catalog_value: "null", entries: [] },
        effective_acl: [
          aclEntry(
            "cloud_agents_migration_owner",
            "cloud_agents_migration_owner",
            ["CREATE", "USAGE"],
            ["CREATE", "USAGE"],
            "owner_implicit",
          ),
        ],
        comment: null,
        security_labels: [],
      },
      default_acl: [],
      relations: [],
      functions: [],
      dependencies: [],
      object_count: 0,
      declared_objects: [],
      denied_objects: [],
    },
  };
}

function namespaceProjectionBody(declared: ReadonlyArray<JsonObject>): JsonObject {
  const owner = "cloud_agents_migration_owner";
  return {
    schema: {
      name: "cloud_agents",
      owner,
      explicit_acl: {
        catalog_value: "explicit",
        entries: [
          aclEntry(owner, "cloud_agents_bootstrap_admin", ["USAGE"], []),
          aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"]),
          aclEntry(owner, "cloud_agents_runtime", ["USAGE"], []),
        ],
      },
      effective_acl: [
        aclEntry(owner, "cloud_agents_bootstrap_admin", ["USAGE"], []),
        aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"], "owner_implicit"),
        aclEntry(owner, "cloud_agents_runtime", ["USAGE"], []),
      ],
      comment: null,
      security_labels: [],
    },
    default_acl: [
      defaultACLProjection(null, "function", [aclEntry(owner, owner, ["EXECUTE"], ["EXECUTE"])]),
      defaultACLProjection(null, "schema", [
        aclEntry(owner, owner, ["CREATE", "USAGE"], ["CREATE", "USAGE"]),
      ]),
      defaultACLProjection(null, "sequence", [
        aclEntry(owner, owner, ["SELECT", "UPDATE", "USAGE"], ["SELECT", "UPDATE", "USAGE"]),
      ]),
      defaultACLProjection(null, "table", [
        aclEntry(
          owner,
          owner,
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
        ),
      ]),
      defaultACLProjection(null, "type", [aclEntry(owner, owner, ["USAGE"], ["USAGE"])]),
      defaultACLProjection("cloud_agents", "function", [
        aclEntry(owner, "cloud_agents_bootstrap_admin", ["EXECUTE"], []),
        aclEntry(owner, owner, ["EXECUTE"], ["EXECUTE"]),
      ]),
      defaultACLProjection("cloud_agents", "sequence", [
        aclEntry(owner, owner, ["SELECT", "UPDATE", "USAGE"], ["SELECT", "UPDATE", "USAGE"]),
      ]),
      defaultACLProjection("cloud_agents", "table", [
        aclEntry(
          owner,
          owner,
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
          ["DELETE", "INSERT", "REFERENCES", "SELECT", "TRIGGER", "TRUNCATE", "UPDATE"],
        ),
        aclEntry(owner, "cloud_agents_runtime", ["SELECT"], []),
      ]),
      defaultACLProjection("cloud_agents", "type", []),
    ],
    relations: [],
    functions: [],
    dependencies: [],
    object_count: declared.length,
    declared_objects: declared,
    denied_objects: [],
  };
}

function defaultACLProjection(
  schema: string | null,
  kind: string,
  entries: ReadonlyArray<JsonObject>,
): JsonObject {
  return {
    owner: "cloud_agents_migration_owner",
    schema,
    object_kind: kind,
    acl: {
      catalog_value: "explicit",
      entries: entries.map((entry) => ({ ...entry, origin: "default_acl_catalog" })),
    },
  };
}

function projectionScope(
  kind: "predecessor" | "statement_prefix" | "final",
  head: string | null,
  migration: string | null,
  index: number | null,
  declared: ReadonlyArray<JsonObject>,
): JsonObject {
  return {
    scope_kind: kind,
    schema_head: head,
    migration_id: migration,
    through_statement_index: index,
    declared_objects: declared,
  };
}

function catalogStateRef(state: JsonObject): JsonObject {
  return {
    scope: requiredObject(state.scope),
    state_kind: state.state!,
    digest: catalogStateDigest(state),
  };
}

function catalogProjectionModel(): JsonObject {
  return {
    projection_slice: "A2.1a_namespace_only",
    catalog_projection_fields: ["schema_head", "body"],
    body_fields: [
      "schema",
      "default_acl",
      "relations",
      "functions",
      "dependencies",
      "object_count",
      "declared_objects",
      "denied_objects",
    ],
    schema_fields: ["name", "owner", "explicit_acl", "effective_acl", "comment", "security_labels"],
    default_acl_fields: ["owner", "schema", "object_kind", "acl"],
    acl_set_fields: ["catalog_value", "entries"],
    acl_entry_fields: ["grantor", "grantee", "privileges", "grantable", "origin"],
    deferred_to_a2_1b: [
      "relation_projection",
      "function_projection",
      "dependency_projection",
      "expression_projection",
    ],
  };
}

function globalAuthorityContract(): JsonObject {
  return {
    format_version: "cloud-agents-platform-global-table-authority/v1",
    contract_kind: "global_table_writer_authority",
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    tables: [
      { name: "schema_migrations", writers: ["cloud_agents_migration_owner"] },
      { name: "workload_database_principals", writers: ["audited_bootstrap_function"] },
      { name: "builtin_roles", writers: ["cloud_agents_migration_owner"] },
      { name: "builtin_role_permissions", writers: ["cloud_agents_migration_owner"] },
    ],
  };
}

function typedIdentities(identities: ReadonlyArray<string>): JsonObject[] {
  return identities
    .map(legacyIdentityToTyped)
    .toSorted((left, right) =>
      Buffer.compare(
        Buffer.from(objectIdentityKey(left), "utf8"),
        Buffer.from(objectIdentityKey(right), "utf8"),
      ),
    );
}

function legacyIdentityToTyped(value: string): JsonObject {
  const parsed = /^(?<kind>schema|table|function|index|policy):unquoted:(?<payload>.+)$/u.exec(
    value,
  );
  if (!parsed?.groups) throw new MigrationValidationError("OBJECT_IDENTITY", value);
  const kind = parsed.groups.kind!;
  const payload = parsed.groups.payload!;
  if (kind === "schema") return { kind: "schema", name: stripLegacyName(payload) };
  const parts = payload.split("/unquoted:");
  const schema = stripLegacyName(parts[0]!);
  const nameWithSignature = parts[1]!;
  if (kind === "table") {
    return { kind: "relation", identity: { schema, name: nameWithSignature } };
  }
  if (kind === "function") {
    const match = /^(?<name>[a-z0-9_]+)\((?<arguments>.*)\)$/u.exec(nameWithSignature);
    if (!match?.groups) throw new MigrationValidationError("OBJECT_IDENTITY", value);
    const argumentsText = match.groups.arguments!;
    return {
      kind: "function",
      identity: {
        schema,
        name: match.groups.name!,
        arguments:
          argumentsText.length === 0
            ? []
            : argumentsText.split(",unquoted:").map((argument) => ({
                schema: "pg_catalog",
                name: stripLegacyName(argument),
              })),
      },
    };
  }
  if (kind === "index") {
    const relation = indexOwningRelation(nameWithSignature);
    return {
      kind: "index",
      identity: { schema, name: nameWithSignature },
      relation: { schema, name: relation },
    };
  }
  if (kind === "policy") {
    const relation = nameWithSignature.replace(/_(?:runtime_tenant|migration_owner)$/u, "");
    return { kind: "policy", relation: { schema, name: relation }, name: nameWithSignature };
  }
  throw new MigrationValidationError("OBJECT_IDENTITY", value);
}

function stripLegacyName(value: string): string {
  return value.replace(/^unquoted:/u, "");
}

function indexOwningRelation(index: string): string {
  const prefixes = [
    "tenant_resource_versions",
    "resource_changes",
    "audit_facts",
    "platform_tenants",
    "organizations",
    "projects",
  ];
  const relation = prefixes.find((prefix) => index.startsWith(`${prefix}_`));
  if (!relation) throw new MigrationValidationError("INDEX_OWNING_RELATION", index);
  return relation;
}

function numericFixture(): JsonObject {
  return {
    format_version: "cloud-agents-platform-projection-numeric-fixtures/v1",
    signed_integer: [
      numericCase({ bits: 16, input: "-32768", expected: "-32768" }),
      numericCase({ bits: 16, input: "32767", expected: "32767" }),
      numericCase({ bits: 32, input: "-2147483648", expected: "-2147483648" }),
      numericCase({ bits: 32, input: "2147483647", expected: "2147483647" }),
      numericCase({ bits: 64, input: "-9223372036854775808", expected: "-9223372036854775808" }),
      numericCase({ bits: 64, input: "0", expected: "0" }),
      numericCase({ bits: 64, input: "9223372036854775807", expected: "9223372036854775807" }),
      numericCase({
        bits: 64,
        input: "9223372036854775808",
        expected_error: "SIGNED_INT64_OUT_OF_RANGE",
      }),
      numericCase({ bits: 64, input: "-0", expected_error: "INVALID_SIGNED_INT64" }),
    ],
    exact_numeric: [
      numericCase({ input: "0", expected: "0" }),
      numericCase({ input: "0.0", expected: "0" }),
      numericCase({ input: "123.4500", expected: "123.45" }),
      numericCase({ input: "-19.125", expected: "-19.125" }),
      numericCase({ input: "-0.125", expected: "-0.125" }),
      numericCase({ input: "1e3", expected_error: "NUMERIC_FORMAT" }),
      numericCase({ input: "-0.0", expected_error: "NUMERIC_NEGATIVE_ZERO" }),
    ],
    float: [
      numericCase({ kind: "float4", input: "0", expected: "0" }),
      numericCase({ kind: "float4", input: "0.1", expected: "0.1" }),
      numericCase({ kind: "float4", input: "1.1754944e-38", expected: "1.1754944e-38" }),
      numericCase({ kind: "float8", input: "5e-324", expected: "5e-324" }),
      numericCase({ kind: "float8", input: "1.0000000000000002", expected: "1.0000000000000002" }),
      numericCase({
        kind: "float8",
        input: "1.7976931348623157e308",
        expected: "1.7976931348623157e308",
      }),
      numericCase({ kind: "float8", input: "1e+21", expected_error: "FLOAT_FORMAT" }),
      numericCase({ kind: "float8", input: "-0", expected_error: "FLOAT_VALUE" }),
      numericCase({ kind: "float8", input: "NaN", expected_error: "FLOAT_FORMAT" }),
    ],
  };
}

function numericCase(input: JsonObject): JsonObject {
  return {
    ...input,
    expected: input.expected ?? null,
    expected_error: input.expected_error ?? null,
  };
}

function intermediateFixture(
  authority: JsonObject,
  binding: JsonObject,
  transition: JsonObject,
): JsonObject {
  const migrationDigestPlaceholder = `sha256:${"1".repeat(64)}`;
  const catalogDigestPlaceholder = `sha256:${"2".repeat(64)}`;
  const authorityProfileDigest = migrationDigest(authority);
  const authorityBindingDigest = migrationDigest(binding);
  const projections = requiredObject(binding.expected_projections);
  const authorityDigest = authorityProjectionDigest(
    requiredObject(projections.migration_transaction),
  );
  const before = requiredObject(transition.catalog_before);
  const after = requiredObject(transition.catalog_after);
  const body = namespaceProjectionBody([]);
  const stateWithoutDigest: JsonObject = {
    schema_bundle_digest: migrationDigestPlaceholder,
    catalog_contract_digest: catalogDigestPlaceholder,
    authority_profile_digest: authorityProfileDigest,
    authority_binding_digest: authorityBindingDigest,
    migration_id: "000001",
    attempt_index: 1,
    statement_index: 0,
    statement_sha256: `sha256:${"3".repeat(64)}`,
    previous_attempt_terminal_digest: null,
    previous_intermediate_state_digest: null,
    control_plane_states: {
      tx_status: "T",
      session_user: "cloud_agents_migration_login_fixture",
      current_user: "cloud_agents_migration_owner",
      migration_role: "cloud_agents_migration_owner",
      advisory_lock: {
        domain: "cloud-agents-platform:migrations:v1",
        key_int64_decimal: "-1047838957622507638",
        held: true,
      },
      verified_authority_decision_digest: `sha256:${"4".repeat(64)}`,
      schema_owner: "cloud_agents_migration_owner",
      schema_explicit_acl_digest: migrationDigest(
        requiredObject(requiredObject(body.schema).explicit_acl),
      ),
      schema_effective_acl_digest: migrationDigest(requiredObject(body.schema).effective_acl!),
      default_acl_digest: migrationDigest(body.default_acl!),
      expected_transition_digest: migrationDigest(transition),
    },
    authority_before_digest: authorityDigest,
    authority_after_digest: authorityDigest,
    catalog_before_digest: before.digest!,
    catalog_after_digest: after.digest!,
  };
  return {
    ...stateWithoutDigest,
    intermediate_state_digest: migrationDigest({
      domain: "cloud-agents-platform-intermediate-state/v1",
      ...stateWithoutDigest,
    }),
  };
}

function terminalFixture(intermediate: JsonObject): JsonObject {
  const withoutDigest: JsonObject = {
    schema_bundle_digest: intermediate.schema_bundle_digest!,
    catalog_contract_digest: intermediate.catalog_contract_digest!,
    authority_profile_digest: intermediate.authority_profile_digest!,
    authority_binding_digest: intermediate.authority_binding_digest!,
    migration_id: "000001",
    attempt_index: 1,
    previous_attempt_terminal_digest: null,
    last_intermediate_state_digest: intermediate.intermediate_state_digest!,
    outcome: "committed",
    stable_error_code: null,
    reconcile_result: "not_run",
  };
  return {
    ...withoutDigest,
    terminal_digest: migrationDigest({
      domain: "cloud-agents-platform-attempt-terminal/v1",
      ...withoutDigest,
    }),
  };
}

function duplicateBindingBytes(binding: JsonObject): Uint8Array {
  const valid = new TextDecoder().decode(prettyJson(binding));
  return new TextEncoder().encode(
    valid.replace(
      '  "format_version": "cloud-agents-platform-authority-binding/v1",',
      '  "format_version": "cloud-agents-platform-authority-binding/v1",\n  "format_version": "cloud-agents-platform-authority-binding/v1",',
    ),
  );
}

function projectionFaultFixture(): JsonObject {
  return {
    format_version: "cloud-agents-platform-projection-faults/v1",
    cases: [
      {
        name: "authority_unknown",
        target: "authority_binding",
        mutation: "unknown_field",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "authority_missing",
        target: "authority_binding",
        mutation: "missing_expires_at",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "authority_duplicate",
        target: "authority_binding_raw",
        mutation: "duplicate_format_version",
        expected_error: "DUPLICATE_JSON_KEY",
      },
      {
        name: "authority_phase",
        target: "authority_binding",
        mutation: "phase_mismatch",
        expected_error: "AUTHORITY_PHASE",
      },
      {
        name: "authority_digest",
        target: "authority_binding",
        mutation: "bad_profile_digest",
        expected_error: "DIGEST_FORMAT",
      },
      {
        name: "authority_acl",
        target: "authority_binding",
        mutation: "null_acl_with_entries",
        expected_error: "ACL_NULL_ENTRIES",
      },
      {
        name: "catalog_scope",
        target: "catalog_state",
        mutation: "final_without_head",
        expected_error: "PROJECTION_SCOPE",
      },
      {
        name: "predecessor_scope_head",
        target: "catalog_state",
        mutation: "predecessor_with_head",
        expected_error: "PROJECTION_SCOPE",
      },
      {
        name: "absent_schema",
        target: "catalog_state",
        mutation: "wrong_schema_name",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "present_closure",
        target: "catalog_state",
        mutation: "scope_body_declared_mismatch",
        expected_error: "CATALOG_STATE_DECLARED_CLOSURE",
      },
      {
        name: "a21a_relation",
        target: "catalog_body",
        mutation: "relation_nonempty",
        expected_error: "A21A_RELATIONS_NOT_IMPLEMENTED",
      },
      {
        name: "dependency_duplicate",
        target: "catalog_body",
        mutation: "duplicate_dependency",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "denied_duplicate",
        target: "catalog_body",
        mutation: "duplicate_denied_object",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "trigger_owner_variant",
        target: "object_identity",
        mutation: "trigger_owner_not_constraint",
        expected_error: "TRIGGER_OWNING_CONSTRAINT",
      },
      {
        name: "transition_digest",
        target: "expected_transition",
        mutation: "bad_after_digest",
        expected_error: "DIGEST_FORMAT",
      },
      {
        name: "transition_unknown",
        target: "expected_transition",
        mutation: "unknown_field",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "transition_object",
        target: "expected_transition",
        mutation: "open_object_identity",
        expected_error: "UNKNOWN_OR_MISSING_FIELD",
      },
      {
        name: "numeric_signed",
        target: "numeric",
        mutation: "signed_overflow",
        expected_error: "SIGNED_INT64_OUT_OF_RANGE",
      },
      {
        name: "uint32_overflow",
        target: "intermediate",
        mutation: "statement_index_overflow",
        expected_error: "UINT32_RANGE",
      },
      {
        name: "numeric_exact",
        target: "numeric",
        mutation: "numeric_exponent",
        expected_error: "NUMERIC_FORMAT",
      },
      {
        name: "numeric_float",
        target: "numeric",
        mutation: "float_nan",
        expected_error: "FLOAT_FORMAT",
      },
      {
        name: "intermediate_digest",
        target: "intermediate",
        mutation: "bad_digest",
        expected_error: "INTERMEDIATE_DIGEST",
      },
      {
        name: "intermediate_attempt_link",
        target: "intermediate",
        mutation: "attempt_two_without_previous_terminal",
        expected_error: "INTERMEDIATE_ATTEMPT_LINK",
      },
      {
        name: "intermediate_statement_link",
        target: "intermediate",
        mutation: "statement_one_without_previous_intermediate",
        expected_error: "INTERMEDIATE_STATEMENT_LINK",
      },
      {
        name: "advisory_identity",
        target: "intermediate",
        mutation: "wrong_advisory_domain",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "attempt_outcome",
        target: "attempt_terminal",
        mutation: "illegal_combination",
        expected_error: "ATTEMPT_TERMINAL_COMBINATION",
      },
      {
        name: "attempt_link",
        target: "attempt_terminal",
        mutation: "attempt_two_without_previous_terminal",
        expected_error: "ATTEMPT_TERMINAL_LINK",
      },
      {
        name: "ambiguous_committed_last_digest",
        target: "attempt_terminal",
        mutation: "ambiguous_committed_without_last_digest",
        expected_error: "ATTEMPT_TERMINAL_COMBINATION",
      },
      {
        name: "authority_icu",
        target: "authority_profile",
        mutation: "icu_locale_nonnull",
        expected_error: "UNEXPECTED_VALUE",
      },
      {
        name: "acl_surface_origin",
        target: "catalog_body",
        mutation: "default_acl_catalog_explicit_origin",
        expected_error: "ACL_ORIGIN",
      },
      {
        name: "acl_surface_privilege",
        target: "catalog_body",
        mutation: "schema_select_privilege",
        expected_error: "ACL_PRIVILEGE",
      },
      {
        name: "role_config_secret",
        target: "authority_binding",
        mutation: "password_setting",
        expected_error: "ROLE_CONFIG_UNSAFE",
      },
      {
        name: "reachability_complete_edge_count",
        target: "authority_binding",
        mutation: "unreachable_edge_count_zero",
        expected_error: "REACHABILITY_EDGE_COUNT",
      },
      {
        name: "reachability_member_to_role_order",
        target: "canonical_membership_witness_synthetic",
        mutation: "reverse_member_role_witness",
        expected_error: "REACHABILITY_WITNESS",
      },
      {
        name: "reachability_equal_length_noncanonical",
        target: "canonical_membership_witness_synthetic",
        mutation: "select_utf8_later_shortest_path",
        expected_error: "REACHABILITY_WITNESS",
      },
      {
        name: "reachability_duplicate_logical_edge",
        target: "canonical_membership_witness_synthetic",
        mutation: "duplicate_member_role_endpoint",
        expected_error: "DIRECT_MEMBERSHIP_DUPLICATE",
      },
      {
        name: "default_acl_invalid_schema_kind_scope",
        target: "default_acl_scope",
        mutation: "schema_kind_scoped_to_cloud_agents",
        expected_error: "DEFAULT_ACL_SCHEMA_KIND_SCOPE",
      },
      {
        name: "default_acl_unknown_schema",
        target: "default_acl_scope",
        mutation: "schema_outside_closed_scope",
        expected_error: "DEFAULT_ACL_SCOPE",
      },
      {
        name: "default_acl_catalog_value",
        target: "default_acl_scope",
        mutation: "catalog_value_null",
        expected_error: "DEFAULT_ACL_CATALOG_VALUE",
      },
      {
        name: "default_acl_owner_creator_closure",
        target: "default_acl_scope",
        mutation: "owner_outside_creator_closure",
        expected_error: "DEFAULT_ACL_OWNER_CLOSURE",
      },
      {
        name: "default_acl_outer_order",
        target: "default_acl_scope",
        mutation: "reverse_rows",
        expected_error: "DUPLICATE_OR_UNSORTED",
      },
      {
        name: "stable_error_unknown",
        target: "attempt_terminal",
        mutation: "unknown_projection_error_code",
        expected_error: "STABLE_ERROR_CODE",
      },
      {
        name: "stable_error_runner_code",
        target: "attempt_terminal",
        mutation: "legacy_runner_error_code",
        expected_error: "STABLE_ERROR_CODE",
      },
    ],
  };
}

function projectionFixtureManifest(
  documents: ReadonlyMap<string, JsonObject>,
  duplicateRaw: Uint8Array,
): JsonObject {
  const cases = [...documents.entries()]
    .map(([path, document]) => ({
      path: path.slice(`${PROJECTION_FIXTURE_ROOT}/`.length),
      size_bytes: prettyJson(document).length,
      sha256: rawSha256(prettyJson(document)),
    }))
    .toSorted((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  cases.push({
    path: AUTHORITY_DUPLICATE_RAW_PATH.slice(`${PROJECTION_FIXTURE_ROOT}/`.length),
    size_bytes: duplicateRaw.length,
    sha256: rawSha256(duplicateRaw),
  });
  cases.sort((left, right) => Buffer.compare(Buffer.from(left.path), Buffer.from(right.path)));
  return {
    format_version: "cloud-agents-platform-projection-fixtures/v1",
    runtime_authority: false,
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    files: cases,
  };
}

function migrationEntry(input: {
  readonly id: string;
  readonly name: string;
  readonly predecessor: string | null;
  readonly schemaFrom: string;
  readonly sql: JsonObject;
  readonly predecessorCatalog: JsonObject;
  readonly catalog: JsonObject;
}): JsonObject {
  return {
    id: input.id,
    name: input.name,
    predecessor_id: input.predecessor,
    phase: "expand",
    schema_from: input.schemaFrom,
    schema_to: input.id,
    compatible_control_plane_min: "0.1.0-alpha.1",
    compatible_control_plane_max: "0.2.0-0",
    compatible_worker_min: "0.1.0-alpha.1",
    compatible_worker_max: "0.2.0-0",
    sql_artifact: input.sql,
    transaction_mode: "transactional",
    reentrancy: "ledger_guarded",
    rollback_boundary: "retain_expanded_schema",
    requires_live_instance_preflight: false,
    requires_pitr_preflight: false,
    predecessor_catalog_contract: input.predecessorCatalog,
    catalog_contract: input.catalog,
  };
}

function initialPredecessorContract(): JsonObject {
  return {
    accepted_states: [initialCatalogState("schema_absent"), initialCatalogState("schema_present")],
  };
}

function validateManifestShape(manifest: JsonObject): void {
  assertKeys(manifest, [
    "format_version",
    "schema_bundle",
    "schema_bundle_digest",
    "bootstrap_bundle",
    "bootstrap_bundle_digest",
    "execution_policy",
    "runtime_artifacts",
    "manifest_digest",
  ]);
  if (manifest.format_version !== "cloud-agents-platform-migration-manifest/v1")
    throw new MigrationValidationError("MANIFEST_VERSION", String(manifest.format_version));
  for (const field of [
    "schema_bundle_digest",
    "bootstrap_bundle_digest",
    "manifest_digest",
  ] as const) {
    if (typeof manifest[field] !== "string" || !DIGEST.test(manifest[field]))
      throw new MigrationValidationError("DIGEST_FORMAT", field);
  }
  const withoutDigest = { ...manifest };
  delete withoutDigest.manifest_digest;
  if (manifest.manifest_digest !== migrationDigest(withoutDigest))
    throw new MigrationValidationError("MANIFEST_DIGEST", "mismatch");
  const schema = requiredObject(manifest.schema_bundle);
  assertKeys(schema, [
    "lineage",
    "schema_head",
    "advisory_lock",
    "global_table_authority",
    "projection_scope_authority",
    "predecessor_schema_bundle",
    "migrations",
  ]);
  validateProjectionScopeAuthority(schema.projection_scope_authority);
  if (
    manifest.schema_bundle_digest !==
    migrationDigest({ domain: "cloud-agents-platform-schema-bundle/v1", schema_bundle: schema })
  )
    throw new MigrationValidationError("SCHEMA_BUNDLE_DIGEST", "mismatch");
  const bootstrap = requiredObject(manifest.bootstrap_bundle);
  if (
    manifest.bootstrap_bundle_digest !==
    migrationDigest({
      domain: "cloud-agents-platform-bootstrap-bundle/v1",
      bootstrap_bundle: bootstrap,
    })
  ) {
    throw new MigrationValidationError("BOOTSTRAP_BUNDLE_DIGEST", "mismatch");
  }
  const runtime = requiredArray(manifest.runtime_artifacts).map(requiredObject);
  validateRuntimeArtifactSafety(runtime);
  const paths = runtime.map((record) => requiredString(record.path, "artifact path"));
  if (paths.join("\0") !== [...paths].toSorted().join("\0") || new Set(paths).size !== paths.length)
    throw new MigrationValidationError("RUNTIME_ARTIFACT_ORDER", paths.join(","));
  runtime.forEach(validateArtifactShape);
}

export function validateRuntimeArtifactSafety(records: ReadonlyArray<JsonObject>): void {
  for (const record of records) {
    const path = requiredString(record.path, "runtime path");
    if (/authority-binding|\/fixtures\/|secret|credential/iu.test(path)) {
      throw new MigrationValidationError(
        "RUNTIME_TAR_DEPLOYMENT_AUTHORITY",
        `deployment authority or secret material is forbidden: ${path}`,
      );
    }
  }
}

export function validateProjectionScopeAuthority(value: MigrationJson): void {
  const authority = requiredObject(value);
  assertKeys(authority, ["default_acl_owners", "object_creator_closure"]);
  const owners = validateProjectionScopeRoles(
    authority.default_acl_owners,
    "projection_scope_authority.default_acl_owners",
  );
  const creators = validateProjectionScopeRoles(
    authority.object_creator_closure,
    "projection_scope_authority.object_creator_closure",
  );
  const creatorSet = new Set(creators);
  for (const owner of owners) {
    if (!creatorSet.has(owner)) {
      throw new MigrationValidationError(
        "PROJECTION_SCOPE_AUTHORITY_CLOSURE",
        `${owner} is outside object_creator_closure`,
      );
    }
  }
}

function validateProjectionScopeRoles(value: MigrationJson, label: string): string[] {
  const roles = requiredArray(value).map((role) => requiredString(role, label));
  if (roles.length === 0 || roles.length > MAX_PROJECTION_SCOPE_PRINCIPALS) {
    throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_LIMIT", label);
  }
  for (const role of roles) {
    if (role.length === 0 || !role.isWellFormed() || role.includes("\0")) {
      throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_ROLE", `${label}:${role}`);
    }
  }
  const sorted = [...roles].toSorted((left, right) =>
    Buffer.compare(Buffer.from(left, "utf8"), Buffer.from(right, "utf8")),
  );
  if (new Set(roles).size !== roles.length || roles.join("\0") !== sorted.join("\0")) {
    throw new MigrationValidationError("PROJECTION_SCOPE_AUTHORITY_ORDER", label);
  }
  return roles;
}

function validateAdvisoryLock(value: MigrationJson): void {
  const lock = requiredObject(value);
  assertKeys(lock, ["domain", "derivation", "key_int64_decimal"]);
  const domain = requiredString(lock.domain, "advisory domain");
  if (lock.derivation !== "sha256-first-8-bytes-signed-big-endian-int64")
    throw new MigrationValidationError("ADVISORY_DERIVATION", String(lock.derivation));
  const key = parseSignedInt64Decimal(requiredString(lock.key_int64_decimal, "advisory key"));
  if (key !== deriveSignedInt64(domain) || key !== -1047838957622507638n)
    throw new MigrationValidationError("ADVISORY_KEY", key.toString());
}

function artifactRecord(path: string, bytes: Uint8Array): JsonObject {
  return { path, mode: "100644", size_bytes: bytes.length, sha256: digestBytes(bytes) };
}

function validateArtifactShape(record: JsonObject): void {
  assertKeys(record, ["path", "mode", "size_bytes", "sha256"]);
  if (
    record.mode !== "100644" ||
    typeof record.size_bytes !== "number" ||
    typeof record.sha256 !== "string" ||
    !DIGEST.test(record.sha256)
  )
    throw new MigrationValidationError("ARTIFACT_RECORD", String(record.path));
}

function readExactFile(root: string, path: string): Uint8Array {
  const absolute = resolve(root, path);
  const stat = lstatSync(absolute, { throwIfNoEntry: false });
  if (!stat?.isFile() || stat.isSymbolicLink() || (stat.mode & 0o111) !== 0)
    throw new MigrationValidationError("ARTIFACT_FILE", path);
  return readFileSync(absolute);
}

function digestBytes(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function prettyJson(value: MigrationJson): Uint8Array {
  return new TextEncoder().encode(`${formatJson(value, 0, 0)}\n`);
}

function formatJson(value: MigrationJson, indent: number, prefixLength: number): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  const padding = " ".repeat(indent);
  const childPadding = " ".repeat(indent + 2);
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    if (value.every((entry) => entry === null || typeof entry !== "object")) {
      const inline = `[${value.map((entry) => JSON.stringify(entry)).join(", ")}]`;
      if (indent + prefixLength + inline.length <= 100) return inline;
    }
    return `[\n${value
      .map((entry) => `${childPadding}${formatJson(entry, indent + 2, 0)}`)
      .join(",\n")}\n${padding}]`;
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return "{}";
  return `{\n${entries
    .map(([key, entry]) => {
      const prefix = `${childPadding}${JSON.stringify(key)}: `;
      return `${prefix}${formatJson(entry, indent + 2, JSON.stringify(key).length + 2)}`;
    })
    .join(",\n")}\n${padding}}`;
}

function compareArtifactPath(left: JsonObject, right: JsonObject): number {
  return Buffer.compare(
    Buffer.from(requiredString(left.path, "path"), "ascii"),
    Buffer.from(requiredString(right.path, "path"), "ascii"),
  );
}

function assertKeys(value: JsonObject, expected: ReadonlyArray<string>): void {
  const actual = Object.keys(value).toSorted();
  const wanted = [...expected].toSorted();
  if (actual.join("\0") !== wanted.join("\0"))
    throw new MigrationValidationError(
      "UNKNOWN_OR_MISSING_FIELD",
      `${actual.join(",")} != ${wanted.join(",")}`,
    );
}

function requiredObject(value: MigrationJson): JsonObject {
  if (typeof value !== "object" || value === null || Array.isArray(value))
    throw new MigrationValidationError("EXPECTED_OBJECT", String(value));
  return value;
}

function requiredArray(value: MigrationJson): MigrationJson[] {
  if (!Array.isArray(value)) throw new MigrationValidationError("EXPECTED_ARRAY", String(value));
  return value;
}

function requiredString(value: MigrationJson, label: string): string {
  if (typeof value !== "string") throw new MigrationValidationError("EXPECTED_STRING", label);
  return value;
}

function canonicalText(value: MigrationJson): string {
  return new TextDecoder().decode(canonicalizeMigrationJson(value));
}
