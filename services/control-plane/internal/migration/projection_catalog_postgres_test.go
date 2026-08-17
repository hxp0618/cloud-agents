package migration

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPGCatalogFixedQueriesParseOnPostgres(t *testing.T) {
	dsn := requireCatalogParseDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL catalog parse target: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	requireEmptyCatalogParseDatabase(t, ctx, connection)
	if _, err := connection.Exec(ctx, `CREATE SCHEMA cloud_agents`); err != nil {
		t.Fatalf("create catalog parse schema: %v", err)
	}
	defer func() { _, _ = connection.Exec(context.Background(), `DROP SCHEMA IF EXISTS cloud_agents CASCADE`) }()
	for id := projectionQueryCatalogRelations; id <= projectionQueryCatalogExpressions; id++ {
		query, ok := projectionFixedQuery(id)
		if !ok {
			t.Fatalf("catalog query id %d is missing", id)
		}
		rows, err := connection.Query(ctx, query, projectionTargetSchema)
		if err != nil {
			t.Fatalf("catalog query id %d failed to parse: %v", id, err)
		}
		for rows.Next() {
			if _, err := rows.Values(); err != nil {
				rows.Close()
				t.Fatalf("catalog query id %d failed to decode: %v", id, err)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("catalog query id %d iteration failed: %v", id, err)
		}
		rows.Close()
	}
}

func TestPGCatalogStructureOnRepresentativePostgres(t *testing.T) {
	dsn := requireCatalogParseDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL catalog structure target: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	requireEmptyCatalogParseDatabase(t, ctx, connection)
	ddl := `CREATE SCHEMA cloud_agents;
CREATE TABLE cloud_agents.parent (id integer PRIMARY KEY);
CREATE TABLE cloud_agents.child (
 id integer PRIMARY KEY,
 parent_id integer NOT NULL REFERENCES cloud_agents.parent(id),
 name text NOT NULL DEFAULT 'unnamed',
 name_len integer GENERATED ALWAYS AS (pg_catalog.length(name)) STORED,
 CONSTRAINT child_name_check CHECK (pg_catalog.length(name) > 0)
);
CREATE INDEX child_parent_idx ON cloud_agents.child(parent_id);
CREATE INDEX child_name_lower_idx ON cloud_agents.child((pg_catalog.lower(name))) WHERE parent_id > 0;
ALTER TABLE cloud_agents.child ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.child FORCE ROW LEVEL SECURITY;
CREATE POLICY child_public ON cloud_agents.child TO PUBLIC USING (parent_id > 0);
CREATE FUNCTION cloud_agents.child_touch_fn() RETURNS trigger
LANGUAGE plpgsql AS $body$ BEGIN RETURN NEW; END $body$;
CREATE FUNCTION cloud_agents.add_one(value integer, delta integer DEFAULT 1)
RETURNS integer LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $body$ SELECT value + delta $body$;
CREATE TRIGGER child_touch BEFORE UPDATE ON cloud_agents.child
FOR EACH ROW WHEN (NEW.parent_id > 0) EXECUTE FUNCTION cloud_agents.child_touch_fn()`
	if _, err := connection.Exec(ctx, ddl); err != nil {
		t.Fatalf("create representative catalog: %v", err)
	}
	defer func() { _, _ = connection.Exec(context.Background(), `DROP SCHEMA IF EXISTS cloud_agents CASCADE`) }()
	declared := []ObjectIdentityProjection{
		{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}},
		{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "parent"}}},
		{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "child"}}},
		{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "child_parent_idx"}, Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "child"}}},
		{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "child_name_lower_idx"}, Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "child"}}},
		{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "child"}, Name: "child_public"}},
		{Function: &SQLObjectIdentity{Kind: "function", Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "child_touch_fn", Arguments: []TypeIdentity{}}}},
		{Function: &SQLObjectIdentity{Kind: "function", Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "add_one", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "int4"}, {Schema: "pg_catalog", Name: "int4"}}}}},
		{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "child"}, Name: "child_touch"}},
	}
	sort.Slice(declared, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(declared[left])
		rightKey, _ := canonicalContractKey(declared[right])
		return leftKey < rightKey
	})
	migrationID := "000001"
	scope := ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: declared}
	major := livePostgresMajor(t, ctx, connection)
	snapshot := newLiveFixedCatalogSnapshot(connection, major)
	projector := &PGProjector{major: major}
	structure, err := projector.readCatalogStructure(ctx, snapshot, scope, []string{}, []string{"postgres"})
	if err != nil {
		for _, denied := range structure.body.DeniedObjects {
			key, _ := canonicalContractKey(denied)
			t.Logf("denied=%s", key)
		}
		t.Fatalf("project representative catalog: %v denied=%+v", err, structure.body.DeniedObjects)
	}
	if len(structure.body.Relations) != 2 || len(structure.body.Functions) != 2 || len(structure.body.Dependencies) == 0 || len(structure.expressions) < 4 {
		t.Fatalf("representative catalog closure is incomplete: relations=%d functions=%d dependencies=%d expressions=%d", len(structure.body.Relations), len(structure.body.Functions), len(structure.body.Dependencies), len(structure.expressions))
	}
	var child RelationProjection
	internalTriggerKeys := map[string]struct{}{}
	userTriggerSeen := false
	for _, relation := range structure.body.Relations {
		if relation.Identity.Name == "child" {
			child = relation
		}
		for _, trigger := range relation.Triggers {
			if trigger.Identity.Internal != nil {
				key, _ := canonicalContractKey(trigger.Identity)
				if _, duplicate := internalTriggerKeys[key]; duplicate {
					t.Fatalf("internal trigger normalization collided: %s", key)
				}
				internalTriggerKeys[key] = struct{}{}
			}
			if trigger.Identity.Trigger != nil && trigger.Identity.Trigger.Name == "child_touch" {
				userTriggerSeen = true
			}
		}
	}
	if child.Identity.Name == "" || len(child.Columns) != 4 || len(child.Constraints) != 3 || len(child.Indexes) != 3 || len(child.Policies) != 1 || !userTriggerSeen || len(internalTriggerKeys) != 4 {
		t.Fatalf("representative child closure mismatch: child=%+v internal_triggers=%d user_trigger=%v", child, len(internalTriggerKeys), userTriggerSeen)
	}
	var defaultArgumentSeen bool
	for _, function := range structure.body.Functions {
		if function.Identity.Name != "add_one" {
			continue
		}
		if len(function.Arguments) != 2 || function.Arguments[0].Name == nil || *function.Arguments[0].Name != "value" || function.Arguments[1].Name == nil || *function.Arguments[1].Name != "delta" {
			t.Fatalf("function argument projection mismatch: %+v", function.Arguments)
		}
		for _, slot := range structure.expressions {
			if slot.Object.Function != nil && slot.Object.Function.Identity.Name == "add_one" && slot.Field == "function_argument_default" && slot.Ordinal == 2 {
				defaultArgumentSeen = true
			}
		}
	}
	if !defaultArgumentSeen {
		t.Fatal("function argument default expression slot is absent")
	}
	if _, err := structure.completeBody(projector.major); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("representative expression slots escaped completion boundary: %v", err)
	}
	resolved, err := projector.readCatalogStructureWithExpressions(ctx, snapshot, scope, []string{}, []string{"postgres"})
	if err != nil {
		t.Fatalf("normalize representative catalog expressions: %v", err)
	}
	if len(resolved.expressions) != 0 {
		t.Fatalf("representative catalog retained unresolved expression slots: %+v", resolved.expressions)
	}
	resolvedCanonical, err := canonicalContractKey(resolved.body)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PG_CATALOG_EXPRESSIONS major=%d digest=%s", projector.major, DigestBytes([]byte(resolvedCanonical)))
	canonical, err := canonicalContractKey(struct {
		Body        CatalogProjectionBody     `json:"body"`
		Expressions []pgCatalogExpressionSlot `json:"expressions"`
	}{Body: structure.body, Expressions: structure.expressions})
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"pg_toast_", "RI_ConstraintTrigger_", `"prosrc"`} {
		if strings.Contains(canonical, forbidden) {
			t.Fatalf("instance-local or raw function source leaked into structural projection: %q", forbidden)
		}
	}
	t.Logf("PG_CATALOG_STRUCTURE major=%d digest=%s", projector.major, DigestBytes([]byte(canonical)))
}

func TestPGCatalogStructureOnCheckedInMigrations(t *testing.T) {
	dsn := requireCatalogParseDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect PostgreSQL checked-in catalog target: %v", err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	requireEmptyCatalogParseDatabase(t, ctx, connection)
	requireCatalogRolesAbsent(t, ctx, connection)
	setup := `CREATE ROLE cloud_agents_migration_owner NOLOGIN;
CREATE ROLE cloud_agents_runtime NOLOGIN;
CREATE ROLE cloud_agents_bootstrap_admin NOLOGIN`
	if _, err := connection.Exec(ctx, setup); err != nil {
		t.Fatalf("prepare checked-in catalog roles: %v", err)
	}
	defer func() {
		_, _ = connection.Exec(context.Background(), `DROP SCHEMA IF EXISTS cloud_agents CASCADE`)
		_, _ = connection.Exec(context.Background(), `DROP ROLE IF EXISTS cloud_agents_runtime, cloud_agents_bootstrap_admin, cloud_agents_migration_owner`)
	}()
	major := livePostgresMajor(t, ctx, connection)
	snapshot := newLiveFixedCatalogSnapshot(connection, major)
	projector := &PGProjector{major: major}
	for _, head := range []string{"000001", "000002"} {
		sqlBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), "migrations", head+map[string]string{"000001": "_expand_migration_kernel.sql", "000002": "_expand_tenancy.sql"}[head]))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := connection.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("apply checked-in migration %s: %v", head, err)
		}
		contractBytes, err := os.ReadFile(filepath.Join(moduleRoot(t), "migrations", "catalog", "schema-"+head+".json"))
		if err != nil {
			t.Fatal(err)
		}
		contract, err := DecodeCatalogContract(contractBytes)
		if err != nil {
			t.Fatalf("decode checked-in catalog %s: %v", head, err)
		}
		headCopy := head
		scope := ProjectionScope{ScopeKind: "final", SchemaHead: &headCopy, DeclaredObjects: cloneProjectionValue(contract.DeclaredObjectIdentities)}
		structure, err := projector.readCatalogStructure(ctx, snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"})
		if err != nil {
			for _, denied := range structure.body.DeniedObjects {
				key, _ := canonicalContractKey(denied)
				t.Logf("head=%s denied=%s", head, key)
			}
			t.Fatalf("project checked-in catalog %s: %v", head, err)
		}
		if len(structure.body.Relations) == 0 || len(structure.body.Functions) == 0 || len(structure.body.Dependencies) == 0 || len(structure.expressions) == 0 {
			t.Fatalf("checked-in catalog %s is structurally incomplete", head)
		}
		canonical, err := canonicalContractKey(struct {
			Body        CatalogProjectionBody     `json:"body"`
			Expressions []pgCatalogExpressionSlot `json:"expressions"`
		}{Body: structure.body, Expressions: structure.expressions})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("PG_CHECKED_IN_CATALOG major=%d head=%s digest=%s", projector.major, head, DigestBytes([]byte(canonical)))
		resolved, err := projector.readCatalogStructureWithExpressions(ctx, snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"})
		if err != nil {
			t.Fatalf("normalize checked-in catalog expressions %s: %v", head, err)
		}
		if len(resolved.expressions) != 0 {
			t.Fatalf("checked-in catalog %s retained unresolved expression slots", head)
		}
		resolvedCanonical, err := canonicalContractKey(resolved.body)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("PG_CHECKED_IN_EXPRESSIONS major=%d head=%s digest=%s", projector.major, head, DigestBytes([]byte(resolvedCanonical)))
	}
}

func TestPGCatalogPG17RejectsWiderMaintainGrant(t *testing.T) {
	dsn := requireCatalogParseDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close(context.Background()) }()
	major := livePostgresMajor(t, ctx, connection)
	if major != 17 {
		t.Skip("MAINTAIN is a PostgreSQL 17 catalog privilege")
	}
	requireEmptyCatalogParseDatabase(t, ctx, connection)
	if _, err := connection.Exec(ctx, `CREATE SCHEMA cloud_agents;
CREATE TABLE cloud_agents.maintained (id integer);
GRANT MAINTAIN ON TABLE cloud_agents.maintained TO PUBLIC`); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = connection.Exec(context.Background(), `DROP SCHEMA IF EXISTS cloud_agents CASCADE`) }()
	declared := []ObjectIdentityProjection{
		{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "maintained"}}},
		{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}},
	}
	sort.Slice(declared, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(declared[left])
		rightKey, _ := canonicalContractKey(declared[right])
		return leftKey < rightKey
	})
	head := "000001"
	scope := ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: declared}
	projector := &PGProjector{major: major}
	if _, err := projector.readCatalogStructure(ctx, newLiveFixedCatalogSnapshot(connection, major), scope, []string{}, []string{"postgres"}); !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("wider PG17 MAINTAIN grant was accepted: %v", err)
	}
}

func requireCatalogParseDSN(t *testing.T) string {
	t.Helper()
	if os.Getenv("CLOUD_AGENTS_REQUIRE_CATALOG_PARSE_TEST") != "1" {
		t.Skip("CLOUD_AGENTS_REQUIRE_CATALOG_PARSE_TEST is not set")
	}
	dsn := os.Getenv("CLOUD_AGENTS_CATALOG_PARSE_DSN")
	if dsn == "" {
		t.Fatal("CLOUD_AGENTS_CATALOG_PARSE_DSN is required by the explicit catalog parse test gate")
	}
	return dsn
}

func requireEmptyCatalogParseDatabase(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	var database string
	var schemaExists bool
	if err := connection.QueryRow(ctx, `SELECT current_database(), EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = 'cloud_agents')`).Scan(&database, &schemaExists); err != nil {
		t.Fatalf("verify catalog parse database boundary: %v", err)
	}
	if database != "cag_catalog_parse" {
		t.Fatalf("catalog parse test refuses database %q", database)
	}
	if schemaExists {
		t.Fatal("catalog parse test refuses to replace an existing cloud_agents schema")
	}
}

func requireCatalogRolesAbsent(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	var count uint32
	if err := connection.QueryRow(ctx, `SELECT pg_catalog.count(*)::pg_catalog.int4 FROM pg_catalog.pg_roles WHERE rolname = ANY($1::pg_catalog.text[])`, []string{MigrationOwnerRole, RuntimeRole, BootstrapAdminRole}).Scan(&count); err != nil {
		t.Fatalf("verify catalog parse role boundary: %v", err)
	}
	if count != 0 {
		t.Fatal("catalog parse test refuses to replace existing Cloud Agents roles")
	}
}

func newLiveFixedCatalogSnapshot(connection *pgx.Conn, major uint16) *fixedQueryProjectionSnapshot {
	return &fixedQueryProjectionSnapshot{
		queryer: pgxQueryer{queryer: connection}, metadata: SnapshotMetadata{PostgresMajor: major}, started: time.Now(),
	}
}

func livePostgresMajor(t *testing.T, ctx context.Context, connection *pgx.Conn) uint16 {
	t.Helper()
	var version uint32
	if err := connection.QueryRow(ctx, `SELECT current_setting('server_version_num')::pg_catalog.int4`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return uint16(version / 10000)
}
