import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { classifyMigrationStatement, splitPostgresStatements } from "./platform-migration-sql";

const root = resolve(import.meta.dirname, "../..");

describe("postgresql-lex-v1 bootstrap", () => {
  it("splits and classifies every current exact SQL statement", () => {
    const counts: number[] = [];
    for (const [index, file] of [
      "services/control-plane/migrations/000001_expand_migration_kernel.sql",
      "services/control-plane/migrations/000002_expand_tenancy.sql",
      "services/control-plane/migrations/000003_expand_membership_rbac.sql",
      "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
      "services/control-plane/migrations/000005_close_membership_binding_authority.sql",
      "services/control-plane/migrations/000006_close_subject_issuer_validation.sql",
      "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
    ].entries()) {
      const bytes = readFileSync(resolve(root, file));
      const statements = splitPostgresStatements(bytes);
      counts.push(statements.length);
      expect(statements.at(-1)?.end).toBeLessThanOrEqual(bytes.length);
      for (const statement of statements) {
        expect(statement.bytes.at(-1)).toBe(0x3b);
        expect(
          classifyMigrationStatement(statement, String(index + 1).padStart(6, "0")).profile,
        ).toBe("postgresql-ddl-v1");
      }
    }
    expect(counts).toEqual([20, 71, 46, 20, 1, 1, 89]);
  });

  it("classifies the managed-agent lifecycle repair as one forward migration", () => {
    const statements = splitPostgresStatements(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/000022_repair_managed_agent_lifecycle_transitions.sql",
        ),
      ),
    );
    expect(statements).toHaveLength(14);
    expect(
      statements
        .slice(0, 4)
        .map((statement) => classifyMigrationStatement(statement, "000022").command),
    ).toEqual(["CREATE", "CREATE", "CREATE", "CREATE"]);
    for (const statement of statements) {
      expect(classifyMigrationStatement(statement, "000022").profile).toBe("postgresql-ddl-v1");
    }
    expect(classifyMigrationStatement(statements.at(-1)!, "000022").target_identity).toContain(
      "append_managed_agent_event_v1",
    );
    expect(() => classifyMigrationStatement(statements[0]!, "000021")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/u,
    );
  });

  it("classifies the scoped managed-agent event-id successor", () => {
    const statements = splitPostgresStatements(
      readFileSync(
        resolve(root, "services/control-plane/migrations/000023_scope_managed_agent_event_ids.sql"),
      ),
    );
    expect(statements).toHaveLength(1);
    const classification = classifyMigrationStatement(statements[0]!, "000023");
    expect(classification).toMatchObject({
      profile: "postgresql-ddl-v1",
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity:
        "function:unquoted:cloud_agents/unquoted:append_managed_agent_event_v1(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:jsonb)",
    });
    const sql = new TextDecoder().decode(statements[0]!.bytes);
    expect(sql).toContain("cloud-agents/managed-agent-events/event-id/v1");
    for (const fragment of [
      "p_tenant_id",
      "p_project_uid",
      "p_session_uid",
      "sequence_value",
      "pg_catalog.sha256",
    ]) {
      expect(sql).toContain(fragment);
    }
  });

  it("classifies Provider resume cursor persistence", () => {
    const statements = splitPostgresStatements(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/000024_persist_managed_agent_provider_resume_cursor.sql",
        ),
      ),
    );
    expect(statements).toHaveLength(5);
    expect(
      statements.map((statement) => classifyMigrationStatement(statement, "000024").command),
    ).toEqual(["ALTER", "CREATE", "ALTER", "REVOKE", "GRANT"]);
    expect(classifyMigrationStatement(statements[1]!, "000024").target_identity).toContain(
      "settle_managed_agent_execution_v2",
    );
  });

  it("classifies initial tenant administrator bootstrap", () => {
    const bytes = readFileSync(
      resolve(root, "services/control-plane/migrations/000025_bootstrap_tenant_administrator.sql"),
    );
    const statements = splitPostgresStatements(bytes);
    const classifications = statements.map((statement) =>
      classifyMigrationStatement(statement, "000025"),
    );
    expect(statements).toHaveLength(5);
    expect(classifications.map(({ command }) => command)).toEqual([
      "CREATE",
      "ALTER",
      "REVOKE",
      "REVOKE",
      "GRANT",
    ]);
    expect(classifications[0]!.target_identity).toContain("bootstrap_tenant_administrator_v1");

    const catalog = JSON.parse(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/product/000025/catalog/schema-000025.json",
        ),
        "utf8",
      ),
    ) as {
      source_descriptors: Array<{ migration_id: string; sql_sha256: string; statements: unknown }>;
      declared_object_identities: Array<{ kind: string; identity?: { name?: string } }>;
    };
    expect(catalog.source_descriptors.at(-1)).toEqual({
      migration_id: "000025",
      sql_sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      statements: statements.map((statement, index) => ({
        index,
        start: statement.start,
        end: statement.end,
        sha256: statement.sha256,
        classification: classifications[index],
      })),
    });
    expect(catalog.declared_object_identities).toContainEqual(
      expect.objectContaining({
        kind: "function",
        identity: expect.objectContaining({ name: "bootstrap_tenant_administrator_v1" }),
      }),
    );
  });

  it("classifies durable Managed Agent terminal Result persistence", () => {
    const bytes = readFileSync(
      resolve(
        root,
        "services/control-plane/migrations/000026_persist_managed_agent_execution_result.sql",
      ),
    );
    const statements = splitPostgresStatements(bytes);
    const classifications = statements.map((statement) =>
      classifyMigrationStatement(statement, "000026"),
    );
    expect(classifications.map(({ command }) => command)).toEqual([
      "ALTER",
      "ALTER",
      "CREATE",
      "ALTER",
      "REVOKE",
      "GRANT",
    ]);
    expect(classifications[2]!.target_identity).toContain("settle_managed_agent_execution_v3");

    const catalog = JSON.parse(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/product/000026/catalog/schema-000026.json",
        ),
        "utf8",
      ),
    ) as {
      source_descriptors: Array<{ migration_id: string; sql_sha256: string; statements: unknown }>;
      declared_object_identities: Array<{ kind: string; identity?: { name?: string } }>;
    };
    expect(catalog.source_descriptors.at(-1)).toEqual({
      migration_id: "000026",
      sql_sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      statements: statements.map((statement, index) => ({
        index,
        start: statement.start,
        end: statement.end,
        sha256: statement.sha256,
        classification: classifications[index],
      })),
    });
    expect(catalog.declared_object_identities).toContainEqual(
      expect.objectContaining({
        kind: "function",
        identity: expect.objectContaining({ name: "settle_managed_agent_execution_v3" }),
      }),
    );
  });

  it("classifies tenant-scoped Organization creation", () => {
    const bytes = readFileSync(
      resolve(root, "services/control-plane/migrations/000027_create_organization.sql"),
    );
    const statements = splitPostgresStatements(bytes);
    const classifications = statements.map((statement) =>
      classifyMigrationStatement(statement, "000027"),
    );
    expect(classifications.map(({ command }) => command)).toEqual([
      "ALTER",
      "ALTER",
      "ALTER",
      "ALTER",
      "ALTER",
      "ALTER",
      "CREATE",
      "REVOKE",
      "GRANT",
    ]);
    expect(classifications[6]!.target_identity).toContain("create_organization");

    const catalog = JSON.parse(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/product/000027/catalog/schema-000027.json",
        ),
        "utf8",
      ),
    ) as {
      source_descriptors: Array<{ migration_id: string; sql_sha256: string; statements: unknown }>;
      declared_object_identities: Array<{ kind: string; identity?: { name?: string } }>;
    };
    expect(catalog.source_descriptors.at(-1)).toEqual({
      migration_id: "000027",
      sql_sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      statements: statements.map((statement, index) => ({
        index,
        start: statement.start,
        end: statement.end,
        sha256: statement.sha256,
        classification: classifications[index],
      })),
    });
    expect(catalog.declared_object_identities).toContainEqual(
      expect.objectContaining({
        kind: "function",
        identity: expect.objectContaining({ name: "create_organization" }),
      }),
    );
  });

  it("classifies suspended Membership resumption", () => {
    const bytes = readFileSync(
      resolve(root, "services/control-plane/migrations/000028_resume_membership.sql"),
    );
    const statements = splitPostgresStatements(bytes);
    const classifications = statements.map((statement) =>
      classifyMigrationStatement(statement, "000028"),
    );
    expect(classifications.map(({ command }) => command)).toEqual([
      "ALTER",
      "ALTER",
      "CREATE",
      "CREATE",
      "REVOKE",
      "GRANT",
    ]);
    expect(classifications[2]!.target_identity).toContain("transition_membership");
    expect(classifications[3]!.target_identity).toContain("resume_membership");

    const catalog = JSON.parse(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/product/000028/catalog/schema-000028.json",
        ),
        "utf8",
      ),
    ) as {
      source_descriptors: Array<{ migration_id: string; sql_sha256: string; statements: unknown }>;
      declared_object_identities: Array<{ kind: string; identity?: { name?: string } }>;
    };
    expect(catalog.source_descriptors.at(-1)).toEqual({
      migration_id: "000028",
      sql_sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      statements: statements.map((statement, index) => ({
        index,
        start: statement.start,
        end: statement.end,
        sha256: statement.sha256,
        classification: classifications[index],
      })),
    });
    expect(catalog.declared_object_identities).toContainEqual(
      expect.objectContaining({
        kind: "function",
        identity: expect.objectContaining({ name: "resume_membership" }),
      }),
    );
  });

  it("admits only the exact generated-profile operation-effect partial index", () => {
    const statements = splitPostgresStatements(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/000007_expand_durable_coordination_kernel.sql",
        ),
      ),
    );
    expect(classifyMigrationStatement(statements[26]!, "000007")).toEqual({
      profile: "postgresql-ddl-v1",
      command: "CREATE",
      object_kind: "INDEX",
      target_identity:
        "index:unquoted:cloud_agents/unquoted:outbox_events_operation_effect_unique_idx",
      grantee: null,
      special_case: null,
    });
    for (const mutation of [
      (sql: string) =>
        sql.replace("event_class = 'operation_effect'", "event_class = 'resource_change'"),
      (sql: string) =>
        sql.replace(
          "outbox_events_operation_effect_unique_idx",
          "outbox_events_unreviewed_unique_idx",
        ),
    ]) {
      const mutated = mutation(new TextDecoder().decode(statements[26]!.bytes));
      const statement = splitPostgresStatements(new TextEncoder().encode(mutated))[0]!;
      expect(() => classifyMigrationStatement(statement, "000007")).toThrow(
        /SQL_STATEMENT_PROFILE_REJECTED/u,
      );
    }
    expect(() => classifyMigrationStatement(statements[26]!, "000006")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/u,
    );
  });

  it("ignores semicolons in comments, strings, identifiers and dollar bodies", () => {
    const sql = new TextEncoder().encode(
      "-- lead ;\nCREATE FUNCTION cloud_agents.f() RETURNS text LANGUAGE sql AS $body$ SELECT ';'; $body$;/* ; */ REVOKE ALL ON FUNCTION cloud_agents.f() FROM PUBLIC;",
    );
    const statements = splitPostgresStatements(sql);
    expect(statements).toHaveLength(2);
    expect(statements[0]?.start).toBe(0);
    expect(new TextDecoder().decode(statements[0]?.bytes).startsWith("-- lead ;")).toBe(true);
  });

  it("distinguishes standard, escape and Unicode strings and rejects numeric dollar tags", () => {
    expect(
      splitPostgresStatements(
        new TextEncoder().encode("CREATE TABLE cloud_agents.x(a text DEFAULT '\\';"),
      ),
    ).toHaveLength(1);
    for (const prefix of ["E", "U&"]) {
      expect(() =>
        splitPostgresStatements(
          new TextEncoder().encode(`CREATE TABLE cloud_agents.x(a text DEFAULT ${prefix}'\\';`),
        ),
      ).toThrow(/UNTERMINATED_SQL_LEXEME/);
    }
    const numericTag = splitPostgresStatements(
      new TextEncoder().encode("CREATE TABLE cloud_agents.x(a text); $1$ SELECT 1; $1$;"),
    );
    expect(numericTag.length).toBeGreaterThan(1);
  });

  it("rejects nested-comment, multi-subcommand and broad GRANT/REVOKE bypasses", () => {
    const rejected = [
      "ALTER TABLE cloud_agents.x DROP COLUMN y, ADD CONSTRAINT ok CHECK (true);",
      "ALTER TABLE cloud_agents.x DROP COLUMN y /* outer /* ADD CONSTRAINT hidden */ still */;",
      "ALTER FUNCTION cloud_agents.f() DROP ATTRIBUTE x OWNER TO cloud_agents_migration_owner;",
      "ALTER DEFAULT PRIVILEGES FOR ROLE cloud_agents_migration_owner IN SCHEMA cloud_agents GRANT ALL ON TABLES TO PUBLIC;",
      "GRANT SELECT ON TABLE cloud_agents.x, cloud_agents.y TO cloud_agents_runtime;",
      "GRANT SELECT ON TABLE cloud_agents.x TO cloud_agents_runtime, PUBLIC;",
      "GRANT SELECT ON TABLE other.x TO cloud_agents_runtime;",
      "GRANT SELECT ON TABLE cloud_agents.x TO cloud_agents_runtime WITH GRANT OPTION;",
      "CREATE TABLE cloud_agents.x AS SELECT 1;",
      "CREATE TABLE cloud_agents.x(a text) TABLESPACE unsafe;",
      "CREATE TABLE cloud_agents.x(a text) WITH (fillfactor = 50);",
      "CREATE TABLE cloud_agents.x(a text), cloud_agents.y(b text);",
      "CREATE INDEX bad ON cloud_agents.x(a) TABLESPACE unsafe;",
      "CREATE FUNCTION cloud_agents.f() RETURNS void LANGUAGE sql AS $$ SELECT 1 $$ EXTRA;",
      "CREATE FUNCTION cloud_agents.f() RETURNS void LANGUAGE sql TABLESPACE unsafe AS $$ SELECT 1 $$;",
      "CREATE FUNCTION cloud_agents.f() RETURNS void LANGUAGE sql WITH unsafe AS $$ SELECT 1 $$;",
      "CREATE OR REPLACE FUNCTION cloud_agents.create_membership() RETURNS void LANGUAGE sql AS $$ SELECT 1 $$;",
    ];
    for (const sql of rejected) {
      const statement = splitPostgresStatements(new TextEncoder().encode(sql))[0]!;
      expect(() => classifyMigrationStatement(statement, "000002"), sql).toThrow(
        /SQL_STATEMENT_PROFILE_REJECTED/,
      );
    }
  });

  it("rejects unterminated, empty, transaction, DML and mutated DO inputs", () => {
    expect(() =>
      splitPostgresStatements(new TextEncoder().encode("CREATE TABLE x(a text)")),
    ).toThrow(/SQL_TERMINATOR_REQUIRED/);
    expect(() => splitPostgresStatements(new TextEncoder().encode(";"))).toThrow(
      /EMPTY_SQL_STATEMENT/,
    );
    for (const sql of [
      "BEGIN;",
      "SELECT 1;",
      "CREATE ROLE attacker;",
      "INSERT INTO cloud_agents.builtin_roles VALUES ('attacker');",
    ]) {
      const statement = splitPostgresStatements(new TextEncoder().encode(sql))[0]!;
      expect(() => classifyMigrationStatement(statement, "000002")).toThrow(
        /SQL_STATEMENT_PROFILE_REJECTED/,
      );
    }
    const doStatement = splitPostgresStatements(
      new TextEncoder().encode("DO $$ BEGIN NULL; END $$;"),
    )[0]!;
    expect(() => classifyMigrationStatement(doStatement, "000001")).toThrow(
      /SQL_DO_SPECIAL_CASE_MISMATCH/,
    );
  });

  it("admits only the exact migration-owned role seed and resource-kind replacement", () => {
    const bytes = readFileSync(
      resolve(root, "services/control-plane/migrations/000003_expand_membership_rbac.sql"),
    );
    const statements = splitPostgresStatements(bytes);
    expect(classifyMigrationStatement(statements[0]!, "000003").command).toBe("ALTER");
    expect(classifyMigrationStatement(statements[44]!, "000003").command).toBe("INSERT");
    expect(classifyMigrationStatement(statements[45]!, "000003").command).toBe("INSERT");
    expect(classifyMigrationStatement(statements[45]!, "000003").special_case).toBeNull();

    const mutatedSeed = new TextEncoder().encode(
      new TextDecoder().decode(statements[44]!.bytes).replace("project.viewer", "project.attacker"),
    );
    expect(() =>
      classifyMigrationStatement(splitPostgresStatements(mutatedSeed)[0]!, "000003"),
    ).toThrow(/SQL_STATEMENT_PROFILE_REJECTED/);

    const wrongDrop = splitPostgresStatements(
      new TextEncoder().encode(
        "ALTER TABLE cloud_agents.resource_changes DROP CONSTRAINT resource_changes_tenant_fk;",
      ),
    )[0]!;
    expect(() => classifyMigrationStatement(wrongDrop, "000003")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
    expect(() => classifyMigrationStatement(statements[0]!, "000002")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );

    const mutationStatements = splitPostgresStatements(
      readFileSync(
        resolve(
          root,
          "services/control-plane/migrations/000004_expand_membership_rbac_mutations.sql",
        ),
      ),
    );
    expect(classifyMigrationStatement(mutationStatements[0]!, "000004").command).toBe("ALTER");
    expect(classifyMigrationStatement(mutationStatements[1]!, "000004").command).toBe("ALTER");
    expect(classifyMigrationStatement(mutationStatements.at(-1)!, "000004")).toEqual({
      profile: "postgresql-ddl-v1",
      command: "REVOKE",
      object_kind: "ALL_FUNCTIONS",
      target_identity: "schema:unquoted:cloud_agents",
      grantee: "PUBLIC",
      special_case: null,
    });
    expect(() => classifyMigrationStatement(mutationStatements[0]!, "000003")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
    expect(() => classifyMigrationStatement(mutationStatements.at(-1)!, "000003")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
    const wrongAuditDrop = splitPostgresStatements(
      new TextEncoder().encode(
        "ALTER TABLE cloud_agents.audit_facts DROP CONSTRAINT audit_facts_tenant_fk;",
      ),
    )[0]!;
    expect(() => classifyMigrationStatement(wrongAuditDrop, "000004")).toThrow(
      /SQL_STATEMENT_PROFILE_REJECTED/,
    );
    for (const sql of [
      "REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA other FROM PUBLIC;",
      "GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA cloud_agents TO PUBLIC;",
      "REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA cloud_agents FROM PUBLIC, cloud_agents_runtime;",
    ]) {
      expect(() =>
        classifyMigrationStatement(
          splitPostgresStatements(new TextEncoder().encode(sql))[0]!,
          "000004",
        ),
      ).toThrow(/SQL_STATEMENT_PROFILE_REJECTED/);
    }
  });

  it("preserves quoted identity spelling while folding unquoted identifiers", () => {
    const classify = (sql: string) =>
      classifyMigrationStatement(
        splitPostgresStatements(new TextEncoder().encode(sql))[0]!,
        "000002",
      ).target_identity;
    expect(classify("GRANT SELECT ON TABLE cloud_agents.MixedCase TO cloud_agents_runtime;")).toBe(
      "table:unquoted:cloud_agents/unquoted:mixedcase",
    );
    expect(
      classify('GRANT SELECT ON TABLE cloud_agents."MixedCase" TO cloud_agents_runtime;'),
    ).toBe('table:unquoted:cloud_agents/quoted:"MixedCase"');
  });
});
