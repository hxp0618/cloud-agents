package migration

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestPGCatalogStructureProjectsExpressionFreeRelationAndFunction(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, false, false)
	projector := &PGProjector{major: 17, capabilities: pgProjectionCapabilities{Major: 17}, normalizer: pg17Normalizer{}}
	structure, err := projector.readCatalogStructure(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil {
		t.Fatal(err)
	}
	if structure.absent || len(structure.expressions) != 0 || len(structure.body.Relations) != 1 || len(structure.body.Functions) != 1 || len(structure.body.DeniedObjects) != 0 {
		t.Fatalf("unexpected catalog structure: %+v", structure)
	}
	if got := structure.body.Relations[0]; got.Identity.Name != "jobs" || got.Relkind != "table" || got.Persistence != "permanent" || got.AccessMethod == nil || *got.AccessMethod != "heap" {
		t.Fatalf("relation normalization mismatch: %+v", got)
	}
	if got := structure.body.Functions[0]; got.Identity.Name != "catalog_probe" || got.Kind != "function" || got.Volatility != "immutable" || got.Parallel != "safe" || got.ProsrcSHA256 != DigestBytes([]byte("SELECT true")) {
		t.Fatalf("function normalization mismatch: %+v", got)
	}
	if _, err := structure.completeBody(17); err != nil {
		r := structure.body.Relations[0]
		t.Fatalf("expression-free A2.1b body did not validate: %v nils=%v/%v/%v/%v/%v/%v/%v", err,
			r.ExplicitACL.Entries == nil, r.Reloptions == nil, r.Columns == nil, r.Constraints == nil, r.Indexes == nil, r.Policies == nil, r.Triggers == nil)
	}
	for id := projectionQueryNamespace; id <= projectionQueryCatalogDependencies; id++ {
		if len(snapshot.queries[id]) != 0 {
			t.Fatalf("query id %d was not consumed exactly once", id)
		}
	}
}

func TestPGCatalogStructureCarriesClosedExpressionSlotsWithoutMintingFullProjection(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, true, false)
	projector := &PGProjector{major: 16, capabilities: pgProjectionCapabilities{Major: 16}, normalizer: pg16Normalizer{}}
	structure, err := projector.readCatalogStructure(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil {
		t.Fatal(err)
	}
	if len(structure.expressions) != 1 || structure.expressions[0].Field != "column_default" || structure.expressions[0].Object.Column == nil || structure.expressions[0].Object.Column.Name != "created_at" {
		t.Fatalf("column expression slot was not retained exactly: %+v", structure.expressions)
	}
	if len(structure.body.Relations[0].Columns) != 1 || structure.body.Relations[0].Columns[0].Default != nil {
		t.Fatalf("impl-1 fabricated an expression projection: %+v", structure.body.Relations[0].Columns)
	}
	if _, err := structure.completeBody(16); !IsCode(err, CodeProjectionNotImplemented) {
		t.Fatalf("unresolved expression structure escaped completion boundary: %v", err)
	}
}

func TestPGCatalogStructureDrainsThenRejectsUndeclaredRelation(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, false, true)
	projector := &PGProjector{major: 15, capabilities: pgProjectionCapabilities{Major: 15}, normalizer: pg15Normalizer{}}
	structure, err := projector.readCatalogStructure(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("undeclared relation error = %v", err)
	}
	if len(structure.body.DeniedObjects) != 1 || structure.body.DeniedObjects[0].Object.Relation == nil || structure.body.DeniedObjects[0].Object.Relation.Identity.Name != "shadow" || structure.body.DeniedObjects[0].ReasonCode != "undeclared_object" {
		t.Fatalf("denied object set lost the undeclared relation: %+v", structure.body.DeniedObjects)
	}
	for id := projectionQueryNamespace; id <= projectionQueryCatalogDependencies; id++ {
		if len(snapshot.queries[id]) != 0 {
			t.Fatalf("denied path did not drain query id %d", id)
		}
	}
}

func TestPGCatalogStructureRejectsDependencyOutsideClosedAnchors(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, false, false)
	snapshot.queries[projectionQueryCatalogDependencies] = []pgTestQuery{{rows: [][]any{{
		"pg_class", "100", "0", "unsupported", "999", "0", "n",
		nil, nil, nil, []string{}, []string{},
	}}}}
	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	structure, err := projector.readCatalogStructure(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if !IsCode(err, CodeProjectionUnknownObject) {
		t.Fatalf("dependency outside closure error = %v", err)
	}
	if len(structure.body.DeniedObjects) != 1 || structure.body.DeniedObjects[0].ReasonCode != "dependency_outside_closure" || structure.body.DeniedObjects[0].DependencyKind == nil || *structure.body.DeniedObjects[0].DependencyKind != "normal" {
		t.Fatalf("dependency denial was not retained exactly: %+v", structure.body.DeniedObjects)
	}
}

func TestPGCatalogFunctionIdentityMustMatchFullArgumentRows(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, false, false)
	snapshot.queries[projectionQueryCatalogFunctionArguments] = []pgTestQuery{{rows: [][]any{{
		"200", "catalog_probe", "1", nil, "i", "23", "pg_catalog", "int4", false,
	}}}}
	projector := &PGProjector{major: 17, normalizer: pg17Normalizer{}}
	if _, err := projector.readCatalogStructure(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole}); !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("function identity/full argument mismatch error = %v", err)
	}
}

func TestPGCatalogPG17MaintainNormalizationIsOwnerOnly(t *testing.T) {
	owner := MigrationOwnerRole
	maintain := "MAINTAIN"
	grantable := false
	if skip, err := normalizeVersionedRelationACL(17, owner, &owner, &owner, &maintain, &grantable); err != nil || !skip {
		t.Fatalf("PG17 owner baseline was not normalized: skip=%v err=%v", skip, err)
	}
	for name, values := range map[string]struct {
		major   uint16
		grantor string
		grantee string
		grant   bool
	}{
		"pg16":       {major: 16, grantor: owner, grantee: owner},
		"other_role": {major: 17, grantor: owner, grantee: "runtime"},
		"grantable":  {major: 17, grantor: owner, grantee: owner, grant: true},
	} {
		t.Run(name, func(t *testing.T) {
			if skip, err := normalizeVersionedRelationACL(values.major, owner, &values.grantor, &values.grantee, &maintain, &values.grant); skip || !IsCode(err, CodeProjectionUnknownObject) {
				t.Fatalf("wider MAINTAIN grant was normalized: skip=%v err=%v", skip, err)
			}
		})
	}
}

func TestPGCatalogQueryRegistryHasClosedStructuralFacts(t *testing.T) {
	for id := projectionQueryCatalogRelations; id <= projectionQueryCatalogExpressions; id++ {
		query, ok := projectionFixedQuery(id)
		if !ok || query == "" {
			t.Fatalf("catalog query %d is absent", id)
		}
		for _, required := range []string{"pg_catalog", "ORDER BY"} {
			if !stringsContains(query, required) {
				t.Fatalf("catalog query %d omits %q", id, required)
			}
		}
	}
	dependencies, _ := projectionFixedQuery(projectionQueryCatalogDependencies)
	for _, class := range []string{"pg_class", "pg_constraint", "pg_trigger", "pg_policy", "pg_proc", "pg_type"} {
		if !stringsContains(dependencies, class) {
			t.Fatalf("dependency query omits %s", class)
		}
	}
}

func TestPGCatalogInternalCompletionCannotReachProductionProjector(t *testing.T) {
	allowed := map[string]struct{}{
		"projection_expression.go\x00readCatalogStructureWithExpressions\x00readCatalogStructure": {},
		"projection_expression.go\x00readCatalogExpressions\x00completeBody":                      {},
		"projection_pg_adapter.go\x00ProjectCatalog\x00readCatalogStructureWithExpressions":       {},
		"projection_pg_adapter.go\x00ProjectCatalog\x00completeBody":                              {},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				var called string
				switch target := call.Fun.(type) {
				case *ast.Ident:
					called = target.Name
				case *ast.SelectorExpr:
					called = target.Sel.Name
				}
				if called != "readCatalogStructure" && called != "readCatalogStructureWithExpressions" && called != "completeBody" {
					return true
				}
				key := name + "\x00" + function.Name.Name + "\x00" + called
				if _, ok := allowed[key]; !ok {
					t.Errorf("%s:%s calls internal catalog completion seam %s", name, function.Name.Name, called)
				}
				return true
			})
		}
	}
}

func pgCatalogTestScope() ProjectionScope {
	migrationID := "000001"
	identities := []ObjectIdentityProjection{
		{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}},
		{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "jobs"}}},
		{Function: &SQLObjectIdentity{Kind: "function", Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "catalog_probe", Arguments: []TypeIdentity{}}}},
	}
	sort.Slice(identities, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(identities[left])
		rightKey, _ := canonicalContractKey(identities[right])
		return leftKey < rightKey
	})
	return ProjectionScope{ScopeKind: "predecessor", MigrationID: &migrationID, DeclaredObjects: identities}
}

func pgCatalogTestSnapshot(scope ProjectionScope, withDefault, withExtraRelation bool) *pgTestSnapshot {
	owner := MigrationOwnerRole
	namespaceRows := [][]any{
		pgNamespaceRow("function", pgString(projectionTargetSchema), &owner, false, nil, nil, nil, nil, pgString("catalog_probe"), pgString("f"), nil),
		pgNamespaceRow("relation", pgString(projectionTargetSchema), &owner, false, nil, nil, nil, nil, pgString("jobs"), pgString("r"), nil),
	}
	relationRows := [][]any{pgCatalogRelationRow("100", "jobs")}
	if withExtraRelation {
		namespaceRows = append(namespaceRows, pgNamespaceRow("relation", pgString(projectionTargetSchema), &owner, false, nil, nil, nil, nil, pgString("shadow"), pgString("r"), nil))
		relationRows = append(relationRows, pgCatalogRelationRow("101", "shadow"))
	}
	namespaceRows = append(namespaceRows, pgNamespaceRow("schema", pgString(projectionTargetSchema), &owner, true, nil, nil, nil, nil, nil, nil, nil))
	columnRows := [][]any{}
	if withDefault {
		columnRows = append(columnRows, []any{
			"100", "jobs", "1", "created_at", "1184", "pg_catalog", "timestamptz", "-1",
			nil, nil, nil, true, "", "", true, "p", "", true, nil, nil, nil, nil,
		})
	}
	queries := map[projectionQueryID][]pgTestQuery{
		projectionQueryNamespace:                {{rows: namespaceRows}},
		projectionQueryNamespaceCreators:        {{rows: [][]any{{MigrationOwnerRole}}}},
		projectionQueryDefaultACLs:              {{rows: [][]any{}}},
		projectionQueryCatalogRelations:         {{rows: relationRows}},
		projectionQueryCatalogColumns:           {{rows: columnRows}},
		projectionQueryCatalogConstraints:       {{rows: [][]any{}}},
		projectionQueryCatalogIndexes:           {{rows: [][]any{}}},
		projectionQueryCatalogIndexTerms:        {{rows: [][]any{}}},
		projectionQueryCatalogPolicies:          {{rows: [][]any{}}},
		projectionQueryCatalogTriggers:          {{rows: [][]any{}}},
		projectionQueryCatalogFunctions:         {{rows: [][]any{pgCatalogFunctionRow()}}},
		projectionQueryCatalogFunctionArguments: {{rows: [][]any{}}},
		projectionQueryCatalogInternalObjects:   {{rows: [][]any{}}},
		projectionQueryCatalogDependencies:      {{rows: [][]any{}}},
		projectionQueryCatalogExpressions:       {{rows: [][]any{}}},
	}
	return &pgTestSnapshot{metadata: pgTestMetadata(17), queries: queries}
}

func pgCatalogRelationRow(id, name string) []any {
	return []any{id, name, "r", "p", pgString("heap"), MigrationOwnerRole, true, nil, nil, nil, nil, []string{}, "d", false, false}
}

func pgCatalogFunctionRow() []any {
	return []any{
		"200", "catalog_probe", "f", "sql", []string{}, []string{},
		nil, nil, nil, "16", "pg_catalog", "bool", false, MigrationOwnerRole, true,
		nil, nil, nil, nil, false, "i", "s", false, false, []string{}, "100", "0", "SELECT true", nil,
	}
}

func stringsContains(value, fragment string) bool {
	return len(fragment) == 0 || len(value) >= len(fragment) && strings.Index(value, fragment) >= 0
}
