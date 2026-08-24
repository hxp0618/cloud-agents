package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const expressionProfileV1 = "cloud-agents-sql-expression/v1"

var expressionRawNodeKinds = map[string]struct{}{
	"ARRAYEXPR": {}, "BOOLEXPR": {}, "CONST": {}, "COERCEVIAIO": {},
	"FUNCEXPR": {}, "NULLTEST": {}, "OPEXPR": {}, "RELABELTYPE": {},
	"SCALARARRAYOPEXPR": {}, "SQLVALUEFUNCTION": {}, "VAR": {},
}

type pgExpressionSource struct {
	sourceKind          string
	relationName        *string
	objectName          *string
	functionSchema      *string
	functionName        *string
	functionArgSchemas  []string
	functionArgNames    []string
	field               string
	ordinals            []uint32
	rawNodes            string
	deparsed            string
	expectedTypeSchemas []string
	expectedTypeNames   []string
}

type pgParsedExpression struct {
	kind     string
	name     []string
	value    any
	fields   map[string]string
	children []*pgParsedExpression
}

type pgExpressionFunction struct {
	identity SQLIdentity
	returns  TypeIdentity
}

type pgExpressionResolver struct {
	major     uint16
	body      *CatalogProjectionBody
	functions []pgExpressionFunction
	resolved  map[string]struct{}
	nodeCount uint64
}

func (projector *PGProjector) readCatalogStructureWithExpressions(ctx context.Context, snapshot ProjectionSnapshot, scope ProjectionScope, defaultACLOwners, objectCreatorClosure []string) (pgCatalogStructure, error) {
	structure, err := projector.readCatalogStructure(ctx, snapshot, scope, defaultACLOwners, objectCreatorClosure)
	if err != nil {
		return structure, err
	}
	return projector.readCatalogExpressions(ctx, snapshot, structure)
}

func (projector *PGProjector) readCatalogExpressions(ctx context.Context, snapshot ProjectionSnapshot, structure pgCatalogStructure) (pgCatalogStructure, error) {
	if structure.absent {
		return structure, nil
	}
	resolver := newPGExpressionResolver(projector.major, &structure.body)
	rows, cancel, err := queryProjectionBounded(ctx, snapshot, projectionQueryCatalogExpressions, projectionTargetSchema)
	if err != nil {
		return pgCatalogStructure{}, err
	}
	defer cancel()
	defer rows.Close()
	budget := projectionReadBudget{major: projector.major}
	for rows.Next() {
		var sourceKind, field, rawNodes, deparsed string
		var relationName, objectName, functionSchema, functionName *string
		var functionArgSchemas, functionArgNames, ordinalTexts, expectedTypeSchemas, expectedTypeNames []string
		if err := rows.Scan(&sourceKind, &relationName, &objectName, &functionSchema, &functionName,
			&functionArgSchemas, &functionArgNames, &field, &ordinalTexts, &rawNodes, &deparsed,
			&expectedTypeSchemas, &expectedTypeNames); err != nil {
			return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.scan", projector.major, "expression source scan failed")
		}
		if err := budget.add("catalog.expressions", sourceKind, nullableString(relationName), nullableString(objectName), nullableString(functionSchema), nullableString(functionName), strings.Join(functionArgSchemas, "\x00"), strings.Join(functionArgNames, "\x00"), field, strings.Join(ordinalTexts, "\x00"), rawNodes, deparsed, strings.Join(expectedTypeSchemas, "\x00"), strings.Join(expectedTypeNames, "\x00")); err != nil {
			return pgCatalogStructure{}, err
		}
		source, err := decodePGExpressionSource(projector.major, sourceKind, relationName, objectName, functionSchema, functionName, functionArgSchemas, functionArgNames, field, ordinalTexts, rawNodes, deparsed, expectedTypeSchemas, expectedTypeNames)
		if err != nil {
			return pgCatalogStructure{}, err
		}
		if err := resolver.consume(source); err != nil {
			return pgCatalogStructure{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.iteration", projector.major, "expression source iteration failed")
	}
	for _, slot := range structure.expressions {
		key, err := pgExpressionSlotKey(slot.Object, slot.Field, slot.Ordinal)
		if err != nil {
			return pgCatalogStructure{}, err
		}
		if _, ok := resolver.resolved[key]; !ok {
			return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.coverage", projector.major, "catalog expression slot has no exact normalized source")
		}
	}
	if len(resolver.resolved) != len(structure.expressions) {
		return pgCatalogStructure{}, pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.coverage", projector.major, "catalog expression sources differ from structural expression slots")
	}
	if err := resolver.finishDependencies(); err != nil {
		return pgCatalogStructure{}, err
	}
	structure.expressions = []pgCatalogExpressionSlot{}
	if _, err := structure.completeBody(projector.major); err != nil {
		return pgCatalogStructure{}, err
	}
	return structure, nil
}

func decodePGExpressionSource(major uint16, sourceKind string, relationName, objectName, functionSchema, functionName *string, functionArgSchemas, functionArgNames []string, field string, ordinalTexts []string, rawNodes, deparsed string, expectedTypeSchemas, expectedTypeNames []string) (pgExpressionSource, error) {
	if sourceKind == "" || field == "" || rawNodes == "" || deparsed == "" || len(ordinalTexts) == 0 || len(ordinalTexts) != len(expectedTypeSchemas) || len(ordinalTexts) != len(expectedTypeNames) {
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "expression source row is sparse")
	}
	ordinals := make([]uint32, len(ordinalTexts))
	for index, value := range ordinalTexts {
		ordinal, err := parsePGUint32(value, "catalog.expressions.ordinal", major)
		if err != nil {
			return pgExpressionSource{}, err
		}
		if index > 0 && ordinal <= ordinals[index-1] {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.ordinal", major, "expression ordinals are duplicate or unsorted")
		}
		ordinals[index] = ordinal
		if expectedTypeSchemas[index] == "" || expectedTypeNames[index] == "" {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.type", major, "expression result type is incomplete")
		}
	}
	if err := validatePGNodeTreeWitness(rawNodes, len(ordinals)); err != nil {
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.raw_node", major, "expression raw node tree is outside the closed adapter profile")
	}
	if len(functionArgSchemas) != len(functionArgNames) {
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.function_identity", major, "expression function identity arrays differ")
	}
	functionSource := sourceKind == "function_defaults"
	if functionSource != (functionSchema != nil && functionName != nil) || functionSource && (*functionSchema == "" || *functionName == "") || functionSource && (relationName != nil || objectName != nil) || !functionSource && (functionSchema != nil || functionName != nil || len(functionArgSchemas) != 0) {
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.function_identity", major, "expression function identity is sparse")
	}
	if !functionSource && (relationName == nil || objectName == nil || *relationName == "" || *objectName == "") {
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.object_identity", major, "expression object identity is sparse")
	}
	singletonZero := len(ordinals) == 1 && ordinals[0] == 0
	switch sourceKind {
	case "column":
		if field != "column_default" || !singletonZero {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "column expression source shape is invalid")
		}
	case "constraint":
		if field != "constraint_expression" || !singletonZero {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "constraint expression source shape is invalid")
		}
	case "index_predicate":
		if field != "index_predicate" || !singletonZero {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "index predicate source shape is invalid")
		}
	case "index_terms":
		if field != "index_term" || ordinals[0] == 0 {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "index expression term source shape is invalid")
		}
	case "policy":
		if field != "policy_using" && field != "policy_with_check" || !singletonZero {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "policy expression source shape is invalid")
		}
	case "trigger":
		if field != "trigger_when" || !singletonZero {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "trigger expression source shape is invalid")
		}
	case "function_defaults":
		if field != "function_argument_default" || ordinals[0] == 0 {
			return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "function default expression source shape is invalid")
		}
	default:
		return pgExpressionSource{}, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.source", major, "expression source kind is outside the closed profile")
	}
	return pgExpressionSource{
		sourceKind: sourceKind, relationName: cloneStringPointer(relationName), objectName: cloneStringPointer(objectName),
		functionSchema: cloneStringPointer(functionSchema), functionName: cloneStringPointer(functionName),
		functionArgSchemas: cloneStringsExplicit(functionArgSchemas), functionArgNames: cloneStringsExplicit(functionArgNames),
		field: field, ordinals: ordinals, rawNodes: rawNodes, deparsed: deparsed,
		expectedTypeSchemas: cloneStringsExplicit(expectedTypeSchemas), expectedTypeNames: cloneStringsExplicit(expectedTypeNames),
	}, nil
}

func newPGExpressionResolver(major uint16, body *CatalogProjectionBody) *pgExpressionResolver {
	return &pgExpressionResolver{major: major, body: body, functions: closedPGExpressionFunctions(body.Functions), resolved: make(map[string]struct{})}
}

func closedPGExpressionFunctions(projected []FunctionProjection) []pgExpressionFunction {
	functions := []pgExpressionFunction{
		{identity: SQLIdentity{Schema: "pg_catalog", Name: "char_length", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "text"}}}, returns: TypeIdentity{Schema: "pg_catalog", Name: "int4"}},
		{identity: SQLIdentity{Schema: "pg_catalog", Name: "clock_timestamp", Arguments: []TypeIdentity{}}, returns: TypeIdentity{Schema: "pg_catalog", Name: "timestamptz"}},
		{identity: SQLIdentity{Schema: "pg_catalog", Name: "length", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "text"}}}, returns: TypeIdentity{Schema: "pg_catalog", Name: "int4"}},
		{identity: SQLIdentity{Schema: "pg_catalog", Name: "lower", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "text"}}}, returns: TypeIdentity{Schema: "pg_catalog", Name: "text"}},
	}
	for _, function := range projected {
		if function.Kind != "function" {
			continue
		}
		functions = append(functions, pgExpressionFunction{identity: cloneProjectionValue(function.Identity), returns: function.Returns})
	}
	return functions
}

func (resolver *pgExpressionResolver) consume(source pgExpressionSource) error {
	deparsed := source.deparsed
	if source.sourceKind == "trigger" {
		var err error
		deparsed, err = extractTriggerWhenExpression(source.deparsed)
		if err != nil {
			return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.trigger_deparse", resolver.major, "trigger deparse is outside the closed adapter profile")
		}
	}
	parser, err := newPGExpressionParser(deparsed)
	if err != nil {
		return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.deparse", resolver.major, "expression deparse is outside the closed lexical profile")
	}
	parsed, err := parser.parseList()
	if err != nil || len(parsed) != len(source.ordinals) {
		return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.deparse", resolver.major, "expression deparse differs from its closed source cardinality")
	}
	relation, function, err := resolver.sourceOwner(source)
	if err != nil {
		return err
	}
	for index, parsedExpression := range parsed {
		expected := TypeIdentity{Schema: source.expectedTypeSchemas[index], Name: source.expectedTypeNames[index]}
		node, count, err := resolver.normalize(parsedExpression, relation, &expected, 0)
		if err != nil {
			return err
		}
		if count == 0 || count > projectionMaxExpressionNodes {
			return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.expressions.nodes", resolver.major, "expression node limit exceeded")
		}
		if resolver.nodeCount > projectionMaxExpressionNodes-count {
			return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.expressions.nodes", resolver.major, "catalog expression node closure exceeds the fixed limit")
		}
		resolver.nodeCount += count
		object, err := resolver.expressionObject(source, relation, function, source.ordinals[index])
		if err != nil {
			return err
		}
		key, err := pgExpressionSlotKey(object, source.field, source.ordinals[index])
		if err != nil {
			return err
		}
		if _, duplicate := resolver.resolved[key]; duplicate {
			return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.duplicate", resolver.major, "expression source is duplicate")
		}
		if err := resolver.apply(source, relation, function, source.ordinals[index], object, node); err != nil {
			return err
		}
		resolver.resolved[key] = struct{}{}
	}
	return nil
}

func (resolver *pgExpressionResolver) sourceOwner(source pgExpressionSource) (*RelationProjection, *FunctionProjection, error) {
	if source.sourceKind == "function_defaults" {
		identity, err := sqlIdentityFromParallel(*source.functionSchema, *source.functionName, source.functionArgSchemas, source.functionArgNames)
		if err != nil {
			return nil, nil, pgProjectionFailure(CodeProjectionCatalogQueryFailed, "catalog.expressions.function_identity", resolver.major, "expression function identity is inconsistent")
		}
		for index := range resolver.body.Functions {
			if equalSQLIdentity(resolver.body.Functions[index].Identity, identity) {
				return nil, &resolver.body.Functions[index], nil
			}
		}
		return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.expressions.function", resolver.major, "expression source references an unknown function")
	}
	for index := range resolver.body.Relations {
		if resolver.body.Relations[index].Identity.Name == *source.relationName {
			return &resolver.body.Relations[index], nil, nil
		}
	}
	return nil, nil, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.expressions.relation", resolver.major, "expression source references an unknown relation")
}

func equalSQLIdentity(identity, other SQLIdentity) bool {
	if identity.Schema != other.Schema || identity.Name != other.Name || len(identity.Arguments) != len(other.Arguments) {
		return false
	}
	for index := range identity.Arguments {
		if identity.Arguments[index] != other.Arguments[index] {
			return false
		}
	}
	return true
}

func pgExpressionSlotKey(object ObjectIdentityProjection, field string, ordinal uint32) (string, error) {
	objectKey, err := canonicalContractKey(object)
	if err != nil {
		return "", err
	}
	return objectKey + "\x00" + field + "\x00" + fmt.Sprintf("%010d", ordinal), nil
}

type pgExpressionTokenKind uint8

const (
	pgExpressionEOF pgExpressionTokenKind = iota
	pgExpressionIdentifier
	pgExpressionString
	pgExpressionNumber
	pgExpressionLeftParen
	pgExpressionRightParen
	pgExpressionLeftBracket
	pgExpressionRightBracket
	pgExpressionComma
	pgExpressionDot
	pgExpressionCast
	pgExpressionOperator
)

type pgExpressionToken struct {
	kind  pgExpressionTokenKind
	value string
}

type pgExpressionParser struct {
	tokens []pgExpressionToken
	index  int
}

func newPGExpressionParser(input string) (*pgExpressionParser, error) {
	if input == "" || !utf8.ValidString(input) || len(input) > int(projectionMaxRowBytes) {
		return nil, fmt.Errorf("expression text is invalid")
	}
	tokens, err := lexPGExpression(input)
	if err != nil {
		return nil, err
	}
	return &pgExpressionParser{tokens: tokens}, nil
}

func lexPGExpression(input string) ([]pgExpressionToken, error) {
	tokens := make([]pgExpressionToken, 0, 32)
	for offset := 0; offset < len(input); {
		r := rune(input[offset])
		if r >= utf8.RuneSelf {
			decoded, size := utf8.DecodeRuneInString(input[offset:])
			if decoded == utf8.RuneError || !unicode.IsLetter(decoded) {
				return nil, fmt.Errorf("unsupported expression rune")
			}
			start := offset
			offset += size
			for offset < len(input) {
				next, nextSize := utf8.DecodeRuneInString(input[offset:])
				if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' && next != '$' {
					break
				}
				offset += nextSize
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionIdentifier, value: input[start:offset]})
			continue
		}
		if unicode.IsSpace(r) {
			offset++
			continue
		}
		escapeString := (input[offset] == 'E' || input[offset] == 'e') && offset+1 < len(input) && input[offset+1] == '\''
		if isPGIdentifierStart(input[offset]) && !escapeString {
			start := offset
			offset++
			for offset < len(input) && isPGIdentifierContinue(input[offset]) {
				offset++
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionIdentifier, value: input[start:offset]})
			continue
		}
		switch input[offset] {
		case '(':
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionLeftParen, value: "("})
			offset++
		case ')':
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionRightParen, value: ")"})
			offset++
		case '[':
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionLeftBracket, value: "["})
			offset++
		case ']':
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionRightBracket, value: "]"})
			offset++
		case ',':
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionComma, value: ","})
			offset++
		case '.':
			if offset+1 < len(input) && input[offset+1] >= '0' && input[offset+1] <= '9' {
				start := offset
				offset++
				for offset < len(input) && input[offset] >= '0' && input[offset] <= '9' {
					offset++
				}
				tokens = append(tokens, pgExpressionToken{kind: pgExpressionNumber, value: input[start:offset]})
				continue
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionDot, value: "."})
			offset++
		case ':':
			if offset+1 >= len(input) || input[offset+1] != ':' {
				return nil, fmt.Errorf("unsupported colon")
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionCast, value: "::"})
			offset += 2
		case '\'', 'E', 'e':
			escape := false
			if input[offset] == 'E' || input[offset] == 'e' {
				escape = true
				offset++
			}
			value, next, err := lexPGString(input, offset, escape)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionString, value: value})
			offset = next
		case '"':
			value, next, err := lexPGQuotedIdentifier(input, offset)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionIdentifier, value: value})
			offset = next
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			start := offset
			for offset < len(input) && input[offset] >= '0' && input[offset] <= '9' {
				offset++
			}
			if offset < len(input) && input[offset] == '.' {
				offset++
				for offset < len(input) && input[offset] >= '0' && input[offset] <= '9' {
					offset++
				}
			}
			if offset < len(input) && (input[offset] == 'e' || input[offset] == 'E') {
				offset++
				if offset < len(input) && (input[offset] == '+' || input[offset] == '-') {
					offset++
				}
				exponentStart := offset
				for offset < len(input) && input[offset] >= '0' && input[offset] <= '9' {
					offset++
				}
				if offset == exponentStart {
					return nil, fmt.Errorf("numeric exponent is incomplete")
				}
			}
			tokens = append(tokens, pgExpressionToken{kind: pgExpressionNumber, value: input[start:offset]})
		default:
			if strings.ContainsRune("=<>~+-*/", rune(input[offset])) {
				start := offset
				offset++
				if offset < len(input) && (input[start] == '<' || input[start] == '>') && (input[offset] == '=' || input[start] == '<' && input[offset] == '>') {
					offset++
				}
				tokens = append(tokens, pgExpressionToken{kind: pgExpressionOperator, value: input[start:offset]})
				continue
			}
			return nil, fmt.Errorf("unsupported expression byte %q at %d", input[offset], offset)
		}
		continue
	}
	tokens = append(tokens, pgExpressionToken{kind: pgExpressionEOF})
	return tokens, nil
}

func lexPGString(input string, quoteOffset int, escape bool) (string, int, error) {
	if quoteOffset >= len(input) || input[quoteOffset] != '\'' {
		return "", 0, fmt.Errorf("invalid string")
	}
	var builder strings.Builder
	for offset := quoteOffset + 1; offset < len(input); offset++ {
		if input[offset] == '\'' {
			if offset+1 < len(input) && input[offset+1] == '\'' {
				builder.WriteByte('\'')
				offset++
				continue
			}
			return builder.String(), offset + 1, nil
		}
		if escape && input[offset] == '\\' {
			if offset+1 >= len(input) {
				return "", 0, fmt.Errorf("truncated escape string")
			}
			offset++
			switch input[offset] {
			case '\\', '\'':
				builder.WriteByte(input[offset])
			case 'n':
				builder.WriteByte('\n')
			case 'r':
				builder.WriteByte('\r')
			case 't':
				builder.WriteByte('\t')
			default:
				return "", 0, fmt.Errorf("unsupported escape string")
			}
			continue
		}
		builder.WriteByte(input[offset])
	}
	return "", 0, fmt.Errorf("unterminated string")
}

func lexPGQuotedIdentifier(input string, quoteOffset int) (string, int, error) {
	var builder strings.Builder
	for offset := quoteOffset + 1; offset < len(input); offset++ {
		if input[offset] == '"' {
			if offset+1 < len(input) && input[offset+1] == '"' {
				builder.WriteByte('"')
				offset++
				continue
			}
			if builder.Len() == 0 {
				return "", 0, fmt.Errorf("empty quoted identifier")
			}
			return builder.String(), offset + 1, nil
		}
		builder.WriteByte(input[offset])
	}
	return "", 0, fmt.Errorf("unterminated quoted identifier")
}

func isPGIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isPGIdentifierContinue(value byte) bool {
	return isPGIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}

func (parser *pgExpressionParser) parseList() ([]*pgParsedExpression, error) {
	result := make([]*pgParsedExpression, 0, 1)
	for {
		expression, err := parser.parseExpression(0)
		if err != nil {
			return nil, err
		}
		result = append(result, expression)
		if parser.peek().kind != pgExpressionComma {
			break
		}
		parser.next()
	}
	if parser.peek().kind != pgExpressionEOF {
		return nil, fmt.Errorf("trailing expression token")
	}
	return result, nil
}

func (parser *pgExpressionParser) parseExpression(minimumPrecedence int) (*pgParsedExpression, error) {
	left, err := parser.parsePrefix()
	if err != nil {
		return nil, err
	}
	for {
		if parser.peek().kind == pgExpressionCast {
			parser.next()
			typeName, err := parser.parseTypeName()
			if err != nil {
				return nil, err
			}
			left = &pgParsedExpression{kind: "cast", name: typeName, children: []*pgParsedExpression{left}}
			continue
		}
		if parser.keyword("IS") {
			precedence := 40
			if precedence < minimumPrecedence {
				break
			}
			parser.next()
			negated := false
			if parser.keyword("NOT") {
				parser.next()
				negated = true
			}
			if !parser.keyword("NULL") {
				return nil, fmt.Errorf("only IS NULL is supported")
			}
			parser.next()
			test := "is_null"
			if negated {
				test = "is_not_null"
			}
			left = &pgParsedExpression{kind: "null_test", fields: map[string]string{"test": test}, children: []*pgParsedExpression{left}}
			continue
		}
		operator, precedence, ok := parser.binaryOperator()
		if !ok || precedence < minimumPrecedence {
			break
		}
		parser.next()
		if operator != "and" && operator != "or" && (parser.keyword("ANY") || parser.keyword("ALL")) {
			quantifier := strings.ToLower(parser.next().value)
			if parser.next().kind != pgExpressionLeftParen {
				return nil, fmt.Errorf("scalar-array operand lacks parenthesis")
			}
			right, err := parser.parseExpression(0)
			if err != nil || parser.next().kind != pgExpressionRightParen {
				return nil, fmt.Errorf("invalid scalar-array operand")
			}
			left = &pgParsedExpression{kind: "scalar_array_operator", name: []string{operator}, fields: map[string]string{"quantifier": quantifier}, children: []*pgParsedExpression{left, right}}
			continue
		}
		right, err := parser.parseExpression(precedence + 1)
		if err != nil {
			return nil, err
		}
		kind := "operator"
		fields := map[string]string{}
		if operator == "and" || operator == "or" {
			kind = "boolean"
			fields["operator"] = operator
		}
		left = &pgParsedExpression{kind: kind, name: []string{operator}, fields: fields, children: []*pgParsedExpression{left, right}}
	}
	return left, nil
}

func (parser *pgExpressionParser) parsePrefix() (*pgParsedExpression, error) {
	token := parser.next()
	switch token.kind {
	case pgExpressionLeftParen:
		expression, err := parser.parseExpression(0)
		if err != nil || parser.next().kind != pgExpressionRightParen {
			return nil, fmt.Errorf("unclosed expression parenthesis")
		}
		return expression, nil
	case pgExpressionString:
		return &pgParsedExpression{kind: "constant", value: token.value, fields: map[string]string{"format": "string"}}, nil
	case pgExpressionNumber:
		return &pgParsedExpression{kind: "constant", value: token.value, fields: map[string]string{"format": "numeric"}}, nil
	case pgExpressionOperator:
		if token.value != "+" && token.value != "-" {
			return nil, fmt.Errorf("unsupported prefix operator")
		}
		number := parser.next()
		if number.kind != pgExpressionNumber {
			return nil, fmt.Errorf("signed expression must be numeric literal")
		}
		value := number.value
		if token.value == "-" {
			value = "-" + value
		}
		return &pgParsedExpression{kind: "constant", value: value, fields: map[string]string{"format": "numeric"}}, nil
	case pgExpressionIdentifier:
		upper := strings.ToUpper(token.value)
		switch upper {
		case "TRUE", "FALSE":
			return &pgParsedExpression{kind: "constant", value: upper == "TRUE", fields: map[string]string{"format": "boolean"}}, nil
		case "NULL":
			return &pgParsedExpression{kind: "constant", fields: map[string]string{"format": "null"}}, nil
		case "SESSION_USER", "CURRENT_USER", "CURRENT_ROLE":
			return &pgParsedExpression{kind: "sql_value", fields: map[string]string{"name": strings.ToLower(upper)}}, nil
		case "NOT":
			child, err := parser.parseExpression(50)
			if err != nil {
				return nil, err
			}
			return &pgParsedExpression{kind: "boolean", name: []string{"not"}, fields: map[string]string{"operator": "not"}, children: []*pgParsedExpression{child}}, nil
		case "ARRAY":
			if parser.next().kind != pgExpressionLeftBracket {
				return nil, fmt.Errorf("ARRAY lacks bracket")
			}
			children := make([]*pgParsedExpression, 0)
			if parser.peek().kind != pgExpressionRightBracket {
				for {
					child, err := parser.parseExpression(0)
					if err != nil {
						return nil, err
					}
					children = append(children, child)
					if parser.peek().kind != pgExpressionComma {
						break
					}
					parser.next()
				}
			}
			if parser.next().kind != pgExpressionRightBracket {
				return nil, fmt.Errorf("ARRAY is unclosed")
			}
			return &pgParsedExpression{kind: "array", children: children}, nil
		}
		name := []string{token.value}
		for parser.peek().kind == pgExpressionDot {
			parser.next()
			part := parser.next()
			if part.kind != pgExpressionIdentifier || len(name) >= 2 {
				return nil, fmt.Errorf("expression identity is invalid")
			}
			name = append(name, part.value)
		}
		if parser.peek().kind != pgExpressionLeftParen {
			return &pgParsedExpression{kind: "column", name: name}, nil
		}
		parser.next()
		arguments := make([]*pgParsedExpression, 0)
		if parser.peek().kind != pgExpressionRightParen {
			for {
				argument, err := parser.parseExpression(0)
				if err != nil {
					return nil, err
				}
				arguments = append(arguments, argument)
				if parser.peek().kind != pgExpressionComma {
					break
				}
				parser.next()
			}
		}
		if parser.next().kind != pgExpressionRightParen {
			return nil, fmt.Errorf("function call is unclosed")
		}
		return &pgParsedExpression{kind: "function", name: name, children: arguments}, nil
	default:
		return nil, fmt.Errorf("unexpected expression token")
	}
}

func (parser *pgExpressionParser) parseTypeName() ([]string, error) {
	first := parser.next()
	if first.kind != pgExpressionIdentifier {
		return nil, fmt.Errorf("cast type is absent")
	}
	name := []string{first.value}
	if parser.peek().kind == pgExpressionDot {
		parser.next()
		second := parser.next()
		if second.kind != pgExpressionIdentifier {
			return nil, fmt.Errorf("cast type identity is invalid")
		}
		name = append(name, second.value)
		return name, nil
	}
	if strings.EqualFold(first.value, "timestamp") && parser.keyword("WITH") {
		parser.next()
		if !parser.keyword("TIME") {
			return nil, fmt.Errorf("invalid timestamp cast")
		}
		parser.next()
		if !parser.keyword("ZONE") {
			return nil, fmt.Errorf("invalid timestamp cast")
		}
		parser.next()
		return []string{"timestamptz"}, nil
	}
	if strings.EqualFold(first.value, "character") && parser.keyword("VARYING") {
		parser.next()
		return []string{"varchar"}, nil
	}
	return name, nil
}

func (parser *pgExpressionParser) binaryOperator() (string, int, bool) {
	token := parser.peek()
	if token.kind == pgExpressionOperator {
		switch token.value {
		case "=", "<>", "<", "<=", ">", ">=", "~", "+", "-", "*", "/":
			return token.value, 40, true
		}
	}
	if token.kind == pgExpressionIdentifier {
		switch strings.ToUpper(token.value) {
		case "AND":
			return "and", 20, true
		case "OR":
			return "or", 10, true
		}
	}
	return "", 0, false
}

func (parser *pgExpressionParser) keyword(value string) bool {
	token := parser.peek()
	return token.kind == pgExpressionIdentifier && strings.EqualFold(token.value, value)
}

func (parser *pgExpressionParser) peek() pgExpressionToken {
	if parser.index >= len(parser.tokens) {
		return pgExpressionToken{kind: pgExpressionEOF}
	}
	return parser.tokens[parser.index]
}

func (parser *pgExpressionParser) next() pgExpressionToken {
	token := parser.peek()
	if parser.index < len(parser.tokens) {
		parser.index++
	}
	return token
}

func extractTriggerWhenExpression(definition string) (string, error) {
	marker := " WHEN ("
	start := strings.LastIndex(definition, marker)
	if start < 0 {
		return "", fmt.Errorf("trigger WHEN clause is absent")
	}
	start += len(" WHEN ")
	depth := 0
	inString, inIdentifier := false, false
	for offset := start; offset < len(definition); offset++ {
		switch definition[offset] {
		case '\'':
			if inIdentifier {
				continue
			}
			if inString && offset+1 < len(definition) && definition[offset+1] == '\'' {
				offset++
				continue
			}
			inString = !inString
		case '"':
			if inString {
				continue
			}
			if inIdentifier && offset+1 < len(definition) && definition[offset+1] == '"' {
				offset++
				continue
			}
			inIdentifier = !inIdentifier
		case '(':
			if !inString && !inIdentifier {
				depth++
			}
		case ')':
			if inString || inIdentifier {
				continue
			}
			depth--
			if depth == 0 {
				expression := definition[start+1 : offset]
				remainder := strings.TrimSpace(definition[offset+1:])
				if expression == "" || !strings.HasPrefix(remainder, "EXECUTE FUNCTION ") && !strings.HasPrefix(remainder, "EXECUTE PROCEDURE ") {
					return "", fmt.Errorf("trigger WHEN boundary is invalid")
				}
				return expression, nil
			}
		}
		if depth < 0 {
			return "", fmt.Errorf("trigger WHEN delimiter underflow")
		}
	}
	return "", fmt.Errorf("trigger WHEN clause is unclosed")
}

func (resolver *pgExpressionResolver) normalize(expression *pgParsedExpression, relation *RelationProjection, expected *TypeIdentity, depth uint64) (ExpressionNode, uint64, error) {
	if expression == nil || depth >= projectionMaxExpressionNodes {
		return ExpressionNode{}, 0, pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.expressions.depth", resolver.major, "expression depth exceeds the fixed node limit")
	}
	switch expression.kind {
	case "column":
		if relation == nil || len(expression.name) == 0 || len(expression.name) > 2 {
			return ExpressionNode{}, 0, resolver.invalid("column reference is outside a relation expression")
		}
		name := expression.name[len(expression.name)-1]
		if len(expression.name) == 2 {
			qualifier := strings.ToLower(expression.name[0])
			if qualifier != "new" && qualifier != relation.Identity.Name {
				return ExpressionNode{}, 0, resolver.invalid("qualified column reference is outside the owning relation")
			}
		}
		for _, column := range relation.Columns {
			if column.Name != name {
				continue
			}
			if expected != nil && column.Type != *expected {
				return ExpressionNode{}, 0, resolver.invalid("column reference type differs from its expression context")
			}
			return ExpressionNode{Kind: "column", Type: cloneTypeIdentityPointer(&column.Type), Fields: map[string]string{"name": name}, Children: []ExpressionNode{}}, 1, nil
		}
		return ExpressionNode{}, 0, resolver.invalid("column reference is absent from the owning relation")
	case "constant":
		return resolver.normalizeConstant(expression, expected)
	case "cast":
		if len(expression.name) == 0 || len(expression.children) != 1 {
			return ExpressionNode{}, 0, resolver.invalid("cast expression is incomplete")
		}
		target, err := normalizeExpressionTypeName(expression.name)
		if err != nil || expected != nil && target != *expected {
			return ExpressionNode{}, 0, resolver.invalid("cast target is outside the closed type profile")
		}
		child, childCount, err := resolver.normalize(expression.children[0], relation, &target, depth+1)
		if err != nil {
			return ExpressionNode{}, 0, err
		}
		if child.Kind == "constant" {
			return child, childCount, nil
		}
		return ExpressionNode{Kind: "cast", Type: cloneTypeIdentityPointer(&target), Fields: map[string]string{"coercion": "explicit"}, Children: []ExpressionNode{child}}, childCount + 1, nil
	case "sql_value":
		name := expression.fields["name"]
		sourceType := TypeIdentity{Schema: "pg_catalog", Name: "name"}
		resultType := sourceType
		fields := map[string]string{"name": name}
		if expected != nil {
			if *expected != sourceType && *expected != (TypeIdentity{Schema: "pg_catalog", Name: "text"}) {
				return ExpressionNode{}, 0, resolver.invalid("SQL value function coercion is outside the closed profile")
			}
			resultType = *expected
			if resultType != sourceType {
				fields["source_type"] = "pg_catalog.name"
				fields["coercion"] = "implicit"
			}
		}
		return ExpressionNode{Kind: "sql_value", Type: cloneTypeIdentityPointer(&resultType), Fields: fields, Children: []ExpressionNode{}}, 1, nil
	case "function":
		return resolver.normalizeFunction(expression, relation, expected, depth)
	case "operator":
		return resolver.normalizeOperator(expression, relation, expected, depth)
	case "boolean":
		boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
		if expected != nil && *expected != boolType {
			return ExpressionNode{}, 0, resolver.invalid("boolean expression result type differs from its context")
		}
		operator := expression.fields["operator"]
		if operator != "and" && operator != "or" && operator != "not" || operator == "not" && len(expression.children) != 1 || operator != "not" && len(expression.children) != 2 {
			return ExpressionNode{}, 0, resolver.invalid("boolean expression shape is invalid")
		}
		children := make([]ExpressionNode, len(expression.children))
		count := uint64(1)
		for index := range expression.children {
			child, childCount, err := resolver.normalize(expression.children[index], relation, &boolType, depth+1)
			if err != nil {
				return ExpressionNode{}, 0, err
			}
			children[index] = child
			count += childCount
		}
		return ExpressionNode{Kind: "boolean", Type: cloneTypeIdentityPointer(&boolType), Fields: map[string]string{"operator": operator}, Children: children}, count, nil
	case "null_test":
		boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
		if expected != nil && *expected != boolType || len(expression.children) != 1 {
			return ExpressionNode{}, 0, resolver.invalid("null test shape or result type is invalid")
		}
		child, childCount, err := resolver.normalize(expression.children[0], relation, nil, depth+1)
		if err != nil {
			return ExpressionNode{}, 0, err
		}
		test := expression.fields["test"]
		if test != "is_null" && test != "is_not_null" {
			return ExpressionNode{}, 0, resolver.invalid("null test kind is outside the closed profile")
		}
		return ExpressionNode{Kind: "null_test", Type: cloneTypeIdentityPointer(&boolType), Fields: map[string]string{"test": test}, Children: []ExpressionNode{child}}, childCount + 1, nil
	case "array":
		return resolver.normalizeArray(expression, relation, expected, depth)
	case "scalar_array_operator":
		return resolver.normalizeScalarArrayOperator(expression, relation, expected, depth)
	default:
		return ExpressionNode{}, 0, resolver.invalid("expression kind is outside the closed profile")
	}
}

func (resolver *pgExpressionResolver) normalizeConstant(expression *pgParsedExpression, expected *TypeIdentity) (ExpressionNode, uint64, error) {
	format := expression.fields["format"]
	var value JSONValue
	var expressionType TypeIdentity
	switch format {
	case "boolean":
		expressionType = TypeIdentity{Schema: "pg_catalog", Name: "bool"}
		value = expression.value.(bool)
	case "string":
		if expected == nil {
			return ExpressionNode{}, 0, resolver.invalid("string constant lacks an exact type")
		}
		expressionType = *expected
		if expressionType.Schema != "pg_catalog" || expressionType.Name != "text" && expressionType.Name != "name" && expressionType.Name != "varchar" {
			return ExpressionNode{}, 0, resolver.invalid("string constant type is outside the closed profile")
		}
		value = expression.value.(string)
	case "numeric":
		raw, ok := expression.value.(string)
		if !ok || raw == "-0" {
			return ExpressionNode{}, 0, resolver.invalid("numeric constant is invalid")
		}
		if expected != nil {
			expressionType = *expected
		} else {
			expressionType = TypeIdentity{Schema: "pg_catalog", Name: "int4"}
		}
		if !isExpressionNumericType(expressionType) {
			return ExpressionNode{}, 0, resolver.invalid("numeric constant type is outside the closed profile")
		}
		switch expressionType.Name {
		case "int4", "int8":
			if strings.ContainsAny(raw, ".eE") {
				return ExpressionNode{}, 0, resolver.invalid("integer constant contains a fractional or exponent form")
			}
			if _, err := ValidateSignedIntegerDecimal(raw, expressionIntegerBits(expressionType)); err != nil {
				return ExpressionNode{}, 0, resolver.invalid("integer constant is outside its exact type")
			}
			value = raw
		case "numeric":
			canonical, err := CanonicalExactNumeric(raw)
			if err != nil {
				return ExpressionNode{}, 0, resolver.invalid("numeric constant is not canonical")
			}
			value = canonical
		case "float4":
			raw = normalizeRyuExponent(strings.ToLower(raw))
			if err := ValidateRyuFloat32(raw); err != nil {
				return ExpressionNode{}, 0, resolver.invalid("float4 constant is not canonical")
			}
			value = raw
		case "float8":
			raw = normalizeRyuExponent(strings.ToLower(raw))
			if err := ValidateRyuFloat64(raw); err != nil {
				return ExpressionNode{}, 0, resolver.invalid("float8 constant is not canonical")
			}
			value = raw
		default:
			return ExpressionNode{}, 0, resolver.invalid("numeric constant type is outside the closed profile")
		}
	case "null":
		if expected == nil {
			return ExpressionNode{}, 0, resolver.invalid("null constant lacks an exact type")
		}
		expressionType = *expected
		value = nil
	default:
		return ExpressionNode{}, 0, resolver.invalid("constant format is outside the closed profile")
	}
	if expected != nil && expressionType != *expected {
		return ExpressionNode{}, 0, resolver.invalid("constant type differs from its expression context")
	}
	return ExpressionNode{Kind: "constant", Type: cloneTypeIdentityPointer(&expressionType), Value: value, Fields: map[string]string{"format": format}, Children: []ExpressionNode{}}, 1, nil
}

func (resolver *pgExpressionResolver) normalizeFunction(expression *pgParsedExpression, relation *RelationProjection, expected *TypeIdentity, depth uint64) (ExpressionNode, uint64, error) {
	if len(expression.name) == 0 || len(expression.name) > 2 {
		return ExpressionNode{}, 0, resolver.invalid("function identity is invalid")
	}
	schema := "pg_catalog"
	name := expression.name[0]
	if len(expression.name) == 2 {
		schema, name = expression.name[0], expression.name[1]
	}
	candidates := make([]pgExpressionFunction, 0, 1)
	for _, function := range resolver.functions {
		if function.identity.Schema == schema && function.identity.Name == name && len(function.identity.Arguments) == len(expression.children) {
			candidates = append(candidates, function)
		}
	}
	if len(candidates) != 1 {
		return ExpressionNode{}, 0, resolver.invalid("function call is unknown or ambiguous")
	}
	function := candidates[0]
	if expected != nil && function.returns != *expected {
		return ExpressionNode{}, 0, resolver.invalid("function result type differs from its expression context")
	}
	children := make([]ExpressionNode, len(expression.children))
	count := uint64(1)
	for index := range expression.children {
		child, childCount, err := resolver.normalize(expression.children[index], relation, &function.identity.Arguments[index], depth+1)
		if err != nil {
			return ExpressionNode{}, 0, err
		}
		children[index] = child
		count += childCount
	}
	identity := cloneProjectionValue(function.identity)
	return ExpressionNode{Kind: "function", Type: cloneTypeIdentityPointer(&function.returns), Identity: &identity, Fields: map[string]string{}, Children: children}, count, nil
}

func (resolver *pgExpressionResolver) normalizeOperator(expression *pgParsedExpression, relation *RelationProjection, expected *TypeIdentity, depth uint64) (ExpressionNode, uint64, error) {
	if len(expression.name) != 1 || len(expression.children) != 2 {
		return ExpressionNode{}, 0, resolver.invalid("operator expression is incomplete")
	}
	left, leftCount, leftErr := resolver.normalize(expression.children[0], relation, nil, depth+1)
	var right ExpressionNode
	var rightCount uint64
	if leftErr == nil && left.Type != nil {
		right, rightCount, leftErr = resolver.normalize(expression.children[1], relation, left.Type, depth+1)
	}
	if leftErr != nil {
		right, rightCount, leftErr = resolver.normalize(expression.children[1], relation, nil, depth+1)
		if leftErr == nil && right.Type != nil {
			left, leftCount, leftErr = resolver.normalize(expression.children[0], relation, right.Type, depth+1)
		}
	}
	if leftErr != nil || left.Type == nil || right.Type == nil || *left.Type != *right.Type {
		return ExpressionNode{}, 0, resolver.invalid("operator operands do not have one exact type")
	}
	resultType, ok := expressionOperatorResult(expression.name[0], *left.Type)
	if !ok || expected != nil && resultType != *expected {
		return ExpressionNode{}, 0, resolver.invalid("operator and operand type are outside the closed profile")
	}
	identity := SQLIdentity{Schema: "pg_catalog", Name: expression.name[0], Arguments: []TypeIdentity{*left.Type, *right.Type}}
	return ExpressionNode{Kind: "operator", Type: cloneTypeIdentityPointer(&resultType), Identity: &identity, Fields: map[string]string{}, Children: []ExpressionNode{left, right}}, leftCount + rightCount + 1, nil
}

func (resolver *pgExpressionResolver) normalizeArray(expression *pgParsedExpression, relation *RelationProjection, expected *TypeIdentity, depth uint64) (ExpressionNode, uint64, error) {
	if len(expression.children) == 0 {
		return ExpressionNode{}, 0, resolver.invalid("empty ARRAY is outside the closed profile")
	}
	var elementType *TypeIdentity
	if expected != nil {
		resolved, ok := expressionArrayElementType(*expected)
		if !ok {
			return ExpressionNode{}, 0, resolver.invalid("ARRAY result type is outside the closed profile")
		}
		elementType = &resolved
	}
	children := make([]ExpressionNode, len(expression.children))
	count := uint64(1)
	for index := range expression.children {
		child, childCount, err := resolver.normalize(expression.children[index], relation, elementType, depth+1)
		if err != nil {
			return ExpressionNode{}, 0, err
		}
		if elementType == nil {
			elementType = child.Type
		}
		if child.Type == nil || *child.Type != *elementType {
			return ExpressionNode{}, 0, resolver.invalid("ARRAY element types differ")
		}
		children[index] = child
		count += childCount
	}
	arrayType, ok := expressionArrayType(*elementType)
	if !ok || expected != nil && arrayType != *expected {
		return ExpressionNode{}, 0, resolver.invalid("ARRAY element type has no closed array identity")
	}
	return ExpressionNode{Kind: "array", Type: cloneTypeIdentityPointer(&arrayType), Fields: map[string]string{}, Children: children}, count, nil
}

func (resolver *pgExpressionResolver) normalizeScalarArrayOperator(expression *pgParsedExpression, relation *RelationProjection, expected *TypeIdentity, depth uint64) (ExpressionNode, uint64, error) {
	boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
	if len(expression.name) != 1 || len(expression.children) != 2 || expected != nil && *expected != boolType {
		return ExpressionNode{}, 0, resolver.invalid("scalar-array operator shape is invalid")
	}
	left, leftCount, err := resolver.normalize(expression.children[0], relation, nil, depth+1)
	if err != nil || left.Type == nil {
		return ExpressionNode{}, 0, resolver.invalid("scalar-array left operand has no exact type")
	}
	arrayType, ok := expressionArrayType(*left.Type)
	if !ok {
		return ExpressionNode{}, 0, resolver.invalid("scalar-array left operand type is unsupported")
	}
	right, rightCount, err := resolver.normalize(expression.children[1], relation, &arrayType, depth+1)
	if err != nil {
		return ExpressionNode{}, 0, err
	}
	if _, ok := expressionOperatorResult(expression.name[0], *left.Type); !ok {
		return ExpressionNode{}, 0, resolver.invalid("scalar-array operator is unsupported")
	}
	quantifier := expression.fields["quantifier"]
	if quantifier != "any" && quantifier != "all" {
		return ExpressionNode{}, 0, resolver.invalid("scalar-array quantifier is unsupported")
	}
	identity := SQLIdentity{Schema: "pg_catalog", Name: expression.name[0], Arguments: []TypeIdentity{*left.Type, *left.Type}}
	return ExpressionNode{Kind: "scalar_array_operator", Type: cloneTypeIdentityPointer(&boolType), Identity: &identity, Fields: map[string]string{"quantifier": quantifier}, Children: []ExpressionNode{left, right}}, leftCount + rightCount + 1, nil
}

func (resolver *pgExpressionResolver) invalid(message string) error {
	return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.normalize", resolver.major, message)
}

func normalizeExpressionTypeName(parts []string) (TypeIdentity, error) {
	schema := "pg_catalog"
	name := parts[0]
	if len(parts) == 2 {
		schema, name = parts[0], parts[1]
	}
	if schema != "pg_catalog" {
		return TypeIdentity{}, fmt.Errorf("unsupported type schema")
	}
	switch strings.ToLower(name) {
	case "bool", "boolean":
		name = "bool"
	case "int4", "integer":
		name = "int4"
	case "int8", "bigint":
		name = "int8"
	case "text", "name", "timestamptz", "varchar", "numeric", "float4", "float8":
		name = strings.ToLower(name)
	default:
		return TypeIdentity{}, fmt.Errorf("unsupported type")
	}
	return TypeIdentity{Schema: schema, Name: name}, nil
}

func expressionOperatorResult(operator string, operand TypeIdentity) (TypeIdentity, bool) {
	boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
	if operand.Schema != "pg_catalog" {
		return TypeIdentity{}, false
	}
	switch operator {
	case "=", "<>":
		switch operand.Name {
		case "bool", "int4", "int8", "name", "text", "timestamptz":
			return boolType, true
		}
	case "<", "<=", ">", ">=":
		switch operand.Name {
		case "int4", "int8", "text", "timestamptz":
			return boolType, true
		}
	case "~":
		if operand.Name == "text" {
			return boolType, true
		}
	case "+", "-", "*", "/":
		if isExpressionNumericType(operand) {
			return operand, true
		}
	}
	return TypeIdentity{}, false
}

func isExpressionNumericType(identity TypeIdentity) bool {
	if identity.Schema != "pg_catalog" {
		return false
	}
	switch identity.Name {
	case "int4", "int8", "numeric", "float4", "float8":
		return true
	default:
		return false
	}
}

func expressionIntegerBits(identity TypeIdentity) int {
	if identity.Name == "int8" {
		return 64
	}
	return 32
}

func expressionArrayType(element TypeIdentity) (TypeIdentity, bool) {
	if element.Schema != "pg_catalog" {
		return TypeIdentity{}, false
	}
	switch element.Name {
	case "bool", "int4", "int8", "text", "timestamptz":
		return TypeIdentity{Schema: "pg_catalog", Name: "_" + element.Name}, true
	default:
		return TypeIdentity{}, false
	}
}

func expressionArrayElementType(array TypeIdentity) (TypeIdentity, bool) {
	if array.Schema != "pg_catalog" || !strings.HasPrefix(array.Name, "_") {
		return TypeIdentity{}, false
	}
	element, err := normalizeExpressionTypeName([]string{strings.TrimPrefix(array.Name, "_")})
	return element, err == nil
}

func cloneTypeIdentityPointer(value *TypeIdentity) *TypeIdentity {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func (resolver *pgExpressionResolver) expressionObject(source pgExpressionSource, relation *RelationProjection, function *FunctionProjection, ordinal uint32) (ObjectIdentityProjection, error) {
	if function != nil {
		return ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: cloneProjectionValue(function.Identity)}}, nil
	}
	if relation == nil {
		return ObjectIdentityProjection{}, resolver.invalid("expression owner is absent")
	}
	switch source.sourceKind {
	case "column":
		return ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.Identity, Name: *source.objectName}}, nil
	case "constraint":
		return ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.Identity, Name: *source.objectName}}, nil
	case "index_predicate", "index_terms":
		return ObjectIdentityProjection{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: *source.objectName}, Relation: relation.Identity}}, nil
	case "policy":
		return ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.Identity, Name: *source.objectName}}, nil
	case "trigger":
		for _, trigger := range relation.Triggers {
			if trigger.Identity.Trigger != nil && trigger.Identity.Trigger.Name == *source.objectName {
				return cloneProjectionValue(trigger.Identity), nil
			}
		}
		return ObjectIdentityProjection{}, pgProjectionFailure(CodeProjectionUnknownObject, "catalog.expressions.trigger", resolver.major, "trigger expression source has no logical trigger identity")
	default:
		_ = ordinal
		return ObjectIdentityProjection{}, resolver.invalid("expression source kind is outside the closed profile")
	}
}

func (resolver *pgExpressionResolver) apply(source pgExpressionSource, relation *RelationProjection, function *FunctionProjection, ordinal uint32, owner ObjectIdentityProjection, node ExpressionNode) error {
	if err := validateExpressionNode(node); err != nil {
		return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.node", resolver.major, "normalized expression node is invalid")
	}
	if function != nil {
		if source.field != "function_argument_default" || ordinal == 0 || int(ordinal) > len(function.Arguments) || function.Arguments[ordinal-1].Default != nil {
			return resolver.invalid("function default source differs from its structural slot")
		}
		function.Arguments[ordinal-1].Default = cloneExpressionNodePointer(&node)
		return resolver.addExpressionDependencies(owner, node)
	}
	if relation == nil {
		return resolver.invalid("relation expression owner is absent")
	}
	switch source.field {
	case "column_default":
		for index := range relation.Columns {
			if relation.Columns[index].Name == *source.objectName && relation.Columns[index].Default == nil {
				relation.Columns[index].Default = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
		}
	case "constraint_expression":
		for index := range relation.Constraints {
			if relation.Constraints[index].Name == *source.objectName && relation.Constraints[index].Expression == nil {
				relation.Constraints[index].Expression = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
		}
	case "index_predicate":
		for index := range relation.Indexes {
			if relation.Indexes[index].Name == *source.objectName && relation.Indexes[index].Predicate == nil {
				relation.Indexes[index].Predicate = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
		}
	case "index_term":
		for index := range relation.Indexes {
			if relation.Indexes[index].Name != *source.objectName || ordinal == 0 || int(ordinal) > len(relation.Indexes[index].Terms) {
				continue
			}
			term := &relation.Indexes[index].Terms[ordinal-1]
			if term.TermKind != "expression" || term.Column != nil || term.Expression != nil {
				return resolver.invalid("index expression term differs from its structural slot")
			}
			term.Expression = cloneExpressionNodePointer(&node)
			return resolver.addExpressionDependencies(owner, node)
		}
	case "policy_using", "policy_with_check":
		for index := range relation.Policies {
			if relation.Policies[index].Name != *source.objectName {
				continue
			}
			if source.field == "policy_using" && relation.Policies[index].Using == nil {
				relation.Policies[index].Using = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
			if source.field == "policy_with_check" && relation.Policies[index].WithCheck == nil {
				relation.Policies[index].WithCheck = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
		}
	case "trigger_when":
		ownerKey, err := canonicalContractKey(owner)
		if err != nil {
			return err
		}
		for index := range relation.Triggers {
			triggerKey, err := canonicalContractKey(relation.Triggers[index].Identity)
			if err != nil {
				return err
			}
			if triggerKey == ownerKey && relation.Triggers[index].When == nil {
				relation.Triggers[index].When = cloneExpressionNodePointer(&node)
				return resolver.addExpressionDependencies(owner, node)
			}
		}
	}
	return resolver.invalid("expression source differs from its structural slot")
}

func cloneExpressionNodePointer(value *ExpressionNode) *ExpressionNode {
	if value == nil {
		return nil
	}
	copyValue := cloneProjectionValue(*value)
	return &copyValue
}

func (resolver *pgExpressionResolver) addExpressionDependencies(owner ObjectIdentityProjection, node ExpressionNode) error {
	var walk func(ExpressionNode) error
	walk = func(current ExpressionNode) error {
		if current.Identity != nil {
			var depended ObjectIdentityProjection
			switch current.Kind {
			case "function":
				depended = ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: cloneProjectionValue(*current.Identity)}}
			case "operator", "scalar_array_operator":
				depended = ObjectIdentityProjection{Operator: &SQLObjectIdentity{Kind: "operator", Identity: cloneProjectionValue(*current.Identity)}}
			}
			if current.Kind == "function" || current.Kind == "operator" || current.Kind == "scalar_array_operator" {
				if err := depended.Validate(); err != nil {
					return err
				}
				dependency := DependencyProjection{Depender: cloneProjectionValue(owner), DependedOn: depended, DependencyKind: "normal"}
				key, err := expressionDependencyKey(dependency)
				if err != nil {
					return err
				}
				present := false
				for _, existing := range resolver.body.Dependencies {
					existingKey, err := expressionDependencyKey(existing)
					if err != nil {
						return err
					}
					if existingKey == key {
						present = true
						break
					}
				}
				if !present {
					resolver.body.Dependencies = append(resolver.body.Dependencies, dependency)
					if uint64(len(resolver.body.Dependencies)) > projectionMaxDependencyEdges {
						return pgProjectionFailure(CodeProjectionLimitExceeded, "catalog.expressions.dependencies", resolver.major, "expression dependency limit exceeded")
					}
				}
			}
		}
		for _, child := range current.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(node)
}

func (resolver *pgExpressionResolver) finishDependencies() error {
	type keyedDependency struct {
		key        string
		dependency DependencyProjection
	}
	keyed := make([]keyedDependency, len(resolver.body.Dependencies))
	for index, dependency := range resolver.body.Dependencies {
		key, err := expressionDependencyKey(dependency)
		if err != nil {
			return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.dependencies", resolver.major, "expression dependency closure is invalid")
		}
		keyed[index] = keyedDependency{key: key, dependency: dependency}
	}
	sort.Slice(keyed, func(left, right int) bool { return keyed[left].key < keyed[right].key })
	for index := 1; index < len(keyed); index++ {
		if keyed[index-1].key == keyed[index].key {
			return pgProjectionFailure(CodeProjectionInvalidExpression, "catalog.expressions.dependencies", resolver.major, "expression dependency closure is invalid")
		}
	}
	for index := range keyed {
		resolver.body.Dependencies[index] = keyed[index].dependency
	}
	return nil
}

func expressionDependencyKey(dependency DependencyProjection) (string, error) {
	depender, err := canonicalContractKey(dependency.Depender)
	if err != nil {
		return "", err
	}
	dependedOn, err := canonicalContractKey(dependency.DependedOn)
	if err != nil {
		return "", err
	}
	return depender + "\x00" + dependedOn + "\x00" + dependency.DependencyKind, nil
}

func validatePGNodeTreeWitness(raw string, expectedRoots int) error {
	if raw == "" || !utf8.ValidString(raw) || strings.IndexByte(raw, 0) >= 0 || expectedRoots <= 0 {
		return fmt.Errorf("raw node tree is invalid")
	}
	braceDepth, parenDepth, bracketDepth, roots := 0, 0, 0, 0
	for offset := 0; offset < len(raw); offset++ {
		switch raw[offset] {
		case '{':
			if braceDepth == 0 {
				roots++
			}
			braceDepth++
			start := offset + 1
			end := start
			for end < len(raw) && raw[end] >= 'A' && raw[end] <= 'Z' {
				end++
			}
			if end == start {
				return fmt.Errorf("node tag is absent")
			}
			if _, ok := expressionRawNodeKinds[raw[start:end]]; !ok {
				return fmt.Errorf("unknown node tag")
			}
		case '}':
			braceDepth--
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		case '[':
			bracketDepth++
		case ']':
			bracketDepth--
		}
		if braceDepth < 0 || parenDepth < 0 || bracketDepth < 0 {
			return fmt.Errorf("raw node delimiter underflow")
		}
	}
	if braceDepth != 0 || parenDepth != 0 || bracketDepth != 0 || roots != expectedRoots {
		return fmt.Errorf("raw node tree cardinality differs")
	}
	return nil
}

func validateExpressionNodeType(node ExpressionNode, expected TypeIdentity) error {
	if err := validateExpressionNode(node); err != nil {
		return err
	}
	if node.Type == nil || *node.Type != expected {
		return invalidProjection("catalog-projection", "expression result type differs from its catalog slot")
	}
	return nil
}

func validateExpressionNode(node ExpressionNode) error {
	count := uint64(0)
	return validateExpressionNodeRecursive(node, &count)
}

func validateExpressionNodeRecursive(node ExpressionNode, count *uint64) error {
	*count = *count + 1
	if *count > projectionMaxExpressionNodes || node.Kind == "" || node.Type == nil || node.Fields == nil || node.Children == nil {
		return invalidProjection("catalog-projection", "expression node is sparse or exceeds the fixed node limit")
	}
	if err := node.Type.Validate(); err != nil {
		return err
	}
	if node.Identity != nil {
		if err := node.Identity.Validate(); err != nil {
			return err
		}
	}
	for _, child := range node.Children {
		if err := validateExpressionNodeRecursive(child, count); err != nil {
			return err
		}
	}
	noIdentity := func() bool { return node.Identity == nil }
	noValue := func() bool { return node.Value == nil }
	switch node.Kind {
	case "column":
		if !noIdentity() || !noValue() || len(node.Children) != 0 || !expressionFieldsExact(node.Fields, "name") || node.Fields["name"] == "" {
			return invalidProjection("catalog-projection", "column expression node shape is invalid")
		}
	case "constant":
		if !noIdentity() || len(node.Children) != 0 || !expressionFieldsExact(node.Fields, "format") {
			return invalidProjection("catalog-projection", "constant expression node shape is invalid")
		}
		switch node.Fields["format"] {
		case "boolean":
			if _, ok := node.Value.(bool); !ok || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) {
				return invalidProjection("catalog-projection", "boolean constant is invalid")
			}
		case "string":
			if value, ok := node.Value.(string); !ok || !utf8.ValidString(value) || node.Type.Schema != "pg_catalog" || node.Type.Name != "text" && node.Type.Name != "name" && node.Type.Name != "varchar" {
				return invalidProjection("catalog-projection", "string constant is invalid")
			}
		case "numeric":
			value, ok := node.Value.(string)
			if !ok || value == "" || !isExpressionNumericType(*node.Type) {
				return invalidProjection("catalog-projection", "numeric constant is invalid")
			}
			switch node.Type.Name {
			case "int4", "int8":
				if _, err := ValidateSignedIntegerDecimal(value, expressionIntegerBits(*node.Type)); err != nil {
					return err
				}
			case "numeric":
				if canonical, err := CanonicalExactNumeric(value); err != nil || canonical != value {
					return invalidProjection("catalog-projection", "numeric constant is not canonical")
				}
			case "float4":
				if err := ValidateRyuFloat32(value); err != nil {
					return err
				}
			case "float8":
				if err := ValidateRyuFloat64(value); err != nil {
					return err
				}
			}
		case "null":
			if node.Value != nil {
				return invalidProjection("catalog-projection", "null constant carries a value")
			}
		default:
			return invalidProjection("catalog-projection", "constant format is outside the closed expression profile")
		}
	case "function":
		if node.Identity == nil || node.Value != nil || len(node.Fields) != 0 || node.Identity.Schema == "" || len(node.Identity.Arguments) != len(node.Children) {
			return invalidProjection("catalog-projection", "function expression node shape is invalid")
		}
		for index := range node.Children {
			if node.Children[index].Type == nil || *node.Children[index].Type != node.Identity.Arguments[index] {
				return invalidProjection("catalog-projection", "function expression argument type differs from its identity")
			}
		}
	case "operator":
		if node.Identity == nil || node.Value != nil || len(node.Fields) != 0 || len(node.Children) != 2 || len(node.Identity.Arguments) != 2 {
			return invalidProjection("catalog-projection", "operator expression node shape is invalid")
		}
		for index := range node.Children {
			if node.Children[index].Type == nil || *node.Children[index].Type != node.Identity.Arguments[index] {
				return invalidProjection("catalog-projection", "operator operand type differs from its identity")
			}
		}
		result, ok := expressionOperatorResult(node.Identity.Name, node.Identity.Arguments[0])
		if !ok || node.Type == nil || *node.Type != result {
			return invalidProjection("catalog-projection", "operator result type differs from its closed operator identity")
		}
	case "boolean":
		operator := node.Fields["operator"]
		if !noIdentity() || !noValue() || !expressionFieldsExact(node.Fields, "operator") || operator != "not" && operator != "and" && operator != "or" || operator == "not" && len(node.Children) != 1 || operator != "not" && len(node.Children) != 2 || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) {
			return invalidProjection("catalog-projection", "boolean expression node shape is invalid")
		}
	case "null_test":
		if !noIdentity() || !noValue() || len(node.Children) != 1 || !expressionFieldsExact(node.Fields, "test") || node.Fields["test"] != "is_null" && node.Fields["test"] != "is_not_null" || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) {
			return invalidProjection("catalog-projection", "null-test expression node shape is invalid")
		}
	case "array":
		if !noIdentity() || !noValue() || len(node.Fields) != 0 || len(node.Children) == 0 {
			return invalidProjection("catalog-projection", "array expression node shape is invalid")
		}
		element, ok := expressionArrayElementType(*node.Type)
		if !ok {
			return invalidProjection("catalog-projection", "array expression type is invalid")
		}
		for _, child := range node.Children {
			if child.Type == nil || *child.Type != element {
				return invalidProjection("catalog-projection", "array element type differs from its array type")
			}
		}
	case "scalar_array_operator":
		if node.Identity == nil || node.Value != nil || len(node.Children) != 2 || !expressionFieldsExact(node.Fields, "quantifier") || node.Fields["quantifier"] != "any" && node.Fields["quantifier"] != "all" || len(node.Identity.Arguments) != 2 || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) {
			return invalidProjection("catalog-projection", "scalar-array expression node shape is invalid")
		}
		if node.Children[0].Type == nil || *node.Children[0].Type != node.Identity.Arguments[0] || node.Identity.Arguments[0] != node.Identity.Arguments[1] {
			return invalidProjection("catalog-projection", "scalar-array operand identity is invalid")
		}
		arrayType, ok := expressionArrayType(node.Identity.Arguments[0])
		if !ok || node.Children[1].Type == nil || *node.Children[1].Type != arrayType {
			return invalidProjection("catalog-projection", "scalar-array array type is invalid")
		}
		if result, ok := expressionOperatorResult(node.Identity.Name, node.Identity.Arguments[0]); !ok || result != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) {
			return invalidProjection("catalog-projection", "scalar-array operator identity is outside the closed profile")
		}
	case "sql_value":
		if !noIdentity() || !noValue() || len(node.Children) != 0 {
			return invalidProjection("catalog-projection", "SQL value expression node shape is invalid")
		}
		name := node.Fields["name"]
		if name != "session_user" && name != "current_user" && name != "current_role" {
			return invalidProjection("catalog-projection", "SQL value expression name is unsupported")
		}
		if len(node.Fields) == 1 {
			if !expressionFieldsExact(node.Fields, "name") || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "name"}) {
				return invalidProjection("catalog-projection", "SQL value expression type is invalid")
			}
		} else if !expressionFieldsExact(node.Fields, "coercion", "name", "source_type") || node.Fields["coercion"] != "implicit" || node.Fields["source_type"] != "pg_catalog.name" || *node.Type != (TypeIdentity{Schema: "pg_catalog", Name: "text"}) {
			return invalidProjection("catalog-projection", "SQL value expression coercion is invalid")
		}
	case "cast":
		if !noIdentity() || !noValue() || len(node.Children) != 1 || !expressionFieldsExact(node.Fields, "coercion") || node.Fields["coercion"] != "explicit" {
			return invalidProjection("catalog-projection", "cast expression node shape is invalid")
		}
	default:
		return invalidProjection("catalog-projection", "expression node kind is outside the closed profile")
	}
	return nil
}

func expressionFieldsExact(fields map[string]string, expected ...string) bool {
	if len(fields) != len(expected) {
		return false
	}
	for _, key := range expected {
		if _, ok := fields[key]; !ok {
			return false
		}
	}
	return true
}

func validateCatalogExpressionClosure(body CatalogProjectionBody) error {
	functions := make(map[string]TypeIdentity)
	for _, function := range closedPGExpressionFunctions(body.Functions) {
		key, err := canonicalContractKey(function.identity)
		if err != nil {
			return err
		}
		if existing, duplicate := functions[key]; duplicate && existing != function.returns {
			return invalidProjection("catalog-projection", "expression function signature has conflicting return types")
		}
		functions[key] = function.returns
	}
	dependencies := make(map[string]struct{}, len(body.Dependencies))
	for _, dependency := range body.Dependencies {
		key, err := expressionDependencyKey(dependency)
		if err != nil {
			return err
		}
		dependencies[key] = struct{}{}
	}

	count := uint64(0)
	consume := func(node *ExpressionNode, relation *RelationProjection, owner ObjectIdentityProjection, expected *TypeIdentity) error {
		if node == nil {
			return nil
		}
		before := uint64(0)
		if err := validateExpressionNodeRecursive(*node, &before); err != nil {
			return err
		}
		if expected != nil && (node.Type == nil || *node.Type != *expected) {
			return invalidProjection("catalog-projection", "expression result type differs from its owning catalog slot")
		}
		context := catalogExpressionValidationContext{relation: relation, owner: owner, functions: functions, dependencies: dependencies}
		if err := validateCatalogExpressionNodeSemantics(*node, context); err != nil {
			return err
		}
		if count > projectionMaxExpressionNodes-before {
			return invalidProjection("catalog-projection", "catalog expression node closure exceeds the fixed limit")
		}
		count += before
		return nil
	}
	for relationIndex := range body.Relations {
		relation := &body.Relations[relationIndex]
		for index := range relation.Columns {
			column := &relation.Columns[index]
			owner := ObjectIdentityProjection{Column: &ColumnObjectIdentity{Kind: "column", Relation: relation.Identity, Name: column.Name}}
			if err := consume(column.Default, relation, owner, &column.Type); err != nil {
				return err
			}
		}
		for index := range relation.Constraints {
			constraint := &relation.Constraints[index]
			owner := ObjectIdentityProjection{Constraint: &ConstraintObjectIdentity{Kind: "constraint", Relation: relation.Identity, Name: constraint.Name}}
			boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
			if err := consume(constraint.Expression, relation, owner, &boolType); err != nil {
				return err
			}
		}
		for index := range relation.Indexes {
			projected := &relation.Indexes[index]
			owner := ObjectIdentityProjection{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: relation.Identity.Schema, Name: projected.Name}, Relation: relation.Identity}}
			boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
			if err := consume(projected.Predicate, relation, owner, &boolType); err != nil {
				return err
			}
			for termIndex := range projected.Terms {
				if err := consume(projected.Terms[termIndex].Expression, relation, owner, nil); err != nil {
					return err
				}
			}
		}
		for index := range relation.Policies {
			policy := &relation.Policies[index]
			owner := ObjectIdentityProjection{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: relation.Identity, Name: policy.Name}}
			boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
			if err := consume(policy.Using, relation, owner, &boolType); err != nil {
				return err
			}
			if err := consume(policy.WithCheck, relation, owner, &boolType); err != nil {
				return err
			}
		}
		for index := range relation.Triggers {
			trigger := &relation.Triggers[index]
			boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
			if err := consume(trigger.When, relation, cloneProjectionValue(trigger.Identity), &boolType); err != nil {
				return err
			}
		}
	}
	for functionIndex := range body.Functions {
		function := &body.Functions[functionIndex]
		owner := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: cloneProjectionValue(function.Identity)}}
		for argumentIndex := range function.Arguments {
			argument := &function.Arguments[argumentIndex]
			if err := consume(argument.Default, nil, owner, &argument.Type); err != nil {
				return err
			}
		}
	}
	return nil
}

type catalogExpressionValidationContext struct {
	relation     *RelationProjection
	owner        ObjectIdentityProjection
	functions    map[string]TypeIdentity
	dependencies map[string]struct{}
}

func validateCatalogExpressionNodeSemantics(node ExpressionNode, context catalogExpressionValidationContext) error {
	if node.Type == nil {
		return invalidProjection("catalog-projection", "expression semantic type is absent")
	}
	switch node.Kind {
	case "column":
		if context.relation == nil {
			return invalidProjection("catalog-projection", "function argument default references a relation column")
		}
		name := node.Fields["name"]
		matched := false
		for _, column := range context.relation.Columns {
			if column.Name == name {
				matched = column.Type == *node.Type
				break
			}
		}
		if !matched {
			return invalidProjection("catalog-projection", "expression column identity or type differs from its owning relation")
		}
	case "function":
		key, err := canonicalContractKey(*node.Identity)
		if err != nil {
			return err
		}
		returns, ok := context.functions[key]
		if !ok || returns != *node.Type {
			return invalidProjection("catalog-projection", "expression function identity or return type is outside the closed catalog closure")
		}
		dependedOn := ObjectIdentityProjection{Function: &SQLObjectIdentity{Kind: "function", Identity: cloneProjectionValue(*node.Identity)}}
		if err := requireCatalogExpressionDependency(context, dependedOn); err != nil {
			return err
		}
	case "operator":
		if node.Identity.Schema != "pg_catalog" || len(node.Identity.Arguments) != 2 || node.Identity.Arguments[0] != node.Identity.Arguments[1] {
			return invalidProjection("catalog-projection", "expression operator identity is outside the closed catalog closure")
		}
		result, ok := expressionOperatorResult(node.Identity.Name, node.Identity.Arguments[0])
		if !ok || result != *node.Type {
			return invalidProjection("catalog-projection", "expression operator result differs from its closed signature")
		}
		dependedOn := ObjectIdentityProjection{Operator: &SQLObjectIdentity{Kind: "operator", Identity: cloneProjectionValue(*node.Identity)}}
		if err := requireCatalogExpressionDependency(context, dependedOn); err != nil {
			return err
		}
	case "scalar_array_operator":
		if node.Identity.Schema != "pg_catalog" || len(node.Identity.Arguments) != 2 || node.Identity.Arguments[0] != node.Identity.Arguments[1] {
			return invalidProjection("catalog-projection", "scalar-array operator identity is outside the closed catalog closure")
		}
		result, ok := expressionOperatorResult(node.Identity.Name, node.Identity.Arguments[0])
		if !ok || result != (TypeIdentity{Schema: "pg_catalog", Name: "bool"}) || *node.Type != result {
			return invalidProjection("catalog-projection", "scalar-array operator result differs from its closed signature")
		}
		dependedOn := ObjectIdentityProjection{Operator: &SQLObjectIdentity{Kind: "operator", Identity: cloneProjectionValue(*node.Identity)}}
		if err := requireCatalogExpressionDependency(context, dependedOn); err != nil {
			return err
		}
	case "boolean":
		boolType := TypeIdentity{Schema: "pg_catalog", Name: "bool"}
		for _, child := range node.Children {
			if child.Type == nil || *child.Type != boolType {
				return invalidProjection("catalog-projection", "boolean expression child is not boolean")
			}
		}
	case "cast":
		if len(node.Children) != 1 || node.Children[0].Type == nil || !expressionCastAllowed(*node.Children[0].Type, *node.Type) {
			return invalidProjection("catalog-projection", "expression cast is outside the closed type conversion profile")
		}
	}
	for _, child := range node.Children {
		if err := validateCatalogExpressionNodeSemantics(child, context); err != nil {
			return err
		}
	}
	return nil
}

func requireCatalogExpressionDependency(context catalogExpressionValidationContext, dependedOn ObjectIdentityProjection) error {
	dependency := DependencyProjection{Depender: cloneProjectionValue(context.owner), DependedOn: dependedOn, DependencyKind: "normal"}
	key, err := expressionDependencyKey(dependency)
	if err != nil {
		return err
	}
	if _, ok := context.dependencies[key]; !ok {
		return invalidProjection("catalog-projection", "expression reference lacks its exact normal dependency edge")
	}
	return nil
}

func expressionCastAllowed(source, target TypeIdentity) bool {
	if source == target {
		return true
	}
	if source.Schema != "pg_catalog" || target.Schema != "pg_catalog" {
		return false
	}
	textLike := func(name string) bool { return name == "name" || name == "text" || name == "varchar" }
	if textLike(source.Name) && textLike(target.Name) {
		return true
	}
	return isExpressionNumericType(source) && isExpressionNumericType(target)
}
