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
import { classifyMigrationStatement, splitPostgresStatements } from "./platform-migration-sql";
import {
  createDeterministicUstar,
  readDeterministicUstar,
  type UstarEntry,
} from "./platform-migration-ustar";

type JsonObject = { [key: string]: MigrationJson };
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
const DIGEST = /^sha256:[0-9a-f]{64}$/u;
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
  const catalogDocuments = buildCatalogDocuments(sqlBytes);
  for (const [path, document] of catalogDocuments) files.set(path, prettyJson(document));

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
  return expected;
}

export function validateCatalogStatementBindings(
  catalog: JsonObject,
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): void {
  const head = requiredString(catalog.schema_head, "catalog schema_head");
  const expectedSourcesByHead = new Map<string, MigrationJson>([
    ["000001", migrationStatementSourceDescriptors(sqlBytes).slice(0, 1)],
    ["000002", migrationStatementSourceDescriptors(sqlBytes)],
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
    requiredString(identity, "catalog declared identity"),
  );
  if (new Set(declared).size !== declared.length) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITY_DUPLICATE", head);
  }
  if (canonicalText(declared) !== canonicalText(expectedDeclared)) {
    throw new MigrationValidationError("CATALOG_DECLARED_IDENTITIES_MISMATCH", head);
  }
  const allowlist = new Set(declared);
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
  if (
    !DIGEST.test(digest) ||
    digest !==
      migrationDigest({
        domain: "cloud-agents-platform-schema-bundle/v1",
        schema_bundle: requiredObject(bundleFile.schema_bundle),
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

function buildCatalogDocuments(
  sqlBytes: ReadonlyMap<string, Uint8Array>,
): ReadonlyMap<string, JsonObject> {
  const statementSources = migrationStatementSourceDescriptors(sqlBytes);
  const projectionModel: JsonObject = {
    schema_fields: ["name", "owner", "effective_acl", "comment", "security_labels"],
    default_acl_fields: ["owner", "schema", "object_kind", "grantee", "privileges", "grantable"],
    relation_fields: [
      "identity",
      "relkind",
      "persistence",
      "access_method",
      "owner",
      "acl",
      "reloptions",
      "replica_identity",
      "rls_enabled",
      "rls_forced",
    ],
    column_fields: [
      "attnum",
      "name",
      "type",
      "typmod",
      "collation",
      "nullable",
      "identity",
      "generated",
      "default",
      "storage",
      "compression",
    ],
    constraint_fields: [
      "name",
      "type",
      "columns",
      "referenced_relation",
      "referenced_columns",
      "match",
      "update",
      "delete",
      "deferrable",
      "deferred",
      "validated",
      "expression",
    ],
    index_fields: [
      "name",
      "access_method",
      "keys",
      "includes",
      "opclass",
      "collation",
      "order",
      "nulls",
      "unique",
      "primary",
      "valid",
      "ready",
      "live",
      "predicate",
      "expression",
    ],
    policy_fields: ["name", "permissive", "command", "roles", "using", "with_check"],
    trigger_fields: [
      "identity",
      "function",
      "enabled",
      "type",
      "columns",
      "arguments",
      "when",
      "internal",
    ],
    function_fields: [
      "identity",
      "kind",
      "language",
      "arguments",
      "returns",
      "owner",
      "acl",
      "security_definer",
      "volatility",
      "parallel",
      "leakproof",
      "strict",
      "config",
      "cost",
      "rows",
      "prosrc_sha256",
      "probin",
    ],
    expression_profile: "cloud-agents-sql-expression/v1",
    denied_object_kinds: [
      "sequence",
      "view",
      "materialized_view",
      "foreign_table",
      "partition",
      "operator",
      "cast",
      "extension",
    ],
  };
  const authority: JsonObject = {
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
      "session_user",
      "current_user",
      "role_attributes",
      "direct_membership",
      "recursive_membership",
      "membership_grantor_options",
      "role_config",
      "database_owner",
      "database_acl",
      "database_role_settings",
      "effective_create",
      "effective_temporary",
    ],
  };
  const globalAuthority: JsonObject = {
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
  const contract = (
    head: string,
    sources: ReadonlyArray<MigrationJson>,
    objects: ReadonlyArray<string>,
  ): JsonObject => ({
    format_version: "cloud-agents-platform-catalog/v1",
    contract_kind: "cumulative_schema_catalog",
    schema_head: head,
    publication_status: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
    runtime_introspection_status: "NOT_IMPLEMENTED",
    source_descriptors: sources,
    projection_model: projectionModel,
    declared_object_identities: objects,
  });
  return new Map([
    [CATALOG_PATHS[0], authority],
    [CATALOG_PATHS[1], globalAuthority],
    [
      CATALOG_PATHS[2],
      contract("000001", statementSources.slice(0, 1), DECLARED_IDENTITIES_000001),
    ],
    [CATALOG_PATHS[3], contract("000002", statementSources, DECLARED_IDENTITIES_000002)],
  ]);
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
    accepted_states: [
      { state: "schema_absent", schema: "cloud_agents" },
      {
        state: "empty_schema",
        schema: "cloud_agents",
        owner: "cloud_agents_migration_owner",
        effective_acl: [
          {
            grantee: "cloud_agents_migration_owner",
            privileges: ["CREATE", "USAGE"],
            grantable: ["CREATE", "USAGE"],
          },
        ],
        object_count: 0,
        comment: null,
        security_labels: [],
      },
    ],
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
  const paths = runtime.map((record) => requiredString(record.path, "artifact path"));
  if (paths.join("\0") !== [...paths].toSorted().join("\0") || new Set(paths).size !== paths.length)
    throw new MigrationValidationError("RUNTIME_ARTIFACT_ORDER", paths.join(","));
  runtime.forEach(validateArtifactShape);
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
