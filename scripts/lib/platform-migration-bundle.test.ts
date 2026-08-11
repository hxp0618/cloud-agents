import { resolve } from "node:path";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  buildMigrationBundle,
  migrationLedgerProjection,
  migrationStatementSourceDescriptors,
  validateCatalogStatementBindings,
  validateCheckedInMigrationBundle,
  validateLedgerPrefix,
  validateSchemaAncestorChain,
} from "./platform-migration-bundle";
import { migrationDigest } from "./platform-migration-json";
import { createDeterministicUstar, readDeterministicUstar } from "./platform-migration-ustar";

const root = resolve(import.meta.dirname, "../..");

describe("migration bundle bootstrap", () => {
  it("matches checked-in exact bytes and preserves explicit open boundaries", () => {
    const bundle = validateCheckedInMigrationBundle(root);
    expect(bundle.manifest.schema_bundle_digest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(bundle.manifest.manifest_digest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(
      new TextDecoder().decode(
        bundle.files.get("services/control-plane/migrations/catalog/schema-000002.json"),
      ),
    ).toContain('"runtime_introspection_status": "NOT_IMPLEMENTED"');
  });

  it("detects generated source drift", () => {
    const first = buildMigrationBundle(root);
    const second = buildMigrationBundle(root);
    expect(Buffer.from(first.runtimeTar).equals(Buffer.from(second.runtimeTar))).toBe(true);
    expect(Buffer.from(first.bootstrapTar).equals(Buffer.from(second.bootstrapTar))).toBe(true);
    expect(readDeterministicUstar(first.bootstrapTar).map((entry) => entry.path)).toEqual([
      "services/control-plane/migrations/bootstrap/database.sql",
      "services/control-plane/migrations/bootstrap/roles.sql",
    ]);
  });

  it("binds every catalog statement descriptor to exact SQL classification", () => {
    const catalog = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/catalog/schema-000002.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    const sql = new Map(
      [
        "services/control-plane/migrations/000001_expand_migration_kernel.sql",
        "services/control-plane/migrations/000002_expand_tenancy.sql",
      ].map((path) => [path, readFileSync(resolve(root, path))] as const),
    );
    expect(() => validateCatalogStatementBindings(catalog, sql)).not.toThrow();
    const drift = structuredClone(catalog) as {
      source_descriptors: Array<{
        statements: Array<{ classification: { command: string } }>;
      }>;
    };
    drift.source_descriptors[0]!.statements[0]!.classification.command = "CREATE";
    expect(() => validateCatalogStatementBindings(drift, sql)).toThrow(
      /CATALOG_STATEMENT_DESCRIPTOR_MISMATCH/,
    );
  });

  it("rejects unknown catalog targets even when SQL and source descriptors drift together", () => {
    const catalog = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/catalog/schema-000002.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    const paths = [
      "services/control-plane/migrations/000001_expand_migration_kernel.sql",
      "services/control-plane/migrations/000002_expand_tenancy.sql",
    ] as const;
    const original = new Map(paths.map((path) => [path, readFileSync(resolve(root, path))]));
    const faults = [
      {
        name: "unknown_table",
        path: paths[1],
        from: "CREATE TABLE cloud_agents.platform_tenants",
        to: "CREATE TABLE cloud_agents.unknown_table",
      },
      {
        name: "unknown_function",
        path: paths[0],
        from: "CREATE FUNCTION cloud_agents.is_valid_identifier",
        to: "CREATE FUNCTION cloud_agents.unknown_function",
      },
      {
        name: "unknown_index",
        path: paths[1],
        from: "CREATE INDEX tenant_resource_versions_tenant_fk_idx",
        to: "CREATE INDEX unknown_index",
      },
      {
        name: "unknown_policy",
        path: paths[1],
        from: "CREATE POLICY platform_tenants_runtime_tenant",
        to: "CREATE POLICY unknown_policy",
      },
      {
        name: "unknown_grant_target",
        path: paths[0],
        from: "GRANT SELECT ON TABLE cloud_agents.schema_migrations",
        to: "GRANT SELECT ON TABLE cloud_agents.unknown_grant_target",
      },
    ] as const;
    for (const fault of faults) {
      const sql = new Map(original);
      const source = new TextDecoder().decode(sql.get(fault.path)!);
      expect(source.includes(fault.from), fault.name).toBe(true);
      sql.set(fault.path, new TextEncoder().encode(source.replace(fault.from, fault.to)));
      const drift = structuredClone(catalog) as Record<string, unknown>;
      drift.source_descriptors = migrationStatementSourceDescriptors(sql);
      expect(() => validateCatalogStatementBindings(drift, sql), fault.name).toThrow(
        /CATALOG_TARGET_NOT_DECLARED/,
      );
    }
  });

  it("validates ancestor strict-prefix and ledger monotonicity", () => {
    const oldest = schemaBundleFile([{ id: "000001" }], null);
    const oldestPath = `services/control-plane/migrations/archive/${String(
      oldest.document.schema_bundle_digest,
    ).slice("sha256:".length)}.schema-bundle.json`;
    const predecessor = {
      schema_bundle_digest: oldest.document.schema_bundle_digest,
      path: oldestPath,
      mode: "100644",
      size_bytes: oldest.bytes.length,
      sha256: `sha256:${createHash("sha256").update(oldest.bytes).digest("hex")}`,
    };
    const current = schemaBundleFile([{ id: "000001" }, { id: "000002" }], predecessor);
    const chain = validateSchemaAncestorChain(
      current.document,
      new Map([
        [String(oldest.document.schema_bundle_digest), { path: oldestPath, bytes: oldest.bytes }],
      ]),
    );
    expect(chain).toHaveLength(2);
  });

  it("rejects noncanonical ancestor descriptors and raw artifact drift", () => {
    const oldest = schemaBundleFile([{ id: "000001" }], null);
    const digest = String(oldest.document.schema_bundle_digest);
    const path = `services/control-plane/migrations/archive/${digest.slice("sha256:".length)}.schema-bundle.json`;
    const valid = {
      schema_bundle_digest: digest,
      path,
      mode: "100644",
      size_bytes: oldest.bytes.length,
      sha256: `sha256:${createHash("sha256").update(oldest.bytes).digest("hex")}`,
    };
    const artifact = new Map([[digest, { path, bytes: oldest.bytes }]]);
    const faultFixture = JSON.parse(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/fixtures/bundle/negative/ancestor-descriptor-cases.json",
        ),
        "utf8",
      ),
    ) as {
      cases: Array<{ field: string; value: unknown; expected_error: string }>;
    };
    for (const fault of faultFixture.cases) {
      const descriptor = structuredClone(valid);
      descriptor[fault.field] = fault.value;
      const current = schemaBundleFile([{ id: "000001" }, { id: "000002" }], descriptor);
      expect(() => validateSchemaAncestorChain(current.document, artifact)).toThrow(
        new RegExp(fault.expected_error),
      );
    }
    const tamperedDocument = structuredClone(oldest.document) as {
      schema_bundle: { migrations: Array<Record<string, unknown>> };
    };
    tamperedDocument.schema_bundle.migrations[0]!.id = "999999";
    const tamperedBytes = new TextEncoder().encode(
      `${JSON.stringify(tamperedDocument, null, 2)}\n`,
    );
    const rawBoundDescriptor = {
      ...valid,
      size_bytes: tamperedBytes.length,
      sha256: `sha256:${createHash("sha256").update(tamperedBytes).digest("hex")}`,
    };
    const rawBoundCurrent = schemaBundleFile(
      [{ id: "000001" }, { id: "000002" }],
      rawBoundDescriptor,
    );
    expect(() =>
      validateSchemaAncestorChain(
        rawBoundCurrent.document,
        new Map([[digest, { path, bytes: tamperedBytes }]]),
      ),
    ).toThrow(/ANCESTOR_SELF_DIGEST/);
  });

  it("compares only ledger-backed columns and rejects identity drift", () => {
    const bundle = buildMigrationBundle(root);
    const schemaBundle = bundle.schemaBundleFile;
    const digest = String(schemaBundle.schema_bundle_digest);
    const migrations = (
      schemaBundle.schema_bundle as { migrations: Array<Record<string, unknown>> }
    ).migrations;
    const rows = migrations.map((entry, index) => ({
      ...migrationLedgerProjection(entry, digest),
      applied_at: `2026-08-11T00:00:0${index}Z`,
      applied_by: index === 0 ? "runner-a" : "runner-b",
    }));
    expect(() => validateLedgerPrefix(rows, [schemaBundle])).not.toThrow();
    const replayMetadataChanged = rows.map((row) => ({
      ...row,
      applied_at: "2099-01-01T00:00:00Z",
      applied_by: "different-audited-runner",
    }));
    expect(() => validateLedgerPrefix(replayMetadataChanged, [schemaBundle])).not.toThrow();
    const drift = structuredClone(rows);
    drift[0]!.migration_name = "identity_drift";
    expect(() => validateLedgerPrefix(drift, [schemaBundle])).toThrow(/LEDGER_ENTRY_MISMATCH/);
    const unknown = structuredClone(rows);
    Object.assign(unknown[0]!, { unexpected_column: true });
    expect(() => validateLedgerPrefix(unknown, [schemaBundle])).toThrow(/LEDGER_UNKNOWN_COLUMN/);
  });
});

describe("deterministic POSIX ustar", () => {
  const entries = [
    { path: "services/control-plane/migrations/b.sql", data: new TextEncoder().encode("b\n") },
    { path: "services/control-plane/migrations/a.sql", data: new TextEncoder().encode("a\n") },
  ];

  it("sorts, round-trips, and reproduces exact bits", () => {
    const archive = createDeterministicUstar(entries);
    const fixture = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/fixtures/bundle/golden/ustar.json"),
        "utf8",
      ),
    ) as { size_bytes: number; sha256: string };
    const parsed = readDeterministicUstar(archive);
    expect(parsed.map((entry) => entry.path)).toEqual([
      "services/control-plane/migrations/a.sql",
      "services/control-plane/migrations/b.sql",
    ]);
    expect(Buffer.from(createDeterministicUstar(parsed)).equals(Buffer.from(archive))).toBe(true);
    expect(archive.slice(-1024).every((byte) => byte === 0)).toBe(true);
    expect(archive.length).toBe(fixture.size_bytes);
    expect(`sha256:${createHash("sha256").update(archive).digest("hex")}`).toBe(fixture.sha256);
  });

  it("rejects checksum, typeflag, traversal and trailing blocks", () => {
    const archive = createDeterministicUstar(entries);
    const checksum = archive.slice();
    checksum[0] ^= 1;
    expect(() => readDeterministicUstar(checksum)).toThrow(/USTAR_CHECKSUM/);
    const typeflag = archive.slice();
    typeflag[156] = 0x32;
    expect(() => readDeterministicUstar(typeflag)).toThrow();
    const linkname = archive.slice();
    linkname[157] = 0x78;
    rewriteFirstHeaderChecksum(linkname);
    expect(() => readDeterministicUstar(linkname)).toThrow(/USTAR_NON_CANONICAL_HEADER/);
    expect(() => createDeterministicUstar([{ path: "../escape", data: new Uint8Array() }])).toThrow(
      /USTAR_PATH/,
    );
    expect(() =>
      readDeterministicUstar(new Uint8Array([...archive, ...new Uint8Array(512)])),
    ).toThrow();
  });
});

function rewriteFirstHeaderChecksum(archive: Uint8Array): void {
  archive.fill(0x20, 148, 156);
  const checksum = archive.slice(0, 512).reduce((sum, byte) => sum + byte, 0);
  const field = `${checksum.toString(8).padStart(6, "0")}\0 `;
  archive.set(new TextEncoder().encode(field), 148);
}

function schemaBundleFile(
  migrations: Array<Record<string, unknown>>,
  predecessor: Record<string, unknown> | null,
): { document: Record<string, unknown>; bytes: Uint8Array } {
  const schemaBundle = { predecessor_schema_bundle: predecessor, migrations };
  const document = {
    format_version: "cloud-agents-platform-schema-bundle/v1",
    schema_bundle: schemaBundle,
    schema_bundle_digest: migrationDigest({
      domain: "cloud-agents-platform-schema-bundle/v1",
      schema_bundle: schemaBundle,
    }),
  };
  return {
    document,
    bytes: new TextEncoder().encode(`${JSON.stringify(document, null, 2)}\n`),
  };
}
