package migration

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

func TestPGCatalogExpressionResolutionCompletesOrdinaryBody(t *testing.T) {
	scope := pgCatalogTestScope()
	snapshot := pgCatalogTestSnapshot(scope, true, false)
	snapshot.queries[projectionQueryCatalogExpressions] = []pgTestQuery{{rows: [][]any{pgClockExpressionRow()}}}
	projector := &PGProjector{major: 17, capabilities: pgProjectionCapabilities{Major: 17}, normalizer: pg17Normalizer{}}
	structure, err := projector.readCatalogStructureWithExpressions(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
	if err != nil {
		t.Fatal(err)
	}
	if len(structure.expressions) != 0 || len(structure.body.Relations) != 1 || len(structure.body.Relations[0].Columns) != 1 {
		t.Fatalf("resolved catalog structure is incomplete: %+v", structure)
	}
	defaultNode := structure.body.Relations[0].Columns[0].Default
	if defaultNode == nil || defaultNode.Kind != "function" || defaultNode.Identity == nil || defaultNode.Identity.Schema != "pg_catalog" || defaultNode.Identity.Name != "clock_timestamp" {
		t.Fatalf("column default was not normalized: %+v", defaultNode)
	}
	if _, err := structure.completeBody(17); err != nil {
		t.Fatalf("resolved ordinary body did not validate: %v", err)
	}
	for id := projectionQueryNamespace; id <= projectionQueryCatalogExpressions; id++ {
		if len(snapshot.queries[id]) != 0 {
			t.Fatalf("expression completion did not consume query id %d", id)
		}
	}
}

func TestPGCatalogExpressionResolutionRequiresExactSourceCoverage(t *testing.T) {
	scope := pgCatalogTestScope()
	tests := []struct {
		name string
		rows [][]any
	}{
		{name: "missing", rows: [][]any{}},
		{name: "duplicate", rows: [][]any{pgClockExpressionRow(), pgClockExpressionRow()}},
		{name: "unknown-node", rows: [][]any{func() []any {
			row := pgClockExpressionRow()
			row[9] = "{SUBLINK :subLinkType 0}"
			return row
		}()}},
		{name: "wrong-slot", rows: [][]any{func() []any {
			row := pgClockExpressionRow()
			row[7] = "trigger_when"
			return row
		}()}},
		{name: "foreign-function-overlay", rows: [][]any{func() []any {
			row := pgClockExpressionRow()
			row[3] = pgString("cloud_agents")
			return row
		}()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := pgCatalogTestSnapshot(scope, true, false)
			snapshot.queries[projectionQueryCatalogExpressions] = []pgTestQuery{{rows: test.rows}}
			projector := &PGProjector{major: 17, capabilities: pgProjectionCapabilities{Major: 17}, normalizer: pg17Normalizer{}}
			structure, err := projector.readCatalogStructureWithExpressions(context.Background(), snapshot, scope, []string{MigrationOwnerRole}, []string{MigrationOwnerRole})
			if err == nil || len(structure.body.Relations) != 0 || len(structure.expressions) != 0 {
				t.Fatalf("expression source fault %q did not fail closed: structure=%+v err=%v", test.name, structure, err)
			}
		})
	}
}

func TestPGExpressionNormalizerClosedGrammar(t *testing.T) {
	body := CatalogProjectionBody{
		Relations: []RelationProjection{{
			Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "items"},
			Columns: []ColumnProjection{
				{Name: "id", Type: TypeIdentity{Schema: "pg_catalog", Name: "int8"}},
				{Name: "name", Type: TypeIdentity{Schema: "pg_catalog", Name: "text"}},
				{Name: "state", Type: TypeIdentity{Schema: "pg_catalog", Name: "text"}},
				{Name: "updated_at", Type: TypeIdentity{Schema: "pg_catalog", Name: "timestamptz"}},
			},
		}},
		Functions: []FunctionProjection{
			{Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "is_valid_identifier", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "text"}}}, Kind: "function", Returns: TypeIdentity{Schema: "pg_catalog", Name: "bool"}},
			{Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "require_tenant_id", Arguments: []TypeIdentity{}}, Kind: "function", Returns: TypeIdentity{Schema: "pg_catalog", Name: "text"}},
		},
		Dependencies: []DependencyProjection{},
	}
	resolver := newPGExpressionResolver(17, &body)
	relation := &body.Relations[0]
	cases := []struct {
		name     string
		input    string
		expected TypeIdentity
		kind     string
	}{
		{name: "range", input: "((char_length(name) >= 1) AND (char_length(name) <= 160))", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "boolean"},
		{name: "array", input: "(state = ANY (ARRAY['active'::text, 'suspended'::text]))", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "scalar_array_operator"},
		{name: "null-or", input: "((name IS NULL) OR (name <> state))", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "boolean"},
		{name: "user-function", input: "cloud_agents.is_valid_identifier(name)", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "function"},
		{name: "tenant", input: "(name = cloud_agents.require_tenant_id())", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "operator"},
		{name: "clock", input: "clock_timestamp()", expected: TypeIdentity{Schema: "pg_catalog", Name: "timestamptz"}, kind: "function"},
		{name: "session-user", input: "SESSION_USER", expected: TypeIdentity{Schema: "pg_catalog", Name: "text"}, kind: "sql_value"},
		{name: "generated", input: "'project'::text", expected: TypeIdentity{Schema: "pg_catalog", Name: "text"}, kind: "constant"},
		{name: "int8", input: "(id > 0)", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "operator"},
		{name: "timestamp", input: "(updated_at >= updated_at)", expected: TypeIdentity{Schema: "pg_catalog", Name: "bool"}, kind: "operator"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			parser, err := newPGExpressionParser(test.input)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parser.parseList()
			if err != nil || len(parsed) != 1 {
				t.Fatalf("parse %q: %v", test.input, err)
			}
			node, count, err := resolver.normalize(parsed[0], relation, &test.expected, 0)
			if err != nil {
				t.Fatalf("normalize %q: %v", test.input, err)
			}
			if node.Kind != test.kind || count == 0 || node.Type == nil || *node.Type != test.expected {
				t.Fatalf("normalized %q = %+v count=%d", test.input, node, count)
			}
			if err := validateExpressionNode(node); err != nil {
				t.Fatalf("validate %q: %v", test.input, err)
			}
		})
	}
}

func TestPGExpressionRawNodeAndASTFaultsFailClosed(t *testing.T) {
	validRaw := "{BOOLEXPR :boolop and :args ({CONST :consttype 16} {CONST :consttype 16})}"
	if err := validatePGNodeTreeWitness(validRaw, 1); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown":     "{SUBLINK :subLinkType 0}",
		"unbalanced":  "{CONST :consttype 16",
		"cardinality": "({CONST :consttype 16} {CONST :consttype 16})",
		"nul":         "{CONST\x00 :consttype 16}",
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePGNodeTreeWitness(raw, 1); err == nil {
				t.Fatalf("raw node fault was accepted: %q", raw)
			}
		})
	}
	boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
	node := ExpressionNode{Kind: "constant", Type: &boolType, Value: true, Fields: map[string]string{"format": "boolean"}, Children: []ExpressionNode{}}
	if err := validateExpressionNode(node); err != nil {
		t.Fatal(err)
	}
	faults := []func(*ExpressionNode){
		func(value *ExpressionNode) { value.Kind = "unknown" },
		func(value *ExpressionNode) { value.Type = nil },
		func(value *ExpressionNode) { value.Fields["extra"] = "x" },
		func(value *ExpressionNode) { value.Children = nil },
		func(value *ExpressionNode) { value.Value = "true" },
	}
	for index, mutate := range faults {
		candidate := cloneProjectionValue(node)
		mutate(&candidate)
		if err := validateExpressionNode(candidate); err == nil {
			t.Fatalf("expression AST fault %d was accepted: %+v", index, candidate)
		}
	}
	if reflect.DeepEqual(node.Fields, map[string]string{}) {
		t.Fatal("test fixture lost its closed field")
	}
}

func TestPGExpressionParserRejectsUnknownAndTrailingForms(t *testing.T) {
	for _, input := range []string{
		"EXISTS (SELECT 1)", "name COLLATE pg_catalog.default", "name AT TIME ZONE 'UTC'",
		"name =", "name; DROP TABLE x", "f(,)",
	} {
		parser, err := newPGExpressionParser(input)
		if err == nil {
			_, err = parser.parseList()
		}
		if err == nil {
			t.Fatalf("unsupported expression was accepted: %q", input)
		}
	}
}

func TestPGExpressionSemanticClosureRejectsCrossBoundMutations(t *testing.T) {
	body := semanticExpressionBody(t)
	if err := validateCatalogExpressionClosure(body); err != nil {
		t.Fatalf("baseline semantic expression body is invalid: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*CatalogProjectionBody)
	}{
		{name: "unknown-column", mutate: func(body *CatalogProjectionBody) {
			body.Relations[0].Constraints[0].Expression.Children[0].Children[0].Fields["name"] = "missing"
		}},
		{name: "column-type", mutate: func(body *CatalogProjectionBody) {
			body.Relations[0].Columns[0].Type = TypeIdentity{Schema: "pg_catalog", Name: "int8"}
		}},
		{name: "function-return", mutate: func(body *CatalogProjectionBody) {
			body.Functions[0].Returns = TypeIdentity{Schema: "pg_catalog", Name: "int8"}
		}},
		{name: "missing-expression-dependency", mutate: func(body *CatalogProjectionBody) {
			body.Dependencies = nil
		}},
		{name: "operator-result", mutate: func(body *CatalogProjectionBody) {
			body.Relations[0].Constraints[0].Expression.Type = cloneTypeIdentityPointer(&TypeIdentity{Schema: "pg_catalog", Name: "int8"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneProjectionValue(body)
			test.mutate(&candidate)
			if err := validateCatalogExpressionClosure(candidate); err == nil {
				t.Fatalf("semantic mutation %q was accepted", test.name)
			}
		})
	}
}

func TestPGExpressionSemanticClosureEnforcesAggregateNodeLimit(t *testing.T) {
	textType := TypeIdentity{Schema: "pg_catalog", Name: "text"}
	relation := RelationProjection{Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "bulk"}, Columns: make([]ColumnProjection, projectionMaxExpressionNodes+1)}
	for index := range relation.Columns {
		relation.Columns[index] = ColumnProjection{
			Name: fmt.Sprintf("c%04d", index), Type: textType,
			Default: &ExpressionNode{Kind: "constant", Type: cloneTypeIdentityPointer(&textType), Value: "x", Fields: map[string]string{"format": "string"}, Children: []ExpressionNode{}},
		}
	}
	body := CatalogProjectionBody{Relations: []RelationProjection{relation}}
	if err := validateCatalogExpressionClosure(body); err == nil {
		t.Fatal("aggregate expression node limit was not enforced")
	}
}

func semanticExpressionBody(t *testing.T) CatalogProjectionBody {
	t.Helper()
	textType := TypeIdentity{Schema: "pg_catalog", Name: "text"}
	functionIdentity := SQLIdentity{Schema: projectionTargetSchema, Name: "normalize", Arguments: []TypeIdentity{textType}}
	body := CatalogProjectionBody{
		Relations: []RelationProjection{{
			Identity:    TypeIdentity{Schema: projectionTargetSchema, Name: "items"},
			Columns:     []ColumnProjection{{Name: "name", Type: textType}},
			Constraints: []ConstraintProjection{{Name: "items_name_check", Type: "check"}},
		}},
		Functions:    []FunctionProjection{{Identity: functionIdentity, Kind: "function", Returns: textType}},
		Dependencies: []DependencyProjection{},
	}
	resolver := newPGExpressionResolver(17, &body)
	parser, err := newPGExpressionParser("(cloud_agents.normalize(name) = 'x'::text)")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.parseList()
	if err != nil || len(parsed) != 1 {
		t.Fatalf("parse semantic fixture: %v", err)
	}
	relation := &body.Relations[0]
	boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
	node, _, err := resolver.normalize(parsed[0], relation, &boolType, 0)
	if err != nil {
		t.Fatalf("normalize semantic fixture: %v", err)
	}
	body.Relations[0].Constraints[0].Expression = &node
	owner := ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.Identity, Name: "items_name_check"}}
	if err := resolver.addExpressionDependencies(owner, node); err != nil {
		t.Fatal(err)
	}
	if err := resolver.finishDependencies(); err != nil {
		t.Fatal(err)
	}
	return body
}

func pgClockExpressionRow() []any {
	return []any{
		"column", pgString("jobs"), pgString("created_at"), nil, nil,
		[]string{}, []string{}, "column_default", []string{"0"},
		"{FUNCEXPR :funcid 2649 :funcresulttype 1184 :funcretset false :funcvariadic false :funcformat 0 :funccollid 0 :inputcollid 0 :args <> :location -1}",
		"clock_timestamp()", []string{"pg_catalog"}, []string{"timestamptz"},
	}
}
