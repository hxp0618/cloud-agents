import { resolve } from "node:path";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  buildMigrationBundle,
  migrationLedgerProjection,
  migrationStatementSourceDescriptors,
  validateBuiltinRoleSeedFixture,
  validateCatalogStatementBindings,
  validateCheckedInMigrationBundle,
  validateDurableCoordinationKernel,
  validateLedgerPrefix,
  validateProjectionScopeAuthority,
  validateRuntimeArtifactSafety,
  validateSchemaAncestorChain,
} from "./platform-migration-bundle";
import { buildDurableCoordinationRegistry } from "./platform-durable-coordination-registry";
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
        bundle.files.get("services/control-plane/migrations/catalog/schema-000008.json"),
      ),
    ).toContain('"runtime_introspection_status": "NOT_IMPLEMENTED"');
  });

  it("detects generated source drift", () => {
    const first = buildMigrationBundle(root);
    const second = buildMigrationBundle(root);
    expect(Buffer.from(first.runtimeTar).equals(Buffer.from(second.runtimeTar))).toBe(true);
    expect(Buffer.from(first.bootstrapTar).equals(Buffer.from(second.bootstrapTar))).toBe(true);
    expect(first.manifest.schema_bundle_digest).toBe(
      "sha256:9084475d8db1e74afeb0d77ffaf9e253c4e6b6c67c1ba09a7c45483a42cc15ab",
    );
    expect(first.manifest.bootstrap_bundle_digest).toBe(
      "sha256:db95649924f259cfa320e897bd5e0934c35fcc9009d8492a69ec5dc71132081c",
    );
    expect(first.manifest.manifest_digest).toBe(
      "sha256:d896285b8835751c7c1567d01c955bd6c44b84586c25a0a9bbba7b01fde8eacc",
    );
    expect(sha256(first.runtimeTar)).toBe(
      "sha256:2bee1a8c98dcdce32d21406d05e15bb317495f574e572e48c612ccfe4f61754d",
    );
    expect(sha256(first.bootstrapTar)).toBe(
      "sha256:6654946d58f707d48c71740a41407674c34b5fbeced2e38eeb6c8d1bb08ae175",
    );
    expect(readDeterministicUstar(first.bootstrapTar).map((entry) => entry.path)).toEqual([
      "services/control-plane/migrations/bootstrap/database.sql",
      "services/control-plane/migrations/bootstrap/roles.sql",
    ]);
    const runtimePaths = readDeterministicUstar(first.runtimeTar).map((entry) => entry.path);
    expect(
      runtimePaths.some((path) => /authority-binding|fixtures|secret|credential/iu.test(path)),
    ).toBe(false);
    const unchangedArtifacts = new Map([
      [
        "services/control-plane/migrations/000001_expand_migration_kernel.sql",
        "sha256:8f9eb57df5fea699c4cfcf39171079d0c88c01f74ddb4bf2e38261dc0cd451b4",
      ],
      [
        "services/control-plane/migrations/000002_expand_tenancy.sql",
        "sha256:d084f003928c1122da7bb88727c12a3e298548514f5da19cb3da14a3a754827a",
      ],
      [
        "services/control-plane/migrations/catalog/authority-v1.json",
        "sha256:eb8c4ad607dc3443471fa376a9da9bf49e17788ffcc9cda6d2ccecd982327ccd",
      ],
      [
        "services/control-plane/migrations/catalog/global-table-authority-v1.json",
        "sha256:d8330d06ead9a1cbc68c89e1741dcb3dc43d88d3e843590fea1ca56e242cb53d",
      ],
      [
        "services/control-plane/migrations/catalog/schema-000001.json",
        "sha256:d9a6e5accb1b6b5765c3f602f7b54781f611a3d8ae83395cb177599c441e946f",
      ],
      [
        "services/control-plane/migrations/catalog/schema-000002.json",
        "sha256:c242d90cb3dfa1a8f7f1782bad557bfcd18257c4432a114e8413c9407c860bd9",
      ],
      [
        "services/control-plane/migrations/archive/efa8240997f191f6e1540897bf391d6ed3c0a921e5958ea97338aec9e3befeec.schema-bundle.json",
        "sha256:8088b2ff98a7077ec98ca4f925c076501f9478b5b3aa1d8f976582d956884336",
      ],
      [
        "services/control-plane/migrations/archive/c6652bef99a83b9a8a76739ef7d84e19321feaa80730c548bb7c50191aec3c23.schema-bundle.json",
        "sha256:a4bd9503c1c11c7bcfc48249f501fd258ff09ad2354d4c042f298bb20c705820",
      ],
      [
        "services/control-plane/migrations/archive/52aea3c0a5fe5270d13a2bf194aedcc3ce0817fe3183dd868d427f7582f7819d.schema-bundle.json",
        "sha256:d938ca1dc174816d1ccb719d82e57505ed2f9d8e5836dfe4109ab82ae20905ae",
      ],
      [
        "services/control-plane/migrations/fixtures/projection/golden/authority-binding-v1.json",
        "sha256:02550b2ad4da6a57fe98be1e9ecbea3924f2fd34f9ad99cebf3e674deae81468",
      ],
    ]);
    for (const [path, digest] of unchangedArtifacts) {
      expect(sha256(readFileSync(resolve(root, path))), path).toBe(digest);
    }
  });

  it("validates the signed projection scope authority as a closed bounded role closure", () => {
    const valid = {
      default_acl_owners: ["cloud_agents_migration_owner"],
      object_creator_closure: ["cloud_agents_migration_owner"],
    };
    expect(() => validateProjectionScopeAuthority(valid)).not.toThrow();
    const faults: Array<{ name: string; value: unknown; error: RegExp }> = [
      {
        name: "missing",
        value: { object_creator_closure: ["cloud_agents_migration_owner"] },
        error: /UNKNOWN_OR_MISSING_FIELD/u,
      },
      {
        name: "null",
        value: null,
        error: /EXPECTED_OBJECT/u,
      },
      {
        name: "unknown",
        value: { ...valid, unknown: true },
        error: /UNKNOWN_OR_MISSING_FIELD/u,
      },
      {
        name: "alias",
        value: {
          defaultAclOwners: ["cloud_agents_migration_owner"],
          object_creator_closure: ["cloud_agents_migration_owner"],
        },
        error: /UNKNOWN_OR_MISSING_FIELD/u,
      },
      {
        name: "duplicate",
        value: {
          ...valid,
          object_creator_closure: ["cloud_agents_migration_owner", "cloud_agents_migration_owner"],
        },
        error: /PROJECTION_SCOPE_AUTHORITY_ORDER/u,
      },
      {
        name: "unsorted",
        value: {
          default_acl_owners: ["a_owner"],
          object_creator_closure: ["cloud_agents_migration_owner", "a_owner"],
        },
        error: /PROJECTION_SCOPE_AUTHORITY_ORDER/u,
      },
      {
        name: "empty",
        value: { ...valid, default_acl_owners: [] },
        error: /PROJECTION_SCOPE_AUTHORITY_LIMIT/u,
      },
      {
        name: "outside closure",
        value: { ...valid, default_acl_owners: ["another_owner"] },
        error: /PROJECTION_SCOPE_AUTHORITY_CLOSURE/u,
      },
      {
        name: "invalid principal",
        value: { ...valid, object_creator_closure: ["role\0name"] },
        error: /PROJECTION_SCOPE_AUTHORITY_ROLE/u,
      },
      {
        name: "bounded",
        value: {
          default_acl_owners: ["role_000"],
          object_creator_closure: Array.from(
            { length: 257 },
            (_, index) => `role_${String(index).padStart(3, "0")}`,
          ),
        },
        error: /PROJECTION_SCOPE_AUTHORITY_LIMIT/u,
      },
    ];
    for (const fault of faults) {
      expect(() => validateProjectionScopeAuthority(fault.value as never), fault.name).toThrow(
        fault.error,
      );
    }
  });

  it("cross-binds the exact migration seed to the built-in role catalog fixture", () => {
    const sql = readFileSync(
      resolve(root, "services/control-plane/migrations/000003_expand_membership_rbac.sql"),
    );
    const catalog = readFileSync(
      resolve(root, "contracts/platform/v1alpha1/fixtures/golden/builtin-role-catalog-v1.json"),
    );
    expect(() => validateBuiltinRoleSeedFixture(sql, catalog)).not.toThrow();

    const drifted = JSON.parse(catalog.toString("utf8")) as {
      roles: Array<{ permissions: string[] }>;
    };
    drifted.roles[0]!.permissions[0] = "memberships.future";
    expect(() =>
      validateBuiltinRoleSeedFixture(sql, new TextEncoder().encode(JSON.stringify(drifted))),
    ).toThrow(/BUILTIN_ROLE_SEED_MISMATCH/u);
  });

  it("binds migration 000007 to the exact generated coordination registry", () => {
    const sql = readFileSync(
      resolve(
        root,
        "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
      ),
    );
    const registry = buildDurableCoordinationRegistry(root);
    expect(() => validateDurableCoordinationKernel(sql, registry)).not.toThrow();

    const profileDrift = structuredClone(registry);
    (profileDrift.profiles as Array<{ profileDigest: string }>)[0]!.profileDigest =
      `sha256:${"0".repeat(64)}`;
    expect(() => validateDurableCoordinationKernel(sql, profileDrift)).toThrow(
      /COORDINATION_KERNEL_PROFILE/u,
    );

    const stateDrift = structuredClone(registry);
    (stateDrift.stateMachines as Array<{ id: string; states: string[] }>)
      .find((machine) => machine.id === "outbox/v1")!
      .states.push("unreviewed");
    expect(() => validateDurableCoordinationKernel(sql, stateDrift)).toThrow(
      /COORDINATION_KERNEL_STATE_SET/u,
    );

    const ttlDrift = structuredClone(registry);
    (
      ttlDrift.profiles as Array<{ spec: { idempotency: { replayTtlSeconds: number } } }>
    )[0]!.spec.idempotency.replayTtlSeconds = 86401;
    expect(() => validateDurableCoordinationKernel(sql, ttlDrift)).toThrow(
      /COORDINATION_KERNEL_PROFILE/u,
    );

    const sqlText = new TextDecoder().decode(sql);
    const profileSqlDrift = new TextEncoder().encode(
      sqlText.replace("'managedAgentCreateProject/v1alpha1'", "'unreviewed/v1alpha1'"),
    );
    expect(() => validateDurableCoordinationKernel(profileSqlDrift, registry)).toThrow(
      /COORDINATION_KERNEL_(PROFILE|STATEMENT_COUNT)/u,
    );
    const secretSqlDrift = new TextEncoder().encode(
      sqlText.replace(
        "subject_digest text NOT NULL,\n    operation_id text,",
        "subject_digest text NOT NULL,\n    raw_request text,\n    operation_id text,",
      ),
    );
    expect(() => validateDurableCoordinationKernel(secretSqlDrift, registry)).toThrow(
      /COORDINATION_KERNEL_SECRET_FIELD/u,
    );
    const volatilityDrift = new TextEncoder().encode(sqlText.replace("IMMUTABLE", "VOLATILE"));
    expect(() => validateDurableCoordinationKernel(volatilityDrift, registry)).toThrow(
      /COORDINATION_KERNEL_HELPER_PURITY/u,
    );
    const operationCreationDrift = new TextEncoder().encode(
      sqlText.replace("AND false", "AND true"),
    );
    expect(() => validateDurableCoordinationKernel(operationCreationDrift, registry)).toThrow(
      /COORDINATION_KERNEL_BINDING/u,
    );
  });

  it("rejects detached deployment authority from the runtime artifact closure", () => {
    expect(() =>
      validateRuntimeArtifactSafety([
        {
          path: "services/control-plane/migrations/fixtures/projection/golden/authority-binding-v1.json",
          mode: "100644",
          size_bytes: 1,
          sha256: `sha256:${"0".repeat(64)}`,
        },
      ]),
    ).toThrow(/RUNTIME_TAR_DEPLOYMENT_AUTHORITY/u);
  });

  it("binds every catalog statement descriptor to exact SQL classification", () => {
    const catalog = JSON.parse(
      readFileSync(
        resolve(root, "services/control-plane/migrations/catalog/schema-000003.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    const sql = new Map(
      [
        "services/control-plane/migrations/000001_expand_migration_kernel.sql",
        "services/control-plane/migrations/000002_expand_tenancy.sql",
        "services/control-plane/migrations/000003_expand_membership_rbac.sql",
        "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
        "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
        "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
        "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
        "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
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
        resolve(root, "services/control-plane/migrations/catalog/schema-000003.json"),
        "utf8",
      ),
    ) as Record<string, unknown>;
    const paths = [
      "services/control-plane/migrations/000001_expand_migration_kernel.sql",
      "services/control-plane/migrations/000002_expand_tenancy.sql",
      "services/control-plane/migrations/000003_expand_membership_rbac.sql",
      "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
      "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
      "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
      "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
      "services/control-plane/migrations/000008_add_durable_coordination_service.sql",
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
      drift.source_descriptors = migrationStatementSourceDescriptors(sql).slice(0, 3);
      expect(() => validateCatalogStatementBindings(drift, sql), fault.name).toThrow(
        /CATALOG_TARGET_NOT_DECLARED/,
      );
    }
    const replacementSQL = new Map(original);
    const replacementSource = new TextDecoder().decode(replacementSQL.get(paths[4])!);
    replacementSQL.set(
      paths[4],
      new TextEncoder().encode(
        replacementSource.replace(
          "CREATE OR REPLACE FUNCTION cloud_agents.bind_role",
          "CREATE OR REPLACE FUNCTION cloud_agents.unknown_binding",
        ),
      ),
    );
    expect(() => migrationStatementSourceDescriptors(replacementSQL)).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
    const issuerReplacementSQL = new Map(original);
    const issuerReplacementSource = new TextDecoder().decode(issuerReplacementSQL.get(paths[5])!);
    issuerReplacementSQL.set(
      paths[5],
      new TextEncoder().encode(
        issuerReplacementSource.replace(
          "CREATE OR REPLACE FUNCTION cloud_agents.subject_ref_digest",
          "CREATE OR REPLACE FUNCTION cloud_agents.unknown_subject_digest",
        ),
      ),
    );
    expect(() => migrationStatementSourceDescriptors(issuerReplacementSQL)).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
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

    const unknownInner = structuredClone(current.document) as {
      schema_bundle: Record<string, unknown>;
      schema_bundle_digest: string;
    };
    unknownInner.schema_bundle.unknown = true;
    unknownInner.schema_bundle_digest = migrationDigest({
      domain: "cloud-agents-platform-schema-bundle/v1",
      schema_bundle: unknownInner.schema_bundle,
    });
    expect(() => validateSchemaAncestorChain(unknownInner, new Map())).toThrow(
      /UNKNOWN_OR_MISSING_FIELD/u,
    );

    const missingScope = structuredClone(current.document) as {
      schema_bundle: Record<string, unknown>;
      schema_bundle_digest: string;
    };
    delete missingScope.schema_bundle.projection_scope_authority;
    missingScope.schema_bundle_digest = migrationDigest({
      domain: "cloud-agents-platform-schema-bundle/v1",
      schema_bundle: missingScope.schema_bundle,
    });
    expect(() => validateSchemaAncestorChain(missingScope, new Map())).toThrow(
      /UNKNOWN_OR_MISSING_FIELD/u,
    );
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
    const ancestorShapeFaults = [
      {
        name: "missing projection scope authority",
        mutate(schema: Record<string, unknown>) {
          delete schema.projection_scope_authority;
        },
      },
      {
        name: "unknown projection scope authority member",
        mutate(schema: Record<string, unknown>) {
          (schema.projection_scope_authority as Record<string, unknown>).unknown = true;
        },
      },
    ];
    for (const fault of ancestorShapeFaults) {
      const malformedDocument = structuredClone(oldest.document) as {
        schema_bundle: Record<string, unknown>;
        schema_bundle_digest: string;
      };
      fault.mutate(malformedDocument.schema_bundle);
      malformedDocument.schema_bundle_digest = migrationDigest({
        domain: "cloud-agents-platform-schema-bundle/v1",
        schema_bundle: malformedDocument.schema_bundle,
      });
      const malformedBytes = new TextEncoder().encode(
        `${JSON.stringify(malformedDocument, null, 2)}\n`,
      );
      const malformedDigest = malformedDocument.schema_bundle_digest;
      const malformedPath = `services/control-plane/migrations/archive/${malformedDigest.slice("sha256:".length)}.schema-bundle.json`;
      const malformedDescriptor = {
        schema_bundle_digest: malformedDigest,
        path: malformedPath,
        mode: "100644",
        size_bytes: malformedBytes.length,
        sha256: sha256(malformedBytes),
      };
      const malformedCurrent = schemaBundleFile(
        [{ id: "000001" }, { id: "000002" }],
        malformedDescriptor,
      );
      expect(
        () =>
          validateSchemaAncestorChain(
            malformedCurrent.document,
            new Map([[malformedDigest, { path: malformedPath, bytes: malformedBytes }]]),
          ),
        fault.name,
      ).toThrow(/UNKNOWN_OR_MISSING_FIELD/u);
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

function sha256(bytes: Uint8Array): string {
  return `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
}

function schemaBundleFile(
  migrations: Array<Record<string, unknown>>,
  predecessor: Record<string, unknown> | null,
): { document: Record<string, unknown>; bytes: Uint8Array } {
  const schemaBundle = {
    lineage: "cloud-agents-platform",
    schema_head: String(migrations.at(-1)?.id ?? "000000"),
    advisory_lock: {
      domain: "cloud-agents-platform:migrations:v1",
      derivation: "sha256-first-8-bytes-signed-big-endian-int64",
      key_int64_decimal: "-1047838957622507638",
    },
    global_table_authority: {
      path: "services/control-plane/migrations/catalog/global-table-authority-v1.json",
      mode: "100644",
      size_bytes: 1,
      sha256: `sha256:${"0".repeat(64)}`,
    },
    projection_scope_authority: {
      default_acl_owners: ["cloud_agents_migration_owner"],
      object_creator_closure: ["cloud_agents_migration_owner"],
    },
    predecessor_schema_bundle: predecessor,
    migrations,
  };
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
