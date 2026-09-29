package migrationcore

import (
	"fmt"

	"strings"

	"unicode/utf8"
)

type pgExpressionFunction struct {
	identity SQLIdentity
	returns  TypeIdentity
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
